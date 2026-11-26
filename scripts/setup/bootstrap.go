package main

import (
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/anomaly"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const (
	// costRegion は、Cost Explorer の API の唯一の endpoint の region。
	costRegion   = "us-east-1"
	notFoundCode = "ResourceNotFoundException"
)

var (
	errContactInput = xerrors.New("連絡先の形が誤っています")

	alternateTypes = []string{"BILLING", "OPERATIONS", "SECURITY"}
	countryCode    = regexp.MustCompile(`^[A-Z]{2}$`)
	primaryPhone   = regexp.MustCompile(`^\+[0-9]{2,19}$`)
	// alternatePhone は、代替連絡先の電話番号（PutAlternateContact の PhoneNumber の文字と長さ）。
	alternatePhone = regexp.MustCompile(`^[0-9()+-][0-9()+ -]{0,24}$`)
	contactEmail   = regexp.MustCompile(`^[\w+=.#|!&-]+@[\w.-]+\.\w+$`)
	stateRequired  = []string{"US", "CA", "GB", "DE", "JP", "IN", "BR"}
)

// requireBootstrapUser は、資格情報が IAM user（bootstrap の一時 IAM user）のものであることを求め、書き込む先を出す。
// root と role の資格情報では書かない。組織はまだ無いので、組織の管理アカウントであることは確かめない。
func requireBootstrapUser(c *client, profile string, w io.Writer) error {
	var caller struct {
		Arn string `json:"Arn"`
	}

	if err := c.read(&caller, "sts", "get-caller-identity"); err != nil {
		return err
	}

	if caller.Arn == "" {
		return xerrors.Wrap(errAWSResponse, "資格情報の ARN を読めません")
	}

	if !strings.Contains(caller.Arn, ":user/") {
		return xerrors.Wrap(errPrecondition, "profile "+profile+" の資格情報は IAM user のものではありません（bootstrap の一時 IAM user で行います）")
	}

	fmt.Fprintf(w, "# 書き込む先: profile %s、bootstrap の一時 IAM user\n", profile)

	return nil
}

// contactInformation は、主の連絡先（put-contact-information の ContactInformation）。
type contactInformation struct {
	FullName      string `json:"FullName"`
	CompanyName   string `json:"CompanyName,omitempty"`
	AddressLine1  string `json:"AddressLine1"`
	AddressLine2  string `json:"AddressLine2,omitempty"`
	City          string `json:"City"`
	StateOrRegion string `json:"StateOrRegion,omitempty"`
	PostalCode    string `json:"PostalCode"`
	CountryCode   string `json:"CountryCode"`
	PhoneNumber   string `json:"PhoneNumber"`
}

// alternateContact は、代替連絡先の1つ（put-alternate-contact の引数）。
type alternateContact struct {
	Name         string `json:"Name"`
	Title        string `json:"Title"`
	EmailAddress string `json:"EmailAddress"`
	PhoneNumber  string `json:"PhoneNumber"`
}

func readContact(c *client) (contactInformation, bool, error) {
	var resp struct {
		ContactInformation contactInformation `json:"ContactInformation"`
	}

	found, err := c.readOptional(notFoundCode, &resp, "account", "get-contact-information")

	return resp.ContactInformation, found, err
}

func readAlternate(c *client, typ string) (alternateContact, bool, error) {
	var resp struct {
		AlternateContact alternateContact `json:"AlternateContact"`
	}

	found, err := c.readOptional(notFoundCode, &resp, "account", "get-alternate-contact", "--alternate-contact-type", typ)

	return resp.AlternateContact, found, err
}

func askContact(t *terminal) (contactInformation, error) {
	var ci contactInformation

	fields := []struct {
		prompt   string
		dst      *string
		max      int
		optional bool
	}{
		{"氏名（Full name）: ", &ci.FullName, 50, false},
		{"会社名（任意）: ", &ci.CompanyName, 50, true},
		{"住所1（番地・建物）: ", &ci.AddressLine1, 60, false},
		{"住所2（任意）: ", &ci.AddressLine2, 60, true},
		{"市区町村（City）: ", &ci.City, 50, false},
		{"都道府県・州（JP・US などでは必須）: ", &ci.StateOrRegion, 50, true},
		{"郵便番号: ", &ci.PostalCode, 20, false},
		{"国（ISO 3166 の2文字、例 JP）: ", &ci.CountryCode, 2, false},
		{"電話番号（+ と国番号から、空白なし。SMS を受けられる番号）: ", &ci.PhoneNumber, 20, false},
	}

	for _, f := range fields {
		v, err := t.ask(f.prompt)
		if err != nil {
			return contactInformation{}, err
		}

		if (v == "" && !f.optional) || len([]rune(v)) > f.max {
			return contactInformation{}, xerrors.Wrap(errContactInput, strings.TrimSpace(f.prompt))
		}

		*f.dst = v
	}

	switch {
	case !countryCode.MatchString(ci.CountryCode):
		return contactInformation{}, xerrors.Wrap(errContactInput, "国は大文字の2文字です")
	case !primaryPhone.MatchString(ci.PhoneNumber):
		return contactInformation{}, xerrors.Wrap(errContactInput, "電話番号は + と国番号から始め、空白を含めません")
	case slices.Contains(stateRequired, ci.CountryCode) && ci.StateOrRegion == "":
		return contactInformation{}, xerrors.Wrap(errContactInput, ci.CountryCode+" では都道府県・州が必須です")
	}

	return ci, nil
}

// askAlternate は、typ の代替連絡先を端末から読む。prev があり、氏名を空にすれば prev と同じにする。
func askAlternate(t *terminal, typ string, prev *alternateContact) (alternateContact, error) {
	hint := ""
	if prev != nil {
		hint = "（空なら1つ前と同じ）"
	}

	name, err := t.ask(typ + " の代替連絡先の氏名" + hint + ": ")
	if err != nil {
		return alternateContact{}, err
	}

	if name == "" && prev != nil {
		return *prev, nil
	}

	ac := alternateContact{Name: name}

	for _, f := range []struct {
		prompt string
		dst    *string
	}{
		{typ + " の役職: ", &ac.Title},
		{typ + " のメール: ", &ac.EmailAddress},
		{typ + " の電話番号: ", &ac.PhoneNumber},
	} {
		v, err := t.ask(f.prompt)
		if err != nil {
			return alternateContact{}, err
		}

		*f.dst = v
	}

	switch {
	case ac.Name == "" || len([]rune(ac.Name)) > 64:
		return alternateContact{}, xerrors.Wrap(errContactInput, typ+" の氏名は1〜64文字です")
	case ac.Title == "" || len([]rune(ac.Title)) > 50:
		return alternateContact{}, xerrors.Wrap(errContactInput, typ+" の役職は1〜50文字です")
	case !contactEmail.MatchString(ac.EmailAddress) || len(ac.EmailAddress) > 254:
		return alternateContact{}, xerrors.Wrap(errContactInput, typ+" のメールの形ではありません")
	case !alternatePhone.MatchString(ac.PhoneNumber):
		return alternateContact{}, xerrors.Wrap(errContactInput, typ+" の電話番号の形ではありません")
	}

	return ac, nil
}

// setContacts は、bootstrap 2-1: 主の連絡先と、BILLING・OPERATIONS・SECURITY の代替連絡先を置く。個人の情報なので、
// 引数に載せず一時ファイルで渡す。
func setContacts(o options, c *client, t *terminal) error {
	current, hasCurrent, err := readContact(c)
	if err != nil {
		return err
	}

	alternates := map[string]alternateContact{}

	for _, typ := range alternateTypes {
		a, found, err := readAlternate(c, typ)
		if err != nil {
			return err
		}

		if found {
			alternates[typ] = a
		}
	}

	fmt.Fprintf(t.out, "  主の連絡先: %s\n", map[bool]string{true: "在る", false: "無い"}[hasCurrent])

	for _, typ := range alternateTypes {
		_, ok := alternates[typ]
		fmt.Fprintf(t.out, "  代替連絡先 %s: %s\n", typ, map[bool]string{true: "在る", false: "無い"}[ok])
	}

	if err := t.requireTTY(); err != nil {
		return err
	}

	desired, err := askContact(t)
	if err != nil {
		return err
	}

	desiredAlt := map[string]alternateContact{}

	var prev *alternateContact

	for _, typ := range alternateTypes {
		a, err := askAlternate(t, typ, prev)
		if err != nil {
			return err
		}

		desiredAlt[typ] = a
		prev = &a
	}

	writeContact := !hasCurrent || current != desired

	var writeTypes []string

	for _, typ := range alternateTypes {
		if a, ok := alternates[typ]; !ok || a != desiredAlt[typ] {
			writeTypes = append(writeTypes, typ)
		}
	}

	if !writeContact && len(writeTypes) == 0 {
		fmt.Fprintln(t.out, "✅ 連絡先は入力と同じです。書き込みません")

		return nil
	}

	fmt.Fprintf(t.out, "書き込むもの: 主の連絡先 %s、代替連絡先 %s\n", map[bool]string{true: "変える", false: "変えない"}[writeContact], joinOrNone(writeTypes))

	if err := t.confirm(o.project); err != nil {
		return err
	}

	if writeContact {
		if err := c.writeSecret(map[string]any{"ContactInformation": desired}, "account", "put-contact-information"); err != nil {
			return err
		}
	}

	for _, typ := range writeTypes {
		a := desiredAlt[typ]
		if err := c.writeSecret(map[string]any{
			"AlternateContactType": typ, "Name": a.Name, "Title": a.Title, "EmailAddress": a.EmailAddress, "PhoneNumber": a.PhoneNumber,
		}, "account", "put-alternate-contact"); err != nil {
			return err
		}
	}

	return verifyContacts(o, c, t.out, desired, desiredAlt)
}

func verifyContacts(o options, c *client, w io.Writer, desired contactInformation, desiredAlt map[string]alternateContact) error {
	if err := reread(o, func() (bool, error) {
		got, found, err := readContact(c)

		return found && got == desired, err
	}); err != nil {
		return xerrors.Wrap(err, "書いた後の主の連絡先が入力と一致しません。make bootstrap-check-account で確かめてください")
	}

	for _, typ := range alternateTypes {
		if err := reread(o, func() (bool, error) {
			got, found, err := readAlternate(c, typ)

			return found && got == desiredAlt[typ], err
		}); err != nil {
			return xerrors.Wrap(err, "書いた後の代替連絡先 "+typ+" が入力と一致しません。make bootstrap-check-account で確かめてください")
		}
	}

	fmt.Fprintln(w, "✅ 連絡先を置きました")

	return nil
}

// anomalyTargets は、AWS services monitor と、その subscription の ARN。ちょうど1つに決まらなければ errPrecondition。
func anomalyTargets(c *client) (string, string, error) {
	var monitors struct {
		AnomalyMonitors []anomaly.Monitor `json:"AnomalyMonitors"`
	}

	if err := c.readIn(costRegion, &monitors, "ce", "get-anomaly-monitors"); err != nil {
		return "", "", err
	}

	monitor, reason := anomaly.ServiceMonitor(monitors.AnomalyMonitors)
	if reason != "" {
		return "", "", xerrors.Wrap(errPrecondition, reason+"。Cost Explorer を有効にした翌日以降に行います（2-2）")
	}

	var subs struct {
		AnomalySubscriptions []anomaly.Subscription `json:"AnomalySubscriptions"`
	}

	if err := c.readIn(costRegion, &subs, "ce", "get-anomaly-subscriptions"); err != nil {
		return "", "", err
	}

	sub, reason := anomaly.MonitorSubscription(subs.AnomalySubscriptions, monitor)
	if reason != "" {
		return "", "", xerrors.Wrap(errPrecondition, reason)
	}

	return monitor, sub, nil
}

// hasCostTag は、Cost Explorer の resource が key の tag を持つか。値は見ない。
func hasCostTag(c *client, arn, key string) (bool, error) {
	var resp struct {
		ResourceTags *[]struct {
			Key string `json:"Key"`
		} `json:"ResourceTags"`
	}

	if err := c.readIn(costRegion, &resp, "ce", "list-tags-for-resource", "--resource-arn", arn); err != nil {
		return false, err
	}

	if resp.ResourceTags == nil {
		return false, xerrors.Wrap(errAWSResponse, "ce list-tags-for-resource: ResourceTags がありません")
	}

	for _, t := range *resp.ResourceTags {
		if t.Key == key {
			return true, nil
		}
	}

	return false, nil
}

// tagAnomaly は、bootstrap 2-4: AWS services monitor とその subscription に tag <project>:bootstrap を付ける。
func tagAnomaly(o options, c *client, t *terminal) error {
	monitor, sub, err := anomalyTargets(c)
	if err != nil {
		return err
	}

	key := o.project + ":bootstrap"

	var missing []string

	for _, arn := range []string{monitor, sub} {
		has, err := hasCostTag(c, arn, key)
		if err != nil {
			return err
		}

		fmt.Fprintf(t.out, "  %s: tag %s %s\n", arn, key, map[bool]string{true: "在る", false: "無い"}[has])

		if !has {
			missing = append(missing, arn)
		}
	}

	if len(missing) == 0 {
		fmt.Fprintln(t.out, "✅ どちらにも tag が在ります。書き込みません")

		return nil
	}

	if err := t.confirm(o.project); err != nil {
		return err
	}

	for _, arn := range missing {
		if err := c.writeIn(costRegion, "ce", "tag-resource", "--resource-arn", arn, "--resource-tags", "Key="+key+",Value=true"); err != nil {
			return err
		}

		if err := reread(o, func() (bool, error) { return hasCostTag(c, arn, key) }); err != nil {
			return xerrors.Wrap(err, "tag を付けた後に "+arn+" の tag を読めません。make bootstrap-check-account で確かめてください")
		}
	}

	fmt.Fprintln(t.out, "✅ tag を付けました")

	return nil
}

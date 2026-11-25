package main

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

var (
	// emailPattern は、accounts.tf の precondition がメールに課す形と同じもの。
	emailPattern = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
	otpPattern   = regexp.MustCompile(`^[A-Za-z0-9]{6,8}$`)

	// errEmailShape は、入力されたメールが precondition の形に合わないこと。
	errEmailShape     = xerrors.New("メールの形ではありません")
	errEmailDuplicate = xerrors.New("同じメールが2つの env に在ります")
	errEmailInUse     = xerrors.New("メールが組織のほかのアカウントで使われています")
	// errEmailChangeNeedsUpdate は、既に在るアカウントのメールを parameter だけで変えようとしたこと。
	errEmailChangeNeedsUpdate = xerrors.New("既に在るアカウントのメールは parameter だけでは変えません。make setup-organization-baseline-member-email-update MEMBER_ENV=<env> で AWS の側から変えてください")
	// errParameterUnreadable は、parameter の値が文字列の JSON object として読めないこと。
	errParameterUnreadable = xerrors.New("parameter の値を、文字列の JSON object として読めません")
	// errPrecondition は、手順の実行の前提が崩れていること。書かずに終える。
	errPrecondition = xerrors.New("前提が崩れているので、書き込みません")
	errVerify       = xerrors.New("書き込んだ後の読み取りが期待と一致しません")
	errOTPShape     = xerrors.New("確認コードは英数字の6から8文字です")

	accountManagementPrincipal = "account.amazonaws.com"
)

// emailsParameter は、メンバーアカウントのメールを置く parameter の名前（accounts.tf の data source と同じ）。
func emailsParameter(project string) string {
	return "/" + project + "/organization-baseline/member-account-emails"
}

// readEmails は、parameter を読む。無ければ found が false。値が読めなければ errParameterUnreadable。
func readEmails(c *client, project string) (map[string]string, bool, error) {
	var resp struct {
		Parameter struct {
			Value string `json:"Value"`
		} `json:"Parameter"`
	}

	found, err := c.readOptional("ParameterNotFound", &resp, "ssm", "get-parameter", "--name", emailsParameter(project))
	if err != nil || !found {
		return nil, found, err
	}

	var emails map[string]string
	if err := json.Unmarshal([]byte(resp.Parameter.Value), &emails); err != nil || emails == nil {
		return nil, true, errParameterUnreadable
	}

	return emails, true, nil
}

// writeEmails は、parameter を emails の JSON で置き、読み直して一致を確かめる。
func writeEmails(c *client, project string, emails map[string]string) error {
	value, err := json.Marshal(emails)
	if err != nil {
		return err
	}

	if err := c.write("ssm", "put-parameter", "--name", emailsParameter(project), "--type", "String", "--overwrite", "--value", string(value)); err != nil {
		return err
	}

	got, found, err := readEmails(c, project)
	if err != nil {
		return err
	}

	if !found || !maps.Equal(got, emails) {
		return xerrors.Wrap(errVerify, emailsParameter(project))
	}

	return nil
}

// checkEmailUse は、env のメールが、ほかの env と、組織の env 以外のアカウントで使われていないことを確かめる。
func checkEmailUse(project string, emails map[string]string, accounts []orgAccount) error {
	seen := map[string]string{}
	keys := slices.Sorted(maps.Keys(emails))

	for _, env := range keys {
		e := strings.ToLower(emails[env])
		if other, ok := seen[e]; ok {
			return xerrors.Wrap(errEmailDuplicate, other+" と "+env)
		}

		seen[e] = env
	}

	for _, env := range keys {
		for _, a := range accounts {
			if strings.EqualFold(a.Email, emails[env]) && a.Name != accountName(project, env) {
				return xerrors.Wrap(errEmailInUse, env+": "+a.Name)
			}
		}
	}

	return nil
}

// putMemberEmails は、3.8 手順3 の手順の実行。env の一覧を対応表から取り、メールを端末から受けて parameter を置く。
func putMemberEmails(o options, c *client, t *terminal, members []memberAccount) error {
	current, found, err := readEmails(c, o.project)
	if err != nil && !xerrors.Is(err, errParameterUnreadable) {
		return err
	}

	if xerrors.Is(err, errParameterUnreadable) {
		fmt.Fprintln(t.out, "⚠️ 今の parameter の値を読めないので、すべての env を入れ直します")

		current = nil
	}

	accounts, err := listAccounts(c)
	if err != nil {
		return err
	}

	if err := t.requireTTY(); err != nil {
		return err
	}

	desired := map[string]string{}

	for _, m := range members {
		prompt := m.env + " のメール: "
		if cur := current[m.env]; cur != "" {
			prompt = fmt.Sprintf("%s のメール（空なら今の %s）: ", m.env, cur)
		}

		v, err := t.ask(prompt)
		if err != nil {
			return err
		}

		if v == "" {
			v = current[m.env]
		}

		if !emailPattern.MatchString(v) {
			return xerrors.Wrap(errEmailShape, m.env+": "+v)
		}

		if a, ok := accountNamed(accounts, accountName(o.project, m.env)); ok && !strings.EqualFold(a.Email, v) {
			return xerrors.Wrap(errEmailChangeNeedsUpdate, m.env)
		}

		desired[m.env] = v
	}

	if err := checkEmailUse(o.project, desired, accounts); err != nil {
		return err
	}

	if found && maps.Equal(current, desired) {
		fmt.Fprintf(t.out, "✅ %s は既に入力どおりです。書き込みません\n", emailsParameter(o.project))

		return nil
	}

	printEmailDiff(t.out, emailsParameter(o.project), current, desired)

	if err := t.confirm(o.project); err != nil {
		return err
	}

	if err := writeEmails(c, o.project, desired); err != nil {
		return err
	}

	fmt.Fprintf(t.out, "✅ %s を置き、読み直して一致を確かめました\n", emailsParameter(o.project))

	return nil
}

func printEmailDiff(w io.Writer, name string, current, desired map[string]string) {
	fmt.Fprintf(w, "# %s に書き込む差分\n", name)

	keys := map[string]bool{}
	for k := range current {
		keys[k] = true
	}

	for k := range desired {
		keys[k] = true
	}

	for _, k := range slices.Sorted(maps.Keys(keys)) {
		cur, inCur := current[k]
		want, inWant := desired[k]

		switch {
		case !inCur:
			fmt.Fprintf(w, "  + %s: %s\n", k, want)
		case !inWant:
			fmt.Fprintf(w, "  - %s: %s\n", k, cur)
		case cur != want:
			fmt.Fprintf(w, "  ~ %s: %s → %s\n", k, cur, want)
		default:
			fmt.Fprintf(w, "    %s: %s\n", k, want)
		}
	}
}

// checkMemberEmails は、3.8 手順3 の確認。parameter の key が対応表の env と一致し、値がメールの形で、
// 既に在るアカウントのメールと一致することを確かめる。
func checkMemberEmails(o options, c *client, w io.Writer, members []memberAccount) error {
	r := newReport(w, "メンバーアカウントのメールの parameter（"+emailsParameter(o.project)+"）")

	emails, found, err := readEmails(c, o.project)

	switch {
	case xerrors.Is(err, errParameterUnreadable):
		r.ng("値が文字列の JSON object ではない")

		return r.result()
	case err != nil:
		return err
	case !found:
		r.ng("parameter が無い")

		return r.result()
	}

	accounts, err := listAccounts(c)
	if err != nil {
		return err
	}

	want := envs(members)
	got := slices.Sorted(maps.Keys(emails))

	r.check(slices.Equal(want, got), fmt.Sprintf("key が対応表の env と一致する（対応表: %s、parameter: %s）", strings.Join(want, "・"), strings.Join(got, "・")))

	for _, env := range got {
		r.check(emailPattern.MatchString(emails[env]), env+" の値がメールの形である")

		if a, ok := accountNamed(accounts, accountName(o.project, env)); ok {
			r.check(strings.EqualFold(a.Email, emails[env]), env+" の値が、既に在るアカウントのメールと一致する")
		}
	}

	r.check(checkEmailUse(o.project, emails, accounts) == nil, "メールが env の間で重ならず、組織のほかのアカウントで使われていない")

	return r.result()
}

// updateMemberEmail は、3.8 末尾の手順の実行。env のアカウントのメールを AWS の側で変え、その後に parameter を合わせる。
func updateMemberEmail(o options, c *client, t *terminal, members []memberAccount) error {
	if !slices.Contains(envs(members), o.env) {
		return xerrors.Wrap(errPrecondition, "env "+o.env+" は対応表に在りません")
	}

	emails, found, err := readEmails(c, o.project)
	if err != nil {
		return err
	}

	if !found {
		return xerrors.Wrap(errPrecondition, emailsParameter(o.project)+" が在りません")
	}

	accounts, err := listAccounts(c)
	if err != nil {
		return err
	}

	account, ok := accountNamed(accounts, accountName(o.project, o.env))
	if !ok || account.State != "ACTIVE" {
		return xerrors.Wrap(errPrecondition, accountName(o.project, o.env)+" が ACTIVE で在りません")
	}

	var primary struct {
		PrimaryEmail string `json:"PrimaryEmail"`
	}

	if err := c.read(&primary, "account", "get-primary-email", "--account-id", account.ID); err != nil {
		return err
	}

	if err := t.requireTTY(); err != nil {
		return err
	}

	next, err := t.ask(fmt.Sprintf("%s の新しいメール（今は %s）: ", o.env, primary.PrimaryEmail))
	if err != nil {
		return err
	}

	if !emailPattern.MatchString(next) {
		return xerrors.Wrap(errEmailShape, o.env+": "+next)
	}

	desired := maps.Clone(emails)
	desired[o.env] = next

	if err := checkEmailUse(o.project, desired, accounts); err != nil {
		return err
	}

	awsDone := strings.EqualFold(primary.PrimaryEmail, next)
	if awsDone && maps.Equal(emails, desired) {
		fmt.Fprintf(t.out, "✅ %s のメールは AWS の側も parameter も既に %s です。書き込みません\n", o.env, next)

		return nil
	}

	trusted, err := serviceAccessEnabled(c)
	if err != nil {
		return err
	}

	needTrust := !awsDone && !slices.Contains(trusted, accountManagementPrincipal)

	fmt.Fprintln(t.out, "# 書き込む順")

	if needTrust {
		fmt.Fprintf(t.out, "  1. Organizations の信頼アクセスを %s に有効にする\n", accountManagementPrincipal)
	}

	if !awsDone {
		fmt.Fprintf(t.out, "  2. %s の主のメールを %s → %s に変える（新しいメールに届く確認コードを入力する）\n", account.Name, primary.PrimaryEmail, next)
	}

	printEmailDiff(t.out, "  3. "+emailsParameter(o.project), emails, desired)

	if err := t.confirm(o.project); err != nil {
		return err
	}

	if needTrust {
		if err := c.write("organizations", "enable-aws-service-access", "--service-principal", accountManagementPrincipal); err != nil {
			return err
		}
	}

	if !awsDone {
		if err := changePrimaryEmail(c, t, account.ID, next); err != nil {
			return err
		}
	}

	if err := writeEmails(c, o.project, desired); err != nil {
		return err
	}

	fmt.Fprintf(t.out, "✅ %s のメールを %s に変え、parameter を合わせました\n", o.env, next)

	return nil
}

// changePrimaryEmail は、主のメールの変更を始め、確認コードを受けて確定し、読み直して確かめる。
func changePrimaryEmail(c *client, t *terminal, accountID, next string) error {
	if err := c.write("account", "start-primary-email-update", "--account-id", accountID, "--primary-email", next); err != nil {
		return err
	}

	otp, err := t.ask(next + " に届いた確認コード: ")
	if err != nil {
		return err
	}

	if !otpPattern.MatchString(otp) {
		return errOTPShape
	}

	if err := c.write("account", "accept-primary-email-update", "--account-id", accountID, "--primary-email", next, "--otp", otp); err != nil {
		return err
	}

	var primary struct {
		PrimaryEmail string `json:"PrimaryEmail"`
	}

	if err := c.read(&primary, "account", "get-primary-email", "--account-id", accountID); err != nil {
		return err
	}

	if !strings.EqualFold(primary.PrimaryEmail, next) {
		return xerrors.Wrap(errVerify, "主のメールが "+primary.PrimaryEmail+" のままです（反映を待ってから同じ入口をやり直すと、parameter だけを合わせます）")
	}

	return nil
}

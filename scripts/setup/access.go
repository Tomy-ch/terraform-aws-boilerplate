package main

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const (
	stackSetsOrgAdminRole = "AWSServiceRoleForCloudFormationStackSetsOrgAdmin"
	// serviceQuotasRegion は、Organizations の quota を Service Quotas から読むときの region。
	serviceQuotasRegion = "us-east-1"
	accountsQuotaName   = "Maximum number of accounts"
)

var (
	// securityServicePrincipals は、3.9 で信頼アクセスを有効にするサービス。委任管理者の指定の API が
	// 信頼アクセスを有効にするサービス（GuardDuty・Inspector・Security Hub）は含めない。
	securityServicePrincipals = []string{"access-analyzer.amazonaws.com", "cloudtrail.amazonaws.com", "config.amazonaws.com"}

	errQuotaNotFound = xerrors.New("Organizations のアカウント数の quota がただ1つに決まりません")
)

// stackSetsAccess は、StackSets の信頼アクセスの状態（ENABLED・DISABLED・DISABLED_PERMANENTLY）。
func stackSetsAccess(c *client) (string, error) {
	var resp struct {
		Status string `json:"Status"`
	}

	if err := c.read(&resp, "cloudformation", "describe-organizations-access"); err != nil {
		return "", err
	}

	return resp.Status, nil
}

func roleExists(c *client, name string) (bool, error) {
	var resp struct{}

	return c.readOptional("NoSuchEntity", &resp, "iam", "get-role", "--role-name", name)
}

// checkStackSetsAccess は、3.5 の確認。StackSets の信頼アクセスが有効で、管理アカウントにその service-linked role が在る。
func checkStackSetsAccess(c *client, w io.Writer) error {
	r := newReport(w, "StackSets の信頼アクセス")

	status, err := stackSetsAccess(c)
	if err != nil {
		return err
	}

	r.check(status == "ENABLED", "信頼アクセスが ENABLED である（"+status+"）")

	exists, err := roleExists(c, stackSetsOrgAdminRole)
	if err != nil {
		return err
	}

	r.check(exists, stackSetsOrgAdminRole+" が在る")

	return r.result()
}

// enableStackSetsAccess は、3.5 の手順の実行。StackSets の信頼アクセスを有効にする。
func enableStackSetsAccess(o options, c *client, t *terminal) error {
	status, err := stackSetsAccess(c)
	if err != nil {
		return err
	}

	switch status {
	case "ENABLED":
		fmt.Fprintln(t.out, "✅ StackSets の信頼アクセスは既に ENABLED です。書き込みません")

		return nil
	case "DISABLED":
	default:
		return xerrors.Wrap(errPrecondition, "StackSets の信頼アクセスの状態が "+status+" です")
	}

	fmt.Fprintln(t.out, "# 書き込む差分")
	fmt.Fprintln(t.out, "  ~ StackSets の信頼アクセス: DISABLED → ENABLED（管理アカウントに "+stackSetsOrgAdminRole+" が作られます）")

	if err := t.confirm(o.project); err != nil {
		return err
	}

	if err := c.write("cloudformation", "activate-organizations-access"); err != nil {
		return err
	}

	return checkStackSetsAccess(c, t.out)
}

// serviceAccessEnabled は、Organizations の信頼アクセスが有効なサービスの principal。
func serviceAccessEnabled(c *client) ([]string, error) {
	var resp struct {
		EnabledServicePrincipals []struct {
			ServicePrincipal string `json:"ServicePrincipal"`
		} `json:"EnabledServicePrincipals"`
	}

	if err := c.read(&resp, "organizations", "list-aws-service-access-for-organization"); err != nil {
		return nil, err
	}

	out := make([]string, 0, len(resp.EnabledServicePrincipals))
	for _, p := range resp.EnabledServicePrincipals {
		out = append(out, p.ServicePrincipal)
	}

	return out, nil
}

func missingPrincipals(enabled []string) []string {
	var out []string

	for _, p := range securityServicePrincipals {
		if !slices.Contains(enabled, p) {
			out = append(out, p)
		}
	}

	return out
}

// checkServiceAccess は、3.9 の確認。セキュリティのサービスの信頼アクセスがすべて有効である。
func checkServiceAccess(c *client, w io.Writer) error {
	r := newReport(w, "セキュリティのサービスの信頼アクセス")

	enabled, err := serviceAccessEnabled(c)
	if err != nil {
		return err
	}

	for _, p := range securityServicePrincipals {
		r.check(slices.Contains(enabled, p), p+" の信頼アクセスが有効である")
	}

	return r.result()
}

// enableServiceAccess は、3.9 の手順の実行。無効なセキュリティのサービスの信頼アクセスを有効にする。
func enableServiceAccess(o options, c *client, t *terminal) error {
	enabled, err := serviceAccessEnabled(c)
	if err != nil {
		return err
	}

	missing := missingPrincipals(enabled)
	if len(missing) == 0 {
		fmt.Fprintln(t.out, "✅ セキュリティのサービスの信頼アクセスは既にすべて有効です。書き込みません")

		return nil
	}

	fmt.Fprintln(t.out, "# 書き込む差分")

	for _, p := range missing {
		fmt.Fprintf(t.out, "  + %s の信頼アクセスを有効にする\n", p)
	}

	if err := t.confirm(o.project); err != nil {
		return err
	}

	for _, p := range missing {
		if err := c.write("organizations", "enable-aws-service-access", "--service-principal", p); err != nil {
			return err
		}
	}

	return checkServiceAccess(c, t.out)
}

// accountsQuota は、Organizations のアカウント数の quota の適用値。quota code は変わりうるので、名前で探す。
func accountsQuota(c *client) (int, error) {
	var list struct {
		Quotas []struct {
			QuotaCode string `json:"QuotaCode"`
			QuotaName string `json:"QuotaName"`
		} `json:"Quotas"`
	}

	if err := c.readIn(serviceQuotasRegion, &list, "service-quotas", "list-service-quotas", "--service-code", "organizations"); err != nil {
		return 0, err
	}

	var codes []string

	for _, q := range list.Quotas {
		if q.QuotaName == accountsQuotaName {
			codes = append(codes, q.QuotaCode)
		}
	}

	if len(codes) != 1 {
		return 0, xerrors.Wrap(errQuotaNotFound, fmt.Sprintf("%q が %d 件", accountsQuotaName, len(codes)))
	}

	var quota struct {
		Quota struct {
			Value float64 `json:"Value"`
		} `json:"Quota"`
	}

	if err := c.readIn(serviceQuotasRegion, &quota, "service-quotas", "get-service-quota", "--service-code", "organizations", "--quota-code", codes[0]); err != nil {
		return 0, err
	}

	return int(quota.Quota.Value), nil
}

// checkAccountQuota は、3.8 手順2 の確認。組織のアカウントの数に、まだ無いメンバーアカウントの数を足したものが quota 以下である。
func checkAccountQuota(o options, c *client, w io.Writer, members []memberAccount) error {
	r := newReport(w, "Organizations のアカウント数の上限")

	quota, err := accountsQuota(c)
	if err != nil {
		return err
	}

	accounts, err := listAccounts(c)
	if err != nil {
		return err
	}

	var adding []string

	for _, m := range members {
		if _, ok := accountNamed(accounts, accountName(o.project, m.env)); !ok {
			adding = append(adding, m.env)
		}
	}

	total := len(accounts) + len(adding)
	r.check(total <= quota, fmt.Sprintf("在るアカウント %d と作るアカウント %d（%s）の計 %d が、上限 %d 以下である", len(accounts), len(adding), strings.Join(adding, "・"), total, quota))

	return r.result()
}

// checkMemberAccounts は、3.8 手順4.1 の確認。対応表の env ごとに、そのアカウントが期待する OU の直下に ACTIVE で在る。
// showIDs なら、取り込みの import block の id に使うアカウントの ID を出す。
func checkMemberAccounts(o options, c *client, w io.Writer, members []memberAccount) error {
	r := newReport(w, "メンバーアカウント")

	for _, m := range members {
		ou, err := resolveOU(c, m.ouPath)
		if err != nil {
			return err
		}

		accounts, err := listAccountsForParent(c, ou)
		if err != nil {
			return err
		}

		name := accountName(o.project, m.env)
		a, ok := accountNamed(accounts, name)
		r.check(ok && a.State == "ACTIVE", fmt.Sprintf("%s が %s の直下に ACTIVE で在る（%s）", name, strings.Join(m.ouPath, "/"), stateOf(a, ok)))

		if ok && o.showIDs {
			fmt.Fprintf(w, "   %s の ID: %s\n", m.env, a.ID)
		}
	}

	return r.result()
}

func stateOf(a orgAccount, ok bool) string {
	if !ok {
		return "無い"
	}

	return a.State
}

// checkStackInstances は、3.5 の昇格の後の確認。project の名前で始まる ACTIVE の StackSet の、すべての stack instance が CURRENT である。
func checkStackInstances(o options, c *client, w io.Writer) error {
	r := newReport(w, "StackSet の stack instance")

	var sets struct {
		Summaries []struct {
			StackSetName string `json:"StackSetName"`
		} `json:"Summaries"`
	}

	if err := c.read(&sets, "cloudformation", "list-stack-sets", "--status", "ACTIVE", "--call-as", "SELF"); err != nil {
		return err
	}

	for _, s := range sets.Summaries {
		if !strings.HasPrefix(s.StackSetName, o.project+"-") {
			continue
		}

		var instances struct {
			Summaries []struct {
				Account string `json:"Account"`
				Region  string `json:"Region"`
				Status  string `json:"Status"`
			} `json:"Summaries"`
		}

		if err := c.read(&instances, "cloudformation", "list-stack-instances", "--stack-set-name", s.StackSetName, "--call-as", "SELF"); err != nil {
			return err
		}

		if len(instances.Summaries) == 0 {
			r.ng(s.StackSetName + " に stack instance が無い")

			continue
		}

		for _, i := range instances.Summaries {
			r.check(i.Status == "CURRENT", fmt.Sprintf("%s の %s・%s が CURRENT である（%s）", s.StackSetName, i.Account, i.Region, i.Status))
		}
	}

	return r.result()
}

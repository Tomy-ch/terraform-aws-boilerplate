package main

import (
	"fmt"
	"io"
	"math"
	"slices"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const (
	stackSetsOrgAdminRole = "AWSServiceRoleForCloudFormationStackSetsOrgAdmin"
	serviceQuotasRegion   = "us-east-1"
	accountsQuotaName     = "Maximum number of accounts"
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

	if resp.Status == "" {
		return "", xerrors.Wrap(errAWSResponse, "cloudformation describe-organizations-access: Status がありません")
	}

	return resp.Status, nil
}

// orgAdminRoleExists は、StackSets が信頼アクセスで作る service-linked role が管理アカウントに在るか。
func orgAdminRoleExists(c *client) (bool, error) {
	var resp struct{}

	return c.readOptional("NoSuchEntity", &resp, "iam", "get-role", "--role-name", stackSetsOrgAdminRole)
}

// checkStackSetsAccess は、3.5 の確認。StackSets の信頼アクセスが有効で、管理アカウントにその service-linked role が在る。
func checkStackSetsAccess(c *client, w io.Writer) error {
	r := newReport(w, "StackSets の信頼アクセス")

	status, err := stackSetsAccess(c)
	if err != nil {
		return err
	}

	r.check(status == "ENABLED", "信頼アクセスが ENABLED である（"+status+"）")

	exists, err := orgAdminRoleExists(c)
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
		EnabledServicePrincipals *[]struct {
			ServicePrincipal string `json:"ServicePrincipal"`
		} `json:"EnabledServicePrincipals"`
	}

	if err := c.read(&resp, "organizations", "list-aws-service-access-for-organization"); err != nil {
		return nil, err
	}

	if resp.EnabledServicePrincipals == nil {
		return nil, xerrors.Wrap(errAWSResponse, "organizations list-aws-service-access-for-organization: EnabledServicePrincipals がありません")
	}

	out := make([]string, 0, len(*resp.EnabledServicePrincipals))

	for _, p := range *resp.EnabledServicePrincipals {
		if p.ServicePrincipal == "" {
			return nil, xerrors.Wrap(errAWSResponse, "organizations list-aws-service-access-for-organization: ServicePrincipal が空です")
		}

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

	if len(codes) != 1 || codes[0] == "" {
		return 0, xerrors.Wrapf(errQuotaNotFound, "%q が %d 件", accountsQuotaName, len(codes))
	}

	var quota struct {
		Quota struct {
			Value *float64 `json:"Value"`
		} `json:"Quota"`
	}

	if err := c.readIn(serviceQuotasRegion, &quota, "service-quotas", "get-service-quota", "--service-code", "organizations", "--quota-code", codes[0]); err != nil {
		return 0, err
	}

	v := quota.Quota.Value
	if v == nil || *v < 1 || *v != math.Trunc(*v) {
		return 0, xerrors.Wrap(errAWSResponse, "service-quotas get-service-quota: Value が1以上の整数ではありません")
	}

	return int(*v), nil
}

// checkAccountQuota は、3.8 手順2 の確認。組織のアカウントの数に、まだ無いメンバーアカウントの数を足したものが quota 以下である。
func checkAccountQuota(o options, c *client, w io.Writer, org organization) error {
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

	for _, m := range org.members {
		_, ok, err := accountNamed(accounts, accountName(o.project, m.env))
		if err != nil {
			return err
		}

		if !ok {
			adding = append(adding, m.env)
		}
	}

	total := len(accounts) + len(adding)
	r.check(total <= quota, fmt.Sprintf("在るアカウント %d と作るアカウント %d（%s）の計 %d が、上限 %d 以下である", len(accounts), len(adding), strings.Join(adding, "・"), total, quota))

	return r.result()
}

// checkMemberAccounts は、3.8 手順4.1 の確認。対応表の env ごとに、そのアカウントが期待する OU の直下に ACTIVE で在る。
// showIDs なら、取り込みの import block の id に使うアカウントの ID を出す。作成の途中で落ちたアカウントは
// OU へ移る前の Root の直下に残りうるので、ID は OU を問わず組織の中から名前で引く。
func checkMemberAccounts(o options, c *client, w io.Writer, org organization) error {
	r := newReport(w, "メンバーアカウント")

	all, err := listAccounts(c)
	if err != nil {
		return err
	}

	for _, m := range org.members {
		ou, err := resolveOU(c, m.ouPath)
		if err != nil {
			return err
		}

		under, err := listAccountsForParent(c, ou)
		if err != nil {
			return err
		}

		name := accountName(o.project, m.env)

		a, inOU, err := accountNamed(under, name)
		if err != nil {
			return err
		}

		where := strings.Join(m.ouPath, "/")

		switch {
		case inOU:
			r.check(a.State == "ACTIVE", fmt.Sprintf("%s が %s の直下に ACTIVE で在る（%s）", name, where, a.State))
		default:
			elsewhere, inOrg, err := accountNamed(all, name)
			if err != nil {
				return err
			}

			state := "組織に無い"
			if inOrg {
				a = elsewhere
				state = "組織には " + elsewhere.State + " で在るが、" + where + " の直下に無い"
			}

			r.ng(fmt.Sprintf("%s が %s の直下に ACTIVE で在る（%s）", name, where, state))

			if !inOrg {
				continue
			}
		}

		if o.showIDs {
			fmt.Fprintf(w, "   %s の ID: %s\n", m.env, a.ID)
		}
	}

	return r.result()
}

// checkStackInstances は、3.5 の昇格の後の確認。break-glass の StackSet が ACTIVE で在り、project の名前で始まる
// ACTIVE の StackSet のそれぞれが、対応表のすべてのメンバーアカウントに stack instance を持ち、どれも CURRENT である。
func checkStackInstances(o options, c *client, w io.Writer, org organization) error {
	r := newReport(w, "StackSet の stack instance")

	accounts, err := listAccounts(c)
	if err != nil {
		return err
	}

	var sets struct {
		Summaries *[]struct {
			StackSetName string `json:"StackSetName"`
		} `json:"Summaries"`
	}

	if err := c.read(&sets, "cloudformation", "list-stack-sets", "--status", "ACTIVE", "--call-as", "SELF"); err != nil {
		return err
	}

	if sets.Summaries == nil {
		return xerrors.Wrap(errAWSResponse, "cloudformation list-stack-sets: Summaries がありません")
	}

	var names []string

	for _, s := range *sets.Summaries {
		if s.StackSetName == "" {
			return xerrors.Wrap(errAWSResponse, "cloudformation list-stack-sets: StackSetName が空です")
		}

		if strings.HasPrefix(s.StackSetName, o.project+"-") {
			names = append(names, s.StackSetName)
		}
	}

	r.check(slices.Contains(names, org.breakGlass), org.breakGlass+" が ACTIVE で在る")

	for _, name := range names {
		if err := checkStackSetInstances(c, r, name, o.project, accounts, org.members); err != nil {
			return err
		}
	}

	return r.result()
}

// stackInstance は、list-stack-instances の Summaries の1件。
type stackInstance struct {
	Account             string `json:"Account"`
	Region              string `json:"Region"`
	Status              string `json:"Status"`
	StatusReason        string `json:"StatusReason"`
	StackInstanceStatus struct {
		DetailedStatus string `json:"DetailedStatus"`
	} `json:"StackInstanceStatus"`
}

// checkStackSetInstances は、1つの StackSet の stack instance を、メンバーアカウントのそれぞれが持ち、すべて CURRENT であるか確かめる。
func checkStackSetInstances(c *client, r *report, name, project string, accounts []orgAccount, members []memberAccount) error {
	var instances struct {
		Summaries *[]stackInstance `json:"Summaries"`
	}

	if err := c.read(&instances, "cloudformation", "list-stack-instances", "--stack-set-name", name, "--call-as", "SELF"); err != nil {
		return err
	}

	if instances.Summaries == nil {
		return xerrors.Wrap(errAWSResponse, "cloudformation list-stack-instances: Summaries がありません")
	}

	for _, m := range members {
		a, ok, err := accountNamed(accounts, accountName(project, m.env))
		if err != nil {
			return err
		}

		if !ok {
			r.ng(fmt.Sprintf("%s の stack instance を持つ %s が組織に在る", name, accountName(project, m.env)))

			continue
		}

		has := slices.ContainsFunc(*instances.Summaries, func(i stackInstance) bool {
			return i.Account == a.ID
		})
		r.check(has, fmt.Sprintf("%s が %s に stack instance を持つ", name, a.Name))
	}

	for _, i := range *instances.Summaries {
		what := fmt.Sprintf("%s の %s・%s が CURRENT である（%s", name, i.Account, i.Region, i.Status)
		if i.Status != "CURRENT" {
			what += fmt.Sprintf("、%s、%s", i.StackInstanceStatus.DetailedStatus, i.StatusReason)
		}

		r.check(i.Status == "CURRENT", what+"）")
	}

	return nil
}

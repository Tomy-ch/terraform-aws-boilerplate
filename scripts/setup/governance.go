package main

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// sameSet は、a と b が順序を除いて同じ並びか（重複も数える）。
func sameSet(a, b []string) bool {
	x, y := slices.Clone(a), slices.Clone(b)
	slices.Sort(x)
	slices.Sort(y)

	return slices.Equal(x, y)
}

func joinOrNone(s []string) string {
	if len(s) == 0 {
		return "なし"
	}

	return strings.Join(s, "・")
}

type orgRoot struct {
	ID          string `json:"Id"`
	PolicyTypes []struct {
		Type   string `json:"Type"`
		Status string `json:"Status"`
	} `json:"PolicyTypes"`
}

// readRoot は、組織のただ1つの Root。
func readRoot(c *client) (orgRoot, error) {
	var resp struct {
		Roots []orgRoot `json:"Roots"`
	}

	if err := c.read(&resp, "organizations", "list-roots"); err != nil {
		return orgRoot{}, err
	}

	if len(resp.Roots) != 1 || resp.Roots[0].ID == "" {
		return orgRoot{}, xerrors.Wrapf(errNoRoot, "%d 件", len(resp.Roots))
	}

	return resp.Roots[0], nil
}

// checkOrganization は、3.2 の確かめ: 組織の機能、Root の policy の種類、Root の直下の OU、IAM の組織の機能が、
// organization-baseline の宣言に一致する。
func checkOrganization(o options, c *client, w io.Writer) error {
	shape, err := readOrgShape(o.readFile, o.usecase)
	if err != nil {
		return err
	}

	var org struct {
		Organization struct {
			FeatureSet string `json:"FeatureSet"`
		} `json:"Organization"`
	}

	if err := c.read(&org, "organizations", "describe-organization"); err != nil {
		return err
	}

	root, err := readRoot(c)
	if err != nil {
		return err
	}

	var ous struct {
		OrganizationalUnits []struct {
			Name string `json:"Name"`
		} `json:"OrganizationalUnits"`
	}

	if err := c.read(&ous, "organizations", "list-organizational-units-for-parent", "--parent-id", root.ID); err != nil {
		return err
	}

	var features struct {
		EnabledFeatures []string `json:"EnabledFeatures"`
	}

	if err := c.read(&features, "iam", "list-organizations-features"); err != nil {
		return err
	}

	var enabled []string

	for _, p := range root.PolicyTypes {
		if p.Status == "ENABLED" {
			enabled = append(enabled, p.Type)
		}
	}

	var names []string
	for _, ou := range ous.OrganizationalUnits {
		names = append(names, ou.Name)
	}

	r := newReport(w, "組織と policy（3.2）")
	r.check(org.Organization.FeatureSet == shape.featureSet, fmt.Sprintf("FeatureSet が %s（今: %s）", shape.featureSet, org.Organization.FeatureSet))
	r.check(sameSet(enabled, shape.policyTypes), fmt.Sprintf("Root で ENABLED の policy の種類が %s（今: %s）", joinOrNone(shape.policyTypes), joinOrNone(enabled)))
	r.check(sameSet(names, shape.topOUs), fmt.Sprintf("Root の直下の OU が %s（今: %s）", joinOrNone(shape.topOUs), joinOrNone(names)))
	r.check(sameSet(features.EnabledFeatures, shape.features), fmt.Sprintf("IAM の組織の機能が %s（今: %s）", joinOrNone(shape.features), joinOrNone(features.EnabledFeatures)))

	return r.result()
}

// checkDelegatedAdmin は、3.9 の昇格の後の確かめ: 宣言のサービスの主体のそれぞれで、委任管理者がちょうど1つで、
// それが宣言の env のアカウント（ACTIVE）である。
func checkDelegatedAdmin(o options, c *client, w io.Writer) error {
	shape, err := readDelegatedShape(o.readFile, o.usecase)
	if err != nil {
		return err
	}

	accounts, err := listAccounts(c)
	if err != nil {
		return err
	}

	name := accountName(o.project, shape.env)

	a, found, err := accountNamed(accounts, name)
	if err != nil {
		return err
	}

	r := newReport(w, "委任管理者（3.9）")

	if !found || a.State != "ACTIVE" {
		r.ng(fmt.Sprintf("委任先のアカウント %s が ACTIVE で在る（今: %s）", name, map[bool]string{true: a.State, false: "無い"}[found]))

		return r.result()
	}

	for _, p := range shape.principals {
		var resp struct {
			DelegatedAdministrators []struct {
				ID string `json:"Id"`
			} `json:"DelegatedAdministrators"`
		}

		if err := c.read(&resp, "organizations", "list-delegated-administrators", "--service-principal", p); err != nil {
			return err
		}

		var ids []string
		for _, d := range resp.DelegatedAdministrators {
			ids = append(ids, d.ID)
		}

		r.check(len(ids) == 1 && ids[0] == a.ID, fmt.Sprintf("%s の委任管理者が %s（%s）だけ（今: %s）", p, name, a.ID, joinOrNone(ids)))
	}

	return r.result()
}

type ssoInstance struct {
	InstanceArn     string `json:"InstanceArn"`
	IdentityStoreID string `json:"IdentityStoreId"`
	OwnerAccountID  string `json:"OwnerAccountId"`
	Status          string `json:"Status"`
}

func readSSOInstances(c *client) ([]ssoInstance, error) {
	var resp struct {
		Instances []ssoInstance `json:"Instances"`
	}

	if err := c.read(&resp, "sso-admin", "list-instances"); err != nil {
		return nil, err
	}

	return resp.Instances, nil
}

func managementAccountID(c *client) (string, error) {
	var org struct {
		Organization struct {
			MasterAccountID string `json:"MasterAccountId"`
		} `json:"Organization"`
	}

	if err := c.read(&org, "organizations", "describe-organization"); err != nil {
		return "", err
	}

	if org.Organization.MasterAccountID == "" {
		return "", xerrors.Wrap(errAWSResponse, "組織の管理アカウントを読めません")
	}

	return org.Organization.MasterAccountID, nil
}

// checkIdentityCenter は、3.3 手順1 の確かめ: region に、組織の管理アカウントが持つ ACTIVE のインスタンスがただ1つ在る。
func checkIdentityCenter(o options, c *client, w io.Writer) error {
	instances, err := readSSOInstances(c)
	if err != nil {
		return err
	}

	master, err := managementAccountID(c)
	if err != nil {
		return err
	}

	r := newReport(w, "Identity Center（3.3 手順1）")

	if len(instances) != 1 {
		r.ng(fmt.Sprintf("%s に Identity Center のインスタンスがただ1つ在る（今: %d 件）", o.region, len(instances)))

		return r.result()
	}

	in := instances[0]
	r.check(in.Status == "ACTIVE", fmt.Sprintf("インスタンスが ACTIVE（今: %s）", in.Status))
	r.check(in.OwnerAccountID == master, fmt.Sprintf("インスタンスの持ち主が組織の管理アカウント %s（今: %s）。組織のインスタンスである", master, in.OwnerAccountID))

	return r.result()
}

// checkIdentityCenterUser は、3.3 手順4 の確かめ: user 名の user が在り、その user ID を出す。すべての user の名前が、
// 環境の接尾辞（-admin と、メンバーアカウントの env）で終わらない。
func checkIdentityCenterUser(o options, c *client, w io.Writer, org organization) error {
	instances, err := readSSOInstances(c)
	if err != nil {
		return err
	}

	if len(instances) != 1 || instances[0].IdentityStoreID == "" {
		return xerrors.Wrapf(errPrecondition, "%s に Identity Center のインスタンスがただ1つ在りません（%d 件）", o.region, len(instances))
	}

	store := instances[0].IdentityStoreID

	id, err := json.Marshal(map[string]any{"UniqueAttribute": map[string]string{"AttributePath": "userName", "AttributeValue": o.user}})
	if err != nil {
		return err
	}

	var user struct {
		UserID string `json:"UserId"`
	}

	found, err := c.readOptional("ResourceNotFoundException", &user, "identitystore", "get-user-id", "--identity-store-id", store, "--alternate-identifier", string(id))
	if err != nil {
		return err
	}

	var users struct {
		Users []struct {
			UserName string `json:"UserName"`
		} `json:"Users"`
	}

	if err := c.read(&users, "identitystore", "list-users", "--identity-store-id", store); err != nil {
		return err
	}

	r := newReport(w, "Identity Center の user（3.3 手順4）")
	r.check(found && user.UserID != "", fmt.Sprintf("user %s が在る（user ID: %s）", o.user, map[bool]string{true: user.UserID, false: "無い"}[found && user.UserID != ""]))

	suffixes := append([]string{"admin"}, envs(org.members)...)

	if len(users.Users) == 0 {
		r.ng("Identity Center に user が在る")
	}

	for _, u := range users.Users {
		hit := slices.IndexFunc(suffixes, func(s string) bool { return strings.HasSuffix(u.UserName, "-"+s) })
		r.check(hit < 0, fmt.Sprintf("user %s の名前が環境の接尾辞（%s）で終わらない", u.UserName, joinOrNone(prefixed(suffixes))))
	}

	return r.result()
}

func prefixed(s []string) []string {
	out := make([]string, len(s))
	for i, v := range s {
		out[i] = "-" + v
	}

	return out
}

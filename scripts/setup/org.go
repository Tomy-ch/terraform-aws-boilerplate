package main

import (
	"fmt"
	"io"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

var (
	errNoRoot = xerrors.New("組織の Root がただ1つに決まりません")
	// errOUNotFound は、対応表の OU が組織に無いか、同じ名前の OU が2つ在ること。
	errOUNotFound = xerrors.New("OU がただ1つに決まりません")
	// errAccountName は、同じ名前のアカウントが組織に2つ以上在り、どれが env のアカウントか決まらないこと。
	errAccountName = xerrors.New("同じ名前のアカウントが組織に2つ以上在ります")
)

// orgAccount は、Organizations のアカウントの1件。Status は廃止されたので State を読む。
type orgAccount struct {
	ID    string `json:"Id"`
	Name  string `json:"Name"`
	Email string `json:"Email"`
	State string `json:"State"`
}

// listAccounts は、組織のすべてのアカウント。組織には管理アカウントが必ず在るので、0件は読めない応答として扱う。
func listAccounts(c *client) ([]orgAccount, error) {
	var resp struct {
		Accounts []orgAccount `json:"Accounts"`
	}

	if err := c.read(&resp, "organizations", "list-accounts"); err != nil {
		return nil, err
	}

	if len(resp.Accounts) == 0 {
		return nil, xerrors.Wrap(errAWSResponse, "organizations list-accounts: アカウントが0件です")
	}

	if err := validAccounts(resp.Accounts); err != nil {
		return nil, err
	}

	return resp.Accounts, nil
}

// validAccounts は、ID・名前・State の欠けたアカウントを読めない応答として扱う。
func validAccounts(accounts []orgAccount) error {
	for _, a := range accounts {
		if a.ID == "" || a.Name == "" || a.State == "" {
			return xerrors.Wrap(errAWSResponse, "Id・Name・State の欠けたアカウントがあります")
		}
	}

	return nil
}

// accountNamed は、accounts のうち name のアカウント。無ければ false、2つ以上在れば errAccountName。
// 閉鎖した後に同じ名前で作り直したアカウントがあれば、閉鎖済みではないものを選ぶ。閉鎖済みのアカウントは
// 閉鎖から約90日、組織の一覧に残る。
func accountNamed(accounts []orgAccount, name string) (orgAccount, bool, error) {
	var open, closed []orgAccount

	for _, a := range accounts {
		switch {
		case a.Name != name:
		case isClosed(a):
			closed = append(closed, a)
		default:
			open = append(open, a)
		}
	}

	switch {
	case len(open) == 1:
		return open[0], true, nil
	case len(open) > 1, len(closed) > 1:
		return orgAccount{}, false, xerrors.Wrap(errAccountName, name)
	case len(closed) == 1:
		return closed[0], true, nil
	default:
		return orgAccount{}, false, nil
	}
}

// isClosed は、アカウントが閉鎖の途中か閉鎖済みか（SUSPENDED・PENDING_CLOSURE・CLOSED）。
func isClosed(a orgAccount) bool {
	switch a.State {
	case "SUSPENDED", "PENDING_CLOSURE", "CLOSED":
		return true
	default:
		return false
	}
}

// accountName は、env のメンバーアカウントの名前（accounts.tf の name と同じ形）。
func accountName(project, env string) string {
	return project + "-" + env
}

// resolveOU は、Root から path の OU を名前でたどり、その ID を返す。
func resolveOU(c *client, path []string) (string, error) {
	var roots struct {
		Roots []struct {
			ID string `json:"Id"`
		} `json:"Roots"`
	}

	if err := c.read(&roots, "organizations", "list-roots"); err != nil {
		return "", err
	}

	if len(roots.Roots) != 1 || roots.Roots[0].ID == "" {
		return "", errNoRoot
	}

	parent := roots.Roots[0].ID

	for _, name := range path {
		var ous struct {
			OrganizationalUnits []struct {
				ID   string `json:"Id"`
				Name string `json:"Name"`
			} `json:"OrganizationalUnits"`
		}

		if err := c.read(&ous, "organizations", "list-organizational-units-for-parent", "--parent-id", parent); err != nil {
			return "", err
		}

		var ids []string

		for _, ou := range ous.OrganizationalUnits {
			if ou.Name == name {
				ids = append(ids, ou.ID)
			}
		}

		if len(ids) != 1 {
			return "", xerrors.Wrap(errOUNotFound, name)
		}

		if ids[0] == "" {
			return "", xerrors.Wrap(errAWSResponse, "OU "+name+" の Id が空です")
		}

		parent = ids[0]
	}

	return parent, nil
}

func listAccountsForParent(c *client, parent string) ([]orgAccount, error) {
	var resp struct {
		Accounts []orgAccount `json:"Accounts"`
	}

	if err := c.read(&resp, "organizations", "list-accounts-for-parent", "--parent-id", parent); err != nil {
		return nil, err
	}

	if err := validAccounts(resp.Accounts); err != nil {
		return nil, err
	}

	return resp.Accounts, nil
}

// requireManagementAccount は、資格情報のアカウントが組織の管理アカウントであることを確かめ、書き込む先を出す。
// 別のアカウントや別の組織の profile を渡したとき、書き込みの前に止める。
func requireManagementAccount(c *client, profile string, w io.Writer) error {
	var caller struct {
		Account string `json:"Account"`
	}

	if err := c.read(&caller, "sts", "get-caller-identity"); err != nil {
		return err
	}

	var org struct {
		Organization struct {
			MasterAccountID string `json:"MasterAccountId"`
		} `json:"Organization"`
	}

	if err := c.read(&org, "organizations", "describe-organization"); err != nil {
		return err
	}

	if caller.Account == "" || org.Organization.MasterAccountID == "" {
		return xerrors.Wrap(errAWSResponse, "資格情報のアカウントか、組織の管理アカウントを読めません")
	}

	if caller.Account != org.Organization.MasterAccountID {
		return xerrors.Wrap(errPrecondition, "profile "+profile+" の資格情報は組織の管理アカウントのものではありません")
	}

	fmt.Fprintf(w, "# 書き込む先: profile %s、region %s、組織の管理アカウント\n", profile, c.region)

	return nil
}

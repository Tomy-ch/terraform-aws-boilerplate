package main

import (
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

var (
	errNoRoot = xerrors.New("組織の Root がただ1つに決まりません")
	// errOUNotFound は、対応表の OU が組織に無いか、同じ名前の OU が2つ在ること。
	errOUNotFound = xerrors.New("OU がただ1つに決まりません")
)

// orgAccount は、Organizations のアカウントの1件。Status は廃止されたので State を読む。
type orgAccount struct {
	ID    string `json:"Id"`
	Name  string `json:"Name"`
	Email string `json:"Email"`
	State string `json:"State"`
}

func listAccounts(c *client) ([]orgAccount, error) {
	var resp struct {
		Accounts []orgAccount `json:"Accounts"`
	}

	if err := c.read(&resp, "organizations", "list-accounts"); err != nil {
		return nil, err
	}

	return resp.Accounts, nil
}

// accountNamed は、accounts のうち name のアカウント。無ければ false。
func accountNamed(accounts []orgAccount, name string) (orgAccount, bool) {
	for _, a := range accounts {
		if a.Name == name {
			return a, true
		}
	}

	return orgAccount{}, false
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

	if len(roots.Roots) != 1 {
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

	return resp.Accounts, nil
}

package main

import (
	"slices"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const (
	memberAccountsLocal = "member_accounts"
	ouResourceType      = "aws_organizations_organizational_unit"
)

var (
	errHCL = xerrors.New("env と OU の対応表を HCL として読めません")
	// errNoMemberAccounts は、対応表（locals の member_accounts）が無いか、行が0件なこと。
	errNoMemberAccounts = xerrors.New("locals の member_accounts が無いか、行がありません")
	// errMemberAccountsShape は、対応表の形を解釈できないこと（key がリテラルでない、値が OU の参照でない、key の重複）。
	errMemberAccountsShape = xerrors.New("locals の member_accounts を解釈できません")

	// ouParents は、OU の resource の名前ごとの、Root からその OU の親までの OU の名前の並び。
	// usecases/organization-baseline/internal/organization/main.tf の OU の階層と同じものを持つ。
	ouParents = map[string][]string{
		"top":       nil,
		"workloads": {"Workloads"},
	}
)

// memberAccount は、対応表の1行。ouPath は Root からその OU までの OU の名前の並び。
type memberAccount struct {
	env    string
	ouPath []string
}

// readMemberAccounts は、src の locals の member_accounts を読み、env の昇順で返す。
func readMemberAccounts(src []byte, filename string) ([]memberAccount, error) {
	file, diags := hclsyntax.ParseConfig(src, filename, hcl.InitialPos)
	if diags.HasErrors() {
		return nil, xerrors.Wrap(errHCL, diags.Error())
	}

	body, ok := file.Body.(*hclsyntax.Body)
	if !ok {
		return nil, xerrors.Wrap(errHCL, filename)
	}

	var found *hclsyntax.Attribute

	for _, b := range body.Blocks {
		if b.Type != "locals" {
			continue
		}

		if a, ok := b.Body.Attributes[memberAccountsLocal]; ok {
			if found != nil {
				return nil, xerrors.Wrap(errMemberAccountsShape, "member_accounts が2つの locals に在ります")
			}

			found = a
		}
	}

	if found == nil {
		return nil, xerrors.Wrap(errNoMemberAccounts, filename)
	}

	obj, ok := found.Expr.(*hclsyntax.ObjectConsExpr)
	if !ok {
		return nil, xerrors.Wrap(errMemberAccountsShape, "object の構文ではありません")
	}

	if len(obj.Items) == 0 {
		return nil, xerrors.Wrap(errNoMemberAccounts, filename)
	}

	accounts := make([]memberAccount, 0, len(obj.Items))

	for _, item := range obj.Items {
		a, err := readMemberAccount(item)
		if err != nil {
			return nil, err
		}

		if slices.ContainsFunc(accounts, func(x memberAccount) bool { return x.env == a.env }) {
			return nil, xerrors.Wrap(errMemberAccountsShape, "env "+a.env+" が重複しています")
		}

		accounts = append(accounts, a)
	}

	slices.SortFunc(accounts, func(a, b memberAccount) int { return strings.Compare(a.env, b.env) })

	return accounts, nil
}

// readMemberAccount は、`<env> = aws_organizations_organizational_unit.<name>["<OU>"].id` の1行を読む。
func readMemberAccount(item hclsyntax.ObjectConsItem) (memberAccount, error) {
	key, diags := item.KeyExpr.Value(nil)
	if diags.HasErrors() || key.Type() != cty.String || !key.IsKnown() || key.IsNull() || key.AsString() == "" {
		return memberAccount{}, xerrors.Wrap(errMemberAccountsShape, "key がリテラルの文字列ではありません")
	}

	env := key.AsString()

	st, ok := item.ValueExpr.(*hclsyntax.ScopeTraversalExpr)
	if !ok {
		return memberAccount{}, xerrors.Wrap(errMemberAccountsShape, env+" の値が OU の参照ではありません")
	}

	tr := st.Traversal
	if len(tr) != 4 || tr.RootName() != ouResourceType {
		return memberAccount{}, xerrors.Wrap(errMemberAccountsShape, env+" の値が OU の参照ではありません")
	}

	name, ok1 := tr[1].(hcl.TraverseAttr)
	index, ok2 := tr[2].(hcl.TraverseIndex)
	id, ok3 := tr[3].(hcl.TraverseAttr)

	if !ok1 || !ok2 || !ok3 || id.Name != "id" || index.Key.Type() != cty.String {
		return memberAccount{}, xerrors.Wrap(errMemberAccountsShape, env+" の値が OU の参照ではありません")
	}

	parents, ok := ouParents[name.Name]
	if !ok {
		return memberAccount{}, xerrors.Wrap(errMemberAccountsShape, env+" の OU の resource "+name.Name+" の階層を知りません")
	}

	return memberAccount{env: env, ouPath: append(slices.Clone(parents), index.Key.AsString())}, nil
}

func envs(accounts []memberAccount) []string {
	out := make([]string, len(accounts))
	for i, a := range accounts {
		out[i] = a.env
	}

	return out
}

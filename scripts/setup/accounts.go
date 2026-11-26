package main

import (
	"path"
	"regexp"
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
	// usecaseFile・accountsFile・ouFile は、-usecase のディレクトリの中で、break-glass の role の名前を持つファイル、
	// 対応表とメールの形を持つファイル、OU の階層を持つファイル。
	usecaseFile         = "main.tf"
	accountsFile        = "internal/organization/accounts.tf"
	ouFile              = "internal/organization/main.tf"
	breakGlassNameLocal = "break_glass_role_name"
)

var (
	errHCL = xerrors.New("ユースケースの Terraform を HCL として読めません")
	// errNoMemberAccounts は、対応表（locals の member_accounts）が無いか、行が0件なこと。
	errNoMemberAccounts = xerrors.New("locals の member_accounts が無いか、行がありません")
	// errMemberAccountsShape は、対応表の形を解釈できないこと（key がリテラルでない、値が OU の参照でない、key の重複）。
	errMemberAccountsShape = xerrors.New("locals の member_accounts を解釈できません")
	// errOUShape は、OU の resource の階層（for_each と parent_id）を解釈できないこと。
	errOUShape = xerrors.New("OU の resource の階層を解釈できません")
	// errBreakGlassName は、usecase の main.tf の locals から break-glass の role の名前を決められないこと。
	errBreakGlassName = xerrors.New("break-glass の role の名前（locals の break_glass_role_name）を決められません")
	// errEmailRegex は、accounts.tf の precondition からメールの形の正規表現をただ1つに決められないこと。
	errEmailRegex = xerrors.New("accounts.tf からメールの形の正規表現をただ1つに決められません")
)

// memberAccount は、対応表の1行。ouPath は Root からその OU までの OU の名前の並び。
type memberAccount struct {
	env    string
	ouPath []string
}

// organization は、organization-baseline の Terraform から読む、手順が守るべき形。対応表の行、OU の階層、
// メールの形、break-glass の StackSet の名前は Terraform が正本で、ここは写しを持たずに毎回読む（ADR-0702 決定12）。
type organization struct {
	members      []memberAccount
	emailPattern *regexp.Regexp
	breakGlass   string
}

// readOrganization は、usecase のディレクトリ dir の Terraform を読む。project は break-glass の名前を組むのに使う。
func readOrganization(readFile func(string) ([]byte, error), dir, project string) (organization, error) {
	usecase, err := parseFile(readFile, path.Join(dir, usecaseFile))
	if err != nil {
		return organization{}, err
	}

	breakGlass, err := readBreakGlassName(usecase, project)
	if err != nil {
		return organization{}, err
	}

	accounts, err := parseFile(readFile, path.Join(dir, accountsFile))
	if err != nil {
		return organization{}, err
	}

	ous, err := parseFile(readFile, path.Join(dir, ouFile))
	if err != nil {
		return organization{}, err
	}

	hierarchy, err := readOUHierarchy(ous)
	if err != nil {
		return organization{}, err
	}

	members, err := readMemberAccounts(accounts, hierarchy)
	if err != nil {
		return organization{}, err
	}

	pattern, err := readEmailPattern(accounts)
	if err != nil {
		return organization{}, err
	}

	return organization{members: members, emailPattern: pattern, breakGlass: breakGlass}, nil
}

// readBreakGlassName は、locals の break_glass_role_name を、var.project に project を入れて評価する。
func readBreakGlassName(body *hclsyntax.Body, project string) (string, error) {
	var found []*hclsyntax.Attribute

	for _, b := range body.Blocks {
		if b.Type == "locals" {
			if a, ok := b.Body.Attributes[breakGlassNameLocal]; ok {
				found = append(found, a)
			}
		}
	}

	if len(found) != 1 {
		return "", xerrors.Wrapf(errBreakGlassName, "%d 件", len(found))
	}

	ctx := &hcl.EvalContext{Variables: map[string]cty.Value{
		"var": cty.ObjectVal(map[string]cty.Value{"project": cty.StringVal(project)}),
	}}

	v, diags := found[0].Expr.Value(ctx)
	if diags.HasErrors() || v.Type() != cty.String || !v.IsKnown() || v.IsNull() || v.AsString() == "" {
		return "", xerrors.Wrap(errBreakGlassName, "var.project だけを使う文字列の式ではありません")
	}

	return v.AsString(), nil
}

func parseFile(readFile func(string) ([]byte, error), name string) (*hclsyntax.Body, error) {
	src, err := readFile(name)
	if err != nil {
		return nil, xerrors.Join(errHCL, err)
	}

	file, diags := hclsyntax.ParseConfig(src, name, hcl.InitialPos)
	if diags.HasErrors() {
		return nil, xerrors.Wrap(errHCL, diags.Error())
	}

	body, ok := file.Body.(*hclsyntax.Body)
	if !ok {
		return nil, xerrors.Wrap(errHCL, name)
	}

	return body, nil
}

// ouResource は、OU の resource の1つ。names は for_each の OU の名前、parent はその親の参照。
type ouResource struct {
	names  []string
	parent hclsyntax.Expression
}

// readOUHierarchy は、OU の resource の名前と OU の名前から、Root からその OU までの OU の名前の並びへの写像を作る。
// parent_id は local.root_id（Root の直下）か、別の OU の resource の参照でなければならない。
func readOUHierarchy(body *hclsyntax.Body) (map[string]map[string][]string, error) {
	resources := map[string]ouResource{}

	for _, b := range body.Blocks {
		if b.Type != "resource" || len(b.Labels) != 2 || b.Labels[0] != ouResourceType {
			continue
		}

		names, err := literalSet(b.Body.Attributes["for_each"])
		if err != nil {
			return nil, xerrors.Wrap(errOUShape, b.Labels[1]+": "+err.Error())
		}

		parent, ok := b.Body.Attributes["parent_id"]
		if !ok {
			return nil, xerrors.Wrap(errOUShape, b.Labels[1]+" に parent_id がありません")
		}

		resources[b.Labels[1]] = ouResource{names: names, parent: parent.Expr}
	}

	if len(resources) == 0 {
		return nil, xerrors.Wrap(errOUShape, ouResourceType+" がありません")
	}

	hierarchy := map[string]map[string][]string{}

	var resolve func(name string, seen []string) ([]string, error)

	resolve = func(name string, seen []string) ([]string, error) {
		if slices.Contains(seen, name) {
			return nil, xerrors.Wrap(errOUShape, "parent_id が循環しています: "+strings.Join(append(seen, name), " → "))
		}

		r, ok := resources[name]
		if !ok {
			return nil, xerrors.Wrap(errOUShape, ouResourceType+"."+name+" がありません")
		}

		st, ok := r.parent.(*hclsyntax.ScopeTraversalExpr)
		if !ok {
			return nil, xerrors.Wrap(errOUShape, name+" の parent_id が参照ではありません")
		}

		if isRootRef(st.Traversal) {
			return nil, nil
		}

		parentRes, ou, err := ouRef(st.Traversal)
		if err != nil {
			return nil, xerrors.Wrap(errOUShape, name+" の parent_id: "+err.Error())
		}

		above, err := resolve(parentRes, append(seen, name))
		if err != nil {
			return nil, err
		}

		if !slices.Contains(resources[parentRes].names, ou) {
			return nil, xerrors.Wrap(errOUShape, name+" の親 "+ou+" が "+parentRes+" の for_each にありません")
		}

		return append(slices.Clone(above), ou), nil
	}

	for name, r := range resources {
		parents, err := resolve(name, nil)
		if err != nil {
			return nil, err
		}

		hierarchy[name] = map[string][]string{}
		for _, ou := range r.names {
			hierarchy[name][ou] = append(slices.Clone(parents), ou)
		}
	}

	return hierarchy, nil
}

func isRootRef(tr hcl.Traversal) bool {
	if len(tr) != 2 || tr.RootName() != "local" {
		return false
	}

	a, ok := tr[1].(hcl.TraverseAttr)

	return ok && a.Name == "root_id"
}

// ouRef は、`aws_organizations_organizational_unit.<resource>["<OU>"].id` を読む。
func ouRef(tr hcl.Traversal) (string, string, error) {
	if len(tr) != 4 || tr.RootName() != ouResourceType {
		return "", "", errNotOURef
	}

	name, ok1 := tr[1].(hcl.TraverseAttr)
	index, ok2 := tr[2].(hcl.TraverseIndex)
	id, ok3 := tr[3].(hcl.TraverseAttr)

	if !ok1 || !ok2 || !ok3 || id.Name != "id" || index.Key.Type() != cty.String || !index.Key.IsKnown() || index.Key.IsNull() || index.Key.AsString() == "" {
		return "", "", errNotOURef
	}

	return name.Name, index.Key.AsString(), nil
}

var errNotOURef = xerrors.New("OU の resource の参照（aws_organizations_organizational_unit.<name>[\"<OU>\"].id）ではありません")

// literalSet は、`toset(["a", "b"])` の文字列を読む。
func literalSet(attr *hclsyntax.Attribute) ([]string, error) {
	if attr == nil {
		return nil, errNotLiteralSet
	}

	call, ok := attr.Expr.(*hclsyntax.FunctionCallExpr)
	if !ok || call.Name != "toset" || len(call.Args) != 1 {
		return nil, errNotLiteralSet
	}

	tuple, ok := call.Args[0].(*hclsyntax.TupleConsExpr)
	if !ok || len(tuple.Exprs) == 0 {
		return nil, errNotLiteralSet
	}

	names := make([]string, 0, len(tuple.Exprs))

	for _, e := range tuple.Exprs {
		v, diags := e.Value(nil)
		if diags.HasErrors() || v.Type() != cty.String || !v.IsKnown() || v.IsNull() || v.AsString() == "" || slices.Contains(names, v.AsString()) {
			return nil, errNotLiteralSet
		}

		names = append(names, v.AsString())
	}

	return names, nil
}

var errNotLiteralSet = xerrors.New("for_each が、重複の無い空でない文字列の toset([...]) ではありません")

// readMemberAccounts は、body の locals の member_accounts を読み、env の昇順で返す。
func readMemberAccounts(body *hclsyntax.Body, hierarchy map[string]map[string][]string) ([]memberAccount, error) {
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
		return nil, errNoMemberAccounts
	}

	obj, ok := found.Expr.(*hclsyntax.ObjectConsExpr)
	if !ok {
		return nil, xerrors.Wrap(errMemberAccountsShape, "object の構文ではありません")
	}

	if len(obj.Items) == 0 {
		return nil, errNoMemberAccounts
	}

	accounts := make([]memberAccount, 0, len(obj.Items))

	for _, item := range obj.Items {
		a, err := readMemberAccount(item, hierarchy)
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
func readMemberAccount(item hclsyntax.ObjectConsItem, hierarchy map[string]map[string][]string) (memberAccount, error) {
	key, diags := item.KeyExpr.Value(nil)
	if diags.HasErrors() || key.Type() != cty.String || !key.IsKnown() || key.IsNull() || key.AsString() == "" {
		return memberAccount{}, xerrors.Wrap(errMemberAccountsShape, "key がリテラルの文字列ではありません")
	}

	env := key.AsString()

	st, ok := item.ValueExpr.(*hclsyntax.ScopeTraversalExpr)
	if !ok {
		return memberAccount{}, xerrors.Wrap(errMemberAccountsShape, env+" の値が OU の参照ではありません")
	}

	res, ou, err := ouRef(st.Traversal)
	if err != nil {
		return memberAccount{}, xerrors.Wrap(errMemberAccountsShape, env+": "+err.Error())
	}

	p, ok := hierarchy[res][ou]
	if !ok {
		return memberAccount{}, xerrors.Wrap(errMemberAccountsShape, env+" の OU "+res+"[\""+ou+"\"] が main.tf にありません")
	}

	return memberAccount{env: env, ouPath: p}, nil
}

func envs(accounts []memberAccount) []string {
	out := make([]string, len(accounts))
	for i, a := range accounts {
		out[i] = a.env
	}

	return out
}

// readEmailPattern は、body の中のただ1つの regex("...") の正規表現（accounts.tf の precondition がメールに課す形）を返す。
func readEmailPattern(body *hclsyntax.Body) (*regexp.Regexp, error) {
	var patterns []string

	diags := hclsyntax.VisitAll(body, func(n hclsyntax.Node) hcl.Diagnostics {
		call, ok := n.(*hclsyntax.FunctionCallExpr)
		if !ok || call.Name != "regex" {
			return nil
		}

		if len(call.Args) != 2 {
			patterns = append(patterns, "")

			return nil
		}

		v, d := call.Args[0].Value(nil)
		if d.HasErrors() || v.Type() != cty.String || !v.IsKnown() || v.IsNull() {
			patterns = append(patterns, "")

			return nil
		}

		patterns = append(patterns, v.AsString())

		return nil
	})
	if diags.HasErrors() {
		return nil, xerrors.Wrap(errEmailRegex, diags.Error())
	}

	if len(patterns) != 1 || patterns[0] == "" {
		return nil, xerrors.Wrapf(errEmailRegex, "regex のリテラルが %d 件", len(patterns))
	}

	re, err := regexp.Compile(patterns[0])
	if err != nil {
		return nil, xerrors.Join(errEmailRegex, err)
	}

	return re, nil
}

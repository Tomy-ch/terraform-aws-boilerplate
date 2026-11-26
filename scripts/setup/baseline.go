package main

import (
	"maps"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/function"
	"github.com/zclconf/go-cty/cty/function/stdlib"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const (
	breakGlassFile    = "internal/break-glass/main.tf"
	delegatedFile     = "internal/organization/delegated-admin.tf"
	outputsFile       = "internal/organization/outputs.tf"
	breakGlassPathKey = "break_glass_path"
	userNamesKey      = "user_names"
	delegatedResource = "aws_organizations_delegated_administrator"
	accountResource   = "aws_organizations_account"
)

var (
	// errDeclShape は、手順が守るべき形を Terraform の宣言から決められないこと（resource が無い・2つ在る、値がリテラルでない）。
	errDeclShape = xerrors.New("ユースケースの Terraform の宣言を解釈できません")

	// passwordPolicyFields は、aws_iam_account_password_policy の argument と、get-account-password-policy の応答の field の対応。
	// 値は Terraform から読む。対応に無い argument と、対応に在って宣言に無い argument は確かめられないので、
	// 宣言の読み取りをエラーにする。
	passwordPolicyFields = map[string]string{
		"minimum_password_length":        "MinimumPasswordLength",
		"password_reuse_prevention":      "PasswordReusePrevention",
		"require_lowercase_characters":   "RequireLowercaseCharacters",
		"require_uppercase_characters":   "RequireUppercaseCharacters",
		"require_numbers":                "RequireNumbers",
		"require_symbols":                "RequireSymbols",
		"allow_users_to_change_password": "AllowUsersToChangePassword",
		"max_password_age":               "MaxPasswordAge",
		"hard_expiry":                    "HardExpiry",
	}
)

// orgShape は、組織の宣言（internal/organization/main.tf）から読む、組織が持つべき形。
type orgShape struct {
	featureSet  string
	policyTypes []string
	features    []string
	topOUs      []string
}

// breakGlassShape は、break-glass の宣言から読む、user の名前、path、role の名前、password policy の値
// （get-account-password-policy の field から、JSON の数か真偽の字面への写像）。
type breakGlassShape struct {
	users  []string
	path   string
	role   string
	policy map[string]string
}

// delegatedShape は、委任管理者の宣言から読む、委任先の env とサービスの主体。
type delegatedShape struct {
	env        string
	principals []string
}

// singleResource は、body の中の type の resource がただ1つであることを求めて返す。
func singleResource(body *hclsyntax.Body, typ string) (*hclsyntax.Block, error) {
	var found []*hclsyntax.Block

	for _, b := range body.Blocks {
		if b.Type == "resource" && len(b.Labels) == 2 && b.Labels[0] == typ {
			found = append(found, b)
		}
	}

	if len(found) != 1 {
		return nil, xerrors.Wrapf(errDeclShape, "resource %s が %d 件", typ, len(found))
	}

	return found[0], nil
}

// singleLocal は、body の locals の name がただ1つであることを求めて返す。
func singleLocal(body *hclsyntax.Body, name string) (*hclsyntax.Attribute, error) {
	var found []*hclsyntax.Attribute

	for _, b := range body.Blocks {
		if b.Type == "locals" {
			if a, ok := b.Body.Attributes[name]; ok {
				found = append(found, a)
			}
		}
	}

	if len(found) != 1 {
		return nil, xerrors.Wrapf(errDeclShape, "locals の %s が %d 件", name, len(found))
	}

	return found[0], nil
}

// literal は、変数も関数も使わない式の値。
func literal(attr *hclsyntax.Attribute) (cty.Value, error) {
	if attr == nil {
		return cty.NilVal, xerrors.Wrap(errDeclShape, "argument がありません")
	}

	v, diags := attr.Expr.Value(nil)
	if diags.HasErrors() || !v.IsWhollyKnown() || v.IsNull() {
		return cty.NilVal, xerrors.Wrap(errDeclShape, attr.Name+" がリテラルではありません")
	}

	return v, nil
}

func literalString(attr *hclsyntax.Attribute) (string, error) {
	v, err := literal(attr)
	if err != nil {
		return "", err
	}

	if v.Type() != cty.String || v.AsString() == "" {
		return "", xerrors.Wrap(errDeclShape, attr.Name+" が空でない文字列ではありません")
	}

	return v.AsString(), nil
}

// literalStrings は、空でない文字列の、重複の無い空でないリストの字面を読む。
func literalStrings(attr *hclsyntax.Attribute) ([]string, error) {
	v, err := literal(attr)
	if err != nil {
		return nil, err
	}

	if !v.Type().IsTupleType() || v.LengthInt() == 0 {
		return nil, xerrors.Wrap(errDeclShape, attr.Name+" が空でないリストではありません")
	}

	var out []string

	for it := v.ElementIterator(); it.Next(); {
		_, e := it.Element()
		if e.Type() != cty.String || e.AsString() == "" || slices.Contains(out, e.AsString()) {
			return nil, xerrors.Wrap(errDeclShape, attr.Name+" が、重複の無い空でない文字列のリストではありません")
		}

		out = append(out, e.AsString())
	}

	return out, nil
}

// readOrgShape は、組織の宣言を読む。Root の直下の OU は、OU の resource の階層（readOUHierarchy）から取る。
func readOrgShape(readFile func(string) ([]byte, error), dir string) (orgShape, error) {
	body, err := parseFile(readFile, path.Join(dir, ouFile))
	if err != nil {
		return orgShape{}, err
	}

	org, err := singleResource(body, "aws_organizations_organization")
	if err != nil {
		return orgShape{}, err
	}

	featureSet, err := literalString(org.Body.Attributes["feature_set"])
	if err != nil {
		return orgShape{}, err
	}

	policyTypes, err := literalStrings(org.Body.Attributes["enabled_policy_types"])
	if err != nil {
		return orgShape{}, err
	}

	features, err := singleResource(body, "aws_iam_organizations_features")
	if err != nil {
		return orgShape{}, err
	}

	enabled, err := literalStrings(features.Body.Attributes["enabled_features"])
	if err != nil {
		return orgShape{}, err
	}

	hierarchy, err := readOUHierarchy(body)
	if err != nil {
		return orgShape{}, err
	}

	var top []string

	for _, ous := range hierarchy {
		for ou, p := range ous {
			if len(p) == 1 {
				top = append(top, ou)
			}
		}
	}

	if len(top) == 0 {
		return orgShape{}, xerrors.Wrap(errOUShape, "Root の直下の OU がありません")
	}

	slices.Sort(top)

	return orgShape{featureSet: featureSet, policyTypes: policyTypes, features: enabled, topOUs: top}, nil
}

// readBreakGlassShape は、break-glass の宣言を読む。project は role の名前を組むのに使う。
func readBreakGlassShape(readFile func(string) ([]byte, error), dir, project string) (breakGlassShape, error) {
	usecase, err := parseFile(readFile, path.Join(dir, usecaseFile))
	if err != nil {
		return breakGlassShape{}, err
	}

	role, err := readBreakGlassName(usecase, project)
	if err != nil {
		return breakGlassShape{}, err
	}

	pathAttr, err := singleLocal(usecase, breakGlassPathKey)
	if err != nil {
		return breakGlassShape{}, err
	}

	p, err := literalString(pathAttr)
	if err != nil {
		return breakGlassShape{}, err
	}

	if !strings.HasPrefix(p, "/") || !strings.HasSuffix(p, "/") {
		return breakGlassShape{}, xerrors.Wrap(errDeclShape, breakGlassPathKey+" が / で始まり / で終わりません")
	}

	body, err := parseFile(readFile, path.Join(dir, breakGlassFile))
	if err != nil {
		return breakGlassShape{}, err
	}

	usersAttr, err := singleLocal(body, userNamesKey)
	if err != nil {
		return breakGlassShape{}, err
	}

	users, err := literalSet(usersAttr)
	if err != nil {
		return breakGlassShape{}, xerrors.Join(errDeclShape, err)
	}

	if err := requireForEachLocal(body, "aws_iam_user", userNamesKey); err != nil {
		return breakGlassShape{}, err
	}

	slices.Sort(users)

	policy, err := readPasswordPolicy(body)
	if err != nil {
		return breakGlassShape{}, err
	}

	return breakGlassShape{users: users, path: p, role: role, policy: policy}, nil
}

// requireForEachLocal は、type のただ1つの resource の for_each が local.<name> であることを求める。
// 読んだ local が resource の作る集合であることを、宣言の側で確かめる。
func requireForEachLocal(body *hclsyntax.Body, typ, name string) error {
	b, err := singleResource(body, typ)
	if err != nil {
		return err
	}

	attr, ok := b.Body.Attributes["for_each"]
	if !ok {
		return xerrors.Wrap(errDeclShape, typ+" に for_each がありません")
	}

	st, ok := attr.Expr.(*hclsyntax.ScopeTraversalExpr)
	if !ok || len(st.Traversal) != 2 || st.Traversal.RootName() != "local" {
		return xerrors.Wrap(errDeclShape, typ+" の for_each が local."+name+" ではありません")
	}

	if a, ok := st.Traversal[1].(hcl.TraverseAttr); !ok || a.Name != name {
		return xerrors.Wrap(errDeclShape, typ+" の for_each が local."+name+" ではありません")
	}

	return nil
}

// readPasswordPolicy は、aws_iam_account_password_policy の argument を、応答の field と JSON の字面の写像にする。
func readPasswordPolicy(body *hclsyntax.Body) (map[string]string, error) {
	b, err := singleResource(body, "aws_iam_account_password_policy")
	if err != nil {
		return nil, err
	}

	if len(b.Body.Attributes) == 0 {
		return nil, xerrors.Wrap(errDeclShape, "aws_iam_account_password_policy に argument がありません")
	}

	policy := map[string]string{}

	for name, attr := range b.Body.Attributes {
		field, ok := passwordPolicyFields[name]
		if !ok {
			return nil, xerrors.Wrap(errDeclShape, "password policy の argument "+name+" を確かめられません")
		}

		v, err := literal(attr)
		if err != nil {
			return nil, err
		}

		switch v.Type() {
		case cty.Bool:
			policy[field] = strconv.FormatBool(v.True())
		case cty.Number:
			if !v.AsBigFloat().IsInt() {
				return nil, xerrors.Wrap(errDeclShape, name+" が整数ではありません")
			}

			policy[field] = v.AsBigFloat().Text('f', 0)
		default:
			return nil, xerrors.Wrap(errDeclShape, name+" が数でも真偽でもありません")
		}
	}

	var missing []string

	for _, name := range slices.Sorted(maps.Keys(passwordPolicyFields)) {
		if _, ok := policy[passwordPolicyFields[name]]; !ok {
			missing = append(missing, name)
		}
	}

	if len(missing) > 0 {
		return nil, xerrors.Wrap(errDeclShape, "password policy に "+strings.Join(missing, "・")+" がありません。書かれていない argument は確かめられません")
	}

	return policy, nil
}

// readDelegatedShape は、委任管理者の宣言を読む。主体は output delegated_admin の service_principals を、
// aws_organizations_delegated_administrator の for_each の key を入れて評価する。委任先は、その resource の
// account_id が指す aws_organizations_account.this["<env>"]。
func readDelegatedShape(readFile func(string) ([]byte, error), dir string) (delegatedShape, error) {
	body, err := parseFile(readFile, path.Join(dir, delegatedFile))
	if err != nil {
		return delegatedShape{}, err
	}

	res, err := singleResource(body, delegatedResource)
	if err != nil {
		return delegatedShape{}, err
	}

	keys, err := literalSet(res.Body.Attributes["for_each"])
	if err != nil {
		return delegatedShape{}, xerrors.Join(errDeclShape, err)
	}

	env, err := accountRefEnv(res.Body.Attributes["account_id"])
	if err != nil {
		return delegatedShape{}, err
	}

	outputs, err := parseFile(readFile, path.Join(dir, outputsFile))
	if err != nil {
		return delegatedShape{}, err
	}

	principals, err := evalDelegatedPrincipals(outputs, res.Labels[1], keys)
	if err != nil {
		return delegatedShape{}, err
	}

	return delegatedShape{env: env, principals: principals}, nil
}

// accountRefEnv は、`aws_organizations_account.this["<env>"].id` の env を読む。
func accountRefEnv(attr *hclsyntax.Attribute) (string, error) {
	if attr == nil {
		return "", xerrors.Wrap(errDeclShape, delegatedResource+" に account_id がありません")
	}

	st, ok := attr.Expr.(*hclsyntax.ScopeTraversalExpr)
	if !ok || len(st.Traversal) != 4 || st.Traversal.RootName() != accountResource {
		return "", xerrors.Wrap(errDeclShape, "account_id が "+accountResource+`.<name>["<env>"].id ではありません`)
	}

	index, ok1 := st.Traversal[2].(hcl.TraverseIndex)
	id, ok2 := st.Traversal[3].(hcl.TraverseAttr)

	if !ok1 || !ok2 || id.Name != "id" || index.Key.Type() != cty.String || !index.Key.IsKnown() || index.Key.IsNull() || index.Key.AsString() == "" {
		return "", xerrors.Wrap(errDeclShape, "account_id が "+accountResource+`.<name>["<env>"].id ではありません`)
	}

	return index.Key.AsString(), nil
}

// evalDelegatedPrincipals は、output delegated_admin の値の service_principals を評価する。
func evalDelegatedPrincipals(body *hclsyntax.Body, resourceName string, keys []string) ([]string, error) {
	var found []*hclsyntax.Block

	for _, b := range body.Blocks {
		if b.Type == "output" && len(b.Labels) == 1 && b.Labels[0] == "delegated_admin" {
			found = append(found, b)
		}
	}

	if len(found) != 1 {
		return nil, xerrors.Wrapf(errDeclShape, "output delegated_admin が %d 件", len(found))
	}

	value, ok := found[0].Body.Attributes["value"]
	if !ok {
		return nil, xerrors.Wrap(errDeclShape, "output delegated_admin に value がありません")
	}

	obj, ok := value.Expr.(*hclsyntax.ObjectConsExpr)
	if !ok {
		return nil, xerrors.Wrap(errDeclShape, "output delegated_admin の value が object の構文ではありません")
	}

	var expr hclsyntax.Expression

	for _, item := range obj.Items {
		if k, diags := item.KeyExpr.Value(nil); !diags.HasErrors() && k.Type() == cty.String && k.AsString() == "service_principals" {
			expr = item.ValueExpr
		}
	}

	if expr == nil {
		return nil, xerrors.Wrap(errDeclShape, "output delegated_admin に service_principals がありません")
	}

	instances := map[string]cty.Value{}
	for _, k := range keys {
		instances[k] = cty.EmptyObjectVal
	}

	ctx := &hcl.EvalContext{
		Variables: map[string]cty.Value{
			delegatedResource: cty.ObjectVal(map[string]cty.Value{resourceName: cty.ObjectVal(instances)}),
		},
		Functions: map[string]function.Function{"sort": stdlib.SortFunc, "concat": stdlib.ConcatFunc, "keys": stdlib.KeysFunc},
	}

	v, diags := expr.Value(ctx)
	if diags.HasErrors() || !v.IsWhollyKnown() || v.IsNull() || (!v.Type().IsListType() && !v.Type().IsTupleType()) || v.LengthInt() == 0 {
		return nil, xerrors.Wrap(errDeclShape, "service_principals を、委任管理者の resource の key だけから評価できません")
	}

	var out []string

	for it := v.ElementIterator(); it.Next(); {
		_, e := it.Element()
		if e.Type() != cty.String || e.AsString() == "" || slices.Contains(out, e.AsString()) {
			return nil, xerrors.Wrap(errDeclShape, "service_principals が、重複の無い空でない文字列のリストではありません")
		}

		out = append(out, e.AsString())
	}

	slices.Sort(out)

	return out, nil
}

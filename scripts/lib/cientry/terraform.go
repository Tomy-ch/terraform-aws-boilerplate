package cientry

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// TerraformRolesFile は CI の role を作る Terraform のソースの、リポジトリの直下からのパスです。
// その `locals` の `roles` を登録簿と突き合わせます。
const TerraformRolesFile = "usecases/deployment-identity/internal/github-oidc/main.tf"

// Terraform の role の name と environment が参照する変数。
const (
	TerraformNamePrefix = "${var.name_prefix}"
	TerraformEnvPrefix  = "${var.env}"
)

// TerraformRole は `local.roles` の1行です。
type TerraformRole struct {
	// Key と role の対応は Contract Test が持ち、ここでは見ません。
	Key string
	// Role は environment の `${var.env}-<role>` の `<role>`。
	Role string
	// Name と Environment は、参照する変数を `${var.<名前>}` の字面で残した値です。
	Name        string
	Environment string
	// Line はキーの行番号（1始まり）。
	Line int
}

var (
	// ErrTerraformParse は `local.roles` を、キーが識別子で、各行が name と environment の template だけを持つ
	// object のリテラルとして読めない場合のエラー。
	ErrTerraformParse = xerrors.New("github-oidc の local.roles を解釈できません")
	// ErrTerraformRole は `local.roles` の role が登録簿と一致しない場合のエラー。
	ErrTerraformRole = xerrors.New("github-oidc の local.roles の role が登録簿と一致しません")
)

// ParseTerraformRoles は Terraform のソース src の `locals` の `roles` を、書かれた順で返します。
//
// `roles` はすべての `locals` を通じてちょうど1つで、値は object のリテラルです。各行のキーは引用符の無い
// 識別子、値は name と environment の2つの属性だけを持つ object のリテラルです。name は
// `"${var.name_prefix}-<文字列>"`、environment は `"${var.env}-<文字列>"` の、変数1つと固定の文字列1つからなる引用符の
// template に限ります。これ以外の形、キー・name・environment の重複、0行は [ErrTerraformParse] です。
// 読めた行数は、字句の列から数えた environment の属性の数と突き合わせます（ADR-0702 決定15）。
func ParseTerraformRoles(src []byte) ([]TerraformRole, error) {
	file, diags := hclsyntax.ParseConfig(src, TerraformRolesFile, hcl.InitialPos)
	if diags.HasErrors() {
		return nil, xerrors.Wrap(ErrTerraformParse, diags.Error())
	}
	body, ok := file.Body.(*hclsyntax.Body)
	if !ok {
		return nil, xerrors.Wrap(ErrTerraformParse, "本体を読めません")
	}
	var attrs []*hclsyntax.Attribute
	for _, block := range body.Blocks {
		if block.Type != "locals" {
			continue
		}
		if a, ok := block.Body.Attributes["roles"]; ok {
			attrs = append(attrs, a)
		}
	}
	if len(attrs) != 1 {
		return nil, xerrors.Wrap(ErrTerraformParse, fmt.Sprintf("locals の roles が %d 個です（1個を期待）", len(attrs)))
	}
	attr := attrs[0]
	obj, ok := attr.Expr.(*hclsyntax.ObjectConsExpr)
	if !ok {
		return nil, xerrors.Wrap(ErrTerraformParse, fmt.Sprintf("%d 行目: roles が object のリテラルではありません", attr.SrcRange.Start.Line))
	}
	if len(obj.Items) == 0 {
		return nil, xerrors.Wrap(ErrTerraformParse, fmt.Sprintf("%d 行目: roles が0行です", attr.SrcRange.Start.Line))
	}
	rows := make([]TerraformRole, 0, len(obj.Items))
	for _, item := range obj.Items {
		row, err := parseTerraformRow(src, item)
		if err != nil {
			return nil, err
		}
		for _, p := range rows {
			if p.Key == row.Key || p.Name == row.Name || p.Environment == row.Environment {
				return nil, xerrors.Wrap(ErrTerraformParse, fmt.Sprintf("%d 行目: %s は %d 行目の %s とキー・name・environment のどれかが重複しています", row.Line, row.Key, p.Line, p.Key))
			}
		}
		rows = append(rows, row)
	}
	count, err := countEnvironmentTokens(src, attr.SrcRange)
	if err != nil {
		return nil, err
	}
	if count != len(rows) {
		return nil, xerrors.Wrap(ErrTerraformParse, fmt.Sprintf("字句の列の environment の属性 %d 個 / 読めた行 %d 行", count, len(rows)))
	}
	return rows, nil
}

func parseTerraformRow(src []byte, item hclsyntax.ObjectConsItem) (TerraformRole, error) {
	line := item.KeyExpr.Range().Start.Line
	key, ok := identKey(item.KeyExpr)
	if !ok {
		return TerraformRole{}, xerrors.Wrap(ErrTerraformParse, fmt.Sprintf("%d 行目: キーが引用符の無い識別子ではありません", line))
	}
	value, ok := item.ValueExpr.(*hclsyntax.ObjectConsExpr)
	if !ok {
		return TerraformRole{}, xerrors.Wrap(ErrTerraformParse, fmt.Sprintf("%d 行目: %s の値が object のリテラルではありません", line, key))
	}
	fields := map[string]hclsyntax.Expression{}
	for _, f := range value.Items {
		name, ok := identKey(f.KeyExpr)
		if !ok || (name != "name" && name != "environment") {
			return TerraformRole{}, xerrors.Wrap(ErrTerraformParse, fmt.Sprintf("%d 行目: %s の属性は name と environment に限ります", line, key))
		}
		if _, dup := fields[name]; dup {
			return TerraformRole{}, xerrors.Wrap(ErrTerraformParse, fmt.Sprintf("%d 行目: %s の %s が重複しています", line, key, name))
		}
		fields[name] = f.ValueExpr
	}
	if len(fields) != 2 {
		return TerraformRole{}, xerrors.Wrap(ErrTerraformParse, fmt.Sprintf("%d 行目: %s に name と environment の両方がありません", line, key))
	}
	nameRest, ok := prefixedTemplate(src, fields["name"], "name_prefix")
	if !ok {
		return TerraformRole{}, xerrors.Wrap(ErrTerraformParse, fmt.Sprintf("%d 行目: %s の name は \"%s-<文字列>\" ではありません", line, key, TerraformNamePrefix))
	}
	envRest, ok := prefixedTemplate(src, fields["environment"], "env")
	if !ok {
		return TerraformRole{}, xerrors.Wrap(ErrTerraformParse, fmt.Sprintf("%d 行目: %s の environment は \"%s-<文字列>\" ではありません", line, key, TerraformEnvPrefix))
	}
	return TerraformRole{
		Key:         key,
		Role:        strings.TrimPrefix(envRest, "-"),
		Name:        TerraformNamePrefix + nameRest,
		Environment: TerraformEnvPrefix + envRest,
		Line:        line,
	}, nil
}

// identKey は object のキーが引用符も括弧も無い識別子1つなら、その名前を返します。
func identKey(expr hclsyntax.Expression) (string, bool) {
	k, ok := expr.(*hclsyntax.ObjectConsKeyExpr)
	if !ok || k.ForceNonLiteral {
		return "", false
	}
	t, ok := k.Wrapped.(*hclsyntax.ScopeTraversalExpr)
	if !ok || len(t.Traversal) != 1 {
		return "", false
	}
	return t.Traversal.RootName(), true
}

// prefixedTemplate は expr が `"${var.<variable>}-<文字列>"` の引用符の template なら、変数に続く固定の文字列を返します。
func prefixedTemplate(src []byte, expr hclsyntax.Expression, variable string) (string, bool) {
	tmpl, ok := expr.(*hclsyntax.TemplateExpr)
	if !ok || len(tmpl.Parts) != 2 {
		return "", false
	}
	// heredoc も TemplateExpr になる。
	if start := tmpl.SrcRange.Start.Byte; start >= len(src) || src[start] != '"' {
		return "", false
	}
	ref, ok := tmpl.Parts[0].(*hclsyntax.ScopeTraversalExpr)
	if !ok || len(ref.Traversal) != 2 || ref.Traversal.RootName() != "var" {
		return "", false
	}
	if a, ok := ref.Traversal[1].(hcl.TraverseAttr); !ok || a.Name != variable {
		return "", false
	}
	lit, ok := tmpl.Parts[1].(*hclsyntax.LiteralValueExpr)
	if !ok || lit.Val.Type() != cty.String {
		return "", false
	}
	rest := lit.Val.AsString()
	if !strings.HasPrefix(rest, "-") || len(rest) == 1 {
		return "", false
	}
	return rest, true
}

// countEnvironmentTokens は rng の中で、識別子 environment に = か : が続く字句の組を数えます。
// コメントと文字列の中の environment は字句が違うので数えません。
func countEnvironmentTokens(src []byte, rng hcl.Range) (int, error) {
	tokens, diags := hclsyntax.LexConfig(src, TerraformRolesFile, hcl.InitialPos)
	if diags.HasErrors() {
		return 0, xerrors.Wrap(ErrTerraformParse, diags.Error())
	}
	n := 0
	for i := 0; i+1 < len(tokens); i++ {
		t := tokens[i]
		if t.Range.Start.Byte < rng.Start.Byte || t.Range.End.Byte > rng.End.Byte {
			continue
		}
		if t.Type == hclsyntax.TokenIdent && string(t.Bytes) == "environment" &&
			(tokens[i+1].Type == hclsyntax.TokenEqual || tokens[i+1].Type == hclsyntax.TokenColon) {
			n++
		}
	}
	return n, nil
}

// TerraformRolesRepo はリポジトリ repo の [TerraformRolesFile] を読んで [ParseTerraformRoles] します。
func TerraformRolesRepo(repo string) ([]TerraformRole, error) {
	r, err := os.OpenRoot(repo)
	if err != nil {
		return nil, xerrors.Wrap(err, "open "+repo)
	}
	defer func() { _ = r.Close() }()
	src, err := r.ReadFile(TerraformRolesFile)
	if err != nil {
		return nil, xerrors.Wrap(err, "read "+TerraformRolesFile)
	}
	rows, err := ParseTerraformRoles(src)
	if err != nil {
		return nil, xerrors.Wrap(err, TerraformRolesFile)
	}
	return rows, nil
}

// CheckTerraformRoles は、rows の各行の name が `${var.name_prefix}-ci-<role>` であることと、rows の role の
// 集合が登録簿の role の集合と過不足なく一致することを見ます。どちらかが成り立たなければ
// [ErrTerraformRole] を返し、登録簿にだけ在る role と rows にだけ在る role を分けて名指します。
func CheckTerraformRoles(rows []TerraformRole) error {
	return std.checkTerraformRoles(rows)
}

func (r registry) checkTerraformRoles(rows []TerraformRole) error {
	if len(r) == 0 {
		return xerrors.Wrap(errRegistry, "種別が1件もありません")
	}
	if len(rows) == 0 {
		return xerrors.Wrap(ErrTerraformRole, "local.roles が0行です")
	}
	var want []string
	for _, k := range r {
		for _, role := range []string{k.IdentityRole, k.WorkloadRole} {
			if !slices.Contains(want, role) {
				want = append(want, role)
			}
		}
	}
	var got []string
	for _, row := range rows {
		if name := TerraformNamePrefix + "-ci-" + row.Role; row.Name != name {
			return xerrors.Wrap(ErrTerraformRole, fmt.Sprintf("%d 行目: %s の name %s は %s ではありません", row.Line, row.Key, row.Name, name))
		}
		got = append(got, row.Role)
	}
	var reasons []string
	if missing := slices.DeleteFunc(slices.Clone(want), func(role string) bool { return slices.Contains(got, role) }); len(missing) > 0 {
		reasons = append(reasons, "登録簿の role "+strings.Join(missing, ", ")+" が local.roles にありません")
	}
	if extra := slices.DeleteFunc(slices.Clone(got), func(role string) bool { return slices.Contains(want, role) }); len(extra) > 0 {
		reasons = append(reasons, "local.roles の role "+strings.Join(extra, ", ")+" は登録簿にありません")
	}
	if len(reasons) > 0 {
		return xerrors.Wrap(ErrTerraformRole, strings.Join(reasons, "。"))
	}
	return nil
}

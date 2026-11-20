package cientry

import (
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// state に書く role の写しを持つファイルの、リポジトリの直下からのパス。
const (
	// StateWriterSCPFile の `locals` の `state_writer_role_patterns` は、deny-state-write の SCP が例外にする role。
	StateWriterSCPFile = "usecases/organization-baseline/internal/organization/policies.tf"
	// StateWriterRegoFile の `state_writer_pattern` は、その例外を Policy Test が許す role。
	StateWriterRegoFile = "conftest/plan/organization_baseline_scp.rego"
)

var (
	// ErrStateWriterParse は state に書く role の写しを、決まった形として読めない場合のエラー。
	ErrStateWriterParse = xerrors.New("state に書く role の写しを解釈できません")
	// ErrStateWriterRole は state に書く role の写しが登録簿と一致しない場合のエラー。
	ErrStateWriterRole = xerrors.New("state に書く role の写しが登録簿と一致しません")
)

const (
	stateWriterLocal = "state_writer_role_patterns"
	regoRolePrefix   = `^arn:aws:iam::\*:role/`
)

var (
	// regoHeadRe は state_writer_pattern を頭に持つ規則の行。代入の記号と規則の形を問わない。
	regoHeadRe    = regexp.MustCompile(`^\s*state_writer_pattern\b`)
	regoPatternRe = regexp.MustCompile("^state_writer_pattern := `([^`]*)`$")
	// regoShapeRe は regoRolePrefix に続く部分の形。env の選択肢は語の形だけを見て、値を問わない。
	regoShapeRe = regexp.MustCompile(`^\[a-z0-9\]\+-\(` + envWord + `(?:\|` + envWord + `)*\)-ci-\(([^()]*)\)\$$`)
)

// StateWriterRoles は state Bucket に書き込む role を、重複を除いて登録簿の順で返します。lock を取らない
// 種別（[LockSkip]）の role は state にも lock file にも書かないので含めません（ADR-0604 決定8）。
func StateWriterRoles() []string {
	return std.stateWriterRoles()
}

func (r registry) stateWriterRoles() []string {
	var out []string
	for _, k := range r {
		if k.Lock == LockSkip {
			continue
		}
		for _, role := range []string{k.IdentityRole, k.WorkloadRole} {
			if !slices.Contains(out, role) {
				out = append(out, role)
			}
		}
	}
	return out
}

// checkStateWriterRoles は、写し name の role の集合 got が [StateWriterRoles] と過不足なく一致することを見ます。
// 一致しなければ [ErrStateWriterRole] を返し、登録簿にだけ在る role と got にだけ在る role を分けて名指します。
func (r registry) checkStateWriterRoles(name string, got []string) error {
	want := r.stateWriterRoles()
	if len(want) == 0 {
		return xerrors.Wrap(errRegistry, "state に書く role が1件もありません")
	}
	if len(got) == 0 {
		return xerrors.Wrap(ErrStateWriterRole, name+" の role が0件です")
	}
	var reasons []string
	if missing := slices.DeleteFunc(slices.Clone(want), func(role string) bool { return slices.Contains(got, role) }); len(missing) > 0 {
		reasons = append(reasons, "登録簿の state に書く role "+strings.Join(missing, ", ")+" が "+name+" にありません")
	}
	if extra := slices.DeleteFunc(slices.Clone(got), func(role string) bool { return slices.Contains(want, role) }); len(extra) > 0 {
		reasons = append(reasons, name+" の role "+strings.Join(extra, ", ")+" は登録簿の state に書く role にありません")
	}
	if len(reasons) > 0 {
		return xerrors.Wrap(ErrStateWriterRole, strings.Join(reasons, "。"))
	}
	return nil
}

// parseSCPStateWriterRoles は Terraform のソース src の `locals` の `state_writer_role_patterns` から、role の接尾辞の
// 一覧を書かれた順で返します。
//
// 値は `flatten([for <env> in [<文字列>, ...] : [for <role> in [<文字列>, ...] : "...-ci-${<role>}"]])` の形に限り、内側の for の
// 集まりが role の一覧です。これ以外の形、role の形でない要素、重複、0件は [ErrStateWriterParse] です。読めた件数は、
// 字句の列から数えた内側の for の集まりの文字列の数と突き合わせます（ADR-0702 決定15）。
func parseSCPStateWriterRoles(src []byte) ([]string, error) {
	file, diags := hclsyntax.ParseConfig(src, StateWriterSCPFile, hcl.InitialPos)
	if diags.HasErrors() {
		return nil, xerrors.Wrap(ErrStateWriterParse, diags.Error())
	}
	// hclsyntax.ParseConfig の返す本体は常に *hclsyntax.Body。
	body := file.Body.(*hclsyntax.Body)
	var attrs []*hclsyntax.Attribute
	for _, block := range body.Blocks {
		if block.Type != "locals" {
			continue
		}
		if a, ok := block.Body.Attributes[stateWriterLocal]; ok {
			attrs = append(attrs, a)
		}
	}
	if len(attrs) != 1 {
		return nil, xerrors.Wrap(ErrStateWriterParse, fmt.Sprintf("locals の %s が %d 個です（1個を期待）", stateWriterLocal, len(attrs)))
	}
	attr := attrs[0]
	inner, err := innerRoleFor(attr.Expr)
	if err != nil {
		return nil, xerrors.Wrap(err, fmt.Sprintf("%d 行目", attr.SrcRange.Start.Line))
	}
	// innerRoleFor は集まりが tuple のリテラルであることを確かめている。
	roles, err := stringLiterals(inner.CollExpr.(*hclsyntax.TupleConsExpr))
	if err != nil {
		return nil, err
	}
	if count := countRoleQuotes(src, attr.SrcRange, inner.ValVar); count != len(roles) {
		return nil, xerrors.Wrap(ErrStateWriterParse, fmt.Sprintf("字句の列の role の文字列 %d 個 / 読めた role %d 個", count, len(roles)))
	}
	return roles, nil
}

// innerRoleFor は expr が `flatten([for <env> in [<文字列>, ...] : [for <role> in [...] : "...-ci-${<role>}"]])` なら
// 内側の for を返します。外側の for の集まりが、文字列のリテラルを1つ以上並べた tuple でなくても
// [ErrStateWriterParse] を返します。
func innerRoleFor(expr hclsyntax.Expression) (*hclsyntax.ForExpr, error) {
	call, ok := expr.(*hclsyntax.FunctionCallExpr)
	if !ok || call.Name != "flatten" || len(call.Args) != 1 || call.ExpandFinal {
		return nil, xerrors.Wrap(ErrStateWriterParse, "値が引数1つの flatten の呼び出しではありません")
	}
	outer, ok := call.Args[0].(*hclsyntax.ForExpr)
	if !ok || outer.KeyExpr != nil || outer.CondExpr != nil {
		return nil, xerrors.Wrap(ErrStateWriterParse, "flatten の引数が条件の無い tuple の for 式ではありません")
	}
	if envs, ok := outer.CollExpr.(*hclsyntax.TupleConsExpr); !ok || len(envs.Exprs) == 0 || slices.ContainsFunc(envs.Exprs, func(e hclsyntax.Expression) bool {
		tmpl, ok := e.(*hclsyntax.TemplateExpr)
		return !ok || !tmpl.IsStringLiteral()
	}) {
		return nil, xerrors.Wrap(ErrStateWriterParse, "外側の for の集まりが、文字列のリテラルを1つ以上並べた tuple ではありません")
	}
	inner, ok := outer.ValExpr.(*hclsyntax.ForExpr)
	if !ok || inner.KeyExpr != nil || inner.CondExpr != nil || inner.KeyVar != "" {
		return nil, xerrors.Wrap(ErrStateWriterParse, "外側の for の値が、キーの変数も条件も無い tuple の for 式ではありません")
	}
	if _, ok := inner.CollExpr.(*hclsyntax.TupleConsExpr); !ok {
		return nil, xerrors.Wrap(ErrStateWriterParse, "内側の for の集まりが tuple のリテラルではありません")
	}
	if !endsWithRole(inner.ValExpr, inner.ValVar) {
		return nil, xerrors.Wrap(ErrStateWriterParse, fmt.Sprintf("内側の for の値が \"...-ci-${%s}\" で終わる template ではありません", inner.ValVar))
	}
	return inner, nil
}

// endsWithRole は expr が、`-ci-` で終わる固定の文字列と変数 name の参照で終わる template なら真を返します。
func endsWithRole(expr hclsyntax.Expression, name string) bool {
	tmpl, ok := expr.(*hclsyntax.TemplateExpr)
	if !ok || len(tmpl.Parts) < 2 {
		return false
	}
	ref, ok := tmpl.Parts[len(tmpl.Parts)-1].(*hclsyntax.ScopeTraversalExpr)
	if !ok || len(ref.Traversal) != 1 || ref.Traversal.RootName() != name {
		return false
	}
	lit, ok := tmpl.Parts[len(tmpl.Parts)-2].(*hclsyntax.LiteralValueExpr)
	// template の固定の部分は常に文字列。
	return ok && strings.HasSuffix(lit.Val.AsString(), "-ci-")
}

// stringLiterals は tuple の要素を、role の形の文字列のリテラルとして書かれた順で返します。要素が文字列のリテラルで
// ないか、role の形でないか、重複するか、0件なら [ErrStateWriterParse] を返します。
func stringLiterals(tuple *hclsyntax.TupleConsExpr) ([]string, error) {
	if len(tuple.Exprs) == 0 {
		return nil, xerrors.Wrap(ErrStateWriterParse, fmt.Sprintf("%d 行目: role の一覧が0件です", tuple.SrcRange.Start.Line))
	}
	roles := make([]string, 0, len(tuple.Exprs))
	for _, e := range tuple.Exprs {
		line := e.Range().Start.Line
		tmpl, ok := e.(*hclsyntax.TemplateExpr)
		if !ok || !tmpl.IsStringLiteral() {
			return nil, xerrors.Wrap(ErrStateWriterParse, fmt.Sprintf("%d 行目: role の一覧の要素が文字列のリテラルではありません", line))
		}
		// IsStringLiteral は Parts が LiteralValueExpr 1つであることを確かめている。
		role := tmpl.Parts[0].(*hclsyntax.LiteralValueExpr).Val.AsString()
		if err := appendRole(&roles, role); err != nil {
			return nil, xerrors.Wrap(err, fmt.Sprintf("%d 行目", line))
		}
	}
	return roles, nil
}

// appendRole は role を roles へ足します。role の形でないか、既に在れば [ErrStateWriterParse] を返します。
func appendRole(roles *[]string, role string) error {
	if !roleRe.MatchString(role) {
		return xerrors.Wrap(ErrStateWriterParse, fmt.Sprintf("role %q を読めません", role))
	}
	if slices.Contains(*roles, role) {
		return xerrors.Wrap(ErrStateWriterParse, "role "+role+" が重複しています")
	}
	*roles = append(*roles, role)
	return nil
}

// countRoleQuotes は rng の中の、字句の並び `for <name> in [` に続いて対応する `]` までに開く文字列の数を、並びが
// 何度現れても合わせて数えます。src は hclsyntax.ParseConfig が誤り無く読んだソースに限ります。字句として読めなければ
// panic します。
func countRoleQuotes(src []byte, rng hcl.Range, name string) int {
	tokens, diags := hclsyntax.LexConfig(src, StateWriterSCPFile, hcl.InitialPos)
	if diags.HasErrors() {
		panic("cientry.countRoleQuotes: hclsyntax.ParseConfig が読めたソースではありません: " + diags.Error())
	}
	var in []hclsyntax.Token
	for _, t := range tokens {
		if t.Range.Start.Byte >= rng.Start.Byte && t.Range.End.Byte <= rng.End.Byte {
			in = append(in, t)
		}
	}
	ident := func(t hclsyntax.Token, s string) bool { return t.Type == hclsyntax.TokenIdent && string(t.Bytes) == s }
	n := 0
	for i := 0; i+3 < len(in); i++ {
		if !ident(in[i], "for") || !ident(in[i+1], name) || !ident(in[i+2], "in") || in[i+3].Type != hclsyntax.TokenOBrack {
			continue
		}
		depth := 0
		for _, t := range in[i+3:] {
			switch t.Type {
			case hclsyntax.TokenOBrack:
				depth++
			case hclsyntax.TokenCBrack:
				depth--
			case hclsyntax.TokenOQuote:
				n++
			}
			if depth == 0 {
				break
			}
		}
	}
	return n
}

// parseRegoStateWriterRoles は Rego のソース src の `state_writer_pattern` の正規表現から、末尾の
// `-ci-(<role>|...)$` の role を書かれた順で返します。
//
// state_writer_pattern を頭に持つ規則の行はちょうど1つで、その行は raw string の正規表現1つの代入に限ります。
// 正規表現は `^arn:aws:iam::\*:role/[a-z0-9]+-(<env>|...)-ci-(<role>|...)$` の形に限り、env の選択肢は1つ以上の
// 語です。正規表現が組めないか、この形でないか、role の形でない選択肢か重複があれば [ErrStateWriterParse] です。
func parseRegoStateWriterRoles(src []byte) ([]string, error) {
	lines := strings.Split(string(src), "\n")
	var heads []int
	for i, l := range lines {
		if regoHeadRe.MatchString(l) {
			heads = append(heads, i)
		}
	}
	if len(heads) != 1 {
		return nil, xerrors.Wrap(ErrStateWriterParse, fmt.Sprintf("state_writer_pattern の規則の行が %d 行です（1行を期待）", len(heads)))
	}
	line := heads[0] + 1
	m := regoPatternRe.FindStringSubmatch(lines[heads[0]])
	if m == nil {
		return nil, xerrors.Wrap(ErrStateWriterParse, fmt.Sprintf("%d 行目: state_writer_pattern が raw string 1つの代入ではありません", line))
	}
	pattern := m[1]
	if _, err := regexp.Compile(pattern); err != nil {
		return nil, xerrors.Wrap(ErrStateWriterParse, fmt.Sprintf("%d 行目: 正規表現を組めません: %v", line, err))
	}
	rest, ok := strings.CutPrefix(pattern, regoRolePrefix)
	if !ok {
		return nil, xerrors.Wrap(ErrStateWriterParse, fmt.Sprintf("%d 行目: 正規表現が %s で始まりません", line, regoRolePrefix))
	}
	g := regoShapeRe.FindStringSubmatch(rest)
	if g == nil {
		return nil, xerrors.Wrap(ErrStateWriterParse, fmt.Sprintf("%d 行目: 正規表現が %s[a-z0-9]+-(<env>|...)-ci-(<role>|...)$ の形ではありません", line, regoRolePrefix))
	}
	var roles []string
	for _, role := range strings.Split(g[1], "|") {
		if err := appendRole(&roles, role); err != nil {
			return nil, xerrors.Wrap(err, fmt.Sprintf("%d 行目", line))
		}
	}
	return roles, nil
}

// readRoles はリポジトリ repo の path を読み、parse で role の一覧にします。
func readRoles(repo, path string, parse func([]byte) ([]string, error)) ([]string, error) {
	r, err := os.OpenRoot(repo)
	if err != nil {
		return nil, xerrors.Wrap(err, "open "+repo)
	}
	defer func() { _ = r.Close() }()
	src, err := r.ReadFile(path)
	if err != nil {
		return nil, xerrors.Wrap(err, "read "+path)
	}
	roles, err := parse(src)
	if err != nil {
		return nil, xerrors.Wrap(err, path)
	}
	return roles, nil
}

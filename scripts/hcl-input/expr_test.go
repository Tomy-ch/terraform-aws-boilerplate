package main

import (
	"encoding/json"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zclconf/go-cty/cty"
)

func exprJSON(t *testing.T, src string) string {
	t.Helper()
	got, err := exprOf(parseExpr(t, src))
	require.NoError(t, err)
	raw, err := json.Marshal(got)
	require.NoError(t, err)

	return string(raw)
}

func Test_exprOf(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		src  string
		want string
	}{
		"数":           {`1.5`, `{"kind":"literal","value":1.5}`},
		"負の数":         {`-2`, `{"kind":"literal","value":-2}`},
		"真偽":          {`true`, `{"kind":"literal","value":true}`},
		"null":        {`null`, `{"kind":"literal","value":null}`},
		"引用符の文字列":     {`"a b"`, `{"kind":"literal","value":"a b"}`},
		"$${ は文字の ${": {`"$${x}"`, `{"kind":"literal","value":"${x}"}`},
		"補間の無いヒアドキュメント":  {"<<EOT\nline\nEOT\n", `{"kind":"literal","value":"line\n"}`},
		"リテラルだけの補間":      {`"${"a"}"`, `{"kind":"literal","value":"a"}`},
		"リテラルだけの tuple":  {`["a", 1, null]`, `{"kind":"literal","value":["a",1,null]}`},
		"リテラルだけの object": {`{a = "x", "b" = [true]}`, `{"kind":"literal","value":{"a":"x","b":[true]}}`},
		"括弧を外す":          {`("a")`, `{"kind":"literal","value":"a"}`},
		"補間を含むヒアドキュメント": {"<<EOT\n${var.x}\nEOT\n",
			`{"kind":"template","parts":[{"kind":"traversal","root":"var","steps":[{"kind":"attr","name":"x"}]},{"kind":"literal","value":"\n"}]}`},
		"path.module のテンプレート": {`"${path.module}/x"`,
			`{"kind":"template","parts":[{"kind":"traversal","root":"path","steps":[{"kind":"attr","name":"module"}]},{"kind":"literal","value":"/x"}]}`},
		"補間だけのテンプレート": {`"${var.x}"`,
			`{"kind":"template","parts":[{"kind":"traversal","root":"var","steps":[{"kind":"attr","name":"x"}]}]}`},
		"file の呼び出し": {`file("x")`,
			`{"kind":"call","name":"file","args":[{"kind":"literal","value":"x"}],"expand":false}`},
		"path.module を取る file": {`file("${path.module}/x")`,
			`{"kind":"call","name":"file","args":[{"kind":"template","parts":[{"kind":"traversal","root":"path","steps":[{"kind":"attr","name":"module"}]},{"kind":"literal","value":"/x"}]}],"expand":false}`},
		"templatefile": {`templatefile("${path.module}/t", {a = var.a})`,
			`{"kind":"call","name":"templatefile","args":[{"kind":"template","parts":[{"kind":"traversal","root":"path","steps":[{"kind":"attr","name":"module"}]},{"kind":"literal","value":"/t"}]},{"kind":"object","items":[{"key":{"kind":"literal","value":"a"},"value":{"kind":"traversal","root":"var","steps":[{"kind":"attr","name":"a"}]}}]}],"expand":false}`},
		"リテラルを取る jsonencode も呼び出し": {`jsonencode({a = 1})`,
			`{"kind":"call","name":"jsonencode","args":[{"kind":"literal","value":{"a":1}}],"expand":false}`},
		"入れ子の呼び出し": {`base64encode(file("x"))`,
			`{"kind":"call","name":"base64encode","args":[{"kind":"call","name":"file","args":[{"kind":"literal","value":"x"}],"expand":false}],"expand":false}`},
		"展開": {`concat(var.xs...)`,
			`{"kind":"call","name":"concat","args":[{"kind":"traversal","root":"var","steps":[{"kind":"attr","name":"xs"}]}],"expand":true}`},
		"添字を持つ traversal": {`aws_s3_bucket.this[0].id`,
			`{"kind":"traversal","root":"aws_s3_bucket","steps":[{"kind":"attr","name":"this"},{"kind":"index"},{"kind":"attr","name":"id"}]}`},
		"depends_on の形": {`[aws_iam_role_policy.first, module.m]`,
			`{"kind":"tuple","items":[{"kind":"traversal","root":"aws_iam_role_policy","steps":[{"kind":"attr","name":"first"}]},{"kind":"traversal","root":"module","steps":[{"kind":"attr","name":"m"}]}]}`},
		"参照を含む object": {`{(var.k) = 1, b = var.v}`,
			`{"kind":"object","items":[{"key":{"kind":"traversal","root":"var","steps":[{"kind":"attr","name":"k"}]},"value":{"kind":"literal","value":1}},{"key":{"kind":"literal","value":"b"},"value":{"kind":"traversal","root":"var","steps":[{"kind":"attr","name":"v"}]}}]}`},
		"tuple の for":  {`[for x in var.xs : x]`, `{"kind":"other","syntax":"for"}`},
		"object の for": {`{for k, v in var.m : k => v}`, `{"kind":"other","syntax":"for"}`},
		"[*] の splat":  {`var.a[*].b`, `{"kind":"other","syntax":"splat"}`},
		".* の splat":   {`var.a.*.b`, `{"kind":"other","syntax":"splat"}`},
		"式の添字":         {`var.m[var.k]`, `{"kind":"other","syntax":"index"}`},
		"条件の中の file":   {`var.c ? file("a") : file("b")`, `{"kind":"other","syntax":"conditional"}`},
		"二項の演算子":       {`var.a + 1`, `{"kind":"other","syntax":"binary"}`},
		"単項の演算子":       {`!var.a`, `{"kind":"other","syntax":"unary"}`},
		"参照の後の属性の段":    {`foo(1).bar`, `{"kind":"other","syntax":"relative_traversal"}`},
		"JSON に書けない値":  {`1 / 0`, `{"kind":"other","syntax":"binary"}`},
		"%{for} の指示子":  {"\"%{for x in var.xs}${x}%{endfor}\"", `{"kind":"template","parts":[{"kind":"other","syntax":"template_join"}]}`},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.JSONEq(t, tt.want, exprJSON(t, tt.src))
		})
	}

	t.Run("構文の誤りの式は errUnknownExpression", func(t *testing.T) {
		t.Parallel()
		_, err := exprOf(&hclsyntax.ExprSyntaxError{})
		require.ErrorIs(t, err, errUnknownExpression)
	})

	t.Run("歩く型の外の式は errUnknownExpression", func(t *testing.T) {
		t.Parallel()
		_, err := exprOf(&hclsyntax.AnonSymbolExpr{})
		require.ErrorIs(t, err, errUnknownExpression)
	})

	t.Run("入れ子の歩く型の外の式も errUnknownExpression", func(t *testing.T) {
		t.Parallel()
		anon := &hclsyntax.AnonSymbolExpr{}
		lit := &hclsyntax.LiteralValueExpr{Val: cty.StringVal("k")}
		tests := map[string]hclsyntax.Expression{
			"テンプレートの補間だけ": &hclsyntax.TemplateWrapExpr{Wrapped: anon},
			"テンプレートの部分":   &hclsyntax.TemplateExpr{Parts: []hclsyntax.Expression{lit, anon}},
			"関数の引数":       &hclsyntax.FunctionCallExpr{Name: "f", Args: []hclsyntax.Expression{anon}},
			"tuple の要素":   &hclsyntax.TupleConsExpr{Exprs: []hclsyntax.Expression{lit, anon}},
			"object のキー":  &hclsyntax.ObjectConsExpr{Items: []hclsyntax.ObjectConsItem{{KeyExpr: anon, ValueExpr: lit}}},
			"object の値":   &hclsyntax.ObjectConsExpr{Items: []hclsyntax.ObjectConsItem{{KeyExpr: lit, ValueExpr: anon}}},
			"段の途中に根を持つ参照": &hclsyntax.ScopeTraversalExpr{Traversal: hcl.Traversal{hcl.TraverseRoot{Name: "a"}, hcl.TraverseRoot{Name: "b"}}},
		}
		for name, e := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				_, err := exprOf(e)
				require.ErrorIs(t, err, errUnknownExpression)
			})
		}
	})
}

func Test_isLiteral(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		src  string
		want bool
	}{
		"文字列":              {`"a"`, true},
		"リテラルだけの入れ子":       {`[1, {a = "b"}]`, true},
		"負の数":              {`-1`, true},
		"補間":               {`"${var.x}"`, false},
		"関数の呼び出し":          {`upper("a")`, false},
		"tuple の中の関数の呼び出し": {`[upper("a")]`, false},
		"参照":               {`local.x`, false},
		"値は決まるが JSON に書けない": {`1 / 0`, true},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, got := isLiteral(parseExpr(t, tt.src))
			assert.Equal(t, tt.want, got)
		})
	}

	t.Run("構文の誤りの式は literal でない", func(t *testing.T) {
		t.Parallel()
		_, got := isLiteral(&hclsyntax.ExprSyntaxError{})
		assert.False(t, got)
	})
}

func Test_jsonOf(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		in   cty.Value
		want string
	}{
		"null":           {cty.NullVal(cty.DynamicPseudoType), `null`},
		"整数":             {cty.NumberIntVal(3), `3`},
		"小数":             {cty.NumberFloatVal(0.25), `0.25`},
		"文字列":            {cty.StringVal("a"), `"a"`},
		"真偽":             {cty.False, `false`},
		"tuple の中の null": {cty.TupleVal([]cty.Value{cty.NullVal(cty.DynamicPseudoType), cty.StringVal("x")}), `[null,"x"]`},
		"空の tuple":       {cty.EmptyTupleVal, `[]`},
		"object":         {cty.ObjectVal(map[string]cty.Value{"a": cty.ListVal([]cty.Value{cty.True})}), `{"a":[true]}`},
		"空の object":      {cty.EmptyObjectVal, `{}`},
		"map":            {cty.MapVal(map[string]cty.Value{"k": cty.StringVal("v")}), `{"k":"v"}`},
		"set":            {cty.SetVal([]cty.Value{cty.StringVal("s")}), `["s"]`},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := jsonOf(tt.in)
			require.NoError(t, err)
			raw, err := json.Marshal(got)
			require.NoError(t, err)
			assert.JSONEq(t, tt.want, string(raw))
		})
	}

	t.Run("無限大は errUnknownExpression", func(t *testing.T) {
		t.Parallel()
		_, err := jsonOf(cty.PositiveInfinity)
		require.ErrorIs(t, err, errUnknownExpression)
	})

	t.Run("未知の値は errUnknownExpression", func(t *testing.T) {
		t.Parallel()
		_, err := jsonOf(cty.UnknownVal(cty.String))
		require.ErrorIs(t, err, errUnknownExpression)
	})
}

func Test_syntaxName(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		src  string
		want string
	}{
		"for":      {`[for x in var.xs : x]`, "for"},
		"splat":    {`var.a[*].b`, "splat"},
		"式の添字":     {`var.m[var.k]`, "index"},
		"条件":       {`var.c ? 1 : 2`, "conditional"},
		"二項の演算子":   {`var.a + 1`, "binary"},
		"単項の演算子":   {`!var.a`, "unary"},
		"呼び出しの後の段": {`foo(1).bar`, "relative_traversal"},
		"歩く型の外":    {`"a"`, ""},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, syntaxName(parseExpr(t, tt.src)))
		})
	}

	t.Run("%{for} の指示子", func(t *testing.T) {
		t.Parallel()
		tmpl := parseExpr(t, "\"%{for x in var.xs}${x}%{endfor}\"").(*hclsyntax.TemplateExpr)
		assert.Equal(t, "template_join", syntaxName(tmpl.Parts[0]))
	})
}

func Test_expr_MarshalJSON(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		in   expr
		want string
	}{
		"null の literal": {expr{Kind: exprLiteral}, `{"kind":"literal","value":null}`},
		"引数の無い call":     {expr{Kind: exprCall, Name: "timestamp"}, `{"kind":"call","name":"timestamp","args":[],"expand":false}`},
		"空の tuple":       {expr{Kind: exprTuple}, `{"kind":"tuple","items":[]}`},
		"空の object":      {expr{Kind: exprObject}, `{"kind":"object","items":[]}`},
		"段の無い traversal": {expr{Kind: exprTraversal, Root: "plan"}, `{"kind":"traversal","root":"plan","steps":[]}`},
		"部分の無い template": {expr{Kind: exprTemplate}, `{"kind":"template","parts":[]}`},
		"other":          {expr{Kind: exprOther, Syntax: "for"}, `{"kind":"other","syntax":"for"}`},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			raw, err := json.Marshal(tt.in)
			require.NoError(t, err)
			assert.JSONEq(t, tt.want, string(raw))
		})
	}
}

func Test_exprsOf(t *testing.T) {
	t.Parallel()

	got, err := exprsOf(nil)
	require.NoError(t, err)
	assert.Equal(t, []expr{}, got)

	_, err = exprsOf([]hclsyntax.Expression{parseExpr(t, `1`), &hclsyntax.AnonSymbolExpr{}})
	require.ErrorIs(t, err, errUnknownExpression)
}

func Test_nonNil(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []expr{}, nonNil(nil))
	assert.Equal(t, []expr{{Kind: exprOther}}, nonNil([]expr{{Kind: exprOther}}))
}

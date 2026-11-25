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

// unknownExpr は hclsyntax.Expression を満たすが、type switch のどの case にも当たらない式。
type unknownExpr struct{ *hclsyntax.LiteralValueExpr }

func newUnknown() hclsyntax.Expression {
	return unknownExpr{&hclsyntax.LiteralValueExpr{Val: cty.True}}
}

func parseExpr(t *testing.T, src string) hclsyntax.Expression {
	t.Helper()
	e, diags := hclsyntax.ParseExpression([]byte(src), "t.tf", hcl.InitialPos)
	require.Empty(t, diags, "fixture の式が構文の誤りを持つ")

	return e
}

func refsJSON(t *testing.T, src string) string {
	t.Helper()
	refs, err := walkExpr(parseExpr(t, src))
	require.NoError(t, err)
	raw, err := json.Marshal(refs)
	require.NoError(t, err)

	return string(raw)
}

func scopeTraversal(names ...string) *hclsyntax.ScopeTraversalExpr {
	trav := hcl.Traversal{hcl.TraverseRoot{Name: names[0]}}
	for _, n := range names[1:] {
		trav = append(trav, hcl.TraverseAttr{Name: n})
	}

	return &hclsyntax.ScopeTraversalExpr{Traversal: trav}
}

func Test_walkExpr(t *testing.T) {
	t.Parallel()

	t.Run("すり抜けの7つの形は、どれも正しい参照の集合になる", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			src  string
			want string
		}{
			"escape1-quote-pair": {
				src: `coalesce(aws_s3_bucket.this.id, "a$", var.z, module.m.x, "b")`,
				want: `[{"root":"aws_s3_bucket","steps":[{"kind":"attr","name":"this"},{"kind":"attr","name":"id"}],"within":[],"filter":false},
					{"root":"var","steps":[{"kind":"attr","name":"z"}],"within":[],"filter":false},
					{"root":"module","steps":[{"kind":"attr","name":"m"},{"kind":"attr","name":"x"}],"within":[],"filter":false}]`,
			},
			"escape2-brace-in-interpolation": {
				src: `"${aws_s3_bucket.this.id != "}" ? var.x : var.y}"`,
				want: `[{"root":"aws_s3_bucket","steps":[{"kind":"attr","name":"this"},{"kind":"attr","name":"id"}],"within":[],"filter":false},
					{"root":"var","steps":[{"kind":"attr","name":"x"}],"within":[],"filter":false},
					{"root":"var","steps":[{"kind":"attr","name":"y"}],"within":[],"filter":false}]`,
			},
			"escape3-template-directive": {
				src: `"${aws_s3_bucket.this.id}%{ if var.a }y%{ endif }"`,
				want: `[{"root":"aws_s3_bucket","steps":[{"kind":"attr","name":"this"},{"kind":"attr","name":"id"}],"within":[],"filter":false},
					{"root":"var","steps":[{"kind":"attr","name":"a"}],"within":[],"filter":false}]`,
			},
			"escape4-concatenated-dollar": {
				src: `"${aws_s3_bucket.this.id}${"$"}${var.x}"`,
				want: `[{"root":"aws_s3_bucket","steps":[{"kind":"attr","name":"this"},{"kind":"attr","name":"id"}],"within":[],"filter":false},
					{"root":"var","steps":[{"kind":"attr","name":"x"}],"within":[],"filter":false}]`,
			},
			"escape5-comment-in-expression": {
				src: `(aws_s3_bucket.this.id /* " */ == var.x ? aws_s3_bucket.this.id : aws_s3_bucket.this.id /* " */)`,
				want: `[{"root":"aws_s3_bucket","steps":[{"kind":"attr","name":"this"},{"kind":"attr","name":"id"}],"within":[],"filter":false},
					{"root":"var","steps":[{"kind":"attr","name":"x"}],"within":[],"filter":false},
					{"root":"aws_s3_bucket","steps":[{"kind":"attr","name":"this"},{"kind":"attr","name":"id"}],"within":[],"filter":false},
					{"root":"aws_s3_bucket","steps":[{"kind":"attr","name":"this"},{"kind":"attr","name":"id"}],"within":[],"filter":false}]`,
			},
			"escape5-comment-in-expression-line": {
				src: "[aws_s3_bucket.this.id # \"\n, var.x # \"\n][1]",
				want: `[{"root":"aws_s3_bucket","steps":[{"kind":"attr","name":"this"},{"kind":"attr","name":"id"}],"within":[],"filter":false},
					{"root":"var","steps":[{"kind":"attr","name":"x"}],"within":[],"filter":false}]`,
			},
			"escape6-iterator-shadowing": {
				src: `concat([for var in aws_s3_bucket.this : var.id], [var.y])[0]`,
				want: `[{"root":"aws_s3_bucket","steps":[{"kind":"attr","name":"this"}],"within":[],"filter":false},
					{"root":"var","steps":[{"kind":"attr","name":"y"}],"within":[],"filter":false}]`,
			},
			"escape6-iterator-shadowing-resource-type": {
				src: `concat([for aws_iam_role in [aws_s3_bucket.this.id] : aws_iam_role], [aws_iam_role.r.arn])[0]`,
				want: `[{"root":"aws_s3_bucket","steps":[{"kind":"attr","name":"this"},{"kind":"attr","name":"id"}],"within":[],"filter":false},
					{"root":"aws_iam_role","steps":[{"kind":"attr","name":"r"},{"kind":"attr","name":"arn"}],"within":[],"filter":false}]`,
			},
			"escape7-object-key": {
				src: `{ (module.m.x) = aws_s3_bucket.this.id }`,
				want: `[{"root":"module","steps":[{"kind":"attr","name":"m"},{"kind":"attr","name":"x"}],"within":[],"filter":false},
					{"root":"aws_s3_bucket","steps":[{"kind":"attr","name":"this"},{"kind":"attr","name":"id"}],"within":[],"filter":false}]`,
			},
		}
		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				assert.JSONEq(t, tt.want, refsJSON(t, tt.src))
			})
		}
	})

	t.Run("添字の中の参照は、外側からすべての祖先の参照と段を within に持つ", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			src  string
			want string
		}{
			"属性の後の添字": {
				src: `aws_s3_bucket.this[module.x.key].id`,
				want: `[{"root":"aws_s3_bucket","steps":[{"kind":"attr","name":"this"},{"kind":"index","index":{"form":"other"}},{"kind":"attr","name":"id"}],"within":[],"filter":false},
					{"root":"module","steps":[{"kind":"attr","name":"x"},{"kind":"attr","name":"key"}],"within":[{"ref":0,"step":1}],"filter":false}]`,
			},
			"2つ目の添字": {
				src: `aws_s3_bucket.this[module.a.x][module.b.y]`,
				want: `[{"root":"aws_s3_bucket","steps":[{"kind":"attr","name":"this"},{"kind":"index","index":{"form":"other"}},{"kind":"index","index":{"form":"other"}}],"within":[],"filter":false},
					{"root":"module","steps":[{"kind":"attr","name":"a"},{"kind":"attr","name":"x"}],"within":[{"ref":0,"step":1}],"filter":false},
					{"root":"module","steps":[{"kind":"attr","name":"b"},{"kind":"attr","name":"y"}],"within":[{"ref":0,"step":2}],"filter":false}]`,
			},
			"属性とリテラルの添字の後の添字": {
				src: `aws_s3_bucket.this.tags["a"][module.m.x]`,
				want: `[{"root":"aws_s3_bucket","steps":[{"kind":"attr","name":"this"},{"kind":"attr","name":"tags"},{"kind":"index","index":{"form":"literal","value":"a"}},{"kind":"index","index":{"form":"other"}}],"within":[],"filter":false},
					{"root":"module","steps":[{"kind":"attr","name":"m"},{"kind":"attr","name":"x"}],"within":[{"ref":0,"step":3}],"filter":false}]`,
			},
			"リテラルの添字の後の添字": {
				src: `aws_s3_bucket.this[0][module.m.x]`,
				want: `[{"root":"aws_s3_bucket","steps":[{"kind":"attr","name":"this"},{"kind":"index","index":{"form":"literal","value":0}},{"kind":"index","index":{"form":"other"}}],"within":[],"filter":false},
					{"root":"module","steps":[{"kind":"attr","name":"m"},{"kind":"attr","name":"x"}],"within":[{"ref":0,"step":2}],"filter":false}]`,
			},
			"関数の呼び出しの後の添字は参照に付かない": {
				src: `element(aws_s3_bucket.this, 0)[module.m.x]`,
				want: `[{"root":"aws_s3_bucket","steps":[{"kind":"attr","name":"this"}],"within":[],"filter":false},
					{"root":"module","steps":[{"kind":"attr","name":"m"},{"kind":"attr","name":"x"}],"within":[],"filter":false}]`,
			},
			"括弧の後の添字は参照に付かない": {
				src: `(aws_s3_bucket.this)[module.m.x]`,
				want: `[{"root":"aws_s3_bucket","steps":[{"kind":"attr","name":"this"}],"within":[],"filter":false},
					{"root":"module","steps":[{"kind":"attr","name":"m"},{"kind":"attr","name":"x"}],"within":[],"filter":false}]`,
			},
			"タプルの後の添字は参照に付かない": {
				src: `[aws_s3_bucket.this][module.m.x]`,
				want: `[{"root":"aws_s3_bucket","steps":[{"kind":"attr","name":"this"}],"within":[],"filter":false},
					{"root":"module","steps":[{"kind":"attr","name":"m"},{"kind":"attr","name":"x"}],"within":[],"filter":false}]`,
			},
			"splat の後の Each の中の添字は辿る側に倒す": {
				src: `aws_s3_bucket.this[*].tags[module.m.x]`,
				want: `[{"root":"aws_s3_bucket","steps":[{"kind":"attr","name":"this"},{"kind":"splat"}],"within":[],"filter":false},
					{"root":"module","steps":[{"kind":"attr","name":"m"},{"kind":"attr","name":"x"}],"within":[],"filter":false}]`,
			},
			"属性の splat も splat の段": {
				src:  `aws_s3_bucket.this.*.id[0]`,
				want: `[{"root":"aws_s3_bucket","steps":[{"kind":"attr","name":"this"},{"kind":"splat"}],"within":[],"filter":false}]`,
			},
			"添字の中の補間": {
				src: `aws_s3_bucket.this["${module.m.x}"]`,
				want: `[{"root":"aws_s3_bucket","steps":[{"kind":"attr","name":"this"},{"kind":"index","index":{"form":"other"}}],"within":[],"filter":false},
					{"root":"module","steps":[{"kind":"attr","name":"m"},{"kind":"attr","name":"x"}],"within":[{"ref":0,"step":1}],"filter":false}]`,
			},
			"補間の中の添字": {
				src: `"x-${aws_s3_bucket.this[module.m.x]}"`,
				want: `[{"root":"aws_s3_bucket","steps":[{"kind":"attr","name":"this"},{"kind":"index","index":{"form":"other"}}],"within":[],"filter":false},
					{"root":"module","steps":[{"kind":"attr","name":"m"},{"kind":"attr","name":"x"}],"within":[{"ref":0,"step":1}],"filter":false}]`,
			},
			"添字の中の for の collection": {
				src: `aws_s3_bucket.this[[for k in module.m.keys : k][0]]`,
				want: `[{"root":"aws_s3_bucket","steps":[{"kind":"attr","name":"this"},{"kind":"index","index":{"form":"other"}}],"within":[],"filter":false},
					{"root":"module","steps":[{"kind":"attr","name":"m"},{"kind":"attr","name":"keys"}],"within":[{"ref":0,"step":1}],"filter":false}]`,
			},
			"for の collection の中の添字": {
				src: `[for b in aws_s3_bucket.this[module.m.x] : b.id]`,
				want: `[{"root":"aws_s3_bucket","steps":[{"kind":"attr","name":"this"},{"kind":"index","index":{"form":"other"}}],"within":[],"filter":false},
					{"root":"module","steps":[{"kind":"attr","name":"m"},{"kind":"attr","name":"x"}],"within":[{"ref":0,"step":1}],"filter":false}]`,
			},
			"テンプレートのリテラルの [ は添字にならない": {
				src: `"${aws_s3_bucket.this.id}-x[${module.x.id}]"`,
				want: `[{"root":"aws_s3_bucket","steps":[{"kind":"attr","name":"this"},{"kind":"attr","name":"id"}],"within":[],"filter":false},
					{"root":"module","steps":[{"kind":"attr","name":"x"},{"kind":"attr","name":"id"}],"within":[],"filter":false}]`,
			},
			"祖先2段の入れ子の添字": {
				src: `aws_s3_bucket.this[module.m.list[var.i]]`,
				want: `[{"root":"aws_s3_bucket","steps":[{"kind":"attr","name":"this"},{"kind":"index","index":{"form":"other"}}],"within":[],"filter":false},
					{"root":"module","steps":[{"kind":"attr","name":"m"},{"kind":"attr","name":"list"},{"kind":"index","index":{"form":"other"}}],"within":[{"ref":0,"step":1}],"filter":false},
					{"root":"var","steps":[{"kind":"attr","name":"i"}],"within":[{"ref":0,"step":1},{"ref":1,"step":2}],"filter":false}]`,
			},
		}
		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				assert.JSONEq(t, tt.want, refsJSON(t, tt.src))
			})
		}
	})

	t.Run("for の反復変数は、その内側でだけ参照から外れる", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			src  string
			want string
		}{
			"入れ子の for の同名の反復変数": {
				src: `[for k, v in var.m : [for k in v : k if k != local.z]]`,
				want: `[{"root":"var","steps":[{"kind":"attr","name":"m"}],"within":[],"filter":false},
					{"root":"local","steps":[{"kind":"attr","name":"z"}],"within":[],"filter":true}]`,
			},
			"予約語と同名の反復変数でも collection は外のスコープ": {
				src:  `[for var in var.list : var]`,
				want: `[{"root":"var","steps":[{"kind":"attr","name":"list"}],"within":[],"filter":false}]`,
			},
			"for の外では同名の参照を数える": {
				src: `concat([for x in var.l : x], [x.y])`,
				want: `[{"root":"var","steps":[{"kind":"attr","name":"l"}],"within":[],"filter":false},
					{"root":"x","steps":[{"kind":"attr","name":"y"}],"within":[],"filter":false}]`,
			},
			"オブジェクトの for のキーと条件": {
				src: `{for k, v in var.m : "${k}-${local.p}" => v if v != local.z}`,
				want: `[{"root":"var","steps":[{"kind":"attr","name":"m"}],"within":[],"filter":false},
					{"root":"local","steps":[{"kind":"attr","name":"p"}],"within":[],"filter":false},
					{"root":"local","steps":[{"kind":"attr","name":"z"}],"within":[],"filter":true}]`,
			},
			"テンプレートの for の指示子": {
				src: `"%{ for x in var.l }${x}${local.y}%{ endfor }"`,
				want: `[{"root":"var","steps":[{"kind":"attr","name":"l"}],"within":[],"filter":false},
					{"root":"local","steps":[{"kind":"attr","name":"y"}],"within":[],"filter":false}]`,
			},
		}
		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				assert.JSONEq(t, tt.want, refsJSON(t, tt.src))
			})
		}
	})

	t.Run("for の条件の中の参照だけが filter を持つ", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			src  string
			want string
		}{
			"条件の中の関数の引数": {
				src: `{for k, a in aws_s3_bucket.this : k => a.id if !contains(var.skip, k)}`,
				want: `[{"root":"aws_s3_bucket","steps":[{"kind":"attr","name":"this"}],"within":[],"filter":false},
					{"root":"var","steps":[{"kind":"attr","name":"skip"}],"within":[],"filter":true}]`,
			},
			"条件の中の for は collection も値も filter": {
				src: `[for a in var.l : a if contains([for x in local.m : x.id], a)]`,
				want: `[{"root":"var","steps":[{"kind":"attr","name":"l"}],"within":[],"filter":false},
					{"root":"local","steps":[{"kind":"attr","name":"m"}],"within":[],"filter":true}]`,
			},
			"値の中の for の条件は、外の値を filter にしない": {
				src: `[for a in var.l : [for x in a : local.v if x != local.c]]`,
				want: `[{"root":"var","steps":[{"kind":"attr","name":"l"}],"within":[],"filter":false},
					{"root":"local","steps":[{"kind":"attr","name":"v"}],"within":[],"filter":false},
					{"root":"local","steps":[{"kind":"attr","name":"c"}],"within":[],"filter":true}]`,
			},
			"条件の中の添字の参照": {
				src: `[for a in var.l : a if aws_s3_bucket.this[local.k].id != a]`,
				want: `[{"root":"var","steps":[{"kind":"attr","name":"l"}],"within":[],"filter":false},
					{"root":"aws_s3_bucket","steps":[{"kind":"attr","name":"this"},{"kind":"index","index":{"form":"other"}},{"kind":"attr","name":"id"}],"within":[],"filter":true},
					{"root":"local","steps":[{"kind":"attr","name":"k"}],"within":[{"ref":1,"step":1}],"filter":true}]`,
			},
		}
		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				assert.JSONEq(t, tt.want, refsJSON(t, tt.src))
			})
		}
	})

	t.Run("オブジェクトのキーは、括弧の無い1語だけがリテラル", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			src  string
			want string
		}{
			"括弧の無い1語":        {src: `{ a = 1 }`, want: `[]`},
			"予約語と同名の括弧の無い1語": {src: `{ var = 1 }`, want: `[]`},
			"括弧つきのキー":        {src: `{ (module.m.x) = 1 }`, want: `[{"root":"module","steps":[{"kind":"attr","name":"m"},{"kind":"attr","name":"x"}],"within":[],"filter":false}]`},
			"括弧つきの1語":        {src: `{ (local.k) = 1 }`, want: `[{"root":"local","steps":[{"kind":"attr","name":"k"}],"within":[],"filter":false}]`},
			"括弧の無い複数段":       {src: `{ b.c = 2 }`, want: `[{"root":"b","steps":[{"kind":"attr","name":"c"}],"within":[],"filter":false}]`},
			"文字列のキー":         {src: `{ "s" = var.v }`, want: `[{"root":"var","steps":[{"kind":"attr","name":"v"}],"within":[],"filter":false}]`},
		}
		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				assert.JSONEq(t, tt.want, refsJSON(t, tt.src))
			})
		}
	})

	t.Run("入れ子の式の中の参照を出す", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			src  string
			want string
		}{
			"深い入れ子のオブジェクト": {
				src: `"${aws_s3_bucket.this.id}${ {a = {b = {c = {d = module.x.id}}}} }"`,
				want: `[{"root":"aws_s3_bucket","steps":[{"kind":"attr","name":"this"},{"kind":"attr","name":"id"}],"within":[],"filter":false},
					{"root":"module","steps":[{"kind":"attr","name":"x"},{"kind":"attr","name":"id"}],"within":[],"filter":false}]`,
			},
			"テンプレートの中のテンプレート": {
				src: `"${aws_s3_bucket.this.id}${"${module.x.id}"}"`,
				want: `[{"root":"aws_s3_bucket","steps":[{"kind":"attr","name":"this"},{"kind":"attr","name":"id"}],"within":[],"filter":false},
					{"root":"module","steps":[{"kind":"attr","name":"x"},{"kind":"attr","name":"id"}],"within":[],"filter":false}]`,
			},
		}
		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				assert.JSONEq(t, tt.want, refsJSON(t, tt.src))
			})
		}
	})

	t.Run("参照の無い式は空の配列を返す", func(t *testing.T) {
		t.Parallel()

		tests := map[string]string{
			"文字列":         `"abc"`,
			"エスケープした補間":   `"$${var.x}"`,
			"true と null": `true ? null : false`,
			"単項演算子と数":     `-1`,
			"関数の呼び出しの引数がリテラル": `upper("a")`,
		}
		for name, src := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				assert.JSONEq(t, `[]`, refsJSON(t, src))
			})
		}
	})

	t.Run("単項演算子と二項演算子の中の参照を出す", func(t *testing.T) {
		t.Parallel()
		assert.JSONEq(t,
			`[{"root":"var","steps":[{"kind":"attr","name":"b"}],"within":[],"filter":false},{"root":"var","steps":[{"kind":"attr","name":"n"}],"within":[],"filter":false}]`,
			refsJSON(t, `!var.b || var.n > 1`))
	})

	t.Run("知らない式の型は errUnknownExpression", func(t *testing.T) {
		t.Parallel()
		_, err := walkExpr(&hclsyntax.FunctionCallExpr{Name: "f", Args: []hclsyntax.Expression{newUnknown()}})
		require.ErrorIs(t, err, errUnknownExpression)
	})
}

func Test_walker_walk(t *testing.T) {
	t.Parallel()

	t.Run("鎖の根の式は、参照の位置を返す", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			src  string
			want int
		}{
			"参照":                 {src: `var.x`, want: 0},
			"参照に続く属性と添字":         {src: `aws_s3_bucket.this[var.k].id`, want: 0},
			"関数の呼び出しは鎖でない":       {src: `f(var.x)`, want: -1},
			"括弧は鎖でない":            {src: `(var.x)`, want: -1},
			"splat は鎖を Each へ渡す": {src: `var.x[*].y`, want: -1},
			"リテラル":               {src: `1`, want: -1},
		}
		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				w := &walker{}
				got, err := w.walk(parseExpr(t, tt.src), scope{})
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			})
		}
	})

	t.Run("スコープの名前を根に持つ参照は数えない", func(t *testing.T) {
		t.Parallel()
		w := &walker{}
		got, err := w.walk(parseExpr(t, `each.value`), scope{locals: map[string]bool{"each": true}})
		require.NoError(t, err)
		assert.Equal(t, -1, got)
		assert.Empty(t, w.refs)
		assert.Empty(t, w.ranges)
	})

	t.Run("渡された within を参照へ写す", func(t *testing.T) {
		t.Parallel()
		w := &walker{}
		outer := []within{{Ref: 3, Step: 1}}
		_, err := w.walk(parseExpr(t, `var.x`), scope{within: outer})
		require.NoError(t, err)
		assert.Equal(t, []ref{{Root: "var", Steps: []step{{Kind: stepAttr, Name: "x"}}, Within: []within{{Ref: 3, Step: 1}}}}, w.refs)
		outer[0].Ref = 9
		assert.Equal(t, 3, w.refs[0].Within[0].Ref, "参照の within が呼び出し側の配列を共有している")
	})

	t.Run("知らない式は、どの位置から現れても errUnknownExpression", func(t *testing.T) {
		t.Parallel()

		literal := &hclsyntax.LiteralValueExpr{Val: cty.True}
		badTraversal := &hclsyntax.ScopeTraversalExpr{Traversal: hcl.Traversal{hcl.TraverseRoot{Name: "a"}, hcl.TraverseRoot{Name: "b"}}}
		tests := map[string]hclsyntax.Expression{
			"埋め込みで作った知らない型":    newUnknown(),
			"ExprSyntaxError":  &hclsyntax.ExprSyntaxError{Placeholder: cty.DynamicVal},
			"参照の知らない段":         badTraversal,
			"相対の段の source":     &hclsyntax.RelativeTraversalExpr{Source: newUnknown(), Traversal: hcl.Traversal{hcl.TraverseAttr{Name: "a"}}},
			"相対の段の知らない段":       &hclsyntax.RelativeTraversalExpr{Source: scopeTraversal("var", "x"), Traversal: hcl.Traversal{hcl.TraverseRoot{Name: "a"}}},
			"添字の collection":   &hclsyntax.IndexExpr{Collection: newUnknown(), Key: literal},
			"添字の key":          &hclsyntax.IndexExpr{Collection: scopeTraversal("var", "x"), Key: newUnknown()},
			"splat の source":   &hclsyntax.SplatExpr{Source: newUnknown(), Each: &hclsyntax.AnonSymbolExpr{}},
			"splat の Each":     &hclsyntax.SplatExpr{Source: scopeTraversal("var", "x"), Each: newUnknown()},
			"関数の引数":            &hclsyntax.FunctionCallExpr{Name: "f", Args: []hclsyntax.Expression{newUnknown()}},
			"条件式":              &hclsyntax.ConditionalExpr{Condition: literal, TrueResult: literal, FalseResult: newUnknown()},
			"二項演算子":            &hclsyntax.BinaryOpExpr{LHS: literal, RHS: newUnknown()},
			"単項演算子":            &hclsyntax.UnaryOpExpr{Val: newUnknown()},
			"タプル":              &hclsyntax.TupleConsExpr{Exprs: []hclsyntax.Expression{newUnknown()}},
			"オブジェクトの値":         &hclsyntax.ObjectConsExpr{Items: []hclsyntax.ObjectConsItem{{KeyExpr: &hclsyntax.ObjectConsKeyExpr{Wrapped: scopeTraversal("a")}, ValueExpr: newUnknown()}}},
			"オブジェクトの式のキー":      &hclsyntax.ObjectConsKeyExpr{Wrapped: newUnknown(), ForceNonLiteral: true},
			"テンプレート":           &hclsyntax.TemplateExpr{Parts: []hclsyntax.Expression{newUnknown()}},
			"テンプレートの包み":        &hclsyntax.TemplateWrapExpr{Wrapped: newUnknown()},
			"テンプレートの結合":        &hclsyntax.TemplateJoinExpr{Tuple: newUnknown()},
			"括弧":               &hclsyntax.ParenthesesExpr{Expression: newUnknown()},
			"for の collection": &hclsyntax.ForExpr{ValVar: "v", CollExpr: newUnknown(), ValExpr: literal},
			"for のキー":          &hclsyntax.ForExpr{ValVar: "v", CollExpr: literal, KeyExpr: newUnknown(), ValExpr: literal},
			"for の値":           &hclsyntax.ForExpr{ValVar: "v", CollExpr: literal, ValExpr: newUnknown()},
			"for の条件":          &hclsyntax.ForExpr{ValVar: "v", CollExpr: literal, ValExpr: literal, CondExpr: newUnknown()},
		}
		for name, e := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				w := &walker{}
				_, err := w.walk(e, scope{})
				require.ErrorIs(t, err, errUnknownExpression)
			})
		}
	})
}

func Test_stepsOf(t *testing.T) {
	t.Parallel()

	t.Run("属性・添字・splat を段の3種類へ写す", func(t *testing.T) {
		t.Parallel()
		got, err := stepsOf(hcl.Traversal{
			hcl.TraverseAttr{Name: "a"},
			hcl.TraverseIndex{Key: cty.NumberIntVal(0)},
			hcl.TraverseSplat{},
		})
		require.NoError(t, err)
		assert.Equal(t, []step{
			{Kind: stepAttr, Name: "a"},
			{Kind: stepIndex, Index: &indexForm{Form: indexLiteral, Value: json.Number("0")}},
			{Kind: stepSplat},
		}, got)
	})

	t.Run("段が無ければ空の配列", func(t *testing.T) {
		t.Parallel()
		got, err := stepsOf(nil)
		require.NoError(t, err)
		assert.Equal(t, []step{}, got)
	})

	t.Run("途中の根は errUnknownExpression", func(t *testing.T) {
		t.Parallel()
		_, err := stepsOf(hcl.Traversal{hcl.TraverseAttr{Name: "a"}, hcl.TraverseRoot{Name: "b"}})
		require.ErrorIs(t, err, errUnknownExpression)
	})
}

func Test_indexFormOf(t *testing.T) {
	t.Parallel()

	t.Run("添字の式を形へ写す", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			src  string
			want *indexForm
		}{
			"each.key":                 {src: `each.key`, want: &indexForm{Form: indexEachKey}},
			"each.value":               {src: `each.value`, want: &indexForm{Form: indexEachValue}},
			"count.index":              {src: `count.index`, want: &indexForm{Form: indexCountIndex}},
			"段が続く each.value は other":  {src: `each.value.id`, want: &indexForm{Form: indexOther}},
			"根だけの each は other":        {src: `each`, want: &indexForm{Form: indexOther}},
			"each の別の属性は other":        {src: `each.other`, want: &indexForm{Form: indexOther}},
			"each の添字は other":          {src: `each["key"]`, want: &indexForm{Form: indexOther}},
			"count の別の属性は other":       {src: `count.other`, want: &indexForm{Form: indexOther}},
			"添字の段が続く each.key は other": {src: `each.key[0]`, want: &indexForm{Form: indexOther}},
			"関数の中の each.key は other":   {src: `upper(each.key)`, want: &indexForm{Form: indexOther}},
			"ほかの参照は other":             {src: `local.k`, want: &indexForm{Form: indexOther}},
			"補間で包んだ each.key は other":  {src: `"${each.key}"`, want: &indexForm{Form: indexOther}},
			"数のリテラル":                   {src: `1`, want: &indexForm{Form: indexLiteral, Value: json.Number("1")}},
			"小数のリテラル":                  {src: `1.5`, want: &indexForm{Form: indexLiteral, Value: json.Number("1.5")}},
			"補間の無い文字列":                 {src: `"a"`, want: &indexForm{Form: indexLiteral, Value: "a"}},
			"真偽値は other":               {src: `true`, want: &indexForm{Form: indexOther}},
			"null は other":             {src: `null`, want: &indexForm{Form: indexOther}},
		}
		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				assert.Equal(t, tt.want, indexFormOf(parseExpr(t, tt.src), nil))
			})
		}
	})

	t.Run("添字の参照の段に形が載る", func(t *testing.T) {
		t.Parallel()
		assert.JSONEq(t, `[{"root":"aws_s3_bucket","steps":[{"kind":"attr","name":"this"},{"kind":"index","index":{"form":"each_key"}},{"kind":"attr","name":"id"}],"within":[],"filter":false},
			{"root":"each","steps":[{"kind":"attr","name":"key"}],"within":[{"ref":0,"step":1}],"filter":false}]`, refsJSON(t, `aws_s3_bucket.this[each.key].id`))
		assert.JSONEq(t, `[{"root":"aws_s3_bucket","steps":[{"kind":"attr","name":"this"},{"kind":"index","index":{"form":"literal","value":"a"}}],"within":[],"filter":false}]`,
			refsJSON(t, `aws_s3_bucket.this["a"]`))
	})

	t.Run("for の反復変数が each か count を覆うなら other", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, &indexForm{Form: indexOther}, indexFormOf(parseExpr(t, `each.key`), map[string]bool{"each": true}))
		assert.Equal(t, &indexForm{Form: indexOther}, indexFormOf(parseExpr(t, `count.index`), map[string]bool{"count": true}))
		assert.JSONEq(t, `[{"root":"var","steps":[{"kind":"attr","name":"x"}],"within":[],"filter":false},
			{"root":"aws_s3_bucket","steps":[{"kind":"attr","name":"this"},{"kind":"index","index":{"form":"other"}}],"within":[],"filter":false}]`,
			refsJSON(t, `[for each in var.x : aws_s3_bucket.this[each.key]]`))
		assert.JSONEq(t, `[{"root":"var","steps":[{"kind":"attr","name":"x"}],"within":[],"filter":false},
			{"root":"aws_s3_bucket","steps":[{"kind":"attr","name":"this"},{"kind":"index","index":{"form":"other"}}],"within":[],"filter":false}]`,
			refsJSON(t, `[for count in var.x : aws_s3_bucket.this[count.index]]`))
	})
}

func Test_literalForm(t *testing.T) {
	t.Parallel()

	t.Run("既知の数と文字列だけを literal にする", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			val  cty.Value
			want *indexForm
		}{
			"整数":    {val: cty.NumberIntVal(0), want: &indexForm{Form: indexLiteral, Value: json.Number("0")}},
			"文字列":   {val: cty.StringVal("0"), want: &indexForm{Form: indexLiteral, Value: "0"}},
			"空の文字列": {val: cty.StringVal(""), want: &indexForm{Form: indexLiteral, Value: ""}},
			"null":  {val: cty.NullVal(cty.String), want: &indexForm{Form: indexOther}},
			"未知の値":  {val: cty.UnknownVal(cty.Number), want: &indexForm{Form: indexOther}},
			"真偽値":   {val: cty.True, want: &indexForm{Form: indexOther}},
		}
		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				assert.Equal(t, tt.want, literalForm(tt.val))
			})
		}
	})

	t.Run("空の文字列と 0 も JSON に value として出る", func(t *testing.T) {
		t.Parallel()
		raw, err := json.Marshal([]*indexForm{literalForm(cty.StringVal("")), literalForm(cty.NumberIntVal(0))})
		require.NoError(t, err)
		assert.JSONEq(t, `[{"form":"literal","value":""},{"form":"literal","value":0}]`, string(raw))
	})
}

func Test_checkRefs(t *testing.T) {
	t.Parallel()

	ranges := func(e hclsyntax.Expression) []hcl.Range {
		var out []hcl.Range
		for _, tr := range hclsyntax.Variables(e) {
			out = append(out, tr.SourceRange())
		}

		return out
	}

	t.Run("Variables() と同じ多重集合なら通す", func(t *testing.T) {
		t.Parallel()
		e := parseExpr(t, `[var.x, var.x, local.y]`)
		got := ranges(e)
		got[0], got[2] = got[2], got[0]
		require.NoError(t, checkRefs(e, got))
	})

	t.Run("参照の無い式で、どちらも空なら通す", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, checkRefs(parseExpr(t, `"a"`), nil))
	})

	t.Run("食い違えば errRefsMismatch", func(t *testing.T) {
		t.Parallel()

		e := parseExpr(t, `[var.x, var.x, local.y]`)
		all := ranges(e)
		tests := map[string][]hcl.Range{
			"自前の歩きが1つ欠く":   all[:2],
			"自前の歩きが1つ多い":   append(append([]hcl.Range{}, all...), all[0]),
			"重複の数だけが違う":    {all[0], all[0], all[0]},
			"自前の歩きが何も出さない": nil,
		}
		for name, got := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				err := checkRefs(e, got)
				require.ErrorIs(t, err, errRefsMismatch)
				assert.Contains(t, err.Error(), "t.tf:1")
			})
		}
	})
}

func Test_walker_each(t *testing.T) {
	t.Parallel()

	t.Run("式を順に歩き、nil の要素を飛ばす", func(t *testing.T) {
		t.Parallel()
		w := &walker{}
		require.NoError(t, w.each(scope{}, parseExpr(t, `var.a`), nil, parseExpr(t, `var.b`)))
		require.Len(t, w.refs, 2)
		assert.Equal(t, "var", w.refs[0].Root)
		assert.Equal(t, []step{{Kind: stepAttr, Name: "a"}}, w.refs[0].Steps)
		assert.Equal(t, []step{{Kind: stepAttr, Name: "b"}}, w.refs[1].Steps)
	})

	t.Run("鎖の根の式でも、後続の段を参照に付けない", func(t *testing.T) {
		t.Parallel()
		w := &walker{}
		require.NoError(t, w.each(scope{}, parseExpr(t, `var.a`)))
		require.NoError(t, w.each(scope{}, parseExpr(t, `var.b[0]`)))
		assert.Equal(t, []step{{Kind: stepAttr, Name: "a"}}, w.refs[0].Steps)
	})

	t.Run("最初の誤りで止まる", func(t *testing.T) {
		t.Parallel()
		w := &walker{}
		err := w.each(scope{}, newUnknown(), parseExpr(t, `var.b`))
		require.ErrorIs(t, err, errUnknownExpression)
		assert.Empty(t, w.refs)
	})
}

package main

import (
	"encoding/json"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_foldDynamic(t *testing.T) {
	t.Parallel()

	// 畳んだ path を層の検査が同じ引数として読むことは、conftest/hcl/layer/tree_test.rego の dynamic_parent_* が固定する。
	t.Run("期待の並びを返す", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			in   string
			want string
		}{
			"1段":                {in: "dynamic.a.content.x", want: "a.x"},
			"2段":                {in: "dynamic.a.content.dynamic.b.content.x", want: "a.b.x"},
			"3段":                {in: "dynamic.a.content.dynamic.b.content.dynamic.c.content.x", want: "a.b.c.x"},
			"5段":                {in: "dynamic.a.content.dynamic.b.content.dynamic.c.content.dynamic.d.content.dynamic.e.content.x", want: "a.b.c.d.e.x"},
			"前置きの後の dynamic":    {in: "s.dynamic.a.content.x", want: "s.a.x"},
			"content で終わる":      {in: "dynamic.a.content", want: "a"},
			"for_each は畳まない":    {in: "dynamic.a.for_each", want: "dynamic.a.for_each"},
			"畳んだ後の for_each":    {in: "dynamic.a.content.dynamic.b.for_each", want: "a.dynamic.b.for_each"},
			"ラベルに . を含む":        {in: "dynamic.a.b.content.x", want: "dynamic.a.b.content.x"},
			"段の途中の dynamic の字面": {in: "xdynamic.a.content.x", want: "xdynamic.a.content.x"},
			"dynamic の無い並び":     {in: "bucket", want: "bucket"},
		}
		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				assert.Equal(t, tt.want, foldDynamic(tt.in))
			})
		}
	})
}

func Test_attributesOf(t *testing.T) {
	t.Parallel()

	t.Run("map の属性をソースの位置の順に並べる", func(t *testing.T) {
		t.Parallel()
		f, diags := hclsyntax.ParseConfig([]byte("x {\n  c = 1\n  a = 2\n  b = 3\n}\n"), "t.tf", hcl.InitialPos)
		require.Empty(t, diags)
		var names []string
		for _, a := range attributesOf(f.Body.(*hclsyntax.Body).Blocks[0].Body) {
			names = append(names, a.Name)
		}
		assert.Equal(t, []string{"c", "a", "b"}, names)
	})

	t.Run("属性が無ければ空", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, attributesOf(&hclsyntax.Body{}))
	})
}

func syntaxJSON(t *testing.T, name, src string) string {
	t.Helper()
	blocks, err := parseSyntaxFile(name, []byte(src))
	require.NoError(t, err)
	raw, err := json.Marshal(blocks)
	require.NoError(t, err)

	return string(raw)
}

func Test_parseConfigBody(t *testing.T) {
	t.Parallel()

	t.Run("最上位の block を返す", func(t *testing.T) {
		t.Parallel()
		body, err := parseConfigBody("m/main.tf", []byte("locals {}\nresource \"a\" \"b\" {}\n"))
		require.NoError(t, err)
		assert.Len(t, body.Blocks, 2)
	})

	t.Run("受け付けない入力", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			src     string
			want    error
			wantMsg []string
		}{
			"構文の誤り": {
				src: "locals {}\nresource \"a\" {\n", want: errSyntax,
				wantMsg: []string{"m/main.tf:2,", "Unclosed configuration block"},
			},
			"非 UTF-8": {src: "locals {\n  a = \"\xff\"\n}\n", want: errSyntax},
			"ファイルの直下の属性": {
				src: "locals {}\nx = 1\n", want: errUnsupportedInput,
				wantMsg: []string{"m/main.tf:2,", `ファイルの直下に属性 "x" があります`},
			},
			"ラベルが1個の resource":                 {src: "resource \"a\" {}\n", want: errUnsupportedInput},
			"ラベルが1個の data":                     {src: "data \"a\" {}\n", want: errUnsupportedInput},
			"check の中のラベルが1個の data":            {src: "check \"c\" {\n  data \"a\" {}\n}\n", want: errUnsupportedInput},
			"check の中の assert の後のラベルが1個の data": {src: "check \"c\" {\n  assert {\n    condition     = true\n    error_message = \"x\"\n  }\n  data \"a\" {}\n}\n", want: errUnsupportedInput},
		}
		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				_, err := parseConfigBody("m/main.tf", []byte(tt.src))
				require.ErrorIs(t, err, tt.want)
				for _, m := range tt.wantMsg {
					assert.Contains(t, err.Error(), m)
				}
			})
		}
	})
}

func Test_parseSyntaxFile(t *testing.T) {
	t.Parallel()

	t.Run("空の入れ子の block と null の属性も出す", func(t *testing.T) {
		t.Parallel()
		src := `resource "aws_iam_role" "r" {
	managed_policy_arns = null
	inline_policy {}
	ingress = []
}

terraform {
	cloud {}
	backend "s3" {
		key = "k"
	}
}
`
		want := `[
			{"file":"m/main.tf","line":1,"type":"resource","labels":["aws_iam_role","r"],
				"blocks":[{"path":"inline_policy","parent":-1,"line":3,"type":"inline_policy","labels":[],"dynamic":false}],
				"attributes":[
				{"path":"managed_policy_arns","parent":-1,"line":2,"expr":{"kind":"literal","value":null},"refs":[]},
				{"path":"ingress","parent":-1,"line":4,"expr":{"kind":"literal","value":[]},"refs":[]}
				]},
			{"file":"m/main.tf","line":7,"type":"terraform","labels":[],
				"blocks":[
				{"path":"cloud","parent":-1,"line":8,"type":"cloud","labels":[],"dynamic":false},
				{"path":"backend.s3","parent":-1,"line":9,"type":"backend","labels":["s3"],"dynamic":false}
				],
				"attributes":[{"path":"backend.s3.key","parent":1,"line":10,"expr":{"kind":"literal","value":"k"},"refs":[]}]}
		]`
		assert.JSONEq(t, want, syntaxJSON(t, "m/main.tf", src))
	})

	// 層の LY304 は、親の引数を書いたかを、空の block・null・[]・dynamic のどれでも数える。属性を持たない
	// dynamic の content も、畳んだ path の block として残す。
	t.Run("属性を持たない dynamic も、畳んだ path の block を出す", func(t *testing.T) {
		t.Parallel()
		src := "resource \"aws_iam_role\" \"r\" {\n  dynamic \"inline_policy\" {\n    for_each = []\n    content {}\n  }\n}\n"
		blocks, err := parseSyntaxFile("m/main.tf", []byte(src))
		require.NoError(t, err)
		require.Len(t, blocks, 1)
		var paths []string
		for _, n := range blocks[0].Blocks {
			paths = append(paths, n.Path)
		}
		assert.Equal(t, []string{"dynamic.inline_policy", "inline_policy"}, paths)
	})

	t.Run("dynamic を自身の block と、畳んだ content の block として出す", func(t *testing.T) {
		t.Parallel()
		src := `resource "aws_s3_bucket" "p" {
	dynamic "provisioner" {
		for_each = []
		content {}
	}
}
`
		want := `[{"file":"m/p.tf","line":1,"type":"resource","labels":["aws_s3_bucket","p"],
			"blocks":[
				{"path":"dynamic.provisioner","parent":-1,"line":2,"type":"dynamic","labels":["provisioner"],"dynamic":true},
				{"path":"provisioner","parent":0,"line":4,"type":"content","labels":[],"dynamic":false}
			],
			"attributes":[{"path":"dynamic.provisioner.for_each","parent":0,"line":3,"expr":{"kind":"literal","value":[]},"refs":[]}]}]`
		assert.JSONEq(t, want, syntaxJSON(t, "m/p.tf", src))
	})

	t.Run(".tftest.hcl の run と assert を読む", func(t *testing.T) {
		t.Parallel()
		src := "run \"ok\" {\n  command = plan\n  assert {\n    condition     = true\n    error_message = \"x\"\n  }\n}\n"
		want := `[{"file":"m/tests/a.tftest.hcl","line":1,"type":"run","labels":["ok"],
			"blocks":[{"path":"assert","parent":-1,"line":3,"type":"assert","labels":[],"dynamic":false}],
			"attributes":[
				{"path":"command","parent":-1,"line":2,"expr":{"kind":"traversal","root":"plan","steps":[]},"refs":[{"root":"plan","steps":[],"within":[],"filter":false}]},
				{"path":"assert.condition","parent":0,"line":4,"expr":{"kind":"literal","value":true},"refs":[]},
				{"path":"assert.error_message","parent":0,"line":5,"expr":{"kind":"literal","value":"x"},"refs":[]}
			]}]`
		assert.JSONEq(t, want, syntaxJSON(t, "m/tests/a.tftest.hcl", src))
	})

	t.Run("BOM と CRLF を受理する", func(t *testing.T) {
		t.Parallel()
		want := `[{"file":"m/a.tf","line":1,"type":"locals","labels":[],"blocks":[],"attributes":[{"path":"a","parent":-1,"line":2,"expr":{"kind":"literal","value":1},"refs":[]}]}]`
		assert.JSONEq(t, want, syntaxJSON(t, "m/a.tf", "\ufefflocals {\r\n  a = 1\r\n}\r\n"))
	})

	t.Run("空のファイルとコメントだけのファイルは空の配列", func(t *testing.T) {
		t.Parallel()
		assert.JSONEq(t, `[]`, syntaxJSON(t, "m/a.tf", ""))
		assert.JSONEq(t, `[]`, syntaxJSON(t, "m/a.tf", "# c\n/* d */\n"))
	})

	t.Run("受理しない入力は parseConfigBody と同じ誤り", func(t *testing.T) {
		t.Parallel()
		_, err := parseSyntaxFile("m/a.tf", []byte("resource \"a\" {\n"))
		require.ErrorIs(t, err, errSyntax)
		_, err = parseSyntaxFile("m/a.tf", []byte("x = 1\n"))
		require.ErrorIs(t, err, errUnsupportedInput)
	})

	t.Run("式の構文の誤りは errSyntax", func(t *testing.T) {
		t.Parallel()
		_, err := parseSyntaxFile("m/a.tf", []byte("locals {\n  a = var.x[*][\n}\n"))
		require.ErrorIs(t, err, errSyntax)
	})

	t.Run("ラベルの個数を Terraform が決めている block の誤りは errUnsupportedInput", func(t *testing.T) {
		t.Parallel()
		for _, src := range []string{
			"ephemeral \"a\" {}\n", "action \"a\" {}\n", "provider {}\n", "provider \"a\" \"b\" {}\n", "module {}\n", "module \"a\" \"b\" {}\n",
		} {
			_, err := parseSyntaxFile("m/a.tf", []byte(src))
			require.ErrorIs(t, err, errUnsupportedInput, src)
		}
	})
}

func unknownBody() *hclsyntax.Body {
	inner := &hclsyntax.Body{Attributes: hclsyntax.Attributes{"a": {Name: "a", Expr: newUnknown()}}}

	return &hclsyntax.Body{Blocks: hclsyntax.Blocks{{Type: "locals", Body: inner}}}
}

func Test_syntaxBlocks(t *testing.T) {
	t.Parallel()

	t.Run("最上位の block を読む", func(t *testing.T) {
		t.Parallel()
		got, err := syntaxBlocks("m/a.tf", &hclsyntax.Body{Blocks: hclsyntax.Blocks{{Type: "locals", Body: &hclsyntax.Body{}}}})
		require.NoError(t, err)
		assert.Equal(t, []synBlock{{File: "m/a.tf", Type: "locals", Labels: []string{}, Blocks: []nested{}, Attributes: []synAttr{}}}, got)
	})

	t.Run("属性の式を読めなければ errUnknownExpression を伝える", func(t *testing.T) {
		t.Parallel()
		_, err := syntaxBlocks("m/a.tf", unknownBody())
		require.ErrorIs(t, err, errUnknownExpression)
	})
}

func Test_syntaxBody(t *testing.T) {
	t.Parallel()

	t.Run("入れ子の block の親の位置と前置きを付けて深さ優先に並べる", func(t *testing.T) {
		t.Parallel()
		f, diags := hclsyntax.ParseConfig([]byte("x {\n  a = 1\n  n \"l\" {\n    m {\n      c = 2\n    }\n  }\n  b = 3\n}\n"), "t.tf", hcl.InitialPos)
		require.Empty(t, diags)
		sb := synBlock{Blocks: []nested{}, Attributes: []synAttr{}}
		require.NoError(t, syntaxBody(f.Body.(*hclsyntax.Body).Blocks[0].Body, []string{"p"}, -1, &sb))
		raw, err := json.Marshal(sb)
		require.NoError(t, err)
		assert.JSONEq(t, `{"file":"","line":0,"type":"","labels":null,
			"blocks":[
				{"path":"p.n.l","parent":-1,"line":3,"type":"n","labels":["l"],"dynamic":false},
				{"path":"p.n.l.m","parent":0,"line":4,"type":"m","labels":[],"dynamic":false}
			],
			"attributes":[
				{"path":"p.a","parent":-1,"line":2,"expr":{"kind":"literal","value":1},"refs":[]},
				{"path":"p.n.l.m.c","parent":1,"line":5,"expr":{"kind":"literal","value":2},"refs":[]},
				{"path":"p.b","parent":-1,"line":8,"expr":{"kind":"literal","value":3},"refs":[]}
			]}`, string(raw))
	})

	t.Run("dynamic の content の属性の path を畳む", func(t *testing.T) {
		t.Parallel()
		f, diags := hclsyntax.ParseConfig([]byte("x {\n  dynamic \"y\" {\n    content {\n      a = 1\n    }\n  }\n}\n"), "t.tf", hcl.InitialPos)
		require.Empty(t, diags)
		sb := synBlock{Blocks: []nested{}, Attributes: []synAttr{}}
		require.NoError(t, syntaxBody(f.Body.(*hclsyntax.Body).Blocks[0].Body, nil, -1, &sb))
		require.Len(t, sb.Attributes, 1)
		assert.Equal(t, "y.a", sb.Attributes[0].Path)
		assert.Equal(t, []string{"dynamic.y", "y"}, []string{sb.Blocks[0].Path, sb.Blocks[1].Path})
	})

	t.Run("属性の式を読めなければ errUnknownExpression", func(t *testing.T) {
		t.Parallel()
		body := &hclsyntax.Body{Attributes: hclsyntax.Attributes{"a": {Name: "a", Expr: newUnknown()}}}
		sb := synBlock{}
		require.ErrorIs(t, syntaxBody(body, nil, -1, &sb), errUnknownExpression)
	})

	t.Run("歩く型の外の式（AnonSymbolExpr）は errUnknownExpression", func(t *testing.T) {
		t.Parallel()
		body := &hclsyntax.Body{Attributes: hclsyntax.Attributes{"a": {Name: "a", Expr: &hclsyntax.AnonSymbolExpr{}}}}
		sb := synBlock{}
		require.ErrorIs(t, syntaxBody(body, nil, -1, &sb), errUnknownExpression)
	})

	t.Run("入れ子の block の中で読めなければ errUnknownExpression", func(t *testing.T) {
		t.Parallel()
		inner := &hclsyntax.Body{Attributes: hclsyntax.Attributes{"a": {Name: "a", Expr: newUnknown()}}}
		body := &hclsyntax.Body{Blocks: hclsyntax.Blocks{{Type: "n", Body: inner}}}
		sb := synBlock{}
		require.ErrorIs(t, syntaxBody(body, nil, -1, &sb), errUnknownExpression)
	})
}

func Test_checkLabels(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		typ    string
		labels []string
		ok     bool
	}{
		"resource の2つ":  {"resource", []string{"a", "b"}, true},
		"resource の1つ":  {"resource", []string{"a"}, false},
		"provider の1つ":  {"provider", []string{"a"}, true},
		"module の2つ":    {"module", []string{"a", "b"}, false},
		"決まりの無い種類は問わない": {"locals", []string{"x", "y", "z"}, true},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := checkLabels(&hclsyntax.Block{Type: tt.typ, Labels: tt.labels})
			if tt.ok {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, errUnsupportedInput)
			}
		})
	}

	t.Run("check の中の正しい data は通る", func(t *testing.T) {
		t.Parallel()
		_, err := parseConfigBody("m/a.tf", []byte("check \"c\" {\n  data \"a\" \"b\" {}\n}\n"))
		require.NoError(t, err)
	})
}

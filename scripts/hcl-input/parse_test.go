package main

import (
	"encoding/json"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func blocksJSON(t *testing.T, src string) string {
	t.Helper()
	blocks, err := parseFile("modules/a/main.tf", []byte(src))
	require.NoError(t, err)
	raw, err := json.Marshal(blocks)
	require.NoError(t, err)

	return string(raw)
}

func Test_parseFile(t *testing.T) {
	t.Parallel()

	t.Run("全種類の block を、ソースの順に属性を並べて出す", func(t *testing.T) {
		t.Parallel()
		src := `terraform {
	required_version = ">= 1.0"
}

resource "aws_s3_bucket_policy" "this" {
	bucket = aws_s3_bucket.this.id
	provisioner "local-exec" {
		command = "echo ${var.x}"
	}
	policy = data.aws_iam_policy_document.p.json
	lifecycle {
		ignore_changes = [tags]
	}
}

locals {
	all = []
	b   = local.all
}

module "m" {
	source = "../m"
}

data "aws_iam_policy_document" "p" {}
`
		want := `[
			{"file":"modules/a/main.tf","kind":"terraform","labels":[],"attributes":[{"path":"required_version","refs":[]}]},
			{"file":"modules/a/main.tf","kind":"resource","labels":["aws_s3_bucket_policy","this"],"attributes":[
				{"path":"bucket","refs":[{"root":"aws_s3_bucket","steps":[{"kind":"attr","name":"this"},{"kind":"attr","name":"id"}],"within":[]}]},
				{"path":"provisioner.local-exec.command","refs":[{"root":"var","steps":[{"kind":"attr","name":"x"}],"within":[]}]},
				{"path":"policy","refs":[{"root":"data","steps":[{"kind":"attr","name":"aws_iam_policy_document"},{"kind":"attr","name":"p"},{"kind":"attr","name":"json"}],"within":[]}]},
				{"path":"lifecycle.ignore_changes","refs":[{"root":"tags","steps":[],"within":[]}]}
			]},
			{"file":"modules/a/main.tf","kind":"locals","labels":[],"attributes":[
				{"path":"all","refs":[]},
				{"path":"b","refs":[{"root":"local","steps":[{"kind":"attr","name":"all"}],"within":[]}]}
			]},
			{"file":"modules/a/main.tf","kind":"module","labels":["m"],"attributes":[{"path":"source","refs":[]}]},
			{"file":"modules/a/main.tf","kind":"data","labels":["aws_iam_policy_document","p"],"attributes":[]}
		]`
		assert.JSONEq(t, want, blocksJSON(t, src))
	})

	t.Run("dynamic を fold_dynamic と同じ並びに畳み、for_each は畳まない", func(t *testing.T) {
		t.Parallel()
		src := `resource "aws_s3_bucket_lifecycle_configuration" "this" {
	dynamic "rule" {
		for_each = var.rules
		content {
			dynamic "filter" {
				for_each = rule.value.filters
				content {
					dynamic "and" {
						for_each = filter.value
						content {
							prefix = and.value
						}
					}
				}
			}
		}
	}
	dynamic "a.b" {
		content {
			x = 1
		}
	}
}
`
		want := `[{"file":"modules/a/main.tf","kind":"resource","labels":["aws_s3_bucket_lifecycle_configuration","this"],"attributes":[
			{"path":"dynamic.rule.for_each","refs":[{"root":"var","steps":[{"kind":"attr","name":"rules"}],"within":[]}]},
			{"path":"rule.dynamic.filter.for_each","refs":[{"root":"rule","steps":[{"kind":"attr","name":"value"},{"kind":"attr","name":"filters"}],"within":[]}]},
			{"path":"rule.filter.dynamic.and.for_each","refs":[{"root":"filter","steps":[{"kind":"attr","name":"value"}],"within":[]}]},
			{"path":"rule.filter.and.prefix","refs":[{"root":"and","steps":[{"kind":"attr","name":"value"}],"within":[]}]},
			{"path":"dynamic.a.b.content.x","refs":[]}
		]}]`
		assert.JSONEq(t, want, blocksJSON(t, src))
	})

	t.Run("同じ path の属性と block を重複のまま残す", func(t *testing.T) {
		t.Parallel()
		src := "resource \"aws_x\" \"y\" {\n  s { v = var.a }\n  s { v = var.b }\n}\n"
		want := `[{"file":"modules/a/main.tf","kind":"resource","labels":["aws_x","y"],"attributes":[
			{"path":"s.v","refs":[{"root":"var","steps":[{"kind":"attr","name":"a"}],"within":[]}]},
			{"path":"s.v","refs":[{"root":"var","steps":[{"kind":"attr","name":"b"}],"within":[]}]}
		]}]`
		assert.JSONEq(t, want, blocksJSON(t, src))
	})

	t.Run("BOM と CRLF を受理する", func(t *testing.T) {
		t.Parallel()
		src := "\ufefflocals {\r\n  a = var.x\r\n}\r\n"
		want := `[{"file":"modules/a/main.tf","kind":"locals","labels":[],"attributes":[{"path":"a","refs":[{"root":"var","steps":[{"kind":"attr","name":"x"}],"within":[]}]}]}]`
		assert.JSONEq(t, want, blocksJSON(t, src))
	})

	t.Run("block の無いファイルは空の配列", func(t *testing.T) {
		t.Parallel()
		assert.JSONEq(t, `[]`, blocksJSON(t, "# comment only\n"))
	})

	t.Run("構文の誤りは errSyntax で、診断の全文を添える", func(t *testing.T) {
		t.Parallel()
		_, err := parseFile("modules/a/broken.tf", []byte("resource \"aws_s3_bucket\" {\n"))
		require.ErrorIs(t, err, errSyntax)
		assert.Contains(t, err.Error(), "modules/a/broken.tf")
		assert.Contains(t, err.Error(), "Unclosed configuration block")
	})

	t.Run("Terraform が受け付けない形は errUnsupportedInput", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			src  string
			want string
		}{
			"ファイルの直下の属性":       {src: "locals {}\nx = 1\n", want: "modules/a/main.tf:2"},
			"ラベルが1個の resource": {src: "resource \"aws_s3_bucket\" {}\n", want: "modules/a/main.tf:1"},
			"ラベルが3個の resource": {src: "resource \"aws_s3_bucket\" \"a\" \"b\" {}\n", want: "modules/a/main.tf:1"},
			"ラベルが無い resource":  {src: "locals {}\nresource {}\n", want: "modules/a/main.tf:2"},
			"ラベルが1個の data":     {src: "data \"aws_iam_policy_document\" {}\n", want: "modules/a/main.tf:1"},
		}
		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				_, err := parseFile("modules/a/main.tf", []byte(tt.src))
				require.ErrorIs(t, err, errUnsupportedInput)
				assert.Contains(t, err.Error(), tt.want)
			})
		}
	})
}

func Test_bodyAttributes(t *testing.T) {
	t.Parallel()

	t.Run("入れ子の block の段を前置きに足して深さ優先に並べる", func(t *testing.T) {
		t.Parallel()
		f, diags := hclsyntax.ParseConfig([]byte("x {\n  b = 1\n  n \"l\" {\n    c = 2\n  }\n  a = 3\n}\n"), "t.tf", hcl.InitialPos)
		require.Empty(t, diags)
		body := f.Body.(*hclsyntax.Body).Blocks[0].Body
		got, err := bodyAttributes(body, []string{"p"})
		require.NoError(t, err)
		assert.Equal(t, []attribute{
			{Path: "p.b", Refs: []ref{}},
			{Path: "p.n.l.c", Refs: []ref{}},
			{Path: "p.a", Refs: []ref{}},
		}, got)
	})

	t.Run("空の body は空の配列", func(t *testing.T) {
		t.Parallel()
		got, err := bodyAttributes(&hclsyntax.Body{}, nil)
		require.NoError(t, err)
		assert.Equal(t, []attribute{}, got)
	})

	t.Run("属性の式を読めなければ errUnknownExpression", func(t *testing.T) {
		t.Parallel()
		body := &hclsyntax.Body{Attributes: hclsyntax.Attributes{"a": {Name: "a", Expr: newUnknown()}}}
		_, err := bodyAttributes(body, nil)
		require.ErrorIs(t, err, errUnknownExpression)
	})

	t.Run("入れ子の block の属性の式を読めなければ errUnknownExpression", func(t *testing.T) {
		t.Parallel()
		inner := &hclsyntax.Body{Attributes: hclsyntax.Attributes{"a": {Name: "a", Expr: newUnknown()}}}
		body := &hclsyntax.Body{Blocks: hclsyntax.Blocks{{Type: "n", Body: inner}}}
		_, err := bodyAttributes(body, nil)
		require.ErrorIs(t, err, errUnknownExpression)
	})
}

func Test_foldDynamic(t *testing.T) {
	t.Parallel()

	// Rego の fold_dynamic と同じ並びであることは、layer-test.mk の dynamic-parent-* のケースが固定する。
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
			src  string
			want error
		}{
			"構文の誤り":                            {src: "resource \"a\" {\n", want: errSyntax},
			"非 UTF-8":                          {src: "locals {\n  a = \"\xff\"\n}\n", want: errSyntax},
			"ファイルの直下の属性":                       {src: "x = 1\n", want: errUnsupportedInput},
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
				{"path":"command","parent":-1,"line":2,"expr":{"kind":"traversal","root":"plan","steps":[]},"refs":[{"root":"plan","steps":[],"within":[]}]},
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

func Test_blocksOf(t *testing.T) {
	t.Parallel()

	t.Run("最上位の block を読む", func(t *testing.T) {
		t.Parallel()
		got, err := blocksOf("m/a.tf", &hclsyntax.Body{Blocks: hclsyntax.Blocks{{Type: "locals", Body: &hclsyntax.Body{}}}})
		require.NoError(t, err)
		assert.Equal(t, []block{{File: "m/a.tf", Kind: "locals", Labels: []string{}, Attributes: []attribute{}}}, got)
	})

	t.Run("属性の式を読めなければ errUnknownExpression を伝える", func(t *testing.T) {
		t.Parallel()
		_, err := blocksOf("m/a.tf", unknownBody())
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

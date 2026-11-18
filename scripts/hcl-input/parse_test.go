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

	t.Run("conftest/hcl/lib/hcl.rego の fold_dynamic と同じ並びを返す", func(t *testing.T) {
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

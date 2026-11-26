package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zclconf/go-cty/cty"
)

func Test_parseHCL(t *testing.T) {
	t.Parallel()

	t.Run("構文の誤りが無ければ body を返す", func(t *testing.T) {
		t.Parallel()
		body, err := parseHCL(writeFile(t, "a.hcl", lockHCL))
		require.NoError(t, err)
		assert.Len(t, body.Blocks, 1)
	})

	t.Run("ファイルが無ければ読めない失敗にする", func(t *testing.T) {
		t.Parallel()
		_, err := parseHCL(filepath.Join(t.TempDir(), "none.hcl"))
		require.ErrorIs(t, err, errRead)
	})

	t.Run("構文の誤りは失敗にする", func(t *testing.T) {
		t.Parallel()
		_, err := parseHCL(writeFile(t, "a.hcl", "variables {"))
		require.ErrorIs(t, err, errSyntax)
	})
}

func Test_readLockedVersion(t *testing.T) {
	t.Parallel()

	t.Run("正本の lock の hashicorp/aws の版を返す", func(t *testing.T) {
		t.Parallel()
		got, err := readLockedVersion(writeFile(t, ".terraform.lock.hcl", lockHCL))
		require.NoError(t, err)
		assert.Equal(t, "6.73.0", got)
	})

	t.Run("ファイルが無ければ読めない失敗にする", func(t *testing.T) {
		t.Parallel()
		_, err := readLockedVersion(filepath.Join(t.TempDir(), ".terraform.lock.hcl"))
		require.ErrorIs(t, err, errRead)
	})

	t.Run("解釈できない lock は errLocked", func(t *testing.T) {
		t.Parallel()
		_, err := readLockedVersion(writeFile(t, ".terraform.lock.hcl", "# x\n"))
		require.ErrorIs(t, err, errLocked)
	})

	t.Run("hashicorp/aws の block が2つある lock は errLocked", func(t *testing.T) {
		t.Parallel()
		_, err := readLockedVersion(writeFile(t, ".terraform.lock.hcl", lockHCL+lockHCL))
		require.ErrorIs(t, err, errLocked)
	})

	t.Run("hashicorp/aws が無い lock は errLocked", func(t *testing.T) {
		t.Parallel()
		_, err := readLockedVersion(writeFile(t, ".terraform.lock.hcl", strings.Replace(lockHCL, "hashicorp/aws", "hashicorp/random", 1)))
		require.ErrorIs(t, err, errLocked)
	})
}

func Test_readCopy(t *testing.T) {
	t.Parallel()

	t.Run("partition ごとの region を昇順で返す", func(t *testing.T) {
		t.Parallel()
		got, err := readCopy(writeFile(t, "c.tftest.hcl", copyHCL))
		require.NoError(t, err)
		assert.Equal(t, partitionSet{"aws": {"ap-northeast-1", "us-east-1"}, "aws-cn": {"cn-north-1"}}, got)
	})

	countCases := []struct{ name, src string }{
		{name: "写しが無ければ成功で返さない", src: "variables {\n  x = 1\n}\n"},
		{name: "run の variables にもう1つあれば失敗する", src: copyHCL + "run \"y\" {\n  variables {\n    aws_partitions = {}\n  }\n}\n"},
		{name: "ファイルの直下の variables が2つあれば失敗する", src: copyHCL + copyHCL},
	}
	for _, tc := range countCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := readCopy(writeFile(t, "c.tftest.hcl", tc.src))
			require.ErrorIs(t, err, errCopyCount)
		})
	}

	copyCases := []struct{ name, value string }{
		{name: "リテラルでなければ失敗する", value: "var.x"},
		{name: "写像でなければ失敗する", value: `["aws"]`},
		{name: "空の写像は失敗する", value: "{}"},
		{name: "region の一覧が配列でなければ失敗する", value: `{ "aws" = "us-east-1" }`},
		{name: "同じ partition が2度あれば失敗する", value: `{ "aws" = ["us-east-1"], "aws" = ["us-west-2"] }`},
	}
	for _, tc := range copyCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := readCopy(writeFile(t, "c.tftest.hcl", "variables {\n  aws_partitions = "+tc.value+"\n}\n"))
			require.ErrorIs(t, err, errCopy)
		})
	}

	t.Run("読めなければ読めない失敗のまま返す", func(t *testing.T) {
		t.Parallel()
		_, err := readCopy(filepath.Join(t.TempDir(), "none.hcl"))
		require.ErrorIs(t, err, errRead)
	})
}

func Test_uniqueKeys(t *testing.T) {
	t.Parallel()

	parse := func(t *testing.T, src string) hclsyntax.Expression {
		t.Helper()
		e, diags := hclsyntax.ParseExpression([]byte(src), "x.hcl", hcl.InitialPos)
		require.False(t, diags.HasErrors())

		return e
	}

	t.Run("キーが重ならなければ成功する", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, uniqueKeys(parse(t, `{ "aws" = [], aws-cn = [] }`)))
	})

	cases := []struct{ name, src, msg string }{
		{name: "同じキーが2度あれば失敗する", src: `{ "aws" = [], aws = [] }`, msg: "が2度あります"},
		{name: "写像のリテラルでなければ失敗する", src: `var.x`, msg: "写像のリテラルではありません"},
		{name: "キーが文字列のリテラルでなければ失敗する", src: `{ (var.k) = [] }`, msg: "キーが文字列のリテラルではありません"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := uniqueKeys(parse(t, tc.src))
			require.ErrorIs(t, err, errCopy)
			assert.ErrorContains(t, err, tc.msg)
		})
	}
}

func Test_copyAttributes(t *testing.T) {
	t.Parallel()

	t.Run("どの深さの variables の写しも集め、ほかの block の同名の属性は数えない", func(t *testing.T) {
		t.Parallel()
		src := copyHCL + "run \"y\" {\n  variables {\n    aws_partitions = {}\n  }\n  module {\n    aws_partitions = {}\n  }\n}\n"
		f, diags := hclsyntax.ParseConfig([]byte(src), "c.hcl", hcl.InitialPos)
		require.False(t, diags.HasErrors())
		assert.Len(t, copyAttributes(f.Body.(*hclsyntax.Body)), 2)
	})
}

func Test_regionList(t *testing.T) {
	t.Parallel()

	t.Run("region を昇順で返す", func(t *testing.T) {
		t.Parallel()
		got, err := regionList(cty.TupleVal([]cty.Value{cty.StringVal("b-1"), cty.StringVal("a-1")}))
		require.NoError(t, err)
		assert.Equal(t, []string{"a-1", "b-1"}, got)
	})

	cases := []struct {
		name  string
		value cty.Value
	}{
		{name: "配列でなければ失敗する", value: cty.StringVal("a-1")},
		{name: "空の配列は失敗する", value: cty.EmptyTupleVal},
		{name: "文字列でない要素は失敗する", value: cty.TupleVal([]cty.Value{cty.NumberIntVal(1)})},
		{name: "空の文字列は失敗する", value: cty.TupleVal([]cty.Value{cty.StringVal("")})},
		{name: "list は配列のリテラルではないので失敗する", value: cty.ListVal([]cty.Value{cty.StringVal("a-1")})},
		{name: "null の要素は失敗する", value: cty.TupleVal([]cty.Value{cty.NullVal(cty.String)})},
		{name: "同じ region が2度あれば失敗する", value: cty.TupleVal([]cty.Value{cty.StringVal("a-1"), cty.StringVal("a-1")})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := regionList(tc.value)
			require.ErrorIs(t, err, errCopy)
		})
	}
}

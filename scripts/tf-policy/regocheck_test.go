package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	policyOK     = "package plan.p\n\ndeny contains msg if {\n\tfalse\n\tmsg := \"[PLAN-IAM-01] x\"\n}\n"
	policyTestOK = "package plan.p_test\n\ntest_violation_x if { \"[PLAN-IAM-01]\" }\n\ntest_pass_x if true\n"
)

func Test_readPolicy(t *testing.T) {
	t.Parallel()

	t.Run("木の下の .rego を相対パスの辞書順で読み、テストを見分ける", func(t *testing.T) {
		t.Parallel()
		dir := tree(t, map[string]string{"p.rego": "a", "p_test.rego": "b", "lib/l.rego": "c", "README.md": "d"})
		got, err := readPolicy(dir)
		require.NoError(t, err)
		assert.Equal(t, []regoFile{{rel: "lib/l.rego", src: "c"}, {rel: "p.rego", src: "a"}, {rel: "p_test.rego", src: "b", test: true}}, got)
	})

	t.Run("木が無ければ errRead", func(t *testing.T) {
		t.Parallel()
		_, err := readPolicy(t.TempDir() + "/none")
		require.ErrorIs(t, err, errRead)
	})
}

func Test_checkRego(t *testing.T) {
	t.Parallel()

	files := func(extra ...regoFile) []regoFile {
		return append([]regoFile{{rel: "p.rego", src: policyOK}, {rel: "p_test.rego", src: policyTestOK, test: true}}, extra...)
	}

	t.Run("形が整っていれば違反なし", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, checkRego(files()))
	})

	for name, c := range map[string]struct {
		files []regoFile
		want  string
	}{
		"deny if の形":          {files(regoFile{rel: "x.rego", src: "package plan.x\n\ndeny if false\n"}), "deny contains 以外の形"},
		"warn の規則":            {files(regoFile{rel: "x.rego", src: "package plan.x\n\nwarn contains \"w\" if false\n"}), "deny contains 以外の形"},
		"deny_ で始まる補助の名前":     {files(regoFile{rel: "x.rego", src: "package plan.x\n\ndeny_list := {\"x\"}\n"}), "deny contains 以外の形"},
		"違反する入力のテストが無い":       {[]regoFile{{rel: "p.rego", src: policyOK}, {rel: "p_test.rego", src: "package plan.p_test\n\ntest_pass_x if { \"[PLAN-IAM-01]\" }\n", test: true}}, "両方のテストを持たない Policy（ADR-0402 決定8・9）: p.rego"},
		"違反しない入力のテストが無い":      {[]regoFile{{rel: "p.rego", src: policyOK}, {rel: "p_test.rego", src: "package plan.p_test\n\ntest_violation_x if { \"[PLAN-IAM-01]\" }\n", test: true}}, "両方のテストを持たない"},
		"テストのファイルが無い":         {[]regoFile{{rel: "p.rego", src: policyOK}}, "両方のテストを持たない"},
		"ID を持たない deny（1行）":   {files(regoFile{rel: "p2.rego", src: "package plan.p2\n\ndeny contains \"unnamed\" if false\n"}, regoFile{rel: "p2_test.rego", src: "package plan.p2_test\n\ntest_violation_a if true\n\ntest_pass_a if true\n", test: true}), "ID（[PLAN-<区分>-<番号>]）を持たない deny 規則があります（ID ごとのテストの要求が届かない）: p2.rego:3"},
		"ID を持たない deny（ブロック）": {files(regoFile{rel: "p2.rego", src: "package plan.p2\n\ndeny contains msg if {\n\tmsg := \"x\"\n}\n\nother := \"[PLAN-IAM-01]\"\n"}, regoFile{rel: "p2_test.rego", src: "package plan.p2_test\n\ntest_violation_a if true\n\ntest_pass_a if true\n", test: true}), "p2.rego:3"},
		"テストが名指ししない ID":       {[]regoFile{{rel: "p.rego", src: policyOK + "\ndeny contains \"[PLAN-IAM-99]\" if false\n"}, {rel: "p_test.rego", src: policyTestOK, test: true}}, "テストが一度も名指ししない違反の ID（ADR-0402 決定8・9）: [PLAN-IAM-99]"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := checkRego(c.files)
			require.NotEmpty(t, got)
			assert.Contains(t, got[len(got)-1]+"\n"+got[0], c.want)
		})
	}
}

func Test_blockHasID(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		lines []string
		want  bool
	}{
		"1行の規則の ID":        {[]string{"deny contains \"[PLAN-A-1] x\" if false"}, true},
		"1行の規則で ID が無い":    {[]string{"deny contains \"x\" if false", "\"[PLAN-A-1]\""}, false},
		"ブロックの中の ID":       {[]string{"deny contains msg if {", "\tmsg := \"[PLAN-A-1] x\"", "}"}, true},
		"ブロックの後の ID は数えない": {[]string{"deny contains msg if {", "\tmsg := \"x\"", "}", "\"[PLAN-A-1]\""}, false},
		"閉じないブロック":         {[]string{"deny contains msg if {", "\tmsg := \"x\""}, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, blockHasID(c.lines, 0))
		})
	}
}

func Test_hasLinePrefix(t *testing.T) {
	t.Parallel()

	t.Run("行頭だけを見る", func(t *testing.T) {
		t.Parallel()
		assert.True(t, hasLinePrefix("a\ntest_pass_x if true\n", "test_pass_"))
		assert.False(t, hasLinePrefix("a\n  test_pass_x if true\n", "test_pass_"))
	})
}

func Test_runRegoCheck(t *testing.T) {
	t.Parallel()

	t.Run("形が整っていれば成功する", func(t *testing.T) {
		t.Parallel()
		dir := tree(t, map[string]string{"p.rego": policyOK, "p_test.rego": policyTestOK})
		require.NoError(t, runRegoCheck([]string{"-policy", dir}, &bytes.Buffer{}))
	})

	t.Run("違反を出して errFailed", func(t *testing.T) {
		t.Parallel()
		dir := tree(t, map[string]string{"p.rego": policyOK + "\nwarn contains \"w\" if false\n", "p_test.rego": policyTestOK})
		var out bytes.Buffer
		require.ErrorIs(t, runRegoCheck([]string{"-policy", dir}, &out), errFailed)
		assert.Contains(t, out.String(), "❌ deny contains 以外の形")
	})

	t.Run("Policy が無ければ errNoInput", func(t *testing.T) {
		t.Parallel()
		dir := tree(t, map[string]string{"p_test.rego": policyTestOK})
		err := runRegoCheck([]string{"-policy", dir}, &bytes.Buffer{})
		require.ErrorIs(t, err, errNoInput)
		assert.Contains(t, err.Error(), "Rego のテストが0件です（Policy も無い")
	})

	t.Run("木が無ければ errRead", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, runRegoCheck([]string{"-policy", t.TempDir() + "/none"}, &bytes.Buffer{}), errRead)
	})

	t.Run("引数の誤りは errUsage", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, runRegoCheck(nil, &bytes.Buffer{}), errUsage)
		require.ErrorIs(t, runRegoCheck([]string{"-policy", "x", "y"}, &bytes.Buffer{}), errUsage)
	})
}

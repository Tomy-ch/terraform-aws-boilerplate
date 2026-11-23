package main

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_countTests(t *testing.T) {
	t.Parallel()

	t.Run("テストの rego の行頭の test_ だけを数える", func(t *testing.T) {
		t.Parallel()
		got := countTests([]regoFile{
			{rel: "p.rego", src: "test_not_counted if true\n"},
			{rel: "p_test.rego", src: "test_a if true\n  test_indented if true\ntest_b if true\nx := `\ntest_in_string\n`\n", test: true},
		})
		assert.Equal(t, 3, got, "字下げした行は数えず、文字列の中の行頭は数える（conftest が走らせる数とずれ、verify-result が落とす）")
	})
}

func Test_judgeVerify(t *testing.T) {
	t.Parallel()

	t.Run("落ちたテストが無く、通った数が宣言と同じなら成功", func(t *testing.T) {
		t.Parallel()
		passed, failed, err := judgeVerify([]byte(`[{"filename":"a","successes":2},{"filename":"b","successes":1}]`), 3)
		require.NoError(t, err)
		assert.Equal(t, 3, passed)
		assert.Empty(t, failed)
	})

	t.Run("落ちたテストがあれば名指して errFailed", func(t *testing.T) {
		t.Parallel()
		_, failed, err := judgeVerify([]byte(`[{"filename":"a","successes":2,"failures":[{"msg":"data.p.test_x"}]}]`), 3)
		require.ErrorIs(t, err, errFailed)
		assert.Contains(t, err.Error(), "Rego のテストが落ちました（1 件）")
		assert.Equal(t, []string{"a: data.p.test_x"}, failed)
	})

	t.Run("通った数が宣言と違えば errFailed", func(t *testing.T) {
		t.Parallel()
		_, _, err := judgeVerify([]byte(`[{"filename":"a","successes":2}]`), 3)
		require.ErrorIs(t, err, errFailed)
		assert.Contains(t, err.Error(), "通った Rego のテストが 2 件で、宣言された 3 件と一致しません")
	})

	t.Run("空の結果は errRead（conftest が起動できなかった）", func(t *testing.T) {
		t.Parallel()
		_, _, err := judgeVerify(nil, 3)
		require.ErrorIs(t, err, errRead)
	})

	t.Run("JSON でない結果は errRead", func(t *testing.T) {
		t.Parallel()
		_, _, err := judgeVerify([]byte("Error: running verification"), 3)
		require.ErrorIs(t, err, errRead)
	})
}

func Test_runVerifyResult(t *testing.T) {
	t.Parallel()

	setup := func(t *testing.T, report string) []string {
		t.Helper()
		dir := tree(t, map[string]string{"p.rego": policyOK, "p_test.rego": policyTestOK, "verify.json": report})
		return []string{"-policy", dir, "-report", filepath.Join(dir, "verify.json")}
	}

	t.Run("宣言の数のテストが通れば成功し、件数を出す", func(t *testing.T) {
		t.Parallel()
		var out bytes.Buffer
		require.NoError(t, runVerifyResult(setup(t, `[{"filename":"p_test.rego","successes":2}]`), &out))
		assert.Equal(t, "✅ Rego のテスト 2 件\n", out.String())
	})

	t.Run("落ちたテストを名指して errFailed", func(t *testing.T) {
		t.Parallel()
		var out bytes.Buffer
		require.ErrorIs(t, runVerifyResult(setup(t, `[{"filename":"p_test.rego","successes":1,"failures":[{"msg":"data.p_test.test_pass_x"}]}]`), &out), errFailed)
		assert.Contains(t, out.String(), "  p_test.rego: data.p_test.test_pass_x")
	})

	t.Run("読めない結果は本文を写して errRead", func(t *testing.T) {
		t.Parallel()
		var out bytes.Buffer
		require.ErrorIs(t, runVerifyResult(setup(t, "Error: eval_cancel_error"), &out), errRead)
		assert.Contains(t, out.String(), "Error: eval_cancel_error")
	})

	t.Run("テストが0件なら errNoInput", func(t *testing.T) {
		t.Parallel()
		dir := tree(t, map[string]string{"p.rego": policyOK, "verify.json": "[]"})
		err := runVerifyResult([]string{"-policy", dir, "-report", filepath.Join(dir, "verify.json")}, &bytes.Buffer{})
		require.ErrorIs(t, err, errNoInput)
	})

	t.Run("結果のファイルが無ければ errRead", func(t *testing.T) {
		t.Parallel()
		dir := tree(t, map[string]string{"p.rego": policyOK, "p_test.rego": policyTestOK})
		require.ErrorIs(t, runVerifyResult([]string{"-policy", dir, "-report", filepath.Join(dir, "none.json")}, &bytes.Buffer{}), errRead)
	})

	t.Run("Policy の木を読めなければ errRead", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, runVerifyResult([]string{"-policy", t.TempDir() + "/none", "-report", "r"}, &bytes.Buffer{}), errRead)
	})

	t.Run("引数の誤りは errUsage", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, runVerifyResult([]string{"-policy", "p"}, &bytes.Buffer{}), errUsage)
	})
}

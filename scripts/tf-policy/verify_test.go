package main

import (
	"bytes"
	"fmt"
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

func opaPass(name string, ns int64) string {
	return fmt.Sprintf(`{"location":{"file":"p_test.rego","row":1,"col":1},"package":"data.p_test","name":%q,"duration":%d}`, name, ns)
}

func Test_judgeVerify(t *testing.T) {
	t.Parallel()

	t.Run("落ちたテストが無く、通った数が宣言と同じなら成功", func(t *testing.T) {
		t.Parallel()
		passed, failed, err := judgeVerify([]byte("["+opaPass("test_a", 1)+","+opaPass("test_b", 2)+"]"), 2)
		require.NoError(t, err)
		assert.Equal(t, 2, passed)
		assert.Empty(t, failed)
	})

	t.Run("fail のケースを名指して errFailed", func(t *testing.T) {
		t.Parallel()
		_, failed, err := judgeVerify([]byte(`[`+opaPass("test_a", 1)+`,{"package":"data.p_test","name":"test_x","fail":true,"duration":1}]`), 2)
		require.ErrorIs(t, err, errFailed)
		assert.Contains(t, err.Error(), "Rego のテストが落ちました（1 件）")
		assert.Equal(t, []string{"p_test.test_x"}, failed)
	})

	t.Run("時間切れ（error）のケースを、理由とともに名指して errFailed", func(t *testing.T) {
		t.Parallel()
		report := `[` + opaPass("test_a", 1) + `,{"package":"data.p_test","name":"test_slow","error":{"code":"eval_cancel_error","message":"context deadline exceeded"},"duration":5000000000}]`
		passed, failed, err := judgeVerify([]byte(report), 2)
		require.ErrorIs(t, err, errFailed)
		assert.Equal(t, 1, passed, "error のケースは通った数に入れない")
		assert.Equal(t, []string{"p_test.test_slow: eval_cancel_error: context deadline exceeded"}, failed)
	})

	t.Run("code を持たない error も落ちたケースとして名指す", func(t *testing.T) {
		t.Parallel()
		_, failed, err := judgeVerify([]byte(`[{"package":"data.p_test","name":"test_e","error":"boom","duration":1}]`), 1)
		require.ErrorIs(t, err, errFailed)
		assert.Equal(t, []string{`p_test.test_e: "boom"`}, failed)
	})

	t.Run("skip のケースは通った数に入れず、宣言とずれて errFailed", func(t *testing.T) {
		t.Parallel()
		passed, _, err := judgeVerify([]byte(`[`+opaPass("test_a", 1)+`,{"package":"data.p_test","name":"todo_test_b","skip":true,"duration":0}]`), 2)
		require.ErrorIs(t, err, errFailed)
		assert.Equal(t, 1, passed)
		assert.Contains(t, err.Error(), "通った Rego のテストが 1 件で、宣言された 2 件と一致しません")
	})

	t.Run("通った数が宣言と違えば errFailed", func(t *testing.T) {
		t.Parallel()
		_, _, err := judgeVerify([]byte("["+opaPass("test_a", 1)+"]"), 3)
		require.ErrorIs(t, err, errFailed)
		assert.Contains(t, err.Error(), "通った Rego のテストが 1 件で、宣言された 3 件と一致しません")
	})

	t.Run("null の要素を通った数に入れず errRead（件数が宣言と合っていても）", func(t *testing.T) {
		t.Parallel()
		_, _, err := judgeVerify([]byte("[null,null]"), 2)
		require.ErrorIs(t, err, errRead)
	})

	t.Run("0件の配列は、宣言とずれて errFailed", func(t *testing.T) {
		t.Parallel()
		_, _, err := judgeVerify([]byte("[]"), 1)
		require.ErrorIs(t, err, errFailed)
	})

	for name, report := range map[string]string{
		"空の結果（opa がコンパイルの誤りで stdout に何も書かなかった）": "",
		"JSON でない結果": "1 error occurred: p.rego:1: rego_parse_error",
		"null":       "null",
		"配列でない JSON": `{"package":"data.p_test","name":"test_a"}`,
		"壊れた配列":      `[{"package":`,
	} {
		t.Run(name+"は errRead", func(t *testing.T) {
			t.Parallel()
			_, _, err := judgeVerify([]byte(report), 1)
			require.ErrorIs(t, err, errRead)
		})
	}
}

func Test_slowest(t *testing.T) {
	t.Parallel()

	t.Run("時間の長い順に n 件を返す", func(t *testing.T) {
		t.Parallel()
		report := "[" + opaPass("test_a", 1_000_000) + "," + opaPass("test_b", 3_000_000) + "," + opaPass("test_c", 2_000_000) + "]"
		assert.Equal(t, []string{"p_test.test_b 3ms", "p_test.test_c 2ms"}, slowest([]byte(report), 2))
	})

	t.Run("n より少なければあるだけ返す", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, []string{"p_test.test_a 1ms"}, slowest([]byte("["+opaPass("test_a", 1_000_000)+"]"), 3))
	})

	t.Run("読めない結果なら何も返さない", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, slowest([]byte("x"), 3))
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
		require.NoError(t, runVerifyResult(setup(t, "["+opaPass("test_pass_a", 2_000_000)+","+opaPass("test_violation_a", 1_000_000)+"]"), &out))
		assert.Equal(t, "✅ Rego のテスト 2 件\n  遅いケース: p_test.test_pass_a 2ms\n  遅いケース: p_test.test_violation_a 1ms\n", out.String())
	})

	t.Run("落ちたテストを名指して errFailed", func(t *testing.T) {
		t.Parallel()
		var out bytes.Buffer
		require.ErrorIs(t, runVerifyResult(setup(t, "["+opaPass("test_pass_a", 1)+`,{"package":"data.p_test","name":"test_violation_a","fail":true}]`), &out), errFailed)
		assert.Contains(t, out.String(), "  p_test.test_violation_a")
	})

	t.Run("読めない結果は本文を写して errRead", func(t *testing.T) {
		t.Parallel()
		var out bytes.Buffer
		require.ErrorIs(t, runVerifyResult(setup(t, "1 error occurred: rego_parse_error"), &out), errRead)
		assert.Contains(t, out.String(), "1 error occurred: rego_parse_error")
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

func Test_verifyEntry_failure(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		entry verifyEntry
		want  string
		ok    bool
	}{
		"通ったケースは落ちていない":                   {entry: verifyEntry{Package: "data.p", Name: "test_a"}, ok: false},
		"error が null なら落ちていない":           {entry: verifyEntry{Package: "data.p", Name: "test_a", Error: []byte("null")}, ok: false},
		"fail は名前だけで落ちる":                  {entry: verifyEntry{Package: "data.p", Name: "test_a", Fail: true}, want: "p.test_a", ok: true},
		"error は code と message を添えて落ちる":  {entry: verifyEntry{Package: "data.p", Name: "test_a", Error: []byte(`{"code":"eval_cancel_error","message":"timed out"}`)}, want: "p.test_a: eval_cancel_error: timed out", ok: true},
		"error と fail の両方なら error の理由を出す": {entry: verifyEntry{Package: "data.p", Name: "test_a", Fail: true, Error: []byte(`{"code":"c","message":"m"}`)}, want: "p.test_a: c: m", ok: true},
		"skip は落ちていない":                    {entry: verifyEntry{Package: "data.p", Name: "todo_test_a", Skip: true}, ok: false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, ok := c.entry.failure()
			assert.Equal(t, c.ok, ok)
			assert.Equal(t, c.want, got)
		})
	}
}

func Test_parseVerify(t *testing.T) {
	t.Parallel()

	t.Run("配列の要素を読む", func(t *testing.T) {
		t.Parallel()
		got, err := parseVerify([]byte("\n  [" + opaPass("test_a", 7) + "]\n"))
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, verifyEntry{Package: "data.p_test", Name: "test_a", Duration: 7}, got[0])
	})

	for name, report := range map[string]string{"null": "null", "オブジェクト": "{}", "文字列": `"x"`, "空": ""} {
		t.Run("配列でなければ、JSON として正しくても errRead: "+name, func(t *testing.T) {
			t.Parallel()
			_, err := parseVerify([]byte(report))
			require.ErrorIs(t, err, errRead)
		})
	}

	t.Run("壊れた配列は errRead", func(t *testing.T) {
		t.Parallel()
		_, err := parseVerify([]byte("[{"))
		require.ErrorIs(t, err, errRead)
	})

	for name, report := range map[string]string{
		"null の要素":      "[" + opaPass("test_a", 1) + ",null]",
		"空のオブジェクトの要素":   "[{}]",
		"name の無い要素":    `[{"package":"data.p_test","duration":1}]`,
		"package の無い要素": `[{"name":"test_a","duration":1}]`,
	} {
		t.Run(name+"は、通った1件に数えずに errRead", func(t *testing.T) {
			t.Parallel()
			_, err := parseVerify([]byte(report))
			require.ErrorIs(t, err, errRead)
		})
	}
}

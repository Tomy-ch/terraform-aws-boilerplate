package main

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_declaredFormat(t *testing.T) {
	t.Parallel()

	t.Run("宣言の provider_format_version を返す", func(t *testing.T) {
		t.Parallel()
		got, err := declaredFormat([]byte(`{"policy_plan":{"provider_format_version":"1.0"}}`))
		require.NoError(t, err)
		assert.Equal(t, "1.0", got)
	})

	t.Run("宣言に無ければ印を返す", func(t *testing.T) {
		t.Parallel()
		got, err := declaredFormat([]byte(`{"policy_plan":{}}`))
		require.NoError(t, err)
		assert.Equal(t, undeclared, got)
	})

	t.Run("JSON でなければ errRead", func(t *testing.T) {
		t.Parallel()
		_, err := declaredFormat([]byte("x"))
		require.ErrorIs(t, err, errRead)
	})
}

func Test_extractPlan(t *testing.T) {
	t.Parallel()

	t.Run("test_plan を1件取り出し、provider_schemas を除く", func(t *testing.T) {
		t.Parallel()
		got, err := extractPlan("a", []byte(`{"type":"other"}`+"\n"+planOK), "1.0")
		require.NoError(t, err)
		assert.JSONEq(t, `{"format_version":"1.2","provider_format_version":"1.0","resource_changes":[]}`, string(got))
	})

	for name, c := range map[string]struct {
		jsonl, want, msg string
	}{
		"test_plan が2件":                      {planOK + planOK, "1.0", "a: test_plan が 2 件です（1件を期待）"},
		"test_plan が0件":                      {`{"type":"other"}` + "\n", "1.0", "a: test_plan が 0 件です"},
		"provider_format_version が違う":        {planOK, "0.0", "test_plan の provider_format_version が 1.0 で、宣言の 0.0 ではありません"},
		"宣言に provider_format_version が無い":    {planOK, undeclared, "宣言の （宣言に無い） ではありません"},
		"plan に provider_format_version が無い": {`{"type":"test_plan","test_plan":{}}` + "\n", "1.0", "が （無い） で"},
		"JSON でない行":                          {"not json\n", "1.0", "terraform test の出力を解釈できません"},
	} {
		t.Run(name+"は errFailed", func(t *testing.T) {
			t.Parallel()
			_, err := extractPlan("a", []byte(c.jsonl), c.want)
			require.ErrorIs(t, err, errFailed)
			assert.Contains(t, err.Error(), c.msg)
		})
	}
}

func Test_extractAll(t *testing.T) {
	t.Parallel()

	t.Run("各 root の入力を書き、件数を返す", func(t *testing.T) {
		t.Parallel()
		out := tree(t, map[string]string{"runs/a.jsonl": planOK})
		require.NoError(t, writeText(filepath.Join(out, "plans.txt"), filepath.Join(out, "runs", "a.jsonl")+"\n"))
		n, err := extractAll(out, "1.0", &bytes.Buffer{})
		require.NoError(t, err)
		assert.Equal(t, 1, n)
		assert.FileExists(t, filepath.Join(out, "inputs", "a.json"))
	})

	t.Run("落ちた root を最後まで見てから名指す", func(t *testing.T) {
		t.Parallel()
		out := tree(t, map[string]string{"runs/a.jsonl": planOK + planOK, "runs/b.jsonl": planOK})
		require.NoError(t, writeText(filepath.Join(out, "plans.txt"), filepath.Join(out, "runs", "a.jsonl")+"\n"+filepath.Join(out, "runs", "b.jsonl")+"\n"))
		var w bytes.Buffer
		n, err := extractAll(out, "1.0", &w)
		require.ErrorIs(t, err, errFailed)
		assert.Equal(t, 1, n)
		assert.Contains(t, w.String(), "❌ tf-policy が落ちたディレクトリ: a")
		assert.FileExists(t, filepath.Join(out, "inputs", "b.json"))
	})

	t.Run("一覧が空なら errNoInput", func(t *testing.T) {
		t.Parallel()
		out := tree(t, map[string]string{"plans.txt": ""})
		_, err := extractAll(out, "1.0", &bytes.Buffer{})
		require.ErrorIs(t, err, errNoInput)
	})

	t.Run("一覧の jsonl が無ければ errRead", func(t *testing.T) {
		t.Parallel()
		out := tree(t, map[string]string{"plans.txt": "/no/such.jsonl\n"})
		_, err := extractAll(out, "1.0", &bytes.Buffer{})
		require.ErrorIs(t, err, errRead)
	})

	t.Run("一覧が無ければ errRead", func(t *testing.T) {
		t.Parallel()
		_, err := extractAll(t.TempDir(), "1.0", &bytes.Buffer{})
		require.ErrorIs(t, err, errRead)
	})

}

func Test_runExtract(t *testing.T) {
	t.Parallel()

	t.Run("宣言の版で取り出し、件数を出す", func(t *testing.T) {
		t.Parallel()
		out := tree(t, map[string]string{"runs/a.jsonl": planOK, "decl.json": `{"policy_plan":{"provider_format_version":"1.0"}}`})
		require.NoError(t, writeText(filepath.Join(out, "plans.txt"), filepath.Join(out, "runs", "a.jsonl")+"\n"))
		var w bytes.Buffer
		require.NoError(t, runExtract([]string{"-out", out, "-decl", filepath.Join(out, "decl.json")}, &w))
		assert.Equal(t, "✅ Policy の入力 1 件（reference の root ごとの plan）\n", w.String())
	})

	t.Run("宣言を読めなければ errRead", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, runExtract([]string{"-out", t.TempDir(), "-decl", filepath.Join(t.TempDir(), "none")}, &bytes.Buffer{}), errRead)
	})

	t.Run("宣言が JSON でなければ errRead", func(t *testing.T) {
		t.Parallel()
		dir := tree(t, map[string]string{"decl.json": "x"})
		require.ErrorIs(t, runExtract([]string{"-out", dir, "-decl", filepath.Join(dir, "decl.json")}, &bytes.Buffer{}), errRead)
	})

	t.Run("引数の誤りは errUsage", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, runExtract([]string{"-out", "o"}, &bytes.Buffer{}), errUsage)
	})
}

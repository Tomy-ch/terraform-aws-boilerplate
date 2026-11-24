package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/testenv"
)

func Test_normalizeSARIF(t *testing.T) {
	t.Parallel()

	t.Run("抑止した result を落とし、uri の前置きを外し、数をそのまま残す", func(t *testing.T) {
		t.Parallel()
		in := `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"x"}},"results":[
{"ruleId":"keep","level":"error","rank":12345678901234567890,"locations":[{"physicalLocation":{"artifactLocation":{"uri":"/work/src/usecases/a/main.tf"}}}]},
{"ruleId":"keep-empty","suppressions":[],"locations":[{"physicalLocation":{"artifactLocation":{"uri":"../../work/src/env/b/main.tf"}}}]},
{"ruleId":"drop","suppressions":[{"kind":"inSource"}]}
],"artifacts":[{"location":{"artifactLocation":{"uri":"work/src/modules/c/main.tf"}}},{"location":{"artifactLocation":{"index":0}}}]}]}`
		dir := tree(t, map[string]string{"r.sarif": in})
		p := filepath.Join(dir, "r.sarif")
		require.NoError(t, normalizeSARIF(p))
		raw, err := os.ReadFile(p)
		require.NoError(t, err)
		want := `{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"x"}},"results":[
{"ruleId":"keep","level":"error","rank":12345678901234567890,"locations":[{"physicalLocation":{"artifactLocation":{"uri":"usecases/a/main.tf"}}}]},
{"ruleId":"keep-empty","suppressions":[],"locations":[{"physicalLocation":{"artifactLocation":{"uri":"env/b/main.tf"}}}]}
],"artifacts":[{"location":{"artifactLocation":{"uri":"modules/c/main.tf"}}},{"location":{"artifactLocation":{"index":0}}}]}]}`
		assert.JSONEq(t, want, string(raw))
		assert.Contains(t, string(raw), "12345678901234567890", "大きな数を浮動小数にしない")
	})

	t.Run("リポジトリの相対パスの途中の work/src/ は外さない", func(t *testing.T) {
		t.Parallel()
		dir := tree(t, map[string]string{"r.sarif": `{"runs":[{"results":[{"locations":[{"physicalLocation":{"artifactLocation":{"uri":"usecases/work/src/main.tf"}}}]}]}]}`})
		p := filepath.Join(dir, "r.sarif")
		require.NoError(t, normalizeSARIF(p))
		raw, err := os.ReadFile(p)
		require.NoError(t, err)
		assert.Contains(t, string(raw), `"usecases/work/src/main.tf"`)
	})

	t.Run("results が無いか null の run はそのまま残す", func(t *testing.T) {
		t.Parallel()
		dir := tree(t, map[string]string{"r.sarif": `{"runs":[{"tool":{}},{"results":null}]}`})
		p := filepath.Join(dir, "r.sarif")
		require.NoError(t, normalizeSARIF(p))
		raw, err := os.ReadFile(p)
		require.NoError(t, err)
		assert.JSONEq(t, `{"runs":[{"tool":{}},{"results":null}]}`, string(raw))
	})

	t.Run("ファイルが無ければ errResult", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		require.ErrorIs(t, normalizeSARIF(filepath.Join(dir, "none.sarif")), errResult)
		assert.NoFileExists(t, filepath.Join(dir, "none.sarif"))
	})

	t.Run("symlink は辿らずに errResult で、指す先を書き換えない", func(t *testing.T) {
		t.Parallel()
		in := `{"runs":[{"results":[{"suppressions":[{"kind":"inSource"}]}]}]}`
		dir := tree(t, map[string]string{"real.sarif": in})
		p := filepath.Join(dir, "r.sarif")
		require.NoError(t, os.Symlink("real.sarif", p))
		require.ErrorIs(t, normalizeSARIF(p), errResult)
		raw, err := os.ReadFile(filepath.Join(dir, "real.sarif"))
		require.NoError(t, err)
		assert.Equal(t, in, string(raw))
	})

	t.Run("書き戻せなければ errOutput で、ファイルを書き換えない", func(t *testing.T) {
		t.Parallel()
		testenv.RequireNonRoot(t, "親ディレクトリを読み取り専用にして書き込みを落とす")
		in := `{"runs":[{"results":[{"suppressions":[{"kind":"inSource"}]}]}]}`
		dir := tree(t, map[string]string{"out/r.sarif": in})
		out := filepath.Join(dir, "out")
		require.NoError(t, os.Chmod(out, 0o500))
		t.Cleanup(func() { _ = os.Chmod(out, 0o700) })
		p := filepath.Join(out, "r.sarif")
		require.ErrorIs(t, normalizeSARIF(p), errOutput)
		raw, err := os.ReadFile(p)
		require.NoError(t, err)
		assert.Equal(t, in, string(raw))
	})

	for name, in := range map[string]string{
		"JSON でない":              "not json",
		"object でない":            `[]`,
		"runs が無い":              `{}`,
		"runs が配列でない":           `{"runs":{}}`,
		"run が object でない":      `{"runs":[1]}`,
		"results が配列でない":        `{"runs":[{"results":{}}]}`,
		"result が object でない":   `{"runs":[{"results":[1]}]}`,
		"suppressions が配列でない":   `{"runs":[{"results":[{"suppressions":{}}]}]}`,
		"uri が文字列でない":           `{"runs":[{"results":[{"locations":[{"physicalLocation":{"artifactLocation":{"uri":1}}}]}]}]}`,
		"artifactLocation が文字列": `{"runs":[{"results":[{"locations":[{"physicalLocation":{"artifactLocation":"x"}}]}]}]}`,
	} {
		t.Run(name+"なら errResult で、ファイルを書き換えない", func(t *testing.T) {
			t.Parallel()
			dir := tree(t, map[string]string{"r.sarif": in})
			p := filepath.Join(dir, "r.sarif")
			require.ErrorIs(t, normalizeSARIF(p), errResult)
			raw, err := os.ReadFile(p)
			require.NoError(t, err)
			assert.Equal(t, in, string(raw))
		})
	}
}

func Test_unsuppressed(t *testing.T) {
	t.Parallel()

	t.Run("空でない suppressions を持つ result だけを落とす", func(t *testing.T) {
		t.Parallel()
		in := []any{
			map[string]any{"id": "a"},
			map[string]any{"id": "b", "suppressions": nil},
			map[string]any{"id": "c", "suppressions": []any{}},
			map[string]any{"id": "d", "suppressions": []any{map[string]any{"kind": "inSource"}}},
		}
		got, err := unsuppressed(in)
		require.NoError(t, err)
		assert.Equal(t, in[:3], got)
	})

	t.Run("nil は nil", func(t *testing.T) {
		t.Parallel()
		got, err := unsuppressed(nil)
		require.NoError(t, err)
		assert.Nil(t, got)
	})

	t.Run("全部落ちれば空の配列", func(t *testing.T) {
		t.Parallel()
		got, err := unsuppressed([]any{map[string]any{"suppressions": []any{1}}})
		require.NoError(t, err)
		assert.NotNil(t, got)
		assert.Empty(t, got)
	})

	for name, in := range map[string]any{
		"配列でない":              map[string]any{},
		"object でない要素":       []any{"x"},
		"配列でない suppressions": []any{map[string]any{"suppressions": "x"}},
	} {
		t.Run(name+"は errResult", func(t *testing.T) {
			t.Parallel()
			_, err := unsuppressed(in)
			require.ErrorIs(t, err, errResult)
		})
	}
}

func Test_stripStagedPrefix(t *testing.T) {
	t.Parallel()

	t.Run("入れ子の artifactLocation の uri をすべて書き直す", func(t *testing.T) {
		t.Parallel()
		var v any
		require.NoError(t, json.Unmarshal([]byte(`{"a":[{"artifactLocation":{"uri":"/work/src/x.tf"},
"b":{"artifactLocation":{"uri":"../work/src/y.tf","uriBaseId":"%SRCROOT%"}}}],"uri":"/work/src/not-a-location"}`), &v))
		require.NoError(t, stripStagedPrefix(v))
		raw, err := json.Marshal(v)
		require.NoError(t, err)
		assert.JSONEq(t, `{"a":[{"artifactLocation":{"uri":"x.tf"},
"b":{"artifactLocation":{"uri":"y.tf","uriBaseId":"%SRCROOT%"}}}],"uri":"/work/src/not-a-location"}`, string(raw))
	})

	t.Run("前置きの形でない uri はそのまま", func(t *testing.T) {
		t.Parallel()
		for _, uri := range []string{"work/srcx/a.tf", "./work/src/a.tf", "x/work/src/a.tf", "file:///work/src/a.tf"} {
			v := map[string]any{"artifactLocation": map[string]any{"uri": uri}}
			require.NoError(t, stripStagedPrefix(v))
			assert.Equal(t, uri, v["artifactLocation"].(map[string]any)["uri"])
		}
	})

	t.Run("uri の無い artifactLocation はそのまま", func(t *testing.T) {
		t.Parallel()
		v := map[string]any{"artifactLocation": map[string]any{"index": 0}}
		require.NoError(t, stripStagedPrefix(v))
		assert.Equal(t, map[string]any{"index": 0}, v["artifactLocation"])
	})

	t.Run("文字列でない uri と object でない artifactLocation は errResult", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, stripStagedPrefix([]any{map[string]any{"artifactLocation": map[string]any{"uri": 1}}}), errResult)
		require.ErrorIs(t, stripStagedPrefix(map[string]any{"artifactLocation": nil}), errResult)
	})
}

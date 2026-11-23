package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_runFixtures(t *testing.T) {
	t.Parallel()

	writeJSON := func(t *testing.T, body string) string {
		t.Helper()
		p := filepath.Join(t.TempDir(), "fixtures.json")
		require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
		return p
	}

	t.Run("ディレクトリの txtar の和とケースの数が一致すれば通す", func(t *testing.T) {
		t.Parallel()
		lib := writeTree(t, map[string]string{"a.txtar": "", "b.txtar": "", "note.md": ""})
		tree := writeTree(t, map[string]string{"c.txtar": ""})
		j := writeJSON(t, `{"fixtures": {"a": {}, "b": {}, "c": {}}}`)
		var stdout bytes.Buffer
		require.NoError(t, runFixtures([]string{"-json", j, lib, tree}, &stdout))
		assert.Equal(t, "✅ hcl-tally fixtures: txtar 3 件と "+j+" のケースの数が一致しました\n", stdout.String())
	})

	t.Run("失敗は errFixtures", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			files map[string]string
			json  string
			msg   string
		}{
			"ケースが欠けている":             {files: map[string]string{"a.txtar": "", "b.txtar": ""}, json: `{"fixtures": {"a": {}}}`, msg: "txtar は 2 件ですが"},
			"ケースが多い":                {files: map[string]string{"a.txtar": ""}, json: `{"fixtures": {"a": {}, "b": {}}}`, msg: "のケースは 2 件です"},
			"fixtures のキーが無い":       {files: map[string]string{"a.txtar": ""}, json: `{}`, msg: "のケースは 0 件です"},
			"JSON が壊れている":           {files: map[string]string{"a.txtar": ""}, json: `{`, msg: "を解釈できません"},
			"txtar が0件":             {files: map[string]string{"a.md": ""}, json: `{"fixtures": {}}`, msg: "txtar が1件もありません"},
			"txtar の名前のディレクトリは数えない": {files: map[string]string{"a.txtar/x": ""}, json: `{"fixtures": {"a": {}}}`, msg: "txtar が1件もありません"},
		}
		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				err := runFixtures([]string{"-json", writeJSON(t, tt.json), writeTree(t, tt.files)}, &bytes.Buffer{})
				require.ErrorIs(t, err, errFixtures)
				assert.Contains(t, err.Error(), tt.msg)
			})
		}
	})

	t.Run("ディレクトリか JSON を読めなければ errFixtures", func(t *testing.T) {
		t.Parallel()
		dir := writeTree(t, map[string]string{"a.txtar": ""})
		require.ErrorIs(t, runFixtures([]string{"-json", filepath.Join(t.TempDir(), "missing.json"), dir}, &bytes.Buffer{}), errFixtures)
		require.ErrorIs(t, runFixtures([]string{"-json", writeJSON(t, `{}`), filepath.Join(t.TempDir(), "missing")}, &bytes.Buffer{}), errFixtures)
	})

	t.Run("引数の誤りは errUsage", func(t *testing.T) {
		t.Parallel()
		for name, args := range map[string][]string{
			"-json が無い": {"."},
			"ディレクトリが無い": {"-json", "x.json"},
			"知らないフラグ":   {"-x"},
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				require.ErrorIs(t, runFixtures(args, &bytes.Buffer{}), errUsage)
			})
		}
	})
}

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTagVersionRecipe は、.makefiles/github/operation/release-tag.mk の tag-version を、PATH の先頭へ置いた
// go の偽物に対して実際に走らせ、release ツールへ渡る -version の値を見る。版の検証は Test_resolveExplicit が持つ。
func TestTagVersionRecipe(t *testing.T) {
	t.Parallel()

	// recipe は、引数を1行ずつ記録して成功する go の偽物を置いて make tag-version を走らせ、記録を返す。
	recipe := func(t *testing.T, args []string, env ...string) []string {
		t.Helper()
		bin := t.TempDir()
		log := filepath.Join(bin, "args")
		script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + "'" + log + "'\n"
		require.NoError(t, os.WriteFile(filepath.Join(bin, "go"), []byte(script), 0o700)) //nolint:gosec // 偽物の道具は実行できなければならない

		cmd := exec.CommandContext(t.Context(), "make", append([]string{"-C", filepath.Join("..", ".."), "-s", "--no-print-directory", "tag-version"}, args...)...)
		cmd.Env = slices.DeleteFunc(os.Environ(), func(kv string) bool {
			return strings.HasPrefix(kv, "MAKEFLAGS=") || strings.HasPrefix(kv, "MFLAGS=") || strings.HasPrefix(kv, "MAKELEVEL=") ||
				strings.HasPrefix(kv, "VERSION=") || strings.HasPrefix(kv, "RELEASE_TAG_VERSION=") || strings.HasPrefix(kv, "PATH=")
		})
		cmd.Env = append(cmd.Env, append(env, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))...)
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))

		raw, err := os.ReadFile(log)
		require.NoError(t, err)

		return strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("コマンドラインの VERSION を -version へ渡す", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, []string{"-C", "scripts", "run", "./release", "tag", "-version", "v0.1.0"}, recipe(t, []string{"VERSION=v0.1.0"}))
		})

		// シェルを経由すると、Go の検証より前に値の中のコマンドが走る。
		t.Run("VERSION の値はシェルに解釈させずにそのまま渡す", func(t *testing.T) {
			t.Parallel()
			marker := filepath.Join(t.TempDir(), "ran")
			value := `v1.0.0"; touch ` + marker + ` #`

			got := recipe(t, []string{"VERSION=" + value})
			assert.Equal(t, value, got[len(got)-1])
			assert.NoFileExists(t, marker)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		// 環境に残った VERSION を渡すと、引数なしの make tag-version がその版で取り消しの効かないタグを打ちうる。
		t.Run("環境の VERSION は渡さず空の -version にする", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, []string{"-C", "scripts", "run", "./release", "tag", "-version", ""}, recipe(t, nil, "VERSION=v9.9.9"))
		})

		t.Run("VERSION が無ければ空の -version にする", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, []string{"-C", "scripts", "run", "./release", "tag", "-version", ""}, recipe(t, nil))
		})
	})
}

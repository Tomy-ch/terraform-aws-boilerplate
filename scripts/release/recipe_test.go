package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// call は、偽物の git / gh が記録した1回の呼び出し。
type call struct {
	dir  string
	line string
}

// mirrorRoot は、リポジトリ直下を写して git init した一時ディレクトリを返す。
// make の入口とツールのソースは本物を読み、リリースノートと git の作業ツリーはこのディレクトリのものになる。
func mirrorRoot(t *testing.T) string {
	t.Helper()

	repo, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)
	root := t.TempDir()
	entries, err := os.ReadDir(repo)
	require.NoError(t, err)
	for _, e := range entries {
		if slices.Contains([]string{".git", ".github", "tmp"}, e.Name()) {
			continue
		}
		require.NoError(t, os.Symlink(filepath.Join(repo, e.Name()), filepath.Join(root, e.Name())))
	}
	out, err := exec.CommandContext(t.Context(), "git", "init", "-q", root).CombinedOutput()
	require.NoError(t, err, string(out))

	return root
}

func writeRootNote(t *testing.T, root, v string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(root, releaseNoteDir), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(root, notePath(v)), []byte("note\n"), 0o600))
}

// fakeTools は、呼び出しごとに作業ディレクトリと引数を1行ずつ記録する git / gh の偽物を PATH の先頭へ置く環境変数と、
// 記録を読む関数を返す。git の rev-parse だけは本物へ渡し、ほかは何もせず成功する。
func fakeTools(t *testing.T) (string, func() []call) {
	t.Helper()

	realGit, err := exec.LookPath("git")
	require.NoError(t, err)
	bin := t.TempDir()
	log := filepath.Join(bin, "calls")
	record := fmt.Sprintf("printf '%%s\\t%%s %%s\\n' \"$(pwd -P)\" \"$(basename \"$0\")\" \"$*\" >> %q\n", log)
	git := "#!/bin/sh\n" + record + fmt.Sprintf("if [ \"$1\" = rev-parse ]; then exec %q \"$@\"; fi\n", realGit)
	require.NoError(t, os.WriteFile(filepath.Join(bin, "git"), []byte(git), 0o700))                 //nolint:gosec // 偽物の道具は実行できなければならない
	require.NoError(t, os.WriteFile(filepath.Join(bin, "gh"), []byte("#!/bin/sh\n"+record), 0o700)) //nolint:gosec // 同上

	return "PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"), func() []call {
		raw, err := os.ReadFile(log)
		if os.IsNotExist(err) {
			return nil
		}
		require.NoError(t, err)
		var calls []call
		for line := range strings.SplitSeq(strings.TrimSuffix(string(raw), "\n"), "\n") {
			dir, rest, ok := strings.Cut(line, "\t")
			require.True(t, ok, line)
			calls = append(calls, call{dir: dir, line: rest})
		}
		return calls
	}
}

// lines は、記録から rev-parse を除いた呼び出しの並びを返す。rev-parse は直下の確認で、手順ではない。
func lines(calls []call) []string {
	got := make([]string, 0, len(calls))
	for _, c := range calls {
		if !strings.HasPrefix(c.line, "git rev-parse ") {
			got = append(got, c.line)
		}
	}
	return got
}

// realDir は、symlink をたどった絶対パスを返す。偽物は `pwd -P` を記録する。
func realDir(t *testing.T, dir string) string {
	t.Helper()
	got, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)
	return got
}

// TestTagVersionRecipe は、.makefiles/github/operation/release-tag.mk の tag-version を、直下を写した一時ディレクトリで
// 実際に走らせる。ツールは本物をビルドし、git / gh だけを偽物にする。版の検証は Test_resolveExplicit が持つ。
func TestTagVersionRecipe(t *testing.T) {
	t.Parallel()

	tagCreate := func(v string) string { return "git tag -a " + v + " -F " + notePath(v) }
	recipe := func(t *testing.T, root string, args []string, env ...string) (string, []call, error) {
		t.Helper()
		path, calls := fakeTools(t)
		cmd := exec.CommandContext(t.Context(), "make", append([]string{"-C", root, "-s", "--no-print-directory", "tag-version"}, args...)...)
		cmd.Env = slices.DeleteFunc(os.Environ(), func(kv string) bool {
			return strings.HasPrefix(kv, "MAKEFLAGS=") || strings.HasPrefix(kv, "MFLAGS=") || strings.HasPrefix(kv, "MAKELEVEL=") ||
				strings.HasPrefix(kv, "VERSION=") || strings.HasPrefix(kv, "RELEASE_TAG_VERSION=") || strings.HasPrefix(kv, "PATH=") ||
				strings.HasPrefix(kv, "GOFLAGS=")
		})
		// go build の VCS の刻印は git を呼ぶ。偽物の記録を手順の呼び出しだけにする。
		cmd.Env = append(cmd.Env, append(env, path, "GOFLAGS=-buildvcs=false")...)
		out, err := cmd.CombinedOutput()
		return string(out), calls(), err
	}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("直下のリリースノートを読み、直下でタグと Release の作成へ進む", func(t *testing.T) {
			t.Parallel()
			root := mirrorRoot(t)
			writeRootNote(t, root, "v0.1.0")

			out, calls, err := recipe(t, root, []string{"VERSION=v0.1.0"})
			require.NoError(t, err, out)
			got := lines(calls)
			assert.Contains(t, got, tagCreate("v0.1.0"))
			assert.Contains(t, got, "gh release create v0.1.0 --title v0.1.0 --notes-file "+notePath("v0.1.0"))
			for _, c := range calls {
				assert.Equal(t, realDir(t, root), c.dir, "相対パスのノートを渡す手順は直下で走る: %s", c.line)
			}
		})

		// シェルを経由すると、Go の検証より前に値の中のコマンドが走る。
		t.Run("VERSION の値はシェルに解釈させずにそのまま渡す", func(t *testing.T) {
			t.Parallel()
			marker := filepath.Join(t.TempDir(), "ran")
			value := `v1.0.0"; touch ` + marker + ` #`

			out, calls, err := recipe(t, mirrorRoot(t), []string{"VERSION=" + value})
			require.Error(t, err, out)
			assert.Contains(t, out, strconv.Quote(value), "値はそのまま -version に届き、形の検査で拒まれる")
			assert.NoFileExists(t, marker)
			assert.Empty(t, lines(calls))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("直下にリリースノートが無ければタグも push も Release も作らない", func(t *testing.T) {
			t.Parallel()
			root := mirrorRoot(t)
			writeRootNote(t, root, "v0.1.0")

			out, calls, err := recipe(t, root, []string{"VERSION=v0.2.0"})
			require.Error(t, err, out)
			assert.Contains(t, out, notePath("v0.2.0"))
			for _, line := range lines(calls) {
				assert.NotContains(t, line, "git tag -a", "タグを作らない")
				assert.NotContains(t, line, "git push", "push しない")
				assert.False(t, strings.HasPrefix(line, "gh "), "gh を呼ばない: %s", line)
			}
		})

		// 環境に残った VERSION を渡すと、引数なしの make tag-version がその版で取り消しの効かないタグを打ちうる。
		t.Run("環境の VERSION は渡さず空の -version にする", func(t *testing.T) {
			t.Parallel()
			root := mirrorRoot(t)
			writeRootNote(t, root, "v9.9.9")

			out, calls, err := recipe(t, root, nil, "VERSION=v9.9.9")
			require.Error(t, err, out)
			assert.Contains(t, out, `"": ❌ -version は vX.Y.Z の形で`)
			assert.Empty(t, lines(calls))
		})

		t.Run("VERSION が無ければ空の -version にする", func(t *testing.T) {
			t.Parallel()

			out, calls, err := recipe(t, mirrorRoot(t), nil)
			require.Error(t, err, out)
			assert.Contains(t, out, `"": ❌ -version は vX.Y.Z の形で`)
			assert.Empty(t, lines(calls))
		})
	})
}

// TestEntryRequiresRepoRoot は、ビルドした release を直下以外で起動したとき、git / gh の手順にもリリースノートの判定にも
// 入らずに止まることを見る。make の入口を経ない起動では、ノートの相対パスが別の場所を指す。
func TestEntryRequiresRepoRoot(t *testing.T) {
	t.Parallel()

	bin := filepath.Join(t.TempDir(), "release")
	out, err := exec.CommandContext(t.Context(), "go", "build", "-o", bin, ".").CombinedOutput()
	require.NoError(t, err, string(out))
	start := func(t *testing.T, dir string) (string, []call, error) {
		t.Helper()
		path, calls := fakeTools(t)
		cmd := exec.CommandContext(t.Context(), bin, "tag", "-version", "v0.1.0")
		cmd.Dir = dir
		cmd.Env = append(slices.DeleteFunc(os.Environ(), func(kv string) bool { return strings.HasPrefix(kv, "PATH=") }), path)
		out, err := cmd.CombinedOutput()
		return string(out), calls(), err
	}

	t.Run("直下の下では、そこにノートが在っても手順に入らない", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		out, err := exec.CommandContext(t.Context(), "git", "init", "-q", root).CombinedOutput()
		require.NoError(t, err, string(out))
		sub := filepath.Join(root, "scripts")
		writeRootNote(t, sub, "v0.1.0")

		got, calls, err := start(t, sub)
		require.Error(t, err, got)
		assert.Contains(t, got, "はリポジトリの直下ではありません")
		assert.Empty(t, lines(calls))
	})

	t.Run("直下を解決できなければ手順に入らない", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeRootNote(t, dir, "v0.1.0")

		got, calls, err := start(t, dir)
		require.Error(t, err, got)
		assert.Contains(t, got, "リポジトリの直下を解決できません")
		assert.Empty(t, lines(calls))
	})
}

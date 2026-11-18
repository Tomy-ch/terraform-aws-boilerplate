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
	git(t, root, "init", "-q")

	return root
}

func placeNotes(t *testing.T, dir string) string {
	t.Helper()
	noteDir := filepath.Join(dir, releaseNoteDir)
	require.NoError(t, os.MkdirAll(noteDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(noteDir, initialTag+".md"), []byte("keep"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(noteDir, "v1.0.0.md"), []byte("drop"), 0o600))
	return noteDir
}

// makeIn は、root で make を走らせ、出力を返す。gh は呼び出しを記録するだけの偽物にする —— make は読み込みの時点で
// `gh repo view` を走らせる。
func makeIn(t *testing.T, root string, args ...string) (string, error) {
	t.Helper()

	bin := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(bin, "gh"), []byte("#!/bin/sh\nexit 0\n"), 0o700)) //nolint:gosec // 偽物の道具は実行できなければならない
	cmd := exec.CommandContext(t.Context(), "make", append([]string{"-C", root, "-s", "--no-print-directory"}, args...)...)
	cmd.Env = append(slices.DeleteFunc(os.Environ(), func(kv string) bool {
		return strings.HasPrefix(kv, "MAKEFLAGS=") || strings.HasPrefix(kv, "MFLAGS=") || strings.HasPrefix(kv, "MAKELEVEL=") ||
			strings.HasPrefix(kv, "DRY_RUN=") || strings.HasPrefix(kv, "GOFLAGS=") || strings.HasPrefix(kv, "PATH=")
	}), "GOFLAGS=-buildvcs=false", "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// TestSetupRepoRecipe は、.makefiles/github/operation/setup-repository.mk の setup-repo と REPO_SETUP を、直下を写した
// 一時ディレクトリで走らせる。連鎖そのものは gh のログインとラベル・保護設定の書き換えを含むので、`make -n` の展開で見る。
func TestSetupRepoRecipe(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("連鎖は preflight・bootstrap・prune-release-notes の順に repo-setup を直下で起動する", func(t *testing.T) {
			t.Parallel()

			out, err := makeIn(t, mirrorRoot(t), "-n", "setup-repo")
			require.NoError(t, err, out)
			at := -1
			for _, sub := range []string{"preflight", "bootstrap", "prune-release-notes"} {
				i := strings.Index(out, `exec "./tmp/bin/repo-setup" `+sub+`'`)
				require.Greater(t, i, at, "%s が前の段より後に在る: %s", sub, out)
				at = i
			}
		})

		t.Run("REPO_SETUP の prune-release-notes は直下のノートを整理する", func(t *testing.T) {
			t.Parallel()
			root := mirrorRoot(t)
			noteDir := placeNotes(t, root)
			entry := "include .makefiles/runner.mk\ninclude .makefiles/github/operation/setup-repository.mk\n" +
				"prune-entry:\n\t@$(call REPO_SETUP,prune-release-notes)\n"
			require.NoError(t, os.WriteFile(filepath.Join(root, "prune.mk"), []byte(entry), 0o600))

			out, err := makeIn(t, root, "-f", "prune.mk", "prune-entry")
			require.NoError(t, err, out)
			assert.FileExists(t, filepath.Join(noteDir, initialTag+".md"))
			assert.NoFileExists(t, filepath.Join(noteDir, "v1.0.0.md"))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("DRY_RUN が立っていればツールを起動せずに止まる", func(t *testing.T) {
			t.Parallel()
			root := mirrorRoot(t)
			noteDir := placeNotes(t, root)

			out, err := makeIn(t, root, "setup-repo", "DRY_RUN=1")
			require.Error(t, err, out)
			assert.Contains(t, out, "DRY_RUN 未対応")
			assert.NoFileExists(t, filepath.Join(root, "tmp", "bin", "repo-setup"))
			assert.FileExists(t, filepath.Join(noteDir, "v1.0.0.md"))
		})
	})
}

// TestEntryRequiresRepoRoot は、ビルドした repo-setup を直下以外で起動したとき、ノートに触れずに止まることを見る。
// make の入口を経ない起動では、ノートの相対パスが別の場所を指し、ディレクトリが無いとして黙って成功する。
func TestEntryRequiresRepoRoot(t *testing.T) {
	t.Parallel()

	bin := filepath.Join(t.TempDir(), "repo-setup")
	out, err := exec.CommandContext(t.Context(), "go", "build", "-o", bin, ".").CombinedOutput()
	require.NoError(t, err, string(out))
	start := func(t *testing.T, dir string, env ...string) (string, error) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), bin, "prune-release-notes")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), env...)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	t.Run("直下の下では、そこのノートを消さない", func(t *testing.T) {
		t.Parallel()
		sub := filepath.Join(initRepo(t), "scripts")
		noteDir := placeNotes(t, sub)

		got, err := start(t, sub)
		require.Error(t, err, got)
		assert.Contains(t, got, "はリポジトリの直下ではありません")
		assert.FileExists(t, filepath.Join(noteDir, "v1.0.0.md"))
	})

	t.Run("直下を解決できなければノートを消さない", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		noteDir := placeNotes(t, dir)

		got, err := start(t, dir)
		require.Error(t, err, got)
		assert.Contains(t, got, "リポジトリの直下を解決できません")
		assert.FileExists(t, filepath.Join(noteDir, "v1.0.0.md"))
	})

	t.Run("GIT_DIR が立っていれば直下の下のノートを消さない", func(t *testing.T) {
		t.Parallel()
		root := initRepo(t)
		sub := filepath.Join(root, "scripts")
		noteDir := placeNotes(t, sub)

		got, err := start(t, sub, "GIT_DIR="+filepath.Join(root, ".git"))
		require.Error(t, err, got)
		assert.Contains(t, got, "GIT_DIR: が環境に立っています")
		assert.FileExists(t, filepath.Join(noteDir, "v1.0.0.md"))
	})
}

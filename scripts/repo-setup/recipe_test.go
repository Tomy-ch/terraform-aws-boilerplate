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

// TestPruneReleaseNotesRecipe は、.makefiles/github/operation/setup-repository.mk の REPO_SETUP を、直下を写した
// 一時ディレクトリで prune-release-notes として実際に走らせる。setup-repo の連鎖は gh のログインとラベル・保護設定の
// 書き換えを含むので、REPO_SETUP だけを呼ぶ makefile を置いて起動する。
func TestPruneReleaseNotesRecipe(t *testing.T) {
	t.Parallel()

	root := mirrorRoot(t)
	noteDir := placeNotes(t, root)
	entry := "include .makefiles/runner.mk\ninclude .makefiles/github/operation/setup-repository.mk\n" +
		"prune-entry:\n\t@$(call REPO_SETUP,prune-release-notes)\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, "prune.mk"), []byte(entry), 0o600))

	cmd := exec.CommandContext(t.Context(), "make", "-C", root, "-s", "--no-print-directory", "-f", "prune.mk", "prune-entry")
	cmd.Env = append(slices.DeleteFunc(os.Environ(), func(kv string) bool {
		return strings.HasPrefix(kv, "MAKEFLAGS=") || strings.HasPrefix(kv, "MFLAGS=") || strings.HasPrefix(kv, "MAKELEVEL=")
	}), "GOFLAGS=-buildvcs=false")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	assert.FileExists(t, filepath.Join(noteDir, initialTag+".md"))
	assert.NoFileExists(t, filepath.Join(noteDir, "v1.0.0.md"), "直下のノートを整理する")
}

// TestEntryRequiresRepoRoot は、ビルドした repo-setup を直下以外で起動したとき、ノートに触れずに止まることを見る。
// make の入口を経ない起動では、ノートの相対パスが別の場所を指し、ディレクトリが無いとして黙って成功する。
func TestEntryRequiresRepoRoot(t *testing.T) {
	t.Parallel()

	bin := filepath.Join(t.TempDir(), "repo-setup")
	out, err := exec.CommandContext(t.Context(), "go", "build", "-o", bin, ".").CombinedOutput()
	require.NoError(t, err, string(out))
	start := func(t *testing.T, dir string) (string, error) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), bin, "prune-release-notes")
		cmd.Dir = dir
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
}

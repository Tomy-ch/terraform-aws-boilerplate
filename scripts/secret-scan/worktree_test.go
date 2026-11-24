package main

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_runTrufflehogWorktree(t *testing.T) {
	t.Parallel()

	// worktreeRepo は、追跡ファイル・ignore されていない未追跡のファイル・ignore されたファイル・抑止に当たるファイル・
	// 削除した追跡ファイルを持つリポジトリである。
	worktreeRepo := func(t *testing.T) *repo {
		t.Helper()
		r := newRepo(t)
		require.NoError(t, os.Mkdir(filepath.Join(r.dir, "dir"), 0o750))
		r.write("dir/tracked", "x\n")
		r.write("gone", "x\n")
		r.write("base-excluded", "x\n")
		r.commit("files")
		require.NoError(t, os.Remove(filepath.Join(r.dir, "gone")))
		require.NoError(t, os.WriteFile(filepath.Join(r.dir, "untracked"), []byte("x\n"), 0o600))
		require.NoError(t, os.MkdirAll(filepath.Join(r.dir, "tmp"), 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(r.dir, "tmp", "ignored"), []byte("x\n"), 0o600))
		return r
	}

	t.Run("選んだファイルを stage へ置いて本走査し、stdout には本走査だけを出す", func(t *testing.T) {
		t.Parallel()
		r := worktreeRepo(t)
		s := newScanner(t)
		s.passingCanaries()
		s.set(stepTrufflehogFilesystem+"@stage", "out", "MAIN\n")
		var stdout bytes.Buffer
		require.NoError(t, runTrufflehogWorktree([]string{"-format", "text"}, &stdout, &bytes.Buffer{}, r.runner(s)))
		assert.Equal(t, "MAIN\n", stdout.String())
		assert.Equal(t, []string{"canary-git " + stepTrufflehogRangeJSON, "canary-fs " + stepTrufflehogFilesystemJSON, "stage " + stepTrufflehogFilesystem}, s.steps())
		assert.Equal(t, "./.gitignore\n./.gitleaks.toml\n./.gitleaksignore\n./.trufflehog-exclude-paths-history.txt\n./.trufflehog-exclude-paths.txt\n./base\n./dir/tracked\n./untracked\n",
			s.read("files-"+stepTrufflehogFilesystem+"@stage"), "削除した・ignore された・抑止に当たるファイルを外す")
		noWork(t, r.dir, "trufflehog-worktree")
	})

	t.Run("json の本走査は json の段で起動する", func(t *testing.T) {
		t.Parallel()
		r := worktreeRepo(t)
		s := newScanner(t)
		s.passingCanaries()
		require.NoError(t, runTrufflehogWorktree([]string{"-format", "json"}, &bytes.Buffer{}, &bytes.Buffer{}, r.runner(s)))
		assert.Equal(t, "stage "+stepTrufflehogFilesystemJSON, s.steps()[2])
	})

	t.Run("走査の対象が0件なら errNoTargets で、走査器を起動しない", func(t *testing.T) {
		t.Parallel()
		r := newRepo(t)
		require.NoError(t, os.WriteFile(filepath.Join(r.dir, trufflehogExclude), []byte(".\n"), 0o600))
		s := newScanner(t)
		s.passingCanaries()
		require.ErrorIs(t, runTrufflehogWorktree(nil, &bytes.Buffer{}, &bytes.Buffer{}, r.runner(s)), errNoTargets)
		assert.Nil(t, s.calls())
	})

	t.Run("抑止の正規表現を解釈できなければ errExclude で、走査器を起動しない", func(t *testing.T) {
		t.Parallel()
		r := newRepo(t)
		require.NoError(t, os.WriteFile(filepath.Join(r.dir, trufflehogExclude), []byte("(\n"), 0o600))
		s := newScanner(t)
		require.ErrorIs(t, runTrufflehogWorktree(nil, &bytes.Buffer{}, &bytes.Buffer{}, r.runner(s)), errExclude)
		assert.Nil(t, s.calls())
	})

	t.Run("canary が落ちれば本走査を起動しない", func(t *testing.T) {
		t.Parallel()
		r := worktreeRepo(t)
		s := newScanner(t)
		s.passingCanaries()
		s.set(stepTrufflehogFilesystemJSON+"@canary-fs", "rc", "0")
		require.ErrorIs(t, runTrufflehogWorktree(nil, &bytes.Buffer{}, &bytes.Buffer{}, r.runner(s)), errCanary)
		assert.Len(t, s.calls(), 2)
	})

	t.Run("本走査の所見は errFindings", func(t *testing.T) {
		t.Parallel()
		r := worktreeRepo(t)
		s := newScanner(t)
		s.passingCanaries()
		s.set(stepTrufflehogFilesystem+"@stage", "rc", "183")
		require.ErrorIs(t, runTrufflehogWorktree(nil, &bytes.Buffer{}, &bytes.Buffer{}, r.runner(s)), errFindings)
	})

	t.Run("リポジトリでなければ errInput", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(root, trufflehogExclude), nil, 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(root, trufflehogExcludeHistory), nil, 0o600))
		s := newScanner(t)
		err := runTrufflehogWorktree(nil, &bytes.Buffer{}, &bytes.Buffer{}, runner{tool: s.tool(), git: command, environ: os.Environ(), root: root})
		require.ErrorIs(t, err, errInput)
		assert.Nil(t, s.calls())
	})
}

func Test_parseExcludes(t *testing.T) {
	t.Parallel()

	t.Run("`#` で始まる行と空行だけを除き、前後の空白を落とさない", func(t *testing.T) {
		t.Parallel()
		got, err := parseExcludes("# c\n\n^a$\n b\n\t#x\n")
		require.NoError(t, err)
		var srcs []string
		for _, re := range got {
			srcs = append(srcs, re.String())
		}
		assert.Equal(t, []string{"^a$", " b", "\t#x"}, srcs)
	})

	t.Run("空なら0件", func(t *testing.T) {
		t.Parallel()
		got, err := parseExcludes("")
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("解釈できない行は errExclude", func(t *testing.T) {
		t.Parallel()
		_, err := parseExcludes("^a$\n[\n")
		require.ErrorIs(t, err, errExclude)
	})
}

func Test_excluded(t *testing.T) {
	t.Parallel()

	patterns := []*regexp.Regexp{regexp.MustCompile(`^\.github/actions-pin\.toml$`), regexp.MustCompile(`main_test\.go`)}
	assert.True(t, excluded(".github/actions-pin.toml", patterns))
	assert.True(t, excluded("scripts/x/main_test.go", patterns), "行のどこかに当たれば外す（grep -E と同じ）")
	assert.False(t, excluded(".github/actions-pin.toml.bak", patterns))
	assert.False(t, excluded("a", nil))
}

func Test_selectFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for _, p := range []string{"a", "dir/b", "skip/c"} {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, p)), 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(root, p), []byte("x"), 0o600))
	}
	require.NoError(t, os.Mkdir(filepath.Join(root, "gitlink"), 0o750))
	require.NoError(t, os.Symlink("a", filepath.Join(root, "link")))
	require.NoError(t, os.Symlink("nowhere", filepath.Join(root, "dangling")))
	skip := []*regexp.Regexp{regexp.MustCompile(`^skip/`)}

	t.Run("無いもの・gitlink・指す先の無い symlink・抑止に当たるものを外す", func(t *testing.T) {
		t.Parallel()
		got, err := selectFiles(root, "a\x00dir/b\x00skip/c\x00gone\x00gitlink\x00link\x00dangling\x00", skip)
		require.NoError(t, err)
		assert.Equal(t, []string{"a", "dir/b", "link"}, got)
	})

	t.Run("残りが0件なら errNoTargets", func(t *testing.T) {
		t.Parallel()
		for name, list := range map[string]string{"空の一覧": "", "すべて外れる": "skip/c\x00gone\x00dangling\x00"} {
			_, err := selectFiles(root, list, skip)
			require.ErrorIs(t, err, errNoTargets, name)
		}
	})
}

func Test_stageFiles(t *testing.T) {
	t.Parallel()

	t.Run("通常のファイルは同じ中身で、symlink は同じ指す先で置く", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, "d"), 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(root, "d", "f"), []byte("body"), 0o600))
		require.NoError(t, os.Symlink("d/f", filepath.Join(root, "l")))
		stage := filepath.Join(t.TempDir(), "stage")
		require.NoError(t, stageFiles(root, []string{"d/f", "l"}, stage))
		body, err := os.ReadFile(filepath.Join(stage, "d", "f"))
		require.NoError(t, err)
		assert.Equal(t, "body", string(body))
		target, err := os.Readlink(filepath.Join(stage, "l"))
		require.NoError(t, err)
		assert.Equal(t, "d/f", target)
	})

	t.Run("元が無ければ errInput", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, stageFiles(t.TempDir(), []string{"gone"}, t.TempDir()), errInput)
	})
}

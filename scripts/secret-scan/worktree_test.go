package main

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/testenv"
)

func Test_runTrufflehogWorktree(t *testing.T) {
	t.Parallel()

	t.Run("置けた対象が0件（root の外を指す symlink だけ）なら errNoTargets で、本走査を起動しない", func(t *testing.T) {
		t.Parallel()
		r := newRepo(t)
		require.NoError(t, os.WriteFile(filepath.Join(r.dir, trufflehogExclude), []byte("^(\\.|base$)\n"), 0o600))
		outside := filepath.Join(t.TempDir(), "o")
		require.NoError(t, os.WriteFile(outside, nil, 0o600))
		require.NoError(t, os.Symlink(outside, filepath.Join(r.dir, "out")))
		s := newScanner(t)
		s.passingCanaries()
		var log bytes.Buffer
		require.ErrorIs(t, runTrufflehogWorktree(nil, &bytes.Buffer{}, &log, r.runner(s)), errNoTargets)
		assert.Equal(t, []string{"canary-git " + stepTrufflehogRangeJSON, "canary-fs " + stepTrufflehogFilesystemJSON}, s.steps(), "本走査を起動した")
		assert.Contains(t, log.String(), "root の外を指す symlink out")
		noWork(t, r.dir, "trufflehog-worktree")
	})

	t.Run("GIT_DIR が外側を指していても、外側を書き換えず、走査器へ GIT_DIR を渡さない", func(t *testing.T) {
		t.Parallel()
		r := newRepo(t)
		outer := newFixture(t)
		before := outer.snapshot()
		s := newScanner(t)
		s.passingCanaries()
		run := r.runner(s)
		run.environ = leaked(outer.dir)
		require.NoError(t, runTrufflehogWorktree(nil, &bytes.Buffer{}, &bytes.Buffer{}, run))
		assert.Equal(t, before, outer.snapshot())
		calls := s.calls()
		require.Len(t, calls, 3)
		for _, c := range calls {
			assert.Empty(t, c.gitDir, "%s へ GIT_DIR を渡した", c.step)
		}
		assert.Contains(t, s.read("files-"+stepTrufflehogFilesystem+"@stage"), "./base\n", "root の一覧を読んだ")
	})

	// worktreeRepo は、追跡ファイル・ignore されていない未追跡のファイル・ignore されたファイル・範囲の抑止に当たるファイル・
	// 全履歴だけの抑止に当たるファイル・削除した追跡ファイルを持つリポジトリである。
	worktreeRepo := func(t *testing.T) *repo {
		t.Helper()
		r := newRepo(t)
		require.NoError(t, os.Mkdir(filepath.Join(r.dir, "dir"), 0o750))
		r.write("dir/tracked", "x\n")
		r.write("gone", "x\n")
		r.write("base-excluded", "x\n")
		r.write("history-excluded", "x\n")
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
		assert.Equal(t, "./.gitignore\n./.gitleaks.toml\n./.gitleaksignore\n./.trufflehog-exclude-paths-history.txt\n./.trufflehog-exclude-paths.txt\n./base\n./dir/tracked\n./history-excluded\n./untracked\n",
			s.read("files-"+stepTrufflehogFilesystem+"@stage"), "削除した・ignore された・範囲の抑止に当たるファイルを外し、全履歴だけの抑止に当たるものは残す")
		assert.Equal(t, "# base\n^base-excluded$\n# history\n^history-excluded$\n", s.read("exclude-"+stepTrufflehogRangeJSON+"@canary-git"), "canary は和集合を読む")
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
		var log bytes.Buffer
		got, err := selectFiles(root, "a\x00dir/b\x00skip/c\x00gone\x00gitlink\x00link\x00dangling\x00", skip, &log)
		require.NoError(t, err)
		assert.Equal(t, []string{"a", "dir/b", "link"}, got)
		assert.Equal(t, "trufflehog-worktree: 辿れない symlink dangling を走査の対象に置きません\n", log.String())
	})

	t.Run("ErrNotExist 以外の lstat の失敗（親が通常のファイル）は errInput", func(t *testing.T) {
		t.Parallel()
		_, err := selectFiles(root, "a\x00a/x\x00", nil, &bytes.Buffer{})
		require.ErrorIs(t, err, errInput)
	})

	for name, link := range map[string]func(dir string){
		"指す先が無い（ENOENT）":         func(dir string) { require.NoError(t, os.Symlink("nowhere", filepath.Join(dir, "bad"))) },
		"指す先の親が通常のファイル（ENOTDIR）": func(dir string) { require.NoError(t, os.Symlink("f/x", filepath.Join(dir, "bad"))) },
		"循環（ELOOP）": func(dir string) {
			require.NoError(t, os.Symlink("bad2", filepath.Join(dir, "bad")))
			require.NoError(t, os.Symlink("bad", filepath.Join(dir, "bad2")))
		},
	} {
		t.Run("辿れない symlink は飛ばし、log に名前を出す: "+name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, "f"), nil, 0o600))
			link(dir)
			var log bytes.Buffer
			got, err := selectFiles(dir, "f\x00bad\x00", nil, &log)
			require.NoError(t, err)
			assert.Equal(t, []string{"f"}, got)
			assert.Equal(t, "trufflehog-worktree: 辿れない symlink bad を走査の対象に置きません\n", log.String())
		})
	}

	t.Run("symlink の指す先のそれ以外の stat の失敗（EACCES）は errInput", func(t *testing.T) {
		t.Parallel()
		testenv.RequireNonRoot(t, "読めないディレクトリの下を stat できないことを作る")
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "f"), nil, 0o600))
		require.NoError(t, os.Mkdir(filepath.Join(dir, "locked"), 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "locked", "x"), nil, 0o600))
		require.NoError(t, os.Chmod(filepath.Join(dir, "locked"), 0o000))
		t.Cleanup(func() { _ = os.Chmod(filepath.Join(dir, "locked"), 0o700) })
		require.NoError(t, os.Symlink("locked/x", filepath.Join(dir, "bad")))
		_, err := selectFiles(dir, "f\x00bad\x00", nil, &bytes.Buffer{})
		require.ErrorIs(t, err, errInput)
	})

	t.Run("残りが0件なら errNoTargets", func(t *testing.T) {
		t.Parallel()
		for name, list := range map[string]string{"空の一覧": "", "すべて外れる": "skip/c\x00gone\x00dangling\x00"} {
			_, err := selectFiles(root, list, skip, &bytes.Buffer{})
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
		var log bytes.Buffer
		placed, err := stageFiles(root, []string{"d/f", "l"}, stage, &log)
		require.NoError(t, err)
		assert.Equal(t, 2, placed)
		assert.Empty(t, log.String())
		body, err := os.ReadFile(filepath.Join(stage, "d", "f"))
		require.NoError(t, err)
		assert.Equal(t, "body", string(body))
		target, err := os.Readlink(filepath.Join(stage, "l"))
		require.NoError(t, err)
		assert.Equal(t, "d/f", target)
	})

	t.Run("元が無ければ errInput", func(t *testing.T) {
		t.Parallel()
		_, err := stageFiles(t.TempDir(), []string{"gone"}, t.TempDir(), &bytes.Buffer{})
		require.ErrorIs(t, err, errInput)
	})

	t.Run("hard link を張れなければ複写で置く", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(root, "f"), []byte("body"), 0o600))
		stage := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(stage, "f"), []byte("old"), 0o600), "置き場が埋まっていて link が失敗する")
		placed, err := stageFiles(root, []string{"f"}, stage, &bytes.Buffer{})
		require.NoError(t, err)
		assert.Equal(t, 1, placed)
		body, err := os.ReadFile(filepath.Join(stage, "f"))
		require.NoError(t, err)
		assert.Equal(t, "body", string(body))
		src, err := os.Stat(filepath.Join(root, "f"))
		require.NoError(t, err)
		dst, err := os.Stat(filepath.Join(stage, "f"))
		require.NoError(t, err)
		assert.False(t, os.SameFile(src, dst), "複写である")
	})

	t.Run("symlink を置けなければ errInput", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(root, "f"), nil, 0o600))
		require.NoError(t, os.Symlink("f", filepath.Join(root, "l")))
		stage := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(stage, "l"), nil, 0o600))
		_, err := stageFiles(root, []string{"l"}, stage, &bytes.Buffer{})
		require.ErrorIs(t, err, errInput)
	})

	for name, target := range map[string]func(root, outside string) string{
		"絶対パスで外を指す":        func(_, outside string) string { return outside },
		"相対パスで root の外へ出る": func(root, outside string) string { rel, _ := filepath.Rel(root, outside); return rel },
	} {
		t.Run(name+" symlink は置かずに飛ばし、log に名前を出す", func(t *testing.T) {
			t.Parallel()
			root, outsideDir := t.TempDir(), t.TempDir()
			outside := filepath.Join(outsideDir, "o")
			require.NoError(t, os.WriteFile(outside, nil, 0o600))
			require.NoError(t, os.WriteFile(filepath.Join(root, "f"), nil, 0o600))
			require.NoError(t, os.Symlink(target(root, outside), filepath.Join(root, "out")))
			stage := t.TempDir()
			var log bytes.Buffer
			placed, err := stageFiles(root, []string{"f", "out"}, stage, &log)
			require.NoError(t, err)
			assert.Equal(t, 1, placed, "外を指す symlink は数えない")
			_, err = os.Lstat(filepath.Join(stage, "out"))
			require.ErrorIs(t, err, os.ErrNotExist)
			assert.FileExists(t, filepath.Join(stage, "f"))
			assert.Contains(t, log.String(), "out")
		})
	}
}

func Test_linkStaysInside(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "d"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(root, "f"), nil, 0o600))
	outside := filepath.Join(t.TempDir(), "o")
	require.NoError(t, os.WriteFile(outside, nil, 0o600))
	require.NoError(t, os.Symlink("../f", filepath.Join(root, "d", "up")))
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "abs")))
	require.NoError(t, os.Symlink("hop", filepath.Join(root, "chain")))
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "hop")))

	for name, c := range map[string]struct {
		p, target string
		want      bool
	}{
		"親を経て root の下":      {"d/up", "../f", true},
		"絶対パス":              {"abs", outside, false},
		"字面で root の外へ出る":    {"f", "../x", false},
		"字面は下でも解決した先が外（連鎖）": {"chain", "hop", false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := linkStaysInside(root, c.p, c.target)
			require.NoError(t, err)
			assert.Equal(t, c.want, got)
		})
	}

	t.Run("解決できなければ errInput", func(t *testing.T) {
		t.Parallel()
		_, err := linkStaysInside(root, "gone", "x")
		require.ErrorIs(t, err, errInput)
	})
}

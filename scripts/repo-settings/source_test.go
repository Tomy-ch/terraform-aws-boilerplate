package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const sourceBranches = `{"deploy": [{"branch": "admin", "env": "admin", "from": ["release/"]}, {"branch": "production", "release_base": true}], "lines": {"release": "release/", "hotfix": "hotfix/"}, "line_version": "v<major>.<minor>.<patch>", "revert": "revert/"}`

func gitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	return strings.TrimSpace(string(out))
}

// sourceRepo は、デプロイブランチ admin の先頭 base と、デフォルトブランチ release/v1（origin の ref）の先頭 head を
// 持つ一時のリポジトリ。evil は base の上の、線に無い commit、ahead は線に無い変更を足した admin の commit、
// clash は head と同じ行を違う値に変えた admin の commit である。
type sourceRepo struct {
	dir                            string
	base, head, evil, ahead, clash string
}

func newSourceRepo(t *testing.T) sourceRepo {
	t.Helper()
	dir := t.TempDir()
	gitT(t, dir, "init", "-q", "-b", "admin")
	commit := func(path, body, msg string) string {
		writeFile(t, dir, path, body)
		gitT(t, dir, "add", path)
		gitT(t, dir, "commit", "-q", "-m", msg)
		return gitT(t, dir, "rev-parse", "HEAD")
	}
	writeFile(t, dir, branchesFile, sourceBranches)
	gitT(t, dir, "add", branchesFile)
	r := sourceRepo{dir: dir, base: commit("main.tf", "a\n", "base")}
	gitT(t, dir, "switch", "-q", "-c", "rel")
	r.head = commit("main.tf", "b\n", "release")
	gitT(t, dir, "update-ref", "refs/remotes/origin/release/v1", r.head)
	gitT(t, dir, "update-ref", "refs/remotes/origin/hotfix/v1", r.base)
	gitT(t, dir, "switch", "-q", "-c", "evil", r.base)
	r.evil = commit("main.tf", "evil\n", "evil")
	gitT(t, dir, "switch", "-q", "-c", "ahead", r.base)
	r.ahead = commit("extra.tf", "x\n", "ahead")
	gitT(t, dir, "switch", "-q", "-c", "clash", r.base)
	r.clash = commit("main.tf", "c\n", "clash")
	gitT(t, dir, "switch", "-q", "admin")
	return r
}

func Test_checkSource(t *testing.T) {
	t.Parallel()

	r := newSourceRepo(t)
	req := func(base, baseSHA, headSHA, def string) sourceRequest {
		return sourceRequest{base: base, baseSHA: baseSHA, headSHA: headSHA, def: def}
	}

	t.Run("デフォルトブランチから到達でき、merge の木が head の木と一致すれば通る", func(t *testing.T) {
		t.Parallel()
		var out bytes.Buffer
		require.NoError(t, checkSource(r.dir, req("admin", r.base, r.head, "release/v1"), realGit, &out))
		assert.Contains(t, out.String(), "release/v1 から到達でき")
	})
	t.Run("デプロイブランチでない base は対象外として通り、git を呼ばない", func(t *testing.T) {
		t.Parallel()
		git, called := recordingGit()
		var out bytes.Buffer
		require.NoError(t, checkSource(r.dir, req("release/v1", "", "", ""), git, &out))
		assert.Contains(t, out.String(), "対象外")
		assert.False(t, *called)
	})
	t.Run("env を持たないデプロイブランチも対象外である", func(t *testing.T) {
		t.Parallel()
		git, called := recordingGit()
		var out bytes.Buffer
		require.NoError(t, checkSource(r.dir, req("production", "", "", ""), git, &out))
		assert.Contains(t, out.String(), "対象外")
		assert.False(t, *called)
	})

	rejected := map[string]struct {
		req  sourceRequest
		want string
	}{
		// 書き込み権限で release/x を作っても、その commit はデフォルトブランチから到達できない。
		"デフォルトブランチから到達できない commit は拒む":         {req("admin", r.base, r.evil, "release/v1"), "から到達できません"},
		"デフォルトブランチが from の線でなければ拒む（hotfix の間）": {req("admin", r.base, r.base, "hotfix/v1"), "デフォルトブランチ hotfix/v1 は admin へ変更を運ぶ線ではありません"},
		"デフォルトブランチがデプロイブランチなら拒む":               {req("admin", r.base, r.head, "production"), "デフォルトブランチ production は admin へ変更を運ぶ線ではありません"},
		"デプロイブランチが線に無い変更を持てば拒む":                {req("admin", r.ahead, r.head, "release/v1"), "一致しません"},
		"merge が衝突すれば拒む":                       {req("admin", r.clash, r.head, "release/v1"), "衝突します"},
	}
	for name, tc := range rejected {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := checkSource(r.dir, tc.req, realGit, &bytes.Buffer{})
			require.ErrorIs(t, err, errSource)
			assert.Contains(t, err.Error(), tc.want)
		})
	}

	usage := map[string]sourceRequest{
		"base の SHA が名前":    req("admin", "main", r.head, "release/v1"),
		"base の SHA が空":     req("admin", "", r.head, "release/v1"),
		"head の SHA が空":     req("admin", r.base, "", "release/v1"),
		"head の SHA が 39 桁": req("admin", r.base, r.head[:39], "release/v1"),
		"head の SHA が大文字":   req("admin", r.base, strings.ToUpper(r.head), "release/v1"),
		"デフォルトブランチが空":       req("admin", r.base, r.head, ""),
	}
	for name, rq := range usage {
		t.Run("使い方の誤りにする（"+name+"）", func(t *testing.T) {
			t.Parallel()
			git, called := recordingGit()
			require.ErrorIs(t, checkSource(r.dir, rq, git, &bytes.Buffer{}), errUsage)
			assert.False(t, *called)
		})
	}
	t.Run("デフォルトブランチの ref が無ければ、到達できないと読まずに失敗する", func(t *testing.T) {
		t.Parallel()
		err := checkSource(r.dir, req("admin", r.base, r.head, "release/v9"), realGit, &bytes.Buffer{})
		require.ErrorIs(t, err, errSourceGit)
	})
	t.Run("木の SHA を読めなければ失敗する", func(t *testing.T) {
		t.Parallel()
		git := realGit
		git.tree = func(string, string) (string, error) { return "not-a-tree\n", nil }
		err := checkSource(r.dir, req("admin", r.base, r.head, "release/v1"), git, &bytes.Buffer{})
		require.ErrorIs(t, err, errSourceGit)
	})
	t.Run("merge-tree が 1 以外で失敗すれば衝突と読まずに失敗する", func(t *testing.T) {
		t.Parallel()
		git := realGit
		git.mergeTree = func(string, string, string) (string, error) {
			return "", xerrors.Wrap(fakeExitErr{code: 128}, "merge-tree")
		}
		err := checkSource(r.dir, req("admin", r.base, r.head, "release/v1"), git, &bytes.Buffer{})
		require.ErrorIs(t, err, errSourceGit)
	})
	t.Run("head の木を読めなければ、拒否と読まずに失敗する", func(t *testing.T) {
		t.Parallel()
		git := realGit
		git.tree = func(string, string) (string, error) { return "", xerrors.Wrap(fakeExitErr{code: 128}, "rev-parse") }
		err := checkSource(r.dir, req("admin", r.base, r.head, "release/v1"), git, &bytes.Buffer{})
		require.ErrorIs(t, err, errSourceGit)
		require.NotErrorIs(t, err, errSource)
	})
	for name, merged := range map[string]string{"空": "", "衝突の報告だけ": "CONFLICT x\n"} {
		// merge-tree の出力の先頭行を木と読まずに比べると、読めない出力が一致と不一致のどちらかへ落ちる。
		t.Run("merge-tree の出力が木の SHA でなければ失敗する（"+name+"）", func(t *testing.T) {
			t.Parallel()
			git := realGit
			git.mergeTree = func(string, string, string) (string, error) { return merged, nil }
			err := checkSource(r.dir, req("admin", r.base, r.head, "release/v1"), git, &bytes.Buffer{})
			require.ErrorIs(t, err, errSourceGit)
		})
	}
	t.Run("祖先の判定が終了コードを持たずに失敗すれば、到達できないと読まずに失敗する", func(t *testing.T) {
		t.Parallel()
		git := realGit
		git.isAncestor = func(string, string, string) error { return errors.New("git が見つかりません") }
		err := checkSource(r.dir, req("admin", r.base, r.head, "release/v1"), git, &bytes.Buffer{})
		require.ErrorIs(t, err, errSourceGit)
		require.NotErrorIs(t, err, errSource)
	})
	t.Run("宣言を読めなければ失敗する", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, checkSource(t.TempDir(), req("admin", r.base, r.head, "release/v1"), realGit, &bytes.Buffer{}), os.ErrNotExist)
	})
}

func recordingGit() (gitOps, *bool) {
	called := new(bool)
	return gitOps{
		isAncestor: func(string, string, string) error { *called = true; return nil },
		mergeTree:  func(string, string, string) (string, error) { *called = true; return "", nil },
		tree:       func(string, string) (string, error) { *called = true; return "", nil },
	}, called
}

type fakeExitErr struct{ code int }

func (e fakeExitErr) Error() string { return "exit status" }
func (e fakeExitErr) ExitCode() int { return e.code }

func Test_gitIsAncestor(t *testing.T) {
	t.Parallel()

	r := newSourceRepo(t)
	t.Run("祖先なら成功する", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, gitIsAncestor(r.dir, r.base, "refs/remotes/origin/release/v1"))
	})
	t.Run("祖先でなければ終了コード 1 を呼び出し側へ渡す", func(t *testing.T) {
		t.Parallel()
		err := gitIsAncestor(r.dir, r.evil, "refs/remotes/origin/release/v1")
		var code exitCoder
		require.ErrorAs(t, err, &code)
		assert.Equal(t, notAncestor, code.ExitCode())
	})
	t.Run("ref が無いことを「祖先でない」と区別する", func(t *testing.T) {
		t.Parallel()
		err := gitIsAncestor(r.dir, r.base, "refs/remotes/origin/none")
		var code exitCoder
		require.ErrorAs(t, err, &code)
		assert.NotEqual(t, notAncestor, code.ExitCode())
	})
}

func Test_gitMergeTree(t *testing.T) {
	t.Parallel()

	r := newSourceRepo(t)
	t.Run("fast-forward になる merge の木は head の木", func(t *testing.T) {
		t.Parallel()
		out, err := gitMergeTree(r.dir, r.base, r.head)
		require.NoError(t, err)
		assert.Equal(t, gitT(t, r.dir, "rev-parse", r.head+"^{tree}"), strings.TrimSpace(out))
	})
	t.Run("衝突は終了コード 1", func(t *testing.T) {
		t.Parallel()
		_, err := gitMergeTree(r.dir, r.clash, r.head)
		var code exitCoder
		require.ErrorAs(t, err, &code)
		assert.Equal(t, notAncestor, code.ExitCode())
	})
}

func Test_gitTree(t *testing.T) {
	t.Parallel()

	r := newSourceRepo(t)
	t.Run("commit の木の SHA を返す", func(t *testing.T) {
		t.Parallel()
		got, err := gitTree(r.dir, r.head)
		require.NoError(t, err)
		assert.Equal(t, gitT(t, r.dir, "rev-parse", r.head+"^{tree}")+"\n", got)
	})
	t.Run("無い commit は終了コードを持って失敗する", func(t *testing.T) {
		t.Parallel()
		_, err := gitTree(r.dir, strings.Repeat("0", 40))
		var code exitCoder
		require.ErrorAs(t, err, &code)
		assert.NotZero(t, code.ExitCode())
	})
}

func Test_deployBranch(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, root, branchesFile, sourceBranches)
	t.Run("env のデプロイブランチの名前を1行で書く", func(t *testing.T) {
		t.Parallel()
		var out bytes.Buffer
		require.NoError(t, deployBranch(root, "admin", &out))
		assert.Equal(t, "admin\n", out.String())
	})
	t.Run("env を持たないデプロイブランチは返さない", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, deployBranch(root, "prd", &bytes.Buffer{}), errNoDeployEnv)
	})
	t.Run("宣言を読めなければ失敗する", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, deployBranch(filepath.Join(root, "none"), "admin", &bytes.Buffer{}), os.ErrNotExist)
	})
}

func Test_git(t *testing.T) {
	t.Parallel()

	r := newSourceRepo(t)
	t.Run("標準出力を返す", func(t *testing.T) {
		t.Parallel()
		got, err := git(r.dir, "rev-parse", "admin")
		require.NoError(t, err)
		assert.Equal(t, r.base+"\n", got)
	})
	t.Run("終了コードを呼び出し側へ渡す", func(t *testing.T) {
		t.Parallel()
		_, err := git(r.dir, "merge-base", "--is-ancestor", r.evil, "refs/remotes/origin/release/v1")
		var code exitCoder
		require.ErrorAs(t, err, &code)
		assert.Equal(t, notAncestor, code.ExitCode())
	})
}

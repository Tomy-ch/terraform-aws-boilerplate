package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPushRecipes は、.makefiles/github/operation/apply-revert.mk の push の入口を一時のリポジトリで実際に走らせ、
// 決めた範囲をそのまま revert して push すること、既存のブランチを書き換えないこと、token を出力へ出さないことを
// 見る。範囲と木の一致の判定は apply-revert のテストが持つ。
func TestPushRecipes(t *testing.T) {
	t.Parallel()

	const token = "TOKEN-SECRET"
	realGit, err := exec.LookPath("git")
	require.NoError(t, err)
	// withGitLog は、PATH の先頭へ git の偽物を置く環境変数と、その記録ファイルを返す。偽物は呼び出しごとに引数と、
	// 資格情報の環境変数（GIT_CONFIG_VALUE_0）を受け取ったかを1行に書き、実物の git へ渡す。
	withGitLog := func(t *testing.T) (string, string) {
		t.Helper()
		bin := t.TempDir()
		log := filepath.Join(bin, "calls")
		script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\tcred=%%s\\n' \"$*\" \"${GIT_CONFIG_VALUE_0:+1}\" >> %q\nexec %q \"$@\"\n", log, realGit)
		require.NoError(t, os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o700)) //nolint:gosec // 偽物の道具は実行できなければならない
		return "PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"), log
	}
	recipe := func(t *testing.T, h history, target string, env ...string) (string, error) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "make", "-C", filepath.Join("..", ".."), "-s", "--no-print-directory", target,
			"RUNNER_MODE=host", "APPLY_REVERT_REPO="+h.dir)
		cmd.Env = append(slices.DeleteFunc(os.Environ(), func(kv string) bool {
			return strings.HasPrefix(kv, "MAKEFLAGS=") || strings.HasPrefix(kv, "MFLAGS=") || strings.HasPrefix(kv, "MAKELEVEL=") ||
				strings.HasPrefix(kv, "GH_TOKEN=") || strings.HasPrefix(kv, "REVERT_") || strings.HasPrefix(kv, "GIT_CONFIG_")
		}), "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com", "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com")
		cmd.Env = append(cmd.Env, env...)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	push := func(t *testing.T, h history, target string, env ...string) (string, string, error) {
		t.Helper()
		remote := filepath.Join(t.TempDir(), "remote.git")
		git(t, h.dir, "init", "-q", "--bare", remote)
		git(t, h.dir, "remote", "add", "origin", remote)
		git(t, h.dir, "push", "-q", "origin", "line", "wf-line")
		out, err := recipe(t, h, target, env...)
		return out, remote, err
	}
	inputs := func(h history) []string {
		return []string{"REVERT_HEAD=" + git(t, h.dir, "rev-parse", h.head+"^2"), "REVERT_COMMITS=" + h.head + " " + h.first, "REVERT_BRANCH=revert/apply-42"}
	}
	branchTree := func(t *testing.T, remote string) string {
		t.Helper()
		return git(t, remote, "rev-parse", "refs/heads/revert/apply-42^{tree}")
	}

	t.Run("CI の入口は範囲を新しい順に revert し、木が一致すれば push する", func(t *testing.T) {
		t.Parallel()
		h := newHistory(t)
		path, log := withGitLog(t)
		out, remote, err := push(t, h, "ci-revert-push", append(inputs(h), "GH_TOKEN="+token, path)...)
		require.NoError(t, err, out)
		assert.Equal(t, git(t, h.dir, "rev-parse", h.base+"^{tree}"), branchTree(t, remote))
		subjects := git(t, remote, "log", "--format=%s|%an", "-2", "refs/heads/revert/apply-42")
		assert.Equal(t, "Revert \"Merge one\"|github-actions[bot]\nRevert \"Merge two\"|github-actions[bot]", subjects, "git revert の標準の題で、bot が作者になる")
		assert.Contains(t, out, "✅ revert/apply-42 を push しました")
		auth := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + token))
		assert.Contains(t, out, "::add-mask::"+auth, "push の資格情報を出力で伏せる")
		raw, err := os.ReadFile(log)
		require.NoError(t, err)
		calls := strings.Split(strings.TrimSpace(string(raw)), "\n")
		pushes := 0
		for _, c := range calls {
			assert.NotContains(t, c, token, "token を argv に置かない")
			assert.NotContains(t, c, auth, "資格情報を argv に置かない")
			if strings.Contains(c, " push ") {
				pushes++
				assert.True(t, strings.HasSuffix(c, "cred=1"), "push は資格情報を環境変数で受ける: %s", c)
				continue
			}
			assert.True(t, strings.HasSuffix(c, "cred="), "push のほかの git は資格情報を受けない: %s", c)
		}
		assert.Equal(t, 1, pushes, calls)
		assert.Equal(t, 1, strings.Count(git(t, h.dir, "worktree", "list"), "\n")+1, "一時の worktree を残さない")
	})
	t.Run("同じ名前のブランチが在れば上書きしない", func(t *testing.T) {
		t.Parallel()
		h := newHistory(t)
		out, remote, err := push(t, h, "ci-revert-push", append(inputs(h), "GH_TOKEN="+token)...)
		require.NoError(t, err, out)
		before := git(t, remote, "rev-parse", "refs/heads/revert/apply-42")
		// commit の日時を変え、同じ範囲でも違う commit になる再実行を作る。
		again, err := recipe(t, h, "ci-revert-push", append(inputs(h), "GH_TOKEN="+token, "GIT_COMMITTER_DATE=2001-01-01T00:00:00Z")...)
		require.Error(t, err, again)
		assert.Contains(t, again, "rejected", "push が既存のブランチの更新として拒まれる")
		assert.Equal(t, before, git(t, remote, "rev-parse", "refs/heads/revert/apply-42"))
	})
	for _, missing := range []string{"REVERT_HEAD", "REVERT_COMMITS", "REVERT_BRANCH"} {
		t.Run(missing+" が無ければ何もしない", func(t *testing.T) {
			t.Parallel()
			h := newHistory(t)
			env := slices.DeleteFunc(inputs(h), func(kv string) bool { return strings.HasPrefix(kv, missing+"=") })
			out, remote, err := push(t, h, "ci-revert-push", append(env, "GH_TOKEN="+token)...)
			require.Error(t, err, out)
			assert.Contains(t, out, missing+" がありません")
			assert.Empty(t, git(t, remote, "for-each-ref", "refs/heads/revert/"))
		})
	}
	t.Run("CI の入口は GH_TOKEN が無ければ何もしない", func(t *testing.T) {
		t.Parallel()
		h := newHistory(t)
		out, remote, err := push(t, h, "ci-revert-push", inputs(h)...)
		require.Error(t, err, out)
		assert.Contains(t, out, "GH_TOKEN がありません")
		assert.Empty(t, git(t, remote, "for-each-ref", "refs/heads/revert/"))
	})
	t.Run("手元の入口は手元の作者で push する", func(t *testing.T) {
		t.Parallel()
		h := newHistory(t)
		out, remote, err := push(t, h, "apply-revert", inputs(h)...)
		require.NoError(t, err, out)
		assert.Equal(t, git(t, h.dir, "rev-parse", h.base+"^{tree}"), branchTree(t, remote))
		assert.Equal(t, "t", git(t, remote, "log", "--format=%an", "-1", "refs/heads/revert/apply-42"))
	})
}

// TestPlanRecipe は、.makefiles/github/operation/apply-revert.mk の ci-revert-plan が workflow の環境変数を
// apply-revert の引数へ正しく渡すことを、展開した引数列を parseOptions に通して見る。flag の名前と環境変数の
// 名前のずれは、ここで落ちなければ CI で初めて落ちる。
func TestPlanRecipe(t *testing.T) {
	t.Parallel()

	cmd := exec.CommandContext(t.Context(), "make", "-C", filepath.Join("..", ".."), "-s", "--no-print-directory", "-n", "ci-revert-plan", "RUNNER_MODE=host")
	cmd.Env = slices.DeleteFunc(os.Environ(), func(kv string) bool {
		return strings.HasPrefix(kv, "MAKEFLAGS=") || strings.HasPrefix(kv, "MFLAGS=") || strings.HasPrefix(kv, "MAKELEVEL=")
	})
	expanded, err := cmd.CombinedOutput()
	require.NoError(t, err, string(expanded))
	const marker = `exec "./tmp/bin/apply-revert" `
	i := strings.Index(string(expanded), marker)
	require.GreaterOrEqual(t, i, 0, string(expanded))
	argv := strings.TrimSuffix(strings.TrimSpace(string(expanded)[i+len(marker):]), "'")

	head := strings.Repeat("a", 40)
	out := filepath.Join(t.TempDir(), "out")
	sh := exec.CommandContext(t.Context(), "sh", "-c", `eval "set -- $ARGV"; printf '%s\n' "$@"`)
	sh.Env = append(os.Environ(), "ARGV="+argv, "GITHUB_REPOSITORY=owner/name", "GITHUB_REF_NAME=admin", "DEFAULT_BRANCH=release/v0.1.0", "GITHUB_SHA="+head,
		"GITHUB_RUN_ID=42", "APPLY_OUTCOME=failure", "APPLY_FAILURE=policy", "GITHUB_SERVER_URL=https://github.com", "GITHUB_OUTPUT="+out)
	split, err := sh.Output()
	require.NoError(t, err)
	args := strings.Split(strings.TrimSuffix(string(split), "\n"), "\n")
	require.Equal(t, "plan", args[0])

	o, err := parseOptions(args[1:])
	require.NoError(t, err, args)
	assert.Equal(t, options{repo: "owner/name", line: "admin", def: "release/v0.1.0", head: head, outcome: "failure", failure: "policy",
		workflow: "terraform-apply.yaml", branches: ".github/settings/branches.json", server: "https://github.com", output: out,
		jobs: []string{"apply-admin-identity"}, runID: 42}, o)
}

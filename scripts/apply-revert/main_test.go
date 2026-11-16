package main

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/branchdecl"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testRepo    = "owner/name"
	testLine    = "admin"
	testDefault = "release/v0.1.0"
	// testWorkflowDefault は、.github/workflows/ を変える線。workflowHead はこの線の commit を運ぶ。
	testWorkflowDefault = "release/v0.2.0"
	testWorkflow        = "terraform-apply.yaml"
	testJob             = "apply-admin-identity"
	testServer          = "https://github.com"
	testRun             = int64(42)
)

var errFake = xerrors.New("fake command failed")

// fakeExit は、終了コードを持つ失敗（*exec.ExitError の代わり）。
type fakeExit struct{ code int }

func (e fakeExit) Error() string { return fmt.Sprintf("exit status %d", e.code) }
func (e fakeExit) ExitCode() int { return e.code }

// fake は、gh を応答の表で差し替え、git を dir の実物のリポジトリで走らせる実行器。
// 1つの fake を並行するサブテストが共有するので、calls は mu の下で書く。
type fake struct {
	dir   string
	gh    map[string]string
	mu    sync.Mutex
	calls []string
}

func (f *fake) runner() runner {
	return runner{output: func(name string, args ...string) (string, error) {
		call := name + " " + strings.Join(args, " ")
		f.mu.Lock()
		f.calls = append(f.calls, call)
		f.mu.Unlock()
		if name == "gh" {
			out, ok := f.gh[strings.Join(args, " ")]
			if !ok {
				return "", xerrors.Wrapf(errFake, "%s", call)
			}
			return out, nil
		}
		cmd := exec.CommandContext(context.Background(), name, args...)
		cmd.Dir = f.dir
		out, err := cmd.Output()
		if err != nil {
			return "", xerrors.Wrap(err, call)
		}
		return string(out), nil
	}}
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	return strings.TrimSpace(string(out))
}

type history struct {
	dir                  string
	base, first, head    string
	direct, workflowHead string
}

// newHistory は、デプロイブランチ line の base の上に、線（refs/remotes/origin/<testDefault>）の commit を運ぶ
// merge を2つ積む。各 merge の木は、運んだ線の commit の木と一致する。
// direct は base の上の merge でない commit、workflowHead は .github/workflows/ を変える線
// （refs/remotes/origin/<testWorkflowDefault>）の commit を運ぶ merge である。
func newHistory(t *testing.T) history {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "line")
	write := func(path, body string) {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(dir, path)), 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(dir, path), []byte(body), 0o600))
		git(t, dir, "add", path)
	}
	// feature を線 into へ merge し、その線の先頭を deploy へ運ぶ merge を作る。
	carry := func(into, deploy, branch, path, body string) string {
		git(t, dir, "switch", "-q", into)
		git(t, dir, "switch", "-q", "-c", branch)
		write(path, body)
		git(t, dir, "commit", "-q", "-m", branch)
		git(t, dir, "switch", "-q", into)
		git(t, dir, "merge", "-q", "--no-ff", "-m", "Merge "+branch, branch)
		git(t, dir, "switch", "-q", deploy)
		git(t, dir, "merge", "-q", "--no-ff", "-m", "Merge "+branch, into)
		return git(t, dir, "rev-parse", "HEAD")
	}

	write("main.tf", "a\n")
	git(t, dir, "commit", "-q", "-m", "base")
	h := history{dir: dir, base: git(t, dir, "rev-parse", "HEAD")}
	git(t, dir, "branch", "rel")
	h.first = carry("rel", "line", "one", "one.tf", "1\n")
	h.head = carry("rel", "line", "two", "main.tf", "b\n")
	git(t, dir, "update-ref", "refs/remotes/origin/"+testDefault, "rel")

	git(t, dir, "switch", "-q", "-c", "direct", h.base)
	write("main.tf", "c\n")
	git(t, dir, "commit", "-q", "-m", "direct")
	h.direct = git(t, dir, "rev-parse", "HEAD")

	git(t, dir, "branch", "wf-line", h.base)
	git(t, dir, "branch", "wf-rel", h.base)
	h.workflowHead = carry("wf-rel", "wf-line", "wf", ".github/workflows/x.yaml", "on: push\n")
	git(t, dir, "update-ref", "refs/remotes/origin/"+testWorkflowDefault, "wf-rel")
	git(t, dir, "switch", "-q", "line")
	return h
}

func runsKey() string {
	return "api repos/" + testRepo + "/actions/workflows/" + testWorkflow + "/runs?branch=admin&event=push&per_page=100&status=completed"
}

func jobsKey(id int64) string {
	return fmt.Sprintf("api repos/%s/actions/runs/%d/jobs?filter=latest&per_page=100", testRepo, id)
}

func refsKey(branch string) string {
	return "api repos/" + testRepo + "/git/matching-refs/heads/" + branch
}

func runsJSON(pairs ...any) string {
	var items []string
	for i := 0; i < len(pairs); i += 2 {
		items = append(items, fmt.Sprintf(`{"id":%d,"head_sha":%q}`, pairs[i], pairs[i+1]))
	}
	return fmt.Sprintf(`{"total_count":%d,"workflow_runs":[%s]}`, len(items), strings.Join(items, ","))
}

func jobsJSON(name, conclusion, sha string) string {
	return fmt.Sprintf(`{"total_count":1,"jobs":[{"name":%q,"conclusion":%q,"head_sha":%q}]}`, name, conclusion, sha)
}

func writeBranches(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "branches.json")
	require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
	return p
}

const branchesBody = `{"deploy":[{"branch":"admin","env":"admin","from":["release/"]}],"lines":["release/"],"revert":"revert/"}`

func planArgs(t *testing.T, head, outcome, failure string) ([]string, string) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "github_output")
	return []string{
		"-repo", testRepo, "-line", testLine, "-default", testDefault, "-head", head, "-run", strconv.FormatInt(testRun, 10),
		"-outcome", outcome, "-failure", failure, "-workflow", testWorkflow, "-job", testJob,
		"-branches", writeBranches(t, branchesBody), "-server", testServer, "-output", out,
	}, out
}

func outputs(t *testing.T, path string) map[string]string {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	m := map[string]string{}
	for line := range strings.Lines(string(raw)) {
		k, v, ok := strings.Cut(strings.TrimSuffix(line, "\n"), "=")
		require.True(t, ok, line)
		_, dup := m[k]
		require.False(t, dup, "鍵 %s が2回書かれています", k)
		m[k] = v
	}
	return m
}

func Test_parseOptions(t *testing.T) {
	t.Parallel()

	args, _ := planArgs(t, strings.Repeat("a", 40), "failure", "apply")
	t.Run("すべての引数を受ける", func(t *testing.T) {
		t.Parallel()
		o, err := parseOptions(args)
		require.NoError(t, err)
		assert.Equal(t, testRun, o.runID)
		assert.Equal(t, "apply", o.failure)
		assert.Equal(t, []string{testJob}, o.jobs)
	})
	t.Run("outcome と failure は空でも受ける（判定は eligibility）", func(t *testing.T) {
		t.Parallel()
		o, err := parseOptions(replace(replace(args, "-outcome", ""), "-failure", ""))
		require.NoError(t, err)
		assert.Empty(t, o.outcome)
		assert.Empty(t, o.failure)
	})
	t.Run("-job を繰り返し受ける", func(t *testing.T) {
		t.Parallel()
		o, err := parseOptions(append(slicesClone(args), "-job", "apply-admin"))
		require.NoError(t, err)
		assert.Equal(t, []string{testJob, "apply-admin"}, o.jobs)
	})
	t.Run("同じ -job を2回渡せば失敗する", func(t *testing.T) {
		t.Parallel()
		_, err := parseOptions(append(slicesClone(args), "-job", testJob))
		require.ErrorIs(t, err, errUsage)
	})

	broken := map[string]func([]string) []string{
		"head が SHA でない": func(a []string) []string { return replace(a, "-head", "main") },
		"run が0":         func(a []string) []string { return replace(a, "-run", "0") },
		"repo の形が違う":     func(a []string) []string { return replace(a, "-repo", "name") },
		"line が空":        func(a []string) []string { return replace(a, "-line", "") },
		"default が空":     func(a []string) []string { return replace(a, "-default", "") },
		"output が空":      func(a []string) []string { return replace(a, "-output", "") },
		"余分な引数がある":       func(a []string) []string { return append(a, "extra") },
		"知らない flag がある":  func(a []string) []string { return append([]string{"-x"}, a...) },
		"-job が無い":       func(a []string) []string { return dropFlag(a, "-job") },
		"workflow が空":    func(a []string) []string { return replace(a, "-workflow", "") },
		"branches が空":    func(a []string) []string { return replace(a, "-branches", "") },
		"server が空":      func(a []string) []string { return replace(a, "-server", "") },
		"head が39桁":      func(a []string) []string { return replace(a, "-head", strings.Repeat("a", 39)) },
		"head が大文字":      func(a []string) []string { return replace(a, "-head", strings.Repeat("A", 40)) },
		"run が数でない":      func(a []string) []string { return replace(a, "-run", "x") },
	}
	for name, mutate := range broken {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := parseOptions(mutate(slicesClone(args)))
			require.ErrorIs(t, err, errUsage)
		})
	}
}

func slicesClone(a []string) []string { return append([]string(nil), a...) }

func dropFlag(a []string, flag string) []string {
	var out []string
	for i := 0; i < len(a); i++ {
		if a[i] == flag {
			i++
			continue
		}
		out = append(out, a[i])
	}
	return out
}

func replace(a []string, flag, value string) []string {
	out := slicesClone(a)
	for i := range out {
		if out[i] == flag {
			out[i+1] = value
		}
	}
	return out
}

func Test_eligibility(t *testing.T) {
	t.Parallel()

	// 種類の名前は ci.mk の CI_APPLY と tf-local の failure-kind が書く値で、ここで字面のまま固定する。
	for _, kind := range []string{"plan", "policy", "apply", "timeout"} {
		t.Run("対象の種類 "+kind, func(t *testing.T) {
			t.Parallel()
			reason, err := eligibility("failure", kind)
			require.NoError(t, err)
			assert.Empty(t, reason)
		})
	}
	for _, kind := range []string{"prepare", "lock", "upload", "applied"} {
		t.Run("対象外の種類 "+kind, func(t *testing.T) {
			t.Parallel()
			reason, err := eligibility("failure", kind)
			require.NoError(t, err)
			assert.Contains(t, reason, kind)
		})
	}
	for _, outcome := range []string{"success", "cancelled", "skipped"} {
		t.Run("step の結果が "+outcome+" なら対象外", func(t *testing.T) {
			t.Parallel()
			reason, err := eligibility(outcome, "")
			require.NoError(t, err)
			assert.Contains(t, reason, outcome)
		})
	}
	errs := map[string]struct {
		outcome, failure string
		want             error
	}{
		"step の結果を受け取れなければ失敗する":            {"", "apply", errNoOutcome},
		"step が失敗したのに種類が無ければ失敗する":          {"failure", "", errNoKind},
		"知らない種類は対象外と読まずに失敗する":              {"failure", "init", errUnknownKind},
		"結果が空なら種類があっても失敗する（outputs が渡らない）": {"", "", errNoOutcome},
		"知らない step の結果は対象外と読まずに失敗する":       {"neutral", "apply", errUnknownOutcome},
		"大文字の step の結果も知らない値として失敗する":       {"Failure", "apply", errUnknownOutcome},
		"空白の付いた step の結果も知らない値として失敗する":     {"failure ", "apply", errUnknownOutcome},
	}
	for name, c := range errs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := eligibility(c.outcome, c.failure)
			require.ErrorIs(t, err, c.want)
		})
	}
}

func Test_lastSuccess(t *testing.T) {
	t.Parallel()

	a, b := strings.Repeat("a", 40), strings.Repeat("b", 40)
	cases := map[string]struct {
		gh   map[string]string
		want string
	}{
		"job が成功した最も新しい run の commit を返す": {map[string]string{
			runsKey(): runsJSON(10, a, 9, b), jobsKey(10): jobsJSON(testJob, "success", a),
		}, a},
		"job が失敗した run を飛ばす": {map[string]string{
			runsKey(): runsJSON(10, a, 9, b), jobsKey(10): jobsJSON(testJob, "failure", a), jobsKey(9): jobsJSON(testJob, "success", b),
		}, b},
		"job が skip された run を飛ばす（デフォルトブランチでない線）": {map[string]string{
			runsKey(): runsJSON(10, a, 9, b), jobsKey(10): jobsJSON(testJob, "skipped", a), jobsKey(9): jobsJSON(testJob, "success", b),
		}, b},
		"別の名前の job の成功を数えない": {map[string]string{
			runsKey(): runsJSON(10, a), jobsKey(10): jobsJSON("notify-success", "success", a),
		}, ""},
		"job の commit が run の commit と違えば数えない": {map[string]string{
			runsKey(): runsJSON(10, a), jobsKey(10): jobsJSON(testJob, "success", b),
		}, ""},
		"いまの run を数えない（再実行で成功した attempt を含む）": {map[string]string{
			runsKey(): runsJSON(testRun, a, 9, b), jobsKey(9): jobsJSON(testJob, "success", b),
		}, b},
		"run が0件なら空を返す": {map[string]string{runsKey(): runsJSON()}, ""},
	}
	t.Run("同じ名前の job が2つ成功しても、名指した別の job の成功とは数えない", func(t *testing.T) {
		t.Parallel()
		dup := fmt.Sprintf(`{"total_count":2,"jobs":[{"name":%q,"conclusion":"success","head_sha":%q},{"name":%q,"conclusion":"success","head_sha":%q}]}`, testJob, a, testJob, a)
		f := &fake{gh: map[string]string{runsKey(): runsJSON(10, a), jobsKey(10): dup}}
		got, err := lastSuccess(f.runner(), testRepo, testLine, testWorkflow, []string{testJob, "apply-admin"}, testRun)
		require.NoError(t, err)
		assert.Empty(t, got)
	})
	t.Run("名指したすべての job が成功した run だけを数える", func(t *testing.T) {
		t.Parallel()
		both := fmt.Sprintf(`{"total_count":2,"jobs":[{"name":%q,"conclusion":"success","head_sha":%q},{"name":"apply-admin","conclusion":"success","head_sha":%q}]}`, testJob, b, b)
		f := &fake{gh: map[string]string{runsKey(): runsJSON(10, a, 9, b), jobsKey(10): jobsJSON(testJob, "success", a), jobsKey(9): both}}
		got, err := lastSuccess(f.runner(), testRepo, testLine, testWorkflow, []string{testJob, "apply-admin"}, testRun)
		require.NoError(t, err)
		assert.Equal(t, b, got, "片方だけが成功した run 10 を飛ばす")
	})
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := &fake{gh: c.gh}
			got, err := lastSuccess(f.runner(), testRepo, testLine, testWorkflow, []string{testJob}, testRun)
			require.NoError(t, err)
			assert.Equal(t, c.want, got)
			assert.NotContains(t, strings.Join(f.calls, "\n"), jobsKey(testRun), "いまの run の job を読まない")
		})
	}

	broken := map[string]map[string]string{
		"run の一覧を取れない":      {},
		"run の一覧が JSON でない": {runsKey(): "<html>"},
		"run の一覧に件数が無い":     {runsKey(): `{"workflow_runs":[]}`},
		"job の一覧を取れない":      {runsKey(): runsJSON(10, a)},
		"job の一覧に件数が無い":     {runsKey(): runsJSON(10, a), jobsKey(10): `{"jobs":[]}`},
	}
	for name, gh := range broken {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := lastSuccess((&fake{gh: gh}).runner(), testRepo, testLine, testWorkflow, []string{testJob}, testRun)
			require.ErrorIs(t, err, errAPI)
		})
	}
}

func Test_revertRange(t *testing.T) {
	t.Parallel()

	h := newHistory(t)
	r := (&fake{dir: h.dir}).runner()

	t.Run("first-parent の merge を新しい順に返す", func(t *testing.T) {
		t.Parallel()
		commits, reason, err := revertRange(r, h.base, h.head)
		require.NoError(t, err)
		assert.Empty(t, reason)
		assert.Equal(t, []string{h.head, h.first}, commits)
	})
	t.Run("祖先でなければ理由を返す", func(t *testing.T) {
		t.Parallel()
		commits, reason, err := revertRange(r, h.direct, h.head)
		require.NoError(t, err)
		assert.Nil(t, commits)
		assert.Contains(t, reason, "祖先ではありません")
	})
	t.Run("木が同じなら理由を返す", func(t *testing.T) {
		t.Parallel()
		commits, reason, err := revertRange(r, h.head, h.head)
		require.NoError(t, err)
		assert.Nil(t, commits)
		assert.Contains(t, reason, "同じです")
	})
	t.Run("merge でない commit があれば理由を返す", func(t *testing.T) {
		t.Parallel()
		commits, reason, err := revertRange(r, h.base, h.direct)
		require.NoError(t, err)
		assert.Nil(t, commits)
		assert.Contains(t, reason, "merge でない commit")
	})
	t.Run("commit を解決できなければ失敗する", func(t *testing.T) {
		t.Parallel()
		_, _, err := revertRange(r, strings.Repeat("0", 40), h.head)
		require.ErrorIs(t, err, errGit)
	})
	t.Run("rev-list が SHA でない行を返せば失敗する", func(t *testing.T) {
		t.Parallel()
		stub := runner{output: func(_ string, args ...string) (string, error) {
			switch args[0] {
			case "rev-parse":
				return "t1\nt2\n", nil
			case "rev-list":
				return "garbage\n", nil
			}
			return "", nil
		}}
		_, _, err := revertRange(stub, h.base, h.head)
		require.ErrorIs(t, err, errGit)
	})
	t.Run("木が違うのに merge が0件なら失敗する", func(t *testing.T) {
		t.Parallel()
		stub := runner{output: func(_ string, args ...string) (string, error) {
			if args[0] == "rev-parse" {
				return "t1\nt2\n", nil
			}
			return "", nil
		}}
		_, _, err := revertRange(stub, h.base, h.head)
		require.ErrorIs(t, err, errGit)
	})
	t.Run("first-parent の鎖が base に届かなければ理由を返す", func(t *testing.T) {
		t.Parallel()
		d := newHistory(t)
		// base の上に merge を2つ積み、side を2つ目の merge の second parent にする。side は祖先だが、
		// first-parent の鎖は side を通らない。
		git(t, d.dir, "switch", "-q", "-c", "fp-line", d.base)
		git(t, d.dir, "switch", "-q", "-c", "fp-side")
		require.NoError(t, os.WriteFile(filepath.Join(d.dir, "side.tf"), []byte("s\n"), 0o600))
		git(t, d.dir, "add", "side.tf")
		git(t, d.dir, "commit", "-q", "-m", "side")
		side := git(t, d.dir, "rev-parse", "HEAD")
		git(t, d.dir, "switch", "-q", "fp-line")
		git(t, d.dir, "merge", "-q", "--no-ff", "-m", "Merge one", "one")
		git(t, d.dir, "merge", "-q", "--no-ff", "-m", "Merge side", "fp-side")
		tip := git(t, d.dir, "rev-parse", "HEAD")
		commits, reason, err := revertRange((&fake{dir: d.dir}).runner(), side, tip)
		require.NoError(t, err)
		assert.Nil(t, commits)
		assert.Contains(t, reason, "届きません")
	})
	t.Run("祖先の判定が 1 以外で失敗すれば理由にせず失敗する", func(t *testing.T) {
		t.Parallel()
		stub := runner{output: func(string, ...string) (string, error) { return "", fakeExit{128} }}
		_, _, err := revertRange(stub, h.base, h.head)
		require.ErrorIs(t, err, errGit)
	})
}

func Test_touchesWorkflows(t *testing.T) {
	t.Parallel()

	h := newHistory(t)
	r := (&fake{dir: h.dir}).runner()
	// 並行する worktree add は、作りかけの他の worktree の commondir を読んで落ちることがある。
	var worktreeMu sync.Mutex
	worktree := func(t *testing.T, args ...string) {
		t.Helper()
		worktreeMu.Lock()
		defer worktreeMu.Unlock()
		git(t, h.dir, append([]string{"worktree"}, args...)...)
	}
	commit := func(t *testing.T, from string, change func(dir string)) string {
		t.Helper()
		w := t.TempDir()
		worktree(t, "add", "-q", "--detach", w, from)
		t.Cleanup(func() { worktree(t, "remove", "--force", w) })
		change(w)
		git(t, w, "add", "-A")
		git(t, w, "commit", "-q", "-m", "change")
		return git(t, w, "rev-parse", "HEAD")
	}
	write := func(dir, path string) {
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(dir, path)), 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(dir, path), []byte("x\n"), 0o600))
	}

	for name, c := range map[string]struct {
		from   func() string
		change func(dir string)
		want   bool
	}{
		"workflows の外だけなら false":                {func() string { return h.base }, func(d string) { write(d, "x.tf") }, false},
		"workflows に足せば true":                   {func() string { return h.base }, func(d string) { write(d, ".github/workflows/a.yaml") }, true},
		"接頭辞だけ同じ workflows-x/ は false":          {func() string { return h.base }, func(d string) { write(d, ".github/workflows-x/a.yaml") }, false},
		"同じ .github の下でも workflows でなければ false": {func() string { return h.base }, func(d string) { write(d, ".github/CODEOWNERS") }, false},
		"workflow を消せば true": {func() string { return h.workflowHead }, func(d string) {
			require.NoError(t, os.Remove(filepath.Join(d, ".github/workflows/x.yaml")))
		}, true},
		"空白を含む名前の workflow も true": {func() string { return h.base }, func(d string) { write(d, ".github/workflows/a b.yaml") }, true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			from := c.from()
			to := commit(t, from, c.change)
			got, err := touchesWorkflows(r, from, to)
			require.NoError(t, err)
			assert.Equal(t, c.want, got)
		})
	}
	t.Run("commit を解決できなければ失敗する", func(t *testing.T) {
		t.Parallel()
		_, err := touchesWorkflows(r, strings.Repeat("0", 40), h.head)
		require.ErrorIs(t, err, errGit)
	})
}

func Test_existingBranch(t *testing.T) {
	t.Parallel()

	tree, sha := strings.Repeat("c", 40), strings.Repeat("d", 40)
	commitKey := "api repos/" + testRepo + "/git/commits/" + sha
	cases := map[string]struct {
		gh     map[string]string
		exists bool
	}{
		"無ければ false": {map[string]string{refsKey("revert/apply-42"): "[]"}, false},
		"接頭辞だけ一致する ref は数えない": {map[string]string{
			refsKey("revert/apply-42"): fmt.Sprintf(`[{"ref":"refs/heads/revert/apply-420","object":{"sha":%q}}]`, sha),
		}, false},
		"接頭辞だけ一致する ref の後ろの本物を採る": {map[string]string{
			refsKey("revert/apply-42"): fmt.Sprintf(`[{"ref":"refs/heads/revert/apply-420","object":{"sha":%q}},{"ref":"refs/heads/revert/apply-42","object":{"sha":%q}}]`, strings.Repeat("9", 40), sha),
			commitKey:                  fmt.Sprintf(`{"tree":{"sha":%q}}`, tree),
		}, true},
		"在れば先頭の commit の木を返す": {map[string]string{
			refsKey("revert/apply-42"): fmt.Sprintf(`[{"ref":"refs/heads/revert/apply-42","object":{"sha":%q}}]`, sha),
			commitKey:                  fmt.Sprintf(`{"tree":{"sha":%q}}`, tree),
		}, true},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, exists, err := existingBranch((&fake{gh: c.gh}).runner(), testRepo, "revert/apply-42")
			require.NoError(t, err)
			assert.Equal(t, c.exists, exists)
			if c.exists {
				assert.Equal(t, tree, got)
			}
		})
	}

	broken := map[string]map[string]string{
		"ref の一覧を取れない":  {},
		"ref の一覧が null": {refsKey("revert/apply-42"): "null"},
		"commit を取れない":  {refsKey("revert/apply-42"): fmt.Sprintf(`[{"ref":"refs/heads/revert/apply-42","object":{"sha":%q}}]`, sha)},
		"commit に木が無い":  {refsKey("revert/apply-42"): fmt.Sprintf(`[{"ref":"refs/heads/revert/apply-42","object":{"sha":%q}}]`, sha), commitKey: `{}`},
	}
	for name, gh := range broken {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, _, err := existingBranch((&fake{gh: gh}).runner(), testRepo, "revert/apply-42")
			require.ErrorIs(t, err, errAPI)
		})
	}
}

func Test_compareURL(t *testing.T) {
	t.Parallel()

	base, c1 := strings.Repeat("a", 40), strings.Repeat("b", 40)
	got := compareURL(testServer, testRepo, testDefault, "revert/apply-42", testRun, base, []string{c1})
	u, err := url.Parse(got)
	require.NoError(t, err)
	assert.Equal(t, "/owner/name/compare/release/v0.1.0...revert/apply-42", u.Path)
	q := u.Query()
	assert.Equal(t, "1", q.Get("quick_pull"))
	assert.Equal(t, "Revert: run 42 の apply の失敗を戻す", q.Get("title"))
	assert.Contains(t, q.Get("body"), "https://github.com/owner/name/actions/runs/42")
	assert.Contains(t, q.Get("body"), "- "+c1)
	assert.Contains(t, q.Get("body"), base)
	assert.NotContains(t, got, "\n", "通知の1行に収まる")

	t.Run("複数の commit を渡された順に並べる", func(t *testing.T) {
		t.Parallel()
		c2 := strings.Repeat("c", 40)
		u, err := url.Parse(compareURL(testServer, testRepo, testDefault, "revert/apply-42", testRun, base, []string{c1, c2}))
		require.NoError(t, err)
		body := u.Query().Get("body")
		assert.Less(t, strings.Index(body, c1), strings.Index(body, c2))
	})
}

func Test_sourceHead(t *testing.T) {
	t.Parallel()

	h := newHistory(t)
	r := (&fake{dir: h.dir}).runner()
	admin := branchdecl.DeployBranch{Branch: "admin", Env: "admin", From: []string{"release/"}}

	t.Run("merge が運んだ線の commit を返す", func(t *testing.T) {
		t.Parallel()
		source, reason, err := sourceHead(r, admin, testDefault, h.head)
		require.NoError(t, err)
		assert.Empty(t, reason)
		assert.Equal(t, git(t, h.dir, "rev-parse", h.head+"^2"), source)
	})
	t.Run("デフォルトブランチが from の線でなければ理由を返す", func(t *testing.T) {
		t.Parallel()
		source, reason, err := sourceHead(r, admin, "hotfix/v0.1.1", h.head)
		require.NoError(t, err)
		assert.Empty(t, source)
		assert.Contains(t, reason, "hotfix/v0.1.1 は admin へ変更を運ぶ線ではありません")
	})
	t.Run("merge でなければ理由を返す", func(t *testing.T) {
		t.Parallel()
		source, reason, err := sourceHead(r, admin, testDefault, h.direct)
		require.NoError(t, err)
		assert.Empty(t, source)
		assert.Contains(t, reason, "線から運んだ merge ではありません")
	})
	t.Run("運んだ commit がデフォルトブランチから到達できなければ理由を返す", func(t *testing.T) {
		t.Parallel()
		source, reason, err := sourceHead(r, admin, testWorkflowDefault, h.head)
		require.NoError(t, err)
		assert.Empty(t, source)
		assert.Contains(t, reason, "デフォルトブランチ release/v0.2.0 から到達できません")
	})
	t.Run("merge の木が運んだ commit の木と違えば理由を返す", func(t *testing.T) {
		t.Parallel()
		d := newHistory(t)
		git(t, d.dir, "switch", "-q", "-c", "skew", d.base)
		require.NoError(t, os.WriteFile(filepath.Join(d.dir, "skew.tf"), []byte("s\n"), 0o600))
		git(t, d.dir, "add", "skew.tf")
		git(t, d.dir, "commit", "-q", "-m", "skew")
		git(t, d.dir, "merge", "-q", "--no-ff", "-m", "Merge rel", "rel")
		skewed := git(t, d.dir, "rev-parse", "HEAD")
		source, reason, err := sourceHead((&fake{dir: d.dir}).runner(), admin, testDefault, skewed)
		require.NoError(t, err)
		assert.Empty(t, source)
		assert.Contains(t, reason, "の木が、運んだ")
	})
	t.Run("commit を解決できなければ失敗する", func(t *testing.T) {
		t.Parallel()
		_, _, err := sourceHead(r, admin, testDefault, strings.Repeat("0", 40))
		require.ErrorIs(t, err, errGit)
	})
	t.Run("デフォルトブランチの ref が無ければ失敗する", func(t *testing.T) {
		t.Parallel()
		_, _, err := sourceHead(r, admin, "release/v9.9.9", h.head)
		require.ErrorIs(t, err, errGit, "到達できないと読まず、失敗する")
	})
}

func Test_verifyRevert(t *testing.T) {
	t.Parallel()

	h := newHistory(t)
	r := (&fake{dir: h.dir}).runner()
	worktrees := func() int { return strings.Count(git(t, h.dir, "worktree", "list"), "\n") + 1 }

	t.Run("範囲をすべて戻せば木が一致する", func(t *testing.T) {
		t.Parallel()
		reason, err := verifyRevert(r, h.base, h.head, []string{h.head, h.first})
		require.NoError(t, err)
		assert.Empty(t, reason)
	})
	t.Run("運んだ線の commit の上で範囲を戻しても木が一致する", func(t *testing.T) {
		t.Parallel()
		reason, err := verifyRevert(r, h.base, git(t, h.dir, "rev-parse", h.head+"^2"), []string{h.head, h.first})
		require.NoError(t, err)
		assert.Empty(t, reason)
	})
	t.Run("範囲が足りなければ理由を返す", func(t *testing.T) {
		t.Parallel()
		reason, err := verifyRevert(r, h.base, h.head, []string{h.head})
		require.NoError(t, err)
		assert.Contains(t, reason, "一致しません")
	})
	t.Run("実物の revert が衝突すれば理由を返す", func(t *testing.T) {
		t.Parallel()
		d := newHistory(t)
		// merge two（main.tf: a→b）の後に main.tf を c にする。merge two の revert は c と a を突き合わせて衝突する。
		require.NoError(t, os.WriteFile(filepath.Join(d.dir, "main.tf"), []byte("c\n"), 0o600))
		git(t, d.dir, "commit", "-q", "-am", "after")
		tip := git(t, d.dir, "rev-parse", "HEAD")
		reason, err := verifyRevert((&fake{dir: d.dir}).runner(), d.base, tip, []string{d.head})
		require.NoError(t, err)
		assert.Contains(t, reason, "衝突")
		assert.Equal(t, 1, strings.Count(git(t, d.dir, "worktree", "list"), "\n")+1, "衝突の後も worktree を残さない")
	})
	t.Run("revert が衝突以外で失敗すれば理由にせず失敗する", func(t *testing.T) {
		t.Parallel()
		_, err := verifyRevert(r, h.base, h.head, []string{strings.Repeat("e", 40)})
		require.ErrorIs(t, err, errGit)
	})
	t.Run("revert が失敗すれば衝突として理由を返し、worktree を外す", func(t *testing.T) {
		t.Parallel()
		var removed bool
		stub := runner{output: func(_ string, args ...string) (string, error) {
			switch {
			case len(args) > 2 && args[2] == "-c":
				return "", fakeExit{1}
			case args[0] == "worktree" && args[1] == "remove":
				removed = true
			}
			return "", nil
		}}
		reason, err := verifyRevert(stub, h.base, h.head, []string{h.head})
		require.NoError(t, err)
		assert.Contains(t, reason, "衝突")
		assert.True(t, removed)
	})
	t.Run("木の比較が 1 以外で失敗すれば理由にせず失敗する", func(t *testing.T) {
		t.Parallel()
		stub := runner{output: func(_ string, args ...string) (string, error) {
			if len(args) > 2 && args[2] == "diff" {
				return "", fakeExit{128}
			}
			return "", nil
		}}
		_, err := verifyRevert(stub, h.base, h.head, []string{h.head})
		require.ErrorIs(t, err, errGit)
	})
	t.Run("worktree を作れなければ失敗する", func(t *testing.T) {
		t.Parallel()
		_, err := verifyRevert(r, h.base, strings.Repeat("0", 40), []string{h.head})
		require.ErrorIs(t, err, errGit)
	})
	// 兄弟の並列ケースと同じリポジトリに worktree を作るので、数えるケースは並列にしない。
	t.Run("一致しても一致しなくても一時の worktree を残さない", func(t *testing.T) {
		before := worktrees()
		_, err := verifyRevert(r, h.base, h.head, []string{h.head, h.first})
		require.NoError(t, err)
		_, err = verifyRevert(r, h.base, h.head, []string{h.head})
		require.NoError(t, err)
		assert.Equal(t, before, worktrees())
	})
}

func Test_writeOutputs(t *testing.T) {
	t.Parallel()

	p := filepath.Join(t.TempDir(), "out")
	require.NoError(t, os.WriteFile(p, []byte("plan=x\n"), 0o600))
	require.NoError(t, writeOutputs(p, [][2]string{{"decision", "push"}, {"branch", "revert/apply-1"}}))
	raw, err := os.ReadFile(p)
	require.NoError(t, err)
	assert.Equal(t, "plan=x\ndecision=push\nbranch=revert/apply-1\n", string(raw), "追記する")

	t.Run("改行を含む値は書かずに失敗する", func(t *testing.T) {
		t.Parallel()
		q := filepath.Join(t.TempDir(), "out")
		require.Error(t, writeOutputs(q, [][2]string{{"decision", "push"}, {"summary", "a\nb"}}))
		_, err := os.Stat(q)
		assert.True(t, os.IsNotExist(err), "途中まで書かない")
	})
	t.Run("\\r だけを含む値も書かずに失敗する", func(t *testing.T) {
		t.Parallel()
		q := filepath.Join(t.TempDir(), "out")
		require.Error(t, writeOutputs(q, [][2]string{{"summary", "a\rb"}}))
		_, err := os.Stat(q)
		assert.True(t, os.IsNotExist(err))
	})
	t.Run("開けなければ失敗する", func(t *testing.T) {
		t.Parallel()
		err := writeOutputs(filepath.Join(t.TempDir(), "missing", "out"), [][2]string{{"a", "b"}})
		require.ErrorIs(t, err, os.ErrNotExist)
	})
}

func Test_output(t *testing.T) {
	t.Parallel()

	got, err := output("git", "--version")
	require.NoError(t, err)
	assert.Contains(t, got, "git version")

	_, err = output("git", "no-such-subcommand")
	var code exitCoder
	require.True(t, xerrors.As(err, &code), "終了コードを運ぶ")
	assert.NotZero(t, code.ExitCode())
}

func Test_decide(t *testing.T) {
	t.Parallel()

	h := newHistory(t)
	baseTree := git(t, h.dir, "rev-parse", h.base+"^{tree}")
	other := strings.Repeat("e", 40)
	opts := func(head string) options {
		return options{repo: testRepo, line: testLine, def: testDefault, head: head, runID: testRun, workflow: testWorkflow, jobs: []string{testJob},
			branches: writeBranches(t, branchesBody), server: testServer}
	}
	success := func() map[string]string {
		return map[string]string{runsKey(): runsJSON(10, h.base), jobsKey(10): jobsJSON(testJob, "success", h.base), refsKey("revert/apply-42"): "[]"}
	}
	get := func(kv [][2]string, key string) string {
		for _, p := range kv {
			if p[0] == key {
				return p[1]
			}
		}
		return ""
	}

	t.Run("push するブランチと範囲を決める", func(t *testing.T) {
		t.Parallel()
		d, err := decide(opts(h.head), (&fake{dir: h.dir, gh: success()}).runner())
		require.NoError(t, err)
		assert.Equal(t, "push", get(d, "decision"))
		assert.Equal(t, h.head+" "+h.first, get(d, "commits"))
		assert.Equal(t, "revert/apply-42", get(d, "branch"))
		assert.Equal(t, git(t, h.dir, "rev-parse", h.head+"^2"), get(d, "head"), "revert は運んだ線の commit の上で作る")
		assert.Contains(t, get(d, "summary"), "compare/release/v0.1.0...revert/apply-42", "宛先はデフォルトブランチ")
	})
	t.Run(".github/workflows/ を戻すなら手元のコマンドを出す", func(t *testing.T) {
		t.Parallel()
		o := opts(h.workflowHead)
		o.def = testWorkflowDefault
		d, err := decide(o, (&fake{dir: h.dir, gh: success()}).runner())
		require.NoError(t, err)
		assert.Equal(t, "local", get(d, "decision"))
		source := git(t, h.dir, "rev-parse", h.workflowHead+"^2")
		assert.Contains(t, get(d, "summary"), "make apply-revert REVERT_HEAD="+source+" REVERT_COMMITS='"+h.workflowHead+"' REVERT_BRANCH=revert/apply-42",
			"revert は運んだ線の commit の上で作る")
		assert.Empty(t, get(d, "branch"), "push の job を起動しない")
	})
	t.Run("hotfix の間は用意しない", func(t *testing.T) {
		t.Parallel()
		o := opts(h.head)
		o.def = "hotfix/v0.1.1"
		d, err := decide(o, (&fake{dir: h.dir, gh: success()}).runner())
		require.NoError(t, err)
		assert.Equal(t, "none", get(d, "decision"))
		assert.Contains(t, get(d, "summary"), "admin へ変更を運ぶ線ではありません")
	})
	t.Run("-line が env を持つデプロイブランチでなければ API を呼ばずに失敗する", func(t *testing.T) {
		t.Parallel()
		o := opts(h.head)
		o.line = "release/v0.1.0"
		f := &fake{dir: h.dir, gh: success()}
		_, err := decide(o, f.runner())
		require.ErrorIs(t, err, errNotDeploy)
		assert.Empty(t, f.calls)
	})
	t.Run("最後に成功した run が無ければ用意しない", func(t *testing.T) {
		t.Parallel()
		d, err := decide(opts(h.head), (&fake{dir: h.dir, gh: map[string]string{runsKey(): runsJSON()}}).runner())
		require.NoError(t, err)
		assert.Equal(t, "none", get(d, "decision"))
		assert.Contains(t, get(d, "summary"), "成功した run")
	})
	t.Run("木が同じなら用意しない", func(t *testing.T) {
		t.Parallel()
		gh := map[string]string{runsKey(): runsJSON(10, h.head), jobsKey(10): jobsJSON(testJob, "success", h.head)}
		d, err := decide(opts(h.head), (&fake{dir: h.dir, gh: gh}).runner())
		require.NoError(t, err)
		assert.Equal(t, "none", get(d, "decision"))
	})
	t.Run("同じ木のブランチが既に在れば push しない", func(t *testing.T) {
		t.Parallel()
		gh := success()
		gh[refsKey("revert/apply-42")] = fmt.Sprintf(`[{"ref":"refs/heads/revert/apply-42","object":{"sha":%q}}]`, other)
		gh["api repos/"+testRepo+"/git/commits/"+other] = fmt.Sprintf(`{"tree":{"sha":%q}}`, baseTree)
		d, err := decide(opts(h.head), (&fake{dir: h.dir, gh: gh}).runner())
		require.NoError(t, err)
		assert.Equal(t, "exists", get(d, "decision"))
		assert.Empty(t, get(d, "branch"))
	})
	t.Run("違う木のブランチが既に在れば失敗する", func(t *testing.T) {
		t.Parallel()
		gh := success()
		gh[refsKey("revert/apply-42")] = fmt.Sprintf(`[{"ref":"refs/heads/revert/apply-42","object":{"sha":%q}}]`, other)
		gh["api repos/"+testRepo+"/git/commits/"+other] = fmt.Sprintf(`{"tree":{"sha":%q}}`, strings.Repeat("f", 40))
		_, err := decide(opts(h.head), (&fake{dir: h.dir, gh: gh}).runner())
		require.ErrorIs(t, err, errBranchTaken)
	})
	t.Run("接頭辞を読めなければ API を呼ばない", func(t *testing.T) {
		t.Parallel()
		o := opts(h.head)
		o.branches = writeBranches(t, `{}`)
		f := &fake{dir: h.dir, gh: success()}
		_, err := decide(o, f.runner())
		require.ErrorIs(t, err, errBranches)
		assert.Empty(t, f.calls)
	})
}

func Test_run(t *testing.T) {
	t.Parallel()

	h := newHistory(t)
	gh := map[string]string{runsKey(): runsJSON(10, h.base), jobsKey(10): jobsJSON(testJob, "success", h.base), refsKey("revert/apply-42"): "[]"}

	t.Run("対象なら決めたことを outputs に書く", func(t *testing.T) {
		t.Parallel()
		args, path := planArgs(t, h.head, "failure", "policy")
		var out bytes.Buffer
		require.NoError(t, run(args, &out, (&fake{dir: h.dir, gh: gh}).runner()))
		m := outputs(t, path)
		// 鍵の名前は terraform-apply.yaml の revert-plan の outputs が読む名前である。
		assert.ElementsMatch(t, []string{"decision", "headline", "summary", "commits", "branch", "head"}, keys(m))
		assert.Equal(t, "push", m["decision"])
		assert.Equal(t, "revert/apply-42", m["branch"])
		assert.Equal(t, h.head+" "+h.first, m["commits"])
		assert.Contains(t, m["summary"], "2 件の merge を戻し")
		assert.Contains(t, out.String(), m["headline"])
		assert.Contains(t, out.String(), m["summary"])
	})
	t.Run("用意できなければ理由を outputs に書き、push の値を書かない", func(t *testing.T) {
		t.Parallel()
		args, path := planArgs(t, h.head, "failure", "apply")
		require.NoError(t, run(args, &bytes.Buffer{}, (&fake{dir: h.dir, gh: map[string]string{runsKey(): runsJSON()}}).runner()))
		m := outputs(t, path)
		assert.ElementsMatch(t, []string{"decision", "headline", "summary"}, keys(m))
		assert.Equal(t, "none", m["decision"])
		assert.Contains(t, m["summary"], "成功した run")
	})
	t.Run(".github/workflows/ を戻すなら手元のコマンドを outputs に書く", func(t *testing.T) {
		t.Parallel()
		args, path := planArgs(t, h.workflowHead, "failure", "apply")
		args = replace(args, "-default", testWorkflowDefault)
		require.NoError(t, run(args, &bytes.Buffer{}, (&fake{dir: h.dir, gh: gh}).runner()))
		m := outputs(t, path)
		assert.ElementsMatch(t, []string{"decision", "headline", "summary"}, keys(m))
		assert.Equal(t, "local", m["decision"])
		assert.Contains(t, m["summary"], "make apply-revert ")
	})
	t.Run("outputs に書けなければ失敗する", func(t *testing.T) {
		t.Parallel()
		args, _ := planArgs(t, h.head, "failure", "lock")
		args = replace(args, "-output", filepath.Join(t.TempDir(), "missing", "out"))
		require.ErrorIs(t, run(args, &bytes.Buffer{}, (&fake{dir: h.dir, gh: gh}).runner()), os.ErrNotExist)
	})
	t.Run("対象外なら skip を書き、API を呼ばない", func(t *testing.T) {
		t.Parallel()
		args, path := planArgs(t, h.head, "failure", "lock")
		f := &fake{dir: h.dir, gh: gh}
		require.NoError(t, run(args, &bytes.Buffer{}, f.runner()))
		m := outputs(t, path)
		assert.Equal(t, "skip", m["decision"])
		assert.Contains(t, m["reason"], "lock")
		assert.Empty(t, f.calls)
	})
	t.Run("結果を受け取れなければ何も書かずに失敗する", func(t *testing.T) {
		t.Parallel()
		args, path := planArgs(t, h.head, "", "")
		require.ErrorIs(t, run(args, &bytes.Buffer{}, (&fake{dir: h.dir, gh: gh}).runner()), errNoOutcome)
		_, err := os.Stat(path)
		assert.True(t, os.IsNotExist(err))
	})
	t.Run("API を読めなければ何も書かずに失敗する", func(t *testing.T) {
		t.Parallel()
		args, path := planArgs(t, h.head, "failure", "apply")
		require.ErrorIs(t, run(args, &bytes.Buffer{}, (&fake{dir: h.dir, gh: map[string]string{}}).runner()), errAPI)
		_, err := os.Stat(path)
		assert.True(t, os.IsNotExist(err))
	})
	t.Run("引数が足りなければ失敗する", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, run(nil, &bytes.Buffer{}, (&fake{}).runner()), errUsage)
	})
}

func Test_jobFlags_String(t *testing.T) {
	t.Parallel()

	j := jobFlags{"a", "b"}
	assert.Equal(t, "a,b", j.String())
	assert.Empty(t, (&jobFlags{}).String())
}

func Test_jobFlags_Set(t *testing.T) {
	t.Parallel()

	var j jobFlags
	require.NoError(t, j.Set("a"))
	require.NoError(t, j.Set("b"))
	assert.Equal(t, jobFlags{"a", "b"}, j)
	require.ErrorIs(t, j.Set("a"), errUsage, "重複を拒む")
	require.ErrorIs(t, j.Set(""), errUsage, "空を拒む")
	assert.Equal(t, jobFlags{"a", "b"}, j, "拒んだ値を足さない")
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

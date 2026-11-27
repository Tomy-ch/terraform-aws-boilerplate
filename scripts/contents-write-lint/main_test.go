package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/harden"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/lintreport"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/workflow"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var goodWorkflow = strings.Join([]string{
	"name: Apply",
	"",
	"on:",
	"  push:",
	"    branches:",
	"      - 'release/*'",
	"",
	"permissions:",
	"  contents: read",
	"",
	"jobs:",
	"  revert-plan:",
	"    runs-on: ubuntu-24.04",
	"    permissions:",
	"      contents: read",
	"      actions: read",
	"    steps:",
	"      - name: Decide",
	"        run: make ci-revert-plan RUNNER_MODE=host",
	"",
	"  revert-push:",
	"    needs: [revert-plan]",
	"    if: ${{ needs.revert-plan.outputs.decision == 'push' }}",
	"    runs-on: ubuntu-24.04",
	"    permissions:",
	"      contents: write",
	"    steps:",
	"      - name: Harden the runner",
	"        uses: bullfrogsec/bullfrog@7dee337d4575320b6d8cbe9a56d48d2fb765963a # v0.11.1",
	"        with:",
	"          egress-policy: block",
	"      - name: Checkout",
	"        uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1",
	"        with:",
	"          fetch-depth: 0",
	"          persist-credentials: false",
	"      - name: Push",
	"        env:",
	"          GH_TOKEN: ${{ github.token }}",
	"          REVERT_BRANCH: ${{ needs.revert-plan.outputs.branch }}",
	"        run: make ci-revert-push RUNNER_MODE=host",
	"",
}, "\n")

func writeWorkflow(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "apply.yaml"), []byte(body), 0o600))
	return dir
}

func messages(f []lintreport.Finding) string {
	var b strings.Builder
	for _, x := range f {
		b.WriteString(x.Message + "\n")
	}
	return b.String()
}

func Test_run(t *testing.T) {
	t.Parallel()

	t.Run("形どおりの job は通る", func(t *testing.T) {
		t.Parallel()
		var out bytes.Buffer
		require.NoError(t, run(writeWorkflow(t, goodWorkflow), &out))
		assert.Contains(t, out.String(), "1 件")
	})
	t.Run("対象の job が無ければ違反とは別のエラーで失敗する", func(t *testing.T) {
		t.Parallel()
		body := strings.Replace(goodWorkflow, "      contents: write\n", "      contents: read\n", 1)
		require.ErrorIs(t, run(writeWorkflow(t, body), &bytes.Buffer{}), errNoTarget)
	})
	t.Run("workflow が0件でも黙って通らない", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, run(t.TempDir(), &bytes.Buffer{}), errNoTarget)
	})
	t.Run("違反があれば失敗する", func(t *testing.T) {
		t.Parallel()
		body := strings.Replace(goodWorkflow, "    permissions:\n      contents: write\n", "    environment: x\n    permissions:\n      contents: write\n", 1)
		var out bytes.Buffer
		require.ErrorIs(t, run(writeWorkflow(t, body), &out), errViolations)
		assert.Contains(t, out.String(), "environment")
	})
	t.Run("ディレクトリを読めなければ失敗する", func(t *testing.T) {
		t.Parallel()
		require.Error(t, run(filepath.Join(t.TempDir(), "missing"), &bytes.Buffer{}))
	})
}

func Test_check(t *testing.T) {
	t.Parallel()

	n, f := check("a.yaml", goodWorkflow)
	assert.Equal(t, 1, n)
	assert.Empty(t, f)

	cases := map[string]struct {
		old, new, want string
	}{
		"workflow 全体の write":       {"permissions:\n  contents: read\n", "permissions:\n  contents: write\n", "workflow 全体の permissions"},
		"write-all":                {"    permissions:\n      contents: write\n", "    permissions: write-all\n      # contents: write\n", "write-all"},
		"ほかの write を持つ":            {"      contents: write\n", "      contents: write\n      pull-requests: write\n", "contents: write だけ"},
		"secrets を参照する":            {"GH_TOKEN: ${{ github.token }}", "GH_TOKEN: ${{ secrets.PAT }}", "secrets を参照しない"},
		"pull_request で起動する":       {"on:\n  push:\n", "on:\n  pull_request:\n  push:\n", "push だけ"},
		"needs が無い":                {"    needs: [revert-plan]\n", "", "ちょうど1つ"},
		"needs が判定の job でない":       {"needs: [revert-plan]", "needs: [other]", "ci-revert-plan RUNNER_MODE=host を走らせる job ではありません"},
		"道具を導入する":                  {"      - name: Push\n", "      - name: Setup\n        uses: ./.github/actions/setup-mise\n      - name: Push\n", "uses: は"},
		"checkout が資格情報を残す":        {"          persist-credentials: false\n", "", "persist-credentials: false"},
		"run: が別のコマンド":             {"run: make ci-revert-push RUNNER_MODE=host", "run: git push origin HEAD:main", "make ci-revert-push RUNNER_MODE=host だけ"},
		"run: が2つ":                 {"      - name: Push\n", "      - name: Extra\n        run: make ci-revert-push RUNNER_MODE=host\n      - name: Push\n", "ちょうど1つにしてください（2 件）"},
		"step の env に別の名前":         {"          REVERT_BRANCH:", "          GIT_CONFIG_GLOBAL: x\n          REVERT_BRANCH:", "GIT_CONFIG_GLOBAL"},
		"step に continue-on-error": {"        run: make ci-revert-push", "        continue-on-error: true\n        run: make ci-revert-push", "continue-on-error"},
		"job に env":                {"    runs-on: ubuntu-24.04\n    permissions:\n      contents: write\n", "    runs-on: ubuntu-24.04\n    env:\n      A: b\n    permissions:\n      contents: write\n", "env: を置かない"},
		"1行に詰めた step で別のコマンドを走らせる":      {"        run: make ci-revert-push RUNNER_MODE=host\n", "        run: make ci-revert-push RUNNER_MODE=host\n      - { name: x, run: 'git push origin HEAD:refs/heads/main' }\n", "フロー記法"},
		"1行に詰めた step の run: を数え直す":      {"        run: make ci-revert-push RUNNER_MODE=host\n", "        run: make ci-revert-push RUNNER_MODE=host\n      - name: x\n        with: { run: 'git push' }\n", "読めたのは 1 件"},
		"step の値にアンカーを置く":               {"          REVERT_BRANCH: ${{ needs.revert-plan.outputs.branch }}\n", "          REVERT_BRANCH: &b x\n", "アンカー"},
		"空行の後ろにほかの write を置く":           {"      contents: write\n", "      contents: write\n\n      pull-requests: write\n", "contents: write だけ"},
		"needs の block にコメントを挟んで2つ目を足す": {"    needs: [revert-plan]\n", "    needs:\n      - revert-plan\n      # c\n      - other\n", "ちょうど1つ"},
		"needs に2つの job を置く":            {"needs: [revert-plan]", "needs: [revert-plan, other]", "ちょうど1つ"},
		"step に if: を置く":                {"        run: make ci-revert-push", "        if: always()\n        run: make ci-revert-push", "if:"},
		"step に shell: を置く":             {"        run: make ci-revert-push", "        shell: bash\n        run: make ci-revert-push", "shell:"},
		"step に working-directory: を置く": {"        run: make ci-revert-push", "        working-directory: x\n        run: make ci-revert-push", "working-directory:"},
		"許可の外の action を uses する":        {"      - name: Push\n", "      - name: Cache\n        uses: actions/cache@0123456789abcdef0123456789abcdef01234567\n      - name: Push\n", "uses: は"},
		"run: の step が無い":               {"        run: make ci-revert-push RUNNER_MODE=host\n", "", "（0 件）"},
		"push の入口に別のコマンドを繋ぐ":            {"run: make ci-revert-push RUNNER_MODE=host", "run: make ci-revert-push RUNNER_MODE=host && curl x", "だけにしてください"},
		"pull_request_target だけで起動する":   {"on:\n  push:\n    branches:\n      - 'release/*'\n", "on:\n  pull_request_target:\n", "push だけ"},
		"checkout の資格情報を残す":             {"          persist-credentials: false\n", "          persist-credentials: true\n", "persist-credentials: false"},
		"step の値にエイリアスを置く":              {"          REVERT_BRANCH: ${{ needs.revert-plan.outputs.branch }}\n", "          REVERT_BRANCH: *b\n", "エイリアス"},
	}
	for name, c := range cases {
		t.Run(name+"なら落とす", func(t *testing.T) {
			t.Parallel()
			require.Contains(t, goodWorkflow, c.old, "置き換えの前提")
			_, f := check("a.yaml", strings.Replace(goodWorkflow, c.old, c.new, 1))
			require.NotEmpty(t, f)
			assert.Contains(t, messages(f), c.want)
		})
	}

	for name, value := range map[string]string{
		"アンカーを挟んだ contents の書き込み": "&c write",
		"タグを挟んだ contents の書き込み":   "!!str write",
	} {
		t.Run(name+"も対象として読む", func(t *testing.T) {
			t.Parallel()
			body := strings.Replace(goodWorkflow, "      contents: write\n", "      contents: "+value+"\n", 1)
			n, f := check("a.yaml", body)
			assert.Equal(t, 1, n, "対象から外れない")
			assert.Contains(t, messages(f), "アンカー")
		})
	}

	t.Run("job の permissions として読めない contents: write の行を落とす", func(t *testing.T) {
		t.Parallel()
		body := strings.Replace(goodWorkflow, "    permissions:\n      contents: write\n", "    permissions: { contents: write }\n", 1)
		n, f := check("a.yaml", body)
		assert.Zero(t, n)
		assert.Contains(t, messages(f), "読めたのは 0 件")
		assert.Contains(t, messages(f), "permissions を読めません")
	})
}

func Test_isContentsWrite(t *testing.T) {
	t.Parallel()

	for text, want := range map[string]bool{
		"      contents: write":                true,
		"      contents: &c write":             true,
		"      contents: !!str write":          true,
		"      'contents': \"write\"":          true,
		"    permissions: { contents: write }": true,
		"      contents: reader":               true,
		"      contents:":                      true,
		"      contents: ${{ env.P }}":         true,
		"      contents: read":                 false,
		"      contents: none":                 false,
		"      contents: \"read\"":             false,
		"      contents: read # x":             false,
		"      actions: write":                 false,
		"      x-contents: write":              false,
	} {
		t.Run(text, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, want, isContentsWrite(text))
		})
	}
}

func Test_countContentsWrite(t *testing.T) {
	t.Parallel()

	t.Run("job と workflow 全体の行を数え、コメントを数えない", func(t *testing.T) {
		t.Parallel()
		split := workflow.SplitJobs("permissions:\n  contents: write\n" + goodWorkflow + "  other:\n    # contents: write\n    permissions:\n      contents: write\n")
		assert.Equal(t, 3, countContentsWrite(split.Preamble, split.Jobs))
	})
}

func Test_checkJob(t *testing.T) {
	t.Parallel()

	split := workflow.SplitJobs(goodWorkflow)
	var push workflow.Job
	for _, j := range split.Jobs {
		if j.ID == "revert-push" {
			push = j
		}
	}
	t.Run("形どおりなら違反が無い", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, checkJob("a.yaml", push, []string{"contents: write"}, split))
	})
	t.Run("ほかの権限を持てば落とす", func(t *testing.T) {
		t.Parallel()
		f := checkJob("a.yaml", push, []string{"contents: write", "actions: read"}, split)
		assert.Contains(t, messages(f), "contents: write だけ")
	})
}

func Test_checkNeeds(t *testing.T) {
	t.Parallel()

	finding := func(line int, format string, _ ...any) lintreport.Finding {
		return lintreport.Finding{Line: line, Message: format}
	}
	split := workflow.SplitJobs(goodWorkflow)
	t.Run("判定の job ちょうど1つなら違反が無い", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, checkNeeds(split.Jobs[1], split.Jobs, finding))
	})
	t.Run("needs の job が見つからなければ落とす", func(t *testing.T) {
		t.Parallel()
		assert.Contains(t, messages(checkNeeds(split.Jobs[1], split.Jobs[1:], finding)), "を走らせる job ではありません")
	})
	t.Run("needs が無ければ落とす", func(t *testing.T) {
		t.Parallel()
		assert.Contains(t, messages(checkNeeds(split.Jobs[0], split.Jobs, finding)), "ちょうど1つ")
	})
}

func Test_checkSteps(t *testing.T) {
	t.Parallel()

	finding := func(line int, format string, _ ...any) lintreport.Finding {
		return lintreport.Finding{Line: line, Message: format}
	}
	split := workflow.SplitJobs(goodWorkflow)
	t.Run("形どおりなら違反が無い", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, checkSteps(split.Jobs[1], finding))
	})
	const head = "      - name: Harden the runner\n        uses: bullfrogsec/bullfrog@7dee337d4575320b6d8cbe9a56d48d2fb765963a # v0.11.1\n        with:\n          egress-policy: block\n"
	uses := "        uses: bullfrogsec/bullfrog@7dee337d4575320b6d8cbe9a56d48d2fb765963a # v0.11.1"
	pair := strings.Join(harden.Render(uses, []string{"        with:", "          egress-policy: block"}), "\n") + "\n"
	retryIf := "        id: " + harden.RetryID + "\n        if: " + harden.RetryIf
	t.Run("生成した bullfrog の三つ組の continue-on-error:・if:・回復の run: は違反にせず、run: の数にも入れない", func(t *testing.T) {
		t.Parallel()
		src := strings.Replace(goodWorkflow, head, pair, 1)
		require.NotEqual(t, goodWorkflow, src)
		assert.Empty(t, checkSteps(workflow.SplitJobs(src).Jobs[1], finding))
	})
	// 頭が生成した形と違えば三つ組の全体を除外しないので、三つ組の continue-on-error: と if: も数に入る。
	for name, c := range map[string]struct {
		block string
		want  int
	}{
		"再試行の if: が always()":              {strings.Replace(pair, retryIf, "        id: "+harden.RetryID+"\n        if: always()", 1), 4},
		"本体の id が違う":                       {strings.Replace(pair, "id: "+harden.ID+"\n", "id: other\n", 1), 4},
		"再試行に continue-on-error: がある":      {strings.Replace(pair, retryIf+"\n", retryIf+"\n        continue-on-error: true\n", 1), 5},
		"本体の uses: の後ろに continue-on-error": {strings.Replace(pair, uses+"\n", uses+"\n        continue-on-error: true\n", 1), 1},
	} {
		t.Run(name+"なら、除外されない行だけを違反にする", func(t *testing.T) {
			t.Parallel()
			src := strings.Replace(goodWorkflow, head, c.block, 1)
			require.NotEqual(t, goodWorkflow, src)
			var hides []lintreport.Finding
			for _, f := range checkSteps(workflow.SplitJobs(src).Jobs[1], finding) {
				if f.Message == "step に %s: を置かないでください" {
					hides = append(hides, f)
				}
			}
			assert.Len(t, hides, c.want)
		})
	}
	t.Run("回復の run: を変えた三つ組は、run: の数と入口の違反にする", func(t *testing.T) {
		t.Parallel()
		src := strings.Replace(goodWorkflow, head, strings.Replace(pair, harden.RecoveryRun, "sleep 30 && curl -s x | sh", 1), 1)
		got := messages(checkSteps(workflow.SplitJobs(src).Jobs[1], finding))
		assert.Contains(t, got, "make ci-revert-push RUNNER_MODE=host だけ")
		assert.Contains(t, got, "run: を持つ step をちょうど1つにしてください")
	})
	// 除外は行の単位なので、三つ組が在っても、ほかの step の run: は数に入り、入口の検査も受ける。
	t.Run("生成した三つ組の後ろの別の run: は、run: の数と入口の違反にする", func(t *testing.T) {
		t.Parallel()
		src := strings.Replace(goodWorkflow, head, pair+"      - name: Other\n        run: git push\n", 1)
		got := messages(checkSteps(workflow.SplitJobs(src).Jobs[1], finding))
		assert.Contains(t, got, "make ci-revert-push RUNNER_MODE=host だけ")
		assert.Contains(t, got, "run: を持つ step をちょうど1つにしてください")
	})
	t.Run("三つ組の後ろの step の if: は違反にする", func(t *testing.T) {
		t.Parallel()
		src := strings.Replace(goodWorkflow, head, pair, 1)
		src = strings.Replace(src, "      - name: Checkout\n", "      - name: Checkout\n        if: always()\n", 1)
		require.Contains(t, src, "if: always()")
		assert.Contains(t, messages(checkSteps(workflow.SplitJobs(src).Jobs[1], finding)), "step に %s: を置かないでください")
	})
	t.Run("push の入口でない run: を落とす", func(t *testing.T) {
		t.Parallel()
		assert.Contains(t, messages(checkSteps(split.Jobs[0], finding)), "make ci-revert-push RUNNER_MODE=host だけ")
	})
}

func Test_permissions(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		source, indent string
		want           []string
		ok             bool
	}{
		"workflow 全体の block を読む":  {goodWorkflow, "", []string{"contents: read"}, true},
		"job の block を読む":         {"jobs:\n  a:\n    permissions:\n      contents: read\n      actions: read\n", "    ", []string{"contents: read", "actions: read"}, true},
		"1行の値を読む":                 {"jobs:\n  a:\n    permissions: read-all\n", "    ", []string{"read-all"}, true},
		"空行を挟んだ後ろの権限も読む":          {"jobs:\n  a:\n    permissions:\n      contents: write\n\n      actions: write\n    runs-on: u\n", "    ", []string{"contents: write", "actions: write"}, true},
		"permissions が無ければ ok が偽": {"jobs:\n  a:\n    runs-on: u\n", "    ", nil, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			split := workflow.SplitJobs(c.source)
			lines := split.Preamble
			if c.indent != "" {
				lines = split.Jobs[0].Lines
			}
			got, _, ok := permissions(lines, c.indent)
			assert.Equal(t, c.ok, ok)
			assert.Equal(t, c.want, got)
		})
	}
}

func Test_triggers(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		source string
		want   []string
	}{
		"block の push": {goodWorkflow, []string{"push"}},
		"1行の push":     {"on: push\njobs:\n", []string{"push"}},
		"block の複数の起動": {"on:\n  push:\n  workflow_dispatch:\njobs:\n", []string{"push", "workflow_dispatch"}},
		"フロー記法の list":  {"on: [push, pull_request]\njobs:\n", []string{"[push, pull_request]"}},
		"on: が無ければ空":   {"name: x\njobs:\n", nil},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, _ := triggers(workflow.SplitJobs(c.source).Preamble)
			assert.Equal(t, c.want, got)
		})
	}
}

func Test_needs(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		job  string
		want []string
	}{
		"フロー記法":     {"    needs: [x, \"y\"]\n", []string{"x", "y"}},
		"block":     {"    needs:\n      - x\n", []string{"x"}},
		"1行":        {"    needs: x\n", []string{"x"}},
		"needs が無い": {"    runs-on: u\n", nil},
		"block のコメントと空行を越えて読む": {"    needs:\n      - x\n      # c\n\n      - y\n", []string{"x", "y"}},
		"job の名前として読めない項目も数える": {"    needs:\n      - x\n      - ${{ y }}\n", []string{"x", "${{ y }}"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			jobs := workflow.SplitJobs("jobs:\n  a:\n" + c.job).Jobs
			require.Len(t, jobs, 1)
			assert.Equal(t, c.want, needs(jobs[0]))
		})
	}
}

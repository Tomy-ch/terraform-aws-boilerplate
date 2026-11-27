package main

import (
	"bytes"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/cientry"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/harden"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/lintreport"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/requiredchecks"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/testenv"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/workflow"
)

const envDecl = `[{"name":"admin-plan"},{"name":"admin-apply"},{"name":"admin-apply-identity"},{"name":"admin-unlock-identity"},{"name":"admin-unlock"}]`

var envs = []string{"admin-plan", "admin-apply", "admin-apply-identity", "admin-unlock-identity", "admin-unlock"}

var planPerms = []string{"contents: read", "id-token: write"}

var planWorkflow = strings.Join([]string{
	"name: Terraform Gates",
	"",
	"on:",
	"  pull_request:",
	"",
	"permissions:",
	"  contents: read",
	"",
	"jobs:",
	"  infra-code-gate:",
	"    runs-on: ubuntu-24.04",
	"    permissions:",
	"      contents: read",
	"    steps:",
	"      - run: make infra-code-gate",
	"",
	"  plan-admin-identity:",
	"    needs: infra-code-gate",
	"    runs-on: ubuntu-24.04",
	"    environment: admin-plan",
	"    permissions:",
	"      contents: read",
	"      id-token: write",
	"    steps:",
	"      - name: Refuse untrusted Pull Requests",
	"        id: refuse-untrusted",
	"        run: test \"$HEAD_REPO_ID\" = \"$REPO_ID\"",
	"      - uses: actions/checkout@0000000000000000000000000000000000000000",
	"        with:",
	"          persist-credentials: false",
	"      - uses: ./.github/actions/setup-mise",
	"        with:",
	"          tools: aqua:hashicorp/terraform",
	"      - name: Run terraform plan through make",
	"        run: make ci-aws-credentials CI_ENV=admin CI_ROLE=plan RUNNER_MODE=host",
	"      - name: Confirm the apply-family roles refuse this job",
	"        env:",
	"          CI_ACCOUNT_ID: x",
	"        run: make ci-assume-denied CI_ENV=admin RUNNER_MODE=host",
	"      - run: make ci-plan-admin-identity RUNNER_MODE=host",
	"",
}, "\n")

var applyWorkflow = strings.Join([]string{
	"name: Terraform Apply",
	"",
	"on:",
	"  push:",
	"    branches: ['release/*']",
	"",
	"permissions:",
	"  contents: read",
	"",
	"concurrency:",
	"  group: terraform-apply-admin",
	"  cancel-in-progress: false",
	"",
	"jobs:",
	"  apply-infra-code-gate:",
	"    runs-on: ubuntu-24.04",
	"    permissions:",
	"      contents: read",
	"    steps:",
	"      - run: make infra-code-gate RUNNER_MODE=host",
	"",
	"  apply-resource-order-check:",
	"    runs-on: ubuntu-24.04",
	"    permissions:",
	"      contents: read",
	"    steps:",
	"      - run: make resource-order-check RUNNER_MODE=host",
	"",
	"  apply-layer-check:",
	"    runs-on: ubuntu-24.04",
	"    permissions:",
	"      contents: read",
	"    steps:",
	"      - run: make layer-check RUNNER_MODE=host",
	"",
	"  apply-root-check:",
	"    runs-on: ubuntu-24.04",
	"    permissions:",
	"      contents: read",
	"    steps:",
	"      - run: make root-check RUNNER_MODE=host",
	"",
	"  apply-admin-identity:",
	"    needs: [apply-infra-code-gate, apply-resource-order-check, apply-layer-check, apply-root-check]",
	"    if: ${{ needs.apply-infra-code-gate.result == 'success' && needs.apply-resource-order-check.result == 'success' && needs.apply-layer-check.result == 'success' && needs.apply-root-check.result == 'success' && github.ref_name == github.event.repository.default_branch }}",
	"    runs-on: ubuntu-24.04",
	"    environment:",
	"      name: admin-apply-identity",
	"    permissions:",
	"      contents: read",
	"      id-token: write",
	"    steps:",
	"      - name: Refuse stale commits",
	"        id: refuse-stale",
	"        run: head=$(git ls-remote \"https://github.com/$REPO.git\" \"refs/heads/$REF\") && test \"$SHA\" = \"$head\" || exit 1",
	"      - uses: actions/checkout@0000000000000000000000000000000000000000",
	"        with:",
	"          persist-credentials: false",
	"      - run: make ci-aws-credentials CI_ENV=admin CI_ROLE=apply-identity RUNNER_MODE=host",
	"      - run: make ci-apply-admin-identity RUNNER_MODE=host",
	"",
	revertJobs,
}, "\n")

// revertJobs は、applyWorkflow の apply の job の失敗を戻す判定と、通知の job。apply.yml に置く前提で
// workflow の名前を書く。
var revertJobs = strings.Join([]string{
	"  revert-plan:",
	"    needs: [apply-admin-identity]",
	"    if: ${{ always() && needs.apply-admin-identity.result == 'failure' }}",
	"    runs-on: ubuntu-24.04",
	"    permissions:",
	"      contents: read",
	"      actions: read",
	"    steps:",
	"      - name: Decide the revert",
	"        env:",
	"          APPLY_REVERT_WORKFLOW: apply.yml",
	"          APPLY_REVERT_RESULTS: >-",
	"            apply-admin-identity=${{ needs.apply-admin-identity.result }}:${{ needs.apply-admin-identity.outputs.apply-outcome }}:${{ needs.apply-admin-identity.outputs.failure }}",
	"        run: make ci-revert-plan RUNNER_MODE=host",
	"",
	"  notify-failure:",
	"    needs: [apply-infra-code-gate, apply-resource-order-check, apply-admin-identity]",
	"    if: ${{ always() && contains(fromJSON('[\"failure\", \"cancelled\"]'), needs.apply-admin-identity.result) }}",
	"    uses: ./.github/workflows/notify.yaml",
	"",
	"  notify-success:",
	"    needs: [apply-admin-identity]",
	"    if: ${{ needs.apply-admin-identity.result == 'success' }}",
	"    uses: ./.github/workflows/notify.yaml",
	"",
}, "\n")

// replaced は、置き換え元が無くて fixture が黙って元のままになることを防ぎます。
func replaced(t *testing.T, src, from, to string) string {
	t.Helper()

	out := strings.Replace(src, from, to, 1)
	require.NotEqual(t, src, out, "置き換え元が見つからない")

	return out
}

const (
	wfConcurrency = "concurrency:\n  group: terraform-apply-admin\n  cancel-in-progress: false\n"
	applyEnv      = "    environment:\n      name: admin-apply-identity\n"
)

func jobConcurrency(cancel string) string {
	return "    concurrency:\n      group: x\n      cancel-in-progress: " + cancel + "\n"
}

func infoOf(source string) workflowInfo {
	split := workflow.SplitJobs(source)
	on, onLine := triggers(split.Preamble)

	return workflowInfo{file: deployGateFile, on: on, onLine: onLine, preamble: split.Preamble, jobs: split.Jobs}
}

func credOf(job workflow.Job) workflow.Line {
	for _, l := range job.Lines {
		if credentials.MatchString(l.Text) {
			return l
		}
	}

	return workflow.Line{}
}

func formatted(line int, format string, args ...any) lintreport.Finding {
	return lintreport.Finding{Line: line, Message: fmt.Sprintf(format, args...)}
}

func repoWith(t *testing.T, workflows map[string]string, decl string) (string, string) {
	t.Helper()

	dir := t.TempDir()
	wf := filepath.Join(dir, "workflows")
	require.NoError(t, os.MkdirAll(wf, 0o750))
	for name, content := range workflows {
		require.NoError(t, os.WriteFile(filepath.Join(wf, name), []byte(content), 0o600))
	}
	if _, ok := workflows["gates.yaml"]; !ok {
		require.NoError(t, os.WriteFile(filepath.Join(wf, "gates.yaml"), []byte(gatesWorkflow), 0o600))
	}
	envFile := filepath.Join(dir, "environments.json")
	require.NoError(t, os.WriteFile(envFile, []byte(decl), 0o600))
	require.NoError(t, os.WriteFile(rulesetOf(envFile), []byte(rulesetDecl), 0o600))

	return wf, envFile
}

// rulesetOf は、repoWith が environments.json の隣に置く保護設定の宣言のパスです。
func rulesetOf(envFile string) string {
	return filepath.Join(filepath.Dir(envFile), "branch-protection.json")
}

const rulesetDecl = `{"rules": [{"type": "required_status_checks", "parameters": {"required_status_checks": [{"context": "tf-lint"}]}}]}`

// gatesWorkflow は、Pull Request の必須検査として順序の検査・層の検査・root の検査を走らせる workflow。repoWith が既定で置く。
var gatesWorkflow = strings.Join([]string{
	"name: Terraform Gates",
	"on:",
	"  pull_request:",
	"permissions:",
	"  contents: read",
	"jobs:",
	"  changes:",
	"    runs-on: ubuntu-24.04",
	"    outputs:",
	"      relevant: ${{ steps.filter.outputs.relevant }}",
	"    steps:",
	"      - id: filter",
	"        uses: dorny/paths-filter@0000000000000000000000000000000000000000",
	"  tf-lint:",
	"    needs: changes",
	"    if: ${{ !cancelled() && needs.changes.outputs.relevant != 'false' }}",
	"    runs-on: ubuntu-24.04",
	"    permissions:",
	"      contents: read",
	"    steps:",
	"      - name: Run TFLint",
	"        env:",
	"          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}",
	"        run: make tf-lint RUNNER_MODE=host",
	"      - name: Check the resource order",
	"        run: make resource-order-check RUNNER_MODE=host",
	"      - name: Check the layer rules",
	"        run: make layer-check RUNNER_MODE=host",
	"      - name: Check the root composition and backend settings",
	"        run: make root-check RUNNER_MODE=host",
	"",
}, "\n")

func jobOf(t *testing.T, source, id string) workflow.Job {
	t.Helper()

	for _, j := range workflow.SplitJobs(source).Jobs {
		if j.ID == id {
			return j
		}
	}
	require.FailNow(t, "job が無い", id)

	return workflow.Job{}
}

func messages(findings []lintreport.Finding) string {
	var b strings.Builder
	for _, f := range findings {
		b.WriteString(f.Message + "\n")
	}

	return b.String()
}

func lines(texts ...string) []workflow.Line {
	out := make([]workflow.Line, 0, len(texts))
	for i, t := range texts {
		out = append(out, workflow.Line{Number: i + 1, Text: t})
	}

	return out
}

func Test_run(t *testing.T) {
	t.Parallel()

	t.Run("job の外に入口の行しか無い workflow だけなら、対象0件より先に違反で失敗する", func(t *testing.T) {
		t.Parallel()

		wf, env := repoWith(t, map[string]string{"p.yaml": "on:\n  push:\nx: make ci-apply-a\n"}, envDecl)
		var out bytes.Buffer
		require.ErrorIs(t, run(wf, env, rulesetOf(env), "", &out), errViolations)
		assert.Contains(t, out.String(), "job の行として読めません")
	})

	t.Run("入口の job が id-token を欠く workflow だけなら、対象0件より先に違反で失敗する", func(t *testing.T) {
		t.Parallel()

		wf, env := repoWith(t, map[string]string{deployGateFile: strings.Replace(planWorkflow, "      id-token: write\n", "", 1)}, envDecl)
		var out bytes.Buffer
		require.ErrorIs(t, run(wf, env, rulesetOf(env), "", &out), errViolations)
		assert.Contains(t, out.String(), "job plan-admin-identity は CI の入口")
	})

	t.Run("plan と apply の job が規則を満たせば成功する", func(t *testing.T) {
		t.Parallel()

		wf, env := repoWith(t, map[string]string{deployGateFile: planWorkflow, "apply.yml": applyWorkflow}, envDecl)
		var out bytes.Buffer
		require.NoError(t, run(wf, env, rulesetOf(env), "", &out))
		assert.Contains(t, out.String(), "job 2 件")
	})

	t.Run("apply の失敗を戻す配線が欠ければ違反で失敗する", func(t *testing.T) {
		t.Parallel()

		wf, env := repoWith(t, map[string]string{deployGateFile: planWorkflow, "apply.yml": replaced(t, applyWorkflow, "  revert-plan:\n", "  revert-gone:\n")}, envDecl)
		var out bytes.Buffer
		require.ErrorIs(t, run(wf, env, rulesetOf(env), "", &out), errViolations)
		assert.Contains(t, out.String(), "revert-plan の job がありません")
	})

	t.Run("対象の job が無ければ errNoTarget", func(t *testing.T) {
		t.Parallel()

		wf, env := repoWith(t, map[string]string{"x.yaml": "on: push\njobs:\n  a:\n    steps:\n      - run: echo\n"}, envDecl)
		require.ErrorIs(t, run(wf, env, rulesetOf(env), "", &bytes.Buffer{}), errNoTarget)
	})

	t.Run("workflow を読めなければ失敗する", func(t *testing.T) {
		t.Parallel()
		testenv.RequireNonRoot(t, "特権実行では権限の無いファイルも読めるため検証できない")

		wf, env := repoWith(t, map[string]string{deployGateFile: planWorkflow, "apply.yml": applyWorkflow}, envDecl)
		require.NoError(t, os.Chmod(filepath.Join(wf, "apply.yml"), 0))
		err := run(wf, env, rulesetOf(env), "", &bytes.Buffer{})
		require.ErrorIs(t, err, fs.ErrPermission)
		assert.Contains(t, err.Error(), "apply.yml の読み取り")
	})

	t.Run("workflow が1つも無ければ errNoTarget", func(t *testing.T) {
		t.Parallel()

		wf, env := repoWith(t, nil, envDecl)
		require.ErrorIs(t, run(wf, env, rulesetOf(env), "", &bytes.Buffer{}), errNoTarget)
	})

	t.Run("対象が無くても違反があれば違反を報告する", func(t *testing.T) {
		t.Parallel()

		wf, env := repoWith(t, map[string]string{"x.yaml": "on: push\npermissions:\n  id-token: write\njobs:\n  a:\n    steps:\n      - run: echo\n"}, envDecl)
		var out bytes.Buffer
		require.ErrorIs(t, run(wf, env, rulesetOf(env), "", &out), errViolations)
		assert.Contains(t, out.String(), "workflow 全体の permissions に id-token")
	})

	t.Run("違反があれば一覧を出して errViolations", func(t *testing.T) {
		t.Parallel()

		bad := strings.Replace(planWorkflow, "    needs: infra-code-gate\n", "", 1)
		wf, env := repoWith(t, map[string]string{deployGateFile: bad}, envDecl)
		var out bytes.Buffer
		require.ErrorIs(t, run(wf, env, rulesetOf(env), "", &out), errViolations)
		assert.Contains(t, out.String(), "make infra-code-gate を走らせる job")
	})

	t.Run("workflow のディレクトリが無ければ失敗する", func(t *testing.T) {
		t.Parallel()

		_, env := repoWith(t, nil, envDecl)
		err := run(filepath.Join(t.TempDir(), "missing"), env, rulesetOf(env), "", &bytes.Buffer{})
		require.Error(t, err)
		require.NotErrorIs(t, err, errNoTarget)
	})

	t.Run("environments.json を読めなければ失敗する", func(t *testing.T) {
		t.Parallel()

		wf, env := repoWith(t, map[string]string{deployGateFile: planWorkflow}, envDecl)
		require.ErrorIs(t, run(wf, filepath.Join(t.TempDir(), "missing.json"), rulesetOf(env), "", &bytes.Buffer{}), errEnvDecl)
	})

	t.Run("Pull Request の必須検査が順序の検査を走らせなければ失敗する", func(t *testing.T) {
		t.Parallel()

		wf, env := repoWith(t, map[string]string{deployGateFile: planWorkflow, "apply.yml": applyWorkflow, "gates.yaml": strings.Replace(gatesWorkflow, "        run: make resource-order-check RUNNER_MODE=host\n", "        run: echo\n", 1)}, envDecl)
		var out bytes.Buffer
		require.ErrorIs(t, run(wf, env, rulesetOf(env), "", &out), errViolations)
		assert.Contains(t, out.String(), "make resource-order-check を走らせるものがありません")
	})

	t.Run("2つの検査を走らせる必須検査の job の job 単位の違反は1件だけ報告する", func(t *testing.T) {
		t.Parallel()

		gates := replaced(t, gatesWorkflow, "  tf-lint:\n    needs: changes\n", "  tf-lint:\n    needs: changes\n    env:\n      X: y\n")
		wf, env := repoWith(t, map[string]string{deployGateFile: planWorkflow, "apply.yml": applyWorkflow, "gates.yaml": gates}, envDecl)
		var out bytes.Buffer
		require.ErrorIs(t, run(wf, env, rulesetOf(env), "", &out), errViolations)
		assert.Equal(t, 1, strings.Count(out.String(), "必須検査の job に env: を置かないでください"))
	})

	t.Run("検査ごとに違う step の違反は両方とも報告する", func(t *testing.T) {
		t.Parallel()

		gates := replaced(t, gatesWorkflow, "        run: make resource-order-check RUNNER_MODE=host\n", "        run: make resource-order-check RUNNER_MODE=host\n        if: false\n")
		gates = replaced(t, gates, "        run: make layer-check RUNNER_MODE=host\n", "        run: make layer-check RUNNER_MODE=host\n        if: false\n")
		wf, env := repoWith(t, map[string]string{deployGateFile: planWorkflow, "apply.yml": applyWorkflow, "gates.yaml": gates}, envDecl)
		var out bytes.Buffer
		require.ErrorIs(t, run(wf, env, rulesetOf(env), "", &out), errViolations)
		assert.Contains(t, out.String(), "順序の検査の step に if:")
		assert.Contains(t, out.String(), "層の検査の step に if:")
	})

	t.Run("Pull Request の必須検査が層の検査を走らせなければ失敗する", func(t *testing.T) {
		t.Parallel()

		wf, env := repoWith(t, map[string]string{deployGateFile: planWorkflow, "apply.yml": applyWorkflow, "gates.yaml": strings.Replace(gatesWorkflow, "        run: make layer-check RUNNER_MODE=host\n", "        run: echo\n", 1)}, envDecl)
		var out bytes.Buffer
		require.ErrorIs(t, run(wf, env, rulesetOf(env), "", &out), errViolations)
		assert.Contains(t, out.String(), "make layer-check を走らせるものがありません")
	})

	t.Run("保護設定を読めなければ失敗する", func(t *testing.T) {
		t.Parallel()

		wf, env := repoWith(t, map[string]string{deployGateFile: planWorkflow}, envDecl)
		require.ErrorIs(t, run(wf, env, filepath.Join(t.TempDir(), "missing.json"), "", &bytes.Buffer{}), requiredchecks.ErrRead)
	})

	t.Run("別の workflow の検出の job は needs に数えない", func(t *testing.T) {
		t.Parallel()

		apply := replaced(t, applyWorkflow, "    needs: [apply-infra-code-gate, apply-resource-order-check, apply-layer-check, apply-root-check]\n", "    needs: [infra-code-gate, apply-resource-order-check, apply-layer-check, apply-root-check]\n")
		wf, env := repoWith(t, map[string]string{deployGateFile: planWorkflow, "apply.yml": apply}, envDecl)
		var out bytes.Buffer
		require.ErrorIs(t, run(wf, env, rulesetOf(env), "", &out), errViolations)
		assert.Contains(t, out.String(), "make infra-code-gate を走らせる job")
	})
}

func Test_readEnvironments(t *testing.T) {
	t.Parallel()

	t.Run("名前の一覧を返す", func(t *testing.T) {
		t.Parallel()

		_, env := repoWith(t, nil, envDecl)
		got, err := readEnvironments(env)
		require.NoError(t, err)
		assert.Equal(t, envs, got)
	})

	cases := map[string]string{
		"JSON でなければ失敗する":  "{",
		"空の並びは失敗する":       "[]",
		"名前の無いものだけなら失敗する": `[{"deploy_from":"any"}]`,
	}
	for name, decl := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, env := repoWith(t, nil, decl)
			_, err := readEnvironments(env)
			require.ErrorIs(t, err, errEnvDecl)
		})
	}

	t.Run("ファイルが無ければ失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := readEnvironments(filepath.Join(t.TempDir(), "missing.json"))
		require.ErrorIs(t, err, errEnvDecl)
	})
}

func Test_check(t *testing.T) {
	t.Parallel()

	t.Run("規則を満たす plan の workflow は対象1件・違反0件", func(t *testing.T) {
		t.Parallel()

		n, f := check(deployGateFile, planWorkflow, envs)
		assert.Equal(t, 1, n)
		assert.Empty(t, f)
	})

	t.Run("id-token を渡して reusable workflow を呼ぶ job は、対象にせず呼び出しの形だけを見る", func(t *testing.T) {
		t.Parallel()

		n, f := check("c.yaml", callerWorkflow, envs)
		assert.Zero(t, n)
		assert.Empty(t, f)
	})

	t.Run("id-token を渡して reusable workflow を呼ぶ job が、ほかの権限を持てば違反", func(t *testing.T) {
		t.Parallel()

		n, f := check("c.yaml", replaced(t, callerWorkflow, "      id-token: write\n", "      id-token: write\n      pull-requests: write\n"), envs)
		assert.Zero(t, n)
		require.Len(t, f, 1, messages(f))
		assert.Contains(t, f[0].Message, "job call-plan: id-token: write を渡して workflow を呼ぶ job の permissions は")
	})

	t.Run("id-token を持たない入口の job は対象にせず違反にする", func(t *testing.T) {
		t.Parallel()

		n, f := check(deployGateFile, strings.Replace(planWorkflow, "      id-token: write\n", "", 1), envs)
		assert.Zero(t, n)
		require.Len(t, f, 1, messages(f))
		assert.Regexp(t, `^job plan-admin-identity は CI の入口（make ci-plan-\*・ci-apply-\*・ci-unlock-\*・ci-hold-\*）を走らせますが（\d+ 行目）、job の permissions に id-token: write がありません。この job はこの検査の規則を受けず、root の経路にも数えません$`, f[0].Message)
		before, _, found := strings.Cut(planWorkflow, "  plan-admin-identity:\n")
		require.True(t, found)
		assert.Equal(t, strings.Count(before, "\n")+1, f[0].Line)
	})

	t.Run("対象の job と id-token を持たない入口の job が並ぶ workflow は、後者だけ違反", func(t *testing.T) {
		t.Parallel()

		src := planWorkflow + "\n  plan-extra:\n    steps:\n      - run: make ci-plan-admin-workload RUNNER_MODE=host\n"
		n, f := check(deployGateFile, src, envs)
		assert.Equal(t, 1, n)
		require.Len(t, f, 1, messages(f))
		assert.Contains(t, f[0].Message, "job plan-extra は CI の入口")
	})

	t.Run("行継続で make と別の行にある入口も、id-token が無ければ違反", func(t *testing.T) {
		t.Parallel()

		n, f := check(deployGateFile, "on:\n  push:\njobs:\n  apply-x:\n    steps:\n      - run: |\n          make \\\n            ci-apply-admin-workload\n", envs)
		assert.Zero(t, n)
		require.Len(t, f, 1, messages(f))
		assert.Contains(t, f[0].Message, "job apply-x は CI の入口")
		assert.Contains(t, f[0].Message, "（8 行目）")
	})

	t.Run("jobs: を持たず入口の行だけがある workflow は違反", func(t *testing.T) {
		t.Parallel()

		n, f := check(deployGateFile, "on:\n  push:\nx: make ci-apply-a\n", envs)
		assert.Zero(t, n)
		require.Len(t, f, 1, messages(f))
		assert.Equal(t, "CI の入口（make ci-plan-*・ci-apply-*・ci-unlock-*・ci-hold-*）を持つ行 1 件のうち 1 件を、job の行として読めません", f[0].Message)
	})

	t.Run("job の外の入口が2行なら2件と報せる", func(t *testing.T) {
		t.Parallel()

		n, f := check(deployGateFile, planWorkflow+"x-note: make ci-apply-a\ny-note: make ci-hold-a\n", envs)
		assert.Equal(t, 1, n)
		require.Len(t, f, 1, messages(f))
		require.Equal(t, 1, strings.Count(planWorkflow, "ci-plan-"), "planWorkflow の入口の行の数が前提と違う")
		assert.Equal(t, "CI の入口（make ci-plan-*・ci-apply-*・ci-unlock-*・ci-hold-*）を持つ行 3 件のうち 2 件を、job の行として読めません", f[0].Message)
	})

	t.Run("permissions を持たない入口の job は違反にする", func(t *testing.T) {
		t.Parallel()

		n, f := check(deployGateFile, strings.Replace(planWorkflow, "    permissions:\n      contents: read\n      id-token: write\n", "", 1), envs)
		assert.Zero(t, n)
		require.Len(t, f, 1, messages(f))
		assert.Contains(t, f[0].Message, "job plan-admin-identity は CI の入口")
	})

	t.Run("入口も id-token も持たない job は違反にしない", func(t *testing.T) {
		t.Parallel()

		n, f := check(deployGateFile, "on:\n  pull_request:\njobs:\n  lint:\n    steps:\n      - run: make lint\n", envs)
		assert.Zero(t, n)
		assert.Empty(t, f)
	})

	t.Run("workflow 全体にだけ id-token を置いた入口の job は両方の違反", func(t *testing.T) {
		t.Parallel()

		src := strings.Replace(planWorkflow, "permissions:\n  contents: read\n\njobs:", "permissions:\n  contents: read\n  id-token: write\n\njobs:", 1)
		src = strings.Replace(src, "      id-token: write\n", "", 1)
		n, f := check(deployGateFile, src, envs)
		assert.Zero(t, n)
		require.Len(t, f, 2, messages(f))
		assert.Contains(t, messages(f), "workflow 全体の permissions に id-token")
		assert.Contains(t, messages(f), "job plan-admin-identity は CI の入口")
	})

	for name, c := range map[string]struct{ from, to string }{
		"jobs: より前のトップレベルのキーの入口は違反": {"\njobs:\n", "\nx-note: make ci-apply-admin-identity\njobs:\n"},
		"jobs: と最初の job の間の入口は違反":   {"\njobs:\n", "\njobs:\n    make ci-apply-admin-identity\n"},
		"桁0の name: の入口も数える":         {"\njobs:\n", "\nname: make ci-apply-admin-identity\njobs:\n"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			n, f := check(deployGateFile, strings.Replace(planWorkflow, c.from, c.to, 1), envs)
			assert.Equal(t, 1, n)
			require.Len(t, f, 1, messages(f))
			assert.Regexp(t, `^CI の入口（make ci-plan-\*・ci-apply-\*・ci-unlock-\*・ci-hold-\*）を持つ行 \d+ 件のうち 1 件を、job の行として読めません$`, f[0].Message)
		})
	}

	t.Run("jobs: の後のトップレベルのキーの入口は違反", func(t *testing.T) {
		t.Parallel()

		n, f := check(deployGateFile, planWorkflow+"x-note: make ci-apply-admin-identity\n", envs)
		assert.Equal(t, 1, n)
		require.Len(t, f, 1, messages(f))
		assert.Contains(t, f[0].Message, "のうち 1 件を、job の行として読めません")
	})

	t.Run("workflow 全体の permissions の id-token は違反", func(t *testing.T) {
		t.Parallel()

		src := strings.Replace(planWorkflow, "permissions:\n  contents: read\n\njobs:", "permissions:\n  contents: read\n  id-token: write\n\njobs:", 1)
		_, f := check(deployGateFile, src, envs)
		assert.Contains(t, messages(f), "workflow 全体の permissions に id-token")
	})

	t.Run("1行の {} の permissions は読めないとして違反", func(t *testing.T) {
		t.Parallel()

		src := strings.Replace(planWorkflow, "    permissions:\n      contents: read\n      id-token: write\n", "    permissions: { contents: read, id-token: write }\n", 1)
		n, f := check(deployGateFile, src, envs)
		assert.Zero(t, n)
		require.Len(t, f, 1, messages(f))
		assert.Contains(t, f[0].Message, "job plan-admin-identity の permissions を読めません")
	})

	t.Run("job の write-all は読めないとして違反", func(t *testing.T) {
		t.Parallel()

		src := strings.Replace(planWorkflow, "    permissions:\n      contents: read\n      id-token: write\n", "    permissions: write-all\n", 1)
		_, f := check(deployGateFile, src, envs)
		require.Len(t, f, 1, messages(f))
		assert.Contains(t, f[0].Message, "job plan-admin-identity の permissions を読めません")
	})

	t.Run("workflow 全体の write-all は違反", func(t *testing.T) {
		t.Parallel()

		src := strings.Replace(planWorkflow, "permissions:\n  contents: read\n\njobs:", "permissions: write-all\n\njobs:", 1)
		_, f := check(deployGateFile, src, envs)
		assert.Contains(t, messages(f), "workflow 全体の permissions を読めません")
	})

	t.Run("jobs: を読めない workflow の id-token は違反", func(t *testing.T) {
		t.Parallel()

		n, f := check(deployGateFile, strings.Replace(planWorkflow, "\njobs:\n", "\n\"jobs\":\n", 1), envs)
		assert.Zero(t, n)
		assert.Contains(t, messages(f), "id-token: write か write-all を持つ行 1 件のうち 1 件を、job の permissions として読めません")
	})

	unreadableCases := map[string]string{
		"値が次の行に続く id-token は読めないとして違反":       "      id-token:\n        write\n",
		"block scalar の id-token は読めないとして違反": "      id-token: >-\n        write\n",
		"tag 付きの id-token は読めないとして違反":        "      id-token: !!str write\n",
		"コロンの前に空白のある id-token は読めないとして違反":    "      id-token : write\n",
	}
	for name, perm := range unreadableCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			n, f := check(deployGateFile, strings.Replace(planWorkflow, "      id-token: write\n", perm, 1), envs)
			assert.Zero(t, n)
			require.NotEmpty(t, f)
			assert.Contains(t, messages(f), "job の permissions として読めません")
			assert.NotContains(t, messages(f), "CI の入口")
		})
	}

	t.Run("引用符付きの permissions キーは job の permissions として読めないので違反", func(t *testing.T) {
		t.Parallel()

		src := strings.Replace(planWorkflow, "    permissions:\n      contents: read\n      id-token: write\n", "    \"permissions\":\n      contents: read\n      id-token: write\n", 1)
		n, f := check(deployGateFile, src, envs)
		assert.Zero(t, n)
		require.NotEmpty(t, f)
		assert.Contains(t, messages(f), "job の permissions として読めません")
		assert.NotContains(t, messages(f), "CI の入口")
	})

	t.Run("id-token: read は token を得る行として数えず、入口の job を違反にする", func(t *testing.T) {
		t.Parallel()

		n, f := check(deployGateFile, strings.Replace(planWorkflow, "      id-token: write\n", "      id-token: read\n", 1), envs)
		assert.Zero(t, n)
		require.Len(t, f, 1, messages(f))
		assert.Contains(t, f[0].Message, "job plan-admin-identity は CI の入口")
	})

	t.Run("長期のアクセスキーの参照は違反", func(t *testing.T) {
		t.Parallel()

		src := planWorkflow + "        env:\n          AWS_ACCESS_KEY_ID: ${{ secrets.KEY }}\n"
		_, f := check(deployGateFile, src, envs)
		require.Len(t, f, 2)
		assert.Contains(t, f[0].Message, "長期のアクセスキー")
		assert.Contains(t, f[1].Message, "env: に置けるのは")
	})

	t.Run("コメントの中のキー名は違反にしない", func(t *testing.T) {
		t.Parallel()

		_, f := check(deployGateFile, "# aws-access-key-id は使わない\n"+planWorkflow, envs)
		assert.Empty(t, f)
	})
}

func Test_checkJob(t *testing.T) {
	t.Parallel()

	t.Run("plan の入口の job が lock の解除の role を取れば、plan の規則で1件だけ role の違反", func(t *testing.T) {
		t.Parallel()

		src := replaced(t, planWorkflow, "CI_ROLE=plan", "CI_ROLE=unlock-identity")
		src = replaced(t, src, "environment: admin-plan", "environment: admin-unlock-identity")
		_, f := check(deployGateFile, src, envs)
		got := messages(f)
		assert.Equal(t, 1, strings.Count(got, "lock の解除の job（make ci-unlock-*）だけが取って"), got)
		assert.NotContains(t, got, "apply の job は")
		assert.NotContains(t, got, "plan の job は plan の role")
	})

	logArchive := func(t *testing.T) string {
		t.Helper()

		src := strings.ReplaceAll(planWorkflow, "CI_ENV=admin", "CI_ENV=log-archive")
		src = replaced(t, src, "environment: admin-plan", "environment: log-archive-plan")
		return replaced(t, src, "make ci-plan-admin-identity", "make ci-plan-log-archive-identity")
	}
	logArchiveEnvs := append(slices.Clone(envs), "log-archive-plan")

	t.Run("ハイフンを含む env の plan の job は、資格情報と拒否の確認と入口を通して違反0件", func(t *testing.T) {
		t.Parallel()

		n, f := check(deployGateFile, logArchive(t), logArchiveEnvs)
		assert.Equal(t, 1, n)
		assert.Empty(t, messages(f))
	})

	t.Run("ハイフンを含む env の資格情報と、入口の env の食い違いは違反", func(t *testing.T) {
		t.Parallel()

		_, f := check(deployGateFile, replaced(t, logArchive(t), "make ci-plan-log-archive-identity", "make ci-plan-archive-identity"), logArchiveEnvs)
		assert.Contains(t, messages(f), "資格情報の CI_ENV=log-archive が、入口 make ci-plan-archive-identity の env archive と違います")
	})

	t.Run("ハイフンを含む env の資格情報と、拒否の確認の CI_ENV の食い違いは違反", func(t *testing.T) {
		t.Parallel()

		_, f := check(deployGateFile, replaced(t, logArchive(t), "ci-assume-denied CI_ENV=log-archive", "ci-assume-denied CI_ENV=log"), logArchiveEnvs)
		assert.Contains(t, messages(f), "make ci-assume-denied の CI_ENV=log が、資格情報の CI_ENV=log-archive と違います")
	})

	t.Run("checkJob を通しても入口の env と CI_ENV の食い違いを出す", func(t *testing.T) {
		t.Parallel()

		_, f := check("a.yaml", replaced(t, applyWorkflow, "make ci-apply-admin-identity", "make ci-apply-stg-identity"), envs)
		assert.Contains(t, messages(f), "資格情報の CI_ENV=admin が、入口 make ci-apply-stg-identity の env stg と違います")
	})

	for _, c := range []struct{ kind, src, entry, other string }{
		{cientry.Plan, planWorkflow, "      - run: make ci-plan-admin-identity RUNNER_MODE=host\n", "      - run: make ci-plan-stg-workload RUNNER_MODE=host\n"},
		{cientry.Apply, applyWorkflow, applyEntryLine, "      - run: make ci-apply-admin-workload RUNNER_MODE=host\n"},
		{cientry.Unlock, unlockWorkflow, unlockEntryLine, "      - run: make ci-unlock-admin-workload RUNNER_MODE=host\n"},
		{cientry.Hold, holdWorkflow, holdEntryLine, "      - run: make ci-hold-admin-workload RUNNER_MODE=host\n"},
	} {
		for n, to := range map[int]string{0: "", 2: c.other + c.entry} {
			t.Run(fmt.Sprintf("%s の入口が %d 件なら role を比べずに数の違反を出す", c.kind, n), func(t *testing.T) {
				t.Parallel()

				_, f := check("w.yaml", replaced(t, c.src, c.entry, to), envs)
				got := messages(f)
				// 入口0件の lock の解除と lock を残す job は、入口で種別が決まるので apply の job として数の違反になる。
				assert.Contains(t, got, fmt.Sprintf("を1回だけ走らせてください（%d 回）", n))
				if n == 2 {
					assert.Contains(t, got, fmt.Sprintf("make ci-%s-* を1回だけ走らせてください（2 回）", c.kind))
				}
				assert.NotContains(t, got, "の root が決める role")
				assert.NotContains(t, got, "資格情報の CI_ENV=")
			})
		}
	}

	w := infoOf(planWorkflow)
	jobWith := func(t *testing.T, from, to string) workflow.Job {
		t.Helper()

		return jobOf(t, replaced(t, planWorkflow, from, to), "plan-admin-identity")
	}

	cases := map[string]struct{ from, to, want string }{
		"environment が無ければ違反":        {"    environment: admin-plan\n", "", "environment を字面で宣言"},
		"式の environment は違反":         {"environment: admin-plan", "environment: ${{ inputs.env }}", "environment を字面で宣言"},
		"宣言に無い environment は違反":      {"environment: admin-plan", "environment: admin-planx", "environments.json にありません"},
		"ci-aws-credentials が無ければ違反": {"make ci-aws-credentials CI_ENV=admin CI_ROLE=plan", "echo", "1回だけ呼んで"},
		"CI_ROLE が無ければ違反":            {" CI_ROLE=plan", "", "CI_ENV と CI_ROLE を明示"},
		"CI_ENV が無ければ違反":             {" CI_ENV=admin", "", "CI_ENV と CI_ROLE を明示"},
		"environment と役割の食い違いは違反":    {"environment: admin-plan", "environment: admin-apply", "一致しません"},
		"environment と env の食い違いは違反": {"CI_ENV=admin", "CI_ENV=dev", "一致しません"},
		"needs に検出が無ければ違反":           {"needs: infra-code-gate", "needs: [changes]", "make infra-code-gate を走らせる job"},
		"入口を隠す形も検査する":                {"    environment: admin-plan\n", "    environment: admin-plan\n    continue-on-error: true\n", "continue-on-error: を置かない"},
		"資格情報の step の SHELL の代入は違反":  {"CI_ROLE=plan RUNNER_MODE=host", "CI_ROLE=plan RUNNER_MODE=host SHELL='/bin/bash -x'", "資格情報を取る step は"},
		"資格情報の step の道具の差し替えは違反":     {"CI_ROLE=plan RUNNER_MODE=host", "CI_ROLE=plan AWS_BIN=./x RUNNER_MODE=host", "資格情報を取る step は"},
		"資格情報の step の後続のコマンドは違反":     {"CI_ROLE=plan RUNNER_MODE=host", "CI_ROLE=plan RUNNER_MODE=host; cat ~/.aws/credentials", "資格情報を取る step は"},
		"ブロックスカラーの資格情報の step は違反":    {"        run: make ci-aws-credentials CI_ENV=admin CI_ROLE=plan RUNNER_MODE=host", "        run: |\n          make ci-aws-credentials CI_ENV=admin CI_ROLE=plan RUNNER_MODE=host\n          cat ~/.aws/credentials", "資格情報を取る step は"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Contains(t, messages(checkJob("p.yaml", jobWith(t, c.from, c.to), planPerms, envs, w)), c.want)
		})
	}

	t.Run("CI_ROLE の役割は usecases/deployment-identity の role と一致する", func(t *testing.T) {
		t.Parallel()

		tf, err := os.ReadFile(filepath.Join("..", "..", "usecases", "deployment-identity", "internal", "github-oidc", "main.tf"))
		require.NoError(t, err)
		var declared []string
		for _, m := range regexp.MustCompile(`"\$\{var\.name_prefix\}-ci-([a-z-]+)"`).FindAllStringSubmatch(string(tf), -1) {
			declared = append(declared, m[1])
		}
		alt := regexp.MustCompile(`\(([a-z|-]+)\)`).FindStringSubmatch(ciRole.String())
		require.NotNil(t, alt)
		accepted := strings.Split(alt[1], "|")
		slices.Sort(declared)
		slices.Sort(accepted)
		assert.NotEmpty(t, declared)
		assert.Equal(t, declared, accepted)
	})

	t.Run("資格情報の step に RUNNER_MODE=host が無くても違反ではない", func(t *testing.T) {
		t.Parallel()

		job := jobWith(t, "CI_ROLE=plan RUNNER_MODE=host", "CI_ROLE=plan")
		assert.NotContains(t, messages(checkJob("p.yaml", job, planPerms, envs, w)), "資格情報を取る step は")
	})

	t.Run("ci-aws-credentials を2回呼べば違反", func(t *testing.T) {
		t.Parallel()

		job := jobWith(t, "      - run: make ci-plan-admin-identity", "      - run: make ci-aws-credentials CI_ENV=admin CI_ROLE=plan\n      - run: make ci-plan-admin-identity")
		assert.Contains(t, messages(checkJob("p.yaml", job, planPerms, envs, w)), "（2 回）")
	})

	t.Run("contents: read と id-token: write 以外の権限は違反", func(t *testing.T) {
		t.Parallel()

		f := checkJob("p.yaml", jobOf(t, planWorkflow, "plan-admin-identity"), append(planPerms, "pull-requests: write"), envs, w)
		assert.Contains(t, messages(f), "pull-requests: write")
	})

	t.Run("資格情報の後の action は違反", func(t *testing.T) {
		t.Parallel()

		job := jobWith(t, "      - run: make ci-plan-admin-identity", "      - uses: octo/comment@v1\n      - run: make ci-plan-admin-identity")
		assert.Contains(t, messages(checkJob("p.yaml", job, planPerms, envs, w)), "action を呼ばない")
	})

	t.Run("name: の中の ci-aws-credentials は呼び出しと数えない", func(t *testing.T) {
		t.Parallel()

		job := jobOf(t, strings.Replace(strings.Replace(planWorkflow, "        run: make ci-aws-credentials CI_ENV=admin CI_ROLE=plan RUNNER_MODE=host", "        run: aws sts assume-role-with-web-identity", 1), "      - name: Run terraform plan through make", "      - name: make ci-aws-credentials CI_ENV=admin CI_ROLE=plan", 1), "plan-admin-identity")
		assert.Contains(t, messages(checkJob("p.yaml", job, planPerms, envs, w)), "（0 回）")
	})

	t.Run("name: に ci-aws-credentials を含んでも run: の1回だけを数える", func(t *testing.T) {
		t.Parallel()

		job := jobOf(t, strings.Replace(planWorkflow, "      - name: Run terraform plan through make", "      - name: make ci-aws-credentials CI_ENV=admin CI_ROLE=plan", 1), "plan-admin-identity")
		assert.Empty(t, checkJob("p.yaml", job, planPerms, envs, w))
	})

	t.Run("規則を満たす job は違反0件", func(t *testing.T) {
		t.Parallel()

		assert.Empty(t, checkJob("p.yaml", jobOf(t, planWorkflow, "plan-admin-identity"), planPerms, envs, w))
	})

	t.Run("規則を満たす apply の job は違反0件", func(t *testing.T) {
		t.Parallel()

		assert.Empty(t, checkJob("a.yaml", jobOf(t, applyWorkflow, "apply-admin-identity"), planPerms, envs, infoOf(applyWorkflow)))
	})

	t.Run("plan の job が unlock-identity の role を取れば違反", func(t *testing.T) {
		t.Parallel()

		src := replaced(t, replaced(t, planWorkflow, "CI_ROLE=plan", "CI_ROLE=unlock-identity"), "environment: admin-plan", "environment: admin-unlock-identity")
		findings := checkJob("p.yaml", jobOf(t, src, "plan-admin-identity"), planPerms, envs, infoOf(src))
		require.Len(t, findings, 1, "役割の違反だけを1件出す: "+messages(findings))
		assert.Contains(t, findings[0].Message, "unlock-identity の role は lock の解除の job（make ci-unlock-*）だけが取って")
	})

	t.Run("plan の job が unlock の role を取れば、役割の違反だけを出す", func(t *testing.T) {
		t.Parallel()

		src := replaced(t, replaced(t, planWorkflow, "CI_ROLE=plan", "CI_ROLE=unlock"), "environment: admin-plan", "environment: admin-unlock")
		findings := checkJob("p.yaml", jobOf(t, src, "plan-admin-identity"), planPerms, envs, infoOf(src))
		require.Len(t, findings, 1, "役割の違反だけを1件出す: "+messages(findings))
		assert.Contains(t, findings[0].Message, "unlock の role は lock の解除の job（make ci-unlock-*）だけが取って")
	})

	t.Run("apply の job が unlock-identity の role を取れば違反", func(t *testing.T) {
		t.Parallel()

		src := replaced(t, replaced(t, applyWorkflow, "CI_ROLE=apply-identity", "CI_ROLE=unlock-identity"), "name: admin-apply-identity", "name: admin-unlock-identity")
		got := messages(checkJob("a.yaml", jobOf(t, src, "apply-admin-identity"), planPerms, envs, infoOf(src)))
		assert.Contains(t, got, "unlock-identity の role は lock の解除の job（make ci-unlock-*）だけが取って")
		assert.NotContains(t, got, "一致しません")
	})

	t.Run("apply の役割は apply の規則で見る", func(t *testing.T) {
		t.Parallel()

		aw := infoOf(applyWorkflow)
		aw.on = map[string][]workflow.Line{"pull_request": nil}
		f := checkJob("a.yaml", jobOf(t, applyWorkflow, "apply-admin-identity"), planPerms, envs, aw)
		assert.Contains(t, messages(f), "push と workflow_dispatch だけ")
		assert.NotContains(t, messages(f), "一致しません", "CI_ROLE=apply-identity を apply と読まない")
	})
}

func Test_checkPlan(t *testing.T) {
	t.Parallel()

	w := infoOf(planWorkflow)
	planWith := func(t *testing.T, from, to string) workflow.Job {
		t.Helper()

		return jobOf(t, replaced(t, planWorkflow, from, to), "plan-admin-identity")
	}

	t.Run("規則を満たせば違反0件", func(t *testing.T) {
		t.Parallel()

		job := jobOf(t, planWorkflow, "plan-admin-identity")
		assert.Empty(t, checkPlan(job, credOf(job), w, formatted))
	})

	t.Run("pull_request 以外の起動は違反", func(t *testing.T) {
		t.Parallel()

		job := jobOf(t, planWorkflow, "plan-admin-identity")
		pw := infoOf(planWorkflow)
		pw.on = map[string][]workflow.Line{"pull_request": nil, "push": nil}
		assert.Contains(t, messages(checkPlan(job, credOf(job), pw, formatted)), "pull_request だけ")
	})

	t.Run("workflow_call だけの起動は、ファイルの名前に依らず違反0件", func(t *testing.T) {
		t.Parallel()

		job := jobOf(t, planWorkflow, "plan-admin-identity")
		pw := infoOf(planWorkflow)
		pw.file = "terraform-plan.yaml"
		pw.on = map[string][]workflow.Line{"workflow_call": nil}
		assert.Empty(t, checkPlan(job, credOf(job), pw, formatted))
	})

	t.Run("deploy-gate.yaml の外の pull_request だけの起動は違反", func(t *testing.T) {
		t.Parallel()

		job := jobOf(t, planWorkflow, "plan-admin-identity")
		pw := infoOf(planWorkflow)
		pw.file = ".github/workflows/terraform-gates.yaml"
		assert.Contains(t, messages(checkPlan(job, credOf(job), pw, formatted)), "workflow_call だけか、deploy-gate.yaml の pull_request だけ")
	})

	t.Run("workflow_call とほかの起動の併用は違反", func(t *testing.T) {
		t.Parallel()

		job := jobOf(t, planWorkflow, "plan-admin-identity")
		pw := infoOf(planWorkflow)
		pw.on = map[string][]workflow.Line{"workflow_call": nil, "pull_request": nil}
		assert.Contains(t, messages(checkPlan(job, credOf(job), pw, formatted)), "（pull_request, workflow_call）")
	})

	t.Run("pull_request_target だけの起動は違反", func(t *testing.T) {
		t.Parallel()

		job := jobOf(t, planWorkflow, "plan-admin-identity")
		pw := infoOf(planWorkflow)
		pw.on = map[string][]workflow.Line{"pull_request_target": nil}
		assert.Contains(t, messages(checkPlan(job, credOf(job), pw, formatted)), "pull_request だけ")
	})

	t.Run("拒否の step が無ければ違反", func(t *testing.T) {
		t.Parallel()

		job := planWith(t, "id: refuse-untrusted", "id: other")
		assert.Contains(t, messages(checkPlan(job, credOf(job), w, formatted)), "id が refuse-untrusted の step")
	})

	t.Run("make ci-plan-* 以外の terraform は違反", func(t *testing.T) {
		t.Parallel()

		job := planWith(t, "      - run: make ci-plan-admin-identity RUNNER_MODE=host\n", "      - run: make ci-plan-admin-identity RUNNER_MODE=host\n      - run: terraform -chdir=env/admin/identity plan\n")
		assert.Contains(t, messages(checkPlan(job, credOf(job), w, formatted)), "make ci-plan-* でだけ")
	})

	t.Run("make ci-apply-* は違反", func(t *testing.T) {
		t.Parallel()

		job := planWith(t, "      - run: make ci-plan-admin-identity RUNNER_MODE=host\n", "      - run: make ci-plan-admin-identity RUNNER_MODE=host\n      - run: make ci-apply-admin-identity RUNNER_MODE=host\n")
		assert.Contains(t, messages(checkPlan(job, credOf(job), w, formatted)), "make ci-plan-* でだけ")
	})
}

func Test_checkApply(t *testing.T) {
	t.Parallel()

	w := infoOf(applyWorkflow)
	job := jobOf(t, applyWorkflow, "apply-admin-identity")
	applyWith := func(t *testing.T, from, to string) (workflow.Job, workflowInfo) {
		t.Helper()

		src := replaced(t, applyWorkflow, from, to)

		return jobOf(t, src, "apply-admin-identity"), infoOf(src)
	}

	t.Run("規則を満たせば違反0件", func(t *testing.T) {
		t.Parallel()

		assert.Empty(t, checkApply(job, credOf(job), w, formatted))
	})

	t.Run("workflow 全体と job の両方が cancel-in-progress: false なら違反0件", func(t *testing.T) {
		t.Parallel()

		j, aw := applyWith(t, applyEnv, applyEnv+jobConcurrency("false"))
		assert.Empty(t, checkApply(j, credOf(j), aw, formatted))
	})

	t.Run("入力の無い workflow_dispatch は違反にしない", func(t *testing.T) {
		t.Parallel()

		j, aw := applyWith(t, "    branches: ['release/*']\n", "    branches: ['release/*']\n  workflow_dispatch:\n")
		assert.Empty(t, checkApply(j, credOf(j), aw, formatted))
	})

	t.Run("pull_request の起動は違反", func(t *testing.T) {
		t.Parallel()

		aw := infoOf(applyWorkflow)
		aw.on = map[string][]workflow.Line{"push": nil, "pull_request": nil}
		assert.Contains(t, messages(checkApply(job, credOf(job), aw, formatted)), "push と workflow_dispatch だけ")
	})

	t.Run("起動条件が読めなければ違反", func(t *testing.T) {
		t.Parallel()

		aw := infoOf(applyWorkflow)
		aw.on = map[string][]workflow.Line{}
		assert.Contains(t, messages(checkApply(job, credOf(job), aw, formatted)), "push と workflow_dispatch だけ")
	})

	t.Run("workflow_dispatch の inputs は違反", func(t *testing.T) {
		t.Parallel()

		j, aw := applyWith(t, "    branches: ['release/*']\n", "    branches: ['release/*']\n  workflow_dispatch:\n    inputs:\n      ref:\n        type: string\n")
		assert.Contains(t, messages(checkApply(j, credOf(j), aw, formatted)), "inputs を置かない")
	})

	t.Run("workflow の cancel-in-progress: true は違反", func(t *testing.T) {
		t.Parallel()

		j, aw := applyWith(t, "  cancel-in-progress: false\n", "  cancel-in-progress: true\n")
		assert.Contains(t, messages(checkApply(j, credOf(j), aw, formatted)), "cancel-in-progress: false の concurrency")
	})

	t.Run("job の cancel-in-progress: false でも満たす", func(t *testing.T) {
		t.Parallel()

		src := replaced(t, applyWorkflow, wfConcurrency, "")
		src = replaced(t, src, applyEnv, applyEnv+jobConcurrency("false"))
		j := jobOf(t, src, "apply-admin-identity")
		assert.Empty(t, checkApply(j, credOf(j), infoOf(src), formatted))
	})

	t.Run("拒否の step が無ければ違反", func(t *testing.T) {
		t.Parallel()

		j, aw := applyWith(t, "id: refuse-stale", "id: other")
		assert.Contains(t, messages(checkApply(j, credOf(j), aw, formatted)), "id が refuse-stale の step")
	})

	t.Run("plan の job の拒否の step では満たさない", func(t *testing.T) {
		t.Parallel()

		j, aw := applyWith(t, "id: refuse-stale", "id: refuse-untrusted")
		assert.Contains(t, messages(checkApply(j, credOf(j), aw, formatted)), "id が refuse-stale の step")
	})

	t.Run("make ci-apply-* 以外の terraform は違反", func(t *testing.T) {
		t.Parallel()

		j, aw := applyWith(t, "      - run: make ci-apply-admin-identity RUNNER_MODE=host\n", "      - run: make ci-apply-admin-identity RUNNER_MODE=host\n      - run: terraform -chdir=env/admin/identity apply -auto-approve\n")
		assert.Contains(t, messages(checkApply(j, credOf(j), aw, formatted)), "make ci-apply-* でだけ")
	})

	t.Run("make ci-plan-* は違反", func(t *testing.T) {
		t.Parallel()

		j, aw := applyWith(t, "      - run: make ci-apply-admin-identity RUNNER_MODE=host\n", "      - run: make ci-plan-admin-identity RUNNER_MODE=host\n      - run: make ci-apply-admin-identity RUNNER_MODE=host\n")
		assert.Contains(t, messages(checkApply(j, credOf(j), aw, formatted)), "make ci-apply-* でだけ")
	})

	t.Run("make ci-apply-* が無ければ違反", func(t *testing.T) {
		t.Parallel()

		j, aw := applyWith(t, "make ci-apply-admin-identity", "echo")
		assert.Contains(t, messages(checkApply(j, credOf(j), aw, formatted)), "apply の job は make ci-apply-* を1回だけ走らせてください（0 回）")
	})

	t.Run("workflow の cancel-in-progress: true は job の false で隠せない", func(t *testing.T) {
		t.Parallel()

		src := replaced(t, applyWorkflow, "  cancel-in-progress: false\n", "  cancel-in-progress: true\n")
		src = replaced(t, src, applyEnv, applyEnv+jobConcurrency("false"))
		j := jobOf(t, src, "apply-admin-identity")
		assert.Contains(t, messages(checkApply(j, credOf(j), infoOf(src), formatted)), "cancel-in-progress: false の concurrency")
	})

	t.Run("push の paths: は違反", func(t *testing.T) {
		t.Parallel()

		for name, key := range map[string]string{"paths": "paths", "paths-ignore": "paths-ignore", "引用符付き": "\"paths\""} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				j, aw := applyWith(t, "    branches: ['release/*']\n", "    branches: ['release/*']\n    "+key+":\n      - 'env/**'\n")
				assert.Contains(t, messages(checkApply(j, credOf(j), aw, formatted)), "push に paths を置かない")
			})
		}
	})

	t.Run("concurrency がどこにも無ければ違反", func(t *testing.T) {
		t.Parallel()

		j, aw := applyWith(t, wfConcurrency, "")
		assert.Contains(t, messages(checkApply(j, credOf(j), aw, formatted)), "cancel-in-progress: false の concurrency")
	})

	t.Run("job の cancel-in-progress: true だけでは違反", func(t *testing.T) {
		t.Parallel()

		src := replaced(t, replaced(t, applyWorkflow, wfConcurrency, ""), applyEnv, applyEnv+jobConcurrency("true"))
		j := jobOf(t, src, "apply-admin-identity")
		assert.Contains(t, messages(checkApply(j, credOf(j), infoOf(src), formatted)), "cancel-in-progress: false の concurrency")
	})

	t.Run("workflow の false は job の cancel-in-progress: true を隠せない", func(t *testing.T) {
		t.Parallel()

		j, aw := applyWith(t, applyEnv, applyEnv+jobConcurrency("true"))
		assert.Contains(t, messages(checkApply(j, credOf(j), aw, formatted)), "cancel-in-progress: false の concurrency")
	})

	t.Run("単引用符の workflow の concurrency も読む", func(t *testing.T) {
		t.Parallel()

		src := replaced(t, applyWorkflow, wfConcurrency, "'concurrency':\n  group: x\n  'cancel-in-progress': true\n")
		src = replaced(t, src, applyEnv, applyEnv+jobConcurrency("false"))
		j := jobOf(t, src, "apply-admin-identity")
		assert.Contains(t, messages(checkApply(j, credOf(j), infoOf(src), formatted)), "cancel-in-progress: false の concurrency")
	})

	t.Run("workflow の1行の concurrency は違反", func(t *testing.T) {
		t.Parallel()

		j, aw := applyWith(t, wfConcurrency, "concurrency: x\n")
		assert.Contains(t, messages(checkApply(j, credOf(j), aw, formatted)), "cancel-in-progress: false の concurrency")
	})

	t.Run("起動条件のフロー形式は読めないとして違反", func(t *testing.T) {
		t.Parallel()

		for name, on := range map[string]string{
			"push":              "  push: { branches: ['release/*'], paths: ['env/**'] }\n",
			"workflow_dispatch": "  push:\n    branches: ['release/*']\n  workflow_dispatch: { inputs: { ref: { type: string } } }\n",
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				j, aw := applyWith(t, "  push:\n    branches: ['release/*']\n", on)
				assert.Contains(t, messages(checkApply(j, credOf(j), aw, formatted)), "フロー形式")
			})
		}
	})

	t.Run("桁の違う paths も違反", func(t *testing.T) {
		t.Parallel()

		j, aw := applyWith(t, "    branches: ['release/*']\n", "    branches: ['release/*']\n   paths:\n     - 'env/**'\n")
		assert.Contains(t, messages(checkApply(j, credOf(j), aw, formatted)), "push に paths を置かない")
	})

	t.Run("workflow_dispatch の inputs の別の書き方も違反", func(t *testing.T) {
		t.Parallel()

		for name, key := range map[string]string{"引用符付き": "    \"inputs\":", "コロンの前の空白": "    inputs :", "桁の違い": "      inputs:"} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				j, aw := applyWith(t, "    branches: ['release/*']\n", "    branches: ['release/*']\n  workflow_dispatch:\n"+key+"\n        ref:\n          type: string\n")
				assert.Contains(t, messages(checkApply(j, credOf(j), aw, formatted)), "inputs を置かない")
			})
		}
	})
}

func Test_checkDetection(t *testing.T) {
	t.Parallel()

	w := infoOf(applyWorkflow)
	applyWith := func(t *testing.T, from, to string) (workflow.Job, workflowInfo) {
		t.Helper()

		src := replaced(t, applyWorkflow, from, to)

		return jobOf(t, src, "apply-admin-identity"), infoOf(src)
	}

	t.Run("検出の job に依り、if: がその成功を見れば違反0件", func(t *testing.T) {
		t.Parallel()

		assert.Empty(t, checkDetection(jobOf(t, applyWorkflow, "apply-admin-identity"), w.jobs, formatted))
	})

	t.Run("if: が無ければ既定の成功の条件で足りる", func(t *testing.T) {
		t.Parallel()

		j, aw := applyWith(t, "    if: ${{ needs.apply-infra-code-gate.result == 'success' && needs.apply-resource-order-check.result == 'success' && needs.apply-layer-check.result == 'success' && needs.apply-root-check.result == 'success' && github.ref_name == github.event.repository.default_branch }}\n", "")
		assert.Empty(t, checkDetection(j, aw.jobs, formatted))
	})

	t.Run("infra-code-gate という名前の job も中身で認める", func(t *testing.T) {
		t.Parallel()

		f := checkDetection(jobOf(t, planWorkflow, "plan-admin-identity"), infoOf(planWorkflow).jobs, formatted)
		assert.Empty(t, f, "infra-code-gate という名前の job も、make infra-code-gate を走らせるので認める")
	})

	t.Run("needs の job が検出を走らせなければ違反", func(t *testing.T) {
		t.Parallel()

		j, aw := applyWith(t, "      - run: make infra-code-gate RUNNER_MODE=host\n", "")
		assert.Contains(t, messages(checkDetection(j, aw.jobs, formatted)), "make infra-code-gate を走らせる job")
	})

	t.Run("検出に似た名前のターゲットだけの job は検出と認めない", func(t *testing.T) {
		t.Parallel()

		j, aw := applyWith(t, "      - run: make infra-code-gate RUNNER_MODE=host\n", "      - run: make infra-code-gate-test RUNNER_MODE=host\n")
		assert.Contains(t, messages(checkDetection(j, aw.jobs, formatted)), "make infra-code-gate を走らせる job")
	})

	t.Run("name: とコメントの中の検出は認めない", func(t *testing.T) {
		t.Parallel()

		j, aw := applyWith(t, "      - run: make infra-code-gate RUNNER_MODE=host\n", "      - name: make infra-code-gate\n        run: echo\n        # make infra-code-gate\n")
		assert.Contains(t, messages(checkDetection(j, aw.jobs, formatted)), "make infra-code-gate を走らせる job")
	})

	t.Run("needs の先の job が無ければ違反", func(t *testing.T) {
		t.Parallel()

		assert.Contains(t, messages(checkDetection(jobOf(t, applyWorkflow, "apply-admin-identity"), infoOf(planWorkflow).jobs, formatted)), "make infra-code-gate を走らせる job")
	})

	t.Run("if: が検出の成功を見なければ違反", func(t *testing.T) {
		t.Parallel()

		j, aw := applyWith(t, "needs.apply-infra-code-gate.result == 'success' && ", "always() && ")
		assert.Contains(t, messages(checkDetection(j, aw.jobs, formatted)), "needs.apply-infra-code-gate.result == 'success'")
	})

	t.Run("検出の job の if: は違反", func(t *testing.T) {
		t.Parallel()

		j, aw := applyWith(t, "  apply-infra-code-gate:\n    runs-on: ubuntu-24.04\n", "  apply-infra-code-gate:\n    if: github.event_name == 'push'\n    runs-on: ubuntu-24.04\n")
		assert.Contains(t, messages(checkDetection(j, aw.jobs, formatted)), "検出の job apply-infra-code-gate に if:")
	})

	t.Run("検出の job の continue-on-error: は違反", func(t *testing.T) {
		t.Parallel()

		for name, to := range map[string]string{
			"job":  "  apply-infra-code-gate:\n    continue-on-error: true\n    runs-on: ubuntu-24.04\n",
			"step": "  apply-infra-code-gate:\n    runs-on: ubuntu-24.04\n    steps:\n      - run: make infra-code-gate\n        continue-on-error: true\n",
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				src := strings.Replace(applyWorkflow, "  apply-infra-code-gate:\n    runs-on: ubuntu-24.04\n", to, 1)
				assert.Contains(t, messages(checkDetection(jobOf(t, src, "apply-admin-identity"), infoOf(src).jobs, formatted)), "continue-on-error:")
			})
		}
	})

	t.Run("検出の step の if: は違反", func(t *testing.T) {
		t.Parallel()

		for name, cond := range map[string]string{"1行": "        if: ${{ false }}\n", "引用符付き": "        \"if\": false\n"} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				j, aw := applyWith(t, "      - run: make infra-code-gate RUNNER_MODE=host\n", "      - run: make infra-code-gate RUNNER_MODE=host\n"+cond)
				assert.Contains(t, messages(checkDetection(j, aw.jobs, formatted)), "検出の job apply-infra-code-gate に if:・continue-on-error:")
			})
		}
	})

	t.Run("引用符付きの continue-on-error: は違反", func(t *testing.T) {
		t.Parallel()

		j, aw := applyWith(t, "  apply-infra-code-gate:\n    runs-on: ubuntu-24.04\n", "  apply-infra-code-gate:\n    'continue-on-error': true\n    runs-on: ubuntu-24.04\n")
		assert.Contains(t, messages(checkDetection(j, aw.jobs, formatted)), "検出の job apply-infra-code-gate に if:・continue-on-error:")
	})

	t.Run("検出の job の needs: は違反", func(t *testing.T) {
		t.Parallel()

		j, aw := applyWith(t, "  apply-infra-code-gate:\n    runs-on: ubuntu-24.04\n", "  apply-infra-code-gate:\n    needs: changes\n    runs-on: ubuntu-24.04\n")
		assert.Contains(t, messages(checkDetection(j, aw.jobs, formatted)), "検出の job apply-infra-code-gate に needs:")
	})

	t.Run("status の関数や論理和で検出の失敗を越える if: は違反", func(t *testing.T) {
		t.Parallel()

		for name, prefix := range map[string]string{
			"|| だけ":          "github.ref_name == 'x' || ",
			"! だけ":           "!github.event.repository.fork && ",
			"always() だけ":    "always() && ",
			"cancelled() だけ": "cancelled() && ",
			"failure() だけ":   "failure() && ",
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				j, aw := applyWith(t, "if: ${{ needs.apply-infra-code-gate", "if: ${{ "+prefix+"needs.apply-infra-code-gate")
				assert.Contains(t, messages(checkDetection(j, aw.jobs, formatted)), "status の関数")
			})
		}
	})

	t.Run("引用符付きの if: も読む", func(t *testing.T) {
		t.Parallel()

		j, aw := applyWith(t, "    if: ${{ needs.apply-infra-code-gate.result == 'success' && ", "    \"if\": ${{ always() && ")
		assert.Contains(t, messages(checkDetection(j, aw.jobs, formatted)), "needs.apply-infra-code-gate.result == 'success'")
	})

	t.Run("!= の比較は status の関数と読まない", func(t *testing.T) {
		t.Parallel()

		j, aw := applyWith(t, "github.ref_name == github.event.repository.default_branch", "github.ref_name != 'x'")
		assert.Empty(t, checkDetection(j, aw.jobs, formatted))
	})

	t.Run("status の関数を持たず検出の成功も見なければ違反", func(t *testing.T) {
		t.Parallel()

		j, aw := applyWith(t, "needs.apply-infra-code-gate.result == 'success' && ", "")
		f := messages(checkDetection(j, aw.jobs, formatted))
		assert.Contains(t, f, "needs.apply-infra-code-gate.result == 'success'")
		assert.NotContains(t, f, "status の関数")
	})

	t.Run("needs の result の別の比較では満たさない", func(t *testing.T) {
		t.Parallel()

		j, aw := applyWith(t, "result == 'success'", "result != 'failure'")
		assert.Contains(t, messages(checkDetection(j, aw.jobs, formatted)), "needs.apply-infra-code-gate.result == 'success' を含めて")
	})

	t.Run("成功の比較を別の比較で包む if: は違反", func(t *testing.T) {
		t.Parallel()

		j, aw := applyWith(t, "${{ needs.apply-infra-code-gate.result == 'success' &&", "${{ (needs.apply-infra-code-gate.result == 'success') == false &&")
		assert.Contains(t, messages(checkDetection(j, aw.jobs, formatted)), "needs.apply-infra-code-gate.result == 'success' を含めて")
	})

	t.Run("ブロックの if: も読む", func(t *testing.T) {
		t.Parallel()

		j, aw := applyWith(t, "    if: ${{ needs.apply-infra-code-gate.result == 'success' && needs.apply-resource-order-check.result == 'success' && needs.apply-layer-check.result == 'success' && needs.apply-root-check.result == 'success' && github.ref_name == github.event.repository.default_branch }}\n",
			"    if: >-\n      needs.apply-infra-code-gate.result == 'success' &&\n      needs.apply-resource-order-check.result == 'success' &&\n      needs.apply-layer-check.result == 'success' &&\n      needs.apply-root-check.result == 'success' &&\n      github.ref_name == github.event.repository.default_branch\n")
		assert.Empty(t, checkDetection(j, aw.jobs, formatted))
	})

	const (
		detJob  = "  apply-infra-code-gate:\n    runs-on: ubuntu-24.04\n"
		detStep = "      - run: make infra-code-gate RUNNER_MODE=host\n"
	)
	hidden := map[string]struct{ from, to, want string }{
		"検出の job の defaults: は違反":       {detJob, "  apply-infra-code-gate:\n    defaults: {}\n    runs-on: ubuntu-24.04\n", "defaults:"},
		"検出の job の services: は違反":       {detJob, "  apply-infra-code-gate:\n    services: {}\n    runs-on: ubuntu-24.04\n", "services:"},
		"検出の job の MFLAGS は違反":          {detStep, detStep + "        env:\n          MFLAGS: -n\n", "MAKEFLAGS と MAKEFILES を渡さない"},
		"検出の job の似た名前のターゲットは違反":        {detStep, detStep + "      - run: make infra-code-gate-testx RUNNER_MODE=host\n", "run: は make infra-code-gate だけ"},
		"検出の job の env: は違反":            {detJob, "  apply-infra-code-gate:\n    env:\n      X: y\n    runs-on: ubuntu-24.04\n", "env:"},
		"検出の job の container: は違反":      {detJob, "  apply-infra-code-gate:\n    container: alpine\n    runs-on: ubuntu-24.04\n", "container:"},
		"検出の job の MAKEFLAGS は違反":       {detJob, "  apply-infra-code-gate:\n    env:\n      MAKEFLAGS: -n\n    runs-on: ubuntu-24.04\n", "MAKEFLAGS と MAKEFILES を渡さない"},
		"検出の step の shell: は違反":         {detStep, detStep + "        shell: sh -c true {0}\n", "shell:"},
		"検出の step の引用符付き env: は違反":      {detStep, detStep + "        \"env\":\n          X: y\n", "env:"},
		"検出の job の MAKEFILES は違反":       {detStep, detStep + "        env:\n          MAKEFILES: x.mk\n", "MAKEFLAGS と MAKEFILES を渡さない"},
		"検出の job のほかの run: は違反":         {detStep, "      - run: echo /tmp/b >> \"$GITHUB_PATH\"\n" + detStep, "run: は make infra-code-gate だけ"},
		"検出の job から GITHUB_PATH に書けば違反": {detStep, "      - run: echo /tmp/b >> \"$GITHUB_PATH\"\n" + detStep, "GITHUB_PATH と GITHUB_ENV"},
	}
	for name, c := range hidden {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			j, aw := applyWith(t, c.from, c.to)
			assert.Contains(t, messages(checkDetection(j, aw.jobs, formatted)), c.want)
		})
	}

	t.Run("needs に検出でも検査の job でもない job があれば違反", func(t *testing.T) {
		t.Parallel()

		j, aw := applyWith(t, "  apply-admin-identity:\n    needs: [apply-infra-code-gate, apply-resource-order-check, apply-layer-check, apply-root-check]\n",
			"  other:\n    runs-on: ubuntu-24.04\n    steps:\n      - run: echo\n\n  apply-admin-identity:\n    needs: [apply-infra-code-gate, apply-resource-order-check, apply-layer-check, apply-root-check, other]\n")
		assert.Contains(t, messages(checkDetection(j, aw.jobs, formatted)), "needs の other は、検出の job（make infra-code-gate）・順序の検査の job（make resource-order-check）・層の検査の job（make layer-check）・root の検査の job（make root-check）・workload root の apply の job に限り同じ env の identity root の apply の job（make ci-apply-<env>-identity） ではありません")
	})

	t.Run("if: が順序の検査の成功を見なければ違反", func(t *testing.T) {
		t.Parallel()

		j, aw := applyWith(t, "needs.apply-resource-order-check.result == 'success' && ", "")
		assert.Contains(t, messages(checkDetection(j, aw.jobs, formatted)), "needs.apply-resource-order-check.result == 'success' を含めて")
	})

	t.Run("順序の検査の job も検出の job と同じ規則で見る", func(t *testing.T) {
		t.Parallel()

		j, aw := applyWith(t, "  apply-resource-order-check:\n    runs-on: ubuntu-24.04\n", "  apply-resource-order-check:\n    continue-on-error: true\n    runs-on: ubuntu-24.04\n")
		assert.Contains(t, messages(checkDetection(j, aw.jobs, formatted)), "順序の検査の job apply-resource-order-check に if:・continue-on-error:")
	})

	t.Run("if: が層の検査の成功を見なければ違反", func(t *testing.T) {
		t.Parallel()

		j, aw := applyWith(t, "needs.apply-layer-check.result == 'success' && ", "")
		assert.Contains(t, messages(checkDetection(j, aw.jobs, formatted)), "needs.apply-layer-check.result == 'success' を含めて")
	})

	t.Run("if: が root の検査の成功を見なければ違反", func(t *testing.T) {
		t.Parallel()

		j, aw := applyWith(t, "needs.apply-root-check.result == 'success' && ", "")
		assert.Contains(t, messages(checkDetection(j, aw.jobs, formatted)), "needs.apply-root-check.result == 'success' を含めて")
	})

	t.Run("層の検査の job も検出の job と同じ規則で見る", func(t *testing.T) {
		t.Parallel()

		j, aw := applyWith(t, "  apply-layer-check:\n    runs-on: ubuntu-24.04\n", "  apply-layer-check:\n    continue-on-error: true\n    runs-on: ubuntu-24.04\n")
		assert.Contains(t, messages(checkDetection(j, aw.jobs, formatted)), "層の検査の job apply-layer-check に if:・continue-on-error:")
	})

	t.Run("2つの検査を1つの job で走らせれば違反", func(t *testing.T) {
		t.Parallel()

		const orderStep = "      - run: make resource-order-check RUNNER_MODE=host\n"
		j, aw := applyWith(t, orderStep, orderStep+"      - run: make layer-check RUNNER_MODE=host\n")
		assert.Contains(t, messages(checkDetection(j, aw.jobs, formatted)), "順序の検査の job apply-resource-order-check の run: は make resource-order-check だけ")
	})

	t.Run("検出の job のコメントの if: は読まない", func(t *testing.T) {
		t.Parallel()

		j, aw := applyWith(t, "      - run: make infra-code-gate RUNNER_MODE=host\n", "      - run: make infra-code-gate RUNNER_MODE=host\n        # if: false\n")
		assert.Empty(t, checkDetection(j, aw.jobs, formatted))
	})

	t.Run("検出の step の working-directory: は違反", func(t *testing.T) {
		t.Parallel()

		j, aw := applyWith(t, "      - run: make infra-code-gate RUNNER_MODE=host\n", "      - run: make infra-code-gate RUNNER_MODE=host\n        working-directory: /tmp\n")
		assert.Contains(t, messages(checkDetection(j, aw.jobs, formatted)), "working-directory:")
	})

	const identitySuccess = " && needs.apply-admin-identity.result == 'success'"
	waiting := func(t *testing.T, edits ...string) (workflow.Job, workflowInfo) {
		t.Helper()

		src := replaced(t, applyWorkflow, "  revert-plan:\n", fullWorkloadApplyJob+"  revert-plan:\n")
		for k := 0; k < len(edits); k += 2 {
			src = replaced(t, src, edits[k], edits[k+1])
		}

		return jobOf(t, src, "apply-admin-workload"), infoOf(src)
	}

	t.Run("workload root の apply が同じ env の identity root の apply の成功を待てば違反0件", func(t *testing.T) {
		t.Parallel()

		j, aw := waiting(t)
		assert.Empty(t, checkDetection(j, aw.jobs, formatted))
	})

	t.Run("identity root の apply を待つのに if: がその成功を見なければ違反", func(t *testing.T) {
		t.Parallel()

		j, aw := waiting(t, identitySuccess, "")
		assert.Contains(t, messages(checkDetection(j, aw.jobs, formatted)), "needs.apply-admin-identity.result == 'success' を含めて")
	})

	refused := func(id string) string {
		return "needs の " + id + " は、" + allowedNeeds() + " ではありません。資格情報を取る job が待つ job は、これらの形に限ります\n"
	}
	// 待てない形は、needs の違反をちょうど1件だけ出す。待てる形と取り違えると、if: の欠落の違反も重なる。
	upstreamRefused := map[string][]string{
		"別の env の identity root の apply は待てない":                 {"      - run: make ci-apply-admin-identity RUNNER_MODE=host\n", "      - run: make ci-apply-stg-identity RUNNER_MODE=host\n"},
		"identity root の plan の job は待てない":                     {"      - run: make ci-apply-admin-identity RUNNER_MODE=host\n", "      - run: make ci-plan-admin-identity RUNNER_MODE=host\n"},
		"identity root の lock の解除の job は待てない":                  {"      - run: make ci-apply-admin-identity RUNNER_MODE=host\n", "      - run: make ci-apply-admin-identity RUNNER_MODE=host\n" + "      - run: make ci-unlock-admin-identity RUNNER_MODE=host\n"},
		"identity root の lock を残す job は待てない":                   {"      - run: make ci-apply-admin-identity RUNNER_MODE=host\n", "      - run: make ci-apply-admin-identity RUNNER_MODE=host\n" + "      - run: make ci-hold-admin-identity RUNNER_MODE=host\n"},
		"入口を run: の値として持たない job は待てない":                         {"      - run: make ci-apply-admin-identity RUNNER_MODE=host\n", "      - run: echo make ci-apply-admin-identity\n"},
		"資格情報を取らない job は待てない":                                  {"      - run: make ci-aws-credentials CI_ENV=admin CI_ROLE=apply-identity RUNNER_MODE=host\n", ""},
		"lock を残す job は apply の job を待てない":                     {"      - run: make ci-apply-admin-workload RUNNER_MODE=host\n", "      - run: make ci-apply-admin-workload RUNNER_MODE=host\n" + "      - run: make ci-hold-admin-workload RUNNER_MODE=host\n"},
		"lock の解除の job は apply の job を待てない":                    {"      - run: make ci-apply-admin-workload RUNNER_MODE=host\n", "      - run: make ci-apply-admin-workload RUNNER_MODE=host\n" + "      - run: make ci-unlock-admin-workload RUNNER_MODE=host\n"},
		"plan の job は apply の job を待てない":                       {"      - run: make ci-apply-admin-workload RUNNER_MODE=host\n", "      - run: make ci-plan-admin-workload RUNNER_MODE=host\n"},
		"identity root の apply を待つのは workload root の apply だけ": {"      - run: make ci-apply-admin-workload RUNNER_MODE=host\n", "      - run: make ci-apply-stg-identity RUNNER_MODE=host\n"},
	}
	for name, edit := range upstreamRefused {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			j, aw := waiting(t, edit...)
			assert.Equal(t, refused("apply-admin-identity"), messages(checkDetection(j, aw.jobs, formatted)))
		})
	}

	t.Run("identity root の apply は workload root の apply を待てない", func(t *testing.T) {
		t.Parallel()

		src := replaced(t, applyWorkflow, "  revert-plan:\n", fullWorkloadApplyJob+"  revert-plan:\n")
		src = replaced(t, src, "    needs: [apply-infra-code-gate, apply-resource-order-check, apply-layer-check, apply-root-check]\n    if: ${{ needs.apply-infra-code-gate.result == 'success' && needs.apply-resource-order-check.result == 'success' && needs.apply-layer-check.result == 'success' && needs.apply-root-check.result == 'success' && github",
			"    needs: [apply-infra-code-gate, apply-resource-order-check, apply-layer-check, apply-root-check, apply-admin-workload]\n    if: ${{ needs.apply-infra-code-gate.result == 'success' && needs.apply-resource-order-check.result == 'success' && needs.apply-layer-check.result == 'success' && needs.apply-root-check.result == 'success' && needs.apply-admin-workload.result == 'success' && github")
		assert.Equal(t, refused("apply-admin-workload"), messages(checkDetection(jobOf(t, src, "apply-admin-identity"), infoOf(src).jobs, formatted)))
	})

	t.Run("identity root の apply を待っても検出を欠けば違反", func(t *testing.T) {
		t.Parallel()

		j, aw := waiting(t, "    needs: [apply-infra-code-gate, apply-resource-order-check, apply-layer-check, apply-root-check, apply-admin-identity]\n    if: ${{ needs.apply-infra-code-gate.result == 'success' && ",
			"    needs: [apply-resource-order-check, apply-layer-check, apply-root-check, apply-admin-identity]\n    if: ${{ ")
		f := checkDetection(j, aw.jobs, formatted)
		assert.Len(t, f, 1, messages(f))
		assert.Contains(t, messages(f), "make infra-code-gate を走らせる job")
	})
}

var fullWorkloadApplyJob = strings.Join([]string{
	"  apply-admin-workload:",
	"    needs: [apply-infra-code-gate, apply-resource-order-check, apply-layer-check, apply-root-check, apply-admin-identity]",
	"    if: ${{ needs.apply-infra-code-gate.result == 'success' && needs.apply-resource-order-check.result == 'success' && needs.apply-layer-check.result == 'success' && needs.apply-root-check.result == 'success' && needs.apply-admin-identity.result == 'success' && github.ref_name == github.event.repository.default_branch }}",
	"    runs-on: ubuntu-24.04",
	"    environment:",
	"      name: admin-apply",
	"    permissions:",
	"      contents: read",
	"      id-token: write",
	"    steps:",
	"      - name: Refuse stale commits",
	"        id: refuse-stale",
	"        run: head=$(git ls-remote \"https://github.com/$REPO.git\" \"refs/heads/$REF\") && test \"$SHA\" = \"$head\" || exit 1",
	"      - uses: actions/checkout@0000000000000000000000000000000000000000",
	"        with:",
	"          persist-credentials: false",
	"      - run: make ci-aws-credentials CI_ENV=admin CI_ROLE=apply RUNNER_MODE=host",
	"      - run: make ci-apply-admin-workload RUNNER_MODE=host",
	"",
	"",
}, "\n")

func Test_upstreamApply(t *testing.T) {
	t.Parallel()

	src := replaced(t, applyWorkflow, "  revert-plan:\n", fullWorkloadApplyJob+"  revert-plan:\n")
	identity, workload := jobOf(t, src, "apply-admin-identity"), jobOf(t, src, "apply-admin-workload")

	t.Run("workload root の apply から同じ env の identity root の apply は true", func(t *testing.T) {
		t.Parallel()

		assert.True(t, upstreamApply(workload, identity))
	})

	t.Run("向きが逆なら false", func(t *testing.T) {
		t.Parallel()

		assert.False(t, upstreamApply(identity, workload))
	})

	t.Run("入口の無い job は false", func(t *testing.T) {
		t.Parallel()

		assert.False(t, upstreamApply(jobOf(t, src, "apply-infra-code-gate"), identity))
	})

	t.Run("env の無い workload の入口は false", func(t *testing.T) {
		t.Parallel()

		j := jobOf(t, replaced(t, src, "make ci-apply-admin-workload", "make ci-apply-workload"), "apply-admin-workload")
		assert.False(t, upstreamApply(j, identity))
	})

	t.Run("env にハイフンを含んでも同じ env の identity root の apply は true", func(t *testing.T) {
		t.Parallel()

		la := replaced(t, replaced(t, src, "make ci-apply-admin-workload", "make ci-apply-log-archive-workload"), "make ci-apply-admin-identity", "make ci-apply-log-archive-identity")
		assert.True(t, upstreamApply(jobOf(t, la, "apply-admin-workload"), jobOf(t, la, "apply-admin-identity")))
	})

	t.Run("env の末尾だけが一致する identity root の apply は false", func(t *testing.T) {
		t.Parallel()

		la := replaced(t, replaced(t, src, "make ci-apply-admin-workload", "make ci-apply-log-archive-workload"), "make ci-apply-admin-identity", "make ci-apply-archive-identity")
		assert.False(t, upstreamApply(jobOf(t, la, "apply-admin-workload"), jobOf(t, la, "apply-admin-identity")))
	})

	t.Run("入口の無い dep は false", func(t *testing.T) {
		t.Parallel()

		assert.False(t, upstreamApply(workload, jobOf(t, src, "apply-infra-code-gate")))
	})

	t.Run("apply の入口を持っていても lock を残す job なら false", func(t *testing.T) {
		t.Parallel()

		h := replaced(t, src, "      - run: make ci-apply-admin-workload RUNNER_MODE=host\n", "      - run: make ci-apply-admin-workload RUNNER_MODE=host\n      - run: make ci-hold-admin-workload RUNNER_MODE=host\n")
		assert.False(t, upstreamApply(jobOf(t, h, "apply-admin-workload"), identity))
	})
}

func Test_checkUpstream(t *testing.T) {
	t.Parallel()

	src := replaced(t, applyWorkflow, "  revert-plan:\n", fullWorkloadApplyJob+"  revert-plan:\n")

	t.Run("同じ env の identity root の apply を needs に持てば違反0件", func(t *testing.T) {
		t.Parallel()

		assert.Empty(t, checkUpstream(jobOf(t, src, "apply-admin-workload"), infoOf(src).jobs, formatted))
	})

	t.Run("同じ workflow の同じ env の identity root の apply を needs に持たなければ違反", func(t *testing.T) {
		t.Parallel()

		m := replaced(t, src, "apply-root-check, apply-admin-identity]\n    if: ${{ needs.apply-infra-code-gate.result == 'success' && needs.apply-resource-order-check.result == 'success' && needs.apply-layer-check.result == 'success' && needs.apply-root-check.result == 'success' && needs.apply-admin-identity.result == 'success'",
			"apply-root-check]\n    if: ${{ needs.apply-infra-code-gate.result == 'success' && needs.apply-resource-order-check.result == 'success' && needs.apply-layer-check.result == 'success' && needs.apply-root-check.result == 'success'")
		assert.Equal(t, "workload root の apply の job の needs に、同じ env の identity root の apply の job apply-admin-identity を置いてください。identity root の apply が書く権限を使う前に走ります\n",
			messages(checkUpstream(jobOf(t, m, "apply-admin-workload"), infoOf(m).jobs, formatted)))
	})

	t.Run("同じ env の identity root の apply が無い workflow では求めない", func(t *testing.T) {
		t.Parallel()

		m := replaced(t, src, "make ci-apply-admin-identity", "make ci-apply-stg-identity")
		assert.Empty(t, checkUpstream(jobOf(t, m, "apply-admin-workload"), infoOf(m).jobs, formatted))
	})

	t.Run("identity root の apply の job には求めない", func(t *testing.T) {
		t.Parallel()

		assert.Empty(t, checkUpstream(jobOf(t, src, "apply-admin-identity"), infoOf(src).jobs, formatted))
	})
}

func Test_allowedNeeds(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "検出の job（make infra-code-gate）・順序の検査の job（make resource-order-check）・層の検査の job（make layer-check）・root の検査の job（make root-check）・workload root の apply の job に限り同じ env の identity root の apply の job（make ci-apply-<env>-identity）", allowedNeeds())
}

func Test_checkGateJob(t *testing.T) {
	t.Parallel()

	const orderStep = "      - run: make resource-order-check RUNNER_MODE=host\n"
	orderJob := func(t *testing.T, from, to string) workflow.Job {
		t.Helper()

		return jobOf(t, replaced(t, applyWorkflow, from, to), "apply-resource-order-check")
	}

	t.Run("順序の検査の2つの make だけなら違反0件", func(t *testing.T) {
		t.Parallel()

		assert.Empty(t, checkGateJob(jobOf(t, applyWorkflow, "apply-resource-order-check"), orderGate, formatted))
	})

	// pair は egress が生成する bullfrog の三つ組。with: は形の検査に関わらないので省く。
	pair := strings.Join(harden.Render("        uses: bullfrogsec/bullfrog@x", nil), "\n") + "\n"
	retryIf := "        id: " + harden.RetryID + "\n        if: " + harden.RetryIf
	t.Run("生成した bullfrog の三つ組の continue-on-error:・if:・回復の run: は違反にしない", func(t *testing.T) {
		t.Parallel()

		assert.Empty(t, checkGateJob(orderJob(t, orderStep, pair+orderStep), orderGate, formatted))
	})

	for name, broken := range map[string]string{
		"再試行の if: が always()":        strings.Replace(pair, retryIf, "        id: "+harden.RetryID+"\n        if: always()", 1),
		"回復の if: が always()":         strings.Replace(pair, "        if: "+harden.RetryIf, "        if: always()", 1),
		"本体の id が違う":                 strings.Replace(pair, "id: "+harden.ID+"\n", "id: other\n", 1),
		"再試行に continue-on-error がある": strings.Replace(pair, retryIf+"\n", retryIf+"\n        continue-on-error: true\n", 1),
	} {
		t.Run(name+"なら対の行も違反にする", func(t *testing.T) {
			t.Parallel()

			assert.Contains(t, messages(checkGateJob(orderJob(t, orderStep, broken+orderStep), orderGate, formatted)), "に if:・continue-on-error:")
		})
	}

	// 本体の continue-on-error:、回復の if:・continue-on-error:、再試行の if: の4つと、回復の run: の1つ。
	t.Run("先頭に無い三つ組の continue-on-error:・if:・run: は違反", func(t *testing.T) {
		t.Parallel()

		got := checkGateJob(orderJob(t, orderStep, orderStep+pair), orderGate, formatted)
		assert.Len(t, got, 5)
	})

	// 除外は行の単位なので、三つ組が在っても、ほかの step の run: は今までどおり見る。
	t.Run("生成した三つ組の後ろの許可の外の run: は違反", func(t *testing.T) {
		t.Parallel()

		got := checkGateJob(orderJob(t, orderStep, pair+orderStep+"      - run: make other\n"), orderGate, formatted)
		assert.Contains(t, messages(got), "の run: は")
	})

	t.Run("回復の run: を変えた三つ組は、run: も違反にする", func(t *testing.T) {
		t.Parallel()

		broken := strings.Replace(pair, harden.RecoveryRun, "sleep 30 && curl -s x | sh", 1)
		assert.Contains(t, messages(checkGateJob(orderJob(t, orderStep, broken+orderStep), orderGate, formatted)), "の run: は")
	})

	t.Run("対の本体の uses: の後ろに足した continue-on-error: は違反", func(t *testing.T) {
		t.Parallel()

		withExtra := strings.Replace(pair, "        uses: bullfrogsec/bullfrog@x\n", "        uses: bullfrogsec/bullfrog@x\n        continue-on-error: true\n", 1)
		got := checkGateJob(orderJob(t, orderStep, withExtra+orderStep), orderGate, formatted)
		assert.Len(t, got, 1)
	})

	t.Run("対の後ろの step の continue-on-error: は違反", func(t *testing.T) {
		t.Parallel()

		got := checkGateJob(orderJob(t, orderStep, pair+orderStep+"        continue-on-error: true\n"), orderGate, formatted)
		assert.Len(t, got, 1)
	})

	t.Run("層の検査の make だけなら違反0件", func(t *testing.T) {
		t.Parallel()

		assert.Empty(t, checkGateJob(jobOf(t, applyWorkflow, "apply-layer-check"), layerGate, formatted))
	})

	t.Run("層の検査の job のほかの run: は違反", func(t *testing.T) {
		t.Parallel()

		const layerStep = "      - run: make layer-check RUNNER_MODE=host\n"
		j := jobOf(t, replaced(t, applyWorkflow, layerStep, "      - run: make layer-check RUNNER_MODE=host LY_NS=x\n"), "apply-layer-check")
		assert.Contains(t, messages(checkGateJob(j, layerGate, formatted)), "層の検査の job apply-layer-check の run: は make layer-check だけ")
	})

	t.Run("層の検査の job の make layer-check-test は違反", func(t *testing.T) {
		t.Parallel()

		const layerStep = "      - run: make layer-check RUNNER_MODE=host\n"
		j := jobOf(t, replaced(t, applyWorkflow, layerStep, "      - run: make layer-check-test RUNNER_MODE=host\n"+layerStep), "apply-layer-check")
		assert.Contains(t, messages(checkGateJob(j, layerGate, formatted)), "層の検査の job apply-layer-check の run: は make layer-check だけ")
	})

	t.Run("root の検査の2つの make だけなら違反0件", func(t *testing.T) {
		t.Parallel()

		assert.Empty(t, checkGateJob(jobOf(t, applyWorkflow, "apply-root-check"), rootGate, formatted))
	})

	t.Run("root の検査の job のほかの run: は違反", func(t *testing.T) {
		t.Parallel()

		const rootStep = "      - run: make root-check RUNNER_MODE=host\n"
		j := jobOf(t, replaced(t, applyWorkflow, rootStep, "      - run: make root-check RUNNER_MODE=host RT_NS=x\n"), "apply-root-check")
		assert.Contains(t, messages(checkGateJob(j, rootGate, formatted)), "root の検査の job apply-root-check の run: は make root-check だけ")
	})

	for name, c := range map[string]struct{ from, to, want string }{
		"ほかの run: は違反":         {orderStep, orderStep + "      - run: echo\n", "順序の検査の job apply-resource-order-check の run: は make resource-order-check だけ"},
		"変数を足した make は違反":      {orderStep, "      - run: make resource-order-check RUNNER_MODE=host RO_NS=x\n", "run: は make resource-order-check だけ"},
		"検出の make は違反":         {orderStep, orderStep + "      - run: make infra-code-gate RUNNER_MODE=host\n", "run: は make resource-order-check だけ"},
		"step の if: は違反":       {orderStep, orderStep + "        if: false\n", "順序の検査の job apply-resource-order-check に if:"},
		"job の needs: は違反":     {"  apply-resource-order-check:\n    runs-on: ubuntu-24.04\n", "  apply-resource-order-check:\n    needs: changes\n    runs-on: ubuntu-24.04\n", "順序の検査の job apply-resource-order-check に needs:"},
		"MAKEFLAGS は違反":        {orderStep, orderStep + "        env:\n          MAKEFLAGS: -n\n", "MAKEFLAGS と MAKEFILES を渡さない"},
		"GITHUB_ENV への書き込みは違反": {orderStep, "      - run: echo X=y >> \"$GITHUB_ENV\"\n" + orderStep, "GITHUB_PATH と GITHUB_ENV"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Contains(t, messages(checkGateJob(orderJob(t, c.from, c.to), orderGate, formatted)), c.want)
		})
	}
}

func Test_checkApplyGate(t *testing.T) {
	t.Parallel()

	applyWith := func(t *testing.T, from, to string) (workflow.Job, workflowInfo) {
		t.Helper()

		src := replaced(t, applyWorkflow, from, to)

		return jobOf(t, src, "apply-admin-identity"), infoOf(src)
	}
	const fullNeeds = "    needs: [apply-infra-code-gate, apply-resource-order-check, apply-layer-check, apply-root-check]\n"

	for _, g := range applyGates {
		want := fmt.Sprintf("apply の job の needs に、%s を走らせる job を同じ workflow の中に置いてください", g.check)

		t.Run(g.noun+"の job を needs に持てば違反0件", func(t *testing.T) {
			t.Parallel()

			assert.Empty(t, checkApplyGate(jobOf(t, applyWorkflow, "apply-admin-identity"), infoOf(applyWorkflow).jobs, g, formatted))
		})

		t.Run(g.noun+": needs に無ければ違反", func(t *testing.T) {
			t.Parallel()

			j, aw := applyWith(t, fullNeeds, "    needs: apply-infra-code-gate\n")
			assert.Contains(t, messages(checkApplyGate(j, aw.jobs, g, formatted)), want)
		})

		t.Run(g.noun+": apply の job の検査に含まれる", func(t *testing.T) {
			t.Parallel()

			j, aw := applyWith(t, fullNeeds, "    needs: apply-infra-code-gate\n")
			assert.Contains(t, messages(checkJob("a.yaml", j, planPerms, envs, aw)), want)
		})
	}

	for name, c := range map[string]struct {
		g        gate
		from, to string
	}{
		"順序の検査: 検査の make を持たない job は数えない":     {orderGate, "      - run: make resource-order-check RUNNER_MODE=host\n", ""},
		"順序の検査: 検出も走らせる job は数えない":            {orderGate, "      - run: make resource-order-check RUNNER_MODE=host\n", "      - run: make infra-code-gate RUNNER_MODE=host\n      - run: make resource-order-check RUNNER_MODE=host\n"},
		"層の検査: 層の検査を走らせない job は数えない":          {layerGate, "      - run: make layer-check RUNNER_MODE=host\n", ""},
		"層の検査: 検出も走らせる job は数えない":             {layerGate, "      - run: make layer-check RUNNER_MODE=host\n", "      - run: make infra-code-gate RUNNER_MODE=host\n      - run: make layer-check RUNNER_MODE=host\n"},
		"層の検査: 順序の検査の job は層の検査と数えない":         {layerGate, "    needs: [apply-infra-code-gate, apply-resource-order-check, apply-layer-check, apply-root-check]\n", "    needs: [apply-infra-code-gate, apply-resource-order-check]\n"},
		"root の検査: 検査の make を持たない job は数えない":  {rootGate, "      - run: make root-check RUNNER_MODE=host\n", ""},
		"root の検査: 検出も走らせる job は数えない":         {rootGate, "      - run: make root-check RUNNER_MODE=host\n", "      - run: make infra-code-gate RUNNER_MODE=host\n      - run: make root-check RUNNER_MODE=host\n"},
		"root の検査: 層の検査の job は root の検査と数えない": {rootGate, "    needs: [apply-infra-code-gate, apply-resource-order-check, apply-layer-check, apply-root-check]\n", "    needs: [apply-infra-code-gate, apply-resource-order-check, apply-layer-check]\n"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			j, aw := applyWith(t, c.from, c.to)
			assert.Contains(t, messages(checkApplyGate(j, aw.jobs, c.g, formatted)), c.g.check+" を走らせる job")
		})
	}
}

func hiddenOf(t *testing.T, job workflow.Job, w workflowInfo, f func(int, string, ...any) lintreport.Finding) []lintreport.Finding {
	t.Helper()

	refusal := refuseStale
	if strings.Contains(job.ID, "plan") {
		refusal = refuseUntrusted
	}

	return checkHidden(job, credOf(job), refusal, w, f)
}

func Test_checkHidden(t *testing.T) {
	t.Parallel()

	const (
		entry = "      - run: make ci-apply-admin-identity RUNNER_MODE=host\n"
		cred  = "      - run: make ci-aws-credentials CI_ENV=admin CI_ROLE=apply-identity RUNNER_MODE=host\n"
	)
	applyWith := func(t *testing.T, from, to string) (workflow.Job, workflowInfo) {
		t.Helper()

		src := replaced(t, applyWorkflow, from, to)

		return jobOf(t, src, "apply-admin-identity"), infoOf(src)
	}

	t.Run("規則を満たせば違反0件", func(t *testing.T) {
		t.Parallel()

		assert.Empty(t, hiddenOf(t, jobOf(t, applyWorkflow, "apply-admin-identity"), infoOf(applyWorkflow), formatted))
	})

	t.Run("plan の workflow も規則を満たせば違反0件", func(t *testing.T) {
		t.Parallel()

		assert.Empty(t, hiddenOf(t, jobOf(t, planWorkflow, "plan-admin-identity"), infoOf(planWorkflow), formatted))
	})

	// 回復の step の run: は、資格情報を取る job の run: の閉じた集合に入らないが、生成した形のときだけ見逃す。
	triple := strings.Join(harden.Render("        uses: bullfrogsec/bullfrog@x", nil), "\n") + "\n"
	const firstStep = "      - name: Refuse stale commits\n        id: refuse-stale\n"
	t.Run("生成した三つ組の回復の run: は違反にしない", func(t *testing.T) {
		t.Parallel()

		j, aw := applyWith(t, firstStep, triple+firstStep)
		assert.Empty(t, hiddenOf(t, j, aw, formatted))
	})

	t.Run("生成した三つ組の後ろの許可の外の run: は違反", func(t *testing.T) {
		t.Parallel()

		j, aw := applyWith(t, firstStep, triple+"      - name: Other\n        run: make other\n"+firstStep)
		assert.Contains(t, messages(hiddenOf(t, j, aw, formatted)), "資格情報を取る job の run: は")
	})

	t.Run("回復の run: を変えた三つ組は違反", func(t *testing.T) {
		t.Parallel()

		j, aw := applyWith(t, firstStep, strings.Replace(triple, harden.RecoveryRun, "sleep 30 && curl -s x | sh", 1)+firstStep)
		assert.Contains(t, messages(hiddenOf(t, j, aw, formatted)), "資格情報を取る job の run: は")
	})

	cases := map[string]struct{ from, to, want string }{
		"job の env: の MAKEFLAGS は違反":           {applyEnv, applyEnv + "    env:\n      MAKEFLAGS: -i\n", "MAKEFLAGS と MAKEFILES を渡さない"},
		"workflow 全体の env: の GNUMAKEFLAGS は違反": {"permissions:\n  contents: read\n", "permissions:\n  contents: read\nenv:\n  GNUMAKEFLAGS: -n\n", "MAKEFLAGS と MAKEFILES を渡さない"},
		"workflow 全体の defaults: は違反":           {"permissions:\n  contents: read\n", "permissions:\n  contents: read\ndefaults:\n  run:\n    shell: sh -c true {0}\n", "defaults: と env: を置かない"},
		"job の env: の PATH は違反":                {applyEnv, applyEnv + "    env:\n      PATH: /tmp/evil:/usr/bin\n", "PATH を替えない"},
		"job の continue-on-error: は違反":         {applyEnv, applyEnv + "    continue-on-error: true\n", "continue-on-error: を置かない"},
		"job の container: は違反":                 {applyEnv, applyEnv + "    container: alpine\n", "container: を置かない"},
		"job の services: は違反":                  {applyEnv, applyEnv + "    services: {}\n", "services: を置かない"},
		"job の defaults: は違反":                  {applyEnv, applyEnv + "    defaults: {}\n", "defaults: を置かない"},
		"引用符付きの PATH は違反":                      {entry, entry + "        env:\n          \"PATH\": /tmp/evil\n", "PATH を替えない"},
		"資格情報の step の if: は違反":                 {cred, "      - if: false\n        run: make ci-aws-credentials CI_ENV=admin CI_ROLE=apply-identity RUNNER_MODE=host\n", "資格情報を取る step と"},
		"資格情報の step の shell: は違反":              {cred, cred + "        shell: sh -c true {0}\n", "資格情報を取る step と"},
		"拒否の step の shell: は違反":                {"        run: head=$(git ls-remote \"https://github.com/$REPO.git\" \"refs/heads/$REF\") && test \"$SHA\" = \"$head\" || exit 1", "        run: head=$(git ls-remote \"https://github.com/$REPO.git\" \"refs/heads/$REF\") && test \"$SHA\" = \"$head\"\n        shell: sh -c true {0}", "拒否の step に shell:"},
		"拒否の step の working-directory: は違反":    {"        run: head=$(git ls-remote \"https://github.com/$REPO.git\" \"refs/heads/$REF\") && test \"$SHA\" = \"$head\" || exit 1", "        run: head=$(git ls-remote \"https://github.com/$REPO.git\" \"refs/heads/$REF\") && test \"$SHA\" = \"$head\"\n        working-directory: /tmp", "拒否の step に shell:"},
		"入口の step の continue-on-error: は違反":    {entry, entry + "        continue-on-error: true\n", "入口の step"},
		"入口の step の if: は違反":                   {entry, "      - if: false\n        run: make ci-apply-admin-identity RUNNER_MODE=host\n", "入口の step"},
		"入口の step の shell: は違反":                {entry, entry + "        shell: sh -c true {0}\n", "入口の step"},
		"入口の step の working-directory: は違反":    {entry, entry + "        working-directory: usecases\n", "入口の step"},
		"資格情報の step の working-directory: は違反":  {cred, cred + "        working-directory: usecases\n", "資格情報を取る step と"},
		"workflow 全体の env: の MAKEFILES は違反":    {"permissions:\n  contents: read\n", "permissions:\n  contents: read\nenv:\n  MAKEFILES: .github/x.mk\n", "MAKEFLAGS と MAKEFILES を渡さない"},
		"workflow 全体の env: は違反":                {"permissions:\n  contents: read\n", "permissions:\n  contents: read\nenv:\n  X: y\n", "defaults: と env: を置かない"},
		"job の env: は違反":                       {applyEnv, applyEnv + "    env:\n      X: y\n", "env: を置かない"},
		"入口の step のフロー形式の env: の PATH は違反":     {entry, entry + "        env: {PATH: /tmp/evil}\n", "PATH を替えない"},
		"入口の step の env: の TF_BIN は違反":         {entry, entry + "        env:\n          TF_BIN: /bin/true\n", "env: に置けるのは"},
		"資格情報の step の env: の BASH_ENV は違反":     {cred, cred + "        env:\n          BASH_ENV: ./x.sh\n", "env: に置けるのは"},
		"拒否の step から GITHUB_PATH に書けば違反":       {"head=$(git ls-remote \"https://github.com/$REPO.git\" \"refs/heads/$REF\") && test \"$SHA\" = \"$head\" || exit 1", "echo /tmp/b >> \"$GITHUB_PATH\"", "GITHUB_PATH と GITHUB_ENV"},
		"拒否の step から GITHUB_ENV に書けば違反":        {"head=$(git ls-remote \"https://github.com/$REPO.git\" \"refs/heads/$REF\") && test \"$SHA\" = \"$head\" || exit 1", "echo X=1 >> \"$GITHUB_ENV\"", "GITHUB_PATH と GITHUB_ENV"},
		"ほかの run: の step は違反":                  {cred, "      - run: echo hi\n" + cred, "run: は、資格情報より前の拒否の step"},
		"資格情報の後の拒否 id の step の run: は違反":       {entry, "      - id: refuse-stale\n        run: cat ~/.aws/credentials\n" + entry, "run: は、資格情報より前の拒否の step"},
		"役割の違う拒否 id の step の run: は違反":         {cred, "      - id: refuse-untrusted\n        run: echo hi\n" + cred, "run: は、資格情報より前の拒否の step"},
		"エイリアスで持ち込む env: は違反":                  {entry, entry + "        env: *m\n", "アンカーとエイリアス"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			j, aw := applyWith(t, c.from, c.to)
			assert.Contains(t, messages(hiddenOf(t, j, aw, formatted)), c.want)
		})
	}

	t.Run("許した名前の env: は違反0件", func(t *testing.T) {
		t.Parallel()

		j, aw := applyWith(t, entry, entry+"        env:\n          CI_ACCOUNT_ID: x\n          CI_PR: \"1\"\n")
		assert.Empty(t, hiddenOf(t, j, aw, formatted))
	})

	t.Run("コメントの MAKEFLAGS は読まない", func(t *testing.T) {
		t.Parallel()

		j, aw := applyWith(t, entry, "      # MAKEFLAGS=-n\n"+entry)
		assert.Empty(t, hiddenOf(t, j, aw, formatted))
	})

	t.Run("apply の job の拒否の確認は違反", func(t *testing.T) {
		t.Parallel()

		j, aw := applyWith(t, entry, "      - run: make ci-assume-denied CI_ENV=admin RUNNER_MODE=host\n"+entry)
		assert.Contains(t, messages(hiddenOf(t, j, aw, formatted)), "plan の job にだけ")
	})

	for name, c := range map[string]struct{ to, want string }{
		"拒否の確認の step の if: は違反":                {"      - if: false\n        run: make ci-assume-denied CI_ENV=admin RUNNER_MODE=host\n", "入口の step"},
		"拒否の確認の step の continue-on-error: は違反": {"      - run: make ci-assume-denied CI_ENV=admin RUNNER_MODE=host\n        continue-on-error: true\n", "入口の step"},
		"拒否の確認の step の shell: は違反":             {"      - run: make ci-assume-denied CI_ENV=admin RUNNER_MODE=host\n        shell: sh -c true {0}\n", "入口の step"},
		"拒否の確認の役割を狭める代入は違反":                    {"      - run: make ci-assume-denied CI_ENV=admin CI_DENIED_ROLES=plan RUNNER_MODE=host\n", "run: は、資格情報より前の拒否の step"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			src := replaced(t, planWorkflow, "        run: make ci-assume-denied CI_ENV=admin RUNNER_MODE=host\n", "")
			src = replaced(t, src, "      - run: make ci-plan-admin-identity RUNNER_MODE=host\n", c.to+"      - run: make ci-plan-admin-identity RUNNER_MODE=host\n")
			assert.Contains(t, messages(hiddenOf(t, jobOf(t, src, "plan-admin-identity"), infoOf(src), formatted)), c.want)
		})
	}

	t.Run("plan の入口の step の continue-on-error: も違反", func(t *testing.T) {
		t.Parallel()

		src := replaced(t, planWorkflow, "      - run: make ci-plan-admin-identity RUNNER_MODE=host\n", "      - run: make ci-plan-admin-identity RUNNER_MODE=host\n        continue-on-error: true\n")
		assert.Contains(t, messages(hiddenOf(t, jobOf(t, src, "plan-admin-identity"), infoOf(src), formatted)), "入口の step")
	})
}

func Test_checkRefusal(t *testing.T) {
	t.Parallel()

	planWith := func(t *testing.T, from, to string) workflow.Job {
		t.Helper()

		return jobOf(t, replaced(t, planWorkflow, from, to), "plan-admin-identity")
	}

	t.Run("資格情報の前の拒否の step は違反0件", func(t *testing.T) {
		t.Parallel()

		job := jobOf(t, planWorkflow, "plan-admin-identity")
		assert.Empty(t, checkRefusal(job, credOf(job), refuseUntrusted, formatted))
	})

	t.Run("拒否の step が無ければ違反", func(t *testing.T) {
		t.Parallel()

		job := planWith(t, "id: refuse-untrusted", "id: other")
		assert.Contains(t, messages(checkRefusal(job, credOf(job), refuseUntrusted, formatted)), "資格情報を取る前に")
	})

	t.Run("拒否の step が資格情報の後なら違反", func(t *testing.T) {
		t.Parallel()

		src := strings.Replace(planWorkflow, "      - name: Refuse untrusted Pull Requests\n        id: refuse-untrusted\n        run: test \"$HEAD_REPO_ID\" = \"$REPO_ID\"\n", "", 1)
		src = strings.Replace(src, "      - run: make ci-plan-admin-identity", "      - id: refuse-untrusted\n        run: exit 1\n      - run: make ci-plan-admin-identity", 1)
		job := jobOf(t, src, "plan-admin-identity")
		assert.Contains(t, messages(checkRefusal(job, credOf(job), refuseUntrusted, formatted)), "資格情報を取る前に")
	})

	t.Run("拒否の step が資格情報を取る step 自身なら違反", func(t *testing.T) {
		t.Parallel()

		job := jobOf(t, strings.Replace(strings.Replace(planWorkflow, "        id: refuse-untrusted\n", "        id: other\n", 1), "      - name: Run terraform plan through make\n        run: make ci-aws-credentials", "      - name: Run terraform plan through make\n        id: refuse-untrusted\n        run: make ci-aws-credentials", 1), "plan-admin-identity")
		assert.Contains(t, messages(checkRefusal(job, credOf(job), refuseUntrusted, formatted)), "資格情報を取る前に")
	})

	t.Run("拒否の step の引用符付きの continue-on-error: は違反", func(t *testing.T) {
		t.Parallel()

		job := planWith(t, "        id: refuse-untrusted\n", "        id: refuse-untrusted\n        \"continue-on-error\": true\n")
		assert.Contains(t, messages(checkRefusal(job, credOf(job), refuseUntrusted, formatted)), "continue-on-error:")
	})

	t.Run("拒否の step の if: は違反", func(t *testing.T) {
		t.Parallel()

		job := planWith(t, "        id: refuse-untrusted\n", "        id: refuse-untrusted\n        if: github.event_name == 'pull_request'\n")
		assert.Contains(t, messages(checkRefusal(job, credOf(job), refuseUntrusted, formatted)), "if: と continue-on-error: を置かない")
	})

	t.Run("拒否の step の continue-on-error: は違反", func(t *testing.T) {
		t.Parallel()

		job := planWith(t, "        id: refuse-untrusted\n", "        id: refuse-untrusted\n        continue-on-error: true\n")
		assert.Contains(t, messages(checkRefusal(job, credOf(job), refuseUntrusted, formatted)), "continue-on-error: を置かない")
	})

	t.Run("別の id の拒否の step では満たさない", func(t *testing.T) {
		t.Parallel()

		job := jobOf(t, planWorkflow, "plan-admin-identity")
		assert.Contains(t, messages(checkRefusal(job, credOf(job), refuseStale, formatted)), "id が refuse-stale の step")
	})

	staleRun := "        run: head=$(git ls-remote \"https://github.com/$REPO.git\" \"refs/heads/$REF\") && test \"$SHA\" = \"$head\" || exit 1"
	fetchAnd := func(extra string) string {
		return "        run: |\n          head=$(git ls-remote \"https://github.com/$REPO.git\" \"refs/heads/$REF\")\n          test \"$SHA\" = \"$head\" || exit 1\n          " + extra
	}
	for name, run := range map[string]string{
		"gh api":     fetchAnd(`x=$(gh api "repos/$REPO/git/ref/heads/$REF" --jq .object.sha)`),
		"逆引用符の中の gh": fetchAnd("x=`gh api rate_limit`"),
		"中括弧の後の gh":  fetchAnd("{ gh api rate_limit; }"),
		"既定値の gh":    fetchAnd("${GH:-gh} api rate_limit"),
		"代入の後の gh":   fetchAnd("G=gh"),
		"バックスラッシュを前置した gh": fetchAnd(`\gh api rate_limit`),
		"行を継ぐ gh":              fetchAnd("gh\\"),
		"curl":                 fetchAnd(`curl -fsS "https://github.com/$REPO"`),
		"wget":                 fetchAnd(`wget -qO- "https://github.com/$REPO"`),
		"api.github.com を引く道具": fetchAnd(`python3 -c 'import urllib.request; urllib.request.urlopen("https://api.github.com/x")'`),
		"GITHUB_API_URL を組み立てる道具": fetchAnd(`python3 -c 'import os, urllib.request; urllib.request.urlopen(os.environ["GITHUB_API_URL"])'`),
		"github-script": fetchAnd("node github-script.js"),
	} {
		t.Run("refuse-stale の step で REST を呼ぶ "+name+" は違反", func(t *testing.T) {
			t.Parallel()

			job := jobOf(t, replaced(t, applyWorkflow, staleRun, run), "apply-admin-identity")
			got := checkRefusal(job, credOf(job), refuseStale, formatted)
			assert.Len(t, got, 1)
			assert.Contains(t, messages(got), "REST API を呼ぶ道具を書かないでください")
		})
	}

	for name, run := range map[string]string{
		"先頭を取らない":               `        run: test "$SHA" = "$HEAD"`,
		"比べない":                  `        run: head=$(git ls-remote "https://github.com/$REPO.git" "refs/heads/$REF")`,
		"自分と比べる":                `        run: head=$(git ls-remote "https://github.com/$REPO.git" "refs/heads/$REF") && test "$SHA" = "$SHA"`,
		"ls-remote をコメントに書く":    "        run: |\n          # git ls-remote\n          test \"$SHA\" = \"$head\"",
		"何もしない":                 "        run: \"true\"",
		"失敗の文にだけ ls-remote を書く": "        run: |\n          head=$SHA || { echo \"❌ git ls-remote で決められません\"; exit 1; }\n          test \"$SHA\" = \"$head\" || exit 1",
		"比べた結果を握り潰す":            `        run: head=$(git ls-remote "https://github.com/$REPO.git" "refs/heads/$REF") && test "$SHA" = "$head" || true`,
		"比べる文を echo する":         `        run: head=$(git ls-remote "https://github.com/$REPO.git" "refs/heads/$REF") && echo 'test "$SHA" = "$head"'`,
	} {
		t.Run("refuse-stale の step が "+name+"なら違反", func(t *testing.T) {
			t.Parallel()

			job := jobOf(t, replaced(t, applyWorkflow, staleRun, run), "apply-admin-identity")
			got := checkRefusal(job, credOf(job), refuseStale, formatted)
			assert.Len(t, got, 1)
			assert.Contains(t, messages(got), "ls-remote ...) で取り、test")
		})
	}

	for name, run := range map[string]string{
		"workflow と同じ形": "        run: |\n          auth=$(printf 'x-access-token:%s' \"$GH_TOKEN\" | base64 -w0)\n          test -n \"$auth\"\n          head=$(git -c \"http.https://github.com/.extraheader=AUTHORIZATION: basic $auth\" ls-remote \"https://github.com/$REPO.git\" \"refs/heads/$REF\" \\\n            | awk '{ print $1 }') \\\n            || { echo \"❌ git ls-remote で決められません\"; exit 1; }\n          test \"$SHA\" = \"$head\" || { echo \"❌ 古い\"; exit 1; }",
		"コメントの gh":      fetchAnd("# gh api は使わない"),
		"gh を含む語":       fetchAnd("echo high-gh-x"),
		"GH_TOKEN":      fetchAnd(`test -n "$GH_TOKEN"`),
	} {
		t.Run("refuse-stale の step の "+name+" は違反0件", func(t *testing.T) {
			t.Parallel()

			job := jobOf(t, replaced(t, applyWorkflow, staleRun, run), "apply-admin-identity")
			assert.Empty(t, checkRefusal(job, credOf(job), refuseStale, formatted))
		})
	}

	for name, c := range map[string]struct{ src, job string }{
		"lock の保持": {holdWorkflow, "hold-admin-identity"},
		"lock の解除": {unlockWorkflow, "unlock-admin-identity"},
	} {
		t.Run(name+"の job の refuse-stale も REST を呼べば違反", func(t *testing.T) {
			t.Parallel()

			job := jobOf(t, replaced(t, c.src, staleRun, fetchAnd("gh api rate_limit")), c.job)
			assert.Contains(t, messages(checkRefusal(job, credOf(job), refuseStale, formatted)), "REST API を呼ぶ道具を書かないでください")
		})

		t.Run(name+"の job の refuse-stale が先頭を取らなければ違反", func(t *testing.T) {
			t.Parallel()

			job := jobOf(t, replaced(t, c.src, staleRun, `        run: test "$SHA" = "$HEAD"`), c.job)
			assert.Contains(t, messages(checkRefusal(job, credOf(job), refuseStale, formatted)), "ls-remote ...) で取り、test")
		})
	}

	t.Run("refuse-untrusted の step の gh は REST の検査の外", func(t *testing.T) {
		t.Parallel()

		job := planWith(t, "        run: test \"$HEAD_REPO_ID\" = \"$REPO_ID\"", "        run: gh api rate_limit")
		assert.Empty(t, checkRefusal(job, credOf(job), refuseUntrusted, formatted))
	})

	t.Run("拒否の step の引用符付きの if: は違反", func(t *testing.T) {
		t.Parallel()

		job := planWith(t, "        id: refuse-untrusted\n", "        id: refuse-untrusted\n        'if': false\n")
		assert.Contains(t, messages(checkRefusal(job, credOf(job), refuseUntrusted, formatted)), "if: と continue-on-error: を置かない")
	})

	t.Run("同じ id の step が資格情報の後にもあれば違反", func(t *testing.T) {
		t.Parallel()

		job := planWith(t, "      - run: make ci-plan-admin-identity", "      - id: refuse-untrusted\n        run: exit 1\n      - run: make ci-plan-admin-identity")
		assert.Contains(t, messages(checkRefusal(job, credOf(job), refuseUntrusted, formatted)), "資格情報を取る前に")
	})
}

func Test_checkStaleBody(t *testing.T) {
	t.Parallel()

	staleRun := "        run: head=$(git ls-remote \"https://github.com/$REPO.git\" \"refs/heads/$REF\") && test \"$SHA\" = \"$head\" || exit 1"
	stepOf := func(t *testing.T, run string) workflow.Step {
		t.Helper()

		src := applyWorkflow
		if run != staleRun {
			src = replaced(t, applyWorkflow, staleRun, run)
		}
		for _, step := range workflow.SplitSteps(jobOf(t, src, "apply-admin-identity")) {
			if hasStepID(step, refuseStale) {
				return step
			}
		}
		require.FailNow(t, "refuse-stale の step がありません")

		return workflow.Step{}
	}

	t.Run("先頭を git ls-remote で取って比べれば違反0件", func(t *testing.T) {
		t.Parallel()

		assert.Empty(t, checkStaleBody(stepOf(t, staleRun), formatted))
	})

	t.Run("REST を呼ぶ行ごとに違反を返す", func(t *testing.T) {
		t.Parallel()

		got := checkStaleBody(stepOf(t, "        run: |\n          gh api a\n          curl b\n          head=$(git ls-remote x y)\n          test \"$SHA\" = \"$head\" || exit 1"), formatted)
		assert.Len(t, got, 2)
		assert.Contains(t, messages(got), "REST API を呼ぶ道具を書かないでください")
	})

	t.Run("取るだけで比べなければ違反", func(t *testing.T) {
		t.Parallel()

		got := checkStaleBody(stepOf(t, `        run: head=$(git ls-remote x y)`), formatted)
		assert.Len(t, got, 1)
		assert.Contains(t, messages(got), "ls-remote ...) で取り、test")
	})

	t.Run("比べるだけで取らなければ違反", func(t *testing.T) {
		t.Parallel()

		got := checkStaleBody(stepOf(t, `        run: test "$SHA" = "$head"`), formatted)
		assert.Len(t, got, 1)
		assert.Contains(t, messages(got), "ls-remote ...) で取り、test")
	})
}

func Test_checkTerraform(t *testing.T) {
	t.Parallel()

	planWith := func(t *testing.T, add string) workflow.Job {
		t.Helper()

		return jobOf(t, replaced(t, planWorkflow, "      - run: make ci-plan-admin-identity RUNNER_MODE=host\n", "      - run: make ci-plan-admin-identity RUNNER_MODE=host\n"+add), "plan-admin-identity")
	}

	t.Run("入口だけで走らせれば違反0件", func(t *testing.T) {
		t.Parallel()

		assert.Empty(t, checkTerraform(jobOf(t, planWorkflow, "plan-admin-identity"), cientry.Plan, formatted))
	})

	cases := map[string]string{
		"terraform の直接の起動は違反":    "      - run: terraform -chdir=env/admin/identity plan\n",
		"local.mk の入口は違反":        "      - run: make plan-admin-identity RUNNER_MODE=host\n",
		"make tf- は違反":           "      - run: make -s tf-validate\n",
		"別の役割の CI の入口は違反":        "      - run: make ci-apply-admin-identity RUNNER_MODE=host\n",
		"lock の解除の入口は違反":         "      - run: make ci-unlock-admin-identity RUNNER_MODE=host\n",
		"local.mk の init の入口は違反": "      - run: make init-admin-identity RUNNER_MODE=host\n",
		"|| true で入口の失敗を隠すのは違反":  "      - run: make ci-plan-admin-identity RUNNER_MODE=host || true\n",
		"; で後続を足すのは違反":           "      - run: make ci-plan-admin-identity; true\n",
		"-n の入口は違反":              "      - run: make -n ci-plan-admin-identity\n",
	}
	for name, add := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Contains(t, messages(checkTerraform(planWith(t, add), cientry.Plan, formatted)), "make ci-plan-* でだけ")
		})
	}

	t.Run("terraform を入れる行は起動と数えない", func(t *testing.T) {
		t.Parallel()

		assert.Empty(t, checkTerraform(planWith(t, "      - uses: ./.github/actions/setup-mise\n        with:\n          tools: aqua:hashicorp/terraform go\n"), cientry.Plan, formatted))
	})

	t.Run("terraform を入れる字面を添えた run: の起動は違反", func(t *testing.T) {
		t.Parallel()

		got := messages(checkTerraform(planWith(t, "      - run: terraform plan # aqua:hashicorp/terraform\n"), cientry.Plan, formatted))
		assert.Contains(t, got, "make ci-plan-* でだけ")
	})

	t.Run("入口が無ければ違反", func(t *testing.T) {
		t.Parallel()

		job := jobOf(t, strings.Replace(planWorkflow, "make ci-plan-admin-identity", "echo", 1), "plan-admin-identity")
		assert.Contains(t, messages(checkTerraform(job, cientry.Plan, formatted)), "plan の job は make ci-plan-* を1回だけ走らせてください（0 回）")
	})

	t.Run("入口が2つなら違反", func(t *testing.T) {
		t.Parallel()

		got := messages(checkTerraform(planWith(t, "      - run: make ci-plan-admin-workload RUNNER_MODE=host\n"), cientry.Plan, formatted))
		assert.Equal(t, "plan の job は make ci-plan-* を1回だけ走らせてください（2 回）\n", got)
	})

	t.Run("apply の入口が2つなら違反", func(t *testing.T) {
		t.Parallel()

		src := replaced(t, applyWorkflow, applyEntryLine, applyEntryLine+"      - run: make ci-apply-admin-workload RUNNER_MODE=host\n")
		got := messages(checkTerraform(jobOf(t, src, "apply-admin-identity"), cientry.Apply, formatted))
		assert.Equal(t, "apply の job は make ci-apply-* を1回だけ走らせてください（2 回）\n", got)
	})

	t.Run("コメントと name: の入口の字面は数えない", func(t *testing.T) {
		t.Parallel()

		assert.Empty(t, checkTerraform(planWith(t, "      # make ci-plan-admin-workload\n      - name: make ci-plan-admin-workload\n        run: echo x\n"), cientry.Plan, formatted))
	})
}

func Test_checkDenied(t *testing.T) {
	t.Parallel()

	const denied = "        run: make ci-assume-denied CI_ENV=admin RUNNER_MODE=host\n"
	planWith := func(t *testing.T, from, to string) workflow.Job {
		t.Helper()

		return jobOf(t, replaced(t, planWorkflow, from, to), "plan-admin-identity")
	}

	t.Run("資格情報の後で plan の前に1回あれば違反0件", func(t *testing.T) {
		t.Parallel()

		job := jobOf(t, planWorkflow, "plan-admin-identity")
		assert.Empty(t, checkDenied(job, credOf(job), formatted))
	})

	t.Run("checkPlan が呼ぶ", func(t *testing.T) {
		t.Parallel()

		job := planWith(t, denied, "        run: echo skipped\n")
		assert.Contains(t, messages(checkPlan(job, credOf(job), infoOf(planWorkflow), formatted)), "1回だけ置いて")
	})

	cases := map[string]struct{ from, to, want string }{
		"無ければ違反":              {denied, "        run: echo skipped\n", "1回だけ置いてください（0 回）"},
		"コメントにあるだけなら違反":       {denied, "        run: echo skipped\n        # make ci-assume-denied CI_ENV=admin RUNNER_MODE=host\n", "1回だけ置いてください（0 回）"},
		"2回あれば違反":             {denied, denied + "      - run: make ci-assume-denied CI_ENV=admin RUNNER_MODE=host\n", "（2 回）"},
		"役割を狭める代入は違反":         {denied, "        run: make ci-assume-denied CI_ENV=admin CI_DENIED_ROLES=plan RUNNER_MODE=host\n", "run: make ci-assume-denied CI_ENV=<env> RUNNER_MODE=host だけ"},
		"失敗を隠す後続は違反":          {denied, "        run: make ci-assume-denied CI_ENV=admin RUNNER_MODE=host || true\n", "run: make ci-assume-denied CI_ENV=<env> RUNNER_MODE=host だけ"},
		"ブロックスカラーの run: は違反":  {denied, "        run: |\n          make ci-assume-denied CI_ENV=admin RUNNER_MODE=host\n", "run: make ci-assume-denied CI_ENV=<env> RUNNER_MODE=host だけ"},
		"step 名にあるだけなら違反":     {denied, "        name: make ci-assume-denied CI_ENV=admin RUNNER_MODE=host\n        run: echo skipped\n", "1回だけ置いてください（0 回）"},
		"資格情報の step に連結すれば違反": {"        run: make ci-aws-credentials CI_ENV=admin CI_ROLE=plan RUNNER_MODE=host\n", "        run: make ci-aws-credentials CI_ENV=admin CI_ROLE=plan RUNNER_MODE=host && make ci-assume-denied CI_ENV=admin RUNNER_MODE=host\n", "1回だけ置いてください（2 回）"},
		"資格情報の CI_ENV と違えば違反": {denied, "        run: make ci-assume-denied CI_ENV=sandbox RUNNER_MODE=host\n", "CI_ENV=sandbox が、資格情報の CI_ENV=admin と違います"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			job := planWith(t, c.from, c.to)
			assert.Contains(t, messages(checkDenied(job, credOf(job), formatted)), c.want)
		})
	}

	t.Run("フロー形式の step は違反", func(t *testing.T) {
		t.Parallel()

		src := replaced(t, planWorkflow, denied, "        run: echo moved\n")
		src = replaced(t, src, "      - run: make ci-plan-admin-identity RUNNER_MODE=host\n", "      - {run: make ci-assume-denied CI_ENV=admin RUNNER_MODE=host}\n      - run: make ci-plan-admin-identity RUNNER_MODE=host\n")
		job := jobOf(t, src, "plan-admin-identity")
		assert.Contains(t, messages(checkDenied(job, credOf(job), formatted)), "run: make ci-assume-denied CI_ENV=<env> RUNNER_MODE=host だけ")
	})

	t.Run("資格情報より前なら違反", func(t *testing.T) {
		t.Parallel()

		src := replaced(t, planWorkflow, denied, "        run: echo moved\n")
		src = replaced(t, src, "      - name: Run terraform plan through make\n", "      - run: make ci-assume-denied CI_ENV=admin RUNNER_MODE=host\n      - name: Run terraform plan through make\n")
		job := jobOf(t, src, "plan-admin-identity")
		assert.Contains(t, messages(checkDenied(job, credOf(job), formatted)), "資格情報を取る step より後")
	})

	t.Run("plan の入口より後なら違反", func(t *testing.T) {
		t.Parallel()

		src := replaced(t, planWorkflow, denied, "        run: echo moved\n")
		src = replaced(t, src, "      - run: make ci-plan-admin-identity RUNNER_MODE=host\n", "      - run: make ci-plan-admin-identity RUNNER_MODE=host\n      - run: make ci-assume-denied CI_ENV=admin RUNNER_MODE=host\n")
		job := jobOf(t, src, "plan-admin-identity")
		assert.Contains(t, messages(checkDenied(job, credOf(job), formatted)), "plan の入口より前")
	})
}

func Test_runsGate(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		step string
		want bool
	}{
		"make infra-code-gate を走らせる": {"      - run: make infra-code-gate RUNNER_MODE=host\n", true},
		"引数の無い make infra-code-gate": {"      - run: make infra-code-gate\n", true},
		"-test の付いた別のターゲットは偽":        {"      - run: make infra-code-gate-test\n", false},
		"name: の中は偽":                 {"      - name: make infra-code-gate\n        run: echo\n", false},
		"コメントは偽":                     {"      # - run: make infra-code-gate\n      - run: echo\n", false},
		"make を通さない検出の道具は偽":          {"      - run: ./tmp/bin/infra-code-gate\n", false},
		"echo の引数は偽":                 {"      - run: echo make infra-code-gate\n", false},
		"|| true で失敗を隠すのは偽":          {"      - run: make infra-code-gate || true\n", false},
		"; で後続を足すのは偽":                {"      - run: make infra-code-gate; true\n", false},
		"ブロックの run: は偽":              {"      - run: |\n          make infra-code-gate\n", false},
		"name: の後の run: も読む":         {"      - name: Detect\n        run: make infra-code-gate\n", true},
		"-n（実行しない）は偽":                {"      - run: make -n infra-code-gate\n", false},
		"-i（失敗を無視）は偽":                {"      - run: make -i infra-code-gate\n", false},
		"--ignore-errors は偽":         {"      - run: make --ignore-errors infra-code-gate\n", false},
		"-C（別のディレクトリ）は偽":             {"      - run: make -C /tmp infra-code-gate\n", false},
		"-f（別の Makefile）は偽":          {"      - run: make -f /dev/null infra-code-gate\n", false},
		"後置の -n は偽":                  {"      - run: make infra-code-gate -n\n", false},
		"RUNNER_MODE=host は認める":      {"      - run: make RUNNER_MODE=host infra-code-gate\n", true},
		"-k は認める":                    {"      - run: make -k infra-code-gate RUNNER_MODE=host\n", true},
		"後置の -k は偽":                  {"      - run: make infra-code-gate -k\n", false},
		"-k の後の -n は偽":               {"      - run: make -k -n infra-code-gate\n", false},
		"MAKEFLAGS=n（実行しない）は偽":       {"      - run: make infra-code-gate MAKEFLAGS=n\n", false},
		"SHELL の差し替えは偽":              {"      - run: make SHELL=/usr/bin/true infra-code-gate\n", false},
		"ほかの変数の代入は偽":                 {"      - run: make infra-code-gate V=1\n", false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, c.want, runsGate(jobOf(t, "jobs:\n  a:\n    steps:\n"+c.step, "a"), detectionGate))
		})
	}

	order := map[string]struct {
		step string
		want bool
	}{
		"make resource-order-check を走らせる": {"      - run: make resource-order-check RUNNER_MODE=host\n", true},
		"-test の付いた別のターゲットは偽":             {"      - run: make resource-order-check-test RUNNER_MODE=host\n", false},
		"検出は順序の検査ではない":                    {"      - run: make infra-code-gate RUNNER_MODE=host\n", false},
		"-n（実行しない）は偽":                     {"      - run: make -n resource-order-check\n", false},
	}
	for name, c := range order {
		t.Run("順序の検査/"+name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, c.want, runsGate(jobOf(t, "jobs:\n  a:\n    steps:\n"+c.step, "a"), orderGate))
		})
	}

	layer := map[string]struct {
		step string
		want bool
	}{
		"make layer-check を走らせる":   {"      - run: make layer-check RUNNER_MODE=host\n", true},
		"-k は認める":                  {"      - run: make -k layer-check RUNNER_MODE=host\n", true},
		"make layer-check-test は偽": {"      - run: make layer-check-test RUNNER_MODE=host\n", false},
		"検出は層の検査ではない":              {"      - run: make infra-code-gate RUNNER_MODE=host\n", false},
		"順序の検査は層の検査ではない":           {"      - run: make resource-order-check RUNNER_MODE=host\n", false},
		"-n（実行しない）は偽":              {"      - run: make -n layer-check\n", false},
		"後置の -n は偽":                {"      - run: make layer-check -n\n", false},
		"|| true で失敗を隠すのは偽":        {"      - run: make layer-check || true\n", false},
		"ほかの変数の代入は偽":               {"      - run: make layer-check RUNNER_MODE=host LY_NS=x\n", false},
	}
	for name, c := range layer {
		t.Run("層の検査/"+name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, c.want, runsGate(jobOf(t, "jobs:\n  a:\n    steps:\n"+c.step, "a"), layerGate))
		})
	}

	root := map[string]struct {
		step string
		want bool
	}{
		"make root-check を走らせる": {"      - run: make root-check RUNNER_MODE=host\n", true},
		"-test の付いた別のターゲットは偽":   {"      - run: make root-check-test RUNNER_MODE=host\n", false},
		"層の検査は root の検査ではない":    {"      - run: make layer-check RUNNER_MODE=host\n", false},
		"-n（実行しない）は偽":           {"      - run: make -n root-check\n", false},
		"|| true で失敗を隠すのは偽":     {"      - run: make root-check || true\n", false},
		"ほかの変数の代入は偽":            {"      - run: make root-check RUNNER_MODE=host RT_NS=x\n", false},
	}
	for name, c := range root {
		t.Run("root の検査/"+name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, c.want, runsGate(jobOf(t, "jobs:\n  a:\n    steps:\n"+c.step, "a"), rootGate))
		})
	}
}

func Test_makeRun(t *testing.T) {
	t.Parallel()

	re := makeRun(`ci-plan-[a-z0-9-]+`)
	cases := map[string]struct {
		line string
		want bool
	}{
		"make と target":         {"      - run: make ci-plan-admin-identity", true},
		"後ろの RUNNER_MODE=host":  {"        run: make ci-plan-admin-identity RUNNER_MODE=host", true},
		"前の RUNNER_MODE=host":   {"      - run: make RUNNER_MODE=host ci-plan-admin-identity", true},
		"host 以外の RUNNER_MODE":  {"      - run: make ci-plan-admin-identity RUNNER_MODE=container", false},
		"MAKEFLAGS の代入は当たらない":   {"      - run: make MAKEFLAGS=-i ci-plan-admin-identity", false},
		"SHELL の代入は当たらない":       {"      - run: make ci-plan-admin-identity SHELL=/usr/bin/true", false},
		"道具の差し替えは当たらない":         {"      - run: make ci-plan-admin-identity TF_BIN=/bin/true", false},
		"Policy の場所の差し替えは当たらない": {"      - run: make ci-plan-admin-identity POLICY_PLAN_DIR=x", false},
		"コマンド置換の代入は当たらない":       {"      - run: make X=$(terraform${IFS}apply) ci-plan-admin-identity", false},
		"引用符付きの run キー":         {"      - \"run\": make ci-plan-admin-identity", true},
		"オプションは当たらない":           {"      - run: make -n ci-plan-admin-identity", false},
		"後続のコマンドは当たらない":         {"      - run: make ci-plan-admin-identity && true", false},
		"step の桁でなければ当たらない":     {"    run: make ci-plan-admin-identity", false},
		"run: の値が make で始まらない":  {"      - run: env make ci-plan-admin-identity", false},
		"target が2つは当たらない":      {"      - run: make ci-plan-a ci-plan-b", false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, c.want, re.MatchString(c.line))
		})
	}
}

func Test_requiresSuccess(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		cond string
		want bool
	}{
		"式の中の1項":          {"${{ needs.g.result == 'success' && github.ref_name == 'x' }}", true},
		"後ろの項":            {"${{ github.ref_name == 'x' && needs.g.result == 'success' }}", true},
		"式の記号の無い1項":       {"needs.g.result=='success'", true},
		"ブロックの値":          {">- needs.g.result == 'success' && a", true},
		"括弧で包んだ比較は認めない":   {"${{ (needs.g.result == 'success') == false }}", false},
		"別の比較は認めない":       {"${{ needs.g.result != 'failure' }}", false},
		"別の job の成功は認めない": {"${{ needs.h.result == 'success' }}", false},
		"空の式":             {"", false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, c.want, requiresSuccess(c.cond, "g"))
		})
	}
}

func Test_jobKey(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		body string
		want string
		ok   bool
	}{
		"1行の値":            {"    if: ${{ always() }}\n    runs-on: x\n", "${{ always() }}", true},
		"ブロックの値をつなぐ":      {"    if: >-\n      a &&\n      b\n    runs-on: x\n", ">- a && b", true},
		"step の if: は見ない": {"    steps:\n      - if: x\n        run: y\n", "", false},
		"無ければ偽":           {"    runs-on: x\n", "", false},
		"引用符付きのキー":        {"    \"if\": x\n", "x", true},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, _, ok := jobKey(jobOf(t, "jobs:\n  a:\n"+c.body, "a"), "if")
			assert.Equal(t, c.ok, ok)
			assert.Equal(t, c.want, got)
		})
	}
}

func Test_hasIDToken(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		values []string
		want   bool
	}{
		"id-token: write を持てば真":     {[]string{"contents: read", "id-token: write"}, true},
		"引用符付きの id-token: write も真": {[]string{`"id-token": 'write'`}, true},
		"id-token: read は偽":         {[]string{"id-token: read"}, false},
		"値が無ければ偽":                   {nil, false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, c.want, hasIDToken(c.values))
		})
	}
}

func Test_permissionsBlock(t *testing.T) {
	t.Parallel()

	t.Run("ブロックの値を返す", func(t *testing.T) {
		t.Parallel()

		got, at, ok := permissionsBlock(lines("    permissions:", "      # c", "      contents: read", "", "      id-token: write", "    steps:"), "    ")
		require.True(t, ok)
		assert.Equal(t, 1, at)
		assert.Equal(t, []string{"contents: read", "id-token: write"}, got)
	})

	t.Run("1行の値を返す", func(t *testing.T) {
		t.Parallel()

		got, _, ok := permissionsBlock(lines("permissions: write-all"), "")
		require.True(t, ok)
		assert.Equal(t, []string{"write-all"}, got)
	})

	t.Run("違う桁の permissions は見ない", func(t *testing.T) {
		t.Parallel()

		_, _, ok := permissionsBlock(lines("  permissions:", "    id-token: write"), "    ")
		assert.False(t, ok)
	})
}

func Test_environment(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		body string
		want string
	}{
		"1行の名前":              {"    environment: admin-plan\n", "admin-plan"},
		"引用符の名前":             {"    environment: \"admin-plan\"\n", "admin-plan"},
		"ブロックの name":         {"    environment:\n      url: x\n      name: admin-plan\n", "admin-plan"},
		"式は空":                {"    environment: ${{ vars.ENV }}\n", ""},
		"environment が無ければ空": {"    runs-on: x\n", ""},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, _ := environment(jobOf(t, "jobs:\n  a:\n"+c.body, "a"))
			assert.Equal(t, c.want, got)
		})
	}
}

func Test_triggers(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		src  string
		want []string
	}{
		"ブロック":        {"on:\n  push:\n    branches: [x]\n  workflow_dispatch:\n", []string{"push", "workflow_dispatch"}},
		"1行":          {"on: pull_request\n", []string{"pull_request"}},
		"フローの並び":      {"on: [push, pull_request]\n", []string{"pull_request", "push"}},
		"引用符の on":     {"\"on\":\n  push:\n", []string{"push"}},
		"on が無ければ空":   {"name: x\n", []string{}},
		"コメントと空行を飛ばす": {"on:\n  # c\n\n  push:\n", []string{"push"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			on, _ := triggers(lines(strings.Split(strings.TrimSuffix(c.src, "\n"), "\n")...))
			assert.Equal(t, c.want, triggerNames(on))
		})
	}

	t.Run("同じ行に値を書いた起動条件は本文として残す", func(t *testing.T) {
		t.Parallel()

		on, _ := triggers(lines("on:", "  push: { branches: [x], paths: [y] }", "  workflow_dispatch:", "jobs:"))
		require.Len(t, on["push"], 1)
		assert.Equal(t, "  push: { branches: [x], paths: [y] }", on["push"][0].Text)
		assert.Empty(t, on["workflow_dispatch"])
	})

	t.Run("起動条件の本文を返す", func(t *testing.T) {
		t.Parallel()

		on, at := triggers(lines("name: x", "on:", "  workflow_dispatch:", "    inputs:", "jobs:"))
		assert.Equal(t, 2, at)
		require.Len(t, on["workflow_dispatch"], 1)
		assert.Equal(t, "    inputs:", on["workflow_dispatch"][0].Text)
	})
}

func Test_triggerNames(t *testing.T) {
	t.Parallel()

	t.Run("名前を整列して返す", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, []string{"a", "b"}, triggerNames(map[string][]workflow.Line{"b": nil, "a": nil}))
	})

	t.Run("起動条件が無ければ空", func(t *testing.T) {
		t.Parallel()

		assert.Empty(t, triggerNames(nil))
	})
}

func Test_needs(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		body string
		want []string
	}{
		"1行":           {"    needs: infra-code-gate\n", []string{"infra-code-gate"}},
		"フローの並び":       {"    needs: [changes, 'infra-code-gate']\n", []string{"changes", "infra-code-gate"}},
		"ブロックの並び":      {"    needs:\n      - changes\n      - infra-code-gate\n    steps:\n", []string{"changes", "infra-code-gate"}},
		"needs が無ければ空": {"    runs-on: x\n", nil},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, c.want, needs(jobOf(t, "jobs:\n  a:\n"+c.body, "a")))
		})
	}
}

func Test_hasStepID(t *testing.T) {
	t.Parallel()

	steps := workflow.SplitSteps(jobOf(t, "jobs:\n  a:\n    steps:\n      - id: refuse-untrusted\n        run: x\n      - name: y\n        id: \"other\"\n", "a"))
	require.Len(t, steps, 2)

	t.Run("先頭の id を読む", func(t *testing.T) {
		t.Parallel()

		assert.True(t, hasStepID(steps[0], "refuse-untrusted"))
	})

	t.Run("引用符付きの id を読む", func(t *testing.T) {
		t.Parallel()

		assert.True(t, hasStepID(steps[1], "other"))
	})

	t.Run("別の id は偽", func(t *testing.T) {
		t.Parallel()

		assert.False(t, hasStepID(steps[1], "refuse-untrusted"))
	})
}

func Test_cancelsNothing(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		body string
		want bool
	}{
		"false を明示":           {"    concurrency:\n      group: x\n      cancel-in-progress: false\n", true},
		"true は不可":            {"    concurrency:\n      group: x\n      cancel-in-progress: true\n", false},
		"1行の concurrency は不可": {"    concurrency: x\n", false},
		"concurrency が無ければ不可": {"    runs-on: x\n", false},
		"間のコメントと空行を飛ばす":       {"    concurrency:\n      group: x\n      # c\n\n      cancel-in-progress: false\n", true},
		"引用符付きのキーを読む":         {"    'concurrency':\n      group: x\n      \"cancel-in-progress\": false\n", true},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, c.want, cancelsNothing(jobOf(t, "jobs:\n  a:\n"+c.body, "a").Lines, "    "))
		})
	}

	t.Run("workflow 全体の false を読む", func(t *testing.T) {
		t.Parallel()

		assert.True(t, cancelsNothing(lines("concurrency:", "  group: x", "  cancel-in-progress: false", "jobs:"), ""))
	})

	t.Run("別の桁の false は読まない", func(t *testing.T) {
		t.Parallel()

		assert.False(t, cancelsNothing(lines("concurrency:", "  group: x", "    cancel-in-progress: false", "jobs:"), ""))
	})
}

func Test_hasConcurrency(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		lines  []workflow.Line
		indent string
		want   bool
	}{
		"ブロックの concurrency を読む":   {lines("concurrency:", "  group: x"), "", true},
		"1行の concurrency も数える":    {lines("concurrency: x"), "", true},
		"引用符付きのキーも数える":            {lines(`"concurrency": x`), "", true},
		"単引用符のキーも数える":             {lines("'concurrency': x"), "", true},
		"別の桁の concurrency は数えない":  {lines("    concurrency: x"), "", false},
		"concurrency が無ければ偽":      {lines("permissions:", "  contents: read"), "", false},
		"job の桁の concurrency を読む": {lines("    concurrency:", "      group: x"), "    ", true},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, c.want, hasConcurrency(c.lines, c.indent))
		})
	}
}

func bodyJob(t *testing.T, body string) workflow.Job {
	t.Helper()
	jobs := workflow.SplitJobs("on:\n  push:\njobs:\n  x:\n" + body).Jobs
	require.Len(t, jobs, 1)
	return jobs[0]
}

func Test_assumesRole(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		body string
		want bool
	}{
		"job の id-token: write は対象":        {"    permissions:\n      contents: read\n      id-token: write\n", true},
		"引用符付きの id-token: write は対象":       {"    permissions:\n      \"id-token\": \"write\"\n", true},
		"permissions が無ければ対象にしない":          {"    steps:\n      - run: make lint\n", false},
		"id-token: read は対象にしない":           {"    permissions:\n      id-token: read\n", false},
		"id-token の無い permissions は対象にしない": {"    permissions:\n      contents: read\n", false},
		"write-all は対象にしない":                {"    permissions: write-all\n", false},
		"1行の {} は対象にしない":                   {"    permissions: { id-token: write }\n", false},
		"空の {} は対象にしない":                    {"    permissions: {}\n", false},
		"行末コメント付きの id-token: write は対象":    {"    permissions:\n      id-token: write # oidc\n", true},
		"値が次の行に続く id-token は対象にしない":        {"    permissions:\n      id-token:\n        write\n", false},
		"step の深さの permissions は対象にしない":    {"    steps:\n      - permissions:\n          id-token: write\n", false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, c.want, assumesRole(bodyJob(t, c.body)))
		})
	}
}

func Test_hasTokenLine(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		body string
		want bool
	}{
		"読める id-token: write を持つ":            {"    permissions:\n      id-token: write\n", true},
		"引用符付きの \"id-token\": \"write\" を持つ": {"    permissions:\n      \"id-token\": \"write\"\n", true},
		"値が次の行に続く id-token を持つ":              {"    permissions:\n      id-token:\n        write\n", true},
		"コロンの前に空白のある id-token を持つ":           {"    permissions:\n      id-token : write\n", true},
		"id-token: read は持たない":               {"    permissions:\n      id-token: read\n", false},
		"id-token: none は持たない":               {"    permissions:\n      id-token: none\n", false},
		"引用符付きの id-token: \"read\" は持たない":    {"    permissions:\n      id-token: \"read\"\n", false},
		"コメント付きの id-token: read は持たない":       {"    permissions:\n      id-token: read # c\n", false},
		"1行の {} の id-token: none は持たない":      {"    permissions: { id-token: none }\n", false},
		"コメントの id-token は持たない":               {"    # id-token: write\n    steps:\n      - run: make lint\n", false},
		"id-token の行が無ければ持たない":               {"    permissions:\n      contents: read\n", false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, c.want, hasTokenLine(bodyJob(t, c.body)))
		})
	}
}

func Test_entryLine(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		line string
		want bool
	}{
		"run: の make ci-plan-* は入口":      {"      - run: make ci-plan-admin-identity RUNNER_MODE=host", true},
		"make ci-apply-* は入口":            {"          make ci-apply-admin-workload RUNNER_MODE=host", true},
		"make ci-unlock-* は入口":           {"        run: make ci-unlock-admin-identity", true},
		"make ci-hold-* は入口":             {"        run: make ci-hold-admin-identity", true},
		"echo の引数の入口も数える":                {"        run: echo \"make ci-unlock-admin-identity で解除\"", true},
		"env: の値の入口も数える":                 {"          NOTE: make ci-apply-admin-identity", true},
		"step と別の行の run: は数える":           {"        run: make ci-apply-admin-identity", true},
		"桁0の name: は数える":                 {"name: make ci-apply-admin-identity", true},
		"コメントの入口は数えない":                   {"      # make ci-apply-admin-identity", false},
		"step の name: の入口は数えない":          {"      - name: make ci-apply-admin-identity", false},
		"job の name: の入口は数えない":           {"    name: make ci-apply-admin-identity", false},
		"ci-revert-* は入口ではない":            {"        run: make ci-revert-plan RUNNER_MODE=host", false},
		"ci-aws-credentials は入口ではない":     {"        run: make ci-aws-credentials CI_ENV=admin CI_ROLE=plan", false},
		"make を伴わない ci-apply-* も入口":      {"        run: echo ci-apply-admin-identity", true},
		"行継続の次の行の入口も数える":                 {"            ci-apply-admin-identity RUNNER_MODE=host", true},
		"$(MAKE) の入口も数える":                {"        run: $(MAKE) ci-hold-admin-identity", true},
		"式で組んだ名前の入口も数える":                 {"        run: make ci-apply-${{ inputs.root }}", true},
		"ci-plan と区切りの無い語は入口ではない":        {"        run: make ci-planner", false},
		"ci-plan- の後の名前は入口":              {"        run: make ci-plan-x", true},
		"語の途中の ci-apply- は入口ではない":        {"        run: make xci-apply-x", false},
		"ハイフンで続く接頭辞の後の ci-apply- は入口":    {"        run: make my-ci-apply-x", true},
		"名前の無い ci-apply- も入口":            {"        run: make ci-apply-", true},
		"concurrency の group の入口の字面も数える": {"      group: ci-apply-${{ github.ref }}", true},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, c.want, entryLine(c.line))
		})
	}
}

func Test_countEntryLines(t *testing.T) {
	t.Parallel()

	t.Run("job の外を含むファイル全体の入口の行を数え、コメントと name: を除く", func(t *testing.T) {
		t.Parallel()

		src := "x-note: make ci-plan-a\njobs:\n  x:\n    steps:\n      - name: make ci-apply-a\n        run: make ci-apply-a\n      # make ci-hold-a\n      - run: |\n          make ci-unlock-a\n"
		assert.Equal(t, 3, countEntryLines(src))
	})

	t.Run("入口の無いファイルは0件", func(t *testing.T) {
		t.Parallel()

		assert.Zero(t, countEntryLines("on:\n  push:\njobs:\n  x:\n    steps:\n      - run: make lint\n      # make ci-apply-a\n"))
	})
}

func Test_jobEntryLines(t *testing.T) {
	t.Parallel()

	t.Run("job の中の入口の行を行ごとに数える", func(t *testing.T) {
		t.Parallel()

		job := bodyJob(t, "    steps:\n      - name: make ci-plan-a\n        run: make ci-plan-a\n      - run: make ci-apply-a\n      # make ci-hold-a\n")
		assert.Equal(t, 2, jobEntryLines(job))
	})

	t.Run("入口の無い job は0件", func(t *testing.T) {
		t.Parallel()

		assert.Zero(t, jobEntryLines(bodyJob(t, "    steps:\n      - run: make lint\n")))
	})

	t.Run("実物の形の plan の workflow の入口は job の中にすべて在る", func(t *testing.T) {
		t.Parallel()

		split := workflow.SplitJobs(planWorkflow)
		n := 0
		for _, job := range split.Jobs {
			n += jobEntryLines(job)
		}
		require.Positive(t, n)
		assert.Equal(t, countEntryLines(planWorkflow), n)
	})
}

func Test_countTokenLines(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		line string
		want int
	}{
		"workflow の write-all を数える":       {"permissions: write-all", 1},
		"1行の {} の中の id-token を数える":        {"    permissions: { contents: read, id-token: write }", 1},
		"ブロックの id-token: write を数える":      {"      id-token: write", 1},
		"値が次の行に続く id-token を数える":          {"      id-token:", 1},
		"コロンの前に空白のある id-token を数える":       {"      id-token : write", 1},
		"コメントの id-token は数えない":            {"      # id-token: write", 0},
		"id-token: read は数えない":            {"      id-token: read", 0},
		"1行の {} の中の id-token: none は数えない": {"    permissions: { id-token: none }", 0},
		"引用符付きの id-token: read は数えない":     {"      id-token: \"read\"", 0},
		"コメント付きの id-token: read は数えない":    {"      id-token: read # c", 0},
		"ブロックの id-token: none は数えない":      {"      id-token: none", 0},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, c.want, countTokenLines(c.line))
		})
	}
}

func Test_unreadable(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		values []string
		want   bool
	}{
		"write-all は読めない":        {[]string{"write-all"}, true},
		"引用符付きの write-all は読めない": {[]string{`"write-all"`}, true},
		"1行の {} の並びは読めない":        {[]string{"{ id-token: write }"}, true},
		"空の {} は読める":             {[]string{"{}"}, false},
		"read-all は読める":          {[]string{"read-all"}, false},
		"行ごとの権限は読める":             {[]string{"contents: read", "id-token: write"}, false},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, c.want, unreadable(c.values))
		})
	}
}

func Test_checkSteps(t *testing.T) {
	t.Parallel()

	t.Run("規則を満たせば違反0件", func(t *testing.T) {
		t.Parallel()

		job := jobOf(t, planWorkflow, "plan-admin-identity")
		assert.Empty(t, checkSteps(job, credOf(job), formatted))
	})

	t.Run("persist-credentials: false の無い checkout は違反", func(t *testing.T) {
		t.Parallel()

		job := jobOf(t, strings.Replace(planWorkflow, "          persist-credentials: false\n", "          fetch-depth: 1\n", 1), "plan-admin-identity")
		assert.Contains(t, messages(checkSteps(job, credOf(job), formatted)), "persist-credentials: false")
	})

	t.Run("資格情報の後の action は違反", func(t *testing.T) {
		t.Parallel()

		job := jobOf(t, strings.Replace(planWorkflow, "      - run: make ci-plan-admin-identity", "      - uses: octo/comment@v1\n      - run: make ci-plan-admin-identity", 1), "plan-admin-identity")
		f := checkSteps(job, credOf(job), formatted)
		require.Len(t, f, 1, "資格情報の前の checkout と setup-mise は数えない")
		assert.Contains(t, f[0].Message, "action を呼ばない")
		assert.Greater(t, f[0].Line, credOf(job).Number)
	})

	t.Run("資格情報の後のコメントの uses: は違反にしない", func(t *testing.T) {
		t.Parallel()

		job := jobOf(t, strings.Replace(planWorkflow, "      - run: make ci-plan-admin-identity", "        # uses: octo/comment@v1\n      - run: make ci-plan-admin-identity", 1), "plan-admin-identity")
		assert.Empty(t, checkSteps(job, credOf(job), formatted))
	})

	t.Run("引用符付きのキーとフロースタイルの uses: は読めないとして違反", func(t *testing.T) {
		t.Parallel()

		for name, step := range map[string]string{
			"引用符付きのキー":     "      - \"uses\": octo/comment@v1",
			"キーの後の空白":      "      - uses : octo/comment@v1",
			"フロースタイル":      "      - { name: Comment, uses: octo/comment@v1 }",
			"継続行の引用符付きのキー": "      - name: Comment\n        'uses': octo/comment@v1",
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				job := jobOf(t, strings.Replace(planWorkflow, "      - run: make ci-plan-admin-identity", step+"\n      - run: make ci-plan-admin-identity", 1), "plan-admin-identity")
				assert.Equal(t, "uses: を読み取れません。`- uses: <action>` の形で書いてください\n", messages(checkSteps(job, credOf(job), formatted)))
			})
		}
	})

	t.Run("引用符付きのキーの checkout は読めないとして違反", func(t *testing.T) {
		t.Parallel()

		src := strings.Replace(planWorkflow, "      - uses: actions/checkout@", "      - \"uses\": actions/checkout@", 1)
		src = strings.Replace(src, "          persist-credentials: false\n", "", 1)
		job := jobOf(t, src, "plan-admin-identity")
		assert.Equal(t, "uses: を読み取れません。`- uses: <action>` の形で書いてください\n", messages(checkSteps(job, credOf(job), formatted)))
	})

	t.Run("steps と同じ桁の並びは読めないとして違反", func(t *testing.T) {
		t.Parallel()

		src := strings.Join([]string{
			"jobs:",
			"  apply:",
			"    steps:",
			"    - uses: actions/checkout@0000000000000000000000000000000000000000",
			"    - run: make ci-aws-credentials CI_ENV=admin CI_ROLE=apply",
			"    - uses: octo/comment@v1",
			"",
		}, "\n")
		job := jobOf(t, src, "apply")
		assert.Contains(t, messages(checkSteps(job, credOf(job), formatted)), "steps を読めません（uses: 2 件のうち 2 件）")
	})
}

func Test_checkIndirect(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		text string
		want []string
	}{
		"ふつうの行は違反0件":            {"        run: make ci-apply-admin-identity RUNNER_MODE=host", nil},
		"glob の * はエイリアスと読まない":  {"          ls *.tf", nil},
		"GITHUB_PATH への書き込みは違反": {"          echo /b >> \"$GITHUB_PATH\"", []string{"GITHUB_PATH と GITHUB_ENV"}},
		"GITHUB_ENV への書き込みは違反":  {"          echo X=1 >> $GITHUB_ENV", []string{"GITHUB_PATH と GITHUB_ENV"}},
		"値のエイリアスは違反":            {"        env: *m", []string{"アンカーとエイリアス"}},
		"値のアンカーは違反":             {"        env: &m", []string{"アンカーとエイリアス"}},
		"リストのエイリアスは違反":          {"      - *step", []string{"アンカーとエイリアス"}},
		"マージキーは違反":              {"          <<: *m", []string{"アンカーとエイリアス"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := checkIndirect(workflow.Line{Number: 1, Text: c.text}, formatted)
			if c.want == nil {
				assert.Empty(t, got)

				return
			}
			for _, w := range c.want {
				assert.Contains(t, messages(got), w)
			}
		})
	}
}

func Test_checkStepEnv(t *testing.T) {
	t.Parallel()

	stepOf := func(t *testing.T, lines ...string) workflow.Step {
		t.Helper()

		src := replaced(t, applyWorkflow, "      - run: make ci-apply-admin-identity RUNNER_MODE=host\n", strings.Join(lines, "\n")+"\n")
		steps := workflow.SplitSteps(jobOf(t, src, "apply-admin-identity"))
		require.NotEmpty(t, steps)

		return steps[len(steps)-1]
	}

	t.Run("許した名前だけなら違反0件", func(t *testing.T) {
		t.Parallel()

		step := stepOf(t, "      - env:", "          CI_ACCOUNT_ID: x", "          # TF_BIN: y", "          CI_PR: |", "            1", "        run: make ci-apply-admin-identity RUNNER_MODE=host")
		assert.Empty(t, checkStepEnv(step, formatted))
	})

	t.Run("git の設定を替える GIT_CONFIG_ の名前は違反", func(t *testing.T) {
		t.Parallel()

		step := stepOf(t, "      - env:", "          GIT_CONFIG_COUNT: \"1\"", "        run: make ci-apply-admin-identity RUNNER_MODE=host")
		assert.Contains(t, messages(checkStepEnv(step, formatted)), "env: に置けるのは")
	})

	t.Run("env: の後の step のキーは名前と読まない", func(t *testing.T) {
		t.Parallel()

		step := stepOf(t, "      - run: make ci-apply-admin-identity RUNNER_MODE=host", "        env:", "          CI_PR: \"1\"", "        timeout-minutes: 5")
		assert.Empty(t, checkStepEnv(step, formatted))
	})

	cases := map[string]struct {
		lines []string
		want  string
	}{
		"許していない名前は違反":          {[]string{"      - run: make ci-apply-admin-identity RUNNER_MODE=host", "        env:", "          GOFLAGS: -toolexec=x"}, "env: に置けるのは"},
		"引用符付きの名前も読む":          {[]string{"      - run: make ci-apply-admin-identity RUNNER_MODE=host", "        env:", "          \"BASH_ENV\": ./x"}, "env: に置けるのは"},
		"フロー形式は違反":             {[]string{"      - run: make ci-apply-admin-identity RUNNER_MODE=host", "        env: {CI_PR: 1}"}, "ブロック形式"},
		"引用符付きの env: も読む":      {[]string{"      - run: make ci-apply-admin-identity RUNNER_MODE=host", "        \"env\":", "          TF_BIN: x"}, "env: に置けるのは"},
		"深く字下げした名前も読む":         {[]string{"      - run: make ci-apply-admin-identity RUNNER_MODE=host", "        env:", "            TF_BIN: /bin/true"}, "env: に置けるのは"},
		"桁のずれた名前は読めない":         {[]string{"      - run: make ci-apply-admin-identity RUNNER_MODE=host", "        env:", "            CI_PR: \"1\"", "           TF_BIN: /bin/true"}, "名前を読めません"},
		"ブロックスカラーでない値の続きは読めない": {[]string{"      - run: make ci-apply-admin-identity RUNNER_MODE=host", "        env:", "          CI_PR: \"1\"", "            TF_BIN: /bin/true"}, "名前を読めません"},
		"- env: の下の深い名前も読む":    {[]string{"      - env:", "            TF_BIN: /bin/true", "        run: make ci-apply-admin-identity RUNNER_MODE=host"}, "env: に置けるのは"},
		"env: の行末コメントの後の名前も読む": {[]string{"      - run: make ci-apply-admin-identity RUNNER_MODE=host", "        env: # c", "          TF_BIN: x"}, "env: に置けるのは"},
		"空行を挟んでも名前を読む":         {[]string{"      - run: make ci-apply-admin-identity RUNNER_MODE=host", "        env:", "          CI_PR: \"1\"", "", "          TF_BIN: x"}, "env: に置けるのは"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Contains(t, messages(checkStepEnv(stepOf(t, c.lines...), formatted)), c.want)
		})
	}
}

func Test_stepRunLine(t *testing.T) {
	t.Parallel()

	steps := workflow.SplitSteps(jobOf(t, applyWorkflow, "apply-admin-identity"))

	t.Run("run: の行を返す", func(t *testing.T) {
		t.Parallel()

		l, ok := stepRunLine(steps[0])
		require.True(t, ok)
		assert.Contains(t, l.Text, "run: head=")
	})

	t.Run("run: の無い step は false", func(t *testing.T) {
		t.Parallel()

		_, ok := stepRunLine(steps[1])
		assert.False(t, ok)
	})
}

var unlockWorkflow = strings.Join([]string{
	"name: Terraform Unlock",
	"",
	"on:",
	"  workflow_dispatch:",
	"",
	"permissions:",
	"  contents: read",
	"",
	"concurrency:",
	"  group: terraform-unlock-admin",
	"  cancel-in-progress: false",
	"",
	"jobs:",
	"  unlock-infra-code-gate:",
	"    runs-on: ubuntu-24.04",
	"    permissions:",
	"      contents: read",
	"    steps:",
	"      - run: make infra-code-gate RUNNER_MODE=host",
	"",
	"  unlock-admin-identity:",
	"    needs: unlock-infra-code-gate",
	"    if: ${{ needs.unlock-infra-code-gate.result == 'success' && github.ref_name == github.event.repository.default_branch }}",
	"    runs-on: ubuntu-24.04",
	"    environment: admin-unlock-identity",
	"    permissions:",
	"      contents: read",
	"      id-token: write",
	"    steps:",
	"      - name: Refuse stale commits",
	"        id: refuse-stale",
	"        run: head=$(git ls-remote \"https://github.com/$REPO.git\" \"refs/heads/$REF\") && test \"$SHA\" = \"$head\" || exit 1",
	"      - uses: actions/checkout@0000000000000000000000000000000000000000",
	"        with:",
	"          persist-credentials: false",
	"      - run: make ci-aws-credentials CI_ENV=admin CI_ROLE=unlock-identity RUNNER_MODE=host",
	"      - run: make ci-unlock-admin-identity RUNNER_MODE=host",
	"",
}, "\n")

const (
	unlockEntryLine     = "      - run: make ci-unlock-admin-identity RUNNER_MODE=host\n"
	applyEntryLine      = "      - run: make ci-apply-admin-identity RUNNER_MODE=host\n"
	unlockGroup         = "  group: terraform-unlock-admin\n"
	unlockEnv           = "    environment: admin-unlock-identity\n"
	unlockWfConcurrency = "concurrency:\n" + unlockGroup + "  cancel-in-progress: false\n"
)

func unlockWith(t *testing.T, from, to string) (workflow.Job, workflowInfo) {
	t.Helper()

	src := replaced(t, unlockWorkflow, from, to)

	return jobOf(t, src, "unlock-admin-identity"), infoOf(src)
}

func Test_runUnlock(t *testing.T) {
	t.Parallel()

	t.Run("plan・apply・lock の解除の job が規則を満たせば成功する", func(t *testing.T) {
		t.Parallel()

		wf, env := repoWith(t, map[string]string{deployGateFile: planWorkflow, "apply.yml": applyWorkflow, "unlock.yaml": unlockWorkflow}, envDecl)
		var out bytes.Buffer
		require.NoError(t, run(wf, env, rulesetOf(env), "", &out), out.String())
		assert.Contains(t, out.String(), "job 3 件")
	})

	sameGroup := strings.Replace(unlockWorkflow, unlockGroup, "  group: terraform-apply-admin\n", 1)
	for name, c := range map[string]struct {
		files map[string]string
		want  string
	}{
		"apply の workflow が無ければ違反":     {map[string]string{"unlock.yaml": unlockWorkflow}, "ちょうど1つではありません（0 件）"},
		"group が apply と同じなら違反":        {map[string]string{"apply.yml": applyWorkflow, "unlock.yaml": sameGroup}, "group と同じです"},
		"同じ root の apply が2つなら違反":      {map[string]string{"apply.yml": applyWorkflow, "apply2.yml": applyWorkflow, "unlock.yaml": unlockWorkflow}, "（2 件）"},
		"別の root の apply は相手に数えない":     {map[string]string{"apply.yml": strings.Replace(applyWorkflow, "ci-apply-admin-identity", "ci-apply-admin-workload", 1), "unlock.yaml": unlockWorkflow}, "make ci-apply-admin-identity を走らせる apply の job が、ちょうど1つではありません（0 件）"},
		"apply の group が式なら比べられないので違反": {map[string]string{"apply.yml": strings.Replace(applyWorkflow, "  group: terraform-apply-admin\n", "  group: ${{ github.workflow }}\n", 1), "unlock.yaml": unlockWorkflow}, "字面で読めません"},
		"lock の解除の group が式なら違反":       {map[string]string{"apply.yml": applyWorkflow, "unlock.yaml": strings.Replace(unlockWorkflow, unlockGroup, "  group: ${{ github.workflow }}\n", 1)}, "字面で書いて"},
		"両方がブロックスカラーの group なら、本文が違っても違うと読まない": {map[string]string{
			"apply.yml":   strings.Replace(applyWorkflow, "  group: terraform-apply-admin\n", "  group: >-\n    terraform-apply-admin\n", 1),
			"unlock.yaml": strings.Replace(unlockWorkflow, unlockGroup, "  group: >-\n    terraform-unlock\n", 1),
		}, "字面で書いて"},
		"apply だけがブロックスカラーの group なら違反": {map[string]string{
			"apply.yml":   strings.Replace(applyWorkflow, "  group: terraform-apply-admin\n", "  group: >-\n    terraform-apply-admin\n", 1),
			"unlock.yaml": unlockWorkflow,
		}, "字面で読めません"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			wf, env := repoWith(t, c.files, envDecl)
			var out bytes.Buffer
			require.ErrorIs(t, run(wf, env, rulesetOf(env), "", &out), errViolations)
			assert.Contains(t, out.String(), c.want)
		})
	}
}

func Test_checkJobUnlock(t *testing.T) {
	t.Parallel()

	t.Run("make ci-unlock-* を持つ job は lock の解除の job として検査する", func(t *testing.T) {
		t.Parallel()

		job := jobOf(t, unlockWorkflow, "unlock-admin-identity")
		assert.Empty(t, checkJob("u.yaml", job, planPerms, envs, infoOf(unlockWorkflow)))
	})

	t.Run("workload root の lock の解除の job は unlock の role で通る", func(t *testing.T) {
		t.Parallel()

		src := workloadUnlockWorkflow(t)
		assert.Empty(t, checkJob("u.yaml", jobOf(t, src, "unlock-admin-identity"), planPerms, envs, infoOf(src)))
	})

	t.Run("apply の入口と併せ持つ job は lock の解除の job として落とす", func(t *testing.T) {
		t.Parallel()

		job, w := unlockWith(t, unlockEntryLine, unlockEntryLine+"      - run: make ci-apply-admin-identity RUNNER_MODE=host\n")
		assert.Contains(t, messages(checkJob("u.yaml", job, planPerms, envs, w)), "他の CI の入口も走らせず")
	})

	for name, c := range map[string]struct{ role, env string }{
		"apply-identity の role の lock の解除の job は違反": {"apply-identity", "admin-apply-identity"},
		"plan の role の lock の解除の job は違反":           {"plan", "admin-plan"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			src := replaced(t, replaced(t, unlockWorkflow, "CI_ROLE=unlock-identity", "CI_ROLE="+c.role), "environment: admin-unlock-identity", "environment: "+c.env)
			got := messages(checkJob("u.yaml", jobOf(t, src, "unlock-admin-identity"), planPerms, envs, infoOf(src)))
			assert.Contains(t, got, "unlock-identity の role を取ってください（"+c.role+"）")
			assert.NotContains(t, got, "一致しません")
			assert.NotContains(t, got, "資格情報を取る job の run: は", "lock の解除の job の拒否の step は、role に関わらず refuse-stale で数える")
		})
	}
}

// workloadUnlockWorkflow は、unlockWorkflow の lock の解除の job を workload root へ向けたもの。job の id は変えない。
func workloadUnlockWorkflow(t *testing.T) string {
	t.Helper()

	src := replaced(t, unlockWorkflow, "make ci-unlock-admin-identity", "make ci-unlock-admin-workload")
	src = replaced(t, src, "CI_ROLE=unlock-identity", "CI_ROLE=unlock")

	return replaced(t, src, "environment: admin-unlock-identity", "environment: admin-unlock")
}

func Test_checkRole(t *testing.T) {
	t.Parallel()

	unlockJob := jobOf(t, unlockWorkflow, "unlock-admin-identity")
	applyJob := jobOf(t, applyWorkflow, "apply-admin-identity")
	planJob := jobOf(t, planWorkflow, "plan-admin-identity")
	workloadUnlockJob := jobOf(t, workloadUnlockWorkflow(t), "unlock-admin-identity")
	holdJob := jobOf(t, holdWorkflow, "hold-admin-identity")
	workloadHoldJob := jobOf(t, workloadHoldWorkflow(t), "hold-admin-identity")
	kindOf := func(j workflow.Job) string {
		switch {
		case isUnlock(j):
			return cientry.Unlock
		case isHold(j):
			return cientry.Hold
		case hasLine(j, planEntry):
			return cientry.Plan
		}
		return cientry.Apply
	}
	workloadApplyJob := jobOf(t, replaced(t, applyWorkflow, "make ci-apply-admin-identity", "make ci-apply-admin-workload"), "apply-admin-identity")
	workloadPlanJob := jobOf(t, replaced(t, planWorkflow, "make ci-plan-admin-identity", "make ci-plan-admin-workload"), "plan-admin-identity")
	otherEnvApplyJob := jobOf(t, replaced(t, applyWorkflow, "make ci-apply-admin-identity", "make ci-apply-stg-identity"), "apply-admin-identity")
	otherEnvUnlockJob := jobOf(t, replaced(t, unlockWorkflow, "make ci-unlock-admin-identity", "make ci-unlock-stg-identity"), "unlock-admin-identity")
	badRootApplyJob := jobOf(t, replaced(t, applyWorkflow, "make ci-apply-admin-identity", "make ci-apply-admin-other"), "apply-admin-identity")
	twoEntryApplyJob := jobOf(t, replaced(t, applyWorkflow, applyEntryLine, applyEntryLine+"      - run: make ci-apply-admin-workload RUNNER_MODE=host\n"), "apply-admin-identity")
	planLine := "      - run: make ci-plan-admin-identity RUNNER_MODE=host\n"
	otherEnvPlanJob := jobOf(t, replaced(t, planWorkflow, "make ci-plan-admin-identity", "make ci-plan-stg-workload"), "plan-admin-identity")
	otherEnvHoldJob := jobOf(t, replaced(t, holdWorkflow, "make ci-hold-admin-identity", "make ci-hold-stg-identity"), "hold-admin-identity")
	badRootPlanJob := jobOf(t, replaced(t, planWorkflow, "make ci-plan-admin-identity", "make ci-plan-admin-other"), "plan-admin-identity")
	badRootUnlockJob := jobOf(t, replaced(t, unlockWorkflow, "make ci-unlock-admin-identity", "make ci-unlock-admin-other"), "unlock-admin-identity")
	badRootHoldJob := jobOf(t, replaced(t, holdWorkflow, "make ci-hold-admin-identity", "make ci-hold-admin-other"), "hold-admin-identity")
	// 先頭の入口は role に合わない root にし、比べれば違反になる形で「比べない」を見る。
	twoEntryPlanJob := jobOf(t, replaced(t, planWorkflow, planLine, "      - run: make ci-plan-stg-workload RUNNER_MODE=host\n"+planLine), "plan-admin-identity")
	twoEntryUnlockJob := jobOf(t, replaced(t, unlockWorkflow, unlockEntryLine, "      - run: make ci-unlock-admin-workload RUNNER_MODE=host\n"+unlockEntryLine), "unlock-admin-identity")
	twoEntryHoldJob := jobOf(t, replaced(t, holdWorkflow, holdEntryLine, "      - run: make ci-hold-admin-workload RUNNER_MODE=host\n"+holdEntryLine), "hold-admin-identity")
	logArchiveJob := jobOf(t, replaced(t, replaced(t, applyWorkflow, "make ci-apply-admin-identity", "make ci-apply-log-archive-workload"), "CI_ENV=admin CI_ROLE=apply-identity", "CI_ENV=log-archive CI_ROLE=apply"), "apply-admin-identity")
	for name, c := range map[string]struct {
		job  workflow.Job
		role string
		want string
	}{
		"workload の apply の job の apply は違反0件":           {workloadApplyJob, "apply", ""},
		"workload の apply の job の apply-identity は違反":    {workloadApplyJob, "apply-identity", "apply の job は apply の role を取って"},
		"identity の apply の job の apply は違反":             {applyJob, "apply", "apply の job は apply-identity の role を取って"},
		"workload の plan の job の plan は違反0件":             {workloadPlanJob, "plan", ""},
		"入口の env と資格情報の CI_ENV が違えば違反":                   {otherEnvApplyJob, "apply-identity", "資格情報の CI_ENV=admin が、入口 make ci-apply-stg-identity の env stg と違います"},
		"lock の解除の入口の env と資格情報の CI_ENV が違えば違反":          {otherEnvUnlockJob, "unlock-identity", "資格情報の CI_ENV=admin が、入口 make ci-unlock-stg-identity の env stg と違います"},
		"root の接尾辞を読めない入口は違反":                            {badRootApplyJob, "apply-identity", "入口 make ci-apply-admin-other の root を読めません"},
		"入口が2つの apply の job は role と env を比べない":          {twoEntryApplyJob, "apply", ""},
		"入口が2つの plan の job は role と env を比べない":           {twoEntryPlanJob, "plan", ""},
		"入口が2つの lock の解除の job は role と env を比べない":        {twoEntryUnlockJob, "unlock-identity", ""},
		"入口が2つの lock を残す job は role と env を比べない":         {twoEntryHoldJob, "apply-identity", ""},
		"plan の入口の env と資格情報の CI_ENV が違えば違反":             {otherEnvPlanJob, "plan", "資格情報の CI_ENV=admin が、入口 make ci-plan-stg-workload の env stg と違います"},
		"lock を残す入口の env と資格情報の CI_ENV が違えば違反":           {otherEnvHoldJob, "apply-identity", "資格情報の CI_ENV=admin が、入口 make ci-hold-stg-identity の env stg と違います"},
		"plan の job の apply-identity は違反":                {planJob, "apply-identity", "plan の job は plan の role を取って"},
		"workload の plan の job の apply は違反":              {workloadPlanJob, "apply", "plan の job は plan の role を取って"},
		"root の接尾辞を読めない plan の入口は違反":                     {badRootPlanJob, "plan", "入口 make ci-plan-admin-other の root を読めません"},
		"root の接尾辞を読めない lock の解除の入口は違反":                  {badRootUnlockJob, "unlock-identity", "入口 make ci-unlock-admin-other の root を読めません"},
		"root の接尾辞を読めない lock を残す入口は違反":                   {badRootHoldJob, "apply-identity", "入口 make ci-hold-admin-other の root を読めません"},
		"ハイフンを含む env の資格情報は入口の env と一致する":                {logArchiveJob, "apply", ""},
		"lock の解除の job の unlock-identity は違反0件":          {unlockJob, "unlock-identity", ""},
		"lock の解除の job の apply-identity は違反":             {unlockJob, "apply-identity", "lock の解除の job は unlock-identity の role を取って"},
		"lock の解除の job の apply は違反":                      {unlockJob, "apply", "lock の解除の job は unlock-identity の role を取って"},
		"lock の解除の job の plan は違反":                       {unlockJob, "plan", "lock の解除の job は unlock-identity の role を取って"},
		"apply の job の apply-identity は違反0件":             {applyJob, "apply-identity", ""},
		"apply の job の unlock-identity は違反":              {applyJob, "unlock-identity", "lock の解除の job（make ci-unlock-*）だけが取って"},
		"plan の job の plan は違反0件":                        {planJob, "plan", ""},
		"plan の job の unlock-identity は違反":               {planJob, "unlock-identity", "lock の解除の job（make ci-unlock-*）だけが取って"},
		"identity の lock の解除の job の unlock は違反":          {unlockJob, "unlock", "lock の解除の job は unlock-identity の role を取って"},
		"workload の lock の解除の job の unlock は違反0件":        {workloadUnlockJob, "unlock", ""},
		"workload の lock の解除の job の unlock-identity は違反": {workloadUnlockJob, "unlock-identity", "lock の解除の job は unlock の role を取って"},
		"apply の job の unlock は違反":                       {applyJob, "unlock", "unlock の role は lock の解除の job（make ci-unlock-*）だけが取って"},
		"plan の job の unlock は違反":                        {planJob, "unlock", "unlock の role は lock の解除の job（make ci-unlock-*）だけが取って"},
		"lock を残す job の apply-identity は違反0件":            {holdJob, "apply-identity", ""},
		"lock を残す job の apply は違反":                       {holdJob, "apply", "lock を残す job は apply-identity の role を取って"},
		"lock を残す job の plan は違反":                        {holdJob, "plan", "lock を残す job は apply-identity の role を取って"},
		"lock を残す job の unlock-identity は違反":             {holdJob, "unlock-identity", "lock を残す job は apply-identity の role を取って"},
		"workload の lock を残す job の apply は違反0件":          {workloadHoldJob, "apply", ""},
		"workload の lock を残す job の apply-identity は違反":   {workloadHoldJob, "apply-identity", "lock を残す job は apply の role を取って"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := messages(checkRole(c.job, credOf(c.job), c.role, kindOf(c.job), formatted))
			if c.want == "" {
				assert.Empty(t, got)

				return
			}
			assert.Contains(t, got, c.want)
		})
	}

	for name, c := range map[string]struct {
		job  workflow.Job
		role string
		n    int
	}{
		"apply の job の unlock-identity は1件だけ違反": {applyJob, "unlock-identity", 1},
		"apply の job の unlock は1件だけ違反":          {applyJob, "unlock", 1},
		"role と env の両方が違えば2件の違反":               {otherEnvApplyJob, "apply", 2},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := checkRole(c.job, credOf(c.job), c.role, kindOf(c.job), formatted)
			assert.Len(t, got, c.n, messages(got))
		})
	}

	t.Run("plan の job の lock の解除の role は1件だけ違反", func(t *testing.T) {
		t.Parallel()

		got := checkRole(planJob, credOf(planJob), "unlock-identity", cientry.Plan, formatted)
		require.Len(t, got, 1, messages(got))
		assert.Contains(t, got[0].Message, "lock の解除の job（make ci-unlock-*）だけが取って")
	})
}

func Test_checkUnlock(t *testing.T) {
	t.Parallel()

	t.Run("規則を満たせば違反0件", func(t *testing.T) {
		t.Parallel()

		job := jobOf(t, unlockWorkflow, "unlock-admin-identity")
		assert.Empty(t, checkUnlock(job, credOf(job), infoOf(unlockWorkflow), formatted))
	})

	for name, c := range map[string]struct{ from, to, want string }{
		"push の起動は違反":                    {"  workflow_dispatch:\n", "  workflow_dispatch:\n  push:\n    branches: ['release/*']\n", "workflow_dispatch だけ"},
		"workflow_dispatch が無ければ違反":      {"  workflow_dispatch:\n", "  push:\n", "workflow_dispatch だけ"},
		"起動条件を読めなければ違反":                  {"on:\n  workflow_dispatch:\n", "", "workflow_dispatch だけ"},
		"inputs は違反":                     {"  workflow_dispatch:\n", "  workflow_dispatch:\n    inputs:\n      ref:\n        type: string\n", "inputs を置かないで"},
		"フロー形式の起動条件は違反":                  {"  workflow_dispatch:\n", "  workflow_dispatch: {inputs: {ref: {}}}\n", "フロー形式で書かないで"},
		"concurrency が無ければ違反":            {unlockWfConcurrency, "", "cancel-in-progress: false の concurrency"},
		"実行中を取り消す concurrency は違反":       {"  cancel-in-progress: false\n", "  cancel-in-progress: true\n", "cancel-in-progress: false の concurrency"},
		"refuse-stale が無ければ違反":           {"        id: refuse-stale\n", "", "refuse-stale の step"},
		"同じ workflow の apply の job は違反":  {"jobs:\n", "jobs:\n  apply:\n    steps:\n      - run: make ci-apply-admin-identity RUNNER_MODE=host\n", "plan と apply と lock を残す job（apply）を置かないで"},
		"同じ workflow の plan の job は違反":   {"jobs:\n", "jobs:\n  plan:\n    steps:\n      - run: make ci-plan-admin-identity RUNNER_MODE=host\n", "plan と apply と lock を残す job（plan）を置かないで"},
		"同じ workflow の lock を残す job は違反": {"jobs:\n", "jobs:\n  hold:\n    steps:\n      - run: make ci-hold-admin-identity RUNNER_MODE=host\n", "plan と apply と lock を残す job（hold）を置かないで"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			job, w := unlockWith(t, c.from, c.to)
			assert.Contains(t, messages(checkUnlock(job, credOf(job), w, formatted)), c.want)
		})
	}
}

func Test_checkUnlockEntry(t *testing.T) {
	t.Parallel()

	t.Run("入口を1回だけ走らせれば違反0件", func(t *testing.T) {
		t.Parallel()

		assert.Empty(t, checkUnlockEntry(jobOf(t, unlockWorkflow, "unlock-admin-identity"), formatted))
	})

	for name, c := range map[string]struct{ add, want string }{
		"入口を2回走らせるのは違反":       {unlockEntryLine, "1回だけ走らせてください（2 回）"},
		"terraform の直接の起動は違反": {"      - run: terraform -chdir=env/admin/identity force-unlock -force x\n", "terraform も他の CI の入口も"},
		"apply の入口は違反":        {"      - run: make ci-apply-admin-identity RUNNER_MODE=host\n", "terraform も他の CI の入口も"},
		"local.mk の入口は違反":     {"      - run: make init-admin-identity RUNNER_MODE=host\n", "terraform も他の CI の入口も"},
		"|| true で失敗を隠す入口は違反": {"      - run: make ci-unlock-admin-identity RUNNER_MODE=host || true\n", "terraform も他の CI の入口も"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			job, _ := unlockWith(t, unlockEntryLine, unlockEntryLine+c.add)
			assert.Contains(t, messages(checkUnlockEntry(job, formatted)), c.want)
		})
	}

	t.Run("入口が無ければ違反", func(t *testing.T) {
		t.Parallel()

		job, _ := unlockWith(t, unlockEntryLine, "")
		assert.Contains(t, messages(checkUnlockEntry(job, formatted)), "（0 回）")
	})

	t.Run("terraform を入れる行と name: は起動と数えない", func(t *testing.T) {
		t.Parallel()

		job, _ := unlockWith(t, unlockEntryLine, "      - name: make ci-unlock-admin-identity terraform\n        run: make ci-unlock-admin-identity RUNNER_MODE=host\n      - uses: ./.github/actions/setup-mise\n        with:\n          tools: aqua:hashicorp/terraform\n")
		assert.Empty(t, checkUnlockEntry(job, formatted))
	})

	t.Run("terraform を入れる字面を添えた run: の起動は違反", func(t *testing.T) {
		t.Parallel()

		job, _ := unlockWith(t, unlockEntryLine, "      - run: terraform force-unlock -force x # aqua:hashicorp/terraform\n"+unlockEntryLine)
		assert.Contains(t, messages(checkUnlockEntry(job, formatted)), "terraform も他の CI の入口も")
	})
}

func Test_checkTriggerForm(t *testing.T) {
	t.Parallel()

	t.Run("ブロック形式で inputs が無ければ違反0件", func(t *testing.T) {
		t.Parallel()

		assert.Empty(t, checkTriggerForm(infoOf(unlockWorkflow), "lock の解除", formatted))
	})

	t.Run("違反の文に job の種別を出す", func(t *testing.T) {
		t.Parallel()

		_, w := unlockWith(t, "  workflow_dispatch:\n", "  workflow_dispatch:\n    inputs:\n")
		assert.Contains(t, messages(checkTriggerForm(w, "lock の解除", formatted)), "lock の解除 の workflow_dispatch に inputs")
	})

	t.Run("push のフロー形式も違反", func(t *testing.T) {
		t.Parallel()

		w := infoOf(replaced(t, applyWorkflow, "  push:\n    branches: ['release/*']\n", "  push: {paths: [a]}\n"))
		assert.Contains(t, messages(checkTriggerForm(w, "apply", formatted)), "apply の workflow の起動条件をフロー形式で")
	})
}

func Test_checkNoCancel(t *testing.T) {
	t.Parallel()

	job := jobOf(t, unlockWorkflow, "unlock-admin-identity")

	t.Run("workflow 全体が cancel-in-progress: false なら違反0件", func(t *testing.T) {
		t.Parallel()

		assert.Empty(t, checkNoCancel(job, infoOf(unlockWorkflow), "lock の解除", formatted))
	})

	t.Run("job の concurrency だけでも cancel-in-progress: false なら違反0件", func(t *testing.T) {
		t.Parallel()

		_, w := unlockWith(t, unlockWfConcurrency, "")
		j := jobOf(t, replaced(t, replaced(t, unlockWorkflow, unlockWfConcurrency, ""), unlockEnv, unlockEnv+jobConcurrency("false")), "unlock-admin-identity")
		assert.Empty(t, checkNoCancel(j, w, "lock の解除", formatted))
	})

	t.Run("job の concurrency が取り消すなら、workflow 全体が false でも違反", func(t *testing.T) {
		t.Parallel()

		j := jobOf(t, replaced(t, unlockWorkflow, unlockEnv, unlockEnv+jobConcurrency("true")), "unlock-admin-identity")
		assert.Contains(t, messages(checkNoCancel(j, infoOf(unlockWorkflow), "lock の解除", formatted)), "lock の解除 の job か workflow 全体に")
	})

	t.Run("どちらにも無ければ違反", func(t *testing.T) {
		t.Parallel()

		j, w := unlockWith(t, unlockWfConcurrency, "")
		assert.Contains(t, messages(checkNoCancel(j, w, "lock の解除", formatted)), "lock の解除 の job か workflow 全体に cancel-in-progress: false の concurrency")
	})

	t.Run("フロー形式の concurrency は読めないとして違反", func(t *testing.T) {
		t.Parallel()

		j, w := unlockWith(t, unlockWfConcurrency, "concurrency: {group: terraform-unlock-admin, cancel-in-progress: false}\n")
		assert.Contains(t, messages(checkNoCancel(j, w, "lock の解除", formatted)), "cancel-in-progress: false の concurrency")
	})
}

func Test_groupRecords(t *testing.T) {
	t.Parallel()

	t.Run("lock の解除の job と apply の job を入口の root で取り出し、plan の job と検出の job は取り出さない", func(t *testing.T) {
		t.Parallel()

		got := map[string]groupRecord{}
		for file, src := range map[string]string{"p.yaml": planWorkflow, "a.yml": applyWorkflow, "u.yaml": unlockWorkflow, "h.yaml": holdWorkflow} {
			for _, r := range groupRecords(file, src) {
				got[r.file] = r
			}
		}
		require.Len(t, got, 3)
		assert.Equal(t, groupRecord{file: "h.yaml", job: jobOf(t, holdWorkflow, "hold-admin-identity"), root: "admin-identity", group: "terraform-apply-admin", kind: cientry.Hold, environment: "admin-apply-identity"}, got["h.yaml"])
		assert.Equal(t, groupRecord{file: "a.yml", job: jobOf(t, applyWorkflow, "apply-admin-identity"), root: "admin-identity", group: "terraform-apply-admin", kind: cientry.Apply, environment: "admin-apply-identity"}, got["a.yml"])
		assert.Equal(t, groupRecord{file: "u.yaml", job: jobOf(t, unlockWorkflow, "unlock-admin-identity"), root: "admin-identity", group: "terraform-unlock-admin", kind: cientry.Unlock, environment: "admin-unlock-identity"}, got["u.yaml"])
	})

	t.Run("lock の解除と lock を残す入口を併せ持つ job は lock の解除の job として取り出す", func(t *testing.T) {
		t.Parallel()

		src := replaced(t, holdWorkflow, holdEntryLine, holdEntryLine+"      - run: make ci-unlock-admin-identity RUNNER_MODE=host\n")
		got := groupRecords("h.yaml", src)
		require.Len(t, got, 1)
		assert.Equal(t, cientry.Unlock, got[0].kind)
	})

	t.Run("id-token を持たない job は取り出さない", func(t *testing.T) {
		t.Parallel()

		src := replaced(t, unlockWorkflow, "      id-token: write\n", "")
		assert.Empty(t, groupRecords("u.yaml", src))
	})

	t.Run("CI の入口を持たない job は取り出さない", func(t *testing.T) {
		t.Parallel()

		src := replaced(t, unlockWorkflow, unlockEntryLine, "")
		assert.Empty(t, groupRecords("u.yaml", src))
	})
}

func Test_entryRoot(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		lines []string
		re    *regexp.Regexp
		want  string
	}{
		"lock の解除の入口の root":    {[]string{"      - run: make ci-unlock-admin-identity RUNNER_MODE=host"}, unlockEntry, "admin-identity"},
		"apply の入口の root":      {[]string{"      - run: make ci-apply-admin-identity RUNNER_MODE=host"}, applyEntry, "admin-identity"},
		"最初の入口を返す":             {[]string{"      - run: make ci-apply-admin-a", "      - run: make ci-apply-admin-b"}, applyEntry, "admin-a"},
		"コメントと name: の入口は読まない": {[]string{"      # make ci-apply-admin-a", "      - name: make ci-apply-admin-b", "      - run: make ci-apply-admin-c"}, applyEntry, "admin-c"},
		"別の種類の入口は読まない":         {[]string{"      - run: make ci-unlock-admin-identity"}, applyEntry, ""},
		"入口が無ければ空":             {[]string{"      - run: echo"}, unlockEntry, ""},
		"行が無ければ空":              {nil, unlockEntry, ""},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, c.want, entryRoot(workflow.Job{Lines: lines(c.lines...)}, c.re))
		})
	}
}

func unlockRecord(file, root, group string, unlock bool) groupRecord {
	kind := cientry.Apply
	if unlock {
		kind = cientry.Unlock
	}

	return groupRecord{file: file, job: workflow.Job{ID: "j", Number: 1}, root: root, group: group, kind: kind}
}

func Test_checkUnlockIsolation(t *testing.T) {
	t.Parallel()

	const root = "admin-identity"

	t.Run("違反は lock の解除の job の行に、job の id を添えて出す", func(t *testing.T) {
		t.Parallel()

		got := checkUnlockIsolation([]groupRecord{unlockRecord("a", root, "g", false), unlockRecord("u", root, "g", true)})
		require.Len(t, got, 1)
		assert.Equal(t, "u", got[0].File)
		assert.Equal(t, 1, got[0].Line)
		assert.True(t, strings.HasPrefix(got[0].Message, "job j: "), got[0].Message)
	})

	for name, c := range map[string]struct {
		records []groupRecord
		want    int
	}{
		"group が違えば違反0件":          {[]groupRecord{unlockRecord("a", root, "g", false), unlockRecord("u", root, "h", true)}, 0},
		"lock の解除の job が無ければ違反0件": {[]groupRecord{unlockRecord("a", root, "g", false)}, 0},
		"記録が無ければ違反0件":             {nil, 0},
		"lock の解除の job ごとに1件出す":   {[]groupRecord{unlockRecord("a", root, "g", false), unlockRecord("u", root, "g", true), unlockRecord("v", root, "g", true)}, 2},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Len(t, checkUnlockIsolation(c.records), c.want)
		})
	}
}

func Test_isolationProblem(t *testing.T) {
	t.Parallel()

	const root = "admin-identity"
	for name, c := range map[string]struct {
		records []groupRecord
		want    string
	}{
		"group が違えば空":                 {[]groupRecord{unlockRecord("a", root, "g", false)}, ""},
		"同じ workflow の apply も相手に数える": {[]groupRecord{unlockRecord("u", root, "g", false)}, ""},
		"group が同じなら違反":               {[]groupRecord{unlockRecord("a", root, "h", false)}, `group "h" が、apply の job j（a）の group と同じです`},
		"apply の job が無ければ違反":         {nil, "make ci-apply-admin-identity を走らせる apply の job が、ちょうど1つではありません（0 件）"},
		"別の root の apply は数えない":       {[]groupRecord{unlockRecord("a", "admin-workload", "g", false)}, "（0 件）"},
		"lock の解除の job は相手に数えない":      {[]groupRecord{unlockRecord("v", root, "g", true)}, "（0 件）"},
		"lock を残す job は相手に数えない":       {[]groupRecord{holdRecord("h", root, "g", cientry.Hold, "")}, "（0 件）"},
		"apply の job が2つなら違反":         {[]groupRecord{unlockRecord("a", root, "g", false), unlockRecord("b", root, "g", false)}, "（2 件）"},
		"apply の group が式なら違反":        {[]groupRecord{unlockRecord("a", root, "${{ github.workflow }}", false)}, "apply の job j（a）の concurrency の group を字面で読めません"},
		"apply の group を読めなければ違反":     {[]groupRecord{unlockRecord("a", root, "", false)}, "字面で読めません"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, c.want == "", isolationProblem(unlockRecord("u", root, "h", true), c.records) == "")
			if c.want != "" {
				assert.Contains(t, isolationProblem(unlockRecord("u", root, "h", true), c.records), c.want)
			}
		})
	}

	for name, group := range map[string]string{"式の group は違反": "${{ github.workflow }}", "group が無ければ違反": ""} {
		t.Run("lock の解除の "+name, func(t *testing.T) {
			t.Parallel()

			got := isolationProblem(unlockRecord("u", root, group, true), []groupRecord{unlockRecord("a", root, group, false)})
			assert.Contains(t, got, "lock の解除の job の concurrency の group を字面で書いてください")
		})
	}
}

func Test_isLiteral(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		in   string
		want bool
	}{
		"字面の値":      {"terraform-unlock-admin", true},
		"空は読めなかった値": {"", false},
		"式":         {"${{ github.workflow }}", false},
		"式を含む値":     {"terraform-${{ github.ref }}", false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, c.want, isLiteral(c.in))
		})
	}
}

func Test_effectiveGroup(t *testing.T) {
	t.Parallel()

	t.Run("job の concurrency が workflow 全体より優先する", func(t *testing.T) {
		t.Parallel()

		src := replaced(t, unlockWorkflow, unlockEnv, unlockEnv+jobConcurrency("false"))
		assert.Equal(t, "x", effectiveGroup(jobOf(t, src, "unlock-admin-identity"), workflow.SplitJobs(src).Preamble))
	})

	t.Run("job に無ければ workflow 全体の group", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, "terraform-unlock-admin", effectiveGroup(jobOf(t, unlockWorkflow, "unlock-admin-identity"), workflow.SplitJobs(unlockWorkflow).Preamble))
	})
}

func Test_concurrencyGroup(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		lines  []string
		indent string
		want   string
	}{
		"ブロック形式の group":               {[]string{"concurrency:", "  group: g", "  cancel-in-progress: false"}, "", "g"},
		"引用符を外す":                      {[]string{"concurrency:", "  group: 'g'"}, "", "g"},
		"1行の形はその値":                    {[]string{"concurrency: g # c"}, "", "g"},
		"job の桁":                      {[]string{"    concurrency:", "      group: \"g\""}, "    ", "g"},
		"別の桁の group は読まない":            {[]string{"concurrency:", "    group: g"}, "", ""},
		"group が無ければ空":                {[]string{"concurrency:", "  cancel-in-progress: false"}, "", ""},
		"concurrency が無ければ空":          {[]string{"group: g"}, "", ""},
		"concurrency の外の group は読まない": {[]string{"concurrency:", "  cancel-in-progress: false", "env:", "  group: g"}, "", ""},
		"間の空行とコメントを飛ばす":               {[]string{"concurrency:", "  # c", "", "  group: g"}, "", "g"},
		"引用符付きの1行の値":                  {[]string{`concurrency: "g"`}, "", "g"},
		"行末コメントの見出し":                  {[]string{"concurrency: # c", "  group: g"}, "", "g"},
		"引用符付きのキー":                    {[]string{`"concurrency":`, `  'group': g`}, "", "g"},
		"ブロックスカラーの group は読めない":       {[]string{"concurrency:", "  group: >-", "    terraform-apply-admin"}, "", ""},
		"1行のブロックスカラーも読めない":            {[]string{"concurrency: >-", "  g"}, "", ""},
		"フロー形式は group として読まない":        {[]string{"concurrency: {group: g, cancel-in-progress: false}"}, "", ""},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, c.want, concurrencyGroup(lines(c.lines...), c.indent))
		})
	}
}

func Test_isUnlock(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		job  workflow.Job
		want bool
	}{
		"make ci-unlock-* を走らせる job":          {jobOf(t, unlockWorkflow, "unlock-admin-identity"), true},
		"apply の job":                         {jobOf(t, applyWorkflow, "apply-admin-identity"), false},
		"コメントと name: の make ci-unlock- は数えない": {workflow.Job{Lines: lines("      # make ci-unlock-admin-identity", "      - name: make ci-unlock-admin-identity")}, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, c.want, isUnlock(c.job))
		})
	}
}

func Test_hasLine(t *testing.T) {
	t.Parallel()

	job := workflow.Job{Lines: lines("      # make ci-apply-x", "      - name: make ci-apply-x", "      - run: make ci-plan-x")}

	t.Run("コメントと name: の行は数えない", func(t *testing.T) {
		t.Parallel()

		assert.False(t, hasLine(job, applyEntry))
	})

	t.Run("run: の行は数える", func(t *testing.T) {
		t.Parallel()

		assert.True(t, hasLine(job, runEntry))
	})
}

func Test_literalGroup(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct{ in, want string }{
		"字面の値":   {"terraform-apply-admin", "terraform-apply-admin"},
		"引用符を外す": {`"g"`, "g"},
		"ブロックスカラーの > は読めない": {">-", ""},
		"ブロックスカラーの | は読めない": {"|", ""},
		"フロー形式は読めない":        {"{group: g}", ""},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, c.want, literalGroup(c.in))
		})
	}
}

func Test_checkRequiredGateJobs(t *testing.T) {
	t.Parallel()

	required := map[string]bool{"tf-lint": true}
	const (
		check = "        run: make resource-order-check RUNNER_MODE=host\n"
	)

	t.Run("必須検査の job が両方を走らせれば1件で違反0件", func(t *testing.T) {
		t.Parallel()

		n, f := checkRequiredGateJobs("g.yaml", gatesWorkflow, required, orderGate)
		assert.Equal(t, 1, n)
		assert.Empty(t, f)
	})

	t.Run("層の検査も同じ job で数える", func(t *testing.T) {
		t.Parallel()

		n, f := checkRequiredGateJobs("g.yaml", gatesWorkflow, required, layerGate)
		assert.Equal(t, 1, n)
		assert.Empty(t, f)
	})

	t.Run("root の検査も同じ job で数える", func(t *testing.T) {
		t.Parallel()

		n, f := checkRequiredGateJobs("g.yaml", gatesWorkflow, required, rootGate)
		assert.Equal(t, 1, n)
		assert.Empty(t, f)
	})

	const (
		rootCheckStep = "        run: make root-check RUNNER_MODE=host\n"
		rootSelfStep  = "        run: make root-check-test RUNNER_MODE=host\n"
	)
	for name, src := range map[string]string{
		"root の検査そのものが無い":             strings.Replace(gatesWorkflow, rootCheckStep, "        run: echo\n", 1),
		"root の検査: required でない job":  strings.Replace(gatesWorkflow, "  tf-lint:\n", "  other:\n", 1),
		"root の検査: 変数を足した make は数えない": strings.Replace(gatesWorkflow, rootCheckStep, "        run: make root-check RUNNER_MODE=host RT_NS=x\n", 1),
		"root の検査: コメントの中は数えない":       strings.Replace(gatesWorkflow, rootCheckStep, "        # run: make root-check RUNNER_MODE=host\n        run: echo\n", 1),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			require.NotEqual(t, gatesWorkflow, src, "置き換え元が見つからない")
			n, f := checkRequiredGateJobs("g.yaml", src, required, rootGate)
			assert.Zero(t, n)
			assert.Empty(t, f)
		})
	}

	t.Run("root の検査の step の形の違反を返す", func(t *testing.T) {
		t.Parallel()

		src := replaced(t, gatesWorkflow, rootCheckStep, rootCheckStep+"        if: false\n")
		_, f := checkRequiredGateJobs("g.yaml", src, required, rootGate)
		assert.Contains(t, messages(f), "root の検査の step に if:")
	})

	const layerCheck = "        run: make layer-check RUNNER_MODE=host\n"
	for name, src := range map[string]string{
		"層の検査そのものが無い":             strings.Replace(gatesWorkflow, layerCheck, "        run: echo\n", 1),
		"層の検査: required でない job":  strings.Replace(gatesWorkflow, "  tf-lint:\n", "  other:\n", 1),
		"層の検査: 変数を足した make は数えない": strings.Replace(gatesWorkflow, layerCheck, "        run: make layer-check RUNNER_MODE=host LY_NS=x\n", 1),
		"層の検査: コメントの中は数えない":       strings.Replace(gatesWorkflow, layerCheck, "        # run: make layer-check RUNNER_MODE=host\n        run: echo\n", 1),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			require.NotEqual(t, gatesWorkflow, src, "置き換え元が見つからない")
			n, f := checkRequiredGateJobs("g.yaml", src, required, layerGate)
			assert.Zero(t, n)
			assert.Empty(t, f)
		})
	}

	t.Run("層の検査の step の形の違反を返す", func(t *testing.T) {
		t.Parallel()

		src := replaced(t, gatesWorkflow, "        run: make layer-check RUNNER_MODE=host\n", "        run: make layer-check RUNNER_MODE=host\n        if: false\n")
		_, f := checkRequiredGateJobs("g.yaml", src, required, layerGate)
		assert.Contains(t, messages(f), "層の検査の step に if:")
	})

	for name, src := range map[string]string{
		"検査そのものが無い":           strings.Replace(gatesWorkflow, check, "        run: echo\n", 1),
		"required でない job":    strings.Replace(gatesWorkflow, "  tf-lint:\n", "  other:\n", 1),
		"pull_request で起動しない": strings.Replace(gatesWorkflow, "  pull_request:\n", "  push:\n", 1),
		"変数を足した make は数えない":   strings.Replace(gatesWorkflow, check, "        run: make resource-order-check RUNNER_MODE=host RO_NS=x\n", 1),
		"コメントの中は数えない":         strings.Replace(gatesWorkflow, check, "        # run: make resource-order-check RUNNER_MODE=host\n        run: echo\n", 1),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			require.NotEqual(t, gatesWorkflow, src, "置き換え元が見つからない")
			n, f := checkRequiredGateJobs("g.yaml", src, required, orderGate)
			assert.Zero(t, n)
			assert.Empty(t, f)
		})
	}

	t.Run("数えた job の形の違反を返す", func(t *testing.T) {
		t.Parallel()

		src := replaced(t, gatesWorkflow, check, check+"        continue-on-error: true\n")
		n, f := checkRequiredGateJobs("g.yaml", src, required, orderGate)
		assert.Equal(t, 1, n)
		assert.Contains(t, messages(f), "順序の検査の step に if:・continue-on-error:")
	})
}

func Test_checkRequiredGateJob(t *testing.T) {
	t.Parallel()

	const (
		check  = "        run: make resource-order-check RUNNER_MODE=host\n"
		jobIf  = "    if: ${{ !cancelled() && needs.changes.outputs.relevant != 'false' }}\n"
		header = "  tf-lint:\n    needs: changes\n"
	)
	job := func(t *testing.T, src string) (workflow.Job, []workflow.Line) {
		t.Helper()

		split := workflow.SplitJobs(src)

		return jobOf(t, src, "tf-lint"), split.Preamble
	}

	t.Run("terraform-gates.yaml の tf-lint の形は違反0件", func(t *testing.T) {
		t.Parallel()

		j, pre := job(t, gatesWorkflow)
		assert.Empty(t, checkRequiredGateJob("g.yaml", j, pre, orderGate))
	})

	t.Run("job の if: が無くても違反0件", func(t *testing.T) {
		t.Parallel()

		j, pre := job(t, replaced(t, gatesWorkflow, jobIf, ""))
		assert.Empty(t, checkRequiredGateJob("g.yaml", j, pre, orderGate))
	})

	t.Run("ブロックの if: でも同じ式なら違反0件", func(t *testing.T) {
		t.Parallel()

		j, pre := job(t, replaced(t, gatesWorkflow, jobIf, "    if: >-\n      !cancelled() &&\n      needs.changes.outputs.relevant != 'false'\n"))
		assert.Empty(t, checkRequiredGateJob("g.yaml", j, pre, orderGate))
	})

	for name, c := range map[string]struct{ from, to, want string }{
		"検査の step の continue-on-error: は違反": {check, check + "        continue-on-error: true\n", "順序の検査の step に if:・continue-on-error:"},
		"検査の step の if: false は違反":          {check, check + "        if: false\n", "順序の検査の step に if:"},
		"検査の step の shell: は違反":             {check, check + "        shell: sh -c true {0}\n", "shell:"},
		"検査の step の working-directory: は違反": {check, check + "        working-directory: /tmp\n", "working-directory:"},
		"job の if: false は違反":               {jobIf, "    if: false\n", "if: は ${{ !cancelled() && needs.changes.outputs.relevant != 'false' }} だけ"},
		"job の if: ${{ false }} は違反":        {jobIf, "    if: ${{ false }}\n", "if: は ${{"},
		"job の if: always() を足せば違反":         {jobIf, "    if: ${{ always() && !cancelled() && needs.changes.outputs.relevant != 'false' }}\n", "if: は ${{"},
		"job の if: always() だけは違反":          {jobIf, "    if: ${{ always() }}\n", "if: は ${{"},
		"job の if: の || は違反":                {jobIf, "    if: ${{ !cancelled() && needs.changes.outputs.relevant != 'false' || false }}\n", "if: は ${{"},
		"job の if: の否定の差し替えは違反":             {jobIf, "    if: ${{ !cancelled() && !(needs.changes.outputs.relevant != 'false') }}\n", "if: は ${{"},
		"job の if: の比較の差し替えは違反":             {jobIf, "    if: ${{ !cancelled() && needs.changes.outputs.relevant == 'false' }}\n", "if: は ${{"},
		"job の continue-on-error: は違反":      {header, header + "    continue-on-error: true\n", "必須検査の job に continue-on-error:"},
		"job の env: は違反":                    {header, header + "    env:\n      X: y\n", "必須検査の job に env:"},
		"job の container: は違反":              {header, header + "    container: alpine\n", "必須検査の job に container:"},
		"workflow の env: は違反":               {"permissions:\n  contents: read\n", "permissions:\n  contents: read\nenv:\n  X: y\n", "workflow に defaults: と env:"},
		"MAKEFLAGS は違反":                     {check, check + "        env:\n          MAKEFLAGS: -n\n", "MAKEFLAGS と MAKEFILES"},
		"GITHUB_ENV への書き込みは違反":              {"        run: make tf-lint RUNNER_MODE=host\n", "        run: echo X=y >> \"$GITHUB_ENV\"\n", "GITHUB_PATH と GITHUB_ENV"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			j, pre := job(t, replaced(t, gatesWorkflow, c.from, c.to))
			assert.Contains(t, messages(checkRequiredGateJob("g.yaml", j, pre, orderGate)), c.want)
		})
	}
}

func Test_ifExpression(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct{ in, want string }{
		"${{ }} を外す":     {"${{ a && b }}", "a && b"},
		"式だけはそのまま":       {"a && b", "a && b"},
		"ブロックの記号と改行を詰める": {">-   a &&    b", "a && b"},
		"定数": {"false", "false"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, c.want, ifExpression(c.in))
		})
	}
}

func Test_checkPullRequestGate(t *testing.T) {
	t.Parallel()

	apply := []groupRecord{{file: "a.yaml", root: "admin-identity", kind: cientry.Apply}}
	unlock := []groupRecord{{file: "u.yaml", root: "admin-identity", kind: cientry.Unlock}}
	hold := []groupRecord{{file: "h.yaml", root: "admin-identity", kind: cientry.Hold}}

	for _, g := range applyGates {
		t.Run(g.noun+": Pull Request の側に在れば違反0件", func(t *testing.T) {
			t.Parallel()

			assert.Empty(t, checkPullRequestGate("wf", apply, 1, g))
		})

		t.Run(g.noun+": apply の job が無ければ求めない", func(t *testing.T) {
			t.Parallel()

			assert.Empty(t, checkPullRequestGate("wf", unlock, 0, g))
			assert.Empty(t, checkPullRequestGate("wf", hold, 0, g), "lock を残す job は apply しないので求めない")
			assert.Empty(t, checkPullRequestGate("wf", nil, 0, g))
		})

		t.Run(g.noun+": apply の job が在って Pull Request の側に無ければ違反", func(t *testing.T) {
			t.Parallel()

			assert.Contains(t, messages(checkPullRequestGate("wf", append(unlock, apply...), 0, g)), g.check+" を走らせるもの")
		})
	}
}

func Test_isUnlockRole(t *testing.T) {
	t.Parallel()

	for role, want := range map[string]bool{
		"unlock-identity": true,
		"unlock":          true,
		"apply":           false,
		"apply-identity":  false,
		"plan":            false,
		"":                false,
	} {
		t.Run(role, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, want, isUnlockRole(role))
		})
	}
}

func Test_kindHasRole(t *testing.T) {
	t.Parallel()

	k := cientry.Kind{IdentityRole: "a-identity", WorkloadRole: "a"}
	for role, want := range map[string]bool{
		"a-identity": true,
		"a":          true,
		"b":          false,
		"":           false,
	} {
		t.Run(role+" を種別の role として判定する", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, want, kindHasRole(k, role))
		})
	}

	t.Run("role が空の種別でも、空の role は種別の role ではない", func(t *testing.T) {
		t.Parallel()
		assert.False(t, kindHasRole(cientry.Kind{}, ""))
	})
}

func Test_eventsAllowed(t *testing.T) {
	t.Parallel()

	k := cientry.Kind{Events: []string{cientry.EventPush, cientry.EventWorkflowDispatch}}
	for name, c := range map[string]struct {
		keys []string
		want bool
	}{
		"許すイベントのすべて":  {[]string{cientry.EventPush, cientry.EventWorkflowDispatch}, true},
		"許すイベントの一部":   {[]string{cientry.EventWorkflowDispatch}, true},
		"同じイベントの重複":   {[]string{cientry.EventPush, cientry.EventPush}, true},
		"起動が無い":       {nil, false},
		"許さないイベントを含む": {[]string{cientry.EventPush, cientry.EventPullRequest}, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, eventsAllowed(c.keys, k))
		})
	}
}

func Test_jobKind(t *testing.T) {
	t.Parallel()

	jobWith := func(t *testing.T, runs ...string) workflow.Job {
		t.Helper()
		var b strings.Builder
		b.WriteString("name: T\n\njobs:\n  j:\n    steps:\n")
		for _, r := range runs {
			b.WriteString("      - run: " + r + "\n")
		}
		split := workflow.SplitJobs(b.String())
		require.Len(t, split.Jobs, 1)
		return split.Jobs[0]
	}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		for _, k := range cientry.Kinds() {
			for _, root := range []cientry.Root{cientry.RootIdentity, cientry.RootWorkload} {
				t.Run(k.Name+" の入口だけを持ち "+k.Role(root)+" の role を取る job は "+k.Name+" の job", func(t *testing.T) {
					t.Parallel()
					job := jobWith(t, "make ci-"+k.Name+"-admin-"+string(root)+" RUNNER_MODE=host")
					assert.Equal(t, k.Name, jobKind(job, k.Role(root)))
				})
			}
		}

		t.Run("plan の入口を持ち lock の解除の role を取る job は plan の job", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, cientry.Plan, jobKind(jobWith(t, "make ci-plan-admin-identity RUNNER_MODE=host"), "unlock-identity"))
		})

		t.Run("lock の解除の入口を持つ job は、ほかの入口と role を問わず lock の解除の job", func(t *testing.T) {
			t.Parallel()
			job := jobWith(t, "make ci-hold-admin-identity RUNNER_MODE=host", "make ci-unlock-admin-identity RUNNER_MODE=host")
			assert.Equal(t, cientry.Unlock, jobKind(job, "plan"))
		})

		t.Run("lock を残す入口を持つ job は、plan の role を取っても lock を残す job", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, cientry.Hold, jobKind(jobWith(t, "make ci-hold-admin-identity RUNNER_MODE=host"), "plan"))
		})

		t.Run("plan の入口を持ち apply の role を取る job は apply の job", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, cientry.Apply, jobKind(jobWith(t, "make ci-plan-admin-identity RUNNER_MODE=host"), "apply-identity"))
		})
	})
}

func Test_entryCredential(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		kind, envRoot, env, role string
	}{
		"identity root の plan は plan":                {cientry.Plan, "admin-identity", "admin", "plan"},
		"workload root の plan は plan":                {cientry.Plan, "admin-workload", "admin", "plan"},
		"identity root の apply は apply-identity":     {cientry.Apply, "admin-identity", "admin", "apply-identity"},
		"workload root の apply は apply":              {cientry.Apply, "admin-workload", "admin", "apply"},
		"identity root の lock を残す入口は apply-identity": {cientry.Hold, "admin-identity", "admin", "apply-identity"},
		"workload root の lock を残す入口は apply":          {cientry.Hold, "admin-workload", "admin", "apply"},
		"identity root の lock の解除は unlock-identity":  {cientry.Unlock, "admin-identity", "admin", "unlock-identity"},
		"workload root の lock の解除は unlock":           {cientry.Unlock, "admin-workload", "admin", "unlock"},
		"ハイフンを含む env も末尾で切る":                         {cientry.Apply, "log-archive-workload", "log-archive", "apply"},
		"接尾辞は末尾から1回だけ切る":                             {cientry.Apply, "x-identity-workload", "x-identity", "apply"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			env, role, ok := entryCredential(c.kind, c.envRoot)
			require.True(t, ok)
			assert.Equal(t, c.env, env)
			assert.Equal(t, c.role, role)
		})
	}

	for name, c := range map[string]struct{ kind, envRoot string }{
		"接尾辞の無い root は読めない":  {cientry.Apply, "admin-other"},
		"env の無い root は読めない": {cientry.Apply, "-identity"},
		"空の入口は読めない":          {cientry.Unlock, ""},
		"知らない種別は読めない":        {"revert", "admin-identity"},
		"接尾辞だけの名前は読めない":      {cientry.Hold, "workload"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			env, role, ok := entryCredential(c.kind, c.envRoot)
			assert.False(t, ok)
			assert.Empty(t, env)
			assert.Empty(t, role)
		})
	}

}

func Test_namedKinds(t *testing.T) {
	t.Parallel()

	t.Run("規則が名指す種別を登録簿から引く", func(t *testing.T) {
		t.Parallel()
		got, err := namedKinds(cientry.Lookup)
		require.NoError(t, err)
		assert.Len(t, got, 4)
		assert.Equal(t, cientry.Hold, got[cientry.Hold].Name)
	})

	t.Run("登録簿に無い種別があれば失敗する", func(t *testing.T) {
		t.Parallel()
		_, err := namedKinds(func(name string) (cientry.Kind, bool) {
			if name == cientry.Hold {
				return cientry.Kind{}, false
			}
			return cientry.Lookup(name)
		})
		require.ErrorIs(t, err, errKindMissing)
		assert.Contains(t, err.Error(), cientry.Hold)
	})
}

func Test_entryRuns(t *testing.T) {
	t.Parallel()

	t.Run("登録簿のすべての種別に入口の run: の形があり、ほかに無い", func(t *testing.T) {
		t.Parallel()

		require.Len(t, entryRuns, len(cientry.Kinds()))
		for _, kind := range cientry.Kinds() {
			k := kind.Name
			re := entryRuns[k]
			require.NotNil(t, re, k)
			m := re.FindStringSubmatch("      - run: make ci-" + k + "-x-identity RUNNER_MODE=host")
			require.NotNil(t, m, k)
			assert.Equal(t, "x-identity", m[1], k)
			_, _, ok := entryCredential(k, m[1])
			assert.True(t, ok, k)
		}
	})
}

func Test_entryNames(t *testing.T) {
	t.Parallel()

	t.Run("違反の文の入口の並びは種別の一覧から組む", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, "make ci-plan-*・ci-apply-*・ci-unlock-*・ci-hold-*", entryNames)
	})

	t.Run("登録簿のすべての種別を登録簿の順に並べる", func(t *testing.T) {
		t.Parallel()

		var names []string
		for _, k := range cientry.Kinds() {
			names = append(names, "ci-"+k.Name+"-*")
		}
		assert.Equal(t, "make "+strings.Join(names, "・"), entryNames)
	})
}

func Test_entryRegexps(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		got  *regexp.Regexp
		want string
	}{
		"ciEntry":     {ciEntry, `\bmake\b.*\bci-(?:plan|apply|unlock|hold)-[a-z0-9-]+`},
		"entryToken":  {entryToken, `\bci-(?:plan|apply|unlock|hold)-`},
		"unlockEntry": {unlockEntry, `\bmake\b.*\bci-(?:unlock)-([a-z0-9-]+)`},
		"holdEntry":   {holdEntry, `\bmake\b.*\bci-(?:hold)-([a-z0-9-]+)`},
		"otherEntry":  {otherEntry, `\bmake\b.*\bci-(?:plan|apply|unlock)-[a-z0-9-]+`},
		"applyEntry":  {applyEntry, `\bmake\b.*\bci-(?:apply)-([a-z0-9-]+)`},
		"planEntry":   {planEntry, `\bmake\b.*\bci-(?:plan)-[a-z0-9-]+`},
		"runEntry":    {runEntry, `\bmake\b.*\bci-(?:plan|apply)-[a-z0-9-]+`},
	} {
		t.Run(name+" は登録簿から同じ字面に組まれる", func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, c.want, c.got.String())
		})
	}

	alt := func(keep func(cientry.Kind) bool) string {
		var names []string
		for _, k := range cientry.Kinds() {
			if keep(k) {
				names = append(names, k.Name)
			}
		}
		return "(?:" + strings.Join(names, "|") + ")"
	}
	is := func(names ...string) func(cientry.Kind) bool {
		return func(k cientry.Kind) bool { return slices.Contains(names, k.Name) }
	}
	all := func(cientry.Kind) bool { return true }
	for name, c := range map[string]struct {
		got  *regexp.Regexp
		want string
	}{
		"ciEntry":     {ciEntry, `\bmake\b.*\bci-` + alt(all) + `-[a-z0-9-]+`},
		"entryToken":  {entryToken, `\bci-` + alt(all) + `-`},
		"unlockEntry": {unlockEntry, `\bmake\b.*\bci-` + alt(is(cientry.Unlock)) + `-([a-z0-9-]+)`},
		"holdEntry":   {holdEntry, `\bmake\b.*\bci-` + alt(is(cientry.Hold)) + `-([a-z0-9-]+)`},
		"otherEntry":  {otherEntry, `\bmake\b.*\bci-` + alt(func(k cientry.Kind) bool { return k.Name != cientry.Hold }) + `-[a-z0-9-]+`},
		"applyEntry":  {applyEntry, `\bmake\b.*\bci-` + alt(is(cientry.Apply)) + `-([a-z0-9-]+)`},
		"planEntry":   {planEntry, `\bmake\b.*\bci-` + alt(is(cientry.Plan)) + `-[a-z0-9-]+`},
		"runEntry":    {runEntry, `\bmake\b.*\bci-` + alt(is(cientry.Plan, cientry.Apply)) + `-[a-z0-9-]+`},
	} {
		t.Run(name+" の種別の並びは登録簿の種別から導かれる", func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, c.want, c.got.String())
		})
	}

	for name, c := range map[string]struct {
		re   *regexp.Regexp
		line string
		want bool
	}{
		"ciEntry は make の hold の入口に当たる": {ciEntry, "run: make ci-hold-admin-identity", true},
		"ciEntry は make の無い入口に当たらない":    {ciEntry, "run: echo ci-hold-admin-identity", false},
		"entryToken は make の無い入口にも当たる":  {entryToken, "group: ci-apply-x", true},
		"entryToken は語の途中に当たらない":        {entryToken, "xci-apply-x", false},
		"otherEntry は hold の入口に当たらない":   {otherEntry, "run: make ci-hold-admin-identity", false},
		"otherEntry は unlock の入口に当たる":   {otherEntry, "run: make ci-unlock-admin-identity", true},
		"runEntry は unlock の入口に当たらない":   {runEntry, "run: make ci-unlock-admin-identity", false},
		"runEntry は apply の入口に当たる":      {runEntry, "run: make ci-apply-admin-identity", true},
		"applyEntry は plan の入口に当たらない":   {applyEntry, "run: make ci-plan-admin-identity", false},
		"planEntry は ci-planner に当たらない": {planEntry, "run: make ci-planner-x", false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, c.want, c.re.MatchString(c.line))
		})
	}
}

func Test_isKindEntry(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		kind, line string
		want       bool
	}{
		"apply の入口の run: は apply の入口": {cientry.Apply, "      - run: make ci-apply-admin-identity RUNNER_MODE=host", true},
		"plan の入口は apply の入口ではない":     {cientry.Apply, "      - run: make ci-plan-admin-identity RUNNER_MODE=host", false},
		"コメントの入口は数えない":                {cientry.Hold, "      # - run: make ci-hold-admin-identity", false},
		"name: の入口は数えない":              {cientry.Unlock, "      - name: make ci-unlock-admin-identity", false},
		"run: の形でない入口は数えない":           {cientry.Plan, "      - run: make ci-plan-admin-identity || true", false},
		"step と別の行の run: も入口":         {cientry.Hold, "        run: make ci-hold-admin-workload", true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, c.want, isKindEntry(c.kind, c.line))
		})
	}
}

func Test_kindEntries(t *testing.T) {
	t.Parallel()

	t.Run("job の中の種別の入口の行を返し、name: とコメントを除く", func(t *testing.T) {
		t.Parallel()

		src := replaced(t, unlockWorkflow, unlockEntryLine, unlockEntryLine+"      # make ci-unlock-admin-workload\n      - name: make ci-unlock-admin-workload\n        run: echo x\n")
		got := kindEntries(jobOf(t, src, "unlock-admin-identity"), cientry.Unlock)
		require.Len(t, got, 1)
		assert.Equal(t, strings.TrimSuffix(unlockEntryLine, "\n"), got[0].Text)
	})

	t.Run("入口を2つ持てば2行を返す", func(t *testing.T) {
		t.Parallel()

		src := replaced(t, unlockWorkflow, unlockEntryLine, unlockEntryLine+"      - run: make ci-unlock-admin-workload RUNNER_MODE=host\n")
		assert.Len(t, kindEntries(jobOf(t, src, "unlock-admin-identity"), cientry.Unlock), 2)
	})

	t.Run("別の種別の入口は返さない", func(t *testing.T) {
		t.Parallel()

		assert.Empty(t, kindEntries(jobOf(t, unlockWorkflow, "unlock-admin-identity"), cientry.Hold))
	})
}

var workloadApplyJob = strings.Join([]string{
	"  apply-admin-workload:",
	"    needs: [apply-infra-code-gate, apply-resource-order-check, apply-admin-identity]",
	"    runs-on: ubuntu-24.04",
	"    steps:",
	"      - run: make ci-apply-admin-workload RUNNER_MODE=host",
	"",
	"",
}, "\n")

func Test_checkRevertWiring(t *testing.T) {
	t.Parallel()

	t.Run("apply の job と revert と通知が揃えば違反0件", func(t *testing.T) {
		t.Parallel()
		ok, findings := checkRevertWiring("apply.yml", applyWorkflow)
		assert.True(t, ok)
		assert.Empty(t, messages(findings))
	})

	t.Run("apply の job に name: があれば違反", func(t *testing.T) {
		t.Parallel()
		src := replaced(t, applyWorkflow, "  apply-admin-identity:\n", "  apply-admin-identity:\n    name: Apply\n")
		ok, findings := checkRevertWiring("apply.yml", src)
		assert.True(t, ok)
		assert.Equal(t, "apply の job apply-admin-identity に name: があります（apply-revert は job を表示名で引く）\n", messages(findings))
	})

	t.Run("apply の job の無い workflow は対象にしない", func(t *testing.T) {
		t.Parallel()
		ok, findings := checkRevertWiring("p.yaml", planWorkflow)
		assert.False(t, ok)
		assert.Empty(t, findings)
	})

	both := func(t *testing.T) string {
		t.Helper()
		src := replaced(t, applyWorkflow, "  revert-plan:\n", workloadApplyJob+"  revert-plan:\n")
		src = replaced(t, src, "  revert-plan:\n    needs: [apply-admin-identity]", "  revert-plan:\n    needs: [apply-admin-identity, apply-admin-workload]")
		src = replaced(t, src, "needs.apply-admin-identity.result == 'failure' }}", "(needs.apply-admin-identity.result == 'failure' || needs.apply-admin-workload.result == 'failure') }}")
		src = replaced(t, src, ":${{ needs.apply-admin-identity.outputs.failure }}\n", ":${{ needs.apply-admin-identity.outputs.failure }}\n            apply-admin-workload=${{ needs.apply-admin-workload.result }}:${{ needs.apply-admin-workload.outputs.apply-outcome }}:${{ needs.apply-admin-workload.outputs.failure }}\n")
		src = replaced(t, src, "  notify-failure:\n    needs: [apply-infra-code-gate, apply-resource-order-check, apply-admin-identity]", "  notify-failure:\n    needs: [apply-infra-code-gate, apply-resource-order-check, apply-admin-identity, apply-admin-workload]")
		return src + "  notify-success-workload:\n    needs: [apply-admin-workload]\n    if: ${{ needs.apply-admin-workload.result == 'success' }}\n    uses: ./.github/workflows/notify.yaml\n"
	}

	t.Run("2つの root の apply の job がそれぞれ揃えば違反0件", func(t *testing.T) {
		t.Parallel()
		ok, findings := checkRevertWiring("apply.yml", both(t))
		assert.True(t, ok)
		assert.Empty(t, messages(findings))
	})

	for name, c := range map[string]struct {
		from, to, want string
	}{
		"revert-plan が無ければ違反":                   {"  revert-plan:\n", "  revert-gone:\n", "revert-plan の job がありません"},
		"revert-plan の needs から外せば違反":           {"needs: [apply-admin-identity, apply-admin-workload]\n    if: ${{ always()", "needs: [apply-admin-identity]\n    if: ${{ always()", "revert-plan の needs が apply の job"},
		"revert-plan の if: に失敗の条件が無ければ違反":       {" || needs.apply-admin-workload.result == 'failure'", "", "revert-plan の if: に needs.apply-admin-workload.result == 'failure'"},
		"結果を渡す一覧から外せば違反":                        {"            apply-admin-workload=${{ needs.apply-admin-workload.result }}:${{ needs.apply-admin-workload.outputs.apply-outcome }}:${{ needs.apply-admin-workload.outputs.failure }}\n", "", "APPLY_REVERT_RESULTS が渡す job"},
		"結果の一覧の名前と needs の参照が食い違えば違反":           {"apply-admin-workload=${{ needs.apply-admin-workload.result }}", "apply-admin-workload=${{ needs.apply-admin-identity.result }}", "APPLY_REVERT_RESULTS の apply-admin-workload"},
		"結果の一覧の apply の step の結果が別の job を指せば違反": {"${{ needs.apply-admin-workload.outputs.apply-outcome }}", "${{ needs.apply-admin-identity.outputs.apply-outcome }}", "APPLY_REVERT_RESULTS の apply-admin-workload"},
		"結果の一覧の失敗の段が別の job を指せば違反":              {"${{ needs.apply-admin-workload.outputs.failure }}", "${{ needs.apply-admin-identity.outputs.failure }}", "APPLY_REVERT_RESULTS の apply-admin-workload"},
		"notify-failure が無ければ違反":                {"  notify-failure:\n", "  notify-gone:\n", "notify-failure の job がありません"},
		"別の workflow の job を名指せば違反":             {"            apply-admin-workload=", "            apply-security-identity=${{ needs.apply-security-identity.result }}:${{ needs.apply-security-identity.outputs.apply-outcome }}:${{ needs.apply-security-identity.outputs.failure }}\n            apply-admin-workload=", "APPLY_REVERT_RESULTS が渡す job"},
		"workflow の名前が自分のファイル名でなければ違反":          {"APPLY_REVERT_WORKFLOW: apply.yml", "APPLY_REVERT_WORKFLOW: terraform-apply.yaml", "APPLY_REVERT_WORKFLOW"},
		"notify-failure の needs から外せば違反":        {"apply-admin-identity, apply-admin-workload]\n    if: ${{ always() && contains", "apply-admin-identity]\n    if: ${{ always() && contains", "notify-failure の needs に apply-admin-workload"},
		"成功の通知の job が無ければ違反":                    {"  notify-success-workload:\n    needs: [apply-admin-workload]\n", "  notify-success-workload:\n    needs: [apply-admin-identity]\n", "apply-admin-workload の成功を通知する job"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ok, findings := checkRevertWiring("apply.yml", replaced(t, both(t), c.from, c.to))
			assert.Len(t, findings, 1, messages(findings))
			assert.True(t, ok)
			assert.Contains(t, messages(findings), c.want)
		})
	}
}

func rootsWith(t *testing.T, roots ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, r := range roots {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, r), 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(dir, r, "main.tf"), []byte("# x\n"), 0o600))
	}
	return dir
}

// rootPerm は、checkRootCoverage が数える対象の job（assumesRole）にする permissions。
const rootPerm = "    permissions:\n      contents: read\n      id-token: write\n"

func rootJobs(roots ...string) map[string]string {
	var plan, apply, unlock, hold strings.Builder
	plan.WriteString("on:\n  pull_request:\njobs:\n")
	apply.WriteString("on:\n  push:\njobs:\n")
	unlock.WriteString("on:\n  workflow_dispatch:\njobs:\n")
	hold.WriteString("on:\n  workflow_dispatch:\njobs:\n")
	for _, r := range roots {
		plan.WriteString("  plan-" + r + ":\n" + rootPerm + "    steps:\n      - run: make ci-plan-" + r + " RUNNER_MODE=host\n")
		apply.WriteString("  apply-" + r + ":\n" + rootPerm + "    steps:\n      - run: make ci-apply-" + r + " RUNNER_MODE=host\n")
		unlock.WriteString("  unlock-" + r + ":\n" + rootPerm + "    steps:\n      - run: make ci-unlock-" + r + " RUNNER_MODE=host\n")
		hold.WriteString("  hold-" + r + ":\n" + rootPerm + "    steps:\n      - run: make ci-hold-" + r + " RUNNER_MODE=host\n")
	}
	return map[string]string{deployGateFile: plan.String(), "apply.yaml": apply.String(), "unlock.yaml": unlock.String(), "hold.yaml": hold.String()}
}

func Test_runRoots(t *testing.T) {
	t.Parallel()

	wf, env := repoWith(t, map[string]string{deployGateFile: planWorkflow, "apply.yml": applyWorkflow, "unlock.yaml": unlockWorkflow, "hold.yaml": holdWorkflow}, envDecl)

	t.Run("root ごとの CI の経路を数えて報せる", func(t *testing.T) {
		t.Parallel()
		var out bytes.Buffer
		require.NoError(t, run(wf, env, rulesetOf(env), rootsWith(t, "admin/identity"), &out), out.String())
		assert.Contains(t, out.String(), "root 1 件の CI の経路")
	})

	t.Run("経路の無い root があれば違反で失敗する", func(t *testing.T) {
		t.Parallel()
		var out bytes.Buffer
		require.ErrorIs(t, run(wf, env, rulesetOf(env), rootsWith(t, "admin/identity", "admin"), &out), errViolations)
		assert.Contains(t, out.String(), "root admin-workload を apply する job が 0 件")
	})

	t.Run("root が1つも無ければ対象なしで失敗する", func(t *testing.T) {
		t.Parallel()
		var out bytes.Buffer
		require.ErrorIs(t, run(wf, env, rulesetOf(env), t.TempDir(), &out), errNoTarget)
		assert.NotContains(t, out.String(), "違反")
	})

	t.Run("root が無くても workflow の違反を先に出して違反で失敗する", func(t *testing.T) {
		t.Parallel()
		bad, benv := repoWith(t, map[string]string{deployGateFile: strings.Replace(planWorkflow, "      id-token: write\n", "", 1)}, envDecl)
		var out bytes.Buffer
		err := run(bad, benv, rulesetOf(benv), t.TempDir(), &out)
		require.ErrorIs(t, err, errViolations)
		assert.NotErrorIs(t, err, errNoTarget)
		assert.Contains(t, out.String(), "job plan-admin-identity は CI の入口")
	})

	t.Run("root を渡さなければ root の有無を見ない", func(t *testing.T) {
		t.Parallel()
		var out bytes.Buffer
		require.NoError(t, run(wf, env, rulesetOf(env), "", &out), out.String())
		assert.Contains(t, out.String(), "root 0 件の CI の経路")
	})
}

func Test_checkRootCoverage(t *testing.T) {
	t.Parallel()

	four := []string{"admin-identity", "admin-workload", "security-identity", "security-workload"}
	dirs := []string{"admin/identity", "admin", "security/identity", "security"}

	t.Run("2つの env の4つの root がそれぞれ plan・apply・lock の解除の job を持てば違反0件", func(t *testing.T) {
		t.Parallel()
		n, findings, err := checkRootCoverage(rootsWith(t, dirs...), rootJobs(four...))
		require.NoError(t, err)
		assert.Equal(t, 4, n)
		assert.Empty(t, messages(findings))
	})

	drift := "on:\n  pull_request:\n  schedule:\njobs:\n  drift:\n" + rootPerm + "    steps:\n      - run: make ci-plan-admin-identity RUNNER_MODE=host\n"
	withoutPlan := func(j map[string]string) map[string]string {
		j[deployGateFile] = strings.Replace(j[deployGateFile], "make ci-plan-admin-identity", "echo", 1)
		return j
	}

	t.Run("同じ root の plan が2つあっても違反にしない（定期 plan）", func(t *testing.T) {
		t.Parallel()
		jobs := rootJobs(four...)
		jobs["drift.yaml"] = drift
		_, findings, err := checkRootCoverage(rootsWith(t, dirs...), jobs)
		require.NoError(t, err)
		assert.Empty(t, messages(findings))
	})

	t.Run("定期 plan の job も plan として数える", func(t *testing.T) {
		t.Parallel()
		jobs := withoutPlan(rootJobs(four...))
		jobs["drift.yaml"] = drift
		_, findings, err := checkRootCoverage(rootsWith(t, dirs...), jobs)
		require.NoError(t, err)
		assert.Empty(t, messages(findings))
	})

	t.Run("id-token を持たない plan の job は数えない", func(t *testing.T) {
		t.Parallel()
		jobs := withoutPlan(rootJobs(four...))
		jobs["drift.yaml"] = strings.Replace(drift, "      id-token: write\n", "", 1)
		_, findings, err := checkRootCoverage(rootsWith(t, dirs...), jobs)
		require.NoError(t, err)
		assert.Equal(t, "root admin-identity を Pull Request で plan する job がありません\n", messages(findings))
	})

	t.Run("id-token を持たない apply の job は数えない", func(t *testing.T) {
		t.Parallel()
		jobs := rootJobs(four...)
		jobs["apply.yaml"] = strings.Replace(jobs["apply.yaml"], "  apply-security-workload:\n"+rootPerm, "  apply-security-workload:\n    permissions:\n      contents: read\n", 1)
		_, findings, err := checkRootCoverage(rootsWith(t, dirs...), jobs)
		require.NoError(t, err)
		assert.Equal(t, "root security-workload を apply する job が 0 件です（1件を期待）\n", messages(findings))
	})

	for name, c := range map[string]struct{ file, job, want string }{
		"id-token を持たない unlock の job は数えない": {"unlock.yaml", "unlock-security-identity", "root security-identity の lock を外す job が 0 件です（1件を期待）\n"},
		"id-token を持たない hold の job は数えない":   {"hold.yaml", "hold-security-identity", "root security-identity の lock を残す job が 0 件です（1件を期待）\n"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			jobs := rootJobs(four...)
			jobs[c.file] = strings.Replace(jobs[c.file], "  "+c.job+":\n"+rootPerm, "  "+c.job+":\n    permissions:\n      contents: read\n", 1)
			_, findings, err := checkRootCoverage(rootsWith(t, dirs...), jobs)
			require.NoError(t, err)
			assert.Equal(t, c.want, messages(findings))
		})
	}

	t.Run("対象の apply の job と並ぶ id-token を持たない入口の job は数えない", func(t *testing.T) {
		t.Parallel()
		jobs := rootJobs(four...)
		jobs["extra.yaml"] = "on:\n  push:\njobs:\n  x:\n    steps:\n      - run: make ci-apply-admin-identity RUNNER_MODE=host\n"
		_, findings, err := checkRootCoverage(rootsWith(t, dirs...), jobs)
		require.NoError(t, err)
		assert.Empty(t, messages(findings))
	})

	t.Run(".terraform は root として数えない", func(t *testing.T) {
		t.Parallel()
		root := rootsWith(t, "admin/identity", "admin/identity/.terraform/modules/x")
		n, findings, err := checkRootCoverage(root, rootJobs("admin-identity"))
		require.NoError(t, err)
		assert.Equal(t, 1, n)
		assert.Empty(t, messages(findings))
	})

	t.Run(".tftest.hcl だけの tests は root として数えない", func(t *testing.T) {
		t.Parallel()
		root := rootsWith(t, "admin/identity")
		require.NoError(t, os.MkdirAll(filepath.Join(root, "admin/identity/tests"), 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(root, "admin/identity/tests/x.tftest.hcl"), []byte("run \"x\" {}\n"), 0o600))
		n, findings, err := checkRootCoverage(root, rootJobs("admin-identity"))
		require.NoError(t, err)
		assert.Equal(t, 1, n)
		assert.Empty(t, messages(findings))
	})

	t.Run("workload root の下の .terraform も root として数えない", func(t *testing.T) {
		t.Parallel()
		root := rootsWith(t, "admin/identity", "admin/.terraform")
		n, findings, err := checkRootCoverage(root, rootJobs("admin-identity"))
		require.NoError(t, err)
		assert.Equal(t, 1, n)
		assert.Empty(t, messages(findings))
	})

	for name, dir := range map[string]string{
		"tests の下の *.tf は配置の外の root":     "admin/identity/tests",
		". で始まるディレクトリの *.tf は配置の外の root": "admin/.alt",
		".terraform で始まるだけの名前は除外しない":     "admin/.terraformx",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := rootsWith(t, "admin/identity", dir)
			n, findings, err := checkRootCoverage(root, rootJobs("admin-identity"))
			require.NoError(t, err)
			assert.Equal(t, 1, n)
			assert.Equal(t, "root "+dir+" が ADR-0604 決定5 の配置（<env>/ と <env>/identity/）の外にあります\n", messages(findings))
		})
	}

	t.Run("root を置くディレクトリの symlink は違反", func(t *testing.T) {
		t.Parallel()
		root := rootsWith(t, dirs...)
		require.NoError(t, os.Symlink(filepath.Join(root, "admin"), filepath.Join(root, "linked")))
		_, findings, err := checkRootCoverage(root, rootJobs(four...))
		require.NoError(t, err)
		require.Len(t, findings, 1)
		assert.Contains(t, findings[0].Message, "symlink")
		assert.Equal(t, filepath.Join(root, "linked"), findings[0].File)
	})

	t.Run("root が1つも無ければ対象なしで失敗する", func(t *testing.T) {
		t.Parallel()
		_, _, err := checkRootCoverage(t.TempDir(), rootJobs())
		require.ErrorIs(t, err, errNoTarget)
	})

	t.Run("root を置くディレクトリが無ければ失敗する", func(t *testing.T) {
		t.Parallel()
		_, _, err := checkRootCoverage(filepath.Join(t.TempDir(), "missing"), rootJobs())
		require.ErrorIs(t, err, os.ErrNotExist)
	})

	for name, c := range map[string]struct {
		dirs []string
		jobs func() map[string]string
		want string
		n    int
	}{
		"配置の外の root は違反":               {append(slices.Clone(dirs), "admin/extra"), func() map[string]string { return rootJobs(four...) }, "admin/extra", 1},
		"名前が前方一致する別の root の job は数えない": {[]string{"admin/identity"}, func() map[string]string { return rootJobs("admin-identity-x") }, "root admin-identity を apply する job が 0 件", 4},
		"apply の job が無い root は違反": {dirs, func() map[string]string {
			j := rootJobs(four...)
			j["apply.yaml"] = strings.Replace(j["apply.yaml"], "make ci-apply-security-workload", "echo", 1)
			return j
		}, "security-workload を apply する job が 0 件", 1},
		"lock の解除の job が2つの root は違反": {dirs, func() map[string]string {
			j := rootJobs(four...)
			j["unlock2.yaml"] = j["unlock.yaml"]
			return j
		}, "lock を外す job が 2 件", 4},
		"lock を残す job が無い root は違反": {dirs, func() map[string]string {
			j := rootJobs(four...)
			j["hold.yaml"] = strings.Replace(j["hold.yaml"], "make ci-hold-security-identity", "echo", 1)
			return j
		}, "root security-identity の lock を残す job が 0 件", 1},
		"lock を残す job が2つの root は違反": {dirs, func() map[string]string {
			j := rootJobs(four...)
			j["hold2.yaml"] = j["hold.yaml"]
			return j
		}, "lock を残す job が 2 件", 4},
		"plan が Pull Request で起動しない root は違反": {dirs, func() map[string]string {
			j := rootJobs(four...)
			j[deployGateFile] = strings.Replace(j[deployGateFile], "  pull_request:", "  push:", 1)
			return j
		}, "Pull Request で plan する job がありません", 4},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, findings, err := checkRootCoverage(rootsWith(t, c.dirs...), c.jobs())
			require.NoError(t, err)
			assert.Contains(t, messages(findings), c.want)
			assert.Len(t, findings, c.n, messages(findings))
		})
	}
}

func Test_checkRevertPlan(t *testing.T) {
	t.Parallel()

	rp := jobOf(t, applyWorkflow, "revert-plan")

	t.Run("needs・if:・渡す job・workflow の名前が揃えば違反0件", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, checkRevertPlan(rp, []string{"apply-admin-identity"}, "apply.yml", formatted))
	})

	t.Run("workflow の名前が違えば違反", func(t *testing.T) {
		t.Parallel()
		got := checkRevertPlan(rp, []string{"apply-admin-identity"}, "terraform-apply.yaml", formatted)
		require.Len(t, got, 1)
		assert.Equal(t, `APPLY_REVERT_WORKFLOW が "apply.yml" で、この workflow のファイル名 "terraform-apply.yaml" ではありません`, got[0].Message)
	})

	t.Run("apply の job が増えれば、needs・if:・渡す job のそれぞれで違反", func(t *testing.T) {
		t.Parallel()
		got := messages(checkRevertPlan(rp, []string{"apply-admin-identity", "apply-admin-workload"}, "apply.yml", formatted))
		assert.Contains(t, got, "revert-plan の needs が apply の job")
		assert.Contains(t, got, "needs.apply-admin-workload.result == 'failure'")
		assert.Contains(t, got, "APPLY_REVERT_RESULTS が渡す job")
	})
}

func Test_notifiesSuccess(t *testing.T) {
	t.Parallel()

	ns := jobOf(t, applyWorkflow, "notify-success")
	nf := jobOf(t, applyWorkflow, "notify-failure")

	t.Run("needs に置き、成功の条件を持つ job は通知する", func(t *testing.T) {
		t.Parallel()
		assert.True(t, notifiesSuccess(ns, "apply-admin-identity"))
	})

	t.Run("needs に無い job の成功は通知しない", func(t *testing.T) {
		t.Parallel()
		assert.False(t, notifiesSuccess(ns, "apply-admin-workload"))
	})

	t.Run("needs に置いても成功の条件が無ければ通知しない", func(t *testing.T) {
		t.Parallel()
		assert.False(t, notifiesSuccess(nf, "apply-admin-identity"))
	})
}

var holdWorkflow = strings.Join([]string{
	"name: Terraform Lock Hold",
	"",
	"on:",
	"  workflow_dispatch:",
	"",
	"permissions:",
	"  contents: read",
	"",
	"concurrency:",
	"  group: terraform-apply-admin",
	"  cancel-in-progress: false",
	"",
	"jobs:",
	"  hold-infra-code-gate:",
	"    runs-on: ubuntu-24.04",
	"    permissions:",
	"      contents: read",
	"    steps:",
	"      - run: make infra-code-gate RUNNER_MODE=host",
	"",
	"  hold-admin-identity:",
	"    needs: hold-infra-code-gate",
	"    if: ${{ needs.hold-infra-code-gate.result == 'success' }}",
	"    runs-on: ubuntu-24.04",
	"    environment: admin-apply-identity",
	"    permissions:",
	"      contents: read",
	"      id-token: write",
	"    steps:",
	"      - name: Refuse stale commits",
	"        id: refuse-stale",
	"        run: head=$(git ls-remote \"https://github.com/$REPO.git\" \"refs/heads/$REF\") && test \"$SHA\" = \"$head\" || exit 1",
	"      - uses: actions/checkout@0000000000000000000000000000000000000000",
	"        with:",
	"          persist-credentials: false",
	"      - run: make ci-aws-credentials CI_ENV=admin CI_ROLE=apply-identity RUNNER_MODE=host",
	"      - run: make ci-hold-admin-identity RUNNER_MODE=host",
	"",
}, "\n")

const (
	holdEntryLine     = "      - run: make ci-hold-admin-identity RUNNER_MODE=host\n"
	holdGroup         = "  group: terraform-apply-admin\n"
	holdEnv           = "    environment: admin-apply-identity\n"
	holdWfConcurrency = "concurrency:\n" + holdGroup + "  cancel-in-progress: false\n"
)

func holdWith(t *testing.T, from, to string) (workflow.Job, workflowInfo) {
	t.Helper()

	src := replaced(t, holdWorkflow, from, to)

	return jobOf(t, src, "hold-admin-identity"), infoOf(src)
}

// workloadHoldWorkflow は、holdWorkflow の lock を残す job を workload root へ向けたもの。job の id は変えない。
func workloadHoldWorkflow(t *testing.T) string {
	t.Helper()

	src := replaced(t, holdWorkflow, "make ci-hold-admin-identity", "make ci-hold-admin-workload")
	src = replaced(t, src, "CI_ROLE=apply-identity", "CI_ROLE=apply")

	return replaced(t, src, "environment: admin-apply-identity", "environment: admin-apply")
}

func Test_runHold(t *testing.T) {
	t.Parallel()

	all := func(over map[string]string) map[string]string {
		m := map[string]string{deployGateFile: planWorkflow, "apply.yml": applyWorkflow, "unlock.yaml": unlockWorkflow, "hold.yaml": holdWorkflow}
		maps.Copy(m, over)

		return m
	}

	t.Run("plan・apply・lock の解除・lock を残す job が規則を満たせば成功する", func(t *testing.T) {
		t.Parallel()

		wf, env := repoWith(t, all(nil), envDecl)
		var out bytes.Buffer
		require.NoError(t, run(wf, env, rulesetOf(env), "", &out), out.String())
		assert.Contains(t, out.String(), "job 4 件")
	})

	for name, c := range map[string]struct {
		files map[string]string
		want  string
	}{
		"group が apply と違えば違反":                     {all(map[string]string{"hold.yaml": strings.Replace(holdWorkflow, holdGroup, "  group: terraform-hold-admin-identity\n", 1)}), "apply と並んで走り、lock を奪い合います"},
		"environment が apply と違えば違反":               {all(map[string]string{"hold.yaml": strings.Replace(strings.Replace(holdWorkflow, "environment: admin-apply-identity", "environment: admin-apply", 1), "CI_ROLE=apply-identity", "CI_ROLE=apply", 1)}), "environment \"admin-apply\" が、apply の job"},
		"apply の workflow が無ければ違反":                 {map[string]string{"hold.yaml": holdWorkflow}, "ちょうど1つではありません（0 件）"},
		"同じ root の apply が2つなら違反":                  {all(map[string]string{"apply2.yml": applyWorkflow}), "（2 件）"},
		"lock を残す job の group が式なら違反":              {all(map[string]string{"hold.yaml": strings.Replace(holdWorkflow, holdGroup, "  group: ${{ github.workflow }}\n", 1)}), "lock を残す job の concurrency の group を字面で書いて"},
		"apply の group が式なら比べられないので違反":             {all(map[string]string{"apply.yml": strings.Replace(applyWorkflow, "  group: terraform-apply-admin\n", "  group: ${{ github.workflow }}\n", 1)}), "lock を残す job の group と同じことを確かめられません"},
		"lock を残す job を lock の解除の workflow に置けば違反": {all(map[string]string{"hold.yaml": "", "unlock.yaml": unlockWorkflow + strings.SplitN(holdWorkflow, "jobs:\n", 2)[1]}), "lock を残す job（hold-admin-identity）を置かないで"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			files := maps.Clone(c.files)
			for k, v := range files {
				if v == "" {
					delete(files, k)
				}
			}
			wf, env := repoWith(t, files, envDecl)
			var out bytes.Buffer
			require.ErrorIs(t, run(wf, env, rulesetOf(env), "", &out), errViolations)
			assert.Contains(t, out.String(), c.want)
		})
	}
}

func Test_checkJobHold(t *testing.T) {
	t.Parallel()

	t.Run("make ci-hold-* を持つ job は lock を残す job として検査する", func(t *testing.T) {
		t.Parallel()

		job := jobOf(t, holdWorkflow, "hold-admin-identity")
		assert.Empty(t, checkJob("h.yaml", job, planPerms, envs, infoOf(holdWorkflow)))
	})

	t.Run("workload root の lock を残す job は apply の role で通る", func(t *testing.T) {
		t.Parallel()

		src := workloadHoldWorkflow(t)
		assert.Empty(t, checkJob("h.yaml", jobOf(t, src, "hold-admin-identity"), planPerms, envs, infoOf(src)))
	})

	t.Run("apply の入口と併せ持つ job は lock を残す job として落とし、apply の job として通さない", func(t *testing.T) {
		t.Parallel()

		job, w := holdWith(t, holdEntryLine, holdEntryLine+"      - run: make ci-apply-admin-identity RUNNER_MODE=host\n")
		got := messages(checkJob("h.yaml", job, planPerms, envs, w))
		assert.Contains(t, got, "他の CI の入口も走らせず")
	})

	t.Run("lock の解除の入口と併せ持つ job は lock の解除の job として落とす", func(t *testing.T) {
		t.Parallel()

		job, w := holdWith(t, holdEntryLine, holdEntryLine+"      - run: make ci-unlock-admin-identity RUNNER_MODE=host\n")
		assert.Contains(t, messages(checkJob("h.yaml", job, planPerms, envs, w)), "make ci-unlock-* だけを走らせてください")
	})

	for name, c := range map[string]struct{ role, env, want string }{
		"plan の role の lock を残す job は plan の規則へ逃がさず違反": {"plan", "admin-plan", "apply-identity の role を取ってください（plan）"},
		"unlock-identity の role の lock を残す job は違反":    {"unlock-identity", "admin-unlock-identity", "apply-identity の role を取ってください（unlock-identity）"},
		"identity root で apply の role は違反":             {"apply", "admin-apply", "apply-identity の role を取ってください（apply）"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			src := replaced(t, replaced(t, holdWorkflow, "CI_ROLE=apply-identity", "CI_ROLE="+c.role), "environment: admin-apply-identity", "environment: "+c.env)
			got := messages(checkJob("h.yaml", jobOf(t, src, "hold-admin-identity"), planPerms, envs, infoOf(src)))
			assert.Contains(t, got, c.want)
			assert.NotContains(t, got, "refuse-untrusted", "plan の規則で見ない")
		})
	}
}

func Test_isHold(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		job  workflow.Job
		want bool
	}{
		"make ci-hold-* を走らせる job":          {jobOf(t, holdWorkflow, "hold-admin-identity"), true},
		"apply の job":                       {jobOf(t, applyWorkflow, "apply-admin-identity"), false},
		"lock の解除の job":                     {jobOf(t, unlockWorkflow, "unlock-admin-identity"), false},
		"コメントと name: の make ci-hold- は数えない": {workflow.Job{Lines: lines("      # make ci-hold-admin-identity", "      - name: make ci-hold-admin-identity")}, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, c.want, isHold(c.job))
		})
	}
}

func Test_checkHold(t *testing.T) {
	t.Parallel()

	t.Run("規則を満たせば違反0件", func(t *testing.T) {
		t.Parallel()

		job := jobOf(t, holdWorkflow, "hold-admin-identity")
		assert.Empty(t, checkHold(job, credOf(job), infoOf(holdWorkflow), formatted))
	})

	t.Run("同じ workflow に lock を残す job が2つ並んでも互いを違反に数えない", func(t *testing.T) {
		t.Parallel()

		src := holdWorkflow + "  hold-admin-workload:\n    steps:\n      - run: make ci-hold-admin-workload RUNNER_MODE=host\n"
		job := jobOf(t, src, "hold-admin-identity")
		assert.Empty(t, checkHold(job, credOf(job), infoOf(src), formatted))
	})

	for name, c := range map[string]struct{ from, to, want string }{
		"push の起動は違反":                     {"  workflow_dispatch:\n", "  workflow_dispatch:\n  push:\n    branches: [admin]\n", "workflow_dispatch だけ"},
		"workflow_dispatch が無ければ違反":       {"  workflow_dispatch:\n", "  push:\n", "workflow_dispatch だけ"},
		"起動条件を読めなければ違反":                   {"on:\n  workflow_dispatch:\n", "", "workflow_dispatch だけ"},
		"inputs は違反":                      {"  workflow_dispatch:\n", "  workflow_dispatch:\n    inputs:\n      ref:\n        type: string\n", "inputs を置かないで"},
		"フロー形式の起動条件は違反":                   {"  workflow_dispatch:\n", "  workflow_dispatch: {inputs: {ref: {}}}\n", "フロー形式で書かないで"},
		"concurrency が無ければ違反":             {holdWfConcurrency, "", "cancel-in-progress: false の concurrency"},
		"実行中を取り消す concurrency は違反":        {"  cancel-in-progress: false\n", "  cancel-in-progress: true\n", "cancel-in-progress: false の concurrency"},
		"refuse-stale が無ければ違反":            {"        id: refuse-stale\n", "", "refuse-stale の step"},
		"同じ workflow の apply の job は違反":   {"jobs:\n", "jobs:\n  apply:\n    steps:\n      - run: make ci-apply-admin-identity RUNNER_MODE=host\n", "ほかの CI の入口を持つ job（apply）を置かないで"},
		"同じ workflow の plan の job は違反":    {"jobs:\n", "jobs:\n  plan:\n    steps:\n      - run: make ci-plan-admin-identity RUNNER_MODE=host\n", "ほかの CI の入口を持つ job（plan）を置かないで"},
		"同じ workflow の lock の解除の job は違反": {"jobs:\n", "jobs:\n  unlock:\n    steps:\n      - run: make ci-unlock-admin-identity RUNNER_MODE=host\n", "ほかの CI の入口を持つ job（unlock）を置かないで"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			job, w := holdWith(t, c.from, c.to)
			assert.Contains(t, messages(checkHold(job, credOf(job), w, formatted)), c.want)
		})
	}
}

func Test_checkHoldEntry(t *testing.T) {
	t.Parallel()

	t.Run("make ci-hold-* を1回だけ走らせれば違反0件", func(t *testing.T) {
		t.Parallel()

		assert.Empty(t, checkHoldEntry(jobOf(t, holdWorkflow, "hold-admin-identity"), formatted))
	})

	t.Run("terraform を入れる行と name: とコメントは数えない", func(t *testing.T) {
		t.Parallel()

		job := workflow.Job{Number: 1, Lines: lines(
			"          tools: aqua:hashicorp/terraform aqua:aws/aws-cli go",
			"      - name: make ci-apply-admin-identity terraform",
			"      # terraform apply",
			"      - run: make ci-hold-admin-identity RUNNER_MODE=host",
		)}
		assert.Empty(t, checkHoldEntry(job, formatted))
	})

	for name, c := range map[string]struct {
		lines []string
		want  string
	}{
		"入口が無ければ違反":                 {[]string{"      - run: echo"}, "1回だけ走らせてください（0 回）"},
		"入口が2回なら違反":                 {[]string{"      - run: make ci-hold-admin-identity RUNNER_MODE=host", "      - run: make ci-hold-admin-identity RUNNER_MODE=host"}, "1回だけ走らせてください（2 回）"},
		"terraform を直に走らせれば違反":      {[]string{"      - run: make ci-hold-admin-identity RUNNER_MODE=host", "      - run: terraform plan"}, "terraform を直に走らせず"},
		"apply の入口を併せ持てば違反":         {[]string{"      - run: make ci-hold-admin-identity ci-apply-admin-identity RUNNER_MODE=host"}, "他の CI の入口も走らせず"},
		"plan の入口を併せ持てば違反":          {[]string{"      - run: make ci-hold-admin-identity RUNNER_MODE=host", "      - run: make ci-plan-admin-identity RUNNER_MODE=host"}, "他の CI の入口も走らせず"},
		"lock の解除の入口を併せ持てば違反":       {[]string{"      - run: make ci-hold-admin-identity RUNNER_MODE=host", "      - run: make ci-unlock-admin-identity RUNNER_MODE=host"}, "他の CI の入口も走らせず"},
		"make の tf- の入口は terraform": {[]string{"      - run: make ci-hold-admin-identity RUNNER_MODE=host", "      - run: make tf-plan"}, "terraform を直に走らせず"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Contains(t, messages(checkHoldEntry(workflow.Job{Number: 1, Lines: lines(c.lines...)}, formatted)), c.want)
		})
	}
}

func holdRecord(file, root, group, kind, env string) groupRecord {
	return groupRecord{file: file, job: workflow.Job{ID: "j", Number: 1}, root: root, group: group, kind: kind, environment: env}
}

func Test_checkHoldPairing(t *testing.T) {
	t.Parallel()

	const (
		root = "admin-identity"
		env  = "admin-apply-identity"
	)

	t.Run("違反は lock を残す job の行に、job の id を添えて出す", func(t *testing.T) {
		t.Parallel()

		got := checkHoldPairing([]groupRecord{holdRecord("a", root, "g", cientry.Apply, env), holdRecord("h", root, "x", cientry.Hold, env)})
		require.Len(t, got, 1)
		assert.Equal(t, "h", got[0].File)
		assert.Equal(t, 1, got[0].Line)
		assert.True(t, strings.HasPrefix(got[0].Message, "job j: "), got[0].Message)
	})

	for name, c := range map[string]struct {
		records []groupRecord
		want    int
	}{
		"group と environment が同じなら違反0件": {[]groupRecord{holdRecord("a", root, "g", cientry.Apply, env), holdRecord("h", root, "g", cientry.Hold, env)}, 0},
		"lock を残す job が無ければ違反0件":        {[]groupRecord{holdRecord("a", root, "g", cientry.Apply, env)}, 0},
		"記録が無ければ違反0件":                   {nil, 0},
		"lock を残す job ごとに1件出す":          {[]groupRecord{holdRecord("a", root, "g", cientry.Apply, env), holdRecord("h", root, "x", cientry.Hold, env), holdRecord("i", root, "y", cientry.Hold, env)}, 2},
		"lock の解除の job は照合しない":          {[]groupRecord{holdRecord("u", root, "x", cientry.Unlock, "admin-unlock-identity")}, 0},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Len(t, checkHoldPairing(c.records), c.want)
		})
	}
}

func Test_holdProblem(t *testing.T) {
	t.Parallel()

	const (
		root = "admin-identity"
		env  = "admin-apply-identity"
	)
	h := holdRecord("h", root, "g", cientry.Hold, env)

	for name, c := range map[string]struct {
		records []groupRecord
		want    string
	}{
		"group と environment が同じなら空":    {[]groupRecord{holdRecord("a", root, "g", cientry.Apply, env)}, ""},
		"group が違えば違反":                  {[]groupRecord{holdRecord("a", root, "x", cientry.Apply, env)}, `group "g" が、apply の job j（a）の group "x" と違います`},
		"environment が違えば違反":            {[]groupRecord{holdRecord("a", root, "g", cientry.Apply, "admin-apply")}, `environment "admin-apply-identity" が、apply の job j（a）の environment "admin-apply" と違います`},
		"apply の environment が読めなければ違反": {[]groupRecord{holdRecord("a", root, "g", cientry.Apply, "")}, `environment "admin-apply-identity" が、apply の job j（a）の environment "" と違います`},
		"apply の job が無ければ違反":           {nil, "make ci-apply-admin-identity を走らせる apply の job が、ちょうど1つではありません（0 件）"},
		"別の root の apply は数えない":         {[]groupRecord{holdRecord("a", "admin-workload", "g", cientry.Apply, env)}, "（0 件）"},
		"lock の解除の job は相手に数えない":        {[]groupRecord{holdRecord("u", root, "g", cientry.Unlock, env)}, "（0 件）"},
		"lock を残す job は相手に数えない":         {[]groupRecord{holdRecord("i", root, "g", cientry.Hold, env)}, "（0 件）"},
		"apply の job が2つなら違反":           {[]groupRecord{holdRecord("a", root, "g", cientry.Apply, env), holdRecord("b", root, "g", cientry.Apply, env)}, "（2 件）"},
		"apply の group が式なら違反":          {[]groupRecord{holdRecord("a", root, "${{ github.workflow }}", cientry.Apply, env)}, "apply の job j（a）の concurrency の group を字面で読めません"},
		"apply の group を読めなければ違反":       {[]groupRecord{holdRecord("a", root, "", cientry.Apply, env)}, "字面で読めません"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := holdProblem(h, c.records)
			if c.want == "" {
				assert.Empty(t, got)

				return
			}
			assert.Contains(t, got, c.want)
		})
	}

	t.Run("lock を残す job と apply の job の environment がどちらも読めなければ違反", func(t *testing.T) {
		t.Parallel()

		got := holdProblem(holdRecord("h", root, "g", cientry.Hold, ""), []groupRecord{holdRecord("a", root, "g", cientry.Apply, "")})
		assert.Contains(t, got, `environment "" が、apply の job j（a）の environment "" と違います`)
	})

	for name, group := range map[string]string{"式の group は違反": "${{ github.workflow }}", "group が無ければ違反": ""} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := holdProblem(holdRecord("h", root, group, cientry.Hold, env), []groupRecord{holdRecord("a", root, group, cientry.Apply, env)})
			assert.Contains(t, got, "lock を残す job の concurrency の group を字面で書いて")
		})
	}
}

func Test_planTriggerAllowed(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		file string
		keys []string
		want bool
	}{
		{"workflow_call だけなら、どのファイルでも許す", "terraform-plan.yaml", []string{"workflow_call"}, true},
		{"deploy-gate.yaml の pull_request だけを許す", ".github/workflows/deploy-gate.yaml", []string{"pull_request"}, true},
		{"deploy-gate.yaml の外の pull_request は拒む", ".github/workflows/terraform-gates.yaml", []string{"pull_request"}, false},
		{"deploy-gate.yaml でも push を足せば拒む", "deploy-gate.yaml", []string{"pull_request", "push"}, false},
		{"workflow_call にほかの起動を足せば拒む", "terraform-plan.yaml", []string{"push", "workflow_call"}, false},
		{"起動が無ければ拒む", "deploy-gate.yaml", nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, c.want, planTriggerAllowed(c.file, c.keys))
		})
	}
}

// callerWorkflow は、id-token: write を渡して reusable workflow を呼ぶ job を持つ workflow。
var callerWorkflow = strings.Join([]string{
	"name: PR Gate",
	"",
	"on:",
	"  pull_request:",
	"",
	"permissions:",
	"  contents: read",
	"",
	"jobs:",
	"  call-plan:",
	"    uses: ./.github/workflows/plan.yaml",
	"    permissions:",
	"      contents: read",
	"      id-token: write",
	"",
}, "\n")

func Test_reusableCall(t *testing.T) {
	t.Parallel()

	t.Run("job の直下の uses: の値と行を返す", func(t *testing.T) {
		t.Parallel()

		callee, at, ok := reusableCall(jobOf(t, callerWorkflow, "call-plan"))
		require.True(t, ok)
		assert.Equal(t, "./.github/workflows/plan.yaml", callee)
		assert.Equal(t, 11, at)
	})

	t.Run("引用符を外す", func(t *testing.T) {
		t.Parallel()

		callee, _, ok := reusableCall(jobOf(t, replaced(t, callerWorkflow, "uses: ./.github/workflows/plan.yaml", "uses: \"./.github/workflows/plan.yaml\""), "call-plan"))
		require.True(t, ok)
		assert.Equal(t, "./.github/workflows/plan.yaml", callee)
	})

	t.Run("step の uses: は reusable workflow の呼び出しではない", func(t *testing.T) {
		t.Parallel()

		_, _, ok := reusableCall(jobOf(t, planWorkflow, "plan-admin-identity"))
		assert.False(t, ok)
	})
}

func Test_checkCaller(t *testing.T) {
	t.Parallel()

	t.Run("このリポジトリの workflow を contents: read と id-token: write で呼べば違反0件", func(t *testing.T) {
		t.Parallel()

		assert.Empty(t, checkCaller("c.yaml", jobOf(t, callerWorkflow, "call-plan"), planPerms))
	})

	t.Run("ほかのリポジトリの workflow を呼べば違反", func(t *testing.T) {
		t.Parallel()

		job := jobOf(t, replaced(t, callerWorkflow, "uses: ./.github/workflows/plan.yaml", "uses: other/repo/.github/workflows/plan.yaml@main"), "call-plan")
		f := checkCaller("c.yaml", job, planPerms)
		require.Len(t, f, 1, messages(f))
		assert.Contains(t, f[0].Message, "このリポジトリの ./.github/workflows/ の下のもの")
		assert.Equal(t, 11, f[0].Line)
	})

	t.Run("ほかの書き込みの権限を持てば違反", func(t *testing.T) {
		t.Parallel()

		f := checkCaller("c.yaml", jobOf(t, callerWorkflow, "call-plan"), append(slices.Clone(planPerms), "pull-requests: write"))
		require.Len(t, f, 1, messages(f))
		assert.Contains(t, f[0].Message, "（pull-requests: write）")
	})
}

func Test_checkCallees(t *testing.T) {
	t.Parallel()

	const dir = ".github/workflows"
	callee := func(on string) string {
		return strings.Replace(planWorkflow, "on:\n  pull_request:\n", on, 1)
	}

	t.Run("呼び出し先が workflow_call だけで起動すれば違反0件", func(t *testing.T) {
		t.Parallel()

		assert.Empty(t, checkCallees(dir, map[string]string{
			filepath.Join(dir, "gate.yaml"): callerWorkflow,
			filepath.Join(dir, "plan.yaml"): callee("on:\n  workflow_call:\n"),
		}))
	})

	t.Run("呼び出し先がほかの起動を持てば違反", func(t *testing.T) {
		t.Parallel()

		f := checkCallees(dir, map[string]string{
			filepath.Join(dir, "gate.yaml"): callerWorkflow,
			filepath.Join(dir, "plan.yaml"): callee("on:\n  workflow_call:\n  push:\n"),
		})
		require.Len(t, f, 1, messages(f))
		assert.Contains(t, f[0].Message, "workflow_call だけにしてください（push, workflow_call）")
		assert.Equal(t, filepath.Join(dir, "gate.yaml"), f[0].File)
	})

	t.Run("呼び出し先が無ければ違反", func(t *testing.T) {
		t.Parallel()

		f := checkCallees(dir, map[string]string{filepath.Join(dir, "gate.yaml"): callerWorkflow})
		require.Len(t, f, 1, messages(f))
		assert.Contains(t, f[0].Message, "呼び出し先 ./.github/workflows/plan.yaml が")
	})

	t.Run("id-token を渡さない呼び出しは見ない", func(t *testing.T) {
		t.Parallel()

		assert.Empty(t, checkCallees(dir, map[string]string{
			filepath.Join(dir, "gate.yaml"): replaced(t, callerWorkflow, "      id-token: write\n", ""),
			filepath.Join(dir, "plan.yaml"): callee("on:\n  push:\n"),
		}))
	})
}

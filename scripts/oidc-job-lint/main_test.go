package main

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/lintreport"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/requiredchecks"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/testenv"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/workflow"
)

const envDecl = `[{"name":"admin-plan"},{"name":"admin-apply"},{"name":"admin-apply-identity"},{"name":"admin-unlock-identity"}]`

var envs = []string{"admin-plan", "admin-apply", "admin-apply-identity", "admin-unlock-identity"}

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
	"  group: terraform-apply-admin-identity",
	"  cancel-in-progress: false",
	"",
	"jobs:",
	"  apply-infra-code-gate:",
	"    runs-on: ubuntu-24.04",
	"    permissions:",
	"      contents: read",
	"    steps:",
	"      - run: make infra-code-gate-test RUNNER_MODE=host",
	"      - run: make infra-code-gate RUNNER_MODE=host",
	"",
	"  apply-resource-order-check:",
	"    runs-on: ubuntu-24.04",
	"    permissions:",
	"      contents: read",
	"    steps:",
	"      - run: make resource-order-check-test RUNNER_MODE=host",
	"      - run: make resource-order-check RUNNER_MODE=host",
	"",
	"  apply-admin-identity:",
	"    needs: [apply-infra-code-gate, apply-resource-order-check]",
	"    if: ${{ needs.apply-infra-code-gate.result == 'success' && needs.apply-resource-order-check.result == 'success' && github.ref_name == github.event.repository.default_branch }}",
	"    runs-on: ubuntu-24.04",
	"    environment:",
	"      name: admin-apply-identity",
	"    permissions:",
	"      contents: read",
	"      id-token: write",
	"    steps:",
	"      - name: Refuse stale commits",
	"        id: refuse-stale",
	"        run: test \"$SHA\" = \"$HEAD\"",
	"      - uses: actions/checkout@0000000000000000000000000000000000000000",
	"        with:",
	"          persist-credentials: false",
	"      - run: make ci-aws-credentials CI_ENV=admin CI_ROLE=apply-identity RUNNER_MODE=host",
	"      - run: make ci-apply-admin-identity RUNNER_MODE=host",
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
	wfConcurrency = "concurrency:\n  group: terraform-apply-admin-identity\n  cancel-in-progress: false\n"
	applyEnv      = "    environment:\n      name: admin-apply-identity\n"
)

func jobConcurrency(cancel string) string {
	return "    concurrency:\n      group: x\n      cancel-in-progress: " + cancel + "\n"
}

func infoOf(source string) workflowInfo {
	split := workflow.SplitJobs(source)
	on, onLine := triggers(split.Preamble)

	return workflowInfo{on: on, onLine: onLine, preamble: split.Preamble, jobs: split.Jobs}
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

// gatesWorkflow は、Pull Request の必須検査として順序の検査を走らせる workflow。repoWith が既定で置く。
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
	"      - name: Run the resource-order check's own tests",
	"        run: make resource-order-check-test RUNNER_MODE=host",
	"      - name: Check the resource order",
	"        run: make resource-order-check RUNNER_MODE=host",
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

	t.Run("plan と apply の job が規則を満たせば成功する", func(t *testing.T) {
		t.Parallel()

		wf, env := repoWith(t, map[string]string{"plan.yaml": planWorkflow, "apply.yml": applyWorkflow}, envDecl)
		var out bytes.Buffer
		require.NoError(t, run(wf, env, rulesetOf(env), &out))
		assert.Contains(t, out.String(), "job 2 件")
	})

	t.Run("対象の job が無ければ errNoTarget", func(t *testing.T) {
		t.Parallel()

		wf, env := repoWith(t, map[string]string{"x.yaml": "on: push\njobs:\n  a:\n    steps:\n      - run: echo\n"}, envDecl)
		require.ErrorIs(t, run(wf, env, rulesetOf(env), &bytes.Buffer{}), errNoTarget)
	})

	t.Run("workflow を読めなければ失敗する", func(t *testing.T) {
		t.Parallel()
		testenv.RequireNonRoot(t, "特権実行では権限の無いファイルも読めるため検証できない")

		wf, env := repoWith(t, map[string]string{"plan.yaml": planWorkflow, "apply.yml": applyWorkflow}, envDecl)
		require.NoError(t, os.Chmod(filepath.Join(wf, "apply.yml"), 0))
		err := run(wf, env, rulesetOf(env), &bytes.Buffer{})
		require.ErrorIs(t, err, fs.ErrPermission)
		assert.Contains(t, err.Error(), "apply.yml の読み取り")
	})

	t.Run("workflow が1つも無ければ errNoTarget", func(t *testing.T) {
		t.Parallel()

		wf, env := repoWith(t, nil, envDecl)
		require.ErrorIs(t, run(wf, env, rulesetOf(env), &bytes.Buffer{}), errNoTarget)
	})

	t.Run("対象が無くても違反があれば違反を報告する", func(t *testing.T) {
		t.Parallel()

		wf, env := repoWith(t, map[string]string{"x.yaml": "on: push\npermissions:\n  id-token: write\njobs:\n  a:\n    steps:\n      - run: echo\n"}, envDecl)
		var out bytes.Buffer
		require.ErrorIs(t, run(wf, env, rulesetOf(env), &out), errViolations)
		assert.Contains(t, out.String(), "workflow 全体の permissions に id-token")
	})

	t.Run("違反があれば一覧を出して errViolations", func(t *testing.T) {
		t.Parallel()

		bad := strings.Replace(planWorkflow, "    needs: infra-code-gate\n", "", 1)
		wf, env := repoWith(t, map[string]string{"plan.yaml": bad}, envDecl)
		var out bytes.Buffer
		require.ErrorIs(t, run(wf, env, rulesetOf(env), &out), errViolations)
		assert.Contains(t, out.String(), "make infra-code-gate を走らせる job")
	})

	t.Run("workflow のディレクトリが無ければ失敗する", func(t *testing.T) {
		t.Parallel()

		_, env := repoWith(t, nil, envDecl)
		err := run(filepath.Join(t.TempDir(), "missing"), env, rulesetOf(env), &bytes.Buffer{})
		require.Error(t, err)
		require.NotErrorIs(t, err, errNoTarget)
	})

	t.Run("environments.json を読めなければ失敗する", func(t *testing.T) {
		t.Parallel()

		wf, env := repoWith(t, map[string]string{"plan.yaml": planWorkflow}, envDecl)
		require.ErrorIs(t, run(wf, filepath.Join(t.TempDir(), "missing.json"), rulesetOf(env), &bytes.Buffer{}), errEnvDecl)
	})

	t.Run("Pull Request の必須検査が順序の検査を走らせなければ失敗する", func(t *testing.T) {
		t.Parallel()

		wf, env := repoWith(t, map[string]string{"plan.yaml": planWorkflow, "apply.yml": applyWorkflow, "gates.yaml": strings.Replace(gatesWorkflow, "        run: make resource-order-check RUNNER_MODE=host\n", "        run: echo\n", 1)}, envDecl)
		var out bytes.Buffer
		require.ErrorIs(t, run(wf, env, rulesetOf(env), &out), errViolations)
		assert.Contains(t, out.String(), "make resource-order-check-test と make resource-order-check を走らせるものがありません")
	})

	t.Run("保護設定を読めなければ失敗する", func(t *testing.T) {
		t.Parallel()

		wf, env := repoWith(t, map[string]string{"plan.yaml": planWorkflow}, envDecl)
		require.ErrorIs(t, run(wf, env, filepath.Join(t.TempDir(), "missing.json"), &bytes.Buffer{}), requiredchecks.ErrRead)
	})

	t.Run("別の workflow の検出の job は needs に数えない", func(t *testing.T) {
		t.Parallel()

		apply := replaced(t, applyWorkflow, "    needs: [apply-infra-code-gate, apply-resource-order-check]\n", "    needs: [infra-code-gate, apply-resource-order-check]\n")
		wf, env := repoWith(t, map[string]string{"plan.yaml": planWorkflow, "apply.yml": apply}, envDecl)
		var out bytes.Buffer
		require.ErrorIs(t, run(wf, env, rulesetOf(env), &out), errViolations)
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

		n, f := check("p.yaml", planWorkflow, envs)
		assert.Equal(t, 1, n)
		assert.Empty(t, f)
	})

	t.Run("id-token を持たない job は対象にしない", func(t *testing.T) {
		t.Parallel()

		n, f := check("p.yaml", strings.Replace(planWorkflow, "      id-token: write\n", "", 1), envs)
		assert.Zero(t, n)
		assert.Empty(t, f)
	})

	t.Run("workflow 全体の permissions の id-token は違反", func(t *testing.T) {
		t.Parallel()

		src := strings.Replace(planWorkflow, "permissions:\n  contents: read\n\njobs:", "permissions:\n  contents: read\n  id-token: write\n\njobs:", 1)
		_, f := check("p.yaml", src, envs)
		assert.Contains(t, messages(f), "workflow 全体の permissions に id-token")
	})

	t.Run("1行の {} の permissions は読めないとして違反", func(t *testing.T) {
		t.Parallel()

		src := strings.Replace(planWorkflow, "    permissions:\n      contents: read\n      id-token: write\n", "    permissions: { contents: read, id-token: write }\n", 1)
		n, f := check("p.yaml", src, envs)
		assert.Zero(t, n)
		assert.Contains(t, messages(f), "job plan-admin-identity の permissions を読めません")
	})

	t.Run("job の write-all は読めないとして違反", func(t *testing.T) {
		t.Parallel()

		src := strings.Replace(planWorkflow, "    permissions:\n      contents: read\n      id-token: write\n", "    permissions: write-all\n", 1)
		_, f := check("p.yaml", src, envs)
		assert.Contains(t, messages(f), "job plan-admin-identity の permissions を読めません")
	})

	t.Run("workflow 全体の write-all は違反", func(t *testing.T) {
		t.Parallel()

		src := strings.Replace(planWorkflow, "permissions:\n  contents: read\n\njobs:", "permissions: write-all\n\njobs:", 1)
		_, f := check("p.yaml", src, envs)
		assert.Contains(t, messages(f), "workflow 全体の permissions を読めません")
	})

	t.Run("jobs: を読めない workflow の id-token は違反", func(t *testing.T) {
		t.Parallel()

		n, f := check("p.yaml", strings.Replace(planWorkflow, "\njobs:\n", "\n\"jobs\":\n", 1), envs)
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

			n, f := check("p.yaml", strings.Replace(planWorkflow, "      id-token: write\n", perm, 1), envs)
			assert.Zero(t, n)
			assert.Contains(t, messages(f), "job の permissions として読めません")
		})
	}

	t.Run("引用符付きの permissions キーは job の permissions として読めないので違反", func(t *testing.T) {
		t.Parallel()

		src := strings.Replace(planWorkflow, "    permissions:\n      contents: read\n      id-token: write\n", "    \"permissions\":\n      contents: read\n      id-token: write\n", 1)
		n, f := check("p.yaml", src, envs)
		assert.Zero(t, n)
		assert.Contains(t, messages(f), "job の permissions として読めません")
	})

	t.Run("id-token: read は token を得る行として数えない", func(t *testing.T) {
		t.Parallel()

		n, f := check("p.yaml", strings.Replace(planWorkflow, "      id-token: write\n", "      id-token: read\n", 1), envs)
		assert.Zero(t, n)
		assert.Empty(t, f)
	})

	t.Run("長期のアクセスキーの参照は違反", func(t *testing.T) {
		t.Parallel()

		src := planWorkflow + "        env:\n          AWS_ACCESS_KEY_ID: ${{ secrets.KEY }}\n"
		_, f := check("p.yaml", src, envs)
		require.Len(t, f, 2)
		assert.Contains(t, f[0].Message, "長期のアクセスキー")
		assert.Contains(t, f[1].Message, "env: に置けるのは")
	})

	t.Run("コメントの中のキー名は違反にしない", func(t *testing.T) {
		t.Parallel()

		_, f := check("p.yaml", "# aws-access-key-id は使わない\n"+planWorkflow, envs)
		assert.Empty(t, f)
	})
}

func Test_checkJob(t *testing.T) {
	t.Parallel()

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
		assert.Contains(t, messages(checkApply(j, credOf(j), aw, formatted)), "make ci-apply-* がありません")
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

		j, aw := applyWith(t, "    if: ${{ needs.apply-infra-code-gate.result == 'success' && needs.apply-resource-order-check.result == 'success' && github.ref_name == github.event.repository.default_branch }}\n", "")
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

	t.Run("検出の自己テストだけの job は検出と認めない", func(t *testing.T) {
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

		j, aw := applyWith(t, "    if: ${{ needs.apply-infra-code-gate.result == 'success' && needs.apply-resource-order-check.result == 'success' && github.ref_name == github.event.repository.default_branch }}\n",
			"    if: >-\n      needs.apply-infra-code-gate.result == 'success' &&\n      needs.apply-resource-order-check.result == 'success' &&\n      github.ref_name == github.event.repository.default_branch\n")
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
		"検出の job の似た名前のターゲットは違反":        {detStep, detStep + "      - run: make infra-code-gate-testx RUNNER_MODE=host\n", "run: は make infra-code-gate-test と make infra-code-gate だけ"},
		"検出の job の env: は違反":            {detJob, "  apply-infra-code-gate:\n    env:\n      X: y\n    runs-on: ubuntu-24.04\n", "env:"},
		"検出の job の container: は違反":      {detJob, "  apply-infra-code-gate:\n    container: alpine\n    runs-on: ubuntu-24.04\n", "container:"},
		"検出の job の MAKEFLAGS は違反":       {detJob, "  apply-infra-code-gate:\n    env:\n      MAKEFLAGS: -n\n    runs-on: ubuntu-24.04\n", "MAKEFLAGS と MAKEFILES を渡さない"},
		"検出の step の shell: は違反":         {detStep, detStep + "        shell: sh -c true {0}\n", "shell:"},
		"検出の step の引用符付き env: は違反":      {detStep, detStep + "        \"env\":\n          X: y\n", "env:"},
		"検出の job の MAKEFILES は違反":       {detStep, detStep + "        env:\n          MAKEFILES: x.mk\n", "MAKEFLAGS と MAKEFILES を渡さない"},
		"検出の job のほかの run: は違反":         {detStep, "      - run: echo /tmp/b >> \"$GITHUB_PATH\"\n" + detStep, "run: は make infra-code-gate-test と make infra-code-gate だけ"},
		"検出の job から GITHUB_PATH に書けば違反": {detStep, "      - run: echo /tmp/b >> \"$GITHUB_PATH\"\n" + detStep, "GITHUB_PATH と GITHUB_ENV"},
	}
	for name, c := range hidden {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			j, aw := applyWith(t, c.from, c.to)
			assert.Contains(t, messages(checkDetection(j, aw.jobs, formatted)), c.want)
		})
	}

	t.Run("needs に検出でも順序の検査でもない job があれば違反", func(t *testing.T) {
		t.Parallel()

		j, aw := applyWith(t, "  apply-admin-identity:\n    needs: [apply-infra-code-gate, apply-resource-order-check]\n",
			"  other:\n    runs-on: ubuntu-24.04\n    steps:\n      - run: echo\n\n  apply-admin-identity:\n    needs: [apply-infra-code-gate, apply-resource-order-check, other]\n")
		assert.Contains(t, messages(checkDetection(j, aw.jobs, formatted)), "needs の other は、検出の job（make infra-code-gate）でも順序の検査の job（make resource-order-check）でもありません")
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

	for name, c := range map[string]struct{ from, to, want string }{
		"ほかの run: は違反":         {orderStep, orderStep + "      - run: echo\n", "順序の検査の job apply-resource-order-check の run: は make resource-order-check-test と make resource-order-check だけ"},
		"変数を足した make は違反":      {orderStep, "      - run: make resource-order-check RUNNER_MODE=host RO_NS=x\n", "run: は make resource-order-check-test と make resource-order-check だけ"},
		"検出の make は違反":         {orderStep, orderStep + "      - run: make infra-code-gate RUNNER_MODE=host\n", "run: は make resource-order-check-test と make resource-order-check だけ"},
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

func Test_checkOrderGate(t *testing.T) {
	t.Parallel()

	const want = "apply の job の needs に、make resource-order-check を走らせる job を同じ workflow の中に置いてください"
	applyWith := func(t *testing.T, from, to string) (workflow.Job, workflowInfo) {
		t.Helper()

		src := replaced(t, applyWorkflow, from, to)

		return jobOf(t, src, "apply-admin-identity"), infoOf(src)
	}

	t.Run("順序の検査の job を needs に持てば違反0件", func(t *testing.T) {
		t.Parallel()

		assert.Empty(t, checkOrderGate(jobOf(t, applyWorkflow, "apply-admin-identity"), infoOf(applyWorkflow).jobs, formatted))
	})

	for name, c := range map[string]struct{ from, to string }{
		"needs に無ければ違反":           {"    needs: [apply-infra-code-gate, apply-resource-order-check]\n", "    needs: apply-infra-code-gate\n"},
		"自己テストだけの job は数えない":      {"      - run: make resource-order-check RUNNER_MODE=host\n", ""},
		"検出も走らせる job は順序の検査と数えない": {"      - run: make resource-order-check-test RUNNER_MODE=host\n", "      - run: make infra-code-gate RUNNER_MODE=host\n"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			j, aw := applyWith(t, c.from, c.to)
			assert.Contains(t, messages(checkOrderGate(j, aw.jobs, formatted)), want)
		})
	}

	t.Run("apply の job の検査に含まれる", func(t *testing.T) {
		t.Parallel()

		j, aw := applyWith(t, "    needs: [apply-infra-code-gate, apply-resource-order-check]\n", "    needs: apply-infra-code-gate\n")
		assert.Contains(t, messages(checkJob("a.yaml", j, planPerms, envs, aw)), want)
	})
}

// hiddenOf は、checkJob と同じく job の役割から拒否の id を決めて checkHidden を呼びます。
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
		"拒否の step の shell: は違反":                {"        run: test \"$SHA\" = \"$HEAD\"", "        run: test \"$SHA\" = \"$HEAD\"\n        shell: sh -c true {0}", "拒否の step に shell:"},
		"拒否の step の working-directory: は違反":    {"        run: test \"$SHA\" = \"$HEAD\"", "        run: test \"$SHA\" = \"$HEAD\"\n        working-directory: /tmp", "拒否の step に shell:"},
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
		"拒否の step から GITHUB_PATH に書けば違反":       {"test \"$SHA\" = \"$HEAD\"", "echo /tmp/b >> \"$GITHUB_PATH\"", "GITHUB_PATH と GITHUB_ENV"},
		"拒否の step から GITHUB_ENV に書けば違反":        {"test \"$SHA\" = \"$HEAD\"", "echo X=1 >> \"$GITHUB_ENV\"", "GITHUB_PATH と GITHUB_ENV"},
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

func Test_checkTerraform(t *testing.T) {
	t.Parallel()

	planWith := func(t *testing.T, add string) workflow.Job {
		t.Helper()

		return jobOf(t, replaced(t, planWorkflow, "      - run: make ci-plan-admin-identity RUNNER_MODE=host\n", "      - run: make ci-plan-admin-identity RUNNER_MODE=host\n"+add), "plan-admin-identity")
	}

	t.Run("入口だけで走らせれば違反0件", func(t *testing.T) {
		t.Parallel()

		assert.Empty(t, checkTerraform(jobOf(t, planWorkflow, "plan-admin-identity"), ciPlan, "plan", formatted))
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

			assert.Contains(t, messages(checkTerraform(planWith(t, add), ciPlan, "plan", formatted)), "make ci-plan-* でだけ")
		})
	}

	t.Run("terraform を入れる行は起動と数えない", func(t *testing.T) {
		t.Parallel()

		assert.Empty(t, checkTerraform(planWith(t, "      - uses: ./.github/actions/setup-mise\n        with:\n          tools: aqua:hashicorp/terraform go\n"), ciPlan, "plan", formatted))
	})

	t.Run("terraform を入れる字面を添えた run: の起動は違反", func(t *testing.T) {
		t.Parallel()

		got := messages(checkTerraform(planWith(t, "      - run: terraform plan # aqua:hashicorp/terraform\n"), ciPlan, "plan", formatted))
		assert.Contains(t, got, "make ci-plan-* でだけ")
	})

	t.Run("入口が無ければ違反", func(t *testing.T) {
		t.Parallel()

		job := jobOf(t, strings.Replace(planWorkflow, "make ci-plan-admin-identity", "echo", 1), "plan-admin-identity")
		assert.Contains(t, messages(checkTerraform(job, ciPlan, "plan", formatted)), "make ci-plan-* がありません")
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
		"自己テストだけは偽":                  {"      - run: make infra-code-gate-test\n", false},
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
		"自己テストだけは偽":                       {"      - run: make resource-order-check-test RUNNER_MODE=host\n", false},
		"検出は順序の検査ではない":                    {"      - run: make infra-code-gate RUNNER_MODE=host\n", false},
		"-n（実行しない）は偽":                     {"      - run: make -n resource-order-check\n", false},
	}
	for name, c := range order {
		t.Run("順序の検査/"+name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, c.want, runsGate(jobOf(t, "jobs:\n  a:\n    steps:\n"+c.step, "a"), orderGate))
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
		assert.Contains(t, l.Text, "run: test")
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
	"  group: terraform-unlock-admin-identity",
	"  cancel-in-progress: false",
	"",
	"jobs:",
	"  unlock-infra-code-gate:",
	"    runs-on: ubuntu-24.04",
	"    permissions:",
	"      contents: read",
	"    steps:",
	"      - run: make infra-code-gate-test RUNNER_MODE=host",
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
	"        run: test \"$SHA\" = \"$HEAD\"",
	"      - uses: actions/checkout@0000000000000000000000000000000000000000",
	"        with:",
	"          persist-credentials: false",
	"      - run: make ci-aws-credentials CI_ENV=admin CI_ROLE=unlock-identity RUNNER_MODE=host",
	"      - run: make ci-unlock-admin-identity RUNNER_MODE=host",
	"",
}, "\n")

const (
	unlockEntryLine     = "      - run: make ci-unlock-admin-identity RUNNER_MODE=host\n"
	unlockGroup         = "  group: terraform-unlock-admin-identity\n"
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

		wf, env := repoWith(t, map[string]string{"plan.yaml": planWorkflow, "apply.yml": applyWorkflow, "unlock.yaml": unlockWorkflow}, envDecl)
		var out bytes.Buffer
		require.NoError(t, run(wf, env, rulesetOf(env), &out), out.String())
		assert.Contains(t, out.String(), "job 3 件")
	})

	sameGroup := strings.Replace(unlockWorkflow, unlockGroup, "  group: terraform-apply-admin-identity\n", 1)
	for name, c := range map[string]struct {
		files map[string]string
		want  string
	}{
		"apply の workflow が無ければ違反":     {map[string]string{"unlock.yaml": unlockWorkflow}, "ちょうど1つではありません（0 件）"},
		"group が apply と同じなら違反":        {map[string]string{"apply.yml": applyWorkflow, "unlock.yaml": sameGroup}, "group と同じです"},
		"同じ root の apply が2つなら違反":      {map[string]string{"apply.yml": applyWorkflow, "apply2.yml": applyWorkflow, "unlock.yaml": unlockWorkflow}, "（2 件）"},
		"別の root の apply は相手に数えない":     {map[string]string{"apply.yml": strings.Replace(applyWorkflow, "ci-apply-admin-identity", "ci-apply-admin-workload", 1), "unlock.yaml": unlockWorkflow}, "make ci-apply-admin-identity を走らせる apply の job が、ちょうど1つではありません（0 件）"},
		"apply の group が式なら比べられないので違反": {map[string]string{"apply.yml": strings.Replace(applyWorkflow, "  group: terraform-apply-admin-identity\n", "  group: ${{ github.workflow }}\n", 1), "unlock.yaml": unlockWorkflow}, "字面で読めません"},
		"lock の解除の group が式なら違反":       {map[string]string{"apply.yml": applyWorkflow, "unlock.yaml": strings.Replace(unlockWorkflow, unlockGroup, "  group: ${{ github.workflow }}\n", 1)}, "字面で書いて"},
		"両方がブロックスカラーの group なら、本文が違っても違うと読まない": {map[string]string{
			"apply.yml":   strings.Replace(applyWorkflow, "  group: terraform-apply-admin-identity\n", "  group: >-\n    terraform-apply-admin-identity\n", 1),
			"unlock.yaml": strings.Replace(unlockWorkflow, unlockGroup, "  group: >-\n    terraform-unlock\n", 1),
		}, "字面で書いて"},
		"apply だけがブロックスカラーの group なら違反": {map[string]string{
			"apply.yml":   strings.Replace(applyWorkflow, "  group: terraform-apply-admin-identity\n", "  group: >-\n    terraform-apply-admin-identity\n", 1),
			"unlock.yaml": unlockWorkflow,
		}, "字面で読めません"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			wf, env := repoWith(t, c.files, envDecl)
			var out bytes.Buffer
			require.ErrorIs(t, run(wf, env, rulesetOf(env), &out), errViolations)
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

func Test_checkRole(t *testing.T) {
	t.Parallel()

	unlockJob := jobOf(t, unlockWorkflow, "unlock-admin-identity")
	applyJob := jobOf(t, applyWorkflow, "apply-admin-identity")
	planJob := jobOf(t, planWorkflow, "plan-admin-identity")
	for name, c := range map[string]struct {
		job  workflow.Job
		role string
		want string
	}{
		"lock の解除の job の unlock-identity は違反0件": {unlockJob, "unlock-identity", ""},
		"lock の解除の job の apply-identity は違反":    {unlockJob, "apply-identity", "lock の解除の job は unlock-identity の role を取って"},
		"lock の解除の job の apply は違反":             {unlockJob, "apply", "lock の解除の job は unlock-identity の role を取って"},
		"lock の解除の job の plan は違反":              {unlockJob, "plan", "lock の解除の job は unlock-identity の role を取って"},
		"apply の job の apply-identity は違反0件":    {applyJob, "apply-identity", ""},
		"apply の job の unlock-identity は違反":     {applyJob, "unlock-identity", "lock の解除の job（make ci-unlock-*）だけが取って"},
		"plan の job の plan は違反0件":               {planJob, "plan", ""},
		"plan の job の unlock-identity は違反":      {planJob, "unlock-identity", "lock の解除の job（make ci-unlock-*）だけが取って"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := messages(checkRole(c.job, credOf(c.job), c.role, formatted))
			if c.want == "" {
				assert.Empty(t, got)

				return
			}
			assert.Contains(t, got, c.want)
		})
	}
}

func Test_checkUnlock(t *testing.T) {
	t.Parallel()

	t.Run("規則を満たせば違反0件", func(t *testing.T) {
		t.Parallel()

		job := jobOf(t, unlockWorkflow, "unlock-admin-identity")
		assert.Empty(t, checkUnlock(job, credOf(job), infoOf(unlockWorkflow), formatted))
	})

	for name, c := range map[string]struct{ from, to, want string }{
		"push の起動は違反":                   {"  workflow_dispatch:\n", "  workflow_dispatch:\n  push:\n    branches: ['release/*']\n", "workflow_dispatch だけ"},
		"workflow_dispatch が無ければ違反":     {"  workflow_dispatch:\n", "  push:\n", "workflow_dispatch だけ"},
		"起動条件を読めなければ違反":                 {"on:\n  workflow_dispatch:\n", "", "workflow_dispatch だけ"},
		"inputs は違反":                    {"  workflow_dispatch:\n", "  workflow_dispatch:\n    inputs:\n      ref:\n        type: string\n", "inputs を置かないで"},
		"フロー形式の起動条件は違反":                 {"  workflow_dispatch:\n", "  workflow_dispatch: {inputs: {ref: {}}}\n", "フロー形式で書かないで"},
		"concurrency が無ければ違反":           {unlockWfConcurrency, "", "cancel-in-progress: false の concurrency"},
		"実行中を取り消す concurrency は違反":      {"  cancel-in-progress: false\n", "  cancel-in-progress: true\n", "cancel-in-progress: false の concurrency"},
		"refuse-stale が無ければ違反":          {"        id: refuse-stale\n", "", "refuse-stale の step"},
		"同じ workflow の apply の job は違反": {"jobs:\n", "jobs:\n  apply:\n    steps:\n      - run: make ci-apply-admin-identity RUNNER_MODE=host\n", "plan と apply の job（apply）を置かないで"},
		"同じ workflow の plan の job は違反":  {"jobs:\n", "jobs:\n  plan:\n    steps:\n      - run: make ci-plan-admin-identity RUNNER_MODE=host\n", "plan と apply の job（plan）を置かないで"},
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

		j, w := unlockWith(t, unlockWfConcurrency, "concurrency: {group: terraform-unlock-admin-identity, cancel-in-progress: false}\n")
		assert.Contains(t, messages(checkNoCancel(j, w, "lock の解除", formatted)), "cancel-in-progress: false の concurrency")
	})
}

func Test_groupRecords(t *testing.T) {
	t.Parallel()

	t.Run("lock の解除の job と apply の job を入口の root で取り出し、plan の job と検出の job は取り出さない", func(t *testing.T) {
		t.Parallel()

		got := map[string]groupRecord{}
		for file, src := range map[string]string{"p.yaml": planWorkflow, "a.yml": applyWorkflow, "u.yaml": unlockWorkflow} {
			for _, r := range groupRecords(file, src) {
				got[r.file] = r
			}
		}
		require.Len(t, got, 2)
		assert.Equal(t, groupRecord{file: "a.yml", job: jobOf(t, applyWorkflow, "apply-admin-identity"), root: "admin-identity", group: "terraform-apply-admin-identity"}, got["a.yml"])
		assert.Equal(t, groupRecord{file: "u.yaml", job: jobOf(t, unlockWorkflow, "unlock-admin-identity"), root: "admin-identity", group: "terraform-unlock-admin-identity", unlock: true}, got["u.yaml"])
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
	return groupRecord{file: file, job: workflow.Job{ID: "j", Number: 1}, root: root, group: group, unlock: unlock}
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
		"字面の値":      {"terraform-unlock-admin-identity", true},
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

		assert.Equal(t, "terraform-unlock-admin-identity", effectiveGroup(jobOf(t, unlockWorkflow, "unlock-admin-identity"), workflow.SplitJobs(unlockWorkflow).Preamble))
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
		"ブロックスカラーの group は読めない":       {[]string{"concurrency:", "  group: >-", "    terraform-apply-admin-identity"}, "", ""},
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
		"字面の値":   {"terraform-apply-admin-identity", "terraform-apply-admin-identity"},
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

func Test_checkRequiredOrderJobs(t *testing.T) {
	t.Parallel()

	required := map[string]bool{"tf-lint": true}
	const (
		check = "        run: make resource-order-check RUNNER_MODE=host\n"
		self  = "        run: make resource-order-check-test RUNNER_MODE=host\n"
	)

	t.Run("必須検査の job が両方を走らせれば1件で違反0件", func(t *testing.T) {
		t.Parallel()

		n, f := checkRequiredOrderJobs("g.yaml", gatesWorkflow, required)
		assert.Equal(t, 1, n)
		assert.Empty(t, f)
	})

	for name, src := range map[string]string{
		"検査そのものが無い":           strings.Replace(gatesWorkflow, check, "        run: echo\n", 1),
		"自己テストが無い":            strings.Replace(gatesWorkflow, self, "        run: echo\n", 1),
		"required でない job":    strings.Replace(gatesWorkflow, "  tf-lint:\n", "  other:\n", 1),
		"pull_request で起動しない": strings.Replace(gatesWorkflow, "  pull_request:\n", "  push:\n", 1),
		"変数を足した make は数えない":   strings.Replace(gatesWorkflow, check, "        run: make resource-order-check RUNNER_MODE=host RO_NS=x\n", 1),
		"コメントの中は数えない":         strings.Replace(gatesWorkflow, check, "        # run: make resource-order-check RUNNER_MODE=host\n        run: echo\n", 1),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			require.NotEqual(t, gatesWorkflow, src, "置き換え元が見つからない")
			n, f := checkRequiredOrderJobs("g.yaml", src, required)
			assert.Zero(t, n)
			assert.Empty(t, f)
		})
	}

	t.Run("数えた job の形の違反を返す", func(t *testing.T) {
		t.Parallel()

		src := replaced(t, gatesWorkflow, check, check+"        continue-on-error: true\n")
		n, f := checkRequiredOrderJobs("g.yaml", src, required)
		assert.Equal(t, 1, n)
		assert.Contains(t, messages(f), "順序の検査の step に if:・continue-on-error:")
	})
}

func Test_checkRequiredOrderJob(t *testing.T) {
	t.Parallel()

	const (
		check  = "        run: make resource-order-check RUNNER_MODE=host\n"
		self   = "        run: make resource-order-check-test RUNNER_MODE=host\n"
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
		assert.Empty(t, checkRequiredOrderJob("g.yaml", j, pre))
	})

	t.Run("job の if: が無くても違反0件", func(t *testing.T) {
		t.Parallel()

		j, pre := job(t, replaced(t, gatesWorkflow, jobIf, ""))
		assert.Empty(t, checkRequiredOrderJob("g.yaml", j, pre))
	})

	t.Run("ブロックの if: でも同じ式なら違反0件", func(t *testing.T) {
		t.Parallel()

		j, pre := job(t, replaced(t, gatesWorkflow, jobIf, "    if: >-\n      !cancelled() &&\n      needs.changes.outputs.relevant != 'false'\n"))
		assert.Empty(t, checkRequiredOrderJob("g.yaml", j, pre))
	})

	for name, c := range map[string]struct{ from, to, want string }{
		"検査の step の continue-on-error: は違反":    {check, check + "        continue-on-error: true\n", "順序の検査の step に if:・continue-on-error:"},
		"自己テストの step の continue-on-error: は違反": {self, self + "        continue-on-error: true\n", "順序の検査の step に if:・continue-on-error:"},
		"検査の step の if: false は違反":             {check, check + "        if: false\n", "順序の検査の step に if:"},
		"検査の step の shell: は違反":                {check, check + "        shell: sh -c true {0}\n", "shell:"},
		"検査の step の working-directory: は違反":    {check, check + "        working-directory: /tmp\n", "working-directory:"},
		"job の if: false は違反":                  {jobIf, "    if: false\n", "if: は ${{ !cancelled() && needs.changes.outputs.relevant != 'false' }} だけ"},
		"job の if: ${{ false }} は違反":           {jobIf, "    if: ${{ false }}\n", "if: は ${{"},
		"job の if: always() を足せば違反":            {jobIf, "    if: ${{ always() && !cancelled() && needs.changes.outputs.relevant != 'false' }}\n", "if: は ${{"},
		"job の if: always() だけは違反":             {jobIf, "    if: ${{ always() }}\n", "if: は ${{"},
		"job の if: の || は違反":                   {jobIf, "    if: ${{ !cancelled() && needs.changes.outputs.relevant != 'false' || false }}\n", "if: は ${{"},
		"job の if: の否定の差し替えは違反":                {jobIf, "    if: ${{ !cancelled() && !(needs.changes.outputs.relevant != 'false') }}\n", "if: は ${{"},
		"job の if: の比較の差し替えは違反":                {jobIf, "    if: ${{ !cancelled() && needs.changes.outputs.relevant == 'false' }}\n", "if: は ${{"},
		"job の continue-on-error: は違反":         {header, header + "    continue-on-error: true\n", "必須検査の job に continue-on-error:"},
		"job の env: は違反":                       {header, header + "    env:\n      X: y\n", "必須検査の job に env:"},
		"job の container: は違反":                 {header, header + "    container: alpine\n", "必須検査の job に container:"},
		"workflow の env: は違反":                  {"permissions:\n  contents: read\n", "permissions:\n  contents: read\nenv:\n  X: y\n", "workflow に defaults: と env:"},
		"MAKEFLAGS は違反":                        {self, self + "        env:\n          MAKEFLAGS: -n\n", "MAKEFLAGS と MAKEFILES"},
		"GITHUB_ENV への書き込みは違反":                 {"        run: make tf-lint RUNNER_MODE=host\n", "        run: echo X=y >> \"$GITHUB_ENV\"\n", "GITHUB_PATH と GITHUB_ENV"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			j, pre := job(t, replaced(t, gatesWorkflow, c.from, c.to))
			assert.Contains(t, messages(checkRequiredOrderJob("g.yaml", j, pre)), c.want)
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

func Test_checkPullRequestOrder(t *testing.T) {
	t.Parallel()

	apply := []groupRecord{{file: "a.yaml", root: "admin-identity"}}
	unlock := []groupRecord{{file: "u.yaml", root: "admin-identity", unlock: true}}

	assert.Empty(t, checkPullRequestOrder("wf", apply, 1), "Pull Request の側に在れば違反0件")
	assert.Empty(t, checkPullRequestOrder("wf", unlock, 0), "apply の job が無ければ求めない")
	assert.Empty(t, checkPullRequestOrder("wf", nil, 0), "apply の job が無ければ求めない")
	assert.Contains(t, messages(checkPullRequestOrder("wf", append(unlock, apply...), 0)), "make resource-order-check-test と make resource-order-check を走らせるもの")
}

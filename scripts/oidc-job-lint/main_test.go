package main

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/lintreport"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/testenv"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/workflow"
)

const envDecl = `[{"name":"admin-plan"},{"name":"admin-apply"},{"name":"admin-apply-identity"}]`

var envs = []string{"admin-plan", "admin-apply", "admin-apply-identity"}

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
	"  apply-admin-identity:",
	"    needs: apply-infra-code-gate",
	"    if: ${{ needs.apply-infra-code-gate.result == 'success' && github.ref_name == github.event.repository.default_branch }}",
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
	envFile := filepath.Join(dir, "environments.json")
	require.NoError(t, os.WriteFile(envFile, []byte(decl), 0o600))

	return wf, envFile
}

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
		require.NoError(t, run(wf, env, &out))
		assert.Contains(t, out.String(), "job 2 件")
	})

	t.Run("対象の job が無ければ errNoTarget", func(t *testing.T) {
		t.Parallel()

		wf, env := repoWith(t, map[string]string{"x.yaml": "on: push\njobs:\n  a:\n    steps:\n      - run: echo\n"}, envDecl)
		require.ErrorIs(t, run(wf, env, &bytes.Buffer{}), errNoTarget)
	})

	t.Run("workflow を読めなければ失敗する", func(t *testing.T) {
		t.Parallel()
		testenv.RequireNonRoot(t, "特権実行では権限の無いファイルも読めるため検証できない")

		wf, env := repoWith(t, map[string]string{"plan.yaml": planWorkflow, "apply.yml": applyWorkflow}, envDecl)
		require.NoError(t, os.Chmod(filepath.Join(wf, "apply.yml"), 0))
		err := run(wf, env, &bytes.Buffer{})
		require.ErrorIs(t, err, fs.ErrPermission)
		assert.Contains(t, err.Error(), "apply.yml の読み取り")
	})

	t.Run("workflow が1つも無ければ errNoTarget", func(t *testing.T) {
		t.Parallel()

		wf, env := repoWith(t, nil, envDecl)
		require.ErrorIs(t, run(wf, env, &bytes.Buffer{}), errNoTarget)
	})

	t.Run("対象が無くても違反があれば違反を報告する", func(t *testing.T) {
		t.Parallel()

		wf, env := repoWith(t, map[string]string{"x.yaml": "on: push\npermissions:\n  id-token: write\njobs:\n  a:\n    steps:\n      - run: echo\n"}, envDecl)
		var out bytes.Buffer
		require.ErrorIs(t, run(wf, env, &out), errViolations)
		assert.Contains(t, out.String(), "workflow 全体の permissions に id-token")
	})

	t.Run("違反があれば一覧を出して errViolations", func(t *testing.T) {
		t.Parallel()

		bad := strings.Replace(planWorkflow, "    needs: infra-code-gate\n", "", 1)
		wf, env := repoWith(t, map[string]string{"plan.yaml": bad}, envDecl)
		var out bytes.Buffer
		require.ErrorIs(t, run(wf, env, &out), errViolations)
		assert.Contains(t, out.String(), "make infra-code-gate を走らせる job")
	})

	t.Run("workflow のディレクトリが無ければ失敗する", func(t *testing.T) {
		t.Parallel()

		_, env := repoWith(t, nil, envDecl)
		err := run(filepath.Join(t.TempDir(), "missing"), env, &bytes.Buffer{})
		require.Error(t, err)
		require.NotErrorIs(t, err, errNoTarget)
	})

	t.Run("environments.json を読めなければ失敗する", func(t *testing.T) {
		t.Parallel()

		wf, _ := repoWith(t, map[string]string{"plan.yaml": planWorkflow}, envDecl)
		require.ErrorIs(t, run(wf, filepath.Join(t.TempDir(), "missing.json"), &bytes.Buffer{}), errEnvDecl)
	})

	t.Run("別の workflow の検出の job は needs に数えない", func(t *testing.T) {
		t.Parallel()

		apply := replaced(t, applyWorkflow, "    needs: apply-infra-code-gate\n", "    needs: infra-code-gate\n")
		wf, env := repoWith(t, map[string]string{"plan.yaml": planWorkflow, "apply.yml": apply}, envDecl)
		var out bytes.Buffer
		require.ErrorIs(t, run(wf, env, &out), errViolations)
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
		require.Len(t, f, 1)
		assert.Contains(t, f[0].Message, "長期のアクセスキー")
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

		return jobOf(t, strings.Replace(planWorkflow, from, to, 1), "plan-admin-identity")
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
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Contains(t, messages(checkJob("p.yaml", jobWith(t, c.from, c.to), planPerms, envs, w)), c.want)
		})
	}

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

		return jobOf(t, strings.Replace(planWorkflow, from, to, 1), "plan-admin-identity")
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

		src := strings.Replace(applyWorkflow, from, to, 1)
		require.NotEqual(t, applyWorkflow, src, "置き換え元が見つからない")

		return jobOf(t, src, "apply-admin-identity"), infoOf(src)
	}

	t.Run("規則を満たせば違反0件", func(t *testing.T) {
		t.Parallel()

		assert.Empty(t, checkApply(job, credOf(job), w, formatted))
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

		src := strings.Replace(applyWorkflow, from, to, 1)
		require.NotEqual(t, applyWorkflow, src, "置き換え元が見つからない")

		return jobOf(t, src, "apply-admin-identity"), infoOf(src)
	}

	t.Run("検出の job に依り、if: がその成功を見れば違反0件", func(t *testing.T) {
		t.Parallel()

		assert.Empty(t, checkDetection(jobOf(t, applyWorkflow, "apply-admin-identity"), w.jobs, formatted))
	})

	t.Run("if: が無ければ既定の成功の条件で足りる", func(t *testing.T) {
		t.Parallel()

		j, aw := applyWith(t, "    if: ${{ needs.apply-infra-code-gate.result == 'success' && github.ref_name == github.event.repository.default_branch }}\n", "")
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

		j, aw := applyWith(t, "    if: ${{ needs.apply-infra-code-gate.result == 'success' && github.ref_name == github.event.repository.default_branch }}\n",
			"    if: >-\n      needs.apply-infra-code-gate.result == 'success' &&\n      github.ref_name == github.event.repository.default_branch\n")
		assert.Empty(t, checkDetection(j, aw.jobs, formatted))
	})

	t.Run("検出の step の working-directory: は違反", func(t *testing.T) {
		t.Parallel()

		j, aw := applyWith(t, "      - run: make infra-code-gate RUNNER_MODE=host\n", "      - run: make infra-code-gate RUNNER_MODE=host\n        working-directory: /tmp\n")
		assert.Contains(t, messages(checkDetection(j, aw.jobs, formatted)), "working-directory:")
	})
}

func Test_checkRefusal(t *testing.T) {
	t.Parallel()

	planWith := func(t *testing.T, from, to string) workflow.Job {
		t.Helper()

		src := strings.Replace(planWorkflow, from, to, 1)
		require.NotEqual(t, planWorkflow, src, "置き換え元が見つからない")

		return jobOf(t, src, "plan-admin-identity")
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

		assert.Empty(t, checkTerraform(planWith(t, "      - run: echo aqua:hashicorp/terraform\n"), ciPlan, "plan", formatted))
	})

	t.Run("入口が無ければ違反", func(t *testing.T) {
		t.Parallel()

		job := jobOf(t, strings.Replace(planWorkflow, "make ci-plan-admin-identity", "echo", 1), "plan-admin-identity")
		assert.Contains(t, messages(checkTerraform(job, ciPlan, "plan", formatted)), "make ci-plan-* がありません")
	})
}

func Test_detects(t *testing.T) {
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
		"変数の代入は認める":                  {"      - run: make RUNNER_MODE=host infra-code-gate V=1\n", true},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, c.want, detects(jobOf(t, "jobs:\n  a:\n    steps:\n"+c.step, "a")))
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
		"make と target":        {"      - run: make ci-plan-admin-identity", true},
		"前後の変数の代入":             {"        run: make V=1 ci-plan-admin-identity RUNNER_MODE=host", true},
		"引用符付きの run キー":        {"      - \"run\": make ci-plan-admin-identity", true},
		"オプションは当たらない":          {"      - run: make -n ci-plan-admin-identity", false},
		"後続のコマンドは当たらない":        {"      - run: make ci-plan-admin-identity && true", false},
		"step の桁でなければ当たらない":    {"    run: make ci-plan-admin-identity", false},
		"run: の値が make で始まらない": {"      - run: env make ci-plan-admin-identity", false},
		"target が2つは当たらない":     {"      - run: make ci-plan-a ci-plan-b", false},
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

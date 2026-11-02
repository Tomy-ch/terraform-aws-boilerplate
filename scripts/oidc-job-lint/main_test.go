package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/lintreport"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/workflow"
)

const envDecl = `[{"name":"admin-plan"},{"name":"admin-apply"},{"name":"admin-apply-identity"}]`

var envs = []string{"admin-plan", "admin-apply", "admin-apply-identity"}

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
	"      - name: Run terraform plan through make",
	"        run: make ci-aws-credentials CI_ENV=admin CI_ROLE=plan RUNNER_MODE=host",
	"      - uses: ./.github/actions/setup-mise",
	"        with:",
	"          tools: aqua:hashicorp/terraform",
	"      - run: make ci-plan-admin-identity RUNNER_MODE=host",
	"",
}, "\n")

var applyWorkflow = strings.Join([]string{
	"name: Terraform Apply",
	"",
	"on:",
	"  push:",
	"    branches: [release/v0.1.0]",
	"  workflow_dispatch:",
	"",
	"permissions:",
	"  contents: read",
	"",
	"jobs:",
	"  apply-admin-identity:",
	"    runs-on: ubuntu-24.04",
	"    environment:",
	"      name: admin-apply-identity",
	"    concurrency:",
	"      group: admin-identity",
	"      cancel-in-progress: false",
	"    permissions:",
	"      contents: read",
	"      id-token: write",
	"    steps:",
	"      - run: make ci-aws-credentials CI_ENV=admin CI_ROLE=apply-identity RUNNER_MODE=host",
	"",
}, "\n")

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
		assert.Contains(t, out.String(), "needs に infra-code-gate")
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

	on, onLine := map[string][]workflow.Line{"pull_request": nil}, 1
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
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Contains(t, messages(checkJob("p.yaml", jobWith(t, c.from, c.to), envs, on, onLine)), c.want)
		})
	}

	t.Run("ci-aws-credentials を2回呼べば違反", func(t *testing.T) {
		t.Parallel()

		job := jobWith(t, "      - run: make ci-plan-admin-identity", "      - run: make ci-aws-credentials CI_ENV=admin CI_ROLE=plan\n      - run: make ci-plan-admin-identity")
		assert.Contains(t, messages(checkJob("p.yaml", job, envs, on, onLine)), "（2 回）")
	})

	t.Run("規則を満たす job は違反0件", func(t *testing.T) {
		t.Parallel()

		assert.Empty(t, checkJob("p.yaml", jobOf(t, planWorkflow, "plan-admin-identity"), envs, on, onLine))
	})

	t.Run("apply の役割は apply の規則で見る", func(t *testing.T) {
		t.Parallel()

		job := jobOf(t, applyWorkflow, "apply-admin-identity")
		f := checkJob("a.yaml", job, envs, map[string][]workflow.Line{"pull_request": nil}, 1)
		assert.Contains(t, messages(f), "push と workflow_dispatch だけ")
	})
}

func Test_checkPlan(t *testing.T) {
	t.Parallel()

	on := map[string][]workflow.Line{"pull_request": nil}
	finding := func(line int, format string, _ ...any) lintreport.Finding {
		return lintreport.Finding{Line: line, Message: format}
	}
	credOf := func(job workflow.Job) workflow.Line {
		for _, l := range job.Lines {
			if credentials.MatchString(l.Text) {
				return l
			}
		}

		return workflow.Line{}
	}
	planWith := func(t *testing.T, from, to string) workflow.Job {
		t.Helper()

		return jobOf(t, strings.Replace(planWorkflow, from, to, 1), "plan-admin-identity")
	}

	t.Run("規則を満たせば違反0件", func(t *testing.T) {
		t.Parallel()

		job := jobOf(t, planWorkflow, "plan-admin-identity")
		assert.Empty(t, checkPlan(job, credOf(job), on, 1, finding))
	})

	t.Run("pull_request 以外の起動は違反", func(t *testing.T) {
		t.Parallel()

		job := jobOf(t, planWorkflow, "plan-admin-identity")
		f := checkPlan(job, credOf(job), map[string][]workflow.Line{"pull_request": nil, "push": nil}, 1, finding)
		assert.Contains(t, messages(f), "pull_request だけ")
	})

	t.Run("needs に検出が無ければ違反", func(t *testing.T) {
		t.Parallel()

		job := planWith(t, "needs: infra-code-gate", "needs: [changes]")
		assert.Contains(t, messages(checkPlan(job, credOf(job), on, 1, finding)), "needs に %s")
	})

	t.Run("拒否の step が無ければ違反", func(t *testing.T) {
		t.Parallel()

		job := planWith(t, "id: refuse-untrusted", "id: other")
		assert.Contains(t, messages(checkPlan(job, credOf(job), on, 1, finding)), "資格情報を取る前に")
	})

	t.Run("拒否の step が資格情報の後なら違反", func(t *testing.T) {
		t.Parallel()

		src := strings.Replace(planWorkflow, "      - name: Refuse untrusted Pull Requests\n        id: refuse-untrusted\n        run: test \"$HEAD_REPO_ID\" = \"$REPO_ID\"\n", "", 1)
		src = strings.Replace(src, "      - run: make ci-plan-admin-identity", "      - id: refuse-untrusted\n        run: exit 1\n      - run: make ci-plan-admin-identity", 1)
		job := jobOf(t, src, "plan-admin-identity")
		assert.Contains(t, messages(checkPlan(job, credOf(job), on, 1, finding)), "資格情報を取る前に")
	})

	t.Run("拒否の step の if: は違反", func(t *testing.T) {
		t.Parallel()

		job := planWith(t, "        id: refuse-untrusted\n", "        id: refuse-untrusted\n        if: github.event_name == 'pull_request'\n")
		assert.Contains(t, messages(checkPlan(job, credOf(job), on, 1, finding)), "if: を置かない")
	})

	t.Run("make ci-plan-* 以外の terraform は違反", func(t *testing.T) {
		t.Parallel()

		job := planWith(t, "      - run: make ci-plan-admin-identity RUNNER_MODE=host\n", "      - run: make ci-plan-admin-identity RUNNER_MODE=host\n      - run: terraform -chdir=env/admin/identity plan\n")
		assert.Contains(t, messages(checkPlan(job, credOf(job), on, 1, finding)), "make ci-plan-* でだけ")
	})

	t.Run("make tf- の terraform も違反", func(t *testing.T) {
		t.Parallel()

		job := planWith(t, "      - run: make ci-plan-admin-identity RUNNER_MODE=host\n", "      - run: make ci-plan-admin-identity RUNNER_MODE=host\n      - run: make -s tf-validate\n")
		assert.Contains(t, messages(checkPlan(job, credOf(job), on, 1, finding)), "make ci-plan-* でだけ")
	})

	t.Run("make ci-plan-* が無ければ違反", func(t *testing.T) {
		t.Parallel()

		job := planWith(t, "make ci-plan-admin-identity", "echo")
		assert.Contains(t, messages(checkPlan(job, credOf(job), on, 1, finding)), "make ci-plan-* がありません")
	})
}

func Test_checkApply(t *testing.T) {
	t.Parallel()

	finding := func(line int, format string, _ ...any) lintreport.Finding {
		return lintreport.Finding{Line: line, Message: format}
	}
	job := jobOf(t, applyWorkflow, "apply-admin-identity")
	on, onLine := triggers(workflow.SplitJobs(applyWorkflow).Preamble)

	t.Run("push と入力の無い workflow_dispatch と concurrency があれば違反0件", func(t *testing.T) {
		t.Parallel()

		assert.Empty(t, checkApply(job, on, onLine, finding))
	})

	t.Run("pull_request の起動は違反", func(t *testing.T) {
		t.Parallel()

		f := checkApply(job, map[string][]workflow.Line{"push": nil, "pull_request": nil}, 1, finding)
		assert.Contains(t, messages(f), "push と workflow_dispatch だけ")
	})

	t.Run("起動条件が読めなければ違反", func(t *testing.T) {
		t.Parallel()

		assert.Contains(t, messages(checkApply(job, map[string][]workflow.Line{}, 1, finding)), "push と workflow_dispatch だけ")
	})

	t.Run("workflow_dispatch の inputs は違反", func(t *testing.T) {
		t.Parallel()

		src := strings.Replace(applyWorkflow, "  workflow_dispatch:\n", "  workflow_dispatch:\n    inputs:\n      ref:\n        type: string\n", 1)
		on, onLine := triggers(workflow.SplitJobs(src).Preamble)
		assert.Contains(t, messages(checkApply(job, on, onLine, finding)), "inputs を置かない")
	})

	t.Run("cancel-in-progress: false が無ければ違反", func(t *testing.T) {
		t.Parallel()

		bad := jobOf(t, strings.Replace(applyWorkflow, "      cancel-in-progress: false\n", "      cancel-in-progress: true\n", 1), "apply-admin-identity")
		assert.Contains(t, messages(checkApply(bad, on, onLine, finding)), "concurrency")
	})
}

func Test_hasIDToken(t *testing.T) {
	t.Parallel()

	assert.True(t, hasIDToken([]string{"contents: read", "id-token: write"}))
	assert.True(t, hasIDToken([]string{`"id-token": 'write'`}))
	assert.False(t, hasIDToken([]string{"id-token: read"}))
	assert.False(t, hasIDToken(nil))
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

	assert.Equal(t, []string{"a", "b"}, triggerNames(map[string][]workflow.Line{"b": nil, "a": nil}))
	assert.Empty(t, triggerNames(nil))
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
	assert.True(t, hasStepID(steps[0], "refuse-untrusted"))
	assert.True(t, hasStepID(steps[1], "other"))
	assert.False(t, hasStepID(steps[1], "refuse-untrusted"))
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
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, c.want, cancelsNothing(jobOf(t, "jobs:\n  a:\n"+c.body, "a")))
		})
	}
}

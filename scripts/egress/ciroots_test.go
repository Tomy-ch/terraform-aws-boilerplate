package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/workflow"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bindingSSOT は root "env/t/identity" が a.yaml:one を名指す宣言。
func bindingSSOT(t *testing.T) *ssot {
	t.Helper()
	return mustParse(t, "[job.\"a.yaml:one\"]\nclasses = []\n[job.\"a.yaml:two\"]\nclasses = []\n[root.\"env/t/identity\"]\njobs = [\"a.yaml:one\"]\n")
}

// makeDryRun は実物のリポジトリの直下で make -n を走らせ、展開した recipe を返す。
func makeDryRun(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "make", append([]string{"-C", filepath.Join("..", ".."), "-n", "--no-print-directory"}, args...)...)
	cmd.Env = slices.DeleteFunc(os.Environ(), func(kv string) bool {
		return strings.HasPrefix(kv, "MAKEFLAGS=") || strings.HasPrefix(kv, "MFLAGS=") || strings.HasPrefix(kv, "MAKELEVEL=")
	})
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	return string(out)
}

func ciWorkflow(jobID, run string) string {
	return workflowHead + "  " + jobID + ":\n    steps:\n      - name: Run\n        run: " + run + "\n"
}

func Test_ssot_checkCIRoots(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("ジョブが実行する root と宣言が一致すれば、ジョブごとに root で Terraform を実行するかを返す", func(t *testing.T) {
			t.Parallel()
			repo := t.TempDir()
			writeFile(t, repo, ciMakefile, testCIMake)
			writeFile(t, repo, ".github/workflows/a.yaml", ciWorkflow("one", "make ci-plan-t-identity"))
			got, err := bindingSSOT(t).checkCIRoots(repo, []string{"a.yaml"})
			require.NoError(t, err)
			assert.Equal(t, map[string]rootRuns{"a.yaml:one": {"env/t/identity": true}}, got)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("リポジトリを開けなければ弾く", func(t *testing.T) {
			t.Parallel()
			_, err := bindingSSOT(t).checkCIRoots(filepath.Join(t.TempDir(), "missing"), nil)
			require.ErrorIs(t, err, os.ErrNotExist)
		})

		t.Run("ci.mk が無ければ弾く", func(t *testing.T) {
			t.Parallel()
			_, err := bindingSSOT(t).checkCIRoots(t.TempDir(), nil)
			require.ErrorIs(t, err, os.ErrNotExist)
		})

		t.Run("ci.mk を解釈できなければ弾く", func(t *testing.T) {
			t.Parallel()
			repo := t.TempDir()
			writeFile(t, repo, ciMakefile, "all:\n")
			_, err := bindingSSOT(t).checkCIRoots(repo, nil)
			require.ErrorIs(t, err, errCIMake)
		})

		t.Run("workflow を読めなければ弾く", func(t *testing.T) {
			t.Parallel()
			repo := t.TempDir()
			writeFile(t, repo, ciMakefile, testCIMake)
			_, err := bindingSSOT(t).checkCIRoots(repo, []string{"missing.yaml"})
			require.ErrorIs(t, err, os.ErrNotExist)
		})

		t.Run("宣言の名指すジョブが root を実行していなければ弾く", func(t *testing.T) {
			t.Parallel()
			repo := t.TempDir()
			writeFile(t, repo, ciMakefile, testCIMake)
			writeFile(t, repo, ".github/workflows/a.yaml", ciWorkflow("one", "make lint"))
			_, err := bindingSSOT(t).checkCIRoots(repo, []string{"a.yaml"})
			require.ErrorIs(t, err, errRootJobIdle)
		})
	})
}

func Test_parseCIMake(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("入口ごとに実行する root のパスを返す", func(t *testing.T) {
			t.Parallel()
			got, err := parseCIMake(testCIMake + "ci-apply-t-workload:\n\t@$(call CI_APPLY,t,workload)\n")
			require.NoError(t, err)
			assert.Equal(t, map[string]ciEntry{
				"ci-plan-t-identity":  {root: "env/t/identity", terraform: true},
				"ci-apply-t-workload": {root: "env/t", terraform: true},
			}, got)
		})

		t.Run("lock の解除の入口も root に結びつけ、Terraform を実行しない入口とする", func(t *testing.T) {
			t.Parallel()
			got, err := parseCIMake(testCIMake + "ci-unlock-t-identity:\n\t@$(call CI_UNLOCK,t,identity)\n")
			require.NoError(t, err)
			assert.Equal(t, map[string]ciEntry{
				"ci-plan-t-identity":   {root: "env/t/identity", terraform: true},
				"ci-unlock-t-identity": {root: "env/t/identity"},
			}, got)
		})

		// lock の解除の job に型の宛先を求めない根拠は、CI_UNLOCK が terraform を呼ばないことにある。ci.mk の
		// 外の define も含めて展開した結果を、実物の ci.mk の入口ごとに make -n で突き合わせる。
		t.Run("実物の ci.mk の入口が terraform を呼ぶかが、Terraform を実行するかの区別と一致する", func(t *testing.T) {
			t.Parallel()
			const sentinel = "tf-bin-sentinel"
			mk, err := openRoot(t, filepath.Join("..", "..")).ReadFile(ciMakefile)
			require.NoError(t, err)
			targets, err := parseCIMake(string(mk))
			require.NoError(t, err)
			kinds := map[bool]int{}
			for target, e := range targets {
				out := makeDryRun(t, target, "TF_BIN="+sentinel)
				assert.Equal(t, e.terraform, strings.Contains(out, sentinel), target)
				kinds[e.terraform]++
			}
			assert.Positive(t, kinds[true], "Terraform を実行する入口が無い")
			assert.Positive(t, kinds[false], "Terraform を実行しない入口が無い")
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		// 異常系はどれも errCIMake を返すので、落ちた理由を文で区別する。重複は件数の突き合わせでも落ちる。
		for name, c := range map[string]struct{ data, reason string }{
			"入口が 1 件も無い":                        {"ci-aws-credentials:\n\t@$(call CI_AWS_CREDENTIALS,a,b)\n", "入口が 1 件もありません"},
			"見出しの次の行が呼び出しでない":                   {"ci-plan-t-identity:\n\t@echo x\n", "次の行が $(call CI_PLAN,"},
			"見出しがファイルの最終行":                      {"ci-plan-t-identity:", "次の行が $(call CI_PLAN,"},
			"plan の入口が CI_APPLY を呼ぶ":            {"ci-plan-t-identity:\n\t@$(call CI_APPLY,t,identity)\n", "次の行が $(call CI_PLAN,"},
			"lock の解除の入口が CI_APPLY を呼ぶ":         {"ci-unlock-t-identity:\n\t@$(call CI_APPLY,t,identity)\n", "次の行が $(call CI_UNLOCK,"},
			"見出しを経ない CI_UNLOCK の字面がある":          {testCIMake + "x: y\n\t@$(call CI_UNLOCK,t,identity)\n", "読めた入口 1 件 / 呼び出しの字面 2 件"},
			"root の種類が identity / workload でない": {"ci-plan-t-other:\n\t@$(call CI_PLAN,t,other)\n", "次の行が $(call CI_PLAN,"},
			"入口が重複している":                         {testCIMake + testCIMake, "ci-plan-t-identity が重複しています"},
			"見出しを経ない呼び出しの字面がある":                 {testCIMake + "x: y\n\t@$(call CI_PLAN,t,identity)\n", "読めた入口 1 件 / 呼び出しの字面 2 件"},
		} {
			t.Run(name+"なら弾く", func(t *testing.T) {
				t.Parallel()
				_, err := parseCIMake(c.data)
				require.ErrorIs(t, err, errCIMake)
				assert.Contains(t, err.Error(), c.reason)
			})
		}
	})
}

func Test_rootPath(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("identity は env の下の identity を返す", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, "env/admin/identity", rootPath("admin", "identity"))
		})

		t.Run("workload は env の直下を返す", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, "env/admin", rootPath("admin", "workload"))
		})

		// 配置の規則は local.mk の TF_LOCAL_DIR も持つ。片方だけが動くと、CI が実行する dir と型を集める dir が
		// ずれるので、実物の ci.mk の入口を make -n で展開して突き合わせる（ADR-0702 決定17）。
		t.Run("実物の ci.mk の入口が terraform を実行する dir と一致する", func(t *testing.T) {
			t.Parallel()
			mk, err := openRoot(t, filepath.Join("..", "..")).ReadFile(ciMakefile)
			require.NoError(t, err)
			targets, err := parseCIMake(string(mk))
			require.NoError(t, err)
			dirRe := regexp.MustCompile(`dir="\./([^"]+)"`)
			for target, e := range targets {
				out := makeDryRun(t, target)
				m := dirRe.FindStringSubmatch(out)
				require.NotNil(t, m, out)
				assert.Equal(t, e.root, m[1], target)
			}
		})
	})
}

func Test_ranRoots(t *testing.T) {
	t.Parallel()

	targets := map[string]ciEntry{
		"ci-plan-t-identity":   {root: "env/t/identity", terraform: true},
		"ci-apply-t-identity":  {root: "env/t/identity", terraform: true},
		"ci-plan-t-workload":   {root: "env/t", terraform: true},
		"ci-unlock-t-identity": {root: "env/t/identity"},
	}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("ジョブごとに実行する root を重複なく返し、コメントの中の名前は数えない", func(t *testing.T) {
			t.Parallel()
			repo := t.TempDir()
			writeFile(t, repo, ".github/workflows/a.yaml", ciWorkflow("one", "make ci-plan-t-identity ci-apply-t-identity ci-plan-t-workload")+
				"      # make ci-plan-unknown\n"+
				"  two:\n    steps:\n      - name: Lint\n        run: make lint\n")
			got, err := ranRoots(openRoot(t, repo), []string{"a.yaml"}, targets)
			require.NoError(t, err)
			assert.Equal(t, map[string]rootRuns{"a.yaml:one": {"env/t/identity": true, "env/t": true}}, got)
		})

		t.Run("lock の解除の入口だけで触れる root は Terraform を実行しない root とする", func(t *testing.T) {
			t.Parallel()
			repo := t.TempDir()
			writeFile(t, repo, ".github/workflows/a.yaml", ciWorkflow("one", "make ci-unlock-t-identity"))
			got, err := ranRoots(openRoot(t, repo), []string{"a.yaml"}, targets)
			require.NoError(t, err)
			assert.Equal(t, map[string]rootRuns{"a.yaml:one": {"env/t/identity": false}}, got)
		})

		// 区別は宣言ではなく呼ぶ入口から導くので、lock の解除のジョブが plan / apply も呼べば、その root で
		// Terraform を実行するジョブに戻り、型の宛先を求められる。入口の順に依らない。
		for _, c := range []struct{ name, run string }{
			{"lock の解除の後に plan", "make ci-unlock-t-identity ci-plan-t-identity"},
			{"apply の後に lock の解除", "make ci-apply-t-identity ci-unlock-t-identity"},
		} {
			t.Run(c.name+"の入口を呼ぶジョブは、その root で Terraform を実行するとみなす", func(t *testing.T) {
				t.Parallel()
				repo := t.TempDir()
				writeFile(t, repo, ".github/workflows/a.yaml", ciWorkflow("one", c.run))
				got, err := ranRoots(openRoot(t, repo), []string{"a.yaml"}, targets)
				require.NoError(t, err)
				assert.Equal(t, map[string]rootRuns{"a.yaml:one": {"env/t/identity": true}}, got)
			})
		}

		t.Run("step の名前と、入口の名前を一部に含む別の名前は数えない", func(t *testing.T) {
			t.Parallel()
			repo := t.TempDir()
			writeFile(t, repo, ".github/workflows/a.yaml", workflowHead+"  one:\n    steps:\n      - name: Plan ci-plan-t-identity\n        run: make tf-ci-plan-t-identity\n")
			got, err := ranRoots(openRoot(t, repo), []string{"a.yaml"}, targets)
			require.NoError(t, err)
			assert.Empty(t, got)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for name, c := range map[string]struct{ run, token string }{
			"式で組んだ名前":       {"make ci-plan-${{ matrix.env }}-identity", ": ci-plan-（"},
			"名前に使わない字が続く名前": {"make ci-plan-t-identity_v2", ": ci-plan-t-identity_v2（"},
		} {
			t.Run(name+"を、切り詰めずに未知の入口として弾く", func(t *testing.T) {
				t.Parallel()
				repo := t.TempDir()
				writeFile(t, repo, ".github/workflows/a.yaml", ciWorkflow("one", c.run))
				_, err := ranRoots(openRoot(t, repo), []string{"a.yaml"}, targets)
				require.ErrorIs(t, err, errCITargetUnknown)
				assert.Contains(t, err.Error(), c.token)
			})
		}

		t.Run("job の外で入口を名指せば弾く", func(t *testing.T) {
			t.Parallel()
			repo := t.TempDir()
			writeFile(t, repo, ".github/workflows/a.yaml", "name: T\nenv:\n  TARGET: ci-plan-t-identity\n\njobs:\n"+
				strings.TrimPrefix(ciWorkflow("one", "make \"$TARGET\""), workflowHead))
			_, err := ranRoots(openRoot(t, repo), []string{"a.yaml"}, targets)
			require.ErrorIs(t, err, errCIOutsideJob)
			assert.Contains(t, err.Error(), "a.yaml 3 行目")
		})

		t.Run("ci.mk に無い入口を呼べば弾く", func(t *testing.T) {
			t.Parallel()
			repo := t.TempDir()
			writeFile(t, repo, ".github/workflows/a.yaml", ciWorkflow("one", "make ci-apply-t-workload"))
			_, err := ranRoots(openRoot(t, repo), []string{"a.yaml"}, targets)
			require.ErrorIs(t, err, errCITargetUnknown)
			assert.Contains(t, err.Error(), "ci-apply-t-workload")
		})
	})
}

func Test_entryTokens(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		for text, want := range map[string][]string{
			"        run: make ci-plan-a-identity ci-apply-a-identity RUNNER_MODE=host": {"ci-plan-a-identity", "ci-apply-a-identity"},
			"        run: make ci-plan-${{ x }}":                                        {"ci-plan-"},
			"        run: make ci-unlock-a-identity RUNNER_MODE=host":                   {"ci-unlock-a-identity"},
			"        run: make tf-ci-plan-a-identity lint":                              nil,
			"      - name: Plan ci-plan-a-identity":                                     nil,
			"        # make ci-plan-a-identity":                                         nil,
		} {
			t.Run(text+" から入口の名前を拾う", func(t *testing.T) {
				t.Parallel()
				assert.Equal(t, want, entryTokens(workflow.Line{Number: 1, Text: text}))
			})
		}
	})
}

func Test_ssot_checkBindings(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("実行する root と名指しが一致すれば成功する", func(t *testing.T) {
			t.Parallel()
			assert.NoError(t, bindingSSOT(t).checkBindings(map[string]rootRuns{"a.yaml:one": {"env/t/identity": true}}))
		})

		t.Run("lock の解除だけで root に触れるジョブも、名指しが一致すれば成功する", func(t *testing.T) {
			t.Parallel()
			assert.NoError(t, bindingSSOT(t).checkBindings(map[string]rootRuns{"a.yaml:one": {"env/t/identity": false}}))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("宣言に無い root を実行するジョブを弾く", func(t *testing.T) {
			t.Parallel()
			err := bindingSSOT(t).checkBindings(map[string]rootRuns{"a.yaml:one": {"env/t/identity": true, "env/t": true}})
			require.ErrorIs(t, err, errRootUnbound)
			assert.Contains(t, err.Error(), "env/t を実行します")
		})

		t.Run("root を名指されていないジョブが root を実行すれば弾く", func(t *testing.T) {
			t.Parallel()
			err := bindingSSOT(t).checkBindings(map[string]rootRuns{"a.yaml:one": {"env/t/identity": true}, "a.yaml:two": {"env/t/identity": false}})
			require.ErrorIs(t, err, errRootUnbound)
			assert.Contains(t, err.Error(), "a.yaml:two")
		})

		t.Run("名指されたジョブが root を実行していなければ弾く", func(t *testing.T) {
			t.Parallel()
			err := bindingSSOT(t).checkBindings(map[string]rootRuns{})
			require.ErrorIs(t, err, errRootJobIdle)
			assert.Contains(t, err.Error(), "a.yaml:one")
		})
	})
}

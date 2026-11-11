package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bindingSSOT は root "env/t/identity" が a.yaml:one を名指す宣言。
func bindingSSOT(t *testing.T) *ssot {
	t.Helper()
	return mustParse(t, "[job.\"a.yaml:one\"]\nclasses = []\n[job.\"a.yaml:two\"]\nclasses = []\n[root.\"env/t/identity\"]\njobs = [\"a.yaml:one\"]\n")
}

func ciWorkflow(jobID, run string) string {
	return workflowHead + "  " + jobID + ":\n    steps:\n      - name: Run\n        run: " + run + "\n"
}

func Test_ssot_checkCIRoots(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("ジョブが実行する root と宣言が一致すれば成功する", func(t *testing.T) {
			t.Parallel()
			repo := t.TempDir()
			writeFile(t, repo, ciMakefile, testCIMake)
			writeFile(t, repo, ".github/workflows/a.yaml", ciWorkflow("one", "make ci-plan-t-identity"))
			assert.NoError(t, bindingSSOT(t).checkCIRoots(repo, []string{"a.yaml"}))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("ci.mk が無ければ弾く", func(t *testing.T) {
			t.Parallel()
			require.Error(t, bindingSSOT(t).checkCIRoots(t.TempDir(), nil))
		})

		t.Run("ci.mk を解釈できなければ弾く", func(t *testing.T) {
			t.Parallel()
			repo := t.TempDir()
			writeFile(t, repo, ciMakefile, "all:\n")
			require.ErrorIs(t, bindingSSOT(t).checkCIRoots(repo, nil), errCIMake)
		})

		t.Run("workflow を読めなければ弾く", func(t *testing.T) {
			t.Parallel()
			repo := t.TempDir()
			writeFile(t, repo, ciMakefile, testCIMake)
			require.Error(t, bindingSSOT(t).checkCIRoots(repo, []string{"missing.yaml"}))
		})

		t.Run("宣言の名指すジョブが root を実行していなければ弾く", func(t *testing.T) {
			t.Parallel()
			repo := t.TempDir()
			writeFile(t, repo, ciMakefile, testCIMake)
			writeFile(t, repo, ".github/workflows/a.yaml", ciWorkflow("one", "make lint"))
			require.ErrorIs(t, bindingSSOT(t).checkCIRoots(repo, []string{"a.yaml"}), errRootJobIdle)
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
			assert.Equal(t, map[string]string{"ci-plan-t-identity": "env/t/identity", "ci-apply-t-workload": "env/t"}, got)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for name, data := range map[string]string{
			"入口が 1 件も無い":                        "ci-aws-credentials:\n\t@$(call CI_AWS_CREDENTIALS,a,b)\n",
			"見出しの次の行が呼び出しでない":                   "ci-plan-t-identity:\n\t@echo x\n",
			"見出しがファイルの最終行":                      "ci-plan-t-identity:",
			"plan の入口が CI_APPLY を呼ぶ":            "ci-plan-t-identity:\n\t@$(call CI_APPLY,t,identity)\n",
			"root の種類が identity / workload でない": "ci-plan-t-other:\n\t@$(call CI_PLAN,t,other)\n",
			"入口が重複している":                         testCIMake + testCIMake,
			"見出しを経ない呼び出しの字面がある":                 testCIMake + "x: y\n\t@$(call CI_PLAN,t,identity)\n",
		} {
			t.Run(name+"なら弾く", func(t *testing.T) {
				t.Parallel()
				_, err := parseCIMake(data)
				require.ErrorIs(t, err, errCIMake)
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
			for target, want := range targets {
				cmd := exec.CommandContext(t.Context(), "make", "-C", filepath.Join("..", ".."), "-n", "--no-print-directory", target)
				cmd.Env = slices.DeleteFunc(os.Environ(), func(kv string) bool {
					return strings.HasPrefix(kv, "MAKEFLAGS=") || strings.HasPrefix(kv, "MFLAGS=") || strings.HasPrefix(kv, "MAKELEVEL=")
				})
				out, err := cmd.CombinedOutput()
				require.NoError(t, err, string(out))
				m := dirRe.FindStringSubmatch(string(out))
				require.NotNil(t, m, string(out))
				assert.Equal(t, want, m[1], target)
			}
		})
	})
}

func Test_ranRoots(t *testing.T) {
	t.Parallel()

	targets := map[string]string{"ci-plan-t-identity": "env/t/identity", "ci-apply-t-identity": "env/t/identity", "ci-plan-t-workload": "env/t"}

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
			assert.Equal(t, map[string][]string{"a.yaml:one": {"env/t/identity", "env/t"}}, got)
		})

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

func Test_ssot_checkBindings(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("実行する root と名指しが一致すれば成功する", func(t *testing.T) {
			t.Parallel()
			assert.NoError(t, bindingSSOT(t).checkBindings(map[string][]string{"a.yaml:one": {"env/t/identity"}}))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("宣言に無い root を実行するジョブを弾く", func(t *testing.T) {
			t.Parallel()
			err := bindingSSOT(t).checkBindings(map[string][]string{"a.yaml:one": {"env/t/identity", "env/t"}})
			require.ErrorIs(t, err, errRootUnbound)
			assert.Contains(t, err.Error(), "env/t")
		})

		t.Run("root を名指されていないジョブが root を実行すれば弾く", func(t *testing.T) {
			t.Parallel()
			err := bindingSSOT(t).checkBindings(map[string][]string{"a.yaml:one": {"env/t/identity"}, "a.yaml:two": {"env/t/identity"}})
			require.ErrorIs(t, err, errRootUnbound)
			assert.Contains(t, err.Error(), "a.yaml:two")
		})

		t.Run("名指されたジョブが root を実行していなければ弾く", func(t *testing.T) {
			t.Parallel()
			err := bindingSSOT(t).checkBindings(map[string][]string{})
			require.ErrorIs(t, err, errRootJobIdle)
			assert.Contains(t, err.Error(), "a.yaml:one")
		})
	})
}

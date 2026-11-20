package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/cientry"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/workflow"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func bindingSSOT(t *testing.T) *ssot {
	t.Helper()
	return mustParse(t, "[job.\"a.yaml:one\"]\nclasses = []\n[job.\"a.yaml:two\"]\nclasses = []\n[root.\"env/t/identity\"]\njobs = [\"a.yaml:one\"]\n")
}

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
			writeFile(t, repo, cientry.Makefile, testCIMake)
			writeFile(t, repo, ".github/workflows/a.yaml", ciWorkflow("one", "make ci-plan-t-identity"))
			got, err := bindingSSOT(t).checkCIRoots(repo, []string{"a.yaml"})
			require.NoError(t, err)
			assert.Equal(t, map[string]rootRuns{"a.yaml:one": {"env/t/identity": true}}, got)
		})

		t.Run("登録簿の一部の種別の入口しか持たない ci.mk も読む", func(t *testing.T) {
			t.Parallel()
			repo := t.TempDir()
			writeFile(t, repo, cientry.Makefile, ciMakeFixture([3]string{cientry.Unlock, "t", "identity"}))
			writeFile(t, repo, ".github/workflows/a.yaml", ciWorkflow("one", "make ci-unlock-t-identity"))
			got, err := bindingSSOT(t).checkCIRoots(repo, []string{"a.yaml"})
			require.NoError(t, err)
			assert.Equal(t, map[string]rootRuns{"a.yaml:one": {"env/t/identity": false}}, got)
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
			writeFile(t, repo, cientry.Makefile, "all:\n")
			_, err := bindingSSOT(t).checkCIRoots(repo, nil)
			require.ErrorIs(t, err, cientry.ErrParse)
		})

		t.Run("ci.mk に登録簿に無い種別の入口があれば弾く", func(t *testing.T) {
			t.Parallel()
			repo := t.TempDir()
			writeFile(t, repo, cientry.Makefile, ciMakeFixture([3]string{cientry.Plan, "t", "identity"}, [3]string{"import", "t", "identity"}))
			writeFile(t, repo, ".github/workflows/a.yaml", ciWorkflow("one", "make ci-plan-t-identity"))
			_, err := bindingSSOT(t).checkCIRoots(repo, []string{"a.yaml"})
			require.ErrorIs(t, err, cientry.ErrKindUnknown)
			assert.Contains(t, err.Error(), "ci-import-t-identity")
		})

		t.Run("workflow を読めなければ弾く", func(t *testing.T) {
			t.Parallel()
			repo := t.TempDir()
			writeFile(t, repo, cientry.Makefile, testCIMake)
			_, err := bindingSSOT(t).checkCIRoots(repo, []string{"missing.yaml"})
			require.ErrorIs(t, err, os.ErrNotExist)
		})

		t.Run("宣言の名指すジョブが root を実行していなければ弾く", func(t *testing.T) {
			t.Parallel()
			repo := t.TempDir()
			writeFile(t, repo, cientry.Makefile, testCIMake)
			writeFile(t, repo, ".github/workflows/a.yaml", ciWorkflow("one", "make lint"))
			_, err := bindingSSOT(t).checkCIRoots(repo, []string{"a.yaml"})
			require.ErrorIs(t, err, errRootJobIdle)
		})
	})
}

func Test_ciEntries(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		for _, k := range cientry.Kinds() {
			t.Run(k.Name+" の入口を root のパスと、その種別が Terraform を実行するかに写す", func(t *testing.T) {
				t.Parallel()
				got := ciEntries([]cientry.Entry{
					{Target: "ci-" + k.Name + "-t-identity", Kind: k, Env: "t", Root: cientry.RootIdentity},
					{Target: "ci-" + k.Name + "-t-workload", Kind: k, Env: "t", Root: cientry.RootWorkload},
				})
				assert.Equal(t, map[string]ciEntry{
					"ci-" + k.Name + "-t-identity": {root: "env/t/identity", terraform: k.Terraform},
					"ci-" + k.Name + "-t-workload": {root: "env/t", terraform: k.Terraform},
				}, got)
			})
		}

		t.Run("入口が無ければ空を返す", func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, ciEntries(nil))
		})
	})
}

func Test_rootPath(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("identity は env の下の identity を返す", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, "env/admin/identity", rootPath("admin", cientry.RootIdentity))
		})

		t.Run("workload は env の直下を返す", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, "env/admin", rootPath("admin", cientry.RootWorkload))
		})

		// 配置の規則は local.mk の TF_LOCAL_DIR も持つ。片方だけが動くと、CI が実行する dir と型を集める dir が
		// ずれるので、実物の ci.mk の入口を make -n で展開して突き合わせる（ADR-0702 決定17）。
		t.Run("実物の ci.mk の入口が terraform を実行する dir と一致する", func(t *testing.T) {
			t.Parallel()
			entries, err := cientry.ParseRepo(filepath.Join("..", ".."))
			require.NoError(t, err)
			targets := ciEntries(entries)
			require.NotEmpty(t, targets)
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
		"ci-hold-t-identity":   {root: "env/t/identity", terraform: true},
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

		t.Run("lock を残す入口で触れる root は Terraform を実行する root とする", func(t *testing.T) {
			t.Parallel()
			repo := t.TempDir()
			writeFile(t, repo, ".github/workflows/a.yaml", ciWorkflow("one", "make ci-hold-t-identity"))
			got, err := ranRoots(openRoot(t, repo), []string{"a.yaml"}, targets)
			require.NoError(t, err)
			assert.Equal(t, map[string]rootRuns{"a.yaml:one": {"env/t/identity": true}}, got)
		})

		t.Run("ci.mk に無い lock を残す入口は未知の入口として落とす", func(t *testing.T) {
			t.Parallel()
			repo := t.TempDir()
			writeFile(t, repo, ".github/workflows/a.yaml", ciWorkflow("one", "make ci-hold-x"))
			_, err := ranRoots(openRoot(t, repo), []string{"a.yaml"}, targets)
			require.ErrorIs(t, err, errCITargetUnknown)
		})

		t.Run("lock の解除の入口だけで触れる root は Terraform を実行しない root とする", func(t *testing.T) {
			t.Parallel()
			repo := t.TempDir()
			writeFile(t, repo, ".github/workflows/a.yaml", ciWorkflow("one", "make ci-unlock-t-identity"))
			got, err := ranRoots(openRoot(t, repo), []string{"a.yaml"}, targets)
			require.NoError(t, err)
			assert.Equal(t, map[string]rootRuns{"a.yaml:one": {"env/t/identity": false}}, got)
		})

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
			"        run: make ci-hold-a-identity RUNNER_MODE=host":                     {"ci-hold-a-identity"},
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

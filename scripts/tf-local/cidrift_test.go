package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/branchdecl"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/cientry"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/ownedattrs"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/testenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	// driftSecret は plan の JSON の属性の値に入れる秘匿の印。どの出力にも現れてはならない。
	driftSecret = "DRIFT-SECRET-VALUE"
	deploySHA   = "89abcdef0123456789abcdef0123456789abcdef"
	branchesOK  = `{"deploy":[{"branch":"admin","env":"admin","from":["release/"]},{"branch":"production","release_base":true}],` +
		`"lines":{"release":"release/","hotfix":"hotfix/"},"line_version":"v<major>.<minor>.<patch>","revert":"revert/"}`
	ownedADR = "20. 一覧。\n\n    | resource type | 属性 | 発火側が行う操作 |\n    | --- | --- | --- |\n" +
		"    | `aws_ecs_service` | `task_definition` | 登録と更新 |\n\n"
)

func driftPlan(changes, drift string) string {
	return `{"format_version":"1.2","resource_changes":[` + changes + `],"resource_drift":[` + drift + `]}`
}

// before と after は JSON の値（object か null）。
func drifted(address, typ, actions, before, after string) string {
	return `{"address":"` + address + `","type":"` + typ + `","change":{"actions":[` + actions + `],"before":` + before + `,"after":` + after + `}}`
}

// driftMakefile は、roots（<env>-<root>）の drift の入口を持つ ci.mk を組む。
func driftMakefile(roots ...string) string {
	var b strings.Builder
	b.WriteString("define CI_DRIFT\nx\nendef\n")
	for _, r := range roots {
		env, root, _ := cientry.SplitEnvRoot(r)
		target := "ci-drift-" + r
		b.WriteString(".PHONY: " + target + " ## x\n" + target + ": TF_LOCAL_PROFILE := $(call CI_PROFILE," + env + ",plan)\n" +
			target + ":\n\t@$(call CI_DRIFT," + env + "," + string(root) + ")\n")
	}
	return b.String()
}

func writeRepo(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o700))
		require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
	}
}

func driftKnobs(extra map[string]string) map[string]string {
	k := map[string]string{
		stepS3List + ".out":       noLock(),
		stepCIPlanNoLock + ".out": planLog + summaryLine(0, 0),
		stepCIPlanNoLock + ".rc":  "0",
		stepCIShow + ".out":       driftPlan(change("a.b", `"no-op"`), ""),
	}
	for name, v := range extra {
		k[name] = v
	}
	return k
}

func newDriftFlow(t *testing.T, knobs map[string]string, extra ...string) *flow {
	t.Helper()
	f := newFlow(t, driftKnobs(knobs), extra...)
	writeRepo(t, f.d.root, map[string]string{branchdecl.File: branchesOK, ownedattrs.File: ownedADR,
		cientry.Makefile: driftMakefile("admin-identity", "admin-workload")})
	return f
}

// drift は ci.mk の CI_DRIFT と同じ順で段を起動する。prepare と init が落ちればそこで止まる。
func (f *flow) drift(root string) error {
	f.state = filepath.Join(filepath.Dir(f.state), "admin-"+root+".json")
	if err := f.step("ci-prepare", "-mode", ciModeDrift, "-project", "tabp", "-env", "admin", "-root", root,
		"-profile", "tabp-admin-ci-plan", "-base", f.base, "-header", f.header, "-state", f.state); err != nil {
		return err
	}
	if err := f.step("ci-init", "-state", f.state); err != nil {
		return err
	}
	_ = f.step("ci-drift-lock", "-at", "before", "-state", f.state)
	_ = f.step("ci-plan", "-state", f.state)
	_ = f.step("ci-drift-lock", "-at", "after", "-state", f.state)
	_ = f.step("ci-drift", "-state", f.state)
	_ = f.step("ci-upload", "-state", f.state)
	return f.step("ci-finish", "-state", f.state)
}

// secretFree は、秘匿の印が stdout・stderr・job summary・GITHUB_OUTPUT・エラー文のどれにも無いことを確かめる。
func (f *flow) secretFree() {
	f.t.Helper()
	f.logsFree()
	assert.NotContains(f.t, readKnob(f.t, filepath.Dir(f.summary), filepath.Base(f.summary)), driftSecret)
}

// logsFree は、秘匿の印と more が、公開されるログ（stdout・stderr）・GITHUB_OUTPUT・エラー文のどれにも無いことを確かめる。
func (f *flow) logsFree(more ...string) {
	f.t.Helper()
	stderr := f.d.stderr.(*bytes.Buffer).String()
	for _, v := range append([]string{driftSecret, planSecret, account}, more...) {
		assert.NotContains(f.t, f.stdout.String(), v)
		assert.NotContains(f.t, stderr, v)
		assert.NotContains(f.t, readKnob(f.t, filepath.Dir(f.output), filepath.Base(f.output)), v)
		for _, err := range f.errs {
			if err != nil {
				assert.NotContains(f.t, err.Error(), v)
			}
		}
	}
}

func Test_driftItem_String(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("属性があれば名前を並べる", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, "update aws_x.y: a, b", driftItem{Op: "update", Address: "aws_x.y", Attrs: []string{"a", "b"}}.String())
		})

		t.Run("属性が無ければ操作と address だけ", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, "delete aws_x.y", driftItem{Op: "delete", Address: "aws_x.y"}.String())
		})
	})
}

func Test_gitStep(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("stdin で値を渡し、stdout を返す", func(t *testing.T) {
			t.Parallel()
			f := newDriftFlow(t, map[string]string{stepGitRevParse + ".out": deploySHA + "\n"})
			out, err := gitStep(f.d, f.d.environ, stepGitRevParse, "in\n")
			require.NoError(t, err)
			assert.Equal(t, deploySHA+"\n", string(out))
			assert.Equal(t, "in\n", readKnob(t, f.k, stepGitRevParse+".stdin"))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("非ゼロで終われば drift の木を決められない", func(t *testing.T) {
			t.Parallel()
			f := newDriftFlow(t, map[string]string{stepGitFetch + ".rc": "128"})
			_, err := gitStep(f.d, f.d.environ, stepGitFetch, "")
			require.ErrorIs(t, err, errDriftTarget)
		})

		t.Run("シグナルで終われば道具を起動できない", func(t *testing.T) {
			t.Parallel()
			f := newDriftFlow(t, map[string]string{stepGitFetch + ".signal": ""})
			_, err := gitStep(f.d, f.d.environ, stepGitFetch, "")
			require.Error(t, err)
			assert.Equal(t, 2, exitCode(err))
		})
	})
}

func Test_catFileBlob(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("見出しの大きさの分だけ中身を返す", func(t *testing.T) {
			t.Parallel()
			got, err := catFileBlob([]byte(deploySHA + " blob 4\nab\nc\n"))
			require.NoError(t, err)
			assert.Equal(t, "ab\nc", string(got))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for name, in := range map[string]string{
			"object が無い":  deploySHA + ":x missing\n",
			"blob でない":    deploySHA + " tree 4\nabcd\n",
			"大きさが中身と合わない": deploySHA + " blob 9\nab\n",
			"大きさが数でない":    deploySHA + " blob x\nab\n",
			"空":           "",
			"末尾の改行が無い":    deploySHA + " blob 2\nab",
		} {
			t.Run(name+"なら失敗する", func(t *testing.T) {
				t.Parallel()
				_, err := catFileBlob([]byte(in))
				require.ErrorIs(t, err, errDriftTarget)
			})
		}
	})
}

func Test_deployBranch(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("env のデプロイブランチを宣言から返す", func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeRepo(t, root, map[string]string{branchdecl.File: branchesOK})
			got, err := deployBranch(root, "admin")
			require.NoError(t, err)
			assert.Equal(t, "admin", got)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("デプロイブランチを持たない env（sandbox）は解かずに失敗する", func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeRepo(t, root, map[string]string{branchdecl.File: branchesOK})
			_, err := deployBranch(root, "sandbox")
			require.ErrorIs(t, err, errDriftTargetEnv)
		})

		t.Run("宣言が無ければ失敗する", func(t *testing.T) {
			t.Parallel()
			_, err := deployBranch(t.TempDir(), "admin")
			require.ErrorIs(t, err, errDriftTarget)
		})
	})
}

func Test_ciDriftTargetStep(t *testing.T) {
	t.Parallel()

	targetKnobs := func(makefile string) map[string]string {
		return map[string]string{
			stepGitRevParse + ".out": deploySHA + "\n",
			stepGitCatFile + ".out":  deploySHA + " blob " + strconv.Itoa(len(makefile)) + "\n" + makefile + "\n",
		}
	}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("デプロイブランチを fetch し、その木が持つ drift の入口の root ごとに promoted=true を書く", func(t *testing.T) {
			t.Parallel()
			f := newDriftFlow(t, targetKnobs(driftMakefile("admin-identity", "admin-workload")))
			require.NoError(t, f.step("ci-drift-target", "-env", "admin"))
			assert.Equal(t, "refs/heads/admin\n", readKnob(t, f.k, stepGitFetch+".stdin"))
			assert.Equal(t, deploySHA+":"+cientry.Makefile+"\n", readKnob(t, f.k, stepGitCatFile+".stdin"))
			assert.Equal(t, "sha="+deploySHA+"\npromoted-admin-identity=true\npromoted-admin-workload=true\n", readKnob(t, filepath.Dir(f.output), filepath.Base(f.output)))
			assert.Contains(t, readKnob(t, filepath.Dir(f.summary), filepath.Base(f.summary)), deploySHA)
		})

		t.Run("その木が一部の root の drift の入口だけを持てば、無い root（昇格の前）は false にする", func(t *testing.T) {
			t.Parallel()
			f := newDriftFlow(t, targetKnobs(driftMakefile("admin-identity", "security-workload")))
			require.NoError(t, f.step("ci-drift-target", "-env", "admin"))
			assert.Equal(t, "sha="+deploySHA+"\npromoted-admin-identity=true\npromoted-admin-workload=false\n", readKnob(t, filepath.Dir(f.output), filepath.Base(f.output)))
		})

		t.Run("その木に env の drift の入口が1つも無ければ、すべての root を false にして成功する", func(t *testing.T) {
			t.Parallel()
			f := newDriftFlow(t, targetKnobs(driftMakefile("security-identity")))
			require.NoError(t, f.step("ci-drift-target", "-env", "admin"))
			assert.Equal(t, "sha="+deploySHA+"\npromoted-admin-identity=false\npromoted-admin-workload=false\n", readKnob(t, filepath.Dir(f.output), filepath.Base(f.output)))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for name, c := range map[string]struct {
			knobs map[string]string
			err   error
		}{
			"fetch が失敗する":              {map[string]string{stepGitFetch + ".rc": "128"}, errDriftTarget},
			"rev-parse が commit を返さない": {map[string]string{stepGitRevParse + ".out": "HEAD\n"}, errDriftTarget},
			"ci.mk を読めない": {map[string]string{
				stepGitRevParse + ".out": deploySHA + "\n", stepGitCatFile + ".out": deploySHA + ":x missing\n",
			}, errDriftTarget},
			"その木の ci.mk を解釈できない": {targetKnobs("ci-unknown:\n"), errDriftTarget},
		} {
			t.Run(name+"なら何も書かずに失敗する", func(t *testing.T) {
				t.Parallel()
				f := newDriftFlow(t, c.knobs)
				require.ErrorIs(t, f.step("ci-drift-target", "-env", "admin"), c.err)
				assert.Empty(t, readKnob(t, filepath.Dir(f.output), filepath.Base(f.output)))
			})
		}

		t.Run("デフォルトブランチの木に env の drift の入口が無ければ git を起動せずに失敗する", func(t *testing.T) {
			t.Parallel()
			f := newDriftFlow(t, nil)
			writeRepo(t, f.d.root, map[string]string{cientry.Makefile: driftMakefile("security-identity")})
			require.ErrorIs(t, f.step("ci-drift-target", "-env", "admin"), errDriftTarget)
			assert.Empty(t, f.calls())
		})

		t.Run("GITHUB_OUTPUT を書けなければ失敗する", func(t *testing.T) {
			t.Parallel()
			testenv.RequireNonRoot(t, "書けないディレクトリを作る")
			ro := t.TempDir()
			require.NoError(t, os.Chmod(ro, 0o500))
			t.Cleanup(func() { _ = os.Chmod(ro, 0o700) })
			f := newDriftFlow(t, targetKnobs(driftMakefile("admin-identity", "admin-workload")), "GITHUB_OUTPUT="+filepath.Join(ro, "out"))
			require.ErrorIs(t, f.step("ci-drift-target", "-env", "admin"), errCIOutput)
		})

		t.Run("デプロイブランチを持たない env は git を起動せずに失敗する", func(t *testing.T) {
			t.Parallel()
			f := newDriftFlow(t, nil)
			require.ErrorIs(t, f.step("ci-drift-target", "-env", "sandbox"), errDriftTargetEnv)
			assert.Empty(t, f.calls())
		})

		for name, args := range map[string][]string{"env が無い": nil, "env の形が違う": {"-env", "Admin"}, "余計な引数": {"-env", "admin", "x"}} {
			t.Run(name+"なら使い方の誤り", func(t *testing.T) {
				t.Parallel()
				f := newDriftFlow(t, nil)
				err := f.step("ci-drift-target", args...)
				require.Error(t, err)
				assert.Equal(t, 2, exitCode(err))
				assert.Empty(t, f.calls())
			})
		}
	})
}

func Test_ciDriftLockStep(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("plan の前と後に lock を見る", func(t *testing.T) {
			t.Parallel()
			f := newDriftFlow(t, nil)
			require.NoError(t, f.drift("identity"))
			var order []string
			for _, l := range strings.Split(strings.TrimSpace(f.calls()), "\n") {
				if step, _, _ := strings.Cut(l, " "); step == stepS3List || step == stepCIPlanNoLock {
					order = append(order, step)
				}
			}
			assert.Equal(t, []string{stepS3List, stepCIPlanNoLock, stepS3List}, order)
			assert.Contains(t, readKnob(t, filepath.Dir(f.output), filepath.Base(f.output)), "result=clean\n")
		})

		for name, n := range map[string]string{"前": "1", "後": "2"} {
			t.Run("plan の"+name+"に lock があれば apply と重なったとして未確定にする", func(t *testing.T) {
				t.Parallel()
				f := newDriftFlow(t, map[string]string{stepS3List + "." + n + ".out": withLock()})
				require.ErrorIs(t, f.drift("identity"), errDriftInconclusive)
				assert.Equal(t, 1, exitCode(f.errs["ci-finish"]))
				assert.Contains(t, readKnob(t, filepath.Dir(f.output), filepath.Base(f.output)), "result=inconclusive\n")
			})
		}
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("lock を確かめられなければ plan せず、result を書かずに判定に届かない失敗にする", func(t *testing.T) {
			t.Parallel()
			f := newDriftFlow(t, map[string]string{stepS3List + ".rc": "255"})
			err := f.drift("identity")
			require.ErrorIs(t, err, errCINotEstablished)
			assert.Equal(t, 2, exitCode(err))
			assert.NotContains(t, f.calls(), stepCIPlanNoLock)
			assert.NotContains(t, readKnob(t, filepath.Dir(f.output), filepath.Base(f.output)), "result=")
		})

		t.Run("plan の失敗の診断は許可の文法の欄だけを job summary に出し、値と address の識別子を出さない", func(t *testing.T) {
			t.Parallel()
			diag := `{"@level":"error","@message":"Error: reading IAM Role","type":"diagnostic","diagnostic":{"severity":"error",` +
				`"summary":"reading IAM Role (` + driftSecret + `): operation error IAM: GetRole, https response error StatusCode: 403, RequestID: r, ` +
				`api error AccessDenied: User: arn:aws:sts::` + account + `:assumed-role/tabp-admin-ci-plan/gha is not authorized ` +
				`to perform: iam:GetRole on resource: x because no identity-based policy allows the iam:GetRole action",` +
				`"detail":"` + driftSecret + `","address":"aws_iam_role.this[\"` + account + `\"]"}}` + "\n"
			f := newDriftFlow(t, map[string]string{stepCIPlanNoLock + ".rc": "1", stepCIPlanNoLock + ".out": planLog + diag})
			require.ErrorIs(t, f.drift("identity"), errCIFailed)
			summary := readKnob(t, filepath.Dir(f.summary), filepath.Base(f.summary))
			assert.Contains(t, summary, "失敗の段: plan")
			assert.Contains(t, summary, "address=aws_iam_role.this[&lt;伏せた key&gt;]")
			assert.Contains(t, summary, "action=iam:GetRole")
			assert.NotContains(t, readKnob(t, filepath.Dir(f.output), filepath.Base(f.output)), "result=")
			f.secretFree()
			assert.NotContains(t, summary, account)
		})

		t.Run("plan が失敗すれば後の lock を見ない", func(t *testing.T) {
			t.Parallel()
			f := newDriftFlow(t, map[string]string{stepCIPlanNoLock + ".rc": "1"})
			require.ErrorIs(t, f.drift("identity"), errCIFailed)
			assert.Equal(t, 1, strings.Count(f.calls(), stepS3List))
		})

		t.Run("-at が無ければ使い方の誤り", func(t *testing.T) {
			t.Parallel()
			f := newDriftFlow(t, nil)
			require.ErrorIs(t, f.step("ci-drift-lock", "-state", f.state), errUsage)
		})

		t.Run("plan の入口の state では lock を見ない", func(t *testing.T) {
			t.Parallel()
			f := newDriftFlow(t, nil)
			require.NoError(t, f.step("ci-prepare", "-mode", ciModePlan, "-project", "tabp", "-env", "admin", "-root", "identity",
				"-profile", "p", "-base", f.base, "-header", f.header, "-state", f.state))
			require.NoError(t, f.step("ci-init", "-state", f.state))
			require.ErrorIs(t, f.step("ci-drift-lock", "-at", "before", "-state", f.state), errUsage)
			assert.NotContains(t, f.calls(), stepS3List)
		})
	})
}

func Test_driftLockHeld(t *testing.T) {
	t.Parallel()

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("lock file の key を読めなければ aws を起動せずに失敗する", func(t *testing.T) {
			t.Parallel()
			f := newDriftFlow(t, nil)
			_, err := driftLockHeld(f.d, ciState{Dir: t.TempDir(), InitArgs: []string{"-backend-config=bucket=b"}})
			require.Error(t, err)
			assert.Empty(t, f.calls())
		})
	})

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		for name, c := range map[string]struct {
			list string
			want bool
		}{"lock が無い": {noLock(), false}, "lock がある": {withLock(), true}} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				f := newDriftFlow(t, map[string]string{stepS3List + ".out": c.list})
				dir := filepath.Join(f.base, "env", "admin", "identity")
				got, err := driftLockHeld(f.d, ciState{Dir: dir, Region: "ap-northeast-1", Account: account, Profile: "p", InitArgs: []string{"-backend-config=bucket=b"}})
				require.NoError(t, err)
				assert.Equal(t, c.want, got)
			})
		}
	})
}

func Test_ciDriftStep(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("宣言との差（終了コード 2）は drift で、job を判定として落とす", func(t *testing.T) {
			t.Parallel()
			f := newDriftFlow(t, map[string]string{
				stepCIPlanNoLock + ".rc":  "2",
				stepCIPlanNoLock + ".out": planLog + summaryOne,
				stepCIShow + ".out":       driftPlan(change("aws_s3_bucket.logs", `"create"`), ""),
			})
			err := f.drift("identity")
			require.ErrorIs(t, err, errDriftFound)
			assert.Equal(t, 1, exitCode(err))
			out := readKnob(t, filepath.Dir(f.output), filepath.Base(f.output))
			assert.Contains(t, out, "result=drift\ncount=1\ndigest=")
			assert.Regexp(t, `(?m)^digest=[0-9a-f]{64}$`, out)
			summary := readKnob(t, filepath.Dir(f.summary), filepath.Base(f.summary))
			assert.Contains(t, summary, "create aws_s3_bucket.logs")
			assert.Contains(t, summary, "key: `branch/admin/8-1-admin-identity.txt`")
		})

		t.Run("終了コード 2 で resource の変更も resource_drift も無ければ、output の項目1件の drift として落とす", func(t *testing.T) {
			t.Parallel()
			f := newDriftFlow(t, map[string]string{
				stepCIPlanNoLock + ".rc": "2",
				stepCIShow + ".out": `{"format_version":"1.2","resource_changes":[` + change("a.b", `"no-op"`) +
					`],"resource_drift":[],"output_changes":{"o":{"actions":["update"]}}}`,
			})
			require.ErrorIs(t, f.drift("identity"), errDriftFound)
			assert.Contains(t, readKnob(t, filepath.Dir(f.output), filepath.Base(f.output)), "result=drift\ncount=1\n")
			assert.Contains(t, readKnob(t, filepath.Dir(f.summary), filepath.Base(f.summary)), "output （root の output）")
		})

		t.Run("宣言していない属性の外での変化（終了コード 0 の resource_drift）も drift にする", func(t *testing.T) {
			t.Parallel()
			f := newDriftFlow(t, map[string]string{
				stepCIShow + ".out": driftPlan(change("aws_iam_role.r", `"no-op"`),
					drifted("aws_iam_role.r", "aws_iam_role", `"update"`, `{"description":"a","tags":{}}`, `{"description":"`+driftSecret+`","tags":{}}`)),
			})
			require.ErrorIs(t, f.drift("identity"), errDriftFound)
			assert.Contains(t, readKnob(t, filepath.Dir(f.summary), filepath.Base(f.summary)), "update aws_iam_role.r: description")
			f.secretFree()
		})

		t.Run("ADR-0601 決定20 の組だけが変わった項目は外し、clean にする", func(t *testing.T) {
			t.Parallel()
			f := newDriftFlow(t, map[string]string{
				stepCIShow + ".out": driftPlan(change("aws_ecs_service.s", `"no-op"`),
					drifted("aws_ecs_service.s", "aws_ecs_service", `"update"`, `{"task_definition":"a:1"}`, `{"task_definition":"a:2"}`)),
			})
			require.NoError(t, f.drift("identity"))
			assert.Contains(t, readKnob(t, filepath.Dir(f.output), filepath.Base(f.output)), "result=clean\ncount=0\ndigest=\n")
		})

		t.Run("address と属性の名前は公開されるログに出さず、job summary にだけ書く", func(t *testing.T) {
			t.Parallel()
			f := newDriftFlow(t, map[string]string{
				stepCIPlanNoLock + ".rc":  "2",
				stepCIPlanNoLock + ".out": planLog + summaryOne,
				stepCIShow + ".out": driftPlan(change("aws_s3_bucket.secretname", `"create"`),
					drifted("aws_iam_role.other", "aws_iam_role", `"update"`, `{"inline_policy":"`+driftSecret+`"}`, `{"inline_policy":"b"}`)),
			})
			require.ErrorIs(t, f.drift("identity"), errDriftFound)
			f.logsFree("aws_s3_bucket.secretname", "aws_iam_role.other", "inline_policy")
			f.secretFree()
			summary := readKnob(t, filepath.Dir(f.summary), filepath.Base(f.summary))
			assert.Contains(t, summary, "create aws_s3_bucket.secretname")
			assert.Contains(t, summary, "update aws_iam_role.other: inline_policy")
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("ADR-0601 決定20 の表を読めなければ判定せず、判定に届かない失敗にする", func(t *testing.T) {
			t.Parallel()
			f := newDriftFlow(t, nil)
			writeRepo(t, f.d.root, map[string]string{ownedattrs.File: "表はありません\n"})
			err := f.drift("identity")
			require.ErrorIs(t, err, errCINotEstablished)
			assert.ErrorIs(t, f.errs["ci-drift"], errDriftOwned)
			assert.NotContains(t, readKnob(t, filepath.Dir(f.output), filepath.Base(f.output)), "result=")
		})

		t.Run("resource_drift を読めなければ判定せずに失敗する", func(t *testing.T) {
			t.Parallel()
			f := newDriftFlow(t, map[string]string{
				stepCIShow + ".out": driftPlan(change("a.b", `"no-op"`), `{"address":"a.b","type":"a","change":{"actions":["update"],"before":"`+driftSecret+`","after":null}}`),
			})
			require.ErrorIs(t, f.drift("identity"), errCINotEstablished)
			require.ErrorIs(t, f.errs["ci-drift"], errDriftJSON)
			f.secretFree()
		})

		t.Run("plan の入口の state では判定しない", func(t *testing.T) {
			t.Parallel()
			f := newDriftFlow(t, nil)
			require.NoError(t, f.step("ci-prepare", "-mode", ciModePlan, "-project", "tabp", "-env", "admin", "-root", "identity",
				"-profile", "p", "-base", f.base, "-header", f.header, "-state", f.state))
			require.ErrorIs(t, f.step("ci-drift", "-state", f.state), errUsage)
		})
	})
}

func Test_ciRunDrift(t *testing.T) {
	t.Parallel()

	jsonFile := func(t *testing.T, body string) string {
		t.Helper()
		p := filepath.Join(t.TempDir(), "plan.json")
		require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
		return p
	}
	d := func(t *testing.T) deps {
		t.Helper()
		root := t.TempDir()
		writeRepo(t, root, map[string]string{ownedattrs.File: ownedADR})
		return deps{root: root}
	}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("output だけが変わる plan も drift にする", func(t *testing.T) {
			t.Parallel()
			s := ciState{JSON: jsonFile(t, driftPlan(change("a.b", `"no-op"`), "")), PlanChanged: true}
			require.NoError(t, ciRunDrift(&s, d(t)))
			assert.Equal(t, verdictDrift, s.Verdict)
			assert.Equal(t, 1, s.Count)
		})

		t.Run("同じ address の変更と drift は1件に数える", func(t *testing.T) {
			t.Parallel()
			s := ciState{JSON: jsonFile(t, driftPlan(change("a.b", `"update"`), drifted("a.b", "a", `"update"`, `{"x":1}`, `{"x":2}`))),
				Changes: []string{"update a.b"}, PlanChanged: true}
			require.NoError(t, ciRunDrift(&s, d(t)))
			assert.Equal(t, verdictDrift, s.Verdict)
			assert.Equal(t, 1, s.Count)
			assert.Len(t, s.Drift, 2)
			assert.Len(t, s.Digest, 64)
		})

		t.Run("lock と重なり drift の項目もあれば未確定で、Count は項目の address の数", func(t *testing.T) {
			t.Parallel()
			s := ciState{JSON: jsonFile(t, driftPlan(change("a.b", `"update"`), drifted("c.d", "c", `"delete"`, `{"x":1}`, `null`))),
				Changes: []string{"update a.b"}, PlanChanged: true, Overlap: true}
			require.NoError(t, ciRunDrift(&s, d(t)))
			assert.Equal(t, verdictInconclusive, s.Verdict)
			assert.Equal(t, 2, s.Count)
			assert.Len(t, s.Drift, 2)
		})

		t.Run("lock と重なれば drift の有無に依らず未確定", func(t *testing.T) {
			t.Parallel()
			s := ciState{JSON: jsonFile(t, driftPlan(change("a.b", `"no-op"`), "")), Overlap: true}
			require.NoError(t, ciRunDrift(&s, d(t)))
			assert.Equal(t, verdictInconclusive, s.Verdict)
		})

		t.Run("差が無ければ clean で、digest は空", func(t *testing.T) {
			t.Parallel()
			s := ciState{JSON: jsonFile(t, driftPlan(change("a.b", `"no-op"`), ""))}
			require.NoError(t, ciRunDrift(&s, d(t)))
			assert.Equal(t, verdictClean, s.Verdict)
			assert.Zero(t, s.Count)
			assert.Empty(t, s.Digest)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("plan の JSON を読めなければ失敗する", func(t *testing.T) {
			t.Parallel()
			s := ciState{JSON: filepath.Join(t.TempDir(), "missing.json")}
			require.ErrorIs(t, ciRunDrift(&s, d(t)), errDriftJSON)
		})

		t.Run("一覧の表が0行なら失敗する", func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeRepo(t, root, map[string]string{ownedattrs.File: "    | resource type | 属性 | 発火側が行う操作 |\n    | --- | --- | --- |\n\n"})
			s := ciState{JSON: jsonFile(t, driftPlan(change("a.b", `"no-op"`), ""))}
			err := ciRunDrift(&s, deps{root: root})
			require.ErrorIs(t, err, errDriftOwned)
			assert.ErrorIs(t, err, ownedattrs.ErrEmpty)
		})
	})
}

func Test_driftItems(t *testing.T) {
	t.Parallel()

	owned := ownedattrs.NewSet([]ownedattrs.Pair{{Type: "aws_ecs_service", Attr: "task_definition"}})
	items := func(t *testing.T, drift ...string) []driftItem {
		t.Helper()
		got, err := driftItems([]byte(driftPlan("", strings.Join(drift, ","))), owned)
		require.NoError(t, err)
		return got
	}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("一覧の属性だけが変わった項目は外す", func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, items(t, drifted("aws_ecs_service.s", "aws_ecs_service", `"update"`, `{"task_definition":"a:1","name":"s"}`, `{"task_definition":"a:2","name":"s"}`)))
		})

		t.Run("一覧の属性と別の属性が同時に変われば外さない", func(t *testing.T) {
			t.Parallel()
			got := items(t, drifted("aws_ecs_service.s", "aws_ecs_service", `"update"`, `{"task_definition":"a:1","desired_count":1}`, `{"task_definition":"a:2","desired_count":2}`))
			assert.Equal(t, []driftItem{{Op: "update", Address: "aws_ecs_service.s", Attrs: []string{"desired_count", "task_definition"}}}, got)
		})

		t.Run("一覧の type の別の属性だけが変われば外さない", func(t *testing.T) {
			t.Parallel()
			got := items(t, drifted("aws_ecs_service.s", "aws_ecs_service", `"update"`, `{"desired_count":1}`, `{"desired_count":2}`))
			assert.Equal(t, []driftItem{{Op: "update", Address: "aws_ecs_service.s", Attrs: []string{"desired_count"}}}, got)
		})

		t.Run("別の type の同名の属性は外さない", func(t *testing.T) {
			t.Parallel()
			got := items(t, drifted("aws_ecs_task_set.t", "aws_ecs_task_set", `"update"`, `{"task_definition":"a:1"}`, `{"task_definition":"a:2"}`))
			assert.Equal(t, []driftItem{{Op: "update", Address: "aws_ecs_task_set.t", Attrs: []string{"task_definition"}}}, got)
		})

		t.Run("Console での削除（after が null）は一覧の type でも外さない", func(t *testing.T) {
			t.Parallel()
			got := items(t, drifted("aws_ecs_service.s", "aws_ecs_service", `"delete"`, `{"task_definition":"a:1"}`, `null`))
			assert.Equal(t, []driftItem{{Op: "delete", Address: "aws_ecs_service.s"}}, got)
		})

		t.Run("置換は一覧の type でも外さない", func(t *testing.T) {
			t.Parallel()
			got := items(t, drifted("aws_ecs_service.s", "aws_ecs_service", `"delete","create"`, `{"task_definition":"a:1"}`, `{"task_definition":"a:2"}`))
			assert.Equal(t, []driftItem{{Op: "replace", Address: "aws_ecs_service.s"}}, got)
		})

		t.Run("moved だけ（no-op）の項目は変化ではないので数えない", func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, items(t, `{"address":"aws_x.new","previous_address":"aws_x.old","type":"aws_x","change":{"actions":["no-op"],"before":{"a":1},"after":{"a":1}}}`))
		})

		t.Run("値が変わらない update は、変わった属性が無くても外さない", func(t *testing.T) {
			t.Parallel()
			got := items(t, drifted("aws_ecs_service.s", "aws_ecs_service", `"update"`, `{"task_definition":"a:1"}`, `{"task_definition":"a:1"}`))
			assert.Equal(t, []driftItem{{Op: "update", Address: "aws_ecs_service.s"}}, got)
		})

		t.Run("識別子を含む address の key と、名前の形でない属性を伏せる", func(t *testing.T) {
			t.Parallel()
			got := items(t, drifted(`aws_x.y[\"123456789012\"]`, "aws_x", `"update"`, `{"a b":1}`, `{"a b":2}`))
			assert.Equal(t, []driftItem{{Op: "update", Address: `aws_x.y[<伏せた key>]`, Attrs: []string{"<伏せた属性>"}}}, got)
		})

		t.Run("format_version の major が 1 なら minor を問わず読む", func(t *testing.T) {
			t.Parallel()
			for _, v := range []string{"1", "1.0", "1.9"} {
				got, err := driftItems([]byte(`{"format_version":"`+v+`"}`), owned)
				require.NoError(t, err, v)
				assert.Empty(t, got)
			}
		})

		t.Run("resource_drift が無ければ0件", func(t *testing.T) {
			t.Parallel()
			got, err := driftItems([]byte(`{"format_version":"1.2"}`), owned)
			require.NoError(t, err)
			assert.Empty(t, got)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for name, in := range map[string]string{
			"JSON でない":                    "x",
			"format_version が無い":          `{"resource_drift":[]}`,
			"format_version の major が 2":  `{"format_version":"2.0","resource_drift":[]}`,
			"format_version の major が 0":  `{"format_version":"0.2","resource_drift":[]}`,
			"format_version が 1 で始まる別の数":  `{"format_version":"10.1","resource_drift":[]}`,
			"change が無い":                  `{"format_version":"1.2","resource_drift":[{"address":"a.b","type":"a"}]}`,
			"actions が空":                  driftPlan("", `{"address":"a.b","type":"a","change":{"actions":[]}}`),
			"address が無い":                 driftPlan("", `{"type":"a","change":{"actions":["delete"]}}`),
			"update の after が object でない": driftPlan("", drifted("a.b", "a", `"update"`, `{"x":1}`, `null`)),
		} {
			t.Run(name+"なら失敗する", func(t *testing.T) {
				t.Parallel()
				_, err := driftItems([]byte(in), owned)
				require.ErrorIs(t, err, errDriftJSON)
			})
		}
	})
}

func Test_changedAttrs(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("値の違う key・片方にだけ在る key を名前の順で返す", func(t *testing.T) {
			t.Parallel()
			got, err := changedAttrs(json.RawMessage(`{"b":1,"a":{"x":[1]},"c":2,"same":"s"}`), json.RawMessage(`{"b":2,"a":{"x":[2]},"d":3,"same":"s"}`))
			require.NoError(t, err)
			assert.Equal(t, []string{"a", "b", "c", "d"}, got)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for name, c := range map[string][2]string{
			"before が null": {`null`, `{}`}, "after が配列": {`{}`, `[]`}, "before が文字列": {`"x"`, `{}`}, "after が空": {`{}`, ``},
		} {
			t.Run(name+"なら失敗する", func(t *testing.T) {
				t.Parallel()
				_, err := changedAttrs(json.RawMessage(c[0]), json.RawMessage(c[1]))
				require.ErrorIs(t, err, errDriftJSON)
			})
		}
	})
}

func Test_itemsDigest(t *testing.T) {
	t.Parallel()

	a := driftItem{Op: "update", Address: "a.b", Attrs: []string{"x"}}
	b := driftItem{Op: "delete", Address: "c.d"}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("集合が同じなら順に依らず同じ値", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, itemsDigest([]driftItem{a, b}), itemsDigest([]driftItem{b, a}))
			assert.Equal(t, itemsDigest([]driftItem{a, b}), itemsDigest([]driftItem{a, b, a}))
		})

		t.Run("属性が変われば違う値", func(t *testing.T) {
			t.Parallel()
			c := a
			c.Attrs = []string{"y"}
			assert.NotEqual(t, itemsDigest([]driftItem{a}), itemsDigest([]driftItem{c}))
		})

		t.Run("項目が無ければ空", func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, itemsDigest(nil))
		})
	})
}

func Test_ciFinishDrift(t *testing.T) {
	t.Parallel()

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("判定の段が無ければ、result を書かずに失敗する", func(t *testing.T) {
			t.Parallel()
			f := newDriftFlow(t, nil)
			s := ciState{Mode: ciModeDrift, Done: []string{stagePrepare, stageInit, stageLockBefore, stagePlan, stageLockAfter, stageUpload}, Established: true}
			_, err := ciFinishDrift(s, f.d)
			require.ErrorIs(t, err, errCIFailed)
			assert.Empty(t, readKnob(t, filepath.Dir(f.output), filepath.Base(f.output)))
		})

		t.Run("判定が空なら失敗する", func(t *testing.T) {
			t.Parallel()
			f := newDriftFlow(t, nil)
			_, err := ciFinishDrift(ciState{Mode: ciModeDrift, Done: driftRequired}, f.d)
			require.ErrorIs(t, err, errCIState)
		})

		t.Run("GITHUB_OUTPUT を書けなければ失敗する", func(t *testing.T) {
			t.Parallel()
			testenv.RequireNonRoot(t, "書けないディレクトリを作る")
			ro := t.TempDir()
			require.NoError(t, os.Chmod(ro, 0o500))
			t.Cleanup(func() { _ = os.Chmod(ro, 0o700) })
			f := newDriftFlow(t, nil, "GITHUB_OUTPUT="+filepath.Join(ro, "out"))
			_, err := ciFinishDrift(ciState{Mode: ciModeDrift, Done: driftRequired, Verdict: verdictClean, Root: "admin-identity"}, f.d)
			require.ErrorIs(t, err, errCIOutput)
		})

		t.Run("upload の前に落ちれば、終わっていない段として判定に届かない失敗にする", func(t *testing.T) {
			t.Parallel()
			f := newDriftFlow(t, nil)
			_, err := ciFinishDrift(ciState{Mode: ciModeDrift, Done: []string{stagePrepare}}, f.d)
			require.ErrorIs(t, err, errCINotEstablished)
			assert.Contains(t, err.Error(), "終わっていない段")
		})
	})
}

func reportFlow(t *testing.T, needs map[string]needsJob, issues string) *flow {
	t.Helper()
	raw, err := json.Marshal(needs)
	require.NoError(t, err)
	f := newFlow(t, nil, "CI_NEEDS="+string(raw))
	writeRepo(t, f.d.root, map[string]string{cientry.Makefile: driftMakefile("admin-identity", "admin-workload"), "issues.json": issues})
	return f
}

func (f *flow) report() error {
	return f.step("ci-drift-report", "-issues", filepath.Join(f.d.root, "issues.json"), "-body", filepath.Join(filepath.Dir(f.output), "body.md"))
}

func (f *flow) outputs() map[string]string {
	f.t.Helper()
	out := map[string]string{}
	raw := readKnob(f.t, filepath.Dir(f.output), filepath.Base(f.output))
	lines := strings.Split(raw, "\n")
	for i := 0; i < len(lines); i++ {
		if name, delim, ok := strings.Cut(lines[i], "<<"); ok {
			var v []string
			for i++; i < len(lines) && lines[i] != delim; i++ {
				v = append(v, lines[i])
			}
			out[name] = strings.Join(v, "\n")
			continue
		}
		if name, v, ok := strings.Cut(lines[i], "="); ok {
			out[name] = v
		}
	}
	return out
}

func okNeeds(identity, workload needsJob) map[string]needsJob {
	return map[string]needsJob{
		"drift-target-admin": {Result: "success", Outputs: map[string]string{
			"sha": deploySHA, "promoted-admin-identity": "true", "promoted-admin-workload": "true"}},
		"drift-infra-code-gate-admin": {Result: "success"},
		"drift-admin-identity":        identity,
		"drift-admin-workload":        workload,
	}
}

var (
	cleanJob = needsJob{Result: "success", Outputs: map[string]string{"result": "clean", "count": "0", "digest": ""}}
	digestA  = strings.Repeat("a", 64)
	digestB  = strings.Repeat("b", 64)
)

func driftJob(count, digest string) needsJob {
	return needsJob{Result: "failure", Outputs: map[string]string{"result": "drift", "count": count, "digest": digest}}
}

// reportIssue は、upsert-report-issue が GITHUB_TOKEN で書いた形の前回の報告を、gh issue list の JSON の1件で組む。
func reportIssue(t *testing.T, body string) openIssue {
	t.Helper()
	o := openIssue{Number: 3, Title: driftTitle, Body: issueMarker + "\n" + body, URL: "https://github.com/o/r/issues/3"}
	o.Author.Login = issueAuthor
	return o
}

func issuesJSON(t *testing.T, issues ...openIssue) string {
	t.Helper()
	raw, err := json.Marshal(issues)
	require.NoError(t, err)
	return string(raw)
}

func Test_ciDriftReportStep(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("drift が新しく出れば、印の無い本文で issue を開いて通知し、印は通知の後に書く", func(t *testing.T) {
			t.Parallel()
			f := reportFlow(t, okNeeds(cleanJob, driftJob("2", digestA)), "[]")
			require.NoError(t, f.report())
			o := f.outputs()
			assert.Equal(t, "open", o["state"])
			assert.Equal(t, "drift", o["notify"])
			assert.Equal(t, driftTitle, o["title"])
			assert.Equal(t, issueMarker, o["marker"])
			assert.Equal(t, "- admin-identity: 0 件\n- admin-workload: 2 件", o["details"])
			body := readKnob(t, filepath.Dir(f.output), "body.md")
			assert.Contains(t, body, "- admin-workload: 2 件")
			assert.NotContains(t, body, "drift-digest")
			assert.Equal(t, filepath.Join(filepath.Dir(f.output), "body.md"), o["body"])
			assert.Equal(t, "open", o["mark-state"])
			assert.Regexp(t, `<!-- drift-digest: [0-9a-f]{64} -->`, o["mark-body"])
		})

		t.Run("drift の集合が前回の issue の印と同じなら issue を更新し、通知しない", func(t *testing.T) {
			t.Parallel()
			first := reportFlow(t, okNeeds(cleanJob, driftJob("2", digestA)), "[]")
			require.NoError(t, first.report())
			issues := issuesJSON(t, reportIssue(t, first.outputs()["mark-body"]))
			f := reportFlow(t, okNeeds(cleanJob, driftJob("2", digestA)), issues)
			require.NoError(t, f.report())
			assert.Equal(t, "open", f.outputs()["state"])
			assert.Equal(t, "none", f.outputs()["notify"])
			assert.Empty(t, f.outputs()["mark-state"])
		})

		t.Run("drift の集合が変われば、前回の印を保った本文で通知し、issue の URL を添える", func(t *testing.T) {
			t.Parallel()
			issues := issuesJSON(t, reportIssue(t, "<!-- drift-digest: "+digestB+" -->"))
			f := reportFlow(t, okNeeds(cleanJob, driftJob("2", digestA)), issues)
			require.NoError(t, f.report())
			assert.Equal(t, "drift", f.outputs()["notify"])
			assert.Contains(t, f.outputs()["details"], "https://github.com/o/r/issues/3")
			assert.Contains(t, readKnob(t, filepath.Dir(f.output), "body.md"), "<!-- drift-digest: "+digestB+" -->")
			assert.NotContains(t, f.outputs()["mark-body"], digestB)
		})

		t.Run("drift が解消すれば issue を動かさずに解消を通知し、閉じるのは通知の後にする", func(t *testing.T) {
			t.Parallel()
			f := reportFlow(t, okNeeds(cleanJob, cleanJob), issuesJSON(t, reportIssue(t, "x")))
			require.NoError(t, f.report())
			assert.Equal(t, "keep", f.outputs()["state"])
			assert.Equal(t, "drift", f.outputs()["notify"])
			assert.Equal(t, "close", f.outputs()["mark-state"])
			assert.Empty(t, f.outputs()["mark-body"])
			assert.NoFileExists(t, filepath.Join(filepath.Dir(f.output), "body.md"))
		})

		t.Run("drift が無く issue も無ければ通知しない", func(t *testing.T) {
			t.Parallel()
			other := reportIssue(t, "")
			other.Title = "別の報告"
			f := reportFlow(t, okNeeds(cleanJob, cleanJob), issuesJSON(t, other))
			require.NoError(t, f.report())
			assert.Equal(t, "close", f.outputs()["state"])
			assert.Equal(t, "none", f.outputs()["notify"])
		})

		for name, mutate := range map[string]func(*openIssue){
			"作者が github-actions でない":           func(o *openIssue) { o.Author.Login = "someone" },
			"本文が upsert-report-issue の印で始まらない": func(o *openIssue) { o.Body = "x\n" + o.Body },
		} {
			t.Run(name+"題名の同じ issue は前回の報告にしない", func(t *testing.T) {
				t.Parallel()
				first := reportFlow(t, okNeeds(cleanJob, driftJob("2", digestA)), "[]")
				require.NoError(t, first.report())
				forged := reportIssue(t, first.outputs()["mark-body"])
				mutate(&forged)
				f := reportFlow(t, okNeeds(cleanJob, driftJob("2", digestA)), issuesJSON(t, forged))
				require.NoError(t, f.report())
				assert.Equal(t, "drift", f.outputs()["notify"])
				assert.NotContains(t, f.outputs()["details"], "issues/3")
			})
		}

		t.Run("昇格の前の root を除いた集合で判定し、昇格の前の root の件数を summary に出す", func(t *testing.T) {
			t.Parallel()
			n := okNeeds(driftJob("1", digestA), needsJob{Result: "skipped"})
			n["drift-target-admin"].Outputs["promoted-admin-workload"] = "false"
			f := reportFlow(t, n, "[]")
			require.NoError(t, f.report())
			assert.Equal(t, "open", f.outputs()["state"])
			assert.Equal(t, "drift", f.outputs()["notify"])
			assert.Equal(t, "- admin-identity: 1 件", f.outputs()["details"])
			assert.Contains(t, readKnob(t, filepath.Dir(f.summary), filepath.Base(f.summary)), "昇格の前の root: 1 件")
		})

		t.Run("昇格済みの root が無ければ issue を動かさず、失敗も通知しない", func(t *testing.T) {
			t.Parallel()
			n := okNeeds(needsJob{Result: "skipped"}, needsJob{Result: "skipped"})
			n["drift-target-admin"].Outputs["promoted-admin-identity"] = "false"
			n["drift-target-admin"].Outputs["promoted-admin-workload"] = "false"
			f := reportFlow(t, n, "[]")
			require.NoError(t, f.report())
			assert.Equal(t, "keep", f.outputs()["state"])
			assert.Equal(t, "none", f.outputs()["notify"])
		})

		t.Run("偽の issue が先に並んでも、後の本物を前回の報告として選ぶ", func(t *testing.T) {
			t.Parallel()
			first := reportFlow(t, okNeeds(cleanJob, driftJob("2", digestA)), "[]")
			require.NoError(t, first.report())
			forged := reportIssue(t, "<!-- drift-digest: "+digestB+" -->")
			forged.Number, forged.URL, forged.Author.Login = 1, "https://github.com/o/r/issues/1", "someone"
			genuine := reportIssue(t, first.outputs()["mark-body"])
			f := reportFlow(t, okNeeds(cleanJob, driftJob("2", digestA)), issuesJSON(t, forged, genuine))
			require.NoError(t, f.report())
			assert.Equal(t, "none", f.outputs()["notify"])
		})

		t.Run("issue の一覧が null なら0件と読む（upsert-report-issue は題名で選ぶので issue は重ならず、通知が増える側に倒れる）", func(t *testing.T) {
			t.Parallel()
			f := reportFlow(t, okNeeds(cleanJob, driftJob("2", digestA)), "null")
			require.NoError(t, f.report())
			assert.Equal(t, "open", f.outputs()["state"])
			assert.Equal(t, "drift", f.outputs()["notify"])
		})

		t.Run("上限は実物の terraform-drift.yaml の gh issue list の --limit と同じ", func(t *testing.T) {
			t.Parallel()
			raw, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "terraform-drift.yaml"))
			require.NoError(t, err)
			m := regexp.MustCompile(`(?m)^\s+run: gh issue list .*--limit ([0-9]+) `).FindAllStringSubmatch(string(raw), -1)
			require.Len(t, m, 1)
			assert.Equal(t, strconv.Itoa(issueListLimit), m[0][1])
		})

		t.Run("drift の job の失敗は issue を動かさず、失敗した job を名指して通知する", func(t *testing.T) {
			t.Parallel()
			f := reportFlow(t, okNeeds(driftJob("1", digestA), needsJob{Result: "cancelled"}), "[]")
			require.NoError(t, f.report())
			assert.Equal(t, "keep", f.outputs()["state"])
			assert.Equal(t, "failure", f.outputs()["notify"])
			assert.Equal(t, "drift-admin-workload", f.outputs()["failed-job"])
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("CI_NEEDS が無ければ失敗する", func(t *testing.T) {
			t.Parallel()
			f := newFlow(t, nil)
			writeRepo(t, f.d.root, map[string]string{cientry.Makefile: driftMakefile("admin-identity"), "issues.json": "[]"})
			require.ErrorIs(t, f.report(), errDriftReport)
		})

		t.Run("issue の一覧が JSON でなければ失敗する", func(t *testing.T) {
			t.Parallel()
			f := reportFlow(t, okNeeds(cleanJob, cleanJob), "x")
			require.ErrorIs(t, f.report(), errDriftReport)
			assert.Empty(t, readKnob(t, filepath.Dir(f.output), filepath.Base(f.output)))
		})

		for name, c := range map[string]struct {
			n    int
			fail bool
		}{"上限の1つ手前": {issueListLimit - 1, false}, "上限ちょうど": {issueListLimit, true}} {
			t.Run("issue の一覧が"+name+"の件数なら、失敗するか: "+strconv.FormatBool(c.fail), func(t *testing.T) {
				t.Parallel()
				issues := make([]openIssue, c.n)
				f := reportFlow(t, okNeeds(cleanJob, cleanJob), issuesJSON(t, issues...))
				err := f.report()
				if c.fail {
					require.ErrorIs(t, err, errDriftReport)
					assert.Empty(t, readKnob(t, filepath.Dir(f.output), filepath.Base(f.output)))
					return
				}
				require.NoError(t, err)
			})
		}

		for name, v := range map[string]string{"{}": "{}", "null": "null"} {
			t.Run("CI_NEEDS が "+name+" なら失敗する", func(t *testing.T) {
				t.Parallel()
				f := newFlow(t, nil, "CI_NEEDS="+v)
				writeRepo(t, f.d.root, map[string]string{cientry.Makefile: driftMakefile("admin-identity", "admin-workload"), "issues.json": "[]"})
				require.ErrorIs(t, f.report(), errDriftReport)
				assert.Empty(t, readKnob(t, filepath.Dir(f.output), filepath.Base(f.output)))
			})
		}

		t.Run("前回の issue の URL が区切りの行を持てば、GITHUB_OUTPUT に何も書かずに失敗する", func(t *testing.T) {
			t.Parallel()
			prev := reportIssue(t, "")
			prev.URL = "https://github.com/o/r/issues/3\n" + outputDelimiter + "\nnotify=none"
			f := reportFlow(t, okNeeds(cleanJob, driftJob("2", digestA)), issuesJSON(t, prev))
			require.ErrorIs(t, f.report(), errDriftReport)
			assert.Empty(t, readKnob(t, filepath.Dir(f.output), filepath.Base(f.output)))
		})

		t.Run("GITHUB_OUTPUT を書けなければ失敗する", func(t *testing.T) {
			t.Parallel()
			testenv.RequireNonRoot(t, "書けないディレクトリを作る")
			ro := t.TempDir()
			require.NoError(t, os.Chmod(ro, 0o500))
			t.Cleanup(func() { _ = os.Chmod(ro, 0o700) })
			raw, err := json.Marshal(okNeeds(cleanJob, driftJob("2", digestA)))
			require.NoError(t, err)
			f := newFlow(t, nil, "CI_NEEDS="+string(raw), "GITHUB_OUTPUT="+filepath.Join(ro, "out"))
			writeRepo(t, f.d.root, map[string]string{cientry.Makefile: driftMakefile("admin-identity", "admin-workload"), "issues.json": "[]"})
			require.ErrorIs(t, f.report(), errCIOutput)
		})

		t.Run("issue の一覧が無ければ失敗する", func(t *testing.T) {
			t.Parallel()
			f := reportFlow(t, okNeeds(cleanJob, cleanJob), "[]")
			require.ErrorIs(t, f.step("ci-drift-report", "-issues", filepath.Join(t.TempDir(), "x.json"), "-body", "b"), errDriftReport)
		})

		t.Run("ci.mk を読めなければ失敗する", func(t *testing.T) {
			t.Parallel()
			f := reportFlow(t, okNeeds(cleanJob, cleanJob), "[]")
			writeRepo(t, f.d.root, map[string]string{cientry.Makefile: "ci-unknown:\n"})
			require.ErrorIs(t, f.report(), errDriftReport)
		})

		for name, args := range map[string][]string{"-issues が無い": {"-body", "b"}, "-body が無い": {"-issues", "i"}, "余計な引数": {"-issues", "i", "-body", "b", "x"}} {
			t.Run(name+"なら使い方の誤り", func(t *testing.T) {
				t.Parallel()
				f := reportFlow(t, okNeeds(cleanJob, cleanJob), "[]")
				require.ErrorIs(t, f.step("ci-drift-report", args...), errUsage)
			})
		}
	})
}

func Test_driftOutcomes(t *testing.T) {
	t.Parallel()

	entries, err := cientry.Parse(driftMakefile("admin-identity", "admin-workload"))
	require.NoError(t, err)

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("ci.mk の drift の入口の順に結果を読む", func(t *testing.T) {
			t.Parallel()
			got, err := driftOutcomes(entries, okNeeds(cleanJob, driftJob("1", digestA)))
			require.NoError(t, err)
			require.Len(t, got, 2)
			assert.Equal(t, rootOutcome{root: "admin-identity", job: "drift-admin-identity", kind: outcomeClean}, got[0])
			assert.Equal(t, rootOutcome{root: "admin-workload", job: "drift-admin-workload", kind: outcomeDrift, count: 1, digest: digestA}, got[1])
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("drift の入口の job が needs に無ければ失敗する", func(t *testing.T) {
			t.Parallel()
			n := okNeeds(cleanJob, cleanJob)
			delete(n, "drift-admin-workload")
			_, err := driftOutcomes(entries, n)
			require.ErrorIs(t, err, errDriftReport)
		})

		t.Run("needs に ci.mk の入口の無い drift の job があれば失敗する", func(t *testing.T) {
			t.Parallel()
			n := okNeeds(cleanJob, cleanJob)
			n["drift-security-identity"] = cleanJob
			_, err := driftOutcomes(entries, n)
			require.ErrorIs(t, err, errDriftReport)
		})

		t.Run("ci.mk に drift の入口が無ければ失敗する", func(t *testing.T) {
			t.Parallel()
			_, err := driftOutcomes(nil, okNeeds(cleanJob, cleanJob))
			require.ErrorIs(t, err, errDriftReport)
		})

		t.Run("needs の job の名前を読めなければ失敗する", func(t *testing.T) {
			t.Parallel()
			n := okNeeds(cleanJob, cleanJob)
			n["Bad Name"] = cleanJob
			_, err := driftOutcomes(entries, n)
			require.ErrorIs(t, err, errDriftReport)
		})
	})
}

func Test_readOutcome(t *testing.T) {
	t.Parallel()

	read := func(t *testing.T, n map[string]needsJob) rootOutcome {
		t.Helper()
		o, err := readOutcome("admin", "admin-identity", "drift-admin-identity", n)
		require.NoError(t, err)
		return o
	}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		for name, c := range map[string]struct {
			mutate   func(map[string]needsJob)
			kind     string
			failedAt string
		}{
			"木を決める job の失敗":      {func(n map[string]needsJob) { n["drift-target-admin"] = needsJob{Result: "failure"} }, outcomeFailure, "drift-target-admin"},
			"promoted の値が読めない":   {func(n map[string]needsJob) { n["drift-target-admin"] = needsJob{Result: "success"} }, outcomeFailure, "drift-target-admin"},
			"検出の job の skip":     {func(n map[string]needsJob) { n["drift-infra-code-gate-admin"] = needsJob{Result: "skipped"} }, outcomeFailure, "drift-infra-code-gate-admin"},
			"drift の job の skip": {func(n map[string]needsJob) { n["drift-admin-identity"] = needsJob{Result: "skipped"} }, outcomeFailure, "drift-admin-identity"},
			"昇格の前":               {func(n map[string]needsJob) { n["drift-target-admin"].Outputs["promoted-admin-identity"] = "false" }, outcomeUnpromoted, "drift-admin-identity"},
			"別の root だけが昇格の前":    {func(n map[string]needsJob) { n["drift-target-admin"].Outputs["promoted-admin-workload"] = "false" }, outcomeClean, "drift-admin-identity"},
			"昇格の前でも検出の job の失敗は失敗": {func(n map[string]needsJob) {
				n["drift-target-admin"].Outputs["promoted-admin-identity"] = "false"
				n["drift-infra-code-gate-admin"] = needsJob{Result: "failure"}
			}, outcomeFailure, "drift-infra-code-gate-admin"},
			"promoted が True":       {func(n map[string]needsJob) { n["drift-target-admin"].Outputs["promoted-admin-identity"] = "True" }, outcomeFailure, "drift-target-admin"},
			"promoted が 1":          {func(n map[string]needsJob) { n["drift-target-admin"].Outputs["promoted-admin-identity"] = "1" }, outcomeFailure, "drift-target-admin"},
			"promoted が空":           {func(n map[string]needsJob) { n["drift-target-admin"].Outputs["promoted-admin-identity"] = "" }, outcomeFailure, "drift-target-admin"},
			"promoted が空白付きの false": {func(n map[string]needsJob) { n["drift-target-admin"].Outputs["promoted-admin-identity"] = "false " }, outcomeFailure, "drift-target-admin"},
			"env 単位の promoted は読まない": {func(n map[string]needsJob) {
				delete(n["drift-target-admin"].Outputs, "promoted-admin-identity")
				n["drift-target-admin"].Outputs["promoted"] = "true"
			}, outcomeFailure, "drift-target-admin"},
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				n := okNeeds(cleanJob, cleanJob)
				c.mutate(n)
				o := read(t, n)
				assert.Equal(t, c.kind, o.kind)
				assert.Equal(t, c.failedAt, o.job)
			})
		}
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for _, id := range []string{"drift-target-admin", "drift-infra-code-gate-admin", "drift-admin-identity"} {
			t.Run(id+" が needs に無ければ失敗する", func(t *testing.T) {
				t.Parallel()
				n := okNeeds(cleanJob, cleanJob)
				delete(n, id)
				_, err := readOutcome("admin", "admin-identity", "drift-admin-identity", n)
				require.ErrorIs(t, err, errDriftReport)
			})
		}

		t.Run("result を読めなければ失敗する", func(t *testing.T) {
			t.Parallel()
			n := okNeeds(needsJob{Result: "Success!"}, cleanJob)
			_, err := readOutcome("admin", "admin-identity", "drift-admin-identity", n)
			require.ErrorIs(t, err, errDriftReport)
		})
	})
}

func Test_jobOutcome(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		job  needsJob
		want string
	}{
		"成功して clean":               {cleanJob, outcomeClean},
		"失敗して drift":               {driftJob("2", digestA), outcomeDrift},
		"成功なのに drift":              {needsJob{Result: "success", Outputs: driftJob("2", digestA).Outputs}, outcomeFailure},
		"失敗なのに clean":              {needsJob{Result: "failure", Outputs: cleanJob.Outputs}, outcomeFailure},
		"未確定":                      {needsJob{Result: "failure", Outputs: map[string]string{"result": "inconclusive", "count": "0", "digest": ""}}, outcomeFailure},
		"result が空（prepare で止まった）": {needsJob{Result: "failure"}, outcomeFailure},
		"drift の件数が0":              {driftJob("0", digestA), outcomeFailure},
		"drift の digest の形が違う":     {driftJob("2", "x"), outcomeFailure},
		"件数が数でない":                  {driftJob("2x", digestA), outcomeFailure},
		"clean なのに digest がある":     {needsJob{Result: "success", Outputs: map[string]string{"result": "clean", "count": "0", "digest": digestA}}, outcomeFailure},
		"取り消し":                     {needsJob{Result: "cancelled"}, outcomeFailure},
		"件数が -1":                   {driftJob("-1", digestA), outcomeFailure},
		"件数が 02":                   {driftJob("02", digestA), outcomeFailure},
		"件数が +2":                   {driftJob("+2", digestA), outcomeFailure},
		"件数が空":                     {driftJob("", digestA), outcomeFailure},
		"outputs が nil":            {needsJob{Result: "failure", Outputs: nil}, outcomeFailure},
		"digest が大文字":              {driftJob("2", strings.Repeat("A", 64)), outcomeFailure},
		"digest が 63 桁":            {driftJob("2", strings.Repeat("a", 63)), outcomeFailure},
		"clean なのに件数が 1":           {needsJob{Result: "success", Outputs: map[string]string{"result": "clean", "count": "1", "digest": ""}}, outcomeFailure},
		"drift なのに digest が空":      {driftJob("2", ""), outcomeFailure},
	} {
		t.Run(name+"は "+c.want, func(t *testing.T) {
			t.Parallel()
			var o rootOutcome
			assert.Equal(t, c.want, jobOutcome(c.job, &o))
			if c.want == outcomeFailure {
				assert.Zero(t, o.count)
				assert.Empty(t, o.digest)
			}
		})
	}
}

func Test_decideReport(t *testing.T) {
	t.Parallel()

	clean := rootOutcome{root: "admin-identity", job: "drift-admin-identity", kind: outcomeClean}
	drift := rootOutcome{root: "admin-workload", job: "drift-admin-workload", kind: outcomeDrift, count: 1, digest: digestA}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("失敗は昇格の前より先に読む", func(t *testing.T) {
			t.Parallel()
			r := decideReport([]rootOutcome{{kind: outcomeUnpromoted}, {kind: outcomeFailure, job: "j"}}, nil)
			assert.Equal(t, driftReport{State: "keep", Notify: "failure", FailedJob: "j"}, r)
		})

		t.Run("通知の後に書く印は、次の回の同じ集合の本文と同じ", func(t *testing.T) {
			t.Parallel()
			a := decideReport([]rootOutcome{clean, drift}, nil)
			b := decideReport([]rootOutcome{clean, drift}, &openIssue{Title: driftTitle, Body: a.MarkBody})
			assert.Equal(t, a.MarkBody, b.Body)
			assert.Equal(t, "none", b.Notify)
			assert.Empty(t, b.MarkState)
		})

		t.Run("通知の後に印を書けなかった回の次の回は、同じ集合でも通知する", func(t *testing.T) {
			t.Parallel()
			a := decideReport([]rootOutcome{clean, drift}, nil)
			b := decideReport([]rootOutcome{clean, drift}, &openIssue{Title: driftTitle, Body: a.Body})
			assert.Equal(t, "drift", b.Notify)
			assert.Equal(t, "open", b.MarkState)
		})

		t.Run("昇格の前の root は集合にも本文にも入れない", func(t *testing.T) {
			t.Parallel()
			r := decideReport([]rootOutcome{clean, {root: "admin-workload", kind: outcomeUnpromoted}}, nil)
			assert.Equal(t, driftReport{State: "close", Notify: "none"}, r)
		})

		t.Run("drift の root が2つあるとき、並びを入れ替えても印は同じ", func(t *testing.T) {
			t.Parallel()
			other := rootOutcome{root: "admin-identity", job: "drift-admin-identity", kind: outcomeDrift, count: 2, digest: digestB}
			a := decideReport([]rootOutcome{other, drift}, nil)
			b := decideReport([]rootOutcome{drift, other}, nil)
			marker := regexp.MustCompile(`<!-- drift-digest: [0-9a-f]{64} -->`)
			require.Regexp(t, marker, a.MarkBody)
			assert.Equal(t, marker.FindString(a.MarkBody), marker.FindString(b.MarkBody))
		})

		t.Run("印の無い前回の issue は、変わったとして通知する", func(t *testing.T) {
			t.Parallel()
			r := decideReport([]rootOutcome{drift}, &openIssue{Title: driftTitle, Body: "手で書き換えた本文"})
			assert.Equal(t, "drift", r.Notify)
		})

		t.Run("https でない issue の URL は添えない", func(t *testing.T) {
			t.Parallel()
			r := decideReport([]rootOutcome{clean}, &openIssue{Title: driftTitle, URL: "javascript:x"})
			assert.Equal(t, "close", r.MarkState)
			assert.Empty(t, r.Details)
		})

		t.Run("本文に address も属性も値も書かない", func(t *testing.T) {
			t.Parallel()
			r := decideReport([]rootOutcome{drift}, nil)
			for _, b := range []string{r.Body, r.MarkBody} {
				assert.NotContains(t, b, "aws_")
				assert.Contains(t, b, "admin-workload: 1 件")
			}
		})
	})
}

func Test_reportSummary(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("root ごとの結果と、issue と通知の決定を並べる", func(t *testing.T) {
			t.Parallel()
			got := reportSummary([]rootOutcome{
				{root: "admin-identity", kind: outcomeDrift, count: 2},
				{root: "admin-workload", kind: outcomeFailure, job: "drift-target-admin"},
				{root: "security-identity", kind: outcomeUnpromoted},
			}, driftReport{State: "keep", Notify: "failure"})
			assert.Equal(t, "- admin-identity: drift（2 件）\n- admin-workload: failure（drift-target-admin）\n- security-identity: unpromoted\n\n昇格の前の root: 1 件\n\nissue: keep、通知: failure、通知の後の issue: 書かない\n", got)
		})
	})
}

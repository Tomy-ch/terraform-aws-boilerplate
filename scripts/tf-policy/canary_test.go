package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun/tooltest"
)

const layerWithNulls = `{"layer_decl": {"attachments": {
	"aws_s3_bucket_acl": {"parent_arguments": {"aws_s3_bucket": {"acl": null, "grant": "x"}}},
	"aws_s3_bucket_website_configuration": {"parent_arguments": {"aws_s3_bucket": {"website": null}}}},
	"unrelated_deprecated_arguments": {"aws_s3_bucket": {"policy": {"unwritten": null}, "region": {"unwritten": "x"}}}}}`

// policyTree は、canaryExcluded のパターンにそれぞれ1件ずつ当たる rego と、残る Policy を持つ木を建てる。
func policyTree(t *testing.T) string {
	t.Helper()
	files := map[string]string{"iam_action_wildcard.rego": policyOK, "iam_action_wildcard_test.rego": policyTestOK, "lib/input.rego": "package plan.lib\n"}
	for _, pat := range canaryExcluded {
		files[filepath.Base(pat[:len(pat)-len("*.rego")])+"x.rego"] = "package plan.excluded\n"
	}
	return tree(t, files)
}

func Test_canaryCases(t *testing.T) {
	t.Parallel()

	t.Run("通るケースと落ちるケースを持ち、名前は重ならない", func(t *testing.T) {
		t.Parallel()
		cases := canaryCases(t.TempDir(), []byte(layerWithNulls), "    acl = null")
		names := map[string]bool{}
		pass := 0
		for _, c := range cases {
			assert.False(t, names[c.name], c.name)
			names[c.name] = true
			assert.NotEmpty(t, c.pattern, c.name)
			if c.pass {
				pass++
			}
		}
		assert.Equal(t, 2, pass, "ok と aws-unwritten")
		assert.Len(t, cases, 13)
	})
}

func Test_dig(t *testing.T) {
	t.Parallel()

	t.Run("入れ子の map をたどる", func(t *testing.T) {
		t.Parallel()
		d := map[string]any{"a": map[string]any{"b": map[string]any{"c": 1}}}
		got, ok := dig(d, "a", "b")
		require.True(t, ok)
		assert.Equal(t, map[string]any{"c": 1}, got)
		_, ok = dig(d, "a", "x")
		assert.False(t, ok)
	})
}

func Test_awsNullValues(t *testing.T) {
	t.Parallel()

	t.Run("親の引数で null のものと、書かないときの値が null の非推奨の引数を並べる", func(t *testing.T) {
		t.Parallel()
		got, err := awsNullValues([]byte(layerWithNulls))
		require.NoError(t, err)
		assert.Equal(t, "    acl = null\n    policy = null\n    website = null", got)
	})

	t.Run("0件なら errCanary", func(t *testing.T) {
		t.Parallel()
		_, err := awsNullValues([]byte(`{"layer_decl": {}}`))
		require.ErrorIs(t, err, errCanary)
	})

	t.Run("JSON でなければ errRead", func(t *testing.T) {
		t.Parallel()
		_, err := awsNullValues([]byte("x"))
		require.ErrorIs(t, err, errRead)
	})
}

func Test_copyPolicy(t *testing.T) {
	t.Parallel()

	t.Run("木を写し、外すパターンに当たる直下の rego だけを外す", func(t *testing.T) {
		t.Parallel()
		src := policyTree(t)
		require.NoError(t, writeText(filepath.Join(src, "lib", "finops_helper.rego"), "package plan.lib\n"))
		dst := filepath.Join(t.TempDir(), "rego")
		require.NoError(t, copyPolicy(src, dst))
		assert.FileExists(t, filepath.Join(dst, "iam_action_wildcard.rego"))
		assert.FileExists(t, filepath.Join(dst, "lib", "finops_helper.rego"), "下の階層は外さない")
		assert.NoFileExists(t, filepath.Join(dst, "account_baselinex.rego"))
	})

	t.Run("当たらないパターンがあれば errCanary", func(t *testing.T) {
		t.Parallel()
		src := tree(t, map[string]string{"iam_action_wildcard.rego": policyOK})
		err := copyPolicy(src, filepath.Join(t.TempDir(), "rego"))
		require.ErrorIs(t, err, errCanary)
		assert.Contains(t, err.Error(), "account_baseline*.rego")
	})

	t.Run("木が無ければ errRead", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, copyPolicy(filepath.Join(t.TempDir(), "none"), t.TempDir()), errRead)
	})
}

func Test_buildCase(t *testing.T) {
	t.Parallel()

	t.Run("宣言と構成と書き換えを置く", func(t *testing.T) {
		t.Parallel()
		dir := filepath.Join(t.TempDir(), "c")
		c := canaryCase{name: "x", main: "m", data: func(d map[string]any) { d["extra"] = true }, extra: func(c string) error {
			return writeText(filepath.Join(c, "marker"), "")
		}}
		require.NoError(t, buildCase(dir, policyTree(t), []byte("D"), []byte("L"), c))
		assert.Equal(t, "D", readText(t, filepath.Join(dir, "decl.json")))
		assert.Equal(t, "L", readText(t, filepath.Join(dir, "layer-decl.json")))
		assert.Equal(t, canaryLayer+"\n", readText(t, filepath.Join(dir, "layer.json")))
		var data map[string]any
		require.NoError(t, json.Unmarshal([]byte(readText(t, filepath.Join(dir, "data.json"))), &data))
		assert.Equal(t, true, data["extra"])
		assert.Equal(t, "m\n", readText(t, filepath.Join(dir, "base/usecases/x/reference/main.tf")))
		assert.Equal(t, canaryPolicyInput, readText(t, filepath.Join(dir, "base/usecases/x/reference/tests/policy-input.tftest.hcl")))
		assert.FileExists(t, filepath.Join(dir, "marker"))
	})

	t.Run("層の宣言を差し替える", func(t *testing.T) {
		t.Parallel()
		dir := filepath.Join(t.TempDir(), "c")
		c := canaryCase{name: "x", main: "m", layer: func(map[string]any) []byte { return []byte("{}") }}
		require.NoError(t, buildCase(dir, policyTree(t), nil, nil, c))
		assert.Equal(t, "{}\n", readText(t, filepath.Join(dir, "layer.json")))
	})
}

// chainDir は、policyChain が読む配置を、偽の道具の出力（<段>.out）とともに建てる。
func chainDir(t *testing.T, verify, plan, conftest string) string {
	t.Helper()
	files := map[string]string{
		"rego/p.rego": policyOK, "rego/p_test.rego": policyTestOK,
		"decl.json": "{}", "layer-decl.json": "{}", "data.json": `{"policy_plan":{"provider_format_version":"1.0"}}`,
		"base/usecases/x/reference/tests/policy-input.tftest.hcl": "", "base/usecases/x/reference/plan.out": plan,
		"verify.out": verify, "conftest.out": conftest,
	}
	return tree(t, files)
}

func Test_policyChain(t *testing.T) {
	t.Parallel()

	const verifyOK = `[{"filename":"p_test.rego","successes":2}]`
	const resultOK = `[{"filename":"Combined","namespace":"plan.p","successes":1}]`

	t.Run("段を順に走らせ、評価の数が宣言と合えば通る", func(t *testing.T) {
		t.Parallel()
		dir := chainDir(t, verifyOK, planOK, resultOK)
		tool, calls := fakeTool(t)
		out, err := policyChain(dir, "/d", tool, map[string][]byte{})
		require.NoError(t, err, out)
		assert.Contains(t, out, "✅ plan.p: 規則 1 件を評価し、違反はありません")
		root := filepath.Join(dir, "base/usecases/x/reference")
		assert.Equal(t, []string{dir + " verify ", root + " init /d/usecases__x__reference", root + " plan /d/usecases__x__reference", dir + " deprecated ", dir + " conftest "}, calls())
		assert.FileExists(t, filepath.Join(dir, "out", "inputs", "x.json"))
	})

	t.Run("同じ Rego の木と宣言なら conftest verify を1回だけ起動する", func(t *testing.T) {
		t.Parallel()
		verified := map[string][]byte{}
		tool, calls := fakeTool(t)
		for range 2 {
			dir := chainDir(t, verifyOK, planOK, resultOK)
			_, err := policyChain(dir, "/d", tool, verified)
			require.NoError(t, err)
		}
		n := 0
		for _, c := range calls() {
			if len(c) > 7 && c[len(c)-7:] == "verify " {
				n++
			}
		}
		assert.Equal(t, 1, n)
	})

	for name, c := range map[string]struct {
		verify, plan, conftest, failStep, want string
	}{
		"Rego のテストが落ちる":          {verify: `[{"filename":"p","successes":1,"failures":[{"msg":"t"}]}]`, plan: planOK, conftest: resultOK, want: "Rego のテストが落ちました"},
		"init が落ちる":              {verify: verifyOK, plan: planOK, conftest: resultOK, failStep: "init", want: "の init"},
		"plan が落ちる":              {verify: verifyOK, plan: planOK, conftest: resultOK, failStep: "plan", want: "tf-policy が落ちたディレクトリ"},
		"test_plan が2件":          {verify: verifyOK, plan: planOK + planOK, conftest: resultOK, want: "test_plan が 2 件"},
		"非推奨の引数を取り出せない":          {verify: verifyOK, plan: planOK, conftest: resultOK, failStep: "deprecated", want: "非推奨の引数を取り出せません"},
		"Conftest の結果が JSON でない": {verify: verifyOK, plan: planOK, conftest: "x", want: "conftest の結果を解釈できません"},
		"違反がある":                  {verify: verifyOK, plan: planOK, conftest: `[{"namespace":"plan.p","failures":[{"msg":"[PLAN-IAM-01] v"}]}]`, want: "[PLAN-IAM-01] v"},
		"評価されない名前空間":             {verify: verifyOK, plan: planOK, conftest: `[{"namespace":"plan.p","successes":0}]`, want: "plan.p: ❌ 名前空間の規則が1つも評価されていません"},
	} {
		t.Run(name+"なら落ちる", func(t *testing.T) {
			t.Parallel()
			dir := chainDir(t, c.verify, c.plan, c.conftest)
			if c.failStep != "" {
				target := dir
				if c.failStep == "init" || c.failStep == "plan" {
					target = filepath.Join(dir, "base/usecases/x/reference")
				}
				require.NoError(t, writeText(filepath.Join(target, "fail-"+c.failStep), ""))
			}
			tool, _ := fakeTool(t)
			out, err := policyChain(dir, "/d", tool, map[string][]byte{})
			require.Error(t, err)
			assert.Contains(t, out+err.Error(), c.want)
		})
	}

	for step, before := range map[string][]string{
		stepVerify:     nil,
		stepInit:       {stepVerify},
		stepDeprecated: {stepVerify, stepInit, stepPlan},
		stepConftest:   {stepVerify, stepInit, stepPlan, stepDeprecated},
	} {
		t.Run(step+" を起動できなければ、後の段を起動せずに errTool", func(t *testing.T) {
			t.Parallel()
			dir := chainDir(t, verifyOK, planOK, resultOK)
			fake, calls := fakeTool(t)
			tool := func(d string, env []string, s string) *exec.Cmd {
				if s == step {
					return tooltest.Missing(d, env, s)
				}
				return fake(d, env, s)
			}
			verified := map[string][]byte{}
			_, err := policyChain(dir, "/d", tool, verified)
			require.ErrorIs(t, err, errTool)
			require.NotErrorIs(t, err, errFailed)
			assert.Equal(t, 2, exitCode(err))
			var started []string
			for _, c := range calls() {
				started = append(started, strings.Fields(c)[1])
			}
			assert.Equal(t, before, started)
			if step == stepVerify {
				assert.Empty(t, verified, "起動できなかった verify の結果を持ち回らない")
			}
		})
	}

	t.Run("init がシグナルで終われば errTool", func(t *testing.T) {
		t.Parallel()
		dir := chainDir(t, verifyOK, planOK, resultOK)
		f := tooltest.New(t)
		f.Set(stepVerify, "out", verifyOK)
		f.Set(stepInit, "kill", "")
		_, err := policyChain(dir, "/d", f.Tool(), map[string][]byte{})
		require.ErrorIs(t, err, errTool)
		assert.Contains(t, err.Error(), "がシグナルで終わりました")
	})

	// rcTool は、verify・conftest が rc=1 で結果を出し、plan が planOK を出す偽の道具。
	rcTool := func(t *testing.T) *tooltest.Fake {
		t.Helper()
		f := tooltest.New(t)
		f.Set(stepVerify, "out", verifyOK)
		f.Set(stepVerify, "rc", "1")
		f.Set(stepPlan, "out", planOK)
		f.Set(stepConftest, "out", resultOK)
		f.Set(stepConftest, "rc", "1")
		return f
	}

	t.Run("対照: verify と conftest の 0 以外の終了コードは捨てて結果を読む", func(t *testing.T) {
		t.Parallel()
		out, err := policyChain(chainDir(t, verifyOK, planOK, resultOK), "/d", rcTool(t).Tool(), map[string][]byte{})
		require.NoError(t, err, out)
	})

	for _, step := range []string{stepInit, stepDeprecated} {
		t.Run("対照: "+step+" の 0 以外の終了コードは errFailed で終了コードは 1", func(t *testing.T) {
			t.Parallel()
			f := rcTool(t)
			f.Set(step, "rc", "1")
			_, err := policyChain(chainDir(t, verifyOK, planOK, resultOK), "/d", f.Tool(), map[string][]byte{})
			require.ErrorIs(t, err, errFailed)
			require.NotErrorIs(t, err, errTool)
			assert.Equal(t, 1, exitCode(err))
		})
	}

	t.Run("Policy の形が崩れていれば Conftest を起動せずに落ちる", func(t *testing.T) {
		t.Parallel()
		dir := chainDir(t, verifyOK, planOK, resultOK)
		require.NoError(t, appendText(filepath.Join(dir, "rego", "p.rego"), "\nwarn contains \"w\" if false\n"))
		tool, calls := fakeTool(t)
		out, err := policyChain(dir, "/d", tool, map[string][]byte{})
		require.ErrorIs(t, err, errFailed)
		assert.Contains(t, out, "deny contains 以外の形")
		assert.Empty(t, calls())
	})
}

func Test_verifyKey(t *testing.T) {
	t.Parallel()

	t.Run("Rego か宣言の中身が変われば鍵が変わる", func(t *testing.T) {
		t.Parallel()
		dir := tree(t, map[string]string{"decl.json": "a", "layer-decl.json": "b"})
		files := []regoFile{{rel: "p.rego", src: "x"}}
		k1, err := verifyKey(dir, files)
		require.NoError(t, err)
		k2, err := verifyKey(dir, []regoFile{{rel: "p.rego", src: "y"}})
		require.NoError(t, err)
		require.NoError(t, writeText(filepath.Join(dir, "decl.json"), "c"))
		k3, err := verifyKey(dir, files)
		require.NoError(t, err)
		assert.NotEqual(t, k1, k2)
		assert.NotEqual(t, k1, k3)
	})

	t.Run("宣言が無ければ errRead", func(t *testing.T) {
		t.Parallel()
		_, err := verifyKey(t.TempDir(), nil)
		require.ErrorIs(t, err, errRead)
	})
}

func Test_runCanary(t *testing.T) {
	t.Parallel()

	argsWith := func(t *testing.T, work string) []string {
		t.Helper()
		ref := tree(t, map[string]string{"versions.tf": "", ".terraform.lock.hcl": "", "decl.json": "{}", "layer.json": layerWithNulls})
		return []string{"-data", t.TempDir(), "-work", work, "-policy", policyTree(t),
			"-decl", filepath.Join(ref, "decl.json"), "-layer", filepath.Join(ref, "layer.json"), "-reference", ref}
	}
	args := func(t *testing.T) []string {
		t.Helper()
		return argsWith(t, "tmp/w")
	}

	t.Run("期待どおりでないケースを名指して errCanary", func(t *testing.T) {
		t.Parallel()
		tool, _ := fakeTool(t)
		var out bytes.Buffer
		err := runCanary(args(t), &out, tool, tree(t, nil))
		require.ErrorIs(t, err, errCanary)
		assert.Contains(t, err.Error(), "ok ")
		assert.Contains(t, out.String(), "--- canary ok の出力")
	})

	t.Run("道具を起動できなければ、ケースの不成立に畳まずに errTool で終了コードは 2", func(t *testing.T) {
		t.Parallel()
		var out bytes.Buffer
		err := runCanary(args(t), &out, tooltest.Missing, tree(t, nil))
		require.ErrorIs(t, err, errTool)
		require.NotErrorIs(t, err, errCanary)
		assert.Equal(t, 2, exitCode(err))
		assert.NotContains(t, out.String(), "--- canary")
	})

	t.Run("落ちることを期待するケースで道具を起動できなくても、期待どおりに落ちたと数えずに errTool", func(t *testing.T) {
		t.Parallel()
		fake, _ := fakeTool(t)
		tool := func(d string, env []string, step string) *exec.Cmd {
			if step == stepVerify && filepath.Base(d) == "failing-rego-test" {
				return tooltest.Missing(d, env, step)
			}
			return fake(d, env, step)
		}
		var out bytes.Buffer
		err := runCanary(args(t), &out, tool, tree(t, nil))
		require.ErrorIs(t, err, errTool)
		require.NotErrorIs(t, err, errCanary)
		assert.Equal(t, 2, exitCode(err))
		assert.Contains(t, out.String(), "--- canary ok の出力", "先のケースは偽の道具で期待どおりでない")
		assert.Contains(t, err.Error(), "canary failing-rego-test で打ち切りました（それまでに落ちたケース: ok ")
	})

	t.Run("層の宣言から null の値を組めなければ errCanary", func(t *testing.T) {
		t.Parallel()
		a := args(t)
		layer := filepath.Join(t.TempDir(), "layer.json")
		require.NoError(t, os.WriteFile(layer, []byte(`{"layer_decl":{}}`), 0o600))
		a[9] = layer
		tool, calls := fakeTool(t)
		require.ErrorIs(t, runCanary(a, &bytes.Buffer{}, tool, tree(t, nil)), errCanary)
		assert.Empty(t, calls())
	})

	t.Run("宣言を読めなければ errRead", func(t *testing.T) {
		t.Parallel()
		a := args(t)
		a[7] = filepath.Join(t.TempDir(), "none")
		tool, _ := fakeTool(t)
		require.ErrorIs(t, runCanary(a, &bytes.Buffer{}, tool, tree(t, nil)), errRead)
	})

	t.Run("引数の誤りは errUsage", func(t *testing.T) {
		t.Parallel()
		tool, _ := fakeTool(t)
		require.ErrorIs(t, runCanary([]string{"-data", "rel"}, &bytes.Buffer{}, tool, tree(t, nil)), errUsage)
	})

	t.Run("-work が tmp/ の下の相対パスでなければ、何も消さず道具も起動せずに、-work を名指す errUsage で終了コードは 2", func(t *testing.T) {
		t.Parallel()
		for _, work := range []string{
			"", ".", "tmp", "tmp/", "tmp/.", "<root>/keep", "keep", "tmp/../keep", "../x", "TMP/x",
			"tmp/out", "tmp/out/w", "tmp/self",
		} {
			t.Run(work, func(t *testing.T) {
				t.Parallel()
				world, root, _ := workWorld(t)
				if work == "<root>/keep" {
					work = filepath.Join(root, "keep")
				}
				before := snapshot(t, world)
				tool, calls := fakeTool(t)
				err := runCanary(argsWith(t, work), &bytes.Buffer{}, tool, root)
				require.ErrorIs(t, err, errUsage)
				assert.Equal(t, 2, exitCode(err))
				if work != "" {
					assert.Contains(t, err.Error(), fmt.Sprintf("-work %q", work))
				}
				assert.Empty(t, calls())
				assert.Equal(t, before, snapshot(t, world))
			})
		}
	})

	t.Run("-work を2度渡せば後の値を使う", func(t *testing.T) {
		t.Parallel()
		world, root, _ := workWorld(t)
		before := snapshot(t, world)
		tool, calls := fakeTool(t)
		err := runCanary(append(argsWith(t, "tmp/c/work"), "-work", "../x"), &bytes.Buffer{}, tool, root)
		require.ErrorIs(t, err, errUsage)
		assert.Contains(t, err.Error(), `-work "../x"`)
		assert.Empty(t, calls())
		assert.Equal(t, before, snapshot(t, world))

		require.ErrorIs(t, runCanary(append(argsWith(t, "../x"), "-work", "tmp/c/work"), &bytes.Buffer{}, tool, root), errCanary)
		assert.DirExists(t, filepath.Join(root, "tmp", "c", "work", "ok"))
	})

	t.Run("-work だけを作り直し、tmp/ 自身・兄弟・tmp/ の外を残す", func(t *testing.T) {
		t.Parallel()
		for _, work := range []string{"tmp/c/work", "./tmp/c/work"} {
			t.Run(work, func(t *testing.T) {
				t.Parallel()
				world, root, _ := workWorld(t)
				before := snapshot(t, world)
				tool, calls := fakeTool(t)
				require.ErrorIs(t, runCanary(argsWith(t, work), &bytes.Buffer{}, tool, root), errCanary, "偽の道具では期待どおりにならない")
				after := snapshot(t, world)
				for rel, sum := range before {
					if !strings.HasPrefix(rel, filepath.Join("root", "tmp", "c", "work")) {
						assert.Equal(t, sum, after[rel], rel)
					}
				}
				assert.NotContains(t, after, filepath.Join("root", "tmp", "c", "work", "old"), "前回の中身は消す")
				require.NotEmpty(t, calls())
				assert.True(t, strings.HasPrefix(calls()[0], filepath.Join(root, "tmp", "c", "work")+string(filepath.Separator)), "道具は絶対パスの fixture で起動する: %s", calls()[0])
			})
		}
	})

	t.Run("root が相対パスでも、道具は絶対パスの fixture で起動する", func(t *testing.T) {
		t.Parallel()
		_, root, _ := workWorld(t)
		cwd, err := os.Getwd()
		require.NoError(t, err)
		rel, err := filepath.Rel(cwd, root)
		require.NoError(t, err)
		require.False(t, filepath.IsAbs(rel))
		fake, _ := fakeTool(t)
		var dirs []string
		tool := func(d string, env []string, step string) *exec.Cmd {
			dirs = append(dirs, d)
			return fake(d, env, step)
		}
		require.ErrorIs(t, runCanary(argsWith(t, "tmp/c/work"), &bytes.Buffer{}, tool, rel), errCanary)
		require.NotEmpty(t, dirs)
		for _, d := range dirs {
			assert.True(t, filepath.IsAbs(d), d)
			assert.True(t, strings.HasPrefix(d, filepath.Join(root, "tmp", "c", "work")+string(filepath.Separator)), d)
		}
	})

	t.Run("root に tmp/ が無ければ作って建てる", func(t *testing.T) {
		t.Parallel()
		root := tree(t, map[string]string{"keep/f": "keep"})
		tool, _ := fakeTool(t)
		require.ErrorIs(t, runCanary(argsWith(t, "tmp/c/work"), &bytes.Buffer{}, tool, root), errCanary)
		assert.DirExists(t, filepath.Join(root, "tmp", "c", "work", "ok"))
		assert.FileExists(t, filepath.Join(root, "keep", "f"))
	})

	git := func(t *testing.T, dir string, args ...string) {
		t.Helper()
		c := exec.CommandContext(t.Context(), "git", append([]string{"-c", "core.hooksPath=/dev/null"}, args...)...)
		c.Dir = dir
		out, err := c.CombinedOutput()
		require.NoError(t, err, string(out))
	}

	t.Run("tmp/ 自身が git の追跡している外への symlink なら使い方の誤りで、先を消さない", func(t *testing.T) {
		t.Parallel()
		world := tree(t, map[string]string{"root/keep/f": "keep", "elsewhere/c/work/old": "old", "elsewhere/stale.txt": "stale"})
		root, elsewhere := filepath.Join(world, "root"), filepath.Join(world, "elsewhere")
		require.NoError(t, os.Symlink(elsewhere, filepath.Join(root, "tmp")))
		git(t, root, "init", "-q")
		git(t, root, "add", "tmp")
		before := snapshot(t, elsewhere)
		tool, calls := fakeTool(t)
		err := runCanary(argsWith(t, "tmp/c/work"), &bytes.Buffer{}, tool, root)
		require.ErrorIs(t, err, errUsage)
		assert.Equal(t, 2, exitCode(err))
		assert.Empty(t, calls())
		assert.Equal(t, before, snapshot(t, elsewhere))
	})

	t.Run("tmp/ 自身が git の追跡していない外への symlink なら、その先を tmp/ として建て、先の兄弟を残す", func(t *testing.T) {
		t.Parallel()
		world := tree(t, map[string]string{"root/keep/f": "keep", "elsewhere/c/data/f": "data", "elsewhere/stale.txt": "stale"})
		root, elsewhere := filepath.Join(world, "root"), filepath.Join(world, "elsewhere")
		require.NoError(t, os.Symlink(elsewhere, filepath.Join(root, "tmp")))
		git(t, root, "init", "-q")
		tool, _ := fakeTool(t)
		require.ErrorIs(t, runCanary(argsWith(t, "tmp/c/work"), &bytes.Buffer{}, tool, root), errCanary, "偽の道具では期待どおりにならない")
		assert.DirExists(t, filepath.Join(elsewhere, "c", "work", "ok"))
		assert.FileExists(t, filepath.Join(elsewhere, "c", "data", "f"))
		assert.FileExists(t, filepath.Join(elsewhere, "stale.txt"))
		assert.FileExists(t, filepath.Join(root, "keep", "f"))
	})
}

func Test_planCanary(t *testing.T) {
	t.Parallel()

	t.Run("root を runCanary へ渡す", func(t *testing.T) {
		t.Parallel()
		ref := tree(t, map[string]string{"versions.tf": "", ".terraform.lock.hcl": "", "decl.json": "{}", "layer.json": layerWithNulls})
		args := func(work string) []string {
			return []string{"-data", t.TempDir(), "-work", work, "-policy", policyTree(t),
				"-decl", filepath.Join(ref, "decl.json"), "-layer", filepath.Join(ref, "layer.json"), "-reference", ref}
		}
		world, root, _ := workWorld(t)
		before := snapshot(t, world)
		tool, calls := fakeTool(t)
		require.ErrorIs(t, planCanary(root)(args("tmp/out/w"), &bytes.Buffer{}, tool), errUsage, "root の tmp/out は外を指す")
		assert.Empty(t, calls())
		assert.Equal(t, before, snapshot(t, world))

		require.ErrorIs(t, planCanary(root)(args("tmp/c/work"), &bytes.Buffer{}, tool), errCanary)
		assert.DirExists(t, filepath.Join(root, "tmp", "c", "work", "ok"))
	})
}

func Test_writeText(t *testing.T) {
	t.Parallel()

	t.Run("親を作って書き、親がファイルなら失敗する", func(t *testing.T) {
		t.Parallel()
		p := filepath.Join(t.TempDir(), "a", "b")
		require.NoError(t, writeText(p, "x"))
		assert.Equal(t, "x", readText(t, p))
		require.Error(t, writeText(filepath.Join(p, "c"), "x"))
	})
}

func Test_appendText(t *testing.T) {
	t.Parallel()

	t.Run("末尾に足し、無ければ失敗する", func(t *testing.T) {
		t.Parallel()
		p := filepath.Join(tree(t, map[string]string{"f": "a"}), "f")
		require.NoError(t, appendText(p, "b"))
		assert.Equal(t, "ab", readText(t, p))
		require.Error(t, appendText(filepath.Join(t.TempDir(), "none"), "b"))
	})
}

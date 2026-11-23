package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	args := func(t *testing.T) []string {
		t.Helper()
		ref := tree(t, map[string]string{"versions.tf": "", ".terraform.lock.hcl": "", "decl.json": "{}", "layer.json": layerWithNulls})
		return []string{"-data", t.TempDir(), "-work", filepath.Join(t.TempDir(), "w"), "-policy", policyTree(t),
			"-decl", filepath.Join(ref, "decl.json"), "-layer", filepath.Join(ref, "layer.json"), "-reference", ref}
	}

	t.Run("期待どおりでないケースを名指して errCanary", func(t *testing.T) {
		t.Parallel()
		tool, _ := fakeTool(t)
		var out bytes.Buffer
		err := runCanary(args(t), &out, tool)
		require.ErrorIs(t, err, errCanary)
		assert.Contains(t, err.Error(), "ok ")
		assert.Contains(t, out.String(), "--- canary ok の出力")
	})

	t.Run("層の宣言から null の値を組めなければ errCanary", func(t *testing.T) {
		t.Parallel()
		a := args(t)
		layer := filepath.Join(t.TempDir(), "layer.json")
		require.NoError(t, os.WriteFile(layer, []byte(`{"layer_decl":{}}`), 0o600))
		a[9] = layer
		tool, calls := fakeTool(t)
		require.ErrorIs(t, runCanary(a, &bytes.Buffer{}, tool), errCanary)
		assert.Empty(t, calls())
	})

	t.Run("宣言を読めなければ errRead", func(t *testing.T) {
		t.Parallel()
		a := args(t)
		a[7] = filepath.Join(t.TempDir(), "none")
		tool, _ := fakeTool(t)
		require.ErrorIs(t, runCanary(a, &bytes.Buffer{}, tool), errRead)
	})

	t.Run("引数の誤りは errUsage", func(t *testing.T) {
		t.Parallel()
		tool, _ := fakeTool(t)
		require.ErrorIs(t, runCanary([]string{"-data", "rel"}, &bytes.Buffer{}, tool), errUsage)
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

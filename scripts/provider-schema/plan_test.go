package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/testenv"
)

// providerBody は、provider_schemas の値（Provider のアドレス → resource_schemas）を組みます。型の名前は
// 並びの順に aws_0、aws_1、… です。raw.json（rawJSON）と test_plan（planJSONL）の両方がこの本体を包みます。
func providerBody(provider string, resources ...string) string {
	items := make([]string, 0, len(resources))
	for i, r := range resources {
		items = append(items, fmt.Sprintf("\"aws_%d\": %s", i, r))
	}

	return fmt.Sprintf(`{%q: {"resource_schemas": {%s}}}`, provider, strings.Join(items, ","))
}

// planJSONL は `terraform test -json -verbose` の出力の形で、test_plan の値を1件だけ持つ jsonl を組みます。
// changes は resource_changes の (mode, type) の組で、省けば managed の aws_0 を1件持ちます。
func planJSONL(schemas string, changes ...[2]string) string {
	if len(changes) == 0 {
		changes = [][2]string{managed("aws_0")}
	}
	rcs := make([]string, 0, len(changes))
	for _, c := range changes {
		rcs = append(rcs, fmt.Sprintf(`{"mode": %q, "type": %q}`, c[0], c[1]))
	}

	return `{"@level": "info", "type": "version"}` + "\n" +
		fmt.Sprintf(`{"@level": "info", "type": "test_plan", "test_plan": {"provider_format_version": "1.0", "provider_schemas": %s, "resource_changes": [%s]}}`, schemas, strings.Join(rcs, ",")) + "\n" +
		`{"@level": "info", "type": "test_summary"}` + "\n"
}

func managed(typ string) [2]string { return [2]string{"managed", typ} }

func Test_decodePlan(t *testing.T) {
	t.Parallel()

	t.Run("test_plan のすべての Provider の resource の型を返す", func(t *testing.T) {
		t.Parallel()
		schemas := fmt.Sprintf(`{%q: {"resource_schemas": {"aws_0": %s}}, "terraform.io/builtin/terraform": {"resource_schemas": {"terraform_data": {"block": {}}}}}`, awsProvider, resourceX)
		got, err := decodePlan("a.jsonl", strings.NewReader(planJSONL(schemas, managed("aws_0"), managed("terraform_data"), [2]string{"data", "aws_region"})))
		require.NoError(t, err)
		assert.Len(t, got, 2)
		assert.Equal(t, entryX, extract(got["aws_0"]))
	})

	// 条件は extract の1つだけなので、2つの経路の一覧が揃うかは、2つの decoder が同じ本体から同じ
	// resourceSchema を作るかで決まる。
	t.Run("raw.json と同じ本体から decodeSchema と同じ resource の schema を返す", func(t *testing.T) {
		t.Parallel()
		body := providerBody(awsProvider, resourceX, `{"block": {"attributes": {"n": {"optional": true}}}}`)
		fromRaw, err := decodeSchema("raw.json", []byte(fmt.Sprintf(`{"format_version": "1.0", "provider_schemas": %s}`, body)), awsProvider)
		require.NoError(t, err)
		fromPlan, err := decodePlan("a.jsonl", strings.NewReader(planJSONL(body)))
		require.NoError(t, err)
		assert.Equal(t, fromRaw, fromPlan)
	})

	t.Run("64KB を超える1行の test_plan も読む", func(t *testing.T) {
		t.Parallel()
		resources := make([]string, 0, 2000)
		for range 2000 {
			resources = append(resources, resourceX)
		}
		body := planJSONL(providerBody(awsProvider, resources...))
		require.Greater(t, len(body), 64*1024)
		got, err := decodePlan("big.jsonl", strings.NewReader(body))
		require.NoError(t, err)
		assert.Len(t, got, 2000)
	})

	t.Run("null の行は test_plan に数えない", func(t *testing.T) {
		t.Parallel()
		_, err := decodePlan("a.jsonl", strings.NewReader("null\n"+planJSONL(providerBody(awsProvider, resourceX))))
		require.NoError(t, err)
	})

	tests := map[string]struct {
		body string
		want error
	}{
		"空":             {body: "", want: errPlanMalformed},
		"空行だけ":          {body: "\n\n", want: errPlanMalformed},
		"JSON として読めない行": {body: `{"type": "test_plan"` + "\n", want: errPlanMalformed},
		"test_plan の後ろに読めない行がある":                 {body: planJSONL(providerBody(awsProvider, resourceX)) + `{"type": "test_summary"` + "\n", want: errPlanMalformed},
		"オブジェクトでない値の行":                           {body: `["test_plan"]` + "\n" + planJSONL(providerBody(awsProvider, resourceX)), want: errPlanMalformed},
		"test_plan が0件":                          {body: `{"type": "version"}` + "\n", want: errPlanMalformed},
		"test_plan が2件":                          {body: planJSONL(providerBody(awsProvider, resourceX)) + planJSONL(providerBody(awsProvider, resourceX)), want: errPlanMalformed},
		"test_plan の値が無い":                        {body: `{"type": "test_plan"}` + "\n", want: errPlanMalformed},
		"provider_schemas が無い（resource を持たない構成）": {body: `{"type": "test_plan", "test_plan": {"output_changes": {}}}` + "\n", want: errNoResources},
		"provider_schemas が null":                {body: `{"type": "test_plan", "test_plan": {"provider_schemas": null, "resource_changes": [{"mode": "managed", "type": "aws_0"}]}}` + "\n", want: errNoResources},
		"provider_schemas が空":                    {body: planJSONL(`{}`), want: errNoResources},
		"resource_schemas が0件":                   {body: planJSONL(providerBody(awsProvider)), want: errNoResources},
		"属性の deprecated が真偽値でない":                 {body: planJSONL(providerBody(awsProvider, `{"block": {"attributes": {"a": {"optional": true, "deprecated": "yes"}}}}`)), want: errPlanMalformed},
		"block の deprecated が真偽値でない":             {body: planJSONL(providerBody(awsProvider, `{"block": {"block_types": {"b": {"block": {"deprecated": 1}}}}}`)), want: errPlanMalformed},
		"resource_changes が無い":                   {body: fmt.Sprintf(`{"type": "test_plan", "test_plan": {"provider_schemas": %s}}`, providerBody(awsProvider, resourceX)) + "\n", want: errPlanMalformed},
		"managed の変更が0件":                         {body: planJSONL(providerBody(awsProvider, resourceX), [2]string{"data", "aws_region"}), want: errPlanMalformed},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := decodePlan("a.jsonl", strings.NewReader(tt.body))
			require.ErrorIs(t, err, tt.want)
		})
	}

	t.Run("同じ型を2つの Provider が持てば、型と2つの Provider を示して errPlanMalformed", func(t *testing.T) {
		t.Parallel()
		body := planJSONL(`{"a": {"resource_schemas": {"x_y": {"block": {}}}}, "b": {"resource_schemas": {"x_y": {"block": {}}}}}`)
		_, err := decodePlan("a.jsonl", strings.NewReader(body))
		require.ErrorIs(t, err, errPlanMalformed)
		assert.Contains(t, err.Error(), "x_y")
		assert.Contains(t, err.Error(), " a ")
		assert.Contains(t, err.Error(), " b ")
	})

	t.Run("schema に無い managed の型を、型ごとに1度だけ昇順で示して errPlanTypeMissing", func(t *testing.T) {
		t.Parallel()
		body := planJSONL(providerBody(awsProvider, resourceX), managed("aws_z"), managed("aws_0"), managed("aws_a"), managed("aws_z"))
		_, err := decodePlan("a.jsonl", strings.NewReader(body))
		require.ErrorIs(t, err, errPlanTypeMissing)
		assert.Contains(t, err.Error(), "a.jsonl: aws_a aws_z:")
	})

	t.Run("data source の型は schema と突き合わせない", func(t *testing.T) {
		t.Parallel()
		_, err := decodePlan("a.jsonl", strings.NewReader(planJSONL(providerBody(awsProvider, resourceX), managed("aws_0"), [2]string{"data", "aws_region"})))
		require.NoError(t, err)
	})
}

func Test_planDeprecated(t *testing.T) {
	t.Parallel()

	readDeprecated := func(t *testing.T, path string) map[string][]string {
		t.Helper()
		var got struct {
			ProviderSchema struct {
				DeprecatedArguments map[string][]string `json:"deprecated_arguments"`
			} `json:"provider_schema"`
		}
		require.NoError(t, json.Unmarshal([]byte(readFile(t, path)), &got))

		return got.ProviderSchema.DeprecatedArguments
	}

	t.Run("root をまたいだ和を、非推奨の無い型も空の配列で書く", func(t *testing.T) {
		t.Parallel()
		base := t.TempDir()
		a := writeFile(t, base, "a.jsonl", planJSONL(providerBody(awsProvider, resourceX, `{"block": {"attributes": {"n": {"optional": true}}}}`)))
		b := writeFile(t, base, "b.jsonl", planJSONL(providerBody(awsProvider, `{"block": {"attributes": {"z": {"required": true, "deprecated": true}}}}`)))
		out := filepath.Join(base, "provider-schema.json")
		var stderr bytes.Buffer
		require.NoError(t, planDeprecated([]string{"-out", out, a, b}, &stderr))
		assert.Equal(t, map[string][]string{"aws_0": {"legacy", "old", "z"}, "aws_1": {}}, readDeprecated(t, out))
		assert.Equal(t, fmt.Sprintf("provider-schema plan-deprecated: 入力 2 件（%s: 2 型、%s: 1 型）、和 2 型を %s へ書きました\n", a, b, out), stderr.String())
	})

	t.Run("root の一部の型だけが書ける引数を持てば受け、引数の無い型も空の配列で書く", func(t *testing.T) {
		t.Parallel()
		base := t.TempDir()
		a := writeFile(t, base, "a.jsonl", planJSONL(providerBody(awsProvider, resourceX, `{"block": {}}`)))
		out := filepath.Join(base, "o.json")
		require.NoError(t, planDeprecated([]string{"-out", out, a}, &bytes.Buffer{}))
		assert.Equal(t, map[string][]string{"aws_0": {"legacy", "old"}, "aws_1": {}}, readDeprecated(t, out))
	})

	t.Run("-list の一覧の jsonl を読む", func(t *testing.T) {
		t.Parallel()
		base := t.TempDir()
		a := writeFile(t, base, "a.jsonl", planJSONL(providerBody(awsProvider, resourceX)))
		list := writeFile(t, base, "plans.txt", a+"\n\n")
		out := filepath.Join(base, "o.json")
		require.NoError(t, planDeprecated([]string{"-out", out, "-list", list}, &bytes.Buffer{}))
		assert.Equal(t, map[string][]string{"aws_0": {"legacy", "old"}}, readDeprecated(t, out))
	})

	t.Run("-list の一覧が空なら errNoPlans", func(t *testing.T) {
		t.Parallel()
		base := t.TempDir()
		list := writeFile(t, base, "plans.txt", "")
		out := filepath.Join(base, "o.json")
		require.ErrorIs(t, planDeprecated([]string{"-out", out, "-list", list}, &bytes.Buffer{}), errNoPlans)
		assert.NoFileExists(t, out)
	})

	t.Run("-list を読めなければ errPlanMalformed", func(t *testing.T) {
		t.Parallel()
		out := filepath.Join(t.TempDir(), "o.json")
		require.ErrorIs(t, planDeprecated([]string{"-out", out, "-list", filepath.Join(t.TempDir(), "none")}, &bytes.Buffer{}), errPlanMalformed)
	})

	t.Run("-list と位置引数を両方渡せば errUsage", func(t *testing.T) {
		t.Parallel()
		base := t.TempDir()
		list := writeFile(t, base, "plans.txt", "x\n")
		require.ErrorIs(t, planDeprecated([]string{"-out", filepath.Join(base, "o.json"), "-list", list, "y"}, &bytes.Buffer{}), errUsage)
	})

	t.Run("入力が0件なら errNoPlans", func(t *testing.T) {
		t.Parallel()
		out := filepath.Join(t.TempDir(), "o.json")
		require.ErrorIs(t, planDeprecated([]string{"-out", out}, &bytes.Buffer{}), errNoPlans)
		assert.NoFileExists(t, out)
	})

	usage := map[string]func(a string) []string{
		"-out が無い":  func(a string) []string { return []string{a} },
		"知らない flag": func(a string) []string { return []string{"-x", a} },
		"引数が無ければ errNoPlans より errUsage": func(string) []string { return nil },
	}
	for name, args := range usage {
		t.Run(name+"なら errUsage", func(t *testing.T) {
			t.Parallel()
			a := writeFile(t, t.TempDir(), "a.jsonl", planJSONL(providerBody(awsProvider, resourceX)))
			err := planDeprecated(args(a), &bytes.Buffer{})
			require.ErrorIs(t, err, errUsage)
			assert.NotErrorIs(t, err, errNoPlans)
		})
	}

	// 失敗はどれも、既に在る -out を書き換えない。
	t.Run("入力の1件でも使えなければ失敗し、既存の -out を変えない", func(t *testing.T) {
		t.Parallel()
		base := t.TempDir()
		good := writeFile(t, base, "a.jsonl", planJSONL(providerBody(awsProvider, resourceX)))
		tests := map[string]struct {
			path string
			want error
		}{
			"無い file":        {path: filepath.Join(base, "missing.jsonl"), want: errPlanMalformed},
			"空の file":        {path: writeFile(t, base, "empty.jsonl", ""), want: errPlanMalformed},
			"読めない JSON":      {path: writeFile(t, base, "broken.jsonl", "{"), want: errPlanMalformed},
			"schema の無い入力":   {path: writeFile(t, base, "none.jsonl", `{"type": "test_plan", "test_plan": {}}`+"\n"), want: errNoResources},
			"schema に無い型の変更": {path: writeFile(t, base, "missing-type.jsonl", planJSONL(providerBody(awsProvider, resourceX), managed("aws_s3_bucket"))), want: errPlanTypeMissing},
			"書ける引数の無い root":  {path: writeFile(t, base, "no-args.jsonl", planJSONL(providerBody(awsProvider, `{"block": {}}`, `{"block": {"attributes": {"id": {"computed": true}}}}`))), want: errNoArguments},
		}
		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				out := writeFile(t, t.TempDir(), "o.json", "previous\n")
				err := planDeprecated([]string{"-out", out, good, tt.path}, &bytes.Buffer{})
				require.ErrorIs(t, err, tt.want)
				assert.Contains(t, err.Error(), tt.path)
				assert.Equal(t, "previous\n", readFile(t, out))
			})
		}
	})

	t.Run("書き込めなければ失敗し、出力を残さない", func(t *testing.T) {
		t.Parallel()
		testenv.RequireNonRoot(t, "特権実行では読み取り専用ディレクトリへも書けるため検証できない")
		base := t.TempDir()
		a := writeFile(t, base, "a.jsonl", planJSONL(providerBody(awsProvider, resourceX)))
		parent := filepath.Join(base, "ro")
		require.NoError(t, os.MkdirAll(parent, 0o750))
		require.NoError(t, os.Chmod(parent, 0o500))
		t.Cleanup(func() { _ = os.Chmod(parent, 0o750) })
		out := filepath.Join(parent, "o.json")
		require.ErrorIs(t, planDeprecated([]string{"-out", out, a}, &bytes.Buffer{}), fs.ErrPermission)
		assert.NoFileExists(t, out)
	})
}

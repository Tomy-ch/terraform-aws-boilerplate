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

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/testenv"
)

// resourceX は、書ける属性・computed だけの属性・非推奨の属性・computed だけの非推奨の属性・block・
// 非推奨の block を1つずつ持つ。
const resourceX = `{"block": {
	"attributes": {
		"id": {"type": "string", "computed": true},
		"name": {"type": "string", "required": true},
		"tags": {"type": ["map", "string"], "optional": true, "computed": true},
		"old": {"type": "string", "optional": true, "deprecated": true},
		"gone": {"type": "string", "computed": true, "deprecated": true}
	},
	"block_types": {
		"rule": {"nesting_mode": "list", "block": {"attributes": {"x": {"type": "string", "optional": true, "deprecated": true}}}},
		"legacy": {"nesting_mode": "list", "block": {"deprecated": true}}
	}
}}`

var entryX = entry{
	Arguments:  []string{"legacy", "name", "old", "rule", "tags"},
	Deprecated: []string{"legacy", "old"},
	Blocks:     []string{"legacy", "rule"},
}

func rawJSON(provider string, resources ...string) string {
	items := make([]string, 0, len(resources))
	for i, r := range resources {
		items = append(items, fmt.Sprintf("\"aws_%d\": %s", i, r))
	}

	return fmt.Sprintf(`{"format_version": "1.0", "provider_schemas": {%q: {"resource_schemas": {%s}}}}`, provider, strings.Join(items, ","))
}

func Test_extract(t *testing.T) {
	t.Parallel()

	t.Run("書ける引数・非推奨・block を取り出し、computed だけの属性を除く", func(t *testing.T) {
		t.Parallel()
		got, err := decodeSchema("raw.json", []byte(rawJSON(awsProvider, resourceX)), awsProvider)
		require.NoError(t, err)
		assert.Equal(t, entryX, extract(got["aws_0"]))
	})

	t.Run("属性も block も無ければ空の配列", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, entry{Arguments: []string{}, Deprecated: []string{}, Blocks: []string{}}, extract(resourceSchema{}))
	})
}

func Test_decodeSchema(t *testing.T) {
	t.Parallel()

	t.Run("指定の Provider の resource の型を返す", func(t *testing.T) {
		t.Parallel()
		got, err := decodeSchema("raw.json", []byte(rawJSON(awsProvider, resourceX, `{"block": {}}`)), awsProvider)
		require.NoError(t, err)
		assert.Len(t, got, 2)
	})

	t.Run("format_version の 1. の後の版を受ける", func(t *testing.T) {
		t.Parallel()
		data := strings.Replace(rawJSON(awsProvider, resourceX), `"format_version": "1.0"`, `"format_version": "1.1"`, 1)
		got, err := decodeSchema("raw.json", []byte(data), awsProvider)
		require.NoError(t, err)
		assert.Len(t, got, 1)
	})

	tests := map[string]struct {
		data string
		want error
	}{
		"空":                     {data: "", want: errSchemaMissing},
		"空白だけ":                  {data: " \n", want: errSchemaMissing},
		"JSON として読めない":          {data: "{", want: errSchemaMalformed},
		"JSON の後ろに余計な出力":        {data: rawJSON(awsProvider, resourceX) + "x", want: errSchemaMalformed},
		"format_version が無い":    {data: `{"provider_schemas": {}}`, want: errSchemaMalformed},
		"format_version が 2.":   {data: `{"format_version": "2.0", "provider_schemas": {}}`, want: errSchemaMalformed},
		"format_version が 1":    {data: `{"format_version": "1", "provider_schemas": {}}`, want: errSchemaMalformed},
		"format_version が 10.0": {data: `{"format_version": "10.0", "provider_schemas": {}}`, want: errSchemaMalformed},
		"format_version が数":     {data: `{"format_version": 1.0, "provider_schemas": {}}`, want: errSchemaMalformed},
		"provider_schemas が無い":  {data: `{"format_version": "1.0"}`, want: errSchemaMalformed},
		"指定の Provider が無い":      {data: rawJSON("registry.terraform.io/hashicorp/random", resourceX), want: errSchemaMalformed},
		"resource_schemas が0件":  {data: rawJSON(awsProvider), want: errNoResources},
		"resource_schemas が無い":  {data: fmt.Sprintf(`{"format_version": "1.0", "provider_schemas": {%q: {}}}`, awsProvider), want: errNoResources},
		"属性の型が違う":               {data: rawJSON(awsProvider, `{"block": {"attributes": {"a": {"optional": "yes"}}}}`), want: errSchemaMalformed},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := decodeSchema("raw.json", []byte(tt.data), awsProvider)
			require.ErrorIs(t, err, tt.want)
		})
	}
}

func Test_readVersions(t *testing.T) {
	t.Parallel()

	t.Run("並びのまま返す", func(t *testing.T) {
		t.Parallel()
		got, err := readVersions(writeFile(t, t.TempDir(), versionsName, "6.2.0\n5.9.0\n6.0.0-beta.1\n"))
		require.NoError(t, err)
		assert.Equal(t, []string{"6.2.0", "5.9.0", "6.0.0-beta.1"}, got)
	})

	tests := map[string]struct {
		body string
		want error
	}{
		"0件":      {body: "", want: errNoVersions},
		"空行":      {body: "6.2.0\n\n", want: errVersionList},
		"版の形でない行": {body: "6.2.0\nlatest\n", want: errVersionList},
		"パスを含む行":  {body: "../6.2.0\n", want: errVersionList},
		"重複した版":   {body: "6.2.0\n6.2.0\n", want: errVersionList},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := readVersions(writeFile(t, t.TempDir(), versionsName, tt.body))
			require.ErrorIs(t, err, tt.want)
		})
	}

	t.Run("一覧が無ければ errVersionList", func(t *testing.T) {
		t.Parallel()
		_, err := readVersions(filepath.Join(t.TempDir(), versionsName))
		require.ErrorIs(t, err, errVersionList)
	})
}

func Test_union(t *testing.T) {
	t.Parallel()

	t.Run("型ごとに3つの配列それぞれの和を重複なしの昇順で返す", func(t *testing.T) {
		t.Parallel()
		got := union([]map[string]entry{
			{
				"aws_a": {Arguments: []string{"b", "old"}, Deprecated: []string{"old"}, Blocks: []string{"b"}},
				"aws_b": {Arguments: []string{"x"}, Deprecated: []string{}, Blocks: []string{}},
			},
			{
				"aws_a": {Arguments: []string{"a", "b"}, Deprecated: []string{}, Blocks: []string{"a", "b"}},
				"aws_c": {Arguments: []string{}, Deprecated: []string{}, Blocks: []string{}},
			},
		})
		assert.Equal(t, map[string]entry{
			"aws_a": {Arguments: []string{"a", "b", "old"}, Deprecated: []string{"old"}, Blocks: []string{"a", "b"}},
			"aws_b": {Arguments: []string{"x"}, Deprecated: []string{}, Blocks: []string{}},
			"aws_c": {Arguments: []string{}, Deprecated: []string{}, Blocks: []string{}},
		}, got)
	})

	t.Run("ある版だけで非推奨の引数も、和の非推奨に含める", func(t *testing.T) {
		t.Parallel()
		got := union([]map[string]entry{
			{"aws_a": {Arguments: []string{"old"}, Deprecated: []string{"old"}, Blocks: []string{}}},
			{"aws_a": {Arguments: []string{"old"}, Deprecated: []string{}, Blocks: []string{}}},
		})
		assert.Equal(t, map[string]entry{
			"aws_a": {Arguments: []string{"old"}, Deprecated: []string{"old"}, Blocks: []string{}},
		}, got)
	})

	t.Run("版が無ければ空", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, union(nil))
	})
}

func Test_merge(t *testing.T) {
	t.Parallel()

	// setup は版ごとの raw.json を置き、versions.txt に版を並べる。
	setup := func(t *testing.T, raws map[string]string, versions string) (dir, out string) {
		t.Helper()
		base := t.TempDir()
		dir = filepath.Join(base, "schema")
		for v, raw := range raws {
			writeFile(t, dir, v+"/raw.json", raw)
		}
		writeFile(t, dir, versionsName, versions)

		return dir, filepath.Join(base, "out", "schema.json")
	}

	t.Run("版の和を layer_schema として書き、版ごとの型の数を報告する", func(t *testing.T) {
		t.Parallel()
		older := `{"block": {"attributes": {"name": {"required": true}, "legacy_name": {"optional": true, "deprecated": true}}}}`
		dir, out := setup(t, map[string]string{
			"6.2.0": rawJSON(awsProvider, resourceX, `{"block": {}}`),
			"5.9.0": rawJSON(awsProvider, older),
		}, "6.2.0\n5.9.0\n")
		require.NoError(t, os.MkdirAll(filepath.Dir(out), 0o750))
		var stderr bytes.Buffer
		require.NoError(t, merge([]string{"-provider", awsProvider, "-dir", dir, "-out", out}, &stderr))

		assert.JSONEq(t, `{"layer_schema": {
			"aws_0": {"arguments": ["legacy", "legacy_name", "name", "old", "rule", "tags"], "deprecated": ["legacy", "legacy_name", "old"], "blocks": ["legacy", "rule"]},
			"aws_1": {"arguments": [], "deprecated": [], "blocks": []}
		}}`, readFile(t, out))
		assert.Equal(t, "provider-schema merge: "+awsProvider+" の版ごとの型の数（6.2.0: 2 型、5.9.0: 1 型）、和 2 型を "+out+" へ書きました\n", stderr.String())
	})

	t.Run("引数が足りなければ errUsage", func(t *testing.T) {
		t.Parallel()
		for name, args := range map[string][]string{
			"-provider が無い":   {"-dir", "d", "-out", "o"},
			"-dir が無い":        {"-provider", awsProvider, "-out", "o"},
			"-out が無い":        {"-provider", awsProvider, "-dir", "d"},
			"余計な引数":           {"-provider", awsProvider, "-dir", "d", "-out", "o", "x"},
			"-provider の形が違う": {"-provider", "aws", "-dir", "d", "-out", "o"},
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				require.ErrorIs(t, merge(args, &bytes.Buffer{}), errUsage)
			})
		}
	})

	t.Run("失敗した実行は出力を書かない", func(t *testing.T) {
		t.Parallel()

		good := rawJSON(awsProvider, resourceX)
		tests := map[string]struct {
			raws     map[string]string
			versions string
			want     error
		}{
			"版の一覧が0件":          {raws: map[string]string{"6.2.0": good}, versions: "", want: errNoVersions},
			"版の一覧に版の形でない行":     {raws: map[string]string{"6.2.0": good}, versions: "6.2.0\nx\n", want: errVersionList},
			"raw.json が無い":     {raws: map[string]string{"6.2.0": good}, versions: "6.2.0\n5.9.0\n", want: errSchemaMissing},
			"raw.json が空":      {raws: map[string]string{"6.2.0": good, "5.9.0": ""}, versions: "6.2.0\n5.9.0\n", want: errSchemaMissing},
			"raw.json を読めない":   {raws: map[string]string{"6.2.0": good, "5.9.0": "{"}, versions: "6.2.0\n5.9.0\n", want: errSchemaMalformed},
			"resource の型が0件の版": {raws: map[string]string{"6.2.0": good, "5.9.0": rawJSON(awsProvider)}, versions: "6.2.0\n5.9.0\n", want: errNoResources},
		}
		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				dir, out := setup(t, tt.raws, tt.versions)
				err := merge([]string{"-provider", awsProvider, "-dir", dir, "-out", out}, &bytes.Buffer{})
				require.ErrorIs(t, err, tt.want)
				assert.NoFileExists(t, out)
			})
		}
	})

	t.Run("失敗した実行は既存の出力を変えない", func(t *testing.T) {
		t.Parallel()
		dir, out := setup(t, map[string]string{"6.2.0": rawJSON(awsProvider, resourceX), "5.9.0": "{"}, "6.2.0\n5.9.0\n")
		writeFile(t, filepath.Dir(out), filepath.Base(out), "previous\n")
		err := merge([]string{"-provider", awsProvider, "-dir", dir, "-out", out}, &bytes.Buffer{})
		require.ErrorIs(t, err, errSchemaMalformed)
		assert.Equal(t, "previous\n", readFile(t, out))
	})

	t.Run("書き込めなければ失敗し、出力を残さない", func(t *testing.T) {
		t.Parallel()
		testenv.RequireNonRoot(t, "特権実行では読み取り専用ディレクトリへも書けるため検証できない")
		dir, out := setup(t, map[string]string{"6.2.0": rawJSON(awsProvider, resourceX)}, "6.2.0\n")
		parent := filepath.Dir(out)
		require.NoError(t, os.MkdirAll(parent, 0o750))
		require.NoError(t, os.Chmod(parent, 0o500))
		t.Cleanup(func() { _ = os.Chmod(parent, 0o750) })
		err := merge([]string{"-provider", awsProvider, "-dir", dir, "-out", out}, &bytes.Buffer{})
		require.ErrorIs(t, err, fs.ErrPermission)
		assert.NoFileExists(t, out)
	})
}

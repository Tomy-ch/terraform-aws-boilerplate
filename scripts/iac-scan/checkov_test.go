package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

const (
	checkovCanaryJSON = `{"check_type":"terraform","results":{"failed_checks":[{"check_id":"CKV2_AWS_65"}]},"summary":{"resource_count":3,"parsing_errors":0}}`
	checkovCleanJSON  = `{"check_type":"terraform","results":{"failed_checks":[]},"summary":{"resource_count":4,"parsing_errors":0}}`
	checkovSARIF      = `{"runs":[{"results":[{"locations":[{"physicalLocation":{"artifactLocation":{"uri":"/work/src/env/c/main.tf"}}}]},{"suppressions":[{"kind":"inSource"}]}]}]}`
)

func Test_runCheckov(t *testing.T) {
	t.Parallel()

	setup := func(t *testing.T, mainRC int, mainJSON string) (*fake, runner, string) {
		t.Helper()
		f, tool := newFake(t)
		f.scenario("checkov-canary", 1, "", map[string]string{"results_json.json": checkovCanaryJSON})
		f.scenario("checkov", mainRC, "", map[string]string{"results_json.json": mainJSON, "results_sarif.sarif": checkovSARIF})
		return f, tool, tfTree(t)
	}
	args := func(base string) []string { return []string{"-base", base, "usecases", "modules", "env"} }

	t.Run("canary が通れば本走査を起動し、所見が無ければ成功する", func(t *testing.T) {
		t.Parallel()
		f, tool, base := setup(t, 0, checkovCleanJSON)
		var log bytes.Buffer
		require.NoError(t, runCheckov(args(base), &log, tool))
		want := append([]string{"iac-scan image-checkov"}, cycle("checkov-canary")...)
		assert.Equal(t, append(want, cycle("checkov")...), f.calls())
		assert.Contains(t, log.String(), "✅ checkov の canary")
		assert.Contains(t, log.String(), "checkov: resource 4 件を走査しました（終了コード 0）")
		assert.NotContains(t, log.String(), "up-stdout checkov-canary", "canary の出力は通ったときに出さない")
		assert.Contains(t, log.String(), "up-stdout checkov", "本走査の出力は log へ流す")
		assert.NoDirExists(t, filepath.Join(base, "tmp", "checkov-canary"))
		assert.FileExists(t, filepath.Join(base, "tmp", "checkov", "work", "src", "env", "c", "main.tf"))
	})

	t.Run("canary と本走査は同じ定義で起動する", func(t *testing.T) {
		t.Parallel()
		f, tool, base := setup(t, 0, checkovCleanJSON)
		require.NoError(t, runCheckov(args(base), &bytes.Buffer{}, tool))
		canary, main := serviceOf(t, f.composeOf("checkov-canary")), serviceOf(t, f.composeOf("checkov"))
		assert.Equal(t, main, canary)
		assert.Equal(t, composeServiceSpec{
			Image:       testImage,
			NetworkMode: "none",
			CapDrop:     []string{"ALL"},
			CapAdd:      []string{"DAC_READ_SEARCH"},
			SecurityOpt: []string{"no-new-privileges:true"},
			PidsLimit:   1024,
			Command: []string{
				"-d", "/work/src", "--framework", "terraform", "--skip-download", "--compact", "--quiet",
				"-o", "cli", "-o", "json", "-o", "sarif",
				"--output-file-path", "console,/tmp/out/results_json.json,/tmp/out/results_sarif.sarif",
			},
		}, main)
	})

	t.Run("本走査の SARIF を正規化する", func(t *testing.T) {
		t.Parallel()
		_, tool, base := setup(t, 0, checkovCleanJSON)
		require.NoError(t, runCheckov(args(base), &bytes.Buffer{}, tool))
		raw, err := os.ReadFile(filepath.Join(base, "tmp", "checkov", "out", "results_sarif.sarif"))
		require.NoError(t, err)
		assert.JSONEq(t, `{"runs":[{"results":[{"locations":[{"physicalLocation":{"artifactLocation":{"uri":"env/c/main.tf"}}}]}]}]}`, string(raw))
	})

	t.Run("所見があれば errFindings", func(t *testing.T) {
		t.Parallel()
		_, tool, base := setup(t, 1, checkovCleanJSON)
		var log bytes.Buffer
		require.ErrorIs(t, runCheckov(args(base), &log, tool), errFindings)
		assert.Contains(t, log.String(), "（終了コード 1）")
	})

	t.Run("canary が落ちなければ本走査を起動せずに errCanary", func(t *testing.T) {
		t.Parallel()
		f, tool, base := setup(t, 0, checkovCleanJSON)
		f.scenario("checkov-canary", 0, "", map[string]string{"results_json.json": checkovCleanJSON})
		var log bytes.Buffer
		require.ErrorIs(t, runCheckov(args(base), &log, tool), errCanary)
		assert.Equal(t, append([]string{"iac-scan image-checkov"}, cycle("checkov-canary")...), f.calls())
		assert.Contains(t, log.String(), "--- checkov の canary の出力:")
	})

	t.Run("canary の docker が落ちれば本走査を起動しない", func(t *testing.T) {
		t.Parallel()
		f, tool, base := setup(t, 0, checkovCleanJSON)
		f.fail(stepCreate)
		require.ErrorIs(t, runCheckov(args(base), &bytes.Buffer{}, tool), errDocker)
		assert.Equal(t, []string{"iac-scan image-checkov", "checkov-canary create", "checkov-canary down"}, f.calls())
	})

	for name, c := range map[string]struct {
		json string
		want error
	}{
		"resource が0件": {`{"summary":{"resource_count":0,"parsing_errors":0}}`, errNoTargets},
		"解釈できなかったファイルがある": {`{"summary":{"resource_count":3,"parsing_errors":1}}`, errCoverage},
		"summary が無い":     {`{"results":{}}`, errResult},
		"結果が JSON でない":    {`not json`, errResult},
	} {
		t.Run(name+"なら、所見の有無に依らず失敗する", func(t *testing.T) {
			t.Parallel()
			_, tool, base := setup(t, 0, c.json)
			require.ErrorIs(t, runCheckov(args(base), &bytes.Buffer{}, tool), c.want)
		})
	}

	t.Run("本走査の docker が落ちれば errDocker", func(t *testing.T) {
		t.Parallel()
		f, tool, base := setup(t, 0, checkovCleanJSON)
		f.failIn(stepCreate, "checkov")
		require.ErrorIs(t, runCheckov(args(base), &bytes.Buffer{}, tool), errDocker)
		calls := f.calls()
		assert.Equal(t, []string{"checkov create", "checkov down"}, calls[len(calls)-2:])
	})

	t.Run("本走査の SARIF が壊れていれば errResult", func(t *testing.T) {
		t.Parallel()
		f, tool, base := setup(t, 0, checkovCleanJSON)
		f.write("scenario/checkov/out/results_sarif.sarif", "not json")
		require.ErrorIs(t, runCheckov(args(base), &bytes.Buffer{}, tool), errResult)
	})

	t.Run("本走査の SARIF が無ければ errResult", func(t *testing.T) {
		t.Parallel()
		f, tool, base := setup(t, 0, checkovCleanJSON)
		require.NoError(t, os.Remove(filepath.Join(f.dir, "scenario", "checkov", "out", "results_sarif.sarif")))
		require.ErrorIs(t, runCheckov(args(base), &bytes.Buffer{}, tool), errResult)
	})

	t.Run("結果が無ければ errResult", func(t *testing.T) {
		t.Parallel()
		f, tool, base := setup(t, 0, checkovCleanJSON)
		require.NoError(t, os.RemoveAll(filepath.Join(f.dir, "scenario", "checkov", "out")))
		require.ErrorIs(t, runCheckov(args(base), &bytes.Buffer{}, tool), errResult)
	})

	t.Run("結果を取り出せなければ、道具が 0 でも errOutput", func(t *testing.T) {
		t.Parallel()
		f, tool, base := setup(t, 0, checkovCleanJSON)
		f.failIn(stepCopyOut, "checkov")
		require.ErrorIs(t, runCheckov(args(base), &bytes.Buffer{}, tool), errOutput)
		calls := f.calls()
		assert.Equal(t, "checkov down", calls[len(calls)-1])
	})

	t.Run("root が無ければ docker を起動せずに errMissingRoot", func(t *testing.T) {
		t.Parallel()
		f, tool, base := setup(t, 0, checkovCleanJSON)
		require.ErrorIs(t, runCheckov([]string{"-base", base, "usecases", "nothing"}, &bytes.Buffer{}, tool), errMissingRoot)
		assert.Empty(t, f.calls())
	})

	t.Run("イメージを決められなければ canary を起動しない", func(t *testing.T) {
		t.Parallel()
		f, tool, base := setup(t, 0, checkovCleanJSON)
		f.write("image", "")
		require.ErrorIs(t, runCheckov(args(base), &bytes.Buffer{}, tool), errImage)
		assert.Equal(t, []string{"iac-scan image-checkov"}, f.calls())
	})

	t.Run("root が無い引数は errUsage", func(t *testing.T) {
		t.Parallel()
		f, tool, base := setup(t, 0, checkovCleanJSON)
		require.ErrorIs(t, runCheckov([]string{"-base", base}, &bytes.Buffer{}, tool), errUsage)
		assert.Empty(t, f.calls())
	})
}

func serviceOf(t *testing.T, raw string) composeServiceSpec {
	t.Helper()
	var c composeFile
	require.NoError(t, yaml.Unmarshal([]byte(raw), &c))
	require.Len(t, c.Services, 1)
	return c.Services["scan"]
}

func Test_checkovReports(t *testing.T) {
	t.Parallel()

	t.Run("object は1件の報告", func(t *testing.T) {
		t.Parallel()
		dir := tree(t, map[string]string{"r.json": `{"check_type":"terraform"}`})
		got, err := checkovReports(filepath.Join(dir, "r.json"))
		require.NoError(t, err)
		assert.Len(t, got, 1)
	})

	t.Run("array は要素ごとの報告", func(t *testing.T) {
		t.Parallel()
		dir := tree(t, map[string]string{"r.json": `[{"check_type":"terraform"},{"check_type":"secrets"}]`})
		got, err := checkovReports(filepath.Join(dir, "r.json"))
		require.NoError(t, err)
		assert.Len(t, got, 2)
	})

	for name, in := range map[string]string{
		"空の array":     `[]`,
		"object でない要素": `[1]`,
		"数":            `3`,
		"JSON でない":     `x`,
	} {
		t.Run(name+"は errResult", func(t *testing.T) {
			t.Parallel()
			dir := tree(t, map[string]string{"r.json": in})
			_, err := checkovReports(filepath.Join(dir, "r.json"))
			require.ErrorIs(t, err, errResult)
		})
	}
}

func Test_checkovSummary(t *testing.T) {
	t.Parallel()

	t.Run("object の summary を読む", func(t *testing.T) {
		t.Parallel()
		dir := tree(t, map[string]string{"r.json": `{"summary":{"resource_count":5,"parsing_errors":2}}`})
		n, e, err := checkovSummary(filepath.Join(dir, "r.json"))
		require.NoError(t, err)
		assert.Equal(t, [2]int64{5, 2}, [2]int64{n, e})
	})

	t.Run("array の summary を足す", func(t *testing.T) {
		t.Parallel()
		dir := tree(t, map[string]string{"r.json": `[{"summary":{"resource_count":5,"parsing_errors":0}},{"summary":{"resource_count":2,"parsing_errors":1}}]`})
		n, e, err := checkovSummary(filepath.Join(dir, "r.json"))
		require.NoError(t, err)
		assert.Equal(t, [2]int64{7, 1}, [2]int64{n, e})
	})

	for name, in := range map[string]string{
		"summary が無い":           `{}`,
		"summary が object でない":  `{"summary":1}`,
		"resource_count が無い":    `{"summary":{"parsing_errors":0}}`,
		"resource_count が文字列":   `{"summary":{"resource_count":"5","parsing_errors":0}}`,
		"parsing_errors が無い":    `{"summary":{"resource_count":5}}`,
		"parsing_errors が null": `{"summary":{"resource_count":5,"parsing_errors":null}}`,
		"array の1つだけ欄が無い":       `[{"summary":{"resource_count":5,"parsing_errors":0}},{"summary":{}}]`,
	} {
		t.Run(name+"は errResult", func(t *testing.T) {
			t.Parallel()
			dir := tree(t, map[string]string{"r.json": in})
			_, _, err := checkovSummary(filepath.Join(dir, "r.json"))
			require.ErrorIs(t, err, errResult)
		})
	}
}

func Test_judgeCheckovCanary(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		rc   int
		json string
		ok   bool
	}{
		"非0で CKV2_AWS_65 を報告すれば通る":        {1, checkovCanaryJSON, true},
		"array の2つ目に CKV2_AWS_65 があっても通る": {1, `[{"results":{"failed_checks":[]}},{"results":{"failed_checks":[{"check_id":"CKV2_AWS_65"}]}}]`, true},
		"0 で終われば落ちる":                      {0, checkovCanaryJSON, false},
		"CKV2_AWS_65 が無ければ落ちる":            {1, `{"results":{"failed_checks":[{"check_id":"CKV_AWS_1"}]}}`, false},
		"CKV2_AWS_65 が通った検査にあるだけなら落ちる":    {1, `{"results":{"passed_checks":[{"check_id":"CKV2_AWS_65"}],"failed_checks":[]}}`, false},
		"結果が JSON でなければ落ちる":               {1, `x`, false},
		"failed_checks が無ければ落ちる":          {1, `{"results":{}}`, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := tree(t, map[string]string{"r.json": c.json})
			err := judgeCheckovCanary(c.rc, filepath.Join(dir, "r.json"))
			if c.ok {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, errCanary)
		})
	}

	t.Run("結果が無ければ落ちる", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, judgeCheckovCanary(1, filepath.Join(t.TempDir(), "none.json")), errCanary)
	})
}

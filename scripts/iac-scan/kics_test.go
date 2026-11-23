package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	kicsCanaryJSON    = `{"files_scanned":1,"files_parsed":1,"queries_failed_to_execute":0,"queries":[{"query_id":"d0cc8694-fcad-43ff-ac86-32331d7e867f"}]}`
	kicsThresholdJSON = `{"files_scanned":1,"files_parsed":1,"queries_failed_to_execute":0,"queries":[{"query_id":"e38a8e0a-b88b-4902-b3fe-b0fcb17d5c10"}]}`
	kicsCleanJSON     = `{"files_scanned":3,"files_parsed":3,"queries_failed_to_execute":0,"queries":[]}`
	kicsAll           = "critical,high,medium,low,info"
)

func Test_kicsTool(t *testing.T) {
	t.Parallel()

	got := kicsTool("critical,high")
	assert.Equal(t, imageTool{
		imageStep: stepImageKics,
		command: []string{
			"scan", "-p", "/work/src", "--type", "Terraform", "--disable-secrets", "--no-progress", "--ci",
			"-o", "/tmp/out", "--report-formats", "json,sarif", "--fail-on", "critical,high",
		},
		environment: map[string]string{"DISABLE_CRASH_REPORT": "0"},
	}, got)
}

func Test_runKics(t *testing.T) {
	t.Parallel()

	setup := func(t *testing.T, mainRC int, mainJSON string) (*fake, toolFunc, string) {
		t.Helper()
		f, tool := newFake(t)
		f.scenario("kics-canary", 50, "", map[string]string{"results.json": kicsCanaryJSON})
		f.scenario("kics-canary-threshold", 20, "", map[string]string{"results.json": kicsThresholdJSON})
		f.scenario("kics", mainRC, "", map[string]string{"results.json": mainJSON, "results.sarif": `{"runs":[{"results":[]}]}`})
		return f, tool, tfTree(t)
	}
	args := func(base, failOn string) []string {
		return []string{"-base", base, "-fail-on", failOn, "usecases", "modules", "env"}
	}

	t.Run("2つの canary が通れば本走査を起動し、所見が無ければ成功する", func(t *testing.T) {
		t.Parallel()
		f, tool, base := setup(t, 0, kicsCleanJSON)
		var log bytes.Buffer
		require.NoError(t, runKics(args(base, kicsAll), &log, tool))
		want := []string{"iac-scan image-kics"}
		want = append(want, cycle("kics-canary")...)
		want = append(want, cycle("kics-canary-threshold")...)
		assert.Equal(t, append(want, cycle("kics")...), f.calls())
		assert.Contains(t, log.String(), "kics: ファイル 3 件を走査しました（終了コード 0）")
		assert.NotContains(t, log.String(), "up-stdout kics-canary")
	})

	t.Run("canary と本走査は同じ定義で起動し、閾値を --fail-on へ写す", func(t *testing.T) {
		t.Parallel()
		f, tool, base := setup(t, 0, kicsCleanJSON)
		require.NoError(t, runKics(args(base, kicsAll), &bytes.Buffer{}, tool))
		main := serviceOf(t, f.composeOf("kics"))
		assert.Equal(t, main, serviceOf(t, f.composeOf("kics-canary")))
		assert.Equal(t, main, serviceOf(t, f.composeOf("kics-canary-threshold")))
		assert.Equal(t, kicsTool(kicsAll).command, main.Command)
		assert.Equal(t, map[string]string{"DISABLE_CRASH_REPORT": "0"}, main.Environment)
		assert.Equal(t, "none", main.NetworkMode)
	})

	t.Run("所見があれば errFindings", func(t *testing.T) {
		t.Parallel()
		_, tool, base := setup(t, 40, kicsCleanJSON)
		require.ErrorIs(t, runKics(args(base, kicsAll), &bytes.Buffer{}, tool), errFindings)
	})

	t.Run("canary が落ちなければ本走査を起動せずに errCanary", func(t *testing.T) {
		t.Parallel()
		f, tool, base := setup(t, 0, kicsCleanJSON)
		f.scenario("kics-canary", 0, "", map[string]string{"results.json": kicsCleanJSON})
		require.ErrorIs(t, runKics(args(base, kicsAll), &bytes.Buffer{}, tool), errCanary)
		assert.NotContains(t, f.calls(), "kics up")
		assert.NotContains(t, f.calls(), "kics-canary-threshold up")
	})

	t.Run("閾値の canary が 20 で落ちなければ本走査を起動せずに errCanary", func(t *testing.T) {
		t.Parallel()
		f, tool, base := setup(t, 0, kicsCleanJSON)
		f.scenario("kics-canary-threshold", 0, "", map[string]string{"results.json": kicsThresholdJSON})
		require.ErrorIs(t, runKics(args(base, kicsAll), &bytes.Buffer{}, tool), errCanary)
		assert.NotContains(t, f.calls(), "kics up")
	})

	for name, c := range map[string]struct {
		json string
		want error
	}{
		"走査が0件":           {`{"files_scanned":0,"files_parsed":0,"queries_failed_to_execute":0}`, errNoTargets},
		"写した .tf に届かない":   {`{"files_scanned":2,"files_parsed":2,"queries_failed_to_execute":0}`, errCoverage},
		"解釈できなかったファイルがある": {`{"files_scanned":3,"files_parsed":2,"queries_failed_to_execute":0}`, errCoverage},
		"欄が無い":            {`{"files_scanned":3}`, errResult},
	} {
		t.Run(name+"なら、所見の有無に依らず失敗する", func(t *testing.T) {
			t.Parallel()
			_, tool, base := setup(t, 0, c.json)
			require.ErrorIs(t, runKics(args(base, kicsAll), &bytes.Buffer{}, tool), c.want)
		})
	}

	t.Run("本走査の SARIF を正規化する", func(t *testing.T) {
		t.Parallel()
		f, tool, base := setup(t, 0, kicsCleanJSON)
		f.write("scenario/kics/out/results.sarif", `{"runs":[{"results":[{"locations":[{"physicalLocation":{"artifactLocation":{"uri":"../../work/src/env/c/main.tf"}}}]}]}]}`)
		require.NoError(t, runKics(args(base, kicsAll), &bytes.Buffer{}, tool))
		raw, err := os.ReadFile(filepath.Join(base, "tmp", "kics", "out", "results.sarif"))
		require.NoError(t, err)
		assert.Contains(t, string(raw), `"uri": "env/c/main.tf"`)
	})

	for _, failOn := range []string{"", "info", "high,critical", "critical,high,medium,low,info,trace", "CRITICAL"} {
		t.Run("一覧に無い閾値 "+failOn+" は docker を起動せずに errUsage", func(t *testing.T) {
			t.Parallel()
			f, tool, base := setup(t, 0, kicsCleanJSON)
			require.ErrorIs(t, runKics(args(base, failOn), &bytes.Buffer{}, tool), errUsage)
			assert.Empty(t, f.calls())
		})
	}

	t.Run("一覧のどの閾値も受ける", func(t *testing.T) {
		t.Parallel()
		for _, failOn := range kicsFailOn {
			_, tool, base := setup(t, 0, kicsCleanJSON)
			require.NoError(t, runKics(args(base, failOn), &bytes.Buffer{}, tool), failOn)
		}
	})

	t.Run("root が無ければ docker を起動せずに errMissingRoot", func(t *testing.T) {
		t.Parallel()
		f, tool, base := setup(t, 0, kicsCleanJSON)
		require.ErrorIs(t, runKics([]string{"-base", base, "-fail-on", kicsAll, "env", "nothing"}, &bytes.Buffer{}, tool), errMissingRoot)
		assert.Empty(t, f.calls())
	})
}

func Test_judgeKics(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		json string
		want int
		err  error
	}{
		"走査が写した数と等しければ通る":    {`{"files_scanned":3,"files_parsed":3,"queries_failed_to_execute":0}`, 3, nil},
		"走査が写した数より多くても通る":    {`{"files_scanned":4,"files_parsed":4,"queries_failed_to_execute":0}`, 3, nil},
		"走査が写した数の1つ手前なら落ちる":  {`{"files_scanned":2,"files_parsed":2,"queries_failed_to_execute":0}`, 3, errCoverage},
		"0件は errNoTargets":   {`{"files_scanned":0,"files_parsed":0,"queries_failed_to_execute":0}`, 0, errNoTargets},
		"解釈できなかったファイル":       {`{"files_scanned":3,"files_parsed":2,"queries_failed_to_execute":0}`, 3, errCoverage},
		"実行できなかった query":     {`{"files_scanned":3,"files_parsed":3,"queries_failed_to_execute":1}`, 3, errCoverage},
		"解釈が走査より多い":          {`{"files_scanned":3,"files_parsed":4,"queries_failed_to_execute":0}`, 3, errCoverage},
		"files_scanned が無い":  {`{"files_parsed":3,"queries_failed_to_execute":0}`, 3, errResult},
		"files_parsed が文字列":  {`{"files_scanned":3,"files_parsed":"3","queries_failed_to_execute":0}`, 3, errResult},
		"queries_failed が無い": {`{"files_scanned":3,"files_parsed":3}`, 3, errResult},
		"object でない":         {`[]`, 3, errResult},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := tree(t, map[string]string{"r.json": c.json})
			n, err := judgeKics(filepath.Join(dir, "r.json"), c.want)
			if c.err != nil {
				require.ErrorIs(t, err, c.err)
				return
			}
			require.NoError(t, err)
			assert.Positive(t, n)
		})
	}
}

func Test_judgeKicsCanary(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		rc     int
		json   string
		id     string
		wantRC int
		ok     bool
	}{
		"非0で id を報告すれば通る":         {50, kicsCanaryJSON, kicsCanaryID, 0, true},
		"0 で終われば落ちる":              {0, kicsCanaryJSON, kicsCanaryID, 0, false},
		"id が無ければ落ちる":             {50, kicsThresholdJSON, kicsCanaryID, 0, false},
		"queries が無ければ落ちる":        {50, `{}`, kicsCanaryID, 0, false},
		"JSON でなければ落ちる":           {50, `x`, kicsCanaryID, 0, false},
		"閾値の canary は 20 で通る":     {20, kicsThresholdJSON, kicsThresholdID, kicsThresholdRC, true},
		"閾値の canary が 30 なら落ちる":   {30, kicsThresholdJSON, kicsThresholdID, kicsThresholdRC, false},
		"閾値の canary が 0 なら落ちる":    {0, kicsThresholdJSON, kicsThresholdID, kicsThresholdRC, false},
		"閾値の canary が id を欠けば落ちる": {20, kicsCanaryJSON, kicsThresholdID, kicsThresholdRC, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := tree(t, map[string]string{"r.json": c.json})
			err := judgeKicsCanary(c.rc, filepath.Join(dir, "r.json"), c.id, c.wantRC)
			if c.ok {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, errCanary)
		})
	}
}

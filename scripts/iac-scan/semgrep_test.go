package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	semgrepCanaryJSON = `{"results":[{"check_id":"terraform.lang.security.s3-public-rw-bucket"}],"paths":{"scanned":["/work/src/main.tf"]}}`
	semgrepCleanJSON  = `{"results":[],"paths":{"scanned":["a","b","c","d"]}}`
	semgrepSARIF      = `{"runs":[{"results":[{"locations":[{"physicalLocation":{"artifactLocation":{"uri":"/work/src/env/c/main.tf"}}}]},{"suppressions":[{"kind":"inSource"}]}]}]}`
)

func semgrepArgs(base, rules, sha string, roots ...string) []string {
	if len(roots) == 0 {
		roots = []string{"usecases", "modules", "env"}
	}
	return append([]string{"-base", base, "-rules", rules, "-rules-sha256", sha}, roots...)
}

func Test_runSemgrep(t *testing.T) {
	t.Parallel()

	setup := func(t *testing.T, mainRC int, mainJSON string) (*fake, runner, string, []string) {
		t.Helper()
		f, tool := newFake(t)
		f.scenario("semgrep-canary", 1, "", map[string]string{"results.json": semgrepCanaryJSON})
		f.scenario("semgrep", mainRC, "", map[string]string{"results.json": mainJSON, "results.sarif": semgrepSARIF})
		base := tfTree(t)
		return f, tool, base, semgrepArgs(base, tarball(t, rulesMembers), rulesSHA256)
	}

	t.Run("canary が通れば本走査を起動し、所見が無ければ成功する", func(t *testing.T) {
		t.Parallel()
		f, tool, base, args := setup(t, 0, semgrepCleanJSON)
		var log bytes.Buffer
		require.NoError(t, runSemgrep(args, &log, tool))
		want := append([]string{"iac-scan image-semgrep"}, cycle("semgrep-canary")...)
		assert.Equal(t, append(want, cycle("semgrep")...), f.calls())
		assert.Contains(t, log.String(), "semgrep: ファイル 4 件を走査しました（終了コード 0）")
		work := filepath.Join(base, "tmp", "semgrep", "work")
		assert.FileExists(t, filepath.Join(work, "cfg", "terraform", "aws", "a.yaml"))
		assert.NoFileExists(t, filepath.Join(work, "cfg", "terraform", "aws", "a.tf"), "rule のサンプルを写さない")
		assert.FileExists(t, filepath.Join(work, "src", ".semgrepignore"))
	})

	t.Run("canary と本走査は同じ定義で起動し、canary にも rule を写す", func(t *testing.T) {
		t.Parallel()
		f, tool, _, args := setup(t, 0, semgrepCleanJSON)
		require.NoError(t, runSemgrep(args, &bytes.Buffer{}, tool))
		assert.Equal(t, []string{"./cfg/terraform/aws/B.yml", "./cfg/terraform/aws/a.yaml", "./cfg/terraform/z/c.yaml", "./src/.semgrepignore", "./src/main.tf"},
			f.workOf("semgrep-canary"))
		assert.Contains(t, f.workOf("semgrep"), "./src/usecases/a/.terraform.lock.hcl")
		main := serviceOf(t, f.composeOf("semgrep"))
		assert.Equal(t, main, serviceOf(t, f.composeOf("semgrep-canary")))
		assert.Equal(t, composeServiceSpec{
			Image:       testImage,
			NetworkMode: "none",
			CapDrop:     []string{"ALL"},
			SecurityOpt: []string{"no-new-privileges:true"},
			PidsLimit:   1024,
			Command: []string{
				"semgrep", "scan", "--config", "terraform", "--metrics", "off", "--disable-version-check", "--error", "--strict",
				"--json-output=/tmp/out/results.json", "--sarif-output=/tmp/out/results.sarif", "/work/src",
			},
			WorkingDir: "/work/cfg",
		}, main)
	})

	t.Run("所見があれば errFindings", func(t *testing.T) {
		t.Parallel()
		_, tool, _, args := setup(t, 1, semgrepCleanJSON)
		require.ErrorIs(t, runSemgrep(args, &bytes.Buffer{}, tool), errFindings)
	})

	t.Run("canary が落ちなければ本走査を起動せずに errCanary", func(t *testing.T) {
		t.Parallel()
		f, tool, _, args := setup(t, 0, semgrepCleanJSON)
		f.scenario("semgrep-canary", 0, "", map[string]string{"results.json": semgrepCleanJSON})
		require.ErrorIs(t, runSemgrep(args, &bytes.Buffer{}, tool), errCanary)
		assert.Equal(t, append([]string{"iac-scan image-semgrep"}, cycle("semgrep-canary")...), f.calls())
	})

	t.Run("本走査の SARIF を正規化する", func(t *testing.T) {
		t.Parallel()
		_, tool, base, args := setup(t, 0, semgrepCleanJSON)
		require.NoError(t, runSemgrep(args, &bytes.Buffer{}, tool))
		raw, err := os.ReadFile(filepath.Join(base, "tmp", "semgrep", "out", "results.sarif"))
		require.NoError(t, err)
		assert.JSONEq(t, `{"runs":[{"results":[{"locations":[{"physicalLocation":{"artifactLocation":{"uri":"env/c/main.tf"}}}]}]}]}`, string(raw))
	})

	t.Run("本走査の docker が落ちれば errDocker", func(t *testing.T) {
		t.Parallel()
		f, tool, _, args := setup(t, 0, semgrepCleanJSON)
		f.failIn(stepCreate, "semgrep")
		require.ErrorIs(t, runSemgrep(args, &bytes.Buffer{}, tool), errDocker)
		calls := f.calls()
		assert.Equal(t, []string{"semgrep create", "semgrep down"}, calls[len(calls)-2:])
	})

	t.Run("本走査の SARIF が壊れていれば errResult", func(t *testing.T) {
		t.Parallel()
		f, tool, _, args := setup(t, 0, semgrepCleanJSON)
		f.write("scenario/semgrep/out/results.sarif", "not json")
		require.ErrorIs(t, runSemgrep(args, &bytes.Buffer{}, tool), errResult)
	})

	for name, rel := range map[string]string{"SARIF": "results.sarif", "results.json": "results.json"} {
		t.Run("本走査の "+name+" が無ければ errResult", func(t *testing.T) {
			t.Parallel()
			f, tool, _, args := setup(t, 0, semgrepCleanJSON)
			require.NoError(t, os.Remove(filepath.Join(f.dir, "scenario", "semgrep", "out", rel)))
			require.ErrorIs(t, runSemgrep(args, &bytes.Buffer{}, tool), errResult)
		})
	}

	t.Run("本走査の results.json が JSON でなければ errResult", func(t *testing.T) {
		t.Parallel()
		_, tool, _, args := setup(t, 0, "not json")
		require.ErrorIs(t, runSemgrep(args, &bytes.Buffer{}, tool), errResult)
	})

	t.Run("走査したファイルが写した .tf と .hcl に届かなければ errCoverage", func(t *testing.T) {
		t.Parallel()
		_, tool, _, args := setup(t, 0, `{"results":[],"paths":{"scanned":["a","b","c"]}}`)
		require.ErrorIs(t, runSemgrep(args, &bytes.Buffer{}, tool), errCoverage)
	})

	t.Run("rule が宣言と違えば docker を起動せずに errInput", func(t *testing.T) {
		t.Parallel()
		f, tool, base, _ := setup(t, 0, semgrepCleanJSON)
		args := semgrepArgs(base, tarball(t, rulesMembers), strings.Repeat("0", 64))
		require.ErrorIs(t, runSemgrep(args, &bytes.Buffer{}, tool), errInput)
		assert.Empty(t, f.calls())
	})

	t.Run("イメージを決められなければ canary を起動しない", func(t *testing.T) {
		t.Parallel()
		f, tool, _, args := setup(t, 0, semgrepCleanJSON)
		f.write("image", "")
		require.ErrorIs(t, runSemgrep(args, &bytes.Buffer{}, tool), errImage)
		assert.Equal(t, []string{"iac-scan image-semgrep"}, f.calls())
	})

	t.Run("root が無ければ docker を起動せずに errMissingRoot", func(t *testing.T) {
		t.Parallel()
		f, tool, base, _ := setup(t, 0, semgrepCleanJSON)
		args := semgrepArgs(base, tarball(t, rulesMembers), rulesSHA256, "env", "nothing")
		require.ErrorIs(t, runSemgrep(args, &bytes.Buffer{}, tool), errMissingRoot)
		assert.Empty(t, f.calls())
	})

	t.Run("写したファイルが0件の root があれば docker を起動せずに errNoTargets", func(t *testing.T) {
		t.Parallel()
		f, tool, _, _ := setup(t, 0, semgrepCleanJSON)
		base := tree(t, map[string]string{"env/c/main.tf": "", "modules/b/.terraform/x.tf": ""})
		args := semgrepArgs(base, tarball(t, rulesMembers), rulesSHA256, "env", "modules")
		require.ErrorIs(t, runSemgrep(args, &bytes.Buffer{}, tool), errNoTargets)
		assert.Empty(t, f.calls())
	})

	for _, work := range []string{"semgrep", "semgrep-canary"} {
		t.Run("作業ディレクトリ tmp/"+work+" が tmp/ の外を指せば、外を消さずに errUsage", func(t *testing.T) {
			t.Parallel()
			f, tool, _, _ := setup(t, 0, semgrepCleanJSON)
			base, outside := escapingWork(t, work)
			args := semgrepArgs(base, tarball(t, rulesMembers), rulesSHA256)
			require.ErrorIs(t, runSemgrep(args, &bytes.Buffer{}, tool), errUsage)
			assert.FileExists(t, filepath.Join(outside, "keep"))
			requireLinkKept(t, base, work)
			assert.NotContains(t, f.calls(), work+" create")
		})
	}

	t.Run("-rules か -rules-sha256 が無ければ errUsage", func(t *testing.T) {
		t.Parallel()
		f, tool, base, _ := setup(t, 0, semgrepCleanJSON)
		require.ErrorIs(t, runSemgrep([]string{"-base", base, "-rules", "x", "env"}, &bytes.Buffer{}, tool), errUsage)
		require.ErrorIs(t, runSemgrep([]string{"-base", base, "-rules-sha256", "x", "env"}, &bytes.Buffer{}, tool), errUsage)
		assert.Empty(t, f.calls())
	})
}

func Test_stageSemgrepWork(t *testing.T) {
	t.Parallel()

	t.Run("rule を cfg へ写し、src に空の .semgrepignore を置く", func(t *testing.T) {
		t.Parallel()
		dir := workDir(t, "semgrep")
		require.NoError(t, stageSemgrepWork(dir, tarball(t, rulesMembers), rulesSHA256))
		assert.ElementsMatch(t, []string{"terraform/aws/a.yaml", "terraform/aws/B.yml", "terraform/z/c.yaml"}, files(t, filepath.Join(dir, "work", "cfg")))
		assert.Equal(t, []string{".semgrepignore"}, files(t, filepath.Join(dir, "work", "src")))
	})

	t.Run("rule が合わなければ .semgrepignore を置かない", func(t *testing.T) {
		t.Parallel()
		dir := workDir(t, "semgrep")
		require.ErrorIs(t, stageSemgrepWork(dir, tarball(t, rulesMembers), "x"), errInput)
		assert.NoFileExists(t, filepath.Join(dir, "work", "src", ".semgrepignore"))
	})
}

func Test_judgeSemgrep(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		json string
		want int
		err  error
	}{
		"写した数と等しければ通る":      {`{"paths":{"scanned":["a","b"]}}`, 2, nil},
		"写した数の1つ手前なら落ちる":    {`{"paths":{"scanned":["a"]}}`, 2, errCoverage},
		"0件は errNoTargets":  {`{"paths":{"scanned":[]}}`, 0, errNoTargets},
		"scanned が数なら読めない":  {`{"paths":{"scanned":2}}`, 2, errResult},
		"scanned が無ければ読めない": {`{"paths":{}}`, 2, errResult},
		"paths が無ければ読めない":   {`{"results":[]}`, 2, errResult},
		"object でなければ読めない":  {`[]`, 2, errResult},
		"JSON でなければ読めない":    {`x`, 2, errResult},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := tree(t, map[string]string{"r.json": c.json})
			n, err := judgeSemgrep(filepath.Join(dir, "r.json"), c.want)
			if c.err != nil {
				require.ErrorIs(t, err, c.err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, c.want, n)
		})
	}
}

func Test_judgeSemgrepCanary(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		rc   int
		json string
		ok   bool
	}{
		"非0で s3-public-rw-bucket を報告すれば通る": {1, semgrepCanaryJSON, true},
		"0 で終われば落ちる":                       {0, semgrepCanaryJSON, false},
		"別の rule だけなら落ちる":                  {1, `{"results":[{"check_id":"terraform.aws.other"}]}`, false},
		"results が無ければ落ちる":                 {1, `{}`, false},
		"JSON でなければ落ちる":                    {2, `x`, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := tree(t, map[string]string{"r.json": c.json})
			err := judgeSemgrepCanary(c.rc, filepath.Join(dir, "r.json"))
			if c.ok {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, errCanary)
		})
	}
}

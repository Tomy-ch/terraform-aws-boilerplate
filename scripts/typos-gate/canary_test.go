package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const hiddenHit = "./.hidden/canary.txt:1:1: error: `" + misspelled + "` should be `the`\n"

var canaryConfig = []byte("[files]\nignore-hidden = false\n")

func Test_runCanary(t *testing.T) {
	t.Parallel()

	t.Run("隠しディレクトリの誤綴りで rc=2、.github/ を走査していれば通り、fixture を消す", func(t *testing.T) {
		t.Parallel()
		f := newFake(t)
		f.set(stepScan, "out", hiddenHit)
		f.set(stepScan, "rc", "2")
		f.set(stepFiles, "out", "./README.md\n./.github/workflows/x.yaml\n")
		work := filepath.Join(t.TempDir(), "work")
		var out bytes.Buffer
		require.NoError(t, runCanary(work, canaryConfig, &out, f.tool()))
		assert.Contains(t, out.String(), "✅ typos canary")
		calls := f.calls()
		require.Len(t, calls, 2)
		workReal, err := filepath.EvalSymlinks(filepath.Dir(work))
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(workReal, "work")+" "+stepScan+" ", calls[0])
		assert.Equal(t, realCwd(t)+" "+stepFiles+" ", calls[1], "--files は fixture ではなくリポジトリ直下（cwd）で走る")
		assert.NoDirExists(t, work)
	})

	degenerate := map[string]struct {
		scanOut, scanRC, filesOut, filesRC string
		want                               string
	}{
		"道具が常に 0 で何も出さない":            {scanRC: "0", filesRC: "0", want: "隠しディレクトリ"},
		"rc=1（shim が版を決められない）で出力は合う": {scanOut: hiddenHit, scanRC: "1", filesOut: "./.github/x\n", want: "隠しディレクトリ"},
		"rc=2 でも出力が空":                {scanRC: "2", filesOut: "./.github/x\n", want: "隠しディレクトリ"},
		"--files が .github/ を含まない":   {scanOut: hiddenHit, scanRC: "2", filesOut: "./README.md\n", want: ".github/"},
		"--files が rc≠0":             {scanOut: hiddenHit, scanRC: "2", filesOut: "./.github/x\n", filesRC: "1", want: ".github/"},
	}
	for name, c := range degenerate {
		t.Run(name+"なら errCanary で、成功を出さない", func(t *testing.T) {
			t.Parallel()
			f := newFake(t)
			f.set(stepScan, "out", c.scanOut)
			f.set(stepFiles, "out", c.filesOut)
			if c.scanRC != "" {
				f.set(stepScan, "rc", c.scanRC)
			}
			if c.filesRC != "" {
				f.set(stepFiles, "rc", c.filesRC)
			}
			var out bytes.Buffer
			err := runCanary(filepath.Join(t.TempDir(), "w"), canaryConfig, &out, f.tool())
			require.ErrorIs(t, err, errCanary)
			assert.Contains(t, err.Error(), c.want)
			assert.NotContains(t, out.String(), "✅")
		})
	}

	t.Run("両方が不成立なら、両方の理由と、各段の stdout・stderr・rc の見出しを出す", func(t *testing.T) {
		t.Parallel()
		f := newFake(t)
		f.set(stepScan, "out", "SCAN-STDOUT\n")
		f.set(stepScan, "err", "SCAN-STDERR-ONLY\n")
		f.set(stepScan, "rc", "1")
		f.set(stepFiles, "out", "FILES-STDOUT\n")
		f.set(stepFiles, "err", "FILES-STDERR-ONLY\n")
		var out bytes.Buffer
		err := runCanary(filepath.Join(t.TempDir(), "w"), canaryConfig, &out, f.tool())
		require.ErrorIs(t, err, errCanary)
		assert.Contains(t, err.Error(), "隠しディレクトリの誤綴りで落ちませんでした")
		assert.Contains(t, err.Error(), "走査の対象に .github/ が入っていません")
		for _, want := range []string{"--- typos --format brief（rc=1）:", "SCAN-STDOUT", "SCAN-STDERR-ONLY", "--- typos --files（rc=0）:", "FILES-STDOUT", "FILES-STDERR-ONLY"} {
			assert.Contains(t, out.String(), want)
		}
	})

	t.Run("canary の走査だけがシグナルで終われば、--files を起動せずに errTool", func(t *testing.T) {
		t.Parallel()
		f := newFake(t)
		f.set(stepScan, "kill", "")
		f.set(stepFiles, "out", "./.github/x\n")
		require.ErrorIs(t, runCanary(filepath.Join(t.TempDir(), "w"), canaryConfig, &bytes.Buffer{}, f.tool()), errTool)
		assert.Len(t, f.calls(), 1)
	})

	t.Run("道具を起動できなければ errTool", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, runCanary(filepath.Join(t.TempDir(), "w"), canaryConfig, &bytes.Buffer{}, missingTool), errTool)
	})

	t.Run("--files を起動できなければ errTool", func(t *testing.T) {
		t.Parallel()
		f := newFake(t)
		f.set(stepScan, "out", hiddenHit)
		f.set(stepScan, "rc", "2")
		f.set(stepFiles, "kill", "")
		require.ErrorIs(t, runCanary(filepath.Join(t.TempDir(), "w"), canaryConfig, &bytes.Buffer{}, f.tool()), errTool)
	})

	t.Run("fixture を置けなければ道具を起動せずに errRead", func(t *testing.T) {
		t.Parallel()
		f := newFake(t)
		file := writeFile(t, filepath.Join(t.TempDir(), "f"), "")
		require.ErrorIs(t, runCanary(filepath.Join(file, "w"), canaryConfig, &bytes.Buffer{}, f.tool()), errRead)
		assert.Empty(t, f.calls())
	})
}

func Test_readConfig(t *testing.T) {
	t.Parallel()

	t.Run("中身を返す", func(t *testing.T) {
		t.Parallel()
		p := writeFile(t, filepath.Join(t.TempDir(), "c"), "x = 1\n")
		got, err := readConfig(p)
		require.NoError(t, err)
		assert.Equal(t, "x = 1\n", string(got))
	})

	t.Run("無ければ errRead", func(t *testing.T) {
		t.Parallel()
		_, err := readConfig(filepath.Join(t.TempDir(), "none"))
		require.ErrorIs(t, err, errRead)
	})

	t.Run("空白だけなら errRead", func(t *testing.T) {
		t.Parallel()
		_, err := readConfig(writeFile(t, filepath.Join(t.TempDir(), "c"), "\n \n"))
		require.ErrorIs(t, err, errRead)
	})
}

func Test_buildFixture(t *testing.T) {
	t.Parallel()

	t.Run("作り直し、設定の複写と隠しディレクトリの誤綴りを置く", func(t *testing.T) {
		t.Parallel()
		work := filepath.Join(t.TempDir(), "w")
		stale := writeFile(t, filepath.Join(work, "stale"), "")
		require.NoError(t, buildFixture(work, []byte("CFG")))
		assert.NoFileExists(t, stale)
		cfg, err := os.ReadFile(filepath.Join(work, ".typos.toml"))
		require.NoError(t, err)
		assert.Equal(t, "CFG", string(cfg))
		word, err := os.ReadFile(filepath.Join(work, ".hidden", "canary.txt"))
		require.NoError(t, err)
		assert.Equal(t, misspelled+"\n", string(word))
	})

	t.Run("置けなければ errRead", func(t *testing.T) {
		t.Parallel()
		file := writeFile(t, filepath.Join(t.TempDir(), "f"), "")
		require.ErrorIs(t, buildFixture(filepath.Join(file, "w"), nil), errRead)
	})
}

func Test_judgeHidden(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		r    result
		want bool
	}{
		"rc=2・パス・語で成り立つ": {result{combined: hiddenHit, code: 2}, true},
		"絶対パスの形でも成り立つ":   {result{combined: "/w/.hidden/canary.txt:1:1: error: `" + misspelled + "`", code: 2}, true},
		"rc=0 は成り立たない":   {result{combined: hiddenHit}, false},
		"rc=1 は成り立たない":   {result{combined: hiddenHit, code: 1}, false},
		"rc=78（設定を読めない）": {result{combined: hiddenHit, code: 78}, false},
		"出力が空":           {result{code: 2}, false},
		"パスだけ":           {result{combined: "./.hidden/canary.txt", code: 2}, false},
		"語だけ":            {result{combined: misspelled, code: 2}, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, judgeHidden(c.r))
		})
	}
}

func Test_scansGitHubDir(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		out  string
		want bool
	}{
		"空":                 {"", false},
		".github 以外だけ":      {"./README.md\n./docs/x.md\n", false},
		"./.github/x":       {"./a\n./.github/x\n", true},
		".github/x":         {".github/x\n", true},
		"docs/.github/x":    {"./docs/.github/x\n", false},
		".githubx/y は当たらない": {"./.githubx/y\n", false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, scansGitHubDir(c.out))
		})
	}
}

package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

var testTool = imageTool{
	imageStep:   stepImageKics,
	command:     []string{"scan", "-p", "/work/src", "--price", "$5"},
	environment: map[string]string{"A": "1", "B": "$HOME"},
	workingDir:  "/work/$cfg",
}

func workDir(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	require.NoError(t, prepareWork(dir))
	return dir
}

func Test_lookupImage(t *testing.T) {
	t.Parallel()

	t.Run("digest で固定した1行を返す", func(t *testing.T) {
		t.Parallel()
		f, tool := newFake(t)
		got, err := lookupImage(tool, stepImageSemgrep)
		require.NoError(t, err)
		assert.Equal(t, testImage, got)
		assert.Equal(t, []string{"iac-scan image-semgrep"}, f.calls())
	})

	for name, out := range map[string]string{
		"空":            "",
		"空行だけ":         "\n",
		"2行":           testImage + "\n" + testImage + "\n",
		"digest が無い":   "example/scan:1.0\n",
		"digest が短い":   "example/scan:1.0@sha256:0123\n",
		"前後に空白":        " " + testImage + "\n",
		"digest だけの名前": "@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef\n",
	} {
		t.Run(name+"なら errImage", func(t *testing.T) {
			t.Parallel()
			f, tool := newFake(t)
			f.write("image", out)
			_, err := lookupImage(tool, stepImageSemgrep)
			require.ErrorIs(t, err, errImage)
		})
	}

	t.Run("docker compose に COMPOSE_ で始まる環境変数を渡さない", func(t *testing.T) {
		t.Parallel()
		f, r := newFake(t)
		r.environ = append(slices.Clone(r.environ), "COMPOSE_FILE=x.yaml", "COMPOSE_PROJECT_NAME=x", "IACSCAN_KEEP=1")
		_, err := lookupImage(r, stepImageSemgrep)
		require.NoError(t, err)
		env := f.envOf("iac-scan", stepImageSemgrep)
		assert.Contains(t, env, "IACSCAN_KEEP=1")
		for _, kv := range env {
			assert.False(t, strings.HasPrefix(kv, "COMPOSE_"), kv)
		}
	})

	t.Run("docker compose が落ちれば errImage", func(t *testing.T) {
		t.Parallel()
		f, tool := newFake(t)
		f.fail(stepImageSemgrep)
		_, err := lookupImage(tool, stepImageSemgrep)
		require.ErrorIs(t, err, errImage)
		assert.Contains(t, err.Error(), "fake: image-semgrep failed")
	})
}

func Test_writeCompose(t *testing.T) {
	t.Parallel()

	t.Run("service を1つだけ持ち、network と capability を与えず、$ を補間させない", func(t *testing.T) {
		t.Parallel()
		dir := workDir(t, "kics")
		require.NoError(t, writeCompose(dir, testImage, testTool))
		raw, err := os.ReadFile(filepath.Join(dir, "compose.yaml"))
		require.NoError(t, err)
		var got composeFile
		require.NoError(t, yaml.Unmarshal(raw, &got))
		assert.Regexp(t, `^iac-scan-[0-9a-f]{16}$`, got.Name)
		assert.Equal(t, map[string]composeServiceSpec{"scan": {
			Image:       testImage,
			NetworkMode: "none",
			CapDrop:     []string{"ALL"},
			CapAdd:      []string{"DAC_READ_SEARCH"},
			SecurityOpt: []string{"no-new-privileges:true"},
			PidsLimit:   1024,
			Command:     []string{"scan", "-p", "/work/src", "--price", "$$5"},
			Environment: map[string]string{"A": "1", "B": "$$HOME"},
			WorkingDir:  "/work/$$cfg",
		}}, got.Services)
	})

	t.Run("environment と working_dir を持たない道具は書かない", func(t *testing.T) {
		t.Parallel()
		dir := workDir(t, "checkov")
		require.NoError(t, writeCompose(dir, testImage, imageTool{command: []string{"-d", "/work/src"}}))
		raw, err := os.ReadFile(filepath.Join(dir, "compose.yaml"))
		require.NoError(t, err)
		assert.NotContains(t, string(raw), "environment")
		assert.NotContains(t, string(raw), "working_dir")
	})

	t.Run("project 名は作業ディレクトリごとに違い、同じディレクトリなら同じ", func(t *testing.T) {
		t.Parallel()
		name := func(dir string) string {
			require.NoError(t, writeCompose(dir, testImage, testTool))
			raw, err := os.ReadFile(filepath.Join(dir, "compose.yaml"))
			require.NoError(t, err)
			var got composeFile
			require.NoError(t, yaml.Unmarshal(raw, &got))
			return got.Name
		}
		a, b := workDir(t, "kics"), workDir(t, "kics")
		assert.NotEqual(t, name(a), name(b))
		assert.Equal(t, name(a), name(a))
	})

	t.Run("書けなければ errInput", func(t *testing.T) {
		t.Parallel()
		dir := tree(t, map[string]string{"file": ""})
		require.ErrorIs(t, writeCompose(filepath.Join(dir, "file"), testImage, testTool), errInput)
	})
}

func Test_runImage(t *testing.T) {
	t.Parallel()

	t.Run("create・copy-in・up・copy-out・down の順に走り、道具の終了コードと結果を返す", func(t *testing.T) {
		t.Parallel()
		f, tool := newFake(t)
		f.scenario("kics", 40, "", map[string]string{"results.json": "{}", "sub/x": "y"})
		dir := workDir(t, "kics")
		var log bytes.Buffer
		rc, err := runImage(dir, testImage, testTool, tool, &log)
		require.NoError(t, err)
		assert.Equal(t, 40, rc)
		assert.Equal(t, cycle("kics"), f.calls())
		assert.FileExists(t, filepath.Join(dir, "out", "results.json"))
		assert.FileExists(t, filepath.Join(dir, "out", "sub", "x"))
		assert.Contains(t, log.String(), "up-stdout kics")
		assert.Contains(t, log.String(), "up-stderr kics")
		assert.FileExists(t, filepath.Join(dir, "compose.yaml"))
	})

	t.Run("docker compose に COMPOSE_ で始まる環境変数を渡さない", func(t *testing.T) {
		t.Parallel()
		f, r := newFake(t)
		r.environ = append(slices.Clone(r.environ), "COMPOSE_FILE=x.yaml", "COMPOSE_PROJECT_NAME=x", "IACSCAN_KEEP=1")
		_, err := runImage(workDir(t, "kics"), testImage, testTool, r, &bytes.Buffer{})
		require.NoError(t, err)
		for _, step := range []string{stepCreate, stepCopyIn, stepUp, stepCopyOut, stepDown} {
			env := f.envOf("kics", step)
			assert.Contains(t, env, "IACSCAN_KEEP=1", step)
			for _, kv := range env {
				assert.False(t, strings.HasPrefix(kv, "COMPOSE_"), "%s: %s", step, kv)
			}
		}
	})

	t.Run("up を起動できなければ errDocker で、down する", func(t *testing.T) {
		t.Parallel()
		f, r := newFake(t)
		fakeTool := r.tool
		r.tool = func(d string, env []string, step string) *exec.Cmd {
			if step == stepUp {
				c := exec.CommandContext(context.Background(), filepath.Join(t.TempDir(), "no-such-docker"))
				c.Dir, c.Env = d, env
				return c
			}
			return fakeTool(d, env, step)
		}
		_, err := runImage(workDir(t, "kics"), testImage, testTool, r, &bytes.Buffer{})
		require.ErrorIs(t, err, errDocker)
		assert.Equal(t, []string{"kics create", "kics copy-in", "kics down"}, f.calls())
	})

	for _, step := range []string{stepCreate, stepCopyIn} {
		t.Run(step+" が落ちれば errDocker で、道具を起動せずに down する", func(t *testing.T) {
			t.Parallel()
			f, tool := newFake(t)
			f.fail(step)
			dir := workDir(t, "kics")
			_, err := runImage(dir, testImage, testTool, tool, &bytes.Buffer{})
			require.ErrorIs(t, err, errDocker)
			calls := f.calls()
			assert.NotContains(t, calls, "kics up")
			assert.Equal(t, "kics down", calls[len(calls)-1])
		})
	}

	t.Run("結果を取り出せなければ、道具が 0 でも errOutput で、down する", func(t *testing.T) {
		t.Parallel()
		f, tool := newFake(t)
		f.fail(stepCopyOut)
		dir := workDir(t, "kics")
		_, err := runImage(dir, testImage, testTool, tool, &bytes.Buffer{})
		require.ErrorIs(t, err, errOutput)
		assert.Equal(t, cycle("kics"), f.calls())
	})

	t.Run("down が落ちれば errDocker", func(t *testing.T) {
		t.Parallel()
		f, tool := newFake(t)
		f.fail(stepDown)
		dir := workDir(t, "kics")
		_, err := runImage(dir, testImage, testTool, tool, &bytes.Buffer{})
		require.ErrorIs(t, err, errDocker)
	})

	t.Run("先に落ちた理由を down の失敗で上書きしない", func(t *testing.T) {
		t.Parallel()
		f, tool := newFake(t)
		f.fail(stepCopyOut)
		f.fail(stepDown)
		dir := workDir(t, "kics")
		_, err := runImage(dir, testImage, testTool, tool, &bytes.Buffer{})
		require.ErrorIs(t, err, errOutput)
	})

	t.Run("compose.yaml を書けなければ docker を起動しない", func(t *testing.T) {
		t.Parallel()
		f, tool := newFake(t)
		dir := tree(t, map[string]string{"file": ""})
		_, err := runImage(filepath.Join(dir, "file"), testImage, testTool, tool, &bytes.Buffer{})
		require.ErrorIs(t, err, errInput)
		assert.Empty(t, f.calls())
	})
}

func Test_prepareWork(t *testing.T) {
	t.Parallel()

	t.Run("前の中身を消して work/src を作る", func(t *testing.T) {
		t.Parallel()
		base := tree(t, map[string]string{"x/out/old.json": "", "x/compose.yaml": ""})
		dir := filepath.Join(base, "x")
		require.NoError(t, prepareWork(dir))
		assert.DirExists(t, filepath.Join(dir, "work", "src"))
		assert.NoDirExists(t, filepath.Join(dir, "out"))
		assert.NoFileExists(t, filepath.Join(dir, "compose.yaml"))
	})

	t.Run("作れなければ errInput", func(t *testing.T) {
		t.Parallel()
		base := tree(t, map[string]string{"file": ""})
		require.ErrorIs(t, prepareWork(filepath.Join(base, "file", "x")), errInput)
	})
}

func Test_runCanary(t *testing.T) {
	t.Parallel()

	judgeRC := func(want int) func(int, string) error {
		return func(rc int, _ string) error {
			if rc != want {
				return errCanary
			}
			return nil
		}
	}

	t.Run("通れば作業ディレクトリを消し、canary の出力を出さない", func(t *testing.T) {
		t.Parallel()
		f, tool := newFake(t)
		f.scenario("x-canary", 1, "", nil)
		dir := filepath.Join(t.TempDir(), "x-canary")
		var prepared bool
		var log bytes.Buffer
		err := runCanary("x の canary", dir, "fixture", testImage, testTool, tool, &log, judgeRC(1), func() error {
			prepared = true
			return writeText(filepath.Join(dir, "work", "cfg", "r.yaml"), "")
		})
		require.NoError(t, err)
		assert.True(t, prepared)
		assert.Equal(t, cycle("x-canary"), f.calls())
		assert.NoDirExists(t, dir)
		assert.Empty(t, log.String())
	})

	t.Run("fixture を work/src/main.tf に置く", func(t *testing.T) {
		t.Parallel()
		_, tool := newFake(t)
		dir := filepath.Join(t.TempDir(), "x-canary")
		var seen string
		err := runCanary("x の canary", dir, "fixture", testImage, testTool, tool, &bytes.Buffer{}, func(int, string) error {
			raw, err := os.ReadFile(filepath.Join(dir, "work", "src", "main.tf"))
			seen = string(raw)
			return err
		}, nil)
		require.NoError(t, err)
		assert.Equal(t, "fixture", seen)
	})

	t.Run("判定が落ちれば、その理由を返し、出力を log へ出し、作業ディレクトリを残す", func(t *testing.T) {
		t.Parallel()
		f, tool := newFake(t)
		f.scenario("x-canary", 0, "", nil)
		dir := filepath.Join(t.TempDir(), "x-canary")
		var log bytes.Buffer
		err := runCanary("x の canary", dir, "fixture", testImage, testTool, tool, &log, judgeRC(1), nil)
		require.ErrorIs(t, err, errCanary)
		assert.Contains(t, log.String(), "--- x の canary の出力:")
		assert.Contains(t, log.String(), "up-stdout x-canary")
		assert.DirExists(t, dir)
	})

	t.Run("docker が落ちれば判定せずに返す", func(t *testing.T) {
		t.Parallel()
		f, tool := newFake(t)
		f.fail(stepCreate)
		dir := filepath.Join(t.TempDir(), "x-canary")
		judged := false
		err := runCanary("x の canary", dir, "fixture", testImage, testTool, tool, &bytes.Buffer{}, func(int, string) error {
			judged = true
			return nil
		}, nil)
		require.ErrorIs(t, err, errDocker)
		assert.False(t, judged)
	})

	t.Run("prepare が落ちれば docker を起動しない", func(t *testing.T) {
		t.Parallel()
		f, tool := newFake(t)
		dir := filepath.Join(t.TempDir(), "x-canary")
		err := runCanary("x の canary", dir, "fixture", testImage, testTool, tool, &bytes.Buffer{}, judgeRC(0), func() error { return errInput })
		require.ErrorIs(t, err, errInput)
		assert.Empty(t, f.calls())
	})
}

func Test_writeText(t *testing.T) {
	t.Parallel()

	t.Run("親を作って書く", func(t *testing.T) {
		t.Parallel()
		p := filepath.Join(t.TempDir(), "a", "b", "c.txt")
		require.NoError(t, writeText(p, "body"))
		raw, err := os.ReadFile(p)
		require.NoError(t, err)
		assert.Equal(t, "body", string(raw))
	})

	t.Run("親がファイルなら errInput", func(t *testing.T) {
		t.Parallel()
		dir := tree(t, map[string]string{"file": ""})
		require.ErrorIs(t, writeText(filepath.Join(dir, "file", "c.txt"), "body"), errInput)
	})
}

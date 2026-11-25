package main

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/testenv"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun/tooltest"
)

var testTool = imageTool{
	imageStep:   stepImageKics,
	command:     []string{"scan", "-p", "/work/src", "--price", "$5"},
	environment: map[string]string{"A": "1", "B": "$HOME"},
	workingDir:  "/work/$cfg",
}

func workDir(t *testing.T, name string) string {
	t.Helper()
	dir, err := prepareWork(t.TempDir(), "tmp/"+name)
	require.NoError(t, err)
	return dir
}

// escapingWork は、tmp/<name> が外のディレクトリを指す symlink である base と、その外のディレクトリを返す。外には keep を
// 置く。
func escapingWork(t *testing.T, name string) (base, outside string) {
	t.Helper()
	base = tfTree(t)
	outside = tree(t, map[string]string{"keep": "k"})
	require.NoError(t, os.MkdirAll(filepath.Join(base, "tmp"), 0o750))
	require.NoError(t, os.Symlink(outside, filepath.Join(base, "tmp", name)))
	return base, outside
}

// escapingParent は、tmp/a が外のディレクトリを指す symlink である base と、その外のディレクトリを返す。外には x/keep を
// 置くので、作業ディレクトリ tmp/a/x を消せば keep も消える。
func escapingParent(t *testing.T) (base, outside string) {
	t.Helper()
	base = tfTree(t)
	outside = tree(t, map[string]string{"x/keep": "k"})
	require.NoError(t, os.MkdirAll(filepath.Join(base, "tmp"), 0o750))
	require.NoError(t, os.Symlink(outside, filepath.Join(base, "tmp", "a")))
	return base, outside
}

func requireLinkKept(t *testing.T, base, name string) {
	t.Helper()
	info, err := os.Lstat(filepath.Join(base, "tmp", name))
	require.NoError(t, err)
	require.Equal(t, os.ModeSymlink, info.Mode().Type())
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
		"digest が63桁":  "example/scan:1.0@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcde\n",
		"digest が65桁":  "example/scan:1.0@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0\n",
		"digest が大文字":  "example/scan:1.0@sha256:0123456789ABCDEF0123456789abcdef0123456789abcdef0123456789abcdef\n",
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

	t.Run("docker compose を起動できなければ errTool", func(t *testing.T) {
		t.Parallel()
		_, err := lookupImage(runner{tool: tooltest.Missing, environ: os.Environ()}, stepImageSemgrep)
		require.ErrorIs(t, err, errTool)
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

	t.Run("copy-in は work/ の tar を stdin で渡す", func(t *testing.T) {
		t.Parallel()
		f, tool := newFake(t)
		dir := workDir(t, "kics")
		require.NoError(t, writeText(filepath.Join(dir, "work", "src", "main.tf"), "x\n"))
		_, err := runImage(dir, testImage, testTool, tool, &bytes.Buffer{})
		require.NoError(t, err)
		assert.Equal(t, []tarEntry{
			{"work/", tar.TypeDir, 0, 0, 0o700, ""},
			{"work/src/", tar.TypeDir, 0, 0, 0o700, ""},
			{"work/src/main.tf", tar.TypeReg, 0, 0, 0o600, "x\n"},
		}, f.tarOf("kics"))
	})

	t.Run("work/ に symlink があれば docker を起動せずに errInput", func(t *testing.T) {
		t.Parallel()
		f, tool := newFake(t)
		dir := workDir(t, "kics")
		require.NoError(t, os.Symlink("/etc/passwd", filepath.Join(dir, "work", "src", "link.tf")))
		_, err := runImage(dir, testImage, testTool, tool, &bytes.Buffer{})
		require.ErrorIs(t, err, errInput)
		assert.Empty(t, f.calls())
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

	t.Run("up を起動できなければ errTool で、down する", func(t *testing.T) {
		t.Parallel()
		f, r := newFake(t)
		fakeTool := r.tool
		r.tool = func(ctx context.Context, d string, env []string, step string, _ ...string) *exec.Cmd {
			if step == stepUp {
				return tooltest.Missing(ctx, d, env, step)
			}
			return fakeTool(ctx, d, env, step)
		}
		_, err := runImage(workDir(t, "kics"), testImage, testTool, r, &bytes.Buffer{})
		require.ErrorIs(t, err, errTool)
		assert.Equal(t, []string{"kics create", "kics copy-in", "kics down"}, f.calls())
	})

	t.Run("up がシグナルで終われば、終了コードとして扱わずに errTool で、down する", func(t *testing.T) {
		t.Parallel()
		f, r := newFake(t)
		f.kill(stepUp)
		_, err := runImage(workDir(t, "kics"), testImage, testTool, r, &bytes.Buffer{})
		require.ErrorIs(t, err, errTool)
		assert.Equal(t, []string{"kics create", "kics copy-in", "kics up", "kics down"}, f.calls())
	})

	for _, step := range []string{stepCreate, stepCopyIn, stepCopyOut, stepDown} {
		t.Run(step+" を起動できなければ errTool", func(t *testing.T) {
			t.Parallel()
			_, r := newFake(t)
			fakeTool := r.tool
			r.tool = func(ctx context.Context, d string, env []string, s string, _ ...string) *exec.Cmd {
				if s == step {
					return tooltest.Missing(ctx, d, env, s)
				}
				return fakeTool(ctx, d, env, s)
			}
			_, err := runImage(workDir(t, "kics"), testImage, testTool, r, &bytes.Buffer{})
			require.ErrorIs(t, err, errTool)
		})
	}

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

func Test_workTar(t *testing.T) {
	t.Parallel()

	t.Run("work/ の下を uid・gid 0、ディレクトリ 0700、ファイル 0600 で並べる", func(t *testing.T) {
		t.Parallel()
		base := tree(t, map[string]string{"work/src/env/main.tf": "resource {}\n", "work/cfg/r.yaml": "rules: []\n"})
		require.NoError(t, os.Chmod(filepath.Join(base, "work", "src", "env", "main.tf"), 0o644))
		raw, err := workTar(filepath.Join(base, "work"))
		require.NoError(t, err)
		assert.Equal(t, []tarEntry{
			{"work/", tar.TypeDir, 0, 0, 0o700, ""},
			{"work/cfg/", tar.TypeDir, 0, 0, 0o700, ""},
			{"work/cfg/r.yaml", tar.TypeReg, 0, 0, 0o600, "rules: []\n"},
			{"work/src/", tar.TypeDir, 0, 0, 0o700, ""},
			{"work/src/env/", tar.TypeDir, 0, 0, 0o700, ""},
			{"work/src/env/main.tf", tar.TypeReg, 0, 0, 0o600, "resource {}\n"},
		}, readTar(t, raw))
	})

	t.Run("symlink は写さずに errInput", func(t *testing.T) {
		t.Parallel()
		base := tree(t, map[string]string{"work/src/main.tf": ""})
		require.NoError(t, os.Symlink("/etc/passwd", filepath.Join(base, "work", "src", "link.tf")))
		_, err := workTar(filepath.Join(base, "work"))
		require.ErrorIs(t, err, errInput)
		assert.Contains(t, err.Error(), "link.tf")
	})

	t.Run("無いディレクトリは errInput", func(t *testing.T) {
		t.Parallel()
		_, err := workTar(filepath.Join(t.TempDir(), "work"))
		require.ErrorIs(t, err, errInput)
	})
}

func readTar(t *testing.T, raw []byte) []tarEntry {
	t.Helper()
	var got []tarEntry
	tr := tar.NewReader(bytes.NewReader(raw))
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return got
		}
		require.NoError(t, err)
		body, err := io.ReadAll(tr)
		require.NoError(t, err)
		got = append(got, tarEntry{h.Name, h.Typeflag, h.Uid, h.Gid, h.Mode, string(body)})
	}
}

func Test_resolveWork(t *testing.T) {
	t.Parallel()

	t.Run("tmp/ の下の作業ディレクトリのパスを返す", func(t *testing.T) {
		t.Parallel()
		base := t.TempDir()
		got, err := resolveWork(base, "tmp/checkov")
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(base, "tmp", "checkov"), got)
	})

	for name, work := range map[string]string{"tmp/ の外": "checkov", "tmp/ 自身": "tmp", "親へ出る": "tmp/../x", "絶対パス": "/tmp/checkov"} {
		t.Run(name+"は errUsage", func(t *testing.T) {
			t.Parallel()
			_, err := resolveWork(t.TempDir(), work)
			require.ErrorIs(t, err, errUsage)
		})
	}

	t.Run("作業ディレクトリが tmp/ の外を指す symlink なら errUsage", func(t *testing.T) {
		t.Parallel()
		base, _ := escapingWork(t, "checkov")
		_, err := resolveWork(base, "tmp/checkov")
		require.ErrorIs(t, err, errUsage)
	})

	t.Run("途中のディレクトリが tmp/ の外を指す symlink なら errUsage", func(t *testing.T) {
		t.Parallel()
		base, _ := escapingParent(t)
		_, err := resolveWork(base, "tmp/a/x")
		require.ErrorIs(t, err, errUsage)
	})
}

func Test_prepareWork(t *testing.T) {
	t.Parallel()

	t.Run("前の中身を消して work/src を作り、そのパスを返す", func(t *testing.T) {
		t.Parallel()
		base := tree(t, map[string]string{"tmp/x/out/old.json": "", "tmp/x/compose.yaml": ""})
		dir, err := prepareWork(base, "tmp/x")
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(base, "tmp", "x"), dir)
		assert.DirExists(t, filepath.Join(dir, "work", "src"))
		assert.NoDirExists(t, filepath.Join(dir, "out"))
		assert.NoFileExists(t, filepath.Join(dir, "compose.yaml"))
	})

	t.Run("作れなければ errInput", func(t *testing.T) {
		t.Parallel()
		testenv.RequireNonRoot(t, "tmp/ を読み取り専用にして作成を落とす")
		base := tree(t, map[string]string{"tmp/.keep": ""})
		tmp := filepath.Join(base, "tmp")
		require.NoError(t, os.Chmod(tmp, 0o500))
		t.Cleanup(func() { _ = os.Chmod(tmp, 0o700) })
		_, err := prepareWork(base, "tmp/x")
		require.ErrorIs(t, err, errInput)
	})

	t.Run("作業ディレクトリが tmp/ の外を指せば、何も消さずに errUsage", func(t *testing.T) {
		t.Parallel()
		base, outside := escapingParent(t)
		_, err := prepareWork(base, "tmp/a/x")
		require.ErrorIs(t, err, errUsage)
		assert.FileExists(t, filepath.Join(outside, "x", "keep"))
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
		base := t.TempDir()
		dir := filepath.Join(base, "tmp", "x-canary")
		var prepared bool
		var log bytes.Buffer
		err := runCanary("x の canary", base, "tmp/x-canary", "fixture", testImage, testTool, tool, &log, judgeRC(1), func(got string) error {
			prepared = true
			assert.Equal(t, dir, got)
			return writeText(filepath.Join(got, "work", "cfg", "r.yaml"), "")
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
		base := t.TempDir()
		dir := filepath.Join(base, "tmp", "x-canary")
		var seen string
		err := runCanary("x の canary", base, "tmp/x-canary", "fixture", testImage, testTool, tool, &bytes.Buffer{}, func(int, string) error {
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
		base := t.TempDir()
		dir := filepath.Join(base, "tmp", "x-canary")
		var log bytes.Buffer
		err := runCanary("x の canary", base, "tmp/x-canary", "fixture", testImage, testTool, tool, &log, judgeRC(1), nil)
		require.ErrorIs(t, err, errCanary)
		assert.Contains(t, log.String(), "--- x の canary の出力:")
		assert.Contains(t, log.String(), "up-stdout x-canary")
		assert.DirExists(t, dir)
	})

	t.Run("docker が落ちれば判定せずに返す", func(t *testing.T) {
		t.Parallel()
		f, tool := newFake(t)
		f.fail(stepCreate)
		base := t.TempDir()
		judged := false
		err := runCanary("x の canary", base, "tmp/x-canary", "fixture", testImage, testTool, tool, &bytes.Buffer{}, func(int, string) error {
			judged = true
			return nil
		}, nil)
		require.ErrorIs(t, err, errDocker)
		assert.False(t, judged)
	})

	t.Run("prepare が落ちれば docker を起動しない", func(t *testing.T) {
		t.Parallel()
		f, tool := newFake(t)
		base := t.TempDir()
		err := runCanary("x の canary", base, "tmp/x-canary", "fixture", testImage, testTool, tool, &bytes.Buffer{}, judgeRC(0), func(string) error { return errInput })
		require.ErrorIs(t, err, errInput)
		assert.Empty(t, f.calls())
	})

	t.Run("作業ディレクトリが tmp/ の外を指せば、何も消さず docker も起動せずに errUsage", func(t *testing.T) {
		t.Parallel()
		f, tool := newFake(t)
		base, outside := escapingParent(t)
		err := runCanary("x の canary", base, "tmp/a/x", "fixture", testImage, testTool, tool, &bytes.Buffer{}, judgeRC(0), nil)
		require.ErrorIs(t, err, errUsage)
		assert.FileExists(t, filepath.Join(outside, "x", "keep"))
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

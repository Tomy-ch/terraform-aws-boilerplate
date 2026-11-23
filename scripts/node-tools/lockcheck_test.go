package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	fixturePackage = `{
  "dependencies": {
    "@commitlint/cli": "20.1.0",
    "markdownlint-cli2": "0.24.1"
  }%s
}
`
	fixtureLock = `lockfileVersion: '9.0'

packages:

  markdownlint-cli2@0.24.1:
    resolution: {integrity: sha512-nLhes/MIPJQLm+Gmh==}
    hasBin: true

snapshots:

  markdownlint-cli2@0.24.1:
    dependencies:
      globby: 16.2.4
`
	fixtureWorkspace = "minimumReleaseAge: 10080\n"
)

// declDir は宣言の1組を置きます。marker は package.json に足す欄で、fakePnpm の振る舞いを選ぶ。
func declDir(t *testing.T, marker string) string {
	t.Helper()
	dir := t.TempDir()
	extra := ""
	if marker != "" {
		extra = ",\n  \"" + marker + "\": 1"
	}
	for name, body := range map[string]string{
		"package.json":        strings.Replace(fixturePackage, "%s", extra, 1),
		"pnpm-lock.yaml":      fixtureLock,
		"pnpm-workspace.yaml": fixtureWorkspace,
	} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600))
	}

	return dir
}

// canaryDeps は、self にテストのバイナリ（TestMain が node-tools として振る舞わせる）を、wd に一時ディレクトリを置きます。
func canaryDeps(t *testing.T, f *fakeTools, stderr io.Writer) (deps, string) {
	t.Helper()
	wd := t.TempDir()
	d := testDeps(f, stderr)
	self := selfAsNodeTools(t)
	d.self = func() (string, error) { return self, nil }
	d.wd = func() (string, error) { return wd, nil }

	return d, wd
}

func assertNoCanaryLeft(t *testing.T, wd string) {
	t.Helper()
	left, err := os.ReadDir(filepath.Join(wd, "tmp"))
	require.NoError(t, err)
	assert.Empty(t, left)
}

func Test_runLockCheck(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	t.Run("canary が通れば -dir のディレクトリで lockfile を照合する", func(t *testing.T) {
		t.Parallel()
		f := &fakeTools{scripts: map[string]string{stepLockCheck: "true"}}
		var stderr bytes.Buffer
		d, _ := canaryDeps(t, f, &stderr)
		dir := declDir(t, "")
		require.NoError(t, runLockCheck(ctx, []string{"-dir", dir}, d))
		assert.Equal(t, []string{stepLockCheck}, f.called())
		assert.Equal(t, []string{dir}, f.dirs())
		assert.Contains(t, stderr.String(), "✅ node-tools canary")
	})
	t.Run("canary が落ちれば照合せず、✅ を出さない", func(t *testing.T) {
		t.Parallel()
		f := &fakeTools{scripts: map[string]string{stepLockCheck: "true"}}
		var stderr bytes.Buffer
		d, _ := canaryDeps(t, f, &stderr)
		require.ErrorIs(t, runLockCheck(ctx, []string{"-dir", declDir(t, "x-test-launch")}, d), errCanaryLaunched)
		assert.Empty(t, f.called())
		assert.NotContains(t, stderr.String(), "✅")
	})
	t.Run("lockfile の形が変わって canary が書き換えられなければ照合しない", func(t *testing.T) {
		t.Parallel()
		dir := declDir(t, "")
		require.NoError(t, os.WriteFile(filepath.Join(dir, "pnpm-lock.yaml"), []byte("packages:\n"), 0o600))
		f := &fakeTools{scripts: map[string]string{stepLockCheck: "true"}}
		d, _ := canaryDeps(t, f, io.Discard)
		require.ErrorIs(t, runLockCheck(ctx, []string{"-dir", dir}, d), errCanaryShape)
		assert.Empty(t, f.called())
	})
	t.Run("照合が落ちれば、その終了コードで落ちる", func(t *testing.T) {
		t.Parallel()
		f := &fakeTools{scripts: map[string]string{stepLockCheck: "echo ERR_PNPM_OUTDATED_LOCKFILE >&2; exit 1"}}
		var stderr bytes.Buffer
		d, _ := canaryDeps(t, f, &stderr)
		err := runLockCheck(ctx, []string{"-dir", declDir(t, "")}, d)
		var exitErr *exec.ExitError
		require.ErrorAs(t, err, &exitErr)
		assert.Equal(t, 1, exitErr.ExitCode())
		assert.Contains(t, stderr.String(), "ERR_PNPM_OUTDATED_LOCKFILE")
	})
	t.Run("照合の途中で中断されたら中断として落ちる", func(t *testing.T) {
		t.Parallel()
		f := &fakeTools{scripts: map[string]string{stepLockCheck: "exec sleep 30"}}
		d, _ := canaryDeps(t, f, io.Discard)
		cctx, cancel := context.WithCancel(ctx)
		d.tool = func(c context.Context, dir, step string) *exec.Cmd {
			cmd := f.tool(c, dir, step)
			cancel()

			return cmd
		}
		require.ErrorIs(t, runLockCheck(cctx, []string{"-dir", declDir(t, "")}, d), errInterrupted)
	})
	for name, args := range map[string][]string{
		"-dir が無い":   nil,
		"余分な引数がある":   {"-dir", "d", "x"},
		"知らないフラグがある": {"-x"},
	} {
		t.Run(name+"なら usage で落ちる", func(t *testing.T) {
			t.Parallel()
			require.ErrorIs(t, runLockCheck(ctx, args, testDeps(&fakeTools{}, io.Discard)), errLockCheckUsage)
		})
	}
}

func Test_runCanary(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	t.Run("どのケースも期待の理由で道具の起動の前に落ちれば通り、一時ディレクトリを残さない", func(t *testing.T) {
		t.Parallel()
		d, wd := canaryDeps(t, &fakeTools{}, io.Discard)
		require.NoError(t, runCanary(ctx, declDir(t, ""), canaryCases, d))
		assertNoCanaryLeft(t, wd)
	})
	t.Run("壊れた写しでも道具が起動すれば落とし、一時ディレクトリを残さない", func(t *testing.T) {
		t.Parallel()
		var stderr bytes.Buffer
		d, wd := canaryDeps(t, &fakeTools{}, &stderr)
		err := runCanary(ctx, declDir(t, "x-test-launch"), canaryCases, d)
		require.ErrorIs(t, err, errCanaryLaunched)
		assert.Contains(t, err.Error(), "drift")
		assert.Contains(t, stderr.String(), "launched")
		assertNoCanaryLeft(t, wd)
	})
	t.Run("drift が通っても integrity が別の理由で落ちれば落とす", func(t *testing.T) {
		t.Parallel()
		var stderr bytes.Buffer
		d, wd := canaryDeps(t, &fakeTools{}, &stderr)
		err := runCanary(ctx, declDir(t, "x-test-integrity-reason"), canaryCases, d)
		require.ErrorIs(t, err, errCanaryReason)
		assert.Contains(t, err.Error(), "integrity")
		assert.Contains(t, stderr.String(), "ERR_PNPM_FETCH_404")
		assertNoCanaryLeft(t, wd)
	})
	t.Run("ケースが0件なら何も確かめずに通さない", func(t *testing.T) {
		t.Parallel()
		d, _ := canaryDeps(t, &fakeTools{}, io.Discard)
		require.ErrorIs(t, runCanary(ctx, declDir(t, ""), nil, d), errCanaryNoCases)
	})
	t.Run("package.json の形が変わって書き換えが当たらなければ落ちる", func(t *testing.T) {
		t.Parallel()
		dir := declDir(t, "")
		require.NoError(t, os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"dependencies": {}}`), 0o600))
		d, wd := canaryDeps(t, &fakeTools{}, io.Discard)
		require.ErrorIs(t, runCanary(ctx, dir, canaryCases, d), errCanaryShape)
		assertNoCanaryLeft(t, wd)
	})
	t.Run("宣言のファイルが欠けていれば落ちる", func(t *testing.T) {
		t.Parallel()
		dir := declDir(t, "")
		require.NoError(t, os.Remove(filepath.Join(dir, "pnpm-workspace.yaml")))
		d, _ := canaryDeps(t, &fakeTools{}, io.Discard)
		require.ErrorIs(t, runCanary(ctx, dir, canaryCases, d), os.ErrNotExist)
	})
	t.Run("一時ディレクトリの置き場を作れなければ落ちる", func(t *testing.T) {
		t.Parallel()
		d, wd := canaryDeps(t, &fakeTools{}, io.Discard)
		require.NoError(t, os.WriteFile(filepath.Join(wd, "tmp"), nil, 0o600))
		require.ErrorIs(t, runCanary(ctx, declDir(t, ""), canaryCases, d), syscall.ENOTDIR)
	})
	t.Run("中断されたら中断として落ち、一時ディレクトリを残さない", func(t *testing.T) {
		t.Parallel()
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		d, wd := canaryDeps(t, &fakeTools{}, io.Discard)
		require.ErrorIs(t, runCanary(cctx, declDir(t, ""), canaryCases, d), errInterrupted)
		assertNoCanaryLeft(t, wd)
	})
	t.Run("作業ディレクトリを取れなければ落ちる", func(t *testing.T) {
		t.Parallel()
		d, _ := canaryDeps(t, &fakeTools{}, io.Discard)
		sentinel := errors.New("wd")
		d.wd = func() (string, error) { return "", sentinel }
		require.ErrorIs(t, runCanary(ctx, declDir(t, ""), canaryCases, d), sentinel)
	})
	t.Run("自分の実行ファイルを取れなければ落ちる", func(t *testing.T) {
		t.Parallel()
		d, _ := canaryDeps(t, &fakeTools{}, io.Discard)
		sentinel := errors.New("self")
		d.self = func() (string, error) { return "", sentinel }
		require.ErrorIs(t, runCanary(ctx, declDir(t, ""), canaryCases, d), sentinel)
	})
}

func Test_canaryCommand(t *testing.T) {
	t.Parallel()

	t.Run("自分の exec を写しと canary の lock で起動する", func(t *testing.T) {
		t.Parallel()
		c := canaryCommand(context.Background(), "/bin/node-tools", "/w/drift", "/w/lock")
		assert.Equal(t, []string{"/bin/node-tools", "exec", "-dir", "/w/drift", "-lock", "/w/lock", canaryTool, "--version"}, c.Args)
	})
}

func Test_judgeCanary(t *testing.T) {
	t.Parallel()

	c := canaryCases[0]
	failed := errors.New("exit status 1")
	for name, tc := range map[string]struct {
		out  string
		err  error
		want error
	}{
		"偽の道具の印があれば退いたとして落とす（期待のエラーも出ていても）": {out: canaryMarker + "\n" + c.want, err: failed, want: errCanaryFallback},
		"偽の道具の印があれば 0 で終わっても退いたとして落とす":      {out: canaryMarker, err: nil, want: errCanaryFallback},
		"0 で終われば起動したとして落とす":                 {out: c.want, err: nil, want: errCanaryLaunched},
		"期待のエラーで落ちていれば通す":                   {out: "x " + c.want + " y", err: failed},
		"別の理由で落ちていれば落とす":                    {out: "ERR_PNPM_FETCH_404", err: failed, want: errCanaryReason},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := judgeCanary(c, []byte(tc.out), tc.err)
			if tc.want == nil {
				require.NoError(t, err)

				return
			}
			require.ErrorIs(t, err, tc.want)
			assert.Contains(t, err.Error(), c.name)
		})
	}
}

func Test_writeFake(t *testing.T) {
	t.Parallel()

	t.Run("同名の偽の道具を実行できる形で置き、印を出す台本にする", func(t *testing.T) {
		t.Parallel()
		bin := t.TempDir()
		require.NoError(t, writeFake(bin))
		p := filepath.Join(bin, canaryTool)
		info, err := os.Stat(p)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o500), info.Mode().Perm())
		// 書いた直後に実行すると、並行の fork が書き込みの fd を握って ETXTBSY になりうるので、中身で確かめる。
		b, err := os.ReadFile(p) //nolint:gosec // t.TempDir 配下
		require.NoError(t, err)
		assert.Equal(t, "#!/bin/sh\necho "+canaryMarker+"\n", string(b))
	})
	t.Run("置き場が無ければ落ちる", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, writeFake(filepath.Join(t.TempDir(), "none")), os.ErrNotExist)
	})
}

func Test_copyDecl(t *testing.T) {
	t.Parallel()

	for _, c := range canaryCases {
		t.Run(c.name+" は1組を写し、"+c.file+" だけを壊す", func(t *testing.T) {
			t.Parallel()
			src := declDir(t, "")
			dst := filepath.Join(t.TempDir(), c.name)
			require.NoError(t, copyDecl(src, dst, c))
			for _, name := range canaryFiles {
				want, err := os.ReadFile(filepath.Join(src, name)) //nolint:gosec // t.TempDir 配下
				require.NoError(t, err)
				got, err := os.ReadFile(filepath.Join(dst, name)) //nolint:gosec // t.TempDir 配下
				require.NoError(t, err)
				if name == c.file {
					assert.NotEqual(t, string(want), string(got))
				} else {
					assert.Equal(t, string(want), string(got))
				}
			}
		})
	}
	t.Run("写し先が既に在れば落ちる", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, copyDecl(declDir(t, ""), t.TempDir(), canaryCases[0]), os.ErrExist)
	})
	t.Run("写し元が無ければ落ちる", func(t *testing.T) {
		t.Parallel()
		dst := filepath.Join(t.TempDir(), "x")
		require.ErrorIs(t, copyDecl(filepath.Join(t.TempDir(), "none"), dst, canaryCases[0]), os.ErrNotExist)
	})
	t.Run("書き換えが当たらなければ、ケースとファイルを名指して落ちる", func(t *testing.T) {
		t.Parallel()
		src := declDir(t, "")
		require.NoError(t, os.WriteFile(filepath.Join(src, "pnpm-lock.yaml"), []byte("packages:\n"), 0o600))
		err := copyDecl(src, filepath.Join(t.TempDir(), "integrity"), canaryCases[1])
		require.ErrorIs(t, err, errCanaryShape)
		assert.Contains(t, err.Error(), "integrity: pnpm-lock.yaml")
	})
}

func Test_mutateDrift(t *testing.T) {
	t.Parallel()

	t.Run("版を 0.0.0 にずらし、他の行を変えない", func(t *testing.T) {
		t.Parallel()
		in := strings.Replace(fixturePackage, "%s", "", 1)
		got, err := mutateDrift([]byte(in))
		require.NoError(t, err)
		assert.Contains(t, string(got), `"markdownlint-cli2": "0.0.0"`)
		assert.NotContains(t, string(got), "0.24.1")
		assert.Contains(t, string(got), `"@commitlint/cli": "20.1.0"`)
	})
	for name, body := range map[string]string{
		"項目が無い":    `{"dependencies": {}}`,
		"項目が2つ在る":  `"markdownlint-cli2": "1", "markdownlint-cli2": "2"`,
		"既に 0.0.0": `"markdownlint-cli2": "0.0.0"`,
	} {
		t.Run(name+"なら形の変化として落ちる", func(t *testing.T) {
			t.Parallel()
			_, err := mutateDrift([]byte(body))
			require.ErrorIs(t, err, errCanaryShape)
		})
	}
}

func Test_mutateIntegrity(t *testing.T) {
	t.Parallel()

	t.Run("packages の項目の integrity の先頭4文字だけを書き換える", func(t *testing.T) {
		t.Parallel()
		got, err := mutateIntegrity([]byte(fixtureLock))
		require.NoError(t, err)
		assert.Contains(t, string(got), "integrity: sha512-AAAAs/MIPJQLm+Gmh==")
		assert.Len(t, got, len(fixtureLock))
	})
	t.Run("元が AAAA なら BBBB にする", func(t *testing.T) {
		t.Parallel()
		in := "  markdownlint-cli2@1.0.0:\n    resolution: {integrity: sha512-AAAAxyz==}\n"
		got, err := mutateIntegrity([]byte(in))
		require.NoError(t, err)
		assert.Contains(t, string(got), "sha512-BBBBxyz==")
	})
	for name, body := range map[string]string{
		"項目が無い":                  "packages:\n",
		"次の行に integrity が無い":     "  markdownlint-cli2@1.0.0:\n    dependencies:\n",
		"integrity が項目の2行目以降にある": "  markdownlint-cli2@1.0.0:\n    engines: {node: '>=22'}\n    resolution: {integrity: sha512-xxxx}\n",
		"項目が2つ在る": "  markdownlint-cli2@1.0.0:\n    resolution: {integrity: sha512-xxxx}\n" +
			"  markdownlint-cli2@2.0.0:\n    resolution: {integrity: sha512-yyyy}\n",
	} {
		t.Run(name+"なら形の変化として落ちる", func(t *testing.T) {
			t.Parallel()
			_, err := mutateIntegrity([]byte(body))
			require.ErrorIs(t, err, errCanaryShape)
		})
	}
}

func Test_withoutPath(t *testing.T) {
	t.Parallel()

	t.Run("PATH だけを外す", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, []string{"A=1", "XPATH=2"}, withoutPath([]string{"A=1", "PATH=/bin", "XPATH=2"}))
	})
}

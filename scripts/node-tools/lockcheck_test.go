package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	fixturePackage = `{
  "dependencies": {
    "@commitlint/cli": "20.1.0",
    "markdownlint-cli2": "0.24.1"
  }
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

func declDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range map[string]string{
		"package.json":        fixturePackage,
		"pnpm-lock.yaml":      fixtureLock,
		"pnpm-workspace.yaml": fixtureWorkspace,
	} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600))
	}

	return dir
}

// selfScripts は、canary が self の代わりに起動する台本。$3 が写しのディレクトリ。
var selfScripts = map[string]string{
	"ok": `case "$3" in
*/drift) echo ERR_PNPM_OUTDATED_LOCKFILE; exit 1;;
*/integrity) echo ERR_PNPM_TARBALL_INTEGRITY; exit 1;;
esac
exit 1
`,
	"fallback": "markdownlint-cli2; exit 1\n",
	"launched": "exit 0\n",
	"reason":   "echo ERR_PNPM_FETCH_404; exit 1\n",
}

func canaryDeps(t *testing.T, mode string, f *fakeTools, stderr io.Writer) (deps, string) {
	t.Helper()
	self := filepath.Join(t.TempDir(), "node-tools")
	writeExecutable(t, self, selfScripts[mode])
	wd := t.TempDir()
	d := testDeps(f, stderr)
	d.self = func() (string, error) { return self, nil }
	d.wd = func() (string, error) { return wd, nil }

	return d, wd
}

func Test_runLockCheck(t *testing.T) {
	t.Parallel()

	t.Run("canary が通れば lockfile を照合する", func(t *testing.T) {
		t.Parallel()
		f := &fakeTools{scripts: map[string]string{stepLockCheck: "true"}}
		var stderr bytes.Buffer
		d, _ := canaryDeps(t, "ok", f, &stderr)
		require.NoError(t, runLockCheck([]string{"-dir", declDir(t)}, d))
		assert.Equal(t, []string{stepLockCheck}, f.called())
		assert.Contains(t, stderr.String(), "✅ node-tools canary")
	})
	t.Run("canary が落ちれば照合しない", func(t *testing.T) {
		t.Parallel()
		f := &fakeTools{scripts: map[string]string{stepLockCheck: "true"}}
		d, _ := canaryDeps(t, "launched", f, io.Discard)
		require.ErrorIs(t, runLockCheck([]string{"-dir", declDir(t)}, d), errCanaryLaunched)
		assert.Empty(t, f.called())
	})
	t.Run("照合が落ちれば落ちる", func(t *testing.T) {
		t.Parallel()
		f := &fakeTools{scripts: map[string]string{stepLockCheck: "echo ERR_PNPM_OUTDATED_LOCKFILE >&2; exit 1"}}
		var stderr bytes.Buffer
		d, _ := canaryDeps(t, "ok", f, &stderr)
		require.Error(t, runLockCheck([]string{"-dir", declDir(t)}, d))
		assert.Contains(t, stderr.String(), "ERR_PNPM_OUTDATED_LOCKFILE")
	})
	for name, args := range map[string][]string{
		"-dir が無い":   nil,
		"余分な引数がある":   {"-dir", "d", "x"},
		"知らないフラグがある": {"-x"},
	} {
		t.Run(name+"なら usage で落ちる", func(t *testing.T) {
			t.Parallel()
			require.ErrorIs(t, runLockCheck(args, testDeps(&fakeTools{}, io.Discard)), errLockCheckUsage)
		})
	}
}

func Test_runCanary(t *testing.T) {
	t.Parallel()

	t.Run("どのケースも期待の理由で落ちれば通り、一時ディレクトリを残さない", func(t *testing.T) {
		t.Parallel()
		d, wd := canaryDeps(t, "ok", &fakeTools{}, io.Discard)
		require.NoError(t, runCanary(declDir(t), d))
		left, err := os.ReadDir(filepath.Join(wd, "tmp"))
		require.NoError(t, err)
		assert.Empty(t, left)
	})
	for mode, want := range map[string]error{
		"fallback": errCanaryFallback,
		"launched": errCanaryLaunched,
		"reason":   errCanaryReason,
	} {
		t.Run(mode+" の振る舞いを落とす", func(t *testing.T) {
			t.Parallel()
			var stderr bytes.Buffer
			d, _ := canaryDeps(t, mode, &fakeTools{}, &stderr)
			require.ErrorIs(t, runCanary(declDir(t), d), want)
		})
	}
	t.Run("宣言の形が変わって書き換えが当たらなければ落ちる", func(t *testing.T) {
		t.Parallel()
		dir := declDir(t)
		require.NoError(t, os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"dependencies": {}}`), 0o600))
		d, _ := canaryDeps(t, "ok", &fakeTools{}, io.Discard)
		require.ErrorIs(t, runCanary(dir, d), errCanaryShape)
	})
	t.Run("宣言のファイルが欠けていれば落ちる", func(t *testing.T) {
		t.Parallel()
		dir := declDir(t)
		require.NoError(t, os.Remove(filepath.Join(dir, "pnpm-workspace.yaml")))
		d, _ := canaryDeps(t, "ok", &fakeTools{}, io.Discard)
		require.ErrorIs(t, runCanary(dir, d), os.ErrNotExist)
	})
	t.Run("作業ディレクトリを取れなければ落ちる", func(t *testing.T) {
		t.Parallel()
		d, _ := canaryDeps(t, "ok", &fakeTools{}, io.Discard)
		sentinel := errors.New("wd")
		d.wd = func() (string, error) { return "", sentinel }
		require.ErrorIs(t, runCanary(declDir(t), d), sentinel)
	})
	t.Run("自分の実行ファイルを取れなければ落ちる", func(t *testing.T) {
		t.Parallel()
		d, _ := canaryDeps(t, "ok", &fakeTools{}, io.Discard)
		sentinel := errors.New("self")
		d.self = func() (string, error) { return "", sentinel }
		require.ErrorIs(t, runCanary(declDir(t), d), sentinel)
	})
}

func Test_canaryCommand(t *testing.T) {
	t.Parallel()

	t.Run("自分の exec を写しと canary の lock で起動する", func(t *testing.T) {
		t.Parallel()
		c := canaryCommand("/bin/node-tools", "/w/drift", "/w/lock")
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
		})
	}
}

func Test_writeFake(t *testing.T) {
	t.Parallel()

	t.Run("同名の偽の道具を実行できる形で置く", func(t *testing.T) {
		t.Parallel()
		bin := t.TempDir()
		require.NoError(t, writeFake(bin))
		p := filepath.Join(bin, canaryTool)
		info, err := os.Stat(p)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o500), info.Mode().Perm())
		b, err := os.ReadFile(p) //nolint:gosec // t.TempDir 配下
		require.NoError(t, err)
		assert.Contains(t, string(b), canaryMarker)
	})
	t.Run("置き場が無ければ落ちる", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, writeFake(filepath.Join(t.TempDir(), "none")), os.ErrNotExist)
	})
}

func Test_copyDecl(t *testing.T) {
	t.Parallel()

	t.Run("1組を写し、対象のファイルだけを壊す", func(t *testing.T) {
		t.Parallel()
		src := declDir(t)
		dst := filepath.Join(t.TempDir(), "drift")
		require.NoError(t, copyDecl(src, dst, canaryCases[0]))
		for _, name := range []string{"pnpm-lock.yaml", "pnpm-workspace.yaml"} {
			want, err := os.ReadFile(filepath.Join(src, name)) //nolint:gosec // t.TempDir 配下
			require.NoError(t, err)
			got, err := os.ReadFile(filepath.Join(dst, name)) //nolint:gosec // t.TempDir 配下
			require.NoError(t, err)
			assert.Equal(t, string(want), string(got))
		}
		got, err := os.ReadFile(filepath.Join(dst, "package.json")) //nolint:gosec // t.TempDir 配下
		require.NoError(t, err)
		assert.Contains(t, string(got), `"markdownlint-cli2": "0.0.0"`)
	})
	t.Run("写し先が既に在れば落ちる", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, copyDecl(declDir(t), t.TempDir(), canaryCases[0]), os.ErrExist)
	})
	t.Run("写し元が無ければ落ちる", func(t *testing.T) {
		t.Parallel()
		dst := filepath.Join(t.TempDir(), "x")
		require.ErrorIs(t, copyDecl(filepath.Join(t.TempDir(), "none"), dst, canaryCases[0]), os.ErrNotExist)
	})
}

func Test_mutateDrift(t *testing.T) {
	t.Parallel()

	t.Run("版を 0.0.0 にずらし、他の行を変えない", func(t *testing.T) {
		t.Parallel()
		got, err := mutateDrift([]byte(fixturePackage))
		require.NoError(t, err)
		assert.Contains(t, string(got), `"markdownlint-cli2": "0.0.0"`)
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
		"項目が無い":              "packages:\n",
		"次の行に integrity が無い": "  markdownlint-cli2@1.0.0:\n    dependencies:\n",
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

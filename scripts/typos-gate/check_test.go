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

// passingFake は、canary と範囲の突き合わせが成り立つ偽の道具です。
func passingFake(t *testing.T) *fake {
	t.Helper()
	f := newFake(t)
	f.set(stepScan, "out", hiddenHit)
	f.set(stepScan, "rc", "2")
	f.set(stepFiles, "out", "./a.md\n./.github/w.yaml\n")
	f.set(stepLsFiles, "out", "a.md\x00.github/w.yaml\x00")
	return f
}

// checkArgs は、設定を置いた一時ディレクトリをリポジトリ直下（root）に見立て、check の引数と root を返します。
func checkArgs(t *testing.T) ([]string, string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	cfg := writeFile(t, filepath.Join(root, ".typos.toml"), coverageConfig)
	return []string{"-work", "tmp/typos-gate", "-config", cfg}, root
}

// checkWith は、checkArgs の引数で runCheck を走らせます。
func checkWith(t *testing.T, out *bytes.Buffer, tool toolFunc, env envFunc) error {
	t.Helper()
	args, root := checkArgs(t)
	return runCheck(args, out, tool, env, root)
}

func steps(calls []string) []string {
	var out []string
	for _, c := range calls {
		out = append(out, strings.Fields(c)[1])
	}
	return out
}

func Test_runCheck(t *testing.T) {
	t.Parallel()

	t.Run("canary・範囲の突き合わせ・本走査の順に走り、本走査が 0 なら通る", func(t *testing.T) {
		t.Parallel()
		f := passingFake(t)
		var out bytes.Buffer
		require.NoError(t, checkWith(t, &out, f.tool(), isolated))
		assert.Equal(t, []string{stepScan, stepFiles, stepFiles, stepLsFiles, stepMain}, steps(f.calls()))
		assert.Contains(t, out.String(), "✅ typos canary")
		assert.Contains(t, out.String(), "✅ typos coverage")
		assert.Contains(t, out.String(), "✅ typos: 綴りの誤りはありません")
	})

	t.Run("canary が成り立たなければ、本走査を起動せずに errCanary", func(t *testing.T) {
		t.Parallel()
		f := passingFake(t)
		f.set(stepScan, "rc", "0")
		err := checkWith(t, new(bytes.Buffer), f.tool(), isolated)
		require.ErrorIs(t, err, errCanary)
		assert.NotContains(t, steps(f.calls()), stepMain)
		assert.NotContains(t, steps(f.calls()), stepLsFiles)
	})

	t.Run("範囲の突き合わせが落ちれば、本走査を起動せずに errUncovered", func(t *testing.T) {
		t.Parallel()
		f := passingFake(t)
		f.set(stepLsFiles, "out", "a.md\x00b.md\x00")
		err := checkWith(t, new(bytes.Buffer), f.tool(), isolated)
		require.ErrorIs(t, err, errUncovered)
		assert.NotContains(t, steps(f.calls()), stepMain)
	})

	t.Run("道具がどの段でも起動できなければ errTool", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, checkWith(t, new(bytes.Buffer), missingTool, isolated), errTool)
	})

	t.Run("本走査だけが起動できなければ errTool", func(t *testing.T) {
		t.Parallel()
		f := passingFake(t)
		f.set(stepMain, "kill", "")
		require.ErrorIs(t, checkWith(t, new(bytes.Buffer), f.tool(), isolated), errTool)
	})

	t.Run("設定が無い・空白だけなら道具を起動せずに errRead", func(t *testing.T) {
		t.Parallel()
		for name, body := range map[string]*string{"無い": nil, "空白だけ": new(" \n\t\n")} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				dir := t.TempDir()
				cfg := filepath.Join(dir, ".typos.toml")
				if body != nil {
					writeFile(t, cfg, *body)
				}
				f := passingFake(t)
				err := runCheck([]string{"-work", "tmp/w", "-config", cfg}, new(bytes.Buffer), f.tool(), isolated, dir)
				require.ErrorIs(t, err, errRead)
				assert.Empty(t, f.calls())
			})
		}
	})

	t.Run("-work が tmp/ の下でなければ、何も消さず道具も起動せずに errUsage", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		keep := writeFile(t, filepath.Join(root, "keep", "f"), "")
		cfg := writeFile(t, filepath.Join(root, ".typos.toml"), coverageConfig)
		f := passingFake(t)
		for _, work := range []string{root + "/keep", "keep", "tmp", "tmp/../keep"} {
			err := runCheck([]string{"-work", work, "-config", cfg}, new(bytes.Buffer), f.tool(), isolated, root)
			require.ErrorIs(t, err, errUsage, work)
		}
		assert.FileExists(t, keep)
		assert.Empty(t, f.calls())
	})

	t.Run("ほかの引数が正しくても、1つ欠けるか余れば errUsage", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		cfg := writeFile(t, filepath.Join(root, ".typos.toml"), coverageConfig)
		for name, args := range map[string][]string{
			"引数が無い":       nil,
			"-config が無い": {"-work", "tmp/w"},
			"-work が無い":   {"-config", cfg},
			"余分な引数":       {"-work", "tmp/w", "-config", cfg, "x"},
			"知らない flag":   {"-work", "tmp/w", "-config", cfg, "-bogus"},
		} {
			f := passingFake(t)
			require.ErrorIs(t, runCheck(args, new(bytes.Buffer), f.tool(), isolated, root), errUsage, name)
			assert.Empty(t, f.calls(), name)
		}
		require.NoError(t, runCheck([]string{"-work", "tmp/w", "-config", cfg}, new(bytes.Buffer), passingFake(t).tool(), isolated, root), "対照: 欠けも余りも無ければ通る")
	})

	t.Run("fixture を root の tmp の下に建て、終わりに消す", func(t *testing.T) {
		t.Parallel()
		args, root := checkArgs(t)
		f := passingFake(t)
		require.NoError(t, runCheck(args, new(bytes.Buffer), f.tool(), isolated, root))
		calls := f.calls()
		require.NotEmpty(t, calls)
		assert.Equal(t, filepath.Join(root, "tmp", "typos-gate")+" "+stepScan+" ", calls[0])
		assert.NoDirExists(t, filepath.Join(root, "tmp", "typos-gate"))
	})

	t.Run("-config の値を範囲の突き合わせの自身の除外へ渡す", func(t *testing.T) {
		t.Parallel()
		args, root := checkArgs(t)
		f := passingFake(t)
		f.set(stepLsFiles, "out", "a.md\x00.github/w.yaml\x00"+args[3]+"\x00")
		require.NoError(t, runCheck(args, new(bytes.Buffer), f.tool(), isolated, root))
	})
}

func Test_workDir(t *testing.T) {
	t.Parallel()

	for _, work := range []string{"tmp/x", "./tmp/x", "tmp/x/y", "tmp/x/"} {
		t.Run(work+" は root の下へ解決する", func(t *testing.T) {
			t.Parallel()
			got, err := workDir("/r", work)
			require.NoError(t, err)
			want := "/r/tmp/x"
			if strings.Contains(work, "x/y") {
				want = "/r/tmp/x/y"
			}
			assert.Equal(t, want, got)
		})
	}

	for _, work := range []string{"", "/tmp/x", "tmp", "tmp/", "tmp/.", "./tmp", "tmp/../x", "tmp/x/../../y", "tmp/x/..", "x/tmp/y", "tmpx/y", "../tmp/x"} {
		t.Run(work+" は errUsage", func(t *testing.T) {
			t.Parallel()
			_, err := workDir("/r", work)
			require.ErrorIs(t, err, errUsage)
		})
	}

	// root と外を、symlink を解いた実パスで建てる。
	realDir := func(t *testing.T) string {
		t.Helper()
		d, err := filepath.EvalSymlinks(t.TempDir())
		require.NoError(t, err)
		return d
	}

	t.Run("tmp/ の下の symlink が外を指せば、その下も自身も errUsage で、外は消さない", func(t *testing.T) {
		t.Parallel()
		root, outside := realDir(t), realDir(t)
		require.NoError(t, os.MkdirAll(filepath.Join(root, "tmp"), 0o750))
		require.NoError(t, os.Symlink(outside, filepath.Join(root, "tmp", "link")))
		for _, work := range []string{"tmp/link/x", "tmp/link", "tmp/link/x/y"} {
			_, err := workDir(root, work)
			require.ErrorIs(t, err, errUsage, work)
		}
	})

	t.Run("tmp/ の下の symlink が tmp/ の下を指せば通る", func(t *testing.T) {
		t.Parallel()
		root := realDir(t)
		require.NoError(t, os.MkdirAll(filepath.Join(root, "tmp", "real"), 0o750))
		require.NoError(t, os.Symlink(filepath.Join(root, "tmp", "real"), filepath.Join(root, "tmp", "link")))
		got, err := workDir(root, "tmp/link/x")
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(root, "tmp", "link", "x"), got)
	})

	t.Run("在る tmp/ の下の、まだ無い -work は通る", func(t *testing.T) {
		t.Parallel()
		root := realDir(t)
		require.NoError(t, os.MkdirAll(filepath.Join(root, "tmp"), 0o750))
		got, err := workDir(root, "tmp/a/b")
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(root, "tmp", "a", "b"), got)
	})

	t.Run("tmp が symlink なら、その先を tmp/ として扱う", func(t *testing.T) {
		t.Parallel()
		root, elsewhere := realDir(t), realDir(t)
		require.NoError(t, os.Symlink(elsewhere, filepath.Join(root, "tmp")))
		_, err := workDir(root, "tmp/x")
		require.NoError(t, err)
	})

	t.Run("tmp が壊れた symlink なら errUsage", func(t *testing.T) {
		t.Parallel()
		root := realDir(t)
		require.NoError(t, os.Symlink(filepath.Join(root, "none"), filepath.Join(root, "tmp")))
		_, err := workDir(root, "tmp/x")
		require.ErrorIs(t, err, errUsage)
	})

	t.Run("tmp の親（リポジトリ直下）を指す symlink の下も自身も errUsage", func(t *testing.T) {
		t.Parallel()
		root := realDir(t)
		require.NoError(t, os.MkdirAll(filepath.Join(root, "tmp"), 0o750))
		require.NoError(t, os.Symlink(root, filepath.Join(root, "tmp", "up")))
		for _, work := range []string{"tmp/up/x", "tmp/up"} {
			_, err := workDir(root, work)
			require.ErrorIs(t, err, errUsage, work)
		}
	})

	t.Run("既に在る tmp/x は通る", func(t *testing.T) {
		t.Parallel()
		root := realDir(t)
		require.NoError(t, os.MkdirAll(filepath.Join(root, "tmp", "x"), 0o750))
		got, err := workDir(root, "tmp/x")
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(root, "tmp", "x"), got)
	})

	t.Run("外を指す壊れた symlink の下も自身も errUsage", func(t *testing.T) {
		t.Parallel()
		root, outside := realDir(t), realDir(t)
		require.NoError(t, os.MkdirAll(filepath.Join(root, "tmp"), 0o750))
		require.NoError(t, os.Symlink(filepath.Join(outside, "none"), filepath.Join(root, "tmp", "dangling")))
		for _, work := range []string{"tmp/dangling/x", "tmp/dangling"} {
			_, err := workDir(root, work)
			require.ErrorIs(t, err, errUsage, work)
		}
	})

	t.Run("root がファイルで tmp を調べられなければ errUsage", func(t *testing.T) {
		t.Parallel()
		file := filepath.Join(realDir(t), "file")
		require.NoError(t, os.WriteFile(file, nil, 0o600))
		_, err := workDir(file, "tmp/x")
		require.ErrorIs(t, err, errUsage)
	})

	t.Run("相対の root でも、symlink の無い tmp の下へ解決する", func(t *testing.T) {
		t.Parallel()
		root := realDir(t)
		require.NoError(t, os.MkdirAll(filepath.Join(root, "tmp"), 0o750))
		cwd, err := os.Getwd()
		require.NoError(t, err)
		rel, err := filepath.Rel(cwd, root)
		require.NoError(t, err)
		got, err := workDir(rel, "tmp/x")
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(rel, "tmp", "x"), got)
	})

	t.Run("-work の途中がファイルなら errUsage", func(t *testing.T) {
		t.Parallel()
		root := realDir(t)
		require.NoError(t, os.MkdirAll(filepath.Join(root, "tmp"), 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(root, "tmp", "f"), nil, 0o600))
		_, err := workDir(root, "tmp/f/x")
		require.ErrorIs(t, err, errUsage)
	})
}

func Test_staysUnder(t *testing.T) {
	t.Parallel()

	t.Run("相対パスの base と target も絶対パスへ直して比べ、外を指す symlink を落とし、中を指す symlink を通す", func(t *testing.T) {
		t.Parallel()
		root, err := filepath.EvalSymlinks(t.TempDir())
		require.NoError(t, err)
		outside := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, "tmp"), 0o750))
		require.NoError(t, os.Symlink(outside, filepath.Join(root, "tmp", "link")))
		cwd, err := os.Getwd()
		require.NoError(t, err)
		rel, err := filepath.Rel(cwd, root)
		require.NoError(t, err)
		require.Error(t, staysUnder(filepath.Join(rel, "tmp"), filepath.Join(rel, "tmp", "link", "x")))
		require.NoError(t, os.MkdirAll(filepath.Join(root, "tmp", "real"), 0o750))
		require.NoError(t, os.Symlink(filepath.Join(root, "tmp", "real"), filepath.Join(root, "tmp", "in")))
		require.NoError(t, staysUnder(filepath.Join(rel, "tmp"), filepath.Join(rel, "tmp", "in", "x")), "絶対パスを指す中の symlink は通る")
	})

	t.Run("base の親を指す symlink の下も自身も落ちる", func(t *testing.T) {
		t.Parallel()
		root, err := filepath.EvalSymlinks(t.TempDir())
		require.NoError(t, err)
		base := filepath.Join(root, "tmp")
		require.NoError(t, os.MkdirAll(base, 0o750))
		require.NoError(t, os.Symlink(root, filepath.Join(base, "up")))
		require.Error(t, staysUnder(base, filepath.Join(base, "up", "x")))
		require.Error(t, staysUnder(base, filepath.Join(base, "up")))
	})

	t.Run("既に在る base の下のディレクトリは通る", func(t *testing.T) {
		t.Parallel()
		base, err := filepath.EvalSymlinks(t.TempDir())
		require.NoError(t, err)
		require.NoError(t, os.MkdirAll(filepath.Join(base, "x"), 0o750))
		require.NoError(t, staysUnder(base, filepath.Join(base, "x")))
	})

	t.Run("外を指す壊れた symlink の下も自身も落ちる", func(t *testing.T) {
		t.Parallel()
		base, err := filepath.EvalSymlinks(t.TempDir())
		require.NoError(t, err)
		require.NoError(t, os.Symlink(filepath.Join(t.TempDir(), "none"), filepath.Join(base, "dangling")))
		require.Error(t, staysUnder(base, filepath.Join(base, "dangling", "x")))
		require.Error(t, staysUnder(base, filepath.Join(base, "dangling")))
	})

	t.Run("base を調べられなければ（ENOTDIR）落ちる", func(t *testing.T) {
		t.Parallel()
		file := filepath.Join(t.TempDir(), "file")
		require.NoError(t, os.WriteFile(file, nil, 0o600))
		require.Error(t, staysUnder(filepath.Join(file, "tmp"), filepath.Join(file, "tmp", "x")))
	})

	t.Run("base が無ければ確かめない", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, staysUnder(filepath.Join(t.TempDir(), "none"), "/anything"))
	})

	t.Run("在る祖先が base 自身なら通り、target 自身が base を指す symlink なら落ちる", func(t *testing.T) {
		t.Parallel()
		base, err := filepath.EvalSymlinks(t.TempDir())
		require.NoError(t, err)
		require.NoError(t, staysUnder(base, filepath.Join(base, "x", "y")))
		self := filepath.Join(base, "self")
		require.NoError(t, os.Symlink(base, self))
		require.Error(t, staysUnder(base, self))
	})
}

func Test_runScan(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		rc   string
		want error
	}{
		"rc=0 は通る":                      {"0", nil},
		"rc=2（誤綴り）は errFailed":          {"2", errFailed},
		"rc=1（shim が版を決められない）は errTool": {"1", errTool},
		"rc=78（設定を読めない）は errTool":       {"78", errTool},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newFake(t)
			f.set(stepMain, "out", "OUT\n")
			f.set(stepMain, "err", "STDERR-ONLY\n")
			f.set(stepMain, "rc", c.rc)
			var out bytes.Buffer
			err := runScan(&out, f.tool())
			if c.want == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, c.want)
			}
			assert.Contains(t, out.String(), "OUT\n", "道具の出力を写す")
			assert.Contains(t, out.String(), "STDERR-ONLY", "stderr も写す")
			assert.Equal(t, []string{realCwd(t) + " " + stepMain + " "}, f.calls(), "本走査はリポジトリ直下（cwd）で走る")
		})
	}

	t.Run("起動できなければ errTool", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, runScan(new(bytes.Buffer), missingTool), errTool)
	})
}

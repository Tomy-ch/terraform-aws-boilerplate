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

// stepsOf は、偽の道具の起動の記録から「作業ディレクトリの末尾 段」を並べます。
func stepsOf(calls []string) []string {
	var out []string
	for _, c := range calls {
		f := strings.Fields(c)
		out = append(out, filepath.Base(f[0])+" "+f[1])
	}
	return out
}

// passingFake は、2つの canary と設定の確認が成り立ち、本走査が 0 で終わる偽の道具です。canary と本走査は同じ段
// （check）なので、作業ディレクトリ（check・directive）で出し分けます。
func passingFake(t *testing.T) *fake {
	t.Helper()
	f := newFake(t)
	f.set(stepCheck+"@check", "out", checkHit)
	f.set(stepCheck+"@check", "rc", "1")
	f.set(stepCheck+"@directive", "out", controlHit)
	f.set(stepCheck+"@directive", "rc", "1")
	f.set(stepDryRun, "out", "a.yaml\n")
	return f
}

// checkCase は、一時ディレクトリをリポジトリ直下（root）に見立てた check の引数と、-work の実パスです。
type checkCase struct {
	args       []string
	root, work string
}

func checkArgs(t *testing.T) checkCase {
	t.Helper()
	p := canaryPaths(t)
	root := filepath.Dir(p.work)
	return checkCase{
		args: []string{"-work", "tmp/editorconfig-gate", "-editorconfig", p.editorconfig, "-checker-config", p.checkerConfig},
		root: root, work: filepath.Join(root, "tmp", "editorconfig-gate"),
	}
}

func (c checkCase) run(out *bytes.Buffer, tool toolFunc, env envFunc) error {
	return runCheck(c.args, out, tool, env, c.root)
}

func Test_runCheck(t *testing.T) {
	t.Parallel()

	t.Run("2つの canary・設定の確認・本走査の順に走り、本走査が 0 なら通って fixture を消す", func(t *testing.T) {
		t.Parallel()
		f := passingFake(t)
		c := checkArgs(t)
		work := c.work
		var out bytes.Buffer
		require.NoError(t, c.run(&out, f.tool(), isolated))
		assert.Equal(t, []string{
			"check git-init", "check git-add", "check check",
			"directive git-init", "directive git-add", "directive check",
			"editorconfig-gate dry-run", "editorconfig-gate check",
		}, stepsOf(f.calls()))
		for _, want := range []string{"✅ editorconfig check-canary", "✅ editorconfig directive-canary", "editorconfig-check: 1 ファイル", "✅ editorconfig-check:"} {
			assert.Contains(t, out.String(), want)
		}
		assert.NoDirExists(t, work)
	})

	t.Run("check-canary が成り立たなければ、後の段を起動せずに errCanary", func(t *testing.T) {
		t.Parallel()
		f := passingFake(t)
		f.set(stepCheck+"@check", "rc", "0")
		require.ErrorIs(t, checkArgs(t).run(new(bytes.Buffer), f.tool(), isolated), errCanary)
		assert.Equal(t, []string{"check git-init", "check git-add", "check check"}, stepsOf(f.calls()))
	})

	t.Run("directive-canary が成り立たなければ、設定の確認も本走査も起動せずに errCanary", func(t *testing.T) {
		t.Parallel()
		f := passingFake(t)
		f.set(stepCheck+"@directive", "out", checkHit+controlHit)
		require.ErrorIs(t, checkArgs(t).run(new(bytes.Buffer), f.tool(), isolated), errCanary)
		assert.NotContains(t, stepsOf(f.calls()), "editorconfig-gate dry-run")
		assert.NotContains(t, stepsOf(f.calls()), "editorconfig-gate check")
	})

	t.Run("走査の対象が0件なら、本走査を起動せずに errNoInput", func(t *testing.T) {
		t.Parallel()
		f := passingFake(t)
		f.set(stepDryRun, "out", "")
		require.ErrorIs(t, checkArgs(t).run(new(bytes.Buffer), f.tool(), isolated), errNoInput)
		assert.NotContains(t, stepsOf(f.calls()), "editorconfig-gate check")
	})

	t.Run("本走査が違反で落ちれば errFailed", func(t *testing.T) {
		t.Parallel()
		f := passingFake(t)
		f.set(stepCheck+"@editorconfig-gate", "rc", "1")
		require.ErrorIs(t, checkArgs(t).run(new(bytes.Buffer), f.tool(), isolated), errFailed)
	})

	t.Run("editorconfig-checker がシグナルで終われば errTool", func(t *testing.T) {
		t.Parallel()
		f := passingFake(t)
		f.set(stepCheck, "kill", "")
		require.ErrorIs(t, checkArgs(t).run(new(bytes.Buffer), f.tool(), isolated), errTool)
	})

	t.Run("git の隔離の一覧を得られなければ、道具を1つも起動せずに errTool", func(t *testing.T) {
		t.Parallel()
		f := passingFake(t)
		c := checkArgs(t)
		work := c.work
		require.ErrorIs(t, c.run(new(bytes.Buffer), f.tool(), failingEnv), errTool)
		assert.Empty(t, f.calls())
		assert.NoDirExists(t, work)
	})

	t.Run("道具がどの段でも起動できなければ errTool", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, checkArgs(t).run(new(bytes.Buffer), missingTool, isolated), errTool)
	})

	t.Run("-work が tmp/ の下でなければ、何も消さず道具も起動せずに errUsage", func(t *testing.T) {
		t.Parallel()
		c := checkArgs(t)
		keep := writeFile(t, filepath.Join(c.root, "keep", "f"), "")
		f := passingFake(t)
		for _, work := range []string{c.root + "/keep", "keep", "tmp", "tmp/../keep"} {
			args := append([]string{"-work", work}, c.args[2:]...)
			require.ErrorIs(t, runCheck(args, new(bytes.Buffer), f.tool(), isolated, c.root), errUsage, work)
		}
		assert.FileExists(t, keep)
		assert.Empty(t, f.calls())
	})

	t.Run("git-init だけがシグナルで終われば errTool", func(t *testing.T) {
		t.Parallel()
		f := passingFake(t)
		f.set(stepGitInit, "kill", "")
		require.ErrorIs(t, checkArgs(t).run(new(bytes.Buffer), f.tool(), isolated), errTool)
		assert.Equal(t, []string{"check git-init"}, stepsOf(f.calls()))
	})

	t.Run("ほかの引数が正しくても、1つ欠けるか余れば errUsage", func(t *testing.T) {
		t.Parallel()
		c := checkArgs(t)
		ec, cc := c.args[3], c.args[5]
		for name, args := range map[string][]string{
			"引数が無い":               nil,
			"-checker-config が無い": {"-work", "tmp/w", "-editorconfig", ec},
			"-editorconfig が無い":   {"-work", "tmp/w", "-checker-config", cc},
			"-work が無い":           {"-editorconfig", ec, "-checker-config", cc},
			"余分な引数":               {"-work", "tmp/w", "-editorconfig", ec, "-checker-config", cc, "x"},
			"知らない flag":           {"-work", "tmp/w", "-editorconfig", ec, "-checker-config", cc, "-bogus"},
		} {
			f := passingFake(t)
			require.ErrorIs(t, runCheck(args, new(bytes.Buffer), f.tool(), isolated, c.root), errUsage, name)
			assert.Empty(t, f.calls(), name)
		}
		require.NoError(t, runCheck([]string{"-work", "tmp/w", "-editorconfig", ec, "-checker-config", cc}, new(bytes.Buffer), passingFake(t).tool(), isolated, c.root), "対照: 欠けも余りも無ければ通る")
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

	t.Run("リポジトリ直下で隔離した env の本走査を起動し、0 なら通って出力を写す", func(t *testing.T) {
		t.Parallel()
		f := newFake(t)
		f.set(stepCheck, "out", "OUT\n")
		f.set(stepCheck, "err", "STDERR-ONLY\n")
		var out bytes.Buffer
		require.NoError(t, runScan(&out, f.tool(), isolatedEnv))
		assert.Contains(t, out.String(), "OUT\n")
		assert.Contains(t, out.String(), "STDERR-ONLY")
		assert.Contains(t, out.String(), "✅ editorconfig-check")
		calls := f.calls()
		require.Len(t, calls, 1)
		assert.True(t, strings.HasSuffix(calls[0], " "+stepCheck+" /isolated"), calls[0])
	})

	t.Run("rc≠0 なら出力を写して errFailed", func(t *testing.T) {
		t.Parallel()
		f := newFake(t)
		f.set(stepCheck, "out", checkHit)
		f.set(stepCheck, "err", "STDERR-ONLY\n")
		f.set(stepCheck, "rc", "1")
		var out bytes.Buffer
		require.ErrorIs(t, runScan(&out, f.tool(), isolatedEnv), errFailed)
		assert.Contains(t, out.String(), "wrong indent")
		assert.Contains(t, out.String(), "STDERR-ONLY")
		assert.NotContains(t, out.String(), "✅")
	})

	t.Run("起動できなければ errTool", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, runScan(new(bytes.Buffer), missingTool, isolatedEnv), errTool)
	})
}

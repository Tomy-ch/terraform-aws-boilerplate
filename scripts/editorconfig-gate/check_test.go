package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun/tooltest"
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
func passingFake(t *testing.T) *tooltest.Fake {
	t.Helper()
	f := newFake(t)
	f.Set(stepCheck+"@check", "out", checkHit)
	f.Set(stepCheck+"@check", "rc", "1")
	f.Set(stepCheck+"@directive", "out", controlHit)
	f.Set(stepCheck+"@directive", "rc", "1")
	f.Set(stepDryRun, "out", "a.yaml\n")
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
		require.NoError(t, c.run(&out, f.Tool(), isolated))
		for _, call := range f.Calls() {
			assert.True(t, strings.HasSuffix(call, " /isolated"), "どの段も隔離した env を受け取る: %s", call)
		}
		assert.Equal(t, []string{
			"check git-init", "check git-add", "check check",
			"directive git-init", "directive git-add", "directive check",
			"editorconfig-gate dry-run", "editorconfig-gate check",
		}, stepsOf(f.Calls()))
		for _, want := range []string{"✅ editorconfig check-canary", "✅ editorconfig directive-canary", "editorconfig-check: 1 ファイル", "✅ editorconfig-check:"} {
			assert.Contains(t, out.String(), want)
		}
		assert.NoDirExists(t, work)
	})

	t.Run("check-canary が成り立たなければ、後の段を起動せずに errCanary", func(t *testing.T) {
		t.Parallel()
		f := passingFake(t)
		f.Set(stepCheck+"@check", "rc", "0")
		require.ErrorIs(t, checkArgs(t).run(new(bytes.Buffer), f.Tool(), isolated), errCanary)
		assert.Equal(t, []string{"check git-init", "check git-add", "check check"}, stepsOf(f.Calls()))
	})

	t.Run("directive-canary が成り立たなければ、設定の確認も本走査も起動せずに errCanary", func(t *testing.T) {
		t.Parallel()
		f := passingFake(t)
		f.Set(stepCheck+"@directive", "out", checkHit+controlHit)
		require.ErrorIs(t, checkArgs(t).run(new(bytes.Buffer), f.Tool(), isolated), errCanary)
		assert.NotContains(t, stepsOf(f.Calls()), "editorconfig-gate dry-run")
		assert.NotContains(t, stepsOf(f.Calls()), "editorconfig-gate check")
	})

	t.Run("走査の対象が0件なら、本走査を起動せずに errNoInput", func(t *testing.T) {
		t.Parallel()
		f := passingFake(t)
		f.Set(stepDryRun, "out", "")
		require.ErrorIs(t, checkArgs(t).run(new(bytes.Buffer), f.Tool(), isolated), errNoInput)
		assert.NotContains(t, stepsOf(f.Calls()), "editorconfig-gate check")
	})

	t.Run("本走査が違反で落ちれば errFailed", func(t *testing.T) {
		t.Parallel()
		f := passingFake(t)
		f.Set(stepCheck+"@editorconfig-gate", "rc", "1")
		require.ErrorIs(t, checkArgs(t).run(new(bytes.Buffer), f.Tool(), isolated), errFailed)
	})

	t.Run("editorconfig-checker がシグナルで終われば errTool", func(t *testing.T) {
		t.Parallel()
		f := passingFake(t)
		f.Set(stepCheck, "kill", "")
		require.ErrorIs(t, checkArgs(t).run(new(bytes.Buffer), f.Tool(), isolated), errTool)
	})

	t.Run("git の隔離の一覧を得られなければ、道具を1つも起動せずに errTool", func(t *testing.T) {
		t.Parallel()
		f := passingFake(t)
		c := checkArgs(t)
		work := c.work
		require.ErrorIs(t, c.run(new(bytes.Buffer), f.Tool(), failingEnv), errTool)
		assert.Empty(t, f.Calls())
		assert.NoDirExists(t, work)
	})

	t.Run("道具がどの段でも起動できなければ errTool", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, checkArgs(t).run(new(bytes.Buffer), tooltest.Missing, isolated), errTool)
	})

	t.Run("-work が tmp/ の下でなければ、何も消さず道具も起動せずに errUsage", func(t *testing.T) {
		t.Parallel()
		c := checkArgs(t)
		keep := writeFile(t, filepath.Join(c.root, "keep", "f"), "")
		f := passingFake(t)
		for _, work := range []string{c.root + "/keep", "keep", "tmp", "tmp/../keep"} {
			args := append([]string{"-work", work}, c.args[2:]...)
			require.ErrorIs(t, runCheck(args, new(bytes.Buffer), f.Tool(), isolated, c.root), errUsage, work)
		}
		assert.FileExists(t, keep)
		assert.Empty(t, f.Calls())
	})

	t.Run("-work が不正なら、-work を名指す errUsage で終了コードは 2", func(t *testing.T) {
		t.Parallel()
		c := checkArgs(t)
		args := append([]string{"-work", "../x"}, c.args[2:]...)
		err := runCheck(args, new(bytes.Buffer), tooltest.Missing, isolated, c.root)
		require.ErrorIs(t, err, errUsage)
		assert.Contains(t, err.Error(), `-work "../x"`)
		assert.Equal(t, 2, exitCode(err))
	})

	t.Run("git-init だけがシグナルで終われば errTool", func(t *testing.T) {
		t.Parallel()
		f := passingFake(t)
		f.Set(stepGitInit, "kill", "")
		require.ErrorIs(t, checkArgs(t).run(new(bytes.Buffer), f.Tool(), isolated), errTool)
		assert.Equal(t, []string{"check git-init"}, stepsOf(f.Calls()))
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
			require.ErrorIs(t, runCheck(args, new(bytes.Buffer), f.Tool(), isolated, c.root), errUsage, name)
			assert.Empty(t, f.Calls(), name)
		}
		require.NoError(t, runCheck([]string{"-work", "tmp/w", "-editorconfig", ec, "-checker-config", cc}, new(bytes.Buffer), passingFake(t).Tool(), isolated, c.root), "対照: 欠けも余りも無ければ通る")
	})
}

func Test_runScan(t *testing.T) {
	t.Parallel()

	t.Run("リポジトリ直下で隔離した env の本走査を起動し、0 なら通って出力を写す", func(t *testing.T) {
		t.Parallel()
		f := newFake(t)
		f.Set(stepCheck, "out", "OUT\n")
		f.Set(stepCheck, "err", "STDERR-ONLY\n")
		var out bytes.Buffer
		require.NoError(t, runScan(&out, f.Tool(), isolatedEnv))
		assert.Contains(t, out.String(), "OUT\n")
		assert.Contains(t, out.String(), "STDERR-ONLY")
		assert.Contains(t, out.String(), "✅ editorconfig-check")
		calls := f.Calls()
		require.Len(t, calls, 1)
		assert.True(t, strings.HasSuffix(calls[0], " "+stepCheck+" /isolated"), calls[0])
	})

	t.Run("rc≠0 なら出力を写して errFailed", func(t *testing.T) {
		t.Parallel()
		f := newFake(t)
		f.Set(stepCheck, "out", checkHit)
		f.Set(stepCheck, "err", "STDERR-ONLY\n")
		f.Set(stepCheck, "rc", "1")
		var out bytes.Buffer
		require.ErrorIs(t, runScan(&out, f.Tool(), isolatedEnv), errFailed)
		assert.Contains(t, out.String(), "wrong indent")
		assert.Contains(t, out.String(), "STDERR-ONLY")
		assert.NotContains(t, out.String(), "✅")
	})

	t.Run("起動できなければ errTool", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, runScan(new(bytes.Buffer), tooltest.Missing, isolatedEnv), errTool)
	})
}

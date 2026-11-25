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

func passingFake(t *testing.T) *tooltest.Fake {
	t.Helper()
	f := tooltest.New(t)
	f.Set(stepScan, "out", hiddenHit)
	f.Set(stepScan, "rc", "2")
	f.Set(stepFiles, "out", "./a.md\n./.github/w.yaml\n")
	f.Set(stepLsFiles, "out", "a.md\x00.github/w.yaml\x00")
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
		require.NoError(t, checkWith(t, &out, f.Tool(), isolated))
		assert.Equal(t, []string{stepScan, stepFiles, stepFiles, stepLsFiles, stepLsDeleted, stepMain}, steps(f.Calls()))
		assert.Contains(t, out.String(), "✅ typos canary")
		assert.Contains(t, out.String(), "✅ typos coverage")
		assert.Contains(t, out.String(), "✅ typos: 綴りの誤りはありません")
	})

	t.Run("canary が成り立たなければ、本走査を起動せずに errCanary", func(t *testing.T) {
		t.Parallel()
		f := passingFake(t)
		f.Set(stepScan, "rc", "0")
		err := checkWith(t, new(bytes.Buffer), f.Tool(), isolated)
		require.ErrorIs(t, err, errCanary)
		assert.NotContains(t, steps(f.Calls()), stepMain)
		assert.NotContains(t, steps(f.Calls()), stepLsFiles)
	})

	t.Run("範囲の突き合わせが落ちれば、本走査を起動せずに errUncovered", func(t *testing.T) {
		t.Parallel()
		f := passingFake(t)
		f.Set(stepLsFiles, "out", "a.md\x00b.md\x00")
		err := checkWith(t, new(bytes.Buffer), f.Tool(), isolated)
		require.ErrorIs(t, err, errUncovered)
		assert.NotContains(t, steps(f.Calls()), stepMain)
	})

	t.Run("道具がどの段でも起動できなければ errTool", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, checkWith(t, new(bytes.Buffer), tooltest.Missing, isolated), errTool)
	})

	t.Run("本走査だけが起動できなければ errTool", func(t *testing.T) {
		t.Parallel()
		f := passingFake(t)
		f.Set(stepMain, "kill", "")
		require.ErrorIs(t, checkWith(t, new(bytes.Buffer), f.Tool(), isolated), errTool)
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
				err := runCheck([]string{"-work", "tmp/w", "-config", cfg}, new(bytes.Buffer), f.Tool(), isolated, dir)
				require.ErrorIs(t, err, errRead)
				assert.Empty(t, f.Calls())
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
			err := runCheck([]string{"-work", work, "-config", cfg}, new(bytes.Buffer), f.Tool(), isolated, root)
			require.ErrorIs(t, err, errUsage, work)
		}
		assert.FileExists(t, keep)
		assert.Empty(t, f.Calls())
	})

	t.Run("-work が不正なら、-work を名指す errUsage で終了コードは 2", func(t *testing.T) {
		t.Parallel()
		args, root := checkArgs(t)
		args[1] = "../x"
		err := runCheck(args, new(bytes.Buffer), tooltest.Missing, isolated, root)
		require.ErrorIs(t, err, errUsage)
		assert.Contains(t, err.Error(), `-work "../x"`)
		assert.Equal(t, 2, exitCode(err))
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
			require.ErrorIs(t, runCheck(args, new(bytes.Buffer), f.Tool(), isolated, root), errUsage, name)
			assert.Empty(t, f.Calls(), name)
		}
		require.NoError(t, runCheck([]string{"-work", "tmp/w", "-config", cfg}, new(bytes.Buffer), passingFake(t).Tool(), isolated, root), "対照: 欠けも余りも無ければ通る")
	})

	t.Run("canary が落ちても -work を終わりに消す", func(t *testing.T) {
		t.Parallel()
		args, root := checkArgs(t)
		err := runCheck(args, new(bytes.Buffer), tooltest.New(t).Tool(), isolated, root)
		require.ErrorIs(t, err, errCanary)
		assert.NoDirExists(t, filepath.Join(root, "tmp", "typos-gate"))
	})

	t.Run("fixture を root の tmp の下に、前の中身を消して建て、終わりに消す", func(t *testing.T) {
		t.Parallel()
		args, root := checkArgs(t)
		writeFile(t, filepath.Join(root, "tmp", "typos-gate", "stale"), "")
		f := tooltest.New(t, tooltest.WithPrelude(`if [ "$step" = "`+stepScan+`" ]; then if [ -e stale ]; then echo yes; else echo no; fi > "$ctl/stale.seen"; fi`))
		f.Set(stepScan, "out", hiddenHit)
		f.Set(stepScan, "rc", "2")
		f.Set(stepFiles, "out", "./a.md\n./.github/w.yaml\n")
		f.Set(stepLsFiles, "out", "a.md\x00.github/w.yaml\x00")
		require.NoError(t, runCheck(args, new(bytes.Buffer), f.Tool(), isolated, root))
		assert.Equal(t, "no\n", f.Read("stale.seen"), "canary の起動時に前の中身が残っていない")
		calls := f.Calls()
		require.NotEmpty(t, calls)
		assert.Equal(t, filepath.Join(root, "tmp", "typos-gate")+" "+stepScan+" ", calls[0])
		assert.NoDirExists(t, filepath.Join(root, "tmp", "typos-gate"))
	})

	t.Run("-config の値を範囲の突き合わせの自身の除外へ渡す", func(t *testing.T) {
		t.Parallel()
		args, root := checkArgs(t)
		f := passingFake(t)
		f.Set(stepLsFiles, "out", "a.md\x00.github/w.yaml\x00"+args[3]+"\x00")
		require.NoError(t, runCheck(args, new(bytes.Buffer), f.Tool(), isolated, root))
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
			f := tooltest.New(t)
			f.Set(stepMain, "out", "OUT\n")
			f.Set(stepMain, "err", "STDERR-ONLY\n")
			f.Set(stepMain, "rc", c.rc)
			var out bytes.Buffer
			err := runScan(&out, f.Tool())
			if c.want == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, c.want)
			}
			assert.Contains(t, out.String(), "OUT\n", "道具の出力を写す")
			assert.Contains(t, out.String(), "STDERR-ONLY", "stderr も写す")
			assert.Equal(t, []string{realCwd(t) + " " + stepMain + " "}, f.Calls(), "本走査はリポジトリ直下（cwd）で走る")
		})
	}

	t.Run("起動できなければ errTool", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, runScan(new(bytes.Buffer), tooltest.Missing), errTool)
	})
}

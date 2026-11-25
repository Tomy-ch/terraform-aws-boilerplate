package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/tfexec"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun/tooltest"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// okTree は、modules/a・usecases/b・env/dev に構成とテストを持つ木を建てる。
func okTree(t *testing.T, extra map[string]string) string {
	t.Helper()
	files := map[string]string{}
	for _, d := range []string{"modules/a", "usecases/b", "env/dev"} {
		files[d+"/main.tf"] = ""
		files[d+"/tests/ok.tftest.hcl"] = ""
	}
	for k, v := range extra {
		files[k] = v
	}
	return tree(t, files)
}

func Test_command(t *testing.T) {
	t.Parallel()

	for step, want := range map[string][]string{
		stepFmt:          {"terraform", "fmt", "-recursive"},
		stepFmtCheck:     {"terraform", "fmt", "-check", "-diff", "-recursive"},
		stepInit:         {"terraform", "init", "-backend=false", "-input=false"},
		stepInitReadonly: {"terraform", "init", "-backend=false", "-input=false", "-lockfile=readonly"},
		stepValidate:     {"terraform", "validate", "-json"},
		stepTest:         {"terraform", "test"},
		stepLintInit:     {"tflint", "--init"},
		stepLint:         {"tflint"},
	} {
		t.Run(step, func(t *testing.T) {
			t.Parallel()
			c := command(context.Background(), "/x/d", []string{"A=1"}, step)
			assert.Equal(t, want, c.Args)
			assert.Equal(t, "/x/d", c.Dir)
			assert.Equal(t, []string{"A=1"}, c.Env)
		})
	}
}

func Test_runEach(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		fail []string
		want string
	}{
		"全部通れば成功":    {},
		"最初だけ落ちる":    {fail: []string{"modules/a"}, want: "❌ tf-x が落ちたディレクトリ: {base}/modules/a\n"},
		"最後だけ落ちる":    {fail: []string{"usecases/b"}, want: "❌ tf-x が落ちたディレクトリ: {base}/usecases/b\n"},
		"両方落ちても全部走る": {fail: []string{"modules/a", "usecases/b"}, want: "❌ tf-x が落ちたディレクトリ: {base}/modules/a {base}/usecases/b\n"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			files := map[string]string{"modules/a/main.tf": "", "usecases/b/main.tf": ""}
			for _, d := range c.fail {
				files[d+"/fail-test"] = ""
			}
			base := tree(t, files)
			tool, calls := fakeTool(t)
			targets, err := requireDirs(scan{base: base, roots: []string{"modules", "usecases"}, log: io.Discard})
			require.NoError(t, err)
			var out bytes.Buffer
			err = runEach("tf-x", targets, &out, nil, func(tg target) *exec.Cmd {
				return tool(context.Background(), tg.path, tfexec.Env(os.Environ()), stepTest)
			})
			assert.Len(t, calls(), 2, "落ちても残りのディレクトリを走らせる")
			if c.want == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, errDirsFailed)
			assert.Contains(t, out.String(), strings.ReplaceAll(c.want, "{base}", base))
		})
	}

	threeTargets := func(t *testing.T, files map[string]string, unstartable string) ([]target, func(target) *exec.Cmd, func() []string) {
		t.Helper()
		files["modules/a/main.tf"], files["usecases/b/main.tf"], files["usecases/c/main.tf"] = "", "", ""
		base := tree(t, files)
		tool, calls := fakeTool(t)
		targets, err := requireDirs(scan{base: base, roots: []string{"modules", "usecases"}, log: io.Discard})
		require.NoError(t, err)
		return targets, func(tg target) *exec.Cmd {
			if tg.rel == unstartable {
				return tooltest.Missing(context.Background(), tg.path, nil, stepTest)
			}
			return tool(context.Background(), tg.path, tfexec.Env(os.Environ()), stepTest)
		}, calls
	}

	t.Run("最初の対象で道具を起動できなければ、残りを起動せずに errTool で終了コードは 2", func(t *testing.T) {
		t.Parallel()
		targets, cmdOf, calls := threeTargets(t, map[string]string{}, "modules/a")
		var out bytes.Buffer
		err := runEach("tf-x", targets, &out, nil, cmdOf)
		require.ErrorIs(t, err, errTool)
		require.NotErrorIs(t, err, errDirsFailed)
		assert.Equal(t, 2, exitCode(err))
		assert.Contains(t, err.Error(), "tf-x: "+targets[0].path+" で打ち切りました")
		assert.NotContains(t, err.Error(), "それまでに落ちた")
		assert.Empty(t, calls(), "残りの対象を起動しない")
		assert.NotContains(t, out.String(), "❌")
	})

	t.Run("先の対象が 1 で落ち、次で道具を起動できなければ、先の失敗を文面にだけ名指して errTool で終了コードは 2", func(t *testing.T) {
		t.Parallel()
		targets, cmdOf, calls := threeTargets(t, map[string]string{"modules/a/fail-test": ""}, "usecases/b")
		var out bytes.Buffer
		err := runEach("tf-x", targets, &out, nil, cmdOf)
		require.ErrorIs(t, err, errTool)
		assert.False(t, errors.Is(err, errDirsFailed), "落ちた対象の sentinel を包まない（包むと終了コードが 1 になる）")
		assert.Equal(t, 2, exitCode(err))
		assert.Contains(t, err.Error(), "tf-x: "+targets[1].path+" で打ち切りました（それまでに落ちたディレクトリ: "+targets[0].path+"）")
		assert.NotContains(t, out.String(), "❌", "先の失敗は文面にだけ名指す")
		assert.Equal(t, []string{targets[0].path + " test   "}, calls(), "打ち切った後の対象を起動しない")
	})

	t.Run("道具がシグナルで終われば、残りを起動せずに errTool で終了コードは 2", func(t *testing.T) {
		t.Parallel()
		f := tooltest.New(t)
		f.Set(stepTest, "kill", "")
		targets, _, _ := threeTargets(t, map[string]string{}, "")
		err := runEach("tf-x", targets, &bytes.Buffer{}, nil, func(tg target) *exec.Cmd { return f.Tool()(context.Background(), tg.path, nil, stepTest) })
		require.ErrorIs(t, err, errTool)
		assert.Contains(t, err.Error(), "がシグナルで終わりました")
		assert.Equal(t, 2, exitCode(err))
		assert.Len(t, f.Calls(), 1, "打ち切った後の対象を起動しない")
	})

	t.Run("対照: 0 以外の終了コードは落ちた対象のままで、終了コードは 1", func(t *testing.T) {
		t.Parallel()
		targets, cmdOf, calls := threeTargets(t, map[string]string{"modules/a/fail-test": ""}, "")
		var out bytes.Buffer
		err := runEach("tf-x", targets, &out, nil, cmdOf)
		require.ErrorIs(t, err, errDirsFailed)
		require.NotErrorIs(t, err, errTool)
		assert.Equal(t, 1, exitCode(err))
		assert.Len(t, calls(), 3)
		assert.Contains(t, out.String(), "tf-x: "+targets[0].path+": exit status 1\n")
	})

	for name, c := range map[string]struct {
		detect   []int
		rc       string
		wantErr  error
		wantCode int
		calls    int
	}{
		"fmt-check の 3 は落ちた対象で、全部走って終了コードは 1":       {detectFmtCheck, "3", errDirsFailed, 1, 3},
		"fmt-check の 1 は不成立で、打ち切って終了コードは 2":         {detectFmtCheck, "1", errToolFailed, 2, 1},
		"fmt-check の 2 は不成立で、打ち切って終了コードは 2":         {detectFmtCheck, "2", errToolFailed, 2, 1},
		"lint の 2 は落ちた対象で、全部走って終了コードは 1":            {detectLint, "2", errDirsFailed, 1, 3},
		"lint の 1 は不成立で、打ち切って終了コードは 2":              {detectLint, "1", errToolFailed, 2, 1},
		"lint の 3 は不成立で、打ち切って終了コードは 2":              {detectLint, "3", errToolFailed, 2, 1},
		"detect が空なら 0 以外の値（ここでは 5）は落ちた対象で終了コードは 1": {nil, "5", errDirsFailed, 1, 3},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := tooltest.New(t)
			f.Set(stepTest, "rc", c.rc)
			targets, _, _ := threeTargets(t, map[string]string{}, "")
			var out bytes.Buffer
			err := runEach("tf-x", targets, &out, c.detect, func(tg target) *exec.Cmd {
				return f.Tool()(context.Background(), tg.path, nil, stepTest)
			})
			require.ErrorIs(t, err, c.wantErr)
			assert.Equal(t, c.wantCode, exitCode(err))
			assert.Len(t, f.Calls(), c.calls)
			if errors.Is(c.wantErr, errToolFailed) {
				assert.Contains(t, err.Error(), "exit status "+c.rc)
				assert.NotContains(t, out.String(), "❌")
			}
		})
	}

	t.Run("detect の値で先の対象が落ち、次が不成立なら、先の失敗を文面に名指して errToolFailed", func(t *testing.T) {
		t.Parallel()
		targets, _, _ := threeTargets(t, map[string]string{}, "")
		f := tooltest.New(t, tooltest.WithBase(filepath.Dir(filepath.Dir(targets[0].path))))
		f.Set(stepTest+"@modules:a", "rc", "2")
		f.Set(stepTest+"@usecases:b", "rc", "1")
		err := runEach("tf-x", targets, &bytes.Buffer{}, detectLint, func(tg target) *exec.Cmd {
			return f.Tool()(context.Background(), tg.path, nil, stepTest)
		})
		require.ErrorIs(t, err, errToolFailed)
		require.NotErrorIs(t, err, errDirsFailed)
		assert.Contains(t, err.Error(), "（それまでに落ちたディレクトリ: "+targets[0].path+"）")
		assert.Len(t, f.Calls(), 2)
	})
}

func Test_runFmt(t *testing.T) {
	t.Parallel()

	t.Run("root ごとに fmt を起動する", func(t *testing.T) {
		t.Parallel()
		base := okTree(t, nil)
		tool, calls := fakeTool(t)
		require.NoError(t, runFmt([]string{"-base", base, "modules", "usecases"}, &bytes.Buffer{}, tool))
		assert.Equal(t, []string{filepath.Join(base, "modules") + " fmt   ", filepath.Join(base, "usecases") + " fmt   "}, calls())
	})

	t.Run("-check は fmt-check を起動し、落ちた root を名指す", func(t *testing.T) {
		t.Parallel()
		base := okTree(t, map[string]string{"usecases/fail-fmt-check": ""})
		tool, calls := fakeTool(t)
		var out bytes.Buffer
		err := runFmt([]string{"-check", "-base", base, "modules", "usecases"}, &out, tool)
		require.ErrorIs(t, err, errDirsFailed)
		assert.Contains(t, out.String(), "❌ tf-fmt-check が落ちたディレクトリ: "+filepath.Join(base, "usecases"))
		assert.Len(t, calls(), 2)
	})

	t.Run("対象が0件なら起動せずに errNoTargets", func(t *testing.T) {
		t.Parallel()
		base := tree(t, map[string]string{"modules/.keep": ""})
		tool, calls := fakeTool(t)
		require.ErrorIs(t, runFmt([]string{"-check", "-base", base, "modules"}, &bytes.Buffer{}, tool), errNoTargets)
		assert.Empty(t, calls())
	})

	t.Run("宣言した root が無ければ起動せずに errMissingRoot", func(t *testing.T) {
		t.Parallel()
		base := tree(t, map[string]string{"modules/a/main.tf": ""})
		tool, calls := fakeTool(t)
		require.ErrorIs(t, runFmt([]string{"-check", "-base", base, "modules", "env"}, &bytes.Buffer{}, tool), errMissingRoot)
		assert.Empty(t, calls())
	})

	t.Run("-allow-missing-roots なら、無い root を名指して飛ばし、在る root だけで fmt を起動する", func(t *testing.T) {
		t.Parallel()
		base := tree(t, map[string]string{"modules/a/main.tf": ""})
		tool, calls := fakeTool(t)
		var out bytes.Buffer
		require.NoError(t, runFmt([]string{"-check", "-allow-missing-roots", "-base", base, "modules", "env"}, &out, tool))
		assert.Equal(t, []string{filepath.Join(base, "modules") + " fmt-check   "}, calls())
		assert.Contains(t, out.String(), "走査の root env が無いので飛ばします")
	})

	t.Run("引数の誤りは errUsage", func(t *testing.T) {
		t.Parallel()
		tool, _ := fakeTool(t)
		require.ErrorIs(t, runFmt([]string{"-x"}, &bytes.Buffer{}, tool), errUsage)
	})

	t.Run("root の親が通常のファイルで Stat が ENOTDIR なら、道具を起動せずに errMissingRoot で終了コードは 2", func(t *testing.T) {
		t.Parallel()
		base := tree(t, map[string]string{"modules": "", "usecases/b/main.tf": ""})
		tool, calls := fakeTool(t)
		err := runFmt([]string{"-check", "-allow-missing-roots", "-base", base, "modules/a", "usecases"}, &bytes.Buffer{}, tool)
		require.ErrorIs(t, err, errMissingRoot)
		assert.Contains(t, err.Error(), "not a directory")
		assert.Equal(t, 2, exitCode(err))
		assert.Empty(t, calls())
	})

	t.Run("道具を起動できなければ errTool で終了コードは 2", func(t *testing.T) {
		t.Parallel()
		err := runFmt([]string{"-check", "-base", okTree(t, nil), "modules", "usecases"}, &bytes.Buffer{}, tooltest.Missing)
		require.ErrorIs(t, err, errTool)
		assert.Equal(t, 2, exitCode(err))
	})
}

func Test_parseData(t *testing.T) {
	t.Parallel()

	t.Run("-data が相対なら errUsage", func(t *testing.T) {
		t.Parallel()
		base := okTree(t, nil)
		_, _, err := parseData("x", []string{"-base", base, "-data", "tmp/d", "modules"}, &bytes.Buffer{}, nil)
		require.ErrorIs(t, err, errUsage)
	})

	t.Run("対象が0件なら errNoTargets", func(t *testing.T) {
		t.Parallel()
		base := tree(t, map[string]string{"modules/.keep": ""})
		_, _, err := parseData("x", []string{"-base", base, "-data", "/d", "modules"}, &bytes.Buffer{}, nil)
		require.ErrorIs(t, err, errNoTargets)
	})

	t.Run("宣言した root が無ければ errMissingRoot", func(t *testing.T) {
		t.Parallel()
		base := okTree(t, nil)
		_, _, err := parseData("x", []string{"-base", base, "-data", "/d", "modules", "nowhere"}, &bytes.Buffer{}, nil)
		require.ErrorIs(t, err, errMissingRoot)
	})

	t.Run("-data と対象を返す", func(t *testing.T) {
		t.Parallel()
		base := okTree(t, nil)
		data, targets, err := parseData("x", []string{"-base", base, "-data", "/d", "modules"}, &bytes.Buffer{}, nil)
		require.NoError(t, err)
		assert.Equal(t, "/d", data)
		assert.Len(t, targets, 1)
	})
}

func Test_runInit(t *testing.T) {
	t.Parallel()

	t.Run("root は lock を書き換えない init、ほかは通常の init を、ディレクトリごとの TF_DATA_DIR で起動する", func(t *testing.T) {
		t.Parallel()
		base := okTree(t, map[string]string{"env/dev/identity/main.tf": "", "usecases/x/reference/main.tf": "", "env/dev/other/main.tf": ""})
		tool, calls := fakeTool(t)
		require.NoError(t, runInit([]string{"-base", base, "-data", "/d", "usecases", "modules", "env"}, &bytes.Buffer{}, tool))
		assert.Equal(t, []string{
			filepath.Join(base, "env/dev") + " init-readonly /d/env__dev  ",
			filepath.Join(base, "env/dev/identity") + " init-readonly /d/env__dev__identity  ",
			filepath.Join(base, "env/dev/other") + " init /d/env__dev__other  ",
			filepath.Join(base, "modules/a") + " init /d/modules__a  ",
			filepath.Join(base, "usecases/b") + " init /d/usecases__b  ",
			filepath.Join(base, "usecases/x/reference") + " init-readonly /d/usecases__x__reference  ",
		}, calls())
	})

	t.Run("落ちたディレクトリを名指す", func(t *testing.T) {
		t.Parallel()
		base := okTree(t, map[string]string{"modules/a/fail-init": ""})
		tool, _ := fakeTool(t)
		var out bytes.Buffer
		require.ErrorIs(t, runInit([]string{"-base", base, "-data", "/d", "modules", "usecases"}, &out, tool), errDirsFailed)
		assert.Contains(t, out.String(), "❌ tf-init が落ちたディレクトリ: "+filepath.Join(base, "modules/a")+"\n")
	})

	t.Run("道具を起動できなければ errTool で終了コードは 2", func(t *testing.T) {
		t.Parallel()
		err := runInit([]string{"-base", okTree(t, nil), "-data", "/d", "modules"}, &bytes.Buffer{}, tooltest.Missing)
		require.ErrorIs(t, err, errTool)
		assert.Equal(t, 2, exitCode(err))
	})
}

func Test_initTargets(t *testing.T) {
	t.Parallel()

	t.Run("対象が無ければ何も起動しない", func(t *testing.T) {
		t.Parallel()
		tool, calls := fakeTool(t)
		require.NoError(t, initTargets("/d", nil, &bytes.Buffer{}, tool))
		assert.Empty(t, calls())
	})
}

var canaryFlags = []string{"-canary-data", "/cd", "-canary-work", "tmp/cw", "-canary-reference", "cr"}

func passCanary(got *[]string) canaryFunc {
	return func(args []string, _ io.Writer, _ toolFunc) error {
		*got = args

		return nil
	}
}

func Test_runValidate(t *testing.T) {
	t.Parallel()

	t.Run("canary を本走査の前に走らせ、各ディレクトリの出力と終了コードを一覧に書き、落ちても全部走らせる", func(t *testing.T) {
		t.Parallel()
		base := okTree(t, map[string]string{"modules/a/validate.out": `{"valid":true}`, "usecases/b/fail-validate": ""})
		tool, calls := fakeTool(t)
		out := filepath.Join(t.TempDir(), "v")
		var got []string
		args := append(append([]string{"-base", base, "-data", "/d", "-out", out}, canaryFlags...), "modules", "usecases")
		require.NoError(t, runValidate(args, &bytes.Buffer{}, tool, passCanary(&got), tree(t, nil)))
		assert.Equal(t, []string{"-data", "/cd", "-work", "tmp/cw", "-reference", "cr"}, got, "-canary-work は検証した元の値のまま渡す")
		assert.Len(t, calls(), 2)
		manifest, err := os.ReadFile(filepath.Join(out, "manifest.tsv"))
		require.NoError(t, err)
		assert.Equal(t,
			filepath.Join(base, "modules/a")+"\t0\t"+filepath.Join(out, "1.json")+"\t"+filepath.Join(out, "1.err")+"\n"+
				filepath.Join(base, "usecases/b")+"\t1\t"+filepath.Join(out, "2.json")+"\t"+filepath.Join(out, "2.err")+"\n",
			string(manifest))
		js, err := os.ReadFile(filepath.Join(out, "1.json"))
		require.NoError(t, err)
		assert.Equal(t, `{"valid":true}`, string(js))
	})

	t.Run("canary が落ちれば本走査を行わずに失敗する", func(t *testing.T) {
		t.Parallel()
		base := okTree(t, nil)
		tool, calls := fakeTool(t)
		out := filepath.Join(t.TempDir(), "v")
		fail := func([]string, io.Writer, toolFunc) error { return xerrors.Wrap(errCanary, "x") }
		err := runValidate(append(append([]string{"-base", base, "-data", "/d", "-out", out}, canaryFlags...), "modules"), &bytes.Buffer{}, tool, fail, tree(t, nil))
		require.ErrorIs(t, err, errCanary)
		assert.Contains(t, err.Error(), "canary が落ちたので、本走査を行いません")
		assert.Empty(t, calls())
		assert.NoFileExists(t, filepath.Join(out, "manifest.tsv"))
	})

	t.Run("-out か canary の引数が無ければ、canary も本走査も起動せずに errUsage", func(t *testing.T) {
		t.Parallel()
		base := okTree(t, nil)
		tool, calls := fakeTool(t)
		var got []string
		require.ErrorIs(t, runValidate(append([]string{"-base", base, "-data", "/d"}, append(canaryFlags, "modules")...), &bytes.Buffer{}, tool, passCanary(&got), tree(t, nil)), errUsage)
		require.ErrorIs(t, runValidate([]string{"-base", base, "-data", "/d", "-out", "o", "modules"}, &bytes.Buffer{}, tool, passCanary(&got), tree(t, nil)), errUsage)
		assert.Nil(t, got)
		assert.Empty(t, calls())
	})

	t.Run("本走査で道具を起動できなければ、errTool で終了コードは 2 で、errDirsFailed を包まない", func(t *testing.T) {
		t.Parallel()
		out := filepath.Join(t.TempDir(), "v")
		var got []string
		args := append(append([]string{"-base", okTree(t, nil), "-data", "/d", "-out", out}, canaryFlags...), "modules")
		err := runValidate(args, &bytes.Buffer{}, tooltest.Missing, passCanary(&got), tree(t, nil))
		require.ErrorIs(t, err, errTool)
		assert.False(t, errors.Is(err, errDirsFailed))
		assert.Equal(t, 2, exitCode(err))
		assert.NotNil(t, got, "canary は走った")
		assert.NoFileExists(t, filepath.Join(out, "manifest.tsv"))
	})

	t.Run("canary が落ちた理由の sentinel と終了コードを保ち、本走査を行わないことを添える", func(t *testing.T) {
		t.Parallel()
		for name, c := range map[string]struct {
			cause error
			code  int
		}{
			"canary の不成立は 1":        {errCanary, 1},
			"canary の引数の誤りは 2":      {errUsage, 2},
			"canary の root が無いのは 2": {errMissingRoot, 2},
			"canary の道具を起動できないのは 2": {errTool, 2},
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				tool, calls := fakeTool(t)
				fail := func([]string, io.Writer, toolFunc) error { return xerrors.Wrap(c.cause, "x") }
				err := runValidate(append(append([]string{"-base", okTree(t, nil), "-data", "/d", "-out", filepath.Join(t.TempDir(), "v")}, canaryFlags...), "modules"), &bytes.Buffer{}, tool, fail, tree(t, nil))
				require.ErrorIs(t, err, c.cause)
				assert.Equal(t, c.code, exitCode(err))
				assert.Contains(t, err.Error(), "canary が落ちたので、本走査を行いません")
				assert.Empty(t, calls())
			})
		}
	})

	t.Run("canary の引数が1つでも無ければ、canary も道具も起動せず何も消さずに errUsage で終了コードは 2", func(t *testing.T) {
		t.Parallel()
		for _, drop := range []string{"-canary-data", "-canary-work", "-canary-reference"} {
			t.Run(drop, func(t *testing.T) {
				t.Parallel()
				world, root, _ := workWorld(t)
				var args []string
				for i := 0; i < len(canaryFlags); i += 2 {
					if canaryFlags[i] != drop {
						args = append(args, canaryFlags[i], canaryFlags[i+1])
					}
				}
				args = append(append([]string{"-base", okTree(t, nil), "-data", "/d", "-out", filepath.Join(t.TempDir(), "v")}, args...), "modules")
				before := snapshot(t, world)
				tool, calls := fakeTool(t)
				var got []string
				err := runValidate(args, &bytes.Buffer{}, tool, passCanary(&got), root)
				require.ErrorIs(t, err, errUsage)
				assert.Equal(t, 2, exitCode(err))
				assert.Nil(t, got, "canary を起動しない")
				assert.Empty(t, calls())
				assert.Equal(t, before, snapshot(t, world))
			})
		}
	})

	t.Run("-canary-work が tmp/ の下の相対パスでなければ、canary も道具も起動せず何も消さずに、-canary-work を名指す errUsage で終了コードは 2", func(t *testing.T) {
		t.Parallel()
		for _, work := range []string{
			"", ".", "tmp", "tmp/", "tmp/.", "<root>/keep", "keep", "tmp/../keep", "../x", "TMP/x",
			"tmp/out", "tmp/out/w", "tmp/self",
		} {
			t.Run(work, func(t *testing.T) {
				t.Parallel()
				world, root, _ := workWorld(t)
				if work == "<root>/keep" {
					work = filepath.Join(root, "keep")
				}
				base := okTree(t, nil)
				before := snapshot(t, world)
				tool, calls := fakeTool(t)
				var got []string
				args := []string{"-base", base, "-data", "/d", "-out", filepath.Join(t.TempDir(), "v"),
					"-canary-data", "/cd", "-canary-work", work, "-canary-reference", "cr", "modules"}
				err := runValidate(args, &bytes.Buffer{}, tool, passCanary(&got), root)
				require.ErrorIs(t, err, errUsage)
				assert.Equal(t, 2, exitCode(err))
				if work != "" {
					assert.Contains(t, err.Error(), fmt.Sprintf("-canary-work %q", work))
				}
				assert.Nil(t, got, "canary を起動しない")
				assert.Empty(t, calls())
				assert.Equal(t, before, snapshot(t, world))
			})
		}
	})
}

func Test_validateTargets(t *testing.T) {
	t.Parallel()

	t.Run("道具を起動できなければ、残りを起動せず、前の一覧を消したまま書かずに errTool で終了コードは 2", func(t *testing.T) {
		t.Parallel()
		base := okTree(t, map[string]string{"env/dev/fail-validate": ""})
		targets, err := requireDirs(scan{base: base, roots: []string{"modules", "usecases", "env"}, log: io.Discard})
		require.NoError(t, err)
		require.Len(t, targets, 3)
		out := tree(t, map[string]string{"manifest.tsv": "stale\t0\tx\ty\n"})
		tool, calls := fakeTool(t)
		cmdOf := func(ctx context.Context, d string, env []string, step string, _ ...string) *exec.Cmd {
			if d == targets[1].path {
				return tooltest.Missing(ctx, d, env, step)
			}
			return tool(ctx, d, env, step)
		}
		err = validateTargets("/d", out, targets, cmdOf)
		require.ErrorIs(t, err, errTool)
		require.NotErrorIs(t, err, errDirsFailed)
		assert.Equal(t, 2, exitCode(err))
		assert.Contains(t, err.Error(), targets[1].path+" の validate で打ち切りました")
		assert.Equal(t, []string{targets[0].path + " validate /d/env__dev  "}, calls(), "打ち切った後の対象を起動しない")
		assert.NoFileExists(t, filepath.Join(out, "manifest.tsv"), "前の一覧を緑として残さない")
	})

	t.Run("道具がシグナルで終われば、一覧を書かずに errTool", func(t *testing.T) {
		t.Parallel()
		f := tooltest.New(t)
		f.Set(stepValidate, "kill", "")
		out := tree(t, map[string]string{"manifest.tsv": "stale\n"})
		err := validateTargets("/d", out, []target{{rel: "m", path: t.TempDir()}}, f.Tool())
		require.ErrorIs(t, err, errTool)
		assert.Contains(t, err.Error(), "がシグナルで終わりました")
		assert.NoFileExists(t, filepath.Join(out, "manifest.tsv"))
	})

	t.Run("前の一覧を消せなければ、道具を起動せずに失敗する", func(t *testing.T) {
		t.Parallel()
		out := tree(t, map[string]string{"manifest.tsv/f": ""})
		tool, calls := fakeTool(t)
		err := validateTargets("/d", out, []target{{rel: "m", path: t.TempDir()}}, tool)
		require.Error(t, err)
		require.NotErrorIs(t, err, errTool)
		assert.Contains(t, err.Error(), "manifest.tsv の削除")
		assert.Empty(t, calls())
	})

	t.Run("前の一覧を書き直す", func(t *testing.T) {
		t.Parallel()
		d := t.TempDir()
		out := tree(t, map[string]string{"manifest.tsv": "stale\n"})
		tool, _ := fakeTool(t)
		require.NoError(t, validateTargets("/d", out, []target{{rel: "m", path: d}}, tool))
		manifest, err := os.ReadFile(filepath.Join(out, "manifest.tsv"))
		require.NoError(t, err)
		assert.Equal(t, d+"\t0\t"+filepath.Join(out, "1.json")+"\t"+filepath.Join(out, "1.err")+"\n", string(manifest))
	})

	t.Run("出力の置き場を作れなければ失敗する", func(t *testing.T) {
		t.Parallel()
		file := filepath.Join(t.TempDir(), "f")
		require.NoError(t, os.WriteFile(file, nil, 0o600))
		tool, _ := fakeTool(t)
		require.Error(t, validateTargets("/d", filepath.Join(file, "v"), []target{{rel: "m", path: t.TempDir()}}, tool))
	})
}

func Test_runTest(t *testing.T) {
	t.Parallel()

	t.Run("*.tftest.hcl を持つディレクトリだけで test を起動する", func(t *testing.T) {
		t.Parallel()
		base := okTree(t, map[string]string{"modules/notest/main.tf": "", "modules/direct/main.tf": "", "modules/direct/d.tftest.hcl": ""})
		tool, calls := fakeTool(t)
		require.NoError(t, runTest([]string{"-base", base, "-data", "/d", "modules"}, &bytes.Buffer{}, tool))
		assert.Equal(t, []string{
			filepath.Join(base, "modules/a") + " test /d/modules__a  ",
			filepath.Join(base, "modules/direct") + " test /d/modules__direct  ",
		}, calls())
	})

	t.Run("テストを持つディレクトリが0件なら errNoTargets", func(t *testing.T) {
		t.Parallel()
		base := tree(t, map[string]string{"modules/a/main.tf": ""})
		tool, calls := fakeTool(t)
		err := runTest([]string{"-base", base, "-data", "/d", "modules"}, &bytes.Buffer{}, tool)
		require.ErrorIs(t, err, errNoTargets)
		assert.Contains(t, err.Error(), "*.tftest.hcl を持つディレクトリが0件")
		assert.Empty(t, calls())
	})

	t.Run("落ちたディレクトリを名指す", func(t *testing.T) {
		t.Parallel()
		base := okTree(t, map[string]string{"modules/a/fail-test": "", "usecases/b/fail-test": ""})
		tool, _ := fakeTool(t)
		var out bytes.Buffer
		require.ErrorIs(t, runTest([]string{"-base", base, "-data", "/d", "modules", "usecases"}, &out, tool), errDirsFailed)
		assert.Contains(t, out.String(), "❌ tf-test が落ちたディレクトリ: "+filepath.Join(base, "modules/a")+" "+filepath.Join(base, "usecases/b"))
	})

	t.Run("道具を起動できなければ errTool で終了コードは 2", func(t *testing.T) {
		t.Parallel()
		err := runTest([]string{"-base", okTree(t, nil), "-data", "/d", "modules"}, &bytes.Buffer{}, tooltest.Missing)
		require.ErrorIs(t, err, errTool)
		assert.Equal(t, 2, exitCode(err))
	})
}

func Test_runLint(t *testing.T) {
	t.Parallel()

	t.Run("ruleset を取得してから、設定と ruleset の置き場を渡して各ディレクトリで TFLint を起動する", func(t *testing.T) {
		t.Parallel()
		base := okTree(t, nil)
		plugins := filepath.Join(t.TempDir(), "p")
		tool, calls := fakeTool(t)
		require.NoError(t, runLint([]string{"-base", base, "-config", "/c/.tflint.hcl", "-plugin-dir", plugins, "modules", "usecases"}, &bytes.Buffer{}, tool))
		assert.Equal(t, []string{
			base + " lint-init  /c/.tflint.hcl " + plugins,
			filepath.Join(base, "modules/a") + " lint  /c/.tflint.hcl " + plugins,
			filepath.Join(base, "usecases/b") + " lint  /c/.tflint.hcl " + plugins,
		}, calls())
		assert.DirExists(t, plugins)
	})

	t.Run("ruleset の取得で道具を起動できなければ、ディレクトリを走らせずに errTool で終了コードは 2", func(t *testing.T) {
		t.Parallel()
		base := okTree(t, nil)
		tool, calls := fakeTool(t)
		cmdOf := func(ctx context.Context, d string, env []string, step string, _ ...string) *exec.Cmd {
			if step == stepLintInit {
				return tooltest.Missing(ctx, d, env, step)
			}
			return tool(ctx, d, env, step)
		}
		err := runLint([]string{"-base", base, "-config", "c", "-plugin-dir", t.TempDir(), "modules"}, &bytes.Buffer{}, cmdOf)
		require.ErrorIs(t, err, errTool)
		assert.Equal(t, 2, exitCode(err))
		assert.Empty(t, calls())
	})

	t.Run("ディレクトリごとの TFLint を起動できなければ、残りを起動せずに errTool で終了コードは 2", func(t *testing.T) {
		t.Parallel()
		base := okTree(t, nil)
		tool, calls := fakeTool(t)
		cmdOf := func(ctx context.Context, d string, env []string, step string, _ ...string) *exec.Cmd {
			if step == stepLint {
				return tooltest.Missing(ctx, d, env, step)
			}
			return tool(ctx, d, env, step)
		}
		err := runLint([]string{"-base", base, "-config", "c", "-plugin-dir", t.TempDir(), "modules", "usecases"}, &bytes.Buffer{}, cmdOf)
		require.ErrorIs(t, err, errTool)
		assert.False(t, errors.Is(err, errDirsFailed))
		assert.Equal(t, 2, exitCode(err))
		assert.Len(t, calls(), 1, "ruleset の取得だけが走った")
	})

	t.Run("ruleset の取得が落ちれば、ディレクトリを走らせずに落ちる", func(t *testing.T) {
		t.Parallel()
		base := okTree(t, map[string]string{"fail-lint-init": ""})
		tool, calls := fakeTool(t)
		require.ErrorIs(t, runLint([]string{"-base", base, "-config", "c", "-plugin-dir", t.TempDir(), "modules"}, &bytes.Buffer{}, tool), errDirsFailed)
		assert.Len(t, calls(), 1)
	})

	t.Run("落ちたディレクトリを名指す", func(t *testing.T) {
		t.Parallel()
		base := okTree(t, map[string]string{"usecases/b/fail-lint": ""})
		tool, _ := fakeTool(t)
		var out bytes.Buffer
		require.ErrorIs(t, runLint([]string{"-base", base, "-config", "c", "-plugin-dir", t.TempDir(), "modules", "usecases"}, &out, tool), errDirsFailed)
		assert.Contains(t, out.String(), "❌ tf-lint が落ちたディレクトリ: "+filepath.Join(base, "usecases/b"))
	})

	t.Run("-config か -plugin-dir が無ければ errUsage", func(t *testing.T) {
		t.Parallel()
		tool, _ := fakeTool(t)
		require.ErrorIs(t, runLint([]string{"-base", ".", "-config", "c", "modules"}, &bytes.Buffer{}, tool), errUsage)
		require.ErrorIs(t, runLint([]string{"-base", ".", "-plugin-dir", "p", "modules"}, &bytes.Buffer{}, tool), errUsage)
	})

	t.Run("対象が0件なら起動せずに errNoTargets", func(t *testing.T) {
		t.Parallel()
		base := tree(t, map[string]string{"modules/.keep": ""})
		tool, calls := fakeTool(t)
		require.ErrorIs(t, runLint([]string{"-base", base, "-config", "c", "-plugin-dir", t.TempDir(), "modules"}, &bytes.Buffer{}, tool), errNoTargets)
		assert.Empty(t, calls())
	})
}

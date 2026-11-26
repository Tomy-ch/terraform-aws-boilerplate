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
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/tflock"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun/tooltest"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

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
		stepInitReadonly: {"terraform", "init", "-backend=false", "-input=false", "-lockfile=readonly"},
		stepValidate:     {"terraform", "validate", "-json"},
		stepTest:         {"terraform", "test"},
		stepLintVersion:  {"tflint", "--version"},
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

	t.Run("providers-lock は追加の引数を platform として並べる", func(t *testing.T) {
		t.Parallel()
		c := command(context.Background(), "/x/d", []string{"A=1"}, stepProvidersLock, "linux_amd64", "darwin_arm64")
		assert.Equal(t, []string{"terraform", "providers", "lock", "-platform=linux_amd64", "-platform=darwin_arm64"}, c.Args)
		assert.Equal(t, "/x/d", c.Dir)
		assert.Equal(t, []string{"A=1"}, c.Env)
	})

	t.Run("providers-lock は platform が0個なら -platform を並べない", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, []string{"terraform", "providers", "lock"}, command(context.Background(), "/x/d", nil, stepProvidersLock).Args)
	})
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
		"fmt-check の 2（構文の誤り）は落ちた対象で、全部走って終了コードは 1": {detectFmtCheck, "2", errDirsFailed, 1, 3},
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

	for name, c := range map[string]struct {
		check    bool
		rc       string
		want     error
		wantCode int
		calls    int
	}{
		"-check の 2（構文の誤り）は落ちた root で、全部走って終了コード 1":           {true, "2", errDirsFailed, 1, 2},
		"-check の 1（flag の誤りなど）は errToolFailed で打ち切り、終了コード 2": {true, "1", errToolFailed, 2, 1},
		"-check なしの fmt は 0 以外をすべて落ちた root にし、終了コード 1":        {false, "1", errDirsFailed, 1, 2},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			step := stepFmt
			if c.check {
				step = stepFmtCheck
			}
			base := okTree(t, map[string]string{"modules/rc-" + step: c.rc})
			args := []string{"-base", base, "modules", "usecases"}
			if c.check {
				args = append([]string{"-check"}, args...)
			}
			tool, calls := fakeTool(t)
			err := runFmt(args, &bytes.Buffer{}, tool)
			require.ErrorIs(t, err, c.want)
			assert.Equal(t, c.wantCode, exitCode(err))
			assert.Len(t, calls(), c.calls)
		})
	}

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

const awsRequired = "terraform {\n  required_providers {\n    aws = {\n      source  = \"hashicorp/aws\"\n      version = \">= 6.68.0, < 7.0.0\"\n    }\n  }\n}\n"

// initArgs は、正本（providers/）と mise.toml を base に置き、runInit の引数の頭を返す。
func initArgs(t *testing.T, base string) []string {
	t.Helper()
	for rel, body := range map[string]string{"providers/providers.toml": testDecl, "providers/.terraform.lock.hcl": testLock, "mise.toml": testMise} {
		require.NoError(t, os.MkdirAll(filepath.Join(base, filepath.Dir(rel)), 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(base, rel), []byte(body), 0o600))
	}

	return []string{"-base", base, "-data", "/d", "-providers", filepath.Join(base, "providers"), "-mise", filepath.Join(base, "mise.toml")}
}

func Test_runInit(t *testing.T) {
	t.Parallel()

	t.Run("すべてのディレクトリを、ディレクトリごとの TF_DATA_DIR で lock を書き換えない init で起動する", func(t *testing.T) {
		t.Parallel()
		base := okTree(t, map[string]string{"env/dev/identity/main.tf": "", "usecases/x/reference/main.tf": "", "env/dev/other/main.tf": ""})
		tool, calls := fakeTool(t)
		require.NoError(t, runInit(append(initArgs(t, base), "usecases", "modules", "env"), &bytes.Buffer{}, tool))
		assert.Equal(t, []string{
			filepath.Join(base, "env/dev") + " init-readonly /d/env__dev  ",
			filepath.Join(base, "env/dev/identity") + " init-readonly /d/env__dev__identity  ",
			filepath.Join(base, "env/dev/other") + " init-readonly /d/env__dev__other  ",
			filepath.Join(base, "modules/a") + " init-readonly /d/modules__a  ",
			filepath.Join(base, "usecases/b") + " init-readonly /d/usecases__b  ",
			filepath.Join(base, "usecases/x/reference") + " init-readonly /d/usecases__x__reference  ",
		}, calls())
	})

	t.Run("root でない、provider を要求するディレクトリにだけ正本の lock を置く", func(t *testing.T) {
		t.Parallel()
		base := okTree(t, map[string]string{"modules/a/versions.tf": awsRequired, "env/dev/versions.tf": awsRequired, "env/dev/.terraform.lock.hcl": "root\n", "usecases/b/.terraform.lock.hcl": "stale\n"})
		tool, _ := fakeTool(t)
		require.NoError(t, runInit(append(initArgs(t, base), "usecases", "modules", "env"), &bytes.Buffer{}, tool))
		assert.Equal(t, testLock, readFile(t, filepath.Join(base, "modules", "a", tflock.LockFile)))
		assert.NoFileExists(t, filepath.Join(base, "usecases", "b", tflock.LockFile), "provider を要求しないディレクトリの前の lock は消す")
		assert.Equal(t, "root\n", readFile(t, filepath.Join(base, "env", "dev", tflock.LockFile)), "root の lock には触れない")
	})

	t.Run("要求する provider が正本と違えば、1つも起動せずに errProviders", func(t *testing.T) {
		t.Parallel()
		base := okTree(t, map[string]string{"modules/a/versions.tf": strings.Replace(awsRequired, "hashicorp/aws", "hashicorp/random", 1)})
		tool, calls := fakeTool(t)
		err := runInit(append(initArgs(t, base), "modules"), &bytes.Buffer{}, tool)
		require.ErrorIs(t, err, errProviders)
		assert.Contains(t, err.Error(), "registry.terraform.io/hashicorp/random")
		assert.Empty(t, calls())
		assert.NoFileExists(t, filepath.Join(base, "modules", "a", tflock.LockFile))
	})

	t.Run("1つでも要求が合わなければ、ほかのディレクトリにも lock を置かず1つも起動しない", func(t *testing.T) {
		t.Parallel()
		base := okTree(t, map[string]string{"modules/a/versions.tf": awsRequired, "usecases/b/versions.tf": strings.Replace(awsRequired, "hashicorp/aws", "hashicorp/random", 1), "modules/a/.terraform.lock.hcl": "old\n"})
		tool, calls := fakeTool(t)
		require.ErrorIs(t, runInit(append(initArgs(t, base), "usecases", "modules"), &bytes.Buffer{}, tool), errProviders)
		assert.Empty(t, calls())
		assert.Equal(t, "old\n", readFile(t, filepath.Join(base, "modules", "a", tflock.LockFile)))
	})

	t.Run("root は要求する provider が正本と違っても、lock を置かず errProviders にもしない", func(t *testing.T) {
		t.Parallel()
		base := okTree(t, map[string]string{"env/dev/versions.tf": strings.Replace(awsRequired, "hashicorp/aws", "hashicorp/random", 1), "env/dev/.terraform.lock.hcl": "root\n"})
		tool, _ := fakeTool(t)
		require.NoError(t, runInit(append(initArgs(t, base), "env"), &bytes.Buffer{}, tool))
		assert.Equal(t, "root\n", readFile(t, filepath.Join(base, "env", "dev", tflock.LockFile)))
	})

	for name, c := range map[string]struct {
		mutate func(t *testing.T, base string)
		want   error
	}{
		"正本の lock が無い": {func(t *testing.T, base string) {
			t.Helper()
			require.NoError(t, os.Remove(filepath.Join(base, "providers", tflock.LockFile)))
		}, os.ErrNotExist},
		"providers.toml を読めない": {func(t *testing.T, base string) {
			t.Helper()
			require.NoError(t, os.WriteFile(filepath.Join(base, "providers", tflock.DeclFile), []byte("x\n"), 0o600))
		}, tflock.ErrInvalidDecl},
		"lockfile_platforms が無い": {func(t *testing.T, base string) {
			t.Helper()
			require.NoError(t, os.WriteFile(filepath.Join(base, "mise.toml"), []byte("[tools]\n"), 0o600))
		}, tflock.ErrInvalidPlatform},
		"正本の lock が宣言とずれている": {func(t *testing.T, base string) {
			t.Helper()
			require.NoError(t, os.WriteFile(filepath.Join(base, "providers", tflock.LockFile), []byte(strings.Replace(testLock, "\"6.73.0\"\n  constraints", "\"6.70.0\"\n  constraints", 1)), 0o600))
		}, errLock},
	} {
		t.Run(name+"なら、1つも起動しない", func(t *testing.T) {
			t.Parallel()
			base := okTree(t, nil)
			args := initArgs(t, base)
			c.mutate(t, base)
			tool, calls := fakeTool(t)
			require.ErrorIs(t, runInit(append(args, "modules"), &bytes.Buffer{}, tool), c.want)
			assert.Empty(t, calls())
		})
	}

	t.Run("-providers が無ければ errUsage", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, runInit([]string{"-base", okTree(t, nil), "-data", "/d", "-mise", "m", "modules"}, &bytes.Buffer{}, command), errUsage)
	})

	t.Run("-mise が無ければ errUsage", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, runInit([]string{"-base", okTree(t, nil), "-data", "/d", "-providers", "p", "modules"}, &bytes.Buffer{}, command), errUsage)
	})

	t.Run("落ちたディレクトリを名指す", func(t *testing.T) {
		t.Parallel()
		base := okTree(t, map[string]string{"modules/a/fail-init-readonly": ""})
		tool, _ := fakeTool(t)
		var out bytes.Buffer
		require.ErrorIs(t, runInit(append(initArgs(t, base), "modules", "usecases"), &out, tool), errDirsFailed)
		assert.Contains(t, out.String(), "❌ tf-init が落ちたディレクトリ: "+filepath.Join(base, "modules/a")+"\n")
	})

	t.Run("道具を起動できなければ errTool で終了コードは 2", func(t *testing.T) {
		t.Parallel()
		base := okTree(t, nil)
		err := runInit(append(initArgs(t, base), "modules"), &bytes.Buffer{}, tooltest.Missing)
		require.ErrorIs(t, err, errTool)
		assert.Equal(t, 2, exitCode(err))
	})
}

func Test_initTargets(t *testing.T) {
	t.Parallel()

	t.Run("対象が無ければ何も起動しない", func(t *testing.T) {
		t.Parallel()
		tool, calls := fakeTool(t)
		require.NoError(t, initTargets("/d", nil, &bytes.Buffer{}, tool, nil))
		assert.Empty(t, calls())
	})

	t.Run("正本が無いときは root だけを起動でき、root でないディレクトリがあれば起動しない", func(t *testing.T) {
		t.Parallel()
		base := okTree(t, nil)
		tool, calls := fakeTool(t)
		require.NoError(t, initTargets("/d", []target{{rel: "env/dev", path: filepath.Join(base, "env", "dev")}}, &bytes.Buffer{}, tool, nil))
		assert.Len(t, calls(), 1)
		err := initTargets("/d", []target{{rel: "modules/a", path: filepath.Join(base, "modules", "a")}}, &bytes.Buffer{}, tool, nil)
		require.ErrorIs(t, err, errUsage)
		assert.Len(t, calls(), 1, "root でないディレクトリだけのときは起動しない")
	})

	t.Run("正本が無いときに root と root でないディレクトリが混ざれば、root も起動しない", func(t *testing.T) {
		t.Parallel()
		base := okTree(t, nil)
		tool, calls := fakeTool(t)
		targets := []target{{rel: "env/dev", path: filepath.Join(base, "env", "dev")}, {rel: "modules/a", path: filepath.Join(base, "modules", "a")}}
		require.ErrorIs(t, initTargets("/d", targets, &bytes.Buffer{}, tool, nil), errUsage)
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

// lintFixture は、AWS ruleset の版を報告する tflint --version の出力を持つ base、実行できる ruleset、その版を
// 宣言する mise.toml を作り、runLint の引数の頭を返す。
func lintFixture(t *testing.T, extra map[string]string) (base string, args []string) {
	t.Helper()
	files := map[string]string{"version.out": "TFLint version 0.66.0\n+ ruleset.aws (0.50.0)\n+ ruleset.terraform (0.15.0-bundled)\n"}
	for k, v := range extra {
		files[k] = v
	}
	base = okTree(t, files)
	ruleset := writeRuleset(t)
	mise := filepath.Join(t.TempDir(), "mise.toml")
	require.NoError(t, os.WriteFile(mise, []byte("[tools]\n\""+rulesetKey+"\" = \"0.50.0\"\n"), 0o600))

	return base, []string{"-base", base, "-config", "/c/.tflint.hcl", "-ruleset", ruleset, "-mise", mise}
}

// writeRuleset は、実行できる tflint-ruleset-aws を一時ディレクトリに置き、そのパスを返す。macOS の /var の
// symlink を解いておく（TFLINT_PLUGIN_DIR と比べるため）。
func writeRuleset(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	path := filepath.Join(dir, rulesetFile)
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"), 0o700))

	return path
}

func Test_runLint(t *testing.T) {
	t.Parallel()

	t.Run("ruleset の版を確かめてから、設定と ruleset の置き場を渡して各ディレクトリで TFLint を起動する", func(t *testing.T) {
		t.Parallel()
		base, args := lintFixture(t, nil)
		tool, calls := fakeTool(t)
		require.NoError(t, runLint(append(args, "modules", "usecases"), &bytes.Buffer{}, tool))
		plugins := filepath.Dir(args[5])
		assert.Equal(t, []string{
			base + " lint-version  /c/.tflint.hcl " + plugins,
			filepath.Join(base, "modules/a") + " lint  /c/.tflint.hcl " + plugins,
			filepath.Join(base, "usecases/b") + " lint  /c/.tflint.hcl " + plugins,
		}, calls())
	})

	t.Run("ruleset の版が宣言と違えば、ディレクトリを走らせずに errRuleset で終了コードは 2", func(t *testing.T) {
		t.Parallel()
		_, args := lintFixture(t, map[string]string{"version.out": "+ ruleset.aws (0.49.0)\n"})
		tool, calls := fakeTool(t)
		err := runLint(append(args, "modules"), &bytes.Buffer{}, tool)
		require.ErrorIs(t, err, errRuleset)
		assert.Equal(t, 2, exitCode(err))
		assert.Len(t, calls(), 1, "版の確かめだけが走った")
	})

	t.Run("版の確かめで道具を起動できなければ、ディレクトリを走らせずに errTool で終了コードは 2", func(t *testing.T) {
		t.Parallel()
		_, args := lintFixture(t, nil)
		tool, calls := fakeTool(t)
		cmdOf := func(ctx context.Context, d string, env []string, step string, _ ...string) *exec.Cmd {
			if step == stepLintVersion {
				return tooltest.Missing(ctx, d, env, step)
			}
			return tool(ctx, d, env, step)
		}
		err := runLint(append(args, "modules"), &bytes.Buffer{}, cmdOf)
		require.ErrorIs(t, err, errTool)
		assert.Equal(t, 2, exitCode(err))
		assert.Empty(t, calls())
	})

	t.Run("ディレクトリごとの TFLint を起動できなければ、残りを起動せずに errTool で終了コードは 2", func(t *testing.T) {
		t.Parallel()
		_, args := lintFixture(t, nil)
		tool, calls := fakeTool(t)
		cmdOf := func(ctx context.Context, d string, env []string, step string, _ ...string) *exec.Cmd {
			if step == stepLint {
				return tooltest.Missing(ctx, d, env, step)
			}
			return tool(ctx, d, env, step)
		}
		err := runLint(append(args, "modules", "usecases"), &bytes.Buffer{}, cmdOf)
		require.ErrorIs(t, err, errTool)
		assert.False(t, errors.Is(err, errDirsFailed))
		assert.Equal(t, 2, exitCode(err))
		assert.Len(t, calls(), 1, "版の確かめだけが走った")
	})

	t.Run("tflint の 1（設定や plugin の誤り）は指摘ではなく errToolFailed で、終了コード 2", func(t *testing.T) {
		t.Parallel()
		_, args := lintFixture(t, map[string]string{"usecases/b/rc-lint": "1"})
		tool, _ := fakeTool(t)
		var out bytes.Buffer
		err := runLint(append(args, "modules", "usecases"), &out, tool)
		require.ErrorIs(t, err, errToolFailed)
		assert.Equal(t, 2, exitCode(err))
		assert.NotContains(t, out.String(), "❌ tf-lint が落ちたディレクトリ")
	})

	t.Run("落ちたディレクトリを名指す", func(t *testing.T) {
		t.Parallel()
		base, args := lintFixture(t, map[string]string{"usecases/b/fail-lint": ""})
		tool, _ := fakeTool(t)
		var out bytes.Buffer
		require.ErrorIs(t, runLint(append(args, "modules", "usecases"), &out, tool), errDirsFailed)
		assert.Contains(t, out.String(), "❌ tf-lint が落ちたディレクトリ: "+filepath.Join(base, "usecases/b"))
	})

	t.Run("ruleset を置いたディレクトリでなければ、起動せずに errRuleset", func(t *testing.T) {
		t.Parallel()
		_, args := lintFixture(t, nil)
		args[5] = filepath.Join(t.TempDir(), rulesetFile)
		tool, calls := fakeTool(t)
		require.ErrorIs(t, runLint(append(args, "modules"), &bytes.Buffer{}, tool), errRuleset)
		assert.Empty(t, calls())
	})

	for missing, args := range map[string][]string{
		"-config":  {"-base", ".", "-ruleset", "r", "-mise", "m", "modules"},
		"-ruleset": {"-base", ".", "-config", "c", "-mise", "m", "modules"},
		"-mise":    {"-base", ".", "-config", "c", "-ruleset", "r", "modules"},
	} {
		t.Run(missing+" が無ければ errUsage", func(t *testing.T) {
			t.Parallel()
			tool, _ := fakeTool(t)

			require.ErrorIs(t, runLint(args, &bytes.Buffer{}, tool), errUsage)
		})
	}

	t.Run("mise.toml が ruleset を宣言しなければ、起動せずに errRuleset で終了コードは 2", func(t *testing.T) {
		t.Parallel()
		_, args := lintFixture(t, nil)
		require.NoError(t, os.WriteFile(args[7], []byte("[tools]\ngo = \"1.27.4\"\n"), 0o600))
		tool, calls := fakeTool(t)

		err := runLint(append(args, "modules"), &bytes.Buffer{}, tool)

		require.ErrorIs(t, err, errRuleset)
		assert.Equal(t, 2, exitCode(err))
		assert.Empty(t, calls())
	})

	t.Run("対象が0件なら起動せずに errNoTargets", func(t *testing.T) {
		t.Parallel()
		base := tree(t, map[string]string{"modules/.keep": ""})
		tool, calls := fakeTool(t)
		_, args := lintFixture(t, nil)
		args[1] = base
		require.ErrorIs(t, runLint(append(args, "modules"), &bytes.Buffer{}, tool), errNoTargets)
		assert.Empty(t, calls())
	})
}

func Test_declaredRuleset(t *testing.T) {
	t.Parallel()

	write := func(t *testing.T, content string) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "mise.toml")
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
		return path
	}

	t.Run("mise.toml が宣言する AWS ruleset の版を返す", func(t *testing.T) {
		t.Parallel()
		got, err := declaredRuleset(write(t, "[tools]\n\""+rulesetKey+"\" = \"0.50.0\"\n"))
		require.NoError(t, err)
		assert.Equal(t, "0.50.0", got)
	})

	t.Run("ほかの org の ruleset しか宣言していなければ errRuleset", func(t *testing.T) {
		t.Parallel()
		_, err := declaredRuleset(write(t, "[tools]\n\"github:other/tflint-ruleset-aws\" = \"0.50.0\"\n"))
		require.ErrorIs(t, err, errRuleset)
	})

	t.Run("mise.toml を読めなければ errRuleset", func(t *testing.T) {
		t.Parallel()
		_, err := declaredRuleset(filepath.Join(t.TempDir(), "mise.toml"))
		require.ErrorIs(t, err, errRuleset)
	})
}

func Test_rulesetDir(t *testing.T) {
	t.Parallel()

	t.Run("ruleset を含むディレクトリを返す", func(t *testing.T) {
		t.Parallel()
		ruleset := writeRuleset(t)
		got, err := rulesetDir(ruleset)
		require.NoError(t, err)
		assert.Equal(t, filepath.Dir(ruleset), got)
	})

	t.Run("名前が tflint-ruleset-aws でなければ errRuleset", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "tflint-ruleset-other")
		require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"), 0o700))
		_, err := rulesetDir(path)
		require.ErrorIs(t, err, errRuleset)
	})

	t.Run("無ければ errRuleset", func(t *testing.T) {
		t.Parallel()
		_, err := rulesetDir(filepath.Join(t.TempDir(), rulesetFile))
		require.ErrorIs(t, err, errRuleset)
	})

	t.Run("実行できなければ errRuleset", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), rulesetFile)
		require.NoError(t, os.WriteFile(path, []byte("x"), 0o600))
		_, err := rulesetDir(path)
		require.ErrorIs(t, err, errRuleset)
	})

	t.Run("ディレクトリなら errRuleset", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), rulesetFile)
		require.NoError(t, os.Mkdir(path, 0o700))
		_, err := rulesetDir(path)
		require.ErrorIs(t, err, errRuleset)
	})
}

func Test_checkRulesetVersion(t *testing.T) {
	t.Parallel()

	env := []string{"TFLINT_DISABLE_VERSION_CHECK=1"}
	tests := map[string]struct {
		files map[string]string
		say   string
	}{
		"報告した版が宣言と同じなら成功する":                      {map[string]string{"version.out": "+ ruleset.aws (0.50.0)\n"}, ""},
		"報告した版が宣言と違えば、両方の版を名指して落ちる":              {map[string]string{"version.out": "+ ruleset.aws (0.50.1)\n"}, "置かれた AWS ruleset は 0.50.1、mise.toml の宣言は 0.50.0 です"},
		"AWS ruleset を報告しなければ落ちる":                {map[string]string{"version.out": "+ ruleset.terraform (0.15.0-bundled)\n"}, "1行で報告しません"},
		"AWS ruleset を2行報告すれば落ちる":                {map[string]string{"version.out": "+ ruleset.aws (0.50.0)\n+ ruleset.aws (0.50.0)\n"}, "1行で報告しません"},
		"tflint --version が 0 以外なら終了コードを名指して落ちる": {map[string]string{"version.out": "+ ruleset.aws (0.50.0)\n", "rc-lint-version": "1"}, "tflint --version の終了コード 1"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			tool, _ := fakeTool(t)

			err := checkRulesetVersion(tool, tree(t, tt.files), env, "0.50.0")

			if tt.say == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, errRuleset)
			assert.Contains(t, err.Error(), tt.say)
		})
	}

	t.Run("道具を起動できなければ errTool", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, checkRulesetVersion(tooltest.Missing, tree(t, nil), env, "0.50.0"), errTool)
	})
}

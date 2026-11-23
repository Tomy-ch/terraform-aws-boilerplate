package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
			c := command("/x/d", []string{"A=1"}, step)
			assert.Equal(t, want, c.Args)
			assert.Equal(t, "/x/d", c.Dir)
			assert.Equal(t, []string{"A=1"}, c.Env)
		})
	}
}

func Test_toolEnv(t *testing.T) {
	t.Parallel()

	t.Run("AWS_ で始まる変数を外し、資格情報を探す先を塞ぎ、extra を足す", func(t *testing.T) {
		t.Parallel()
		got := toolEnv([]string{"PATH=/bin", "AWS_PROFILE=p", "AWS_ACCESS_KEY_ID=x", "XAWS_KEEP=1"}, "TF_DATA_DIR=/d")
		assert.Equal(t, []string{"PATH=/bin", "XAWS_KEEP=1",
			"AWS_SHARED_CREDENTIALS_FILE=/dev/null", "AWS_CONFIG_FILE=/dev/null", "AWS_EC2_METADATA_DISABLED=true", "CHECKPOINT_DISABLE=1",
			"TF_DATA_DIR=/d"}, got)
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
			err = runEach("tf-x", targets, &out, func(tg target) *exec.Cmd { return tool(tg.path, toolEnv(os.Environ()), stepTest) })
			assert.Len(t, calls(), 2, "落ちても残りのディレクトリを走らせる")
			if c.want == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, errDirsFailed)
			assert.Contains(t, out.String(), strings.ReplaceAll(c.want, "{base}", base))
		})
	}
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

func Test_runValidate(t *testing.T) {
	t.Parallel()

	t.Run("各ディレクトリの出力と終了コードを一覧に書き、落ちても全部走らせる", func(t *testing.T) {
		t.Parallel()
		base := okTree(t, map[string]string{"modules/a/validate.out": `{"valid":true}`, "usecases/b/fail-validate": ""})
		tool, calls := fakeTool(t)
		out := filepath.Join(t.TempDir(), "v")
		require.NoError(t, runValidate([]string{"-base", base, "-data", "/d", "-out", out, "modules", "usecases"}, &bytes.Buffer{}, tool))
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

	t.Run("-out が無ければ errUsage", func(t *testing.T) {
		t.Parallel()
		base := okTree(t, nil)
		tool, _ := fakeTool(t)
		require.ErrorIs(t, runValidate([]string{"-base", base, "-data", "/d", "modules"}, &bytes.Buffer{}, tool), errUsage)
	})
}

func Test_validateTargets(t *testing.T) {
	t.Parallel()

	t.Run("道具を起動できなければ errDirsFailed で、一覧を書かない", func(t *testing.T) {
		t.Parallel()
		base := okTree(t, nil)
		targets, err := requireDirs(scan{base: base, roots: []string{"modules"}, log: io.Discard})
		require.NoError(t, err)
		out := filepath.Join(t.TempDir(), "v")
		broken := func(d string, env []string, _ string) *exec.Cmd {
			c := exec.CommandContext(context.Background(), filepath.Join(d, "no-such-tool"))
			c.Dir, c.Env = d, env
			return c
		}
		require.ErrorIs(t, validateTargets("/d", out, targets, broken), errDirsFailed)
		assert.NoFileExists(t, filepath.Join(out, "manifest.tsv"))
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

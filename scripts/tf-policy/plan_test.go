package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun/tooltest"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const planOK = `{"@level":"info","type":"test_plan","test_plan":{"format_version":"1.2","provider_format_version":"1.0","provider_schemas":{"x":{}},"resource_changes":[]}}` + "\n"

func Test_command(t *testing.T) {
	t.Parallel()

	for step, want := range map[string][]string{
		stepInit:     {"terraform", "init", "-backend=false", "-input=false", "-lockfile=readonly"},
		stepPlan:     {"terraform", "test", "-filter=tests/policy-input.tftest.hcl", "-json", "-verbose"},
		stepVerify:   {"conftest", "verify", "--policy", "rego", "--data", "decl.json", "--data", "layer-decl.json", "-o", "json"},
		stepConftest: {"conftest", "test", "--combine", "--all-namespaces", "--no-color", "-o", "json", "--policy", "rego", "--data", "data.json", "--data", "layer.json", "--data", "out/provider-schema.json", "out/inputs"},
	} {
		t.Run(step, func(t *testing.T) {
			t.Parallel()
			c := command("/x", []string{"A=1"}, step)
			assert.Equal(t, want, c.Args)
			assert.Equal(t, "/x", c.Dir)
			assert.Equal(t, []string{"A=1"}, c.Env)
		})
	}

	t.Run("deprecated は PATH の provider-schema を起動する", func(t *testing.T) {
		t.Parallel()
		c := command("/x", nil, stepDeprecated)
		assert.Equal(t, []string{"provider-schema", "plan-deprecated", "-out", "out/provider-schema.json", "-list", "out/plans.txt"}, c.Args)
	})
}

func Test_listRoots(t *testing.T) {
	t.Parallel()

	t.Run("policy-input を持つ reference の root を名前の順に返す", func(t *testing.T) {
		t.Parallel()
		base := tree(t, map[string]string{
			"usecases/b/reference/tests/policy-input.tftest.hcl": "", "usecases/a/reference/tests/policy-input.tftest.hcl": "",
			"usecases/c/main.tf": "", "usecases/README.md": "",
		})
		got, err := listRoots(base)
		require.NoError(t, err)
		assert.Equal(t, []root{
			{name: "a", rel: "usecases/a/reference", path: filepath.Join(base, "usecases/a/reference")},
			{name: "b", rel: "usecases/b/reference", path: filepath.Join(base, "usecases/b/reference")},
		}, got)
	})

	t.Run("reference の root が0件なら errNoInput", func(t *testing.T) {
		t.Parallel()
		_, err := listRoots(tree(t, map[string]string{"modules/a/main.tf": ""}))
		require.ErrorIs(t, err, errNoInput)
		assert.Contains(t, err.Error(), "reference の root が0件")
	})

	t.Run("policy-input を持たない root があれば errFailed", func(t *testing.T) {
		t.Parallel()
		base := tree(t, map[string]string{"usecases/a/reference/main.tf": "", "usecases/b/reference/tests/policy-input.tftest.hcl": ""})
		_, err := listRoots(base)
		require.ErrorIs(t, err, errFailed)
		assert.Contains(t, err.Error(), "tests/policy-input.tftest.hcl を持たない reference の root: "+filepath.Join(base, "usecases/a/reference"))
	})

	t.Run("usecases を読めなければ errRead", func(t *testing.T) {
		t.Parallel()
		base := tree(t, map[string]string{"usecases": ""})
		_, err := listRoots(base)
		require.ErrorIs(t, err, errRead)
	})
}

func Test_errorMessages(t *testing.T) {
	t.Parallel()

	t.Run("error の行の本文だけを返す", func(t *testing.T) {
		t.Parallel()
		got := errorMessages([]byte(`{"@level":"info","@message":"a"}` + "\nnot json\n" + `{"@level":"error","@message":"b"}` + "\n"))
		assert.Equal(t, []string{"b"}, got)
	})
}

func Test_planRoots(t *testing.T) {
	t.Parallel()

	t.Run("root ごとに出力を置き、一覧を書き、落ちた root を最後まで走らせてから名指す", func(t *testing.T) {
		t.Parallel()
		base := tree(t, map[string]string{
			"usecases/a/reference/plan.out": planOK, "usecases/b/reference/fail-plan": "",
			"usecases/b/reference/plan.out": `{"@level":"error","@message":"b の誤り"}` + "\n",
		})
		roots := []root{
			{name: "a", rel: "usecases/a/reference", path: filepath.Join(base, "usecases/a/reference")},
			{name: "b", rel: "usecases/b/reference", path: filepath.Join(base, "usecases/b/reference")},
		}
		tool, calls := fakeTool(t)
		out := filepath.Join(t.TempDir(), "run")
		var w bytes.Buffer
		require.ErrorIs(t, planRoots(roots, "/d", out, &w, tool), errFailed)
		assert.Equal(t, []string{roots[0].path + " plan /d/usecases__a__reference", roots[1].path + " plan /d/usecases__b__reference"}, calls())
		assert.Equal(t, filepath.Join(out, "runs", "a.jsonl")+"\n", readText(t, filepath.Join(out, "plans.txt")))
		assert.Equal(t, planOK, readText(t, filepath.Join(out, "runs", "a.jsonl")))
		assert.Contains(t, w.String(), "b の誤り")
		assert.Contains(t, w.String(), "❌ tf-policy が落ちたディレクトリ: "+roots[1].path)
	})

	// files のキーは基点からの相対パス。
	threeRoots := func(t *testing.T, files map[string]string) []root {
		t.Helper()
		base := tree(t, files)
		var roots []root
		for _, n := range []string{"a", "b", "c"} {
			rel := "usecases/" + n + "/reference"
			roots = append(roots, root{name: n, rel: rel, path: filepath.Join(base, rel)})
		}
		return roots
	}
	unstartableAt := func(tool toolFunc, path string) toolFunc {
		return func(d string, env []string, step string) *exec.Cmd {
			if d == path {
				return tooltest.Missing(d, env, step)
			}
			return tool(d, env, step)
		}
	}

	t.Run("最初の root で道具を起動できなければ、残りを起動せず plans.txt を書かずに errTool で終了コードは 2", func(t *testing.T) {
		t.Parallel()
		roots := threeRoots(t, map[string]string{"usecases/a/reference/.keep": "", "usecases/b/reference/plan.out": planOK, "usecases/c/reference/.keep": ""})
		tool, calls := fakeTool(t)
		out := filepath.Join(t.TempDir(), "run")
		err := planRoots(roots, "/d", out, &bytes.Buffer{}, unstartableAt(tool, roots[0].path))
		require.ErrorIs(t, err, errTool)
		require.NotErrorIs(t, err, errFailed)
		assert.Equal(t, 2, exitCode(err))
		assert.Contains(t, err.Error(), roots[0].path+" の plan で打ち切りました")
		assert.NotContains(t, err.Error(), "それまでに落ちた")
		assert.Empty(t, calls(), "残りの root を起動しない")
		assert.NoFileExists(t, filepath.Join(out, "plans.txt"))
		assert.NoFileExists(t, filepath.Join(out, "runs", "a.jsonl"))
	})

	t.Run("先の root が 1 で落ち、次で道具を起動できなければ、先の失敗を文面にだけ名指し、plans.txt を書かずに errTool で終了コードは 2", func(t *testing.T) {
		t.Parallel()
		roots := threeRoots(t, map[string]string{
			"usecases/a/reference/fail-plan": "", "usecases/a/reference/plan.out": `{"@level":"error","@message":"a の誤り"}` + "\n",
			"usecases/c/reference/.keep": "",
		})
		tool, calls := fakeTool(t)
		out := filepath.Join(t.TempDir(), "run")
		var w bytes.Buffer
		err := planRoots(roots, "/d", out, &w, unstartableAt(tool, roots[1].path))
		require.ErrorIs(t, err, errTool)
		assert.False(t, errors.Is(err, errFailed), "落ちた root の sentinel を包まない（包むと終了コードが 1 になる）")
		assert.Equal(t, 2, exitCode(err))
		assert.Contains(t, err.Error(), roots[1].path+" の plan で打ち切りました（それまでに落ちた root: "+roots[0].path+"）")
		assert.Equal(t, []string{roots[0].path + " plan /d/usecases__a__reference"}, calls(), "打ち切った後の root を起動しない")
		assert.NoFileExists(t, filepath.Join(out, "plans.txt"))
		assert.FileExists(t, filepath.Join(out, "runs", "a.jsonl"), "0 以外の終了コードの root の出力は書く")
		assert.NoFileExists(t, filepath.Join(out, "runs", "b.jsonl"))
		assert.Contains(t, w.String(), "a の誤り")
	})

	t.Run("道具がシグナルで終われば、残りを起動せず plans.txt を書かずに errTool で終了コードは 2", func(t *testing.T) {
		t.Parallel()
		roots := threeRoots(t, map[string]string{"usecases/a/reference/.keep": "", "usecases/b/reference/.keep": "", "usecases/c/reference/.keep": ""})
		f := tooltest.New(t)
		f.Set(stepPlan, "kill", "")
		out := filepath.Join(t.TempDir(), "run")
		err := planRoots(roots, "/d", out, &bytes.Buffer{}, f.Tool())
		require.ErrorIs(t, err, errTool)
		assert.Contains(t, err.Error(), "がシグナルで終わりました")
		assert.Equal(t, 2, exitCode(err))
		assert.Len(t, f.Calls(), 1)
		assert.NoFileExists(t, filepath.Join(out, "plans.txt"))
	})

	t.Run("前の出力を消してから書く", func(t *testing.T) {
		t.Parallel()
		base := tree(t, map[string]string{"usecases/a/reference/plan.out": planOK})
		out := tree(t, map[string]string{"stale.txt": ""})
		tool, _ := fakeTool(t)
		require.NoError(t, planRoots([]root{{name: "a", rel: "usecases/a/reference", path: filepath.Join(base, "usecases/a/reference")}}, "/d", out, &bytes.Buffer{}, tool))
		assert.NoFileExists(t, filepath.Join(out, "stale.txt"))
	})
}

var canaryFlags = []string{"-canary-data", "/cd", "-canary-work", "tmp/cw", "-policy", "p", "-decl", "d.json", "-layer", "l.json", "-reference", "r"}

func passCanary(got *[]string) canaryFunc {
	return func(args []string, _ io.Writer, _ toolFunc) error {
		*got = args

		return nil
	}
}

func Test_runPlan(t *testing.T) {
	t.Parallel()

	planBase := func(t *testing.T) string {
		t.Helper()
		return tree(t, map[string]string{"usecases/a/reference/tests/policy-input.tftest.hcl": "", "usecases/a/reference/plan.out": planOK})
	}
	planArgs := func(base, out string) []string {
		return append([]string{"-base", base, "-data", "/d", "-out", out}, canaryFlags...)
	}

	t.Run("canary を本走査の前に走らせ、reference の root で plan を起動する", func(t *testing.T) {
		t.Parallel()
		root := tree(t, nil)
		tool, calls := fakeTool(t)
		var canaryArgs []string
		require.NoError(t, runPlan(planArgs(planBase(t), "tmp/run"), &bytes.Buffer{}, tool, passCanary(&canaryArgs), root))
		assert.Equal(t, []string{"-data", "/cd", "-work", "tmp/cw", "-policy", "p", "-decl", "d.json", "-layer", "l.json", "-reference", "r"}, canaryArgs, "-canary-work は検証した元の値のまま渡す")
		assert.Len(t, calls(), 1)
		assert.Equal(t, filepath.Join(root, "tmp", "run", "runs", "a.jsonl")+"\n", readText(t, filepath.Join(root, "tmp", "run", "plans.txt")))
	})

	t.Run("canary が落ちれば本走査を行わずに失敗する", func(t *testing.T) {
		t.Parallel()
		base := tree(t, map[string]string{"usecases/a/reference/tests/policy-input.tftest.hcl": ""})
		tool, calls := fakeTool(t)
		fail := func([]string, io.Writer, toolFunc) error { return xerrors.Wrap(errCanary, "x") }
		err := runPlan(planArgs(base, "tmp/run"), &bytes.Buffer{}, tool, fail, tree(t, nil))
		require.ErrorIs(t, err, errCanary)
		assert.Contains(t, err.Error(), "canary が落ちたので、本走査を行いません")
		assert.Empty(t, calls())
	})

	t.Run("canary を root の列挙より先に走らせ、root が0件か列挙できなければ本走査を起動しない", func(t *testing.T) {
		t.Parallel()
		for name, c := range map[string]struct {
			files map[string]string
			want  error
		}{
			"root が0件なら errNoInput":    {map[string]string{"x": ""}, errNoInput},
			"usecases を読めなければ errRead": {map[string]string{"usecases": ""}, errRead},
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				tool, calls := fakeTool(t)
				var canaryArgs []string
				require.ErrorIs(t, runPlan(planArgs(tree(t, c.files), "tmp/run"), &bytes.Buffer{}, tool, passCanary(&canaryArgs), tree(t, nil)), c.want)
				assert.NotNil(t, canaryArgs, "canary は走った")
				assert.Empty(t, calls())
			})
		}
	})

	t.Run("canary が落ちた理由の sentinel と終了コードを保ち、本走査を行わないことを添える", func(t *testing.T) {
		t.Parallel()
		for name, c := range map[string]struct {
			cause error
			code  int
		}{
			"canary の不成立は 1":        {errCanary, 1},
			"canary の引数の誤りは 2":      {errUsage, 2},
			"canary が読めないのは 2":      {errRead, 2},
			"canary の道具を起動できないのは 2": {errTool, 2},
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				tool, calls := fakeTool(t)
				fail := func([]string, io.Writer, toolFunc) error { return xerrors.Wrap(c.cause, "x") }
				err := runPlan(planArgs(planBase(t), "tmp/run"), &bytes.Buffer{}, tool, fail, tree(t, nil))
				require.ErrorIs(t, err, c.cause)
				assert.Equal(t, c.code, exitCode(err))
				assert.Contains(t, err.Error(), "canary が落ちたので、本走査を行いません")
				assert.Empty(t, calls())
			})
		}
	})

	t.Run("必須の引数が1つでも無ければ、canary も道具も起動せず何も消さずに errUsage で終了コードは 2", func(t *testing.T) {
		t.Parallel()
		for _, drop := range []string{"-canary-data", "-canary-work", "-policy", "-decl", "-layer", "-reference"} {
			t.Run(drop, func(t *testing.T) {
				t.Parallel()
				world, root, _ := workWorld(t)
				args := []string{"-base", planBase(t), "-data", "/d", "-out", "tmp/c/work"}
				for i := 0; i < len(canaryFlags); i += 2 {
					if canaryFlags[i] != drop {
						args = append(args, canaryFlags[i], canaryFlags[i+1])
					}
				}
				before := snapshot(t, world)
				tool, calls := fakeTool(t)
				var canaryArgs []string
				err := runPlan(args, &bytes.Buffer{}, tool, passCanary(&canaryArgs), root)
				require.ErrorIs(t, err, errUsage)
				assert.Equal(t, 2, exitCode(err))
				assert.Nil(t, canaryArgs, "canary を起動しない")
				assert.Empty(t, calls())
				assert.Equal(t, before, snapshot(t, world))
			})
		}
	})

	t.Run("引数の誤りは、canary も本走査も起動せずに errUsage", func(t *testing.T) {
		t.Parallel()
		tool, calls := fakeTool(t)
		var canaryArgs []string
		require.ErrorIs(t, runPlan(append([]string{"-base", ".", "-data", "rel", "-out", "tmp/o"}, canaryFlags...), &bytes.Buffer{}, tool, passCanary(&canaryArgs), tree(t, nil)), errUsage)
		require.ErrorIs(t, runPlan([]string{"-base", ".", "-data", "/d", "-out", "tmp/o"}, &bytes.Buffer{}, tool, passCanary(&canaryArgs), tree(t, nil)), errUsage)
		assert.Nil(t, canaryArgs)
		assert.Empty(t, calls())
	})

	// bad は、-out か -canary-work の一方だけを誤った値にした引数で runPlan を呼び、canary も道具も起動せず、world が
	// 前後で変わらないことを確かめる。
	bad := func(t *testing.T, flag, value string, mk func(root, outside string) []string) {
		t.Helper()
		world, root, outside := workWorld(t)
		switch value {
		case "<root>/keep":
			value = filepath.Join(root, "keep")
		case "<outside>":
			value = outside
		}
		before := snapshot(t, world)
		tool, calls := fakeTool(t)
		var canaryArgs []string
		args := mk(root, outside)
		for i := range args {
			if args[i] == "<bad>" {
				args[i] = value
			}
		}
		err := runPlan(args, &bytes.Buffer{}, tool, passCanary(&canaryArgs), root)
		require.ErrorIs(t, err, errUsage)
		assert.Equal(t, 2, exitCode(err))
		if value != "" {
			assert.Contains(t, err.Error(), fmt.Sprintf("%s %q", flag, value))
		}
		assert.Nil(t, canaryArgs, "canary を起動しない")
		assert.Empty(t, calls())
		assert.Equal(t, before, snapshot(t, world))
	}
	badValues := []string{
		"", ".", "tmp", "tmp/", "tmp/.", "<root>/keep", "<outside>", "keep", "tmp/../keep", "../x", "TMP/x",
		"tmp/out", "tmp/out/w", "tmp/self",
	}

	t.Run("-out だけが tmp/ の下の相対パスでなければ、canary も道具も起動せず何も消さずに、-out を名指す errUsage で終了コードは 2", func(t *testing.T) {
		t.Parallel()
		for _, v := range badValues {
			t.Run(v, func(t *testing.T) {
				t.Parallel()
				bad(t, "-out", v, func(string, string) []string {
					return append([]string{"-base", planBase(t), "-data", "/d", "-out", "<bad>"}, canaryFlags...)
				})
			})
		}
	})

	t.Run("-canary-work だけが tmp/ の下の相対パスでなければ、canary も道具も起動せず何も消さずに、-canary-work を名指す errUsage で終了コードは 2", func(t *testing.T) {
		t.Parallel()
		for _, v := range badValues {
			t.Run(v, func(t *testing.T) {
				t.Parallel()
				bad(t, "-canary-work", v, func(string, string) []string {
					return []string{"-base", planBase(t), "-data", "/d", "-out", "tmp/c/work", "-canary-data", "/cd", "-canary-work", "<bad>",
						"-policy", "p", "-decl", "d.json", "-layer", "l.json", "-reference", "r"}
				})
			})
		}
	})

	t.Run("-out を2度渡せば後の値を使う", func(t *testing.T) {
		t.Parallel()
		world, root, _ := workWorld(t)
		before := snapshot(t, world)
		tool, calls := fakeTool(t)
		var canaryArgs []string
		err := runPlan(append(planArgs(planBase(t), "tmp/c/work"), "-out", "../x"), &bytes.Buffer{}, tool, passCanary(&canaryArgs), root)
		require.ErrorIs(t, err, errUsage)
		assert.Contains(t, err.Error(), `-out "../x"`)
		assert.Nil(t, canaryArgs)
		assert.Empty(t, calls())
		assert.Equal(t, before, snapshot(t, world))

		require.NoError(t, runPlan(append(planArgs(planBase(t), "../x"), "-out", "tmp/c/work"), &bytes.Buffer{}, tool, passCanary(&canaryArgs), root))
		assert.FileExists(t, filepath.Join(root, "tmp", "c", "work", "plans.txt"))
	})

	t.Run("-out だけを作り直し、tmp/ 自身・兄弟・tmp/ の外を残す", func(t *testing.T) {
		t.Parallel()
		for _, out := range []string{"tmp/c/work", "./tmp/c/work"} {
			t.Run(out, func(t *testing.T) {
				t.Parallel()
				world, root, _ := workWorld(t)
				before := snapshot(t, world)
				tool, _ := fakeTool(t)
				var canaryArgs []string
				require.NoError(t, runPlan(planArgs(planBase(t), out), &bytes.Buffer{}, tool, passCanary(&canaryArgs), root))
				after := snapshot(t, world)
				for rel, sum := range before {
					if !strings.HasPrefix(rel, filepath.Join("root", "tmp", "c", "work")) {
						assert.Equal(t, sum, after[rel], rel)
					}
				}
				assert.NotContains(t, after, filepath.Join("root", "tmp", "c", "work", "old"), "前回の中身は消す")
				assert.Contains(t, after, filepath.Join("root", "tmp", "c", "work", "plans.txt"))
			})
		}
	})

	t.Run("root に tmp/ が無ければ作って通る", func(t *testing.T) {
		t.Parallel()
		root := tree(t, map[string]string{"keep/f": "keep"})
		tool, _ := fakeTool(t)
		var canaryArgs []string
		require.NoError(t, runPlan(planArgs(planBase(t), "tmp/run"), &bytes.Buffer{}, tool, passCanary(&canaryArgs), root))
		assert.FileExists(t, filepath.Join(root, "tmp", "run", "plans.txt"))
		assert.FileExists(t, filepath.Join(root, "keep", "f"))
	})
}

func Test_joinLines(t *testing.T) {
	t.Parallel()

	t.Run("行ごとに改行で終え、空なら空", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "a\nb\n", joinLines([]string{"a", "b"}))
		assert.Empty(t, joinLines(nil))
	})
}

func Test_readLines(t *testing.T) {
	t.Parallel()

	t.Run("空の行を除いて読む", func(t *testing.T) {
		t.Parallel()
		dir := tree(t, map[string]string{"l.txt": "a\n\nb\n"})
		got, err := readLines(filepath.Join(dir, "l.txt"))
		require.NoError(t, err)
		assert.Equal(t, []string{"a", "b"}, got)
	})

	t.Run("読めなければ errRead", func(t *testing.T) {
		t.Parallel()
		_, err := readLines(filepath.Join(t.TempDir(), "none"))
		require.ErrorIs(t, err, errRead)
	})
}

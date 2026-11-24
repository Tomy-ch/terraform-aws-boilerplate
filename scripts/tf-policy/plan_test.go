package main

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	t.Run("前の出力を消してから書く", func(t *testing.T) {
		t.Parallel()
		base := tree(t, map[string]string{"usecases/a/reference/plan.out": planOK})
		out := tree(t, map[string]string{"stale.txt": ""})
		tool, _ := fakeTool(t)
		require.NoError(t, planRoots([]root{{name: "a", rel: "usecases/a/reference", path: filepath.Join(base, "usecases/a/reference")}}, "/d", out, &bytes.Buffer{}, tool))
		assert.NoFileExists(t, filepath.Join(out, "stale.txt"))
	})
}

func Test_runPlan(t *testing.T) {
	t.Parallel()

	planBase := func(t *testing.T) string {
		t.Helper()
		return tree(t, map[string]string{"usecases/a/reference/tests/policy-input.tftest.hcl": "", "usecases/a/reference/plan.out": planOK})
	}

	t.Run("reference の root で plan を起動する", func(t *testing.T) {
		t.Parallel()
		root := tree(t, nil)
		tool, calls := fakeTool(t)
		require.NoError(t, runPlan([]string{"-base", planBase(t), "-data", "/d", "-out", "tmp/run"}, &bytes.Buffer{}, tool, root))
		assert.Len(t, calls(), 1)
		assert.Equal(t, filepath.Join(root, "tmp", "run", "runs", "a.jsonl")+"\n", readText(t, filepath.Join(root, "tmp", "run", "plans.txt")))
	})

	t.Run("root が0件なら起動せずに errNoInput", func(t *testing.T) {
		t.Parallel()
		tool, calls := fakeTool(t)
		require.ErrorIs(t, runPlan([]string{"-base", tree(t, map[string]string{"x": ""}), "-data", "/d", "-out", "tmp/run"}, &bytes.Buffer{}, tool, tree(t, nil)), errNoInput)
		assert.Empty(t, calls())
	})

	t.Run("引数の誤りは errUsage", func(t *testing.T) {
		t.Parallel()
		tool, _ := fakeTool(t)
		require.ErrorIs(t, runPlan([]string{"-base", ".", "-data", "rel", "-out", "tmp/o"}, &bytes.Buffer{}, tool, tree(t, nil)), errUsage)
		require.ErrorIs(t, runPlan([]string{"-data", "/d", "-out", "tmp/o"}, &bytes.Buffer{}, tool, tree(t, nil)), errUsage)
	})

	t.Run("-out が tmp/ の下の相対パスでなければ、何も消さず道具も起動せずに、-out を名指す errUsage で終了コードは 2", func(t *testing.T) {
		t.Parallel()
		for _, out := range []string{
			"", ".", "tmp", "tmp/", "tmp/.", "<root>/keep", "<outside>", "keep", "tmp/../keep", "../x", "TMP/x",
			"tmp/out", "tmp/out/w", "tmp/self",
		} {
			t.Run(out, func(t *testing.T) {
				t.Parallel()
				world, root, outside := workWorld(t)
				switch out {
				case "<root>/keep":
					out = filepath.Join(root, "keep")
				case "<outside>":
					out = outside
				}
				before := snapshot(t, world)
				tool, calls := fakeTool(t)
				err := runPlan([]string{"-base", planBase(t), "-data", "/d", "-out", out}, &bytes.Buffer{}, tool, root)
				require.ErrorIs(t, err, errUsage)
				assert.Equal(t, 2, exitCode(err))
				if out != "" {
					assert.Contains(t, err.Error(), fmt.Sprintf("-out %q", out))
				}
				assert.Empty(t, calls())
				assert.Equal(t, before, snapshot(t, world))
			})
		}
	})

	t.Run("-out を2度渡せば後の値を使う", func(t *testing.T) {
		t.Parallel()
		world, root, _ := workWorld(t)
		before := snapshot(t, world)
		tool, calls := fakeTool(t)
		err := runPlan([]string{"-base", planBase(t), "-data", "/d", "-out", "tmp/c/work", "-out", "../x"}, &bytes.Buffer{}, tool, root)
		require.ErrorIs(t, err, errUsage)
		assert.Contains(t, err.Error(), `-out "../x"`)
		assert.Empty(t, calls())
		assert.Equal(t, before, snapshot(t, world))

		require.NoError(t, runPlan([]string{"-base", planBase(t), "-data", "/d", "-out", "../x", "-out", "tmp/c/work"}, &bytes.Buffer{}, tool, root))
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
				require.NoError(t, runPlan([]string{"-base", planBase(t), "-data", "/d", "-out", out}, &bytes.Buffer{}, tool, root))
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
		require.NoError(t, runPlan([]string{"-base", planBase(t), "-data", "/d", "-out", "tmp/run"}, &bytes.Buffer{}, tool, root))
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

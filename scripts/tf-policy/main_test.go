package main

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

func Test_run(t *testing.T) {
	t.Parallel()

	t.Run("サブコマンドが無いか知らなければ errUsage", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, run(nil, &bytes.Buffer{}, command, t.TempDir()), errUsage)
		require.ErrorIs(t, run([]string{"apply"}, &bytes.Buffer{}, command, t.TempDir()), errUsage)
	})

	t.Run("各サブコマンドへ渡す", func(t *testing.T) {
		t.Parallel()
		for _, sub := range []string{"rego-check", "verify-result", "plan", "extract"} {
			t.Run(sub, func(t *testing.T) {
				t.Parallel()
				var out bytes.Buffer
				require.ErrorIs(t, run([]string{sub, "-no-such-flag"}, &out, command, t.TempDir()), errUsage)
				assert.Contains(t, out.String(), "-no-such-flag", "そのサブコマンドの flag の解釈に届いている")
			})
		}
	})

	t.Run("plan は root を -out・-canary-work の検証と canary へ渡す", func(t *testing.T) {
		t.Parallel()
		ref := tree(t, map[string]string{"versions.tf": "", ".terraform.lock.hcl": "", "decl.json": "{}", "layer.json": layerWithNulls})
		base := tree(t, map[string]string{"usecases/a/reference/tests/policy-input.tftest.hcl": ""})
		plan := func(root, out, work string, tool toolFunc) error {
			return run([]string{"plan", "-base", base, "-data", "/d", "-out", out, "-canary-data", t.TempDir(), "-canary-work", work,
				"-policy", policyTree(t), "-decl", filepath.Join(ref, "decl.json"), "-layer", filepath.Join(ref, "layer.json"), "-reference", ref},
				&bytes.Buffer{}, tool, root)
		}

		world, root, _ := workWorld(t)
		before := snapshot(t, world)
		tool, calls := fakeTool(t)
		for flag, args := range map[string][2]string{"-out": {"tmp/out/w", "tmp/c/work"}, "-canary-work": {"tmp/c/work", "tmp/out/w"}} {
			err := plan(root, args[0], args[1], tool)
			require.ErrorIs(t, err, errUsage, "root の tmp/out は外を指す")
			assert.Contains(t, err.Error(), flag+` "tmp/out/w"`)
		}
		assert.Empty(t, calls())
		assert.Equal(t, before, snapshot(t, world))

		require.ErrorIs(t, plan(root, "tmp/run", "tmp/c/work", tool), errCanary, "偽の道具では canary が成り立たない")
		assert.DirExists(t, filepath.Join(root, "tmp", "c", "work", "ok"), "canary は root の下に建てる")
	})
}

func Test_report(t *testing.T) {
	t.Parallel()

	t.Run("成功なら何も書かず 0", func(t *testing.T) {
		t.Parallel()
		var out bytes.Buffer
		assert.Equal(t, 0, report(nil, &out))
		assert.Empty(t, out.String())
	})

	t.Run("エラーを1行で書き、終了コードを返す", func(t *testing.T) {
		t.Parallel()
		var out bytes.Buffer
		assert.Equal(t, 1, report(xerrors.Wrap(errFailed, "x"), &out))
		assert.Equal(t, "❌ tf-policy: x: 検査が落ちました\n", out.String())
	})
}

func Test_exitCode(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		err  error
		want int
	}{
		"成功は 0":           {nil, 0},
		"落ちた検査は 1":        {xerrors.Wrap(errFailed, "x"), 1},
		"canary の食い違いは 1": {xerrors.Wrap(errCanary, "x"), 1},
		"0件は 2":           {xerrors.Wrap(errNoInput, "x"), 2},
		"読めない入力は 2":       {xerrors.Wrap(errRead, "x"), 2},
		"使い方の誤りは 2":       {xerrors.Wrap(errUsage, "x"), 2},
		"道具を起動できない・シグナルで終わったは 2": {xerrors.Wrap(errTool, "x"), 2},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, exitCode(c.err))
		})
	}
}

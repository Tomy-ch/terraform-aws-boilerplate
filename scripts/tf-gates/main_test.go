package main

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_run(t *testing.T) {
	t.Parallel()

	t.Run("サブコマンドが無ければ errUsage", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, run(nil, &bytes.Buffer{}, command, t.TempDir()), errUsage)
	})

	t.Run("知らないサブコマンドは errUsage", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, run([]string{"plan"}, &bytes.Buffer{}, command, t.TempDir()), errUsage)
	})

	t.Run("各サブコマンドへ渡す", func(t *testing.T) {
		t.Parallel()
		for _, sub := range []string{"dirs", "none", "fmt", "init", "validate", "result", "test", "lint"} {
			t.Run(sub, func(t *testing.T) {
				t.Parallel()
				var out bytes.Buffer
				err := run([]string{sub, "-no-such-flag"}, &out, command, t.TempDir())
				require.ErrorIs(t, err, errUsage)
				assert.Contains(t, out.String(), "-no-such-flag", "そのサブコマンドの flag の解釈に届いている")
			})
		}
	})
	t.Run("validate は root を -canary-work の検証と canary へ渡す", func(t *testing.T) {
		t.Parallel()
		ref := tree(t, map[string]string{"versions.tf": "terraform {}\n", ".terraform.lock.hcl": "# lock\n"})
		validate := func(root, work string) error {
			return run([]string{"validate", "-base", okTree(t, nil), "-data", "/d", "-out", filepath.Join(t.TempDir(), "v"),
				"-canary-data", t.TempDir(), "-canary-work", work, "-canary-reference", ref, "modules"}, &bytes.Buffer{}, canaryTool(t, ""), root)
		}

		world, root, _ := workWorld(t)
		before := snapshot(t, world)
		err := validate(root, "tmp/out/w")
		require.ErrorIs(t, err, errUsage, "root の tmp/out は外を指す")
		assert.Contains(t, err.Error(), `-canary-work "tmp/out/w"`)
		assert.Equal(t, before, snapshot(t, world))

		require.NoError(t, validate(root, "tmp/c/work"))
		assert.DirExists(t, filepath.Join(root, "tmp", "c", "work", "clean"), "canary は root の下に建てる")
	})
}

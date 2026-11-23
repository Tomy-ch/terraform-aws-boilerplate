package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var roots3 = []string{"usecases", "modules", "env"}

func Test_listDirs(t *testing.T) {
	t.Parallel()

	t.Run(".tf を持つディレクトリを辞書順に返し、tests と .terraform の下を除く", func(t *testing.T) {
		t.Parallel()
		base := tree(t, map[string]string{
			"usecases/b/main.tf": "", "usecases/b/tests/setup/main.tf": "", "usecases/b/README.md": "",
			"modules/a/x.tf": "", "modules/a/y.tf": "", "modules/a/.terraform/m/main.tf": "",
			"env/dev/main.tf": "", "env/dev/identity/main.tf": "", "env/only.md/x.md": "",
		})
		got, err := listDirs(base, roots3)
		require.NoError(t, err)
		var rels []string
		for _, g := range got {
			rels = append(rels, g.rel)
			assert.Equal(t, filepath.Join(base, g.rel), g.path)
		}
		assert.Equal(t, []string{"env/dev", "env/dev/identity", "modules/a", "usecases/b"}, rels)
	})

	t.Run("setup module だけなら0件", func(t *testing.T) {
		t.Parallel()
		base := tree(t, map[string]string{"modules/a/tests/setup/main.tf": "", "usecases/.keep": "", "env/.keep": ""})
		got, err := listDirs(base, roots3)
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("無い root は飛ばす", func(t *testing.T) {
		t.Parallel()
		base := tree(t, map[string]string{"modules/a/x.tf": ""})
		got, err := listDirs(base, []string{"modules", "usecases"})
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, "modules/a", got[0].rel)
	})

	t.Run("root がファイルなら errMissingRoot", func(t *testing.T) {
		t.Parallel()
		base := tree(t, map[string]string{"modules/a/x.tf": "", "env": ""})
		_, err := listDirs(base, []string{"modules", "env"})
		require.ErrorIs(t, err, errMissingRoot)
	})

	t.Run("読めないディレクトリは errWalk", func(t *testing.T) {
		t.Parallel()
		if os.Geteuid() == 0 {
			t.Skip("特権実行では読めないディレクトリも読めるため検証できない")
		}
		base := tree(t, map[string]string{"modules/a/sub/x.tf": ""})
		sub := filepath.Join(base, "modules/a/sub")
		require.NoError(t, os.Chmod(sub, 0o000))
		t.Cleanup(func() { _ = os.Chmod(sub, 0o750) })
		_, err := listDirs(base, []string{"modules"})
		require.ErrorIs(t, err, errWalk)
	})
}

func Test_requireDirs(t *testing.T) {
	t.Parallel()

	t.Run("0件は errNoTargets", func(t *testing.T) {
		t.Parallel()
		base := tree(t, map[string]string{"modules/a/README.md": ""})
		_, err := requireDirs(base, []string{"modules"})
		require.ErrorIs(t, err, errNoTargets)
		assert.Contains(t, err.Error(), "対象ディレクトリが0件")
	})

	t.Run("在れば返す", func(t *testing.T) {
		t.Parallel()
		base := tree(t, map[string]string{"modules/a/x.tf": ""})
		got, err := requireDirs(base, []string{"modules"})
		require.NoError(t, err)
		assert.Len(t, got, 1)
	})
}

func Test_isRoot(t *testing.T) {
	t.Parallel()

	for rel, want := range map[string]bool{
		"env/dev":                true,
		"env/dev/identity":       true,
		"usecases/x/reference":   true,
		"env/dev/other":          false,
		"env":                    false,
		"usecases/x":             false,
		"usecases/x/reference/m": false,
		"modules/a":              false,
		"modules/x/reference":    false,
	} {
		t.Run(rel, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, want, isRoot(rel))
		})
	}
}

func Test_dataDir(t *testing.T) {
	t.Parallel()

	t.Run("_ を先にエスケープし、/ を __ にする", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, filepath.Join("/d", "modules__a_u__b"), dataDir("/d", "modules/a_/b"))
	})

	t.Run("_ と / の並びが違えば別の名前になる", func(t *testing.T) {
		t.Parallel()
		assert.NotEqual(t, dataDir("/d", "modules/a_/b"), dataDir("/d", "modules/a/_b"))
	})
}

func Test_hasTests(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		files map[string]string
		want  bool
	}{
		"直下に持つ":          {files: map[string]string{"m/a.tftest.hcl": ""}, want: true},
		"tests/ に持つ":     {files: map[string]string{"m/tests/a.tftest.hcl": ""}, want: true},
		"持たない":           {files: map[string]string{"m/main.tf": "", "m/tests/x.tf": ""}, want: false},
		"深い所は数えない":       {files: map[string]string{"m/tests/s/a.tftest.hcl": ""}, want: false},
		"名前のディレクトリは数えない": {files: map[string]string{"m/a.tftest.hcl/x": ""}, want: false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			base := tree(t, c.files)
			got, err := hasTests(filepath.Join(base, "m"))
			require.NoError(t, err)
			assert.Equal(t, c.want, got)
		})
	}
}

func Test_parseCommon(t *testing.T) {
	t.Parallel()

	t.Run("-base と root を読み、root を正規化する", func(t *testing.T) {
		t.Parallel()
		base, roots, err := parseCommon("x", []string{"-base", "b", "./modules", "env/"}, &bytes.Buffer{}, nil)
		require.NoError(t, err)
		assert.Equal(t, "b", base)
		assert.Equal(t, []string{"modules", "env"}, roots)
	})

	for name, args := range map[string][]string{
		"-base が無い": {"modules"},
		"root が無い":  {"-base", "b"},
		"知らないフラグ":   {"-x"},
	} {
		t.Run(name+"は errUsage", func(t *testing.T) {
			t.Parallel()
			_, _, err := parseCommon("x", args, &bytes.Buffer{}, nil)
			require.ErrorIs(t, err, errUsage)
		})
	}
}

func Test_runDirs(t *testing.T) {
	t.Parallel()

	t.Run("対象を1行ずつ出す", func(t *testing.T) {
		t.Parallel()
		base := tree(t, map[string]string{"modules/a/x.tf": "", "modules/b/x.tf": ""})
		var out bytes.Buffer
		require.NoError(t, runDirs([]string{"-base", base, "modules"}, &out))
		assert.Equal(t, filepath.Join(base, "modules/a")+"\n"+filepath.Join(base, "modules/b")+"\n", out.String())
	})

	t.Run("0件は errNoTargets", func(t *testing.T) {
		t.Parallel()
		base := tree(t, map[string]string{"modules/.keep": ""})
		require.ErrorIs(t, runDirs([]string{"-base", base, "modules"}, &bytes.Buffer{}), errNoTargets)
	})
}

func Test_runNone(t *testing.T) {
	t.Parallel()

	t.Run("0件なら成功する", func(t *testing.T) {
		t.Parallel()
		base := tree(t, map[string]string{"modules/a/tests/setup/main.tf": ""})
		require.NoError(t, runNone([]string{"-base", base, "modules"}, &bytes.Buffer{}))
	})

	t.Run("対象が在れば errTargets", func(t *testing.T) {
		t.Parallel()
		base := tree(t, map[string]string{"modules/a/x.tf": ""})
		require.ErrorIs(t, runNone([]string{"-base", base, "modules"}, &bytes.Buffer{}), errTargets)
	})

	t.Run("root が無ければ対象0件として成功する", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, runNone([]string{"-base", t.TempDir(), "modules"}, &bytes.Buffer{}))
	})

	t.Run("root がファイルなら errMissingRoot（skip しない）", func(t *testing.T) {
		t.Parallel()
		base := tree(t, map[string]string{"modules": ""})
		require.ErrorIs(t, runNone([]string{"-base", base, "modules"}, &bytes.Buffer{}), errMissingRoot)
	})

	t.Run("引数の誤りは errUsage", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, runNone(nil, &bytes.Buffer{}), errUsage)
	})
}

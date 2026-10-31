package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func identityModule(source, env string) string {
	return "module \"deployment_identity\" {\n  source = " + source + "\n  env    = " + env + "\n}\n"
}

func Test_identityEnvs(t *testing.T) {
	t.Parallel()

	t.Run("deployment-identity を呼ぶ root の env を重複なく昇順で返す", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		writeFile(t, root, "env/b/identity/main.tf", identityModule(`"../../../usecases/deployment-identity"`, `"b"`))
		writeFile(t, root, "env/a/identity/main.tf", identityModule(`"../../../usecases/deployment-identity"`, `"a"`))
		writeFile(t, root, "env/a/identity/again.tf", identityModule(`"../../../usecases/deployment-identity"`, `"a"`))
		writeFile(t, root, "env/a/identity/README.md", "not terraform")
		got, err := identityEnvs(root)
		require.NoError(t, err)
		assert.Equal(t, []string{"a", "b"}, got)
	})

	t.Run("deployment-identity を呼ぶ root が無ければ失敗する", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		writeFile(t, root, "env/a/identity/main.tf", identityModule(`"../../../usecases/state-backend"`, `"a"`))
		_, err := identityEnvs(root)
		require.ErrorIs(t, err, errNoIdentityRoot)
	})

	t.Run("env/ が無ければ失敗する", func(t *testing.T) {
		t.Parallel()
		_, err := identityEnvs(t.TempDir())
		require.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("読めない .tf が1つでもあれば失敗する", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		writeFile(t, root, "env/a/identity/main.tf", identityModule(`"../../../usecases/deployment-identity"`, `"a"`))
		writeFile(t, root, "env/a/identity/broken.tf", "module \"x\" {")
		_, err := identityEnvs(root)
		require.ErrorIs(t, err, errTerraform)
	})
}

func Test_moduleEnvs(t *testing.T) {
	t.Parallel()

	target := func(dir string) string { return filepath.Join(dir, identityUseCaseDir) }

	t.Run("source が deployment-identity を指す module だけの env を返す", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		p := writeFile(t, root, "env/a/identity/main.tf",
			identityModule(`"../../../usecases/state-backend"`, `"s"`)+
				identityModule(`"../../../usecases/deployment-identity"`, `"a"`)+
				"resource \"x\" \"y\" {}\n")
		got, err := moduleEnvs(p, target(root))
		require.NoError(t, err)
		assert.Equal(t, []string{"a"}, got)
	})

	cases := []struct {
		name string
		body string
	}{
		{"HCL として読めなければ失敗する", "module \"x\" {"},
		{"source が無い module は失敗する", "module \"x\" {\n  env = \"a\"\n}\n"},
		{"source が変数なら失敗する", identityModule(`var.source`, `"a"`)},
		{"env が無ければ失敗する", "module \"x\" {\n  source = \"../../../usecases/deployment-identity\"\n}\n"},
		{"env が変数なら失敗する", identityModule(`"../../../usecases/deployment-identity"`, `var.env`)},
		{"env が文字列でなければ失敗する", identityModule(`"../../../usecases/deployment-identity"`, `1`)},
		{"env が式で組まれていれば失敗する", identityModule(`"../../../usecases/deployment-identity"`, `"${local.env}"`)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			p := writeFile(t, root, "env/a/identity/main.tf", tc.body)
			_, err := moduleEnvs(p, target(root))
			require.ErrorIs(t, err, errTerraform)
		})
	}

	t.Run("ファイルを読めなければ失敗する", func(t *testing.T) {
		t.Parallel()
		_, err := moduleEnvs(filepath.Join(t.TempDir(), "none.tf"), "x")
		require.ErrorIs(t, err, os.ErrNotExist)
	})
}

func Test_literalString(t *testing.T) {
	t.Parallel()

	body := func(t *testing.T, src string) *hclsyntax.Body {
		t.Helper()
		file, diags := hclsyntax.ParseConfig([]byte(src), "t.tf", hcl.InitialPos)
		require.False(t, diags.HasErrors())
		b, ok := file.Body.(*hclsyntax.Body)
		require.True(t, ok)
		return b
	}

	t.Run("リテラルの文字列を返す", func(t *testing.T) {
		t.Parallel()
		got, err := literalString(body(t, `env = "admin"`), "env")
		require.NoError(t, err)
		assert.Equal(t, "admin", got)
	})

	cases := []struct {
		name string
		src  string
	}{
		{"属性が無ければ失敗する", `other = "x"`},
		{"null なら失敗する", `env = null`},
		{"数なら失敗する", `env = 1`},
		{"変数を参照していれば失敗する", `env = var.env`},
		{"関数を呼んでいれば失敗する", `env = lower("A")`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := literalString(body(t, tc.src), "env")
			require.ErrorIs(t, err, errTerraform)
		})
	}
}

func Test_checkIdentityEnvironments(t *testing.T) {
	t.Parallel()

	declared := func(names ...string) []environment {
		out := make([]environment, 0, len(names))
		for _, n := range names {
			out = append(out, environment{Name: n})
		}
		return out
	}

	t.Run("各 env の3つが宣言に在り、宣言の3つがどれも env に当たれば通る", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, checkIdentityEnvironments([]string{"admin"},
			declared("admin-plan", "admin-apply", "admin-apply-identity", "copilot")))
	})

	t.Run("信頼条件が名指す environment が宣言に無ければ失敗する", func(t *testing.T) {
		t.Parallel()
		// 宣言に無い名前を workflow が参照すると、GitHub は保護なしの environment を作る。
		err := checkIdentityEnvironments([]string{"admin", "dev"},
			declared("admin-plan", "admin-apply", "admin-apply-identity"))
		require.ErrorIs(t, err, errEnvironmentMismatch)
		assert.Contains(t, err.Error(), "environment dev-plan が宣言にありません; environment dev-apply が宣言にありません; environment dev-apply-identity が宣言にありません")
	})

	t.Run("どの env にも当たらない宣言は失敗する", func(t *testing.T) {
		t.Parallel()
		err := checkIdentityEnvironments([]string{"admin"},
			declared("admin-plan", "admin-apply", "admin-apply-identity", "old-apply"))
		require.ErrorIs(t, err, errEnvironmentMismatch)
		assert.Contains(t, err.Error(), "environment old-apply に当たる deployment-identity の env がありません")
	})
}

func Test_identityBase(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		base   string
		wantOK bool
	}{
		{"admin-plan", "admin", true},
		{"admin-apply", "admin", true},
		{"admin-apply-identity", "admin", true},
		{"x-identity-apply", "x-identity", true},
		{"copilot", "", false},
		{"-plan", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			base, ok := identityBase(tc.name)
			assert.Equal(t, tc.wantOK, ok)
			assert.Equal(t, tc.base, base)
		})
	}
}

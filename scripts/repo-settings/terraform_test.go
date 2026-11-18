package main

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/branchdecl"
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

	t.Run(".terraform の下は読まない", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		writeFile(t, root, "env/a/identity/main.tf", identityModule(`"../../../usecases/deployment-identity"`, `"a"`))
		writeFile(t, root, "env/a/identity/.terraform/modules/x/main.tf", identityModule(`var.source`, `"z"`))
		got, err := identityEnvs(root)
		require.NoError(t, err)
		assert.Equal(t, []string{"a"}, got)
	})

	unreadable := []struct {
		name  string
		setup func(t *testing.T, root string)
	}{
		// 読み飛ばすと、別の root が見つかる限り緑のまま、その root の env が検査から落ちる。
		{"JSON の構成があれば失敗する", func(t *testing.T, root string) {
			writeFile(t, root, "env/b/identity/main.tf.json", `{"module": {}}`)
		}},
		{"symlink があれば失敗する", func(t *testing.T, root string) {
			require.NoError(t, os.Symlink(filepath.Join(root, "env/a"), filepath.Join(root, "env/c")))
		}},
		{"別の module を経由した呼び出しは件数が食い違って失敗する", func(t *testing.T, root string) {
			writeFile(t, root, "env/b/identity/main.tf", "module \"wrap\" {\n  source = \"../../../modules/wrap\"\n  # usecases/deployment-identity を env = \"b\" で呼ぶ\n}\n")
		}},
		{"相対パスでない source は件数が食い違って失敗する", func(t *testing.T, root string) {
			writeFile(t, root, "env/b/identity/main.tf", identityModule(`"git::https://example.com/x.git//usecases/deployment-identity"`, `"b"`))
		}},
	}
	for _, tc := range unreadable {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeFile(t, root, "env/a/identity/main.tf", identityModule(`"../../../usecases/deployment-identity"`, `"a"`))
			tc.setup(t, root)
			_, err := identityEnvs(root)
			require.ErrorIs(t, err, errTerraform)
		})
	}

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
		got, mentioned, err := moduleEnvs(p, target(root))
		require.NoError(t, err)
		assert.Equal(t, []string{"a"}, got)
		assert.Equal(t, 1, mentioned)
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
		{"env が空なら失敗する", identityModule(`"../../../usecases/deployment-identity"`, `""`)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			p := writeFile(t, root, "env/a/identity/main.tf", tc.body)
			_, _, err := moduleEnvs(p, target(root))
			require.ErrorIs(t, err, errTerraform)
		})
	}

	t.Run("ファイルを読めなければ失敗する", func(t *testing.T) {
		t.Parallel()
		_, _, err := moduleEnvs(filepath.Join(t.TempDir(), "none.tf"), "x")
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
		{"空の文字列なら失敗する", `env = ""`},
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

	t.Run("各 env の5つが宣言に在り、宣言の5つがどれも env に当たれば通る", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, checkIdentityEnvironments([]string{"admin"},
			declared("admin-plan", "admin-apply", "admin-apply-identity", "admin-unlock-identity", "admin-unlock", "copilot")))
	})

	t.Run("2つの env がそれぞれ5つを持てば通る", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, checkIdentityEnvironments([]string{"admin", "security"},
			declared("admin-plan", "admin-apply", "admin-apply-identity", "admin-unlock-identity", "admin-unlock",
				"security-plan", "security-apply", "security-apply-identity", "security-unlock-identity", "security-unlock")))
	})

	t.Run("2つ目の env の unlock が宣言に無ければ失敗する", func(t *testing.T) {
		t.Parallel()
		err := checkIdentityEnvironments([]string{"admin", "security"},
			declared("admin-plan", "admin-apply", "admin-apply-identity", "admin-unlock-identity", "admin-unlock",
				"security-plan", "security-apply", "security-apply-identity", "security-unlock-identity"))
		require.ErrorIs(t, err, errEnvironmentMismatch)
		assert.Contains(t, err.Error(), "environment security-unlock が宣言にありません")
	})

	t.Run("信頼条件が名指す environment が宣言に無ければ失敗する", func(t *testing.T) {
		t.Parallel()
		// 宣言に無い名前を workflow が参照すると、GitHub は保護なしの environment を作る。
		err := checkIdentityEnvironments([]string{"admin", "dev"},
			declared("admin-plan", "admin-apply", "admin-apply-identity", "admin-unlock-identity", "admin-unlock"))
		require.ErrorIs(t, err, errEnvironmentMismatch)
		assert.Contains(t, err.Error(), "environment dev-plan が宣言にありません; environment dev-apply が宣言にありません; environment dev-apply-identity が宣言にありません; environment dev-unlock-identity が宣言にありません; environment dev-unlock が宣言にありません")
	})

	t.Run("どの env にも当たらない宣言は失敗する", func(t *testing.T) {
		t.Parallel()
		err := checkIdentityEnvironments([]string{"admin"},
			declared("admin-plan", "admin-apply", "admin-apply-identity", "admin-unlock-identity", "admin-unlock", "old-apply"))
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
		{"admin-unlock-identity", "admin", true},
		{"admin-unlock", "admin", true},
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

func Test_splitIdentity(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name, base, suffix string
		wantOK             bool
	}{
		{"admin-plan", "admin", "-plan", true},
		{"admin-apply", "admin", "-apply", true},
		{"admin-apply-identity", "admin", "-apply-identity", true},
		{"admin-unlock-identity", "admin", "-unlock-identity", true},
		{"admin-unlock", "admin", "-unlock", true},
		{"admin-unlocker", "", "", false},
		{"-plan", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			base, suffix, ok := splitIdentity(tc.name)
			assert.Equal(t, tc.wantOK, ok)
			assert.Equal(t, tc.base, base)
			assert.Equal(t, tc.suffix, suffix)
		})
	}

	t.Run("写しと正本のずれ", func(t *testing.T) {
		t.Parallel()

		src, err := os.ReadFile(filepath.Join("..", "..", identityUseCaseDir, "internal", "github-oidc", "main.tf"))
		require.NoError(t, err)
		var declared []string
		for _, m := range regexp.MustCompile(`environment\s*=\s*"\$\{var\.env\}(-[a-z0-9-]+)"`).FindAllStringSubmatch(string(src), -1) {
			declared = append(declared, m[1])
		}
		require.NotEmpty(t, declared, "github-oidc の main.tf から role の environment を1つも読めません")
		assert.ElementsMatch(t, identitySuffixes, declared, "identitySuffixes は github-oidc の role の environment の接尾辞と過不足なく一致する")
	})
}

func Test_checkDeployEnvs(t *testing.T) {
	t.Parallel()

	admin := branchdecl.DeployBranch{Branch: "admin", Env: "admin", From: []string{"release/"}}
	cases := []struct {
		name    string
		envs    []string
		deploy  []branchdecl.DeployBranch
		wantErr string
	}{
		{"各 env にデプロイブランチが在れば通る", []string{"admin"}, []branchdecl.DeployBranch{admin, {Branch: "production"}}, ""},
		{"env にデプロイブランチが無ければ拒む", []string{"admin", "dev"}, []branchdecl.DeployBranch{admin}, "env dev のデプロイブランチがありません"},
		{"env を持たないデプロイブランチは env に数えない", []string{"admin"}, []branchdecl.DeployBranch{{Branch: "admin"}}, "env admin のデプロイブランチがありません"},
		{"deployment-identity に無い env を指すデプロイブランチは拒む", []string{"admin"}, []branchdecl.DeployBranch{admin, {Branch: "develop", Env: "dev", From: []string{"release/"}}}, "デプロイブランチ develop の env dev を deployment-identity が持ちません"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := checkDeployEnvs(tc.envs, branches{Deploy: tc.deploy, Lines: branchdecl.Lines{Release: "release/"}, Revert: "revert/"})
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, errDeployMismatch)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func backendFixture(kind string) string {
	return "terraform {\n  backend \"s3\" {\n    key          = \"" + kind + "/terraform.tfstate\"\n    use_lockfile = true\n  }\n}\n"
}

func Test_checkBackendKeys(t *testing.T) {
	t.Parallel()

	t.Run("2つの env の identity root と workload root が決まった key を持てば通る", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		for _, env := range []string{"admin", "security"} {
			writeFile(t, root, "env/"+env+"/identity/backend.tf", backendFixture("identity"))
			writeFile(t, root, "env/"+env+"/backend.tf", backendFixture("workload"))
		}
		n, err := checkBackendKeys(root)
		require.NoError(t, err)
		assert.Equal(t, 4, n)
	})

	t.Run("env/ の下に symlink があれば失敗する", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		writeFile(t, root, "env/admin/identity/backend.tf", backendFixture("identity"))
		require.NoError(t, os.Symlink(filepath.Join(root, "env/admin"), filepath.Join(root, "env/linked")))
		_, err := checkBackendKeys(root)
		require.ErrorIs(t, err, errBackendKey)
		assert.Contains(t, err.Error(), "symlink は読みません")
	})

	t.Run(".terraform と tests の下は読まない", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		writeFile(t, root, "env/admin/identity/backend.tf", backendFixture("identity"))
		writeFile(t, root, "env/admin/identity/.terraform/modules/x/backend.tf", backendFixture("other"))
		writeFile(t, root, "env/admin/identity/tests/x.tf", "# x\n")
		n, err := checkBackendKeys(root)
		require.NoError(t, err)
		assert.Equal(t, 1, n)
	})

	for name, c := range map[string]struct {
		files map[string]string
		want  string
	}{
		"identity root の key が workload なら失敗する": {map[string]string{"env/admin/identity/backend.tf": backendFixture("workload")}, "env/admin/identity の backend の key が workload/terraform.tfstate で、identity/terraform.tfstate ではありません"},
		"workload root の key が identity なら失敗する": {map[string]string{"env/admin/backend.tf": backendFixture("identity")}, "env/admin の backend の key が identity/terraform.tfstate で、workload/terraform.tfstate ではありません"},
		"root に backend.tf が無ければ失敗する":           {map[string]string{"env/admin/main.tf": "# x\n"}, "env/admin: backend.tf がありません"},
		"key がリテラルでなければ失敗する":                    {map[string]string{"env/admin/backend.tf": "terraform {\n  backend \"s3\" {\n    key = var.key\n  }\n}\n"}, "key がリテラルの文字列ではありません"},
		"s3 の backend が無ければ失敗する":                {map[string]string{"env/admin/backend.tf": "terraform {\n}\n"}, "s3 の backend がありません"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			for f, body := range c.files {
				writeFile(t, root, f, body)
			}
			_, err := checkBackendKeys(root)
			require.ErrorIs(t, err, errBackendKey)
			assert.Contains(t, err.Error(), c.want)
		})
	}

	t.Run("root が1つも無ければ失敗する", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		writeFile(t, root, "env/README.md", "x")
		_, err := checkBackendKeys(root)
		require.ErrorIs(t, err, errBackendKey)
	})
}

func Test_backendKey(t *testing.T) {
	t.Parallel()

	t.Run("s3 の backend の key を読む", func(t *testing.T) {
		t.Parallel()
		got, err := backendKey(writeFile(t, t.TempDir(), "backend.tf", backendFixture("identity")))
		require.NoError(t, err)
		assert.Equal(t, "identity/terraform.tfstate", got)
	})

	for name, body := range map[string]string{
		"s3 でない backend は読まない": "terraform {\n  backend \"local\" {\n    path = \"x\"\n  }\n}\n",
		"HCL として読めなければ失敗する":    "terraform {\n",
		"key が無ければ失敗する":        "terraform {\n  backend \"s3\" {\n  }\n}\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := backendKey(writeFile(t, t.TempDir(), "backend.tf", body))
			require.Error(t, err)
		})
	}

	t.Run("ファイルが無ければ失敗する", func(t *testing.T) {
		t.Parallel()
		_, err := backendKey(filepath.Join(t.TempDir(), "backend.tf"))
		require.Error(t, err)
	})
}

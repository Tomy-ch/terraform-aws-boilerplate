package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/branchdecl"
)

func testBranches() branchdecl.Branches {
	return branchdecl.Branches{
		Deploy: []branchdecl.DeployBranch{
			{Branch: "admin", Env: "admin", From: []string{"release/"}},
			{Branch: "production", ReleaseBase: true},
		},
		Lines:       branchdecl.Lines{Release: "release/", Hotfix: "hotfix/"},
		LineVersion: branchdecl.VersionForm,
		Revert:      "revert/",
	}
}

func boolp(v bool) *bool { return &v }

func Test_selectChecks(t *testing.T) {
	t.Parallel()

	d := decl{Checks: []check{
		{Name: "always", Paths: []string{}, Scope: scopeChange, Required: boolp(true)},
		{Name: "go", Paths: []string{"**/*.go"}, Scope: scopeTree, Required: boolp(true)},
		{Name: "plan", Paths: []string{"env/**"}, Scope: scopeTree, Required: boolp(true), PlanEnv: "admin"},
		{Name: "release", Paths: []string{}, Scope: scopeTree, Required: boolp(false), Bases: []string{baseDeploy}},
	}}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("変更が0件でも常に走る検査は走らせる", func(t *testing.T) {
			t.Parallel()
			got, err := selectChecks(d, nil, "release/v0.2.0", testBranches())
			require.NoError(t, err)
			assert.Equal(t, []string{"always"}, got)
		})

		t.Run("当たる path の検査を宣言の順に加える", func(t *testing.T) {
			t.Parallel()
			got, err := selectChecks(d, []string{"env/admin/main.tf", "scripts/a.go"}, "release/v0.2.0", testBranches())
			require.NoError(t, err)
			assert.Equal(t, []string{"always", "go", "plan"}, got)
		})

		t.Run("bases が deploy の検査はデプロイブランチ宛でだけ走らせる", func(t *testing.T) {
			t.Parallel()
			got, err := selectChecks(d, nil, "production", testBranches())
			require.NoError(t, err)
			assert.Equal(t, []string{"always", "release"}, got)
		})

		t.Run("plan は env へ運ばない base では走らせない", func(t *testing.T) {
			t.Parallel()
			got, err := selectChecks(d, []string{"env/admin/main.tf"}, "hotfix/v0.2.1", testBranches())
			require.NoError(t, err)
			assert.Equal(t, []string{"always"}, got)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("base が線でもデプロイブランチでもなければ誤りにする", func(t *testing.T) {
			t.Parallel()
			_, err := selectChecks(d, nil, "feature/x", testBranches())
			require.ErrorIs(t, err, errBase)
		})

		t.Run("plan_env の env を持つデプロイブランチが無ければ誤りにする", func(t *testing.T) {
			t.Parallel()
			bad := decl{Checks: []check{{Name: "plan", Paths: []string{}, Scope: scopeTree, Required: boolp(true), PlanEnv: "security"}}}
			_, err := selectChecks(bad, nil, "release/v0.2.0", testBranches())
			require.ErrorIs(t, err, errPlanEnv)
		})
	})
}

func Test_touches(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("どれかのファイルがどれかの path に当たれば真", func(t *testing.T) {
			t.Parallel()
			assert.True(t, touches([]string{"a/**", "b"}, []string{"x", "b"}))
			assert.False(t, touches([]string{"a/**"}, []string{"x"}))
			assert.False(t, touches([]string{"a/**"}, nil))
		})
	})
}

func Test_feedsEnv(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("from の線なら真、線でない名前や別の線なら偽", func(t *testing.T) {
			t.Parallel()
			for base, want := range map[string]bool{"release/v0.2.0": true, "release/": false, "hotfix/v0.2.1": false, "admin": false} {
				got, err := feedsEnv(testBranches(), "admin", base)
				require.NoError(t, err)
				assert.Equal(t, want, got, base)
			}
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("env を持つデプロイブランチが無ければ誤りにする", func(t *testing.T) {
			t.Parallel()
			_, err := feedsEnv(testBranches(), "dev", "release/v0.2.0")
			require.ErrorIs(t, err, errPlanEnv)
		})
	})
}

func Test_baseRole(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("線とデプロイブランチを見分ける", func(t *testing.T) {
			t.Parallel()
			for base, want := range map[string]string{"release/v0.2.0": baseLine, "hotfix/v0.2.1": baseLine, "admin": baseDeploy, "production": baseDeploy} {
				got, err := baseRole(testBranches(), base)
				require.NoError(t, err)
				assert.Equal(t, want, got, base)
			}
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("どちらでもない base は誤りにする", func(t *testing.T) {
			t.Parallel()
			for _, base := range []string{"feature/x", "release/", ""} {
				_, err := baseRole(testBranches(), base)
				require.ErrorIs(t, err, errBase, base)
			}
		})
	})
}

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/branchdecl"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/checksdecl"
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

	d := checksdecl.Decl{Checks: []checksdecl.Check{
		{Name: "always", Paths: []string{}, Scope: checksdecl.ScopeChange, Required: boolp(true)},
		{Name: "go", Paths: []string{"**/*.go"}, Scope: checksdecl.ScopeTree, Required: boolp(true)},
		{Name: "plan", Paths: []string{"env/**"}, Scope: checksdecl.ScopeTree, Required: boolp(true), PlanEnv: "admin", Contexts: []string{"plan-admin"}},
		{Name: "release", Paths: []string{}, Scope: checksdecl.ScopeTree, Required: boolp(false), Bases: []string{checksdecl.BaseDeploy}},
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
			bad := checksdecl.Decl{Checks: []checksdecl.Check{{Name: "plan", Paths: []string{}, Scope: checksdecl.ScopeTree, Required: boolp(true), PlanEnv: "security", Contexts: []string{"plan-security"}}}}
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

		for name, c := range map[string]struct {
			base string
			want bool
		}{
			"from の線なら真":       {"release/v0.2.0", true},
			"接頭辞だけの名前は線でないので偽": {"release/", false},
			"from でない線なら偽":     {"hotfix/v0.2.1", false},
			"デプロイブランチなら偽":      {"admin", false},
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				got, err := feedsEnv(testBranches(), "admin", c.base)
				require.NoError(t, err)
				assert.Equal(t, c.want, got)
			})
		}
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

		for name, c := range map[string]struct {
			base, want string
		}{
			"release の線は線": {"release/v0.2.0", checksdecl.BaseLine},
			"hotfix の線は線":  {"hotfix/v0.2.1", checksdecl.BaseLine},
			"env を持つデプロイブランチはデプロイブランチ":   {"admin", checksdecl.BaseDeploy},
			"env を持たないデプロイブランチもデプロイブランチ": {"production", checksdecl.BaseDeploy},
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				got, err := baseRole(testBranches(), c.base)
				require.NoError(t, err)
				assert.Equal(t, c.want, got)
			})
		}
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for name, base := range map[string]string{
			"線でもデプロイブランチでもない名前": "feature/x",
			"線の接頭辞だけの名前":        "release/",
			"空の名前":              "",
		} {
			t.Run(name+"は誤りにする", func(t *testing.T) {
				t.Parallel()
				_, err := baseRole(testBranches(), base)
				require.ErrorIs(t, err, errBase)
			})
		}
	})
}

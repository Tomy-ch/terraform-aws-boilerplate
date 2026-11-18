package main

import (
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/branchdecl"
)

func writeFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o750))
	require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
	return p
}

func boolPtr(v bool) *bool { return &v }

func deployBranches(names ...string) []branchdecl.DeployBranch {
	out := make([]branchdecl.DeployBranch, 0, len(names))
	for _, n := range names {
		out = append(out, branchdecl.DeployBranch{Branch: n})
	}
	return out
}

func Test_readEnvironments(t *testing.T) {
	t.Parallel()

	t.Run("宣言を読む", func(t *testing.T) {
		t.Parallel()
		p := writeFile(t, t.TempDir(), "e.json", `[{"name": "x-plan", "deploy_from": "protected-and-pull-requests", "can_admins_bypass": false}]`)
		envs, err := readEnvironments(p)
		require.NoError(t, err)
		require.Len(t, envs, 1)
		assert.Equal(t, "x-plan", envs[0].Name)
	})

	t.Run("解釈できなければ失敗する", func(t *testing.T) {
		t.Parallel()
		p := writeFile(t, t.TempDir(), "e.json", `[{"name": "x", "deploy_from": "any", "can_admins_bypass": false, "reviewers": []}]`)
		_, err := readEnvironments(p)
		require.ErrorIs(t, err, errDeclaration)
	})

	t.Run("規則に反すれば失敗する", func(t *testing.T) {
		t.Parallel()
		p := writeFile(t, t.TempDir(), "e.json", `[]`)
		_, err := readEnvironments(p)
		require.ErrorIs(t, err, errEmptyDeclaration)
	})

	cases := []struct {
		name string
		body string
	}{
		// 後の false が前の true を黙って上書きすれば、管理者の迂回を許す宣言が通ってしまう。
		{"同じ要素の can_admins_bypass の重複は拒む", `[{"name": "x", "deploy_from": "any", "can_admins_bypass": true, "can_admins_bypass": false}]`},
		{"object なら拒む", `{}`},
		{"要素が null なら拒む", `[null]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := writeFile(t, t.TempDir(), "e.json", tc.body)
			_, err := readEnvironments(p)
			require.ErrorIs(t, err, errDeclaration)
		})
	}
}

func Test_validateEnvironments(t *testing.T) {
	t.Parallel()

	env := func(name, from string, bypass *bool) environment {
		return environment{Name: name, DeployFrom: from, CanAdminsBypass: bypass}
	}
	off := boolPtr(false)
	cases := []struct {
		name    string
		in      []environment
		wantErr error
	}{
		{"plan / apply / apply-identity / unlock-identity / 任意の名前は通る", []environment{
			env("a-plan", deployFromProtectedPR, off),
			env("a-apply", deployFromDeployBranch, off),
			env("a-apply-identity", deployFromDeployBranch, off),
			env("a-unlock-identity", deployFromDeployBranch, off),
			env("copilot", deployFromAny, off),
		}, nil},
		{"0件は拒む", nil, errEmptyDeclaration},
		{"名前が空なら拒む", []environment{env("", deployFromAny, off)}, errDeclaration},
		{"名前の重複は拒む", []environment{env("x", deployFromAny, off), env("x", deployFromAny, off)}, errDeclaration},
		{"未知の deploy_from は拒む", []environment{env("x", "all", off)}, errDeclaration},
		{"can_admins_bypass の省略は拒む", []environment{env("x", deployFromAny, nil)}, errDeclaration},
		{"管理者の迂回を許す宣言は拒む", []environment{env("x", deployFromAny, boolPtr(true))}, errDeclaration},
		{"apply が Pull Request を許す宣言は拒む", []environment{env("a-apply", deployFromProtectedPR, off)}, errDeclaration},
		{"apply-identity が任意のブランチを許す宣言は拒む", []environment{env("a-apply-identity", deployFromAny, off)}, errDeclaration},
		{"unlock-identity が Pull Request を許す宣言は拒む", []environment{env("a-unlock-identity", deployFromProtectedPR, off)}, errDeclaration},
		{"plan が Pull Request を許さない宣言は拒む", []environment{env("a-plan", deployFromProtected, off)}, errDeclaration},
		{"apply が保護ブランチ全体を許す宣言は拒む", []environment{env("a-apply", deployFromProtected, off)}, errDeclaration},
		{"unlock-identity が保護ブランチ全体を許す宣言は拒む", []environment{env("a-unlock-identity", deployFromProtected, off)}, errDeclaration},
		{"plan がデプロイブランチだけを許す宣言は拒む", []environment{env("a-plan", deployFromDeployBranch, off)}, errDeclaration},
		{"deployment-identity の名前でない environment は deploy-branch を使えない", []environment{env("copilot", deployFromDeployBranch, off)}, errDeclaration},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateEnvironments(tc.in)
			if tc.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tc.wantErr)
		})
	}
}

func Test_requiredDeployFrom(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		want   string
		wantOK bool
	}{
		{"admin-apply", deployFromDeployBranch, true},
		{"admin-apply-identity", deployFromDeployBranch, true},
		{"admin-unlock-identity", deployFromDeployBranch, true},
		{"admin-plan", deployFromProtectedPR, true},
		{"admin-unlock", "", false},
		{"copilot", "", false},
		{"admin-planner", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := requiredDeployFrom(tc.name)
			assert.Equal(t, tc.wantOK, ok)
			assert.Equal(t, tc.want, got)
		})
	}
}

func Test_rulesetIncludes(t *testing.T) {
	t.Parallel()

	got := rulesetIncludes(branches{Deploy: deployBranches("production", "develop"), Lines: branchdecl.Lines{Release: "release/", Hotfix: "hotfix/"}, Revert: "revert/"})
	assert.Equal(t, []string{
		"refs/heads/production",
		"refs/heads/develop",
		"refs/heads/release/**/*",
		"refs/heads/hotfix/**/*",
	}, got)
}

// `**` は `/` を跨ぐ。
func rulesetProtects(includes []string, ref string) bool {
	for _, inc := range includes {
		if prefix, ok := strings.CutSuffix(inc, "**/*"); ok {
			if strings.HasPrefix(ref, prefix) && len(ref) > len(prefix) {
				return true
			}
			continue
		}
		if inc == ref {
			return true
		}
	}
	return false
}

func Test_environmentPatterns(t *testing.T) {
	t.Parallel()

	b := branches{
		Deploy: []branchdecl.DeployBranch{{Branch: "admin", Env: "admin", From: []string{"release/"}}, {Branch: "production"}, {Branch: "develop"}},
		Lines:  branchdecl.Lines{Release: "release/"},
		Revert: "revert/",
	}
	env := func(name, from string) environment { return environment{Name: name, DeployFrom: from} }

	t.Run("protected は固定名と1階層の接頭辞に展開する", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, []string{"admin", "production", "develop", "release/*"}, environmentPatterns(b, env("x", deployFromProtected)))
	})

	t.Run("protected-and-pull-requests は Pull Request の ref を足す", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, []string{"admin", "production", "develop", "release/*", pullRequestRef}, environmentPatterns(b, env("admin-plan", deployFromProtectedPR)))
	})

	t.Run("any は policy を持たない", func(t *testing.T) {
		t.Parallel()
		assert.Nil(t, environmentPatterns(b, env("copilot", deployFromAny)))
	})

	t.Run("deploy-branch は名前の env のデプロイブランチだけに展開する", func(t *testing.T) {
		t.Parallel()
		for _, name := range []string{"admin-apply", "admin-apply-identity", "admin-unlock-identity"} {
			assert.Equal(t, []string{"admin"}, environmentPatterns(b, env(name, deployFromDeployBranch)), name)
		}
	})

	t.Run("deploy-branch は env のデプロイブランチが無ければ何も許さない", func(t *testing.T) {
		t.Parallel()
		got := environmentPatterns(b, env("prd-apply", deployFromDeployBranch))
		assert.NotNil(t, got)
		assert.Empty(t, got)
	})

	t.Run("environment が許すブランチは ruleset も保護する", func(t *testing.T) {
		t.Parallel()
		// environment のパターンの `*` は `/` に当たらない（path.Match と同じ意味）。
		patterns := environmentPatterns(b, env("x", deployFromProtected))
		includes := rulesetIncludes(b)
		for _, branch := range []string{"production", "develop", "release/v0.1.0", "release/a/b", "feature/x", "staging"} {
			allowed := false
			for _, p := range patterns {
				ok, err := path.Match(p, branch)
				require.NoError(t, err)
				allowed = allowed || ok
			}
			if allowed {
				assert.True(t, rulesetProtects(includes, "refs/heads/"+branch), branch)
			}
		}
	})
}

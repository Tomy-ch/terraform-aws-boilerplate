package main

import (
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o750))
	require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
	return p
}

func boolPtr(v bool) *bool { return &v }

func Test_decodeStrict(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		body    string
		wantErr error
	}{
		{"正しい JSON は読める", `{"deploy": ["a"]}`, nil},
		{"未知のキーは拒む", `{"deploy": ["a"], "extra": 1}`, errDeclaration},
		{"値の後の余分なトークンは拒む", `{"deploy": ["a"]} {}`, errDeclaration},
		{"JSON でないものは拒む", `deploy: a`, errDeclaration},
		{"空のファイルは拒む", ``, errDeclaration},
		{"キーの重複は拒む", `{"deploy": ["b"], "deploy": ["a"]}`, errDeclaration},
		{"字面の違うキーは拒む", `{"Deploy": ["a"]}`, errDeclaration},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := writeFile(t, t.TempDir(), "b.json", tc.body)
			var b branches
			err := decodeStrict(p, &b)
			if tc.wantErr == nil {
				require.NoError(t, err)
				assert.Equal(t, []string{"a"}, b.Deploy)
				return
			}
			require.ErrorIs(t, err, tc.wantErr)
		})
	}

	t.Run("ファイルが無ければ失敗する", func(t *testing.T) {
		t.Parallel()
		var b branches
		err := decodeStrict(filepath.Join(t.TempDir(), "none.json"), &b)
		require.ErrorIs(t, err, os.ErrNotExist)
	})
}

func Test_checkKeys(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		src     string
		wantErr bool
	}{
		{"宣言のキーだけなら通る", `[{"name": "x", "deploy_from": "any", "can_admins_bypass": false}]`, false},
		{"入れ子の配列とスカラーも辿る", `{"deploy": ["a", "b"], "lines": []}`, false},
		{"宣言のキーでなければ拒む", `{"extra": 1}`, true},
		{"字面の違うキーは拒む", `{"DEPLOY_FROM": "any"}`, true},
		{"入れ子の object のキーの重複も拒む", `[{"name": "a", "name": "b"}]`, true},
		{"配列の中の不正なキーも拒む", `[{"name": "a"}, {"nope": 1}]`, true},
		{"壊れた JSON は拒む", `{"deploy": [`, true},
		{"閉じられていない object は拒む", `{"deploy": ["a"]`, true},
		{"キーの位置の構文エラーは拒む", `{"deploy": ["a"],}`, true},
		{"空の入力は拒む", ``, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := checkKeys(newDecoder([]byte(tc.src)))
			if !tc.wantErr {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, errDeclaration)
		})
	}
}

func Test_readBranches(t *testing.T) {
	t.Parallel()

	t.Run("宣言を読む", func(t *testing.T) {
		t.Parallel()
		p := writeFile(t, t.TempDir(), "b.json", `{"deploy": ["production"], "lines": ["release/"]}`)
		b, err := readBranches(p)
		require.NoError(t, err)
		assert.Equal(t, branches{Deploy: []string{"production"}, Lines: []string{"release/"}}, b)
	})

	t.Run("解釈できなければ失敗する", func(t *testing.T) {
		t.Parallel()
		p := writeFile(t, t.TempDir(), "b.json", `{"deploy": "production"}`)
		_, err := readBranches(p)
		require.ErrorIs(t, err, errDeclaration)
	})

	t.Run("規則に反すれば失敗する", func(t *testing.T) {
		t.Parallel()
		p := writeFile(t, t.TempDir(), "b.json", `{"deploy": [], "lines": []}`)
		_, err := readBranches(p)
		require.ErrorIs(t, err, errEmptyDeclaration)
	})

	t.Run("null は0件として拒む", func(t *testing.T) {
		t.Parallel()
		p := writeFile(t, t.TempDir(), "b.json", `null`)
		_, err := readBranches(p)
		require.ErrorIs(t, err, errEmptyDeclaration)
	})

	t.Run("配列なら拒む", func(t *testing.T) {
		t.Parallel()
		p := writeFile(t, t.TempDir(), "b.json", `["production"]`)
		_, err := readBranches(p)
		require.ErrorIs(t, err, errDeclaration)
	})
}

func Test_validateBranches(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		in      branches
		wantErr error
	}{
		{"固定名と接頭辞を持つ宣言は通る", branches{Deploy: []string{"production", "develop"}, Lines: []string{"release/"}}, nil},
		{"接頭辞だけの宣言も通る", branches{Lines: []string{"release/"}}, nil},
		{"0件は拒む", branches{}, errEmptyDeclaration},
		{"glob の文字を含む固定名は拒む", branches{Deploy: []string{"release/*"}}, errDeclaration},
		{"大文字の固定名は拒む", branches{Deploy: []string{"Main"}}, errDeclaration},
		{"固定名の重複は拒む", branches{Deploy: []string{"develop", "develop"}}, errDeclaration},
		{"`/` で終わらない接頭辞は拒む", branches{Lines: []string{"release"}}, errDeclaration},
		{"glob の文字を含む接頭辞は拒む", branches{Lines: []string{"release/*/"}}, errDeclaration},
		{"接頭辞の重複は拒む", branches{Lines: []string{"release/", "release/"}}, errDeclaration},
		{"空文字列の固定名は拒む", branches{Deploy: []string{""}}, errDeclaration},
		{"空文字列の接頭辞は拒む", branches{Lines: []string{""}}, errDeclaration},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateBranches(tc.in)
			if tc.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tc.wantErr)
		})
	}
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
		{"plan / apply / apply-identity / 任意の名前は通る", []environment{
			env("a-plan", deployFromProtectedPR, off),
			env("a-apply", deployFromProtected, off),
			env("a-apply-identity", deployFromProtected, off),
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
		{"plan が Pull Request を許さない宣言は拒む", []environment{env("a-plan", deployFromProtected, off)}, errDeclaration},
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
		{"admin-apply", deployFromProtected, true},
		{"admin-apply-identity", deployFromProtected, true},
		{"admin-plan", deployFromProtectedPR, true},
		{"repo-settings", deployFromProtected, true},
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

	got := rulesetIncludes(branches{Deploy: []string{"production", "develop"}, Lines: []string{"release/", "hotfix/"}})
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

	b := branches{Deploy: []string{"production", "develop"}, Lines: []string{"release/"}}

	t.Run("protected は固定名と1階層の接頭辞に展開する", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, []string{"production", "develop", "release/*"}, environmentPatterns(b, deployFromProtected))
	})

	t.Run("protected-and-pull-requests は Pull Request の ref を足す", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, []string{"production", "develop", "release/*", pullRequestRef}, environmentPatterns(b, deployFromProtectedPR))
	})

	t.Run("any は policy を持たない", func(t *testing.T) {
		t.Parallel()
		assert.Nil(t, environmentPatterns(b, deployFromAny))
	})

	t.Run("environment が許すブランチは ruleset も保護する", func(t *testing.T) {
		t.Parallel()
		// environment のパターンの `*` は `/` に当たらない（path.Match と同じ意味）。
		patterns := environmentPatterns(b, deployFromProtected)
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

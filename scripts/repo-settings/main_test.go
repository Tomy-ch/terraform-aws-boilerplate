package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/testenv"
)

// editorconfig-checker-disable -- raw string の fixture は埋め込んだ JSON の字下げを持つ。撤回: fixture を外部ファイルへ移したとき

const (
	branchesFixture     = `{"deploy": ["main"], "lines": ["release/"]}`
	environmentsFixture = `[
  {"name": "x-plan", "deploy_from": "protected-and-pull-requests", "can_admins_bypass": false},
  {"name": "x-apply", "deploy_from": "protected", "can_admins_bypass": false},
  {"name": "x-apply-identity", "deploy_from": "protected", "can_admins_bypass": false},
  {"name": "copilot", "deploy_from": "any", "can_admins_bypass": false}
]`
	identityRootFixture = `module "deployment_identity" {
  source = "../../../usecases/deployment-identity"
  env    = "x"
}
`
)

// editorconfig-checker-enable

var syncedRuleset = strings.Replace(rulesetFixture,
	"[\n        \"refs/heads/old\"\n      ]",
	"[\n        \"refs/heads/main\",\n        \"refs/heads/release/**/*\"\n      ]", 1)

func writeRepo(t *testing.T, ruleset string) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, branchesFile, branchesFixture)
	writeFile(t, root, environmentsFile, environmentsFixture)
	writeFile(t, root, rulesetFile, ruleset)
	writeFile(t, root, "env/x/identity/main.tf", identityRootFixture)
	return root
}

func liveRoutes(t *testing.T) map[string]fakeResponse {
	t.Helper()
	var ruleset map[string]any
	require.NoError(t, json.Unmarshal([]byte(syncedRuleset), &ruleset))
	ruleset["id"] = 7.0
	body, err := json.Marshal(ruleset)
	require.NoError(t, err)
	return map[string]fakeResponse{
		rulesetListPath(1):          ok(`[{"id": 7, "name": "branch-protection", "source_type": "Repository"}]`),
		"GET /repos/o/r/rulesets/7": ok(string(body)),
		envListPath(1): ok(`{"total_count": 4, "environments": [
			{"name": "x-plan", "protection_rules": [], "can_admins_bypass": false, "deployment_branch_policy": {"protected_branches": false, "custom_branch_policies": true}},
			{"name": "x-apply", "protection_rules": [], "can_admins_bypass": false, "deployment_branch_policy": {"protected_branches": false, "custom_branch_policies": true}},
			{"name": "x-apply-identity", "protection_rules": [], "can_admins_bypass": false, "deployment_branch_policy": {"protected_branches": false, "custom_branch_policies": true}},
			{"name": "copilot", "protection_rules": [], "can_admins_bypass": false, "deployment_branch_policy": null}]}`),
		policyPath("x-plan", 1): ok(`{"total_count": 3, "branch_policies": [
			{"id": 1, "name": "main", "type": "branch"}, {"id": 2, "name": "release/*", "type": "branch"}, {"id": 3, "name": "refs/pull/*/merge", "type": "branch"}]}`),
		policyPath("x-apply", 1):          ok(protectedPolicies),
		policyPath("x-apply-identity", 1): ok(protectedPolicies),
	}
}

const protectedPolicies = `{"total_count": 2, "branch_policies": [{"id": 4, "name": "main", "type": "branch"}, {"id": 5, "name": "release/*", "type": "branch"}]}`

// applyEnvsJSON は liveRoutes の x-apply と x-apply-identity。environment の一覧を差し替えるケースが含める。
const applyEnvsJSON = `{"name": "x-apply", "protection_rules": [], "can_admins_bypass": false, "deployment_branch_policy": {"protected_branches": false, "custom_branch_policies": true}},
			{"name": "x-apply-identity", "protection_rules": [], "can_admins_bypass": false, "deployment_branch_policy": {"protected_branches": false, "custom_branch_policies": true}},`

func fixedWD(root string) func() (string, error) {
	return func() (string, error) { return root, nil }
}

func Test_run(t *testing.T) {
	t.Parallel()

	usageCases := []struct {
		name string
		args []string
	}{
		{"サブコマンドが無ければ usage で失敗する", nil},
		{"未知のサブコマンドは usage で失敗する", []string{"unknown"}},
		{"余分な引数は usage で失敗する", []string{"check", "extra"}},
		{"未知のフラグは usage で失敗する", []string{"check", "-nope"}},
		{"sync に -only が無ければ usage で失敗する", []string{"sync", "-repo=o/r"}},
		{"sync の -only が未知なら usage で失敗する", []string{"sync", "-repo=o/r", "-only", "labels"}},
		{"使わないフラグは usage で失敗する", []string{"apply", "-repo=o/r"}},
		{"drift に -only を渡せば usage で失敗する", []string{"drift", "-repo=o/r", "-only", "ruleset"}},
	}
	for _, tc := range usageCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := run(tc.args, fixedWD(writeRepo(t, syncedRuleset)), githubClient{}, &bytes.Buffer{})
			require.ErrorIs(t, err, errUsage)
		})
	}

	repoCases := []struct {
		name string
		args []string
	}{
		{"drift のリポジトリが空なら、それと分かるエラーで失敗する", []string{"drift", "-repo="}},
		{"sync のリポジトリが空なら、それと分かるエラーで失敗する", []string{"sync", "-only", "ruleset", "-repo="}},
	}
	for _, tc := range repoCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f, c := newFakeGitHub(t, liveRoutes(t))
			err := run(tc.args, fixedWD(writeRepo(t, syncedRuleset)), c, &bytes.Buffer{})
			require.ErrorIs(t, err, errNoRepo)
			assert.Empty(t, f.requests)
		})
	}

	t.Run("作業ディレクトリを得られなければ、そのエラーで失敗する", func(t *testing.T) {
		t.Parallel()
		errWD := errors.New("no wd")
		wd := func() (string, error) { return "", errWD }
		require.ErrorIs(t, run([]string{"check"}, wd, githubClient{}, &bytes.Buffer{}), errWD)
	})

	t.Run("check と apply を呼ぶ", func(t *testing.T) {
		t.Parallel()
		root := writeRepo(t, rulesetFixture)
		require.ErrorIs(t, run([]string{"check"}, fixedWD(root), githubClient{}, &bytes.Buffer{}), errIncludeDrift)
		var out bytes.Buffer
		require.NoError(t, run([]string{"apply"}, fixedWD(root), githubClient{}, &out))
		assert.Contains(t, out.String(), "include を書き換えました")
		require.NoError(t, run([]string{"check"}, fixedWD(root), githubClient{}, &bytes.Buffer{}))
	})

	t.Run("drift は -repo のリポジトリを読む", func(t *testing.T) {
		t.Parallel()
		f, c := newFakeGitHub(t, liveRoutes(t))
		var out bytes.Buffer
		require.NoError(t, run([]string{"drift", "-repo=o/r"}, fixedWD(writeRepo(t, syncedRuleset)), c, &out))
		require.NotEmpty(t, f.requests)
		assert.Equal(t, rulesetListPath(1), "GET "+f.requests[0].target)
		assert.Equal(t, "✅ repo-settings: o/r の ruleset 1 件と environment 4 件（実在 4 件）が宣言どおりです\n", out.String())
	})

	t.Run("sync は GITHUB_TOKEN が無ければ何も送らずに失敗する", func(t *testing.T) {
		t.Parallel()
		f, c := newFakeGitHub(t, liveRoutes(t))
		c.token = ""
		err := run([]string{"sync", "-repo=o/r", "-only", onlyRuleset}, fixedWD(writeRepo(t, syncedRuleset)), c, &bytes.Buffer{})
		require.ErrorIs(t, err, errNoToken)
		assert.Empty(t, f.requests)
	})

	t.Run("sync は -only の側だけを扱う", func(t *testing.T) {
		t.Parallel()
		_, c := newFakeGitHub(t, liveRoutes(t))
		var out bytes.Buffer
		require.NoError(t, run([]string{"sync", "-repo=o/r", "-only", onlyEnvironments}, fixedWD(writeRepo(t, syncedRuleset)), c, &out))
		assert.Equal(t, "✅ repo-settings: o/r の environments へ送る要求はありません\n", out.String())
	})
}

func Test_readDeclarations(t *testing.T) {
	t.Parallel()

	t.Run("3つの宣言を読み、include を生成し直す", func(t *testing.T) {
		t.Parallel()
		decl, err := readDeclarations(writeRepo(t, rulesetFixture))
		require.NoError(t, err)
		assert.Equal(t, []string{"main"}, decl.branches.Deploy)
		assert.Len(t, decl.environments, 4)
		assert.Equal(t, rulesetFixture, string(decl.ruleset))
		assert.Equal(t, syncedRuleset, string(decl.rendered))
	})

	cases := []struct {
		name    string
		mutate  func(root string)
		wantErr error
	}{
		{"branches.json が壊れていれば失敗する", func(root string) {
			writeFile(t, root, branchesFile, `{}`)
		}, errEmptyDeclaration},
		{"environments.json が壊れていれば失敗する", func(root string) {
			writeFile(t, root, environmentsFile, `[{"name": "x", "deploy_from": "any", "can_admins_bypass": true}]`)
		}, errDeclaration},
		{"branch-protection.json が無ければ失敗する", func(root string) {
			require.NoError(t, os.Remove(filepath.Join(root, rulesetFile)))
		}, os.ErrNotExist},
		{"branch-protection.json が ADR-0603 を満たさなければ失敗する", func(root string) {
			writeFile(t, root, rulesetFile, strings.Replace(rulesetFixture, `"bypass_actors": []`, `"bypass_actors": [1]`, 1))
		}, errRulesetPolicy},
		{"信頼条件の environment が宣言に揃っていなければ失敗する", func(root string) {
			writeFile(t, root, "env/y/identity/main.tf", strings.Replace(identityRootFixture, `"x"`, `"y"`, 1))
		}, errEnvironmentMismatch},
		{"deployment-identity を呼ぶ root が無ければ失敗する", func(root string) {
			require.NoError(t, os.RemoveAll(filepath.Join(root, "env")))
		}, os.ErrNotExist},
		{"include を定められなければ失敗する", func(root string) {
			writeFile(t, root, rulesetFile, strings.Replace(rulesetFixture, `"include"`, `"included"`, 1))
		}, errInclude},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := writeRepo(t, rulesetFixture)
			tc.mutate(root)
			_, err := readDeclarations(root)
			require.ErrorIs(t, err, tc.wantErr)
		})
	}
}

func Test_loadDesired(t *testing.T) {
	t.Parallel()

	t.Run("宣言からあるべき実態を組む", func(t *testing.T) {
		t.Parallel()
		want, err := loadDesired(writeRepo(t, syncedRuleset))
		require.NoError(t, err)
		assert.Equal(t, "branch-protection", want.Ruleset["name"])
		assert.Equal(t, []desiredEnv{
			{Name: "x-plan", Patterns: []string{"main", "release/*", pullRequestRef}},
			{Name: "x-apply", Patterns: []string{"main", "release/*"}},
			{Name: "x-apply-identity", Patterns: []string{"main", "release/*"}},
			{Name: "copilot"},
		}, want.Envs)
	})

	t.Run("include がずれていれば失敗する", func(t *testing.T) {
		t.Parallel()
		_, err := loadDesired(writeRepo(t, rulesetFixture))
		require.ErrorIs(t, err, errIncludeDrift)
	})

	t.Run("宣言を読めなければ失敗する", func(t *testing.T) {
		t.Parallel()
		root := writeRepo(t, syncedRuleset)
		writeFile(t, root, branchesFile, `x`)
		_, err := loadDesired(root)
		require.ErrorIs(t, err, errDeclaration)
	})
}

func Test_applyOrCheck(t *testing.T) {
	t.Parallel()

	t.Run("apply は include の区間を書き換える", func(t *testing.T) {
		t.Parallel()
		root := writeRepo(t, rulesetFixture)
		var out bytes.Buffer
		require.NoError(t, applyOrCheck(root, false, &out))
		got, err := os.ReadFile(filepath.Join(root, rulesetFile))
		require.NoError(t, err)
		assert.Equal(t, syncedRuleset, string(got))
		assert.Contains(t, out.String(), "保護対象 2 件、environment 4 件")
		assert.Contains(t, out.String(), "書き換えました")
	})

	t.Run("ずれが無ければ apply は何も書かない", func(t *testing.T) {
		t.Parallel()
		root := writeRepo(t, syncedRuleset)
		p := filepath.Join(root, rulesetFile)
		before, err := os.Stat(p)
		require.NoError(t, err)
		var out bytes.Buffer
		require.NoError(t, applyOrCheck(root, false, &out))
		after, err := os.Stat(p)
		require.NoError(t, err)
		// 書き込みは rename なので、書けば別のファイルになる。
		assert.True(t, os.SameFile(before, after))
		assert.Equal(t, "✅ repo-settings: 保護対象 2 件、environment 4 件。.github/settings/branch-protection.json は宣言どおりです\n", out.String())
	})

	t.Run("check はずれを報せ、書かない", func(t *testing.T) {
		t.Parallel()
		root := writeRepo(t, rulesetFixture)
		require.ErrorIs(t, applyOrCheck(root, true, &bytes.Buffer{}), errIncludeDrift)
		got, err := os.ReadFile(filepath.Join(root, rulesetFile))
		require.NoError(t, err)
		assert.Equal(t, rulesetFixture, string(got))
	})

	t.Run("宣言が不正なら apply は何も書かない", func(t *testing.T) {
		t.Parallel()
		root := writeRepo(t, rulesetFixture)
		writeFile(t, root, environmentsFile, `[]`)
		require.ErrorIs(t, applyOrCheck(root, false, &bytes.Buffer{}), errEmptyDeclaration)
		got, err := os.ReadFile(filepath.Join(root, rulesetFile))
		require.NoError(t, err)
		assert.Equal(t, rulesetFixture, string(got))
	})

	t.Run("書き込めなければ失敗し、ファイルは元のまま", func(t *testing.T) {
		t.Parallel()
		testenv.RequireNonRoot(t, "読み取り専用のディレクトリへの書き込みの失敗を作る")
		root := writeRepo(t, rulesetFixture)
		dir := filepath.Dir(filepath.Join(root, rulesetFile))
		require.NoError(t, os.Chmod(dir, 0o500))       //nolint:gosec // 書き込み失敗を作るため
		t.Cleanup(func() { _ = os.Chmod(dir, 0o750) }) //nolint:gosec // 後始末
		require.ErrorIs(t, applyOrCheck(root, false, &bytes.Buffer{}), fs.ErrPermission)
		got, err := os.ReadFile(filepath.Join(root, rulesetFile))
		require.NoError(t, err)
		assert.Equal(t, rulesetFixture, string(got))
		entries, err := os.ReadDir(dir)
		require.NoError(t, err)
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		assert.ElementsMatch(t, []string{"branches.json", "environments.json", "branch-protection.json"}, names)
	})
}

func Test_drift(t *testing.T) {
	t.Parallel()

	t.Run("一致すれば件数を報せて成功する", func(t *testing.T) {
		t.Parallel()
		root := writeRepo(t, syncedRuleset)
		f, c := newFakeGitHub(t, liveRoutes(t))
		var out bytes.Buffer
		require.NoError(t, drift(context.Background(), root, c, repo, &out))
		assert.Equal(t, "✅ repo-settings: o/r の ruleset 1 件と environment 4 件（実在 4 件）が宣言どおりです\n", out.String())
		assert.Empty(t, f.writes())
	})

	t.Run("ずれをすべて出力して失敗する", func(t *testing.T) {
		t.Parallel()
		root := writeRepo(t, syncedRuleset)
		routes := liveRoutes(t)
		routes[envListPath(1)] = ok(`{"total_count": 4, "environments": [
			` + applyEnvsJSON + `
			{"name": "copilot", "protection_rules": [], "can_admins_bypass": true, "deployment_branch_policy": null},
			{"name": "other", "protection_rules": [], "can_admins_bypass": false}]}`)
		f, c := newFakeGitHub(t, routes)
		var out bytes.Buffer
		err := drift(context.Background(), root, c, repo, &out)
		require.ErrorIs(t, err, errDrift)
		assert.Equal(t, strings.Join([]string{
			"❌ repo-settings: o/r で宣言と実態のずれが 3 件あります",
			"  - environment x-plan: 実在しません",
			"  - environment copilot: 管理者による保護の迂回が有効です",
			"  - environment other: 宣言に無い environment が実在します",
		}, "\n")+"\n", out.String())
		assert.Empty(t, f.writes())
	})

	t.Run("観測できなかったものを出力する", func(t *testing.T) {
		t.Parallel()
		root := writeRepo(t, syncedRuleset)
		routes := liveRoutes(t)
		routes["GET /repos/o/r/rulesets/7"] = ok(strings.Replace(syncedRuleset, ",\n  \"bypass_actors\": []", "", 1))
		_, c := newFakeGitHub(t, routes)
		var out bytes.Buffer
		require.NoError(t, drift(context.Background(), root, c, repo, &out))
		assert.Equal(t, strings.Join([]string{
			"⚠️ 突合の対象外（観測できない）: ruleset の bypass_actors（write 権限の無い読み取りには返らない）",
			"✅ repo-settings: o/r の ruleset 1 件と environment 4 件（実在 4 件）が宣言どおりです（突合の対象外 1 件を除く）",
		}, "\n")+"\n", out.String())
	})

	t.Run("実態を読めなければ失敗する", func(t *testing.T) {
		t.Parallel()
		_, c := newFakeGitHub(t, map[string]fakeResponse{})
		require.ErrorIs(t, drift(context.Background(), writeRepo(t, syncedRuleset), c, repo, &bytes.Buffer{}), errHTTP)
	})
}

func Test_syncSettings(t *testing.T) {
	t.Parallel()

	t.Run("差分が無ければ何も書かない", func(t *testing.T) {
		t.Parallel()
		f, c := newFakeGitHub(t, liveRoutes(t))
		var out bytes.Buffer
		require.NoError(t, syncSettings(context.Background(), writeRepo(t, syncedRuleset), c, repo, onlyRuleset, &out))
		assert.Empty(t, f.writes())
		assert.Equal(t, "✅ repo-settings: o/r の ruleset へ送る要求はありません\n", out.String())
	})

	t.Run("environment の差分を drift と同じ差分から解消し、宣言に無いものは消さない", func(t *testing.T) {
		t.Parallel()
		routes := liveRoutes(t)
		routes[envListPath(1)] = ok(`{"total_count": 4, "environments": [
			` + applyEnvsJSON + `
			{"name": "copilot", "protection_rules": [], "can_admins_bypass": true, "deployment_branch_policy": null},
			{"name": "other", "protection_rules": [], "can_admins_bypass": false}]}`)
		routes["PUT /repos/o/r/environments/x-plan"] = ok(`{}`)
		routes["PUT /repos/o/r/environments/copilot"] = ok(`{}`)
		routes["POST /repos/o/r/environments/x-plan/deployment-branch-policies"] = ok(`{}`)
		f, c := newFakeGitHub(t, routes)
		var out bytes.Buffer
		require.NoError(t, syncSettings(context.Background(), writeRepo(t, syncedRuleset), c, repo, onlyEnvironments, &out))
		writes := f.writes()
		targets := make([]string, 0, len(writes))
		for _, w := range writes {
			targets = append(targets, w.method+" "+w.target)
		}
		assert.Equal(t, []string{
			"PUT /repos/o/r/environments/x-plan",
			"POST /repos/o/r/environments/x-plan/deployment-branch-policies",
			"POST /repos/o/r/environments/x-plan/deployment-branch-policies",
			"POST /repos/o/r/environments/x-plan/deployment-branch-policies",
			"PUT /repos/o/r/environments/copilot",
		}, targets)
		assert.JSONEq(t, `{"can_admins_bypass": false, "deployment_branch_policy": {"protected_branches": false, "custom_branch_policies": true},
			"prevent_self_review": false, "reviewers": null, "wait_timer": 0}`, writes[0].body)
		assert.JSONEq(t, `{"name": "refs/pull/*/merge", "type": "branch"}`, writes[3].body)
		assert.Equal(t, strings.Join([]string{
			"🔧 PUT /repos/o/r/environments/x-plan",
			"🔧 POST /repos/o/r/environments/x-plan/deployment-branch-policies",
			"🔧 POST /repos/o/r/environments/x-plan/deployment-branch-policies",
			"🔧 POST /repos/o/r/environments/x-plan/deployment-branch-policies",
			"🔧 PUT /repos/o/r/environments/copilot",
			"⚠️ 宣言に無い environment は消していません（make repo-settings-drift はこれをずれとして報せます）: other",
			"✅ repo-settings: o/r へ 5 件の要求を送りました。make repo-settings-drift で結果を確かめてください",
		}, "\n")+"\n", out.String())
	})

	t.Run("宣言に無い environment だけがずれていれば、何も送らずにそれを報せる", func(t *testing.T) {
		t.Parallel()
		routes := liveRoutes(t)
		routes[envListPath(1)] = ok(`{"total_count": 5, "environments": [
			{"name": "x-plan", "protection_rules": [], "can_admins_bypass": false, "deployment_branch_policy": {"protected_branches": false, "custom_branch_policies": true}},
			` + applyEnvsJSON + `
			{"name": "copilot", "protection_rules": [], "can_admins_bypass": false, "deployment_branch_policy": null},
			{"name": "other", "protection_rules": [], "can_admins_bypass": false}]}`)
		f, c := newFakeGitHub(t, routes)
		var out bytes.Buffer
		require.NoError(t, syncSettings(context.Background(), writeRepo(t, syncedRuleset), c, repo, onlyEnvironments, &out))
		assert.Empty(t, f.writes())
		assert.Equal(t, strings.Join([]string{
			"⚠️ 宣言に無い environment は消していません（make repo-settings-drift はこれをずれとして報せます）: other",
			"✅ repo-settings: o/r の environments へ送る要求はありません",
		}, "\n")+"\n", out.String())
	})

	t.Run("ruleset だけを扱うときは、宣言に無い environment を報せない", func(t *testing.T) {
		t.Parallel()
		routes := liveRoutes(t)
		routes[envListPath(1)] = ok(`{"total_count": 5, "environments": [
			{"name": "x-plan", "protection_rules": [], "can_admins_bypass": false, "deployment_branch_policy": {"protected_branches": false, "custom_branch_policies": true}},
			` + applyEnvsJSON + `
			{"name": "copilot", "protection_rules": [], "can_admins_bypass": false, "deployment_branch_policy": null},
			{"name": "other", "protection_rules": [], "can_admins_bypass": false}]}`)
		_, c := newFakeGitHub(t, routes)
		var out bytes.Buffer
		require.NoError(t, syncSettings(context.Background(), writeRepo(t, syncedRuleset), c, repo, onlyRuleset, &out))
		assert.Equal(t, "✅ repo-settings: o/r の ruleset へ送る要求はありません\n", out.String())
	})

	t.Run("ruleset の差分を PUT で解消する", func(t *testing.T) {
		t.Parallel()
		routes := liveRoutes(t)
		routes["GET /repos/o/r/rulesets/7"] = ok(`{"name": "branch-protection", "rules": []}`)
		routes["PUT /repos/o/r/rulesets/7"] = ok(`{}`)
		f, c := newFakeGitHub(t, routes)
		require.NoError(t, syncSettings(context.Background(), writeRepo(t, syncedRuleset), c, repo, onlyRuleset, &bytes.Buffer{}))
		writes := f.writes()
		require.Len(t, writes, 1)
		assert.JSONEq(t, compactJSON(t, syncedRuleset), writes[0].body)
	})

	t.Run("書き込みが失敗すれば、後の要求を送らずに止まる", func(t *testing.T) {
		t.Parallel()
		// x-plan が無いので、PUT の後に POST が3件続く計画になる。PUT は経路が無く 404 で失敗する。
		routes := liveRoutes(t)
		routes[envListPath(1)] = ok(`{"total_count": 3, "environments": [
			` + applyEnvsJSON + `
			{"name": "copilot", "protection_rules": [], "can_admins_bypass": false, "deployment_branch_policy": null}]}`)
		f, c := newFakeGitHub(t, routes)
		err := syncSettings(context.Background(), writeRepo(t, syncedRuleset), c, repo, onlyEnvironments, &bytes.Buffer{})
		require.ErrorIs(t, err, errHTTP)
		writes := f.writes()
		require.Len(t, writes, 1)
		assert.Equal(t, "PUT /repos/o/r/environments/x-plan", writes[0].method+" "+writes[0].target)
	})

	t.Run("宣言を読めなければ何も送らない", func(t *testing.T) {
		t.Parallel()
		f, c := newFakeGitHub(t, liveRoutes(t))
		require.ErrorIs(t, syncSettings(context.Background(), writeRepo(t, rulesetFixture), c, repo, onlyRuleset, &bytes.Buffer{}), errIncludeDrift)
		assert.Empty(t, f.requests)
	})
}

func compactJSON(t *testing.T, s string) string {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, json.Compact(&buf, []byte(s)))
	return buf.String()
}

func Test_observe(t *testing.T) {
	t.Parallel()

	t.Run("宣言と実態と差分を返す", func(t *testing.T) {
		t.Parallel()
		_, c := newFakeGitHub(t, liveRoutes(t))
		want, got, d, err := observe(context.Background(), writeRepo(t, syncedRuleset), c, repo)
		require.NoError(t, err)
		assert.Len(t, want.Envs, 4)
		assert.Len(t, got.Envs, 4)
		assert.Empty(t, d.findings())
	})

	t.Run("宣言を読めなければ GitHub へ問わない", func(t *testing.T) {
		t.Parallel()
		f, c := newFakeGitHub(t, liveRoutes(t))
		_, _, _, err := observe(context.Background(), writeRepo(t, rulesetFixture), c, repo)
		require.ErrorIs(t, err, errIncludeDrift)
		assert.Empty(t, f.requests)
	})

	t.Run("実態を読めなければ失敗する", func(t *testing.T) {
		t.Parallel()
		_, c := newFakeGitHub(t, map[string]fakeResponse{})
		_, _, _, err := observe(context.Background(), writeRepo(t, syncedRuleset), c, repo)
		require.ErrorIs(t, err, errHTTP)
	})
}

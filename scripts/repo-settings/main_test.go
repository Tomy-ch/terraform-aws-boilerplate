package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
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
	branchesFixture     = `{"deploy": [{"branch": "main", "env": "x", "from": ["release/"], "release_base": true}], "lines": {"release": "release/", "hotfix": "hotfix/"}, "line_version": "v<major>.<minor>.<patch>", "revert": "revert/"}`
	environmentsFixture = `[
  {"name": "x-plan", "deploy_from": "protected-and-pull-requests", "can_admins_bypass": false},
  {"name": "x-apply", "deploy_from": "deploy-branch", "can_admins_bypass": false},
  {"name": "x-apply-identity", "deploy_from": "deploy-branch", "can_admins_bypass": false},
  {"name": "x-unlock-identity", "deploy_from": "deploy-branch", "can_admins_bypass": false},
  {"name": "x-unlock", "deploy_from": "deploy-branch", "can_admins_bypass": false},
  {"name": "copilot", "deploy_from": "any", "can_admins_bypass": false}
]`
	applyWorkflowFixture = `name: Apply

on:
  push:
    branches:
      - main

jobs:
  apply:
    runs-on: ubuntu-24.04
    environment: x-apply-identity
    steps:
      - run: make ci-apply
`
	identityRootFixture = `module "deployment_identity" {
  source = "../../../usecases/deployment-identity"
  env    = "x"
}
`
)

// editorconfig-checker-enable

var syncedRuleset = strings.Replace(rulesetFixture,
	"[\n        \"refs/heads/old\"\n      ]",
	"[\n        \"refs/heads/main\",\n        \"refs/heads/release/**/*\",\n        \"refs/heads/hotfix/**/*\"\n      ]", 1)

func writeRepo(t *testing.T, ruleset string) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, branchesFile, branchesFixture)
	writeFile(t, root, environmentsFile, environmentsFixture)
	writeFile(t, root, rulesetFile, ruleset)
	writeFile(t, root, vulnerabilityReportingFile, `{"enabled": true}`)
	writeFile(t, root, dependabotFile, dependabotFixture)
	writeFile(t, root, labelsFile, labelsFixture)
	writeFile(t, root, "env/x/identity/main.tf", identityRootFixture)
	writeFile(t, root, ".github/workflows/apply.yaml", applyWorkflowFixture)
	writeFile(t, root, ".github/workflows/lint.yaml", "on:\n  push:\n    branches: # branch-set: deploy\n      - 'main'\n  pull_request:\nconcurrency:\n  group: ${{ github.ref }}\n")
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
		rulesetListPath(1):                         ok(`[{"id": 7, "name": "branch-protection", "source_type": "Repository"}]`),
		"GET /repos/o/r/rulesets/7":                ok(string(body)),
		"GET /repos/o/r":                           ok(`{"visibility": "public"}`),
		"GET " + vulnerabilityReportingPath("o/r"): ok(`{"enabled": true}`),
		envListPath(1): ok(`{"total_count": 6, "environments": [
			{"name": "x-plan", "protection_rules": [], "can_admins_bypass": false, "deployment_branch_policy": {"protected_branches": false, "custom_branch_policies": true}},
			{"name": "x-apply", "protection_rules": [], "can_admins_bypass": false, "deployment_branch_policy": {"protected_branches": false, "custom_branch_policies": true}},
			{"name": "x-apply-identity", "protection_rules": [], "can_admins_bypass": false, "deployment_branch_policy": {"protected_branches": false, "custom_branch_policies": true}},
			{"name": "x-unlock-identity", "protection_rules": [], "can_admins_bypass": false, "deployment_branch_policy": {"protected_branches": false, "custom_branch_policies": true}},
			{"name": "x-unlock", "protection_rules": [], "can_admins_bypass": false, "deployment_branch_policy": {"protected_branches": false, "custom_branch_policies": true}},
			{"name": "copilot", "protection_rules": [], "can_admins_bypass": false, "deployment_branch_policy": null}]}`),
		policyPath("x-plan", 1): ok(`{"total_count": 4, "branch_policies": [
			{"id": 1, "name": "main", "type": "branch"}, {"id": 2, "name": "release/*", "type": "branch"}, {"id": 5, "name": "hotfix/*", "type": "branch"}, {"id": 3, "name": "refs/pull/*/merge", "type": "branch"}]}`),
		policyPath("x-apply", 1):           ok(deployPolicies),
		policyPath("x-apply-identity", 1):  ok(deployPolicies),
		policyPath("x-unlock-identity", 1): ok(deployPolicies),
		policyPath("x-unlock", 1):          ok(deployPolicies),
	}
}

const deployPolicies = `{"total_count": 1, "branch_policies": [{"id": 4, "name": "main", "type": "branch"}]}`

// applyEnvsJSON は liveRoutes の plan 以外の environment。environment の一覧を差し替えるケースが含める。
const applyEnvsJSON = `{"name": "x-apply", "protection_rules": [], "can_admins_bypass": false, "deployment_branch_policy": {"protected_branches": false, "custom_branch_policies": true}},
			{"name": "x-apply-identity", "protection_rules": [], "can_admins_bypass": false, "deployment_branch_policy": {"protected_branches": false, "custom_branch_policies": true}},
			{"name": "x-unlock-identity", "protection_rules": [], "can_admins_bypass": false, "deployment_branch_policy": {"protected_branches": false, "custom_branch_policies": true}},
			{"name": "x-unlock", "protection_rules": [], "can_admins_bypass": false, "deployment_branch_policy": {"protected_branches": false, "custom_branch_policies": true}},`

func appendFile(t *testing.T, dir, name, body string) {
	t.Helper()
	p := filepath.Join(dir, name)
	src, err := os.ReadFile(p)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(p, append(src, []byte(body)...), 0o600))
}

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
		{"source に -base が無ければ usage で失敗する", []string{"source", "-default", "release/v1"}},
		{"source に -env を渡せば usage で失敗する", []string{"source", "-base", "main", "-env", "x"}},
		{"deploy-branch に -env が無ければ usage で失敗する", []string{"deploy-branch"}},
		{"deploy-branch に -base を渡せば usage で失敗する", []string{"deploy-branch", "-env", "x", "-base", "main"}},
	}

	t.Run("source と deploy-branch を呼ぶ", func(t *testing.T) {
		t.Parallel()
		root := writeRepo(t, syncedRuleset)
		var out bytes.Buffer
		require.NoError(t, run([]string{"source", "-base", "release/v1"}, fixedWD(root), githubClient{}, &out))
		assert.Contains(t, out.String(), "対象外")
		out.Reset()
		require.NoError(t, run([]string{"deploy-branch", "-env", "x"}, fixedWD(root), githubClient{}, &out))
		assert.Equal(t, "main\n", out.String())
	})
	t.Run("source は -base-sha と -head-sha と -default を検査へ渡す", func(t *testing.T) {
		t.Parallel()
		root := writeRepo(t, syncedRuleset)
		gitT(t, root, "init", "-q", "-b", "main")
		gitT(t, root, "add", ".")
		gitT(t, root, "commit", "-q", "-m", "base")
		base := gitT(t, root, "rev-parse", "HEAD")
		gitT(t, root, "switch", "-q", "-c", "rel")
		writeFile(t, root, "main.tf", "b\n")
		gitT(t, root, "add", "main.tf")
		gitT(t, root, "commit", "-q", "-m", "release")
		head := gitT(t, root, "rev-parse", "HEAD")
		gitT(t, root, "update-ref", "refs/remotes/origin/release/v1", head)
		gitT(t, root, "switch", "-q", "-c", "evil", base)
		writeFile(t, root, "main.tf", "evil\n")
		gitT(t, root, "add", "main.tf")
		gitT(t, root, "commit", "-q", "-m", "evil")
		evil := gitT(t, root, "rev-parse", "HEAD")

		var out bytes.Buffer
		require.NoError(t, run([]string{"source", "-base", "main", "-base-sha", base, "-head-sha", head, "-default", "release/v1"}, fixedWD(root), githubClient{}, &out))
		assert.Contains(t, out.String(), "release/v1 から到達でき")
		err := run([]string{"source", "-base", "main", "-base-sha", base, "-head-sha", evil, "-default", "release/v1"}, fixedWD(root), githubClient{}, &bytes.Buffer{})
		require.ErrorIs(t, err, errSource)
	})
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
		assert.Contains(t, out.String(), rulesetFile+" を書き換えました")
		require.NoError(t, run([]string{"check"}, fixedWD(root), githubClient{}, &bytes.Buffer{}))
	})

	t.Run("drift は -repo のリポジトリを読む", func(t *testing.T) {
		t.Parallel()
		f, c := newFakeGitHub(t, liveRoutes(t))
		var out bytes.Buffer
		require.NoError(t, run([]string{"drift", "-repo=o/r"}, fixedWD(writeRepo(t, syncedRuleset)), c, &out))
		require.NotEmpty(t, f.requests)
		assert.Equal(t, rulesetListPath(1), "GET "+f.requests[0].target)
		assert.Equal(t, "✅ repo-settings: o/r の ruleset 1 件と environment 6 件（実在 6 件）と Private Vulnerability Reporting が宣言どおりです\n", out.String())
	})

	t.Run("sync は GITHUB_TOKEN が無ければ何も送らずに失敗する", func(t *testing.T) {
		t.Parallel()
		f, c := newFakeGitHub(t, liveRoutes(t))
		c.token = ""
		err := run([]string{"sync", "-repo=o/r", "-only", onlyRuleset}, fixedWD(writeRepo(t, syncedRuleset)), c, &bytes.Buffer{})
		require.ErrorIs(t, err, errNoToken)
		assert.Empty(t, f.requests)
	})

	t.Run("sync は -only vulnerability-reporting を受ける", func(t *testing.T) {
		t.Parallel()
		_, c := newFakeGitHub(t, liveRoutes(t))
		var out bytes.Buffer
		require.NoError(t, run([]string{"sync", "-repo=o/r", "-only", onlyVulnerabilityReporting}, fixedWD(writeRepo(t, syncedRuleset)), c, &out))
		assert.Equal(t, "✅ repo-settings: o/r の vulnerability-reporting へ送る要求はありません\n", out.String())
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

	t.Run("宣言を読み、include を生成し直す", func(t *testing.T) {
		t.Parallel()
		decl, err := readDeclarations(writeRepo(t, rulesetFixture))
		require.NoError(t, err)
		assert.Equal(t, []string{"main"}, decl.branches.DeployNames())
		assert.Len(t, decl.environments, 6)
		assert.True(t, decl.vulnerabilityReporting)
		assert.Equal(t, rulesetFixture, string(decl.ruleset))
		assert.Equal(t, syncedRuleset, string(decl.rendered))
		assert.Len(t, decl.workflows, 1)
		assert.Len(t, decl.sets, 1)
		assert.Equal(t, 1, decl.marks)
		assert.Equal(t, 1, decl.exprs)
		assert.Equal(t, 2, decl.dependabotLabels)
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
		{"private-vulnerability-reporting.json が壊れていれば失敗する", func(root string) {
			writeFile(t, root, vulnerabilityReportingFile, `{"enabled": null}`)
		}, errDeclaration},
		{"private-vulnerability-reporting.json が無ければ失敗する", func(root string) {
			require.NoError(t, os.Remove(filepath.Join(root, vulnerabilityReportingFile)))
		}, os.ErrNotExist},
		{"Dependabot のラベルが labels.json に無ければ失敗する", func(root string) {
			writeFile(t, root, labelsFile, `[{"name": "bug"}]`)
		}, errDependabotLabels},
		{"dependabot.yml が無ければ失敗する", func(root string) {
			require.NoError(t, os.Remove(filepath.Join(root, dependabotFile)))
		}, os.ErrNotExist},
		{"labels.json が無ければ失敗する", func(root string) {
			require.NoError(t, os.Remove(filepath.Join(root, labelsFile)))
		}, os.ErrNotExist},
		{"branch-protection.json が無ければ失敗する", func(root string) {
			require.NoError(t, os.Remove(filepath.Join(root, rulesetFile)))
		}, os.ErrNotExist},
		{"branch-protection.json が ADR-0603 を満たさなければ失敗する", func(root string) {
			writeFile(t, root, rulesetFile, strings.Replace(rulesetFixture, `"bypass_actors": []`, `"bypass_actors": [1]`, 1))
		}, errRulesetPolicy},
		{"信頼条件の environment が宣言に揃っていなければ失敗する", func(root string) {
			writeFile(t, root, "env/y/identity/main.tf", strings.Replace(identityRootFixture, `"x"`, `"y"`, 1))
		}, errEnvironmentMismatch},
		{"deployment-identity の env にデプロイブランチが無ければ失敗する", func(root string) {
			writeFile(t, root, branchesFile, `{"deploy": [{"branch": "main", "release_base": true}], "lines": {"release": "release/", "hotfix": "hotfix/"}, "line_version": "v<major>.<minor>.<patch>", "revert": "revert/"}`)
		}, errDeployMismatch},
		{"deployment-identity を呼ぶ root が無ければ失敗する", func(root string) {
			require.NoError(t, os.RemoveAll(filepath.Join(root, "env")))
		}, os.ErrNotExist},
		{"apply の workflow が無ければ失敗する", func(root string) {
			require.NoError(t, os.Remove(filepath.Join(root, ".github/workflows/apply.yaml")))
		}, errTrigger},
		{"ref を宣言の外の文字列と比べる式があれば失敗する", func(root string) {
			appendFile(t, root, ".github/workflows/lint.yaml", "jobs:\n  a:\n    if: github.ref_name == 'main'\n")
		}, errRefLiteral},
		{"読めない式があれば失敗する", func(root string) {
			appendFile(t, root, ".github/workflows/lint.yaml", "jobs:\n  a:\n    steps:\n      - run: echo ${{ github.ref\n")
		}, errExpression},
		{"印の無い branches の一覧があれば失敗する", func(root string) {
			appendFile(t, root, ".github/workflows/lint.yaml", "  workflow_run:\n    branches:\n      - main\n")
		}, errBranchSet},
		{"apply の workflow に印があれば失敗する", func(root string) {
			appendFile(t, root, ".github/workflows/apply.yaml", "    if: ${{ contains(fromJSON('[\"main\"]'), github.base_ref) }} # branch-set: deploy\n")
		}, errBranchSet},
		{"印 branch-set の行が1つも無ければ失敗する", func(root string) {
			writeFile(t, root, ".github/workflows/lint.yaml", "on:\n  pull_request:\nconcurrency:\n  group: ${{ github.ref }}\n")
		}, errBranchSet},
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

	// 印の生成の失敗は、印が0件のときと同じ errBranchSet なので、どちらの失敗かを文言で分ける。
	for name, c := range map[string]struct {
		file, body, want string
	}{
		"印の無い branches の一覧は、その一覧の失敗として返す":  {".github/workflows/lint.yaml", "  workflow_run:\n    branches:\n      - main\n", "印 `# branch-set: <集合>` の無い branches"},
		"apply の workflow の印は、その印の失敗として返す": {".github/workflows/apply.yaml", "    if: ${{ contains(fromJSON('[\"main\"]'), github.base_ref) }} # branch-set: deploy\n", "apply の workflow には印を置けません"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := writeRepo(t, rulesetFixture)
			appendFile(t, root, c.file, c.body)
			_, err := readDeclarations(root)
			require.ErrorIs(t, err, errBranchSet)
			assert.ErrorContains(t, err, c.want)
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
			{Name: "x-plan", Patterns: []string{"main", "release/*", "hotfix/*", pullRequestRef}},
			{Name: "x-apply", Patterns: []string{"main"}},
			{Name: "x-apply-identity", Patterns: []string{"main"}},
			{Name: "x-unlock-identity", Patterns: []string{"main"}},
			{Name: "x-unlock", Patterns: []string{"main"}},
			{Name: "copilot"},
		}, want.Envs)
	})

	t.Run("Private Vulnerability Reporting の宣言を渡す", func(t *testing.T) {
		t.Parallel()
		root := writeRepo(t, syncedRuleset)
		writeFile(t, root, vulnerabilityReportingFile, `{"enabled": false}`)
		want, err := loadDesired(root)
		require.NoError(t, err)
		assert.False(t, want.VulnerabilityReporting)
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
		assert.Contains(t, out.String(), "保護対象 3 件、environment 6 件、Private Vulnerability Reporting の宣言（enabled=true）、apply の workflow 1 件")
		assert.Contains(t, out.String(), rulesetFile+" を書き換えました")
	})

	t.Run("apply は apply の workflow の起動条件を書き換える", func(t *testing.T) {
		t.Parallel()
		root := writeRepo(t, syncedRuleset)
		writeFile(t, root, ".github/workflows/apply.yaml", strings.Replace(applyWorkflowFixture, "      - main", "      - 'release/*'", 1))
		var out bytes.Buffer
		require.NoError(t, applyOrCheck(root, false, &out))
		got, err := os.ReadFile(filepath.Join(root, ".github/workflows/apply.yaml"))
		require.NoError(t, err)
		assert.Equal(t, applyWorkflowFixture, string(got))
		assert.Contains(t, out.String(), ".github/workflows/apply.yaml を書き換えました")
	})

	t.Run("check は起動条件のずれを報せ、書かない", func(t *testing.T) {
		t.Parallel()
		root := writeRepo(t, syncedRuleset)
		drifted := strings.Replace(applyWorkflowFixture, "      - main", "      - 'release/*'", 1)
		writeFile(t, root, ".github/workflows/apply.yaml", drifted)
		err := applyOrCheck(root, true, &bytes.Buffer{})
		require.ErrorIs(t, err, errTriggerDrift)
		assert.Contains(t, err.Error(), ".github/workflows/apply.yaml")
		got, err := os.ReadFile(filepath.Join(root, ".github/workflows/apply.yaml"))
		require.NoError(t, err)
		assert.Equal(t, drifted, string(got))
	})

	t.Run("apply は印 branch-set の行を生成し直して書き換える", func(t *testing.T) {
		t.Parallel()
		root := writeRepo(t, syncedRuleset)
		synced, err := os.ReadFile(filepath.Join(root, ".github/workflows/lint.yaml"))
		require.NoError(t, err)
		writeFile(t, root, ".github/workflows/lint.yaml", strings.Replace(string(synced), "      - 'main'", "      - 'old'", 1))
		var out bytes.Buffer
		require.NoError(t, applyOrCheck(root, false, &out))
		got, err := os.ReadFile(filepath.Join(root, ".github/workflows/lint.yaml"))
		require.NoError(t, err)
		assert.Equal(t, string(synced), string(got))
		assert.Contains(t, out.String(), "分岐の集合の印 1 件（workflow 1 件）、ref を比べる式の検査 1 件")
		assert.Contains(t, out.String(), ".github/workflows/lint.yaml を書き換えました")
	})

	t.Run("check は印 branch-set の行のずれを報せ、書かない", func(t *testing.T) {
		t.Parallel()
		root := writeRepo(t, syncedRuleset)
		synced, err := os.ReadFile(filepath.Join(root, ".github/workflows/lint.yaml"))
		require.NoError(t, err)
		drifted := strings.Replace(string(synced), "      - 'main'", "      - 'old'", 1)
		writeFile(t, root, ".github/workflows/lint.yaml", drifted)
		err = applyOrCheck(root, true, &bytes.Buffer{})
		require.ErrorIs(t, err, errSetDrift)
		assert.ErrorContains(t, err, ".github/workflows/lint.yaml")
		got, err := os.ReadFile(filepath.Join(root, ".github/workflows/lint.yaml"))
		require.NoError(t, err)
		assert.Equal(t, drifted, string(got))
	})

	t.Run("include と起動条件と印が同時にずれていれば、check は include を先に報せ、次に起動条件を報せる", func(t *testing.T) {
		t.Parallel()
		root := writeRepo(t, rulesetFixture)
		writeFile(t, root, ".github/workflows/apply.yaml", strings.Replace(applyWorkflowFixture, "      - main", "      - 'release/*'", 1))
		synced, err := os.ReadFile(filepath.Join(root, ".github/workflows/lint.yaml"))
		require.NoError(t, err)
		writeFile(t, root, ".github/workflows/lint.yaml", strings.Replace(string(synced), "      - 'main'", "      - 'old'", 1))
		require.ErrorIs(t, applyOrCheck(root, true, &bytes.Buffer{}), errIncludeDrift)

		writeFile(t, root, rulesetFile, syncedRuleset)
		err = applyOrCheck(root, true, &bytes.Buffer{})
		require.ErrorIs(t, err, errTriggerDrift)
		assert.NotErrorIs(t, err, errSetDrift)
	})

	t.Run("include と起動条件が同時にずれていれば、apply は両方を書き換える", func(t *testing.T) {
		t.Parallel()
		root := writeRepo(t, rulesetFixture)
		writeFile(t, root, ".github/workflows/apply.yaml", strings.Replace(applyWorkflowFixture, "      - main", "      - 'release/*'", 1))
		var out bytes.Buffer
		require.NoError(t, applyOrCheck(root, false, &out))
		ruleset, err := os.ReadFile(filepath.Join(root, rulesetFile))
		require.NoError(t, err)
		assert.Equal(t, syncedRuleset, string(ruleset))
		wf, err := os.ReadFile(filepath.Join(root, ".github/workflows/apply.yaml"))
		require.NoError(t, err)
		assert.Equal(t, applyWorkflowFixture, string(wf))
		assert.Contains(t, out.String(), rulesetFile)
		assert.Contains(t, out.String(), ".github/workflows/apply.yaml")
	})

	t.Run("include と起動条件が同時にずれていれば、check は include のずれを報せ、どちらも書かない", func(t *testing.T) {
		t.Parallel()
		root := writeRepo(t, rulesetFixture)
		drifted := strings.Replace(applyWorkflowFixture, "      - main", "      - 'release/*'", 1)
		writeFile(t, root, ".github/workflows/apply.yaml", drifted)
		require.ErrorIs(t, applyOrCheck(root, true, &bytes.Buffer{}), errIncludeDrift)
		ruleset, err := os.ReadFile(filepath.Join(root, rulesetFile))
		require.NoError(t, err)
		assert.Equal(t, rulesetFixture, string(ruleset))
		wf, err := os.ReadFile(filepath.Join(root, ".github/workflows/apply.yaml"))
		require.NoError(t, err)
		assert.Equal(t, drifted, string(wf))
	})

	t.Run("workflow の起動条件を生成できなければ、apply は include のずれも書かない", func(t *testing.T) {
		t.Parallel()
		root := writeRepo(t, rulesetFixture)
		invalid := strings.Replace(applyWorkflowFixture, "    branches:\n      - main", "    branches: [main]", 1)
		writeFile(t, root, ".github/workflows/apply.yaml", invalid)
		require.ErrorIs(t, applyOrCheck(root, false, &bytes.Buffer{}), errTrigger)
		ruleset, err := os.ReadFile(filepath.Join(root, rulesetFile))
		require.NoError(t, err)
		assert.Equal(t, rulesetFixture, string(ruleset))
		wf, err := os.ReadFile(filepath.Join(root, ".github/workflows/apply.yaml"))
		require.NoError(t, err)
		assert.Equal(t, invalid, string(wf))
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
		assert.Equal(t, "✅ repo-settings: 保護対象 3 件、environment 6 件、Private Vulnerability Reporting の宣言（enabled=true）、apply の workflow 1 件、分岐の集合の印 1 件（workflow 1 件）、ref を比べる式の検査 1 件、labels.json にある Dependabot のラベル 2 件。生成する区間は宣言どおりです\n", out.String())
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

	t.Run("Private Vulnerability Reporting の宣言が不正なら apply は include の区間も書き換えない", func(t *testing.T) {
		t.Parallel()
		root := writeRepo(t, rulesetFixture)
		writeFile(t, root, vulnerabilityReportingFile, `{"enabled": null}`)
		require.ErrorIs(t, applyOrCheck(root, false, &bytes.Buffer{}), errDeclaration)
		got, err := os.ReadFile(filepath.Join(root, rulesetFile))
		require.NoError(t, err)
		assert.Equal(t, rulesetFixture, string(got))
	})

	t.Run("Dependabot のラベルが labels.json に無ければ、apply は include の区間も起動条件も書き換えない", func(t *testing.T) {
		t.Parallel()
		root := writeRepo(t, rulesetFixture)
		drifted := strings.Replace(applyWorkflowFixture, "      - main", "      - 'release/*'", 1)
		writeFile(t, root, ".github/workflows/apply.yaml", drifted)
		writeFile(t, root, labelsFile, `[{"name": "bug"}]`)
		require.ErrorIs(t, applyOrCheck(root, false, &bytes.Buffer{}), errDependabotLabels)
		ruleset, err := os.ReadFile(filepath.Join(root, rulesetFile))
		require.NoError(t, err)
		assert.Equal(t, rulesetFixture, string(ruleset))
		wf, err := os.ReadFile(filepath.Join(root, ".github/workflows/apply.yaml"))
		require.NoError(t, err)
		assert.Equal(t, drifted, string(wf))
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
		assert.ElementsMatch(t, []string{"branches.json", "environments.json", "branch-protection.json", "private-vulnerability-reporting.json", "labels.json"}, names)
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
		assert.Equal(t, "✅ repo-settings: o/r の ruleset 1 件と environment 6 件（実在 6 件）と Private Vulnerability Reporting が宣言どおりです\n", out.String())
		assert.Empty(t, f.writes())
	})

	t.Run("ずれをすべて出力して失敗する", func(t *testing.T) {
		t.Parallel()
		root := writeRepo(t, syncedRuleset)
		routes := liveRoutes(t)
		routes[envListPath(1)] = ok(`{"total_count": 6, "environments": [
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

	approvalRoutes := func(t *testing.T, from, to string) map[string]fakeResponse {
		t.Helper()
		var ruleset map[string]any
		require.NoError(t, json.Unmarshal([]byte(strings.Replace(syncedRuleset, from, to, 1)), &ruleset))
		ruleset["id"] = 7.0
		body, err := json.Marshal(ruleset)
		require.NoError(t, err)
		routes := liveRoutes(t)
		routes["GET /repos/o/r/rulesets/7"] = ok(string(body))
		return routes
	}

	t.Run("承認者数だけのずれを、決定7 の例外として分けて出力して失敗する", func(t *testing.T) {
		t.Parallel()
		routes := approvalRoutes(t, `"required_approving_review_count": 1`, `"required_approving_review_count": 0`)
		f, c := newFakeGitHub(t, routes)
		var out bytes.Buffer
		require.ErrorIs(t, drift(context.Background(), writeRepo(t, syncedRuleset), c, repo, &out), errDrift)
		assert.Equal(t, strings.Join([]string{
			"❌ repo-settings: o/r で宣言と実態のずれが 1 件あります",
			"  ADR-0603 決定7 の例外（承認者数の要件）に当たるずれ:",
			"  - ruleset: ルール pull_request の required_approving_review_count が宣言と異なります",
		}, "\n")+"\n", out.String())
		assert.Empty(t, f.writes())
	})

	t.Run("承認者数とほかのパラメータのずれは、両方を分けて出力して失敗する", func(t *testing.T) {
		t.Parallel()
		routes := approvalRoutes(t,
			"\"required_review_thread_resolution\": true,\n      \"required_approving_review_count\": 1",
			"\"required_review_thread_resolution\": false,\n      \"required_approving_review_count\": 0")
		_, c := newFakeGitHub(t, routes)
		var out bytes.Buffer
		require.ErrorIs(t, drift(context.Background(), writeRepo(t, syncedRuleset), c, repo, &out), errDrift)
		assert.Equal(t, strings.Join([]string{
			"❌ repo-settings: o/r で宣言と実態のずれが 2 件あります",
			"  - ruleset: ルール pull_request が宣言と異なります",
			"  ADR-0603 決定7 の例外（承認者数の要件）に当たるずれ:",
			"  - ruleset: ルール pull_request の required_approving_review_count が宣言と異なります",
		}, "\n")+"\n", out.String())
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
			"✅ repo-settings: o/r の ruleset 1 件と environment 6 件（実在 6 件）と Private Vulnerability Reporting が宣言どおりです（突合の対象外 1 件を除く）",
		}, "\n")+"\n", out.String())
	})

	t.Run("実態を読めなければ失敗する", func(t *testing.T) {
		t.Parallel()
		_, c := newFakeGitHub(t, map[string]fakeResponse{})
		require.ErrorIs(t, drift(context.Background(), writeRepo(t, syncedRuleset), c, repo, &bytes.Buffer{}), errHTTP)
	})

	t.Run("Private Vulnerability Reporting のずれを出力して失敗する", func(t *testing.T) {
		t.Parallel()
		routes := liveRoutes(t)
		routes["GET "+vulnerabilityReportingPath(repo)] = ok(`{"enabled": false}`)
		f, c := newFakeGitHub(t, routes)
		var out bytes.Buffer
		require.ErrorIs(t, drift(context.Background(), writeRepo(t, syncedRuleset), c, repo, &out), errDrift)
		assert.Equal(t, strings.Join([]string{
			"❌ repo-settings: o/r で宣言と実態のずれが 1 件あります",
			"  - Private Vulnerability Reporting: enabled が宣言の true と異なります",
		}, "\n")+"\n", out.String())
		assert.Empty(t, f.writes())
	})

	t.Run("公開でないリポジトリが有効を宣言していれば、ずれとして失敗する", func(t *testing.T) {
		t.Parallel()
		routes := liveRoutes(t)
		routes["GET /repos/o/r"] = ok(`{"visibility": "private"}`)
		_, c := newFakeGitHub(t, routes)
		var out bytes.Buffer
		require.ErrorIs(t, drift(context.Background(), writeRepo(t, syncedRuleset), c, repo, &out), errDrift)
		assert.Equal(t, strings.Join([]string{
			"❌ repo-settings: o/r で宣言と実態のずれが 1 件あります",
			"  - Private Vulnerability Reporting: 公開でないリポジトリでは使えません",
		}, "\n")+"\n", out.String())
	})

	t.Run("公開でないリポジトリが無効を宣言していれば、突合の対象外として数え、一致に含めない", func(t *testing.T) {
		t.Parallel()
		root := writeRepo(t, syncedRuleset)
		writeFile(t, root, vulnerabilityReportingFile, `{"enabled": false}`)
		routes := liveRoutes(t)
		routes["GET /repos/o/r"] = ok(`{"visibility": "private"}`)
		_, c := newFakeGitHub(t, routes)
		var out bytes.Buffer
		require.NoError(t, drift(context.Background(), root, c, repo, &out))
		assert.Equal(t, strings.Join([]string{
			"⚠️ 突合の対象外（観測できない）: Private Vulnerability Reporting（公開でないリポジトリには提供されない）",
			"✅ repo-settings: o/r の ruleset 1 件と environment 6 件（実在 6 件）が宣言どおりです（突合の対象外 1 件を除く）",
		}, "\n")+"\n", out.String())
	})

	t.Run("Private Vulnerability Reporting を読めなくても ruleset と environment の結果を出し、読めなかったことで失敗する", func(t *testing.T) {
		t.Parallel()
		routes := liveRoutes(t)
		routes["GET "+vulnerabilityReportingPath(repo)] = fakeResponse{status: http.StatusForbidden, body: `{"message": "Resource not accessible"}`}
		_, c := newFakeGitHub(t, routes)
		var out bytes.Buffer
		err := drift(context.Background(), writeRepo(t, syncedRuleset), c, repo, &out)
		require.ErrorIs(t, err, errHTTP)
		assert.Equal(t, "❌ repo-settings: o/r の ruleset 1 件と environment 6 件（実在 6 件）は宣言どおりですが、Private Vulnerability Reporting の実態を読めませんでした\n", out.String())
		assert.ErrorContains(t, err, "Private Vulnerability Reporting の実態の読み取り")
	})

	t.Run("ほかにずれがあり Private Vulnerability Reporting を読めなければ、両方を出力する", func(t *testing.T) {
		t.Parallel()
		routes := liveRoutes(t)
		routes["GET "+vulnerabilityReportingPath(repo)] = fakeResponse{status: http.StatusForbidden, body: `{}`}
		routes[envListPath(1)] = ok(`{"total_count": 6, "environments": [
			` + applyEnvsJSON + `
			{"name": "copilot", "protection_rules": [], "can_admins_bypass": true, "deployment_branch_policy": null},
			{"name": "x-plan", "protection_rules": [], "can_admins_bypass": false, "deployment_branch_policy": {"protected_branches": false, "custom_branch_policies": true}}]}`)
		_, c := newFakeGitHub(t, routes)
		var out bytes.Buffer
		require.ErrorIs(t, drift(context.Background(), writeRepo(t, syncedRuleset), c, repo, &out), errDrift)
		assert.Contains(t, out.String(), "  - environment copilot: 管理者による保護の迂回が有効です\n")
		assert.Contains(t, out.String(), "❌ repo-settings: Private Vulnerability Reporting の実態を読めませんでした: ")
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
		routes[envListPath(1)] = ok(`{"total_count": 6, "environments": [
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
			"POST /repos/o/r/environments/x-plan/deployment-branch-policies",
			"PUT /repos/o/r/environments/copilot",
		}, targets)
		assert.JSONEq(t, `{"can_admins_bypass": false, "deployment_branch_policy": {"protected_branches": false, "custom_branch_policies": true},
			"prevent_self_review": false, "reviewers": null, "wait_timer": 0}`, writes[0].body)
		assert.JSONEq(t, `{"name": "refs/pull/*/merge", "type": "branch"}`, writes[4].body)
		assert.Equal(t, strings.Join([]string{
			"🔧 PUT /repos/o/r/environments/x-plan",
			"🔧 POST /repos/o/r/environments/x-plan/deployment-branch-policies",
			"🔧 POST /repos/o/r/environments/x-plan/deployment-branch-policies",
			"🔧 POST /repos/o/r/environments/x-plan/deployment-branch-policies",
			"🔧 POST /repos/o/r/environments/x-plan/deployment-branch-policies",
			"🔧 PUT /repos/o/r/environments/copilot",
			"⚠️ 宣言に無い environment は消していません（make repo-settings-drift はこれをずれとして報せます）: other",
			"✅ repo-settings: o/r へ 6 件の要求を送りました。make repo-settings-drift で結果を確かめてください",
		}, "\n")+"\n", out.String())
	})

	t.Run("宣言に無い environment だけがずれていれば、何も送らずにそれを報せる", func(t *testing.T) {
		t.Parallel()
		routes := liveRoutes(t)
		routes[envListPath(1)] = ok(`{"total_count": 7, "environments": [
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
		routes[envListPath(1)] = ok(`{"total_count": 7, "environments": [
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
		routes[envListPath(1)] = ok(`{"total_count": 5, "environments": [
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

	for _, only := range []string{onlyRuleset, onlyEnvironments} {
		t.Run(only+" の適用は Private Vulnerability Reporting を問わない", func(t *testing.T) {
			t.Parallel()
			routes := liveRoutes(t)
			routes["GET /repos/o/r"] = fakeResponse{status: http.StatusForbidden, body: `{}`}
			routes["GET "+vulnerabilityReportingPath(repo)] = fakeResponse{status: http.StatusForbidden, body: `{}`}
			f, c := newFakeGitHub(t, routes)
			require.NoError(t, syncSettings(context.Background(), writeRepo(t, syncedRuleset), c, repo, only, &bytes.Buffer{}))
			for _, r := range f.requests {
				assert.NotEqual(t, "/repos/o/r", r.target)
				assert.NotEqual(t, vulnerabilityReportingPath(repo), r.target)
			}
		})
	}

	t.Run("vulnerability-reporting の適用は ruleset と environment を問わない", func(t *testing.T) {
		t.Parallel()
		routes := liveRoutes(t)
		routes["GET "+vulnerabilityReportingPath(repo)] = ok(`{"enabled": false}`)
		routes["PUT "+vulnerabilityReportingPath(repo)] = fakeResponse{status: http.StatusNoContent}
		f, c := newFakeGitHub(t, routes)
		require.NoError(t, syncSettings(context.Background(), writeRepo(t, syncedRuleset), c, repo, onlyVulnerabilityReporting, &bytes.Buffer{}))
		targets := make([]string, 0, len(f.requests))
		for _, r := range f.requests {
			targets = append(targets, r.method+" "+r.target)
		}
		assert.Equal(t, []string{"GET /repos/o/r", "GET " + vulnerabilityReportingPath(repo), "PUT " + vulnerabilityReportingPath(repo)}, targets)
	})
}

func Test_syncVulnerabilityReporting(t *testing.T) {
	t.Parallel()

	pvr := vulnerabilityReportingPath(repo)

	t.Run("宣言どおりなら何も送らない", func(t *testing.T) {
		t.Parallel()
		f, c := newFakeGitHub(t, liveRoutes(t))
		var out bytes.Buffer
		require.NoError(t, syncVulnerabilityReporting(context.Background(), writeRepo(t, syncedRuleset), c, repo, &out))
		assert.Empty(t, f.writes())
		assert.Equal(t, "✅ repo-settings: o/r の vulnerability-reporting へ送る要求はありません\n", out.String())
	})

	writeCases := []struct {
		name     string
		declared string
		live     string
		method   string
	}{
		{"宣言が有効で実態が無効なら PUT で有効にする", `{"enabled": true}`, `{"enabled": false}`, http.MethodPut},
		{"宣言が無効で実態が有効なら DELETE で無効にする", `{"enabled": false}`, `{"enabled": true}`, http.MethodDelete},
	}
	for _, tc := range writeCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := writeRepo(t, syncedRuleset)
			writeFile(t, root, vulnerabilityReportingFile, tc.declared)
			routes := liveRoutes(t)
			routes["GET "+pvr] = ok(tc.live)
			routes[tc.method+" "+pvr] = fakeResponse{status: http.StatusNoContent}
			f, c := newFakeGitHub(t, routes)
			var out bytes.Buffer
			require.NoError(t, syncVulnerabilityReporting(context.Background(), root, c, repo, &out))
			writes := f.writes()
			require.Len(t, writes, 1)
			assert.Equal(t, tc.method+" "+pvr, writes[0].method+" "+writes[0].target)
			assert.Empty(t, writes[0].body)
			assert.Equal(t, strings.Join([]string{
				"🔧 " + tc.method + " " + pvr,
				"✅ repo-settings: o/r へ 1 件の要求を送りました。make repo-settings-drift で結果を確かめてください",
			}, "\n")+"\n", out.String())
		})
	}

	t.Run("公開でないリポジトリが有効を宣言していれば、何も送らずに直し方を返す", func(t *testing.T) {
		t.Parallel()
		routes := liveRoutes(t)
		routes["GET /repos/o/r"] = ok(`{"visibility": "private"}`)
		f, c := newFakeGitHub(t, routes)
		err := syncVulnerabilityReporting(context.Background(), writeRepo(t, syncedRuleset), c, repo, &bytes.Buffer{})
		require.ErrorIs(t, err, errVulnerabilityReportingUnavailable)
		assert.Contains(t, err.Error(), vulnerabilityReportingFile+" の enabled を false にして commit")
		assert.Empty(t, f.writes())
	})

	t.Run("公開でないリポジトリが無効を宣言していれば、何も送らずに対象外と報せる", func(t *testing.T) {
		t.Parallel()
		root := writeRepo(t, syncedRuleset)
		writeFile(t, root, vulnerabilityReportingFile, `{"enabled": false}`)
		routes := liveRoutes(t)
		routes["GET /repos/o/r"] = ok(`{"visibility": "internal"}`)
		f, c := newFakeGitHub(t, routes)
		var out bytes.Buffer
		require.NoError(t, syncVulnerabilityReporting(context.Background(), root, c, repo, &out))
		assert.Empty(t, f.writes())
		assert.Equal(t, strings.Join([]string{
			"⚠️ 突合の対象外（観測できない）: Private Vulnerability Reporting（公開でないリポジトリには提供されない）",
			"✅ repo-settings: o/r の vulnerability-reporting へ送る要求はありません",
		}, "\n")+"\n", out.String())
	})

	t.Run("実態を読めなければ何も送らずに失敗する", func(t *testing.T) {
		t.Parallel()
		routes := liveRoutes(t)
		routes["GET "+pvr] = fakeResponse{status: http.StatusForbidden, body: `{}`}
		f, c := newFakeGitHub(t, routes)
		require.ErrorIs(t, syncVulnerabilityReporting(context.Background(), writeRepo(t, syncedRuleset), c, repo, &bytes.Buffer{}), errHTTP)
		assert.Empty(t, f.writes())
	})

	t.Run("書き込みが失敗すれば失敗する", func(t *testing.T) {
		t.Parallel()
		routes := liveRoutes(t)
		routes["GET "+pvr] = ok(`{"enabled": false}`)
		_, c := newFakeGitHub(t, routes)
		require.ErrorIs(t, syncVulnerabilityReporting(context.Background(), writeRepo(t, syncedRuleset), c, repo, &bytes.Buffer{}), errHTTP)
	})

	t.Run("宣言を読めなければ GitHub へ問わない", func(t *testing.T) {
		t.Parallel()
		root := writeRepo(t, syncedRuleset)
		writeFile(t, root, vulnerabilityReportingFile, `{}`)
		f, c := newFakeGitHub(t, liveRoutes(t))
		require.ErrorIs(t, syncVulnerabilityReporting(context.Background(), root, c, repo, &bytes.Buffer{}), errDeclaration)
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
		assert.Len(t, want.Envs, 6)
		assert.Len(t, got.Envs, 6)
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

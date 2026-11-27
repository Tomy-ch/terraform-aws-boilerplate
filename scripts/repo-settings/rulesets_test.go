package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/branchdecl"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/checksdecl"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/testenv"
)

// editorconfig-checker-disable -- raw string の fixture は埋め込んだ JSON の字下げを持つ。撤回: fixture を外部ファイルへ移したとき

const commonFixture = `{
  "name": "common",
  "target": "branch",
  "enforcement": "active",
  "conditions": {
    "ref_name": {
      "exclude": [],
      "include": [
        "refs/heads/old"
      ]
    }
  },
  "rules": [
    {"type": "deletion"},
    {"type": "non_fast_forward"},
    {"type": "pull_request", "parameters": {
      "require_code_owner_review": true,
      "required_review_thread_resolution": true,
      "required_approving_review_count": 1,
      "allowed_merge_methods": ["merge"]
    }}
  ],
  "bypass_actors": []
}
`

const linesFixture = `{
  "name": "lines",
  "target": "branch",
  "enforcement": "active",
  "conditions": {
    "ref_name": {
      "exclude": [],
      "include": [
        "refs/heads/old"
      ]
    }
  },
  "rules": [
    {"type": "required_status_checks", "parameters": {"required_status_checks": [{"context": "gate", "integration_id": 15368}]}}
  ],
  "bypass_actors": []
}
`

const checksFixture = `{"checks": [
  {"name": "lint", "workflow": "lint.yaml", "paths": [], "scope": "tree", "required": true},
  {"name": "plan-x", "workflow": "plan.yaml", "paths": [], "scope": "tree", "required": true, "plan_env": "x", "contexts": ["plan-x-identity", "plan-x-workload"]}
]}`

// deployMainFixture は、branchesFixture と checksFixture から生成する deploy-main.json そのもの。
const deployMainFixture = `{
  "name": "deploy-main",
  "target": "branch",
  "source_type": "Repository",
  "enforcement": "active",
  "conditions": {
    "ref_name": {
      "exclude": [],
      "include": [
        "refs/heads/main"
      ]
    }
  },
  "rules": [
    {
      "type": "required_status_checks",
      "parameters": {
        "strict_required_status_checks_policy": false,
        "do_not_enforce_on_create": false,
        "required_status_checks": [
          {
            "context": "deploy-source-check",
            "integration_id": 15368
          },
          {
            "context": "plan-x-identity",
            "integration_id": 15368
          },
          {
            "context": "plan-x-workload",
            "integration_id": 15368
          },
          {
            "context": "line-gate",
            "integration_id": 15368
          }
        ]
      }
    }
  ],
  "bypass_actors": []
}
`

// editorconfig-checker-enable

const (
	oldInclude     = "[\n        \"refs/heads/old\"\n      ]"
	deployMainFile = rulesetsDir + "/deploy-main.json"
)

var (
	syncedCommon = strings.Replace(commonFixture, oldInclude,
		"[\n        \"refs/heads/main\",\n        \"refs/heads/release/**/*\",\n        \"refs/heads/hotfix/**/*\"\n      ]", 1)
	syncedLines = strings.Replace(linesFixture, oldInclude,
		"[\n        \"refs/heads/release/**/*\",\n        \"refs/heads/hotfix/**/*\"\n      ]", 1)
	syncedLegacy = strings.Replace(rulesetFixture, oldInclude,
		"[\n        \"refs/heads/main\",\n        \"refs/heads/release/**/*\",\n        \"refs/heads/hotfix/**/*\"\n      ]", 1)

	commonSpec = rulesetSpec{path: commonRulesetFile, name: "common", role: roleCommon}
	linesSpec  = rulesetSpec{path: linesRulesetFile, name: "lines", role: roleLines}
	deploySpec = rulesetSpec{path: deployMainFile, name: "deploy-main", role: roleDeploy}
	legacySpec = rulesetSpec{path: legacyRulesetFile, name: "branch-protection", role: roleLegacy}
)

func lineBranches(deploy ...branchdecl.DeployBranch) branches {
	return branches{Deploy: deploy, Lines: branchdecl.Lines{Release: "release/", Hotfix: "hotfix/"}, Revert: "revert/"}
}

func Test_specFor(t *testing.T) {
	t.Parallel()

	t.Run("name はファイル名から .json を除いたもの", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, deploySpec, specFor(deployMainFile, roleDeploy))
		assert.Equal(t, legacySpec, specFor(legacyRulesetFile, roleLegacy))
	})
}

func Test_readRulesets(t *testing.T) {
	t.Parallel()

	read := func(t *testing.T, root string) ([]rulesetDecl, error) {
		t.Helper()
		b, err := branchdecl.Read(filepath.Join(root, branchesFile))
		require.NoError(t, err)
		return readRulesets(root, b)
	}

	t.Run("common・lines・deploy-* の順に、今の内容と生成し直した内容を返す", func(t *testing.T) {
		t.Parallel()
		root := writeRepo(t, commonFixture)
		writeFile(t, root, linesRulesetFile, linesFixture)
		got, err := read(t, root)
		require.NoError(t, err)
		require.Len(t, got, 3)
		assert.Equal(t, []rulesetSpec{commonSpec, linesSpec, deploySpec}, []rulesetSpec{got[0].spec, got[1].spec, got[2].spec})
		assert.Equal(t, commonFixture, string(got[0].current))
		assert.Equal(t, syncedCommon, string(got[0].rendered))
		assert.Equal(t, linesFixture, string(got[1].current))
		assert.Equal(t, syncedLines, string(got[1].rendered))
		assert.Equal(t, deployMainFixture, string(got[2].current))
		assert.Equal(t, deployMainFixture, string(got[2].rendered))
	})

	t.Run("deploy-* のファイルが無ければ、今の内容を nil にして生成する", func(t *testing.T) {
		t.Parallel()
		root := writeRepo(t, syncedCommon)
		require.NoError(t, os.Remove(filepath.Join(root, deployMainFile)))
		got, err := read(t, root)
		require.NoError(t, err)
		require.Len(t, got, 3)
		assert.Nil(t, got[2].current)
		assert.Equal(t, deployMainFixture, string(got[2].rendered))
	})

	t.Run("branch-protection.json が在れば、最後に並べて include を生成し直す", func(t *testing.T) {
		t.Parallel()
		root := writeRepo(t, syncedCommon)
		writeFile(t, root, legacyRulesetFile, rulesetFixture)
		got, err := read(t, root)
		require.NoError(t, err)
		require.Len(t, got, 4)
		assert.Equal(t, legacySpec, got[3].spec)
		assert.Equal(t, rulesetFixture, string(got[3].current))
		assert.Equal(t, syncedLegacy, string(got[3].rendered))
	})

	cases := []struct {
		name    string
		mutate  func(t *testing.T, root string)
		wantErr error
		message string
	}{
		{"rulesets/ が無ければ失敗する", func(t *testing.T, root string) {
			t.Helper()
			testenv.RemoveUnder(t, root, rulesetsDir)
		}, os.ErrNotExist, commonRulesetFile},
		{"lines.json が無ければ失敗する", func(t *testing.T, root string) {
			t.Helper()
			require.NoError(t, os.Remove(filepath.Join(root, linesRulesetFile)))
		}, os.ErrNotExist, linesRulesetFile},
		{"checks.json が無ければ失敗する", func(t *testing.T, root string) {
			t.Helper()
			require.NoError(t, os.Remove(filepath.Join(root, checksdecl.File)))
		}, os.ErrNotExist, checksdecl.File},
		{"checks.json を解釈できなければ失敗する", func(t *testing.T, root string) {
			t.Helper()
			writeFile(t, root, checksdecl.File, `{"checks": [{"name": "x"}]}`)
		}, checksdecl.ErrDecl, checksdecl.File},
		{"checks.json に検査が無ければ失敗する", func(t *testing.T, root string) {
			t.Helper()
			writeFile(t, root, checksdecl.File, `{"checks": []}`)
		}, checksdecl.ErrNoChecks, checksdecl.File},
		{"宣言に無い deploy-*.json があれば失敗する", func(t *testing.T, root string) {
			t.Helper()
			writeFile(t, root, rulesetsDir+"/deploy-old.json", deployMainFixture)
		}, errStaleRuleset, rulesetsDir + "/deploy-old.json"},
		{"plan_env の env にデプロイブランチが無ければ失敗する", func(t *testing.T, root string) {
			t.Helper()
			writeFile(t, root, checksdecl.File, strings.Replace(checksFixture, `"plan_env": "x"`, `"plan_env": "y"`, 1))
		}, errDeployRuleset, `"y"`},
		{"common.json が ADR-0603 を満たさなければ失敗する", func(t *testing.T, root string) {
			t.Helper()
			writeFile(t, root, commonRulesetFile, strings.Replace(syncedCommon, `{"type": "deletion"},`, ``, 1))
		}, errRulesetPolicy, commonRulesetFile},
		{"lines.json が ADR-0603 を満たさなければ失敗する", func(t *testing.T, root string) {
			t.Helper()
			writeFile(t, root, linesRulesetFile, strings.Replace(syncedLines, `"integration_id": 15368`, `"integration_id": 1`, 1))
		}, errRulesetPolicy, linesRulesetFile},
		{"lines.json の include を定められなければ失敗する", func(t *testing.T, root string) {
			t.Helper()
			writeFile(t, root, linesRulesetFile, strings.Replace(syncedLines, `"include"`, `"included"`, 1))
		}, errInclude, linesRulesetFile},
		{"branch-protection.json が ADR-0603 を満たさなければ失敗する", func(t *testing.T, root string) {
			t.Helper()
			writeFile(t, root, legacyRulesetFile, strings.Replace(rulesetFixture, `"type": "required_status_checks"`, `"type": "other"`, 1))
		}, errRulesetPolicy, legacyRulesetFile},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := writeRepo(t, syncedCommon)
			tc.mutate(t, root)
			_, err := read(t, root)
			require.ErrorIs(t, err, tc.wantErr)
			assert.ErrorContains(t, err, tc.message)
		})
	}

	t.Run("branch-protection.json を読めなければ、無いとは扱わずに失敗する", func(t *testing.T) {
		t.Parallel()
		root := writeRepo(t, syncedCommon)
		require.NoError(t, os.MkdirAll(filepath.Join(root, legacyRulesetFile), 0o750))
		_, err := read(t, root)
		require.Error(t, err)
		assert.NotErrorIs(t, err, os.ErrNotExist)
		assert.ErrorContains(t, err, legacyRulesetFile+" の読み取り")
	})

	t.Run("deploy-* のファイルを読めなければ、無いとは扱わずに失敗する", func(t *testing.T) {
		t.Parallel()
		root := writeRepo(t, syncedCommon)
		require.NoError(t, os.Remove(filepath.Join(root, deployMainFile)))
		require.NoError(t, os.MkdirAll(filepath.Join(root, deployMainFile), 0o750))
		_, err := read(t, root)
		require.Error(t, err)
		assert.NotErrorIs(t, err, os.ErrNotExist)
		assert.ErrorContains(t, err, deployMainFile+" の読み取り")
	})
}

func Test_readChecks(t *testing.T) {
	t.Parallel()

	t.Run("checks.json を checksdecl で読む", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		writeFile(t, root, checksdecl.File, checksFixture)
		got, err := readChecks(root)
		require.NoError(t, err)
		assert.Equal(t, []string{"lint", "plan-x"}, got.Names())
	})

	t.Run("無ければ失敗する", func(t *testing.T) {
		t.Parallel()
		_, err := readChecks(t.TempDir())
		require.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("解釈できなければ失敗する", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		writeFile(t, root, checksdecl.File, `x`)
		_, err := readChecks(root)
		require.ErrorIs(t, err, checksdecl.ErrDecl)
	})
}

func Test_renderAuthored(t *testing.T) {
	t.Parallel()

	includes := []string{"refs/heads/release/**/*", "refs/heads/hotfix/**/*"}

	t.Run("検証し、include の区間を生成し直す", func(t *testing.T) {
		t.Parallel()
		got, err := renderAuthored(linesSpec, []byte(linesFixture), includes)
		require.NoError(t, err)
		assert.Equal(t, rulesetDecl{spec: linesSpec, current: []byte(linesFixture), rendered: []byte(syncedLines)}, got)
	})

	t.Run("ADR-0603 を満たさなければ、ファイルを名指して失敗する", func(t *testing.T) {
		t.Parallel()
		_, err := renderAuthored(linesSpec, []byte(commonFixture), includes)
		require.ErrorIs(t, err, errRulesetPolicy)
		assert.ErrorContains(t, err, linesRulesetFile)
	})

	t.Run("include を定められなければ失敗する", func(t *testing.T) {
		t.Parallel()
		_, err := renderAuthored(linesSpec, []byte(strings.Replace(linesFixture, `"include"`, `"included"`, 1)), includes)
		require.ErrorIs(t, err, errInclude)
	})
}

func Test_linesIncludes(t *testing.T) {
	t.Parallel()

	t.Run("env を持たないデプロイブランチを宣言の順に並べ、各線を続ける", func(t *testing.T) {
		t.Parallel()
		b := lineBranches(
			branchdecl.DeployBranch{Branch: "admin", Env: "admin"},
			branchdecl.DeployBranch{Branch: "production"},
			branchdecl.DeployBranch{Branch: "develop"},
		)
		assert.Equal(t, []string{"refs/heads/production", "refs/heads/develop", "refs/heads/release/**/*", "refs/heads/hotfix/**/*"}, linesIncludes(b))
	})

	t.Run("env を持たないデプロイブランチが無ければ各線だけ", func(t *testing.T) {
		t.Parallel()
		b := lineBranches(branchdecl.DeployBranch{Branch: "admin", Env: "admin"})
		assert.Equal(t, []string{"refs/heads/release/**/*", "refs/heads/hotfix/**/*"}, linesIncludes(b))
	})
}

func Test_deployRulesets(t *testing.T) {
	t.Parallel()

	checks, err := checksdecl.Parse([]byte(checksFixture))
	require.NoError(t, err)

	t.Run("env を持つデプロイブランチだけを宣言の順に生成し、今の内容と並べる", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		writeFile(t, root, rulesetsDir+"/deploy-b.json", "old")
		b := lineBranches(
			branchdecl.DeployBranch{Branch: "b", Env: "y"},
			branchdecl.DeployBranch{Branch: "production"},
			branchdecl.DeployBranch{Branch: "main", Env: "x"},
		)
		got, err := deployRulesets(root, b, checks)
		require.NoError(t, err)
		require.Len(t, got, 2)
		assert.Equal(t, rulesetSpec{path: rulesetsDir + "/deploy-b.json", name: "deploy-b", role: roleDeploy}, got[0].spec)
		assert.Equal(t, "old", string(got[0].current))
		assert.Contains(t, string(got[0].rendered), `"refs/heads/b"`)
		assert.NotContains(t, string(got[0].rendered), "plan-x")
		assert.Equal(t, deploySpec, got[1].spec)
		assert.Nil(t, got[1].current)
		assert.Equal(t, deployMainFixture, string(got[1].rendered))
	})

	t.Run("env を持つデプロイブランチが無ければ0件", func(t *testing.T) {
		t.Parallel()
		noPlan, err := checksdecl.Parse([]byte(`{"checks": [{"name": "lint", "workflow": "lint.yaml", "paths": [], "scope": "tree", "required": true}]}`))
		require.NoError(t, err)
		got, err := deployRulesets(t.TempDir(), lineBranches(branchdecl.DeployBranch{Branch: "production"}), noPlan)
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("plan_env の env にデプロイブランチが無ければ失敗する", func(t *testing.T) {
		t.Parallel()
		_, err := deployRulesets(t.TempDir(), lineBranches(branchdecl.DeployBranch{Branch: "b", Env: "y"}), checks)
		require.ErrorIs(t, err, errDeployRuleset)
		assert.ErrorContains(t, err, `plan-x の plan_env "x"`)
	})

	t.Run("今のファイルを読めなければ失敗する", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, deployMainFile), 0o750))
		_, err := deployRulesets(root, lineBranches(branchdecl.DeployBranch{Branch: "main", Env: "x"}), checks)
		require.Error(t, err)
		assert.NotErrorIs(t, err, os.ErrNotExist)
	})
}

func Test_deployContexts(t *testing.T) {
	t.Parallel()

	checks, err := checksdecl.Parse([]byte(`{"checks": [
  {"name": "plan-x-a", "workflow": "a.yaml", "paths": [], "scope": "tree", "required": true, "plan_env": "x", "contexts": ["a1", "a2"]},
  {"name": "plan-y", "workflow": "y.yaml", "paths": [], "scope": "tree", "required": true, "plan_env": "y", "contexts": ["y1"]},
  {"name": "plan-x-b", "workflow": "b.yaml", "paths": [], "scope": "tree", "required": true, "plan_env": "x", "contexts": ["b1"]}
]}`))
	require.NoError(t, err)

	t.Run("決定31 の検査、その env の plan を宣言の順、線の検証の順に並べる", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, []string{contextDeploySource, "a1", "a2", "b1", contextLineGate}, deployContexts("x", checks))
	})

	t.Run("plan の無い env は決定31 の検査と線の検証だけ", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, []string{contextDeploySource, contextLineGate}, deployContexts("z", checks))
	})
}

func Test_renderDeployRuleset(t *testing.T) {
	t.Parallel()

	t.Run("2つの空白で字下げし、末尾に改行を置いた形で生成する", func(t *testing.T) {
		t.Parallel()
		got, err := renderDeployRuleset("deploy-main", "main", []string{contextDeploySource, "plan-x-identity", "plan-x-workload", contextLineGate})
		require.NoError(t, err)
		assert.Equal(t, deployMainFixture, string(got))
	})

	t.Run("同じ入力からは同じバイト列を生成する", func(t *testing.T) {
		t.Parallel()
		a, err := renderDeployRuleset("deploy-a", "a", []string{"c"})
		require.NoError(t, err)
		b, err := renderDeployRuleset("deploy-a", "a", []string{"c"})
		require.NoError(t, err)
		assert.Equal(t, a, b)
	})

	t.Run("context の文字を HTML のために書き換えない", func(t *testing.T) {
		t.Parallel()
		got, err := renderDeployRuleset("deploy-a", "a", []string{"a<b>&c"})
		require.NoError(t, err)
		assert.Contains(t, string(got), `"a<b>&c"`)
	})
}

func Test_checkStaleRulesets(t *testing.T) {
	t.Parallel()

	decls := []rulesetDecl{{spec: commonSpec}, {spec: linesSpec}, {spec: deploySpec}}

	t.Run("宣言のファイルだけなら通り、.json でないファイルは見ない", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		for _, p := range []string{commonRulesetFile, linesRulesetFile, rulesetsDir + "/README.md"} {
			writeFile(t, root, p, "{}")
		}
		require.NoError(t, checkStaleRulesets(root, decls))
	})

	t.Run("宣言に無い .json を名指して失敗する", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		writeFile(t, root, commonRulesetFile, "{}")
		writeFile(t, root, rulesetsDir+"/deploy-old.json", "{}")
		writeFile(t, root, rulesetsDir+"/other.json", "{}")
		err := checkStaleRulesets(root, decls)
		require.ErrorIs(t, err, errStaleRuleset)
		assert.ErrorContains(t, err, rulesetsDir+"/deploy-old.json, "+rulesetsDir+"/other.json")
	})

	t.Run("rulesets/ が無ければ失敗する", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, checkStaleRulesets(t.TempDir(), decls), os.ErrNotExist)
	})
}

func Test_checkCoverage(t *testing.T) {
	t.Parallel()

	protected := []string{"refs/heads/main", "refs/heads/release/**/*", "refs/heads/hotfix/**/*"}
	decl := func(spec rulesetSpec, src string) rulesetDecl { return rulesetDecl{spec: spec, rendered: []byte(src)} }

	t.Run("common が保護対象の全集合を覆い、ほかがその内にあれば通る", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, checkCoverage([]rulesetDecl{decl(commonSpec, syncedCommon), decl(linesSpec, syncedLines), decl(deploySpec, deployMainFixture)}, protected))
	})

	t.Run("common の include の順は問わない", func(t *testing.T) {
		t.Parallel()
		reordered := []string{"refs/heads/hotfix/**/*", "refs/heads/main", "refs/heads/release/**/*"}
		require.NoError(t, checkCoverage([]rulesetDecl{decl(commonSpec, syncedCommon)}, reordered))
	})

	t.Run("common が保護対象を欠けば失敗する", func(t *testing.T) {
		t.Parallel()
		err := checkCoverage([]rulesetDecl{decl(commonSpec, syncedCommon)}, append(append([]string{}, protected...), "refs/heads/x"))
		require.ErrorIs(t, err, errRulesetPolicy)
		assert.ErrorContains(t, err, "保護対象の全集合と一致しません")
	})

	t.Run("common が保護対象の外を含めば失敗する", func(t *testing.T) {
		t.Parallel()
		err := checkCoverage([]rulesetDecl{decl(commonSpec, syncedCommon)}, protected[:2])
		require.ErrorIs(t, err, errRulesetPolicy)
	})

	t.Run("ほかの ruleset が common の外を対象にすれば失敗する", func(t *testing.T) {
		t.Parallel()
		other := strings.Replace(deployMainFixture, `"refs/heads/main"`, `"refs/heads/other"`, 1)
		err := checkCoverage([]rulesetDecl{decl(commonSpec, syncedCommon), decl(deploySpec, other)}, protected)
		require.ErrorIs(t, err, errRulesetPolicy)
		assert.ErrorContains(t, err, deployMainFile+" の include refs/heads/other")
	})

	t.Run("common が無ければ失敗する", func(t *testing.T) {
		t.Parallel()
		err := checkCoverage([]rulesetDecl{decl(linesSpec, syncedLines)}, protected)
		require.ErrorIs(t, err, errRulesetPolicy)
		assert.ErrorContains(t, err, "common の ruleset がありません")
	})

	t.Run("ruleset が0件なら失敗する", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, checkCoverage(nil, protected), errRulesetPolicy)
	})

	t.Run("生成した内容を読めなければ失敗する", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, checkCoverage([]rulesetDecl{decl(commonSpec, `x`)}, protected), errInclude)
	})
}

func Test_decodeInclude(t *testing.T) {
	t.Parallel()

	t.Run("conditions.ref_name.include を返す", func(t *testing.T) {
		t.Parallel()
		got, err := decodeInclude([]byte(syncedLines))
		require.NoError(t, err)
		assert.Equal(t, []string{"refs/heads/release/**/*", "refs/heads/hotfix/**/*"}, got)
	})

	t.Run("JSON でなければ失敗する", func(t *testing.T) {
		t.Parallel()
		_, err := decodeInclude([]byte(`x`))
		require.ErrorIs(t, err, errInclude)
	})
}

func Test_sortedCopy(t *testing.T) {
	t.Parallel()

	t.Run("並べ替えた写しを返し、元を変えない", func(t *testing.T) {
		t.Parallel()
		in := []string{"b", "a"}
		assert.Equal(t, []string{"a", "b"}, sortedCopy(in))
		assert.Equal(t, []string{"b", "a"}, in)
	})
}

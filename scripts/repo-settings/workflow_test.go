package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/branchdecl"
)

// editorconfig-checker-disable -- raw string の fixture は埋め込んだ YAML の字下げを持つ。撤回: fixture を外部ファイルへ移したとき

const unlockWorkflowFixture = `name: Unlock

on:
  workflow_dispatch:

jobs:
  unlock:
    environment: x-unlock-identity
    steps:
      - run: make ci-unlock
`

// editorconfig-checker-enable

var deployX = branches{
	Deploy: []branchdecl.DeployBranch{{Branch: "main", Env: "x", From: []string{"release/"}}},
	Lines:  []string{"release/"},
	Revert: "revert/",
}

func Test_renderApplyTriggers(t *testing.T) {
	t.Parallel()

	repo := func(t *testing.T, files map[string]string) string {
		t.Helper()
		root := t.TempDir()
		for name, body := range files {
			writeFile(t, root, filepath.Join(workflowsDir, name), body)
		}
		return root
	}
	drifted := strings.Replace(applyWorkflowFixture, "      - main", "      - 'release/*'\n      - hotfix/x", 1)

	t.Run("apply の environment を使う workflow だけを、その env のデプロイブランチへ生成する", func(t *testing.T) {
		t.Parallel()
		root := repo(t, map[string]string{"apply.yaml": drifted, "unlock.yml": unlockWorkflowFixture, "notes.txt": "x"})
		edits, err := renderApplyTriggers(root, deployX)
		require.NoError(t, err)
		require.Len(t, edits, 1)
		assert.Equal(t, filepath.Join(workflowsDir, "apply.yaml"), edits[0].path)
		assert.Equal(t, drifted, edits[0].current)
		assert.Equal(t, applyWorkflowFixture, edits[0].rendered)
	})
	t.Run("生成したものはもう一度生成しても変わらない", func(t *testing.T) {
		t.Parallel()
		edits, err := renderApplyTriggers(repo(t, map[string]string{"apply.yaml": applyWorkflowFixture}), deployX)
		require.NoError(t, err)
		require.Len(t, edits, 1)
		assert.Equal(t, edits[0].current, edits[0].rendered)
	})
	t.Run("引用符で囲んだ environment も読む", func(t *testing.T) {
		t.Parallel()
		quoted := strings.Replace(drifted, "environment: x-apply-identity", `environment: "x-apply" # comment`, 1)
		edits, err := renderApplyTriggers(repo(t, map[string]string{"apply.yaml": quoted}), deployX)
		require.NoError(t, err)
		require.Len(t, edits, 1)
		assert.Contains(t, edits[0].rendered, "    branches:\n      - main\n\n")
	})

	cases := map[string]struct {
		files map[string]string
		b     branches
		want  string
	}{
		"apply の workflow が1件も無ければ失敗する":          {map[string]string{"unlock.yaml": unlockWorkflowFixture}, deployX, "apply の environment を使う job がありません"},
		"1つの workflow が複数の env の apply を持てば失敗する": {map[string]string{"apply.yaml": applyWorkflowFixture + "  other:\n    environment: y-apply\n"}, deployX, "複数の env"},
		"env のデプロイブランチが無ければ失敗する":                 {map[string]string{"apply.yaml": applyWorkflowFixture}, branches{Deploy: deployBranches("main"), Lines: []string{"release/"}, Revert: "revert/"}, "env x のデプロイブランチ"},
		"on.push.branches がフロー形式なら失敗する":          {map[string]string{"apply.yaml": strings.Replace(applyWorkflowFixture, "    branches:\n      - main", "    branches: [main]", 1)}, deployX, "on.push.branches"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := renderApplyTriggers(repo(t, tc.files), tc.b)
			require.ErrorIs(t, err, errTrigger)
			assert.Contains(t, err.Error(), tc.want)
		})
	}

	t.Run("workflow のディレクトリが無ければ失敗する", func(t *testing.T) {
		t.Parallel()
		_, err := renderApplyTriggers(t.TempDir(), deployX)
		require.ErrorIs(t, err, os.ErrNotExist)
	})
}

func Test_applyEnvs(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		src  string
		want []string
	}{
		"apply-identity の env を返す":            {applyWorkflowFixture, []string{"x"}},
		"lock の解除と plan の env は返さない":          {unlockWorkflowFixture + "  plan:\n    environment: x-plan\n", nil},
		"同じ env の apply と apply-identity は1つ": {applyWorkflowFixture + "  workload:\n    environment: x-apply\n", []string{"x"}},
		"jobs の外の environment は読まない":          {"env:\n    environment: x-apply\njobs:\n  a:\n    runs-on: x\n", nil},
		"接尾辞だけの名前は env を持たない":                 {"jobs:\n  a:\n    environment: -apply\n", nil},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, applyEnvs(tc.src))
		})
	}
}

func Test_renderPushBranches(t *testing.T) {
	t.Parallel()

	got, err := renderPushBranches("on:\n  push:\n    branches:\n      - a\n      - b\n  workflow_dispatch:\n", "admin")
	require.NoError(t, err)
	assert.Equal(t, "on:\n  push:\n    branches:\n      - admin\n  workflow_dispatch:\n", got)

	_, err = renderPushBranches("on:\n  workflow_dispatch:\n", "admin")
	require.ErrorIs(t, err, errTrigger)
}

func Test_pushBranchesItems(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		src        string
		start, end int
		wantErr    string
	}{
		"一覧の要素の範囲を返す":             {"on:\n  push:\n    branches:\n      - a\n      - b\n", 3, 5, ""},
		"コメントと空行を挟んでも辿る":          {"on:\n  # c\n\n  push:\n    # c\n    branches:\n      - a\n", 6, 7, ""},
		"引用符で囲んだ on も読む":          {"'on':\n  push:\n    branches:\n      - a\n", 3, 4, ""},
		"on が無ければ失敗する":            {"name: x\n", 0, 0, "on: が"},
		"push が無ければ失敗する":          {"on:\n  workflow_dispatch:\n", 0, 0, "on.push が"},
		"push の外の branches は読まない": {"on:\n  push:\n  pull_request:\n    branches:\n      - a\n", 0, 0, "on.push.branches が"},
		"要素が無ければ失敗する":             {"on:\n  push:\n    branches:\n    paths:\n      - x\n", 0, 0, "要素がありません"},
		"フロー形式の on は読まない":         {"on: [push]\n", 0, 0, "on: が"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			start, end, err := pushBranchesItems(strings.Split(tc.src, "\n"))
			if tc.wantErr != "" {
				require.ErrorIs(t, err, errTrigger)
				assert.Contains(t, err.Error(), tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, [2]int{tc.start, tc.end}, [2]int{start, end})
		})
	}
}

func Test_nextMatch(t *testing.T) {
	t.Parallel()

	lines := []string{"on:", "  # c", "", "  pull_request:", "  push:", "jobs:", "  push:"}
	assert.Equal(t, 4, nextMatch(lines, 1, 2, pushKey))
	assert.Equal(t, -1, nextMatch(lines, 5, 2, pushKey), "浅い行に当たれば打ち切る")
	assert.Equal(t, -1, nextMatch(lines, len(lines), 2, pushKey))
}

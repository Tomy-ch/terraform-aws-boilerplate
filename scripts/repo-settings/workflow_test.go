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
	Lines:  branchdecl.Lines{Release: "release/"},
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

	t.Run(".yml の apply の workflow も生成する", func(t *testing.T) {
		t.Parallel()
		edits, err := renderApplyTriggers(repo(t, map[string]string{"apply.yml": drifted}), deployX)
		require.NoError(t, err)
		require.Len(t, edits, 1)
		assert.Equal(t, filepath.Join(workflowsDir, "apply.yml"), edits[0].path)
		assert.Equal(t, applyWorkflowFixture, edits[0].rendered)
	})
	t.Run("サブディレクトリは workflow として読まない", func(t *testing.T) {
		t.Parallel()
		edits, err := renderApplyTriggers(repo(t, map[string]string{"apply.yaml": drifted, "sub.yaml/apply.yaml": drifted}), deployX)
		require.NoError(t, err)
		require.Len(t, edits, 1)
		assert.Equal(t, filepath.Join(workflowsDir, "apply.yaml"), edits[0].path)
	})
	// 読めない workflow を飛ばすと、その workflow の起動条件が宣言から外れたまま緑になる。
	t.Run("読めない workflow があれば失敗する", func(t *testing.T) {
		t.Parallel()
		root := repo(t, map[string]string{"apply.yaml": applyWorkflowFixture})
		require.NoError(t, os.Symlink(filepath.Join(root, "none"), filepath.Join(root, workflowsDir, "x.yaml")))
		edits, err := renderApplyTriggers(root, deployX)
		require.ErrorIs(t, err, os.ErrNotExist)
		assert.Contains(t, err.Error(), "x.yaml")
		assert.Nil(t, edits)
	})

	cases := map[string]struct {
		files map[string]string
		b     branches
		file  string
		want  string
	}{
		"apply の workflow が1件も無ければ失敗する":          {map[string]string{"unlock.yaml": unlockWorkflowFixture}, deployX, workflowsDir, "apply の environment を使う job がありません"},
		"1つの workflow が複数の env の apply を持てば失敗する": {map[string]string{"apply.yaml": applyWorkflowFixture + "  other:\n    environment: y-apply\n"}, deployX, filepath.Join(workflowsDir, "apply.yaml"), "複数の env"},
		"env のデプロイブランチが無ければ失敗する":                 {map[string]string{"apply.yaml": applyWorkflowFixture}, branches{Deploy: deployBranches("main"), Lines: branchdecl.Lines{Release: "release/"}, Revert: "revert/"}, filepath.Join(workflowsDir, "apply.yaml"), "env x のデプロイブランチ"},
		"apply の job の environment: を読めなければ失敗する": {map[string]string{"apply.yaml": applyWorkflowFixture, "other.yaml": "on:\n  push:\n    branches:\n      - 'release/*'\njobs:\n  a:\n    environment:\n      name: x-apply\n"}, deployX, filepath.Join(workflowsDir, "other.yaml"), "environment: を読めません"},
		"on.push.branches がフロー形式なら失敗する":          {map[string]string{"apply.yaml": strings.Replace(applyWorkflowFixture, "    branches:\n      - main", "    branches: [main]", 1)}, deployX, filepath.Join(workflowsDir, "apply.yaml"), "on.push.branches"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := renderApplyTriggers(repo(t, tc.files), tc.b)
			require.ErrorIs(t, err, errTrigger)
			assert.Contains(t, err.Error(), tc.file)
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
		"apply-identity の env を返す":                        {applyWorkflowFixture, []string{"x"}},
		"lock の解除と plan の env は返さない":                      {unlockWorkflowFixture + "  plan:\n    environment: x-plan\n", nil},
		"同じ env の apply と apply-identity は1つ":             {applyWorkflowFixture + "  workload:\n    environment: x-apply\n", []string{"x"}},
		"jobs の外の environment は読まない":                      {"env:\n    environment: x-apply\njobs:\n  a:\n    runs-on: x\n", nil},
		"接尾辞だけの名前は env を持たない":                             {"jobs:\n  a:\n    environment: -apply\n", nil},
		"一重引用符で囲んだ environment も読む":                       {"jobs:\n  a:\n    environment: 'x-apply'\n", []string{"x"}},
		"step の入力の environment: は job の environment ではない": {"jobs:\n  a:\n    environment: x-apply\n    steps:\n      - uses: a/b@v1\n        with:\n          environment: prod\n  b:\n    steps:\n      with:\n        environment: prod\n", []string{"x"}},
		"steps と同じ字下げの要素の中の environment: は読まない":           {"jobs:\n  a:\n    steps:\n    - uses: a/b@v1\n      with:\n        environment: prod\n    environment: x-apply\n", []string{"x"}},
		"steps の中の空行の後の step の入力の environment: は読まない":     {"jobs:\n  a:\n    environment: x-apply\n    steps:\n      - uses: a/b@v1\n\n        # 入力\n        with:\n          environment: prod\n", []string{"x"}},
		"steps の後に書いた job の environment も読む":              {"jobs:\n  a:\n    steps:\n      - uses: a/b@v1\n        with:\n          environment: prod\n\n    environment: x-apply\n", []string{"x"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := applyEnvs(tc.src)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}

	// 読めない形を対象外と読むと、その workflow の起動条件が宣言から外れる。
	unreadable := map[string]string{
		"mapping の形":           "jobs:\n  a:\n    environment:\n      name: x-apply\n",
		"式":                    "jobs:\n  a:\n    environment: ${{ inputs.env }}\n",
		"アンカー":                 "jobs:\n  a:\n    environment: &e x-apply\n",
		"plan の mapping":       "jobs:\n  a:\n    environment:\n      name: x-plan\n",
		"別の字下げ":                "jobs:\n  a:\n      runs-on: x\n      environment: x-apply\n",
		"引用符付きのキー":             "jobs:\n  a:\n    \"environment\": x-apply\n",
		"コロンの前の空白":             "jobs:\n  a:\n    environment : x-apply\n",
		"タブで字下げした environment": "jobs:\n  a:\n\tenvironment: x-apply\n",
		"steps の後の job のキー":    "jobs:\n  a:\n    steps:\n      - run: x\n    environment:\n      name: x-apply\n",
	}
	for name, src := range unreadable {
		t.Run("読めない environment: は失敗する（"+name+"）", func(t *testing.T) {
			t.Parallel()
			_, err := applyEnvs(src)
			require.ErrorIs(t, err, errTrigger)
		})
	}
}

func Test_renderPushBranches(t *testing.T) {
	t.Parallel()

	t.Run("一覧を branch 1つへ置き換える", func(t *testing.T) {
		t.Parallel()
		got, err := renderPushBranches("on:\n  push:\n    branches:\n      - a\n      - b\n  workflow_dispatch:\n", "admin")
		require.NoError(t, err)
		assert.Equal(t, "on:\n  push:\n    branches:\n      - admin\n  workflow_dispatch:\n", got)
	})
	t.Run("on.push が無ければ失敗する", func(t *testing.T) {
		t.Parallel()
		_, err := renderPushBranches("on:\n  workflow_dispatch:\n", "admin")
		require.ErrorIs(t, err, errTrigger)
	})
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
		// 要素の間の空行とコメントで打ち切ると、その後ろの要素が生成からも検査からも漏れる。
		"要素の間のコメントを越えて最後の要素まで返す": {"on:\n  push:\n    branches:\n      - a\n      # c\n      - b\n", 3, 6, ""},
		"要素の間の空行を越えて最後の要素まで返す":   {"on:\n  push:\n    branches:\n      - a\n\n      - b\n", 3, 6, ""},
		"一覧の後ろの空行は範囲に含めない":       {"on:\n  push:\n    branches:\n      - a\n\njobs:\n", 3, 4, ""},
		"一覧の中の要素でない行は失敗する":       {"on:\n  push:\n    branches:\n      - a\n      b: c\n", 0, 0, "要素ではありません"},
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
	cases := map[string]struct {
		from int
		want int
	}{
		"コメントと空行と別のキーを越えて当たる": {1, 4},
		"浅い行に当たれば打ち切る":        {5, -1},
		"末尾から始めれば見つからない":      {len(lines), -1},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, nextMatch(lines, tc.from, 2, pushKey))
		})
	}
}

func Test_isApplySuffix(t *testing.T) {
	t.Parallel()

	want := map[string]bool{"-apply": true, "-apply-identity": true, planSuffix: false, unlockIdentitySuffix: false}
	for suffix, w := range want {
		t.Run(suffix, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, w, isApplySuffix(suffix))
		})
	}
	t.Run("宣言の接尾辞はどれも期待値を持つ", func(t *testing.T) {
		t.Parallel()
		require.NotEmpty(t, identitySuffixes)
		for _, suffix := range identitySuffixes {
			assert.Contains(t, want, suffix)
		}
	})
}

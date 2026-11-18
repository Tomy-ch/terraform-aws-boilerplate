package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/branchdecl"
)

// setDecl は、デプロイブランチ2本と線2本を持つ宣言です。
var setDecl = branches{
	Deploy: []branchdecl.DeployBranch{{Branch: "admin", Env: "admin", From: []string{"rel/"}}, {Branch: "prod", ReleaseBase: true}},
	Lines:  branchdecl.Lines{Release: "rel/", Hotfix: "hot/"},
	Revert: "revert/",
}

// editorconfig-checker-disable -- raw string の fixture は埋め込んだ YAML の字下げを持つ。撤回: fixture を外部ファイルへ移したとき

const scanWorkflow = `name: Scan

on:
  push:
    branches: # branch-set: ruleset
      - 'release/*'
      - production
    paths:
      - 'x/**'
  pull_request:

jobs:
  gate:
    if: ${{ github.event_name != 'pull_request' || contains(fromJSON('["old"]'), github.base_ref) }} # branch-set: deploy
    runs-on: ubuntu-latest
    steps:
      - run: echo '# not a comment'
`

const scanWorkflowRendered = `name: Scan

on:
  push:
    branches: # branch-set: ruleset
      - admin
      - prod
      - 'rel/**'
      - 'hot/**'
    paths:
      - 'x/**'
  pull_request:

jobs:
  gate:
    if: ${{ github.event_name != 'pull_request' || contains(fromJSON('["admin", "prod"]'), github.base_ref) }} # branch-set: deploy
    runs-on: ubuntu-latest
    steps:
      - run: echo '# not a comment'
`

// editorconfig-checker-enable

func Test_renderBranchSets(t *testing.T) {
	t.Parallel()

	repo := func(t *testing.T, files map[string]string) string {
		t.Helper()
		root := t.TempDir()
		for name, body := range files {
			writeFile(t, root, filepath.Join(workflowsDir, name), body)
		}
		return root
	}

	t.Run("印を持つ workflow だけを生成し、印を数える", func(t *testing.T) {
		t.Parallel()
		root := repo(t, map[string]string{"scan.yaml": scanWorkflow, "plain.yml": "on:\n  pull_request:\n", "notes.txt": "branches:\n"})
		edits, marks, err := renderBranchSets(root, setDecl, nil)
		require.NoError(t, err)
		require.Len(t, edits, 1)
		assert.Equal(t, 2, marks)
		assert.Equal(t, filepath.Join(workflowsDir, "scan.yaml"), edits[0].path)
		assert.Equal(t, scanWorkflow, edits[0].current)
		assert.Equal(t, scanWorkflowRendered, edits[0].rendered)
	})
	t.Run("生成したものはもう一度生成しても変わらない", func(t *testing.T) {
		t.Parallel()
		edits, _, err := renderBranchSets(repo(t, map[string]string{"scan.yaml": scanWorkflowRendered}), setDecl, nil)
		require.NoError(t, err)
		require.Len(t, edits, 1)
		assert.Equal(t, edits[0].current, edits[0].rendered)
	})
	t.Run("apply の workflow の on.push.branches は印なしで通す", func(t *testing.T) {
		t.Parallel()
		root := repo(t, map[string]string{"apply.yaml": applyWorkflowFixture})
		edits, marks, err := renderBranchSets(root, setDecl, []string{filepath.Join(workflowsDir, "apply.yaml")})
		require.NoError(t, err)
		assert.Empty(t, edits)
		assert.Zero(t, marks)
	})
	t.Run("apply の workflow でなければ印の無い一覧で失敗する", func(t *testing.T) {
		t.Parallel()
		_, _, err := renderBranchSets(repo(t, map[string]string{"apply.yaml": applyWorkflowFixture}), setDecl, nil)
		require.ErrorIs(t, err, errBranchSet)
		assert.ErrorContains(t, err, filepath.Join(workflowsDir, "apply.yaml"))
	})
	t.Run("workflow の置き場が無ければ失敗する", func(t *testing.T) {
		t.Parallel()
		_, _, err := renderBranchSets(t.TempDir(), setDecl, nil)
		require.Error(t, err)
	})
}

func Test_renderSetLines(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		src     string
		isApply bool
		wantErr string
	}{
		"印の無い branches は拒む":            {"on:\n  push:\n    branches:\n      - main\n", false, "印 `# branch-set: <集合>` の無い branches"},
		"印の無い branches-ignore も拒む":     {"on:\n  push:\n    branches-ignore:\n      - main\n", false, "印 `# branch-set: <集合>` の無い branches"},
		"フロー形式の branches は拒む":          {"on:\n  push:\n    branches: [main] # branch-set: deploy\n", false, "ブロック形式"},
		"引用符のキーも branches として読む":       {"on:\n  push:\n    \"branches\":\n      - main\n", false, "印 `# branch-set: <集合>` の無い branches"},
		"読めない書き方の branches は数の食い違いで拒む": {"on:\n  push: {branches: [main]}\n", false, "字面では 1 件"},
		"要素の無い branches は拒む":           {"on:\n  push:\n    branches: # branch-set: deploy\n  pull_request:\n", false, "要素がありません"},
		"要素でない行を持つ branches は拒む":       {"on:\n  push:\n    branches: # branch-set: deploy\n      - a\n      b\n", false, "要素ではありません"},
		"未知の集合は拒む":                     {"on:\n  push:\n    branches: # branch-set: all\n      - a\n", false, "未知の集合"},
		"branches と if: 以外の印は拒む":       {"jobs:\n  a:\n    runs-on: x # branch-set: deploy\n", false, "branches か if: の行にだけ"},
		"apply の workflow の印は拒む":       {applyWorkflowFixture + "    if: ${{ contains(fromJSON('[\"a\"]'), github.base_ref) }} # branch-set: deploy\n", true, "apply の workflow には印を置けません"},
		"apply の workflow のほかの一覧は拒む":   {strings.Replace(applyWorkflowFixture, "on:\n", "on:\n  pull_request:\n    branches:\n      - x\n", 1), true, "印 `# branch-set: <集合>` の無い branches"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, _, err := renderSetLines(tc.src, setDecl, tc.isApply)
			require.ErrorIs(t, err, errBranchSet)
			assert.ErrorContains(t, err, tc.wantErr)
		})
	}

	t.Run("コメントの中の branches と印は読まない", func(t *testing.T) {
		t.Parallel()
		src := "# branches: # branch-set: deploy\non:\n  pull_request:\n"
		got, marks, err := renderSetLines(src, setDecl, false)
		require.NoError(t, err)
		assert.Zero(t, marks)
		assert.Equal(t, src, got)
	})
	t.Run("要素の間の空行とコメントは要素と一緒に置き換える", func(t *testing.T) {
		t.Parallel()
		src := "on:\n  push:\n    branches: # branch-set: deploy\n      - a\n\n      # c\n      - b\n    paths:\n      - x\n"
		got, marks, err := renderSetLines(src, setDecl, false)
		require.NoError(t, err)
		assert.Equal(t, 1, marks)
		assert.Equal(t, "on:\n  push:\n    branches: # branch-set: deploy\n      - admin\n      - prod\n    paths:\n      - x\n", got)
	})
}

func Test_renderBranchesItems(t *testing.T) {
	t.Parallel()

	lines := strings.Split("    branches: # branch-set: lines\n      - a\n    paths:", "\n")
	code, _ := splitComment(lines[0])
	items, next, err := renderBranchesItems(lines, 0, code, setDecl, setLines)
	require.NoError(t, err)
	assert.Equal(t, []string{"      - 'rel/**'", "      - 'hot/**'"}, items)
	assert.Equal(t, 2, next)
}

func Test_renderIfLine(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		code    string
		set     string
		want    string
		wantErr string
	}{
		"deploy は fromJSON の配列を書き換える": {
			`    if: ${{ github.event_name == 'push' || contains(fromJSON('["x"]'), github.base_ref) }}`, setDeploy,
			`    if: ${{ github.event_name == 'push' || contains(fromJSON('["admin", "prod"]'), github.base_ref) }}`, "",
		},
		"ref と無関係な fromJSON は書き換えない": {
			`    if: ${{ contains(fromJSON('["failure"]'), needs.a.result) || contains(fromJSON('["x"]'), github.base_ref) }}`, setDeploy,
			`    if: ${{ contains(fromJSON('["failure"]'), needs.a.result) || contains(fromJSON('["admin", "prod"]'), github.base_ref) }}`, "",
		},
		"lines は startsWith の群を書き換える": {
			`    if: ${{ github.event_name == 'pull_request' && (startsWith(github.head_ref, 'release/')) }}`, setLines,
			`    if: ${{ github.event_name == 'pull_request' && (startsWith(github.head_ref, 'rel/') || startsWith(github.head_ref, 'hot/')) }}`, "",
		},
		"deploy の項が無ければ拒む":      {`    if: ${{ github.base_ref == 'x' }}`, setDeploy, "", "ちょうど1つ"},
		"deploy の項が2つなら拒む":      {`    if: ${{ contains(fromJSON('["a"]'), github.base_ref) || contains(fromJSON('["b"]'), github.head_ref) }}`, setDeploy, "", "（2 件）"},
		"lines の群が無ければ拒む":       {`    if: ${{ startsWith(github.head_ref, 'release/') }}`, setLines, "", "ちょうど1つ"},
		"群の中で context が違えば拒む":   {`    if: ${{ (startsWith(github.head_ref, 'a/') || startsWith(github.base_ref, 'b/')) }}`, setLines, "", "別の context"},
		"if: に ruleset の印は置けない": {`    if: ${{ contains(fromJSON('["a"]'), github.base_ref) }}`, setRuleset, "", "if: の行に置ける印"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := renderIfLine(tc.code, setDecl, tc.set)
			if tc.wantErr != "" {
				require.ErrorIs(t, err, errBranchSet)
				assert.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}

	t.Run("デプロイブランチが無ければ拒む", func(t *testing.T) {
		t.Parallel()
		_, err := renderIfLine(`if: ${{ contains(fromJSON('["a"]'), github.base_ref) }}`, branches{Lines: setDecl.Lines}, setDeploy)
		require.ErrorIs(t, err, errBranchSet)
	})
}

func Test_prefixExpression(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "(startsWith(github.ref_name, 'a/') || startsWith(github.ref_name, 'b/'))", prefixExpression("github.ref_name", []string{"a/", "b/"}))
}

func Test_jsonArray(t *testing.T) {
	t.Parallel()

	got, err := jsonArray([]string{"a", "b"})
	require.NoError(t, err)
	assert.Equal(t, `["a", "b"]`, got)

	_, err = jsonArray(nil)
	require.ErrorIs(t, err, errBranchSet)
}

func Test_splitComment(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		line, code, comment string
	}{
		"行末のコメントを分ける":         {"    branches: # branch-set: deploy", "    branches:", " # branch-set: deploy"},
		"行全体のコメント":            {"# c", "", "# c"},
		"字下げしたコメント":           {"  # c", "", "  # c"},
		"コメントが無ければ全体がコード":     {"    - main", "    - main", ""},
		"単引用符の中の # はコメントでない":  {"run: echo '# x' # c", "run: echo '# x'", " # c"},
		"二重引用符の中の # はコメントでない": {`name: "a # b"`, `name: "a # b"`, ""},
		"空白の前置の無い # はコメントでない": {"url: a#b", "url: a#b", ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			code, comment := splitComment(tc.line)
			assert.Equal(t, tc.code, code)
			assert.Equal(t, tc.comment, comment)
		})
	}
}

func Test_triggerPatterns(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		set     string
		want    []string
		wantErr bool
	}{
		"ruleset はデプロイブランチと線": {setRuleset, []string{"admin", "prod", "rel/**", "hot/**"}, false},
		"deploy はデプロイブランチだけ":  {setDeploy, []string{"admin", "prod"}, false},
		"lines は線だけ":          {setLines, []string{"rel/**", "hot/**"}, false},
		"未知の集合は拒む":            {"protected", nil, true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := triggerPatterns(setDecl, tc.set)
			if tc.wantErr {
				require.ErrorIs(t, err, errBranchSet)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}

	t.Run("展開が0件なら拒む", func(t *testing.T) {
		t.Parallel()
		_, err := triggerPatterns(branches{Lines: setDecl.Lines}, setDeploy)
		require.ErrorIs(t, err, errBranchSet)
	})
}

func Test_lineGlobs(t *testing.T) {
	t.Parallel()
	assert.Equal(t, []string{"rel/**", "hot/**"}, lineGlobs(setDecl))
}

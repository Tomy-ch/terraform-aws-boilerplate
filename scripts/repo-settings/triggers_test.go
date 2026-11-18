package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/branchdecl"
)

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
      - 'admin'
      - 'prod'
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
		"印の付いた一覧の中のコメント行は拒む":           {"on:\n  push:\n    branches: # branch-set: deploy\n      - a\n      # c\n      - b\n", false, "コメントを置けません"},
		"印の付いた一覧の要素の行末コメントは拒む":         {"on:\n  push:\n    branches: # branch-set: deploy\n      - a # why\n", false, "コメントを置けません"},
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
	t.Run("要素の間の空行は要素と一緒に置き換える", func(t *testing.T) {
		t.Parallel()
		src := "on:\n  push:\n    branches: # branch-set: deploy\n      - a\n\n      - b\n    paths:\n      - x\n"
		got, marks, err := renderSetLines(src, setDecl, false)
		require.NoError(t, err)
		assert.Equal(t, 1, marks)
		assert.Equal(t, "on:\n  push:\n    branches: # branch-set: deploy\n      - 'admin'\n      - 'prod'\n    paths:\n      - x\n", got)
	})

}

func Test_renderBranchesItems(t *testing.T) {
	t.Parallel()

	lines := strings.Split("    branches: # branch-set: lines\n      - a\n    paths:", "\n")
	code, _ := splitComment(lines[0])
	items, next, err := renderBranchesItems(lines, 0, code, setDecl, setLines)
	require.NoError(t, err)
	assert.Equal(t, []string{"      - 'rel/**'", "      - 'hot/**'"}, items)

	t.Run("名前は YAML の文字列として引用する", func(t *testing.T) {
		t.Parallel()
		b := branches{Deploy: []branchdecl.DeployBranch{{Branch: "null"}, {Branch: "123"}}}
		lines := strings.Split("    branches: # branch-set: deploy\n      - a", "\n")
		code, _ := splitComment(lines[0])
		items, _, err := renderBranchesItems(lines, 0, code, b, setDeploy)
		require.NoError(t, err)
		assert.Equal(t, []string{"      - 'null'", "      - '123'"}, items)
	})
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
		"引用符の値の if: は拒む":        {`    if: "${{ contains(fromJSON('[\\"x\\"]'), github.base_ref) }}"`, setDeploy, "", "引用符で囲めません"},
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

	t.Run("env を持つデプロイブランチが複数でも、宣言の順にすべて並べる", func(t *testing.T) {
		t.Parallel()
		b := setDecl
		b.Deploy = append([]branchdecl.DeployBranch{{Branch: "security", Env: "security", From: []string{"rel/"}}}, setDecl.Deploy...)
		got, err := renderIfLine(`if: ${{ contains(fromJSON('["x"]'), github.base_ref) }}`, b, setDeploy)
		require.NoError(t, err)
		assert.Equal(t, `if: ${{ contains(fromJSON('["security", "admin", "prod"]'), github.base_ref) }}`, got)
	})
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

func Test_checkRefLiterals(t *testing.T) {
	t.Parallel()

	repo := func(t *testing.T, files map[string]string) string {
		t.Helper()
		root := t.TempDir()
		for name, body := range files {
			writeFile(t, root, name, body)
		}
		return root
	}
	wf := func(name string) string { return filepath.Join(workflowsDir, name) }

	t.Run("生成した項と ref でない比較は通し、読んだ式を数える", func(t *testing.T) {
		t.Parallel()
		root := repo(t, map[string]string{
			wf("scan.yaml"): scanWorkflowRendered,
			filepath.Join(actionsDir, "a", "action.yml"): "runs:\n  steps:\n    - run: echo ${{ inputs.x }}\n",
		})
		n, err := checkRefLiterals(root)
		require.NoError(t, err)
		assert.Equal(t, 2, n)
	})
	t.Run("composite action の比較も落とす", func(t *testing.T) {
		t.Parallel()
		root := repo(t, map[string]string{
			wf("scan.yaml"): scanWorkflowRendered,
			filepath.Join(actionsDir, "a", "action.yaml"): "runs:\n  steps:\n    - if: github.ref_name == 'production'\n      run: x\n",
		})
		_, err := checkRefLiterals(root)
		require.ErrorIs(t, err, errRefLiteral)
		assert.ErrorContains(t, err, filepath.Join(actionsDir, "a", "action.yaml")+":3")
	})
	t.Run("印の行でも生成した項の外の比較は落とす", func(t *testing.T) {
		t.Parallel()
		src := "jobs:\n  a:\n    if: ${{ contains(fromJSON('[\"a\"]'), github.base_ref) && github.head_ref != 'release/x' }} # branch-set: deploy\n"
		_, err := checkRefLiterals(repo(t, map[string]string{wf("a.yaml"): src}))
		require.ErrorIs(t, err, errRefLiteral)
		assert.ErrorContains(t, err, "github.head_ref != '")
	})
	t.Run("印の無い行の fromJSON の集合は落とす", func(t *testing.T) {
		t.Parallel()
		src := "jobs:\n  a:\n    if: ${{ contains(fromJSON('[\"a\"]'), github.base_ref) }}\n"
		_, err := checkRefLiterals(repo(t, map[string]string{wf("a.yaml"): src}))
		require.ErrorIs(t, err, errRefLiteral)
	})
	t.Run("run の中の式も落とす", func(t *testing.T) {
		t.Parallel()
		src := "jobs:\n  a:\n    steps:\n      - run: |\n          echo ${{ github.event.pull_request.base.ref == 'production' }}\n"
		_, err := checkRefLiterals(repo(t, map[string]string{wf("a.yaml"): src}))
		require.ErrorIs(t, err, errRefLiteral)
	})
	t.Run("大文字小文字の違う context の比較も落とす", func(t *testing.T) {
		t.Parallel()
		src := "jobs:\n  a:\n    if: ${{ GitHub.Base_Ref == 'main' }}\n"
		_, err := checkRefLiterals(repo(t, map[string]string{wf("a.yaml"): src}))
		require.ErrorIs(t, err, errRefLiteral)
	})
	t.Run("折り返した if: の続きの行の比較も落とす", func(t *testing.T) {
		t.Parallel()
		src := "jobs:\n  a:\n    if: github.event_name == 'push' &&\n      github.ref_name == 'main'\n"
		_, err := checkRefLiterals(repo(t, map[string]string{wf("a.yaml"): src}))
		require.ErrorIs(t, err, errRefLiteral)
	})
	t.Run("github を添字で引く式は読めないとして失敗する", func(t *testing.T) {
		t.Parallel()
		src := "jobs:\n  a:\n    if: ${{ github['ref_name'] == 'main' }}\n"
		_, err := checkRefLiterals(repo(t, map[string]string{wf("a.yaml"): src}))
		require.ErrorIs(t, err, errExpression)
	})
	t.Run("対象のファイルが無ければ失敗する", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		writeFile(t, root, filepath.Join(workflowsDir, "notes.txt"), "x")
		_, err := checkRefLiterals(root)
		require.ErrorIs(t, err, errExpression)
	})
	t.Run("式が1件も無ければ失敗する", func(t *testing.T) {
		t.Parallel()
		_, err := checkRefLiterals(repo(t, map[string]string{wf("a.yaml"): "on:\n  pull_request:\n"}))
		require.ErrorIs(t, err, errExpression)
	})
	t.Run("workflow の置き場が無ければ失敗する", func(t *testing.T) {
		t.Parallel()
		_, err := checkRefLiterals(t.TempDir())
		require.Error(t, err)
	})
}

func Test_expressionFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for _, name := range []string{
		filepath.Join(workflowsDir, "a.yaml"), filepath.Join(workflowsDir, "b.yml"), filepath.Join(workflowsDir, "README.md"),
		filepath.Join(actionsDir, "x", "action.yml"), filepath.Join(actionsDir, "y", "z", "action.yaml"), filepath.Join(actionsDir, "x", "other.yaml"),
	} {
		writeFile(t, root, name, "x")
	}
	got, err := expressionFiles(root)
	require.NoError(t, err)
	rels := make([]string, 0, len(got))
	for _, p := range got {
		rel, _ := filepath.Rel(root, p)
		rels = append(rels, rel)
	}
	assert.ElementsMatch(t, []string{
		filepath.Join(workflowsDir, "a.yaml"), filepath.Join(workflowsDir, "b.yml"),
		filepath.Join(actionsDir, "x", "action.yml"), filepath.Join(actionsDir, "y", "z", "action.yaml"),
	}, rels)

	t.Run("actions の置き場が無くても workflow を返す", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		writeFile(t, root, filepath.Join(workflowsDir, "a.yaml"), "x")
		got, err := expressionFiles(root)
		require.NoError(t, err)
		assert.Len(t, got, 1)
	})
}

func Test_expressions(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		src     string
		want    []expression
		wantErr string
	}{
		"行の中の式を取り出す":                   {"a: ${{ x }}-${{ y }}\n", []expression{{1, "${{ x }}"}, {1, "${{ y }}"}}, ""},
		"if: の値は1つの式":                  {"    if: github.ref == 'a' && ${{ b }}\n", []expression{{1, "github.ref == 'a' && ${{ b }}"}}, ""},
		"step の if: も読む":               {"      - if: x\n", []expression{{1, "x"}}, ""},
		"ブロックスカラーの if: を読む":            {"    if: >-\n      a &&\n      ${{ b }}\n    run: x\n", []expression{{1, "      a &&\n      ${{ b }}"}}, ""},
		"折り返した plain の if: を1つの式として読む": {"    if: a &&\n      github.ref_name == 'main'\n    run: x\n", []expression{{1, "a && github.ref_name == 'main'"}}, ""},
		"折り返した二重引用符の if: を1つの式として読む":   {"    if: \"a &&\n      b\"\n    run: x\n", []expression{{1, "\"a && b\""}}, ""},
		"閉じていない引用符の if: は拒む":           {"    if: \"a &&\n      b\n    run: x\n", nil, "閉じていません"},
		"run のブロックスカラーの中の # の後ろの式も読む":  {"      - run: |\n          echo x # ${{ y }}\n          # ${{ z }}\n      - run: w\n", []expression{{2, "${{ y }}"}, {3, "${{ z }}"}}, ""},
		"コメントは読まない":                    {"# if: ${{ github.ref == 'a' }}\na: x # ${{ y }}\n", nil, ""},
		"印の if: は生成した項を除く": {
			"    if: ${{ contains(fromJSON('[\"a\"]'), github.base_ref) || x }} # branch-set: deploy\n",
			[]expression{{1, "${{ true || x }}"}}, "",
		},
		"閉じていない式は拒む":       {"a: ${{ x\n", nil, "閉じていない式"},
		"読めない書き方の if: は拒む": {"    steps: [{if: x}]\n", nil, "読めない書き方"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := expressions(tc.src)
			if tc.wantErr != "" {
				require.ErrorIs(t, err, errExpression)
				assert.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func Test_withoutGenerated(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "true && x", withoutGenerated("contains(fromJSON('[\"a\"]'), github.base_ref) && x", setDeploy))
	assert.Equal(t, "true && x", withoutGenerated("(startsWith(github.head_ref, 'a/')) && x", setLines))
	assert.Equal(t, "y", withoutGenerated("y", setRuleset))
}

func Test_refLiteralTerms(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		expr string
		want int
	}{
		"== の右のリテラル":                {"github.ref_name == 'production'", 1},
		"!= の左のリテラル":                {"'main' != github.base_ref", 1},
		"startsWith":                {"startsWith(github.head_ref, 'release/')", 1},
		"endsWith":                  {"endsWith(github.ref, '/main')", 1},
		"contains の文字列":             {"contains(github.event.ref, 'hotfix')", 1},
		"contains の fromJSON":       {"contains(fromJSON('[\"a\"]'), github.event.workflow_run.head_branch)", 1},
		"pull_request の head の ref": {"github.event.pull_request.head.ref == 'x'", 1},
		"context 同士の比較は通す":          {"github.ref_name == github.event.repository.default_branch", 0},
		"ref でない context は通す":       {"github.event_name == 'push' || github.ref_type == 'tag'", 0},
		"ref と無関係な fromJSON は通す":    {"contains(fromJSON('[\"failure\"]'), needs.a.result)", 0},
		"ref を値として渡すだけなら通す":         {"${{ github.ref }}", 0},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Len(t, refLiteralTerms(tc.expr), tc.want)
		})
	}
}

func Test_keyColumn(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		code string
		want int
	}{
		"字下げしたキー":        {"    if: x", 4},
		"列の要素のキーは - を含む": {"      - if: x", 8},
		"入れ子の列の要素":       {"  - - run: x", 6},
		"字下げの無いキー":       {"on:", 0},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, keyColumn(tc.code))
		})
	}
}

func Test_blockBody(t *testing.T) {
	t.Parallel()

	lines := strings.Split("    run: |\n      a\n\n      b\n\n    if: x", "\n")
	body, next := blockBody(lines, 1, 4)
	assert.Equal(t, []string{"      a", "", "      b"}, body)
	assert.Equal(t, 4, next)

	t.Run("本文が無ければ空を返す", func(t *testing.T) {
		t.Parallel()
		body, next := blockBody([]string{"    run: |", "    if: x"}, 1, 4)
		assert.Empty(t, body)
		assert.Equal(t, 1, next)
	})
}

func Test_foldedScalar(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		src     string
		want    string
		wantErr bool
	}{
		"続きの行を空白でつなぐ":      {"    if: a &&\n      b\n    run: x", "a && b", false},
		"続きの行のコメントは読まない":   {"    if: a &&\n      b # c\n    run: x", "a && b", false},
		"続きが無ければ1行目だけ":     {"    if: a\n    run: x", "a", false},
		"二重引用符の値は閉じるまでつなぐ": {"    if: \"a &&\n      b\"\n    run: x", "\"a && b\"", false},
		"閉じていない引用符は失敗する":   {"    if: 'a &&\n      b\n    run: x", "", true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			lines := strings.Split(tc.src, "\n")
			m := ifLine.FindStringSubmatch(lines[0])
			got, _, err := foldedScalar(lines, 0, strings.TrimSpace(m[2]), 4)
			if tc.wantErr {
				require.ErrorIs(t, err, errExpression)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

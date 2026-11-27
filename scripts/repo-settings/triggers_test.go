package main

import (
	"io/fs"
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
		require.ErrorIs(t, err, fs.ErrNotExist)
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
		"branches と同じ字下げの列は要素として読まない":  {"on:\n  push:\n    branches: # branch-set: deploy\n    - a\n", false, "要素がありません"},
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
		"二重引用符の値の if: は拒む":      {`    if: "${{ contains(fromJSON('[x]'), github.base_ref) }}"`, setDeploy, "", "引用符で囲めません"},
		"群の中で大文字小文字だけ違う同じ context は同じとみなす": {
			`    if: ${{ (startsWith(github.head_ref, 'a/') || startsWith(GITHUB.HEAD_REF, 'b/')) }}`, setLines,
			`    if: ${{ (startsWith(github.head_ref, 'rel/') || startsWith(github.head_ref, 'hot/')) }}`, "",
		},
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
	t.Run("線が無ければ lines の群を生成せずに拒む", func(t *testing.T) {
		t.Parallel()
		_, err := renderIfLine(`if: ${{ (startsWith(github.head_ref, 'a/')) }}`, branches{Deploy: setDecl.Deploy}, setLines)
		require.ErrorIs(t, err, errBranchSet)
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
		"行末のコメントを分ける":             {"    branches: # branch-set: deploy", "    branches:", " # branch-set: deploy"},
		"行全体のコメント":                {"# c", "", "# c"},
		"字下げしたコメント":               {"  # c", "", "  # c"},
		"コメントが無ければ全体がコード":         {"    - main", "    - main", ""},
		"単引用符の中の # はコメントでない":      {"run: echo '# x' # c", "run: echo '# x'", " # c"},
		"二重引用符の中の # はコメントでない":     {`name: "a # b"`, `name: "a # b"`, ""},
		"二重引用符の中の \\\" は引用符を閉じない": {`name: "a \" # b" # c`, `name: "a \" # b"`, " # c"},
		"空白の前置の無い # はコメントでない":     {"url: a#b", "url: a#b", ""},
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
		"ruleset はデプロイブランチと線":            {setRuleset, []string{"admin", "prod", "rel/**", "hot/**"}, false},
		"deploy はデプロイブランチだけ":             {setDeploy, []string{"admin", "prod"}, false},
		"lines は線だけ":                     {setLines, []string{"rel/**", "hot/**"}, false},
		"gate は env を持たないデプロイブランチと線":     {setGate, []string{"prod", "rel/**", "hot/**"}, false},
		"deploy-env は env を持つデプロイブランチだけ": {setDeployEnv, []string{"admin"}, false},
		"未知の集合は拒む":                       {"protected", nil, true},
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

	t.Run("env を持つデプロイブランチが無ければ deploy-env の展開は0件として拒む", func(t *testing.T) {
		t.Parallel()
		_, err := triggerPatterns(branches{Deploy: []branchdecl.DeployBranch{{Branch: "prod"}}, Lines: setDecl.Lines}, setDeployEnv)
		require.ErrorIs(t, err, errBranchSet)
	})

	// 線の ruleset の対象と gate の集合は、同じ分岐を指す（ADR-0603 決定16）。
	t.Run("gate の集合は線の ruleset の include と同じ分岐を指す", func(t *testing.T) {
		t.Parallel()
		got, err := triggerPatterns(setDecl, setGate)
		require.NoError(t, err)
		var want []string
		for _, inc := range linesIncludes(setDecl) {
			want = append(want, strings.TrimSuffix(strings.TrimPrefix(inc, "refs/heads/"), "/*"))
		}
		assert.Equal(t, want, got)
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
	t.Run("大文字小文字の違う関数名の比較も落とす", func(t *testing.T) {
		t.Parallel()
		for _, expr := range []string{
			"STARTSWITH(github.ref, 'x')",
			"CONTAINS('a,b', github.ref)",
			"contains(FROMJSON('[\\\"a\\\"]'), github.ref)",
		} {
			src := "jobs:\n  a:\n    if: ${{ " + expr + " }}\n"
			_, err := checkRefLiterals(repo(t, map[string]string{wf("a.yaml"): src}))
			require.ErrorIs(t, err, errRefLiteral, expr)
		}
	})
	t.Run("大文字の GITHUB を文字列リテラルの添字で引く比較も落とす", func(t *testing.T) {
		t.Parallel()
		src := "jobs:\n  a:\n    if: ${{ GITHUB['ref'] == 'main' }}\n"
		_, err := checkRefLiterals(repo(t, map[string]string{wf("a.yaml"): src}))
		require.ErrorIs(t, err, errRefLiteral)
	})
	t.Run("github を文字列リテラルの添字で引く比較も落とす", func(t *testing.T) {
		t.Parallel()
		src := "jobs:\n  a:\n    if: ${{ github['ref_name'] == 'main' }}\n"
		_, err := checkRefLiterals(repo(t, map[string]string{wf("a.yaml"): src}))
		require.ErrorIs(t, err, errRefLiteral)
	})
	t.Run("github をリテラルでない添字で引く式は読めないとして失敗する", func(t *testing.T) {
		t.Parallel()
		src := "jobs:\n  a:\n    if: ${{ github.event[format('{0}', 'ref')] == 'main' }}\n"
		_, err := checkRefLiterals(repo(t, map[string]string{wf("a.yaml"): src}))
		require.ErrorIs(t, err, errExpression)
		assert.ErrorContains(t, err, wf("a.yaml")+":3")
	})
	t.Run("包みの外に字面が残る if: は読めないとして失敗する", func(t *testing.T) {
		t.Parallel()
		src := "jobs:\n  a:\n    if: ${{ github.event_name }} == 'push'\n"
		_, err := checkRefLiterals(repo(t, map[string]string{wf("a.yaml"): src}))
		require.ErrorIs(t, err, errExpression)
	})
	t.Run("関数で加工した ref と kebab-case の input の比較も落とす", func(t *testing.T) {
		t.Parallel()
		src := "runs:\n  steps:\n    - if: format('{0}', github.ref_name) == 'main' || inputs.base-ref == 'main'\n      run: x\n"
		_, err := checkRefLiterals(repo(t, map[string]string{
			wf("scan.yaml"): scanWorkflowRendered,
			filepath.Join(actionsDir, "a", "action.yaml"): src,
		}))
		require.ErrorIs(t, err, errRefLiteral)
		assert.ErrorContains(t, err, "format('{0}', github.ref_name) == 'main'")
		assert.ErrorContains(t, err, "inputs.base-ref == 'main'")
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
		require.ErrorIs(t, err, fs.ErrNotExist)
	})
	t.Run("二重引用符の中の \\\" の後ろの式も読み、落とす", func(t *testing.T) {
		t.Parallel()
		src := "jobs:\n  a:\n    if: \"github.event_name != 'a \\\" #' && github.ref_name == 'x'\"\n"
		_, err := checkRefLiterals(repo(t, map[string]string{wf("a.yaml"): src}))
		require.ErrorIs(t, err, errRefLiteral)
		assert.ErrorContains(t, err, "github.ref_name == 'x'")
	})
	t.Run("一重引用符の値の中の式も YAML の文字列として読み、落とす", func(t *testing.T) {
		t.Parallel()
		src := "jobs:\n  a:\n    name: '${{ github.ref_name == ''main'' }}'\n"
		_, err := checkRefLiterals(repo(t, map[string]string{wf("a.yaml"): src}))
		require.ErrorIs(t, err, errRefLiteral)
		assert.ErrorContains(t, err, "github.ref_name == 'main'")
	})
	t.Run("読めない式と ref の比較が並べば、最初に読めない式で失敗する", func(t *testing.T) {
		t.Parallel()
		src := "jobs:\n  a:\n    if: github.ref_name == 'main'\n    name: ${{ lower(github.ref) }}\n"
		_, err := checkRefLiterals(repo(t, map[string]string{wf("a.yaml"): src}))
		require.ErrorIs(t, err, errExpression)
		assert.NotErrorIs(t, err, errRefLiteral)
		assert.ErrorContains(t, err, wf("a.yaml")+":4")
	})
	t.Run("複数のファイルの比較をすべて報せる", func(t *testing.T) {
		t.Parallel()
		root := repo(t, map[string]string{
			wf("a.yaml"): "jobs:\n  a:\n    if: github.ref_name == 'a'\n",
			wf("b.yaml"): "jobs:\n  b:\n    if: github.base_ref == 'b'\n",
		})
		_, err := checkRefLiterals(root)
		require.ErrorIs(t, err, errRefLiteral)
		assert.ErrorContains(t, err, wf("a.yaml")+":3")
		assert.ErrorContains(t, err, wf("b.yaml")+":3")
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
		"アンカーの付いたブロックスカラーの本文も生のまま読む":   {"      - run: &a |\n          echo x # ${{ y }}\n      - run: w\n", []expression{{2, "${{ y }}"}}, ""},
		"キーの無い列の要素のブロックスカラーも生のまま読む":    {"    - |\n      echo # ${{ y }}\n", []expression{{2, "${{ y }}"}}, ""},
		"コメントは読まない":                    {"# if: ${{ github.ref == 'a' }}\na: x # ${{ y }}\n", nil, ""},
		"印の if: は生成した項を除く": {
			"    if: ${{ contains(fromJSON('[\"a\"]'), github.base_ref) || x }} # branch-set: deploy\n",
			[]expression{{1, "${{ true || x }}"}}, "",
		},
		"文字列リテラルの中の }} で式を切らない":        {"a: ${{ format('}}') }}\n", []expression{{1, "${{ format('}}') }}"}}, ""},
		"閉じていない式は拒む":                   {"a: ${{ x\n", nil, "閉じていない式"},
		"読めない書き方の if: は拒む":             {"    steps: [{if: x}]\n", nil, "読めない書き方"},
		"明示的なキーの if: は拒む":              {"    ? if\n    : github.ref_name == 'main'\n", nil, "明示的なキー"},
		"名前の無い明示的なキーは拒む":               {"    ?\n    : x\n", nil, "明示的なキー"},
		"列の要素の明示的なキーは拒む":               {"    - ? if\n", nil, "明示的なキー"},
		"複数行の plain のスカラーの続きの行の ? も拒む": {"    a: foo\n      ? bar\n", nil, "明示的なキー"},
		"? で始まる名前のキーの値の式も読む":           {"    ?x: ${{ y }}\n", []expression{{1, "${{ y }}"}}, ""},
		"ブロックスカラーの本文の ? の行の式も読む":       {"    run: |\n      ? ${{ y }}\n", []expression{{2, "${{ y }}"}}, ""},
		"一重引用符の値は '' を ' として読む":        {"    name: '${{ a == ''b'' }}'\n", []expression{{1, "${{ a == 'b' }}"}}, ""},
		"行をまたぐ一重引用符の値の式は拒む":            {"    name: '${{ a ==\n      ''b'' }}'\n", nil, "YAML の文字列として読めません"},
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

func Test_unquotedValue(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct{ code, want string }{
		"キーの値の一重引用符を外す":           {"  name: '${{ a == ''b'' }}'", "${{ a == 'b' }}"},
		"列の要素の一重引用符を外す":           {"  - 'x ''${{ y }}'''", "x '${{ y }}'"},
		"引用符のキーの値の一重引用符を外す":       {`  "k": '${{ x }}'`, "${{ x }}"},
		"一重引用符のキーの plain の値はそのまま": {"  'k': ${{ x }}", "  'k': ${{ x }}"},
		"値の無い一重引用符のキーはそのまま":       {"  'k':", "  'k':"},
		"plain の値の中の一重引用符はそのまま":   {"  run: echo '${{ x }}'", "  run: echo '${{ x }}'"},
		"二重引用符の値はそのまま":            {`  a: "${{ x }}"`, `  a: "${{ x }}"`},
		"式の無い閉じない一重引用符の値はそのまま":    {"  a: 'x", "  a: 'x"},
		"列の要素のキーの値の一重引用符を外す":      {"  - name: '${{ a }}'", "${{ a }}"},
		"一重引用符のキーの一重引用符の値を外す":     {"  'k': '${{ x }}'", "${{ x }}"},
		"空の一重引用符の値は空文字列":          {"  a: ''", ""},
		"式の無い後ろに字面が続く値はそのまま":      {"  a: 'x' y", "  a: 'x' y"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := unquotedValue(tc.code, keyColumn(tc.code))
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
	for name, code := range map[string]string{
		"閉じない一重引用符の値の式は失敗する":    "  a: '${{ x",
		"閉じた引用符の後ろに字面が続く値は失敗する": "  a: '${{ x }}' y",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := unquotedValue(code, keyColumn(code))
			require.ErrorIs(t, err, errExpression)
			assert.ErrorContains(t, err, "YAML の文字列として読めません")
		})
	}
}

func Test_keyColumn(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		code string
		want int
	}{
		"字下げしたキー":               {"    if: x", 4},
		"列の要素のキーは - を含む":        {"      - if: x", 8},
		"入れ子の列の要素":              {"  - - run: x", 6},
		"- の後ろの空白が複数でもキーの桁を数える": {"  -   if: x", 6},
		"字下げの無いキー":              {"on:", 0},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, keyColumn(tc.code))
		})
	}
}

func Test_blockColumn(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		code string
		want int
	}{
		"キーの値はキーの桁":          {"      - run: |", 8},
		"キーの無い列の要素は - の桁":    {"    - |", 4},
		"- の後ろに空白が続いても - の桁": {"    -   |", 4},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, blockColumn(tc.code, keyColumn(tc.code)))
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
		"続きの行を空白でつなぐ":              {"    if: a &&\n      b\n    run: x", "a && b", false},
		"続きの行のコメントは読まない":           {"    if: a &&\n      b # c\n    run: x", "a && b", false},
		"続きが無ければ1行目だけ":             {"    if: a\n    run: x", "a", false},
		"二重引用符の値は閉じるまでつなぐ":         {"    if: \"a &&\n      b\"\n    run: x", "\"a && b\"", false},
		"閉じていない引用符は失敗する":           {"    if: 'a &&\n      b\n    run: x", "", true},
		"引用符1文字だけの値は閉じていないとして失敗する": {"    if: \"\n    run: x", "", true},
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

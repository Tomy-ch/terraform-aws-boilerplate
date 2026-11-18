package main

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/workflow"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

var (
	errBranchSet = xerrors.New("workflow の分岐の集合を生成できません")
	errSetDrift  = xerrors.New("workflow の印 `# branch-set:` の行が branches.json からずれています（make repo-settings-apply）")

	// setMarker は、生成する行の印。行末のコメントとして置く。
	setMarker = regexp.MustCompile(`#\s*branch-set:\s*(\S+)\s*$`)
	// branchesLine は、起動条件の branches / branches-ignore のキーの行。
	branchesLine = regexp.MustCompile(`^(\s*)(?:"branches(?:-ignore)?"|'branches(?:-ignore)?'|branches(?:-ignore)?)[ \t]*:(.*)$`)
	// branchesLoose は、branchesLine が読めない書き方も拾う。数が食い違えば、読めない形がある。
	branchesLoose = regexp.MustCompile(`branches(?:-ignore)?["']?\s*:`)
	// ifLine は、job か step の if: の行。
	ifLine = regexp.MustCompile(`^(\s*)(?:-\s+)?(?:"if"|'if'|if)[ \t]*:(.*)$`)
	// refContext は、ブランチの名前か ref を値に持つ context の閉じた一覧。
	refContext = regexp.MustCompile(`github\.(?:ref_name|ref|base_ref|head_ref|event\.ref|event\.pull_request\.base\.ref|event\.pull_request\.head\.ref|event\.workflow_run\.head_branch)\b`)
	// containsSet は、完全一致の集合を名指す項 contains(fromJSON('[...]'), <ref>)。
	containsSet = regexp.MustCompile(`contains\(fromJSON\('(\[[^']*\])'\),\s*(` + refContext.String() + `)\)`)
	// prefixGroup は、前方一致の集合を名指す項 (startsWith(<ref>, '<prefix>') || ...)。
	prefixGroup = regexp.MustCompile(`\(startsWith\((` + refContext.String() + `),\s*'[^']*'\)(?:\s*\|\|\s*startsWith\((?:` + refContext.String() + `),\s*'[^']*'\))*\)`)
	startsCtx   = regexp.MustCompile(`startsWith\((` + refContext.String() + `),`)
)

// renderBranchSets は、印 `# branch-set: <集合>` を持つ行を、分岐のパターンの宣言から生成し直します
// （ADR-0603 決定2-3）。対象は .github/workflows の workflow で、印を置けるのは on.<event>.branches(-ignore) の
// キーの行と、if: の行である。印の無い branches(-ignore) の一覧は、apply の workflow の on.push.branches
// （[renderApplyTriggers] が生成する）のほかはエラーにする —— 宣言の外にパターンが残る。applyPaths は
// apply の workflow の root からの位置で、そこに印を置くこともエラーにする。書き込みはしない。
func renderBranchSets(root string, b branches, applyPaths []string) ([]workflowEdit, int, error) {
	dir := filepath.Join(root, workflowsDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, 0, xerrors.Wrapf(err, "%s の読み取り", workflowsDir)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	var (
		edits []workflowEdit
		marks int
	)
	for _, path := range workflow.SelectFiles(names, dir) {
		src, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			return nil, 0, xerrors.Wrapf(err, "%s の読み取り", path)
		}
		rel, _ := filepath.Rel(root, path)
		rendered, n, err := renderSetLines(string(src), b, slices.Contains(applyPaths, rel))
		if err != nil {
			return nil, 0, xerrors.Wrapf(err, "%s", rel)
		}
		marks += n
		if n > 0 {
			edits = append(edits, workflowEdit{path: rel, current: string(src), rendered: rendered})
		}
	}
	return edits, marks, nil
}

// renderSetLines は、1つの workflow の印を持つ行を生成し直した本文と、印の数を返します。isApply は apply の
// workflow であることで、そのとき on.push.branches は印を持たずに [renderApplyTriggers] が生成する。
func renderSetLines(src string, b branches, isApply bool) (string, int, error) {
	lines := strings.Split(src, "\n")
	var applyStart int
	if isApply {
		start, _, err := pushBranchesItems(lines)
		if err != nil {
			return "", 0, err
		}
		applyStart = start - 1
	}
	var (
		out   []string
		marks int
		keys  int
		loose int
	)
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		code, comment := splitComment(l)
		if branchesLoose.MatchString(code) {
			loose++
		}
		mark := ""
		if m := setMarker.FindStringSubmatch(comment); m != nil && strings.TrimSpace(code) != "" {
			mark = m[1]
		}
		if mark != "" && isApply {
			return "", 0, xerrors.Wrapf(errBranchSet, "%d 行目: apply の workflow には印を置けません（起動条件は job の environment から生成します）", i+1)
		}
		switch {
		case branchesLine.MatchString(code):
			keys++
			if mark == "" {
				if isApply && i == applyStart {
					out = append(out, l)
					continue
				}
				return "", 0, xerrors.Wrapf(errBranchSet, "%d 行目: 印 `# branch-set: <集合>` の無い branches の一覧があります", i+1)
			}
			items, next, err := renderBranchesItems(lines, i, code, b, mark)
			if err != nil {
				return "", 0, err
			}
			out = append(out, l)
			out = append(out, items...)
			marks++
			i = next - 1
		case mark != "" && ifLine.MatchString(code):
			rendered, err := renderIfLine(code, b, mark)
			if err != nil {
				return "", 0, xerrors.Wrapf(err, "%d 行目", i+1)
			}
			out = append(out, rendered+comment)
			marks++
		case mark != "":
			return "", 0, xerrors.Wrapf(errBranchSet, "%d 行目: 印は branches か if: の行にだけ置けます", i+1)
		default:
			out = append(out, l)
		}
	}
	if keys != loose {
		return "", 0, xerrors.Wrapf(errBranchSet, "branches のキーを %d 件読みましたが、字面では %d 件あります。読めない書き方があります", keys, loose)
	}
	return strings.Join(out, "\n"), marks, nil
}

// renderBranchesItems は、at 行の branches のキーの下の一覧を、集合 set の展開で置き換えた行と、一覧の次の行の
// 位置を返します。キーはブロック形式の一覧でなければならない。
func renderBranchesItems(lines []string, at int, code string, b branches, set string) ([]string, int, error) {
	m := branchesLine.FindStringSubmatch(code)
	if strings.TrimSpace(m[2]) != "" {
		return nil, 0, xerrors.Wrapf(errBranchSet, "%d 行目: branches はブロック形式の一覧で書いてください", at+1)
	}
	patterns, err := triggerPatterns(b, set)
	if err != nil {
		return nil, 0, xerrors.Wrapf(err, "%d 行目", at+1)
	}
	indent := len(m[1])
	pad := strings.Repeat(" ", indent+2)
	end := at + 1
	for j := at + 1; j < len(lines); j++ {
		l := lines[j]
		switch {
		case strings.HasPrefix(l, pad+"- "):
			end = j + 1
		case blankOrComment.MatchString(l):
		case len(l)-len(strings.TrimLeft(l, " ")) <= indent:
			j = len(lines)
		default:
			return nil, 0, xerrors.Wrapf(errBranchSet, "%d 行目が branches の要素ではありません", j+1)
		}
	}
	if end == at+1 {
		return nil, 0, xerrors.Wrapf(errBranchSet, "%d 行目: branches に要素がありません", at+1)
	}
	out := make([]string, 0, len(patterns))
	for _, p := range patterns {
		if strings.ContainsAny(p, "*") {
			p = "'" + p + "'"
		}
		out = append(out, pad+"- "+p)
	}
	return out, end, nil
}

// renderIfLine は、if: の行の式のうち、集合 set を名指す項1つを宣言から生成し直します。deploy は
// contains(fromJSON('[...]'), <ref>) の配列を、lines は (startsWith(<ref>, '<prefix>') || ...) の群を書き換える。
func renderIfLine(code string, b branches, set string) (string, error) {
	switch set {
	case setDeploy:
		found := containsSet.FindAllStringSubmatchIndex(code, -1)
		if len(found) != 1 {
			return "", xerrors.Wrapf(errBranchSet, "印 deploy の行には contains(fromJSON('[...]'), <ref>) がちょうど1つ要ります（%d 件）", len(found))
		}
		array, err := jsonArray(b.DeployNames())
		if err != nil {
			return "", err
		}
		f := found[0]
		return code[:f[2]] + array + code[f[3]:], nil
	case setLines:
		found := prefixGroup.FindAllStringIndex(code, -1)
		if len(found) != 1 {
			return "", xerrors.Wrapf(errBranchSet, "印 lines の行には (startsWith(<ref>, '<接頭辞>') || ...) の群がちょうど1つ要ります（%d 件）", len(found))
		}
		f := found[0]
		group := code[f[0]:f[1]]
		var ctx string
		for _, m := range startsCtx.FindAllStringSubmatch(group, -1) {
			if ctx != "" && m[1] != ctx {
				return "", xerrors.Wrapf(errBranchSet, "startsWith の群が別の context（%s と %s）を比べています", ctx, m[1])
			}
			ctx = m[1]
		}
		return code[:f[0]] + prefixExpression(ctx, b.Lines.Prefixes()) + code[f[1]:], nil
	}
	return "", xerrors.Wrapf(errBranchSet, "if: の行に置ける印は %s と %s です（%q）", setDeploy, setLines, set)
}

// prefixExpression は、ctx が prefixes のどれかで始まることを表す式を返します。
func prefixExpression(ctx string, prefixes []string) string {
	terms := make([]string, 0, len(prefixes))
	for _, p := range prefixes {
		terms = append(terms, "startsWith("+ctx+", '"+p+"')")
	}
	return "(" + strings.Join(terms, " || ") + ")"
}

// jsonArray は、名前を Actions の式の fromJSON に渡す JSON の配列の字面で返します。
func jsonArray(names []string) (string, error) {
	if len(names) == 0 {
		return "", xerrors.Wrap(errBranchSet, "集合の展開が0件です")
	}
	quoted := make([]string, 0, len(names))
	for _, n := range names {
		quoted = append(quoted, `"`+n+`"`)
	}
	return "[" + strings.Join(quoted, ", ") + "]", nil
}

// splitComment は、YAML の1行をコードと行末のコメント（空白と `#` から始まる部分）へ分けます。引用符の中の
// `#` はコメントにしない。
func splitComment(line string) (code, comment string) {
	var quote rune
	for i, r := range line {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '\'' || r == '"':
			quote = r
		case r == '#' && (i == 0 || line[i-1] == ' ' || line[i-1] == '\t'):
			j := i
			for j > 0 && (line[j-1] == ' ' || line[j-1] == '\t') {
				j--
			}
			return line[:j], line[j:]
		}
	}
	return line, ""
}

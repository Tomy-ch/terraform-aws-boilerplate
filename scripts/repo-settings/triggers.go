package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/workflow"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

var (
	errBranchSet  = xerrors.New("workflow の分岐の集合を生成できません")
	errRefLiteral = xerrors.New("workflow の式が ref を宣言から生成していない文字列と比べています（印 `# branch-set:` で生成してください）")
	errExpression = xerrors.New("workflow の式を読めません")
	errSetDrift   = xerrors.New("workflow の印 `# branch-set:` の行が branches.json からずれています（make repo-settings-apply）")

	// setMarker は、生成する行の印。行末のコメントとして置く。
	setMarker = regexp.MustCompile(`#\s*branch-set:\s*(\S+)\s*$`)
	// branchesLine は、起動条件の branches / branches-ignore のキーの行。
	branchesLine = regexp.MustCompile(`^(\s*)(?:"branches(?:-ignore)?"|'branches(?:-ignore)?'|branches(?:-ignore)?)[ \t]*:(.*)$`)
	// branchesLoose は、branchesLine が読めない書き方も拾う。数が食い違えば、読めない形がある。
	branchesLoose = regexp.MustCompile(`branches(?:-ignore)?["']?\s*:`)
	ifLine        = regexp.MustCompile(`^(\s*)(?:-\s+)?(?:"if"|'if'|if)[ \t]*:(.*)$`)
	// refContext は、ブランチの名前か ref を値に持つ context の閉じた一覧。Actions の式は context と property の
	// 名前の大文字小文字を区別しないので、ここも区別しない。
	refContext = regexp.MustCompile(`(?i:github\.(?:ref_name|ref|base_ref|head_ref|event\.ref|event\.pull_request\.base\.ref|event\.pull_request\.head\.ref|event\.workflow_run\.head_branch))\b`)
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
	// 一覧は丸ごと生成し直すので、中のコメントは残せない。黙って消さずに止める。
	for j := at + 1; j < end; j++ {
		if _, comment := splitComment(lines[j]); comment != "" {
			return nil, 0, xerrors.Wrapf(errBranchSet, "%d 行目: 印の付いた branches の一覧の中にはコメントを置けません", j+1)
		}
	}
	// 名前は YAML の文字列として読ませる。素のままだと null や数に読まれる名前がある。
	out := make([]string, 0, len(patterns))
	for _, p := range patterns {
		out = append(out, pad+"- '"+strings.ReplaceAll(p, "'", "''")+"'")
	}
	return out, end, nil
}

// renderIfLine は、if: の行の式のうち、集合 set を名指す項1つを宣言から生成し直します。deploy は
// contains(fromJSON('[...]'), <ref>) の配列を、lines は (startsWith(<ref>, '<prefix>') || ...) の群を書き換える。
func renderIfLine(code string, b branches, set string) (string, error) {
	// 生成する項は `"` と `'` を含むので、引用符のスカラーの中へは置けない。
	if m := ifLine.FindStringSubmatch(code); m != nil {
		if v := strings.TrimSpace(m[2]); strings.HasPrefix(v, `"`) || strings.HasPrefix(v, "'") {
			return "", xerrors.Wrap(errBranchSet, "印の付いた if: の値は引用符で囲めません")
		}
	}
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
			if ctx != "" && !strings.EqualFold(m[1], ctx) {
				return "", xerrors.Wrapf(errBranchSet, "startsWith の群が別の context（%s と %s）を比べています", ctx, m[1])
			}
			ctx = m[1]
		}
		prefixes := b.Lines.Prefixes()
		if len(prefixes) == 0 {
			return "", xerrors.Wrap(errBranchSet, "集合 lines の展開が0件です")
		}
		return code[:f[0]] + prefixExpression(ctx, prefixes) + code[f[1]:], nil
	}
	return "", xerrors.Wrapf(errBranchSet, "if: の行に置ける印は %s と %s です（%q）", setDeploy, setLines, set)
}

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
// `#` はコメントにしない。二重引用符の中の `\` は次の1文字をエスケープする。
func splitComment(line string) (code, comment string) {
	var (
		quote   rune
		escaped bool
	)
	for i, r := range line {
		switch {
		case escaped:
			escaped = false
		case quote == '"' && r == '\\':
			escaped = true
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

const actionsDir = ".github/actions"

var (
	// refLiteral は、ref の context と文字列リテラルを比べる項。
	refLiteral = []*regexp.Regexp{
		regexp.MustCompile(`(` + refContext.String() + `)\s*(?:==|!=)\s*'`),
		regexp.MustCompile(`'[^']*'\s*(?:==|!=)\s*(` + refContext.String() + `)`),
		regexp.MustCompile(`(?i:startsWith|endsWith|contains)\(\s*(` + refContext.String() + `)\s*,\s*'`),
		regexp.MustCompile(`(?i:contains)\(\s*(?:(?i:fromJSON)\(\s*)?'[^']*'\s*\)?\s*,\s*(` + refContext.String() + `)`),
	}
	// indexedGithub は、github の context を添字で引く書き方。添字の先が ref かを字面では決められないので、
	// 読めない式として扱う。
	indexedGithub = regexp.MustCompile(`(?i)\bgithub\s*\[`)
	exprOpen      = regexp.MustCompile(`\$\{\{`)
	exprWhole     = regexp.MustCompile(`\$\{\{.*?\}\}`)
	ifLoose       = regexp.MustCompile(`(?:^|[\s{,-])["']?if["']?\s*:`)
	// blockScalar は、値がブロックスカラーで始まるキーの行（コメントを除いた字面）。
	blockScalar = regexp.MustCompile(`:[ \t]*[|>][-+0-9]*[ \t]*$`)
)

// checkRefLiterals は、workflow と composite action の式（`${{ }}` と if: の値）が、ref の context
// （[refContext]）を文字列リテラルと比べていないことを確かめます（ADR-0603 検証方法）。印 `# branch-set:` の
// 行では、生成した項（[renderIfLine] が書き換える部分）だけを除いて見る。読んだ式の数を返す。対象のファイルか
// 式が1件も無いとき、`${{` と if: のキーの字面の数が読めた数と食い違うときは失敗する。
func checkRefLiterals(root string) (int, error) {
	files, err := expressionFiles(root)
	if err != nil {
		return 0, err
	}
	if len(files) == 0 {
		return 0, xerrors.Wrapf(errExpression, "%s と %s に対象のファイルがありません", workflowsDir, actionsDir)
	}
	total := 0
	var findings []string
	for _, path := range files {
		src, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			return 0, xerrors.Wrapf(err, "%s の読み取り", path)
		}
		rel, _ := filepath.Rel(root, path)
		exprs, err := expressions(string(src))
		if err != nil {
			return 0, xerrors.Wrapf(err, "%s", rel)
		}
		total += len(exprs)
		for _, e := range exprs {
			if indexedGithub.MatchString(e.text) {
				return 0, xerrors.Wrapf(errExpression, "%s:%d: github の context を添字で引く書き方は読めません。ドット記法で書いてください", rel, e.line)
			}
			for _, term := range refLiteralTerms(e.text) {
				findings = append(findings, fmt.Sprintf("%s:%d: %s", rel, e.line, term))
			}
		}
	}
	if total == 0 {
		return 0, xerrors.Wrap(errExpression, "式を1件も読めませんでした")
	}
	if len(findings) > 0 {
		return 0, xerrors.Wrapf(errRefLiteral, "%s", strings.Join(findings, "; "))
	}
	return total, nil
}

func expressionFiles(root string) ([]string, error) {
	dir := filepath.Join(root, workflowsDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, xerrors.Wrapf(err, "%s の読み取り", workflowsDir)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	files := workflow.SelectFiles(names, dir)
	actions := filepath.Join(root, actionsDir)
	err = filepath.WalkDir(actions, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && (d.Name() == "action.yml" || d.Name() == "action.yaml") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil && !xerrors.Is(err, fs.ErrNotExist) {
		return nil, xerrors.Wrapf(err, "%s の読み取り", actionsDir)
	}
	return files, nil
}

type expression struct {
	line int
	text string
}

// expressions は、YAML の本文から式を取り出します。if: の値は1つの式で、折り返した plain・引用符のスカラーと
// ブロックスカラーを含む。ほかの行は、その中の `${{ }}` が式である。ブロックスカラーの本文は Actions が評価する
// 文字列なので、`#` を含めて生のまま読む。それ以外のコメントは読まない。印 `# branch-set:` の if: の行は、生成した
// 項を除く。
func expressions(src string) ([]expression, error) {
	lines := strings.Split(src, "\n")
	var (
		out        []expression
		opens, ifs int
		readOpens  int
		readIfs    int
	)
	for i := 0; i < len(lines); i++ {
		code, comment := splitComment(lines[i])
		if strings.TrimSpace(code) == "" {
			continue
		}
		opens += len(exprOpen.FindAllString(code, -1))
		if ifLoose.MatchString(code) {
			ifs++
		}
		col := keyColumn(code)
		m := ifLine.FindStringSubmatch(code)
		if m == nil {
			for _, e := range exprWhole.FindAllString(code, -1) {
				out = append(out, expression{line: i + 1, text: e})
				readOpens++
			}
			if blockScalar.MatchString(code) {
				body, next := blockBody(lines, i+1, col)
				for j, l := range body {
					opens += len(exprOpen.FindAllString(l, -1))
					for _, e := range exprWhole.FindAllString(l, -1) {
						out = append(out, expression{line: i + 2 + j, text: e})
						readOpens++
					}
				}
				i = next - 1
			}
			continue
		}
		readIfs++
		at := i + 1
		value := strings.TrimSpace(m[2])
		if blockScalar.MatchString(code) {
			body, next := blockBody(lines, i+1, col)
			for _, l := range body {
				n := len(exprOpen.FindAllString(l, -1))
				opens += n
				readOpens += n
			}
			value = strings.Join(body, "\n")
			i = next - 1
		} else {
			folded, next, err := foldedScalar(lines, i, value, col)
			if err != nil {
				return nil, xerrors.Wrapf(err, "%d 行目", at)
			}
			for _, l := range lines[i+1 : next] {
				opens += len(exprOpen.FindAllString(l, -1))
			}
			value = folded
			readOpens += len(exprWhole.FindAllString(value, -1))
			i = next - 1
		}
		if mark := setMarker.FindStringSubmatch(comment); mark != nil {
			value = withoutGenerated(value, mark[1])
		}
		out = append(out, expression{line: at, text: value})
	}
	if opens != readOpens {
		return nil, xerrors.Wrapf(errExpression, "`${{` を %d 件読みましたが、字面では %d 件あります。閉じていない式があります", readOpens, opens)
	}
	if ifs != readIfs {
		return nil, xerrors.Wrapf(errExpression, "if: を %d 件読みましたが、字面では %d 件あります。読めない書き方があります", readIfs, ifs)
	}
	return out, nil
}

// keyColumn は、行のキーが始まる桁を返します。列の要素の `- ` はキーの桁に含める。
func keyColumn(code string) int {
	rest := strings.TrimLeft(code, " ")
	col := len(code) - len(rest)
	for strings.HasPrefix(rest, "- ") {
		trimmed := strings.TrimLeft(rest[2:], " ")
		col += len(rest) - len(trimmed)
		rest = trimmed
	}
	return col
}

// blockBody は、from 行からの、字下げが col より深い行と空行（ブロックスカラーの本文）と、その次の行の位置を
// 返します。本文の末尾の空行は含めない。
func blockBody(lines []string, from, col int) ([]string, int) {
	end := from
	for j := from; j < len(lines); j++ {
		l := lines[j]
		if strings.TrimSpace(l) == "" {
			continue
		}
		if len(l)-len(strings.TrimLeft(l, " ")) <= col {
			break
		}
		end = j + 1
	}
	return lines[from:end], end
}

// foldedScalar は、at 行のキーの値 first を、字下げが col より深い続きの行と合わせて1つの値にし、その次の行の
// 位置を返します。plain スカラーの続きの行のコメントは読まない。引用符で始まる値が、続きの行を合わせても
// 同じ引用符で閉じていなければ失敗する。
func foldedScalar(lines []string, at int, first string, col int) (string, int, error) {
	quote := ""
	if strings.HasPrefix(first, `"`) || strings.HasPrefix(first, "'") {
		quote = first[:1]
	}
	parts := []string{first}
	next := at + 1
	for j := at + 1; j < len(lines); j++ {
		l := lines[j]
		if strings.TrimSpace(l) == "" {
			continue
		}
		if len(l)-len(strings.TrimLeft(l, " ")) <= col {
			break
		}
		if quote == "" {
			l, _ = splitComment(l)
		}
		parts = append(parts, strings.TrimSpace(l))
		next = j + 1
	}
	value := strings.Join(parts, " ")
	if quote != "" && (len(value) < 2 || !strings.HasSuffix(value, quote)) {
		return "", 0, xerrors.Wrap(errExpression, "引用符の if: の値が閉じていません")
	}
	return value, next, nil
}

// withoutGenerated は、印 set の if: の値から、[renderIfLine] が生成した項を除きます。
func withoutGenerated(value, set string) string {
	switch set {
	case setDeploy:
		return containsSet.ReplaceAllString(value, "true")
	case setLines:
		return prefixGroup.ReplaceAllString(value, "true")
	}
	return value
}

func refLiteralTerms(expr string) []string {
	out := make([]string, 0, len(refLiteral))
	for _, re := range refLiteral {
		out = append(out, re.FindAllString(expr, -1)...)
	}
	return out
}

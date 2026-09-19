// Package main は、分岐のパターンの単一宣言を生成先へ反映・検証するツール。
//
//	apply   生成先を宣言から組み直す
//	check   生成先が宣言からずれていないかを検査する（書き換えなし）
//
// 宣言は scripts/lib/branches が持つ（ADR-0603 決定1）。生成先は2つある。
//
//	.github/settings/branch-protection.json   conditions.ref_name.include
//	.github/workflows/*.yaml                  on.push.branches と、ブランチ集合の式
//
// 他の読み手（base-branch / release / repo-setup）は同じパッケージを import するので、
// 生成も突合も要らない —— コンパイラが一致を保証する。
//
// ずれは静かに壊れる。保護対象から漏れたブランチは「保護されていない」ではなく
// 「保護されているつもり」になる。だから check を持ち、pre-commit と CI の両方で走らせる。
//
// **閉じる方向で落ちる。** 宣言の対象外の workflow が起動条件やブランチ集合の式を持つこと、
// 宣言が指す workflow の不在、branches ブロックに項目以外の行があること —— いずれもエラーに
// して読み飛ばさない。読み飛ばすと、打ち間違えた1行が「宣言されていない」と同じ扱いになり、
// 生成の対象から黙って消える（ADR-0702 決定14）。
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/branches"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/yamlblock"
)

const (
	protectionFile = ".github/settings/branch-protection.json"
	workflowDir    = ".github/workflows"
	refPrefix      = "refs/heads/"
	filePerm       = 0o644
	jsonIndent     = "  "
)

// includePath は、書き換える配列までの経路。requireRefName と includeSpan の両方が
// これを見るので、経路が2箇所に書かれない。
var includePath = []string{"conditions", "ref_name", "include"}

var (
	// errUsage は、サブコマンドの与え方が誤っていることを表す。
	errUsage = xerrors.New("usage: branches <apply|check>")
	// errDrift は、生成先が宣言からずれていることを表す。
	errDrift = xerrors.New("生成先が宣言からずれています")
	// errShape は、生成先が期待する構造を持たないことを表す。
	errShape = xerrors.New("生成先の構造が想定と異なります")
	// errUnmanaged は、宣言の対象外の workflow がブランチのパターンを直接書いていることを表す。
	errUnmanaged = xerrors.New("宣言の対象外の workflow がブランチのパターンを持っています")
	// errOrphan は、宣言が指す workflow が生成対象を持たないことを表す。
	errOrphan = xerrors.New("宣言が指す workflow に生成対象がありません")
)

// workflowSets は、push 側の起動条件を生成する先と、その集合。
//
// **ここに無い workflow がブランチのパターンを持っていたら落とす。** 生成の対象から漏れた
// 記述は、宣言を直しても追随せず、黙って古いパターンで動き続ける（ADR-0603 決定3）。
var workflowSets = map[string][]string{
	"trivy-config.yaml":       branches.GatePush,
	"zizmor.yaml":             branches.GatePush,
	"trivy-fs.yaml":           branches.ReleasePush,
	"trivy-release-gate.yaml": branches.Deploy,
}

var (
	// onKeyRe は最上位の `on:`。YAML 1.1 の真偽値を避けて引用する書き方も受ける。
	onKeyRe = regexp.MustCompile(`^(?:on|"on"|'on'):\s*$`)
	// blockKeyRe は、字下げされた値なしのキー（`push:` / `branches:`）。
	blockKeyRe = regexp.MustCompile(`^(\s+)([A-Za-z_][\w-]*):\s*$`)
	// itemRe は配列の項目。引用の有無を問わず値だけを取り出す。
	itemRe = regexp.MustCompile(`^\s*-\s+(?:'([^']*)'|"([^"]*)"|(\S+))\s*$`)
	// branchSetRe は、ブランチ名と突き合わせる式に現れる集合のリテラル。
	branchSetRe = regexp.MustCompile(`fromJSON\('(\[[^']*\])'\)`)
	// branchRefRe は、その式が突き合わせている相手。**これが無い行は対象にしない** ——
	// job の結果を並べた fromJSON（`["failure", "cancelled"]`）を掴まないため。
	branchRefRe = regexp.MustCompile(`github\.(?:base_ref|ref_name)`)
	// yamlQuoteRe は、素のまま書くと YAML が別の意味に取りうる文字。
	yamlQuoteRe = regexp.MustCompile(`[*?\[\]{}#,&!|>%@` + "`" + `]`)
)

// main は 1:1 テスト規約の対象外で分岐を検査できないため、判断は run に置きます。
func main() {
	log.SetFlags(0)

	if err := run(os.Args[1:], os.Stdout); err != nil {
		log.Fatalf("%v", err)
	}
}

// run は、サブコマンドを解釈して固定処理へ振り分けます。
func run(args []string, out io.Writer) error {
	if len(args) != 1 {
		return errUsage
	}

	switch args[0] {
	case "apply":
		return applyAll(".", false, out)
	case "check":
		return applyAll(".", true, out)
	default:
		return xerrors.Wrap(errUsage, "unknown subcommand: "+args[0])
	}
}

// applyAll は、2つの生成先を順に揃えます。
//
// **ずれは両方を見てから返す。** 片方で打ち切ると、直して再実行するまでもう片方のずれが
// 見えず、「直した」と「まだ残っている」が交互に現れる。構造の違反はその場で止める ——
// 構造が読めない状態で生成を続けると、何を書き換えたのかが誰にも分からなくなる。
func applyAll(root string, dryRun bool, out io.Writer) error {
	jsonErr := applyOrCheck(filepath.Join(root, protectionFile), dryRun, out)
	if jsonErr != nil && !errors.Is(jsonErr, errDrift) {
		return jsonErr
	}

	wfErr := applyOrCheckWorkflows(filepath.Join(root, workflowDir), workflowSets, dryRun, out)
	if wfErr != nil && !errors.Is(wfErr, errDrift) {
		return wfErr
	}

	if jsonErr != nil {
		return jsonErr
	}

	return wfErr
}

// applyOrCheck は保護設定を宣言へ揃えます。dryRun=true は書き換えず、ずれを報告して
// 非ゼロで終わります。
func applyOrCheck(path string, dryRun bool, out io.Writer) error {
	before, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return xerrors.Wrap(err, path)
	}

	after, err := rewrite(before, branches.Protected)
	if err != nil {
		return xerrors.Wrap(err, path)
	}

	if string(before) == string(after) {
		fmt.Fprintf(out, "✅ branches: 保護対象 %d 件が宣言と一致しています\n", len(branches.Protected))

		return nil
	}

	if dryRun {
		fmt.Fprintf(out, "❌ %s が宣言からずれています（make branches-apply で反映）\n", path)

		return errDrift
	}

	// path を引数に取るのは、一時ディレクトリで書き換えを試すための seam である。
	// 実行時に渡るのは package 定数 protectionFile だけで、外部入力は通らない。
	// 撤回条件: 呼び出し側が package の外から path を受け取る形になったとき。
	if err := os.WriteFile(path, after, filePerm); err != nil { //nolint:gosec // 上のコメントを参照
		return xerrors.Wrap(err, path)
	}
	fmt.Fprintf(out, "✅ branches-apply: %s へ保護対象 %d 件を反映しました\n", path, len(branches.Protected))

	return nil
}

// rewrite は保護設定の conditions.ref_name.include を宣言から組み直します。
//
// **まず JSON として検証し、置換は include 配列の区間だけに限る。** 全体を再直列化すると
// キーの並びが辞書順へ変わり、生成器が触るべきでない箇所まで書き換わる。逆に検証を省いて
// 文字列置換だけで済ませると、整形が変わった瞬間に別の配列の閉じ括弧へ着地する。
func rewrite(content []byte, protected []string) ([]byte, error) {
	var doc map[string]any
	if err := json.Unmarshal(content, &doc); err != nil {
		return nil, xerrors.Wrap(err, "JSON として読めません")
	}

	if err := requireRefName(doc); err != nil {
		return nil, err
	}

	// **宣言が空なら生成しない。** 保護対象0件の設定は「保護されていない」ではなく
	// 「保護されているつもり」を作る（ADR-0702 決定13）。定数だから空にならない、は
	// 保証ではない —— Protected は var であり、書き換えられる。
	if len(protected) == 0 {
		return nil, xerrors.Wrap(errShape, "宣言の保護対象が0件です")
	}

	start, end, err := includeSpan(content)
	if err != nil {
		return nil, err
	}

	indent := indentOf(content, start)
	items := make([]string, 0, len(protected))
	for _, p := range protected {
		items = append(items, indent+jsonIndent+strconv.Quote(refPrefix+p))
	}
	block := "[\n" + strings.Join(items, ",\n") + "\n" + indent + "]"

	out := make([]byte, 0, len(content))
	out = append(out, content[:start]...)
	out = append(out, block...)
	out = append(out, content[end:]...)

	return out, nil
}

// requireRefName は conditions.ref_name.include の存在を確かめます。**不在は構造の違反として
// エラーにする** —— 作って続行すると、保護対象を1件も持たない設定を生成したまま成功で返る。
func requireRefName(doc map[string]any) error {
	conditions, ok := doc["conditions"].(map[string]any)
	if !ok {
		return xerrors.Wrap(errShape, "conditions がありません")
	}

	refName, ok := conditions["ref_name"].(map[string]any)
	if !ok {
		return xerrors.Wrap(errShape, "conditions.ref_name がありません")
	}

	if _, ok := refName["include"].([]any); !ok {
		return xerrors.Wrap(errShape, "conditions.ref_name.include がありません")
	}

	return nil
}

// includeSpan は conditions.ref_name.include の値（配列）が占めるバイト区間を返します。
//
// **構造を辿って位置を決める。** バイト列から `"include"` を素朴に探すと、先に現れた
// 同名のキー —— GitHub の ruleset は conditions.repository_name にも同じ include / exclude の
// 形を持つ —— や、値として現れた文字列を掴む。掴んだ先を Protected で上書きすれば、
// 保護設定は「本来の include が古いまま、無関係な配列が壊れた」状態になり、しかも成功で返る。
func includeSpan(content []byte) (int, int, error) {
	dec := json.NewDecoder(bytes.NewReader(content))

	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return 0, 0, xerrors.Wrap(errShape, "最上位が object ではありません")
	}

	start, end, found, err := findArray(dec, includePath)
	if err != nil {
		return 0, 0, xerrors.Wrap(errShape, strings.Join(includePath, ".")+" を辿れません")
	}
	if !found {
		return 0, 0, xerrors.Wrap(errShape, strings.Join(includePath, ".")+" がありません")
	}

	return start, end, nil
}

// findArray は、開き波括弧を読み終えた object の中で want の経路を辿り、
// 行き着いた配列のバイト区間を返します。
func findArray(dec *json.Decoder, want []string) (int, int, bool, error) {
	for {
		tok, err := dec.Token()
		if err != nil {
			return 0, 0, false, err
		}
		if d, ok := tok.(json.Delim); ok && d == '}' {
			return 0, 0, false, nil
		}

		key, _ := tok.(string)

		value, err := dec.Token()
		if err != nil {
			return 0, 0, false, err
		}

		d, ok := value.(json.Delim)
		if !ok {
			continue // スカラーは経路にならない
		}

		switch d {
		case '{':
			if len(want) > 1 && key == want[0] {
				s, e, f, err := findArray(dec, want[1:])
				if err != nil || f {
					return s, e, f, err
				}

				continue
			}
			if err := skipContainer(dec); err != nil {
				return 0, 0, false, err
			}
		case '[':
			if len(want) == 1 && key == want[0] {
				// Token が返した直後の位置は開き括弧の1つ先にある。
				at := int(dec.InputOffset()) - 1
				if err := skipContainer(dec); err != nil {
					return 0, 0, false, err
				}

				return at, int(dec.InputOffset()), true, nil
			}
			if err := skipContainer(dec); err != nil {
				return 0, 0, false, err
			}
		}
	}
}

// skipContainer は、開き括弧を読み終えた container を閉じ括弧まで読み飛ばします。
func skipContainer(dec *json.Decoder) error {
	for depth := 1; depth > 0; {
		tok, err := dec.Token()
		if err != nil {
			return err
		}

		if d, ok := tok.(json.Delim); ok {
			switch d {
			case '{', '[':
				depth++
			case '}', ']':
				depth--
			}
		}
	}

	return nil
}

// indentOf は、その位置を含む行の字下げを返します。
func indentOf(content []byte, at int) string {
	lineStart := bytes.LastIndexByte(content[:at], '\n') + 1

	n := 0
	for lineStart+n < len(content) && content[lineStart+n] == ' ' {
		n++
	}

	return strings.Repeat(" ", n)
}

// ---- workflow の起動条件とブランチ集合 --------------------------------------

// applyOrCheckWorkflows は、workflow のブランチのパターンを宣言へ揃えます。
//
// **走査は全 workflow に掛ける。** 宣言に載っている分だけを開くと、宣言の対象外の
// workflow が直接書いたパターンは誰の目にも触れない —— それが ADR-0603 決定3 が禁じている
// 状態そのものである。
func applyOrCheckWorkflows(dir string, sets map[string][]string, dryRun bool, out io.Writer) error {
	paths, err := workflowFiles(dir)
	if err != nil {
		return err
	}

	seen := make(map[string]bool, len(sets))
	drift := make([]string, 0, len(sets))

	for _, path := range paths {
		name := filepath.Base(path)

		data, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			return xerrors.Wrap(err, path)
		}
		lines := strings.Split(string(data), "\n")

		hasBranches, hasSet, err := workflowTargets(lines)
		if err != nil {
			return xerrors.Wrap(err, name)
		}

		if literal := unmanagedLiteral(lines); literal != "" {
			return xerrors.Wrap(errUnmanaged, name+" が "+literal+" をブランチ名として直接書いています")
		}

		set, mapped := sets[name]
		if !mapped {
			if hasBranches || hasSet {
				return xerrors.Wrap(errUnmanaged, name+"（on.push.branches またはブランチ集合の式を持っています）")
			}

			continue
		}

		if !hasBranches && !hasSet {
			return xerrors.Wrap(errOrphan, name)
		}
		seen[name] = true

		after, err := rewriteWorkflow(lines, set)
		if err != nil {
			return xerrors.Wrap(err, name)
		}
		if after == string(data) {
			continue
		}

		if dryRun {
			drift = append(drift, name)

			continue
		}
		if err := os.WriteFile(path, []byte(after), filePerm); err != nil { //nolint:gosec // path は dir 直下の走査結果であり、外部入力は通らない
			return xerrors.Wrap(err, path)
		}
	}

	if err := requireAllSeen(seen, sets); err != nil {
		return err
	}

	if len(drift) > 0 {
		fmt.Fprintf(out, "❌ workflow が宣言からずれています: %s（make branches-apply で反映）\n", strings.Join(drift, ", "))

		return errDrift
	}
	fmt.Fprintf(out, "✅ branches: workflow %d 件の起動条件が宣言と一致しています\n", len(sets))

	return nil
}

// workflowFiles は走査対象の workflow を返します。**拡張子は両方を見る** ——
// 片方だけを見ると、もう片方で書かれた workflow が走査から黙って外れる。
func workflowFiles(dir string) ([]string, error) {
	var paths []string
	for _, ext := range []string{"*.yaml", "*.yml"} {
		found, err := filepath.Glob(filepath.Join(dir, ext))
		if err != nil {
			return nil, xerrors.Wrap(err, dir)
		}
		paths = append(paths, found...)
	}

	// **0件は成功にしない。** 走査対象を失った検査は、合格ではなく検査していない状態である
	// （ADR-0702 決定13）。
	if len(paths) == 0 {
		return nil, xerrors.Wrap(errShape, dir+" に workflow が1件もありません")
	}
	sort.Strings(paths)

	return paths, nil
}

// requireAllSeen は、宣言が指す workflow がすべて実在したことを確かめます。
func requireAllSeen(seen map[string]bool, sets map[string][]string) error {
	missing := make([]string, 0, len(sets))
	for name := range sets {
		if !seen[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	sort.Strings(missing)

	return xerrors.Wrap(errOrphan, strings.Join(missing, ", "))
}

// expressionLines は、式として読んでよい行を返します。
//
// **ブロックスカラーの中身を外す。** `run:` のスクリプトは文字列であって式ではなく、そこに
// 現れた fromJSON を書き換えれば、走るはずのコマンドが変わる。除外の判定を自前で持つと、
// 同じ workflow が経路によって違う判定を受ける（scripts/lib/yamlblock）。
func expressionLines(lines []string) []int {
	content := yamlblock.ContentLines(strings.Join(lines, "\n"))

	out := make([]int, 0, len(lines))
	for i := range lines {
		if !content[i+1] {
			out = append(out, i)
		}
	}

	return out
}

// workflowTargets は、その workflow が持つ生成対象の有無を返します。
func workflowTargets(lines []string) (bool, bool, error) {
	_, _, _, hasBranches, err := branchesBlock(lines)
	if err != nil {
		return false, false, err
	}

	hasSet := false
	for _, i := range expressionLines(lines) {
		if branchRefRe.MatchString(lines[i]) && branchSetRe.MatchString(lines[i]) {
			hasSet = true

			break
		}
	}

	return hasBranches, hasSet, nil
}

// rewriteWorkflow は、on.push.branches とブランチ集合の式を宣言から組み直します。
func rewriteWorkflow(lines []string, set []string) (string, error) {
	// **宣言が空なら生成しない。** 起動条件が空の workflow は push で一度も走らず、
	// それでいて check は緑を返す（ADR-0702 決定13）。
	if len(set) == 0 {
		return "", xerrors.Wrap(errShape, "宣言の集合が0件です")
	}

	out := slices.Clone(lines)

	first, last, indent, found, err := branchesBlock(out)
	if err != nil {
		return "", err
	}
	if found {
		items := make([]string, 0, len(set))
		for _, v := range set {
			items = append(items, indent+"- "+yamlScalar(v))
		}
		out = slices.Concat(out[:first], items, out[last:])
	}

	quoted := make([]string, 0, len(set))
	for _, v := range set {
		quoted = append(quoted, strconv.Quote(v))
	}
	want := "[" + strings.Join(quoted, ", ") + "]"

	for _, i := range expressionLines(out) {
		l := out[i]
		if !branchRefRe.MatchString(l) {
			continue
		}
		if m := branchSetRe.FindStringSubmatchIndex(l); m != nil {
			out[i] = l[:m[2]] + want + l[m[3]:]
		}
	}

	return strings.Join(out, "\n"), nil
}

// branchesBlock は on.push.branches の項目が占める行の区間と、その字下げを返します。
//
// **YAML として読み直さない。** 往復させるとコメントも引用の仕方も並びも失われ、生成器が
// 触るべきでない箇所まで書き換わる。行で扱い、構造は字下げの深さで辿る。
func branchesBlock(lines []string) (int, int, string, bool, error) {
	on := -1
	for i, l := range lines {
		if onKeyRe.MatchString(l) {
			on = i

			break
		}
	}
	if on < 0 {
		return 0, 0, "", false, nil
	}

	onEnd := blockEnd(lines, on+1, len(lines), indentWidth(lines[on]))

	push := findKey(lines, on+1, onEnd, "push")
	if push < 0 {
		return 0, 0, "", false, nil
	}

	pushEnd := blockEnd(lines, push+1, onEnd, indentWidth(lines[push]))

	br := findKey(lines, push+1, pushEnd, "branches")
	if br < 0 {
		return 0, 0, "", false, nil
	}

	brEnd := blockEnd(lines, br+1, pushEnd, indentWidth(lines[br]))

	// 末尾の空行は区間に属さない。ブロックの直後でファイルが終わると、末尾の改行が
	// 空文字列の行として残り、項目以外の行として落ちる。
	for brEnd > br+1 && strings.TrimSpace(lines[brEnd-1]) == "" {
		brEnd--
	}

	// **項目以外の行を読み飛ばさない。** 読み飛ばすと、その行だけが宣言の外に取り残される。
	for i := br + 1; i < brEnd; i++ {
		if !itemRe.MatchString(lines[i]) {
			return 0, 0, "", false, xerrors.Wrap(errShape,
				"on.push.branches に項目以外の行があります: "+strings.TrimSpace(lines[i]))
		}
	}
	if br+1 >= brEnd {
		return 0, 0, "", false, xerrors.Wrap(errShape, "on.push.branches が空です")
	}

	return br + 1, brEnd, strings.Repeat(" ", indentWidth(lines[br+1])), true, nil
}

// findKey は、区間の中で最も浅い階層にある値なしキーのうち、name の行番号を返します。
//
// **深さは区間全体の最小値で決める。** 最初に見つけたキーの深さを採ると、区間が入れ子の
// 途中から始まっているとき、より深い階層のキーを親の直下と取り違える。
func findKey(lines []string, from, to int, name string) int {
	depth := -1
	for i := from; i < to; i++ {
		if m := blockKeyRe.FindStringSubmatch(lines[i]); m != nil {
			if depth < 0 || len(m[1]) < depth {
				depth = len(m[1])
			}
		}
	}
	if depth < 0 {
		return -1
	}

	for i := from; i < to; i++ {
		m := blockKeyRe.FindStringSubmatch(lines[i])
		if m != nil && len(m[1]) == depth && m[2] == name {
			return i
		}
	}

	return -1
}

// blockEnd は、字下げが parent 以下へ戻る最初の行番号を返します。空行とコメントは
// 区間を終わらせない —— そこで切ると、間に注記を挟んだ宣言が途中で打ち切られる。
func blockEnd(lines []string, from, to, parent int) int {
	for i := from; i < to; i++ {
		t := strings.TrimSpace(lines[i])
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		if indentWidth(lines[i]) <= parent {
			return i
		}
	}

	return to
}

// indentWidth は行頭の空白の数を返します。
func indentWidth(line string) int {
	return len(line) - len(strings.TrimLeft(line, " "))
}

// yamlScalar は、素のまま書くと YAML が別の意味に取りうる値を引用します。
func yamlScalar(v string) string {
	if yamlQuoteRe.MatchString(v) {
		return "'" + v + "'"
	}

	return v
}

// unmanagedLiteral は、ブランチ名と突き合わせる式が宣言の値を直接書いている箇所を返します。
//
// **生成できる形（fromJSON の配列）は対象にしない** —— そちらは rewriteWorkflow が揃える。
// ここが捕まえるのは `github.base_ref == 'production'` のように、生成では追随できない形で
// 書かれたものである。見つけたら落とす。書き換えられない以上、残せば必ずずれる。
func unmanagedLiteral(lines []string) string {
	for _, i := range expressionLines(lines) {
		l := lines[i]
		if !branchRefRe.MatchString(l) || branchSetRe.MatchString(l) {
			continue
		}
		for _, v := range declaredLiterals() {
			if strings.Contains(l, "'"+v+"'") || strings.Contains(l, `"`+v+`"`) {
				return v
			}
		}
	}

	return ""
}

// declaredLiterals は、宣言が持つブランチ名（glob を除く）を重複なく返します。
func declaredLiterals() []string {
	out := make([]string, 0, len(branches.Protected))
	for _, set := range [][]string{branches.Deploy, branches.Protected, branches.GatePush, branches.ReleasePush} {
		for _, v := range set {
			if strings.ContainsAny(v, "*?") || slices.Contains(out, v) {
				continue
			}
			out = append(out, v)
		}
	}

	return out
}

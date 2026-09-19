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

	"gopkg.in/yaml.v3"

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
	errUsage     = xerrors.New("usage: branches <apply|check>")
	errDrift     = xerrors.New("生成先が宣言からずれています")
	errShape     = xerrors.New("生成先の構造が想定と異なります")
	errUnmanaged = xerrors.New("宣言の対象外の workflow がブランチのパターンを持っています")
	errOrphan    = xerrors.New("宣言が指す workflow に生成対象がありません")
	errNotation  = xerrors.New("workflow の記法を解釈できません")
)

// branchFilterKeys は on.push の下でブランチを絞り込むキー。**生成できるのは branches だけ**で、
// もう一方は見つけた時点で落とす —— 書き換えられないものを残せば、必ずずれる。
var branchFilterKeys = []string{"branches", "branches-ignore"}

// workflowSets は、push 側の起動条件を生成する先と、その集合。
// ここに無い workflow がパターンを持つときどうなるかは applyOrCheckWorkflows。
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

// yamlReserved は、YAML 1.1 が文字列以外へ解決する語（小文字で引く）。
var yamlReserved = map[string]bool{
	"y": true, "yes": true, "n": true, "no": true,
	"true": true, "false": true, "on": true, "off": true,
	"null": true, "~": true,
}

// main は判断を持たず run へ委譲します（entry と判断の分離は scripts/README.md の Test Strategy）。
func main() {
	log.SetFlags(0)

	if err := run(os.Args[1:], ".", os.Stdout); err != nil {
		log.Fatalf("%v", err)
	}
}

// run は、サブコマンドを解釈して固定処理へ振り分けます。
//
// root を引数で受けるのは、分岐をテストから到達可能にするためである（scripts/README.md の
// Test Strategy）。作業ディレクトリは不純な依存であり、ここへ焼き込むと `apply` と `check` の
// 対応そのものを誰も確かめられなくなる。
func run(args []string, root string, out io.Writer) error {
	if len(args) != 1 {
		return errUsage
	}

	switch args[0] {
	case "apply":
		return applyAll(root, workflowSets, false, out)
	case "check":
		return applyAll(root, workflowSets, true, out)
	default:
		return xerrors.Wrap(errUsage, "unknown subcommand: "+args[0])
	}
}

// plan は、書き換えが要る生成先とその内容。空なら宣言と一致している。
type plan map[string]string

// applyAll は、2つの生成先を宣言へ揃えます。
//
// **検証が全部終わるまで1バイトも書かない。** ファイルごとに検証と書き込みを交互にすると、
// 途中で落ちたとき前半だけが書き換わった状態が残り、呼び出し側には「失敗した」としか
// 見えない。何が適用されたのかは、誰にも分からなくなる。計画を組んでから書く。
//
// ずれも両方を見てから返す。片方で打ち切ると、直して再実行するまでもう片方のずれが
// 見えず、「直した」と「まだ残っている」が交互に現れる。
func applyAll(root string, sets map[string][]string, dryRun bool, out io.Writer) error {
	jsonPlan, err := planProtection(filepath.Join(root, protectionFile))
	if err != nil {
		return err
	}

	wfPlan, err := planWorkflows(filepath.Join(root, workflowDir), sets)
	if err != nil {
		return err
	}

	names := planNames(jsonPlan, wfPlan)
	if len(names) == 0 {
		fmt.Fprintf(out, "✅ branches: 保護対象 %d 件と workflow %d 件が宣言と一致しています\n",
			len(branches.Protected), len(sets))

		return nil
	}

	if dryRun {
		fmt.Fprintf(out, "❌ 生成先が宣言からずれています: %s（make branches-apply で反映）\n",
			strings.Join(names, ", "))

		return errDrift
	}

	if err := writePlan(jsonPlan, wfPlan); err != nil {
		return err
	}
	fmt.Fprintf(out, "✅ branches-apply: %s へ反映しました\n", strings.Join(names, ", "))

	return nil
}

// planProtection は保護設定の書き換えを計画します。
func planProtection(path string) (plan, error) {
	before, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, xerrors.Wrap(err, path)
	}

	after, err := rewrite(before, branches.Protected)
	if err != nil {
		return nil, xerrors.Wrap(err, path)
	}

	if string(before) == string(after) {
		return plan{}, nil
	}

	return plan{path: string(after)}, nil
}

// planNames は計画が触れるファイルの名前を、並びを決めて返します。
func planNames(plans ...plan) []string {
	names := []string{}
	for _, p := range plans {
		for path := range p {
			names = append(names, filepath.Base(path))
		}
	}
	sort.Strings(names)

	return names
}

// writePlan は計画をファイルへ書き出します。**ここに検証は無い** —— 検証はすべて計画の側で
// 終わっている。
//
// **全部を一時ファイルへ書き切ってから差し替える。** 素直に上書きしていくと、途中の1件が
// 失敗したとき前半だけが新しい内容になった状態が残る。計画の側で検証を終えても、書き込みの
// 側で同じ形の部分適用が起きる。
//
// 差し替え（rename）そのものは1件ずつで、複数ファイルを不可分に入れ替える手段はファイル
// システムに無い。ただし同じディレクトリへ置いた一時ファイルからの rename は、書き込みが
// 済んだ後の操作なので、失敗する余地がほとんど残っていない。
func writePlan(plans ...plan) error {
	type staged struct{ tmp, final string }

	var pending []staged
	defer func() {
		for _, p := range pending {
			_ = os.Remove(p.tmp)
		}
	}()

	for _, p := range plans {
		for _, path := range sortedPaths(p) {
			tmp, err := stage(path, p[path])
			if err != nil {
				return err
			}
			pending = append(pending, staged{tmp: tmp, final: path})
		}
	}

	for _, p := range pending {
		if err := os.Rename(p.tmp, p.final); err != nil {
			return xerrors.Wrap(err, p.final)
		}
	}
	pending = nil

	return nil
}

// sortedPaths は計画の書き出し先を、並びを決めて返します。
func sortedPaths(p plan) []string {
	paths := make([]string, 0, len(p))
	for path := range p {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	return paths
}

// stage は内容を差し替え先と同じディレクトリの一時ファイルへ書き、その名前を返します。
func stage(path, content string) (string, error) {
	// 差し替え先がディレクトリなら rename の段で落ちる。**そこまで進めない** ——
	// 進めると、先に差し替えた分だけが新しい状態で残る。
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return "", xerrors.Wrap(errShape, path+" はディレクトリです")
	}

	// path は package 定数から組んだ経路か、その直下の走査結果である。
	// 撤回条件: 経路が root 以外から決まる形になったとき。
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp") //nolint:gosec // 上のコメントを参照
	if err != nil {
		return "", xerrors.Wrap(err, path)
	}
	defer func() { _ = f.Close() }()

	if _, err := f.WriteString(content); err != nil {
		return "", xerrors.Wrap(err, path)
	}
	if err := f.Chmod(filePerm); err != nil {
		return "", xerrors.Wrap(err, path)
	}

	return f.Name(), nil
}

// rewrite は保護設定の conditions.ref_name.include を宣言から組み直します。まず JSON として
// 検証し、置換を include 配列の区間だけに限る理由は scripts/README.md の branches/ の行。
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
		if errors.Is(err, errShape) {
			return 0, 0, err
		}

		return 0, 0, xerrors.Wrap(errShape, strings.Join(includePath, ".")+" を辿れません")
	}
	if !found {
		return 0, 0, xerrors.Wrap(errShape, strings.Join(includePath, ".")+" がありません")
	}

	return start, end, nil
}

// findArray は、開き波括弧を読み終えた object の中で want の経路を辿り、
// 行き着いた配列のバイト区間を返します。**見つけた後も同じ階層を読み切る** —— 途中で
// 打ち切ると、後ろに現れた同名のキーを見ないまま返す。
//
// **同じ階層にキーが2度現れたら落とす。** JSON はそれを許すが、構造の検査（map へ復号 ——
// 後が勝つ）と区間の特定（トークン走査 —— 先が勝つ）で別の値を見ることになり、検査した方とは
// 別の配列を書き換える。しかも成功で返る。
func findArray(dec *json.Decoder, want []string) (int, int, bool, error) {
	seen := map[string]bool{}
	start, end, found := 0, 0, false

	for {
		tok, err := dec.Token()
		if err != nil {
			return 0, 0, false, err
		}
		if d, ok := tok.(json.Delim); ok && d == '}' {
			return start, end, found, nil
		}

		key, _ := tok.(string)
		if seen[key] {
			return 0, 0, false, xerrors.Wrap(errShape, "同じ階層にキーが 2 度現れます: "+key)
		}
		seen[key] = true

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
				if err != nil {
					return 0, 0, false, err
				}
				if f {
					start, end, found = s, e, true
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
				start, end, found = at, int(dec.InputOffset()), true

				continue
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

// planWorkflows は、workflow のブランチのパターンの書き換えを計画します。
// 走査を全 workflow に掛ける理由は scripts/README.md の branches/ の行。
func planWorkflows(dir string, sets map[string][]string) (plan, error) {
	paths, err := workflowFiles(dir)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool, len(sets))
	changes := plan{}

	for _, path := range paths {
		name := filepath.Base(path)

		data, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			return nil, xerrors.Wrap(err, path)
		}
		lines := strings.Split(string(data), "\n")

		hasBranches, hasSet, err := workflowTargets(data, lines)
		if err != nil {
			return nil, xerrors.Wrap(err, name)
		}

		if expr := unmanagedExpression(lines); expr != "" {
			return nil, xerrors.Wrap(errUnmanaged, name+" がブランチ名を式へ直接書いています: "+expr)
		}

		set, mapped := sets[name]
		if !mapped {
			if hasBranches || hasSet {
				return nil, xerrors.Wrap(errUnmanaged, name+"（on.push.branches またはブランチ集合の式を持っています）")
			}

			continue
		}

		if !hasBranches && !hasSet {
			return nil, xerrors.Wrap(errOrphan, name)
		}
		seen[name] = true

		after, err := rewriteWorkflow(lines, set)
		if err != nil {
			return nil, xerrors.Wrap(err, name)
		}
		if after != string(data) {
			changes[path] = after
		}
	}

	if err := requireAllSeen(seen, sets); err != nil {
		return nil, err
	}

	return changes, nil
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
//
// **2つの独立した経路で見て、食い違えば落とす**（ADR-0702 決定15）。行の走査は正規表現で
// 構造を近似しており、一致しない形 —— フロースタイル、行末コメント、別名のキー —— を
// すべて「無い」へ畳む。畳んだ先が成功だと、宣言の外へ直書きされたパターンが誰の目にも
// 触れないまま緑になる。それはこの道具が防ぐと名乗っている当のものである（決定14）。
func workflowTargets(data []byte, lines []string) (bool, bool, error) {
	_, _, _, lineFound, err := branchesBlock(lines)
	if err != nil {
		return false, false, err
	}

	yamlFound, key, flow, err := pushBranchFilter(data)
	if err != nil {
		return false, false, err
	}

	switch {
	case yamlFound && key != branchFilterKeys[0]:
		return false, false, xerrors.Wrap(errUnmanaged, "on.push."+key+" は生成できません")
	case yamlFound && flow:
		return false, false, xerrors.Wrap(errNotation,
			"on.push.branches がフロースタイルです（- を並べるブロック形式で書くこと）")
	case yamlFound != lineFound:
		return false, false, xerrors.Wrap(errNotation,
			"on.push.branches を行として特定できません（キーの行末にコメントを置いていないか）")
	}

	hasSet := false
	for _, i := range expressionLines(lines) {
		if branchRefRe.MatchString(lines[i]) && branchSetRe.MatchString(lines[i]) {
			hasSet = true

			break
		}
	}

	return lineFound, hasSet, nil
}

// pushBranchFilter は、YAML として解釈した on.push のブランチ絞り込みを返します。
// 戻り値は、存在するか・そのキー名・フロースタイルか。
func pushBranchFilter(data []byte) (bool, string, bool, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return false, "", false, xerrors.Wrap(errShape, "YAML として読めません: "+err.Error())
	}

	// **空を通さない。** 空の workflow は GitHub が何も起動しない壊れた状態であり、
	// 「ブランチのパターンを持たない」と同じ扱いにしてよいものではない。
	if len(doc.Content) == 0 {
		return false, "", false, xerrors.Wrap(errShape, "空です")
	}
	if doc.Content[0].Kind != yaml.MappingNode {
		return false, "", false, xerrors.Wrap(errShape, "最上位が mapping ではありません")
	}

	push := mapValue(mapValue(doc.Content[0], "on"), "push")
	for _, k := range branchFilterKeys {
		if v := mapValue(push, k); v != nil {
			return true, k, v.Style&yaml.FlowStyle != 0, nil
		}
	}

	return false, "", false, nil
}

// mapValue は mapping から key に対応する値を返します。**キーは Node の生の文字列で比較する**
// —— on は YAML 1.1 では真偽値に解決されうるので、型で辿ると見失う。
func mapValue(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}

	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}

	return nil
}

// rewriteWorkflow は、on.push.branches とブランチ集合の式を宣言から組み直します。
func rewriteWorkflow(lines []string, set []string) (string, error) {
	// **宣言が空なら生成しない。** 起動条件が空の workflow は push で一度も走らず、
	// それでいて check は緑を返す（ADR-0702 決定13）。
	if len(set) == 0 {
		return "", xerrors.Wrap(errShape, "宣言の集合が0件です")
	}

	// **書けない値は書かない。** 生成先は YAML の引用スカラーと、単一引用符で囲む
	// GitHub Actions の式である。値が引用符や改行を含むと囲みが破れ、壊れた workflow を
	// 成功で書き込む。落とす側へ倒す。
	for _, v := range set {
		if v == "" || strings.ContainsAny(v, "'\"\n\r\t") {
			return "", xerrors.Wrap(errShape, "宣言に書き出せない値があります: "+strconv.Quote(v))
		}
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
// YAML として読み直さない理由は scripts/README.md の branches/ の行。
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
//
// 記号だけでは足りない。**YAML 1.1 は on / off / yes / no を真偽値に解決する**ので、
// そういう名前のブランチを素で書くと項目が文字列でなくなる。先頭の - と数字に見える形も同じ。
func yamlScalar(v string) string {
	if yamlQuoteRe.MatchString(v) || yamlReserved[strings.ToLower(v)] ||
		strings.HasPrefix(v, "-") || v[0] >= '0' && v[0] <= '9' {
		return "'" + v + "'"
	}

	return v
}

// unmanagedExpression は、ブランチ名と突き合わせる式が宣言の値を直接書いている行を返します。
// 生成できる形（fromJSON の配列）は rewriteWorkflow が揃えるので対象にしない。落とす理由は
// .github/workflows/README.md の「分岐のパターン」節。
//
// **値ではなく行を返す。** 1行に複数の文字列が並んでいると、どれが犯人かは判定できない
// （`github.ref_name == 'develop' && vars.STAGE == 'production'`）。当たった値だけを名指すと、
// 読んだ人は無関係な方を直しに行く。
func unmanagedExpression(lines []string) string {
	for _, i := range expressionLines(lines) {
		l := lines[i]
		if !branchRefRe.MatchString(l) || branchSetRe.MatchString(l) {
			continue
		}
		for _, v := range declaredLiterals() {
			if strings.Contains(l, "'"+v+"'") || strings.Contains(l, `"`+v+`"`) {
				return strings.TrimSpace(l)
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

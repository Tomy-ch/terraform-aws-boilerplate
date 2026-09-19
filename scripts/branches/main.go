// Package main は、分岐のパターンの単一宣言を保護設定へ反映・検証するツール。
//
//	apply   .github/settings/branch-protection.json の保護対象を宣言から組み直す
//	check   生成先が宣言からずれていないかを検査する（書き換えなし）
//
// 宣言は scripts/lib/branches が持つ（ADR-0603 決定1）。**生成できるのは保護設定だけである。**
// 他の読み手（base-branch / release / repo-setup）は同じパッケージを import するので、
// 生成も突合も要らない —— コンパイラが一致を保証する。
//
// ずれは静かに壊れる。保護対象から漏れたブランチは「保護されていない」ではなく
// 「保護されているつもり」になる。だから check を持ち、pre-commit と CI の両方で走らせる。
//
// **workflow の push 側の起動条件は生成しない。** そちらは required context を報告せず、
// 絞っても merge を塞がない（ADR-0603 決定1）。塞がないものを生成対象にすると、生成器が
// YAML の書式追随という終わりのない仕事を抱え、その取りこぼしが「検査が素通りする」形で出る。
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/branches"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const (
	protectionFile = ".github/settings/branch-protection.json"
	refPrefix      = "refs/heads/"
	filePerm       = 0o644
	jsonIndent     = "  "
)

var (
	// errUsage は、サブコマンドの与え方が誤っていることを表す。
	errUsage = xerrors.New("usage: branches <apply|check>")
	// errDrift は、生成先が宣言からずれていることを表す。
	errDrift = xerrors.New("保護設定が宣言からずれています")
	// errShape は、保護設定が期待する構造を持たないことを表す。
	errShape = xerrors.New("保護設定の構造が想定と異なります")
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
		return applyOrCheck(protectionFile, false, out)
	case "check":
		return applyOrCheck(protectionFile, true, out)
	default:
		return xerrors.Wrap(errUsage, "unknown subcommand: "+args[0])
	}
}

// applyOrCheck は保護設定を宣言へ揃えます。dryRun=true は書き換えず、ずれを報告して
// 非ゼロで終わります。
func applyOrCheck(path string, dryRun bool, out io.Writer) error {
	before, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return xerrors.Wrap(err, path)
	}

	after, err := rewrite(before)
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
func rewrite(content []byte) ([]byte, error) {
	var doc map[string]any
	if err := json.Unmarshal(content, &doc); err != nil {
		return nil, xerrors.Wrap(err, "JSON として読めません")
	}

	if err := requireRefName(doc); err != nil {
		return nil, err
	}

	start, end, err := includeSpan(content)
	if err != nil {
		return nil, err
	}

	indent := indentOf(content, start)
	items := make([]string, 0, len(branches.Protected))
	for _, p := range branches.Protected {
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

// includeSpan は include の値（配列）が占めるバイト区間を返します。
//
// 入力は JSON として検証済みなので、`[` から対応する `]` までを数えれば足ります。
// **行の走査で終端を探さない** —— インライン形式（`"include": []`）や整形の変化で、
// 無関係な配列の閉じ括弧へ着地する経路が開く。
func includeSpan(content []byte) (int, int, error) {
	key := []byte(`"include"`)
	at := bytes.Index(content, key)
	if at < 0 {
		return 0, 0, xerrors.Wrap(errShape, `"include" が見つかりません`)
	}

	start := bytes.IndexByte(content[at:], '[')
	if start < 0 {
		return 0, 0, xerrors.Wrap(errShape, `"include" の値が配列ではありません`)
	}
	start += at

	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(content); i++ {
		c := content[i]
		switch {
		case escaped:
			escaped = false
		case c == '\\' && inString:
			escaped = true
		case c == '"':
			inString = !inString
		case inString:
		case c == '[':
			depth++
		case c == ']':
			depth--
			if depth == 0 {
				return start, i + 1, nil
			}
		}
	}

	return 0, 0, xerrors.Wrap(errShape, `"include" の配列が閉じていません`)
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

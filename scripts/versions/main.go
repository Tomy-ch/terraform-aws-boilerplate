// Package main は、mise.toml が宣言する言語ランタイムの版を、それを書き写している箇所へ
// 反映・検証するツール。
//
//	apply   宣言どおりに書き換える
//	check   宣言からずれていないかを検査する（書き換えなし）
//
// **版の正本は mise.toml である**（ADR-0501 決定19）。Dockerfile の `FROM golang:` や
// go.mod の `go` ディレクティブは同じ事実の写しであり、写しは放っておくと古くなる。
//
// ずれの捕まりにくさがこの道具の存在理由である。`mise.toml` の go と Dockerfile の
// `FROM golang:` がずれても、**イメージのビルドは通ってしまう** —— 落ちるのは、ずれた版に
// 存在しない機能を使ったときだけである。go.mod の `go` ディレクティブに至っては、
// 低い版を書いてもビルドは通る。どちらも「ずれている」と「揃っている」が緑で区別できない。
package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const (
	miseFile = "mise.toml"
	filePerm = 0o644
)

var (
	// errUsage は、サブコマンドの与え方が誤っていることを表す。
	errUsage = xerrors.New("usage: versions <apply|check>")
	// errDrift は、写しが宣言からずれていることを表す。
	errDrift = xerrors.New("版の写しが mise.toml からずれています")
	// errShape は、宣言か写しが期待する形を持たないことを表す。
	errShape = xerrors.New("版の宣言または写しの形が想定と異なります")
)

var (
	// miseSectionRe は `[tools]` のような table の見出し。
	miseSectionRe = regexp.MustCompile(`^\[([^\]]+)\]`)
	// miseKeyRe は `go = "1.27.1"` のような代入。
	miseKeyRe = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_-]*)\s*=\s*"([^"]+)"`)
	// goDirectiveRe は go.mod の `go` ディレクティブ。行全体に錨を打つ。
	goDirectiveRe = regexp.MustCompile(`(?m)^(go )\d+(?:\.\d+){0,2}$`)
)

// dockerFromRe は Dockerfile の `FROM <image>:<version><suffix>` を捉える正規表現を返します。
//
// **コメント行を除く。** `[^#\n]*?` の `\n` を落とすと、Go の文字クラスは改行にも一致するので
// マッチが行をまたいで広がり、間の行ごと置換で消える。件数は変わらないので、件数のガードも
// 通り抜ける。
func dockerFromRe(image string) *regexp.Regexp {
	return regexp.MustCompile(`(?m)^[^#\n]*?(FROM\s+` + regexp.QuoteMeta(image) + `:)\d+(?:\.\d+){0,2}(-[\w.-]+)`)
}

// declared は mise.toml が宣言する版。
type declared struct {
	Go   string
	Node string
}

// rule は、1つのファイルの中で正規表現に一致した箇所を1つの版へ揃える単位。
type rule struct {
	// label は報告に出す名前。
	label string
	// file は root からの相対パス。
	file string
	re   *regexp.Regexp
	// version は宣言側の値。
	version string
	// count は期待する一致件数。**下限ではなく厳密な件数である** —— 写しが増えたことも
	// 減ったことも、どちらも宣言との対応が崩れた合図なので落とす（ADR-0702 決定15）。
	count int
}

// main は 1:1 テスト規約の対象外で分岐を検査できないため、判断は run に置きます。
func main() {
	log.SetFlags(0)

	if err := run(os.Args[1:], ".", os.Stdout); err != nil {
		log.Fatalf("%v", err)
	}
}

// run は、サブコマンドを解釈して固定処理へ振り分けます。root を引数で受けるのは、分岐を
// テストから到達可能にするためです（scripts/README.md の Test Strategy）。
func run(args []string, root string, out io.Writer) error {
	if len(args) != 1 {
		return errUsage
	}

	switch args[0] {
	case "apply":
		return applyAll(root, false, out)
	case "check":
		return applyAll(root, true, out)
	default:
		return xerrors.Wrap(errUsage, "unknown subcommand: "+args[0])
	}
}

// applyAll は写しを宣言へ揃えます。
//
// **全部の rule を検証し終えるまで1バイトも書かない。** 途中で落ちると、前半だけが新しい版に
// なった状態が残り、呼び出し側には「失敗した」としか見えない。
func applyAll(root string, dryRun bool, out io.Writer) error {
	v, err := parseMise(filepath.Join(root, miseFile))
	if err != nil {
		return err
	}

	changes, err := plan(rules(v), root)
	if err != nil {
		return err
	}

	names := planNames(changes)
	if len(names) == 0 {
		fmt.Fprintf(out, "✅ versions: go %s / node %s の写しが宣言と一致しています\n", v.Go, v.Node)

		return nil
	}

	if dryRun {
		fmt.Fprintf(out, "❌ 版の写しが宣言からずれています: %s（make versions-apply で反映）\n",
			strings.Join(names, ", "))

		return errDrift
	}

	for _, path := range sortedPaths(changes) {
		// path は root と package 定数から組む。外部入力は通らない。
		// 撤回条件: 経路が root 以外から決まる形になったとき。
		if err := os.WriteFile(path, []byte(changes[path]), filePerm); err != nil { //nolint:gosec // 上のコメントを参照
			return xerrors.Wrap(err, path)
		}
	}
	fmt.Fprintf(out, "✅ versions-apply: %s へ反映しました\n", strings.Join(names, ", "))

	return nil
}

// rules は、宣言から写しへの対応を返します。**ここが対応表の唯一の在処である。**
func rules(v declared) []rule {
	const toolsDockerfile = "docker/tools/Dockerfile"

	return []rule{
		{label: "golang イメージ", file: toolsDockerfile, re: dockerFromRe("golang"), version: v.Go, count: 2},
		{label: "node イメージ", file: toolsDockerfile, re: dockerFromRe("node"), version: v.Node, count: 1},
		{label: "go ディレクティブ", file: "scripts/go.mod", re: goDirectiveRe, version: v.Go, count: 1},
	}
}

// plan は、書き換えが要るファイルとその内容を返します。空なら宣言と一致しています。
//
// 同じファイルに複数の rule が掛かるので、途中の状態を持ち回って順に当てます。
func plan(rs []rule, root string) (map[string]string, error) {
	// **rule が0件なら何も検査していない。** 表が空になったことを「一致」と報告しない
	// （ADR-0702 決定13）。
	if len(rs) == 0 {
		return nil, xerrors.Wrap(errShape, "対応表が空です")
	}

	current := map[string]string{}
	original := map[string]string{}

	for _, r := range rs {
		path := filepath.Join(root, r.file)

		if _, ok := current[path]; !ok {
			data, err := os.ReadFile(filepath.Clean(path))
			if err != nil {
				return nil, xerrors.Wrap(err, r.file)
			}
			current[path] = string(data)
			original[path] = string(data)
		}

		next, err := applyRule(r, current[path])
		if err != nil {
			return nil, err
		}
		current[path] = next
	}

	changes := map[string]string{}
	for path, after := range current {
		if after != original[path] {
			changes[path] = after
		}
	}

	return changes, nil
}

// applyRule は1つの rule を内容へ当てます。一致件数が期待と違えばエラーにします。
func applyRule(r rule, content string) (string, error) {
	// **宣言が空なら書き換えない。** 空の版で書き換えると `FROM golang:-bookworm` のような
	// 壊れた行を作り、しかも件数のガードは通る。
	if r.version == "" {
		return "", xerrors.Wrap(errShape, r.label+": 宣言側の版が空です")
	}

	found := len(r.re.FindAllString(content, -1))
	if found != r.count {
		return "", xerrors.Wrap(errShape,
			fmt.Sprintf("%s (%s): 一致が %d 件、期待は %d 件", r.label, r.file, found, r.count))
	}

	return r.re.ReplaceAllString(content, "${1}"+r.version+"${2}"), nil
}

// parseMise は mise.toml の [tools] が宣言する版を返します。**用途特化の最小の読み取りで、
// TOML の仕様には準拠しない** —— 見るのは `[tools]` 直下の `go` と `node` だけである。
func parseMise(path string) (declared, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return declared{}, xerrors.Wrap(err, path)
	}

	var v declared
	section := ""

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if m := miseSectionRe.FindStringSubmatch(line); m != nil {
			section = m[1]

			continue
		}
		if section != "tools" {
			continue
		}
		if m := miseKeyRe.FindStringSubmatch(line); m != nil {
			switch m[1] {
			case "go":
				v.Go = m[2]
			case "node":
				v.Node = m[2]
			}
		}
	}

	// **宣言が欠けていたらそこで止める。** 空のまま進むと、写しを空の版で書き潰す。
	var missing []string
	if v.Go == "" {
		missing = append(missing, "go")
	}
	if v.Node == "" {
		missing = append(missing, "node")
	}
	if len(missing) > 0 {
		return declared{}, xerrors.Wrap(errShape, miseFile+" の [tools] に "+strings.Join(missing, ", ")+" がありません")
	}

	return v, nil
}

// planNames は書き換え先の名前を、並びを決めて返します。
func planNames(changes map[string]string) []string {
	names := make([]string, 0, len(changes))
	for path := range changes {
		names = append(names, filepath.Base(path))
	}
	sort.Strings(names)

	return names
}

// sortedPaths は書き換え先を、並びを決めて返します。
func sortedPaths(changes map[string]string) []string {
	paths := make([]string, 0, len(changes))
	for path := range changes {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	return paths
}

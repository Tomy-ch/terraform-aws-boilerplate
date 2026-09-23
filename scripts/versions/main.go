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

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/atomicwrite"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const (
	miseFile = "mise.toml"
	filePerm = 0o644

	// terraformTool / awsCLITool は mise.toml の [tools] のキーであり、`.makefiles/host-tools.mk`
	// が焼き込む名前でもある。同じ文字列が宣言側と写し側の両方の錨になる。
	terraformTool = "aqua:hashicorp/terraform"
	awsCLITool    = "aqua:aws/aws-cli"
)

var (
	errUsage = xerrors.New("usage: versions <apply|check>")
	errDrift = xerrors.New("版の写しが mise.toml からずれています")
	errShape = xerrors.New("版の宣言または写しの形が想定と異なります")
)

// versionPattern は版の桁の並び。**1〜3桁を許す** —— `go = "1"` も `= "1.27"` も
// `= "1.27.1"` も宣言として現れる。捕獲群を持たないので、これを使う正規表現の群番号は
// この式の有無で動かない。**その不変条件は Test_versionPattern が固定する** —— コメントだけで
// 守ると、ここへ群を1つ足した日に3つの正規表現が同時にずれ、置換が err == nil のまま壊れる。
const versionPattern = `\d+(?:\.\d+){0,2}`

// bareKeyPattern は TOML の裸キーに使える文字。仕様が許すのは ASCII 英数字と `_` `-` だけで、
// `:` や `/` を含む backend 付きのキーは引用符付きでしか書けない。**数字始まりも仕様では
// 正しい**（`1password-cli` のような名前が在り得る）。
//
// **同じ [tools] を scripts/tool-cooldown も読む。** 集合がずれると、一方だけが読める宣言が
// 生まれ、もう一方は黙ってその道具を検査の対象から外す。
const bareKeyPattern = `[A-Za-z0-9_-]+`

var (
	// miseSectionRe は `[tools]` のような table の見出し。
	miseSectionRe = regexp.MustCompile(`^\[([^\]]+)\]`)
	// miseKeyRe は `go = "1.27.1"` と `"aqua:aws/aws-cli" = "2.36.40"` の両方を捉える代入。
	// 裸のキーが許す文字は bareKeyPattern が持つ。
	//
	// **引用符は対で要求する。** 前後を独立した `"?` にすると `"go = "1.27.1"` のような壊れた行も
	// 読めてしまい、手編集で壊れた宣言から拾った値のまま写しを書き換える。第1群が引用符付きの
	// キー、第2群が裸のキーで、どちらか一方だけが埋まる。
	miseKeyRe = regexp.MustCompile(`^(?:"([^"]+)"|(` + bareKeyPattern + `))\s*=\s*"([^"]+)"`)
	// goDirectiveRe は go.mod の `go` ディレクティブ。行全体に錨を打つ。
	goDirectiveRe = regexp.MustCompile(`(?m)^(go )` + versionPattern + `$`)
	// versionRe は、宣言側から読んだ版がその形をしているかを見る。**一致に使う形と置換に
	// 入れる値が別物だと、`1.2.3$1` のような値がそのまま写し先へ埋まる** —— Go の置換文字列は
	// `$` を後方参照として解釈し、写し先が Makefile なら `$(shell ...)` が実行され得る。
	versionRe = regexp.MustCompile(`^` + versionPattern + `$`)
)

// dockerFromRe は Dockerfile の `FROM <image>:<version><suffix>` を捉える正規表現を返します。
//
// **コメント行を除く。** `[^#\n]*?` の `\n` を落とすと、Go の文字クラスは改行にも一致するので
// マッチが行をまたいで広がり、間の行ごと置換で消える。件数は変わらないので、件数のガードも
// 通り抜ける。
func dockerFromRe(image string) *regexp.Regexp {
	return regexp.MustCompile(`(?m)^[^#\n]*?(FROM\s+` + regexp.QuoteMeta(image) + `:)` + versionPattern + `(-[\w.-]+)`)
}

// miseInstallRe は `.makefiles/` が焼き込んだ `mise install "<名前>@<版>"` を捉える正規表現を
// 返します。**閉じ引用符を第2群に取るのは、版を行末に置かないためである** —— 行末が錨だと、
// 末尾の空白や継続行の有無で一致が変わる。
//
// `[^#\n]*?` でコメント行を除く。`\n` を落とすと行をまたいで広がる。**前置きは第1群の中に入れる**
// —— レシピ行は `\t@` で始まり、群の外で消費すると置換でその2文字ごと消える。
func miseInstallRe(tool string) *regexp.Regexp {
	return regexp.MustCompile(`(?m)^([^#\n]*?mise install "` + regexp.QuoteMeta(tool) + `@)` + versionPattern + `(")`)
}

type declared struct {
	Go        string
	Node      string
	Terraform string
	AWSCLI    string
}

// rule は、1つのファイルの中で正規表現に一致した箇所を1つの版へ揃える単位。
type rule struct {
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

// main は判断を持ちません。分岐は run に置きます（scripts/README.md の Test Strategy）。
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
		fmt.Fprintf(out, "✅ versions: go %s / node %s / terraform %s / aws-cli %s の写しが宣言と一致しています\n",
			v.Go, v.Node, v.Terraform, v.AWSCLI)

		return nil
	}

	if dryRun {
		fmt.Fprintf(out, "❌ 版の写しが宣言からずれています: %s（make versions-apply で反映）\n",
			strings.Join(names, ", "))

		return errDrift
	}

	if err := atomicwrite.Apply(changes, filePerm); err != nil {
		return err
	}
	fmt.Fprintf(out, "✅ versions-apply: %s へ反映しました\n", strings.Join(names, ", "))

	return nil
}

// rules は、宣言から写しへの対応を返します。**写し先の対応表はここが唯一の在処である。**
// ただし mise.toml 側で何を読むかは parseMise が別に持つ —— 道具を足すときは、declared の
// フィールド・parseMise の分岐・欠落の検査・この表の4つを揃える必要がある。
func rules(v declared) []rule {
	const (
		toolsDockerfile = "docker/tools/Dockerfile"
		hostToolsMk     = ".makefiles/host-tools.mk"
	)

	return []rule{
		{label: "golang イメージ", file: toolsDockerfile, re: dockerFromRe("golang"), version: v.Go, count: 2},
		{label: "node イメージ", file: toolsDockerfile, re: dockerFromRe("node"), version: v.Node, count: 1},
		{label: "go ディレクティブ", file: "scripts/go.mod", re: goDirectiveRe, version: v.Go, count: 1},
		{label: "terraform の導入", file: hostToolsMk, re: miseInstallRe(terraformTool), version: v.Terraform, count: 1},
		{label: "AWS CLI の導入", file: hostToolsMk, re: miseInstallRe(awsCLITool), version: v.AWSCLI, count: 1},
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

	// **書き込む値も版の形に照らす。** 一致は versionPattern で見るのに置換は無検査、という
	// 非対称を塞ぐ。宣言は手で編集されるので、余分な空白も `$` も現実に入り得る。
	if !versionRe.MatchString(r.version) {
		return "", xerrors.Wrap(errShape, r.label+": 宣言側の版が版の形をしていません: "+r.version)
	}

	// **置換は前置きの群 ${1} を要する。** 群が無い正規表現を渡されると、Go は参照を
	// エラーにせず空文字へ落とすので、前置きを失った内容が err == nil のまま書き出される。
	// 後置きの ${2} は goDirectiveRe のように持たない rule もあり、そちらは空でよい。
	if r.re.NumSubexp() < 1 {
		return "", xerrors.Wrap(errShape, r.label+": 写し先の正規表現が前置きの捕獲群を持ちません")
	}

	found := len(r.re.FindAllString(content, -1))
	if found != r.count {
		return "", xerrors.Wrap(errShape,
			fmt.Sprintf("%s (%s): 一致が %d 件、期待は %d 件", r.label, r.file, found, r.count))
	}

	return r.re.ReplaceAllString(content, "${1}"+r.version+"${2}"), nil
}

// parseMise は mise.toml の [tools] が宣言する版を返します。**用途特化の最小の読み取りで、
// TOML の仕様には準拠しない** —— 見るのは `[tools]` 直下の、rules が写し先を持つキーだけである。
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
			key := m[1]
			if key == "" {
				key = m[2]
			}

			switch key {
			case "go":
				v.Go = m[3]
			case "node":
				v.Node = m[3]
			case terraformTool:
				v.Terraform = m[3]
			case awsCLITool:
				v.AWSCLI = m[3]
			}
		}
	}

	// **宣言が欠けていたらそこで止める。** 空のまま進むと、写しを空の版で書き潰す。
	var missing []string
	for _, d := range []struct {
		name  string
		value string
	}{
		{"go", v.Go},
		{"node", v.Node},
		{terraformTool, v.Terraform},
		{awsCLITool, v.AWSCLI},
	} {
		if d.value == "" {
			missing = append(missing, d.name)
		}
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

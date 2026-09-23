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
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/misetoml"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const (
	miseFile = "mise.toml"
	filePerm = 0o644

	// terraformTool / awsCLITool は mise.toml の [tools] のキーであり、`.makefiles/host-tools.mk`
	// が焼き込む名前でもある。同じ文字列が宣言側と写し側の両方の錨になる。
	terraformTool = "aqua:hashicorp/terraform"
	awsCLITool    = "aqua:aws/aws-cli"

	// npm の道具は、宣言側のキーと写し側の名前が `npm:` の有無だけ違う。**片方から導く** ——
	// 2つ並べて書くと、パッケージ名を直した側だけが動く。
	markdownlintPkg  = "markdownlint-cli2"
	commitlintPkg    = "@commitlint/cli"
	markdownlintTool = "npm:" + markdownlintPkg
	commitlintTool   = "npm:" + commitlintPkg
)

var (
	errUsage = xerrors.New("usage: versions <apply|check>")
	errDrift = xerrors.New("版の写しが mise.toml からずれています")
	errShape = xerrors.New("版の宣言または写しの形が想定と異なります")
)

// versionPattern は版の桁の並び。**1〜3桁を許す** —— `go = "1"` も `= "1.27"` も
// `= "1.27.1"` も宣言として現れる。捕獲群を持たないので、これを使う正規表現の群番号は
// この式の有無で動かない —— 群を1つ足すと dockerFromRe と bakedRe の後置きが `${3}` へ
// ずれ、goDirectiveRe は版の桁を `${2}` として拾う。Go はどちらもエラーにしない。
// **その不変条件は Test_versionPattern が固定する。**
const versionPattern = `\d+(?:\.\d+){0,2}`

var (
	// goDirectiveRe は go.mod の `go` ディレクティブ。行全体に錨を打つ。
	goDirectiveRe = regexp.MustCompile(`(?m)^(go )` + versionPattern + `$`)
	// versionRe は、宣言側から読んだ版がその形をしているかを見る。検める理由は applyRule の
	// 当該チェックが持つ。
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

// bakedRe は `<前置き><錨><版><閉じ>` の形で焼き込まれた版を捉える正規表現を返します。
//
// **閉じを第2群に取るのは、版を行末に置かないためである** —— 行末が錨だと、末尾の空白や
// 継続行の有無で一致が変わる。
//
// `[^#\n]*?` でコメント行を除く。`\n` を落とすと Go の文字クラスは改行にも一致するので、
// マッチが行をまたいで広がり、間の行ごと置換で消える。件数は変わらないので件数のガードも
// 通り抜ける。**前置きは第1群の中に入れる** —— Makefile のレシピ行は `\t@` で、シェルの
// 継続行は空白で始まり、群の外で消費すると置換でその文字ごと消える。
func bakedRe(anchor, closing string) *regexp.Regexp {
	return regexp.MustCompile(
		`(?m)^([^#\n]*?` + regexp.QuoteMeta(anchor) + `)` + versionPattern + `(` + regexp.QuoteMeta(closing) + `)`)
}

// miseInstallRe は `.makefiles/` が焼き込んだ `mise install "<名前>@<版>"` を捉えます。
func miseInstallRe(tool string) *regexp.Regexp {
	return bakedRe(`mise install "`+tool+`@`, `"`)
}

// npmPkgRe は Dockerfile が `npm install -g` へ渡す `"<パッケージ>@<版>"` を捉えます。
func npmPkgRe(pkg string) *regexp.Regexp {
	return bakedRe(`"`+pkg+`@`, `"`)
}

// shellVarRe は Dockerfile のレシピが焼き込んだ `<名前>="<版>"` を捉えます。**ARG ではなく
// シェル変数にするのは、`--build-arg` で外から差し替えられないようにするためである** ——
// 差し替えられる値で照合する検査は、照合しているふりをしているだけになる。
func shellVarRe(name string) *regexp.Regexp {
	return bakedRe(name+`="`, `"`)
}

type declared struct {
	Go        string
	Node      string
	Terraform string
	AWSCLI    string
	// Markdownlint / Commitlint は npm エコシステムの道具で、Dockerfile の node 段が
	// `npm install -g` へ版付きで渡す。
	Markdownlint string
	Commitlint   string
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
	// 減ったことも、どちらも宣言との対応が崩れた合図なので落とす。
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
		// **照合した宣言をすべて名乗る。** 一部しか挙げないと、表から落ちた宣言があっても
		// 報告は同じ形で緑を返す。
		fmt.Fprintf(out,
			"✅ versions: go %s / node %s / terraform %s / aws-cli %s / markdownlint %s / commitlint %s の写しが宣言と一致しています\n",
			v.Go, v.Node, v.Terraform, v.AWSCLI, v.Markdownlint, v.Commitlint)

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
		// ベースイメージの実ランタイムと突き合わせる側の値（ADR-0503 決定3）。イメージの
		// タグ（上の2行）が合っていても、tag が指す中身がずれていればこちらが落とす。
		{label: "go の照合値", file: toolsDockerfile, re: shellVarRe("declared_go"), version: v.Go, count: 1},
		{label: "node の照合値", file: toolsDockerfile, re: shellVarRe("declared_node"), version: v.Node, count: 1},
		{label: "markdownlint の導入", file: toolsDockerfile, re: npmPkgRe(markdownlintPkg), version: v.Markdownlint, count: 1},
		{label: "commitlint の導入", file: toolsDockerfile, re: npmPkgRe(commitlintPkg), version: v.Commitlint, count: 1},
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
	// 非対称を塞ぐ —— 宣言は手で編集されるので、余分な空白も `$` も現実に入り得る。Go の置換
	// 文字列は `$` を後方参照として解釈するので、`1.2.3$1` のような値は別の群へ化けて写し先へ
	// 埋まり、写し先が Makefile なら `$(shell ...)` が実行され得る。
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

// parseMise は mise.toml の [tools] が宣言する版のうち、rules が写し先を持つものを返します。
//
// **[tools] の読み取りそのものは misetoml が持つ。** 同じ宣言を読む実装をリポジトリに2つ置くと、
// 一方だけが読める宣言が生まれる。ここが決めるのは、どのキーを要求するかだけである。
func parseMise(path string) (declared, error) {
	entries, err := misetoml.ParseFile(path)
	if err != nil {
		return declared{}, err
	}

	var v declared
	var missing []string
	for _, d := range []struct {
		name string
		dst  *string
	}{
		{"go", &v.Go},
		{"node", &v.Node},
		{terraformTool, &v.Terraform},
		{awsCLITool, &v.AWSCLI},
		{markdownlintTool, &v.Markdownlint},
		{commitlintTool, &v.Commitlint},
	} {
		// **宣言が欠けていたらそこで止める。** 空のまま進むと、写しを空の版で書き潰す。
		version, ok := misetoml.Lookup(entries, d.name)
		if !ok {
			missing = append(missing, d.name)

			continue
		}
		*d.dst = version
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

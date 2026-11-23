// hcl-input は、HCL のソースから、conftest/hcl/ の検査の入力の JSON を作ります（ADR-0501 決定5）。順序と
// Conftest への受け渡しは make が持ちます（ADR-0702 決定24）。
//
//	hcl-input -base <dir> -listed <走査の root の下の通常のファイルの一覧> -out <json> <root>...
//	hcl-input fixtures -out <json> <txtar のディレクトリ>...
//
// root の下の通常のファイルすべての一覧と、`.tf`・`.tftest.hcl` の block を、入れ子の block と属性の式の
// 構文の木とともに出します（Conftest の入力のファイルそのもの）。fixtures は、txtar の1ファイルを1つの木として
// 同じ経路で読み、すべてのディレクトリのケースを、Rego のテストに渡す1つの JSON（トップキー `fixtures`）へ書きます。
//
// 読めなかったもの —— 構文の誤り、知らない式の型、`Variables()` との食い違い、symlink —— は JSON に現れず、
// 非ゼロで終えて -out を書きません。読んだファイルの集合は、別の経路の走査（-listed）と突き合わせます
// （ADR-0702 決定15）。
//
// 構文として読むファイルが0件のときは、違反（1）とは別の終了コード（2）で終えます（ADR-0702 決定13）。
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/atomicwrite"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const outPerm = 0o644

var (
	errUsage             = xerrors.New("使い方: hcl-input -base <dir> -listed <path> -out <path> <root>...")
	errMissingRoot       = xerrors.New("走査の root がありません")
	errNoInput           = xerrors.New("Terraform の構成ファイル（.tf）が1件もありません")
	errSyntax            = xerrors.New("HCL の構文の誤りがあります")
	errUnsupportedInput  = xerrors.New("読まない形の入力があります")
	errSymlink           = xerrors.New("走査の root の下に symlink があります")
	errUnknownExpression = xerrors.New("知らない形の式があります")
	errRefsMismatch      = xerrors.New("式から取り出した参照が、hclsyntax の Variables() と一致しません")
	errFileSetMismatch   = xerrors.New("読んだファイルの集合が、独立の走査の一覧（-listed）と一致しません")
	errListed            = xerrors.New("独立の走査の一覧（-listed）を読めません")
	errFixture           = xerrors.New("fixture の txtar を読めません")
)

func main() {
	log.SetFlags(0)
	if err := run(os.Args[1:], os.Stdout); err != nil {
		log.Printf("❌ hcl-input: %v", err)
		os.Exit(exitCode(err))
	}
}

func exitCode(err error) int {
	switch {
	case err == nil:
		return 0
	case xerrors.Is(err, errNoInput):
		return 2
	default:
		return 1
	}
}

// run は、走査の分類（symlink と、構成ファイルの名前のディレクトリ）を一覧との突き合わせより先に判定します。一覧の過不足として
// 報告すると、原因のファイルの種類が読み手に伝わらないためです。
func run(args []string, out io.Writer) error {
	if len(args) > 0 && args[0] == "fixtures" {
		return runFixtures(args[1:], out)
	}
	fset := flag.NewFlagSet("hcl-input", flag.ContinueOnError)
	fset.SetOutput(out)
	base := fset.String("base", "", "走査の root と、JSON と -listed の一覧に書くパスの起点のディレクトリ（-listed と -out のパスそのものは作業ディレクトリから解決する）")
	listedPath := fset.String("listed", "", "走査の root の下の通常のファイルの一覧（独立の走査 find が列挙したもの）")
	outPath := fset.String("out", "", "書き出す JSON のパス")
	if err := fset.Parse(args); err != nil {
		return xerrors.Wrapf(errUsage, "%v", err)
	}
	roots := fset.Args()
	if *base == "" || *listedPath == "" || *outPath == "" || len(roots) == 0 {
		return errUsage
	}

	return runSyntax(*base, *listedPath, *outPath, roots, out)
}

// runSyntax は JSON を outPath へ書きます。
func runSyntax(base, listedPath, outPath string, roots []string, out io.Writer) error {
	listed, err := readListed(listedPath)
	if err != nil {
		return err
	}
	doc, err := buildSyntax(base, roots, listed)
	if err != nil {
		return err
	}
	data, err := encodeSyntax(doc)
	if err != nil {
		return err
	}
	if err := atomicwrite.Apply(map[string]string{outPath: string(data)}, outPerm); err != nil {
		return xerrors.Wrapf(err, "%s への書き込み", outPath)
	}
	fmt.Fprintf(out, "✅ hcl-input: %d 件のファイル（構文として %d 件）から %d 個の block を %s へ書きました\n", len(doc.Files), countParsed(doc.Files), len(doc.Blocks), outPath)

	return nil
}

// buildSyntax は base の下の roots を歩いて listed と突き合わせ、構文として読むファイルを読みます。
// symlink と走査の分類を、一覧との突き合わせより先に判定します。
func buildSyntax(base string, roots, listed []string) (syntaxDoc, error) {
	files, err := walkAllFiles(base, roots)
	if err != nil {
		return syntaxDoc{}, err
	}
	paths := make([]string, 0, len(files))
	for _, f := range files {
		paths = append(paths, f.Path)
	}
	if err := compareFiles(paths, listed); err != nil {
		return syntaxDoc{}, err
	}
	if countParsed(files) == 0 {
		return syntaxDoc{}, xerrors.Wrapf(errNoInput, "%v", roots)
	}

	root, err := os.OpenRoot(base)
	if err != nil {
		return syntaxDoc{}, xerrors.Wrapf(err, "%s を開く", base)
	}
	defer func() { _ = root.Close() }()

	doc := syntaxDoc{Files: files, Blocks: []synBlock{}}
	for _, f := range files {
		if !f.Parsed {
			continue
		}
		src, err := root.ReadFile(filepath.FromSlash(f.Path))
		if err != nil {
			return syntaxDoc{}, xerrors.Wrapf(err, "%s の読み取り", f.Path)
		}
		got, err := parseSyntaxFile(f.Path, src)
		if err != nil {
			return syntaxDoc{}, err
		}
		doc.Blocks = append(doc.Blocks, got...)
	}

	return doc, nil
}

func countParsed(files []fileEntry) int {
	n := 0
	for _, f := range files {
		if f.Parsed {
			n++
		}
	}

	return n
}

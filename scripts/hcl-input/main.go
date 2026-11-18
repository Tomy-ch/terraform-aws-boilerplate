// hcl-input は、HCL のソースから Conftest の入力の JSON（data のトップキー `hcl_input`）を作ります
// （ADR-0501 決定5）。順序と Conftest への受け渡しは make が持ちます（ADR-0702 決定24）。
//
//	hcl-input -base <dir> -listed <find の一覧> -out <json> <root>...
//
// root の下の `.tf` を読み、全種類の block と、その属性の式が持つ参照を出します。読めなかったもの
// —— 構文の誤り、知らない式の型、`Variables()` との食い違い、`.tf.json`、symlink —— は JSON に現れず、
// 非ゼロで終えて -out を書きません。読んだファイルの集合は、別の経路の走査（-listed）と突き合わせます
// （ADR-0702 決定15）。
//
// 読むファイルが0件のときは、違反（1）とは別の終了コード（2）で終えます（ADR-0702 決定13）。
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
)

func main() {
	log.SetFlags(0)
	if err := run(os.Args[1:], os.Stdout); err != nil {
		if xerrors.Is(err, errNoInput) {
			log.Printf("❌ hcl-input: %v", err)
			os.Exit(2)
		}
		log.Fatalf("❌ hcl-input: %v", err)
	}
}

// run は、走査の分類（symlink・`.tf.json`）を一覧との突き合わせより先に判定します。一覧の過不足として
// 報告すると、原因のファイルの種類が読み手に伝わらないためです。
func run(args []string, out io.Writer) error {
	fset := flag.NewFlagSet("hcl-input", flag.ContinueOnError)
	fset.SetOutput(out)
	base := fset.String("base", "", "走査の root と出力のパスの起点のディレクトリ")
	listedPath := fset.String("listed", "", "独立の走査（find）が列挙した構成ファイルの一覧")
	outPath := fset.String("out", "", "書き出す JSON のパス")
	if err := fset.Parse(args); err != nil {
		return xerrors.Wrapf(errUsage, "%v", err)
	}
	roots := fset.Args()
	if *base == "" || *listedPath == "" || *outPath == "" || len(roots) == 0 {
		return errUsage
	}

	files, err := walkFiles(*base, roots)
	if err != nil {
		return err
	}
	listed, err := readListed(*listedPath)
	if err != nil {
		return err
	}
	if err := compareFiles(files, listed); err != nil {
		return err
	}
	if len(files) == 0 {
		return xerrors.Wrapf(errNoInput, "%v", roots)
	}

	var blocks []block
	for _, rel := range files {
		src, err := os.ReadFile(filepath.Join(*base, filepath.FromSlash(rel)))
		if err != nil {
			return xerrors.Wrapf(err, "%s の読み取り", rel)
		}
		got, err := parseFile(rel, src)
		if err != nil {
			return err
		}
		blocks = append(blocks, got...)
	}

	data, err := encode(files, blocks)
	if err != nil {
		return err
	}
	if err := atomicwrite.Apply(map[string]string{*outPath: string(data)}, outPerm); err != nil {
		return xerrors.Wrapf(err, "%s への書き込み", *outPath)
	}
	fmt.Fprintf(out, "✅ hcl-input: %d 件のファイルから %d 個の block を %s へ書きました\n", len(files), len(blocks), *outPath)

	return nil
}

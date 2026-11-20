// provider-schema は、lock した Provider の版ごとの schema から、層の検査（`make layer-check`）が
// Conftest に `--data` で渡す一覧（data のトップキー `layer_schema`）を作ります。schema の取得
// （`terraform init` と `terraform providers schema -json`）とその順序は make が持ちます
// （ADR-0702 決定24）。
//
//	provider-schema prepare -provider <address> -out <dir> -listed <file> <.terraform.lock.hcl>...
//	provider-schema merge -provider <address> -dir <dir> -out <json>
//
// prepare は、位置引数の lock の集合を -listed の一覧（独立に走査した lock のパスを1行に1件）の集合と
// 突き合わせてから（ADR-0702 決定15）、lock から Provider の版を取り出し、版ごとに `<dir>/<version>/` へ、
// その版だけを固定した `.terraform.lock.hcl` と `versions.tf` を置きます。schema の取り直しが要る版の
// ディレクトリを stdout へ1行ずつ出し、版の一覧を `<dir>/versions.txt` へ書きます。make は stdout の各行で
// schema を `<dir>/<version>/raw.json` へ取ります。
//
// merge は `<dir>/versions.txt` の各版の raw.json から resource の型ごとに書ける引数・非推奨の引数・
// block を取り出し、版をまたいだ和を -out へ書きます。
//
// 読めない lock、版を取れない Provider の block、読めない schema は、黙って飛ばさずに失敗させます
// （ADR-0702 決定14）。lock が0件のときは、それ以外の失敗（1）とは別の終了コード（2）で終えます
// （ADR-0702 決定13）。
package main

import (
	"io"
	"log"
	"os"
	"regexp"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const outPerm = 0o644

// versionPattern は、ディレクトリ名に使ってよい版の形です。パスの区切りや `..` を含む値を通しません。
var versionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$`)

var (
	errUsage           = xerrors.New("使い方: provider-schema prepare -provider <address> -out <dir> -listed <path> <lock>... | provider-schema merge -provider <address> -dir <dir> -out <path>")
	errNoLocks         = xerrors.New(".terraform.lock.hcl が1件も渡されていません")
	errLockSyntax      = xerrors.New("lock の HCL の構文の誤りがあります")
	errDuplicateBlock  = xerrors.New("lock に同じ Provider の block が2つ以上あります")
	errVersion         = xerrors.New("lock の Provider の block から版を取れません")
	errLockList        = xerrors.New("独立に走査した lock の一覧（-listed）を解釈できません")
	errCountMismatch   = xerrors.New("引数の lock の集合が、独立に走査した lock の集合と一致しません")
	errNoVersions      = xerrors.New("lock に Provider の版が1つもありません")
	errVersionList     = xerrors.New("版の一覧（versions.txt）を解釈できません")
	errSchemaMissing   = xerrors.New("schema（raw.json）が無いか空です")
	errSchemaMalformed = xerrors.New("schema（raw.json）を解釈できません")
	errNoResources     = xerrors.New("schema に resource の型が1つもありません")
)

func main() {
	log.SetFlags(0)
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		log.Printf("❌ provider-schema: %v", err)
		os.Exit(exitCode(err))
	}
}

func exitCode(err error) int {
	switch {
	case err == nil:
		return 0
	case xerrors.Is(err, errNoLocks):
		return 2
	default:
		return 1
	}
}

// run はサブコマンドを選びます。stdout は make が読む値だけを、stderr は人が読む要約を受けます。
func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errUsage
	}
	switch args[0] {
	case "prepare":
		return prepare(args[1:], stdout, stderr)
	case "merge":
		return merge(args[1:], stderr)
	default:
		return xerrors.Wrapf(errUsage, "知らないサブコマンド %q", args[0])
	}
}

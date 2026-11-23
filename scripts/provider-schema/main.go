// provider-schema は、Provider の schema から、Conftest に `--data` で渡す一覧を作ります。層の検査
// （`make layer-check`）には lock した Provider の版ごとの schema から data のトップキー `layer_schema` を、
// Policy Test（`make tf-policy`）には mock の plan の schema から data のトップキー `provider_schema` を
// 作ります。非推奨の書ける引数は、どちらも同じ関数（schema.go の extract）で取り出します。1回の起動は1つの
// 目的を持ち、順序は make が持ちます（ADR-0702 決定24）。
//
//	provider-schema prepare -provider <address> -out <dir> -base <dir> -listed <file> <root>...
//	provider-schema fetch -dir <dir>
//	provider-schema merge -provider <address> -dir <dir> -out <json>
//	provider-schema plan-deprecated -out <json> <test.jsonl>...
//
// prepare は、root の下で git が追跡する lock の集合を、-listed の一覧（git とは独立に走査したファイルの一覧）の
// lock の集合と突き合わせてから（ADR-0702 決定15）、lock から Provider の版を取り出し、版ごとに
// `<dir>/<version>/` へ、その版だけを固定した `.terraform.lock.hcl` と `versions.tf` を置きます。schema の
// 取り直しが要る版のディレクトリを `<dir>/fetch.txt` へ、版の一覧を `<dir>/versions.txt` へ書きます。
//
// fetch は、`<dir>/fetch.txt` の各ディレクトリで `terraform init` と `terraform providers schema -json` を
// 順に起動し、schema を `<version>/raw.json` へ書きます。init か schema の取得が1つでも失敗すれば、そこで
// 止まります。
//
// merge は `<dir>/versions.txt` の各版の raw.json から resource の型ごとに書ける引数・非推奨の引数・
// block を取り出し、版をまたいだ和を -out へ書きます。
//
// plan-deprecated は、reference の root ごとの `terraform test -json -verbose` の出力から test_plan の
// すべての Provider の schema を読み、型ごとの非推奨の書ける引数の root をまたいだ和を -out へ書きます。
//
// 読めない lock、版を取れない Provider の block、読めない schema と plan は、黙って飛ばさずに失敗させます
// （ADR-0702 決定14）。lock か plan が0件のときは、それ以外の失敗（1）とは別の終了コード（2）で終えます
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
	errUsage           = xerrors.New("使い方: provider-schema prepare -provider <address> -out <dir> -base <dir> -listed <path> <root>... | provider-schema fetch -dir <dir> | provider-schema merge -provider <address> -dir <dir> -out <path> | provider-schema plan-deprecated -out <path> <test.jsonl>...")
	errNoLocks         = xerrors.New("git が追跡する .terraform.lock.hcl が root の下に1件もありません")
	errLockSyntax      = xerrors.New("lock の HCL の構文の誤りがあります")
	errDuplicateBlock  = xerrors.New("lock に同じ Provider の block が2つ以上あります")
	errVersion         = xerrors.New("lock の Provider の block から版を取れません")
	errLockList        = xerrors.New("独立に走査した lock の一覧（-listed）を解釈できません")
	errCountMismatch   = xerrors.New("git が追跡する lock の集合が、独立に走査した lock の集合と一致しません")
	errFetchList       = xerrors.New("取り直す版の一覧（fetch.txt）を読めません")
	errFetch           = xerrors.New("schema を取れませんでした")
	errNoVersions      = xerrors.New("lock に Provider の版が1つもありません")
	errVersionList     = xerrors.New("版の一覧（versions.txt）を解釈できません")
	errSchemaMissing   = xerrors.New("schema（raw.json）が無いか空です")
	errSchemaMalformed = xerrors.New("schema（raw.json）を解釈できません")
	errNoResources     = xerrors.New("schema に resource の型が1つもありません")
	errNoPlans         = xerrors.New("terraform test の出力（jsonl）が1件も渡されていません")
	errPlanMalformed   = xerrors.New("terraform test の出力（jsonl）を解釈できません")
	errPlanTypeMissing = xerrors.New("plan の resource_changes の型が、plan の Provider の schema にありません")
	errNoArguments     = xerrors.New("schema のどの resource の型も、書ける引数を持ちません")
)

func main() {
	log.SetFlags(0)
	if err := run(os.Args[1:], os.Stderr); err != nil {
		log.Printf("❌ provider-schema: %v", err)
		os.Exit(exitCode(err))
	}
}

func exitCode(err error) int {
	switch {
	case err == nil:
		return 0
	case xerrors.Is(err, errNoLocks), xerrors.Is(err, errNoPlans):
		return 2
	default:
		return 1
	}
}

// run はサブコマンドを選びます。stderr は人が読む要約を受けます。
func run(args []string, stderr io.Writer) error {
	if len(args) == 0 {
		return errUsage
	}
	switch args[0] {
	case "prepare":
		return prepare(args[1:], stderr, gitLocks)
	case "fetch":
		return fetch(args[1:], stderr, terraformCmd)
	case "merge":
		return merge(args[1:], stderr)
	case "plan-deprecated":
		return planDeprecated(args[1:], stderr)
	default:
		return xerrors.Wrapf(errUsage, "知らないサブコマンド %q", args[0])
	}
}

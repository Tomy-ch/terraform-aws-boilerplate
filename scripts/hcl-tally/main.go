// hcl-tally は、conftest/hcl/ の検査の入力を、hcl-input とは別の経路で数えます（ADR-0702 決定15）。数えたものは
// Conftest に --data で渡り、Rego が hcl-input の JSON と突き合わせます。hcl-input の走査と構文の木を使わない
// ことが、このツールの存在理由です —— 同じ経路で数えた数は、同じ壊れ方をして一致します。
//
//	hcl-tally inputs -base <dir> -out <dir> [-files-key <key>] [-module-counts-key <key>] <root>...
//	hcl-tally fixtures -json <json> <txtar のディレクトリ>...
//
// inputs は、root の下の通常のファイル（`.terraform` の下を除く）を列挙して `<out>/found.txt`（hcl-input の
// -listed に渡す一覧）に書き、`.tf`・`.tftest.hcl` のトップレベルの block の行頭を正規表現で数えて
// `<out>/hcl-counts.json` に書きます。-files-key を渡せば、構成ファイル（`.tf`・`.tf.json`）の一覧を
// そのキーで `<out>/files.json` に書き、-module-counts-key を渡せば、`.tf` ごとの module block の行頭の数も
// そこへ書きます。
//
// fixtures は、ディレクトリの `*.txtar` の数の和と、hcl-input fixtures が書いた JSON のケースの数を突き合わせます。
//
// 無い root、ディレクトリでない root、構成ファイルが0件、txtar が0件はどれも失敗です。構成ファイルが0件のときは、
// 違反（1）とは別の終了コード（2）で終えます（ADR-0702 決定13）。
package main

import (
	"io"
	"log"
	"os"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const outPerm = 0o644

var (
	errUsage       = xerrors.New("使い方: hcl-tally inputs -base <dir> -out <dir> [-files-key <key>] [-module-counts-key <key>] <root>... | hcl-tally fixtures -json <path> <dir>...")
	errMissingRoot = xerrors.New("走査の root がありません")
	errNoInput     = xerrors.New("Terraform の構成ファイル（.tf）が1件もありません")
	errWalk        = xerrors.New("走査の root の下を列挙できません")
	errFixtures    = xerrors.New("fixture の txtar の数と JSON のケースの数が一致しません")
)

func main() {
	log.SetFlags(0)
	if err := run(os.Args[1:], os.Stdout); err != nil {
		log.Printf("❌ hcl-tally: %v", err)
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

func run(args []string, out io.Writer) error {
	if len(args) == 0 {
		return errUsage
	}
	switch args[0] {
	case "inputs":
		return runInputs(args[1:], out)
	case "fixtures":
		return runFixtures(args[1:], out)
	default:
		return xerrors.Wrapf(errUsage, "知らないサブコマンド %q", args[0])
	}
}

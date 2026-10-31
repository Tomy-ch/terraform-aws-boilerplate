// infra-code-gate は、インフラ内コードの検出（ADR-0103）のうち、HCL のソースから判定できない部分を持ちます。
// HCL から判定できる部分 —— 型の分類、コードを実行させる構成要素の検出、許可リストとの照合 —— は
// conftest/hcl/ の Rego が持ち、ここは持ちません。
//
// サブコマンド:
//
//	decl        許可リストの宣言を、ファイルシステム・ADR・code owner の宣言・保護設定と突き合わせる
//	count       Conftest が読んだファイルの集合を、独立に走査した集合と突き合わせる
//	rego-tests  Rego の deny の ID ごとに違反と非違反のテストが在り、それが走って通ったことを確かめる
//
// 検査対象が0件のときは、違反（1）とは別の終了コード（2）で終えます。「ゲートが外れた」と「合格した」を
// 区別するためです（ADR-0702 決定13）。
package main

import (
	"fmt"
	"io"
	"log"
	"os"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/lintreport"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

var (
	errUsage      = xerrors.New("使い方: infra-code-gate {decl|count|rego-tests} [flags]")
	errNoInput    = xerrors.New("検査の対象が0件です")
	errViolations = xerrors.New("違反があります")
)

func main() {
	log.SetFlags(0)
	if err := run(os.Args[1:], ".", os.Stdout); err != nil {
		if xerrors.Is(err, errNoInput) {
			log.Printf("❌ infra-code-gate: %v", err)
			os.Exit(2)
		}
		log.Fatalf("❌ infra-code-gate: %v", err)
	}
}

func run(args []string, repo string, out io.Writer) error {
	if len(args) == 0 {
		return errUsage
	}
	switch args[0] {
	case "decl":
		return runDecl(args[1:], repo, out)
	case "count":
		return runCount(args[1:], repo, out)
	case "rego-tests":
		return runRegoTests(args[1:], repo, out)
	default:
		return xerrors.Wrapf(errUsage, "%q", args[0])
	}
}

func report(out io.Writer, name string, findings []lintreport.Finding) error {
	if len(findings) == 0 {
		return nil
	}
	lintreport.Sort(findings)
	fmt.Fprintf(out, "❌ infra-code-gate %s: %d 件の違反\n\n", name, len(findings))
	fmt.Fprintln(out, lintreport.Format(findings))
	return xerrors.Wrapf(errViolations, "%s: %d 件", name, len(findings))
}

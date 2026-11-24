// tf-policy は、Policy Test（ADR-0402、ADR-0501 決定4・8）の段を持ちます。make は段を順に起動するだけで、
// Rego の形の検査、Rego のテストの件数の突き合わせ、reference の root ごとの plan の取得と取り出しは、ここが持ちます
// （ADR-0702 決定24・25）。Conftest の起動は make が持ち、その結果の判定は verify-result と tf-local policy-result が
// 持ちます。
//
//	tf-policy rego-check -policy <dir>
//	tf-policy verify-result -policy <dir> -report <conftest verify -o json の出力>
//	tf-policy plan -base <dir> -data <dir> -out <tmp/ の下の相対パス> -canary-data <dir> -canary-work <tmp/ の下の相対パス> -policy <dir> -decl <decl.json> -layer <decl.json> -reference <dir>
//	tf-policy extract -out <dir> -decl <decl.json>
//
// plan は、本走査の前に同じ起動の中で canary を走らせ、canary が落ちれば本走査を行いません（ADR-0702 決定25）。
//
// conftest verify はテストが0件でも、conftest test は入力が0件でも失敗しません。件数はここで独立に数えます
// （ADR-0702 決定13・15）。落ちた検査は終了コード 1、入力を読めない・対象が0件は 2 です。
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

var (
	errUsage   = xerrors.New("引数が正しくありません")
	errNoInput = xerrors.New("検査の対象が0件です")
	errRead    = xerrors.New("入力を読めません")
	errFailed  = xerrors.New("検査が落ちました")
	errCanary  = xerrors.New("canary が期待どおりではありません")
)

func main() {
	os.Exit(report(run(os.Args[1:], os.Stdout, command), os.Stderr))
}

func run(args []string, w io.Writer, tool toolFunc) error {
	if len(args) == 0 {
		return xerrors.Wrap(errUsage, "サブコマンドがありません")
	}
	rest := args[1:]
	switch args[0] {
	case "rego-check":
		return runRegoCheck(rest, w)
	case "verify-result":
		return runVerifyResult(rest, w)
	case "plan":
		return runPlan(rest, w, tool, func(a []string, w io.Writer, t toolFunc) error { return runCanary(a, w, t, ".") }, ".")
	case "extract":
		return runExtract(rest, w)
	default:
		return xerrors.Wrapf(errUsage, "知らないサブコマンド %q", args[0])
	}
}

// report は run のエラーを1行で w に書き、終了コードを返します。
func report(err error, w io.Writer) int {
	if err != nil {
		fmt.Fprintf(w, "❌ tf-policy: %v\n", err)
	}

	return exitCode(err)
}

func exitCode(err error) int {
	switch {
	case err == nil:
		return 0
	case xerrors.Is(err, errFailed), xerrors.Is(err, errCanary):
		return 1
	default:
		return 2
	}
}

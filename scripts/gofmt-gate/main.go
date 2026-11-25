// gofmt-gate は、運用機構（scripts/）の Go が gofmt で整形済みであることの検査（make go-fmt-check）を持ちます。
// 1回の起動の目的は整形の検査で、その前提として canary を先に走らせ、落ちれば本走査（gofmt -l .）を起動しません
// （ADR-0702 決定24・25）。
//
//	gofmt-gate check -work <dir> -dir <scripts>
//
// canary は -work に整形されていないファイル（直下と入れ子に1つずつ）と整形済みのファイルを建て、実物の gofmt -l が
// 整形されていないものだけを挙げることを確かめます。fixture は毎回作り直し、終わりに消します。
//
// 本走査は -dir で gofmt -l . を走らせます。**gofmt は構文の誤りでも、対象が無くても、一覧を出さずに終わり得る**
// ので、gofmt の終了コードが 0 でないことと、-dir の下の .go が0件であることはどちらも落とします。.go の件数は
// gofmt と同じ選び方（. で始まらない名前の .go）で数える0件の番兵であり、gofmt が何を読んだかの突き合わせでは
// ありません。gofmt が何かを挙げる力は canary が確かめます。
//
// canary が成り立たないことと整形されていないファイルは終了コード 1 です。使い方の誤り（-dir がリポジトリ直下から
// 外へ出るものを含む）、入力を読めない、道具を起動できない・0 でない終了コード、.go が0件は 2 です。
package main

import (
	"io"
	"os"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

var (
	errUsage   = xerrors.New("引数が正しくありません")
	errRead    = xerrors.New("入力を読めません")
	errNoInput = xerrors.New("検査の対象が0件です")
	errTool    = toolrun.ErrTool
	errExit    = xerrors.New("gofmt が 0 でない終了コードで終わりました")
	errCanary  = xerrors.New("canary が期待どおりではありません")
	errFailed  = xerrors.New("検査が落ちました")
)

func main() {
	os.Exit(report(run(os.Args[1:], os.Stdout, command), os.Stderr))
}

func run(args []string, w io.Writer, tool toolFunc) error {
	if len(args) == 0 {
		return xerrors.Wrap(errUsage, "サブコマンドがありません")
	}
	switch args[0] {
	case "check":
		return runCheck(args[1:], w, tool, ".")
	default:
		return xerrors.Wrapf(errUsage, "知らないサブコマンド %q", args[0])
	}
}

func report(err error, w io.Writer) int {
	toolrun.Report("gofmt-gate", err, w)

	return exitCode(err)
}

func exitCode(err error) int {
	return toolrun.ExitCode(err, errCanary, errFailed)
}

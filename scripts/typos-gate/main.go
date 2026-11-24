// typos-gate は、綴りの検査（make typos）を持ちます。1回の起動の目的は綴りの検査で、その前提として canary と
// 走査の範囲の突き合わせを先に走らせ、どちらかが落ちれば本走査（typos --format brief）を起動しません
// （ADR-0702 決定24・25）。
//
//	typos-gate check -work <dir> -config <.typos.toml>
//
// 範囲の突き合わせは、本走査の対象（typos --files）が git の一覧を覆っていることを、独立の経路の突き合わせで確かめます
// （ADR-0702 決定15）。外れてよいのは、expectedExcludes に当たるものと、typos が走査から外す自身の設定ファイル
// だけです。**expectedExcludes は .typos.toml の [files] の extend-exclude と意図して二重に持つ独立の宣言です。**
// 設定から期待を導くと、設定へ足した除外がそのまま「外れてよい」になり、範囲の縮みが何も赤くしません。両者が集合と
// して一致しなければ落ちるので、除外を変えるときは両方を変えます。
//
// canary は、実物の typos で2つを確かめます。設定を読んだうえで隠しディレクトリの中の誤綴りを落とすこと
// （ignore-hidden が既定の true に戻ると .github/ と .makefiles/ が黙って対象から外れる）と、本走査の対象に
// .github/ が入っていることです。fixture は -work に毎回作り直し、終わりに消します。設定は fixture へ複写し、
// 複写を --config で名指して読ませます —— 設定が見つからない走査は隠しディレクトリを飛ばして通るので、複写元が
// 無い・空なら道具を起動せずに落とします。誤綴りの語は、このソース自身が本走査に掛からないよう連結で組みます。
//
// canary が成り立たないことと綴りの誤りは終了コード 1 です。使い方の誤り、入力を読めない、道具を起動できない、
// 対象が0件、除外の宣言が食い違う・走査の対象が追跡ファイルを覆っていない（ゲートの範囲が崩れた）は 2 です。
package main

import (
	"io"
	"os"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/gitenv"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

var (
	errUsage     = xerrors.New("引数が正しくありません")
	errRead      = xerrors.New("入力を読めません")
	errNoInput   = xerrors.New("検査の対象が0件です")
	errTool      = toolrun.ErrTool
	errUncovered = xerrors.New("追跡ファイルが走査の対象から外れています")
	errCanary    = xerrors.New("canary が期待どおりではありません")
	errFailed    = xerrors.New("検査が落ちました")
	errExcludes  = xerrors.New(".typos.toml の extend-exclude が typos-gate の期待する除外と食い違っています")
)

// envFunc は、外側のリポジトリから切り離した git の env を返します。テストは差し替えます。
type envFunc func() ([]string, error)

func main() {
	os.Exit(report(run(os.Args[1:], os.Stdout, command, gitEnv), os.Stderr))
}

func gitEnv() ([]string, error) { return gitenv.Env(os.Environ()) }

func run(args []string, w io.Writer, tool toolFunc, env envFunc) error {
	if len(args) == 0 {
		return xerrors.Wrap(errUsage, "サブコマンドがありません")
	}
	rest := args[1:]
	switch args[0] {
	case "check":
		return runCheck(rest, w, tool, env, ".")
	default:
		return xerrors.Wrapf(errUsage, "知らないサブコマンド %q", args[0])
	}
}

// report は run のエラーを1行で w に書き、終了コードを返します。
func report(err error, w io.Writer) int {
	toolrun.Report("typos-gate", err, w)

	return exitCode(err)
}

func exitCode(err error) int {
	return toolrun.ExitCode(err, errCanary, errFailed)
}

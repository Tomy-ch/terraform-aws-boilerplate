// editorconfig-gate は、.editorconfig の宣言とファイルの一致の検査（make editorconfig-check）を持ちます。1回の
// 起動の目的はこの検査で、その前提として2つの canary と設定の確認を先に走らせ、どれかが落ちれば本走査（引数なしの
// editorconfig-checker）を起動しません（ADR-0702 決定24・25）。
//
//	editorconfig-gate check -work <dir> -editorconfig <.editorconfig> -checker-config <.editorconfig-checker.json>
//
// 引数を渡さない editorconfig-checker は、git の一覧（追跡ファイルと、ignore されていない未追跡のファイル）を
// 走査します。設定の確認は、.editorconfig-checker.json が空（`{}`）であること —— 除外を足すと検査対象が黙って
// 縮む —— と、本走査の対象（-dry-run の一覧）が1件以上あることを確かめます。
//
// 2つの canary は、fixture を -work の下に毎回作り直してその中にリポジトリを建て、本走査と同じ探索経路（引数なしの
// 起動）で実物の editorconfig-checker を走らせ、終わりに fixture を消します。
//
//   - check-canary: tab で字下げした YAML で落ちること
//   - directive-canary: 範囲を開く無効化の印を閉じなければ、そのファイルの残りが検査されないこと。
//     editorconfig-directive-lint はこの前提を写して印を分類します。道具の版を上げて読み方が変われば、ここが
//     落ち、lint の分類を見直す合図になります。印の対照として、印の無い同じ違反のファイルを置きます
//
// git と editorconfig-checker（git を起動する）には、外側のリポジトリから切り離した env（lib/gitenv）を渡します。
// その一覧を得られなければ、道具を1つも起動せずに落ちます。印の綴りは、このソース自身が検査の外に出ないよう
// 連結で組みます。
//
// canary が成り立たないことと本走査の違反は終了コード 1 です。使い方の誤り、入力を読めない、道具を起動できない、
// 対象が0件、.editorconfig-checker.json が空でない（ゲートの範囲が崩れた）は 2 です。
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/gitenv"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

var (
	errUsage   = xerrors.New("引数が正しくありません")
	errRead    = xerrors.New("入力を読めません")
	errNoInput = xerrors.New("検査の対象が0件です")
	errTool    = xerrors.New("道具を起動できません")
	errScope   = xerrors.New(".editorconfig-checker.json が空ではありません")
	errCanary  = xerrors.New("canary が期待どおりではありません")
	errFailed  = xerrors.New("検査が落ちました")
)

// envFunc は、外側のリポジトリから切り離した git の env を返します。テストは差し替えます。
type envFunc func() ([]string, error)

func main() {
	os.Exit(report(run(os.Args[1:], os.Stdout, command, gitEnv), os.Stderr))
}

func gitEnv() ([]string, error) { return gitEnvFrom(os.Environ()) }

// gitEnvFrom は、environ から外側のリポジトリを指す git の変数を外した写しを返します。
func gitEnvFrom(environ []string) ([]string, error) {
	return gitenv.Env(environ)
}

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
	if err != nil {
		fmt.Fprintf(w, "❌ editorconfig-gate: %v\n", err)
	}

	return exitCode(err)
}

func exitCode(err error) int {
	switch {
	case err == nil:
		return 0
	case xerrors.Is(err, errCanary), xerrors.Is(err, errFailed):
		return 1
	default:
		return 2
	}
}

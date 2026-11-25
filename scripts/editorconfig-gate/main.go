// editorconfig-gate は、.editorconfig の宣言とファイルの一致の検査（make editorconfig-check）を持ちます。1回の
// 起動の目的はこの検査で、その前提として3つの canary と、設定の確認・走査の範囲の突き合わせを先に走らせ、どれかが
// 落ちれば本走査（引数なしの editorconfig-checker）を起動しません（ADR-0702 決定24・25）。
//
//	editorconfig-gate check -work <dir> -editorconfig <.editorconfig> -checker-config <.editorconfig-checker.json>
//
// 引数を渡さない editorconfig-checker は、git の一覧（追跡ファイルと、ignore されていない未追跡のファイル）から
// 既定の除外を引いたものを走査します。その git が引用したパス（非 ASCII の名前）を editorconfig-checker は黙って
// 外すので、editorconfig-checker の起動にはどれも core.quotepath=false を env（GIT_CONFIG_COUNT・KEY_0・VALUE_0）で
// 渡し、受け継いだ git の設定の変数は外します（checkerEnv）。設定の確認は、.editorconfig-checker.json が空（`{}`）であること —— 除外を
// 足すと検査対象が黙って縮む —— を確かめます。
//
// 範囲の突き合わせは、本走査の対象（-dry-run の一覧）が、git の追跡ファイル（git ls-files --stage）から作業ツリーから
// 消えたもの（git ls-files --deleted）・submodule・excludedNames の名前を引いたものを、すべて含むことを確かめます
// （ADR-0702 決定15）。-dry-run だけに在るもの（未追跡のファイル）は許します。editorconfig-checker も git から
// 一覧を得るので、独立しているのは除外の層です —— 道具の既定の除外と excludedNames が食い違えば落ちます。パスは
// どちらも同じリポジトリの git が返す形で比べます（macOS では core.precomposeunicode により NFC）。改行を含むパスは
// 1行1パスの -dry-run に載り得ないので、必ず欠けとして落ちます。
//
// 3つの canary は、fixture を -work の下に毎回作り直してその中にリポジトリを建て、本走査と同じ探索経路（パスを
// 渡さない起動）で実物の editorconfig-checker を走らせ、終わりに fixture を消します。canary と -dry-run は出力を
// 読むので -format default で形を固定し、本走査は GitHub Actions で注釈を出せるよう形を固定しません。
//
//   - check-canary: tab で字下げした YAML で落ち、同じ違反を置いた非 ASCII の名前のファイルも名指すこと
//   - directive-canary: 範囲を開く無効化の印を閉じなければ、そのファイルの残りが検査されないこと。
//     editorconfig-directive-lint はこの前提を写して印を分類します。道具の版を上げて読み方が変われば、ここが
//     落ち、lint の分類を見直す合図になります。印の対照として、印の無い同じ違反のファイルを置きます
//   - exclude-canary: excludedNames の名前を入れ子のディレクトリに置くと、どれも -dry-run に載らないこと。対照と
//     して、宣言に無い名前のファイルが、ASCII と非 ASCII の名前の両方で載ることを見ます
//
// git と editorconfig-checker（git を起動する）には、外側のリポジトリから切り離した env（lib/gitenv）を渡します。
// その一覧を得られなければ、道具を1つも起動せずに落ちます。印の綴りは、このソース自身が検査の外に出ないよう
// 連結で組みます。
//
// canary が成り立たないこと、追跡ファイルが走査の対象から外れていること、本走査の違反は終了コード 1 です。使い方の
// 誤り、入力を読めない、道具を起動できない、引いた後の追跡ファイルが0件、.editorconfig-checker.json が空でない
// （ゲートの範囲が崩れた）は 2 です。
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
	errScope     = xerrors.New(".editorconfig-checker.json が空ではありません")
	errUncovered = xerrors.New("追跡ファイルが走査の対象から外れています")
	errCanary    = xerrors.New("canary が期待どおりではありません")
	errFailed    = xerrors.New("検査が落ちました")
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
	toolrun.Report("editorconfig-gate", err, w)

	return exitCode(err)
}

func exitCode(err error) int {
	return toolrun.ExitCode(err, errCanary, errFailed, errUncovered)
}

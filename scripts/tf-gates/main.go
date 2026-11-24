// tf-gates は、Terraform の静的な検査の段（fmt・init・validate・test・TFLint）を、対象のディレクトリごとに起動し、
// 結果を判定します（ADR-0501 決定4）。make は段を順に呼ぶだけで、対象の選び方・root の判定・失敗の集約は
// ここが持ちます（ADR-0702 決定24・25）。
//
//	tf-gates dirs -base <dir> <root>...
//	tf-gates none -base <dir> <root>...
//	tf-gates fmt [-check] -base <dir> <root>...
//	tf-gates init -base <dir> -data <dir> <root>...
//	tf-gates validate -base <dir> -data <dir> -out <dir> -canary-data <dir> -canary-work <tmp/ の下の相対パス> -canary-reference <dir> <root>...
//	tf-gates result -base <dir> -manifest <tsv> -ignore <yaml>
//	tf-gates test -base <dir> -data <dir> <root>...
//	tf-gates lint -base <dir> -config <file> -plugin-dir <dir> <root>...
//
// 対象のディレクトリは、走査の root の下の `.tf` を持つディレクトリで、段に `tests` か `.terraform` を持つものを
// 除きます（tests の下の setup module は親の terraform test が読む）。宣言した root が1つでも無いときと、対象が
// 0件のときは成功させません（ADR-0702 決定13）。-allow-missing-roots を渡したときだけ、無い root を名指して
// 飛ばします（root の一部だけを持つ canary の fixture のため）。terraform と TFLint には AWS_ で始まる環境変数を渡さず、
// 資格情報を持つ手元でも、持たない CI と同じ条件で走らせます（ADR-0401 決定6）。
//
// validate は、本走査の前に同じ起動の中で canary を走らせ、canary が落ちれば本走査を行いません（ADR-0702 決定25）。
//
// 落ちた検査は終了コード 1、入力や宣言を読めない・対象が0件・道具を起動できない・道具がシグナルで終わったは 2 です。
// 道具を起動できない、またはシグナルで終わったときは、その時点で残りの対象を走らせません。
package main

import (
	"io"
	"os"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

var (
	errUsage       = xerrors.New("引数が正しくありません")
	errMissingRoot = xerrors.New("走査の root がありません")
	errWalk        = xerrors.New("走査の root の下を列挙できません")
	errTargets     = xerrors.New("対象ディレクトリがあります")
	errDirsFailed  = xerrors.New("落ちたディレクトリがあります")
	errCanary      = xerrors.New("canary が期待どおりではありません")
	errTool        = toolrun.ErrTool
)

func main() {
	os.Exit(report(run(os.Args[1:], os.Stdout, command, "."), os.Stderr))
}

// run は、サブコマンドへ振り分けます。root はリポジトリ直下です。
func run(args []string, w io.Writer, tool toolFunc, root string) error {
	if len(args) == 0 {
		return xerrors.Wrap(errUsage, "サブコマンドがありません")
	}
	rest := args[1:]
	switch args[0] {
	case "dirs":
		return runDirs(rest, w)
	case "none":
		return runNone(rest, w)
	case "fmt":
		return runFmt(rest, w, tool)
	case "init":
		return runInit(rest, w, tool)
	case "validate":
		return runValidate(rest, w, tool, validateCanary(root), root)
	case "result":
		return runResult(rest, w)
	case "test":
		return runTest(rest, w, tool)
	case "lint":
		return runLint(rest, w, tool)
	default:
		return xerrors.Wrapf(errUsage, "知らないサブコマンド %q", args[0])
	}
}

// report は run のエラーを1行で w に書き、終了コードを返します。エラーの文言はパスや validate の出力を含むので、
// lineGuard を通します。
func report(err error, w io.Writer) int {
	if err != nil {
		g := &lineGuard{w: w}
		_, _ = io.WriteString(g, "❌ tf-gates: "+err.Error()+"\n")
		_ = g.flush()
	}

	return exitCode(err)
}

func exitCode(err error) int {
	return toolrun.ExitCode(err, errFailed, errDirsFailed, errTargets, errCanary)
}

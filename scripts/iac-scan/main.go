// iac-scan は、IaC の誤設定の走査器（Trivy の config・Checkov・KICS・Semgrep CE）を起動し、結果を判定します
// （ADR-0501 決定6）。サブコマンドは1つの走査器だけを起動し、入力の準備と番兵、canary、本走査、件数の突き合わせを
// この順に行います。canary が期待どおりに落ちなければ、本走査を起動せずに失敗します（ADR-0702 決定13・15・25）。
//
//	iac-scan trivy-config [-base <dir>] -severity <値> [-format table|json|sarif] <root>...
//	iac-scan checkov [-base <dir>] <root>...
//	iac-scan kics [-base <dir>] -fail-on <値> <root>...
//	iac-scan semgrep [-base <dir>] -rules <tar.gz> -rules-sha256 <hex> <root>...
//
// 閾値（-severity・-fail-on）は呼び出し元の .mk が渡し、ここは決まった値の一覧と照らして、引数の定数へ写すだけです。
// 一覧に無い値は使い方の誤りです。
//
// 上流のイメージで走る道具（Checkov・KICS・Semgrep CE）は、<base>/tmp/<道具>/ に入力（work/）と compose.yaml を置き、
// そこを作業ディレクトリにして docker compose で起動します。ホストのパスを mount せず、network も与えず、
// 入力を複写で渡して結果（out/）を複写で取り出します（ADR-0503 決定7）。イメージは docker-compose.yaml の service から
// 引きます。結果を取り出せなければ、道具が 0 で終わっていても失敗です。本走査の出力は stderr へ流し、canary の出力は
// canary が落ちたときだけ stderr へ出します。stdout へ書くのは trivy-config の本走査の結果（要求された形）だけです。
//
// 所見・canary の失敗・件数のずれ・結果の取り出しの失敗は終了コード 1、使い方・入力・対象が0件・docker の失敗・道具を
// 起動できないかシグナルで終わったこと（toolrun.ErrTool）は 2 です。作業ディレクトリは workdir.Resolve で <base>/tmp/ の
// 下にあることを確かめてから消し、外を指せば使い方の誤りです。
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
	errNoTargets   = xerrors.New("走査の対象が0件です")
	errInput       = xerrors.New("入力を読めません")
	errImage       = xerrors.New("イメージを決められません")
	errDocker      = xerrors.New("docker compose が失敗しました")
	errTool        = toolrun.ErrTool
	errResult      = xerrors.New("走査の結果を読めません")
	errOutput      = xerrors.New("走査の結果を取り出せません")
	errCoverage    = xerrors.New("走査した範囲が入力に届きません")
	errCanary      = xerrors.New("canary が期待どおりではありません")
	errFindings    = xerrors.New("所見があります")
)

func main() {
	os.Exit(report(run(os.Args[1:], os.Stdout, os.Stderr, runner{tool: command, environ: os.Environ()}), os.Stderr))
}

func run(args []string, stdout, stderr io.Writer, r runner) error {
	if len(args) == 0 {
		return xerrors.Wrap(errUsage, "サブコマンドがありません")
	}
	rest := args[1:]
	switch args[0] {
	case "trivy-config":
		return runTrivyConfig(rest, stdout, stderr, r)
	case "checkov":
		return runCheckov(rest, stderr, r)
	case "kics":
		return runKics(rest, stderr, r)
	case "semgrep":
		return runSemgrep(rest, stderr, r)
	default:
		return xerrors.Wrapf(errUsage, "知らないサブコマンド %q", args[0])
	}
}

// report は run のエラーを1行で w に書き、終了コードを返します。
func report(err error, w io.Writer) int {
	toolrun.Report("iac-scan", err, w)

	return exitCode(err)
}

func exitCode(err error) int {
	return toolrun.ExitCode(err, errFindings, errCanary, errCoverage, errOutput)
}

// secret-scan は、秘密の混入の走査器（gitleaks・trufflehog・Trivy の secret）を起動し、結果を判定します
// （ADR-0302）。サブコマンドは1つの走査器だけを起動し、入力の番兵、canary、本走査、件数の突き合わせをこの順に
// 行います。canary が期待どおりに検出しなければ、本走査を起動せずに失敗します（ADR-0702 決定13・15・24・25）。
// git は、入力の準備（範囲の起点の解決、canary のリポジトリ、走査の対象の一覧）と、走査した数の独立の数え直しにだけ
// 使います。
//
//	secret-scan gitleaks-range -from <rev>
//	secret-scan gitleaks-history
//	secret-scan trufflehog-range -from <rev> [-format text|json]
//	secret-scan trufflehog-history [-format text|json]
//	secret-scan trufflehog-worktree [-format text|json]
//	secret-scan trivy [-format table|json|sarif]
//
// 範囲の起点は git cat-file --batch-check に stdin で渡して commit へ解決し、解決した完全な sha だけを走査器の引数に
// 置きます。空・改行を含む・`-` で始まる・解決できない・commit でない起点は、走査器を起動せずに終了コード 2 です
// （gitleaks も trufflehog も、起点が解決できないと範囲を0件として扱い、所見なしで通る）。ほかの可変の値は、作業
// ディレクトリと、そこに置いた固定名のファイルで渡します。canary は本走査と同じ段（引数）を、作業ディレクトリだけ
// 替えて起動します。
//
// 作業ディレクトリは tmp/secret-scan/<サブコマンド>/ で、workdir.Resolve で tmp/ の下にあることを確かめてから
// 作り直し、終わりに消します。canary の偽の資格情報は実行時に乱数で作り、リポジトリに置きません。
//
// git と、git を起動する走査器（gitleaks・trufflehog）には、外側のリポジトリから切り離した env（lib/gitenv）を
// 渡し、git には core.hooksPath=/dev/null を付けます。trivy には TRIVY_ で始まる環境変数を渡しません —— 呼び出した
// 人の環境が、走査の設定や抑止を差し替えないため。
//
// stdout へ書くのは本走査の結果（trufflehog と trivy は要求された形）だけです。CI はそれを機械可読な結果として
// 数えます。gitleaks の本走査の出力と canary の失敗の詳細は stderr へ出します。
//
// 所見・canary の失敗・走査した数のずれは終了コード 1、使い方・範囲の起点・入力・対象が0件・走査器の不全・道具を
// 起動できないかシグナルで終わったこと（toolrun.ErrTool）は 2 です。
package main

import (
	"io"
	"os"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

var (
	errUsage          = xerrors.New("引数が正しくありません")
	errFromEmpty      = xerrors.New("走査範囲の起点が空です。GITHUB_BASE_REF を渡すか COMMITLINT_FROM を指定してください")
	errFromUnresolved = xerrors.New("浅い取得か、ref の取り違えです")
	errInput          = xerrors.New("入力を読めません")
	errExclude        = xerrors.New("抑止の正規表現を解釈できません")
	errNoTargets      = xerrors.New("走査の対象が0件です")
	errTool           = toolrun.ErrTool
	errScan           = xerrors.New("走査器が走査を終えられませんでした")
	errCanary         = xerrors.New("canary が期待どおりではありません")
	errFindings       = xerrors.New("秘密の所見があります")
)

func main() {
	os.Exit(report(run(os.Args[1:], os.Stdout, os.Stderr, runner{tool: command, git: command, environ: os.Environ(), root: "."}), os.Stderr))
}

func run(args []string, stdout, stderr io.Writer, r runner) error {
	if len(args) == 0 {
		return xerrors.Wrap(errUsage, "サブコマンドがありません")
	}
	rest := args[1:]
	switch args[0] {
	case "gitleaks-range":
		return runGitleaks(rest, true, stderr, r)
	case "gitleaks-history":
		return runGitleaks(rest, false, stderr, r)
	case "trufflehog-range":
		return runTrufflehogGit(rest, true, stdout, stderr, r)
	case "trufflehog-history":
		return runTrufflehogGit(rest, false, stdout, stderr, r)
	case "trufflehog-worktree":
		return runTrufflehogWorktree(rest, stdout, stderr, r)
	case "trivy":
		return runTrivy(rest, stdout, stderr, r)
	default:
		return xerrors.Wrapf(errUsage, "知らないサブコマンド %q", args[0])
	}
}

// report は run のエラーを1行で w に書き、終了コードを返します。
func report(err error, w io.Writer) int {
	toolrun.Report("secret-scan", err, w)

	return exitCode(err)
}

func exitCode(err error) int {
	return toolrun.ExitCode(err, errFindings, errCanary, errMismatch)
}

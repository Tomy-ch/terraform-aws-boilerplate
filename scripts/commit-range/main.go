// commit-range は、commit message の検査（make commitlint-range）の前段で、検査する範囲 -from..-to が merge でない
// commit を1件以上持つことを確かめ、その件数を出します。commitlint の起動は make が持ちます（ADR-0702 決定24）。
//
//	commit-range check -from <base> -to <head>
//
// **commitlint は範囲が0件でも違反なしで通り、merge commit の message は既定で無視する**ので、merge でない commit が
// 0件の範囲は「積んだ commit が無い」のか「ref が解決できない」のかに関わらず落とします。-from と -to は git の引数に置かず、[gitrev] の取り決めで commit の sha へ解決してから、
// 件数を git rev-list --count --stdin の stdin で数えます。
//
// 失敗はどれも終了コード 2 です —— 違反（commitlint の 1）と検査不全を呼び出し側が分けるためです。
package main

import (
	"io"
	"os"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/gitenv"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

var (
	errUsage      = xerrors.New("引数が正しくありません")
	errNoBase     = xerrors.New("比較対象が解決できません。GITHUB_BASE_REF を渡すか COMMITLINT_FROM を指定してください")
	errUnresolved = xerrors.New("commit へ解決できません（浅い取得、ref の取り違え、または rev の形が不正）")
	errGit        = xerrors.New("git が失敗しました")
	errNoCommits  = xerrors.New("検査対象の commit が 0 件です。参照解決が壊れている可能性があります")
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
	switch args[0] {
	case "check":
		return runCheck(args[1:], w, tool, env, ".")
	default:
		return xerrors.Wrapf(errUsage, "知らないサブコマンド %q", args[0])
	}
}

func report(err error, w io.Writer) int {
	toolrun.Report("commit-range", err, w)

	return toolrun.ExitCode(err)
}

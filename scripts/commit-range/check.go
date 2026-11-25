package main

import (
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/gitrev"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// runCheck は、root のリポジトリで -from と -to を commit へ解決し、範囲の commit を数えて w へ出します。0件は
// errNoCommits です。
func runCheck(args []string, w io.Writer, tool toolFunc, env envFunc, root string) error {
	fset := flag.NewFlagSet("commit-range check", flag.ContinueOnError)
	fset.SetOutput(w)
	from := fset.String("from", "", "範囲の起点（ベースの先端）")
	to := fset.String("to", "", "範囲の終点")
	if err := fset.Parse(args); err != nil {
		return xerrors.Wrapf(errUsage, "%v", err)
	}
	if *to == "" || fset.NArg() > 0 {
		return xerrors.Wrap(errUsage, "commit-range check -from <base> -to <head>")
	}
	if *from == "" {
		return errNoBase
	}
	e, err := env()
	if err != nil {
		return xerrors.Wrapf(errGit, "%v", err)
	}
	fromSHA, err := resolve(tool, e, root, *from)
	if err != nil {
		return err
	}
	toSHA, err := resolve(tool, e, root, *to)
	if err != nil {
		return err
	}
	n, err := count(tool, e, root, fromSHA, toSHA)
	if err != nil {
		return err
	}
	if n == 0 {
		return xerrors.Wrapf(errNoCommits, "base=%s head=%s", *from, *to)
	}
	fmt.Fprintf(w, "検査対象: %d commit (%s..%s)\n", n, *from, *to)

	return nil
}

// resolve は、rev を root のリポジトリで commit の完全な sha へ解決します。解決できなければ errUnresolved です。
func resolve(tool toolFunc, env []string, root, rev string) (string, error) {
	unresolved := xerrors.Wrapf(errUnresolved, "%q", rev)
	stdin, err := gitrev.Query(rev)
	if err != nil {
		return "", unresolved
	}
	out, err := gitOutput(tool, env, root, stepResolve, stdin)
	if err != nil {
		return "", err
	}
	sha, err := gitrev.Parse(out)
	if err != nil {
		return "", unresolved
	}

	return sha, nil
}

// count は、root のリポジトリで from..to の merge でない commit の数を git rev-list --count で数えます。出力が 0 以上の整数で
// なければ errGit です。
func count(tool toolFunc, env []string, root, from, to string) (int, error) {
	out, err := gitOutput(tool, env, root, stepCount, "^"+from+"\n"+to+"\n")
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil || n < 0 {
		return 0, xerrors.Wrapf(errGit, "rev-list --count の出力 %q が数ではありません", out)
	}

	return n, nil
}

// gitOutput は、root で git の段 step を stdin を渡して起動し、stdout を返します。0 でない終了コードは errGit です。
func gitOutput(tool toolFunc, env []string, root, step, stdin string) (string, error) {
	cmd := tool(root, env, step)
	cmd.Stdin = strings.NewReader(stdin)
	r, err := toolrun.Run(cmd)
	if err != nil {
		return "", err
	}
	if r.Code != 0 {
		return "", xerrors.Wrapf(errGit, "git（%s）が rc=%d で終わりました: %s", step, r.Code, strings.TrimSpace(r.Combined))
	}

	return r.Stdout, nil
}

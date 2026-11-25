package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/workdir"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// runCheck は、.go の0件の番兵・canary・本走査を、この順に走らせます。前の段が落ちれば、後の段を起動しません。
// root はリポジトリ直下です。-work の検証は [workdir.Resolve] が持ちます。
func runCheck(args []string, w io.Writer, tool toolFunc, root string) error {
	fset := flag.NewFlagSet("gofmt-gate check", flag.ContinueOnError)
	fset.SetOutput(w)
	work := fset.String("work", "", "canary の fixture を建てるディレクトリ（中身は毎回作り直し、終わりに消す）")
	dir := fset.String("dir", "", "検査するディレクトリ。リポジトリ直下からの相対パス")
	if err := fset.Parse(args); err != nil {
		return xerrors.Wrapf(errUsage, "%v", err)
	}
	if *work == "" || *dir == "" || fset.NArg() > 0 {
		return xerrors.Wrap(errUsage, "gofmt-gate check -work <dir> -dir <scripts>")
	}
	if !filepath.IsLocal(*dir) {
		return xerrors.Wrapf(errUsage, "-dir %q はリポジトリ直下からの相対パスで、外へ出ないものに限ります", *dir)
	}
	if _, err := workdir.Resolve(root, *work); err != nil {
		return xerrors.Wrapf(errUsage, "-work %v", err)
	}
	n, err := countGoFiles(filepath.Join(root, *dir))
	if err != nil {
		return err
	}
	fixture, err := workdir.Recreate(root, *work)
	if err != nil {
		return xerrors.Wrapf(errRead, "-work %v", err)
	}
	defer func() { _ = workdir.Remove(root, *work) }()
	if err := runCanary(fixture, w, tool); err != nil {
		return err
	}

	return runScan(filepath.Join(root, *dir), *dir, n, w, tool)
}

// countGoFiles は、dir の下の .go を gofmt と同じ選び方（. で始まらない名前）で数えます。dir が無い・読めないなら
// errRead、0件なら errNoInput です。
func countGoFiles(dir string) (int, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return 0, xerrors.Wrapf(errRead, "%v", err)
	}
	if !info.IsDir() {
		return 0, xerrors.Wrapf(errRead, "%s はディレクトリではありません", dir)
	}
	n := 0
	err = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && !strings.HasPrefix(d.Name(), ".") && strings.HasSuffix(d.Name(), ".go") {
			n++
		}

		return nil
	})
	if err != nil {
		return 0, xerrors.Wrapf(errRead, "%v", err)
	}
	if n == 0 {
		return 0, xerrors.Wrapf(errNoInput, "%s の下に .go がありません", dir)
	}

	return n, nil
}

// runScan は、dir で本走査（gofmt -l .）を走らせます。0 でない終了コードは errExit、挙がったファイルは label からの
// パスで w へ書いて errFailed です。
func runScan(dir, label string, n int, w io.Writer, tool toolFunc) error {
	r, err := toolrun.Run(tool(context.Background(), dir, os.Environ(), stepMain))
	if err != nil {
		return err
	}
	if r.Code != 0 {
		fmt.Fprint(w, r.Combined)

		return xerrors.Wrapf(errExit, "rc=%d", r.Code)
	}
	got := lines(r.Stdout)
	if len(got) > 0 {
		for _, p := range got {
			fmt.Fprintln(w, filepath.Join(label, filepath.FromSlash(p)))
		}

		return xerrors.Wrapf(errFailed, "整形されていないファイルが %d 件あります（make go-fmt で直ります）", len(got))
	}
	fmt.Fprintf(w, "✅ gofmt: %s の .go %d 件は整形済みです\n", label, n)

	return nil
}

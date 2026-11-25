package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/workdir"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// runCheck は、canary・走査の範囲の突き合わせ・本走査を、この順に走らせます。前の段が落ちれば、後の段を起動しません。
// root はリポジトリ直下です。-work の検証は [workdir.Resolve] が持ちます。
func runCheck(args []string, w io.Writer, tool toolFunc, env envFunc, root string) error {
	fset := flag.NewFlagSet("typos-gate check", flag.ContinueOnError)
	fset.SetOutput(w)
	work := fset.String("work", "", "canary の fixture を建てるディレクトリ（中身は毎回作り直し、終わりに消す）")
	config := fset.String("config", "", "typos の設定（.typos.toml）。リポジトリ直下からの相対パス")
	if err := fset.Parse(args); err != nil {
		return xerrors.Wrapf(errUsage, "%v", err)
	}
	if *work == "" || *config == "" || fset.NArg() > 0 {
		return xerrors.Wrap(errUsage, "typos-gate check -work <dir> -config <.typos.toml>")
	}
	if _, err := workdir.Resolve(root, *work); err != nil {
		return xerrors.Wrapf(errUsage, "-work %v", err)
	}
	cfg, err := readConfig(*config)
	if err != nil {
		return err
	}
	fixture, err := workdir.Recreate(root, *work)
	if err != nil {
		return xerrors.Wrapf(errRead, "-work %v", err)
	}
	defer workdir.Cleanup(root, *work, w)
	if err := runCanary(fixture, cfg, w, tool); err != nil {
		return err
	}
	// 本走査の前に fixture を消す。走査の範囲を .gitignore の扱いに預けない。
	if err := workdir.Remove(root, *work); err != nil {
		return xerrors.Wrapf(errRead, "-work の後片付け: %v", err)
	}
	if err := runCoverage(*config, cfg, w, tool, env); err != nil {
		return err
	}

	return runScan(w, tool)
}

// runScan は、リポジトリ直下で本走査（typos --format brief）を走らせ、出力を w へ写します。誤綴りの終了コードは
// errFailed、それ以外の 0 でない終了コード（不正な引数・設定を読めない）は errTool です。
func runScan(w io.Writer, tool toolFunc) error {
	r, err := toolrun.Run(tool(context.Background(), ".", os.Environ(), stepMain))
	fmt.Fprint(w, r.Combined)
	if err != nil {
		return err
	}
	switch r.Code {
	case 0:
		fmt.Fprintln(w, "✅ typos: 綴りの誤りはありません")

		return nil
	case typosFound:
		return xerrors.Wrap(errFailed, "綴りの誤りがあります")
	default:
		return xerrors.Wrapf(errTool, "typos が rc=%d で終わりました", r.Code)
	}
}

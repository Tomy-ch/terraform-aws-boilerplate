package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"path/filepath"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/workdir"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// runCheck は、3つの canary・設定の確認と走査の範囲の突き合わせ・本走査を、この順に走らせます。前の段が落ちれば、後の段を
// 起動しません。root はリポジトリ直下です。-work の検証は [workdir.Resolve] が持ち、-work は終わりに消します。
func runCheck(args []string, w io.Writer, tool toolFunc, env envFunc, root string) error {
	fset := flag.NewFlagSet("editorconfig-gate check", flag.ContinueOnError)
	fset.SetOutput(w)
	work := fset.String("work", "", "canary の fixture を建てるディレクトリ（中身は毎回作り直し、終わりに消す）")
	editorconfig := fset.String("editorconfig", "", "canary の fixture へ複写する .editorconfig")
	checkerConfig := fset.String("checker-config", "", "editorconfig-checker の設定（.editorconfig-checker.json。`{}` であること）")
	if err := fset.Parse(args); err != nil {
		return xerrors.Wrapf(errUsage, "%v", err)
	}
	if *work == "" || *editorconfig == "" || *checkerConfig == "" || fset.NArg() > 0 {
		return xerrors.Wrap(errUsage, "editorconfig-gate check -work <dir> -editorconfig <path> -checker-config <path>")
	}
	if _, err := workdir.Resolve(root, *work); err != nil {
		return xerrors.Wrapf(errUsage, "-work %v", err)
	}
	gitEnv, err := env()
	if err != nil {
		return xerrors.Wrapf(errTool, "%v", err)
	}
	base, err := workdir.Recreate(root, *work)
	if err != nil {
		return xerrors.Wrapf(errRead, "-work %v", err)
	}
	defer workdir.Cleanup(root, *work, w)
	fixture := func(sub string) fixturePaths {
		return fixturePaths{work: filepath.Join(base, sub), editorconfig: *editorconfig, checkerConfig: *checkerConfig}
	}
	if err := runCheckCanary(fixture("check"), w, tool, gitEnv); err != nil {
		return err
	}
	if err := runDirectiveCanary(fixture("directive"), w, tool, gitEnv); err != nil {
		return err
	}
	if err := runExcludeCanary(fixture("exclude"), w, tool, gitEnv); err != nil {
		return err
	}
	// 本走査の前に fixture を消す。走査の範囲を .gitignore の扱いに預けない。
	if err := workdir.Remove(root, *work); err != nil {
		return xerrors.Wrapf(errRead, "-work の後片付け: %v", err)
	}
	if err := runConfig(*checkerConfig, w, tool, gitEnv); err != nil {
		return err
	}

	return runScan(w, tool, gitEnv)
}

// runScan は、リポジトリ直下で本走査（引数なしの editorconfig-checker）を走らせ、出力を w へ写します。0 でない
// 終了コードは errFailed です。
func runScan(w io.Writer, tool toolFunc, env []string) error {
	r, err := toolrun.Run(tool(context.Background(), ".", checkerEnv(env), stepScan))
	fmt.Fprint(w, r.Combined)
	if err != nil {
		return err
	}
	if r.Code != 0 {
		return xerrors.Wrapf(errFailed, "editorconfig-checker が rc=%d で終わりました", r.Code)
	}
	fmt.Fprintln(w, "✅ editorconfig-check: .editorconfig の宣言との不一致はありません")

	return nil
}

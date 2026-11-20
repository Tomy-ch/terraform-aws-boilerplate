package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/atomicwrite"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// tfCommand は、ディレクトリ d で terraform の段 step（init か schema）を起動するコマンドを作ります。
type tfCommand func(d, step string) *exec.Cmd

// terraformCmd は tfCommand の実物です。引数は段ごとに固定で、ディレクトリは作業ディレクトリとして渡します。
func terraformCmd(d, step string) *exec.Cmd {
	var cmd *exec.Cmd
	if step == "init" {
		cmd = exec.CommandContext(context.Background(), "terraform", "init", "-backend=false", "-lockfile=readonly", "-input=false", "-no-color")
	} else {
		cmd = exec.CommandContext(context.Background(), "terraform", "providers", "schema", "-json")
	}
	cmd.Dir = d

	return cmd
}

// fetch は、prepare が書いた `<dir>/fetch.txt` の各ディレクトリで schema を取ります。init の出力は
// `<version>/init.log` に残し、失敗したときだけ stderr へ出します。schema は取れたときだけ raw.json へ書きます。
func fetch(args []string, stderr io.Writer, tf tfCommand) error {
	fset := flag.NewFlagSet("provider-schema fetch", flag.ContinueOnError)
	fset.SetOutput(stderr)
	dir := fset.String("dir", "", "prepare の -out に渡したディレクトリ")
	if err := fset.Parse(args); err != nil {
		return xerrors.Wrapf(errUsage, "%v", err)
	}
	if *dir == "" || fset.NArg() > 0 {
		return errUsage
	}
	raw, err := os.ReadFile(filepath.Join(*dir, fetchName))
	if err != nil {
		return xerrors.Wrapf(errFetchList, "%v", err)
	}
	var dirs []string
	for line := range strings.SplitSeq(string(raw), "\n") {
		if line != "" {
			dirs = append(dirs, line)
		}
	}
	for _, d := range dirs {
		if err := fetchOne(tf, d, stderr); err != nil {
			return err
		}
	}
	fmt.Fprintf(stderr, "provider-schema fetch: %d 版の schema を取りました\n", len(dirs))

	return nil
}

// fetchOne は、d で init と schema の取得を順に起動します。
func fetchOne(tf tfCommand, d string, stderr io.Writer) error {
	var initLog bytes.Buffer
	init := tf(d, "init")
	init.Stdout, init.Stderr = &initLog, &initLog
	initErr := init.Run()
	if err := os.WriteFile(filepath.Join(d, "init.log"), initLog.Bytes(), outPerm); err != nil {
		return xerrors.Wrapf(err, "%s の init.log の書き込み", d)
	}
	if initErr != nil {
		_, _ = stderr.Write(initLog.Bytes())

		return xerrors.Wrapf(errFetch, "%s の init が失敗しました: %v", d, initErr)
	}
	var schema bytes.Buffer
	cmd := tf(d, "schema")
	cmd.Stdout, cmd.Stderr = &schema, stderr
	if err := cmd.Run(); err != nil {
		return xerrors.Wrapf(errFetch, "%s の schema を取れませんでした: %v", d, err)
	}
	if err := atomicwrite.Apply(map[string]string{filepath.Join(d, rawName): schema.String()}, outPerm); err != nil {
		return xerrors.Wrapf(err, "%s への書き込み", filepath.Join(d, rawName))
	}

	return nil
}

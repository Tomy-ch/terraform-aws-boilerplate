package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// runConfig は、.editorconfig-checker.json が `{}` であることを確かめ、本走査の対象（-dry-run の一覧）が git の
// 追跡ファイルを覆っていることを [runCoverage] で突き合わせます。
func runConfig(config string, w io.Writer, tool toolFunc, env []string) error {
	if _, err := readCheckerConfig(config); err != nil {
		return err
	}
	r, err := toolrun.Run(tool(".", checkerEnv(env), stepDryRun))
	if err != nil {
		return err
	}
	if r.Code != 0 {
		return xerrors.Wrapf(errTool, "editorconfig-checker -dry-run が rc=%d で終わりました\n%s", r.Code, r.Combined)
	}
	scanned := dryRunPaths(r.Stdout)
	fmt.Fprintf(w, "editorconfig-check: 走査対象 %d ファイル\n", len(scanned))

	return runCoverage(scanned, w, tool, env)
}

// readCheckerConfig は、空白を除いて `{}` である p の中身を返します。読めなければ errRead、`{}` でなければ
// errScope です。
func readCheckerConfig(p string) ([]byte, error) {
	raw, err := os.ReadFile(filepath.Clean(p))
	if err != nil {
		return nil, xerrors.Wrapf(errRead, "%v", err)
	}
	if !isEmptyObject(string(raw)) {
		return nil, xerrors.Wrapf(errScope, "%s", p)
	}

	return raw, nil
}

func isEmptyObject(s string) bool {
	return strings.Join(strings.Fields(s), "") == "{}"
}

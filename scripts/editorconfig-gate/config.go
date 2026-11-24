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

// runConfig は、.editorconfig-checker.json が `{}` であることと、本走査の対象（-dry-run の一覧）が1件以上あることを
// 確かめます。
func runConfig(config string, w io.Writer, tool toolFunc, env []string) error {
	if _, err := readCheckerConfig(config); err != nil {
		return err
	}
	r, err := toolrun.Run(tool(".", env, stepDryRun))
	if err != nil {
		return err
	}
	if r.Code != 0 {
		return xerrors.Wrapf(errTool, "editorconfig-checker -dry-run が rc=%d で終わりました\n%s", r.Code, r.Combined)
	}
	n := countFiles(r.Stdout)
	if n == 0 {
		return xerrors.Wrap(errNoInput, "editorconfig-checker -dry-run の走査対象のファイルが0件です")
	}
	fmt.Fprintf(w, "editorconfig-check: %d ファイル\n", n)

	return nil
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

// countFiles は、-dry-run の出力（1行1パス）の、色の制御列と空白を除いて空でない行を数えます。
func countFiles(out string) int {
	n := 0
	for _, line := range strings.Split(stripANSI(out), "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}

	return n
}

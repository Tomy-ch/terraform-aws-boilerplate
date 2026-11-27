package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// versionLine は、text の行のうち prefix で始まる最初の行の残りを返します。
func versionLine(text, prefix string) (string, bool) {
	for line := range strings.SplitSeq(text, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), prefix); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v), true
		}
	}

	return "", false
}

// runOpaParity は、`conftest --version` の OPA の版と、`opa version` の版を突き合わせます。Rego のテストを走らせる
// opa と、適用で評価する Conftest の内蔵の OPA が違うと、テストが通ったのと別の意味論で適用することになります。
func runOpaParity(args []string, w io.Writer) error {
	fset := flag.NewFlagSet("tf-policy opa-parity", flag.ContinueOnError)
	fset.SetOutput(w)
	conftestPath := fset.String("conftest", "", "`conftest --version` の出力")
	opaPath := fset.String("opa", "", "`opa version` の出力")
	if err := fset.Parse(args); err != nil {
		return xerrors.Wrapf(errUsage, "%v", err)
	}
	if *conftestPath == "" || *opaPath == "" || fset.NArg() > 0 {
		return xerrors.Wrap(errUsage, "tf-policy opa-parity -conftest <path> -opa <path>")
	}
	embedded, err := readVersion(*conftestPath, "OPA:")
	if err != nil {
		return err
	}
	cli, err := readVersion(*opaPath, "Version:")
	if err != nil {
		return err
	}
	if embedded != cli {
		return xerrors.Wrapf(errFailed, "opa の版 %s が、Conftest の内蔵の OPA の版 %s と違います（mise.toml の opa を Conftest に合わせる）", cli, embedded)
	}
	fmt.Fprintf(w, "✅ opa %s は Conftest の内蔵の OPA と同じ版です\n", cli)

	return nil
}

func readVersion(p, prefix string) (string, error) {
	raw, err := os.ReadFile(filepath.Clean(p))
	if err != nil {
		return "", xerrors.Wrapf(errRead, "%s: %v", p, err)
	}
	v, ok := versionLine(string(raw), prefix)
	if !ok {
		return "", xerrors.Wrapf(errRead, "%s に %q の行がありません: %s", p, prefix, strings.TrimSpace(string(raw)))
	}

	return v, nil
}

package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/atomicwrite"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/workdir"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// canaryRole は、AWS Provider が非推奨とする引数を足す前の、canary の構成です。
const canaryRole = "resource \"aws_iam_role\" \"this\" {\n  name               = \"canary\"\n  assume_role_policy = \"{}\"\n%s}\n"

// canaryDeprecated は、canary が書く非推奨の引数と、その警告の位置です。
const (
	canaryDeprecated = "  managed_policy_arns = []\n"
	canaryWarning    = `usecases/x/reference/main.tf:4:25: [warning] Argument is deprecated（resource "aws_iam_role" "this"）`
)

// runValidateCanary は、実物の terraform と AWS Provider で validate と判定の全体を走らせ、非推奨の引数で落ち、
// 宣言に当たる警告と非推奨の無い構成で通ることを確かめます（ADR-0501 決定13）。Provider の版は -reference の
// lock に揃えます。抑止の宣言の detail は、canary の出力から取ります（版で文言が変わるため）。[errTool] の
// ときは、ケースの判定に畳まず [errTool] を返し、それまでに落ちたケースは文面にだけ添えます。root はリポジトリ直下です。
func runValidateCanary(args []string, w io.Writer, tool toolFunc, root string) error {
	fset := flag.NewFlagSet("tf-gates validate の canary", flag.ContinueOnError)
	fset.SetOutput(w)
	data := fset.String("data", "", "TF_DATA_DIR を置くディレクトリ（絶対パス）")
	work := fset.String("work", "", "fixture を建てるディレクトリ（中身は毎回作り直す）")
	reference := fset.String("reference", "", "versions.tf と .terraform.lock.hcl を写す reference の root")
	if err := fset.Parse(args); err != nil {
		return xerrors.Wrapf(errUsage, "%v", err)
	}
	if *data == "" || *work == "" || *reference == "" || fset.NArg() > 0 || !filepath.IsAbs(*data) {
		return xerrors.Wrap(errUsage, "canary -data <絶対パス> -work <tmp/ の下の相対パス> -reference <dir>")
	}
	workDir, err := workdir.Resolve(root, *work)
	if err != nil {
		return xerrors.Wrapf(errUsage, "-work %v", err)
	}
	if err := os.RemoveAll(workDir); err != nil {
		return xerrors.Wrapf(err, "%s の削除", workDir)
	}
	empty := filepath.Join(workDir, "ignore-empty.yaml")
	if err := writeText(empty, "warnings: []\n"); err != nil {
		return err
	}

	var failed []string
	check := func(name string, ok bool, out string) {
		if !ok {
			failed = append(failed, name)
			fmt.Fprintf(w, "--- canary %s の出力:\n%s\n", name, out)
		}
	}
	aborted := func(name string, err error) error {
		if len(failed) == 0 {
			return err
		}

		return xerrors.Wrapf(err, "canary %s で打ち切りました（それまでに落ちたケース: %s）", name, strings.Join(failed, " "))
	}

	clean, err := canaryCase(workDir, "clean", *reference, "")
	if err != nil {
		return err
	}
	out, err := validateFixture(clean, *data, empty, tool)
	if xerrors.Is(err, errTool) {
		return aborted("clean", err)
	}
	check("clean", err == nil && strings.Contains(out, "1 ディレクトリを validate し"), out)

	deprecated, err := canaryCase(workDir, "deprecated", *reference, canaryDeprecated)
	if err != nil {
		return err
	}
	out, err = validateFixture(deprecated, *data, empty, tool)
	if xerrors.Is(err, errTool) {
		return aborted("deprecated", err)
	}
	check("deprecated", xerrors.Is(err, errFailed) && strings.Contains(out, canaryWarning), out)

	detail, err := onlyWarningDetail(filepath.Join(deprecated, "tmp", "tf-validate", "1.json"))
	if err != nil {
		check("detail", false, err.Error())
	} else {
		ignore := filepath.Join(workDir, "ignore-aws.yaml")
		if err := writeIgnore(ignore, detail); err != nil {
			return err
		}
		out, err = validateFixture(deprecated, *data, ignore, tool)
		if xerrors.Is(err, errTool) {
			return aborted("ignored", err)
		}
		check("ignored", err == nil && strings.Contains(out, "抑止 1 件"), out)
	}

	if len(failed) > 0 {
		return xerrors.Wrapf(errCanary, "tf-validate の canary が期待どおりでなかったケース: %s", strings.Join(failed, " "))
	}
	fmt.Fprintln(w, "✅ tf-validate は AWS Provider の非推奨の引数で落ち、宣言に当たる警告と非推奨の無い構成で通る")

	return nil
}

// validateCanary は、root をリポジトリ直下として runValidateCanary を走らせる canary です。
func validateCanary(root string) canaryFunc {
	return func(args []string, w io.Writer, tool toolFunc) error { return runValidateCanary(args, w, tool, root) }
}

// canaryCase は、<work>/<name> に reference の版を写した root を建て、その基点を返します。
func canaryCase(work, name, reference, extra string) (string, error) {
	base := filepath.Join(work, name)
	root := filepath.Join(base, "usecases", "x", "reference")
	if err := os.MkdirAll(root, 0o750); err != nil {
		return "", xerrors.Wrapf(err, "%s の作成", root)
	}
	for _, f := range []string{"versions.tf", ".terraform.lock.hcl"} {
		raw, err := os.ReadFile(filepath.Clean(filepath.Join(reference, f)))
		if err != nil {
			return "", xerrors.Wrapf(err, "%s の読み取り", filepath.Join(reference, f))
		}
		if err := writeText(filepath.Join(root, f), string(raw)); err != nil {
			return "", err
		}
	}
	if err := writeText(filepath.Join(root, "main.tf"), fmt.Sprintf(canaryRole, extra)); err != nil {
		return "", err
	}

	return base, nil
}

// validateFixture は、base の init・validate・判定を、検査と同じ関数で走らせ、判定の出力を返します。
func validateFixture(base, data, ignore string, tool toolFunc) (string, error) {
	var out bytes.Buffer
	targets, err := requireDirs(scan{base: base, roots: []string{"usecases"}, log: &out})
	if err != nil {
		return "", err
	}
	if err := initTargets(data, targets, &out, tool); err != nil {
		return out.String(), err
	}
	results := filepath.Join(base, "tmp", "tf-validate")
	if err := validateTargets(data, results, targets, tool); err != nil {
		return out.String(), err
	}
	err = runResult([]string{"-base", base, "-manifest", filepath.Join(results, "manifest.tsv"), "-ignore", ignore}, &out)

	return out.String(), err
}

// onlyWarningDetail は、validate の出力の警告がちょうど1件なら、その detail を返します。
func onlyWarningDetail(p string) (string, error) {
	raw, err := os.ReadFile(filepath.Clean(p))
	if err != nil {
		return "", xerrors.Wrapf(errCanary, "%s を読めません: %v", p, err)
	}
	var out struct {
		Diagnostics []diagnostic `json:"diagnostics"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", xerrors.Wrapf(errCanary, "%s を読めません: %v", p, err)
	}
	var details []string
	for _, d := range out.Diagnostics {
		if d.Severity == "warning" {
			details = append(details, d.Detail)
		}
	}
	if len(details) != 1 {
		return "", xerrors.Wrapf(errCanary, "%s の警告が %d 件です（1件を期待）", p, len(details))
	}

	return details[0], nil
}

// writeIgnore は、canary の警告1件に当たる抑止の宣言を書きます。
func writeIgnore(p, detail string) error {
	raw, err := yaml.Marshal(ignoreFile{Warnings: &[]ignoreEntry{{
		Summary: "Argument is deprecated", Path: "usecases/x/reference/main.tf", Block: `resource "aws_iam_role" "this"`,
		Detail: detail, Occurrences: 1, Reason: "canary", Withdrawal: "canary",
	}}})
	if err != nil {
		return xerrors.Wrap(err, "抑止の宣言の組み立て")
	}

	return writeText(p, string(raw))
}

func writeText(p, body string) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return xerrors.Wrapf(err, "%s の作成", filepath.Dir(p))
	}
	if err := atomicwrite.Apply(map[string]string{p: body}, 0o600); err != nil {
		return xerrors.Wrapf(err, "%s の書き込み", p)
	}

	return nil
}

package main

import (
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// runCheck は、2つの canary・設定と走査の対象の確認・本走査を、この順に走らせます。前の段が落ちれば、後の段を
// 起動しません。-work は終わりに消します。
// root はリポジトリ直下で、-work はその下の tmp/ の下に限ります。
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
	base, err := workDir(root, *work)
	if err != nil {
		return err
	}
	gitEnv, err := env()
	if err != nil {
		return xerrors.Wrapf(errTool, "%v", err)
	}
	defer func() { _ = os.RemoveAll(base) }()
	fixture := func(sub string) fixturePaths {
		return fixturePaths{work: filepath.Join(base, sub), editorconfig: *editorconfig, checkerConfig: *checkerConfig}
	}
	if err := runCheckCanary(fixture("check"), w, tool, gitEnv); err != nil {
		return err
	}
	if err := runDirectiveCanary(fixture("directive"), w, tool, gitEnv); err != nil {
		return err
	}
	if err := runConfig(*checkerConfig, w, tool, gitEnv); err != nil {
		return err
	}

	return runScan(w, tool, gitEnv)
}

// runScan は、リポジトリ直下で本走査（引数なしの editorconfig-checker）を走らせ、出力を w へ写します。0 でない
// 終了コードは errFailed です。
func runScan(w io.Writer, tool toolFunc, env []string) error {
	r, err := runTool(tool(".", env, stepCheck))
	fmt.Fprint(w, r.combined)
	if err != nil {
		return err
	}
	if r.code != 0 {
		return xerrors.Wrapf(errFailed, "editorconfig-checker が rc=%d で終わりました", r.code)
	}
	fmt.Fprintln(w, "✅ editorconfig-check: .editorconfig の宣言との不一致はありません")

	return nil
}

// workDir は、-work の値 work を検証し、root からのパスを返します。work はリポジトリ直下の tmp/ の下の相対パスに
// 限ります（絶対パス・`..` を含むもの・tmp/ 自身は errUsage）。fixture は毎回 RemoveAll で作り直すので、ほかの
// 場所を名指せば消してしまいます。字面が tmp/ の下でも、途中の symlink が外を指せば errUsage です。
func workDir(root, work string) (string, error) {
	slashed := filepath.ToSlash(work)
	clean := path.Clean(slashed)
	if work == "" || filepath.IsAbs(work) || strings.HasPrefix(slashed, "/") || slices.Contains(strings.Split(slashed, "/"), "..") ||
		!strings.HasPrefix(clean, "tmp/") {
		return "", xerrors.Wrapf(errUsage, "-work はリポジトリ直下の tmp/ の下の相対パスに限ります: %q", work)
	}
	target := filepath.Join(root, filepath.FromSlash(clean))
	if err := staysUnder(filepath.Join(root, "tmp"), target); err != nil {
		return "", xerrors.Wrapf(errUsage, "-work %q: %v", work, err)
	}

	return target, nil
}

// staysUnder は、target の在る祖先のうち最も深いものの symlink を解いた先が、base の symlink を解いた先の下に
// あることを確かめます。どちらも絶対パスへ直して比べます。base が無ければ、その下に symlink も無いので確かめません。
func staysUnder(base, target string) error {
	base, err := filepath.Abs(base)
	if err != nil {
		return err
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(base); xerrors.Is(err, fs.ErrNotExist) {
		return nil
	}
	baseReal, err := filepath.EvalSymlinks(base)
	if err != nil {
		return err
	}
	existing := target
	for {
		_, err := os.Lstat(existing)
		if err == nil {
			break
		}
		if !xerrors.Is(err, fs.ErrNotExist) {
			return err
		}
		existing = filepath.Dir(existing)
	}
	resolved, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(baseReal, resolved)
	if err != nil {
		return err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || (rel == "." && existing == target) {
		return xerrors.Newf("%s は %s の下にありません", resolved, baseReal)
	}

	return nil
}

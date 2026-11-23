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

// target は対象のディレクトリです。rel は走査の基点からの `/` 区切りの相対パス、path は基点を前置したパスです。
type target struct {
	rel  string
	path string
}

// listDirs は、roots の下の `.tf` を持つディレクトリを、相対パスの辞書順で返します。段に `tests` か `.terraform`
// を持つディレクトリは除きます。無い root は飛ばし（対象が0件なら呼び出し側が落とす）、ディレクトリでない root は
// errMissingRoot です。
func listDirs(base string, roots []string) ([]target, error) {
	seen := map[string]bool{}
	for _, root := range roots {
		dir := filepath.Join(base, filepath.FromSlash(root))
		info, err := os.Stat(dir)
		switch {
		case xerrors.Is(err, fs.ErrNotExist):
			continue
		case err != nil:
			return nil, xerrors.Wrapf(errMissingRoot, "%s: %v", root, err)
		case !info.IsDir():
			return nil, xerrors.Wrapf(errMissingRoot, "%s はディレクトリではありません", root)
		}
		err = filepath.WalkDir(dir, func(p string, e fs.DirEntry, err error) error {
			switch {
			case err != nil:
				return err
			case e.IsDir() && (e.Name() == ".terraform" || e.Name() == "tests"):
				return filepath.SkipDir
			case e.IsDir() || !strings.HasSuffix(e.Name(), ".tf"):
				return nil
			}
			rel, err := filepath.Rel(base, filepath.Dir(p))
			if err != nil {
				return err
			}
			seen[filepath.ToSlash(rel)] = true

			return nil
		})
		if err != nil {
			return nil, xerrors.Wrapf(errWalk, "%s: %v", root, err)
		}
	}
	rels := make([]string, 0, len(seen))
	for r := range seen {
		rels = append(rels, r)
	}
	slices.Sort(rels)
	targets := make([]target, 0, len(rels))
	for _, r := range rels {
		targets = append(targets, target{rel: r, path: filepath.Join(base, filepath.FromSlash(r))})
	}

	return targets, nil
}

// requireDirs は listDirs の結果が0件なら errNoTargets にします。
func requireDirs(base string, roots []string) ([]target, error) {
	targets, err := listDirs(base, roots)
	if err != nil {
		return nil, err
	}
	if len(targets) == 0 {
		return nil, xerrors.Wrapf(errNoTargets, "Terraform の対象ディレクトリが0件です（走査: %s）", strings.Join(roots, " "))
	}

	return targets, nil
}

// isRoot は、相対パスの形が root か（env/<x>、env/<x>/identity、usecases/<x>/reference）を返します
// （ADR-0604 決定5、ADR-0102 決定12）。
func isRoot(rel string) bool {
	seg := strings.Split(rel, "/")
	switch {
	case len(seg) == 2 && seg[0] == "env":
		return true
	case len(seg) == 3 && seg[0] == "env" && seg[2] == "identity":
		return true
	case len(seg) == 3 && seg[0] == "usecases" && seg[2] == "reference":
		return true
	}

	return false
}

// dataDir は、ディレクトリごとの TF_DATA_DIR です。別のディレクトリが同じ名前にならないよう、_ を先にエスケープ
// します。検査の init は root の .terraform を使わない —— 手元で実 AWS の backend に init した root は、
// .terraform に backend の設定を残し、-backend=false の init がその資格情報を探して落ちる。
func dataDir(data, rel string) string {
	return filepath.Join(data, strings.NewReplacer("_", "_u", "/", "__").Replace(rel))
}

// hasTests は、ディレクトリが直下か tests/ に *.tftest.hcl を持つかを返します。
func hasTests(dir string) (bool, error) {
	for _, d := range []string{dir, filepath.Join(dir, "tests")} {
		entries, err := os.ReadDir(d)
		switch {
		case xerrors.Is(err, fs.ErrNotExist):
			continue
		case err != nil:
			return false, xerrors.Wrapf(errWalk, "%s: %v", d, err)
		}
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".tftest.hcl") {
				return true, nil
			}
		}
	}

	return false, nil
}

// parseCommon は、-base と root を持つサブコマンドの引数を読みます。extra は各サブコマンドのフラグを足します。
func parseCommon(name string, args []string, w io.Writer, extra func(*flag.FlagSet)) (string, []string, error) {
	fset := flag.NewFlagSet("tf-gates "+name, flag.ContinueOnError)
	fset.SetOutput(w)
	base := fset.String("base", "", "走査の基点（TF_BASE）")
	if extra != nil {
		extra(fset)
	}
	if err := fset.Parse(args); err != nil {
		return "", nil, xerrors.Wrapf(errUsage, "%v", err)
	}
	if *base == "" || fset.NArg() == 0 {
		return "", nil, xerrors.Wrapf(errUsage, "tf-gates %s は -base と root が要ります", name)
	}
	roots := fset.Args()
	for i, r := range roots {
		roots[i] = path.Clean(r)
	}

	return *base, roots, nil
}

func runDirs(args []string, w io.Writer) error {
	base, roots, err := parseCommon("dirs", args, w, nil)
	if err != nil {
		return err
	}
	targets, err := requireDirs(base, roots)
	if err != nil {
		return err
	}
	for _, t := range targets {
		fmt.Fprintln(w, t.path)
	}

	return nil
}

// runNone は、対象が0件なら成功し、在れば errTargets で失敗します（lefthook の skip の条件）。
func runNone(args []string, w io.Writer) error {
	base, roots, err := parseCommon("none", args, w, nil)
	if err != nil {
		return err
	}
	targets, err := listDirs(base, roots)
	if err != nil {
		return err
	}
	if len(targets) > 0 {
		return xerrors.Wrapf(errTargets, "%d 件", len(targets))
	}

	return nil
}

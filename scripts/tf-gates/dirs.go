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

// scan は走査の範囲です。allowMissing が true のときだけ、無い root を飛ばします。既定は、宣言した root が
// 1つでも無ければ落とします —— 黙って範囲が縮むと、対象が減ったことと合格が区別できない（ADR-0702 決定13）。
// 飛ばすのは、root の一部だけを持つ fixture で検査を走らせる canary のためです。
type scan struct {
	base         string
	roots        []string
	allowMissing bool
	log          io.Writer
}

// listDirs は、roots の下の `.tf` を持つディレクトリを、相対パスの辞書順で返します。段に `tests` か `.terraform`
// を持つディレクトリは除きます。無い root は allowMissing のときだけ飛ばし、飛ばした root を log に名指します。
// ディレクトリでない root は errMissingRoot です。
func listDirs(s scan) ([]target, error) {
	base, roots := s.base, s.roots
	seen := map[string]bool{}
	for _, root := range roots {
		dir := filepath.Join(base, filepath.FromSlash(root))
		info, err := os.Stat(dir)
		switch {
		case xerrors.Is(err, fs.ErrNotExist) && s.allowMissing:
			fmt.Fprintf(s.log, "走査の root %s が無いので飛ばします（%s）\n", root, dir)

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

// requireDirs は listDirs の結果が0件なら errNoTargets にします。root を全部飛ばした結果の0件も同じです。
func requireDirs(s scan) ([]target, error) {
	targets, err := listDirs(s)
	if err != nil {
		return nil, err
	}
	if len(targets) == 0 {
		return nil, xerrors.Wrapf(errNoTargets, "Terraform の対象ディレクトリが0件です（走査: %s）", strings.Join(s.roots, " "))
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

// parseCommon は、-base・-allow-missing-roots と root を持つサブコマンドの引数を読みます。extra は各サブコマンドの
// フラグを足します。
func parseCommon(name string, args []string, w io.Writer, extra func(*flag.FlagSet)) (scan, error) {
	fset := flag.NewFlagSet("tf-gates "+name, flag.ContinueOnError)
	fset.SetOutput(w)
	base := fset.String("base", "", "走査の基点（TF_BASE）")
	allow := fset.Bool("allow-missing-roots", false, "無い root を、名指したうえで飛ばす（root の一部だけを持つ canary の fixture のため）")
	if extra != nil {
		extra(fset)
	}
	if err := fset.Parse(args); err != nil {
		return scan{}, xerrors.Wrapf(errUsage, "%v", err)
	}
	if *base == "" || fset.NArg() == 0 {
		return scan{}, xerrors.Wrapf(errUsage, "tf-gates %s は -base と root が要ります", name)
	}
	roots := fset.Args()
	for i, r := range roots {
		roots[i] = path.Clean(r)
	}

	return scan{base: *base, roots: roots, allowMissing: *allow, log: w}, nil
}

func runDirs(args []string, w io.Writer) error {
	s, err := parseCommon("dirs", args, w, nil)
	if err != nil {
		return err
	}
	targets, err := requireDirs(s)
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
	s, err := parseCommon("none", args, w, nil)
	if err != nil {
		return err
	}
	targets, err := listDirs(s)
	if err != nil {
		return err
	}
	if len(targets) > 0 {
		return xerrors.Wrapf(errTargets, "%d 件", len(targets))
	}

	return nil
}

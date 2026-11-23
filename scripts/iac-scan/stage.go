package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

type scanArgs struct {
	base  string
	roots []string
}

// parseScan は、-base と、1つ以上の走査の root を読みます。extra はサブコマンドに固有の flag を足します。
func parseScan(name string, args []string, w io.Writer, extra func(*flag.FlagSet)) (scanArgs, error) {
	f := flag.NewFlagSet("iac-scan "+name, flag.ContinueOnError)
	f.SetOutput(w)
	var s scanArgs
	f.StringVar(&s.base, "base", ".", "走査の root と作業ディレクトリ（tmp/）の基点")
	if extra != nil {
		extra(f)
	}
	if err := f.Parse(args); err != nil {
		return scanArgs{}, xerrors.Wrapf(errUsage, "%v", err)
	}
	s.roots = f.Args()
	if len(s.roots) == 0 {
		return scanArgs{}, xerrors.Wrapf(errUsage, "iac-scan %s は走査の root が要ります", name)
	}

	return s, nil
}

// readJSON は、p を JSON として読みます。数は json.Number のまま残します。
func readJSON(p string) (any, error) {
	raw, err := os.ReadFile(filepath.Clean(p))
	if err != nil {
		return nil, xerrors.Wrapf(errResult, "%v", err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, xerrors.Wrapf(errResult, "%s は JSON として読めません: %v", p, err)
	}

	return v, nil
}

func intField(m map[string]any, key string) (int64, error) {
	n, ok := m[key].(json.Number)
	if !ok {
		return 0, xerrors.Wrapf(errResult, "%s が数ではありません（%v）", key, m[key])
	}
	i, err := n.Int64()
	if err != nil {
		return 0, xerrors.Wrapf(errResult, "%s が整数ではありません（%s）", key, n)
	}

	return i, nil
}

// stageTerraform は、base の下の roots を dst へ同じ相対パスで写し、写したファイルの `/` 区切りの相対パスを返します。
// 名前がちょうど `.terraform` のものは写しません（`.terraform.lock.hcl` は写す）。宣言した root が1つでも無いとき、
// symlink か通常のファイルでないもの、読めないファイル、写したファイルが0件のときは成功させません（ADR-0702 決定13・14）。
func stageTerraform(base string, roots []string, dst string) ([]string, error) {
	var staged []string
	for _, root := range roots {
		if root == "" || filepath.IsAbs(root) || !filepath.IsLocal(root) {
			return nil, xerrors.Wrapf(errUsage, "走査の root %q は base からの相対パスにする", root)
		}
		dir := filepath.Join(base, filepath.FromSlash(root))
		info, err := os.Stat(dir)
		switch {
		case err != nil:
			return nil, xerrors.Wrapf(errMissingRoot, "%s: %v", root, err)
		case !info.IsDir():
			return nil, xerrors.Wrapf(errMissingRoot, "%s はディレクトリではありません", root)
		}
		err = filepath.WalkDir(dir, func(p string, e fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if e.Name() == ".terraform" {
				if e.IsDir() {
					return filepath.SkipDir
				}

				return nil
			}
			rel, err := filepath.Rel(base, p)
			if err != nil {
				return err
			}
			to := filepath.Join(dst, rel)
			switch {
			case e.IsDir():
				return os.MkdirAll(to, 0o750)
			case !e.Type().IsRegular():
				return xerrors.Newf("%s は通常のファイルではありません（%s）", p, e.Type())
			}
			staged = append(staged, filepath.ToSlash(rel))

			return copyFile(p, to)
		})
		if err != nil {
			return nil, xerrors.Wrapf(errInput, "%s を写せません: %v", root, err)
		}
	}
	if len(staged) == 0 {
		return nil, xerrors.Wrapf(errNoTargets, "%s の下に写すファイルがありません", strings.Join(roots, " "))
	}

	return staged, nil
}

func copyFile(src, dst string) (err error) {
	in, err := os.Open(filepath.Clean(src))
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(filepath.Clean(dst), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := out.Close(); err == nil {
			err = cerr
		}
	}()
	_, err = io.Copy(out, in)

	return err
}

func countSuffix(paths []string, suffixes ...string) int {
	n := 0
	for _, p := range paths {
		for _, s := range suffixes {
			if strings.HasSuffix(p, s) {
				n++

				break
			}
		}
	}

	return n
}

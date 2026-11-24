package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"io"
	"io/fs"
	"os"
	"path"
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

// readResult は、コンテナから取り出した結果 p を読みます。通常のファイルでなければ（symlink を含む）辿らずに失敗します。
func readResult(p string) ([]byte, error) {
	info, err := os.Lstat(p)
	if err != nil {
		return nil, xerrors.Wrapf(errResult, "%v", err)
	}
	if !info.Mode().IsRegular() {
		return nil, xerrors.Wrapf(errResult, "%s は通常のファイルではありません（%s）", p, info.Mode().Type())
	}
	raw, err := os.ReadFile(filepath.Clean(p))
	if err != nil {
		return nil, xerrors.Wrapf(errResult, "%v", err)
	}

	return raw, nil
}

// readJSON は、p を readResult で読み、JSON として解釈します。数は json.Number のまま残します。
func readJSON(p string) (any, error) {
	raw, err := readResult(p)
	if err != nil {
		return nil, err
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

// checkRoots は、roots を base の下の `/` 区切りの相対パスに整えて返します。base の外か base 自身を指す root、
// 重なる root（同じもの、一方が他方を含むもの）は使い方の誤り、ディレクトリとして無い root は errMissingRoot です。
func checkRoots(base string, roots []string) ([]string, error) {
	clean := make([]string, 0, len(roots))
	for _, root := range roots {
		c := path.Clean(filepath.ToSlash(root))
		if root == "" || filepath.IsAbs(root) || !filepath.IsLocal(root) || c == "." {
			return nil, xerrors.Wrapf(errUsage, "走査の root %q は base の下の相対パスにする", root)
		}
		for _, prev := range clean {
			if c == prev || strings.HasPrefix(c, prev+"/") || strings.HasPrefix(prev, c+"/") {
				return nil, xerrors.Wrapf(errUsage, "走査の root %q と %q が重なっています", prev, root)
			}
		}
		info, err := os.Stat(filepath.Join(base, filepath.FromSlash(c)))
		switch {
		case err != nil:
			return nil, xerrors.Wrapf(errMissingRoot, "%s: %v", root, err)
		case !info.IsDir():
			return nil, xerrors.Wrapf(errMissingRoot, "%s はディレクトリではありません", root)
		}
		clean = append(clean, c)
	}

	return clean, nil
}

// stageTerraform は、base の下の roots（checkRoots で確かめる）を dst へ同じ相対パスで写し、写したファイルの `/` 区切りの
// 相対パスを返します。名前がちょうど `.terraform` のものは写しません（`.terraform.lock.hcl` は写す）。symlink か通常の
// ファイルでないもの、読めないファイル、写したファイルが0件のときは成功させません（ADR-0702 決定13・14）。
func stageTerraform(base string, roots []string, dst string) ([]string, error) {
	roots, err := checkRoots(base, roots)
	if err != nil {
		return nil, err
	}
	var staged []string
	for _, root := range roots {
		dir := filepath.Join(base, filepath.FromSlash(root))
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
	defer func() {
		if err != nil {
			err = xerrors.Wrapf(errInput, "%s を %s へ写せません: %v", src, dst, err)
		}
	}()
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

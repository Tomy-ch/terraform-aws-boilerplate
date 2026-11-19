package main

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// walkFiles は、roots の下の `.tf` を、base からの `/` 区切りの相対パスで辞書順に返します。
// `.terraform` は init が落とした写しなので降りません。`.tftest.hcl` は読みません。
// symlink は、ファイルもディレクトリも root そのものも errSymlink です。`.tf.json` と、構成ファイルの
// 名前を持つ通常のファイルでないもの（`*.tf` の名前のディレクトリ）は errUnsupportedInput です。
func walkFiles(base string, roots []string) ([]string, error) {
	var files []string
	for _, root := range roots {
		dir := filepath.Join(base, filepath.FromSlash(root))
		info, err := os.Lstat(dir)
		switch {
		case err != nil:
			return nil, xerrors.Wrapf(errMissingRoot, "%s: %v", root, err)
		case info.Mode()&fs.ModeSymlink != 0:
			return nil, xerrors.Wrapf(errSymlink, "%s", root)
		case !info.IsDir():
			return nil, xerrors.Wrapf(errMissingRoot, "%s はディレクトリではありません", root)
		}
		err = filepath.WalkDir(dir, func(p string, e fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(base, p)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			name := e.Name()
			switch {
			case e.Type()&fs.ModeSymlink != 0:
				return xerrors.Wrapf(errSymlink, "%s", rel)
			case e.IsDir() && name == ".terraform":
				return filepath.SkipDir
			case strings.HasSuffix(name, ".tf.json"):
				return xerrors.Wrapf(errUnsupportedInput, "%s: .tf.json は読みません", rel)
			case strings.HasSuffix(name, ".tf") && !e.Type().IsRegular():
				return xerrors.Wrapf(errUnsupportedInput, "%s: 構成ファイルの名前を持つ、通常のファイルでないものがあります", rel)
			case strings.HasSuffix(name, ".tf"):
				files = append(files, rel)
			}

			return nil
		})
		if err != nil {
			return nil, xerrors.Wrapf(err, "%s の走査", root)
		}
	}
	slices.Sort(files)

	return slices.Compact(files), nil
}

// readListed は、find の出力（1行1パス）を、`./` を外した辞書順の重複なしの一覧で返します。空の行は
// 読み飛ばします。
func readListed(p string) ([]string, error) {
	raw, err := os.ReadFile(filepath.Clean(p))
	if err != nil {
		return nil, xerrors.Wrapf(errListed, "%s: %v", p, err)
	}
	var listed []string
	for line := range strings.SplitSeq(string(raw), "\n") {
		if line != "" {
			listed = append(listed, path.Clean(line))
		}
	}
	slices.Sort(listed)

	return slices.Compact(listed), nil
}

// compareFiles は、walked と listed（どちらも辞書順の重複なし）の過不足を、両向きとも errFileSetMismatch
// で名指します。
func compareFiles(walked, listed []string) error {
	var onlyWalked, onlyListed []string
	for _, f := range walked {
		if _, found := slices.BinarySearch(listed, f); !found {
			onlyWalked = append(onlyWalked, f)
		}
	}
	for _, f := range listed {
		if _, found := slices.BinarySearch(walked, f); !found {
			onlyListed = append(onlyListed, f)
		}
	}
	if len(onlyWalked)+len(onlyListed) == 0 {
		return nil
	}

	return xerrors.Wrapf(errFileSetMismatch, "一覧に無いファイルを読みました: %v、一覧のファイルを読んでいません: %v", onlyWalked, onlyListed)
}

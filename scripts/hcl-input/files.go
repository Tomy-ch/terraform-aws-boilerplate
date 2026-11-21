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

// configDirSuffixes は、ディレクトリの名前として持つことを errUnsupportedInput にする、構成ファイルの接尾辞です。
var configDirSuffixes = []string{".tf", ".tf.json", ".tftest.hcl", ".tftest.json"}

// parsedSuffixes は、構文として読むファイルの接尾辞です。
var parsedSuffixes = []string{".tf", ".tftest.hcl"}

// fileEntry は、列挙したファイルです。Parsed は構文として読んだかどうかで、読まない
// ファイルを未対応とするか対象外とするかは Rego が決めます。
type fileEntry struct {
	Path   string `json:"path"`
	Parsed bool   `json:"parsed"`
}

// walkAllFiles は、roots の下の通常のファイルすべてを、base からの `/` 区切りの相対パスで辞書順に返します。
// `.terraform` には降りません。symlink は errSymlink、ディレクトリでも通常のファイルでもないものは
// errUnsupportedInput です。
func walkAllFiles(base string, roots []string) ([]fileEntry, error) {
	var paths []string
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
			switch {
			case e.Type()&fs.ModeSymlink != 0:
				return xerrors.Wrapf(errSymlink, "%s", rel)
			case e.IsDir() && e.Name() == ".terraform":
				return filepath.SkipDir
			case e.IsDir() && hasSuffix(e.Name(), configDirSuffixes):
				return xerrors.Wrapf(errUnsupportedInput, "%s: 構成ファイルの名前を持つ、通常のファイルでないものがあります", rel)
			case e.IsDir():
				return nil
			case !e.Type().IsRegular():
				return xerrors.Wrapf(errUnsupportedInput, "%s: 通常のファイルでもディレクトリでもありません", rel)
			}
			paths = append(paths, rel)

			return nil
		})
		if err != nil {
			return nil, xerrors.Wrapf(err, "%s の走査", root)
		}
	}
	slices.Sort(paths)
	paths = slices.Compact(paths)
	entries := make([]fileEntry, 0, len(paths))
	for _, p := range paths {
		entries = append(entries, fileEntry{Path: p, Parsed: isParsed(p)})
	}

	return entries, nil
}

func isParsed(name string) bool { return hasSuffix(name, parsedSuffixes) }

func hasSuffix(name string, suffixes []string) bool {
	for _, s := range suffixes {
		if strings.HasSuffix(name, s) {
			return true
		}
	}

	return false
}

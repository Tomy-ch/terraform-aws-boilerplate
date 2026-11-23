package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/atomicwrite"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

var (
	// blockLine は、トップレベルの block の行頭です。terraform fmt の木では、トップレベルの block は行頭に在り、
	// 入れ子は字下げされます。ヒアドキュメントやブロックコメントの中の行頭の一致は数え過ぎになり、検査の入力の規則で
	// 落ちます（通す向きには壊れない）。
	blockLine = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*([ \t]+("[^"]*"|[A-Za-z_][A-Za-z0-9_-]*))*[ \t]*\{`)
	// moduleLine は、module block の行頭です。ラベルは引用符で囲むか識別子で書けます。
	moduleLine = regexp.MustCompile(`^[ \t]*module[ \t]+("|[A-Za-z_])`)
	// keyPattern は、data のトップキーに使ってよい名前です。
	keyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
)

// hclCounts は `<out>/hcl-counts.json` です。conftest/hcl/lib/hcl_input.rego が読みます。
type hclCounts struct {
	ParsedFiles []string       `json:"hcl_parsed_files"`
	BlockCounts map[string]int `json:"hcl_block_counts"`
}

func runInputs(args []string, out io.Writer) error {
	fset := flag.NewFlagSet("hcl-tally inputs", flag.ContinueOnError)
	fset.SetOutput(out)
	base := fset.String("base", "", "走査の root と、書くパスの起点のディレクトリ")
	outDir := fset.String("out", "", "found.txt・hcl-counts.json・files.json を書くディレクトリ")
	filesKey := fset.String("files-key", "", "構成ファイルの一覧を files.json に書くときのトップキー")
	moduleKey := fset.String("module-counts-key", "", "module block の行頭の数を files.json に書くときのトップキー（-files-key と一緒に渡す）")
	if err := fset.Parse(args); err != nil {
		return xerrors.Wrapf(errUsage, "%v", err)
	}
	roots := fset.Args()
	switch {
	case *base == "" || *outDir == "" || len(roots) == 0:
		return xerrors.Wrap(errUsage, "hcl-tally inputs -base <dir> -out <dir> <root>...")
	case *moduleKey != "" && *filesKey == "":
		return xerrors.Wrap(errUsage, "-module-counts-key は -files-key と一緒に渡す")
	case *filesKey != "" && !keyPattern.MatchString(*filesKey), *moduleKey != "" && !keyPattern.MatchString(*moduleKey):
		return xerrors.Wrap(errUsage, "トップキーは英小文字・数字・_ にする")
	case *filesKey != "" && *filesKey == *moduleKey:
		return xerrors.Wrap(errUsage, "-files-key と -module-counts-key に同じ名前を渡している")
	}

	found, err := listFiles(*base, roots)
	if err != nil {
		return err
	}
	var configs, parsed, tf []string
	for _, p := range found {
		switch {
		case strings.HasSuffix(p, ".tf"):
			configs, parsed, tf = append(configs, p), append(parsed, p), append(tf, p)
		case strings.HasSuffix(p, ".tf.json"):
			configs = append(configs, p)
		case strings.HasSuffix(p, ".tftest.hcl"):
			parsed = append(parsed, p)
		}
	}
	if len(tf) == 0 {
		return xerrors.Wrapf(errNoInput, "%s", strings.Join(roots, " "))
	}

	counts := hclCounts{ParsedFiles: parsed, BlockCounts: map[string]int{}}
	for _, p := range parsed {
		if counts.BlockCounts[p], err = countLines(filepath.Join(*base, filepath.FromSlash(p)), blockLine); err != nil {
			return err
		}
	}
	changes := map[string]string{
		filepath.Join(*outDir, "found.txt"): joinLines(found),
	}
	if changes[filepath.Join(*outDir, "hcl-counts.json")], err = marshal(counts); err != nil {
		return err
	}
	if *filesKey != "" {
		doc := map[string]any{*filesKey: configs}
		if *moduleKey != "" {
			modules := map[string]int{}
			for _, p := range tf {
				if modules[p], err = countLines(filepath.Join(*base, filepath.FromSlash(p)), moduleLine); err != nil {
					return err
				}
			}
			doc[*moduleKey] = modules
		}
		if changes[filepath.Join(*outDir, "files.json")], err = marshal(doc); err != nil {
			return err
		}
	}
	if err := atomicwrite.Apply(changes, outPerm); err != nil {
		return xerrors.Wrapf(err, "%s への書き込み", *outDir)
	}
	fmt.Fprintf(out, "✅ hcl-tally inputs: ファイル %d 件（構成ファイル %d 件、構文として読むもの %d 件）を %s へ数えました\n",
		len(found), len(configs), len(parsed), *outDir)

	return nil
}

// listFiles は、roots の下の通常のファイルを、base からの `/` 区切りの相対パスで辞書順に返します。`.terraform` の
// 下には降りません。通常のファイルでないもの（symlink など）は列挙しません —— それを落とすのは hcl-input で、
// ここが数えないことで、両者の一覧が食い違って現れます。
func listFiles(base string, roots []string) ([]string, error) {
	var files []string
	for _, root := range roots {
		dir := filepath.Join(base, filepath.FromSlash(root))
		info, err := os.Lstat(dir)
		switch {
		case err != nil:
			return nil, xerrors.Wrapf(errMissingRoot, "%s: %v", root, err)
		case !info.IsDir():
			return nil, xerrors.Wrapf(errMissingRoot, "%s はディレクトリではありません", root)
		}
		err = filepath.WalkDir(dir, func(p string, e fs.DirEntry, err error) error {
			switch {
			case err != nil:
				return err
			case e.IsDir() && e.Name() == ".terraform":
				return filepath.SkipDir
			case !e.Type().IsRegular():
				return nil
			}
			rel, err := filepath.Rel(base, p)
			if err != nil {
				return err
			}
			files = append(files, filepath.ToSlash(rel))

			return nil
		})
		if err != nil {
			return nil, xerrors.Wrapf(errWalk, "%s: %v", root, err)
		}
	}
	slices.Sort(files)

	return slices.Compact(files), nil
}

// countLines は、p の行のうち re に当たるものの数を返します。
func countLines(p string, re *regexp.Regexp) (int, error) {
	f, err := os.Open(filepath.Clean(p))
	if err != nil {
		return 0, xerrors.Wrapf(errWalk, "%s: %v", p, err)
	}
	defer func() { _ = f.Close() }()
	n := 0
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		if re.MatchString(sc.Text()) {
			n++
		}
	}
	if err := sc.Err(); err != nil {
		return 0, xerrors.Wrapf(errWalk, "%s: %v", p, err)
	}

	return n, nil
}

func joinLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}

	return strings.Join(lines, "\n") + "\n"
}

func marshal(v any) (string, error) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", xerrors.Wrap(err, "JSON の組み立て")
	}

	return string(data) + "\n", nil
}

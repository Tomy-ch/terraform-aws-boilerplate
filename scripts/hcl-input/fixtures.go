package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/tools/txtar"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/atomicwrite"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// fixtureDoc は fixtures の JSON です。キーはケースの名前（txtar のファイル名から .txtar を除いたもの）。
type fixtureDoc struct {
	Fixtures map[string]syntaxDoc `json:"fixtures"`
}

// runFixtures は、-dir の `*.txtar` の1ファイルを1つの木として一時ディレクトリに建て、`-format syntax` と
// 同じ経路（buildSyntax）で読んで -out へ書きます。txtar の0件、ファイルの0件のケースは errFixture です。
func runFixtures(args []string, out io.Writer) error {
	fset := flag.NewFlagSet("hcl-input fixtures", flag.ContinueOnError)
	fset.SetOutput(out)
	dir := fset.String("dir", "", "fixture の txtar を置いたディレクトリ")
	outPath := fset.String("out", "", "書き出す JSON のパス")
	if err := fset.Parse(args); err != nil {
		return xerrors.Wrapf(errUsage, "%v", err)
	}
	if *dir == "" || *outPath == "" || fset.NArg() > 0 {
		return xerrors.Wrap(errUsage, "hcl-input fixtures -dir <dir> -out <path>")
	}
	cases, err := filepath.Glob(filepath.Join(*dir, "*.txtar"))
	if err != nil {
		return xerrors.Wrapf(errFixture, "%v", err)
	}
	if len(cases) == 0 {
		return xerrors.Wrapf(errFixture, "%s に txtar が1件もありません", *dir)
	}
	slices.Sort(cases)

	doc := fixtureDoc{Fixtures: map[string]syntaxDoc{}}
	for _, c := range cases {
		files, err := readCase(c)
		if err != nil {
			return err
		}
		got, err := buildCase(files)
		if err != nil {
			return xerrors.Wrapf(err, "%s", c)
		}
		doc.Fixtures[strings.TrimSuffix(filepath.Base(c), ".txtar")] = got
	}
	data, err := marshalIndent(doc)
	if err != nil {
		return err
	}
	if err := atomicwrite.Apply(map[string]string{*outPath: string(data)}, outPerm); err != nil {
		return xerrors.Wrapf(err, "%s への書き込み", *outPath)
	}
	fmt.Fprintf(out, "✅ hcl-input fixtures: %d 件のケースを %s へ書きました\n", len(doc.Fixtures), *outPath)

	return nil
}

// readCase は txtar の1ケースを、木の中の相対パスから中身への対応で返します。パスは `/` 区切りの、
// ディレクトリを1段以上持つ相対パスに限ります（最初の段を走査の root にするため）。
func readCase(p string) (map[string]string, error) {
	a, err := txtar.ParseFile(p)
	if err != nil {
		return nil, xerrors.Wrapf(errFixture, "%s: %v", p, err)
	}
	if len(a.Files) == 0 {
		return nil, xerrors.Wrapf(errFixture, "%s: ファイルが1件もありません", p)
	}
	files := map[string]string{}
	for _, f := range a.Files {
		name := f.Name
		switch {
		case name == "" || path.IsAbs(name) || path.Clean(name) != name || strings.HasPrefix(name, "../") || name == "..":
			return nil, xerrors.Wrapf(errFixture, "%s: 木の中の相対パスでない名前 %q", p, name)
		case !strings.Contains(name, "/"):
			return nil, xerrors.Wrapf(errFixture, "%s: ディレクトリを持たない名前 %q", p, name)
		}
		if _, dup := files[name]; dup {
			return nil, xerrors.Wrapf(errFixture, "%s: 同じ名前 %q が2つあります", p, name)
		}
		files[name] = string(f.Data)
	}

	return files, nil
}

// buildCase は files を一時ディレクトリに建て、最初の段のディレクトリを走査の root、files の名前の一覧を
// 独立の一覧として buildSyntax に渡します。
func buildCase(files map[string]string) (syntaxDoc, error) {
	base, err := os.MkdirTemp("", "hcl-input-fixture-")
	if err != nil {
		return syntaxDoc{}, xerrors.Wrapf(errFixture, "一時ディレクトリ: %v", err)
	}
	defer os.RemoveAll(base)

	var listed, roots []string
	for name, body := range files {
		p := filepath.Join(base, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			return syntaxDoc{}, xerrors.Wrapf(errFixture, "%s を置けません（ファイルとディレクトリの名前の衝突など）: %v", name, err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			return syntaxDoc{}, xerrors.Wrapf(errFixture, "%s を置けません（ファイルとディレクトリの名前の衝突など）: %v", name, err)
		}
		listed = append(listed, name)
		roots = append(roots, strings.SplitN(name, "/", 2)[0])
	}
	slices.Sort(listed)
	slices.Sort(roots)

	return buildSyntax(base, slices.Compact(roots), listed)
}

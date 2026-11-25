package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// fixture の名前は、本走査が頼る入れ子への再帰も canary が確かめるよう、入れ子にも整形されていないファイルを置く。
const (
	unformattedFile       = "unformatted.go"
	nestedUnformattedFile = "sub/unformatted.go"
	formattedFile         = "formatted.go"
	unformattedBody       = "package canary\nfunc  f( ) {}\n"
	formattedBody         = "package canary\n\nfunc g() {}\n"
)

// canaryWant は、canary で gofmt -l が挙げるべきファイルの一覧です（順は問わない）。
var canaryWant = []string{nestedUnformattedFile, unformattedFile}

// runCanary は、fixture を空の work に建て、実物の gofmt -l が入れ子を含む整形されていないファイルだけを挙げることを
// 確かめます。work の作り直しと後片付けは呼び出し側が持ちます。
func runCanary(work string, w io.Writer, tool toolFunc) error {
	if err := buildFixture(work); err != nil {
		return err
	}

	r, err := toolrun.Run(tool(context.Background(), work, os.Environ(), stepCanary))
	if err != nil {
		return err
	}
	if r.Code != 0 || !sameLines(r.Stdout, canaryWant) {
		fmt.Fprintf(w, "--- gofmt -l .（rc=%d）:\n%s\n", r.Code, r.Combined)

		return xerrors.Wrapf(errCanary, "gofmt -l が %s だけを挙げませんでした", strings.Join(canaryWant, "・"))
	}
	fmt.Fprintln(w, "✅ gofmt canary: 整形されていないファイルだけを挙げました")

	return nil
}

func buildFixture(work string) error {
	if err := os.MkdirAll(filepath.Join(work, filepath.Dir(filepath.FromSlash(nestedUnformattedFile))), 0o750); err != nil {
		return xerrors.Wrapf(errRead, "%v", err)
	}
	for name, body := range map[string]string{
		unformattedFile:       unformattedBody,
		nestedUnformattedFile: unformattedBody,
		formattedFile:         formattedBody,
	} {
		if err := os.WriteFile(filepath.Join(work, filepath.FromSlash(name)), []byte(body), 0o600); err != nil {
			return xerrors.Wrapf(errRead, "%v", err)
		}
	}

	return nil
}

func sameLines(out string, want []string) bool {
	got := lines(out)
	slices.Sort(got)
	sorted := slices.Clone(want)
	slices.Sort(sorted)

	return slices.Equal(got, sorted)
}

// lines は、out の空でない行を、行末の CR と先頭の ./ を外して返します。名前の前後の空白は名前の一部として残します。
func lines(out string) []string {
	var got []string
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimPrefix(strings.TrimRight(l, "\r"), "./"); l != "" {
			got = append(got, l)
		}
	}

	return got
}

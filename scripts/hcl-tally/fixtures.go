package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// runFixtures は、dirs の `*.txtar` の数の和を、-json の `fixtures` のキーの数と突き合わせます。txtar は
// os.ReadDir で数え、hcl-input fixtures の Glob とは別の経路にします。
func runFixtures(args []string, out io.Writer) error {
	fset := flag.NewFlagSet("hcl-tally fixtures", flag.ContinueOnError)
	fset.SetOutput(out)
	jsonPath := fset.String("json", "", "hcl-input fixtures が書いた JSON のパス")
	if err := fset.Parse(args); err != nil {
		return xerrors.Wrapf(errUsage, "%v", err)
	}
	dirs := fset.Args()
	if *jsonPath == "" || len(dirs) == 0 {
		return xerrors.Wrap(errUsage, "hcl-tally fixtures -json <path> <dir>...")
	}

	want := 0
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return xerrors.Wrapf(errFixtures, "%s を読めません: %v", dir, err)
		}
		n := 0
		for _, e := range entries {
			if e.Type().IsRegular() && strings.HasSuffix(e.Name(), ".txtar") {
				n++
			}
		}
		if n == 0 {
			return xerrors.Wrapf(errFixtures, "%s に txtar が1件もありません", dir)
		}
		want += n
	}

	raw, err := os.ReadFile(filepath.Clean(*jsonPath))
	if err != nil {
		return xerrors.Wrapf(errFixtures, "%s を読めません: %v", *jsonPath, err)
	}
	var doc struct {
		Fixtures map[string]json.RawMessage `json:"fixtures"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return xerrors.Wrapf(errFixtures, "%s を解釈できません: %v", *jsonPath, err)
	}
	if got := len(doc.Fixtures); got != want {
		return xerrors.Wrapf(errFixtures, "%s の txtar は %d 件ですが、%s のケースは %d 件です", strings.Join(dirs, " "), want, *jsonPath, got)
	}
	fmt.Fprintf(out, "✅ hcl-tally fixtures: txtar %d 件と %s のケースの数が一致しました\n", want, *jsonPath)

	return nil
}

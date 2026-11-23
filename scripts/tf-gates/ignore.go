package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// ignoreEntry は抑止の宣言の1件です。occurrences は当たる警告の件数です（ADR-0501 決定13）。
type ignoreEntry struct {
	Summary     string `yaml:"summary"`
	Path        string `yaml:"path"`
	Block       string `yaml:"block"`
	Detail      string `yaml:"detail"`
	Occurrences int    `yaml:"occurrences"`
	Reason      string `yaml:"reason"`
	Withdrawal  string `yaml:"withdrawal"`
}

type ignoreFile struct {
	Warnings *[]ignoreEntry `yaml:"warnings"`
}

// loadIgnore は抑止の宣言を読みます。無いファイル、空のファイル、`warnings` の無い宣言を
// 「抑止なし」と読まず、エラーにします —— 壊れた宣言と0件の宣言を区別するためです。
func loadIgnore(name string) ([]ignoreEntry, error) {
	raw, err := os.ReadFile(filepath.Clean(name))
	if err != nil {
		return nil, xerrors.Wrapf(errIgnore, "%s を読めません", name)
	}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	var f ignoreFile
	if err := dec.Decode(&f); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, xerrors.Wrapf(errIgnore, "%s が空です", name)
		}
		return nil, xerrors.Wrapf(errIgnore, "%s: %v", name, err)
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, xerrors.Wrapf(errIgnore, "%s: YAML の文書が2つ以上あります", name)
	}
	if f.Warnings == nil {
		return nil, xerrors.Wrapf(errIgnore, "%s: warnings がありません", name)
	}

	type key struct{ summary, path, block, detail string }
	seen := map[key]bool{}
	for i, e := range *f.Warnings {
		for _, f := range []struct{ name, value string }{
			{"summary", e.Summary}, {"path", e.Path}, {"block", e.Block}, {"detail", e.Detail},
			{"reason", e.Reason}, {"withdrawal", e.Withdrawal},
		} {
			if strings.TrimSpace(f.value) == "" {
				return nil, xerrors.Wrapf(errIgnore, "%s: warnings[%d] の %s が空です", name, i, f.name)
			}
		}
		if e.Occurrences < 1 {
			return nil, xerrors.Wrapf(errIgnore, "%s: warnings[%d] の occurrences は1以上です", name, i)
		}
		if path.IsAbs(e.Path) || path.Clean(e.Path) != e.Path || e.Path == ".." || strings.HasPrefix(e.Path, "../") {
			return nil, xerrors.Wrapf(errIgnore, "%s: warnings[%d] の path %q は走査の基点からの正規化したパスではありません", name, i, e.Path)
		}
		k := key{e.Summary, e.Path, e.Block, e.Detail}
		if seen[k] {
			return nil, xerrors.Wrapf(errIgnore, "%s: warnings[%d] は前の宣言と同じ警告を名指します", name, i)
		}
		seen[k] = true
	}
	return *f.Warnings, nil
}

// ignoreMismatch は、当たった警告の件数が occurrences と合わない宣言です。0件も含みます。
type ignoreMismatch struct {
	entry   ignoreEntry
	matched int
}

// applyIgnore は宣言に当たる警告を外し、残った警告、外した件数、件数の合わない宣言を返します。
// 照合は summary・path・block・detail の完全一致で、位置を持たない警告には当たりません。
func applyIgnore(entries []ignoreEntry, warnings []warning) ([]warning, int, []ignoreMismatch) {
	matched := make([]int, len(entries))
	var remaining []warning
	suppressed := 0
	for _, w := range warnings {
		hit := -1
		if w.path != "" {
			for i, e := range entries {
				if e.Summary == w.summary && e.Path == w.path && e.Block == w.block && e.Detail == w.detail {
					hit = i
					break
				}
			}
		}
		if hit < 0 {
			remaining = append(remaining, w)
			continue
		}
		matched[hit]++
		suppressed++
	}
	var mismatches []ignoreMismatch
	for i, e := range entries {
		if matched[i] != e.Occurrences {
			mismatches = append(mismatches, ignoreMismatch{entry: e, matched: matched[i]})
		}
	}
	return remaining, suppressed, mismatches
}

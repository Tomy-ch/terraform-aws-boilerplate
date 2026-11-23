package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// validateOutput は `terraform validate -json` の出力です。欄の欠けを 0 や空と区別するため、
// すべてポインタで受けます。
type validateOutput struct {
	FormatVersion *string       `json:"format_version"`
	Valid         *bool         `json:"valid"`
	ErrorCount    *int          `json:"error_count"`
	WarningCount  *int          `json:"warning_count"`
	Diagnostics   *[]diagnostic `json:"diagnostics"`
}

type diagnostic struct {
	Severity string     `json:"severity"`
	Summary  string     `json:"summary"`
	Detail   string     `json:"detail"`
	Range    *diagRange `json:"range"`
	Snippet  *struct {
		Context *string `json:"context"`
	} `json:"snippet"`
}

type diagRange struct {
	Filename string `json:"filename"`
	Start    struct {
		Line   int `json:"line"`
		Column int `json:"column"`
	} `json:"start"`
}

// location は diagnostic の位置を、validate を走らせたディレクトリからのパスで表します。
func (d diagnostic) location(dir string) string {
	if d.Range == nil {
		return dir + "（位置なし）"
	}
	return fmt.Sprintf("%s:%d:%d", filepath.ToSlash(filepath.Join(dir, d.Range.Filename)), d.Range.Start.Line, d.Range.Start.Column)
}

type dirResult struct {
	dir      string
	valid    bool
	errors   []diagnostic
	warnings []diagnostic
}

// parseResult は1ディレクトリの出力を読みます。件数の欄と diagnostic の数の突き合わせは、ここで
// ディレクトリごとに行います（重複を除く前）。
func parseResult(dir string, code int, data []byte) (dirResult, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	var o validateOutput
	if err := dec.Decode(&o); err != nil {
		return dirResult{}, xerrors.Wrapf(errUnreadable, "%s: JSON として読めません（%v）", dir, err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return dirResult{}, xerrors.Wrapf(errUnreadable, "%s: JSON の後ろに余計な出力があります", dir)
	}
	if o.FormatVersion == nil || o.Valid == nil || o.ErrorCount == nil || o.WarningCount == nil || o.Diagnostics == nil {
		return dirResult{}, xerrors.Wrapf(errMalformed, "%s: format_version・valid・error_count・warning_count・diagnostics のいずれかがありません", dir)
	}
	if !strings.HasPrefix(*o.FormatVersion, "1.") {
		return dirResult{}, xerrors.Wrapf(errMalformed, "%s: format_version %q は読めません", dir, *o.FormatVersion)
	}

	r := dirResult{dir: dir, valid: *o.Valid}
	for _, d := range *o.Diagnostics {
		switch d.Severity {
		case "error":
			r.errors = append(r.errors, d)
		case "warning":
			r.warnings = append(r.warnings, d)
		default:
			return dirResult{}, xerrors.Wrapf(errMalformed, "%s: 知らない severity %q", dir, d.Severity)
		}
	}
	if len(r.errors) != *o.ErrorCount || len(r.warnings) != *o.WarningCount {
		return dirResult{}, xerrors.Wrapf(errCountMismatch, "%s: error_count %d・warning_count %d に対し、diagnostic は error %d 件・warning %d 件",
			dir, *o.ErrorCount, *o.WarningCount, len(r.errors), len(r.warnings))
	}
	if (code == 0) != r.valid || r.valid != (len(r.errors) == 0) {
		return dirResult{}, xerrors.Wrapf(errExitMismatch, "%s: 終了コード %d、valid %t、error %d 件", dir, code, r.valid, len(r.errors))
	}
	return r, nil
}

// warning は、重複を除いた後の1件の警告です。path は走査の基点からのパスで、位置を持たない警告では空です。
type warning struct {
	path    string
	line    int
	column  int
	summary string
	detail  string
	block   string
	dirs    []string
}

func (w warning) location() string {
	if w.path == "" {
		return "（位置なし）"
	}
	return fmt.Sprintf("%s:%d:%d", w.path, w.line, w.column)
}

// collectWarnings は各ディレクトリの警告を、走査の基点からのパス・行・列・summary・detail で1件に
// まとめます。基点の外か `.terraform` の下を指す警告は、宣言で名指せないのでエラーにします。
func collectWarnings(base string, results []dirResult) ([]warning, error) {
	absBase, err := filepath.Abs(base)
	if err != nil {
		return nil, xerrors.Wrapf(errOutside, "基点 %s", base)
	}
	type key struct {
		path            string
		line, column    int
		summary, detail string
	}
	index := map[key]int{}
	var out []warning
	for _, r := range results {
		for _, d := range r.warnings {
			w := warning{summary: d.Summary, detail: d.Detail}
			if d.Snippet != nil && d.Snippet.Context != nil {
				w.block = *d.Snippet.Context
			}
			if d.Range != nil {
				abs, err := filepath.Abs(filepath.Join(r.dir, d.Range.Filename))
				if err != nil {
					return nil, xerrors.Wrapf(errOutside, "%s: %s", r.dir, d.Range.Filename)
				}
				rel, err := filepath.Rel(absBase, abs)
				rel = filepath.ToSlash(rel)
				if err != nil || rel == ".." || strings.HasPrefix(rel, "../") || slices.Contains(strings.Split(rel, "/"), ".terraform") {
					return nil, xerrors.Wrapf(errOutside, "%s: %s", r.dir, d.Range.Filename)
				}
				w.path, w.line, w.column = rel, d.Range.Start.Line, d.Range.Start.Column
			}
			k := key{w.path, w.line, w.column, w.summary, w.detail}
			if i, ok := index[k]; ok {
				if !slices.Contains(out[i].dirs, r.dir) {
					out[i].dirs = append(out[i].dirs, r.dir)
				}
				continue
			}
			w.dirs = []string{r.dir}
			index[k] = len(out)
			out = append(out, w)
		}
	}
	return out, nil
}

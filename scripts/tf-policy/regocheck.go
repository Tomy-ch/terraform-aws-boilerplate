package main

import (
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

var (
	// ruleLine は、Conftest が規則として評価する名前の行頭です。deny_list のような名前も含みます。
	ruleLine = regexp.MustCompile(`^(default +)?(deny|warn|violation)`)
	// planID は、違反の ID（[PLAN-<区分>-<番号>]）です。
	planID = regexp.MustCompile(`\[PLAN-[A-Z0-9]*-[0-9]*\]`)
)

// regoFile は、Policy の木の .rego の1つです。rel は木の root からの `/` 区切りの相対パスです。
type regoFile struct {
	rel  string
	src  string
	test bool
}

// readPolicy は、dir の下の .rego をすべて読み、相対パスの辞書順で返します。
func readPolicy(dir string) ([]regoFile, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, xerrors.Wrapf(errRead, "%s: %v", dir, err)
	}
	defer func() { _ = root.Close() }()
	var files []regoFile
	err = fs.WalkDir(root.FS(), ".", func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".rego") {
			return nil
		}
		raw, err := root.ReadFile(p)
		if err != nil {
			return err
		}
		files = append(files, regoFile{rel: p, src: string(raw), test: strings.HasSuffix(e.Name(), "_test.rego")})

		return nil
	})
	if err != nil {
		return nil, xerrors.Wrapf(errRead, "%s: %v", dir, err)
	}
	slices.SortFunc(files, func(a, b regoFile) int { return strings.Compare(a.rel, b.rel) })

	return files, nil
}

// checkRego は、Policy の木が ADR-0402 決定8・9 の形を持つかを見て、違反の文を返します。規則は deny contains の形
// だけを認め（warn は失敗にならず、下の検査も届かない）、deny を持つ Policy は違反する入力と違反しない入力の両方の
// テストを持ち、deny は違反の ID を持ち、各 ID はテストのどれかが名指します。
func checkRego(files []regoFile) []string {
	byRel := map[string]regoFile{}
	var testSrc strings.Builder
	for _, f := range files {
		byRel[f.rel] = f
		if f.test {
			testSrc.WriteString(f.src)
		}
	}
	var problems, otherForm, noTest, noID []string
	ids := map[string]bool{}
	for _, f := range files {
		if f.test {
			continue
		}
		hasDeny := false
		lines := strings.Split(f.src, "\n")
		for i, line := range lines {
			switch {
			case strings.HasPrefix(line, "deny contains "):
				hasDeny = true
				if !blockHasID(lines, i) {
					noID = append(noID, fmt.Sprintf("%s:%d", f.rel, i+1))
				}
			case ruleLine.MatchString(line):
				otherForm = append(otherForm, f.rel)
			}
		}
		for _, id := range planID.FindAllString(f.src, -1) {
			ids[id] = true
		}
		if hasDeny {
			t, ok := byRel[strings.TrimSuffix(f.rel, ".rego")+"_test.rego"]
			if !ok || !hasLinePrefix(t.src, "test_violation_") || !hasLinePrefix(t.src, "test_pass_") {
				noTest = append(noTest, f.rel)
			}
		}
	}
	var untested []string
	for id := range ids {
		if !strings.Contains(testSrc.String(), `"`+id) {
			untested = append(untested, id)
		}
	}
	slices.Sort(untested)
	if len(otherForm) > 0 {
		problems = append(problems, "deny contains 以外の形で書かれた規則があります（warn は失敗にならず、下の検査も届かない）: "+strings.Join(slices.Compact(otherForm), " "))
	}
	if len(noTest) > 0 {
		problems = append(problems, "違反する入力と違反しない入力の両方のテストを持たない Policy（ADR-0402 決定8・9）: "+strings.Join(noTest, " "))
	}
	if len(noID) > 0 {
		problems = append(problems, "違反の ID（[PLAN-<区分>-<番号>]）を持たない deny 規則があります（ID ごとのテストの要求が届かない）: "+strings.Join(noID, " "))
	}
	if len(untested) > 0 {
		problems = append(problems, "テストが一度も名指ししない違反の ID（ADR-0402 決定8・9）: "+strings.Join(untested, " "))
	}

	return problems
}

// blockHasID は、lines[start] で始まる deny contains の規則が、違反の ID を持つかを返します。規則は行頭の `}` で
// 閉じるか、`{` で終わらない1行で閉じます。
func blockHasID(lines []string, start int) bool {
	if !strings.HasSuffix(strings.TrimRight(lines[start], " \t"), "{") {
		return planID.MatchString(lines[start])
	}
	for _, line := range lines[start:] {
		if planID.MatchString(line) {
			return true
		}
		if strings.HasPrefix(line, "}") {
			return false
		}
	}

	return false
}

func hasLinePrefix(src, prefix string) bool {
	for line := range strings.SplitSeq(src, "\n") {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}

	return false
}

func runRegoCheck(args []string, w io.Writer) error {
	fset := flag.NewFlagSet("tf-policy rego-check", flag.ContinueOnError)
	fset.SetOutput(w)
	policy := fset.String("policy", "", "Policy の木（conftest/plan）")
	if err := fset.Parse(args); err != nil {
		return xerrors.Wrapf(errUsage, "%v", err)
	}
	if *policy == "" || fset.NArg() > 0 {
		return xerrors.Wrap(errUsage, "tf-policy rego-check -policy <dir>")
	}
	files, err := readPolicy(*policy)
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(files, func(f regoFile) bool { return !f.test }) {
		return xerrors.Wrapf(errNoInput, "Rego のテストが0件です（Policy も無い: %s）", *policy)
	}
	if problems := checkRego(files); len(problems) > 0 {
		for _, p := range problems {
			fmt.Fprintf(w, "❌ %s\n", p)
		}

		return xerrors.Wrapf(errFailed, "Policy の形の違反 %d 種", len(problems))
	}

	return nil
}

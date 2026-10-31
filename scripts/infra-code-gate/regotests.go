package main

import (
	"flag"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/lintreport"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

var (
	packageLine = regexp.MustCompile(`(?m)^package[ \t]+([A-Za-z0-9_.]+)`)
	// denyID は deny のメッセージの先頭に置く固定の ID です。
	denyID = regexp.MustCompile(`"(IC\d{3}) `)
	// Conftest が規則として評価する名前。`deny[msg]` の書き方も同じ規則である。
	ruleHead = regexp.MustCompile(`(?m)^(?:deny|warn|violation)(?:_[A-Za-z0-9]+)*\s*(?:contains\b|\[|if\b|:?=|\{)`)
	testName = regexp.MustCompile(`(?m)^(test_[A-Za-z0-9_]+)\b`)
	pairName = regexp.MustCompile(`^test_(IC\d{3})_(deny|allow)_`)
	// forbiddenBuiltin は、判定を外界に依存させる組み込み関数です。
	forbiddenBuiltin = regexp.MustCompile(`\bhttp\.send\b|\bnet\.[a-z_]+\b|\bopa\.runtime\b`)
	reportLine       = regexp.MustCompile(`^(data\.[A-Za-z0-9_.]+): (PASS|FAIL|ERROR)\b`)
)

// regoSuite は、Rego のソースから静的に読み取ったものです。
type regoSuite struct {
	ids      map[string]string // ID -> その ID を出す deny を持つファイル
	tests    map[string]string // data.<package>.<test> -> ファイル
	findings []lintreport.Finding
}

func runRegoTests(args []string, repo string, out io.Writer) error {
	fset := flag.NewFlagSet("rego-tests", flag.ContinueOnError)
	fset.SetOutput(out)
	policyDirs := fset.String("policy", "", "Rego の置き場所（, 区切り）。conftest verify の -p と同じもの")
	reportPath := fset.String("report", "", "`conftest verify --report notes` の出力のパス")
	namespaces := fset.String("namespaces", "", "検出が評価する名前空間（, 区切り）。規則を持つ package はこのどれかでなければならない")
	repoFlag := fset.String("repo", repo, "Rego の置き場所を解くリポジトリの root")
	if err := fset.Parse(args); err != nil {
		return xerrors.Wrap(err, "引数の解釈")
	}
	repo = *repoFlag
	if *reportPath == "" || *namespaces == "" || *policyDirs == "" {
		return xerrors.Wrap(errUsage, "rego-tests -policy <dir,...> -report <path> -namespaces <ns,...>")
	}

	suite := &regoSuite{ids: map[string]string{}, tests: map[string]string{}}
	for _, dir := range strings.Split(*policyDirs, ",") {
		s, err := scanRego(filepath.Join(repo, dir), strings.Split(*namespaces, ","))
		if err != nil {
			return err
		}
		maps.Copy(suite.ids, s.ids)
		maps.Copy(suite.tests, s.tests)
		suite.findings = append(suite.findings, s.findings...)
	}
	if len(suite.ids) == 0 {
		return xerrors.Wrapf(errNoInput, "%s に ID を持つ deny が1つもありません", *policyDirs)
	}
	results, err := readReport(*reportPath)
	if err != nil {
		return err
	}
	if len(results) == 0 {
		return xerrors.Wrapf(errNoInput, "%s: テストが1件も走っていません", *reportPath)
	}

	findings := slices.Concat(suite.findings, checkPairs(suite), checkRan(suite, results))
	if err := report(out, "rego-tests", findings); err != nil {
		return err
	}
	fmt.Fprintf(out, "✅ infra-code-gate rego-tests: deny の ID %d 件のそれぞれに違反と非違反のテストがあり、テスト %d 件が走って通りました\n",
		len(suite.ids), len(results))
	return nil
}

func scanRego(dir string, namespaces []string) (*regoSuite, error) {
	suite := &regoSuite{ids: map[string]string{}, tests: map[string]string{}}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, xerrors.Wrapf(err, "%s を開く", dir)
	}
	defer func() { _ = root.Close() }()
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
		src := string(raw)
		rel := filepath.ToSlash(filepath.Join(dir, p))
		if loc := forbiddenBuiltin.FindStringIndex(src); loc != nil {
			line := strings.Count(src[:loc[0]], "\n") + 1
			suite.findings = append(suite.findings, lintreport.Finding{File: rel, Line: line,
				Message: fmt.Sprintf("判定を外界に依存させる組み込み関数 %s を使っています", src[loc[0]:loc[1]])})
		}
		pkg := packageLine.FindStringSubmatch(src)
		if pkg == nil {
			suite.findings = append(suite.findings, lintreport.Finding{File: rel, Line: 1, Message: "package 宣言がありません"})
			return nil
		}
		if !strings.HasSuffix(e.Name(), "_test.rego") {
			ids := denyID.FindAllStringSubmatch(src, -1)
			for _, m := range ids {
				suite.ids[m[1]] = rel
			}
			rules := len(ruleHead.FindAllStringIndex(src, -1))
			// ID を持たない規則は、違反と非違反のテストの対を要求されないまま通る。
			if rules != len(ids) {
				suite.findings = append(suite.findings, lintreport.Finding{File: rel, Line: 1,
					Message: fmt.Sprintf("規則 %d 個に対し、メッセージの先頭の ID が %d 個です。各規則のメッセージを ID で始める", rules, len(ids))})
			}
			// 検出が評価しない名前空間の規則は、テストが通っても検出では一度も評価されない。
			if rules > 0 && !slices.Contains(namespaces, pkg[1]) {
				suite.findings = append(suite.findings, lintreport.Finding{File: rel, Line: 1,
					Message: fmt.Sprintf("package %s の規則は、検出が評価する名前空間（%s）にありません", pkg[1], strings.Join(namespaces, ", "))})
			}
			return nil
		}
		for _, m := range testName.FindAllStringSubmatch(src, -1) {
			suite.tests["data."+pkg[1]+"."+m[1]] = rel
		}
		return nil
	})
	if err != nil {
		return nil, xerrors.Wrapf(err, "%s の走査", dir)
	}
	return suite, nil
}

// checkPairs は、deny の ID ごとに違反と非違反のテストが各1つ以上あることを検査します（ADR-0402 決定8）。
// ID を持たないテストの名前が ID を名乗るなら、その ID の deny が実在することも見ます。
func checkPairs(suite *regoSuite) []lintreport.Finding {
	have := map[string]map[string]bool{}
	var findings []lintreport.Finding
	for _, full := range sortedKeys(suite.tests) {
		name := full[strings.LastIndex(full, ".")+1:]
		m := pairName.FindStringSubmatch(name)
		if m == nil {
			continue
		}
		if _, ok := suite.ids[m[1]]; !ok {
			findings = append(findings, lintreport.Finding{File: suite.tests[full], Line: 1, Message: fmt.Sprintf("テスト %s の ID %s を出す deny がありません", name, m[1])})
			continue
		}
		if have[m[1]] == nil {
			have[m[1]] = map[string]bool{}
		}
		have[m[1]][m[2]] = true
	}
	for _, id := range sortedKeys(suite.ids) {
		for _, kind := range []string{"deny", "allow"} {
			if !have[id][kind] {
				findings = append(findings, lintreport.Finding{File: suite.ids[id], Line: 1, Message: fmt.Sprintf("ID %s に test_%s_%s_* のテストがありません", id, id, kind)})
			}
		}
	}
	return findings
}

// readReport は `conftest verify --report notes` の出力から、テストごとの結果を読みます。
func readReport(p string) (map[string]string, error) {
	raw, err := os.ReadFile(filepath.Clean(p))
	if err != nil {
		return nil, xerrors.Wrapf(err, "%s の読み取り", p)
	}
	results := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		if m := reportLine.FindStringSubmatch(line); m != nil {
			if results[m[1]] == "" || m[2] != "PASS" {
				results[m[1]] = m[2]
			}
		}
	}
	return results, nil
}

// checkRan は、ソースに在るテストがすべて走って通ったことを検査します。名前が在ることと、走って
// 通ったことは別です —— package を綴り違えたテストは、verify に拾われないまま緑を返します。
func checkRan(suite *regoSuite, results map[string]string) []lintreport.Finding {
	var findings []lintreport.Finding
	for _, full := range sortedKeys(suite.tests) {
		switch got := results[full]; got {
		case "PASS":
		case "":
			findings = append(findings, lintreport.Finding{File: suite.tests[full], Line: 1, Message: fmt.Sprintf("テスト %s が走っていません", full)})
		default:
			findings = append(findings, lintreport.Finding{File: suite.tests[full], Line: 1, Message: fmt.Sprintf("テスト %s が %s です", full, got)})
		}
	}
	for _, full := range sortedKeys(results) {
		if _, ok := suite.tests[full]; !ok && results[full] != "PASS" {
			findings = append(findings, lintreport.Finding{File: full, Line: 1, Message: fmt.Sprintf("テスト %s が %s です", full, results[full])})
		}
	}
	return findings
}

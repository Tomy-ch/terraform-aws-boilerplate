// Package policyresult は、`conftest test -o json` の結果を、Rego の行頭の `deny contains` の数と突き合わせます。
// conftest は名前空間に当たる package が無くても、規則が減っても、評価できた分だけを成功として返すためです。
// tf-local policy-result と tf-policy の canary が使います。
package policyresult

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

var (
	// ErrJSON は、conftest の結果を JSON として読めないことです。
	ErrJSON = xerrors.New("❌ conftest の結果を解釈できません")
	// ErrEmpty は、宣言に在る名前空間の規則が1つも評価されていないことです。
	ErrEmpty = xerrors.New("❌ 名前空間の規則が1つも評価されていません")
	// ErrViolation は、Policy の違反があることです。
	ErrViolation = xerrors.New("❌ Policy の違反があります")
	// ErrCount は、評価された規則の数が宣言の数と違うことです。
	ErrCount = xerrors.New("❌ 評価された規則の数が、宣言された規則の数と一致しません")
	// ErrRules は、rego から規則の数の宣言を読めないことです。
	ErrRules = xerrors.New("❌ rego から規則の数を読めません")
)

// Entry は `conftest test -o json` の1要素です。
type Entry struct {
	Namespace string `json:"namespace"`
	Successes int    `json:"successes"`
	Failures  []struct {
		Msg string `json:"msg"`
	} `json:"failures"`
	Warnings   []json.RawMessage `json:"warnings"`
	Exceptions []json.RawMessage `json:"exceptions"`
}

var regoPackageRe = regexp.MustCompile(`(?m)^package[ \t]+([A-Za-z0-9_.]+)`)

// CountRules は、rego の行頭の `deny contains` を数えます。conftest が評価した規則の数と独立に得る宣言の側である。
func CountRules(rego []byte) int {
	n := 0

	for line := range strings.SplitSeq(string(rego), "\n") {
		if strings.HasPrefix(line, "deny contains") {
			n++
		}
	}

	return n
}

// Rules は、dir の下のすべての rego（conftest -p が読むのと同じ集合）を歩き、package ごとに行頭の
// `deny contains` の数を返します。規則の数の宣言を別に持たないので、規則を足すだけで突き合わせの対象に入る。
// 消えた規則に気づくために、テストの package（<名前>_test）は対象の package が規則を持つことを要求する。
// 規則を持たない package は lib/ の下にだけ置ける。
func Rules(dir string, readFile func(string) ([]byte, error)) (map[string]int, error) {
	rules := map[string]int{}
	tested := map[string]string{}

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() || filepath.Ext(path) != ".rego" {
			return nil
		}

		src, err := readFile(path)
		if err != nil {
			return xerrors.Wrap(ErrRules, path+": ファイルを読めません")
		}

		m := regoPackageRe.FindSubmatch(src)
		if m == nil {
			return xerrors.Wrap(ErrRules, path+": package の行がありません")
		}

		pkg, n := string(m[1]), CountRules(src)
		rel, _ := filepath.Rel(dir, path)

		switch {
		case strings.HasSuffix(path, "_test.rego"):
			subject, ok := strings.CutSuffix(pkg, "_test")
			if !ok {
				return xerrors.Wrap(ErrRules, path+": テストの package が _test で終わりません")
			}

			tested[subject] = path
		case n == 0 && strings.Split(filepath.ToSlash(rel), "/")[0] != "lib":
			return xerrors.Wrap(ErrRules, path+": deny contains を持たない rego は lib/ の下にだけ置けます")
		}

		if n > 0 {
			rules[pkg] += n
		}

		return nil
	})
	if err != nil {
		if xerrors.Is(err, ErrRules) {
			return nil, err
		}

		return nil, xerrors.Wrap(ErrRules, err.Error())
	}

	if len(rules) == 0 {
		return nil, xerrors.Wrap(ErrRules, dir+": deny contains が1つもありません")
	}

	for _, subject := range slices.Sorted(maps.Keys(tested)) {
		if rules[subject] == 0 {
			return nil, xerrors.Wrap(ErrRules, tested[subject]+": テストの対象の "+subject+" に deny contains がありません")
		}
	}

	return rules, nil
}

// Parse は `conftest test -o json` の結果を読みます。
func Parse(data []byte) ([]Entry, error) {
	var entries []Entry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, xerrors.Wrap(ErrJSON, err.Error())
	}

	return entries, nil
}

// Judge は、conftest の結果を名前空間ごとに rules と突き合わせ、違反が無いのに評価された規則が宣言の数で
// なければ失敗します。conftest は名前空間に当たる package が無くても、規則が減っても、評価できた分だけを成功
// として返すためです。conftest は違反の無い規則を successes に、違反の文を failures に数えるので、違反がある
// ときは件数を突き合わせられない。
func Judge(rules map[string]int, entries []Entry) (string, error) {
	if len(rules) == 0 {
		return "", xerrors.Wrap(ErrRules, "deny contains が1つもありません")
	}

	evaluated := map[string]int{}

	var messages []string

	for _, e := range entries {
		evaluated[e.Namespace] += e.Successes + len(e.Failures) + len(e.Warnings) + len(e.Exceptions)

		for _, f := range e.Failures {
			messages = append(messages, f.Msg)
		}
	}

	if len(messages) > 0 {
		return "", xerrors.Wrap(ErrViolation, strings.Join(messages, "\n"))
	}

	namespaces := map[string]bool{}
	for ns := range rules {
		namespaces[ns] = true
	}

	for ns, n := range evaluated {
		if n > 0 {
			namespaces[ns] = true
		}
	}

	var (
		errs  []error
		lines []string
	)

	for _, ns := range slices.Sorted(maps.Keys(namespaces)) {
		switch {
		case evaluated[ns] == 0:
			errs = append(errs, xerrors.Wrap(ErrEmpty, ns))
		case evaluated[ns] != rules[ns]:
			errs = append(errs, xerrors.Wrapf(ErrCount, "%s: 評価 %d 件、宣言 %d 件", ns, evaluated[ns], rules[ns]))
		default:
			lines = append(lines, fmt.Sprintf("✅ %s: 規則 %d 件を評価し、違反はありません", ns, evaluated[ns]))
		}
	}

	if len(errs) > 0 {
		return "", xerrors.Join(errs...)
	}

	return strings.Join(lines, "\n"), nil
}

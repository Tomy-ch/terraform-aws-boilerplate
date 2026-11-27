// Package requiredchecks は、保護設定の宣言（`.github/settings/branch-protection.json` と
// `.github/settings/rulesets/*.json`）が required とする status check の context を読む。
//
// 読み手は required-check-lint、oidc-job-lint、infra-code-gate の3つである。同じ宣言を各々が読むと、一方だけが読める宣言が
// 生まれ、もう一方は黙って検査の対象を失う。
package requiredchecks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

var (
	// ErrRead は、宣言を読めないか解釈できないことを表します。
	ErrRead = xerrors.New("保護設定の宣言を読めません")
	// ErrNone は、required の context が1つも無いことを表します。どの job も必須検査ではないと判定させる
	// ので、呼び手は合格と区別して扱います。
	ErrNone = xerrors.New("保護設定に required status check がありません")
)

type ruleset struct {
	Rules []struct {
		Type       string `json:"type"`
		Parameters struct {
			RequiredStatusChecks []struct {
				Context *string `json:"context"`
			} `json:"required_status_checks"`
		} `json:"parameters"`
	} `json:"rules"`
}

// Context は、ruleset の宣言が required とする context 1つと、それを宣言したファイル。
type Context struct {
	Name string
	File string
}

// Read は、required_status_checks の規則が名指す context を宣言の順に返します。context を欠くか空の項目は
// ErrRead にします。
func Read(path string) ([]string, error) {
	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, xerrors.Wrapf(ErrRead, "%s: %v", path, err)
	}

	var rs ruleset
	if err := json.Unmarshal(raw, &rs); err != nil {
		return nil, xerrors.Wrapf(ErrRead, "%s: %v", path, err)
	}

	var contexts []string
	for _, rule := range rs.Rules {
		if rule.Type != "required_status_checks" {
			continue
		}
		for _, c := range rule.Parameters.RequiredStatusChecks {
			if c.Context == nil || *c.Context == "" {
				return nil, xerrors.Wrapf(ErrRead, "%s: context の無い required status check があります", path)
			}
			contexts = append(contexts, *c.Context)
		}
	}
	if len(contexts) == 0 {
		return nil, xerrors.Wrapf(ErrNone, "%s", path)
	}
	return contexts, nil
}

// ReadDir は、dir の直下の *.json をそれぞれ ruleset の宣言として読み、required の context をファイル名の順、
// 各ファイルの中では宣言の順に返します。required の規則を持たない ruleset は在ってよいが、全体で0件なら
// ErrNone にします。dir を読めないことと、*.json を解釈できないことは ErrRead にします。
func ReadDir(dir string) ([]Context, error) {
	entries, err := os.ReadDir(filepath.Clean(dir))
	if err != nil {
		return nil, xerrors.Wrapf(ErrRead, "%s: %v", dir, err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	var out []Context
	for _, n := range names {
		path := filepath.Join(dir, n)
		contexts, err := Read(path)
		if xerrors.Is(err, ErrNone) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, c := range contexts {
			out = append(out, Context{Name: c, File: path})
		}
	}
	if len(out) == 0 {
		return nil, xerrors.Wrapf(ErrNone, "%s（ruleset %d 件）", dir, len(names))
	}
	return out, nil
}

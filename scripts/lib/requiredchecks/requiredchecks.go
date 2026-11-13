// Package requiredchecks は、保護設定の宣言（`.github/settings/branch-protection.json`）が required とする
// status check の context を読む。
//
// 読み手は required-check-lint と oidc-job-lint の2つである。同じ宣言を各々が読むと、一方だけが読める宣言が
// 生まれ、もう一方は黙って検査の対象を失う。
package requiredchecks

import (
	"encoding/json"
	"os"
	"path/filepath"

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

// Read は、required_status_checks の規則が名指す context を宣言の順に返します。context を欠くか空の項目は
// ErrRead にする —— 報告する job を名指せない項目を、黙って読み飛ばさない。
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

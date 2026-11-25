package main

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/cooldown"
)

// ecosystemYAML は、cooldown の本体（字下げ済みの行）を持つ1つの ecosystem の項を返します。
func ecosystemYAML(ecosystem, body string) string {
	s := fmt.Sprintf("  - package-ecosystem: %q\n", ecosystem)
	if body != "" {
		s += "    cooldown:\n" + body
	}
	return s
}

func days(key string, n int) string { return fmt.Sprintf("      %s: %d\n", key, n) }

// updatesYAML は、窓を持つ ecosystem をすべて窓ちょうどの default-days で並べた updates を返します。
// override に挙げた ecosystem は cooldown の本体をその値（"" なら cooldown 無し）に置き換え、omit に挙げた
// ecosystem は外します。
func updatesYAML(override map[string]string, omit ...string) string {
	windows := []struct {
		ecosystem string
		days      int
	}{
		{"gomod", cooldown.RegistryWindowDays},
		{"docker", cooldown.PinWindowDays},
		{"docker-compose", cooldown.PinWindowDays},
		{"github-actions", cooldown.PinWindowDays},
	}
	var b strings.Builder
	b.WriteString("updates:\n")
	for _, w := range windows {
		if slices.Contains(omit, w.ecosystem) {
			continue
		}
		body, ok := override[w.ecosystem]
		if !ok {
			body = days("default-days", w.days)
		}
		b.WriteString(ecosystemYAML(w.ecosystem, body))
	}
	return b.String()
}

func Test_checkDependabotCooldown(t *testing.T) {
	t.Parallel()

	allAtWindow := updatesYAML(map[string]string{
		"gomod": days("default-days", cooldown.RegistryWindowDays) + days("semver-patch-days", cooldown.RegistryWindowDays),
	})

	t.Run("すべての ecosystem が窓ちょうどなら、確かめた ecosystem の数を返す", func(t *testing.T) {
		t.Parallel()
		n, err := checkDependabotCooldown([]byte(allAtWindow))
		require.NoError(t, err)
		assert.Equal(t, 4, n)
	})

	t.Run("違反を ecosystem をまたいですべて挙げる", func(t *testing.T) {
		t.Parallel()
		src := "updates:\n" +
			ecosystemYAML("gomod", days("default-days", cooldown.RegistryWindowDays-1)) +
			ecosystemYAML("github-actions", days("default-days", cooldown.PinWindowDays-1))
		_, err := checkDependabotCooldown([]byte(src))
		assertOnlySentinel(t, err, errDependabotCooldown)
		assert.ErrorContains(t, err, fmt.Sprintf("gomod の default-days が %d 日", cooldown.RegistryWindowDays-1))
		assert.ErrorContains(t, err, fmt.Sprintf("github-actions の default-days が %d 日", cooldown.PinWindowDays-1))
	})

	t.Run("同じ ecosystem を別の directory に置いた項も1つずつ数える", func(t *testing.T) {
		t.Parallel()
		src := allAtWindow +
			"  - package-ecosystem: \"gomod\"\n    directory: \"/other\"\n    cooldown:\n" + days("default-days", cooldown.RegistryWindowDays)
		n, err := checkDependabotCooldown([]byte(src))
		require.NoError(t, err)
		assert.Equal(t, 5, n)
	})

	cases := []struct {
		name     string
		src      string
		sentinel error
		message  string
	}{
		{"default-days が小数なら日数でないとして拒む", updatesYAML(map[string]string{"docker": fmt.Sprintf("      default-days: %d.0\n", cooldown.PinWindowDays)}), errDependabotCooldown, "docker の default-days が日数ではありません"},
		{"cooldown の無い ecosystem は拒む", updatesYAML(map[string]string{"github-actions": ""}), errDependabotCooldown, "github-actions に cooldown がありません"},
		{"default-days が 0 なら拒む", updatesYAML(map[string]string{"gomod": days("default-days", 0)}), errDependabotCooldown, "gomod の default-days が 0 日"},
		{"gomod は RegistryWindowDays の1日手前で拒む", "updates:\n" + ecosystemYAML("gomod", days("default-days", cooldown.RegistryWindowDays-1)), errDependabotCooldown, "gomod の default-days"},
		{"docker は PinWindowDays の1日手前で拒む", "updates:\n" + ecosystemYAML("docker", days("default-days", cooldown.PinWindowDays-1)), errDependabotCooldown, "docker の default-days"},
		{"docker-compose は PinWindowDays の1日手前で拒む", "updates:\n" + ecosystemYAML("docker-compose", days("default-days", cooldown.PinWindowDays-1)), errDependabotCooldown, "docker-compose の default-days"},
		{"github-actions は PinWindowDays の1日手前で拒む", "updates:\n" + ecosystemYAML("github-actions", days("default-days", cooldown.PinWindowDays-1)), errDependabotCooldown, "github-actions の default-days"},
		{"窓を知らない ecosystem は拒む", "updates:\n" + ecosystemYAML("npm", days("default-days", 30)), errDependabotCooldown, `"npm" は窓を知らない ecosystem`},
		{"ecosystem が1つも無ければ拒む", "version: 2\nupdates: []\n", errDependabotCooldown, "ecosystem が1つもありません"},
		{"updates が無ければ拒む", "version: 2\n", errDependabotCooldown, "ecosystem が1つもありません"},
		{"dependabot.yml が空なら拒む", "", errDependabotCooldown, "ecosystem が1つもありません"},
		{"YAML として読めなければ拒む", "updates: [\n", errDependabotParse, dependabotFile},
		{"cooldown が対応でなければ拒む", "updates:\n  - package-ecosystem: \"gomod\"\n    cooldown: 7\n", errDependabotParse, dependabotFile},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := checkDependabotCooldown([]byte(tc.src))
			assertOnlySentinel(t, err, tc.sentinel)
			assert.ErrorContains(t, err, tc.message)
		})
	}
}

func Test_cooldownProblems(t *testing.T) {
	t.Parallel()

	const window = 7

	t.Run("default-days と semver-*-days が窓ちょうどなら理由を返さない", func(t *testing.T) {
		t.Parallel()
		c := map[string]any{"default-days": window, "semver-major-days": window, "semver-minor-days": window, "semver-patch-days": window}
		assert.Empty(t, cooldownProblems("gomod", window, c))
	})

	t.Run("semver-*-days を書かなくても default-days が窓以上なら理由を返さない", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, cooldownProblems("gomod", window, map[string]any{"default-days": window + 23}))
	})

	cases := []struct {
		name string
		c    map[string]any
		want []string
	}{
		{"cooldown が無ければ挙げる", nil, []string{"gomod に cooldown がありません（窓 7 日）"}},
		{"cooldown が空なら default-days の欠けを挙げる", map[string]any{}, []string{"gomod の cooldown に default-days がありません（窓 7 日）"}},
		{"default-days が窓の1日手前なら挙げる", map[string]any{"default-days": window - 1}, []string{"gomod の default-days が 6 日で、窓 7 日を下回ります"}},
		{"semver-major-days が窓の1日手前なら挙げる", map[string]any{"default-days": window, "semver-major-days": window - 1}, []string{"gomod の semver-major-days が 6 日で、窓 7 日を下回ります"}},
		{"semver-minor-days が窓の1日手前なら挙げる", map[string]any{"default-days": window, "semver-minor-days": window - 1}, []string{"gomod の semver-minor-days が 6 日で、窓 7 日を下回ります"}},
		{"semver-patch-days が窓の1日手前なら挙げる", map[string]any{"default-days": window, "semver-patch-days": window - 1}, []string{"gomod の semver-patch-days が 6 日で、窓 7 日を下回ります"}},
		{"日数でない値は挙げる", map[string]any{"default-days": "7"}, []string{"gomod の default-days が日数ではありません: 7"}},
		{"include は挙げる", map[string]any{"default-days": window, "include": []any{"x"}}, []string{"gomod の cooldown に include があり、窓が一部の依存にしか掛かりません"}},
		{"exclude は挙げる", map[string]any{"default-days": window, "exclude": []any{"x"}}, []string{"gomod の cooldown に exclude があり、窓が一部の依存にしか掛かりません"}},
		{"未知のキーは名前の順に挙げる", map[string]any{"default-days": window, "z-days": 1, "semver-patch-day": 1}, []string{`gomod の cooldown に未知のキー "semver-patch-day" があります`, `gomod の cooldown に未知のキー "z-days" があります`}},
		{"複数の理由をキーの順にすべて挙げる", map[string]any{"semver-patch-days": 1, "semver-minor-days": 2}, []string{
			"gomod の cooldown に default-days がありません（窓 7 日）",
			"gomod の semver-minor-days が 2 日で、窓 7 日を下回ります",
			"gomod の semver-patch-days が 1 日で、窓 7 日を下回ります",
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, cooldownProblems("gomod", window, tc.c))
		})
	}
}

package main

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/cooldown"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

var errDependabotCooldown = xerrors.New("Dependabot の cooldown が、更新を取り込むときの窓を下回っています")

// dependabotWindows は、ecosystem ごとに、その更新を取り込むときに掛かる窓（日）。docker・docker-compose・
// github-actions は digest / SHA の固定を解決し直す窓、gomod は go-cooldown の gate の窓である（ADR-0501 決定17）。
var dependabotWindows = map[string]int{
	"docker":         cooldown.PinWindowDays,
	"docker-compose": cooldown.PinWindowDays,
	"github-actions": cooldown.PinWindowDays,
	"gomod":          cooldown.RegistryWindowDays,
}

// cooldownDayKeys は、日数を持つ cooldown のキー。default-days は必須で、ほかは書かれていれば見る。
var cooldownDayKeys = []string{"default-days", "semver-major-days", "semver-minor-days", "semver-patch-days"}

// checkDependabotCooldown は、dependabot の各 ecosystem の cooldown の日数（default-days と、書かれた
// semver-*-days）が、その ecosystem の窓（dependabotWindows）を下回らないことを確かめ、確かめた updates の
// 項の数を返します。窓を知らない ecosystem、窓を持つのに updates に無い ecosystem、cooldown か default-days の
// 無い ecosystem、日数でない値、未知のキー、cooldown を一部の依存に限るか外す include / exclude、ecosystem が
// 0件はエラーにします。違反はすべて挙げます。
func checkDependabotCooldown(dependabot []byte) (int, error) {
	var config struct {
		Updates []struct {
			Ecosystem string         `yaml:"package-ecosystem"`
			Cooldown  map[string]any `yaml:"cooldown"`
		} `yaml:"updates"`
	}
	if err := yaml.Unmarshal(dependabot, &config); err != nil {
		return 0, xerrors.Wrapf(errDependabotParse, "%s: %v", dependabotFile, err)
	}
	if len(config.Updates) == 0 {
		return 0, xerrors.Wrapf(errDependabotCooldown, "%s に updates の ecosystem が1つもありません", dependabotFile)
	}
	var problems []string
	seen := map[string]bool{}
	for _, u := range config.Updates {
		window, ok := dependabotWindows[u.Ecosystem]
		if !ok {
			problems = append(problems, fmt.Sprintf("%q は窓を知らない ecosystem です", u.Ecosystem))
			continue
		}
		seen[u.Ecosystem] = true
		problems = append(problems, cooldownProblems(u.Ecosystem, window, u.Cooldown)...)
	}
	var missing []string
	for ecosystem := range dependabotWindows {
		if !seen[ecosystem] {
			missing = append(missing, ecosystem)
		}
	}
	sort.Strings(missing)
	for _, ecosystem := range missing {
		problems = append(problems, fmt.Sprintf("%q は窓を宣言した ecosystem ですが updates にありません", ecosystem))
	}
	if len(problems) > 0 {
		return 0, xerrors.Wrapf(errDependabotCooldown, "%s: %s", dependabotFile, strings.Join(problems, ", "))
	}
	return len(config.Updates), nil
}

// cooldownProblems は、1つの ecosystem の cooldown が窓を満たさない理由を、キーの順に返します。
func cooldownProblems(ecosystem string, window int, c map[string]any) []string {
	if c == nil {
		return []string{fmt.Sprintf("%s に cooldown がありません（窓 %d 日）", ecosystem, window)}
	}
	var problems []string
	if _, ok := c["default-days"]; !ok {
		problems = append(problems, fmt.Sprintf("%s の cooldown に default-days がありません（窓 %d 日）", ecosystem, window))
	}
	known := map[string]bool{"include": true, "exclude": true}
	for _, k := range cooldownDayKeys {
		known[k] = true
		v, ok := c[k]
		if !ok {
			continue
		}
		days, isInt := v.(int)
		switch {
		case !isInt:
			problems = append(problems, fmt.Sprintf("%s の %s が日数ではありません: %v", ecosystem, k, v))
		case days < window:
			problems = append(problems, fmt.Sprintf("%s の %s が %d 日で、窓 %d 日を下回ります", ecosystem, k, days, window))
		}
	}
	for _, k := range []string{"include", "exclude"} {
		if _, ok := c[k]; ok {
			problems = append(problems, fmt.Sprintf("%s の cooldown に %s があり、窓が一部の依存にしか掛かりません", ecosystem, k))
		}
	}
	var unknown []string
	for k := range c {
		if !known[k] {
			unknown = append(unknown, k)
		}
	}
	sort.Strings(unknown)
	for _, k := range unknown {
		problems = append(problems, fmt.Sprintf("%s の cooldown に未知のキー %q があります", ecosystem, k))
	}
	return problems
}

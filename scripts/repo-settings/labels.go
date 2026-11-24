package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const (
	dependabotFile = ".github/dependabot.yml"
	labelsFile     = ".github/settings/labels.json"
)

var errDependabotLabels = xerrors.New("Dependabot が付けるラベルが labels.json の宣言に揃っていません")

// checkDependabotLabels は、dependabot の各 ecosystem の labels: が名指すラベルが、すべて labels の
// name にあることを確かめ、確かめたラベルの数を返します。labels: の無い ecosystem は対象にしません。
// ラベル集合は make setup-repo が labels.json だけから作り直すため、そこに無いラベルを Dependabot は付けられない。
func checkDependabotLabels(dependabot, labels []byte) (int, error) {
	var config struct {
		Updates []struct {
			Ecosystem string   `yaml:"package-ecosystem"`
			Labels    []string `yaml:"labels"`
		} `yaml:"updates"`
	}
	if err := yaml.Unmarshal(dependabot, &config); err != nil {
		return 0, xerrors.Wrapf(errDependabotLabels, "%s の解釈: %v", dependabotFile, err)
	}
	if len(config.Updates) == 0 {
		return 0, xerrors.Wrapf(errDependabotLabels, "%s に updates の ecosystem が1つもありません", dependabotFile)
	}
	var decl []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(labels, &decl); err != nil {
		return 0, xerrors.Wrapf(errDependabotLabels, "%s の解釈: %v", labelsFile, err)
	}
	declared := map[string]bool{}
	for i, l := range decl {
		if l.Name == "" {
			return 0, xerrors.Wrapf(errDependabotLabels, "%s の %d 番目の要素に name がありません", labelsFile, i+1)
		}
		declared[l.Name] = true
	}
	var missing []string
	n := 0
	for _, u := range config.Updates {
		for _, l := range u.Labels {
			n++
			if !declared[l] {
				missing = append(missing, fmt.Sprintf("%s の %q", u.Ecosystem, l))
			}
		}
	}
	if len(missing) > 0 {
		return 0, xerrors.Wrapf(errDependabotLabels, "%s に無いラベル: %s", labelsFile, strings.Join(missing, ", "))
	}
	return n, nil
}

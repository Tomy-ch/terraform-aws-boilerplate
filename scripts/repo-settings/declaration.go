package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// 分岐のパターンの単一宣言（ADR-0603 決定1）。
type branches struct {
	Deploy []string `json:"deploy"`
	Lines  []string `json:"lines"`
}

// environment は、デプロイの environment 1件の宣言（ADR-0603 決定4）。
// パターンは持たず、deploy_from が種類を名指す。展開は branches から行う（同 決定3）。
type environment struct {
	Name       string `json:"name"`
	DeployFrom string `json:"deploy_from"`
	// CanAdminsBypass は省略を許さないためにポインタで受ける。
	CanAdminsBypass *bool `json:"can_admins_bypass"`
}

const (
	deployFromProtected   = "protected"
	deployFromProtectedPR = "protected-and-pull-requests"
	deployFromAny         = "any"
)

// pullRequestRef は、plan の environment が Pull Request の job を通すために許す ref。
// 宣言の値ではなく GitHub が Pull Request の run に与える ref の形である。
const pullRequestRef = "refs/pull/*/merge"

var (
	deployNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
	linePrefixRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*/$`)
)

var (
	errDeclaration      = xerrors.New("宣言が不正です")
	errEmptyDeclaration = xerrors.New("宣言が1件もありません")
)

// declarationKeys は、宣言の object が持ってよいキー。encoding/json はキー名の大文字小文字を
// 区別せず、重複したキーは後の値を黙って採るので、デコードの前に字面で確かめる。
var declarationKeys = map[string]bool{"deploy": true, "lines": true, "name": true, "deploy_from": true, "can_admins_bypass": true}

// decodeStrict は path の JSON を out へ読みます。未知のキー、字面の違うキー、重複したキー、
// 値の後の余分なトークンを拒みます。
func decodeStrict(path string, out any) error {
	src, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return xerrors.Wrapf(err, "%s の読み取り", path)
	}
	if err := checkKeys(json.NewDecoder(bytes.NewReader(src))); err != nil {
		return xerrors.Wrapf(err, "%s", path)
	}
	dec := json.NewDecoder(bytes.NewReader(src))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return xerrors.Wrapf(errDeclaration, "%s: %v", path, err)
	}
	if _, err := dec.Token(); !xerrors.Is(err, io.EOF) {
		return xerrors.Wrapf(errDeclaration, "%s: 値の後に余分な内容があります", path)
	}
	return nil
}

func checkKeys(dec *json.Decoder) error {
	tok, err := dec.Token()
	if err != nil {
		return xerrors.Wrapf(errDeclaration, "%v", err)
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return nil
	}
	if delim == '{' {
		seen := map[string]bool{}
		for dec.More() {
			keyTok, err := dec.Token()
			if err != nil {
				return xerrors.Wrapf(errDeclaration, "%v", err)
			}
			key, _ := keyTok.(string)
			if !declarationKeys[key] {
				return xerrors.Wrapf(errDeclaration, "キー %q は宣言のキーではありません", key)
			}
			if seen[key] {
				return xerrors.Wrapf(errDeclaration, "キー %q が重複しています", key)
			}
			seen[key] = true
			if err := checkKeys(dec); err != nil {
				return err
			}
		}
	} else {
		for dec.More() {
			if err := checkKeys(dec); err != nil {
				return err
			}
		}
	}
	if _, err := dec.Token(); err != nil {
		return xerrors.Wrapf(errDeclaration, "%v", err)
	}
	return nil
}

func readBranches(path string) (branches, error) {
	var b branches
	if err := decodeStrict(path, &b); err != nil {
		return branches{}, err
	}
	if err := validateBranches(b); err != nil {
		return branches{}, xerrors.Wrapf(err, "%s", path)
	}
	return b, nil
}

func validateBranches(b branches) error {
	if len(b.Deploy)+len(b.Lines) == 0 {
		return errEmptyDeclaration
	}
	seen := map[string]bool{}
	for _, name := range b.Deploy {
		if !deployNameRe.MatchString(name) {
			return xerrors.Wrapf(errDeclaration, "deploy の名前 %q", name)
		}
		if seen[name] {
			return xerrors.Wrapf(errDeclaration, "deploy の名前 %q が重複しています", name)
		}
		seen[name] = true
	}
	for _, prefix := range b.Lines {
		if !linePrefixRe.MatchString(prefix) {
			return xerrors.Wrapf(errDeclaration, "lines の接頭辞 %q（`/` で終わる名前）", prefix)
		}
		if seen[prefix] {
			return xerrors.Wrapf(errDeclaration, "lines の接頭辞 %q が重複しています", prefix)
		}
		seen[prefix] = true
	}
	return nil
}

func readEnvironments(path string) ([]environment, error) {
	var envs []environment
	if err := decodeStrict(path, &envs); err != nil {
		return nil, err
	}
	if err := validateEnvironments(envs); err != nil {
		return nil, xerrors.Wrapf(err, "%s", path)
	}
	return envs, nil
}

// validateEnvironments は、名前の接尾辞と deploy_from の対応（ADR-0602 決定14）を宣言の側で固定します。
func validateEnvironments(envs []environment) error {
	if len(envs) == 0 {
		return errEmptyDeclaration
	}
	seen := map[string]bool{}
	for _, e := range envs {
		if e.Name == "" {
			return xerrors.Wrap(errDeclaration, "name が空の environment があります")
		}
		if seen[e.Name] {
			return xerrors.Wrapf(errDeclaration, "environment %q が重複しています", e.Name)
		}
		seen[e.Name] = true
		switch e.DeployFrom {
		case deployFromProtected, deployFromProtectedPR, deployFromAny:
		default:
			return xerrors.Wrapf(errDeclaration, "environment %q の deploy_from %q", e.Name, e.DeployFrom)
		}
		if e.CanAdminsBypass == nil || *e.CanAdminsBypass {
			return xerrors.Wrapf(errDeclaration, "environment %q の can_admins_bypass は false でなければなりません", e.Name)
		}
		if want, ok := requiredDeployFrom(e.Name); ok && e.DeployFrom != want {
			return xerrors.Wrapf(errDeclaration, "environment %q の deploy_from は %q でなければなりません", e.Name, want)
		}
	}
	return nil
}

func requiredDeployFrom(name string) (string, bool) {
	switch {
	// App の秘密鍵を置く environment（ADR-0603 決定30）。Pull Request の run に鍵を渡さない。
	case name == appEnvironment, strings.HasSuffix(name, "-apply"), strings.HasSuffix(name, "-apply-identity"):
		return deployFromProtected, true
	case strings.HasSuffix(name, "-plan"):
		return deployFromProtectedPR, true
	default:
		return "", false
	}
}

// rulesetIncludes は、保護設定の宣言の conditions.ref_name.include に置く値を返します。
// lines は階層をまたいで保護する。
func rulesetIncludes(b branches) []string {
	out := make([]string, 0, len(b.Deploy)+len(b.Lines))
	for _, name := range b.Deploy {
		out = append(out, "refs/heads/"+name)
	}
	for _, prefix := range b.Lines {
		out = append(out, "refs/heads/"+prefix+"**/*")
	}
	return out
}

// environmentPatterns は、deploy_from を deployment branch policy のパターンへ展開します。
// any は policy を持たないので nil を返す。
func environmentPatterns(b branches, deployFrom string) []string {
	if deployFrom == deployFromAny {
		return nil
	}
	out := make([]string, 0, len(b.Deploy)+len(b.Lines)+1)
	out = append(out, b.Deploy...)
	for _, prefix := range b.Lines {
		out = append(out, prefix+"*")
	}
	if deployFrom == deployFromProtectedPR {
		out = append(out, pullRequestRef)
	}
	return out
}

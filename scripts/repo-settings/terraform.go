package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// Terraform のソースから読むのは、deployment-identity へ渡す env の値だけである（ADR-0501 決定5 の但し書き）。
const (
	envDir             = "env"
	identityUseCaseDir = "usecases/deployment-identity"
)

// identitySuffixes は、deployment-identity の role の信頼条件が名指す environment 名の接尾辞。
// `<env>` に続く形は deployment-identity の公開契約で、tests/contract.tftest.hcl が固定する。
var identitySuffixes = []string{"-plan", "-apply", "-apply-identity"}

var (
	errTerraform           = xerrors.New("Terraform のソースから deployment-identity の env を読めません")
	errNoIdentityRoot      = xerrors.New("deployment-identity を呼ぶ root が1件もありません")
	errEnvironmentMismatch = xerrors.New("environment の宣言が deployment-identity の信頼条件と一致しません")
)

// identityEnvs は env/ の下の .tf を読み、source が deployment-identity を指す module の env の値を返します。
// source と env はリテラルの文字列に限る。それ以外は読めないものとしてエラーにする（ADR-0702 決定14）。
func identityEnvs(root string) ([]string, error) {
	target := filepath.Join(root, identityUseCaseDir)
	seen := map[string]bool{}
	var envs []string
	err := filepath.WalkDir(filepath.Join(root, envDir), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".tf") {
			return nil
		}
		found, err := moduleEnvs(path, target)
		if err != nil {
			return err
		}
		for _, e := range found {
			if !seen[e] {
				seen[e] = true
				envs = append(envs, e)
			}
		}
		return nil
	})
	if err != nil {
		return nil, xerrors.Wrapf(err, "%s の走査", envDir)
	}
	if len(envs) == 0 {
		return nil, xerrors.Wrapf(errNoIdentityRoot, "%s", envDir)
	}
	sort.Strings(envs)
	return envs, nil
}

func moduleEnvs(path, target string) ([]string, error) {
	src, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, xerrors.Wrapf(err, "%s の読み取り", path)
	}
	file, diags := hclsyntax.ParseConfig(src, path, hcl.InitialPos)
	if diags.HasErrors() {
		return nil, xerrors.Wrapf(errTerraform, "%s: %s", path, diags.Error())
	}
	body, ok := file.Body.(*hclsyntax.Body)
	if !ok {
		return nil, xerrors.Wrapf(errTerraform, "%s: 本体を読めません", path)
	}
	var envs []string
	for _, block := range body.Blocks {
		if block.Type != "module" {
			continue
		}
		source, err := literalString(block.Body, "source")
		if err != nil {
			return nil, xerrors.Wrapf(err, "%s の module %q", path, strings.Join(block.Labels, "."))
		}
		if filepath.Join(filepath.Dir(path), source) != target {
			continue
		}
		env, err := literalString(block.Body, "env")
		if err != nil {
			return nil, xerrors.Wrapf(err, "%s の module %q", path, strings.Join(block.Labels, "."))
		}
		envs = append(envs, env)
	}
	return envs, nil
}

// literalString は body の属性 name を、変数も関数も使わないリテラルの文字列として読みます。
func literalString(body *hclsyntax.Body, name string) (string, error) {
	attr, ok := body.Attributes[name]
	if !ok {
		return "", xerrors.Wrapf(errTerraform, "%s がありません", name)
	}
	value, diags := attr.Expr.Value(nil)
	if diags.HasErrors() || value.Type() != cty.String || !value.IsKnown() || value.IsNull() {
		return "", xerrors.Wrapf(errTerraform, "%s がリテラルの文字列ではありません", name)
	}
	return value.AsString(), nil
}

// checkIdentityEnvironments は、各 env について信頼条件が名指す environment がすべて宣言に在り、
// 宣言の plan / apply / apply-identity の environment がどれもいずれかの env に当たることを見ます。
func checkIdentityEnvironments(envs []string, declared []environment) error {
	names := map[string]bool{}
	for _, e := range declared {
		names[e.Name] = true
	}
	known := map[string]bool{}
	var problems []string
	for _, env := range envs {
		known[env] = true
		for _, suffix := range identitySuffixes {
			if !names[env+suffix] {
				problems = append(problems, "environment "+env+suffix+" が宣言にありません")
			}
		}
	}
	for _, e := range declared {
		if base, ok := identityBase(e.Name); ok && !known[base] {
			problems = append(problems, "environment "+e.Name+" に当たる deployment-identity の env がありません")
		}
	}
	if len(problems) > 0 {
		return xerrors.Wrapf(errEnvironmentMismatch, "%s", strings.Join(problems, "; "))
	}
	return nil
}

func identityBase(name string) (string, bool) {
	for _, suffix := range identitySuffixes {
		if base, ok := strings.CutSuffix(name, suffix); ok && base != "" {
			return base, true
		}
	}
	return "", false
}

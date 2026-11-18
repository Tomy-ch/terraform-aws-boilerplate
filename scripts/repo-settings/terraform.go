package main

import (
	"bytes"
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

const (
	envDir             = "env"
	identityUseCaseDir = "usecases/deployment-identity"
)

// planSuffix は、Pull Request からも起動する plan の environment の接尾辞。
const planSuffix = "-plan"

// unlockIdentitySuffix は、人が workflow_dispatch で起動する lock の解除の environment の接尾辞。
const unlockIdentitySuffix = "-unlock-identity"

// unlockSuffix は、workload root の lock の解除の environment の接尾辞。
const unlockSuffix = "-unlock"

// identitySuffixes は、deployment-identity の role の信頼条件が名指す environment 名の接尾辞。
// `<env>` に続く形は deployment-identity の公開契約で、tests/contract.tftest.hcl が固定する。
var identitySuffixes = []string{planSuffix, "-apply", "-apply-identity", unlockIdentitySuffix, unlockSuffix}

var (
	errTerraform           = xerrors.New("Terraform のソースから deployment-identity の env を読めません")
	errNoIdentityRoot      = xerrors.New("deployment-identity を呼ぶ root が1件もありません")
	errEnvironmentMismatch = xerrors.New("environment の宣言が deployment-identity の信頼条件と一致しません")
	errDeployMismatch      = xerrors.New("branches.json のデプロイブランチが deployment-identity の env と一致しません")
	errBackendKey          = xerrors.New("root の backend の key が ADR-0604 決定7 の値ではありません")
)

// backendKeys は、root の配置（env/<env>/identity/ と env/<env>/）ごとの state の key（ADR-0604 決定7）。
// CI の role の権限の Resource にも同じ値が写っている（usecases/deployment-identity/README.md）。
var backendKeys = map[bool]string{true: "identity/terraform.tfstate", false: "workload/terraform.tfstate"}

// identityEnvs は env/ の下の .tf を読み、source が deployment-identity を指す module の env の値を返します。
// source と env はリテラルの文字列に限る。それ以外は読めないものとしてエラーにする（ADR-0702 決定14）。
// .tf.json と symlink は読めない形式としてエラーにする。
// 見分けた module の件数は、ソースの字面に現れる deployment-identity の件数と突き合わせる（ADR-0702 決定15）。
func identityEnvs(root string) ([]string, error) {
	target := filepath.Join(root, identityUseCaseDir)
	seen := map[string]bool{}
	var envs []string
	modules, mentions := 0, 0
	err := filepath.WalkDir(filepath.Join(root, envDir), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		switch {
		case d.IsDir() && d.Name() == ".terraform":
			return filepath.SkipDir
		case d.IsDir():
			return nil
		case d.Type()&fs.ModeSymlink != 0:
			return xerrors.Wrapf(errTerraform, "%s: symlink は読みません", path)
		case strings.HasSuffix(path, ".tf.json"):
			return xerrors.Wrapf(errTerraform, "%s: JSON の構成は読みません", path)
		case !strings.HasSuffix(path, ".tf"):
			return nil
		}
		found, mentioned, err := moduleEnvs(path, target)
		if err != nil {
			return err
		}
		modules += len(found)
		mentions += mentioned
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
	if modules != mentions {
		return nil, xerrors.Wrapf(errTerraform, "%s の字面に %s が %d 回現れますが、見分けた module は %d 件です", envDir, identityUseCaseDir, mentions, modules)
	}
	if len(envs) == 0 {
		return nil, xerrors.Wrapf(errNoIdentityRoot, "%s", envDir)
	}
	sort.Strings(envs)
	return envs, nil
}

// moduleEnvs は、見分けた module の env の値と、ソースの字面に identityUseCaseDir が現れた回数を返します。
func moduleEnvs(path, target string) ([]string, int, error) {
	src, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, 0, xerrors.Wrapf(err, "%s の読み取り", path)
	}
	mentioned := bytes.Count(src, []byte(identityUseCaseDir))
	file, diags := hclsyntax.ParseConfig(src, path, hcl.InitialPos)
	if diags.HasErrors() {
		return nil, 0, xerrors.Wrapf(errTerraform, "%s: %s", path, diags.Error())
	}
	body, ok := file.Body.(*hclsyntax.Body)
	if !ok {
		return nil, 0, xerrors.Wrapf(errTerraform, "%s: 本体を読めません", path)
	}
	var envs []string
	for _, block := range body.Blocks {
		if block.Type != "module" {
			continue
		}
		source, err := literalString(block.Body, "source")
		if err != nil {
			return nil, 0, xerrors.Wrapf(err, "%s の module %q", path, strings.Join(block.Labels, "."))
		}
		if filepath.Join(filepath.Dir(path), source) != target {
			continue
		}
		env, err := literalString(block.Body, "env")
		if err != nil {
			return nil, 0, xerrors.Wrapf(err, "%s の module %q", path, strings.Join(block.Labels, "."))
		}
		envs = append(envs, env)
	}
	return envs, mentioned, nil
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
	if value.AsString() == "" {
		return "", xerrors.Wrapf(errTerraform, "%s が空です", name)
	}
	return value.AsString(), nil
}

// checkIdentityEnvironments は、各 env について信頼条件が名指す environment がすべて宣言に在り、
// 宣言の identitySuffixes の environment がどれもいずれかの env に当たることを見ます。
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

// checkDeployEnvs は、deployment-identity の各 env にデプロイブランチが1つ在り、env を持つデプロイブランチが
// どれも deployment-identity の env を指すことを見ます。apply の environment は env のデプロイブランチだけを許す
// （ADR-0602 決定14）ので、それが無い env の apply はどこからも起動できない。
func checkDeployEnvs(envs []string, b branches) error {
	known := map[string]bool{}
	var problems []string
	for _, env := range envs {
		known[env] = true
		if _, ok := b.DeployForEnv(env); !ok {
			problems = append(problems, "env "+env+" のデプロイブランチがありません")
		}
	}
	for _, d := range b.Deploy {
		if d.Env != "" && !known[d.Env] {
			problems = append(problems, "デプロイブランチ "+d.Branch+" の env "+d.Env+" を deployment-identity が持ちません")
		}
	}
	if len(problems) > 0 {
		return xerrors.Wrapf(errDeployMismatch, "%s", strings.Join(problems, "; "))
	}
	return nil
}

func identityBase(name string) (string, bool) {
	base, _, ok := splitIdentity(name)
	return base, ok
}

// splitIdentity は、environment 名を env と identitySuffixes の接尾辞に分けます。
func splitIdentity(name string) (string, string, bool) {
	for _, suffix := range identitySuffixes {
		if base, ok := strings.CutSuffix(name, suffix); ok && base != "" {
			return base, suffix, true
		}
	}
	return "", "", false
}

// checkBackendKeys は、env/ の下で *.tf を持つ root ごとに backend.tf の s3 の backend の key を読み、配置から決まる
// key（backendKeys）と一致することを見ます。見た root の数を返す。配置の外の root は oidc-job-lint が見る。
func checkBackendKeys(root string) (int, error) {
	base := filepath.Join(root, envDir)
	n := 0
	err := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if path != base && (strings.HasPrefix(d.Name(), ".") || d.Name() == "tests") {
			return filepath.SkipDir
		}
		tf, err := filepath.Glob(filepath.Join(path, "*.tf"))
		if err != nil || len(tf) == 0 {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		identity := len(parts) == 3 && parts[2] == "identity"
		if len(parts) != 2 && !identity {
			return nil
		}
		key, err := backendKey(filepath.Join(path, "backend.tf"))
		if err != nil {
			return xerrors.Wrapf(errBackendKey, "%s: %v", filepath.ToSlash(rel), err)
		}
		if key != backendKeys[identity] {
			return xerrors.Wrapf(errBackendKey, "%s の backend の key が %s で、%s ではありません", filepath.ToSlash(rel), key, backendKeys[identity])
		}
		n++
		return nil
	})
	if err != nil {
		return 0, err
	}
	if n == 0 {
		return 0, xerrors.Wrapf(errBackendKey, "%s に root がありません", envDir)
	}
	return n, nil
}

// backendKey は、backend.tf の terraform ブロックの s3 の backend の key を、リテラルの文字列として読みます。
func backendKey(path string) (string, error) {
	src, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return "", xerrors.New("backend.tf がありません")
	}
	file, diags := hclsyntax.ParseConfig(src, path, hcl.InitialPos)
	if diags.HasErrors() {
		return "", xerrors.New(diags.Error())
	}
	body, ok := file.Body.(*hclsyntax.Body)
	if !ok {
		return "", xerrors.New("本体を読めません")
	}
	for _, tf := range body.Blocks {
		if tf.Type != "terraform" {
			continue
		}
		for _, b := range tf.Body.Blocks {
			if b.Type == "backend" && len(b.Labels) == 1 && b.Labels[0] == "s3" {
				return literalString(b.Body, "key")
			}
		}
	}
	return "", xerrors.New("s3 の backend がありません")
}

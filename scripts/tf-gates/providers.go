package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/tflock"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// errProviders は、ディレクトリの required_providers を読めないか、正本と合わないときのエラーです。
var errProviders = xerrors.New("required_providers が provider の版の正本と合いません")

// requiredProviders は、dir の `.tf` の terraform block が required_providers で要求する provider の完全な名前を、
// 重複を除いて辞書順で返します。source の host を省いた形は Terraform Registry の名前として読みます。
func requiredProviders(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, xerrors.Wrapf(err, "%s", dir)
	}
	seen := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".tf") {
			continue
		}
		p := filepath.Join(dir, e.Name())
		src, err := os.ReadFile(filepath.Clean(p))
		if err != nil {
			return nil, xerrors.Wrapf(err, "%s", p)
		}
		addrs, err := requiredIn(src, p)
		if err != nil {
			return nil, err
		}
		for _, a := range addrs {
			seen[a] = true
		}
	}
	out := make([]string, 0, len(seen))
	for a := range seen {
		out = append(out, a)
	}
	slices.Sort(out)

	return out, nil
}

func requiredIn(src []byte, name string) ([]string, error) {
	file, diags := hclsyntax.ParseConfig(src, name, hcl.InitialPos)
	if diags.HasErrors() {
		return nil, xerrors.Wrapf(errProviders, "%s: %s", name, diags.Error())
	}
	body, ok := file.Body.(*hclsyntax.Body)
	if !ok {
		return nil, xerrors.Wrapf(errProviders, "%s", name)
	}
	var addrs []string
	for _, tf := range body.Blocks {
		if tf.Type != "terraform" {
			continue
		}
		for _, rp := range tf.Body.Blocks {
			if rp.Type != "required_providers" {
				continue
			}
			for local, attr := range rp.Body.Attributes {
				val, diags := attr.Expr.Value(nil)
				if diags.HasErrors() || !val.Type().IsObjectType() || !val.Type().HasAttribute("source") || val.GetAttr("source").Type() != cty.String {
					return nil, xerrors.Wrapf(errProviders, "%s: required_providers の %s の source を読めません", name, local)
				}
				addrs = append(addrs, providerAddr(val.GetAttr("source").AsString()))
			}
		}
	}

	return addrs, nil
}

// providerAddr は source を `host/namespace/type` の小文字の形にします。
func providerAddr(source string) string {
	s := strings.ToLower(source)
	if strings.Count(s, "/") == 1 {
		return tflock.Registry + "/" + s
	}

	return s
}

// placeLock は、root でないディレクトリ dir に正本の lock を置くか消すかします。provider を要求しないディレクトリには
// 置かず、前の起動が置いた lock を消します（readonly の init は、要らない provider の entry も変更として落とす）。
// 要求する provider の集合が正本と違えば errProviders です —— 写しの絞り込みは持たない。
func placeLock(dir string, c canonical) error {
	req, err := requiredProviders(dir)
	if err != nil {
		return err
	}
	lockPath := filepath.Join(dir, tflock.LockFile)
	if len(req) == 0 {
		if err := os.Remove(lockPath); err != nil && !xerrors.Is(err, fs.ErrNotExist) {
			return xerrors.Wrapf(err, "%s の削除", lockPath)
		}

		return nil
	}
	want := make([]string, 0, len(c.decls))
	for _, d := range c.decls {
		want = append(want, providerAddr(d.Source))
	}
	slices.Sort(want)
	if !slices.Equal(req, want) {
		return xerrors.Wrapf(errProviders, "%s は %s を要求し、正本は %s を固定します", dir, strings.Join(req, " "), strings.Join(want, " "))
	}
	if err := os.WriteFile(lockPath, c.content, lockPerm); err != nil {
		return xerrors.Wrapf(err, "%s の書き込み", lockPath)
	}

	return nil
}

package main

import (
	"os"
	"path/filepath"
	"slices"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const (
	copyVariable   = "aws_partitions"
	providerName   = "aws"
	variablesBlock = "variables"
)

var (
	errRead       = xerrors.New("ファイルを読めません")
	errSyntax     = xerrors.New("HCL の構文の誤りがあります")
	errConstraint = xerrors.New("versions.tf から provider の source と版の制約を読めません")
	errCopy       = xerrors.New("写しを読めません")
	errCopyCount  = xerrors.New("写しを宣言する属性がちょうど1つではありません")
)

func parseHCL(path string) (*hclsyntax.Body, error) {
	src, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, xerrors.Wrapf(errRead, "%v", err)
	}
	f, diags := hclsyntax.ParseConfig(src, path, hcl.InitialPos)
	if diags.HasErrors() {
		return nil, xerrors.Wrapf(errSyntax, "%s", diags.Error())
	}

	// ParseConfig の body は常に *hclsyntax.Body です。
	return f.Body.(*hclsyntax.Body), nil
}

// readConstraint は、`terraform` の `required_providers` の `aws` から source と version を返します。
// 該当する宣言がちょうど1つでなければ失敗します。
func readConstraint(path string) (string, string, error) {
	body, err := parseHCL(path)
	if err != nil {
		return "", "", err
	}
	var found []*hclsyntax.Attribute
	for _, tb := range body.Blocks {
		if tb.Type != "terraform" {
			continue
		}
		for _, rp := range tb.Body.Blocks {
			if rp.Type != "required_providers" {
				continue
			}
			if attr, ok := rp.Body.Attributes[providerName]; ok {
				found = append(found, attr)
			}
		}
	}
	if len(found) != 1 {
		return "", "", xerrors.Wrapf(errConstraint, "required_providers の %s が %d 件です", providerName, len(found))
	}
	value, diags := found[0].Expr.Value(nil)
	if diags.HasErrors() || !value.Type().IsObjectType() {
		return "", "", xerrors.Wrapf(errConstraint, "%s がリテラルのオブジェクトではありません", providerName)
	}
	source, err := stringAttr(value, "source")
	if err != nil {
		return "", "", err
	}
	constraint, err := stringAttr(value, "version")
	if err != nil {
		return "", "", err
	}

	return source, constraint, nil
}

func stringAttr(obj cty.Value, name string) (string, error) {
	if !obj.Type().HasAttribute(name) {
		return "", xerrors.Wrapf(errConstraint, "%s がありません", name)
	}
	v := obj.GetAttr(name)
	if v.IsNull() || !v.IsKnown() || v.Type() != cty.String || v.AsString() == "" {
		return "", xerrors.Wrapf(errConstraint, "%s が空でない文字列ではありません", name)
	}

	return v.AsString(), nil
}

// readCopy は、ファイルのどの深さの `variables` の block も見て、写しの属性がちょうど1つであることを
// 確かめます。2つ目を足した変更を、片方だけ見て一致と報告しないためです。
func readCopy(path string) (partitionSet, error) {
	body, err := parseHCL(path)
	if err != nil {
		return nil, err
	}
	attrs := copyAttributes(body)
	if len(attrs) != 1 {
		return nil, xerrors.Wrapf(errCopyCount, "%s が %d 件です", copyVariable, len(attrs))
	}
	value, diags := attrs[0].Expr.Value(nil)
	if diags.HasErrors() {
		return nil, xerrors.Wrapf(errCopy, "%s がリテラルではありません: %s", copyVariable, diags.Error())
	}
	if !value.Type().IsObjectType() {
		return nil, xerrors.Wrapf(errCopy, "%s が partition をキーにした写像ではありません", copyVariable)
	}
	if err := uniqueKeys(attrs[0].Expr); err != nil {
		return nil, err
	}

	set := partitionSet{}
	for it := value.ElementIterator(); it.Next(); {
		k, v := it.Element()
		partition := k.AsString()
		regions, err := regionList(v)
		if err != nil {
			return nil, xerrors.Wrapf(err, "partition %q", partition)
		}
		set[partition] = regions
	}
	if len(set) == 0 {
		return nil, xerrors.Wrapf(errCopy, "%s が空です", copyVariable)
	}

	return set, nil
}

func copyAttributes(body *hclsyntax.Body) []*hclsyntax.Attribute {
	var found []*hclsyntax.Attribute
	for _, b := range body.Blocks {
		if b.Type == variablesBlock {
			if attr, ok := b.Body.Attributes[copyVariable]; ok {
				found = append(found, attr)
			}
		}
		found = append(found, copyAttributes(b.Body)...)
	}

	return found
}

// uniqueKeys は、写像のリテラルに同じ partition が2度ないことを確かめます。評価した値では後のキーが
// 先のキーを黙って上書きするので、ソースの式で数えます。
func uniqueKeys(expr hclsyntax.Expression) error {
	obj, ok := expr.(*hclsyntax.ObjectConsExpr)
	if !ok {
		return xerrors.Wrapf(errCopy, "%s が写像のリテラルではありません", copyVariable)
	}
	var keys []string
	for _, item := range obj.Items {
		k, diags := item.KeyExpr.Value(nil)
		if diags.HasErrors() || k.Type() != cty.String {
			return xerrors.Wrapf(errCopy, "%s のキーが文字列のリテラルではありません", copyVariable)
		}
		if slices.Contains(keys, k.AsString()) {
			return xerrors.Wrapf(errCopy, "partition %q が2度あります", k.AsString())
		}
		keys = append(keys, k.AsString())
	}

	return nil
}

func regionList(v cty.Value) ([]string, error) {
	if !v.Type().IsTupleType() {
		return nil, xerrors.Wrapf(errCopy, "region の一覧が配列ではありません")
	}
	var regions []string
	for it := v.ElementIterator(); it.Next(); {
		_, r := it.Element()
		if r.IsNull() || r.Type() != cty.String || r.AsString() == "" {
			return nil, xerrors.Wrapf(errCopy, "region が空でない文字列ではありません")
		}
		if slices.Contains(regions, r.AsString()) {
			return nil, xerrors.Wrapf(errCopy, "region %q が2度あります", r.AsString())
		}
		regions = append(regions, r.AsString())
	}
	if len(regions) == 0 {
		return nil, xerrors.Wrapf(errCopy, "region の一覧が空です")
	}
	slices.Sort(regions)

	return regions, nil
}

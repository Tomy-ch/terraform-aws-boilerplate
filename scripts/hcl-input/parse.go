package main

import (
	"cmp"
	"regexp"
	"slices"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// dynamicContent は conftest/hcl/layer/hcl2.rego の fold_dynamic と同じ正規表現です。Rego の values_at と
// 同じ並びで引けるよう、規則も適用の回数（2回）も揃えます。
var dynamicContent = regexp.MustCompile(`(^|\.)dynamic\.([^.]+)\.content(\.|$)`)

// labelCounts は、ラベルの個数を Terraform が決めている block の種類です。
var labelCounts = map[string]int{"resource": 2, "data": 2, "ephemeral": 2, "action": 2, "provider": 1, "module": 1}

type block struct {
	File       string      `json:"file"`
	Kind       string      `json:"kind"`
	Labels     []string    `json:"labels"`
	Attributes []attribute `json:"attributes"`
}

// attribute の Refs は空でも必ず在り、「すべて読んで参照が無い」を意味します。
type attribute struct {
	Path string `json:"path"`
	Refs []ref  `json:"refs"`
}

// parseFile は、name のファイルの最上位の block をソースの順に返します。診断は warning を含めて1つでも
// あれば errSyntax です。
func parseFile(name string, src []byte) ([]block, error) {
	body, err := parseConfigBody(name, src)
	if err != nil {
		return nil, err
	}

	return blocksOf(name, body)
}

// blocksOf は、body の最上位の block を、既定の形でソースの順に返します。
func blocksOf(name string, body *hclsyntax.Body) ([]block, error) {
	blocks := make([]block, 0, len(body.Blocks))
	for _, b := range body.Blocks {
		attrs, err := bodyAttributes(b.Body, nil)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, block{File: name, Kind: b.Type, Labels: append([]string{}, b.Labels...), Attributes: attrs})
	}

	return blocks, nil
}

// parseConfigBody は name のファイルを構文として読み、最上位の body を返します。診断は warning を含めて
// 1つでもあれば errSyntax、ファイルの直下の属性とラベルの個数の違う resource・data は errUnsupportedInput です。
func parseConfigBody(name string, src []byte) (*hclsyntax.Body, error) {
	f, diags := hclsyntax.ParseConfig(src, name, hcl.InitialPos)
	if len(diags) > 0 {
		return nil, xerrors.Wrapf(errSyntax, "%s", diags.Error())
	}
	// ParseConfig の body は常に *hclsyntax.Body です。
	body := f.Body.(*hclsyntax.Body)
	if len(body.Attributes) > 0 {
		first := attributesOf(body)[0]

		return nil, xerrors.Wrapf(errUnsupportedInput, "%s: ファイルの直下に属性 %q があります", first.SrcRange, first.Name)
	}
	for _, b := range body.Blocks {
		if want, ok := labelCounts[b.Type]; ok && len(b.Labels) != want {
			return nil, xerrors.Wrapf(errUnsupportedInput, "%s: %s の block のラベルが %d 個です（%d 個を要します）", b.TypeRange, b.Type, len(b.Labels), want)
		}
	}

	return body, nil
}

// bodyAttributes は、body の属性と入れ子の block の属性を、深さ優先のソースの順に返します。入れ子の
// block は種類とラベルを段に持ち（Conftest の hcl2 の形と同じ）、path は foldDynamic で畳みます。
func bodyAttributes(body *hclsyntax.Body, prefix []string) ([]attribute, error) {
	type item struct {
		pos  int
		attr *hclsyntax.Attribute
		blk  *hclsyntax.Block
	}
	items := make([]item, 0, len(body.Attributes)+len(body.Blocks))
	for _, a := range attributesOf(body) {
		items = append(items, item{pos: a.SrcRange.Start.Byte, attr: a})
	}
	for _, b := range body.Blocks {
		items = append(items, item{pos: b.TypeRange.Start.Byte, blk: b})
	}
	slices.SortStableFunc(items, func(a, b item) int { return cmp.Compare(a.pos, b.pos) })

	attrs := make([]attribute, 0, len(items))
	for _, it := range items {
		if it.attr != nil {
			refs, err := walkExpr(it.attr.Expr)
			if err != nil {
				return nil, err
			}
			path := append(slices.Clone(prefix), it.attr.Name)
			attrs = append(attrs, attribute{Path: foldDynamic(strings.Join(path, ".")), Refs: refs})

			continue
		}
		sub := append(append(slices.Clone(prefix), it.blk.Type), it.blk.Labels...)
		nested, err := bodyAttributes(it.blk.Body, sub)
		if err != nil {
			return nil, err
		}
		attrs = append(attrs, nested...)
	}

	return attrs, nil
}

func attributesOf(body *hclsyntax.Body) []*hclsyntax.Attribute {
	attrs := make([]*hclsyntax.Attribute, 0, len(body.Attributes))
	for _, a := range body.Attributes {
		attrs = append(attrs, a)
	}
	slices.SortFunc(attrs, func(a, b *hclsyntax.Attribute) int { return cmp.Compare(a.SrcRange.Start.Byte, b.SrcRange.Start.Byte) })

	return attrs
}

// foldDynamic は `dynamic.<名前>.content` を `<名前>` に畳みます。同じ正規表現を2回当てると、隣り合う
// dynamic の奇数番目と偶数番目が畳まれ、何段の入れ子でも畳み切ります。
func foldDynamic(dotted string) string {
	const repl = "${1}${2}${3}"

	return dynamicContent.ReplaceAllString(dynamicContent.ReplaceAllString(dotted, repl), repl)
}

// synBlock は -format syntax の最上位の block です。入れ子の block と属性は、深さを問わず Blocks と
// Attributes に深さ優先・ソースの順で平らに並び、Parent（Blocks の位置、直下は -1）で親を引けます。
type synBlock struct {
	File       string    `json:"file"`
	Line       int       `json:"line"`
	Type       string    `json:"type"`
	Labels     []string  `json:"labels"`
	Blocks     []nested  `json:"blocks"`
	Attributes []synAttr `json:"attributes"`
}

// nested の Path は種類とラベルを段に持つ dotted な並びを foldDynamic で畳んだものです。属性を持たない
// block も1つの nested として残ります。
type nested struct {
	Path    string   `json:"path"`
	Parent  int      `json:"parent"`
	Line    int      `json:"line"`
	Type    string   `json:"type"`
	Labels  []string `json:"labels"`
	Dynamic bool     `json:"dynamic"`
}

type synAttr struct {
	Path   string `json:"path"`
	Parent int    `json:"parent"`
	Line   int    `json:"line"`
	Expr   expr   `json:"expr"`
	Refs   []ref  `json:"refs"`
}

// parseSyntaxFile は、name のファイルの最上位の block を、入れ子の block と属性の式を含めてソースの順に
// 返します。受理の条件は parseFile と同じです。
func parseSyntaxFile(name string, src []byte) ([]synBlock, error) {
	body, err := parseConfigBody(name, src)
	if err != nil {
		return nil, err
	}

	return syntaxBlocks(name, body)
}

// syntaxBlocks は、body の最上位の block を、-format syntax の形でソースの順に返します。
func syntaxBlocks(name string, body *hclsyntax.Body) ([]synBlock, error) {
	blocks := make([]synBlock, 0, len(body.Blocks))
	for _, b := range body.Blocks {
		sb := synBlock{
			File: name, Line: b.TypeRange.Start.Line, Type: b.Type, Labels: append([]string{}, b.Labels...),
			Blocks: []nested{}, Attributes: []synAttr{},
		}
		if err := syntaxBody(b.Body, nil, -1, &sb); err != nil {
			return nil, err
		}
		blocks = append(blocks, sb)
	}

	return blocks, nil
}

// syntaxBody は body の属性と入れ子の block を、親 parent・前置き prefix のもとで sb へ足します。
func syntaxBody(body *hclsyntax.Body, prefix []string, parent int, sb *synBlock) error {
	type item struct {
		pos  int
		attr *hclsyntax.Attribute
		blk  *hclsyntax.Block
	}
	items := make([]item, 0, len(body.Attributes)+len(body.Blocks))
	for _, a := range attributesOf(body) {
		items = append(items, item{pos: a.SrcRange.Start.Byte, attr: a})
	}
	for _, b := range body.Blocks {
		items = append(items, item{pos: b.TypeRange.Start.Byte, blk: b})
	}
	slices.SortStableFunc(items, func(a, b item) int { return cmp.Compare(a.pos, b.pos) })

	for _, it := range items {
		if it.attr != nil {
			refs, err := walkExpr(it.attr.Expr)
			if err != nil {
				return err
			}
			e, err := exprOf(it.attr.Expr)
			if err != nil {
				return err
			}
			path := append(slices.Clone(prefix), it.attr.Name)
			sb.Attributes = append(sb.Attributes, synAttr{
				Path: foldDynamic(strings.Join(path, ".")), Parent: parent, Line: it.attr.SrcRange.Start.Line, Expr: e, Refs: refs,
			})

			continue
		}
		sub := append(append(slices.Clone(prefix), it.blk.Type), it.blk.Labels...)
		sb.Blocks = append(sb.Blocks, nested{
			Path: foldDynamic(strings.Join(sub, ".")), Parent: parent, Line: it.blk.TypeRange.Start.Line,
			Type: it.blk.Type, Labels: append([]string{}, it.blk.Labels...), Dynamic: it.blk.Type == "dynamic",
		})
		if err := syntaxBody(it.blk.Body, sub, len(sb.Blocks)-1, sb); err != nil {
			return err
		}
	}

	return nil
}

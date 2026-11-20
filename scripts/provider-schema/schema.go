package main

import "slices"

type providerSchema struct {
	ResourceSchemas map[string]resourceSchema `json:"resource_schemas"`
}

type resourceSchema struct {
	Block schemaBlock `json:"block"`
}

type schemaBlock struct {
	Attributes map[string]schemaAttribute `json:"attributes"`
	BlockTypes map[string]blockType       `json:"block_types"`
}

type schemaAttribute struct {
	Optional   bool `json:"optional"`
	Required   bool `json:"required"`
	Deprecated bool `json:"deprecated"`
}

// blockType は入れ子の block の種類です。入れ子の中身は読まないので、非推奨の印だけを受けます。
type blockType struct {
	Block struct {
		Deprecated bool `json:"deprecated"`
	} `json:"block"`
}

// entry は1つの resource の型について層の検査が読むものです。conftest/hcl/layer/layer.rego の
// well_formed_schema_entry がこの形を検査します。
type entry struct {
	Arguments  []string `json:"arguments"`
	Deprecated []string `json:"deprecated"`
	Blocks     []string `json:"blocks"`
}

// extract は、書ける属性（optional か required）と入れ子の block を arguments に、そのうち Provider が
// 非推奨とするものを deprecated に、入れ子の block を blocks に、それぞれ昇順で入れます。
// 「Provider の schema が非推奨とする書ける引数」の唯一の定義で、層の検査（merge）と Policy Test
// （plan-deprecated）はどちらもここから取り出します（ADR-0102 決定18、ADR-0402 決定18）。
func extract(r resourceSchema) entry {
	e := entry{Arguments: []string{}, Deprecated: []string{}, Blocks: []string{}}
	for name, a := range r.Block.Attributes {
		if !a.Optional && !a.Required {
			continue
		}
		e.Arguments = append(e.Arguments, name)
		if a.Deprecated {
			e.Deprecated = append(e.Deprecated, name)
		}
	}
	for name, b := range r.Block.BlockTypes {
		e.Arguments = append(e.Arguments, name)
		e.Blocks = append(e.Blocks, name)
		if b.Block.Deprecated {
			e.Deprecated = append(e.Deprecated, name)
		}
	}
	slices.Sort(e.Arguments)
	slices.Sort(e.Deprecated)
	slices.Sort(e.Blocks)

	return e
}

// union は版ごと（merge）か root ごと（plan-deprecated）の一覧を型ごとに合わせ、3つの配列それぞれの和を
// 重複なしの昇順で返します。
func union(parts []map[string]entry) map[string]entry {
	out := map[string]entry{}
	for _, m := range parts {
		for typ, e := range m {
			u, ok := out[typ]
			if !ok {
				u = entry{Arguments: []string{}, Deprecated: []string{}, Blocks: []string{}}
			}
			u.Arguments = append(u.Arguments, e.Arguments...)
			u.Deprecated = append(u.Deprecated, e.Deprecated...)
			u.Blocks = append(u.Blocks, e.Blocks...)
			out[typ] = u
		}
	}
	for typ, u := range out {
		for _, s := range []*[]string{&u.Arguments, &u.Deprecated, &u.Blocks} {
			slices.Sort(*s)
			*s = slices.Compact(*s)
		}
		out[typ] = u
	}

	return out
}

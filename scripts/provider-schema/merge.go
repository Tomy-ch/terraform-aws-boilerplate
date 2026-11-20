package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/atomicwrite"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// rawSchema は `terraform providers schema -json` の出力のうち、読む欄だけです。欄の欠けを空と区別する
// ため、format_version と provider_schemas はポインタで受けます。
type rawSchema struct {
	FormatVersion   *string                    `json:"format_version"`
	ProviderSchemas *map[string]providerSchema `json:"provider_schemas"`
}

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
// 非推奨の条件は .makefiles/terraform/policy.mk の TF_POLICY_DEPRECATED と同じです。
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

// decodeSchema は1つの版の raw.json を読み、provider の resource の型ごとの schema を返します。
func decodeSchema(name string, data []byte, provider string) (map[string]resourceSchema, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, xerrors.Wrapf(errSchemaMissing, "%s が空です", name)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	var raw rawSchema
	if err := dec.Decode(&raw); err != nil {
		return nil, xerrors.Wrapf(errSchemaMalformed, "%s: JSON として読めません（%v）", name, err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, xerrors.Wrapf(errSchemaMalformed, "%s: JSON の後ろに余計な出力があります", name)
	}
	if raw.FormatVersion == nil || !strings.HasPrefix(*raw.FormatVersion, "1.") {
		return nil, xerrors.Wrapf(errSchemaMalformed, "%s: format_version が無いか、1. で始まりません", name)
	}
	if raw.ProviderSchemas == nil {
		return nil, xerrors.Wrapf(errSchemaMalformed, "%s: provider_schemas がありません", name)
	}
	ps, ok := (*raw.ProviderSchemas)[provider]
	if !ok {
		return nil, xerrors.Wrapf(errSchemaMalformed, "%s: provider_schemas に %s がありません", name, provider)
	}
	if len(ps.ResourceSchemas) == 0 {
		return nil, xerrors.Wrapf(errNoResources, "%s: %s", name, provider)
	}

	return ps.ResourceSchemas, nil
}

// readVersions は版の一覧を読みます。版の形でない行、重複した版、0件の一覧はエラーです。
func readVersions(path string) ([]string, error) {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, xerrors.Wrapf(errVersionList, "%s を開けません（%v）", path, err)
	}
	defer func() { _ = f.Close() }()

	var versions []string
	sc := bufio.NewScanner(f)
	n := 0
	for sc.Scan() {
		n++
		v := sc.Text()
		if !versionPattern.MatchString(v) {
			return nil, xerrors.Wrapf(errVersionList, "%s:%d: %q は版の形ではありません", path, n, v)
		}
		if slices.Contains(versions, v) {
			return nil, xerrors.Wrapf(errVersionList, "%s:%d: 版 %s が重複しています", path, n, v)
		}
		versions = append(versions, v)
	}
	if err := sc.Err(); err != nil {
		return nil, xerrors.Wrapf(errVersionList, "%s を読めません（%v）", path, err)
	}
	if len(versions) == 0 {
		return nil, xerrors.Wrapf(errNoVersions, "%s", path)
	}

	return versions, nil
}

// union は版ごとの一覧を型ごとに合わせ、3つの配列それぞれの和を重複なしの昇順で返します。
func union(perVersion []map[string]entry) map[string]entry {
	out := map[string]entry{}
	for _, m := range perVersion {
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

// merge は、すべての版を読み終えてから -out を書きます。読めない版が1つでもあれば -out に触れません。
func merge(args []string, stderr io.Writer) error {
	fset := flag.NewFlagSet("provider-schema merge", flag.ContinueOnError)
	fset.SetOutput(stderr)
	providerFlag := fset.String("provider", "", "schema を取り出す Provider のアドレス（例: registry.terraform.io/hashicorp/aws）")
	dir := fset.String("dir", "", "prepare が版ごとのディレクトリを置いたディレクトリ")
	outPath := fset.String("out", "", "書き出す JSON のパス")
	if err := fset.Parse(args); err != nil {
		return xerrors.Wrapf(errUsage, "%v", err)
	}
	if *providerFlag == "" || *dir == "" || *outPath == "" || fset.NArg() > 0 {
		return errUsage
	}
	provider, err := parseProvider(*providerFlag)
	if err != nil {
		return err
	}
	versions, err := readVersions(filepath.Join(*dir, versionsName))
	if err != nil {
		return err
	}

	perVersion := make([]map[string]entry, 0, len(versions))
	counts := make([]string, 0, len(versions))
	for _, v := range versions {
		path := filepath.Join(*dir, v, rawName)
		data, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			return xerrors.Wrapf(errSchemaMissing, "%s を読めません（%v）", path, err)
		}
		schemas, err := decodeSchema(path, data, provider.full)
		if err != nil {
			return err
		}
		m := make(map[string]entry, len(schemas))
		for typ, r := range schemas {
			m[typ] = extract(r)
		}
		perVersion = append(perVersion, m)
		counts = append(counts, fmt.Sprintf("%s: %d 型", v, len(m)))
	}

	merged := union(perVersion)
	if len(merged) == 0 {
		return xerrors.Wrapf(errNoResources, "版 %s の和", strings.Join(versions, " "))
	}
	data, err := json.Marshal(map[string]map[string]entry{"layer_schema": merged})
	if err != nil {
		return xerrors.Wrap(err, "JSON の組み立て")
	}
	if err := atomicwrite.Apply(map[string]string{*outPath: string(data) + "\n"}, outPerm); err != nil {
		return xerrors.Wrapf(err, "%s への書き込み", *outPath)
	}
	fmt.Fprintf(stderr, "provider-schema merge: %s の版ごとの型の数（%s）、和 %d 型を %s へ書きました\n",
		provider.full, strings.Join(counts, "、"), len(merged), *outPath)

	return nil
}

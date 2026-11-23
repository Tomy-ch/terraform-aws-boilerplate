package main

import (
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

// planLine は `terraform test -json -verbose` の出力の1つの値のうち、読む欄だけです。test_plan は
// plan の JSON（`terraform show -json` の形）で、provider_schemas は欄の欠けを空と区別するためポインタで受けます。
type planLine struct {
	Type     string `json:"type"`
	TestPlan *struct {
		ProviderSchemas *map[string]providerSchema `json:"provider_schemas"`
		ResourceChanges []struct {
			Mode string `json:"mode"`
			Type string `json:"type"`
		} `json:"resource_changes"`
	} `json:"test_plan"`
}

// decodePlan は1つの root の jsonl を読み、test_plan のすべての Provider の resource の型ごとの schema を
// 返します。値を1つずつ読むので、AWS Provider の schema を含む長い行も読めます。test_plan の値が
// ちょうど1件でないもの、schema に resource の型が無いもの、resource_changes に managed の変更が無いか、
// その型が schema に無いもの（ADR-0702 決定15 の独立の経路）、同じ型を2つの Provider が持つものはエラーです。
func decodePlan(name string, r io.Reader) (map[string]resourceSchema, error) {
	dec := json.NewDecoder(r)
	var plans []planLine
	for {
		var l planLine
		err := dec.Decode(&l)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, xerrors.Wrapf(errPlanMalformed, "%s: JSON として読めません（%v）", name, err)
		}
		if l.Type == "test_plan" {
			plans = append(plans, l)
		}
	}
	if len(plans) != 1 || plans[0].TestPlan == nil {
		return nil, xerrors.Wrapf(errPlanMalformed, "%s: test_plan が %d 件です（1件を期待）", name, len(plans))
	}
	tp := plans[0].TestPlan
	if tp.ProviderSchemas == nil {
		return nil, xerrors.Wrapf(errNoResources, "%s: test_plan に provider_schemas がありません", name)
	}

	schemas := map[string]resourceSchema{}
	owner := map[string]string{}
	for provider, ps := range *tp.ProviderSchemas {
		for typ, rs := range ps.ResourceSchemas {
			if other, ok := owner[typ]; ok {
				return nil, xerrors.Wrapf(errPlanMalformed, "%s: 型 %s を %s と %s の2つの Provider が持っています", name, typ, other, provider)
			}
			owner[typ] = provider
			schemas[typ] = rs
		}
	}
	if len(schemas) == 0 {
		return nil, xerrors.Wrapf(errNoResources, "%s: test_plan の provider_schemas", name)
	}
	var missing []string
	managed := 0
	for _, rc := range tp.ResourceChanges {
		if rc.Mode != "managed" {
			continue
		}
		managed++
		if _, ok := schemas[rc.Type]; !ok && !slices.Contains(missing, rc.Type) {
			missing = append(missing, rc.Type)
		}
	}
	// 突き合わせの相手が0件なら、突き合わせは何も見ない。schema を持つ plan は managed の変更を持つ。
	if managed == 0 {
		return nil, xerrors.Wrapf(errPlanMalformed, "%s: test_plan の resource_changes に managed の変更がありません", name)
	}
	if len(missing) > 0 {
		slices.Sort(missing)

		return nil, xerrors.Wrapf(errPlanTypeMissing, "%s: %s", name, strings.Join(missing, " "))
	}

	return schemas, nil
}

// planDeprecated は、reference の root ごとの jsonl から非推奨の書ける引数を extract で取り出し、root を
// またいだ和を Policy Test が `--data` で読む形（data のトップキー provider_schema）で -out へ書きます。
// すべての入力を読み終えてから書くので、読めない入力が1つでもあれば -out に触れません。
func planDeprecated(args []string, stderr io.Writer) error {
	fset := flag.NewFlagSet("provider-schema plan-deprecated", flag.ContinueOnError)
	fset.SetOutput(stderr)
	outPath := fset.String("out", "", "書き出す JSON のパス")
	listPath := fset.String("list", "", "読む jsonl の一覧（1行に1件）。位置引数の代わりに渡す")
	if err := fset.Parse(args); err != nil {
		return xerrors.Wrapf(errUsage, "%v", err)
	}
	if *outPath == "" || (*listPath != "" && fset.NArg() > 0) {
		return errUsage
	}
	paths := fset.Args()
	if *listPath != "" {
		raw, err := os.ReadFile(filepath.Clean(*listPath))
		if err != nil {
			return xerrors.Wrapf(errPlanMalformed, "%s を読めません（%v）", *listPath, err)
		}
		paths = nil
		for line := range strings.SplitSeq(string(raw), "\n") {
			if line != "" {
				paths = append(paths, line)
			}
		}
	}
	if len(paths) == 0 {
		return errNoPlans
	}

	perRoot := make([]map[string]entry, 0, len(paths))
	counts := make([]string, 0, len(paths))
	for _, path := range paths {
		f, err := os.Open(filepath.Clean(path))
		if err != nil {
			return xerrors.Wrapf(errPlanMalformed, "%s を開けません（%v）", path, err)
		}
		schemas, err := decodePlan(path, f)
		_ = f.Close()
		if err != nil {
			return err
		}
		m := make(map[string]entry, len(schemas))
		writable := 0
		for typ, r := range schemas {
			m[typ] = extract(r)
			if len(m[typ].Arguments) > 0 {
				writable++
			}
		}
		// schema の欄の名前が変わって読めなくなると、その root のすべての型が書ける引数を持たない形で揃う。
		// root ごとに見る。和で見ると、ほかの root の型が隠す。
		if writable == 0 {
			return xerrors.Wrapf(errNoArguments, "%s: %d 型", path, len(m))
		}
		perRoot = append(perRoot, m)
		counts = append(counts, fmt.Sprintf("%s: %d 型", path, len(m)))
	}

	merged := union(perRoot)
	deprecated := make(map[string][]string, len(merged))
	for typ, e := range merged {
		deprecated[typ] = e.Deprecated
	}
	data, err := json.Marshal(map[string]map[string]map[string][]string{"provider_schema": {"deprecated_arguments": deprecated}})
	if err != nil {
		return xerrors.Wrap(err, "JSON の組み立て")
	}
	if err := atomicwrite.Apply(map[string]string{*outPath: string(data) + "\n"}, outPerm); err != nil {
		return xerrors.Wrapf(err, "%s への書き込み", *outPath)
	}
	fmt.Fprintf(stderr, "provider-schema plan-deprecated: 入力 %d 件（%s）、和 %d 型を %s へ書きました\n",
		len(perRoot), strings.Join(counts, "、"), len(merged), *outPath)

	return nil
}

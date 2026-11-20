package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const (
	partitionsVar      = "partitions"
	regionsField       = "regions"
	partitionIDSuffix  = "PartitionID"
	regionIDSuffix     = "RegionID"
	endpointsSourceTag = "endpoints_gen.go"
)

var (
	errEndpointsSyntax = xerrors.New("endpoints の Go のソースを解析できません")
	errEndpointsShape  = xerrors.New("endpoints の Go のソースが知らない形です")
	errNoPartitions    = xerrors.New("endpoints から partition を1件も読めません")
	errConstMismatch   = xerrors.New("endpoints の partitions の map が、partition と region の ID の const と一致しません")
)

// partitionSet は partition の ID から、その region の ID の集合（昇順）への写像です。
type partitionSet map[string][]string

// parseEndpoints は、`partitions` の map から partition と region の ID を読みます。読んだ集合は、
// 同じファイルの `*PartitionID` と `*RegionID` の const の集合と突き合わせます（ADR-0702 決定15）。
func parseEndpoints(src []byte) (partitionSet, error) {
	f, err := parser.ParseFile(token.NewFileSet(), endpointsSourceTag, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, xerrors.Wrapf(errEndpointsSyntax, "%v", err)
	}
	consts, partitionIDs, regionIDs, err := idConsts(f)
	if err != nil {
		return nil, err
	}
	lit, err := partitionsLiteral(f)
	if err != nil {
		return nil, err
	}

	set := partitionSet{}
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			return nil, xerrors.Wrapf(errEndpointsShape, "partitions の要素がキーと値の組ではありません")
		}
		id, err := resolveID(kv.Key, consts)
		if err != nil {
			return nil, err
		}
		if _, dup := set[id]; dup {
			return nil, xerrors.Wrapf(errEndpointsShape, "partition %q が2度あります", id)
		}
		regions, err := regionKeys(kv.Value, consts)
		if err != nil {
			return nil, xerrors.Wrapf(err, "partition %q", id)
		}
		set[id] = regions
	}
	if len(set) == 0 {
		return nil, errNoPartitions
	}

	if got := slices.Sorted(maps.Keys(set)); !slices.Equal(got, partitionIDs) {
		return nil, xerrors.Wrapf(errConstMismatch, "partitions の map %v / const %v", got, partitionIDs)
	}
	var allRegions []string
	for _, rs := range set {
		allRegions = append(allRegions, rs...)
	}
	slices.Sort(allRegions)
	if !slices.Equal(allRegions, regionIDs) {
		return nil, xerrors.Wrapf(errConstMismatch, "regions の map %d 件 / const %d 件", len(allRegions), len(regionIDs))
	}

	return set, nil
}

// idConsts は、`*PartitionID` と `*RegionID` の const を名前から値へ引ける形で返します。値は文字列の
// リテラルに限ります。
func idConsts(f *ast.File) (map[string]string, []string, []string, error) {
	consts := map[string]string{}
	var partitionIDs, regionIDs []string
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range vs.Names {
				isPartition := strings.HasSuffix(name.Name, partitionIDSuffix)
				if !isPartition && !strings.HasSuffix(name.Name, regionIDSuffix) {
					continue
				}
				if len(vs.Values) != len(vs.Names) {
					return nil, nil, nil, xerrors.Wrapf(errEndpointsShape, "const %s に値がありません", name.Name)
				}
				value, err := stringLiteral(vs.Values[i])
				if err != nil {
					return nil, nil, nil, xerrors.Wrapf(err, "const %s", name.Name)
				}
				consts[name.Name] = value
				if isPartition {
					partitionIDs = append(partitionIDs, value)
				} else {
					regionIDs = append(regionIDs, value)
				}
			}
		}
	}
	slices.Sort(partitionIDs)
	slices.Sort(regionIDs)

	return consts, partitionIDs, regionIDs, nil
}

func partitionsLiteral(f *ast.File) (*ast.CompositeLit, error) {
	var found *ast.CompositeLit
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.VAR {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range vs.Names {
				if name.Name != partitionsVar {
					continue
				}
				if found != nil || i >= len(vs.Values) {
					return nil, xerrors.Wrapf(errEndpointsShape, "var %s が1つの合成リテラルではありません", partitionsVar)
				}
				lit, ok := vs.Values[i].(*ast.CompositeLit)
				if !ok {
					return nil, xerrors.Wrapf(errEndpointsShape, "var %s が合成リテラルではありません", partitionsVar)
				}
				found = lit
			}
		}
	}
	if found == nil {
		return nil, xerrors.Wrapf(errEndpointsShape, "var %s がありません", partitionsVar)
	}

	return found, nil
}

// regionKeys は、partition の値の `regions` の map のキーを返します。
func regionKeys(value ast.Expr, consts map[string]string) ([]string, error) {
	lit, ok := value.(*ast.CompositeLit)
	if !ok {
		return nil, xerrors.Wrapf(errEndpointsShape, "partition の値が合成リテラルではありません")
	}
	var regionsLit *ast.CompositeLit
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if key, ok := kv.Key.(*ast.Ident); ok && key.Name == regionsField {
			if regionsLit != nil {
				return nil, xerrors.Wrapf(errEndpointsShape, "%s が2度あります", regionsField)
			}
			if regionsLit, ok = kv.Value.(*ast.CompositeLit); !ok {
				return nil, xerrors.Wrapf(errEndpointsShape, "%s が合成リテラルではありません", regionsField)
			}
		}
	}
	if regionsLit == nil {
		return nil, xerrors.Wrapf(errEndpointsShape, "%s がありません", regionsField)
	}

	regions := make([]string, 0, len(regionsLit.Elts))
	for _, elt := range regionsLit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			return nil, xerrors.Wrapf(errEndpointsShape, "%s の要素がキーと値の組ではありません", regionsField)
		}
		id, err := resolveID(kv.Key, consts)
		if err != nil {
			return nil, err
		}
		if slices.Contains(regions, id) {
			return nil, xerrors.Wrapf(errEndpointsShape, "region %q が2度あります", id)
		}
		regions = append(regions, id)
	}
	slices.Sort(regions)

	return regions, nil
}

func resolveID(expr ast.Expr, consts map[string]string) (string, error) {
	if ident, ok := expr.(*ast.Ident); ok {
		value, found := consts[ident.Name]
		if !found {
			return "", xerrors.Wrapf(errEndpointsShape, "ID の const %s がありません", ident.Name)
		}

		return value, nil
	}

	return stringLiteral(expr)
}

func stringLiteral(expr ast.Expr) (string, error) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", xerrors.Wrapf(errEndpointsShape, "文字列のリテラルではありません")
	}
	value, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", xerrors.Wrapf(errEndpointsShape, "%v", err)
	}

	return value, nil
}

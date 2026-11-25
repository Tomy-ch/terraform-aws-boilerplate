package main

import (
	"fmt"
	"path"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// errRegionSet は `region = each.key` のブロックの for_each を、region の集合として解けないときのエラー。
// 解ける形は閉じた一覧（scripts/README.md の egress/ の行）で、ほかの形と、空の集合、region の形でない要素を拒む。
var errRegionSet = xerrors.New("ブロックの for_each を region の集合として解けません")

// moduleCall は module ブロックの source と、呼ばれる側の var へ渡す引数。
type moduleCall struct {
	source string
	args   map[string]hclsyntax.Expression
}

// moduleInfo は 1 ディレクトリの locals・output・module の呼び出し。
type moduleInfo struct {
	dir     string
	locals  map[string]hclsyntax.Expression
	outputs map[string]hclsyntax.Expression
	calls   map[string]*moduleCall
}

// scope は module の 1 つの呼び出し。var は caller の文脈で args を解く。root は caller を持たない。
type scope struct {
	mod    *moduleInfo
	args   map[string]hclsyntax.Expression
	caller *scope
}

// regionValue は評価の値。set は region の名前の集合、entries は object / map の key ごとの式（値は使うときに解く）。
type regionValue struct {
	set     map[string]bool
	entries map[string]hclsyntax.Expression
	scope   *scope
}

func (v regionValue) isMap() bool { return v.entries != nil }

// regionEvaluator は scope の中で for_each の式を解く。load は module の情報を、rootRegion は root の aws の provider の
// region（partition の添字を解くときだけ読む）を返す。
type regionEvaluator struct {
	load       func(dir string) (*moduleInfo, error)
	rootRegion func() (string, error)
	active     map[string]bool
}

// metaArgs は module ブロックの、var へ渡さない引数。
var metaArgs = map[string]bool{"source": true, "version": true, "providers": true, "count": true, "for_each": true, "depends_on": true}

// readModuleInfo は bodies から locals・output・module の呼び出しを集める。
func readModuleInfo(dir string, bodies []*hclsyntax.Body) (*moduleInfo, error) {
	m := &moduleInfo{dir: dir, locals: map[string]hclsyntax.Expression{}, outputs: map[string]hclsyntax.Expression{}, calls: map[string]*moduleCall{}}
	for _, body := range bodies {
		for _, b := range body.Blocks {
			switch {
			case b.Type == "locals":
				for name, attr := range b.Body.Attributes {
					m.locals[name] = attr.Expr
				}
			case b.Type == "output" && len(b.Labels) == 1:
				if attr, ok := b.Body.Attributes["value"]; ok {
					m.outputs[b.Labels[0]] = attr.Expr
				}
			case b.Type == "module" && len(b.Labels) == 1:
				source, err := moduleSource(b.Body)
				if err != nil {
					return nil, xerrors.Wrap(err, fmt.Sprintf("%d 行目: module %q", b.TypeRange.Start.Line, b.Labels[0]))
				}
				call := &moduleCall{source: source, args: map[string]hclsyntax.Expression{}}
				for name, attr := range b.Body.Attributes {
					if !metaArgs[name] {
						call.args[name] = attr.Expr
					}
				}
				m.calls[b.Labels[0]] = call
			}
		}
	}
	return m, nil
}

// isEachKey は式がちょうど `each.key` かを返す。
func isEachKey(expr hclsyntax.Expression) bool {
	t, ok := expr.(*hclsyntax.ScopeTraversalExpr)
	if !ok || len(t.Traversal) != 2 || t.Traversal.RootName() != "each" {
		return false
	}
	attr, ok := t.Traversal[1].(hcl.TraverseAttr)
	return ok && attr.Name == "key"
}

// forEachRegions はブロックの for_each を解き、置く region の集合を返す。map なら key、set なら要素を取る。
func (e *regionEvaluator) forEachRegions(b *hclsyntax.Block, sc *scope) ([]string, error) {
	attr, ok := b.Body.Attributes["for_each"]
	if !ok {
		return nil, xerrors.Wrap(errRegionSet, "for_each が無いブロックの each.key です")
	}
	top := attr.Expr
	for {
		p, ok := top.(*hclsyntax.ParenthesesExpr)
		if !ok {
			break
		}
		top = p.Expression
	}
	if _, ok := top.(*hclsyntax.TupleConsExpr); ok {
		return nil, xerrors.Wrap(errRegionSet, "for_each の tuple は toset で包んでください（Terraform は tuple を受けません）")
	}
	v, err := e.eval(attr.Expr, sc)
	if err != nil {
		return nil, err
	}
	keys := v.set
	if v.isMap() {
		keys = map[string]bool{}
		for k := range v.entries {
			keys[k] = true
		}
	}
	if len(keys) == 0 {
		return nil, xerrors.Wrap(errRegionSet, "空の集合です")
	}
	regions := sortedKeys(keys)
	for _, r := range regions {
		if !regionLabelRe.MatchString(r) {
			return nil, xerrors.Wrap(errRegionSet, fmt.Sprintf("%q は region の形ではありません", r))
		}
	}
	return regions, nil
}

// eval は閉じた形の一覧の式だけを解く（scripts/README.md の egress/ の行）。ほかの式は落とす。
func (e *regionEvaluator) eval(expr hclsyntax.Expression, sc *scope) (regionValue, error) {
	at := fmt.Sprintf("%s %d 行目", sc.mod.dir, expr.Range().Start.Line)
	switch x := expr.(type) {
	case *hclsyntax.ParenthesesExpr:
		return e.eval(x.Expression, sc)
	case *hclsyntax.FunctionCallExpr:
		if x.Name != "toset" || len(x.Args) != 1 || x.ExpandFinal {
			return regionValue{}, xerrors.Wrap(errRegionSet, at+": 関数 "+x.Name+" は解けません")
		}
		v, err := e.eval(x.Args[0], sc)
		if err != nil {
			return regionValue{}, err
		}
		if v.isMap() {
			return regionValue{}, xerrors.Wrap(errRegionSet, at+": toset の引数が map です")
		}
		return v, nil
	case *hclsyntax.TupleConsExpr:
		set := map[string]bool{}
		for _, item := range x.Exprs {
			s, ok := literalString(item)
			if !ok {
				return regionValue{}, xerrors.Wrap(errRegionSet, at+": 要素が文字列のリテラルではありません")
			}
			set[s] = true
		}
		return regionValue{set: set}, nil
	case *hclsyntax.ObjectConsExpr:
		entries := map[string]hclsyntax.Expression{}
		for _, item := range x.Items {
			k, ok := literalString(item.KeyExpr)
			if !ok {
				return regionValue{}, xerrors.Wrap(errRegionSet, at+": key がリテラルではありません")
			}
			entries[k] = item.ValueExpr
		}
		return regionValue{entries: entries, scope: sc}, nil
	case *hclsyntax.ScopeTraversalExpr:
		return e.traversal(x.Traversal, sc, at)
	case *hclsyntax.IndexExpr:
		coll, err := e.eval(x.Collection, sc)
		if err != nil {
			return regionValue{}, err
		}
		return e.index(coll, x.Key, at)
	}
	return regionValue{}, xerrors.Wrap(errRegionSet, fmt.Sprintf("%s: %T の式は解けません", at, expr))
}

// traversal は local・var・module の参照を解き、続く添字と属性を map の key として当てる。
func (e *regionEvaluator) traversal(t hcl.Traversal, sc *scope, at string) (regionValue, error) {
	var (
		v    regionValue
		err  error
		rest hcl.Traversal
	)
	name := func(i int) (string, bool) {
		if i >= len(t) {
			return "", false
		}
		a, ok := t[i].(hcl.TraverseAttr)
		return a.Name, ok
	}
	switch t.RootName() {
	case "local":
		n, ok := name(1)
		if !ok {
			return regionValue{}, xerrors.Wrap(errRegionSet, at+": local の名前を読めません")
		}
		v, err = e.local(sc, n, at)
		rest = t[2:]
	case "var":
		n, ok := name(1)
		if !ok {
			return regionValue{}, xerrors.Wrap(errRegionSet, at+": var の名前を読めません")
		}
		v, err = e.variable(sc, n, at)
		rest = t[2:]
	case "module":
		call, ok1 := name(1)
		out, ok2 := name(2)
		if !ok1 || !ok2 {
			return regionValue{}, xerrors.Wrap(errRegionSet, at+": module の出力の参照を読めません")
		}
		v, err = e.output(sc, call, out, at)
		rest = t[3:]
	default:
		return regionValue{}, xerrors.Wrap(errRegionSet, at+": "+t.RootName()+" の参照は解けません")
	}
	if err != nil {
		return regionValue{}, err
	}
	for _, step := range rest {
		var key string
		switch s := step.(type) {
		case hcl.TraverseAttr:
			key = s.Name
		case hcl.TraverseIndex:
			if s.Key.Type() != cty.String {
				return regionValue{}, xerrors.Wrap(errRegionSet, at+": 添字が文字列ではありません")
			}
			key = s.Key.AsString()
		default:
			return regionValue{}, xerrors.Wrap(errRegionSet, at+": 参照の形を読めません")
		}
		if v, err = e.entry(v, key, at); err != nil {
			return regionValue{}, err
		}
	}
	return v, nil
}

func (e *regionEvaluator) local(sc *scope, n, at string) (regionValue, error) {
	expr, ok := sc.mod.locals[n]
	if !ok {
		return regionValue{}, xerrors.Wrap(errRegionSet, at+": local."+n+" がありません")
	}
	key := fmt.Sprintf("%p local.%s", sc, n)
	if e.active[key] {
		return regionValue{}, xerrors.Wrap(errRegionSet, at+": local."+n+" が循環しています")
	}
	e.active[key] = true
	defer delete(e.active, key)
	return e.eval(expr, sc)
}

func (e *regionEvaluator) variable(sc *scope, n, at string) (regionValue, error) {
	if sc.caller == nil {
		return regionValue{}, xerrors.Wrap(errRegionSet, at+": root の var."+n+" は解けません")
	}
	expr, ok := sc.args[n]
	if !ok {
		return regionValue{}, xerrors.Wrap(errRegionSet, at+": 呼び手が var."+n+" を渡していません（既定値は読みません）")
	}
	return e.eval(expr, sc.caller)
}

func (e *regionEvaluator) output(sc *scope, callName, out, at string) (regionValue, error) {
	call, ok := sc.mod.calls[callName]
	if !ok {
		return regionValue{}, xerrors.Wrap(errRegionSet, at+": module."+callName+" がありません")
	}
	child, err := e.load(joinModule(sc.mod.dir, call.source))
	if err != nil {
		return regionValue{}, err
	}
	expr, ok := child.outputs[out]
	if !ok {
		return regionValue{}, xerrors.Wrap(errRegionSet, at+": module."+callName+"."+out+" がありません")
	}
	// 同じ output を評価の途中でもう一度求めるのは、module の呼び出しが循環するときだけである。
	key := child.dir + " output." + out
	if e.active[key] {
		return regionValue{}, xerrors.Wrap(errRegionSet, at+": module."+callName+"."+out+" が循環しています")
	}
	e.active[key] = true
	defer delete(e.active, key)
	return e.eval(expr, &scope{mod: child, args: call.args, caller: sc})
}

// index は map の、式の添字を解く。添字は data.aws_partition.<name>.partition に限る。partition の添字は、値の集合のうち root の aws の provider の region を含む entry がちょうど 1 つのとき、その entry を選ぶ。
func (e *regionEvaluator) index(coll regionValue, key hclsyntax.Expression, at string) (regionValue, error) {
	if !coll.isMap() {
		return regionValue{}, xerrors.Wrap(errRegionSet, at+": 添字を取る値が map ではありません")
	}
	// 文字列のリテラルの添字は、HCL が参照の TraverseIndex に畳むので traversal が解く。
	if !isPartitionRef(key) {
		return regionValue{}, xerrors.Wrap(errRegionSet, at+": 添字が data.aws_partition の partition ではありません")
	}
	region, err := e.rootRegion()
	if err != nil {
		return regionValue{}, err
	}
	var picked []string
	var value regionValue
	for _, k := range sortedKeys(coll.entries) {
		v, err := e.entry(coll, k, at)
		if err != nil {
			return regionValue{}, err
		}
		if !v.isMap() && v.set[region] {
			picked = append(picked, k)
			value = v
		}
	}
	if len(picked) != 1 {
		return regionValue{}, xerrors.Wrap(errRegionSet, fmt.Sprintf("%s: provider の region %s を含む partition の entry が %d 個です（%s）", at, region, len(picked), strings.Join(picked, ", ")))
	}
	return value, nil
}

func (e *regionEvaluator) entry(v regionValue, key, at string) (regionValue, error) {
	if !v.isMap() {
		return regionValue{}, xerrors.Wrap(errRegionSet, at+": "+key+" を取る値が map ではありません")
	}
	expr, ok := v.entries[key]
	if !ok {
		return regionValue{}, xerrors.Wrap(errRegionSet, at+": key "+key+" がありません")
	}
	return e.eval(expr, v.scope)
}

func isPartitionRef(expr hclsyntax.Expression) bool {
	t, ok := expr.(*hclsyntax.ScopeTraversalExpr)
	if !ok || len(t.Traversal) != 4 || t.Traversal.RootName() != "data" {
		return false
	}
	names := make([]string, 0, 3)
	for _, step := range t.Traversal[1:] {
		a, ok := step.(hcl.TraverseAttr)
		if !ok {
			return false
		}
		names = append(names, a.Name)
	}
	return names[0] == "aws_partition" && names[2] == "partition"
}

// literalString は式が変数を参照しない文字列の値なら、その値を返す。
func literalString(expr hclsyntax.Expression) (string, bool) {
	if k, ok := expr.(*hclsyntax.ObjectConsKeyExpr); ok {
		if t, ok := k.Wrapped.(*hclsyntax.ScopeTraversalExpr); ok && !k.ForceNonLiteral && len(t.Traversal) == 1 {
			return t.Traversal.RootName(), true
		}
		expr = k.Wrapped
	}
	if len(expr.Variables()) > 0 {
		return "", false
	}
	value, diags := expr.Value(nil)
	if diags.HasErrors() || !value.IsKnown() || value.IsNull() || value.Type() != cty.String {
		return "", false
	}
	return value.AsString(), true
}

// joinModule は呼び手のディレクトリと source から、呼ばれる module のディレクトリを返す。
func joinModule(dir, source string) string {
	return path.Join(dir, source)
}

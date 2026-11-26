package main

import (
	"regexp"
	"slices"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// errEnvName は、-env が、ユースケースの variable "env" の validation が許す env でないこと。
var errEnvName = xerrors.New("env が、ユースケースの宣言が許す env ではありません")

// variableBlock は、body の variable name がただ1つであることを求めて返す。
func variableBlock(body *hclsyntax.Body, name string) (*hclsyntax.Block, error) {
	var found []*hclsyntax.Block

	for _, b := range body.Blocks {
		if b.Type == "variable" && len(b.Labels) == 1 && b.Labels[0] == name {
			found = append(found, b)
		}
	}

	if len(found) != 1 {
		return nil, xerrors.Wrapf(errDeclShape, "variable %s が %d 件", name, len(found))
	}

	return found[0], nil
}

// regexCall は、regex(pattern, subject) の呼び出しの1つ。
type regexCall struct {
	pattern *regexp.Regexp
	subject hclsyntax.Expression
}

// regexCalls は、node の中の regex の呼び出しのすべて。正規表現がリテラルでないか、Go の正規表現として読めなければ
// errDeclShape（読めない呼び出しを数えずに落とさない）。
func regexCalls(node hclsyntax.Node) ([]regexCall, error) {
	var found []*hclsyntax.FunctionCallExpr

	_ = hclsyntax.VisitAll(node, func(n hclsyntax.Node) hcl.Diagnostics {
		if call, ok := n.(*hclsyntax.FunctionCallExpr); ok && call.Name == "regex" {
			found = append(found, call)
		}

		return nil
	})

	out := make([]regexCall, 0, len(found))

	for _, call := range found {
		if len(call.Args) != 2 {
			return nil, xerrors.Wrap(errDeclShape, "regex の引数が2つではありません")
		}

		v, d := call.Args[0].Value(nil)
		if d.HasErrors() || v.Type() != cty.String || !v.IsKnown() || v.IsNull() {
			return nil, xerrors.Wrap(errDeclShape, "regex の正規表現がリテラルではありません")
		}

		re, err := regexp.Compile(v.AsString())
		if err != nil {
			return nil, xerrors.Join(errDeclShape, err)
		}

		out = append(out, regexCall{pattern: re, subject: call.Args[1]})
	}

	return out, nil
}

// literalListArg は、node の中のただ1つの fn の呼び出しの、index 番目の引数のリテラルの文字列のリスト。
func literalListArg(node hclsyntax.Node, fn string, index int) ([]string, error) {
	var calls []*hclsyntax.FunctionCallExpr

	_ = hclsyntax.VisitAll(node, func(n hclsyntax.Node) hcl.Diagnostics {
		if call, ok := n.(*hclsyntax.FunctionCallExpr); ok && call.Name == fn {
			calls = append(calls, call)
		}

		return nil
	})

	if len(calls) != 1 || len(calls[0].Args) <= index {
		return nil, xerrors.Wrapf(errDeclShape, "%s の呼び出しが %d 件", fn, len(calls))
	}

	v, d := calls[0].Args[index].Value(nil)
	if d.HasErrors() || !v.IsWhollyKnown() || v.IsNull() || !v.Type().IsTupleType() || v.LengthInt() == 0 {
		return nil, xerrors.Wrap(errDeclShape, fn+" の引数が、空でないリテラルのリストではありません")
	}

	var out []string

	for it := v.ElementIterator(); it.Next(); {
		_, e := it.Element()
		if e.Type() != cty.String || e.AsString() == "" || slices.Contains(out, e.AsString()) {
			return nil, xerrors.Wrap(errDeclShape, fn+" の引数が、重複の無い空でない文字列のリストではありません")
		}

		out = append(out, e.AsString())
	}

	return out, nil
}

// allowedEnvs は、variables の variable "env" の validation の contains([...], var.env) が許す env の一覧。
func allowedEnvs(variables *hclsyntax.Body) ([]string, error) {
	b, err := variableBlock(variables, "env")
	if err != nil {
		return nil, err
	}

	return literalListArg(b.Body, "contains", 0)
}

// requireEnv は、env が variables の宣言が許す env であることを求める。
func requireEnv(variables *hclsyntax.Body, env string) error {
	envs, err := allowedEnvs(variables)
	if err != nil {
		return err
	}

	if !slices.Contains(envs, env) {
		return xerrors.Wrapf(errEnvName, "%s（宣言: %s）", env, joinOrNone(envs))
	}

	return nil
}

// evalLocals は、body の locals の names を、var に vars を入れて、その順に評価する。後の local は前の local を
// local.<name> で参照してよい。どれも空でない文字列でなければ errDeclShape。評価に使った文脈も返す。
func evalLocals(body *hclsyntax.Body, names []string, vars map[string]string) (map[string]string, *hcl.EvalContext, error) {
	varVals := map[string]cty.Value{}
	for k, v := range vars {
		varVals[k] = cty.StringVal(v)
	}

	locals := map[string]cty.Value{}
	out := map[string]string{}

	ctx := func() *hcl.EvalContext {
		return &hcl.EvalContext{Variables: map[string]cty.Value{"var": cty.ObjectVal(varVals), "local": cty.ObjectVal(locals)}}
	}

	for _, name := range names {
		attr, err := singleLocal(body, name)
		if err != nil {
			return nil, nil, err
		}

		v, d := attr.Expr.Value(ctx())
		if d.HasErrors() || v.Type() != cty.String || !v.IsKnown() || v.IsNull() || v.AsString() == "" {
			return nil, nil, xerrors.Wrap(errDeclShape, "locals の "+name+" が、var と前の local だけを使う文字列の式ではありません")
		}

		locals[name] = v
		out[name] = v.AsString()
	}

	return out, ctx(), nil
}

// localItemString は、body の locals の local（object の式）の key の項目を ctx で評価した、空でない文字列。
func localItemString(body *hclsyntax.Body, local, key string, ctx *hcl.EvalContext) (string, error) {
	attr, err := singleLocal(body, local)
	if err != nil {
		return "", err
	}

	obj, ok := attr.Expr.(*hclsyntax.ObjectConsExpr)
	if !ok {
		return "", xerrors.Wrap(errDeclShape, "locals の "+local+" が object の式ではありません")
	}

	var found []hclsyntax.ObjectConsItem

	for _, item := range obj.Items {
		k, d := item.KeyExpr.Value(nil)
		if !d.HasErrors() && k.Type() == cty.String && k.IsKnown() && !k.IsNull() && k.AsString() == key {
			found = append(found, item)
		}
	}

	if len(found) != 1 {
		return "", xerrors.Wrapf(errDeclShape, "locals の %s の %s が %d 件", local, key, len(found))
	}

	v, d := found[0].ValueExpr.Value(ctx)
	if d.HasErrors() || v.Type() != cty.String || !v.IsKnown() || v.IsNull() || v.AsString() == "" {
		return "", xerrors.Wrap(errDeclShape, "locals の "+local+" の "+key+" が、var と local だけを使う文字列の式ではありません")
	}

	return v.AsString(), nil
}

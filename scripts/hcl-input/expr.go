package main

import (
	"encoding/json"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// 式の種類。どの式がどの規則に当たるかは Rego が持ち、ここは構文の事実だけを写します。
const (
	exprLiteral   = "literal"
	exprTemplate  = "template"
	exprCall      = "call"
	exprTraversal = "traversal"
	exprTuple     = "tuple"
	exprObject    = "object"
	exprOther     = "other"
)

// expr は式の構文の木です。other は子を持たず、中の参照は属性の refs が持ちます。
type expr struct {
	Kind   string     `json:"kind"`
	Value  any        `json:"value,omitempty"`
	Parts  []expr     `json:"parts,omitempty"`
	Name   string     `json:"name,omitempty"`
	Args   []expr     `json:"args,omitempty"`
	Expand *bool      `json:"expand,omitempty"`
	Root   string     `json:"root,omitempty"`
	Steps  []step     `json:"steps,omitempty"`
	Items  []exprItem `json:"items,omitempty"`
	Syntax string     `json:"syntax,omitempty"`
}

type exprItem struct {
	Key   *expr `json:"key,omitempty"`
	Value expr  `json:"value"`
}

// MarshalJSON は、literal の value を null でも、call の args と tuple・object の items を空でも出します。
// omitempty は種類を問わず空を落とすので、種類ごとに要るキーをここで固定します。
func (e expr) MarshalJSON() ([]byte, error) {
	m := map[string]any{"kind": e.Kind}
	switch e.Kind {
	case exprLiteral:
		m["value"] = e.Value
	case exprTemplate:
		m["parts"] = nonNil(e.Parts)
	case exprCall:
		m["name"] = e.Name
		m["args"] = nonNil(e.Args)
		m["expand"] = e.Expand != nil && *e.Expand
	case exprTraversal:
		m["root"] = e.Root
		m["steps"] = append([]step{}, e.Steps...)
	case exprTuple:
		items := make([]expr, 0, len(e.Items))
		for _, it := range e.Items {
			items = append(items, it.Value)
		}
		m["items"] = items
	case exprObject:
		m["items"] = append([]exprItem{}, e.Items...)
	case exprOther:
		m["syntax"] = e.Syntax
	}

	return json.Marshal(m)
}

func nonNil(es []expr) []expr { return append([]expr{}, es...) }

// exprOf は e を構文の木に写します。参照も関数の呼び出しも含まず値の決まる式は literal にし、括弧は外します。
// 構文の誤りの式と、歩く型の外の式は errUnknownExpression です。
func exprOf(e hclsyntax.Expression) (expr, error) {
	if v, ok := isLiteral(e); ok {
		if j, err := jsonOf(v); err == nil {
			return expr{Kind: exprLiteral, Value: j}, nil
		}
	}
	switch e := e.(type) {
	case *hclsyntax.ParenthesesExpr:
		return exprOf(e.Expression)
	case *hclsyntax.TemplateWrapExpr:
		part, err := exprOf(e.Wrapped)
		if err != nil {
			return expr{}, err
		}

		return expr{Kind: exprTemplate, Parts: []expr{part}}, nil
	case *hclsyntax.TemplateExpr:
		parts, err := exprsOf(e.Parts)

		return expr{Kind: exprTemplate, Parts: parts}, err
	case *hclsyntax.FunctionCallExpr:
		args, err := exprsOf(e.Args)
		expand := e.ExpandFinal

		return expr{Kind: exprCall, Name: e.Name, Args: args, Expand: &expand}, err
	case *hclsyntax.ScopeTraversalExpr:
		steps, err := stepsOf(e.Traversal[1:])
		if err != nil {
			return expr{}, xerrors.Wrapf(err, "%s", e.Range())
		}

		return expr{Kind: exprTraversal, Root: e.Traversal.RootName(), Steps: steps}, nil
	case *hclsyntax.TupleConsExpr:
		items := make([]exprItem, 0, len(e.Exprs))
		for _, x := range e.Exprs {
			v, err := exprOf(x)
			if err != nil {
				return expr{}, err
			}
			items = append(items, exprItem{Value: v})
		}

		return expr{Kind: exprTuple, Items: items}, nil
	case *hclsyntax.ObjectConsExpr:
		items := make([]exprItem, 0, len(e.Items))
		for _, it := range e.Items {
			k, err := exprOf(it.KeyExpr)
			if err != nil {
				return expr{}, err
			}
			v, err := exprOf(it.ValueExpr)
			if err != nil {
				return expr{}, err
			}
			items = append(items, exprItem{Key: &k, Value: v})
		}

		return expr{Kind: exprObject, Items: items}, nil
	case *hclsyntax.ObjectConsKeyExpr:
		// 括弧の無い名前のキーは参照を持たないので、上の literal で返る。ここに来るのは参照を持つキーである。
		return exprOf(e.Wrapped)
	}
	if name := syntaxName(e); name != "" {
		return expr{Kind: exprOther, Syntax: name}, nil
	}

	return expr{}, xerrors.Wrapf(errUnknownExpression, "%s: %T", e.Range(), e)
}

func exprsOf(es []hclsyntax.Expression) ([]expr, error) {
	out := make([]expr, 0, len(es))
	for _, e := range es {
		v, err := exprOf(e)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}

	return out, nil
}

// isLiteral は、e が参照も関数の呼び出しも含まず、診断なしに値を返すとき、その値を返します。
func isLiteral(e hclsyntax.Expression) (cty.Value, bool) {
	if len(hclsyntax.Variables(e)) > 0 {
		return cty.NilVal, false
	}
	calls := false
	_ = hclsyntax.VisitAll(e, func(n hclsyntax.Node) hcl.Diagnostics {
		if _, ok := n.(*hclsyntax.FunctionCallExpr); ok {
			calls = true
		}
		// 構文の誤りの式は Value が診断なしに値を返すので、ここで literal から外す（Test_exprOf の構文の誤りのケース）。
		if _, ok := n.(*hclsyntax.ExprSyntaxError); ok {
			calls = true
		}

		return nil
	})
	if calls {
		return cty.NilVal, false
	}
	v, diags := e.Value(nil)
	if len(diags) > 0 || !v.IsWhollyKnown() {
		return cty.NilVal, false
	}

	return v, true
}

// jsonOf は値を JSON の値（nil・string・bool・json.Number・[]any・map[string]any）に写します。
func jsonOf(v cty.Value) (any, error) {
	if !v.IsWhollyKnown() {
		return nil, xerrors.Wrapf(errUnknownExpression, "未知の値")
	}
	if v.IsNull() {
		return nil, nil
	}
	t := v.Type()
	switch {
	case t == cty.String:
		return v.AsString(), nil
	case t == cty.Bool:
		return v.True(), nil
	case t == cty.Number:
		f := v.AsBigFloat()
		if f.IsInf() {
			return nil, xerrors.Wrapf(errUnknownExpression, "JSON に書けない数 %s", f.String())
		}

		return json.Number(f.Text('g', -1)), nil
	case t.IsListType() || t.IsTupleType() || t.IsSetType():
		out := []any{}
		for it := v.ElementIterator(); it.Next(); {
			_, ev := it.Element()
			j, err := jsonOf(ev)
			if err != nil {
				return nil, err
			}
			out = append(out, j)
		}

		return out, nil
	case t.IsMapType() || t.IsObjectType():
		out := map[string]any{}
		for it := v.ElementIterator(); it.Next(); {
			k, ev := it.Element()
			j, err := jsonOf(ev)
			if err != nil {
				return nil, err
			}
			out[k.AsString()] = j
		}

		return out, nil
	}

	return nil, xerrors.Wrapf(errUnknownExpression, "値の型 %s", t.FriendlyName())
}

// syntaxName は、other に写す式の種類の名前を返します。歩く型の外なら空です。
func syntaxName(e hclsyntax.Expression) string {
	switch e.(type) {
	case *hclsyntax.ForExpr:
		return "for"
	case *hclsyntax.SplatExpr:
		return "splat"
	case *hclsyntax.IndexExpr:
		return "index"
	case *hclsyntax.ConditionalExpr:
		return "conditional"
	case *hclsyntax.BinaryOpExpr:
		return "binary"
	case *hclsyntax.UnaryOpExpr:
		return "unary"
	case *hclsyntax.RelativeTraversalExpr:
		return "relative_traversal"
	case *hclsyntax.TemplateJoinExpr:
		return "template_join"
	}

	return ""
}

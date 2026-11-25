package main

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// 段の種類。`[*]` と `.*` は区別しません。添字の段は、添字の式の形を Index に持ちます。
const (
	stepAttr  = "attr"
	stepIndex = "index"
	stepSplat = "splat"
)

// 添字の式の形。each_key・each_value・count_index は、添字がちょうどその参照（段が続かない）のとき、
// literal は数か文字列のリテラル（補間の無いテンプレートと、traversal の静的な添字を含む）のときで、
// ほかはすべて other です。
const (
	indexEachKey    = "each_key"
	indexEachValue  = "each_value"
	indexCountIndex = "count_index"
	indexLiteral    = "literal"
	indexOther      = "other"
)

// ref は、式の中の1つの参照です。Within は、この参照が添字の中に在るとき、その添字を持つ参照
// （配列の位置）と段（Steps の位置）を、外側から祖先すべて持ちます。Rego が再帰なしで
// 「どの添字の中か」を1段で判定できるようにするためです。Filter は、この参照が for の条件（if）の
// 中に在るかです。条件は要素を絞るだけで、要素の値を作りません。
type ref struct {
	Root   string   `json:"root"`
	Steps  []step   `json:"steps"`
	Within []within `json:"within"`
	Filter bool     `json:"filter"`
}

type step struct {
	Kind  string     `json:"kind"`
	Name  string     `json:"name,omitempty"`
	Index *indexForm `json:"index,omitempty"`
}

// indexForm は、添字の段の式の形です。Value は Form が literal のときだけ持ち、数は json.Number、
// 文字列は string です。
type indexForm struct {
	Form  string `json:"form"`
	Value any    `json:"value,omitempty"`
}

type within struct {
	Ref  int `json:"ref"`
	Step int `json:"step"`
}

// walker は、式を歩いて参照を集めます。ranges は refs と同じ位置の参照の traversal の範囲で、
// Variables() との突き合わせに使います。
type walker struct {
	refs   []ref
	ranges []hcl.Range
}

// scope は、歩いている位置の文脈です。within は外側の添字、locals は for の反復変数の名前、
// filter は for の条件の中かです。
type scope struct {
	within []within
	locals map[string]bool
	filter bool
}

// walkExpr は、e の参照をソースの順に返します。参照の集合は hclsyntax の Variables() と突き合わせ、
// 食い違えば errRefsMismatch です。
func walkExpr(e hclsyntax.Expression) ([]ref, error) {
	w := &walker{refs: []ref{}}
	if _, err := w.walk(e, scope{}); err != nil {
		return nil, err
	}
	if err := checkRefs(e, w.ranges); err != nil {
		return nil, err
	}

	return w.refs, nil
}

// walk は e を歩き、e が参照の鎖（参照と、それに続く属性・添字・splat）なら、その参照の位置を返します。
// 鎖でなければ -1 です。関数の呼び出し・括弧・タプルの後の段は参照に付かず、その添字の中の参照は
// 外側の within のままです。splat の後の段は Each が持つので、splat は鎖を終えます。
// 歩く型の外の式と ExprSyntaxError は errUnknownExpression です。
func (w *walker) walk(e hclsyntax.Expression, sc scope) (int, error) {
	switch e := e.(type) {
	case *hclsyntax.ScopeTraversalExpr:
		if sc.locals[e.Traversal.RootName()] {
			return -1, nil
		}
		steps, err := stepsOf(e.Traversal[1:])
		if err != nil {
			return -1, xerrors.Wrapf(err, "%s", e.Range())
		}
		w.refs = append(w.refs, ref{Root: e.Traversal.RootName(), Steps: steps, Within: append([]within{}, sc.within...), Filter: sc.filter})
		w.ranges = append(w.ranges, e.Traversal.SourceRange())

		return len(w.refs) - 1, nil
	case *hclsyntax.RelativeTraversalExpr:
		idx, err := w.walk(e.Source, sc)
		if err != nil {
			return -1, err
		}
		steps, err := stepsOf(e.Traversal)
		if err != nil {
			return -1, xerrors.Wrapf(err, "%s", e.Range())
		}
		if idx >= 0 {
			w.refs[idx].Steps = append(w.refs[idx].Steps, steps...)
		}

		return idx, nil
	case *hclsyntax.IndexExpr:
		idx, err := w.walk(e.Collection, sc)
		if err != nil {
			return -1, err
		}
		inner := sc
		if idx >= 0 {
			w.refs[idx].Steps = append(w.refs[idx].Steps, step{Kind: stepIndex, Index: indexFormOf(e.Key, sc.locals)})
			inner.within = append(slices.Clone(sc.within), within{Ref: idx, Step: len(w.refs[idx].Steps) - 1})
		}

		return idx, w.each(inner, e.Key)
	case *hclsyntax.SplatExpr:
		idx, err := w.walk(e.Source, sc)
		if err != nil {
			return -1, err
		}
		if idx >= 0 {
			w.refs[idx].Steps = append(w.refs[idx].Steps, step{Kind: stepSplat})
		}

		return -1, w.each(sc, e.Each)
	case *hclsyntax.ForExpr:
		if err := w.each(sc, e.CollExpr); err != nil {
			return -1, err
		}
		inner := scope{within: sc.within, locals: map[string]bool{}, filter: sc.filter}
		for name := range sc.locals {
			inner.locals[name] = true
		}
		for _, name := range []string{e.KeyVar, e.ValVar} {
			if name != "" {
				inner.locals[name] = true
			}
		}
		if err := w.each(inner, e.KeyExpr, e.ValExpr); err != nil {
			return -1, err
		}
		cond := inner
		cond.filter = true

		return -1, w.each(cond, e.CondExpr)
	case *hclsyntax.ObjectConsKeyExpr:
		if !e.ForceNonLiteral && hcl.ExprAsKeyword(e.Wrapped) != "" {
			return -1, nil
		}

		return -1, w.each(sc, e.Wrapped)
	case *hclsyntax.ObjectConsExpr:
		exprs := make([]hclsyntax.Expression, 0, 2*len(e.Items))
		for _, item := range e.Items {
			exprs = append(exprs, item.KeyExpr, item.ValueExpr)
		}

		return -1, w.each(sc, exprs...)
	case *hclsyntax.FunctionCallExpr:
		return -1, w.each(sc, e.Args...)
	case *hclsyntax.TupleConsExpr:
		return -1, w.each(sc, e.Exprs...)
	case *hclsyntax.TemplateExpr:
		return -1, w.each(sc, e.Parts...)
	case *hclsyntax.ConditionalExpr:
		return -1, w.each(sc, e.Condition, e.TrueResult, e.FalseResult)
	case *hclsyntax.BinaryOpExpr:
		return -1, w.each(sc, e.LHS, e.RHS)
	case *hclsyntax.UnaryOpExpr:
		return -1, w.each(sc, e.Val)
	case *hclsyntax.TemplateWrapExpr:
		return -1, w.each(sc, e.Wrapped)
	case *hclsyntax.TemplateJoinExpr:
		return -1, w.each(sc, e.Tuple)
	case *hclsyntax.ParenthesesExpr:
		return -1, w.each(sc, e.Expression)
	case *hclsyntax.LiteralValueExpr, *hclsyntax.AnonSymbolExpr:
		return -1, nil
	case *hclsyntax.ExprSyntaxError:
		return -1, xerrors.Wrapf(errUnknownExpression, "%s: 構文の誤りの式", e.Range())
	default:
		return -1, xerrors.Wrapf(errUnknownExpression, "%s: %T", e.Range(), e)
	}
}

// each は、exprs を順に、鎖の外の式として歩きます。nil の要素（for のキーや条件の欠け）は飛ばします。
func (w *walker) each(sc scope, exprs ...hclsyntax.Expression) error {
	for _, e := range exprs {
		if e == nil {
			continue
		}
		if _, err := w.walk(e, sc); err != nil {
			return err
		}
	}

	return nil
}

// stepsOf は、traversal の段を段の3種類へ写します。それ以外の段（途中の根）は errUnknownExpression です。
func stepsOf(trav hcl.Traversal) ([]step, error) {
	steps := make([]step, 0, len(trav))
	for _, t := range trav {
		switch t := t.(type) {
		case hcl.TraverseAttr:
			steps = append(steps, step{Kind: stepAttr, Name: t.Name})
		case hcl.TraverseIndex:
			steps = append(steps, step{Kind: stepIndex, Index: literalForm(t.Key)})
		case hcl.TraverseSplat:
			steps = append(steps, step{Kind: stepSplat})
		default:
			return nil, xerrors.Wrapf(errUnknownExpression, "traversal の段 %T", t)
		}
	}

	return steps, nil
}

// indexFormOf は、添字の式 e の形を返します。locals は for の反復変数の名前で、each か count を覆うなら other です。
func indexFormOf(e hclsyntax.Expression, locals map[string]bool) *indexForm {
	switch e := e.(type) {
	case *hclsyntax.ScopeTraversalExpr:
		if len(e.Traversal) != 2 || locals[e.Traversal.RootName()] {
			return &indexForm{Form: indexOther}
		}
		attr, ok := e.Traversal[1].(hcl.TraverseAttr)
		if !ok {
			return &indexForm{Form: indexOther}
		}
		switch [2]string{e.Traversal.RootName(), attr.Name} {
		case [2]string{"each", "key"}:
			return &indexForm{Form: indexEachKey}
		case [2]string{"each", "value"}:
			return &indexForm{Form: indexEachValue}
		case [2]string{"count", "index"}:
			return &indexForm{Form: indexCountIndex}
		}
	case *hclsyntax.LiteralValueExpr:
		return literalForm(e.Val)
	case *hclsyntax.TemplateExpr:
		if e.IsStringLiteral() {
			v, diags := e.Value(nil)
			if !diags.HasErrors() {
				return literalForm(v)
			}
		}
	}

	return &indexForm{Form: indexOther}
}

// literalForm は、既知の数か文字列の v を literal として返します。それ以外の値は other です。
func literalForm(v cty.Value) *indexForm {
	if v.IsNull() || !v.IsKnown() {
		return &indexForm{Form: indexOther}
	}
	switch v.Type() {
	case cty.String:
		return &indexForm{Form: indexLiteral, Value: v.AsString()}
	case cty.Number:
		return &indexForm{Form: indexLiteral, Value: json.Number(v.AsBigFloat().Text('f', -1))}
	}

	return &indexForm{Form: indexOther}
}

// checkRefs は、got（自前の歩きが参照とした traversal の範囲）が、hclsyntax.Variables(e) の traversal の
// 範囲と多重集合として一致するかを確かめます。for のスコープとオブジェクトのキーの扱いを、hclsyntax 自身の
// 実装を独立の経路として確かめるためです（ADR-0702 決定15）。
func checkRefs(e hclsyntax.Expression, got []hcl.Range) error {
	count := map[hcl.Range]int{}
	for _, r := range got {
		count[r]++
	}
	for _, t := range hclsyntax.Variables(e) {
		count[t.SourceRange()]--
	}
	var diffs []string
	for r, n := range count {
		if n != 0 {
			diffs = append(diffs, fmt.Sprintf("%s（自前の歩きの数 - Variables() の数 = %d）", r, n))
		}
	}
	if len(diffs) == 0 {
		return nil
	}
	slices.Sort(diffs)

	return xerrors.Wrapf(errRefsMismatch, "%v", diffs)
}

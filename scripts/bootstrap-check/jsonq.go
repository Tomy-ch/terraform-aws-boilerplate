package main

import (
	"encoding/json"
	"math"
	"reflect"
	"slices"
	"unicode/utf8"
)

// ここの関数は、aws の JSON の応答を判定する。null をたどれば null、object でも null でもない値をたどればエラー
// （ok が false）、エラーは判定を偽にする。

// get は、keys を順にたどった値。
func get(v any, keys ...string) (any, bool) {
	for _, k := range keys {
		switch m := v.(type) {
		case nil:
		case map[string]any:
			v = m[k]
		default:
			return nil, false
		}
	}

	return v, true
}

// items は、v が配列ならその要素。配列でなければ false（object を回す形は判定の対象に現れない）。
func items(v any, ok bool) ([]any, bool) {
	a, isArray := v.([]any)

	return a, ok && isArray
}

// arrOf は、配列ならそのまま、それ以外は1要素の配列にしたもの。
func arrOf(v any) []any {
	if a, ok := v.([]any); ok {
		return a
	}

	return []any{v}
}

// strs は、要素がすべて文字列なら、その並び。
func strs(a []any) ([]string, bool) {
	out := make([]string, 0, len(a))

	for _, e := range a {
		s, ok := e.(string)
		if !ok {
			return nil, false
		}

		out = append(out, s)
	}

	return out, true
}

// truthy は、false と null だけが偽。
func truthy(v any) bool {
	return v != nil && v != false
}

// jsonEqual は、v が JSON の文字列 literal の値と等しいこと。
func jsonEqual(v any, literal string) bool {
	var want any
	if err := json.Unmarshal([]byte(literal), &want); err != nil {
		return false
	}

	return reflect.DeepEqual(v, want)
}

// anyString は、v の中の文字列（object の key は含まない）のどれかが f を満たすこと。
func anyString(v any, f func(string) bool) bool {
	switch x := v.(type) {
	case string:
		return f(x)
	case []any:
		return slices.ContainsFunc(x, func(e any) bool { return anyString(e, f) })
	case map[string]any:
		for _, e := range x {
			if anyString(e, f) {
				return true
			}
		}
	}

	return false
}

// jsonLength は、null は 0、配列・object は要素数、文字列は文字数、数は絶対値。真偽値はエラー（false）。
func jsonLength(v any) (float64, bool) {
	switch x := v.(type) {
	case nil:
		return 0, true
	case []any:
		return float64(len(x)), true
	case map[string]any:
		return float64(len(x)), true
	case string:
		return float64(utf8.RuneCountInString(x)), true
	case float64:
		return math.Abs(x), true
	}

	return 0, false
}

// cond は、1つの文（Statement の要素など）に対する比較1つ。返り値は（真偽、評価できたか）。
type cond func(s any) (bool, bool)

// fieldEq は、keys をたどった値が want に等しいこと。want は文字列・数・真偽値に限る。
func fieldEq(want any, keys ...string) cond {
	return func(s any) (bool, bool) {
		v, ok := get(s, keys...)

		return ok && v == want, ok
	}
}

// arrEq は、keys をたどった値を配列にして（配列でなければ1要素の配列）、want に等しいこと。sorted なら並べ替えて比べる。
func arrEq(want []string, sorted bool, keys ...string) cond {
	return func(s any) (bool, bool) {
		v, ok := get(s, keys...)
		if !ok {
			return false, false
		}

		got, isStrings := strs(arrOf(v))
		if !isStrings {
			return false, true
		}

		w := want
		if sorted {
			got = slices.Sorted(slices.Values(got))
			w = slices.Sorted(slices.Values(want))
		}

		return slices.Equal(got, w), true
	}
}

// allConds は、cs がすべて真であること。偽の比較の後ろは評価しない。
func allConds(s any, cs ...cond) (bool, bool) {
	for _, c := range cs {
		m, ok := c(s)
		if !ok {
			return false, false
		}

		if !m {
			return false, true
		}
	}

	return true, true
}

// anyStatement は、どれかの要素で cs がすべて真であること。どれかの要素で評価できなければ、判定ごと偽にする。
func anyStatement(stmts []any, cs ...cond) bool {
	found := false

	for _, s := range stmts {
		m, ok := allConds(s, cs...)
		if !ok {
			return false
		}

		found = found || m
	}

	return found
}

// everyStatement は、すべての要素で cs がすべて真であること。空の配列は真。
func everyStatement(stmts []any, cs ...cond) bool {
	for _, s := range stmts {
		if m, ok := allConds(s, cs...); !ok || !m {
			return false
		}
	}

	return true
}

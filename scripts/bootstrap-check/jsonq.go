package main

import (
	"encoding/json"
	"math"
	"reflect"
	"slices"
	"unicode/utf8"
)

// ここの関数は、aws の JSON の応答の判定に jq と同じ評価の規則を与える:
// null をたどれば null、object でも null でもない値をたどればエラー（ok が false）、エラーは判定を偽にする。

// get は jq の .k1.k2… の値。
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

// items は jq の .[] で回す配列。配列でなければ false（object を回す形は判定の対象に現れない）。
func items(v any, ok bool) ([]any, bool) {
	a, isArray := v.([]any)

	return a, ok && isArray
}

// arrOf は jq の def arr: if type == "array" then . else [.] end。
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

// truthy は jq の真偽。false と null だけが偽。
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

// anyString は、jq の .. | strings が出す文字列（object の key は含まない）のどれかが f を満たすこと。
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

// jqLength は jq の length。真偽値はエラー（false）。
func jqLength(v any) (float64, bool) {
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

// cond は、1つの文（Statement の要素など）に対する jq の比較1つ。返り値は（真偽、評価できたか）。
type cond func(s any) (bool, bool)

// fieldEq は jq の .k1.k2… == want。want は文字列・数・真偽値に限る。
func fieldEq(want any, keys ...string) cond {
	return func(s any) (bool, bool) {
		v, ok := get(s, keys...)

		return ok && v == want, ok
	}
}

// arrEq は jq の (.k1.k2… | arr [| sort]) == want。
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

// allConds は jq の c1 and c2 and …。偽の比較の後ろは評価しない。
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

// anyStatement は jq の any(c1 and c2 and …)。どれかの要素で評価できなければ、判定ごと偽にする。
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

// everyStatement は jq の all(c1 and c2 and …)。空の配列は真。
func everyStatement(stmts []any, cs ...cond) bool {
	for _, s := range stmts {
		if m, ok := allConds(s, cs...); !ok || !m {
			return false
		}
	}

	return true
}

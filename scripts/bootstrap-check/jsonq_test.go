package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func decode(t *testing.T, s string) any {
	t.Helper()

	var v any
	require.NoError(t, json.Unmarshal([]byte(s), &v))

	return v
}

func Test_get(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		in     string
		keys   []string
		want   any
		wantOK bool
	}{
		"object をたどる":           {`{"a":{"b":"x"}}`, []string{"a", "b"}, "x", true},
		"無い key は null":         {`{"a":{}}`, []string{"a", "b"}, nil, true},
		"null をたどれば null":       {`{"a":null}`, []string{"a", "b"}, nil, true},
		"文字列をたどればエラー":           {`{"a":"s"}`, []string{"a", "b"}, nil, false},
		"配列をたどればエラー":            {`{"a":[1]}`, []string{"a", "b"}, nil, false},
		"key が無ければ値そのもの":        {`"s"`, nil, "s", true},
		"根が null でもたどれる":        {`null`, []string{"a"}, nil, true},
		"数をたどればエラー":             {`1`, []string{"a"}, nil, false},
		"たどった先が false でもエラーでない": {`{"a":false}`, []string{"a"}, false, true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, ok := get(decode(t, tc.in), tc.keys...)

			assert.Equal(t, tc.wantOK, ok)
			assert.Equal(t, tc.want, got)
		})
	}
}

func Test_items(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		in     string
		ok     bool
		want   []any
		wantOK bool
	}{
		"配列":           {`[1,"a"]`, true, []any{float64(1), "a"}, true},
		"空の配列":         {`[]`, true, []any{}, true},
		"null は回せない":   {`null`, true, nil, false},
		"object は回さない": {`{"a":1}`, true, nil, false},
		"たどれなかった値":     {`[1]`, false, []any{float64(1)}, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, ok := items(decode(t, tc.in), tc.ok)

			assert.Equal(t, tc.wantOK, ok)

			if tc.wantOK {
				assert.Equal(t, tc.want, got)
			}
		})
	}
}

func Test_arrOf(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []any{"a", "b"}, arrOf([]any{"a", "b"}))
	assert.Equal(t, []any{"a"}, arrOf("a"))
	assert.Equal(t, []any{nil}, arrOf(nil), "null も1要素の配列にする")
	assert.Equal(t, []any{}, arrOf([]any{}))
}

func Test_strs(t *testing.T) {
	t.Parallel()

	got, ok := strs([]any{"a", "b"})
	require.True(t, ok)
	assert.Equal(t, []string{"a", "b"}, got)

	got, ok = strs([]any{})
	require.True(t, ok)
	assert.Empty(t, got)

	_, ok = strs([]any{"a", float64(1)})
	assert.False(t, ok)

	_, ok = strs([]any{nil})
	assert.False(t, ok)
}

func Test_truthy(t *testing.T) {
	t.Parallel()

	assert.False(t, truthy(nil))
	assert.False(t, truthy(false))
	assert.True(t, truthy(true))
	assert.True(t, truthy(""), "空の文字列は jq では真")
	assert.True(t, truthy(float64(0)), "0 は jq では真")
}

func Test_jsonEqual(t *testing.T) {
	t.Parallel()

	assert.True(t, jsonEqual(decode(t, `[{"a":"b"}]`), `[{"a":"b"}]`))
	assert.True(t, jsonEqual(decode(t, `{}`), `{}`))
	assert.False(t, jsonEqual(decode(t, `[{"a":"b"},{}]`), `[{"a":"b"}]`))
	assert.False(t, jsonEqual(nil, `[]`))
	assert.True(t, jsonEqual(float64(1), `1`))
	assert.False(t, jsonEqual("x", `{`), "literal を読めなければ偽")
}

func Test_anyString(t *testing.T) {
	t.Parallel()

	isX := func(s string) bool { return s == "x" }

	assert.True(t, anyString(decode(t, `{"a":[{"b":"x"}]}`), isX), "入れ子の値をたどる")
	assert.True(t, anyString("x", isX))
	assert.False(t, anyString(decode(t, `{"x":"y"}`), isX), "object の key は見ない")
	assert.False(t, anyString(decode(t, `[1,true,null]`), isX))
	assert.False(t, anyString(nil, isX))
}

func Test_jsonLength(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		in     any
		want   float64
		wantOK bool
	}{
		"null は 0":        {nil, 0, true},
		"配列は要素の数":         {[]any{1, 2}, 2, true},
		"object は key の数": {map[string]any{"a": 1}, 1, true},
		"文字列は文字の数":        {"あい", 2, true},
		"数は絶対値":           {float64(-3), 3, true},
		"真偽値はエラー":         {true, 0, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, ok := jsonLength(tc.in)

			assert.Equal(t, tc.wantOK, ok)
			assert.InDelta(t, tc.want, got, 0)
		})
	}
}

func Test_fieldEq(t *testing.T) {
	t.Parallel()

	m, ok := fieldEq("Deny", "Effect")(decode(t, `{"Effect":"Deny"}`))
	assert.True(t, m)
	assert.True(t, ok)

	m, ok = fieldEq("Deny", "Effect")(decode(t, `{"Effect":"Allow"}`))
	assert.False(t, m)
	assert.True(t, ok)

	m, ok = fieldEq("false", "Condition", "Bool", "k")(decode(t, `{}`))
	assert.False(t, m, "無いものは null で、文字列と等しくない")
	assert.True(t, ok)

	m, ok = fieldEq("x", "Condition", "Bool")(decode(t, `{"Condition":"s"}`))
	assert.False(t, m)
	assert.False(t, ok, "文字列をたどればエラー")

	m, ok = fieldEq(float64(1), "n")(decode(t, `{"n":{"a":1}}`))
	assert.False(t, m, "object の値と数の比較は偽で、panic しない")
	assert.True(t, ok)
}

func Test_arrEq(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		in     string
		keys   []string
		want   []string
		sorted bool
		wantM  bool
		wantOK bool
	}{
		"文字列1つを配列として比べる": {`{"a":"x"}`, []string{"a"}, []string{"x"}, false, true, true},
		"並べ替えて比べる":       {`{"a":["y","x"]}`, []string{"a"}, []string{"x", "y"}, true, true, true},
		"並べ替えなければ順序も比べる": {`{"a":["y","x"]}`, []string{"a"}, []string{"x", "y"}, false, false, true},
		"要素の過不足":         {`{"a":["x"]}`, []string{"a"}, []string{"x", "y"}, true, false, true},
		"文字列でない要素があれば偽":  {`{"a":["x",1]}`, []string{"a"}, []string{"x"}, true, false, true},
		"無ければ [null] で偽": {`{}`, []string{"a"}, []string{"x"}, false, false, true},
		"たどれなければエラー":     {`{"a":1}`, []string{"a", "b"}, nil, false, false, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			m, ok := arrEq(tc.want, tc.sorted, tc.keys...)(decode(t, tc.in))

			assert.Equal(t, tc.wantM, m)
			assert.Equal(t, tc.wantOK, ok)
		})
	}

	t.Run("比べる値の順序を書き換えない", func(t *testing.T) {
		t.Parallel()

		want := []string{"y", "x"}
		_, _ = arrEq(want, true, "a")(decode(t, `{"a":["x","y"]}`))

		assert.Equal(t, []string{"y", "x"}, want)
	})
}

func Test_allConds(t *testing.T) {
	t.Parallel()

	called := false
	never := func(any) (bool, bool) {
		called = true

		return true, true
	}

	m, ok := allConds(decode(t, `{"a":"x"}`), fieldEq("y", "a"), never)
	assert.False(t, m)
	assert.True(t, ok)
	assert.False(t, called, "偽の比較の後ろは評価しない")

	m, ok = allConds(decode(t, `{"a":"x","b":"y"}`), fieldEq("x", "a"), fieldEq("y", "b"))
	assert.True(t, m)
	assert.True(t, ok)

	m, ok = allConds(decode(t, `{"a":"x"}`), fieldEq("x", "a"), fieldEq("y", "a", "b"))
	assert.False(t, m)
	assert.False(t, ok)

	m, ok = allConds(nil)
	assert.True(t, m, "比較が無ければ真")
	assert.True(t, ok)
}

func Test_anyStatement(t *testing.T) {
	t.Parallel()

	stmts := func(s string) []any {
		a, _ := decode(t, s).([]any)

		return a
	}

	assert.True(t, anyStatement(stmts(`[{"a":"x"},{"a":"y"}]`), fieldEq("y", "a")))
	assert.False(t, anyStatement(stmts(`[{"a":"x"}]`), fieldEq("y", "a")))
	assert.False(t, anyStatement(stmts(`[]`), fieldEq("y", "a")), "空の配列は偽")
	assert.False(t, anyStatement(stmts(`[{"a":"y"},"s"]`), fieldEq("y", "a")), "評価できない要素があれば判定ごと偽")
	assert.True(t, anyStatement(stmts(`[{"a":"y","e":{"f":"x"}},{"a":"z","e":"s"}]`), fieldEq("y", "a"), fieldEq("x", "e", "f")),
		"先の比較が偽なら、後ろのたどれない比較は評価しない")
}

func Test_everyStatement(t *testing.T) {
	t.Parallel()

	stmts := func(s string) []any {
		a, _ := decode(t, s).([]any)

		return a
	}

	assert.True(t, everyStatement(stmts(`[{"a":"x"},{"a":"x"}]`), fieldEq("x", "a")))
	assert.False(t, everyStatement(stmts(`[{"a":"x"},{"a":"y"}]`), fieldEq("x", "a")))
	assert.True(t, everyStatement(stmts(`[]`), fieldEq("x", "a")), "空の配列は真")
	assert.False(t, everyStatement(stmts(`[{"a":"x"},1]`), fieldEq("x", "a")), "評価できない要素があれば偽")
}

package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_extract(t *testing.T) {
	t.Parallel()

	t.Run("書ける引数・非推奨・block を取り出し、computed だけの属性を除く", func(t *testing.T) {
		t.Parallel()
		var r resourceSchema
		require.NoError(t, json.Unmarshal([]byte(resourceX), &r))
		assert.Equal(t, entryX, extract(r))
	})

	t.Run("非推奨の required の属性も取り出す", func(t *testing.T) {
		t.Parallel()
		r := resourceSchema{Block: schemaBlock{Attributes: map[string]schemaAttribute{"a": {Required: true, Deprecated: true}}}}
		assert.Equal(t, entry{Arguments: []string{"a"}, Deprecated: []string{"a"}, Blocks: []string{}}, extract(r))
	})

	t.Run("属性も block も無ければ空の配列", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, entry{Arguments: []string{}, Deprecated: []string{}, Blocks: []string{}}, extract(resourceSchema{}))
	})
}

func Test_union(t *testing.T) {
	t.Parallel()

	t.Run("型ごとに3つの配列それぞれの和を重複なしの昇順で返す", func(t *testing.T) {
		t.Parallel()
		got := union([]map[string]entry{
			{
				"aws_a": {Arguments: []string{"b", "old"}, Deprecated: []string{"old"}, Blocks: []string{"b"}},
				"aws_b": {Arguments: []string{"x"}, Deprecated: []string{}, Blocks: []string{}},
			},
			{
				"aws_a": {Arguments: []string{"a", "b"}, Deprecated: []string{}, Blocks: []string{"a", "b"}},
				"aws_c": {Arguments: []string{}, Deprecated: []string{}, Blocks: []string{}},
			},
		})
		assert.Equal(t, map[string]entry{
			"aws_a": {Arguments: []string{"a", "b", "old"}, Deprecated: []string{"old"}, Blocks: []string{"a", "b"}},
			"aws_b": {Arguments: []string{"x"}, Deprecated: []string{}, Blocks: []string{}},
			"aws_c": {Arguments: []string{}, Deprecated: []string{}, Blocks: []string{}},
		}, got)
	})

	t.Run("ある版だけで非推奨の引数も、和の非推奨に含める", func(t *testing.T) {
		t.Parallel()
		got := union([]map[string]entry{
			{"aws_a": {Arguments: []string{"old"}, Deprecated: []string{"old"}, Blocks: []string{}}},
			{"aws_a": {Arguments: []string{"old"}, Deprecated: []string{}, Blocks: []string{}}},
		})
		assert.Equal(t, map[string]entry{
			"aws_a": {Arguments: []string{"old"}, Deprecated: []string{"old"}, Blocks: []string{}},
		}, got)
	})

	t.Run("版が無ければ空", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, union(nil))
	})
}

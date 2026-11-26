package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_drift_empty(t *testing.T) {
	t.Parallel()

	t.Run("差が1つも無ければ真", func(t *testing.T) {
		t.Parallel()
		assert.True(t, drift{missingRegions: map[string][]string{}, extraRegions: map[string][]string{}}.empty())
	})

	cases := []struct {
		name string
		d    drift
	}{
		{name: "写しに無い partition", d: drift{missingPartitions: []string{"a"}}},
		{name: "上流に無い partition", d: drift{extraPartitions: []string{"a"}}},
		{name: "写しに無い region", d: drift{missingRegions: map[string][]string{"a": {"x"}}}},
		{name: "上流に無い region", d: drift{extraRegions: map[string][]string{"a": {"x"}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name+"があれば偽", func(t *testing.T) {
			t.Parallel()
			assert.False(t, tc.d.empty())
		})
	}
}

func Test_report_verdict(t *testing.T) {
	t.Parallel()

	t.Run("比べて差が無ければ nil", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, report{compared: true}.verdict())
	})

	t.Run("差があれば errDrift", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, report{compared: true, drift: drift{extraPartitions: []string{"a"}}}.verdict(), errDrift)
	})

	t.Run("途中の失敗はそのまま返す", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, report{err: errHTTP}.verdict(), errHTTP)
	})

	t.Run("比べていなければ成功で返さない", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, report{}.verdict(), errNotCompared)
	})
}

func Test_compare(t *testing.T) {
	t.Parallel()

	t.Run("同じなら差は空", func(t *testing.T) {
		t.Parallel()
		set := partitionSet{"aws": {"a-1"}}
		assert.True(t, compare(set, set).empty())
	})

	t.Run("partition と region の差を両方向で出す", func(t *testing.T) {
		t.Parallel()
		up := partitionSet{"aws": {"a-1", "a-2"}, "aws-new": {"n-1"}}
		cp := partitionSet{"aws": {"a-1", "a-9"}, "aws-old": {"o-1"}}
		d := compare(up, cp)
		assert.Equal(t, []string{"aws-new"}, d.missingPartitions)
		assert.Equal(t, []string{"aws-old"}, d.extraPartitions)
		assert.Equal(t, map[string][]string{"aws": {"a-2"}}, d.missingRegions)
		assert.Equal(t, map[string][]string{"aws": {"a-9"}}, d.extraRegions)
	})
}

func Test_minus(t *testing.T) {
	t.Parallel()

	t.Run("a にだけ在るものを a の順で返す", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, []string{"c", "a"}, minus([]string{"c", "b", "a"}, []string{"b"}))
		assert.Empty(t, minus([]string{"a"}, []string{"a"}))
	})
}

func Test_renderSummary(t *testing.T) {
	t.Parallel()

	t.Run("一致なら一致の見出しと版を書く", func(t *testing.T) {
		t.Parallel()
		got := renderSummary(report{compared: true, lock: "providers/.terraform.lock.hcl", provider: "6.73.0", sdk: "v2.0.0-beta.76"})
		assert.Contains(t, got, "providers/.terraform.lock.hcl")
		assert.True(t, strings.HasPrefix(got, "## ✅"))
		assert.Contains(t, got, "- 比べた hashicorp/aws: 6.73.0\n")
		assert.NotContains(t, got, "| 差 |")
	})

	t.Run("ずれなら差の表を書く", func(t *testing.T) {
		t.Parallel()
		got := renderSummary(report{compared: true, drift: drift{
			missingPartitions: []string{"aws-new"},
			extraPartitions:   []string{"aws-old"},
			missingRegions:    map[string][]string{"aws": {"a-2", "a-3"}},
			extraRegions:      map[string][]string{"aws": {"a-9"}},
		}})
		assert.True(t, strings.HasPrefix(got, "## ⚠️"))
		assert.Contains(t, got, "| 写しに無い | `aws-new` | |\n")
		assert.Contains(t, got, "| 上流に無い | `aws-old` | |\n")
		assert.Contains(t, got, "| 写しに無い | `aws` | `a-2`, `a-3` |\n")
		assert.Contains(t, got, "| 上流に無い | `aws` | `a-9` |\n")
	})

	t.Run("差の表を partition の昇順で並べる", func(t *testing.T) {
		t.Parallel()
		got := renderSummary(report{compared: true, drift: drift{
			missingRegions: map[string][]string{"aws-us-gov": {"g-1"}, "aws": {"a-1"}, "aws-cn": {"c-1"}},
		}})
		a := strings.Index(got, "| `aws` |")
		c := strings.Index(got, "| `aws-cn` |")
		g := strings.Index(got, "| `aws-us-gov` |")
		require.NotEqual(t, -1, a)
		assert.Less(t, a, c)
		assert.Less(t, c, g)
	})

	t.Run("確認不能ならエラーを書き、分かった URL だけを書く", func(t *testing.T) {
		t.Parallel()
		got := renderSummary(report{err: errHTTP, endpointsURL: "https://e"})
		assert.True(t, strings.HasPrefix(got, "## ❌"))
		assert.Contains(t, got, errHTTP.Error())
		assert.Contains(t, got, "- endpoints: https://e\n")
		assert.NotContains(t, got, "go.mod:")
	})
}

func Test_writeField(t *testing.T) {
	t.Parallel()

	t.Run("値があれば1行を書き、空なら書かない", func(t *testing.T) {
		t.Parallel()
		var b strings.Builder
		writeField(&b, "a", "1")
		writeField(&b, "b", "")
		assert.Equal(t, "- a: 1\n", b.String())
	})
}

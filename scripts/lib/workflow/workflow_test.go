package workflow_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/workflow"
)

func Test_SelectFiles(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		names []string
		want  []string
	}{
		".yaml と .yml の両方を採る": {
			// GitHub がどちらも読むため。片方だけを見ると、拡張子を替えただけの workflow が
			// 検査から静かに外れる。
			names: []string{"b.yml", "a.yaml"},
			want:  []string{"d/a.yaml", "d/b.yml"},
		},
		"並びを固定する": {
			// 違反の出力順が実行ごとに揺れると、CI の失敗差分が読めなくなる。
			names: []string{"z.yaml", "a.yaml", "m.yaml"},
			want:  []string{"d/a.yaml", "d/m.yaml", "d/z.yaml"},
		},
		"対象外の拡張子は落とす": {
			names: []string{"README.md", "a.yaml", "notes.txt"},
			want:  []string{"d/a.yaml"},
		},
		"1件も無い": {
			names: []string{"README.md"},
			want:  []string{},
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, workflow.SelectFiles(tt.names, "d"))
		})
	}
}

func Test_SplitJobs(t *testing.T) {
	t.Parallel()

	t.Run("jobs: が無いとき found は false", func(t *testing.T) {
		t.Parallel()
		// 検査対象の取り違えを、ジョブ0件と区別するため。
		got := workflow.SplitJobs("name: x\non:\n  pull_request:\n")
		require.False(t, got.Found)
		assert.Empty(t, got.Jobs)
	})

	t.Run("ジョブ見出しと本文を切り出す", func(t *testing.T) {
		t.Parallel()
		src := "jobs:\n  build:\n    runs-on: ubuntu-latest\n  test:\n    runs-on: ubuntu-latest\n"
		got := workflow.SplitJobs(src)
		require.True(t, got.Found)
		require.Len(t, got.Jobs, 2)
		assert.Equal(t, "build", got.Jobs[0].ID)
		assert.Equal(t, 2, got.Jobs[0].Number)
		assert.Equal(t, "test", got.Jobs[1].ID)
		assert.Equal(t, 4, got.Jobs[1].Number)
	})

	t.Run("桁0のコメント行で打ち切らない", func(t *testing.T) {
		t.Parallel()
		// トップレベルキーではないため、ここで打ち切ると以降のジョブが丸ごと検査対象から外れる。
		src := "jobs:\n  a:\n    x: 1\n# コメント\n  b:\n    y: 2\n"
		got := workflow.SplitJobs(src)
		require.True(t, got.Found)
		require.Len(t, got.Jobs, 2)
		assert.Equal(t, "b", got.Jobs[1].ID)
	})

	t.Run("トップレベルキーで打ち切る", func(t *testing.T) {
		t.Parallel()
		src := "jobs:\n  a:\n    x: 1\nenv:\n  FOO: bar\n"
		got := workflow.SplitJobs(src)
		require.Len(t, got.Jobs, 1)
		// jobs: の外側は preamble として残る。ジョブ本文には現れないが、ジョブへ届く。
		var texts []string
		for _, l := range got.Preamble {
			texts = append(texts, l.Text)
		}
		assert.Contains(t, texts, "  FOO: bar")
	})

	t.Run("引用符付きの見出しも読む", func(t *testing.T) {
		t.Parallel()
		// 書式差で検出が外れると、規約が破られた瞬間に検査が沈黙する。
		for _, src := range []string{
			"jobs:\n  \"quoted\":\n    x: 1\n",
			"jobs:\n  'quoted':\n    x: 1\n",
			"jobs:\n  quoted: # 行末コメント\n    x: 1\n",
		} {
			got := workflow.SplitJobs(src)
			require.Len(t, got.Jobs, 1, src)
			assert.Equal(t, "quoted", got.Jobs[0].ID, src)
		}
	})
}

func Test_SplitSteps(t *testing.T) {
	t.Parallel()

	t.Run("steps: が無いとき空", func(t *testing.T) {
		t.Parallel()
		got := workflow.SplitJobs("jobs:\n  a:\n    runs-on: x\n")
		assert.Empty(t, workflow.SplitSteps(got.Jobs[0]))
	})

	t.Run("- で区切る", func(t *testing.T) {
		t.Parallel()
		src := "jobs:\n  a:\n    steps:\n      - name: one\n        run: x\n      - name: two\n        run: y\n"
		job := workflow.SplitJobs(src).Jobs[0]
		steps := workflow.SplitSteps(job)
		require.Len(t, steps, 2)
		assert.Equal(t, 4, steps[0].Number)
		assert.Equal(t, 6, steps[1].Number)
	})

	t.Run("steps: と同じかそれより浅い桁で終わる", func(t *testing.T) {
		t.Parallel()
		src := "jobs:\n  a:\n    steps:\n      - name: one\n    outputs:\n      x: 1\n"
		job := workflow.SplitJobs(src).Jobs[0]
		steps := workflow.SplitSteps(job)
		require.Len(t, steps, 1)
		for _, l := range steps[0].Lines {
			assert.NotContains(t, l.Text, "outputs")
		}
	})
}

func Test_UsesActionPattern(t *testing.T) {
	t.Parallel()

	action := "./.github/actions/upsert-pr-comment"

	t.Run("anchored はステップの桁だけに当たる", func(t *testing.T) {
		t.Parallel()
		re := workflow.UsesActionPattern(action, true)
		assert.True(t, re.MatchString("      - uses: "+action))
		assert.True(t, re.MatchString("        uses: "+action))
		assert.False(t, re.MatchString("uses: "+action))
	})

	t.Run("非 anchored は行中どこでも当たる", func(t *testing.T) {
		t.Parallel()
		re := workflow.UsesActionPattern(action, false)
		assert.True(t, re.MatchString("        uses: "+action))
		assert.True(t, re.MatchString(`        uses: "`+action+`"`))
		assert.True(t, re.MatchString("        uses: "+action+" # コメント"))
	})

	t.Run("別のアクションには当たらない", func(t *testing.T) {
		t.Parallel()
		re := workflow.UsesActionPattern(action, false)
		assert.False(t, re.MatchString("        uses: ./.github/actions/notify-detail"))
	})
}

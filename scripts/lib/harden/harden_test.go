package harden

import (
	"strings"
	"testing"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const uses = "        uses: bullfrogsec/bullfrog@abc # v1"

func jobOf(t *testing.T, steps ...string) workflow.Job {
	t.Helper()
	src := "jobs:\n  j:\n    runs-on: x\n    steps:\n" + strings.Join(steps, "\n") + "\n"
	split := workflow.SplitJobs(src)
	require.Len(t, split.Jobs, 1)

	return split.Jobs[0]
}

func TestHead(t *testing.T) {
	t.Parallel()

	t.Run("本体は id と continue-on-error: true を持つ", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, []string{"      - name: Harden the runner", "        id: harden-runner", "        continue-on-error: true"}, Head())
	})
}

func TestRetryHead(t *testing.T) {
	t.Parallel()

	t.Run("再試行は本体が成功しなかったときだけ走り、continue-on-error を持たない", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, []string{"      - name: Harden the runner (retry)", "        if: steps.harden-runner.outcome != 'success'"}, RetryHead())
	})
}

func TestRender(t *testing.T) {
	t.Parallel()

	t.Run("本体と再試行に同じ uses と with を書く", func(t *testing.T) {
		t.Parallel()
		with := []string{"        with:", "          egress-policy: block"}
		got := Render(uses, with)
		want := append(append(append(append(Head(), uses), with...), RetryHead()...), append([]string{uses}, with...)...)
		assert.Equal(t, want, got)
	})
}

func TestGeneratedLines(t *testing.T) {
	t.Parallel()

	pair := strings.Join(Render(uses, []string{"        with:", "          egress-policy: block"}), "\n")
	t.Run("生成した対なら continue-on-error と if の行だけを返す", func(t *testing.T) {
		t.Parallel()
		job := jobOf(t, pair, "      - name: Check", "        run: make x")
		got := GeneratedLines(job)
		// job の見出しが1行目、runs-on・steps: が2・3行目なので、本体の continue-on-error は7行目、再試行の if は
		// 本体（name・id・continue-on-error・uses・with の2行）の後ろの2行目。
		lineOf := func(n int) string {
			for _, l := range job.Lines {
				if l.Number == n {
					return l.Text
				}
			}
			return ""
		}
		require.Len(t, got, 2)
		var texts []string
		for n := range got {
			texts = append(texts, lineOf(n))
		}
		assert.ElementsMatch(t, []string{"        continue-on-error: true", "        if: " + RetryIf}, texts)
	})
	// uses と with の対の一致は egress の検査が持つ。ここは頭の形だけを見て除外する。
	t.Run("再試行の with: が本体と違っても、頭の形が同じなら除外する", func(t *testing.T) {
		t.Parallel()
		src := strings.Replace(pair, "          egress-policy: block", "          egress-policy: audit", 2)
		src = strings.Replace(src, "          egress-policy: audit", "          egress-policy: block", 1)
		assert.Len(t, GeneratedLines(jobOf(t, src, "      - name: Check", "        run: make x")), 2)
	})
	for name, steps := range map[string][]string{
		"対が2番目にある":   {"      - name: Check", "        run: make x", pair},
		"対が3番目以降にある": {"      - name: Check", "        run: make x", "      - name: Other", "        run: make y", pair},
	} {
		t.Run(name+"なら除外しない", func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, GeneratedLines(jobOf(t, steps...)))
		})
	}
	for name, src := range map[string]string{
		"再試行の if が別の式":              strings.Replace(pair, RetryIf, "failure()", 1),
		"再試行の if が always()":        strings.Replace(pair, RetryIf, "always()", 1),
		"本体の id が違う":                strings.Replace(pair, "id: harden-runner", "id: other", 1),
		"本体の continue-on-error が無い": strings.Replace(pair, "        continue-on-error: true\n", "", 1),
		"再試行が無い":                    strings.Join(append(Head(), uses), "\n"),
		"再試行が bullfrog でない":         strings.Replace(pair, "      - name: Harden the runner (retry)\n        if: "+RetryIf+"\n"+uses, "      - name: Harden the runner (retry)\n        if: "+RetryIf+"\n        uses: other/action@x", 1),
		"本体が bullfrog でない":          strings.Replace(pair, "        continue-on-error: true\n"+uses, "        continue-on-error: true\n        uses: other/action@x", 1),
		"本体の名前が違う":                  strings.Replace(pair, "- name: Harden the runner\n", "- name: Harden\n", 1),
	} {
		t.Run(name+"なら除外しない", func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, GeneratedLines(jobOf(t, src, "      - name: Check", "        run: make x")))
		})
	}
	t.Run("step が0件なら除外しない", func(t *testing.T) {
		t.Parallel()
		split := workflow.SplitJobs("jobs:\n  j:\n    runs-on: x\n")
		require.Len(t, split.Jobs, 1)
		assert.Empty(t, GeneratedLines(split.Jobs[0]))
	})
	t.Run("step が1つだけなら除外しない", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, GeneratedLines(jobOf(t, "      - name: Check", "        run: make x")))
	})
}

func Test_startsWith(t *testing.T) {
	t.Parallel()

	st := workflow.Step{Lines: []workflow.Line{{Text: "a"}, {Text: "b"}, {Text: "c"}}}
	t.Run("頭が一字一句同じで、後ろに行が続けば真", func(t *testing.T) {
		t.Parallel()
		assert.True(t, startsWith(st, []string{"a", "b"}))
	})
	t.Run("頭の1行でも違えば偽", func(t *testing.T) {
		t.Parallel()
		assert.False(t, startsWith(st, []string{"a", "x"}))
	})
	t.Run("頭の後ろに行が無ければ偽", func(t *testing.T) {
		t.Parallel()
		assert.False(t, startsWith(st, []string{"a", "b", "c"}))
	})
}

func Test_usesBullfrog(t *testing.T) {
	t.Parallel()

	for text, want := range map[string]bool{
		uses:                                     true,
		"        uses: bullfrogsec/bullfrog-x@1": false,
		"        uses: other/action@1":           false,
		"          uses: bullfrogsec/bullfrog@1": false,
	} {
		t.Run(text, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, want, usesBullfrog(workflow.Line{Text: text}))
		})
	}
}

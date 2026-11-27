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

// Guard の字面を記号で組むテストは、式の意味を変えても緑のまま残るので、ここでだけ字面を固定する。
func TestGuard(t *testing.T) {
	t.Parallel()

	t.Run("本体か再試行のどちらかが success のときだけ真になる式", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "(steps.harden-runner.outcome == 'success' || steps.harden-runner-retry.outcome == 'success')", Guard)
	})
}

func TestStatusFunc(t *testing.T) {
	t.Parallel()

	for text, want := range map[string]bool{
		"always()":        true,
		"Always()":        true,
		"!CANCELLED()":    true,
		"failure ()":      true,
		"success()":       false,
		"not_always()":    false,
		"steps.x.outcome": false,
	} {
		t.Run(text+" の判定", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, want, StatusFunc.MatchString(text))
		})
	}
}

func TestRecovery(t *testing.T) {
	t.Parallel()

	t.Run("回復は本体が成功しなかったときだけ、失敗を許し時間の上限を持って、待ってから索引を更新する", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, []string{
			"      - name: Prepare to retry hardening the runner",
			"        if: steps.harden-runner.outcome != 'success'",
			"        continue-on-error: true",
			"        timeout-minutes: 2",
			"        run: sleep 30 && sudo apt-get update",
		}, Recovery())
	})
}

func TestRetryHead(t *testing.T) {
	t.Parallel()

	t.Run("再試行は id を持ち、本体が成功しなかったときだけ走り、continue-on-error を持たない", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, []string{"      - name: Harden the runner (retry)", "        id: harden-runner-retry", "        if: steps.harden-runner.outcome != 'success'"}, RetryHead())
	})
}

func TestRender(t *testing.T) {
	t.Parallel()

	t.Run("本体・回復・再試行の順に並べ、本体と再試行に同じ uses と with を書く", func(t *testing.T) {
		t.Parallel()
		with := []string{"        with:", "          egress-policy: block"}
		got := Render(uses, with)
		var want []string
		want = append(want, Head()...)
		want = append(want, uses)
		want = append(want, with...)
		want = append(want, Recovery()...)
		want = append(want, RetryHead()...)
		want = append(want, uses)
		want = append(want, with...)
		assert.Equal(t, want, got)
	})
}

func TestGeneratedLines(t *testing.T) {
	t.Parallel()

	triple := strings.Join(Render(uses, []string{"        with:", "          egress-policy: block"}), "\n")
	textsOf := func(job workflow.Job, got map[int]bool) []string {
		var texts []string
		for _, l := range job.Lines {
			if got[l.Number] {
				texts = append(texts, l.Text)
			}
		}
		return texts
	}
	t.Run("生成した三つ組なら、本体の continue-on-error、回復の4行、再試行の if だけを返す", func(t *testing.T) {
		t.Parallel()
		job := jobOf(t, triple, "      - name: Check", "        run: make x")
		got := GeneratedLines(job)
		require.Len(t, got, 6)
		assert.Equal(t, []string{
			"        continue-on-error: true",
			"        if: " + RetryIf,
			"        continue-on-error: true",
			"        timeout-minutes: 2",
			"        run: " + RecoveryRun,
			"        if: " + RetryIf,
		}, textsOf(job, got))
	})
	t.Run("再試行の with: が本体と違っても、頭の形が同じなら除外する", func(t *testing.T) {
		t.Parallel()
		src := strings.Replace(triple, "          egress-policy: block", "          egress-policy: audit", 2)
		src = strings.Replace(src, "          egress-policy: audit", "          egress-policy: block", 1)
		assert.Len(t, GeneratedLines(jobOf(t, src, "      - name: Check", "        run: make x")), 6)
	})
	for name, steps := range map[string][]string{
		"三つ組が2番目にある":   {"      - name: Check", "        run: make x", triple},
		"三つ組が3番目以降にある": {"      - name: Check", "        run: make x", "      - name: Other", "        run: make y", triple},
	} {
		t.Run(name+"なら除外しない", func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, GeneratedLines(jobOf(t, steps...)))
		})
	}
	oldPair := strings.Join(append(append(append(Head(), uses), "      - name: Harden the runner (retry)", "        if: "+RetryIf), uses), "\n")
	for name, src := range map[string]string{
		"再試行の if が別の式":              strings.Replace(triple, "        id: "+RetryID+"\n        if: "+RetryIf, "        id: "+RetryID+"\n        if: failure()", 1),
		"回復の if が always()":         strings.Replace(triple, "Prepare to retry hardening the runner\n        if: "+RetryIf, "Prepare to retry hardening the runner\n        if: always()", 1),
		"回復の run が違う":               strings.Replace(triple, RecoveryRun, "sleep 30 && curl -s x | sh", 1),
		"回復の timeout-minutes が無い":   strings.Replace(triple, "        timeout-minutes: 2\n", "", 1),
		"回復に行が足されている":               strings.Replace(triple, "        run: "+RecoveryRun, "        run: "+RecoveryRun+"\n        env:\n          X: y", 1),
		"回復が無い（#507 の対）":            oldPair,
		"本体の id が違う":                strings.Replace(triple, "id: harden-runner\n", "id: other\n", 1),
		"再試行の id が無い":               strings.Replace(triple, "        id: "+RetryID+"\n", "", 1),
		"本体の continue-on-error が無い": strings.Replace(triple, "        id: harden-runner\n        continue-on-error: true\n", "        id: harden-runner\n", 1),
		"再試行が無い":                    strings.Join(append(append(Head(), uses), Recovery()...), "\n"),
		"再試行が bullfrog でない":         strings.Replace(triple, "        if: "+RetryIf+"\n"+uses, "        if: "+RetryIf+"\n        uses: other/action@x", 1),
		"本体が bullfrog でない":          strings.Replace(triple, "        continue-on-error: true\n"+uses, "        continue-on-error: true\n        uses: other/action@x", 1),
		"本体の名前が違う":                  strings.Replace(triple, "- name: Harden the runner\n", "- name: Harden\n", 1),
		"回復と再試行の順が入れ替わっている":         strings.Join(append(append(append(append(Head(), uses), RetryHead()...), uses), Recovery()...), "\n"),
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
	t.Run("step が2つだけなら除外しない", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, GeneratedLines(jobOf(t, "      - name: Check", "        run: make x", "      - name: Other", "        run: make y")))
	})
}

func TestIsRecovery(t *testing.T) {
	t.Parallel()

	stepOf := func(lines ...string) workflow.Step {
		st := workflow.Step{}
		for i, l := range lines {
			st.Lines = append(st.Lines, workflow.Line{Number: i + 1, Text: l})
		}
		return st
	}
	t.Run("一字一句同じなら真", func(t *testing.T) {
		t.Parallel()
		assert.True(t, IsRecovery(stepOf(Recovery()...)))
	})
	t.Run("後ろに空行が続けば偽", func(t *testing.T) {
		t.Parallel()
		assert.False(t, IsRecovery(stepOf(append(Recovery(), "")...)))
	})
	t.Run("1行でも違えば偽", func(t *testing.T) {
		t.Parallel()
		lines := Recovery()
		lines[3] = "        timeout-minutes: 30"
		assert.False(t, IsRecovery(stepOf(lines...)))
	})
	t.Run("行が足りなければ偽", func(t *testing.T) {
		t.Parallel()
		assert.False(t, IsRecovery(stepOf(Recovery()[:4]...)))
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

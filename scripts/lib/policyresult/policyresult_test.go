package policyresult

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCountRules(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		rego string
		want int
	}{
		"行頭の deny contains を数える":    {"package p\n\ndeny contains msg if {\n}\n\ndeny contains msg if {\n}\n", 2},
		"字下げした deny contains は数えない": {"package p\n\n  deny contains msg if {\n}\n", 0},
		"コメントの deny contains は数えない": {"package p\n# deny contains msg\n", 0},
		"規則が無ければ0":                  {"package p\n", 0},
		"空なら0":                      {"", 0},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, c.want, CountRules([]byte(c.rego)))
		})
	}
}

func TestJudge(t *testing.T) {
	t.Parallel()

	entries := func(t *testing.T, data string) []Entry {
		t.Helper()

		got, err := Parse([]byte(data))
		require.NoError(t, err)

		return got
	}

	t.Run("宣言した数の規則が評価され違反が無ければ件数を出す", func(t *testing.T) {
		t.Parallel()

		got, err := Judge(map[string]int{"plan.p": 3}, entries(t, `[{"filename":"Combined","namespace":"plan.p","successes":2,"warnings":[{"msg":"w"}]}]`))
		require.NoError(t, err)
		assert.Equal(t, "✅ plan.p: 規則 3 件を評価し、違反はありません", got)
	})

	t.Run("同じ名前空間の複数の結果を合算する", func(t *testing.T) {
		t.Parallel()

		got, err := Judge(map[string]int{"plan.p": 3}, entries(t, `[{"namespace":"plan.p","successes":1},{"namespace":"plan.p","successes":2}]`))
		require.NoError(t, err)
		assert.Equal(t, "✅ plan.p: 規則 3 件を評価し、違反はありません", got)
	})

	t.Run("例外も評価として数える", func(t *testing.T) {
		t.Parallel()

		got, err := Judge(map[string]int{"plan.p": 2}, entries(t, `[{"namespace":"plan.p","successes":1,"exceptions":[{"msg":"e"}]}]`))
		require.NoError(t, err)
		assert.Equal(t, "✅ plan.p: 規則 2 件を評価し、違反はありません", got)
	})

	t.Run("複数の名前空間がすべて一致すれば、名前空間ごとに1行を名前の順で出す", func(t *testing.T) {
		t.Parallel()

		got, err := Judge(map[string]int{"plan.q": 1, "plan.p": 2}, entries(t, `[{"namespace":"plan.q","successes":1},{"namespace":"plan.p","successes":2}]`))
		require.NoError(t, err)
		assert.Equal(t, "✅ plan.p: 規則 2 件を評価し、違反はありません\n✅ plan.q: 規則 1 件を評価し、違反はありません", got)
	})

	t.Run("宣言も評価も0件の名前空間（テストの package）は数えない", func(t *testing.T) {
		t.Parallel()

		got, err := Judge(map[string]int{"plan.p": 1}, entries(t, `[{"namespace":"plan.p","successes":1},{"namespace":"plan.p_test","successes":0}]`))
		require.NoError(t, err)
		assert.Equal(t, "✅ plan.p: 規則 1 件を評価し、違反はありません", got)
	})

	t.Run("違反があれば、その文を持って失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := Judge(map[string]int{"plan.p": 3, "plan.q": 1}, entries(t, `[{"namespace":"plan.p","successes":1,"failures":[{"msg":"[PLAN-STATE-01] x"}]}]`))
		require.ErrorIs(t, err, ErrViolation)
		assert.Contains(t, err.Error(), "[PLAN-STATE-01] x")
		assert.NotErrorIs(t, err, ErrCount, "違反があるときは件数を突き合わせない")
		assert.NotErrorIs(t, err, ErrEmpty, "違反があるときは評価の欠けを報告しない")
	})

	t.Run("複数の名前空間の違反は、すべての文を持って失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := Judge(map[string]int{"plan.p": 1, "plan.q": 1}, entries(t, `[{"namespace":"plan.p","failures":[{"msg":"a"}]},{"namespace":"plan.q","failures":[{"msg":"b"}]}]`))
		require.ErrorIs(t, err, ErrViolation)
		assert.Contains(t, err.Error(), "a\nb")
	})

	t.Run("違反だけの結果は、0件の評価ではなく違反として失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := Judge(map[string]int{"plan.p": 3}, entries(t, `[{"namespace":"plan.p","successes":0,"failures":[{"msg":"a"}]}]`))
		require.ErrorIs(t, err, ErrViolation)
	})

	t.Run("評価された規則が宣言より少なければ失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := Judge(map[string]int{"plan.p": 3}, entries(t, `[{"namespace":"plan.p","successes":1}]`))
		require.ErrorIs(t, err, ErrCount)
		assert.Contains(t, err.Error(), "plan.p: 評価 1 件、宣言 3 件")
	})

	t.Run("評価された規則が宣言より多ければ失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := Judge(map[string]int{"plan.p": 3}, entries(t, `[{"namespace":"plan.p","successes":4}]`))
		require.ErrorIs(t, err, ErrCount)
	})

	t.Run("宣言の無い名前空間を評価していれば失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := Judge(map[string]int{"plan.p": 1}, entries(t, `[{"namespace":"plan.p","successes":1},{"namespace":"plan.q","successes":2}]`))
		require.ErrorIs(t, err, ErrCount)
		assert.Contains(t, err.Error(), "plan.q: 評価 2 件、宣言 0 件")
	})

	t.Run("名前空間の規則が0件の評価なら失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := Judge(map[string]int{"plan.p": 3}, entries(t, `[{"namespace":"plan.p","successes":0}]`))
		require.ErrorIs(t, err, ErrEmpty)
	})

	t.Run("宣言した名前空間が結果に無ければ（package が無い）失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := Judge(map[string]int{"plan.p": 1, "plan.q": 2}, entries(t, `[{"namespace":"plan.p","successes":1}]`))
		require.ErrorIs(t, err, ErrEmpty)
		assert.Contains(t, err.Error(), "plan.q")
	})

	t.Run("評価されない名前空間と件数の食い違いが混ざれば、両方を返す", func(t *testing.T) {
		t.Parallel()

		_, err := Judge(map[string]int{"plan.p": 1, "plan.q": 2}, entries(t, `[{"namespace":"plan.p","successes":3}]`))
		require.ErrorIs(t, err, ErrEmpty)
		require.ErrorIs(t, err, ErrCount)
	})

	t.Run("結果が空の配列なら失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := Judge(map[string]int{"plan.p": 3}, entries(t, `[]`))
		require.ErrorIs(t, err, ErrEmpty)
	})

	t.Run("結果が null なら、評価されていないとして失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := Judge(map[string]int{"plan.p": 1}, entries(t, `null`))
		require.ErrorIs(t, err, ErrEmpty)
	})

	t.Run("宣言が空なら、結果を見ずに失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := Judge(map[string]int{}, entries(t, `[]`))
		require.ErrorIs(t, err, ErrRules)
	})
}

func TestParse(t *testing.T) {
	t.Parallel()

	t.Run("conftest の JSON の項目を読む", func(t *testing.T) {
		t.Parallel()

		got, err := Parse([]byte(`[{"namespace":"plan.p","successes":2,"failures":[{"msg":"a"}]}]`))
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, "plan.p", got[0].Namespace)
		assert.Equal(t, 2, got[0].Successes)
		assert.Equal(t, "a", got[0].Failures[0].Msg)
	})

	cases := map[string]string{
		"空の入力":     "",
		"JSON でない": "FAIL - x",
		"配列でない":    `{"namespace":"plan.p"}`,
		"件数が数でない":  `[{"namespace":"plan.p","successes":"3"}]`,
	}
	for name, data := range cases {
		t.Run(name+"なら失敗する", func(t *testing.T) {
			t.Parallel()

			_, err := Parse([]byte(data))
			require.ErrorIs(t, err, ErrJSON)
		})
	}
}

func TestRules(t *testing.T) {
	t.Parallel()

	rule := "deny contains msg if {\n\tfalse\n\tmsg := \"x\"\n}\n\n"

	t.Run("package ごとに行頭の deny contains を数え、ファイルをまたいで合算する", func(t *testing.T) {
		t.Parallel()

		dir := policyDirWith(t, map[string]string{
			"a.rego":              "package plan.a\n\n" + rule + rule,
			"a_more.rego":         "package plan.a\n\n" + rule,
			"a_test.rego":         "package plan.a_test\n",
			"lib/input.rego":      "package plan.input\n\n" + rule,
			"lib/plan.rego":       "package plan.lib\n",
			"lib/README.md":       "deny contains x\n",
			"lib/input_test.rego": "package plan.input_test\n",
		})
		got, err := Rules(dir, os.ReadFile)
		require.NoError(t, err)
		assert.Equal(t, map[string]int{"plan.a": 3, "plan.input": 1}, got)
	})

	t.Run("字下げした deny contains は規則として数えない", func(t *testing.T) {
		t.Parallel()

		dir := policyDirWith(t, map[string]string{"a.rego": "package plan.a\n\n" + rule + "  " + rule})
		got, err := Rules(dir, os.ReadFile)
		require.NoError(t, err)
		assert.Equal(t, map[string]int{"plan.a": 1}, got)
	})

	valid := "package plan.a\n\n" + rule
	failures := map[string]struct {
		files map[string]string
		want  string
	}{
		"規則が全体で0件":    {map[string]string{"lib/plan.rego": "package plan.lib\n"}, "deny contains が1つもありません"},
		"rego が1つも無い": {map[string]string{"README.md": "x\n"}, "deny contains が1つもありません"},
		"他の rego が有効でも、package の行を持たない rego": {map[string]string{"a.rego": valid, "b.rego": rule}, "b.rego: package の行がありません"},
		"lib の外で規則を持たない rego":                {map[string]string{"a.rego": valid, "b.rego": "package plan.b\n"}, "b.rego: deny contains を持たない rego は lib/ の下にだけ置けます"},
		"lib の下の lib で規則を持たない rego":          {map[string]string{"a.rego": valid, "sub/lib/b.rego": "package plan.b\n"}, "lib/b.rego: deny contains を持たない"},
		"lib で始まる別のディレクトリで規則を持たない rego":      {map[string]string{"a.rego": valid, "libx/b.rego": "package plan.b\n"}, "libx/b.rego: deny contains を持たない"},
		"直下の lib.rego で規則を持たない":              {map[string]string{"a.rego": valid, "lib.rego": "package plan.lib\n"}, "lib.rego: deny contains を持たない"},
		"テストの対象の package が無い":                {map[string]string{"a.rego": valid, "b_test.rego": "package plan.b_test\n"}, "テストの対象の plan.b に deny contains がありません"},
		"テストの対象の package が規則を持たない":           {map[string]string{"a.rego": valid, "lib/b.rego": "package plan.b\n", "lib/b_test.rego": "package plan.b_test\n"}, "テストの対象の plan.b に deny contains がありません"},
		"テストの package が _test で終わらない":        {map[string]string{"a.rego": valid, "a_test.rego": "package plan.a\n"}, "a_test.rego: テストの package が _test で終わりません"},
	}
	for name, c := range failures {
		t.Run(name+"なら失敗する", func(t *testing.T) {
			t.Parallel()

			_, err := Rules(policyDirWith(t, c.files), os.ReadFile)
			require.ErrorIs(t, err, ErrRules)
			assert.Contains(t, err.Error(), c.want)
		})
	}

	t.Run("ディレクトリが無ければ失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := Rules(filepath.Join(t.TempDir(), "missing"), os.ReadFile)
		require.ErrorIs(t, err, ErrRules)
		assert.Contains(t, err.Error(), "missing")
	})

	t.Run("他の rego が有効でも、読めない rego があれば失敗する", func(t *testing.T) {
		t.Parallel()

		dir := policyDirWith(t, map[string]string{"a.rego": valid, "b.rego": valid})
		_, err := Rules(dir, func(path string) ([]byte, error) {
			if filepath.Base(path) == "b.rego" {
				return nil, os.ErrPermission
			}

			return os.ReadFile(path)
		})
		require.ErrorIs(t, err, ErrRules)
		assert.Contains(t, err.Error(), "b.rego: ファイルを読めません")
	})
}

// policyDirWith は、files（相対パスから中身）を一時ディレクトリに置き、そのパスを返す。
func policyDirWith(t *testing.T, files map[string]string) string {
	t.Helper()

	dir := t.TempDir()
	for name, body := range files {
		path := filepath.Join(dir, name)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	}

	return dir
}

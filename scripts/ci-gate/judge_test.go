package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func judgeDecl() decl {
	return decl{Checks: []check{
		{Name: "a", Paths: []string{}, Scope: scopeTree, Required: boolp(true)},
		{Name: "b", Paths: []string{"b/**"}, Scope: scopeTree, Required: boolp(true)},
		{Name: "c", Paths: []string{}, Scope: scopeChange, Required: boolp(true)},
		{Name: "info", Paths: []string{}, Scope: scopeTree, Required: boolp(false)},
	}}
}

func okNeeds(run string, results map[string]string) map[string]need {
	n := map[string]need{changesJob: {Result: "success", Outputs: map[string]string{"run": run}}}
	for k, v := range results {
		n[k] = need{Result: v}
	}
	return n
}

func Test_parseNeeds(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("job ごとの result と outputs を読む", func(t *testing.T) {
			t.Parallel()
			n, err := parseNeeds(`{"changes": {"result": "success", "outputs": {"run": "[]"}}, "a": {"result": "skipped", "outputs": {}}}`)
			require.NoError(t, err)
			assert.Equal(t, "success", n["changes"].Result)
			assert.Equal(t, "[]", n["changes"].Outputs["run"])
			assert.Equal(t, "skipped", n["a"].Result)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for name, raw := range map[string]string{"空": "", "JSON でない": "{", "job が0件": "{}"} {
			t.Run(name+"なら誤りにする", func(t *testing.T) {
				t.Parallel()
				_, err := parseNeeds(raw)
				require.ErrorIs(t, err, errNeeds)
			})
		}
	})
}

func Test_sameSet(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("集合が一致すれば誤りにしない", func(t *testing.T) {
			t.Parallel()
			require.NoError(t, sameSet([]string{"a", "b"}, map[string]need{"b": {}, "a": {}}))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("宣言にあって needs に無い job を誤りにする", func(t *testing.T) {
			t.Parallel()
			err := sameSet([]string{"a", "b"}, map[string]need{"a": {}})
			require.ErrorIs(t, err, errNeeds)
			assert.Contains(t, err.Error(), "needs に無い: [b]")
		})

		t.Run("needs にあって宣言に無い job を誤りにする", func(t *testing.T) {
			t.Parallel()
			err := sameSet([]string{"a"}, map[string]need{"a": {}, "z": {}})
			require.ErrorIs(t, err, errNeeds)
			assert.Contains(t, err.Error(), "宣言に無い: [z]")
		})
	})
}

func Test_judgePR(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("走るべき検査が success で走るべきでない検査が skipped なら合格", func(t *testing.T) {
			t.Parallel()
			rows, err := judgePR(judgeDecl(), []string{"a", "c", "info"}, okNeeds(`["a","c","info"]`,
				map[string]string{"a": "success", "b": "skipped", "c": "success", "info": "success"}))
			require.NoError(t, err)
			assert.Len(t, rows, 5)
		})

		t.Run("required でない検査の失敗は合否に数えない", func(t *testing.T) {
			t.Parallel()
			rows, err := judgePR(judgeDecl(), []string{"a", "c", "info"}, okNeeds(`["a","c","info"]`,
				map[string]string{"a": "success", "b": "skipped", "c": "success", "info": "failure"}))
			require.NoError(t, err)
			assert.False(t, rows[4].OK)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for name, results := range map[string]map[string]string{
			"走るべき検査が skipped":      {"a": "skipped", "b": "skipped", "c": "success", "info": "success"},
			"走るべき検査が cancelled":    {"a": "cancelled", "b": "skipped", "c": "success", "info": "success"},
			"走るべき検査が failure":      {"a": "failure", "b": "skipped", "c": "success", "info": "success"},
			"走るべきでない検査が failure":   {"a": "success", "b": "failure", "c": "success", "info": "success"},
			"走るべきでない検査が cancelled": {"a": "success", "b": "cancelled", "c": "success", "info": "success"},
		} {
			t.Run(name+"なら不合格", func(t *testing.T) {
				t.Parallel()
				_, err := judgePR(judgeDecl(), []string{"a", "c", "info"}, okNeeds(`["a","c","info"]`, results))
				require.ErrorIs(t, err, errFailed)
			})
		}

		all := map[string]string{"a": "success", "b": "skipped", "c": "success", "info": "success"}

		// changes の出力を信じると、それが過小なときに検査を省いたまま合格する。
		t.Run("changes の出力が決め直した集合と違えば不合格", func(t *testing.T) {
			t.Parallel()
			_, err := judgePR(judgeDecl(), []string{"a", "c", "info"}, okNeeds(`["a","info"]`, all))
			require.ErrorIs(t, err, errFailed)
		})

		t.Run("changes の出力が JSON でなければ不合格", func(t *testing.T) {
			t.Parallel()
			_, err := judgePR(judgeDecl(), []string{"a", "c", "info"}, okNeeds(``, all))
			require.ErrorIs(t, err, errFailed)
		})

		t.Run("changes の job が success でなければ不合格", func(t *testing.T) {
			t.Parallel()
			n := okNeeds(`["a","c","info"]`, all)
			n[changesJob] = need{Result: "failure"}
			_, err := judgePR(judgeDecl(), []string{"a", "c", "info"}, n)
			require.ErrorIs(t, err, errFailed)
		})

		t.Run("needs に宣言の検査が欠ければ誤りにする", func(t *testing.T) {
			t.Parallel()
			n := okNeeds(`["a","c","info"]`, all)
			delete(n, "b")
			_, err := judgePR(judgeDecl(), []string{"a", "c", "info"}, n)
			require.ErrorIs(t, err, errNeeds)
		})
	})
}

func Test_treeChecks(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("scope が tree で required の検査だけを返す", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, []string{"a", "b"}, treeChecks(judgeDecl()))
		})
	})
}

func Test_judgeLine(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("木の検査がすべて success なら合格", func(t *testing.T) {
			t.Parallel()
			rows, err := judgeLine(judgeDecl(), map[string]need{"a": {Result: "success"}, "b": {Result: "success"}})
			require.NoError(t, err)
			assert.Len(t, rows, 2)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		// 線の検証は path で絞らないので、skipped は走らなかったことを表す。
		t.Run("skipped があれば不合格", func(t *testing.T) {
			t.Parallel()
			_, err := judgeLine(judgeDecl(), map[string]need{"a": {Result: "success"}, "b": {Result: "skipped"}})
			require.ErrorIs(t, err, errFailed)
		})

		t.Run("needs が木の検査と合わなければ誤りにする", func(t *testing.T) {
			t.Parallel()
			_, err := judgeLine(judgeDecl(), map[string]need{"a": {Result: "success"}, "c": {Result: "success"}})
			require.ErrorIs(t, err, errNeeds)
		})

		t.Run("木の検査が宣言に無ければ合格と区別する", func(t *testing.T) {
			t.Parallel()
			d := decl{Checks: []check{{Name: "c", Paths: []string{}, Scope: scopeChange, Required: boolp(true)}}}
			_, err := judgeLine(d, map[string]need{"c": {Result: "success"}})
			require.ErrorIs(t, err, errNoChecks)
		})
	})
}

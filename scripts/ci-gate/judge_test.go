package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/checksdecl"
)

func judgeDecl() checksdecl.Decl {
	return checksdecl.Decl{Checks: []checksdecl.Check{
		{Name: "a", Paths: []string{}, Scope: checksdecl.ScopeTree, Required: boolp(true)},
		{Name: "b", Paths: []string{"b/**"}, Scope: checksdecl.ScopeTree, Required: boolp(true)},
		{Name: "c", Paths: []string{}, Scope: checksdecl.ScopeChange, Required: boolp(true)},
		{Name: "info", Paths: []string{}, Scope: checksdecl.ScopeTree, Required: boolp(false)},
	}}
}

func okNeeds(run string, results map[string]string) map[string]need {
	n := map[string]need{changesJob: {Result: "success", Outputs: map[string]string{"run": run}}}
	for k, v := range results {
		n[callerJob(k)] = need{Result: v}
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
			require.NoError(t, sameSet([]string{"a", "b"}, nil, map[string]need{"b": {}, "a": {}}))
		})

		t.Run("optional の key は在っても無くてもよい", func(t *testing.T) {
			t.Parallel()
			require.NoError(t, sameSet([]string{"a"}, []string{"o"}, map[string]need{"a": {}, "o": {}}))
			require.NoError(t, sameSet([]string{"a"}, []string{"o"}, map[string]need{"a": {}}))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("宣言にあって needs に無い job を誤りにする", func(t *testing.T) {
			t.Parallel()
			err := sameSet([]string{"a", "b"}, nil, map[string]need{"a": {}})
			require.ErrorIs(t, err, errNeeds)
			assert.Contains(t, err.Error(), "needs に無い: [b]")
		})

		t.Run("needs にあって宣言に無い job を誤りにする", func(t *testing.T) {
			t.Parallel()
			err := sameSet([]string{"a"}, nil, map[string]need{"a": {}, "z": {}})
			require.ErrorIs(t, err, errNeeds)
			assert.Contains(t, err.Error(), "宣言に無い: [z]")
		})

		t.Run("optional に無い余分な key は誤りにする", func(t *testing.T) {
			t.Parallel()
			err := sameSet([]string{"a"}, []string{"o"}, map[string]need{"a": {}, "z": {}})
			require.ErrorIs(t, err, errNeeds)
		})
	})
}

func Test_judgePR(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("走るべき検査が success で走るべきでない検査が skipped なら合格", func(t *testing.T) {
			t.Parallel()
			rows, err := judgePR(judgeDecl(), nil, []string{"a", "c", "info"}, okNeeds(`["a","c","info"]`,
				map[string]string{"a": "success", "b": "skipped", "c": "success", "info": "success"}))
			require.NoError(t, err)
			assert.Len(t, rows, 5)
		})

		t.Run("required でない検査の失敗は合否に数えない", func(t *testing.T) {
			t.Parallel()
			rows, err := judgePR(judgeDecl(), nil, []string{"a", "c", "info"}, okNeeds(`["a","c","info"]`,
				map[string]string{"a": "success", "b": "skipped", "c": "success", "info": "failure"}))
			require.NoError(t, err)
			assert.False(t, rows[4].OK)
		})

		t.Run("外した検査は needs に無くてよく、表に載せて合否に数えない", func(t *testing.T) {
			t.Parallel()
			rows, err := judgePR(judgeDecl(), []string{"gone"}, []string{"a", "c", "info"}, okNeeds(`["a","c","info"]`,
				map[string]string{"a": "success", "b": "skipped", "c": "success", "info": "success"}))
			require.NoError(t, err)
			require.Len(t, rows, 6)
			assert.Equal(t, row{Name: "gone", Result: removedResult, OK: true}, rows[5])
		})

		t.Run("外した検査の job が needs に在れば結果を添えて表に載せ、失敗でも合否に数えない", func(t *testing.T) {
			t.Parallel()
			rows, err := judgePR(judgeDecl(), []string{"gone"}, []string{"a", "c", "info"}, okNeeds(`["a","c","info"]`,
				map[string]string{"a": "success", "b": "skipped", "c": "success", "info": "success", "gone": "failure"}))
			require.NoError(t, err)
			assert.Equal(t, removedResult+"（failure）", rows[5].Result)
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
				_, err := judgePR(judgeDecl(), nil, []string{"a", "c", "info"}, okNeeds(`["a","c","info"]`, results))
				require.ErrorIs(t, err, errFailed)
			})
		}

		all := map[string]string{"a": "success", "b": "skipped", "c": "success", "info": "success"}

		// changes の出力を信じると、それが過小なときに検査を省いたまま合格する。
		t.Run("changes の出力が決め直した集合と違えば不合格", func(t *testing.T) {
			t.Parallel()
			_, err := judgePR(judgeDecl(), nil, []string{"a", "c", "info"}, okNeeds(`["a","info"]`, all))
			require.ErrorIs(t, err, errFailed)
		})

		t.Run("changes の出力が JSON でなければ不合格", func(t *testing.T) {
			t.Parallel()
			_, err := judgePR(judgeDecl(), nil, []string{"a", "c", "info"}, okNeeds(``, all))
			require.ErrorIs(t, err, errFailed)
		})

		t.Run("changes の job が success でなければ不合格", func(t *testing.T) {
			t.Parallel()
			n := okNeeds(`["a","c","info"]`, all)
			n[changesJob] = need{Result: "failure"}
			_, err := judgePR(judgeDecl(), nil, []string{"a", "c", "info"}, n)
			require.ErrorIs(t, err, errFailed)
		})

		t.Run("合否に数える検査が無ければ合格と区別する", func(t *testing.T) {
			t.Parallel()
			d := checksdecl.Decl{Checks: []checksdecl.Check{{Name: "info", Paths: []string{}, Scope: checksdecl.ScopeTree, Required: boolp(false)}}}
			_, err := judgePR(d, nil, []string{"info"}, okNeeds(`["info"]`, map[string]string{"info": "success"}))
			require.ErrorIs(t, err, checksdecl.ErrNoChecks)
		})

		t.Run("判定する検査が0件なら合格と区別する", func(t *testing.T) {
			t.Parallel()
			_, err := judgePR(checksdecl.Decl{}, []string{"gone"}, nil, okNeeds(`null`, nil))
			require.ErrorIs(t, err, checksdecl.ErrNoChecks)
		})

		t.Run("どちらの宣言にも無い検査の job が needs に在れば誤りにする", func(t *testing.T) {
			t.Parallel()
			n := okNeeds(`["a","c","info"]`, all)
			n[callerJob("stray")] = need{Result: "skipped"}
			_, err := judgePR(judgeDecl(), []string{"gone"}, []string{"a", "c", "info"}, n)
			require.ErrorIs(t, err, errNeeds)
		})

		t.Run("needs に宣言の検査が欠ければ誤りにする", func(t *testing.T) {
			t.Parallel()
			n := okNeeds(`["a","c","info"]`, all)
			delete(n, callerJob("b"))
			_, err := judgePR(judgeDecl(), nil, []string{"a", "c", "info"}, n)
			require.ErrorIs(t, err, errNeeds)
		})
	})
}

func Test_mergeDecls(t *testing.T) {
	t.Parallel()

	check := func(name string, required bool, paths ...string) checksdecl.Check {
		return checksdecl.Check{Name: name, Paths: append([]string{}, paths...), Scope: checksdecl.ScopeTree, Required: boolp(required)}
	}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("両方に在る検査は base の値で、head にだけ在る検査は head の値で判定する", func(t *testing.T) {
			t.Parallel()
			base := checksdecl.Decl{Checks: []checksdecl.Check{check("kept", true, "a/**"), check("gone", true)}}
			head := checksdecl.Decl{Checks: []checksdecl.Check{check("added", false, "b/**"), check("kept", false)}}
			judged, removed := mergeDecls(base, head)
			assert.Equal(t, []checksdecl.Check{check("kept", true, "a/**"), check("added", false, "b/**")}, judged.Checks)
			assert.Equal(t, []string{"gone"}, removed)
		})

		t.Run("宣言が同じなら base の宣言をそのまま返す", func(t *testing.T) {
			t.Parallel()
			judged, removed := mergeDecls(judgeDecl(), judgeDecl())
			assert.Equal(t, judgeDecl(), judged)
			assert.Empty(t, removed)
		})

		t.Run("head が全部を外せば判定する検査は0件になる", func(t *testing.T) {
			t.Parallel()
			judged, removed := mergeDecls(judgeDecl(), checksdecl.Decl{})
			assert.Empty(t, judged.Checks)
			assert.Equal(t, []string{"a", "b", "c", "info"}, removed)
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

		t.Run("plan_env を持つ検査は線の検証に数えない", func(t *testing.T) {
			t.Parallel()
			d := judgeDecl()
			d.Checks = append(d.Checks, checksdecl.Check{Name: "plan", Paths: []string{}, Scope: checksdecl.ScopeTree, Required: boolp(true), PlanEnv: "admin", Contexts: []string{"plan-admin"}})
			_, err := judgeLine(d, map[string]need{"call-a": {Result: "success"}, "call-b": {Result: "success"}})
			require.NoError(t, err)
		})

		t.Run("木の検査がすべて success なら合格", func(t *testing.T) {
			t.Parallel()
			rows, err := judgeLine(judgeDecl(), map[string]need{"call-a": {Result: "success"}, "call-b": {Result: "success"}})
			require.NoError(t, err)
			assert.Len(t, rows, 2)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		// 線の検証は path で絞らないので、skipped は走らなかったことを表す。
		t.Run("skipped があれば不合格", func(t *testing.T) {
			t.Parallel()
			_, err := judgeLine(judgeDecl(), map[string]need{"call-a": {Result: "success"}, "call-b": {Result: "skipped"}})
			require.ErrorIs(t, err, errFailed)
		})

		t.Run("needs が木の検査と合わなければ誤りにする", func(t *testing.T) {
			t.Parallel()
			_, err := judgeLine(judgeDecl(), map[string]need{"call-a": {Result: "success"}, "call-c": {Result: "success"}})
			require.ErrorIs(t, err, errNeeds)
		})

		t.Run("木の検査が宣言に無ければ合格と区別する", func(t *testing.T) {
			t.Parallel()
			d := checksdecl.Decl{Checks: []checksdecl.Check{{Name: "c", Paths: []string{}, Scope: checksdecl.ScopeChange, Required: boolp(true)}}}
			_, err := judgeLine(d, map[string]need{"call-c": {Result: "success"}})
			require.ErrorIs(t, err, checksdecl.ErrNoChecks)
		})
	})
}

func Test_callerJob(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("検査の名前に接頭辞を付ける", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, "call-go-lint", callerJob("go-lint"))
		})
	})
}

func Test_callerJobs(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("順を保って接頭辞を付ける", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, []string{"call-b", "call-a"}, callerJobs([]string{"b", "a"}))
			assert.Empty(t, callerJobs(nil))
		})
	})
}

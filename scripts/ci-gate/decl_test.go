package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const validDecl = `{"checks": [
  {"name": "go-test", "workflow": "go-test.yaml", "paths": ["**/*.go"], "scope": "tree", "required": true},
  {"name": "commitlint", "workflow": "commitlint.yaml", "paths": [], "scope": "change", "required": true},
  {"name": "trivy-fs", "workflow": "trivy-fs.yaml", "paths": [], "scope": "tree", "required": false}
]}`

func Test_parseDecl(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("宣言の順に検査を読む", func(t *testing.T) {
			t.Parallel()
			d, err := parseDecl([]byte(validDecl))
			require.NoError(t, err)
			assert.Equal(t, []string{"go-test", "commitlint", "trivy-fs"}, d.names())
			assert.True(t, d.Checks[0].required())
			assert.False(t, d.Checks[2].required())
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for name, body := range map[string]string{
			"JSON として読めない":            `{"checks": [`,
			"未知のキー":                   `{"checks": [{"name": "a", "workflow": "a.yaml", "paths": [], "scope": "tree", "required": true, "x": 1}]}`,
			"name が規則に合わない":           `{"checks": [{"name": "A", "workflow": "a.yaml", "paths": [], "scope": "tree", "required": true}]}`,
			"name の重複":                `{"checks": [{"name": "a", "workflow": "a.yaml", "paths": [], "scope": "tree", "required": true}, {"name": "a", "workflow": "b.yaml", "paths": [], "scope": "tree", "required": true}]}`,
			"workflow が yaml でない":     `{"checks": [{"name": "a", "workflow": "a.yml", "paths": [], "scope": "tree", "required": true}]}`,
			"paths が無い":               `{"checks": [{"name": "a", "workflow": "a.yaml", "scope": "tree", "required": true}]}`,
			"scope が未知":               `{"checks": [{"name": "a", "workflow": "a.yaml", "paths": [], "scope": "x", "required": true}]}`,
			"required が無い":            `{"checks": [{"name": "a", "workflow": "a.yaml", "paths": [], "scope": "tree"}]}`,
			"plan_env の形が誤り":          `{"checks": [{"name": "a", "workflow": "a.yaml", "paths": [], "scope": "tree", "required": true, "plan_env": "A B"}]}`,
			"path を解釈できない":            `{"checks": [{"name": "a", "workflow": "a.yaml", "paths": ["!x"], "scope": "tree", "required": true}]}`,
			"plan_env に contexts が無い": `{"checks": [{"name": "a", "workflow": "a.yaml", "paths": [], "scope": "tree", "required": true, "plan_env": "admin"}]}`,
			"plan_env が無いのに contexts": `{"checks": [{"name": "a", "workflow": "a.yaml", "paths": [], "scope": "tree", "required": true, "contexts": ["x"]}]}`,
			"bases が未知":               `{"checks": [{"name": "a", "workflow": "a.yaml", "paths": [], "scope": "tree", "required": true, "bases": ["x"]}]}`,
			"宣言のあとに値がある":              `{"checks": [{"name": "a", "workflow": "a.yaml", "paths": [], "scope": "tree", "required": true}]} {}`,
		} {
			t.Run(name+"なら誤りにする", func(t *testing.T) {
				t.Parallel()
				_, err := parseDecl([]byte(body))
				require.ErrorIs(t, err, errDecl)
			})
		}

		// 検査が0件の宣言を合格として読むと、gate は何も見ずに成功する。
		t.Run("検査が0件なら合格と区別する", func(t *testing.T) {
			t.Parallel()
			_, err := parseDecl([]byte(`{"checks": []}`))
			require.ErrorIs(t, err, errNoChecks)
		})
	})
}

func Test_check_required(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		yes, no := true, false
		t.Run("true のときだけ真を返す", func(t *testing.T) {
			t.Parallel()
			assert.True(t, check{Required: &yes}.required())
			assert.False(t, check{Required: &no}.required())
			assert.False(t, check{}.required())
		})
	})
}

func Test_decl_names(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("宣言の順に名前を返す", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, []string{"b", "a"}, decl{Checks: []check{{Name: "b"}, {Name: "a"}}}.names())
		})
	})
}

func Test_joinNames(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("読点と空白で繋ぐ", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, "a, b", joinNames([]string{"a", "b"}))
			assert.Empty(t, joinNames(nil))
		})
	})
}

func Test_validBases(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("line と deploy の重複の無い配列だけを受け付ける", func(t *testing.T) {
			t.Parallel()
			assert.True(t, validBases([]string{"deploy"}))
			assert.True(t, validBases([]string{"line", "deploy"}))
			assert.False(t, validBases([]string{}))
			assert.False(t, validBases([]string{"line", "line"}))
			assert.False(t, validBases([]string{"x"}))
		})
	})
}

func Test_check_runsOn(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("bases が無ければどの役割でも走り、あれば含む役割でだけ走る", func(t *testing.T) {
			t.Parallel()
			assert.True(t, check{}.runsOn(baseLine))
			assert.True(t, check{}.runsOn(baseDeploy))
			assert.True(t, check{Bases: []string{baseDeploy}}.runsOn(baseDeploy))
			assert.False(t, check{Bases: []string{baseDeploy}}.runsOn(baseLine))
		})
	})
}

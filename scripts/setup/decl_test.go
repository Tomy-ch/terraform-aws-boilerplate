package main

import (
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_variableBlock(t *testing.T) {
	t.Parallel()

	t.Run("ただ1つの variable を返す", func(t *testing.T) {
		t.Parallel()

		b, err := variableBlock(parseBody(t, "variable \"env\" {}\nvariable \"project\" {}\n"), "env")
		require.NoError(t, err)
		assert.Equal(t, []string{"env"}, b.Labels)
	})

	for name, src := range map[string]string{
		"無ければ":          "variable \"project\" {}\n",
		"2つ在れば":         "variable \"env\" {}\nvariable \"env\" {}\n",
		"locals の中は数えず": "locals {\n  env = 1\n}\n",
	} {
		t.Run(name+" errDeclShape", func(t *testing.T) {
			t.Parallel()

			_, err := variableBlock(parseBody(t, src), "env")
			require.ErrorIs(t, err, errDeclShape)
		})
	}
}

func Test_regexCalls(t *testing.T) {
	t.Parallel()

	t.Run("入れ子の regex をすべて、正規表現と対象とで返す", func(t *testing.T) {
		t.Parallel()

		calls, err := regexCalls(parseBody(t, `x = can(regex("^a$", var.a)) && alltrue([for v in var.b : can(regex("^b\\.c$", v))])`))
		require.NoError(t, err)
		require.Len(t, calls, 2)
		assert.Equal(t, "^a$", calls[0].pattern.String())
		assert.Equal(t, `^b\.c$`, calls[1].pattern.String())
		assert.True(t, calls[1].pattern.MatchString("b.c"))
		assert.False(t, calls[1].pattern.MatchString("bxc"))
	})

	t.Run("regex が無ければ0件で、エラーにしない（件数は呼び出し側が見る）", func(t *testing.T) {
		t.Parallel()

		calls, err := regexCalls(parseBody(t, `x = contains(["a"], var.a)`))
		require.NoError(t, err)
		assert.Empty(t, calls)
	})

	for name, src := range map[string]string{
		"正規表現がリテラルでなければ": `x = regex(var.p, var.a)`,
		"引数が1つなら":        `x = regex("^a$")`,
		"Go で読めない正規表現なら": `x = regex("(?<=a)b", var.a)`,
	} {
		t.Run(name+" errDeclShape", func(t *testing.T) {
			t.Parallel()

			_, err := regexCalls(parseBody(t, src))
			require.ErrorIs(t, err, errDeclShape)
		})
	}
}

func Test_literalListArg(t *testing.T) {
	t.Parallel()

	t.Run("ただ1つの呼び出しの引数のリストを返す", func(t *testing.T) {
		t.Parallel()

		got, err := literalListArg(parseBody(t, `x = length(setsubtract(keys(var.d), ["dev", "stg"])) == 0`), "setsubtract", 1)
		require.NoError(t, err)
		assert.Equal(t, []string{"dev", "stg"}, got)
	})

	for name, src := range map[string]string{
		"呼び出しが無ければ":    `x = 1`,
		"呼び出しが2つなら":    `x = contains(["a"], var.a) && contains(["b"], var.a)`,
		"引数が無ければ":      `x = contains()`,
		"リテラルでなければ":    `x = contains(var.l, var.a)`,
		"空のリストなら":      `x = contains([], var.a)`,
		"文字列でない要素が在れば": `x = contains(["a", 1], var.a)`,
		"空の文字列が在れば":    `x = contains(["a", ""], var.a)`,
		"重複が在れば":       `x = contains(["a", "a"], var.a)`,
		"リストでなく文字列なら":  `x = contains("a", var.a)`,
	} {
		t.Run(name+" errDeclShape", func(t *testing.T) {
			t.Parallel()

			_, err := literalListArg(parseBody(t, src), "contains", 0)
			require.ErrorIs(t, err, errDeclShape)
		})
	}

	t.Run("index が引数の数を超えれば errDeclShape", func(t *testing.T) {
		t.Parallel()

		_, err := literalListArg(parseBody(t, `x = contains(["a"], var.a)`), "contains", 2)
		require.ErrorIs(t, err, errDeclShape)
	})
}

const envVariableTF = `variable "env" {
  type = string

  validation {
    condition     = contains(["dev", "stg", "prd"], var.env)
    error_message = "x"
  }
}
`

func Test_allowedEnvs(t *testing.T) {
	t.Parallel()

	t.Run("variable env の contains の一覧を返す", func(t *testing.T) {
		t.Parallel()

		got, err := allowedEnvs(parseBody(t, envVariableTF))
		require.NoError(t, err)
		assert.Equal(t, []string{"dev", "stg", "prd"}, got)
	})

	t.Run("variable env が無ければ errDeclShape", func(t *testing.T) {
		t.Parallel()

		_, err := allowedEnvs(parseBody(t, "variable \"project\" {}\n"))
		require.ErrorIs(t, err, errDeclShape)
	})

	t.Run("validation の contains が無ければ errDeclShape", func(t *testing.T) {
		t.Parallel()

		_, err := allowedEnvs(parseBody(t, "variable \"env\" {\n  type = string\n}\n"))
		require.ErrorIs(t, err, errDeclShape)
	})
}

func Test_requireEnv(t *testing.T) {
	t.Parallel()

	t.Run("宣言の一覧に在れば通す", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, requireEnv(parseBody(t, envVariableTF), "stg"))
	})

	t.Run("一覧に無ければ errEnvName で、宣言の一覧を添える", func(t *testing.T) {
		t.Parallel()

		err := requireEnv(parseBody(t, envVariableTF), "sandbox")
		require.ErrorIs(t, err, errEnvName)
		assert.Contains(t, err.Error(), "sandbox（宣言: dev・stg・prd）")
	})

	t.Run("宣言を読めなければ errDeclShape", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, requireEnv(parseBody(t, ""), "dev"), errDeclShape)
	})
}

func Test_evalLocals(t *testing.T) {
	t.Parallel()

	src := `locals {
  prefix = "${var.project}-${var.env}"
  name   = "${local.prefix}-x"
  other  = data.x.y
}
`

	t.Run("var と前の local を使って順に評価する", func(t *testing.T) {
		t.Parallel()

		got, ctx, err := evalLocals(parseBody(t, src), []string{"prefix", "name"}, map[string]string{"project": "tabp", "env": "dev"})
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"prefix": "tabp-dev", "name": "tabp-dev-x"}, got)
		assert.Equal(t, "tabp-dev", ctx.Variables["local"].GetAttr("prefix").AsString())
	})

	t.Run("後の local を先に評価すると errDeclShape", func(t *testing.T) {
		t.Parallel()

		_, _, err := evalLocals(parseBody(t, src), []string{"name", "prefix"}, map[string]string{"project": "tabp", "env": "dev"})
		require.ErrorIs(t, err, errDeclShape)
	})

	t.Run("var と local のほかを参照すると errDeclShape", func(t *testing.T) {
		t.Parallel()

		_, _, err := evalLocals(parseBody(t, src), []string{"other"}, nil)
		require.ErrorIs(t, err, errDeclShape)
	})

	t.Run("渡していない var を参照すると errDeclShape", func(t *testing.T) {
		t.Parallel()

		_, _, err := evalLocals(parseBody(t, src), []string{"prefix"}, map[string]string{"project": "tabp"})
		require.ErrorIs(t, err, errDeclShape)
	})

	t.Run("文字列でない local は errDeclShape", func(t *testing.T) {
		t.Parallel()

		_, _, err := evalLocals(parseBody(t, "locals {\n  n = 1\n}\n"), []string{"n"}, nil)
		require.ErrorIs(t, err, errDeclShape)
	})

	t.Run("空の文字列の local は errDeclShape", func(t *testing.T) {
		t.Parallel()

		_, _, err := evalLocals(parseBody(t, "locals {\n  n = \"\"\n}\n"), []string{"n"}, nil)
		require.ErrorIs(t, err, errDeclShape)
	})

	t.Run("無い local は errDeclShape", func(t *testing.T) {
		t.Parallel()

		_, _, err := evalLocals(parseBody(t, src), []string{"nope"}, nil)
		require.ErrorIs(t, err, errDeclShape)
	})

	t.Run("null の local は errDeclShape", func(t *testing.T) {
		t.Parallel()

		_, _, err := evalLocals(parseBody(t, "locals {\n  n = null\n}\n"), []string{"n"}, nil)
		require.ErrorIs(t, err, errDeclShape)
	})
}

func Test_localItemString(t *testing.T) {
	t.Parallel()

	src := `locals {
  prefix = "${var.project}-dev"
  args = {
    name  = "${local.prefix}-chat"
    slack = var.chat.slack
  }
  dup = {
    name = "a"
    name = "b"
  }
  notobj = "x"
  num = {
    name = 1
  }
}
`

	ctx := func(t *testing.T) *hcl.EvalContext {
		t.Helper()

		_, c, err := evalLocals(parseBody(t, src), []string{"prefix"}, map[string]string{"project": "tabp"})
		require.NoError(t, err)

		return c
	}

	t.Run("object の項目を評価する。ほかの項目は評価しない", func(t *testing.T) {
		t.Parallel()

		got, err := localItemString(parseBody(t, src), "args", "name", ctx(t))
		require.NoError(t, err)
		assert.Equal(t, "tabp-dev-chat", got)
	})

	for name, tc := range map[string]struct{ local, key string }{
		"項目が無ければ":      {"args", "nope"},
		"項目が2つ在れば":     {"dup", "name"},
		"object でなければ": {"notobj", "name"},
		"文字列でなければ":     {"num", "name"},
		"local が無ければ":  {"nope", "name"},
		"文脈に無い参照を含めば":  {"args", "slack"},
	} {
		t.Run(name+" errDeclShape", func(t *testing.T) {
			t.Parallel()

			_, err := localItemString(parseBody(t, src), tc.local, tc.key, ctx(t))
			require.ErrorIs(t, err, errDeclShape)
		})
	}
}

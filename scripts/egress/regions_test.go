package main

import (
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// evalSetup は files を一時ディレクトリへ書き、r を root とした評価器と、dir の scope を返す。
func evalSetup(t *testing.T, files map[string]string, dir string) (*regionEvaluator, *scope) {
	t.Helper()
	repo := t.TempDir()
	for rel, body := range files {
		writeFile(t, repo, rel, body)
	}
	w := newWalker(openRoot(t, repo), "r")
	m, err := w.load(dir)
	require.NoError(t, err)
	return &w.region, &scope{mod: m}
}

// fileBody は src のファイルの本体を返す。
func fileBody(t *testing.T, src string) *hclsyntax.Body {
	t.Helper()
	f, diags := hclsyntax.ParseConfig([]byte(src), "t.tf", hcl.InitialPos)
	require.False(t, diags.HasErrors(), diags.Error())
	body, ok := f.Body.(*hclsyntax.Body)
	require.True(t, ok)
	return body
}

func parseExpr(t *testing.T, src string) hclsyntax.Expression {
	t.Helper()
	expr, diags := hclsyntax.ParseExpression([]byte(src), "e.tf", hcl.InitialPos)
	require.False(t, diags.HasErrors(), diags.Error())
	return expr
}

func evalSet(t *testing.T, ev *regionEvaluator, sc *scope, src string) ([]string, error) {
	t.Helper()
	v, err := ev.eval(parseExpr(t, src), sc)
	if err != nil {
		return nil, err
	}
	if v.isMap() {
		return sortedKeys(v.entries), nil
	}
	return sortedKeys(v.set), nil
}

const providerTF = "provider \"aws\" {\n  region = \"ap-northeast-1\"\n}\n"

func Test_readModuleInfo(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("locals・output の value・module の引数（meta の引数を除く）を集める", func(t *testing.T) {
			t.Parallel()
			body := fileBody(t, "locals {\n  a = 1\n}\noutput \"o\" {\n  value = 2\n}\noutput \"novalue\" {}\n"+
				"module \"m\" {\n  source   = \"./m\"\n  count    = 1\n  regions  = [\"us-east-1\"]\n  depends_on = []\n}\n")
			m, err := readModuleInfo("r", []*hclsyntax.Body{body})
			require.NoError(t, err)
			assert.Equal(t, "r", m.dir)
			assert.Equal(t, []string{"a"}, sortedKeys(m.locals))
			assert.Equal(t, []string{"o"}, sortedKeys(m.outputs))
			require.Contains(t, m.calls, "m")
			assert.Equal(t, "./m", m.calls["m"].source)
			assert.Equal(t, []string{"regions"}, sortedKeys(m.calls["m"].args))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("module の source がリテラルの相対パスでなければ弾く", func(t *testing.T) {
			t.Parallel()
			body := fileBody(t, "module \"m\" {\n  source = var.s\n}\n")
			_, err := readModuleInfo("r", []*hclsyntax.Body{body})
			require.ErrorIs(t, err, errModuleSource)
		})
	})
}

func Test_isEachKey(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("each.key だけを真とする", func(t *testing.T) {
			t.Parallel()
			assert.True(t, isEachKey(parseExpr(t, "each.key")))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for name, src := range map[string]string{
			"each.value":   "each.value",
			"each だけ":      "each",
			"each.key の添字": "each.key[0]",
			"var.key":      "var.key",
			"文字列":          `"each.key"`,
		} {
			t.Run(name+"は偽", func(t *testing.T) {
				t.Parallel()
				assert.False(t, isEachKey(parseExpr(t, src)))
			})
		}
	})
}

func Test_regionEvaluator_forEachRegions(t *testing.T) {
	t.Parallel()

	block := func(t *testing.T, src string) *hclsyntax.Block {
		t.Helper()
		return fileBody(t, src).Blocks[0]
	}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("set の要素を整列して返す", func(t *testing.T) {
			t.Parallel()
			ev, sc := evalSetup(t, map[string]string{"r/main.tf": providerTF}, "r")
			got, err := ev.forEachRegions(block(t, "resource \"aws_a\" \"x\" {\n  for_each = toset([\"us-west-2\", \"eu-west-1\"])\n}\n"), sc)
			require.NoError(t, err)
			assert.Equal(t, []string{"eu-west-1", "us-west-2"}, got)
		})

		t.Run("map なら key を返す", func(t *testing.T) {
			t.Parallel()
			ev, sc := evalSetup(t, map[string]string{"r/main.tf": providerTF}, "r")
			got, err := ev.forEachRegions(block(t, "resource \"aws_a\" \"x\" {\n  for_each = { \"us-east-1\" = 1, \"eu-west-1\" = 2 }\n}\n"), sc)
			require.NoError(t, err)
			assert.Equal(t, []string{"eu-west-1", "us-east-1"}, got)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for name, tc := range map[string]struct{ src, msg string }{
			"for_each が無い":       {"resource \"aws_a\" \"x\" {}\n", "for_each が無い"},
			"空の集合":               {"resource \"aws_a\" \"x\" {\n  for_each = toset([])\n}\n", "空の集合"},
			"空の map":             {"resource \"aws_a\" \"x\" {\n  for_each = {}\n}\n", "空の集合"},
			"toset で包まない tuple":  {"resource \"aws_a\" \"x\" {\n  for_each = ([\"us-east-1\"])\n}\n", "toset で包んで"},
			"region の形でない要素":     {"resource \"aws_a\" \"x\" {\n  for_each = toset([\"us-east-1\", \"global\"])\n}\n", "region の形ではありません"},
			"region の形でない key":   {"resource \"aws_a\" \"x\" {\n  for_each = { a = 1 }\n}\n", "region の形ではありません"},
			"解けない式（data）":        {"resource \"aws_a\" \"x\" {\n  for_each = data.aws_regions.x.names\n}\n", "data の参照は解けません"},
			"解けない式（条件式）":         {"resource \"aws_a\" \"x\" {\n  for_each = true ? toset([\"us-east-1\"]) : toset([])\n}\n", "の式は解けません"},
			"解けない式（for 式）":       {"resource \"aws_a\" \"x\" {\n  for_each = toset([for r in [\"us-east-1\"] : r])\n}\n", "の式は解けません"},
			"解けない式（setsubtract）": {"resource \"aws_a\" \"x\" {\n  for_each = setsubtract([\"us-east-1\"], [])\n}\n", "関数 setsubtract は解けません"},
		} {
			t.Run(name+"を弾く", func(t *testing.T) {
				t.Parallel()
				ev, sc := evalSetup(t, map[string]string{"r/main.tf": providerTF}, "r")
				_, err := ev.forEachRegions(block(t, tc.src), sc)
				require.ErrorIs(t, err, errRegionSet)
				assert.Contains(t, err.Error(), tc.msg)
			})
		}
	})
}

func Test_regionEvaluator_eval(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		for name, tc := range map[string]struct {
			src  string
			want []string
		}{
			"tuple の文字列":         {`["us-east-1", "eu-west-1"]`, []string{"eu-west-1", "us-east-1"}},
			"toset と括弧":          {`(toset(["us-east-1"]))`, []string{"us-east-1"}},
			"object の key":       {`{ "us-east-1" = 1, eu-west-1 = 2 }`, []string{"eu-west-1", "us-east-1"}},
			"local":              {`local.regions`, []string{"us-east-1"}},
			"local の map の添字":    {`local.by["aws"]`, []string{"us-east-1"}},
			"local の map の属性":    {`local.by.aws`, []string{"us-east-1"}},
			"partition の添字":      {`local.by[data.aws_partition.current.partition]`, []string{"us-east-1"}},
			"index 式の partition": {`(local.by)[data.aws_partition.current.partition]`, []string{"us-east-1"}},
		} {
			t.Run(name+"を解く", func(t *testing.T) {
				t.Parallel()
				ev, sc := evalSetup(t, map[string]string{
					"r/main.tf": "provider \"aws\" {\n  region = \"us-east-1\"\n}\nlocals {\n  regions = toset([\"us-east-1\"])\n" +
						"  by = { aws = toset([\"us-east-1\"]), aws-us-gov = toset([\"us-gov-west-1\"]) }\n}\n",
				}, "r")
				got, err := evalSet(t, ev, sc, tc.src)
				require.NoError(t, err)
				assert.Equal(t, tc.want, got)
			})
		}
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for name, tc := range map[string]struct{ src, msg string }{
			"toset 以外の関数":        {`keys({ "us-east-1" = 1 })`, "関数 keys は解けません"},
			"toset の引数が2つ":       {`toset(["us-east-1"], ["eu-west-1"])`, "関数 toset は解けません"},
			"toset の展開":          {`toset(local.list...)`, "関数 toset は解けません"},
			"toset の引数が map":     {`toset({ a = 1 })`, "toset の引数が map です"},
			"要素がリテラルでない":         {`[local.x]`, "要素が文字列のリテラルではありません"},
			"key が式の object":     {`{ (local.k) = 1 }`, "key がリテラルではありません"},
			"each.value":         {`each.value`, "each の参照は解けません"},
			"テンプレート":             {`"${local.x}"`, "の式は解けません"},
			"関数の値の添字":            {`toset(["us-east-1"])["us-east-1"]`, "の式は解けません"},
			"添字を取る値が set":        {`local.s[data.aws_partition.current.partition]`, "map ではありません"},
			"添字を取る値を解けない":        {`local.nope[data.aws_partition.current.partition]`, "local.nope がありません"},
			"添字が式":               {`local.by[local.k]`, "data.aws_partition の partition ではありません"},
			"partition 以外の data": {`local.by[data.aws_region.current.region]`, "data.aws_partition の partition ではありません"},
		} {
			t.Run(name+"を弾く", func(t *testing.T) {
				t.Parallel()
				ev, sc := evalSetup(t, map[string]string{
					"r/main.tf": providerTF + "locals {\n  by = { aws = [\"us-east-1\"] }\n  k = \"aws\"\n  list = []\n  x = \"us-east-1\"\n  s = toset([\"us-east-1\"])\n}\n",
				}, "r")
				_, err := ev.eval(parseExpr(t, tc.src), sc)
				require.ErrorIs(t, err, errRegionSet)
				assert.Contains(t, err.Error(), tc.msg)
			})
		}
	})
}

func Test_regionEvaluator_traversal(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("参照の後の添字と属性を map の key として当てる", func(t *testing.T) {
			t.Parallel()
			ev, sc := evalSetup(t, map[string]string{"r/main.tf": "locals {\n  m = { a = { b = [\"us-east-1\"] } }\n}\n"}, "r")
			got, err := evalSet(t, ev, sc, `local.m.a["b"]`)
			require.NoError(t, err)
			assert.Equal(t, []string{"us-east-1"}, got)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for name, tc := range map[string]struct{ src, msg string }{
			"local の名前が無い":          {`local`, "local の名前を読めません"},
			"var の名前が無い":            {`var`, "var の名前を読めません"},
			"module の出力の名前が無い":      {`module.m`, "module の出力の参照を読めません"},
			"count の module の添字の出力": {`module.m[0].o`, "module の出力の参照を読めません"},
			"知らない root":             {`path.module`, "path の参照は解けません"},
			"数の添字":                  {`local.m[0]`, "添字が文字列ではありません"},
			"splat":                 {`local.m[*]`, "の式は解けません"},
			"ない key":                {`local.m.nope`, "key nope がありません"},
			"set の属性":               {`local.s.a`, "map ではありません"},
		} {
			t.Run(name+"を弾く", func(t *testing.T) {
				t.Parallel()
				ev, sc := evalSetup(t, map[string]string{"r/main.tf": "locals {\n  m = { a = [\"us-east-1\"] }\n  s = [\"us-east-1\"]\n}\n"}, "r")
				_, err := ev.eval(parseExpr(t, tc.src), sc)
				require.ErrorIs(t, err, errRegionSet)
				assert.Contains(t, err.Error(), tc.msg)
			})
		}
	})
}

func Test_regionEvaluator_local(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("別の local を辿って解く", func(t *testing.T) {
			t.Parallel()
			ev, sc := evalSetup(t, map[string]string{"r/main.tf": "locals {\n  a = local.b\n  b = [\"us-east-1\"]\n}\n"}, "r")
			v, err := ev.local(sc, "a", "at")
			require.NoError(t, err)
			assert.Equal(t, map[string]bool{"us-east-1": true}, v.set)
			assert.Empty(t, ev.active)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("無い local を弾く", func(t *testing.T) {
			t.Parallel()
			ev, sc := evalSetup(t, map[string]string{"r/main.tf": "locals {\n  a = 1\n}\n"}, "r")
			_, err := ev.local(sc, "nope", "at")
			require.ErrorIs(t, err, errRegionSet)
		})

		t.Run("循環を弾く", func(t *testing.T) {
			t.Parallel()
			ev, sc := evalSetup(t, map[string]string{"r/main.tf": "locals {\n  a = local.b\n  b = local.a\n}\n"}, "r")
			_, err := ev.local(sc, "a", "at")
			require.ErrorIs(t, err, errRegionSet)
			assert.Contains(t, err.Error(), "循環")
		})
	})
}

func Test_regionEvaluator_variable(t *testing.T) {
	t.Parallel()

	files := map[string]string{
		"r/main.tf":   "locals {\n  rs = [\"eu-west-1\"]\n}\nmodule \"m\" {\n  source  = \"./m\"\n  regions = local.rs\n}\n",
		"r/m/main.tf": "variable \"regions\" {}\nvariable \"other\" {\n  default = [\"us-east-1\"]\n}\n",
	}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("呼び手の引数を呼び手の文脈で解く", func(t *testing.T) {
			t.Parallel()
			ev, root := evalSetup(t, files, "r")
			child, err := ev.load("r/m")
			require.NoError(t, err)
			v, err := ev.variable(&scope{mod: child, args: root.mod.calls["m"].args, caller: root}, "regions", "at")
			require.NoError(t, err)
			assert.Equal(t, map[string]bool{"eu-west-1": true}, v.set)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("呼び手が渡さない var は既定値を読まずに弾く", func(t *testing.T) {
			t.Parallel()
			ev, root := evalSetup(t, files, "r")
			child, err := ev.load("r/m")
			require.NoError(t, err)
			_, err = ev.variable(&scope{mod: child, args: root.mod.calls["m"].args, caller: root}, "other", "at")
			require.ErrorIs(t, err, errRegionSet)
		})

		t.Run("root の var を弾く", func(t *testing.T) {
			t.Parallel()
			ev, root := evalSetup(t, files, "r")
			_, err := ev.variable(root, "regions", "at")
			require.ErrorIs(t, err, errRegionSet)
		})
	})
}

func Test_regionEvaluator_output(t *testing.T) {
	t.Parallel()

	files := map[string]string{
		"r/main.tf":   providerTF + "module \"d\" {\n  source = \"./d\"\n}\nmodule \"p\" {\n  source = \"./p\"\n  rs     = [\"eu-west-1\"]\n}\n",
		"r/d/main.tf": "output \"by_partition\" {\n  value = { aws = toset([\"ap-northeast-1\", \"us-east-1\"]) }\n}\n",
		"r/p/main.tf": "variable \"rs\" {}\noutput \"o\" {\n  value = var.rs\n}\n",
	}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("module の出力を、その module の文脈で解く", func(t *testing.T) {
			t.Parallel()
			ev, sc := evalSetup(t, files, "r")
			got, err := evalSet(t, ev, sc, "module.d.by_partition[data.aws_partition.current.partition]")
			require.NoError(t, err)
			assert.Equal(t, []string{"ap-northeast-1", "us-east-1"}, got)
		})

		t.Run("出力が var を指すなら、呼び出しの引数を呼び手の文脈で解く", func(t *testing.T) {
			t.Parallel()
			ev, sc := evalSetup(t, files, "r")
			v, err := ev.output(sc, "p", "o", "at")
			require.NoError(t, err)
			assert.Equal(t, map[string]bool{"eu-west-1": true}, v.set)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("無い module を弾く", func(t *testing.T) {
			t.Parallel()
			ev, sc := evalSetup(t, files, "r")
			_, err := ev.output(sc, "nope", "o", "at")
			require.ErrorIs(t, err, errRegionSet)
		})

		t.Run("無い出力を弾く", func(t *testing.T) {
			t.Parallel()
			ev, sc := evalSetup(t, files, "r")
			_, err := ev.output(sc, "d", "nope", "at")
			require.ErrorIs(t, err, errRegionSet)
		})

		t.Run("module の出力が循環すれば、再帰を打ち切って弾く", func(t *testing.T) {
			t.Parallel()
			ev, sc := evalSetup(t, map[string]string{
				"r/main.tf":   "module \"m\" {\n  source = \"./c\"\n}\n",
				"r/c/main.tf": "module \"s\" {\n  source = \"../c\"\n}\noutput \"o\" {\n  value = module.s.o\n}\n",
			}, "r")
			_, err := ev.output(sc, "m", "o", "at")
			require.ErrorIs(t, err, errRegionSet)
			assert.Contains(t, err.Error(), "循環")
			assert.Empty(t, ev.active)
		})

		t.Run("呼ばれる module を読めなければ弾く", func(t *testing.T) {
			t.Parallel()
			ev, sc := evalSetup(t, map[string]string{"r/main.tf": "module \"x\" {\n  source = \"./x\"\n}\n"}, "r")
			_, err := ev.output(sc, "x", "o", "at")
			require.ErrorIs(t, err, errNoTerraform)
		})
	})
}

func Test_regionEvaluator_index(t *testing.T) {
	t.Parallel()

	const byPartition = "locals {\n  by = { aws = toset([\"us-east-1\", \"ap-northeast-1\"]), aws-us-gov = toset([\"us-gov-west-1\"]) }\n" +
		"  dup = { a = [\"us-east-1\"], b = [\"us-east-1\"] }\n  nested = { aws = { x = 1 } }\n}\n"

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("partition の添字は provider の region を含む entry を選ぶ", func(t *testing.T) {
			t.Parallel()
			ev, sc := evalSetup(t, map[string]string{"r/main.tf": providerTF + byPartition}, "r")
			got, err := evalSet(t, ev, sc, "local.by[data.aws_partition.current.partition]")
			require.NoError(t, err)
			assert.Equal(t, []string{"ap-northeast-1", "us-east-1"}, got)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("provider の region を含む entry が無ければ弾く", func(t *testing.T) {
			t.Parallel()
			ev, sc := evalSetup(t, map[string]string{"r/main.tf": "provider \"aws\" {\n  region = \"eu-central-1\"\n}\n" + byPartition}, "r")
			_, err := evalSet(t, ev, sc, "local.by[data.aws_partition.current.partition]")
			require.ErrorIs(t, err, errRegionSet)
			assert.Contains(t, err.Error(), "0 個")
		})

		t.Run("provider の region を含む entry が 2 つなら弾く", func(t *testing.T) {
			t.Parallel()
			ev, sc := evalSetup(t, map[string]string{"r/main.tf": "provider \"aws\" {\n  region = \"us-east-1\"\n}\n" + byPartition}, "r")
			_, err := evalSet(t, ev, sc, "local.dup[data.aws_partition.current.partition]")
			require.ErrorIs(t, err, errRegionSet)
			assert.Contains(t, err.Error(), "2 個")
		})

		t.Run("entry が map なら選ばない", func(t *testing.T) {
			t.Parallel()
			ev, sc := evalSetup(t, map[string]string{"r/main.tf": providerTF + byPartition}, "r")
			_, err := evalSet(t, ev, sc, "local.nested[data.aws_partition.current.partition]")
			require.ErrorIs(t, err, errRegionSet)
		})

		t.Run("root の provider を読めなければ弾く", func(t *testing.T) {
			t.Parallel()
			ev, sc := evalSetup(t, map[string]string{"r/main.tf": byPartition}, "r")
			_, err := evalSet(t, ev, sc, "local.by[data.aws_partition.current.partition]")
			require.ErrorIs(t, err, errRegionUnknown)
		})

		t.Run("entry の値を解けなければ弾く", func(t *testing.T) {
			t.Parallel()
			ev, sc := evalSetup(t, map[string]string{"r/main.tf": providerTF + "locals {\n  bad = { aws = data.x.y }\n}\n"}, "r")
			_, err := evalSet(t, ev, sc, "local.bad[data.aws_partition.current.partition]")
			require.ErrorIs(t, err, errRegionSet)
		})
	})
}

func Test_regionEvaluator_entry(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("map の key の値を、map の文脈で解く", func(t *testing.T) {
			t.Parallel()
			ev, sc := evalSetup(t, map[string]string{"r/main.tf": "locals {\n  x = [\"us-east-1\"]\n}\n"}, "r")
			v := regionValue{entries: map[string]hclsyntax.Expression{"k": parseExpr(t, "local.x")}, scope: sc}
			got, err := ev.entry(v, "k", "at")
			require.NoError(t, err)
			assert.Equal(t, map[string]bool{"us-east-1": true}, got.set)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("set の key を弾く", func(t *testing.T) {
			t.Parallel()
			ev, _ := evalSetup(t, map[string]string{"r/main.tf": "locals {}\n"}, "r")
			_, err := ev.entry(regionValue{set: map[string]bool{"k": true}}, "k", "at")
			require.ErrorIs(t, err, errRegionSet)
		})

		t.Run("無い key を弾く", func(t *testing.T) {
			t.Parallel()
			ev, sc := evalSetup(t, map[string]string{"r/main.tf": "locals {}\n"}, "r")
			_, err := ev.entry(regionValue{entries: map[string]hclsyntax.Expression{}, scope: sc}, "k", "at")
			require.ErrorIs(t, err, errRegionSet)
		})
	})
}

func Test_regionValue_isMap(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("entries を持つ値だけを map とする", func(t *testing.T) {
			t.Parallel()
			assert.True(t, regionValue{entries: map[string]hclsyntax.Expression{}}.isMap())
			assert.False(t, regionValue{set: map[string]bool{}}.isMap())
		})
	})
}

func Test_isPartitionRef(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("data.aws_partition.<name>.partition を真とする", func(t *testing.T) {
			t.Parallel()
			assert.True(t, isPartitionRef(parseExpr(t, "data.aws_partition.current.partition")))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for name, src := range map[string]string{
			"別の data":  "data.aws_region.current.region",
			"別の属性":     "data.aws_partition.current.dns_suffix",
			"data でない": "local.aws_partition.current.partition",
			"短い参照":     "data.aws_partition.current",
			"添字の参照":    `data.aws_partition["current"].partition`,
			"文字列":      `"aws"`,
		} {
			t.Run(name+"は偽", func(t *testing.T) {
				t.Parallel()
				assert.False(t, isPartitionRef(parseExpr(t, src)))
			})
		}
	})
}

func Test_literalString(t *testing.T) {
	t.Parallel()

	key := func(t *testing.T, src string) hclsyntax.Expression {
		t.Helper()
		obj, ok := parseExpr(t, "{ "+src+" = 1 }").(*hclsyntax.ObjectConsExpr)
		require.True(t, ok)
		return obj.Items[0].KeyExpr
	}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("文字列のリテラルを返す", func(t *testing.T) {
			t.Parallel()
			s, ok := literalString(parseExpr(t, `"us-east-1"`))
			require.True(t, ok)
			assert.Equal(t, "us-east-1", s)
		})

		t.Run("object の裸の key を名前として返す", func(t *testing.T) {
			t.Parallel()
			s, ok := literalString(key(t, "aws-us-gov"))
			require.True(t, ok)
			assert.Equal(t, "aws-us-gov", s)
		})

		t.Run("object の引用符の key を返す", func(t *testing.T) {
			t.Parallel()
			s, ok := literalString(key(t, `"us-east-1"`))
			require.True(t, ok)
			assert.Equal(t, "us-east-1", s)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for name, expr := range map[string]func(t *testing.T) hclsyntax.Expression{
			"変数を参照する":    func(t *testing.T) hclsyntax.Expression { return parseExpr(t, "local.x") },
			"数":          func(t *testing.T) hclsyntax.Expression { return parseExpr(t, "1") },
			"null":       func(t *testing.T) hclsyntax.Expression { return parseExpr(t, "null") },
			"括弧の key（式）": func(t *testing.T) hclsyntax.Expression { return key(t, "(local.k)") },
			"テンプレートの補間":  func(t *testing.T) hclsyntax.Expression { return parseExpr(t, `"${local.x}"`) },
		} {
			t.Run(name+"は文字列のリテラルとしない", func(t *testing.T) {
				t.Parallel()
				_, ok := literalString(expr(t))
				assert.False(t, ok)
			})
		}
	})
}

func Test_joinModule(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		for name, tc := range map[string]struct{ dir, source, want string }{
			"下へ":  {"usecases/a", "./internal/b", "usecases/a/internal/b"},
			"上へ":  {"usecases/a/reference", "../", "usecases/a"},
			"兄弟へ": {"usecases/a", "../../modules/m", "modules/m"},
		} {
			t.Run(name+"の source を正規化して繋ぐ", func(t *testing.T) {
				t.Parallel()
				assert.Equal(t, tc.want, joinModule(tc.dir, tc.source))
			})
		}
	})
}

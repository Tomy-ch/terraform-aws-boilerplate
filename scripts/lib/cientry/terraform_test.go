package cientry

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func roleRow(key, nameRole, envRole string) string {
	return "    " + key + ` = { name = "${var.name_prefix}-ci-` + nameRole + `", environment = "${var.env}-` + envRole + `" }` + "\n"
}

func rolesSrc(rows ...string) string {
	return "locals {\n  issuer = \"x\"\n\n  roles = {\n" + strings.Join(rows, "") + "  }\n}\n"
}

// registryRows のキーは role の `-` を `_` にした形。
func registryRows() []string {
	var rows []string
	for _, role := range Roles() {
		rows = append(rows, roleRow(strings.ReplaceAll(role, "-", "_"), role, role))
	}
	return rows
}

func registryRowsWithout(role string) []string {
	return slices.DeleteFunc(registryRows(), func(r string) bool {
		return strings.Contains(r, `"${var.env}-`+role+`"`)
	})
}

func TestParseTerraformRoles(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("行を書かれた順で読み、role を environment の接尾辞から取る", func(t *testing.T) {
			t.Parallel()
			got, err := ParseTerraformRoles([]byte(rolesSrc(roleRow("apply_identity", "apply-identity", "apply-identity"), roleRow("plan", "plan", "plan"))))
			require.NoError(t, err)
			assert.Equal(t, []TerraformRole{
				{Key: "apply_identity", Role: "apply-identity", Name: "${var.name_prefix}-ci-apply-identity", Environment: "${var.env}-apply-identity", Line: 5},
				{Key: "plan", Role: "plan", Name: "${var.name_prefix}-ci-plan", Environment: "${var.env}-plan", Line: 6},
			}, got)
		})

		t.Run("name の接尾辞が environment と違っても読む", func(t *testing.T) {
			t.Parallel()
			got, err := ParseTerraformRoles([]byte(rolesSrc(roleRow("plan", "plna", "plan"))))
			require.NoError(t, err)
			require.Len(t, got, 1)
			assert.Equal(t, "plan", got[0].Role)
			assert.Equal(t, "${var.name_prefix}-ci-plna", got[0].Name)
		})

		t.Run("属性の区切りの : と複数行の object を読む", func(t *testing.T) {
			t.Parallel()
			src := "locals {\n  roles = {\n    plan = {\n      name        = \"${var.name_prefix}-ci-plan\"\n      environment: \"${var.env}-plan\"\n    }\n  }\n}\n"
			got, err := ParseTerraformRoles([]byte(src))
			require.NoError(t, err)
			require.Len(t, got, 1)
			assert.Equal(t, "plan", got[0].Role)
		})

		t.Run("ほかの locals と、locals でないブロックの roles を読まない", func(t *testing.T) {
			t.Parallel()
			src := "variable \"roles\" {\n  default = { x = { name = \"a\" } }\n}\n" +
				"locals {\n  other = 1\n}\n" + rolesSrc(roleRow("plan", "plan", "plan"))
			got, err := ParseTerraformRoles([]byte(src))
			require.NoError(t, err)
			require.Len(t, got, 1)
			assert.Equal(t, "plan", got[0].Key)
		})

		t.Run("コメントと文字列の中の environment を件数に数えない", func(t *testing.T) {
			t.Parallel()
			src := "locals {\n  roles = {\n    # environment = \"${var.env}-x\"\n" +
				"    plan = { name = \"${var.name_prefix}-ci-plan\", environment = \"${var.env}-plan\" } // environment =\n  }\n" +
				"  note = \"environment = x\"\n}\n"
			got, err := ParseTerraformRoles([]byte(src))
			require.NoError(t, err)
			assert.Len(t, got, 1)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		plan := roleRow("plan", "plan", "plan")
		cases := []struct {
			name, src, reason string
		}{
			{"roles が無い", "locals {\n  other = 1\n}\n", "0 個"},
			{"roles が locals の外にしか無い", "variable \"roles\" {\n  default = {}\n}\n", "0 個"},
			{"roles が2つの locals に在る", rolesSrc(plan) + rolesSrc(plan), "2 個"},
			{"roles が0行", rolesSrc(), "0行"},
			{"roles が for 式", "locals {\n  roles = { for k in [\"plan\"] : k => { name = \"${var.name_prefix}-ci-${k}\", environment = \"${var.env}-${k}\" } }\n}\n", "object のリテラルではありません"},
			{"roles が変数の参照", "locals {\n  roles = var.roles\n}\n", "object のリテラルではありません"},
			{"roles が関数の呼び出し", "locals {\n  roles = merge({}, {})\n}\n", "object のリテラルではありません"},
			{"roles が括弧の式", "locals {\n  roles = ({ plan = { name = \"${var.name_prefix}-ci-plan\", environment = \"${var.env}-plan\" } })\n}\n", "object のリテラルではありません"},
			{"キーが引用符付き", rolesSrc(`    "plan" = { name = "${var.name_prefix}-ci-plan", environment = "${var.env}-plan" }` + "\n"), "キーが引用符の無い識別子ではありません"},
			{"キーが括弧の式", rolesSrc(`    (local.k) = { name = "${var.name_prefix}-ci-plan", environment = "${var.env}-plan" }` + "\n"), "キーが引用符の無い識別子ではありません"},
			{"キーが参照", rolesSrc(`    local.k = { name = "${var.name_prefix}-ci-plan", environment = "${var.env}-plan" }` + "\n"), "キーが引用符の無い識別子ではありません"},
			{"行の値が参照", rolesSrc("    plan = local.plan\n"), "値が object のリテラルではありません"},
			{"余分な属性", rolesSrc(`    plan = { name = "${var.name_prefix}-ci-plan", environment = "${var.env}-plan", arn = "x" }` + "\n"), "name と environment に限ります"},
			{"属性のキーが引用符付き", rolesSrc(`    plan = { "name" = "${var.name_prefix}-ci-plan", environment = "${var.env}-plan" }` + "\n"), "name と environment に限ります"},
			{"name が無い", rolesSrc(`    plan = { environment = "${var.env}-plan" }` + "\n"), "両方がありません"},
			{"environment が無い", rolesSrc(`    plan = { name = "${var.name_prefix}-ci-plan" }` + "\n"), "両方がありません"},
			{"name が重複", rolesSrc(`    plan = { name = "${var.name_prefix}-ci-plan", name = "${var.name_prefix}-ci-plan" }` + "\n"), "name が重複"},
			{"environment が重複", rolesSrc(`    plan = { name = "${var.name_prefix}-ci-plan", environment = "${var.env}-plan", environment = "${var.env}-plan" }` + "\n"), "environment が重複"},
			{"name が template の包みだけ", rolesSrc(`    plan = { name = "${var.name_prefix}", environment = "${var.env}-plan" }` + "\n"), "の name は"},
			{"name が固定の文字列だけ", rolesSrc(`    plan = { name = "tabp-admin-ci-plan", environment = "${var.env}-plan" }` + "\n"), "の name は"},
			{"name が別の変数を参照", rolesSrc(`    plan = { name = "${var.project}-ci-plan", environment = "${var.env}-plan" }` + "\n"), "の name は"},
			{"name が local を参照", rolesSrc(`    plan = { name = "${local.name_prefix}-ci-plan", environment = "${var.env}-plan" }` + "\n"), "の name は"},
			{"name が関数を使う", rolesSrc(`    plan = { name = format("%s-ci-plan", var.name_prefix), environment = "${var.env}-plan" }` + "\n"), "の name は"},
			{"name の template に関数を挟む", rolesSrc(`    plan = { name = "${lower(var.name_prefix)}-ci-plan", environment = "${var.env}-plan" }` + "\n"), "の name は"},
			{"name の template に参照が2つ", rolesSrc(`    plan = { name = "${var.name_prefix}-ci-${var.env}", environment = "${var.env}-plan" }` + "\n"), "の name は"},
			{"name が括弧の式", rolesSrc(`    plan = { name = ("${var.name_prefix}-ci-plan"), environment = "${var.env}-plan" }` + "\n"), "の name は"},
			{"name が heredoc", rolesSrc("    plan = {\n      name = <<EOT\n${var.name_prefix}-ci-plan\nEOT\n      environment = \"${var.env}-plan\"\n    }\n"), "の name は"},
			{"name の変数の後がハイフンで始まらない", rolesSrc(`    plan = { name = "${var.name_prefix}ci-plan", environment = "${var.env}-plan" }` + "\n"), "の name は"},
			{"environment が template の包みだけ", rolesSrc(`    plan = { name = "${var.name_prefix}-ci-plan", environment = "${var.env}" }` + "\n"), "の environment は"},
			{"environment の接尾辞がハイフンだけ", rolesSrc(`    plan = { name = "${var.name_prefix}-ci-plan", environment = "${var.env}-" }` + "\n"), "の environment は"},
			{"environment が var.name_prefix を参照", rolesSrc(`    plan = { name = "${var.name_prefix}-ci-plan", environment = "${var.name_prefix}-plan" }` + "\n"), "の environment は"},
			{"environment が var.env の下を参照", rolesSrc(`    plan = { name = "${var.name_prefix}-ci-plan", environment = "${var.env.x}-plan" }` + "\n"), "の environment は"},
			{"キーが重複", rolesSrc(plan, roleRow("plan", "apply", "apply")), "重複"},
			{"同じ environment を別のキーが持つ", rolesSrc(plan, roleRow("plan2", "plan2", "plan")), "重複"},
			{"同じ name を別のキーが持つ", rolesSrc(plan, roleRow("apply", "plan", "apply")), "重複"},
			{"字句の列の environment の数が読めた行数と食い違う", rolesSrc(plan, roleRow("environment", "apply", "apply")), "字句の列の environment の属性 3 個 / 読めた行 2 行"},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				t.Parallel()
				_, err := ParseTerraformRoles([]byte(c.src))
				require.ErrorIs(t, err, ErrTerraformParse)
				assert.Contains(t, err.Error(), c.reason)
			})
		}

		t.Run("HCL として読めなければ、roles を探す前に弾く", func(t *testing.T) {
			t.Parallel()
			_, err := ParseTerraformRoles([]byte("locals {\n  roles = {\n"))
			require.ErrorIs(t, err, ErrTerraformParse)
			assert.NotContains(t, err.Error(), "locals の roles が")
		})

		t.Run("空のソースは roles が無いとして弾く", func(t *testing.T) {
			t.Parallel()
			_, err := ParseTerraformRoles(nil)
			require.ErrorIs(t, err, ErrTerraformParse)
			assert.Contains(t, err.Error(), "0 個")
		})
	})
}

func Test_parseTerraformRow(t *testing.T) {
	t.Parallel()

	item := func(t *testing.T, src string) hclsyntax.ObjectConsItem {
		t.Helper()
		expr, diags := hclsyntax.ParseExpression([]byte(src), "x", hcl.InitialPos)
		require.False(t, diags.HasErrors(), diags.Error())
		obj, ok := expr.(*hclsyntax.ObjectConsExpr)
		require.True(t, ok)
		require.Len(t, obj.Items, 1)
		return obj.Items[0]
	}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("キー・role・name・environment・行番号を読む", func(t *testing.T) {
			t.Parallel()
			src := "{\n  unlock_identity = { environment = \"${var.env}-unlock-identity\", name = \"${var.name_prefix}-ci-unlock-identity\" }\n}"
			got, err := parseTerraformRow([]byte(src), item(t, src))
			require.NoError(t, err)
			assert.Equal(t, TerraformRole{Key: "unlock_identity", Role: "unlock-identity", Name: "${var.name_prefix}-ci-unlock-identity", Environment: "${var.env}-unlock-identity", Line: 2}, got)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("値が object のリテラルでなければ弾く", func(t *testing.T) {
			t.Parallel()
			src := "{ plan = \"x\" }"
			_, err := parseTerraformRow([]byte(src), item(t, src))
			require.ErrorIs(t, err, ErrTerraformParse)
			assert.Contains(t, err.Error(), "値が object のリテラルではありません")
		})

		t.Run("name と environment のほかの属性があれば弾く", func(t *testing.T) {
			t.Parallel()
			src := "{ plan = { name = \"${var.name_prefix}-ci-plan\", environment = \"${var.env}-plan\", arn = \"x\" } }"
			_, err := parseTerraformRow([]byte(src), item(t, src))
			require.ErrorIs(t, err, ErrTerraformParse)
			assert.Contains(t, err.Error(), "name と environment に限ります")
		})

		t.Run("name と environment の両方が無ければ弾く", func(t *testing.T) {
			t.Parallel()
			src := "{ plan = {} }"
			_, err := parseTerraformRow([]byte(src), item(t, src))
			require.ErrorIs(t, err, ErrTerraformParse)
			assert.Contains(t, err.Error(), "両方がありません")
		})
	})
}

func Test_identKey(t *testing.T) {
	t.Parallel()

	key := func(t *testing.T, src string) hclsyntax.Expression {
		t.Helper()
		expr, diags := hclsyntax.ParseExpression([]byte("{ "+src+" = 1 }"), "x", hcl.InitialPos)
		require.False(t, diags.HasErrors(), diags.Error())
		obj, ok := expr.(*hclsyntax.ObjectConsExpr)
		require.True(t, ok)
		return obj.Items[0].KeyExpr
	}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("引用符の無い識別子の名前を返す", func(t *testing.T) {
			t.Parallel()
			got, ok := identKey(key(t, "apply_identity"))
			require.True(t, ok)
			assert.Equal(t, "apply_identity", got)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for _, src := range []string{`"plan"`, "(plan)", "local.plan", `"${x}"`} {
			t.Run(src+" を識別子と読まない", func(t *testing.T) {
				t.Parallel()
				_, ok := identKey(key(t, src))
				assert.False(t, ok)
			})
		}

		t.Run("object のキーでない式を識別子と読まない", func(t *testing.T) {
			t.Parallel()
			expr, diags := hclsyntax.ParseExpression([]byte("plan"), "x", hcl.InitialPos)
			require.False(t, diags.HasErrors())
			_, ok := identKey(expr)
			assert.False(t, ok)
		})
	})
}

func Test_prefixedTemplate(t *testing.T) {
	t.Parallel()

	read := func(t *testing.T, src string) (string, bool) {
		t.Helper()
		expr, diags := hclsyntax.ParseExpression([]byte(src), "x", hcl.InitialPos)
		require.False(t, diags.HasErrors(), diags.Error())
		return prefixedTemplate([]byte(src), expr, "env")
	}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("変数に続く固定の文字列を返す", func(t *testing.T) {
			t.Parallel()
			got, ok := read(t, `"${var.env}-apply-identity"`)
			require.True(t, ok)
			assert.Equal(t, "-apply-identity", got)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for _, src := range []string{
			`"${var.env}"`, `"plan"`, `"${var.name_prefix}-plan"`, `"${var.env.x}-plan"`, `"${local.env}-plan"`,
			`"${env}-plan"`, `"${var["env"]}-plan"`, `"${var.env}${var.env}"`, `"${var.env}plan"`, `"${var.env}-"`, "(\"${var.env}-plan\")",
			"<<EOT\n${var.env}-plan\nEOT\n", `"-plan${var.env}"`,
		} {
			t.Run(src+" を読まない", func(t *testing.T) {
				t.Parallel()
				_, ok := read(t, src)
				assert.False(t, ok)
			})
		}

		t.Run("ソースの範囲の外を指す式を読まない", func(t *testing.T) {
			t.Parallel()
			src := `"${var.env}-plan"`
			expr, diags := hclsyntax.ParseExpression([]byte(src), "x", hcl.InitialPos)
			require.False(t, diags.HasErrors())
			_, ok := prefixedTemplate(nil, expr, "env")
			assert.False(t, ok)
		})
	})
}

func Test_countEnvironmentTokens(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("範囲の中の、= か : が続く識別子 environment だけを数える", func(t *testing.T) {
			t.Parallel()
			inside := "a = { environment = 1, environment: 2, environment_id = 3, x = environment, y = \"environment = 0\" }\n# environment = 4\n"
			src := []byte(inside + "b = { environment = 5 }\n")
			assert.Equal(t, 2, countEnvironmentTokens(src, hcl.Range{Start: hcl.InitialPos, End: hcl.Pos{Byte: len(inside)}}))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("字句として読めないソースは panic する", func(t *testing.T) {
			t.Parallel()
			src := []byte("a = \"\xff\"\n")
			var got any
			func() {
				defer func() { got = recover() }()
				countEnvironmentTokens(src, hcl.Range{Start: hcl.InitialPos, End: hcl.Pos{Byte: len(src)}})
			}()
			msg, ok := got.(string)
			require.True(t, ok, "panic しないか、値が文字列ではありません: %v", got)
			assert.True(t, strings.HasPrefix(msg, "cientry.countEnvironmentTokens: hclsyntax.ParseConfig が読めたソースではありません: "), msg)
		})
	})
}

func TestTerraformRolesRepo(t *testing.T) {
	t.Parallel()

	write := func(t *testing.T, body string) string {
		t.Helper()
		repo := t.TempDir()
		path := filepath.Join(repo, filepath.FromSlash(TerraformRolesFile))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
		return repo
	}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("リポジトリの github-oidc の main.tf を読む", func(t *testing.T) {
			t.Parallel()
			got, err := TerraformRolesRepo(write(t, rolesSrc(roleRow("plan", "plan", "plan"))))
			require.NoError(t, err)
			require.Len(t, got, 1)
			assert.Equal(t, "plan", got[0].Role)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("リポジトリが無ければ弾く", func(t *testing.T) {
			t.Parallel()
			_, err := TerraformRolesRepo(filepath.Join(t.TempDir(), "missing"))
			require.ErrorIs(t, err, fs.ErrNotExist)
		})

		t.Run("main.tf が無ければ弾く", func(t *testing.T) {
			t.Parallel()
			_, err := TerraformRolesRepo(t.TempDir())
			require.ErrorIs(t, err, fs.ErrNotExist)
		})

		t.Run("解釈できない main.tf は、パスを添えて弾く", func(t *testing.T) {
			t.Parallel()
			_, err := TerraformRolesRepo(write(t, ""))
			require.ErrorIs(t, err, ErrTerraformParse)
			assert.Contains(t, err.Error(), TerraformRolesFile)
		})
	})
}

func TestCheckTerraformRoles(t *testing.T) {
	t.Parallel()

	parse := func(t *testing.T, rows ...string) []TerraformRole {
		t.Helper()
		got, err := ParseTerraformRoles([]byte(rolesSrc(rows...)))
		require.NoError(t, err)
		return got
	}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("登録簿のすべての role の行が揃えば成功する", func(t *testing.T) {
			t.Parallel()
			assert.NoError(t, CheckTerraformRoles(parse(t, registryRows()...)))
		})

		// 写しと正本のずれを検出する検査は、正本の実物を読んでよい（ADR-0702 決定17）。
		t.Run("写しと正本のずれ: 実物の github-oidc の local.roles が登録簿の role と一致する", func(t *testing.T) {
			t.Parallel()
			rows, err := TerraformRolesRepo(repoRoot)
			require.NoError(t, err)
			assert.NoError(t, CheckTerraformRoles(rows))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("登録簿の role の行が無ければ、その role を登録簿の側として名指して弾く", func(t *testing.T) {
			t.Parallel()
			err := CheckTerraformRoles(parse(t, registryRowsWithout(Apply)...))
			require.ErrorIs(t, err, ErrTerraformRole)
			assert.Contains(t, err.Error(), "登録簿の role apply が local.roles にありません")
			assert.NotContains(t, err.Error(), "は登録簿にありません")
		})

		t.Run("登録簿に無い role の行があれば、その role を local.roles の側として名指して弾く", func(t *testing.T) {
			t.Parallel()
			err := CheckTerraformRoles(parse(t, append(registryRows(), roleRow("audit", "audit", "audit"))...))
			require.ErrorIs(t, err, ErrTerraformRole)
			assert.Contains(t, err.Error(), "local.roles の role audit は登録簿にありません")
			assert.NotContains(t, err.Error(), "が local.roles にありません")
		})

		t.Run("欠けた role と余分な role を両方名指す", func(t *testing.T) {
			t.Parallel()
			err := CheckTerraformRoles(parse(t, append(registryRowsWithout(Plan), roleRow("audit", "audit", "audit"))...))
			require.ErrorIs(t, err, ErrTerraformRole)
			assert.Contains(t, err.Error(), "登録簿の role plan が local.roles にありません")
			assert.Contains(t, err.Error(), "local.roles の role audit は登録簿にありません")
		})

		t.Run("name だけが登録簿に無い role を指す行を、その行を名指して弾く", func(t *testing.T) {
			t.Parallel()
			rows := append(registryRowsWithout(Plan), roleRow("plan", "audit", "plan"))
			err := CheckTerraformRoles(parse(t, rows...))
			require.ErrorIs(t, err, ErrTerraformRole)
			assert.Contains(t, err.Error(), "plan の name ${var.name_prefix}-ci-audit は ${var.name_prefix}-ci-plan ではありません")
		})

		t.Run("name だけが欠けた role を、その role の行の name のずれとして弾く", func(t *testing.T) {
			t.Parallel()
			rows := append(registryRowsWithout(Apply), roleRow("apply", "apply-x", "apply"))
			err := CheckTerraformRoles(parse(t, rows...))
			require.ErrorIs(t, err, ErrTerraformRole)
			assert.Contains(t, err.Error(), "apply の name ${var.name_prefix}-ci-apply-x は ${var.name_prefix}-ci-apply ではありません")
		})

		t.Run("name の -ci- の接頭辞が違えば弾く", func(t *testing.T) {
			t.Parallel()
			rows := append(registryRowsWithout(Plan), `    plan = { name = "${var.name_prefix}-cd-plan", environment = "${var.env}-plan" }`+"\n")
			err := CheckTerraformRoles(parse(t, rows...))
			require.ErrorIs(t, err, ErrTerraformRole)
			assert.Contains(t, err.Error(), "${var.name_prefix}-cd-plan")
		})

		t.Run("行が無ければ弾く", func(t *testing.T) {
			t.Parallel()
			require.ErrorIs(t, CheckTerraformRoles(nil), ErrTerraformRole)
		})
	})
}

func Test_registry_checkTerraformRoles(t *testing.T) {
	t.Parallel()

	r := registry{{Name: "a", IdentityRole: "a-identity", WorkloadRole: "a"}, {Name: "b", IdentityRole: "a-identity", WorkloadRole: "b"}}
	row := func(role string) TerraformRole {
		return TerraformRole{Key: role, Role: role, Name: TerraformNamePrefix + "-ci-" + role, Environment: TerraformEnvPrefix + "-" + role}
	}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("種別の間で重なる role を1つと数え、順を問わず一致すれば成功する", func(t *testing.T) {
			t.Parallel()
			assert.NoError(t, r.checkTerraformRoles([]TerraformRole{row("b"), row("a-identity"), row("a")}))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("identity の role の行が無ければ弾く", func(t *testing.T) {
			t.Parallel()
			err := r.checkTerraformRoles([]TerraformRole{row("a"), row("b")})
			require.ErrorIs(t, err, ErrTerraformRole)
			assert.Contains(t, err.Error(), "a-identity")
		})

		t.Run("行が無ければ弾く", func(t *testing.T) {
			t.Parallel()
			require.ErrorIs(t, r.checkTerraformRoles(nil), ErrTerraformRole)
		})

		t.Run("同じ role の行が2つあれば、両方の行を名指して弾く", func(t *testing.T) {
			t.Parallel()
			dup := row("a")
			dup.Key, dup.Line = "a2", 4
			first := row("a")
			first.Line = 2
			err := r.checkTerraformRoles([]TerraformRole{first, row("b"), row("a-identity"), dup})
			require.ErrorIs(t, err, ErrTerraformRole)
			assert.Contains(t, err.Error(), "4 行目: a2 の role a が 2 行目の a と重複しています")
		})

		t.Run("登録簿が空なら、行の有無を問わず弾く", func(t *testing.T) {
			t.Parallel()
			require.ErrorIs(t, registry{}.checkTerraformRoles(nil), errRegistry)
			require.ErrorIs(t, registry{}.checkTerraformRoles([]TerraformRole{row("a")}), errRegistry)
		})
	})
}

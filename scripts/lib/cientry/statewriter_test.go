package cientry

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func quoted(roles ...string) string {
	q := make([]string, len(roles))
	for i, r := range roles {
		q[i] = `"` + r + `"`
	}
	return strings.Join(q, ", ")
}

func scpPatterns(roles ...string) string {
	return "flatten([\n    for env in [\"dev\", \"prd\"] : [\n      for suffix in [" + quoted(roles...) +
		"] : \"arn:aws:iam::*:role/${var.project}-${env}-ci-${suffix}\"\n    ]\n  ])"
}

func scpSrc(expr string) string {
	return "locals {\n  organization_id = \"o\"\n\n  state_writer_role_patterns = " + expr + "\n}\n"
}

func regoSrc(group string) string {
	return "package x\n\nstate_writer_pattern := `^arn:aws:iam::\\*:role/[a-z0-9]+-(dev|prd)-ci-(" + group + ")$`\n\n" +
		"deny contains msg if {\n\tnot regex.match(state_writer_pattern, v)\n}\n"
}

func parseExpr(t *testing.T, src string) hclsyntax.Expression {
	t.Helper()
	expr, diags := hclsyntax.ParseExpression([]byte(src), "x", hcl.InitialPos)
	require.False(t, diags.HasErrors(), diags.Error())
	return expr
}

func TestStateWriterRoles(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("lock を扱う種別の role を重複なく登録簿の順で返し、plan の role を含めない", func(t *testing.T) {
			t.Parallel()
			got := StateWriterRoles()
			assert.Equal(t, []string{"apply-identity", "apply", "unlock-identity", "unlock"}, got)
			assert.NotContains(t, got, "plan")
		})

		t.Run("返り値を書き換えても次の呼び出しは変わらない", func(t *testing.T) {
			t.Parallel()
			got := StateWriterRoles()
			got[0] = "x"
			assert.Equal(t, "apply-identity", StateWriterRoles()[0])
		})
	})
}

func Test_registry_stateWriterRoles(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("LockSkip の種別だけが持つ role を除き、種別の間で重なる role を1つにする", func(t *testing.T) {
			t.Parallel()
			r := registry{
				{Name: "a", Lock: LockSkip, IdentityRole: "a", WorkloadRole: "a"},
				{Name: "b", Lock: LockRelease, IdentityRole: "b-identity", WorkloadRole: "b"},
				{Name: "c", Lock: LockKeep, IdentityRole: "b-identity", WorkloadRole: "b"},
				{Name: "d", Lock: LockRemove, IdentityRole: "d-identity", WorkloadRole: "d"},
			}
			assert.Equal(t, []string{"b-identity", "b", "d-identity", "d"}, r.stateWriterRoles())
		})

		t.Run("LockSkip の種別の role でも、ほかの種別が持てば含める", func(t *testing.T) {
			t.Parallel()
			r := registry{
				{Name: "a", Lock: LockSkip, IdentityRole: "x", WorkloadRole: "x"},
				{Name: "b", Lock: LockRelease, IdentityRole: "x", WorkloadRole: "y"},
			}
			assert.Equal(t, []string{"x", "y"}, r.stateWriterRoles())
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("すべてが LockSkip なら空を返す", func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, registry{{Name: "a", Lock: LockSkip, IdentityRole: "a", WorkloadRole: "a"}}.stateWriterRoles())
		})
	})
}

func Test_registry_checkStateWriterRoles(t *testing.T) {
	t.Parallel()

	r := registry{
		{Name: "a", Lock: LockSkip, IdentityRole: "a", WorkloadRole: "a"},
		{Name: "b", Lock: LockRelease, IdentityRole: "b-identity", WorkloadRole: "b"},
	}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("順を問わず集合が一致すれば成功する", func(t *testing.T) {
			t.Parallel()
			assert.NoError(t, r.checkStateWriterRoles("写し", []string{"b", "b-identity"}))
		})

		// 写しと正本のずれを検出する検査は、正本の実物を読んでよい（ADR-0702 決定17）。
		t.Run("写しと正本のずれ", func(t *testing.T) {
			t.Parallel()

			for _, c := range []struct {
				path  string
				parse func([]byte) ([]string, error)
			}{
				{StateWriterSCPFile, parseSCPStateWriterRoles},
				{StateWriterRegoFile, parseRegoStateWriterRoles},
			} {
				t.Run(c.path+" の role が登録簿の state に書く role と一致する", func(t *testing.T) {
					t.Parallel()
					got, err := readRoles(repoRoot, c.path, c.parse)
					require.NoError(t, err)
					assert.NoError(t, std.checkStateWriterRoles(c.path, got))
				})
			}
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("登録簿の role が無ければ、その role を登録簿の側として名指して弾く", func(t *testing.T) {
			t.Parallel()
			err := r.checkStateWriterRoles("写し", []string{"b"})
			require.ErrorIs(t, err, ErrStateWriterRole)
			assert.Contains(t, err.Error(), "登録簿の state に書く role b-identity が 写し にありません")
			assert.NotContains(t, err.Error(), "は登録簿の state に書く role にありません")
		})

		t.Run("登録簿の state に書く role でない role があれば、その role を写しの側として名指して弾く", func(t *testing.T) {
			t.Parallel()
			err := r.checkStateWriterRoles("写し", []string{"b", "b-identity", "a"})
			require.ErrorIs(t, err, ErrStateWriterRole)
			assert.Contains(t, err.Error(), "写し の role a は登録簿の state に書く role にありません")
			assert.NotContains(t, err.Error(), "にありません。")
		})

		t.Run("欠けた role と余分な role を両方名指す", func(t *testing.T) {
			t.Parallel()
			err := r.checkStateWriterRoles("写し", []string{"b", "c"})
			require.ErrorIs(t, err, ErrStateWriterRole)
			assert.Contains(t, err.Error(), "登録簿の state に書く role b-identity が 写し にありません")
			assert.Contains(t, err.Error(), "写し の role c は登録簿の state に書く role にありません")
		})

		t.Run("写しの role が0件なら弾く", func(t *testing.T) {
			t.Parallel()
			err := r.checkStateWriterRoles("写し", nil)
			require.ErrorIs(t, err, ErrStateWriterRole)
			assert.Contains(t, err.Error(), "写し の role が0件です")
		})

		t.Run("登録簿に state に書く role が無ければ、写しを問わず弾く", func(t *testing.T) {
			t.Parallel()
			skip := registry{{Name: "a", Lock: LockSkip, IdentityRole: "a", WorkloadRole: "a"}}
			require.ErrorIs(t, skip.checkStateWriterRoles("写し", nil), errRegistry)
			require.ErrorIs(t, skip.checkStateWriterRoles("写し", []string{"a"}), errRegistry)
		})
	})
}

func Test_parseSCPStateWriterRoles(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("内側の for の集まりの role を書かれた順で読む", func(t *testing.T) {
			t.Parallel()
			got, err := parseSCPStateWriterRoles([]byte(scpSrc(scpPatterns("unlock", "apply-identity"))))
			require.NoError(t, err)
			assert.Equal(t, []string{"unlock", "apply-identity"}, got)
		})

		t.Run("ほかの locals と、locals でないブロックの同名の属性を読まない", func(t *testing.T) {
			t.Parallel()
			src := "variable \"state_writer_role_patterns\" {\n  default = 1\n}\nlocals {\n  other = 1\n}\n" + scpSrc(scpPatterns("apply"))
			got, err := parseSCPStateWriterRoles([]byte(src))
			require.NoError(t, err)
			assert.Equal(t, []string{"apply"}, got)
		})

		t.Run("コメントの中の for の並びを件数に数えない", func(t *testing.T) {
			t.Parallel()
			expr := "flatten([\n    # for suffix in [\"x\"]\n    for env in [\"dev\"] : [\n      for suffix in [\"apply\"] : \"r/${env}-ci-${suffix}\"\n    ]\n  ])"
			got, err := parseSCPStateWriterRoles([]byte(scpSrc(expr)))
			require.NoError(t, err)
			assert.Equal(t, []string{"apply"}, got)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		cases := []struct {
			name, src, reason string
		}{
			{"属性が無い", "locals {\n  other = 1\n}\n", "0 個"},
			{"属性が locals の外にしか無い", "variable \"state_writer_role_patterns\" {\n  default = 1\n}\n", "0 個"},
			{"属性が2つの locals に在る", scpSrc(scpPatterns("apply")) + scpSrc(scpPatterns("apply")), "2 個"},
			{"値が flatten でない", scpSrc("[]"), "4 行目: 値が引数1つの flatten の呼び出しではありません"},
			{"外側の集まりが空", scpSrc("flatten([for env in [] : [for suffix in [\"apply\"] : \"r/${env}-ci-${suffix}\"]])"), "4 行目: 外側の for の集まりが、文字列のリテラルを1つ以上並べた tuple ではありません"},
			{"外側の集まりが参照", scpSrc("flatten([for env in local.envs : [for suffix in [\"apply\"] : \"r/${env}-ci-${suffix}\"]])"), "外側の for の集まりが、文字列のリテラルを1つ以上並べた tuple ではありません"},
			{"内側の集まりが参照", scpSrc("flatten([for env in [\"dev\"] : [for suffix in local.s : \"r/${env}-ci-${suffix}\"]])"), "集まりが tuple のリテラルではありません"},
			{"role の要素が template", scpSrc(scpPatterns("${var.x}")), "文字列のリテラルではありません"},
			{"role が重複", scpSrc(scpPatterns("apply", "apply")), "role apply が重複しています"},
			{"role の一覧が0件", scpSrc(scpPatterns()), "role の一覧が0件です"},
			{
				"字句の列の role の文字列の数が読めた件数と食い違う",
				scpSrc("flatten([\n    for env in [\"dev\"] : [\n      for suffix in [\"apply\"] : \"r/${[for suffix in [\"zz\"] : suffix][0]}-ci-${suffix}\"\n    ]\n  ])"),
				"字句の列の role の文字列 2 個 / 読めた role 1 個",
			},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				t.Parallel()
				_, err := parseSCPStateWriterRoles([]byte(c.src))
				require.ErrorIs(t, err, ErrStateWriterParse)
				assert.Contains(t, err.Error(), c.reason)
			})
		}

		t.Run("HCL として読めなければ、属性を探す前に弾く", func(t *testing.T) {
			t.Parallel()
			_, err := parseSCPStateWriterRoles([]byte("locals {\n  state_writer_role_patterns = [\n"))
			require.ErrorIs(t, err, ErrStateWriterParse)
			assert.NotContains(t, err.Error(), "locals の state_writer_role_patterns が")
		})

		t.Run("空のソースは属性が無いとして弾く", func(t *testing.T) {
			t.Parallel()
			_, err := parseSCPStateWriterRoles(nil)
			require.ErrorIs(t, err, ErrStateWriterParse)
			assert.Contains(t, err.Error(), "0 個")
		})
	})
}

func Test_innerRoleFor(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("内側の for を返す", func(t *testing.T) {
			t.Parallel()
			got, err := innerRoleFor(parseExpr(t, scpPatterns("apply")))
			require.NoError(t, err)
			assert.Equal(t, "suffix", got.ValVar)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		inner := `[for s in ["apply"] : "r/${e}-ci-${s}"]`
		cases := []struct {
			name, src, reason string
		}{
			{"値が tuple", "[]", "引数1つの flatten の呼び出しではありません"},
			{"flatten でない関数", "concat([for e in [\"dev\"] : " + inner + "])", "引数1つの flatten の呼び出しではありません"},
			{"flatten の引数が2つ", "flatten([], [])", "引数1つの flatten の呼び出しではありません"},
			{"flatten の引数を展開する", "flatten(local.x...)", "引数1つの flatten の呼び出しではありません"},
			{"flatten の引数が for 式でない", "flatten([" + inner + "])", "tuple の for 式ではありません"},
			{"外側が object の for 式", "flatten({for e in [\"dev\"] : e => " + inner + "})", "tuple の for 式ではありません"},
			{"外側に条件がある", "flatten([for e in [\"dev\"] : " + inner + " if e != \"\"])", "tuple の for 式ではありません"},
			{"外側の集まりが空の tuple", "flatten([for e in [] : " + inner + "])", "外側の for の集まりが、文字列のリテラルを1つ以上並べた tuple ではありません"},
			{"外側の集まりが参照", "flatten([for e in local.envs : " + inner + "])", "外側の for の集まりが、文字列のリテラルを1つ以上並べた tuple ではありません"},
			{"外側の集まりが関数の呼び出し", "flatten([for e in tolist([\"dev\"]) : " + inner + "])", "外側の for の集まりが、文字列のリテラルを1つ以上並べた tuple ではありません"},
			{"外側の集まりの要素が参照", "flatten([for e in [\"dev\", var.env] : " + inner + "])", "外側の for の集まりが、文字列のリテラルを1つ以上並べた tuple ではありません"},
			{"外側の集まりの要素が template", "flatten([for e in [\"${var.env}\"] : " + inner + "])", "外側の for の集まりが、文字列のリテラルを1つ以上並べた tuple ではありません"},
			{"外側の値が for 式でない", "flatten([for e in [\"dev\"] : e])", "外側の for の値が、キーの変数も条件も無い tuple の for 式ではありません"},
			{"外側の値が for 式を包む tuple", "flatten([for e in [\"dev\"] : [" + inner + "]])", "外側の for の値が、キーの変数も条件も無い"},
			{"内側がキーの変数を持つ", "flatten([for e in [\"dev\"] : [for i, s in [\"apply\"] : \"r/${e}-ci-${s}\"]])", "外側の for の値が、キーの変数も条件も無い"},
			{"内側に条件がある", "flatten([for e in [\"dev\"] : [for s in [\"apply\"] : \"r/${e}-ci-${s}\" if s != \"\"]])", "外側の for の値が、キーの変数も条件も無い"},
			{"内側が object の for 式", "flatten([for e in [\"dev\"] : {for s in [\"apply\"] : s => \"r/${e}-ci-${s}\"}])", "外側の for の値が、キーの変数も条件も無い"},
			{"内側の集まりが関数の呼び出し", "flatten([for e in [\"dev\"] : [for s in tolist([\"apply\"]) : \"r/${e}-ci-${s}\"]])", "集まりが tuple のリテラルではありません"},
			{"内側の値が -ci-${<role>} で終わらない", "flatten([for e in [\"dev\"] : [for s in [\"apply\"] : \"r/${e}-cd-${s}\"]])", "内側の for の値が \"...-ci-${s}\" で終わる template ではありません"},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				t.Parallel()
				_, err := innerRoleFor(parseExpr(t, c.src))
				require.ErrorIs(t, err, ErrStateWriterParse)
				assert.Contains(t, err.Error(), c.reason)
			})
		}
	})
}

func Test_endsWithRole(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("-ci- で終わる固定の文字列と変数の参照で終わる template を受け付ける", func(t *testing.T) {
			t.Parallel()
			assert.True(t, endsWithRole(parseExpr(t, `"r/${var.p}-${e}-ci-${s}"`), "s"))
			assert.True(t, endsWithRole(parseExpr(t, `"-ci-${s}"`), "s"))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for _, src := range []string{
			`s`, `"${s}"`, `"r/-ci-"`, `"r/-ci-${s}x"`, `"r/-ci-${t}"`, `"r/-ci-${s.x}"`, `"r/${e}${s}"`, `"r/-cd-${s}"`,
		} {
			t.Run(src+" を受け付けない", func(t *testing.T) {
				t.Parallel()
				assert.False(t, endsWithRole(parseExpr(t, src), "s"))
			})
		}
	})
}

func Test_stringLiterals(t *testing.T) {
	t.Parallel()

	tuple := func(t *testing.T, src string) *hclsyntax.TupleConsExpr {
		t.Helper()
		got, ok := parseExpr(t, src).(*hclsyntax.TupleConsExpr)
		require.True(t, ok)
		return got
	}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("文字列のリテラルを書かれた順で返す", func(t *testing.T) {
			t.Parallel()
			got, err := stringLiterals(tuple(t, `["unlock-identity", "apply"]`))
			require.NoError(t, err)
			assert.Equal(t, []string{"unlock-identity", "apply"}, got)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		cases := []struct {
			name, src, reason string
		}{
			{"0件", `[]`, "1 行目: role の一覧が0件です"},
			{"参照", `["apply", local.x]`, "1 行目: role の一覧の要素が文字列のリテラルではありません"},
			{"数", `[1]`, "文字列のリテラルではありません"},
			{"template", `["a-${x}"]`, "文字列のリテラルではありません"},
			{"role の形でない", `["Apply"]`, `1 行目: role "Apply" を読めません`},
			{"重複", "[\n\"apply\",\n\"apply\"\n]", "3 行目: role apply が重複しています"},
		}
		for _, c := range cases {
			t.Run(c.name+"を弾く", func(t *testing.T) {
				t.Parallel()
				_, err := stringLiterals(tuple(t, c.src))
				require.ErrorIs(t, err, ErrStateWriterParse)
				assert.Contains(t, err.Error(), c.reason)
			})
		}
	})
}

func Test_appendRole(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("末尾へ足す", func(t *testing.T) {
			t.Parallel()
			roles := []string{"apply"}
			require.NoError(t, appendRole(&roles, "unlock-identity"))
			assert.Equal(t, []string{"apply", "unlock-identity"}, roles)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for _, role := range []string{"", "Apply", "apply-", "-apply", "apply identity", "apply_identity"} {
			t.Run(role+" を role の形でないとして弾く", func(t *testing.T) {
				t.Parallel()
				var roles []string
				err := appendRole(&roles, role)
				require.ErrorIs(t, err, ErrStateWriterParse)
				assert.Contains(t, err.Error(), "を読めません")
				assert.Empty(t, roles)
			})
		}

		t.Run("既に在る role を弾き、足さない", func(t *testing.T) {
			t.Parallel()
			roles := []string{"apply"}
			err := appendRole(&roles, "apply")
			require.ErrorIs(t, err, ErrStateWriterParse)
			assert.Contains(t, err.Error(), "role apply が重複しています")
			assert.Equal(t, []string{"apply"}, roles)
		})
	})
}

func Test_countRoleQuotes(t *testing.T) {
	t.Parallel()

	whole := func(src []byte) hcl.Range {
		return hcl.Range{Start: hcl.InitialPos, End: hcl.Pos{Byte: len(src)}}
	}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("for <name> in [ から対応する ] までの文字列を数え、入れ子の [ ] を越えない", func(t *testing.T) {
			t.Parallel()
			src := []byte("a = [for s in [\"a\", [\"b\"], \"c\"] : s]\nb = [\"d\"]\n")
			assert.Equal(t, 3, countRoleQuotes(src, whole(src), "s"))
		})

		t.Run("並びが2度現れれば合わせて数える", func(t *testing.T) {
			t.Parallel()
			src := []byte("a = [for s in [\"a\"] : s]\nb = [for s in [\"b\", \"c\"] : s]\n")
			assert.Equal(t, 3, countRoleQuotes(src, whole(src), "s"))
		})

		t.Run("範囲の外、別の変数の for、コメントを数えない", func(t *testing.T) {
			t.Parallel()
			inside := "a = [for t in [\"x\"] : t]\n# for s in [\"y\"]\n"
			src := []byte(inside + "b = [for s in [\"z\"] : s]\n")
			assert.Equal(t, 0, countRoleQuotes(src, hcl.Range{Start: hcl.InitialPos, End: hcl.Pos{Byte: len(inside)}}, "s"))
		})

		t.Run("集まりが [ で始まらない並びを数えない", func(t *testing.T) {
			t.Parallel()
			src := []byte("a = [for s in local.x : \"${s}\"]\n")
			assert.Equal(t, 0, countRoleQuotes(src, whole(src), "s"))
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
				countRoleQuotes(src, whole(src), "s")
			}()
			msg, ok := got.(string)
			require.True(t, ok, "panic しないか、値が文字列ではありません: %v", got)
			assert.Contains(t, msg, "hclsyntax.ParseConfig が読めたソースではありません")
		})
	})
}

func Test_parseRegoStateWriterRoles(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("末尾の選択肢の role を書かれた順で読み、本体の中の参照を規則の行と数えない", func(t *testing.T) {
			t.Parallel()
			got, err := parseRegoStateWriterRoles([]byte(regoSrc("unlock|apply-identity")))
			require.NoError(t, err)
			assert.Equal(t, []string{"unlock", "apply-identity"}, got)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		head := "state_writer_pattern := `^r/(dev)-ci-(apply)$`\n"
		regoLine := func(pattern string) string { return "state_writer_pattern := `" + pattern + "`\n" }
		env := `^arn:aws:iam::\*:role/[a-z0-9]+-(dev|log-archive)-ci-`
		cases := []struct {
			name, src, reason string
		}{
			{"規則の行が無い", "package x\n", "規則の行が 0 行です"},
			{"規則の行が2行", regoSrc("apply") + head, "規則の行が 2 行です"},
			{"字下げした規則の行も数える", regoSrc("apply") + "  " + head, "規則の行が 2 行です"},
			{"本体を持つ規則も数える", regoSrc("apply") + "state_writer_pattern if {\n\ttrue\n}\n", "規則の行が 2 行です"},
			{"二重引用符の文字列", "state_writer_pattern := \"^r-ci-(apply)$\"\n", "1 行目: state_writer_pattern が raw string 1つの代入ではありません"},
			{"= の代入", "state_writer_pattern = `^r-ci-(apply)$`\n", "raw string 1つの代入ではありません"},
			{"正規表現を組めない", "x := 1\n" + regoLine(env+"(apply$"), "2 行目: 正規表現を組めません"},
			{"ARN の接頭辞が無い", regoLine(`^r/[a-z0-9]+-(dev)-ci-(apply)$`), `1 行目: 正規表現が ^arn:aws:iam::\*:role/ で始まりません`},
			{"接頭辞を .* に緩める", regoLine(`^.*-ci-(apply)$`), "で始まりません"},
			{"^ が無い", regoLine(`arn:aws:iam::\*:role/[a-z0-9]+-(dev)-ci-(apply)$`), "で始まりません"},
			{"project を .* に緩める", regoLine(`^arn:aws:iam::\*:role/.*-(dev)-ci-(apply)$`), "1 行目: 正規表現が ^arn:aws:iam::\\*:role/[a-z0-9]+-(<env>|...)-ci-(<role>|...)$ の形ではありません"},
			{"env の選択肢が無い", regoLine(`^arn:aws:iam::\*:role/[a-z0-9]+-ci-(apply)$`), "の形ではありません"},
			{"env の選択肢を括弧で囲まない", regoLine(`^arn:aws:iam::\*:role/[a-z0-9]+-dev-ci-(apply)$`), "の形ではありません"},
			{"env の選択肢が空", regoLine(`^arn:aws:iam::\*:role/[a-z0-9]+-()-ci-(apply)$`), "の形ではありません"},
			{"env の選択肢に空の語", regoLine(`^arn:aws:iam::\*:role/[a-z0-9]+-(dev|)-ci-(apply)$`), "の形ではありません"},
			{"env を .* に緩める", regoLine(`^arn:aws:iam::\*:role/[a-z0-9]+-(.*)-ci-(apply)$`), "の形ではありません"},
			{"-ci-( が無い", regoLine(env + "apply$"), "の形ではありません"},
			{"-ci-( が2つ", regoLine(env + "(a)-ci-(apply)$"), "の形ではありません"},
			{"選択肢が末尾に無い", regoLine(env + "(apply)-x$"), "の形ではありません"},
			{"選択肢が入れ子", regoLine(env + "(apply(-identity)?)$"), "の形ではありません"},
			{"空の選択肢", regoSrc("apply||unlock"), `3 行目: role "" を読めません`},
			{"role の形でない選択肢", regoSrc("apply|unlock.*"), `role "unlock.*" を読めません`},
			{"選択肢が重複", regoSrc("apply|apply"), "role apply が重複しています"},
		}
		for _, c := range cases {
			t.Run(c.name+"を弾く", func(t *testing.T) {
				t.Parallel()
				_, err := parseRegoStateWriterRoles([]byte(c.src))
				require.ErrorIs(t, err, ErrStateWriterParse)
				assert.Contains(t, err.Error(), c.reason)
			})
		}
	})
}

func Test_readRoles(t *testing.T) {
	t.Parallel()

	write := func(t *testing.T, body string) string {
		t.Helper()
		repo := t.TempDir()
		path := filepath.Join(repo, filepath.FromSlash(StateWriterRegoFile))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
		return repo
	}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("リポジトリのファイルを読んで解釈する", func(t *testing.T) {
			t.Parallel()
			got, err := readRoles(write(t, regoSrc("apply")), StateWriterRegoFile, parseRegoStateWriterRoles)
			require.NoError(t, err)
			assert.Equal(t, []string{"apply"}, got)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("リポジトリが無ければ弾く", func(t *testing.T) {
			t.Parallel()
			_, err := readRoles(filepath.Join(t.TempDir(), "missing"), StateWriterRegoFile, parseRegoStateWriterRoles)
			require.ErrorIs(t, err, fs.ErrNotExist)
		})

		t.Run("ファイルが無ければ弾く", func(t *testing.T) {
			t.Parallel()
			_, err := readRoles(t.TempDir(), StateWriterSCPFile, parseSCPStateWriterRoles)
			require.ErrorIs(t, err, fs.ErrNotExist)
		})

		t.Run("解釈できないファイルは、パスを添えて弾く", func(t *testing.T) {
			t.Parallel()
			_, err := readRoles(write(t, "package x\n"), StateWriterRegoFile, parseRegoStateWriterRoles)
			require.ErrorIs(t, err, ErrStateWriterParse)
			assert.Contains(t, err.Error(), StateWriterRegoFile)
		})
	})
}

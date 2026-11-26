package main

import (
	"os"
	"strings"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func parseBody(t *testing.T, src string) *hclsyntax.Body {
	t.Helper()

	f, diags := hclsyntax.ParseConfig([]byte(src), "x.tf", hcl.InitialPos)
	require.False(t, diags.HasErrors(), diags.Error())

	body, ok := f.Body.(*hclsyntax.Body)
	require.True(t, ok)

	return body
}

func testHierarchy(t *testing.T) map[string]map[string][]string {
	t.Helper()

	h, err := readOUHierarchy(parseBody(t, ouMainTF))
	require.NoError(t, err)

	return h
}

func Test_readOrganization(t *testing.T) {
	t.Parallel()

	t.Run("3つのファイルから、対応表・OU の階層・メールの形・break-glass の名前を読む", func(t *testing.T) {
		t.Parallel()

		org := testOrg(t)
		assert.Equal(t, []memberAccount{
			{env: "log-archive", ouPath: []string{"Security"}},
			{env: "security", ouPath: []string{"Security"}},
		}, org.members)
		assert.Equal(t, "tabp-break-glass", org.breakGlass)
		assert.True(t, org.emailPattern.MatchString("a@b.c"))
		assert.False(t, org.emailPattern.MatchString("a@b"))
	})

	missing := map[string]string{
		"usecase の main.tf が読めなければ errHCL":      usecaseFile,
		"accounts.tf が読めなければ errHCL":            accountsFile,
		"organization の main.tf が読めなければ errHCL": ouFile,
	}
	for name, file := range missing {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			files := fixtureFiles(usecaseMainTF, ouMainTF, accountsTF)
			delete(files, testUsecase+"/"+file)

			_, err := readOrganization(readFiles(files), testUsecase, testProject)
			require.ErrorIs(t, err, errHCL)
			require.ErrorIs(t, err, os.ErrNotExist)
		})
	}

	t.Run("break-glass の名前が無ければ errBreakGlassName", func(t *testing.T) {
		t.Parallel()

		_, err := readOrganization(readFiles(fixtureFiles("locals {\n  x = 1\n}\n", ouMainTF, accountsTF)), testUsecase, testProject)
		require.ErrorIs(t, err, errBreakGlassName)
	})

	t.Run("OU の resource が無ければ errOUShape", func(t *testing.T) {
		t.Parallel()

		_, err := readOrganization(readFiles(fixtureFiles(usecaseMainTF, "locals {\n  root_id = \"r\"\n}\n", accountsTF)), testUsecase, testProject)
		require.ErrorIs(t, err, errOUShape)
	})

	t.Run("メールの正規表現が無ければ errEmailRegex", func(t *testing.T) {
		t.Parallel()

		accounts := strings.Replace(accountsTF, `can(regex("^[^@\\s]+@[^@\\s]+\\.[^@\\s]+$", local.member_account_emails[each.key]))`, "true", 1)
		_, err := readOrganization(readFiles(fixtureFiles(usecaseMainTF, ouMainTF, accounts)), testUsecase, testProject)
		require.ErrorIs(t, err, errEmailRegex)
	})
}

func Test_parseFile(t *testing.T) {
	t.Parallel()

	t.Run("HCL の body を返す", func(t *testing.T) {
		t.Parallel()

		body, err := parseFile(readFiles(map[string]string{"a.tf": "locals {}\n"}), "a.tf")
		require.NoError(t, err)
		assert.Len(t, body.Blocks, 1)
	})

	t.Run("構文が壊れていれば errHCL", func(t *testing.T) {
		t.Parallel()

		_, err := parseFile(readFiles(map[string]string{"a.tf": "locals {"}), "a.tf")
		require.ErrorIs(t, err, errHCL)
	})

	t.Run("読めなければ、元のエラーを残して errHCL", func(t *testing.T) {
		t.Parallel()

		_, err := parseFile(readFiles(nil), "a.tf")
		require.ErrorIs(t, err, errHCL)
		require.ErrorIs(t, err, os.ErrNotExist)
	})
}

func Test_readBreakGlassName(t *testing.T) {
	t.Parallel()

	t.Run("var.project を入れて評価する", func(t *testing.T) {
		t.Parallel()

		got, err := readBreakGlassName(parseBody(t, usecaseMainTF), "p")
		require.NoError(t, err)
		assert.Equal(t, "p-break-glass", got)
	})

	bad := map[string]string{
		"無ければ errBreakGlassName":                  "locals {\n  a = 1\n}\n",
		"2つの locals に在れば errBreakGlassName":       "locals {\n  break_glass_role_name = \"a\"\n}\nlocals {\n  break_glass_role_name = \"b\"\n}\n",
		"var.project 以外の参照を使えば errBreakGlassName": "locals {\n  break_glass_role_name = \"${var.other}-x\"\n}\n",
		"文字列でなければ errBreakGlassName":              "locals {\n  break_glass_role_name = 1\n}\n",
		"空文字なら errBreakGlassName":                 "locals {\n  break_glass_role_name = \"\"\n}\n",
	}
	for name, src := range bad {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := readBreakGlassName(parseBody(t, src), "p")
			require.ErrorIs(t, err, errBreakGlassName)
		})
	}
}

func Test_readOUHierarchy(t *testing.T) {
	t.Parallel()

	t.Run("Root からの OU の並びを resource と OU の名前ごとに返す", func(t *testing.T) {
		t.Parallel()

		h := testHierarchy(t)
		assert.Equal(t, []string{"Security"}, h["top"]["Security"])
		assert.Equal(t, []string{"Workloads", "NonProd"}, h["workloads"]["NonProd"])
		assert.Len(t, h["top"], 3)
		assert.Len(t, h["workloads"], 2)
	})

	bad := map[string]string{
		"OU の resource が無ければ errOUShape": `locals {}`,
		"for_each が無ければ errOUShape": `resource "aws_organizations_organizational_unit" "a" {
  parent_id = local.root_id
}`,
		"for_each が toset でなければ errOUShape": `resource "aws_organizations_organizational_unit" "a" {
  for_each  = var.ous
  parent_id = local.root_id
}`,
		"for_each に重複があれば errOUShape": `resource "aws_organizations_organizational_unit" "a" {
  for_each  = toset(["X", "X"])
  parent_id = local.root_id
}`,
		"for_each が空なら errOUShape": `resource "aws_organizations_organizational_unit" "a" {
  for_each  = toset([])
  parent_id = local.root_id
}`,
		"parent_id が無ければ errOUShape": `resource "aws_organizations_organizational_unit" "a" {
  for_each = toset(["X"])
}`,
		"parent_id が参照でなければ errOUShape": `resource "aws_organizations_organizational_unit" "a" {
  for_each  = toset(["X"])
  parent_id = "r-1"
}`,
		"parent_id が別の local なら errOUShape": `resource "aws_organizations_organizational_unit" "a" {
  for_each  = toset(["X"])
  parent_id = local.other
}`,
		"親の resource が無ければ errOUShape": `resource "aws_organizations_organizational_unit" "a" {
  for_each  = toset(["X"])
  parent_id = aws_organizations_organizational_unit.b["Y"].id
}`,
		"親の OU が for_each に無ければ errOUShape": `resource "aws_organizations_organizational_unit" "a" {
  for_each  = toset(["X"])
  parent_id = local.root_id
}
resource "aws_organizations_organizational_unit" "b" {
  for_each  = toset(["Y"])
  parent_id = aws_organizations_organizational_unit.a["Z"].id
}`,
		"parent_id が循環すれば errOUShape": `resource "aws_organizations_organizational_unit" "a" {
  for_each  = toset(["X"])
  parent_id = aws_organizations_organizational_unit.b["Y"].id
}
resource "aws_organizations_organizational_unit" "b" {
  for_each  = toset(["Y"])
  parent_id = aws_organizations_organizational_unit.a["X"].id
}`,
	}
	for name, src := range bad {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := readOUHierarchy(parseBody(t, src))
			require.ErrorIs(t, err, errOUShape)
		})
	}
}

func Test_isRootRef(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		expr string
		want bool
	}{
		"local.root_id は Root":       {"local.root_id", true},
		"local の別の名前は Root でない":      {"local.other", false},
		"var.root_id は Root でない":     {"var.root_id", false},
		"local.root_id.x は Root でない": {"local.root_id.x", false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			e, diags := hclsyntax.ParseExpression([]byte(tc.expr), "x", hcl.InitialPos)
			require.False(t, diags.HasErrors())

			st, ok := e.(*hclsyntax.ScopeTraversalExpr)
			require.True(t, ok)
			assert.Equal(t, tc.want, isRootRef(st.Traversal))
		})
	}
}

func Test_ouRef(t *testing.T) {
	t.Parallel()

	traversal := func(t *testing.T, expr string) hcl.Traversal {
		t.Helper()

		e, diags := hclsyntax.ParseExpression([]byte(expr), "x", hcl.InitialPos)
		require.False(t, diags.HasErrors())

		st, ok := e.(*hclsyntax.ScopeTraversalExpr)
		require.True(t, ok)

		return st.Traversal
	}

	t.Run("resource の名前と OU の名前を返す", func(t *testing.T) {
		t.Parallel()

		res, ou, err := ouRef(traversal(t, `aws_organizations_organizational_unit.top["Security"].id`))
		require.NoError(t, err)
		assert.Equal(t, "top", res)
		assert.Equal(t, "Security", ou)
	})

	bad := map[string]string{
		"別の resource なら errNotOURef": `aws_s3_bucket.top["Security"].id`,
		".id でなければ errNotOURef":      `aws_organizations_organizational_unit.top["Security"].arn`,
		"添字が文字列でなければ errNotOURef":    `aws_organizations_organizational_unit.top[0].id`,
		"添字が空文字なら errNotOURef":       `aws_organizations_organizational_unit.top[""].id`,
		"添字が無ければ errNotOURef":        `aws_organizations_organizational_unit.top.id`,
		"5つの段を持てば errNotOURef":       `aws_organizations_organizational_unit.top["S"].id.x`,
	}
	for name, expr := range bad {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, _, err := ouRef(traversal(t, expr))
			require.ErrorIs(t, err, errNotOURef)
		})
	}
}

func Test_literalSet(t *testing.T) {
	t.Parallel()

	attr := func(t *testing.T, expr string) *hclsyntax.Attribute {
		t.Helper()

		return parseBody(t, "a = "+expr+"\n").Attributes["a"]
	}

	t.Run("toset の文字列を並びのまま返す", func(t *testing.T) {
		t.Parallel()

		got, err := literalSet(attr(t, `toset(["B", "A"])`))
		require.NoError(t, err)
		assert.Equal(t, []string{"B", "A"}, got)
	})

	t.Run("属性が無ければ errNotLiteralSet", func(t *testing.T) {
		t.Parallel()

		_, err := literalSet(nil)
		require.ErrorIs(t, err, errNotLiteralSet)
	})

	bad := map[string]string{
		"toset でなければ errNotLiteralSet":  `tolist(["A"])`,
		"tuple でなければ errNotLiteralSet":  `toset(var.a)`,
		"空なら errNotLiteralSet":          `toset([])`,
		"文字列でない要素があれば errNotLiteralSet": `toset([1])`,
		"空文字の要素があれば errNotLiteralSet":   `toset([""])`,
		"重複があれば errNotLiteralSet":       `toset(["A", "A"])`,
		"引数が2つなら errNotLiteralSet":      `toset(["A"], ["B"])`,
	}
	for name, expr := range bad {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := literalSet(attr(t, expr))
			require.ErrorIs(t, err, errNotLiteralSet)
		})
	}
}

func Test_readMemberAccounts(t *testing.T) {
	t.Parallel()

	read := func(t *testing.T, src string) ([]memberAccount, error) {
		t.Helper()

		return readMemberAccounts(parseBody(t, src), testHierarchy(t))
	}

	t.Run("対応表の行を env の昇順で、OU の階層を付けて返す", func(t *testing.T) {
		t.Parallel()

		got, err := read(t, `locals {
  other = 1
}
locals {
  member_accounts = {
    stg           = aws_organizations_organizational_unit.workloads["NonProd"].id
    security      = aws_organizations_organizational_unit.top["Security"].id
    "log-archive" = aws_organizations_organizational_unit.top["Security"].id
  }
}
`)
		require.NoError(t, err)
		assert.Equal(t, []memberAccount{
			{env: "log-archive", ouPath: []string{"Security"}},
			{env: "security", ouPath: []string{"Security"}},
			{env: "stg", ouPath: []string{"Workloads", "NonProd"}},
		}, got)
	})

	cases := map[string]struct {
		src  string
		want error
	}{
		"空の入力なら errNoMemberAccounts":                {"", errNoMemberAccounts},
		"member_accounts が無ければ errNoMemberAccounts": {"locals {\n  x = 1\n}\n", errNoMemberAccounts},
		"行が0件なら errNoMemberAccounts":                {"locals {\n  member_accounts = {}\n}\n", errNoMemberAccounts},
		"object の構文でなければ errMemberAccountsShape":    {"locals {\n  member_accounts = merge({}, {})\n}\n", errMemberAccountsShape},
		"良い行と形の悪い行が混ざれば errMemberAccountsShape": {
			"locals {\n  member_accounts = {\n    a = aws_organizations_organizational_unit.top[\"Security\"].id\n    b = \"ou-1\"\n  }\n}\n",
			errMemberAccountsShape,
		},
		"2つの locals に在れば errMemberAccountsShape": {
			"locals {\n  member_accounts = { a = aws_organizations_organizational_unit.top[\"Security\"].id }\n}\nlocals {\n  member_accounts = { b = aws_organizations_organizational_unit.top[\"Security\"].id }\n}\n",
			errMemberAccountsShape,
		},
		"env が重複すれば errMemberAccountsShape": {
			"locals {\n  member_accounts = {\n    a = aws_organizations_organizational_unit.top[\"Security\"].id\n    \"a\" = aws_organizations_organizational_unit.top[\"Sandbox\"].id\n  }\n}\n",
			errMemberAccountsShape,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := read(t, tc.src)
			require.ErrorIs(t, err, tc.want)
		})
	}
}

func Test_readMemberAccount(t *testing.T) {
	t.Parallel()

	item := func(t *testing.T, line string) hclsyntax.ObjectConsItem {
		t.Helper()

		expr, diags := hclsyntax.ParseExpression([]byte("{\n"+line+"\n}"), "x", hcl.InitialPos)
		require.False(t, diags.HasErrors(), diags.Error())

		obj, ok := expr.(*hclsyntax.ObjectConsExpr)
		require.True(t, ok)
		require.Len(t, obj.Items, 1)

		return obj.Items[0]
	}

	t.Run("裸の key と OU の参照を読み、階層を付ける", func(t *testing.T) {
		t.Parallel()

		got, err := readMemberAccount(item(t, `prd = aws_organizations_organizational_unit.workloads["Prod"].id`), testHierarchy(t))
		require.NoError(t, err)
		assert.Equal(t, memberAccount{env: "prd", ouPath: []string{"Workloads", "Prod"}}, got)
	})

	shapes := map[string]string{
		"key が式なら読まない":                      `(var.x) = aws_organizations_organizational_unit.top["Security"].id`,
		"key が空なら読まない":                      `"" = aws_organizations_organizational_unit.top["Security"].id`,
		"key が数なら読まない":                      `1 = aws_organizations_organizational_unit.top["Security"].id`,
		"値が文字列なら読まない":                       `a = "ou-1"`,
		"値が別の resource の参照なら読まない":           `a = aws_s3_bucket.top["Security"].id`,
		"main.tf に無い OU の resource なら読まない":  `a = aws_organizations_organizational_unit.other["X"].id`,
		"resource の for_each に無い OU なら読まない": `a = aws_organizations_organizational_unit.top["Prod"].id`,
	}
	for name, line := range shapes {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := readMemberAccount(item(t, line), testHierarchy(t))
			require.ErrorIs(t, err, errMemberAccountsShape)
		})
	}
}

func Test_envs(t *testing.T) {
	t.Parallel()

	t.Run("env を並びのまま返す", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, []string{"log-archive", "security"}, envs(testOrg(t).members))
	})
}

func Test_readEmailPattern(t *testing.T) {
	t.Parallel()

	t.Run("ただ1つの regex のリテラルを、HCL のエスケープを解いて正規表現にする", func(t *testing.T) {
		t.Parallel()

		re, err := readEmailPattern(parseBody(t, accountsTF))
		require.NoError(t, err)
		assert.Equal(t, `^[^@\s]+@[^@\s]+\.[^@\s]+$`, re.String())
	})

	bad := map[string]string{
		"regex が無ければ errEmailRegex":       "locals {\n  a = 1\n}\n",
		"regex が2つなら errEmailRegex":       "locals {\n  a = regex(\"a\", \"x\")\n  b = regex(\"b\", \"x\")\n}\n",
		"regex がリテラルでなければ errEmailRegex":  "locals {\n  a = regex(var.p, \"x\")\n}\n",
		"regex の引数が1つなら errEmailRegex":    "locals {\n  a = regex(\"a\")\n}\n",
		"Go の正規表現として壊れていれば errEmailRegex": "locals {\n  a = regex(\"(\", \"x\")\n}\n",
	}
	for name, src := range bad {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := readEmailPattern(parseBody(t, src))
			require.ErrorIs(t, err, errEmailRegex)
		})
	}
}

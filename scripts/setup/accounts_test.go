package main

import (
	"testing"

	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_readMemberAccounts(t *testing.T) {
	t.Parallel()

	t.Run("対応表の行を env の昇順で、OU の階層を付けて返す", func(t *testing.T) {
		t.Parallel()

		src := `resource "x" "y" {}
locals {
  other = 1
}
locals {
  member_accounts = {
    stg = aws_organizations_organizational_unit.workloads["NonProd"].id
    security = aws_organizations_organizational_unit.top["Security"].id
    "log-archive" = aws_organizations_organizational_unit.top["Security"].id
  }
}
`
		got, err := readMemberAccounts([]byte(src), "a.tf")
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
		"HCL として読めなければ errHCL":                      {"locals {", errHCL},
		"member_accounts が無ければ errNoMemberAccounts": {"locals {\n  x = 1\n}\n", errNoMemberAccounts},
		"行が0件なら errNoMemberAccounts":                {"locals {\n  member_accounts = {}\n}\n", errNoMemberAccounts},
		"object の構文でなければ errMemberAccountsShape":    {"locals {\n  member_accounts = merge({}, {})\n}\n", errMemberAccountsShape},
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

			_, err := readMemberAccounts([]byte(tc.src), "a.tf")
			require.ErrorIs(t, err, tc.want)
		})
	}
}

func Test_readMemberAccount(t *testing.T) {
	t.Parallel()

	item := func(t *testing.T, line string) hclsyntax.ObjectConsItem {
		t.Helper()

		expr, diags := hclsyntax.ParseExpression([]byte("{\n"+line+"\n}"), "x", hclPos())
		require.False(t, diags.HasErrors(), diags.Error())

		obj, ok := expr.(*hclsyntax.ObjectConsExpr)
		require.True(t, ok)
		require.Len(t, obj.Items, 1)

		return obj.Items[0]
	}

	t.Run("裸の key と OU の参照を読む", func(t *testing.T) {
		t.Parallel()

		got, err := readMemberAccount(item(t, `prd = aws_organizations_organizational_unit.workloads["Prod"].id`))
		require.NoError(t, err)
		assert.Equal(t, memberAccount{env: "prd", ouPath: []string{"Workloads", "Prod"}}, got)
	})

	shapes := map[string]string{
		"key が式なら読まない":                 `(var.x) = aws_organizations_organizational_unit.top["Security"].id`,
		"key が空なら読まない":                 `"" = aws_organizations_organizational_unit.top["Security"].id`,
		"値が文字列なら読まない":                  `a = "ou-1"`,
		"値が別の resource の参照なら読まない":      `a = aws_s3_bucket.top["Security"].id`,
		"値が .id でなければ読まない":             `a = aws_organizations_organizational_unit.top["Security"].arn`,
		"値の添字が文字列でなければ読まない":            `a = aws_organizations_organizational_unit.top[0].id`,
		"値が添字を持たなければ読まない":              `a = aws_organizations_organizational_unit.top.id`,
		"階層を知らない OU の resource なら読まない": `a = aws_organizations_organizational_unit.other["X"].id`,
	}
	for name, line := range shapes {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := readMemberAccount(item(t, line))
			require.ErrorIs(t, err, errMemberAccountsShape)
		})
	}
}

func Test_envs(t *testing.T) {
	t.Parallel()

	t.Run("env を並びのまま返す", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, []string{"log-archive", "security"}, envs(testMembers(t)))
	})
}

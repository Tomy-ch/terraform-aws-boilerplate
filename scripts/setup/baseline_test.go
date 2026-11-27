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

func parseHCL(t *testing.T, src string) *hclsyntax.Body {
	t.Helper()

	f, diags := hclsyntax.ParseConfig([]byte(src), "x.tf", hcl.InitialPos)
	require.False(t, diags.HasErrors(), diags.Error())

	body, ok := f.Body.(*hclsyntax.Body)
	require.True(t, ok)

	return body
}

func attrOf(t *testing.T, src string) *hclsyntax.Attribute {
	t.Helper()

	return parseHCL(t, src).Attributes["a"]
}

func Test_singleResource(t *testing.T) {
	t.Parallel()

	t.Run("ただ1つなら返す", func(t *testing.T) {
		t.Parallel()

		b, err := singleResource(parseHCL(t, `resource "x" "this" {}`+"\n"+`resource "y" "this" {}`), "x")
		require.NoError(t, err)
		assert.Equal(t, []string{"x", "this"}, b.Labels)
	})

	for name, src := range map[string]string{
		"無ければ errDeclShape":  `resource "y" "this" {}`,
		"2つ在れば errDeclShape": `resource "x" "a" {}` + "\n" + `resource "x" "b" {}`,
		"data は数えない（無いのと同じ）": `data "x" "this" {}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := singleResource(parseHCL(t, src), "x")
			require.ErrorIs(t, err, errDeclShape)
		})
	}
}

func Test_singleLocal(t *testing.T) {
	t.Parallel()

	t.Run("ただ1つなら返す", func(t *testing.T) {
		t.Parallel()

		a, err := singleLocal(parseHCL(t, "locals {\n  x = 1\n}\nlocals {\n  y = 2\n}\n"), "x")
		require.NoError(t, err)
		assert.Equal(t, "x", a.Name)
	})

	for name, src := range map[string]string{
		"無ければ errDeclShape":            "locals {\n  y = 1\n}\n",
		"2つの locals に在れば errDeclShape": "locals {\n  x = 1\n}\nlocals {\n  x = 2\n}\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := singleLocal(parseHCL(t, src), "x")
			require.ErrorIs(t, err, errDeclShape)
		})
	}
}

func Test_literal(t *testing.T) {
	t.Parallel()

	t.Run("リテラルなら値を返す", func(t *testing.T) {
		t.Parallel()

		v, err := literal(attrOf(t, "a = 3\n"))
		require.NoError(t, err)
		assert.Equal(t, "3", v.AsBigFloat().Text('f', 0))
	})

	for name, src := range map[string]string{
		"変数を使えば errDeclShape":  "a = var.x\n",
		"null なら errDeclShape": "a = null\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := literal(attrOf(t, src))
			require.ErrorIs(t, err, errDeclShape)
		})
	}

	t.Run("argument が無ければ errDeclShape", func(t *testing.T) {
		t.Parallel()

		_, err := literal(nil)
		require.ErrorIs(t, err, errDeclShape)
	})
}

func Test_literalString(t *testing.T) {
	t.Parallel()

	t.Run("空でない文字列を返す", func(t *testing.T) {
		t.Parallel()

		s, err := literalString(attrOf(t, `a = "ALL"`+"\n"))
		require.NoError(t, err)
		assert.Equal(t, "ALL", s)
	})

	for name, src := range map[string]string{
		"空なら errDeclShape":        `a = ""` + "\n",
		"数なら errDeclShape":        "a = 1\n",
		"テンプレートの変数は errDeclShape": `a = "${var.x}"` + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := literalString(attrOf(t, src))
			require.ErrorIs(t, err, errDeclShape)
		})
	}
}

func Test_literalStrings(t *testing.T) {
	t.Parallel()

	t.Run("並びのまま返す", func(t *testing.T) {
		t.Parallel()

		s, err := literalStrings(attrOf(t, `a = ["b", "a"]`+"\n"))
		require.NoError(t, err)
		assert.Equal(t, []string{"b", "a"}, s)
	})

	for name, src := range map[string]string{
		"空のリストは errDeclShape":    "a = []\n",
		"重複は errDeclShape":       `a = ["a", "a"]` + "\n",
		"空の要素は errDeclShape":     `a = ["a", ""]` + "\n",
		"文字列でない要素は errDeclShape": `a = ["a", 1]` + "\n",
		"toset は errDeclShape":   `a = toset(["a"])` + "\n",
		"文字列は errDeclShape":      `a = "x"` + "\n",
		"object は errDeclShape":  "a = { x = 1 }\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := literalStrings(attrOf(t, src))
			require.ErrorIs(t, err, errDeclShape)
		})
	}
}

func Test_readOrgShape(t *testing.T) {
	t.Parallel()

	t.Run("組織の機能、policy の種類、IAM の機能、Root の直下の OU を読む", func(t *testing.T) {
		t.Parallel()

		s, err := readOrgShape(readFiles(fixtureFiles(usecaseMainTF, ouMainTF, accountsTF)), testUsecase)
		require.NoError(t, err)
		assert.Equal(t, orgShape{
			featureSet:  "ALL",
			policyTypes: []string{"SERVICE_CONTROL_POLICY", "RESOURCE_CONTROL_POLICY"},
			features:    []string{"RootCredentialsManagement", "RootSessions"},
			topOUs:      []string{"Sandbox", "Security", "Workloads"},
		}, s)
	})

	t.Run("実物の宣言を読める", func(t *testing.T) {
		t.Parallel()

		s, err := readOrgShape(os.ReadFile, "../../usecases/organization-baseline")
		require.NoError(t, err)
		assert.NotEmpty(t, s.policyTypes)
		assert.NotEmpty(t, s.features)
		assert.Contains(t, s.topOUs, "Security")
	})

	t.Run("ファイルが無ければ errHCL", func(t *testing.T) {
		t.Parallel()

		_, err := readOrgShape(readFiles(map[string]string{}), testUsecase)
		require.ErrorIs(t, err, errHCL)
	})

	for name, src := range map[string]string{
		"組織の resource が無ければ errDeclShape":         strings.Replace(ouMainTF, `"aws_organizations_organization"`, `"x"`, 1),
		"IAM の組織の機能の resource が無ければ errDeclShape": strings.Replace(ouMainTF, `"aws_iam_organizations_features"`, `"x"`, 1),
		"feature_set がリテラルでなければ errDeclShape":     strings.Replace(ouMainTF, `"ALL"`, "var.x", 1),
		"enabled_policy_types が空なら errDeclShape":  strings.Replace(ouMainTF, `["SERVICE_CONTROL_POLICY", "RESOURCE_CONTROL_POLICY"]`, "[]", 1),
		"enabled_features が空なら errDeclShape":      strings.Replace(ouMainTF, `["RootCredentialsManagement", "RootSessions"]`, "[]", 1),
		"enabled_features が無ければ errDeclShape":     strings.Replace(ouMainTF, "enabled_features", "x", 1),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := readOrgShape(readFiles(fixtureFiles(usecaseMainTF, src, accountsTF)), testUsecase)
			require.ErrorIs(t, err, errDeclShape)
		})
	}

	t.Run("OU の resource が無ければ errOUShape", func(t *testing.T) {
		t.Parallel()

		src, _, found := strings.Cut(ouMainTF, `resource "aws_organizations_organizational_unit"`)
		require.True(t, found)
		_, err := readOrgShape(readFiles(fixtureFiles(usecaseMainTF, src, accountsTF)), testUsecase)
		require.ErrorIs(t, err, errOUShape)
	})
}

func Test_readBreakGlassShape(t *testing.T) {
	t.Parallel()

	t.Run("user、path、role、password policy を読む", func(t *testing.T) {
		t.Parallel()

		s, err := readBreakGlassShape(readFiles(fixtureFiles(usecaseMainTF, ouMainTF, accountsTF)), testUsecase, testProject)
		require.NoError(t, err)
		assert.Equal(t, breakGlassShape{
			users: []string{"break-glass-1", "break-glass-2"},
			path:  "/break-glass/",
			role:  "tabp-break-glass",
			policy: map[string]string{
				"MinimumPasswordLength": "14", "PasswordReusePrevention": "24", "RequireLowercaseCharacters": "true",
				"RequireUppercaseCharacters": "true", "RequireNumbers": "true", "RequireSymbols": "true",
				"AllowUsersToChangePassword": "true", "MaxPasswordAge": "90", "HardExpiry": "false",
			},
		}, s)
	})

	t.Run("実物の宣言を読め、role の名前が StackSet の名前と同じ読み手で決まる", func(t *testing.T) {
		t.Parallel()

		dir := "../../usecases/organization-baseline"
		s, err := readBreakGlassShape(os.ReadFile, dir, testProject)
		require.NoError(t, err)
		assert.Len(t, s.users, 2)
		assert.NotEmpty(t, s.policy)

		org, err := readOrganization(os.ReadFile, dir, testProject)
		require.NoError(t, err)
		assert.Equal(t, org.breakGlass, s.role)
	})

	for name, src := range map[string]string{
		"path が無ければ errDeclShape":        strings.Replace(usecaseMainTF, "break_glass_path ", "x ", 1),
		"path が / で終わらなければ errDeclShape": strings.Replace(usecaseMainTF, `"/break-glass/"`, `"/break-glass"`, 1),
		"path が / で始まらなければ errDeclShape": strings.Replace(usecaseMainTF, `"/break-glass/"`, `"break-glass/"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := readBreakGlassShape(readFiles(fixtureFiles(src, ouMainTF, accountsTF)), testUsecase, testProject)
			require.ErrorIs(t, err, errDeclShape)
		})
	}

	for name, src := range map[string]string{
		"user_names が無ければ errDeclShape":                     strings.Replace(breakGlassTF, "user_names", "x", 1),
		"user_names が toset でなければ errDeclShape":             strings.Replace(breakGlassTF, `toset(["break-glass-1", "break-glass-2"])`, `["break-glass-1"]`, 1),
		"password policy が無ければ errDeclShape":                strings.Replace(breakGlassTF, "aws_iam_account_password_policy", "x", 1),
		"user の resource が user_names を使わなければ errDeclShape": strings.Replace(breakGlassTF, "for_each = local.user_names", `for_each = toset(["break-glass-1"])`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			files := withFile(fixtureFiles(usecaseMainTF, ouMainTF, accountsTF), breakGlassFile, src)
			_, err := readBreakGlassShape(readFiles(files), testUsecase, testProject)
			require.ErrorIs(t, err, errDeclShape)
		})
	}

	t.Run("break-glass のファイルが無ければ errHCL", func(t *testing.T) {
		t.Parallel()

		files := fixtureFiles(usecaseMainTF, ouMainTF, accountsTF)
		delete(files, testUsecase+"/"+breakGlassFile)
		_, err := readBreakGlassShape(readFiles(files), testUsecase, testProject)
		require.ErrorIs(t, err, errHCL)
	})
}

const allPolicyArgs = "minimum_password_length = 14\npassword_reuse_prevention = 24\nrequire_lowercase_characters = true\n" +
	"require_uppercase_characters = true\nrequire_numbers = true\nrequire_symbols = true\n" +
	"allow_users_to_change_password = true\nmax_password_age = 90\nhard_expiry = false\n"

func Test_readPasswordPolicy(t *testing.T) {
	t.Parallel()

	policy := func(body string) string {
		return "resource \"aws_iam_account_password_policy\" \"this\" {\n" + body + "}\n"
	}

	t.Run("数と真偽を JSON の字面にする", func(t *testing.T) {
		t.Parallel()

		p, err := readPasswordPolicy(parseHCL(t, policy(allPolicyArgs)))
		require.NoError(t, err)
		assert.Equal(t, "90", p["MaxPasswordAge"])
		assert.Equal(t, "true", p["RequireNumbers"])
		assert.Equal(t, "false", p["HardExpiry"])
		assert.Len(t, p, len(passwordPolicyFields))
	})

	for name := range passwordPolicyFields {
		t.Run(name+" が宣言に無ければ errDeclShape（消した argument を確かめずに通さない）", func(t *testing.T) {
			t.Parallel()

			var kept []string

			for _, l := range strings.Split(strings.TrimSpace(allPolicyArgs), "\n") {
				if !strings.HasPrefix(l, name+" ") {
					kept = append(kept, l)
				}
			}

			require.Len(t, kept, len(passwordPolicyFields)-1)

			_, err := readPasswordPolicy(parseHCL(t, policy(strings.Join(kept, "\n")+"\n")))
			require.ErrorIs(t, err, errDeclShape)
			assert.Contains(t, err.Error(), name)
		})
	}

	t.Run("欠けた argument はすべて名前の順に挙げる", func(t *testing.T) {
		t.Parallel()

		src := strings.Replace(strings.Replace(allPolicyArgs, "require_symbols = true\n", "", 1), "hard_expiry = false\n", "", 1)
		_, err := readPasswordPolicy(parseHCL(t, policy(src)))
		require.ErrorIs(t, err, errDeclShape)
		assert.Contains(t, err.Error(), "password policy に hard_expiry・require_symbols がありません")
	})

	for name, body := range map[string]string{
		"argument が無ければ errDeclShape":   "",
		"対応の無い argument は errDeclShape": allPolicyArgs + "x = 1\n",
		"整数でない数は errDeclShape":          strings.Replace(allPolicyArgs, "= 90", "= 1.5", 1),
		"文字列は errDeclShape":             strings.Replace(allPolicyArgs, "= 90", `= "90"`, 1),
		"リテラルでなければ errDeclShape":        strings.Replace(allPolicyArgs, "= 90", "= var.x", 1),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := readPasswordPolicy(parseHCL(t, policy(body)))
			require.ErrorIs(t, err, errDeclShape)
		})
	}
}

func Test_readDelegatedShape(t *testing.T) {
	t.Parallel()

	files := fixtureFiles(usecaseMainTF, ouMainTF, accountsTF)

	t.Run("委任先の env と、output の式を評価した主体を読む", func(t *testing.T) {
		t.Parallel()

		s, err := readDelegatedShape(readFiles(files), testUsecase)
		require.NoError(t, err)
		assert.Equal(t, delegatedShape{env: "security", principals: []string{"cloudtrail.amazonaws.com", "config.amazonaws.com"}}, s)
	})

	t.Run("実物の宣言を読め、委任先が security で、主体が6つ", func(t *testing.T) {
		t.Parallel()

		s, err := readDelegatedShape(os.ReadFile, "../../usecases/organization-baseline")
		require.NoError(t, err)
		assert.Equal(t, delegatedShape{env: "security", principals: []string{
			"access-analyzer.amazonaws.com",
			"cloudtrail.amazonaws.com",
			"config.amazonaws.com",
			"guardduty.amazonaws.com",
			"inspector2.amazonaws.com",
			"securityhub.amazonaws.com",
		}}, s)
	})

	t.Run("委任管理者の宣言が無ければ errHCL", func(t *testing.T) {
		t.Parallel()

		f := withFile(files, delegatedFile, "")
		delete(f, testUsecase+"/"+delegatedFile)
		_, err := readDelegatedShape(readFiles(f), testUsecase)
		require.ErrorIs(t, err, errHCL)
	})

	for name, src := range map[string]string{
		"resource が無ければ errDeclShape":         strings.Replace(delegatedTF, delegatedResource, "x", 1),
		"for_each が toset でなければ errDeclShape": strings.Replace(delegatedTF, `toset(["config.amazonaws.com"])`, "var.x", 1),
		"account_id が無ければ errDeclShape":       strings.Replace(delegatedTF, "account_id ", "x ", 1),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := readDelegatedShape(readFiles(withFile(files, delegatedFile, src)), testUsecase)
			require.ErrorIs(t, err, errDeclShape)
		})
	}

	t.Run("output が無ければ errDeclShape", func(t *testing.T) {
		t.Parallel()

		_, err := readDelegatedShape(readFiles(withFile(files, outputsFile, "")), testUsecase)
		require.ErrorIs(t, err, errDeclShape)
	})

	t.Run("outputs.tf が読めなければ errHCL", func(t *testing.T) {
		t.Parallel()

		f := withFile(files, outputsFile, "")
		delete(f, testUsecase+"/"+outputsFile)
		_, err := readDelegatedShape(readFiles(f), testUsecase)
		require.ErrorIs(t, err, errHCL)
	})
}

func Test_accountRefEnv(t *testing.T) {
	t.Parallel()

	t.Run("index の env を返す", func(t *testing.T) {
		t.Parallel()

		env, err := accountRefEnv(attrOf(t, `a = aws_organizations_account.this["security"].id`+"\n"))
		require.NoError(t, err)
		assert.Equal(t, "security", env)
	})

	for name, src := range map[string]string{
		"別の resource は errDeclShape": `a = aws_iam_user.this["security"].id` + "\n",
		"id でなければ errDeclShape":      `a = aws_organizations_account.this["security"].arn` + "\n",
		"index が無ければ errDeclShape":   "a = aws_organizations_account.this.id\n",
		"参照でなければ errDeclShape":       `a = "222222222222"` + "\n",
		"env が空なら errDeclShape":      `a = aws_organizations_account.this[""].id` + "\n",
		"数の添字は errDeclShape":         "a = aws_organizations_account.this[0].id\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := accountRefEnv(attrOf(t, src))
			require.ErrorIs(t, err, errDeclShape)
		})
	}

	t.Run("argument が無ければ errDeclShape", func(t *testing.T) {
		t.Parallel()

		_, err := accountRefEnv(nil)
		require.ErrorIs(t, err, errDeclShape)
	})
}

func Test_evalDelegatedPrincipals(t *testing.T) {
	t.Parallel()

	out := func(expr string) string {
		return "output \"delegated_admin\" {\n  value = {\n    service_principals = " + expr + "\n  }\n}\n"
	}

	t.Run("resource の key を入れて評価し、昇順で返す", func(t *testing.T) {
		t.Parallel()

		got, err := evalDelegatedPrincipals(parseHCL(t, out(`concat(["z"], keys(aws_organizations_delegated_administrator.this))`)), "this", []string{"b", "a"})
		require.NoError(t, err)
		assert.Equal(t, []string{"a", "b", "z"}, got)
	})

	for name, src := range map[string]string{
		"output が無ければ errDeclShape":             "",
		"value が object でなければ errDeclShape":     "output \"delegated_admin\" {\n  value = 1\n}\n",
		"service_principals が無ければ errDeclShape": "output \"delegated_admin\" {\n  value = {\n    x = 1\n  }\n}\n",
		"ほかの参照を使えば errDeclShape":                out(`concat(["z"], keys(var.x))`),
		"重複は errDeclShape":                      out(`concat(["a"], keys(aws_organizations_delegated_administrator.this))`),
		"空なら errDeclShape":                      out("[]"),
		"output が2つ在れば errDeclShape":            out(`["a"]`) + out(`["b"]`),
		"value が無ければ errDeclShape":              "output \"delegated_admin\" {}\n",
		"文字列でない要素は errDeclShape":                out("[1]"),
		"リストでない結果は errDeclShape":                out(`"x"`),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := evalDelegatedPrincipals(parseHCL(t, src), "this", []string{"a"})
			require.ErrorIs(t, err, errDeclShape)
		})
	}
}

func Test_requireForEachLocal(t *testing.T) {
	t.Parallel()

	t.Run("for_each が local.<name> なら nil", func(t *testing.T) {
		t.Parallel()

		require.NoError(t, requireForEachLocal(parseHCL(t, "resource \"x\" \"this\" {\n  for_each = local.names\n}\n"), "x", "names"))
	})

	for name, src := range map[string]string{
		"別の local は errDeclShape":     "resource \"x\" \"this\" {\n  for_each = local.other\n}\n",
		"var は errDeclShape":          "resource \"x\" \"this\" {\n  for_each = var.names\n}\n",
		"local の添字は errDeclShape":     "resource \"x\" \"this\" {\n  for_each = local[\"names\"]\n}\n",
		"local の下の属性は errDeclShape":   "resource \"x\" \"this\" {\n  for_each = local.names.a\n}\n",
		"リテラルは errDeclShape":          "resource \"x\" \"this\" {\n  for_each = toset([\"a\"])\n}\n",
		"for_each が無ければ errDeclShape": "resource \"x\" \"this\" {}\n",
		"resource が無ければ errDeclShape": "",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			require.ErrorIs(t, requireForEachLocal(parseHCL(t, src), "x", "names"), errDeclShape)
		})
	}
}

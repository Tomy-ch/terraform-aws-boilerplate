package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	noSuchRole = "An error occurred (NoSuchEntity) when calling the GetRole operation"
	getRole    = "iam get-role --role-name " + stackSetsOrgAdminRole
	activate   = "cloudformation activate-organizations-access"
)

func stackSetsWorld(t *testing.T, before, after string, roleBefore, roleAfter bool) *fakeAWS {
	t.Helper()

	role := func(exists bool) awsResult {
		if exists {
			return ok(`{"Role":{}}`)
		}

		return fail(t, noSuchRole)
	}

	f := newFakeAWS(t, map[string]awsResult{
		"cloudformation describe-organizations-access": ok(`{"Status":"` + before + `"}`),
		activate: ok(""),
		getRole:  role(roleBefore),
	})
	f.onWrite(activate, map[string]awsResult{
		"cloudformation describe-organizations-access": ok(`{"Status":"` + after + `"}`),
		getRole: role(roleAfter),
	})

	return f
}

func Test_stackSetsAccess(t *testing.T) {
	t.Parallel()

	t.Run("状態を返す", func(t *testing.T) {
		t.Parallel()

		got, err := stackSetsAccess(testClient(stackSetsWorld(t, "DISABLED", "", false, false)))
		require.NoError(t, err)
		assert.Equal(t, "DISABLED", got)
	})

	for name, stdout := range map[string]string{
		"Status が無ければ errAWSResponse": `{}`,
		"Status が空なら errAWSResponse":  `{"Status":""}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakeAWS(t, map[string]awsResult{"cloudformation describe-organizations-access": ok(stdout)})
			_, err := stackSetsAccess(testClient(f))
			require.ErrorIs(t, err, errAWSResponse)
		})
	}
}

func Test_orgAdminRoleExists(t *testing.T) {
	t.Parallel()

	t.Run("在れば true、NoSuchEntity なら false", func(t *testing.T) {
		t.Parallel()

		yes, err := orgAdminRoleExists(testClient(stackSetsWorld(t, "", "", true, true)))
		require.NoError(t, err)
		assert.True(t, yes)

		no, err := orgAdminRoleExists(testClient(stackSetsWorld(t, "", "", false, false)))
		require.NoError(t, err)
		assert.False(t, no)
	})

	t.Run("ほかの失敗は、無いと読まずに errAWSFailed", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{getRole: fail(t, "An error occurred (AccessDenied)")})
		_, err := orgAdminRoleExists(testClient(f))
		require.ErrorIs(t, err, errAWSFailed)
	})
}

func Test_checkStackSetsAccess(t *testing.T) {
	t.Parallel()

	t.Run("ENABLED で role が在れば nil", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer
		require.NoError(t, checkStackSetsAccess(testClient(stackSetsWorld(t, "ENABLED", "", true, true)), &out))
		assert.Equal(t, "# StackSets の信頼アクセス\n✅ 信頼アクセスが ENABLED である（ENABLED）\n✅ "+stackSetsOrgAdminRole+" が在る\n", out.String())
	})

	t.Run("DISABLED なら NG", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer
		require.ErrorIs(t, checkStackSetsAccess(testClient(stackSetsWorld(t, "DISABLED", "", true, true)), &out), errNG)
		assert.Contains(t, out.String(), "❌ 信頼アクセスが ENABLED である（DISABLED）")
	})

	t.Run("role が無ければ NG", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer
		require.ErrorIs(t, checkStackSetsAccess(testClient(stackSetsWorld(t, "ENABLED", "", false, false)), &out), errNG)
		assert.Contains(t, out.String(), "❌ "+stackSetsOrgAdminRole+" が在る")
	})
}

func Test_enableStackSetsAccess(t *testing.T) {
	t.Parallel()

	writes := []string{"cloudformation activate-organizations-access"}

	t.Run("DISABLED なら、確認を受けて有効にし、確かめ直す", func(t *testing.T) {
		t.Parallel()

		f := stackSetsWorld(t, "DISABLED", "ENABLED", false, true)
		term, out := tty("tabp\n")
		require.NoError(t, enableStackSetsAccess(testOptions(), testClient(f, writes...), term))
		assert.Equal(t, []string{activate}, f.writes())
		assert.Contains(t, out.String(), "✅ 信頼アクセスが ENABLED である")
	})

	t.Run("有効にした後も DISABLED なら errNG", func(t *testing.T) {
		t.Parallel()

		f := stackSetsWorld(t, "DISABLED", "DISABLED", false, true)
		term, _ := tty("tabp\n")
		require.ErrorIs(t, enableStackSetsAccess(testOptions(), testClient(f, writes...), term), errNG)
	})

	t.Run("有効にした後に role が無ければ errNG", func(t *testing.T) {
		t.Parallel()

		f := stackSetsWorld(t, "DISABLED", "ENABLED", false, false)
		term, out := tty("tabp\n")
		require.ErrorIs(t, enableStackSetsAccess(testOptions(), testClient(f, writes...), term), errNG)
		assert.Contains(t, out.String(), "❌ "+stackSetsOrgAdminRole+" が在る")
	})

	t.Run("既に ENABLED なら書かない", func(t *testing.T) {
		t.Parallel()

		f := stackSetsWorld(t, "ENABLED", "", true, true)
		term, out := tty("")
		require.NoError(t, enableStackSetsAccess(testOptions(), testClient(f, writes...), term))
		assert.Empty(t, f.writes())
		assert.Contains(t, out.String(), "既に ENABLED")
	})

	t.Run("DISABLED_PERMANENTLY なら errPrecondition で書かない", func(t *testing.T) {
		t.Parallel()

		f := stackSetsWorld(t, "DISABLED_PERMANENTLY", "", false, false)
		term, _ := tty("tabp\n")
		require.ErrorIs(t, enableStackSetsAccess(testOptions(), testClient(f, writes...), term), errPrecondition)
		assert.Empty(t, f.writes())
	})

	t.Run("確認の入力が違えば書かない", func(t *testing.T) {
		t.Parallel()

		f := stackSetsWorld(t, "DISABLED", "ENABLED", false, true)
		term, _ := tty("y\n")
		require.ErrorIs(t, enableStackSetsAccess(testOptions(), testClient(f, writes...), term), errNotConfirmed)
		assert.Empty(t, f.writes())
	})

	t.Run("端末でなければ書かない", func(t *testing.T) {
		t.Parallel()

		f := stackSetsWorld(t, "DISABLED", "ENABLED", false, true)
		term := newTerminal(strings.NewReader("tabp\n"), &bytes.Buffer{}, false)
		require.ErrorIs(t, enableStackSetsAccess(testOptions(), testClient(f, writes...), term), errNotTerminal)
		assert.Empty(t, f.writes())
	})
}

func principals(names ...string) string {
	parts := make([]string, len(names))
	for i, n := range names {
		parts[i] = `{"ServicePrincipal":"` + n + `"}`
	}

	return `{"EnabledServicePrincipals":[` + strings.Join(parts, ",") + `]}`
}

func enablePrincipal(p string) string {
	return "organizations enable-aws-service-access --service-principal " + p
}

func serviceWorld(t *testing.T, before, after string) *fakeAWS {
	t.Helper()

	f := newFakeAWS(t, map[string]awsResult{"organizations list-aws-service-access-for-organization": ok(before)})
	for _, p := range securityServicePrincipals {
		f.responses[enablePrincipal(p)] = ok("")
	}

	for _, p := range securityServicePrincipals {
		f.onWrite(enablePrincipal(p), map[string]awsResult{"organizations list-aws-service-access-for-organization": ok(after)})
	}

	return f
}

func Test_serviceAccessEnabled(t *testing.T) {
	t.Parallel()

	t.Run("有効な principal を返す", func(t *testing.T) {
		t.Parallel()

		got, err := serviceAccessEnabled(testClient(serviceWorld(t, principals("a", "b"), "")))
		require.NoError(t, err)
		assert.Equal(t, []string{"a", "b"}, got)
	})

	t.Run("有効な principal が無ければ空", func(t *testing.T) {
		t.Parallel()

		got, err := serviceAccessEnabled(testClient(serviceWorld(t, principals(), "")))
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	for name, stdout := range map[string]string{
		"EnabledServicePrincipals が無ければ errAWSResponse": `{}`,
		"ServicePrincipal が空の要素があれば errAWSResponse":     `{"EnabledServicePrincipals":[{}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := serviceAccessEnabled(testClient(serviceWorld(t, stdout, "")))
			require.ErrorIs(t, err, errAWSResponse)
		})
	}
}

func Test_missingPrincipals(t *testing.T) {
	t.Parallel()

	t.Run("一覧のうち有効でないものを返す", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, []string{"access-analyzer.amazonaws.com", "config.amazonaws.com"}, missingPrincipals([]string{"cloudtrail.amazonaws.com", "x"}))
	})

	t.Run("すべて有効なら空", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, missingPrincipals(securityServicePrincipals))
	})
}

func Test_checkServiceAccess(t *testing.T) {
	t.Parallel()

	t.Run("すべて有効なら nil", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer
		require.NoError(t, checkServiceAccess(testClient(serviceWorld(t, principals(securityServicePrincipals...), "")), &out))
		assert.Equal(t, 3, strings.Count(out.String(), "✅"))
	})

	t.Run("1つでも無効なら NG", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer
		require.ErrorIs(t, checkServiceAccess(testClient(serviceWorld(t, principals("cloudtrail.amazonaws.com"), "")), &out), errNG)
		assert.Contains(t, out.String(), "❌ config.amazonaws.com の信頼アクセスが有効である")
	})
}

func Test_enableServiceAccess(t *testing.T) {
	t.Parallel()

	writes := []string{"organizations enable-aws-service-access"}

	t.Run("無効なものだけを、確認を受けてそれぞれ有効にし、確かめ直す", func(t *testing.T) {
		t.Parallel()

		f := serviceWorld(t, principals("cloudtrail.amazonaws.com"), principals(securityServicePrincipals...))
		term, out := tty("tabp\n")
		require.NoError(t, enableServiceAccess(testOptions(), testClient(f, writes...), term))
		assert.Equal(t, []string{enablePrincipal("access-analyzer.amazonaws.com"), enablePrincipal("config.amazonaws.com")}, f.writes())
		assert.Contains(t, out.String(), "  + access-analyzer.amazonaws.com の信頼アクセスを有効にする\n")
		assert.NotContains(t, out.String(), "+ cloudtrail")
		assert.Contains(t, out.String(), "✅ config.amazonaws.com の信頼アクセスが有効である")
	})

	t.Run("有効にした後も一部が無効なら errNG", func(t *testing.T) {
		t.Parallel()

		f := serviceWorld(t, principals(), principals("cloudtrail.amazonaws.com", "config.amazonaws.com"))
		term, out := tty("tabp\n")
		require.ErrorIs(t, enableServiceAccess(testOptions(), testClient(f, writes...), term), errNG)
		assert.Contains(t, out.String(), "❌ access-analyzer.amazonaws.com の信頼アクセスが有効である")
	})

	t.Run("既にすべて有効なら書かない", func(t *testing.T) {
		t.Parallel()

		f := serviceWorld(t, principals(securityServicePrincipals...), "")
		term, _ := tty("")
		require.NoError(t, enableServiceAccess(testOptions(), testClient(f, writes...), term))
		assert.Empty(t, f.writes())
	})

	t.Run("確認の入力が違えば書かない", func(t *testing.T) {
		t.Parallel()

		f := serviceWorld(t, principals(), "")
		term, _ := tty("\n")
		require.ErrorIs(t, enableServiceAccess(testOptions(), testClient(f, writes...), term), errNotConfirmed)
		assert.Empty(t, f.writes())
	})

	t.Run("途中で失敗すれば、残りを有効にしない", func(t *testing.T) {
		t.Parallel()

		f := serviceWorld(t, principals(), "")
		f.responses[enablePrincipal("access-analyzer.amazonaws.com")] = fail(t, "denied")
		term, _ := tty("tabp\n")
		require.ErrorIs(t, enableServiceAccess(testOptions(), testClient(f, writes...), term), errAWSFailed)
		assert.Equal(t, []string{enablePrincipal("access-analyzer.amazonaws.com")}, f.writes())
	})
}

const (
	enableRAM  = "ram enable-sharing-with-aws-organization"
	getRAMRole = "iam get-role --role-name " + ramRole
)

func ramWorld(t *testing.T, before, after string, roleBefore, roleAfter bool) *fakeAWS {
	t.Helper()

	role := func(exists bool) awsResult {
		if exists {
			return ok(`{"Role":{}}`)
		}

		return fail(t, noSuchRole)
	}

	f := newFakeAWS(t, map[string]awsResult{
		"organizations list-aws-service-access-for-organization": ok(before),
		enableRAM:  ok(""),
		getRAMRole: role(roleBefore),
	})
	f.onWrite(enableRAM, map[string]awsResult{
		"organizations list-aws-service-access-for-organization": ok(after),
		getRAMRole: role(roleAfter),
	})

	return f
}

func Test_checkRAMSharing(t *testing.T) {
	t.Parallel()

	t.Run("信頼アクセスが有効で role が在れば nil", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer
		require.NoError(t, checkRAMSharing(testClient(ramWorld(t, principals("cloudtrail.amazonaws.com", ramPrincipal), "", true, true)), &out))
		assert.Equal(t, 2, strings.Count(out.String(), "✅"))
	})

	t.Run("信頼アクセスが無効なら NG", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer
		require.ErrorIs(t, checkRAMSharing(testClient(ramWorld(t, principals("cloudtrail.amazonaws.com"), "", true, true)), &out), errNG)
		assert.Contains(t, out.String(), "❌ "+ramPrincipal+" の信頼アクセスが有効である")
	})

	t.Run("role が無ければ NG", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer
		require.ErrorIs(t, checkRAMSharing(testClient(ramWorld(t, principals(ramPrincipal), "", false, false)), &out), errNG)
		assert.Contains(t, out.String(), "❌ "+ramRole+" が在る")
	})

	t.Run("role の読み取りが NoSuchEntity の外の失敗なら errAWSFailed", func(t *testing.T) {
		t.Parallel()

		f := ramWorld(t, principals(ramPrincipal), "", true, true)
		f.responses[getRAMRole] = fail(t, "denied")
		require.ErrorIs(t, checkRAMSharing(testClient(f), &bytes.Buffer{}), errAWSFailed)
	})

	t.Run("信頼アクセスの応答が読めなければ errAWSResponse", func(t *testing.T) {
		t.Parallel()

		require.ErrorIs(t, checkRAMSharing(testClient(ramWorld(t, `{}`, "", true, true)), &bytes.Buffer{}), errAWSResponse)
	})
}

func Test_enableRAMSharing(t *testing.T) {
	t.Parallel()

	writes := []string{enableRAM}

	t.Run("無効なら、確認を受けて有効にし、確かめ直す", func(t *testing.T) {
		t.Parallel()

		f := ramWorld(t, principals(), principals(ramPrincipal), false, true)
		term, out := tty("tabp\n")
		require.NoError(t, enableRAMSharing(testOptions(), testClient(f, writes...), term))
		assert.Equal(t, []string{enableRAM}, f.writes())
		assert.Contains(t, out.String(), "  + RAM の組織との共有を有効にする")
		assert.Contains(t, out.String(), "✅ "+ramRole+" が在る")
	})

	t.Run("有効にした後も role が無ければ errNG", func(t *testing.T) {
		t.Parallel()

		f := ramWorld(t, principals(), principals(ramPrincipal), false, false)
		term, out := tty("tabp\n")
		require.ErrorIs(t, enableRAMSharing(testOptions(), testClient(f, writes...), term), errNG)
		assert.Contains(t, out.String(), "❌ "+ramRole+" が在る")
	})

	t.Run("既に有効なら書かない", func(t *testing.T) {
		t.Parallel()

		f := ramWorld(t, principals(ramPrincipal), "", true, true)
		term, out := tty("")
		require.NoError(t, enableRAMSharing(testOptions(), testClient(f, writes...), term))
		assert.Empty(t, f.writes())
		assert.Contains(t, out.String(), "既に有効です")
	})

	t.Run("確認の入力が違えば書かない", func(t *testing.T) {
		t.Parallel()

		f := ramWorld(t, principals(), "", false, false)
		term, _ := tty("\n")
		require.ErrorIs(t, enableRAMSharing(testOptions(), testClient(f, writes...), term), errNotConfirmed)
		assert.Empty(t, f.writes())
	})

	t.Run("信頼アクセスの応答が読めなければ、書かずに errAWSResponse", func(t *testing.T) {
		t.Parallel()

		f := ramWorld(t, `{}`, "", false, false)
		term, _ := tty("tabp\n")
		require.ErrorIs(t, enableRAMSharing(testOptions(), testClient(f, writes...), term), errAWSResponse)
		assert.Empty(t, f.writes())
	})

	t.Run("書き込みが失敗すれば errAWSFailed", func(t *testing.T) {
		t.Parallel()

		f := ramWorld(t, principals(), "", false, false)
		f.responses[enableRAM] = fail(t, "denied")
		term, _ := tty("tabp\n")
		require.ErrorIs(t, enableRAMSharing(testOptions(), testClient(f, writes...), term), errAWSFailed)
		assert.Equal(t, []string{enableRAM}, f.writes())
	})
}

const (
	quotaList     = `{"Quotas":[{"QuotaCode":"L-OTHER","QuotaName":"Maximum number of OUs"},{"QuotaCode":"L-ACCOUNTS","QuotaName":"Maximum number of accounts"}]}`
	listQuotas    = "service-quotas list-service-quotas --service-code organizations"
	getQuotaValue = "service-quotas get-service-quota --service-code organizations --quota-code L-ACCOUNTS"
)

func quotaWorld(t *testing.T, quotas, quota string) *fakeAWS {
	t.Helper()

	return newFakeAWS(t, map[string]awsResult{
		listQuotas:                    ok(quotas),
		getQuotaValue:                 ok(quota),
		"organizations list-accounts": ok(accountsJSON),
	})
}

func Test_accountsQuota(t *testing.T) {
	t.Parallel()

	t.Run("名前で quota code を探し、us-east-1 で適用値を読む", func(t *testing.T) {
		t.Parallel()

		f := quotaWorld(t, quotaList, `{"Quota":{"Value":10.0}}`)
		got, err := accountsQuota(testClient(f))
		require.NoError(t, err)
		assert.Equal(t, 10, got)
		require.Len(t, f.calls, 2)

		for _, c := range f.calls {
			assert.Equal(t, "us-east-1", c[len(c)-1])
		}
	})

	for name, list := range map[string]string{
		"名前の quota が無ければ errQuotaNotFound": `{"Quotas":[{"QuotaCode":"L-OTHER","QuotaName":"Maximum number of OUs"}]}`,
		"名前の quota が2つなら errQuotaNotFound": `{"Quotas":[{"QuotaCode":"A","QuotaName":"Maximum number of accounts"},{"QuotaCode":"B","QuotaName":"Maximum number of accounts"}]}`,
		"quota code が空なら errQuotaNotFound": `{"Quotas":[{"QuotaName":"Maximum number of accounts"}]}`,
		"Quotas が無ければ errQuotaNotFound":    `{}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := accountsQuota(testClient(quotaWorld(t, list, `{"Quota":{"Value":10}}`)))
			require.ErrorIs(t, err, errQuotaNotFound)
		})
	}

	for name, quota := range map[string]string{
		"Value が無ければ errAWSResponse":    `{"Quota":{}}`,
		"Quota が無ければ errAWSResponse":    `{}`,
		"Value が0なら errAWSResponse":     `{"Quota":{"Value":0}}`,
		"Value が整数でなければ errAWSResponse": `{"Quota":{"Value":10.5}}`,
		"Value が数でなければ errAWSResponse":  `{"Quota":{"Value":"10"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := accountsQuota(testClient(quotaWorld(t, quotaList, quota)))
			require.ErrorIs(t, err, errAWSResponse)
		})
	}
}

func Test_checkAccountQuota(t *testing.T) {
	t.Parallel()

	check := func(t *testing.T, f *fakeAWS) (string, error) {
		t.Helper()

		var out bytes.Buffer
		err := checkAccountQuota(testOptions(), testClient(f), &out, testOrg(t))

		return out.String(), err
	}

	t.Run("在るアカウントと作るアカウントの計が上限と等しければ通す", func(t *testing.T) {
		t.Parallel()

		out, err := check(t, quotaWorld(t, quotaList, `{"Quota":{"Value":3}}`))
		require.NoError(t, err)
		assert.Contains(t, out, "✅ 在るアカウント 2 と作るアカウント 1（log-archive）の計 3 が、上限 3 以下である")
	})

	t.Run("上限を1つ超えれば NG", func(t *testing.T) {
		t.Parallel()

		out, err := check(t, quotaWorld(t, quotaList, `{"Quota":{"Value":2}}`))
		require.ErrorIs(t, err, errNG)
		assert.Contains(t, out, "❌ 在るアカウント 2 と作るアカウント 1（log-archive）の計 3 が、上限 2 以下である")
	})

	t.Run("quota を読めなければ、NG にせず errAWSResponse", func(t *testing.T) {
		t.Parallel()

		_, err := check(t, quotaWorld(t, quotaList, `{"Quota":{}}`))
		require.ErrorIs(t, err, errAWSResponse)
	})

	t.Run("アカウントが0件の組織は errAWSResponse", func(t *testing.T) {
		t.Parallel()

		f := quotaWorld(t, quotaList, `{"Quota":{"Value":3}}`)
		f.responses["organizations list-accounts"] = ok(`{"Accounts":[]}`)
		_, err := check(t, f)
		require.ErrorIs(t, err, errAWSResponse)
	})
}

const (
	securityAccount  = `{"Id":"222222222222","Name":"tabp-security","State":"ACTIVE"}`
	logArchive       = `{"Id":"333333333333","Name":"tabp-log-archive","State":"ACTIVE"}`
	allAccountsJSON  = `{"Accounts":[{"Id":"111111111111","Name":"tabp-management","State":"ACTIVE"},` + securityAccount + `,` + logArchive + `]}`
	underSecurityKey = "organizations list-accounts-for-parent --parent-id ou-sec"
)

func memberAccountsWorld(t *testing.T, underSecurity, all string) *fakeAWS {
	t.Helper()

	tree := orgTree()
	tree[underSecurityKey] = ok(`{"Accounts":[` + underSecurity + `]}`)
	tree["organizations list-accounts"] = ok(all)

	return newFakeAWS(t, tree)
}

func Test_checkMemberAccounts(t *testing.T) {
	t.Parallel()

	check := func(t *testing.T, f *fakeAWS, o options, org organization) (string, error) {
		t.Helper()

		var out bytes.Buffer
		err := checkMemberAccounts(o, testClient(f), &out, org)

		return out.String(), err
	}

	both := securityAccount + "," + logArchive

	t.Run("env ごとに、期待する OU の直下に ACTIVE で在れば nil", func(t *testing.T) {
		t.Parallel()

		out, err := check(t, memberAccountsWorld(t, both, allAccountsJSON), testOptions(), testOrg(t))
		require.NoError(t, err)
		assert.Contains(t, out, "✅ tabp-security が Security の直下に ACTIVE で在る（ACTIVE）")
		assert.NotContains(t, out, "の ID")
	})

	t.Run("-ids なら、在るアカウントそれぞれの ID を出す", func(t *testing.T) {
		t.Parallel()

		o := testOptions()
		o.showIDs = true
		out, err := check(t, memberAccountsWorld(t, both, allAccountsJSON), o, testOrg(t))
		require.NoError(t, err)
		assert.Contains(t, out, "   log-archive の ID: 333333333333\n")
		assert.Contains(t, out, "   security の ID: 222222222222\n")
	})

	t.Run("OU へ移る前に残ったアカウントは、組織の中に在ると NG に書き、-ids なら ID を出す", func(t *testing.T) {
		t.Parallel()

		o := testOptions()
		o.showIDs = true
		out, err := check(t, memberAccountsWorld(t, securityAccount, allAccountsJSON), o, testOrg(t))
		require.ErrorIs(t, err, errNG)
		assert.Contains(t, out, "❌ tabp-log-archive が Security の直下に ACTIVE で在る（組織には ACTIVE で在るが、Security の直下に無い）")
		assert.Contains(t, out, "   log-archive の ID: 333333333333\n")
	})

	t.Run("組織に無いアカウントは NG にし、ID の行を出さない", func(t *testing.T) {
		t.Parallel()

		o := testOptions()
		o.showIDs = true
		all := `{"Accounts":[{"Id":"111111111111","Name":"tabp-management","State":"ACTIVE"},` + securityAccount + `]}`
		out, err := check(t, memberAccountsWorld(t, securityAccount, all), o, testOrg(t))
		require.ErrorIs(t, err, errNG)
		assert.Contains(t, out, "❌ tabp-log-archive が Security の直下に ACTIVE で在る（組織に無い）")
		assert.NotContains(t, out, "log-archive の ID")
	})

	t.Run("ACTIVE でなければ NG", func(t *testing.T) {
		t.Parallel()

		suspended := `{"Id":"222222222222","Name":"tabp-security","State":"SUSPENDED"},` + logArchive
		out, err := check(t, memberAccountsWorld(t, suspended, allAccountsJSON), testOptions(), testOrg(t))
		require.ErrorIs(t, err, errNG)
		assert.Contains(t, out, "❌ tabp-security が Security の直下に ACTIVE で在る（SUSPENDED）")
	})

	t.Run("別の OU のメンバーは、その OU の直下を読む", func(t *testing.T) {
		t.Parallel()

		org := testOrg(t)
		org.members = []memberAccount{{env: "stg", ouPath: []string{"Workloads", "NonProd"}}}
		f := memberAccountsWorld(t, "", `{"Accounts":[{"Id":"444444444444","Name":"tabp-stg","State":"ACTIVE"}]}`)
		f.responses["organizations list-accounts-for-parent --parent-id ou-np"] = ok(`{"Accounts":[{"Id":"444444444444","Name":"tabp-stg","State":"ACTIVE"}]}`)
		out, err := check(t, f, testOptions(), org)
		require.NoError(t, err)
		assert.Contains(t, out, "✅ tabp-stg が Workloads/NonProd の直下に ACTIVE で在る")
	})

	t.Run("OU が無ければ確認を続けない", func(t *testing.T) {
		t.Parallel()

		org := testOrg(t)
		org.members = []memberAccount{{env: "sandbox", ouPath: []string{"Sandbox"}}}
		_, err := check(t, memberAccountsWorld(t, "", allAccountsJSON), testOptions(), org)
		require.ErrorIs(t, err, errOUNotFound)
	})

	t.Run("OU の直下を読めなければ、確認を続けずに errAWSFailed", func(t *testing.T) {
		t.Parallel()

		f := memberAccountsWorld(t, "", allAccountsJSON)
		f.responses[underSecurityKey] = fail(t, "denied")
		_, err := check(t, f, testOptions(), testOrg(t))
		require.ErrorIs(t, err, errAWSFailed)
	})
}

const (
	listStackSets       = "cloudformation list-stack-sets --status ACTIVE --call-as SELF"
	breakGlassInstances = "cloudformation list-stack-instances --stack-set-name tabp-break-glass --call-as SELF"
)

func instance(account, status string) string {
	return `{"Account":"` + account + `","Region":"ap-northeast-1","Status":"` + status + `","StatusReason":"r","StackInstanceStatus":{"DetailedStatus":"d"}}`
}

func stackInstancesWorld(t *testing.T, sets, instances string) *fakeAWS {
	t.Helper()

	return newFakeAWS(t, map[string]awsResult{
		"organizations list-accounts": ok(allAccountsJSON),
		listStackSets:                 ok(`{"Summaries":[` + sets + `]}`),
		breakGlassInstances:           ok(`{"Summaries":[` + instances + `]}`),
	})
}

func Test_checkStackInstances(t *testing.T) {
	t.Parallel()

	check := func(t *testing.T, f *fakeAWS) (string, error) {
		t.Helper()

		var out bytes.Buffer
		err := checkStackInstances(testOptions(), testClient(f), &out, testOrg(t))

		return out.String(), err
	}

	const glass = `{"StackSetName":"tabp-break-glass"},{"StackSetName":"other-x"}`

	allCurrent := instance("222222222222", "CURRENT") + "," + instance("333333333333", "CURRENT")

	t.Run("break-glass が在り、メンバーのそれぞれが CURRENT の instance を持てば nil。ほかの project の StackSet は見ない", func(t *testing.T) {
		t.Parallel()

		f := stackInstancesWorld(t, glass, allCurrent)
		out, err := check(t, f)
		require.NoError(t, err)
		assert.Contains(t, out, "✅ tabp-break-glass が ACTIVE で在る")
		assert.Contains(t, out, "✅ tabp-break-glass が tabp-log-archive に stack instance を持つ")
		assert.Contains(t, out, "✅ tabp-break-glass の 222222222222・ap-northeast-1 が CURRENT である（CURRENT）")
		assert.NotContains(t, strings.Join(f.joined(), "\n"), "--stack-set-name other-x")
	})

	t.Run("project で始まるほかの StackSet も同じく確かめる", func(t *testing.T) {
		t.Parallel()

		f := stackInstancesWorld(t, glass+`,{"StackSetName":"tabp-bootstrap-read"}`, allCurrent)
		f.responses["cloudformation list-stack-instances --stack-set-name tabp-bootstrap-read --call-as SELF"] = ok(`{"Summaries":[` + instance("222222222222", "CURRENT") + `]}`)
		out, err := check(t, f)
		require.ErrorIs(t, err, errNG)
		assert.Contains(t, out, "❌ tabp-bootstrap-read が tabp-log-archive に stack instance を持つ")
	})

	t.Run("break-glass が無ければ、ほかの StackSet が揃っていても NG", func(t *testing.T) {
		t.Parallel()

		f := stackInstancesWorld(t, `{"StackSetName":"tabp-other"}`, "")
		f.responses["cloudformation list-stack-instances --stack-set-name tabp-other --call-as SELF"] = ok(`{"Summaries":[` + allCurrent + `]}`)
		out, err := check(t, f)
		require.ErrorIs(t, err, errNG)
		assert.Contains(t, out, "❌ tabp-break-glass が ACTIVE で在る")
	})

	t.Run("メンバーの instance が欠ければ NG", func(t *testing.T) {
		t.Parallel()

		out, err := check(t, stackInstancesWorld(t, glass, instance("222222222222", "CURRENT")))
		require.ErrorIs(t, err, errNG)
		assert.Contains(t, out, "❌ tabp-break-glass が tabp-log-archive に stack instance を持つ")
	})

	t.Run("CURRENT でない instance は、詳しい状態と理由を添えて NG", func(t *testing.T) {
		t.Parallel()

		out, err := check(t, stackInstancesWorld(t, glass, instance("222222222222", "CURRENT")+","+instance("333333333333", "OUTDATED")))
		require.ErrorIs(t, err, errNG)
		assert.Contains(t, out, "❌ tabp-break-glass の 333333333333・ap-northeast-1 が CURRENT である（OUTDATED、d、r）")
	})

	t.Run("閉鎖済みのアカウントの instance は、CURRENT でなくても確かめない", func(t *testing.T) {
		t.Parallel()

		f := stackInstancesWorld(t, glass, allCurrent+","+instance("555555555555", "INOPERABLE"))
		f.responses["organizations list-accounts"] = ok(`{"Accounts":[` + securityAccount + `,` + logArchive + `,{"Id":"555555555555","Name":"tabp-old","State":"SUSPENDED"}]}`)
		out, err := check(t, f)
		require.NoError(t, err)
		assert.Contains(t, out, "   tabp-break-glass の 555555555555・ap-northeast-1 は閉鎖済みのアカウントなので確かめない（INOPERABLE）")
	})

	t.Run("メンバーのアカウントが組織に無ければ NG", func(t *testing.T) {
		t.Parallel()

		f := stackInstancesWorld(t, glass, allCurrent)
		f.responses["organizations list-accounts"] = ok(`{"Accounts":[` + securityAccount + `]}`)
		out, err := check(t, f)
		require.ErrorIs(t, err, errNG)
		assert.Contains(t, out, "❌ tabp-break-glass の stack instance を持つ tabp-log-archive が組織に在る")
	})

	for name, world := range map[string]func(t *testing.T) *fakeAWS{
		"StackSet の一覧に Summaries が無ければ errAWSResponse": func(t *testing.T) *fakeAWS {
			f := stackInstancesWorld(t, glass, allCurrent)
			f.responses[listStackSets] = ok(`{}`)

			return f
		},
		"StackSetName が空の要素があれば errAWSResponse": func(t *testing.T) *fakeAWS {
			return stackInstancesWorld(t, `{"StackSetName":""}`, allCurrent)
		},
		"instance の一覧に Summaries が無ければ errAWSResponse": func(t *testing.T) *fakeAWS {
			f := stackInstancesWorld(t, glass, allCurrent)
			f.responses[breakGlassInstances] = ok(`{}`)

			return f
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := check(t, world(t))
			require.ErrorIs(t, err, errAWSResponse)
		})
	}

	for name, key := range map[string]string{
		"StackSet の一覧を読めなければ errAWSFailed": listStackSets,
		"instance の一覧を読めなければ errAWSFailed": breakGlassInstances,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := stackInstancesWorld(t, glass, allCurrent)
			f.responses[key] = fail(t, "denied")
			_, err := check(t, f)
			require.ErrorIs(t, err, errAWSFailed)
		})
	}
}

func Test_checkStackSetInstances(t *testing.T) {
	t.Parallel()

	t.Run("instance が0件なら、メンバーのそれぞれを NG にする", func(t *testing.T) {
		t.Parallel()

		f := stackInstancesWorld(t, "", "")

		var out bytes.Buffer

		r := newReport(&out, "T")
		accounts := []orgAccount{{ID: "222222222222", Name: "tabp-security", State: "ACTIVE"}, {ID: "333333333333", Name: "tabp-log-archive", State: "ACTIVE"}}
		require.NoError(t, checkStackSetInstances(testClient(f), r, "tabp-break-glass", testProject, accounts, testOrg(t).members))
		require.ErrorIs(t, r.result(), errNG)
		assert.Equal(t, 2, strings.Count(out.String(), "❌"))
	})
}

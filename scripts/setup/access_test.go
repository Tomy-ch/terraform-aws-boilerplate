package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const noSuchRole = "An error occurred (NoSuchEntity) when calling the GetRole operation"

func stackSetsWorld(t *testing.T, before, after string, role bool) *fakeAWS {
	t.Helper()

	r := fail(t, noSuchRole)
	if role {
		r = ok(`{"Role":{}}`)
	}

	f := newFakeAWS(t, map[string]awsResult{
		"cloudformation describe-organizations-access":      ok(`{"Status":"` + before + `"}`),
		"cloudformation activate-organizations-access":      ok(""),
		"iam get-role --role-name " + stackSetsOrgAdminRole: r,
	})
	f.afterWrite = map[string]awsResult{
		"cloudformation describe-organizations-access":      ok(`{"Status":"` + after + `"}`),
		"iam get-role --role-name " + stackSetsOrgAdminRole: ok(`{"Role":{}}`),
	}

	return f
}

func Test_stackSetsAccess(t *testing.T) {
	t.Parallel()

	t.Run("状態を返す", func(t *testing.T) {
		t.Parallel()

		got, err := stackSetsAccess(testClient(stackSetsWorld(t, "DISABLED", "", false)))
		require.NoError(t, err)
		assert.Equal(t, "DISABLED", got)
	})
}

func Test_roleExists(t *testing.T) {
	t.Parallel()

	t.Run("在れば true、NoSuchEntity なら false", func(t *testing.T) {
		t.Parallel()

		yes, err := roleExists(testClient(stackSetsWorld(t, "", "", true)), stackSetsOrgAdminRole)
		require.NoError(t, err)
		assert.True(t, yes)

		no, err := roleExists(testClient(stackSetsWorld(t, "", "", false)), stackSetsOrgAdminRole)
		require.NoError(t, err)
		assert.False(t, no)
	})
}

func Test_checkStackSetsAccess(t *testing.T) {
	t.Parallel()

	t.Run("ENABLED で role が在れば nil", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer
		require.NoError(t, checkStackSetsAccess(testClient(stackSetsWorld(t, "ENABLED", "", true)), &out))
		assert.NotContains(t, out.String(), "❌")
	})

	t.Run("DISABLED なら NG", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer
		require.ErrorIs(t, checkStackSetsAccess(testClient(stackSetsWorld(t, "DISABLED", "", true)), &out), errNG)
		assert.Contains(t, out.String(), "❌ 信頼アクセスが ENABLED である（DISABLED）")
	})

	t.Run("role が無ければ NG", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer
		require.ErrorIs(t, checkStackSetsAccess(testClient(stackSetsWorld(t, "ENABLED", "", false)), &out), errNG)
		assert.Contains(t, out.String(), "❌ "+stackSetsOrgAdminRole+" が在る")
	})
}

func Test_enableStackSetsAccess(t *testing.T) {
	t.Parallel()

	writes := entries["stacksets-access"].writes

	t.Run("DISABLED なら、確認を受けて有効にし、確かめ直す", func(t *testing.T) {
		t.Parallel()

		f := stackSetsWorld(t, "DISABLED", "ENABLED", false)
		term, out := tty("tabp\n")
		require.NoError(t, enableStackSetsAccess(testOptions(), testClient(f, writes...), term))
		assert.Equal(t, []string{"cloudformation activate-organizations-access"}, f.writes())
		assert.Contains(t, out.String(), "✅ 信頼アクセスが ENABLED である")
	})

	t.Run("有効にした後も DISABLED なら errNG", func(t *testing.T) {
		t.Parallel()

		f := stackSetsWorld(t, "DISABLED", "DISABLED", false)
		term, _ := tty("tabp\n")
		require.ErrorIs(t, enableStackSetsAccess(testOptions(), testClient(f, writes...), term), errNG)
	})

	t.Run("既に ENABLED なら書かない", func(t *testing.T) {
		t.Parallel()

		f := stackSetsWorld(t, "ENABLED", "", true)
		term, out := tty("")
		require.NoError(t, enableStackSetsAccess(testOptions(), testClient(f, writes...), term))
		assert.Empty(t, f.writes())
		assert.Contains(t, out.String(), "既に ENABLED")
	})

	t.Run("DISABLED_PERMANENTLY なら errPrecondition で書かない", func(t *testing.T) {
		t.Parallel()

		f := stackSetsWorld(t, "DISABLED_PERMANENTLY", "", false)
		term, _ := tty("tabp\n")
		require.ErrorIs(t, enableStackSetsAccess(testOptions(), testClient(f, writes...), term), errPrecondition)
		assert.Empty(t, f.writes())
	})

	t.Run("確認の入力が違えば書かない", func(t *testing.T) {
		t.Parallel()

		f := stackSetsWorld(t, "DISABLED", "ENABLED", false)
		term, _ := tty("y\n")
		require.ErrorIs(t, enableStackSetsAccess(testOptions(), testClient(f, writes...), term), errNotConfirmed)
		assert.Empty(t, f.writes())
	})

	t.Run("端末でなければ書かない", func(t *testing.T) {
		t.Parallel()

		f := stackSetsWorld(t, "DISABLED", "ENABLED", false)
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

func serviceWorld(t *testing.T, before, after string) *fakeAWS {
	t.Helper()

	f := newFakeAWS(t, map[string]awsResult{
		"organizations list-aws-service-access-for-organization": ok(before),
		"organizations enable-aws-service-access":                ok(""),
	})
	f.afterWrite = map[string]awsResult{"organizations list-aws-service-access-for-organization": ok(after)}

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

	writes := entries["service-access"].writes

	t.Run("無効なものだけを、確認を受けて有効にし、確かめ直す", func(t *testing.T) {
		t.Parallel()

		f := serviceWorld(t, principals("cloudtrail.amazonaws.com"), principals(securityServicePrincipals...))
		term, out := tty("tabp\n")
		require.NoError(t, enableServiceAccess(testOptions(), testClient(f, writes...), term))
		assert.Equal(t, []string{"organizations enable-aws-service-access", "organizations enable-aws-service-access"}, f.writes())
		assert.Contains(t, f.calls, []string{"organizations", "enable-aws-service-access", "--service-principal", "config.amazonaws.com", "--output", "json", "--region", testRegion})
		assert.Contains(t, out.String(), "  + access-analyzer.amazonaws.com の信頼アクセスを有効にする\n")
		assert.NotContains(t, out.String(), "+ cloudtrail")
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
		f.responses["organizations enable-aws-service-access"] = fail(t, "denied")
		term, _ := tty("tabp\n")
		require.ErrorIs(t, enableServiceAccess(testOptions(), testClient(f, writes...), term), errAWSFailed)
		assert.Len(t, f.writes(), 1)
	})
}

func quotaWorld(t *testing.T, quotas string, value string) *fakeAWS {
	t.Helper()

	return newFakeAWS(t, map[string]awsResult{
		"service-quotas list-service-quotas --service-code organizations":                       ok(quotas),
		"service-quotas get-service-quota --service-code organizations --quota-code L-ACCOUNTS": ok(`{"Quota":{"Value":` + value + `}}`),
		"organizations list-accounts": ok(accountsJSON),
	})
}

const quotaList = `{"Quotas":[{"QuotaCode":"L-OTHER","QuotaName":"Maximum number of OUs"},{"QuotaCode":"L-ACCOUNTS","QuotaName":"Maximum number of accounts"}]}`

func Test_accountsQuota(t *testing.T) {
	t.Parallel()

	t.Run("名前で quota code を探し、us-east-1 で適用値を読む", func(t *testing.T) {
		t.Parallel()

		f := quotaWorld(t, quotaList, "10.0")
		got, err := accountsQuota(testClient(f))
		require.NoError(t, err)
		assert.Equal(t, 10, got)

		for _, c := range f.calls {
			assert.Equal(t, "us-east-1", c[len(c)-1])
		}
	})

	for name, list := range map[string]string{
		"名前の quota が無ければ errQuotaNotFound": `{"Quotas":[{"QuotaCode":"L-OTHER","QuotaName":"Maximum number of OUs"}]}`,
		"名前の quota が2つなら errQuotaNotFound": `{"Quotas":[{"QuotaCode":"A","QuotaName":"Maximum number of accounts"},{"QuotaCode":"B","QuotaName":"Maximum number of accounts"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := accountsQuota(testClient(quotaWorld(t, list, "10")))
			require.ErrorIs(t, err, errQuotaNotFound)
		})
	}
}

func Test_checkAccountQuota(t *testing.T) {
	t.Parallel()

	check := func(t *testing.T, value string) (string, error) {
		t.Helper()

		var out bytes.Buffer
		err := checkAccountQuota(testOptions(), testClient(quotaWorld(t, quotaList, value)), &out, testMembers(t))

		return out.String(), err
	}

	t.Run("在るアカウントと作るアカウントの計が上限と等しければ通す", func(t *testing.T) {
		t.Parallel()

		out, err := check(t, "3")
		require.NoError(t, err)
		assert.Contains(t, out, "✅ 在るアカウント 2 と作るアカウント 1（log-archive）の計 3 が、上限 3 以下である")
	})

	t.Run("上限を1つ超えれば NG", func(t *testing.T) {
		t.Parallel()

		_, err := check(t, "2")
		require.ErrorIs(t, err, errNG)
	})
}

func memberAccountsWorld(t *testing.T, security string) *fakeAWS {
	t.Helper()

	tree := orgTree()
	tree["organizations list-accounts-for-parent --parent-id ou-sec"] = ok(`{"Accounts":[` + security + `]}`)

	return newFakeAWS(t, tree)
}

func Test_checkMemberAccounts(t *testing.T) {
	t.Parallel()

	both := `{"Id":"222222222222","Name":"tabp-security","State":"ACTIVE"},{"Id":"333333333333","Name":"tabp-log-archive","State":"ACTIVE"}`

	t.Run("env ごとに、期待する OU の直下に ACTIVE で在れば nil", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer
		require.NoError(t, checkMemberAccounts(testOptions(), testClient(memberAccountsWorld(t, both)), &out, testMembers(t)))
		assert.Contains(t, out.String(), "✅ tabp-security が Security の直下に ACTIVE で在る（ACTIVE）")
		assert.NotContains(t, out.String(), "の ID")
	})

	t.Run("-ids ならアカウントの ID を出す", func(t *testing.T) {
		t.Parallel()

		o := testOptions()
		o.showIDs = true

		var out bytes.Buffer
		require.NoError(t, checkMemberAccounts(o, testClient(memberAccountsWorld(t, both)), &out, testMembers(t)))
		assert.Contains(t, out.String(), "   log-archive の ID: 333333333333\n")
	})

	t.Run("無いか ACTIVE でなければ NG", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer
		err := checkMemberAccounts(testOptions(), testClient(memberAccountsWorld(t, `{"Id":"222222222222","Name":"tabp-security","State":"SUSPENDED"}`)), &out, testMembers(t))
		require.ErrorIs(t, err, errNG)
		assert.Contains(t, out.String(), "❌ tabp-log-archive が Security の直下に ACTIVE で在る（無い）")
		assert.Contains(t, out.String(), "❌ tabp-security が Security の直下に ACTIVE で在る（SUSPENDED）")
	})

	t.Run("OU が無ければ確認を続けない", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer
		err := checkMemberAccounts(testOptions(), testClient(newFakeAWS(t, orgTree())), &out, []memberAccount{{env: "sandbox", ouPath: []string{"Sandbox"}}})
		require.ErrorIs(t, err, errOUNotFound)
	})
}

func Test_stateOf(t *testing.T) {
	t.Parallel()

	t.Run("無ければ「無い」、在れば State", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "無い", stateOf(orgAccount{}, false))
		assert.Equal(t, "ACTIVE", stateOf(orgAccount{State: "ACTIVE"}, true))
	})
}

func Test_checkStackInstances(t *testing.T) {
	t.Parallel()

	world := func(t *testing.T, sets, instances string) *fakeAWS {
		t.Helper()

		return newFakeAWS(t, map[string]awsResult{
			"cloudformation list-stack-sets --status ACTIVE --call-as SELF":                        ok(`{"Summaries":[` + sets + `]}`),
			"cloudformation list-stack-instances --stack-set-name tabp-break-glass --call-as SELF": ok(`{"Summaries":[` + instances + `]}`),
		})
	}

	check := func(t *testing.T, f *fakeAWS) (string, error) {
		t.Helper()

		var out bytes.Buffer
		err := checkStackInstances(testOptions(), testClient(f), &out)

		return out.String(), err
	}

	const glass = `{"StackSetName":"tabp-break-glass"},{"StackSetName":"other-x"}`

	t.Run("project の StackSet の instance がすべて CURRENT なら nil、ほかの StackSet は見ない", func(t *testing.T) {
		t.Parallel()

		f := world(t, glass, `{"Account":"222222222222","Region":"ap-northeast-1","Status":"CURRENT"}`)
		out, err := check(t, f)
		require.NoError(t, err)
		assert.Contains(t, out, "✅ tabp-break-glass の 222222222222・ap-northeast-1 が CURRENT である")
		assert.NotContains(t, strings.Join(f.ops(), ","), "other-x")
	})

	t.Run("CURRENT でない instance があれば NG", func(t *testing.T) {
		t.Parallel()

		out, err := check(t, world(t, glass, `{"Account":"2","Region":"r","Status":"CURRENT"},{"Account":"3","Region":"r","Status":"OUTDATED"}`))
		require.ErrorIs(t, err, errNG)
		assert.Contains(t, out, "❌ tabp-break-glass の 3・r が CURRENT である（OUTDATED）")
	})

	t.Run("instance が無ければ NG", func(t *testing.T) {
		t.Parallel()

		out, err := check(t, world(t, glass, ""))
		require.ErrorIs(t, err, errNG)
		assert.Contains(t, out, "❌ tabp-break-glass に stack instance が無い")
	})

	t.Run("project の StackSet が無ければ、確かめた項目が無いので NG", func(t *testing.T) {
		t.Parallel()

		out, err := check(t, world(t, `{"StackSetName":"other-x"}`, ""))
		require.ErrorIs(t, err, errNG)
		assert.Contains(t, out, "❌ 確かめた項目がありません")
	})
}

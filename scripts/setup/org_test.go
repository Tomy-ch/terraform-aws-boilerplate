package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const accountsJSON = `{"Accounts":[
 {"Id":"111111111111","Name":"tabp-management","Email":"m@x.y","State":"ACTIVE","Status":"ACTIVE"},
 {"Id":"222222222222","Name":"tabp-security","Email":"sec@x.y","State":"ACTIVE"}
]}`

func Test_listAccounts(t *testing.T) {
	t.Parallel()

	t.Run("State を読む", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{"organizations list-accounts": ok(accountsJSON)})
		got, err := listAccounts(testClient(f))
		require.NoError(t, err)
		assert.Equal(t, []orgAccount{
			{ID: "111111111111", Name: "tabp-management", Email: "m@x.y", State: "ACTIVE"},
			{ID: "222222222222", Name: "tabp-security", Email: "sec@x.y", State: "ACTIVE"},
		}, got)
	})

	for name, stdout := range map[string]string{
		"アカウントが0件なら errAWSResponse":         `{"Accounts":[]}`,
		"Accounts が無ければ errAWSResponse":     `{}`,
		"State が無いアカウントがあれば errAWSResponse": `{"Accounts":[{"Id":"111111111111","Name":"a","Status":"ACTIVE"}]}`,
		"Id が無いアカウントがあれば errAWSResponse":    `{"Accounts":[{"Name":"a","State":"ACTIVE"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakeAWS(t, map[string]awsResult{"organizations list-accounts": ok(stdout)})
			_, err := listAccounts(testClient(f))
			require.ErrorIs(t, err, errAWSResponse)
		})
	}

	t.Run("失敗はそのまま返す", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{"organizations list-accounts": fail(t, "denied")})
		_, err := listAccounts(testClient(f))
		require.ErrorIs(t, err, errAWSFailed)
	})
}

func Test_validAccounts(t *testing.T) {
	t.Parallel()

	t.Run("欠けが無ければ nil、空の並びも nil", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, validAccounts([]orgAccount{{ID: "1", Name: "a", State: "ACTIVE"}}))
		require.NoError(t, validAccounts(nil))
	})

	t.Run("Name が欠ければ errAWSResponse", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, validAccounts([]orgAccount{{ID: "1", State: "ACTIVE"}}), errAWSResponse)
	})
}

func Test_accountNamed(t *testing.T) {
	t.Parallel()

	accounts := []orgAccount{{Name: "a", ID: "1"}, {Name: "b", ID: "2"}, {Name: "c", ID: "3", State: "SUSPENDED"}, {Name: "c", ID: "4", State: "ACTIVE"}}

	t.Run("名前の一致するアカウントを返す", func(t *testing.T) {
		t.Parallel()

		a, found, err := accountNamed(accounts, "b")
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, "2", a.ID)
	})

	t.Run("無ければ false", func(t *testing.T) {
		t.Parallel()

		_, found, err := accountNamed(accounts, "x")
		require.NoError(t, err)
		assert.False(t, found)
	})

	t.Run("閉鎖済みと閉鎖済みでない同じ名前が在れば、閉鎖済みでないほうを返す", func(t *testing.T) {
		t.Parallel()

		a, found, err := accountNamed(accounts, "c")
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, "4", a.ID)
	})

	t.Run("閉鎖済みだけが1つ在れば、それを返す", func(t *testing.T) {
		t.Parallel()

		a, found, err := accountNamed([]orgAccount{{Name: "c", ID: "3", State: "PENDING_CLOSURE"}}, "c")
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, "PENDING_CLOSURE", a.State)
	})

	for name, accounts := range map[string][]orgAccount{
		"閉鎖済みでない同じ名前が2つ在れば errAccountName": {{Name: "c", State: "ACTIVE"}, {Name: "c", State: "PENDING_ACTIVATION"}},
		"閉鎖済みだけが2つ在れば errAccountName":      {{Name: "c", State: "SUSPENDED"}, {Name: "c", State: "CLOSED"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, _, err := accountNamed(accounts, "c")
			require.ErrorIs(t, err, errAccountName)
		})
	}
}

func Test_isClosed(t *testing.T) {
	t.Parallel()

	cases := map[string]bool{"SUSPENDED": true, "PENDING_CLOSURE": true, "CLOSED": true, "ACTIVE": false, "PENDING_ACTIVATION": false}
	for state, want := range cases {
		t.Run(state+" の判定", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, want, isClosed(orgAccount{State: state}))
		})
	}
}

func Test_accountName(t *testing.T) {
	t.Parallel()

	t.Run("project と env を繋ぐ", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "tabp-log-archive", accountName("tabp", "log-archive"))
	})
}

func orgTree() map[string]awsResult {
	return map[string]awsResult{
		"organizations list-roots": ok(`{"Roots":[{"Id":"r-1"}]}`),
		"organizations list-organizational-units-for-parent --parent-id r-1":   ok(`{"OrganizationalUnits":[{"Id":"ou-sec","Name":"Security"},{"Id":"ou-wl","Name":"Workloads"}]}`),
		"organizations list-organizational-units-for-parent --parent-id ou-wl": ok(`{"OrganizationalUnits":[{"Id":"ou-np","Name":"NonProd"}]}`),
	}
}

func Test_resolveOU(t *testing.T) {
	t.Parallel()

	t.Run("Root から名前でたどる", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, orgTree())
		got, err := resolveOU(testClient(f), []string{"Workloads", "NonProd"})
		require.NoError(t, err)
		assert.Equal(t, "ou-np", got)
		assert.Contains(t, f.joined(), "organizations list-organizational-units-for-parent --parent-id ou-wl")
	})

	t.Run("OU が無ければ errOUNotFound", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, orgTree())
		_, err := resolveOU(testClient(f), []string{"Sandbox"})
		require.ErrorIs(t, err, errOUNotFound)
	})

	t.Run("同じ名前の OU が2つなら errOUNotFound", func(t *testing.T) {
		t.Parallel()

		tree := orgTree()
		tree["organizations list-organizational-units-for-parent --parent-id r-1"] = ok(`{"OrganizationalUnits":[{"Id":"a","Name":"Security"},{"Id":"b","Name":"Security"}]}`)
		f := newFakeAWS(t, tree)
		_, err := resolveOU(testClient(f), []string{"Security"})
		require.ErrorIs(t, err, errOUNotFound)
	})

	t.Run("OU の Id が空なら errAWSResponse", func(t *testing.T) {
		t.Parallel()

		tree := orgTree()
		tree["organizations list-organizational-units-for-parent --parent-id r-1"] = ok(`{"OrganizationalUnits":[{"Name":"Security"}]}`)
		f := newFakeAWS(t, tree)
		_, err := resolveOU(testClient(f), []string{"Security"})
		require.ErrorIs(t, err, errAWSResponse)
	})

	for name, roots := range map[string]string{
		"Root が0件なら errNoRoot":     `{"Roots":[]}`,
		"Root が2件なら errNoRoot":     `{"Roots":[{"Id":"r-1"},{"Id":"r-2"}]}`,
		"Roots が無ければ errNoRoot":    `{}`,
		"Root の Id が空なら errNoRoot": `{"Roots":[{}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakeAWS(t, map[string]awsResult{"organizations list-roots": ok(roots)})
			_, err := resolveOU(testClient(f), []string{"Security"})
			require.ErrorIs(t, err, errNoRoot)
		})
	}

	t.Run("Root を読めなければ errAWSFailed", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{"organizations list-roots": fail(t, "denied")})
		_, err := resolveOU(testClient(f), []string{"Security"})
		require.ErrorIs(t, err, errAWSFailed)
	})

	t.Run("2段目の OU を読めなければ errAWSFailed", func(t *testing.T) {
		t.Parallel()

		tree := orgTree()
		tree["organizations list-organizational-units-for-parent --parent-id ou-wl"] = fail(t, "denied")
		f := newFakeAWS(t, tree)
		_, err := resolveOU(testClient(f), []string{"Workloads", "NonProd"})
		require.ErrorIs(t, err, errAWSFailed)
	})
}

func Test_listAccountsForParent(t *testing.T) {
	t.Parallel()

	t.Run("OU の直下のアカウントを読む", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{"organizations list-accounts-for-parent --parent-id ou-sec": ok(`{"Accounts":[{"Id":"222222222222","Name":"tabp-security","State":"ACTIVE"}]}`)})
		got, err := listAccountsForParent(testClient(f), "ou-sec")
		require.NoError(t, err)
		assert.Equal(t, []orgAccount{{ID: "222222222222", Name: "tabp-security", State: "ACTIVE"}}, got)
	})

	t.Run("0件は直下にアカウントが無いことなので、そのまま返す", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{"organizations list-accounts-for-parent": ok(`{"Accounts":[]}`)})
		got, err := listAccountsForParent(testClient(f), "ou-sec")
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("失敗は errAWSFailed、欠けたアカウントは errAWSResponse", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{"organizations list-accounts-for-parent": fail(t, "denied")})
		_, err := listAccountsForParent(testClient(f), "ou-sec")
		require.ErrorIs(t, err, errAWSFailed)

		g := newFakeAWS(t, map[string]awsResult{"organizations list-accounts-for-parent": ok(`{"Accounts":[{"Name":"a"}]}`)})
		_, err = listAccountsForParent(testClient(g), "ou-sec")
		require.ErrorIs(t, err, errAWSResponse)
	})
}

// managementWorld は、資格情報のアカウントが caller で、組織の管理アカウントが master の状態。
func managementWorld(caller, master string) map[string]awsResult {
	return map[string]awsResult{
		"sts get-caller-identity":             ok(`{"Account":"` + caller + `"}`),
		"organizations describe-organization": ok(`{"Organization":{"MasterAccountId":"` + master + `"}}`),
	}
}

func Test_requireManagementAccount(t *testing.T) {
	t.Parallel()

	t.Run("管理アカウントなら、書き込む先の profile と region を出す", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer
		require.NoError(t, requireManagementAccount(testClient(newFakeAWS(t, managementWorld("111111111111", "111111111111"))), "admin", &out))
		assert.Equal(t, "# 書き込む先: profile admin、region ap-northeast-1、組織の管理アカウント\n", out.String())
	})

	t.Run("管理アカウントでなければ errPrecondition で、書き込む先を出さない", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer
		err := requireManagementAccount(testClient(newFakeAWS(t, managementWorld("222222222222", "111111111111"))), "admin", &out)
		require.ErrorIs(t, err, errPrecondition)
		assert.Empty(t, out.String())
	})

	for name, world := range map[string]map[string]awsResult{
		"資格情報のアカウントが空なら errAWSResponse":   managementWorld("", "111111111111"),
		"管理アカウントの ID が空なら errAWSResponse": managementWorld("111111111111", ""),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := requireManagementAccount(testClient(newFakeAWS(t, world)), "admin", &bytes.Buffer{})
			require.ErrorIs(t, err, errAWSResponse)
		})
	}

	t.Run("資格情報を読めなければ、組織を読まずに errAWSFailed で書き込む先を出さない", func(t *testing.T) {
		t.Parallel()

		world := managementWorld("1", "1")
		world["sts get-caller-identity"] = fail(t, "ExpiredToken")
		f := newFakeAWS(t, world)

		var out bytes.Buffer
		require.ErrorIs(t, requireManagementAccount(testClient(f), "admin", &out), errAWSFailed)
		assert.Equal(t, []string{"sts get-caller-identity"}, f.joined())
		assert.Empty(t, out.String())
	})

	t.Run("Account の無い応答は errAWSResponse", func(t *testing.T) {
		t.Parallel()

		world := managementWorld("1", "1")
		world["sts get-caller-identity"] = ok(`{}`)
		require.ErrorIs(t, requireManagementAccount(testClient(newFakeAWS(t, world)), "admin", &bytes.Buffer{}), errAWSResponse)
	})

	t.Run("組織を読めなければ errAWSFailed", func(t *testing.T) {
		t.Parallel()

		world := managementWorld("1", "1")
		world["organizations describe-organization"] = fail(t, "AWSOrganizationsNotInUseException")
		err := requireManagementAccount(testClient(newFakeAWS(t, world)), "admin", &bytes.Buffer{})
		require.ErrorIs(t, err, errAWSFailed)
	})
}

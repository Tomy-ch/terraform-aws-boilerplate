package main

import (
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

	t.Run("失敗はそのまま返す", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{"organizations list-accounts": fail(t, "denied")})
		_, err := listAccounts(testClient(f))
		require.ErrorIs(t, err, errAWSFailed)
	})
}

func Test_accountNamed(t *testing.T) {
	t.Parallel()

	accounts := []orgAccount{{Name: "a", ID: "1"}, {Name: "b", ID: "2"}}

	t.Run("名前の一致するアカウントを返す", func(t *testing.T) {
		t.Parallel()

		a, found := accountNamed(accounts, "b")
		assert.True(t, found)
		assert.Equal(t, "2", a.ID)
	})

	t.Run("無ければ false", func(t *testing.T) {
		t.Parallel()

		_, found := accountNamed(accounts, "c")
		assert.False(t, found)
	})
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

	t.Run("Root が1つでなければ errNoRoot", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{"organizations list-roots": ok(`{"Roots":[]}`)})
		_, err := resolveOU(testClient(f), []string{"Security"})
		require.ErrorIs(t, err, errNoRoot)
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
}

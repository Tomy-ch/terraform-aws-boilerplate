package main

import (
	"bytes"
	"maps"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	listRoots   = "organizations list-roots"
	rootsJSON   = `{"Roots":[{"Id":"r-1","PolicyTypes":[{"Type":"SERVICE_CONTROL_POLICY","Status":"ENABLED"},{"Type":"RESOURCE_CONTROL_POLICY","Status":"ENABLED"}]}]}`
	topOUsCall  = "organizations list-organizational-units-for-parent --parent-id r-1"
	topOUsJSON  = `{"OrganizationalUnits":[{"Name":"Security"},{"Name":"Workloads"},{"Name":"Sandbox"}]}`
	orgFeatures = "iam list-organizations-features"
	listSSO     = "sso-admin list-instances"
	ssoJSON     = `{"Instances":[{"InstanceArn":"arn:aws:sso:::instance/ssoins-1","IdentityStoreId":"d-1","OwnerAccountId":"111111111111","Status":"ACTIVE"}]}`
	listIDCUser = "identitystore list-users --identity-store-id d-1"
	getUserID   = `identitystore get-user-id --identity-store-id d-1 --alternate-identifier {"UniqueAttribute":{"AttributePath":"userName","AttributeValue":"alice"}}`
)

func orgWorld(t *testing.T, overrides map[string]awsResult) *fakeAWS {
	t.Helper()

	m := map[string]awsResult{
		"organizations describe-organization": ok(`{"Organization":{"FeatureSet":"ALL","MasterAccountId":"111111111111"}}`),
		listRoots:                             ok(rootsJSON),
		topOUsCall:                            ok(topOUsJSON),
		orgFeatures:                           ok(`{"EnabledFeatures":["RootSessions","RootCredentialsManagement"]}`),
	}
	maps.Copy(m, overrides)

	return newFakeAWS(t, m)
}

func Test_sameSet(t *testing.T) {
	t.Parallel()

	t.Run("順序を除いて比べ、重複も数える", func(t *testing.T) {
		t.Parallel()

		assert.True(t, sameSet([]string{"b", "a"}, []string{"a", "b"}))
		assert.True(t, sameSet(nil, []string{}))
		assert.False(t, sameSet([]string{"a"}, []string{"a", "b"}))
		assert.False(t, sameSet([]string{"a", "a"}, []string{"a"}))
		assert.False(t, sameSet(nil, []string{"a"}))
	})
}

func Test_joinOrNone(t *testing.T) {
	t.Parallel()

	t.Run("空なら「なし」、ほかは・で繋ぐ", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, "なし", joinOrNone(nil))
		assert.Equal(t, "a・b", joinOrNone([]string{"a", "b"}))
	})
}

func Test_prefixed(t *testing.T) {
	t.Parallel()

	t.Run("それぞれに - を付ける", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, []string{"-a", "-b"}, prefixed([]string{"a", "b"}))
	})
}

func Test_readRoot(t *testing.T) {
	t.Parallel()

	t.Run("ただ1つの Root を返す", func(t *testing.T) {
		t.Parallel()

		r, err := readRoot(testClient(orgWorld(t, nil)))
		require.NoError(t, err)
		assert.Equal(t, "r-1", r.ID)
		assert.Len(t, r.PolicyTypes, 2)
	})

	for name, stdout := range map[string]string{
		"0件なら errNoRoot":    `{"Roots":[]}`,
		"2件なら errNoRoot":    `{"Roots":[{"Id":"r-1"},{"Id":"r-2"}]}`,
		"Id が空なら errNoRoot": `{"Roots":[{"Id":""}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := readRoot(testClient(orgWorld(t, map[string]awsResult{listRoots: ok(stdout)})))
			require.ErrorIs(t, err, errNoRoot)
		})
	}
}

func Test_checkOrganization(t *testing.T) {
	t.Parallel()

	files := fixtureFiles(usecaseMainTF, ouMainTF, accountsTF)

	t.Run("宣言に一致すれば4項目とも ✅", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer
		require.NoError(t, checkOrganization(testOptionsUC(files), testClient(orgWorld(t, nil)), &out))
		assert.Equal(t, 4, bytes.Count(out.Bytes(), []byte("✅")))
	})

	for name, tc := range map[string]struct {
		key, stdout, want string
	}{
		"FeatureSet が違えば NG": {"organizations describe-organization", `{"Organization":{"FeatureSet":"CONSOLIDATED_BILLING"}}`, "❌ FeatureSet が ALL（今: CONSOLIDATED_BILLING）"},
		"PENDING_ENABLE の種類は有効と数えない": {listRoots, `{"Roots":[{"Id":"r-1","PolicyTypes":[{"Type":"SERVICE_CONTROL_POLICY","Status":"ENABLED"},{"Type":"RESOURCE_CONTROL_POLICY","Status":"PENDING_ENABLE"}]}]}`,
			"❌ Root で ENABLED の policy の種類"},
		"宣言に無い種類が有効なら NG": {listRoots, `{"Roots":[{"Id":"r-1","PolicyTypes":[{"Type":"SERVICE_CONTROL_POLICY","Status":"ENABLED"},{"Type":"RESOURCE_CONTROL_POLICY","Status":"ENABLED"},{"Type":"TAG_POLICY","Status":"ENABLED"}]}]}`,
			"❌ Root で ENABLED の policy の種類"},
		"Root の直下の OU が足りなければ NG":   {topOUsCall, `{"OrganizationalUnits":[{"Name":"Security"}]}`, "❌ Root の直下の OU"},
		"宣言に無い OU が在れば NG":          {topOUsCall, `{"OrganizationalUnits":[{"Name":"Security"},{"Name":"Workloads"},{"Name":"Sandbox"},{"Name":"Extra"}]}`, "❌ Root の直下の OU"},
		"IAM の機能が足りなければ NG":         {orgFeatures, `{"EnabledFeatures":["RootSessions"]}`, "❌ IAM の組織の機能"},
		"Root の直下の OU が0件なら NG":     {topOUsCall, `{"OrganizationalUnits":[]}`, "❌ Root の直下の OU が Sandbox・Security・Workloads（今: なし）"},
		"IAM の組織の機能の欄が無ければ NG":      {orgFeatures, `{}`, "❌ IAM の組織の機能が RootCredentialsManagement・RootSessions（今: なし）"},
		"Root の policy の種類が0件なら NG": {listRoots, `{"Roots":[{"Id":"r-1","PolicyTypes":[]}]}`, "❌ Root で ENABLED の policy の種類"},
		"FeatureSet が無ければ NG":       {"organizations describe-organization", `{"Organization":{}}`, "❌ FeatureSet が ALL（今: ）"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer

			err := checkOrganization(testOptionsUC(files), testClient(orgWorld(t, map[string]awsResult{tc.key: ok(tc.stdout)})), &out)
			require.ErrorIs(t, err, errNG)
			assert.Contains(t, out.String(), tc.want)
			assert.Equal(t, 1, strings.Count(out.String(), "❌"))
			assert.Equal(t, 3, strings.Count(out.String(), "✅"))
		})
	}

	t.Run("宣言を読めなければ aws を起動しない", func(t *testing.T) {
		t.Parallel()

		f := orgWorld(t, nil)
		err := checkOrganization(testOptionsUC(map[string]string{}), testClient(f), &bytes.Buffer{})
		require.ErrorIs(t, err, errHCL)
		assert.Empty(t, f.ops())
	})

	for name, key := range map[string]string{
		"describe-organization の失敗は errAWSFailed で NG に数えない": "organizations describe-organization",
		"list-roots の失敗は errAWSFailed":                       listRoots,
		"OU の読み取りの失敗は errAWSFailed":                          topOUsCall,
		"IAM の組織の機能の読み取りの失敗は errAWSFailed":                   orgFeatures,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := checkOrganization(testOptionsUC(files), testClient(orgWorld(t, map[string]awsResult{key: fail(t, "AccessDenied")})), &bytes.Buffer{})
			require.ErrorIs(t, err, errAWSFailed)
			assert.NotErrorIs(t, err, errNG)
		})
	}
}

func delegatedWorld(t *testing.T, accounts string, admins map[string]string) *fakeAWS {
	t.Helper()

	m := map[string]awsResult{"organizations list-accounts": ok(`{"Accounts":[` + accounts + `]}`)}
	for p, ids := range admins {
		m["organizations list-delegated-administrators --service-principal "+p] = ok(`{"DelegatedAdministrators":[` + ids + `]}`)
	}

	return newFakeAWS(t, m)
}

func Test_checkDelegatedAdmin(t *testing.T) {
	t.Parallel()

	files := fixtureFiles(usecaseMainTF, ouMainTF, accountsTF)
	sec := `{"Id":"222222222222","Name":"tabp-security","State":"ACTIVE"}`
	mgmt := `{"Id":"111111111111","Name":"tabp-admin","State":"ACTIVE"}`
	only := func(id string) string { return `{"Id":"` + id + `"}` }

	t.Run("主体のそれぞれで security のアカウントだけが委任管理者なら ✅", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer

		f := delegatedWorld(t, mgmt+","+sec, map[string]string{"cloudtrail.amazonaws.com": only("222222222222"), "config.amazonaws.com": only("222222222222")})
		require.NoError(t, checkDelegatedAdmin(testOptionsUC(files), testClient(f), &out))
		assert.Equal(t, 2, bytes.Count(out.Bytes(), []byte("✅")))
	})

	for name, admins := range map[string]map[string]string{
		"委任管理者が無ければ NG":        {"cloudtrail.amazonaws.com": "", "config.amazonaws.com": only("222222222222")},
		"別のアカウントなら NG":         {"cloudtrail.amazonaws.com": only("111111111111"), "config.amazonaws.com": only("222222222222")},
		"security のほかにも在れば NG": {"cloudtrail.amazonaws.com": only("222222222222") + "," + only("111111111111"), "config.amazonaws.com": only("222222222222")},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer

			err := checkDelegatedAdmin(testOptionsUC(files), testClient(delegatedWorld(t, mgmt+","+sec, admins)), &out)
			require.ErrorIs(t, err, errNG)
			assert.Contains(t, out.String(), "❌ cloudtrail.amazonaws.com の委任管理者")
		})
	}

	t.Run("委任先のアカウントが無ければ、主体を読まずに NG", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer

		f := delegatedWorld(t, mgmt, nil)
		err := checkDelegatedAdmin(testOptionsUC(files), testClient(f), &out)
		require.ErrorIs(t, err, errNG)
		assert.Contains(t, out.String(), "❌ 委任先のアカウント tabp-security が ACTIVE で在る（今: 無い）")
		assert.Equal(t, []string{"organizations list-accounts"}, f.ops())
	})

	t.Run("委任先のアカウントが ACTIVE でなければ NG", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer

		f := delegatedWorld(t, mgmt+`,{"Id":"222222222222","Name":"tabp-security","State":"SUSPENDED"}`, nil)
		require.ErrorIs(t, checkDelegatedAdmin(testOptionsUC(files), testClient(f), &out), errNG)
		assert.Contains(t, out.String(), "（今: SUSPENDED）")
	})

	t.Run("宣言を読めなければ aws を起動しない", func(t *testing.T) {
		t.Parallel()

		f := delegatedWorld(t, mgmt, nil)
		require.ErrorIs(t, checkDelegatedAdmin(testOptionsUC(map[string]string{}), testClient(f), &bytes.Buffer{}), errHCL)
		assert.Empty(t, f.ops())
	})

	t.Run("2つ目の主体 config だけが NG でも errNG で、cloudtrail は ✅", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer

		f := delegatedWorld(t, mgmt+","+sec, map[string]string{"cloudtrail.amazonaws.com": only("222222222222"), "config.amazonaws.com": only("111111111111")})
		require.ErrorIs(t, checkDelegatedAdmin(testOptionsUC(files), testClient(f), &out), errNG)
		assert.Contains(t, out.String(), "✅ cloudtrail.amazonaws.com の委任管理者")
		assert.Contains(t, out.String(), "❌ config.amazonaws.com の委任管理者")
	})

	t.Run("list-delegated-administrators の失敗は errAWSFailed で NG に数えない", func(t *testing.T) {
		t.Parallel()

		f := delegatedWorld(t, mgmt+","+sec, map[string]string{"cloudtrail.amazonaws.com": only("222222222222")})
		f.responses["organizations list-delegated-administrators --service-principal cloudtrail.amazonaws.com"] = fail(t, "AccessDenied")
		err := checkDelegatedAdmin(testOptionsUC(files), testClient(f), &bytes.Buffer{})
		require.ErrorIs(t, err, errAWSFailed)
		assert.NotErrorIs(t, err, errNG)
	})
}

func Test_readSSOInstances(t *testing.T) {
	t.Parallel()

	t.Run("インスタンスを返す", func(t *testing.T) {
		t.Parallel()

		got, err := readSSOInstances(testClient(newFakeAWS(t, map[string]awsResult{listSSO: ok(ssoJSON)})))
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, "d-1", got[0].IdentityStoreID)
	})

	t.Run("読めなければ errAWSFailed", func(t *testing.T) {
		t.Parallel()

		_, err := readSSOInstances(testClient(newFakeAWS(t, map[string]awsResult{listSSO: fail(t, "AccessDenied")})))
		require.ErrorIs(t, err, errAWSFailed)
	})
}

func Test_managementAccountID(t *testing.T) {
	t.Parallel()

	t.Run("MasterAccountId を返す", func(t *testing.T) {
		t.Parallel()

		id, err := managementAccountID(testClient(orgWorld(t, nil)))
		require.NoError(t, err)
		assert.Equal(t, "111111111111", id)
	})

	t.Run("空なら errAWSResponse", func(t *testing.T) {
		t.Parallel()

		_, err := managementAccountID(testClient(orgWorld(t, map[string]awsResult{"organizations describe-organization": ok(`{"Organization":{}}`)})))
		require.ErrorIs(t, err, errAWSResponse)
	})
}

func Test_checkIdentityCenter(t *testing.T) {
	t.Parallel()

	world := func(t *testing.T, instances string) *fakeAWS {
		t.Helper()

		return orgWorld(t, map[string]awsResult{listSSO: ok(instances)})
	}

	t.Run("管理アカウントの ACTIVE のインスタンスがただ1つなら ✅", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer
		require.NoError(t, checkIdentityCenter(testOptions(), testClient(world(t, ssoJSON)), &out))
		assert.Equal(t, 2, bytes.Count(out.Bytes(), []byte("✅")))
	})

	for name, tc := range map[string]struct{ instances, want string }{
		"インスタンスが無ければ NG":        {`{"Instances":[]}`, "（今: 0 件）"},
		"2つ在れば NG":              {`{"Instances":[{"OwnerAccountId":"111111111111","Status":"ACTIVE"},{"OwnerAccountId":"111111111111","Status":"ACTIVE"}]}`, "（今: 2 件）"},
		"ACTIVE でなければ NG":       {`{"Instances":[{"OwnerAccountId":"111111111111","Status":"CREATE_IN_PROGRESS"}]}`, "❌ インスタンスが ACTIVE"},
		"メンバーアカウントのインスタンスなら NG": {`{"Instances":[{"OwnerAccountId":"222222222222","Status":"ACTIVE"}]}`, "❌ インスタンスの持ち主"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer

			err := checkIdentityCenter(testOptions(), testClient(world(t, tc.instances)), &out)
			require.ErrorIs(t, err, errNG)
			assert.Contains(t, out.String(), tc.want)
			assert.Equal(t, 1, strings.Count(out.String(), "❌"))
		})
	}

	t.Run("管理アカウントを読めなければ errAWSFailed", func(t *testing.T) {
		t.Parallel()

		f := orgWorld(t, map[string]awsResult{listSSO: ok(ssoJSON), "organizations describe-organization": fail(t, "AccessDenied")})
		require.ErrorIs(t, checkIdentityCenter(testOptions(), testClient(f), &bytes.Buffer{}), errAWSFailed)
	})
}

func idcUserWorld(t *testing.T, userID awsResult, users string) *fakeAWS {
	t.Helper()

	return newFakeAWS(t, map[string]awsResult{
		listSSO:     ok(ssoJSON),
		getUserID:   userID,
		listIDCUser: ok(`{"Users":[` + users + `]}`),
	})
}

func Test_checkIdentityCenterUser(t *testing.T) {
	t.Parallel()

	opts := func() options {
		o := testOptionsUC(fixtureFiles(usecaseMainTF, ouMainTF, accountsTF))
		o.user = "alice"

		return o
	}
	found := ok(`{"IdentityStoreId":"d-1","UserId":"90676d1234-aaaa-bbbb"}`)

	t.Run("user が在り、名前に接尾辞が無ければ、user ID を出して ✅", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer
		require.NoError(t, checkIdentityCenterUser(opts(), testClient(idcUserWorld(t, found, `{"UserName":"alice"},{"UserName":"bob"}`)), &out, testOrg(t)))
		assert.Contains(t, out.String(), "✅ user alice が在る（user ID: 90676d1234-aaaa-bbbb）")
	})

	t.Run("user が無ければ NG", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer

		f := idcUserWorld(t, fail(t, "An error occurred (ResourceNotFoundException) when calling the GetUserId operation"), `{"UserName":"bob"}`)
		require.ErrorIs(t, checkIdentityCenterUser(opts(), testClient(f), &out, testOrg(t)), errNG)
		assert.Contains(t, out.String(), "❌ user alice が在る（user ID: 無い）")
	})

	for name, user := range map[string]string{
		"-admin で終わる名前は NG":  "alice-admin",
		"env の接尾辞で終わる名前は NG": "alice-security",
		"複数語の env の接尾辞も NG":  "alice-log-archive",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer

			f := idcUserWorld(t, found, `{"UserName":"alice"},{"UserName":"`+user+`"}`)
			require.ErrorIs(t, checkIdentityCenterUser(opts(), testClient(f), &out, testOrg(t)), errNG)
			assert.Contains(t, out.String(), "❌ user "+user+" の名前が環境の接尾辞")
		})
	}

	t.Run("接尾辞でなく途中に在るだけなら ✅", func(t *testing.T) {
		t.Parallel()

		f := idcUserWorld(t, found, `{"UserName":"alice"},{"UserName":"admin-bob"}`)
		require.NoError(t, checkIdentityCenterUser(opts(), testClient(f), &bytes.Buffer{}, testOrg(t)))
	})

	t.Run("インスタンスが無ければ errPrecondition", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{listSSO: ok(`{"Instances":[]}`)})
		require.ErrorIs(t, checkIdentityCenterUser(opts(), testClient(f), &bytes.Buffer{}, testOrg(t)), errPrecondition)
	})

	t.Run("UserName の欠けた user は ✅ に数えず errAWSResponse", func(t *testing.T) {
		t.Parallel()

		f := idcUserWorld(t, ok(`{"UserId":"u-1"}`), `{"UserName":"alice"},{}`)
		require.ErrorIs(t, checkIdentityCenterUser(opts(), testClient(f), &bytes.Buffer{}, testOrg(t)), errAWSResponse)
	})

	t.Run("インスタンスが ACTIVE でなければ、user を読まずに errPrecondition", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{listSSO: ok(`{"Instances":[{"IdentityStoreId":"d-1","Status":"CREATE_IN_PROGRESS"}]}`)})
		require.ErrorIs(t, checkIdentityCenterUser(opts(), testClient(f), &bytes.Buffer{}, testOrg(t)), errPrecondition)
		assert.Equal(t, []string{listSSO}, f.ops())
	})

	t.Run("get-user-id のほかの失敗は errAWSFailed", func(t *testing.T) {
		t.Parallel()

		f := idcUserWorld(t, fail(t, "AccessDeniedException"), "")
		require.ErrorIs(t, checkIdentityCenterUser(opts(), testClient(f), &bytes.Buffer{}, testOrg(t)), errAWSFailed)
	})

	t.Run("list-users が0件なら NG（接尾辞を見る対象が無い）", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer
		require.ErrorIs(t, checkIdentityCenterUser(opts(), testClient(idcUserWorld(t, found, "")), &out, testOrg(t)), errNG)
		assert.Contains(t, out.String(), "❌ Identity Center に user が在る")
	})

	t.Run("get-user-id が成功でも UserId が無ければ NG", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer
		require.ErrorIs(t, checkIdentityCenterUser(opts(), testClient(idcUserWorld(t, ok(`{"IdentityStoreId":"d-1"}`), `{"UserName":"alice"}`)), &out, testOrg(t)), errNG)
		assert.Contains(t, out.String(), "❌ user alice が在る（user ID: 無い）")
	})

	t.Run("IdentityStoreId が空なら、user を読まずに errPrecondition", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{listSSO: ok(`{"Instances":[{"IdentityStoreId":"","Status":"ACTIVE"}]}`)})
		require.ErrorIs(t, checkIdentityCenterUser(opts(), testClient(f), &bytes.Buffer{}, testOrg(t)), errPrecondition)
		assert.Equal(t, []string{listSSO}, f.ops())
	})

	t.Run("インスタンスの Status が無ければ errPrecondition", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{listSSO: ok(`{"Instances":[{"IdentityStoreId":"d-1"}]}`)})
		require.ErrorIs(t, checkIdentityCenterUser(opts(), testClient(f), &bytes.Buffer{}, testOrg(t)), errPrecondition)
	})
}

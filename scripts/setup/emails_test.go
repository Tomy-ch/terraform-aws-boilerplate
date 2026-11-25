package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	notFound     = "An error occurred (ParameterNotFound) when calling the GetParameter operation"
	desiredValue = `{"log-archive":"la@x.y","security":"sec@x.y"}`
)

// emailsWorld は、parameter が current（空なら無い）で、組織に accountsJSON のアカウントが在る状態。
// 書き込んだ後の get-parameter は after を返す。
func emailsWorld(t *testing.T, current, after string) *fakeAWS {
	t.Helper()

	param := fail(t, notFound)
	if current != "" {
		param = paramResponse(current)
	}

	f := newFakeAWS(t, map[string]awsResult{
		"ssm get-parameter":           param,
		"organizations list-accounts": ok(accountsJSON),
		"ssm put-parameter":           ok(`{"Version":1}`),
	})
	f.afterWrite = map[string]awsResult{"ssm get-parameter": paramResponse(after)}

	return f
}

func Test_emailsParameter(t *testing.T) {
	t.Parallel()

	t.Run("accounts.tf の data source と同じ名前", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, paramName, emailsParameter(testProject))
	})
}

func Test_readEmails(t *testing.T) {
	t.Parallel()

	t.Run("値の JSON を読む", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{"ssm get-parameter --name " + paramName: paramResponse(desiredValue)})
		got, found, err := readEmails(testClient(f), testProject)
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, map[string]string{"log-archive": "la@x.y", "security": "sec@x.y"}, got)
	})

	t.Run("無ければ found が false", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{"ssm get-parameter": fail(t, notFound)})
		_, found, err := readEmails(testClient(f), testProject)
		require.NoError(t, err)
		assert.False(t, found)
	})

	for name, value := range map[string]string{
		"JSON でなければ errParameterUnreadable":   "a@b.c",
		"値が文字列でなければ errParameterUnreadable":   `{"a":1}`,
		"null なら errParameterUnreadable":      "null",
		"object でなければ errParameterUnreadable": `["a@b.c"]`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakeAWS(t, map[string]awsResult{"ssm get-parameter": paramResponse(value)})
			_, found, err := readEmails(testClient(f), testProject)
			require.ErrorIs(t, err, errParameterUnreadable)
			assert.True(t, found)
		})
	}
}

func Test_writeEmails(t *testing.T) {
	t.Parallel()

	desired := map[string]string{"security": "sec@x.y", "log-archive": "la@x.y"}

	t.Run("String の parameter として上書きで置き、読み直す", func(t *testing.T) {
		t.Parallel()

		f := emailsWorld(t, "", desiredValue)
		require.NoError(t, writeEmails(testClient(f, "ssm put-parameter"), testProject, desired))
		assert.Equal(t, []string{"ssm", "put-parameter", "--name", paramName, "--type", "String", "--overwrite", "--value", desiredValue, "--output", "json", "--region", testRegion}, f.calls[0])
		assert.Equal(t, []string{"ssm put-parameter", "ssm get-parameter"}, f.ops())
	})

	t.Run("読み直した値が違えば errVerify", func(t *testing.T) {
		t.Parallel()

		f := emailsWorld(t, "", `{"security":"sec@x.y"}`)
		require.ErrorIs(t, writeEmails(testClient(f, "ssm put-parameter"), testProject, desired), errVerify)
	})

	t.Run("書き込みが落ちれば読み直さない", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{"ssm put-parameter": fail(t, "denied")})
		require.ErrorIs(t, writeEmails(testClient(f, "ssm put-parameter"), testProject, desired), errAWSFailed)
		assert.Equal(t, []string{"ssm put-parameter"}, f.ops())
	})
}

func Test_checkEmailUse(t *testing.T) {
	t.Parallel()

	accounts := []orgAccount{{Name: "tabp-security", Email: "sec@x.y"}, {Name: "tabp-management", Email: "m@x.y"}}

	t.Run("自分の env のアカウントと同じメールは通す", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, checkEmailUse(testProject, map[string]string{"security": "SEC@x.y", "log-archive": "la@x.y"}, accounts))
	})

	t.Run("2つの env が大文字小文字だけ違うメールなら errEmailDuplicate", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, checkEmailUse(testProject, map[string]string{"security": "a@x.y", "log-archive": "A@x.y"}, nil), errEmailDuplicate)
	})

	t.Run("ほかのアカウントのメールなら errEmailInUse", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, checkEmailUse(testProject, map[string]string{"log-archive": "m@x.y"}, accounts), errEmailInUse)
	})
}

func Test_putMemberEmails(t *testing.T) {
	t.Parallel()

	put := func(t *testing.T, f *fakeAWS, input string) (string, error) {
		t.Helper()

		term, out := tty(input)
		err := putMemberEmails(testOptions(), testClient(f, "ssm put-parameter"), term, testMembers(t))

		return out.String(), err
	}

	t.Run("parameter が無ければ、env ごとに受けたメールで置き、読み直す", func(t *testing.T) {
		t.Parallel()

		f := emailsWorld(t, "", desiredValue)
		out, err := put(t, f, "la@x.y\nsec@x.y\ntabp\n")
		require.NoError(t, err)
		assert.Equal(t, []string{"ssm put-parameter"}, f.writes())
		assert.Contains(t, out, "log-archive のメール: ")
		assert.Contains(t, out, "  + log-archive: la@x.y\n")
		assert.Contains(t, out, "✅ "+paramName+" を置き")
	})

	t.Run("空の入力は今の値を使い、変わった env だけを差分に示す", func(t *testing.T) {
		t.Parallel()

		f := emailsWorld(t, `{"log-archive":"old@x.y","security":"sec@x.y"}`, desiredValue)
		out, err := put(t, f, "la@x.y\n\ntabp\n")
		require.NoError(t, err)
		assert.Contains(t, out, "security のメール（空なら今の sec@x.y）: ")
		assert.Contains(t, out, "  ~ log-archive: old@x.y → la@x.y\n")
		assert.Contains(t, out, "    security: sec@x.y\n")
		assert.Equal(t, []string{"ssm put-parameter"}, f.writes())
	})

	t.Run("対応表に無い key は消える差分として示す", func(t *testing.T) {
		t.Parallel()

		f := emailsWorld(t, `{"dev":"d@x.y","log-archive":"la@x.y","security":"sec@x.y"}`, desiredValue)
		out, err := put(t, f, "\n\ntabp\n")
		require.NoError(t, err)
		assert.Contains(t, out, "  - dev: d@x.y\n")
	})

	t.Run("既に入力どおりなら、確認を問わずに書かない", func(t *testing.T) {
		t.Parallel()

		f := emailsWorld(t, desiredValue, desiredValue)
		out, err := put(t, f, "\n\n")
		require.NoError(t, err)
		assert.Empty(t, f.writes())
		assert.Contains(t, out, "既に入力どおり")
		assert.NotContains(t, out, "project の名前")
	})

	t.Run("今の値が読めなければ、知らせてすべてを入れ直させる", func(t *testing.T) {
		t.Parallel()

		f := emailsWorld(t, "broken", desiredValue)
		out, err := put(t, f, "la@x.y\nsec@x.y\ntabp\n")
		require.NoError(t, err)
		assert.Contains(t, out, "今の parameter の値を読めない")
		assert.Contains(t, out, "log-archive のメール: ")
	})

	refused := map[string]struct {
		input string
		want  error
	}{
		"確認の入力が project の名前でなければ errNotConfirmed":     {"la@x.y\nsec@x.y\nyes\n", errNotConfirmed},
		"メールの形でなければ errEmailShape":                    {"la-at-x\n", errEmailShape},
		"空の入力で今の値も無ければ errEmailShape":                 {"\n", errEmailShape},
		"2つの env に同じメールなら errEmailDuplicate":          {"SEC@x.y\nsec@x.y\n", errEmailDuplicate},
		"ほかのアカウントのメールなら errEmailInUse":                {"m@x.y\nsec@x.y\n", errEmailInUse},
		"在るアカウントのメールを変えるなら errEmailChangeNeedsUpdate": {"la@x.y\nnew@x.y\n", errEmailChangeNeedsUpdate},
		"入力が途中で尽きれば errNoInput":                       {"la@x.y\n", errNoInput},
	}
	for name, tc := range refused {
		t.Run(name+"、書かない", func(t *testing.T) {
			t.Parallel()

			f := emailsWorld(t, "", desiredValue)
			_, err := put(t, f, tc.input)
			require.ErrorIs(t, err, tc.want)
			assert.Empty(t, f.writes())
		})
	}

	t.Run("端末でなければ、問わずに errNotTerminal で書かない", func(t *testing.T) {
		t.Parallel()

		f := emailsWorld(t, "", desiredValue)

		var out bytes.Buffer

		err := putMemberEmails(testOptions(), testClient(f, "ssm put-parameter"), newTerminal(strings.NewReader("la@x.y\nsec@x.y\ntabp\n"), &out, false), testMembers(t))
		require.ErrorIs(t, err, errNotTerminal)
		assert.Empty(t, f.writes())
		assert.NotContains(t, out.String(), "のメール")
	})

	t.Run("組織を読めなければ、問わずに止める", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{"ssm get-parameter": fail(t, notFound), "organizations list-accounts": fail(t, "denied")})
		out, err := put(t, f, "la@x.y\nsec@x.y\ntabp\n")
		require.ErrorIs(t, err, errAWSFailed)
		assert.Empty(t, out)
	})
}

func Test_printEmailDiff(t *testing.T) {
	t.Parallel()

	t.Run("足す・消す・変える・変えないを key の昇順で出す", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer

		printEmailDiff(&out, "N", map[string]string{"a": "1", "b": "2", "c": "3"}, map[string]string{"b": "2", "c": "4", "d": "5"})
		assert.Equal(t, "# N に書き込む差分\n  - a: 1\n    b: 2\n  ~ c: 3 → 4\n  + d: 5\n", out.String())
	})
}

func Test_checkMemberEmails(t *testing.T) {
	t.Parallel()

	check := func(t *testing.T, f *fakeAWS) (string, error) {
		t.Helper()

		var out bytes.Buffer
		err := checkMemberEmails(testOptions(), testClient(f), &out, testMembers(t))

		return out.String(), err
	}

	t.Run("key が一致し、形が合い、在るアカウントのメールと一致すれば nil", func(t *testing.T) {
		t.Parallel()

		out, err := check(t, emailsWorld(t, desiredValue, ""))
		require.NoError(t, err)
		assert.NotContains(t, out, "❌")
		assert.Contains(t, out, "✅ security の値が、既に在るアカウントのメールと一致する")
	})

	ngs := map[string]struct {
		value string
		line  string
	}{
		"parameter が無ければ NG":   {"", "❌ parameter が無い"},
		"値が読めなければ NG":          {"broken", "❌ 値が文字列の JSON object ではない"},
		"key が足りなければ NG":       {`{"security":"sec@x.y"}`, "❌ key が対応表の env と一致する（対応表: log-archive・security、parameter: security）"},
		"key が余れば NG":          {`{"dev":"d@x.y","log-archive":"la@x.y","security":"sec@x.y"}`, "❌ key が対応表の env と一致する"},
		"値がメールの形でなければ NG":      {`{"log-archive":"la","security":"sec@x.y"}`, "❌ log-archive の値がメールの形である"},
		"在るアカウントのメールと違えば NG":   {`{"log-archive":"la@x.y","security":"other@x.y"}`, "❌ security の値が、既に在るアカウントのメールと一致する"},
		"ほかのアカウントのメールと重なれば NG": {`{"log-archive":"m@x.y","security":"sec@x.y"}`, "❌ メールが env の間で重ならず"},
	}
	for name, tc := range ngs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			out, err := check(t, emailsWorld(t, tc.value, ""))
			require.ErrorIs(t, err, errNG)
			assert.Contains(t, out, tc.line)
		})
	}
}

// updateWorld は、security のアカウントの主のメールが primaryBefore で、信頼アクセスが trusted の状態。
func updateWorld(t *testing.T, primaryBefore, primaryAfter, trusted string) *fakeAWS {
	t.Helper()

	f := newFakeAWS(t, map[string]awsResult{
		"ssm get-parameter":                                      paramResponse(desiredValue),
		"organizations list-accounts":                            ok(accountsJSON),
		"account get-primary-email --account-id 222222222222":    ok(`{"PrimaryEmail":"` + primaryBefore + `"}`),
		"organizations list-aws-service-access-for-organization": ok(`{"EnabledServicePrincipals":[` + trusted + `]}`),
		"organizations enable-aws-service-access":                ok(""),
		"account start-primary-email-update":                     ok(`{"Status":"PENDING"}`),
		"account accept-primary-email-update":                    ok(`{"Status":"ACCEPTED"}`),
		"ssm put-parameter":                                      ok(`{"Version":2}`),
	})
	f.afterWrite = map[string]awsResult{
		"ssm get-parameter": paramResponse(`{"log-archive":"la@x.y","security":"new@x.y"}`),
		"account get-primary-email --account-id 222222222222": ok(`{"PrimaryEmail":"` + primaryAfter + `"}`),
	}

	return f
}

var updateWrites = entries["member-email-update"].writes

func Test_updateMemberEmail(t *testing.T) {
	t.Parallel()

	update := func(t *testing.T, f *fakeAWS, env, input string) (string, error) {
		t.Helper()

		o := testOptions()
		o.env = env
		term, out := tty(input)
		err := updateMemberEmail(o, testClient(f, updateWrites...), term, testMembers(t))

		return out.String(), err
	}

	const trustedAccount = `{"ServicePrincipal":"account.amazonaws.com"}`

	t.Run("AWS の側を変え、確認コードで確定し、その後に parameter を合わせる", func(t *testing.T) {
		t.Parallel()

		f := updateWorld(t, "sec@x.y", "new@x.y", trustedAccount)
		out, err := update(t, f, "security", "new@x.y\ntabp\nABC123\n")
		require.NoError(t, err)
		assert.Equal(t, []string{"account start-primary-email-update", "account accept-primary-email-update", "ssm put-parameter"}, f.writes())
		assert.Contains(t, f.calls, []string{"account", "accept-primary-email-update", "--account-id", "222222222222", "--primary-email", "new@x.y", "--otp", "ABC123", "--output", "json", "--region", testRegion})
		assert.Contains(t, out, "  ~ security: sec@x.y → new@x.y\n")
		assert.NotContains(t, out, "222222222222")
	})

	t.Run("Account Management の信頼アクセスが無ければ、先に有効にする", func(t *testing.T) {
		t.Parallel()

		f := updateWorld(t, "sec@x.y", "new@x.y", "")
		out, err := update(t, f, "security", "new@x.y\ntabp\nABC123\n")
		require.NoError(t, err)
		assert.Equal(t, []string{"organizations enable-aws-service-access", "account start-primary-email-update", "account accept-primary-email-update", "ssm put-parameter"}, f.writes())
		assert.Contains(t, f.calls, []string{"organizations", "enable-aws-service-access", "--service-principal", "account.amazonaws.com", "--output", "json", "--region", testRegion})
		assert.Contains(t, out, "信頼アクセスを account.amazonaws.com に有効にする")
	})

	t.Run("AWS の側が既に新しいメールなら、parameter だけを合わせる", func(t *testing.T) {
		t.Parallel()

		f := updateWorld(t, "new@x.y", "new@x.y", "")
		_, err := update(t, f, "security", "new@x.y\ntabp\n")
		require.NoError(t, err)
		assert.Equal(t, []string{"ssm put-parameter"}, f.writes())
	})

	t.Run("AWS の側も parameter も既に新しいメールなら書かない", func(t *testing.T) {
		t.Parallel()

		f := updateWorld(t, "sec@x.y", "sec@x.y", trustedAccount)
		out, err := update(t, f, "security", "sec@x.y\n")
		require.NoError(t, err)
		assert.Empty(t, f.writes())
		assert.Contains(t, out, "既に sec@x.y です")
	})

	t.Run("確定の後に主のメールが変わっていなければ、parameter を書かずに errVerify", func(t *testing.T) {
		t.Parallel()

		f := updateWorld(t, "sec@x.y", "sec@x.y", trustedAccount)
		_, err := update(t, f, "security", "new@x.y\ntabp\nABC123\n")
		require.ErrorIs(t, err, errVerify)
		assert.NotContains(t, f.writes(), "ssm put-parameter")
	})

	t.Run("確認コードの形が合わなければ、確定せずに errOTPShape", func(t *testing.T) {
		t.Parallel()

		f := updateWorld(t, "sec@x.y", "new@x.y", trustedAccount)
		_, err := update(t, f, "security", "new@x.y\ntabp\n12\n")
		require.ErrorIs(t, err, errOTPShape)
		assert.Equal(t, []string{"account start-primary-email-update"}, f.writes())
	})

	t.Run("確認の入力が project の名前でなければ書かない", func(t *testing.T) {
		t.Parallel()

		f := updateWorld(t, "sec@x.y", "new@x.y", "")
		_, err := update(t, f, "security", "new@x.y\nno\n")
		require.ErrorIs(t, err, errNotConfirmed)
		assert.Empty(t, f.writes())
	})

	preconditions := map[string]struct {
		env   string
		world func(t *testing.T) *fakeAWS
	}{
		"env が対応表に無ければ": {"dev", func(t *testing.T) *fakeAWS { return newFakeAWS(t, nil) }},
		"parameter が無ければ": {"security", func(t *testing.T) *fakeAWS {
			return newFakeAWS(t, map[string]awsResult{"ssm get-parameter": fail(t, notFound)})
		}},
		"アカウントがまだ無ければ": {"log-archive", func(t *testing.T) *fakeAWS { return updateWorld(t, "", "", "") }},
		"アカウントが ACTIVE でなければ": {"security", func(t *testing.T) *fakeAWS {
			f := updateWorld(t, "", "", "")
			f.responses["organizations list-accounts"] = ok(`{"Accounts":[{"Id":"222222222222","Name":"tabp-security","State":"SUSPENDED"}]}`)

			return f
		}},
	}
	for name, tc := range preconditions {
		t.Run(name+"、errPrecondition で書かない", func(t *testing.T) {
			t.Parallel()

			f := tc.world(t)
			_, err := update(t, f, tc.env, "new@x.y\ntabp\nABC123\n")
			require.ErrorIs(t, err, errPrecondition)
			assert.Empty(t, f.writes())
		})
	}

	t.Run("ほかのアカウントのメールなら errEmailInUse で書かない", func(t *testing.T) {
		t.Parallel()

		f := updateWorld(t, "sec@x.y", "", "")
		_, err := update(t, f, "security", "m@x.y\ntabp\n")
		require.ErrorIs(t, err, errEmailInUse)
		assert.Empty(t, f.writes())
	})

	t.Run("メールの形でなければ errEmailShape で書かない", func(t *testing.T) {
		t.Parallel()

		f := updateWorld(t, "sec@x.y", "", "")
		_, err := update(t, f, "security", "x\n")
		require.ErrorIs(t, err, errEmailShape)
		assert.Empty(t, f.writes())
	})

	t.Run("端末でなければ errNotTerminal で書かない", func(t *testing.T) {
		t.Parallel()

		f := updateWorld(t, "sec@x.y", "", "")
		o := testOptions()
		o.env = "security"
		err := updateMemberEmail(o, testClient(f, updateWrites...), newTerminal(strings.NewReader("new@x.y\ntabp\n"), &bytes.Buffer{}, false), testMembers(t))
		require.ErrorIs(t, err, errNotTerminal)
		assert.Empty(t, f.writes())
	})
}

func Test_changePrimaryEmail(t *testing.T) {
	t.Parallel()

	t.Run("始めに失敗すれば、確認コードを問わない", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{"account start-primary-email-update": fail(t, "ConflictException")})
		term, out := tty("ABC123\n")
		require.ErrorIs(t, changePrimaryEmail(testClient(f, updateWrites...), term, "222222222222", "new@x.y"), errAWSFailed)
		assert.Empty(t, out.String())
	})

	t.Run("確定に失敗すれば errAWSFailed", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{
			"account start-primary-email-update":  ok(""),
			"account accept-primary-email-update": fail(t, "ValidationException"),
		})
		term, _ := tty("ABC123\n")
		require.ErrorIs(t, changePrimaryEmail(testClient(f, updateWrites...), term, "222222222222", "new@x.y"), errAWSFailed)
	})

	t.Run("8桁の確認コードも受ける", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{
			"account start-primary-email-update":  ok(""),
			"account accept-primary-email-update": ok(""),
			"account get-primary-email":           ok(`{"PrimaryEmail":"NEW@x.y"}`),
		})
		term, _ := tty("12345678\n")
		require.NoError(t, changePrimaryEmail(testClient(f, updateWrites...), term, "222222222222", "new@x.y"))
	})
}

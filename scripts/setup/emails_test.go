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
	updatedValue = `{"log-archive":"la@x.y","security":"new@x.y"}`
)

// emailsWorld は、parameter が current（空なら無い）で、組織に accountsJSON のアカウントが在る状態。
// parameter を written で置く書き込みが成功し、その後の get-parameter は written を返す。
func emailsWorld(t *testing.T, current, written string) *fakeAWS {
	t.Helper()

	param := fail(t, notFound)
	if current != "" {
		param = paramResponse(current)
	}

	f := newFakeAWS(t, map[string]awsResult{
		"ssm get-parameter --name " + paramName: param,
		"organizations list-accounts":           ok(accountsJSON),
		putParam(written):                       ok(`{"Version":1}`),
	})
	f.onWrite(putParam(written), map[string]awsResult{"ssm get-parameter --name " + paramName: paramResponse(written)})

	return f
}

func Test_emailsParameter(t *testing.T) {
	t.Parallel()

	t.Run("accounts.tf の data source と同じ名前", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, paramName, emailsParameter(testProject))
	})
}

func Test_validEmail(t *testing.T) {
	t.Parallel()

	org := testOrg(t)

	cases := map[string]struct {
		v    string
		want bool
	}{
		"precondition の形なら通す": {"a.b+c@x.example", true},
		"ドットの無いドメインは通さない":     {"a@b", false},
		"空白を含めば通さない":          {"a b@c.d", false},
		"@ が2つなら通さない":         {"a@@b.c", false},
		"file:// の形は通さない":     {"file:///x@a.b", false},
		"http:// の形は通さない":     {"http://u@host.tld", false},
		"- で始まれば通さない":         {"-a@b.c", false},
		"空なら通さない":             {"", false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, validEmail(org, tc.v))
		})
	}
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

	t.Run("値が空の object なら、在る空の写像として返す", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{"ssm get-parameter": paramResponse("{}")})
		got, found, err := readEmails(testClient(f), testProject)
		require.NoError(t, err)
		assert.True(t, found)
		assert.Empty(t, got)
	})

	t.Run("無ければ found が false", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{"ssm get-parameter": fail(t, notFound)})
		_, found, err := readEmails(testClient(f), testProject)
		require.NoError(t, err)
		assert.False(t, found)
	})

	t.Run("ParameterNotFound 以外の失敗は、無いと読まずに errAWSFailed", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{"ssm get-parameter": fail(t, "An error occurred (AccessDeniedException)")})
		_, _, err := readEmails(testClient(f), testProject)
		require.ErrorIs(t, err, errAWSFailed)
	})

	for name, resp := range map[string]awsResult{
		"JSON でなければ errParameterUnreadable":            paramResponse("a@b.c"),
		"値が文字列でなければ errParameterUnreadable":            paramResponse(`{"a":1}`),
		"null なら errParameterUnreadable":               paramResponse("null"),
		"値が null の key があれば errParameterUnreadable":    paramResponse(`{"security":null}`),
		"object でなければ errParameterUnreadable":          paramResponse(`["a@b.c"]`),
		"Parameter.Value が無ければ errParameterUnreadable": ok(`{"Parameter":{}}`),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFakeAWS(t, map[string]awsResult{"ssm get-parameter": resp})
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
		assert.Equal(t, []string{putParam(desiredValue)}, f.writes())
		assert.Equal(t, []string{"ssm put-parameter", "ssm get-parameter"}, f.ops())
	})

	t.Run("読み直した値が違えば errVerify", func(t *testing.T) {
		t.Parallel()

		f := emailsWorld(t, "", desiredValue)
		f.after[putParam(desiredValue)]["ssm get-parameter --name "+paramName] = paramResponse(`{"security":"sec@x.y"}`)
		require.ErrorIs(t, writeEmails(testClient(f, "ssm put-parameter"), testProject, desired), errVerify)
	})

	t.Run("読み直して無ければ errVerify", func(t *testing.T) {
		t.Parallel()

		f := emailsWorld(t, "", desiredValue)
		f.after[putParam(desiredValue)]["ssm get-parameter --name "+paramName] = fail(t, notFound)
		require.ErrorIs(t, writeEmails(testClient(f, "ssm put-parameter"), testProject, desired), errVerify)
	})

	t.Run("読み直した値が読めなければ errParameterUnreadable", func(t *testing.T) {
		t.Parallel()

		f := emailsWorld(t, "", desiredValue)
		f.after[putParam(desiredValue)]["ssm get-parameter --name "+paramName] = paramResponse("broken")
		require.ErrorIs(t, writeEmails(testClient(f, "ssm put-parameter"), testProject, desired), errParameterUnreadable)
	})

	t.Run("書き込みが落ちれば読み直さない", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{putParam(desiredValue): fail(t, "denied")})
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

	t.Run("ほかのアカウントのメールなら、大文字小文字が違っても errEmailInUse", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, checkEmailUse(testProject, map[string]string{"log-archive": "M@x.y"}, accounts), errEmailInUse)
	})

	t.Run("重複は、ほかのアカウントとの重なりより先に見る", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, checkEmailUse(testProject, map[string]string{"log-archive": "sec@x.y", "security": "sec@x.y"}, accounts), errEmailDuplicate)
	})
}

func Test_putMemberEmails(t *testing.T) {
	t.Parallel()

	put := func(t *testing.T, f *fakeAWS, input string) (string, error) {
		t.Helper()

		term, out := tty(input)
		err := putMemberEmails(testOptions(), testClient(f, "ssm put-parameter"), term, testOrg(t))

		return out.String(), err
	}

	t.Run("parameter が無ければ、env ごとに受けたメールで置き、読み直す", func(t *testing.T) {
		t.Parallel()

		f := emailsWorld(t, "", desiredValue)
		out, err := put(t, f, "la@x.y\nsec@x.y\ntabp\n")
		require.NoError(t, err)
		assert.Equal(t, []string{putParam(desiredValue)}, f.writes())
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
		assert.Equal(t, []string{putParam(desiredValue)}, f.writes())
	})

	t.Run("対応表に無い key は消える差分として示し、書かない", func(t *testing.T) {
		t.Parallel()

		f := emailsWorld(t, `{"dev":"d@x.y","log-archive":"la@x.y","security":"sec@x.y"}`, desiredValue)
		out, err := put(t, f, "\n\ntabp\n")
		require.NoError(t, err)
		assert.Contains(t, out, "  - dev: d@x.y\n")
		assert.Equal(t, []string{putParam(desiredValue)}, f.writes())
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

	t.Run("今の値が読めなければ、知らせてすべてを新しく入れさせる", func(t *testing.T) {
		t.Parallel()

		f := emailsWorld(t, "broken", desiredValue)
		out, err := put(t, f, "la@x.y\nsec@x.y\ntabp\n")
		require.NoError(t, err)
		assert.Contains(t, out, "今の parameter の値を読めない")
		assert.Contains(t, out, "log-archive のメール: ")
		assert.Contains(t, out, "  + log-archive: la@x.y\n")
		assert.Contains(t, out, "  + security: sec@x.y\n")
		assert.Equal(t, []string{putParam(desiredValue)}, f.writes())
	})

	refused := map[string]struct {
		input string
		want  error
	}{
		"確認の入力が project の名前でなければ errNotConfirmed":           {"la@x.y\nsec@x.y\nyes\n", errNotConfirmed},
		"確認の入力が尽きていれば errNoInput":                           {"la@x.y\nsec@x.y\n", errNoInput},
		"メールの形でなければ errEmailShape":                          {"la-at-x\n", errEmailShape},
		"file:// の形なら errEmailShape":                        {"file:///x@a.b\n", errEmailShape},
		"空の入力で今の値も無ければ errEmailShape":                       {"\n", errEmailShape},
		"2つの env に同じメールなら errEmailDuplicate":                {"SEC@x.y\nsec@x.y\n", errEmailDuplicate},
		"ほかのアカウントのメールなら errEmailInUse":                      {"m@x.y\nsec@x.y\n", errEmailInUse},
		"在るアカウントのメールを変えるなら errEmailChangeNeedsUpdate":       {"la@x.y\nnew@x.y\n", errEmailChangeNeedsUpdate},
		"在るアカウントのメールと大文字小文字だけ違えば errEmailChangeNeedsUpdate": {"la@x.y\nSEC@x.y\n", errEmailChangeNeedsUpdate},
		"入力が途中で尽きれば errNoInput":                             {"la@x.y\n", errNoInput},
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

	t.Run("同じ名前のアカウントが2つなら errAccountName で書かない", func(t *testing.T) {
		t.Parallel()

		f := emailsWorld(t, "", desiredValue)
		f.responses["organizations list-accounts"] = ok(`{"Accounts":[{"Id":"1","Name":"tabp-security","State":"ACTIVE"},{"Id":"2","Name":"tabp-security","State":"PENDING_ACTIVATION"}]}`)
		_, err := put(t, f, "la@x.y\nsec@x.y\ntabp\n")
		require.ErrorIs(t, err, errAccountName)
		assert.Empty(t, f.writes())
	})

	t.Run("端末でなければ、問わずに errNotTerminal で書かない", func(t *testing.T) {
		t.Parallel()

		f := emailsWorld(t, "", desiredValue)

		var out bytes.Buffer

		err := putMemberEmails(testOptions(), testClient(f, "ssm put-parameter"), newTerminal(strings.NewReader("la@x.y\nsec@x.y\ntabp\n"), &out, false), testOrg(t))
		require.ErrorIs(t, err, errNotTerminal)
		assert.Empty(t, f.writes())
		assert.NotContains(t, out.String(), "のメール")
	})

	for name, world := range map[string]func(t *testing.T) *fakeAWS{
		"組織を読めなければ": func(t *testing.T) *fakeAWS {
			f := emailsWorld(t, "", desiredValue)
			f.responses["organizations list-accounts"] = fail(t, "denied")

			return f
		},
		"parameter を AccessDenied で読めなければ": func(t *testing.T) *fakeAWS {
			f := emailsWorld(t, "", desiredValue)
			f.responses["ssm get-parameter --name "+paramName] = fail(t, "An error occurred (AccessDeniedException)")

			return f
		},
	} {
		t.Run(name+"、問わずに errAWSFailed で書かない", func(t *testing.T) {
			t.Parallel()

			f := world(t)
			out, err := put(t, f, "la@x.y\nsec@x.y\ntabp\n")
			require.ErrorIs(t, err, errAWSFailed)
			assert.Empty(t, out)
			assert.Empty(t, f.writes())
		})
	}

	t.Run("parameter の書き込みが落ちれば、置いたと出さずに errAWSFailed", func(t *testing.T) {
		t.Parallel()

		f := emailsWorld(t, "", desiredValue)
		f.responses[putParam(desiredValue)] = fail(t, "denied")
		out, err := put(t, f, "la@x.y\nsec@x.y\ntabp\n")
		require.ErrorIs(t, err, errAWSFailed)
		assert.NotContains(t, out, "✅")
	})

	t.Run("読み直した parameter が違えば、置いたと出さずに errVerify", func(t *testing.T) {
		t.Parallel()

		f := emailsWorld(t, "", desiredValue)
		f.after[putParam(desiredValue)]["ssm get-parameter --name "+paramName] = paramResponse(`{"security":"sec@x.y"}`)
		out, err := put(t, f, "la@x.y\nsec@x.y\ntabp\n")
		require.ErrorIs(t, err, errVerify)
		assert.NotContains(t, out, "✅")
	})

	t.Run("アカウントが0件の組織は読めない応答として、書かない", func(t *testing.T) {
		t.Parallel()

		f := emailsWorld(t, "", desiredValue)
		f.responses["organizations list-accounts"] = ok(`{"Accounts":[]}`)
		_, err := put(t, f, "la@x.y\nsec@x.y\ntabp\n")
		require.ErrorIs(t, err, errAWSResponse)
		assert.Empty(t, f.writes())
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
		err := checkMemberEmails(testOptions(), testClient(f), &out, testOrg(t))

		return out.String(), err
	}

	t.Run("key が一致し、形が合い、在るアカウントのメールと一致すれば nil", func(t *testing.T) {
		t.Parallel()

		out, err := check(t, emailsWorld(t, desiredValue, desiredValue))
		require.NoError(t, err)
		assert.NotContains(t, out, "❌")
		assert.Contains(t, out, "✅ security の値が、既に在るアカウントのメールと大文字小文字まで一致する")
	})

	ngs := map[string]struct {
		value string
		line  string
	}{
		"parameter が無ければ NG":         {"", "❌ parameter が無い"},
		"値が読めなければ NG":                {"broken", "❌ 値が文字列の JSON object ではない"},
		"key が足りなければ NG":             {`{"security":"sec@x.y"}`, "❌ key が対応表の env と一致する（対応表: log-archive・security、parameter: security）"},
		"key が余れば NG":                {`{"dev":"d@x.y","log-archive":"la@x.y","security":"sec@x.y"}`, "❌ key が対応表の env と一致する"},
		"値がメールの形でなければ NG":            {`{"log-archive":"la","security":"sec@x.y"}`, "❌ log-archive の値がメールの形である"},
		"在るアカウントのメールと違えば NG":         {`{"log-archive":"la@x.y","security":"other@x.y"}`, "❌ security の値が、既に在るアカウントのメールと大文字小文字まで一致する"},
		"在るアカウントのメールと大文字小文字だけ違えば NG": {`{"log-archive":"la@x.y","security":"SEC@x.y"}`, "❌ security の値が、既に在るアカウントのメールと大文字小文字まで一致する"},
		"ほかのアカウントのメールと重なれば NG":       {`{"log-archive":"m@x.y","security":"sec@x.y"}`, "❌ メールが env の間で重ならず"},
		"env の間で重なれば NG":             {`{"log-archive":"sec@x.y","security":"sec@x.y"}`, "❌ メールが env の間で重ならず"},
	}
	for name, tc := range ngs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			out, err := check(t, emailsWorld(t, tc.value, tc.value))
			require.ErrorIs(t, err, errNG)
			assert.Contains(t, out, tc.line)
		})
	}

	t.Run("組織を読めなければ errAWSFailed", func(t *testing.T) {
		t.Parallel()

		f := emailsWorld(t, desiredValue, desiredValue)
		f.responses["organizations list-accounts"] = fail(t, "denied")
		_, err := check(t, f)
		require.ErrorIs(t, err, errAWSFailed)
	})

	t.Run("parameter を AccessDenied で読めなければ、NG にせず errAWSFailed", func(t *testing.T) {
		t.Parallel()

		f := emailsWorld(t, desiredValue, desiredValue)
		f.responses["ssm get-parameter --name "+paramName] = fail(t, "An error occurred (AccessDeniedException)")
		_, err := check(t, f)
		require.ErrorIs(t, err, errAWSFailed)
	})
}

const (
	startUpdate   = "account start-primary-email-update --account-id 222222222222 --primary-email new@x.y"
	acceptUpdate  = "account accept-primary-email-update --account-id 222222222222 --primary-email new@x.y"
	enableTrust   = "organizations enable-aws-service-access --service-principal account.amazonaws.com"
	getPrimary    = "account get-primary-email --account-id 222222222222"
	trustedJSON   = `{"EnabledServicePrincipals":[{"ServicePrincipal":"account.amazonaws.com"}]}`
	untrustedJSON = `{"EnabledServicePrincipals":[{"ServicePrincipal":"cloudtrail.amazonaws.com"}]}`
)

// updateWorld は、security のアカウントの主のメールが before で、信頼アクセスが trusted の状態。
// 信頼アクセスが無ければ、主のメールの読み取りは失敗する（AWS が拒む）。書き込みの後の主のメールは after。
func updateWorld(t *testing.T, before, after string, trusted bool) *fakeAWS {
	t.Helper()

	access := untrustedJSON
	primary := fail(t, "An error occurred (AccessDeniedException) when calling the GetPrimaryEmail operation")

	if trusted {
		access = trustedJSON
		primary = ok(`{"PrimaryEmail":"` + before + `"}`)
	}

	f := newFakeAWS(t, map[string]awsResult{
		"ssm get-parameter --name " + paramName:                  paramResponse(desiredValue),
		"organizations list-accounts":                            ok(accountsJSON),
		getPrimary:                                               primary,
		"organizations list-aws-service-access-for-organization": ok(access),
		enableTrust:            ok(""),
		startUpdate:            ok(`{"Status":"PENDING"}`),
		acceptUpdate:           ok(`{"Status":"ACCEPTED"}`),
		putParam(updatedValue): ok(`{"Version":2}`),
	})
	f.onWrite(acceptUpdate, map[string]awsResult{getPrimary: ok(`{"PrimaryEmail":"` + after + `"}`)})
	f.onWrite(putParam(updatedValue), map[string]awsResult{"ssm get-parameter --name " + paramName: paramResponse(updatedValue)})

	return f
}

var updateWrites = []string{"organizations enable-aws-service-access", "account start-primary-email-update", "account accept-primary-email-update", "ssm put-parameter"}

func Test_updateMemberEmail(t *testing.T) {
	t.Parallel()

	update := func(t *testing.T, f *fakeAWS, env, input string) (string, error) {
		t.Helper()

		o := testOptions()
		o.env = env
		term, out := tty(input)
		err := updateMemberEmail(o, testClient(f, updateWrites...), term, testOrg(t))

		return out.String(), err
	}

	t.Run("AWS の側を変え、確認コードを一時ファイルで渡して確定し、その後に parameter を合わせる", func(t *testing.T) {
		t.Parallel()

		f := updateWorld(t, "sec@x.y", "new@x.y", true)
		out, err := update(t, f, "security", "new@x.y\ntabp\nABC123\n")
		require.NoError(t, err)
		assert.Equal(t, []string{startUpdate, acceptUpdate, putParam(updatedValue)}, f.writes())
		assert.Equal(t, []string{`{"Otp":"ABC123"}`}, f.secrets)
		assert.NotContains(t, strings.Join(f.joined(), " "), "ABC123")
		assert.Contains(t, out, "security の新しいメール（今は sec@x.y）: ")
		assert.Contains(t, out, "  ~ security: sec@x.y → new@x.y\n")
		assert.NotContains(t, out, "1. Organizations の信頼アクセス")
	})

	t.Run("Account Management の信頼アクセスが無ければ、主のメールを読まずに、最初に有効にする", func(t *testing.T) {
		t.Parallel()

		f := updateWorld(t, "", "new@x.y", false)
		out, err := update(t, f, "security", "new@x.y\ntabp\nABC123\n")
		require.NoError(t, err)
		assert.Equal(t, []string{enableTrust, startUpdate, acceptUpdate, putParam(updatedValue)}, f.writes())
		assert.Contains(t, out, "（Account Management の信頼アクセスが無いので読めません）")
		assert.Contains(t, out, "1. Organizations の信頼アクセスを account.amazonaws.com に有効にする")
	})

	t.Run("AWS の側が既に新しいメールなら、parameter だけを合わせる", func(t *testing.T) {
		t.Parallel()

		f := updateWorld(t, "new@x.y", "new@x.y", true)
		_, err := update(t, f, "security", "new@x.y\ntabp\n")
		require.NoError(t, err)
		assert.Equal(t, []string{putParam(updatedValue)}, f.writes())
	})

	t.Run("AWS の側も parameter も既に新しいメールなら書かない", func(t *testing.T) {
		t.Parallel()

		f := updateWorld(t, "sec@x.y", "sec@x.y", true)
		out, err := update(t, f, "security", "sec@x.y\n")
		require.NoError(t, err)
		assert.Empty(t, f.writes())
		assert.Contains(t, out, "既に sec@x.y です")
	})

	t.Run("AWS の側と大文字小文字だけ違うメールは、変える差分として扱う", func(t *testing.T) {
		t.Parallel()

		f := updateWorld(t, "SEC@x.y", "sec@x.y", true)
		const acceptSec = "account accept-primary-email-update --account-id 222222222222 --primary-email sec@x.y"
		f.responses[putParam(desiredValue)] = ok("")
		f.responses["account start-primary-email-update --account-id 222222222222 --primary-email sec@x.y"] = ok("")
		f.responses[acceptSec] = ok("")
		f.onWrite(acceptSec, map[string]awsResult{getPrimary: ok(`{"PrimaryEmail":"sec@x.y"}`)})
		f.onWrite(putParam(desiredValue), map[string]awsResult{"ssm get-parameter --name " + paramName: paramResponse(desiredValue)})
		_, err := update(t, f, "security", "sec@x.y\ntabp\nABC123\n")
		require.NoError(t, err)
		assert.Contains(t, f.writes(), "account start-primary-email-update --account-id 222222222222 --primary-email sec@x.y")
	})

	t.Run("確定の後に主のメールが変わっていなければ、parameter を書かずに errVerify", func(t *testing.T) {
		t.Parallel()

		f := updateWorld(t, "sec@x.y", "sec@x.y", true)
		_, err := update(t, f, "security", "new@x.y\ntabp\nABC123\n")
		require.ErrorIs(t, err, errVerify)
		assert.NotContains(t, f.writes(), putParam(updatedValue))
	})

	t.Run("今の主のメールが空なら、問わずに errAWSResponse で書かない", func(t *testing.T) {
		t.Parallel()

		f := updateWorld(t, "", "", true)
		out, err := update(t, f, "security", "new@x.y\ntabp\nABC123\n")
		require.ErrorIs(t, err, errAWSResponse)
		assert.Empty(t, f.writes())
		assert.NotContains(t, out, "新しいメール")
	})

	t.Run("確認の入力が project の名前でなければ書かない", func(t *testing.T) {
		t.Parallel()

		f := updateWorld(t, "", "new@x.y", false)
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
		"parameter の key が対応表と一致しなければ": {"security", func(t *testing.T) *fakeAWS {
			return newFakeAWS(t, map[string]awsResult{"ssm get-parameter": paramResponse(`{"security":"sec@x.y"}`)})
		}},
		"parameter が空の object なら": {"security", func(t *testing.T) *fakeAWS {
			return newFakeAWS(t, map[string]awsResult{"ssm get-parameter": paramResponse(`{}`)})
		}},
		"アカウントがまだ無ければ": {"log-archive", func(t *testing.T) *fakeAWS { return updateWorld(t, "", "", true) }},
		"アカウントが ACTIVE でなければ": {"security", func(t *testing.T) *fakeAWS {
			f := updateWorld(t, "", "", true)
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

	failures := map[string]struct {
		world func(t *testing.T) *fakeAWS
		want  error
	}{
		"parameter が読めなければ errParameterUnreadable": {func(t *testing.T) *fakeAWS {
			return newFakeAWS(t, map[string]awsResult{"ssm get-parameter": paramResponse("broken")})
		}, errParameterUnreadable},
		"信頼アクセスの一覧を読めなければ errAWSFailed": {func(t *testing.T) *fakeAWS {
			f := updateWorld(t, "sec@x.y", "new@x.y", true)
			f.responses["organizations list-aws-service-access-for-organization"] = fail(t, "denied")

			return f
		}, errAWSFailed},
		"信頼アクセスがあるのに主のメールを読めなければ errAWSFailed": {func(t *testing.T) *fakeAWS {
			f := updateWorld(t, "sec@x.y", "new@x.y", true)
			f.responses[getPrimary] = fail(t, "denied")

			return f
		}, errAWSFailed},
		"同じ名前のアカウントが2つなら errAccountName": {func(t *testing.T) *fakeAWS {
			f := updateWorld(t, "sec@x.y", "new@x.y", true)
			f.responses["organizations list-accounts"] = ok(`{"Accounts":[{"Id":"1","Name":"tabp-security","State":"ACTIVE"},{"Id":"2","Name":"tabp-security","State":"PENDING_ACTIVATION"}]}`)

			return f
		}, errAccountName},
	}
	for name, tc := range failures {
		t.Run(name+"、書かない", func(t *testing.T) {
			t.Parallel()

			f := tc.world(t)
			_, err := update(t, f, "security", "new@x.y\ntabp\nABC123\n")
			require.ErrorIs(t, err, tc.want)
			assert.Empty(t, f.writes())
		})
	}

	t.Run("信頼アクセスの有効化が落ちれば、主のメールを変えない", func(t *testing.T) {
		t.Parallel()

		f := updateWorld(t, "", "new@x.y", false)
		f.responses[enableTrust] = fail(t, "denied")
		_, err := update(t, f, "security", "new@x.y\ntabp\nABC123\n")
		require.ErrorIs(t, err, errAWSFailed)
		assert.Equal(t, []string{enableTrust}, f.writes())
	})

	t.Run("主のメールを変えた後に parameter の書き込みが落ちれば errAWSFailed", func(t *testing.T) {
		t.Parallel()

		f := updateWorld(t, "sec@x.y", "new@x.y", true)
		f.responses[putParam(updatedValue)] = fail(t, "denied")
		_, err := update(t, f, "security", "new@x.y\ntabp\nABC123\n")
		require.ErrorIs(t, err, errAWSFailed)
		assert.Equal(t, []string{startUpdate, acceptUpdate, putParam(updatedValue)}, f.writes())
	})

	refused := map[string]struct {
		input string
		want  error
	}{
		"ほかのアカウントのメールなら errEmailInUse": {"m@x.y\n", errEmailInUse},
		"メールの形でなければ errEmailShape":     {"x\n", errEmailShape},
		"- で始まれば errEmailShape":        {"-x@y.z\n", errEmailShape},
		"新しいメールの入力が尽きれば errNoInput":    {"", errNoInput},
	}
	for name, tc := range refused {
		t.Run(name+"、書かない", func(t *testing.T) {
			t.Parallel()

			f := updateWorld(t, "sec@x.y", "", true)
			_, err := update(t, f, "security", tc.input)
			require.ErrorIs(t, err, tc.want)
			assert.Empty(t, f.writes())
		})
	}

	t.Run("端末でなければ errNotTerminal で書かない", func(t *testing.T) {
		t.Parallel()

		f := updateWorld(t, "sec@x.y", "", true)
		o := testOptions()
		o.env = "security"
		err := updateMemberEmail(o, testClient(f, updateWrites...), newTerminal(strings.NewReader("new@x.y\ntabp\n"), &bytes.Buffer{}, false), testOrg(t))
		require.ErrorIs(t, err, errNotTerminal)
		assert.Empty(t, f.writes())
	})
}

func Test_primaryEmail(t *testing.T) {
	t.Parallel()

	t.Run("主のメールを返す", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{getPrimary: ok(`{"PrimaryEmail":"a@b.c"}`)})
		got, err := primaryEmail(testClient(f), "222222222222")
		require.NoError(t, err)
		assert.Equal(t, "a@b.c", got)
	})

	t.Run("PrimaryEmail が無ければ errAWSResponse", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{getPrimary: ok(`{}`)})
		_, err := primaryEmail(testClient(f), "222222222222")
		require.ErrorIs(t, err, errAWSResponse)
	})
}

func Test_changePrimaryEmail(t *testing.T) {
	t.Parallel()

	world := func(t *testing.T) *fakeAWS {
		t.Helper()

		f := newFakeAWS(t, map[string]awsResult{startUpdate: ok(""), acceptUpdate: ok("")})
		f.onWrite(acceptUpdate, map[string]awsResult{getPrimary: ok(`{"PrimaryEmail":"new@x.y"}`)})

		return f
	}

	change := func(t *testing.T, f *fakeAWS, input string) (string, error) {
		t.Helper()

		term, out := tty(input)
		err := changePrimaryEmail(testClient(f, updateWrites...), term, "222222222222", "new@x.y")

		return out.String(), err
	}

	t.Run("始めに失敗すれば、確認コードを問わない", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{startUpdate: fail(t, "ConflictException")})
		out, err := change(t, f, "ABC123\n")
		require.ErrorIs(t, err, errAWSFailed)
		assert.Empty(t, out)
	})

	t.Run("確定に失敗すれば errAWSFailed", func(t *testing.T) {
		t.Parallel()

		f := world(t)
		f.responses[acceptUpdate] = fail(t, "ValidationException")
		_, err := change(t, f, "ABC123\n")
		require.ErrorIs(t, err, errAWSFailed)
	})

	for name, otp := range map[string]string{
		"6文字の確認コードを受ける": "ABC123",
		"8文字の確認コードを受ける": "12345678",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := world(t)
			_, err := change(t, f, otp+"\n")
			require.NoError(t, err)
			assert.Equal(t, []string{`{"Otp":"` + otp + `"}`}, f.secrets)
		})
	}

	for name, otp := range map[string]string{
		"5文字なら errOTPShape":  "ABC12",
		"9文字なら errOTPShape":  "123456789",
		"記号を含めば errOTPShape": "ABC-123",
		"2文字なら errOTPShape":  "12",
	} {
		t.Run(name+"で、確定しない", func(t *testing.T) {
			t.Parallel()

			f := world(t)
			_, err := change(t, f, otp+"\n")
			require.ErrorIs(t, err, errOTPShape)
			assert.Equal(t, []string{startUpdate}, f.writes())
		})
	}

	t.Run("確認コードの入力が尽きれば、確定しない", func(t *testing.T) {
		t.Parallel()

		f := world(t)
		_, err := change(t, f, "")
		require.ErrorIs(t, err, errNoInput)
		assert.Equal(t, []string{startUpdate}, f.writes())
	})

	t.Run("主のメールは確定の後に読み直す", func(t *testing.T) {
		t.Parallel()

		f := world(t)
		_, err := change(t, f, "ABC123\n")
		require.NoError(t, err)
		assert.Equal(t, []string{"account start-primary-email-update", "account accept-primary-email-update", "account get-primary-email"}, f.ops())
	})

	t.Run("確定の後の読み直しが落ちれば errAWSFailed", func(t *testing.T) {
		t.Parallel()

		f := world(t)
		f.after[acceptUpdate][getPrimary] = fail(t, "denied")
		_, err := change(t, f, "ABC123\n")
		require.ErrorIs(t, err, errAWSFailed)
	})

	t.Run("確定の後に読み直した主のメールが大文字小文字だけ違っても errVerify", func(t *testing.T) {
		t.Parallel()

		f := world(t)
		f.after[acceptUpdate][getPrimary] = ok(`{"PrimaryEmail":"NEW@x.y"}`)
		_, err := change(t, f, "ABC123\n")
		require.ErrorIs(t, err, errVerify)
	})
}

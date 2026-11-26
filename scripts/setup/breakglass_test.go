package main

import (
	"bytes"
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	bg1          = "break-glass-1"
	bg2          = "break-glass-2"
	noSuchUser   = "An error occurred (NoSuchEntity) when calling the GetLoginProfile operation"
	hwSerial     = "GAKT12345678"
	virtualMFA   = "arn:aws:iam::111111111111:mfa/break-glass-1"
	fidoMFA      = "arn:aws:iam::111111111111:u2f/user/break-glass-1/fidosecuritykey1-7XNL7NFNLZ123456789EXAMPLE"
	createLogin  = "iam create-login-profile"
	enableMFA    = "iam enable-mfa-device"
	listBGUsers  = "iam list-users --path-prefix /break-glass/"
	passwordPol  = "iam get-account-password-policy"
	bgPolicyJSON = `{"PasswordPolicy":{"MinimumPasswordLength":14,"RequireSymbols":true,"HardExpiry":false,"ExpirePasswords":true}}`
)

func getUser(u string) string     { return "iam get-user --user-name " + u }
func getLogin(u string) string    { return "iam get-login-profile --user-name " + u }
func listMFA(u string) string     { return "iam list-mfa-devices --user-name " + u }
func listKeys(u string) string    { return "iam list-access-keys --user-name " + u }
func userJSON(u string) awsResult { return ok(`{"User":{"UserName":"` + u + `"}}`) }
func loginJSON(u string) awsResult {
	return ok(`{"LoginProfile":{"UserName":"` + u + `"}}`)
}

func mfaJSON(serials ...string) awsResult {
	var items []string
	for _, s := range serials {
		items = append(items, `{"SerialNumber":"`+s+`"}`)
	}

	return ok(`{"MFADevices":[` + strings.Join(items, ",") + `]}`)
}

func bgOptions() options {
	return testOptionsUC(fixtureFiles(usecaseMainTF, ouMainTF, accountsTF))
}

// bgWorld は、2つの break-glass の user が在り、login profile と MFA が login・mfa の世界。
func bgWorld(t *testing.T, login map[string]bool, mfa map[string][]string) *fakeAWS {
	t.Helper()

	m := map[string]awsResult{
		listBGUsers: ok(`{"Users":[{"UserName":"` + bg1 + `"},{"UserName":"` + bg2 + `"}]}`),
		passwordPol: ok(bgPolicyJSON),
	}

	for _, u := range []string{bg1, bg2} {
		m[getUser(u)] = userJSON(u)
		m[listMFA(u)] = mfaJSON(mfa[u]...)
		m[listKeys(u)] = ok(`{"AccessKeyMetadata":[]}`)

		if login[u] {
			m[getLogin(u)] = loginJSON(u)
		} else {
			m[getLogin(u)] = fail(t, noSuchUser)
		}
	}

	maps.Copy(m, managementWorld("111111111111", "111111111111"))

	return newFakeAWS(t, m)
}

func bothLogin() map[string]bool { return map[string]bool{bg1: true, bg2: true} }

func bothMFA() map[string][]string { return map[string][]string{bg1: {hwSerial}, bg2: {fidoMFA}} }

func Test_readBreakGlass(t *testing.T) {
	t.Parallel()

	t.Run("options の usecase と project で宣言を読む", func(t *testing.T) {
		t.Parallel()

		bg, err := readBreakGlass(bgOptions())
		require.NoError(t, err)
		assert.Equal(t, []string{bg1, bg2}, bg.users)
		assert.Equal(t, "tabp-break-glass", bg.role)
	})
}

func Test_userExists(t *testing.T) {
	t.Parallel()

	t.Run("在れば true、NoSuchEntity なら false", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{getUser(bg1): userJSON(bg1), getUser(bg2): fail(t, "An error occurred (NoSuchEntity)")})

		yes, err := userExists(testClient(f), bg1)
		require.NoError(t, err)
		assert.True(t, yes)

		no, err := userExists(testClient(f), bg2)
		require.NoError(t, err)
		assert.False(t, no)
	})

	t.Run("別の名前の応答は errAWSResponse", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{getUser(bg1): userJSON(bg2)})
		_, err := userExists(testClient(f), bg1)
		require.ErrorIs(t, err, errAWSResponse)
	})
}

func Test_requireUsers(t *testing.T) {
	t.Parallel()

	t.Run("すべて在れば nil", func(t *testing.T) {
		t.Parallel()

		require.NoError(t, requireUsers(testClient(bgWorld(t, nil, nil)), []string{bg1, bg2}))
	})

	t.Run("1つでも無ければ errPrecondition", func(t *testing.T) {
		t.Parallel()

		f := bgWorld(t, nil, nil)
		f.responses[getUser(bg2)] = fail(t, "An error occurred (NoSuchEntity)")
		require.ErrorIs(t, requireUsers(testClient(f), []string{bg1, bg2}), errPrecondition)
	})
}

func Test_hasLoginProfile(t *testing.T) {
	t.Parallel()

	t.Run("在れば true、NoSuchEntity なら false", func(t *testing.T) {
		t.Parallel()

		f := bgWorld(t, map[string]bool{bg1: true}, nil)

		yes, err := hasLoginProfile(testClient(f), bg1)
		require.NoError(t, err)
		assert.True(t, yes)

		no, err := hasLoginProfile(testClient(f), bg2)
		require.NoError(t, err)
		assert.False(t, no)
	})

	t.Run("UserName の無い応答は errAWSResponse", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{getLogin(bg1): ok(`{"LoginProfile":{}}`)})
		_, err := hasLoginProfile(testClient(f), bg1)
		require.ErrorIs(t, err, errAWSResponse)
	})

	t.Run("ほかの失敗は errAWSFailed", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{getLogin(bg1): fail(t, "AccessDenied")})
		_, err := hasLoginProfile(testClient(f), bg1)
		require.ErrorIs(t, err, errAWSFailed)
	})
}

func Test_mfaSerials(t *testing.T) {
	t.Parallel()

	t.Run("シリアルを返す。0件は空", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{listMFA(bg1): mfaJSON(hwSerial, fidoMFA), listMFA(bg2): mfaJSON()})

		got, err := mfaSerials(testClient(f), bg1)
		require.NoError(t, err)
		assert.Equal(t, []string{hwSerial, fidoMFA}, got)

		none, err := mfaSerials(testClient(f), bg2)
		require.NoError(t, err)
		assert.Empty(t, none)
	})

	for name, stdout := range map[string]string{
		"MFADevices が無ければ errAWSResponse":   `{}`,
		"SerialNumber が欠ければ errAWSResponse": `{"MFADevices":[{}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := mfaSerials(testClient(newFakeAWS(t, map[string]awsResult{listMFA(bg1): ok(stdout)})), bg1)
			require.ErrorIs(t, err, errAWSResponse)
		})
	}
}

func Test_isVirtualMFA(t *testing.T) {
	t.Parallel()

	t.Run("mfa/ の ARN だけが仮想", func(t *testing.T) {
		t.Parallel()

		assert.True(t, isVirtualMFA(virtualMFA))
		assert.False(t, isVirtualMFA(fidoMFA))
		assert.False(t, isVirtualMFA(hwSerial))
	})
}

func Test_hasHardwareMFA(t *testing.T) {
	t.Parallel()

	t.Run("仮想でない装置だけが在れば true", func(t *testing.T) {
		t.Parallel()

		assert.True(t, hasHardwareMFA([]string{hwSerial}))
		assert.True(t, hasHardwareMFA([]string{hwSerial, fidoMFA}))
	})

	t.Run("0件か、仮想 MFA が混じれば false", func(t *testing.T) {
		t.Parallel()

		assert.False(t, hasHardwareMFA(nil))
		assert.False(t, hasHardwareMFA([]string{virtualMFA}))
		assert.False(t, hasHardwareMFA([]string{hwSerial, virtualMFA}))
	})
}

func loginWorld(t *testing.T, before map[string]bool) *fakeAWS {
	t.Helper()

	f := bgWorld(t, before, nil)
	f.responses[createLogin] = ok(`{"LoginProfile":{}}`)

	after := map[string]awsResult{}
	for _, u := range []string{bg1, bg2} {
		after[getLogin(u)] = loginJSON(u)
	}

	f.onWrite(createLogin, after)

	return f
}

func Test_setBreakGlassLogin(t *testing.T) {
	t.Parallel()

	t.Run("無い user にだけ、伏せた打ち込みのパスワードを一時ファイルで置き、読み直す", func(t *testing.T) {
		t.Parallel()

		f := loginWorld(t, map[string]bool{bg1: true})
		term, out := tty("tabp\n")
		term.hidden = hiddenLines(testPassword, testPassword)

		require.NoError(t, setBreakGlassLogin(bgOptions(), testClient(f, createLogin), term))
		assert.Equal(t, []string{createLogin}, f.writes())
		assert.Equal(t, []string{`{"Password":"` + testPassword + `","PasswordResetRequired":false,"UserName":"` + bg2 + `"}`}, f.secrets)
		assert.NotContains(t, out.String(), testPassword)
		assert.Contains(t, out.String(), "✅ "+bg2+" にパスワードを置きました")
		assert.Equal(t, getLogin(bg2), f.joined()[len(f.joined())-1])
	})

	t.Run("すべて在れば、確認を求めず書き込まない", func(t *testing.T) {
		t.Parallel()

		f := loginWorld(t, bothLogin())
		term, out := tty("")

		require.NoError(t, setBreakGlassLogin(bgOptions(), testClient(f, createLogin), term))
		assert.Empty(t, f.writes())
		assert.Contains(t, out.String(), "書き込みません")
	})

	t.Run("2回の打ち込みが違えば errPasswordMismatch で書き込まない", func(t *testing.T) {
		t.Parallel()

		f := loginWorld(t, nil)
		term, _ := tty("tabp\n")
		term.hidden = hiddenLines(testPassword, testPassword+"x")

		require.ErrorIs(t, setBreakGlassLogin(bgOptions(), testClient(f, createLogin), term), errPasswordMismatch)
		assert.Empty(t, f.writes())
	})

	t.Run("空のパスワードは errPasswordMismatch", func(t *testing.T) {
		t.Parallel()

		f := loginWorld(t, nil)
		term, _ := tty("tabp\n")
		term.hidden = hiddenLines("", "")

		require.ErrorIs(t, setBreakGlassLogin(bgOptions(), testClient(f, createLogin), term), errPasswordMismatch)
		assert.Empty(t, f.writes())
	})

	t.Run("確認が project の名前でなければ、パスワードを読まずに書き込まない", func(t *testing.T) {
		t.Parallel()

		f := loginWorld(t, nil)
		term, _ := tty("other\n")
		term.hidden = func() ([]byte, error) { t.Error("パスワードを読んだ"); return nil, errNoInput }

		require.ErrorIs(t, setBreakGlassLogin(bgOptions(), testClient(f, createLogin), term), errNotConfirmed)
		assert.Empty(t, f.writes())
	})

	t.Run("端末でなければ errNotTerminal", func(t *testing.T) {
		t.Parallel()

		f := loginWorld(t, nil)
		term := newTerminal(strings.NewReader("tabp\n"), &bytes.Buffer{}, false)

		require.ErrorIs(t, setBreakGlassLogin(bgOptions(), testClient(f, createLogin), term), errNotTerminal)
		assert.Empty(t, f.writes())
	})

	t.Run("user が無ければ errPrecondition で書き込まない", func(t *testing.T) {
		t.Parallel()

		f := loginWorld(t, nil)
		f.responses[getUser(bg1)] = fail(t, "An error occurred (NoSuchEntity)")
		term, _ := tty("tabp\n")

		require.ErrorIs(t, setBreakGlassLogin(bgOptions(), testClient(f, createLogin), term), errPrecondition)
		assert.Empty(t, f.writes())
	})

	t.Run("置いた後に読めなければ errAWSResponse", func(t *testing.T) {
		t.Parallel()

		f := bgWorld(t, map[string]bool{bg1: true}, nil)
		f.responses[createLogin] = ok(`{}`)
		term, _ := tty("tabp\n")
		term.hidden = hiddenLines(testPassword, testPassword)

		require.ErrorIs(t, setBreakGlassLogin(bgOptions(), testClient(f, createLogin), term), errAWSResponse)
	})

	t.Run("password policy に合わなければ、aws の失敗をそのまま返す", func(t *testing.T) {
		t.Parallel()

		f := bgWorld(t, map[string]bool{bg1: true}, nil)
		f.responses[createLogin] = fail(t, "An error occurred (PasswordPolicyViolation)")
		term, _ := tty("tabp\n")
		term.hidden = hiddenLines("short", "short")

		err := setBreakGlassLogin(bgOptions(), testClient(f, createLogin), term)
		require.ErrorIs(t, err, errAWSFailed)
		assert.Contains(t, err.Error(), "PasswordPolicyViolation")
	})
}

func mfaWorld(t *testing.T, before map[string][]string) *fakeAWS {
	t.Helper()

	f := bgWorld(t, nil, before)

	f.responses[enableMFA] = ok("")
	f.onWrite(enableMFA, map[string]awsResult{listMFA(bg1): mfaJSON(hwSerial), listMFA(bg2): mfaJSON(hwSerial)})

	return f
}

func Test_setBreakGlassMFA(t *testing.T) {
	t.Parallel()

	t.Run("MFA の無い user にだけ、シリアルと2つのコードを一時ファイルで渡して登録し、読み直す", func(t *testing.T) {
		t.Parallel()

		f := mfaWorld(t, map[string][]string{bg1: {fidoMFA}})
		term, out := tty("tabp\n" + hwSerial + "\n123456\n654321\n")

		require.NoError(t, setBreakGlassMFA(bgOptions(), testClient(f, enableMFA), term))
		assert.Equal(t, []string{enableMFA}, f.writes())
		assert.Equal(t, []string{`{"AuthenticationCode1":"123456","AuthenticationCode2":"654321","SerialNumber":"` + hwSerial + `","UserName":"` + bg2 + `"}`}, f.secrets)
		assert.Contains(t, out.String(), "✅ "+bg2+" に MFA を登録しました")
	})

	t.Run("すべてに MFA が在れば、書き込まない", func(t *testing.T) {
		t.Parallel()

		f := mfaWorld(t, map[string][]string{bg1: {hwSerial}, bg2: {virtualMFA}})
		term, out := tty("")

		require.NoError(t, setBreakGlassMFA(bgOptions(), testClient(f, enableMFA), term))
		assert.Empty(t, f.writes())
		assert.Contains(t, out.String(), "仮想 MFA は break-glass-check が NG にします")
	})

	t.Run("確認が project の名前でなければ書き込まない", func(t *testing.T) {
		t.Parallel()

		f := mfaWorld(t, nil)
		term, _ := tty("x\n")

		require.ErrorIs(t, setBreakGlassMFA(bgOptions(), testClient(f, enableMFA), term), errNotConfirmed)
		assert.Empty(t, f.writes())
	})

	t.Run("user が無ければ errPrecondition", func(t *testing.T) {
		t.Parallel()

		f := mfaWorld(t, nil)
		f.responses[getUser(bg2)] = fail(t, "An error occurred (NoSuchEntity)")
		term, _ := tty("tabp\n")

		require.ErrorIs(t, setBreakGlassMFA(bgOptions(), testClient(f, enableMFA), term), errPrecondition)
	})
}

func Test_registerTOTP(t *testing.T) {
	t.Parallel()

	t.Run("登録した装置を読み直せれば nil", func(t *testing.T) {
		t.Parallel()

		f := mfaWorld(t, nil)
		term, _ := tty(hwSerial + "\n123456\n654321\n")
		require.NoError(t, registerTOTP(testClient(f, enableMFA), term, bg1))
	})

	for name, input := range map[string]string{
		"ARN のシリアルは errMFAInput": virtualMFA + "\n123456\n654321\n",
		"短いシリアルは errMFAInput":    "GAKT1\n123456\n654321\n",
		"6桁でないコードは errMFAInput":  hwSerial + "\n12345\n654321\n",
		"数字でないコードは errMFAInput":  hwSerial + "\n12345a\n654321\n",
		"同じ2つのコードは errMFAInput":  hwSerial + "\n123456\n123456\n",
		"空白を含むシリアルは errMFAInput": "GAKT 12345678\n123456\n654321\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := mfaWorld(t, nil)
			term, _ := tty(input)
			require.ErrorIs(t, registerTOTP(testClient(f, enableMFA), term, bg1), errMFAInput)
			assert.Empty(t, f.writes())
		})
	}

	t.Run("登録した後に装置が無ければ errAWSResponse", func(t *testing.T) {
		t.Parallel()

		f := bgWorld(t, nil, nil)
		f.responses[enableMFA] = ok("")
		term, _ := tty(hwSerial + "\n123456\n654321\n")
		require.ErrorIs(t, registerTOTP(testClient(f, enableMFA), term, bg1), errAWSResponse)
	})

	t.Run("入力が尽きれば errNoInput", func(t *testing.T) {
		t.Parallel()

		term, _ := tty(hwSerial + "\n")
		require.ErrorIs(t, registerTOTP(testClient(mfaWorld(t, nil), enableMFA), term, bg1), errNoInput)
	})
}

func Test_checkBreakGlass(t *testing.T) {
	t.Parallel()

	t.Run("揃っていればすべて ✅", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer
		require.NoError(t, checkBreakGlass(bgOptions(), testClient(bgWorld(t, bothLogin(), bothMFA())), &out))
		assert.NotContains(t, out.String(), "❌")
		assert.Equal(t, 1+2*3+3, bytes.Count(out.Bytes(), []byte("✅")))
	})

	for name, tc := range map[string]struct {
		key  string
		res  func(t *testing.T) awsResult
		want string
	}{
		"path の下に宣言に無い user が在れば NG": {listBGUsers, func(*testing.T) awsResult {
			return ok(`{"Users":[{"UserName":"break-glass-1"},{"UserName":"break-glass-2"},{"UserName":"extra"}]}`)
		}, "❌ path /break-glass/ の IAM user"},
		"path の下に user が無ければ NG": {listBGUsers, func(*testing.T) awsResult { return ok(`{"Users":[]}`) }, "（今: なし）"},
		"パスワードが無ければ NG":          {getLogin(bg2), func(t *testing.T) awsResult { return fail(t, noSuchUser) }, "❌ " + bg2 + " にパスワードが在る"},
		"MFA が無ければ NG":           {listMFA(bg2), func(*testing.T) awsResult { return mfaJSON() }, "❌ " + bg2 + " にハードウェアの MFA"},
		"仮想 MFA が在れば NG":         {listMFA(bg1), func(*testing.T) awsResult { return mfaJSON(hwSerial, virtualMFA) }, "❌ " + bg1 + " にハードウェアの MFA"},
		"アクセスキーが在れば NG":          {listKeys(bg1), func(*testing.T) awsResult { return ok(`{"AccessKeyMetadata":[{"AccessKeyId":"AKIA"}]}`) }, "❌ " + bg1 + " にアクセスキーが無い（今: 1 件）"},
		"password policy の値が違えば NG": {passwordPol, func(*testing.T) awsResult {
			return ok(`{"PasswordPolicy":{"MinimumPasswordLength":8,"RequireSymbols":true,"HardExpiry":false}}`)
		}, "❌ password policy の MinimumPasswordLength が 14（今: 8）"},
		"password policy の field が無ければ NG": {passwordPol, func(*testing.T) awsResult {
			return ok(`{"PasswordPolicy":{"MinimumPasswordLength":14,"RequireSymbols":true}}`)
		}, "❌ password policy の HardExpiry が false（今: 無い）"},
		"password policy が無ければ NG": {passwordPol, func(t *testing.T) awsResult {
			return fail(t, "An error occurred (NoSuchEntity) when calling the GetAccountPasswordPolicy operation")
		}, "❌ アカウントの password policy が在る"},
		"宣言の user が無ければ NG": {getUser(bg2), func(t *testing.T) awsResult { return fail(t, "An error occurred (NoSuchEntity)") }, "❌ " + bg2 + " が在る"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := bgWorld(t, bothLogin(), bothMFA())
			f.responses[tc.key] = tc.res(t)

			var out bytes.Buffer
			require.ErrorIs(t, checkBreakGlass(bgOptions(), testClient(f), &out), errNG)
			assert.Contains(t, out.String(), tc.want)
		})
	}

	t.Run("宣言を読めなければ aws を起動しない", func(t *testing.T) {
		t.Parallel()

		f := bgWorld(t, nil, nil)
		o := bgOptions()
		o.readFile = readFiles(map[string]string{})
		require.ErrorIs(t, checkBreakGlass(o, testClient(f), &bytes.Buffer{}), errHCL)
		assert.Empty(t, f.ops())
	})
}

func Test_checkBreakGlassUser(t *testing.T) {
	t.Parallel()

	t.Run("AccessKeyMetadata の無い応答は errAWSResponse", func(t *testing.T) {
		t.Parallel()

		f := bgWorld(t, bothLogin(), bothMFA())
		f.responses[listKeys(bg1)] = ok(`{}`)
		require.ErrorIs(t, checkBreakGlassUser(testClient(f), newReport(&bytes.Buffer{}, "T"), bg1), errAWSResponse)
	})

	t.Run("user が無ければ、ほかを読まずに NG を1つ数える", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{getUser(bg1): fail(t, "An error occurred (NoSuchEntity)")})
		r := newReport(&bytes.Buffer{}, "T")
		require.NoError(t, checkBreakGlassUser(testClient(f), r, bg1))
		assert.Equal(t, 1, r.ngs)
		assert.Equal(t, []string{"iam get-user"}, f.ops())
	})
}

func Test_checkPasswordPolicy(t *testing.T) {
	t.Parallel()

	t.Run("field ごとに字面で比べる", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{passwordPol: ok(bgPolicyJSON)})
		r := newReport(&bytes.Buffer{}, "T")
		require.NoError(t, checkPasswordPolicy(testClient(f), r, map[string]string{"MinimumPasswordLength": "14", "RequireSymbols": "true"}))
		assert.Equal(t, 2, r.count)
		assert.Equal(t, 0, r.ngs)
	})

	t.Run("ほかの失敗は errAWSFailed", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{passwordPol: fail(t, "AccessDenied")})
		require.ErrorIs(t, checkPasswordPolicy(testClient(f), newReport(&bytes.Buffer{}, "T"), map[string]string{"A": "1"}), errAWSFailed)
	})
}

func usageWorld(t *testing.T, user1, user2, role string) *fakeAWS {
	t.Helper()

	field := func(name, v string) string {
		if v == "" {
			return ""
		}

		return `,"` + name + `":"` + v + `"`
	}

	return newFakeAWS(t, map[string]awsResult{
		getUser(bg1): ok(`{"User":{"UserName":"` + bg1 + `"` + field("PasswordLastUsed", user1) + `}}`),
		getUser(bg2): ok(`{"User":{"UserName":"` + bg2 + `"` + field("PasswordLastUsed", user2) + `}}`),
		"iam get-role --role-name tabp-break-glass": ok(`{"Role":{"RoleName":"tabp-break-glass","RoleLastUsed":{` +
			strings.TrimPrefix(field("LastUsedDate", role), ",") + `}}}`),
	})
}

func daysAgo(d int) string {
	return testNow.Add(-time.Duration(d) * 24 * time.Hour).Format(time.RFC3339)
}

func Test_checkBreakGlassUsage(t *testing.T) {
	t.Parallel()

	t.Run("44日前の使用は ✅", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer
		require.NoError(t, checkBreakGlassUsage(bgOptions(), testClient(usageWorld(t, daysAgo(44), daysAgo(1), daysAgo(44))), &out))
		assert.Contains(t, out.String(), "✅ "+bg1+" の最後のサインイン: 2026-10-13（44 日前）")
		assert.Equal(t, 3, bytes.Count(out.Bytes(), []byte("✅")))
	})

	for name, tc := range map[string]struct{ u1, u2, role, want string }{
		"45日前のサインインは NG":       {daysAgo(45), daysAgo(1), daysAgo(1), "❌ " + bg1 + " の最後のサインイン: 2026-10-12（45 日前）"},
		"サインインの記録が無ければ NG":     {daysAgo(1), "", daysAgo(1), "❌ " + bg2 + " の最後のサインイン: 記録が無い"},
		"45日前の role の引き受けは NG": {daysAgo(1), daysAgo(1), daysAgo(45), "❌ role tabp-break-glass の最後の引き受け"},
		"引き受けの記録が無ければ NG":      {daysAgo(1), daysAgo(1), "", "❌ role tabp-break-glass の最後の引き受け: 記録が無い"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			require.ErrorIs(t, checkBreakGlassUsage(bgOptions(), testClient(usageWorld(t, tc.u1, tc.u2, tc.role)), &out), errNG)
			assert.Contains(t, out.String(), tc.want)
		})
	}

	t.Run("user か role が無ければ NG", func(t *testing.T) {
		t.Parallel()

		f := usageWorld(t, daysAgo(1), daysAgo(1), daysAgo(1))
		f.responses[getUser(bg2)] = fail(t, "An error occurred (NoSuchEntity)")
		f.responses["iam get-role --role-name tabp-break-glass"] = fail(t, "An error occurred (NoSuchEntity)")

		var out bytes.Buffer
		require.ErrorIs(t, checkBreakGlassUsage(bgOptions(), testClient(f), &out), errNG)
		assert.Contains(t, out.String(), "❌ "+bg2+" が在る")
		assert.Contains(t, out.String(), "❌ role tabp-break-glass が在る")
	})
}

func Test_recent(t *testing.T) {
	t.Parallel()

	t.Run("45日未満なら true、45日ちょうどと nil は false", func(t *testing.T) {
		t.Parallel()

		just := testNow.Add(-unusedDays * 24 * time.Hour)
		inside := just.Add(time.Second)

		assert.True(t, recent(testNow, &inside))
		assert.False(t, recent(testNow, &just))
		assert.False(t, recent(testNow, nil))
	})
}

func Test_lastUsed(t *testing.T) {
	t.Parallel()

	t.Run("日付と日数、nil は「記録が無い」", func(t *testing.T) {
		t.Parallel()

		d := testNow.Add(-3 * 24 * time.Hour)
		assert.Equal(t, "2026-11-23（3 日前）", lastUsed(testNow, &d))
		assert.Equal(t, "記録が無い", lastUsed(testNow, nil))
	})
}

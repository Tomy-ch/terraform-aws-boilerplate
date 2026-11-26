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
	listBGUsers  = "iam list-users --path-prefix /break-glass/"
	passwordPol  = "iam get-account-password-policy"
	bgPolicyJSON = `{"PasswordPolicy":{"MinimumPasswordLength":14,"PasswordReusePrevention":24,"RequireLowercaseCharacters":true,` +
		`"RequireUppercaseCharacters":true,"RequireNumbers":true,"RequireSymbols":true,"AllowUsersToChangePassword":true,` +
		`"MaxPasswordAge":90,"HardExpiry":false,"ExpirePasswords":true}}`
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

func bothMFA() map[string][]string { return map[string][]string{bg1: {fidoMFA}, bg2: {fidoMFA + "2"}} }

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

	t.Run("ほかの失敗は「無い」にせず errAWSFailed", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{getUser(bg1): fail(t, "AccessDenied")})
		_, err := userExists(testClient(f), bg1)
		require.ErrorIs(t, err, errAWSFailed)
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

	t.Run("読めなければ errPrecondition にせず errAWSFailed", func(t *testing.T) {
		t.Parallel()

		f := bgWorld(t, nil, nil)
		f.responses[getUser(bg1)] = fail(t, "AccessDenied")
		err := requireUsers(testClient(f), []string{bg1, bg2})
		require.ErrorIs(t, err, errAWSFailed)
		assert.NotErrorIs(t, err, errPrecondition)
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

func Test_isFIDO(t *testing.T) {
	t.Parallel()

	t.Run("u2f/ の ARN だけが FIDO", func(t *testing.T) {
		t.Parallel()

		assert.True(t, isFIDO(fidoMFA))
		assert.False(t, isFIDO(virtualMFA))
		assert.False(t, isFIDO(hwSerial))
		assert.False(t, isFIDO("u2f/user/x"))
	})
}

func Test_hasOnlyFIDO(t *testing.T) {
	t.Parallel()

	t.Run("FIDO の装置だけが1つ以上在れば true", func(t *testing.T) {
		t.Parallel()

		assert.True(t, hasOnlyFIDO([]string{fidoMFA}))
		assert.True(t, hasOnlyFIDO([]string{fidoMFA, fidoMFA + "2"}))
	})

	t.Run("0件か、TOTP（仮想かハードウェア）が混じれば false", func(t *testing.T) {
		t.Parallel()

		assert.False(t, hasOnlyFIDO(nil))
		assert.False(t, hasOnlyFIDO([]string{virtualMFA}))
		assert.False(t, hasOnlyFIDO([]string{hwSerial}))
		assert.False(t, hasOnlyFIDO([]string{fidoMFA, virtualMFA}))
		assert.False(t, hasOnlyFIDO([]string{fidoMFA, hwSerial}))
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

	t.Run("2人とも無ければ確認は1回で、順に2件置く", func(t *testing.T) {
		t.Parallel()

		f := loginWorld(t, nil)
		term, out := tty("tabp\n")
		term.hidden = hiddenLines(testPassword, testPassword, testPassword, testPassword)

		require.NoError(t, setBreakGlassLogin(bgOptions(), testClient(f, createLogin), term))
		assert.Equal(t, []string{createLogin, createLogin}, f.writes())
		assert.Len(t, f.secrets, 2)
		assert.Contains(t, f.secrets[0], `"UserName":"`+bg1+`"`)
		assert.Contains(t, f.secrets[1], `"UserName":"`+bg2+`"`)
		assert.Equal(t, 1, strings.Count(out.String(), "project の名前"))
	})

	t.Run("2人目の打ち込みが違えば、1人目だけを置いて errPasswordMismatch", func(t *testing.T) {
		t.Parallel()

		f := loginWorld(t, nil)
		term, _ := tty("tabp\n")
		term.hidden = hiddenLines(testPassword, testPassword, testPassword, testPassword+"x")

		require.ErrorIs(t, setBreakGlassLogin(bgOptions(), testClient(f, createLogin), term), errPasswordMismatch)
		assert.Equal(t, []string{createLogin}, f.writes())
		assert.Contains(t, f.secrets[0], `"UserName":"`+bg1+`"`)
	})

	t.Run("宣言を読めなければ aws を起動しない", func(t *testing.T) {
		t.Parallel()

		f := bgWorld(t, nil, nil)
		o := bgOptions()
		o.readFile = readFiles(map[string]string{})
		term, _ := tty("tabp\n")
		require.ErrorIs(t, setBreakGlassLogin(o, testClient(f, createLogin), term), errHCL)
		assert.Empty(t, f.ops())
	})

	t.Run("置いた直後に見えなくても、読み直しをやり直して3回目に見えれば ✅", func(t *testing.T) {
		t.Parallel()

		f := loginWorld(t, map[string]bool{bg1: true})
		f.seq = map[string][]awsResult{getLogin(bg2): {fail(t, noSuchUser), fail(t, noSuchUser), fail(t, noSuchUser)}}
		term, out := tty("tabp\n")
		term.hidden = hiddenLines(testPassword, testPassword)

		var waits int

		o := bgOptions()
		o.sleep = func(time.Duration) { waits++ }

		require.NoError(t, setBreakGlassLogin(o, testClient(f, createLogin), term))
		assert.Equal(t, 2, waits)
		assert.Contains(t, out.String(), "✅ "+bg2+" にパスワードを置きました")
	})

	t.Run("読み直しを尽くしても見えなければ errAWSResponse で、break-glass-check を案内し ✅ を出さない", func(t *testing.T) {
		t.Parallel()

		f := bgWorld(t, map[string]bool{bg1: true}, nil)
		f.responses[createLogin] = ok(`{}`)
		term, out := tty("tabp\n")
		term.hidden = hiddenLines(testPassword, testPassword)

		var waits int

		o := bgOptions()
		o.sleep = func(time.Duration) { waits++ }

		err := setBreakGlassLogin(o, testClient(f, createLogin), term)
		require.ErrorIs(t, err, errAWSResponse)
		assert.Contains(t, err.Error(), "break-glass-check")
		assert.Equal(t, rereadTries-1, waits)
		assert.NotContains(t, out.String(), "にパスワードを置きました")
	})
}

func Test_checkBreakGlass(t *testing.T) {
	t.Parallel()

	t.Run("揃っていればすべて ✅", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer
		require.NoError(t, checkBreakGlass(bgOptions(), testClient(bgWorld(t, bothLogin(), bothMFA())), &out))
		assert.NotContains(t, out.String(), "❌")
		assert.Equal(t, 1+2*3+len(passwordPolicyFields), bytes.Count(out.Bytes(), []byte("✅")))
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
		"MFA が無ければ NG":           {listMFA(bg2), func(*testing.T) awsResult { return mfaJSON() }, "❌ " + bg2 + " の MFA がすべて FIDO"},
		"仮想 MFA が在れば NG":         {listMFA(bg1), func(*testing.T) awsResult { return mfaJSON(fidoMFA, virtualMFA) }, "❌ " + bg1 + " の MFA がすべて FIDO"},
		"ハードウェアの TOTP が在れば NG":   {listMFA(bg1), func(*testing.T) awsResult { return mfaJSON(fidoMFA, hwSerial) }, "❌ " + bg1 + " の MFA がすべて FIDO"},
		"ハードウェアの TOTP だけなら NG":   {listMFA(bg2), func(*testing.T) awsResult { return mfaJSON(hwSerial) }, "❌ " + bg2 + " の MFA がすべて FIDO"},
		"アクセスキーが在れば NG":          {listKeys(bg1), func(*testing.T) awsResult { return ok(`{"AccessKeyMetadata":[{"AccessKeyId":"AKIA"}]}`) }, "❌ " + bg1 + " にアクセスキーが無い（今: 1 件）"},
		"password policy の値が違えば NG": {passwordPol, func(*testing.T) awsResult {
			return ok(strings.Replace(bgPolicyJSON, `"MinimumPasswordLength":14`, `"MinimumPasswordLength":8`, 1))
		}, "❌ password policy の MinimumPasswordLength が 14（今: 8）"},
		"password policy の数の field が無ければ NG": {passwordPol, func(*testing.T) awsResult {
			return ok(strings.Replace(bgPolicyJSON, `"MaxPasswordAge":90,`, "", 1))
		}, "❌ password policy の MaxPasswordAge が 90（今: 無い）"},
		"password policy の真の field が無ければ NG": {passwordPol, func(*testing.T) awsResult {
			return ok(strings.Replace(bgPolicyJSON, `"RequireSymbols":true,`, "", 1))
		}, "❌ password policy の RequireSymbols が true（今: 無い）"},
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
			assert.Equal(t, 1, strings.Count(out.String(), "❌"))
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

	for name, key := range map[string]string{
		"list-users の失敗は errAWSFailed で NG に数えない":       listBGUsers,
		"list-access-keys の失敗は errAWSFailed で NG に数えない": listKeys(bg1),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := bgWorld(t, bothLogin(), bothMFA())
			f.responses[key] = fail(t, "AccessDenied")
			err := checkBreakGlass(bgOptions(), testClient(f), &bytes.Buffer{})
			require.ErrorIs(t, err, errAWSFailed)
			assert.NotErrorIs(t, err, errNG)
		})
	}
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

		var out bytes.Buffer

		require.NoError(t, checkBreakGlassUser(testClient(f), newReport(&out, "T"), bg1))
		assert.Equal(t, "# T\n❌ "+bg1+" が在る\n", out.String())
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

	t.Run("応答に無い真偽の field は false とみなし、true は無い扱いにする", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{passwordPol: ok(`{"PasswordPolicy":{"MinimumPasswordLength":14}}`)})

		var out bytes.Buffer

		require.NoError(t, checkPasswordPolicy(testClient(f), newReport(&out, "T"), map[string]string{"HardExpiry": "false", "RequireSymbols": "true"}))
		assert.Contains(t, out.String(), "✅ password policy の HardExpiry が false（今: false）")
		assert.Contains(t, out.String(), "❌ password policy の RequireSymbols が true（今: 無い）")
	})

	t.Run("宣言の false に対して応答が true なら NG", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{passwordPol: ok(`{"PasswordPolicy":{"HardExpiry":true}}`)})

		var out bytes.Buffer

		require.NoError(t, checkPasswordPolicy(testClient(f), newReport(&out, "T"), map[string]string{"HardExpiry": "false"}))
		assert.Contains(t, out.String(), "❌ password policy の HardExpiry が false（今: true）")
	})

	t.Run("空の PasswordPolicy なら、真と数の項目が NG で、偽の項目だけが ✅", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{passwordPol: ok(`{"PasswordPolicy":{}}`)})

		var out bytes.Buffer

		require.NoError(t, checkPasswordPolicy(testClient(f), newReport(&out, "T"), map[string]string{"HardExpiry": "false", "RequireSymbols": "true", "MaxPasswordAge": "90"}))
		assert.Equal(t, 2, strings.Count(out.String(), "❌"))
		assert.Equal(t, 1, strings.Count(out.String(), "✅"))
	})

	t.Run("ほかの失敗は errAWSFailed", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{passwordPol: fail(t, "AccessDenied")})
		require.ErrorIs(t, checkPasswordPolicy(testClient(f), newReport(&bytes.Buffer{}, "T"), map[string]string{"A": "1"}), errAWSFailed)
	})

	t.Run("省かれうる field の外の偽は、応答に無ければ NG", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{passwordPol: ok(`{"PasswordPolicy":{}}`)})

		var out bytes.Buffer

		require.NoError(t, checkPasswordPolicy(testClient(f), newReport(&out, "T"), map[string]string{"AllowUsersToChangePassword": "false"}))
		assert.Contains(t, out.String(), "❌ password policy の AllowUsersToChangePassword が false（今: 無い）")
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

	for name, tc := range map[string]struct{ key, stdout string }{
		"get-user の UserName が無ければ errAWSResponse": {getUser(bg1), `{"User":{"PasswordLastUsed":"` + daysAgo(1) + `"}}`},
		"get-role の RoleName が無ければ errAWSResponse": {"iam get-role --role-name tabp-break-glass", `{"Role":{"RoleLastUsed":{"LastUsedDate":"` + daysAgo(1) + `"}}}`},
		"PasswordLastUsed が日時でなければ errAWSResponse": {getUser(bg1), `{"User":{"UserName":"` + bg1 + `","PasswordLastUsed":"yesterday"}}`},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := usageWorld(t, daysAgo(1), daysAgo(1), daysAgo(1))
			f.responses[tc.key] = ok(tc.stdout)
			require.ErrorIs(t, checkBreakGlassUsage(bgOptions(), testClient(f), &bytes.Buffer{}), errAWSResponse)
		})
	}

	for name, key := range map[string]string{
		"get-user の失敗は errAWSFailed で NG に数えない": getUser(bg1),
		"get-role の失敗は errAWSFailed で NG に数えない": "iam get-role --role-name tabp-break-glass",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := usageWorld(t, daysAgo(1), daysAgo(1), daysAgo(1))
			f.responses[key] = fail(t, "AccessDenied")
			err := checkBreakGlassUsage(bgOptions(), testClient(f), &bytes.Buffer{})
			require.ErrorIs(t, err, errAWSFailed)
			assert.NotErrorIs(t, err, errNG)
		})
	}

	t.Run("宣言を読めなければ aws を起動しない", func(t *testing.T) {
		t.Parallel()

		f := usageWorld(t, daysAgo(1), daysAgo(1), daysAgo(1))
		o := bgOptions()
		o.readFile = readFiles(map[string]string{})
		require.ErrorIs(t, checkBreakGlassUsage(o, testClient(f), &bytes.Buffer{}), errHCL)
		assert.Empty(t, f.ops())
	})

	for name, tc := range map[string]struct{ key, stdout string }{
		"get-user が別の user を返せば errAWSResponse": {getUser(bg1), `{"User":{"UserName":"` + bg2 + `","PasswordLastUsed":"` + daysAgo(1) + `"}}`},
		"get-role が別の role を返せば errAWSResponse": {"iam get-role --role-name tabp-break-glass", `{"Role":{"RoleName":"other","RoleLastUsed":{"LastUsedDate":"` + daysAgo(1) + `"}}}`},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := usageWorld(t, daysAgo(1), daysAgo(1), daysAgo(1))
			f.responses[tc.key] = ok(tc.stdout)
			require.ErrorIs(t, checkBreakGlassUsage(bgOptions(), testClient(f), &bytes.Buffer{}), errAWSResponse)
		})
	}
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

func Test_reread(t *testing.T) {
	t.Parallel()

	t.Run("見えるまでやり直し、やり直しの間だけ待つ", func(t *testing.T) {
		t.Parallel()

		var waits, reads int

		o := bgOptions()
		o.sleep = func(time.Duration) { waits++ }

		require.NoError(t, reread(o, func() (bool, error) { reads++; return reads == 3, nil }))
		assert.Equal(t, 3, reads)
		assert.Equal(t, 2, waits)
	})

	t.Run("回数を尽くしても見えなければ errAWSResponse", func(t *testing.T) {
		t.Parallel()

		var reads int

		var waits int

		o := bgOptions()
		o.sleep = func(time.Duration) { waits++ }

		require.ErrorIs(t, reread(o, func() (bool, error) { reads++; return false, nil }), errAWSResponse)
		assert.Equal(t, rereadTries, reads)
		assert.Equal(t, rereadTries-1, waits)
	})

	t.Run("最後の回で見えれば nil", func(t *testing.T) {
		t.Parallel()

		var reads int

		require.NoError(t, reread(bgOptions(), func() (bool, error) { reads++; return reads == rereadTries, nil }))
		assert.Equal(t, rereadTries, reads)
	})

	t.Run("読み取りの失敗はやり直さずに返す", func(t *testing.T) {
		t.Parallel()

		var reads int

		require.ErrorIs(t, reread(bgOptions(), func() (bool, error) { reads++; return false, errAWSFailed }), errAWSFailed)
		assert.Equal(t, 1, reads)
	})
}

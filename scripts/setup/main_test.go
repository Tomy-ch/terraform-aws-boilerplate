package main

import (
	"bytes"
	"maps"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_main(t *testing.T) {
	t.Skip("main は os の標準入出力と終了を execute へ渡すだけで、プロセスを終えるので単体では呼べない")
}

// testDeps は、fixture の Terraform を -usecase uc の下に持ち、読んだパスを read に覚える deps。
func testDeps(f *fakeAWS, input string, read *[]string) deps {
	files := readFiles(fixtureFiles(usecaseMainTF, ouMainTF, accountsTF))

	return deps{
		aws:    f.fn(),
		getenv: profileEnv,
		readFile: func(name string) ([]byte, error) {
			if read != nil {
				*read = append(*read, name)
			}

			return files(name)
		},
		stdin:  strings.NewReader(input),
		isTTY:  true,
		hidden: hiddenLines(testPassword, testPassword, testPassword, testPassword),
		now:    func() time.Time { return testNow },
		sleep:  func(time.Duration) {},
	}
}

func Test_entries(t *testing.T) {
	t.Parallel()

	t.Run("入口ごとの書き込みの操作は、この一覧に閉じている", func(t *testing.T) {
		t.Parallel()

		want := map[string][]string{
			"member-emails":              {"ssm put-parameter"},
			"member-emails-check":        nil,
			"member-email-update":        {"organizations enable-aws-service-access", "account start-primary-email-update", "account accept-primary-email-update", "ssm put-parameter"},
			"account-quota-check":        nil,
			"member-accounts-check":      nil,
			"stacksets-access":           {"cloudformation activate-organizations-access"},
			"stacksets-access-check":     nil,
			"stack-instances-check":      nil,
			"service-access":             {"organizations enable-aws-service-access"},
			"service-access-check":       nil,
			"delegated-admin-check":      nil,
			"organization-check":         nil,
			"identity-center-check":      nil,
			"identity-center-user-check": nil,
			"break-glass-login":          {"iam create-login-profile"},
			"break-glass-check":          nil,
			"break-glass-usage-check":    nil,
		}
		require.Equal(t, slices.Sorted(maps.Keys(want)), slices.Sorted(maps.Keys(entries)))

		for name, w := range want {
			assert.Equal(t, w, entries[name].writes, name)
		}
	})

	t.Run("-check の入口は書き込みの操作を持たない", func(t *testing.T) {
		t.Parallel()

		var checks int

		for name, e := range entries {
			if strings.HasSuffix(name, "-check") {
				checks++

				assert.Empty(t, e.writes, name)
			}
		}

		assert.Equal(t, 12, checks)
	})

	// 各入口を run から起動し、入口に固有の操作が起動されることで、entries の配線（run・usecase・needsEnv）を固定する。
	runs := map[string]struct {
		args  []string
		world func(t *testing.T) *fakeAWS
		input string
		want  string
	}{
		"member-emails": {[]string{"-usecase", testUsecase}, func(t *testing.T) *fakeAWS {
			f := emailsWorld(t, "", desiredValue)
			maps.Copy(f.responses, managementWorld("111111111111", "111111111111"))

			return f
		}, "la@x.y\nsec@x.y\ntabp\n", putParam(desiredValue)},
		"member-emails-check": {[]string{"-usecase", testUsecase}, func(t *testing.T) *fakeAWS {
			return emailsWorld(t, desiredValue, desiredValue)
		}, "", "ssm get-parameter --name " + paramName},
		"member-email-update": {[]string{"-usecase", testUsecase, "-env", "security"}, func(t *testing.T) *fakeAWS {
			f := updateWorld(t, "sec@x.y", "new@x.y", true)
			maps.Copy(f.responses, managementWorld("111111111111", "111111111111"))

			return f
		}, "new@x.y\ntabp\nABC123\n", acceptUpdate},
		"account-quota-check": {[]string{"-usecase", testUsecase}, func(t *testing.T) *fakeAWS {
			return quotaWorld(t, quotaList, `{"Quota":{"Value":3}}`)
		}, "", getQuotaValue},
		"member-accounts-check": {[]string{"-usecase", testUsecase}, func(t *testing.T) *fakeAWS {
			return memberAccountsWorld(t, securityAccount+","+logArchive, allAccountsJSON)
		}, "", underSecurityKey},
		"stacksets-access": {nil, func(t *testing.T) *fakeAWS {
			f := stackSetsWorld(t, "DISABLED", "ENABLED", false, true)
			maps.Copy(f.responses, managementWorld("111111111111", "111111111111"))

			return f
		}, "tabp\n", activate},
		"stacksets-access-check": {nil, func(t *testing.T) *fakeAWS {
			return stackSetsWorld(t, "ENABLED", "", true, true)
		}, "", getRole},
		"stack-instances-check": {[]string{"-usecase", testUsecase}, func(t *testing.T) *fakeAWS {
			return stackInstancesWorld(t, `{"StackSetName":"tabp-break-glass"}`, instance("222222222222", "CURRENT")+","+instance("333333333333", "CURRENT"))
		}, "", breakGlassInstances},
		"service-access": {nil, func(t *testing.T) *fakeAWS {
			f := serviceWorld(t, principals(), principals(securityServicePrincipals...))
			maps.Copy(f.responses, managementWorld("111111111111", "111111111111"))

			return f
		}, "tabp\n", enablePrincipal("config.amazonaws.com")},
		"service-access-check": {nil, func(t *testing.T) *fakeAWS {
			return serviceWorld(t, principals(securityServicePrincipals...), "")
		}, "", "organizations list-aws-service-access-for-organization"},
		"delegated-admin-check": {[]string{"-usecase", testUsecase}, func(t *testing.T) *fakeAWS {
			return delegatedWorld(t, `{"Id":"222222222222","Name":"tabp-security","State":"ACTIVE"}`,
				map[string]string{"cloudtrail.amazonaws.com": `{"Id":"222222222222"}`, "config.amazonaws.com": `{"Id":"222222222222"}`})
		}, "", "organizations list-delegated-administrators --service-principal config.amazonaws.com"},
		"organization-check": {[]string{"-usecase", testUsecase}, func(t *testing.T) *fakeAWS {
			return orgWorld(t, nil)
		}, "", topOUsCall},
		"identity-center-check": {nil, func(t *testing.T) *fakeAWS {
			return orgWorld(t, map[string]awsResult{listSSO: ok(ssoJSON)})
		}, "", listSSO},
		"identity-center-user-check": {[]string{"-usecase", testUsecase, "-user", "alice"}, func(t *testing.T) *fakeAWS {
			return idcUserWorld(t, ok(`{"UserId":"u-1"}`), `{"UserName":"alice"}`)
		}, "", getUserID},
		"break-glass-login": {[]string{"-usecase", testUsecase}, func(t *testing.T) *fakeAWS {
			return loginWorld(t, map[string]bool{bg1: true})
		}, "tabp\n", createLogin},
		"break-glass-check": {[]string{"-usecase", testUsecase}, func(t *testing.T) *fakeAWS {
			return bgWorld(t, bothLogin(), bothMFA())
		}, "", passwordPol},
		"break-glass-usage-check": {[]string{"-usecase", testUsecase}, func(t *testing.T) *fakeAWS {
			return usageWorld(t, daysAgo(1), daysAgo(1), daysAgo(1))
		}, "", "iam get-role --role-name tabp-break-glass"},
	}

	t.Run("run の表が entries のすべての入口を覆う", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, slices.Sorted(maps.Keys(entries)), slices.Sorted(maps.Keys(runs)))
	})

	for name, tc := range runs {
		t.Run(name+" を run から起動すると、その入口に固有の操作を起動して完了する", func(t *testing.T) {
			t.Parallel()

			f := tc.world(t)
			args := append([]string{name, "-project", testProject, "-region", testRegion}, tc.args...)
			require.NoError(t, run(args, &bytes.Buffer{}, testDeps(f, tc.input, nil)))
			assert.Contains(t, append(f.joined(), f.writes()...), tc.want)

			if len(entries[name].writes) > 0 {
				assert.Equal(t, "sts get-caller-identity", f.joined()[0])
			} else {
				assert.NotContains(t, f.joined(), "sts get-caller-identity")
			}
		})
	}
}

func Test_execute(t *testing.T) {
	t.Parallel()

	t.Run("出力とエラーの行の12桁の数字を伏せる", func(t *testing.T) {
		t.Parallel()

		f := memberAccountsWorld(t, securityAccount, allAccountsJSON)
		f.responses[underSecurityKey] = fail(t, "An error occurred (AccessDenied): 222222222222 is not authorized")

		var out bytes.Buffer

		code := execute([]string{"member-accounts-check", "-project", testProject, "-region", testRegion, "-usecase", testUsecase}, &out, testDeps(f, "", nil))
		assert.Equal(t, 2, code)
		assert.NotContains(t, out.String(), "222222222222")
		assert.Contains(t, out.String(), "<account> is not authorized")
	})

	t.Run("-ids なら伏せない", func(t *testing.T) {
		t.Parallel()

		f := memberAccountsWorld(t, securityAccount+","+logArchive, allAccountsJSON)

		var out bytes.Buffer

		code := execute([]string{"member-accounts-check", "-project", testProject, "-region", testRegion, "-usecase", testUsecase, "-ids"}, &out, testDeps(f, "", nil))
		assert.Equal(t, 0, code)
		assert.Contains(t, out.String(), "security の ID: 222222222222")
	})

	t.Run("確認の NG は 1 で、最後の行にエラーを出す", func(t *testing.T) {
		t.Parallel()

		f := memberAccountsWorld(t, securityAccount, allAccountsJSON)

		var out bytes.Buffer

		code := execute([]string{"member-accounts-check", "-project", testProject, "-region", testRegion, "-usecase", testUsecase}, &out, testDeps(f, "", nil))
		assert.Equal(t, 1, code)
		assert.True(t, strings.HasSuffix(out.String(), "❌ "+errNG.Error()+"\n"))
	})

	t.Run("引数の誤りは 2 で、-ids を付けても伏せる", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer
		assert.Equal(t, 2, execute([]string{"123456789012", "-ids"}, &out, testDeps(newFakeAWS(t, nil), "", nil)))
		assert.Contains(t, out.String(), "入口 <account> はありません")
		assert.NotContains(t, out.String(), "123456789012")
	})
}

func Test_isTerminal(t *testing.T) {
	t.Parallel()

	t.Run("pipe は端末でない", func(t *testing.T) {
		t.Parallel()

		r, w, err := os.Pipe()
		require.NoError(t, err)
		t.Cleanup(func() { _ = r.Close(); _ = w.Close() })
		assert.False(t, isTerminal(r))
	})

	t.Run("/dev/null は文字デバイスでも端末でない", func(t *testing.T) {
		t.Parallel()

		f, err := os.Open(os.DevNull)
		require.NoError(t, err)
		t.Cleanup(func() { _ = f.Close() })
		assert.False(t, isTerminal(f))
	})

	t.Run("通常のファイルは端末でない", func(t *testing.T) {
		t.Parallel()

		f, err := os.CreateTemp(t.TempDir(), "x")
		require.NoError(t, err)
		t.Cleanup(func() { _ = f.Close() })
		assert.False(t, isTerminal(f))
	})

	t.Run("閉じたファイルは端末でない", func(t *testing.T) {
		t.Parallel()

		f, err := os.CreateTemp(t.TempDir(), "x")
		require.NoError(t, err)
		require.NoError(t, f.Close())
		assert.False(t, isTerminal(f))
	})
}

func Test_exitCode(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		err  error
		want int
	}{
		"nil は 0":           {nil, 0},
		"確認の NG は 1":        {xerrors.Wrap(errNG, "x"), 1},
		"前提が崩れていれば 1":       {xerrors.Wrap(errPrecondition, "x"), 1},
		"確認の入力の不一致は 2":      {errNotConfirmed, 2},
		"aws の失敗は 2":        {errAWSFailed, 2},
		"使われているメールは 1":      {xerrors.Wrap(errEmailInUse, "x"), 1},
		"在るアカウントのメールの変更は 1": {xerrors.Wrap(errEmailChangeNeedsUpdate, "x"), 1},
		"同じ名前のアカウントは 1":     {xerrors.Wrap(errAccountName, "x"), 1},
		"OU が決まらなければ 1":     {xerrors.Wrap(errOUNotFound, "x"), 1},
		"Root が決まらなければ 1":   {errNoRoot, 1},
		"メールの形の誤りは 2":       {errEmailShape, 2},
		"メールの重複は 2":         {errEmailDuplicate, 2},
		"確認コードの形の誤りは 2":     {errOTPShape, 2},
		"読めない応答は 2":         {errAWSResponse, 2},
		"パスワードの不一致は 2":      {errPasswordMismatch, 2},
		"伏せて読めなければ 2":       {errNoHidden, 2},
		"中断は 2":             {errInterrupted, 2},
		"宣言を解釈できなければ 2":     {errDeclShape, 2},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, exitCode(tc.err))
		})
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, os.ErrClosed }

func Test_maskWriter_Write(t *testing.T) {
	t.Parallel()

	t.Run("12桁の数字を伏せ、受けた長さを返す", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer

		n, err := (&maskWriter{w: &out, mask: true}).Write([]byte("id 123456789012 x"))
		require.NoError(t, err)
		assert.Equal(t, 17, n)
		assert.Equal(t, "id <account> x", out.String())
	})

	t.Run("11桁は伏せず、13桁は先頭の12桁を伏せる", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer

		_, err := (&maskWriter{w: &out, mask: true}).Write([]byte("12345678901 1234567890123"))
		require.NoError(t, err)
		assert.Equal(t, "12345678901 <account>3", out.String())
	})

	t.Run("mask が偽ならそのまま書く", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer

		_, err := (&maskWriter{w: &out}).Write([]byte("123456789012"))
		require.NoError(t, err)
		assert.Equal(t, "123456789012", out.String())
	})

	t.Run("書けなければエラー", func(t *testing.T) {
		t.Parallel()

		_, err := (&maskWriter{w: failingWriter{}, mask: true}).Write([]byte("x"))
		require.ErrorIs(t, err, os.ErrClosed)
	})
}

func Test_parseArgs(t *testing.T) {
	t.Parallel()

	t.Run("入口と旗を読む", func(t *testing.T) {
		t.Parallel()

		o, e, err := parseArgs([]string{"member-email-update", "-project", "p", "-region", "r", "-usecase", "u", "-env", "security"})
		require.NoError(t, err)
		assert.Equal(t, options{entry: "member-email-update", project: "p", region: "r", usecase: "u", env: "security"}, o)
		assert.True(t, e.needsEnv)
	})

	t.Run("member-accounts-check は -ids を受ける", func(t *testing.T) {
		t.Parallel()

		o, _, err := parseArgs([]string{"member-accounts-check", "-project", "p", "-region", "r", "-usecase", "u", "-ids"})
		require.NoError(t, err)
		assert.True(t, o.showIDs)
	})

	bad := map[string]struct {
		args []string
		msg  string
	}{
		"引数が無い":       {[]string{}, "使い方"},
		"入口が無い":       {[]string{"nope", "-project", "p", "-region", "r"}, "入口 nope はありません"},
		"project が無い": {[]string{"stacksets-access", "-region", "r"}, "-project と -region は必須です"},
		"region が無い":  {[]string{"stacksets-access", "-project", "p"}, "-project と -region は必須です"},
		"余分な引数がある":    {[]string{"stacksets-access", "-project", "p", "-region", "r", "x"}, "余分な引数があります"},
		"知らない旗":       {[]string{"stacksets-access", "-project", "p", "-region", "r", "-x"}, "flag provided but not defined"},
		"Terraform を読む入口に -usecase が無い":          {[]string{"member-emails", "-project", "p", "-region", "r"}, "-usecase は"},
		"Terraform を読まない入口に -usecase":            {[]string{"stacksets-access", "-project", "p", "-region", "r", "-usecase", "a"}, "-usecase は"},
		"member-email-update に -env が無い":         {[]string{"member-email-update", "-project", "p", "-region", "r", "-usecase", "a"}, "-env は"},
		"ほかの入口に -env":                            {[]string{"member-emails", "-project", "p", "-region", "r", "-usecase", "a", "-env", "x"}, "-env は"},
		"identity-center-user-check に -user が無い": {[]string{"identity-center-user-check", "-project", "p", "-region", "r", "-usecase", "a"}, "-user は"},
		"ほかの入口に -user":                           {[]string{"break-glass-check", "-project", "p", "-region", "r", "-usecase", "a", "-user", "x"}, "-user は"},
		"member-accounts-check でない入口に -ids":      {[]string{"member-emails-check", "-project", "p", "-region", "r", "-usecase", "a", "-ids"}, "-ids は"},
	}
	for name, tc := range bad {
		t.Run(name+"なら errUsage", func(t *testing.T) {
			t.Parallel()

			_, _, err := parseArgs(tc.args)
			require.ErrorIs(t, err, errUsage)
			assert.Contains(t, err.Error(), tc.msg)
		})
	}
}

func Test_run(t *testing.T) {
	t.Parallel()

	args := []string{"member-emails", "-project", testProject, "-region", testRegion, "-usecase", testUsecase}

	t.Run("-usecase の下の3つのファイルを読み、管理アカウントを確かめてから入口を走らせる", func(t *testing.T) {
		t.Parallel()

		f := emailsWorld(t, "", desiredValue)
		maps.Copy(f.responses, managementWorld("111111111111", "111111111111"))

		var read []string

		var out bytes.Buffer
		require.NoError(t, run(args, &out, testDeps(f, "la@x.y\nsec@x.y\ntabp\n", &read)))
		assert.ElementsMatch(t, []string{"uc/main.tf", "uc/internal/organization/main.tf", "uc/internal/organization/accounts.tf"}, read)
		assert.Equal(t, []string{putParam(desiredValue)}, f.writes())
		assert.True(t, strings.HasPrefix(out.String(), "# 書き込む先: profile p、region ap-northeast-1、組織の管理アカウント\n"))
	})

	t.Run("管理アカウントの資格情報でなければ、入口を走らせずに errPrecondition", func(t *testing.T) {
		t.Parallel()

		f := emailsWorld(t, "", desiredValue)
		maps.Copy(f.responses, managementWorld("222222222222", "111111111111"))
		require.ErrorIs(t, run(args, &bytes.Buffer{}, testDeps(f, "la@x.y\nsec@x.y\ntabp\n", nil)), errPrecondition)
		assert.Equal(t, []string{"sts get-caller-identity", "organizations describe-organization"}, f.joined())
	})

	t.Run("profile が無ければ aws を起動しない", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, nil)
		d := testDeps(f, "", nil)
		d.getenv = func(string) string { return "" }

		require.ErrorIs(t, run(args, &bytes.Buffer{}, d), errNoProfile)
		assert.Empty(t, f.calls)
	})

	t.Run("静的な資格情報が環境に在れば aws を起動しない", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, nil)
		d := testDeps(f, "", nil)
		d.getenv = func(k string) string {
			if k == "AWS_ACCESS_KEY_ID" || k == "AWS_PROFILE" {
				return "x"
			}

			return ""
		}

		require.ErrorIs(t, run(args, &bytes.Buffer{}, d), errStaticCredentials)
		assert.Empty(t, f.calls)
	})

	t.Run("Terraform を読めなければ aws を起動しない", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, nil)
		d := testDeps(f, "", nil)
		d.readFile = func(string) ([]byte, error) { return nil, os.ErrNotExist }

		err := run(args, &bytes.Buffer{}, d)
		require.ErrorIs(t, err, errHCL)
		require.ErrorIs(t, err, os.ErrNotExist)
		assert.Empty(t, f.calls)
	})

	t.Run("対応表の行が0件なら aws を起動しない", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, nil)
		d := testDeps(f, "", nil)
		d.readFile = readFiles(fixtureFiles(usecaseMainTF, ouMainTF, "locals {\n  member_accounts = {}\n}\n"))

		require.ErrorIs(t, run(args, &bytes.Buffer{}, d), errNoMemberAccounts)
		assert.Empty(t, f.calls)
	})

	t.Run("Terraform を使わない入口はファイルを読まない", func(t *testing.T) {
		t.Parallel()

		f := serviceWorld(t, principals(securityServicePrincipals...), "")

		var read []string

		require.NoError(t, run([]string{"service-access-check", "-project", testProject, "-region", testRegion}, &bytes.Buffer{}, testDeps(f, "", &read)))
		assert.Empty(t, read)
	})

	t.Run("伏せた入力の begin と read を terminal へ渡す", func(t *testing.T) {
		t.Parallel()

		var begins, reads atomic.Int32

		d := testDeps(loginWorld(t, map[string]bool{bg1: true}), "tabp\n", nil)
		lines := hiddenLines(testPassword, testPassword)
		d.hidden = func() ([]byte, error) { reads.Add(1); return lines() }
		d.begin = func() (func(), error) { begins.Add(1); return func() {}, nil }

		require.NoError(t, run([]string{"break-glass-login", "-project", testProject, "-region", testRegion, "-usecase", testUsecase}, &bytes.Buffer{}, d))
		assert.Equal(t, int32(2), begins.Load())
		assert.Equal(t, int32(2), reads.Load())
	})
}

func Test_passwordReader(t *testing.T) {
	t.Parallel()

	t.Run("端末でなければ、状態を控えられず errTerminalState", func(t *testing.T) {
		t.Parallel()

		r, w, err := os.Pipe()
		require.NoError(t, err)
		t.Cleanup(func() { _ = r.Close(); _ = w.Close() })

		begin, _ := passwordReader(r)
		restore, err := begin()
		require.ErrorIs(t, err, errTerminalState)
		assert.Nil(t, restore)
	})
}

func Test_newHiddenReader(t *testing.T) {
	t.Parallel()

	type world struct {
		reads, restores int
	}

	build := func(w *world, getErr error) (func() (func(), error), func() ([]byte, error)) {
		return newHiddenReader(
			func() (func() error, error) {
				if getErr != nil {
					return nil, getErr
				}

				return func() error { w.restores++; return nil }, nil
			},
			func() ([]byte, error) { w.reads++; return []byte("s3cret"), nil },
		)
	}

	t.Run("begin の後の read は読む", func(t *testing.T) {
		t.Parallel()

		var w world

		begin, read := build(&w, nil)
		_, err := begin()
		require.NoError(t, err)

		got, err := read()
		require.NoError(t, err)
		assert.Equal(t, "s3cret", string(got))
		assert.Equal(t, 1, w.reads)
	})

	t.Run("戻した後の read は読まずに errInterrupted で、状態を1回戻す", func(t *testing.T) {
		t.Parallel()

		var w world

		begin, read := build(&w, nil)
		restore, err := begin()
		require.NoError(t, err)

		restore()

		_, err = read()
		require.ErrorIs(t, err, errInterrupted)
		assert.Equal(t, 0, w.reads)
		assert.Equal(t, 1, w.restores)
	})

	t.Run("もう一度 begin すれば、また読む", func(t *testing.T) {
		t.Parallel()

		var w world

		begin, read := build(&w, nil)
		restore, err := begin()
		require.NoError(t, err)
		restore()

		_, err = begin()
		require.NoError(t, err)

		_, err = read()
		require.NoError(t, err)
		assert.Equal(t, 1, w.reads)
	})

	t.Run("状態を読めなければ errTerminalState で、戻す関数を返さない", func(t *testing.T) {
		t.Parallel()

		var w world

		begin, _ := build(&w, os.ErrInvalid)
		restore, err := begin()
		require.ErrorIs(t, err, errTerminalState)
		require.ErrorIs(t, err, os.ErrInvalid)
		assert.Nil(t, restore)
	})
}

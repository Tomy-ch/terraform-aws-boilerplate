package main

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_main(t *testing.T) {
	t.Skip("main は os の標準入出力と終了を execute へ渡すだけで、プロセスを終えるので単体では呼べない")
}

func testDeps(f *fakeAWS, input string) deps {
	return deps{
		aws:    f.fn(),
		getenv: profileEnv,
		readFile: func(string) ([]byte, error) {
			return []byte(testAccounts), nil
		},
		stdin: strings.NewReader(input),
		isTTY: true,
	}
}

func Test_entries(t *testing.T) {
	t.Parallel()

	t.Run("-check の入口は書き込みの操作を持たない", func(t *testing.T) {
		t.Parallel()

		for name, e := range entries {
			if strings.HasSuffix(name, "-check") {
				assert.Empty(t, e.writes, name)
			}
		}
	})

	t.Run("書き込みの一覧は読み取りの操作を含まない", func(t *testing.T) {
		t.Parallel()

		for name, e := range entries {
			for _, w := range e.writes {
				assert.False(t, isRead(strings.Fields(w)), name+": "+w)
			}
		}
	})
}

func Test_execute(t *testing.T) {
	t.Parallel()

	t.Run("12桁の数字を伏せ、エラーを最後の行に出す", func(t *testing.T) {
		t.Parallel()

		f := memberAccountsWorld(t, `{"Id":"222222222222","Name":"tabp-security","State":"SUSPENDED"}`)

		var out bytes.Buffer

		code := execute([]string{"member-accounts-check", "-project", testProject, "-region", testRegion, "-accounts", "a.tf"}, &out, testDeps(f, ""))
		assert.Equal(t, 1, code)
		assert.NotContains(t, out.String(), "222222222222")
		assert.True(t, strings.HasSuffix(out.String(), "❌ "+errNG.Error()+"\n"))
	})

	t.Run("-ids なら伏せない", func(t *testing.T) {
		t.Parallel()

		f := memberAccountsWorld(t, `{"Id":"222222222222","Name":"tabp-security","State":"ACTIVE"},{"Id":"333333333333","Name":"tabp-log-archive","State":"ACTIVE"}`)

		var out bytes.Buffer

		code := execute([]string{"member-accounts-check", "-project", testProject, "-region", testRegion, "-accounts", "a.tf", "-ids"}, &out, testDeps(f, ""))
		assert.Equal(t, 0, code)
		assert.Contains(t, out.String(), "222222222222")
	})

	t.Run("引数の誤りは 2", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer
		assert.Equal(t, 2, execute([]string{"nope"}, &out, testDeps(newFakeAWS(t, nil), "")))
		assert.Contains(t, out.String(), "入口 nope はありません")
	})
}

func Test_exitCode(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		err  error
		want int
	}{
		"nil は 0":      {nil, 0},
		"確認の NG は 1":   {xerrors.Wrap(errNG, "x"), 1},
		"前提が崩れていれば 1":  {xerrors.Wrap(errPrecondition, "x"), 1},
		"確認の入力の不一致は 2": {errNotConfirmed, 2},
		"aws の失敗は 2":   {errAWSFailed, 2},
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

		o, e, err := parseArgs([]string{"member-email-update", "-project", "p", "-region", "r", "-accounts", "a.tf", "-env", "security"})
		require.NoError(t, err)
		assert.Equal(t, options{entry: "member-email-update", project: "p", region: "r", accounts: "a.tf", env: "security"}, o)
		assert.True(t, e.needsEnv)
	})

	bad := map[string][]string{
		"引数が無い":       {},
		"入口が無い":       {"nope", "-project", "p", "-region", "r"},
		"project が無い": {"stacksets-access", "-region", "r"},
		"region が無い":  {"stacksets-access", "-project", "p"},
		"余分な引数がある":    {"stacksets-access", "-project", "p", "-region", "r", "x"},
		"知らない旗":       {"stacksets-access", "-project", "p", "-region", "r", "-x"},
		"env の一覧を使う入口に -accounts が無い":       {"member-emails", "-project", "p", "-region", "r"},
		"env の一覧を使わない入口に -accounts":         {"stacksets-access", "-project", "p", "-region", "r", "-accounts", "a"},
		"member-email-update に -env が無い":    {"member-email-update", "-project", "p", "-region", "r", "-accounts", "a"},
		"ほかの入口に -env":                       {"member-emails", "-project", "p", "-region", "r", "-accounts", "a", "-env", "x"},
		"member-accounts-check でない入口に -ids": {"member-emails-check", "-project", "p", "-region", "r", "-accounts", "a", "-ids"},
	}
	for name, args := range bad {
		t.Run(name+"なら errUsage", func(t *testing.T) {
			t.Parallel()

			_, _, err := parseArgs(args)
			require.ErrorIs(t, err, errUsage)
		})
	}
}

func Test_run(t *testing.T) {
	t.Parallel()

	args := []string{"member-emails", "-project", testProject, "-region", testRegion, "-accounts", "a.tf"}

	t.Run("入口の一覧の書き込みだけを許した client で入口を走らせる", func(t *testing.T) {
		t.Parallel()

		f := emailsWorld(t, "", desiredValue)

		var out bytes.Buffer
		require.NoError(t, run(args, &out, testDeps(f, "la@x.y\nsec@x.y\ntabp\n")))
		assert.Equal(t, []string{"ssm put-parameter"}, f.writes())
	})

	t.Run("profile が無ければ aws を起動しない", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, nil)
		d := testDeps(f, "")
		d.getenv = func(string) string { return "" }

		require.ErrorIs(t, run(args, &bytes.Buffer{}, d), errNoProfile)
		assert.Empty(t, f.calls)
	})

	t.Run("対応表を読めなければ aws を起動しない", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, nil)
		d := testDeps(f, "")
		d.readFile = func(string) ([]byte, error) { return nil, os.ErrNotExist }

		err := run(args, &bytes.Buffer{}, d)
		require.ErrorIs(t, err, errHCL)
		require.ErrorIs(t, err, os.ErrNotExist)
		assert.Empty(t, f.calls)
	})

	t.Run("対応表の行が0件なら aws を起動しない", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, nil)
		d := testDeps(f, "")
		d.readFile = func(string) ([]byte, error) { return []byte("locals {\n  member_accounts = {}\n}\n"), nil }

		require.ErrorIs(t, run(args, &bytes.Buffer{}, d), errNoMemberAccounts)
		assert.Empty(t, f.calls)
	})

	t.Run("対応表を使わない入口はファイルを読まない", func(t *testing.T) {
		t.Parallel()

		f := serviceWorld(t, principals(securityServicePrincipals...), "")
		d := testDeps(f, "")
		d.readFile = func(string) ([]byte, error) {
			t.Error("読まないはずのファイルを読んだ")

			return nil, os.ErrNotExist
		}

		require.NoError(t, run([]string{"service-access-check", "-project", testProject, "-region", testRegion}, &bytes.Buffer{}, d))
	})
}

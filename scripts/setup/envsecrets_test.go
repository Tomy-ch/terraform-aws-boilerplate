package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testRepo     = "acme/infra"
	listPlan     = "secret list --repo " + testRepo + " --env admin-plan --json name,updatedAt"
	listApply    = "secret list --repo " + testRepo + " --env admin-apply --json name,updatedAt"
	setPlan      = "secret set AWS_ACCOUNT_ID --repo " + testRepo + " --env admin-plan"
	setApply     = "secret set AWS_ACCOUNT_ID --repo " + testRepo + " --env admin-apply"
	listSecurity = "secret list --repo " + testRepo + " --env security-plan --json name,updatedAt"
	setSecurity  = "secret set AWS_ACCOUNT_ID --repo " + testRepo + " --env security-plan"
	oldUpdated   = `[{"name":"AWS_ACCOUNT_ID","updatedAt":"2026-01-01T00:00:00Z"},{"name":"OTHER","updatedAt":"2026-01-01T00:00:00Z"}]`
	newUpdated   = `[{"name":"AWS_ACCOUNT_ID","updatedAt":"2026-11-26T00:00:00Z"}]`
)

func ghWorld(t *testing.T) *fakeGH {
	t.Helper()

	return &fakeGH{t: t,
		responses: map[string]awsResult{listPlan: ok(`[]`), listApply: ok(oldUpdated), listSecurity: ok(`[]`), setPlan: ok(""), setApply: ok(""), setSecurity: ok("")},
		after:     map[string]map[string]awsResult{setPlan: {listPlan: ok(newUpdated)}, setApply: {listApply: ok(newUpdated)}, setSecurity: {listSecurity: ok(newUpdated)}},
	}
}

func secretOptions(envs string) options {
	o := bgOptions()
	o.environments = envs
	o.repo = testRepo
	o.getenv = func(string) string { return "" }

	return o
}

func callerWorld(t *testing.T) *fakeAWS {
	t.Helper()

	return newFakeAWS(t, map[string]awsResult{"sts get-caller-identity": ok(callerUser)})
}

func Test_hostGH(t *testing.T) {
	t.Parallel()

	t.Run("標準入力を子へ渡し、出力を返す", func(t *testing.T) {
		t.Parallel()

		r := hostGH("cat")([]byte("v"))
		require.NoError(t, r.err)
		assert.Equal(t, "v", string(r.stdout))
	})

	t.Run("見つからなければ起動の失敗", func(t *testing.T) {
		t.Parallel()

		r := hostGH("no-such-gh-binary")(nil, "secret", "list")
		require.Error(t, r.err)
	})
}

func Test_runGH(t *testing.T) {
	t.Parallel()

	t.Run("一覧の操作は起動する", func(t *testing.T) {
		t.Parallel()

		g := &fakeGH{t: t, responses: map[string]awsResult{"secret list --env e": ok("[]")}}
		out, err := runGH(g.fn(), nil, "secret", "list", "--env", "e")
		require.NoError(t, err)
		assert.Equal(t, "[]", string(out))
	})

	for name, args := range map[string][]string{
		"一覧に無い操作は errNotAllowed":  {"secret", "delete", "X"},
		"引数が足りなければ errNotAllowed": {"secret"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			g := &fakeGH{t: t}
			_, err := runGH(g.fn(), nil, args...)
			require.ErrorIs(t, err, errNotAllowed)
			assert.Empty(t, g.calls)
		})
	}

	t.Run("終了の失敗は errGHFailed", func(t *testing.T) {
		t.Parallel()

		g := &fakeGH{t: t, responses: map[string]awsResult{"secret list": fail(t, "HTTP 404")}}
		_, err := runGH(g.fn(), nil, "secret", "list")
		require.ErrorIs(t, err, errGHFailed)
		assert.Contains(t, err.Error(), "HTTP 404")
	})

	t.Run("起動の失敗は errGHStart", func(t *testing.T) {
		t.Parallel()

		_, err := runGH(hostGH("no-such-gh-binary"), nil, "secret", "list")
		require.ErrorIs(t, err, errGHStart)
	})
}

func Test_secretUpdated(t *testing.T) {
	t.Parallel()

	t.Run("在れば更新日時、無ければ nil", func(t *testing.T) {
		t.Parallel()

		g := ghWorld(t)

		u, err := secretUpdated(g.fn(), testRepo, "admin-apply")
		require.NoError(t, err)
		require.NotNil(t, u)
		assert.Equal(t, 2026, u.Year())

		none, err := secretUpdated(g.fn(), testRepo, "admin-plan")
		require.NoError(t, err)
		assert.Nil(t, none)
	})

	for name, stdout := range map[string]string{
		"配列でなければ errGHResponse":         `{}`,
		"空なら errGHResponse":             ``,
		"updatedAt が無ければ errGHResponse": `[{"name":"AWS_ACCOUNT_ID"}]`,
		"name の無い要素は errGHResponse":     `[{"updatedAt":"2026-01-01T00:00:00Z"}]`,
		"name が空の要素は errGHResponse":     `[{"name":"","updatedAt":"2026-01-01T00:00:00Z"}]`,
		"日時でなければ errGHResponse":         `[{"name":"AWS_ACCOUNT_ID","updatedAt":"x"}]`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			g := &fakeGH{t: t, responses: map[string]awsResult{listPlan: ok(stdout)}}
			_, err := secretUpdated(g.fn(), testRepo, "admin-plan")
			require.ErrorIs(t, err, errGHResponse)
		})
	}
}

func Test_declaredEnvironments(t *testing.T) {
	t.Parallel()

	t.Run("宣言の名前を返す", func(t *testing.T) {
		t.Parallel()

		got, err := declaredEnvironments(readFiles(map[string]string{environmentsFile: environmentsJSON}))
		require.NoError(t, err)
		assert.Equal(t, []string{"admin-plan", "admin-apply", "admin-unlock", "security-plan", "copilot"}, got)
	})

	t.Run("実物の宣言を読め、手順書の5つの environment が在る", func(t *testing.T) {
		t.Parallel()

		got, err := declaredEnvironments(func(n string) ([]byte, error) { return readRepoFile(t, n) })
		require.NoError(t, err)
		assert.Subset(t, got, []string{"admin-plan", "admin-apply-identity", "admin-apply", "admin-unlock", "admin-unlock-identity"})
	})

	for name, src := range map[string]string{
		"0件なら errEnvironment":       `[]`,
		"name が無ければ errEnvironment": `[{"deploy_from":"x"}]`,
		"JSON でなければ errEnvironment": `x`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := declaredEnvironments(readFiles(map[string]string{environmentsFile: src}))
			require.ErrorIs(t, err, errEnvironment)
		})
	}

	t.Run("読めなければ errEnvironment", func(t *testing.T) {
		t.Parallel()

		_, err := declaredEnvironments(readFiles(map[string]string{}))
		require.ErrorIs(t, err, errEnvironment)
	})
}

func Test_targetEnvironments(t *testing.T) {
	t.Parallel()

	t.Run("カンマで分け、前後の空白を落とす", func(t *testing.T) {
		t.Parallel()

		got, err := targetEnvironments(secretOptions("admin-plan, admin-apply"))
		require.NoError(t, err)
		assert.Equal(t, []string{"admin-plan", "admin-apply"}, got)
	})

	for name, tc := range map[string]struct {
		envs string
		want error
	}{
		"宣言に無い名前は errEnvironment": {"admin-plan,typo", errEnvironment},
		"空の名前は errUsage":          {"admin-plan,", errUsage},
		"重複は errUsage":            {"admin-plan,admin-plan", errUsage},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := targetEnvironments(secretOptions(tc.envs))
			require.ErrorIs(t, err, tc.want)
		})
	}
}

func Test_setEnvSecrets(t *testing.T) {
	t.Parallel()

	run := func(t *testing.T, o options, g *fakeGH, input string) (string, error) {
		t.Helper()

		term, out := tty(input)
		err := setEnvSecrets(o, testClient(callerWorld(t)), term, g.fn(), false)

		return out.String(), err
	}

	t.Run("無い secret だけを、値を標準入力で渡し --repo を付けて置き、在るものは書かない", func(t *testing.T) {
		t.Parallel()

		g := ghWorld(t)
		out, err := run(t, secretOptions("admin-plan,admin-apply"), g, "tabp\n")
		require.NoError(t, err)
		assert.Equal(t, []string{listPlan, listApply, setPlan, listPlan}, g.calls)
		assert.Equal(t, "111111111111", g.stdins[2])
		assert.NotContains(t, strings.Join(g.calls, " "), "111111111111")
		assert.NotContains(t, out, "111111111111")
		assert.Contains(t, out, "admin-apply: AWS_ACCOUNT_ID 在る（更新 2026-01-01 00:00:00。値は読めないので確かめていない）。書かない（置き直すなら REPLACE=1）")
		assert.Contains(t, out, "置くもの: リポジトリ acme/infra の admin-plan に AWS_ACCOUNT_ID（アカウント ID の末尾 1111）")
		assert.Contains(t, out, "✅ admin-plan に AWS_ACCOUNT_ID を置きました")
		assert.NotContains(t, out, "✅ admin-apply")
	})

	t.Run("メンバーの environment には、組織の <project>-<env> のアカウントの ID を置く", func(t *testing.T) {
		t.Parallel()

		g := &fakeGH{t: t,
			responses: map[string]awsResult{listSecurity: ok(`[]`), setSecurity: ok("")},
			after:     map[string]map[string]awsResult{setSecurity: {listSecurity: ok(newUpdated)}},
		}
		f := memberWorld(t, `{"Accounts":[{"Id":"111111111111","Name":"tabp-security","State":"ACTIVE"}]}`)
		term, out := tty("tabp\n")
		require.NoError(t, setEnvSecrets(secretOptions("security-plan"), testClient(f), term, g.fn(), true))
		assert.Equal(t, []string{listSecurity, setSecurity, listSecurity}, g.calls)
		assert.Equal(t, "111111111111", g.stdins[1])
		assert.Contains(t, f.ops(), listAccountsCall)
		assert.Contains(t, out.String(), "置くもの: リポジトリ acme/infra の security-plan に AWS_ACCOUNT_ID（アカウント ID の末尾 1111）")
	})

	t.Run("メンバーのアカウントが無ければ、gh を起動せずに errNoMember", func(t *testing.T) {
		t.Parallel()

		g := &fakeGH{t: t}
		f := memberWorld(t, `{"Accounts":[{"Id":"111111111111","Name":"management","State":"ACTIVE"}]}`)
		term, _ := tty("tabp\n")
		require.ErrorIs(t, setEnvSecrets(secretOptions("security-plan"), testClient(f), term, g.fn(), true), errNoMember)
		assert.Empty(t, g.calls)
	})

	t.Run("すべて在れば、確認を求めず書かない", func(t *testing.T) {
		t.Parallel()

		g := ghWorld(t)
		out, err := run(t, secretOptions("admin-apply"), g, "")
		require.NoError(t, err)
		assert.Equal(t, []string{listApply}, g.calls)
		assert.Contains(t, out, "値は読めないので確かめていません")
		assert.Contains(t, out, "書き込みません")
	})

	t.Run("-replace なら在るものも置き直し、更新日時が新しくなったことを確かめる", func(t *testing.T) {
		t.Parallel()

		g := ghWorld(t)
		o := secretOptions("admin-apply")
		o.replace = true
		out, err := run(t, o, g, "tabp\n")
		require.NoError(t, err)
		assert.Equal(t, []string{listApply, setApply, listApply}, g.calls)
		assert.Contains(t, out, "置き直す")
	})

	t.Run("-replace で、無いものは置き、在るものは置き直す", func(t *testing.T) {
		t.Parallel()

		g := ghWorld(t)
		o := secretOptions("admin-plan,admin-apply")
		o.replace = true
		_, err := run(t, o, g, "tabp\n")
		require.NoError(t, err)
		assert.Equal(t, []string{listPlan, listApply, setPlan, listPlan, setApply, listApply}, g.calls)
	})

	t.Run("1秒でも新しくなれば置いたと読む", func(t *testing.T) {
		t.Parallel()

		g := ghWorld(t)
		g.after[setApply] = map[string]awsResult{listApply: ok(`[{"name":"AWS_ACCOUNT_ID","updatedAt":"2026-01-01T00:00:01Z"}]`)}
		o := secretOptions("admin-apply")
		o.replace = true
		_, err := run(t, o, g, "tabp\n")
		require.NoError(t, err)
	})

	t.Run("置いた後も更新日時が同じなら errAWSResponse で確かめ方を案内する", func(t *testing.T) {
		t.Parallel()

		g := ghWorld(t)
		g.after = nil

		var waits int

		o := secretOptions("admin-apply")
		o.replace = true
		o.sleep = func(_ time.Duration) { waits++ }

		out, err := run(t, o, g, "tabp\n")
		require.ErrorIs(t, err, errAWSResponse)
		assert.Contains(t, err.Error(), "gh secret list --repo acme/infra --env admin-apply")
		assert.Equal(t, rereadTries-1, waits)
		assert.NotContains(t, out, "を置きました")
	})

	t.Run("置いた後の読み直しが失敗すれば、やり直さずに errGHFailed", func(t *testing.T) {
		t.Parallel()

		g := ghWorld(t)
		g.after[setPlan] = map[string]awsResult{listPlan: fail(t, "HTTP 500")}
		_, err := run(t, secretOptions("admin-plan"), g, "tabp\n")
		require.ErrorIs(t, err, errGHFailed)
		assert.Equal(t, []string{listPlan, setPlan, listPlan}, g.calls)
	})

	t.Run("2つ目で置くのが失敗すれば、1つ目は置いたまま errGHFailed", func(t *testing.T) {
		t.Parallel()

		g := ghWorld(t)
		g.responses[listApply] = ok(`[]`)
		g.responses[setApply] = fail(t, "HTTP 403")
		out, err := run(t, secretOptions("admin-plan,admin-apply"), g, "tabp\n")
		require.ErrorIs(t, err, errGHFailed)
		assert.Equal(t, []string{listPlan, listApply, setPlan, listPlan, setApply}, g.calls)
		assert.Contains(t, out, "✅ admin-plan に AWS_ACCOUNT_ID を置きました")
		assert.NotContains(t, out, "✅ admin-apply")
	})

	t.Run("確認が project の名前でなければ置かない", func(t *testing.T) {
		t.Parallel()

		g := ghWorld(t)
		_, err := run(t, secretOptions("admin-plan"), g, "x\n")
		require.ErrorIs(t, err, errNotConfirmed)
		assert.Equal(t, []string{listPlan}, g.calls)
	})

	for _, repo := range []string{"my-org/repo.name_1", "a/b"} {
		t.Run("-repo "+repo+" は受ける", func(t *testing.T) {
			t.Parallel()

			g := &fakeGH{t: t, responses: map[string]awsResult{
				"secret list --repo " + repo + " --env admin-plan --json name,updatedAt": ok(newUpdated),
			}}
			o := secretOptions("admin-plan")
			o.repo = repo
			_, err := run(t, o, g, "")
			require.NoError(t, err)
		})
	}

	for name, repo := range map[string]string{
		"-repo が空なら errUsage":         "",
		"-repo が3段なら errUsage":        "a/b/c",
		"-repo の名前が空なら errUsage":      "acme/",
		"-repo が host 付きなら errUsage":  "github.com/acme/infra",
		"-repo が . なら errUsage":       "./..",
		"-repo が - で始まれば errUsage":    "-a/b",
		"-repo に / が無ければ errUsage":    "infra",
		"-repo に空白が在れば errUsage":      "acme/in fra",
		"-repo の owner が空なら errUsage": "/infra",
	} {
		t.Run(name+"で gh を起動しない", func(t *testing.T) {
			t.Parallel()

			g := &fakeGH{t: t}
			o := secretOptions("admin-plan")
			o.repo = repo
			_, err := run(t, o, g, "tabp\n")
			require.ErrorIs(t, err, errUsage)
			assert.Empty(t, g.calls)
		})
	}

	for _, k := range ghRepoEnv {
		t.Run(k+" が在れば、gh を起動せずに errPrecondition", func(t *testing.T) {
			t.Parallel()

			g := &fakeGH{t: t}
			o := secretOptions("admin-plan")
			o.getenv = func(n string) string {
				if n == k {
					return "x"
				}

				return ""
			}

			_, err := run(t, o, g, "tabp\n")
			require.ErrorIs(t, err, errPrecondition)
			assert.Empty(t, g.calls)
		})
	}

	t.Run("宣言に無い environment なら、gh を起動せずに errEnvironment", func(t *testing.T) {
		t.Parallel()

		g := &fakeGH{t: t}
		term, _ := tty("tabp\n")
		require.ErrorIs(t, setEnvSecrets(secretOptions("typo"), testClient(newFakeAWS(t, nil)), term, g.fn(), false), errEnvironment)
		assert.Empty(t, g.calls)
	})

	for name, account := range map[string]string{
		"空":     "",
		"11桁":   "11111111111",
		"13桁":   "1111111111111",
		"英字混じり": "11111111111a",
	} {
		t.Run("アカウント ID が"+name+"なら errAWSResponse で置かない", func(t *testing.T) {
			t.Parallel()

			g := &fakeGH{t: t}
			term, _ := tty("tabp\n")
			f := newFakeAWS(t, map[string]awsResult{"sts get-caller-identity": ok(`{"Account":"` + account + `"}`)})
			require.ErrorIs(t, setEnvSecrets(secretOptions("admin-plan"), testClient(f), term, g.fn(), false), errAWSResponse)
			assert.Empty(t, g.calls)
		})
	}

	t.Run("今の secret を読めなければ errGHFailed で置かない", func(t *testing.T) {
		t.Parallel()

		g := ghWorld(t)
		g.responses[listPlan] = fail(t, "HTTP 401")
		_, err := run(t, secretOptions("admin-plan"), g, "tabp\n")
		require.ErrorIs(t, err, errGHFailed)
		assert.Equal(t, []string{listPlan}, g.calls)
	})
}

func readRepoFile(t *testing.T, name string) ([]byte, error) {
	t.Helper()

	return os.ReadFile(filepath.Join("..", "..", name))
}

func Test_envOf(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		in   string
		want string
		ok   bool
	}{
		"admin の plan": {"admin-plan", "admin", true},
		"ハイフンを含む env と2語の role":          {"log-archive-apply-identity", "log-archive", true},
		"unlock-identity を unlock と読まない": {"security-unlock-identity", "security", true},
		"role の接尾辞が無い":                   {"copilot", "", false},
		"env が空":                         {"-plan", "", false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, ok := envOf(tc.in)
			assert.Equal(t, tc.ok, ok)
			assert.Equal(t, tc.want, got)
		})
	}
}

const listAccountsCall = "organizations list-accounts"

func memberWorld(t *testing.T, accounts string) *fakeAWS {
	t.Helper()

	return newFakeAWS(t, map[string]awsResult{listAccountsCall: ok(accounts)})
}

func Test_accountIDs(t *testing.T) {
	t.Parallel()

	const (
		mgmt     = `{"Id":"111111111111","Name":"management","State":"ACTIVE"}`
		security = `{"Id":"111111111111","Name":"tabp-security","State":"ACTIVE"}`
	)

	t.Run("admin の入口は、組織を読まずに資格情報のアカウントの ID にする", func(t *testing.T) {
		t.Parallel()

		f := callerWorld(t)
		ids, err := accountIDs(secretOptions(""), testClient(f), []string{"admin-plan", "admin-apply"}, false)
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"admin-plan": "111111111111", "admin-apply": "111111111111"}, ids)
		assert.Equal(t, []string{"sts get-caller-identity"}, f.ops())
	})

	t.Run("メンバーの入口は、組織の <project>-<env> のアカウントの ID にする", func(t *testing.T) {
		t.Parallel()

		f := memberWorld(t, `{"Accounts":[`+mgmt+`,`+security+`]}`)
		ids, err := accountIDs(secretOptions(""), testClient(f), []string{"security-plan", "security-apply-identity"}, true)
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"security-plan": "111111111111", "security-apply-identity": "111111111111"}, ids)
		assert.Equal(t, []string{listAccountsCall}, f.ops())
	})

	for name, tc := range map[string]struct {
		envs   []string
		member bool
	}{
		"admin の入口にメンバーの environment":  {[]string{"admin-plan", "security-plan"}, false},
		"メンバーの入口に admin の environment": {[]string{"security-plan", "admin-plan"}, true},
	} {
		t.Run(name+"なら、AWS を読まずに errEntryEnv", func(t *testing.T) {
			t.Parallel()

			f := newFakeAWS(t, nil)
			_, err := accountIDs(secretOptions(""), testClient(f), tc.envs, tc.member)
			require.ErrorIs(t, err, errEntryEnv)
			assert.Empty(t, f.ops())
		})
	}

	t.Run("メンバーのアカウントが組織に無ければ errNoMember", func(t *testing.T) {
		t.Parallel()

		f := memberWorld(t, `{"Accounts":[`+mgmt+`]}`)
		_, err := accountIDs(secretOptions(""), testClient(f), []string{"security-plan"}, true)
		require.ErrorIs(t, err, errNoMember)
	})

	t.Run("メンバーのアカウントが閉鎖済みなら errPrecondition", func(t *testing.T) {
		t.Parallel()

		f := memberWorld(t, `{"Accounts":[`+mgmt+`,{"Id":"111111111111","Name":"tabp-security","State":"CLOSED"}]}`)
		_, err := accountIDs(secretOptions(""), testClient(f), []string{"security-plan"}, true)
		require.ErrorIs(t, err, errPrecondition)
	})

	t.Run("メンバーのアカウントの ID が12桁でなければ errAWSResponse", func(t *testing.T) {
		t.Parallel()

		f := memberWorld(t, `{"Accounts":[`+mgmt+`,{"Id":"x","Name":"tabp-security","State":"ACTIVE"}]}`)
		_, err := accountIDs(secretOptions(""), testClient(f), []string{"security-plan"}, true)
		require.ErrorIs(t, err, errAWSResponse)
	})

	t.Run("<env>-<role> の形でない environment は、AWS を読まずに errNoAccountEnv", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, nil)
		_, err := accountIDs(secretOptions(""), testClient(f), []string{"copilot"}, true)
		require.ErrorIs(t, err, errNoAccountEnv)
		assert.Empty(t, f.ops())
	})
}

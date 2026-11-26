package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	listPlan   = "secret list --env admin-plan --json name,updatedAt"
	listApply  = "secret list --env admin-apply --json name,updatedAt"
	setPlan    = "secret set AWS_ACCOUNT_ID --env admin-plan"
	setApply   = "secret set AWS_ACCOUNT_ID --env admin-apply"
	oldUpdated = `[{"name":"AWS_ACCOUNT_ID","updatedAt":"2026-01-01T00:00:00Z"},{"name":"OTHER","updatedAt":"2026-01-01T00:00:00Z"}]`
	newUpdated = `[{"name":"AWS_ACCOUNT_ID","updatedAt":"2026-11-26T00:00:00Z"}]`
)

// ghWorld は、admin-plan に secret が無く、admin-apply に古い secret が在る世界。置いた後は新しくなる。
func ghWorld(t *testing.T) *fakeGH {
	t.Helper()

	return &fakeGH{t: t,
		responses: map[string]awsResult{listPlan: ok(`[]`), listApply: ok(oldUpdated), setPlan: ok(""), setApply: ok("")},
		after:     map[string]map[string]awsResult{setPlan: {listPlan: ok(newUpdated)}, setApply: {listApply: ok(newUpdated)}},
	}
}

func secretOptions(envs string) options {
	o := bgOptions()
	o.environments = envs
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

	t.Run("起動の失敗は errAWSStart", func(t *testing.T) {
		t.Parallel()

		_, err := runGH(hostGH("no-such-gh-binary"), nil, "secret", "list")
		require.ErrorIs(t, err, errAWSStart)
	})
}

func Test_secretUpdated(t *testing.T) {
	t.Parallel()

	t.Run("在れば更新日時、無ければ nil", func(t *testing.T) {
		t.Parallel()

		g := ghWorld(t)

		u, err := secretUpdated(g.fn(), "admin-apply")
		require.NoError(t, err)
		require.NotNil(t, u)
		assert.Equal(t, 2026, u.Year())

		none, err := secretUpdated(g.fn(), "admin-plan")
		require.NoError(t, err)
		assert.Nil(t, none)
	})

	for name, stdout := range map[string]string{
		"配列でなければ errGHResponse":         `{}`,
		"空なら errGHResponse":             ``,
		"updatedAt が無ければ errGHResponse": `[{"name":"AWS_ACCOUNT_ID"}]`,
		"日時でなければ errGHResponse":         `[{"name":"AWS_ACCOUNT_ID","updatedAt":"x"}]`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			g := &fakeGH{t: t, responses: map[string]awsResult{listPlan: ok(stdout)}}
			_, err := secretUpdated(g.fn(), "admin-plan")
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
		assert.Equal(t, []string{"admin-plan", "admin-apply", "admin-unlock"}, got)
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

	t.Run("値を標準入力で渡して置き、更新日時が新しくなったことを確かめる", func(t *testing.T) {
		t.Parallel()

		g := ghWorld(t)
		term, out := tty("tabp\n")

		require.NoError(t, setEnvSecrets(secretOptions("admin-plan,admin-apply"), testClient(callerWorld(t)), term, g.fn()))
		assert.Equal(t, []string{listPlan, listApply, setPlan, listPlan, setApply, listApply}, g.calls)
		assert.Equal(t, "111111111111", g.stdins[2])
		assert.Equal(t, "111111111111", g.stdins[4])
		assert.NotContains(t, strings.Join(g.calls, " "), "111111111111")
		assert.NotContains(t, out.String(), "111111111111")
		assert.Contains(t, out.String(), "✅ admin-apply に AWS_ACCOUNT_ID を置きました")
	})

	t.Run("確認が project の名前でなければ置かない", func(t *testing.T) {
		t.Parallel()

		g := ghWorld(t)
		term, _ := tty("x\n")
		require.ErrorIs(t, setEnvSecrets(secretOptions("admin-plan"), testClient(callerWorld(t)), term, g.fn()), errNotConfirmed)
		assert.Equal(t, []string{listPlan}, g.calls)
	})

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

			term, _ := tty("tabp\n")
			require.ErrorIs(t, setEnvSecrets(o, testClient(callerWorld(t)), term, g.fn()), errPrecondition)
			assert.Empty(t, g.calls)
		})
	}

	t.Run("宣言に無い environment なら、gh を起動せずに errEnvironment", func(t *testing.T) {
		t.Parallel()

		g := &fakeGH{t: t}
		term, _ := tty("tabp\n")
		require.ErrorIs(t, setEnvSecrets(secretOptions("typo"), testClient(newFakeAWS(t, nil)), term, g.fn()), errEnvironment)
		assert.Empty(t, g.calls)
	})

	t.Run("アカウント ID を読めなければ errAWSResponse で置かない", func(t *testing.T) {
		t.Parallel()

		g := &fakeGH{t: t}
		term, _ := tty("tabp\n")
		f := newFakeAWS(t, map[string]awsResult{"sts get-caller-identity": ok(`{"Account":""}`)})
		require.ErrorIs(t, setEnvSecrets(secretOptions("admin-plan"), testClient(f), term, g.fn()), errAWSResponse)
		assert.Empty(t, g.calls)
	})

	t.Run("置くのが失敗すれば errGHFailed で、後の environment には置かない", func(t *testing.T) {
		t.Parallel()

		g := ghWorld(t)
		g.responses[setPlan] = fail(t, "HTTP 403")
		term, _ := tty("tabp\n")
		require.ErrorIs(t, setEnvSecrets(secretOptions("admin-plan,admin-apply"), testClient(callerWorld(t)), term, g.fn()), errGHFailed)
		assert.NotContains(t, g.calls, setApply)
	})

	t.Run("置いた後も更新日時が古いままなら errAWSResponse で確かめ方を案内する", func(t *testing.T) {
		t.Parallel()

		g := ghWorld(t)
		g.after = nil
		term, out := tty("tabp\n")

		var waits int

		o := secretOptions("admin-apply")
		o.sleep = func(_ time.Duration) { waits++ }

		err := setEnvSecrets(o, testClient(callerWorld(t)), term, g.fn())
		require.ErrorIs(t, err, errAWSResponse)
		assert.Contains(t, err.Error(), "gh secret list --env admin-apply")
		assert.Equal(t, rereadTries-1, waits)
		assert.NotContains(t, out.String(), "を置きました")
	})

	t.Run("今の secret を読めなければ errGHFailed で置かない", func(t *testing.T) {
		t.Parallel()

		g := ghWorld(t)
		g.responses[listPlan] = fail(t, "HTTP 401")
		term, _ := tty("tabp\n")
		require.ErrorIs(t, setEnvSecrets(secretOptions("admin-plan"), testClient(callerWorld(t)), term, g.fn()), errGHFailed)
		assert.Equal(t, []string{listPlan}, g.calls)
	})

	t.Run("出力は伏せる処理を通しても値を含まない", func(t *testing.T) {
		t.Parallel()

		g := ghWorld(t)

		var raw bytes.Buffer

		term := newTerminal(strings.NewReader("tabp\n"), &raw, true)
		require.NoError(t, setEnvSecrets(secretOptions("admin-plan"), testClient(callerWorld(t)), term, g.fn()))
		assert.NotContains(t, raw.String(), "111111111111")
	})
}

func readRepoFile(t *testing.T, name string) ([]byte, error) {
	t.Helper()

	return os.ReadFile(filepath.Join("..", "..", name))
}

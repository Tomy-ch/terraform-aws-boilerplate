package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/cientry"
)

const credsJSON = `{"Credentials":{"AccessKeyId":"KEY-SECRET","SecretAccessKey":"SECRET-SECRET","SessionToken":"SESSION-SECRET","Expiration":"x"}}`

var errFetch = errors.New("fetch failed")

// fakeAssume は、偽の aws を作る。起動ごとに「段 role の末尾 session 名」と、AWS_PROFILE・AWS_REGION の値と、
// 外すべき変数が残っていないかを calls.log へ書く。role の末尾ごとに <role>.rc（終了コード）・<role>.out（stdout）・
// <role>.stderr を置けば、その role の振る舞いになる。無ければ default.* を使う。偽の道具は /bin/sh に読ませて起動する
// （書いたばかりのファイルの exec は ETXTBSY で揺れる）。
func fakeAssume(t *testing.T, knobs map[string]string) (toolFunc, func() []string) {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "calls.log")
	for name, body := range knobs {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600))
	}
	script := filepath.Join(dir, "aws")
	body := `#!/bin/sh
role=$(sed -n 's/.*"RoleArn":"[^"]*-ci-\([a-z-]*\)".*/\1/p' input.json)
session=$(sed -n 's/.*"RoleSessionName":"\([^"]*\)".*/\1/p' input.json)
leak=$(env | grep -E '^(AWS_ACCESS_KEY_ID|AWS_PROFILE|ACTIONS_ID_TOKEN_REQUEST_TOKEN|TF_CLI_ARGS)=' | cut -d= -f1 | tr '\n' ' ')
echo "$1 $role $session region=${AWS_REGION:-} imds=${AWS_EC2_METADATA_DISABLED:-} leak=$leak" >> '` + log + `'
k='` + dir + `'
pick() { if [ -e "$k/$role.$1" ]; then cat "$k/$role.$1"; elif [ -e "$k/default.$1" ]; then cat "$k/default.$1"; fi; }
pick out
pick stderr >&2
rc=$(pick rc)
exit "${rc:-0}"
`
	require.NoError(t, os.WriteFile(script, []byte(body), 0o600))
	tool := func(ctx context.Context, d string, env []string, step string, _ ...string) *exec.Cmd {
		c := exec.CommandContext(ctx, "/bin/sh", script, step)
		c.Dir, c.Env = d, env
		return c
	}
	calls := func() []string {
		raw, err := os.ReadFile(log)
		if os.IsNotExist(err) {
			return nil
		}
		require.NoError(t, err)
		return strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	}
	return tool, calls
}

// fakeFetch は、bearer を控えて body を返す偽の OIDC の要求先を作る。
func fakeFetch(body string, err error) (fetchFunc, *[]string) {
	var seen []string
	return func(url, bearer string) ([]byte, error) {
		seen = append(seen, url+" "+bearer)
		return []byte(body), err
	}, &seen
}

// ciEnviron は CI の job の環境を組む。extra は同じ名前の値を置き換える。偽の道具が sed などを使うので PATH は継ぐ。
func ciEnviron(home string, extra ...string) []string {
	env := []string{
		"ACTIONS_ID_TOKEN_REQUEST_URL=https://t.actions.githubusercontent.com/x", "ACTIONS_ID_TOKEN_REQUEST_TOKEN=bearer",
		"CI_ACCOUNT_ID=" + account, "GITHUB_RUN_ID=8", "GITHUB_RUN_ATTEMPT=1", "HOME=" + home, "PATH=" + os.Getenv("PATH"),
		"AWS_ACCESS_KEY_ID=outer", "AWS_PROFILE=outer", "TF_CLI_ARGS=-x",
	}
	for _, kv := range extra {
		name, _, _ := strings.Cut(kv, "=")
		env = slices.DeleteFunc(env, func(e string) bool { return strings.HasPrefix(e, name+"=") })
		env = append(env, kv)
	}

	return env
}

// ciDeps は、identity root を置いた base と、偽の道具と要求先を持つ deps を返す。
func ciDeps(t *testing.T, tool toolFunc, fetch fetchFunc, extra ...string) (deps, string, string) {
	t.Helper()
	base := localBase(t)
	home := t.TempDir()
	var stderr bytes.Buffer
	return deps{readFile: os.ReadFile, environ: ciEnviron(home, extra...), git: stubGit("", ""), now: time.Now,
		tool: tool, fetch: fetch, stderr: &stderr, root: t.TempDir()}, base, home
}

func oidcBody(sub string) string {
	return `{"value":"` + jwtWith(`{"sub":"`+sub+`"}`) + `"}`
}

func Test_fetchOIDC(t *testing.T) {
	t.Parallel()

	serve := func(t *testing.T, codes ...int) (string, *atomic.Int32) {
		t.Helper()
		var n atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			i := int(n.Add(1)) - 1
			assert.Equal(t, "bearer b", r.Header.Get("Authorization"))
			w.WriteHeader(codes[min(i, len(codes)-1)])
			_, _ = w.Write([]byte(`{"value":"a.b.c"}`))
		}))
		t.Cleanup(srv.Close)
		return srv.URL, &n
	}

	t.Run("bearer を付けて GET し、本文を返す", func(t *testing.T) {
		t.Parallel()
		url, n := serve(t, http.StatusOK)
		got, err := fetchOIDC(url, "b")
		require.NoError(t, err)
		assert.JSONEq(t, `{"value":"a.b.c"}`, string(got))
		assert.Equal(t, int32(1), n.Load())
	})

	t.Run("5xx は試し直し、3回目で通れば本文を返す", func(t *testing.T) {
		t.Parallel()
		url, n := serve(t, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusOK)
		_, err := fetchOIDC(url, "b")
		require.NoError(t, err)
		assert.Equal(t, int32(3), n.Load())
	})

	t.Run("5xx が続けば3回で諦める", func(t *testing.T) {
		t.Parallel()
		url, n := serve(t, http.StatusInternalServerError)
		_, err := fetchOIDC(url, "b")
		require.Error(t, err)
		assert.Equal(t, int32(oidcAttempts), n.Load())
	})

	t.Run("4xx は試し直さずに失敗する", func(t *testing.T) {
		t.Parallel()
		url, n := serve(t, http.StatusForbidden)
		_, err := fetchOIDC(url, "b")
		require.Error(t, err)
		assert.Equal(t, int32(1), n.Load())
	})
}

func Test_fetchOnce(t *testing.T) {
	t.Parallel()

	t.Run("接続できなければ試し直す側の失敗", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.NotFoundHandler())
		url := srv.URL
		srv.Close()
		_, retry, err := fetchOnce(&http.Client{Timeout: time.Second}, url, "b")
		require.Error(t, err)
		assert.True(t, retry)
	})

	t.Run("URL を組めなければ試し直さない", func(t *testing.T) {
		t.Parallel()
		_, retry, err := fetchOnce(&http.Client{}, "://x", "b")
		require.Error(t, err)
		assert.False(t, retry)
	})
}

func Test_envValue(t *testing.T) {
	t.Parallel()

	t.Run("名前の値を返し、無ければ空", func(t *testing.T) {
		t.Parallel()
		env := []string{"A=1", "AB=2", "B="}
		assert.Equal(t, "1", envValue(env, "A"))
		assert.Empty(t, envValue(env, "B"))
		assert.Empty(t, envValue(env, "C"))
	})
}

func Test_ciAssumeEnv(t *testing.T) {
	t.Parallel()

	t.Run("資格情報と token の要求の変数を外し、profile を使わずに region を渡す", func(t *testing.T) {
		t.Parallel()
		got := ciAssumeEnv([]string{"PATH=/bin", "AWS_PROFILE=p", "AWS_ACCESS_KEY_ID=x", "ACTIONS_ID_TOKEN_REQUEST_TOKEN=b", "HOME=/h"}, "ap-northeast-1")
		assert.Equal(t, []string{"PATH=/bin", "HOME=/h", "AWS_EC2_METADATA_DISABLED=true", "AWS_REGION=ap-northeast-1"}, got)
	})
}

func Test_ciOIDC(t *testing.T) {
	t.Parallel()

	t.Run("要求先に audience を付けて token を取り、sub を出す", func(t *testing.T) {
		t.Parallel()
		fetch, seen := fakeFetch(oidcBody(immutable), nil)
		d, _, _ := ciDeps(t, nil, fetch)
		var out bytes.Buffer
		token, err := ciOIDC(d, &out)
		require.NoError(t, err)
		assert.Equal(t, jwtWith(`{"sub":"`+immutable+`"}`), token)
		assert.Equal(t, "OIDC の sub: "+immutable+"\n", out.String())
		assert.Equal(t, []string{"https://t.actions.githubusercontent.com/x?audience=sts.amazonaws.com bearer"}, *seen)
	})

	for name, c := range map[string]struct {
		body  string
		err   error
		extra []string
		want  error
	}{
		"要求先が無ければ要求しない":               {extra: []string{"ACTIONS_ID_TOKEN_REQUEST_URL="}, want: errOIDCRequest},
		"要求が失敗すれば失敗する":                {err: errFetch, want: errOIDCResponse},
		"応答から token を読めなければ失敗する":      {body: "nope", want: errOIDCResponse},
		"sub が environment でなければ失敗する": {body: oidcBody("repo:o/r:ref:refs/heads/main"), want: errOIDCToken},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fetch, seen := fakeFetch(c.body, c.err)
			d, _, _ := ciDeps(t, nil, fetch, c.extra...)
			var out bytes.Buffer
			_, err := ciOIDC(d, &out)
			require.ErrorIs(t, err, c.want)
			assert.NotContains(t, err.Error(), "bearer")
			if errors.Is(c.want, errOIDCRequest) {
				assert.Empty(t, *seen)
			}
		})
	}
}

func Test_ciPrepare(t *testing.T) {
	t.Parallel()

	t.Run("region・アカウント ID・run の番号と token を揃える", func(t *testing.T) {
		t.Parallel()
		fetch, _ := fakeFetch(oidcBody(immutable), nil)
		d, base, _ := ciDeps(t, nil, fetch)
		c, err := ciPrepare(d, &bytes.Buffer{}, "tabp", "admin", base)
		require.NoError(t, err)
		assert.Equal(t, ciTarget{project: "tabp", env: "admin", region: "ap-northeast-1", account: account, run: "8-1",
			token: jwtWith(`{"sub":"` + immutable + `"}`)}, c)
	})

	t.Run("run の番号が無ければ token を要求しない", func(t *testing.T) {
		t.Parallel()
		fetch, seen := fakeFetch(oidcBody(immutable), nil)
		d, base, _ := ciDeps(t, nil, fetch, "GITHUB_RUN_ATTEMPT=")
		_, err := ciPrepare(d, &bytes.Buffer{}, "tabp", "admin", base)
		require.ErrorIs(t, err, errCredRun)
		assert.Empty(t, *seen)
	})

	t.Run("root の region を読めなければ token を要求しない", func(t *testing.T) {
		t.Parallel()
		fetch, seen := fakeFetch(oidcBody(immutable), nil)
		d, _, _ := ciDeps(t, nil, fetch)
		_, err := ciPrepare(d, &bytes.Buffer{}, "tabp", "admin", t.TempDir())
		require.ErrorIs(t, err, errBackendMissing)
		assert.Empty(t, *seen)
	})

	t.Run("形の違う env は失敗する", func(t *testing.T) {
		t.Parallel()
		fetch, _ := fakeFetch(oidcBody(immutable), nil)
		d, base, _ := ciDeps(t, nil, fetch)
		_, err := ciPrepare(d, &bytes.Buffer{}, "tabp", "../x", base)
		require.ErrorIs(t, err, errToken)
	})
}

func Test_ciTarget_assumeInput(t *testing.T) {
	t.Parallel()

	c := ciTarget{project: "tabp", env: "admin", account: account, token: "a.b.c"}

	t.Run("role の ARN と session 名と token を、所有者だけが読める input.json に書く", func(t *testing.T) {
		t.Parallel()
		work := t.TempDir()
		require.NoError(t, c.assumeInput(work, "apply", "gha-8-1"))
		path := filepath.Join(work, "input.json")
		raw, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.JSONEq(t, `{"RoleArn":"arn:aws:iam::123456789012:role/tabp-admin-ci-apply","RoleSessionName":"gha-8-1","WebIdentityToken":"a.b.c"}`, string(raw))
		info, err := os.Stat(path)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	})

	t.Run("登録簿に無い役割とアカウント ID の誤りは書かずに失敗する", func(t *testing.T) {
		t.Parallel()
		work := t.TempDir()
		require.ErrorIs(t, c.assumeInput(work, "admin", "s"), errRole)
		bad := c
		bad.account = "1234"
		require.ErrorIs(t, bad.assumeInput(work, "apply", "s"), errRole)
		assert.NoFileExists(t, filepath.Join(work, "input.json"))
	})
}

func Test_ciWorkUnder(t *testing.T) {
	t.Parallel()

	t.Run("root の tmp/ の下の -work の下に作業ディレクトリを作り、返す関数がその子だけを消す", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		work, cleanup, err := ciWorkUnder(root, "tmp/ci-plan", "assume-")
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(root, "tmp", "ci-plan"), filepath.Dir(work))
		assert.DirExists(t, work)
		cleanup()
		assert.NoDirExists(t, work)
		assert.DirExists(t, filepath.Join(root, "tmp", "ci-plan"))
	})

	for _, parent := range []string{"/abs/ci-plan", "ci-plan", "tmp", "../x"} {
		t.Run(parent+" は errUsage で、何も作らない", func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			_, _, err := ciWorkUnder(root, parent, "assume-")
			require.ErrorIs(t, err, errUsage)
			assert.Equal(t, 2, exitCode(err))
			entries, err := os.ReadDir(root)
			require.NoError(t, err)
			assert.Empty(t, entries)
		})
	}

	t.Run("-work の親を作れなければ失敗する", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, "tmp"), 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(root, "tmp", "f"), nil, 0o600))
		_, _, err := ciWorkUnder(root, "tmp/f/x", "assume-")
		require.Error(t, err)
	})
}

func credArgs(base, work string, extra ...string) []string {
	return append([]string{"-project", "tabp", "-env", "admin", "-base", base, "-work", work}, extra...)
}

func Test_ciCredentials(t *testing.T) {
	t.Parallel()

	t.Run("token で1回だけ assume し、資格情報を伏せさせてから役割の profile を置き、token を消す", func(t *testing.T) {
		t.Parallel()
		tool, calls := fakeAssume(t, map[string]string{"default.out": credsJSON})
		fetch, _ := fakeFetch(oidcBody(immutable), nil)
		d, base, home := ciDeps(t, tool, fetch)
		work := "tmp/ci-plan"
		var out bytes.Buffer
		got, err := ciCredentials(credArgs(base, work, "-role", "apply-identity"), d, &out)
		require.NoError(t, err)
		assert.Equal(t, "✅ tabp-admin-ci-apply-identity の profile に資格情報を置きました", got)
		assert.Equal(t, []string{stepAssumeRole + " apply-identity gha-8-1 region=ap-northeast-1 imds=true leak="}, calls())
		for _, v := range []string{"KEY-SECRET", "SECRET-SECRET", "SESSION-SECRET"} {
			assert.Contains(t, out.String(), "::add-mask::"+v+"\n")
		}
		assert.Contains(t, out.String(), "OIDC の sub: "+immutable)
		assert.NotContains(t, out.String(), jwtWith(`{"sub":"`+immutable+`"}`))

		creds, err := os.ReadFile(filepath.Join(home, ".aws", "credentials"))
		require.NoError(t, err)
		assert.Equal(t, "[tabp-admin-ci-apply-identity]\naws_access_key_id = KEY-SECRET\naws_secret_access_key = SECRET-SECRET\naws_session_token = SESSION-SECRET\n", string(creds))
		config, err := os.ReadFile(filepath.Join(home, ".aws", "config"))
		require.NoError(t, err)
		assert.Equal(t, "[profile tabp-admin-ci-apply-identity]\nregion = ap-northeast-1\n", string(config))
		entries, err := os.ReadDir(filepath.Join(d.root, work))
		require.NoError(t, err)
		assert.Empty(t, entries, "token を含む input.json が残っています")
	})

	for name, c := range map[string]struct {
		knobs map[string]string
		fetch error
		extra []string
		want  error
		tries int
	}{
		"token を取れなければ assume しない":    {fetch: errFetch, want: errOIDCResponse},
		"アカウント ID が不正なら assume しない":   {extra: []string{"CI_ACCOUNT_ID=1234"}, want: errRole},
		"assume できなければ profile を置かない": {knobs: map[string]string{"default.rc": "254", "default.stderr": deniedStderr}, want: errLocalAWS, tries: 1},
		"応答を読めなければ profile を置かない":     {knobs: map[string]string{"default.out": `{"Credentials":{"AccessKeyId":"k"}}`}, want: errCredResponse, tries: 1},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			tool, calls := fakeAssume(t, c.knobs)
			fetch, _ := fakeFetch(oidcBody(immutable), c.fetch)
			d, base, home := ciDeps(t, tool, fetch, c.extra...)
			work := "tmp/ci-plan"
			_, err := ciCredentials(credArgs(base, work, "-role", "plan"), d, &bytes.Buffer{})
			require.ErrorIs(t, err, c.want)
			assert.Len(t, calls(), c.tries)
			assert.NoDirExists(t, filepath.Join(home, ".aws"))
		})
	}

	t.Run("引数の誤りは errUsage", func(t *testing.T) {
		t.Parallel()
		d, base, _ := ciDeps(t, nil, nil)
		for _, args := range [][]string{credArgs(base, "w"), credArgs(base, "w", "-role", "plan", "x"), {"-x"}} {
			_, err := ciCredentials(args, d, &bytes.Buffer{})
			require.ErrorIs(t, err, errUsage)
		}
	})
}

func Test_parseCredentials(t *testing.T) {
	t.Parallel()

	t.Run("応答の3つの値を返す", func(t *testing.T) {
		t.Parallel()
		got, err := parseCredentials([]byte(credsJSON))
		require.NoError(t, err)
		assert.Equal(t, credentials{AccessKeyID: "KEY-SECRET", SecretAccessKey: "SECRET-SECRET", SessionToken: "SESSION-SECRET"}, got)
	})

	for name, body := range map[string]string{
		"JSON でない":      "x",
		"値が欠ける":         `{"Credentials":{"AccessKeyId":"k","SecretAccessKey":"s"}}`,
		"改行を含む":         `{"Credentials":{"AccessKeyId":"k","SecretAccessKey":"s\n[x]","SessionToken":"t"}}`,
		"節の見出しになる文字を含む": `{"Credentials":{"AccessKeyId":"[k]","SecretAccessKey":"s","SessionToken":"t"}}`,
	} {
		t.Run(name+"応答は値を文に入れずに失敗する", func(t *testing.T) {
			t.Parallel()
			_, err := parseCredentials([]byte(body))
			require.ErrorIs(t, err, errCredResponse)
			assert.NotContains(t, err.Error(), "s\n")
		})
	}
}

func Test_writeProfile(t *testing.T) {
	t.Parallel()

	c := credentials{AccessKeyID: "k", SecretAccessKey: "s", SessionToken: "t"}

	t.Run("ほかの節を残して profile の節を置き換え、所有者だけが読める権限で書く", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		dir := filepath.Join(home, ".aws")
		require.NoError(t, os.MkdirAll(dir, 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "credentials"), []byte("[other]\naws_access_key_id = o\n[p]\naws_access_key_id = old\n"), 0o644))
		require.NoError(t, writeProfile(home, "p", c, "us-east-1"))
		raw, err := os.ReadFile(filepath.Join(dir, "credentials"))
		require.NoError(t, err)
		assert.Equal(t, "[other]\naws_access_key_id = o\n[p]\naws_access_key_id = k\naws_secret_access_key = s\naws_session_token = t\n", string(raw))
		for _, name := range []string{"credentials", "config"} {
			info, err := os.Stat(filepath.Join(dir, name))
			require.NoError(t, err)
			assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), name)
		}
	})

	t.Run("HOME が絶対パスでなければ書かない", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, writeProfile("", "p", c, "r"), errCredProfile)
	})

	t.Run(".aws を作れなければ失敗する", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(home, ".aws"), nil, 0o600))
		require.ErrorIs(t, writeProfile(home, "p", c, "r"), errCredProfile)
	})

	t.Run("既存のファイルを読めなければ失敗する", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(home, ".aws", "credentials"), 0o700))
		require.ErrorIs(t, writeProfile(home, "p", c, "r"), errCredProfile)
	})
}

func Test_replaceSection(t *testing.T) {
	t.Parallel()

	t.Run("同じ名前の節だけを除いて末尾に足し、改行の無い最終行を補う", func(t *testing.T) {
		t.Parallel()
		src := "[a]\nx = 1\n[ p ]\ny = 2\n[profile p]\nz = 3"
		got := replaceSection([]byte(src), "p", []string{"k = v"})
		assert.Equal(t, "[a]\nx = 1\n[profile p]\nz = 3\n[p]\nk = v\n", string(got))
	})

	t.Run("空の本文には節だけを書く", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "[p]\nk = v\n", string(replaceSection(nil, "p", []string{"k = v"})))
	})
}

func Test_writeFileAtomic(t *testing.T) {
	t.Parallel()

	t.Run("0600 で書いて置き換え、一時ファイルを残さない", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := filepath.Join(dir, "f")
		require.NoError(t, os.WriteFile(path, []byte("old"), 0o644))
		require.NoError(t, writeFileAtomic(path, []byte("new")))
		raw, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, "new", string(raw))
		info, err := os.Stat(path)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
		entries, err := os.ReadDir(dir)
		require.NoError(t, err)
		assert.Len(t, entries, 1)
	})

	t.Run("ディレクトリが無ければ失敗する", func(t *testing.T) {
		t.Parallel()
		require.Error(t, writeFileAtomic(filepath.Join(t.TempDir(), "none", "f"), nil))
	})
}

func Test_ciAssumeDenied(t *testing.T) {
	t.Parallel()

	denied := func(extra map[string]string) map[string]string {
		k := map[string]string{"default.rc": "254", "default.stderr": deniedStderr, "default.out": credsJSON}
		for name, v := range extra {
			k[name] = v
		}
		return k
	}
	run := func(t *testing.T, knobs map[string]string, extra ...string) (string, []string, string, string, error) {
		t.Helper()
		tool, calls := fakeAssume(t, knobs)
		fetch, _ := fakeFetch(oidcBody(immutable), nil)
		d, base, home := ciDeps(t, tool, fetch, extra...)
		work := "tmp/ci-plan"
		var out bytes.Buffer
		got, err := ciAssumeDenied(credArgs(base, work), d, &out)
		entries, rerr := os.ReadDir(filepath.Join(d.root, work))
		if !os.IsNotExist(rerr) {
			require.NoError(t, rerr)
		}
		assert.Empty(t, entries, "token を含む input.json が残っています")
		assert.NoDirExists(t, filepath.Join(home, ".aws"), "資格情報を置いています")
		for _, v := range []string{"KEY-SECRET", "SECRET-SECRET", "SESSION-SECRET", account} {
			assert.NotContains(t, out.String(), v)
			if err != nil {
				assert.NotContains(t, err.Error(), v)
			}
		}
		return got, calls(), out.String(), d.stderr.(*bytes.Buffer).String(), err
	}

	t.Run("登録簿の拒否されるべき role をすべて試し、すべて AccessDenied なら成功する", func(t *testing.T) {
		t.Parallel()
		got, calls, out, _, err := run(t, denied(nil))
		require.NoError(t, err)
		want := cientry.DeniedRoles()
		require.Len(t, calls, len(want))
		for i, role := range want {
			assert.Equal(t, stepAssumeRole+" "+role+" gha-denied-8-1 region=ap-northeast-1 imds=true leak=", calls[i])
			assert.Contains(t, out, "✅ "+role+" の role の assume は拒否されました（AccessDenied）\n")
		}
		assert.NotContains(t, strings.Join(calls, "\n"), " plan ")
		assert.Equal(t, "✅ 4 個の role の assume がすべて拒否されました", got)
	})

	t.Run("assume が通った role で失敗し、後の role を試さない", func(t *testing.T) {
		t.Parallel()
		second := cientry.DeniedRoles()[1]
		_, calls, _, _, err := run(t, denied(map[string]string{second + ".rc": "0", second + ".stderr": ""}))
		require.ErrorIs(t, err, errAssumeSucceeded)
		assert.Contains(t, err.Error(), second+" の role")
		assert.Len(t, calls, 2)
	})

	t.Run("AccessDenied でない失敗は拒否と認めず、stderr をログへ出さない", func(t *testing.T) {
		t.Parallel()
		stderr := strings.Replace(invalidToken, "Incorrect token audience", "arn:aws:iam::"+account+":role/tabp-admin-ci-apply", 1)
		_, calls, _, logged, err := run(t, denied(map[string]string{"default.stderr": stderr}))
		require.ErrorIs(t, err, errAssumeNotDenied)
		assert.Len(t, calls, 1)
		assert.NotContains(t, logged, "arn:aws")
	})

	t.Run("STS に届かない失敗は、終了コードを添えて拒否と認めない", func(t *testing.T) {
		t.Parallel()
		_, _, _, _, err := run(t, denied(map[string]string{"default.rc": "255", "default.stderr": "Could not connect to the endpoint URL\n"}))
		require.ErrorIs(t, err, errAssumeUnreadable)
		assert.Contains(t, err.Error(), "aws の終了コード 255")
	})

	t.Run("run の番号が無ければ assume しない", func(t *testing.T) {
		t.Parallel()
		_, calls, _, _, err := run(t, denied(nil), "GITHUB_RUN_ID=")
		require.ErrorIs(t, err, errCredRun)
		assert.Empty(t, calls)
	})

	t.Run("アカウント ID が不正なら assume しない", func(t *testing.T) {
		t.Parallel()
		_, calls, _, _, err := run(t, denied(nil), "CI_ACCOUNT_ID=x")
		require.ErrorIs(t, err, errRole)
		assert.Empty(t, calls)
	})

	t.Run("引数の誤りは errUsage", func(t *testing.T) {
		t.Parallel()
		d, base, _ := ciDeps(t, nil, nil)
		for _, args := range [][]string{{"-env", "admin", "-base", base, "-work", "w"}, append(credArgs(base, "w"), "x"), {"-x"}} {
			_, err := ciAssumeDenied(args, d, &bytes.Buffer{})
			require.ErrorIs(t, err, errUsage)
		}
	})
}

func Test_runExitCode(t *testing.T) {
	t.Parallel()

	t.Run("終了コードと stderr を返し、stdout を捨てる", func(t *testing.T) {
		t.Parallel()
		tool, _ := fakeAssume(t, map[string]string{"default.rc": "7", "default.stderr": "e", "default.out": "o"})
		work := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(work, "input.json"), []byte(`{}`), 0o600))
		code, stderr, err := runExitCode(tool, work, nil, stepAssumeRole)
		require.NoError(t, err)
		assert.Equal(t, 7, code)
		assert.Equal(t, "e", string(stderr))
	})

	t.Run("起動できなければ ErrTool で、終了コード 2", func(t *testing.T) {
		t.Parallel()
		tool := func(ctx context.Context, _ string, _ []string, _ string, _ ...string) *exec.Cmd {
			return exec.CommandContext(ctx, filepath.Join(t.TempDir(), "none"))
		}
		_, _, err := runExitCode(tool, t.TempDir(), nil, stepAssumeRole)
		require.ErrorIs(t, err, toolrun.ErrTool)
		assert.Equal(t, 2, exitCode(err))
	})
}

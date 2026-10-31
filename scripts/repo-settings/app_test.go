package main

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const appDeclFixture = `{"app_id": 42, "permissions": {"administration": "write", "metadata": "read"}, "repository_selection": "selected"}`

var testKey = sync.OnceValue(func() *rsa.PrivateKey {
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	return k
})

func pkcs1PEM() string {
	return string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(testKey())}))
}

func fixedNow() time.Time { return time.Unix(1_700_000_000, 0) }

// appRoutes は installation の確認と token の発行と、token が届くリポジトリの一覧を返す GitHub。
func appRoutes(t *testing.T) map[string]fakeResponse {
	t.Helper()
	routes := liveRoutes(t)
	routes["GET /repos/o/r/installation"] = ok(`{"id": 9, "app_id": 42, "permissions": {"administration": "write", "metadata": "read"}, "repository_selection": "selected"}`)
	routes["POST /app/installations/9/access_tokens"] = ok(`{"token": "ghs_inst"}`)
	routes["GET /installation/repositories"] = ok(`{"total_count": 1, "repositories": [{"full_name": "o/r"}]}`)
	return routes
}

func writeAppRepo(t *testing.T, decl string) string {
	t.Helper()
	root := writeRepo(t, syncedRuleset)
	writeFile(t, root, appFile, decl)
	return root
}

func Test_readAppDecl(t *testing.T) {
	t.Parallel()

	t.Run("宣言どおりの App を読む", func(t *testing.T) {
		t.Parallel()
		d, err := readAppDecl(writeAppRepo(t, appDeclFixture))
		require.NoError(t, err)
		assert.Equal(t, appDecl{AppID: 42, Permissions: appPermissions, RepositorySelection: "selected"}, d)
	})

	t.Run("宣言が無ければ失敗する", func(t *testing.T) {
		t.Parallel()
		_, err := readAppDecl(writeRepo(t, syncedRuleset))
		require.Error(t, err)
		require.NotErrorIs(t, err, errAppDecl)
	})

	cases := []struct{ name, decl string }{
		{"未知のキーを拒む", `{"app_id": 42, "permissions": {"administration": "write", "metadata": "read"}, "repository_selection": "selected", "x": 1}`},
		{"値の後の続きを拒む", appDeclFixture + `{}`},
		{"app_id の欠落を拒む", `{"permissions": {"administration": "write", "metadata": "read"}, "repository_selection": "selected"}`},
		{"権限の追加を拒む", `{"app_id": 42, "permissions": {"administration": "write", "metadata": "read", "contents": "write"}, "repository_selection": "selected"}`},
		{"権限の欠落を拒む", `{"app_id": 42, "permissions": {"administration": "write"}, "repository_selection": "selected"}`},
		{"全リポジトリへの install を拒む", `{"app_id": 42, "permissions": {"administration": "write", "metadata": "read"}, "repository_selection": "all"}`},
		{"JSON でないものを拒む", `app_id = 42`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := readAppDecl(writeAppRepo(t, tc.decl))
			require.ErrorIs(t, err, errAppDecl)
		})
	}
}

func Test_parseAppKey(t *testing.T) {
	t.Parallel()

	t.Run("PKCS#1 を読む", func(t *testing.T) {
		t.Parallel()
		k, err := parseAppKey(pkcs1PEM())
		require.NoError(t, err)
		assert.True(t, k.Equal(testKey()))
	})

	t.Run("PKCS#8 を読む", func(t *testing.T) {
		t.Parallel()
		der, err := x509.MarshalPKCS8PrivateKey(testKey())
		require.NoError(t, err)
		k, err := parseAppKey(string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})))
		require.NoError(t, err)
		assert.True(t, k.Equal(testKey()))
	})

	t.Run("PEM でなければ失敗する", func(t *testing.T) {
		t.Parallel()
		_, err := parseAppKey("not a key")
		require.ErrorIs(t, err, errAppKey)
	})

	t.Run("鍵として読めない PEM は失敗する", func(t *testing.T) {
		t.Parallel()
		_, err := parseAppKey(string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("junk")})))
		require.ErrorIs(t, err, errAppKey)
	})

	t.Run("RSA でない鍵は失敗する", func(t *testing.T) {
		t.Parallel()
		ec, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		require.NoError(t, err)
		der, err := x509.MarshalPKCS8PrivateKey(ec)
		require.NoError(t, err)
		_, err = parseAppKey(string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})))
		require.ErrorIs(t, err, errAppKey)
	})
}

func Test_appJWT(t *testing.T) {
	t.Parallel()

	t.Run("RS256 で署名し、iat を60秒前、exp を9分後、iss を App の ID にする", func(t *testing.T) {
		t.Parallel()
		jwt, err := appJWT(testKey(), 42, fixedNow())
		require.NoError(t, err)
		parts := strings.Split(jwt, ".")
		require.Len(t, parts, 3)
		header, err := base64.RawURLEncoding.DecodeString(parts[0])
		require.NoError(t, err)
		assert.JSONEq(t, `{"alg":"RS256","typ":"JWT"}`, string(header))
		claims, err := base64.RawURLEncoding.DecodeString(parts[1])
		require.NoError(t, err)
		assert.JSONEq(t, `{"iat": 1699999940, "exp": 1700000540, "iss": "42"}`, string(claims))
		sig, err := base64.RawURLEncoding.DecodeString(parts[2])
		require.NoError(t, err)
		sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
		assert.NoError(t, rsa.VerifyPKCS1v15(&testKey().PublicKey, crypto.SHA256, sum[:], sig))
	})

	t.Run("署名できない鍵は失敗する", func(t *testing.T) {
		t.Parallel()
		_, err := appJWT(&rsa.PrivateKey{PublicKey: rsa.PublicKey{N: testKey().N, E: 3}}, 42, fixedNow())
		require.ErrorIs(t, err, errAppKey)
	})
}

func Test_appClient(t *testing.T) {
	t.Parallel()
	src := appSource{key: pkcs1PEM(), now: fixedNow}

	t.Run("宣言どおりなら、Administration の write と対象リポジトリだけに絞った token を返す", func(t *testing.T) {
		t.Parallel()
		f, c := newFakeGitHub(t, appRoutes(t))
		got, err := appClient(context.Background(), c, writeAppRepo(t, appDeclFixture), repo, src)
		require.NoError(t, err)
		assert.Equal(t, "ghs_inst", got.token)
		require.Len(t, f.requests, 3)
		assert.True(t, strings.HasPrefix(f.requests[0].auth, "Bearer ey"), "installation は JWT で問う")
		var body map[string]any
		require.NoError(t, json.Unmarshal([]byte(f.requests[1].body), &body))
		assert.Equal(t, map[string]any{"repositories": []any{"r"}, "permissions": map[string]any{"administration": "write"}}, body)
		assert.Equal(t, "Bearer ghs_inst", f.requests[2].auth)
	})

	t.Run("鍵が空なら何も問わずに失敗する", func(t *testing.T) {
		t.Parallel()
		f, c := newFakeGitHub(t, appRoutes(t))
		_, err := appClient(context.Background(), c, writeAppRepo(t, appDeclFixture), repo, appSource{now: fixedNow})
		require.ErrorIs(t, err, errAppKey)
		assert.Empty(t, f.requests)
	})

	t.Run("鍵が読めなければ失敗する", func(t *testing.T) {
		t.Parallel()
		_, c := newFakeGitHub(t, appRoutes(t))
		_, err := appClient(context.Background(), c, writeAppRepo(t, appDeclFixture), repo, appSource{key: "x", now: fixedNow})
		require.ErrorIs(t, err, errAppKey)
	})

	t.Run("宣言が不正なら失敗する", func(t *testing.T) {
		t.Parallel()
		_, c := newFakeGitHub(t, appRoutes(t))
		_, err := appClient(context.Background(), c, writeAppRepo(t, `{}`), repo, src)
		require.ErrorIs(t, err, errAppDecl)
	})

	mismatches := []struct{ name, route, body string }{
		{"App の ID が宣言と違えば書かない", "GET /repos/o/r/installation", `{"id": 9, "app_id": 7, "permissions": {"administration": "write", "metadata": "read"}, "repository_selection": "selected"}`},
		{"権限が広がっていれば書かない", "GET /repos/o/r/installation", `{"id": 9, "app_id": 42, "permissions": {"administration": "write", "metadata": "read", "contents": "write"}, "repository_selection": "selected"}`},
		{"全リポジトリへ install されていれば書かない", "GET /repos/o/r/installation", `{"id": 9, "app_id": 42, "permissions": {"administration": "write", "metadata": "read"}, "repository_selection": "all"}`},
		{"token が他のリポジトリへも届くなら書かない", "GET /installation/repositories", `{"total_count": 2, "repositories": [{"full_name": "o/r"}, {"full_name": "o/x"}]}`},
		{"token が届くリポジトリが別なら書かない", "GET /installation/repositories", `{"total_count": 1, "repositories": [{"full_name": "o/x"}]}`},
		{"total_count が無ければ書かない", "GET /installation/repositories", `{"repositories": [{"full_name": "o/r"}]}`},
	}
	for _, tc := range mismatches {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			routes := appRoutes(t)
			routes[tc.route] = ok(tc.body)
			_, c := newFakeGitHub(t, routes)
			_, err := appClient(context.Background(), c, writeAppRepo(t, appDeclFixture), repo, src)
			require.ErrorIs(t, err, errAppMismatch)
		})
	}

	unexpected := []struct{ name, route, body string }{
		{"installation の id が無ければ失敗する", "GET /repos/o/r/installation", `{"app_id": 42}`},
		{"token が空なら失敗する", "POST /app/installations/9/access_tokens", `{}`},
	}
	for _, tc := range unexpected {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			routes := appRoutes(t)
			routes[tc.route] = ok(tc.body)
			_, c := newFakeGitHub(t, routes)
			_, err := appClient(context.Background(), c, writeAppRepo(t, appDeclFixture), repo, src)
			require.ErrorIs(t, err, errUnexpectedResponse)
		})
	}

	failures := []string{"GET /repos/o/r/installation", "POST /app/installations/9/access_tokens", "GET /installation/repositories"}
	for _, route := range failures {
		t.Run(route+" の失敗をそのまま返す", func(t *testing.T) {
			t.Parallel()
			routes := appRoutes(t)
			routes[route] = fakeResponse{status: http.StatusForbidden, body: `{}`}
			_, c := newFakeGitHub(t, routes)
			_, err := appClient(context.Background(), c, writeAppRepo(t, appDeclFixture), repo, src)
			require.ErrorIs(t, err, errHTTP)
		})
	}
}

func Test_verifySynced(t *testing.T) {
	t.Parallel()

	t.Run("書いた側にずれが無ければ成功する", func(t *testing.T) {
		t.Parallel()
		_, c := newFakeGitHub(t, liveRoutes(t))
		var out bytes.Buffer
		require.NoError(t, verifySynced(context.Background(), writeRepo(t, syncedRuleset), c, c, repo, onlyRuleset, 1, &out))
		assert.Equal(t, "✅ repo-settings: o/r の ruleset は宣言どおりです（送った要求 1 件）\n", out.String())
	})

	t.Run("ruleset の bypass_actors が観測できなければ失敗する", func(t *testing.T) {
		t.Parallel()
		routes := liveRoutes(t)
		var ruleset map[string]any
		require.NoError(t, json.Unmarshal([]byte(routes["GET /repos/o/r/rulesets/7"].body), &ruleset))
		delete(ruleset, "bypass_actors")
		body, err := json.Marshal(ruleset)
		require.NoError(t, err)
		routes["GET /repos/o/r/rulesets/7"] = ok(string(body))
		_, c := newFakeGitHub(t, routes)
		err = verifySynced(context.Background(), writeRepo(t, syncedRuleset), c, c, repo, onlyRuleset, 1, &bytes.Buffer{})
		require.ErrorIs(t, err, errNotApplied)
	})

	t.Run("ruleset が無ければ失敗する", func(t *testing.T) {
		t.Parallel()
		routes := liveRoutes(t)
		routes[rulesetListPath(1)] = ok(`[]`)
		_, c := newFakeGitHub(t, routes)
		err := verifySynced(context.Background(), writeRepo(t, syncedRuleset), c, c, repo, onlyRuleset, 1, &bytes.Buffer{})
		require.ErrorIs(t, err, errNotApplied)
	})

	t.Run("environment にずれが残れば失敗し、宣言に無い environment は数えない", func(t *testing.T) {
		t.Parallel()
		routes := liveRoutes(t)
		routes[envListPath(1)] = ok(`{"total_count": 4, "environments": [
			` + applyEnvsJSON + `
			{"name": "copilot", "protection_rules": [], "can_admins_bypass": true, "deployment_branch_policy": null},
			{"name": "other", "protection_rules": [], "can_admins_bypass": false}]}`)
		_, c := newFakeGitHub(t, routes)
		var out bytes.Buffer
		err := verifySynced(context.Background(), writeRepo(t, syncedRuleset), c, c, repo, onlyEnvironments, 2, &out)
		require.ErrorIs(t, err, errNotApplied)
		assert.NotContains(t, out.String(), "other")
	})

	t.Run("読み直しの失敗をそのまま返す", func(t *testing.T) {
		t.Parallel()
		routes := liveRoutes(t)
		routes[rulesetListPath(1)] = fakeResponse{status: http.StatusInternalServerError, body: `{}`}
		_, c := newFakeGitHub(t, routes)
		err := verifySynced(context.Background(), writeRepo(t, syncedRuleset), c, c, repo, onlyRuleset, 0, &bytes.Buffer{})
		require.ErrorIs(t, err, errHTTP)
	})
}

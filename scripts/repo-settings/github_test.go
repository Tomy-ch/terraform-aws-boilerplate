package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeResponse struct {
	status int
	body   string
}

type recorded struct {
	method      string
	target      string
	body        string
	auth        string
	accept      string
	apiVersion  string
	contentType string
}

// fakeGitHub は "<METHOD> <path?query>" から応答を引く GitHub の代役。無い経路は 404 を返す。
type fakeGitHub struct {
	mu       sync.Mutex
	routes   map[string]fakeResponse
	requests []recorded
}

func newFakeGitHub(t *testing.T, routes map[string]fakeResponse) (*fakeGitHub, githubClient) {
	t.Helper()
	f := &fakeGitHub{routes: routes}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		key := r.Method + " " + r.URL.RequestURI()
		f.mu.Lock()
		f.requests = append(f.requests, recorded{
			method: r.Method, target: r.URL.RequestURI(), body: string(body), auth: r.Header.Get("Authorization"),
			accept: r.Header.Get("Accept"), apiVersion: r.Header.Get("X-GitHub-Api-Version"), contentType: r.Header.Get("Content-Type"),
		})
		resp, ok := f.routes[key]
		f.mu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = fmt.Fprintf(w, `{"message": "no route %s"}`, key)
			return
		}
		w.WriteHeader(resp.status)
		_, _ = io.WriteString(w, resp.body)
	}))
	t.Cleanup(srv.Close)
	return f, githubClient{base: srv.URL, token: "tkn", http: srv.Client()}
}

func (f *fakeGitHub) writes() []recorded {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []recorded
	for _, r := range f.requests {
		if r.method != http.MethodGet {
			out = append(out, r)
		}
	}
	return out
}

func ok(body string) fakeResponse { return fakeResponse{status: http.StatusOK, body: body} }

const repo = "o/r"

func rulesetListPath(page int) string {
	return fmt.Sprintf("GET /repos/o/r/rulesets?includes_parents=false&per_page=100&page=%d", page)
}

func envListPath(page int) string {
	return fmt.Sprintf("GET /repos/o/r/environments?per_page=100&page=%d", page)
}

func policyPath(name string, page int) string {
	return fmt.Sprintf("GET /repos/o/r/environments/%s/deployment-branch-policies?per_page=100&page=%d", name, page)
}

func Test_githubClient_do(t *testing.T) {
	t.Parallel()

	t.Run("2xx の応答を読み、資格情報と body を送る", func(t *testing.T) {
		t.Parallel()
		f, c := newFakeGitHub(t, map[string]fakeResponse{"PUT /x": ok(`{"a": 1}`)})
		var out map[string]any
		require.NoError(t, c.do(context.Background(), http.MethodPut, "/x", map[string]any{"k": "v"}, &out))
		assert.Equal(t, map[string]any{"a": 1.0}, out)
		require.Len(t, f.requests, 1)
		assert.Equal(t, `{"k":"v"}`, f.requests[0].body)
		assert.Equal(t, "Bearer tkn", f.requests[0].auth)
		assert.Equal(t, "application/vnd.github+json", f.requests[0].accept)
		assert.Equal(t, "2022-11-28", f.requests[0].apiVersion)
		assert.Equal(t, "application/json", f.requests[0].contentType)
	})

	t.Run("token と body が無ければ Authorization と Content-Type を送らない", func(t *testing.T) {
		t.Parallel()
		f, c := newFakeGitHub(t, map[string]fakeResponse{"GET /x": ok(`{}`)})
		c.token = ""
		require.NoError(t, c.do(context.Background(), http.MethodGet, "/x", nil, nil))
		require.Len(t, f.requests, 1)
		assert.Empty(t, f.requests[0].auth)
		assert.Empty(t, f.requests[0].contentType)
	})

	t.Run("out が nil なら 2xx の本文を解釈しない", func(t *testing.T) {
		t.Parallel()
		_, c := newFakeGitHub(t, map[string]fakeResponse{"DELETE /x": {status: http.StatusNoContent}})
		require.NoError(t, c.do(context.Background(), http.MethodDelete, "/x", nil, nil))
	})

	t.Run("2xx 以外は応答の抜粋つきで失敗し、抜粋は300文字を超えると切る", func(t *testing.T) {
		t.Parallel()
		_, c := newFakeGitHub(t, map[string]fakeResponse{"GET /x": {status: http.StatusForbidden, body: strings.Repeat("e", 301)}})
		err := c.do(context.Background(), http.MethodGet, "/x", nil, nil)
		require.ErrorIs(t, err, errHTTP)
		assert.Contains(t, err.Error(), "GET /x: 403 "+strings.Repeat("e", 300)+"...")
	})

	t.Run("抜粋がちょうど300文字なら切らない", func(t *testing.T) {
		t.Parallel()
		_, c := newFakeGitHub(t, map[string]fakeResponse{"GET /x": {status: http.StatusForbidden, body: strings.Repeat("e", 300)}})
		err := c.do(context.Background(), http.MethodGet, "/x", nil, nil)
		require.ErrorIs(t, err, errHTTP)
		assert.NotContains(t, err.Error(), "...")
	})

	t.Run("5xx も失敗する", func(t *testing.T) {
		t.Parallel()
		_, c := newFakeGitHub(t, map[string]fakeResponse{"GET /x": {status: http.StatusBadGateway, body: "bad"}})
		require.ErrorIs(t, c.do(context.Background(), http.MethodGet, "/x", nil, nil), errHTTP)
	})

	t.Run("JSON でない応答は失敗する", func(t *testing.T) {
		t.Parallel()
		_, c := newFakeGitHub(t, map[string]fakeResponse{"GET /x": ok(`<html>`)})
		var out map[string]any
		require.ErrorIs(t, c.do(context.Background(), http.MethodGet, "/x", nil, &out), errUnexpectedResponse)
	})

	t.Run("body を JSON にできなければ送らずに失敗する", func(t *testing.T) {
		t.Parallel()
		f, c := newFakeGitHub(t, map[string]fakeResponse{})
		require.ErrorIs(t, c.do(context.Background(), http.MethodPost, "/x", map[string]any{"f": func() {}}, nil), errRequest)
		assert.Empty(t, f.requests)
	})

	t.Run("URL を組めなければ失敗する", func(t *testing.T) {
		t.Parallel()
		c := githubClient{base: "://bad", http: http.DefaultClient}
		require.ErrorIs(t, c.do(context.Background(), http.MethodGet, "/x", nil, nil), errRequest)
	})

	t.Run("接続できなければ失敗する", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.NotFoundHandler())
		srv.Close()
		c := githubClient{base: srv.URL, http: http.DefaultClient}
		require.ErrorIs(t, c.do(context.Background(), http.MethodGet, "/x", nil, nil), errRequest)
	})

	t.Run("応答の本文を読み切れなければ失敗する", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Length", "100")
			_, _ = io.WriteString(w, "short")
		}))
		t.Cleanup(srv.Close)
		c := githubClient{base: srv.URL, http: srv.Client()}
		require.ErrorIs(t, c.do(context.Background(), http.MethodGet, "/x", nil, nil), errRequest)
	})
}

func Test_pagedPath(t *testing.T) {
	t.Parallel()

	t.Run("クエリの無い path には ? で足す", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "/a?per_page=100&page=2", pagedPath("/a", 2))
	})

	t.Run("クエリの在る path には & で足す", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "/a?x=1&per_page=100&page=1", pagedPath("/a?x=1", 1))
	})
}

func Test_fetchLive(t *testing.T) {
	t.Parallel()

	t.Run("ruleset と environment をまとめて読む", func(t *testing.T) {
		t.Parallel()
		f, c := newFakeGitHub(t, map[string]fakeResponse{
			rulesetListPath(1):          ok(`[{"id": 7, "name": "bp", "source_type": "Repository"}]`),
			"GET /repos/o/r/rulesets/7": ok(`{"name": "bp"}`),
			envListPath(1):              ok(`{"total_count": 1, "environments": [{"name": "copilot", "protection_rules": [], "can_admins_bypass": true, "deployment_branch_policy": null}]}`),
		})
		got, err := fetchLive(context.Background(), c, repo, "bp")
		require.NoError(t, err)
		assert.Equal(t, int64(7), got.RulesetID)
		assert.Equal(t, map[string]any{"name": "bp"}, got.Ruleset)
		require.Contains(t, got.Envs, "copilot")
		assert.Empty(t, f.writes())
	})

	t.Run("ruleset を読めなければ失敗する", func(t *testing.T) {
		t.Parallel()
		_, c := newFakeGitHub(t, map[string]fakeResponse{})
		_, err := fetchLive(context.Background(), c, repo, "bp")
		require.ErrorIs(t, err, errHTTP)
	})

	t.Run("environment を読めなければ失敗する", func(t *testing.T) {
		t.Parallel()
		_, c := newFakeGitHub(t, map[string]fakeResponse{rulesetListPath(1): ok(`[]`)})
		_, err := fetchLive(context.Background(), c, repo, "bp")
		require.ErrorIs(t, err, errHTTP)
		assert.Contains(t, err.Error(), "GET /repos/o/r/environments")
	})
}

func Test_fetchRuleset(t *testing.T) {
	t.Parallel()

	full := make([]string, 0, perPage)
	for i := range perPage {
		full = append(full, fmt.Sprintf(`{"id": %d, "name": "other%d", "source_type": "Repository"}`, i+100, i))
	}

	t.Run("2ページ目まで読んで同名の ruleset を見つける", func(t *testing.T) {
		t.Parallel()
		_, c := newFakeGitHub(t, map[string]fakeResponse{
			rulesetListPath(1):          ok("[" + strings.Join(full, ",") + "]"),
			rulesetListPath(2):          ok(`[{"id": 7, "name": "bp", "source_type": "Repository"}]`),
			"GET /repos/o/r/rulesets/7": ok(`{"name": "bp", "rules": []}`),
		})
		got, id, err := fetchRuleset(context.Background(), c, repo, "bp")
		require.NoError(t, err)
		assert.Equal(t, int64(7), id)
		assert.Equal(t, "bp", got["name"])
	})

	t.Run("ちょうど100件のページの次が空なら、そこで終える", func(t *testing.T) {
		t.Parallel()
		f, c := newFakeGitHub(t, map[string]fakeResponse{
			rulesetListPath(1): ok("[" + strings.Join(full, ",") + "]"),
			rulesetListPath(2): ok(`[]`),
		})
		got, _, err := fetchRuleset(context.Background(), c, repo, "bp")
		require.NoError(t, err)
		assert.Nil(t, got)
		assert.Len(t, f.requests, 2)
	})

	t.Run("99件のページで終え、次のページを問わない", func(t *testing.T) {
		t.Parallel()
		f, c := newFakeGitHub(t, map[string]fakeResponse{
			rulesetListPath(1): ok("[" + strings.Join(full[:perPage-1], ",") + "]"),
		})
		_, _, err := fetchRuleset(context.Background(), c, repo, "bp")
		require.NoError(t, err)
		assert.Len(t, f.requests, 1)
	})

	t.Run("ページをまたいで同名が2件なら失敗する", func(t *testing.T) {
		t.Parallel()
		page1 := append(append([]string{}, full[:perPage-1]...), `{"id": 7, "name": "bp", "source_type": "Repository"}`)
		_, c := newFakeGitHub(t, map[string]fakeResponse{
			rulesetListPath(1): ok("[" + strings.Join(page1, ",") + "]"),
			rulesetListPath(2): ok(`[{"id": 8, "name": "bp", "source_type": "Repository"}]`),
		})
		_, _, err := fetchRuleset(context.Background(), c, repo, "bp")
		require.ErrorIs(t, err, errAmbiguousRuleset)
	})

	t.Run("本体が null なら、無いとは扱わずに失敗する", func(t *testing.T) {
		t.Parallel()
		_, c := newFakeGitHub(t, map[string]fakeResponse{
			rulesetListPath(1):          ok(`[{"id": 7, "name": "bp", "source_type": "Repository"}]`),
			"GET /repos/o/r/rulesets/7": ok(`null`),
		})
		_, _, err := fetchRuleset(context.Background(), c, repo, "bp")
		require.ErrorIs(t, err, errUnexpectedResponse)
	})

	t.Run("組織から継いだ同名の ruleset は数えない", func(t *testing.T) {
		t.Parallel()
		_, c := newFakeGitHub(t, map[string]fakeResponse{
			rulesetListPath(1): ok(`[{"id": 7, "name": "bp", "source_type": "Organization"}]`),
		})
		got, id, err := fetchRuleset(context.Background(), c, repo, "bp")
		require.NoError(t, err)
		assert.Nil(t, got)
		assert.Zero(t, id)
	})

	t.Run("同名が2件なら失敗する", func(t *testing.T) {
		t.Parallel()
		_, c := newFakeGitHub(t, map[string]fakeResponse{
			rulesetListPath(1): ok(`[{"id": 7, "name": "bp", "source_type": "Repository"}, {"id": 8, "name": "bp", "source_type": "Repository"}]`),
		})
		_, _, err := fetchRuleset(context.Background(), c, repo, "bp")
		require.ErrorIs(t, err, errAmbiguousRuleset)
	})

	t.Run("一覧を読めなければ失敗する", func(t *testing.T) {
		t.Parallel()
		_, c := newFakeGitHub(t, map[string]fakeResponse{rulesetListPath(1): {status: http.StatusInternalServerError}})
		_, _, err := fetchRuleset(context.Background(), c, repo, "bp")
		require.ErrorIs(t, err, errHTTP)
	})

	t.Run("本体を読めなければ失敗する", func(t *testing.T) {
		t.Parallel()
		_, c := newFakeGitHub(t, map[string]fakeResponse{
			rulesetListPath(1): ok(`[{"id": 7, "name": "bp", "source_type": "Repository"}]`),
		})
		_, _, err := fetchRuleset(context.Background(), c, repo, "bp")
		require.ErrorIs(t, err, errHTTP)
	})
}

func Test_fetchEnvironments(t *testing.T) {
	t.Parallel()

	t.Run("保護の規則と custom のパターンを読む", func(t *testing.T) {
		t.Parallel()
		_, c := newFakeGitHub(t, map[string]fakeResponse{
			envListPath(1): ok(`{"total_count": 2, "environments": [{"name": "a-plan", "can_admins_bypass": false,
				"protection_rules": [{"type": "required_reviewers", "reviewers": [{}, {}]}, {"type": "wait_timer", "wait_timer": 3}, {"type": "branch_policy"}],
				"deployment_branch_policy": {"protected_branches": false, "custom_branch_policies": true}}]}`),
			envListPath(2):          ok(`{"total_count": 2, "environments": [{"name": "copilot", "protection_rules": [], "deployment_branch_policy": {"protected_branches": true, "custom_branch_policies": false}}]}`),
			policyPath("a-plan", 1): ok(`{"total_count": 1, "branch_policies": [{"id": 5, "name": "main", "type": "branch"}]}`),
		})
		got, err := fetchEnvironments(context.Background(), c, repo)
		require.NoError(t, err)
		assert.Equal(t, liveEnv{
			CanAdminsBypass: boolPtr(false), Reviewers: 2, WaitTimer: 3,
			Policy: &customOnly, Patterns: map[string]int64{"main": 5},
		}, got["a-plan"])
		// custom でない environment には一覧を問わない。
		assert.Equal(t, liveEnv{Policy: &branchPolicy{ProtectedBranches: true}}, got["copilot"])
	})

	t.Run("environment が0件なら空を返す", func(t *testing.T) {
		t.Parallel()
		_, c := newFakeGitHub(t, map[string]fakeResponse{envListPath(1): ok(`{"total_count": 0, "environments": []}`)})
		got, err := fetchEnvironments(context.Background(), c, repo)
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("総数に届く前にページが尽きれば失敗する", func(t *testing.T) {
		t.Parallel()
		_, c := newFakeGitHub(t, map[string]fakeResponse{
			envListPath(1): ok(`{"total_count": 3, "environments": [{"name": "a"}]}`),
			envListPath(2): ok(`{"total_count": 3, "environments": []}`),
		})
		_, err := fetchEnvironments(context.Background(), c, repo)
		require.ErrorIs(t, err, errShortPage)
	})

	t.Run("総数ちょうどで終え、次のページを問わない", func(t *testing.T) {
		t.Parallel()
		f, c := newFakeGitHub(t, map[string]fakeResponse{
			envListPath(1): ok(`{"total_count": 1, "environments": [{"name": "a", "protection_rules": []}]}`),
		})
		got, err := fetchEnvironments(context.Background(), c, repo)
		require.NoError(t, err)
		assert.Len(t, got, 1)
		assert.Len(t, f.requests, 1)
	})

	t.Run("protection_rules が応答に無ければ、観測していないと記録する", func(t *testing.T) {
		t.Parallel()
		_, c := newFakeGitHub(t, map[string]fakeResponse{
			envListPath(1): ok(`{"total_count": 1, "environments": [{"name": "a", "can_admins_bypass": false}]}`),
		})
		got, err := fetchEnvironments(context.Background(), c, repo)
		require.NoError(t, err)
		assert.True(t, got["a"].RulesMissing)
	})

	cases := []struct {
		name string
		body string
	}{
		// total_count が無いまま要素だけを読むと、1ページ目で打ち切って残りを黙って落とす。
		{"total_count が無ければ失敗する", `{"environments": [{"name": "a", "protection_rules": []}]}`},
		{"total_count を超えて要素が返れば失敗する", `{"total_count": 0, "environments": [{"name": "a", "protection_rules": []}]}`},
		{"同名の environment が2回現れれば失敗する", `{"total_count": 2, "environments": [{"name": "a", "protection_rules": []}, {"name": "a", "protection_rules": []}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, c := newFakeGitHub(t, map[string]fakeResponse{envListPath(1): ok(tc.body)})
			_, err := fetchEnvironments(context.Background(), c, repo)
			require.ErrorIs(t, err, errUnexpectedResponse)
		})
	}

	t.Run("2ページ目を読めなければ失敗する", func(t *testing.T) {
		t.Parallel()
		_, c := newFakeGitHub(t, map[string]fakeResponse{
			envListPath(1): ok(`{"total_count": 2, "environments": [{"name": "a", "protection_rules": []}]}`),
		})
		_, err := fetchEnvironments(context.Background(), c, repo)
		require.ErrorIs(t, err, errHTTP)
	})

	t.Run("一覧を読めなければ失敗する", func(t *testing.T) {
		t.Parallel()
		_, c := newFakeGitHub(t, map[string]fakeResponse{})
		_, err := fetchEnvironments(context.Background(), c, repo)
		require.ErrorIs(t, err, errHTTP)
	})

	t.Run("パターンを読めなければ失敗する", func(t *testing.T) {
		t.Parallel()
		_, c := newFakeGitHub(t, map[string]fakeResponse{
			envListPath(1): ok(`{"total_count": 1, "environments": [{"name": "a", "deployment_branch_policy": {"protected_branches": false, "custom_branch_policies": true}}]}`),
		})
		_, err := fetchEnvironments(context.Background(), c, repo)
		require.ErrorIs(t, err, errHTTP)
	})
}

func Test_fetchBranchPolicies(t *testing.T) {
	t.Parallel()

	t.Run("2ページを読み、branch 以外の type は名前に前置する", func(t *testing.T) {
		t.Parallel()
		_, c := newFakeGitHub(t, map[string]fakeResponse{
			policyPath("a%20b", 1): ok(`{"total_count": 2, "branch_policies": [{"id": 1, "name": "main", "type": "branch"}]}`),
			policyPath("a%20b", 2): ok(`{"total_count": 2, "branch_policies": [{"id": 2, "name": "v*", "type": "tag"}]}`),
		})
		got, err := fetchBranchPolicies(context.Background(), c, repo, "a b")
		require.NoError(t, err)
		assert.Equal(t, map[string]int64{"main": 1, "tag:v*": 2}, got)
	})

	t.Run("0件なら空を返す", func(t *testing.T) {
		t.Parallel()
		_, c := newFakeGitHub(t, map[string]fakeResponse{policyPath("a", 1): ok(`{"total_count": 0, "branch_policies": []}`)})
		got, err := fetchBranchPolicies(context.Background(), c, repo, "a")
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("総数に届く前にページが尽きれば失敗する", func(t *testing.T) {
		t.Parallel()
		_, c := newFakeGitHub(t, map[string]fakeResponse{
			policyPath("a", 1): ok(`{"total_count": 5, "branch_policies": [{"id": 1, "name": "main", "type": "branch"}]}`),
			policyPath("a", 2): ok(`{"total_count": 5, "branch_policies": []}`),
		})
		_, err := fetchBranchPolicies(context.Background(), c, repo, "a")
		require.ErrorIs(t, err, errShortPage)
	})

	cases := []struct {
		name string
		body string
	}{
		{"total_count が無ければ失敗する", `{"branch_policies": [{"id": 1, "name": "main", "type": "branch"}]}`},
		{"total_count を超えて要素が返れば失敗する", `{"total_count": 0, "branch_policies": [{"id": 1, "name": "main", "type": "branch"}]}`},
		{"同名のパターンが2回現れれば失敗する", `{"total_count": 2, "branch_policies": [{"id": 1, "name": "main", "type": "branch"}, {"id": 2, "name": "main", "type": "branch"}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, c := newFakeGitHub(t, map[string]fakeResponse{policyPath("a", 1): ok(tc.body)})
			_, err := fetchBranchPolicies(context.Background(), c, repo, "a")
			require.ErrorIs(t, err, errUnexpectedResponse)
		})
	}

	t.Run("読めなければ失敗する", func(t *testing.T) {
		t.Parallel()
		_, c := newFakeGitHub(t, map[string]fakeResponse{})
		_, err := fetchBranchPolicies(context.Background(), c, repo, "a")
		require.ErrorIs(t, err, errHTTP)
	})
}

func Test_pageDone(t *testing.T) {
	t.Parallel()

	total := func(n int) *int { return &n }
	cases := []struct {
		name      string
		collected int
		onPage    int
		total     *int
		wantDone  bool
		wantErr   error
	}{
		{"総数ちょうどで終える", 2, 1, total(2), true, nil},
		{"総数の1件手前なら続ける", 1, 1, total(2), false, nil},
		{"総数を1件超えれば失敗する", 3, 1, total(2), false, errUnexpectedResponse},
		{"総数が無ければ失敗する", 1, 1, nil, false, errUnexpectedResponse},
		{"総数に届く前にページが空なら失敗する", 1, 0, total(2), false, errShortPage},
		{"総数0で何も無ければ終える", 0, 0, total(0), true, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			done, err := pageDone(tc.collected, tc.onPage, tc.total)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantDone, done)
		})
	}
}

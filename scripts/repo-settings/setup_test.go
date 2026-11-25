package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const labelsDecl = `[{"name": "bug", "description": "不具合", "color": "d73a4a"}, {"name": "dependencies", "color": "0366d6"}]`

func labelsRoot(t *testing.T, body string) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, root, labelsFile, body)
	return root
}

func labelListPath(page int) string { return pagedPath("/repos/"+repo+"/labels", page) }

func workflowListPath(page int) string { return pagedPath("/repos/"+repo+"/actions/workflows", page) }

func labelPage(prefix string, n int) string {
	items := make([]string, n)
	for i := range items {
		items[i] = fmt.Sprintf(`{"name": "%s-%d"}`, prefix, i)
	}
	return "[" + strings.Join(items, ",") + "]"
}

func noContent() fakeResponse { return fakeResponse{status: http.StatusNoContent} }

func targets(rs []recorded) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.method + " " + r.target
	}
	return out
}

func requestCount(f *fakeGitHub) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

func Test_readLabels(t *testing.T) {
	t.Parallel()

	t.Run("宣言を順に読み、description は省ける", func(t *testing.T) {
		t.Parallel()
		got, err := readLabels(labelsRoot(t, labelsDecl))
		require.NoError(t, err)
		assert.Equal(t, []labelDecl{{Name: "bug", Description: "不具合", Color: "d73a4a"}, {Name: "dependencies", Color: "0366d6"}}, got)
	})

	t.Run("ファイルが無ければ失敗する", func(t *testing.T) {
		t.Parallel()
		_, err := readLabels(t.TempDir())
		require.ErrorIs(t, err, os.ErrNotExist)
	})
}

func Test_parseLabels(t *testing.T) {
	t.Parallel()

	t.Run("color は大文字の16進も通す", func(t *testing.T) {
		t.Parallel()
		got, err := parseLabels([]byte(`[{"name": "a", "color": "FFFFFF"}]`))
		require.NoError(t, err)
		assert.Equal(t, "FFFFFF", got[0].Color)
	})

	for name, c := range map[string]struct{ body, message string }{
		"空の配列":          {`[]`, "1件もありません"},
		"要素が null":      {`[null]`, "name がありません"},
		"name が無い":      {`[{"color": "ffffff"}]`, "name がありません"},
		"name が空白だけ":    {`[{"name": " ", "color": "ffffff"}]`, "name がありません"},
		"color が無い":     {`[{"name": "a"}]`, "6桁の16進ではありません"},
		"color が # 付き":  {`[{"name": "a", "color": "#ffffff"}]`, "6桁の16進ではありません"},
		"color が5桁":     {`[{"name": "a", "color": "fffff"}]`, "6桁の16進ではありません"},
		"color が7桁":     {`[{"name": "a", "color": "fffffff"}]`, "6桁の16進ではありません"},
		"color が16進でない": {`[{"name": "a", "color": "gggggg"}]`, "6桁の16進ではありません"},
		"大文字小文字だけが違う重複": {`[{"name": "Bug", "color": "ffffff"}, {"name": "bug", "color": "000000"}]`, "2回あります"},
	} {
		t.Run(name+"は errLabelsDecl", func(t *testing.T) {
			t.Parallel()
			_, err := parseLabels([]byte(c.body))
			assertOnlySentinel(t, err, errLabelsDecl)
			assert.Contains(t, err.Error(), c.message)
		})
	}

	for name, body := range map[string]string{
		"null":                `null`,
		"配列でない":               `{"name": "a"}`,
		"途中で切れた JSON":         `[{"name": "a", "co`,
		"空":                   ``,
		"description が文字列でない": `[{"name": "a", "color": "ffffff", "description": 123}]`,
	} {
		t.Run(name+"は errLabelsParse", func(t *testing.T) {
			t.Parallel()
			_, err := parseLabels([]byte(body))
			assertOnlySentinel(t, err, errLabelsParse)
		})
	}
}

func Test_listLabels(t *testing.T) {
	t.Parallel()

	t.Run("per_page に満たないページで尽きたとみなし、全ページの名前を返す", func(t *testing.T) {
		t.Parallel()
		_, c := newFakeGitHub(t, map[string]fakeResponse{
			"GET " + labelListPath(1): ok(labelPage("a", perPage)),
			"GET " + labelListPath(2): ok(labelPage("b", 1)),
		})
		got, err := listLabels(context.Background(), c, repo)
		require.NoError(t, err)
		assert.Len(t, got, perPage+1)
		assert.Equal(t, "b-0", got[perPage])
	})

	t.Run("ちょうど per_page 件なら次のページも読み、空のページで尽きる", func(t *testing.T) {
		t.Parallel()
		_, c := newFakeGitHub(t, map[string]fakeResponse{
			"GET " + labelListPath(1): ok(labelPage("a", perPage)),
			"GET " + labelListPath(2): ok(`[]`),
		})
		got, err := listLabels(context.Background(), c, repo)
		require.NoError(t, err)
		assert.Len(t, got, perPage)
	})

	t.Run("0件なら空", func(t *testing.T) {
		t.Parallel()
		_, c := newFakeGitHub(t, map[string]fakeResponse{"GET " + labelListPath(1): ok(`[]`)})
		got, err := listLabels(context.Background(), c, repo)
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	full := func(last string) map[string]fakeResponse {
		routes := map[string]fakeResponse{}
		for p := 1; p < maxLabelPages; p++ {
			routes["GET "+labelListPath(p)] = ok(labelPage(fmt.Sprintf("p%d", p), perPage))
		}
		routes["GET "+labelListPath(maxLabelPages)] = ok(last)
		return routes
	}

	t.Run("上限のページが per_page に満たなければ成功する", func(t *testing.T) {
		t.Parallel()
		_, c := newFakeGitHub(t, full(labelPage("last", 1)))
		got, err := listLabels(context.Background(), c, repo)
		require.NoError(t, err)
		assert.Len(t, got, (maxLabelPages-1)*perPage+1)
	})

	t.Run("上限のページまで尽きなければ、上限の回数だけ読んで errLabelPages", func(t *testing.T) {
		t.Parallel()
		f, c := newFakeGitHub(t, full(labelPage("last", perPage)))
		_, err := listLabels(context.Background(), c, repo)
		require.ErrorIs(t, err, errLabelPages)
		assert.Equal(t, maxLabelPages, requestCount(f))
	})

	t.Run("name の無い要素は errUnexpectedResponse", func(t *testing.T) {
		t.Parallel()
		_, c := newFakeGitHub(t, map[string]fakeResponse{"GET " + labelListPath(1): ok(`[{"color": "ffffff"}]`)})
		_, err := listLabels(context.Background(), c, repo)
		require.ErrorIs(t, err, errUnexpectedResponse)
	})

	t.Run("途中のページが失敗すれば errHTTP", func(t *testing.T) {
		t.Parallel()
		_, c := newFakeGitHub(t, map[string]fakeResponse{
			"GET " + labelListPath(1): ok(labelPage("a", perPage)),
			"GET " + labelListPath(2): {status: http.StatusBadGateway, body: `{}`},
		})
		_, err := listLabels(context.Background(), c, repo)
		require.ErrorIs(t, err, errHTTP)
	})
}

func Test_labelPath(t *testing.T) {
	t.Parallel()

	t.Run("名前の空白と / と % と非 ASCII をパスの1区間へエスケープする", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "/repos/o/r/labels/good%20first%20issue", labelPath(repo, "good first issue"))
		assert.Equal(t, "/repos/o/r/labels/a%2Fb", labelPath(repo, "a/b"))
		assert.Equal(t, "/repos/o/r/labels/100%25", labelPath(repo, "100%"))
		assert.Equal(t, "/repos/o/r/labels/%E4%B8%8D%E5%85%B7%E5%90%88", labelPath(repo, "不具合"))
	})
}

func Test_deleteLabels(t *testing.T) {
	t.Parallel()

	root := labelsRoot(t, labelsDecl)

	t.Run("全ページのラベルを1件ずつ消し、件数を出す", func(t *testing.T) {
		t.Parallel()
		routes := map[string]fakeResponse{
			"GET " + labelListPath(1):                       ok(labelPage("a", perPage)),
			"GET " + labelListPath(2):                       ok(`[{"name": "good first issue"}]`),
			"DELETE /repos/o/r/labels/good%20first%20issue": noContent(),
		}
		for i := range perPage {
			routes[fmt.Sprintf("DELETE /repos/o/r/labels/a-%d", i)] = noContent()
		}
		f, c := newFakeGitHub(t, routes)
		var out bytes.Buffer
		require.NoError(t, deleteLabels(context.Background(), root, c, repo, &out))
		w := f.writes()
		require.Len(t, w, perPage+1)
		assert.Equal(t, "DELETE /repos/o/r/labels/good%20first%20issue", targets(w)[perPage])
		assert.Contains(t, out.String(), "🔸 delete label: good first issue")
		assert.Contains(t, out.String(), fmt.Sprintf("✅ ラベルを %d 件削除しました", perPage+1))
	})

	t.Run("ラベルが0件なら何も消さず、0件であることを出して通る", func(t *testing.T) {
		t.Parallel()
		f, c := newFakeGitHub(t, map[string]fakeResponse{"GET " + labelListPath(1): ok(`[]`)})
		var out bytes.Buffer
		require.NoError(t, deleteLabels(context.Background(), root, c, repo, &out))
		assert.Empty(t, f.writes())
		assert.Contains(t, out.String(), "🟡 削除するラベルはありませんでした")
		assert.NotContains(t, out.String(), "✅")
	})

	t.Run("labels.json が宣言として正しくなければ、GitHub に問わず何も消さない", func(t *testing.T) {
		t.Parallel()
		f, c := newFakeGitHub(t, map[string]fakeResponse{"GET " + labelListPath(1): ok(`[{"name": "a"}]`)})
		require.ErrorIs(t, deleteLabels(context.Background(), labelsRoot(t, `[{"name": "a", "color": "red"}]`), c, repo, &bytes.Buffer{}), errLabelsDecl)
		assert.Zero(t, requestCount(f))
	})

	t.Run("一覧を読めなければ1件も消さない", func(t *testing.T) {
		t.Parallel()
		f, c := newFakeGitHub(t, map[string]fakeResponse{"GET " + labelListPath(1): {status: http.StatusForbidden, body: `{}`}})
		require.ErrorIs(t, deleteLabels(context.Background(), root, c, repo, &bytes.Buffer{}), errHTTP)
		assert.Empty(t, f.writes())
	})

	t.Run("2件目の削除が失敗すれば3件目を消さず、成功を出さない", func(t *testing.T) {
		t.Parallel()
		f, c := newFakeGitHub(t, map[string]fakeResponse{
			"GET " + labelListPath(1):    ok(`[{"name": "a"}, {"name": "b"}, {"name": "c"}]`),
			"DELETE /repos/o/r/labels/a": noContent(),
			"DELETE /repos/o/r/labels/c": noContent(),
		})
		var out bytes.Buffer
		require.ErrorIs(t, deleteLabels(context.Background(), root, c, repo, &out), errHTTP)
		assert.Equal(t, []string{"DELETE /repos/o/r/labels/a", "DELETE /repos/o/r/labels/b"}, targets(f.writes()))
		assert.NotContains(t, out.String(), "✅")
	})
}

func Test_createLabels(t *testing.T) {
	t.Parallel()

	t.Run("無いものは作り、大文字小文字を区別せずに同じ名前のものは宣言へ書き換え、宣言に無いものには触らない", func(t *testing.T) {
		t.Parallel()
		f, c := newFakeGitHub(t, map[string]fakeResponse{
			"GET " + labelListPath(1):     ok(`[{"name": "Bug"}, {"name": "stale"}]`),
			"PATCH /repos/o/r/labels/Bug": ok(`{}`),
			"POST /repos/o/r/labels":      {status: http.StatusCreated, body: `{}`},
		})
		var out bytes.Buffer
		require.NoError(t, createLabels(context.Background(), labelsRoot(t, labelsDecl), c, repo, &out))
		w := f.writes()
		assert.Equal(t, []string{"PATCH /repos/o/r/labels/Bug", "POST /repos/o/r/labels"}, targets(w))
		assert.JSONEq(t, `{"new_name": "bug", "color": "d73a4a", "description": "不具合"}`, w[0].body)
		assert.JSONEq(t, `{"name": "dependencies", "color": "0366d6", "description": ""}`, w[1].body)
		assert.Contains(t, out.String(), "✅ ラベルを 2 件そろえました")
	})

	t.Run("空白を含む既存のラベルは、PATCH のパスをエスケープする", func(t *testing.T) {
		t.Parallel()
		f, c := newFakeGitHub(t, map[string]fakeResponse{
			"GET " + labelListPath(1):                      ok(`[{"name": "Good First Issue"}]`),
			"PATCH /repos/o/r/labels/Good%20First%20Issue": ok(`{}`),
		})
		require.NoError(t, createLabels(context.Background(), labelsRoot(t, `[{"name": "good first issue", "color": "7057ff"}]`), c, repo, &bytes.Buffer{}))
		assert.Equal(t, []string{"PATCH /repos/o/r/labels/Good%20First%20Issue"}, targets(f.writes()))
	})

	t.Run("宣言が正しくなければ GitHub に問わない", func(t *testing.T) {
		t.Parallel()
		f, c := newFakeGitHub(t, map[string]fakeResponse{})
		require.ErrorIs(t, createLabels(context.Background(), labelsRoot(t, `[]`), c, repo, &bytes.Buffer{}), errLabelsDecl)
		assert.Zero(t, requestCount(f))
	})

	t.Run("一覧を読めなければ何も書かない", func(t *testing.T) {
		t.Parallel()
		f, c := newFakeGitHub(t, map[string]fakeResponse{"GET " + labelListPath(1): {status: http.StatusForbidden, body: `{}`}})
		require.ErrorIs(t, createLabels(context.Background(), labelsRoot(t, labelsDecl), c, repo, &bytes.Buffer{}), errHTTP)
		assert.Empty(t, f.writes())
	})

	t.Run("PATCH が失敗すれば残りを書かずに止め、成功を出さない", func(t *testing.T) {
		t.Parallel()
		f, c := newFakeGitHub(t, map[string]fakeResponse{
			"GET " + labelListPath(1): ok(`[{"name": "bug"}]`),
			"POST /repos/o/r/labels":  {status: http.StatusCreated, body: `{}`},
		})
		var out bytes.Buffer
		require.ErrorIs(t, createLabels(context.Background(), labelsRoot(t, labelsDecl), c, repo, &out), errHTTP)
		assert.Equal(t, []string{"PATCH /repos/o/r/labels/bug"}, targets(f.writes()))
		assert.NotContains(t, out.String(), "✅")
	})

	t.Run("POST が失敗すれば残りを書かずに止める", func(t *testing.T) {
		t.Parallel()
		f, c := newFakeGitHub(t, map[string]fakeResponse{"GET " + labelListPath(1): ok(`[]`)})
		require.ErrorIs(t, createLabels(context.Background(), labelsRoot(t, labelsDecl), c, repo, &bytes.Buffer{}), errHTTP)
		assert.Equal(t, []string{"POST /repos/o/r/labels"}, targets(f.writes()))
	})
}

func Test_listWorkflows(t *testing.T) {
	t.Parallel()

	t.Run("total_count に届くまでページを読む", func(t *testing.T) {
		t.Parallel()
		_, c := newFakeGitHub(t, map[string]fakeResponse{
			"GET " + workflowListPath(1): ok(`{"total_count": 2, "workflows": [{"id": 1, "name": "a", "state": "active"}]}`),
			"GET " + workflowListPath(2): ok(`{"total_count": 2, "workflows": [{"id": 2, "name": "b", "state": "disabled_fork"}]}`),
		})
		got, err := listWorkflows(context.Background(), c, repo)
		require.NoError(t, err)
		assert.Equal(t, []workflowState{{1, "a", "active"}, {2, "b", "disabled_fork"}}, got)
	})

	t.Run("total_count が0なら空", func(t *testing.T) {
		t.Parallel()
		_, c := newFakeGitHub(t, map[string]fakeResponse{"GET " + workflowListPath(1): ok(`{"total_count": 0, "workflows": []}`)})
		got, err := listWorkflows(context.Background(), c, repo)
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	for name, c := range map[string]struct {
		pages []string
		want  error
	}{
		"total_count が無い":             {[]string{`{"workflows": [{"id": 1, "name": "a", "state": "active"}]}`}, errUnexpectedResponse},
		"total_count が null":          {[]string{`{"total_count": null, "workflows": [{"id": 1, "name": "a", "state": "active"}]}`}, errUnexpectedResponse},
		"total_count より多く返る":          {[]string{`{"total_count": 0, "workflows": [{"id": 1, "name": "a", "state": "active"}]}`}, errUnexpectedResponse},
		"2ページ目まで読んで total_count を超える": {[]string{`{"total_count": 2, "workflows": [{"id": 1, "name": "a", "state": "active"}]}`, `{"total_count": 2, "workflows": [{"id": 2, "name": "b", "state": "active"}, {"id": 3, "name": "c", "state": "active"}]}`}, errUnexpectedResponse},
		"id の無い workflow":             {[]string{`{"total_count": 1, "workflows": [{"name": "a", "state": "active"}]}`}, errUnexpectedResponse},
		"state の無い workflow":          {[]string{`{"total_count": 1, "workflows": [{"id": 1, "name": "a"}]}`}, errUnexpectedResponse},
		"total_count に届く前にページが尽きる":    {[]string{`{"total_count": 2, "workflows": []}`}, errShortPage},
	} {
		t.Run(name+"なら失敗する", func(t *testing.T) {
			t.Parallel()
			routes := map[string]fakeResponse{}
			for i, body := range c.pages {
				routes["GET "+workflowListPath(i+1)] = ok(body)
			}
			_, cl := newFakeGitHub(t, routes)
			_, err := listWorkflows(context.Background(), cl, repo)
			require.ErrorIs(t, err, c.want)
		})
	}
}

func Test_enableWorkflows(t *testing.T) {
	t.Parallel()

	list := `{"total_count": 4, "workflows": [
		{"id": 1, "name": "fork", "state": "disabled_fork"},
		{"id": 2, "name": "manual", "state": "disabled_manually"},
		{"id": 3, "name": "idle", "state": "disabled_inactivity"},
		{"id": 4, "name": "fork2", "state": "disabled_fork"}]}`

	t.Run("disabled_fork だけを有効にし、人が止めたものには触らない", func(t *testing.T) {
		t.Parallel()
		f, c := newFakeGitHub(t, map[string]fakeResponse{
			"GET " + workflowListPath(1):                ok(list),
			"PUT /repos/o/r/actions/workflows/1/enable": noContent(),
			"PUT /repos/o/r/actions/workflows/4/enable": noContent(),
		})
		var out bytes.Buffer
		require.NoError(t, enableWorkflows(context.Background(), c, repo, &out))
		assert.Equal(t, []string{"PUT /repos/o/r/actions/workflows/1/enable", "PUT /repos/o/r/actions/workflows/4/enable"}, targets(f.writes()))
		assert.Contains(t, out.String(), "✅ ワークフローを 2 件有効化しました。")
	})

	t.Run("2ページ目の disabled_fork も有効にする", func(t *testing.T) {
		t.Parallel()
		f, c := newFakeGitHub(t, map[string]fakeResponse{
			"GET " + workflowListPath(1):                ok(`{"total_count": 2, "workflows": [{"id": 1, "name": "a", "state": "active"}]}`),
			"GET " + workflowListPath(2):                ok(`{"total_count": 2, "workflows": [{"id": 2, "name": "b", "state": "disabled_fork"}]}`),
			"PUT /repos/o/r/actions/workflows/2/enable": noContent(),
		})
		require.NoError(t, enableWorkflows(context.Background(), c, repo, &bytes.Buffer{}))
		assert.Equal(t, []string{"PUT /repos/o/r/actions/workflows/2/enable"}, targets(f.writes()))
	})

	t.Run("disabled_fork が無ければ何も書かずに通る", func(t *testing.T) {
		t.Parallel()
		f, c := newFakeGitHub(t, map[string]fakeResponse{
			"GET " + workflowListPath(1): ok(`{"total_count": 1, "workflows": [{"id": 1, "name": "a", "state": "active"}]}`),
		})
		var out bytes.Buffer
		require.NoError(t, enableWorkflows(context.Background(), c, repo, &out))
		assert.Empty(t, f.writes())
		assert.Contains(t, out.String(), "🟡 disabled_fork 状態のワークフローはありません（workflow 1 件）。")
	})

	t.Run("workflow が1つも無ければ errNoWorkflows（何も見ていないことを成功にしない）", func(t *testing.T) {
		t.Parallel()
		_, c := newFakeGitHub(t, map[string]fakeResponse{"GET " + workflowListPath(1): ok(`{"total_count": 0, "workflows": []}`)})
		require.ErrorIs(t, enableWorkflows(context.Background(), c, repo, &bytes.Buffer{}), errNoWorkflows)
	})

	t.Run("一覧を読めなければ errHTTP で、何も書かない", func(t *testing.T) {
		t.Parallel()
		f, c := newFakeGitHub(t, map[string]fakeResponse{"GET " + workflowListPath(1): {status: http.StatusForbidden, body: `{}`}})
		require.ErrorIs(t, enableWorkflows(context.Background(), c, repo, &bytes.Buffer{}), errHTTP)
		assert.Empty(t, f.writes())
	})

	t.Run("有効化の失敗は全件を試してから、失敗したものだけを名指して errEnable", func(t *testing.T) {
		t.Parallel()
		f, c := newFakeGitHub(t, map[string]fakeResponse{
			"GET " + workflowListPath(1):                ok(list),
			"PUT /repos/o/r/actions/workflows/4/enable": noContent(),
		})
		var out bytes.Buffer
		err := enableWorkflows(context.Background(), c, repo, &out)
		require.ErrorIs(t, err, errEnable)
		assert.True(t, strings.HasSuffix(err.Error(), "fork: "+errEnable.Error()), err.Error())
		assert.Equal(t, []string{"PUT /repos/o/r/actions/workflows/1/enable", "PUT /repos/o/r/actions/workflows/4/enable"}, targets(f.writes()), "失敗した後の workflow も試す")
		assert.Contains(t, out.String(), "⚠️ 有効化に失敗: fork")
		assert.NotContains(t, out.String(), "✅")
	})

	t.Run("全件の有効化が失敗すれば全件を名指して errEnable", func(t *testing.T) {
		t.Parallel()
		_, c := newFakeGitHub(t, map[string]fakeResponse{"GET " + workflowListPath(1): ok(list)})
		err := enableWorkflows(context.Background(), c, repo, &bytes.Buffer{})
		require.ErrorIs(t, err, errEnable)
		assert.Contains(t, err.Error(), "fork, fork2")
	})
}

func Test_createDeployBranch(t *testing.T) {
	t.Parallel()

	sha := strings.Repeat("a", 40)
	root := t.TempDir()
	writeFile(t, root, branchesFile, sourceBranches)
	routes := func(defaultBranch, refPath, refBody string) map[string]fakeResponse {
		return map[string]fakeResponse{
			"GET /repos/o/r":           ok(`{"default_branch": "` + defaultBranch + `"}`),
			"GET " + refPath:           ok(refBody),
			"POST /repos/o/r/git/refs": {status: http.StatusCreated, body: `{}`},
		}
	}
	mainRef := "/repos/o/r/git/ref/heads/main"

	t.Run("デフォルトブランチ（/ を含む）の先頭から、env のデプロイブランチを作る", func(t *testing.T) {
		t.Parallel()
		f, c := newFakeGitHub(t, routes("release/v0.2.0", "/repos/o/r/git/ref/heads/release/v0.2.0", `{"object": {"sha": "`+sha+`"}}`))
		var out bytes.Buffer
		require.NoError(t, createDeployBranch(context.Background(), root, c, repo, "admin", &out))
		w := f.writes()
		require.Len(t, w, 1)
		assert.Equal(t, "POST /repos/o/r/git/refs", targets(w)[0])
		assert.JSONEq(t, `{"ref": "refs/heads/admin", "sha": "`+sha+`"}`, w[0].body)
		assert.Equal(t, "✅ admin を release/v0.2.0 の先頭 "+sha+" から作りました\n", out.String())
	})

	t.Run("sha が64桁の16進でも作る", func(t *testing.T) {
		t.Parallel()
		long := strings.Repeat("b", 64)
		f, c := newFakeGitHub(t, routes("main", mainRef, `{"object": {"sha": "`+long+`"}}`))
		require.NoError(t, createDeployBranch(context.Background(), root, c, repo, "admin", &bytes.Buffer{}))
		assert.Len(t, f.writes(), 1)
	})

	t.Run("宣言に無い env は GitHub に問わずに errNoDeployEnv", func(t *testing.T) {
		t.Parallel()
		f, c := newFakeGitHub(t, map[string]fakeResponse{})
		require.ErrorIs(t, createDeployBranch(context.Background(), root, c, repo, "prd", &bytes.Buffer{}), errNoDeployEnv)
		assert.Zero(t, requestCount(f))
	})

	t.Run("branches.json が無ければ GitHub に問わずに失敗する", func(t *testing.T) {
		t.Parallel()
		f, c := newFakeGitHub(t, map[string]fakeResponse{})
		require.ErrorIs(t, createDeployBranch(context.Background(), t.TempDir(), c, repo, "admin", &bytes.Buffer{}), os.ErrNotExist)
		assert.Zero(t, requestCount(f))
	})

	for name, c := range map[string]struct{ def, ref string }{
		"default_branch が空": {"", `{"object": {"sha": "` + sha + `"}}`},
		"object が無い":        {"main", `{}`},
		"sha が無い":           {"main", `{"object": {}}`},
		"sha が短い":           {"main", `{"object": {"sha": "abc"}}`},
		"sha が41桁":          {"main", `{"object": {"sha": "` + sha + `a"}}`},
		"sha が大文字":          {"main", `{"object": {"sha": "` + strings.ToUpper(sha) + `"}}`},
	} {
		t.Run(name+"なら作らずに errUnexpectedResponse", func(t *testing.T) {
			t.Parallel()
			ref := mainRef
			if c.def == "" {
				ref = "/repos/o/r/git/ref/heads/"
			}
			f, cl := newFakeGitHub(t, routes(c.def, ref, c.ref))
			require.ErrorIs(t, createDeployBranch(context.Background(), root, cl, repo, "admin", &bytes.Buffer{}), errUnexpectedResponse)
			assert.Empty(t, f.writes())
		})
	}

	t.Run("リポジトリを読めなければ作らずに errHTTP", func(t *testing.T) {
		t.Parallel()
		f, c := newFakeGitHub(t, map[string]fakeResponse{"GET /repos/o/r": {status: http.StatusNotFound, body: `{}`}})
		require.ErrorIs(t, createDeployBranch(context.Background(), root, c, repo, "admin", &bytes.Buffer{}), errHTTP)
		assert.Empty(t, f.writes())
	})

	t.Run("デフォルトブランチの ref が 404（空のリポジトリ）なら作らずに errHTTP", func(t *testing.T) {
		t.Parallel()
		f, c := newFakeGitHub(t, map[string]fakeResponse{"GET /repos/o/r": ok(`{"default_branch": "main"}`)})
		require.ErrorIs(t, createDeployBranch(context.Background(), root, c, repo, "admin", &bytes.Buffer{}), errHTTP)
		assert.Empty(t, f.writes())
	})

	t.Run("既に在れば（422）上書きせずに errHTTP", func(t *testing.T) {
		t.Parallel()
		r := routes("main", mainRef, `{"object": {"sha": "`+sha+`"}}`)
		r["POST /repos/o/r/git/refs"] = fakeResponse{status: http.StatusUnprocessableEntity, body: `{"message": "Reference already exists"}`}
		f, c := newFakeGitHub(t, r)
		err := createDeployBranch(context.Background(), root, c, repo, "admin", &bytes.Buffer{})
		require.ErrorIs(t, err, errHTTP)
		assert.Contains(t, err.Error(), "422")
		assert.Len(t, f.writes(), 1)
	})
}

func Test_escapeSegments(t *testing.T) {
	t.Parallel()

	for in, want := range map[string]string{
		"release/v0.2.0": "release/v0.2.0",
		"a b/c#d":        "a%20b/c%23d",
		"":               "",
		"/a/":            "/a/",
	} {
		t.Run(fmt.Sprintf("%q", in), func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, want, escapeSegments(in))
		})
	}
}

func Test_runSetup(t *testing.T) {
	t.Parallel()

	sha := strings.Repeat("a", 40)
	root := labelsRoot(t, labelsDecl)
	writeFile(t, root, branchesFile, sourceBranches)

	t.Run("create-deploy-branch に env が無ければ、GitHub に問わずに errDeployEnvArg", func(t *testing.T) {
		t.Parallel()
		f, c := newFakeGitHub(t, map[string]fakeResponse{})
		require.ErrorIs(t, runSetup(context.Background(), "create-deploy-branch", root, c, repo, "", &bytes.Buffer{}), errDeployEnvArg)
		assert.Zero(t, requestCount(f))
	})

	for _, sub := range []string{"delete-labels", "create-labels", "enable-workflows", "create-deploy-branch"} {
		t.Run(sub+"は repo が無ければ GitHub に問わずに errNoRepo", func(t *testing.T) {
			t.Parallel()
			f, c := newFakeGitHub(t, map[string]fakeResponse{})
			require.ErrorIs(t, runSetup(context.Background(), sub, root, c, "", "admin", &bytes.Buffer{}), errNoRepo)
			assert.Zero(t, requestCount(f))
		})
		t.Run(sub+"は token が無ければ GitHub に問わずに errNoToken", func(t *testing.T) {
			t.Parallel()
			f, c := newFakeGitHub(t, map[string]fakeResponse{})
			c.token = ""
			require.ErrorIs(t, runSetup(context.Background(), sub, root, c, repo, "admin", &bytes.Buffer{}), errNoToken)
			assert.Zero(t, requestCount(f))
		})
	}

	labelList := map[string]fakeResponse{
		"GET " + labelListPath(1):                   ok(`[{"name": "x"}]`),
		"DELETE /repos/o/r/labels/x":                noContent(),
		"POST /repos/o/r/labels":                    {status: http.StatusCreated, body: `{}`},
		"GET " + workflowListPath(1):                ok(`{"total_count": 1, "workflows": [{"id": 9, "name": "w", "state": "disabled_fork"}]}`),
		"PUT /repos/o/r/actions/workflows/9/enable": noContent(),
		"GET /repos/o/r":                            ok(`{"default_branch": "main"}`),
		"GET /repos/o/r/git/ref/heads/main":         ok(`{"object": {"sha": "` + sha + `"}}`),
		"POST /repos/o/r/git/refs":                  {status: http.StatusCreated, body: `{}`},
	}
	for sub, want := range map[string][]string{
		"delete-labels":        {"DELETE /repos/o/r/labels/x"},
		"create-labels":        {"POST /repos/o/r/labels", "POST /repos/o/r/labels"},
		"enable-workflows":     {"PUT /repos/o/r/actions/workflows/9/enable"},
		"create-deploy-branch": {"POST /repos/o/r/git/refs"},
	} {
		t.Run(sub+"はそのサブコマンドの書き込みだけをする", func(t *testing.T) {
			t.Parallel()
			f, c := newFakeGitHub(t, labelList)
			require.NoError(t, runSetup(context.Background(), sub, root, c, repo, "admin", &bytes.Buffer{}))
			assert.Equal(t, want, targets(f.writes()))
		})
	}
}

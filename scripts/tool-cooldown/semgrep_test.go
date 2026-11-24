package main

import (
	"bufio"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	semgrepPin     = strings.Repeat("a", 40)
	semgrepNewer   = strings.Repeat("b", 40)
	semgrepNewest  = strings.Repeat("c", 40)
	semgrepFixture = "SEMGREP_RULES_COMMIT := " + semgrepPin + "\n"
)

const (
	commitsListURI = "/repos/" + semgrepRulesRepo + "/commits?per_page=100&page=1"
	treesURI       = "/repos/" + semgrepRulesRepo + "/git/trees/"
	repoURI        = "/repos/" + semgrepRulesRepo
	compareURI     = repoURI + "/compare/develop..."
)

func commitsPageURI(page int) string {
	return fmt.Sprintf("/repos/%s/commits?per_page=100&page=%d", semgrepRulesRepo, page)
}

// freshPage は窓の内側の commit で埋まった1ページを返す。pin も窓を満たす commit も含まない。
func freshPage(page int) string {
	entries := make([]string, commitsPerPage)
	for i := range entries {
		entries[i] = commitJSON(fmt.Sprintf("%040x", page*commitsPerPage+i), daysAgo(1), "rootf")
	}

	return "[" + strings.Join(entries, ",") + "]"
}

// onDefaultBranch は sha を既定ブランチ develop と比べた結果が status になる上流の経路を返す。
func onDefaultBranch(sha, status string) map[string]string {
	return map[string]string{
		repoURI:          `{"default_branch":"develop"}`,
		compareURI + sha: `{"status":"` + status + `"}`,
	}
}

func writeSemgrepDecl(t *testing.T, dir string) {
	t.Helper()
	path := filepath.Join(dir, semgrepFile)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte(semgrepFixture), 0o600))
}

func commitJSON(sha, date, tree string) string {
	return `{"sha":"` + sha + `","commit":{"committer":{"date":"` + date + `"},"tree":{"sha":"` + tree + `"}}}`
}

// rules が空なら terraform/ を持たない。
func rootTreeJSON(rules string) string {
	entries := `{"path":"python","type":"tree","sha":"py0"}`
	if rules != "" {
		entries += `,{"path":"terraform","type":"tree","sha":"` + rules + `"}`
	}

	return `{"tree":[` + entries + `]}`
}

func semgrepTool() tool {
	return tool{key: semgrepRulesKey, version: semgrepPin, backend: semgrepRulesKey, file: semgrepFile}
}

func Test_parseSemgrepRules(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("SEMGREP_RULES_COMMIT の代入から commit を読む", func(t *testing.T) {
			t.Parallel()
			content := "## 説明\n" + semgrepFixture +
				"SEMGREP_RULES_SHA256 := " + strings.Repeat("0", 64) + "\n" +
				"SEMGREP_RULES_TGZ := tmp/semgrep-rules-$(SEMGREP_RULES_COMMIT).tar.gz\n"

			got, err := parseSemgrepRules([]byte(content))
			require.NoError(t, err)
			require.Len(t, got, 1)
			assert.Equal(t, semgrepRulesKey+"@"+semgrepPin, got[0].id())
		})

		t.Run("コメントの中の代入は読まない", func(t *testing.T) {
			t.Parallel()
			got, err := parseSemgrepRules([]byte("# SEMGREP_RULES_COMMIT := " + semgrepNewer + "\n" + semgrepFixture))
			require.NoError(t, err)
			require.Len(t, got, 1)
			assert.Equal(t, semgrepPin, got[0].version)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		const noAssign, firstLine = "代入がありません", "1 行目: "
		for name, tc := range map[string]struct{ content, want string }{
			"代入が無い":          {"SEMGREP_RULES_SHA256 := " + strings.Repeat("0", 64) + "\n", noAssign},
			"コメントだけで代入が無い":   {"# SEMGREP_RULES_COMMIT := " + semgrepPin + "\n", noAssign},
			"空":              {"", noAssign},
			"39桁":            {"SEMGREP_RULES_COMMIT := " + semgrepPin[:39] + "\n", firstLine},
			"大文字の16進":        {"SEMGREP_RULES_COMMIT := " + strings.ToUpper(semgrepPin) + "\n", firstLine},
			"再帰展開の代入":        {"SEMGREP_RULES_COMMIT = " + semgrepPin + "\n", firstLine},
			"条件付きの代入":        {"SEMGREP_RULES_COMMIT ?= " + semgrepPin + "\n", firstLine},
			"追記の代入":          {"SEMGREP_RULES_COMMIT += " + semgrepPin + "\n", firstLine},
			"POSIX の即時展開の代入": {"SEMGREP_RULES_COMMIT ::= " + semgrepPin + "\n", firstLine},
			"シェルの代入":         {"SEMGREP_RULES_COMMIT != echo " + semgrepPin + "\n", firstLine},
			"export 付きの代入":   {"export SEMGREP_RULES_COMMIT := " + semgrepPin + "\n", firstLine},
			"override 付きの代入": {"override SEMGREP_RULES_COMMIT := " + semgrepPin + "\n", firstLine},
			"行末のコメント":        {"SEMGREP_RULES_COMMIT := " + semgrepPin + " # 固定\n", firstLine},
			"変数の参照":          {"SEMGREP_RULES_COMMIT := $(OTHER)\n", firstLine},
			"2つの代入":          {semgrepFixture + "SEMGREP_RULES_COMMIT := " + semgrepNewer + "\n", "2 行目: 2つ目の代入です"},
		} {
			t.Run(name+"ならエラーにする", func(t *testing.T) {
				t.Parallel()
				_, err := parseSemgrepRules([]byte(tc.content))
				require.ErrorIs(t, err, ErrInvalidSemgrep)
				assert.Contains(t, err.Error(), tc.want)
			})
		}

		t.Run("読めない長さの行は宣言の誤りではなく読み取りのエラーにする", func(t *testing.T) {
			t.Parallel()
			_, err := parseSemgrepRules([]byte(strings.Repeat("x", 64*1024+1) + "\n" + semgrepFixture))
			require.ErrorIs(t, err, bufio.ErrTooLong)
			assert.NotErrorIs(t, err, ErrInvalidSemgrep)
		})
	})
}

func Test_githubCommit(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("committer の日付と根の tree を返す", func(t *testing.T) {
			t.Parallel()
			client := fakeUpstream(t, respondJSON("/repos/"+semgrepRulesRepo+"/commits/"+semgrepPin,
				commitJSON(semgrepPin, "2026-08-01T00:00:00Z", "root0")))

			got, err := githubCommit(t.Context(), client, semgrepRulesRepo, semgrepPin)
			require.NoError(t, err)
			assert.Equal(t, "2026-08-01T00:00:00Z", got.at.Format(time.RFC3339))
			assert.Equal(t, "root0", got.tree)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("上流が commit を知らなければエラーにする", func(t *testing.T) {
			t.Parallel()
			client := fakeUpstream(t, respondStatus(http.StatusNotFound))

			_, err := githubCommit(t.Context(), client, semgrepRulesRepo, semgrepPin)
			require.ErrorIs(t, err, errNotFound)
		})

		t.Run("応答の sha が求めた commit と違えばエラーにする", func(t *testing.T) {
			t.Parallel()
			client := fakeUpstream(t, respondJSON("/repos/"+semgrepRulesRepo+"/commits/"+semgrepPin,
				commitJSON(semgrepNewer, "2026-08-01T00:00:00Z", "root0")))

			_, err := githubCommit(t.Context(), client, semgrepRulesRepo, semgrepPin)
			require.ErrorIs(t, err, errNotFound)
		})

		// 日付をゼロ値のまま返すと「公開から数十年」と読まれ、窓を無条件に通る。
		t.Run("committer の日付が無ければエラーにする", func(t *testing.T) {
			t.Parallel()
			client := fakeUpstream(t, respondJSON("/repos/"+semgrepRulesRepo+"/commits/"+semgrepPin,
				`{"sha":"`+semgrepPin+`","commit":{"tree":{"sha":"root0"}}}`))

			_, err := githubCommit(t.Context(), client, semgrepRulesRepo, semgrepPin)
			require.ErrorIs(t, err, errNotFound)
		})

		t.Run("根の tree が無ければエラーにする", func(t *testing.T) {
			t.Parallel()
			client := fakeUpstream(t, respondJSON("/repos/"+semgrepRulesRepo+"/commits/"+semgrepPin,
				commitJSON(semgrepPin, "2026-08-01T00:00:00Z", "")))

			_, err := githubCommit(t.Context(), client, semgrepRulesRepo, semgrepPin)
			require.ErrorIs(t, err, errNotFound)
		})
	})
}

func Test_verifyOnDefaultBranch(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		for _, status := range []string{"behind", "identical"} {
			t.Run("既定ブランチとの比較が "+status+" なら既定ブランチの commit として通す", func(t *testing.T) {
				t.Parallel()
				client := upstreamStub(t, onDefaultBranch(semgrepPin, status))

				require.NoError(t, verifyOnDefaultBranch(t.Context(), client, semgrepRulesRepo, semgrepPin))
			})
		}
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for _, status := range []string{"ahead", "diverged", ""} {
			t.Run("既定ブランチとの比較が "+strconv.Quote(status)+" ならエラーにする", func(t *testing.T) {
				t.Parallel()
				client := upstreamStub(t, onDefaultBranch(semgrepPin, status))

				err := verifyOnDefaultBranch(t.Context(), client, semgrepRulesRepo, semgrepPin)
				require.ErrorIs(t, err, errNotOnDefaultBranch)
			})
		}

		t.Run("既定ブランチの名前が無ければエラーにする", func(t *testing.T) {
			t.Parallel()
			routes := onDefaultBranch(semgrepPin, "behind")
			routes[repoURI] = `{}`
			client := upstreamStub(t, routes)

			err := verifyOnDefaultBranch(t.Context(), client, semgrepRulesRepo, semgrepPin)
			require.ErrorIs(t, err, errNotFound)
		})

		t.Run("比較を引けなければエラーにする", func(t *testing.T) {
			t.Parallel()
			client := upstreamStub(t, map[string]string{repoURI: `{"default_branch":"develop"}`})

			err := verifyOnDefaultBranch(t.Context(), client, semgrepRulesRepo, semgrepPin)
			require.ErrorIs(t, err, errNotFound)
		})

		t.Run("repository を引けなければエラーにする", func(t *testing.T) {
			t.Parallel()
			client := fakeUpstream(t, respondStatus(http.StatusInternalServerError))

			err := verifyOnDefaultBranch(t.Context(), client, semgrepRulesRepo, semgrepPin)
			require.ErrorIs(t, err, errUpstreamStatus)
		})
	})
}

func Test_githubCommits(t *testing.T) {
	t.Parallel()

	list := func(commits ...string) string { return "[" + strings.Join(commits, ",") + "]" }
	commitsOf := func(t *testing.T, client *http.Client) ([]commitInfo, error) {
		t.Helper()

		return githubCommits(t.Context(), client, semgrepRulesRepo, semgrepPin, releaseWindowDays, outdatedNow)
	}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("上流の順序のまま返す", func(t *testing.T) {
			t.Parallel()
			client := upstreamStub(t, map[string]string{
				commitsListURI: list(commitJSON(semgrepNewer, daysAgo(1), "root1"), commitJSON(semgrepPin, daysAgo(30), "root0")),
			})

			got, err := commitsOf(t, client)
			require.NoError(t, err)
			require.Len(t, got, 2)
			assert.Equal(t, semgrepNewer, got[0].sha)
			assert.Equal(t, "root0", got[1].tree)
		})

		t.Run("1ページ目に pin が無ければ次のページを遡る", func(t *testing.T) {
			t.Parallel()
			client := upstreamStub(t, map[string]string{
				commitsPageURI(1): freshPage(1),
				commitsPageURI(2): list(commitJSON(semgrepPin, daysAgo(30), "root0")),
			})

			got, err := commitsOf(t, client)
			require.NoError(t, err)
			require.Len(t, got, commitsPerPage+1)
			assert.Equal(t, semgrepPin, got[commitsPerPage].sha)
		})

		// 2ページ目の経路を持たない上流で、引けば errNotFound になる。
		t.Run("pin と窓を満たす commit が揃えば次のページを引かない", func(t *testing.T) {
			t.Parallel()
			entries := make([]string, 0, commitsPerPage)
			for i := range commitsPerPage - 1 {
				entries = append(entries, commitJSON(strings.Repeat("d", 39)+strconv.Itoa(i%10), daysAgo(1), "rootf"))
			}
			entries = append(entries, commitJSON(semgrepPin, daysAgo(30), "root0"))
			client := upstreamStub(t, map[string]string{commitsListURI: list(entries...)})

			got, err := commitsOf(t, client)
			require.NoError(t, err)
			assert.Len(t, got, commitsPerPage)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("0件ならエラーにする", func(t *testing.T) {
			t.Parallel()
			client := upstreamStub(t, map[string]string{commitsListURI: `[]`})

			_, err := commitsOf(t, client)
			require.ErrorIs(t, err, errNotFound)
		})

		for name, entry := range map[string]string{
			"tree":          commitJSON(semgrepNewer, daysAgo(1), ""),
			"committer の日付": `{"sha":"` + semgrepNewer + `","commit":{"tree":{"sha":"root1"}}}`,
			"sha":           commitJSON("", daysAgo(1), "root1"),
		} {
			t.Run(name+"を持たない commit があればエラーにする", func(t *testing.T) {
				t.Parallel()
				client := upstreamStub(t, map[string]string{
					commitsListURI: list(entry, commitJSON(semgrepPin, daysAgo(30), "root0")),
				})

				_, err := commitsOf(t, client)
				require.ErrorIs(t, err, errNotFound)
			})
		}

		t.Run("一覧が尽きても pin が無ければエラーにする", func(t *testing.T) {
			t.Parallel()
			client := upstreamStub(t, map[string]string{
				commitsListURI: list(commitJSON(semgrepNewer, daysAgo(20), "root1")),
			})

			_, err := commitsOf(t, client)
			require.ErrorIs(t, err, errCommitsExhausted)
		})

		t.Run("一覧が尽きても窓を満たす commit が無ければエラーにする", func(t *testing.T) {
			t.Parallel()
			client := upstreamStub(t, map[string]string{
				commitsListURI: list(commitJSON(semgrepNewer, daysAgo(1), "root1"), commitJSON(semgrepPin, daysAgo(2), "root0")),
			})

			_, err := commitsOf(t, client)
			require.ErrorIs(t, err, errCommitsExhausted)
		})

		// 上限の次のページの経路を持たない上流で、引けば errNotFound になる。
		t.Run("上限のページまで遡っても揃わなければそれ以上引かずにエラーにする", func(t *testing.T) {
			t.Parallel()
			routes := map[string]string{}
			for page := 1; page <= maxCommitPages; page++ {
				routes[commitsPageURI(page)] = freshPage(page)
			}

			_, err := commitsOf(t, upstreamStub(t, routes))
			require.ErrorIs(t, err, errCommitsExhausted)
		})

		t.Run("次のページを引けなければエラーにする", func(t *testing.T) {
			t.Parallel()
			client := upstreamStub(t, map[string]string{commitsPageURI(1): freshPage(1)})

			_, err := commitsOf(t, client)
			require.ErrorIs(t, err, errNotFound)
		})

		t.Run("上流が失敗すればエラーにする", func(t *testing.T) {
			t.Parallel()
			client := fakeUpstream(t, respondStatus(http.StatusInternalServerError))

			_, err := commitsOf(t, client)
			require.ErrorIs(t, err, errUpstreamStatus)
		})
	})
}

func Test_rulesTree(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("terraform の tree の SHA を返す", func(t *testing.T) {
			t.Parallel()
			client := upstreamStub(t, map[string]string{treesURI + "root0": rootTreeJSON("tf0")})

			got, err := rulesTree(t.Context(), client, semgrepRulesRepo, "root0")
			require.NoError(t, err)
			assert.Equal(t, "tf0", got)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("terraform が無ければエラーにする", func(t *testing.T) {
			t.Parallel()
			client := upstreamStub(t, map[string]string{treesURI + "root0": rootTreeJSON("")})

			_, err := rulesTree(t.Context(), client, semgrepRulesRepo, "root0")
			require.ErrorIs(t, err, errNoRulesTree)
		})

		t.Run("terraform の tree の SHA が空ならエラーにする", func(t *testing.T) {
			t.Parallel()
			client := upstreamStub(t, map[string]string{
				treesURI + "root0": `{"tree":[{"path":"terraform","type":"tree","sha":""}]}`,
			})

			_, err := rulesTree(t.Context(), client, semgrepRulesRepo, "root0")
			require.ErrorIs(t, err, errNoRulesTree)
		})

		t.Run("terraform がディレクトリでなければエラーにする", func(t *testing.T) {
			t.Parallel()
			client := upstreamStub(t, map[string]string{
				treesURI + "root0": `{"tree":[{"path":"terraform","type":"blob","sha":"x"}]}`,
			})

			_, err := rulesTree(t.Context(), client, semgrepRulesRepo, "root0")
			require.ErrorIs(t, err, errNoRulesTree)
		})

		t.Run("tree を引けなければエラーにする", func(t *testing.T) {
			t.Parallel()
			client := upstreamStub(t, map[string]string{})

			_, err := rulesTree(t.Context(), client, semgrepRulesRepo, "root0")
			require.ErrorIs(t, err, errNotFound)
		})
	})
}

func Test_evaluateCommit(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("terraform の tree が pin と違う窓を満たす commit は動かせる", func(t *testing.T) {
			t.Parallel()
			client := upstreamStub(t, map[string]string{
				commitsListURI: `[` + commitJSON(semgrepNewer, daysAgo(20), "root1") + `,` +
					commitJSON(semgrepPin, daysAgo(40), "root0") + `]`,
				treesURI + "root0": rootTreeJSON("tf0"),
				treesURI + "root1": rootTreeJSON("tf1"),
			})

			c := evaluateCommit(t.Context(), client, semgrepTool(), outdatedNow)

			require.NoError(t, c.err)
			assert.Equal(t, semgrepNewer, c.eligible)
			assert.Equal(t, 20, c.eligibleAge)
			assert.Equal(t, releaseWindowDays, c.window)
			assert.True(t, c.actionable())
			assert.False(t, c.heldByWindow())
		})

		// 上流の commit が進むたびに動かせると言うと、報告が自分で閉じなくなる。
		t.Run("terraform の tree が pin と同じなら動かせるとは言わない", func(t *testing.T) {
			t.Parallel()
			client := upstreamStub(t, map[string]string{
				commitsListURI: `[` + commitJSON(semgrepNewer, daysAgo(20), "root1") + `,` +
					commitJSON(semgrepPin, daysAgo(40), "root0") + `]`,
				treesURI + "root0": rootTreeJSON("tf0"),
				treesURI + "root1": rootTreeJSON("tf0"),
			})

			c := evaluateCommit(t.Context(), client, semgrepTool(), outdatedNow)

			require.NoError(t, c.err)
			assert.Equal(t, semgrepNewer, c.eligible)
			assert.False(t, c.actionable())
			assert.False(t, c.heldByWindow())
		})

		t.Run("窓の内側で terraform が変わった commit は待ちになる", func(t *testing.T) {
			t.Parallel()
			client := upstreamStub(t, map[string]string{
				commitsListURI: `[` + commitJSON(semgrepNewer, daysAgo(2), "root1") + `,` +
					commitJSON(semgrepPin, daysAgo(40), "root0") + `]`,
				treesURI + "root0": rootTreeJSON("tf0"),
				treesURI + "root1": rootTreeJSON("tf1"),
			})

			c := evaluateCommit(t.Context(), client, semgrepTool(), outdatedNow)

			require.NoError(t, c.err)
			assert.Equal(t, semgrepNewer, c.latest)
			assert.Equal(t, semgrepPin, c.eligible, "窓を満たす最新は pin そのもの")
			assert.False(t, c.actionable())
			assert.True(t, c.heldByWindow())
		})

		t.Run("公開14日の commit は窓を満たし13日の commit は満たさない", func(t *testing.T) {
			t.Parallel()
			client := upstreamStub(t, map[string]string{
				commitsListURI: `[` + commitJSON(semgrepNewest, daysAgo(13), "root2") + `,` +
					commitJSON(semgrepNewer, daysAgo(14), "root1") + `,` +
					commitJSON(semgrepPin, daysAgo(40), "root0") + `]`,
				treesURI + "root0": rootTreeJSON("tf0"),
				treesURI + "root1": rootTreeJSON("tf1"),
				treesURI + "root2": rootTreeJSON("tf2"),
			})

			c := evaluateCommit(t.Context(), client, semgrepTool(), outdatedNow)

			require.NoError(t, c.err)
			assert.Equal(t, semgrepNewer, c.eligible)
			assert.Equal(t, semgrepNewest, c.latest)
		})

		t.Run("pin が eligible より新しければ動かせるとは言わない", func(t *testing.T) {
			t.Parallel()
			client := upstreamStub(t, map[string]string{
				commitsListURI: `[` + commitJSON(semgrepPin, daysAgo(2), "root0") + `,` +
					commitJSON(semgrepNewer, daysAgo(20), "root1") + `]`,
				treesURI + "root0": rootTreeJSON("tf0"),
				treesURI + "root1": rootTreeJSON("tf1"),
			})

			c := evaluateCommit(t.Context(), client, semgrepTool(), outdatedNow)

			require.NoError(t, c.err)
			assert.Equal(t, semgrepNewer, c.eligible)
			assert.False(t, c.actionable())
			assert.False(t, c.heldByWindow())
		})

		t.Run("pin が次のページにあっても遡って比べる", func(t *testing.T) {
			t.Parallel()
			client := upstreamStub(t, map[string]string{
				commitsPageURI(1): freshPage(1),
				commitsPageURI(2): `[` + commitJSON(semgrepNewer, daysAgo(20), "root1") + `,` +
					commitJSON(semgrepPin, daysAgo(400), "root0") + `]`,
				treesURI + "rootf": rootTreeJSON("tff"),
				treesURI + "root0": rootTreeJSON("tf0"),
				treesURI + "root1": rootTreeJSON("tf1"),
			})

			c := evaluateCommit(t.Context(), client, semgrepTool(), outdatedNow)

			require.NoError(t, c.err)
			assert.Equal(t, semgrepNewer, c.eligible)
			assert.True(t, c.actionable())
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("一覧を引けなければ err を持たせる", func(t *testing.T) {
			t.Parallel()
			c := evaluateCommit(t.Context(), upstreamStub(t, map[string]string{}), semgrepTool(), outdatedNow)

			require.ErrorIs(t, c.err, errNotFound)
			assert.False(t, c.actionable())
			assert.False(t, c.heldByWindow())
		})

		// 既定ブランチの一覧に現れない pin は、fork や未 merge の branch の commit でありうる。
		t.Run("pin が一覧に無ければ err を持たせる", func(t *testing.T) {
			t.Parallel()
			client := upstreamStub(t, map[string]string{
				commitsListURI: `[` + commitJSON(semgrepNewer, daysAgo(20), "root1") + `]`,
				"/repos/" + semgrepRulesRepo + "/commits/" + semgrepPin: commitJSON(semgrepPin, daysAgo(400), "root0"),
				treesURI + "root0": rootTreeJSON("tf0"),
				treesURI + "root1": rootTreeJSON("tf1"),
			})

			c := evaluateCommit(t.Context(), client, semgrepTool(), outdatedNow)

			require.ErrorIs(t, c.err, errCommitsExhausted)
			assert.False(t, c.actionable())
		})

		t.Run("窓を満たす commit が無ければ eligible を空のまま err を持たせる", func(t *testing.T) {
			t.Parallel()
			client := upstreamStub(t, map[string]string{
				commitsListURI: `[` + commitJSON(semgrepNewer, daysAgo(1), "root1") + `,` +
					commitJSON(semgrepPin, daysAgo(2), "root0") + `]`,
				treesURI + "root0": rootTreeJSON("tf0"),
				treesURI + "root1": rootTreeJSON("tf1"),
			})

			c := evaluateCommit(t.Context(), client, semgrepTool(), outdatedNow)

			require.ErrorIs(t, c.err, errCommitsExhausted)
			assert.Empty(t, c.eligible)
			assert.False(t, c.actionable())
		})

		t.Run("窓を満たす commit の tree を引けなければ err を持たせる", func(t *testing.T) {
			t.Parallel()
			client := upstreamStub(t, map[string]string{
				commitsListURI: `[` + commitJSON(semgrepNewest, daysAgo(2), "root2") + `,` +
					commitJSON(semgrepNewer, daysAgo(20), "root1") + `,` +
					commitJSON(semgrepPin, daysAgo(40), "root0") + `]`,
				treesURI + "root0": rootTreeJSON("tf0"),
				treesURI + "root2": rootTreeJSON("tf2"),
			})

			c := evaluateCommit(t.Context(), client, semgrepTool(), outdatedNow)

			require.ErrorIs(t, c.err, errNotFound)
			assert.False(t, c.actionable())
		})

		// 比べる先が無いまま「同じ」と読むと、報告は黙って空になる。
		t.Run("terraform の tree を引けなければ err を持たせる", func(t *testing.T) {
			t.Parallel()
			client := upstreamStub(t, map[string]string{
				commitsListURI: `[` + commitJSON(semgrepNewer, daysAgo(20), "root1") + `,` +
					commitJSON(semgrepPin, daysAgo(40), "root0") + `]`,
				treesURI + "root0": rootTreeJSON(""),
				treesURI + "root1": rootTreeJSON("tf1"),
			})

			c := evaluateCommit(t.Context(), client, semgrepTool(), outdatedNow)

			require.ErrorIs(t, c.err, errNoRulesTree)
			assert.False(t, c.actionable())
		})
	})
}

func Test_commitBody_info(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("sha と committer の日付と根の tree を写す", func(t *testing.T) {
			t.Parallel()
			var b commitBody
			b.SHA = semgrepPin
			b.Commit.Committer.Date = outdatedNow
			b.Commit.Tree.SHA = "root0"

			assert.Equal(t, commitInfo{sha: semgrepPin, at: outdatedNow, tree: "root0"}, b.info())
		})
	})
}

package main

import (
	"net/http"
	"os"
	"path/filepath"
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
	commitsListURI = "/repos/" + semgrepRulesRepo + "/commits?per_page=100"
	treesURI       = "/repos/" + semgrepRulesRepo + "/git/trees/"
)

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

		for name, content := range map[string]string{
			"代入が無い":          "SEMGREP_RULES_SHA256 := " + strings.Repeat("0", 64) + "\n",
			"空":              "",
			"39桁":            "SEMGREP_RULES_COMMIT := " + semgrepPin[:39] + "\n",
			"大文字の16進":        "SEMGREP_RULES_COMMIT := " + strings.ToUpper(semgrepPin) + "\n",
			"再帰展開の代入":        "SEMGREP_RULES_COMMIT = " + semgrepPin + "\n",
			"override 付きの代入": "override SEMGREP_RULES_COMMIT := " + semgrepPin + "\n",
			"行末のコメント":        "SEMGREP_RULES_COMMIT := " + semgrepPin + " # 固定\n",
			"変数の参照":          "SEMGREP_RULES_COMMIT := $(OTHER)\n",
			"2つの代入":          semgrepFixture + "SEMGREP_RULES_COMMIT := " + semgrepNewer + "\n",
		} {
			t.Run(name+"ならエラーにする", func(t *testing.T) {
				t.Parallel()
				_, err := parseSemgrepRules([]byte(content))
				require.ErrorIs(t, err, ErrInvalidSemgrep)
			})
		}
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
	})
}

func Test_githubCommits(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("上流の順序のまま返す", func(t *testing.T) {
			t.Parallel()
			client := upstreamStub(t, map[string]string{
				commitsListURI: `[` + commitJSON(semgrepNewer, daysAgo(1), "root1") + `,` +
					commitJSON(semgrepPin, daysAgo(30), "root0") + `]`,
			})

			got, err := githubCommits(t.Context(), client, semgrepRulesRepo)
			require.NoError(t, err)
			require.Len(t, got, 2)
			assert.Equal(t, semgrepNewer, got[0].sha)
			assert.Equal(t, "root0", got[1].tree)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("0件ならエラーにする", func(t *testing.T) {
			t.Parallel()
			client := upstreamStub(t, map[string]string{commitsListURI: `[]`})

			_, err := githubCommits(t.Context(), client, semgrepRulesRepo)
			require.ErrorIs(t, err, errNotFound)
		})

		t.Run("tree を持たない commit があればエラーにする", func(t *testing.T) {
			t.Parallel()
			client := upstreamStub(t, map[string]string{
				commitsListURI: `[` + commitJSON(semgrepNewer, daysAgo(1), "") + `]`,
			})

			_, err := githubCommits(t.Context(), client, semgrepRulesRepo)
			require.ErrorIs(t, err, errNotFound)
		})

		t.Run("上流が失敗すればエラーにする", func(t *testing.T) {
			t.Parallel()
			client := fakeUpstream(t, respondStatus(http.StatusInternalServerError))

			_, err := githubCommits(t.Context(), client, semgrepRulesRepo)
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

		t.Run("pin が一覧に無ければ commit を引いて比べる", func(t *testing.T) {
			t.Parallel()
			client := upstreamStub(t, map[string]string{
				commitsListURI: `[` + commitJSON(semgrepNewer, daysAgo(20), "root1") + `]`,
				"/repos/" + semgrepRulesRepo + "/commits/" + semgrepPin: commitJSON(semgrepPin, daysAgo(400), "root0"),
				treesURI + "root0": rootTreeJSON("tf0"),
				treesURI + "root1": rootTreeJSON("tf1"),
			})

			c := evaluateCommit(t.Context(), client, semgrepTool(), outdatedNow)

			require.NoError(t, c.err)
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

		t.Run("pin の commit を上流が知らなければ err を持たせる", func(t *testing.T) {
			t.Parallel()
			client := upstreamStub(t, map[string]string{
				commitsListURI: `[` + commitJSON(semgrepNewer, daysAgo(20), "root1") + `]`,
			})

			c := evaluateCommit(t.Context(), client, semgrepTool(), outdatedNow)

			require.ErrorIs(t, c.err, errNotFound)
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

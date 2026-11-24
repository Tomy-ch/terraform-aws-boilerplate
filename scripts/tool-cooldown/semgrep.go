package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// semgrepFile は semgrep-rules を固定する commit を宣言するファイル。版ではなく commit で
// 固定するので、窓は commit の公開時刻に掛ける。
const (
	semgrepFile      = ".makefiles/security/semgrep.mk"
	semgrepRulesRepo = "semgrep/semgrep-rules"
	semgrepRulesKey  = "github-commit:" + semgrepRulesRepo
	// semgrepRulesDir は make semgrep が展開して走査に使う唯一のディレクトリ。
	semgrepRulesDir = "terraform"

	commitsPerPage = 100
)

var (
	// ErrInvalidSemgrep は semgrep.mk から固定の commit を1つに定められない場合のエラー。
	ErrInvalidSemgrep = xerrors.New("semgrep.mk の SEMGREP_RULES_COMMIT を読めません")
	errNoRulesTree    = xerrors.New("commit に走査対象のディレクトリがありません")

	semgrepCommitLine = regexp.MustCompile(`^SEMGREP_RULES_COMMIT\s*:=\s*([0-9a-f]{40})\s*$`)
	// semgrepCommitAssign は代入の形を問わず SEMGREP_RULES_COMMIT へ値を与える行を拾う。
	// semgrepCommitLine に合わないものをエラーにして、読めない代入を読み飛ばさない。
	semgrepCommitAssign = regexp.MustCompile(`(^|\s)SEMGREP_RULES_COMMIT\s*(:{1,3}|[+?!])?=`)
)

// commitInfo は上流の commit 1つ。tree は commit の根の tree の SHA。
type commitInfo struct {
	sha  string
	at   time.Time
	tree string
}

// commitSurvey は commit で固定する宣言の棚卸し結果。moves は、その commit が pin より新しく、
// 走査対象のディレクトリの中身が pin と違うことを表す。
type commitSurvey struct {
	eligibleMoves bool
	latestMoves   bool
}

// parseSemgrepRules は semgrep.mk の `SEMGREP_RULES_COMMIT := <40桁>` を読む。代入が無い・
// 2つある・形が違うときはエラーにする（ADR-0702 決定14）。
func parseSemgrepRules(content []byte) ([]tool, error) {
	var (
		sha    string
		lineNo int
	)
	scanner := bufio.NewScanner(bytes.NewReader(content))
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		if strings.HasPrefix(strings.TrimSpace(line), "#") || !semgrepCommitAssign.MatchString(line) {
			continue
		}
		m := semgrepCommitLine.FindStringSubmatch(line)
		if m == nil {
			return nil, xerrors.Wrap(ErrInvalidSemgrep, fmt.Sprintf("%d 行目: %s", lineNo, line))
		}
		if sha != "" {
			return nil, xerrors.Wrap(ErrInvalidSemgrep, fmt.Sprintf("%d 行目: 2つ目の代入です", lineNo))
		}
		sha = m[1]
	}
	if err := scanner.Err(); err != nil {
		return nil, xerrors.Wrap(err, "読み取り")
	}
	if sha == "" {
		return nil, xerrors.Wrap(ErrInvalidSemgrep, "代入がありません")
	}

	return []tool{{key: semgrepRulesKey, version: sha}}, nil
}

// githubCommit は commit の公開時刻と根の tree を返す。公開時刻は committer の日付である。
func githubCommit(ctx context.Context, client *http.Client, repo, sha string) (commitInfo, error) {
	var body commitBody
	if err := getJSON(ctx, client, githubAPI+repo+"/commits/"+sha, &body); err != nil {
		return commitInfo{}, err
	}
	info := body.info()
	// commits/{ref} は branch 名なども受ける。求めた commit そのものでなければ時刻を採らない。
	if info.sha != sha {
		return commitInfo{}, xerrors.Wrap(errNotFound, fmt.Sprintf("%s@%s（応答は %s）", repo, sha, info.sha))
	}
	if info.at.IsZero() || info.tree == "" {
		return commitInfo{}, xerrors.Wrap(errNotFound, repo+"@"+sha+" の committer の日付か tree")
	}

	return info, nil
}

// githubCommits は既定ブランチの commit を新しい順に1ページ分返す。0件はエラーにする。
func githubCommits(ctx context.Context, client *http.Client, repo string) ([]commitInfo, error) {
	var body []commitBody
	if err := getJSON(ctx, client, fmt.Sprintf("%s%s/commits?per_page=%d", githubAPI, repo, commitsPerPage), &body); err != nil {
		return nil, err
	}
	if len(body) == 0 {
		return nil, xerrors.Wrap(errNotFound, repo+" の commit")
	}

	out := make([]commitInfo, 0, len(body))
	for _, b := range body {
		info := b.info()
		if info.sha == "" || info.at.IsZero() || info.tree == "" {
			return nil, xerrors.Wrap(errNotFound, repo+" の commit の sha・committer の日付・tree")
		}
		out = append(out, info)
	}

	return out, nil
}

// rulesTree は根の tree から走査対象のディレクトリの tree の SHA を返す。
func rulesTree(ctx context.Context, client *http.Client, repo, tree string) (string, error) {
	var body struct {
		Tree []struct {
			Path string `json:"path"`
			Type string `json:"type"`
			SHA  string `json:"sha"`
		} `json:"tree"`
	}
	if err := getJSON(ctx, client, githubAPI+repo+"/git/trees/"+tree, &body); err != nil {
		return "", err
	}
	for _, e := range body.Tree {
		if e.Path == semgrepRulesDir && e.Type == "tree" && e.SHA != "" {
			return e.SHA, nil
		}
	}

	return "", xerrors.Wrap(errNoRulesTree, repo+" の tree "+tree+" に "+semgrepRulesDir+"/")
}

// evaluateCommit は commit で固定する宣言を candidate に畳む。走査が読むのは semgrepRulesDir
// だけなので、その中身が pin と同じ commit へ動かす理由は無く、moves を立てない。
func evaluateCommit(ctx context.Context, client *http.Client, t tool, now time.Time) candidate {
	c := candidate{tool: t, window: windowFor(t.backend), commit: &commitSurvey{}}
	_, repo, _ := strings.Cut(t.backend, ":")

	commits, err := githubCommits(ctx, client, repo)
	if err != nil {
		c.err = err

		return c
	}

	pinIndex := len(commits)
	pin := commitInfo{}
	for i, ci := range commits {
		if ci.sha == t.version {
			pinIndex, pin = i, ci

			break
		}
	}
	if pin.sha == "" {
		if pin, err = githubCommit(ctx, client, repo, t.version); err != nil {
			c.err = err

			return c
		}
	}

	trees := map[string]string{}
	changes := func(ci commitInfo) (bool, error) {
		for _, tree := range []string{pin.tree, ci.tree} {
			if _, ok := trees[tree]; ok {
				continue
			}
			sha, treeErr := rulesTree(ctx, client, repo, tree)
			if treeErr != nil {
				return false, treeErr
			}
			trees[tree] = sha
		}

		return trees[pin.tree] != trees[ci.tree], nil
	}

	c.latest = commits[0].sha
	c.latestAge = ageDays(commits[0].at, now)
	if pinIndex > 0 {
		if c.commit.latestMoves, err = changes(commits[0]); err != nil {
			c.err = err

			return c
		}
	}

	for i, ci := range commits {
		if ageDays(ci.at, now) < c.window {
			continue
		}
		c.eligible = ci.sha
		c.eligibleAge = ageDays(ci.at, now)
		if i < pinIndex {
			if c.commit.eligibleMoves, err = changes(ci); err != nil {
				c.err = err

				return c
			}
		}

		break
	}

	return c
}

type commitBody struct {
	SHA    string `json:"sha"`
	Commit struct {
		Committer struct {
			Date time.Time `json:"date"`
		} `json:"committer"`
		Tree struct {
			SHA string `json:"sha"`
		} `json:"tree"`
	} `json:"commit"`
}

func (b commitBody) info() commitInfo {
	return commitInfo{sha: b.SHA, at: b.Commit.Committer.Date, tree: b.Commit.Tree.SHA}
}

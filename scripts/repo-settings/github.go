package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// GitHub の上限。
const perPage = 100

var (
	errHTTP               = xerrors.New("GitHub API が失敗を返しました")
	errAmbiguousRuleset   = xerrors.New("同名の ruleset が複数あります")
	errShortPage          = xerrors.New("総数に届く前にページが尽きました")
	errUnexpectedResponse = xerrors.New("GitHub API の応答が想定した形ではありません")
	errRequest            = xerrors.New("GitHub API へ要求できませんでした")
	// errNotFound は 404 の応答。errHTTP の一種なので、errHTTP で判定する呼び出し側はそのまま落ちる。
	errNotFound   = xerrors.Wrap(errHTTP, "404")
	errNoRulesets = xerrors.New("突合する ruleset の宣言がありません")
)

// githubClient は GitHub REST API の呼び出し。base はテストで差し替える。
type githubClient struct {
	base  string
	token string
	http  *http.Client
}

// do は method で path を呼び、body を JSON で送り、2xx の応答を out へ読みます（out が nil なら読まない）。
func (c githubClient) do(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return xerrors.Wrapf(errRequest, "%s %s の body: %v", method, path, err)
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return xerrors.Wrapf(errRequest, "%s %s: %v", method, path, err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return xerrors.Wrapf(errRequest, "%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return xerrors.Wrapf(errRequest, "%s %s の応答の読み取り: %v", method, path, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		const limit = 300
		excerpt := strings.TrimSpace(string(data))
		if len(excerpt) > limit {
			excerpt = excerpt[:limit] + "..."
		}
		cause := errHTTP
		if resp.StatusCode == http.StatusNotFound {
			cause = errNotFound
		}
		return xerrors.Wrapf(cause, "%s %s: %d %s", method, path, resp.StatusCode, excerpt)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return xerrors.Wrapf(errUnexpectedResponse, "%s %s の応答の解釈: %v", method, path, err)
	}
	return nil
}

func pagedPath(path string, page int) string {
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return fmt.Sprintf("%s%sper_page=%d&page=%d", path, sep, perPage, page)
}

// fetchLive は、名前が names の ruleset と environment の実態を読みます。GET だけを使います。
func fetchLive(ctx context.Context, c githubClient, repo string, names []string) (live, error) {
	rulesets, undeclared, err := fetchRulesets(ctx, c, repo, names)
	if err != nil {
		return live{}, err
	}
	envs, err := fetchEnvironments(ctx, c, repo)
	if err != nil {
		return live{}, err
	}
	return live{Rulesets: rulesets, UndeclaredRulesets: undeclared, Envs: envs}, nil
}

// fetchRulesets は、リポジトリが持つ ruleset のうち名前が names のものを読み、names に無い名前を昇順で返します。
// 実在しない名前は戻り値の map に持ちません。names が空なら何も問わずに失敗します —— 突合する ruleset が無いまま
// 一致と報せない。
func fetchRulesets(ctx context.Context, c githubClient, repo string, names []string) (map[string]liveRuleset, []string, error) {
	if len(names) == 0 {
		return nil, nil, errNoRulesets
	}
	declared := map[string]bool{}
	for _, n := range names {
		declared[n] = true
	}
	ids := map[string][]int64{}
	undeclared := map[string]bool{}
	for page := 1; ; page++ {
		var list []struct {
			ID         int64  `json:"id"`
			Name       string `json:"name"`
			SourceType string `json:"source_type"`
		}
		if err := c.do(ctx, http.MethodGet, pagedPath("/repos/"+repo+"/rulesets?includes_parents=false", page), nil, &list); err != nil {
			return nil, nil, err
		}
		for _, r := range list {
			// source_type が無い要素を数えずに進むと、同名の ruleset を見落として重複して作る。
			if r.SourceType == "" {
				return nil, nil, xerrors.Wrapf(errUnexpectedResponse, "ruleset %d に source_type がありません", r.ID)
			}
			if r.SourceType != "Repository" {
				continue
			}
			if declared[r.Name] {
				ids[r.Name] = append(ids[r.Name], r.ID)
			} else {
				undeclared[r.Name] = true
			}
		}
		if len(list) < perPage {
			break
		}
	}
	for _, n := range names {
		if len(ids[n]) > 1 {
			return nil, nil, xerrors.Wrapf(errAmbiguousRuleset, "%s: %d 件", n, len(ids[n]))
		}
	}
	out := map[string]liveRuleset{}
	for _, n := range names {
		if len(ids[n]) == 0 {
			continue
		}
		id := ids[n][0]
		var body map[string]any
		if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/rulesets/%d", repo, id), nil, &body); err != nil {
			return nil, nil, err
		}
		if len(body) == 0 {
			return nil, nil, xerrors.Wrapf(errUnexpectedResponse, "ruleset %d の本体が空です", id)
		}
		out[n] = liveRuleset{ID: id, Body: body}
	}
	extra := make([]string, 0, len(undeclared))
	for n := range undeclared {
		extra = append(extra, n)
	}
	sort.Strings(extra)
	return out, extra, nil
}

const (
	visibilityPublic   = "public"
	visibilityPrivate  = "private"
	visibilityInternal = "internal"
)

func vulnerabilityReportingPath(repo string) string {
	return "/repos/" + repo + "/private-vulnerability-reporting"
}

// fetchVulnerabilityReporting は Private Vulnerability Reporting の実態を読みます。GET だけを使います。
// 公開でないリポジトリでは PVR が提供されないので、可視性だけを返し、PVR を問いません。
func fetchVulnerabilityReporting(ctx context.Context, c githubClient, repo string) (liveVulnerabilityReporting, error) {
	var meta struct {
		Visibility string `json:"visibility"`
	}
	if err := c.do(ctx, http.MethodGet, "/repos/"+repo, nil, &meta); err != nil {
		return liveVulnerabilityReporting{}, err
	}
	switch meta.Visibility {
	case visibilityPublic:
	case visibilityPrivate, visibilityInternal:
		return liveVulnerabilityReporting{}, nil
	default:
		return liveVulnerabilityReporting{}, xerrors.Wrapf(errUnexpectedResponse, "%s の visibility %q", repo, meta.Visibility)
	}
	var resp struct {
		Enabled *bool `json:"enabled"`
	}
	if err := c.do(ctx, http.MethodGet, vulnerabilityReportingPath(repo), nil, &resp); err != nil {
		return liveVulnerabilityReporting{}, err
	}
	if resp.Enabled == nil {
		return liveVulnerabilityReporting{}, xerrors.Wrapf(errUnexpectedResponse, "%s の Private Vulnerability Reporting に enabled がありません", repo)
	}
	return liveVulnerabilityReporting{Public: true, Enabled: *resp.Enabled}, nil
}

func fetchEnvironments(ctx context.Context, c githubClient, repo string) (map[string]liveEnv, error) {
	type rule struct {
		Type      string `json:"type"`
		WaitTimer int    `json:"wait_timer"`
		Reviewers []any  `json:"reviewers"`
	}
	type env struct {
		Name            string        `json:"name"`
		CanAdminsBypass *bool         `json:"can_admins_bypass"`
		ProtectionRules *[]rule       `json:"protection_rules"`
		Policy          *branchPolicy `json:"deployment_branch_policy"`
	}
	var all []env
	for page := 1; ; page++ {
		var resp struct {
			TotalCount   *int  `json:"total_count"`
			Environments []env `json:"environments"`
		}
		if err := c.do(ctx, http.MethodGet, pagedPath("/repos/"+repo+"/environments", page), nil, &resp); err != nil {
			return nil, err
		}
		all = append(all, resp.Environments...)
		done, err := pageDone(len(all), len(resp.Environments), resp.TotalCount)
		if err != nil {
			return nil, xerrors.Wrapf(err, "environments")
		}
		if done {
			break
		}
	}
	out := make(map[string]liveEnv, len(all))
	for _, e := range all {
		if _, dup := out[e.Name]; dup {
			return nil, xerrors.Wrapf(errUnexpectedResponse, "environment %q が2回現れました", e.Name)
		}
		le := liveEnv{CanAdminsBypass: e.CanAdminsBypass, Policy: e.Policy, RulesMissing: e.ProtectionRules == nil}
		var rules []rule
		if e.ProtectionRules != nil {
			rules = *e.ProtectionRules
		}
		for _, r := range rules {
			switch r.Type {
			case "required_reviewers":
				le.Reviewers += len(r.Reviewers)
			case "wait_timer":
				le.WaitTimer = r.WaitTimer
			}
		}
		// policy を持たない environment に一覧を問うと 404 になる。custom の時だけ読む。
		if e.Policy != nil && e.Policy.CustomBranchPolicies {
			patterns, err := fetchBranchPolicies(ctx, c, repo, e.Name)
			if err != nil {
				return nil, err
			}
			le.Patterns = patterns
		}
		out[e.Name] = le
	}
	return out, nil
}

func fetchBranchPolicies(ctx context.Context, c githubClient, repo, name string) (map[string]int64, error) {
	out := map[string]int64{}
	count := 0
	for page := 1; ; page++ {
		var resp struct {
			TotalCount *int `json:"total_count"`
			Policies   []struct {
				ID   int64  `json:"id"`
				Name string `json:"name"`
				Type string `json:"type"`
			} `json:"branch_policies"`
		}
		path := pagedPath("/repos/"+repo+"/environments/"+url.PathEscape(name)+"/deployment-branch-policies", page)
		if err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
			return nil, err
		}
		for _, p := range resp.Policies {
			key := p.Name
			if p.Type != "branch" {
				key = p.Type + ":" + p.Name
			}
			if _, dup := out[key]; dup {
				return nil, xerrors.Wrapf(errUnexpectedResponse, "%s の deployment branch policy %q が2回現れました", name, key)
			}
			out[key] = p.ID
		}
		count += len(resp.Policies)
		done, err := pageDone(count, len(resp.Policies), resp.TotalCount)
		if err != nil {
			return nil, xerrors.Wrapf(err, "%s の deployment branch policy", name)
		}
		if done {
			return out, nil
		}
	}
}

// pageDone は、total_count を持つ一覧のページ送りを続けるかを決めます。総数が無い、集めた件数が総数を
// 超えた、総数に届く前にページが空になった —— どれも1ページ目だけを見て緑にしないためにエラーにする。
func pageDone(collected, onPage int, total *int) (bool, error) {
	switch {
	case total == nil:
		return false, xerrors.Wrap(errUnexpectedResponse, "total_count がありません")
	case collected > *total:
		return false, xerrors.Wrapf(errUnexpectedResponse, "%d 件を集めましたが total_count は %d です", collected, *total)
	case collected == *total:
		return true, nil
	case onPage == 0:
		return false, xerrors.Wrapf(errShortPage, "%d / %d", collected, *total)
	default:
		return false, nil
	}
}

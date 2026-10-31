package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
		return xerrors.Wrapf(errHTTP, "%s %s: %d %s", method, path, resp.StatusCode, excerpt)
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

// fetchLive は ruleset を rc で、environment を ec で読みます。GET だけを使います。
func fetchLive(ctx context.Context, rc, ec githubClient, repo, rulesetName string) (live, error) {
	var got live
	ruleset, id, err := fetchRuleset(ctx, rc, repo, rulesetName)
	if err != nil {
		return live{}, err
	}
	got.Ruleset, got.RulesetID = ruleset, id
	envs, err := fetchEnvironments(ctx, ec, repo)
	if err != nil {
		return live{}, err
	}
	got.Envs = envs
	return got, nil
}

// fetchRuleset は、名前が rulesetName でリポジトリが持つ ruleset を読みます。無ければ nil を返します。
func fetchRuleset(ctx context.Context, c githubClient, repo, rulesetName string) (map[string]any, int64, error) {
	var ids []int64
	for page := 1; ; page++ {
		var list []struct {
			ID         int64  `json:"id"`
			Name       string `json:"name"`
			SourceType string `json:"source_type"`
		}
		if err := c.do(ctx, http.MethodGet, pagedPath("/repos/"+repo+"/rulesets?includes_parents=false", page), nil, &list); err != nil {
			return nil, 0, err
		}
		for _, r := range list {
			// source_type が無い要素を数えずに進むと、同名の ruleset を見落として重複して作る。
			if r.SourceType == "" {
				return nil, 0, xerrors.Wrapf(errUnexpectedResponse, "ruleset %d に source_type がありません", r.ID)
			}
			if r.Name == rulesetName && r.SourceType == "Repository" {
				ids = append(ids, r.ID)
			}
		}
		if len(list) < perPage {
			break
		}
	}
	switch len(ids) {
	case 0:
		return nil, 0, nil
	case 1:
	default:
		return nil, 0, xerrors.Wrapf(errAmbiguousRuleset, "%s: %d 件", rulesetName, len(ids))
	}
	var ruleset map[string]any
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/repos/%s/rulesets/%d", repo, ids[0]), nil, &ruleset); err != nil {
		return nil, 0, err
	}
	if len(ruleset) == 0 {
		return nil, 0, xerrors.Wrapf(errUnexpectedResponse, "ruleset %d の本体が空です", ids[0])
	}
	return ruleset, ids[0], nil
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

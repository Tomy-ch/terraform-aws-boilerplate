package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/branchdecl"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// maxLabelPages は、ラベルの一覧を読むページの上限です。GET /labels は総数を返さないので、ページが尽きたことは
// per_page に満たないページで判定し、尽きないまま上限に届けば取りこぼしとして落とします。
const maxLabelPages = 50

var (
	errLabelsDecl   = xerrors.New("labels.json がラベルの宣言として正しくありません")
	errNoWorkflows  = xerrors.New("workflow が1つも見つかりません")
	errEnable       = xerrors.New("有効化できなかった workflow があります")
	errLabelPages   = xerrors.New("ラベルの一覧がページの上限までに尽きませんでした")
	errDeployEnvArg = xerrors.New("DEPLOY_ENV を指定してください（例: DEPLOY_ENV=admin）")
)

var (
	labelColor = regexp.MustCompile(`^[0-9a-fA-F]{6}$`)
	objectSHA  = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)
)

// labelDecl は labels.json の1件です。
type labelDecl struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Color       string `json:"color"`
}

// readLabels は labels.json を読み、[parseLabels] で検めます。
func readLabels(root string) ([]labelDecl, error) {
	raw, err := os.ReadFile(filepath.Clean(filepath.Join(root, labelsFile)))
	if err != nil {
		return nil, xerrors.Wrapf(err, "%s の読み取り", labelsFile)
	}

	return parseLabels(raw)
}

// parseLabels は labels.json の中身を宣言として検めます。配列として読めない・null は errLabelsParse、0件、name が
// 空（空白だけを含む）、color が6桁の16進でない、名前が大文字小文字を区別せずに重複する、のどれも errLabelsDecl です。description は
// 省けます。make repo-settings-check と、ラベルを書き換える前の setup が同じこの検査を通ります。
func parseLabels(raw []byte) ([]labelDecl, error) {
	var decl []labelDecl
	if err := json.Unmarshal(raw, &decl); err != nil {
		return nil, xerrors.Wrapf(errLabelsParse, "%s: %v", labelsFile, err)
	}
	if decl == nil {
		return nil, xerrors.Wrapf(errLabelsParse, "%s が null です", labelsFile)
	}
	if len(decl) == 0 {
		return nil, xerrors.Wrapf(errLabelsDecl, "%s にラベルが1件もありません", labelsFile)
	}
	seen := map[string]bool{}
	for i, l := range decl {
		switch {
		case strings.TrimSpace(l.Name) == "":
			return nil, xerrors.Wrapf(errLabelsDecl, "%s の %d 番目の要素に name がありません", labelsFile, i+1)
		case !labelColor.MatchString(l.Color):
			return nil, xerrors.Wrapf(errLabelsDecl, "%q の color %q は6桁の16進ではありません", l.Name, l.Color)
		case seen[strings.ToLower(l.Name)]:
			return nil, xerrors.Wrapf(errLabelsDecl, "%q が2回あります（GitHub は名前の大文字小文字を区別しません）", l.Name)
		}
		seen[strings.ToLower(l.Name)] = true
	}

	return decl, nil
}

func listLabels(ctx context.Context, c githubClient, repo string) ([]string, error) {
	var names []string
	for page := 1; page <= maxLabelPages; page++ {
		var list []struct {
			Name string `json:"name"`
		}
		if err := c.do(ctx, http.MethodGet, pagedPath("/repos/"+repo+"/labels", page), nil, &list); err != nil {
			return nil, err
		}
		for _, l := range list {
			if l.Name == "" {
				return nil, xerrors.Wrap(errUnexpectedResponse, "name の無いラベルがあります")
			}
			names = append(names, l.Name)
		}
		if len(list) < perPage {
			return names, nil
		}
	}

	return nil, xerrors.Wrapf(errLabelPages, "%d ページ", maxLabelPages)
}

func labelPath(repo, name string) string {
	return "/repos/" + repo + "/labels/" + url.PathEscape(name)
}

// deleteLabels は、labels.json が宣言として正しいことを確かめてから消します（後の create-labels が宣言で落ちて、
// ラベルが0件のまま残らないように）。1件でも失敗すれば残りを消さずに止めます。
func deleteLabels(ctx context.Context, root string, c githubClient, repo string, out io.Writer) error {
	if _, err := readLabels(root); err != nil {
		return err
	}
	fmt.Fprintln(out, "🗑 既存のラベルを削除します...")
	names, err := listLabels(ctx, c, repo)
	if err != nil {
		return err
	}
	for _, name := range names {
		fmt.Fprintf(out, "🔸 delete label: %s\n", name)
		if err := c.do(ctx, http.MethodDelete, labelPath(repo, name), nil, nil); err != nil {
			return err
		}
	}
	if len(names) == 0 {
		fmt.Fprintln(out, "🟡 削除するラベルはありませんでした")

		return nil
	}
	fmt.Fprintf(out, "✅ ラベルを %d 件削除しました\n", len(names))

	return nil
}

// createLabels は、labels.json のラベルを作るか、同じ名前（大文字小文字を区別しない）のものがあれば宣言へ
// 書き換えます。1件でも失敗すれば、残りを書かずに止めます。
func createLabels(ctx context.Context, root string, c githubClient, repo string, out io.Writer) error {
	decl, err := readLabels(root)
	if err != nil {
		return err
	}
	fmt.Fprintln(out, "🏷 ラベルを作成します...")
	names, err := listLabels(ctx, c, repo)
	if err != nil {
		return err
	}
	existing := map[string]string{}
	for _, n := range names {
		existing[strings.ToLower(n)] = n
	}
	for _, l := range decl {
		fmt.Fprintf(out, "🔸 upsert label: %s\n", l.Name)
		current, ok := existing[strings.ToLower(l.Name)]
		if !ok {
			body := map[string]string{"name": l.Name, "color": l.Color, "description": l.Description}
			if err := c.do(ctx, http.MethodPost, "/repos/"+repo+"/labels", body, nil); err != nil {
				return err
			}
			continue
		}
		body := map[string]string{"new_name": l.Name, "color": l.Color, "description": l.Description}
		if err := c.do(ctx, http.MethodPatch, labelPath(repo, current), body, nil); err != nil {
			return err
		}
	}
	fmt.Fprintf(out, "✅ ラベルを %d 件そろえました\n", len(decl))

	return nil
}

type workflowState struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	State string `json:"state"`
}

// listWorkflows は、集めた workflow の数を total_count と突き合わせます。
func listWorkflows(ctx context.Context, c githubClient, repo string) ([]workflowState, error) {
	var all []workflowState
	for page := 1; ; page++ {
		var resp struct {
			TotalCount *int            `json:"total_count"`
			Workflows  []workflowState `json:"workflows"`
		}
		if err := c.do(ctx, http.MethodGet, pagedPath("/repos/"+repo+"/actions/workflows", page), nil, &resp); err != nil {
			return nil, err
		}
		for _, w := range resp.Workflows {
			if w.ID == 0 || w.State == "" {
				return nil, xerrors.Wrapf(errUnexpectedResponse, "workflow %q に id か state がありません", w.Name)
			}
		}
		all = append(all, resp.Workflows...)
		done, err := pageDone(len(all), len(resp.Workflows), resp.TotalCount)
		if err != nil {
			return nil, xerrors.Wrapf(err, "workflows")
		}
		if done {
			return all, nil
		}
	}
}

// enableWorkflows は、disabled_fork の workflow をすべて有効にします。人が止めたもの（disabled_manually など）には
// 触りません。workflow が1つも無ければ errNoWorkflows、有効化の失敗は全件を試してから名指して errEnable です。
func enableWorkflows(ctx context.Context, c githubClient, repo string, out io.Writer) error {
	all, err := listWorkflows(ctx, c, repo)
	if err != nil {
		return err
	}
	if len(all) == 0 {
		return xerrors.Wrapf(errNoWorkflows, "%s", repo)
	}
	var enabled int
	var failed []string
	for _, w := range all {
		if w.State != "disabled_fork" {
			continue
		}
		fmt.Fprintf(out, "  有効化: %s\n", w.Name)
		path := "/repos/" + repo + "/actions/workflows/" + strconv.FormatInt(w.ID, 10) + "/enable"
		if err := c.do(ctx, http.MethodPut, path, nil, nil); err != nil {
			fmt.Fprintf(out, "  ⚠️ 有効化に失敗: %s: %v\n", w.Name, err)
			failed = append(failed, w.Name)
			continue
		}
		enabled++
	}
	if len(failed) > 0 {
		return xerrors.Wrapf(errEnable, "%s", strings.Join(failed, ", "))
	}
	if enabled == 0 {
		fmt.Fprintf(out, "🟡 disabled_fork 状態のワークフローはありません（workflow %d 件）。\n", len(all))

		return nil
	}
	fmt.Fprintf(out, "✅ ワークフローを %d 件有効化しました。\n", enabled)

	return nil
}

// createDeployBranch は、env のデプロイブランチを、デフォルトブランチの先頭から作ります。既に在れば GitHub が
// 拒むので、上書きしません。
func createDeployBranch(ctx context.Context, root string, c githubClient, repo, env string, out io.Writer) error {
	b, err := branchdecl.Read(filepath.Join(root, branchesFile))
	if err != nil {
		return err
	}
	d, ok := b.DeployForEnv(env)
	if !ok {
		return xerrors.Wrapf(errNoDeployEnv, "%q", env)
	}
	var meta struct {
		DefaultBranch string `json:"default_branch"`
	}
	if err := c.do(ctx, http.MethodGet, "/repos/"+repo, nil, &meta); err != nil {
		return err
	}
	if meta.DefaultBranch == "" {
		return xerrors.Wrapf(errUnexpectedResponse, "%s に default_branch がありません", repo)
	}
	var ref struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := c.do(ctx, http.MethodGet, "/repos/"+repo+"/git/ref/heads/"+escapeSegments(meta.DefaultBranch), nil, &ref); err != nil {
		return err
	}
	if !objectSHA.MatchString(ref.Object.SHA) {
		return xerrors.Wrapf(errUnexpectedResponse, "%s の先頭の sha %q", meta.DefaultBranch, ref.Object.SHA)
	}
	body := map[string]string{"ref": "refs/heads/" + d.Branch, "sha": ref.Object.SHA}
	if err := c.do(ctx, http.MethodPost, "/repos/"+repo+"/git/refs", body, nil); err != nil {
		return err
	}
	fmt.Fprintf(out, "✅ %s を %s の先頭 %s から作りました\n", d.Branch, meta.DefaultBranch, ref.Object.SHA)

	return nil
}

// escapeSegments は、ブランチの名前を `/` で区切った区間ごとに URL のパスへエスケープします。`/` を %2F にすると
// GitHub は ref を見つけません。
func escapeSegments(name string) string {
	parts := strings.Split(name, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}

	return strings.Join(parts, "/")
}

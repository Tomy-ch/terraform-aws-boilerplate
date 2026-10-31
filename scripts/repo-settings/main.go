// repo-settings は、リポジトリの保護設定 —— ブランチの ruleset とデプロイの environment —— を、
// 宣言から生成・適用し、実態と突き合わせます（ADR-0603 決定1-7、ADR-0702 決定10-12）。
//
// 宣言は2つです。
//
//	.github/settings/branches.json      分岐のパターンの単一宣言
//	.github/settings/environments.json  environment の名前と、どのブランチから使えるか
//
// ruleset の宣言 .github/settings/branch-protection.json は、include の区間だけを branches.json から
// 生成します。environment のパターンはファイルへ書き出さず、適用と突合のたびに展開します。
//
// サブコマンド:
//
//	apply  include の区間を書き換える（ネットワークを使わない）
//	check  apply と同じ判定で、書き換えずにずれを報せる。宣言の Static Analysis も見る
//	drift  実態を GET で読み、宣言との差分を報せる
//	sync   drift と同じ差分を、GitHub への書き込みで解消する（管理者の資格情報を要する）
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/atomicwrite"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const (
	toolName         = "repo-settings"
	branchesFile     = ".github/settings/branches.json"
	environmentsFile = ".github/settings/environments.json"
	rulesetFile      = ".github/settings/branch-protection.json"
	defaultAPI       = "https://api.github.com"
	filePerm         = 0o644
	requestTimeout   = 2 * time.Minute
)

var (
	errUsage        = xerrors.New("usage: repo-settings <apply|check|drift|sync> [-repo owner/name] [-only ruleset|environments]")
	errIncludeDrift = xerrors.New("branch-protection.json の include が branches.json からずれています（make repo-settings-apply）")
	errDrift        = xerrors.New("宣言と実態がずれています")
	errNoToken      = xerrors.New("GITHUB_TOKEN がありません")
	errNoRepo       = xerrors.New("対象のリポジトリを解決できません（-repo、GITHUB_REPOSITORY、gh repo view のいずれも空です）")
)

func main() {
	log.SetFlags(0)
	client := &http.Client{Timeout: requestTimeout}
	if err := run(os.Args[1:], os.Getwd, githubClient{base: defaultAPI, token: os.Getenv("GITHUB_TOKEN"), http: client}, os.Stdout); err != nil {
		log.Fatalf("❌ %s: %v", toolName, err)
	}
}

// run は引数を解釈してサブコマンドを呼びます。依存はすべて引数で受け取ります（scripts/README.md の Test Strategy）。
func run(args []string, wd func() (string, error), c githubClient, out io.Writer) error {
	if len(args) == 0 {
		return errUsage
	}
	fs := flag.NewFlagSet(toolName, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repo := fs.String("repo", "", "対象のリポジトリ（owner/name）")
	only := fs.String("only", "", "sync の対象（ruleset か environments）")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 {
		return errUsage
	}
	root, err := wd()
	if err != nil {
		return xerrors.Wrap(err, "作業ディレクトリの取得")
	}
	ctx := context.Background()
	switch args[0] {
	case "apply":
		return applyOrCheck(root, false, out)
	case "check":
		return applyOrCheck(root, true, out)
	case "drift":
		if *repo == "" {
			return errNoRepo
		}
		return drift(ctx, root, c, *repo, out)
	case "sync":
		if *only != onlyRuleset && *only != onlyEnvironments {
			return errUsage
		}
		if *repo == "" {
			return errNoRepo
		}
		if c.token == "" {
			return errNoToken
		}
		return syncSettings(ctx, root, c, *repo, *only, out)
	default:
		return errUsage
	}
}

// declarations は、検証を通った3つの宣言と、include の区間を生成し直した保護設定の宣言。
type declarations struct {
	branches     branches
	environments []environment
	ruleset      []byte
	rendered     []byte
}

// readDeclarations は3つの宣言を読み、検証し、include の区間を生成し直します。書き込みはしません。
func readDeclarations(root string) (declarations, error) {
	b, err := readBranches(filepath.Join(root, branchesFile))
	if err != nil {
		return declarations{}, err
	}
	envs, err := readEnvironments(filepath.Join(root, environmentsFile))
	if err != nil {
		return declarations{}, err
	}
	identity, err := identityEnvs(root)
	if err != nil {
		return declarations{}, err
	}
	if err := checkIdentityEnvironments(identity, envs); err != nil {
		return declarations{}, xerrors.Wrapf(err, "%s", environmentsFile)
	}
	src, err := os.ReadFile(filepath.Clean(filepath.Join(root, rulesetFile)))
	if err != nil {
		return declarations{}, xerrors.Wrapf(err, "%s の読み取り", rulesetFile)
	}
	if err := checkRuleset(src); err != nil {
		return declarations{}, xerrors.Wrapf(err, "%s", rulesetFile)
	}
	includes := rulesetIncludes(b)
	rendered, err := renderInclude(src, includes)
	if err != nil {
		return declarations{}, xerrors.Wrapf(err, "%s", rulesetFile)
	}
	if err := verifyInclude(rendered, includes); err != nil {
		return declarations{}, xerrors.Wrapf(err, "%s", rulesetFile)
	}
	return declarations{branches: b, environments: envs, ruleset: src, rendered: rendered}, nil
}

// loadDesired は宣言からあるべき実態を組みます。include の区間がずれていればエラーにします
// —— ずれた宣言を GitHub と比べても、どちらが正しいかを答えられない。
func loadDesired(root string) (desired, error) {
	decl, err := readDeclarations(root)
	if err != nil {
		return desired{}, err
	}
	if string(decl.rendered) != string(decl.ruleset) {
		return desired{}, errIncludeDrift
	}
	var ruleset map[string]any
	if err := json.Unmarshal(decl.ruleset, &ruleset); err != nil {
		return desired{}, xerrors.Wrapf(err, "%s の解釈", rulesetFile)
	}
	want := desired{Ruleset: ruleset}
	for _, e := range decl.environments {
		want.Envs = append(want.Envs, desiredEnv{Name: e.Name, Patterns: environmentPatterns(decl.branches, e.DeployFrom)})
	}
	return want, nil
}

// applyOrCheck は include の区間を branches.json から生成します。dryRun が真なら書かず、ずれを報せます。
// 書き込みは、3つの宣言の検証がすべて通った後です。
func applyOrCheck(root string, dryRun bool, out io.Writer) error {
	decl, err := readDeclarations(root)
	if err != nil {
		return err
	}
	summary := fmt.Sprintf("保護対象 %d 件、environment %d 件", len(rulesetIncludes(decl.branches)), len(decl.environments))
	if string(decl.rendered) == string(decl.ruleset) {
		fmt.Fprintf(out, "✅ %s: %s。%s は宣言どおりです\n", toolName, summary, rulesetFile)
		return nil
	}
	if dryRun {
		return errIncludeDrift
	}
	path := filepath.Join(root, rulesetFile)
	if err := atomicwrite.Apply(map[string]string{path: string(decl.rendered)}, filePerm); err != nil {
		return xerrors.Wrapf(err, "%s の書き込み", rulesetFile)
	}
	fmt.Fprintf(out, "✅ %s: %s。%s の include を書き換えました\n", toolName, summary, rulesetFile)
	return nil
}

// drift は実態を読み、宣言との差分を報せます。書き込みはしません。
func drift(ctx context.Context, root string, c githubClient, repo string, out io.Writer) error {
	want, got, d, err := observe(ctx, root, c, repo)
	if err != nil {
		return err
	}
	for _, u := range d.Unobserved {
		fmt.Fprintf(out, "⚠️ 突合の対象外（観測できない）: %s\n", u)
	}
	findings := d.findings()
	if len(findings) > 0 {
		fmt.Fprintf(out, "❌ %s: %s で宣言と実態のずれが %d 件あります\n", toolName, repo, len(findings))
		for _, f := range findings {
			fmt.Fprintf(out, "  - %s\n", f)
		}
		return xerrors.Wrapf(errDrift, "%d 件", len(findings))
	}
	excluded := ""
	if len(d.Unobserved) > 0 {
		excluded = fmt.Sprintf("（突合の対象外 %d 件を除く）", len(d.Unobserved))
	}
	fmt.Fprintf(out, "✅ %s: %s の ruleset 1 件と environment %d 件（実在 %d 件）が宣言どおりです%s\n",
		toolName, repo, len(want.Envs), len(got.Envs), excluded)
	return nil
}

// syncSettings は drift と同じ差分を、only が指す側について GitHub への書き込みで解消します。
// 要求はすべて組んでから送ります。宣言に無い environment は消しません。
func syncSettings(ctx context.Context, root string, c githubClient, repo, only string, out io.Writer) error {
	want, _, d, err := observe(ctx, root, c, repo)
	if err != nil {
		return err
	}
	reqs := planRequests(d, want, repo, only)
	for _, r := range reqs {
		if err := c.do(ctx, r.Method, r.Path, r.Body, nil); err != nil {
			return err
		}
		fmt.Fprintf(out, "🔧 %s %s\n", r.Method, r.Path)
	}
	if only == onlyEnvironments && len(d.Extra) > 0 {
		fmt.Fprintf(out, "⚠️ 宣言に無い environment は消していません（make repo-settings-drift はこれをずれとして報せます）: %s\n", strings.Join(d.Extra, ", "))
	}
	if len(reqs) == 0 {
		fmt.Fprintf(out, "✅ %s: %s の %s へ送る要求はありません\n", toolName, repo, only)
		return nil
	}
	fmt.Fprintf(out, "✅ %s: %s へ %d 件の要求を送りました。make repo-settings-drift で結果を確かめてください\n", toolName, repo, len(reqs))
	return nil
}

func observe(ctx context.Context, root string, c githubClient, repo string) (desired, live, diff, error) {
	want, err := loadDesired(root)
	if err != nil {
		return desired{}, live{}, diff{}, err
	}
	name, _ := want.Ruleset["name"].(string)
	got, err := fetchLive(ctx, c, repo, name)
	if err != nil {
		return desired{}, live{}, diff{}, err
	}
	return want, got, computeDiff(want, got), nil
}

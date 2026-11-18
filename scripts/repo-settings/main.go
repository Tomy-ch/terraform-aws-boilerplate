// repo-settings は、リポジトリの保護設定 —— ブランチの ruleset とデプロイの environment —— を、
// 宣言から生成・適用し、実態と突き合わせます（ADR-0603 決定1-7、ADR-0702 決定10-12）。
//
// 宣言は2つです。
//
//	.github/settings/branches.json      分岐のパターンの単一宣言
//	.github/settings/environments.json  environment の名前と、どのブランチから使えるか
//
// ruleset の宣言 .github/settings/branch-protection.json は、include の区間だけを branches.json から
// 生成します。apply の environment を使う workflow は、on.push.branches をその env のデプロイブランチへ
// 生成します。environment のパターンはファイルへ書き出さず、適用と突合のたびに展開します。
//
// サブコマンド:
//
//	apply  include の区間と apply の workflow の起動条件を書き換える（ネットワークを使わない）
//	check  apply と同じ判定で、書き換えずにずれを報せる。宣言の Static Analysis も見る
//	drift  実態を GET で読み、宣言との差分を報せる
//	sync   drift と同じ差分を、GitHub への書き込みで解消する（管理者の資格情報を要する）
//	source デプロイブランチへ入る Pull Request の head を検査する（ADR-0603 決定31。ネットワークを使わない）
//	deploy-branch  env のデプロイブランチの名前を出す
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
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/branchdecl"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const (
	toolName         = "repo-settings"
	branchesFile     = branchdecl.File
	environmentsFile = ".github/settings/environments.json"
	rulesetFile      = ".github/settings/branch-protection.json"
	defaultAPI       = "https://api.github.com"
	filePerm         = 0o644
	requestTimeout   = 2 * time.Minute
)

var (
	errUsage        = xerrors.New("usage: repo-settings <apply|check|drift|sync|source|deploy-branch> [-repo owner/name] [-only ruleset|environments] [-base <ref> -base-sha <sha> -head-sha <sha> -default <branch>] [-env <env>]")
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
	base := fs.String("base", "", "Pull Request の base のブランチ")
	baseSHA := fs.String("base-sha", "", "Pull Request の base の commit")
	headSHA := fs.String("head-sha", "", "Pull Request の head の commit")
	def := fs.String("default", "", "デフォルトブランチ")
	env := fs.String("env", "", "env の token")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 {
		return errUsage
	}
	allowed := map[string]map[string]bool{
		"apply": {}, "check": {}, "drift": {"repo": true}, "sync": {"repo": true, "only": true},
		"source":        {"base": true, "base-sha": true, "head-sha": true, "default": true},
		"deploy-branch": {"env": true},
	}
	unused := false
	fs.Visit(func(f *flag.Flag) {
		if !allowed[args[0]][f.Name] {
			unused = true
		}
	})
	if unused {
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
	case "source":
		if *base == "" {
			return errUsage
		}
		return checkSource(root, sourceRequest{base: *base, baseSHA: *baseSHA, headSHA: *headSHA, def: *def}, realGit, out)
	case "deploy-branch":
		if *env == "" {
			return errUsage
		}
		return deployBranch(root, *env, out)
	default:
		return errUsage
	}
}

// declarations は、検証を通った3つの宣言と、include の区間を生成し直した保護設定の宣言と、起動条件を
// 生成し直した apply の workflow。
type declarations struct {
	branches     branches
	environments []environment
	ruleset      []byte
	rendered     []byte
	workflows    []workflowEdit
	// sets は、印 `# branch-set:` を持つ workflow。marks はその印の数。
	sets  []workflowEdit
	marks int
}

// readDeclarations は3つの宣言を読み、検証し、include の区間を生成し直します。書き込みはしません。
func readDeclarations(root string) (declarations, error) {
	b, err := branchdecl.Read(filepath.Join(root, branchesFile))
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
	if err := checkDeployEnvs(identity, b); err != nil {
		return declarations{}, xerrors.Wrapf(err, "%s", branchesFile)
	}
	workflows, err := renderApplyTriggers(root, b)
	if err != nil {
		return declarations{}, err
	}
	applyPaths := make([]string, 0, len(workflows))
	for _, w := range workflows {
		applyPaths = append(applyPaths, w.path)
	}
	sets, marks, err := renderBranchSets(root, b, applyPaths)
	if err != nil {
		return declarations{}, err
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
	return declarations{branches: b, environments: envs, ruleset: src, rendered: rendered, workflows: workflows, sets: sets, marks: marks}, nil
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
		want.Envs = append(want.Envs, desiredEnv{Name: e.Name, Patterns: environmentPatterns(decl.branches, e)})
	}
	return want, nil
}

// applyOrCheck は include の区間と apply の workflow の起動条件を branches.json から生成します。dryRun が
// 真なら書かず、ずれを報せます。書き込みは、3つの宣言の検証がすべて通った後に、まとめて行います。
func applyOrCheck(root string, dryRun bool, out io.Writer) error {
	decl, err := readDeclarations(root)
	if err != nil {
		return err
	}
	summary := fmt.Sprintf("保護対象 %d 件、environment %d 件、apply の workflow %d 件、分岐の集合の印 %d 件（workflow %d 件）",
		len(rulesetIncludes(decl.branches)), len(decl.environments), len(decl.workflows), decl.marks, len(decl.sets))
	changed := map[string]string{}
	var names []string
	if string(decl.rendered) != string(decl.ruleset) {
		changed[filepath.Join(root, rulesetFile)] = string(decl.rendered)
		names = append(names, rulesetFile)
	}
	var applyDrift, setDrift []string
	for _, w := range decl.workflows {
		if w.rendered != w.current {
			changed[filepath.Join(root, w.path)] = w.rendered
			names = append(names, w.path)
			applyDrift = append(applyDrift, w.path)
		}
	}
	for _, w := range decl.sets {
		if w.rendered != w.current {
			changed[filepath.Join(root, w.path)] = w.rendered
			names = append(names, w.path)
			setDrift = append(setDrift, w.path)
		}
	}
	if len(changed) == 0 {
		fmt.Fprintf(out, "✅ %s: %s。生成する区間は宣言どおりです\n", toolName, summary)
		return nil
	}
	if dryRun {
		if _, ok := changed[filepath.Join(root, rulesetFile)]; ok {
			return errIncludeDrift
		}
		if len(applyDrift) > 0 {
			return xerrors.Wrapf(errTriggerDrift, "%s", strings.Join(applyDrift, ", "))
		}
		return xerrors.Wrapf(errSetDrift, "%s", strings.Join(setDrift, ", "))
	}
	if err := atomicwrite.Apply(changed, filePerm); err != nil {
		return xerrors.Wrapf(err, "%s の書き込み", strings.Join(names, ", "))
	}
	fmt.Fprintf(out, "✅ %s: %s。%s を書き換えました\n", toolName, summary, strings.Join(names, ", "))
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

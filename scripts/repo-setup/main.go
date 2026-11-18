// Package main は、boilerplate を自分のリポジトリとして初期化する際の
// git / gh 操作を担うツール。
//
//	preflight           初期化してよい状態かを確かめる（v0.0.0 タグか、宛先を上書きする環境変数があれば中止）
//	bootstrap           gh の宛先を origin に固定し、タグを作り直し、env を持たないデプロイブランチを用意して
//	                    デフォルトブランチを移す
//
// 作るブランチ（branches.json の deploy のうち env を持たないもの）と初期のデフォルトブランチ（release_base）、
// 初期化前に居たら消す線は、分岐のパターンの宣言（`.github/settings/branches.json`、ADR-0603 決定1）を直下から読む。
// env を持つデプロイブランチは、そこへの merge が apply を起動するので作らない（make create-deploy-branch が作る）。
//
//	prune-release-notes v0.0.0.md 以外のリリースノートを削除する
//
// ラベル・ルールセット・ワークフローの初期化と全体の連鎖は setup-repository.mk が持ち、
// ここが持つのは git / gh の手順だけ。取り消しの効かない操作（タグの一括削除 / デフォルト
// ブランチの移動）を含むため make のレシピにしない。理由は scripts/README.md の repo-setup 行。
//
// git / gh はホストの認証情報を使うため、ツールランナーではなくホストで実行する。
package main

import (
	"context"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/branchdecl"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/reporoot"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const (
	// initialTag は、初期化後に唯一残るタグ。
	initialTag     = "v0.0.0"
	releaseNoteDir = ".github/release"
	stepsPerTag    = 2
	commandTimeout = 120 * time.Second
)

var (
	errInitialTagExists  = xerrors.New("があります。初期化を停止します")
	errUsage             = xerrors.New("❌ usage: repo-setup <preflight|bootstrap|prune-release-notes>")
	errUnknownSubcommand = xerrors.New("❌ unknown subcommand (preflight / bootstrap / prune-release-notes)")
	errNoReleaseBase     = xerrors.New("❌ 宣言に release_base の印を持つブランチがありません")
	errBaseHasEnv        = xerrors.New("は env を持つデプロイブランチです。初期化では作れないので、初期のデフォルトブランチにできません")
	errTargetOverridden  = xerrors.New("が環境にあると、gh と make の宛先が origin に固定されません。外してから実行してください（例: env -u GH_REPO -u GITHUB_REPOSITORY make setup-repo）")
)

type setup struct {
	// branches は、初期化で作るブランチ（env を持たないデプロイブランチ。宣言の順）。
	branches []string
	// base は、初期化後のデフォルトブランチ（release_base）。
	base string
	decl branchdecl.Branches
}

// setupFromDecl は、release_base が env を持てば失敗する —— 初期化はそれを作らないので、
// デフォルトブランチを無いブランチへ移すことになる。
func setupFromDecl(decl branchdecl.Branches) (setup, error) {
	base, ok := decl.ReleaseBase()
	if !ok {
		return setup{}, errNoReleaseBase
	}

	if base.Env != "" {
		return setup{}, xerrors.Wrap(errBaseHasEnv, "❌ release_base の "+base.Branch)
	}

	branches := make([]string, 0, len(decl.Deploy))

	for _, d := range decl.Deploy {
		if d.Env == "" {
			branches = append(branches, d.Branch)
		}
	}

	return setup{branches: branches, base: base.Branch, decl: decl}, nil
}

// step は、実行する 1 コマンド。allowFail はリモートに対象が無い場合など、
// 失敗しても続行してよい操作に付ける。
type step struct {
	name      string
	args      []string
	allowFail bool
}

// runner は、手順を実際に走らせる実行層。テストが差し替える継ぎ目（scripts/README.md の Test Strategy）。
type runner struct {
	run          func(s step) error
	output       func(name string, args ...string) (string, error)
	branchExists func(branch string) bool
	getenv       func(key string) string
}

func main() {
	log.SetFlags(0)

	if err := start(hostRunner(), os.Args[1:], os.Getwd, reporoot.Toplevel); err != nil {
		log.Fatalf("%v", err)
	}
}

// start は、作業ディレクトリがリポジトリの直下であることを確かめ、分岐のパターンの宣言を直下から読んでから
// execute を呼びます。手順はリリースノートを直下からの相対パスで読む。宣言を読めないか初期化の対象を
// 決められなければ、手順を1つも実行しない。
func start(r runner, args []string, getwd, toplevel func() (string, error)) error {
	if err := reporoot.Require(getwd, toplevel); err != nil {
		return err
	}

	root, err := getwd()
	if err != nil {
		return xerrors.Wrap(err, "作業ディレクトリの取得")
	}

	decl, err := branchdecl.Read(filepath.Join(root, branchdecl.File))
	if err != nil {
		return err
	}

	s, err := setupFromDecl(decl)
	if err != nil {
		return err
	}

	return execute(r, s, args)
}

func execute(r runner, s setup, args []string) error {
	if len(args) == 0 {
		return errUsage
	}

	switch args[0] {
	case "preflight":
		return runPreflight(r.getenv)
	case "bootstrap":
		return runBootstrap(r, s)
	case "prune-release-notes":
		return runPruneReleaseNotes()
	default:
		return errUnknownSubcommand
	}
}

func (s step) String() string { return s.name + " " + strings.Join(s.args, " ") }

func parseLines(out string) []string {
	lines := make([]string, 0)

	for line := range strings.SplitSeq(out, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			lines = append(lines, t)
		}
	}

	return lines
}

// リモートに存在しないタグの削除は失敗しても続行します（ローカルにだけ在るタグは正常な状態）。
func tagDeletionSteps(tags []string) []step {
	steps := make([]step, 0, len(tags)*stepsPerTag)

	for _, tag := range tags {
		steps = append(steps,
			step{name: "git", args: []string{"tag", "-d", tag}},
			step{name: "git", args: []string{"push", "origin", ":refs/tags/" + tag}, allowFail: true},
		)
	}

	return steps
}

func initialTagSteps() []step {
	return []step{
		{name: "git", args: []string{"tag", "-a", initialTag, "-m", "Initial boilerplate tag"}},
		{name: "git", args: []string{"push", "origin", initialTag}},
	}
}

// branchCreationSteps は、まだ無いブランチだけを作る手順と、既存のためスキップした名前を返します。
func branchCreationSteps(managed, existing []string) ([]step, []string) {
	have := make(map[string]bool, len(existing))
	for _, b := range existing {
		have[b] = true
	}

	steps := make([]step, 0, len(managed))
	skipped := make([]string, 0)

	for _, b := range managed {
		if have[b] {
			skipped = append(skipped, b)

			continue
		}

		steps = append(steps, step{name: "git", args: []string{"branch", b}})
	}

	return steps, skipped
}

func branchPushStep(managed []string) step {
	return step{name: "git", args: append([]string{"push", "origin"}, managed...)}
}

func defaultBranchStep(repo, base string) step {
	return step{name: "gh", args: []string{"api", "-X", "PATCH", "repos/" + repo, "-f", "default_branch=" + base}}
}

// originalBranchCleanupSteps は、初期化前に居たブランチが宣言の線（リリース線か hotfix の線）だった場合に
// それを削除する手順を返します。線でなければ何もしません。
// リモートに未 push のブランチもあり得るため、リモート削除の失敗は許容します。
func originalBranchCleanupSteps(decl branchdecl.Branches, original string) []step {
	if !decl.IsLine(original) {
		return nil
	}

	return []step{
		{name: "git", args: []string{"branch", "-D", original}},
		{name: "git", args: []string{"push", "origin", "--delete", original}, allowFail: true},
	}
}

func releaseNotesToDelete(entries []string) []string {
	keep := initialTag + ".md"
	targets := make([]string, 0, len(entries))

	for _, e := range entries {
		if e != keep {
			targets = append(targets, e)
		}
	}

	return targets
}

// targetOverrides は、gh repo set-default による宛先の固定より優先される環境変数。GH_REPO は gh が、
// GITHUB_REPOSITORY は repo-settings.mk の REPO_SETTINGS_REPO が読む。
var targetOverrides = []string{"GH_REPO", "GITHUB_REPOSITORY"}

func runPreflight(getenv func(string) string) error {
	log.Printf("🔧 設定を確認中...")

	for _, key := range targetOverrides {
		if getenv(key) != "" {
			return xerrors.Wrap(errTargetOverridden, "❌ "+key)
		}
	}

	if tagExists(initialTag) {
		return xerrors.Wrap(errInitialTagExists, "❌ タグ 【"+initialTag+"】")
	}

	log.Printf("✅ 初期化を開始します")

	return nil
}

func runBootstrap(r runner, s setup) error {
	// 書き込みの前に、この後のすべての gh（make の連鎖の後段を含む）の宛先を origin に固定する。
	// 理由は scripts/README.md の repo-setup 行。
	if err := r.run(pinTargetStep()); err != nil {
		return err
	}

	if err := resetTags(r); err != nil {
		return err
	}

	if err := createBranches(r, s.branches); err != nil {
		return err
	}

	return moveDefaultBranch(r, s)
}

func pinTargetStep() step {
	return step{name: "gh", args: []string{"repo", "set-default", "origin"}}
}

func resetTags(r runner) error {
	log.Printf("🔧 タグの初期化を開始します...")

	out, err := r.output("git", "tag")
	if err != nil {
		return err
	}

	tags := parseLines(out)
	if len(tags) == 0 {
		log.Printf("🟡 削除対象のタグが存在しません。")
	} else {
		if err := r.runAll(tagDeletionSteps(tags)); err != nil {
			return err
		}

		log.Printf("🧹 すべてのタグを削除しました。")
	}

	log.Printf("✅ タグの初期化を終了します。")
	log.Printf("🔧 %s のタグ打ちを開始します...", initialTag)

	if err := r.runAll(initialTagSteps()); err != nil {
		return err
	}

	log.Printf("✅ %s のタグ打ちが完了しました。", initialTag)

	return nil
}

func createBranches(r runner, managed []string) error {
	log.Printf("🔧 ブランチ作成を開始します...")

	existing := make([]string, 0, len(managed))

	for _, b := range managed {
		if r.branchExists(b) {
			existing = append(existing, b)
		}
	}

	steps, skipped := branchCreationSteps(managed, existing)

	for _, b := range skipped {
		log.Printf("🟡 ブランチ 【%s】 は既に存在します。作成処理をスキップします。", b)
	}

	if err := r.runAll(steps); err != nil {
		return err
	}

	if err := r.run(branchPushStep(managed)); err != nil {
		return err
	}

	log.Printf("✅ ブランチの作成を終了します。")

	return nil
}

func moveDefaultBranch(r runner, s setup) error {
	log.Printf("🔧 デフォルトブランチの設定を開始します...")

	repo, err := r.output("gh", "repo", "view", "--json", "name,owner", "-q", `.owner.login + "/" + .name`)
	if err != nil {
		return err
	}

	if err := r.run(defaultBranchStep(strings.TrimSpace(repo), s.base)); err != nil {
		return err
	}

	if err := r.run(step{name: "git", args: []string{"fetch", "--prune"}}); err != nil {
		return err
	}

	// 元居たブランチ名は switch する前に控える。
	original, err := r.output("git", "branch", "--show-current")
	if err != nil {
		return err
	}

	if err := r.run(step{name: "git", args: []string{"switch", s.base}}); err != nil {
		return err
	}

	if err := r.runAll(originalBranchCleanupSteps(s.decl, strings.TrimSpace(original))); err != nil {
		return err
	}

	log.Printf("✅ デフォルトブランチを %s に設定しました。", s.base)

	return nil
}

func runPruneReleaseNotes() error {
	log.Printf("🔧 リリースノートの初期化を開始します...")

	entries, err := os.ReadDir(releaseNoteDir)
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("🟡 %s ディレクトリが存在しないためスキップします。", releaseNoteDir)

			return nil
		}

		return xerrors.Wrap(err, "failed to read "+releaseNoteDir)
	}

	names := make([]string, 0, len(entries))

	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}

	for _, name := range releaseNotesToDelete(names) {
		if err := os.Remove(filepath.Join(releaseNoteDir, name)); err != nil {
			return xerrors.Wrap(err, "failed to remove "+name)
		}
	}

	log.Printf("🧹 %s.md 以外のリリースノートを削除しました。", initialTag)
	log.Printf("✅ リリースノートの初期化を終了します。")

	return nil
}

func hostRunner() runner {
	return runner{run: run, output: output, branchExists: branchExists, getenv: os.Getenv}
}

// runAll は、失敗した時点で残りの手順を実行しません。
func (r runner) runAll(steps []step) error {
	for _, s := range steps {
		if err := r.run(s); err != nil {
			return err
		}
	}

	return nil
}

func tagExists(tag string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()

	//nolint:gosec // tag は本ファイル内の定数
	return exec.CommandContext(ctx, "git", "rev-parse", "--verify", "refs/tags/"+tag).Run() == nil
}

func branchExists(branch string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()

	//nolint:gosec // branch は宣言の deploy の名前（branchdecl の検証が形を限る）
	return exec.CommandContext(ctx, "git", "show-ref", "--verify", "--quiet", "refs/heads/"+branch).Run() == nil
}

func run(s step) error {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, s.name, s.args...) //nolint:gosec // 引数は本ファイル内で組み立てた固定手順
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil && !s.allowFail {
		return xerrors.Wrap(err, "failed: "+s.String())
	}

	return nil
}

func output(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, name, args...).Output() //nolint:gosec // 引数は本ファイル内の固定値
	if err != nil {
		return "", xerrors.Wrap(err, "failed: "+name+" "+strings.Join(args, " "))
	}

	return string(out), nil
}

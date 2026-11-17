package main

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/branchdecl"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const gitTimeout = 60 * time.Second

// notAncestor は、`git merge-base --is-ancestor` が「祖先でない」ときに返す終了コード。
// `git merge-tree --write-tree` も、衝突したときに同じ 1 を返す。
const notAncestor = 1

var (
	errSource       = xerrors.New("デプロイブランチへ入れてよい commit ではありません（ADR-0603 決定31）")
	errSourceGit    = xerrors.New("git の出力を読めません")
	errNoDeployEnv  = xerrors.New("env のデプロイブランチが branches.json にありません")
	sourceSHAFormat = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

// gitOps は、checkSource が使う git の3つの問い合わせ。テストが差し替える継ぎ目である。
type gitOps struct {
	isAncestor func(dir, commit, ref string) error
	mergeTree  func(dir, base, head string) (string, error)
	tree       func(dir, commit string) (string, error)
}

var realGit = gitOps{isAncestor: gitIsAncestor, mergeTree: gitMergeTree, tree: gitTree}

type exitCoder interface {
	ExitCode() int
}

// gitIsAncestor は、commit が ref の祖先かを `git merge-base --is-ancestor` に問います。祖先でなければ
// 終了コード notAncestor を持つエラーを返す。
func gitIsAncestor(dir, commit, ref string) error {
	_, err := git(dir, "merge-base", "--is-ancestor", "--end-of-options", commit, ref)
	return err
}

// gitMergeTree は、base と head の merge の結果を `git merge-tree --write-tree` に書かせ、その出力を返します。
// 衝突すれば終了コード notAncestor を持つエラーを返す。
func gitMergeTree(dir, base, head string) (string, error) {
	return git(dir, "merge-tree", "--write-tree", "--end-of-options", base, head)
}

func gitTree(dir, commit string) (string, error) {
	return git(dir, "rev-parse", "--verify", "--end-of-options", commit+"^{tree}")
}

// git は dir で git を起動し、標準出力を返します。失敗は終了コードを保ったまま返す。
func git(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	//nolint:gosec // 引数はこのファイルの関数が組み立てる。commit は 40 桁の SHA に限り、ref は refs/remotes/origin/ の下の名前、--end-of-options の後に置く
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return string(out), xerrors.Wrap(err, "git "+strings.Join(args, " "))
}

// sourceRequest は、デプロイブランチ宛かもしれない Pull Request 1件。
type sourceRequest struct {
	base, baseSHA, headSHA, def string
}

// checkSource は、Pull Request の head の commit をデプロイブランチ base へ入れてよいかを決めます（ADR-0603 決定31）。
// base が env を持つデプロイブランチでなければ対象外として成功する。そうなら次をすべて要求する。
//
//   - デフォルトブランチがデプロイブランチでなく、その entry の from の線のどれかである
//   - head が origin/<デフォルトブランチ> の祖先である
//   - base と head の merge の結果の木が、衝突せず head の木と一致する
//
// head のブランチ名とリポジトリは見ない（理由は ADR-0603 決定31）。
// 宣言は root（base の commit を取り出した木）のものを読む。
func checkSource(root string, req sourceRequest, ops gitOps, out io.Writer) error {
	b, err := branchdecl.Read(filepath.Join(root, branchesFile))
	if err != nil {
		return err
	}
	d, ok := b.DeployForBranch(req.base)
	if !ok {
		fmt.Fprintf(out, "✅ %s: %s はデプロイブランチではないので対象外です\n", toolName, req.base)
		return nil
	}
	if !sourceSHAFormat.MatchString(req.baseSHA) || !sourceSHAFormat.MatchString(req.headSHA) || req.def == "" {
		return errUsage
	}
	if slices.Contains(b.DeployNames(), req.def) || !d.FromLine(req.def) {
		return xerrors.Wrapf(errSource, "デフォルトブランチ %s は %s へ変更を運ぶ線ではありません（from: %s）",
			req.def, d.Branch, strings.Join(d.From, ", "))
	}
	if err := ops.isAncestor(root, req.headSHA, "refs/remotes/origin/"+req.def); err != nil {
		var code exitCoder
		if xerrors.As(err, &code) && code.ExitCode() == notAncestor {
			return xerrors.Wrapf(errSource, "%s はデフォルトブランチ %s から到達できません。%s でレビューされた commit だけを運べます",
				req.headSHA[:7], req.def, req.def)
		}
		return xerrors.Wrap(errSourceGit, err.Error())
	}
	merged, err := ops.mergeTree(root, req.baseSHA, req.headSHA)
	if err != nil {
		var code exitCoder
		if xerrors.As(err, &code) && code.ExitCode() == notAncestor {
			return xerrors.Wrapf(errSource, "%s と %s の merge が衝突します", d.Branch, req.headSHA[:7])
		}
		return xerrors.Wrap(errSourceGit, err.Error())
	}
	tree, err := ops.tree(root, req.headSHA)
	if err != nil {
		return xerrors.Wrap(errSourceGit, err.Error())
	}
	mergedTree := strings.TrimSpace(strings.SplitN(merged, "\n", 2)[0])
	if !sourceSHAFormat.MatchString(mergedTree) || !sourceSHAFormat.MatchString(strings.TrimSpace(tree)) {
		return xerrors.Wrapf(errSourceGit, "木の SHA を読めません: %q %q", mergedTree, tree)
	}
	if mergedTree != strings.TrimSpace(tree) {
		return xerrors.Wrapf(errSource, "merge の結果の木が %s の木と一致しません。%s が %s に無い変更を持ちます",
			req.headSHA[:7], d.Branch, req.def)
	}
	fmt.Fprintf(out, "✅ %s: %s は %s から到達でき、%s へ merge した木が一致します\n", toolName, req.headSHA[:7], req.def, d.Branch)
	return nil
}

// deployBranch は、env のデプロイブランチの名前を1行で書きます。人の手順の入口が、宣言から名前を得るのに使う。
func deployBranch(root, env string, out io.Writer) error {
	b, err := branchdecl.Read(filepath.Join(root, branchesFile))
	if err != nil {
		return err
	}
	d, ok := b.DeployForEnv(env)
	if !ok {
		return xerrors.Wrapf(errNoDeployEnv, "%q", env)
	}
	fmt.Fprintln(out, d.Branch)
	return nil
}

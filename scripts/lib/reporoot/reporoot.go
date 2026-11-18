// Package reporoot は、作業ディレクトリがリポジトリの直下であることを確かめる。
//
// 直下からの相対パスでファイルを読み、取り消しの効かない手順へ進むツールが、手順に入る前に呼ぶ。
// 直下以外で走ると、相対パスは別の場所を指し、「無い」と読んで止まるか、黙って飛ばして成功する。
package reporoot

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const commandTimeout = 30 * time.Second

// repoEnvVars は、git が見るリポジトリと作業ツリーを作業ディレクトリから切り離す環境変数。
// 立っていると show-toplevel は作業ディレクトリの位置を答えず、呼び出し側が続けて起動する git も同じ値を継ぐ。
var repoEnvVars = []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR", "GIT_INDEX_FILE"}

var (
	// ErrNotRoot は、作業ディレクトリがリポジトリの直下ではないことを表す。
	ErrNotRoot = xerrors.New("はリポジトリの直下ではありません。make の入口から起動してください")
	// ErrNoToplevel は、リポジトリの直下を解決できないことを表す。
	ErrNoToplevel = xerrors.New("❌ リポジトリの直下を解決できません")
	// ErrRepoEnv は、repoEnvVars のいずれかが環境に立っていることを表す。
	ErrRepoEnv = xerrors.New("が環境に立っています。git が見るリポジトリが作業ディレクトリと食い違いうるので止めます")
)

// Require は、getwd が返す作業ディレクトリと toplevel が返すリポジトリの直下が同じディレクトリであることを確かめます。
// どちらかを取れないとき、同じディレクトリでないときはエラーを返します。
func Require(getwd, toplevel func() (string, error)) error {
	root, err := toplevel()
	if err != nil {
		return err
	}

	cwd, err := getwd()
	if err != nil {
		return xerrors.Wrap(err, "failed to get working directory")
	}

	cwdInfo, err := os.Stat(cwd)
	if err != nil {
		return xerrors.Wrap(err, "failed to stat "+cwd)
	}

	rootInfo, err := os.Stat(root)
	if err != nil {
		return xerrors.Wrap(err, "failed to stat "+root)
	}

	if !os.SameFile(cwdInfo, rootInfo) {
		return xerrors.Wrap(ErrNotRoot, "❌ 作業ディレクトリ "+cwd+"（直下: "+root+"）")
	}

	return nil
}

// Toplevel は、`git rev-parse --show-toplevel` が返すリポジトリの直下を返します。
// git の外、作業ツリーを持たない場所では ErrNoToplevel を、repoEnvVars が立っていれば ErrRepoEnv を返します。
func Toplevel() (string, error) {
	for _, name := range repoEnvVars {
		if _, ok := os.LookupEnv(name); ok {
			return "", xerrors.Wrap(ErrRepoEnv, "❌ "+name)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && len(exit.Stderr) > 0 {
			return "", xerrors.Wrap(ErrNoToplevel, strings.TrimSpace(string(exit.Stderr)))
		}

		return "", xerrors.Wrap(ErrNoToplevel, err.Error())
	}

	root := strings.TrimSpace(string(out))
	if root == "" {
		return "", ErrNoToplevel
	}

	return root, nil
}

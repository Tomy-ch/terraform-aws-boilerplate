// Package reporoot は、作業ディレクトリがリポジトリの直下であることを確かめる。
//
// 直下からの相対パスでファイルを読み、取り消しの効かない手順へ進むツールが、手順に入る前に呼ぶ。
// 直下以外で走ると、相対パスは別の場所を指し、「無い」と読んで止まるか、黙って飛ばして成功する。
package reporoot

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const commandTimeout = 30 * time.Second

var (
	errNotRoot    = xerrors.New("はリポジトリの直下ではありません。make の入口から起動してください")
	errNoToplevel = xerrors.New("❌ リポジトリの直下を解決できません")
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
		return xerrors.Wrap(errNotRoot, "❌ 作業ディレクトリ "+cwd+"（直下: "+root+"）")
	}

	return nil
}

// Toplevel は、`git rev-parse --show-toplevel` が返すリポジトリの直下を返します。
// git の外、作業ツリーを持たない場所では errNoToplevel を返します。
func Toplevel() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", xerrors.Wrap(errNoToplevel, err.Error())
	}

	root := strings.TrimSpace(string(out))
	if root == "" {
		return "", errNoToplevel
	}

	return root, nil
}

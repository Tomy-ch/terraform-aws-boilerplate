// Package gitenv は、運用機構が起動する git と、git を起動する道具（editorconfig-checker など）へ渡す env を、
// 外側のリポジトリから切り離した形で作ります。
//
// git は hook に GIT_DIR や GIT_INDEX_FILE を渡します。それを継いだ git は、作業ディレクトリのリポジトリではなく
// 外側のリポジトリを読み書きします。外す変数の一覧の取り方は lib/testenv と同じで、GIT_ で始まる変数を外した env
// で git 自身の `git rev-parse --local-env-vars` に問います。lib/testenv はテストのプロセスから変数を外し、
// ここは起動する子プロセスの env から外します。
package gitenv

import (
	"context"
	"os/exec"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// ErrNames は、外す環境変数の一覧を git から得られないことを表します。
var ErrNames = xerrors.New("git rev-parse --local-env-vars から、外す環境変数の一覧を得られません")

// queryFunc は、env で `git rev-parse --local-env-vars` を起動し、その出力を返します。
type queryFunc func(env []string) ([]byte, error)

// Env は、environ から git がリポジトリの位置を決める環境変数を外した写しを返します。一覧を得られなければ
// ErrNames を返し、写しを返しません。
func Env(environ []string) ([]string, error) {
	return isolate(environ, queryLocalEnvVars)
}

func isolate(environ []string, query queryFunc) ([]string, error) {
	out, err := query(withoutGitPrefix(environ))
	if err != nil {
		return nil, xerrors.Wrapf(ErrNames, "%v", err)
	}
	names := strings.Fields(string(out))
	if len(names) == 0 {
		return nil, ErrNames
	}

	return without(environ, names), nil
}

func queryLocalEnvVars(env []string) ([]byte, error) {
	cmd := exec.CommandContext(context.Background(), "git", "rev-parse", "--local-env-vars")
	cmd.Env = env

	return cmd.Output()
}

// withoutGitPrefix は、GIT_ で始まる変数を除いた写しを返します。一覧を問う git が、これから外す変数に
// 左右されないようにするためです。
func withoutGitPrefix(environ []string) []string {
	kept := make([]string, 0, len(environ))
	for _, kv := range environ {
		if !strings.HasPrefix(kv, "GIT_") {
			kept = append(kept, kv)
		}
	}

	return kept
}

// without は、names の変数を除いた写しを、順序を保って返します。
func without(environ, names []string) []string {
	drop := make(map[string]bool, len(names))
	for _, n := range names {
		drop[n] = true
	}
	kept := make([]string, 0, len(environ))
	for _, kv := range environ {
		name, _, _ := strings.Cut(kv, "=")
		if !drop[name] {
			kept = append(kept, kv)
		}
	}

	return kept
}

// Package testenv は、テストが実行環境の都合に対処するときの共通の入口を持ちます。
//
// skip は既定の出力に現れません。報告より少ない検査で緑が残るので、CI では skip を
// 失敗へ変えられる必要があります（scripts/README.md の Test Strategy）。
package testenv

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/shellcheck"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// RequireNonRootEnv は、root 実行による skip を失敗へ変える環境変数の名前です。
const RequireNonRootEnv = "REQUIRE_NONROOT"

// 外界。判断はこれを直に読むので、入口が何かを渡し間違える余地がありません。
var (
	geteuid                = os.Geteuid
	getenv                 = os.Getenv
	lookPath               = exec.LookPath
	localEnvVars           = gitLocalEnvVars
	unsetenv               = os.Unsetenv
	stderr       io.Writer = os.Stderr
)

// reporter は requireNonRoot と requireShellcheck が使う *testing.T の部分です。判断の分岐を、
// root でない環境や shellcheck が在る環境からも到達可能にするために切り出しています。
type reporter interface {
	Helper()
	Skip(args ...any)
	Fatalf(format string, args ...any)
}

// RequireNonRoot は、root で実行している場合に reason を添えて skip します。
// RequireNonRootEnv が空でなければ skip せず失敗させます（理由は scripts/README.md の
// Test Strategy）。
func RequireNonRoot(t *testing.T, reason string) {
	t.Helper()
	requireNonRoot(t, reason)
}

func requireNonRoot(t reporter, reason string) {
	t.Helper()

	if geteuid() != 0 {
		return
	}
	if getenv(RequireNonRootEnv) != "" {
		t.Fatalf("root で実行しています（%s 指定時は skip しません）: %s", RequireNonRootEnv, reason)

		return
	}
	t.Skip(reason)
}

// RequireShellcheckEnv は、shellcheck 不在による skip を失敗へ変える環境変数の名前です。
const RequireShellcheckEnv = "REQUIRE_SHELLCHECK"

// RequireShellcheck は、shellcheck が PATH に無ければ skip します。
// RequireShellcheckEnv が空でなければ skip せず失敗させます（理由は scripts/README.md の
// Test Strategy）。
func RequireShellcheck(t *testing.T) {
	t.Helper()
	requireShellcheck(t)
}

func requireShellcheck(t reporter) {
	t.Helper()

	if _, err := lookPath(shellcheck.Binary); err == nil {
		return
	}
	if getenv(RequireShellcheckEnv) != "" {
		t.Fatalf("shellcheck が PATH にありません（%s 指定時は skip しません）", RequireShellcheckEnv)

		return
	}
	t.Skip("shellcheck が PATH にありません")
}

// ErrNoLocalEnvVars は、git がリポジトリの位置を決める環境変数を1つも返さなかったことを表します。
// 外すものが無いのではなく、一覧を得られなかったと読みます。
var ErrNoLocalEnvVars = xerrors.New("git rev-parse --local-env-vars が1件も返しませんでした")

type runner interface {
	Run() int
}

// RunGitIsolated は、git がリポジトリの位置を決める環境変数（git rev-parse --local-env-vars）を
// プロセスから外してから m を走らせ、その終了コードを返します。外す一覧を得られなければ
// m を走らせず 2 を返します。git を起動するテストのパッケージは TestMain からこれを呼びます
// （理由は scripts/README.md の Test Strategy）。
func RunGitIsolated(m runner) int {
	if err := isolateGit(); err != nil {
		fmt.Fprintf(stderr, "❌ git の環境変数を外せませんでした: %v\n", err)

		return 2
	}

	return m.Run()
}

func isolateGit() error {
	names, err := localEnvVars()
	if err != nil {
		return err
	}
	if len(names) == 0 {
		return ErrNoLocalEnvVars
	}
	for _, name := range names {
		if err := unsetenv(name); err != nil {
			return xerrors.Wrapf(err, "%s を外せません", name)
		}
	}

	return nil
}

// gitLocalEnvVars は git 自身が定義する一覧を返します。リポジトリを読まないので、外す前の
// GIT_DIR が存在しない場所を指していても答えます。
func gitLocalEnvVars() ([]string, error) {
	out, err := exec.CommandContext(context.Background(), "git", "rev-parse", "--local-env-vars").Output()
	if err != nil {
		return nil, xerrors.Wrap(err, "git rev-parse --local-env-vars")
	}

	return strings.Fields(string(out)), nil
}

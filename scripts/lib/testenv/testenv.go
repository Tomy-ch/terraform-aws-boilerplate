// Package testenv は、テストが実行環境の都合で skip するときの共通の入口と、テストの git を
// 外側のリポジトリから切り離す入口を持ちます。
//
// skip は既定の出力に現れません。報告より少ない検査で緑が残るので、CI では skip を
// 失敗へ変えられる必要があります（scripts/README.md の Test Strategy）。
package testenv

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/shellcheck"
)

// RequireNonRootEnv は、root 実行による skip を失敗へ変える環境変数の名前です。
const RequireNonRootEnv = "REQUIRE_NONROOT"

// 外界。判断はこれを直に読むので、入口が何かを渡し間違える余地がありません。
var (
	geteuid  = os.Geteuid
	getenv   = os.Getenv
	lookPath = exec.LookPath
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

var errGitEnvVars = errors.New("git rev-parse --local-env-vars から、外す環境変数の一覧を得られません")

// runner は *testing.M の部分です。
type runner interface {
	Run() int
}

// RunGitIsolated は、git がリポジトリの位置を決める環境変数をプロセスから外してから m を走らせ、
// 終了コードを返します。TestMain から `os.Exit(testenv.RunGitIsolated(m))` と呼びます。
//
// git は hook に GIT_DIR や GIT_INDEX_FILE を渡します。それを継いだテストの git は、一時
// リポジトリではなく外側のリポジトリを書き換えます。外す変数の一覧は git 自身の
// `git rev-parse --local-env-vars` に従います。一覧を得られなければ、m を走らせずに 2 を返します。
func RunGitIsolated(m runner) int {
	return runGitIsolated(m, gitEnvVars, os.Unsetenv, os.Stderr)
}

func runGitIsolated(m runner, list func() ([]string, error), unset func(string) error, stderr io.Writer) int {
	names, err := list()
	if err == nil && len(names) == 0 {
		err = errGitEnvVars
	}
	for _, name := range names {
		if err != nil {
			break
		}
		err = unset(name)
	}
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "❌ git の環境変数を外せません: %v\n", err)

		return 2
	}

	return m.Run()
}

// gitEnvVars は、git がリポジトリの位置を決めるのに読む環境変数の名前を返します。
// GIT_DIR が壊れていても、この問い合わせはリポジトリを読みません。
func gitEnvVars() ([]string, error) {
	out, err := exec.CommandContext(context.Background(), "git", "rev-parse", "--local-env-vars").Output()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errGitEnvVars, err)
	}

	return strings.Fields(string(out)), nil
}

// Package testenv は、テストが実行環境の都合で skip するときの共通の入口を持ちます。
//
// skip は既定の出力に現れません。報告より少ない検査で緑が残るので、CI では skip を
// 失敗へ変えられる必要があります（scripts/README.md の Test Strategy）。
package testenv

import (
	"os"
	"os/exec"
	"testing"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/shellcheck"
)

// RequireNonRootEnv は、root 実行による skip を失敗へ変える環境変数の名前です。
const RequireNonRootEnv = "REQUIRE_NONROOT"

// 外界。判断はこれを直に読むので、入口が何かを渡し間違える余地がありません。
// 既定値が本物を指していることは内部テストが実際の syscall と突き合わせて見ます。
var (
	geteuid  = os.Geteuid
	getenv   = os.Getenv
	lookPath = exec.LookPath
)

// reporter は requireNonRoot が使う *testing.T の部分です。分岐を root でない環境から
// 到達可能にするために切り出しています。
type reporter interface {
	Helper()
	Skip(args ...any)
	Fatalf(format string, args ...any)
}

// RequireNonRoot は、root で実行している場合に reason を添えて skip します。
// RequireNonRootEnv が空でなければ skip せず失敗させます。
//
// 権限を落として書き込みや削除の失敗を作るテストは、root では成立しません —— 落としたはずの
// 権限が効かず、失敗を作れないまま通ります。CI の実行主体が root へ変わった日に、これらの
// ケースが黙って skip され続けることを防ぎます。
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
// RequireShellcheckEnv が空でなければ skip せず失敗させます。
//
// 実物の shellcheck を駆動するテストは、不在の環境では成立しません。CI では必ず在るので、
// そこで skip が起きたなら検査範囲が報告より狭くなっています。
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

// Package testenv は、テストが実行環境の都合で skip するときの共通の入口を持ちます。
//
// skip は既定の出力に現れません。報告より少ない検査で緑が残るので、CI では skip を
// 失敗へ変えられる必要があります（scripts/README.md の Test Strategy）。
package testenv

import (
	"os"
	"testing"
)

// RequireNonRootEnv は、root 実行による skip を失敗へ変える環境変数の名前です。
const RequireNonRootEnv = "REQUIRE_NONROOT"

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
	requireNonRoot(t, reason, os.Geteuid, os.Getenv)
}

func requireNonRoot(t reporter, reason string, geteuid func() int, getenv func(string) string) {
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

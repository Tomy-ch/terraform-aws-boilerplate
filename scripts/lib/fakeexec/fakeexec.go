// Package fakeexec は、テストが直に exec する偽の道具（実行権付きのスクリプト）を書く入口を持ちます。
//
// 直に exec する偽の道具は、[Write] を通して書きます（scripts/README.md の Test Strategy）。
// os.WriteFile で書くと、並行に走る別のテストの fork が書き込みの fd を子へ持ち越し、その子が exec するまでの間に
// 偽の道具を exec すると、Linux は ETXTBSY（text file busy）を返します。macOS では起きないので、手元で通っても
// 起きないことの証拠になりません。
package fakeexec

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// Write は、body を perm で path へ新しく書きます。body は `#!` で始まり、perm は所有者の実行権を持つこと。
// path が既に在るとき、書けないときは t を失敗させます。
//
// open から close までを syscall.ForkLock の読みロックで囲みます。Go の fork（syscall.forkExec）は
// ForkLock の書き込みロックを握ったまま fork するので、書き込みの fd が開いている間は、このプロセスのどの
// fork もその fd を子へ持ち越せません。
func Write(t testing.TB, path, body string, perm os.FileMode) {
	t.Helper()

	if !strings.HasPrefix(body, "#!") {
		t.Fatalf("偽の道具 %s の本体が #! で始まりません（直の exec は ENOEXEC で落ちます）", path)

		return
	}

	if perm&0o100 == 0 {
		t.Fatalf("偽の道具 %s の権限 %#o は所有者の実行権を持ちません", path, perm)

		return
	}

	if err := write(path, body, perm, nil); err != nil {
		t.Fatalf("偽の道具 %s を書けません: %v", path, err)
	}
}

// write は、ForkLock の読みロックを握ったまま path を作って body を書き、閉じます。beforeClose が nil でなければ、
// 書いた後・閉じる前に呼びます。
func write(path, body string, perm os.FileMode, beforeClose func()) error {
	syscall.ForkLock.RLock()
	defer syscall.ForkLock.RUnlock()

	f, err := os.OpenFile(filepath.Clean(path), os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}

	if _, err := f.WriteString(body); err != nil {
		_ = f.Close()

		return err
	}

	if beforeClose != nil {
		beforeClose()
	}

	return f.Close()
}

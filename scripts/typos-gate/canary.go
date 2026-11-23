package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// misspelled は canary の誤綴りの語です。
const misspelled = "t" + "eh"

const hiddenFile = ".hidden/canary.txt"

// typosFound は、typos が誤綴りを見つけたときの終了コードです。道具のエラーは 64・78 などで、ここに当たらない。
const typosFound = 2

// runCanary は、fixture を work に建て、実物の typos が隠しディレクトリの誤綴りで落ち、本走査の対象に .github/ が
// 入っていることを確かめます。fixture は終わりに消します。
func runCanary(work string, cfg []byte, w io.Writer, tool toolFunc) error {
	if err := buildFixture(work, cfg); err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(work) }()

	var failed []string
	scan, err := runTool(tool(work, os.Environ(), stepScan))
	if err != nil {
		return err
	}
	if !judgeHidden(scan) {
		failed = append(failed, "隠しディレクトリの誤綴りで落ちませんでした")
		fmt.Fprintf(w, "--- typos --format brief（rc=%d）:\n%s\n", scan.code, scan.combined)
	}
	files, err := runTool(tool(".", os.Environ(), stepFiles))
	if err != nil {
		return err
	}
	if files.code != 0 || !scansGitHubDir(files.stdout) {
		failed = append(failed, "走査の対象に .github/ が入っていません")
		fmt.Fprintf(w, "--- typos --files（rc=%d）:\n%s\n", files.code, files.combined)
	}
	if len(failed) > 0 {
		return xerrors.Wrap(errCanary, strings.Join(failed, "。"))
	}
	fmt.Fprintln(w, "✅ typos canary: 隠しディレクトリの誤綴りで落ち、.github/ を走査しています")

	return nil
}

// readConfig は typos の設定を読みます。無い・空白だけなら errRead です —— 設定を読まない走査は隠しディレクトリを
// 飛ばし、canary も本走査も黙って範囲を縮めます。
func readConfig(p string) ([]byte, error) {
	raw, err := os.ReadFile(filepath.Clean(p))
	if err != nil {
		return nil, xerrors.Wrapf(errRead, "%v", err)
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, xerrors.Wrapf(errRead, "%s が空です", p)
	}

	return raw, nil
}

// buildFixture は、work を作り直し、設定の複写と、隠しディレクトリの中の誤綴りを置きます。
func buildFixture(work string, cfg []byte) error {
	if err := os.RemoveAll(work); err != nil {
		return xerrors.Wrapf(errRead, "%s の削除: %v", work, err)
	}
	hidden := filepath.Join(work, filepath.FromSlash(hiddenFile))
	if err := os.MkdirAll(filepath.Dir(hidden), 0o750); err != nil {
		return xerrors.Wrapf(errRead, "%v", err)
	}
	if err := os.WriteFile(filepath.Join(work, fixtureConfig), cfg, 0o600); err != nil {
		return xerrors.Wrapf(errRead, "%v", err)
	}
	if err := os.WriteFile(hidden, []byte(misspelled+"\n"), 0o600); err != nil {
		return xerrors.Wrapf(errRead, "%v", err)
	}

	return nil
}

// judgeHidden は、typos が誤綴りを見つけた終了コードで終わり、出力が隠しディレクトリのファイルと語を名指して
// いれば true です。
func judgeHidden(r result) bool {
	return r.code == typosFound && strings.Contains(r.combined, hiddenFile) && strings.Contains(r.combined, misspelled)
}

// scansGitHubDir は、typos --files の出力（1行1パス）に、リポジトリ直下の .github/ の下のファイルがあれば true です。
func scansGitHubDir(out string) bool {
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimPrefix(line, "./"), ".github/") {
			return true
		}
	}

	return false
}

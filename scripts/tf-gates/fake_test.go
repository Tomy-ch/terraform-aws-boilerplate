package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// 偽の道具は書いた直後のファイルを exec せず、/bin/sh に読ませて起動する。並行のテストが別の偽の道具を書いている
// 間に fork した子がその書き込みの fd を持ち越すと、書いたばかりのファイルの exec が ETXTBSY で落ちるため。
// fakeTool は、起動ごとに「作業ディレクトリ 段 TF_DATA_DIR」を calls.log へ書く偽の道具を作る。作業ディレクトリに
// `fail-<段>` があればその段は 1 で、`validate.out` があれば validate はその中身を出す。環境に AWS_ で始まる変数
// （宣言で塞ぐ3つを除く）が残っていれば 3 で落ちる。
func fakeTool(t *testing.T) (toolFunc, func() []string) {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "calls.log")
	script := filepath.Join(dir, "tool")
	body := `#!/bin/sh
step="$1"
echo "$PWD $step ${TF_DATA_DIR:-} ${TFLINT_CONFIG_FILE:-} ${TFLINT_PLUGIN_DIR:-}" >> '` + log + `'
left=$(env | grep '^AWS_' | grep -vE '^AWS_(SHARED_CREDENTIALS_FILE|CONFIG_FILE|EC2_METADATA_DISABLED)=')
if [ -n "$left" ]; then echo "AWS の環境変数が残っています: $left"; exit 3; fi
if [ -e "fail-$step" ]; then echo "$step が落ちた"; exit 1; fi
if [ "$step" = validate ] && [ -e validate.out ]; then cat validate.out; fi
exit 0
`
	require.NoError(t, os.WriteFile(script, []byte(body), 0o600))
	tool := func(d string, env []string, step string) *exec.Cmd {
		c := exec.CommandContext(context.Background(), "/bin/sh", script, step)
		c.Dir = d
		c.Env = env
		return c
	}
	calls := func() []string {
		raw, err := os.ReadFile(log)
		if os.IsNotExist(err) {
			return nil
		}
		require.NoError(t, err)
		return strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	}
	return tool, calls
}

// tree は files（`/` 区切りの相対パスから中身）を一時ディレクトリに置き、そのパスを返す。macOS の /var の symlink を
// 解いておく（作業ディレクトリの $PWD と比べるため）。
func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	for rel, body := range files {
		p := filepath.Join(base, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o750))
		require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
	}
	return base
}

// snapshot は、dir の下のすべてのパスの内容の写しを返す。差を取るために使い、symlink はたどらない。
func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	got := map[string]string{}
	require.NoError(t, filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			dest, err := os.Readlink(p)
			if err != nil {
				return err
			}
			got[rel] = "link " + dest
		case d.IsDir():
			got[rel] = "dir"
		default:
			raw, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			sum := sha256.Sum256(raw)
			got[rel] = hex.EncodeToString(sum[:])
		}
		return nil
	}))
	return got
}

// workWorld は、リポジトリ直下に見立てる root と、その外の outside を、1つの一時ディレクトリ world の下に建てる。
// world/x は、root から見て ../x に当たる。
func workWorld(t *testing.T) (world, root, outside string) {
	t.Helper()
	world = tree(t, map[string]string{
		"root/keep/f": "keep", "root/tmp/c/data/f": "data", "root/tmp/other/f": "other", "root/tmp/c/work/old": "old",
		"outside/stale.txt": "stale", "x/stale.txt": "stale",
	})
	root, outside = filepath.Join(world, "root"), filepath.Join(world, "outside")
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "tmp", "out")))
	require.NoError(t, os.Symlink(filepath.Join(root, "tmp"), filepath.Join(root, "tmp", "self")))
	return world, root, outside
}

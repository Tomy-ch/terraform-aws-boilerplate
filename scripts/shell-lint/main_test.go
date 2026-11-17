package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/shellcheck"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/testenv"
)

const cleanScript = "#!/bin/sh\nset -eu\necho hello\n"

// SC2086: 引用符の無い展開。方言に依存せず必ず指摘される。
const dirtyScript = "#!/bin/sh\nset -eu\nx=$1\necho $x\n"

func Test_run(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("指摘の無いスクリプトだけなら成功する", func(t *testing.T) {
			t.Parallel()
			testenv.RequireShellcheck(t)

			root := writeScripts(t, map[string]string{"ok.sh": cleanScript})

			require.NoError(t, run(t.Context(), rootAt(root), exec.LookPath, io.Discard))
		})

		// 「所見なし」の報告は、見た件数を伴わなければ「何も見なかった」と区別できない。
		t.Run("成功時に検査した件数を報告する", func(t *testing.T) {
			t.Parallel()
			testenv.RequireShellcheck(t)

			var out bytes.Buffer
			root := writeScripts(t, map[string]string{"a.sh": cleanScript, "b.sh": cleanScript})

			require.NoError(t, run(t.Context(), rootAt(root), exec.LookPath, &out))
			assert.Contains(t, out.String(), "2 ファイル")
		})

		t.Run("除外ディレクトリ配下は検査しない", func(t *testing.T) {
			t.Parallel()
			testenv.RequireShellcheck(t)

			root := writeScripts(t, map[string]string{
				"ok.sh":         cleanScript,
				"vendor/bad.sh": dirtyScript,
				"tmp/bad.sh":    dirtyScript,
				".git/bad.sh":   dirtyScript,
			})

			require.NoError(t, run(t.Context(), rootAt(root), exec.LookPath, io.Discard))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		// 0 件は成功ではない（ADR-0702 決定13）。対象を失った検査は、壊れた日に
		// 赤ではなく緑を返す。「違反が無かった」と「何も見なかった」を区別し続ける。
		t.Run("走査対象が1件も無ければエラーを返す", func(t *testing.T) {
			t.Parallel()
			testenv.RequireShellcheck(t)

			root := writeScripts(t, map[string]string{"README.md": "not a script\n"})

			require.ErrorIs(t, run(t.Context(), rootAt(root), exec.LookPath, io.Discard), errNoTargets)
		})

		t.Run("除外ディレクトリにしか対象が無ければエラーを返す", func(t *testing.T) {
			t.Parallel()
			testenv.RequireShellcheck(t)

			root := writeScripts(t, map[string]string{"vendor/bad.sh": dirtyScript})

			require.ErrorIs(t, run(t.Context(), rootAt(root), exec.LookPath, io.Discard), errNoTargets)
		})

		// 実装は xerrors.Wrap(err, "read") で落ちる。ここが将来 continue へ変わって
		// 黙ってスキップされても、このケースが無ければ誰も気づかない。
		t.Run("走査対象が読めなければエラーを返す", func(t *testing.T) {
			t.Parallel()
			testenv.RequireShellcheck(t)

			root := t.TempDir()
			require.NoError(t, os.Symlink(
				filepath.Join(root, "no-such-target"),
				filepath.Join(root, "broken.sh"),
			))

			require.Error(t, run(t.Context(), rootAt(root), exec.LookPath, io.Discard))
		})

		t.Run("指摘のあるスクリプトを検出する", func(t *testing.T) {
			t.Parallel()
			testenv.RequireShellcheck(t)

			root := writeScripts(t, map[string]string{"bad.sh": dirtyScript})

			require.ErrorIs(t, run(t.Context(), rootAt(root), exec.LookPath, io.Discard), errFindings)
		})

		// 出力そのものが契約である —— 人が読んで直す手掛かりはこれしかない。
		t.Run("指摘のテキストにどのファイルの何行目かを載せる", func(t *testing.T) {
			t.Parallel()
			testenv.RequireShellcheck(t)

			var out bytes.Buffer
			root := writeScripts(t, map[string]string{"bad.sh": dirtyScript})

			require.ErrorIs(t, run(t.Context(), rootAt(root), exec.LookPath, &out), errFindings)
			assert.Contains(t, out.String(), "bad.sh:")
		})

		t.Run("複数ファイルの指摘を合算して報告する", func(t *testing.T) {
			t.Parallel()
			testenv.RequireShellcheck(t)

			var out bytes.Buffer
			root := writeScripts(t, map[string]string{"a.sh": dirtyScript, "b.sh": dirtyScript})

			require.ErrorIs(t, run(t.Context(), rootAt(root), exec.LookPath, &out), errFindings)
			assert.Contains(t, out.String(), "a.sh:")
			assert.Contains(t, out.String(), "b.sh:")
			assert.GreaterOrEqual(t, strings.Count(out.String(), ".sh:"), 2)
		})

		t.Run("shellcheck が無ければ実行せずに報告する", func(t *testing.T) {
			t.Parallel()

			missing := func(string) (string, error) { return "", os.ErrNotExist }

			require.ErrorIs(t, run(t.Context(), rootAt(t.TempDir()), missing, io.Discard), shellcheck.ErrMissing)
		})

		t.Run("基点ディレクトリを解決できなければ報告する", func(t *testing.T) {
			t.Parallel()
			testenv.RequireShellcheck(t)

			failing := func() (string, error) { return "", os.ErrPermission }

			require.ErrorIs(t, run(t.Context(), failing, exec.LookPath, io.Discard), os.ErrPermission)
		})

		t.Run("shellcheck の起動自体に失敗したら報告する", func(t *testing.T) {
			t.Parallel()
			testenv.RequireShellcheck(t)

			root := writeScripts(t, map[string]string{"ok.sh": cleanScript})

			require.ErrorIs(t, run(canceledContext(t), rootAt(root), exec.LookPath, io.Discard), shellcheck.ErrRun)
		})
	})
}

func Test_shellScripts(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("リポジトリ相対パスを昇順で返す", func(t *testing.T) {
			t.Parallel()

			root := writeScripts(t, map[string]string{
				"b.sh":        cleanScript,
				"a/nested.sh": cleanScript,
				"note.txt":    "x\n",
			})

			scripts, err := shellScripts(root)

			require.NoError(t, err)
			assert.Equal(t, []string{"a/nested.sh", "b.sh"}, scripts)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("走査できない基点なら報告する", func(t *testing.T) {
			t.Parallel()

			_, err := shellScripts(filepath.Join(t.TempDir(), "missing"))

			require.Error(t, err)
		})
	})
}

func Test_prefixFindings(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("パスをリポジトリ相対へ揃える", func(t *testing.T) {
			t.Parallel()

			out := "/abs/path/bad.sh:4:6: note: Double quote [SC2086]\n"

			got, err := prefixFindings("a/bad.sh", out)
			require.NoError(t, err)
			assert.Equal(t, []string{"a/bad.sh:4:6: note: Double quote [SC2086]"}, got)
		})

		t.Run("複数行の指摘をすべて前置する", func(t *testing.T) {
			t.Parallel()

			out := "-:4:6: note: Double quote [SC2086]\n-:9:1: warning: Unused [SC2034]\n"

			got, err := prefixFindings("a/bad.sh", out)
			require.NoError(t, err)
			assert.Equal(t, []string{
				"a/bad.sh:4:6: note: Double quote [SC2086]",
				"a/bad.sh:9:1: warning: Unused [SC2034]",
			}, got)
		})

		t.Run("空の出力を指摘にしない", func(t *testing.T) {
			t.Parallel()

			got, err := prefixFindings("a/bad.sh", "  \n")
			require.NoError(t, err)
			assert.Nil(t, got)
		})

		t.Run("指摘の間の空行を読み飛ばす", func(t *testing.T) {
			t.Parallel()

			out := "-:4:6: note: A [SC2086]\n\n-:9:1: note: B [SC2034]\n"

			got, err := prefixFindings("a/bad.sh", out)
			require.NoError(t, err)
			assert.Len(t, got, 2)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		// 解釈できない行を黙って捨てると、出力形式が変わった日に「指摘なし」と
		// 「1行も解釈できなかった」が緑で区別できなくなる（ADR-0702 決定14）。
		t.Run("区切りを持たない行はエラーにする", func(t *testing.T) {
			t.Parallel()

			_, err := prefixFindings("a/bad.sh", "malformed line\n")
			require.ErrorIs(t, err, errUnparsedFinding)
		})

		t.Run("解釈できない行が混ざっていてもエラーにする", func(t *testing.T) {
			t.Parallel()

			out := "-:4:6: note: A [SC2086]\nmalformed line\n"

			_, err := prefixFindings("a/bad.sh", out)
			require.ErrorIs(t, err, errUnparsedFinding)
		})
	})
}

// writeScripts は相対パスと内容の対からテスト用のツリーを作り、その基点を返します。
func writeScripts(t *testing.T, files map[string]string) string {
	t.Helper()

	root := t.TempDir()
	for name, body := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	}

	return root
}

// rootAt は固定の基点を返す wd 相当の関数を作ります。
func rootAt(root string) func() (string, error) {
	return func() (string, error) { return root, nil }
}

func canceledContext(t *testing.T) context.Context {
	t.Helper()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	return ctx
}

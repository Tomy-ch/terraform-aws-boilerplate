package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const sampleLog = "ok  \tgithub.com/x/a\t0.1s\tcoverage: 97.0% of statements\n" +
	"?   \tgithub.com/x/nop\t[no test files]\n" +
	"\tgithub.com/x/c\t\tcoverage: 0.0% of statements\n" +
	"--- FAIL: TestB (0.00s)\n" +
	"    b_test.go:10: /work/repo/scripts/b/b.go が違う\n" +
	"coverage: 80.0% of statements\n" +
	"FAIL\tgithub.com/x/b\t0.2s\n\n\n"

func Test_failures(t *testing.T) {
	t.Parallel()

	t.Run("成功しか報告していない行とカバレッジの行を落とし、作業ディレクトリを消す", func(t *testing.T) {
		t.Parallel()
		got, err := failures(strings.NewReader(sampleLog), "/work/repo/")
		require.NoError(t, err)
		assert.Equal(t, []string{"--- FAIL: TestB (0.00s)", "    b_test.go:10: scripts/b/b.go が違う", "FAIL\tgithub.com/x/b\t0.2s"}, got)
	})

	t.Run("gh run view --log の接頭辞を剥がしてから行頭を見る", func(t *testing.T) {
		t.Parallel()
		in := "go-test\tRun Tests\t\xef\xbb\xbf2026-11-24T02:38:21.6187862Z ok  \tgithub.com/x/a\t0.1s\n" +
			"go-test\tRun Tests\t2026-11-24T02:38:21.6188860Z --- FAIL: TestB (0.00s)\n"
		got, err := failures(strings.NewReader(in), "")
		require.NoError(t, err)
		assert.Equal(t, []string{"--- FAIL: TestB (0.00s)"}, got)
	})

	t.Run("NUL を含む行も読む", func(t *testing.T) {
		t.Parallel()
		got, err := failures(strings.NewReader("--- FAIL: T\x00x\n"), "")
		require.NoError(t, err)
		assert.Equal(t, []string{"--- FAIL: T\x00x"}, got)
	})

	t.Run("成功だけのログは空", func(t *testing.T) {
		t.Parallel()
		got, err := failures(strings.NewReader("ok  \tx\t0.1s\n\n"), "")
		require.NoError(t, err)
		assert.Empty(t, got)
	})
}

func Test_run(t *testing.T) {
	t.Parallel()

	t.Run("ファイルのログの失敗の行を stdout に出す", func(t *testing.T) {
		t.Parallel()
		p := filepath.Join(t.TempDir(), "log")
		require.NoError(t, os.WriteFile(p, []byte(sampleLog), 0o600))
		var stdout, stderr bytes.Buffer
		require.NoError(t, run([]string{"-log", p, "-strip", "/work/repo/"}, nil, &stdout, &stderr))
		assert.Equal(t, "--- FAIL: TestB (0.00s)\n    b_test.go:10: scripts/b/b.go が違う\nFAIL\tgithub.com/x/b\t0.2s\n", stdout.String())
		assert.Empty(t, stderr.String())
	})

	t.Run("- なら標準入力を読み、失敗の行が無ければ stderr に報せて stdout は空", func(t *testing.T) {
		t.Parallel()
		for _, log := range []string{"-", "/dev/stdin"} {
			var stdout, stderr bytes.Buffer
			require.NoError(t, run([]string{"-log", log}, strings.NewReader("ok  \tx\t0.1s\n"), &stdout, &stderr))
			assert.Empty(t, stdout.String())
			assert.Equal(t, "✅ 失敗行はありません（"+log+"）\n", stderr.String())
		}
	})

	t.Run("ログを読めなければ errRead", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, run([]string{"-log", filepath.Join(t.TempDir(), "none")}, nil, &bytes.Buffer{}, &bytes.Buffer{}), errRead)
	})

	t.Run("引数の誤りは errUsage", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, run(nil, nil, &bytes.Buffer{}, &bytes.Buffer{}), errUsage)
		require.ErrorIs(t, run([]string{"-log", "x", "y"}, nil, &bytes.Buffer{}, &bytes.Buffer{}), errUsage)
		require.ErrorIs(t, run([]string{"-x"}, nil, &bytes.Buffer{}, &bytes.Buffer{}), errUsage)
	})
}

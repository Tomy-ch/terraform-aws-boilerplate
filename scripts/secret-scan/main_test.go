package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/gitenv"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

func Test_run(t *testing.T) {
	t.Parallel()

	t.Run("サブコマンドが無い・知らないなら errUsage", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, run(nil, &bytes.Buffer{}, &bytes.Buffer{}, runner{}), errUsage)
		require.ErrorIs(t, run([]string{"gitleaks"}, &bytes.Buffer{}, &bytes.Buffer{}, runner{}), errUsage)
	})

	for _, sub := range []string{"gitleaks-range", "gitleaks-history", "trufflehog-range", "trufflehog-history", "trufflehog-worktree", "trivy"} {
		t.Run(sub+" の flag の解釈へ渡し、誤りを stdout へ書かない", func(t *testing.T) {
			t.Parallel()
			s := newScanner(t)
			var stdout, stderr bytes.Buffer
			err := run([]string{sub, "-no-such-flag"}, &stdout, &stderr, runner{tool: s.tool(), git: s.tool()})
			require.ErrorIs(t, err, errUsage)
			assert.Contains(t, stderr.String(), "Usage of secret-scan "+sub+":")
			assert.Empty(t, stdout.String())
			assert.Nil(t, s.calls())
		})
	}

	for _, args := range [][]string{
		{"gitleaks-range", "-from", "HEAD"},
		{"gitleaks-history"},
		{"trufflehog-range", "-from", "HEAD"},
		{"trufflehog-history"},
		{"trufflehog-worktree"},
	} {
		t.Run(args[0]+": git の env を作れなければ（壊れた .gitconfig）gitenv.ErrNames で、走査器を1つも起動しない", func(t *testing.T) {
			t.Parallel()
			r := newRepo(t)
			s := newScanner(t)
			rn := r.runner(s)
			home := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(home, ".gitconfig"), []byte("[broken\n"), 0o600))
			rn.environ = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "XDG_CONFIG_HOME=" + home}
			require.ErrorIs(t, run(args, &bytes.Buffer{}, &bytes.Buffer{}, rn), gitenv.ErrNames)
			assert.Nil(t, s.calls())
			_, err := os.Stat(filepath.Join(r.dir, "tmp"))
			require.ErrorIs(t, err, os.ErrNotExist, "作業ディレクトリを作った")
		})
	}

	t.Run("範囲の走査へ -from を渡す", func(t *testing.T) {
		t.Parallel()
		r := newRepo(t)
		s := newScanner(t)
		for _, sub := range []string{"gitleaks-range", "trufflehog-range"} {
			require.ErrorIs(t, run([]string{sub, "-from", ""}, &bytes.Buffer{}, &bytes.Buffer{}, r.runner(s)), errFromEmpty, sub)
		}
		assert.Nil(t, s.calls())
	})
}

func Test_report(t *testing.T) {
	t.Parallel()

	t.Run("エラーが無ければ何も書かずに 0", func(t *testing.T) {
		t.Parallel()
		var out bytes.Buffer
		assert.Equal(t, 0, report(nil, &out))
		assert.Empty(t, out.String())
	})

	t.Run("エラーを1行で書き、終了コードを返す", func(t *testing.T) {
		t.Parallel()
		var out bytes.Buffer
		assert.Equal(t, 2, report(errFromEmpty, &out))
		assert.Equal(t, "❌ secret-scan: 走査範囲の起点が空です。GITHUB_BASE_REF を渡すか COMMITLINT_FROM を指定してください\n", out.String())
	})
}

func Test_exitCode(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		err  error
		want int
	}{
		"成功は 0":               {nil, 0},
		"所見は 1":               {errFindings, 1},
		"canary の失敗は 1":       {errCanary, 1},
		"走査した数のずれは 1":         {errMismatch, 1},
		"包んでも区分は変わらない":        {xerrors.Wrap(errCanary, "x"), 1},
		"使い方の誤りは 2":           {errUsage, 2},
		"起点が空は 2":             {errFromEmpty, 2},
		"起点を解決できないのは 2":       {errFromUnresolved, 2},
		"入力を読めないのは 2":         {errInput, 2},
		"抑止を解釈できないのは 2":       {errExclude, 2},
		"対象が0件は 2":            {errNoTargets, 2},
		"道具を起動できないのは 2":       {errTool, 2},
		"走査器の不全は 2":           {errScan, 2},
		"数の行を読めないのは 2":        {errNoCount, 2},
		"numstat を解釈できないのは 2": {errNumstat, 2},
		"未分類のエラーは 2":          {xerrors.New("x"), 2},
		"git の env を作れないのは 2": {gitenv.ErrNames, 2},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, exitCode(c.err))
		})
	}
}

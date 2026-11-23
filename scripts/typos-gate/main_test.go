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

func Test_gitEnv(t *testing.T) {
	t.Parallel()

	t.Run("このプロセスの PATH を残す", func(t *testing.T) {
		t.Parallel()
		env, err := gitEnv()
		require.NoError(t, err)
		assert.Contains(t, env, "PATH="+os.Getenv("PATH"))
	})
}

func Test_gitEnvFrom(t *testing.T) {
	t.Parallel()

	t.Run("GIT_DIR を外し、ほかは残す", func(t *testing.T) {
		t.Parallel()
		env, err := gitEnvFrom([]string{"PATH=" + os.Getenv("PATH"), "GIT_DIR=/x", "A=1"})
		require.NoError(t, err)
		assert.NotContains(t, env, "GIT_DIR=/x")
		assert.Contains(t, env, "A=1")
	})

	t.Run("git が一覧を返せなければエラーを返す", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(home, ".gitconfig"), []byte("[broken\n"), 0o600))
		_, err := gitEnvFrom([]string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "XDG_CONFIG_HOME=" + home})
		require.ErrorIs(t, err, gitenv.ErrNames)
	})
}

func Test_run(t *testing.T) {
	t.Parallel()

	t.Run("サブコマンドが無いか知らなければ errUsage", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, run(nil, &bytes.Buffer{}, command, gitEnv), errUsage)
		require.ErrorIs(t, run([]string{"canary"}, &bytes.Buffer{}, command, gitEnv), errUsage)
	})

	t.Run("各サブコマンドへ渡す", func(t *testing.T) {
		t.Parallel()
		for _, sub := range []string{"check"} {
			t.Run(sub, func(t *testing.T) {
				t.Parallel()
				var out bytes.Buffer
				require.ErrorIs(t, run([]string{sub, "-no-such-flag"}, &out, command, gitEnv), errUsage)
				assert.Contains(t, out.String(), "-no-such-flag", "そのサブコマンドの flag の解釈に届いている")
			})
		}
	})
}

func Test_report(t *testing.T) {
	t.Parallel()

	t.Run("成功なら何も書かず 0", func(t *testing.T) {
		t.Parallel()
		var out bytes.Buffer
		assert.Equal(t, 0, report(nil, &out))
		assert.Empty(t, out.String())
	})

	t.Run("エラーを1行で書き、終了コードを返す", func(t *testing.T) {
		t.Parallel()
		var out bytes.Buffer
		assert.Equal(t, 1, report(xerrors.Wrap(errCanary, "x"), &out))
		assert.Equal(t, "❌ typos-gate: x: canary が期待どおりではありません\n", out.String())
	})
}

func Test_exitCode(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		err  error
		want int
	}{
		"成功は 0":          {nil, 0},
		"canary の不成立は 1": {xerrors.Wrap(errCanary, "x"), 1},
		"綴りの誤りは 1":       {xerrors.Wrap(errFailed, "x"), 1},
		"使い方の誤りは 2":      {xerrors.Wrap(errUsage, "x"), 2},
		"読めない入力は 2":      {xerrors.Wrap(errRead, "x"), 2},
		"0件は 2":          {xerrors.Wrap(errNoInput, "x"), 2},
		"道具を起動できないのは 2":  {xerrors.Wrap(errTool, "x"), 2},
		"走査の対象から外れた追跡ファイルは 2": {xerrors.Wrap(errUncovered, "x"), 2},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, exitCode(c.err))
		})
	}
}

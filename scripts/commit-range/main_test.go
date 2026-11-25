package main

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// t.Setenv はプロセスの env を変えるので、このテストは並行に走らせない。
func Test_gitEnv(t *testing.T) {
	t.Run("このプロセスの env から GIT_DIR を外し、PATH を残す", func(t *testing.T) {
		t.Setenv("GIT_DIR", "/x")
		env, err := gitEnv()
		require.NoError(t, err)
		for _, kv := range env {
			assert.False(t, strings.HasPrefix(kv, "GIT_DIR="), kv)
		}
		assert.Contains(t, env, "PATH="+os.Getenv("PATH"))
	})
}

func Test_run(t *testing.T) {
	t.Parallel()

	t.Run("サブコマンドが無いか知らなければ errUsage", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, run(nil, &bytes.Buffer{}, command, noEnv), errUsage)
		require.ErrorIs(t, run([]string{"count"}, &bytes.Buffer{}, command, noEnv), errUsage)
	})

	t.Run("check へ渡す", func(t *testing.T) {
		t.Parallel()
		var out bytes.Buffer
		require.ErrorIs(t, run([]string{"check", "-no-such-flag"}, &out, command, noEnv), errUsage)
		assert.Contains(t, out.String(), "-no-such-flag", "check の flag の解釈に届いている")
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

	for name, err := range map[string]error{
		"使い方の誤り":    xerrors.Wrap(errUsage, "x"),
		"起点が空":      errNoBase,
		"解決できない":    xerrors.Wrap(errUnresolved, "x"),
		"git の失敗":   xerrors.Wrap(errGit, "x"),
		"0件":        xerrors.Wrap(errNoCommits, "x"),
		"道具を起動できない": xerrors.Wrap(toolrun.ErrTool, "x"),
	} {
		t.Run(name+"は1行で書き、終了コード 2（commitlint の違反の 1 と分ける）", func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			assert.Equal(t, 2, report(err, &out))
			assert.True(t, strings.HasPrefix(out.String(), "❌ commit-range: "), out.String())
			assert.Equal(t, 1, strings.Count(out.String(), "\n"))
		})
	}
}

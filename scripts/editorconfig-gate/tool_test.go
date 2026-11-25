package main

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_command(t *testing.T) {
	t.Parallel()

	for step, want := range map[string][]string{
		stepGitInit:   {"git", "-c", "core.hooksPath=/dev/null", "init", "-q"},
		stepGitAdd:    {"git", "-c", "core.hooksPath=/dev/null", "add", "-A"},
		stepCheck:     {"editorconfig-checker", "-format", "default"},
		stepDryRun:    {"editorconfig-checker", "-format", "default", "-dry-run"},
		stepScan:      {"editorconfig-checker"},
		stepLsFiles:   {"git", "-c", "core.hooksPath=/dev/null", "ls-files", "-z", "--stage"},
		stepLsDeleted: {"git", "-c", "core.hooksPath=/dev/null", "ls-files", "-z", "--deleted"},
	} {
		t.Run(step, func(t *testing.T) {
			t.Parallel()
			c := command("/x", []string{"A=1"}, step)
			assert.Equal(t, want, c.Args)
			assert.Equal(t, "/x", c.Dir)
			assert.Equal(t, []string{"A=1"}, c.Env)
		})
	}
}

func Test_stripANSI(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct{ in, want string }{
		"色の制御列を外す":     {"\x1b[33;1mcanary.yaml:\x1b[33;0m\n\x1b[31;1m\t2: wrong indent\x1b[0m", "canary.yaml:\n\t2: wrong indent"},
		"制御列が無ければそのまま": {"a\tb\n", "a\tb\n"},
		"空":            {"", ""},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, stripANSI(c.in))
		})
	}
}

func Test_checkerEnv(t *testing.T) {
	t.Parallel()

	t.Run("受け継いだ env の後に core.quotepath=false を足し、ほかの変数は順に残す", func(t *testing.T) {
		t.Parallel()
		got := checkerEnv([]string{"PATH=/bin", "GIT_DIR=/i", "HOME=/h"})
		assert.Equal(t, []string{"PATH=/bin", "GIT_DIR=/i", "HOME=/h", "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=core.quotepath", "GIT_CONFIG_VALUE_0=false"}, got)
	})

	t.Run("受け継いだ git の設定の変数はすべて外す", func(t *testing.T) {
		t.Parallel()
		got := checkerEnv([]string{
			"GIT_CONFIG_COUNT=3", "GIT_CONFIG_KEY_0=core.quotepath", "GIT_CONFIG_VALUE_0=true",
			"GIT_CONFIG_KEY_2=core.x", "GIT_CONFIG_VALUE_2=y", "GIT_CONFIG_PARAMETERS='core.quotepath'='true'", "A=1",
		})
		assert.Equal(t, []string{"A=1", "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=core.quotepath", "GIT_CONFIG_VALUE_0=false"}, got)
	})

	t.Run("名前が前置きを含むだけの変数（GIT_CONFIG_GLOBAL など）は残す", func(t *testing.T) {
		t.Parallel()
		got := checkerEnv([]string{"GIT_CONFIG_GLOBAL=/g", "GIT_CONFIG_NOSYSTEM=1"})
		assert.Equal(t, []string{"GIT_CONFIG_GLOBAL=/g", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=core.quotepath", "GIT_CONFIG_VALUE_0=false"}, got)
	})

	t.Run("実物の git は、受け継いだ設定があっても core.quotepath を false と読む", func(t *testing.T) {
		t.Parallel()
		env, err := gitEnv()
		require.NoError(t, err)
		env = append(env, "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=core.quotepath", "GIT_CONFIG_VALUE_0=true", "GIT_CONFIG_PARAMETERS='core.quotepath'='true'")
		cmd := exec.CommandContext(context.Background(), "git", "config", "--get", "core.quotepath")
		cmd.Dir, cmd.Env = t.TempDir(), checkerEnv(env)
		out, err := cmd.Output()
		require.NoError(t, err)
		assert.Equal(t, "false", strings.TrimSpace(string(out)))
	})
}

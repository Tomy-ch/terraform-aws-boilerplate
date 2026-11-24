package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_command(t *testing.T) {
	t.Parallel()

	for step, want := range map[string][]string{
		stepGitInit: {"git", "-c", "core.hooksPath=/dev/null", "init", "-q"},
		stepGitAdd:  {"git", "-c", "core.hooksPath=/dev/null", "add", "-A"},
		stepCheck:   {"editorconfig-checker"},
		stepDryRun:  {"editorconfig-checker", "-dry-run"},
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

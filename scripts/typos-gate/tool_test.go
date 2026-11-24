package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_command(t *testing.T) {
	t.Parallel()

	for step, want := range map[string][]string{
		stepMain:    {"typos", "--format", "brief"},
		stepScan:    {"typos", "--config", ".typos.toml", "--format", "brief"},
		stepFiles:   {"typos", "--files"},
		stepLsFiles: {"git", "-c", "core.quotepath=false", "ls-files", "-z"},
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

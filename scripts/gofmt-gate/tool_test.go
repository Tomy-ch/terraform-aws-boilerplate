package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_command(t *testing.T) {
	t.Parallel()

	for _, step := range []string{stepCanary, stepMain} {
		t.Run(step+"は作業ディレクトリで gofmt -l . を起動する", func(t *testing.T) {
			t.Parallel()
			c := command(context.Background(), "/x", []string{"A=1"}, step)
			assert.Equal(t, []string{"gofmt", "-l", "."}, c.Args)
			assert.Equal(t, "/x", c.Dir)
			assert.Equal(t, []string{"A=1"}, c.Env)
		})
	}
}

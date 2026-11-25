package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_command(t *testing.T) {
	t.Parallel()

	for step, want := range map[string][]string{
		stepResolve: {"git", "-c", "core.hooksPath=/dev/null", "cat-file", "--batch-check=%(objectname) %(objecttype)"},
		stepCount:   {"git", "-c", "core.hooksPath=/dev/null", "rev-list", "--count", "--no-merges", "--stdin"},
	} {
		t.Run(step+"は rev を引数に置かない固定の引数で起動する", func(t *testing.T) {
			t.Parallel()
			c := command(context.Background(), "/x", []string{"A=1"}, step)
			assert.Equal(t, want, c.Args)
			assert.Equal(t, "/x", c.Dir)
			assert.Equal(t, []string{"A=1"}, c.Env)
		})
	}

	t.Run("知らない段は失敗するコマンドにする（段の名前の誤りを resolve として走らせない）", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, []string{"false"}, command(context.Background(), "/x", nil, "resolv").Args)
	})
}

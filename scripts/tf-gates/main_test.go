package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_run(t *testing.T) {
	t.Parallel()

	t.Run("サブコマンドが無ければ errUsage", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, run(nil, &bytes.Buffer{}, command), errUsage)
	})

	t.Run("知らないサブコマンドは errUsage", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, run([]string{"plan"}, &bytes.Buffer{}, command), errUsage)
	})

	t.Run("各サブコマンドへ渡す", func(t *testing.T) {
		t.Parallel()
		for _, sub := range []string{"dirs", "none", "fmt", "init", "validate", "result", "test", "lint", "validate-canary"} {
			t.Run(sub, func(t *testing.T) {
				t.Parallel()
				var out bytes.Buffer
				err := run([]string{sub, "-no-such-flag"}, &out, command)
				require.ErrorIs(t, err, errUsage)
				assert.Contains(t, out.String(), "-no-such-flag", "そのサブコマンドの flag の解釈に届いている")
			})
		}
	})
}

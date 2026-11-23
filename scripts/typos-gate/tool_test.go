package main

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func Test_runTool(t *testing.T) {
	t.Parallel()

	t.Run("stdout だけを stdout に、両方を combined に束ね、終了コードを返す", func(t *testing.T) {
		t.Parallel()
		f := newFake(t)
		f.set("s", "out", "O\n")
		f.set("s", "err", "E\n")
		f.set("s", "rc", "3")
		r, err := runTool(f.tool()(t.TempDir(), nil, "s"))
		require.NoError(t, err)
		assert.Equal(t, "O\n", r.stdout)
		assert.Contains(t, r.combined, "O\n")
		assert.Contains(t, r.combined, "E\n")
		assert.Equal(t, 3, r.code)
	})

	t.Run("stderr だけでも combined に載る", func(t *testing.T) {
		t.Parallel()
		f := newFake(t)
		f.set("s", "err", "E\n")
		r, err := runTool(f.tool()(t.TempDir(), nil, "s"))
		require.NoError(t, err)
		assert.Empty(t, r.stdout)
		assert.Equal(t, "E\n", r.combined)
		assert.Equal(t, 0, r.code)
	})

	t.Run("起動できなければ errTool", func(t *testing.T) {
		t.Parallel()
		_, err := runTool(missingTool(t.TempDir(), nil, "s"))
		require.ErrorIs(t, err, errTool)
	})

	t.Run("シグナルで終われば errTool", func(t *testing.T) {
		t.Parallel()
		f := newFake(t)
		f.set("s", "kill", "")
		_, err := runTool(f.tool()(t.TempDir(), nil, "s"))
		require.ErrorIs(t, err, errTool)
		assert.Contains(t, err.Error(), "シグナル")
	})
}

func Test_lockedBuffer_Write(t *testing.T) {
	t.Parallel()

	t.Run("並行に書いても取りこぼさない", func(t *testing.T) {
		t.Parallel()
		b := &lockedBuffer{}
		var wg sync.WaitGroup
		for range 50 {
			wg.Go(func() {
				_, _ = b.Write([]byte("x"))
			})
		}
		wg.Wait()
		assert.Len(t, b.String(), 50)
	})
}

func Test_lockedBuffer_String(t *testing.T) {
	t.Parallel()

	t.Run("書いた順に返す", func(t *testing.T) {
		t.Parallel()
		b := &lockedBuffer{}
		_, _ = b.Write([]byte("a"))
		_, _ = b.Write([]byte("b"))
		assert.Equal(t, "ab", b.String())
	})
}

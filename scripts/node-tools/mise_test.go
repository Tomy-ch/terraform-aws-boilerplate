package main

import (
	"bytes"
	"context"
	"io"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_runMiseNPMDisabled(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mise := func(script string) *fakeTools {
		return &fakeTools{scripts: map[string]string{stepMiseLs: script}}
	}
	t.Run("npm: の道具が無ければ通る", func(t *testing.T) {
		t.Parallel()
		var stderr bytes.Buffer
		require.NoError(t, runMiseNPMDisabled(ctx, nil, testDeps(mise(`echo '{"go": [], "aqua:pnpm/pnpm": []}'`), &stderr)))
		assert.Contains(t, stderr.String(), "✅ mise-npm-disabled")
	})
	t.Run("npm: の道具が在れば名前を挙げて落ちる", func(t *testing.T) {
		t.Parallel()
		err := runMiseNPMDisabled(ctx, nil, testDeps(mise(`echo '{"go": [], "npm:b": [], "npm:a": []}'`), io.Discard))
		require.ErrorIs(t, err, errMiseNPM)
		assert.Contains(t, err.Error(), "npm:a npm:b")
	})
	t.Run("mise が正しい JSON を出してから非0で終われば、npm: が無くても落ちる", func(t *testing.T) {
		t.Parallel()
		err := runMiseNPMDisabled(ctx, nil, testDeps(mise(`echo '{"go": []}'; exit 3`), io.Discard))
		var exitErr *exec.ExitError
		require.ErrorAs(t, err, &exitErr)
		assert.Equal(t, 3, exitErr.ExitCode())
	})
	t.Run("mise が何も出さずに 0 で終われば解釈できないとして落ちる", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, runMiseNPMDisabled(ctx, nil, testDeps(mise("true"), io.Discard)), errMiseJSON)
	})
	t.Run("mise が道具を返さなければ落ちる", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, runMiseNPMDisabled(ctx, nil, testDeps(mise(`echo '{}'`), io.Discard)), errMiseNoTools)
	})
	t.Run("mise ls は作業ディレクトリで起動する", func(t *testing.T) {
		t.Parallel()
		f := mise(`echo '{"go": []}'`)
		require.NoError(t, runMiseNPMDisabled(ctx, nil, testDeps(f, io.Discard)))
		assert.Equal(t, []string{""}, f.dirs())
	})
	t.Run("mise ls の途中で中断されたら中断として落ちる", func(t *testing.T) {
		t.Parallel()
		cctx, cancel := context.WithCancel(ctx)
		f := mise("exec sleep 30")
		d := testDeps(f, io.Discard)
		d.tool = func(c context.Context, dir, step string) *exec.Cmd {
			cmd := f.tool(c, dir, step)
			cancel()

			return cmd
		}
		require.ErrorIs(t, runMiseNPMDisabled(cctx, nil, d), errInterrupted)
	})
	t.Run("引数があれば usage で落ちる", func(t *testing.T) {
		t.Parallel()
		f := &fakeTools{}
		require.ErrorIs(t, runMiseNPMDisabled(ctx, []string{"x"}, testDeps(f, io.Discard)), errMiseUsage)
		assert.Empty(t, f.called())
	})
}

func Test_npmTools(t *testing.T) {
	t.Parallel()

	t.Run("npm: の道具を整列して返す", func(t *testing.T) {
		t.Parallel()
		got, err := npmTools([]byte(`{"npm:z": [], "go": [], "npm:a": []}`))
		require.NoError(t, err)
		assert.Equal(t, []string{"npm:a", "npm:z"}, got)
	})
	t.Run("npm: で始まらない名前は数えない", func(t *testing.T) {
		t.Parallel()
		got, err := npmTools([]byte(`{"npm": [], "xnpm:a": [], "aqua:npm:x": []}`))
		require.NoError(t, err)
		assert.Empty(t, got)
	})
	for name, body := range map[string]string{
		"空の出力":     "",
		"配列":       "[]",
		"壊れた JSON": "{",
	} {
		t.Run(name+"は解釈できないとして落ちる", func(t *testing.T) {
			t.Parallel()
			_, err := npmTools([]byte(body))
			require.ErrorIs(t, err, errMiseJSON)
		})
	}
	for name, body := range map[string]string{
		"空のオブジェクト": `{}`,
		"null":     `null`,
	} {
		t.Run(name+"は道具が0件として落ちる", func(t *testing.T) {
			t.Parallel()
			_, err := npmTools([]byte(body))
			require.ErrorIs(t, err, errMiseNoTools)
		})
	}
}

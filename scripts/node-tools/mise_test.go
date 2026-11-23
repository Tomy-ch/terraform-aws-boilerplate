package main

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_runMiseNPMDisabled(t *testing.T) {
	t.Parallel()

	mise := func(script string) *fakeTools {
		return &fakeTools{scripts: map[string]string{stepMiseLs: script}}
	}
	t.Run("npm: の道具が無ければ通る", func(t *testing.T) {
		t.Parallel()
		var stderr bytes.Buffer
		require.NoError(t, runMiseNPMDisabled(nil, testDeps(mise(`echo '{"go": [], "aqua:pnpm/pnpm": []}'`), &stderr)))
		assert.Contains(t, stderr.String(), "✅ mise-npm-disabled")
	})
	t.Run("npm: の道具が在れば名前を挙げて落ちる", func(t *testing.T) {
		t.Parallel()
		err := runMiseNPMDisabled(nil, testDeps(mise(`echo '{"go": [], "npm:b": [], "npm:a": []}'`), io.Discard))
		require.ErrorIs(t, err, errMiseNPM)
		assert.Contains(t, err.Error(), "npm:a npm:b")
	})
	t.Run("mise が落ちれば落ちる", func(t *testing.T) {
		t.Parallel()
		require.Error(t, runMiseNPMDisabled(nil, testDeps(mise("exit 3"), io.Discard)))
	})
	t.Run("mise が道具を返さなければ落ちる", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, runMiseNPMDisabled(nil, testDeps(mise(`echo '{}'`), io.Discard)), errMiseNoTools)
	})
	t.Run("引数があれば usage で落ちる", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, runMiseNPMDisabled([]string{"x"}, testDeps(&fakeTools{}, io.Discard)), errMiseUsage)
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
	t.Run("npm: の道具が無ければ空を返す", func(t *testing.T) {
		t.Parallel()
		got, err := npmTools([]byte(`{"go": []}`))
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
	t.Run("道具が0件なら落ちる", func(t *testing.T) {
		t.Parallel()
		_, err := npmTools([]byte(`{}`))
		require.ErrorIs(t, err, errMiseNoTools)
	})
}

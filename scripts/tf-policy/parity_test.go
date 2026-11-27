package main

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	conftestVersion = "Conftest: 0.71.1\nOPA: 1.21.1\n"
	opaVersion      = "Version: 1.21.1\nBuild Commit: x\nRego Version: v1\n"
)

func Test_versionLine(t *testing.T) {
	t.Parallel()

	t.Run("prefix で始まる最初の行の残りを返す", func(t *testing.T) {
		t.Parallel()
		got, ok := versionLine("Conftest: 0.71.1\nOPA: 1.21.1\n", "OPA:")
		require.True(t, ok)
		assert.Equal(t, "1.21.1", got)
	})

	t.Run("prefix を行の途中に含む行は当たらず、行頭の行を返す", func(t *testing.T) {
		t.Parallel()
		got, ok := versionLine("Rego Version: v1\nVersion: 1.21.1\n", "Version:")
		require.True(t, ok)
		assert.Equal(t, "1.21.1", got)
	})

	t.Run("値の無い行は当たらない", func(t *testing.T) {
		t.Parallel()
		_, ok := versionLine("OPA:\n", "OPA:")
		assert.False(t, ok)
	})

	t.Run("行が無ければ当たらない", func(t *testing.T) {
		t.Parallel()
		_, ok := versionLine("Conftest: 0.71.1\n", "OPA:")
		assert.False(t, ok)
	})
}

func Test_readVersion(t *testing.T) {
	t.Parallel()

	t.Run("ファイルの版の行を返す", func(t *testing.T) {
		t.Parallel()
		dir := tree(t, map[string]string{"v.txt": opaVersion})
		got, err := readVersion(filepath.Join(dir, "v.txt"), "Version:")
		require.NoError(t, err)
		assert.Equal(t, "1.21.1", got)
	})

	t.Run("空のファイルは errRead（道具を起動できなかった）", func(t *testing.T) {
		t.Parallel()
		dir := tree(t, map[string]string{"v.txt": ""})
		_, err := readVersion(filepath.Join(dir, "v.txt"), "Version:")
		require.ErrorIs(t, err, errRead)
	})

	t.Run("ファイルが無ければ errRead", func(t *testing.T) {
		t.Parallel()
		_, err := readVersion(filepath.Join(t.TempDir(), "none"), "Version:")
		require.ErrorIs(t, err, errRead)
	})
}

func Test_runOpaParity(t *testing.T) {
	t.Parallel()

	setup := func(t *testing.T, conftest, opa string) []string {
		t.Helper()
		dir := tree(t, map[string]string{"c.txt": conftest, "o.txt": opa})
		return []string{"-conftest", filepath.Join(dir, "c.txt"), "-opa", filepath.Join(dir, "o.txt")}
	}

	t.Run("版が同じなら成功し、版を出す", func(t *testing.T) {
		t.Parallel()
		var out bytes.Buffer
		require.NoError(t, runOpaParity(setup(t, conftestVersion, opaVersion), &out))
		assert.Equal(t, "✅ opa 1.21.1 は Conftest の内蔵の OPA と同じ版です\n", out.String())
	})

	t.Run("版が違えば両方を名指して errFailed", func(t *testing.T) {
		t.Parallel()
		err := runOpaParity(setup(t, conftestVersion, "Version: 1.23.0\n"), &bytes.Buffer{})
		require.ErrorIs(t, err, errFailed)
		assert.Contains(t, err.Error(), "opa の版 1.23.0 が、Conftest の内蔵の OPA の版 1.21.1 と違います")
	})

	t.Run("Conftest の出力に OPA の行が無ければ errRead", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, runOpaParity(setup(t, "Conftest: 0.71.1\n", opaVersion), &bytes.Buffer{}), errRead)
	})

	t.Run("opa の出力が空なら errRead", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, runOpaParity(setup(t, conftestVersion, ""), &bytes.Buffer{}), errRead)
	})

	t.Run("引数の誤りは errUsage", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, runOpaParity([]string{"-conftest", "c"}, &bytes.Buffer{}), errUsage)
	})
}

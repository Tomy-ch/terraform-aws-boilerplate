package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeTerraform は、init で initRC を、schema で schema を出して schemaRC を返す偽の terraform を置き、それを起動する
// tfCommand を返す。起動ごとに作業ディレクトリと段を calls.log へ1行ずつ書く。
func fakeTerraform(t *testing.T, initRC, schemaRC int, schema string) tfCommand {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "schema.json"), []byte(schema), 0o600))
	body := fmt.Sprintf(`#!/bin/sh
echo "$PWD $1" >> '%s'
case "$1" in
init) echo "init の出力"; exit %d ;;
schema) cat '%s'; echo "schema の警告" >&2; exit %d ;;
esac
exit 99
`, filepath.Join(dir, "calls.log"), initRC, filepath.Join(dir, "schema.json"), schemaRC)
	p := filepath.Join(dir, "terraform")
	require.NoError(t, os.WriteFile(p, []byte(body), 0o700))

	return func(d, step string) *exec.Cmd {
		c := exec.CommandContext(context.Background(), p, step)
		c.Dir = d

		return c
	}
}

// calls は、fakeTerraform の calls.log を返す。偽の terraform を1度も起動していなければ空。
func calls(t *testing.T, tf tfCommand) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(tf(".", "init").Path), "calls.log"))
	if os.IsNotExist(err) {
		return ""
	}
	require.NoError(t, err)

	return string(raw)
}

func writeFetchList(t *testing.T, dirs ...string) string {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	var list strings.Builder
	for _, d := range dirs {
		p := filepath.Join(base, d)
		require.NoError(t, os.MkdirAll(p, 0o750))
		list.WriteString(p + "\n")
	}
	writeFile(t, base, fetchName, list.String())

	return base
}

func Test_terraformCmd(t *testing.T) {
	t.Parallel()

	t.Run("init は backend を使わず lock を書き換えない引数で、ディレクトリで起動する", func(t *testing.T) {
		t.Parallel()
		c := terraformCmd("/x/6.2.0", "init")
		assert.Equal(t, []string{"terraform", "init", "-backend=false", "-lockfile=readonly", "-input=false", "-no-color"}, c.Args)
		assert.Equal(t, "/x/6.2.0", c.Dir)
	})

	t.Run("schema は providers schema -json を、ディレクトリで起動する", func(t *testing.T) {
		t.Parallel()
		c := terraformCmd("/x/6.2.0", "schema")
		assert.Equal(t, []string{"terraform", "providers", "schema", "-json"}, c.Args)
		assert.Equal(t, "/x/6.2.0", c.Dir)
	})
}

func Test_fetch(t *testing.T) {
	t.Parallel()

	t.Run("一覧の各ディレクトリで init と schema の取得を順に起動し、raw.json を書く", func(t *testing.T) {
		t.Parallel()
		base := writeFetchList(t, "6.2.0", "5.9.0")
		tf := fakeTerraform(t, 0, 0, `{"format_version":"1.0"}`)
		var stderr bytes.Buffer
		require.NoError(t, fetch([]string{"-dir", base}, &stderr, tf))

		v6, v5 := filepath.Join(base, "6.2.0"), filepath.Join(base, "5.9.0")
		assert.Equal(t, v6+" init\n"+v6+" schema\n"+v5+" init\n"+v5+" schema\n", calls(t, tf))
		assert.Equal(t, `{"format_version":"1.0"}`, readFile(t, filepath.Join(v6, rawName)))
		assert.Equal(t, "init の出力\n", readFile(t, filepath.Join(v6, "init.log")))
		assert.NotContains(t, stderr.String(), "init の出力", "成功した init の出力は出さない")
		assert.Contains(t, stderr.String(), "provider-schema fetch: 2 版の schema を取りました")
	})

	t.Run("一覧が空なら terraform を起動しない", func(t *testing.T) {
		t.Parallel()
		base := writeFetchList(t)
		tf := fakeTerraform(t, 0, 0, "{}")
		require.NoError(t, fetch([]string{"-dir", base}, &bytes.Buffer{}, tf))
		assert.Empty(t, calls(t, tf))
	})

	t.Run("init が失敗すれば、その出力を出して止まり、schema を取らない", func(t *testing.T) {
		t.Parallel()
		base := writeFetchList(t, "6.2.0", "5.9.0")
		tf := fakeTerraform(t, 1, 0, "{}")
		var stderr bytes.Buffer
		err := fetch([]string{"-dir", base}, &stderr, tf)
		require.ErrorIs(t, err, errFetch)
		assert.Contains(t, err.Error(), "init が失敗しました")
		assert.Contains(t, stderr.String(), "init の出力")
		assert.Equal(t, filepath.Join(base, "6.2.0")+" init\n", calls(t, tf))
		assert.NoFileExists(t, filepath.Join(base, "6.2.0", rawName))
	})

	t.Run("schema の取得が失敗すれば止まり、raw.json を書かない", func(t *testing.T) {
		t.Parallel()
		base := writeFetchList(t, "6.2.0", "5.9.0")
		tf := fakeTerraform(t, 0, 1, "{}")
		var stderr bytes.Buffer
		err := fetch([]string{"-dir", base}, &stderr, tf)
		require.ErrorIs(t, err, errFetch)
		assert.Contains(t, err.Error(), "schema を取れませんでした")
		assert.Contains(t, stderr.String(), "schema の警告")
		assert.NoFileExists(t, filepath.Join(base, "6.2.0", rawName))
		assert.NotContains(t, calls(t, tf), "5.9.0")
	})

	t.Run("terraform を起動できなければ errFetch", func(t *testing.T) {
		t.Parallel()
		base := writeFetchList(t, "6.2.0")
		missing := func(d, step string) *exec.Cmd {
			c := exec.CommandContext(context.Background(), filepath.Join(d, "missing"), step)
			c.Dir = d

			return c
		}
		require.ErrorIs(t, fetch([]string{"-dir", base}, &bytes.Buffer{}, missing), errFetch)
	})

	t.Run("一覧が無ければ errFetchList", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, fetch([]string{"-dir", t.TempDir()}, &bytes.Buffer{}, fakeTerraform(t, 0, 0, "{}")), errFetchList)
	})

	t.Run("引数の誤りは errUsage", func(t *testing.T) {
		t.Parallel()
		for name, args := range map[string][]string{
			"-dir が無い": {},
			"余計な引数":    {"-dir", ".", "x"},
			"知らないフラグ":  {"-terraform", "x"},
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				require.ErrorIs(t, fetch(args, &bytes.Buffer{}, fakeTerraform(t, 0, 0, "{}")), errUsage)
			})
		}
	})
}

func Test_fetchOne(t *testing.T) {
	t.Parallel()

	t.Run("init.log を書けなければ失敗し、schema を取らない", func(t *testing.T) {
		t.Parallel()
		tf := fakeTerraform(t, 0, 0, "{}")
		d := filepath.Join(t.TempDir(), "missing")
		err := fetchOne(func(_, step string) *exec.Cmd { return tf(t.TempDir(), step) }, d, &bytes.Buffer{})
		require.Error(t, err)
		assert.NotContains(t, calls(t, tf), " schema\n")
	})
}

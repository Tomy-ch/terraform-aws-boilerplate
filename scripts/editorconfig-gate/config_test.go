package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// isolated は、隔離した git の env の代わりに、印の GIT_DIR を持つ env を返します。
func isolated() ([]string, error) {
	return []string{"PATH=" + os.Getenv("PATH"), "GIT_DIR=/isolated"}, nil
}

func failingEnv() ([]string, error) {
	return nil, errors.New("boom")
}

func Test_runConfig(t *testing.T) {
	t.Parallel()

	emptyConfig := func(t *testing.T) string {
		t.Helper()
		return writeFile(t, filepath.Join(t.TempDir(), "c.json"), "{ }\n")
	}
	env, _ := isolated()

	t.Run("設定が {} で -dry-run が1件以上なら件数を出して通り、隔離した env を渡す", func(t *testing.T) {
		t.Parallel()
		f := newFake(t)
		f.set(stepDryRun, "out", "a.yaml\n\x1b[0m\nb.go\n\n")
		var out bytes.Buffer
		require.NoError(t, runConfig(emptyConfig(t), &out, f.tool(), env))
		assert.Equal(t, "editorconfig-check: 2 ファイル\n", out.String())
		calls := f.calls()
		require.Len(t, calls, 1)
		assert.True(t, strings.HasSuffix(calls[0], " "+stepDryRun+" /isolated"), calls[0])
	})

	t.Run("-dry-run が0件なら errNoInput", func(t *testing.T) {
		t.Parallel()
		for name, out := range map[string]string{"空": "", "空白と制御列だけ": " \n\x1b[0m\n"} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				f := newFake(t)
				f.set(stepDryRun, "out", out)
				require.ErrorIs(t, runConfig(emptyConfig(t), &bytes.Buffer{}, f.tool(), env), errNoInput)
			})
		}
	})

	t.Run("-dry-run の stderr は数えない", func(t *testing.T) {
		t.Parallel()
		f := newFake(t)
		f.set(stepDryRun, "err", "warning\n")
		require.ErrorIs(t, runConfig(emptyConfig(t), &bytes.Buffer{}, f.tool(), env), errNoInput)
	})

	t.Run("-dry-run が rc≠0 なら、出力があっても errTool", func(t *testing.T) {
		t.Parallel()
		f := newFake(t)
		f.set(stepDryRun, "out", "a.yaml\n")
		f.set(stepDryRun, "rc", "1")
		require.ErrorIs(t, runConfig(emptyConfig(t), &bytes.Buffer{}, f.tool(), env), errTool)
	})

	t.Run("道具を起動できなければ errTool", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, runConfig(emptyConfig(t), &bytes.Buffer{}, missingTool, env), errTool)
	})

	t.Run("設定が {} でなければ、道具を起動せずに errScope", func(t *testing.T) {
		t.Parallel()
		f := newFake(t)
		cfg := writeFile(t, filepath.Join(t.TempDir(), "c.json"), `{"Exclude": ["x"]}`)
		require.ErrorIs(t, runConfig(cfg, &bytes.Buffer{}, f.tool(), env), errScope)
		assert.Empty(t, f.calls())
	})

	t.Run("設定が無ければ errRead", func(t *testing.T) {
		t.Parallel()
		f := newFake(t)
		require.ErrorIs(t, runConfig(filepath.Join(t.TempDir(), "none"), &bytes.Buffer{}, f.tool(), env), errRead)
		assert.Empty(t, f.calls())
	})
}

func Test_readCheckerConfig(t *testing.T) {
	t.Parallel()

	t.Run("{} なら中身を返す", func(t *testing.T) {
		t.Parallel()
		got, err := readCheckerConfig(writeFile(t, filepath.Join(t.TempDir(), "c"), "{}\n"))
		require.NoError(t, err)
		assert.Equal(t, "{}\n", string(got))
	})

	t.Run("{} でなければ errScope", func(t *testing.T) {
		t.Parallel()
		_, err := readCheckerConfig(writeFile(t, filepath.Join(t.TempDir(), "c"), "{\"a\":1}"))
		require.ErrorIs(t, err, errScope)
	})

	t.Run("無ければ errRead", func(t *testing.T) {
		t.Parallel()
		_, err := readCheckerConfig(filepath.Join(t.TempDir(), "none"))
		require.ErrorIs(t, err, errRead)
	})
}

func Test_isEmptyObject(t *testing.T) {
	t.Parallel()

	for in, want := range map[string]bool{
		"{}":           true,
		" {\n\t}\n ":   true,
		"":             false,
		"{ \"a\": 1 }": false,
		"[]":           false,
		"{}{}":         false,
	} {
		t.Run(in, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, want, isEmptyObject(in))
		})
	}
}

func Test_countFiles(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		out  string
		want int
	}{
		"空":         {"", 0},
		"1行1パス":     {"a\nb\n", 2},
		"空行と空白だけの行": {"a\n\n  \nb", 2},
		"色の制御列だけの行": {"\x1b[0m\na\n", 1},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, countFiles(c.out))
		})
	}
}

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

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun/tooltest"
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

	t.Run("設定を確かめ、-dry-run・ls-files・ls-deleted の順に隔離した env で起動し、件数を出して通る", func(t *testing.T) {
		t.Parallel()
		f := coverFake(t, staged("a.yaml", "b.go"), "")
		f.Set(stepDryRun, "out", "a.yaml\n\x1b[0m\n./b.go\n\n")
		var out bytes.Buffer
		require.NoError(t, runConfig(emptyConfig(t), &out, f.Tool(), env))
		assert.Contains(t, out.String(), "editorconfig-check: 走査対象 2 ファイル\n")
		assert.Contains(t, out.String(), "✅ editorconfig coverage: 追跡ファイル 2 件")
		calls := f.Calls()
		assert.Equal(t, []string{"editorconfig-gate dry-run", "editorconfig-gate ls-files", "editorconfig-gate ls-deleted"}, stepsOf(calls))
		for _, c := range calls {
			assert.True(t, strings.HasSuffix(c, " /isolated"), c)
		}
	})

	t.Run("追跡ファイルが在り -dry-run が0件なら errUncovered", func(t *testing.T) {
		t.Parallel()
		for name, c := range map[string]struct{ out, err string }{
			"空":            {},
			"空白と制御列だけ":     {out: " \n\x1b[0m\n"},
			"stderr にだけ出す": {err: "a.yaml\n"},
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				f := coverFake(t, staged("a.yaml"), "")
				f.Set(stepDryRun, "out", c.out)
				f.Set(stepDryRun, "err", c.err)
				var out bytes.Buffer
				require.ErrorIs(t, runConfig(emptyConfig(t), &out, f.Tool(), env), errUncovered)
				assert.Contains(t, out.String(), "editorconfig-check: 走査対象 0 ファイル\n")
			})
		}
	})

	t.Run("追跡ファイルも -dry-run も0件なら、errUncovered より先に errNoInput", func(t *testing.T) {
		t.Parallel()
		f := coverFake(t, "", "")
		require.ErrorIs(t, runConfig(emptyConfig(t), &bytes.Buffer{}, f.Tool(), env), errNoInput)
	})

	t.Run("-dry-run が rc≠0 なら、出力があっても git を起動せずに errTool", func(t *testing.T) {
		t.Parallel()
		f := coverFake(t, staged("a.yaml"), "")
		f.Set(stepDryRun, "out", "a.yaml\n")
		f.Set(stepDryRun, "rc", "1")
		require.ErrorIs(t, runConfig(emptyConfig(t), &bytes.Buffer{}, f.Tool(), env), errTool)
		assert.Equal(t, []string{"editorconfig-gate dry-run"}, stepsOf(f.Calls()))
	})

	t.Run("-dry-run がシグナルで終われば、git を起動せずに errTool", func(t *testing.T) {
		t.Parallel()
		f := coverFake(t, staged("a.yaml"), "")
		f.Set(stepDryRun, "kill", "")
		require.ErrorIs(t, runConfig(emptyConfig(t), &bytes.Buffer{}, f.Tool(), env), errTool)
		assert.Equal(t, []string{"editorconfig-gate dry-run"}, stepsOf(f.Calls()))
	})

	t.Run("道具を起動できなければ errTool", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, runConfig(emptyConfig(t), &bytes.Buffer{}, tooltest.Missing, env), errTool)
	})

	t.Run("設定が {} でなければ、道具を起動せずに errScope", func(t *testing.T) {
		t.Parallel()
		f := newFake(t)
		cfg := writeFile(t, filepath.Join(t.TempDir(), "c.json"), `{"Exclude": ["x"]}`)
		require.ErrorIs(t, runConfig(cfg, &bytes.Buffer{}, f.Tool(), env), errScope)
		assert.Empty(t, f.Calls())
	})

	t.Run("設定が無ければ errRead", func(t *testing.T) {
		t.Parallel()
		f := newFake(t)
		require.ErrorIs(t, runConfig(filepath.Join(t.TempDir(), "none"), &bytes.Buffer{}, f.Tool(), env), errRead)
		assert.Empty(t, f.Calls())
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

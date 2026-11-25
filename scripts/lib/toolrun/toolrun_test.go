package toolrun

import (
	"bytes"
	"context"
	"os/exec"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun/tooltest"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

func TestRun(t *testing.T) {
	t.Parallel()

	t.Run("stdout だけを stdout に、両方を combined に束ね、終了コードを返す", func(t *testing.T) {
		t.Parallel()
		f := tooltest.New(t)
		f.Set("s", "out", "O\n")
		f.Set("s", "err", "E\n")
		f.Set("s", "rc", "3")
		r, err := Run(f.Tool()(context.Background(), t.TempDir(), nil, "s"))
		require.NoError(t, err)
		assert.Equal(t, "O\n", r.Stdout)
		assert.Contains(t, r.Combined, "O\n")
		assert.Contains(t, r.Combined, "E\n")
		assert.Equal(t, 3, r.Code)
	})

	t.Run("stderr だけでも combined に載る", func(t *testing.T) {
		t.Parallel()
		f := tooltest.New(t)
		f.Set("s", "err", "E\n")
		r, err := Run(f.Tool()(context.Background(), t.TempDir(), nil, "s"))
		require.NoError(t, err)
		assert.Empty(t, r.Stdout)
		assert.Equal(t, "E\n", r.Combined)
		assert.Equal(t, 0, r.Code)
	})

	t.Run("起動できなければ ErrTool", func(t *testing.T) {
		t.Parallel()
		_, err := Run(tooltest.Missing(context.Background(), t.TempDir(), nil, "s"))
		require.ErrorIs(t, err, ErrTool)
	})

	t.Run("シグナルで終われば ErrTool で、捕捉した出力を添える", func(t *testing.T) {
		t.Parallel()
		cmd := exec.CommandContext(context.Background(), "/bin/sh", "-c", "echo OUT; kill -9 $$")
		r, err := Run(cmd)
		require.ErrorIs(t, err, ErrTool)
		assert.Contains(t, err.Error(), cmd.Path+" がシグナルで終わりました: signal: killed\nOUT\n: ")
		assert.Equal(t, "OUT\n", r.Stdout)
		assert.Equal(t, "OUT\n", r.Combined)
	})

	t.Run("並行に大量の stdout と stderr を出しても取りこぼさない", func(t *testing.T) {
		t.Parallel()
		cmd := exec.CommandContext(context.Background(), "/bin/sh", "-c", `i=0; while [ $i -lt 2000 ]; do echo o; echo e >&2; i=$((i+1)); done`)
		r, err := Run(cmd)
		require.NoError(t, err)
		assert.Len(t, r.Stdout, 2000*2)
		assert.Len(t, r.Combined, 2000*4)
	})
}

func TestExec(t *testing.T) {
	t.Parallel()

	t.Run("0 以外の終了コードはエラーにせず返し、出力の行き先は cmd のまま", func(t *testing.T) {
		t.Parallel()
		f := tooltest.New(t)
		f.Set("s", "out", "O\n")
		f.Set("s", "rc", "7")
		cmd := f.Tool()(context.Background(), t.TempDir(), nil, "s")
		var out bytes.Buffer
		cmd.Stdout = &out
		code, err := Exec(cmd)
		require.NoError(t, err)
		assert.Equal(t, 7, code)
		assert.Equal(t, "O\n", out.String())
	})

	t.Run("0 で終われば 0", func(t *testing.T) {
		t.Parallel()
		code, err := Exec(tooltest.New(t).Tool()(context.Background(), t.TempDir(), nil, "s"))
		require.NoError(t, err)
		assert.Equal(t, 0, code)
	})

	t.Run("起動できなければ ErrTool で、道具のパスを名指す", func(t *testing.T) {
		t.Parallel()
		cmd := tooltest.Missing(context.Background(), t.TempDir(), nil, "s")
		_, err := Exec(cmd)
		require.ErrorIs(t, err, ErrTool)
		assert.Contains(t, err.Error(), cmd.Path)
	})

	t.Run("シグナルで終われば ErrTool", func(t *testing.T) {
		t.Parallel()
		f := tooltest.New(t)
		f.Set("s", "kill", "")
		_, err := Exec(f.Tool()(context.Background(), t.TempDir(), nil, "s"))
		require.ErrorIs(t, err, ErrTool)
		assert.Contains(t, err.Error(), "シグナル")
	})
}

func Test_classify(t *testing.T) {
	t.Parallel()

	run := func(t *testing.T, step string, set func(*tooltest.Fake)) (*exec.Cmd, error) {
		t.Helper()
		f := tooltest.New(t)
		set(f)
		cmd := f.Tool()(context.Background(), t.TempDir(), nil, step)
		return cmd, cmd.Run()
	}

	t.Run("err が nil なら 0", func(t *testing.T) {
		t.Parallel()
		code, err := classify(&exec.Cmd{}, nil, "D")
		require.NoError(t, err)
		assert.Equal(t, 0, code)
	})

	t.Run("終了コードを返す", func(t *testing.T) {
		t.Parallel()
		cmd, runErr := run(t, "s", func(f *tooltest.Fake) { f.Set("s", "rc", "4") })
		code, err := classify(cmd, runErr, "D")
		require.NoError(t, err)
		assert.Equal(t, 4, code)
	})

	t.Run("シグナルは ErrTool で、detail を末尾に添える", func(t *testing.T) {
		t.Parallel()
		cmd, runErr := run(t, "s", func(f *tooltest.Fake) { f.Set("s", "kill", "") })
		code, err := classify(cmd, runErr, "\nDETAIL")
		require.ErrorIs(t, err, ErrTool)
		assert.Equal(t, 0, code)
		assert.Contains(t, err.Error(), cmd.Path+" がシグナルで終わりました: signal: killed\nDETAIL")
	})

	t.Run("ExitError でないエラーは ErrTool で、detail を添えない", func(t *testing.T) {
		t.Parallel()
		code, err := classify(&exec.Cmd{Path: "/p"}, xerrors.New("boom"), "DETAIL")
		require.ErrorIs(t, err, ErrTool)
		assert.Equal(t, 0, code)
		assert.Contains(t, err.Error(), "/p: boom")
		assert.NotContains(t, err.Error(), "DETAIL")
	})
}

func TestExitCode(t *testing.T) {
	t.Parallel()

	failA, failB, other := xerrors.New("a"), xerrors.New("b"), xerrors.New("c")
	for name, c := range map[string]struct {
		err  error
		want int
	}{
		"nil は 0":              {nil, 0},
		"1つ目の失敗に当たれば 1":        {xerrors.Wrap(failA, "x"), 1},
		"2つ目の失敗に当たれば 1":        {xerrors.Wrap(failB, "x"), 1},
		"失敗に当たらなければ 2":         {xerrors.Wrap(other, "x"), 2},
		"ErrTool は失敗に挙げなければ 2": {xerrors.Wrap(ErrTool, "x"), 2},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, ExitCode(c.err, failA, failB))
		})
	}

	t.Run("失敗を挙げなければ、エラーはすべて 2", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, 2, ExitCode(failA))
	})
}

func TestReport(t *testing.T) {
	t.Parallel()

	t.Run("nil なら何も書かない", func(t *testing.T) {
		t.Parallel()
		var out bytes.Buffer
		Report("tool", nil, &out)
		assert.Empty(t, out.String())
	})

	t.Run("エラーを道具の名前つきの1行で書く", func(t *testing.T) {
		t.Parallel()
		var out bytes.Buffer
		Report("tool", xerrors.Wrap(xerrors.New("b"), "a"), &out)
		assert.Equal(t, "❌ tool: a: b\n", out.String())
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

package tooltest

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// realDir は、symlink を解いた t.TempDir です（偽の道具は物理パスの作業ディレクトリを控える）。
func realDir(t *testing.T) string {
	t.Helper()
	d, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	return d
}

func run(t *testing.T, f *Fake, dir, step string, env []string) (string, int) {
	t.Helper()
	cmd := f.Tool()(context.Background(), dir, env, step)
	out, err := cmd.Output()
	var exit *exec.ExitError
	if err != nil {
		require.ErrorAs(t, err, &exit)
		return string(out), exit.ExitCode()
	}
	return string(out), 0
}

func TestNew(t *testing.T) {
	t.Parallel()

	t.Run("何も置かなければ、出力なしで 0 で終わり、起動を控える", func(t *testing.T) {
		t.Parallel()
		f := New(t)
		dir := realDir(t)
		out, code := run(t, f, dir, "s", []string{"GIT_DIR=/g"})
		assert.Empty(t, out)
		assert.Equal(t, 0, code)
		assert.Equal(t, []string{dir + " s /g"}, f.Calls())
	})

	t.Run("symlink を経た作業ディレクトリは、解いた実パスで控える", func(t *testing.T) {
		t.Parallel()
		f := New(t)
		dir := realDir(t)
		link := filepath.Join(realDir(t), "link")
		require.NoError(t, os.Symlink(dir, link))
		run(t, f, link, "s", nil)
		assert.Equal(t, []string{dir + " s "}, f.Calls())
	})

	t.Run("@<作業ディレクトリの名前> の out と rc は、その作業ディレクトリでだけ段の既定に勝つ", func(t *testing.T) {
		t.Parallel()
		f := New(t)
		f.Set("s", "out", "DEFAULT\n")
		f.Set("s", "rc", "1")
		f.Set("s@here", "out", "HERE\n")
		f.Set("s@here", "rc", "3")
		here := filepath.Join(realDir(t), "here")
		require.NoError(t, os.Mkdir(here, 0o750))
		out, code := run(t, f, here, "s", nil)
		assert.Equal(t, "HERE\n", out)
		assert.Equal(t, 3, code)
		out, code = run(t, f, realDir(t), "s", nil)
		assert.Equal(t, "DEFAULT\n", out)
		assert.Equal(t, 1, code)
	})
}

func TestWithPrelude(t *testing.T) {
	t.Parallel()

	t.Run("断片は作業ディレクトリで、段の出力より前に走り、$ctl と $step を参照できる", func(t *testing.T) {
		t.Parallel()
		f := New(t, WithPrelude(`echo "PRE $step"; pwd -P > "$ctl/$step.where"`))
		f.Set("s", "out", "OUT\n")
		dir := realDir(t)
		out, _ := run(t, f, dir, "s", nil)
		assert.Equal(t, "PRE s\nOUT\n", out)
		assert.Equal(t, dir+"\n", f.Read("s.where"))
	})

	t.Run("断片の前に起動を控え、kill は断片を走らせない", func(t *testing.T) {
		t.Parallel()
		f := New(t, WithPrelude(`echo PRE`))
		f.Set("s", "kill", "")
		out, code := run(t, f, realDir(t), "s", nil)
		assert.Empty(t, out)
		assert.Equal(t, -1, code)
		assert.Len(t, f.Calls(), 1)
	})
}

func TestFake_Set(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		suffix, body string
		out, err     string
		code         int
	}{
		"out は stdout へ": {"out", "O\n", "O\n", "", 0},
		"err は stderr へ": {"err", "E\n", "", "E\n", 0},
		"rc は終了コード":      {"rc", "5", "", "", 5},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := New(t)
			f.Set("s", c.suffix, c.body)
			cmd := f.Tool()(context.Background(), realDir(t), nil, "s")
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			err := cmd.Run()
			code := 0
			var exit *exec.ExitError
			if err != nil {
				require.ErrorAs(t, err, &exit)
				code = exit.ExitCode()
			}
			assert.Equal(t, c.out, stdout.String())
			assert.Equal(t, c.err, stderr.String())
			assert.Equal(t, c.code, code)
		})
	}

	t.Run("kill はシグナルで終わらせる", func(t *testing.T) {
		t.Parallel()
		f := New(t)
		f.Set("s", "kill", "")
		f.Set("s", "out", "O\n")
		out, code := run(t, f, realDir(t), "s", nil)
		assert.Empty(t, out)
		assert.Equal(t, -1, code)
	})

	t.Run("ほかの段には効かない", func(t *testing.T) {
		t.Parallel()
		f := New(t)
		f.Set("s", "rc", "5")
		_, code := run(t, f, realDir(t), "t", nil)
		assert.Equal(t, 0, code)
	})
}

func TestFake_Tool(t *testing.T) {
	t.Parallel()

	t.Run("/bin/sh に段を引数として渡し、作業ディレクトリと env を設定する", func(t *testing.T) {
		t.Parallel()
		f := New(t)
		cmd := f.Tool()(context.Background(), "/d", []string{"A=1"}, "s")
		assert.Equal(t, []string{"/bin/sh", filepath.Join(f.dir, "tool"), "s"}, cmd.Args)
		assert.Equal(t, "/d", cmd.Dir)
		assert.Equal(t, []string{"A=1"}, cmd.Env)
	})

	t.Run("operand は段の後ろに順に渡す", func(t *testing.T) {
		t.Parallel()
		f := New(t)
		cmd := f.Tool()(context.Background(), "/d", nil, "s", "--", "x", "-y")
		assert.Equal(t, []string{"/bin/sh", filepath.Join(f.dir, "tool"), "s", "--", "x", "-y"}, cmd.Args)
	})

	t.Run("ctx の取り消しで道具を止める", func(t *testing.T) {
		t.Parallel()
		f := New(t, WithPrelude(`sleep 5`))
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := f.Tool()(ctx, realDir(t), nil, "s").Run()
		require.Error(t, err)
		assert.Nil(t, f.Calls(), "取り消し済みの ctx では起動しない")
	})
}

func TestWithBase(t *testing.T) {
	t.Parallel()

	t.Run("名前が同じ2つのディレクトリを、base からの相対パスで分けて上書きする", func(t *testing.T) {
		t.Parallel()
		base := realDir(t)
		a, b := filepath.Join(base, "a", "reference"), filepath.Join(base, "b", "reference")
		require.NoError(t, os.MkdirAll(a, 0o750))
		require.NoError(t, os.MkdirAll(b, 0o750))
		f := New(t, WithBase(base))
		f.Set("s@a:reference", "out", "A\n")
		f.Set("s@b:reference", "rc", "4")
		f.Set("s", "out", "DEFAULT\n")
		out, code := run(t, f, a, "s", nil)
		assert.Equal(t, "A\n", out)
		assert.Equal(t, 0, code)
		out, code = run(t, f, b, "s", nil)
		assert.Equal(t, "DEFAULT\n", out)
		assert.Equal(t, 4, code)
	})

	t.Run("名前だけの鍵は、WithBase の下では当たらない", func(t *testing.T) {
		t.Parallel()
		base := realDir(t)
		d := filepath.Join(base, "x", "reference")
		require.NoError(t, os.MkdirAll(d, 0o750))
		f := New(t, WithBase(base))
		f.Set("s@reference", "rc", "9")
		_, code := run(t, f, d, "s", nil)
		assert.Equal(t, 0, code)
	})
}

func TestFake_Args(t *testing.T) {
	t.Parallel()

	t.Run("起動していなければ nil", func(t *testing.T) {
		t.Parallel()
		assert.Nil(t, New(t).Args())
	})

	t.Run("段と operand を起動の順に控える", func(t *testing.T) {
		t.Parallel()
		f := New(t)
		dir := realDir(t)
		_, err := f.Tool()(context.Background(), dir, nil, "a").Output()
		require.NoError(t, err)
		_, err = f.Tool()(context.Background(), dir, nil, "b", "--", "ref").Output()
		require.NoError(t, err)
		assert.Equal(t, []string{"a", "b -- ref"}, f.Args())
	})
}

func TestFake_Calls(t *testing.T) {
	t.Parallel()

	t.Run("起動していなければ nil", func(t *testing.T) {
		t.Parallel()
		assert.Nil(t, New(t).Calls())
	})

	t.Run("起動の順に控える", func(t *testing.T) {
		t.Parallel()
		f := New(t)
		dir := realDir(t)
		run(t, f, dir, "a", nil)
		run(t, f, dir, "b", []string{"GIT_DIR=/g"})
		assert.Equal(t, []string{dir + " a ", dir + " b /g"}, f.Calls())
	})
}

func TestFake_Read(t *testing.T) {
	t.Parallel()

	t.Run("制御用のディレクトリのファイルを読む", func(t *testing.T) {
		t.Parallel()
		f := New(t)
		f.Set("s", "x", "BODY")
		assert.Equal(t, "BODY", f.Read("s.x"))
	})
}

func TestMissing(t *testing.T) {
	t.Parallel()

	t.Run("在らない道具を作業ディレクトリと env つきで起動し、起動に失敗する", func(t *testing.T) {
		t.Parallel()
		cmd := Missing(context.Background(), "/d", []string{"A=1"}, "s")
		assert.Equal(t, "/d", cmd.Dir)
		assert.Equal(t, []string{"A=1"}, cmd.Env)
		cmd.Dir = t.TempDir()
		err := cmd.Run()
		require.Error(t, err)
		var exit *exec.ExitError
		assert.NotErrorAs(t, err, &exit, "起動そのものが失敗する")
	})
}

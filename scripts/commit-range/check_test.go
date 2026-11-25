package main

import (
	"bytes"
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun/tooltest"
)

func environ() ([]string, error) { return os.Environ(), nil }

func Test_runCheck(t *testing.T) {
	t.Parallel()

	r := newRepo(t)
	base := r.git("rev-parse", "HEAD")
	r.git("switch", "-q", "-c", "topic")
	r.commit("a")
	r.commit("b")
	r.git("switch", "-q", "main")
	r.commit("main-1")
	r.git("merge", "-q", "--no-ff", "-m", "merge", "topic")
	head := r.git("rev-parse", "HEAD")
	r.git("tag", "-a", "-m", "annotated", "v1", base)
	tree := r.git("rev-parse", "HEAD^{tree}")
	blob := r.git("hash-object", "-w", "--stdin")

	for name, c := range map[string]struct{ from, want string }{
		"sha の範囲は merge commit を数えない": {base, "検査対象: 3 commit (" + base + ".." + head + ")\n"},
		"短い sha":                     {base[:8], "検査対象: 3 commit (" + base[:8] + ".." + head + ")\n"},
		"annotated tag は commit へ剥く": {"v1", "検査対象: 3 commit (v1.." + head + ")\n"},
		"first parent からは topic の commit の2件（merge は数えない）": {"HEAD~1", "検査対象: 2 commit (HEAD~1.." + head + ")\n"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			require.NoError(t, runCheck([]string{"-from", c.from, "-to", head}, &out, command, environ, r.dir))
			assert.Equal(t, c.want, out.String())
		})
	}

	t.Run("範囲が0件（起点と終点が同じ）なら errNoCommits で、件数を出さない", func(t *testing.T) {
		t.Parallel()
		var out bytes.Buffer
		err := runCheck([]string{"-from", head, "-to", "HEAD"}, &out, command, environ, r.dir)
		require.ErrorIs(t, err, errNoCommits)
		assert.Contains(t, err.Error(), "base="+head+" head=HEAD")
		assert.Empty(t, out.String())
	})

	t.Run("merge commit だけの範囲は errNoCommits（commitlint が何も検査しないまま通らないように）", func(t *testing.T) {
		t.Parallel()
		m := newRepo(t)
		m.git("switch", "-q", "-c", "topic")
		m.commit("a")
		m.git("switch", "-q", "main")
		m.commit("main-1")
		before := m.git("rev-parse", "HEAD")
		m.git("merge", "-q", "--no-ff", "-m", "merge", "topic")
		m.git("switch", "-q", "-c", "only-merge", before)
		m.git("merge", "-q", "--no-ff", "-m", "merge", "topic")
		require.ErrorIs(t, runCheck([]string{"-from", "main", "-to", "only-merge"}, new(bytes.Buffer), command, environ, m.dir), errNoCommits)
	})

	t.Run("終点が起点の祖先でも0件で errNoCommits", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, runCheck([]string{"-from", head, "-to", base}, new(bytes.Buffer), command, environ, r.dir), errNoCommits)
	})

	for name, args := range map[string][]string{
		"在らない起点":     {"-from", "origin/does-not-exist", "-to", head},
		"blob を指す起点": {"-from", blob, "-to", head},
		"空白を含む起点":    {"-from", "HEAD HEAD", "-to", head},
		"在らない終点":     {"-from", base, "-to", "does-not-exist"},
		"tree を指す起点": {"-from", tree, "-to", head},
	} {
		t.Run(name+"は errUnresolved", func(t *testing.T) {
			t.Parallel()
			err := runCheck(args, new(bytes.Buffer), command, environ, r.dir)
			require.ErrorIs(t, err, errUnresolved)
			require.NotErrorIs(t, err, errNoCommits, "解決できないことを0件に化けさせない")
		})
	}

	for name, c := range map[string]struct {
		args  []string
		calls int
	}{
		"-from が `-` で始まるなら git を起動せずに":   {[]string{"-from", "--all", "-to", "HEAD"}, 0},
		"-from が改行を含むなら git を起動せずに":       {[]string{"-from", "HEAD\nHEAD", "-to", "HEAD"}, 0},
		"-to が `-` で始まるなら from の解決の1回だけで": {[]string{"-from", "HEAD~1", "-to", "--all"}, 1},
		"-to が改行を含むなら from の解決の1回だけで":     {[]string{"-from", "HEAD~1", "-to", "HEAD\nHEAD"}, 1},
	} {
		t.Run(name+" errUnresolved", func(t *testing.T) {
			t.Parallel()
			f := tooltest.New(t)
			f.Set(stepResolve, "out", base+" commit\n")
			require.ErrorIs(t, runCheck(c.args, new(bytes.Buffer), f.Tool(), environ, r.dir), errUnresolved)
			assert.Len(t, f.Calls(), c.calls)
		})
	}

	t.Run("数えた結果が 0 なら errNoCommits", func(t *testing.T) {
		t.Parallel()
		f := tooltest.New(t)
		f.Set(stepResolve, "out", base+" commit\n")
		f.Set(stepCount, "out", "0\n")
		require.ErrorIs(t, runCheck([]string{"-from", base, "-to", head}, new(bytes.Buffer), f.Tool(), environ, r.dir), errNoCommits)
	})

	t.Run("起点が空なら git を起動せずに errNoBase", func(t *testing.T) {
		t.Parallel()
		f := tooltest.New(t)
		require.ErrorIs(t, runCheck([]string{"-from", "", "-to", "HEAD"}, new(bytes.Buffer), f.Tool(), environ, r.dir), errNoBase)
		assert.Nil(t, f.Calls())
	})

	for name, args := range map[string][]string{
		"-to が無い":   {"-from", base},
		"-to が空":    {"-from", base, "-to", ""},
		"余分な引数":     {"-from", base, "-to", head, "x"},
		"知らない flag": {"-x"},
	} {
		t.Run(name+"なら git を起動せずに errUsage", func(t *testing.T) {
			t.Parallel()
			f := tooltest.New(t)
			require.ErrorIs(t, runCheck(args, new(bytes.Buffer), f.Tool(), environ, r.dir), errUsage)
			assert.Nil(t, f.Calls())
		})
	}

	t.Run("切り離した env を作れなければ git を起動せずに errGit", func(t *testing.T) {
		t.Parallel()
		f := tooltest.New(t)
		failing := func() ([]string, error) { return nil, errors.New("x") }
		require.ErrorIs(t, runCheck([]string{"-from", base, "-to", head}, new(bytes.Buffer), f.Tool(), failing, r.dir), errGit)
		assert.Nil(t, f.Calls())
	})

	t.Run("リポジトリでなければ errGit", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, runCheck([]string{"-from", "HEAD~1", "-to", "HEAD"}, new(bytes.Buffer), command, environ, t.TempDir()), errGit)
	})

	t.Run("git を起動できなければ errTool", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, runCheck([]string{"-from", base, "-to", head}, new(bytes.Buffer), tooltest.Missing, environ, r.dir), toolrun.ErrTool)
	})
}

func Test_resolve(t *testing.T) {
	t.Parallel()

	r := newRepo(t)
	head := r.git("rev-parse", "HEAD")

	t.Run("commit の完全な sha を返す", func(t *testing.T) {
		t.Parallel()
		got, err := resolve(command, os.Environ(), r.dir, "HEAD")
		require.NoError(t, err)
		assert.Equal(t, head, got)
	})

	t.Run("rev を commit へ剥く1行を stdin で渡す", func(t *testing.T) {
		t.Parallel()
		f := tooltest.New(t, tooltest.WithPrelude(`cat > "$ctl/stdin.txt"`))
		f.Set(stepResolve, "out", head+" commit\n")
		got, err := resolve(f.Tool(), nil, r.dir, "origin/main")
		require.NoError(t, err)
		assert.Equal(t, head, got)
		assert.Equal(t, "origin/main^{commit}\n", f.Read("stdin.txt"))
	})

	t.Run("空は git を起動せずに errUnresolved", func(t *testing.T) {
		t.Parallel()
		f := tooltest.New(t)
		_, err := resolve(f.Tool(), nil, r.dir, "")
		require.ErrorIs(t, err, errUnresolved)
		assert.Nil(t, f.Calls())
	})

	t.Run("git の出力が commit でなければ errUnresolved", func(t *testing.T) {
		t.Parallel()
		f := tooltest.New(t)
		f.Set(stepResolve, "out", head+" tree\n")
		_, err := resolve(f.Tool(), nil, r.dir, "HEAD")
		require.ErrorIs(t, err, errUnresolved)
	})

	t.Run("git が rc≠0 なら errGit", func(t *testing.T) {
		t.Parallel()
		f := tooltest.New(t)
		f.Set(stepResolve, "rc", "128")
		_, err := resolve(f.Tool(), nil, r.dir, "HEAD")
		require.ErrorIs(t, err, errGit)
	})
}

func Test_count(t *testing.T) {
	t.Parallel()

	r := newRepo(t)
	base := r.git("rev-parse", "HEAD")
	r.commit("a")
	head := r.git("rev-parse", "HEAD")

	t.Run("from..to の commit を数える", func(t *testing.T) {
		t.Parallel()
		n, err := count(command, os.Environ(), r.dir, base, head)
		require.NoError(t, err)
		assert.Equal(t, 1, n)
	})

	t.Run("stdin で ^from と to を渡す", func(t *testing.T) {
		t.Parallel()
		f := tooltest.New(t, tooltest.WithPrelude(`cat > "$ctl/stdin.txt"`))
		f.Set(stepCount, "out", "3\n")
		n, err := count(f.Tool(), nil, r.dir, base, head)
		require.NoError(t, err)
		assert.Equal(t, 3, n)
		assert.Equal(t, "^"+base+"\n"+head+"\n", f.Read("stdin.txt"))
	})

	for name, out := range map[string]string{
		"空":      "",
		"数でない":   "x\n",
		"負の数":    "-1\n",
		"2つの数":   "1 2\n",
		"桁あふれの数": "99999999999999999999\n",
	} {
		t.Run("出力が"+name+"なら errGit", func(t *testing.T) {
			t.Parallel()
			f := tooltest.New(t)
			f.Set(stepCount, "out", out)
			_, err := count(f.Tool(), nil, r.dir, base, head)
			require.ErrorIs(t, err, errGit)
		})
	}

	t.Run("出力の前後の空白は落として数える", func(t *testing.T) {
		t.Parallel()
		f := tooltest.New(t)
		f.Set(stepCount, "out", " 3 \n")
		n, err := count(f.Tool(), nil, r.dir, base, head)
		require.NoError(t, err)
		assert.Equal(t, 3, n)
	})

	t.Run("rc≠0 なら errGit", func(t *testing.T) {
		t.Parallel()
		f := tooltest.New(t)
		f.Set(stepCount, "out", "0\n")
		f.Set(stepCount, "rc", "128")
		_, err := count(f.Tool(), nil, r.dir, base, head)
		require.ErrorIs(t, err, errGit)
	})
}

func Test_gitOutput(t *testing.T) {
	t.Parallel()

	t.Run("stdout を返す", func(t *testing.T) {
		t.Parallel()
		f := tooltest.New(t)
		f.Set(stepResolve, "out", "OUT\n")
		f.Set(stepResolve, "err", "ERR\n")
		got, err := gitOutput(f.Tool(), nil, t.TempDir(), stepResolve, "")
		require.NoError(t, err)
		assert.Equal(t, "OUT\n", got)
	})

	t.Run("rc≠0 なら段と rc と出力を添えて errGit", func(t *testing.T) {
		t.Parallel()
		f := tooltest.New(t)
		f.Set(stepCount, "err", "fatal: bad revision\n")
		f.Set(stepCount, "rc", "128")
		_, err := gitOutput(f.Tool(), nil, t.TempDir(), stepCount, "")
		require.ErrorIs(t, err, errGit)
		assert.Contains(t, err.Error(), "git（count）が rc=128")
		assert.Contains(t, err.Error(), "fatal: bad revision")
	})

	t.Run("起動できなければ errTool", func(t *testing.T) {
		t.Parallel()
		_, err := gitOutput(tooltest.Missing, nil, t.TempDir(), stepCount, "")
		require.ErrorIs(t, err, toolrun.ErrTool)
	})
}

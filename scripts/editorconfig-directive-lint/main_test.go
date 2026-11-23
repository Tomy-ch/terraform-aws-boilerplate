package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/gitenv"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const (
	disable = "// " + prefix + "disable -- 理由。撤回: 条件"
	enable  = "// " + prefix + "enable"
)

func fixedLister(names []string, err error) listFunc {
	return func() ([]string, error) { return names, err }
}

func unexpectedLister(t *testing.T) listFunc {
	t.Helper()
	return func() ([]string, error) {
		t.Errorf("-files を渡したのに git の一覧を読んだ")
		return nil, errors.New("unexpected")
	}
}

func writeFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	var list []string
	for name, body := range files {
		path := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
		list = append(list, path)
	}
	listPath := filepath.Join(dir, "files.txt")
	require.NoError(t, os.WriteFile(listPath, []byte(strings.Join(list, "\n")+"\n"), 0o600))
	return listPath
}

func Test_run(t *testing.T) {
	t.Parallel()

	t.Run("閉じた範囲だけなら通り、見たファイルと印の数を出す", func(t *testing.T) {
		t.Parallel()
		list := writeFiles(t, map[string]string{
			"a.go": "package a\n" + disable + "\nx\n" + enable + "\n",
			"b.go": "package b\n",
		})
		var out bytes.Buffer
		require.NoError(t, run([]string{"-files", list}, &out, unexpectedLister(t)))
		assert.Contains(t, out.String(), "2 ファイル / 印 2 行")
	})

	t.Run("閉じない範囲があれば違反を出して失敗する", func(t *testing.T) {
		t.Parallel()
		list := writeFiles(t, map[string]string{"a.go": "package a\n" + disable + "\nx\n"})
		var out bytes.Buffer
		err := run([]string{"-files", list}, &out, unexpectedLister(t))
		require.ErrorIs(t, err, errViolations)
		assert.Contains(t, out.String(), "閉じていない")
	})

	t.Run("一覧の指定を省けば、追跡ファイルのうち作業ツリーに在るものを見る", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		a := filepath.Join(dir, "a.go")
		require.NoError(t, os.WriteFile(a, []byte("package a\n"+disable+"\n"+enable+"\n"), 0o600))
		var out bytes.Buffer
		require.NoError(t, run(nil, &out, fixedLister([]string{a, filepath.Join(dir, "gone.go")}, nil)))
		assert.Contains(t, out.String(), "1 ファイル / 印 2 行")
	})

	t.Run("一覧の指定を省いて、追跡ファイルの一覧を得られなければ番兵で返す", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, run(nil, &bytes.Buffer{}, fixedLister(nil, errors.New("boom"))), errUnreadable)
	})

	t.Run("一覧の指定を省いて、作業ツリーに在るものが0件なら番兵で返す", func(t *testing.T) {
		t.Parallel()
		err := run(nil, &bytes.Buffer{}, fixedLister([]string{filepath.Join(t.TempDir(), "gone.go")}, nil))
		require.ErrorIs(t, err, errNoTargets)
	})

	t.Run("知らない引数と余分な引数は違反ではなく使い方の誤りで返す", func(t *testing.T) {
		t.Parallel()
		for _, args := range [][]string{{"-bogus"}, {"extra"}} {
			err := run(args, &bytes.Buffer{}, unexpectedLister(t))
			require.ErrorIs(t, err, errUsage)
			assert.NotErrorIs(t, err, errViolations)
		}
	})

	t.Run("一覧が空なら番兵で返す", func(t *testing.T) {
		t.Parallel()
		list := filepath.Join(t.TempDir(), "files.txt")
		require.NoError(t, os.WriteFile(list, []byte("\n"), 0o600))
		err := run([]string{"-files", list}, &bytes.Buffer{}, unexpectedLister(t))
		require.ErrorIs(t, err, errNoTargets)
	})

	t.Run("一覧を読めなければ番兵で返す", func(t *testing.T) {
		t.Parallel()
		err := run([]string{"-files", filepath.Join(t.TempDir(), "missing.txt")}, &bytes.Buffer{}, unexpectedLister(t))
		require.ErrorIs(t, err, errUnreadable)
	})

	t.Run("一覧に載ったファイルを読めなければ番兵で返す", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		list := filepath.Join(dir, "files.txt")
		require.NoError(t, os.WriteFile(list, []byte(filepath.Join(dir, "gone.go")+"\n"), 0o600))
		err := run([]string{"-files", list}, &bytes.Buffer{}, unexpectedLister(t))
		require.ErrorIs(t, err, errUnreadable)
	})
}

func Test_exitCode(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		err  error
		want int
	}{
		"エラーが無い":    {want: 0},
		"検査対象を読めない": {err: xerrors.Wrapf(errUnreadable, "x"), want: 2},
		"検査対象が0件":   {err: xerrors.Wrapf(errNoTargets, "x"), want: 2},
		"違反がある":     {err: xerrors.Wrapf(errViolations, "1 件"), want: 1},
		"使い方の誤り":    {err: xerrors.Wrapf(errUsage, "x"), want: 2},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, exitCode(tt.err))
		})
	}
}

func Test_readList(t *testing.T) {
	t.Parallel()

	t.Run("空行を捨てて残りを返す", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "files.txt")
		require.NoError(t, os.WriteFile(path, []byte("a\n\n b \n"), 0o600))
		paths, err := readList(path)
		require.NoError(t, err)
		assert.Equal(t, []string{"a", "b"}, paths)
	})

	t.Run("空行だけなら番兵で返す", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(t.TempDir(), "files.txt")
		require.NoError(t, os.WriteFile(path, []byte("\n\n"), 0o600))
		_, err := readList(path)
		require.ErrorIs(t, err, errNoTargets)
	})
}

func Test_check(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		body  string
		lines []int
		count int
	}{
		"印が無い": {body: "a\nb\n"},
		"閉じた範囲が2つ": {
			body:  disable + "\n" + enable + "\n" + disable + "\n" + enable + "\n",
			count: 4,
		},
		"閉じない範囲は開いた行で報告する": {
			body:  "a\n" + disable + "\nb\n",
			lines: []int{2},
			count: 1,
		},
		"範囲の中でまた開く": {
			body:  disable + "\n" + disable + "\n" + enable + "\n",
			lines: []int{2},
			count: 3,
		},
		"入れ子のまま閉じなければ最初に開いた行で報告する": {
			body:  disable + "\n" + disable + "\n",
			lines: []int{2, 1},
			count: 2,
		},
		"開いていない範囲を閉じる": {
			body:  enable + "\n",
			lines: []int{1},
			count: 1,
		},
		"理由が無い": {
			body:  "// " + prefix + "disable\n" + enable + "\n",
			lines: []int{1},
			count: 2,
		},
		"理由はあるが撤回条件が無い": {
			body:  "// " + prefix + "disable -- 理由だけ\n" + enable + "\n",
			lines: []int{1},
			count: 2,
		},
		"ファイル単位の印": {
			body:  "// " + prefix + "disable-file\n",
			lines: []int{1},
			count: 1,
		},
		"行単位の印": {
			body:  "x // " + prefix + "disable-line\n",
			lines: []int{1},
			count: 1,
		},
		"次の行の印": {
			body:  "// " + prefix + "disable-next-line\nx\n",
			lines: []int{1},
			count: 1,
		},
		"散文の中の引用も印として数える": {
			body:  "## 抑止は `// " + prefix + "disable` / `-enable` で囲む\nx\n",
			lines: []int{1, 1},
			count: 1,
		},
		"1行に2つの印": {
			body:  disable + " " + enable + "\n",
			lines: []int{1, 1},
			count: 1,
		},
		"知らない語の印": {
			body:  "// " + prefix + "pause\n",
			lines: []int{1},
			count: 1,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			findings, count := check("f", []byte(tt.body))
			var lines []int
			for _, f := range findings {
				lines = append(lines, f.Line)
			}
			assert.Equal(t, tt.lines, lines)
			assert.Equal(t, tt.count, count)
		})
	}
}

func Test_hasReason(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		tail string
		want bool
	}{
		"理由と撤回条件": {tail: " -- 理由。撤回: 条件", want: true},
		"区切りが無い":  {tail: " 理由。撤回: 条件"},
		"区切りの後が空": {tail: " -- "},
		"撤回条件が無い": {tail: " -- 理由"},
		"何も無い":    {tail: ""},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, hasReason(tt.tail))
		})
	}
}

func Test_existingFiles(t *testing.T) {
	t.Parallel()

	t.Run("作業ツリーに無いものだけを外し、順序を保つ", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
		require.NoError(t, os.WriteFile(a, nil, 0o600))
		require.NoError(t, os.Mkdir(b, 0o750))
		got, err := existingFiles(fixedLister([]string{b, filepath.Join(dir, "gone"), a}, nil))
		require.NoError(t, err)
		assert.Equal(t, []string{b, a}, got, "ディレクトリは外さず、読むときに落とす")
	})

	t.Run("ファイルをディレクトリとして名指したパス（ENOTDIR）は外さず、読むときに落とす", func(t *testing.T) {
		t.Parallel()
		file := filepath.Join(t.TempDir(), "file")
		require.NoError(t, os.WriteFile(file, nil, 0o600))
		notDir := filepath.Join(file, "x")
		got, err := existingFiles(fixedLister([]string{notDir}, nil))
		require.NoError(t, err)
		assert.Equal(t, []string{notDir}, got)
		require.ErrorIs(t, run(nil, &bytes.Buffer{}, fixedLister([]string{notDir}, nil)), errUnreadable)
	})

	t.Run("壊れた symlink は作業ツリーに無いものとして外す", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		link := filepath.Join(dir, "link")
		require.NoError(t, os.Symlink(filepath.Join(dir, "none"), link))
		_, err := existingFiles(fixedLister([]string{link}, nil))
		require.ErrorIs(t, err, errNoTargets)
	})

	t.Run("一覧が0件なら errNoTargets", func(t *testing.T) {
		t.Parallel()
		_, err := existingFiles(fixedLister(nil, nil))
		require.ErrorIs(t, err, errNoTargets)
	})

	t.Run("一覧を得られなければ errUnreadable", func(t *testing.T) {
		t.Parallel()
		_, err := existingFiles(fixedLister(nil, errors.New("boom")))
		require.ErrorIs(t, err, errUnreadable)
		assert.Contains(t, err.Error(), "boom")
	})
}

func Test_gitFiles(t *testing.T) {
	t.Parallel()

	t.Run("追跡ファイルを、非 ASCII の名前も引用せずに返す", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		git := func(args ...string) {
			t.Helper()
			cmd := exec.CommandContext(t.Context(), "git", args...)
			cmd.Dir = dir
			out, err := cmd.CombinedOutput()
			require.NoError(t, err, string(out))
		}
		git("init", "-q")
		for _, name := range []string{"a.go", "日本語.md", "untracked.go"} {
			require.NoError(t, os.WriteFile(filepath.Join(dir, name), nil, 0o600))
		}
		git("add", "a.go", "日本語.md")
		var asked bool
		got, err := gitFiles(dir, func() ([]string, error) { asked = true; return gitEnv() })
		require.NoError(t, err)
		assert.Equal(t, []string{"a.go", "日本語.md"}, got)
		assert.True(t, asked, "隔離した env を使う")
	})

	t.Run("isolated が返した env で git を起動する", func(t *testing.T) {
		t.Parallel()
		other := t.TempDir()
		for _, args := range [][]string{{"init", "-q"}, {"add", "other.go"}} {
			if args[0] == "add" {
				require.NoError(t, os.WriteFile(filepath.Join(other, "other.go"), nil, 0o600))
			}
			cmd := exec.CommandContext(t.Context(), "git", args...)
			cmd.Dir = other
			out, err := cmd.CombinedOutput()
			require.NoError(t, err, string(out))
		}
		got, err := gitFiles(t.TempDir(), func() ([]string, error) {
			return append(os.Environ(), "GIT_DIR="+filepath.Join(other, ".git")), nil
		})
		require.NoError(t, err)
		assert.Equal(t, []string{"other.go"}, got)
	})

	t.Run("隔離した env を得られなければ git を起動せずにエラー", func(t *testing.T) {
		t.Parallel()
		_, err := gitFiles(t.TempDir(), func() ([]string, error) { return nil, errors.New("boom") })
		require.ErrorContains(t, err, "boom")
	})

	t.Run("リポジトリの外ならエラー", func(t *testing.T) {
		t.Parallel()
		_, err := gitFiles(filepath.Join(t.TempDir(), "none"), gitEnv)
		require.Error(t, err)
	})
}

func Test_splitNUL(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		in   string
		want []string
	}{
		"空":        {"", nil},
		"末尾の NUL":  {"a\x00b c\x00", []string{"a", "b c"}},
		"改行を含む名前":  {"a\nb\x00", []string{"a\nb"}},
		"連続した NUL": {"a\x00\x00b", []string{"a", "b"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, splitNUL(c.in))
		})
	}
}

func Test_gitEnv(t *testing.T) {
	t.Parallel()

	t.Run("このプロセスの PATH を残す", func(t *testing.T) {
		t.Parallel()
		env, err := gitEnv()
		require.NoError(t, err)
		assert.Contains(t, env, "PATH="+os.Getenv("PATH"))
	})
}

func Test_gitEnvFrom(t *testing.T) {
	t.Parallel()

	t.Run("GIT_DIR を外し、ほかは残す", func(t *testing.T) {
		t.Parallel()
		env, err := gitEnvFrom([]string{"PATH=" + os.Getenv("PATH"), "GIT_DIR=/x", "A=1"})
		require.NoError(t, err)
		assert.NotContains(t, env, "GIT_DIR=/x")
		assert.Contains(t, env, "A=1")
	})

	t.Run("git が一覧を返せなければエラーを返す", func(t *testing.T) {
		t.Parallel()
		home := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(home, ".gitconfig"), []byte("[broken\n"), 0o600))
		_, err := gitEnvFrom([]string{"PATH=" + os.Getenv("PATH"), "HOME=" + home, "XDG_CONFIG_HOME=" + home})
		require.ErrorIs(t, err, gitenv.ErrNames)
	})
}

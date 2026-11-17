package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const (
	disable = "// " + prefix + "disable -- 理由。撤回: 条件"
	enable  = "// " + prefix + "enable"
)

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
		require.NoError(t, run([]string{"-files", list}, &out))
		assert.Contains(t, out.String(), "2 ファイル / 印 2 行")
	})

	t.Run("閉じない範囲があれば違反を出して失敗する", func(t *testing.T) {
		t.Parallel()
		list := writeFiles(t, map[string]string{"a.go": "package a\n" + disable + "\nx\n"})
		var out bytes.Buffer
		err := run([]string{"-files", list}, &out)
		require.ErrorIs(t, err, errViolations)
		assert.Contains(t, out.String(), "閉じていない")
	})

	t.Run("一覧の指定を省けば読めない番兵で返す", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, run(nil, &bytes.Buffer{}), errUnreadable)
	})

	t.Run("知らない引数は違反ではなく解釈のエラーで返す", func(t *testing.T) {
		t.Parallel()
		err := run([]string{"-bogus"}, &bytes.Buffer{})
		require.Error(t, err)
		assert.NotErrorIs(t, err, errViolations)
	})

	t.Run("一覧が空なら番兵で返す", func(t *testing.T) {
		t.Parallel()
		list := filepath.Join(t.TempDir(), "files.txt")
		require.NoError(t, os.WriteFile(list, []byte("\n"), 0o600))
		err := run([]string{"-files", list}, &bytes.Buffer{})
		require.ErrorIs(t, err, errNoTargets)
	})

	t.Run("一覧を読めなければ番兵で返す", func(t *testing.T) {
		t.Parallel()
		err := run([]string{"-files", filepath.Join(t.TempDir(), "missing.txt")}, &bytes.Buffer{})
		require.ErrorIs(t, err, errUnreadable)
	})

	t.Run("一覧に載ったファイルを読めなければ番兵で返す", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		list := filepath.Join(dir, "files.txt")
		require.NoError(t, os.WriteFile(list, []byte(filepath.Join(dir, "gone.go")+"\n"), 0o600))
		err := run([]string{"-files", list}, &bytes.Buffer{})
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
		"引数の解釈の失敗":  {err: xerrors.New("x"), want: 1},
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
		// 入れ子の印で開き直さない。開き直せば、閉じない範囲を後の行で報告する。
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

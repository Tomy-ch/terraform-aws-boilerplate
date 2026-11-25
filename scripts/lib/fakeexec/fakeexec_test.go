package fakeexec

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fatalRecorder は、Fatalf の呼び出しを控え、テストを止めずに戻ります。
type fatalRecorder struct {
	testing.TB

	fatals []string
}

func (r *fatalRecorder) Helper() {}

func (r *fatalRecorder) Fatalf(format string, args ...any) {
	r.fatals = append(r.fatals, fmt.Sprintf(format, args...))
}

// WriteString と Close の失敗の分岐はテストから到達できない（O_EXCL なので /dev/full へ書けない）。未検証である。
func TestWrite(t *testing.T) {
	t.Parallel()

	const body = "#!/bin/sh\necho \"$0 $*\"\n"

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		for _, perm := range []os.FileMode{0o700, 0o755, 0o500} {
			t.Run(strconv.FormatUint(uint64(perm), 8), func(t *testing.T) {
				t.Parallel()

				path := filepath.Join(t.TempDir(), "tool")
				Write(t, path, body, perm)

				got, err := os.ReadFile(path)
				require.NoError(t, err)
				assert.Equal(t, body, string(got))

				info, err := os.Stat(path)
				require.NoError(t, err)
				assert.Equal(t, perm, info.Mode().Perm())

				out, err := exec.CommandContext(t.Context(), path, "a", "b").Output()
				require.NoError(t, err)
				assert.Equal(t, path+" a b\n", string(out))
			})
		}
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("親ディレクトリが無ければ失敗させる", func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "missing", "tool")
			r := &fatalRecorder{TB: t}
			Write(r, path, body, 0o700)

			require.Len(t, r.fatals, 1)
			assert.Contains(t, r.fatals[0], "を書けません")
			assert.NoFileExists(t, path)
		})

		t.Run("既に在るファイルを上書きせず失敗させる", func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "tool")
			require.NoError(t, os.WriteFile(path, []byte("keep\n"), 0o600))
			r := &fatalRecorder{TB: t}
			Write(r, path, body, 0o700)

			require.Len(t, r.fatals, 1)
			assert.Contains(t, r.fatals[0], "を書けません")
			got, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, "keep\n", string(got))
		})

		for name, b := range map[string]string{
			"本体が空なら書かずに失敗させる":          "",
			"本体が #! で始まらなければ書かずに失敗させる": "echo hi\n",
			"本体が # だけなら書かずに失敗させる":      "#\n",
			"本体が # ! なら書かずに失敗させる":      "# !\n",
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				path := filepath.Join(t.TempDir(), "tool")
				r := &fatalRecorder{TB: t}
				Write(r, path, b, 0o700)

				require.Len(t, r.fatals, 1)
				assert.Contains(t, r.fatals[0], "#! で始まりません")
				assert.NoFileExists(t, path)
			})
		}

		for _, perm := range []os.FileMode{0o600, 0o070, 0o007} {
			t.Run("所有者の実行権の無い "+strconv.FormatUint(uint64(perm), 8)+" は書かずに失敗させる", func(t *testing.T) {
				t.Parallel()

				path := filepath.Join(t.TempDir(), "tool")
				r := &fatalRecorder{TB: t}
				Write(r, path, body, perm)

				require.Len(t, r.fatals, 1)
				assert.Contains(t, r.fatals[0], "所有者の実行権を持ちません")
				_, err := os.Stat(path)
				require.ErrorIs(t, err, fs.ErrNotExist)
			})
		}
	})

	// Linux でだけ意味を持つ（macOS は ETXTBSY を返さないので、ここが緑でも確かめたことにならない）。
	t.Run("並行に書いた偽の道具を直に exec できる", func(t *testing.T) {
		t.Parallel()

		for i := range 32 {
			t.Run(strconv.Itoa(i), func(t *testing.T) {
				t.Parallel()

				dir := t.TempDir()
				for j := range 16 {
					path := filepath.Join(dir, "tool"+strconv.Itoa(j))
					Write(t, path, body, 0o700)

					out, err := exec.CommandContext(t.Context(), path).Output()
					require.NoError(t, err)
					assert.Equal(t, path+" \n", string(out))
				}
			})
		}
	})
}

// t.Parallel を呼ばない。ForkLock はプロセスで1つなので、並行のテストの fork が書き込みロックを握ると TryLock の
// 結果が揺れる。並行のテストが走り出す前に終える。
func Test_write(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tool")
	const body = "#!/bin/sh\n"

	called := false
	err := write(path, body, 0o700, func() {
		called = true

		assert.False(t, syscall.ForkLock.TryLock(), "閉じる前に ForkLock の読みロックが外れている")

		got, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, body, string(got))
	})
	require.NoError(t, err)
	require.True(t, called)

	require.True(t, syscall.ForkLock.TryLock(), "書き終えた後も ForkLock の読みロックが残っている")
	syscall.ForkLock.Unlock()
}

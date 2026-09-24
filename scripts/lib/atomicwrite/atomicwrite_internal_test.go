package atomicwrite

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 引き継ぎが起きたかを見るため、既存ファイルのモードとは必ず異なる値にします。
const fallbackPerm fs.FileMode = 0o644

func Test_modeOf(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string]fs.FileMode{
			"所有者だけが読み書きできる": 0o600,
			"実行ビットが立っている":   0o755,
			"誰も書き込めない":      0o444,
		}

		for name, perm := range tests {
			t.Run(name+"場合、fallback ではなく既存のモードを返す", func(t *testing.T) {
				t.Parallel()

				path := filepath.Join(t.TempDir(), "f")
				require.NoError(t, os.WriteFile(path, []byte("x"), perm))
				// umask が落とすビットがあるため、作成時の perm だけには頼りません。
				require.NoError(t, os.Chmod(path, perm))

				assert.Equal(t, perm, modeOf(path, fallbackPerm))
			})
		}

		// 型ビットを落としているのはここだけが見られます。通常ファイルでは
		// Mode() と Perm() が一致するため、`.Perm()` を外しても緑のままになります。
		t.Run("ディレクトリの場合、型ビットを落とした許可ビットだけを返す", func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			require.NoError(t, os.Chmod(dir, 0o700))

			assert.Equal(t, fs.FileMode(0o700), modeOf(dir, fallbackPerm))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("存在しないパスの場合、fallback を返す", func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, fallbackPerm, modeOf(filepath.Join(t.TempDir(), "missing"), fallbackPerm))
		})

		t.Run("空のパスの場合、fallback を返す", func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, fallbackPerm, modeOf("", fallbackPerm))
		})
	})
}

func Test_sortedPaths(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			changes map[string]string
			want    []string
		}{
			"複数件の場合、昇順で返す": {
				changes: map[string]string{"z.yaml": "", "a.yaml": "", "m.yaml": ""},
				want:    []string{"a.yaml", "m.yaml", "z.yaml"},
			},
			"大文字と小文字が混ざる場合、バイト順で返す": {
				changes: map[string]string{"a": "", "B": "", "1": ""},
				want:    []string{"1", "B", "a"},
			},
			"1件の場合、その1件を返す": {
				changes: map[string]string{"only": ""},
				want:    []string{"only"},
			},
		}

		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				assert.Equal(t, tt.want, sortedPaths(tt.changes))
			})
		}

		t.Run("空の場合、長さ0のスライスを返す", func(t *testing.T) {
			t.Parallel()

			got := sortedPaths(map[string]string{})

			assert.NotNil(t, got, "呼び出し側は range で回すだけだが、nil と空を区別できる形を保つ")
			assert.Empty(t, got)
		})

		t.Run("nil の場合、長さ0のスライスを返す", func(t *testing.T) {
			t.Parallel()

			assert.Empty(t, sortedPaths(nil))
		})
	})
}

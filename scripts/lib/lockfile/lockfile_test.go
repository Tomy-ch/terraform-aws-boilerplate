package lockfile_test

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/lockfile"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const (
	shaA = "1111111111111111111111111111111111111111"
	shaB = "2222222222222222222222222222222222222222"
)

func testFormat() lockfile.Format {
	return lockfile.Format{
		Line:    regexp.MustCompile(`^"([^"]+)"\s*=\s*"([0-9a-f]{40})"`),
		Header:  []string{"見出し1。", "見出し2。"},
		Resolve: "sample-resolve",
		Perm:    0o644,
	}
}

// writeAt は body を lockfile として一時ディレクトリへ書き、そのパスを返す。
func writeAt(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pin.toml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	return path
}

func TestFormat_Read(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("コメント行と空行を読み飛ばして key→value を読み込む", func(t *testing.T) {
			t.Parallel()
			body := "# comment\n\n\"a@v1\" = \"" + shaA + "\"\n\"b@v2\" = \"" + shaB + "\"\n"

			lock, err := testFormat().Read(writeAt(t, body))

			require.NoError(t, err)
			assert.Equal(t, map[string]string{"a@v1": shaA, "b@v2": shaB}, lock)
		})

		t.Run("中身が無ければ空の対応表を返す", func(t *testing.T) {
			t.Parallel()

			lock, err := testFormat().Read(writeAt(t, "# comment のみ\n"))

			require.NoError(t, err)
			assert.Empty(t, lock)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("ファイルが存在しなければエラーを返す", func(t *testing.T) {
			t.Parallel()

			_, err := testFormat().Read(filepath.Join(t.TempDir(), "absent.toml"))

			require.ErrorIs(t, err, os.ErrNotExist)
		})

		// 読み飛ばすと、そのエントリは検査されないまま緑で通る（ADR-0702 決定14）。
		t.Run("代入として解釈できない行があれば行番号付きでエラーを返す", func(t *testing.T) {
			t.Parallel()
			body := "\"a@v1\" = \"" + shaA + "\"\ninvalid line\n"

			_, err := testFormat().Read(writeAt(t, body))

			require.ErrorIs(t, err, lockfile.ErrInvalidLine)
			assert.ErrorContains(t, err, "2 行目")
			assert.ErrorContains(t, err, "sample-resolve", "直し方の案内が Resolve から来ていない")
		})

		t.Run("先頭行がいきなり不正でも行番号を 1 と報告する", func(t *testing.T) {
			t.Parallel()

			_, err := testFormat().Read(writeAt(t, "invalid line\n"))

			require.ErrorIs(t, err, lockfile.ErrInvalidLine)
			assert.ErrorContains(t, err, "1 行目")
		})

		// 後勝ちで上書きすると、どちらが効いたかが実行ごとに決まる。
		t.Run("キーが重複していれば後勝ちにせずエラーを返す", func(t *testing.T) {
			t.Parallel()
			body := "\"a@v1\" = \"" + shaA + "\"\n\"a@v1\" = \"" + shaB + "\"\n"

			_, err := testFormat().Read(writeAt(t, body))

			require.ErrorIs(t, err, lockfile.ErrDuplicateKey)
			assert.ErrorContains(t, err, "2 行目")
			assert.ErrorContains(t, err, "a@v1", "どのキーが重複したかが分からない")
		})

		t.Run("値の形が合わない行はエラーにする", func(t *testing.T) {
			t.Parallel()

			_, err := testFormat().Read(writeAt(t, "\"a@v1\" = \"not-a-sha\"\n"))

			require.ErrorIs(t, err, lockfile.ErrInvalidLine)
		})

		// 部分一致を許すと、行末に付いたゴミが黙って捨てられる。
		t.Run("行末に解釈できない残りがあればエラーにする", func(t *testing.T) {
			t.Parallel()

			_, err := testFormat().Read(writeAt(t, "\"a@v1\" = \""+shaA+"\" ゴミ\n"))

			require.ErrorIs(t, err, lockfile.ErrInvalidLine)
		})

		// Line を書き落とした Format をそのまま使うと nil の regexp を呼んで panic する。
		t.Run("Line が未設定なら読まずにエラーを返す", func(t *testing.T) {
			t.Parallel()

			got, err := lockfile.Format{}.Read(writeAt(t, "\"a@v1\" = \""+shaA+"\"\n"))

			require.ErrorIs(t, err, lockfile.ErrNoLinePattern)
			assert.Nil(t, got)
		})
	})
}

func TestFormat_Write(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("見出しを付けてキーの昇順で書き出す", func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "pin.toml")

			require.NoError(t, testFormat().Write(path, map[string]string{"b@v2": shaB, "a@v1": shaA}))

			got, err := os.ReadFile(path) //nolint:gosec // t.TempDir() 配下
			require.NoError(t, err)
			assert.Equal(t,
				"# 見出し1。\n# 見出し2。\n\"a@v1\" = \""+shaA+"\"\n\"b@v2\" = \""+shaB+"\"\n",
				string(got))
		})

		t.Run("空の対応表でも見出し付きのファイルを書き、読み戻すと空になる", func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "pin.toml")

			require.NoError(t, testFormat().Write(path, map[string]string{}))

			body, err := os.ReadFile(path) //nolint:gosec // t.TempDir() 配下
			require.NoError(t, err)
			assert.Equal(t, "# 見出し1。\n# 見出し2。\n", string(body))

			got, readErr := testFormat().Read(path)
			require.NoError(t, readErr)
			assert.Empty(t, got)
		})

		t.Run("書いたものを読み直せる", func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "pin.toml")
			want := map[string]string{"a@v1": shaA}

			require.NoError(t, testFormat().Write(path, want))
			got, err := testFormat().Read(path)

			require.NoError(t, err)
			assert.Equal(t, want, got)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("書き込めなければエラーを返す", func(t *testing.T) {
			t.Parallel()

			err := testFormat().Write(filepath.Join(t.TempDir(), "missing", "pin.toml"), nil)

			require.ErrorIs(t, err, os.ErrNotExist)
		})
	})
}

func TestIsIgnorableErr(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("nil は無視してよい", func(t *testing.T) {
			t.Parallel()
			assert.True(t, lockfile.IsIgnorableErr(nil))
		})

		t.Run("ファイル不在は初回 resolve なので無視してよい", func(t *testing.T) {
			t.Parallel()
			assert.True(t, lockfile.IsIgnorableErr(xerrors.Wrap(os.ErrNotExist, "read")))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		// 不在以外を「空」へ倒すと、既存のピンが黙って消えたまま続行する。
		t.Run("不在以外は無視してはならない", func(t *testing.T) {
			t.Parallel()
			assert.False(t, lockfile.IsIgnorableErr(lockfile.ErrInvalidLine))
			assert.False(t, lockfile.IsIgnorableErr(os.ErrPermission))
		})
	})
}

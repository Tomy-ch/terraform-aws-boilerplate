package atomicwrite_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/atomicwrite"
)

const filePerm = 0o644

func TestApply(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("すべてのファイルへ反映する", func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			a := filepath.Join(dir, "a.txt")
			b := filepath.Join(dir, "b.txt")
			require.NoError(t, os.WriteFile(a, []byte("old-a"), filePerm))
			require.NoError(t, os.WriteFile(b, []byte("old-b"), filePerm))

			require.NoError(t, atomicwrite.Apply(map[string]string{a: "new-a", b: "new-b"}, filePerm))

			assert.Equal(t, "new-a", read(t, a))
			assert.Equal(t, "new-b", read(t, b))
		})

		t.Run("対象が空なら何もせず成功する", func(t *testing.T) {
			t.Parallel()

			require.NoError(t, atomicwrite.Apply(map[string]string{}, filePerm))
		})

		t.Run("一時ファイルを残さない", func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			a := filepath.Join(dir, "a.txt")
			require.NoError(t, os.WriteFile(a, []byte("old"), filePerm))

			require.NoError(t, atomicwrite.Apply(map[string]string{a: "new"}, filePerm))
			assertNoTemp(t, dir)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		// このパッケージの存在理由に直結するケース。書き込みが途中で失敗したとき、
		// 先に処理したファイルが新しい内容のまま残ると、呼び出し側からは
		// 「失敗した」と「一部だけ適用された」が区別できなくなる。
		t.Run("途中で書けなければ、先のファイルも書き換えない", func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			ok := filepath.Join(dir, "a.txt")
			ng := filepath.Join(dir, "missing", "b.txt")
			require.NoError(t, os.WriteFile(ok, []byte("old-a"), filePerm))

			require.Error(t, atomicwrite.Apply(map[string]string{ok: "new-a", ng: "new-b"}, filePerm))

			assert.Equal(t, "old-a", read(t, ok), "先に処理したファイルが書き換わっている")
			assert.NoFileExists(t, ng)
		})

		t.Run("失敗しても一時ファイルを残さない", func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			ok := filepath.Join(dir, "a.txt")
			ng := filepath.Join(dir, "missing", "b.txt")
			require.NoError(t, os.WriteFile(ok, []byte("old-a"), filePerm))

			require.Error(t, atomicwrite.Apply(map[string]string{ok: "new-a", ng: "new-b"}, filePerm))
			assertNoTemp(t, dir)
		})

		// **これは「望ましい挙動」ではなく、既知の限界の固定である。**
		// rename 自体が途中で失敗する窓は消せない（パッケージの doc コメント）。
		// 緩和が入ったらこのケースが赤くなる。
		t.Run("rename が途中で失敗すると、先に rename した分は新しい内容のまま残る", func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			a := filepath.Join(dir, "a.txt")
			b := filepath.Join(dir, "b.txt")
			require.NoError(t, os.WriteFile(a, []byte("old-a"), filePerm))
			// b を既存のディレクトリにすると、そこへの rename が失敗する。
			require.NoError(t, os.Mkdir(b, 0o750))

			require.Error(t, atomicwrite.Apply(map[string]string{a: "new-a", b: "new-b"}, filePerm))

			assert.Equal(t, "new-a", read(t, a),
				"rename の窓が塞がれたなら、このケースを書き換えること")
		})
	})
}

// read は、テスト中にファイルの中身を文字列で読みます。
func read(t *testing.T, path string) string {
	t.Helper()

	body, err := os.ReadFile(path) //nolint:gosec // G304: テストが自分で作った t.TempDir() 配下のみ
	require.NoError(t, err)

	return string(body)
}

// assertNoTemp は、ディレクトリに一時ファイルが残っていないことを確かめます。
func assertNoTemp(t *testing.T, dir string) {
	t.Helper()

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	for _, e := range entries {
		assert.NotContains(t, e.Name(), ".atomicwrite.tmp")
	}
}

func TestApply_モードの引き継ぎ(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		// os.WriteFile は既存ファイルのモードを変えない。一時ファイル経由で置き換える
		// ここでも、引き継がないと書き換えのたびにモードが既定値へ戻る。
		t.Run("既存ファイルのモードを引き継ぐ", func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			a := filepath.Join(dir, "a.txt")
			require.NoError(t, os.WriteFile(a, []byte("old"), 0o600))

			require.NoError(t, atomicwrite.Apply(map[string]string{a: "new"}, filePerm))

			info, err := os.Stat(a)
			require.NoError(t, err)
			assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
		})

		t.Run("新規ファイルは渡されたモードで作る", func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			a := filepath.Join(dir, "new.txt")

			require.NoError(t, atomicwrite.Apply(map[string]string{a: "body"}, 0o600))

			info, err := os.Stat(a)
			require.NoError(t, err)
			assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
		})
	})
}

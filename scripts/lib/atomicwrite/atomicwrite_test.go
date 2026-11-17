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
			assertOnly(t, dir, "a.txt")
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		// このパッケージの存在理由に直結するケース（package doc の「一部だけ適用された」）。
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

		t.Run("前回の残骸が在れば、上書きせずに落ちる", func(t *testing.T) {
			t.Parallel()
			// 強制終了で残った一時ファイルは、人が正体を見て消すまで次を通さない。
			// symlink のケースと合わせて「既に在れば落ちる」の両側を固定する。
			dir := t.TempDir()
			target := filepath.Join(dir, "a.txt")
			require.NoError(t, os.WriteFile(target, []byte("old"), 0o600))
			require.NoError(t, os.WriteFile(target+".atomicwrite.tmp", []byte("stale"), 0o600))

			require.Error(t, atomicwrite.Apply(map[string]string{target: "new"}, 0o644))
			assert.Equal(t, "old", read(t, target))
			assert.Equal(t, "stale", read(t, target+".atomicwrite.tmp"), "残骸を消している")
		})

		t.Run("一時ファイルの名前に何かが在れば、辿らず書かずに落ちる", func(t *testing.T) {
			t.Parallel()
			// **接尾辞をここだけ写す。** 攻撃の形を作るには名前が要る。写しであることは
			// 承知のうえで、定数が変わればこのケースは symlink を置く場所を外し、
			// 「落ちる」を主張しなくなる（緑になる）。
			dir := t.TempDir()
			target := filepath.Join(dir, "a.txt")
			require.NoError(t, os.WriteFile(target, []byte("old"), 0o600))

			outside := filepath.Join(t.TempDir(), "victim")
			require.NoError(t, os.WriteFile(outside, []byte("untouched"), 0o600))
			require.NoError(t, os.Symlink(outside, target+".atomicwrite.tmp"))

			require.Error(t, atomicwrite.Apply(map[string]string{target: "new"}, 0o644))
			assert.Equal(t, "untouched", read(t, outside), "ツリーの外へ書いている")
			assert.Equal(t, "old", read(t, target))
		})

		// 書き込みはパスの昇順なので、失敗する missing/b.txt より前に a.txt を置くことで
		// 「一時ファイルを作ってから消した」経路を踏む。Apply は最初の失敗で打ち切るため、
		// 後ろに対象を足しても書き込みが試みられない。
		t.Run("失敗しても一時ファイルを残さない", func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			first := filepath.Join(dir, "a.txt")
			ng := filepath.Join(dir, "missing", "b.txt")
			require.NoError(t, os.WriteFile(first, []byte("old-a"), filePerm))

			require.Error(t, atomicwrite.Apply(map[string]string{first: "new-a", ng: "new-b"}, filePerm))

			assertOnly(t, dir, "a.txt")
			assert.Equal(t, "old-a", read(t, first), "先に処理したファイルが書き換わっている")
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

func read(t *testing.T, path string) string {
	t.Helper()

	body, err := os.ReadFile(path) //nolint:gosec // G304: テストが自分で作った t.TempDir() 配下のみ
	require.NoError(t, err)

	return string(body)
}

// assertOnly は、ディレクトリの中身が want と完全に一致することを確かめます。
//
// 一時ファイルの名前で照合しない。接尾辞は atomicwrite の非公開定数で、テストから参照できない
// ため写しになる。写した側は、定数が変われば「残っていないこと」を検査せずに通る。
func assertOnly(t *testing.T, dir string, want ...string) {
	t.Helper()

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	got := make([]string, 0, len(entries))
	for _, e := range entries {
		got = append(got, e.Name())
	}

	assert.ElementsMatch(t, want, got, "想定外のファイルが残っている")
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

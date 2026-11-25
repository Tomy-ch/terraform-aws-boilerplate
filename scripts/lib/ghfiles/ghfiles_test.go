package ghfiles_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/ghfiles"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/testenv"
)

func writeFile(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte("jobs:\n"), 0o600))
}

func Test_Collect(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("workflow 定義を拡張子の違いごと集める", func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeFile(t, filepath.Join(root, ".github", "workflows", "ci.yaml"))
			writeFile(t, filepath.Join(root, ".github", "workflows", "lint.yml"))

			got, err := ghfiles.Collect(root)
			require.NoError(t, err)
			assert.Equal(t, []string{
				filepath.Join(root, ".github", "workflows", "ci.yaml"),
				filepath.Join(root, ".github", "workflows", "lint.yml"),
			}, got)
		})

		t.Run("入れ子に置かれた composite action 定義まで再帰的に集める", func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeFile(t, filepath.Join(root, ".github", "actions", "setup", "action.yml"))
			writeFile(t, filepath.Join(root, ".github", "actions", "g", "nested", "action.yaml"))

			got, err := ghfiles.Collect(root)
			require.NoError(t, err)
			assert.Equal(t, []string{
				filepath.Join(root, ".github", "actions", "g", "nested", "action.yaml"),
				filepath.Join(root, ".github", "actions", "setup", "action.yml"),
			}, got)
		})

		t.Run("workflow と composite action を混ぜてもパス順に整列する", func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeFile(t, filepath.Join(root, ".github", "workflows", "ci.yaml"))
			writeFile(t, filepath.Join(root, ".github", "actions", "setup", "action.yml"))

			got, err := ghfiles.Collect(root)
			require.NoError(t, err)
			assert.Equal(t, []string{
				filepath.Join(root, ".github", "actions", "setup", "action.yml"),
				filepath.Join(root, ".github", "workflows", "ci.yaml"),
			}, got)
		})

		t.Run(".github が無いリポジトリでは空を返す", func(t *testing.T) {
			t.Parallel()

			got, err := ghfiles.Collect(t.TempDir())
			require.NoError(t, err)
			assert.Empty(t, got)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("workflow ディレクトリの YAML 以外は集めない", func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeFile(t, filepath.Join(root, ".github", "workflows", "README.md"))

			got, err := ghfiles.Collect(root)
			require.NoError(t, err)
			assert.Empty(t, got)
		})

		t.Run("composite action ディレクトリの定義ファイル以外は集めない", func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeFile(t, filepath.Join(root, ".github", "actions", "setup", "README.md"))
			writeFile(t, filepath.Join(root, ".github", "actions", "setup", "entrypoint.sh"))

			got, err := ghfiles.Collect(root)
			require.NoError(t, err)
			assert.Empty(t, got)
		})

		t.Run("不在以外の走査失敗は握り潰さず返す", func(t *testing.T) {
			t.Parallel()
			testenv.RequireNonRoot(t, "root では読み取り権限の剥奪が効かない")
			root := t.TempDir()
			actions := filepath.Join(root, ".github", "actions")
			writeFile(t, filepath.Join(actions, "setup", "action.yml"))
			require.NoError(t, os.Chmod(actions, 0o000))
			// TempDir の後片付けが子を unlink できるよう書き込みまで戻す。
			t.Cleanup(func() { _ = os.Chmod(actions, 0o700) }) //nolint:gosec // 削除に書き込み権限が要る

			_, err := ghfiles.Collect(root)
			assert.ErrorIs(t, err, os.ErrPermission)
		})

		t.Run("workflows の外に置かれた YAML は集めない", func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeFile(t, filepath.Join(root, ".github", "ci.yaml"))
			writeFile(t, filepath.Join(root, ".github", "workflows", "nested", "ci.yaml"))

			got, err := ghfiles.Collect(root)
			require.NoError(t, err)
			assert.Empty(t, got)
		})
	})
}

// matchesPattern は Patterns の返す glob に rel が一致するかを返す。`/**/` は任意の深さの
// ディレクトリ（0 階層を含まない）として扱う。
func matchesPattern(t *testing.T, pattern, rel string) bool {
	t.Helper()
	dir, base, deep := strings.Cut(pattern, "/**/")
	if !deep {
		ok, err := filepath.Match(pattern, rel)
		require.NoError(t, err)
		return ok
	}
	if !strings.HasPrefix(rel, dir+"/") || strings.Count(rel[len(dir)+1:], "/") == 0 {
		return false
	}
	ok, err := filepath.Match(base, filepath.Base(rel))
	require.NoError(t, err)
	return ok
}

func Test_Patterns(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("workflow と入れ子の composite action の定義を拡張子ごとに返す", func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, []string{
				".github/workflows/*.yaml",
				".github/workflows/*.yml",
				".github/actions/**/action.yml",
				".github/actions/**/action.yaml",
			}, ghfiles.Patterns())
		})

		t.Run("Collect が集める各パスはいずれかのパターンに一致する", func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			writeFile(t, filepath.Join(root, ".github", "workflows", "ci.yaml"))
			writeFile(t, filepath.Join(root, ".github", "workflows", "lint.yml"))
			writeFile(t, filepath.Join(root, ".github", "actions", "setup", "action.yml"))
			writeFile(t, filepath.Join(root, ".github", "actions", "g", "nested", "action.yaml"))

			got, err := ghfiles.Collect(root)
			require.NoError(t, err)
			require.Len(t, got, 4)
			for _, p := range got {
				rel, err := filepath.Rel(root, p)
				require.NoError(t, err)
				rel = filepath.ToSlash(rel)
				assert.Truef(t, slices.ContainsFunc(ghfiles.Patterns(), func(pat string) bool {
					return matchesPattern(t, pat, rel)
				}), "%s がどのパターンにも一致しない", rel)
			}
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("返した一覧を書き換えても次の呼び出しへ漏れない", func(t *testing.T) {
			t.Parallel()

			first := ghfiles.Patterns()
			first[0] = "changed"

			assert.Equal(t, ".github/workflows/*.yaml", ghfiles.Patterns()[0])
		})
	})
}

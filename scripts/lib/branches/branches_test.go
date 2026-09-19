package branches

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sound は、解釈できる最小の宣言。
const sound = `
[default]
branch = "production"

[sets]
deploy = ["develop", "staging", "production"]
protected = ["develop", "release/**/*"]

[release]
prefix = "release/"
pattern = '^release/v(\d+)\.(\d+)\.(\d+)$'

[workflows]
"zizmor.yaml" = "deploy"
`

func Test_Parse(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("集合と単一値と workflow の対応を読み分ける", func(t *testing.T) {
			t.Parallel()
			decl, err := Parse(sound, "t.toml")
			require.NoError(t, err)

			set, err := decl.Set("deploy")
			require.NoError(t, err)
			assert.Equal(t, []string{"develop", "staging", "production"}, set)

			branch, err := decl.DefaultBranch()
			require.NoError(t, err)
			assert.Equal(t, "production", branch)

			assert.Equal(t, map[string]string{"zizmor.yaml": "deploy"}, decl.Workflows())
		})

		// 捕捉群の順序が変わると base-branch が別の版を「最新」と判定する。
		t.Run("release.pattern は major / minor / patch の順で捕捉する", func(t *testing.T) {
			t.Parallel()
			decl, err := Parse(sound, "t.toml")
			require.NoError(t, err)

			re, err := decl.ReleasePattern()
			require.NoError(t, err)
			assert.Equal(t, []string{"release/v2.10.3", "2", "10", "3"}, re.FindStringSubmatch("release/v2.10.3"))
		})

		t.Run("Workflows は内部の map を共有しない", func(t *testing.T) {
			t.Parallel()
			decl, err := Parse(sound, "t.toml")
			require.NoError(t, err)

			decl.Workflows()["zizmor.yaml"] = "書き換え"
			assert.Equal(t, "deploy", decl.Workflows()["zizmor.yaml"])
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		// 読み飛ばすと、打ち間違えた1行が「宣言されていない」と同じ扱いになり、
		// 保護対象から黙って消える。
		t.Run("解釈できない行はエラーにする", func(t *testing.T) {
			t.Parallel()
			_, err := Parse(sound+"\nbranch = production\n", "t.toml")
			require.ErrorIs(t, err, ErrSyntax)
			assert.Contains(t, err.Error(), "t.toml:")
		})

		t.Run("集合が1件も無ければエラーにする", func(t *testing.T) {
			t.Parallel()
			_, err := Parse("[default]\nbranch = \"production\"\n", "t.toml")
			require.ErrorIs(t, err, ErrEmpty)
		})

		t.Run("単一値が1件も無ければエラーにする", func(t *testing.T) {
			t.Parallel()
			_, err := Parse("[sets]\ndeploy = [\"develop\"]\n", "t.toml")
			require.ErrorIs(t, err, ErrEmpty)
		})

		t.Run("空の宣言はエラーにする", func(t *testing.T) {
			t.Parallel()
			_, err := Parse("", "t.toml")
			require.ErrorIs(t, err, ErrEmpty)
		})

		// 空を返すと、打ち間違えた名前が「要素0件の集合」に化け、対象を1件も
		// 持たないまま通る。
		t.Run("存在しない集合はエラーにする", func(t *testing.T) {
			t.Parallel()
			decl, err := Parse(sound, "t.toml")
			require.NoError(t, err)

			_, err = decl.Set("no-such-set")
			require.ErrorIs(t, err, ErrUnknownSet)
		})

		t.Run("存在しないキーはエラーにする", func(t *testing.T) {
			t.Parallel()
			decl, err := Parse(sound, "t.toml")
			require.NoError(t, err)

			_, err = decl.Scalar("no.such.key")
			require.ErrorIs(t, err, ErrUnknownKey)
		})

		t.Run("正規表現として不正な release.pattern はエラーにする", func(t *testing.T) {
			t.Parallel()
			decl, err := Parse("[sets]\nx = [\"a\"]\n[release]\npattern = \"([\"\n", "t.toml")
			require.NoError(t, err)

			_, err = decl.ReleasePattern()
			require.ErrorIs(t, err, ErrSyntax)
		})
	})
}

func Test_Load(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("ファイルを読んで解釈する", func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := filepath.Join(dir, "branches.toml")
			require.NoError(t, writeFile(path, sound))

			decl, err := Load(path)
			require.NoError(t, err)

			branch, err := decl.DefaultBranch()
			require.NoError(t, err)
			assert.Equal(t, "production", branch)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		// 不在を空の宣言として続行すると、保護対象が0件のまま apply が通る。
		t.Run("ファイルが無ければエラーにする", func(t *testing.T) {
			t.Parallel()
			_, err := Load(filepath.Join(t.TempDir(), "no-such.toml"))
			require.Error(t, err)
		})
	})
}

// writeFile はテスト用の書き出し。
func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o600)
}

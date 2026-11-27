package checksdecl

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateGlob(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		for _, p := range []string{"**", "**/*.go", "scripts/go.mod", ".github/workflows/**", "env/*/identity/**"} {
			t.Run(p+" を受け付ける", func(t *testing.T) {
				t.Parallel()
				require.NoError(t, ValidateGlob(p))
			})
		}
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for _, p := range []string{"", "/a", "a/", "!a", "a/{b,c}", `a\b`, "a//b", "a**/b", "a/[b"} {
			t.Run(p+" を誤りにする", func(t *testing.T) {
				t.Parallel()
				require.ErrorIs(t, ValidateGlob(p), ErrGlob)
			})
		}
	})
}

func TestMatchGlob(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		for name, tc := range map[string]struct {
			pattern, file string
			want          bool
		}{
			"先頭の ** は直下のファイルにも当たる":        {"**/*.go", "main.go", true},
			"先頭の ** は深いファイルにも当たる":         {"**/*.go", "scripts/a/b.go", true},
			"拡張子の後ろに続きがあれば当たらない":          {"**/*.go", "scripts/a/b.go.txt", false},
			"末尾の ** はディレクトリそのものにも当たる":     {"scripts/**", "scripts", true},
			"末尾の ** は配下に当たる":              {"scripts/**", "scripts/x", true},
			"ドットで始まる segment にも当たる":       {".github/workflows/**", ".github/workflows/a.yaml", true},
			"途中の * は1つの segment に当たる":     {"env/*/identity/**", "env/admin/identity/main.tf", true},
			"途中の * の後の segment が違えば当たらない": {"env/*/identity/**", "env/admin/main.tf", false},
			"固定の path はそれ自身に当たる":          {"mise.toml", "mise.toml", true},
			"固定の path は別のディレクトリの同名に当たらない": {"mise.toml", "a/mise.toml", false},
			"固定の path はその下の path に当たらない":  {"mise.toml", "mise.toml/x", false},
			"** だけはすべてに当たる":               {"**", "a/b/c", true},
			"* は / をまたがない":                {"*.go", "a/b.go", false},
			"途中の * は2つの segment に当たらない":   {"env/*/main.tf", "env/a/b/main.tf", false},
			"末尾の * は配下の深いファイルに当たらない":      {"scripts/*", "scripts/a/b", false},
			"末尾の ** は接頭辞が同じだけの名前に当たらない":   {"scripts/**", "scriptsx/a", false},
			"固定の path は接頭辞が同じだけの名前に当たらない": {"scripts", "scripts-old", false},
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				assert.Equal(t, tc.want, MatchGlob(tc.pattern, tc.file))
			})
		}
	})
}

func Test_matchSegs(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("** は0個の segment にも当たる", func(t *testing.T) {
			t.Parallel()
			assert.True(t, matchSegs([]string{"a", "**", "b"}, []string{"a", "b"}))
			assert.True(t, matchSegs([]string{"a", "**", "b"}, []string{"a", "x", "y", "b"}))
			assert.False(t, matchSegs([]string{"a", "**", "b"}, []string{"a", "x"}))
		})

		t.Run("segment の数が違えば当たらない", func(t *testing.T) {
			t.Parallel()
			assert.False(t, matchSegs([]string{"a"}, []string{"a", "b"}))
			assert.False(t, matchSegs([]string{"a", "b"}, []string{"a"}))
		})
	})
}

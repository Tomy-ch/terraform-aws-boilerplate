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

		for _, tc := range []struct {
			pattern, file string
			want          bool
		}{
			{"**/*.go", "main.go", true},
			{"**/*.go", "scripts/a/b.go", true},
			{"**/*.go", "scripts/a/b.go.txt", false},
			{"scripts/**", "scripts", true},
			{"scripts/**", "scripts/x", true},
			{".github/workflows/**", ".github/workflows/a.yaml", true},
			{"env/*/identity/**", "env/admin/identity/main.tf", true},
			{"env/*/identity/**", "env/admin/main.tf", false},
			{"mise.toml", "mise.toml", true},
			{"mise.toml", "a/mise.toml", false},
			{"**", "a/b/c", true},
		} {
			t.Run(tc.pattern+" と "+tc.file, func(t *testing.T) {
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

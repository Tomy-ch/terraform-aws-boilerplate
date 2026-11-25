package gitrev

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQuery(t *testing.T) {
	t.Parallel()

	t.Run("rev を commit へ剥く1行を返す", func(t *testing.T) {
		t.Parallel()
		got, err := Query("origin/release/v0.2.0")
		require.NoError(t, err)
		assert.Equal(t, "origin/release/v0.2.0^{commit}\n", got)
	})

	t.Run("空白を含む rev は Query では通し、判定を Parse に任せる", func(t *testing.T) {
		t.Parallel()
		got, err := Query("HEAD HEAD")
		require.NoError(t, err)
		assert.Equal(t, "HEAD HEAD^{commit}\n", got)
	})

	t.Run("空は ErrEmpty", func(t *testing.T) {
		t.Parallel()
		_, err := Query("")
		require.ErrorIs(t, err, ErrEmpty)
	})

	for name, rev := range map[string]string{
		"改行を含む":    "HEAD\nHEAD",
		"CR を含む":   "HEAD\r",
		"NUL を含む":  "HEAD\x00",
		"`-` で始まる": "--all",
	} {
		t.Run(name+"は ErrUnresolved", func(t *testing.T) {
			t.Parallel()
			_, err := Query(rev)
			require.ErrorIs(t, err, ErrUnresolved)
		})
	}
}

func TestParse(t *testing.T) {
	t.Parallel()

	sha := strings.Repeat("a", 40)
	t.Run("64桁の sha も返す", func(t *testing.T) {
		t.Parallel()
		long := strings.Repeat("b", 64)
		got, err := Parse(long + " commit\n")
		require.NoError(t, err)
		assert.Equal(t, long, got)
	})

	t.Run("1件の commit なら sha を返す", func(t *testing.T) {
		t.Parallel()
		got, err := Parse(sha + " commit\n")
		require.NoError(t, err)
		assert.Equal(t, sha, got)
	})

	for name, out := range map[string]string{
		"空":               "",
		"missing":         "HEAD^{commit} missing\n",
		"tree":            sha + " tree\n",
		"blob":            sha + " blob\n",
		"2件":              sha + " commit\n" + sha + " commit\n",
		"sha が短い":         sha[:39] + " commit\n",
		"sha が大文字":        strings.ToUpper(sha) + " commit\n",
		"余分な列":            sha + " commit 1\n",
		"ambiguous":       "abcd ambiguous\n",
		"rev が空白を含み3列になる": "HEAD HEAD^{commit} missing\n",
		"sha が16進でない":     strings.Repeat("g", 40) + " commit\n",
	} {
		t.Run(name+"は ErrUnresolved", func(t *testing.T) {
			t.Parallel()
			_, err := Parse(out)
			require.ErrorIs(t, err, ErrUnresolved)
		})
	}
}

func TestIsHash(t *testing.T) {
	t.Parallel()

	for s, want := range map[string]bool{
		strings.Repeat("0", 40): true,
		strings.Repeat("f", 64): true,
		strings.Repeat("0", 39): false,
		strings.Repeat("0", 41): false,
		strings.Repeat("g", 40): false,
		strings.Repeat("F", 40): false,
		"":                      false,
	} {
		t.Run(fmt.Sprintf("%q", s), func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, want, IsHash(s))
		})
	}
}

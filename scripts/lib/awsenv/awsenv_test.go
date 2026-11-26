package awsenv

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFound(t *testing.T) {
	t.Parallel()

	for _, k := range StaticCredentials {
		t.Run(k+" が在れば見つける", func(t *testing.T) {
			t.Parallel()

			got := Found(func(name string) string {
				if name == k {
					return "x"
				}

				return ""
			})
			assert.Equal(t, []string{k}, got)
		})
	}

	t.Run("AWS_PROFILE だけなら何も見つけない", func(t *testing.T) {
		t.Parallel()

		assert.Empty(t, Found(func(name string) string {
			if name == "AWS_PROFILE" {
				return "p"
			}

			return ""
		}))
	})

	t.Run("一覧の順で返す", func(t *testing.T) {
		t.Parallel()

		got := Found(func(name string) string {
			if name == "AWS_CONFIG_FILE" || name == "AWS_ACCESS_KEY_ID" {
				return "x"
			}

			return ""
		})
		assert.Equal(t, []string{"AWS_ACCESS_KEY_ID", "AWS_CONFIG_FILE"}, got)
	})
}

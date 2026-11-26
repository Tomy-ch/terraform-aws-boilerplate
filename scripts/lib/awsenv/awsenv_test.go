package awsenv

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFound(t *testing.T) {
	t.Parallel()

	// 一覧から名前が消えたことを落とすため、期待の一覧は StaticCredentials から作らない。
	want := []string{
		"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN", "AWS_ROLE_ARN", "AWS_WEB_IDENTITY_TOKEN_FILE",
		"AWS_CONTAINER_CREDENTIALS_FULL_URI", "AWS_CONTAINER_CREDENTIALS_RELATIVE_URI", "AWS_SHARED_CREDENTIALS_FILE", "AWS_CONFIG_FILE",
	}

	for _, k := range want {
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

	t.Run("すべて在れば、一覧の順ですべてを返す", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, want, Found(func(string) string { return "x" }))
	})

	t.Run("AWS_PROFILE だけなら何も見つけない", func(t *testing.T) {
		t.Parallel()

		assert.Empty(t, Found(func(name string) string {
			if name == "AWS_PROFILE" {
				return "p"
			}

			return ""
		}))
	})

	t.Run("空の値は在るとみなさない", func(t *testing.T) {
		t.Parallel()

		assert.Empty(t, Found(func(string) string { return "" }))
	})
}

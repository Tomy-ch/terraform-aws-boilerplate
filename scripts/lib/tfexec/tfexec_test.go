package tfexec

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDataDir(t *testing.T) {
	t.Parallel()

	t.Run("_ を先にエスケープし、/ を __ にする", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, filepath.Join("/d", "modules__a_u__b"), DataDir("/d", "modules/a_/b"))
	})

	t.Run("_ と / の並びが違えば別の名前になる", func(t *testing.T) {
		t.Parallel()
		assert.NotEqual(t, DataDir("/d", "modules/a_/b"), DataDir("/d", "modules/a/_b"))
	})
}

func TestEnv(t *testing.T) {
	t.Parallel()

	t.Run("AWS_ で始まる変数を外し、資格情報を探す先を塞ぎ、extra を足す", func(t *testing.T) {
		t.Parallel()
		got := Env([]string{"PATH=/bin", "AWS_PROFILE=p", "AWS_ACCESS_KEY_ID=x", "XAWS_KEEP=1"}, "TF_DATA_DIR=/d")
		assert.Equal(t, []string{"PATH=/bin", "XAWS_KEEP=1",
			"AWS_SHARED_CREDENTIALS_FILE=/dev/null", "AWS_CONFIG_FILE=/dev/null", "AWS_EC2_METADATA_DISABLED=true", "CHECKPOINT_DISABLE=1",
			"TF_DATA_DIR=/d"}, got)
	})
}

package requiredchecks_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/requiredchecks"
)

func TestRead(t *testing.T) {
	t.Parallel()

	write := func(t *testing.T, body string) string {
		t.Helper()

		p := filepath.Join(t.TempDir(), "branch-protection.json")
		require.NoError(t, os.WriteFile(p, []byte(body), 0o600))

		return p
	}

	t.Run("required_status_checks の context を宣言の順に返す", func(t *testing.T) {
		t.Parallel()

		got, err := requiredchecks.Read(write(t, `{"rules": [{"type": "deletion"}, {"type": "required_status_checks", "parameters": {"required_status_checks": [{"context": "tf-lint"}, {"context": "go-test"}]}}]}`))
		require.NoError(t, err)
		assert.Equal(t, []string{"tf-lint", "go-test"}, got)
	})

	for name, c := range map[string]struct {
		body string
		want error
	}{
		"required の規則が無ければ0件のエラー": {`{"rules": [{"type": "deletion"}]}`, requiredchecks.ErrNone},
		"規則の一覧が空なら0件のエラー":         {`{"rules": [{"type": "required_status_checks", "parameters": {"required_status_checks": []}}]}`, requiredchecks.ErrNone},
		"JSON でなければ読めない":          {"nope", requiredchecks.ErrRead},
		"rules の形が違えば読めない":        {`{"rules": {}}`, requiredchecks.ErrRead},
		"context が空なら読めない":        {`{"rules": [{"type": "required_status_checks", "parameters": {"required_status_checks": [{"context": ""}]}}]}`, requiredchecks.ErrRead},
		"context を欠けば読めない":        {`{"rules": [{"type": "required_status_checks", "parameters": {"required_status_checks": [{"integration_id": 1}]}}]}`, requiredchecks.ErrRead},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := requiredchecks.Read(write(t, c.body))
			require.ErrorIs(t, err, c.want)
		})
	}

	t.Run("ファイルが無ければ読めない", func(t *testing.T) {
		t.Parallel()

		_, err := requiredchecks.Read(filepath.Join(t.TempDir(), "missing.json"))
		require.ErrorIs(t, err, requiredchecks.ErrRead)
	})
}

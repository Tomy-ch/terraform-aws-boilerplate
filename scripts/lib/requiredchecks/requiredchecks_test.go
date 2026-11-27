package requiredchecks_test

import (
	"os"
	"path/filepath"
	"strings"
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

func TestReadDir(t *testing.T) {
	t.Parallel()

	const deletion = `{"rules": [{"type": "deletion"}]}`
	required := func(contexts ...string) string {
		items := make([]string, 0, len(contexts))
		for _, c := range contexts {
			items = append(items, `{"context": "`+c+`"}`)
		}
		return `{"rules": [{"type": "required_status_checks", "parameters": {"required_status_checks": [` + strings.Join(items, ", ") + `]}}]}`
	}
	build := func(t *testing.T, files map[string]string) string {
		t.Helper()

		dir := t.TempDir()
		for name, body := range files {
			p := filepath.Join(dir, name)
			require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o750))
			require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
		}

		return dir
	}

	t.Run("ファイル名の順に、各ファイルの宣言の順で、宣言したファイルとともに返す", func(t *testing.T) {
		t.Parallel()

		dir := build(t, map[string]string{
			"lines.json":  required("gate"),
			"common.json": deletion,
			"deploy.json": required("deploy-source-check", "line-gate"),
		})
		got, err := requiredchecks.ReadDir(dir)
		require.NoError(t, err)
		assert.Equal(t, []requiredchecks.Context{
			{Name: "deploy-source-check", File: filepath.Join(dir, "deploy.json")},
			{Name: "line-gate", File: filepath.Join(dir, "deploy.json")},
			{Name: "gate", File: filepath.Join(dir, "lines.json")},
		}, got)
	})

	t.Run(".json でないファイルとサブディレクトリは読まない", func(t *testing.T) {
		t.Parallel()

		dir := build(t, map[string]string{
			"lines.json":     required("gate"),
			"README.md":      "nope",
			"sub/other.json": required("other"),
		})
		got, err := requiredchecks.ReadDir(dir)
		require.NoError(t, err)
		assert.Equal(t, []requiredchecks.Context{{Name: "gate", File: filepath.Join(dir, "lines.json")}}, got)
	})

	for name, c := range map[string]struct {
		files map[string]string
		want  error
	}{
		"ruleset が1つも無ければ0件のエラー":              {map[string]string{"README.md": "x"}, requiredchecks.ErrNone},
		"どの ruleset も required を持たなければ0件のエラー": {map[string]string{"common.json": deletion}, requiredchecks.ErrNone},
		"解釈できない ruleset が1つでもあれば読めない":         {map[string]string{"lines.json": required("gate"), "broken.json": "nope"}, requiredchecks.ErrRead},
		"context の空な ruleset が1つでもあれば読めない":    {map[string]string{"lines.json": required("gate"), "deploy.json": required("")}, requiredchecks.ErrRead},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := requiredchecks.ReadDir(build(t, c.files))
			require.ErrorIs(t, err, c.want)
		})
	}

	t.Run("空のディレクトリは0件のエラー", func(t *testing.T) {
		t.Parallel()

		_, err := requiredchecks.ReadDir(t.TempDir())
		require.ErrorIs(t, err, requiredchecks.ErrNone)
	})

	t.Run("ディレクトリが無ければ読めない", func(t *testing.T) {
		t.Parallel()

		_, err := requiredchecks.ReadDir(filepath.Join(t.TempDir(), "missing"))
		require.ErrorIs(t, err, requiredchecks.ErrRead)
	})
}

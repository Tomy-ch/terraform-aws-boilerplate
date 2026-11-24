package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	dependabotFixture = "version: 2\nupdates:\n  - package-ecosystem: \"gomod\"\n    labels:\n      - \"dependencies\"\n      - \"go\"\n  - package-ecosystem: \"docker\"\n"
	labelsFixture     = `[{"name": "dependencies", "color": "0366d6"}, {"name": "go", "color": "16e2e2"}, {"name": "bug", "color": "d73a4a"}]`
)

func Test_checkDependabotLabels(t *testing.T) {
	t.Parallel()

	t.Run("名指すラベルがすべて宣言にあれば、その数を返す", func(t *testing.T) {
		t.Parallel()
		n, err := checkDependabotLabels([]byte(dependabotFixture), []byte(labelsFixture))
		require.NoError(t, err)
		assert.Equal(t, 2, n)
	})

	t.Run("labels の無い ecosystem だけなら0件で通る", func(t *testing.T) {
		t.Parallel()
		n, err := checkDependabotLabels([]byte("updates:\n  - package-ecosystem: \"docker\"\n"), []byte(labelsFixture))
		require.NoError(t, err)
		assert.Equal(t, 0, n)
	})

	cases := []struct {
		name       string
		dependabot string
		labels     string
		message    string
	}{
		{"宣言に無いラベルは拒む", dependabotFixture + "    labels:\n      - \"docker\"\n", labelsFixture, `docker の "docker"`},
		{"ecosystem が1つも無ければ拒む", "version: 2\nupdates: []\n", labelsFixture, "ecosystem が1つもありません"},
		{"updates が無ければ拒む", "version: 2\n", labelsFixture, "ecosystem が1つもありません"},
		{"YAML として読めなければ拒む", "updates: [\n", labelsFixture, dependabotFile},
		{"labels が文字列の一覧でなければ拒む", "updates:\n  - package-ecosystem: \"gomod\"\n    labels:\n      a: b\n", labelsFixture, dependabotFile},
		{"labels.json が JSON として読めなければ拒む", dependabotFixture, `x`, labelsFile},
		{"labels.json が配列でなければ拒む", dependabotFixture, `{"name": "go"}`, labelsFile},
		{"labels.json に name の無い要素があれば拒む", dependabotFixture, `[{"name": "dependencies"}, {"color": "000000"}]`, "2 番目の要素に name がありません"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := checkDependabotLabels([]byte(tc.dependabot), []byte(tc.labels))
			require.ErrorIs(t, err, errDependabotLabels)
			assert.Contains(t, err.Error(), tc.message)
		})
	}
}

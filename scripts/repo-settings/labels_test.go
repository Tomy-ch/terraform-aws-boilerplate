package main

import (
	"errors"
	"strings"
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

	t.Run("GitHub と同じく、ラベルの名前の大文字小文字を区別せずに突き合わせる", func(t *testing.T) {
		t.Parallel()
		n, err := checkDependabotLabels([]byte(strings.ReplaceAll(dependabotFixture, `"go"`, `"Go"`)), []byte(labelsFixture))
		require.NoError(t, err)
		assert.Equal(t, 2, n)
	})

	t.Run("名指すラベルがすべて宣言にあれば、その数を返す", func(t *testing.T) {
		t.Parallel()
		n, err := checkDependabotLabels([]byte(dependabotFixture), []byte(labelsFixture))
		require.NoError(t, err)
		assert.Equal(t, 2, n)
	})

	t.Run("宣言に無いラベルを、ecosystem をまたいですべて ecosystem の名前とともに挙げる", func(t *testing.T) {
		t.Parallel()
		dependabot := "updates:\n  - package-ecosystem: \"gomod\"\n    labels:\n      - \"go\"\n      - \"x\"\n" +
			"  - package-ecosystem: \"docker\"\n    labels:\n      - \"y\"\n"
		_, err := checkDependabotLabels([]byte(dependabot), []byte(labelsFixture))
		assertOnlySentinel(t, err, errDependabotLabels)
		assert.Contains(t, err.Error(), `gomod の "x", docker の "y"`)
	})

	cases := []struct {
		name       string
		dependabot string
		labels     string
		sentinel   error
		message    string
	}{
		{"宣言に無いラベルは拒む", dependabotFixture + "    labels:\n      - \"docker\"\n", labelsFixture, errDependabotLabels, `docker の "docker"`},
		{"labels の無い ecosystem だけなら、確かめるラベルが0件なので拒む", "updates:\n  - package-ecosystem: \"docker\"\n", labelsFixture, errDependabotLabels, "ラベルを1つも名指していません"},
		{"dependabot.yml が空なら拒む", "", labelsFixture, errDependabotLabels, "ecosystem が1つもありません"},
		{"ecosystem が1つも無ければ拒む", "version: 2\nupdates: []\n", labelsFixture, errDependabotLabels, "ecosystem が1つもありません"},
		{"updates が無ければ拒む", "version: 2\n", labelsFixture, errDependabotLabels, "ecosystem が1つもありません"},
		{"YAML として読めなければ拒む", "updates: [\n", labelsFixture, errDependabotParse, dependabotFile},
		{"labels が文字列の一覧でなければ拒む", "updates:\n  - package-ecosystem: \"gomod\"\n    labels:\n      a: b\n", labelsFixture, errDependabotParse, dependabotFile},
		{"labels.json が JSON として読めなければ拒む", dependabotFixture, `x`, errLabelsParse, labelsFile},
		{"labels.json が配列でなければ拒む", dependabotFixture, `{"name": "go"}`, errLabelsParse, labelsFile},
		{"labels.json が null なら拒む", dependabotFixture, `null`, errLabelsParse, "null です"},
		{"labels.json に name の無い要素があれば拒む", dependabotFixture, `[{"name": "dependencies", "color": "0366d6"}, {"color": "000000"}]`, errLabelsDecl, "2 番目の要素に name がありません"},
		{"labels.json の color が6桁の16進でなければ、宣言として拒む", dependabotFixture, `[{"name": "dependencies", "color": "red"}, {"name": "go", "color": "16e2e2"}]`, errLabelsDecl, "6桁の16進ではありません"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := checkDependabotLabels([]byte(tc.dependabot), []byte(tc.labels))
			assertOnlySentinel(t, err, tc.sentinel)
			assert.Contains(t, err.Error(), tc.message)
		})
	}
}

// assertOnlySentinel は、err が sentinel に当たり、ラベルの検査のほかの sentinel に当たらないことを要求します。
func assertOnlySentinel(t *testing.T, err, sentinel error) {
	t.Helper()
	require.ErrorIs(t, err, sentinel)
	for _, other := range []error{errDependabotParse, errLabelsParse, errDependabotLabels, errLabelsDecl} {
		if !errors.Is(other, sentinel) {
			assert.NotErrorIs(t, err, other)
		}
	}
}

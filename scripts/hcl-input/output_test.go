package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_encodeSyntax(t *testing.T) {
	t.Parallel()

	t.Run("トップキーを置かず files と blocks を書く", func(t *testing.T) {
		t.Parallel()
		got, err := encodeSyntax(syntaxDoc{
			Files:  []fileEntry{{Path: "m/a.tf", Parsed: true}},
			Blocks: []synBlock{{File: "m/a.tf", Line: 1, Type: "locals", Labels: []string{}, Blocks: []nested{}, Attributes: []synAttr{}}},
		})
		require.NoError(t, err)
		assert.JSONEq(t, `{"files":[{"path":"m/a.tf","parsed":true}],"blocks":[{"file":"m/a.tf","line":1,"type":"locals","labels":[],"blocks":[],"attributes":[]}]}`, string(got))
	})

	t.Run("空の集合を null でなく空の配列で出す", func(t *testing.T) {
		t.Parallel()
		got, err := encodeSyntax(syntaxDoc{})
		require.NoError(t, err)
		assert.JSONEq(t, `{"files": [], "blocks": []}`, string(got))
	})
}

func Test_marshalIndent(t *testing.T) {
	t.Parallel()

	got, err := marshalIndent(map[string]string{"a": "<&>"})
	require.NoError(t, err)
	assert.Equal(t, "{\n  \"a\": \"<&>\"\n}\n", string(got))

	_, err = marshalIndent(func() {})
	require.Error(t, err)
}

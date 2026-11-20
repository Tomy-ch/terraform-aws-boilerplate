package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_encode(t *testing.T) {
	t.Parallel()

	t.Run("hcl_input の下に files と blocks を置く", func(t *testing.T) {
		t.Parallel()
		got, err := encode([]string{"modules/a/main.tf"}, []block{{
			File:   "modules/a/main.tf",
			Kind:   "resource",
			Labels: []string{"aws_s3_bucket_policy", "this"},
			Attributes: []attribute{{Path: "bucket", Refs: []ref{
				{Root: "aws_s3_bucket", Steps: []step{{Kind: stepAttr, Name: "this"}, {Kind: stepIndex, Index: &indexForm{Form: indexEachKey}}, {Kind: stepAttr, Name: "id"}}, Within: []within{}},
				{Root: "module", Steps: []step{{Kind: stepAttr, Name: "x"}, {Kind: stepAttr, Name: "key"}}, Within: []within{{Ref: 0, Step: 1}}},
			}}},
		}})
		require.NoError(t, err)
		assert.JSONEq(t, `{"hcl_input": {
			"files": ["modules/a/main.tf"],
			"blocks": [{"file": "modules/a/main.tf", "kind": "resource", "labels": ["aws_s3_bucket_policy", "this"], "attributes": [
				{"path": "bucket", "refs": [
					{"root": "aws_s3_bucket", "steps": [{"kind": "attr", "name": "this"}, {"kind": "index", "index": {"form": "each_key"}}, {"kind": "attr", "name": "id"}], "within": []},
					{"root": "module", "steps": [{"kind": "attr", "name": "x"}, {"kind": "attr", "name": "key"}], "within": [{"ref": 0, "step": 1}]}
				]}
			]}]
		}}`, string(got))
		assert.True(t, strings.HasSuffix(string(got), "}\n"), "末尾の改行がない")
	})

	t.Run("空の集合を null でなく空の配列で出す", func(t *testing.T) {
		t.Parallel()
		got, err := encode(nil, nil)
		require.NoError(t, err)
		assert.JSONEq(t, `{"hcl_input": {"files": [], "blocks": []}}`, string(got))
	})

	t.Run("ファイル名の記号を HTML のエスケープにしない", func(t *testing.T) {
		t.Parallel()
		got, err := encode([]string{`m/a<b>&"c\.tf`}, nil)
		require.NoError(t, err)
		assert.Contains(t, string(got), `"m/a<b>&\"c\\.tf"`)
	})
}

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

package main

import (
	"bytes"
	"encoding/json"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// syntaxDoc は hcl-input の JSON で、Conftest の入力のファイルそのものになります。
type syntaxDoc struct {
	Files  []fileEntry `json:"files"`
	Blocks []synBlock  `json:"blocks"`
}

// encodeSyntax は hcl-input の JSON を返します。空の集合は空の配列で出します。
func encodeSyntax(doc syntaxDoc) ([]byte, error) {
	return marshalIndent(syntaxDoc{Files: append([]fileEntry{}, doc.Files...), Blocks: append([]synBlock{}, doc.Blocks...)})
}

func marshalIndent(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, xerrors.Wrap(err, "JSON の符号化")
	}

	return buf.Bytes(), nil
}

package main

import (
	"bytes"
	"encoding/json"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

type document struct {
	HCLInput input `json:"hcl_input"`
}

type input struct {
	Files  []string `json:"files"`
	Blocks []block  `json:"blocks"`
}

// encode は、Conftest の --data に渡す JSON を返します。空の集合は null でなく空の配列で出します
// （Rego の側の形の検査が、配列でないものを落とすため）。
func encode(files []string, blocks []block) ([]byte, error) {
	doc := document{HCLInput: input{Files: append([]string{}, files...), Blocks: append([]block{}, blocks...)}}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return nil, xerrors.Wrap(err, "JSON の符号化")
	}

	return buf.Bytes(), nil
}

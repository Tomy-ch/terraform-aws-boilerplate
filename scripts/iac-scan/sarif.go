package main

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/atomicwrite"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// stagedPrefix は、コンテナの中の入力の置き場を指す uri の前置きです。
var stagedPrefix = regexp.MustCompile(`^(\.\./)*/?work/src/`)

// normalizeSARIF は、コンテナから取り出した SARIF を code scanning が読める形に書き直します。source の中で抑止した
// result（空でない suppressions を持つもの）を落とし —— code scanning は suppressions を見ずに open の alert として
// 載せる ——、artifactLocation の uri から入力の置き場の前置きを外してリポジトリの相対パスへ戻します。
// ファイルが無ければ何もしません。results が無いか null の run はそのままにします。
func normalizeSARIF(p string) error {
	raw, err := os.ReadFile(filepath.Clean(p))
	if xerrors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return xerrors.Wrapf(errResult, "%s: %v", p, err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil {
		return xerrors.Wrapf(errResult, "%s は JSON の object として読めません: %v", p, err)
	}
	runs, ok := doc["runs"].([]any)
	if !ok {
		return xerrors.Wrapf(errResult, "%s の runs が配列ではありません", p)
	}
	for i, r := range runs {
		run, ok := r.(map[string]any)
		if !ok {
			return xerrors.Wrapf(errResult, "%s の runs[%d] が object ではありません", p, i)
		}
		kept, err := unsuppressed(run["results"])
		if err != nil {
			return xerrors.Wrapf(errResult, "%s の runs[%d]: %v", p, i, err)
		}
		if kept != nil {
			run["results"] = kept
		}
	}
	if err := stripStagedPrefix(doc); err != nil {
		return xerrors.Wrapf(errResult, "%s: %v", p, err)
	}
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return xerrors.Wrapf(errResult, "%s の書き出し: %v", p, err)
	}
	if err := atomicwrite.Apply(map[string]string{p: out.String()}, 0o600); err != nil {
		return xerrors.Wrapf(err, "%s の書き込み", p)
	}

	return nil
}

// unsuppressed は、results から空でない suppressions を持つものを除いた配列を返します。results が無いか null なら
// nil を返します。
func unsuppressed(results any) ([]any, error) {
	if results == nil {
		return nil, nil
	}
	list, ok := results.([]any)
	if !ok {
		return nil, xerrors.New("results が配列ではありません")
	}
	kept := make([]any, 0, len(list))
	for i, r := range list {
		res, ok := r.(map[string]any)
		if !ok {
			return nil, xerrors.Newf("results[%d] が object ではありません", i)
		}
		switch s := res["suppressions"].(type) {
		case nil:
		case []any:
			if len(s) > 0 {
				continue
			}
		default:
			return nil, xerrors.Newf("results[%d] の suppressions が配列ではありません", i)
		}
		kept = append(kept, res)
	}

	return kept, nil
}

// stripStagedPrefix は、v の中の artifactLocation の uri から stagedPrefix を外します。uri の無い artifactLocation は
// そのままにし、object でない artifactLocation と文字列でない uri はエラーにします。
func stripStagedPrefix(v any) error {
	switch x := v.(type) {
	case map[string]any:
		if l, has := x["artifactLocation"]; has {
			loc, ok := l.(map[string]any)
			if !ok {
				return xerrors.Newf("artifactLocation が object ではありません（%v）", l)
			}
			switch uri := loc["uri"].(type) {
			case nil:
			case string:
				loc["uri"] = stagedPrefix.ReplaceAllString(uri, "")
			default:
				return xerrors.Newf("artifactLocation の uri が文字列ではありません（%v）", uri)
			}
		}
		for _, c := range x {
			if err := stripStagedPrefix(c); err != nil {
				return err
			}
		}
	case []any:
		for _, c := range x {
			if err := stripStagedPrefix(c); err != nil {
				return err
			}
		}
	}

	return nil
}

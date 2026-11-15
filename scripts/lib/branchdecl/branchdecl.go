// Package branchdecl は、分岐のパターンの単一宣言（`.github/settings/branches.json`、ADR-0603 決定1）を読み、
// 検証する。
//
// 読み手は repo-settings（ruleset と environment の生成）と apply-revert（revert のブランチの名前）の2つである。
// 同じ宣言を各々が別の規則で読むと、一方の検査を通る宣言がもう一方で壊れる（ADR-0702 決定11）。
package branchdecl

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// Branches は、分岐のパターンの宣言である。
type Branches struct {
	// Deploy は、固定名で保護するブランチ。
	Deploy []string `json:"deploy"`
	// Lines は、接頭辞（`/` で終わる）で表す、保護するブランチの線。
	Lines []string `json:"lines"`
	// Revert は、apply の失敗を戻す revert のブランチの接頭辞（`/` で終わる）。保護しないので、ruleset と
	// environment のパターンには展開しない。
	Revert string `json:"revert"`
}

var (
	deployNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
	prefixRe     = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*/$`)
)

var (
	// ErrDeclaration は、宣言を解釈できないか、宣言が規則に反することを表す。
	ErrDeclaration = xerrors.New("宣言が不正です")
	// ErrEmpty は、宣言が1件も無いことを表す。
	ErrEmpty = xerrors.New("宣言が1件もありません")
)

var branchKeys = map[string]bool{"deploy": true, "lines": true, "revert": true}

// Read は、path の宣言を読み、Validate を通したものを返します。
func Read(path string) (Branches, error) {
	var b Branches
	if err := DecodeStrict(path, &b, branchKeys); err != nil {
		return Branches{}, err
	}
	if err := Validate(b); err != nil {
		return Branches{}, xerrors.Wrapf(err, "%s", path)
	}
	return b, nil
}

// Validate は、固定名と接頭辞の形と重複、revert の接頭辞の有無と、それが lines と重ならないことを確かめます。
func Validate(b Branches) error {
	if len(b.Deploy)+len(b.Lines) == 0 {
		return ErrEmpty
	}
	seen := map[string]bool{}
	for _, name := range b.Deploy {
		if !deployNameRe.MatchString(name) {
			return xerrors.Wrapf(ErrDeclaration, "deploy の名前 %q", name)
		}
		if seen[name] {
			return xerrors.Wrapf(ErrDeclaration, "deploy の名前 %q が重複しています", name)
		}
		seen[name] = true
	}
	for _, prefix := range b.Lines {
		if !prefixRe.MatchString(prefix) {
			return xerrors.Wrapf(ErrDeclaration, "lines の接頭辞 %q（`/` で終わる名前）", prefix)
		}
		if seen[prefix] {
			return xerrors.Wrapf(ErrDeclaration, "lines の接頭辞 %q が重複しています", prefix)
		}
		seen[prefix] = true
	}
	return validateRevert(b)
}

// validateRevert は、revert の接頭辞が lines の接頭辞と重ならないことを確かめます。重なると、CI が
// GITHUB_TOKEN で push する先が保護の対象に入る。deploy の名前は `/` を持てないので重ならない。
func validateRevert(b Branches) error {
	if !prefixRe.MatchString(b.Revert) {
		return xerrors.Wrapf(ErrDeclaration, "revert の接頭辞 %q（`/` で終わる名前）", b.Revert)
	}
	for _, prefix := range b.Lines {
		if strings.HasPrefix(b.Revert, prefix) || strings.HasPrefix(prefix, b.Revert) {
			return xerrors.Wrapf(ErrDeclaration, "revert の接頭辞 %q が lines の接頭辞 %q と重なります", b.Revert, prefix)
		}
	}
	return nil
}

// DecodeStrict は path の JSON を out へ読みます。keys に無いキー、字面の違うキー、重複したキー、値の後の
// 余分なトークンを拒みます。encoding/json はキー名の大文字小文字を区別せず、重複したキーは後の値を黙って
// 採るので、デコードの前に字面で確かめる。
func DecodeStrict(path string, out any, keys map[string]bool) error {
	src, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return xerrors.Wrapf(err, "%s の読み取り", path)
	}
	if err := checkKeys(json.NewDecoder(bytes.NewReader(src)), keys); err != nil {
		return xerrors.Wrapf(err, "%s", path)
	}
	dec := json.NewDecoder(bytes.NewReader(src))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return xerrors.Wrapf(ErrDeclaration, "%s: %v", path, err)
	}
	if _, err := dec.Token(); !xerrors.Is(err, io.EOF) {
		return xerrors.Wrapf(ErrDeclaration, "%s: 値の後に余分な内容があります", path)
	}
	return nil
}

func checkKeys(dec *json.Decoder, keys map[string]bool) error {
	tok, err := dec.Token()
	if err != nil {
		return xerrors.Wrapf(ErrDeclaration, "%v", err)
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return nil
	}
	if delim == '{' {
		seen := map[string]bool{}
		for dec.More() {
			keyTok, err := dec.Token()
			if err != nil {
				return xerrors.Wrapf(ErrDeclaration, "%v", err)
			}
			key, _ := keyTok.(string)
			if !keys[key] {
				return xerrors.Wrapf(ErrDeclaration, "キー %q は宣言のキーではありません", key)
			}
			if seen[key] {
				return xerrors.Wrapf(ErrDeclaration, "キー %q が重複しています", key)
			}
			seen[key] = true
			if err := checkKeys(dec, keys); err != nil {
				return err
			}
		}
	} else {
		for dec.More() {
			if err := checkKeys(dec, keys); err != nil {
				return err
			}
		}
	}
	if _, err := dec.Token(); err != nil {
		return xerrors.Wrapf(ErrDeclaration, "%v", err)
	}
	return nil
}

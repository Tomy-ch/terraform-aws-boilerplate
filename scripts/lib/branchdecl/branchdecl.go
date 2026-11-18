// Package branchdecl は、分岐のパターンの単一宣言（`.github/settings/branches.json`、ADR-0603 決定1）を読み、
// 検証する。
//
// 読み手は repo-settings（ruleset・environment・workflow の起動条件の生成と、デプロイブランチへ入る
// Pull Request の検査）、apply-revert（revert のブランチの名前と、revert を置く commit の判定）、base-branch
// （最新のリリースライン）、release（タグの基点と線の名前）、repo-setup（初期化で作るブランチと消す線）である。
// 同じ宣言を各々が別の規則で読むと、一方の検査を通る宣言がもう一方で壊れる（ADR-0702 決定11）。
package branchdecl

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// File は、宣言のリポジトリ直下からの位置である。
const File = ".github/settings/branches.json"

// Branches は、分岐のパターンの宣言である。
type Branches struct {
	// Deploy は、デプロイブランチ（固定名で保護するブランチ）。
	Deploy []DeployBranch `json:"deploy"`
	// Lines は、接頭辞（`/` で終わる）で表す、保護するブランチの線。
	Lines Lines `json:"lines"`
	// LineVersion は、線の名前のうち接頭辞に続く版の形。受け付けるのは [VersionForm] だけである。
	LineVersion string `json:"line_version"`
	// Revert は、apply の失敗を戻す revert のブランチの接頭辞（`/` で終わる）。保護しないので、ruleset と
	// environment のパターンには展開しない。
	Revert string `json:"revert"`
}

// Lines は、線の役割ごとの接頭辞である。役割は [Roles] に閉じ、どれも欠かせない。
type Lines struct {
	// Release は、リリース線の接頭辞。
	Release string `json:"release"`
	// Hotfix は、hotfix の線の接頭辞。
	Hotfix string `json:"hotfix"`
}

// 線の役割の名前。
const (
	RoleRelease = "release"
	RoleHotfix  = "hotfix"
)

// Roles は、線の役割の名前を [Lines.Prefixes] と同じ順で返します。
func Roles() []string {
	return []string{RoleRelease, RoleHotfix}
}

// Prefixes は、空でない線の接頭辞を役割の順（release, hotfix）で返します。
func (l Lines) Prefixes() []string {
	out := make([]string, 0, len(Roles()))
	for _, role := range Roles() {
		if p, _ := l.Prefix(role); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Prefix は、役割 role の線の接頭辞を返します。role が役割でなければ ok は false。
func (l Lines) Prefix(role string) (prefix string, ok bool) {
	switch role {
	case RoleRelease:
		return l.Release, true
	case RoleHotfix:
		return l.Hotfix, true
	}
	return "", false
}

// DeployBranch は、デプロイブランチ1つの宣言である（ADR-0601 決定4）。
type DeployBranch struct {
	Branch string `json:"branch"`
	// Env は、そのブランチへの merge が apply する env。空なら apply しない。
	Env string `json:"env,omitempty"`
	// From は、そのブランチへ変更を運んでよい線（Lines の要素）。Env を持つ entry だけが持つ。
	From []string `json:"from,omitempty"`
	// ReleaseBase は、タグを打ち、新しい線を切る基点のブランチであることを表す。宣言にちょうど1つ在る。
	ReleaseBase bool `json:"release_base,omitempty"`
}

// ReleaseBase は、release_base の印を持つデプロイブランチを返します。[Validate] を通った宣言では必ず在る。
func (b Branches) ReleaseBase() (DeployBranch, bool) {
	for _, d := range b.Deploy {
		if d.ReleaseBase {
			return d, true
		}
	}
	return DeployBranch{}, false
}

// VersionForm は、線の名前の版の形として受け付ける唯一の値である。
const VersionForm = "v<major>.<minor>.<patch>"

// Version は、線の名前とタグの版である。
type Version struct {
	Major, Minor, Patch int
}

var versionRe = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)$`)

// ParseVersion は、[VersionForm] の形の s を読みます。形が違うか、桁が int に収まらなければ ok は false。
// プレリリースとビルドメタデータは受け付けない。
func ParseVersion(s string) (Version, bool) {
	m := versionRe.FindStringSubmatch(s)
	if m == nil {
		return Version{}, false
	}
	var parts [3]int
	for i, digits := range m[1:] {
		n, err := strconv.Atoi(digits)
		if err != nil {
			return Version{}, false
		}
		parts[i] = n
	}
	return Version{Major: parts[0], Minor: parts[1], Patch: parts[2]}, true
}

// String は、[VersionForm] の形で版を返します。
func (v Version) String() string {
	return "v" + strconv.Itoa(v.Major) + "." + strconv.Itoa(v.Minor) + "." + strconv.Itoa(v.Patch)
}

// Less は、v が o より前の版かを数値で比べて返します。文字列の順では v1.10.0 が v1.9.0 より前に並ぶ。
func (v Version) Less(o Version) bool {
	if v.Major != o.Major {
		return v.Major < o.Major
	}
	if v.Minor != o.Minor {
		return v.Minor < o.Minor
	}
	return v.Patch < o.Patch
}

// LineName は、役割 role の線の版 v の名前を返します。role が役割でなければ ok は false。
func (b Branches) LineName(role string, v Version) (string, bool) {
	prefix, ok := b.Lines.Prefix(role)
	if !ok || prefix == "" {
		return "", false
	}
	return prefix + v.String(), true
}

// ParseLine は、name を線の名前として読み、その役割と版を返します。どの線の接頭辞にも当たらないか、
// 接頭辞に続く部分が版の形でなければ ok は false。
func (b Branches) ParseLine(name string) (role string, v Version, ok bool) {
	for _, r := range Roles() {
		prefix, _ := b.Lines.Prefix(r)
		rest, found := strings.CutPrefix(name, prefix)
		if prefix == "" || !found {
			continue
		}
		if v, ok := ParseVersion(rest); ok {
			return r, v, true
		}
		return "", Version{}, false
	}
	return "", Version{}, false
}

// IsLine は、name がどれかの線の接頭辞で始まり、接頭辞より長いかを返します。版の形は問わない。
func (b Branches) IsLine(name string) bool {
	for _, prefix := range b.Lines.Prefixes() {
		if strings.HasPrefix(name, prefix) && len(name) > len(prefix) {
			return true
		}
	}
	return false
}

// DeployNames は、デプロイブランチの名前を宣言の順に返します。
func (b Branches) DeployNames() []string {
	out := make([]string, 0, len(b.Deploy))
	for _, d := range b.Deploy {
		out = append(out, d.Branch)
	}
	return out
}

// DeployForEnv は、env を apply するデプロイブランチを返します。
func (b Branches) DeployForEnv(env string) (DeployBranch, bool) {
	for _, d := range b.Deploy {
		if d.Env != "" && d.Env == env {
			return d, true
		}
	}
	return DeployBranch{}, false
}

// DeployForBranch は、名前が branch で、env を持つデプロイブランチを返します。
func (b Branches) DeployForBranch(branch string) (DeployBranch, bool) {
	for _, d := range b.Deploy {
		if d.Env != "" && d.Branch == branch {
			return d, true
		}
	}
	return DeployBranch{}, false
}

// FromLine は、line（ブランチ名）が d.From のどれかの線に当たるかを返します。
func (d DeployBranch) FromLine(line string) bool {
	for _, prefix := range d.From {
		if strings.HasPrefix(line, prefix) && len(line) > len(prefix) {
			return true
		}
	}
	return false
}

var (
	deployNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
	envRe        = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	prefixRe     = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*/$`)
)

var (
	// ErrDeclaration は、宣言を解釈できないか、宣言が規則に反することを表す。
	ErrDeclaration = xerrors.New("宣言が不正です")
	// ErrEmpty は、宣言が1件も無いことを表す。
	ErrEmpty = xerrors.New("宣言が1件もありません")
)

var branchKeys = map[string]bool{
	"deploy": true, "lines": true, "line_version": true, "revert": true,
	"branch": true, "env": true, "from": true, "release_base": true,
	RoleRelease: true, RoleHotfix: true,
}

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

// Validate は、固定名と接頭辞の形と重複、線の役割が揃っていること、版の形、release_base の印がちょうど1つで
// あること、デプロイブランチの env と from、revert の接頭辞の有無と、それが lines と重ならないことを確かめます。
func Validate(b Branches) error {
	if len(b.Deploy) == 0 && b.Lines == (Lines{}) {
		return ErrEmpty
	}
	seen := map[string]bool{}
	for _, role := range Roles() {
		prefix, _ := b.Lines.Prefix(role)
		if !prefixRe.MatchString(prefix) {
			return xerrors.Wrapf(ErrDeclaration, "lines の %s の接頭辞 %q（`/` で終わる名前）", role, prefix)
		}
		if seen[prefix] {
			return xerrors.Wrapf(ErrDeclaration, "lines の接頭辞 %q が重複しています", prefix)
		}
		seen[prefix] = true
	}
	if b.LineVersion != VersionForm {
		return xerrors.Wrapf(ErrDeclaration, "line_version %q（受け付けるのは %q だけです）", b.LineVersion, VersionForm)
	}
	envs := map[string]bool{}
	bases := 0
	for _, d := range b.Deploy {
		if !deployNameRe.MatchString(d.Branch) {
			return xerrors.Wrapf(ErrDeclaration, "deploy の branch %q", d.Branch)
		}
		if seen[d.Branch] {
			return xerrors.Wrapf(ErrDeclaration, "deploy の branch %q が重複しています", d.Branch)
		}
		seen[d.Branch] = true
		if d.ReleaseBase {
			bases++
		}
		if err := validateDeployEnv(d, envs, b.Lines.Prefixes()); err != nil {
			return err
		}
	}
	if bases != 1 {
		return xerrors.Wrapf(ErrDeclaration, "release_base の印を持つ deploy の entry が %d 件あります（ちょうど1件）", bases)
	}
	return validateRevert(b)
}

// validateDeployEnv は、env を持つデプロイブランチが1つ以上の from を持ち、その各々が lines の接頭辞であることと、
// env を持たない entry が from を持たないことを確かめます。from の無い env は、どの線からも変更を受け取れない。
func validateDeployEnv(d DeployBranch, envs map[string]bool, lines []string) error {
	if d.Env == "" {
		if d.From != nil {
			return xerrors.Wrapf(ErrDeclaration, "deploy の branch %q は env を持たないので from を持てません", d.Branch)
		}
		return nil
	}
	if !envRe.MatchString(d.Env) {
		return xerrors.Wrapf(ErrDeclaration, "deploy の branch %q の env %q", d.Branch, d.Env)
	}
	if envs[d.Env] {
		return xerrors.Wrapf(ErrDeclaration, "env %q のデプロイブランチが複数あります", d.Env)
	}
	envs[d.Env] = true
	if len(d.From) == 0 {
		return xerrors.Wrapf(ErrDeclaration, "deploy の branch %q は from を1つ以上持たなければなりません", d.Branch)
	}
	from := map[string]bool{}
	for _, prefix := range d.From {
		if !slices.Contains(lines, prefix) {
			return xerrors.Wrapf(ErrDeclaration, "deploy の branch %q の from %q は lines の接頭辞ではありません", d.Branch, prefix)
		}
		if from[prefix] {
			return xerrors.Wrapf(ErrDeclaration, "deploy の branch %q の from %q が重複しています", d.Branch, prefix)
		}
		from[prefix] = true
	}
	return nil
}

// validateRevert は、revert の接頭辞が lines の接頭辞と重ならないことを確かめます。重なると、CI が
// GITHUB_TOKEN で push する先が保護の対象に入る。deploy の名前は `/` を持てないので重ならない。
func validateRevert(b Branches) error {
	if !prefixRe.MatchString(b.Revert) {
		return xerrors.Wrapf(ErrDeclaration, "revert の接頭辞 %q（`/` で終わる名前）", b.Revert)
	}
	for _, prefix := range b.Lines.Prefixes() {
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

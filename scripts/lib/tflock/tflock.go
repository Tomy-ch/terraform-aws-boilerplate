// Package tflock は、provider の版の正本（ADR-0205 決定9）の読み手です。正本は2つのファイルから成ります。
// providers/providers.toml は各 provider の版を1つに固定する宣言で、人が書きます。providers/.terraform.lock.hcl は
// そこから `terraform providers lock` が書く生成物です。
//
// どちらも決まった形だけを読み、知らない行・key・block はエラーにします（ADR-0702 決定14）。読み飛ばすと、その
// provider は検査されないまま init が使います。
package tflock

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const (
	// Dir は正本を置くディレクトリ（リポジトリ直下からの相対）。
	Dir = "providers"
	// DeclFile は版の宣言のファイル名。
	DeclFile = "providers.toml"
	// LockFile は terraform が書く lock のファイル名。正本も root もこの名前を持つ。
	LockFile = ".terraform.lock.hcl"
	// Registry は provider の取得元の host。宣言の source はこの host の下の namespace/type だけを書く。
	Registry = "registry.terraform.io"
)

var (
	// ErrInvalidDecl は providers.toml を読めないか、決まった形でないときのエラー。
	ErrInvalidDecl = xerrors.New("providers.toml を解釈できません")
	// ErrInvalidLock は lock を読めないか、決まった形でないときのエラー。
	ErrInvalidLock = xerrors.New("lock を解釈できません")
	// ErrInvalidPlatform は lockfile_platforms に terraform の platform へ写せない名前があるときのエラー。
	ErrInvalidPlatform = xerrors.New("lockfile_platforms を terraform の platform へ写せません")
)

var (
	declSectionRe = regexp.MustCompile(`^\[([a-z][a-z0-9_-]*)\]$`)
	declFieldRe   = regexp.MustCompile(`^([a-z]+)\s*=\s*"([^"]*)"$`)
	sourceRe      = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*/[a-z0-9][a-z0-9-]*$`)
	// versionRe は完全一致の版。範囲や `~>` を書くと、生成した日の最新を採ることになる。
	versionRe = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
	// providerLineRe は、block の数を hclsyntax とは別の経路で数える（ADR-0702 決定15）。
	providerLineRe = regexp.MustCompile(`(?m)^provider\s+"`)
)

// Decl は providers.toml の provider 1つです。
type Decl struct {
	// Name は節の名前（terraform の required_providers のローカル名）。
	Name    string
	Source  string
	Version string
}

// Addr は lock の block の見出しになる完全な名前（`registry.terraform.io/hashicorp/aws`）です。
func (d Decl) Addr() string {
	return Registry + "/" + d.Source
}

// Provider は lock の provider block 1つです。
type Provider struct {
	Addr    string
	Version string
	Hashes  []string
}

// ParseDecl は providers.toml を、書かれた順で返します。節は名前1つと source・version の2つの key だけを持ちます。
// 空の宣言もエラーです（ADR-0702 決定13）。
func ParseDecl(content []byte) ([]Decl, error) {
	var decls []Decl
	seen := map[string]bool{}
	var fields map[string]bool
	for i, raw := range strings.Split(string(content), "\n") {
		line := strings.TrimSpace(raw)
		where := strconv.Itoa(i+1) + "行目: " + line
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if m := declSectionRe.FindStringSubmatch(line); m != nil {
			if seen[m[1]] {
				return nil, xerrors.Wrap(ErrInvalidDecl, "節の重複 "+where)
			}
			seen[m[1]] = true
			decls = append(decls, Decl{Name: m[1]})
			fields = map[string]bool{}

			continue
		}
		m := declFieldRe.FindStringSubmatch(line)
		if m == nil || len(decls) == 0 {
			return nil, xerrors.Wrap(ErrInvalidDecl, where)
		}
		if fields[m[1]] {
			return nil, xerrors.Wrap(ErrInvalidDecl, m[1]+" の重複 "+where)
		}
		fields[m[1]] = true
		cur := &decls[len(decls)-1]
		switch m[1] {
		case "source":
			if !sourceRe.MatchString(m[2]) {
				return nil, xerrors.Wrap(ErrInvalidDecl, "source は <namespace>/<type> で書く "+where)
			}
			cur.Source = m[2]
		case "version":
			if !versionRe.MatchString(m[2]) {
				return nil, xerrors.Wrap(ErrInvalidDecl, "version は完全一致の版（X.Y.Z）で書く "+where)
			}
			cur.Version = m[2]
		default:
			return nil, xerrors.Wrap(ErrInvalidDecl, "知らない key "+where)
		}
	}
	if len(decls) == 0 {
		return nil, xerrors.Wrap(ErrInvalidDecl, "provider が0件です")
	}
	sources := map[string]bool{}
	for _, d := range decls {
		if d.Source == "" || d.Version == "" {
			return nil, xerrors.Wrapf(ErrInvalidDecl, "[%s] に source か version がありません", d.Name)
		}
		if sources[d.Source] {
			return nil, xerrors.Wrapf(ErrInvalidDecl, "source %s の重複", d.Source)
		}
		sources[d.Source] = true
	}

	return decls, nil
}

// ParseDeclFile は path を読んで ParseDecl します。
func ParseDeclFile(path string) ([]Decl, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, xerrors.Wrap(err, path)
	}
	decls, err := ParseDecl(data)
	if err != nil {
		return nil, xerrors.Wrap(err, path)
	}

	return decls, nil
}

// VersionsTF は、decls を固定した版で要求する versions.tf の中身です。lock を作る作業ディレクトリに置きます。
func VersionsTF(decls []Decl) string {
	var b strings.Builder
	b.WriteString("terraform {\n  required_providers {\n")
	for _, d := range decls {
		fmt.Fprintf(&b, "    %s = {\n      source  = %q\n      version = %q\n    }\n", d.Name, d.Source, d.Version)
	}
	b.WriteString("  }\n}\n")

	return b.String()
}

// ParseLock は lock の provider block を返します。block は provider だけ、属性は version・constraints・hashes だけを
// 読みます。0件、同じ provider の block の重複、block の数と行頭の `provider "` の数の食い違いはエラーです。
func ParseLock(content []byte, name string) ([]Provider, error) {
	file, diags := hclsyntax.ParseConfig(content, name, hcl.InitialPos)
	if diags.HasErrors() {
		return nil, xerrors.Wrap(ErrInvalidLock, diags.Error())
	}
	body, ok := file.Body.(*hclsyntax.Body)
	if !ok {
		return nil, xerrors.Wrap(ErrInvalidLock, name)
	}
	if len(body.Attributes) > 0 {
		return nil, xerrors.Wrap(ErrInvalidLock, name+" の最上位に属性があります")
	}
	var providers []Provider
	seen := map[string]bool{}
	for _, block := range body.Blocks {
		p, err := providerOf(block, name)
		if err != nil {
			return nil, err
		}
		if seen[p.Addr] {
			return nil, xerrors.Wrapf(ErrInvalidLock, "%s: provider %s の重複", name, p.Addr)
		}
		seen[p.Addr] = true
		providers = append(providers, p)
	}
	if len(providers) == 0 {
		return nil, xerrors.Wrapf(ErrInvalidLock, "%s: provider が0件です", name)
	}
	if n := len(providerLineRe.FindAll(content, -1)); n != len(providers) {
		return nil, xerrors.Wrapf(ErrInvalidLock, "%s: provider の行 %d 件のうち %d 件しか読めていません", name, n, len(providers))
	}

	return providers, nil
}

func providerOf(block *hclsyntax.Block, name string) (Provider, error) {
	if block.Type != "provider" || len(block.Labels) != 1 || len(block.Body.Blocks) > 0 {
		return Provider{}, xerrors.Wrapf(ErrInvalidLock, "%s:%d: 知らない block %s", name, block.TypeRange.Start.Line, block.Type)
	}
	p := Provider{Addr: block.Labels[0]}
	for key, attr := range block.Body.Attributes {
		val, diags := attr.Expr.Value(nil)
		if diags.HasErrors() {
			return Provider{}, xerrors.Wrapf(ErrInvalidLock, "%s:%d: %s", name, attr.NameRange.Start.Line, diags.Error())
		}
		switch key {
		case "version":
			if val.Type() != cty.String {
				return Provider{}, xerrors.Wrapf(ErrInvalidLock, "%s: %s の version が文字列ではありません", name, p.Addr)
			}
			p.Version = val.AsString()
		case "constraints":
			// init が書く制約の記録で、照合には使わない。
		case "hashes":
			hashes, err := stringList(val)
			if err != nil {
				return Provider{}, xerrors.Wrapf(ErrInvalidLock, "%s: %s の hashes: %v", name, p.Addr, err)
			}
			p.Hashes = hashes
		default:
			return Provider{}, xerrors.Wrapf(ErrInvalidLock, "%s: %s の知らない属性 %s", name, p.Addr, key)
		}
	}
	if p.Version == "" || len(p.Hashes) == 0 {
		return Provider{}, xerrors.Wrapf(ErrInvalidLock, "%s: %s に version か hashes がありません", name, p.Addr)
	}

	return p, nil
}

func stringList(val cty.Value) ([]string, error) {
	if !val.Type().IsTupleType() && !val.Type().IsListType() {
		return nil, xerrors.New("文字列の並びではありません")
	}
	var out []string
	for it := val.ElementIterator(); it.Next(); {
		_, v := it.Element()
		if v.Type() != cty.String {
			return nil, xerrors.New("文字列でない要素があります")
		}
		out = append(out, v.AsString())
	}

	return out, nil
}

// terraformPlatforms は、mise の lockfile_platforms の名前を terraform の platform へ写す表です。
var terraformPlatforms = map[string]string{
	"linux-x64":   "linux_amd64",
	"linux-arm64": "linux_arm64",
	"macos-arm64": "darwin_arm64",
	"macos-x64":   "darwin_amd64",
}

// Platforms は、mise の lockfile_platforms を terraform の platform へ写します。道具の platform の宣言を1か所
// （mise.toml）に保つためです。空と、写せない名前はエラーです。
func Platforms(mise []string) ([]string, error) {
	if len(mise) == 0 {
		return nil, xerrors.Wrap(ErrInvalidPlatform, "lockfile_platforms が空です")
	}
	out := make([]string, 0, len(mise))
	for _, m := range mise {
		p, ok := terraformPlatforms[m]
		if !ok {
			return nil, xerrors.Wrapf(ErrInvalidPlatform, "%q", m)
		}
		out = append(out, p)
	}

	return out, nil
}

// Check は、正本の lock が宣言と同じ provider の集合と版を持ち、各 provider が platform ごとの `h1:` と1つ以上の
// `zh:` を持つことを確かめ、食い違いを返します。`h1:` は重複を除いて数えます —— 素の `terraform init` は自分の
// platform の1つだけを書くので、数が platform の数と違えば `providers lock -platform` で作った形ではありません。
func Check(decls []Decl, providers []Provider, platforms int) []string {
	var problems []string
	want := map[string]Decl{}
	for _, d := range decls {
		want[d.Addr()] = d
	}
	got := map[string]Provider{}
	for _, p := range providers {
		got[p.Addr] = p
	}
	for _, addr := range sortedKeys(want) {
		p, ok := got[addr]
		switch {
		case !ok:
			problems = append(problems, addr+" が lock にありません")
		case p.Version != want[addr].Version:
			problems = append(problems, fmt.Sprintf("%s の版が lock は %s、providers.toml は %s です", addr, p.Version, want[addr].Version))
		}
	}
	for _, addr := range sortedKeys(got) {
		if _, ok := want[addr]; !ok {
			problems = append(problems, addr+" は providers.toml にありません")
		}
		h1, zh := hashCounts(got[addr].Hashes)
		if h1 != platforms {
			problems = append(problems, fmt.Sprintf("%s の h1: が %d 種で、platform の %d と合いません（make tf-providers-lock で作り直す）", addr, h1, platforms))
		}
		if zh == 0 {
			problems = append(problems, addr+" に zh: がありません")
		}
	}

	return problems
}

func hashCounts(hashes []string) (h1, zh int) {
	seen := map[string]bool{}
	for _, h := range hashes {
		if seen[h] {
			continue
		}
		seen[h] = true
		switch {
		case strings.HasPrefix(h, "h1:"):
			h1++
		case strings.HasPrefix(h, "zh:"):
			zh++
		}
	}

	return h1, zh
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	return keys
}

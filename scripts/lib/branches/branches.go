// Package branches は、分岐のパターンの単一宣言を読みます。
//
// 呼び出し側は4つある —— 宣言を生成先へ反映する `branches` と、実行時に読む
// `base-branch` / `release` / `repo-setup` である。**いずれもパターンを自分で持たない**
// （ADR-0603 決定2）。同じパターンが2箇所に在ると、片方だけが古くなる。
//
// 解釈できない行は読み飛ばさずエラーにします。読み飛ばすと、打ち間違えた1行が
// 「宣言されていない」と同じ扱いになり、保護対象から黙って消えます。
package branches

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// File は宣言の既定の場所。呼び出し側はリポジトリ直下から起動される前提です。
const File = ".github/branches.toml"

var (
	// ErrSyntax は、宣言に解釈できない行があったことを表す。
	ErrSyntax = xerrors.New("宣言に解釈できない行があります")
	// ErrEmpty は、宣言が1件も読めなかったことを表す。
	ErrEmpty = xerrors.New("分岐のパターンの宣言が空です")
	// ErrUnknownSet は、存在しない集合を参照したことを表す。
	ErrUnknownSet = xerrors.New("宣言に無い集合です")
	// ErrUnknownKey は、存在しないキーを参照したことを表す。
	ErrUnknownKey = xerrors.New("宣言に無いキーです")

	sectionRe = regexp.MustCompile(`^\s*\[([^\]]+)\]\s*$`)
	scalarRe  = regexp.MustCompile(`^\s*"?([A-Za-z0-9_.@/-]+)"?\s*=\s*(?:"([^"]*)"|'([^']*)')\s*$`)
	arrayRe   = regexp.MustCompile(`^\s*"?([A-Za-z0-9_.@/-]+)"?\s*=\s*\[(.*)\]\s*$`)
	itemRe    = regexp.MustCompile(`"([^"]*)"`)
)

// Declaration は宣言の中身です。
type Declaration struct {
	sets      map[string][]string
	scalars   map[string]string
	workflows map[string]string
}

// Load は path の宣言を読みます。
func Load(path string) (Declaration, error) {
	content, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return Declaration{}, xerrors.Wrap(err, path)
	}

	return Parse(string(content), path)
}

// Parse は宣言の本文を読み取ります。path はエラーに添える位置情報にのみ使います。
func Parse(content, path string) (Declaration, error) {
	decl := Declaration{
		sets:      map[string][]string{},
		scalars:   map[string]string{},
		workflows: map[string]string{},
	}

	section := ""
	for i, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		if m := sectionRe.FindStringSubmatch(trimmed); m != nil {
			section = m[1]

			continue
		}

		if m := arrayRe.FindStringSubmatch(trimmed); m != nil && section == "sets" {
			items := make([]string, 0, len(m[2]))
			for _, item := range itemRe.FindAllStringSubmatch(m[2], -1) {
				items = append(items, item[1])
			}
			decl.sets[m[1]] = items

			continue
		}

		m := scalarRe.FindStringSubmatch(trimmed)
		if m == nil {
			return Declaration{}, xerrors.Wrap(ErrSyntax, fmt.Sprintf("%s:%d %s", path, i+1, trimmed))
		}

		value := m[2] + m[3]
		if section == "workflows" {
			decl.workflows[m[1]] = value

			continue
		}
		decl.scalars[section+"."+m[1]] = value
	}

	if len(decl.sets) == 0 || len(decl.scalars) == 0 {
		return Declaration{}, xerrors.Wrap(ErrEmpty, path)
	}

	return decl, nil
}

// Set は名前の付いた集合を返します。存在しない名前はエラーです —— 空を返すと、
// 打ち間違えた名前が「要素0件の集合」に化け、対象を1件も持たないまま通ります。
func (d Declaration) Set(name string) ([]string, error) {
	items, ok := d.sets[name]
	if !ok {
		return nil, xerrors.Wrap(ErrUnknownSet, name)
	}

	return items, nil
}

// Scalar は単一の値を返します。キーは `<section>.<name>` です。
func (d Declaration) Scalar(key string) (string, error) {
	value, ok := d.scalars[key]
	if !ok {
		return "", xerrors.Wrap(ErrUnknownKey, key)
	}

	return value, nil
}

// Workflows は、push 側の起動条件を生成する先と、それが読む集合の名前の対応を返します。
func (d Declaration) Workflows() map[string]string {
	out := make(map[string]string, len(d.workflows))
	for k, v := range d.workflows {
		out[k] = v
	}

	return out
}

// DefaultBranch はリリースの起点であり GitHub のデフォルトブランチでもあるブランチ名。
func (d Declaration) DefaultBranch() (string, error) { return d.Scalar("default.branch") }

// ReleasePrefix はリリース線の接頭辞。前方一致の判定に使います。
func (d Declaration) ReleasePrefix() (string, error) { return d.Scalar("release.prefix") }

// ReleasePattern はリリース線の形。捕捉群は major / minor / patch の順です。
func (d Declaration) ReleasePattern() (*regexp.Regexp, error) {
	pattern, err := d.Scalar("release.pattern")
	if err != nil {
		return nil, err
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, xerrors.Wrap(ErrSyntax, "release.pattern: "+pattern)
	}

	return re, nil
}

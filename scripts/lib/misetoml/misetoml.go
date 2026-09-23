// Package misetoml は mise.toml の `[tools]` が宣言する版を読みます。
//
// **この宣言を読む実装をここ1つに保ちます。** 同じ表を複数の実装が各々の正規表現で読むと、
// 一方だけが読める宣言が生まれ、もう一方は黙ってその道具を検査の対象から外します。落ちるのは
// 検査そのものではなく、検査の対象の方です。
//
// 解釈できない行をエラーにするのも同じ理由です（ADR-0702 決定14）。読み飛ばしは「そのエントリは
// 検査されなかった」を、何も赤くならないまま作ります。
package misetoml

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

// BareKeyPattern は TOML の裸キーに使える文字です。仕様が許すのは ASCII 英数字と `_` `-` だけで、
// **数字始まりも正しい**。`.` は dotted key の区切りであって裸キーの文字ではなく、`:` や `/` を
// 含むキー（`aqua:owner/repo` など）は引用符を要します。
const BareKeyPattern = `[A-Za-z0-9_-]+`

var (
	// ErrInvalidLine は `[tools]` に解釈できない行があった場合のエラー。
	ErrInvalidLine = xerrors.New("mise.toml の [tools] に解釈できない行があります")
	// ErrDuplicateKey は `[tools]` で同一キーが複数回現れた場合のエラー。
	ErrDuplicateKey = xerrors.New("mise.toml の [tools] にキーの重複があります")
)

var (
	// sectionRe は TOML の table 見出し。`[tools.foo]` は `tools` と別の節として扱われる。
	sectionRe = regexp.MustCompile(`^\[([^\]]+)\]\s*(?:#.*)?$`)
	// valueRe は `key = "版"`。キーは裸でも引用符付きでもよい。
	valueRe = regexp.MustCompile(`^(?:"([^"]+)"|(` + BareKeyPattern + `))\s*=\s*"([^"]+)"\s*(?:#.*)?$`)
	// tableRe は tool option 付きの1行宣言 `key = { version = "版", ... }`。
	tableRe = regexp.MustCompile(
		`^(?:"([^"]+)"|(` + BareKeyPattern + `))\s*=\s*\{.*\bversion\s*=\s*"([^"]+)".*\}\s*(?:#.*)?$`,
	)
)

// Entry は `[tools]` の宣言1件です。
type Entry struct {
	// Key は宣言に書かれたキーそのもの。引用符は外すが、`aqua:` などの backend 接頭辞は残す。
	Key string
	// Version は宣言された版の文字列。形の検査はしない —— 何が版として妥当かは呼び手が決める。
	Version string
}

// Parse は `[tools]` の宣言を、書かれた順で返します。
//
// `[settings]` や `[env]` が持つ版らしき値を拾わないよう、節を見て範囲を限ります。`[tools]` の
// 中で解釈できない行に当たったらエラーを返します —— **読み飛ばして先へ進みません。**
//
// 受け付ける形は `key = "版"` と `key = { version = "版", ... }` の2つです。複数版を並べる配列
// （`go = ["1.27.1", "1.26.0"]`）は受け付けません。どれが有効かを黙って選ぶより、呼び手に
// 気づかせる方が安全だからです。
func Parse(content []byte) ([]Entry, error) {
	var entries []Entry
	seen := make(map[string]int)
	section := ""

	sc := bufio.NewScanner(bytes.NewReader(content))
	for lineNo := 1; sc.Scan(); lineNo++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if m := sectionRe.FindStringSubmatch(line); m != nil {
			section = m[1]

			continue
		}
		if section != "tools" {
			continue
		}

		key, version, ok := parseLine(line)
		if !ok {
			return nil, xerrors.Wrap(ErrInvalidLine, strconv.Itoa(lineNo)+"行目: "+line)
		}
		if prev, dup := seen[key]; dup {
			return nil, xerrors.Wrap(ErrDuplicateKey,
				strconv.Itoa(lineNo)+"行目: "+key+"（"+strconv.Itoa(prev)+"行目にもあります）")
		}
		seen[key] = lineNo
		entries = append(entries, Entry{Key: key, Version: version})
	}
	if err := sc.Err(); err != nil {
		return nil, xerrors.Wrap(err, "mise.toml の走査")
	}

	return entries, nil
}

// ParseFile は path を読んで Parse します。
func ParseFile(path string) ([]Entry, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, xerrors.Wrap(err, path)
	}

	entries, err := Parse(data)
	if err != nil {
		return nil, xerrors.Wrap(err, path)
	}

	return entries, nil
}

// Lookup は key の版を返します。2つ目の戻り値は宣言が在ったかどうかです。
func Lookup(entries []Entry, key string) (string, bool) {
	for _, e := range entries {
		if e.Key == key {
			return e.Version, true
		}
	}

	return "", false
}

// parseLine は `[tools]` の1行を key と版へ割ります。どちらの形にも当たらなければ ok が false。
func parseLine(line string) (key, version string, ok bool) {
	m := valueRe.FindStringSubmatch(line)
	if m == nil {
		m = tableRe.FindStringSubmatch(line)
	}
	if m == nil {
		return "", "", false
	}

	// 引用符付きなら m[1]、裸なら m[2] に入る。両方が空になる経路は正規表現が持たない。
	key = m[1]
	if key == "" {
		key = m[2]
	}

	return key, m[3], true
}

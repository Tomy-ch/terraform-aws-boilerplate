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

// LockFile は `mise lock` が書く lockfile の名前です。
const LockFile = "mise.lock"

// ErrInvalidLock は mise.lock に解釈できない行があった場合のエラー。
var ErrInvalidLock = xerrors.New("mise.lock に解釈できない行があります")

var (
	// lockEntryRe は道具の entry の見出し `[[tools.<key>]]`。
	lockEntryRe = regexp.MustCompile(`^\[\[tools\.(?:"([^"]+)"|(` + BareKeyPattern + `))\]\]$`)
	// lockPlatformRe は platform の見出し `[tools.<key>."platforms.<platform>"]`。
	lockPlatformRe = regexp.MustCompile(`^\[tools\.(?:"([^"]+)"|(` + BareKeyPattern + `))\."platforms\.([a-z0-9_-]+)"\]$`)
	// lockStringRe は `name = "値"`。
	lockStringRe = regexp.MustCompile(`^([a-z_]+)\s*=\s*"([^"]*)"$`)
	// lockArrayRe は1行の文字列の配列 `name = ["値", ...]`。
	lockArrayRe = regexp.MustCompile(`^([a-z_]+)\s*=\s*\[[^\]]*\]$`)
	// lockFormatRe は先頭の `lockfile_version = <整数>`。
	lockFormatRe = regexp.MustCompile(`^lockfile_version\s*=\s*([0-9]+)$`)
)

// LockEntry は mise.lock の道具1件です。
type LockEntry struct {
	// Key は mise.toml の [tools] のキーと同じ表記。
	Key string
	// Version は lock が固定した版。
	Version string
	// Backend は mise が解決した backend（`aqua:owner/repo` など）。
	Backend string
	// Platforms は platform の名前（`linux-x64` など）ごとの取得元。
	Platforms map[string]LockPlatform
}

// LockPlatform は1つの platform の取得元です。
type LockPlatform struct {
	URL      string
	Checksum string
}

// ParseLock は mise.lock の道具を、書かれた順で返します。
//
// 読むのは `mise lock` が書く形だけで、知らない行と知らない key はエラーにします（ADR-0702 決定14）。
// 読み飛ばすと、その取得元は検査されないまま CI が使います。同じ道具の entry が2つある lock も
// エラーにします —— mise.toml は道具ごとに1つの版しか宣言しません。
func ParseLock(content []byte) ([]LockEntry, error) {
	var (
		entries  []LockEntry
		cur      *LockEntry
		platform string
		format   bool
	)
	seen := map[string]bool{}

	sc := bufio.NewScanner(bytes.NewReader(content))
	for lineNo := 1; sc.Scan(); lineNo++ {
		line := strings.TrimSpace(sc.Text())
		where := strconv.Itoa(lineNo) + "行目: " + line
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if m := lockEntryRe.FindStringSubmatch(line); m != nil {
			key := m[1] + m[2]
			if seen[key] {
				return nil, xerrors.Wrap(ErrInvalidLock, "道具の entry の重複 "+where)
			}
			seen[key] = true
			entries = append(entries, LockEntry{Key: key, Platforms: map[string]LockPlatform{}})
			cur, platform = &entries[len(entries)-1], ""

			continue
		}
		if m := lockPlatformRe.FindStringSubmatch(line); m != nil {
			if cur == nil || cur.Key != m[1]+m[2] {
				return nil, xerrors.Wrap(ErrInvalidLock, "道具の entry の外の platform "+where)
			}
			if _, dup := cur.Platforms[m[3]]; dup {
				return nil, xerrors.Wrap(ErrInvalidLock, "platform の重複 "+where)
			}
			platform = m[3]
			cur.Platforms[platform] = LockPlatform{}

			continue
		}
		if cur == nil {
			m := lockFormatRe.FindStringSubmatch(line)
			if m == nil || format {
				return nil, xerrors.Wrap(ErrInvalidLock, where)
			}
			format = true

			continue
		}
		if err := lockField(cur, platform, line); err != nil {
			return nil, xerrors.Wrap(err, where)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, xerrors.Wrap(err, "mise.lock の走査")
	}
	if !format {
		return nil, xerrors.Wrap(ErrInvalidLock, "lockfile_version がありません")
	}
	for _, e := range entries {
		if e.Version == "" || e.Backend == "" {
			return nil, xerrors.Wrap(ErrInvalidLock, e.Key+" に version か backend がありません")
		}
	}

	return entries, nil
}

// lockField は entry か platform の中の1行を cur へ書きます。
func lockField(cur *LockEntry, platform, line string) error {
	if platform == "" {
		if m := lockArrayRe.FindStringSubmatch(line); m != nil && m[1] == "specifiers" {
			return nil
		}
		m := lockStringRe.FindStringSubmatch(line)
		if m == nil {
			return ErrInvalidLock
		}
		switch m[1] {
		case "version":
			cur.Version = m[2]
		case "backend":
			cur.Backend = m[2]
		default:
			return xerrors.Wrap(ErrInvalidLock, "知らない key "+m[1])
		}

		return nil
	}

	m := lockStringRe.FindStringSubmatch(line)
	if m == nil {
		return ErrInvalidLock
	}
	p := cur.Platforms[platform]
	switch m[1] {
	case "url":
		p.URL = m[2]
	case "checksum":
		p.Checksum = m[2]
	case "url_api", "provenance":
		// 取得には使わない（url_api は locked のとき引かず、provenance は lock の生成のときの記録）。
	default:
		return xerrors.Wrap(ErrInvalidLock, "知らない key "+m[1])
	}
	cur.Platforms[platform] = p

	return nil
}

// ParseLockFile は path を読んで ParseLock します。
func ParseLockFile(path string) ([]LockEntry, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, xerrors.Wrap(err, path)
	}

	entries, err := ParseLock(data)
	if err != nil {
		return nil, xerrors.Wrap(err, path)
	}

	return entries, nil
}

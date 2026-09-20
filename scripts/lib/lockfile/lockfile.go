// Package lockfile は、pin の SSOT が使う `"key" = "value"` 形式の読み書きを持ちます。
//
// 書式は pin-actions と pin-images で同じで、違うのは値の形（commit SHA か digest か）と
// 見出しだけです。解釈できない行とキーの重複をエラーにするのは、読み飛ばしや後勝ちの
// 上書きが「そのエントリは検査されなかった」を静かに作るためです（ADR-0702 決定14）。
package lockfile

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

var (
	// ErrInvalidLine は、代入として解釈できない行があった場合のエラー。
	ErrInvalidLine = xerrors.New("lockfile に解釈できない行があります")
	// ErrDuplicateKey は、同一キーが複数回現れた場合のエラー。
	ErrDuplicateKey = xerrors.New("lockfile にキーの重複があります")
)

// Format は、1つの lockfile の書式です。
type Format struct {
	// Line は1行を key と value へ分解する正規表現。値の形はツールごとに違う。
	Line *regexp.Regexp
	// Header は書き出す先頭のコメント行。
	Header []string
	// Resolve は、直し方として案内する make target 名。
	Resolve string
	// Perm は書き出すファイルの permission。
	Perm fs.FileMode
}

// Read は lockfile を読みます。解釈できない行とキーの重複はエラーにします。
func (f Format) Read(path string) (map[string]string, error) {
	file, err := os.Open(path) //nolint:gosec // path は呼び出し側が cwd と固定名から組む
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	lock := map[string]string{}
	sc := bufio.NewScanner(file)
	for lineNo := 1; sc.Scan(); lineNo++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		m := f.Line.FindStringSubmatch(line)
		if m == nil {
			return nil, xerrors.Wrap(ErrInvalidLine,
				fmt.Sprintf("%d 行目: %q（make %s を実行するか該当行を削除してください）", lineNo, line, f.Resolve))
		}
		if _, dup := lock[m[1]]; dup {
			return nil, xerrors.Wrap(ErrDuplicateKey,
				fmt.Sprintf("%d 行目: %q（make %s を実行するか重複行を削除してください）", lineNo, m[1], f.Resolve))
		}
		lock[m[1]] = m[2]
	}

	return lock, sc.Err()
}

// Write は lockfile をキーの昇順で書き出します。
func (f Format) Write(path string, lock map[string]string) error {
	keys := make([]string, 0, len(lock))
	for k := range lock {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	for _, line := range f.Header {
		b.WriteString("# " + line + "\n")
	}
	for _, k := range keys {
		fmt.Fprintf(&b, "%q = %q\n", k, lock[k])
	}

	return os.WriteFile(path, []byte(b.String()), f.Perm)
}

// IsIgnorableErr は、Read のエラーのうち無視して続行してよいものを判定します。
// nil（成功）と「ファイル不在」（初回 resolve）のみ true。それ以外は fail-close 対象です。
func IsIgnorableErr(err error) bool {
	return err == nil || xerrors.Is(err, os.ErrNotExist)
}

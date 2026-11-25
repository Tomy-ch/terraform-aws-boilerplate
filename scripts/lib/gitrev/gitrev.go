// Package gitrev は、利用者が渡した rev を commit の完全な sha へ解決するときの取り決めを持ちます。rev は git の
// 引数ではなく git cat-file --batch-check（書式は `%(objectname) %(objecttype)`）の stdin で渡し、解決した sha だけを後段へ
// 渡します。**git は引数の `-` で始まる値を option として読み、範囲の起点が解決できない走査器は範囲を0件として
// 扱って通る**ので、検証と解決を道具ごとに書くと片方だけが緩みます。起動は各道具の継ぎ目が持ち、ここは stdin の
// 組み立てと出力の解釈だけを持ちます。
package gitrev

import (
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

var (
	// ErrEmpty は、rev が空であることです。
	ErrEmpty = xerrors.New("rev が空です")
	// ErrUnresolved は、rev が commit の sha へ解決できないことです。
	ErrUnresolved = xerrors.New("rev を commit へ解決できません")
)

// Query は、rev を commit へ剥く git cat-file --batch-check の stdin を返します。空は [ErrEmpty]、改行・CR・NUL を
// 含むか `-` で始まるものは、git を起動するまでもなく [ErrUnresolved] です。
func Query(rev string) (string, error) {
	if rev == "" {
		return "", ErrEmpty
	}
	if strings.ContainsAny(rev, "\n\r\x00") || strings.HasPrefix(rev, "-") {
		return "", ErrUnresolved
	}

	return rev + "^{commit}\n", nil
}

// Parse は、[Query] の stdin に対する git cat-file --batch-check（上の書式）の stdout から、commit の完全な sha を返します。
// 1件の commit でなければ（missing・tree・blob・複数行を含む）[ErrUnresolved] です。
func Parse(out string) (string, error) {
	fields := strings.Fields(out)
	if len(fields) != 2 || fields[1] != "commit" || !IsHash(fields[0]) {
		return "", ErrUnresolved
	}

	return fields[0], nil
}

// IsHash は s が SHA-1 か SHA-256 の object 名（小文字の16進）であるかを返します。
func IsHash(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}

	return true
}

package main

import (
	"path"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

var errGlob = xerrors.New("path の形を解釈できません")

// validateGlob は、宣言の path の形を検める。受け付けるのは `/` で区切った segment の列で、segment は
// path.Match の形か、segment 全体の `**` だけである。否定（`!`）と波括弧は受け付けない —— 意味の違う glob の
// 方言を黙って別の意味で読むと、走るべき検査を省く。
func validateGlob(p string) error {
	switch {
	case p == "":
		return xerrors.Wrap(errGlob, "空の path")
	case strings.HasPrefix(p, "/") || strings.HasSuffix(p, "/"):
		return xerrors.Wrapf(errGlob, "%q: 先頭と末尾に / を置きません", p)
	case strings.HasPrefix(p, "!"):
		return xerrors.Wrapf(errGlob, "%q: 否定は受け付けません", p)
	case strings.ContainsAny(p, "{}\\"):
		return xerrors.Wrapf(errGlob, "%q: 波括弧とバックスラッシュは受け付けません", p)
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" {
			return xerrors.Wrapf(errGlob, "%q: 空の segment があります", p)
		}
		if strings.Contains(seg, "**") && seg != "**" {
			return xerrors.Wrapf(errGlob, "%q: ** は segment 全体に置きます", p)
		}
		if _, err := path.Match(seg, ""); err != nil {
			return xerrors.Wrapf(errGlob, "%q: %v", p, err)
		}
	}
	return nil
}

// matchGlob は、file（リポジトリ直下からの `/` 区切りの path）が pattern に当たるかを返す。`**` は0個以上の
// segment に当たる。pattern は validateGlob を通っていること。
func matchGlob(pattern, file string) bool {
	return matchSegs(strings.Split(pattern, "/"), strings.Split(file, "/"))
}

func matchSegs(pat, segs []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			for i := 0; i <= len(segs); i++ {
				if matchSegs(pat[1:], segs[i:]) {
					return true
				}
			}
			return false
		}
		if len(segs) == 0 {
			return false
		}
		if ok, _ := path.Match(pat[0], segs[0]); !ok {
			return false
		}
		pat, segs = pat[1:], segs[1:]
	}
	return len(segs) == 0
}

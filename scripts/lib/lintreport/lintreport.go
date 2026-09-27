// Package lintreport は、workflow に対する lint が見つけた違反の持ち方と、失敗出力の組み立てを提供します。
//
// 複数の lint が同じ形で報告するのは、読む側が同じ CI ログだからです。整形を各入口へ複製すると、
// 片方だけ書式が動いたときに、出力を機械で拾っている側が黙って壊れます。
package lintreport

import (
	"sort"
	"strings"
)

// Finding は違反1件を表します。Line は1始まりで、その違反を直すために開く行です。
type Finding struct {
	File    string
	Line    int
	Message string
}

// Sort は違反をファイル名、次に行番号の昇順へ並べます。安定なので、同じ位置の違反は
// 渡された順に残ります。
//
// **検査ごとに違反を集めると、出力はファイル順ではなく検査順になります。** 読み手は
// 1つのファイルを直すために出力を行き来することになるので、報告の前にここを通します。
func Sort(findings []Finding) {
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].File != findings[j].File {
			return findings[i].File < findings[j].File
		}
		return findings[i].Line < findings[j].Line
	})
}

// Format は違反一覧をファイル単位にまとめた失敗出力へ整形します。
//
// ファイルの並びは渡された順のままにします。走査は選別済みのファイル順に回るため、ここで並べ替えると
// 「どこまで進んだか」が出力から読めなくなります。
func Format(findings []Finding) string {
	var lines []string
	current := ""
	first := true

	for _, f := range findings {
		if f.File != current {
			if !first {
				lines = append(lines, "")
			}
			lines = append(lines, "  "+f.File)
			current = f.File
			first = false
		}
		lines = append(lines, "    :"+itoa(f.Line)+"  "+f.Message)
	}

	return strings.Join(lines, "\n")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	// 符号を反転させずに桁を作る。**math.MinInt は反転しても正にならない** ——
	// `n = -n` を前提にすると桁が1つも作られず、"-" だけが返る。
	var b [20]byte
	i := len(b)
	for n != 0 {
		d := n % 10
		if d < 0 {
			d = -d
		}
		i--
		b[i] = byte('0' + d)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

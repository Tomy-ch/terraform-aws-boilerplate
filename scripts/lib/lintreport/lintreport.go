// Package lintreport は、workflow に対する lint が見つけた違反の持ち方と、失敗出力の組み立てを提供します。
//
// 複数の lint が同じ形で報告するのは、読む側が同じ CI ログだからです。整形を各入口へ複製すると、
// 片方だけ書式が動いたときに、出力を機械で拾っている側が黙って壊れます。
package lintreport

import "strings"

// Finding は違反1件を表します。Line は1始まりで、その違反を直すために開く行です。
type Finding struct {
	File    string
	Line    int
	Message string
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
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

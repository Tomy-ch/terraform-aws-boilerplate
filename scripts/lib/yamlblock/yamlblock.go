// Package yamlblock は YAML のブロックスカラー（`key: |` / `key: >-`）の中身の判定を 1 箇所へ集める。
//
// 呼び出し側は workflow を行単位で走査する。ブロックスカラーの中身を外さなければ、`run:`
// スクリプトが走査対象のキー（`uses:` など）を含む文字列を出力するだけで検出が誤爆する。
//
// 呼び出し側の一部だけが除外すると、同じ workflow が経路によって違う判定を受ける。
package yamlblock

import (
	"regexp"
	"strings"
)

// headerRe は値がブロックスカラー（`|` / `>`）で始まる行。
// 字下げ指示子と chomp 指示子は YAML がどちらの順序も許すため（`|2-` / `|-2`）両方を受ける。
// 注記は ContentLines が判定の前に落とすので、ここでは受けない。
var headerRe = regexp.MustCompile(`:[ \t]*[|>][+-]?\d?[+-]?[ \t]*$`)

// ContentLines は data のうちブロックスカラーの中身に当たる行番号（1 始まり）を返す。
//
// 中身の範囲は字下げで決まる。ヘッダ行より深い字下げの行と、その途中の空行が中身で、
// 字下げがヘッダ以下へ戻った行で終わる。ヘッダ行そのものは中身に含めない。
func ContentLines(data string) map[int]bool {
	content := map[int]bool{}
	headerIndent := -1

	for i, line := range strings.Split(data, "\n") {
		indent := len(line) - len(strings.TrimLeft(line, " \t"))
		if headerIndent >= 0 {
			if strings.TrimSpace(line) == "" || indent > headerIndent {
				content[i+1] = true
				continue
			}
			headerIndent = -1
		}
		// **注記を落としてからヘッダを判定する** —— 注記の中の `: |` は headerRe に一致する。
		// YAML の注記は行頭か空白の直後の `#` から始まる。引用符の中の `#` まで落とすが、
		// その向きは「ヘッダと見なさない」側 —— 走査対象が増える方 —— なので安全側である。
		head := line
		for k := range len(head) {
			if head[k] == '#' && (k == 0 || head[k-1] == ' ' || head[k-1] == '\t') {
				head = head[:k]
				break
			}
		}
		if headerRe.MatchString(head) {
			headerIndent = indent
		}
	}

	return content
}

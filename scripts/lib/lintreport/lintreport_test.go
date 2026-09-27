package lintreport_test

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/lintreport"
)

func Test_Format(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		findings []lintreport.Finding
		want     string
	}{
		"1件": {
			findings: []lintreport.Finding{{File: "a.yaml", Line: 3, Message: "だめ"}},
			want:     "  a.yaml\n    :3  だめ",
		},
		"同じファイルはまとめる": {
			findings: []lintreport.Finding{
				{File: "a.yaml", Line: 3, Message: "一"},
				{File: "a.yaml", Line: 9, Message: "二"},
			},
			want: "  a.yaml\n    :3  一\n    :9  二",
		},
		"ファイルが変わると空行を挟む": {
			findings: []lintreport.Finding{
				{File: "a.yaml", Line: 1, Message: "一"},
				{File: "b.yaml", Line: 2, Message: "二"},
			},
			want: "  a.yaml\n    :1  一\n\n  b.yaml\n    :2  二",
		},
		"渡された順を保つ": {
			// 走査は選別済みのファイル順に回る。ここで並べ替えると「どこまで進んだか」が読めなくなる。
			findings: []lintreport.Finding{
				{File: "z.yaml", Line: 1, Message: "一"},
				{File: "a.yaml", Line: 1, Message: "二"},
			},
			want: "  z.yaml\n    :1  一\n\n  a.yaml\n    :1  二",
		},
		"0件": {findings: nil, want: ""},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, lintreport.Format(tt.findings))
		})
	}
}

func TestSort(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("ファイル名で並べ、同じファイルなら行番号で並べる", func(t *testing.T) {
			t.Parallel()
			// 同じ File と Line を持つ2件は、渡された順のまま残る。検出の順序は
			// 走査の進み方そのものであり、並べ替えで失わせない。
			findings := []lintreport.Finding{
				{File: "b.md", Line: 1, Message: "b1"},
				{File: "a.md", Line: 10, Message: "a10 先"},
				{File: "a.md", Line: 2, Message: "a2"},
				{File: "a.md", Line: 10, Message: "a10 後"},
			}
			lintreport.Sort(findings)
			assert.Equal(t, []lintreport.Finding{
				{File: "a.md", Line: 2, Message: "a2"},
				{File: "a.md", Line: 10, Message: "a10 先"},
				{File: "a.md", Line: 10, Message: "a10 後"},
				{File: "b.md", Line: 1, Message: "b1"},
			}, findings)
		})

		t.Run("同じ位置の違反は渡された順のまま残る", func(t *testing.T) {
			t.Parallel()
			// **要素が少ない入力や、全件が同じ位置の入力では安定性の喪失が現れない。**
			// 前者は挿入ソートへ落ち、後者は整列済みとして扱われる。2つのファイルを
			// 交互に並べて実際に要素を動かさせたうえで、各ファイルの中の順序を見る。
			const n = 40
			var findings []lintreport.Finding
			var wantA, wantB []lintreport.Finding
			for i := range n {
				f := lintreport.Finding{File: "b.md", Line: 1, Message: strconv.Itoa(i)}
				if i%2 == 1 {
					f.File = "a.md"
					wantA = append(wantA, f)
				} else {
					wantB = append(wantB, f)
				}
				findings = append(findings, f)
			}
			lintreport.Sort(findings)
			assert.Equal(t, append(wantA, wantB...), findings)
		})

		t.Run("違反が0件でも1件でも壊れない", func(t *testing.T) {
			t.Parallel()
			lintreport.Sort(nil)
			one := []lintreport.Finding{{File: "a.md", Line: 3, Message: "x"}}
			lintreport.Sort(one)
			assert.Equal(t, []lintreport.Finding{{File: "a.md", Line: 3, Message: "x"}}, one)
		})
	})
}

package lintreport_test

import (
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

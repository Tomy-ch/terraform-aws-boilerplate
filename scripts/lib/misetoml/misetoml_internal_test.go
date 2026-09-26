package misetoml

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_sectionName(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			raw  string
			want string
		}{
			"装飾が無い場合、そのまま返す": {raw: "tools", want: "tools"},

			// 正規化を落とすと、これらが「[tools] ではない節」になり、中身が丸ごと
			// 読み飛ばされたうえでエラーも出ません。
			"前後に空白がある場合、空白を落とす":        {raw: " tools ", want: "tools"},
			"二重引用符付きの場合、引用符を外す":        {raw: `"tools"`, want: "tools"},
			"単引用符付きの場合、引用符を外す":         {raw: `'tools'`, want: "tools"},
			"引用符の外側に空白がある場合、両方を落とす":    {raw: ` "tools" `, want: "tools"},
			"引用符の内側の空白は、名前の一部として残す":    {raw: `" tools "`, want: " tools "},
			"dotted key の場合、別の節のままにする": {raw: "tools.go", want: "tools.go"},
			"閉じていない引用符の場合、外さない":        {raw: `"tools`, want: `"tools`},

			// 引用符を外す条件は `len(name) >= 2` です。両側を固定しないと、
			// 1文字の引用符で添字が負へ回る形を見落とします。
			"引用符2つだけの場合、空の名前になる": {raw: `""`, want: ""},
			"引用符1つだけの場合、外さない":    {raw: `"`, want: `"`},

			"空文字の場合、空文字を返す":  {raw: "", want: ""},
			"空白だけの場合、空文字を返す": {raw: "   ", want: ""},
		}

		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				assert.Equal(t, tt.want, sectionName(tt.raw))
			})
		}
	})
}

func Test_parseLine(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		tests := map[string]struct {
			line        string
			wantKey     string
			wantVersion string
		}{
			"裸キーの場合、キーと版を返す":       {line: `go = "1.27.1"`, wantKey: "go", wantVersion: "1.27.1"},
			"数字始まりの裸キーの場合、キーと版を返す": {line: `1password = "2.0.0"`, wantKey: "1password", wantVersion: "2.0.0"},
			"引用符付きキーの場合、引用符を外して返す": {
				line: `"aqua:owner/repo" = "1.0.0"`, wantKey: "aqua:owner/repo", wantVersion: "1.0.0",
			},
			"等号の周りに空白が無い場合、キーと版を返す": {line: `go="1.27.1"`, wantKey: "go", wantVersion: "1.27.1"},
			"行末コメントがある場合、コメントを無視する": {
				line: `go = "1.27.1" # 理由`, wantKey: "go", wantVersion: "1.27.1",
			},
			"tool option 付きの場合、version を取り出す": {
				line: `node = { version = "24.21.0", postinstall = "x" }`, wantKey: "node", wantVersion: "24.21.0",
			},
			"tool option が version より前にある場合も、version を取り出す": {
				line: `node = { postinstall = "x", version = "24.21.0" }`, wantKey: "node", wantVersion: "24.21.0",
			},
			"引用符付きキーの tool option の場合、引用符を外して返す": {
				line: `"aqua:owner/repo" = { version = "1.0.0" }`, wantKey: "aqua:owner/repo", wantVersion: "1.0.0",
			},
		}

		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				key, version, ok := parseLine(tt.line)

				assert.True(t, ok)
				assert.Equal(t, tt.wantKey, key)
				assert.Equal(t, tt.wantVersion, version)
			})
		}
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		// ここで true を返した行は、呼び出し側でエラーにならずに通ります。
		// 「解釈できない行を読み飛ばさない」規律は、この false に乗っています。
		tests := map[string]string{
			"配列で複数版を並べている場合、ok が false になる":                `go = ["1.27.1", "1.26.0"]`,
			"tool option が version を持たない場合、ok が false になる": `node = { postinstall = "x" }`,
			"版が引用符で囲まれていない場合、ok が false になる":               `go = 1.27.1`,
			"版が単引用符で囲まれている場合、ok が false になる":               `go = '1.27.1'`,
			"版が空の場合、ok が false になる":                        `go = ""`,
			"キーが空の場合、ok が false になる":                       `"" = "1.27.1"`,
			"裸キーに使えない文字を引用符なしで含む場合、ok が false になる":         `aqua:owner/repo = "1.0.0"`,
			"等号が無い場合、ok が false になる":                       `go "1.27.1"`,
			"節の見出しの場合、ok が false になる":                      `[tools]`,
			"コメント行の場合、ok が false になる":                      `# go = "1.27.1"`,
			"空行の場合、ok が false になる":                         ``,
		}

		for name, line := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				key, version, ok := parseLine(line)

				assert.False(t, ok)
				assert.Empty(t, key)
				assert.Empty(t, version)
			})
		}
	})
}

package adrdoc

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

func TestDir(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		scope string
		want  string
	}{
		"root scope は docs/adr":          {scope: RootScope, want: "docs/adr"},
		"ユースケースは usecases/<名前>/docs/adr": {scope: "state-backend", want: filepath.Join("usecases", "state-backend", "docs", "adr")},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, Dir(tt.scope))
		})
	}
}

func TestFind(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		repo := t.TempDir()
		writeFile(t, filepath.Join(repo, "docs/adr/0103-infrastructure-code-gate.md"), "x")
		writeFile(t, filepath.Join(repo, "usecases/x/docs/adr/0001-fn-runtime.md"), "x")

		tests := map[string]struct {
			id   string
			want string
		}{
			"root scope の slug を引く":    {id: "repository-wide/infrastructure-code-gate", want: filepath.Join("docs", "adr", "0103-infrastructure-code-gate.md")},
			"ユースケースの scope の slug を引く": {id: "x/fn-runtime", want: filepath.Join("usecases", "x", "docs", "adr", "0001-fn-runtime.md")},
		}
		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				got, err := Find(repo, tt.id)
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			})
		}
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		repo := t.TempDir()
		writeFile(t, filepath.Join(repo, "docs/adr/0103-infrastructure-code-gate.md"), "x")
		writeFile(t, filepath.Join(repo, "docs/adr/0104-infrastructure-code-gate.md"), "x")
		writeFile(t, filepath.Join(repo, "docs/adr/0200-Upper.md"), "x")
		require.NoError(t, os.MkdirAll(filepath.Join(repo, "docs/adr/0300-dir-named.md"), 0o750))

		tests := map[string]struct {
			id   string
			want error
		}{
			"番号での参照は identity ではない":          {id: "repository-wide/0103", want: ErrNotFound},
			"ADR-NNNN の形は identity ではない":     {id: "ADR-0103", want: ErrIdentity},
			"scope の無い slug は identity ではない": {id: "infrastructure-code-gate", want: ErrIdentity},
			"大文字を含む identity は弾く":            {id: "repository-wide/Upper", want: ErrIdentity},
			"slug の当たる ADR が無い":              {id: "repository-wide/no-such", want: ErrNotFound},
			"置き場所の無い scope":                  {id: "no-such/fn", want: ErrNotFound},
			"同じ slug が2つあれば決めない":             {id: "repository-wide/infrastructure-code-gate", want: ErrAmbiguous},
			"slug に当たる名前のディレクトリは数えない":        {id: "repository-wide/dir-named", want: ErrNotFound},
		}
		for name, tt := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				_, err := Find(repo, tt.id)
				require.ErrorIs(t, err, tt.want)
			})
		}
	})
}

func TestStatus(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		source string
		want   string
		ok     bool
	}{
		"Status 行の値を返す":          {source: "# ADR\n\n- Status: Accepted\n- Date: x\n", want: "Accepted", ok: true},
		"値の後ろの空白を落とす":            {source: "- Status: Proposed  \n", want: "Proposed", ok: true},
		"Status 行が無ければ ok=false": {source: "# ADR\n\n- Date: x\n", ok: false},
		"値の無い Status 行は無いものとする":  {source: "- Status:\n", ok: false},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, ok := Status(tt.source)
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestHeadings(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		source string
		want   []string
	}{
		"レベルを問わず見出しのテキストを返す": {
			source: "# A\n\n## B\n\n#### C\n",
			want:   []string{"A", "B", "C"},
		},
		"閉じの # と前後の空白を落とす": {
			source: "##   入出力  ##\n",
			want:   []string{"入出力"},
		},
		"フェンスの中の見出しは返さない": {
			source: "```\n## 入出力\n```\n## 失敗時の扱い\n",
			want:   []string{"失敗時の扱い"},
		},
		"HTML コメントの中の見出しは返さない": {
			source: "<!--\n## 入出力\n-->\n## 失敗時の扱い\n<!-- ## 一行のコメント -->\n",
			want:   []string{"失敗時の扱い"},
		},
		"同じ行で閉じたコメントの後の、閉じないコメントも見る": {
			source: "<!-- a --> <!-- b\n## 入出力\n-->\n## 失敗時の扱い\n",
			want:   []string{"失敗時の扱い"},
		},
		"inline code の中の <!-- はコメントを開かない": {
			source: "`<!--` は使わない\n## 入出力\n",
			want:   []string{"入出力"},
		},
		"# の後に空白の無い行は見出しではない": {
			source: "#入出力\n",
			want:   nil,
		},
		"7つ以上の # は見出しではない": {
			source: "####### 入出力\n",
			want:   nil,
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, Headings([]byte(tt.source)))
		})
	}
}

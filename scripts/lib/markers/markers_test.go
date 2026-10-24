package markers_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/markers"
)

func TestNew(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("渡した名前空間の印だけを数える", func(t *testing.T) {
			t.Parallel()
			n, err := markers.New("sample").Count("# sample:line\n# other:line\n")
			require.NoError(t, err)
			assert.Equal(t, 1, n)
		})

		// 名前空間を正規表現として読むと、`.` が任意の1文字に当たり別の名前空間の印を拾う。
		t.Run("名前空間の記号を文字どおりに読む", func(t *testing.T) {
			t.Parallel()
			n, err := markers.New("a.b").Count("# axb:line\n")
			require.NoError(t, err)
			assert.Equal(t, 0, n)
		})
	})
}

func TestMarkers_Count(t *testing.T) {
	t.Parallel()
	m := markers.New("sample")

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("3つのコメント記法の印を数える", func(t *testing.T) {
			t.Parallel()
			n, err := m.Count("// sample:begin\n# sample:end\n<!-- sample:line -->\n")
			require.NoError(t, err)
			assert.Equal(t, 3, n)
		})

		t.Run("名前空間を含まない行は数えない", func(t *testing.T) {
			t.Parallel()
			n, err := m.Count("a\n# 普通のコメント\n")
			require.NoError(t, err)
			assert.Equal(t, 0, n)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		// コメントの外に置いた印は取り除かれないので、綴りの揺れとして落とす。
		t.Run("コメントの外の印はエラーにする", func(t *testing.T) {
			t.Parallel()
			_, err := m.Count("sample:line\n")
			require.ErrorIs(t, err, markers.ErrUnknownMarker)
		})

		t.Run("解釈できない印はエラーにして0を返す", func(t *testing.T) {
			t.Parallel()
			n, err := m.Count("ok\n# sample:lines\n")
			assert.Zero(t, n)
			require.ErrorIs(t, err, markers.ErrUnknownMarker)
			assert.Contains(t, err.Error(), "2 行目")
		})
	})
}

func TestMarkers_Strip(t *testing.T) {
	t.Parallel()
	m := markers.New("sample")

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name string
			in   string
			want string
		}{
			{
				name: "begin から end までを印の行ごと消す",
				in:   "a\n# sample:begin\nb\n# sample:end\nc\n",
				want: "a\nc\n",
			},
			{
				name: "入れ子の begin を深さで数える",
				in:   "a\n# sample:begin\n# sample:begin\nb\n# sample:end\nc\n# sample:end\nd\n",
				want: "a\nd\n",
			},
			{
				name: "line の印がある行を消す",
				in:   "a\nb <!-- sample:line -->\nc\n",
				want: "a\nc\n",
			},
			{
				name: "replace-with の後ろの行を字下げを残して戻す",
				in:   "a\n  # sample:replace-begin\n  old: 1\n  # sample:replace-with\n  # = new: 2\n  # sample:replace-end\nb\n",
				want: "a\n  new: 2\nb\n",
			},
			{
				name: "Markdown の replace-with の行を戻す",
				in:   "<!-- sample:replace-begin -->\n古い\n<!-- sample:replace-with -->\n<!-- = 新しい -->\n<!-- sample:replace-end -->\n",
				want: "新しい\n",
			},
			{
				name: "replace-with の無い replace は区間を消すだけにする",
				in:   "a\n// sample:replace-begin\nb\n// sample:replace-end\nc",
				want: "a\nc",
			},
			{
				name: "begin の中の replace は戻さずに消す",
				in:   "a\n# sample:begin\n# sample:replace-begin\n# sample:replace-with\n# = x\n# sample:replace-end\n# sample:end\nb",
				want: "a\nb",
			},
			{
				name: "取り除いた跡に並ぶ空行を1つにする",
				in:   "a\n\n# sample:begin\nb\n# sample:end\n\nc",
				want: "a\n\nc",
			},
			{
				name: "取り除いていない場所の連続した空行は残す",
				in:   "a\n\n\nb\n# sample:line\nc",
				want: "a\n\n\nb\nc",
			},
			{
				name: "CRLF のファイルの行末を保つ",
				in:   "a\r\n<!-- sample:replace-begin -->\r\nb\r\n<!-- sample:replace-with -->\r\n<!-- = c -->\r\n<!-- sample:replace-end -->\r\n",
				want: "a\r\nc\r\n",
			},
			{
				name: "空の内容は空のまま返す",
				in:   "",
				want: "",
			},
			{
				name: "印が無ければ内容を変えない",
				in:   "a\nb\n",
				want: "a\nb\n",
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				got, err := m.Strip(tt.in)
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			})
		}
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name    string
			in      string
			wantErr error
			wantMsg string
		}{
			{name: "閉じていない begin はエラーにする", in: "a\n# sample:begin\nb", wantErr: markers.ErrUnbalanced, wantMsg: "2 行目"},
			{name: "begin の無い end はエラーにする", in: "# sample:end", wantErr: markers.ErrUnbalanced, wantMsg: "1 行目"},
			{name: "replace-begin の無い replace-with はエラーにする", in: "# sample:replace-with", wantErr: markers.ErrUnbalanced},
			{name: "replace-begin の無い replace-end はエラーにする", in: "# sample:replace-end", wantErr: markers.ErrUnbalanced},
			{name: "閉じていない replace-begin はエラーにする", in: "# sample:replace-begin\na", wantErr: markers.ErrUnbalanced, wantMsg: "1 行目"},
			{name: "入れ子の replace-begin はエラーにする", in: "# sample:replace-begin\n# sample:replace-begin", wantErr: markers.ErrNestedReplace},
			{name: "replace-with が2回あればエラーにする", in: "# sample:replace-begin\n# sample:replace-with\n# sample:replace-with", wantErr: markers.ErrUnbalanced},
			{name: "replace の区間の中の begin はエラーにする", in: "# sample:replace-begin\n# sample:begin", wantErr: markers.ErrUnbalanced},
			{name: "begin の中の = の形でない戻す行もエラーにする", in: "# sample:begin\n# sample:replace-begin\n# sample:replace-with\nraw\n# sample:replace-end\n# sample:end", wantErr: markers.ErrBadReplacement},
			{name: "replace の区間の中の line はエラーにする", in: "# sample:replace-begin\n# sample:line", wantErr: markers.ErrUnbalanced},
			{name: "= の形でない戻す行はエラーにする", in: "# sample:replace-begin\n# sample:replace-with\nraw\n# sample:replace-end", wantErr: markers.ErrBadReplacement, wantMsg: "3 行目"},
			{name: "解釈できない印はエラーにする", in: "# sample:bgin", wantErr: markers.ErrUnknownMarker},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				got, err := m.Strip(tt.in)
				require.ErrorIs(t, err, tt.wantErr)
				assert.Empty(t, got)
				if tt.wantMsg != "" {
					assert.Contains(t, err.Error(), tt.wantMsg)
				}
			})
		}
	})
}

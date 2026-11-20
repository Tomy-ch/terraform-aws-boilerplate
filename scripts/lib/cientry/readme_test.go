package cientry

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const readmeHead = "| 役割 | role 名 | GitHub の environment | 使う場面 |\n| --- | --- | --- | --- |\n"

func readmeRow(role string) string {
	return "| " + role + " | `<project>-<env>-ci-" + role + "` | `<env>-" + role + "` | x |\n"
}

func readmeSrc(rows ...string) string {
	return "# x\n\n| 役割 | 説明 |\n| --- | --- |\n| plan | y |\n\n" + readmeHead + strings.Join(rows, "") + "\n本文\n"
}

func Test_parseReadmeRoles(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("見出しの先頭の2つの列で表を選び、役割の列を書かれた順で読む", func(t *testing.T) {
			t.Parallel()
			got, err := parseReadmeRoles([]byte(readmeSrc(readmeRow("unlock"), readmeRow("apply-identity"))))
			require.NoError(t, err)
			assert.Equal(t, []string{"unlock", "apply-identity"}, got)
		})

		t.Run("ファイルの末尾で終わる表と、行末の空白を読む", func(t *testing.T) {
			t.Parallel()
			src := readmeHead + "| apply | a | b | c |  \n| plan | a | b | c |"
			got, err := parseReadmeRoles([]byte(src))
			require.NoError(t, err)
			assert.Equal(t, []string{"apply", "plan"}, got)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		cases := []struct {
			name, src, reason string
		}{
			{"表が無い", "# x\n\n| 役割 | 説明 |\n| --- | --- |\n| plan | y |\n", "見出しの行が 0 行です"},
			{"表が2つ", readmeSrc(readmeRow("plan")) + "\n" + readmeHead + readmeRow("apply"), "見出しの行が 2 行です"},
			{"見出しが | で終わらない", "| 役割 | role 名 | x\n| --- | --- |\n| plan | a |\n", "1 行目: 見出しの行が | で終わりません"},
			{"見出しの次が区切りでない", "| 役割 | role 名 |\n| plan | a |\n", "2 行目: 見出しの次が区切りの行ではありません"},
			{"見出しがファイルの末尾", "| 役割 | role 名 |", "2 行目: 見出しの次が区切りの行ではありません"},
			{"行が0行", readmeSrc(), "7 行目: role の表が0行です"},
			{"行が | で終わらない", readmeSrc("| plan | a | b | c\n"), "9 行目: | で終わらないか、列の数が見出しの 4 個と違います"},
			{"行の列が足りない", readmeSrc("| plan | a | b |\n"), "列の数が見出しの 4 個と違います"},
			{"エスケープした | を含む行（分割で列が増える）", readmeSrc("| plan | a \\| b | c | d |\n"), "列の数が見出しの 4 個と違います"},
			{"区切りの列の数が見出しと違う", "| 役割 | role 名 | c |\n| --- |\n| plan | a | b |\n", "2 行目: 区切りの列 1 個が見出しの 3 個と違います"},
			{"行末が CRLF", strings.ReplaceAll(readmeSrc(readmeRow("plan")), "\n", "\r\n"), "見出しの行が | で終わりません"},
			{"フェンスの中の表だけが在る", "```\n" + readmeHead + readmeRow("plan") + "\n```\n", "見出しの行が 0 行です"},
			{"役割の列が role の形でない", readmeSrc("| `plan` | a | b | c |\n"), "9 行目: role \"`plan`\" を読めません"},
			{"役割の列が空", readmeSrc("|  | a | b | c |\n"), `role "" を読めません`},
			{"役割が重複", readmeSrc(readmeRow("plan"), readmeRow("plan")), "10 行目: role plan が重複しています"},
			{"| で始まらない行が表の途中に在る", readmeSrc(readmeRow("plan"), " "+readmeRow("apply"), readmeRow("unlock")), "区切りの行から空行までの行 3 個 / 読めた role 1 個"},
			{"表の直後に空行を置かずに本文が続く", readmeHead + readmeRow("plan") + "本文\n", "区切りの行から空行までの行 2 個 / 読めた role 1 個"},
		}
		for _, c := range cases {
			t.Run(c.name+"なら弾く", func(t *testing.T) {
				t.Parallel()
				_, err := parseReadmeRoles([]byte(c.src))
				require.ErrorIs(t, err, ErrReadmeParse)
				assert.Contains(t, err.Error(), c.reason)
			})
		}

		t.Run("空のソースは表が無いとして弾く", func(t *testing.T) {
			t.Parallel()
			_, err := parseReadmeRoles(nil)
			require.ErrorIs(t, err, ErrReadmeParse)
			assert.Contains(t, err.Error(), "見出しの行が 0 行です")
		})
	})
}

func Test_registry_checkReadmeRoles(t *testing.T) {
	t.Parallel()

	r := registry{
		{Name: "a", Lock: LockSkip, IdentityRole: "a", WorkloadRole: "a"},
		{Name: "b", Lock: LockRelease, IdentityRole: "b-identity", WorkloadRole: "b"},
	}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("lock を取らない種別の role も含めて、順を問わず集合が一致すれば成功する", func(t *testing.T) {
			t.Parallel()
			assert.NoError(t, r.checkReadmeRoles([]string{"b", "a", "b-identity"}))
		})

		// 写しと正本のずれを検出する検査は、正本の実物を読んでよい（ADR-0702 決定17）。
		t.Run("写しと正本のずれ", func(t *testing.T) {
			t.Parallel()

			t.Run(ReadmeRolesFile+" の role の表が登録簿の role と一致する", func(t *testing.T) {
				t.Parallel()
				got, err := readRoles(repoRoot, ReadmeRolesFile, parseReadmeRoles)
				require.NoError(t, err)
				assert.NoError(t, std.checkReadmeRoles(got))
			})
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("登録簿の role が無ければ、その role を登録簿の側として名指して弾く", func(t *testing.T) {
			t.Parallel()
			err := r.checkReadmeRoles([]string{"a", "b"})
			require.ErrorIs(t, err, ErrReadmeRole)
			assert.Contains(t, err.Error(), "登録簿の role b-identity が README の role の表にありません")
			assert.NotContains(t, err.Error(), "は登録簿にありません")
		})

		t.Run("登録簿に無い role があれば、その role を表の側として名指して弾く", func(t *testing.T) {
			t.Parallel()
			err := r.checkReadmeRoles([]string{"a", "b", "b-identity", "c"})
			require.ErrorIs(t, err, ErrReadmeRole)
			assert.Contains(t, err.Error(), "README の role の表の role c は登録簿にありません")
			assert.NotContains(t, err.Error(), "が README の role の表にありません")
		})

		t.Run("欠けた role と余分な role を両方名指す", func(t *testing.T) {
			t.Parallel()
			err := r.checkReadmeRoles([]string{"a", "b", "c"})
			require.ErrorIs(t, err, ErrReadmeRole)
			assert.Contains(t, err.Error(), "登録簿の role b-identity が README の role の表にありません")
			assert.Contains(t, err.Error(), "README の role の表の role c は登録簿にありません")
		})

		t.Run("表の role が0件なら弾く", func(t *testing.T) {
			t.Parallel()
			err := r.checkReadmeRoles(nil)
			require.ErrorIs(t, err, ErrReadmeRole)
			assert.Contains(t, err.Error(), "README の role の表が0行です")
		})

		t.Run("登録簿に種別が無ければ、表を問わず弾く", func(t *testing.T) {
			t.Parallel()
			require.ErrorIs(t, registry{}.checkReadmeRoles(nil), errRegistry)
			require.ErrorIs(t, registry{}.checkReadmeRoles([]string{"a"}), errRegistry)
		})
	})
}

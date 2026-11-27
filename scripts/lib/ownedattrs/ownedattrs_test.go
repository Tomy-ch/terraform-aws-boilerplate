package ownedattrs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const header = "    | resource type | 属性 | 発火側が行う操作 |\n    | --- | --- | --- |\n"

func doc(rows ...string) string {
	return "## Decision\n\n20. 次の一覧に限る。\n\n" + header + strings.Join(rows, "") + "\n    後の段落。\n"
}

const (
	rowECS = "    | `aws_ecs_service` | `task_definition` | 登録と更新 |\n"
	rowOrg = "    | `aws_organizations_organization` | `aws_service_access_principals` | 人の操作 |\n"
)

func TestParse(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("表の行を書かれた順に resource type と属性の組で返す", func(t *testing.T) {
			t.Parallel()
			got, err := Parse([]byte(doc(rowECS, rowOrg)))
			require.NoError(t, err)
			assert.Equal(t, []Pair{
				{Type: "aws_ecs_service", Attr: "task_definition"},
				{Type: "aws_organizations_organization", Attr: "aws_service_access_principals"},
			}, got)
		})

		t.Run("字下げの無い表も読む", func(t *testing.T) {
			t.Parallel()
			in := strings.ReplaceAll(doc(rowECS), "    |", "|")
			got, err := Parse([]byte(in))
			require.NoError(t, err)
			assert.Equal(t, []Pair{{Type: "aws_ecs_service", Attr: "task_definition"}}, got)
		})

		t.Run("コードフェンスの中の同じ見出しの表は数えない", func(t *testing.T) {
			t.Parallel()
			fenced := "```markdown\n" + header + rowOrg + "```\n\n"
			got, err := Parse([]byte(fenced + doc(rowECS)))
			require.NoError(t, err)
			assert.Equal(t, []Pair{{Type: "aws_ecs_service", Attr: "task_definition"}}, got)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for name, c := range map[string]struct {
			in  string
			err error
		}{
			"表が無い":                            {"## Decision\n\n20. 一覧。\n", ErrNoTable},
			"本文が空":                            {"", ErrNoTable},
			"表が2つある":                          {doc(rowECS) + "\n" + doc(rowOrg), ErrNoTable},
			"表が0行":                            {doc(), ErrEmpty},
			"区切りの行が無い":                        {"    | resource type | 属性 | 発火側が行う操作 |\n" + rowECS, ErrMalformed},
			"列が2つしかない":                        {doc("    | `aws_ecs_service` | `task_definition` |\n"), ErrMalformed},
			"resource type が inline code でない": {doc("    | aws_ecs_service | `task_definition` | x |\n"), ErrMalformed},
			"属性が inline code でない":             {doc("    | `aws_ecs_service` | task_definition | x |\n"), ErrMalformed},
			"属性が all":                         {doc("    | `aws_ecs_service` | `all` | x |\n"), ErrMalformed},
			"属性が2つの code を持つ":                 {doc("    | `aws_ecs_service` | `a` `b` | x |\n"), ErrMalformed},
			"resource type の形が違う":             {doc("    | `aws-ecs` | `task_definition` | x |\n"), ErrMalformed},
			"属性の形が違う":                         {doc("    | `aws_ecs_service` | `task.definition` | x |\n"), ErrMalformed},
			"組が重複する":                          {doc(rowECS, rowECS), ErrMalformed},
			"HTML コメントが閉じない":                  {"<!--\n" + doc(rowECS), ErrMalformed},
			"表の途中で行が | で始まらない":                {doc(rowECS, "    `aws_x` | `y` | z |\n", rowOrg), ErrMalformed},
			"見出しの列名が違う":                       {strings.Replace(doc(rowECS), "| 属性 |", "| 属性名 |", 1), ErrNoTable},
			"見出しに列が足されている":                    {strings.Replace(doc(rowECS), "| 発火側が行う操作 |", "| 発火側が行う操作 | 備考 |", 1), ErrNoTable},
		} {
			t.Run(name+"なら失敗する", func(t *testing.T) {
				t.Parallel()
				got, err := Parse([]byte(c.in))
				require.ErrorIs(t, err, c.err)
				assert.Nil(t, got)
			})
		}
	})
}

func TestRead(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("リポジトリの File を読む", func(t *testing.T) {
			t.Parallel()
			repo := t.TempDir()
			require.NoError(t, os.MkdirAll(filepath.Join(repo, filepath.Dir(File)), 0o700))
			require.NoError(t, os.WriteFile(filepath.Join(repo, File), []byte(doc(rowOrg)), 0o600))
			got, err := Read(repo)
			require.NoError(t, err)
			assert.Equal(t, []Pair{{Type: "aws_organizations_organization", Attr: "aws_service_access_principals"}}, got)
		})

		t.Run("実物の ADR-0601 の表を機構が読め、すべての組が resource type と属性の名前の形を持つ", func(t *testing.T) {
			t.Parallel()
			got, err := Read(filepath.Join("..", "..", ".."))
			require.NoError(t, err)
			require.NotEmpty(t, got)
			for _, p := range got {
				assert.Regexp(t, typeName, p.Type)
				assert.Regexp(t, attrName, p.Attr)
				assert.NotEqual(t, "all", p.Attr)
			}
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("File が無ければ失敗する", func(t *testing.T) {
			t.Parallel()
			got, err := Read(t.TempDir())
			require.ErrorIs(t, err, ErrRead)
			assert.Nil(t, got)
		})

		t.Run("リポジトリが無ければ失敗する", func(t *testing.T) {
			t.Parallel()
			got, err := Read(filepath.Join(t.TempDir(), "missing"))
			require.ErrorIs(t, err, ErrRead)
			assert.Nil(t, got)
		})

		t.Run("File の表が読めなければその理由で失敗する", func(t *testing.T) {
			t.Parallel()
			repo := t.TempDir()
			require.NoError(t, os.MkdirAll(filepath.Join(repo, filepath.Dir(File)), 0o700))
			require.NoError(t, os.WriteFile(filepath.Join(repo, File), []byte(doc()), 0o600))
			_, err := Read(repo)
			require.ErrorIs(t, err, ErrEmpty)
		})
	})
}

func TestSet_Has(t *testing.T) {
	t.Parallel()

	s := NewSet([]Pair{{Type: "aws_ecs_service", Attr: "task_definition"}})

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("一覧の組は持つ", func(t *testing.T) {
			t.Parallel()
			assert.True(t, s.Has("aws_ecs_service", "task_definition"))
		})

		t.Run("別の type の同名の属性は持たない", func(t *testing.T) {
			t.Parallel()
			assert.False(t, s.Has("aws_ecs_task_set", "task_definition"))
		})

		t.Run("一覧の type の別の属性は持たない", func(t *testing.T) {
			t.Parallel()
			assert.False(t, s.Has("aws_ecs_service", "desired_count"))
		})

		t.Run("空の集合は何も持たない", func(t *testing.T) {
			t.Parallel()
			assert.False(t, NewSet(nil).Has("aws_ecs_service", "task_definition"))
		})
	})
}

func TestNewSet(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("渡した組をすべて持つ", func(t *testing.T) {
			t.Parallel()
			pairs := []Pair{{Type: "a_b", Attr: "c"}, {Type: "d_e", Attr: "f"}}
			s := NewSet(pairs)
			for _, p := range pairs {
				assert.True(t, s.Has(p.Type, p.Attr))
			}
		})
	})
}

func Test_parseRow(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("3列の行から resource type と属性を読む。3列目は読まない", func(t *testing.T) {
			t.Parallel()
			got, err := parseRow("| `aws_ecs_service` | `task_definition` | 登録と更新 |")
			require.NoError(t, err)
			assert.Equal(t, Pair{Type: "aws_ecs_service", Attr: "task_definition"}, got)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for name, in := range map[string]string{
			"列が4つ":    "| `a_b` | `c` | d | e |",
			"属性が all": "| `a_b` | `all` | d |",
		} {
			t.Run(name+"なら失敗する", func(t *testing.T) {
				t.Parallel()
				_, err := parseRow(in)
				require.ErrorIs(t, err, ErrMalformed)
			})
		}
	})
}

func Test_codeCell(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("前後の空白を除いた1つの inline code の中身を返す", func(t *testing.T) {
			t.Parallel()
			got, err := codeCell("  `task_definition` ", attrName)
			require.NoError(t, err)
			assert.Equal(t, "task_definition", got)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for name, in := range map[string]string{
			"inline code の外に文字がある": "`a` です",
			"inline code が無い":      "a",
			"空":                    "",
			"形が違う":                 "`A`",
		} {
			t.Run(name+"なら失敗する", func(t *testing.T) {
				t.Parallel()
				_, err := codeCell(in, attrName)
				require.ErrorIs(t, err, ErrMalformed)
			})
		}
	})
}

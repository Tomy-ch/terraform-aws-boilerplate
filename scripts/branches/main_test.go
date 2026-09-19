package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/branches"
)

// sound は、書き換え先として最小限の構造を持つ保護設定。
const sound = `{
  "name": "branch-protection",
  "conditions": {
    "ref_name": {
      "exclude": [],
      "include": [
        "refs/heads/old"
      ]
    }
  },
  "rules": []
}
`

// writeProtection は一時ディレクトリへ保護設定を置き、そのパスを返します。
// 実物を読むテストは、今日の保護設定の内容で通ったり落ちたりするようになります。
func writeProtection(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "branch-protection.json")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	return path
}

// includeOf は保護設定から conditions.ref_name.include を取り出します。
func includeOf(t *testing.T, content []byte) []string {
	t.Helper()
	var doc struct {
		Conditions struct {
			RefName struct {
				Include []string `json:"include"`
			} `json:"ref_name"`
		} `json:"conditions"`
	}
	require.NoError(t, json.Unmarshal(content, &doc))

	return doc.Conditions.RefName.Include
}

func Test_rewrite(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("include を宣言から組み直す", func(t *testing.T) {
			t.Parallel()
			got, err := rewrite([]byte(sound), branches.Protected)
			require.NoError(t, err)

			want := make([]string, 0, len(branches.Protected))
			for _, p := range branches.Protected {
				want = append(want, refPrefix+p)
			}
			assert.Equal(t, want, includeOf(t, got))
		})

		// 全体を再直列化するとキーの並びが辞書順へ変わり、生成器が触るべきでない箇所まで
		// 書き換わる。include の区間だけを置き換えていることを、並びで確かめる。
		t.Run("include 以外の並びと内容をそのまま保つ", func(t *testing.T) {
			t.Parallel()
			got, err := rewrite([]byte(sound), branches.Protected)
			require.NoError(t, err)

			before, after := sound, string(got)

			head, _, ok := strings.Cut(before, `"include"`)
			require.True(t, ok)
			assert.True(t, strings.HasPrefix(after, head), "include より前が変わっている")

			_, tail, ok := strings.Cut(before, "      ]\n")
			require.True(t, ok)
			assert.True(t, strings.HasSuffix(after, tail), "include より後が変わっている")
		})

		// exclude が include の前に在るので、終端を「最初に現れる ]」で探すと取り違える。
		t.Run("直前のインライン配列に引きずられない", func(t *testing.T) {
			t.Parallel()
			got, err := rewrite([]byte(sound), branches.Protected)
			require.NoError(t, err)
			assert.Contains(t, string(got), `"exclude": []`)
		})

		// インライン形式で来ても、行の走査ではなく括弧の対応で終端を決める。
		t.Run("include がインライン形式でも置き換える", func(t *testing.T) {
			t.Parallel()
			inline := `{"conditions":{"ref_name":{"exclude":[],"include":[]}},"name":"x"}`
			got, err := rewrite([]byte(inline), branches.Protected)
			require.NoError(t, err)
			assert.Equal(t, len(branches.Protected), len(includeOf(t, got)))
			assert.Contains(t, string(got), `"name":"x"`)
		})

		// includeSpan の経路解決が壊れると、ここが無関係な配列を書き換えたまま成功する。
		t.Run("別条件の include を書き換えない", func(t *testing.T) {
			t.Parallel()
			const decoy = `{"conditions": {"repository_name": {"include": ["repo-a"]}, "ref_name": {"include": ["refs/heads/old"]}}}`
			got, err := rewrite([]byte(decoy), branches.Protected)
			require.NoError(t, err)

			assert.Contains(t, string(got), `"repository_name": {"include": ["repo-a"]}`)
			assert.Equal(t, len(branches.Protected), len(includeOf(t, got)))
		})

		// 整形が毎回変わると、内容が同じでも check が落ち続ける。
		t.Run("2 度かけても同じ結果になる", func(t *testing.T) {
			t.Parallel()
			once, err := rewrite([]byte(sound), branches.Protected)
			require.NoError(t, err)
			twice, err := rewrite(once, branches.Protected)
			require.NoError(t, err)
			assert.Equal(t, string(once), string(twice))
		})

		t.Run("末尾に改行を残す", func(t *testing.T) {
			t.Parallel()
			got, err := rewrite([]byte(sound), branches.Protected)
			require.NoError(t, err)
			assert.Equal(t, byte('\n'), got[len(got)-1])
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		// 読めない入力を取りこぼしとして扱うと、保護設定を空のまま生成しうる。
		t.Run("JSON として読めなければエラーにする", func(t *testing.T) {
			t.Parallel()
			_, err := rewrite([]byte("{ not json"), branches.Protected)
			require.Error(t, err)
		})

		// 宣言が空のまま生成すると、保護対象0件の設定を作ったうえで成功を報告する。
		// 「保護されていない」ではなく「保護されているつもり」を作る経路。
		t.Run("宣言の保護対象が0件ならエラーにする", func(t *testing.T) {
			t.Parallel()
			_, err := rewrite([]byte(sound), nil)
			require.ErrorIs(t, err, errShape)
		})

		// 構造の不在を補って続行すると、保護対象0件の設定を作ったまま成功で返る。
		for name, content := range map[string]string{
			"conditions が無い":                  `{"name": "x"}`,
			"conditions.ref_name が無い":         `{"conditions": {}}`,
			"conditions.ref_name.include が無い": `{"conditions": {"ref_name": {"exclude": []}}}`,
		} {
			t.Run(name+"ならエラーにする", func(t *testing.T) {
				t.Parallel()
				_, err := rewrite([]byte(content), branches.Protected)
				require.ErrorIs(t, err, errShape)
			})
		}
	})
}

func Test_applyOrCheck(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("ずれていれば check は errDrift を返す", func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			err := applyOrCheck(writeProtection(t, sound), true, &out)
			require.ErrorIs(t, err, errDrift)
			assert.Contains(t, out.String(), "❌")
		})

		// check が書き換えると、検査が対象を自分で直して緑を返すことになる。
		t.Run("check は書き換えない", func(t *testing.T) {
			t.Parallel()
			path := writeProtection(t, sound)
			var out bytes.Buffer
			_ = applyOrCheck(path, true, &out)

			after, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, sound, string(after))
		})

		t.Run("apply は書き換えて成功する", func(t *testing.T) {
			t.Parallel()
			path := writeProtection(t, sound)
			var out bytes.Buffer
			require.NoError(t, applyOrCheck(path, false, &out))

			after, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.NotContains(t, includeOf(t, after), refPrefix+"old")
			assert.Contains(t, out.String(), "✅")
		})

		t.Run("apply の直後は check が通る", func(t *testing.T) {
			t.Parallel()
			path := writeProtection(t, sound)
			var out bytes.Buffer
			require.NoError(t, applyOrCheck(path, false, &out))
			require.NoError(t, applyOrCheck(path, true, &out))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		// 不在を「ずれなし」として通すと、保護設定を失ったまま緑になる。
		t.Run("ファイルが無ければエラーにする", func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			err := applyOrCheck(filepath.Join(t.TempDir(), "no-such.json"), true, &out)
			require.Error(t, err)
			require.NotErrorIs(t, err, errDrift)
		})

		t.Run("構造が違えば check でもエラーにする", func(t *testing.T) {
			var out bytes.Buffer
			err := applyOrCheck(writeProtection(t, `{"name":"x"}`), true, &out)
			require.ErrorIs(t, err, errShape)
		})
	})
}

func Test_run(t *testing.T) {
	t.Parallel()

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for name, args := range map[string][]string{
			"サブコマンドが無い": nil,
			"未知のサブコマンド": {"no-such"},
			"引数が多い":     {"check", "extra"},
		} {
			t.Run(name+"ならエラーにする", func(t *testing.T) {
				t.Parallel()
				var out bytes.Buffer
				require.ErrorIs(t, run(args, &out), errUsage)
			})
		}
	})
}

func Test_requireRefName(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("include が配列なら通る", func(t *testing.T) {
			t.Parallel()
			doc := map[string]any{"conditions": map[string]any{"ref_name": map[string]any{"include": []any{}}}}
			require.NoError(t, requireRefName(doc))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		// 構造の不在を補って続行すると、保護対象0件の設定を作ったまま成功で返る。
		for name, doc := range map[string]map[string]any{
			"conditions が無い":          {"name": "x"},
			"conditions が object でない": {"conditions": "x"},
			"ref_name が無い":            {"conditions": map[string]any{}},
			"ref_name が object でない":   {"conditions": map[string]any{"ref_name": "x"}},
			"include が無い":             {"conditions": map[string]any{"ref_name": map[string]any{"exclude": []any{}}}},
			"include が配列でない":          {"conditions": map[string]any{"ref_name": map[string]any{"include": "x"}}},
		} {
			t.Run(name+"ならエラーにする", func(t *testing.T) {
				t.Parallel()
				require.ErrorIs(t, requireRefName(doc), errShape)
			})
		}
	})
}

func Test_includeSpan(t *testing.T) {
	t.Parallel()

	// nest は conditions.ref_name の中身を包んで、経路を持つ最小の保護設定にします。
	nest := func(refName string) string {
		return `{"conditions": {"ref_name": ` + refName + `}, "rules": []}`
	}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("複数行の配列の区間を返す", func(t *testing.T) {
			t.Parallel()
			content := []byte("{\n  \"conditions\": {\n    \"ref_name\": {\n      \"include\": [\n        \"a\"\n      ]\n    }\n  }\n}\n")
			start, end, err := requireSpan(t, content)
			require.NoError(t, err)
			assert.Equal(t, "[\n        \"a\"\n      ]", string(content[start:end]))
		})

		// GitHub の ruleset は conditions.repository_name にも同じ include / exclude を持つ。
		// 先に現れたそちらを掴むと、保護対象を無関係な配列へ書き込む。
		t.Run("別条件の include を掴まない", func(t *testing.T) {
			t.Parallel()
			content := []byte(`{"conditions": {"repository_name": {"include": ["repo-a"]}, "ref_name": {"include": ["refs/heads/x"]}}}`)
			start, end, err := requireSpan(t, content)
			require.NoError(t, err)
			assert.Equal(t, `["refs/heads/x"]`, string(content[start:end]))
		})

		// 値として現れた "include" という文字列を、キーと取り違えない。
		t.Run("値に現れた include という文字列を掴まない", func(t *testing.T) {
			t.Parallel()
			content := []byte(`{"description": "include", "conditions": {"ref_name": {"exclude": [], "include": ["refs/heads/x"]}}}`)
			start, end, err := requireSpan(t, content)
			require.NoError(t, err)
			assert.Equal(t, `["refs/heads/x"]`, string(content[start:end]))
		})

		// 同じ object の中で、include より前に現れた配列に引きずられない。
		t.Run("直前の exclude に引きずられない", func(t *testing.T) {
			t.Parallel()
			content := []byte(nest(`{"exclude": ["a"], "include": ["b"]}`))
			start, end, err := requireSpan(t, content)
			require.NoError(t, err)
			assert.Equal(t, `["b"]`, string(content[start:end]))
		})

		// fnmatch は文字クラス [...] を許すので、値に角括弧が来る。
		t.Run("文字列の中の角括弧を終端と読まない", func(t *testing.T) {
			t.Parallel()
			content := []byte(nest(`{"include": ["a]b", "c"]}`))
			start, end, err := requireSpan(t, content)
			require.NoError(t, err)
			assert.Equal(t, `["a]b", "c"]`, string(content[start:end]))
		})

		t.Run("エスケープされた引用符で文字列を抜けない", func(t *testing.T) {
			t.Parallel()
			content := []byte(nest(`{"include": ["a\"]", "b"]}`))
			start, end, err := requireSpan(t, content)
			require.NoError(t, err)
			assert.Equal(t, `["a\"]", "b"]`, string(content[start:end]))
		})

		t.Run("入れ子の配列を数え違えない", func(t *testing.T) {
			t.Parallel()
			content := []byte(nest(`{"include": [["a"], "b"]}`))
			start, end, err := requireSpan(t, content)
			require.NoError(t, err)
			assert.Equal(t, `[["a"], "b"]`, string(content[start:end]))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		// 区間を決められないまま続行すると、無関係な位置へ書き込む。
		for name, content := range map[string]string{
			"最上位が object でない": `["include"]`,
			"conditions が無い":  `{"name": "x"}`,
			"ref_name が無い":    `{"conditions": {"repository_name": {"include": []}}}`,
			"include が無い":     `{"conditions": {"ref_name": {"exclude": []}}}`,
			"値が配列でない":         `{"conditions": {"ref_name": {"include": "x"}}}`,
			"配列が閉じていない":       `{"conditions": {"ref_name": {"include": ["a"`,
		} {
			t.Run(name+"ならエラーにする", func(t *testing.T) {
				t.Parallel()
				_, _, err := includeSpan([]byte(content))
				require.ErrorIs(t, err, errShape)
			})
		}
	})
}

// requireSpan は includeSpan を呼び、区間が content の範囲に収まっていることを確かめます。
func requireSpan(t *testing.T, content []byte) (int, int, error) {
	t.Helper()

	start, end, err := includeSpan(content)
	if err == nil {
		require.GreaterOrEqual(t, start, 0)
		require.LessOrEqual(t, end, len(content))
		require.Less(t, start, end)
	}

	return start, end, err
}

func Test_indentOf(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		for name, tc := range map[string]struct {
			content string
			at      int
			want    string
		}{
			"行頭からの空白を数える":    {content: "a\n    b", at: 6, want: "    "},
			"字下げが無ければ空を返す":   {content: "a\nb", at: 2, want: ""},
			"改行が無い先頭行でも数える":  {content: "  x", at: 2, want: "  "},
			"位置が行頭そのものでも数える": {content: "a\n  b", at: 2, want: "  "},
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				assert.Equal(t, tc.want, indentOf([]byte(tc.content), tc.at))
			})
		}
	})
}

// ---- workflow の起動条件とブランチ集合 --------------------------------------

// workflowFixture は、生成対象を2種類とも持つ最小の workflow。
// 実物を読むテストは、今日の workflow の内容で通ったり落ちたりするようになります。
const workflowFixture = `name: Probe

on:
  pull_request:
    branches:
      - touched-by-nobody
  push:
    branches:
      - old
      - 'release/old'
    paths:
      - 'x'
  workflow_dispatch:

jobs:
  probe:
    if: ${{ contains(fromJSON('["old"]'), github.base_ref) }}
    runs-on: ubuntu-latest
  notify:
    if: ${{ always() && contains(fromJSON('["failure", "cancelled"]'), needs.probe.result) }}
    runs-on: ubuntu-latest
`

// linesOf は、テスト対象が受け取る形へ整えます。
func linesOf(s string) []string { return strings.Split(s, "\n") }

// writeWorkflows は一時ディレクトリへ workflow を置き、そのディレクトリを返します。
func writeWorkflows(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
	}

	return dir
}

func Test_branchesBlock(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		// pull_request 側にも branches: がある。押さえどころは「どちらを掴むか」で、
		// 取り違えると push を絞ったつもりで必須検査の起動条件を書き換える。
		t.Run("push 側の区間と字下げを返す", func(t *testing.T) {
			t.Parallel()
			lines := linesOf(workflowFixture)
			first, last, indent, found, err := branchesBlock(lines)
			require.NoError(t, err)
			require.True(t, found)
			assert.Equal(t, []string{"      - old", "      - 'release/old'"}, lines[first:last])
			assert.Equal(t, "      ", indent)
		})

		// YAML 1.1 は素の on を真偽値に読むため、引用して書く流儀がある。
		t.Run("on が引用されていても見つける", func(t *testing.T) {
			t.Parallel()
			_, _, _, found, err := branchesBlock(linesOf(strings.Replace(workflowFixture, "on:", `"on":`, 1)))
			require.NoError(t, err)
			assert.True(t, found)
		})

		// 生成の対象なので、注記は次の apply で黙って消える。区間を切って見なかったことに
		// するのでも、消してしまうのでもなく、落とす。
		t.Run("項目の間にコメントがあればエラーにする", func(t *testing.T) {
			t.Parallel()
			src := strings.Replace(workflowFixture, "      - old\n", "      - old\n      # 注記\n", 1)
			_, _, _, _, err := branchesBlock(linesOf(src))
			require.ErrorIs(t, err, errShape)
		})

		t.Run("push を持たなければ found は false", func(t *testing.T) {
			t.Parallel()
			_, _, _, found, err := branchesBlock(linesOf("on:\n  pull_request:\n\njobs:\n  a:\n    runs-on: x\n"))
			require.NoError(t, err)
			assert.False(t, found)
		})

		// on: の区間を末尾まで延ばすと、jobs 配下の push という名の job を掴む。
		t.Run("jobs 配下の push という名の job を掴まない", func(t *testing.T) {
			t.Parallel()
			src := "on:\n  pull_request:\n\njobs:\n  push:\n    branches:\n      - x\n"
			_, _, _, found, err := branchesBlock(linesOf(src))
			require.NoError(t, err)
			assert.False(t, found)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for name, src := range map[string]string{
			"項目以外の行がある": "on:\n  push:\n    branches:\n      - a\n      unexpected: 1\n",
			"項目が0件":     "on:\n  push:\n    branches:\n    paths:\n      - x\n",
		} {
			t.Run(name+"ならエラーにする", func(t *testing.T) {
				t.Parallel()
				_, _, _, _, err := branchesBlock(linesOf(src))
				require.ErrorIs(t, err, errShape)
			})
		}
	})
}

func Test_rewriteWorkflow(t *testing.T) {
	t.Parallel()

	set := []string{"develop", "release/*"}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("起動条件を宣言へ組み直す", func(t *testing.T) {
			t.Parallel()
			got, err := rewriteWorkflow(linesOf(workflowFixture), set)
			require.NoError(t, err)
			assert.Contains(t, got, "    branches:\n      - develop\n      - 'release/*'\n    paths:\n")
		})

		t.Run("ブランチ集合の式を宣言へ組み直す", func(t *testing.T) {
			t.Parallel()
			got, err := rewriteWorkflow(linesOf(workflowFixture), set)
			require.NoError(t, err)
			assert.Contains(t, got, `fromJSON('["develop", "release/*"]'), github.base_ref`)
		})

		// job の結果を並べた fromJSON を掴むと、通知の条件が壊れる。
		t.Run("ブランチ名と突き合わせていない集合は触らない", func(t *testing.T) {
			t.Parallel()
			got, err := rewriteWorkflow(linesOf(workflowFixture), set)
			require.NoError(t, err)
			assert.Contains(t, got, `fromJSON('["failure", "cancelled"]'), needs.probe.result`)
		})

		// pull_request 側を絞ると、報告の不在が合格として数えられる（ADR-0603 決定16-18）。
		t.Run("pull_request 側の branches を書き換えない", func(t *testing.T) {
			t.Parallel()
			got, err := rewriteWorkflow(linesOf(workflowFixture), set)
			require.NoError(t, err)
			assert.Contains(t, got, "      - touched-by-nobody")
		})

		t.Run("2 度かけても同じ結果になる", func(t *testing.T) {
			t.Parallel()
			once, err := rewriteWorkflow(linesOf(workflowFixture), set)
			require.NoError(t, err)
			twice, err := rewriteWorkflow(linesOf(once), set)
			require.NoError(t, err)
			assert.Equal(t, once, twice)
		})

		t.Run("生成対象を持たない workflow をそのまま返す", func(t *testing.T) {
			t.Parallel()
			src := "name: X\n\njobs:\n  a:\n    runs-on: x\n"
			got, err := rewriteWorkflow(linesOf(src), set)
			require.NoError(t, err)
			assert.Equal(t, src, got)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		// 起動条件が空の workflow は push で一度も走らず、それでいて check は緑を返す。
		t.Run("宣言の集合が0件ならエラーにする", func(t *testing.T) {
			t.Parallel()
			_, err := rewriteWorkflow(linesOf(workflowFixture), nil)
			require.ErrorIs(t, err, errShape)
		})

		// 生成先は YAML の引用スカラーと、単一引用符で囲む GitHub Actions の式である。
		// 囲みが破れた workflow を成功で書き込まない。
		for name, v := range map[string]string{
			"単一引用符を含む": "a'b",
			"二重引用符を含む": `a"b`,
			"改行を含む":    "a\nb",
			"空文字列":     "",
		} {
			t.Run("宣言に"+name+"値があればエラーにする", func(t *testing.T) {
				t.Parallel()
				_, err := rewriteWorkflow(linesOf(workflowFixture), []string{"develop", v})
				require.ErrorIs(t, err, errShape)
			})
		}
	})
}

func Test_applyOrCheckWorkflows(t *testing.T) {
	t.Parallel()

	sets := map[string][]string{"probe.yaml": {"develop"}}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("ずれていれば check は errDrift を返す", func(t *testing.T) {
			t.Parallel()
			dir := writeWorkflows(t, map[string]string{"probe.yaml": workflowFixture})
			var out bytes.Buffer
			require.ErrorIs(t, applyOrCheckWorkflows(dir, sets, true, &out), errDrift)
			assert.Contains(t, out.String(), "probe.yaml")
		})

		t.Run("check は書き換えない", func(t *testing.T) {
			t.Parallel()
			dir := writeWorkflows(t, map[string]string{"probe.yaml": workflowFixture})
			var out bytes.Buffer
			require.Error(t, applyOrCheckWorkflows(dir, sets, true, &out))
			after, err := os.ReadFile(filepath.Join(dir, "probe.yaml"))
			require.NoError(t, err)
			assert.Equal(t, workflowFixture, string(after))
		})

		t.Run("apply の直後は check が通る", func(t *testing.T) {
			t.Parallel()
			dir := writeWorkflows(t, map[string]string{"probe.yaml": workflowFixture})
			var out bytes.Buffer
			require.NoError(t, applyOrCheckWorkflows(dir, sets, false, &out))
			require.NoError(t, applyOrCheckWorkflows(dir, sets, true, &out))
		})

		// 宣言の対象外でも、ブランチのパターンを持たない workflow は通す。
		t.Run("対象外でもパターンを持たなければ通す", func(t *testing.T) {
			t.Parallel()
			dir := writeWorkflows(t, map[string]string{
				"probe.yaml": workflowFixture,
				"other.yaml": "name: Other\n\non:\n  pull_request:\n\njobs:\n  a:\n    runs-on: x\n",
			})
			var out bytes.Buffer
			require.NoError(t, applyOrCheckWorkflows(dir, sets, false, &out))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		// 走査対象を失った検査は、合格ではなく検査していない状態である。
		t.Run("workflow が1件も無ければエラーにする", func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			require.ErrorIs(t, applyOrCheckWorkflows(t.TempDir(), sets, true, &out), errShape)
		})

		t.Run("宣言が指す workflow が無ければエラーにする", func(t *testing.T) {
			t.Parallel()
			dir := writeWorkflows(t, map[string]string{"other.yaml": "name: Other\n\njobs:\n  a:\n    runs-on: x\n"})
			var out bytes.Buffer
			require.ErrorIs(t, applyOrCheckWorkflows(dir, sets, true, &out), errOrphan)
		})

		t.Run("宣言が指す workflow が生成対象を持たなければエラーにする", func(t *testing.T) {
			t.Parallel()
			dir := writeWorkflows(t, map[string]string{"probe.yaml": "name: P\n\njobs:\n  a:\n    runs-on: x\n"})
			var out bytes.Buffer
			require.ErrorIs(t, applyOrCheckWorkflows(dir, sets, true, &out), errOrphan)
		})

		t.Run("対象外の workflow が起動条件を持てばエラーにする", func(t *testing.T) {
			t.Parallel()
			dir := writeWorkflows(t, map[string]string{
				"probe.yaml": workflowFixture,
				"other.yaml": "on:\n  push:\n    branches:\n      - develop\n",
			})
			var out bytes.Buffer
			require.ErrorIs(t, applyOrCheckWorkflows(dir, sets, true, &out), errUnmanaged)
		})

		t.Run("ブランチ名を式へ直接書いていればエラーにする", func(t *testing.T) {
			t.Parallel()
			dir := writeWorkflows(t, map[string]string{
				"probe.yaml": workflowFixture,
				"other.yaml": "jobs:\n  a:\n    if: ${{ github.base_ref == '" + branches.Default + "' }}\n",
			})
			var out bytes.Buffer
			require.ErrorIs(t, applyOrCheckWorkflows(dir, sets, true, &out), errUnmanaged)
		})
	})
}

func Test_workflowFiles(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		// 片方の拡張子だけを見ると、もう片方で書かれた workflow が走査から黙って外れる。
		t.Run("yaml と yml の両方を拾う", func(t *testing.T) {
			t.Parallel()
			dir := writeWorkflows(t, map[string]string{"a.yaml": "x", "b.yml": "x"})
			got, err := workflowFiles(dir)
			require.NoError(t, err)
			assert.Len(t, got, 2)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("0 件ならエラーにする", func(t *testing.T) {
			t.Parallel()
			_, err := workflowFiles(t.TempDir())
			require.ErrorIs(t, err, errShape)
		})
	})
}

func Test_workflowTargets(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("起動条件と集合の式をそれぞれ報告する", func(t *testing.T) {
			t.Parallel()
			hasBranches, hasSet, err := targetsOf(workflowFixture)
			require.NoError(t, err)
			assert.True(t, hasBranches)
			assert.True(t, hasSet)
		})

		t.Run("job の結果の集合を生成対象と数えない", func(t *testing.T) {
			t.Parallel()
			src := "jobs:\n  a:\n    if: ${{ contains(fromJSON('[\"failure\"]'), needs.b.result) }}\n"
			hasBranches, hasSet, err := targetsOf(src)
			require.NoError(t, err)
			assert.False(t, hasBranches)
			assert.False(t, hasSet)
		})

		t.Run("push を持たない workflow を通す", func(t *testing.T) {
			t.Parallel()
			hasBranches, hasSet, err := targetsOf("on:\n  pull_request:\n")
			require.NoError(t, err)
			assert.False(t, hasBranches)
			assert.False(t, hasSet)
		})
	})

	// **行の走査が読めない形は、すべてここで落ちなければならない。** 落ちずに「対象なし」へ
	// 畳まれると、宣言の外へ直書きされたパターンが誰の目にも触れないまま check が緑になる。
	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for name, src := range map[string]string{
			"branches がフロー配列":   "on:\n  push:\n    branches: [develop, evil]\n",
			"on 自体がフロー":         "on: {push: {branches: [evil]}}\n",
			"on の行末にコメント":       "on: # trigger\n  push:\n    branches:\n      - evil\n",
			"push の行末にコメント":     "on:\n  push: # x\n    branches:\n      - evil\n",
			"branches の行末にコメント": "on:\n  push:\n    branches: # x\n      - evil\n",
		} {
			t.Run(name+"ならエラーにする", func(t *testing.T) {
				t.Parallel()
				_, _, err := targetsOf(src)
				require.ErrorIs(t, err, errNotation)
			})
		}

		// 生成できないものを残せば、宣言を直しても追随せず必ずずれる。
		t.Run("branches-ignore ならエラーにする", func(t *testing.T) {
			t.Parallel()
			_, _, err := targetsOf("on:\n  push:\n    branches-ignore:\n      - evil\n")
			require.ErrorIs(t, err, errUnmanaged)
		})

		for name, src := range map[string]string{
			"空":                "",
			"YAML として読めない":     "on:\n  push:\n   - a\n  - b\n",
			"最上位が mapping でない": "- a\n- b\n",
		} {
			t.Run(name+"ならエラーにする", func(t *testing.T) {
				t.Parallel()
				_, _, err := targetsOf(src)
				require.ErrorIs(t, err, errShape)
			})
		}
	})
}

// targetsOf は、2つの経路へ同じ内容を渡します。
func targetsOf(src string) (bool, bool, error) {
	return workflowTargets([]byte(src), linesOf(src))
}

func Test_yamlScalar(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		// 素で書くと YAML が別の意味に取る値だけを引用する。全部引用すると既存の書式を壊す。
		// 記号だけでは足りない —— YAML 1.1 は on / off / yes / no を真偽値に解決するので、
		// 素で書くと項目が文字列でなくなる。
		for in, want := range map[string]string{
			"develop":   "develop",
			"release/*": "'release/*'",
			"a,b":       "'a,b'",
			"#x":        "'#x'",
			"on":        "'on'",
			"OFF":       "'OFF'",
			"No":        "'No'",
			"null":      "'null'",
			"-x":        "'-x'",
			"2024":      "'2024'",
		} {
			t.Run(in+" を "+want+" にする", func(t *testing.T) {
				t.Parallel()
				assert.Equal(t, want, yamlScalar(in))
			})
		}
	})
}

func Test_unmanagedExpression(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("生成できる形は対象にしない", func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, unmanagedExpression(linesOf(workflowFixture)))
		})

		t.Run("ブランチ名に触れない式は対象にしない", func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, unmanagedExpression(linesOf("        REF_NAME: ${{ github.ref_name }}\n")))
		})

		// run: のスクリプトは文字列であって式ではない。
		t.Run("ブロックスカラーの中身は対象にしない", func(t *testing.T) {
			t.Parallel()
			src := "    steps:\n      - run: |\n          echo ${{ github.base_ref }} '" + branches.Default + "'\n"
			assert.Empty(t, unmanagedExpression(linesOf(src)))
		})

		t.Run("直接比較を見つける", func(t *testing.T) {
			t.Parallel()
			src := "    if: ${{ github.base_ref == '" + branches.Default + "' }}\n"
			assert.Contains(t, unmanagedExpression(linesOf(src)), branches.Default)
		})

		// 1行に複数の文字列が並ぶと、どれが犯人かは判定できない。当たった値だけを名指すと、
		// 読んだ人は無関係な方を直しに行く。
		t.Run("犯人を1つに決めず、行をそのまま返す", func(t *testing.T) {
			t.Parallel()
			src := "    if: ${{ github.ref_name == 'develop' && vars.STAGE == '" + branches.Default + "' }}\n"
			got := unmanagedExpression(linesOf(src))
			assert.Contains(t, got, "develop")
			assert.Contains(t, got, branches.Default)
		})
	})
}

func Test_declaredLiterals(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		// glob を含めると、`release/*` という文字列を含む行すべてが直接比較に見える。
		t.Run("glob を含まない", func(t *testing.T) {
			t.Parallel()
			for _, v := range declaredLiterals() {
				assert.NotContains(t, v, "*", v)
			}
		})

		t.Run("重複を持たない", func(t *testing.T) {
			t.Parallel()
			seen := map[string]bool{}
			for _, v := range declaredLiterals() {
				require.False(t, seen[v], "重複: %s", v)
				seen[v] = true
			}
		})
	})
}

func Test_findKey(t *testing.T) {
	t.Parallel()

	lines := linesOf("  a:\n    b:\n    push:\n  push:\n")

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		// 区間が入れ子の途中から始まっていても、親の直下だけを返す。
		t.Run("最も浅い階層のキーを返す", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, 3, findKey(lines, 0, len(lines), "push"))
		})

		t.Run("無ければ -1 を返す", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, -1, findKey(lines, 0, len(lines), "no-such"))
			assert.Equal(t, -1, findKey(nil, 0, 0, "push"))
		})
	})
}

func Test_blockEnd(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("字下げが親以下へ戻る行を返す", func(t *testing.T) {
			t.Parallel()
			lines := linesOf("on:\n  push:\n    branches:\njobs:\n")
			assert.Equal(t, 3, blockEnd(lines, 1, len(lines), 0))
		})

		t.Run("空行とコメントで区間を終わらせない", func(t *testing.T) {
			t.Parallel()
			lines := linesOf("on:\n  push:\n\n  # 注記\n  x:\njobs:\n")
			assert.Equal(t, 5, blockEnd(lines, 1, len(lines), 0))
		})

		t.Run("戻らなければ区間の終端を返す", func(t *testing.T) {
			t.Parallel()
			lines := linesOf("on:\n  push:\n")
			assert.Equal(t, len(lines), blockEnd(lines, 1, len(lines), 0))
		})
	})
}

func Test_indentWidth(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		for in, want := range map[string]int{"": 0, "a": 0, "  a": 2, "    ": 4} {
			t.Run("「"+in+"」は "+strconv.Itoa(want), func(t *testing.T) {
				t.Parallel()
				assert.Equal(t, want, indentWidth(in))
			})
		}
	})
}

func Test_requireAllSeen(t *testing.T) {
	t.Parallel()

	sets := map[string][]string{"a.yaml": {"x"}, "b.yaml": {"x"}}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("すべて実在すれば通る", func(t *testing.T) {
			t.Parallel()
			require.NoError(t, requireAllSeen(map[string]bool{"a.yaml": true, "b.yaml": true}, sets))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("欠けていればその名前を添えてエラーにする", func(t *testing.T) {
			t.Parallel()
			err := requireAllSeen(map[string]bool{"a.yaml": true}, sets)
			require.ErrorIs(t, err, errOrphan)
			assert.Contains(t, err.Error(), "b.yaml")
		})
	})
}

func Test_expressionLines(t *testing.T) {
	t.Parallel()

	// run: のスクリプトは文字列であって式ではない。ここを外さないと、走るはずの
	// コマンドを書き換える。
	src := "jobs:\n  a:\n    if: ${{ x }}\n    steps:\n      - run: |\n          echo ${{ x }}\n      - run: echo done\n"

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("ブロックスカラーの中身を外す", func(t *testing.T) {
			t.Parallel()
			lines := linesOf(src)
			got := make([]string, 0, len(lines))
			for _, i := range expressionLines(lines) {
				got = append(got, lines[i])
			}
			assert.NotContains(t, got, "          echo ${{ x }}")
			assert.Contains(t, got, "    if: ${{ x }}")
			assert.Contains(t, got, "      - run: echo done")
		})
	})
}

func Test_pushBranchFilter(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("ブロック形式の branches を見つける", func(t *testing.T) {
			t.Parallel()
			found, key, flow, err := pushBranchFilter([]byte("on:\n  push:\n    branches:\n      - a\n"))
			require.NoError(t, err)
			assert.True(t, found)
			assert.Equal(t, "branches", key)
			assert.False(t, flow)
		})

		t.Run("フロー形式を flow として報告する", func(t *testing.T) {
			t.Parallel()
			_, _, flow, err := pushBranchFilter([]byte("on:\n  push:\n    branches: [a]\n"))
			require.NoError(t, err)
			assert.True(t, flow)
		})

		// on は YAML 1.1 では真偽値に解決されうる。型で辿ると見失う。
		t.Run("on がフローでも辿れる", func(t *testing.T) {
			t.Parallel()
			found, _, _, err := pushBranchFilter([]byte("on: {push: {branches: [a]}}\n"))
			require.NoError(t, err)
			assert.True(t, found)
		})

		t.Run("branches-ignore をキー名ごと報告する", func(t *testing.T) {
			t.Parallel()
			_, key, _, err := pushBranchFilter([]byte("on:\n  push:\n    branches-ignore:\n      - a\n"))
			require.NoError(t, err)
			assert.Equal(t, "branches-ignore", key)
		})

		// pull_request 側を掴むと、必須 context を報告する側を絞ってしまう。
		t.Run("pull_request 側の branches を掴まない", func(t *testing.T) {
			t.Parallel()
			found, _, _, err := pushBranchFilter([]byte("on:\n  pull_request:\n    branches:\n      - a\n"))
			require.NoError(t, err)
			assert.False(t, found)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for name, src := range map[string]string{
			"空":                "",
			"YAML として読めない":     "on:\n  push:\n   - a\n  - b\n",
			"最上位が mapping でない": "- a\n",
		} {
			t.Run(name+"ならエラーにする", func(t *testing.T) {
				t.Parallel()
				_, _, _, err := pushBranchFilter([]byte(src))
				require.ErrorIs(t, err, errShape)
			})
		}
	})
}

func Test_mapValue(t *testing.T) {
	t.Parallel()

	var doc yaml.Node
	require.NoError(t, yaml.Unmarshal([]byte("a:\n  b: 1\n"), &doc))

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("キーに対応する値を返す", func(t *testing.T) {
			t.Parallel()
			assert.NotNil(t, mapValue(doc.Content[0], "a"))
			assert.Equal(t, "1", mapValue(mapValue(doc.Content[0], "a"), "b").Value)
		})

		t.Run("無いキーと mapping でない入力に nil を返す", func(t *testing.T) {
			t.Parallel()
			assert.Nil(t, mapValue(doc.Content[0], "no-such"))
			assert.Nil(t, mapValue(nil, "a"))
			assert.Nil(t, mapValue(mapValue(mapValue(doc.Content[0], "a"), "b"), "c"))
		})
	})
}

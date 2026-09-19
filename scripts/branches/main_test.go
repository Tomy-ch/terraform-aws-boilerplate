package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
			got, err := rewrite([]byte(sound))
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
			got, err := rewrite([]byte(sound))
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
			got, err := rewrite([]byte(sound))
			require.NoError(t, err)
			assert.Contains(t, string(got), `"exclude": []`)
		})

		// インライン形式で来ても、行の走査ではなく括弧の対応で終端を決める。
		t.Run("include がインライン形式でも置き換える", func(t *testing.T) {
			t.Parallel()
			inline := `{"conditions":{"ref_name":{"exclude":[],"include":[]}},"name":"x"}`
			got, err := rewrite([]byte(inline))
			require.NoError(t, err)
			assert.Equal(t, len(branches.Protected), len(includeOf(t, got)))
			assert.Contains(t, string(got), `"name":"x"`)
		})

		// 整形が毎回変わると、内容が同じでも check が落ち続ける。
		t.Run("2 度かけても同じ結果になる", func(t *testing.T) {
			t.Parallel()
			once, err := rewrite([]byte(sound))
			require.NoError(t, err)
			twice, err := rewrite(once)
			require.NoError(t, err)
			assert.Equal(t, string(once), string(twice))
		})

		t.Run("末尾に改行を残す", func(t *testing.T) {
			t.Parallel()
			got, err := rewrite([]byte(sound))
			require.NoError(t, err)
			assert.Equal(t, byte('\n'), got[len(got)-1])
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		// 読めない入力を取りこぼしとして扱うと、保護設定を空のまま生成しうる。
		t.Run("JSON として読めなければエラーにする", func(t *testing.T) {
			t.Parallel()
			_, err := rewrite([]byte("{ not json"))
			require.Error(t, err)
		})

		// 構造の不在を補って続行すると、保護対象0件の設定を作ったまま成功で返る。
		for name, content := range map[string]string{
			"conditions が無い":                  `{"name": "x"}`,
			"conditions.ref_name が無い":         `{"conditions": {}}`,
			"conditions.ref_name.include が無い": `{"conditions": {"ref_name": {"exclude": []}}}`,
		} {
			t.Run(name+"ならエラーにする", func(t *testing.T) {
				t.Parallel()
				_, err := rewrite([]byte(content))
				require.ErrorIs(t, err, errShape)
			})
		}
	})
}

//nolint:paralleltest // applyOrCheck はファイルを書き換えるため、同じパスを共有しない形で順に回す
func Test_applyOrCheck(t *testing.T) {
	t.Run("正常系", func(t *testing.T) {
		t.Run("ずれていれば check は errDrift を返す", func(t *testing.T) {
			var out bytes.Buffer
			err := applyOrCheck(writeProtection(t, sound), true, &out)
			require.ErrorIs(t, err, errDrift)
			assert.Contains(t, out.String(), "❌")
		})

		// check が書き換えると、検査が対象を自分で直して緑を返すことになる。
		t.Run("check は書き換えない", func(t *testing.T) {
			path := writeProtection(t, sound)
			var out bytes.Buffer
			_ = applyOrCheck(path, true, &out)

			after, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, sound, string(after))
		})

		t.Run("apply は書き換えて成功する", func(t *testing.T) {
			path := writeProtection(t, sound)
			var out bytes.Buffer
			require.NoError(t, applyOrCheck(path, false, &out))

			after, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.NotContains(t, includeOf(t, after), refPrefix+"old")
			assert.Contains(t, out.String(), "✅")
		})

		t.Run("apply の直後は check が通る", func(t *testing.T) {
			path := writeProtection(t, sound)
			var out bytes.Buffer
			require.NoError(t, applyOrCheck(path, false, &out))
			require.NoError(t, applyOrCheck(path, true, &out))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		// 不在を「ずれなし」として通すと、保護設定を失ったまま緑になる。
		t.Run("ファイルが無ければエラーにする", func(t *testing.T) {
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

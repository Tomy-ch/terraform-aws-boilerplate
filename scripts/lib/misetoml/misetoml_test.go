package misetoml_test

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/misetoml"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

func TestParse(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("裸のキーと引用符付きのキーを、書かれた順で返す", func(t *testing.T) {
			t.Parallel()

			got, err := misetoml.Parse([]byte("[tools]\n" +
				"go = \"1.27.1\"\n" +
				"\"aqua:hashicorp/terraform\" = \"1.16.2\"\n" +
				"node = \"24.21.0\"\n"))
			require.NoError(t, err)
			assert.Equal(t, []misetoml.Entry{
				{Key: "go", Version: "1.27.1"},
				{Key: "aqua:hashicorp/terraform", Version: "1.16.2"},
				{Key: "node", Version: "24.21.0"},
			}, got)
		})

		t.Run("tool option 付きの1行宣言から版を読む", func(t *testing.T) {
			t.Parallel()

			got, err := misetoml.Parse([]byte("[tools]\n" +
				"node = { version = \"24.21.0\", postinstall = \"echo hi\" }\n"))
			require.NoError(t, err)
			assert.Equal(t, []misetoml.Entry{{Key: "node", Version: "24.21.0"}}, got)
		})

		t.Run("行末のコメントを値に混ぜない", func(t *testing.T) {
			t.Parallel()

			got, err := misetoml.Parse([]byte("[tools]\ngo = \"1.27.1\" # 実装言語\n"))
			require.NoError(t, err)
			assert.Equal(t, []misetoml.Entry{{Key: "go", Version: "1.27.1"}}, got)
		})

		t.Run("[tools] の外は読まない", func(t *testing.T) {
			t.Parallel()

			got, err := misetoml.Parse([]byte("[settings]\n" +
				"pipx_uvx = \"1.2.3\"\n" +
				"[tools]\n" +
				"go = \"1.27.1\"\n" +
				"[env]\n" +
				"SOME_VERSION = \"9.9.9\"\n"))
			require.NoError(t, err)
			assert.Equal(t, []misetoml.Entry{{Key: "go", Version: "1.27.1"}}, got)
		})

		t.Run("[tools.foo] は [tools] とは別の節として扱う", func(t *testing.T) {
			t.Parallel()

			got, err := misetoml.Parse([]byte("[tools]\n" +
				"go = \"1.27.1\"\n" +
				"[tools.node]\n" +
				"version = \"24.21.0\"\n"))
			require.NoError(t, err)
			assert.Equal(t, []misetoml.Entry{{Key: "go", Version: "1.27.1"}}, got)
		})

		t.Run("コメント行と空行を読み飛ばす", func(t *testing.T) {
			t.Parallel()

			got, err := misetoml.Parse([]byte("# 見出し\n\n[tools]\n\n# 実装言語\ngo = \"1.27.1\"\n"))
			require.NoError(t, err)
			assert.Equal(t, []misetoml.Entry{{Key: "go", Version: "1.27.1"}}, got)
		})

		t.Run("見出しの後ろに注記があっても節として読む", func(t *testing.T) {
			t.Parallel()

			got, err := misetoml.Parse([]byte("[tools] # 道具の宣言\ngo = \"1.27.1\"\n"))
			require.NoError(t, err)
			assert.Equal(t, []misetoml.Entry{{Key: "go", Version: "1.27.1"}}, got)
		})

		// 正規化しない実装では、下の表記はいずれも `[tools]` と別の節になり、供給網の
		// 検査が対象を1件も持たないまま緑を返す（理由は sectionName の宣言）。
		for name, heading := range map[string]string{
			"内側の両端に空白":  "[ tools ]",
			"内側の右に空白":   "[tools ]",
			"内側の左に空白":   "[ tools]",
			"二重引用符付き":   `["tools"]`,
			"単一引用符付き":   `['tools']`,
			"引用符と空白の両方": `[ "tools" ]`,
		} {
			t.Run(name+"の見出しも [tools] として読む", func(t *testing.T) {
				t.Parallel()

				got, err := misetoml.Parse([]byte(heading + "\ngo = \"1.27.1\"\n"))
				require.NoError(t, err)
				assert.Equal(t, []misetoml.Entry{{Key: "go", Version: "1.27.1"}}, got)
			})
		}

		t.Run("正規化しても [tools.foo] は [tools] にならない", func(t *testing.T) {
			t.Parallel()

			got, err := misetoml.Parse([]byte("[ tools.node ]\nversion = \"24.21.0\"\n"))
			require.NoError(t, err)
			assert.Empty(t, got)
		})

		t.Run("[tools] が無ければ空を返す", func(t *testing.T) {
			t.Parallel()

			got, err := misetoml.Parse([]byte("[settings]\nfoo = \"1\"\n"))
			require.NoError(t, err)
			assert.Empty(t, got)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		// **読み飛ばすと、その道具は検査の対象から外れたままゲートが緑を返す**（ADR-0702 決定14）。
		for _, tt := range []struct {
			name string
			line string
		}{
			{"単一引用符は TOML として正しいが、この読み取りは受け付けない", "go = '1.27.1'"},
			{"版が引用符で閉じていない", "go = 1.27.1"},
			{"`.` を含む裸キーは TOML の裸キーではない", "foo.bar = \"1.0.0\""},
			// 引用符を対で要求しないと、手編集で壊れた宣言から拾った値のまま写しが書き換わる。
			{"開き引用符しかない", "\"go = \"1.27.1\""},
			{"閉じ引用符しかない", "go\" = \"1.27.1\""},
			{"複数版の配列は、どれが有効かを黙って選ばない", "go = [\"1.27.1\", \"1.26.0\"]"},
			{"version を持たない tool option", "go = { postinstall = \"echo hi\" }"},
			{"代入ですらない行", "これは宣言ではない"},
			// 見出しと取り違えると section が動き、別の table のキーを道具として読む。
			// 見出しになり損ねた行は、節の中では解釈できない行である。
			{"閉じ括弧が無い見出しもどき", "[tools"},
			{"開き括弧が無い見出しもどき", "tools]"},
			{"名前が空の見出しもどき", "[]"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				_, err := misetoml.Parse([]byte("[tools]\n" + tt.line + "\n"))
				require.Error(t, err)
				assert.True(t, xerrors.Is(err, misetoml.ErrInvalidLine))
				// 直す先が分かる形で落ちること。行番号と行そのものを載せる。
				assert.Contains(t, err.Error(), "2行目")
				assert.Contains(t, err.Error(), tt.line)
			})
		}

		t.Run("キーの重複は後勝ちで上書きせずエラーにする", func(t *testing.T) {
			t.Parallel()

			// 後勝ちで通すと、検査されるのは2つ目だけになり、1つ目は宣言されているのに
			// 誰も見ていない状態になる。
			_, err := misetoml.Parse([]byte("[tools]\ngo = \"1.27.1\"\ngo = \"1.26.0\"\n"))
			require.Error(t, err)
			assert.True(t, xerrors.Is(err, misetoml.ErrDuplicateKey))
			assert.Contains(t, err.Error(), "3行目")
			assert.Contains(t, err.Error(), "2行目にもあります")
		})

		t.Run("[tools] の外の解釈できない行はエラーにしない", func(t *testing.T) {
			t.Parallel()

			// 範囲を限る目的がここに出る。`[settings]` の書式まで面倒を見ると、
			// このパッケージが mise の設定全体の parser になってしまう。
			got, err := misetoml.Parse([]byte("[settings]\nこれは宣言ではない\n[tools]\ngo = \"1.27.1\"\n"))
			require.NoError(t, err)
			assert.Equal(t, []misetoml.Entry{{Key: "go", Version: "1.27.1"}}, got)
		})
	})
}

func TestParseFile(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("ファイルを読んで Parse した結果を返す", func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "mise.toml")
			require.NoError(t, os.WriteFile(path, []byte("[tools]\ngo = \"1.27.1\"\n"), 0o600))

			got, err := misetoml.ParseFile(path)
			require.NoError(t, err)
			assert.Equal(t, []misetoml.Entry{{Key: "go", Version: "1.27.1"}}, got)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("読めないパスはエラーにする", func(t *testing.T) {
			t.Parallel()

			_, err := misetoml.ParseFile(filepath.Join(t.TempDir(), "居ない.toml"))
			require.Error(t, err)
		})

		t.Run("解釈できない行はパスを添えてエラーにする", func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "mise.toml")
			require.NoError(t, os.WriteFile(path, []byte("[tools]\ngo = 1.27.1\n"), 0o600))

			_, err := misetoml.ParseFile(path)
			require.Error(t, err)
			assert.True(t, xerrors.Is(err, misetoml.ErrInvalidLine))
			assert.Contains(t, err.Error(), path)
		})
	})
}

func TestLookup(t *testing.T) {
	t.Parallel()

	entries := []misetoml.Entry{
		{Key: "go", Version: "1.27.1"},
		{Key: "aqua:aws/aws-cli", Version: "2.36.40"},
	}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("宣言が在れば版と true を返す", func(t *testing.T) {
			t.Parallel()

			v, ok := misetoml.Lookup(entries, "aqua:aws/aws-cli")
			assert.True(t, ok)
			assert.Equal(t, "2.36.40", v)
		})

		t.Run("宣言が無ければ false を返す", func(t *testing.T) {
			t.Parallel()

			// 空文字と false を分けて返すのは、呼び手が「宣言が無い」と「空の版」を
			// 区別できるようにするため。空の版で写し先を書き潰す経路を作らない。
			v, ok := misetoml.Lookup(entries, "node")
			assert.False(t, ok)
			assert.Empty(t, v)
		})
	})
}

func TestBareKeyPattern(t *testing.T) {
	t.Parallel()

	re := regexp.MustCompile(`^` + misetoml.BareKeyPattern + `$`)

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		for _, key := range []string{"go", "node", "golangci-lint", "a_b", "0start", "A1"} {
			assert.True(t, re.MatchString(key), key)
		}
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		// 裸で通すと、引用符付きでしか書けない宣言を裸キーとして読んでしまう。
		for _, key := range []string{"aqua:aws/aws-cli", "npm:markdownlint-cli2", "foo.bar", "a b", ""} {
			assert.False(t, re.MatchString(key), key)
		}
	})
}

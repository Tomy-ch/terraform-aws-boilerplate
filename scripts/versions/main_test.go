package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const soundMise = `min_version = "2026.6.0"

[env]
go = "should-be-ignored"

[tools]
go = "1.27.1"
node = "24.21.0"
`

// soundDockerfile は、写しを両方持つ Dockerfile。
const soundDockerfile = `# FROM golang:9.9.9-bookworm は例示であって写しではない
FROM golang:1.27.1-bookworm@sha256:aaa AS builder
FROM golang:1.27.1-bookworm@sha256:aaa AS tools
FROM node:24.21.0-alpine@sha256:bbb AS node_tools
`

const soundGoMod = `module example

go 1.27.1

require ()
`

// newRepo は、宣言と写しを持つ一時リポジトリの root を返します
// （フィクスチャ方針は scripts/README.md の Test Strategy 節）。
func newRepo(t *testing.T, mise, dockerfile, gomod string) string {
	t.Helper()
	root := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(root, miseFile), []byte(mise), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "docker", "tools"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "docker", "tools", "Dockerfile"), []byte(dockerfile), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "scripts"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "scripts", "go.mod"), []byte(gomod), 0o600))

	return root
}

func readAt(t *testing.T, root string, parts ...string) string {
	t.Helper()
	got, err := os.ReadFile(filepath.Join(append([]string{root}, parts...)...))
	require.NoError(t, err)

	return string(got)
}

func Test_run(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("check は書き換えず errDrift を返す", func(t *testing.T) {
			t.Parallel()
			drifted := strings.ReplaceAll(soundDockerfile, "golang:1.27.1", "golang:1.26.0")
			root := newRepo(t, soundMise, drifted, soundGoMod)
			var out bytes.Buffer

			require.ErrorIs(t, run([]string{"check"}, root, &out), errDrift)
			assert.Equal(t, drifted, readAt(t, root, "docker", "tools", "Dockerfile"))
			// 出力そのものが契約である —— CI ログを読む人が、どの写しがずれたかを知る唯一の手掛かり。
			assert.Contains(t, out.String(), "Dockerfile")
		})

		t.Run("apply は写しを宣言へ揃える", func(t *testing.T) {
			t.Parallel()
			drifted := strings.ReplaceAll(soundDockerfile, "golang:1.27.1", "golang:1.26.0")
			root := newRepo(t, soundMise, drifted, soundGoMod)
			var out bytes.Buffer

			require.NoError(t, run([]string{"apply"}, root, &out))
			assert.Equal(t, soundDockerfile, readAt(t, root, "docker", "tools", "Dockerfile"))
			assert.Contains(t, out.String(), "Dockerfile")
		})

		t.Run("apply の直後は check が通る", func(t *testing.T) {
			t.Parallel()
			root := newRepo(t, soundMise, strings.ReplaceAll(soundDockerfile, "1.27.1", "1.26.0"), soundGoMod)
			var out bytes.Buffer

			require.NoError(t, run([]string{"apply"}, root, &out))
			require.NoError(t, run([]string{"check"}, root, &out))
		})

		t.Run("一致していれば宣言の値を報告する", func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			require.NoError(t, run([]string{"check"}, newRepo(t, soundMise, soundDockerfile, soundGoMod), &out))
			assert.Contains(t, out.String(), "1.27.1")
			assert.Contains(t, out.String(), "24.21.0")
		})
	})

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
				require.ErrorIs(t, run(args, t.TempDir(), &out), errUsage)
			})
		}

		// 片方だけ書き換わった状態が残ると、呼び出し側には「失敗した」としか見えない。
		t.Run("写しの1つが壊れていれば他も書き換えない", func(t *testing.T) {
			t.Parallel()
			drifted := strings.ReplaceAll(soundDockerfile, "golang:1.27.1", "golang:1.26.0")
			// go ディレクティブを失った go.mod。3つ目の rule で落ちる。
			root := newRepo(t, soundMise, drifted, "module example\n")
			var out bytes.Buffer

			require.Error(t, run([]string{"apply"}, root, &out))
			assert.Equal(t, drifted, readAt(t, root, "docker", "tools", "Dockerfile"))
		})
	})
}

func Test_parseMise(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		// [env] にも go というキーがある。table を見ずにキー名だけで拾うと取り違える。
		t.Run("tools 配下だけを読む", func(t *testing.T) {
			t.Parallel()
			root := newRepo(t, soundMise, soundDockerfile, soundGoMod)
			got, err := parseMise(filepath.Join(root, miseFile))
			require.NoError(t, err)
			assert.Equal(t, declared{Go: "1.27.1", Node: "24.21.0"}, got)
		})

		t.Run("コメントを読み飛ばす", func(t *testing.T) {
			t.Parallel()
			src := "[tools]\n# go = \"0.0.0\"\ngo = \"1.27.1\"\nnode = \"24.21.0\"\n"
			root := newRepo(t, src, soundDockerfile, soundGoMod)
			got, err := parseMise(filepath.Join(root, miseFile))
			require.NoError(t, err)
			assert.Equal(t, "1.27.1", got.Go)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		// 空のまま進むと、写しを空の版で書き潰す。
		for name, src := range map[string]string{
			"go が無い":    "[tools]\nnode = \"24.21.0\"\n",
			"node が無い":  "[tools]\ngo = \"1.27.1\"\n",
			"tools が無い": "[env]\ngo = \"1.27.1\"\n",
			"空":         "",
			// TOML は引用符付きキーもインラインテーブルも許す。この読み取り器はどちらも
			// 解釈しないので、黙って「宣言が無い」側へ落ちることを固定する。
			"go が引用符付きキー":   "[tools]\n\"go\" = \"1.27.1\"\nnode = \"24.21.0\"\n",
			"go がインラインテーブル": "[tools]\ngo = { version = \"1.27.1\" }\nnode = \"24.21.0\"\n",
		} {
			t.Run(name+"ならエラーにする", func(t *testing.T) {
				t.Parallel()
				root := newRepo(t, src, soundDockerfile, soundGoMod)
				_, err := parseMise(filepath.Join(root, miseFile))
				require.ErrorIs(t, err, errShape)
			})
		}

		t.Run("ファイルが無ければエラーにする", func(t *testing.T) {
			t.Parallel()
			_, err := parseMise(filepath.Join(t.TempDir(), "no-such.toml"))
			require.ErrorIs(t, err, os.ErrNotExist)
		})
	})
}

func Test_applyRule(t *testing.T) {
	t.Parallel()

	r := rule{label: "golang", file: "Dockerfile", re: dockerFromRe("golang"), version: "1.27.1", count: 2}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("接尾辞を保ったまま版だけ差し替える", func(t *testing.T) {
			t.Parallel()
			got, err := applyRule(r, "FROM golang:1.0.0-bookworm AS a\nFROM golang:1.0.0-alpine AS b\n")
			require.NoError(t, err)
			assert.Equal(t, "FROM golang:1.27.1-bookworm AS a\nFROM golang:1.27.1-alpine AS b\n", got)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		// 写しが増えたことも減ったことも、宣言との対応が崩れた合図である。
		for name, src := range map[string]string{
			"件数が足りない": "FROM golang:1.0.0-bookworm\n",
			"件数が多い":   "FROM golang:1.0.0-a\nFROM golang:1.0.0-b\nFROM golang:1.0.0-c\n",
			"1件も無い":   "FROM node:1.0.0-alpine\n",
		} {
			t.Run(name+"ならエラーにする", func(t *testing.T) {
				t.Parallel()
				_, err := applyRule(r, src)
				require.ErrorIs(t, err, errShape)
			})
		}

		// 空で書き換えると `FROM golang:-bookworm` を作り、件数のガードは通る。
		t.Run("宣言側の版が空ならエラーにする", func(t *testing.T) {
			t.Parallel()
			empty := r
			empty.version = ""
			_, err := applyRule(empty, "FROM golang:1.0.0-a\nFROM golang:1.0.0-b\n")
			require.ErrorIs(t, err, errShape)
		})
	})
}

func Test_dockerFromRe(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		// 文字クラスから改行を落とすと、マッチが行をまたいで広がり、間の行が置換で消える。
		// 件数は変わらないので、件数のガードも通り抜ける。
		t.Run("コメント行を対象にしない", func(t *testing.T) {
			t.Parallel()
			src := "# FROM golang:9.9.9-bookworm\nFROM golang:1.0.0-bookworm\n"
			assert.Len(t, dockerFromRe("golang").FindAllString(src, -1), 1)
		})

		t.Run("別のイメージを掴まない", func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, dockerFromRe("node").FindAllString("FROM golang:1.0.0-a\n", -1))
		})

		// レジストリを明示した FROM は、宣言の写しとしては別物である。
		t.Run("前置されたレジストリを掴まない", func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, dockerFromRe("golang").FindAllString("FROM docker.io/library/golang:1.0.0-a\n", -1))
		})

		// 版は `\d+(?:\.\d+){0,2}` なので1〜3桁を許す。3桁だけを試していると、
		// 桁数を絞る方向の変更が起きても気づけない。
		t.Run("1桁・2桁の版にも一致する", func(t *testing.T) {
			t.Parallel()
			src := "FROM golang:1-bookworm\nFROM golang:1.27-bookworm\n"
			assert.Len(t, dockerFromRe("golang").FindAllString(src, -1), 2)
		})

		// suffix は必須である（`-bookworm` 等）。無い FROM は写しとして扱わない。
		t.Run("suffix の無い FROM を掴まない", func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, dockerFromRe("golang").FindAllString("FROM golang:1.27.1\n", -1))
		})
	})
}

func Test_applyAll(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("差分が無ければ宣言の版を報告して成功する", func(t *testing.T) {
			t.Parallel()

			root := newRepo(t, soundMise, soundDockerfile, soundGoMod)

			var out bytes.Buffer

			require.NoError(t, applyAll(root, true, &out))
			assert.Contains(t, out.String(), "1.27.1")
			assert.Contains(t, out.String(), "24.21.0")
		})

		t.Run("dryRun でなければ写しを揃えて成功する", func(t *testing.T) {
			t.Parallel()

			drifted := strings.ReplaceAll(soundDockerfile, "golang:1.27.1", "golang:1.26.0")
			root := newRepo(t, soundMise, drifted, soundGoMod)

			var out bytes.Buffer

			require.NoError(t, applyAll(root, false, &out))
			assert.Equal(t, soundDockerfile, readAt(t, root, "docker", "tools", "Dockerfile"))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		// applyAll は複数のファイルへ書き込む。changes の組み立てを配線し損ねた場合に
		// 部分適用が残らないことを、この呼び出し経路で固定する。
		t.Run("途中で書けなければ、先のファイルも書き換えない", func(t *testing.T) {
			t.Parallel()

			drifted := strings.ReplaceAll(soundDockerfile, "golang:1.27.1", "golang:1.26.0")
			driftedMod := strings.ReplaceAll(soundGoMod, "go 1.27.1", "go 1.26.0")
			root := newRepo(t, soundMise, drifted, driftedMod)

			// 書き込みは昇順（docker/... が先、scripts/... が後）。後者の一時ファイル名を
			// ディレクトリで塞ぎ、先に書いた分が残らないことを見る。
			blocked := filepath.Join(root, "scripts", "go.mod.atomicwrite.tmp")
			require.NoError(t, os.MkdirAll(blocked, 0o700))

			var out bytes.Buffer

			require.Error(t, applyAll(root, false, &out))
			assert.Equal(t, drifted, readAt(t, root, "docker", "tools", "Dockerfile"),
				"先に処理したファイルが書き換わっている")
		})

		t.Run("dryRun は書き換えず errDrift を返す", func(t *testing.T) {
			t.Parallel()

			drifted := strings.ReplaceAll(soundDockerfile, "golang:1.27.1", "golang:1.26.0")
			root := newRepo(t, soundMise, drifted, soundGoMod)

			var out bytes.Buffer

			require.ErrorIs(t, applyAll(root, true, &out), errDrift)
			assert.Equal(t, drifted, readAt(t, root, "docker", "tools", "Dockerfile"))
			assert.Contains(t, out.String(), "Dockerfile")
		})

		t.Run("mise.toml が読めなければエラーを返す", func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer

			require.ErrorIs(t, applyAll(t.TempDir(), true, &out), os.ErrNotExist)
		})
	})
}

func Test_rules(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		// 対応表が空になると、何も検査していない状態が「一致」として報告される。
		t.Run("空でない", func(t *testing.T) {
			t.Parallel()
			assert.NotEmpty(t, rules(declared{Go: "1", Node: "2"}))
		})

		t.Run("宣言の版をそのまま持つ", func(t *testing.T) {
			t.Parallel()
			for _, r := range rules(declared{Go: "1.27.1", Node: "24.21.0"}) {
				assert.Contains(t, []string{"1.27.1", "24.21.0"}, r.version, r.label)
				assert.Positive(t, r.count, r.label)
			}
		})
	})
}

func Test_plan(t *testing.T) {
	t.Parallel()

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		// 表が空になったことを「一致」と報告しない。
		t.Run("対応表が空ならエラーにする", func(t *testing.T) {
			t.Parallel()
			_, err := plan(nil, t.TempDir())
			require.ErrorIs(t, err, errShape)
		})

		t.Run("写しのファイルが無ければエラーにする", func(t *testing.T) {
			t.Parallel()
			rs := []rule{{label: "x", file: "no-such", re: goDirectiveRe, version: "1", count: 1}}
			_, err := plan(rs, t.TempDir())
			require.ErrorIs(t, err, os.ErrNotExist)
			require.NotErrorIs(t, err, errShape, "対応表の異常と読み取りの失敗を取り違えている")
		})
	})
}

func Test_planNames(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("名前を並べて返す", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, []string{"a", "z"}, planNames(map[string]string{"/x/z": "", "/y/a": ""}))
		})

		t.Run("空には空を返す", func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, planNames(map[string]string{}))
		})
	})
}

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
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
"aqua:hashicorp/terraform" = "1.16.2"
"aqua:aws/aws-cli" = "2.36.40"
`

// soundHostTools は、焼き込んだ版を両方持つレシピ。**行頭の `\t@` を含める** —— 置換で前置きが
// 落ちる欠陥は、前置きの無い入力では現れない。
const soundHostTools = `host-tools-install:
	@command -v mise >/dev/null 2>&1 || exit 1
	@mise install "aqua:hashicorp/terraform@1.16.2"
	@mise install "aqua:aws/aws-cli@2.36.40"
	@mise reshim
`

// soundDockerfile は、写しを両方持つ Dockerfile。
const soundDockerfile = `# FROM golang:9.9.9-bookworm は例示であって写しではない
FROM golang:1.27.1-bookworm@sha256:aaa AS builder
FROM golang:1.27.1-bookworm@sha256:aaa AS tools
FROM node:24.21.0-alpine@sha256:bbb AS node_tools
`

// driftedHostTools は、terraform の版だけが宣言からずれたレシピ。**この写し先を起点にする
// ケースが無いと、apply が host-tools.mk へ実際に書き込む経路も、check がこのファイル名を
// 報告に載せる経路も、一度も実行されない。**
var driftedHostTools = strings.ReplaceAll(soundHostTools, "terraform@1.16.2", "terraform@1.0.0")

const soundGoMod = `module example

go 1.27.1

require ()
`

// newRepo は、宣言と写しを持つ一時リポジトリの root を返します
// （フィクスチャ方針は scripts/README.md の Test Strategy 節）。
// hostTools は任意で、省略すると soundHostTools を書きます。**位置引数を増やさないのは、
// 4本の文字列が並ぶ呼び出しで取り違えても型が通るためです。**
func newRepo(t *testing.T, mise, dockerfile, gomod string, hostTools ...string) string {
	t.Helper()
	require.LessOrEqual(t, len(hostTools), 1, "host-tools.mk の内容は1つだけ渡せます")

	mk := soundHostTools
	if len(hostTools) == 1 {
		mk = hostTools[0]
	}

	root := t.TempDir()

	require.NoError(t, os.WriteFile(filepath.Join(root, miseFile), []byte(mise), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "docker", "tools"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "docker", "tools", "Dockerfile"), []byte(dockerfile), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "scripts"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "scripts", "go.mod"), []byte(gomod), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".makefiles"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".makefiles", "host-tools.mk"), []byte(mk), 0o600))

	return root
}

// miseWithout は soundMise から [tools] の宣言を1つだけ落とした内容を返します。
//
// **他の宣言は残す。** 全部欠けた入力では、テストの名前が指す宣言の欠落を検出したのか、
// 別の宣言の欠落を検出したのかを区別できない。落とす対象が実在したことも確かめる ——
// 綴りを間違えると、何も落とさない入力で「エラーになった」を確認してしまう。
func miseWithout(t *testing.T, key string) string {
	t.Helper()

	var kept []string
	section := ""
	dropped := false

	for _, line := range strings.Split(soundMise, "\n") {
		if m := miseSectionRe.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			section = m[1]
		}
		if section == "tools" {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, key+" =") || strings.HasPrefix(trimmed, `"`+key+`" =`) {
				dropped = true

				continue
			}
		}
		kept = append(kept, line)
	}
	require.True(t, dropped, "落とす対象 %q が soundMise の [tools] に無い", key)

	return strings.Join(kept, "\n")
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

		// 起点にする理由は driftedHostTools の宣言が持つ。
		t.Run("check は host-tools.mk のずれも名前を挙げて報告する", func(t *testing.T) {
			t.Parallel()
			root := newRepo(t, soundMise, soundDockerfile, soundGoMod, driftedHostTools)
			var out bytes.Buffer

			require.ErrorIs(t, run([]string{"check"}, root, &out), errDrift)
			assert.Equal(t, driftedHostTools, readAt(t, root, ".makefiles", "host-tools.mk"))
			assert.Contains(t, out.String(), "host-tools.mk")
		})

		t.Run("apply は host-tools.mk の版も揃える", func(t *testing.T) {
			t.Parallel()
			root := newRepo(t, soundMise, soundDockerfile, soundGoMod, driftedHostTools)
			var out bytes.Buffer

			require.NoError(t, run([]string{"apply"}, root, &out))
			assert.Equal(t, soundHostTools, readAt(t, root, ".makefiles", "host-tools.mk"))
		})

		t.Run("apply は複数のファイルを同時に揃える", func(t *testing.T) {
			t.Parallel()
			drifted := strings.ReplaceAll(soundDockerfile, "golang:1.27.1", "golang:1.26.0")
			root := newRepo(t, soundMise, drifted, soundGoMod, driftedHostTools)
			var out bytes.Buffer

			require.NoError(t, run([]string{"apply"}, root, &out))
			assert.Equal(t, soundDockerfile, readAt(t, root, "docker", "tools", "Dockerfile"))
			assert.Equal(t, soundHostTools, readAt(t, root, ".makefiles", "host-tools.mk"))
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
			assert.Contains(t, out.String(), "1.16.2")
			assert.Contains(t, out.String(), "2.36.40")
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

func Test_versionPattern(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		// **捕獲群を持たないことが、3つの正規表現の群番号を動かさない条件である。**
		// ここへ群を1つ足すと dockerFromRe と miseInstallRe の後置きが ${3} へずれ、
		// goDirectiveRe は版の桁を ${2} として拾う。Go はどちらもエラーにしない。
		t.Run("捕獲群を持たない", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, 0, regexp.MustCompile(versionPattern).NumSubexp())
		})

		// **同じ [tools] を scripts/tool-cooldown も読む。** 集合がずれると、一方だけが読める
		// 宣言が生まれ、もう一方は黙ってその道具を検査の対象から外す。同じ表を両方のテストが
		// 持ち、どちらかを動かせば落ちる形にしておく。
		t.Run("裸のキーは TOML の仕様どおりの文字だけを許す", func(t *testing.T) {
			t.Parallel()

			re := regexp.MustCompile(`^` + bareKeyPattern + `$`)
			for _, ok := range []string{"go", "golangci-lint", "node_tool", "1password-cli", "A1"} {
				assert.True(t, re.MatchString(ok), ok)
			}
			// `:` `/` を含む backend 付きのキーと、dotted key は裸で書けない。
			for _, ng := range []string{"", "aqua:owner/repo", "npm:@scope/pkg", "tools.go", "a b"} {
				assert.False(t, re.MatchString(ng), ng)
			}
		})

		t.Run("1〜3桁の版だけに一致する", func(t *testing.T) {
			t.Parallel()

			re := regexp.MustCompile(`^` + versionPattern + `$`)
			for _, ok := range []string{"1", "1.27", "1.27.1"} {
				assert.True(t, re.MatchString(ok), ok)
			}
			for _, ng := range []string{"", "1.27.1.2", "1.27.1 ", " 1.27.1", "1.27.1$1", "v1.27.1", "1.27.1-rc1"} {
				assert.False(t, re.MatchString(ng), ng)
			}
		})
	})
}

func Test_goDirectiveRe(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		// 桁の並びは versionPattern が持つ。3桁だけを試していると、そこが絞られても気づけない。
		t.Run("1〜3桁の版に一致する", func(t *testing.T) {
			t.Parallel()
			assert.Len(t, goDirectiveRe.FindAllString("go 1\ngo 1.27\ngo 1.27.1\n", -1), 3)
		})

		// **行末に錨を打つ。** 外すと `go 1.27.1 # comment` の版だけを差し替えて注記を残すなど、
		// go.mod として壊れた行を作りながら件数のガードは通る。
		t.Run("行末に余りがある行を掴まない", func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, goDirectiveRe.FindAllString("go 1.27.1 \n", -1))
			assert.Empty(t, goDirectiveRe.FindAllString("go 1.27.1.2\n", -1))
		})

		// go.mod は toolchain も持つ。掴むと go ディレクティブの写しが2件になり、件数が合わなくなる。
		t.Run("toolchain 行を掴まない", func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, goDirectiveRe.FindAllString("toolchain go1.27.1\n", -1))
		})

		t.Run("行頭でない go を掴まない", func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, goDirectiveRe.FindAllString("\tgo 1.27.1\n", -1))
		})
	})
}

func Test_miseSectionRe(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		for name, tc := range map[string]struct{ line, section string }{
			"tools の見出し": {"[tools]", "tools"},
			"env の見出し":   {"[env]", "env"},
			// ネストした table は "tools" と一致しないので、parseMise が読み飛ばす。
			// ここが `[tools.go]` から "tools" を返すようになると、別の table のキーを拾う。
			"ネストした見出し":     {"[tools.go]", "tools.go"},
			"見出しの後ろに注記がある": {"[tools] # 注記", "tools"},
		} {
			t.Run(name+"から名前を返す", func(t *testing.T) {
				t.Parallel()

				m := miseSectionRe.FindStringSubmatch(tc.line)
				require.NotNil(t, m, "一致しない")
				assert.Equal(t, tc.section, m[1])
			})
		}
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		// 見出しでない行を見出しと取り違えると、section が変わって別の table のキーを読む。
		for name, line := range map[string]string{
			"開き括弧が無い": "tools]",
			"閉じ括弧が無い": "[tools",
			"名前が空":    "[]",
			"代入行":     `go = "1.27.1"`,
		} {
			t.Run(name+"は見出しにしない", func(t *testing.T) {
				t.Parallel()
				assert.Nil(t, miseSectionRe.FindStringSubmatch(line))
			})
		}
	})
}

func Test_miseKeyRe(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		for name, tc := range map[string]struct{ line, key, version string }{
			"裸のキー":     {`go = "1.27.1"`, "go", "1.27.1"},
			"引用符付きのキー": {`"aqua:aws/aws-cli" = "2.36.40"`, "aqua:aws/aws-cli", "2.36.40"},
			// 裸で書けるキーを引用符で囲むのも TOML では正しい。
			"引用符で囲んだ裸のキー": {`"go" = "1.27.1"`, "go", "1.27.1"},
		} {
			t.Run(name+"を読む", func(t *testing.T) {
				t.Parallel()

				m := miseKeyRe.FindStringSubmatch(tc.line)
				require.NotNil(t, m, "一致しない")

				key := m[1]
				if key == "" {
					key = m[2]
				}
				assert.Equal(t, tc.key, key)
				assert.Equal(t, tc.version, m[3])
			})
		}
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		// 引用符を対で要求する理由は miseKeyRe の宣言が持つ。ここはそれを外したときに
		// 何が通ってしまうかを固定する。
		for name, line := range map[string]string{
			"開き引用符だけ":     `"aqua:aws/aws-cli = "2.36.40"`,
			"閉じ引用符だけ":     `aqua:aws/aws-cli" = "2.36.40"`,
			"値が配列":        `"aqua:aws/aws-cli" = ["2.36.40"]`,
			"値がインラインテーブル": `"aqua:aws/aws-cli" = { version = "2.36.40" }`,
			"値に引用符が無い":    `go = 1.27.1`,
			"見出し行":        `[tools]`,
		} {
			t.Run(name+"は掴まない", func(t *testing.T) {
				t.Parallel()
				assert.Nil(t, miseKeyRe.FindStringSubmatch(line))
			})
		}
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
			assert.Equal(t, declared{
				Go: "1.27.1", Node: "24.21.0", Terraform: "1.16.2", AWSCLI: "2.36.40",
			}, got)
		})

		// backend 付きのキーは `:` と `/` を含むため TOML では引用符が要る。引用符を剥がせないと
		// 宣言が在るのに「無い」と報告する。
		t.Run("引用符付きキーを読む", func(t *testing.T) {
			t.Parallel()
			root := newRepo(t, soundMise, soundDockerfile, soundGoMod)
			got, err := parseMise(filepath.Join(root, miseFile))
			require.NoError(t, err)
			assert.Equal(t, "1.16.2", got.Terraform)
			assert.Equal(t, "2.36.40", got.AWSCLI)
		})

		t.Run("コメントを読み飛ばす", func(t *testing.T) {
			t.Parallel()
			src := strings.Replace(soundMise, "go = \"1.27.1\"", "# go = \"0.0.0\"\ngo = \"1.27.1\"", 1)
			root := newRepo(t, src, soundDockerfile, soundGoMod)
			got, err := parseMise(filepath.Join(root, miseFile))
			require.NoError(t, err)
			assert.Equal(t, "1.27.1", got.Go)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		// 空のまま進むと、写しを空の版で書き潰す。**1つだけ落とす** —— 他も欠けた入力だと、
		// 名前が指す宣言の欠落を検出したのかが分からない。
		for _, key := range []string{"go", "node", terraformTool, awsCLITool} {
			t.Run(key+" が無ければエラーにする", func(t *testing.T) {
				t.Parallel()
				root := newRepo(t, miseWithout(t, key), soundDockerfile, soundGoMod)
				_, err := parseMise(filepath.Join(root, miseFile))
				require.ErrorIs(t, err, errShape)
				assert.Contains(t, err.Error(), key, "欠落した宣言の名前を報告していない")
			})
		}

		// TOML はインラインテーブルも配列も許すが、この読み取り器はどちらも解釈しない。
		// **配列は mise が複数版の併存のために実際に使う記法である** —— 黙って別の値を拾わず
		// 「宣言が無い」側へ落ちることを固定する。
		arrayed := strings.Replace(soundMise,
			`"aqua:hashicorp/terraform" = "1.16.2"`, `"aqua:hashicorp/terraform" = ["1.16.2", "1.9.0"]`, 1)
		multiline := strings.Replace(soundMise,
			`"aqua:hashicorp/terraform" = "1.16.2"`, "\"aqua:hashicorp/terraform\" = [\n  \"1.16.2\",\n]", 1)

		for name, src := range map[string]string{
			"tools が無い":        "[env]\ngo = \"1.27.1\"\n",
			"空":                "",
			"go がインラインテーブル":    strings.Replace(soundMise, "go = \"1.27.1\"", "go = { version = \"1.27.1\" }", 1),
			"terraform が配列":    arrayed,
			"terraform が複数行配列": multiline,
			"aws-cli がインラインテーブル": strings.Replace(soundMise,
				`"aqua:aws/aws-cli" = "2.36.40"`, `"aqua:aws/aws-cli" = { version = "2.36.40" }`, 1),
			// miseKeyRe の対の要求を、parseMise を通した側でも固定する。
			"キーの開き引用符だけがある": strings.Replace(soundMise,
				`"aqua:aws/aws-cli" = "2.36.40"`, `"aqua:aws/aws-cli = "2.36.40"`, 1),
			"キーの閉じ引用符だけがある": strings.Replace(soundMise,
				`"aqua:aws/aws-cli" = "2.36.40"`, `aqua:aws/aws-cli" = "2.36.40"`, 1),
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

		// **一致は versionPattern で見るのに置換が無検査だと、宣言に書かれた何でも写し先へ
		// 埋まる。** Go の置換文字列は `$` を後方参照として解釈するので、`1.2.3$1` は別の群へ
		// 化け、写し先が Makefile なら `$(shell ...)` が実行され得る。
		for name, version := range map[string]string{
			"後方参照を含む":   "1.2.3$1",
			"シェル関数を含む":  "1.2.3$(shell touch x)",
			"末尾に空白がある":  "1.27.1 ",
			"桁が多すぎる":    "1.2.3.4",
			"接頭辞が付いている": "v1.27.1",
		} {
			t.Run("宣言側の版が"+name+"ならエラーにする", func(t *testing.T) {
				t.Parallel()

				bad := r
				bad.version = version
				_, err := applyRule(bad, "FROM golang:1.0.0-a\nFROM golang:1.0.0-b\n")
				require.ErrorIs(t, err, errShape)
			})
		}

		// 群が無い正規表現では `${1}` が空文字へ落ち、前置きを失った内容が err == nil のまま
		// 書き出される。Go は存在しない群の参照をエラーにしない。
		t.Run("写し先の正規表現が捕獲群を持たなければエラーにする", func(t *testing.T) {
			t.Parallel()

			noGroup := r
			noGroup.re = regexp.MustCompile(`FROM golang:` + versionPattern + `-bookworm`)
			noGroup.count = 1
			_, err := applyRule(noGroup, "FROM golang:1.0.0-bookworm\n")
			require.ErrorIs(t, err, errShape)
		})

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

		// 桁の並びは versionPattern が持つ。3桁だけを試していると、そこが絞られても気づけない。
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

func Test_miseInstallRe(t *testing.T) {
	t.Parallel()

	re := miseInstallRe(terraformTool)

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		// **前置きを群の外で消費すると、置換でレシピ行の `\t@` ごと消える。** 一致件数は
		// 変わらないので件数のガードは通り抜け、壊れた Makefile が書き出される。
		t.Run("レシピ行の前置きを保ったまま版だけ差し替える", func(t *testing.T) {
			t.Parallel()
			r := rule{label: "terraform", file: "host-tools.mk", re: re, version: "1.16.2", count: 1}
			got, err := applyRule(r, "\t@mise install \"aqua:hashicorp/terraform@1.0.0\"\n")
			require.NoError(t, err)
			assert.Equal(t, "\t@mise install \"aqua:hashicorp/terraform@1.16.2\"\n", got)
		})

		t.Run("コメント行を対象にしない", func(t *testing.T) {
			t.Parallel()
			src := "# @mise install \"aqua:hashicorp/terraform@9.9.9\"\n\t@mise install \"aqua:hashicorp/terraform@1.0.0\"\n"
			assert.Len(t, re.FindAllString(src, -1), 1)
		})

		t.Run("別の道具を掴まない", func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, re.FindAllString("\t@mise install \"aqua:aws/aws-cli@2.36.40\"\n", -1))
		})

		// 桁の並びは versionPattern が持つ。3桁だけを試していると、そこが絞られても気づけない。
		t.Run("1桁・2桁の版にも一致する", func(t *testing.T) {
			t.Parallel()
			src := "\t@mise install \"aqua:hashicorp/terraform@1\"\n\t@mise install \"aqua:hashicorp/terraform@1.16\"\n"
			assert.Len(t, re.FindAllString(src, -1), 2)
		})

		// 閉じ引用符が錨である。無い行を掴むと、版の終わりを決められないまま置換する。
		t.Run("閉じ引用符の無い行を掴まない", func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, re.FindAllString("\t@mise install aqua:hashicorp/terraform@1.16.2\n", -1))
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
			assert.Contains(t, out.String(), "1.16.2")
			assert.Contains(t, out.String(), "2.36.40")
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

		// plan が守るのは「表が空」までである。**表から1行消えても、残りが一致していれば
		// 緑が返る** —— 写しが1つ検査されなくなったことを、誰も報せない。
		//
		// **宣言ごとに違う版を渡す。** 集合への所属や件数の合計では、rule 同士の割当が
		// 入れ替わっても通ってしまう。
		t.Run("表そのものを固定する", func(t *testing.T) {
			t.Parallel()

			type target struct {
				file    string
				version string
				count   int
			}

			got := map[string]target{}
			for _, r := range rules(declared{Go: "1", Node: "2", Terraform: "3", AWSCLI: "4"}) {
				got[r.label] = target{file: r.file, version: r.version, count: r.count}
			}
			assert.Equal(t, map[string]target{
				"golang イメージ":   {file: "docker/tools/Dockerfile", version: "1", count: 2},
				"node イメージ":     {file: "docker/tools/Dockerfile", version: "2", count: 1},
				"go ディレクティブ":    {file: "scripts/go.mod", version: "1", count: 1},
				"terraform の導入": {file: ".makefiles/host-tools.mk", version: "3", count: 1},
				"AWS CLI の導入":   {file: ".makefiles/host-tools.mk", version: "4", count: 1},
			}, got)
		})
	})
}

func Test_plan(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		// 同じファイルに複数の rule が掛かる。**途中の状態を持ち回らないと、2つ目の rule が
		// 1つ目の書き換えを捨てる。** 実物の Dockerfile には golang と node が両方在るので
		// 統合テストが間接的に守っているが、その保証は「たまたま2件掛かっている」に依っている。
		t.Run("同じファイルへ複数の rule を順に当てる", func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			path := filepath.Join(root, "f")
			require.NoError(t, os.WriteFile(path, []byte("FROM golang:1.0.0-a\nFROM node:1.0.0-b\n"), 0o600))

			changes, err := plan([]rule{
				{label: "golang", file: "f", re: dockerFromRe("golang"), version: "9.9.9", count: 1},
				{label: "node", file: "f", re: dockerFromRe("node"), version: "8.8.8", count: 1},
			}, root)

			require.NoError(t, err)
			assert.Equal(t, map[string]string{path: "FROM golang:9.9.9-a\nFROM node:8.8.8-b\n"}, changes)
		})

		// 一致しているファイルを changes に入れると、atomicwrite が無用に書き、apply の報告に
		// 出ないはずの名前が並ぶ。
		t.Run("一致しているファイルは changes に入れない", func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(root, "f"), []byte("FROM golang:1.27.1-a\n"), 0o600))

			changes, err := plan([]rule{
				{label: "golang", file: "f", re: dockerFromRe("golang"), version: "1.27.1", count: 1},
			}, root)

			require.NoError(t, err)
			assert.Empty(t, changes)
		})
	})

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

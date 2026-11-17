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

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/misetoml"
)

const soundMise = `min_version = "2026.6.0"

[env]
go = "should-be-ignored"

[tools]
go = "1.27.1"
node = "24.21.0"
"aqua:hashicorp/terraform" = "1.16.2"
"aqua:aws/aws-cli" = "2.36.40"
"aqua:pnpm/pnpm" = "12.17.0"
"npm:markdownlint-cli2" = "0.23.2"
"npm:@commitlint/cli" = "21.2.2"
`

// soundHostTools は、焼き込んだ版を両方持つレシピ。**行頭の `\t@` を含める** —— 置換で前置きが
// 落ちる欠陥は、前置きの無い入力では現れない。
const soundHostTools = `host-tools-install:
	@command -v mise >/dev/null 2>&1 || exit 1
	@mise install "aqua:hashicorp/terraform@1.16.2"
	@mise install "aqua:aws/aws-cli@2.36.40"
	@mise install "aqua:pnpm/pnpm@12.17.0"
	@mise reshim
`

// editorconfig-checker-disable -- raw string の fixture は埋め込んだ YAML / シェルの字下げを持つ。撤回: fixture を外部ファイルへ移したとき

// soundDockerfile は、写しをすべて持つ Dockerfile。**レシピの継続行をそのまま含める** ——
// 行頭の空白と行末の `; \` は、置換で前置きが落ちる欠陥が現れる唯一の場所である。
const soundDockerfile = `# FROM golang:9.9.9-bookworm は例示であって写しではない
FROM golang:1.27.1-bookworm@sha256:aaa AS builder
RUN set -eu; \
    declared_go="1.27.1"; \
    true
FROM golang:1.27.1-bookworm@sha256:aaa AS tools
FROM node:24.21.0-alpine@sha256:bbb AS node_tools_builder
FROM node:24.21.0-alpine@sha256:bbb AS node_tools
RUN set -eu; \
    declared_node="24.21.0"; \
    true
`

const soundPackageJSON = `{
  "private": true,
  "dependencies": {
    "@commitlint/cli": "21.2.2",
    "markdownlint-cli2": "0.23.2"
  }
}
`

// soundWorkspace は、窓と除外の一覧を持つ pnpm-workspace.yaml。
const soundWorkspace = `minimumReleaseAge: 10080
minimumReleaseAgeExclude:
  - '@commitlint/cli'
  - 'markdownlint-cli2'

strictDepBuilds: true
`

// editorconfig-checker-enable

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
	require.NoError(t, os.MkdirAll(filepath.Join(root, "docker", "tools", "node"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "docker", "tools", "node", "package.json"), []byte(soundPackageJSON), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "docker", "tools", "node", "pnpm-workspace.yaml"), []byte(soundWorkspace), 0o600))

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
		if trimmed := strings.TrimSpace(line); strings.HasPrefix(trimmed, "[") {
			section = strings.Trim(trimmed, "[]")
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

		// 起点にする理由は driftedHostTools の宣言と同じ —— この写し先からずれた入力が無いと、
		// **Dockerfile の焼き込み側へ apply が書き込む経路が一度も実行されない。**
		t.Run("apply は Dockerfile の照合値も揃える", func(t *testing.T) {
			t.Parallel()
			drifted := strings.NewReplacer(
				`declared_go="1.27.1"`, `declared_go="1.0.0"`,
				`declared_node="24.21.0"`, `declared_node="1.0.0"`,
			).Replace(soundDockerfile)
			require.NotEqual(t, soundDockerfile, drifted, "ずらせていない")
			root := newRepo(t, soundMise, drifted, soundGoMod)
			var out bytes.Buffer

			require.ErrorIs(t, run([]string{"check"}, root, &out), errDrift)
			require.NoError(t, run([]string{"apply"}, root, &out))
			assert.Equal(t, soundDockerfile, readAt(t, root, "docker", "tools", "Dockerfile"))
		})

		// 起点にする理由は driftedHostTools の宣言と同じ —— **package.json へ apply が書き込む
		// 経路と、check がその名前を報告する経路を実行する。**
		// 窓と除外は別の rule が持つ。1つの入力で両方をずらすと、片方の rule が死んでも他方が落とす。
		for name, drifted := range map[string]string{
			"窓":     strings.Replace(soundWorkspace, "minimumReleaseAge: 10080", "minimumReleaseAge: 1", 1),
			"除外の一覧": strings.Replace(soundWorkspace, "  - 'markdownlint-cli2'\n", "", 1),
		} {
			t.Run("apply は pnpm-workspace.yaml の"+name+"を揃え、check は書き換えない", func(t *testing.T) {
				t.Parallel()
				require.NotEqual(t, soundWorkspace, drifted, "ずらせていない")
				root := newRepo(t, soundMise, soundDockerfile, soundGoMod)
				ws := filepath.Join(root, "docker", "tools", "node", "pnpm-workspace.yaml")
				require.NoError(t, os.WriteFile(ws, []byte(drifted), 0o600))
				var out bytes.Buffer

				require.ErrorIs(t, run([]string{"check"}, root, &out), errDrift)
				assert.Contains(t, out.String(), "pnpm-workspace.yaml")
				assert.Equal(t, drifted, readAt(t, root, "docker", "tools", "node", "pnpm-workspace.yaml"))
				require.NoError(t, run([]string{"apply"}, root, &out))
				assert.Equal(t, soundWorkspace, readAt(t, root, "docker", "tools", "node", "pnpm-workspace.yaml"))
			})
		}

		t.Run("apply は package.json の npm の版も揃える", func(t *testing.T) {
			t.Parallel()
			drifted := strings.NewReplacer(
				`"markdownlint-cli2": "0.23.2"`, `"markdownlint-cli2": "1.0.0"`,
				`"@commitlint/cli": "21.2.2"`, `"@commitlint/cli": "1.0.0"`,
			).Replace(soundPackageJSON)
			require.NotEqual(t, soundPackageJSON, drifted, "ずらせていない")
			root := newRepo(t, soundMise, soundDockerfile, soundGoMod)
			pkg := filepath.Join(root, "docker", "tools", "node", "package.json")
			require.NoError(t, os.WriteFile(pkg, []byte(drifted), 0o600))
			var out bytes.Buffer

			require.ErrorIs(t, run([]string{"check"}, root, &out), errDrift)
			assert.Equal(t, drifted, readAt(t, root, "docker", "tools", "node", "package.json"))
			assert.Contains(t, out.String(), "package.json")
			require.NoError(t, run([]string{"apply"}, root, &out))
			assert.Equal(t, soundPackageJSON, readAt(t, root, "docker", "tools", "node", "package.json"))
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
			assert.Contains(t, out.String(), "pnpm 12.17.0")
			assert.Contains(t, out.String(), "markdownlint 0.23.2")
			assert.Contains(t, out.String(), "commitlint 21.2.2")
			assert.Contains(t, out.String(), "窓 7 日")
		})

		// 起点にする理由は driftedHostTools の宣言と同じ。
		t.Run("apply は host-tools.mk の pnpm の版も揃える", func(t *testing.T) {
			t.Parallel()
			drifted := strings.ReplaceAll(soundHostTools, "pnpm@12.17.0", "pnpm@1.0.0")
			root := newRepo(t, soundMise, soundDockerfile, soundGoMod, drifted)
			var out bytes.Buffer

			require.ErrorIs(t, run([]string{"check"}, root, &out), errDrift)
			require.NoError(t, run([]string{"apply"}, root, &out))
			assert.Equal(t, soundHostTools, readAt(t, root, ".makefiles", "host-tools.mk"))
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

		// 部分適用が残らない理由は applyAll の宣言が持つ。
		t.Run("写しの1つが壊れていれば他も書き換えない", func(t *testing.T) {
			t.Parallel()
			drifted := strings.ReplaceAll(soundDockerfile, "golang:1.27.1", "golang:1.26.0")
			// go ディレクティブを失った go.mod。
			root := newRepo(t, soundMise, drifted, "module example\n")
			var out bytes.Buffer

			require.Error(t, run([]string{"apply"}, root, &out))
			assert.Equal(t, drifted, readAt(t, root, "docker", "tools", "Dockerfile"))
		})

		for _, name := range []string{"package.json", "pnpm-workspace.yaml"} {
			t.Run(name+" が無ければ名指しでエラーにする", func(t *testing.T) {
				t.Parallel()
				root := newRepo(t, soundMise, soundDockerfile, soundGoMod)
				require.NoError(t, os.Remove(filepath.Join(root, "docker", "tools", "node", name)))
				var out bytes.Buffer

				err := run([]string{"check"}, root, &out)
				require.ErrorIs(t, err, os.ErrNotExist)
				assert.Contains(t, err.Error(), name)
			})
		}

		// 写し先の行が消えた・形が変わった入力を「一致」と報告しない。
		type broken struct {
			file    []string
			replace [2]string
		}
		for name, b := range map[string]broken{
			"pnpm の窓の行が無い":                {[]string{"docker", "tools", "node", "pnpm-workspace.yaml"}, [2]string{"minimumReleaseAge: 10080\n", ""}},
			"除外の一覧の見出しが無い":                {[]string{"docker", "tools", "node", "pnpm-workspace.yaml"}, [2]string{"minimumReleaseAgeExclude:\n", ""}},
			"package.json の依存の書式が違う":      {[]string{"docker", "tools", "node", "package.json"}, [2]string{`"markdownlint-cli2": "`, `"markdownlint-cli2" : "`}},
			"host-tools.mk の pnpm の導入が無い": {[]string{".makefiles", "host-tools.mk"}, [2]string{"\t@mise install \"aqua:pnpm/pnpm@12.17.0\"\n", ""}},
			"node イメージの FROM が1件しか無い":     {[]string{"docker", "tools", "Dockerfile"}, [2]string{"FROM node:24.21.0-alpine@sha256:bbb AS node_tools_builder\n", ""}},
		} {
			t.Run(name+"なら errShape で落とし、何も書かない", func(t *testing.T) {
				t.Parallel()
				root := newRepo(t, soundMise, strings.ReplaceAll(soundDockerfile, "golang:1.27.1", "golang:1.26.0"), soundGoMod)
				path := filepath.Join(append([]string{root}, b.file...)...)
				before := readAt(t, root, b.file...)
				after := strings.Replace(before, b.replace[0], b.replace[1], 1)
				require.NotEqual(t, before, after, "壊せていない")
				require.NoError(t, os.WriteFile(path, []byte(after), 0o600))
				var out bytes.Buffer

				require.ErrorIs(t, run([]string{"apply"}, root, &out), errShape)
				assert.Contains(t, readAt(t, root, "docker", "tools", "Dockerfile"), "golang:1.26.0")
			})
		}
	})
}

func Test_versionPattern(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		// 捕獲群を持たないことの意味（ずれると何が起きるか）は versionPattern の宣言が持つ。
		t.Run("捕獲群を持たない", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, 0, regexp.MustCompile(versionPattern).NumSubexp())
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

		// 桁の並びは versionPattern が持つ。3桁だけを試していると、そこが絞られても気づけない
		// —— 同じ理由で dockerFromRe と miseInstallRe のテストも1〜3桁を確かめる。
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
				Go: "1.27.1", Node: "24.21.0", Terraform: "1.16.2", AWSCLI: "2.36.40", Pnpm: "12.17.0",
				Markdownlint: "0.23.2", Commitlint: "21.2.2",
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

		// tool option 付きの宣言も mise では正しい。読めないと、宣言が在るのに「無い」と
		// 報告する —— 直す先が宣言の側に無いエラーになる。
		t.Run("tool option 付きの宣言を読む", func(t *testing.T) {
			t.Parallel()
			src := strings.Replace(soundMise,
				`"aqua:aws/aws-cli" = "2.36.40"`, `"aqua:aws/aws-cli" = { version = "2.36.40" }`, 1)
			root := newRepo(t, src, soundDockerfile, soundGoMod)
			got, err := parseMise(filepath.Join(root, miseFile))
			require.NoError(t, err)
			assert.Equal(t, "2.36.40", got.AWSCLI)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		// 空のまま進むとどうなるかは parseMise の当該チェックが持つ。1つだけ落とす方針は
		// miseWithout の宣言が持つ。
		for _, key := range []string{"go", "node", terraformTool, awsCLITool, pnpmTool} {
			t.Run(key+" が無ければエラーにする", func(t *testing.T) {
				t.Parallel()
				root := newRepo(t, miseWithout(t, key), soundDockerfile, soundGoMod)
				_, err := parseMise(filepath.Join(root, miseFile))
				require.ErrorIs(t, err, errShape)
				assert.Contains(t, err.Error(), key, "欠落した宣言の名前を報告していない")
			})
		}

		// **[tools] に解釈できない行が在れば、欠落ではなく行そのものを名指して落ちる。**
		// 判定は misetoml が持つ。ここが固定するのは、その落ち方が parseMise を通しても
		// 保たれること —— 握り潰して「宣言が無い」へ丸めると、直す先が分からなくなる。
		arrayed := strings.Replace(soundMise,
			`"aqua:hashicorp/terraform" = "1.16.2"`, `"aqua:hashicorp/terraform" = ["1.16.2", "1.9.0"]`, 1)
		multiline := strings.Replace(soundMise,
			`"aqua:hashicorp/terraform" = "1.16.2"`, "\"aqua:hashicorp/terraform\" = [\n  \"1.16.2\",\n]", 1)

		for name, src := range map[string]string{
			"terraform が配列":    arrayed,
			"terraform が複数行配列": multiline,
			"キーの開き引用符だけがある": strings.Replace(soundMise,
				`"aqua:aws/aws-cli" = "2.36.40"`, `"aqua:aws/aws-cli = "2.36.40"`, 1),
			"キーの閉じ引用符だけがある": strings.Replace(soundMise,
				`"aqua:aws/aws-cli" = "2.36.40"`, `aqua:aws/aws-cli" = "2.36.40"`, 1),
		} {
			t.Run(name+"なら行を名指してエラーにする", func(t *testing.T) {
				t.Parallel()
				root := newRepo(t, src, soundDockerfile, soundGoMod)
				_, err := parseMise(filepath.Join(root, miseFile))
				require.ErrorIs(t, err, misetoml.ErrInvalidLine)
			})
		}

		// 行は読めたが、要求するキーがそもそも [tools] に無い場合。
		for name, src := range map[string]string{
			"tools が無い": "[env]\ngo = \"1.27.1\"\n",
			"空":         "",
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

		// 除外の一覧は版ではなく範囲ごと置き換える。要素が増えても減っても、宣言どおりの一覧へ戻す。
		t.Run("block は一致した範囲を丸ごと置き換える", func(t *testing.T) {
			t.Parallel()
			br := rule{label: "除外", file: "w", blockKey: excludeKey, block: excludeBlock(), count: 1}
			src := "minimumReleaseAge: 1\nminimumReleaseAgeExclude:\n  - stale\n  - '@commitlint/cli'\n  - other\n\nstrictDepBuilds: true\n"
			got, err := applyRule(br, src)
			require.NoError(t, err)
			assert.Equal(t, "minimumReleaseAge: 1\n"+excludeBlock()+"\nstrictDepBuilds: true\n", got)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for name, src := range map[string]string{
			"見出しが2件ある": "minimumReleaseAgeExclude:\n  - a\nminimumReleaseAgeExclude:\n  - b\n",
			"キーが引用符付き": "\"minimumReleaseAgeExclude\":\n  - a\n",
		} {
			t.Run("block の"+name+"ならエラーにする", func(t *testing.T) {
				t.Parallel()
				br := rule{label: "除外", file: "w", blockKey: excludeKey, block: excludeBlock(), count: 1}
				_, err := applyRule(br, src)
				require.ErrorIs(t, err, errShape)
			})
		}

		// 一覧の見出しが消えた入力を「一致」と報告しない。
		t.Run("block の範囲が見つからなければエラーにする", func(t *testing.T) {
			t.Parallel()
			br := rule{label: "除外", file: "w", blockKey: excludeKey, block: excludeBlock(), count: 1}
			_, err := applyRule(br, "minimumReleaseAge: 1\nstrictDepBuilds: true\n")
			require.ErrorIs(t, err, errShape)
		})

		// 下限ではなく厳密な件数である理由は rule.count の宣言が持つ。
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

		// 宣言側の版を検める理由は applyRule の当該チェックが持つ。ここはそれを外したときに
		// 何が通ってしまうかを固定する。
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

		// 捕獲群が無いとき何が起きるかは applyRule の当該チェックが持つ。
		t.Run("写し先の正規表現が捕獲群を持たなければエラーにする", func(t *testing.T) {
			t.Parallel()

			noGroup := r
			noGroup.re = regexp.MustCompile(`FROM golang:` + versionPattern + `-bookworm`)
			noGroup.count = 1
			_, err := applyRule(noGroup, "FROM golang:1.0.0-bookworm\n")
			require.ErrorIs(t, err, errShape)
		})

		// 空の版で書き換えたときに何が起きるかは applyRule の当該チェックが持つ。
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

		// 改行を文字クラスから落としたときに何が起きるかは dockerFromRe の宣言が持つ。
		t.Run("コメント行を対象にしない", func(t *testing.T) {
			t.Parallel()
			src := "# FROM golang:9.9.9-bookworm\nFROM golang:1.0.0-bookworm\n"
			assert.Len(t, dockerFromRe("golang").FindAllString(src, -1), 1)
		})

		t.Run("別のイメージを掴まない", func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, dockerFromRe("node").FindAllString("FROM golang:1.0.0-a\n", -1))
		})

		t.Run("前置されたレジストリを掴まない", func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, dockerFromRe("golang").FindAllString("FROM docker.io/library/golang:1.0.0-a\n", -1))
		})

		// 3桁だけを試すと絞られても気づけない理由は Test_goDirectiveRe の
		// 「1〜3桁の版に一致する」ケースが持つ。
		t.Run("1桁・2桁の版にも一致する", func(t *testing.T) {
			t.Parallel()
			src := "FROM golang:1-bookworm\nFROM golang:1.27-bookworm\n"
			assert.Len(t, dockerFromRe("golang").FindAllString(src, -1), 2)
		})

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

		// 前置きを群の外で消費したときに何が起きるかは bakedRe の宣言が持つ。
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

		// 3桁だけを試すと絞られても気づけない理由は Test_goDirectiveRe の
		// 「1〜3桁の版に一致する」ケースが持つ。
		t.Run("1桁・2桁の版にも一致する", func(t *testing.T) {
			t.Parallel()
			src := "\t@mise install \"aqua:hashicorp/terraform@1\"\n\t@mise install \"aqua:hashicorp/terraform@1.16\"\n"
			assert.Len(t, re.FindAllString(src, -1), 2)
		})

		// 閉じを錨にする理由は Test_bakedRe の「閉じの無い行を掴まない」ケースが持つ。
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

			// 書き込みは昇順（docker/... が先、scripts/... が後）。この順序が変わると、
			// 後者を塞ぐこの手法自体が成立しなくなる。
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

func Test_shellVarRe(t *testing.T) {
	t.Parallel()

	re := shellVarRe("declared_go")

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		// 前置きを群の外で消費したときに何が起きるかは bakedRe の宣言が持つ。
		t.Run("継続行の前置きと行末を保ったまま版だけ差し替える", func(t *testing.T) {
			t.Parallel()
			r := rule{label: "go の照合値", file: "Dockerfile", re: re, version: "1.27.1", count: 1}
			got, err := applyRule(r, "    declared_go=\"1.0.0\"; \\\n")
			require.NoError(t, err)
			assert.Equal(t, "    declared_go=\"1.27.1\"; \\\n", got)
		})

		t.Run("コメント行を対象にしない", func(t *testing.T) {
			t.Parallel()
			src := "# declared_go=\"9.9.9\"\n    declared_go=\"1.0.0\"; \\\n"
			assert.Len(t, re.FindAllString(src, -1), 1)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		// 名前が前方一致で掴めると、declared_node の行を go の照合値として書き換える。
		t.Run("別の名前の変数を掴まない", func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, re.FindAllString("    declared_node=\"24.21.0\"; \\\n", -1))
		})
	})
}

func Test_yamlValueSpans(t *testing.T) {
	t.Parallel()

	const key = "k"
	value := func(t *testing.T, src string) []string {
		t.Helper()
		var got []string
		for _, sp := range yamlValueSpans(src, key) {
			got = append(got, src[sp[0]:sp[1]])
		}

		return got
	}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		// 要素の形で終わりを決めると、間に挟まった空行やコメントの後ろの要素が範囲の外に残る。
		tests := map[string]struct{ src, want string }{
			"字下げした要素":         {"k:\n  - a\n  - b\nnext: 1\n", "k:\n  - a\n  - b\n"},
			"要素の間のコメントと空行":    {"k:\n  - a\n# c\n\n  # d\n  - evil\nnext: 1\n", "k:\n  - a\n# c\n\n  # d\n  - evil\n"},
			"字下げの無い要素":        {"k:\n- a\n- stale\nnext: 1\n", "k:\n- a\n- stale\n"},
			"末尾の空行とコメントは残す":   {"k:\n  - a\n\n# next の説明\nnext: 1\n", "k:\n  - a\n"},
			"末尾に改行が無い":        {"k:\n  - a\n  - b", "k:\n  - a\n  - b"},
			"フロースタイルは見出しの行だけ": {"k: [a, b]\nnext: 1\n", "k: [a, b]\n"},
		}
		for name, tc := range tests {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				assert.Equal(t, []string{tc.want}, value(t, tc.src))
			})
		}

		t.Run("出現ごとに返す", func(t *testing.T) {
			t.Parallel()
			assert.Len(t, yamlValueSpans("k:\n  - a\nk:\n  - b\n", key), 2)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for name, src := range map[string]string{
			"キーが無い":     "other:\n  - a\n",
			"字下げされたキー":  "  k:\n  - a\n",
			"名前の前方だけ一致": "kk:\n  - a\n",
		} {
			t.Run(name+"なら返さない", func(t *testing.T) {
				t.Parallel()
				assert.Empty(t, yamlValueSpans(src, key))
			})
		}
	})
}

func Test_npmPkgs(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("package.json へ版を書く道具を名前順に返す", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, []string{commitlintPkg, markdownlintPkg}, npmPkgs())
		})
	})
}

func Test_checkNodeToolsDeps(t *testing.T) {
	t.Parallel()

	write := func(t *testing.T, body string) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "package.json")
		require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

		return path
	}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, checkNodeToolsDeps(write(t, soundPackageJSON)))
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		// 宣言に無い依存は、mise.toml の版の宣言も tool-cooldown の gate も経ずにイメージへ入る。
		for name, body := range map[string]string{
			"宣言に無い依存がある":          `{"dependencies": {"@commitlint/cli": "1", "markdownlint-cli2": "1", "evil": "1"}}`,
			"宣言した依存が欠けている":        `{"dependencies": {"@commitlint/cli": "1"}}`,
			"dependencies が無い":    `{"private": true}`,
			"devDependencies がある": `{"dependencies": {"@commitlint/cli": "1", "markdownlint-cli2": "1"}, "devDependencies": {"x": "1"}}`,
			"JSON でない":            `not json`,
		} {
			t.Run(name+"なら errShape", func(t *testing.T) {
				t.Parallel()
				require.ErrorIs(t, checkNodeToolsDeps(write(t, body)), errShape)
			})
		}

		t.Run("読めなければエラー", func(t *testing.T) {
			t.Parallel()
			require.ErrorIs(t, checkNodeToolsDeps(filepath.Join(t.TempDir(), "package.json")), os.ErrNotExist)
		})
	})
}

func Test_excludeBlock(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		// package.json へ版を書く道具と同じ名前を、並びを決めて引用符付きで並べる。
		t.Run("npm の道具を名前順に並べる", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t,
				"minimumReleaseAgeExclude:\n  - '@commitlint/cli'\n  - 'markdownlint-cli2'\n",
				excludeBlock())
		})
	})
}

func Test_npmDepRe(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("scope 付きのパッケージ名を掴み、行末のカンマを残す", func(t *testing.T) {
			t.Parallel()
			r := rule{
				label: "commitlint", file: "package.json",
				re: npmDepRe(commitlintPkg), version: "21.2.2", count: 1,
			}
			got, err := applyRule(r, "    \"@commitlint/cli\": \"1.0.0\",\n")
			require.NoError(t, err)
			assert.Equal(t, "    \"@commitlint/cli\": \"21.2.2\",\n", got)
		})

		// 名前が後方一致で掴めると、`@scope/markdownlint-cli2` のような別のパッケージを書き換える。
		t.Run("名前を末尾に含む別パッケージを掴まない", func(t *testing.T) {
			t.Parallel()
			src := "    \"@x/markdownlint-cli2-extra\": \"0.1.0\",\n    \"markdownlint-cli2\": \"0.23.2\"\n"
			assert.Len(t, npmDepRe(markdownlintPkg).FindAllString(src, -1), 1)
		})
	})
}

func Test_rules(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		// 表が空のときに「一致」を返してしまう理由は plan の当該ガードが持つ。
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
			for _, r := range rules(declared{
				Go: "1", Node: "2", Terraform: "3", AWSCLI: "4", Markdownlint: "5", Commitlint: "6", Pnpm: "7",
			}) {
				got[r.label] = target{file: r.file, version: r.version, count: r.count}
			}
			assert.Equal(t, map[string]target{
				"golang イメージ":      {file: "docker/tools/Dockerfile", version: "1", count: 2},
				"node イメージ":        {file: "docker/tools/Dockerfile", version: "2", count: 2},
				"go ディレクティブ":       {file: "scripts/go.mod", version: "1", count: 1},
				"terraform の導入":    {file: ".makefiles/host-tools.mk", version: "3", count: 1},
				"AWS CLI の導入":      {file: ".makefiles/host-tools.mk", version: "4", count: 1},
				"pnpm の導入":         {file: ".makefiles/host-tools.mk", version: "7", count: 1},
				"go の照合値":          {file: "docker/tools/Dockerfile", version: "1", count: 1},
				"node の照合値":        {file: "docker/tools/Dockerfile", version: "2", count: 1},
				"markdownlint の導入": {file: "docker/tools/node/package.json", version: "5", count: 1},
				"commitlint の導入":   {file: "docker/tools/node/package.json", version: "6", count: 1},
				"pnpm の供給網の窓":      {file: "docker/tools/node/pnpm-workspace.yaml", version: "10080", count: 1},
				"pnpm の窓の除外":       {file: "docker/tools/node/pnpm-workspace.yaml", count: 1},
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

		// 理由は plan の当該ガードが持つ。
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

func Test_bakedRe(t *testing.T) {
	t.Parallel()

	re := bakedRe(`v="`, `"`)

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		// 群が欠けたときに何が起きるかは applyRule の当該チェックが持つ。
		t.Run("前置きと閉じの2つの捕獲群を持つ", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, 2, re.NumSubexp())
		})

		t.Run("前置きと行末を保ったまま版だけ差し替える", func(t *testing.T) {
			t.Parallel()
			got := re.ReplaceAllString("\t@v=\"1.0.0\"; \\\n", "${1}1.27.1${2}")
			assert.Equal(t, "\t@v=\"1.27.1\"; \\\n", got)
		})

		// 3桁だけを試すと絞られても気づけない理由は Test_goDirectiveRe の
		// 「1〜3桁の版に一致する」ケースが持つ。
		t.Run("1〜3桁の版に一致する", func(t *testing.T) {
			t.Parallel()
			assert.Len(t, re.FindAllString("v=\"1\"\nv=\"1.27\"\nv=\"1.27.1\"\n", -1), 3)
		})

		// 錨と閉じはリテラルとして扱う。中和しないと、閉じの `)` は括弧の不一致で
		// コンパイルに失敗し、錨の `.` は任意の1文字に化けて別の行を掴む。
		t.Run("錨と閉じの正規表現メタ文字を中和する", func(t *testing.T) {
			t.Parallel()
			meta := bakedRe(`v.n(`, `)`)
			assert.Len(t, meta.FindAllString("v.n(1.27.1)\n", -1), 1)
			assert.Empty(t, meta.FindAllString("vxn(1.27.1)\n", -1))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("コメント行を掴まない", func(t *testing.T) {
			t.Parallel()
			assert.Len(t, re.FindAllString("# v=\"9.9.9\"\nv=\"1.0.0\"\n", -1), 1)
		})

		// 閉じが錨である。無い行を掴むと、版の終わりを決められないまま置換する。
		t.Run("閉じの無い行を掴まない", func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, re.FindAllString("v=\"1.27.1\n", -1))
		})

		t.Run("版の形をしていない値を掴まない", func(t *testing.T) {
			t.Parallel()
			assert.Empty(t, re.FindAllString("v=\"latest\"\n", -1))
		})
	})
}

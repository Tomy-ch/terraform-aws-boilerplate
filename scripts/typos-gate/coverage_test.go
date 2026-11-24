package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun/tooltest"
)

const coverageConfig = `[files]
ignore-hidden = false
extend-exclude = [".git/", "**/.terraform.lock.hcl", "scripts/go.sum"]
`

// isolated は、隔離した git の env の代わりに、印の GIT_DIR を持つ env を返します。
func isolated() ([]string, error) {
	return []string{"PATH=" + os.Getenv("PATH"), "GIT_DIR=/isolated"}, nil
}

var isolatedEnv = []string{"PATH=" + os.Getenv("PATH"), "GIT_DIR=/isolated"}

func cover(cfg string, out *bytes.Buffer, tool toolFunc, env envFunc) error {
	return runCoverage(".typos.toml", []byte(cfg), out, tool, env)
}

func Test_runCoverage(t *testing.T) {
	t.Parallel()

	t.Run("除外と自身の設定を除いた追跡ファイルがすべて走査の対象なら通る", func(t *testing.T) {
		t.Parallel()
		f := tooltest.New(t)
		f.Set(stepLsFiles, "out", "a.md\x00.typos.toml\x00x/.terraform.lock.hcl\x00scripts/go.sum\x00.github/w.yaml\x00")
		f.Set(stepFiles, "out", "./a.md\n./.github/w.yaml\n./untracked.md\n")
		var out bytes.Buffer
		require.NoError(t, runCoverage("./.typos.toml", []byte(coverageConfig), &out, f.Tool(), isolated))
		assert.Contains(t, out.String(), "追跡ファイル 2 件")
		calls := f.Calls()
		require.Len(t, calls, 3)
		assert.Equal(t, realCwd(t)+" "+stepFiles+" ", calls[0], "--files はリポジトリ直下（cwd）で走る")
		assert.True(t, strings.HasSuffix(calls[1], " "+stepLsFiles+" /isolated"), "git には隔離した env を渡す: %s", calls[1])
		assert.True(t, strings.HasSuffix(calls[2], " "+stepLsDeleted+" /isolated"), "git には隔離した env を渡す: %s", calls[2])
	})

	t.Run("作業ツリーから消えた追跡ファイルは引き、件数を出す", func(t *testing.T) {
		t.Parallel()
		f := tooltest.New(t)
		f.Set(stepLsFiles, "out", "a.md\x00gone.md\x00x/gone two.md\x00")
		f.Set(stepLsDeleted, "out", "gone.md\x00x/gone two.md\x00")
		f.Set(stepFiles, "out", "./a.md\n")
		var out bytes.Buffer
		require.NoError(t, cover(coverageConfig, &out, f.Tool(), isolated))
		assert.Contains(t, out.String(), "追跡ファイル 1 件")
		assert.Contains(t, out.String(), "作業ツリーから消えた 2 件")
	})

	t.Run("消えた追跡ファイルを引いて0件なら errNoInput", func(t *testing.T) {
		t.Parallel()
		f := tooltest.New(t)
		f.Set(stepLsFiles, "out", "gone.md\x00")
		f.Set(stepLsDeleted, "out", "gone.md\x00")
		f.Set(stepFiles, "out", "./x\n")
		require.ErrorIs(t, cover(coverageConfig, new(bytes.Buffer), f.Tool(), isolated), errNoInput)
	})

	t.Run("git ls-files --deleted が stderr にだけ出したパスは引かない", func(t *testing.T) {
		t.Parallel()
		f := tooltest.New(t)
		f.Set(stepLsFiles, "out", "a.md\x00gone.md\x00")
		f.Set(stepLsDeleted, "err", "gone.md\x00")
		f.Set(stepFiles, "out", "./a.md\n")
		require.ErrorIs(t, cover(coverageConfig, new(bytes.Buffer), f.Tool(), isolated), errUncovered)
	})

	t.Run("git ls-files が rc≠0 なら --deleted を起動しない", func(t *testing.T) {
		t.Parallel()
		f := tooltest.New(t)
		f.Set(stepLsFiles, "rc", "128")
		f.Set(stepFiles, "out", "./a\n")
		require.ErrorIs(t, cover(coverageConfig, new(bytes.Buffer), f.Tool(), isolated), errTool)
		assert.Len(t, f.Calls(), 2)
	})

	t.Run("走査の対象から外れた追跡ファイルを先頭20件まで出し errUncovered", func(t *testing.T) {
		t.Parallel()
		f := tooltest.New(t)
		var tracked []string
		for i := range 25 {
			tracked = append(tracked, fmt.Sprintf("f%02d", i))
		}
		f.Set(stepLsFiles, "out", strings.Join(tracked, "\x00")+"\x00")
		f.Set(stepFiles, "out", "./f00\n")
		var out bytes.Buffer
		err := cover(coverageConfig, &out, f.Tool(), isolated)
		require.ErrorIs(t, err, errUncovered)
		assert.Contains(t, err.Error(), "24 件")
		assert.Contains(t, out.String(), "f20\n")
		assert.NotContains(t, out.String(), "f21")
		assert.NotContains(t, out.String(), "f00")
		assert.NotContains(t, out.String(), "✅")
	})

	t.Run("除外を引いた追跡ファイルが0件なら errNoInput", func(t *testing.T) {
		t.Parallel()
		for name, listed := range map[string]string{"空": "", "除外だけ": "scripts/go.sum\x00a/.terraform.lock.hcl\x00"} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				f := tooltest.New(t)
				f.Set(stepLsFiles, "out", listed)
				f.Set(stepFiles, "out", "./x\n")
				require.ErrorIs(t, cover(coverageConfig, new(bytes.Buffer), f.Tool(), isolated), errNoInput)
			})
		}
	})

	for name, c := range map[string]struct{ step, suffix, body string }{
		"typos --files が rc≠0":             {stepFiles, "rc", "64"},
		"git ls-files が rc≠0":              {stepLsFiles, "rc", "128"},
		"git ls-files がシグナルで終わる":           {stepLsFiles, "kill", ""},
		"git ls-files --deleted が rc≠0":    {stepLsDeleted, "rc", "128"},
		"git ls-files --deleted がシグナルで終わる": {stepLsDeleted, "kill", ""},
	} {
		t.Run(name+"なら errTool", func(t *testing.T) {
			t.Parallel()
			f := tooltest.New(t)
			f.Set(stepLsFiles, "out", "a\x00")
			f.Set(stepFiles, "out", "./a\n")
			f.Set(c.step, c.suffix, c.body)
			require.ErrorIs(t, cover(coverageConfig, new(bytes.Buffer), f.Tool(), isolated), errTool)
		})
	}

	t.Run("typos を起動できなければ errTool", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, cover(coverageConfig, new(bytes.Buffer), tooltest.Missing, isolated), errTool)
	})

	t.Run("git の隔離の一覧を得られなければ、道具を起動せずに errTool", func(t *testing.T) {
		t.Parallel()
		f := tooltest.New(t)
		err := cover(coverageConfig, new(bytes.Buffer), f.Tool(), func() ([]string, error) { return nil, errors.New("boom") })
		require.ErrorIs(t, err, errTool)
		assert.Empty(t, f.Calls())
	})

	t.Run("設定を解釈できなければ、道具を起動せずに errRead", func(t *testing.T) {
		t.Parallel()
		f := tooltest.New(t)
		require.ErrorIs(t, cover("[files]\nextend-exclude = \"x\"\n", new(bytes.Buffer), f.Tool(), isolated), errRead)
		assert.Empty(t, f.Calls())
	})

	for name, c := range map[string]struct{ cfg, declared string }{
		"除外が多い（scripts/ を足した）": {"[files]\nextend-exclude = [\".git/\", \"**/.terraform.lock.hcl\", \"scripts/go.sum\", \"scripts/\"]\n", `["**/.terraform.lock.hcl" ".git/" "scripts/" "scripts/go.sum"]`},
		"除外が足りない":              {"[files]\nextend-exclude = [\".git/\", \"scripts/go.sum\"]\n", `[".git/" "scripts/go.sum"]`},
		"extend-exclude が無い":   {"[files]\nignore-hidden = false\n", `[]`},
		"解釈しない形の除外":            {"[files]\nextend-exclude = [\"!a\"]\n", `["!a"]`},
	} {
		t.Run(name+"なら、両方の一覧を名指して道具を起動せずに errExcludes", func(t *testing.T) {
			t.Parallel()
			f := tooltest.New(t)
			err := cover(c.cfg, new(bytes.Buffer), f.Tool(), isolated)
			require.ErrorIs(t, err, errExcludes)
			assert.Contains(t, err.Error(), ".typos.toml: "+c.declared)
			assert.Contains(t, err.Error(), `typos-gate: ["**/.terraform.lock.hcl" ".git/" "scripts/go.sum"]`)
			assert.Empty(t, f.Calls())
		})
	}

	t.Run("順序と重複が違っても同じ集合なら通る", func(t *testing.T) {
		t.Parallel()
		f := tooltest.New(t)
		f.Set(stepLsFiles, "out", "a\x00scripts/go.sum\x00")
		f.Set(stepFiles, "out", "./a\n")
		cfg := "[files]\nextend-exclude = [\"scripts/go.sum\", \".git/\", \"**/.terraform.lock.hcl\", \".git/\"]\n"
		require.NoError(t, cover(cfg, new(bytes.Buffer), f.Tool(), isolated))
	})

	for name, c := range map[string]struct{ filesOut, filesErr string }{
		"typos --files が rc=0 で何も出さない":    {},
		"typos --files が空白と改行だけ":          {filesOut: "  \n\n \n"},
		"typos --files が stderr にだけパスを出す": {filesErr: "./a.md\n"},
	} {
		t.Run(name+"なら、成功を出さずに errUncovered", func(t *testing.T) {
			t.Parallel()
			f := tooltest.New(t)
			f.Set(stepLsFiles, "out", "a.md\x00")
			f.Set(stepFiles, "out", c.filesOut)
			f.Set(stepFiles, "err", c.filesErr)
			var out bytes.Buffer
			require.ErrorIs(t, cover(coverageConfig, &out, f.Tool(), isolated), errUncovered)
			assert.NotContains(t, out.String(), "✅")
		})
	}

	for _, step := range []string{stepFiles, stepLsFiles, stepLsDeleted} {
		t.Run(step+" が rc≠0 なら、stderr にだけ出した診断を載せて errTool", func(t *testing.T) {
			t.Parallel()
			f := tooltest.New(t)
			f.Set(stepLsFiles, "out", "a\x00")
			f.Set(stepFiles, "out", "./a\n")
			f.Set(step, "err", "STDERR-ONLY\n")
			f.Set(step, "rc", "1")
			err := cover(coverageConfig, new(bytes.Buffer), f.Tool(), isolated)
			require.ErrorIs(t, err, errTool)
			assert.Contains(t, err.Error(), "STDERR-ONLY")
		})
	}

	t.Run("git ls-files が stderr にだけ出したパスは数えず errNoInput", func(t *testing.T) {
		t.Parallel()
		f := tooltest.New(t)
		f.Set(stepLsFiles, "err", "a.md\x00")
		f.Set(stepFiles, "out", "./a.md\n")
		require.ErrorIs(t, cover(coverageConfig, new(bytes.Buffer), f.Tool(), isolated), errNoInput)
	})

	t.Run("typos --files だけがシグナルで終われば、git を起動せずに errTool", func(t *testing.T) {
		t.Parallel()
		f := tooltest.New(t)
		f.Set(stepFiles, "kill", "")
		f.Set(stepLsFiles, "out", "a\x00")
		require.ErrorIs(t, cover(coverageConfig, new(bytes.Buffer), f.Tool(), isolated), errTool)
		assert.Len(t, f.Calls(), 1)
	})
}

func Test_sameExcludes(t *testing.T) {
	t.Parallel()

	t.Run("同じ集合なら nil", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, sameExcludes([]string{"b", "a", "b"}, []string{"a", "b"}))
	})

	for name, c := range map[string]struct {
		declared []string
		named    string
	}{
		"多い":   {[]string{"c", "a", "b"}, `.typos.toml: ["a" "b" "c"]`},
		"足りない": {[]string{"a"}, `.typos.toml: ["a"]`},
		"空":    {nil, `.typos.toml: []`},
	} {
		t.Run(name+"なら、両方の一覧を名指して errExcludes", func(t *testing.T) {
			t.Parallel()
			err := sameExcludes(c.declared, []string{"b", "a"})
			require.ErrorIs(t, err, errExcludes)
			assert.Contains(t, err.Error(), c.named)
			assert.Contains(t, err.Error(), `typos-gate: ["a" "b"]`)
		})
	}

	t.Run("期待の宣言は .typos.toml に足してよい形だけで、どれも解釈できる", func(t *testing.T) {
		t.Parallel()
		_, err := excluded("x", expectedExcludes)
		require.NoError(t, err)
	})
}

func Test_missingFiles(t *testing.T) {
	t.Parallel()

	t.Run("./ を外して比べ、want の順で欠けを返す", func(t *testing.T) {
		t.Parallel()
		got := missingFiles("./b\n./x/c\n\n", []string{"c", "b", "x/c", "a"})
		assert.Equal(t, []string{"c", "a"}, got)
	})

	t.Run("走査の対象が空なら want のすべて", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, []string{"a"}, missingFiles("", []string{"a"}))
	})

	t.Run("欠けが無ければ nil", func(t *testing.T) {
		t.Parallel()
		assert.Nil(t, missingFiles("./a\n", []string{"a"}))
	})
}

func Test_extendExcludes(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		cfg  string
		want []string
	}{
		"1行の配列": {coverageConfig, []string{".git/", "**/.terraform.lock.hcl", "scripts/go.sum"}},
		"複数行・コメント・末尾のカンマ": {"[files]\nextend-exclude = [ # 生成物\n  \"a/\", # x\n  'b[ch]',\n]\n", []string{"a/", "b[ch]"}},
		"空の配列":               {"[files]\nextend-exclude = []\n", nil},
		"extend-exclude が無い": {"[files]\nignore-hidden = false\n", nil},
		"直下の dotted key":     {"files.extend-exclude = [\"a\"]\n[default]\n", []string{"a"}},
		"節の名前の前後の空白":         {"[ files ]\nextend-exclude = [\"a\"]\n", []string{"a"}},
		"コメントの中の語は読まない":      {"# extend-exclude = [\"z\"]\n[files]\nextend-exclude = [\"a#b\"] # c\n", []string{"a#b"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := extendExcludes(c.cfg)
			require.NoError(t, err)
			assert.Equal(t, c.want, got)
		})
	}

	for name, cfg := range map[string]string{
		"[files] の外":      "[default]\nextend-exclude = [\"a\"]\n",
		"節の中の dotted key": "[default]\nfiles.extend-exclude = [\"a\"]\n",
		"重複":              "[files]\nextend-exclude = [\"a\"]\nextend-exclude = [\"b\"]\n",
		"閉じない配列":          "[files]\nextend-exclude = [\"a\",\n",
		"配列でない":           "[files]\nextend-exclude = \"a\"\n",
		"エスケープを含む文字列":     "[files]\nextend-exclude = [\"a\\\\b\"]\n",
		"文字列でない要素":        "[files]\nextend-exclude = [1]\n",
	} {
		t.Run(name+"なら errRead", func(t *testing.T) {
			t.Parallel()
			_, err := extendExcludes(cfg)
			require.ErrorIs(t, err, errRead)
		})
	}

	for name, c := range map[string]struct{ cfg, want string }{
		"[files] の外は、その行を名指す": {"# x\n[default]\nextend-exclude = [\"a\"]\n", "3 行目: [files] の外"},
		"重複は、2つ目の行を名指す":       {"[files]\nextend-exclude = [\"a\"]\n\nextend-exclude = [\"b\"]\n", "4 行目: extend-exclude が重複"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := extendExcludes(c.cfg)
			require.ErrorIs(t, err, errRead)
			assert.Contains(t, err.Error(), c.want)
		})
	}
}

func Test_checkExcludeLine(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		prefix, section string
		found           bool
		want            string
	}{
		"[files] の中は通る":                {"", "files", false, ""},
		"直下の files. は通る":               {"files.", "", false, ""},
		"[files] の外は errRead":          {"", "default", false, "3 行目: [files] の外の extend-exclude は解釈しません"},
		"直下の前置きなしは errRead":            {"", "", false, "3 行目: [files] の外の extend-exclude は解釈しません"},
		"節の中の files. は errRead":        {"files.", "default", false, "3 行目: [files] の外の extend-exclude は解釈しません"},
		"[files] の中の files. は errRead": {"files.", "files", false, "3 行目: [files] の外の extend-exclude は解釈しません"},
		"重複は errRead":                  {"", "files", true, "3 行目: extend-exclude が重複しています"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := checkExcludeLine(3, c.prefix, c.section, c.found)
			if c.want == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, errRead)
			assert.Contains(t, err.Error(), c.want)
		})
	}
}

func Test_stripComment(t *testing.T) {
	t.Parallel()

	for in, want := range map[string]string{
		"a # c":      "a ",
		`"a#b" # c`:  `"a#b" `,
		`'a#b'`:      `'a#b'`,
		`"it's" # c`: `"it's" `,
		`'a"b' # c`:  `'a"b' `,
		"no comment": "no comment",
	} {
		t.Run(in, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, want, stripComment(in))
		})
	}
}

func Test_parseStringArray(t *testing.T) {
	t.Parallel()

	t.Run("基本文字列とリテラル文字列を読む", func(t *testing.T) {
		t.Parallel()
		got, err := parseStringArray(` [ "a" , 'b',  ] `)
		require.NoError(t, err)
		assert.Equal(t, []string{"a", "b"}, got)
	})

	for name, v := range map[string]string{"括弧が無い": `"a"`, "カンマが無い": `["a" "b"]`, "裸の語": `[a]`} {
		t.Run(name+"なら errRead", func(t *testing.T) {
			t.Parallel()
			_, err := parseStringArray(v)
			require.ErrorIs(t, err, errRead)
		})
	}
}

func Test_excluded(t *testing.T) {
	t.Parallel()

	pats := []string{".git/", "**/.terraform.lock.hcl", "scripts/go.sum"}
	for p, want := range map[string]bool{
		".terraform.lock.hcl":            true,
		"usecases/x/.terraform.lock.hcl": true,
		"scripts/go.sum":                 true,
		"x/scripts/go.sum":               false,
		"scripts/go.mod":                 false,
		".git/config":                    true,
		".github/x":                      false,
	} {
		t.Run(p, func(t *testing.T) {
			t.Parallel()
			got, err := excluded(p, pats)
			require.NoError(t, err)
			assert.Equal(t, want, got)
		})
	}

	t.Run("解釈しないパターンは errRead", func(t *testing.T) {
		t.Parallel()
		_, err := excluded("a", []string{"a/**/b"})
		require.ErrorIs(t, err, errRead)
	})
}

func Test_matchGlob(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		pat, p string
		want   bool
	}{
		"名前だけはどの深さにも当たる":              {"*.lock", "a/b/x.lock", true},
		"名前だけはディレクトリにも当たる":            {"vendor", "a/vendor/x", true},
		"末尾の / はディレクトリだけ":             {"build/", "a/build/x", true},
		"末尾の / はファイルに当たらない":           {"build/", "a/build", false},
		"途中の / は直下に固定":                {"a/b", "a/b", true},
		"途中の / は深い位置に当たらない":           {"a/b", "x/a/b", false},
		"途中の / はディレクトリの下も外す":          {"a/b", "a/b/c", true},
		"先頭の / は直下に固定":                {"/x", "x", true},
		"先頭の / は深い位置に当たらない":           {"/x", "d/x", false},
		"先頭の **/ と / を含む残りはどの深さにも当たる": {"**/a/b", "x/y/a/b", true},
		"文字の集合": {"go.[sm]*", "scripts/go.sum", true},
		"当たらない": {"*.md", "a/b.txt", false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := matchGlob(c.pat, c.p)
			require.NoError(t, err)
			assert.Equal(t, c.want, got)
		})
	}

	for name, pat := range map[string]string{
		"空": "", "/ だけ": "/", "否定": "!a", "エスケープ": `a\*`, "途中の **": "a/**/b", "末尾の **": "a/**", "** だけ": "**/", "壊れた集合": "a[",
	} {
		t.Run(name+"なら errRead", func(t *testing.T) {
			t.Parallel()
			_, err := matchGlob(pat, "a")
			require.ErrorIs(t, err, errRead)
		})
	}
}

func Test_gitList(t *testing.T) {
	t.Parallel()

	t.Run("リポジトリ直下で段の git を隔離した env で起動し、stdout だけを返す", func(t *testing.T) {
		t.Parallel()
		f := tooltest.New(t)
		f.Set(stepLsDeleted, "out", "a\x00b\x00")
		f.Set(stepLsDeleted, "err", "STDERR-ONLY\n")
		got, err := gitList(f.Tool(), isolatedEnv, stepLsDeleted)
		require.NoError(t, err)
		assert.Equal(t, "a\x00b\x00", got)
		calls := f.Calls()
		require.Len(t, calls, 1)
		assert.True(t, strings.HasSuffix(calls[0], " "+stepLsDeleted+" /isolated"), calls[0])
	})

	t.Run("rc≠0 なら、段と出力を名指して errTool", func(t *testing.T) {
		t.Parallel()
		f := tooltest.New(t)
		f.Set(stepLsDeleted, "err", "fatal: x\n")
		f.Set(stepLsDeleted, "rc", "128")
		_, err := gitList(f.Tool(), isolatedEnv, stepLsDeleted)
		require.ErrorIs(t, err, errTool)
		assert.Contains(t, err.Error(), stepLsDeleted+" が rc=128")
		assert.Contains(t, err.Error(), "fatal: x")
	})

	t.Run("シグナルで終わるか起動できなければ errTool", func(t *testing.T) {
		t.Parallel()
		f := tooltest.New(t)
		f.Set(stepLsDeleted, "kill", "")
		_, err := gitList(f.Tool(), isolatedEnv, stepLsDeleted)
		require.ErrorIs(t, err, errTool)
		_, err = gitList(tooltest.Missing, isolatedEnv, stepLsDeleted)
		require.ErrorIs(t, err, errTool)
	})
}

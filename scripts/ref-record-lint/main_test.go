package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/lintreport"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/mdscan"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/testenv"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const validSection = `## 公式実装の参照（ADR-0206 決定7・8）

参照日: 2026-11-19（一部は 2026-11-20）、AWS Provider v6.70.0 の文書

- 参照した一次資料: 文書

採用した知見:

- 知見 A
` + "  続きの行\n" + `
採用しなかった知見と理由:

- 知見 B: 理由
`

func readmeWith(sectionBody string) string {
	return "# x\n\n## 責務\n\n本文\n\n" + sectionBody + "\n## 公式推奨からの逸脱\n\n無い。\n"
}

// buildTree は、root の下に files を書き、追跡ファイルの一覧（files のキー）を返す。
func buildTree(t *testing.T, files map[string]string) (string, []string) {
	t.Helper()
	root := t.TempDir()
	var tracked []string
	for name, body := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
		tracked = append(tracked, name)
	}
	return root, tracked
}

func validTree() map[string]string {
	return map[string]string{
		"usecases/a/README.md": readmeWith(validSection),
		"usecases/b/README.md": readmeWith(validSection),
		"modules/m/README.md":  readmeWith(validSection),
	}
}

func fixedLister(names []string, err error) listFunc {
	return func(string) ([]string, error) { return names, err }
}

// lineOf は、body の中で sub が最初に現れる行の番号を返す。
func lineOf(t *testing.T, body, sub string) string {
	t.Helper()
	i := strings.Index(body, sub)
	require.NotEqual(t, -1, i, sub)
	return strconv.Itoa(strings.Count(body[:i], "\n") + 1)
}

func lines(body string) []scanLine {
	return scanReadme([]byte(body)).lines
}

func Test_gitEnv(t *testing.T) {
	t.Run("このプロセスの env から GIT_DIR を外し、PATH を残す", func(t *testing.T) {
		t.Setenv("GIT_DIR", "/x")
		env, err := gitEnv()
		require.NoError(t, err)
		for _, kv := range env {
			assert.False(t, strings.HasPrefix(kv, "GIT_DIR="), kv)
		}
		assert.Contains(t, env, "PATH="+os.Getenv("PATH"))
	})
}

func Test_exitCode(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		err  error
		want int
	}{
		"エラーなし":   {nil, 0},
		"違反":      {xerrors.Wrapf(errViolations, "1 件"), 1},
		"引数":      {errUsage, 2},
		"読めない":    {errUnreadable, 2},
		"root 無し": {errNoRoot, 2},
		"対象 0件":   {errNoTargets, 2},
		"件数の食い違い": {errCountMismatch, 2},
		"未知のエラー":  {errors.New("boom"), 2},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, exitCode(c.err))
		})
	}
}

func Test_run(t *testing.T) {
	t.Parallel()

	t.Run("すべての対象が記録を持てば成功し、root ごとの件数を出す", func(t *testing.T) {
		t.Parallel()
		root, tracked := buildTree(t, validTree())
		var out bytes.Buffer
		require.NoError(t, run([]string{"-root", root}, &out, fixedLister(tracked, nil)))
		assert.Contains(t, out.String(), "usecases 2 件 / modules 1 件の README")
	})

	t.Run("違反があれば errViolations で、件数とファイルと行をファイルの順に出す", func(t *testing.T) {
		t.Parallel()
		files := validTree()
		files["usecases/b/README.md"] = readmeWith(strings.Replace(validSection, "- 知見 B: 理由", "- 知見 B", 1))
		files["modules/m/README.md"] = readmeWith(strings.Replace(validSection, "参照日: 2026-11-19", "参照日: 2026-13-19", 1))
		root, tracked := buildTree(t, files)
		var out bytes.Buffer
		err := run([]string{"-root", root}, &out, fixedLister(tracked, nil))
		require.ErrorIs(t, err, errViolations)
		got := out.String()
		assert.True(t, strings.HasPrefix(got, "❌ ref-record-lint: 2 件の違反\n"), got)
		modules := strings.Index(got, "  modules/m/README.md\n    :"+lineOf(t, files["modules/m/README.md"], "参照日: ")+"  ")
		usecases := strings.Index(got, "  usecases/b/README.md\n    :"+lineOf(t, files["usecases/b/README.md"], "- 知見 B")+"  ")
		require.NotEqual(t, -1, modules, got)
		require.NotEqual(t, -1, usecases, got)
		assert.Less(t, modules, usecases, "ファイルの名前の順に出す")
	})

	t.Run("README が無い対象は違反になる", func(t *testing.T) {
		t.Parallel()
		files := validTree()
		delete(files, "usecases/b/README.md")
		files["usecases/b/main.tf"] = ""
		root, tracked := buildTree(t, files)
		var out bytes.Buffer
		require.ErrorIs(t, run([]string{"-root", root}, &out, fixedLister(tracked, nil)), errViolations)
		assert.Contains(t, out.String(), "README.md がありません")
	})

	t.Run("片方の root が無ければ errNoRoot", func(t *testing.T) {
		t.Parallel()
		files := validTree()
		delete(files, "modules/m/README.md")
		root, tracked := buildTree(t, files)
		require.ErrorIs(t, run([]string{"-root", root}, &bytes.Buffer{}, fixedLister(tracked, nil)), errNoRoot)
	})

	t.Run("片方の root の対象が0件なら errNoTargets", func(t *testing.T) {
		t.Parallel()
		files := validTree()
		delete(files, "modules/m/README.md")
		root, tracked := buildTree(t, files)
		require.NoError(t, os.Mkdir(filepath.Join(root, "modules"), 0o750))
		require.ErrorIs(t, run([]string{"-root", root}, &bytes.Buffer{}, fixedLister(tracked, nil)), errNoTargets)
	})

	t.Run("追跡ファイルの集合と食い違えば errCountMismatch", func(t *testing.T) {
		t.Parallel()
		root, tracked := buildTree(t, validTree())
		require.NoError(t, os.Mkdir(filepath.Join(root, "usecases", "untracked"), 0o750))
		err := run([]string{"-root", root}, &bytes.Buffer{}, fixedLister(tracked, nil))
		require.ErrorIs(t, err, errCountMismatch)
		assert.Contains(t, err.Error(), "untracked")
	})

	t.Run("追跡にだけ在る対象があれば errCountMismatch で名指す", func(t *testing.T) {
		t.Parallel()
		root, tracked := buildTree(t, validTree())
		err := run([]string{"-root", root}, &bytes.Buffer{}, fixedLister(append(tracked, "usecases/ghost/README.md"), nil))
		require.ErrorIs(t, err, errCountMismatch)
		assert.Contains(t, err.Error(), `追跡にだけ在る ["ghost"]`)
	})

	t.Run("追跡ファイルの一覧を得られなければ errUnreadable", func(t *testing.T) {
		t.Parallel()
		root, _ := buildTree(t, validTree())
		require.ErrorIs(t, run([]string{"-root", root}, &bytes.Buffer{}, fixedLister(nil, errors.New("boom"))), errUnreadable)
	})

	t.Run("追跡ファイルが0件なら、対象が在っても errCountMismatch", func(t *testing.T) {
		t.Parallel()
		root, _ := buildTree(t, validTree())
		require.ErrorIs(t, run([]string{"-root", root}, &bytes.Buffer{}, fixedLister(nil, nil)), errCountMismatch)
	})

	t.Run("README が読めない形なら errUnreadable", func(t *testing.T) {
		t.Parallel()
		files := validTree()
		delete(files, "usecases/b/README.md")
		files["usecases/b/real.md"] = readmeWith(validSection)
		root, tracked := buildTree(t, files)
		require.NoError(t, os.Symlink("real.md", filepath.Join(root, "usecases", "b", "README.md")))
		require.ErrorIs(t, run([]string{"-root", root}, &bytes.Buffer{}, fixedLister(tracked, nil)), errUnreadable)
	})

	for name, args := range map[string][]string{
		"未知のフラグは errUsage": {"-nope"},
		"余分な引数は errUsage":  {"extra"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.ErrorIs(t, run(args, &bytes.Buffer{}, fixedLister(nil, nil)), errUsage)
		})
	}
}

func Test_gitFiles(t *testing.T) {
	t.Parallel()

	t.Run("追跡ファイルを、非 ASCII の名前も引用せずに返す", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		git := func(args ...string) {
			t.Helper()
			cmd := exec.CommandContext(t.Context(), "git", args...)
			cmd.Dir = dir
			out, err := cmd.CombinedOutput()
			require.NoError(t, err, string(out))
		}
		git("init", "-q")
		for _, name := range []string{"a.md", "日本語.md", "untracked.md"} {
			require.NoError(t, os.WriteFile(filepath.Join(dir, name), nil, 0o600))
		}
		git("add", "a.md", "日本語.md")
		var asked bool
		got, err := gitFiles(dir, func() ([]string, error) { asked = true; return gitEnv() })
		require.NoError(t, err)
		assert.Equal(t, []string{"a.md", "日本語.md"}, got)
		assert.True(t, asked, "隔離した env を使う")
	})

	t.Run("env を得られなければ、git を起動せずにエラーを返す", func(t *testing.T) {
		t.Parallel()
		boom := errors.New("boom")
		_, err := gitFiles(t.TempDir(), func() ([]string, error) { return nil, boom })
		require.ErrorIs(t, err, boom)
	})

	t.Run("リポジトリでなければ、git の失敗を返す", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		_, err := gitFiles(dir, func() ([]string, error) {
			env, err := gitEnv()
			return append(env, "GIT_CEILING_DIRECTORIES="+filepath.Dir(dir)), err
		})
		var exit *exec.ExitError
		require.ErrorAs(t, err, &exit, "git を起動し、git が失敗した")
	})
}

func Test_splitNUL(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		in   string
		want []string
	}{
		"空":        {"", nil},
		"末尾の NUL":  {"a\x00b c\x00", []string{"a", "b c"}},
		"連続した NUL": {"a\x00\x00b", []string{"a", "b"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, splitNUL(c.in))
		})
	}
}

func Test_listTargets(t *testing.T) {
	t.Parallel()

	t.Run("直下のディレクトリを昇順で返す", func(t *testing.T) {
		t.Parallel()
		root, _ := buildTree(t, map[string]string{"usecases/b/x": "", "usecases/a/x": ""})
		got, err := listTargets(root, "usecases")
		require.NoError(t, err)
		assert.Equal(t, []string{"a", "b"}, got)
	})

	t.Run("root が無ければ errNoRoot", func(t *testing.T) {
		t.Parallel()
		_, err := listTargets(t.TempDir(), "usecases")
		require.ErrorIs(t, err, errNoRoot)
	})

	t.Run("root が空なら errNoTargets", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		require.NoError(t, os.Mkdir(filepath.Join(root, "usecases"), 0o750))
		_, err := listTargets(root, "usecases")
		require.ErrorIs(t, err, errNoTargets)
	})

	t.Run("root がディレクトリでなければ errUnreadable", func(t *testing.T) {
		t.Parallel()
		root, _ := buildTree(t, map[string]string{"usecases": ""})
		_, err := listTargets(root, "usecases")
		require.ErrorIs(t, err, errUnreadable)
	})

	t.Run("直下の通常のファイルは errUnreadable", func(t *testing.T) {
		t.Parallel()
		root, _ := buildTree(t, map[string]string{"usecases/a/x": "", "usecases/README.md": ""})
		_, err := listTargets(root, "usecases")
		require.ErrorIs(t, err, errUnreadable)
	})

	t.Run("直下の symlink は、ディレクトリを指していても errUnreadable", func(t *testing.T) {
		t.Parallel()
		root, _ := buildTree(t, map[string]string{"usecases/a/x": ""})
		require.NoError(t, os.Symlink("a", filepath.Join(root, "usecases", "link")))
		_, err := listTargets(root, "usecases")
		require.ErrorIs(t, err, errUnreadable)
	})
}

func Test_trackedTargets(t *testing.T) {
	t.Parallel()

	t.Run("直下のディレクトリの名前を昇順で返し、直下のファイルと接頭辞が一致するだけの root を数えない", func(t *testing.T) {
		t.Parallel()
		got := trackedTargets([]string{
			"usecases/b/README.md",
			"usecases/a/tests/x.tftest.hcl",
			"usecases/a/main.tf",
			"usecases/top.md",
			"modules/m/README.md",
			"usecasesx/c/README.md",
		}, "usecases")
		assert.Equal(t, []string{"a", "b"}, got)
	})

	t.Run("追跡ファイルが無ければ空", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, trackedTargets(nil, "usecases"))
	})
}

func Test_compareSets(t *testing.T) {
	t.Parallel()

	t.Run("一致すれば nil", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, compareSets("usecases", []string{"a", "b"}, []string{"a", "b"}))
	})

	for name, c := range map[string]struct {
		dirs, tracked []string
		want          string
	}{
		"ディレクトリにだけ在る": {[]string{"a", "b"}, []string{"a"}, `ディレクトリにだけ在る ["b"]、追跡にだけ在る []`},
		"追跡にだけ在る":     {[]string{"a"}, []string{"a", "c"}, `ディレクトリにだけ在る []、追跡にだけ在る ["c"]`},
		"追跡が0件":       {[]string{"a"}, nil, `ディレクトリにだけ在る ["a"]、追跡にだけ在る []`},
		"件数が同じで名前が違う": {[]string{"a", "b"}, []string{"a", "c"}, `ディレクトリにだけ在る ["b"]、追跡にだけ在る ["c"]`},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := compareSets("usecases", c.dirs, c.tracked)
			require.ErrorIs(t, err, errCountMismatch)
			assert.Contains(t, err.Error(), c.want)
		})
	}
}

func Test_difference(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		a, b []string
		want []string
	}{
		"a にだけ在るものを a の順で返す": {[]string{"a", "b", "c"}, []string{"b"}, []string{"a", "c"}},
		"同じなら nil":   {[]string{"a"}, []string{"a"}, nil},
		"a が空なら nil": {nil, []string{"a"}, nil},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, difference(c.a, c.b))
		})
	}
}

func Test_checkTarget(t *testing.T) {
	t.Parallel()

	t.Run("通る README は違反なし", func(t *testing.T) {
		t.Parallel()
		root, _ := buildTree(t, map[string]string{"usecases/a/README.md": readmeWith(validSection)})
		got, err := checkTarget(root, "usecases/a/README.md")
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("README が無ければ1行目の違反", func(t *testing.T) {
		t.Parallel()
		got, err := checkTarget(t.TempDir(), "usecases/a/README.md")
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, lintreport.Finding{File: "usecases/a/README.md", Line: 1, Message: "README.md がありません"}, got[0])
	})

	t.Run("README がディレクトリなら errUnreadable", func(t *testing.T) {
		t.Parallel()
		root, _ := buildTree(t, map[string]string{"usecases/a/README.md/x": ""})
		_, err := checkTarget(root, "usecases/a/README.md")
		require.ErrorIs(t, err, errUnreadable)
	})

	t.Run("README を Lstat できなければ errUnreadable", func(t *testing.T) {
		t.Parallel()
		testenv.RequireNonRoot(t, "root はディレクトリの権限を無視する")
		root, _ := buildTree(t, map[string]string{"usecases/a/README.md": readmeWith(validSection)})
		dir := filepath.Join(root, "usecases", "a")
		require.NoError(t, os.Chmod(dir, 0o000))
		t.Cleanup(func() { _ = os.Chmod(dir, 0o750) })
		_, err := checkTarget(root, "usecases/a/README.md")
		require.ErrorIs(t, err, errUnreadable)
	})

	t.Run("README を読めなければ errUnreadable", func(t *testing.T) {
		t.Parallel()
		testenv.RequireNonRoot(t, "root はファイルの権限を無視する")
		root, _ := buildTree(t, map[string]string{"usecases/a/README.md": readmeWith(validSection)})
		require.NoError(t, os.Chmod(filepath.Join(root, "usecases", "a", "README.md"), 0o000))
		_, err := checkTarget(root, "usecases/a/README.md")
		require.ErrorIs(t, err, errUnreadable)
	})

	t.Run("見出しの数が食い違えば errCountMismatch を伝える", func(t *testing.T) {
		t.Parallel()
		root, _ := buildTree(t, map[string]string{"usecases/a/README.md": "```\n" + heading + "\n```\n"})
		_, err := checkTarget(root, "usecases/a/README.md")
		require.ErrorIs(t, err, errCountMismatch)
	})
}

func Test_checkReadme(t *testing.T) {
	t.Parallel()

	t.Run("通る README は違反なし", func(t *testing.T) {
		t.Parallel()
		got, err := checkReadme("R.md", []byte(readmeWith(validSection)))
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("見出しが無ければ1行目の違反", func(t *testing.T) {
		t.Parallel()
		got, err := checkReadme("R.md", []byte("# x\n\n本文\n"))
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, 1, got[0].Line)
		assert.Contains(t, got[0].Message, "見出しがありません")
	})

	t.Run("見出しが2つなら2つ目の行の違反", func(t *testing.T) {
		t.Parallel()
		got, err := checkReadme("R.md", []byte(validSection+"\n"+validSection))
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, strings.Count(validSection, "\n")+2, got[0].Line)
		assert.Contains(t, got[0].Message, "2 件")
	})

	t.Run("フェンスの中にだけ見出しがあれば errCountMismatch", func(t *testing.T) {
		t.Parallel()
		_, err := checkReadme("R.md", []byte("# x\n\n```md\n"+validSection+"```\n"))
		require.ErrorIs(t, err, errCountMismatch)
	})

	t.Run("フェンスの外の見出しに加えてフェンスの中にもあれば errCountMismatch", func(t *testing.T) {
		t.Parallel()
		_, err := checkReadme("R.md", []byte(readmeWith(validSection)+"\n````\n"+heading+"\n````\n"))
		require.ErrorIs(t, err, errCountMismatch)
	})

	t.Run("コメントの中にだけ節があれば、その行と理由を名指して errCountMismatch", func(t *testing.T) {
		t.Parallel()
		body := readmeWith("<!--\n" + validSection + "-->\n")
		_, err := checkReadme("R.md", []byte(body))
		require.ErrorIs(t, err, errCountMismatch)
		assert.Contains(t, err.Error(), "読まなかった行: "+lineOf(t, body, heading)+" 行目（HTML コメント）")
	})

	t.Run("フェンスの中の見出しは、その行と理由を名指す", func(t *testing.T) {
		t.Parallel()
		_, err := checkReadme("R.md", []byte("# x\n```\n"+heading+"\n```\n"))
		require.ErrorIs(t, err, errCountMismatch)
		assert.Contains(t, err.Error(), "読まなかった行: 3 行目（フェンス）")
	})

	t.Run("フェンスの中の閉じない開きは、後ろの見出しを隠さない", func(t *testing.T) {
		t.Parallel()
		got, err := checkReadme("R.md", []byte("# x\n\n```html\n<!-- 例\n```\n\n"+readmeWith(validSection)))
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("コメントの中のフェンスの区切りは、後ろの見出しを隠さない", func(t *testing.T) {
		t.Parallel()
		got, err := checkReadme("R.md", []byte("# x\n\n<!--\n```\n-->\n\n"+readmeWith(validSection)))
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("行頭がコメントの行は見出しとして数えない", func(t *testing.T) {
		t.Parallel()
		got, err := checkReadme("R.md", []byte(readmeWith(strings.Replace(validSection, heading, "<!-- -->"+heading, 1))))
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Contains(t, got[0].Message, "見出しがありません")
	})

	t.Run("コメントだけの項目は、本文が空の項目", func(t *testing.T) {
		t.Parallel()
		body := readmeWith(strings.Replace(validSection, "- 知見 A\n", "- 知見 A\n- <!-- c -->\n", 1))
		got, err := checkReadme("R.md", []byte(body))
		require.NoError(t, err)
		line, err := strconv.Atoi(lineOf(t, body, "- <!-- c -->"))
		require.NoError(t, err)
		assert.Equal(t, []lintreport.Finding{{File: "R.md", Line: line, Message: `"採用した知見:" の項目の本文が空です`}}, got)
	})

	t.Run("字下げした見出しも、参照記録の見出しとして読む", func(t *testing.T) {
		t.Parallel()
		got, err := checkReadme("R.md", []byte(readmeWith(strings.Replace(validSection, heading, "  "+heading, 1))))
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("HTML ブロックの開きと閉じのタグの行は、それぞれの行の違反", func(t *testing.T) {
		t.Parallel()
		body := readmeWith(validSection) + "\n<pre>\nx\n</pre>\n"
		got, err := checkReadme("R.md", []byte(body))
		require.NoError(t, err)
		line, err := strconv.Atoi(lineOf(t, body, "<pre>"))
		require.NoError(t, err)
		assert.Equal(t, []lintreport.Finding{{File: "R.md", Line: line, Message: msgHTMLBlock}, {File: "R.md", Line: line + 2, Message: msgHTMLBlock}}, got)
	})

	t.Run("div と details の閉じのタグで始まる行は、それぞれの行の違反", func(t *testing.T) {
		t.Parallel()
		body := readmeWith(validSection) + "\n<div>\nx\n\n</details>\n"
		got, err := checkReadme("R.md", []byte(body))
		require.NoError(t, err)
		div, err := strconv.Atoi(lineOf(t, body, "<div>"))
		require.NoError(t, err)
		details, err := strconv.Atoi(lineOf(t, body, "</details>"))
		require.NoError(t, err)
		assert.Equal(t, []lintreport.Finding{
			{File: "R.md", Line: div, Message: msgHTMLBlock},
			{File: "R.md", Line: details, Message: msgHTMLBlock},
		}, got)
	})

	t.Run("段落の途中のタグは違反でない", func(t *testing.T) {
		t.Parallel()
		got, err := checkReadme("R.md", []byte(readmeWith(validSection)+"\na <b>x</b>\n"))
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("setext 見出しは、下線の行の違反", func(t *testing.T) {
		t.Parallel()
		body := readmeWith(validSection) + "\n本文\n---\n"
		got, err := checkReadme("R.md", []byte(body))
		require.NoError(t, err)
		assert.Equal(t, []lintreport.Finding{{File: "R.md", Line: strings.Count(body, "\n"), Message: msgSetext}}, got)
	})

	t.Run("見出しより前の閉じないコメントは、見出しの行をコメントの理由で名指す errCountMismatch", func(t *testing.T) {
		t.Parallel()
		body := "<!-- 閉じない\n\n" + readmeWith(validSection)
		_, err := checkReadme("R.md", []byte(body))
		require.ErrorIs(t, err, errCountMismatch)
		assert.Contains(t, err.Error(), "読まなかった行: "+lineOf(t, body, heading)+" 行目（HTML コメント）")
	})

	t.Run("閉じないフェンスが節の見出しを飲み込めば、その行をフェンスの理由で名指す errCountMismatch", func(t *testing.T) {
		t.Parallel()
		body := "# x\n\n```\n\n" + validSection
		_, err := checkReadme("R.md", []byte(body))
		require.ErrorIs(t, err, errCountMismatch)
		assert.Contains(t, err.Error(), "読まなかった行: "+lineOf(t, body, heading)+" 行目（フェンス）")
	})

	t.Run("コメントの中の参照日と AWS Provider の version は数えない", func(t *testing.T) {
		t.Parallel()
		date := "参照日: 2026-11-19（一部は 2026-11-20）、AWS Provider v6.70.0 の文書"
		body := readmeWith(strings.Replace(validSection, date, "<!-- "+date+" -->", 1))
		got, err := checkReadme("R.md", []byte(body))
		require.NoError(t, err)
		msgs := make([]string, 0, len(got))
		for _, f := range got {
			msgs = append(msgs, f.Message)
		}
		assert.ElementsMatch(t, []string{
			`節に "参照日: YYYY-MM-DD" の行がありません`,
			"節に AWS Provider の version（`AWS Provider v6.70.0` の形）がありません",
		}, msgs)
	})

	t.Run("空のコメントは閉じないコメントではない", func(t *testing.T) {
		t.Parallel()
		got, err := checkReadme("R.md", []byte("<!--> a <!--->\n"+readmeWith(validSection)))
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("1行のコメントの中の見出しは数えない", func(t *testing.T) {
		t.Parallel()
		got, err := checkReadme("R.md", []byte("<!-- "+heading+" -->\n"+readmeWith(validSection)))
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("コメントの中のラベルと項目は数えない", func(t *testing.T) {
		t.Parallel()
		section := strings.Replace(validSection, "採用した知見:\n\n- 知見 A\n", "<!--\n採用した知見:\n\n- 知見 A\n-->\n", 1)
		got, err := checkReadme("R.md", []byte(readmeWith(section)))
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Contains(t, got[0].Message, `"採用した知見:" の行がありません`)
	})

	t.Run("閉じないコメントは、開いた行の違反", func(t *testing.T) {
		t.Parallel()
		body := readmeWith(validSection) + "\n<!-- 閉じない\n"
		got, err := checkReadme("R.md", []byte(body))
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, strings.Count(body, "\n"), got[0].Line)
		assert.Contains(t, got[0].Message, "HTML コメントが閉じていません")
	})

	t.Run("見出しの語が長ければ見出しとして数えない", func(t *testing.T) {
		t.Parallel()
		got, err := checkReadme("R.md", []byte(readmeWith(strings.Replace(validSection, heading, heading+"メモ", 1))))
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Contains(t, got[0].Message, "見出しがありません")
	})

	t.Run("見出しの後ろの空白とコメントは許す", func(t *testing.T) {
		t.Parallel()
		got, err := checkReadme("R.md", []byte(readmeWith(strings.Replace(validSection, heading+"（ADR-0206 決定7・8）", heading+" <!-- sample:line -->", 1))))
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("CRLF の README も、中身が揃えば違反なし", func(t *testing.T) {
		t.Parallel()
		got, err := checkReadme("R.md", []byte(strings.ReplaceAll(readmeWith(validSection), "\n", "\r\n")))
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("節は次の2段目の見出しで終わり、その後の項目を数えない", func(t *testing.T) {
		t.Parallel()
		body := "## 公式実装の参照\n\n参照日: 2026-11-19、AWS Provider v6.70.0\n\n## 別\n\n" +
			"採用した知見:\n\n- A\n\n採用しなかった知見と理由:\n\n- B: 理由\n"
		got, err := checkReadme("R.md", []byte(body))
		require.NoError(t, err)
		require.Len(t, got, 2)
		for _, f := range got {
			assert.Equal(t, 1, f.Line)
			assert.Contains(t, f.Message, "の行がありません")
		}
	})
}

func Test_section(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		body string
		want []string
	}{
		"3段目の見出しでは終わらず、次の2段目の見出しで終わる": {"## 公式実装の参照\na\n### 小見出し\nb\n## 次\nc\n", []string{"a", "### 小見出し", "b"}},
		"1段目の見出しで終わる":                 {"## 公式実装の参照\na\n# 1段目\n", []string{"a"}},
		"字下げした2段目の見出しで終わる":            {"## 公式実装の参照\na\n   ## 次\n", []string{"a"}},
		"見出しが続かなければ末尾まで":              {"## 公式実装の参照\na\nb", []string{"a", "b"}},
		"行頭がコメントの行と、コメントの中の行では終わらない":  {"## 公式実装の参照\na\n<!-- -->## 別\nb\n<!--\n## 中\n-->\nc", []string{"a", "## 別", "b", "", "", "", "c"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := section(lines(c.body), 0)
			texts := make([]string, 0, len(got))
			for _, l := range got {
				texts = append(texts, l.Text)
			}
			assert.Equal(t, c.want, texts)
		})
	}
}

func Test_headingLevel(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		text string
		want int
	}{
		"1段目":       {"# a", 1},
		"6段目":       {"###### a", 6},
		"# だけの行":    {"##", 2},
		"7段は見出しでない": {"####### a", 0},
		"空白の無い # は見出しでない": {"#hash", 0},
		"行頭がコメント":         {"<!-- -->## a", 0},
		"1つの字下げ":          {" ## a", 2},
		"3つの字下げ":          {"   ## a", 2},
		"4つの字下げは見出しでない":   {"    ## a", 0},
		"タブで区切る":          {"##\ta", 2},
		"本文":              {"a", 0},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, headingLevel(c.text))
		})
	}
}

func Test_frontMatterEnd(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		texts []string
		want  int
	}{
		"閉じの行の添字を返す":        {[]string{"---", "a: b", "---", "---"}, 2},
		"閉じが無ければ 0":         {[]string{"---", "a: b"}, 0},
		"1行目が --- でなければ 0":  {[]string{"a", "---", "---"}, 0},
		"字下げした --- は区切りでない": {[]string{"---", " ---"}, 0},
		"空": {nil, 0},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, frontMatterEnd(c.texts))
		})
	}
}

func Test_isHTMLBlockStart(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		text string
		want bool
	}{
		"pre":             {"<pre>", true},
		"属性が続く script":    {"<script src=x>", true},
		"大文字の STYLE":      {"<STYLE>", true},
		"行末で終わる textarea": {"<textarea", true},
		"div":             {"<div>", true},
		"閉じのタグ details":   {"</details>", true},
		"任意のタグ名":          {"<prefix>", true},
		"3つまでの字下げ":        {"   <pre>", true},
		"4つの字下げ":          {"    <pre>", false},
		"行頭でない":           {"a <b>x</b>", false},
		"< の後が英字でない":      {"< a", true},
		"< の後が数字":         {"<1>", true},
		"コメントは HTML ブロックとして扱わない": {"<!-- x -->", false},
		"</ の後が英字でない":            {"</ a>", true},
		"処理命令":                   {"<?php", true},
		"宣言":                     {"<!X", true},
		"CDATA":                  {"<![CDATA[", true},
		"autolink":               {"<https://x>", true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, isHTMLBlockStart(c.text))
		})
	}
}

func Test_isSetextUnderline(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		text string
		want bool
	}{
		"= の並び":    {"===", true},
		"- が1つ":    {"-", true},
		"後ろの空白を許す": {"---  ", true},
		"3つまでの字下げ": {"   ==", true},
		"4つの字下げ":   {"    ==", false},
		"混ざる":      {"=-", false},
		"間に空白":     {"- -", false},
		"本文が続く":    {"- A", false},
		"空":        {"", false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, isSetextUnderline(c.text))
		})
	}
}

func Test_isParagraphLine(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		text string
		want bool
	}{
		"本文":          {"本文", true},
		"label":       {adoptedLabel, true},
		"= の並び":       {"===", true},
		"空白だけ":        {"  ", false},
		"見出し":         {"## a", false},
		"- の項目":       {"- A", false},
		"- だけの項目":     {"-", false},
		"番号の項目":       {"1. A", false},
		"区切り線":        {"* * *", false},
		"HTML ブロック":   {"<pre>", false},
		"行頭がコメント":     {"<!-- x -->", false},
		"引用":          {"> quote", false},
		"字下げした引用":     {"   > quote", false},
		"4つの字下げ":      {"    code", false},
		"タブの字下げ":      {"\tcode", false},
		"3つまでの字下げの本文": {"   本文", true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, isParagraphLine(c.text))
		})
	}
}

func Test_rawLines(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		content string
		want    []mdscan.Line
	}{
		"フェンスも区別せず、全行を1始まりの番号で返す": {"a\n```\n", []mdscan.Line{{Number: 1, Text: "a"}, {Number: 2, Text: "```"}, {Number: 3, Text: ""}}},
		"コメントも区別しない":              {"<!--\nb", []mdscan.Line{{Number: 1, Text: "<!--"}, {Number: 2, Text: "b"}}},
		"空の入力は1行の空文字":             {"", []mdscan.Line{{Number: 1, Text: ""}}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, rawLines([]byte(c.content)))
		})
	}
}

func Test_isHeading(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		text string
		want bool
	}{
		"見出しだけ":    {heading, true},
		"空白が続く":    {heading + " <!-- x -->", true},
		"括弧書きが続く":  {heading + "（ADR-0206 決定7・8）", true},
		"語が続く":     {heading + "メモ", false},
		"3段目":      {"#" + heading, false},
		"3つまでの字下げ": {"   " + heading, true},
		"4つの字下げ":   {"    " + heading, false},
		"タブで区切る":   {"##\t公式実装の参照", true},
		"空白が2つ":    {"##  公式実装の参照", true},
		"別の見出し":    {"## 公式実装", false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, isHeading(c.text))
		})
	}
}

func Test_hasTail(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		rest string
		want bool
	}{
		"空":         {"", true},
		"どれかで始まる":   {"（x）", true},
		"2つ目で始まる":   {" x", true},
		"どれでも始まらない": {"x", false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, hasTail(c.rest, []string{"（", " "}))
		})
	}
}

func Test_scanReadme(t *testing.T) {
	t.Parallel()

	type line = scanLine
	for name, c := range map[string]struct {
		content string
		want    scanned
	}{
		"フェンスもコメントも無ければ全行を返し、見出しの添字を持つ": {
			"a\n" + heading + "\nb",
			scanned{lines: []line{{Number: 1, Text: "a"}, {Number: 2, Text: heading, Level: 2}, {Number: 3, Text: "b"}}, headings: []int{1}},
		},
		"フェンスの行を返さず、中の見出しをフェンスの理由で報告する": {
			"a\n```\n" + heading + "\n```\nb",
			scanned{lines: []line{{Number: 1, Text: "a"}, {Number: 5, Text: "b"}}, hidden: []hiddenLine{{Line: 3, Reason: reasonFence}}},
		},
		"コメントの中の見出しを、空の行にしてコメントの理由で報告する": {
			"<!--\n" + heading + "\n-->",
			scanned{lines: []line{{Number: 1}, {Number: 2}, {Number: 3}}, hidden: []hiddenLine{{Line: 2, Reason: reasonComment}}},
		},
		"フェンスの中の閉じない開きはコメントを開かない": {
			"```\n<!--\n```\n" + heading,
			scanned{lines: []line{{Number: 4, Text: heading, Level: 2}}, headings: []int{0}},
		},
		"コメントの中のフェンスの区切りはフェンスを開かない": {
			"<!--\n```\n-->\n" + heading,
			scanned{lines: []line{{Number: 1}, {Number: 2}, {Number: 3}, {Number: 4, Text: heading, Level: 2}}, headings: []int{3}},
		},
		"行頭がコメントの行は、残りが見出しの形でも見出しではない": {
			"<!-- -->" + heading + "\n<!-- -->### 小",
			scanned{lines: []line{{Number: 1, Text: heading}, {Number: 2, Text: "### 小"}}},
		},
		"見出しの段を元の行から読み、コメントの中で始まる行は見出しにしない": {
			"# 一\n### 三\n<!--\n## 中 -->\n####### 七",
			scanned{lines: []line{{Number: 1, Text: "# 一", Level: 1}, {Number: 2, Text: "### 三", Level: 3}, {Number: 3}, {Number: 4}, {Number: 5, Text: "####### 七"}}},
		},
		"閉じなければ開いた行を返す": {
			"a\n<!-- b\nc",
			scanned{lines: []line{{Number: 1, Text: "a"}, {Number: 2}, {Number: 3}}, openedAt: 2},
		},
		"コメントの中で閉じて開き直せば、開き直した行を返す": {
			"<!-- a\nb --> c <!-- d\ne",
			scanned{lines: []line{{Number: 1}, {Number: 2, Text: " c "}, {Number: 3}}, openedAt: 2},
		},
		"字下げした見出しも見出しとして数える": {
			"   " + heading,
			scanned{lines: []line{{Number: 1, Text: "   " + heading, Level: 2}}, headings: []int{0}},
		},
		"HTML ブロックの開始行を、読まない形として報告する": {
			"a\n\n<PRE>\n</pre>",
			scanned{lines: []line{{Number: 1, Text: "a"}, {Number: 2}, {Number: 3, Text: "<PRE>"}, {Number: 4, Text: "</pre>"}},
				unsupported: []lintreport.Finding{{Line: 3, Message: msgHTMLBlock}, {Line: 4, Message: msgHTMLBlock}}},
		},
		"段落の直後の = か - だけの行を、setext 見出しとして報告する": {
			"a\n===\n\nb\n-",
			scanned{lines: []line{{Number: 1, Text: "a"}, {Number: 2, Text: "==="}, {Number: 3}, {Number: 4, Text: "b"}, {Number: 5, Text: "-"}},
				unsupported: []lintreport.Finding{{Line: 2, Message: msgSetext}, {Line: 5, Message: msgSetext}}},
		},
		"フェンスと、行頭で開いたコメントの直後の --- は setext ではない": {
			"a\n```\n```\n---\n<!--\n-->\n---",
			scanned{lines: []line{{Number: 1, Text: "a"}, {Number: 4, Text: "---"}, {Number: 5}, {Number: 6}, {Number: 7, Text: "---"}}},
		},
		"引用と字下げしたコードの直後の --- は setext ではない": {
			"> quote\n---\n\n    code\n---",
			scanned{lines: []line{{Number: 1, Text: "> quote"}, {Number: 2, Text: "---"}, {Number: 3}, {Number: 4, Text: "    code"}, {Number: 5, Text: "---"}}},
		},
		"1行目からの front matter の閉じの --- は setext ではない": {
			"---\ntitle: x\n---\n本文",
			scanned{lines: []line{{Number: 1, Text: "---"}, {Number: 2, Text: "title: x"}, {Number: 3, Text: "---"}, {Number: 4, Text: "本文"}}},
		},
		"front matter の中の < は HTML ブロックではなく、閉じた後は読む": {
			"---\nx: <y>\n<z>\n---\n<div>",
			scanned{lines: []line{{Number: 1, Text: "---"}, {Number: 2, Text: "x: <y>"}, {Number: 3, Text: "<z>"}, {Number: 4, Text: "---"}, {Number: 5, Text: "<div>"}},
				unsupported: []lintreport.Finding{{Line: 5, Message: msgHTMLBlock}}},
		},
		"front matter の中の見出しを front matter の理由で報告する": {
			"---\n" + heading + "\n---",
			scanned{lines: []line{{Number: 1, Text: "---"}, {Number: 2, Text: heading}, {Number: 3, Text: "---"}},
				hidden: []hiddenLine{{Line: 2, Reason: reasonFrontMatter}}},
		},
		"閉じない 1行目の --- は front matter ではない": {
			"---\n<x>",
			scanned{lines: []line{{Number: 1, Text: "---"}, {Number: 2, Text: "<x>"}},
				unsupported: []lintreport.Finding{{Line: 2, Message: msgHTMLBlock}}},
		},
		"2行目からの --- の組は front matter ではない": {
			"a\n---\nb\n---",
			scanned{lines: []line{{Number: 1, Text: "a"}, {Number: 2, Text: "---"}, {Number: 3, Text: "b"}, {Number: 4, Text: "---"}},
				unsupported: []lintreport.Finding{{Line: 2, Message: msgSetext}, {Line: 4, Message: msgSetext}}},
		},
		"段落の中で開いたコメントの後ろは段落が続き、--- は setext": {
			"b <!--\n-->\n---",
			scanned{lines: []line{{Number: 1, Text: "b "}, {Number: 2}, {Number: 3, Text: "---"}},
				unsupported: []lintreport.Finding{{Line: 3, Message: msgSetext}}},
		},
		"空行の後の --- と、項目の後の - と、フェンスとコメントの中は報告しない": {
			"a\n\n---\n- A\n-\n```\n<pre>\n```\n<!--\n<pre>\n-->",
			scanned{lines: []line{{Number: 1, Text: "a"}, {Number: 2}, {Number: 3, Text: "---"}, {Number: 4, Text: "- A"}, {Number: 5, Text: "-"},
				{Number: 9}, {Number: 10}, {Number: 11}}},
		},
		"閉じないフェンスは後ろの行を返さず、中の見出しをフェンスの理由で報告する": {
			"a\n```\nb\n" + heading + "\nc",
			scanned{lines: []line{{Number: 1, Text: "a"}}, hidden: []hiddenLine{{Line: 4, Reason: reasonFence}}},
		},
		"閉じれば開いた行を忘れる": {
			"<!-- a\nb -->",
			scanned{lines: []line{{Number: 1}, {Number: 2}}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, scanReadme([]byte(c.content)))
		})
	}
}

func Test_stripComments(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		text      string
		open      bool
		want      string
		stillOpen bool
	}{
		"コメントが無ければそのまま":                        {"a", false, "a", false},
		"1行のコメントは空になる":                         {"<!-- a -->", false, "", false},
		"行末のコメントを外す":                           {"- A <!-- sample:line -->", false, "- A ", false},
		"開いたままの行は空になる":                         {"## x", true, "", true},
		"閉じた後ろの文字は残す":                          {"b --> c", true, " c", false},
		"1行に2つのコメント":                           {"a<!-- x -->b<!-- y -->c", false, "abc", false},
		"inline code の中の開きは開かない":               {"`<!--` a", false, "`<!--` a", false},
		"閉じない inline code の後ろは開く":              {"`a <!-- b", false, "`a ", true},
		"閉じなければ開いたままを返す":                       {"a <!-- b", false, "a ", true},
		"長さの違う並びを読み飛ばして inline code を閉じる":      {"`a``b<!-- c`", false, "`a``b<!-- c`", false},
		"長さの違う閉じは inline code を閉じない":           {"`a``<!-- b", false, "`a``", true},
		"2つ以上のバッククォートの inline code の中の開きは開かない": {"``a`<!--``", false, "``a`<!--``", false},
		"閉じた同じ行で開き直す":                          {"<!-- a --> b <!-- c", false, " b ", true},
		"コメントでない < の後ろの開きは開く":                  {"a <div> <!-- x", false, "a <div> ", true},
		"<! で始まるがコメントでないものは文字":                 {"a <div> <!x> b", false, "a <div> <!x> b", false},
		"バックスラッシュで逃がした < は開かない":                {"\\<!-- x", false, "\\<!-- x", false},
		"逃がしたバックスラッシュの後ろの < は開く":               {"\\\\<!-- x", false, "\\\\", true},
		"逃がしたバッククォートは inline code を開かない":       {"\\`<!-- x`", false, "\\`", true},
		"<!--> は空のコメント":                        {"a<!-->b", false, "ab", false},
		"<!---> は空のコメント":                       {"a<!--->b", false, "ab", false},
		"<!----> は閉じたコメント":                     {"a<!---->b", false, "ab", false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, stillOpen := stripComments(c.text, c.open)
			assert.Equal(t, c.want, got)
			assert.Equal(t, c.stillOpen, stillOpen)
		})
	}
}

func Test_cutEmptyComment(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		rest string
		want string
		ok   bool
	}{
		"> で閉じる":          {">b", "b", true},
		"-> で閉じる":         {"->b", "b", true},
		"--> は空のコメントではない": {"-->b", "", false},
		"本文が続く":           {" a -->", "", false},
		"空":               {"", "", false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, ok := cutEmptyComment(c.rest)
			assert.Equal(t, c.want, got)
			assert.Equal(t, c.ok, ok)
		})
	}
}

func Test_checkSection(t *testing.T) {
	t.Parallel()

	t.Run("すべて揃えば違反なし", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, checkSection("R.md", 1, lines(validSection)[1:]))
	})

	t.Run("項目が無い違反は見出しの行に付く", func(t *testing.T) {
		t.Parallel()
		got := checkSection("R.md", 7, lines("本文だけ\n"))
		require.Len(t, got, 4)
		msgs := make([]string, 0, len(got))
		for _, f := range got {
			assert.Equal(t, 7, f.Line)
			assert.Equal(t, "R.md", f.File)
			msgs = append(msgs, f.Message)
		}
		joined := strings.Join(msgs, "\n")
		for _, want := range []string{"参照日", "AWS Provider", adoptedLabel, rejectedLabel} {
			assert.Contains(t, joined, want)
		}
	})

	t.Run("項目の行に付く違反は、その行を保つ", func(t *testing.T) {
		t.Parallel()
		body := lines("x\n参照日: 2026-02-30\nAWS Provider 6.70.0\n採用した知見:\n- A\n採用しなかった知見と理由:\n- B\n")
		got := checkSection("R.md", 1, body)
		require.Len(t, got, 2)
		assert.Equal(t, 2, got[0].Line)
		assert.Equal(t, 7, got[1].Line)
	})
}

func Test_checkDate(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		body     string
		ok       bool
		line     int
		contains string
	}{
		"日付だけ":        {"x\n参照日: 2026-11-19\n", true, 2, ""},
		"括弧書きが続く":     {"参照日: 2026-10-25（lifecycle の項は 2026-11-08）\n", true, 1, ""},
		"読点で続く":       {"参照日: 2026-11-19、AWS Provider v6.70.0\n", true, 1, ""},
		"行が無い":        {"2026-11-19 に確認した。\n", false, 0, "の行がありません"},
		"コロンの後に空白が無い": {"参照日:2026-11-19\n", false, 0, "の行がありません"},
		"行頭でない":       {"- 参照日: 2026-11-19\n", false, 0, "の行がありません"},
		"2行ある":        {"参照日: 2026-11-19\n参照日: 2026-11-20\n", false, 2, "2 件"},
		"月が範囲の外":      {"参照日: 2026-13-01\n", false, 1, "読めません"},
		"日が範囲の外":      {"参照日: 2026-02-29\n", false, 1, "読めません"},
		"閏日":          {"参照日: 2028-02-29\n", true, 1, ""},
		"短い":          {"参照日: 2026-11-1\n", false, 1, "読めません"},
		"空":           {"参照日: \n", false, 1, "読めません"},
		"日付でない":       {"参照日: 不明です、確かめる\n", false, 1, "読めません"},
		"空白で続く":       {"参照日: 2026-11-19 AWS Provider v6.70.0\n", true, 1, ""},
		"日の後ろに数字が続く":  {"参照日: 2026-11-199\n", false, 1, "読めません"},
		"日の後ろに文字が続く":  {"参照日: 2026-01-01abc\n", false, 1, "読めません"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			line, msg, ok := checkDate(lines(c.body))
			assert.Equal(t, c.ok, ok)
			assert.Equal(t, c.line, line)
			if c.ok {
				assert.Empty(t, msg)
			} else {
				assert.Contains(t, msg, c.contains)
			}
		})
	}
}

func Test_hasProviderVersion(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		body string
		want bool
	}{
		"v 付き":        {"AWS Provider v6.70.0 の文書", true},
		"v 無し":        {"- 参照した一次資料: AWS Provider 6.70.0 の `aws_s3_bucket`", true},
		"2行目":         {"x\n参照は AWS Provider v6.73.0", true},
		"AWS が無い":     {"Provider v6.70.0", false},
		"version が無い": {"AWS Provider の文書", false},
		"2桁の version": {"AWS Provider v6.70", false},
		"空":           {"", false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, hasProviderVersion(lines(c.body)))
		})
	}
}

func Test_checkList(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		body       string
		label      string
		needReason bool
		want       []lintreport.Finding
	}{
		"項目が続く": {
			"採用した知見:\n\n- A\n  続き\n\n- B\n", adoptedLabel, false, nil,
		},
		"後ろの空白を許す": {
			"採用した知見:  \n- A\n", adoptedLabel, false, nil,
		},
		"理由を持つ項目": {
			"採用しなかった知見と理由:\n- A（`https://x`）: 理由\n- B: 理由 2\n", rejectedLabel, true, nil,
		},
		"label の行が無い": {
			"採用:\n- A\n", adoptedLabel, false,
			[]lintreport.Finding{{Message: `節に "採用した知見:" の行がありません`}},
		},
		"label が2つ": {
			"採用した知見:\n- A\n採用した知見:\n- B\n", adoptedLabel, false,
			[]lintreport.Finding{{Line: 3, Message: `"採用した知見:" の行が 2 件あります`}},
		},
		"label の後に項目が無い": {
			"採用した知見:\n\n採用しなかった知見と理由:\n- B: 理由\n", adoptedLabel, false,
			[]lintreport.Finding{{Line: 1, Message: `"採用した知見:" の後に項目がありません`}},
		},
		"項目の前の文は、一覧の途中の違反": {
			"採用した知見:\n本文\n- A\n", adoptedLabel, false,
			[]lintreport.Finding{{Line: 2, Message: `"採用した知見:" の一覧の途中に項目でない行があります`}},
		},
		"項目の間の文は、一覧の途中の違反": {
			"採用した知見:\n- A\n\n本文\n本文 2\n\n- B\n", adoptedLabel, false,
			[]lintreport.Finding{
				{Line: 4, Message: `"採用した知見:" の一覧の途中に項目でない行があります`},
				{Line: 5, Message: `"採用した知見:" の一覧の途中に項目でない行があります`},
			},
		},
		"項目の後の文は、後ろに項目が無ければ許す": {
			"採用した知見:\n- A\n\n本文\n", adoptedLabel, false, nil,
		},
		"次の label で一覧が終わる": {
			"採用した知見:\n- A\n採用しなかった知見と理由:\n本文\n- B: 理由\n", adoptedLabel, false, nil,
		},
		"見出しで一覧が終わる": {
			"採用した知見:\n- A\n### 小見出し\n本文\n- B\n", adoptedLabel, false, nil,
		},
		"末尾で label が終わる": {
			"採用した知見:", adoptedLabel, false,
			[]lintreport.Finding{{Line: 1, Message: `"採用した知見:" の後に項目がありません`}},
		},
		"本文が空の項目": {
			"採用した知見:\n- A\n-  \n", adoptedLabel, false,
			[]lintreport.Finding{{Line: 3, Message: `"採用した知見:" の項目の本文が空です`}},
		},
		"ハイフンだけの項目": {
			"採用した知見:\n- A\n-\n", adoptedLabel, false,
			[]lintreport.Finding{{Line: 3, Message: `"採用した知見:" の項目の本文が空です`}},
		},
		"ハイフンだけの項目だけなら、項目が無い違反は出さない": {
			"採用した知見:\n-\t\n", adoptedLabel, false,
			[]lintreport.Finding{{Line: 2, Message: `"採用した知見:" の項目の本文が空です`}},
		},
		"理由が無い": {
			"採用しなかった知見と理由:\n- A\n", rejectedLabel, true,
			[]lintreport.Finding{{Line: 2, Message: "\"採用しなかった知見と理由:\" の項目が `<知見>: <理由>` の形ではありません"}},
		},
		"理由が空": {
			"採用しなかった知見と理由:\n- A: \n", rejectedLabel, true,
			[]lintreport.Finding{{Line: 2, Message: "\"採用しなかった知見と理由:\" の項目が `<知見>: <理由>` の形ではありません"}},
		},
		"知見が空": {
			"採用しなかった知見と理由:\n- : 理由\n", rejectedLabel, true,
			[]lintreport.Finding{{Line: 2, Message: "\"採用しなかった知見と理由:\" の項目が `<知見>: <理由>` の形ではありません"}},
		},
		"コロンの後に空白が無い": {
			"採用しなかった知見と理由:\n- A:理由\n", rejectedLabel, true,
			[]lintreport.Finding{{Line: 2, Message: "\"採用しなかった知見と理由:\" の項目が `<知見>: <理由>` の形ではありません"}},
		},
		"項目の無い文だけなら、項目が無い違反だけを出す": {
			"採用した知見:\n本文\n本文 2\n", adoptedLabel, false,
			[]lintreport.Finding{{Line: 1, Message: `"採用した知見:" の後に項目がありません`}},
		},
		"採用しなかった知見の一覧は、採用した知見の label で終わる": {
			"採用しなかった知見と理由:\n- A: 理由\n採用した知見:\n- B\n", rejectedLabel, true, nil,
		},
		"7段の # と空白の無い # は見出しでなく、空白の無い - は項目でない": {
			"採用した知見:\n- A\n#hash\n####### x\n-x\n- B\n", adoptedLabel, false,
			[]lintreport.Finding{
				{Line: 3, Message: `"採用した知見:" の一覧の途中に項目でない行があります`},
				{Line: 4, Message: `"採用した知見:" の一覧の途中に項目でない行があります`},
				{Line: 5, Message: `"採用した知見:" の一覧の途中に項目でない行があります`},
			},
		},
		"行頭がコメントの見出しの形では一覧が終わらない": {
			"採用した知見:\n- A\n<!-- -->### 小\n- B\n", adoptedLabel, false,
			[]lintreport.Finding{{Line: 3, Message: `"採用した知見:" の一覧の途中に項目でない行があります`}},
		},
		"採用しなかった知見のハイフンだけの項目は、本文が空の違反だけを出す": {
			"採用しなかった知見と理由:\n- A: 理由\n-\n", rejectedLabel, true,
			[]lintreport.Finding{{Line: 3, Message: `"採用しなかった知見と理由:" の項目の本文が空です`}},
		},
		"理由を求めなければコロンを見ない": {
			"採用した知見:\n- A\n", adoptedLabel, false, nil,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, checkList(lines(c.body), c.label, c.needReason))
		})
	}
}

func Test_isLabel(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		text string
		want bool
	}{
		"label だけ":  {adoptedLabel, true},
		"後ろの空白を許す":  {adoptedLabel + " \t", true},
		"前の空白は許さない": {" " + adoptedLabel, false},
		"語が続く":      {adoptedLabel + "x", false},
		"別の label":  {rejectedLabel, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, isLabel(c.text, adoptedLabel))
		})
	}
}

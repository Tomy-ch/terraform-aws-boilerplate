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

func lines(body string) []mdscan.Line {
	return mdscan.Lines([]byte(body))
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
		"nil":     {nil, 0},
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

	t.Run("引数の誤りは errUsage", func(t *testing.T) {
		t.Parallel()
		for _, args := range [][]string{{"-nope"}, {"extra"}} {
			require.ErrorIs(t, run(args, &bytes.Buffer{}, fixedLister(nil, nil)), errUsage)
		}
	})
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

	got := trackedTargets([]string{
		"usecases/b/README.md",
		"usecases/a/tests/x.tftest.hcl",
		"usecases/a/main.tf",
		"usecases/top.md",
		"modules/m/README.md",
		"usecasesx/c/README.md",
	}, "usecases")
	assert.Equal(t, []string{"a", "b"}, got, "直下のファイルと、名前が接頭辞で一致するだけの root を数えない")
	assert.Empty(t, trackedTargets(nil, "usecases"))
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

	assert.Equal(t, []string{"a", "c"}, difference([]string{"a", "b", "c"}, []string{"b"}))
	assert.Nil(t, difference([]string{"a"}, []string{"a"}))
	assert.Nil(t, difference(nil, []string{"a"}))
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

	t.Run("コメントの中にだけ節があれば見出しが無い違反", func(t *testing.T) {
		t.Parallel()
		got, err := checkReadme("R.md", []byte(readmeWith("<!--\n"+validSection+"-->\n")))
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, 1, got[0].Line)
		assert.Contains(t, got[0].Message, "見出しがありません")
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

	ls := lines("## 公式実装の参照\na\n### 小見出し\nb\n## 次\nc\n")
	got := section(ls, 0)
	require.Len(t, got, 3, "3段目の見出しでは終わらない")
	assert.Equal(t, "a", got[0].Text)
	assert.Equal(t, "b", got[2].Text)

	ls = lines("## 公式実装の参照\na\n# 1段目\n")
	assert.Len(t, section(ls, 0), 1, "1段目の見出しで終わる")

	ls = lines("## 公式実装の参照\na\nb")
	assert.Len(t, section(ls, 0), 2, "見出しが続かなければ末尾まで")
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
		"見出しだけ":   {heading, true},
		"空白が続く":   {heading + " <!-- x -->", true},
		"括弧書きが続く": {heading + "（ADR-0206 決定7・8）", true},
		"語が続く":    {heading + "メモ", false},
		"3段目":     {"#" + heading, false},
		"行頭でない":   {" " + heading, false},
		"別の見出し":   {"## 公式実装", false},
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

func Test_visibleLines(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		body     string
		want     []string
		openedAt int
	}{
		"コメントが無ければそのまま": {"a\nb", []string{"a", "b"}, 0},
		"1行のコメントは空になる":  {"<!-- a -->\nb", []string{"", "b"}, 0},
		"行末のコメントを外す":    {"- A <!-- sample:line -->", []string{"- A "}, 0},
		"複数行のコメントの中は空になる": {
			"a\n<!--\n## x\n-->\nb", []string{"a", "", "", "", "b"}, 0,
		},
		"閉じた後ろの文字は残す":                {"<!-- a\nb --> c", []string{"", " c"}, 0},
		"1行に2つのコメント":                 {"a<!-- x -->b<!-- y -->c", []string{"abc"}, 0},
		"inline code の中の開きは開かない":     {"`<!--` a\nb", []string{"`<!--` a", "b"}, 0},
		"閉じない inline code の後ろの開きは開く": {"`a <!-- b\nc -->", []string{"`a ", ""}, 0},
		"閉じなければ開いた行を返す":              {"a\n<!-- b\nc", []string{"a", "", ""}, 2},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, openedAt := visibleLines(lines(c.body))
			texts := make([]string, 0, len(got))
			for i, l := range got {
				assert.Equal(t, i+1, l.Number, "行の番号を保つ")
				texts = append(texts, l.Text)
			}
			assert.Equal(t, c.want, texts)
			assert.Equal(t, c.openedAt, openedAt)
		})
	}
}

func Test_backtickRun(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		s    string
		want int
	}{
		"先頭がバッククォートでない": {"a`", 0},
		"1つ":   {"`a`", 1},
		"3つ続く": {"```", 3},
		"空":    {"", 0},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, backtickRun(c.s))
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

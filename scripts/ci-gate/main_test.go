package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/checksdecl"
)

const branchesFixture = `{
  "deploy": [
    { "branch": "admin", "env": "admin", "from": ["release/"] },
    { "branch": "production", "release_base": true }
  ],
  "lines": { "release": "release/", "hotfix": "hotfix/" },
  "line_version": "v<major>.<minor>.<patch>",
  "revert": "revert/"
}`

const repoDecl = `{"checks": [
  {"name": "always", "workflow": "always.yaml", "paths": [], "scope": "change", "required": true},
  {"name": "go", "workflow": "go.yaml", "paths": ["**/*.go"], "scope": "tree", "required": true}
]}`

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	base := []string{"-C", dir, "-c", "user.email=t@example.com", "-c", "user.name=t", "-c", "commit.gpgsign=false"}
	//nolint:gosec // 引数は本ファイル内のリテラルと t.TempDir のパス
	out, err := exec.CommandContext(t.Context(), "git", append(base, args...)...).CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
	return strings.TrimSpace(string(out))
}

func writeFile(t *testing.T, dir, rel, body string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o750))
	require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
}

// useRepo は、base の commit に宣言を置き、head の commit で files を足したリポジトリを作ってそこへ移る。
// base と head の SHA を返す。
func useRepo(t *testing.T, declBody string, files map[string]string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q")
	writeFile(t, dir, checksdecl.File, declBody)
	writeFile(t, dir, ".github/settings/branches.json", branchesFixture)
	writeFile(t, dir, "README.md", "x\n")
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-q", "-m", "base")
	base := gitIn(t, dir, "rev-parse", "HEAD")
	for rel, body := range files {
		writeFile(t, dir, rel, body)
	}
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-q", "--allow-empty", "-m", "head")
	head := gitIn(t, dir, "rev-parse", "HEAD")
	t.Chdir(dir)
	return base, head
}

func envOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func Test_run(t *testing.T) {
	//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
	t.Run("正常系", func(t *testing.T) {
		//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
		t.Run("changes は走るべき検査を GITHUB_OUTPUT へ書く", func(t *testing.T) {
			base, head := useRepo(t, repoDecl, map[string]string{"a/b.go": "package b\n"})
			outFile := filepath.Join(t.TempDir(), "out")
			var out bytes.Buffer
			require.NoError(t, run([]string{"changes", "--decl-rev", base, "--base", base, "--head", head, "--base-ref", "release/v0.2.0", "--output", outFile}, envOf(nil), &out))
			got, err := os.ReadFile(outFile) //nolint:gosec // t.TempDir の下
			require.NoError(t, err)
			assert.Equal(t, "run=[\"always\",\"go\"]\n", string(got))
			assert.Contains(t, out.String(), "変更したファイル 1 件")
		})

		//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
		t.Run("judge は needs が決め直した集合と合えば合格し、表を summary へ書く", func(t *testing.T) {
			base, head := useRepo(t, repoDecl, map[string]string{"docs/a.md": "x\n"})
			summary := filepath.Join(t.TempDir(), "summary")
			needs := `{"changes": {"result": "success", "outputs": {"run": "[\"always\"]"}}, "call-always": {"result": "success"}, "call-go": {"result": "skipped"}}`
			var out bytes.Buffer
			require.NoError(t, run([]string{"judge", "--decl-rev", base, "--base", base, "--head", head, "--base-ref", "release/v0.2.0", "--summary", summary}, envOf(map[string]string{"NEEDS": needs}), &out))
			got, err := os.ReadFile(summary) //nolint:gosec // t.TempDir の下
			require.NoError(t, err)
			assert.Contains(t, string(got), "| go | はい | いいえ | skipped | ✅ |")
		})

		//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
		t.Run("line は線への push で木の検査が success なら合格", func(t *testing.T) {
			_, head := useRepo(t, repoDecl, nil)
			needs := `{"call-go": {"result": "success"}}`
			var out bytes.Buffer
			require.NoError(t, run([]string{"line", "--decl-rev", head, "--event", "push", "--ref", "refs/heads/release/v0.2.0"}, envOf(map[string]string{"NEEDS": needs}), &out))
		})
	})

	//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
	t.Run("異常系", func(t *testing.T) {
		//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
		t.Run("宣言を PR の木ではなく decl-rev の木から読む", func(t *testing.T) {
			base, head := useRepo(t, repoDecl, map[string]string{
				"a/b.go":        "package b\n",
				checksdecl.File: `{"checks": [{"name": "always", "workflow": "always.yaml", "paths": [], "scope": "change", "required": true}]}`,
			})
			needs := `{"changes": {"result": "success", "outputs": {"run": "[\"always\"]"}}, "call-always": {"result": "success"}}`
			var out bytes.Buffer
			err := run([]string{"judge", "--decl-rev", base, "--base", base, "--head", head, "--base-ref", "release/v0.2.0"}, envOf(map[string]string{"NEEDS": needs}), &out)
			require.ErrorIs(t, err, errNeeds)
		})

		//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
		t.Run("judge は走るべき検査が skipped なら不合格", func(t *testing.T) {
			base, head := useRepo(t, repoDecl, map[string]string{"a/b.go": "package b\n"})
			needs := `{"changes": {"result": "success", "outputs": {"run": "[\"always\",\"go\"]"}}, "call-always": {"result": "success"}, "call-go": {"result": "skipped"}}`
			var out bytes.Buffer
			err := run([]string{"judge", "--decl-rev", base, "--base", base, "--head", head, "--base-ref", "release/v0.2.0"}, envOf(map[string]string{"NEEDS": needs}), &out)
			require.ErrorIs(t, err, errFailed)
			assert.Contains(t, out.String(), "| go | はい | はい | skipped | ❌ |")
		})

		//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
		t.Run("judge は NEEDS が無ければ誤りにする", func(t *testing.T) {
			base, head := useRepo(t, repoDecl, nil)
			var out bytes.Buffer
			err := run([]string{"judge", "--decl-rev", base, "--base", base, "--head", head, "--base-ref", "release/v0.2.0"}, envOf(nil), &out)
			require.ErrorIs(t, err, errNeeds)
		})

		for name, args := range map[string][]string{
			"push でない":  {"--event", "pull_request", "--ref", "refs/heads/release/v0.2.0"},
			"ref が線でない": {"--event", "push", "--ref", "refs/heads/feature/x"},
			"ref がタグ":   {"--event", "push", "--ref", "refs/tags/release/v0.2.0"},
		} {
			//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
			t.Run("line は"+name+"なら誤りにする", func(t *testing.T) {
				_, head := useRepo(t, repoDecl, nil)
				var out bytes.Buffer
				err := run(append([]string{"line", "--decl-rev", head}, args...), envOf(map[string]string{"NEEDS": `{"call-go": {"result": "success"}}`}), &out)
				require.ErrorIs(t, err, errEvent)
			})
		}

		//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
		t.Run("宣言に検査が無ければ合格と区別する", func(t *testing.T) {
			_, head := useRepo(t, `{"checks": []}`, nil)
			var out bytes.Buffer
			err := run([]string{"line", "--decl-rev", head, "--event", "push", "--ref", "refs/heads/release/v0.2.0"}, envOf(nil), &out)
			require.ErrorIs(t, err, checksdecl.ErrNoChecks)
		})

		for name, args := range map[string][]string{
			"サブコマンドが無い":    {},
			"未知のサブコマンド":    {"x", "--decl-rev", "HEAD"},
			"decl-rev が無い": {"line"},
			"base が無い":     {"changes", "--decl-rev", "HEAD", "--head", "HEAD", "--base-ref", "release/v0.2.0"},
			"未知のフラグ":       {"line", "--x"},
		} {
			//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
			t.Run(name+"なら使い方の誤りにする", func(t *testing.T) {
				useRepo(t, repoDecl, nil)
				var out bytes.Buffer
				require.ErrorIs(t, run(args, envOf(nil), &out), errUsage)
			})
		}
	})
}

func Test_readDecls(t *testing.T) {
	//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
	t.Run("正常系", func(t *testing.T) {
		//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
		t.Run("rev の木から2つの宣言を読む", func(t *testing.T) {
			_, head := useRepo(t, repoDecl, nil)
			d, b, err := readDecls(head)
			require.NoError(t, err)
			assert.Equal(t, []string{"always", "go"}, d.Names())
			assert.True(t, b.IsLine("release/v0.2.0"))
		})
	})

	//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
	t.Run("異常系", func(t *testing.T) {
		//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
		t.Run("rev に宣言が無ければ誤りにする", func(t *testing.T) {
			useRepo(t, repoDecl, nil)
			_, _, err := readDecls("no-such-rev")
			require.ErrorIs(t, err, errGit)
		})

		//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
		t.Run("分岐の宣言を解釈できなければ誤りにする", func(t *testing.T) {
			_, head := useRepo(t, repoDecl, map[string]string{".github/settings/branches.json": "{"})
			_, _, err := readDecls(head)
			require.Error(t, err)
		})

		//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
		t.Run("検査の宣言を解釈できなければ誤りにする", func(t *testing.T) {
			_, head := useRepo(t, repoDecl, map[string]string{checksdecl.File: "{"})
			_, _, err := readDecls(head)
			require.ErrorIs(t, err, checksdecl.ErrDecl)
		})
	})
}

func Test_git(t *testing.T) {
	//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
	t.Run("正常系", func(t *testing.T) {
		//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
		t.Run("標準出力を返す", func(t *testing.T) {
			_, head := useRepo(t, repoDecl, nil)
			out, err := git("rev-parse", "HEAD")
			require.NoError(t, err)
			assert.Equal(t, head, strings.TrimSpace(string(out)))
		})
	})

	//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
	t.Run("異常系", func(t *testing.T) {
		//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
		t.Run("失敗は標準エラーを含めて返す", func(t *testing.T) {
			useRepo(t, repoDecl, nil)
			_, err := git("rev-parse", "no-such-rev")
			require.ErrorIs(t, err, errGit)
			assert.Contains(t, err.Error(), "no-such-rev")
		})
	})
}

func Test_changedFiles(t *testing.T) {
	//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
	t.Run("正常系", func(t *testing.T) {
		//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
		t.Run("rename は旧い path の削除と新しい path の追加に分ける", func(t *testing.T) {
			dir := t.TempDir()
			gitIn(t, dir, "init", "-q")
			writeFile(t, dir, "scripts/a.go", "package a\n\n// 本文\nfunc A() {}\n")
			gitIn(t, dir, "add", "-A")
			gitIn(t, dir, "commit", "-q", "-m", "base")
			base := gitIn(t, dir, "rev-parse", "HEAD")
			require.NoError(t, os.MkdirAll(filepath.Join(dir, "docs"), 0o750))
			gitIn(t, dir, "mv", "scripts/a.go", "docs/a.go.txt")
			gitIn(t, dir, "commit", "-q", "-m", "mv")
			t.Chdir(dir)

			got, err := changedFiles(base, "HEAD")
			require.NoError(t, err)
			assert.ElementsMatch(t, []string{"scripts/a.go", "docs/a.go.txt"}, got)
		})

		//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
		t.Run("差分が無ければ空を返す", func(t *testing.T) {
			_, head := useRepo(t, repoDecl, nil)
			got, err := changedFiles(head, head)
			require.NoError(t, err)
			assert.Empty(t, got)
		})
	})

	//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
	t.Run("異常系", func(t *testing.T) {
		// 浅い clone で merge-base が無いと、差分が取れない。空として読むと検査を省く。
		//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
		t.Run("merge-base を取れなければ誤りにする", func(t *testing.T) {
			_, head := useRepo(t, repoDecl, nil)
			_, err := changedFiles("no-such-rev", head)
			require.ErrorIs(t, err, errGit)
		})
	})
}

func Test_splitZ(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("NUL で区切り空の要素を落とす", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, []string{"a", "b c"}, splitZ([]byte("a\x00b c\x00")))
			assert.Empty(t, splitZ(nil))
		})
	})
}

func Test_fileAt(t *testing.T) {
	//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
	t.Run("正常系", func(t *testing.T) {
		//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
		t.Run("rev の木のファイルの中身を返す", func(t *testing.T) {
			base, _ := useRepo(t, repoDecl, map[string]string{"README.md": "y\n"})
			got, err := fileAt(base, "README.md")
			require.NoError(t, err)
			assert.Equal(t, "x\n", string(got))
		})
	})

	//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
	t.Run("異常系", func(t *testing.T) {
		//nolint:paralleltest // 親が t.Chdir を使うため並列化不可
		t.Run("木に無い path は誤りにする", func(t *testing.T) {
			_, head := useRepo(t, repoDecl, nil)
			_, err := fileAt(head, "no/such")
			require.ErrorIs(t, err, errGit)
		})
	})
}

func Test_writeChanges(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("既にある内容へ追記する", func(t *testing.T) {
			t.Parallel()
			p := filepath.Join(t.TempDir(), "out")
			require.NoError(t, os.WriteFile(p, []byte("x=1\n"), 0o600))
			var out bytes.Buffer
			require.NoError(t, writeChanges(p, []string{"a"}, 3, &out))
			got, err := os.ReadFile(p) //nolint:gosec // t.TempDir の下
			require.NoError(t, err)
			assert.Equal(t, "x=1\nrun=[\"a\"]\n", string(got))
		})

		t.Run("出力先が無ければ標準出力だけに書く", func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			require.NoError(t, writeChanges("", nil, 0, &out))
			assert.Contains(t, out.String(), "走るべき検査 0 件")
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("出力先を開けなければ誤りにする", func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			require.Error(t, writeChanges(filepath.Join(t.TempDir(), "no", "dir"), nil, 0, &out))
		})
	})
}

func Test_report(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("全行を書き verdict を返す", func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			verdict := errFailed
			err := report("", "gate", []row{{Name: "a", Required: true, Should: true, Result: "failure"}, {Name: "b", OK: true, Result: "skipped"}}, verdict, &out)
			require.ErrorIs(t, err, errFailed)
			assert.Contains(t, out.String(), "| a | はい | はい | failure | ❌ |")
			assert.Contains(t, out.String(), "| b | いいえ | いいえ | skipped | ✅ |")
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("summary を開けなければ誤りにする", func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			err := report(filepath.Join(t.TempDir(), "no", "dir"), "gate", nil, nil, &out)
			require.Error(t, err)
			assert.NotErrorIs(t, err, errFailed)
		})
	})
}

func Test_yesNo(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("真偽を日本語で返す", func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, "はい", yesNo(true))
			assert.Equal(t, "いいえ", yesNo(false))
		})
	})
}

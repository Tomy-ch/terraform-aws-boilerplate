package main

import (
	"bytes"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/branchdecl"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/checksdecl"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
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

// addedDecl は、repoDecl に、paths の無い required の検査 new を足した宣言。
const addedDecl = `{"checks": [
	{"name": "always", "workflow": "always.yaml", "paths": [], "scope": "change", "required": true},
	{"name": "go", "workflow": "go.yaml", "paths": ["**/*.go"], "scope": "tree", "required": true},
	{"name": "new", "workflow": "new.yaml", "paths": [], "scope": "tree", "required": true}
]}`

// fakeGit は、rev ごとの木と、rng の差分で変わったファイルを持つ偽の git。numstat が nil でなければ、--numstat は
// files の代わりにそれを返す。diff は ci-gate が組み立てる引数列だけを受け付ける。
type fakeGit struct {
	trees   map[string]map[string]string
	rng     string
	files   []string
	numstat []string
}

func (f fakeGit) reader() gitReader {
	return gitReader{
		show: func(rev, path string) ([]byte, error) {
			body, ok := f.trees[rev][path]
			if !ok {
				return nil, xerrors.Wrapf(errGit, "偽の git: %s:%s がありません", rev, path)
			}
			return []byte(body), nil
		},
		diff: func(args ...string) ([]byte, error) {
			var b strings.Builder
			switch {
			case slices.Equal(args, []string{"--no-renames", "--name-only", "-z", f.rng}):
				for _, p := range f.files {
					b.WriteString(p + "\x00")
				}
			case slices.Equal(args, []string{"--no-renames", "--numstat", "-z", f.rng}):
				stat := f.files
				if f.numstat != nil {
					stat = f.numstat
				}
				for _, p := range stat {
					b.WriteString("1\t0\t" + p + "\x00")
				}
			default:
				return nil, xerrors.Wrapf(errGit, "偽の git: diff %s を受け付けません", strings.Join(args, " "))
			}
			return []byte(b.String()), nil
		},
	}
}

// repoOf は、rev base の木に宣言を置き、rev head の木で files を足した偽の git を返す。base...head の差分は files。
func repoOf(declBody string, files map[string]string) fakeGit {
	base := map[string]string{checksdecl.File: declBody, branchdecl.File: branchesFixture, "README.md": "x\n"}
	head := maps.Clone(base)
	maps.Copy(head, files)
	return fakeGit{trees: map[string]map[string]string{"base": base, "head": head}, rng: "base...head", files: slices.Sorted(maps.Keys(files))}
}

func envOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

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

// commitRepo は、t.TempDir に実物のリポジトリを作り、base の commit に files を置く。dir と base の SHA を返す。
func commitRepo(t *testing.T, files map[string]string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q")
	for rel, body := range files {
		writeFile(t, dir, rel, body)
	}
	gitIn(t, dir, "add", "-A")
	gitIn(t, dir, "commit", "-q", "--allow-empty", "-m", "base")
	return dir, gitIn(t, dir, "rev-parse", "HEAD")
}

func Test_run(t *testing.T) {
	t.Parallel()

	prArgs := func(sub string, extra ...string) []string {
		return append([]string{sub, "--decl-rev", "base", "--base", "base", "--head", "head", "--base-ref", "release/v0.2.0"}, extra...)
	}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("changes は走るべき検査を GITHUB_OUTPUT へ書く", func(t *testing.T) {
			t.Parallel()
			outFile := filepath.Join(t.TempDir(), "out")
			var out bytes.Buffer
			require.NoError(t, run(prArgs("changes", "--output", outFile), repoOf(repoDecl, map[string]string{"a/b.go": "package b\n"}).reader(), envOf(nil), &out))
			got, err := os.ReadFile(outFile) //nolint:gosec // t.TempDir の下
			require.NoError(t, err)
			assert.Equal(t, "run=[\"always\",\"go\"]\n", string(got))
			assert.Contains(t, out.String(), "変更したファイル 1 件")
		})

		t.Run("changes は PR の木で足した検査を head の宣言で選び、外した検査を run に入れない", func(t *testing.T) {
			t.Parallel()
			outFile := filepath.Join(t.TempDir(), "out")
			var out bytes.Buffer
			g := repoOf(repoDecl, map[string]string{"a/b.go": "package b\n", checksdecl.File: addedDecl})
			require.NoError(t, run(prArgs("changes", "--output", outFile), g.reader(), envOf(nil), &out))
			got, err := os.ReadFile(outFile) //nolint:gosec // t.TempDir の下
			require.NoError(t, err)
			assert.Equal(t, "run=[\"always\",\"go\",\"new\"]\n", string(got))
		})

		t.Run("judge は PR の木で外した検査を判定せず、表に載せる", func(t *testing.T) {
			t.Parallel()
			g := repoOf(repoDecl, map[string]string{
				"a/b.go":        "package b\n",
				checksdecl.File: `{"checks": [{"name": "always", "workflow": "always.yaml", "paths": [], "scope": "change", "required": true}]}`,
			})
			needs := `{"changes": {"result": "success", "outputs": {"run": "[\"always\"]"}}, "call-always": {"result": "success"}}`
			var out bytes.Buffer
			require.NoError(t, run(prArgs("judge"), g.reader(), envOf(map[string]string{"NEEDS": needs}), &out))
			assert.Contains(t, out.String(), "| go | いいえ | いいえ | 宣言から外した | ✅ |")
		})

		t.Run("judge は needs が決め直した集合と合えば合格し、表を summary へ書く", func(t *testing.T) {
			t.Parallel()
			summary := filepath.Join(t.TempDir(), "summary")
			needs := `{"changes": {"result": "success", "outputs": {"run": "[\"always\"]"}}, "call-always": {"result": "success"}, "call-go": {"result": "skipped"}}`
			var out bytes.Buffer
			require.NoError(t, run(prArgs("judge", "--summary", summary), repoOf(repoDecl, map[string]string{"docs/a.md": "x\n"}).reader(), envOf(map[string]string{"NEEDS": needs}), &out))
			got, err := os.ReadFile(summary) //nolint:gosec // t.TempDir の下
			require.NoError(t, err)
			assert.Contains(t, string(got), "| go | はい | いいえ | skipped | ✅ |")
		})

		t.Run("line は線への push で木の検査が success なら合格", func(t *testing.T) {
			t.Parallel()
			needs := `{"call-go": {"result": "success"}}`
			var out bytes.Buffer
			require.NoError(t, run([]string{"line", "--decl-rev", "head", "--event", "push", "--ref", "refs/heads/release/v0.2.0"}, repoOf(repoDecl, nil).reader(), envOf(map[string]string{"NEEDS": needs}), &out))
			assert.Contains(t, out.String(), "| go | はい | はい | success | ✅ |")
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("PR の木で paths と required を緩めても base の宣言で判定する", func(t *testing.T) {
			t.Parallel()
			g := repoOf(repoDecl, map[string]string{
				"a/b.go": "package b\n",
				checksdecl.File: `{"checks": [
	{"name": "always", "workflow": "always.yaml", "paths": [], "scope": "change", "required": true},
	{"name": "go", "workflow": "go.yaml", "paths": ["none/**"], "scope": "tree", "required": false}
]}`,
			})
			needs := `{"changes": {"result": "success", "outputs": {"run": "[\"always\",\"go\"]"}}, "call-always": {"result": "success"}, "call-go": {"result": "skipped"}}`
			var out bytes.Buffer
			err := run(prArgs("judge"), g.reader(), envOf(map[string]string{"NEEDS": needs}), &out)
			require.ErrorIs(t, err, errFailed)
			assert.Contains(t, out.String(), "| go | はい | はい | skipped | ❌ |")
		})

		t.Run("judge は PR の木で足した検査の job が needs に無ければ誤りにする", func(t *testing.T) {
			t.Parallel()
			needs := `{"changes": {"result": "success", "outputs": {"run": "[\"always\",\"new\"]"}}, "call-always": {"result": "success"}, "call-go": {"result": "skipped"}}`
			var out bytes.Buffer
			err := run(prArgs("judge"), repoOf(repoDecl, map[string]string{checksdecl.File: addedDecl}).reader(), envOf(map[string]string{"NEEDS": needs}), &out)
			require.ErrorIs(t, err, errNeeds)
		})

		t.Run("judge は PR の木で足した required の検査が skipped なら不合格", func(t *testing.T) {
			t.Parallel()
			needs := `{"changes": {"result": "success", "outputs": {"run": "[\"always\",\"new\"]"}}, "call-always": {"result": "success"}, "call-go": {"result": "skipped"}, "call-new": {"result": "skipped"}}`
			var out bytes.Buffer
			err := run(prArgs("judge"), repoOf(repoDecl, map[string]string{checksdecl.File: addedDecl}).reader(), envOf(map[string]string{"NEEDS": needs}), &out)
			require.ErrorIs(t, err, errFailed)
			assert.Contains(t, out.String(), "| new | はい | はい | skipped | ❌ |")
		})

		for _, sub := range []string{"changes", "judge"} {
			t.Run(sub+" は PR の木の検査の宣言を読めなければ誤りにする", func(t *testing.T) {
				t.Parallel()
				var out bytes.Buffer
				err := run(prArgs(sub, "--output", filepath.Join(t.TempDir(), "out")), repoOf(repoDecl, map[string]string{checksdecl.File: "{"}).reader(),
					envOf(map[string]string{"NEEDS": `{"changes": {"result": "success"}}`}), &out)
				require.ErrorIs(t, err, checksdecl.ErrDecl)
			})
		}

		t.Run("PR の木に検査の宣言が無ければ誤りにする", func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			args := []string{"changes", "--decl-rev", "base", "--base", "base", "--head", "no-such-rev", "--base-ref", "release/v0.2.0", "--output", filepath.Join(t.TempDir(), "out")}
			require.ErrorIs(t, run(args, repoOf(repoDecl, nil).reader(), envOf(nil), &out), errGit)
		})

		t.Run("差分を取れなければ誤りにする", func(t *testing.T) {
			t.Parallel()
			g := repoOf(repoDecl, nil)
			g.rng = "other...head"
			var out bytes.Buffer
			require.ErrorIs(t, run(prArgs("changes", "--output", filepath.Join(t.TempDir(), "out")), g.reader(), envOf(nil), &out), errGit)
		})

		t.Run("changes は --output が空なら使い方の誤りにする", func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			require.ErrorIs(t, run(prArgs("changes", "--output", ""), repoOf(repoDecl, nil).reader(), envOf(nil), &out), errUsage)
		})

		t.Run("judge は走るべき検査が skipped なら不合格", func(t *testing.T) {
			t.Parallel()
			needs := `{"changes": {"result": "success", "outputs": {"run": "[\"always\",\"go\"]"}}, "call-always": {"result": "success"}, "call-go": {"result": "skipped"}}`
			var out bytes.Buffer
			err := run(prArgs("judge"), repoOf(repoDecl, map[string]string{"a/b.go": "package b\n"}).reader(), envOf(map[string]string{"NEEDS": needs}), &out)
			require.ErrorIs(t, err, errFailed)
			assert.Contains(t, out.String(), "| go | はい | はい | skipped | ❌ |")
		})

		t.Run("judge は NEEDS が無ければ誤りにする", func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			require.ErrorIs(t, run(prArgs("judge"), repoOf(repoDecl, nil).reader(), envOf(nil), &out), errNeeds)
		})

		t.Run("line は NEEDS が無ければ誤りにする", func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			err := run([]string{"line", "--decl-rev", "head", "--event", "push", "--ref", "refs/heads/release/v0.2.0"}, repoOf(repoDecl, nil).reader(), envOf(nil), &out)
			require.ErrorIs(t, err, errNeeds)
		})

		for name, args := range map[string][]string{
			"push でない":  {"--event", "pull_request", "--ref", "refs/heads/release/v0.2.0"},
			"ref が線でない": {"--event", "push", "--ref", "refs/heads/feature/x"},
			"ref がタグ":   {"--event", "push", "--ref", "refs/tags/release/v0.2.0"},
		} {
			t.Run("line は"+name+"なら誤りにする", func(t *testing.T) {
				t.Parallel()
				var out bytes.Buffer
				err := run(append([]string{"line", "--decl-rev", "head"}, args...), repoOf(repoDecl, nil).reader(), envOf(map[string]string{"NEEDS": `{"call-go": {"result": "success"}}`}), &out)
				require.ErrorIs(t, err, errEvent)
			})
		}

		t.Run("宣言に検査が無ければ合格と区別する", func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			err := run([]string{"line", "--decl-rev", "head", "--event", "push", "--ref", "refs/heads/release/v0.2.0"}, repoOf(`{"checks": []}`, nil).reader(), envOf(nil), &out)
			require.ErrorIs(t, err, checksdecl.ErrNoChecks)
		})

		// どれも --output を付け、名前の挙がった1つの理由だけで落ちる形にする。
		for name, args := range map[string][]string{
			"サブコマンドが無い":        {},
			"未知のサブコマンド":        {"x", "--decl-rev", "base"},
			"--decl-rev が無い":   {"line"},
			"--base だけが無い":     {"changes", "--decl-rev", "base", "--head", "head", "--base-ref", "release/v0.2.0", "--output", "out"},
			"--head だけが無い":     {"changes", "--decl-rev", "base", "--base", "base", "--base-ref", "release/v0.2.0", "--output", "out"},
			"--base-ref だけが無い": {"changes", "--decl-rev", "base", "--base", "base", "--head", "head", "--output", "out"},
			"未知のフラグ":           {"line", "--x"},
		} {
			t.Run(name+"なら使い方の誤りにする", func(t *testing.T) {
				t.Parallel()
				var out bytes.Buffer
				require.ErrorIs(t, run(args, repoOf(repoDecl, nil).reader(), envOf(nil), &out), errUsage)
			})
		}
	})
}

func Test_readChecks(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("rev の木の検査の宣言を読む", func(t *testing.T) {
			t.Parallel()
			g := repoOf(repoDecl, map[string]string{checksdecl.File: addedDecl}).reader()
			d, err := readChecks(g, "base")
			require.NoError(t, err)
			assert.Equal(t, []string{"always", "go"}, d.Names())
			d, err = readChecks(g, "head")
			require.NoError(t, err)
			assert.Equal(t, []string{"always", "go", "new"}, d.Names())
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("rev が無ければ git の誤りにする", func(t *testing.T) {
			t.Parallel()
			_, err := readChecks(repoOf(repoDecl, nil).reader(), "no-such-rev")
			require.ErrorIs(t, err, errGit)
		})

		t.Run("宣言を解釈できなければ誤りにする", func(t *testing.T) {
			t.Parallel()
			_, err := readChecks(repoOf(repoDecl, map[string]string{checksdecl.File: `{"checks": [{"name": "a"}]}`}).reader(), "head")
			require.ErrorIs(t, err, checksdecl.ErrDecl)
			assert.Contains(t, err.Error(), "head:"+checksdecl.File)
		})

		t.Run("宣言に検査が無ければ合格と区別する", func(t *testing.T) {
			t.Parallel()
			_, err := readChecks(repoOf(repoDecl, map[string]string{checksdecl.File: `{"checks": []}`}).reader(), "head")
			require.ErrorIs(t, err, checksdecl.ErrNoChecks)
		})
	})
}

func Test_readDecls(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("rev の木から2つの宣言を読む", func(t *testing.T) {
			t.Parallel()
			d, b, err := readDecls(repoOf(repoDecl, nil).reader(), "head")
			require.NoError(t, err)
			assert.Equal(t, []string{"always", "go"}, d.Names())
			assert.True(t, b.IsLine("release/v0.2.0"))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("rev に宣言が無ければ誤りにする", func(t *testing.T) {
			t.Parallel()
			_, _, err := readDecls(repoOf(repoDecl, nil).reader(), "no-such-rev")
			require.ErrorIs(t, err, errGit)
		})

		t.Run("rev に分岐の宣言が無ければ誤りにする", func(t *testing.T) {
			t.Parallel()
			g := repoOf(repoDecl, nil)
			delete(g.trees["head"], branchdecl.File)
			_, _, err := readDecls(g.reader(), "head")
			require.ErrorIs(t, err, errGit)
		})

		t.Run("分岐の宣言を解釈できなければ誤りにする", func(t *testing.T) {
			t.Parallel()
			_, _, err := readDecls(repoOf(repoDecl, map[string]string{branchdecl.File: "{"}).reader(), "head")
			require.ErrorIs(t, err, branchdecl.ErrDeclaration)
			assert.Contains(t, err.Error(), "head:"+branchdecl.File)
		})

		t.Run("分岐の宣言が不正なら誤りにする", func(t *testing.T) {
			t.Parallel()
			_, _, err := readDecls(repoOf(repoDecl, map[string]string{branchdecl.File: strings.Replace(branchesFixture, `"release/"`, `"release"`, 1)}).reader(), "head")
			require.ErrorIs(t, err, branchdecl.ErrDeclaration)
		})

		t.Run("検査の宣言を解釈できなければ誤りにする", func(t *testing.T) {
			t.Parallel()
			_, _, err := readDecls(repoOf(repoDecl, map[string]string{checksdecl.File: "{"}).reader(), "head")
			require.ErrorIs(t, err, checksdecl.ErrDecl)
		})
	})
}

func Test_hostGit(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("show は dir のリポジトリの rev の木のファイルを返す", func(t *testing.T) {
			t.Parallel()
			dir, base := commitRepo(t, map[string]string{"README.md": "x\n"})
			got, err := hostGit(dir).show(base, "README.md")
			require.NoError(t, err)
			assert.Equal(t, "x\n", string(got))
		})

		t.Run("diff は dir のリポジトリで git diff に引数を渡す", func(t *testing.T) {
			t.Parallel()
			dir, base := commitRepo(t, map[string]string{"README.md": "x\n"})
			writeFile(t, dir, "a.txt", "a\n")
			gitIn(t, dir, "add", "-A")
			gitIn(t, dir, "commit", "-q", "-m", "head")
			got, err := hostGit(dir).diff("--name-only", base, "HEAD")
			require.NoError(t, err)
			assert.Equal(t, "a.txt\n", string(got))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("木に無い path は誤りにする", func(t *testing.T) {
			t.Parallel()
			dir, base := commitRepo(t, nil)
			_, err := hostGit(dir).show(base, "no/such")
			require.ErrorIs(t, err, errGit)
		})
	})
}

func Test_git(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("dir のリポジトリで起動し標準出力を返す", func(t *testing.T) {
			t.Parallel()
			dir, base := commitRepo(t, nil)
			out, err := git(dir, "rev-parse", "HEAD")
			require.NoError(t, err)
			assert.Equal(t, base, strings.TrimSpace(string(out)))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("失敗は標準エラーを含めて返す", func(t *testing.T) {
			t.Parallel()
			dir, _ := commitRepo(t, nil)
			_, err := git(dir, "rev-parse", "no-such-rev")
			require.ErrorIs(t, err, errGit)
			assert.Contains(t, err.Error(), "no-such-rev")
		})
	})
}

func Test_changedFiles(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("rename は旧い path の削除と新しい path の追加に分ける", func(t *testing.T) {
			t.Parallel()
			dir, base := commitRepo(t, map[string]string{"scripts/a.go": "package a\n\n// 本文\nfunc A() {}\n"})
			require.NoError(t, os.MkdirAll(filepath.Join(dir, "docs"), 0o750))
			gitIn(t, dir, "mv", "scripts/a.go", "docs/a.go.txt")
			gitIn(t, dir, "commit", "-q", "-m", "mv")

			got, err := changedFiles(hostGit(dir), base, "HEAD")
			require.NoError(t, err)
			assert.ElementsMatch(t, []string{"scripts/a.go", "docs/a.go.txt"}, got)
		})

		t.Run("2つの経路の件数が合えば変わったファイルを返す", func(t *testing.T) {
			t.Parallel()
			got, err := changedFiles(repoOf(repoDecl, map[string]string{"a.go": "", "b c.md": ""}).reader(), "base", "head")
			require.NoError(t, err)
			assert.Equal(t, []string{"a.go", "b c.md"}, got)
		})

		t.Run("差分が無ければ空を返す", func(t *testing.T) {
			t.Parallel()
			got, err := changedFiles(repoOf(repoDecl, nil).reader(), "base", "head")
			require.NoError(t, err)
			assert.Empty(t, got)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		// 浅い clone で merge-base が無いと、差分が取れない。空として読むと検査を省く。
		t.Run("merge-base を取れなければ誤りにする", func(t *testing.T) {
			t.Parallel()
			dir, _ := commitRepo(t, nil)
			_, err := changedFiles(hostGit(dir), "no-such-rev", "HEAD")
			require.ErrorIs(t, err, errGit)
		})

		t.Run("--name-only と --numstat の件数が違えば誤りにする", func(t *testing.T) {
			t.Parallel()
			g := repoOf(repoDecl, map[string]string{"a.go": ""})
			g.numstat = []string{"a.go", "b.go"}
			_, err := changedFiles(g.reader(), "base", "head")
			require.ErrorIs(t, err, errDiffCount)
			assert.Contains(t, err.Error(), "--name-only 1 件 / --numstat 2 件")
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

		t.Run("走るべき検査が0件なら null ではなく空の配列を書く", func(t *testing.T) {
			t.Parallel()
			p := filepath.Join(t.TempDir(), "out")
			var out bytes.Buffer
			require.NoError(t, writeChanges(p, nil, 0, &out))
			got, err := os.ReadFile(p) //nolint:gosec // t.TempDir の下
			require.NoError(t, err)
			assert.Equal(t, "run=[]\n", string(got))
			assert.Contains(t, out.String(), "走るべき検査 0 件")
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("出力先が無ければ使い方の誤りにする", func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			require.ErrorIs(t, writeChanges("", nil, 0, &out), errUsage)
		})

		t.Run("出力先を開けなければ誤りにする", func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			require.ErrorIs(t, writeChanges(filepath.Join(t.TempDir(), "no", "dir"), nil, 0, &out), errWrite)
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

		t.Run("失敗の判定は summary にも表と ❌ の行を書き verdict を返す", func(t *testing.T) {
			t.Parallel()
			summary := filepath.Join(t.TempDir(), "summary.md")
			var out bytes.Buffer
			err := report(summary, "gate", []row{{Name: "a", Required: true, Should: true, Result: "failure"}}, errFailed, &out)
			require.ErrorIs(t, err, errFailed)
			raw, err := os.ReadFile(summary)
			require.NoError(t, err)
			got := string(raw)
			assert.Contains(t, got, "| 検査 | 合否に数える | 走るべき | 結果 | 判定 |\n")
			assert.Contains(t, got, "| a | はい | はい | failure | ❌ |\n")
			assert.Contains(t, got, "\n❌ "+errFailed.Error()+"\n")
			assert.Equal(t, got, out.String())
		})

		t.Run("summary が既にあれば追記し、前の内容を残す", func(t *testing.T) {
			t.Parallel()
			summary := filepath.Join(t.TempDir(), "summary.md")
			require.NoError(t, os.WriteFile(summary, []byte("prev\n"), 0o600))
			var out bytes.Buffer
			err := report(summary, "gate", []row{{Name: "a", Required: true, Should: true, Result: "failure"}}, errFailed, &out)
			require.ErrorIs(t, err, errFailed)
			raw, err := os.ReadFile(summary)
			require.NoError(t, err)
			assert.Equal(t, "prev\n"+out.String(), string(raw))
		})

		for _, title := range []string{"gate", "line-gate"} {
			t.Run(title+" の成功の判定は summary に全体を書く", func(t *testing.T) {
				t.Parallel()
				summary := filepath.Join(t.TempDir(), "summary.md")
				var out bytes.Buffer
				err := report(summary, title, []row{{Name: "a", Required: true, Should: true, OK: true, Result: "success"}}, nil, &out)
				require.NoError(t, err)
				raw, err := os.ReadFile(summary)
				require.NoError(t, err)
				assert.Equal(t, "## "+title+"\n\n"+
					"| 検査 | 合否に数える | 走るべき | 結果 | 判定 |\n| --- | --- | --- | --- | --- |\n"+
					"| a | はい | はい | success | ✅ |\n"+
					"\n✅ 1 件を確かめました\n", string(raw))
			})
		}
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("summary を開けなければ誤りにする", func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			err := report(filepath.Join(t.TempDir(), "no", "dir"), "gate", nil, nil, &out)
			require.ErrorIs(t, err, errWrite)
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

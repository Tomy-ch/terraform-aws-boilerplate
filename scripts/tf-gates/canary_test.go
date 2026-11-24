package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const canaryWarningJSON = `{"format_version":"1.0","valid":true,"error_count":0,"warning_count":1,"diagnostics":[
{"severity":"warning","summary":"Argument is deprecated","detail":"managed_policy_arns is deprecated.","address":"aws_iam_role.this",
"range":{"filename":"main.tf","start":{"line":4,"column":25,"byte":90},"end":{"line":4,"column":27,"byte":92}},
"snippet":{"context":"resource \"aws_iam_role\" \"this\"","code":"  managed_policy_arns = []","start_line":4,"highlight_start_offset":24,"highlight_end_offset":26,"values":[]}}]}`

// 偽の道具は書いた直後のファイルを exec せず、/bin/sh に読ませて起動する。並行のテストが別の偽の道具を書いている
// 間に fork した子がその書き込みの fd を持ち越すと、書いたばかりのファイルの exec が ETXTBSY で落ちるため。
// canaryTool は、main.tf が非推奨の引数を持てば警告を、持たなければ警告の無い結果を出す偽の terraform を作る。
// behaves が "silent" なら、非推奨の引数を書いても警告を出さない（Provider が警告をやめた形）。
func canaryTool(t *testing.T, behaves string) toolFunc {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "warn.json"), []byte(canaryWarningJSON), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "clean.json"), []byte(cleanJSON), 0o600))
	script := filepath.Join(dir, "terraform")
	body := `#!/bin/sh
[ "$1" = validate ] || exit 0
if [ "` + behaves + `" != silent ] && grep -q managed_policy_arns main.tf; then cat '` + filepath.Join(dir, "warn.json") + `'; else cat '` + filepath.Join(dir, "clean.json") + `'; fi
`
	require.NoError(t, os.WriteFile(script, []byte(body), 0o600))
	return func(d string, env []string, step string) *exec.Cmd {
		c := exec.CommandContext(context.Background(), "/bin/sh", script, step)
		c.Dir, c.Env = d, env
		return c
	}
}

func canaryArgs(t *testing.T, work string) []string {
	t.Helper()
	ref := tree(t, map[string]string{"versions.tf": "terraform {}\n", ".terraform.lock.hcl": "# lock\n"})
	return []string{"-data", t.TempDir(), "-work", work, "-reference", ref}
}

func Test_runValidateCanary(t *testing.T) {
	t.Parallel()

	t.Run("非推奨の引数で落ち、抑止の宣言ときれいな構成で通れば成功する", func(t *testing.T) {
		t.Parallel()
		var out bytes.Buffer
		require.NoError(t, runValidateCanary(canaryArgs(t, "tmp/w"), &out, canaryTool(t, ""), tree(t, nil)))
		assert.Contains(t, out.String(), "✅ tf-validate は AWS Provider の非推奨の引数で落ち")
	})

	t.Run("非推奨の引数で警告が出なければ失敗する", func(t *testing.T) {
		t.Parallel()
		var out bytes.Buffer
		err := runValidateCanary(canaryArgs(t, "tmp/w"), &out, canaryTool(t, "silent"), tree(t, nil))
		require.ErrorIs(t, err, errCanary)
		assert.Contains(t, err.Error(), "deprecated detail")
	})

	t.Run("init が落ちれば失敗する", func(t *testing.T) {
		t.Parallel()
		broken := func(d string, env []string, _ string) *exec.Cmd {
			c := exec.CommandContext(context.Background(), "false")
			c.Dir, c.Env = d, env
			return c
		}
		require.ErrorIs(t, runValidateCanary(canaryArgs(t, "tmp/w"), &bytes.Buffer{}, broken, tree(t, nil)), errCanary)
	})

	t.Run("reference の lock が無ければ失敗する", func(t *testing.T) {
		t.Parallel()
		ref := tree(t, map[string]string{"versions.tf": ""})
		args := []string{"-data", t.TempDir(), "-work", "tmp/w", "-reference", ref}
		err := runValidateCanary(args, &bytes.Buffer{}, canaryTool(t, ""), tree(t, nil))
		require.Error(t, err)
		require.NotErrorIs(t, err, errCanary)
	})

	t.Run("引数の誤りは errUsage", func(t *testing.T) {
		t.Parallel()
		for name, args := range map[string][]string{
			"-data が相対": {"-data", "d", "-work", "tmp/w", "-reference", "r"},
			"-work が無い": {"-data", "/d", "-reference", "r"},
			"余計な引数":     {"-data", "/d", "-work", "tmp/w", "-reference", "r", "x"},
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				require.ErrorIs(t, runValidateCanary(args, &bytes.Buffer{}, canaryTool(t, ""), tree(t, nil)), errUsage)
			})
		}
	})

	t.Run("-work が tmp/ の下の相対パスでなければ、何も消さず道具も起動せずに、-work を名指す errUsage で終了コードは 2", func(t *testing.T) {
		t.Parallel()
		for _, work := range []string{
			"", ".", "tmp", "tmp/", "tmp/.", "<root>/keep", "keep", "tmp/../keep", "../x", "TMP/x",
			"tmp/out", "tmp/out/w", "tmp/self",
		} {
			t.Run(work, func(t *testing.T) {
				t.Parallel()
				world, root, _ := workWorld(t)
				if work == "<root>/keep" {
					work = filepath.Join(root, "keep")
				}
				before := snapshot(t, world)
				tool, calls := fakeTool(t)
				err := runValidateCanary(canaryArgs(t, work), &bytes.Buffer{}, tool, root)
				require.ErrorIs(t, err, errUsage)
				assert.Equal(t, 2, exitCode(err))
				if work != "" {
					assert.Contains(t, err.Error(), fmt.Sprintf("-work %q", work))
				}
				assert.Empty(t, calls())
				assert.Equal(t, before, snapshot(t, world))
			})
		}
	})

	t.Run("-work を2度渡せば後の値を使う", func(t *testing.T) {
		t.Parallel()
		world, root, _ := workWorld(t)
		before := snapshot(t, world)
		tool, calls := fakeTool(t)
		args := append(canaryArgs(t, "tmp/c/work"), "-work", "../x")
		err := runValidateCanary(args, &bytes.Buffer{}, tool, root)
		require.ErrorIs(t, err, errUsage)
		assert.Contains(t, err.Error(), `-work "../x"`)
		assert.Empty(t, calls())
		assert.Equal(t, before, snapshot(t, world))

		args = append(canaryArgs(t, "../x"), "-work", "tmp/c/work")
		require.NoError(t, runValidateCanary(args, &bytes.Buffer{}, canaryTool(t, ""), root))
		assert.DirExists(t, filepath.Join(root, "tmp", "c", "work", "clean"))
	})

	t.Run("-work だけを作り直し、tmp/ 自身・兄弟・tmp/ の外を残す", func(t *testing.T) {
		t.Parallel()
		for _, work := range []string{"tmp/c/work", "./tmp/c/work"} {
			t.Run(work, func(t *testing.T) {
				t.Parallel()
				world, root, _ := workWorld(t)
				before := snapshot(t, world)
				require.NoError(t, runValidateCanary(canaryArgs(t, work), &bytes.Buffer{}, canaryTool(t, ""), root))
				after := snapshot(t, world)
				for rel, sum := range before {
					if !strings.HasPrefix(rel, filepath.Join("root", "tmp", "c", "work")) {
						assert.Equal(t, sum, after[rel], rel)
					}
				}
				assert.NotContains(t, after, filepath.Join("root", "tmp", "c", "work", "old"), "前回の中身は消す")
				assert.Contains(t, after, filepath.Join("root", "tmp", "c", "work", "clean"))
			})
		}
	})

	t.Run("root に tmp/ が無ければ作って通る", func(t *testing.T) {
		t.Parallel()
		root := tree(t, map[string]string{"keep/f": "keep"})
		require.NoError(t, runValidateCanary(canaryArgs(t, "tmp/c/work"), &bytes.Buffer{}, canaryTool(t, ""), root))
		assert.DirExists(t, filepath.Join(root, "tmp", "c", "work", "clean"))
		assert.FileExists(t, filepath.Join(root, "keep", "f"))
	})

	t.Run("workdir.Resolve が通す字面は、整えた tmp/ の下のパスに建てる", func(t *testing.T) {
		t.Parallel()
		for _, c := range []struct{ work, want string }{
			{"tmp//x", "tmp/x"}, {"tmp/x/", "tmp/x"}, {"tmp/x ", "tmp/x "}, {"tmp/self/w", "tmp/self/w"},
		} {
			t.Run(c.work, func(t *testing.T) {
				t.Parallel()
				_, root, _ := workWorld(t)
				require.NoError(t, runValidateCanary(canaryArgs(t, c.work), &bytes.Buffer{}, canaryTool(t, ""), root))
				assert.DirExists(t, filepath.Join(root, filepath.FromSlash(c.want), "clean"))
			})
		}
	})

	t.Run("tmp/ 自身が外への symlink なら、その先を tmp/ として通し、先の兄弟を残す", func(t *testing.T) {
		t.Parallel()
		world := tree(t, map[string]string{"root/keep/f": "keep", "elsewhere/c/data/f": "data", "elsewhere/stale.txt": "stale"})
		root, elsewhere := filepath.Join(world, "root"), filepath.Join(world, "elsewhere")
		require.NoError(t, os.Symlink(elsewhere, filepath.Join(root, "tmp")))
		require.NoError(t, runValidateCanary(canaryArgs(t, "tmp/c/work"), &bytes.Buffer{}, canaryTool(t, ""), root))
		assert.DirExists(t, filepath.Join(elsewhere, "c", "work", "clean"))
		assert.FileExists(t, filepath.Join(elsewhere, "c", "data", "f"))
		assert.FileExists(t, filepath.Join(elsewhere, "stale.txt"))
		assert.FileExists(t, filepath.Join(root, "keep", "f"))
	})
}

func Test_canaryCase(t *testing.T) {
	t.Parallel()

	t.Run("reference の版を写した root を建て、構成に引数を足す", func(t *testing.T) {
		t.Parallel()
		ref := tree(t, map[string]string{"versions.tf": "v\n", ".terraform.lock.hcl": "l\n"})
		base, err := canaryCase(t.TempDir(), "c", ref, "  x = 1\n")
		require.NoError(t, err)
		root := filepath.Join(base, "usecases/x/reference")
		for f, want := range map[string]string{"versions.tf": "v\n", ".terraform.lock.hcl": "l\n"} {
			got, err := os.ReadFile(filepath.Join(root, f))
			require.NoError(t, err)
			assert.Equal(t, want, string(got))
		}
		main, err := os.ReadFile(filepath.Join(root, "main.tf"))
		require.NoError(t, err)
		assert.Contains(t, string(main), "  assume_role_policy = \"{}\"\n  x = 1\n}\n")
	})
}

func Test_validateFixture(t *testing.T) {
	t.Parallel()

	t.Run("構成が無ければ errNoTargets", func(t *testing.T) {
		t.Parallel()
		base := tree(t, map[string]string{"usecases/.keep": ""})
		_, err := validateFixture(base, "/d", "i.yaml", canaryTool(t, ""))
		require.ErrorIs(t, err, errNoTargets)
	})
}

func Test_onlyWarningDetail(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		body, want string
		ok         bool
	}{
		"警告がちょうど1件": {body: canaryWarningJSON, want: "managed_policy_arns is deprecated.", ok: true},
		"警告が0件":     {body: cleanJSON},
		"警告が2件":     {body: awsWarningJSON},
		"JSON でない":  {body: "x"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			p := filepath.Join(t.TempDir(), "1.json")
			require.NoError(t, os.WriteFile(p, []byte(c.body), 0o600))
			got, err := onlyWarningDetail(p)
			if !c.ok {
				require.ErrorIs(t, err, errCanary)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, c.want, got)
		})
	}

	t.Run("読めなければ errCanary", func(t *testing.T) {
		t.Parallel()
		_, err := onlyWarningDetail(filepath.Join(t.TempDir(), "missing.json"))
		require.ErrorIs(t, err, errCanary)
	})
}

func Test_writeIgnore(t *testing.T) {
	t.Parallel()

	t.Run("loadIgnore が読める宣言を書く", func(t *testing.T) {
		t.Parallel()
		p := filepath.Join(t.TempDir(), "i.yaml")
		require.NoError(t, writeIgnore(p, "d: with \"quotes\"\nand lines"))
		got, err := loadIgnore(p)
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, "d: with \"quotes\"\nand lines", got[0].Detail)
		assert.Equal(t, 1, got[0].Occurrences)
	})
}

func Test_writeText(t *testing.T) {
	t.Parallel()

	t.Run("親のディレクトリを作って書く", func(t *testing.T) {
		t.Parallel()
		p := filepath.Join(t.TempDir(), "a", "b.txt")
		require.NoError(t, writeText(p, "x"))
		got, err := os.ReadFile(p)
		require.NoError(t, err)
		assert.Equal(t, "x", string(got))
	})

	t.Run("親がファイルなら失敗する", func(t *testing.T) {
		t.Parallel()
		f := filepath.Join(t.TempDir(), "f")
		require.NoError(t, os.WriteFile(f, nil, 0o600))
		require.Error(t, writeText(filepath.Join(f, "b.txt"), "x"))
	})
}

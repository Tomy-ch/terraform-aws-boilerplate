package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const canaryWarningJSON = `{"format_version":"1.0","valid":true,"error_count":0,"warning_count":1,"diagnostics":[
{"severity":"warning","summary":"Argument is deprecated","detail":"managed_policy_arns is deprecated.","address":"aws_iam_role.this",
"range":{"filename":"main.tf","start":{"line":4,"column":25,"byte":90},"end":{"line":4,"column":27,"byte":92}},
"snippet":{"context":"resource \"aws_iam_role\" \"this\"","code":"  managed_policy_arns = []","start_line":4,"highlight_start_offset":24,"highlight_end_offset":26,"values":[]}}]}`

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
	require.NoError(t, os.WriteFile(script, []byte(body), 0o700))
	return func(d string, env []string, step string) *exec.Cmd {
		c := exec.CommandContext(context.Background(), script, step)
		c.Dir, c.Env = d, env
		return c
	}
}

func canaryArgs(t *testing.T) []string {
	t.Helper()
	ref := tree(t, map[string]string{"versions.tf": "terraform {}\n", ".terraform.lock.hcl": "# lock\n"})
	return []string{"-data", t.TempDir(), "-work", filepath.Join(t.TempDir(), "w"), "-reference", ref}
}

func Test_runValidateCanary(t *testing.T) {
	t.Parallel()

	t.Run("非推奨の引数で落ち、抑止の宣言ときれいな構成で通れば成功する", func(t *testing.T) {
		t.Parallel()
		var out bytes.Buffer
		require.NoError(t, runValidateCanary(canaryArgs(t), &out, canaryTool(t, "")))
		assert.Contains(t, out.String(), "✅ tf-validate は AWS Provider の非推奨の引数で落ち")
	})

	t.Run("非推奨の引数で警告が出なければ失敗する", func(t *testing.T) {
		t.Parallel()
		var out bytes.Buffer
		err := runValidateCanary(canaryArgs(t), &out, canaryTool(t, "silent"))
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
		require.ErrorIs(t, runValidateCanary(canaryArgs(t), &bytes.Buffer{}, broken), errCanary)
	})

	t.Run("reference の lock が無ければ失敗する", func(t *testing.T) {
		t.Parallel()
		ref := tree(t, map[string]string{"versions.tf": ""})
		args := []string{"-data", t.TempDir(), "-work", t.TempDir(), "-reference", ref}
		err := runValidateCanary(args, &bytes.Buffer{}, canaryTool(t, ""))
		require.Error(t, err)
		require.NotErrorIs(t, err, errCanary)
	})

	t.Run("引数の誤りは errUsage", func(t *testing.T) {
		t.Parallel()
		for name, args := range map[string][]string{
			"-data が相対": {"-data", "d", "-work", "w", "-reference", "r"},
			"-work が無い": {"-data", "/d", "-reference", "r"},
			"余計な引数":     {"-data", "/d", "-work", "w", "-reference", "r", "x"},
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				require.ErrorIs(t, runValidateCanary(args, &bytes.Buffer{}, canaryTool(t, "")), errUsage)
			})
		}
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

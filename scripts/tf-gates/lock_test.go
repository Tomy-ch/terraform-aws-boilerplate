package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/tflock"
)

const (
	testDecl = "[aws]\nsource = \"hashicorp/aws\"\nversion = \"6.73.0\"\n"
	testMise = "[settings]\nlockfile_platforms = [\"linux-x64\", \"macos-arm64\"]\n"
	testLock = "provider \"registry.terraform.io/hashicorp/aws\" {\n  version     = \"6.73.0\"\n  constraints = \"6.73.0\"\n  hashes = [\n    \"h1:a=\",\n    \"h1:b=\",\n    \"zh:01\",\n  ]\n}\n"
)

// lockRepo は、正本（providers/）と mise.toml と、root と root でないディレクトリを持つ走査の基点を建てる。
func lockRepo(t *testing.T, lock string, extra map[string]string) (base string, args []string) {
	t.Helper()
	files := map[string]string{
		"providers/providers.toml":                 testDecl,
		"mise.toml":                                testMise,
		"env/admin/main.tf":                        "",
		"usecases/u/reference/main.tf":             "",
		"usecases/u/main.tf":                       "",
		"modules/m/main.tf":                        "",
		"env/admin/.terraform.lock.hcl":            testLock,
		"usecases/u/reference/.terraform.lock.hcl": testLock,
	}
	if lock != "" {
		files["providers/.terraform.lock.hcl"] = lock
	}
	for k, v := range extra {
		files[k] = v
	}
	base = tree(t, files)

	return base, []string{"-base", base, "-providers", filepath.Join(base, "providers"), "-mise", filepath.Join(base, "mise.toml"), "usecases", "modules", "env"}
}

// tracked は、追跡しているファイルとして paths を返す lsFilesFunc。
func tracked(paths ...string) lsFilesFunc {
	return func(string) ([]string, error) { return paths, nil }
}

var rootLocks = tracked("env/admin/.terraform.lock.hcl", "usecases/u/reference/.terraform.lock.hcl", "providers/.terraform.lock.hcl")

func Test_loadCanonical(t *testing.T) {
	t.Parallel()

	t.Run("宣言と合う lock を返す", func(t *testing.T) {
		t.Parallel()
		base, _ := lockRepo(t, testLock, nil)
		c, err := loadCanonical(filepath.Join(base, "providers"), filepath.Join(base, "mise.toml"))
		require.NoError(t, err)
		assert.Equal(t, testLock, string(c.content))
		assert.Len(t, c.decls, 1)
	})

	t.Run("lock が無ければ make tf-providers-lock を案内する", func(t *testing.T) {
		t.Parallel()
		base, _ := lockRepo(t, "", nil)
		_, err := loadCanonical(filepath.Join(base, "providers"), filepath.Join(base, "mise.toml"))
		require.ErrorIs(t, err, os.ErrNotExist)
		assert.Contains(t, err.Error(), "make tf-providers-lock")
	})

	t.Run("宣言が無ければエラー", func(t *testing.T) {
		t.Parallel()
		base, _ := lockRepo(t, testLock, nil)
		require.NoError(t, os.Remove(filepath.Join(base, "providers", tflock.DeclFile)))
		_, err := loadCanonical(filepath.Join(base, "providers"), filepath.Join(base, "mise.toml"))
		require.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("版が宣言と違えば errLock", func(t *testing.T) {
		t.Parallel()
		base, _ := lockRepo(t, strings.Replace(testLock, "\"6.73.0\"\n  constraints", "\"6.70.0\"\n  constraints", 1), nil)
		_, err := loadCanonical(filepath.Join(base, "providers"), filepath.Join(base, "mise.toml"))
		require.ErrorIs(t, err, errLock)
	})

	t.Run("mise.toml に lockfile_platforms が無ければエラー", func(t *testing.T) {
		t.Parallel()
		base, _ := lockRepo(t, testLock, map[string]string{"mise.toml": "[tools]\n"})
		_, err := loadCanonical(filepath.Join(base, "providers"), filepath.Join(base, "mise.toml"))
		require.ErrorIs(t, err, tflock.ErrInvalidPlatform)
	})
}

func Test_platformsOf(t *testing.T) {
	t.Parallel()

	t.Run("lockfile_platforms を terraform の platform へ写す", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(tree(t, map[string]string{"mise.toml": testMise}), "mise.toml")
		got, err := platformsOf(path)
		require.NoError(t, err)
		assert.Equal(t, []string{"linux_amd64", "darwin_arm64"}, got)
	})

	t.Run("lockfile_platforms が無ければ ErrInvalidPlatform", func(t *testing.T) {
		t.Parallel()
		path := filepath.Join(tree(t, map[string]string{"mise.toml": "[settings]\n"}), "mise.toml")
		_, err := platformsOf(path)
		require.ErrorIs(t, err, tflock.ErrInvalidPlatform)
	})

	t.Run("mise.toml が無ければエラー", func(t *testing.T) {
		t.Parallel()
		_, err := platformsOf(filepath.Join(t.TempDir(), "mise.toml"))
		require.Error(t, err)
	})
}

func Test_checkLock(t *testing.T) {
	t.Parallel()

	decls := []tflock.Decl{{Name: "aws", Source: "hashicorp/aws", Version: "6.73.0"}}

	t.Run("宣言と platform の数に合えば nil", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, checkLock(decls, []byte(testLock), "x", 2))
	})

	t.Run("h1 の数が platform と違えば errLock で食い違いを名指す", func(t *testing.T) {
		t.Parallel()
		err := checkLock(decls, []byte(testLock), "x", 3)
		require.ErrorIs(t, err, errLock)
		assert.Contains(t, err.Error(), "h1: が 2 種")
	})

	t.Run("読めない lock は ErrInvalidLock", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, checkLock(decls, []byte("x"), "x", 2), tflock.ErrInvalidLock)
	})
}

// providersLockTool は、作業ディレクトリに lock を書く偽の terraform。引数と、作業ディレクトリの versions.tf を
// calls に残す。rc が 0 でなければ lock を書かずにその終了コードで終わる。
func providersLockTool(t *testing.T, lock string, rc int) (toolFunc, *[]string) {
	t.Helper()
	src := filepath.Join(t.TempDir(), "lock")
	require.NoError(t, os.WriteFile(src, []byte(lock), 0o600))
	var calls []string
	tool := func(ctx context.Context, d string, env []string, step string, extra ...string) *exec.Cmd {
		versions, err := os.ReadFile(filepath.Join(d, "versions.tf"))
		require.NoError(t, err)
		calls = append(calls, step+" "+strings.Join(extra, " "), string(versions))
		script := "cp '" + src + "' .terraform.lock.hcl"
		if lock == "" {
			script = "true"
		}
		if rc != 0 {
			script = "exit " + strconv.Itoa(rc)
		}
		c := exec.CommandContext(ctx, "/bin/sh", "-c", script)
		c.Dir = d
		c.Env = env

		return c
	}

	return tool, &calls
}

func Test_runLockGenerate(t *testing.T) {
	t.Parallel()

	gen := func(t *testing.T, tool toolFunc, root string) error {
		t.Helper()

		return runLockGenerate([]string{"-providers", filepath.Join(root, "providers"), "-work", "tmp/c/work", "-mise", filepath.Join(root, "mise.toml")}, &bytes.Buffer{}, tool, root)
	}
	repo := func(t *testing.T) string {
		t.Helper()
		_, root, _ := workWorld(t)
		for rel, body := range map[string]string{"providers/providers.toml": testDecl, "mise.toml": testMise} {
			require.NoError(t, os.MkdirAll(filepath.Join(root, filepath.Dir(rel)), 0o750))
			require.NoError(t, os.WriteFile(filepath.Join(root, rel), []byte(body), 0o600))
		}

		return root
	}

	t.Run("宣言の versions.tf で providers lock を platform つきで起動し、正本へ置く", func(t *testing.T) {
		t.Parallel()
		root := repo(t)
		tool, calls := providersLockTool(t, testLock, 0)
		require.NoError(t, gen(t, tool, root))
		assert.Equal(t, []string{"providers-lock linux_amd64 darwin_arm64", tflock.VersionsTF([]tflock.Decl{{Name: "aws", Source: "hashicorp/aws", Version: "6.73.0"}})}, *calls)
		got, err := os.ReadFile(filepath.Join(root, "providers", tflock.LockFile))
		require.NoError(t, err)
		assert.Equal(t, testLock, string(got))
	})

	t.Run("作業ディレクトリの古い lock を使わない", func(t *testing.T) {
		t.Parallel()
		root := repo(t)
		require.NoError(t, os.WriteFile(filepath.Join(root, "tmp", "c", "work", tflock.LockFile), []byte(testLock), 0o600))
		tool, _ := providersLockTool(t, "", 0)
		require.ErrorIs(t, gen(t, tool, root), os.ErrNotExist)
		assert.NoFileExists(t, filepath.Join(root, "providers", tflock.LockFile))
	})

	t.Run("terraform が落ちれば正本を書かない", func(t *testing.T) {
		t.Parallel()
		root := repo(t)
		tool, _ := providersLockTool(t, testLock, 1)
		require.ErrorIs(t, gen(t, tool, root), errToolFailed)
		assert.NoFileExists(t, filepath.Join(root, "providers", tflock.LockFile))
	})

	t.Run("書かれた lock が宣言と合わなければ正本を書かない", func(t *testing.T) {
		t.Parallel()
		root := repo(t)
		tool, _ := providersLockTool(t, strings.Replace(testLock, "    \"h1:b=\",\n", "", 1), 0)
		require.ErrorIs(t, gen(t, tool, root), errLock)
		assert.NoFileExists(t, filepath.Join(root, "providers", tflock.LockFile))
	})

	t.Run("宣言を読めなければ起動しない", func(t *testing.T) {
		t.Parallel()
		root := repo(t)
		require.NoError(t, os.WriteFile(filepath.Join(root, "providers", tflock.DeclFile), []byte("x\n"), 0o600))
		tool, calls := providersLockTool(t, testLock, 0)
		require.ErrorIs(t, gen(t, tool, root), tflock.ErrInvalidDecl)
		assert.Empty(t, *calls)
	})

	t.Run("-work が tmp/ の外なら errUsage", func(t *testing.T) {
		t.Parallel()
		root := repo(t)
		tool, calls := providersLockTool(t, testLock, 0)
		err := runLockGenerate([]string{"-providers", filepath.Join(root, "providers"), "-work", "tmp/out/w", "-mise", filepath.Join(root, "mise.toml")}, &bytes.Buffer{}, tool, root)
		require.ErrorIs(t, err, errUsage)
		assert.Empty(t, *calls)
	})

	t.Run("flag が欠ければ errUsage", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, runLockGenerate([]string{"-providers", "p"}, &bytes.Buffer{}, command, t.TempDir()), errUsage)
	})
}

func Test_runLockSync(t *testing.T) {
	t.Parallel()

	t.Run("root の lock が正本と同じなら、check も apply も何も書かない", func(t *testing.T) {
		t.Parallel()
		base, args := lockRepo(t, testLock, nil)
		before := snapshot(t, base)
		require.NoError(t, runLockSync("lock-check", false, args, &bytes.Buffer{}, rootLocks))
		require.NoError(t, runLockSync("lock-apply", true, args, &bytes.Buffer{}, rootLocks))
		assert.Equal(t, before, snapshot(t, base))
	})

	t.Run("違う root と無い root を、check は名指して落とし、apply は正本で書く", func(t *testing.T) {
		t.Parallel()
		base, args := lockRepo(t, testLock, map[string]string{"env/admin/.terraform.lock.hcl": "old\n", "usecases/v/reference/main.tf": ""})
		var out bytes.Buffer
		err := runLockSync("lock-check", false, args, &out, rootLocks)
		require.ErrorIs(t, err, errLock)
		assert.Contains(t, err.Error(), "env/admin usecases/v/reference")
		assert.Contains(t, err.Error(), "make tf-lock-apply")

		require.NoError(t, runLockSync("lock-apply", true, args, &out, rootLocks))
		for _, rel := range []string{"env/admin", "usecases/v/reference", "usecases/u/reference"} {
			got, err := os.ReadFile(filepath.Join(base, rel, tflock.LockFile))
			require.NoError(t, err)
			assert.Equal(t, testLock, string(got), rel)
		}
		assert.NoFileExists(t, filepath.Join(base, "modules", "m", tflock.LockFile), "root でないディレクトリへは写さない")
		require.NoError(t, runLockSync("lock-check", false, args, &out, rootLocks))
	})

	t.Run("root でないディレクトリの lock が追跡されていれば、どちらも書かずに落とす", func(t *testing.T) {
		t.Parallel()
		base, args := lockRepo(t, testLock, map[string]string{"env/admin/.terraform.lock.hcl": "old\n"})
		before := snapshot(t, base)
		lsFiles := tracked("env/admin/.terraform.lock.hcl", "modules/m/.terraform.lock.hcl", "usecases/u/.terraform.lock.hcl")
		for _, write := range []bool{false, true} {
			err := runLockSync("lock-sync", write, args, &bytes.Buffer{}, lsFiles)
			require.ErrorIs(t, err, errLock)
			assert.Contains(t, err.Error(), "modules/m usecases/u")
		}
		assert.Equal(t, before, snapshot(t, base))
	})

	t.Run("正本が宣言と合わなければ、どちらも書かずに落とす", func(t *testing.T) {
		t.Parallel()
		base, args := lockRepo(t, strings.Replace(testLock, "    \"zh:01\",\n", "", 1), nil)
		before := snapshot(t, base)
		require.ErrorIs(t, runLockSync("lock-apply", true, args, &bytes.Buffer{}, rootLocks), errLock)
		assert.Equal(t, before, snapshot(t, base))
	})

	t.Run("root が0件なら errNoTargets", func(t *testing.T) {
		t.Parallel()
		base, _ := lockRepo(t, testLock, nil)
		args := []string{"-base", base, "-providers", filepath.Join(base, "providers"), "-mise", filepath.Join(base, "mise.toml"), "modules"}
		require.ErrorIs(t, runLockSync("lock-check", false, args, &bytes.Buffer{}, rootLocks), errNoTargets)
	})

	t.Run("追跡の一覧を取れなければ落とす", func(t *testing.T) {
		t.Parallel()
		_, args := lockRepo(t, testLock, nil)
		fail := func(string) ([]string, error) { return nil, errTool }
		require.ErrorIs(t, runLockSync("lock-check", false, args, &bytes.Buffer{}, fail), errTool)
	})

	t.Run("-providers か -mise が無ければ errUsage", func(t *testing.T) {
		t.Parallel()
		base, _ := lockRepo(t, testLock, nil)
		require.ErrorIs(t, runLockSync("lock-check", false, []string{"-base", base, "-mise", "m", "env"}, &bytes.Buffer{}, rootLocks), errUsage)
		require.ErrorIs(t, runLockSync("lock-check", false, []string{"-base", base, "-providers", "p", "env"}, &bytes.Buffer{}, rootLocks), errUsage)
	})
}

func Test_runLockApply(t *testing.T) {
	t.Parallel()

	base, args := lockRepo(t, testLock, map[string]string{"env/admin/.terraform.lock.hcl": "old\n"})
	require.NoError(t, runLockApply(args, &bytes.Buffer{}, rootLocks))
	got, err := os.ReadFile(filepath.Join(base, "env", "admin", tflock.LockFile))
	require.NoError(t, err)
	assert.Equal(t, testLock, string(got))
}

func Test_runLockCheck(t *testing.T) {
	t.Parallel()

	base, args := lockRepo(t, testLock, map[string]string{"env/admin/.terraform.lock.hcl": "old\n"})
	require.ErrorIs(t, runLockCheck(args, &bytes.Buffer{}, rootLocks), errLock)
	got, err := os.ReadFile(filepath.Join(base, "env", "admin", tflock.LockFile))
	require.NoError(t, err)
	assert.Equal(t, "old\n", string(got), "check は書かない")
}

func Test_diffRoots(t *testing.T) {
	t.Parallel()

	base := tree(t, map[string]string{"a/.terraform.lock.hcl": "same", "b/.terraform.lock.hcl": "old", "c/main.tf": ""})
	roots := []target{{rel: "a", path: filepath.Join(base, "a")}, {rel: "b", path: filepath.Join(base, "b")}, {rel: "c", path: filepath.Join(base, "c")}}
	changes, drifted, err := diffRoots(roots, []byte("same"))
	require.NoError(t, err)
	assert.Equal(t, []string{"b", "c"}, drifted)
	assert.Equal(t, map[string]string{filepath.Join(base, "b", tflock.LockFile): "same", filepath.Join(base, "c", tflock.LockFile): "same"}, changes)
}

func Test_checkTracked(t *testing.T) {
	t.Parallel()

	s := scan{base: "/b", roots: []string{"usecases", "env"}}
	roots := []target{{rel: "env/admin"}, {rel: "usecases/u/reference"}}

	t.Run("root と走査の外の lock だけなら nil", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, checkTracked(s, roots, tracked("env/admin/.terraform.lock.hcl", "providers/.terraform.lock.hcl", "modules/m/.terraform.lock.hcl", "usecases/u/main.tf")))
	})

	t.Run("走査の root の下の root でない lock を名指す", func(t *testing.T) {
		t.Parallel()
		err := checkTracked(s, roots, tracked("usecases/u/.terraform.lock.hcl", "env/admin/sub/.terraform.lock.hcl"))
		require.ErrorIs(t, err, errLock)
		assert.Contains(t, err.Error(), "env/admin/sub usecases/u")
	})
}

func Test_underRoots(t *testing.T) {
	t.Parallel()

	assert.True(t, underRoots("env", []string{"env"}))
	assert.True(t, underRoots("env/admin", []string{"usecases", "env"}))
	assert.False(t, underRoots("envx/admin", []string{"env"}), "接頭辞が同じだけの別の名前")
	assert.False(t, underRoots("providers", []string{"env"}))
}

func Test_gitLsFiles(t *testing.T) {
	t.Parallel()

	t.Run("追跡しているファイルを返す", func(t *testing.T) {
		t.Parallel()
		base := tree(t, map[string]string{"a/.terraform.lock.hcl": "x", "b/untracked": "y"})
		for _, args := range [][]string{{"init", "-q"}, {"add", "a/.terraform.lock.hcl"}} {
			cmd := exec.CommandContext(context.Background(), "git", args...)
			cmd.Dir = base
			out, err := cmd.CombinedOutput()
			require.NoError(t, err, string(out))
		}
		got, err := gitLsFiles(base)
		require.NoError(t, err)
		assert.Equal(t, []string{"a/.terraform.lock.hcl"}, got)
	})

	t.Run("git のリポジトリでなければ errTool", func(t *testing.T) {
		t.Parallel()
		_, err := gitLsFiles(t.TempDir())
		require.ErrorIs(t, err, errTool)
	})
}

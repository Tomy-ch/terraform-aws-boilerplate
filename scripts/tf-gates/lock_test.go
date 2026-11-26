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

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/misetoml"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/testenv"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/tflock"
)

const (
	testDecl = "[aws]\nsource = \"hashicorp/aws\"\nversion = \"6.73.0\"\n"
	testMise = "[settings]\nlockfile_platforms = [\"linux-x64\", \"macos-arm64\"]\n"
)

var (
	testH1a  = "h1:" + strings.Repeat("a", 43) + "="
	testH1b  = "h1:" + strings.Repeat("b", 43) + "="
	testZh   = "zh:" + strings.Repeat("0", 64)
	testLock = "provider \"registry.terraform.io/hashicorp/aws\" {\n  version     = \"6.73.0\"\n  constraints = \"6.73.0\"\n  hashes = [\n    \"" + testH1a + "\",\n    \"" + testH1b + "\",\n    \"" + testZh + "\",\n  ]\n}\n"
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

func readFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err)

	return string(raw)
}

func Test_loadCanonical(t *testing.T) {
	t.Parallel()

	load := func(base string) (canonical, error) {
		return loadCanonical(filepath.Join(base, "providers"), filepath.Join(base, "mise.toml"))
	}

	t.Run("宣言と合う lock を返す", func(t *testing.T) {
		t.Parallel()
		base, _ := lockRepo(t, testLock, nil)
		c, err := load(base)
		require.NoError(t, err)
		assert.Equal(t, testLock, string(c.content))
		assert.Len(t, c.decls, 1)
	})

	t.Run("lock が無ければ make tf-providers-lock を案内する", func(t *testing.T) {
		t.Parallel()
		base, _ := lockRepo(t, "", nil)
		_, err := load(base)
		require.ErrorIs(t, err, os.ErrNotExist)
		assert.Contains(t, err.Error(), "make tf-providers-lock")
	})

	t.Run("lock がディレクトリなら読み取りのエラー", func(t *testing.T) {
		t.Parallel()
		base, _ := lockRepo(t, "", map[string]string{"providers/.terraform.lock.hcl/x": ""})
		_, err := load(base)
		require.Error(t, err)
		require.NotErrorIs(t, err, os.ErrNotExist)
	})

	for name, lock := range map[string]string{
		"空":            "\n",
		"HCL として壊れている": "provider \"x\" {\n",
		"provider が0件": "# x\n",
	} {
		t.Run("lock が"+name+"なら ErrInvalidLock", func(t *testing.T) {
			t.Parallel()
			base, _ := lockRepo(t, lock, nil)
			_, err := load(base)
			require.ErrorIs(t, err, tflock.ErrInvalidLock)
		})
	}

	t.Run("宣言が無ければエラー", func(t *testing.T) {
		t.Parallel()
		base, _ := lockRepo(t, testLock, nil)
		require.NoError(t, os.Remove(filepath.Join(base, "providers", tflock.DeclFile)))
		_, err := load(base)
		require.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("宣言が壊れていれば ErrInvalidDecl", func(t *testing.T) {
		t.Parallel()
		base, _ := lockRepo(t, testLock, map[string]string{"providers/providers.toml": "x\n"})
		_, err := load(base)
		require.ErrorIs(t, err, tflock.ErrInvalidDecl)
	})

	t.Run("版が宣言と違えば errLock", func(t *testing.T) {
		t.Parallel()
		base, _ := lockRepo(t, strings.Replace(testLock, "\"6.73.0\"\n  constraints", "\"6.70.0\"\n  constraints", 1), nil)
		_, err := load(base)
		require.ErrorIs(t, err, errLock)
	})

	t.Run("mise.toml に lockfile_platforms が無ければ ErrInvalidPlatform", func(t *testing.T) {
		t.Parallel()
		base, _ := lockRepo(t, testLock, map[string]string{"mise.toml": "[tools]\n"})
		_, err := load(base)
		require.ErrorIs(t, err, tflock.ErrInvalidPlatform)
	})
}

func Test_platformsOf(t *testing.T) {
	t.Parallel()

	of := func(t *testing.T, mise string) ([]string, error) {
		t.Helper()

		return platformsOf(filepath.Join(tree(t, map[string]string{"mise.toml": mise}), "mise.toml"))
	}

	t.Run("lockfile_platforms を terraform の platform へ写す", func(t *testing.T) {
		t.Parallel()
		got, err := of(t, testMise)
		require.NoError(t, err)
		assert.Equal(t, []string{"linux_amd64", "darwin_arm64"}, got)
	})

	for name, mise := range map[string]string{
		"lockfile_platforms が無い":  "[settings]\n",
		"lockfile_platforms が空配列": "[settings]\nlockfile_platforms = []\n",
		"写せない platform 名":         "[settings]\nlockfile_platforms = [\"windows-x64\"]\n",
	} {
		t.Run(name+"なら ErrInvalidPlatform", func(t *testing.T) {
			t.Parallel()
			_, err := of(t, mise)
			require.ErrorIs(t, err, tflock.ErrInvalidPlatform)
		})
	}

	t.Run("解釈できない mise.toml は misetoml のエラーを返す", func(t *testing.T) {
		t.Parallel()
		_, err := of(t, "[settings]\nlockfile_platforms = [\"linux-x64\"]\nlockfile_platforms = [\"linux-x64\"]\n")
		require.ErrorIs(t, err, misetoml.ErrInvalidLockfilePlatforms)
	})

	t.Run("mise.toml が無ければ ErrNotExist", func(t *testing.T) {
		t.Parallel()
		_, err := platformsOf(filepath.Join(t.TempDir(), "mise.toml"))
		require.ErrorIs(t, err, os.ErrNotExist)
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

// providersLockTool は、作業ディレクトリに lock を書く偽の terraform。起動ごとに「段 引数」と、作業ディレクトリの
// versions.tf と、AWS_ で始まる環境変数の有無を calls に残す。rc が 0 でなければ lock を書かずにその終了コードで
// 終わり、lock が空でなければその中身を書く。
func providersLockTool(t *testing.T, lock string, rc int) (toolFunc, *[]string) {
	t.Helper()
	src := filepath.Join(t.TempDir(), "lock")
	require.NoError(t, os.WriteFile(src, []byte(lock), 0o600))
	var calls []string
	tool := func(ctx context.Context, d string, env []string, step string, extra ...string) *exec.Cmd {
		versions, err := os.ReadFile(filepath.Join(d, "versions.tf"))
		require.NoError(t, err)
		aws := "AWS なし"
		for _, e := range env {
			if strings.HasPrefix(e, "AWS_") && !strings.HasPrefix(e, "AWS_SHARED_CREDENTIALS_FILE=") && !strings.HasPrefix(e, "AWS_CONFIG_FILE=") && !strings.HasPrefix(e, "AWS_EC2_METADATA_DISABLED=") {
				aws = "AWS あり: " + e
			}
		}
		calls = append(calls, step+" "+strings.Join(extra, " "), string(versions), aws)
		script := "cp '" + src + "' .terraform.lock.hcl"
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

// noopTool は、何も書かずに成功する偽の terraform。
func noopTool(ctx context.Context, d string, env []string, _ string, _ ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", "true")
	cmd.Dir, cmd.Env = d, env

	return cmd
}

func Test_runLockGenerate(t *testing.T) {
	t.Parallel()

	const oldLock = "# 古い正本\n"
	genArgs := func(root string) []string {
		return []string{"-providers", filepath.Join(root, "providers"), "-work", "tmp/c/work", "-mise", filepath.Join(root, "mise.toml")}
	}
	// repo は、古い正本を持つリポジトリ直下を建てる。
	repo := func(t *testing.T) string {
		t.Helper()
		_, root, _ := workWorld(t)
		for rel, body := range map[string]string{"providers/providers.toml": testDecl, "providers/.terraform.lock.hcl": oldLock, "mise.toml": testMise} {
			require.NoError(t, os.MkdirAll(filepath.Join(root, filepath.Dir(rel)), 0o750))
			require.NoError(t, os.WriteFile(filepath.Join(root, rel), []byte(body), 0o600))
		}

		return root
	}
	canonicalOf := func(t *testing.T, root string) string {
		t.Helper()

		return readFile(t, filepath.Join(root, "providers", tflock.LockFile))
	}

	t.Run("宣言の versions.tf で providers lock を platform つきで起動し、正本へ置く", func(t *testing.T) {
		t.Parallel()
		root := repo(t)
		tool, calls := providersLockTool(t, testLock, 0)
		var out bytes.Buffer
		require.NoError(t, runLockGenerate(genArgs(root), &out, tool, root))
		assert.Equal(t, []string{"providers-lock linux_amd64 darwin_arm64", tflock.VersionsTF([]tflock.Decl{{Name: "aws", Source: "hashicorp/aws", Version: "6.73.0"}}), "AWS なし"}, *calls)
		assert.Equal(t, testLock, canonicalOf(t, root))
		assert.Contains(t, out.String(), "provider 1 件・platform linux_amd64 darwin_arm64")
	})

	for name, c := range map[string]struct {
		lock string
		rc   int
		want error
	}{
		"terraform が落ちる":        {testLock, 1, errToolFailed},
		"lock を書かない":            {"", 0, os.ErrNotExist},
		"空の lock を書く":           {"\n", 0, tflock.ErrInvalidLock},
		"provider 0件の lock を書く": {"# x\n", 0, tflock.ErrInvalidLock},
		"宣言と合わない lock を書く":      {strings.Replace(testLock, "    \""+testH1b+"\",\n", "", 1), 0, errLock},
	} {
		t.Run(name+"なら、既存の正本を1バイトも変えない", func(t *testing.T) {
			t.Parallel()
			root := repo(t)
			tool, _ := providersLockTool(t, c.lock, c.rc)
			if c.lock == "" {
				tool = noopTool
			}
			require.ErrorIs(t, runLockGenerate(genArgs(root), &bytes.Buffer{}, tool, root), c.want)
			assert.Equal(t, oldLock, canonicalOf(t, root))
		})
	}

	t.Run("作業ディレクトリに残った古い lock を使わない", func(t *testing.T) {
		t.Parallel()
		root := repo(t)
		require.NoError(t, os.WriteFile(filepath.Join(root, "tmp", "c", "work", tflock.LockFile), []byte(testLock), 0o600))
		require.ErrorIs(t, runLockGenerate(genArgs(root), &bytes.Buffer{}, noopTool, root), os.ErrNotExist)
		assert.Equal(t, oldLock, canonicalOf(t, root))
	})

	t.Run("terraform を起動できなければ errTool で、正本を書かない", func(t *testing.T) {
		t.Parallel()
		root := repo(t)
		tool := func(ctx context.Context, d string, env []string, _ string, _ ...string) *exec.Cmd {
			cmd := exec.CommandContext(ctx, filepath.Join(t.TempDir(), "no-such-terraform"))
			cmd.Dir, cmd.Env = d, env

			return cmd
		}
		require.ErrorIs(t, runLockGenerate(genArgs(root), &bytes.Buffer{}, tool, root), errTool)
		assert.Equal(t, oldLock, canonicalOf(t, root))
	})

	t.Run("正本のディレクトリに書けなければ、既存の正本を変えない", func(t *testing.T) {
		t.Parallel()
		testenv.RequireNonRoot(t, "root はディレクトリの書き込みの権限を無視する")
		root := repo(t)
		dir := filepath.Join(root, "providers")
		require.NoError(t, os.Chmod(dir, 0o500))
		t.Cleanup(func() { _ = os.Chmod(dir, 0o750) })
		tool, _ := providersLockTool(t, testLock, 0)
		require.Error(t, runLockGenerate(genArgs(root), &bytes.Buffer{}, tool, root))
		assert.Equal(t, oldLock, canonicalOf(t, root))
	})

	t.Run("宣言を読めなければ起動しない", func(t *testing.T) {
		t.Parallel()
		root := repo(t)
		require.NoError(t, os.WriteFile(filepath.Join(root, "providers", tflock.DeclFile), []byte("x\n"), 0o600))
		tool, calls := providersLockTool(t, testLock, 0)
		require.ErrorIs(t, runLockGenerate(genArgs(root), &bytes.Buffer{}, tool, root), tflock.ErrInvalidDecl)
		assert.Empty(t, *calls)
	})

	t.Run("正本のディレクトリが無ければ ErrNotExist で起動しない", func(t *testing.T) {
		t.Parallel()
		root := repo(t)
		tool, calls := providersLockTool(t, testLock, 0)
		args := []string{"-providers", filepath.Join(root, "no-such"), "-work", "tmp/c/work", "-mise", filepath.Join(root, "mise.toml")}
		require.ErrorIs(t, runLockGenerate(args, &bytes.Buffer{}, tool, root), os.ErrNotExist)
		assert.Empty(t, *calls)
	})

	t.Run("-work が tmp/ の外なら errUsage で起動しない", func(t *testing.T) {
		t.Parallel()
		root := repo(t)
		tool, calls := providersLockTool(t, testLock, 0)
		args := []string{"-providers", filepath.Join(root, "providers"), "-work", "tmp/out/w", "-mise", filepath.Join(root, "mise.toml")}
		require.ErrorIs(t, runLockGenerate(args, &bytes.Buffer{}, tool, root), errUsage)
		assert.Empty(t, *calls)
	})

	for name, args := range map[string][]string{
		"-work が無い":      {"-providers", "p", "-mise", "m"},
		"-mise が無い":      {"-providers", "p", "-work", "w"},
		"-providers が無い": {"-work", "w", "-mise", "m"},
		"位置引数がある":        {"-providers", "p", "-work", "w", "-mise", "m", "extra"},
		"知らない flag":      {"-no-such-flag"},
	} {
		t.Run(name+"なら errUsage", func(t *testing.T) {
			t.Parallel()
			require.ErrorIs(t, runLockGenerate(args, &bytes.Buffer{}, command, t.TempDir()), errUsage)
		})
	}
}

func Test_runLockSync(t *testing.T) {
	t.Parallel()

	withNewRoot := tracked("env/admin/.terraform.lock.hcl", "usecases/u/reference/.terraform.lock.hcl", "usecases/v/reference/.terraform.lock.hcl")

	t.Run("root の lock が正本と同じなら、check も apply も何も書かない", func(t *testing.T) {
		t.Parallel()
		base, args := lockRepo(t, testLock, nil)
		before := snapshot(t, base)
		var out bytes.Buffer
		require.NoError(t, runLockSync("lock-check", false, args, &out, rootLocks))
		require.NoError(t, runLockSync("lock-apply", true, args, &out, rootLocks))
		assert.Equal(t, before, snapshot(t, base))
		assert.Contains(t, out.String(), "root 2 件の lock が正本と同じです")
		assert.Contains(t, out.String(), "root 2 件のうち 0 件")
	})

	t.Run("check は、違う root と lock の無い root を名指して落とし、何も書かない", func(t *testing.T) {
		t.Parallel()
		base, args := lockRepo(t, testLock, map[string]string{"env/admin/.terraform.lock.hcl": "old\n", "usecases/v/reference/main.tf": ""})
		before := snapshot(t, base)
		err := runLockSync("lock-check", false, args, &bytes.Buffer{}, withNewRoot)
		require.ErrorIs(t, err, errLock)
		assert.Contains(t, err.Error(), "env/admin usecases/v/reference")
		assert.Contains(t, err.Error(), "make tf-lock-apply")
		assert.Equal(t, before, snapshot(t, base))
	})

	t.Run("apply は、違う root と lock の無い root を正本で書き、root でないディレクトリへは写さない", func(t *testing.T) {
		t.Parallel()
		base, args := lockRepo(t, testLock, map[string]string{"env/admin/.terraform.lock.hcl": "old\n", "usecases/v/reference/main.tf": ""})
		var out bytes.Buffer
		require.NoError(t, runLockSync("lock-apply", true, args, &out, rootLocks))
		for _, rel := range []string{"env/admin", "usecases/v/reference", "usecases/u/reference"} {
			assert.Equal(t, testLock, readFile(t, filepath.Join(base, rel, tflock.LockFile)), rel)
		}
		assert.NoFileExists(t, filepath.Join(base, "modules", "m", tflock.LockFile))
		assert.NoFileExists(t, filepath.Join(base, "usecases", "u", tflock.LockFile))
		assert.Contains(t, out.String(), "root 3 件のうち 2 件")
	})

	t.Run("apply の後の check は通る", func(t *testing.T) {
		t.Parallel()
		_, args := lockRepo(t, testLock, map[string]string{"env/admin/.terraform.lock.hcl": "old\n"})
		require.NoError(t, runLockSync("lock-apply", true, args, &bytes.Buffer{}, rootLocks))
		require.NoError(t, runLockSync("lock-check", false, args, &bytes.Buffer{}, rootLocks))
	})

	t.Run("check は、lock が追跡されていない root を名指して落とす。apply はそれを求めない", func(t *testing.T) {
		t.Parallel()
		_, args := lockRepo(t, testLock, nil)
		lsFiles := tracked("env/admin/.terraform.lock.hcl")
		err := runLockSync("lock-check", false, args, &bytes.Buffer{}, lsFiles)
		require.ErrorIs(t, err, errLock)
		assert.Contains(t, err.Error(), "lock が追跡されていない root: usecases/u/reference")
		require.NoError(t, runLockSync("lock-apply", true, args, &bytes.Buffer{}, lsFiles))
	})

	t.Run("root でないディレクトリの lock が追跡されていれば、どちらも書かずに落とす", func(t *testing.T) {
		t.Parallel()
		base, args := lockRepo(t, testLock, map[string]string{"env/admin/.terraform.lock.hcl": "old\n"})
		before := snapshot(t, base)
		lsFiles := tracked("env/admin/.terraform.lock.hcl", "usecases/u/reference/.terraform.lock.hcl", "modules/m/.terraform.lock.hcl", "usecases/u/.terraform.lock.hcl")
		for _, write := range []bool{false, true} {
			err := runLockSync("lock-sync", write, args, &bytes.Buffer{}, lsFiles)
			require.ErrorIs(t, err, errLock)
			assert.Contains(t, err.Error(), "modules/m usecases/u")
		}
		assert.Equal(t, before, snapshot(t, base))
	})

	t.Run("1つの root に書けなければ、ほかの root の lock も書き換えない", func(t *testing.T) {
		t.Parallel()
		testenv.RequireNonRoot(t, "root はディレクトリの書き込みの権限を無視する")
		base, args := lockRepo(t, testLock, map[string]string{"env/admin/.terraform.lock.hcl": "old\n", "usecases/u/reference/.terraform.lock.hcl": "old\n"})
		dir := filepath.Join(base, "usecases", "u", "reference")
		require.NoError(t, os.Chmod(dir, 0o500))
		t.Cleanup(func() { _ = os.Chmod(dir, 0o750) })
		require.Error(t, runLockSync("lock-apply", true, args, &bytes.Buffer{}, rootLocks))
		assert.Equal(t, "old\n", readFile(t, filepath.Join(base, "env", "admin", tflock.LockFile)))
	})

	t.Run("正本が宣言と合わなければ、どちらも書かずに落とす", func(t *testing.T) {
		t.Parallel()
		base, args := lockRepo(t, strings.Replace(testLock, "    \""+testZh+"\",\n", "", 1), nil)
		before := snapshot(t, base)
		require.ErrorIs(t, runLockSync("lock-apply", true, args, &bytes.Buffer{}, rootLocks), errLock)
		require.ErrorIs(t, runLockSync("lock-check", false, args, &bytes.Buffer{}, rootLocks), errLock)
		assert.Equal(t, before, snapshot(t, base))
	})

	t.Run("root が0件なら errNoTargets", func(t *testing.T) {
		t.Parallel()
		base, _ := lockRepo(t, testLock, nil)
		args := []string{"-base", base, "-providers", filepath.Join(base, "providers"), "-mise", filepath.Join(base, "mise.toml"), "modules"}
		require.ErrorIs(t, runLockSync("lock-check", false, args, &bytes.Buffer{}, rootLocks), errNoTargets)
	})

	t.Run("走査の root が無ければ errMissingRoot", func(t *testing.T) {
		t.Parallel()
		base, _ := lockRepo(t, testLock, nil)
		args := []string{"-base", base, "-providers", filepath.Join(base, "providers"), "-mise", filepath.Join(base, "mise.toml"), "no-such"}
		require.ErrorIs(t, runLockSync("lock-check", false, args, &bytes.Buffer{}, rootLocks), errMissingRoot)
	})

	t.Run("追跡の一覧を取れなければ落とす", func(t *testing.T) {
		t.Parallel()
		_, args := lockRepo(t, testLock, nil)
		fail := func(string) ([]string, error) { return nil, errTool }
		require.ErrorIs(t, runLockSync("lock-check", false, args, &bytes.Buffer{}, fail), errTool)
	})

	t.Run("追跡しているファイルが0件なら errLock", func(t *testing.T) {
		t.Parallel()
		_, args := lockRepo(t, testLock, nil)
		require.ErrorIs(t, runLockSync("lock-apply", true, args, &bytes.Buffer{}, tracked()), errLock)
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
	assert.Equal(t, testLock, readFile(t, filepath.Join(base, "env", "admin", tflock.LockFile)))
}

func Test_runLockCheck(t *testing.T) {
	t.Parallel()

	base, args := lockRepo(t, testLock, map[string]string{"env/admin/.terraform.lock.hcl": "old\n"})
	require.ErrorIs(t, runLockCheck(args, &bytes.Buffer{}, rootLocks), errLock)
	assert.Equal(t, "old\n", readFile(t, filepath.Join(base, "env", "admin", tflock.LockFile)), "check は書かない")
}

func Test_diffRoots(t *testing.T) {
	t.Parallel()

	t.Run("違う root と lock の無い root を返す", func(t *testing.T) {
		t.Parallel()
		base := tree(t, map[string]string{"a/.terraform.lock.hcl": "same", "b/.terraform.lock.hcl": "old", "c/main.tf": ""})
		roots := []target{{rel: "a", path: filepath.Join(base, "a")}, {rel: "b", path: filepath.Join(base, "b")}, {rel: "c", path: filepath.Join(base, "c")}}
		changes, drifted, err := diffRoots(roots, []byte("same"))
		require.NoError(t, err)
		assert.Equal(t, []string{"b", "c"}, drifted)
		assert.Equal(t, map[string]string{filepath.Join(base, "b", tflock.LockFile): "same", filepath.Join(base, "c", tflock.LockFile): "same"}, changes)
	})

	t.Run("ENOENT でない読み取りの失敗はエラーで、ずれとして数えない", func(t *testing.T) {
		t.Parallel()
		base := tree(t, map[string]string{"a/.terraform.lock.hcl/x": ""})
		changes, drifted, err := diffRoots([]target{{rel: "a", path: filepath.Join(base, "a")}}, []byte("same"))
		require.Error(t, err)
		assert.Nil(t, changes)
		assert.Nil(t, drifted)
	})
}

func Test_checkTracked(t *testing.T) {
	t.Parallel()

	s := scan{base: "/b", roots: []string{"usecases", "env"}}
	roots := []target{{rel: "env/admin"}, {rel: "usecases/u/reference"}}
	both := []string{"env/admin/.terraform.lock.hcl", "usecases/u/reference/.terraform.lock.hcl"}

	t.Run("root と走査の外の lock だけなら nil", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, checkTracked(s, roots, tracked(append(both, "providers/.terraform.lock.hcl", "modules/m/.terraform.lock.hcl", "usecases/u/main.tf")...), true))
	})

	t.Run("走査の root の下の root でない lock を名指す", func(t *testing.T) {
		t.Parallel()
		err := checkTracked(s, roots, tracked(append(both, "usecases/u/.terraform.lock.hcl", "env/admin/sub/.terraform.lock.hcl")...), false)
		require.ErrorIs(t, err, errLock)
		assert.Contains(t, err.Error(), "env/admin/sub usecases/u")
	})

	t.Run("requireRoots なら、lock が追跡されていない root を名指す", func(t *testing.T) {
		t.Parallel()
		err := checkTracked(s, roots, tracked("env/admin/.terraform.lock.hcl"), true)
		require.ErrorIs(t, err, errLock)
		assert.Contains(t, err.Error(), "usecases/u/reference")
		require.NoError(t, checkTracked(s, roots, tracked("env/admin/.terraform.lock.hcl"), false), "requireRoots でなければ求めない")
	})

	t.Run("追跡しているファイルが0件なら errLock", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, checkTracked(s, roots, tracked(), false), errLock)
	})
}

func Test_underRoots(t *testing.T) {
	t.Parallel()

	for name, c := range map[string]struct {
		dir   string
		roots []string
		want  bool
	}{
		"root そのもの": {"env", []string{"env"}, true},
		"root の下":   {"env/admin", []string{"usecases", "env"}, true},
		"接頭辞が同じだけの別の名前": {"envx/admin", []string{"env"}, false},
		"走査の外": {"providers", []string{"env"}, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, c.want, underRoots(c.dir, c.roots))
		})
	}
}

func Test_gitLsFiles(t *testing.T) {
	t.Parallel()

	git := func(t *testing.T, dir string, args ...string) {
		t.Helper()
		cmd := exec.CommandContext(context.Background(), "git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))
	}

	t.Run("追跡しているファイルを返す", func(t *testing.T) {
		t.Parallel()
		base := tree(t, map[string]string{"a/.terraform.lock.hcl": "x", "b/untracked": "y", "c d/ü": "z"})
		git(t, base, "init", "-q")
		git(t, base, "add", "a/.terraform.lock.hcl", "c d/ü")
		got, err := gitLsFiles(base)
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"a/.terraform.lock.hcl", "c d/ü"}, got)
	})

	t.Run("追跡が0件のリポジトリは空を返す", func(t *testing.T) {
		t.Parallel()
		base := t.TempDir()
		git(t, base, "init", "-q")
		got, err := gitLsFiles(base)
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("git のリポジトリでなければ errTool", func(t *testing.T) {
		t.Parallel()
		_, err := gitLsFiles(t.TempDir())
		require.ErrorIs(t, err, errTool)
	})
}

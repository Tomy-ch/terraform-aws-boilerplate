package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/misetoml"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/toolrun/tooltest"
)

const lockMise = `[tools]
actionlint = "1.7.12"
"aqua:aws/aws-cli" = "2.36.40"
"github:terraform-linters/tflint-ruleset-aws" = "0.50.0"
"go:golang.org/x/vuln/cmd/govulncheck" = "1.10.0"
"npm:markdownlint-cli2" = "0.24.1"

[settings]
disable_tools = ["npm:markdownlint-cli2"]
lockfile_platforms = ["linux-x64"]
`

// lockSound は lockMise と一致する mise.lock。
const lockSound = `lockfile_version = 3

[[tools.actionlint]]
version = "1.7.12"
backend = "aqua:rhysd/actionlint"

[tools.actionlint."platforms.linux-x64"]
checksum = "sha256:a"
url = "https://github.com/rhysd/actionlint/releases/download/v1.7.12/actionlint_1.7.12_linux_amd64.tar.gz"

[[tools."aqua:aws/aws-cli"]]
version = "2.36.40"
backend = "aqua:aws/aws-cli"

[tools."aqua:aws/aws-cli"."platforms.linux-x64"]
url = "https://awscli.amazonaws.com/awscli-exe-linux-x86_64-2.36.40.zip"

[[tools."github:terraform-linters/tflint-ruleset-aws"]]
version = "0.50.0"
backend = "github:terraform-linters/tflint-ruleset-aws"

[tools."github:terraform-linters/tflint-ruleset-aws"."platforms.linux-x64"]
checksum = "sha256:b"
url = "https://github.com/terraform-linters/tflint-ruleset-aws/releases/download/v0.50.0/tflint-ruleset-aws_linux_amd64.zip"

[[tools."go:golang.org/x/vuln/cmd/govulncheck"]]
version = "1.10.0"
backend = "go:golang.org/x/vuln/cmd/govulncheck"
`

// lockRepo は mise.toml と mise.lock を持つ一時リポジトリの root を返す。lock が空なら mise.lock を置かない。
func lockRepo(t *testing.T, mise, lock string) string {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, miseFile), []byte(mise), 0o600))
	if lock != "" {
		require.NoError(t, os.WriteFile(filepath.Join(root, misetoml.LockFile), []byte(lock), 0o600))
	}

	return root
}

func Test_command(t *testing.T) {
	t.Parallel()

	c := command(context.Background(), "/x", []string{"A=1"}, stepMiseLock, "go", "aqua:o/r")
	assert.Equal(t, []string{"mise", "lock", "--", "go", "aqua:o/r"}, c.Args)
	assert.Equal(t, "/x", c.Dir)
	assert.Equal(t, []string{"A=1"}, c.Env)
}

func Test_lockTools(t *testing.T) {
	t.Parallel()

	t.Run("disable_tools の道具を除き、宣言の順で返す", func(t *testing.T) {
		t.Parallel()

		got, err := lockTools(lockRepo(t, lockMise, ""))

		require.NoError(t, err)
		keys := make([]string, 0, len(got))
		for _, e := range got {
			keys = append(keys, e.Key)
		}
		assert.Equal(t, []string{"actionlint", "aqua:aws/aws-cli", "github:terraform-linters/tflint-ruleset-aws", "go:golang.org/x/vuln/cmd/govulncheck"}, keys)
	})

	t.Run("mise.toml を読めなければエラー", func(t *testing.T) {
		t.Parallel()

		_, err := lockTools(t.TempDir())

		require.ErrorIs(t, err, os.ErrNotExist)
	})
}

func Test_writeLock(t *testing.T) {
	t.Parallel()

	fakeLock := func(t *testing.T, lock string) *tooltest.Fake {
		t.Helper()
		f := tooltest.New(t, tooltest.WithPrelude(`if [ -e "$ctl/lock.src" ]; then cp "$ctl/lock.src" ./mise.lock; fi`))
		if lock != "" {
			f.Set("lock", "src", lock)
		}
		return f
	}

	t.Run("disable_tools を除いた道具を名指して mise lock を起動し、書けた lock を検める", func(t *testing.T) {
		t.Parallel()
		root := lockRepo(t, lockMise, "")
		f := fakeLock(t, lockSound)

		require.NoError(t, writeLock(root, &bytes.Buffer{}, f.Tool()))

		assert.Equal(t, []string{"mise-lock actionlint aqua:aws/aws-cli github:terraform-linters/tflint-ruleset-aws go:golang.org/x/vuln/cmd/govulncheck"}, f.Args())
	})

	t.Run("書けた lock が宣言と合わなければ errLock", func(t *testing.T) {
		t.Parallel()
		root := lockRepo(t, lockMise, "")
		f := fakeLock(t, strings.Replace(lockSound, `version = "1.7.12"`, `version = "1.7.11"`, 1))

		require.ErrorIs(t, writeLock(root, &bytes.Buffer{}, f.Tool()), errLock)
	})

	t.Run("mise lock が 0 以外で終われば errLockFailed で、lock を検めない", func(t *testing.T) {
		t.Parallel()
		root := lockRepo(t, lockMise, "")
		f := fakeLock(t, "")
		f.Set("mise-lock", "rc", "1")

		require.ErrorIs(t, writeLock(root, &bytes.Buffer{}, f.Tool()), errLockFailed)
	})

	t.Run("mise を起動できなければ toolrun.ErrTool", func(t *testing.T) {
		t.Parallel()

		require.ErrorIs(t, writeLock(lockRepo(t, lockMise, ""), &bytes.Buffer{}, tooltest.Missing), toolrun.ErrTool)
	})

	t.Run("lock する道具が0件なら mise を起動せずに errShape", func(t *testing.T) {
		t.Parallel()
		f := fakeLock(t, "")

		require.ErrorIs(t, writeLock(lockRepo(t, "[tools]\n\"npm:a\" = \"1\"\n[settings]\ndisable_tools = [\"npm:a\"]\n", ""), &bytes.Buffer{}, f.Tool()), errShape)
		assert.Nil(t, f.Calls())
	})
}

func Test_checkLock(t *testing.T) {
	t.Parallel()

	t.Run("宣言と同じ道具・版で、取得元が配布元を指すなら成功する", func(t *testing.T) {
		t.Parallel()

		require.NoError(t, checkLock(lockRepo(t, lockMise, lockSound)))
	})

	tests := map[string]struct {
		mise, lock string
		want       error
		say        string
	}{
		"宣言した道具の entry が無い": {
			lockMise, strings.ReplaceAll(lockSound, "tools.actionlint", "tools.other"), errLock, "actionlint の entry がありません",
		},
		"版が宣言と違う": {
			lockMise, strings.Replace(lockSound, `version = "0.50.0"`, `version = "0.49.0"`, 1), errLock, "の版が 0.49.0",
		},
		"disable_tools の道具が lock にある": {
			lockMise, lockSound + "\n[[tools.\"npm:markdownlint-cli2\"]]\nversion = \"0.24.1\"\nbackend = \"npm:markdownlint-cli2\"\n", errLock, "npm:markdownlint-cli2 は lock する道具ではありません",
		},
		"取得元が別の org の Release を指す": {
			lockMise, strings.Replace(lockSound, "github.com/terraform-linters/tflint-ruleset-aws/releases", "github.com/other/tflint-ruleset-aws/releases", 1), errLock, "github.com/terraform-linters/tflint-ruleset-aws の Release です",
		},
		"lockfile_platforms が無い": {
			strings.Replace(lockMise, "lockfile_platforms = [\"linux-x64\"]\n", "", 1), lockSound, errShape, "lockfile_platforms がありません",
		},
		"lockfile_platforms が空": {
			strings.Replace(lockMise, `lockfile_platforms = ["linux-x64"]`, `lockfile_platforms = []`, 1), lockSound, errShape, "lockfile_platforms がありません",
		},
		"mise.lock が無い": {
			lockMise, "", os.ErrNotExist, misetoml.LockFile,
		},
	}
	for name, tt := range tests {
		t.Run(name+"なら落ち、理由を名指す", func(t *testing.T) {
			t.Parallel()

			err := checkLock(lockRepo(t, tt.mise, tt.lock))

			require.ErrorIs(t, err, tt.want)
			assert.Contains(t, err.Error(), tt.say)
		})
	}
}

func Test_checkSources(t *testing.T) {
	t.Parallel()

	github := misetoml.LockEntry{
		Key: "aqua:o/r", Version: "1.0.0", Backend: "aqua:o/r",
		Platforms: map[string]misetoml.LockPlatform{"linux-x64": {URL: "https://github.com/o/r/releases/download/v1.0.0/r.tar.gz"}},
	}

	t.Run("lockfile_platforms の各 platform の url が配布元を指すなら、問題を返さない", func(t *testing.T) {
		t.Parallel()

		assert.Empty(t, checkSources("aqua:o/r", github, []string{"linux-x64"}))
	})

	t.Run("lockfile_platforms の platform に url が無ければ名指す", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, []string{"aqua:o/r の linux-arm64 に url がありません"}, checkSources("aqua:o/r", github, []string{"linux-x64", "linux-arm64"}))
	})

	t.Run("lockfile_platforms に無い platform を名指す", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, []string{"aqua:o/r の linux-x64 は lockfile_platforms にありません"}, checkSources("aqua:o/r", github, nil))
	})

	t.Run("owner/repo を書いたキーの backend が別なら名指す", func(t *testing.T) {
		t.Parallel()
		e := github
		e.Backend = "aqua:x/r"

		assert.Equal(t, []string{"aqua:o/r の backend が aqua:x/r です"}, checkSources("aqua:o/r", e, []string{"linux-x64"}))
	})

	t.Run("go: の道具は platform を持たなければ問題を返さず、持てば名指す", func(t *testing.T) {
		t.Parallel()
		e := misetoml.LockEntry{Key: "go:x", Version: "1", Backend: "go:x", Platforms: map[string]misetoml.LockPlatform{}}
		assert.Empty(t, checkSources("go:x", e, []string{"linux-x64"}))

		e.Platforms["linux-x64"] = misetoml.LockPlatform{URL: "https://x/1"}
		assert.Equal(t, []string{"go:x は platform の取得元を持たない backend です"}, checkSources("go:x", e, []string{"linux-x64"}))
	})
}

func Test_checkURL(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		backend, version, url, want string
	}{
		"backend の Release を指せば空":        {"aqua:o/r", "1.0.0", "https://github.com/o/r/releases/download/v1.0.0/r.zip", ""},
		"GitHub の外の配布元を指せば空":             {"core:go", "1.27.4", "https://dl.google.com/go/go1.27.4.linux-amd64.tar.gz", ""},
		"https でなければ名指す":                 {"aqua:o/r", "1.0.0", "http://github.com/o/r/releases/download/v1.0.0/r.zip", "https の url ではありません"},
		"版を含まなければ名指す":                    {"aqua:o/r", "1.0.0", "https://github.com/o/r/releases/download/v1.0.1/r.zip", "版 1.0.0 を含みません"},
		"別の repo の Release なら名指す":        {"aqua:o/r", "1.0.0", "https://github.com/o/other/releases/download/v1.0.0/r.zip", "配布元は github.com/o/r の Release です"},
		"Release の外の github.com なら名指す":   {"aqua:o/r", "1.0.0", "https://github.com/o/r/archive/v1.0.0.zip", "配布元は github.com/o/r の Release です"},
		"GitHub の外の配布元が別の host なら名指す":    {"core:go", "1.27.4", "https://example.com/go1.27.4.tar.gz", "配布元は dl.google.com です"},
		"owner/repo を持たない backend なら名指す": {"core:python", "3.12.0", "https://example.com/3.12.0.tar.gz", "配布元を決められない backend です"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, checkURL(tt.backend, tt.version, tt.url))
		})
	}
}

func Test_repoOf(t *testing.T) {
	t.Parallel()

	for key, want := range map[string]string{"aqua:o/r": "o/r", "github:o/r": "o/r", "actionlint": "", "aqua:o": "", "aqua:o/r/x": "", "core:go": ""} {
		t.Run(key, func(t *testing.T) {
			t.Parallel()

			got, ok := repoOf(key)

			assert.Equal(t, want != "", ok)
			assert.Equal(t, want, got)
		})
	}
}

func Test_checkLockedInstalls(t *testing.T) {
	t.Parallel()

	write := func(t *testing.T, action, dockerfile string) string {
		t.Helper()
		root := t.TempDir()
		for path, content := range map[string]string{lockedInstalls[0].path: action, lockedInstalls[1].path: dockerfile} {
			require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, path)), 0o700))
			require.NoError(t, os.WriteFile(filepath.Join(root, path), []byte(content), 0o600))
		}
		return root
	}
	action := "      env:\n        MISE_LOCKED: \"1\"\n"
	dockerfile := "ENV A=1 \\\n    MISE_LOCKED=1\n"

	t.Run("setup-mise と Dockerfile が MISE_LOCKED を持てば成功する", func(t *testing.T) {
		t.Parallel()

		require.NoError(t, checkLockedInstalls(write(t, action, dockerfile)))
	})

	t.Run("どちらかが持たなければ errLock で、そのファイルを名指す", func(t *testing.T) {
		t.Parallel()

		err := checkLockedInstalls(write(t, "      env:\n        MISE_LOCKED: \"0\"\n", dockerfile))

		require.ErrorIs(t, err, errLock)
		assert.Contains(t, err.Error(), lockedInstalls[0].path)
		assert.NotContains(t, err.Error(), lockedInstalls[1].path)
	})

	t.Run("コメントの中の MISE_LOCKED は数えない", func(t *testing.T) {
		t.Parallel()

		err := checkLockedInstalls(write(t, action, "# MISE_LOCKED=1\n"))

		require.ErrorIs(t, err, errLock)
		assert.Contains(t, err.Error(), lockedInstalls[1].path)
	})

	t.Run("ファイルを読めなければエラー", func(t *testing.T) {
		t.Parallel()

		require.ErrorIs(t, checkLockedInstalls(t.TempDir()), os.ErrNotExist)
	})
}

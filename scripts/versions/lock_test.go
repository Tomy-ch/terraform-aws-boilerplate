package main

import (
	"bytes"
	"context"
	"fmt"
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
checksum = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
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
checksum = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
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

	sum := "sha256:" + strings.Repeat("c", 64)
	github := misetoml.LockEntry{
		Key: "aqua:o/r", Version: "1.0.0", Backend: "aqua:o/r",
		Platforms: map[string]misetoml.LockPlatform{"linux-x64": {URL: "https://github.com/o/r/releases/download/v1.0.0/r.tar.gz", Checksum: sum}},
	}

	t.Run("lockfile_platforms の各 platform の url が配布元を指し照合値を持つなら、問題を返さない", func(t *testing.T) {
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

	t.Run("照合値が無いか sha256 でなければ名指す", func(t *testing.T) {
		t.Parallel()
		for _, bad := range []string{"", "sha256:abc", "sha512:" + strings.Repeat("c", 64), "sha256:" + strings.Repeat("C", 64)} {
			e := github
			e.Platforms = map[string]misetoml.LockPlatform{"linux-x64": {URL: github.Platforms["linux-x64"].URL, Checksum: bad}}

			assert.Equal(t, []string{fmt.Sprintf("aqua:o/r の linux-x64 の照合値 %q が sha256 ではありません", bad)}, checkSources("aqua:o/r", e, []string{"linux-x64"}), bad)
		}
	})

	t.Run("照合値を持てない配布元は照合値が無くてよく、持てば名指す", func(t *testing.T) {
		t.Parallel()
		e := misetoml.LockEntry{
			Key: "aqua:aws/aws-cli", Version: "2.36.40", Backend: "aqua:aws/aws-cli",
			Platforms: map[string]misetoml.LockPlatform{"linux-x64": {URL: "https://awscli.amazonaws.com/awscli-exe-linux-x86_64-2.36.40.zip"}},
		}
		assert.Empty(t, checkSources("aqua:aws/aws-cli", e, []string{"linux-x64"}))

		e.Platforms["linux-x64"] = misetoml.LockPlatform{URL: e.Platforms["linux-x64"].URL, Checksum: sum}
		assert.Equal(t, []string{"aqua:aws/aws-cli の linux-x64 が照合値を持ちます（unverifiedSources から外す）"}, checkSources("aqua:aws/aws-cli", e, []string{"linux-x64"}))
	})

	t.Run("owner/repo を書いたキーの backend が別なら名指す", func(t *testing.T) {
		t.Parallel()
		e := github
		e.Backend = "aqua:x/r"

		assert.Equal(t, []string{"aqua:o/r の backend が aqua:x/r です（宣言からは aqua:o/r）"}, checkSources("aqua:o/r", e, []string{"linux-x64"}))
	})

	t.Run("短い名前の道具の backend が表と違えば名指す", func(t *testing.T) {
		t.Parallel()
		e := misetoml.LockEntry{
			Key: "actionlint", Version: "1.0.0", Backend: "aqua:evil/actionlint",
			Platforms: map[string]misetoml.LockPlatform{"linux-x64": {URL: "https://github.com/evil/actionlint/releases/download/v1.0.0/a.tar.gz", Checksum: sum}},
		}

		assert.Equal(t, []string{"actionlint の backend が aqua:evil/actionlint です（宣言からは aqua:rhysd/actionlint）"}, checkSources("actionlint", e, []string{"linux-x64"}))
	})

	t.Run("表に無い短い名前は名指す", func(t *testing.T) {
		t.Parallel()
		e := misetoml.LockEntry{Key: "unknown", Version: "1", Backend: "aqua:o/unknown"}

		assert.Equal(t, []string{"unknown の解決されるべき backend を決められません（shortBackends に足す）"}, checkSources("unknown", e, []string{"linux-x64"}))
	})

	t.Run("go: の道具は platform を持たなければ問題を返さず、持てば名指す", func(t *testing.T) {
		t.Parallel()
		e := misetoml.LockEntry{Key: "go:x", Version: "1", Backend: "go:x", Platforms: map[string]misetoml.LockPlatform{}}
		assert.Empty(t, checkSources("go:x", e, []string{"linux-x64"}))

		e.Platforms["linux-x64"] = misetoml.LockPlatform{URL: "https://x/1"}
		assert.Equal(t, []string{"go:x は platform の取得元を持たない backend です"}, checkSources("go:x", e, []string{"linux-x64"}))
	})

	t.Run("go: の道具の backend がキーと違えば名指す", func(t *testing.T) {
		t.Parallel()
		e := misetoml.LockEntry{Key: "go:x", Version: "1", Backend: "go:y"}

		assert.Equal(t, []string{"go:x の backend が go:y です（宣言からは go:x）"}, checkSources("go:x", e, nil))
	})
}

func Test_expectedBackend(t *testing.T) {
	t.Parallel()

	for key, want := range map[string]string{"aqua:o/r": "aqua:o/r", "github:o/r": "github:o/r", "go:x/y": "go:x/y", "npm:a": "npm:a", "actionlint": "aqua:rhysd/actionlint", "go": "core:go", "unknown": ""} {
		t.Run(key, func(t *testing.T) {
			t.Parallel()

			got, ok := expectedBackend(key)

			assert.Equal(t, want != "", ok)
			assert.Equal(t, want, got)
		})
	}
}

func Test_checkURL(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		backend, version, url, want string
	}{
		"backend の Release を指せば空":        {"aqua:o/r", "1.0.0", "https://github.com/o/r/releases/download/v1.0.0/r.zip", ""},
		"GitHub の外の配布元を指せば空":             {"core:go", "1.27.4", "https://dl.google.com/go/go1.27.4.linux-amd64.tar.gz", ""},
		"https でなければ名指す":                 {"aqua:o/r", "1.0.0", "http://github.com/o/r/releases/download/v1.0.0/r.zip", "query・fragment・利用者を持たない https の url ではありません"},
		"query があれば名指す":                  {"aqua:o/r", "1.0.0", "https://github.com/o/r/releases/download/v1.0.0/r.zip?x=1", "query・fragment・利用者を持たない https の url ではありません"},
		"fragment があれば名指す":               {"aqua:o/r", "1.0.0", "https://github.com/o/r/releases/download/v1.0.0/r.zip#x", "query・fragment・利用者を持たない https の url ではありません"},
		"利用者があれば名指す":                     {"aqua:o/r", "1.0.0", "https://u@github.com/o/r/releases/download/v1.0.0/r.zip", "query・fragment・利用者を持たない https の url ではありません"},
		"path に .. があれば名指す":              {"aqua:o/r", "1.0.0", "https://github.com/o/r/releases/download/v1.0.0/../../../../x/y/releases/download/v1.0.0/r.zip", "path が正規化されていません"},
		"版を含まなければ名指す":                    {"aqua:o/r", "1.0.0", "https://github.com/o/r/releases/download/v1.0.1/r.zip", "版 1.0.0 を含みません"},
		"版が別の版の一部としてしか現れなければ名指す":         {"aqua:o/r", "0.5", "https://github.com/o/r/releases/download/v0.50.0/r.zip", "版 0.5 を含みません"},
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

func Test_versionIn(t *testing.T) {
	t.Parallel()

	for name, tt := range map[string]struct {
		path, version string
		want          bool
	}{
		"v の後ろの版":           {"/o/r/releases/download/v1.7.12/a.tar.gz", "1.7.12", true},
		"名前の中の版の後ろに . と語":   {"/go/go1.27.4.linux-amd64.tar.gz", "1.27.4", true},
		"- の後ろで . と拡張子で終わる": {"/awscli-exe-linux-x86_64-2.36.40.zip", "2.36.40", true},
		"末尾の版":              {"/x/1.0.0", "1.0.0", true},
		"後ろに数字が続く":          {"/o/r/releases/download/v0.50.0/a", "0.5", false},
		"後ろに . と数字が続く":      {"/o/r/releases/download/v1.2.3/a", "1.2", false},
		"前に数字が付く":           {"/o/r/releases/download/v11.0.0/a", "1.0.0", false},
		"前に . が付く":          {"/o/r/releases/download/v2.1.0.0/a", "1.0.0", false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, versionIn(tt.path, tt.version))
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

// soundAction は、mise install を走らせる step が MISE_LOCKED を持つ composite action。
const soundAction = "" +
	"runs:\n" +
	"  using: composite\n" +
	"  steps:\n" +
	"    - name: Resolve\n" +
	"      shell: bash\n" +
	"      run: echo x\n" +
	"    - name: Install requested tools\n" +
	"      shell: bash\n" +
	"      env:\n" +
	"        MISE_LOCKED: \"1\"\n" +
	"      run: |\n" +
	"        mise install $TOOLS\n"

// soundToolsDockerfile は、最初の mise install より前に MISE_LOCKED を1つだけ代入する Dockerfile。
const soundToolsDockerfile = "# MISE_LOCKED=0 はコメント\nENV A=1 \\\n    MISE_LOCKED=1\nRUN mise install x\n"

func Test_checkLockedInstalls(t *testing.T) {
	t.Parallel()

	write := func(t *testing.T, action, dockerfile string) string {
		t.Helper()
		root := t.TempDir()
		for p, content := range map[string]string{setupMiseAction: action, toolsDockerfile: dockerfile} {
			require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(root, p)), 0o700))
			require.NoError(t, os.WriteFile(filepath.Join(root, p), []byte(content), 0o600))
		}
		return root
	}

	t.Run("setup-mise と Dockerfile が MISE_LOCKED を持てば成功する", func(t *testing.T) {
		t.Parallel()

		require.NoError(t, checkLockedInstalls(write(t, soundAction, soundToolsDockerfile)))
	})

	t.Run("どちらかが持たなければ errLock で、そのファイルを名指す", func(t *testing.T) {
		t.Parallel()

		err := checkLockedInstalls(write(t, strings.Replace(soundAction, `MISE_LOCKED: "1"`, `MISE_LOCKED: "0"`, 1), soundToolsDockerfile))

		require.ErrorIs(t, err, errLock)
		assert.Contains(t, err.Error(), setupMiseAction)
		assert.NotContains(t, err.Error(), toolsDockerfile)
	})

	t.Run("ファイルを読めなければエラー", func(t *testing.T) {
		t.Parallel()

		require.ErrorIs(t, checkLockedInstalls(t.TempDir()), os.ErrNotExist)
	})
}

func Test_lockedAction(t *testing.T) {
	t.Parallel()

	for name, tt := range map[string]struct {
		content, want string
	}{
		"mise install の step が MISE_LOCKED を持てば空": {soundAction, ""},
		"MISE_LOCKED を別の step へ移せば名指す": {
			strings.Replace(strings.Replace(soundAction, "      env:\n        MISE_LOCKED: \"1\"\n", "", 1), "      run: echo x\n", "      env:\n        MISE_LOCKED: \"1\"\n      run: echo x\n", 1),
			`step "Install requested tools" が MISE_LOCKED を "1" にしていません`,
		},
		"mise install の step が無ければ名指す": {strings.ReplaceAll(soundAction, "mise install", "mise exec"), "mise install を走らせる step がありません"},
		"YAML として読めなければ名指す":            {"runs: [", "読めません"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := lockedAction([]byte(tt.content))

			if tt.want == "" {
				assert.Empty(t, got)
				return
			}
			assert.Contains(t, got, tt.want)
		})
	}
}

func Test_lockedDockerfile(t *testing.T) {
	t.Parallel()

	for name, tt := range map[string]struct {
		content, want string
	}{
		"代入が1つで 1 で、mise install より前なら空": {soundToolsDockerfile, ""},
		"後から 0 で上書きすれば名指す":               {soundToolsDockerfile + "ENV MISE_LOCKED=0\n", "MISE_LOCKED の代入が 2 件です（1件にする）"},
		"代入が無ければ名指す":                     {"RUN mise install x\n", "MISE_LOCKED の代入が 0 件です（1件にする）"},
		"値が 1 でなければ名指す":                  {"ENV MISE_LOCKED=0\nRUN mise install x\n", "MISE_LOCKED が 0 です"},
		"mise install より後ろなら名指す":         {"RUN mise install x\nENV MISE_LOCKED=1\n", "MISE_LOCKED の代入が最初の mise install より後ろにあります"},
		"mise install が無ければ名指す":          {"ENV MISE_LOCKED=1\n", "mise install がありません"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, lockedDockerfile([]byte(tt.content)))
		})
	}
}

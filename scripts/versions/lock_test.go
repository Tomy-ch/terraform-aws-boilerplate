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

// lockTables は lockMise と lockSound に当てる表。孤児の検査が、fixture に無い本物の道具を名指さないように持つ。
var lockTables = sourceTables{
	short:      map[string]string{"actionlint": "aqua:rhysd/actionlint"},
	offGitHub:  map[string]string{"aqua:aws/aws-cli": "awscli.amazonaws.com"},
	unverified: map[string]string{"aqua:aws/aws-cli": "配布元が照合値を公開しない"},
}

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

		require.NoError(t, writeLock(root, &bytes.Buffer{}, f.Tool(), lockTables))

		assert.Equal(t, []string{"mise-lock actionlint aqua:aws/aws-cli github:terraform-linters/tflint-ruleset-aws go:golang.org/x/vuln/cmd/govulncheck"}, f.Args())
	})

	t.Run("mise lock の出力を out へ流す", func(t *testing.T) {
		t.Parallel()
		root := lockRepo(t, lockMise, "")
		f := fakeLock(t, lockSound)
		f.Set("mise-lock", "out", "lock を書きました\n")
		var out bytes.Buffer

		require.NoError(t, writeLock(root, &out, f.Tool(), lockTables))

		assert.Contains(t, out.String(), "lock を書きました")
	})

	t.Run("書けた lock が宣言と合わなければ errLock", func(t *testing.T) {
		t.Parallel()
		root := lockRepo(t, lockMise, "")
		f := fakeLock(t, strings.Replace(lockSound, `version = "1.7.12"`, `version = "1.7.11"`, 1))

		require.ErrorIs(t, writeLock(root, &bytes.Buffer{}, f.Tool(), lockTables), errLock)
	})

	t.Run("mise lock が 0 以外で終われば errLockFailed で、lock を検めない", func(t *testing.T) {
		t.Parallel()
		root := lockRepo(t, lockMise, "")
		f := fakeLock(t, "")
		f.Set("mise-lock", "rc", "1")

		require.ErrorIs(t, writeLock(root, &bytes.Buffer{}, f.Tool(), lockTables), errLockFailed)
	})

	t.Run("mise を起動できなければ toolrun.ErrTool", func(t *testing.T) {
		t.Parallel()

		require.ErrorIs(t, writeLock(lockRepo(t, lockMise, ""), &bytes.Buffer{}, tooltest.Missing, lockTables), toolrun.ErrTool)
	})

	t.Run("lock する道具が0件なら mise を起動せずに errShape", func(t *testing.T) {
		t.Parallel()
		f := fakeLock(t, "")

		require.ErrorIs(t, writeLock(lockRepo(t, "[tools]\n\"npm:a\" = \"1\"\n[settings]\ndisable_tools = [\"npm:a\"]\n", ""), &bytes.Buffer{}, f.Tool(), lockTables), errShape)
		assert.Nil(t, f.Calls())
	})
}

func Test_checkLock(t *testing.T) {
	t.Parallel()

	t.Run("宣言と同じ道具・版で、取得元が配布元を指すなら成功する", func(t *testing.T) {
		t.Parallel()

		require.NoError(t, checkLock(lockRepo(t, lockMise, lockSound), lockTables))
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
		"lock する道具が0件": {
			"[tools]\n\"npm:a\" = \"1\"\n[settings]\ndisable_tools = [\"npm:a\"]\nlockfile_platforms = [\"linux-x64\"]\n", "lockfile_version = 3\n", errShape, "lock する道具がありません",
		},
		"lockfile_platforms が読めない": {
			strings.Replace(lockMise, `lockfile_platforms = ["linux-x64"]`, "lockfile_platforms = [\n  \"linux-x64\",\n]", 1), lockSound, misetoml.ErrInvalidLockfilePlatforms, "lockfile_platforms",
		},
		"mise.lock が読めない": {
			lockMise, lockSound + "x = 1\n", misetoml.ErrInvalidLock, misetoml.LockFile,
		},
	}
	for name, tt := range tests {
		t.Run(name+"なら落ち、理由を名指す", func(t *testing.T) {
			t.Parallel()

			err := checkLock(lockRepo(t, tt.mise, tt.lock), lockTables)

			require.ErrorIs(t, err, tt.want)
			assert.Contains(t, err.Error(), tt.say)
		})
	}

	orphans := map[string]struct {
		tables sourceTables
		say    string
	}{
		"shortBackends に宣言に無い道具がある": {
			sourceTables{short: map[string]string{"actionlint": "aqua:rhysd/actionlint", "gone": "aqua:o/gone"}, offGitHub: lockTables.offGitHub, unverified: lockTables.unverified},
			"shortBackends の gone は mise.toml の [tools] にありません",
		},
		"offGitHubHosts に lock に無い backend がある": {
			sourceTables{short: lockTables.short, offGitHub: map[string]string{"aqua:aws/aws-cli": "awscli.amazonaws.com", "core:gone": "example.com"}, unverified: lockTables.unverified},
			"offGitHubHosts の core:gone は mise.lock のどの backend にも当たりません",
		},
		"unverifiedSources に lock に無い backend がある": {
			sourceTables{short: lockTables.short, offGitHub: lockTables.offGitHub, unverified: map[string]string{"aqua:aws/aws-cli": "x", "aqua:o/gone": "x"}},
			"unverifiedSources の aqua:o/gone は mise.lock のどの backend にも当たりません",
		},
	}
	for name, tt := range orphans {
		t.Run(name+"なら errLock で、そのエントリを名指す", func(t *testing.T) {
			t.Parallel()

			err := checkLock(lockRepo(t, lockMise, lockSound), tt.tables)

			require.ErrorIs(t, err, errLock)
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

	for name, bad := range map[string]string{
		"照合値が無い":     "",
		"桁が短い":       "sha256:abc",
		"63桁":        "sha256:" + strings.Repeat("c", 63),
		"65桁":        "sha256:" + strings.Repeat("c", 65),
		"sha256 でない": "sha512:" + strings.Repeat("c", 64),
		"大文字の16進":    "sha256:" + strings.Repeat("C", 64),
	} {
		t.Run(name+"なら照合値を名指す", func(t *testing.T) {
			t.Parallel()
			e := github
			e.Platforms = map[string]misetoml.LockPlatform{"linux-x64": {URL: github.Platforms["linux-x64"].URL, Checksum: bad}}

			assert.Equal(t, []string{fmt.Sprintf("aqua:o/r の linux-x64 の照合値 %q が sha256 ではありません", bad)}, checkSources("aqua:o/r", e, []string{"linux-x64"}))
		})
	}

	awscli := misetoml.LockEntry{
		Key: "aqua:aws/aws-cli", Version: "2.36.40", Backend: "aqua:aws/aws-cli",
		Platforms: map[string]misetoml.LockPlatform{"linux-x64": {URL: "https://awscli.amazonaws.com/awscli-exe-linux-x86_64-2.36.40.zip"}},
	}

	t.Run("照合値を持てない配布元は、照合値が無くても問題を返さない", func(t *testing.T) {
		t.Parallel()

		assert.Empty(t, checkSources("aqua:aws/aws-cli", awscli, []string{"linux-x64"}))
	})

	t.Run("照合値を持てない配布元が照合値を持てば名指す", func(t *testing.T) {
		t.Parallel()
		e := awscli
		e.Platforms = map[string]misetoml.LockPlatform{"linux-x64": {URL: awscli.Platforms["linux-x64"].URL, Checksum: sum}}

		assert.Equal(t, []string{"aqua:aws/aws-cli の linux-x64 が照合値を持ちます（unverifiedSources から外す）"}, checkSources("aqua:aws/aws-cli", e, []string{"linux-x64"}))
	})

	t.Run("url と照合値の両方に問題があれば両方を名指す", func(t *testing.T) {
		t.Parallel()
		e := github
		e.Platforms = map[string]misetoml.LockPlatform{"linux-x64": {URL: "https://github.com/x/r/releases/download/v1.0.0/r.tar.gz"}}

		assert.Equal(t, []string{
			"aqua:o/r の linux-x64 の url https://github.com/x/r/releases/download/v1.0.0/r.tar.gz: 配布元は github.com/o/r の Release です",
			`aqua:o/r の linux-x64 の照合値 "" が sha256 ではありません`,
		}, checkSources("aqua:o/r", e, []string{"linux-x64"}))
	})

	t.Run("platform の見出しはあるが url が空なら名指す", func(t *testing.T) {
		t.Parallel()
		e := github
		e.Platforms = map[string]misetoml.LockPlatform{"linux-x64": {Checksum: sum}}

		assert.Equal(t, []string{"aqua:o/r の linux-x64 に url がありません"}, checkSources("aqua:o/r", e, []string{"linux-x64"}))
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

	t.Run("go: の道具が platform を持たなければ問題を返さない", func(t *testing.T) {
		t.Parallel()
		e := misetoml.LockEntry{Key: "go:x", Version: "1", Backend: "go:x", Platforms: map[string]misetoml.LockPlatform{}}

		assert.Empty(t, checkSources("go:x", e, []string{"linux-x64"}))
	})

	for _, key := range []string{"go:x", "npm:x"} {
		t.Run(key+" の道具が platform を持てば名指す", func(t *testing.T) {
			t.Parallel()
			e := misetoml.LockEntry{Key: key, Version: "1", Backend: key, Platforms: map[string]misetoml.LockPlatform{"linux-x64": {URL: "https://x/1"}}}

			assert.Equal(t, []string{key + " は platform の取得元を持たない backend です"}, checkSources(key, e, []string{"linux-x64"}))
		})
	}

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
		"url として読めなければ名指す":               {"aqua:o/r", "1.0.0", "https://github.com/%zz", "query・fragment・利用者を持たない https の url ではありません"},
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

	t.Run("Dockerfile だけが外れれば errLock で、Dockerfile だけを名指す", func(t *testing.T) {
		t.Parallel()

		err := checkLockedInstalls(write(t, soundAction, "RUN mise install x\n"))

		require.ErrorIs(t, err, errLock)
		assert.Contains(t, err.Error(), toolsDockerfile)
		assert.NotContains(t, err.Error(), setupMiseAction)
	})

	t.Run("両方が外れれば両方を名指す", func(t *testing.T) {
		t.Parallel()

		err := checkLockedInstalls(write(t, strings.Replace(soundAction, `MISE_LOCKED: "1"`, `MISE_LOCKED: "0"`, 1), "RUN mise install x\n"))

		require.ErrorIs(t, err, errLock)
		assert.Contains(t, err.Error(), setupMiseAction)
		assert.Contains(t, err.Error(), toolsDockerfile)
	})

	t.Run("action が無ければ、action の path を添えたエラー", func(t *testing.T) {
		t.Parallel()
		root := write(t, soundAction, soundToolsDockerfile)
		require.NoError(t, os.Remove(filepath.Join(root, setupMiseAction)))

		err := checkLockedInstalls(root)

		require.ErrorIs(t, err, os.ErrNotExist)
		assert.Contains(t, err.Error(), setupMiseAction)
	})

	t.Run("Dockerfile が無ければ、Dockerfile の path を添えたエラー", func(t *testing.T) {
		t.Parallel()
		root := write(t, soundAction, soundToolsDockerfile)
		require.NoError(t, os.Remove(filepath.Join(root, toolsDockerfile)))

		err := checkLockedInstalls(root)

		require.ErrorIs(t, err, os.ErrNotExist)
		assert.Contains(t, err.Error(), toolsDockerfile)
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
		"mise が道具を入れる step が無ければ名指す": {strings.ReplaceAll(soundAction, "mise install $TOOLS", "mise --version"), "mise が道具を入れる step がありません"},
		"2つ目の install の step が MISE_LOCKED を持たなければ名指す": {
			soundAction + "    - name: Second\n      shell: bash\n      run: mise install y\n",
			`step "Second" が MISE_LOCKED を "1" にしていません`,
		},
		"mise exec で道具を入れる step が MISE_LOCKED を持たなければ名指す": {
			soundAction + "    - name: Exec\n      shell: bash\n      run: mise exec y -- y\n",
			`step "Exec" が MISE_LOCKED を "1" にしていません`,
		},
		"空白が2つの mise  install も道具を入れる step として数える": {
			soundAction + "    - name: Spaced\n      shell: bash\n      run: mise  install y\n",
			`step "Spaced" が MISE_LOCKED を "1" にしていません`,
		},
		"YAML として読めなければ名指す": {"runs: [", "読めません"},
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
		"代入が2つなら名指す":                     {soundToolsDockerfile + "ENV MISE_LOCKED=0\n", "MISE_LOCKED の代入が 2 件です（1件にする）"},
		"代入が無ければ名指す":                     {"RUN mise install x\n", "MISE_LOCKED の代入が 0 件です（1件にする）"},
		"値が 1 でなければ名指す":                  {"ENV MISE_LOCKED=0\nRUN mise install x\n", "MISE_LOCKED が 0 です"},
		"mise install より後ろの行なら名指す":       {"RUN mise install x\nENV MISE_LOCKED=1\n", "MISE_LOCKED の代入が、mise が最初に道具を入れるより後ろにあります"},
		"同じ行で mise install の後ろに置けば名指す":   {"RUN mise install x; export MISE_LOCKED=1\n", "MISE_LOCKED の代入が、mise が最初に道具を入れるより後ろにあります"},
		"# を含む行の代入も数える":                  {"ENV A=\"a#b\" MISE_LOCKED=0\nENV MISE_LOCKED=1\nRUN mise install x\n", "MISE_LOCKED の代入が 2 件です（1件にする）"},
		"# で始まる行の mise install は数えない":    {"ENV MISE_LOCKED=1\n# RUN mise install x\n", "mise が道具を入れる行がありません"},
		"mise が道具を入れる行が無ければ名指す":          {"ENV MISE_LOCKED=1\n", "mise が道具を入れる行がありません"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, lockedDockerfile([]byte(tt.content)))
		})
	}
}

func Test_orphanEntries(t *testing.T) {
	t.Parallel()

	tools := []misetoml.Entry{{Key: "actionlint", Version: "1"}}
	locked := []misetoml.LockEntry{{Key: "actionlint", Backend: "aqua:rhysd/actionlint"}, {Key: "aqua:aws/aws-cli", Backend: "aqua:aws/aws-cli"}}

	t.Run("どの表のエントリも宣言と lock に当たれば、問題を返さない", func(t *testing.T) {
		t.Parallel()

		assert.Empty(t, orphanEntries(tools, locked, lockTables))
	})

	t.Run("3つの表の孤児を、表ごとに名前の順で名指す", func(t *testing.T) {
		t.Parallel()
		tables := sourceTables{
			short:      map[string]string{"zz": "aqua:o/zz", "actionlint": "aqua:rhysd/actionlint", "aa": "aqua:o/aa"},
			offGitHub:  map[string]string{"core:gone": "example.com"},
			unverified: map[string]string{"aqua:o/gone": "x"},
		}

		assert.Equal(t, []string{
			"shortBackends の aa は mise.toml の [tools] にありません",
			"shortBackends の zz は mise.toml の [tools] にありません",
			"offGitHubHosts の core:gone は mise.lock のどの backend にも当たりません",
			"unverifiedSources の aqua:o/gone は mise.lock のどの backend にも当たりません",
		}, orphanEntries(tools, locked, tables))
	})
}

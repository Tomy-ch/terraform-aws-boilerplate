package main

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/tflock"
)

const (
	providerKey     = providerBackend + ":registry.terraform.io/hashicorp/aws"
	providerFixture = "provider \"registry.terraform.io/hashicorp/aws\" {\n  version     = \"6.73.0\"\n  constraints = \"6.73.0\"\n  hashes = [\n    \"zh:01\",\n  ]\n}\n"
)

// writeProviderDecl は dir に provider の正本の lock を置く。
func writeProviderDecl(t *testing.T, dir string) {
	t.Helper()
	path := filepath.Join(dir, providerLockFile)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte(providerFixture), 0o600))
}

func Test_parseProviderLock(t *testing.T) {
	t.Parallel()

	t.Run("lock の provider を terraform-registry の key と版で返す", func(t *testing.T) {
		t.Parallel()
		got, err := parseProviderLock([]byte(providerFixture))
		require.NoError(t, err)
		assert.Equal(t, []tool{{key: providerKey, version: "6.73.0"}}, got)
	})

	t.Run("読めない lock は ErrInvalidLock", func(t *testing.T) {
		t.Parallel()
		_, err := parseProviderLock([]byte("provider \"x\" {\n"))
		require.ErrorIs(t, err, tflock.ErrInvalidLock)
	})

	t.Run("版が X.Y.Z でない lock は ErrInvalidLock", func(t *testing.T) {
		t.Parallel()
		_, err := parseProviderLock([]byte(strings.Replace(providerFixture, "\"6.73.0\"\n  constraints", "\"6.73.0/../6.0.0\"\n  constraints", 1)))
		require.ErrorIs(t, err, tflock.ErrInvalidLock)
	})

	t.Run("provider が0件の lock は ErrInvalidLock", func(t *testing.T) {
		t.Parallel()
		_, err := parseProviderLock([]byte("# x\n"))
		require.ErrorIs(t, err, tflock.ErrInvalidLock)
	})
}

func Test_registryProvider(t *testing.T) {
	t.Parallel()

	t.Run("Terraform Registry の host を外して namespace/type を返す", func(t *testing.T) {
		t.Parallel()
		got, err := registryProvider("registry.terraform.io/hashicorp/aws")
		require.NoError(t, err)
		assert.Equal(t, "hashicorp/aws", got)
	})

	for name, addr := range map[string]string{
		"Terraform Registry でない host": "example.com/hashicorp/aws",
		"host が無い":                    "hashicorp/aws",
		"段が多い":                        "registry.terraform.io/a/b/c",
	} {
		t.Run(name+"は errUnsupportedBackend", func(t *testing.T) {
			t.Parallel()
			_, err := registryProvider(addr)
			require.ErrorIs(t, err, errUnsupportedBackend)
		})
	}
}

func Test_registryProviderAt(t *testing.T) {
	t.Parallel()

	t.Run("v1 の published_at を返す", func(t *testing.T) {
		t.Parallel()
		client := fakeUpstream(t, respondJSON("/v1/providers/hashicorp/aws/6.73.0", `{"published_at":"2026-11-11T21:33:11Z"}`))
		at, err := registryProviderAt(t.Context(), client, "registry.terraform.io/hashicorp/aws", "6.73.0")
		require.NoError(t, err)
		assert.Equal(t, "2026-11-11T21:33:11Z", at.Format(time.RFC3339))
	})

	t.Run("published_at が無ければ errNotFound", func(t *testing.T) {
		t.Parallel()
		client := fakeUpstream(t, respondJSON("/v1/providers/hashicorp/aws/6.73.0", `{}`))
		_, err := registryProviderAt(t.Context(), client, "registry.terraform.io/hashicorp/aws", "6.73.0")
		require.ErrorIs(t, err, errNotFound)
	})

	t.Run("JSON でない応答はエラー", func(t *testing.T) {
		t.Parallel()
		client := fakeUpstream(t, respondJSON("/v1/providers/hashicorp/aws/6.73.0", `not json`))
		_, err := registryProviderAt(t.Context(), client, "registry.terraform.io/hashicorp/aws", "6.73.0")
		require.Error(t, err)
		require.NotErrorIs(t, err, errNotFound)
	})

	t.Run("published_at が RFC3339 でなければエラー", func(t *testing.T) {
		t.Parallel()
		client := fakeUpstream(t, respondJSON("/v1/providers/hashicorp/aws/6.73.0", `{"published_at":"2026/11/11"}`))
		_, err := registryProviderAt(t.Context(), client, "registry.terraform.io/hashicorp/aws", "6.73.0")
		require.Error(t, err)
	})

	t.Run("上流の 404 は errNotFound", func(t *testing.T) {
		t.Parallel()
		client := fakeUpstream(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
		_, err := registryProviderAt(t.Context(), client, "registry.terraform.io/hashicorp/aws", "9.9.9")
		require.ErrorIs(t, err, errNotFound)
	})

	t.Run("Terraform Registry でない provider は問い合わせずに errUnsupportedBackend", func(t *testing.T) {
		t.Parallel()
		_, err := registryProviderAt(t.Context(), &http.Client{Transport: unreachableUpstream{}}, "example.com/x/y", "1.0.0")
		require.ErrorIs(t, err, errUnsupportedBackend)
	})
}

func Test_registryProviderReleases(t *testing.T) {
	t.Parallel()

	t.Run("v2 の provider-versions の安定版だけを返す", func(t *testing.T) {
		t.Parallel()
		body := `{"included":[` +
			`{"type":"provider-versions","attributes":{"version":"6.73.0","published-at":"2026-11-11T21:33:11Z"}},` +
			`{"type":"provider-versions","attributes":{"version":"7.0.0-beta1","published-at":"2026-11-20T00:00:00Z"}},` +
			`{"type":"provider-platforms","attributes":{"version":"6.74.0","published-at":"2026-11-20T00:00:00Z"}},` +
			`{"type":"provider-versions","attributes":{"version":"6.74.0"}}]}`
		client := fakeUpstream(t, respondJSON("/v2/providers/hashicorp/aws", body))
		got, err := registryProviderReleases(t.Context(), client, "registry.terraform.io/hashicorp/aws")
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, "6.73.0", got[0].version)
	})

	t.Run("安定版が1つも無ければ errNotFound", func(t *testing.T) {
		t.Parallel()
		client := fakeUpstream(t, respondJSON("/v2/providers/hashicorp/aws", `{"included":[{"type":"provider-versions","attributes":{"version":"7.0.0-beta1","published-at":"2026-11-20T00:00:00Z"}},{"type":"provider-platforms","attributes":{"version":"6.74.0","published-at":"2026-11-20T00:00:00Z"}}]}`))
		_, err := registryProviderReleases(t.Context(), client, "registry.terraform.io/hashicorp/aws")
		require.ErrorIs(t, err, errNotFound)
	})

	t.Run("JSON でない応答はエラー", func(t *testing.T) {
		t.Parallel()
		client := fakeUpstream(t, respondJSON("/v2/providers/hashicorp/aws", `not json`))
		_, err := registryProviderReleases(t.Context(), client, "registry.terraform.io/hashicorp/aws")
		require.Error(t, err)
		require.NotErrorIs(t, err, errNotFound)
	})

	t.Run("版が1つも無ければ errNotFound", func(t *testing.T) {
		t.Parallel()
		client := fakeUpstream(t, respondJSON("/v2/providers/hashicorp/aws", `{"included":[]}`))
		_, err := registryProviderReleases(t.Context(), client, "registry.terraform.io/hashicorp/aws")
		require.ErrorIs(t, err, errNotFound)
	})

	t.Run("Terraform Registry でない provider は errUnsupportedBackend", func(t *testing.T) {
		t.Parallel()
		_, err := registryProviderReleases(t.Context(), &http.Client{Transport: unreachableUpstream{}}, "example.com/x/y")
		require.ErrorIs(t, err, errUnsupportedBackend)
	})
}

func Test_parseBypasses(t *testing.T) {
	t.Parallel()

	t.Run("エントリを読む", func(t *testing.T) {
		t.Parallel()
		got, err := parseBypasses(strings.NewReader(`"a@1" = { expires = 2026-12-01, issue = 1, reason = "x" }` + "\n"))
		require.NoError(t, err)
		assert.Contains(t, got, "a@1")
	})

	t.Run("解釈できない行は errBypassInvalidLine", func(t *testing.T) {
		t.Parallel()
		_, err := parseBypasses(strings.NewReader("x\n"))
		require.ErrorIs(t, err, errBypassInvalidLine)
	})
}

func Test_introduced(t *testing.T) {
	t.Parallel()

	same := bypass{expires: day("2026-01-01"), issue: 1, reason: "r"}
	current := map[string]bypass{"kept@1": same, "added@2": same, "new@3": same, "changed@4": {expires: day("2026-01-02"), issue: 1, reason: "r"}}
	base := map[string]bypass{"kept@1": same, "added@2": same, "changed@4": same}
	violations := []violation{{key: "kept@1"}, {key: "added@2"}, {key: "new@3"}, {key: "changed@4"}}

	got := introduced(violations, []tool{{key: "added", version: "2"}}, current, base)
	assert.Equal(t, []violation{{key: "added@2"}, {key: "new@3"}, {key: "changed@4"}}, got, "変えていないエントリの違反は数えない")
}

// useBypassGateRepo は、mise.toml と semgrep.mk と provider の正本の lock と、バイパス lockfile（base）を持つ commit を
// 1 つ作り、作業ツリーのバイパス lockfile を current へ差し替えてそこへ移動する。
func useBypassGateRepo(t *testing.T, base, current string) {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, miseFile), []byte(miseOneAquaTool), 0o600))
	writeSemgrepDecl(t, dir)
	writeProviderDecl(t, dir)
	writeWorkTreeBypass(t, dir, base)
	git := []string{"-C", dir, "-c", "user.email=t@example.com", "-c", "user.name=t", "-c", "commit.gpgsign=false"}
	for _, args := range [][]string{{"init", "-q"}, {"add", "-A"}, {"commit", "-q", "-m", "init"}} {
		//nolint:gosec // 引数は本ファイル内のリテラルと t.TempDir のパス
		out, err := exec.CommandContext(t.Context(), "git", append(git, args...)...).CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}
	writeWorkTreeBypass(t, dir, current)
	t.Chdir(dir)
}

func Test_bypassesAt(t *testing.T) { //nolint:paralleltest // t.Chdir を使う
	expired := `"aqua:owner/repo@9.9.9" = { expires = 2026-01-01, issue = 1, reason = "x" }` + "\n"

	t.Run("base のバイパス lockfile を読む", func(t *testing.T) {
		useBypassGateRepo(t, expired, "")
		got, err := bypassesAt("HEAD", command)
		require.NoError(t, err)
		assert.Contains(t, got, "aqua:owner/repo@9.9.9")
	})

	t.Run("base に無ければ空", func(t *testing.T) {
		useMiseGateRepo(t, miseOneAquaTool, miseOneAquaTool)
		got, err := bypassesAt("HEAD", command)
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("base を解決できなければ errBaseUnresolved", func(t *testing.T) {
		useMiseGateRepo(t, miseOneAquaTool, miseOneAquaTool)
		_, err := bypassesAt("no-such-ref", command)
		require.ErrorIs(t, err, errBaseUnresolved)
	})
}

func Test_run_providerとバイパスのgate(t *testing.T) { //nolint:paralleltest // t.Chdir を使う
	now := day("2026-08-06")
	expired := `"aqua:owner/repo@9.9.9" = { expires = 2026-01-01, issue = 1, reason = "x" }` + "\n"

	t.Run("gate は、変えていない期限切れのバイパスで落とさず、audit は落とす", func(t *testing.T) {
		useBypassGateRepo(t, expired, expired)
		_, err := runCaptured(t, []string{"gate", "--base=HEAD"}, &http.Client{Transport: unreachableUpstream{}}, now, command)
		require.NoError(t, err)
		_, err = runCaptured(t, []string{"audit"}, &http.Client{Transport: unreachableUpstream{}}, now, command)
		require.ErrorIs(t, err, errBlocking)
	})

	t.Run("gate は、この変更が足した期限切れのバイパスで落とす", func(t *testing.T) {
		useBypassGateRepo(t, "", expired)
		out, err := runCaptured(t, []string{"gate", "--base=HEAD"}, &http.Client{Transport: unreachableUpstream{}}, now, command)
		require.ErrorIs(t, err, errBlocking)
		assert.Contains(t, out, "期限 2026-01-01 が切れています")
	})

	t.Run("gate は、base に provider の正本の lock が無ければ、今の版を足されたものとして窓に掛ける", func(t *testing.T) {
		useGateRepo(t, miseOneAquaTool, miseOneAquaTool, miseFile, semgrepFile)
		client := fakeUpstream(t, respondJSON("/v1/providers/hashicorp/aws/6.73.0", `{"published_at":"2026-08-05T00:00:00Z"}`))
		out, err := runCaptured(t, []string{"gate", "--base=HEAD", "--github"}, client, now, command)
		require.ErrorIs(t, err, errBlocking)
		assert.Contains(t, out, "::error file="+providerLockFile+"::"+providerKey+"@6.73.0")
	})
}

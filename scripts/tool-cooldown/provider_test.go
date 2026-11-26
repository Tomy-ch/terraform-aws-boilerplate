package main

import (
	"net/http"
	"os"
	"path/filepath"

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

	t.Run("provider が0件の lock は ErrInvalidLock", func(t *testing.T) {
		t.Parallel()
		_, err := parseProviderLock([]byte("# x\n"))
		require.ErrorIs(t, err, tflock.ErrInvalidLock)
	})
}

func Test_registryProvider(t *testing.T) {
	t.Parallel()

	got, err := registryProvider("registry.terraform.io/hashicorp/aws")
	require.NoError(t, err)
	assert.Equal(t, "hashicorp/aws", got)

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

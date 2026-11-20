package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_upstream_get(t *testing.T) {
	t.Parallel()

	t.Run("200 なら本文を返す", func(t *testing.T) {
		t.Parallel()
		up := fakeUpstream(t, map[string]string{"/a": "body"})
		got, err := up.get(context.Background(), up.raw+"/a")
		require.NoError(t, err)
		assert.Equal(t, "body", string(got))
	})

	t.Run("200 以外は取得の失敗にする", func(t *testing.T) {
		t.Parallel()
		up := fakeUpstream(t, map[string]string{})
		_, err := up.get(context.Background(), up.raw+"/a")
		require.ErrorIs(t, err, errHTTP)
		assert.Contains(t, err.Error(), "HTTP 404")
	})

	t.Run("接続できなければ取得の失敗にする", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.NotFoundHandler())
		url := srv.URL
		srv.Close()
		_, err := upstream{client: http.DefaultClient}.get(context.Background(), url+"/a")
		require.ErrorIs(t, err, errHTTP)
	})

	t.Run("URL を組めなければ取得の失敗にする", func(t *testing.T) {
		t.Parallel()
		_, err := upstream{client: http.DefaultClient}.get(context.Background(), "://bad")
		require.ErrorIs(t, err, errHTTP)
	})

	t.Run("本文が上限ちょうどなら返し、1バイト超えれば取得の失敗にする", func(t *testing.T) {
		t.Parallel()
		up := fakeUpstream(t, map[string]string{
			"/max":  strings.Repeat("a", maxBody),
			"/over": strings.Repeat("a", maxBody+1),
		})
		got, err := up.get(context.Background(), up.raw+"/max")
		require.NoError(t, err)
		assert.Len(t, got, maxBody)
		_, err = up.get(context.Background(), up.raw+"/over")
		require.ErrorIs(t, err, errHTTP)
	})

	t.Run("本文を読み切れなければ取得の失敗にする", func(t *testing.T) {
		t.Parallel()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Length", "100")
			_, _ = w.Write([]byte("short"))
		}))
		t.Cleanup(srv.Close)
		_, err := upstream{client: srv.Client()}.get(context.Background(), srv.URL)
		require.ErrorIs(t, err, errHTTP)
	})
}

func Test_upstream_providerVersions(t *testing.T) {
	t.Parallel()

	const path = "/v1/providers/hashicorp/aws/versions"

	t.Run("版の文字列を一覧の順に返す", func(t *testing.T) {
		t.Parallel()
		up := fakeUpstream(t, map[string]string{path: `{"versions":[{"version":"6.70.0"},{"version":"6.73.0"}]}`})
		got, err := up.providerVersions(context.Background(), "hashicorp/aws")
		require.NoError(t, err)
		assert.Equal(t, []string{"6.70.0", "6.73.0"}, got)
	})

	t.Run("版の一覧が空なら成功で返さない", func(t *testing.T) {
		t.Parallel()
		up := fakeUpstream(t, map[string]string{path: `{"versions":[]}`})
		_, err := up.providerVersions(context.Background(), "hashicorp/aws")
		require.ErrorIs(t, err, errNoVersions)
	})

	t.Run("versions の鍵が無ければ空として失敗する", func(t *testing.T) {
		t.Parallel()
		up := fakeUpstream(t, map[string]string{path: `{"id":"hashicorp/aws"}`})
		_, err := up.providerVersions(context.Background(), "hashicorp/aws")
		require.ErrorIs(t, err, errNoVersions)
	})

	t.Run("version の無い要素があれば解釈の失敗にする", func(t *testing.T) {
		t.Parallel()
		up := fakeUpstream(t, map[string]string{path: `{"versions":[{"version":"6.73.0"},{}]}`})
		_, err := up.providerVersions(context.Background(), "hashicorp/aws")
		require.ErrorIs(t, err, errBadVersion)
	})

	t.Run("本文が空なら解釈の失敗にする", func(t *testing.T) {
		t.Parallel()
		up := fakeUpstream(t, map[string]string{path: ""})
		_, err := up.providerVersions(context.Background(), "hashicorp/aws")
		require.ErrorIs(t, err, errBadVersion)
	})

	t.Run("JSON でなければ解釈の失敗にする", func(t *testing.T) {
		t.Parallel()
		up := fakeUpstream(t, map[string]string{path: `<html>`})
		_, err := up.providerVersions(context.Background(), "hashicorp/aws")
		require.ErrorIs(t, err, errBadVersion)
	})

	t.Run("取得できなければ取得の失敗にする", func(t *testing.T) {
		t.Parallel()
		up := fakeUpstream(t, map[string]string{})
		_, err := up.providerVersions(context.Background(), "hashicorp/aws")
		require.ErrorIs(t, err, errHTTP)
	})
}

func Test_latestMatching(t *testing.T) {
	t.Parallel()

	const constraint = ">= 6.68.0, < 7.0.0"

	t.Run("制約を満たす最新の正式版を選ぶ", func(t *testing.T) {
		t.Parallel()
		got, err := latestMatching([]string{"6.68.0", "6.73.0", "6.70.0", "7.0.0", "6.67.9"}, constraint)
		require.NoError(t, err)
		assert.Equal(t, "6.73.0", got)
	})

	t.Run("制約の下限そのものを満たす版として選ぶ", func(t *testing.T) {
		t.Parallel()
		got, err := latestMatching([]string{"6.67.9", "6.68.0"}, constraint)
		require.NoError(t, err)
		assert.Equal(t, "6.68.0", got)
	})

	t.Run("prerelease が最新でも選ばない", func(t *testing.T) {
		t.Parallel()
		got, err := latestMatching([]string{"6.73.0", "6.74.0-beta1", "7.0.0-beta1"}, constraint)
		require.NoError(t, err)
		assert.Equal(t, "6.73.0", got)
	})

	t.Run("制約を満たすのが prerelease だけなら失敗する", func(t *testing.T) {
		t.Parallel()
		_, err := latestMatching([]string{"6.74.0-beta1", "7.0.0"}, constraint)
		require.ErrorIs(t, err, errNoMatch)
	})

	t.Run("一覧が空なら失敗する", func(t *testing.T) {
		t.Parallel()
		_, err := latestMatching(nil, constraint)
		require.ErrorIs(t, err, errNoMatch)
	})

	t.Run("解釈できない版があれば失敗する", func(t *testing.T) {
		t.Parallel()
		_, err := latestMatching([]string{"6.73.0", "latest"}, constraint)
		require.ErrorIs(t, err, errBadVersion)
	})

	t.Run("解釈できない制約は失敗にする", func(t *testing.T) {
		t.Parallel()
		_, err := latestMatching([]string{"6.73.0"}, ">>> 6")
		require.ErrorIs(t, err, errBadConstraint)
	})
}

func Test_upstream_sdkVersion(t *testing.T) {
	t.Parallel()

	t.Run("タグの go.mod から版と URL を返す", func(t *testing.T) {
		t.Parallel()
		up := fakeUpstream(t, map[string]string{"/hashicorp/terraform-provider-aws/v6.73.0/go.mod": providerGoMod})
		got, url, err := up.sdkVersion(context.Background(), "6.73.0")
		require.NoError(t, err)
		assert.Equal(t, "v2.0.0-beta.76", got)
		assert.Equal(t, up.raw+"/hashicorp/terraform-provider-aws/v6.73.0/go.mod", url)
	})

	t.Run("取得できなければ URL を残して失敗する", func(t *testing.T) {
		t.Parallel()
		up := fakeUpstream(t, map[string]string{})
		_, url, err := up.sdkVersion(context.Background(), "6.73.0")
		require.ErrorIs(t, err, errHTTP)
		assert.NotEmpty(t, url)
	})
}

func Test_sdkVersionFromGoMod(t *testing.T) {
	t.Parallel()

	t.Run("require の版を返す", func(t *testing.T) {
		t.Parallel()
		got, err := sdkVersionFromGoMod([]byte(providerGoMod))
		require.NoError(t, err)
		assert.Equal(t, "v2.0.0-beta.76", got)
	})

	t.Run("別の module の replace は版に影響しない", func(t *testing.T) {
		t.Parallel()
		got, err := sdkVersionFromGoMod([]byte(providerGoMod + "replace github.com/goccy/go-yaml => github.com/x/go-yaml v1.0.0\n"))
		require.NoError(t, err)
		assert.Equal(t, "v2.0.0-beta.76", got)
	})

	t.Run("replace が差し替えていれば失敗する", func(t *testing.T) {
		t.Parallel()
		_, err := sdkVersionFromGoMod([]byte(providerGoMod + "replace github.com/hashicorp/aws-sdk-go-base/v2 => ../sdk\n"))
		require.ErrorIs(t, err, errGoMod)
	})

	t.Run("require に無ければ失敗する", func(t *testing.T) {
		t.Parallel()
		_, err := sdkVersionFromGoMod([]byte("module x\n\ngo 1.25\n"))
		require.ErrorIs(t, err, errGoMod)
	})

	t.Run("go.mod として解釈できなければ失敗する", func(t *testing.T) {
		t.Parallel()
		_, err := sdkVersionFromGoMod([]byte("require (\n"))
		require.ErrorIs(t, err, errGoMod)
	})
}

func Test_upstream_endpoints(t *testing.T) {
	t.Parallel()

	const path = "/hashicorp/aws-sdk-go-base/v2.0.0-beta.76/endpoints/endpoints_gen.go"

	t.Run("SDK のタグの endpoints と URL を返す", func(t *testing.T) {
		t.Parallel()
		up := fakeUpstream(t, map[string]string{path: endpointsSrc})
		got, url, err := up.endpoints(context.Background(), "v2.0.0-beta.76")
		require.NoError(t, err)
		assert.Equal(t, endpointsSrc, string(got))
		assert.Equal(t, up.raw+path, url)
	})

	t.Run("取得できなければ URL を残して失敗する", func(t *testing.T) {
		t.Parallel()
		up := fakeUpstream(t, map[string]string{})
		_, url, err := up.endpoints(context.Background(), "v2.0.0-beta.76")
		require.ErrorIs(t, err, errHTTP)
		assert.Equal(t, up.raw+path, url)
	})
}

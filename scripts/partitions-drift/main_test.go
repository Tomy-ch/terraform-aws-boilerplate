package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const (
	versionsTF = `terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = ">= 6.68.0, < 7.0.0"
    }
  }
}
`
	copyHCL = `variables {
  aws_partitions = {
    "aws"    = ["us-east-1", "ap-northeast-1"]
    "aws-cn" = ["cn-north-1"]
  }
}

run "x" {
  command = plan
}
`
	endpointsSrc = `package endpoints

const (
	AwsPartitionID   = "aws"
	AwsCnPartitionID = "aws-cn"
)

const (
	UsEast1RegionID      = "us-east-1"
	ApNortheast1RegionID = "ap-northeast-1"
	CnNorth1RegionID     = "cn-north-1"
)

var (
	partitions = map[string]Partition{
		AwsPartitionID: {
			id: AwsPartitionID,
			regions: map[string]Region{
				UsEast1RegionID:      {id: UsEast1RegionID},
				ApNortheast1RegionID: {id: ApNortheast1RegionID},
			},
			services: map[string]Service{"s3": {id: "s3"}},
		},
		AwsCnPartitionID: {
			id: AwsCnPartitionID,
			regions: map[string]Region{
				CnNorth1RegionID: {id: CnNorth1RegionID},
			},
		},
	}
)
`
	providerGoMod = "module github.com/hashicorp/terraform-provider-aws\n\ngo 1.25\n\nrequire github.com/hashicorp/aws-sdk-go-base/v2 v2.0.0-beta.76\n"
)

// fakeUpstream の routes に無いパスは 404 です。
func fakeUpstream(t *testing.T, routes map[string]string) upstream {
	t.Helper()
	up, _ := countingUpstream(t, routes)

	return up
}

// countingUpstream は fakeUpstream に、届いた要求の数を足したものです。
func countingUpstream(t *testing.T, routes map[string]string) (upstream, *atomic.Int64) {
	t.Helper()
	calls := &atomic.Int64{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		body, ok := routes[r.URL.Path]
		if !ok {
			http.NotFound(w, r)

			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	return upstream{client: srv.Client(), registry: srv.URL, raw: srv.URL}, calls
}

func defaultRoutes() map[string]string {
	return map[string]string{
		"/v1/providers/hashicorp/aws/versions":                                 `{"versions":[{"version":"6.70.0"},{"version":"6.73.0"},{"version":"7.0.0-beta1"},{"version":"7.1.0"}]}`,
		"/hashicorp/terraform-provider-aws/v6.73.0/go.mod":                     providerGoMod,
		"/hashicorp/aws-sdk-go-base/v2.0.0-beta.76/endpoints/endpoints_gen.go": endpointsSrc,
	}
}

func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	return path
}

func Test_exitCode(t *testing.T) {
	t.Parallel()

	t.Run("一致は0、ずれは1、それ以外は2を返す", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, 0, exitCode(nil))
		assert.Equal(t, 1, exitCode(xerrors.Wrap(errDrift, "x")))
		assert.Equal(t, 2, exitCode(errHTTP))
		assert.Equal(t, 2, exitCode(errUsage))
	})
}

func Test_stateOf(t *testing.T) {
	t.Parallel()

	t.Run("一致は match、ずれは drift、それ以外は unconfirmed", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "match", stateOf(nil))
		assert.Equal(t, "drift", stateOf(xerrors.Wrap(errDrift, "x")))
		assert.Equal(t, "unconfirmed", stateOf(errHTTP))
		assert.Equal(t, "unconfirmed", stateOf(errNotCompared))
	})
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }

// nonEmptyDir は、消すことも書くこともできない出力先です。
func nonEmptyDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "keep"), nil, 0o600))

	return dir
}

func readState(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(t, err)

	return string(raw)
}

func Test_run(t *testing.T) {
	t.Parallel()

	t.Run("一致すれば要約を書いて成功する", func(t *testing.T) {
		t.Parallel()
		up := fakeUpstream(t, defaultRoutes())
		summary := filepath.Join(t.TempDir(), "s.md")
		state := filepath.Join(t.TempDir(), "state")
		var out bytes.Buffer
		require.NoError(t, run(context.Background(), []string{
			"-versions-tf", writeFile(t, "versions.tf", versionsTF),
			"-copy", writeFile(t, "c.tftest.hcl", copyHCL),
			"-summary-out", summary,
			"-state-out", state,
		}, up, &out))
		assert.Equal(t, "match\n", readState(t, state))

		raw, err := os.ReadFile(summary)
		require.NoError(t, err)
		assert.Equal(t, out.String(), string(raw))
		assert.Contains(t, string(raw), "比べた hashicorp/aws: 6.73.0")
	})

	t.Run("ずれていれば errDrift を返し、差を要約に書く", func(t *testing.T) {
		t.Parallel()
		up := fakeUpstream(t, defaultRoutes())
		summary := filepath.Join(t.TempDir(), "s.md")
		state := filepath.Join(t.TempDir(), "state")
		cp := strings.Replace(copyHCL, `["cn-north-1"]`, `["cn-north-1", "cn-south-9"]`, 1)
		err := run(context.Background(), []string{
			"-versions-tf", writeFile(t, "versions.tf", versionsTF),
			"-copy", writeFile(t, "c.tftest.hcl", cp),
			"-summary-out", summary,
			"-state-out", state,
		}, up, &bytes.Buffer{})
		require.ErrorIs(t, err, errDrift)
		assert.Equal(t, "drift\n", readState(t, state))

		raw, readErr := os.ReadFile(summary)
		require.NoError(t, readErr)
		assert.Contains(t, string(raw), "| 上流に無い | `aws-cn` | `cn-south-9` |")
	})

	t.Run("上流を確認できなくても要約を書き、確認不能のエラーを返す", func(t *testing.T) {
		t.Parallel()
		up := fakeUpstream(t, map[string]string{})
		summary := filepath.Join(t.TempDir(), "s.md")
		state := filepath.Join(t.TempDir(), "state")
		err := run(context.Background(), []string{
			"-versions-tf", writeFile(t, "versions.tf", versionsTF),
			"-copy", writeFile(t, "c.tftest.hcl", copyHCL),
			"-summary-out", summary,
			"-state-out", state,
		}, up, &bytes.Buffer{})
		require.ErrorIs(t, err, errHTTP)
		assert.Equal(t, "unconfirmed\n", readState(t, state))
		assert.Equal(t, 2, exitCode(err))

		raw, readErr := os.ReadFile(summary)
		require.NoError(t, readErr)
		assert.Contains(t, string(raw), "上流と比べられませんでした")
	})

	t.Run("必須の引数が無ければ判定を書かずに使い方の誤りにする", func(t *testing.T) {
		t.Parallel()
		state := filepath.Join(t.TempDir(), "state")
		for _, args := range [][]string{{"-copy", "x"}, {"-versions-tf", "y"}} {
			err := run(context.Background(), append(args, "-state-out", state), upstream{}, &bytes.Buffer{})
			require.ErrorIs(t, err, errUsage)
		}
		assert.NoFileExists(t, state)
	})

	t.Run("前の実行の要約と判定は、今回書かずに終わっても残さない", func(t *testing.T) {
		t.Parallel()
		summary := writeFile(t, "s.md", "## ✅ 前の実行")
		state := writeFile(t, "state", "match\n")
		err := run(context.Background(), []string{
			"-copy", "x", "-summary-out", summary, "-state-out", state,
		}, upstream{}, &bytes.Buffer{})
		require.ErrorIs(t, err, errUsage)
		assert.NoFileExists(t, summary)
		assert.NoFileExists(t, state)
	})

	t.Run("前の実行の要約と判定を消せなければ、それぞれの失敗にする", func(t *testing.T) {
		t.Parallel()
		err := run(context.Background(), []string{
			"-copy", "x", "-versions-tf", "y", "-state-out", nonEmptyDir(t),
		}, upstream{}, &bytes.Buffer{})
		require.ErrorIs(t, err, errStateFile)
		err = run(context.Background(), []string{
			"-copy", "x", "-versions-tf", "y", "-summary-out", nonEmptyDir(t),
		}, upstream{}, &bytes.Buffer{})
		require.ErrorIs(t, err, errSummaryFile)
	})

	t.Run("-state-out を省けば判定のファイルを書かずに成功する", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		up := fakeUpstream(t, defaultRoutes())
		require.NoError(t, run(context.Background(), []string{
			"-versions-tf", writeFile(t, "versions.tf", versionsTF),
			"-copy", writeFile(t, "c.tftest.hcl", copyHCL),
			"-summary-out", filepath.Join(dir, "s.md"),
		}, up, &bytes.Buffer{}))
		entries, err := os.ReadDir(dir)
		require.NoError(t, err)
		assert.Len(t, entries, 1)
	})

	t.Run("要約を標準出力へ書けなければ要約も判定も書かずに失敗する", func(t *testing.T) {
		t.Parallel()
		up := fakeUpstream(t, defaultRoutes())
		summary := filepath.Join(t.TempDir(), "s.md")
		state := filepath.Join(t.TempDir(), "state")
		err := run(context.Background(), []string{
			"-versions-tf", writeFile(t, "versions.tf", versionsTF),
			"-copy", writeFile(t, "c.tftest.hcl", copyHCL),
			"-summary-out", summary,
			"-state-out", state,
		}, up, failingWriter{})
		require.ErrorIs(t, err, errOutput)
		assert.NoFileExists(t, summary)
		assert.NoFileExists(t, state)
	})

	t.Run("位置引数があれば使い方の誤りにする", func(t *testing.T) {
		t.Parallel()
		err := run(context.Background(), []string{"-copy", "x", "-versions-tf", "y", "z"}, upstream{}, &bytes.Buffer{})
		require.ErrorIs(t, err, errUsage)
	})

	t.Run("知らないフラグは使い方の誤りにする", func(t *testing.T) {
		t.Parallel()
		err := run(context.Background(), []string{"-nope"}, upstream{}, &bytes.Buffer{})
		require.ErrorIs(t, err, errUsage)
	})

	t.Run("要約を書けなければ判定も書かずに失敗する", func(t *testing.T) {
		t.Parallel()
		up := fakeUpstream(t, defaultRoutes())
		state := filepath.Join(t.TempDir(), "state")
		err := run(context.Background(), []string{
			"-versions-tf", writeFile(t, "versions.tf", versionsTF),
			"-copy", writeFile(t, "c.tftest.hcl", copyHCL),
			"-summary-out", filepath.Join(t.TempDir(), "missing", "s.md"),
			"-state-out", state,
		}, up, &bytes.Buffer{})
		require.ErrorIs(t, err, errSummaryFile)
		assert.NoFileExists(t, state)
	})

	t.Run("判定を書けなければ失敗する", func(t *testing.T) {
		t.Parallel()
		up := fakeUpstream(t, defaultRoutes())
		err := run(context.Background(), []string{
			"-versions-tf", writeFile(t, "versions.tf", versionsTF),
			"-copy", writeFile(t, "c.tftest.hcl", copyHCL),
			"-state-out", filepath.Join(t.TempDir(), "missing", "state"),
		}, up, &bytes.Buffer{})
		require.ErrorIs(t, err, errStateFile)
	})
}

func Test_compareWithUpstream(t *testing.T) {
	t.Parallel()

	const (
		versionsPath  = "/v1/providers/hashicorp/aws/versions"
		goModPath     = "/hashicorp/terraform-provider-aws/v6.73.0/go.mod"
		endpointsPath = "/hashicorp/aws-sdk-go-base/v2.0.0-beta.76/endpoints/endpoints_gen.go"
	)
	// want* は、失敗した段までに分かった値が report に残ることを見る。
	cases := []struct {
		name           string
		mutate         func(map[string]string)
		copy           string
		version        string
		want           error
		wantCalls      int64
		wantConstraint string
		wantProvider   string
		wantSDK        string
		wantGoModURL   bool
		wantURL        bool
	}{
		{name: "写しを読めなければ上流に問い合わせずに失敗する", copy: "variables {}\n", want: errCopyCount},
		{name: "versions.tf に aws の宣言が無ければ上流に問い合わせずに失敗する", version: "terraform {}\n", want: errConstraint},
		{
			name: "版の一覧を取れなければ制約だけを残して失敗する", mutate: func(r map[string]string) { delete(r, versionsPath) },
			want: errHTTP, wantCalls: 1, wantConstraint: ">= 6.68.0, < 7.0.0",
		},
		{
			name: "制約を満たす版が無ければ失敗する", mutate: func(r map[string]string) { r[versionsPath] = `{"versions":[{"version":"5.0.0"}]}` },
			want: errNoMatch, wantCalls: 1, wantConstraint: ">= 6.68.0, < 7.0.0",
		},
		{
			name: "go.mod を取れなければ provider の版と URL を残して失敗する", mutate: func(r map[string]string) { delete(r, goModPath) },
			want: errHTTP, wantCalls: 2, wantConstraint: ">= 6.68.0, < 7.0.0", wantProvider: "6.73.0", wantGoModURL: true,
		},
		{
			name: "go.mod が aws-sdk-go-base を差し替えていれば失敗する", mutate: func(r map[string]string) {
				r[goModPath] = providerGoMod + "replace github.com/hashicorp/aws-sdk-go-base/v2 => ../sdk\n"
			},
			want: errGoMod, wantCalls: 2, wantConstraint: ">= 6.68.0, < 7.0.0", wantProvider: "6.73.0", wantGoModURL: true,
		},
		{
			name: "endpoints を取れなければ SDK の版と URL を残して失敗する", mutate: func(r map[string]string) { delete(r, endpointsPath) },
			want: errHTTP, wantCalls: 3, wantConstraint: ">= 6.68.0, < 7.0.0", wantProvider: "6.73.0", wantSDK: "v2.0.0-beta.76",
			wantGoModURL: true, wantURL: true,
		},
		{
			name: "endpoints を解釈できなければ失敗する", mutate: func(r map[string]string) { r[endpointsPath] = "package endpoints\n" },
			want: errEndpointsShape, wantCalls: 3, wantConstraint: ">= 6.68.0, < 7.0.0", wantProvider: "6.73.0", wantSDK: "v2.0.0-beta.76",
			wantGoModURL: true, wantURL: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			routes := defaultRoutes()
			if tc.mutate != nil {
				tc.mutate(routes)
			}
			cp, vt := copyHCL, versionsTF
			if tc.copy != "" {
				cp = tc.copy
			}
			if tc.version != "" {
				vt = tc.version
			}
			up, calls := countingUpstream(t, routes)
			r := compareWithUpstream(context.Background(), up,
				writeFile(t, "versions.tf", vt), writeFile(t, "c.tftest.hcl", cp))
			require.ErrorIs(t, r.err, tc.want)
			assert.False(t, r.compared)
			assert.Equal(t, tc.wantCalls, calls.Load())
			assert.Equal(t, tc.wantConstraint, r.constraint)
			assert.Equal(t, tc.wantProvider, r.provider)
			assert.Equal(t, tc.wantSDK, r.sdk)
			assert.Equal(t, tc.wantGoModURL, r.goModURL != "")
			assert.Equal(t, tc.wantURL, r.endpointsURL != "")
		})
	}

	t.Run("比べ終えたら版と URL と差を残す", func(t *testing.T) {
		t.Parallel()
		r := compareWithUpstream(context.Background(), fakeUpstream(t, defaultRoutes()),
			writeFile(t, "versions.tf", versionsTF), writeFile(t, "c.tftest.hcl", copyHCL))
		require.NoError(t, r.err)
		assert.True(t, r.compared)
		assert.Equal(t, ">= 6.68.0, < 7.0.0", r.constraint)
		assert.Equal(t, "6.73.0", r.provider)
		assert.Equal(t, "v2.0.0-beta.76", r.sdk)
		assert.True(t, r.drift.empty())
	})
}

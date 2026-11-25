package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/fakeexec"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_awsProfile(t *testing.T) {
	t.Parallel()

	t.Run("AWS_PROFILE を返す", func(t *testing.T) {
		t.Parallel()

		p, err := awsProfile(profileEnv)
		require.NoError(t, err)
		assert.Equal(t, "p", p)
	})

	t.Run("AWS_PROFILE が空なら errNoProfile", func(t *testing.T) {
		t.Parallel()

		_, err := awsProfile(func(string) string { return "" })
		require.ErrorIs(t, err, errNoProfile)
	})

	t.Run("静的な資格情報が在れば、その名前を添えて errStaticCredentials", func(t *testing.T) {
		t.Parallel()

		_, err := awsProfile(func(k string) string {
			if k == "AWS_ACCESS_KEY_ID" || k == "AWS_PROFILE" {
				return "x"
			}

			return ""
		})
		require.ErrorIs(t, err, errStaticCredentials)
		assert.Contains(t, err.Error(), "AWS_ACCESS_KEY_ID")
	})
}

func Test_hostAWS(t *testing.T) {
	t.Parallel()

	t.Run("引数どおりに起動し、標準出力と標準エラーを返す", func(t *testing.T) {
		t.Parallel()

		bin := filepath.Join(t.TempDir(), "aws")
		fakeexec.Write(t, bin, "#!/bin/sh\necho \"out:$*\"\necho err >&2\n", 0o700)

		r := hostAWS(bin)("ssm", "get-parameter")
		require.NoError(t, r.err)
		assert.Equal(t, "out:ssm get-parameter\n", string(r.stdout))
		assert.Equal(t, "err\n", r.stderr)
	})

	t.Run("非ゼロの終了は *exec.ExitError", func(t *testing.T) {
		t.Parallel()

		bin := filepath.Join(t.TempDir(), "aws")
		fakeexec.Write(t, bin, "#!/bin/sh\nexit 3\n", 0o700)

		r := hostAWS(bin)("x", "y")
		require.Error(t, r.err)
		assert.Equal(t, 3, r.err.(interface{ ExitCode() int }).ExitCode())
	})

	t.Run("起動できなければ err を返す", func(t *testing.T) {
		t.Parallel()

		r := hostAWS(filepath.Join(t.TempDir(), "missing"))("x", "y")
		require.Error(t, r.err)
		assert.ErrorIs(t, r.err, os.ErrNotExist)
	})
}

func Test_op(t *testing.T) {
	t.Parallel()

	t.Run("サービスと操作を繋ぐ", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "ssm put-parameter", op([]string{"ssm", "put-parameter", "--name", "x"}))
	})

	t.Run("2つに満たなければそのまま繋ぐ", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "ssm", op([]string{"ssm"}))
	})
}

func Test_isRead(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		args []string
		want bool
	}{
		"get- は読み取り":          {[]string{"ssm", "get-parameter"}, true},
		"list- は読み取り":         {[]string{"organizations", "list-accounts"}, true},
		"describe- は読み取り":     {[]string{"cloudformation", "describe-organizations-access"}, true},
		"put- は読み取りでない":       {[]string{"ssm", "put-parameter"}, false},
		"activate- は読み取りでない":  {[]string{"cloudformation", "activate-organizations-access"}, false},
		"simulate- は確認でも組まない": {[]string{"iam", "simulate-principal-policy"}, false},
		"操作が無ければ読み取りでない":      {[]string{"ssm"}, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, isRead(tc.args))
		})
	}
}

func Test_client_call(t *testing.T) {
	t.Parallel()

	t.Run("--output json と client の region を付けて起動する", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{"ssm get-parameter": ok("{}")})
		_, err := testClient(f).call("", "ssm", "get-parameter")
		require.NoError(t, err)
		assert.Equal(t, [][]string{{"ssm", "get-parameter", "--output", "json", "--region", testRegion}}, f.calls)
	})

	t.Run("region を渡せば、client の region の代わりに使う", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{"service-quotas list-service-quotas": ok("{}")})
		_, err := testClient(f).call("us-east-1", "service-quotas", "list-service-quotas")
		require.NoError(t, err)
		assert.Equal(t, "us-east-1", f.calls[0][len(f.calls[0])-1])
	})

	t.Run("一覧に無い書き込みの操作は起動せずに errNotAllowed", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, nil)
		_, err := testClient(f, "ssm put-parameter").call("", "ssm", "delete-parameter")
		require.ErrorIs(t, err, errNotAllowed)
		assert.Empty(t, f.calls)
	})

	t.Run("一覧に在る書き込みの操作は起動する", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{"ssm put-parameter": ok("")})
		_, err := testClient(f, "ssm put-parameter").call("", "ssm", "put-parameter")
		require.NoError(t, err)
		assert.Equal(t, []string{"ssm put-parameter"}, f.ops())
	})

	t.Run("非ゼロの終了は、標準エラーの最初の行を添えて errAWSFailed", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{"ssm get-parameter": fail(t, "\nAn error occurred (AccessDenied)\nmore")})
		_, err := testClient(f).call("", "ssm", "get-parameter")
		require.ErrorIs(t, err, errAWSFailed)
		assert.Contains(t, err.Error(), "An error occurred (AccessDenied)")
	})

	t.Run("起動の失敗は errAWSStart", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{"ssm get-parameter": {err: os.ErrNotExist}})
		_, err := testClient(f).call("", "ssm", "get-parameter")
		require.ErrorIs(t, err, errAWSStart)
		assert.ErrorIs(t, err, os.ErrNotExist)
	})
}

func Test_client_read(t *testing.T) {
	t.Parallel()

	t.Run("応答の JSON を読む", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{"account get-primary-email": ok(`{"PrimaryEmail":"a@b.c"}`)})

		var v struct{ PrimaryEmail string }
		require.NoError(t, testClient(f).read(&v, "account", "get-primary-email"))
		assert.Equal(t, "a@b.c", v.PrimaryEmail)
	})

	t.Run("JSON でない応答は errAWSResponse", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{"account get-primary-email": ok("not json")})

		var v struct{}
		require.ErrorIs(t, testClient(f).read(&v, "account", "get-primary-email"), errAWSResponse)
	})

	t.Run("書き込みの操作は、一覧に在っても read からは起動しない", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, nil)

		var v struct{}
		require.ErrorIs(t, testClient(f, "ssm put-parameter").read(&v, "ssm", "put-parameter"), errNotAllowed)
		assert.Empty(t, f.calls)
	})
}

func Test_client_readIn(t *testing.T) {
	t.Parallel()

	t.Run("渡した region で読む", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{"service-quotas get-service-quota": ok(`{"Quota":{"Value":10}}`)})

		var v struct{ Quota struct{ Value float64 } }
		require.NoError(t, testClient(f).readIn("us-east-1", &v, "service-quotas", "get-service-quota"))
		assert.InDelta(t, 10, v.Quota.Value, 0)
		assert.Equal(t, "us-east-1", f.calls[0][len(f.calls[0])-1])
	})
}

func Test_client_readOptional(t *testing.T) {
	t.Parallel()

	t.Run("notFound の失敗は、無い（false）として返す", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{"ssm get-parameter": fail(t, "An error occurred (ParameterNotFound) when calling")})

		var v struct{}
		found, err := testClient(f).readOptional("ParameterNotFound", &v, "ssm", "get-parameter")
		require.NoError(t, err)
		assert.False(t, found)
	})

	t.Run("ほかの失敗はエラーのまま返す", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{"ssm get-parameter": fail(t, "An error occurred (AccessDeniedException)")})

		var v struct{}
		_, err := testClient(f).readOptional("ParameterNotFound", &v, "ssm", "get-parameter")
		require.ErrorIs(t, err, errAWSFailed)
	})

	t.Run("在れば true", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{"ssm get-parameter": ok("{}")})

		var v struct{}
		found, err := testClient(f).readOptional("ParameterNotFound", &v, "ssm", "get-parameter")
		require.NoError(t, err)
		assert.True(t, found)
	})
}

func Test_client_write(t *testing.T) {
	t.Parallel()

	t.Run("一覧に在る書き込みを起動する", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{"ssm put-parameter": ok("")})
		require.NoError(t, testClient(f, "ssm put-parameter").write("ssm", "put-parameter", "--name", "n"))
		assert.Equal(t, []string{"ssm put-parameter"}, f.writes())
	})

	t.Run("読み取りの操作は write からは起動しない", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, nil)
		require.ErrorIs(t, testClient(f).write("ssm", "get-parameter"), errNotAllowed)
		assert.Empty(t, f.calls)
	})

	t.Run("失敗は errAWSFailed", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{"ssm put-parameter": fail(t, "boom")})
		err := testClient(f, "ssm put-parameter").write("ssm", "put-parameter")
		require.ErrorIs(t, err, errAWSFailed)
		assert.False(t, xerrors.Is(err, errNotAllowed))
	})
}

func Test_firstLine(t *testing.T) {
	t.Parallel()

	t.Run("空白だけでない最初の行", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "b", firstLine(" \n\nb\r\nc"))
	})

	t.Run("無ければ「不明」", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "不明", firstLine(" \n"))
	})
}

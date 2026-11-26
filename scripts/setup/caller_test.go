package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const roleCaller = "arn:aws:sts::111111111111:assumed-role/tabp-dev-admin/s"

func callerResult(account, arn string) map[string]awsResult {
	return map[string]awsResult{"sts get-caller-identity": ok(`{"Account":"` + account + `","Arn":"` + arn + `"}`)}
}

func Test_requireAccountCaller(t *testing.T) {
	t.Parallel()

	t.Run("role なら role の名前と末尾4桁を出す", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer

		f := newFakeAWS(t, callerResult("123456789012", "arn:aws:sts::123456789012:assumed-role/tabp-admin/alice"))
		require.NoError(t, requireAccountCaller(testClient(f), "tabp-dev", &out))
		assert.Equal(t, "# 書き込む先: profile tabp-dev、region ap-northeast-1、role tabp-admin、アカウント ID の末尾 9012\n", out.String())
		assert.NotContains(t, out.String(), "123456789012")
	})

	t.Run("IAM user なら user の名前（path を除く）を出す", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer

		f := newFakeAWS(t, callerResult("111111111111", "arn:aws:iam::111111111111:user/ops/bob"))
		require.NoError(t, requireAccountCaller(testClient(f), "p", &out))
		assert.Contains(t, out.String(), "、IAM user bob、アカウント ID の末尾 1111")
	})

	t.Run("組織の管理アカウントであることを読まない", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, callerResult("111111111111", "arn:aws:iam::111111111111:user/bob"))
		require.NoError(t, requireAccountCaller(testClient(f), "p", &bytes.Buffer{}))
		assert.Equal(t, []string{"sts get-caller-identity"}, f.joined())
	})

	for name, arn := range map[string]string{
		"root":                       "arn:aws:iam::111111111111:root",
		"federated user":             "arn:aws:sts::111111111111:federated-user/x",
		"session の無い assumed-role":   "arn:aws:sts::111111111111:assumed-role/x",
		"role の名前が空の assumed-role":   "arn:aws:sts::111111111111:assumed-role//s",
		"ARN の形でない":                  "role/x",
		"ARN の account が12桁でない role": "arn:aws:sts::1111:assumed-role/x/s",
	} {
		t.Run(name+"なら errPrecondition で、何も出さない", func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer

			f := newFakeAWS(t, callerResult("111111111111", arn))
			require.ErrorIs(t, requireAccountCaller(testClient(f), "p", &out), errPrecondition)
			assert.Empty(t, out.String())
		})
	}

	t.Run("アカウント ID が12桁でなければ errAWSResponse", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, callerResult("11111111111", "arn:aws:iam::111111111111:user/x"))
		require.ErrorIs(t, requireAccountCaller(testClient(f), "p", &bytes.Buffer{}), errAWSResponse)
	})

	t.Run("ARN が無ければ errAWSResponse", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{"sts get-caller-identity": ok(`{"Account":"111111111111"}`)})
		require.ErrorIs(t, requireAccountCaller(testClient(f), "p", &bytes.Buffer{}), errAWSResponse)
	})

	t.Run("読めなければ errAWSFailed", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{"sts get-caller-identity": fail(t, "ExpiredToken")})
		require.ErrorIs(t, requireAccountCaller(testClient(f), "p", &bytes.Buffer{}), errAWSFailed)
	})
}

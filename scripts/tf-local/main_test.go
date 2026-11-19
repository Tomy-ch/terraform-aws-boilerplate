package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	identityUser = "123456789012\tarn:aws:iam::123456789012:user/bootstrap\n"
	backendOK    = "terraform {\n  backend \"s3\" {\n    key    = \"identity/terraform.tfstate\"\n    region = \"ap-northeast-1\"\n  }\n}\n"
	providersOK  = "provider \"aws\" {\n  region = \"ap-northeast-1\"\n}\n"
	commitSHA    = "0123456789abcdef0123456789abcdef01234567"
)

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errWrite }

var (
	errWrite = errors.New("write failed")
	errExec  = errors.New("git failed")
)

func rootWith(t *testing.T, backend, providers string) string {
	t.Helper()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "backend.tf"), []byte(backend), 0o600))

	if providers != "" {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "providers.tf"), []byte(providers), 0o600))
	}

	return dir
}

func stubGit(status, failOn string) func(args ...string) (string, error) {
	return stubGitWith(commitSHA+"\n", status, failOn)
}

func stubGitWith(revParse, status, failOn string) func(args ...string) (string, error) {
	return func(args ...string) (string, error) {
		if args[0] == failOn {
			return "", errExec
		}

		switch {
		case slices.Equal(args, []string{"rev-parse", "HEAD"}):
			return revParse, nil
		case slices.Equal(args, []string{"status", "--porcelain"}):
			return status, nil
		}

		return "", fmt.Errorf("unexpected git %v", args)
	}
}

func testDeps(environ []string) deps {
	return deps{readFile: os.ReadFile, environ: environ, git: stubGit("", ""), now: time.Now}
}

func Test_run(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("region は backend.tf の region を1行で出す", func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			require.NoError(t, run([]string{"region", rootWith(t, backendOK, providersOK)}, &out, testDeps(nil)))
			assert.Equal(t, "ap-northeast-1\n", out.String())
		})

		t.Run("bucket は資格情報から Bucket 名を1行で出す", func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			args := []string{"bucket", "-project", "tabp", "-env", "admin", "-region", "ap-northeast-1", "-identity", identityUser}
			require.NoError(t, run(args, &out, testDeps(nil)))
			assert.Equal(t, "tabp-admin-tfstate-123456789012-ap-northeast-1-an\n", out.String())
		})

		t.Run("env-unset は外す名前を1行で出す", func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			require.NoError(t, run([]string{"env-unset"}, &out, testDeps([]string{"AWS_PROFILE=x", "HOME=/h"})))
			assert.Equal(t, "-u AWS_PROFILE\n", out.String())
		})

		t.Run("state-key は鍵の ARN を渡す引数を1行で出す", func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			args := []string{"state-key", "-project", "tabp", "-env", "admin", "-region", "ap-northeast-1", "-identity", identityUser,
				"-aliases", withAlias, "-encryption", encAES256}
			require.NoError(t, run(args, &out, testDeps(nil)))
			assert.Equal(t, "-backend-config=kms_key_id="+stateKeyARN+"\n", out.String())
		})

		t.Run("state-key は -missing sse-s3 で鍵が無ければ空行を出す", func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			args := []string{"state-key", "-project", "tabp", "-env", "admin", "-region", "ap-northeast-1", "-identity", identityUser,
				"-aliases", withoutAlias, "-encryption", encAES256, "-missing", "sse-s3"}
			require.NoError(t, run(args, &out, testDeps(nil)))
			assert.Equal(t, "\n", out.String())
		})

		t.Run("env-unset は外す変数が無ければ空行を出す", func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			require.NoError(t, run([]string{"env-unset"}, &out, testDeps([]string{"HOME=/h"})))
			assert.Equal(t, "\n", out.String())
		})

		t.Run("plan-header は commit の行を1行で出す", func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			require.NoError(t, run([]string{"plan-header"}, &out, testDeps(nil)))
			assert.Equal(t, "commit: "+commitSHA+"\n", out.String())
		})

		t.Run("role-arn は role の ARN を1行で出す", func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			args := []string{"role-arn", "-project", "tabp", "-env", "admin", "-role", "plan", "-account", "123456789012"}
			require.NoError(t, run(args, &out, testDeps(nil)))
			assert.Equal(t, "arn:aws:iam::123456789012:role/tabp-admin-ci-plan\n", out.String())
		})

		t.Run("assume-denied は AccessDenied の拒否を1行で出す", func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			args := []string{"assume-denied", "-role", "apply", "-exitcode", "254", fileWith(t, deniedStderr)}
			require.NoError(t, run(args, &out, testDeps(nil)))
			assert.Equal(t, "✅ apply の role の assume は拒否されました（AccessDenied）\n", out.String())
		})

		t.Run("assume-denied は assume が通れば失敗する", func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			args := []string{"assume-denied", "-role", "apply", "-exitcode", "0", fileWith(t, "")}
			require.ErrorIs(t, run(args, &out, testDeps(nil)), errAssumeSucceeded)
			assert.Empty(t, out.String())
		})

		t.Run("oidc-sub は sub を1行で出す", func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			require.NoError(t, run([]string{"oidc-sub", fileWith(t, jwtWith(`{"sub":"`+immutable+`"}`))}, &out, testDeps(nil)))
			assert.Equal(t, immutable+"\n", out.String())
		})

		t.Run("plan-summary は要約を1行で出す", func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			path := fileWith(t, planWith(change("a.b", `"create"`), ""))
			require.NoError(t, run([]string{"plan-summary", "-exitcode", "2", path}, &out, testDeps(nil)))
			assert.Equal(t, "Plan: 0 to import, 1 to add, 0 to change, 0 to destroy.\n", out.String())
		})

		t.Run("policy-result は評価の件数を1行で出す", func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			path := fileWith(t, `[{"namespace":"plan.p","successes":2}]`)
			require.NoError(t, run([]string{"policy-result", "-namespace", "plan.p", "-rego", regoWith(t, 2), path}, &out, testDeps(nil)))
			assert.Equal(t, "✅ plan.p: 規則 2 件を評価し、違反はありません\n", out.String())
		})

		t.Run("policy-result -policy は名前空間ごとの件数を1行ずつ出す", func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			dir := policyDirWith(t, map[string]string{"p.rego": "package plan.p\n\ndeny contains msg if {\n\tfalse\n\tmsg := \"x\"\n}\n"})
			path := fileWith(t, `[{"namespace":"plan.p","successes":1}]`)
			require.NoError(t, run([]string{"policy-result", "-policy", dir, path}, &out, testDeps(nil)))
			assert.Equal(t, "✅ plan.p: 規則 1 件を評価し、違反はありません\n", out.String())
		})

		t.Run("account は資格情報からアカウント ID を1行で出す", func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			require.NoError(t, run([]string{"account", "-identity", identityUser}, &out, testDeps(nil)))
			assert.Equal(t, "123456789012\n", out.String())
		})

		t.Run("sha256 はファイルの SHA-256 を base64 で1行で出す", func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			require.NoError(t, run([]string{"sha256", fileWith(t, "abc")}, &out, testDeps(nil)))
			assert.Equal(t, "ungWv48Bz+pBQUDeXa4iI7ADYaOWF3qctBD/YfIAFa0=\n", out.String())
		})

		t.Run("oidc-url は要求先を1行で出す", func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			environ := []string{"ACTIONS_ID_TOKEN_REQUEST_URL=https://t.actions.githubusercontent.com/x", "ACTIONS_ID_TOKEN_REQUEST_TOKEN=b"}
			require.NoError(t, run([]string{"oidc-url"}, &out, testDeps(environ)))
			assert.Equal(t, "https://t.actions.githubusercontent.com/x?audience=sts.amazonaws.com\n", out.String())
		})

		t.Run("oidc-token は token を1行で出す", func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			require.NoError(t, run([]string{"oidc-token", fileWith(t, `{"value":"a.b.c"}`)}, &out, testDeps(nil)))
			assert.Equal(t, "a.b.c\n", out.String())
		})

		t.Run("plan-key は key を1行で出す", func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			require.NoError(t, run([]string{"plan-key", "-pr", "1", "-run", "2", "-attempt", "3", "-root", "admin-identity"}, &out, testDeps(nil)))
			assert.Equal(t, "pr/1/2-3-admin-identity.txt\n", out.String())
		})

		t.Run("apply-key は key を1行で出す", func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			require.NoError(t, run([]string{"apply-key", "-run", "2", "-attempt", "3", "-root", "admin-identity"}, &out, testDeps(nil)))
			assert.Equal(t, "apply/2-3-admin-identity.txt\n", out.String())
		})

		t.Run("apply-result は成功の行を1行で出す", func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			require.NoError(t, run([]string{"apply-result", "-exitcode", "0"}, &out, testDeps(nil)))
			assert.Equal(t, "✅ apply が終わりました\n", out.String())
		})

		t.Run("failure-kind は全文を読んで段の種類を1行で出す", func(t *testing.T) {
			t.Parallel()

			log := filepath.Join(t.TempDir(), "out.txt")
			require.NoError(t, os.WriteFile(log, []byte("Error: Error acquiring the state lock\n"), 0o600))
			var out bytes.Buffer
			require.NoError(t, run([]string{"failure-kind", "-step", "apply", "-exitcode", "1", log}, &out, testDeps(nil)))
			assert.Equal(t, "lock\n", out.String())
		})

		t.Run("lock-key は lock file の key を1行で出す", func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			require.NoError(t, run([]string{"lock-key", rootWith(t, backendOK, providersOK)}, &out, testDeps(nil)))
			assert.Equal(t, "identity/terraform.tfstate.tflock\n", out.String())
		})

		t.Run("lock-etag は lock が無ければ空行を出す", func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			list := fileWith(t, `{"Prefix":"identity/terraform.tfstate.tflock","KeyCount":0,"IsTruncated":false}`)
			require.NoError(t, run([]string{"lock-etag", "-key", "identity/terraform.tfstate.tflock", list}, &out, testDeps(nil)))
			assert.Equal(t, "\n", out.String())
		})

		t.Run("lock-held は lock の説明と消せる時刻を出す", func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			meta := fileWith(t, `{"ETag":"\"e\"","LastModified":"2026-11-12T12:00:00+00:00","VersionId":"v"}`)
			body := fileWith(t, `{"ID":"c7f6a1b2-3d4e-4f50-8a9b-0c1d2e3f4a5b","Operation":"OperationTypePlan","Who":"runner@h"}`)
			d := testDeps(nil)
			d.now = func() time.Time { return time.Date(2026, 11, 12, 12, 1, 0, 0, time.UTC) }
			require.NoError(t, run([]string{"lock-held", "-etag", `"e"`, "-meta", meta, body}, &out, d))
			assert.Contains(t, out.String(), "Terraform Unlock が消せるのは 2026-11-12T12:35:00Z より後\n")
		})

		t.Run("lock-wait は plan の全文に lock を取った後の行があれば held を出す", func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			d := testDeps(nil)
			d.alive = func(int) bool { return true }
			d.sleep = func(time.Duration) {}
			require.NoError(t, run([]string{"lock-wait", "-pid", "1", fileWith(t, "a.b: Refreshing state... [id=x]\n")}, &out, d))
			assert.Equal(t, "held\n", out.String())
		})

		t.Run("lock-wait は行が出ずに plan が終われば空行を出す", func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			d := testDeps(nil)
			d.alive = func(int) bool { return false }
			d.sleep = func(time.Duration) {}
			require.NoError(t, run([]string{"lock-wait", "-pid", "1", fileWith(t, "Initializing...\n")}, &out, d))
			assert.Equal(t, "\n", out.String())
		})

		t.Run("lock-stale は消してよい lock の説明を1行で出す", func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			meta := fileWith(t, `{"ETag":"\"e\"","LastModified":"2000-01-01T00:00:00+00:00","VersionId":"v"}`)
			body := fileWith(t, `{"ID":"c7f6a1b2-3d4e-4f50-8a9b-0c1d2e3f4a5b","Operation":"OperationTypeApply","Who":"runner@h"}`)
			d := testDeps(nil)
			d.now = func() time.Time { return time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC) }
			require.NoError(t, run([]string{"lock-stale", "-etag", `"e"`, "-meta", meta, body}, &out, d))
			assert.True(t, strings.HasPrefix(out.String(), "lock: ID=c7f6a1b2-3d4e-4f50-8a9b-0c1d2e3f4a5b "), out.String())
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		cases := map[string][]string{
			"引数が無ければ失敗する":               nil,
			"知らないサブコマンドは失敗する":           {"apply"},
			"region に root が無ければ失敗する":   {"region"},
			"region に余分な引数があれば失敗する":     {"region", "a", "b"},
			"env-unset に引数があれば失敗する":     {"env-unset", "x"},
			"plan-header に引数があれば失敗する":   {"plan-header", "x"},
			"oidc-sub にファイルが無ければ失敗する":   {"oidc-sub"},
			"oidc-sub に余分な引数があれば失敗する":   {"oidc-sub", "a", "b"},
			"sha256 にファイルが無ければ失敗する":     {"sha256"},
			"oidc-url に引数があれば失敗する":      {"oidc-url", "x"},
			"oidc-token にファイルが無ければ失敗する": {"oidc-token"},
			"sha256 に余分な引数があれば失敗する":     {"sha256", "a", "b"},
			"lock-key に root が無ければ失敗する": {"lock-key"},
			"lock-key に余分な引数があれば失敗する":   {"lock-key", "a", "b"},
			"lock-etag にファイルが無ければ失敗する":  {"lock-etag", "-key", "k"},
			"lock-stale にファイルが無ければ失敗する": {"lock-stale", "-etag", "e", "-meta", "m"},
			"lock-held にファイルが無ければ失敗する":  {"lock-held", "-etag", "e", "-meta", "m"},
			"lock-wait に pid が無ければ失敗する": {"lock-wait", "x"},
		}
		for name, args := range cases {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				var out bytes.Buffer
				require.ErrorIs(t, run(args, &out, testDeps(nil)), errUsage)
				assert.Empty(t, out.String())
			})
		}

		t.Run("新しい lock は消してよいと出力しない", func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			meta := fileWith(t, `{"ETag":"\"e\"","LastModified":"2026-11-12T11:59:00+00:00","VersionId":"v"}`)
			body := fileWith(t, `{"ID":"c7f6a1b2-3d4e-4f50-8a9b-0c1d2e3f4a5b","Operation":"OperationTypeApply","Who":"runner@h"}`)
			d := testDeps(nil)
			d.now = func() time.Time { return time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC) }
			require.ErrorIs(t, run([]string{"lock-stale", "-etag", `"e"`, "-meta", meta, body}, &out, d), errLockFresh)
			assert.Empty(t, out.String())
		})

		t.Run("state の鍵を決められなければ何も出力しない", func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			args := []string{"state-key", "-project", "tabp", "-env", "admin", "-region", "ap-northeast-1", "-identity", identityUser,
				"-aliases", withoutAlias, "-encryption", encAES256}
			require.ErrorIs(t, run(args, &out, testDeps(nil)), errStateKeyMissing)
			assert.Empty(t, out.String())
		})

		t.Run("region を決められなければ何も出力しない", func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			root := rootWith(t, backendOK, "provider \"aws\" {\n  region = \"us-east-1\"\n}\n")
			require.ErrorIs(t, run([]string{"region", root}, &out, testDeps(nil)), errRegionMismatch)
			assert.Empty(t, out.String())
		})

		t.Run("Bucket 名を決められなければ何も出力しない", func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			args := []string{"bucket", "-project", "tabp", "-env", "admin", "-region", "ap-northeast-1", "-identity", ""}
			require.ErrorIs(t, run(args, &out, testDeps(nil)), errIdentity)
			assert.Empty(t, out.String())
		})

		t.Run("外す変数の名前が不正なら何も出力しない", func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			require.ErrorIs(t, run([]string{"env-unset"}, &out, testDeps([]string{"AWS_X Y=1"})), errEnvName)
			assert.Empty(t, out.String())
		})

		t.Run("commit を決められなければ何も出力しない", func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			d := testDeps(nil)
			d.git = stubGit("", "rev-parse")
			require.ErrorIs(t, run([]string{"plan-header"}, &out, d), errGit)
			assert.Empty(t, out.String())
		})

		t.Run("出力に失敗すれば失敗する", func(t *testing.T) {
			t.Parallel()

			require.ErrorIs(t, run([]string{"region", rootWith(t, backendOK, providersOK)}, failingWriter{}, testDeps(nil)), errWrite)
		})
	})
}

func Test_runBucket(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("フラグから Bucket 名を組む", func(t *testing.T) {
			t.Parallel()

			got, err := runBucket([]string{"-project", "tabp", "-env", "admin", "-region", "ap-northeast-1", "-identity", identityUser})
			require.NoError(t, err)
			assert.Equal(t, "tabp-admin-tfstate-123456789012-ap-northeast-1-an", got)
		})

		t.Run("region が違えば名前の region も変わる", func(t *testing.T) {
			t.Parallel()

			got, err := runBucket([]string{"-project", "tabp", "-env", "dev", "-region", "us-east-1", "-identity", identityUser})
			require.NoError(t, err)
			assert.Equal(t, "tabp-dev-tfstate-123456789012-us-east-1-an", got)
		})

		for kind, want := range map[string]string{
			"tfstate": "tabp-security-tfstate-123456789012-ap-northeast-1-an",
			"tfplan":  "tabp-security-tfplan-123456789012-ap-northeast-1-an",
			"tfapply": "tabp-security-tfapply-123456789012-ap-northeast-1-an",
		} {
			t.Run("-kind "+kind+" の Bucket 名を env に従って組む", func(t *testing.T) {
				t.Parallel()

				got, err := runBucket([]string{"-project", "tabp", "-env", "security", "-kind", kind, "-region", "ap-northeast-1", "-identity", identityUser})
				require.NoError(t, err)
				assert.Equal(t, want, got)
			})
		}
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for name, args := range map[string][]string{
			"知らないフラグは失敗する": {"-target", "x"},
			"余分な引数は失敗する":   {"-project", "tabp", "-env", "admin", "-region", "ap-northeast-1", "-identity", identityUser, "x"},
			"値の無いフラグは失敗する": {"-project"},
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				_, err := runBucket(args)
				require.ErrorIs(t, err, errUsage)
			})
		}

		for name, kind := range map[string]string{
			"知らない種類は失敗する": "tflog",
			"種類が空なら失敗する":  "",
			"種類の大文字は受けない": "TFSTATE",
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				_, err := runBucket([]string{"-project", "tabp", "-env", "admin", "-kind", kind, "-region", "ap-northeast-1", "-identity", identityUser})
				require.ErrorIs(t, err, errBucketKind)
			})
		}

		t.Run("失敗の文に資格情報の値を入れない", func(t *testing.T) {
			t.Parallel()

			_, err := runBucket([]string{"-project", "tabp", "-env", "admin", "-region", "ap-northeast-1", "-identity", "123456789012\tarn:aws:iam::123456789012:user/b\textra\n"})
			require.ErrorIs(t, err, errIdentity)
			assert.NotContains(t, err.Error(), "123456789012")
		})

		t.Run("root の資格情報は Bucket 名を組まない", func(t *testing.T) {
			t.Parallel()

			_, err := runBucket([]string{"-project", "tabp", "-env", "admin", "-region", "ap-northeast-1", "-identity", "123456789012\tarn:aws:iam::123456789012:root\n"})
			require.ErrorIs(t, err, errRootCredentials)
			assert.NotContains(t, err.Error(), "123456789012")
		})

		t.Run("-identity を欠くと Bucket 名を組まない", func(t *testing.T) {
			t.Parallel()

			_, err := runBucket([]string{"-project", "tabp", "-env", "admin", "-region", "ap-northeast-1"})
			require.ErrorIs(t, err, errIdentity)
		})

		t.Run("-region を欠くと Bucket 名を組まない", func(t *testing.T) {
			t.Parallel()

			_, err := runBucket([]string{"-project", "tabp", "-env", "admin", "-identity", identityUser})
			require.ErrorIs(t, err, errRegionMalformed)
		})

		t.Run("-project を欠くと Bucket 名を組まない", func(t *testing.T) {
			t.Parallel()

			_, err := runBucket([]string{"-env", "admin", "-region", "ap-northeast-1", "-identity", identityUser})
			require.ErrorIs(t, err, errToken)
		})
	})
}

func Test_accountOf(t *testing.T) {
	t.Parallel()

	t.Run("IAM user の identity からアカウント ID を返す", func(t *testing.T) {
		t.Parallel()

		got, err := accountOf(identityUser)
		require.NoError(t, err)
		assert.Equal(t, "123456789012", got)
	})

	t.Run("root の資格情報は値を落として errRootCredentials を返す", func(t *testing.T) {
		t.Parallel()

		_, err := accountOf("123456789012\tarn:aws:iam::123456789012:root\n")
		require.ErrorIs(t, err, errRootCredentials)
		assert.NotContains(t, err.Error(), "123456789012")
	})

	t.Run("読めない identity は値を落として errIdentity を返す", func(t *testing.T) {
		t.Parallel()

		_, err := accountOf("123456789012\tarn:aws:iam::123456789012:user/b\textra\n")
		require.ErrorIs(t, err, errIdentity)
		assert.NotContains(t, err.Error(), "123456789012")
	})
}

func Test_rootRegion(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("root の backend.tf を読む", func(t *testing.T) {
			t.Parallel()

			got, err := rootRegion(rootWith(t, backendOK, providersOK), os.ReadFile)
			require.NoError(t, err)
			assert.Equal(t, "ap-northeast-1", got)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("backend.tf が無ければ失敗する", func(t *testing.T) {
			t.Parallel()

			_, err := rootRegion(t.TempDir(), os.ReadFile)
			require.ErrorIs(t, err, errBackendMissing)
		})

		t.Run("backend.tf を読めなければ失敗する", func(t *testing.T) {
			t.Parallel()

			deny := func(string) ([]byte, error) { return nil, fs.ErrPermission }
			_, err := rootRegion(t.TempDir(), deny)
			require.ErrorIs(t, err, errBackendMissing)
		})

		t.Run("backend の region が無ければ provider が正しくても backend.tf を名指しして失敗する", func(t *testing.T) {
			t.Parallel()

			root := rootWith(t, "terraform {\n  backend \"s3\" {}\n}\n", providersOK)
			_, err := rootRegion(root, os.ReadFile)
			require.ErrorIs(t, err, errRegionNotOne)
			assert.Contains(t, err.Error(), filepath.Join(root, "backend.tf"))
		})

		t.Run("backend の region の形が誤っていれば失敗する", func(t *testing.T) {
			t.Parallel()

			_, err := rootRegion(rootWith(t, "region = \"Tokyo\"\n", providersOK), os.ReadFile)
			require.ErrorIs(t, err, errRegionMalformed)
		})

		t.Run("provider の region の形が誤っていれば providers.tf を名指しして失敗する", func(t *testing.T) {
			t.Parallel()

			root := rootWith(t, backendOK, "provider \"aws\" {\n  region = \"Tokyo\"\n}\n")
			_, err := rootRegion(root, os.ReadFile)
			require.ErrorIs(t, err, errRegionMalformed)
			assert.Contains(t, err.Error(), filepath.Join(root, "providers.tf"))
		})

		t.Run("providers.tf が無ければ失敗する", func(t *testing.T) {
			t.Parallel()

			_, err := rootRegion(rootWith(t, backendOK, ""), os.ReadFile)
			require.ErrorIs(t, err, errProvidersMissing)
		})

		t.Run("provider と backend の region が違えば失敗する", func(t *testing.T) {
			t.Parallel()

			_, err := rootRegion(rootWith(t, backendOK, "provider \"aws\" {\n  region = \"us-east-1\"\n}\n"), os.ReadFile)
			require.ErrorIs(t, err, errRegionMismatch)
		})

		t.Run("provider の region を1つに決められなければ失敗する", func(t *testing.T) {
			t.Parallel()

			_, err := rootRegion(rootWith(t, backendOK, "provider \"aws\" {}\n"), os.ReadFile)
			require.ErrorIs(t, err, errRegionNotOne)
		})
	})
}

func Test_backendRegion(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		for _, region := range []string{"ap-northeast-1", "us-gov-west-1", "cn-north-1"} {
			t.Run(region+" を受け入れる", func(t *testing.T) {
				t.Parallel()

				got, err := backendRegion("    region = \"" + region + "\"\n")
				require.NoError(t, err)
				assert.Equal(t, region, got)
			})
		}
	})

	t.Run("コメントの region は数えない", func(t *testing.T) {
		t.Parallel()

		got, err := backendRegion("# region = \"us-east-1\"\n  region = \"ap-northeast-1\"\n")
		require.NoError(t, err)
		assert.Equal(t, "ap-northeast-1", got)
	})

	t.Run("空白の無い書き方も読む", func(t *testing.T) {
		t.Parallel()

		got, err := backendRegion("\tregion=\"ap-northeast-1\"\n")
		require.NoError(t, err)
		assert.Equal(t, "ap-northeast-1", got)
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("空なら失敗する", func(t *testing.T) {
			t.Parallel()

			_, err := backendRegion("")
			require.ErrorIs(t, err, errRegionNotOne)
		})

		t.Run("コメントの region しか無ければ失敗する", func(t *testing.T) {
			t.Parallel()

			_, err := backendRegion("# region = \"ap-northeast-1\"\n")
			require.ErrorIs(t, err, errRegionNotOne)
		})

		t.Run("region が変数の参照なら失敗する", func(t *testing.T) {
			t.Parallel()

			_, err := backendRegion("region = var.region\n")
			require.ErrorIs(t, err, errRegionNotOne)
		})

		t.Run("region が無ければ失敗する", func(t *testing.T) {
			t.Parallel()

			_, err := backendRegion("terraform {\n  backend \"s3\" {}\n}\n")
			require.ErrorIs(t, err, errRegionNotOne)
		})

		t.Run("region が2つあればどちらも選ばない", func(t *testing.T) {
			t.Parallel()

			_, err := backendRegion("region = \"ap-northeast-1\"\nregion = \"us-east-1\"\n")
			require.ErrorIs(t, err, errRegionNotOne)
		})

		for _, region := range []string{"Tokyo", "ap-northeast", "ap-northeast-1; rm -rf /", ""} {
			t.Run("形の誤った region "+region+" を拒む", func(t *testing.T) {
				t.Parallel()

				_, err := backendRegion("region = \"" + region + "\"\n")
				require.ErrorIs(t, err, errRegionMalformed)
			})
		}
	})
}

func Test_unset(t *testing.T) {
	t.Parallel()

	cases := map[string]bool{
		"AWS_PROFILE": true, "TF_VAR_x": true, "ACTIONS_ID_TOKEN_REQUEST_URL": true, "ACTIONS_ID_TOKEN_REQUEST_TOKEN": true,
		"HOME": false, "TF_PLUGIN_CACHE_DIR": false, "ACTIONS_ID_TOKEN_REQUEST_URLX": false,
	}
	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, want, unset(name))
		})
	}
}

func Test_runAccount(t *testing.T) {
	t.Parallel()

	t.Run("資格情報からアカウント ID を取り出す", func(t *testing.T) {
		t.Parallel()

		got, err := runAccount([]string{"-identity", identityUser})
		require.NoError(t, err)
		assert.Equal(t, "123456789012", got)
	})

	t.Run("root の資格情報は拒む", func(t *testing.T) {
		t.Parallel()

		_, err := runAccount([]string{"-identity", "123456789012\tarn:aws:iam::123456789012:root\n"})
		require.ErrorIs(t, err, errRootCredentials)
	})

	t.Run("資格情報が無ければ失敗する", func(t *testing.T) {
		t.Parallel()

		_, err := runAccount(nil)
		require.ErrorIs(t, err, errIdentity)
	})

	leaks := map[string]struct {
		identity string
		want     error
	}{
		"読めない identity の失敗の文に値を入れない": {"123456789012\tarn:aws:iam::123456789012:user/b\textra\n", errIdentity},
		"root の資格情報の失敗の文に値を入れない":     {"123456789012\tarn:aws:iam::123456789012:root\n", errRootCredentials},
	}
	for name, c := range leaks {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := runAccount([]string{"-identity", c.identity})
			require.ErrorIs(t, err, c.want)
			assert.NotContains(t, err.Error(), "123456789012")
		})
	}

	usage := map[string][]string{
		"余分な引数があれば失敗する": {"-identity", identityUser, "x"},
		"知らないフラグは失敗する":  {"-x", "y"},
	}
	for name, args := range usage {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := runAccount(args)
			require.ErrorIs(t, err, errUsage)
		})
	}
}

func Test_callerAccount(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("IAM user の資格情報からアカウント ID を取り出す", func(t *testing.T) {
			t.Parallel()

			got, err := callerAccount(identityUser)
			require.NoError(t, err)
			assert.Equal(t, "123456789012", got)
		})

		t.Run("名前が root の IAM user は拒まない", func(t *testing.T) {
			t.Parallel()

			got, err := callerAccount("123456789012\tarn:aws:iam::123456789012:user/root\n")
			require.NoError(t, err)
			assert.Equal(t, "123456789012", got)
		})

		t.Run("assumed role の資格情報は受け入れる", func(t *testing.T) {
			t.Parallel()

			got, err := callerAccount("123456789012\tarn:aws:sts::123456789012:assumed-role/r/s\n")
			require.NoError(t, err)
			assert.Equal(t, "123456789012", got)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		cases := map[string]string{
			"空白だけなら失敗する":        "  \n\t\n",
			"空なら失敗する":           "",
			"11桁なら失敗する":         "12345678901\tarn:aws:iam::12345678901:user/b\n",
			"13桁なら失敗する":         "1234567890123\tarn:aws:iam::1234567890123:user/b\n",
			"Arn が無ければ失敗する":     "123456789012\n",
			"フィールドが多ければ失敗する":    "123456789012\tarn:aws:iam::123456789012:user/b\textra\n",
			"数字でなければ失敗する":       "None\tNone\n",
			"12文字でも数字でなければ失敗する": "12345678901a\tarn:aws:iam::12345678901a:user/b\n",
			"2行分の出力なら失敗する":      identityUser + identityUser,
		}
		for name, identity := range cases {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				_, err := callerAccount(identity)
				require.ErrorIs(t, err, errIdentity)
			})
		}

		for name, identity := range map[string]string{
			"root の資格情報は拒む":        "123456789012\tarn:aws:iam::123456789012:root\n",
			"CRLF の root の資格情報も拒む": "123456789012\tarn:aws:iam::123456789012:root\r\n",
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				_, err := callerAccount(identity)
				require.ErrorIs(t, err, errRootCredentials)
			})
		}
	})
}

func Test_bucketName(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("modules/s3 と usecases/state-backend の規則で組む", func(t *testing.T) {
			t.Parallel()

			got, err := bucketName("tabp", "admin", "tfstate", "123456789012", "ap-northeast-1")
			require.NoError(t, err)
			assert.Equal(t, "tabp-admin-tfstate-123456789012-ap-northeast-1-an", got)
		})

	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		_, err := bucketName("tabp", "admin", "tflog", "123456789012", "ap-northeast-1")
		require.ErrorIs(t, err, errBucketKind)
	})

	t.Run("写しと正本のずれ", func(t *testing.T) {
		t.Parallel()

		t.Run("env/admin/identity のテストが期待する名前と一致する", func(t *testing.T) {
			t.Parallel()

			got, err := bucketName("tabp", "admin", "tfstate", "123456789012", "ap-northeast-1")
			require.NoError(t, err)

			tftest, err := os.ReadFile(filepath.Join("..", "..", "env", "admin", "identity", "tests", "imports.tftest.hcl"))
			require.NoError(t, err)
			assert.Contains(t, string(tftest), `== "`+got+`"`)
		})

		t.Run("Bucket の種類はどれも usecases/state-backend の名前の規則に在る", func(t *testing.T) {
			t.Parallel()

			src, err := os.ReadFile(filepath.Join("..", "..", "usecases", "state-backend", "main.tf"))
			require.NoError(t, err)
			require.NotEmpty(t, bucketKinds)
			for _, kind := range bucketKinds {
				assert.Contains(t, string(src), "${var.project}-${var.env}-"+kind, kind)
			}
		})

		t.Run("docs/setup/bootstrap.md の make を使わない手順が同じ変数を外す", func(t *testing.T) {
			t.Parallel()

			doc, err := os.ReadFile(filepath.Join("..", "..", "docs", "setup", "bootstrap.md"))
			require.NoError(t, err)
			want := "grep -oE '^((" + strings.Join(unsetPrefixes, "|") + ")[A-Za-z0-9_]*|" + strings.Join(unsetNames, "|") + ")='"
			assert.Contains(t, string(doc), want)
		})

		t.Run("docs/setup/bootstrap.md の make を使わない terraform の実行がすべて CLI の設定を読ませない", func(t *testing.T) {
			t.Parallel()

			doc, err := os.ReadFile(filepath.Join("..", "..", "docs", "setup", "bootstrap.md"))
			require.NoError(t, err)
			all := strings.Count(string(doc), "env $unset_env AWS_PROFILE=tabp-bootstrap")
			require.Positive(t, all)
			assert.Equal(t, all, strings.Count(string(doc), "env $unset_env AWS_PROFILE=tabp-bootstrap TF_CLI_CONFIG_FILE=/dev/null "))
		})

		t.Run("docs/setup/bootstrap.md の段1 の組み立てと一致する", func(t *testing.T) {
			t.Parallel()

			doc, err := os.ReadFile(filepath.Join("..", "..", "docs", "setup", "bootstrap.md"))
			require.NoError(t, err)
			assert.Contains(t, string(doc), `BUCKET="`+fmt.Sprintf(bucketFormat, "tabp", "admin", "tfstate", "${ACCOUNT_ID}", "${REGION}")+`"`)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for name, args := range map[string][2]string{
			"project が空なら失敗する":      {"", "admin"},
			"env が空なら失敗する":          {"tabp", ""},
			"project にハイフンがあれば失敗する": {"ta-bp", "admin"},
			"env に大文字があれば失敗する":      {"tabp", "Admin"},
			"env にハイフンがあれば失敗する":     {"tabp", "ad-min"},
			"env に改行があれば失敗する":       {"tabp", "admin\n"},
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				_, err := bucketName(args[0], args[1], "tfstate", "123456789012", "ap-northeast-1")
				require.ErrorIs(t, err, errToken)
			})
		}

		for _, region := range []string{"Tokyo", "ap-northeast-1\n"} {
			t.Run("region の形が誤っていれば失敗する "+region, func(t *testing.T) {
				t.Parallel()

				_, err := bucketName("tabp", "admin", "tfstate", "123456789012", region)
				require.ErrorIs(t, err, errRegionMalformed)
			})
		}
	})
}

func Test_envUnset(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("資格情報と振る舞いを変える変数を外し、ほかは残す", func(t *testing.T) {
			t.Parallel()

			got, err := envUnset([]string{
				"HOME=/h", "PATH=/bin", "TF_PLUGIN_CACHE_DIR=/c", "TF_IN_AUTOMATION=1",
				"AWS_PROFILE=other", "AWS_ACCESS_KEY_ID=x", "AWS_CONTAINER_CREDENTIALS_FULL_URI=http://127.0.0.1/",
				"TF_CLI_ARGS=-x", "TF_CLI_ARGS_plan=-target=a.b", "TF_VAR_a=b",
				"TF_LOG=trace", "TF_LOG_PATH=/tmp/x", "TF_LOG_PROVIDER=trace",
				"TF_WORKSPACE=w", "TF_DATA_DIR=/d", "TF_CLI_CONFIG_FILE=/f", "TF_REATTACH_PROVIDERS=x",
				"ACTIONS_ID_TOKEN_REQUEST_URL=https://x", "ACTIONS_ID_TOKEN_REQUEST_TOKEN=t",
			})
			require.NoError(t, err)
			assert.Equal(t, "-u ACTIONS_ID_TOKEN_REQUEST_TOKEN -u ACTIONS_ID_TOKEN_REQUEST_URL -u AWS_ACCESS_KEY_ID -u AWS_CONTAINER_CREDENTIALS_FULL_URI -u AWS_PROFILE"+
				" -u TF_CLI_ARGS -u TF_CLI_ARGS_plan -u TF_CLI_CONFIG_FILE -u TF_DATA_DIR"+
				" -u TF_LOG -u TF_LOG_PATH -u TF_LOG_PROVIDER -u TF_REATTACH_PROVIDERS -u TF_VAR_a -u TF_WORKSPACE", got)
		})

		for name, environ := range map[string][]string{
			"外す変数が無ければ空にする":     {"HOME=/h", "TF_PLUGIN_CACHE_DIR=/c"},
			"環境が空なら空にする":        nil,
			"名前が前方一致するだけの変数は残す": {"MY_AWS_PROFILE=x", "TF_WORKSPACE_X=y", "TF_DATA_DIRS=z", "ACTIONS_ID_TOKEN_REQUEST_URLX=w"},
			"名前が空のエントリは外さない":    {"=x"},
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				got, err := envUnset(environ)
				require.NoError(t, err)
				assert.Empty(t, got)
			})
		}

		t.Run("= の無いエントリは全体を名前として扱う", func(t *testing.T) {
			t.Parallel()

			got, err := envUnset([]string{"AWS_PROFILE", "HOME"})
			require.NoError(t, err)
			assert.Equal(t, "-u AWS_PROFILE", got)
		})

		t.Run("接頭辞そのものの名前も外す", func(t *testing.T) {
			t.Parallel()

			got, err := envUnset([]string{"AWS_=x", "TF_VAR_=y"})
			require.NoError(t, err)
			assert.Equal(t, "-u AWS_ -u TF_VAR_", got)
		})

		t.Run("同じ名前が2度あっても1度だけ外す", func(t *testing.T) {
			t.Parallel()

			got, err := envUnset([]string{"AWS_PROFILE=a", "AWS_PROFILE=b"})
			require.NoError(t, err)
			assert.Equal(t, "-u AWS_PROFILE", got)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for _, kv := range []string{"AWS_X Y=1", "TF_VAR_*=1", "TF_LOG;rm=1"} {
			t.Run("外す変数の名前が不正なら失敗する "+kv, func(t *testing.T) {
				t.Parallel()

				_, err := envUnset([]string{kv})
				require.ErrorIs(t, err, errEnvName)
			})
		}
	})
}

func Test_planHeader(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("作業ツリーが clean なら commit だけを書く", func(t *testing.T) {
			t.Parallel()

			got, err := planHeader(stubGit("", ""))
			require.NoError(t, err)
			assert.Equal(t, "commit: "+commitSHA, got)
		})

		t.Run("未コミットの変更があれば注記する", func(t *testing.T) {
			t.Parallel()

			got, err := planHeader(stubGit(" M x\n", ""))
			require.NoError(t, err)
			assert.Equal(t, "commit: "+commitSHA+" （作業ツリーに未コミットの変更あり）", got)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for name, revParse := range map[string]string{
			"rev-parse が空なら失敗する":           "",
			"rev-parse が空白だけなら失敗する":        " \n",
			"rev-parse が commit でなければ失敗する": "HEAD\n",
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				_, err := planHeader(stubGitWith(revParse, "", ""))
				require.ErrorIs(t, err, errGit)
			})
		}

		for _, sub := range []string{"rev-parse", "status"} {
			t.Run(sub+" が失敗すれば失敗する", func(t *testing.T) {
				t.Parallel()

				_, err := planHeader(stubGit("", sub))
				require.ErrorIs(t, err, errGit)
			})
		}
	})
}

func Test_gitOutput(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("git の標準出力を返す", func(t *testing.T) {
			t.Parallel()

			got, err := gitOutput("--version")
			require.NoError(t, err)
			assert.True(t, strings.HasPrefix(got, "git version "), got)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("git が失敗すれば失敗する", func(t *testing.T) {
			t.Parallel()

			_, err := gitOutput("no-such-subcommand")

			var exitErr *exec.ExitError
			require.ErrorAs(t, err, &exitErr)
			assert.Contains(t, err.Error(), "git no-such-subcommand")
		})
	})
}

// TestLocalMk は、local.mk のレシピが tf-local の値を aws と terraform へ渡す配線を、make -n の
// 展開結果と、偽物の道具で init を走らせた結果で見る。
func TestLocalMk(t *testing.T) {
	t.Parallel()

	expand := func(t *testing.T, target string, vars ...string) string {
		t.Helper()

		cmd := exec.CommandContext(t.Context(), "make", append([]string{"-C", filepath.Join("..", ".."), "-n", "--no-print-directory", target}, vars...)...)
		cmd.Env = slices.DeleteFunc(os.Environ(), func(kv string) bool {
			return strings.HasPrefix(kv, "MAKEFLAGS=") || strings.HasPrefix(kv, "MFLAGS=") || strings.HasPrefix(kv, "MAKELEVEL=") ||
				strings.HasPrefix(kv, "TF_LOCAL_MISSING_STATE_KEY=")
		})

		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))

		return string(out)
	}

	const envPrefix = "env $unset_env AWS_PROFILE=tabp-bootstrap TF_CLI_CONFIG_FILE=/dev/null AWS_EC2_METADATA_DISABLED=true CHECKPOINT_DISABLE=1"

	t.Run("init は失敗で止まり、aws と terraform の両方に絞った環境を渡す", func(t *testing.T) {
		t.Parallel()

		got := expand(t, "init-admin-identity")
		assert.True(t, strings.HasPrefix(strings.TrimSpace(got), "set -eu;"), got)
		assert.Equal(t, 4, strings.Count(got, envPrefix), got)
		assert.Contains(t, got, envPrefix+" aws sts get-caller-identity --region \"$region\" --query '[Account,Arn]' --output text")
		assert.Contains(t, got, `init -input=false -lockfile=readonly -backend-config="bucket=$bucket" $kms`)
		assert.Contains(t, got, "bucket -project tabp -env admin")
		assert.Contains(t, got, `account -identity "$identity"`)
	})

	t.Run("init は state の鍵を alias と Bucket の既定の暗号化から求め、既定では鍵が無ければ失敗させる", func(t *testing.T) {
		t.Parallel()

		got := expand(t, "init-admin-identity")
		assert.Contains(t, got, envPrefix+` aws kms list-aliases --region "$region" --output json`)
		assert.Contains(t, got, envPrefix+` aws s3api get-bucket-encryption --region "$region" --bucket "$bucket" --expected-bucket-owner "$account" --output json`)
		assert.Contains(t, got, `state-key -project tabp -env admin -region "$region" -identity "$identity" -aliases "$aliases" -encryption "$encryption" -missing fail'`)
		assert.Less(t, strings.Index(got, "state-key"), strings.Index(got, " init -input=false"), got)
	})

	t.Run("鍵を渡さない init は TF_LOCAL_MISSING_STATE_KEY で人が選ぶ", func(t *testing.T) {
		t.Parallel()

		got := expand(t, "init-admin-identity", "TF_LOCAL_MISSING_STATE_KEY=sse-s3")
		assert.Contains(t, got, `-missing sse-s3'`)
	})

	run := func(t *testing.T, knobs map[string]string, vars ...string) (string, string, error) {
		t.Helper()

		root := t.TempDir()
		bin := filepath.Join(root, "tf-local")
		build := exec.CommandContext(t.Context(), "go", "build", "-o", bin, ".")
		out, err := build.CombinedOutput()
		require.NoError(t, err, string(out))

		k := filepath.Join(root, "knobs")
		identity := filepath.Join(root, "base", "env", "admin", "identity")
		for _, d := range []string{k, identity, filepath.Join(root, "bin")} {
			require.NoError(t, os.MkdirAll(d, 0o700))
		}

		require.NoError(t, os.WriteFile(filepath.Join(identity, "backend.tf"), []byte(backendOK), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(identity, "providers.tf"), []byte(providersOK), 0o600))

		for name, v := range knobs {
			require.NoError(t, os.WriteFile(filepath.Join(k, name), []byte(v), 0o600))
		}

		writeFakes(t, filepath.Join(root, "bin"), k, "terraform", "aws")

		args := append([]string{"-C", filepath.Join("..", ".."), "-s", "--no-print-directory", "init-admin-identity", "RUNNER_MODE=host",
			"TF_LOCAL=" + tfLocalShell(bin), "TF_BASE=" + filepath.Join(root, "base"),
			"TF_BIN=" + filepath.Join(root, "bin", "terraform"), "AWS_BIN=" + filepath.Join(root, "bin", "aws")}, vars...)
		cmd := exec.CommandContext(t.Context(), "make", args...)
		cmd.Env = slices.DeleteFunc(os.Environ(), func(kv string) bool {
			return strings.HasPrefix(kv, "MAKEFLAGS=") || strings.HasPrefix(kv, "MFLAGS=") || strings.HasPrefix(kv, "MAKELEVEL=") ||
				strings.HasPrefix(kv, "TF_LOCAL_MISSING_STATE_KEY=")
		})

		combined, err := cmd.CombinedOutput()
		calls, _ := os.ReadFile(filepath.Join(k, "calls"))

		return string(calls), string(combined), err
	}

	initLine := func(calls string) string {
		for line := range strings.Lines(calls) {
			if strings.HasPrefix(line, "terraform -chdir=") && strings.Contains(line, " init ") {
				return line
			}
		}

		return ""
	}

	t.Run("鍵が在れば init に鍵の ARN を渡す", func(t *testing.T) {
		t.Parallel()

		calls, out, err := run(t, nil)
		require.NoError(t, err, out)
		assert.Contains(t, initLine(calls), "-backend-config=kms_key_id=arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab")
		assert.NotContains(t, out, "SSE-S3 で書きます")
	})

	t.Run("鍵が無く sse-s3 を選べば、kms_key_id を渡さずに init し、そう告げる", func(t *testing.T) {
		t.Parallel()

		calls, out, err := run(t, map[string]string{"aliases": `{"Aliases":[]}`}, "TF_LOCAL_MISSING_STATE_KEY=sse-s3")
		require.NoError(t, err, out)
		line := initLine(calls)
		require.NotEmpty(t, line, calls)
		assert.NotContains(t, line, "-backend-config=kms_key_id=")
		assert.Contains(t, out, "init に鍵を渡さず SSE-S3 で書きます")
	})

	for name, knobs := range map[string]map[string]string{
		"alias の一覧を取れなければ init を呼ばない":      {"kms-rc": "1"},
		"Bucket の既定の暗号化を取れなければ init を呼ばない": {"encryption-rc": "1"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			calls, out, err := run(t, knobs, "TF_LOCAL_MISSING_STATE_KEY=sse-s3")
			require.Error(t, err, out)
			assert.Contains(t, calls, "aws kms list-aliases", "判定の経路を通っていません")
			assert.Empty(t, initLine(calls), calls)
		})
	}

	t.Run("鍵が無ければ既定では init を呼ばない", func(t *testing.T) {
		t.Parallel()

		calls, out, err := run(t, map[string]string{"aliases": `{"Aliases":[]}`})
		require.Error(t, err, out)
		assert.Contains(t, out, "state の鍵の alias がありません")
		assert.Empty(t, initLine(calls), calls)
	})

	t.Run("plan は init の後に lock を取らずに走り、log を所有者だけの権限で作り直す", func(t *testing.T) {
		t.Parallel()

		got := expand(t, "plan-admin-identity")
		initAt := strings.Index(got, "-lockfile=readonly")
		planAt := strings.Index(got, "plan -lock=false -input=false -no-color")
		require.GreaterOrEqual(t, initAt, 0, got)
		require.Greater(t, planAt, initAt, got)
		assert.Contains(t, got, "set -eu -o pipefail; umask 077;")
		assert.Contains(t, got, `rm -f "$log"`)
		assert.Contains(t, got, `| tee "$log"`)
		assert.Equal(t, 5, strings.Count(got, envPrefix), got)
	})

	t.Run("aws-login は絞った環境で bootstrap の profile にログインする", func(t *testing.T) {
		t.Parallel()

		got := expand(t, "aws-login")
		assert.Contains(t, got, "env $unset_env aws login --profile tabp-bootstrap")
	})
}

// TestCiMk は、ci.mk のレシピが CI の profile で plan・apply・lock を残す入口・lock の解除を行い、terraform の出力を公開される
// ログに出さない配線を、make -n の展開結果で見る。
func TestCiMk(t *testing.T) {
	t.Parallel()

	expand := func(t *testing.T, target string, vars ...string) string {
		t.Helper()

		cmd := exec.CommandContext(t.Context(), "make", append([]string{"-C", filepath.Join("..", ".."), "-n", "--no-print-directory", target}, vars...)...)
		cmd.Env = slices.DeleteFunc(os.Environ(), func(kv string) bool {
			return strings.HasPrefix(kv, "MAKEFLAGS=") || strings.HasPrefix(kv, "MFLAGS=") || strings.HasPrefix(kv, "MAKELEVEL=")
		})

		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))

		return string(out)
	}

	envPrefix := func(role string) string {
		return "env $unset_env AWS_PROFILE=tabp-admin-ci-" + role + " TF_CLI_CONFIG_FILE=/dev/null AWS_EC2_METADATA_DISABLED=true CHECKPOINT_DISABLE=1"
	}

	order := func(t *testing.T, got string, want ...string) {
		t.Helper()

		at := 0
		for _, w := range want {
			i := strings.Index(got[at:], w)
			require.GreaterOrEqual(t, i, 0, "%q が %q より後にありません: %s", w, got[:at], got)
			at += i + len(w)
		}
	}

	t.Run("plan は CI の profile だけで lock を取らずに走り、全文を置いてから判定する", func(t *testing.T) {
		t.Parallel()

		got := expand(t, "ci-plan-admin-identity")
		assert.True(t, strings.HasPrefix(strings.TrimSpace(got), "set -eu -o pipefail; umask 077;"), got)
		assert.NotContains(t, got, "tabp-bootstrap")
		assert.Contains(t, got, `trap 'rm -f "$out" "$json" "$tfplan" "$pol"' EXIT`)
		order(t, got,
			`account -identity "$identity"`,
			`state-key -project tabp -env admin -region "$region" -identity "$identity" -aliases "$aliases" -encryption "$encryption" -missing fail'`,
			envPrefix("plan")+` terraform -chdir="$dir" init -input=false -no-color -lockfile=readonly -backend-config="bucket=$bucket" $kms >> "$out" 2>&1`,
			`bucket -project tabp -env admin -kind tfplan -region "$region" -identity "$identity"`,
			`plan -lock=false -input=false -no-color -detailed-exitcode -out="$tfplan" >> "$out" 2>&1 || rc=$?`,
			`show -json "$tfplan" > "$json" 2>> "$out" || :`,
			`sha256 "$out"`,
			envPrefix("plan")+` aws s3api put-object`,
			`--checksum-sha256 "$sha" --expected-bucket-owner "$account"`,
			`plan-summary -exitcode "$rc" "$json"`,
			`for ns in plan.stateful_protection plan.kms_key_policy plan.resource_policy_exclusive; do export ns`,
			`conftest test --combine --no-color -o json --namespace "$ns" --policy conftest/plan --data conftest/plan/decl.json "$json" > "$pol"`,
			`policy-result -namespace "$ns" -rego "conftest/plan/${ns#plan.}.rego" "$pol"`,
			`done`,
		)
		assert.NotContains(t, got, " apply ")
		assert.NotContains(t, got, "output -raw", "Bucket の名前は state から読まない")
		assert.Equal(t, 1, strings.Count(got, "| tee"), "tee は commit の行だけに使う: "+got)
	})

	t.Run("ci.mk のすべての plan の入口は lock を取らず、apply の入口は lock を取る", func(t *testing.T) {
		t.Parallel()

		raw, err := os.ReadFile(filepath.Join("..", "..", ".makefiles", "terraform", "operation", "ci.mk"))
		require.NoError(t, err)
		phony := map[string]string{}
		for _, m := range regexp.MustCompile(`(?m)^\.PHONY: (ci-(plan|apply|hold)-[a-z0-9-]+)`).FindAllStringSubmatch(string(raw), -1) {
			phony[m[1]] = m[2]
		}
		var rules []string
		for _, m := range regexp.MustCompile(`(?m)^(ci-(?:plan|apply|hold)-[A-Za-z0-9_-]+):\s*$`).FindAllStringSubmatch(string(raw), -1) {
			rules = append(rules, m[1])
		}
		require.ElementsMatch(t, slices.Collect(maps.Keys(phony)), rules, "入口の .PHONY とレシピの行が食い違います")
		require.Contains(t, slices.Collect(maps.Values(phony)), "plan", "ci.mk に plan の入口がありません")
		require.Contains(t, slices.Collect(maps.Values(phony)), "apply", "ci.mk に apply の入口がありません")
		require.Contains(t, slices.Collect(maps.Values(phony)), "hold", "ci.mk に lock を残す入口がありません")
		for target, kind := range phony {
			t.Run("入口 "+target, func(t *testing.T) {
				t.Parallel()

				got := expand(t, target)
				if kind == "plan" {
					assert.Contains(t, got, "plan -lock=false -input=false")

					return
				}
				assert.NotContains(t, got, "-lock=false")
				assert.Contains(t, got, "plan -input=false")
			})
		}
	})

	t.Run("Policy の名前空間が空なら展開で落ちる", func(t *testing.T) {
		t.Parallel()

		for _, target := range []string{"ci-plan-admin-identity", "ci-apply-admin-identity", "ci-plan-admin-workload", "ci-apply-admin-workload"} {
			for _, value := range []string{"", " "} {
				t.Run(target+" "+strconv.Quote(value), func(t *testing.T) {
					t.Parallel()

					cmd := exec.CommandContext(t.Context(), "make", "-C", filepath.Join("..", ".."), "-n", "--no-print-directory", target, "CI_POLICY_NAMESPACES="+value)
					cmd.Env = slices.DeleteFunc(os.Environ(), func(kv string) bool {
						return strings.HasPrefix(kv, "MAKEFLAGS=") || strings.HasPrefix(kv, "MFLAGS=") || strings.HasPrefix(kv, "MAKELEVEL=")
					})
					out, err := cmd.CombinedOutput()
					require.Error(t, err)
					assert.Contains(t, string(out), "CI_POLICY_NAMESPACES が空です")
				})
			}
		}
	})

	t.Run("CI の init は鍵を渡さない選択を受けない", func(t *testing.T) {
		t.Parallel()

		for _, target := range []string{"ci-plan-admin-identity", "ci-apply-admin-identity", "ci-plan-admin-workload", "ci-apply-admin-workload"} {
			got := expand(t, target, "TF_LOCAL_MISSING_STATE_KEY=sse-s3")
			assert.Contains(t, got, `-missing fail'`, target)
			assert.NotContains(t, got, "sse-s3", target)
		}
	})

	t.Run("apply は apply-identity の profile で lock を取って plan し直し、Policy を通ってから apply する", func(t *testing.T) {
		t.Parallel()

		got := expand(t, "ci-apply-admin-identity")
		assert.NotContains(t, got, "tabp-bootstrap")
		assert.NotContains(t, got, "-lock=false")
		assert.Contains(t, got, `trap 'rc=$?; rm -f "$out" "$json" "$tfplan" "$pol"; test "$rc" = 0 || test -z "${GITHUB_OUTPUT:-}" || printf "failure=%s\n" "$stage" >> "$GITHUB_OUTPUT"' EXIT`,
			"失敗して終わるときに段を書く")
		order(t, got,
			`stage=prepare`,
			envPrefix("apply-identity")+` terraform -chdir="$dir" init -input=false -no-color -lockfile=readonly`,
			`bucket -project tabp -env admin -kind tfplan -region "$region" -identity "$identity"`,
			`apply-key -run "$GITHUB_RUN_ID" -attempt "$GITHUB_RUN_ATTEMPT" -root "$root"`,
			envPrefix("apply-identity")+` aws s3api put-object --region "$region" --bucket "$plan_bucket" --key "$key" --body "$out"`,
			`--checksum-sha256 "$sha" --expected-bucket-owner "$account"`,
			`stage=plan`,
			`plan -input=false -no-color -detailed-exitcode -out="$tfplan" >> "$out" 2>&1 || rc=$?`,
			`show -json "$tfplan" > "$json" 2>> "$out" || :`,
			`failure-kind -step plan -exitcode "$rc" "$out"`,
			`plan-summary -exitcode "$rc" "$json"`,
			`stage=policy`,
			`for ns in plan.stateful_protection plan.kms_key_policy plan.resource_policy_exclusive; do export ns`,
			`policy-result -namespace "$ns" -rego "conftest/plan/${ns#plan.}.rego" "$pol"`,
			`done`,
			`stage=apply`,
			`timeout -s INT 20m `+envPrefix("apply-identity")+` terraform -chdir="$dir" apply -input=false -no-color "$tfplan" >> "$out" 2>&1 || arc=$?; export arc`,
			`failure-kind -step apply -exitcode "$arc"`,
			`upload`,
			`apply-result -exitcode "$arc"`,
		)
		assert.Equal(t, 1, strings.Count(got, "put-object"), "全文を置くのは upload の1か所だけ: "+got)
		assert.NotContains(t, got, "output -raw", "Bucket の名前は state から読まない")
		m := regexp.MustCompile(`timeout -s INT ([0-9]+)m `).FindStringSubmatch(got)
		require.Len(t, m, 2, got)
		limit, err := strconv.Atoi(m[1])
		require.NoError(t, err)
		assert.Less(t, time.Duration(limit)*time.Minute, applyJobTimeout, "job の timeout より先に打ち切らなければ全文を置けない")
		assert.Equal(t, 1, strings.Count(got, " apply -input=false"), got)
		assert.Equal(t, 1, strings.Count(got, "| tee"), "tee は commit の行だけに使う: "+got)
	})

	t.Run("unlock は unlock-identity の profile で、terraform を呼ばずに、読んだ版の ETag を条件に消す", func(t *testing.T) {
		t.Parallel()

		got := expand(t, "ci-unlock-admin-identity")
		assert.True(t, strings.HasPrefix(strings.TrimSpace(got), "set -eu -o pipefail; umask 077;"), got)
		assert.NotContains(t, got, "tabp-bootstrap")
		assert.NotContains(t, got, " terraform ")
		assert.NotContains(t, got, "AWS_PROFILE=tabp-admin-ci-plan")
		assert.NotContains(t, got, "AWS_PROFILE=tabp-admin-ci-apply-identity")
		assert.Contains(t, got, `trap 'rm -f "$list" "$body" "$meta"' EXIT`)
		order(t, got,
			`account -identity "$identity"`,
			`lock-key "$dir"'`,
			envPrefix("unlock-identity")+` aws s3api list-objects-v2 --region "$region" --bucket "$bucket" --prefix "$key" --expected-bucket-owner "$account" --no-paginate --output json > "$list"`,
			`lock-etag -key "$key" "$list"'`,
			envPrefix("unlock-identity")+` aws s3api get-object --region "$region" --bucket "$bucket" --key "$key" --if-match "$etag" --expected-bucket-owner "$account" --output json "$body" > "$meta"`,
			`lock-stale -etag "$etag" -meta "$meta" "$body"'`,
			envPrefix("unlock-identity")+` aws s3api delete-object --region "$region" --bucket "$bucket" --key "$key" --if-match "$etag" --expected-bucket-owner "$account"`,
		)
		assert.Equal(t, 1, strings.Count(got, "delete-object"), got)
		assert.NotContains(t, got, "--version-id")
	})

	t.Run("lock を残す入口は apply-identity の profile で lock を取る plan を裏で起動し、apply も書き込みもしない", func(t *testing.T) {
		t.Parallel()

		got := expand(t, "ci-hold-admin-identity")
		assert.NotContains(t, got, "AWS_PROFILE=tabp-admin-ci-plan")
		assert.NotContains(t, got, "AWS_PROFILE=tabp-admin-ci-unlock-identity")
		assert.NotContains(t, got, "-lock=false")
		assert.NotContains(t, got, "-out=")
		assert.NotContains(t, got, " apply ")
		assert.NotContains(t, got, "put-object")
		assert.NotContains(t, got, "delete-object")
		assert.Contains(t, got, `trap 'rc=$?; test -z "$pid" || kill -KILL "$pid" 2>/dev/null || :;`)
		order(t, got,
			envPrefix("apply-identity")+` terraform -chdir="$dir" init -input=false`,
			`lock-key "$dir"'`,
			envPrefix("apply-identity")+` aws s3api list-objects-v2 --region "$region" --bucket "$bucket" --prefix "$key" --expected-bucket-owner "$account" --no-paginate --output json > "$list"`,
			`lock-etag -key "$key" -want absent "$list"'`,
			envPrefix("apply-identity")+` terraform -chdir="$dir" plan -input=false -no-color -parallelism=1 > "$hold" 2>&1 & pid=$!`,
			`lock-wait -pid "$pid" -interval 200ms -limit 10m "$hold"'`,
			`kill -KILL "$pid"`,
			`test -n "$held" ||`,
			envPrefix("apply-identity")+` aws s3api list-objects-v2`,
			`lock-etag -key "$key" -want present "$list"'`,
			envPrefix("apply-identity")+` aws s3api get-object --region "$region" --bucket "$bucket" --key "$key" --if-match "$etag" --expected-bucket-owner "$account" --output json "$body" > "$meta"`,
			`lock-held -etag "$etag" -meta "$meta" "$body"'`,
		)
		assert.Equal(t, 1, strings.Count(got, " plan -input=false"), got)
	})

	t.Run("workload root の入口は役割の profile で workload root を扱い、apply の全文を apply の出力の Bucket に置く", func(t *testing.T) {
		t.Parallel()

		cases := map[string]struct {
			role    string
			want    []string
			notWant []string
		}{
			"ci-plan-admin-workload":   {"plan", []string{`for ns in plan.stateful_protection; do export ns`, `bucket -project tabp -env admin -kind tfplan`}, nil},
			"ci-apply-admin-workload":  {"apply", []string{`for ns in plan.stateful_protection; do export ns`, `bucket -project tabp -env admin -kind tfapply`, `timeout -s INT 20m `}, []string{"-kind tfplan", "-lock=false"}},
			"ci-unlock-admin-workload": {"unlock", []string{`lock-key "$dir"'`, `--if-match "$etag"`}, []string{" terraform ", "--version-id"}},
			"ci-hold-admin-workload":   {"apply", []string{`lock-wait -pid "$pid"`, `-parallelism=1 > "$hold"`}, []string{" apply ", "-lock=false", "-out=", "put-object", "delete-object"}},
		}
		for target, c := range cases {
			t.Run(target, func(t *testing.T) {
				t.Parallel()

				got := expand(t, target)
				assert.Contains(t, got, `dir="./env/admin"; root="admin-workload"`)
				assert.Contains(t, got, "AWS_PROFILE=tabp-admin-ci-"+c.role+" ")
				for _, other := range []string{"plan", "apply", "apply-identity", "unlock", "unlock-identity"} {
					if other != c.role {
						assert.NotContains(t, got, "AWS_PROFILE=tabp-admin-ci-"+other+" ", target)
					}
				}
				for _, w := range c.want {
					assert.Contains(t, got, w)
				}
				for _, w := range c.notWant {
					assert.NotContains(t, got, w)
				}
				for _, once := range []string{" apply -input=false", "put-object", "delete-object"} {
					assert.LessOrEqual(t, strings.Count(got, once), 1, once)
				}
			})
		}
	})

	t.Run("apply の全文の置き場は、identity root なら plan の出力の Bucket、ほかの root なら apply の出力の Bucket", func(t *testing.T) {
		t.Parallel()

		extra := filepath.Join(t.TempDir(), "kind.mk")
		require.NoError(t, os.WriteFile(extra, []byte("ci-apply-output-kind-%: ; @echo $(call CI_APPLY_OUTPUT_KIND,$*)\n"), 0o600))
		for root, want := range map[string]string{"identity": "tfplan", "workload": "tfapply"} {
			got := expand(t, "ci-apply-output-kind-"+root, "-f", "Makefile", "-f", extra)
			assert.Equal(t, "echo "+want, strings.TrimSpace(got), root)
		}
	})

	t.Run("plan と apply は env と root から出力の Bucket を組み、workload root の apply は apply の出力の Bucket に置く", func(t *testing.T) {
		t.Parallel()

		extra := filepath.Join(t.TempDir(), "roots.mk")
		require.NoError(t, os.WriteFile(extra, []byte("ci-x-plan: SHELL := /bin/bash\nci-x-plan: CI_POLICY_NAMESPACES := plan.x\nci-x-plan:\n\t@$(call CI_PLAN,security,identity)\nci-x-apply: SHELL := /bin/bash\nci-x-apply: CI_POLICY_NAMESPACES := plan.x\nci-x-apply:\n\t@$(call CI_APPLY,security,workload)\n"), 0o600))
		plan := expand(t, "ci-x-plan", "-f", "Makefile", "-f", extra)
		assert.Contains(t, plan, `bucket -project tabp -env security -kind tfplan -region "$region" -identity "$identity"`)
		apply := expand(t, "ci-x-apply", "-f", "Makefile", "-f", extra)
		assert.Contains(t, apply, `bucket -project tabp -env security -kind tfapply -region "$region" -identity "$identity"`)
	})

	t.Run("apply の全文の置き場は、identity でも workload でもない root では展開で落ちる", func(t *testing.T) {
		t.Parallel()

		extra := filepath.Join(t.TempDir(), "kind.mk")
		require.NoError(t, os.WriteFile(extra, []byte("ci-apply-output-kind-%: ; @echo $(call CI_APPLY_OUTPUT_KIND,$*)\n"), 0o600))
		cmd := exec.CommandContext(t.Context(), "make", "-C", filepath.Join("..", ".."), "-n", "--no-print-directory", "-f", "Makefile", "-f", extra, "ci-apply-output-kind-security-identity")
		cmd.Env = slices.DeleteFunc(os.Environ(), func(kv string) bool {
			return strings.HasPrefix(kv, "MAKEFLAGS=") || strings.HasPrefix(kv, "MFLAGS=") || strings.HasPrefix(kv, "MAKELEVEL=")
		})
		out, err := cmd.CombinedOutput()
		require.Error(t, err)
		assert.Contains(t, string(out), "root が identity か workload ではありません: security-identity")
	})

	t.Run("CI_ENV を渡さなければ資格情報と拒否の確認の入口は展開で落ちる", func(t *testing.T) {
		t.Parallel()

		for _, target := range []string{"ci-aws-credentials", "ci-assume-denied"} {
			cmd := exec.CommandContext(t.Context(), "make", "-C", filepath.Join("..", ".."), "-n", "--no-print-directory", target)
			cmd.Env = slices.DeleteFunc(os.Environ(), func(kv string) bool {
				return strings.HasPrefix(kv, "MAKEFLAGS=") || strings.HasPrefix(kv, "MFLAGS=") || strings.HasPrefix(kv, "MAKELEVEL=") || strings.HasPrefix(kv, "CI_ENV=")
			})
			out, err := cmd.CombinedOutput()
			require.Error(t, err, target)
			assert.Contains(t, string(out), "CI_ENV が空です", target)
		}
	})

	t.Run("資格情報は OIDC の token で1回だけ assume し、token を消す", func(t *testing.T) {
		t.Parallel()

		got := expand(t, "ci-aws-credentials", "CI_ENV=admin")
		assert.Equal(t, 1, strings.Count(got, "assume-role-with-web-identity"), got)
		order(t, got, `oidc-url`, `-o "$resp" "$url"`, `oidc-token "$resp"' > "$token"`, `oidc-sub "$token"`, "assume-role-with-web-identity")
		assert.Contains(t, got, `trap 'rm -f "$token" "$resp"' EXIT`)
		assert.NotContains(t, got, "jq ")
		assert.Contains(t, got, "role-arn -project tabp -env admin -role plan")
		assert.Contains(t, got, `do echo "::add-mask::$v"; done`)
		assert.Contains(t, got, "configure set --profile tabp-admin-ci-plan")
		assert.NotContains(t, got, "AWS_ACCESS_KEY_ID")
	})

	t.Run("拒否の確認は token を取ってから役割ごとに assume を試み、応答を捨てて判定する", func(t *testing.T) {
		t.Parallel()

		got := expand(t, "ci-assume-denied", "CI_ENV=admin")
		assert.True(t, strings.HasPrefix(strings.TrimSpace(got), "set -eu -o pipefail; umask 077;"), got)
		assert.Contains(t, got, `trap 'rm -f "$token" "$resp" "$d/assume-stderr"' EXIT`)
		order(t, got, `oidc-url`, `-o "$resp" "$url"`, `oidc-token "$resp"' > "$token"`, `oidc-sub "$token"`,
			`for r in apply apply-identity unlock-identity unlock; do export r`,
			`role-arn -project tabp -env admin -role "$r" -account "$CI_ACCOUNT_ID"`,
			`--role-session-name "gha-denied-$GITHUB_RUN_ID-$GITHUB_RUN_ATTEMPT"`,
			`--output json > /dev/null 2> "$err" || rc=$?`,
			`assume-denied -role "$r" -exitcode "$rc" "$err"`,
			`done`,
		)
		assert.Equal(t, 1, strings.Count(got, "assume-role-with-web-identity"), got)
		for _, banned := range []string{"configure", "--query", "add-mask", "AWS_PROFILE"} {
			assert.NotContains(t, got, banned)
		}
	})

	t.Run("既定の拒否の役割は plan のほかの役割すべてである", func(t *testing.T) {
		t.Parallel()

		got := expand(t, "ci-assume-denied", "CI_ENV=admin")
		m := regexp.MustCompile(`for r in ([a-z -]+); do export r`).FindStringSubmatch(got)
		require.Len(t, m, 2, got)
		want := slices.DeleteFunc(slices.Clone(roleNames), func(r string) bool { return r == "plan" })
		assert.ElementsMatch(t, want, strings.Fields(m[1]))
	})

	t.Run("拒否の役割が空なら展開で落ちる", func(t *testing.T) {
		t.Parallel()

		for _, value := range []string{"", " "} {
			t.Run(strconv.Quote(value), func(t *testing.T) {
				t.Parallel()

				cmd := exec.CommandContext(t.Context(), "make", "-C", filepath.Join("..", ".."), "-n", "--no-print-directory", "ci-assume-denied", "CI_ENV=admin", "CI_DENIED_ROLES="+value)
				cmd.Env = slices.DeleteFunc(os.Environ(), func(kv string) bool {
					return strings.HasPrefix(kv, "MAKEFLAGS=") || strings.HasPrefix(kv, "MFLAGS=") || strings.HasPrefix(kv, "MAKELEVEL=")
				})
				out, err := cmd.CombinedOutput()
				require.Error(t, err)
				assert.Contains(t, string(out), "CI_DENIED_ROLES が空です")
			})
		}
	})
}

// ciFakes の plan・apply・assume-role は secret の文字列を出し、それが公開されるログへ漏れないことを見られるようにする。
const ciFakes = `#!/bin/sh
k=%[1]q
rc() { cat "$k/$1" 2>/dev/null || echo 0; }
echo "$(basename "$0") $*" >> "$k/calls"
case "$(basename "$0")" in
aws)
	case "$1 $2" in
	"sts get-caller-identity") cat "$k/identity" 2>/dev/null || printf '123456789012\tarn:aws:iam::123456789012:user/ci\n'; exit "$(rc sts-rc)" ;;
	"sts assume-role-with-web-identity")
		for a; do test "$prev" = --role-arn && r=${a##*-ci-}; prev=$a; done
		printf 'KEY-SECRET\tSECRET-SECRET\tSESSION-SECRET\n'
		cat "$k/assume-stderr-$r" >&2 2>/dev/null || cat "$k/assume-stderr" >&2 2>/dev/null
		test -f "$k/assume-rc-$r" && exit "$(cat "$k/assume-rc-$r")"
		exit "$(rc assume-rc)" ;;
	"kms list-aliases") cat "$k/aliases" 2>/dev/null || echo '{"Aliases":[{"AliasName":"alias/tabp-admin-tfstate","AliasArn":"arn:aws:kms:ap-northeast-1:123456789012:alias/tabp-admin-tfstate","TargetKeyId":"1234abcd-12ab-34cd-56ef-1234567890ab"}]}'; exit "$(rc kms-rc)" ;;
	"s3api get-bucket-encryption") cat "$k/encryption" 2>/dev/null || echo '{"ServerSideEncryptionConfiguration":{"Rules":[{"ApplyServerSideEncryptionByDefault":{"SSEAlgorithm":"AES256"}}]}}'; exit "$(rc encryption-rc)" ;;
	"s3api list-objects-v2")
		n=$(grep -c '^aws s3api list-objects-v2' "$k/calls")
		test -f "$k/list-rc-$n" && exit "$(cat "$k/list-rc-$n")"
		test -f "$k/lock-list-$n" && { cat "$k/lock-list-$n"; exit "$(rc list-rc)"; }
		cat "$k/lock-list" 2>/dev/null || echo '{"Name":"b","Prefix":"identity/terraform.tfstate.tflock","KeyCount":0,"MaxKeys":1000,"IsTruncated":false}'; exit "$(rc list-rc)" ;;
	"s3api get-object")
		for a; do last=$a; done
		cat "$k/lock-body" > "$last"; cat "$k/lock-meta"; exit "$(rc get-rc)" ;;
	"s3api delete-object") echo marker-1; exit "$(rc delete-rc)" ;;
	s3api*) echo v-1; exit "$(rc s3api-rc)" ;;
	esac
	exit 0 ;;
conftest)
	for a; do test "$prev" = --namespace && ns=$a; prev=$a; done
	if test -f "$k/policy-$ns.json"; then cat "$k/policy-$ns.json"; else cat "$k/policy.json"; fi
	exit 1 ;;
timeout)
	shift 3
	"$@"
	c=$?
	test -f "$k/timeout-rc" && exit "$(cat "$k/timeout-rc")"
	exit "$c" ;;
curl)
	for a; do test "$prev" = -o && out=$a; prev=$a; done
	cat "$k/oidc-response" > "$out"
	exit "$(rc curl-rc)" ;;
git)
	case "$1" in
	rev-parse) echo 0123456789abcdef0123456789abcdef01234567 ;;
	esac
	exit 0 ;;
esac
for a; do case "$a" in -chdir=*) ;; *) sub=$a; break ;; esac; done
for a; do last=$a; done
case "$sub" in
init)
	for a; do case "$a" in init|-*) ;; *) echo "terraform: positional argument: '$a'" >&2; exit 64 ;; esac; done
	exit "$(rc init-rc)" ;;
output) echo "terraform output を呼びました" >&2; exit 1 ;;
plan)
	for a; do case "$a" in -out=*) echo plan > "${a#-out=}" ;; esac; done
	test -f "$k/plan-hold" && echo $$ > "$k/plan-pid"
	echo PLAN-SECRET
	cat "$k/plan-out" 2>/dev/null
	test -f "$k/plan-hold" && exec sleep 60
	exit "$(rc plan-rc)" ;;
show) test -s "$last" || exit 1; cat "$k/plan.json" ;;
apply) echo APPLY-SECRET; cat "$k/apply-out" 2>/dev/null; exit "$(rc apply-rc)" ;;
esac
`

func writeFakes(t *testing.T, dir, knobs string, tools ...string) {
	t.Helper()

	for _, tool := range tools {
		require.NoError(t, os.WriteFile(filepath.Join(dir, tool), fmt.Appendf(nil, ciFakes, knobs), 0o700)) //nolint:gosec // 偽物の道具は実行できなければならない
	}
}

// tfLocalShell は、make の TF_LOCAL を差し替える値を返す。実物（RUN_SCRIPT_HOST）と同じく sh -c の中で
// 起動するので、レシピが export しない値は tf-local に届かない。
func tfLocalShell(bin string) string {
	return "sh -c 'set -eu; exec " + bin + " $(1)'"
}

const (
	recipeNoop     = `{"format_version":"1.2","resource_changes":[{"address":"a.b","change":{"actions":["no-op"]}}]}`
	recipeCreate   = `{"format_version":"1.2","resource_changes":[{"address":"a.b","change":{"actions":["create"]}}]}`
	recipePass     = `[{"namespace":"plan.stateful_protection","successes":2}]`
	recipeKMSPass  = `[{"namespace":"plan.kms_key_policy","successes":3}]`
	recipeRPEPass  = `[{"namespace":"plan.resource_policy_exclusive","successes":4}]`
	recipeViolated = `[{"namespace":"plan.stateful_protection","successes":1,"failures":[{"msg":"[PLAN-STATE-01] stub"}]}]`
)

type recipeResult struct {
	out, calls, summary, output, plans string
	err                                error
	left                               []string
}

const applyCall = "terraform -chdir="

func countCalls(calls, prefix string) int {
	n := 0

	for line := range strings.Lines(calls) {
		if strings.HasPrefix(line, prefix) {
			n++
		}
	}

	return n
}

func initArgs(calls string) string {
	for line := range strings.Lines(calls) {
		if strings.HasPrefix(line, applyCall) && strings.Contains(line, " init ") {
			return line
		}
	}

	return ""
}

func applies(calls string) int {
	n := 0

	for line := range strings.Lines(calls) {
		if strings.HasPrefix(line, applyCall) && strings.Contains(line, " apply ") {
			n++
		}
	}

	return n
}

type recipeFunc func(t *testing.T, target string, knobs map[string]string, env ...string) recipeResult

func newRecipe(bin string) recipeFunc {
	// env は既定の環境を上書きする。git と curl は PATH の先頭の偽物が受ける。
	return func(t *testing.T, target string, knobs map[string]string, env ...string) recipeResult {
		t.Helper()

		root := t.TempDir()
		k := filepath.Join(root, "knobs")
		identity := filepath.Join(root, "base", "env", "admin", "identity")
		policy := filepath.Join(root, "policy")
		plans := filepath.Join(root, "plans")
		summary := filepath.Join(root, "summary.md")
		output := filepath.Join(root, "output")

		for _, d := range []string{k, identity, policy, filepath.Join(root, "bin")} {
			require.NoError(t, os.MkdirAll(d, 0o700))
		}

		backend := backendOK
		if v, ok := knobs["backend.tf"]; ok {
			backend = v
		}

		require.NoError(t, os.WriteFile(filepath.Join(identity, "backend.tf"), []byte(backend), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(identity, "providers.tf"), []byte(providersOK), 0o600))
		// workload root は identity root の親のディレクトリに在る（ADR-0604 決定5）。
		workload := filepath.Dir(identity)
		require.NoError(t, os.WriteFile(filepath.Join(workload, "backend.tf"), []byte(strings.Replace(backendOK, "identity/terraform.tfstate", "workload/terraform.tfstate", 1)), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(workload, "providers.tf"), []byte(providersOK), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(policy, "stateful_protection.rego"), []byte(strings.Repeat("deny contains msg if {\n}\n\n", 2)), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(policy, "kms_key_policy.rego"), []byte(strings.Repeat("deny contains msg if {\n}\n\n", 3)), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(policy, "resource_policy_exclusive.rego"), []byte(strings.Repeat("deny contains msg if {\n}\n\n", 4)), 0o600))

		if _, ok := knobs["policy-plan.kms_key_policy.json"]; !ok {
			require.NoError(t, os.WriteFile(filepath.Join(k, "policy-plan.kms_key_policy.json"), []byte(recipeKMSPass), 0o600))
		}

		if _, ok := knobs["policy-plan.resource_policy_exclusive.json"]; !ok {
			require.NoError(t, os.WriteFile(filepath.Join(k, "policy-plan.resource_policy_exclusive.json"), []byte(recipeRPEPass), 0o600))
		}

		for name, v := range knobs {
			require.NoError(t, os.WriteFile(filepath.Join(k, name), []byte(v), 0o600))
		}

		writeFakes(t, filepath.Join(root, "bin"), k, "terraform", "aws", "conftest", "curl", "git", "timeout")

		cmd := exec.CommandContext(t.Context(), "make", "-C", filepath.Join("..", ".."), "-s", "--no-print-directory", target, "RUNNER_MODE=host", "GO_TOOL=",
			"TF_LOCAL="+tfLocalShell(bin), "TF_BASE="+filepath.Join(root, "base"), "CI_PLAN_DIR="+plans,
			"TF_BIN="+filepath.Join(root, "bin", "terraform"), "AWS_BIN="+filepath.Join(root, "bin", "aws"), "CONFTEST_BIN="+filepath.Join(root, "bin", "conftest"),
			"TIMEOUT_BIN="+filepath.Join(root, "bin", "timeout"),
			"POLICY_PLAN_DIR="+policy, "POLICY_PLAN_DECL="+filepath.Join(policy, "decl.json"))
		cmd.Env = append(slices.DeleteFunc(os.Environ(), func(kv string) bool {
			return strings.HasPrefix(kv, "MAKEFLAGS=") || strings.HasPrefix(kv, "MFLAGS=") || strings.HasPrefix(kv, "MAKELEVEL=") ||
				strings.HasPrefix(kv, "GITHUB_") || strings.HasPrefix(kv, "CI_") || strings.HasPrefix(kv, "ACTIONS_ID_TOKEN_REQUEST_") || strings.HasPrefix(kv, "PATH=")
		}), "PATH="+filepath.Join(root, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"),
			"CI_PR=7", "GITHUB_RUN_ID=8", "GITHUB_RUN_ATTEMPT=1", "GITHUB_STEP_SUMMARY="+summary, "GITHUB_OUTPUT="+output)
		cmd.Env = append(cmd.Env, env...)

		combined, err := cmd.CombinedOutput()
		calls, _ := os.ReadFile(filepath.Join(k, "calls"))
		written, _ := os.ReadFile(summary)
		outputs, _ := os.ReadFile(output)
		entries, dirErr := os.ReadDir(plans)
		require.NoError(t, dirErr, "レシピが作業ディレクトリを作っていません: %s", combined)

		left := make([]string, 0, len(entries))
		for _, e := range entries {
			left = append(left, e.Name())
		}

		return recipeResult{out: string(combined), calls: string(calls), summary: string(written), output: string(outputs), plans: plans, err: err, left: left}
	}

}

// TestCiRecipes は、ci.mk のレシピを偽物の道具で実際に走らせ、判定の失敗が apply・lock の削除と後続を止めることと、
// plan と apply の全文を置いてから失敗することを見る。判定そのものは tf-local のテストが持ち、ここはレシピが
// その終了コードを握り潰さないことを固定する（ADR-0602 決定13、ADR-0604 決定11、ADR-0702 決定24）。
func TestCiRecipes(t *testing.T) {
	t.Parallel()

	bin := filepath.Join(t.TempDir(), "tf-local")
	build := exec.CommandContext(t.Context(), "go", "build", "-o", bin, ".")
	out, err := build.CombinedOutput()
	require.NoError(t, err, string(out))

	recipe := newRecipe(bin)

	t.Run("apply の入口", func(t *testing.T) {
		t.Parallel()
		testCiApplyEntry(t, recipe)
	})

	t.Run("plan の入口", func(t *testing.T) {
		t.Parallel()
		testCiPlanEntry(t, recipe)
	})

	t.Run("資格情報", func(t *testing.T) {
		t.Parallel()
		testCiCredentials(t, recipe)
	})

	t.Run("lock の解除の入口", func(t *testing.T) {
		t.Parallel()
		testCiUnlockEntry(t, recipe)
	})

	t.Run("lock を残す入口", func(t *testing.T) {
		t.Parallel()
		testCiHoldEntry(t, recipe)
	})

	t.Run("拒否の確認の入口", func(t *testing.T) {
		t.Parallel()
		testCiAssumeDenied(t, recipe)
	})
}

func testCiAssumeDenied(t *testing.T, recipe recipeFunc) {
	t.Helper()

	jwt := jwtWith(`{"sub":"repo:o/r:environment:admin-plan"}`)
	env := []string{
		"ACTIONS_ID_TOKEN_REQUEST_URL=https://t.actions.githubusercontent.com/x", "ACTIONS_ID_TOKEN_REQUEST_TOKEN=bearer",
		"CI_ACCOUNT_ID=123456789012", "CI_ENV=admin",
	}
	denied := func(extra map[string]string) map[string]string {
		knobs := map[string]string{"oidc-response": `{"value":"` + jwt + `"}`, "assume-rc": "254", "assume-stderr": deniedStderr}
		maps.Copy(knobs, extra)

		return knobs
	}
	const assume = "aws sts assume-role-with-web-identity"

	noSecrets := func(t *testing.T, r recipeResult) {
		t.Helper()

		for _, v := range []string{"KEY-SECRET", "SECRET-SECRET", "SESSION-SECRET", jwt, "123456789012"} {
			assert.NotContains(t, r.out, v)
		}
		assert.Zero(t, countCalls(r.calls, "aws configure"), r.calls)
		assert.Empty(t, r.left, "作業ファイルが残っています")
	}

	t.Run("既定の4つの役割がすべて拒否されれば成功し、資格情報をどこにも置かない", func(t *testing.T) {
		t.Parallel()

		r := recipe(t, "ci-assume-denied", denied(nil), env...)
		require.NoError(t, r.err, r.out)
		assert.Equal(t, 4, countCalls(r.calls, assume), r.calls)
		for _, role := range []string{"apply", "apply-identity", "unlock-identity", "unlock"} {
			assert.Contains(t, r.calls, "--role-arn arn:aws:iam::123456789012:role/tabp-admin-ci-"+role+" ")
			assert.Contains(t, r.out, "✅ "+role+" の role の assume は拒否されました（AccessDenied）")
		}
		assert.NotContains(t, r.calls, "tabp-admin-ci-plan ")
		assert.Contains(t, r.calls, "--role-session-name gha-denied-8-1 ")
		assert.Contains(t, r.out, "OIDC の sub: repo:o/r:environment:admin-plan")
		noSecrets(t, r)
	})

	t.Run("assume が通った役割で失敗し、後の役割を試さない", func(t *testing.T) {
		t.Parallel()

		r := recipe(t, "ci-assume-denied", denied(map[string]string{"assume-rc-apply-identity": "0", "assume-stderr-apply-identity": ""}), env...)
		require.Error(t, r.err, r.out)
		assert.Contains(t, r.out, "apply-identity の role")
		assert.Contains(t, r.out, "assume が通りました")
		assert.Equal(t, 2, countCalls(r.calls, assume), r.calls)
		assert.NotContains(t, r.calls, "tabp-admin-ci-unlock-identity")
		noSecrets(t, r)
	})

	t.Run("AccessDenied でない失敗は拒否と認めず、stderr をログへ出さない", func(t *testing.T) {
		t.Parallel()

		stderr := strings.Replace(invalidToken, "Incorrect token audience", "arn:aws:iam::123456789012:role/tabp-admin-ci-apply", 1)
		r := recipe(t, "ci-assume-denied", denied(map[string]string{"assume-stderr": stderr}), env...)
		assert.NotContains(t, r.out, "arn:aws")
		require.Error(t, r.err, r.out)
		assert.Contains(t, r.out, "AccessDenied ではない理由")
		assert.Equal(t, 1, countCalls(r.calls, assume), r.calls)
		noSecrets(t, r)
	})

	t.Run("STS に届かない失敗は拒否と認めない", func(t *testing.T) {
		t.Parallel()

		r := recipe(t, "ci-assume-denied", denied(map[string]string{"assume-stderr": "Could not connect to the endpoint URL\n"}), env...)
		require.Error(t, r.err, r.out)
		assert.Contains(t, r.out, "STS のエラーを1つに読めません")
		noSecrets(t, r)
	})

	t.Run("役割を指定すればその role だけを試す", func(t *testing.T) {
		t.Parallel()

		r := recipe(t, "ci-assume-denied", denied(nil), append(slices.Clone(env), "CI_DENIED_ROLES=unlock-identity")...)
		require.NoError(t, r.err, r.out)
		assert.Equal(t, 1, countCalls(r.calls, assume), r.calls)
		assert.Contains(t, r.calls, "tabp-admin-ci-unlock-identity ")
	})

	t.Run("知らない役割は assume せずに失敗する", func(t *testing.T) {
		t.Parallel()

		r := recipe(t, "ci-assume-denied", denied(nil), append(slices.Clone(env), "CI_DENIED_ROLES=admin")...)
		require.Error(t, r.err, r.out)
		assert.Contains(t, r.out, "role の ARN を組めません")
		assert.Zero(t, countCalls(r.calls, assume), r.calls)
		noSecrets(t, r)
	})

	t.Run("token を要求できなければ assume しない", func(t *testing.T) {
		t.Parallel()

		r := recipe(t, "ci-assume-denied", denied(nil), append(slices.Clone(env), "ACTIONS_ID_TOKEN_REQUEST_URL=")...)
		require.Error(t, r.err, r.out)
		assert.Contains(t, r.out, "id-token: write がありません")
		assert.Zero(t, countCalls(r.calls, assume), r.calls)
		noSecrets(t, r)
	})
}

func testCiApplyEntry(t *testing.T, recipe recipeFunc) {
	t.Helper()

	t.Run("workload root の apply は stateful_protection だけを掛け、全文を apply の出力の Bucket に置く", func(t *testing.T) {
		t.Parallel()

		r := recipe(t, "ci-apply-admin-workload", map[string]string{"plan-rc": "2", "plan.json": recipeCreate, "policy.json": recipePass})
		require.NoError(t, r.err, r.out)
		assert.Equal(t, 1, applies(r.calls), r.calls)
		assert.Equal(t, 1, countCalls(r.calls, "conftest test "), r.calls)
		assert.Contains(t, r.calls, "--namespace plan.stateful_protection")
		assert.Equal(t, 1, countCalls(r.calls, "aws s3api put-object"), r.calls)
		assert.Contains(t, r.calls, "--bucket tabp-admin-tfapply-123456789012-ap-northeast-1-an --key apply/8-1-admin-workload.txt")
		assert.NotContains(t, r.calls, "tabp-admin-tfplan-", "apply の role は plan の出力の Bucket に書かない")
	})

	t.Run("workload root の apply の失敗も全文を apply の出力の Bucket に置いてから失敗する", func(t *testing.T) {
		t.Parallel()

		r := recipe(t, "ci-apply-admin-workload", map[string]string{"plan-rc": "2", "plan.json": recipeCreate, "policy.json": recipePass, "apply-rc": "1"})
		require.Error(t, r.err, r.out)
		assert.Contains(t, r.calls, "--bucket tabp-admin-tfapply-123456789012-ap-northeast-1-an --key apply/8-1-admin-workload.txt")
		assert.Contains(t, r.output, "failure=apply")
	})

	// output は成功の通知が運ぶ件数の行。変更の有無を通知で見分けられるのは、この行だけである。
	ok := map[string]struct{ planRC, plan, output string }{
		"変更が無くても保存した plan を apply する": {"0", recipeNoop, "plan=No changes.\n"},
		"変更があれば保存した plan を apply する":  {"2", recipeCreate, "plan=Plan: 0 to import, 1 to add,"},
	}
	for name, c := range ok {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			r := recipe(t, "ci-apply-admin-identity", map[string]string{"plan-rc": c.planRC, "plan.json": c.plan, "policy.json": recipePass})
			require.NoError(t, r.err, r.out)
			assert.Equal(t, 1, applies(r.calls), r.calls)
			saved := filepath.Join(r.plans, "admin-identity.tfplan")
			assert.Contains(t, r.calls, "-out="+saved, "plan した先")
			assert.Contains(t, r.calls, "apply -input=false -no-color "+saved, "plan したファイルを apply する")
			assert.Contains(t, r.out, "✅ apply が終わりました")
			assert.Contains(t, r.summary, "apply が終わりました")
			assert.True(t, strings.HasPrefix(r.output, c.output), r.output)
			assert.Equal(t, 1, strings.Count(r.output, "\n"), "件数は1行で job の outputs に渡る: %q", r.output)
			assert.NotContains(t, r.output, "failure=", "成功した run は失敗の段を書かない")
			assert.Equal(t, 3, countCalls(r.calls, "conftest test "), r.calls)
			assert.Less(t, strings.Index(r.calls, "--namespace plan.stateful_protection"), strings.Index(r.calls, "--namespace plan.kms_key_policy"), r.calls)
			assert.Less(t, strings.Index(r.calls, "--namespace plan.kms_key_policy"), strings.Index(r.calls, "--namespace plan.resource_policy_exclusive"), r.calls)
			assert.Contains(t, r.out, "plan.kms_key_policy: 規則 3 件")
			assert.Contains(t, r.out, "plan.resource_policy_exclusive: 規則 4 件")
			assert.Equal(t, 1, countCalls(r.calls, "aws s3api put-object"), r.calls)
			assert.Contains(t, r.calls, "--bucket tabp-admin-tfplan-123456789012-ap-northeast-1-an --key apply/8-1-admin-identity.txt")
			assert.Contains(t, r.calls, "--expected-bucket-owner 123456789012")
			assert.Equal(t, 1, countCalls(r.calls, "timeout -s INT 20m "), "apply を timeout で包む")
			assert.Less(t, strings.Index(r.calls, " apply -input=false"), strings.Index(r.calls, "aws s3api put-object"), "apply の後に全文を置く")
			assert.Contains(t, r.out, "apply の全文の版 ID: v-1")
			assert.Contains(t, r.out, "apply の終了コード: 0")
			assert.Contains(t, r.out, "apply の全文: key=apply/8-1-admin-identity.txt sha256=")
			assert.Contains(t, r.summary, "- key: `apply/8-1-admin-identity.txt`")
			assert.Regexp(t, "- sha256: `[A-Za-z0-9+/]{43}=`", r.summary)
			assert.Contains(t, r.summary, "- 版 ID: `v-1`")
			assert.NotContains(t, r.out, "APPLY-SECRET")
			assert.Contains(t, initArgs(r.calls), "-backend-config=kms_key_id=arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab", "init に state の鍵を渡す")
			assert.NotContains(t, r.calls, "-lock=false")
			assert.Empty(t, r.left, "作業ファイルが残っています")
		})
	}

	fail := map[string]struct {
		knobs map[string]string
		want  string
		kind  string
	}{
		"plan が lock の取得で落ちれば、lock として apply しない":            {map[string]string{"plan-rc": "1", "plan.json": recipeNoop, "policy.json": recipePass, "plan-out": "Error: Error acquiring the state lock"}, "plan が失敗しました", "lock"},
		"plan が失敗すれば apply しない":                              {map[string]string{"plan-rc": "1", "plan.json": recipeNoop, "policy.json": recipePass}, "plan が失敗しました", "plan"},
		"終了コードと JSON が食い違えば apply しない":                       {map[string]string{"plan-rc": "0", "plan.json": recipeCreate, "policy.json": recipePass}, "食い違います", "plan"},
		"plan を JSON にできなければ apply しない":                      {map[string]string{"plan-rc": "2", "plan.json": "", "policy.json": recipePass}, "plan の JSON を解釈できません", "plan"},
		"Policy に違反すれば apply しない":                            {map[string]string{"plan-rc": "2", "plan.json": recipeCreate, "policy.json": recipeViolated}, "[PLAN-STATE-01] stub", "policy"},
		"Policy が評価されなければ apply しない":                         {map[string]string{"plan-rc": "2", "plan.json": recipeCreate, "policy.json": "[]"}, "Policy を通りません", "policy"},
		"conftest が何も出さなければ apply しない":                       {map[string]string{"plan-rc": "2", "plan.json": recipeCreate, "policy.json": ""}, "Policy を通りません", "policy"},
		"評価の数が規則の数と違えば apply しない":                            {map[string]string{"plan-rc": "2", "plan.json": recipeCreate, "policy.json": `[{"namespace":"plan.stateful_protection","successes":3}]`}, "一致しません", "policy"},
		"Key Policy の Policy に違反すれば apply しない":               {map[string]string{"plan-rc": "2", "plan.json": recipeCreate, "policy.json": recipePass, "policy-plan.kms_key_policy.json": `[{"namespace":"plan.kms_key_policy","successes":1,"failures":[{"msg":"[PLAN-KMS-09] stub"}]}]`}, "[PLAN-KMS-09] stub", "policy"},
		"Key Policy の評価の数が規則の数と違えば apply しない":                {map[string]string{"plan-rc": "2", "plan.json": recipeCreate, "policy.json": recipePass, "policy-plan.kms_key_policy.json": `[{"namespace":"plan.kms_key_policy","successes":2}]`}, "一致しません", "policy"},
		"Key Policy の Policy が評価されなければ apply しない":            {map[string]string{"plan-rc": "2", "plan.json": recipeCreate, "policy.json": recipePass, "policy-plan.kms_key_policy.json": "[]"}, "plan.kms_key_policy の Policy を通りません", "policy"},
		"宣言の外の resource policy があれば apply しない":               {map[string]string{"plan-rc": "2", "plan.json": recipeCreate, "policy.json": recipePass, "policy-plan.resource_policy_exclusive.json": `[{"namespace":"plan.resource_policy_exclusive","successes":3,"failures":[{"msg":"[PLAN-RPE-01] stub"}]}]`}, "[PLAN-RPE-01] stub", "policy"},
		"宣言の外の resource policy の評価の数が規則の数と違えば apply しない":     {map[string]string{"plan-rc": "2", "plan.json": recipeCreate, "policy.json": recipePass, "policy-plan.resource_policy_exclusive.json": `[{"namespace":"plan.resource_policy_exclusive","successes":3}]`}, "一致しません", "policy"},
		"宣言の外の resource policy の Policy が評価されなければ apply しない": {map[string]string{"plan-rc": "2", "plan.json": recipeCreate, "policy.json": recipePass, "policy-plan.resource_policy_exclusive.json": "[]"}, "plan.resource_policy_exclusive の Policy を通りません", "policy"},
	}
	for name, c := range fail {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			r := recipe(t, "ci-apply-admin-identity", c.knobs)
			require.Error(t, r.err, r.out)
			assert.Contains(t, r.out, c.want)
			assert.Zero(t, applies(r.calls), r.calls)
			assert.Equal(t, 1, countCalls(r.calls, "aws s3api put-object"), "失敗しても全文を置く: "+r.calls)
			assert.Contains(t, r.out, "apply の全文の版 ID: v-1")
			assert.NotContains(t, r.out, "✅ apply")
			assert.NotContains(t, r.out, "PLAN-SECRET")
			assert.Empty(t, r.summary)
			assert.Equal(t, "failure="+c.kind+"\n", r.output, "apply しなかった run は件数を書かず、失敗の段だけを書く")
			assert.Empty(t, r.left, "作業ファイルが残っています")
		})
	}

	for name, c := range map[string]struct {
		knobs     map[string]string
		lastCall  string
		notCalled string
		want      string
	}{
		"state の鍵の alias が無ければ init を呼ばない": {map[string]string{"aliases": `{"Aliases":[]}`}, "aws s3api get-bucket-encryption", "", "state の鍵の alias がありません"},
		"Bucket の既定の鍵が alias と違えば init を呼ばない": {
			map[string]string{"encryption": `{"ServerSideEncryptionConfiguration":{"Rules":[{"ApplyServerSideEncryptionByDefault":{"SSEAlgorithm":"aws:kms","KMSMasterKeyID":"arn:aws:kms:ap-northeast-1:123456789012:key/abcdef01-2345-6789-abcd-ef0123456789"}}]}}`},
			"aws s3api get-bucket-encryption", "", "既定の暗号化の鍵が違います",
		},
		"alias の一覧を取れなければ init を呼ばない":      {map[string]string{"kms-rc": "1"}, "aws kms list-aliases", "aws s3api get-bucket-encryption", ""},
		"Bucket の既定の暗号化を取れなければ init を呼ばない": {map[string]string{"encryption-rc": "1"}, "aws s3api get-bucket-encryption", "", ""},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			r := recipe(t, "ci-apply-admin-identity", c.knobs)
			require.Error(t, r.err, r.out)
			assert.Contains(t, r.calls, c.lastCall, "判定の経路を通っていません")
			if c.notCalled != "" {
				assert.NotContains(t, r.calls, c.notCalled)
			}
			if c.want != "" {
				assert.Contains(t, r.out, c.want)
			}
			assert.Empty(t, initArgs(r.calls), r.calls)
			assert.Equal(t, "failure=prepare\n", r.output, "init より前の失敗は prepare")
			assert.Empty(t, r.left, "作業ファイルが残っています")
		})
	}

	t.Run("init が失敗すれば plan を呼ばない", func(t *testing.T) {
		t.Parallel()

		r := recipe(t, "ci-apply-admin-identity", map[string]string{"init-rc": "1"})
		require.Error(t, r.err, r.out)
		assert.Contains(t, r.out, "init が失敗しました")
		assert.NotContains(t, r.calls, " plan ")
		assert.Zero(t, countCalls(r.calls, "aws s3api put-object"), "Bucket の名前は init の後でなければ読めない")
		assert.Equal(t, "failure=prepare\n", r.output, "plan より前の失敗は prepare")
		assert.Empty(t, r.left, "作業ファイルが残っています")
	})

	t.Run("run の番号が無ければ plan を呼ばない", func(t *testing.T) {
		t.Parallel()

		r := recipe(t, "ci-apply-admin-identity", map[string]string{"plan-rc": "2", "plan.json": recipeCreate, "policy.json": recipePass}, "GITHUB_RUN_ID=")
		require.Error(t, r.err, r.out)
		assert.Contains(t, r.out, "apply の出力の key を組めません")
		assert.NotContains(t, r.calls, " plan ")
		assert.Zero(t, applies(r.calls), r.calls)
		assert.Equal(t, "failure=prepare\n", r.output, "plan より前の失敗は prepare")
		assert.Empty(t, r.left, "作業ファイルが残っています")
	})

	ended := map[string]struct {
		knobs map[string]string
		want  string
		kind  string
	}{
		"apply が失敗すれば全文を置いてから非ゼロで終わる": {map[string]string{"apply-rc": "1"}, "apply が失敗しました", "apply"},
		"apply を打ち切れば全文を置いてから非ゼロで終わる": {map[string]string{"apply-rc": "1", "timeout-rc": "124"}, "apply を打ち切りました", "timeout"},
		"打ち切りの後に apply が成功を返しても失敗とする": {map[string]string{"apply-rc": "0", "timeout-rc": "124"}, "apply を打ち切りました", "timeout"},
	}
	for name, c := range ended {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			knobs := map[string]string{"plan-rc": "2", "plan.json": recipeCreate, "policy.json": recipePass}
			maps.Copy(knobs, c.knobs)
			r := recipe(t, "ci-apply-admin-identity", knobs)
			require.Error(t, r.err, r.out)
			assert.Equal(t, 1, applies(r.calls), r.calls)
			assert.Contains(t, r.out, c.want)
			assert.Equal(t, 1, countCalls(r.calls, "aws s3api put-object"), r.calls)
			assert.Less(t, strings.Index(r.calls, " apply -input=false"), strings.Index(r.calls, "aws s3api put-object"), "apply の後に全文を置く")
			assert.Contains(t, r.out, "apply の全文の版 ID: v-1")
			assert.Contains(t, r.out, "全文は上の key にあります")
			assert.Contains(t, r.out, "apply の終了コード: ")
			assert.NotContains(t, r.out, "apply の終了コード: 0")
			assert.NotContains(t, r.out, "✅ apply")
			assert.NotContains(t, r.out, "APPLY-SECRET")
			assert.Empty(t, r.summary)
			assert.Equal(t, "failure="+c.kind+"\n", r.output, "失敗した apply は件数を書かず、失敗の段だけを書く")
			assert.Empty(t, r.left, "作業ファイルが残っています")
		})
	}

	// 全文を置けない失敗が、先に起きた失敗の段を消すと、revert の対象の失敗が対象外と読まれる。
	for name, c := range map[string]struct {
		knobs       map[string]string
		uploadFails bool
		kind        string
	}{
		"apply が失敗した後に全文を置けなくても apply の段を書く":   {map[string]string{"apply-rc": "1"}, true, "apply"},
		"Policy に違反した後に全文を置けなくても policy の段を書く": {map[string]string{"policy.json": recipeViolated}, true, "policy"},
		"apply が lock の取得で落ちれば lock の段を書く":     {map[string]string{"apply-rc": "1", "apply-out": "Error: Error acquiring the state lock"}, false, "lock"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			knobs := map[string]string{"plan-rc": "2", "plan.json": recipeCreate, "policy.json": recipePass}
			if c.uploadFails {
				knobs["s3api-rc"] = "1"
			}
			maps.Copy(knobs, c.knobs)
			r := recipe(t, "ci-apply-admin-identity", knobs)
			require.Error(t, r.err, r.out)
			assert.Equal(t, "failure="+c.kind+"\n", r.output)
			assert.Empty(t, r.left, "作業ファイルが残っています")
		})
	}

	t.Run("apply が終わった後の失敗は applied の段を書く", func(t *testing.T) {
		t.Parallel()

		// job summary を書けない場所を渡し、apply と全文の保存が終わった後に失敗させる。
		r := recipe(t, "ci-apply-admin-identity", map[string]string{"plan-rc": "2", "plan.json": recipeCreate, "policy.json": recipePass},
			"GITHUB_STEP_SUMMARY="+filepath.Join(t.TempDir(), "missing", "summary.md"))
		require.Error(t, r.err, r.out)
		assert.Equal(t, 1, applies(r.calls), r.calls)
		assert.Contains(t, r.out, "apply の全文の版 ID: v-1")
		assert.Equal(t, "failure=applied\n", r.output)
	})

	t.Run("全文を置けなければ apply が成功しても失敗する", func(t *testing.T) {
		t.Parallel()

		r := recipe(t, "ci-apply-admin-identity", map[string]string{"plan-rc": "2", "plan.json": recipeCreate, "policy.json": recipePass, "s3api-rc": "1"})
		require.Error(t, r.err, r.out)
		assert.Equal(t, 1, applies(r.calls), r.calls)
		assert.Contains(t, r.out, "apply の終了コード: 0", "全文を置けなくても apply の成否はログに残す")
		assert.Contains(t, r.out, "apply の全文: key=")
		assert.NotContains(t, r.out, "apply の全文の版 ID")
		assert.NotContains(t, r.out, "✅ apply")
		assert.Empty(t, r.summary)
		assert.Equal(t, "failure=upload\n", r.output, "全文を置けない失敗は upload")
		assert.Empty(t, r.left, "作業ファイルが残っています")
	})

	t.Run("terraform の出力を公開されるログに出さない", func(t *testing.T) {
		t.Parallel()

		r := recipe(t, "ci-apply-admin-identity", map[string]string{"plan-rc": "2", "plan.json": recipeCreate, "policy.json": recipePass, "apply-rc": "1"})
		require.Error(t, r.err, r.out)
		require.Contains(t, r.calls, " plan ", "偽物が PLAN-SECRET を出す経路を通っていません")
		require.Equal(t, 1, applies(r.calls), "偽物が APPLY-SECRET を出す経路を通っていません")
		assert.NotContains(t, r.out, "PLAN-SECRET")
		assert.NotContains(t, r.out, "APPLY-SECRET")
	})

	// job summary と outputs は別々に書く。片方が無いことが、もう片方を書かない理由にならない。
	t.Run("job summary が無くても outputs は書く", func(t *testing.T) {
		t.Parallel()

		r := recipe(t, "ci-apply-admin-identity", map[string]string{"plan-rc": "2", "plan.json": recipeCreate, "policy.json": recipePass}, "GITHUB_STEP_SUMMARY=")
		require.NoError(t, r.err, r.out)
		assert.Empty(t, r.summary)
		assert.True(t, strings.HasPrefix(r.output, "plan=Plan: "), r.output)
	})

	t.Run("outputs が無くても job summary は書く", func(t *testing.T) {
		t.Parallel()

		r := recipe(t, "ci-apply-admin-identity", map[string]string{"plan-rc": "2", "plan.json": recipeCreate, "policy.json": recipePass}, "GITHUB_OUTPUT=")
		require.NoError(t, r.err, r.out)
		assert.Contains(t, r.summary, "apply が終わりました")
		assert.Empty(t, r.output)
	})

	t.Run("outputs に書けなければ非ゼロで終わる", func(t *testing.T) {
		t.Parallel()

		// 件数を渡せないまま成功で終わると、成功の通知は変更の有無を言えない。
		r := recipe(t, "ci-apply-admin-identity", map[string]string{"plan-rc": "2", "plan.json": recipeCreate, "policy.json": recipePass},
			"GITHUB_OUTPUT="+filepath.Join(t.TempDir(), "none", "output"))
		require.Error(t, r.err, r.out)
		assert.Equal(t, 1, applies(r.calls), r.calls)
	})

	t.Run("job summary と outputs が無くても成功する", func(t *testing.T) {
		t.Parallel()

		r := recipe(t, "ci-apply-admin-identity", map[string]string{"plan-rc": "2", "plan.json": recipeCreate, "policy.json": recipePass}, "GITHUB_STEP_SUMMARY=", "GITHUB_OUTPUT=")
		require.NoError(t, r.err, r.out)
		assert.Equal(t, 1, applies(r.calls), r.calls)
		assert.Equal(t, 1, countCalls(r.calls, "aws s3api put-object"), r.calls)
		assert.Contains(t, r.out, "apply の全文の版 ID: v-1")
		assert.Empty(t, r.summary)
		assert.Empty(t, r.output)
	})

	prepare := map[string]struct {
		knobs map[string]string
		want  string
	}{
		"root の資格情報なら init を呼ばない":     {map[string]string{"identity": "123456789012\tarn:aws:iam::123456789012:root\n"}, "root の資格情報では実行しません"},
		"identity を取れなければ init を呼ばない": {map[string]string{"sts-rc": "255"}, ""},
	}
	for name, c := range prepare {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			r := recipe(t, "ci-apply-admin-identity", c.knobs)
			require.Error(t, r.err, r.out)
			assert.Contains(t, r.out, c.want)
			assert.Contains(t, r.calls, "aws sts get-caller-identity")
			assert.NotContains(t, r.calls, " init ")
			assert.NotContains(t, r.out, "123456789012")
			assert.Empty(t, r.left, "作業ファイルが残っています")
		})
	}
}

func testCiPlanEntry(t *testing.T, recipe recipeFunc) {
	t.Helper()

	t.Run("全文を版 ID 付きで置き、Policy を掛け、apply しない", func(t *testing.T) {
		t.Parallel()

		r := recipe(t, "ci-plan-admin-identity", map[string]string{"plan-rc": "2", "plan.json": recipeCreate, "policy.json": recipePass})
		require.NoError(t, r.err, r.out)
		assert.Equal(t, 1, countCalls(r.calls, "aws s3api put-object"), r.calls)
		assert.Contains(t, r.calls, "--bucket tabp-admin-tfplan-123456789012-ap-northeast-1-an --key pr/7/8-1-admin-identity.txt")
		assert.Contains(t, r.calls, "--expected-bucket-owner 123456789012")
		assert.Contains(t, r.calls, "plan -lock=false")
		assert.Contains(t, initArgs(r.calls), "-backend-config=kms_key_id=arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab", "init に state の鍵を渡す")
		assert.Zero(t, applies(r.calls), r.calls)
		assert.Contains(t, r.out, "plan の全文の版 ID: v-1")
		assert.Contains(t, r.summary, "- 版 ID: `v-1`")
		assert.NotContains(t, r.out, "PLAN-SECRET")
		assert.Equal(t, 3, countCalls(r.calls, "conftest test "), r.calls)
		assert.Less(t, strings.Index(r.calls, "--namespace plan.stateful_protection"), strings.Index(r.calls, "--namespace plan.kms_key_policy"), r.calls)
		assert.Less(t, strings.Index(r.calls, "--namespace plan.kms_key_policy"), strings.Index(r.calls, "--namespace plan.resource_policy_exclusive"), r.calls)
		assert.Contains(t, r.out, "plan.kms_key_policy: 規則 3 件")
		assert.Contains(t, r.out, "plan.resource_policy_exclusive: 規則 4 件")
		assert.Empty(t, r.left, "作業ファイルが残っています")
	})

	fail := map[string]struct {
		knobs map[string]string
		want  string
	}{
		"plan が失敗しても全文を置いてから失敗する":                              {map[string]string{"plan-rc": "1", "plan.json": recipeNoop, "policy.json": recipePass}, "全文は上の key にあります"},
		"Policy に違反すれば全文を置いてから失敗する":                            {map[string]string{"plan-rc": "2", "plan.json": recipeCreate, "policy.json": recipeViolated}, "[PLAN-STATE-01] stub"},
		"plan を JSON にできなくても全文を置いてから失敗する":                      {map[string]string{"plan-rc": "2", "plan.json": "", "policy.json": recipePass}, "plan の JSON を解釈できません"},
		"Policy が評価されなくても全文を置いてから失敗する":                         {map[string]string{"plan-rc": "2", "plan.json": recipeCreate, "policy.json": "[]"}, "Policy を通りません"},
		"Key Policy の Policy が評価されなくても全文を置いてから失敗する":            {map[string]string{"plan-rc": "2", "plan.json": recipeCreate, "policy.json": recipePass, "policy-plan.kms_key_policy.json": "[]"}, "plan.kms_key_policy の Policy を通りません"},
		"Key Policy の Policy に違反すれば全文を置いてから失敗する":               {map[string]string{"plan-rc": "2", "plan.json": recipeCreate, "policy.json": recipePass, "policy-plan.kms_key_policy.json": `[{"namespace":"plan.kms_key_policy","successes":1,"failures":[{"msg":"[PLAN-KMS-09] stub"}]}]`}, "[PLAN-KMS-09] stub"},
		"宣言の外の resource policy があれば全文を置いてから失敗する":               {map[string]string{"plan-rc": "2", "plan.json": recipeCreate, "policy.json": recipePass, "policy-plan.resource_policy_exclusive.json": `[{"namespace":"plan.resource_policy_exclusive","successes":3,"failures":[{"msg":"[PLAN-RPE-01] stub"}]}]`}, "[PLAN-RPE-01] stub"},
		"宣言の外の resource policy の Policy が評価されなくても全文を置いてから失敗する": {map[string]string{"plan-rc": "2", "plan.json": recipeCreate, "policy.json": recipePass, "policy-plan.resource_policy_exclusive.json": "[]"}, "plan.resource_policy_exclusive の Policy を通りません"},
	}
	for name, c := range fail {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			r := recipe(t, "ci-plan-admin-identity", c.knobs)
			require.Error(t, r.err, r.out)
			assert.Contains(t, r.out, c.want)
			assert.Equal(t, 1, countCalls(r.calls, "aws s3api put-object"), r.calls)
			assert.NotContains(t, r.out, "PLAN-SECRET")
			assert.Empty(t, r.summary)
			assert.NotContains(t, r.output, "failure=", "plan の入口は失敗の段を書かない")
			assert.Empty(t, r.left, "作業ファイルが残っています")
		})
	}

	t.Run("変更が無ければ No changes. を置く", func(t *testing.T) {
		t.Parallel()

		r := recipe(t, "ci-plan-admin-identity", map[string]string{"plan-rc": "0", "plan.json": recipeNoop, "policy.json": recipePass})
		require.NoError(t, r.err, r.out)
		assert.Equal(t, 1, countCalls(r.calls, "aws s3api put-object"), r.calls)
		assert.Contains(t, r.summary, "No changes.")
	})

	early := map[string]struct {
		knobs map[string]string
		env   []string
		want  string
		plans bool
	}{
		"全文を置けなければ判定へ進まない":             {map[string]string{"plan-rc": "2", "plan.json": recipeCreate, "policy.json": recipePass, "s3api-rc": "1"}, nil, "plan の全文: key=", true},
		"Pull Request の番号が無ければ全文を置かない": {map[string]string{"plan-rc": "2", "plan.json": recipeCreate, "policy.json": recipePass}, []string{"CI_PR="}, "plan の出力の key を組めません", true},
	}
	for name, c := range early {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			r := recipe(t, "ci-plan-admin-identity", c.knobs, c.env...)
			require.Error(t, r.err, r.out)
			assert.Contains(t, r.out, c.want)
			assert.Equal(t, c.plans, strings.Contains(r.calls, " plan "), r.calls)
			assert.NotContains(t, r.out, "plan の全文の版 ID")
			assert.Zero(t, countCalls(r.calls, "conftest"), r.calls)
			assert.Empty(t, r.summary)
			assert.Empty(t, r.left, "作業ファイルが残っています")
		})
	}
}

func testCiUnlockEntry(t *testing.T, recipe recipeFunc) {
	t.Helper()

	const (
		key  = "identity/terraform.tfstate.tflock"
		etag = `"0123456789abcdef0123456789abcdef"`
		id   = "c7f6a1b2-3d4e-4f50-8a9b-0c1d2e3f4a5b"
	)
	list := `{"Name":"b","Prefix":"` + key + `","KeyCount":1,"MaxKeys":1000,"IsTruncated":false,"Contents":[{"Key":"` + key + `","ETag":"\"0123456789abcdef0123456789abcdef\""}]}`
	body := `{"ID":"` + id + `","Operation":"OperationTypeApply","Who":"runner@fv-az1-2","Version":"1.16.6","Path":"tabp-admin-tfstate-123456789012-ap-northeast-1-an/identity/terraform.tfstate"}`
	meta := func(modified string) string {
		return `{"ETag":"\"0123456789abcdef0123456789abcdef\"","LastModified":"` + modified + `","VersionId":"lock-ver-1"}`
	}
	old := map[string]string{"lock-list": list, "lock-body": body, "lock-meta": meta("2000-01-01T00:00:00+00:00")}
	with := func(over map[string]string) map[string]string {
		m := maps.Clone(old)
		maps.Copy(m, over)

		return m
	}
	deletes := func(calls string) int { return countCalls(calls, "aws s3api delete-object") }

	t.Run("lock が無ければ何も消さずに成功する", func(t *testing.T) {
		t.Parallel()

		r := recipe(t, "ci-unlock-admin-identity", nil)
		require.NoError(t, r.err, r.out)
		assert.Contains(t, r.out, "✅ lock はありません")
		assert.Contains(t, r.calls, "--prefix "+key+" --expected-bucket-owner 123456789012 --no-paginate")
		assert.Zero(t, countCalls(r.calls, "aws s3api get-object"), r.calls)
		assert.Zero(t, deletes(r.calls), r.calls)
		assert.Zero(t, countCalls(r.calls, "terraform"), "terraform を呼ばない")
		assert.Contains(t, r.summary, "lock はありません")
		assert.Empty(t, r.left, "作業ファイルが残っています")
	})

	t.Run("古い lock は読んだ版の ETag を条件に1回だけ消す", func(t *testing.T) {
		t.Parallel()

		r := recipe(t, "ci-unlock-admin-identity", old)
		require.NoError(t, r.err, r.out)
		assert.Equal(t, 1, deletes(r.calls), r.calls)
		assert.Contains(t, r.calls, "aws s3api get-object --region ap-northeast-1 --bucket tabp-admin-tfstate-123456789012-ap-northeast-1-an --key "+key+" --if-match "+etag)
		assert.Contains(t, r.calls, "aws s3api delete-object --region ap-northeast-1 --bucket tabp-admin-tfstate-123456789012-ap-northeast-1-an --key "+key+" --if-match "+etag)
		assert.Contains(t, r.out, "lock: ID="+id)
		assert.Contains(t, r.out, "版 ID=lock-ver-1")
		assert.Contains(t, r.out, "✅ lock を外しました（delete marker の版 ID: marker-1）")
		assert.NotContains(t, r.out, "123456789012", "アカウント ID と Bucket 名をログに出さない")
		assert.Contains(t, r.summary, "```text\nlock: ID="+id, "lock の説明は summary でコードブロックに入れる")
		assert.Contains(t, r.summary, "lock を外しました")
		assert.Zero(t, countCalls(r.calls, "terraform"), "terraform を呼ばない")
		assert.Empty(t, r.left, "作業ファイルが残っています")
	})

	fresh := time.Now().UTC().Add(-time.Minute).Format("2006-01-02T15:04:05+00:00")
	for name, c := range map[string]struct {
		knobs                map[string]string
		want                 string
		lists, gets, deletes int
	}{
		"新しい lock は消さずに ID を出して失敗する":   {with(map[string]string{"lock-meta": meta(fresh)}), "ID=" + id, 1, 1, 0},
		"lock の中身を読めなければ消さない":          {with(map[string]string{"lock-body": "nope"}), "Terraform の lock として読めません", 1, 1, 0},
		"人の手元の apply の lock は古くても消さない": {with(map[string]string{"lock-body": strings.Replace(body, "runner@fv-az1-2", "alice@laptop.local", 1)}), "CI のランナーが apply の job か lock を残す job で取った lock ではない", 1, 1, 0},
		"一覧の後に置き換わった lock は消さない":       {with(map[string]string{"lock-meta": strings.Replace(meta("2000-01-01T00:00:00+00:00"), "0123", "ffff", 1)}), "置き換わりました", 1, 1, 0},
		"版 ID の無い lock は消さない":          {with(map[string]string{"lock-meta": `{"ETag":"\"0123456789abcdef0123456789abcdef\"","LastModified":"2000-01-01T00:00:00+00:00"}`}), "版 ID がありません", 1, 1, 0},
		"一覧の形を読めなければ lock 無しと読まない":     {map[string]string{"lock-list": `{"Contents":[]}`}, "一覧を読めません", 1, 0, 0},
		"一覧が空の出力なら lock 無しと読まない":       {map[string]string{"lock-list": ""}, "一覧を読めません", 1, 0, 0},
		"一覧を取れなければ lock を読まない":         {map[string]string{"list-rc": "254"}, "", 1, 0, 0},
		"lock を読めなければ消さない":             {with(map[string]string{"get-rc": "254"}), "", 1, 1, 0},
		"消せなければ非ゼロで終わり、外したと出さない":       {with(map[string]string{"delete-rc": "254"}), "", 1, 1, 1},
		"root の資格情報なら一覧を取らない":          {map[string]string{"identity": "123456789012\tarn:aws:iam::123456789012:root\n"}, "root の資格情報では実行しません", 0, 0, 0},
		"identity を取れなければ一覧を取らない":      {map[string]string{"sts-rc": "255"}, "", 0, 0, 0},
		"lock の key を決められなければ一覧を取らない":  {map[string]string{"backend.tf": "terraform {\n  backend \"s3\" {\n    region = \"ap-northeast-1\"\n  }\n}\n"}, "key が1つではないか", 0, 0, 0},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			r := recipe(t, "ci-unlock-admin-identity", c.knobs)
			require.Error(t, r.err, r.out)
			if c.want != "" {
				assert.Contains(t, r.out, c.want)
			}
			assert.Equal(t, c.lists, countCalls(r.calls, "aws s3api list-objects-v2"), r.calls)
			assert.Equal(t, c.gets, countCalls(r.calls, "aws s3api get-object"), r.calls)
			assert.Equal(t, c.deletes, deletes(r.calls), r.calls)
			assert.NotContains(t, r.out, "✅")
			assert.Empty(t, r.summary)
			assert.Empty(t, r.left, "作業ファイルが残っています")
		})
	}

	for name, knobs := range map[string]map[string]string{
		"lock が無ければ job summary が無くても成功する":   nil,
		"古い lock は job summary が無くても外して成功する": old,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			r := recipe(t, "ci-unlock-admin-identity", knobs, "GITHUB_STEP_SUMMARY=")
			require.NoError(t, r.err, r.out)
			assert.Contains(t, r.out, "✅")
			assert.Empty(t, r.summary)
		})
	}
}

func testCiCredentials(t *testing.T, recipe recipeFunc) {
	t.Helper()

	jwt := jwtWith(`{"sub":"repo:o/r:environment:admin-plan"}`)
	response := map[string]string{"oidc-response": `{"value":"` + jwt + `"}`}
	env := []string{
		"ACTIONS_ID_TOKEN_REQUEST_URL=https://t.actions.githubusercontent.com/x", "ACTIONS_ID_TOKEN_REQUEST_TOKEN=bearer",
		"CI_ACCOUNT_ID=123456789012", "CI_ENV=admin",
	}
	const assume = "aws sts assume-role-with-web-identity"

	t.Run("assume して profile を置き、token と応答を消す", func(t *testing.T) {
		t.Parallel()

		r := recipe(t, "ci-aws-credentials", response, env...)
		require.NoError(t, r.err, r.out)
		assert.Equal(t, 1, countCalls(r.calls, assume), r.calls)
		assert.Contains(t, r.calls, "--role-arn arn:aws:iam::123456789012:role/tabp-admin-ci-plan")
		assert.Equal(t, 4, countCalls(r.calls, "aws configure set --profile tabp-admin-ci-plan "), r.calls)
		for _, v := range []string{"KEY-SECRET", "SECRET-SECRET", "SESSION-SECRET"} {
			assert.Contains(t, r.out, "::add-mask::"+v)
		}
		assert.Contains(t, r.out, "OIDC の sub: repo:o/r:environment:admin-plan")
		assert.NotContains(t, r.out, jwt)
		assert.Contains(t, r.out, "profile に資格情報を置きました")
		assert.Empty(t, r.left, "token と応答が残っています")
	})

	t.Run("役割の profile と role を取る", func(t *testing.T) {
		t.Parallel()

		r := recipe(t, "ci-aws-credentials", response, append(slices.Clone(env), "CI_ROLE=apply-identity")...)
		require.NoError(t, r.err, r.out)
		assert.Contains(t, r.calls, "--role-arn arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity")
		assert.Equal(t, 4, countCalls(r.calls, "aws configure set --profile tabp-admin-ci-apply-identity "), r.calls)
	})

	fail := map[string]struct {
		knobs map[string]string
		env   []string
		want  string
	}{
		"要求先が無ければ token を要求しない":              {response, []string{"ACTIONS_ID_TOKEN_REQUEST_URL="}, "id-token: write がありません"},
		"token を要求できなければ assume しない":         {map[string]string{"oidc-response": "", "curl-rc": "22"}, nil, ""},
		"応答から token を読めなければ assume しない":      {map[string]string{"oidc-response": "nope"}, nil, "token を読めません"},
		"sub が environment でなければ assume しない": {map[string]string{"oidc-response": `{"value":"` + jwtWith(`{"sub":"repo:o/r:ref:refs/heads/main"}`) + `"}`}, nil, "sub を読めません"},
		"アカウント ID が不正なら assume しない":          {response, []string{"CI_ACCOUNT_ID=1234"}, "role の ARN を組めません"},
	}
	for name, c := range fail {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			r := recipe(t, "ci-aws-credentials", c.knobs, append(slices.Clone(env), c.env...)...)
			require.Error(t, r.err, r.out)
			assert.Contains(t, r.out, c.want)
			assert.Zero(t, countCalls(r.calls, assume), r.calls)
			assert.Zero(t, countCalls(r.calls, "aws configure"), r.calls)
			assert.Empty(t, r.left, "token と応答が残っています")
		})
	}

	t.Run("assume できなければ profile を置かない", func(t *testing.T) {
		t.Parallel()

		r := recipe(t, "ci-aws-credentials", map[string]string{"oidc-response": response["oidc-response"], "assume-rc": "254"}, env...)
		require.Error(t, r.err, r.out)
		assert.Equal(t, 1, countCalls(r.calls, assume), r.calls)
		assert.Zero(t, countCalls(r.calls, "aws configure"), r.calls)
		assert.NotContains(t, r.out, "profile に資格情報を置きました")
		assert.Empty(t, r.left, "token と応答が残っています")
	})
}

func Test_processAlive(t *testing.T) {
	t.Parallel()

	t.Run("走っているプロセスは生きている", func(t *testing.T) {
		t.Parallel()

		assert.True(t, processAlive(os.Getpid()))
	})

	t.Run("終わって回収されたプロセスは生きていない", func(t *testing.T) {
		t.Parallel()

		cmd := exec.CommandContext(t.Context(), "true")
		require.NoError(t, cmd.Run())
		assert.False(t, processAlive(cmd.Process.Pid))
	})
}

func testCiHoldEntry(t *testing.T, recipe recipeFunc) {
	t.Helper()

	const (
		key  = "identity/terraform.tfstate.tflock"
		etag = `"0123456789abcdef0123456789abcdef"`
		id   = "c7f6a1b2-3d4e-4f50-8a9b-0c1d2e3f4a5b"
	)
	empty := `{"Name":"b","Prefix":"` + key + `","KeyCount":0,"MaxKeys":1000,"IsTruncated":false}`
	held := `{"Name":"b","Prefix":"` + key + `","KeyCount":1,"MaxKeys":1000,"IsTruncated":false,"Contents":[{"Key":"` + key + `","ETag":"\"0123456789abcdef0123456789abcdef\""}]}`
	body := `{"ID":"` + id + `","Operation":"OperationTypePlan","Who":"runner@fv-az1-2","Version":"1.17.1","Path":"tabp-admin-tfstate-123456789012-ap-northeast-1-an/identity/terraform.tfstate"}`
	now := time.Now().UTC().Format("2006-01-02T15:04:05+00:00")
	meta := `{"ETag":"\"0123456789abcdef0123456789abcdef\"","LastModified":"` + now + `","VersionId":"lock-ver-1"}`
	ok := map[string]string{
		"plan-out": "aws_iam_role.apply: Refreshing state... [id=apply]\n", "plan-hold": "1",
		"lock-list-1": empty, "lock-list-2": held, "lock-body": body, "lock-meta": meta,
	}
	with := func(over map[string]string, drop ...string) map[string]string {
		m := maps.Clone(ok)
		maps.Copy(m, over)

		for _, k := range drop {
			delete(m, k)
		}

		return m
	}
	plans := func(calls string) int {
		n := 0

		for line := range strings.Lines(calls) {
			if strings.HasPrefix(line, applyCall) && strings.Contains(line, " plan ") {
				n++
			}
		}

		return n
	}
	planGone := func(t *testing.T, r recipeResult, knobs map[string]string) {
		t.Helper()

		raw, err := os.ReadFile(filepath.Join(filepath.Dir(r.plans), "knobs", "plan-pid"))
		if _, hold := knobs["plan-hold"]; !hold || plans(r.calls) == 0 {
			require.Error(t, err, "plan-hold の無い偽物の plan も、起動しなかった plan も pid を書かない")

			return
		}

		require.NoError(t, err, "偽物の plan が起動していません")

		pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
		require.NoError(t, err)
		assert.Eventually(t, func() bool { return !processAlive(pid) }, 5*time.Second, 50*time.Millisecond, "裏の terraform が残っています")
	}

	t.Run("lock を取った後に止め、残った lock の形と消せる時刻を出して成功する", func(t *testing.T) {
		t.Parallel()

		start := time.Now()
		r := recipe(t, "ci-hold-admin-identity", ok)
		require.NoError(t, r.err, r.out)
		assert.Less(t, time.Since(start), 30*time.Second, "plan の終わりを待たずに止める")
		assert.Contains(t, r.out, "lock: ID="+id)
		assert.Contains(t, r.out, "Operation=OperationTypePlan")
		assert.Contains(t, r.out, "Terraform Unlock が消せるのは ")
		assert.Contains(t, r.out, "✅ lock を残しました")
		assert.NotContains(t, r.out, "PLAN-SECRET", "plan の全文をログに出さない")
		assert.NotContains(t, r.out, "123456789012", "アカウント ID と Bucket 名をログに出さない")
		assert.Equal(t, 1, plans(r.calls), r.calls)
		assert.Zero(t, applies(r.calls), "apply しない")
		assert.Zero(t, countCalls(r.calls, "aws s3api delete-object"), r.calls)
		assert.Zero(t, countCalls(r.calls, "aws s3api put-object"), r.calls)
		assert.Contains(t, r.calls, "aws s3api get-object --region ap-northeast-1 --bucket tabp-admin-tfstate-123456789012-ap-northeast-1-an --key "+key+" --if-match "+etag)
		assert.Contains(t, r.summary, "```text\nlock: ID="+id)
		assert.NotContains(t, r.out, "plan を起動した後に失敗しました")
		assert.Empty(t, r.left, "作業ファイルが残っています")
		planGone(t, r, ok)
	})

	for name, c := range map[string]struct {
		knobs              map[string]string
		env                []string
		want               string
		lists, plans, gets int
	}{
		"lock が既にあれば plan を起動しない":              {with(map[string]string{"lock-list-1": held}), nil, "lock が既にあります", 1, 0, 0},
		"lock を取った後の行を出さずに plan が終われば失敗する":     {with(nil, "plan-hold", "plan-out"), nil, "lock を取った後の行を出さずに終わりました", 1, 1, 0},
		"行を出した後に lock が無ければ失敗する":               {with(map[string]string{"lock-list-2": empty}), nil, "lock が残りませんでした", 2, 1, 0},
		"残った lock が lock の解除の消す形でなければ失敗する":     {with(map[string]string{"lock-body": strings.Replace(body, "runner@fv-az1-2", "alice@laptop.local", 1)}), nil, "lock ではないので消しません", 2, 1, 1},
		"待つ時間の内に行が現れなければ裏の terraform を止めて失敗する": {with(nil, "plan-out"), []string{"CI_HOLD_LIMIT=1s"}, "待つ時間の内に出しませんでした", 1, 1, 0},
		"一覧を取れなければ plan を起動しない":                {with(map[string]string{"list-rc": "254"}), nil, "", 1, 0, 0},
		"lock を読めなければ成功しない":                    {with(map[string]string{"get-rc": "254"}), nil, "", 2, 1, 1},
		"止めた後の一覧を取れなければ lock を読まない":            {with(map[string]string{"list-rc-2": "254"}), nil, "", 2, 1, 0},
		"残った lock の中身を読めなければ失敗する":              {with(map[string]string{"lock-body": "nope"}), nil, "Terraform の lock として読めません", 2, 1, 1},
		"一覧の後に置き換わった lock は失敗する":               {with(map[string]string{"lock-meta": strings.Replace(meta, "0123", "ffff", 1)}), nil, "置き換わりました", 2, 1, 1},
		"行を出した直後に plan が終わって lock が消えていれば失敗する": {with(map[string]string{"lock-list-2": empty}, "plan-hold"), nil, "lock が残りませんでした", 2, 1, 0},
		"init が失敗すれば一覧も plan も呼ばない":            {with(map[string]string{"init-rc": "1"}), nil, "init が失敗しました", 0, 0, 0},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			r := recipe(t, "ci-hold-admin-identity", c.knobs, c.env...)
			require.Error(t, r.err, r.out)
			if c.want != "" {
				assert.Contains(t, r.out, c.want)
			}
			assert.Equal(t, c.lists, countCalls(r.calls, "aws s3api list-objects-v2"), r.calls)
			assert.Equal(t, c.plans, plans(r.calls), r.calls)
			assert.Equal(t, c.gets, countCalls(r.calls, "aws s3api get-object"), r.calls)
			assert.Zero(t, applies(r.calls), "apply しない")
			assert.Zero(t, countCalls(r.calls, "aws s3api delete-object"), r.calls)
			assert.NotContains(t, r.out, "✅")
			assert.NotContains(t, r.out, "PLAN-SECRET")
			assert.Empty(t, r.summary)
			assert.Empty(t, r.left, "作業ファイルが残っています")
			if c.plans > 0 {
				assert.Contains(t, r.out, "plan を起動した後に失敗しました。lock が残っていれば", "lock が残りうることを報せる")
			} else {
				assert.NotContains(t, r.out, "plan を起動した後に失敗しました")
			}
			planGone(t, r, c.knobs)
		})
	}

	t.Run("job summary が無くても成功する", func(t *testing.T) {
		t.Parallel()

		r := recipe(t, "ci-hold-admin-identity", ok, "GITHUB_STEP_SUMMARY=")
		require.NoError(t, r.err, r.out)
		assert.Contains(t, r.out, "✅ lock を残しました")
		planGone(t, r, ok)
	})
}

package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

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
	return deps{readFile: os.ReadFile, environ: environ, git: stubGit("", "")}
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
		}
		for name, args := range cases {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				var out bytes.Buffer
				require.ErrorIs(t, run(args, &out, testDeps(nil)), errUsage)
				assert.Empty(t, out.String())
			})
		}

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

			got, err := bucketName("tabp", "admin", "123456789012", "ap-northeast-1")
			require.NoError(t, err)
			assert.Equal(t, "tabp-admin-tfstate-123456789012-ap-northeast-1-an", got)
		})

	})

	// 写しと正本のずれを見るので、正本である実物を読む（ADR-0702 決定17 の但し書き）。
	t.Run("写しと正本のずれ", func(t *testing.T) {
		t.Parallel()

		t.Run("env/admin/identity のテストが期待する名前と一致する", func(t *testing.T) {
			t.Parallel()

			got, err := bucketName("tabp", "admin", "123456789012", "ap-northeast-1")
			require.NoError(t, err)

			tftest, err := os.ReadFile(filepath.Join("..", "..", "env", "admin", "identity", "tests", "imports.tftest.hcl"))
			require.NoError(t, err)
			assert.Contains(t, string(tftest), `== "`+got+`"`)
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
			assert.Contains(t, string(doc), `BUCKET="`+fmt.Sprintf(bucketFormat, "tabp", "admin", "${ACCOUNT_ID}", "${REGION}")+`"`)
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

				_, err := bucketName(args[0], args[1], "123456789012", "ap-northeast-1")
				require.ErrorIs(t, err, errToken)
			})
		}

		for _, region := range []string{"Tokyo", "ap-northeast-1\n"} {
			t.Run("region の形が誤っていれば失敗する "+region, func(t *testing.T) {
				t.Parallel()

				_, err := bucketName("tabp", "admin", "123456789012", region)
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
// 展開結果と、偽物の道具で init を走らせた結果で見る。レシピそのものが対象なので実物の local.mk を読む。
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

	// run は init-admin-identity を ciFakes の偽物で走らせ、偽物の呼び出しの記録とレシピの出力を返す。
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

// TestCiMk は、ci.mk のレシピが CI の profile で plan と apply を行い、terraform の出力を公開されるログに
// 出さない配線を、make -n の展開結果で見る。レシピそのものが対象なので実物の ci.mk を読む。
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
			`output -raw plan_output_bucket_name 2>> "$out"`,
			`plan -lock=false -input=false -no-color -detailed-exitcode -out="$tfplan" >> "$out" 2>&1 || rc=$?`,
			`show -json "$tfplan" > "$json" 2>> "$out" || :`,
			`sha256 "$out"`,
			envPrefix("plan")+` aws s3api put-object`,
			`--checksum-sha256 "$sha" --expected-bucket-owner "$account"`,
			`plan-summary -exitcode "$rc" "$json"`,
			`conftest test --combine --no-color -o json --namespace plan.stateful_protection --policy conftest/plan --data conftest/plan/decl.json "$json" > "$pol"`,
			`policy-result -namespace plan.stateful_protection -rego conftest/plan/stateful_protection.rego "$pol"`,
		)
		assert.NotContains(t, got, " apply ")
		assert.Equal(t, 1, strings.Count(got, "| tee"), "tee は commit の行だけに使う: "+got)
	})

	t.Run("CI の init は鍵を渡さない選択を受けない", func(t *testing.T) {
		t.Parallel()

		for _, target := range []string{"ci-plan-admin-identity", "ci-apply-admin-identity"} {
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
		assert.NotContains(t, got, "put-object")
		assert.Contains(t, got, `trap 'rm -f "$out" "$json" "$tfplan" "$pol"' EXIT`)
		order(t, got,
			envPrefix("apply-identity")+` terraform -chdir="$dir" init -input=false -no-color -lockfile=readonly`,
			`plan -input=false -no-color -detailed-exitcode -out="$tfplan" >> "$out" 2>&1 || rc=$?`,
			`show -json "$tfplan" > "$json" 2>> "$out" || :`,
			`plan-summary -exitcode "$rc" "$json"`,
			`policy-result -namespace plan.stateful_protection -rego conftest/plan/stateful_protection.rego "$pol"`,
			envPrefix("apply-identity")+` terraform -chdir="$dir" apply -input=false -no-color "$tfplan" >> "$out" 2>&1`,
		)
		assert.Equal(t, 1, strings.Count(got, " apply -input=false"), got)
		assert.Equal(t, 1, strings.Count(got, "| tee"), "tee は commit の行だけに使う: "+got)
	})

	t.Run("資格情報は OIDC の token で1回だけ assume し、token を消す", func(t *testing.T) {
		t.Parallel()

		got := expand(t, "ci-aws-credentials")
		assert.Equal(t, 1, strings.Count(got, "assume-role-with-web-identity"), got)
		order(t, got, `oidc-url`, `-o "$resp" "$url"`, `oidc-token "$resp"' > "$token"`, `oidc-sub "$token"`, "assume-role-with-web-identity")
		assert.Contains(t, got, `trap 'rm -f "$token" "$resp"' EXIT`)
		assert.NotContains(t, got, "jq ")
		assert.Contains(t, got, "role-arn -project tabp -env admin -role plan")
		assert.Contains(t, got, `do echo "::add-mask::$v"; done`)
		assert.Contains(t, got, "configure set --profile tabp-admin-ci-plan")
		assert.NotContains(t, got, "AWS_ACCESS_KEY_ID")
	})
}

// ciFakes は、ci.mk のレシピが起動する terraform・aws・conftest・curl・git の偽物です。
// plan・apply・assume-role は secret の文字列を出し、それが公開されるログへ漏れないことを見られるようにする。
const ciFakes = `#!/bin/sh
k=%[1]q
rc() { cat "$k/$1" 2>/dev/null || echo 0; }
echo "$(basename "$0") $*" >> "$k/calls"
case "$(basename "$0")" in
aws)
	case "$1 $2" in
	"sts get-caller-identity") cat "$k/identity" 2>/dev/null || printf '123456789012\tarn:aws:iam::123456789012:user/ci\n'; exit "$(rc sts-rc)" ;;
	"sts assume-role-with-web-identity") printf 'KEY-SECRET\tSECRET-SECRET\tSESSION-SECRET\n'; exit "$(rc assume-rc)" ;;
	"kms list-aliases") cat "$k/aliases" 2>/dev/null || echo '{"Aliases":[{"AliasName":"alias/tabp-admin-tfstate","AliasArn":"arn:aws:kms:ap-northeast-1:123456789012:alias/tabp-admin-tfstate","TargetKeyId":"1234abcd-12ab-34cd-56ef-1234567890ab"}]}'; exit "$(rc kms-rc)" ;;
	"s3api get-bucket-encryption") cat "$k/encryption" 2>/dev/null || echo '{"ServerSideEncryptionConfiguration":{"Rules":[{"ApplyServerSideEncryptionByDefault":{"SSEAlgorithm":"AES256"}}]}}'; exit "$(rc encryption-rc)" ;;
	s3api*) echo v-1; exit "$(rc s3api-rc)" ;;
	esac
	exit 0 ;;
conftest) cat "$k/policy.json"; exit 1 ;;
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
output) echo plan-output-bucket; exit "$(rc output-rc)" ;;
plan)
	for a; do case "$a" in -out=*) echo plan > "${a#-out=}" ;; esac; done
	echo PLAN-SECRET
	exit "$(rc plan-rc)" ;;
show) test -s "$last" || exit 1; cat "$k/plan.json" ;;
apply) echo APPLY-SECRET; exit "$(rc apply-rc)" ;;
esac
`

// writeFakes は、ciFakes の偽物の道具を dir に置き、結果を knobs のファイルで切り替えるようにする。
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
	recipeViolated = `[{"namespace":"plan.stateful_protection","successes":1,"failures":[{"msg":"[PLAN-STATE-01] stub"}]}]`
)

type recipeResult struct {
	out, calls, summary, plans string
	err                        error
	left                       []string
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

// initArgs は、terraform の init の行を返す。init が呼ばれていなければ空文字。
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

// recipeFunc は、ci.mk のレシピを偽物の道具で走らせ、その結果を返す。
type recipeFunc func(t *testing.T, target string, knobs map[string]string, env ...string) recipeResult

// newRecipe は、build 済みの tf-local を使う recipeFunc を返す。
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

		for _, d := range []string{k, identity, policy, filepath.Join(root, "bin")} {
			require.NoError(t, os.MkdirAll(d, 0o700))
		}

		require.NoError(t, os.WriteFile(filepath.Join(identity, "backend.tf"), []byte(backendOK), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(identity, "providers.tf"), []byte(providersOK), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(policy, "stateful_protection.rego"), []byte("deny contains msg if {\n}\n\ndeny contains msg if {\n}\n"), 0o600))

		for name, v := range knobs {
			require.NoError(t, os.WriteFile(filepath.Join(k, name), []byte(v), 0o600))
		}

		writeFakes(t, filepath.Join(root, "bin"), k, "terraform", "aws", "conftest", "curl", "git")

		cmd := exec.CommandContext(t.Context(), "make", "-C", filepath.Join("..", ".."), "-s", "--no-print-directory", target, "RUNNER_MODE=host", "GO_TOOL=",
			"TF_LOCAL="+tfLocalShell(bin), "TF_BASE="+filepath.Join(root, "base"), "CI_PLAN_DIR="+plans,
			"TF_BIN="+filepath.Join(root, "bin", "terraform"), "AWS_BIN="+filepath.Join(root, "bin", "aws"), "CONFTEST_BIN="+filepath.Join(root, "bin", "conftest"),
			"POLICY_PLAN_DIR="+policy, "POLICY_PLAN_DECL="+filepath.Join(policy, "decl.json"))
		cmd.Env = append(slices.DeleteFunc(os.Environ(), func(kv string) bool {
			return strings.HasPrefix(kv, "MAKEFLAGS=") || strings.HasPrefix(kv, "MFLAGS=") || strings.HasPrefix(kv, "MAKELEVEL=") ||
				strings.HasPrefix(kv, "GITHUB_") || strings.HasPrefix(kv, "CI_") || strings.HasPrefix(kv, "ACTIONS_ID_TOKEN_REQUEST_") || strings.HasPrefix(kv, "PATH=")
		}), "PATH="+filepath.Join(root, "bin")+string(os.PathListSeparator)+os.Getenv("PATH"),
			"CI_PR=7", "GITHUB_RUN_ID=8", "GITHUB_RUN_ATTEMPT=1", "GITHUB_STEP_SUMMARY="+summary)
		cmd.Env = append(cmd.Env, env...)

		combined, err := cmd.CombinedOutput()
		calls, _ := os.ReadFile(filepath.Join(k, "calls"))
		written, _ := os.ReadFile(summary)
		entries, dirErr := os.ReadDir(plans)
		require.NoError(t, dirErr, "レシピが作業ディレクトリを作っていません: %s", combined)

		left := make([]string, 0, len(entries))
		for _, e := range entries {
			left = append(left, e.Name())
		}

		return recipeResult{out: string(combined), calls: string(calls), summary: string(written), plans: plans, err: err, left: left}
	}

}

// TestCiRecipes は、ci.mk のレシピを偽物の道具で実際に走らせ、判定の失敗が apply と後続を止めることと、
// plan の全文を置いてから失敗することを見る。判定そのものは tf-local のテストが持ち、ここはレシピが
// その終了コードを握り潰さないことを固定する（ADR-0604 決定11、ADR-0702 決定24）。
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
}

func testCiApplyEntry(t *testing.T, recipe recipeFunc) {
	t.Helper()

	ok := map[string]struct{ planRC, plan string }{
		"変更が無くても保存した plan を apply する": {"0", recipeNoop},
		"変更があれば保存した plan を apply する":  {"2", recipeCreate},
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
			assert.Zero(t, countCalls(r.calls, "aws s3api put-object"), "apply は全文を Bucket に置かない")
			assert.Contains(t, initArgs(r.calls), "-backend-config=kms_key_id=arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab", "init に state の鍵を渡す")
			assert.NotContains(t, r.calls, "-lock=false")
			assert.Empty(t, r.left, "作業ファイルが残っています")
		})
	}

	fail := map[string]struct {
		knobs map[string]string
		want  string
	}{
		"plan が失敗すれば apply しない":         {map[string]string{"plan-rc": "1", "plan.json": recipeNoop, "policy.json": recipePass}, "plan が失敗しました"},
		"終了コードと JSON が食い違えば apply しない":  {map[string]string{"plan-rc": "0", "plan.json": recipeCreate, "policy.json": recipePass}, "食い違います"},
		"plan を JSON にできなければ apply しない": {map[string]string{"plan-rc": "2", "plan.json": "", "policy.json": recipePass}, "plan の JSON を解釈できません"},
		"Policy に違反すれば apply しない":       {map[string]string{"plan-rc": "2", "plan.json": recipeCreate, "policy.json": recipeViolated}, "[PLAN-STATE-01] stub"},
		"Policy が評価されなければ apply しない":    {map[string]string{"plan-rc": "2", "plan.json": recipeCreate, "policy.json": "[]"}, "Policy を通りません"},
		"conftest が何も出さなければ apply しない":  {map[string]string{"plan-rc": "2", "plan.json": recipeCreate, "policy.json": ""}, "Policy を通りません"},
		"評価の数が規則の数と違えば apply しない":       {map[string]string{"plan-rc": "2", "plan.json": recipeCreate, "policy.json": `[{"namespace":"plan.stateful_protection","successes":3}]`}, "一致しません"},
	}
	for name, c := range fail {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			r := recipe(t, "ci-apply-admin-identity", c.knobs)
			require.Error(t, r.err, r.out)
			assert.Contains(t, r.out, c.want)
			assert.Zero(t, applies(r.calls), r.calls)
			assert.NotContains(t, r.out, "✅ apply")
			assert.Empty(t, r.summary)
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
			assert.Empty(t, r.left, "作業ファイルが残っています")
		})
	}

	t.Run("init が失敗すれば plan を呼ばない", func(t *testing.T) {
		t.Parallel()

		r := recipe(t, "ci-apply-admin-identity", map[string]string{"init-rc": "1"})
		require.Error(t, r.err, r.out)
		assert.Contains(t, r.out, "init が失敗しました")
		assert.NotContains(t, r.calls, " plan ")
		assert.Empty(t, r.left, "作業ファイルが残っています")
	})

	t.Run("apply が失敗すれば非ゼロで終わり、成功を出さない", func(t *testing.T) {
		t.Parallel()

		r := recipe(t, "ci-apply-admin-identity", map[string]string{"plan-rc": "2", "plan.json": recipeCreate, "policy.json": recipePass, "apply-rc": "1"})
		require.Error(t, r.err, r.out)
		assert.Equal(t, 1, applies(r.calls), r.calls)
		assert.Contains(t, r.out, "apply が失敗しました")
		assert.NotContains(t, r.out, "✅ apply")
		assert.Empty(t, r.summary)
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

	t.Run("job summary が無くても成功する", func(t *testing.T) {
		t.Parallel()

		r := recipe(t, "ci-apply-admin-identity", map[string]string{"plan-rc": "2", "plan.json": recipeCreate, "policy.json": recipePass}, "GITHUB_STEP_SUMMARY=")
		require.NoError(t, r.err, r.out)
		assert.Equal(t, 1, applies(r.calls), r.calls)
		assert.Empty(t, r.summary)
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
		assert.Contains(t, r.calls, "--bucket plan-output-bucket --key pr/7/8-1-admin-identity.txt")
		assert.Contains(t, r.calls, "--expected-bucket-owner 123456789012")
		assert.Contains(t, r.calls, "plan -lock=false")
		assert.Contains(t, initArgs(r.calls), "-backend-config=kms_key_id=arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab", "init に state の鍵を渡す")
		assert.Zero(t, applies(r.calls), r.calls)
		assert.Contains(t, r.out, "plan の全文の版 ID: v-1")
		assert.Contains(t, r.summary, "- 版 ID: `v-1`")
		assert.NotContains(t, r.out, "PLAN-SECRET")
		assert.Empty(t, r.left, "作業ファイルが残っています")
	})

	fail := map[string]struct {
		knobs map[string]string
		want  string
	}{
		"plan が失敗しても全文を置いてから失敗する":         {map[string]string{"plan-rc": "1", "plan.json": recipeNoop, "policy.json": recipePass}, "全文は上の key にあります"},
		"Policy に違反すれば全文を置いてから失敗する":       {map[string]string{"plan-rc": "2", "plan.json": recipeCreate, "policy.json": recipeViolated}, "[PLAN-STATE-01] stub"},
		"plan を JSON にできなくても全文を置いてから失敗する": {map[string]string{"plan-rc": "2", "plan.json": "", "policy.json": recipePass}, "plan の JSON を解釈できません"},
		"Policy が評価されなくても全文を置いてから失敗する":    {map[string]string{"plan-rc": "2", "plan.json": recipeCreate, "policy.json": "[]"}, "Policy を通りません"},
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
		"plan の出力の Bucket の名前を読めなければ plan を呼ばない": {map[string]string{"output-rc": "1"}, nil, "Bucket の名前を state から読めません", false},
		"全文を置けなければ判定へ進まない":                       {map[string]string{"plan-rc": "2", "plan.json": recipeCreate, "policy.json": recipePass, "s3api-rc": "1"}, nil, "plan の全文: key=", true},
		"Pull Request の番号が無ければ全文を置かない":           {map[string]string{"plan-rc": "2", "plan.json": recipeCreate, "policy.json": recipePass}, []string{"CI_PR="}, "plan の出力の key を組めません", true},
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

func testCiCredentials(t *testing.T, recipe recipeFunc) {
	t.Helper()

	jwt := jwtWith(`{"sub":"repo:o/r:environment:admin-plan"}`)
	response := map[string]string{"oidc-response": `{"value":"` + jwt + `"}`}
	env := []string{
		"ACTIONS_ID_TOKEN_REQUEST_URL=https://t.actions.githubusercontent.com/x", "ACTIONS_ID_TOKEN_REQUEST_TOKEN=bearer",
		"CI_ACCOUNT_ID=123456789012",
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

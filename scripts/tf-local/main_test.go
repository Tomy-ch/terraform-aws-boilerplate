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
			"引数が無ければ失敗する":             nil,
			"知らないサブコマンドは失敗する":         {"apply"},
			"region に root が無ければ失敗する": {"region"},
			"region に余分な引数があれば失敗する":   {"region", "a", "b"},
			"env-unset に引数があれば失敗する":   {"env-unset", "x"},
			"plan-header に引数があれば失敗する": {"plan-header", "x"},
			"oidc-sub にファイルが無ければ失敗する": {"oidc-sub"},
			"oidc-sub に余分な引数があれば失敗する": {"oidc-sub", "a", "b"},
		}
		for name, args := range cases {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				var out bytes.Buffer
				require.ErrorIs(t, run(args, &out, testDeps(nil)), errUsage)
				assert.Empty(t, out.String())
			})
		}

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
			})
			require.NoError(t, err)
			assert.Equal(t, "-u AWS_ACCESS_KEY_ID -u AWS_CONTAINER_CREDENTIALS_FULL_URI -u AWS_PROFILE"+
				" -u TF_CLI_ARGS -u TF_CLI_ARGS_plan -u TF_CLI_CONFIG_FILE -u TF_DATA_DIR"+
				" -u TF_LOG -u TF_LOG_PATH -u TF_LOG_PROVIDER -u TF_REATTACH_PROVIDERS -u TF_VAR_a -u TF_WORKSPACE", got)
		})

		for name, environ := range map[string][]string{
			"外す変数が無ければ空にする":     {"HOME=/h", "TF_PLUGIN_CACHE_DIR=/c"},
			"環境が空なら空にする":        nil,
			"名前が前方一致するだけの変数は残す": {"MY_AWS_PROFILE=x", "TF_WORKSPACE_X=y", "TF_DATA_DIRS=z"},
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
// 展開結果で見る。レシピそのものが対象なので実物の local.mk を読む。
func TestLocalMk(t *testing.T) {
	t.Parallel()

	expand := func(t *testing.T, target string) string {
		t.Helper()

		cmd := exec.CommandContext(t.Context(), "make", "-C", filepath.Join("..", ".."), "-n", "--no-print-directory", target)
		cmd.Env = slices.DeleteFunc(os.Environ(), func(kv string) bool {
			return strings.HasPrefix(kv, "MAKEFLAGS=") || strings.HasPrefix(kv, "MFLAGS=") || strings.HasPrefix(kv, "MAKELEVEL=")
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
		assert.Equal(t, 2, strings.Count(got, envPrefix), got)
		assert.Contains(t, got, envPrefix+" aws sts get-caller-identity --region \"$region\" --query '[Account,Arn]' --output text")
		assert.Contains(t, got, `init -input=false -lockfile=readonly -backend-config="bucket=$bucket"`)
		assert.Contains(t, got, "bucket -project tabp -env admin")
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
		assert.Equal(t, 3, strings.Count(got, envPrefix), got)
	})

	t.Run("aws-login は絞った環境で bootstrap の profile にログインする", func(t *testing.T) {
		t.Parallel()

		got := expand(t, "aws-login")
		assert.Contains(t, got, "env $unset_env aws login --profile tabp-bootstrap")
	})
}

// TestCiMk は、ci.mk のレシピが CI の profile で plan し、terraform の出力を公開されるログに出さない
// 配線を、make -n の展開結果で見る。レシピそのものが対象なので実物の ci.mk を読む。
func TestCiMk(t *testing.T) {
	t.Parallel()

	expand := func(t *testing.T, target string) string {
		t.Helper()

		cmd := exec.CommandContext(t.Context(), "make", "-C", filepath.Join("..", ".."), "-n", "--no-print-directory", target)
		cmd.Env = slices.DeleteFunc(os.Environ(), func(kv string) bool {
			return strings.HasPrefix(kv, "MAKEFLAGS=") || strings.HasPrefix(kv, "MFLAGS=") || strings.HasPrefix(kv, "MAKELEVEL=")
		})

		out, err := cmd.CombinedOutput()
		require.NoError(t, err, string(out))

		return string(out)
	}

	const envPrefix = "env $unset_env AWS_PROFILE=tabp-admin-ci-plan TF_CLI_CONFIG_FILE=/dev/null AWS_EC2_METADATA_DISABLED=true CHECKPOINT_DISABLE=1"

	t.Run("plan は CI の profile だけで、lock を取らずに走り、terraform の出力をファイルへ書く", func(t *testing.T) {
		t.Parallel()

		got := expand(t, "ci-plan-admin-identity")
		assert.True(t, strings.HasPrefix(strings.TrimSpace(got), "set -eu -o pipefail; umask 077;"), got)
		assert.NotContains(t, got, "AWS_PROFILE=tabp-bootstrap")
		assert.Contains(t, got, envPrefix+" terraform -chdir=\"$dir\" init -input=false -no-color -lockfile=readonly")
		assert.Contains(t, got, `plan -lock=false -input=false -no-color -detailed-exitcode -out="$tfplan" >> "$out" 2>&1`)
		assert.Contains(t, got, `show -json "$tfplan" > "$json" 2>> "$out"`)
		assert.Contains(t, got, `--checksum-sha256 "$sha"`)
		assert.Contains(t, got, `plan-summary -exitcode "$rc" "$json"`)
		assert.NotContains(t, got, `tee "$log"`)
		assert.Equal(t, 1, strings.Count(got, "| tee"), "tee は commit の行だけに使う: "+got)
	})

	t.Run("資格情報は OIDC の token で1回だけ assume し、token を消す", func(t *testing.T) {
		t.Parallel()

		got := expand(t, "ci-aws-credentials")
		assert.Equal(t, 1, strings.Count(got, "assume-role-with-web-identity"), got)
		assert.Contains(t, got, "audience=sts.amazonaws.com")
		assert.Contains(t, got, "role-arn -project tabp -env admin -role plan")
		assert.Contains(t, got, `rm -f "$token"`)
		assert.Contains(t, got, "configure set --profile tabp-admin-ci-plan")
		assert.NotContains(t, got, "AWS_ACCESS_KEY_ID")
	})
}

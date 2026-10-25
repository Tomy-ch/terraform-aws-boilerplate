package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	identityUser = "123456789012\tarn:aws:iam::123456789012:user/bootstrap\n"
	backendOK    = "terraform {\n  backend \"s3\" {\n    key    = \"identity/terraform.tfstate\"\n    region = \"ap-northeast-1\"\n  }\n}\n"
)

func rootWith(t *testing.T, backend string) string {
	t.Helper()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "backend.tf"), []byte(backend), 0o600))

	return dir
}

func Test_run(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("region は backend.tf の region を1行で出す", func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			require.NoError(t, run([]string{"region", rootWith(t, backendOK)}, strings.NewReader(""), &out, os.ReadFile))
			assert.Equal(t, "ap-northeast-1\n", out.String())
		})

		t.Run("bucket は標準入力の資格情報から Bucket 名を1行で出す", func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			args := []string{"bucket", "-project", "tabp", "-env", "admin", "-region", "ap-northeast-1"}
			require.NoError(t, run(args, strings.NewReader(identityUser), &out, os.ReadFile))
			assert.Equal(t, "tabp-admin-tfstate-123456789012-ap-northeast-1-an\n", out.String())
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		cases := map[string][]string{
			"引数が無ければ失敗する":             nil,
			"知らないサブコマンドは失敗する":         {"apply"},
			"region に root が無ければ失敗する": {"region"},
			"region に余分な引数があれば失敗する":   {"region", "a", "b"},
		}
		for name, args := range cases {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				var out bytes.Buffer
				require.ErrorIs(t, run(args, strings.NewReader(""), &out, os.ReadFile), errUsage)
				assert.Empty(t, out.String())
			})
		}

		t.Run("値を決められなければ何も出力しない", func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			args := []string{"bucket", "-project", "tabp", "-env", "admin", "-region", "ap-northeast-1"}
			require.ErrorIs(t, run(args, strings.NewReader(""), &out, os.ReadFile), errIdentity)
			assert.Empty(t, out.String())
		})
	})
}

func Test_runBucket(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("フラグと標準入力から Bucket 名を組む", func(t *testing.T) {
			t.Parallel()

			got, err := runBucket([]string{"-project", "tabp", "-env", "admin", "-region", "ap-northeast-1"}, strings.NewReader(identityUser))
			require.NoError(t, err)
			assert.Equal(t, "tabp-admin-tfstate-123456789012-ap-northeast-1-an", got)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		t.Run("知らないフラグは失敗する", func(t *testing.T) {
			t.Parallel()

			_, err := runBucket([]string{"-target", "x"}, strings.NewReader(identityUser))
			require.ErrorIs(t, err, errUsage)
		})

		t.Run("余分な引数は失敗する", func(t *testing.T) {
			t.Parallel()

			_, err := runBucket([]string{"-project", "tabp", "-env", "admin", "-region", "ap-northeast-1", "x"}, strings.NewReader(identityUser))
			require.ErrorIs(t, err, errUsage)
		})

		t.Run("フラグを欠くと Bucket 名を組まない", func(t *testing.T) {
			t.Parallel()

			_, err := runBucket([]string{"-env", "admin", "-region", "ap-northeast-1"}, strings.NewReader(identityUser))
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

			got, err := rootRegion(rootWith(t, backendOK), os.ReadFile)
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

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

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
			"空なら失敗する":        "",
			"11桁なら失敗する":      "12345678901\tarn:aws:iam::12345678901:user/b\n",
			"13桁なら失敗する":      "1234567890123\tarn:aws:iam::1234567890123:user/b\n",
			"Arn が無ければ失敗する":  "123456789012\n",
			"フィールドが多ければ失敗する": "123456789012\tarn:aws:iam::123456789012:user/b\textra\n",
			"数字でなければ失敗する":    "None\tNone\n",
		}
		for name, identity := range cases {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				_, err := callerAccount(identity)
				require.ErrorIs(t, err, errIdentity)
			})
		}

		t.Run("root の資格情報は拒む", func(t *testing.T) {
			t.Parallel()

			_, err := callerAccount("123456789012\tarn:aws:iam::123456789012:root\n")
			require.ErrorIs(t, err, errRootCredentials)
		})
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

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for name, args := range map[string][2]string{
			"project が空なら失敗する":      {"", "admin"},
			"env が空なら失敗する":          {"tabp", ""},
			"project にハイフンがあれば失敗する": {"ta-bp", "admin"},
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				_, err := bucketName(args[0], args[1], "123456789012", "ap-northeast-1")
				require.ErrorIs(t, err, errToken)
			})
		}

		t.Run("region の形が誤っていれば失敗する", func(t *testing.T) {
			t.Parallel()

			_, err := bucketName("tabp", "admin", "123456789012", "Tokyo")
			require.ErrorIs(t, err, errRegionMalformed)
		})
	})
}

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	stateKeyID  = "1234abcd-12ab-34cd-56ef-1234567890ab"
	stateKeyARN = "arn:aws:kms:ap-northeast-1:123456789012:key/" + stateKeyID
	otherKeyARN = "arn:aws:kms:ap-northeast-1:123456789012:key/abcdef01-2345-6789-abcd-ef0123456789"
)

func aliasesJSON(entries ...string) string {
	return `{"Aliases":[` + strings.Join(entries, ",") + `]}`
}

func aliasEntry(name, arn, target string) string {
	return fmt.Sprintf(`{"AliasName":%q,"AliasArn":%q,"TargetKeyId":%q}`, name, arn, target)
}

var (
	stateAlias   = aliasEntry("alias/tabp-admin-tfstate", "arn:aws:kms:ap-northeast-1:123456789012:alias/tabp-admin-tfstate", stateKeyID)
	managedAlias = `{"AliasName":"alias/aws/s3","AliasArn":"arn:aws:kms:ap-northeast-1:123456789012:alias/aws/s3"}`
	withAlias    = aliasesJSON(managedAlias, stateAlias)
	withoutAlias = aliasesJSON(managedAlias)
	encAES256    = `{"ServerSideEncryptionConfiguration":{"Rules":[{"ApplyServerSideEncryptionByDefault":{"SSEAlgorithm":"AES256"},"BucketKeyEnabled":false}]}}`
)

func encKMS(key string) string {
	return `{"ServerSideEncryptionConfiguration":{"Rules":[{"ApplyServerSideEncryptionByDefault":{"SSEAlgorithm":"aws:kms","KMSMasterKeyID":"` + key + `"},"BucketKeyEnabled":false}]}}`
}

func Test_runStateKey(t *testing.T) {
	t.Parallel()

	args := func(extra ...string) []string {
		return append([]string{"-project", "tabp", "-env", "admin", "-region", "ap-northeast-1", "-identity", identityUser,
			"-aliases", withAlias, "-encryption", encAES256}, extra...)
	}

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("フラグから鍵の ARN を渡す引数を組む", func(t *testing.T) {
			t.Parallel()

			got, err := runStateKey(args())
			require.NoError(t, err)
			assert.Equal(t, "-backend-config=kms_key_id="+stateKeyARN, got)
		})

		t.Run("-missing sse-s3 を受け、鍵が無く AES256 なら鍵を渡さない", func(t *testing.T) {
			t.Parallel()

			got, err := runStateKey([]string{"-project", "tabp", "-env", "admin", "-region", "ap-northeast-1", "-identity", identityUser,
				"-aliases", withoutAlias, "-encryption", encAES256, "-missing", "sse-s3"})
			require.NoError(t, err)
			assert.Empty(t, got)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for name, a := range map[string][]string{
			"知らないフラグは失敗する": {"-target", "x"},
			"余分な引数は失敗する":   args("x"),
			"値の無いフラグは失敗する": {"-aliases"},
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				_, err := runStateKey(a)
				require.ErrorIs(t, err, errUsage)
			})
		}

		t.Run("root の資格情報では組まない", func(t *testing.T) {
			t.Parallel()

			_, err := runStateKey([]string{"-project", "tabp", "-env", "admin", "-region", "ap-northeast-1",
				"-identity", "123456789012\tarn:aws:iam::123456789012:root\n", "-aliases", withAlias, "-encryption", encAES256})
			require.ErrorIs(t, err, errRootCredentials)
			assert.NotContains(t, err.Error(), "123456789012")
		})

		t.Run("-missing を省くと fail として扱う", func(t *testing.T) {
			t.Parallel()

			_, err := runStateKey([]string{"-project", "tabp", "-env", "admin", "-region", "ap-northeast-1", "-identity", identityUser,
				"-aliases", withoutAlias, "-encryption", encAES256})
			require.ErrorIs(t, err, errStateKeyMissing)
		})

		t.Run("-identity を欠くと組まない", func(t *testing.T) {
			t.Parallel()

			_, err := runStateKey([]string{"-project", "tabp", "-env", "admin", "-region", "ap-northeast-1", "-aliases", withAlias, "-encryption", encAES256})
			require.ErrorIs(t, err, errIdentity)
		})
	})
}

func Test_stateKey(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		for name, tc := range map[string]struct {
			aliases, encryption, missing, want string
		}{
			"alias あり・AES256・fail は鍵を渡す":         {withAlias, encAES256, missingFail, "-backend-config=kms_key_id=" + stateKeyARN},
			"alias あり・AES256・sse-s3 も鍵を渡す":       {withAlias, encAES256, missingSSES3, "-backend-config=kms_key_id=" + stateKeyARN},
			"alias あり・同じ鍵の SSE-KMS・fail は鍵を渡す":   {withAlias, encKMS(stateKeyARN), missingFail, "-backend-config=kms_key_id=" + stateKeyARN},
			"alias あり・同じ鍵の SSE-KMS・sse-s3 も鍵を渡す": {withAlias, encKMS(stateKeyARN), missingSSES3, "-backend-config=kms_key_id=" + stateKeyARN},
			"alias なし・AES256・sse-s3 だけ鍵を渡さない":    {withoutAlias, encAES256, missingSSES3, ""},
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				got, err := stateKey("tabp", "admin", "ap-northeast-1", "123456789012", []byte(tc.aliases), []byte(tc.encryption), tc.missing)
				require.NoError(t, err)
				assert.Equal(t, tc.want, got)
			})
		}

		t.Run("env が違えば、その env の alias から鍵を組む", func(t *testing.T) {
			t.Parallel()

			aliases := aliasesJSON(stateAlias, aliasEntry("alias/tabp-dev-tfstate", "arn:aws:kms:ap-northeast-1:123456789012:alias/tabp-dev-tfstate", "abcdef01-2345-6789-abcd-ef0123456789"))
			got, err := stateKey("tabp", "dev", "ap-northeast-1", "123456789012", []byte(aliases), []byte(encAES256), missingFail)
			require.NoError(t, err)
			assert.Equal(t, "-backend-config=kms_key_id="+otherKeyARN, got)
		})

		t.Run("partition は alias の ARN から取る", func(t *testing.T) {
			t.Parallel()

			aliases := aliasesJSON(aliasEntry("alias/tabp-admin-tfstate", "arn:aws-us-gov:kms:us-gov-west-1:123456789012:alias/tabp-admin-tfstate", stateKeyID))
			got, err := stateKey("tabp", "admin", "us-gov-west-1", "123456789012", []byte(aliases), []byte(encAES256), missingFail)
			require.NoError(t, err)
			assert.Equal(t, "-backend-config=kms_key_id=arn:aws-us-gov:kms:us-gov-west-1:123456789012:key/"+stateKeyID, got)
		})

	})

	// 写しと正本のずれを見るので、正本である実物を読む（ADR-0702 決定17 の但し書き）。
	t.Run("写しと正本のずれ", func(t *testing.T) {
		t.Parallel()

		t.Run("alias の名前が env/admin/identity のテストの直書きと一致する", func(t *testing.T) {
			t.Parallel()

			tftest, err := os.ReadFile(filepath.Join("..", "..", "env", "admin", "identity", "tests", "imports.tftest.hcl"))
			require.NoError(t, err)
			assert.Contains(t, string(tftest), `== "`+fmt.Sprintf(stateKeyAliasFormat, "tabp", "admin")+`"`)
		})

		t.Run("鍵 ID の形が deployment-identity の state_key_arn の validation と一致する", func(t *testing.T) {
			t.Parallel()

			tf, err := os.ReadFile(filepath.Join("..", "..", "usecases", "deployment-identity", "variables.tf"))
			require.NoError(t, err)
			assert.Contains(t, string(tf), `:key/`+strings.TrimPrefix(keyIDPattern.String(), "^"))
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for name, tc := range map[string]struct {
			aliases, encryption, missing string
			wantErr                      error
		}{
			"alias なし・AES256・fail は失敗する":             {withoutAlias, encAES256, missingFail, errStateKeyMissing},
			"alias なし・SSE-KMS・fail は失敗する":            {withoutAlias, encKMS(stateKeyARN), missingFail, errStateKeyMissing},
			"alias なし・SSE-KMS・sse-s3 も失敗する":          {withoutAlias, encKMS(stateKeyARN), missingSSES3, errStateKeyMissing},
			"alias あり・違う鍵の SSE-KMS・sse-s3 も失敗する":     {withAlias, encKMS(otherKeyARN), missingSSES3, errStateKeyMismatch},
			"既定が鍵を名指さない SSE-KMS（aws/s3）なら失敗する":       {withAlias, encKMS(""), missingFail, errStateKeyMismatch},
			"既定の鍵が鍵 ID だけで書かれていれば一致と見ない":             {withAlias, encKMS(stateKeyID), missingFail, errStateKeyMismatch},
			"既定の鍵が alias の ARN なら一致と見ない":             {withAlias, encKMS("arn:aws:kms:ap-northeast-1:123456789012:alias/tabp-admin-tfstate"), missingFail, errStateKeyMismatch},
			"別の env の alias だけなら state の鍵と見ない":       {aliasesJSON(aliasEntry("alias/tabp-dev-tfstate", "arn:aws:kms:ap-northeast-1:123456789012:alias/tabp-dev-tfstate", stateKeyID)), encAES256, missingFail, errStateKeyMissing},
			"alias の一覧に Aliases が無ければ sse-s3 でも通さない": {`{}`, encAES256, missingSSES3, errStateKeyAliases},
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				_, err := stateKey("tabp", "admin", "ap-northeast-1", "123456789012", []byte(tc.aliases), []byte(tc.encryption), tc.missing)
				require.ErrorIs(t, err, tc.wantErr)
			})
		}

		t.Run("Bucket の既定の鍵が alias の鍵と違えば失敗する", func(t *testing.T) {
			t.Parallel()

			_, err := stateKey("tabp", "admin", "ap-northeast-1", "123456789012", []byte(withAlias), []byte(encKMS(otherKeyARN)), missingFail)
			require.ErrorIs(t, err, errStateKeyMismatch)
			assert.NotContains(t, err.Error(), "123456789012")
			assert.NotContains(t, err.Error(), stateKeyID)
		})

		t.Run("-missing が知らない値なら失敗する", func(t *testing.T) {
			t.Parallel()

			_, err := stateKey("tabp", "admin", "ap-northeast-1", "123456789012", []byte(withAlias), []byte(encAES256), "skip")
			require.ErrorIs(t, err, errStateKeyMissingArg)
		})

		t.Run("project の形が不正なら失敗する", func(t *testing.T) {
			t.Parallel()

			_, err := stateKey("tabp-x", "admin", "ap-northeast-1", "123456789012", []byte(withAlias), []byte(encAES256), missingFail)
			require.ErrorIs(t, err, errToken)
		})

		t.Run("env の形が不正なら失敗する", func(t *testing.T) {
			t.Parallel()

			_, err := stateKey("tabp", "ad-min", "ap-northeast-1", "123456789012", []byte(withAlias), []byte(encAES256), missingFail)
			require.ErrorIs(t, err, errToken)
		})

		t.Run("region の形が不正なら失敗する", func(t *testing.T) {
			t.Parallel()

			_, err := stateKey("tabp", "admin", "tokyo", "123456789012", []byte(withAlias), []byte(encAES256), missingFail)
			require.ErrorIs(t, err, errRegionMalformed)
		})

		t.Run("既定の暗号化を読めなければ、alias が無くても sse-s3 で通さない", func(t *testing.T) {
			t.Parallel()

			_, err := stateKey("tabp", "admin", "ap-northeast-1", "123456789012", []byte(withoutAlias), []byte(""), missingSSES3)
			require.ErrorIs(t, err, errStateKeyEncryption)
		})

		t.Run("alias の一覧を読めなければ失敗する", func(t *testing.T) {
			t.Parallel()

			_, err := stateKey("tabp", "admin", "ap-northeast-1", "123456789012", []byte(""), []byte(encAES256), missingSSES3)
			require.ErrorIs(t, err, errStateKeyAliases)
		})
	})
}

func Test_defaultEncryption(t *testing.T) {
	t.Parallel()

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("AES256 は鍵を返さない", func(t *testing.T) {
			t.Parallel()

			algorithm, key, err := defaultEncryption([]byte(encAES256))
			require.NoError(t, err)
			assert.Equal(t, "AES256", algorithm)
			assert.Empty(t, key)
		})

		t.Run("aws:kms は鍵を返す", func(t *testing.T) {
			t.Parallel()

			algorithm, key, err := defaultEncryption([]byte(encKMS(stateKeyARN)))
			require.NoError(t, err)
			assert.Equal(t, "aws:kms", algorithm)
			assert.Equal(t, stateKeyARN, key)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for name, data := range map[string]string{
			"JSON でなければ失敗する":     "not json",
			"規則が無ければ失敗する":        `{"ServerSideEncryptionConfiguration":{"Rules":[]}}`,
			"規則が2つなら失敗する":        `{"ServerSideEncryptionConfiguration":{"Rules":[{"ApplyServerSideEncryptionByDefault":{"SSEAlgorithm":"AES256"}},{"ApplyServerSideEncryptionByDefault":{"SSEAlgorithm":"AES256"}}]}}`,
			"aws:kms:dsse は受けない": `{"ServerSideEncryptionConfiguration":{"Rules":[{"ApplyServerSideEncryptionByDefault":{"SSEAlgorithm":"aws:kms:dsse"}}]}}`,
			"方式が空なら失敗する":         `{"ServerSideEncryptionConfiguration":{"Rules":[{}]}}`,
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				_, _, err := defaultEncryption([]byte(data))
				require.ErrorIs(t, err, errStateKeyEncryption)
			})
		}
	})
}

func Test_aliasKeyARN(t *testing.T) {
	t.Parallel()

	const name = "alias/tabp-admin-tfstate"

	t.Run("正常系", func(t *testing.T) {
		t.Parallel()

		t.Run("alias が指す鍵の ARN を組む", func(t *testing.T) {
			t.Parallel()

			got, found, err := aliasKeyARN(name, "ap-northeast-1", "123456789012", []byte(withAlias))
			require.NoError(t, err)
			assert.True(t, found)
			assert.Equal(t, stateKeyARN, got)
		})

		t.Run("alias が無ければ見つからないと返す", func(t *testing.T) {
			t.Parallel()

			_, found, err := aliasKeyARN(name, "ap-northeast-1", "123456789012", []byte(withoutAlias))
			require.NoError(t, err)
			assert.False(t, found)
		})
	})

	t.Run("異常系", func(t *testing.T) {
		t.Parallel()

		for name2, data := range map[string]string{
			"JSON でなければ失敗する":                "not json",
			"一覧が途中で切れていれば失敗する":              `{"Aliases":[],"Truncated":true}`,
			"Aliases が無ければ失敗する":             `{}`,
			"Aliases が null なら失敗する":         `{"Aliases":null}`,
			"一覧が null なら失敗する":               `null`,
			"alias の ARN が無ければ失敗する":         aliasesJSON(aliasEntry(name, "", stateKeyID)),
			"alias の ARN が ARN の形でなければ失敗する": aliasesJSON(aliasEntry(name, "alias/tabp-admin-tfstate", stateKeyID)),
			"別のアカウントの alias は受けない":          aliasesJSON(aliasEntry(name, "arn:aws:kms:ap-northeast-1:210987654321:alias/tabp-admin-tfstate", stateKeyID)),
			"別の region の alias は受けない":       aliasesJSON(aliasEntry(name, "arn:aws:kms:us-east-1:123456789012:alias/tabp-admin-tfstate", stateKeyID)),
			"ARN の名前が違えば受けない":               aliasesJSON(aliasEntry(name, "arn:aws:kms:ap-northeast-1:123456789012:alias/other", stateKeyID)),
			"多リージョンの鍵は受けない":                 aliasesJSON(aliasEntry(name, "arn:aws:kms:ap-northeast-1:123456789012:alias/tabp-admin-tfstate", "mrk-1234abcd12ab34cd56ef1234567890ab")),
			"鍵 ID が無ければ失敗する":                aliasesJSON(aliasEntry(name, "arn:aws:kms:ap-northeast-1:123456789012:alias/tabp-admin-tfstate", "")),
		} {
			t.Run(name2, func(t *testing.T) {
				t.Parallel()

				_, _, err := aliasKeyARN(name, "ap-northeast-1", "123456789012", []byte(data))
				require.ErrorIs(t, err, errStateKeyAliases)
				assert.NotContains(t, err.Error(), "123456789012")
			})
		}
	})
}

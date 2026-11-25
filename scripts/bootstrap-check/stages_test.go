package main

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 段ごとの desc を、表と独立に書いたもの（ADR-0702 決定15）。順序も出力の契約である。
var (
	stage1Descs = []string{
		"Public Access Block の4項目が true",
		"所有権が BucketOwnerEnforced",
		"版管理が Enabled",
		"既定の暗号化が AES256 で SSE-C を拒否（違えば段2 へ進まず PR に記録して止まる）",
	}
	beforeApplyDescs = []string{
		"OIDC provider（token.actions.githubusercontent.com）がまだ無い",
		"role tabp-admin-ci-plan がまだ無い",
		"role tabp-admin-ci-apply がまだ無い",
		"role tabp-admin-ci-apply-identity がまだ無い",
		"role tabp-admin-ci-unlock-identity がまだ無い",
		"role tabp-admin-ci-unlock がまだ無い",
		"state Bucket に Bucket Policy がまだ無い",
		"state Bucket に lifecycle がまだ無い",
		"state の鍵の alias（alias/tabp-admin-tfstate）がまだ無い",
		"組織がまだ無い",
		"IAM user break-glass-1 がまだ無い",
		"IAM user break-glass-2 がまだ無い",
		"IAM group break-glass がまだ無い",
		"role tabp-break-glass がまだ無い",
		"StackSet tabp-break-glass がまだ無い",
		"policy tabp-admin-ci-apply-identity-governance がまだ無い",
		"policy tabp-admin-ci-apply-identity-governance-2 がまだ無い",
		"policy tabp-admin-ci-apply-identity-manage がまだ無い",
		"policy tabp-admin-ci-apply-workload がまだ無い",
		"policy tabp-admin-ci-apply-workload-read がまだ無い",
		"policy tabp-admin-ci-boundary-ops-notification がまだ無い",
		"account password policy がまだ無い",
	}
	encryptionDescs = []string{
		testBucket + " の既定の暗号化が state の鍵の SSE-KMS（Bucket Key なし、SSE-C を拒否）",
		testPlanBucket + " の既定の暗号化が state の鍵の SSE-KMS（Bucket Key なし、SSE-C を拒否）",
		testApplyBucket + " の既定の暗号化が state の鍵の SSE-KMS（Bucket Key なし、SSE-C を拒否）",
	}
	noExecutorDesc = "Bucket Policy に IAM user が無い（段3 で段4 の実行者を例外に入れていない）"
	executorDesc   = "Bucket Policy の書き込みの例外（DenyObjectWriteByOthers）に、段4 の実行者 bob が在る"
	lifecycleDesc  = "lifecycle が規則1つ（版の保持 90 日、delete marker の掃除、未完了の multipart の中止 1 日）"
	keyPolicyDesc  = "Key Policy が、アカウントへの委任・plan と apply-identity と apply の読み書きする object に限った S3 経由の暗号操作・plan の state の復号・unlock-identity と unlock のそれぞれの lock file の復号の7文だけ"
	stage5Descs    = append(append([]string{}, encryptionDescs...),
		"identity root の state が state の鍵の SSE-KMS で書かれている",
		"Bucket Policy の例外に IAM user（段4 の実行者）が無い",
	)
)

// stage4Descs は stage4 の desc。executor は Bucket Policy の例外の確認の desc。
func stage4Descs(executor string) []string {
	return append(append([]string{}, encryptionDescs...),
		"OIDC provider の ClientIDList が sts.amazonaws.com だけ",
		"role tabp-admin-ci-plan の inline policy が plan だけ",
		"role tabp-admin-ci-plan に付く managed policy が、workload の読み取りの policy だけ",
		"role tabp-admin-ci-apply の inline policy が apply だけ",
		"role tabp-admin-ci-apply に付く managed policy が、workload の3つの policy だけ",
		"role tabp-admin-ci-apply-identity の inline policy が apply_identity だけ",
		"role tabp-admin-ci-apply-identity に付く managed policy が、統制の policy と委任管理者の policy と manage の policy だけ",
		"role tabp-admin-ci-unlock-identity の inline policy が unlock_identity だけ",
		"role tabp-admin-ci-unlock-identity に managed policy が付いていない",
		"role tabp-admin-ci-unlock の inline policy が unlock だけ",
		"role tabp-admin-ci-unlock に managed policy が付いていない",
		"state Bucket の Bucket Policy の文がすべて Deny で、TLS でない要求を拒む文が在る",
		executor,
		lifecycleDesc,
		"state の鍵が有効な対称鍵（単一リージョン、AWS_KMS）",
		"組織の FeatureSet が ALL",
		"IAM user break-glass-1 がアクセスキーを持たない",
		"IAM user break-glass-2 がアクセスキーを持たない",
		"role tabp-break-glass の信頼条件が MFA を求める",
		"state の鍵の自動 rotation が 365 日",
		keyPolicyDesc,
	)
}

const (
	stage1Count      = 4
	beforeApplyCount = 22
	stage4Count      = 24
	stage5Count      = 5

	describeKeyBody   = `{"KeyMetadata":{"Arn":"` + testKeyARN + `","KeyState":"Enabled","KeySpec":"SYMMETRIC_DEFAULT","KeyUsage":"ENCRYPT_DECRYPT","MultiRegion":false,"Origin":"AWS_KMS"}}`
	kmsEncryptionBody = `{"ServerSideEncryptionConfiguration":{"Rules":[{"ApplyServerSideEncryptionByDefault":{"SSEAlgorithm":"aws:kms","KMSMasterKeyID":"` +
		testKeyARN + `"},"BucketKeyEnabled":false,"BlockedEncryptionTypes":{"EncryptionType":["SSE-C"]}}]}}`
	stateBucketPolicy = `{"Version":"2012-10-17","Statement":[` +
		`{"Sid":"DenyInsecureTransport","Effect":"Deny","Principal":"*","Action":"s3:*","Resource":"*","Condition":{"Bool":{"aws:SecureTransport":"false"}}},` +
		`{"Sid":"DenyObjectWriteByOthers","Effect":"Deny","Principal":"*","Action":"s3:PutObject","Resource":"*",` +
		`"Condition":{"ArnNotEquals":{"aws:PrincipalArn":["arn:aws:iam::123456789012:role/tabp-admin-ci-apply"]}}}]}`
	lifecycleBody = `{"Rules":[{"ID":"x","Status":"Enabled","Filter":{},"Expiration":{"ExpiredObjectDeleteMarker":true},` +
		`"NoncurrentVersionExpiration":{"NoncurrentDays":90},"AbortIncompleteMultipartUpload":{"DaysAfterInitiation":1}}]}`
	retentionFile = "locals {\n  state_noncurrent_version_retention_days = 90 # 日\n}\n"
)

func stage1Responses() map[string]awsResult {
	return map[string]awsResult{
		s3Key("get-public-access-block", testBucket):       okResult(`{"PublicAccessBlockConfiguration":{"BlockPublicAcls":true,"IgnorePublicAcls":true,"BlockPublicPolicy":true,"RestrictPublicBuckets":true}}`),
		s3Key("get-bucket-ownership-controls", testBucket): okResult(`{"OwnershipControls":{"Rules":[{"ObjectOwnership":"BucketOwnerEnforced"}]}}`),
		s3Key("get-bucket-versioning", testBucket):         okResult(`{"Status":"Enabled"}`),
		s3Key("get-bucket-encryption", testBucket): okResult(`{"ServerSideEncryptionConfiguration":{"Rules":[{"ApplyServerSideEncryptionByDefault":{"SSEAlgorithm":"AES256"},` +
			`"BucketKeyEnabled":false,"BlockedEncryptionTypes":{"EncryptionType":["SSE-C"]}}]}}`),
	}
}

func beforeApplyResponses() map[string]awsResult {
	r := map[string]awsResult{
		jsonKey("iam", "list-open-id-connect-providers"):                                        okResult(`{"OpenIDConnectProviderList":[{"Arn":"arn:aws:iam::123456789012:oidc-provider/example.com"}]}`),
		s3Key("get-bucket-policy", testBucket):                                                  awsError("NoSuchBucketPolicy"),
		s3Key("get-bucket-lifecycle-configuration", testBucket):                                 awsError("NoSuchLifecycleConfiguration"),
		jsonKey("kms", "list-aliases"):                                                          okResult(`{"Aliases":[{"AliasName":"alias/aws/s3"},{"AliasName":"alias/tabp-admin-tfstate-old"}]}`),
		jsonKey("organizations", "describe-organization"):                                       awsError("AWSOrganizationsNotInUseException"),
		jsonKey("iam", "get-group", "--group-name", "break-glass"):                              awsError("NoSuchEntity"),
		jsonKey("iam", "get-role", "--role-name", "tabp-break-glass"):                           awsError("NoSuchEntity"),
		jsonKey("iam", "get-account-password-policy"):                                           awsError("NoSuchEntity"),
		jsonKey("cloudformation", "describe-stack-set", "--stack-set-name", "tabp-break-glass"): awsError("StackSetNotFoundException"),
	}

	for _, role := range []string{"plan", "apply", "apply-identity", "unlock-identity", "unlock"} {
		r[jsonKey("iam", "get-role", "--role-name", "tabp-admin-ci-"+role)] = awsError("NoSuchEntity")
	}

	for _, u := range []string{"break-glass-1", "break-glass-2"} {
		r[jsonKey("iam", "get-user", "--user-name", u)] = awsError("NoSuchEntity")
	}

	for _, p := range []string{"apply-identity-governance", "apply-identity-governance-2", "apply-identity-manage", "apply-workload", "apply-workload-read", "boundary-ops-notification"} {
		r[jsonKey("iam", "get-policy", "--policy-arn", "arn:aws:iam::123456789012:policy/tabp-admin-ci-"+p)] = awsError("NoSuchEntity")
	}

	return r
}

func encryptionResponses() map[string]awsResult {
	return map[string]awsResult{
		jsonKey("kms", "describe-key", "--key-id", testAlias): okResult(describeKeyBody),
		s3Key("get-bucket-encryption", testBucket):            okResult(kmsEncryptionBody),
		s3Key("get-bucket-encryption", testPlanBucket):        okResult(kmsEncryptionBody),
		s3Key("get-bucket-encryption", testApplyBucket):       okResult(kmsEncryptionBody),
	}
}

// keyPolicyDoc は、state の鍵の Key Policy の7文。
func keyPolicyDoc() string {
	role := func(r string) string { return `"arn:aws:iam::123456789012:role/tabp-admin-ci-` + r + `"` }
	sb, pb, ab := "arn:aws:s3:::"+testBucket, "arn:aws:s3:::"+testPlanBucket, "arn:aws:s3:::"+testApplyBucket
	stmt := func(principal, actions, op, objects string) string {
		context := `"kms:EncryptionContext:aws:s3:arn":` + objects
		via := `"kms:ViaService":"s3.ap-northeast-1.amazonaws.com"`
		condition := `"` + op + `":{` + context + `},"StringEquals":{` + via + `}`

		if op == "StringEquals" {
			condition = `"StringEquals":{` + context + `,` + via + `}`
		}

		return `{"Effect":"Allow","Principal":{"AWS":` + principal + `},"Action":` + actions + `,"Resource":"*","Condition":{` + condition + `}}`
	}
	rw := `["kms:GenerateDataKey","kms:Decrypt"]`

	return `{"Version":"2012-10-17","Statement":[` +
		`{"Sid":"root","Effect":"Allow","Principal":{"AWS":"arn:aws:iam::123456789012:root"},"Action":"kms:*","Resource":"*"},` +
		stmt(role("plan"), rw, "StringLike", `["`+pb+`/branch/*","`+pb+`/pr/*"]`) + `,` +
		stmt("["+role("plan")+"]", `"kms:Decrypt"`, "StringEquals", `["`+sb+`/identity/terraform.tfstate","`+sb+`/workload/terraform.tfstate"]`) + `,` +
		stmt(role("apply-identity"), rw, "StringLike", `["`+sb+`/identity/terraform.tfstate","`+sb+`/identity/terraform.tfstate.tflock","`+pb+`/apply/*"]`) + `,` +
		stmt(role("apply"), rw, "StringLike", `["`+ab+`/*","`+sb+`/workload/terraform.tfstate","`+sb+`/workload/terraform.tfstate.tflock"]`) + `,` +
		stmt(role("unlock-identity"), `["kms:Decrypt"]`, "StringEquals", `"`+sb+`/identity/terraform.tfstate.tflock"`) + `,` +
		stmt(role("unlock"), `"kms:Decrypt"`, "StringEquals", `["`+sb+`/workload/terraform.tfstate.tflock"]`) +
		`]}`
}

func stage4Responses(t *testing.T, bucketPolicy string) map[string]awsResult {
	t.Helper()

	r := encryptionResponses()

	r[jsonKey("iam", "get-open-id-connect-provider", "--open-id-connect-provider-arn", "arn:aws:iam::123456789012:oidc-provider/token.actions.githubusercontent.com")] =
		okResult(`{"Url":"token.actions.githubusercontent.com","ClientIDList":["sts.amazonaws.com"]}`)

	attached := map[string]string{
		"apply-identity": `{"AttachedPolicies":[{"PolicyName":"tabp-admin-ci-apply-identity-manage"},{"PolicyName":"tabp-admin-ci-apply-identity-governance"},{"PolicyName":"tabp-admin-ci-apply-identity-governance-2"}]}`,
		"apply":          `{"AttachedPolicies":[{"PolicyName":"tabp-admin-ci-apply-workload-read"},{"PolicyName":"tabp-admin-ci-apply-workload-2"},{"PolicyName":"tabp-admin-ci-apply-workload"}]}`,
		"plan":           `{"AttachedPolicies":[{"PolicyName":"tabp-admin-ci-apply-workload-read"}]}`,
	}

	for _, role := range []string{"plan", "apply", "apply-identity", "unlock-identity", "unlock"} {
		r[jsonKey("iam", "list-role-policies", "--role-name", "tabp-admin-ci-"+role)] = okResult(`{"PolicyNames":["` + strings.ReplaceAll(role, "-", "_") + `"]}`)

		body, ok := attached[role]
		if !ok {
			body = `{"AttachedPolicies":[]}`
		}

		r[jsonKey("iam", "list-attached-role-policies", "--role-name", "tabp-admin-ci-"+role)] = okResult(body)
	}

	r[s3Key("get-bucket-policy", testBucket)] = policyResult(t, bucketPolicy)
	r[s3Key("get-bucket-lifecycle-configuration", testBucket)] = okResult(lifecycleBody)
	r[jsonKey("organizations", "describe-organization")] = okResult(`{"Organization":{"FeatureSet":"ALL"}}`)

	for _, u := range []string{"break-glass-1", "break-glass-2"} {
		r[jsonKey("iam", "list-access-keys", "--user-name", u)] = okResult(`{"AccessKeyMetadata":[]}`)
	}

	r[jsonKey("iam", "get-role", "--role-name", "tabp-break-glass")] = okResult(`{"Role":{"AssumeRolePolicyDocument":{"Statement":[` +
		`{"Effect":"Allow","Condition":{"Bool":{"aws:MultiFactorAuthPresent":"true"}}},{"Effect":"Allow","Condition":{"Bool":{"aws:MultiFactorAuthPresent":"true"}}}]}}}`)
	r[jsonKey("kms", "get-key-rotation-status", "--key-id", testAlias)] = okResult(`{"KeyRotationEnabled":true,"RotationPeriodInDays":365}`)
	r[jsonKey("kms", "get-key-policy", "--key-id", testAlias, "--policy-name", "default")] = policyResult(t, keyPolicyDoc())

	return r
}

func stage5Responses(t *testing.T) map[string]awsResult {
	t.Helper()

	r := encryptionResponses()
	r[s3Key("head-object", testBucket, "--key", "identity/terraform.tfstate")] = okResult(`{"ServerSideEncryption":"aws:kms","SSEKMSKeyId":"` + testKeyARN + `"}`)
	r[s3Key("get-bucket-policy", testBucket)] = policyResult(t, stateBucketPolicy)

	return r
}

func retentionReader(content string) func(string) ([]byte, error) {
	return func(p string) ([]byte, error) {
		if p != identityMain {
			return nil, os.ErrNotExist
		}

		return []byte(content), nil
	}
}

// allNG は、すべての呼び出しに同じ応答を返すとき、段の全行が NG で、結果の行が件数どおりであること。
// first は、先頭の行の NG に添える理由。
func allNG(t *testing.T, steps []step, descs []string, r awsResult, first string) {
	t.Helper()

	f := &fakeAWS{t: t, handler: func([]string) (awsResult, bool) { return r, true }}

	lines, err := runStepLines(t, f, steps)

	require.ErrorIs(t, err, errMismatch)
	require.Len(t, lines, len(descs)+1)

	for i, d := range descs {
		assert.True(t, strings.HasPrefix(lines[i], "NG  "+d), lines[i])
	}

	assert.Equal(t, "NG  "+descs[0]+"（"+first+"）", lines[0])
	assert.Equal(t, "# 結果: "+strconv.Itoa(len(descs))+" 件のうち NG "+strconv.Itoa(len(descs))+" 件", lines[len(descs)])
}

func Test_stateKeyARN(t *testing.T) {
	t.Parallel()

	key := jsonKey("kms", "describe-key", "--key-id", testAlias)

	for name, tc := range map[string]struct {
		r          awsResult
		want       string
		wantReason string
	}{
		"ARN を読む":      {okResult(describeKeyBody), testKeyARN, ""},
		"呼び出しの失敗":      {failed("NotFoundException"), "", "呼び出しが失敗: NotFoundException"},
		"ARN が無い":      {okResult(`{"KeyMetadata":{}}`), "", "応答を読めない"},
		"ARN が文字列でない":  {okResult(`{"KeyMetadata":{"Arn":1}}`), "", "応答を読めない"},
		"応答が JSON でない": {okResult(``), "", "応答を読めない"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := verifyFake(t, map[string]awsResult{key: tc.r}, nil)

			arn, reason, err := stateKeyARN(newQuery(f.fn, testRegion), testNames(""))

			require.NoError(t, err)
			assert.Equal(t, tc.want, arn)
			assert.Equal(t, tc.wantReason, reason)
		})
	}

	t.Run("aws を起動できなければ errAWSStart", func(t *testing.T) {
		t.Parallel()

		_, _, err := stateKeyARN(newQuery(func(...string) awsResult { return awsResult{err: os.ErrNotExist} }, testRegion), testNames(""))

		require.ErrorIs(t, err, errAWSStart)
	})
}

func Test_expectWithKey(t *testing.T) {
	t.Parallel()

	args := []string{"s3api", "get-x"}
	pred := func(v map[string]any, key string) bool { return v["k"] == key }
	describe := jsonKey("kms", "describe-key", "--key-id", testAlias)

	for name, tc := range map[string]struct {
		responses map[string]awsResult
		want      verdict
	}{
		"鍵と応答が述語を満たせば ok": {map[string]awsResult{describe: okResult(describeKeyBody), jsonKey(args...): okResult(`{"k":"` + testKeyARN + `"}`)}, verdict{ok: true}},
		"満たさなければ NG":      {map[string]awsResult{describe: okResult(describeKeyBody), jsonKey(args...): okResult(`{"k":"other"}`)}, verdict{}},
		"鍵の ARN を読めなければ、応答を読まずに理由を添えた NG": {map[string]awsResult{describe: failed("denied")},
			verdict{reason: "state の鍵の ARN を読めない: 呼び出しが失敗: denied"}},
		"応答の呼び出しの失敗": {map[string]awsResult{describe: okResult(describeKeyBody), jsonKey(args...): failed("denied")}, verdict{reason: "呼び出しが失敗: denied"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := verifyFake(t, tc.responses, nil)

			v, err := expectWithKey("d", testNames(""), pred, args...).eval(newQuery(f.fn, testRegion))

			require.NoError(t, err)
			assert.Equal(t, tc.want, v)
		})
	}

	for name, failing := range map[string]string{"鍵の読み取り": "describe-key", "応答の読み取り": "get-x"} {
		t.Run(name+"で aws を起動できなければ errAWSStart", func(t *testing.T) {
			t.Parallel()

			f := verifyFake(t, map[string]awsResult{describe: okResult(describeKeyBody)}, nil)
			q := newQuery(func(a ...string) awsResult {
				if a[1] == failing {
					return awsResult{err: os.ErrNotExist}
				}

				return f.fn(a...)
			}, testRegion)

			_, err := expectWithKey("d", testNames(""), pred, args...).eval(q)

			require.ErrorIs(t, err, errAWSStart)
		})
	}
}

func Test_singleRule(t *testing.T) {
	t.Parallel()

	r, ok := singleRule(decode(t, `{"a":{"Rules":[{"x":1}]}}`), "a", "Rules")
	assert.True(t, ok)
	assert.Equal(t, map[string]any{"x": float64(1)}, r)

	for name, in := range map[string]string{
		"規則が2つ":          `{"Rules":[{},{}]}`,
		"規則が0":           `{"Rules":[]}`,
		"Rules が無い":      `{}`,
		"Rules が object": `{"Rules":{"x":1}}`,
	} {
		t.Run(name+"なら偽", func(t *testing.T) {
			t.Parallel()

			_, ok := singleRule(decode(t, in), "Rules")
			assert.False(t, ok)
		})
	}
}

func Test_isSSEC(t *testing.T) {
	t.Parallel()

	assert.True(t, isSSEC("SSE-C"))
	assert.False(t, isSSEC("SSE-C2"))
	assert.False(t, isSSEC(""))
}

func Test_kmsEncryption(t *testing.T) {
	t.Parallel()

	rule := func(body string) map[string]any {
		v, ok := decode(t, `{"ServerSideEncryptionConfiguration":{"Rules":[`+body+`]}}`).(map[string]any)
		require.True(t, ok)

		return v
	}
	kms := `"ApplyServerSideEncryptionByDefault":{"SSEAlgorithm":"aws:kms","KMSMasterKeyID":"k"}`
	ssec := `"BlockedEncryptionTypes":{"EncryptionType":["SSE-C"]}`

	assert.True(t, kmsEncryption(rule(`{`+kms+`,`+ssec+`}`), "k"), "Bucket Key が無くても ok")
	assert.True(t, kmsEncryption(rule(`{`+kms+`,"BucketKeyEnabled":null,`+ssec+`}`), "k"))
	assert.False(t, kmsEncryption(rule(`{`+kms+`,"BucketKeyEnabled":true,`+ssec+`}`), "k"))
	assert.False(t, kmsEncryption(rule(`{`+kms+`,`+ssec+`}`), "other"), "別の鍵")
	assert.False(t, kmsEncryption(rule(`{`+kms+`}`), "k"), "SSE-C を拒まない")
	assert.False(t, kmsEncryption(rule(`{"ApplyServerSideEncryptionByDefault":{"SSEAlgorithm":"AES256","KMSMasterKeyID":"k"},`+ssec+`}`), "k"))
	assert.False(t, kmsEncryption(rule(`{`+kms+`,`+ssec+`},{`+kms+`,`+ssec+`}`), "k"), "規則が2つ")
	assert.False(t, kmsEncryption(rule(`"s"`), "k"), "規則が object でない")
	assert.False(t, kmsEncryption(map[string]any{}, "k"))
}

func Test_kmsEncryptionSteps(t *testing.T) {
	t.Parallel()

	n := testNames("")

	t.Run("3つの Bucket を state の鍵で確かめる", func(t *testing.T) {
		t.Parallel()

		lines, err := runStepLines(t, verifyFake(t, encryptionResponses(), nil), kmsEncryptionSteps(n))

		require.NoError(t, err)
		assert.Equal(t, append(okLines(encryptionDescs), "# 結果: 3 件のうち NG 0 件"), lines)
	})

	t.Run("1つの Bucket が Bucket Key を使えば、その行だけ NG", func(t *testing.T) {
		t.Parallel()

		f := verifyFake(t, encryptionResponses(), map[string]awsResult{
			s3Key("get-bucket-encryption", testPlanBucket): okResult(strings.Replace(kmsEncryptionBody, `"BucketKeyEnabled":false`, `"BucketKeyEnabled":true`, 1)),
		})

		lines, err := runStepLines(t, f, kmsEncryptionSteps(n))

		require.ErrorIs(t, err, errMismatch)
		assert.Equal(t, "NG  "+encryptionDescs[1], lines[1])
	})
}

func Test_stage1Steps(t *testing.T) {
	t.Parallel()

	n := testNames("")

	t.Run("すべて ok", func(t *testing.T) {
		t.Parallel()

		steps := stage1Steps(n)
		lines, err := runStepLines(t, verifyFake(t, stage1Responses(), nil), steps)

		require.NoError(t, err)
		assert.Len(t, steps, stage1Count)
		assert.Equal(t, append(okLines(stage1Descs), "# 結果: 4 件のうち NG 0 件"), lines)
	})

	for name, tc := range map[string]struct {
		key  string
		body awsResult
		line int
	}{
		"Public Access Block の1項目が false": {s3Key("get-public-access-block", testBucket),
			okResult(`{"PublicAccessBlockConfiguration":{"BlockPublicAcls":true,"IgnorePublicAcls":false,"BlockPublicPolicy":true,"RestrictPublicBuckets":true}}`), 0},
		"Public Access Block の項目が無い": {s3Key("get-public-access-block", testBucket),
			okResult(`{"PublicAccessBlockConfiguration":{"BlockPublicAcls":true,"IgnorePublicAcls":true,"BlockPublicPolicy":true}}`), 0},
		"Public Access Block が object でない": {s3Key("get-public-access-block", testBucket), okResult(`{"PublicAccessBlockConfiguration":"x"}`), 0},
		"所有権が ObjectWriter": {s3Key("get-bucket-ownership-controls", testBucket),
			okResult(`{"OwnershipControls":{"Rules":[{"ObjectOwnership":"ObjectWriter"}]}}`), 1},
		"所有権の規則が2つ": {s3Key("get-bucket-ownership-controls", testBucket),
			okResult(`{"OwnershipControls":{"Rules":[{"ObjectOwnership":"BucketOwnerEnforced"},{"ObjectOwnership":"BucketOwnerEnforced"}]}}`), 1},
		"版管理が Suspended": {s3Key("get-bucket-versioning", testBucket), okResult(`{"Status":"Suspended"}`), 2},
		"版管理が未設定":        {s3Key("get-bucket-versioning", testBucket), okResult(`{}`), 2},
		"暗号化が aws:kms": {s3Key("get-bucket-encryption", testBucket),
			okResult(`{"ServerSideEncryptionConfiguration":{"Rules":[{"ApplyServerSideEncryptionByDefault":{"SSEAlgorithm":"aws:kms"},"BlockedEncryptionTypes":{"EncryptionType":["SSE-C"]}}]}}`), 3},
		"SSE-C を拒まない": {s3Key("get-bucket-encryption", testBucket),
			okResult(`{"ServerSideEncryptionConfiguration":{"Rules":[{"ApplyServerSideEncryptionByDefault":{"SSEAlgorithm":"AES256"}}]}}`), 3},
		"暗号化の規則が2つ": {s3Key("get-bucket-encryption", testBucket),
			okResult(`{"ServerSideEncryptionConfiguration":{"Rules":[{"ApplyServerSideEncryptionByDefault":{"SSEAlgorithm":"AES256"},"BlockedEncryptionTypes":{"EncryptionType":["SSE-C"]}},{}]}}`), 3},
	} {
		t.Run(name+"なら、その行だけ NG", func(t *testing.T) {
			t.Parallel()

			lines, err := runStepLines(t, verifyFake(t, stage1Responses(), map[string]awsResult{tc.key: tc.body}), stage1Steps(n))

			require.ErrorIs(t, err, errMismatch)
			assert.Equal(t, "NG  "+stage1Descs[tc.line], lines[tc.line])
			assert.Equal(t, "# 結果: 4 件のうち NG 1 件", lines[len(lines)-1])
		})
	}

	t.Run("応答がすべて空なら、すべて理由を添えた NG", func(t *testing.T) {
		t.Parallel()

		f := &fakeAWS{t: t, handler: func([]string) (awsResult, bool) { return okResult(""), true }}

		lines, err := runStepLines(t, f, stage1Steps(n))

		require.ErrorIs(t, err, errMismatch)

		for i, d := range stage1Descs {
			assert.Equal(t, "NG  "+d+"（応答を読めない）", lines[i])
		}
	})
}

func Test_noneMatch(t *testing.T) {
	t.Parallel()

	hasX := func(s string) bool { return s == "x" }
	obj := func(s string) map[string]any {
		v, ok := decode(t, s).(map[string]any)
		require.True(t, ok)

		return v
	}

	assert.True(t, noneMatch(obj(`{"L":[{"A":"y"}]}`), "L", "A", hasX))
	assert.True(t, noneMatch(obj(`{"L":[]}`), "L", "A", hasX))
	assert.False(t, noneMatch(obj(`{"L":[{"A":"y"},{"A":"x"}]}`), "L", "A", hasX))
	assert.False(t, noneMatch(obj(`{}`), "L", "A", hasX), "配列が無ければ偽")
	assert.False(t, noneMatch(obj(`{"L":[{"A":1}]}`), "L", "A", hasX), "文字列でなければ偽")
	assert.False(t, noneMatch(obj(`{"L":[{}]}`), "L", "A", hasX), "field が無ければ偽")
	assert.False(t, noneMatch(obj(`{"L":["s"]}`), "L", "A", hasX), "要素が object でなければ偽")
}

func Test_beforeApplySteps(t *testing.T) {
	t.Parallel()

	n := testNames("")

	t.Run("すべて ok", func(t *testing.T) {
		t.Parallel()

		steps := beforeApplySteps(n)
		lines, err := runStepLines(t, verifyFake(t, beforeApplyResponses(), nil), steps)

		require.NoError(t, err)
		assert.Len(t, steps, beforeApplyCount)
		assert.Equal(t, append(okLines(beforeApplyDescs), "# 結果: 22 件のうち NG 0 件"), lines)
	})

	for name, tc := range map[string]struct {
		key  string
		body awsResult
		line string
	}{
		"OIDC provider が在る": {jsonKey("iam", "list-open-id-connect-providers"),
			okResult(`{"OpenIDConnectProviderList":[{"Arn":"arn:aws:iam::123456789012:oidc-provider/token.actions.githubusercontent.com"}]}`),
			"NG  " + beforeApplyDescs[0]},
		"OIDC provider の一覧が読めない":      {jsonKey("iam", "list-open-id-connect-providers"), okResult(`{}`), "NG  " + beforeApplyDescs[0]},
		"OIDC provider の Arn が文字列でない": {jsonKey("iam", "list-open-id-connect-providers"), okResult(`{"OpenIDConnectProviderList":[{"Arn":1}]}`), "NG  " + beforeApplyDescs[0]},
		"role が在る": {jsonKey("iam", "get-role", "--role-name", "tabp-admin-ci-apply"), okResult(`{"Role":{}}`),
			"NG  role tabp-admin-ci-apply がまだ無い（エラーが返らなかった）"},
		"role の読み取りが拒否": {jsonKey("iam", "get-role", "--role-name", "tabp-admin-ci-plan"), awsError("AccessDenied"),
			"NG  role tabp-admin-ci-plan がまだ無い（NoSuchEntity ではないエラー: AccessDenied）"},
		"Bucket Policy が在る": {s3Key("get-bucket-policy", testBucket), okResult(`{"Policy":"{}"}`),
			"NG  state Bucket に Bucket Policy がまだ無い（エラーが返らなかった）"},
		"lifecycle の読み取りが別のエラー": {s3Key("get-bucket-lifecycle-configuration", testBucket), awsError("NoSuchBucket"),
			"NG  state Bucket に lifecycle がまだ無い（NoSuchLifecycleConfiguration ではないエラー: NoSuchBucket）"},
		"alias が在る": {jsonKey("kms", "list-aliases"), okResult(`{"Aliases":[{"AliasName":"alias/tabp-admin-tfstate"}]}`),
			"NG  state の鍵の alias（alias/tabp-admin-tfstate）がまだ無い"},
		"alias の一覧が配列でない": {jsonKey("kms", "list-aliases"), okResult(`{"Aliases":null}`), "NG  state の鍵の alias（alias/tabp-admin-tfstate）がまだ無い"},
		"組織が在る":           {jsonKey("organizations", "describe-organization"), okResult(`{"Organization":{}}`), "NG  組織がまだ無い（エラーが返らなかった）"},
		"StackSet の code を読めない": {jsonKey("cloudformation", "describe-stack-set", "--stack-set-name", "tabp-break-glass"), failed("Could not connect"),
			"NG  StackSet tabp-break-glass がまだ無い（StackSetNotFoundException ではないエラー）"},
		"policy が在る": {jsonKey("iam", "get-policy", "--policy-arn", "arn:aws:iam::123456789012:policy/tabp-admin-ci-apply-workload"), okResult(`{}`),
			"NG  policy tabp-admin-ci-apply-workload がまだ無い（エラーが返らなかった）"},
		"password policy が在る": {jsonKey("iam", "get-account-password-policy"), okResult(`{}`), "NG  account password policy がまだ無い（エラーが返らなかった）"},
	} {
		t.Run(name+"なら NG", func(t *testing.T) {
			t.Parallel()

			lines, err := runStepLines(t, verifyFake(t, beforeApplyResponses(), map[string]awsResult{tc.key: tc.body}), beforeApplySteps(n))

			require.ErrorIs(t, err, errMismatch)
			assert.Contains(t, lines, tc.line)
			assert.Equal(t, "# 結果: 22 件のうち NG 1 件", lines[len(lines)-1])
		})
	}

	for name, tc := range map[string]struct {
		r     awsResult
		first string
	}{
		"応答がすべて空":    {okResult(""), "応答を読めない"},
		"呼び出しがすべて失敗": {failed("An error occurred (AccessDenied)"), "呼び出しが失敗: An error occurred (AccessDenied)"},
	} {
		t.Run(name+"なら、全行が NG", func(t *testing.T) {
			t.Parallel()

			allNG(t, beforeApplySteps(n), beforeApplyDescs, tc.r, tc.first)
		})
	}

	t.Run("ホスト名の接尾辞だけ一致する別の provider は数えない", func(t *testing.T) {
		t.Parallel()

		f := verifyFake(t, beforeApplyResponses(), map[string]awsResult{jsonKey("iam", "list-open-id-connect-providers"): okResult(
			`{"OpenIDConnectProviderList":[{"Arn":"arn:aws:iam::123456789012:oidc-provider/evil-token.actions.githubusercontent.com"}]}`)})

		lines, err := runStepLines(t, f, beforeApplySteps(n))

		require.NoError(t, err)
		assert.Equal(t, "ok  "+beforeApplyDescs[0], lines[0])
	})
}

func Test_policyNames(t *testing.T) {
	t.Parallel()

	obj := func(s string) map[string]any {
		v, ok := decode(t, s).(map[string]any)
		require.True(t, ok)

		return v
	}

	got, ok := policyNames(obj(`{"A":[{"PolicyName":"b"},{"PolicyName":"a"}]}`), "A")
	require.True(t, ok)
	assert.Equal(t, []string{"a", "b"}, got, "並べ替える")

	got, ok = policyNames(obj(`{"A":[]}`), "A")
	require.True(t, ok)
	assert.Empty(t, got)

	for name, in := range map[string]string{
		"配列が無い":          `{}`,
		"名前が文字列でない":      `{"A":[{"PolicyName":1}]}`,
		"名前が無い":          `{"A":[{}]}`,
		"要素が object でない": `{"A":["a"]}`,
	} {
		_, ok := policyNames(obj(in), "A")
		assert.False(t, ok, name)
	}
}

func Test_tlsDenyPolicy(t *testing.T) {
	t.Parallel()

	doc := func(s string) map[string]any {
		v, ok := decode(t, s).(map[string]any)
		require.True(t, ok)

		return v
	}

	assert.True(t, tlsDenyPolicy(doc(stateBucketPolicy)))
	assert.False(t, tlsDenyPolicy(doc(`{"Statement":[{"Effect":"Deny","Condition":{"Bool":{"aws:SecureTransport":"false"}}},{"Effect":"Allow"}]}`)), "Allow が混ざる")
	assert.False(t, tlsDenyPolicy(doc(`{"Statement":[{"Effect":"Deny"}]}`)), "TLS の文が無い")
	assert.False(t, tlsDenyPolicy(doc(`{"Statement":[{"Effect":"Deny","Condition":{"Bool":{"aws:SecureTransport":false}}}]}`)), "値が文字列でない")
	assert.False(t, tlsDenyPolicy(doc(`{"Statement":[]}`)), "文が無い")
	assert.False(t, tlsDenyPolicy(doc(`{}`)))
	assert.False(t, tlsDenyPolicy(doc(`{"Statement":[{"Effect":"Deny","Condition":{"Bool":{"aws:SecureTransport":"false"}}},{"Effect":"Deny","Condition":"x"}]}`)),
		"評価できない文があれば偽")
	assert.False(t, tlsDenyPolicy(doc(`{"Statement":[{"Effect":"Deny","Condition":{"Bool":{"aws:SecureTransport":"true"}}}]}`)),
		"TLS の要求を拒む Deny だけ")
}

func Test_executorException(t *testing.T) {
	t.Parallel()

	doc := func(s string) map[string]any {
		v, ok := decode(t, s).(map[string]any)
		require.True(t, ok)

		return v
	}
	stmt := func(arns string) string {
		return `{"Statement":[{"Sid":"Other","Effect":"Deny"},{"Sid":"DenyObjectWriteByOthers","Condition":{"ArnNotEquals":{"aws:PrincipalArn":` + arns + `}}}]}`
	}

	assert.True(t, executorException(doc(stmt(`["arn:aws:iam::1:role/r","arn:aws:iam::1:user/bob"]`)), ":user/bob"))
	assert.True(t, executorException(doc(stmt(`"arn:aws:iam::1:user/bob"`)), ":user/bob"), "単一の文字列も読む")
	assert.False(t, executorException(doc(stmt(`["arn:aws:iam::1:user/bobby"]`)), ":user/bob"), "末尾で比べる")
	assert.False(t, executorException(doc(stmt(`["arn:aws:iam::1:role/r"]`)), ":user/bob"))
	assert.False(t, executorException(doc(stmt(`[1]`)), ":user/bob"), "文字列でない値")
	assert.False(t, executorException(doc(`{"Statement":[{"Sid":"DenyObjectWriteByOthers"}]}`), ":user/bob"), "条件が無い")
	assert.False(t, executorException(doc(`{"Statement":[{"Sid":"DenyObjectWriteByOthers","Condition":"x"}]}`), ":user/bob"), "条件をたどれない")
	assert.False(t, executorException(doc(`{"Statement":[{"Sid":"X","Condition":{"ArnNotEquals":{"aws:PrincipalArn":"arn:aws:iam::1:user/bob"}}}]}`), ":user/bob"),
		"別の文の例外は数えない")
	assert.False(t, executorException(doc(`{}`), ":user/bob"))
	assert.True(t, executorException(doc(`{"Statement":[`+
		`{"Sid":"DenyObjectWriteByOthers","Condition":{"ArnNotEquals":{"aws:PrincipalArn":"arn:aws:iam::1:user/bob"}}},`+
		`{"Sid":"DenyObjectWriteByOthers","Condition":{"ArnNotEquals":{"aws:PrincipalArn":"arn:aws:iam::1:role/r"}}}]}`), ":user/bob"),
		"同名の文が2つで前者だけ一致しても真")
	assert.False(t, executorException(doc(`{"Statement":["s"]}`), ":user/bob"), "文が object でない")
}

func Test_noUser(t *testing.T) {
	t.Parallel()

	doc := func(s string) map[string]any {
		v, ok := decode(t, s).(map[string]any)
		require.True(t, ok)

		return v
	}

	assert.True(t, noUser(doc(stateBucketPolicy)))
	assert.False(t, noUser(doc(`{"Statement":[{"Condition":{"ArnNotEquals":{"aws:PrincipalArn":["arn:aws:iam::1:user/bob"]}}}]}`)))
	assert.False(t, noUser(doc(`{"Sid":"arn:aws:iam::1:user/x"}`)), "どこに在っても数える")
	assert.True(t, noUser(doc(`{"Statement":[{":user/":"key は見ない"}]}`)))
	assert.True(t, noUser(doc(`{"Statement":{"Effect":"Deny"}}`)), "1文の object")
	assert.False(t, noUser(doc(`{"Statement":[]}`)), "文が無い文書")
	assert.False(t, noUser(doc(`{}`)), "空の文書")
}

func Test_retentionDays(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		content    string
		want       int
		wantReason string
	}{
		"1か所の値を読む":     {retentionFile, 90, ""},
		"空白とタブを許す":     {"\tstate_noncurrent_version_retention_days=30\n", 30, ""},
		"// のコメントを許す":  {"state_noncurrent_version_retention_days = 30 // 日\n", 30, ""},
		"# のコメントを許す":   {"state_noncurrent_version_retention_days = 30 # 日\n", 30, ""},
		"値の後ろの空白を許す":   {"state_noncurrent_version_retention_days = 30 \t\n", 30, ""},
		"引用符つきの値":      {"state_noncurrent_version_retention_days = \"30\"\n", 0, identityMain + " の state_noncurrent_version_retention_days を日数として読めない"},
		"負の値":          {"state_noncurrent_version_retention_days = -30\n", 0, identityMain + " の state_noncurrent_version_retention_days を日数として読めない"},
		"CRLF を許す":     {"state_noncurrent_version_retention_days = 30\r\n", 30, ""},
		"行が無い":         {"locals {}\n", 0, identityMain + " の state_noncurrent_version_retention_days の代入が1か所ではない（0 か所）"},
		"2か所":          {"state_noncurrent_version_retention_days = 1\nstate_noncurrent_version_retention_days = 2\n", 0, identityMain + " の state_noncurrent_version_retention_days の代入が1か所ではない（2 か所）"},
		"値が数でない":       {"state_noncurrent_version_retention_days = var.days\n", 0, identityMain + " の state_noncurrent_version_retention_days を日数として読めない"},
		"式":            {"state_noncurrent_version_retention_days = 90 * 2\n", 0, identityMain + " の state_noncurrent_version_retention_days を日数として読めない"},
		"数の後ろに文字":      {"state_noncurrent_version_retention_days = 90d\n", 0, identityMain + " の state_noncurrent_version_retention_days を日数として読めない"},
		"値が空":          {"state_noncurrent_version_retention_days =\n", 0, identityMain + " の state_noncurrent_version_retention_days を日数として読めない"},
		"行の途中の名前は数えない": {"# x state_noncurrent_version_retention_days = 1\n", 0, identityMain + " の state_noncurrent_version_retention_days の代入が1か所ではない（0 か所）"},
		"桁が多すぎる":       {"state_noncurrent_version_retention_days = 99999999999999999999\n", 0, identityMain + " の state_noncurrent_version_retention_days を日数として読めない"},
		"空のファイル":       {"", 0, identityMain + " の state_noncurrent_version_retention_days の代入が1か所ではない（0 か所）"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			days, reason := retentionDays(retentionReader(tc.content))

			assert.Equal(t, tc.want, days)
			assert.Equal(t, tc.wantReason, reason)
		})
	}

	t.Run("読めなければ理由を返す", func(t *testing.T) {
		t.Parallel()

		_, reason := retentionDays(func(string) ([]byte, error) { return nil, os.ErrPermission })

		assert.Equal(t, identityMain+" を読めない: "+os.ErrPermission.Error(), reason)
	})
}

func Test_lifecycleRule(t *testing.T) {
	t.Parallel()

	obj := func(s string) map[string]any {
		v, ok := decode(t, s).(map[string]any)
		require.True(t, ok)

		return v
	}
	swap := func(old, repl string) map[string]any { return obj(strings.Replace(lifecycleBody, old, repl, 1)) }

	assert.True(t, lifecycleRule(obj(lifecycleBody), 90))
	assert.True(t, lifecycleRule(swap(`"Filter":{},`, `"Filter":{"Prefix":""},`), 90), "空の prefix")
	assert.True(t, lifecycleRule(swap(`"Filter":{},`, ``), 90), "Filter が無い")
	assert.True(t, lifecycleRule(swap(`"Filter":{},`, `"Filter":null,`), 90))
	assert.False(t, lifecycleRule(obj(lifecycleBody), 30), "保持日数の違い")
	assert.False(t, lifecycleRule(swap(`"Filter":{},`, `"Filter":{"Prefix":"x/"},`), 90), "prefix で限る")
	assert.False(t, lifecycleRule(swap(`"Enabled"`, `"Disabled"`), 90))
	assert.False(t, lifecycleRule(swap(`{"ExpiredObjectDeleteMarker":true}`, `{"Days":1}`), 90))
	assert.False(t, lifecycleRule(swap(`{"ExpiredObjectDeleteMarker":true}`, `{"ExpiredObjectDeleteMarker":true,"Days":1}`), 90))
	assert.False(t, lifecycleRule(swap(`"DaysAfterInitiation":1`, `"DaysAfterInitiation":7`), 90))
	assert.False(t, lifecycleRule(obj(`{"Rules":[`+strings.TrimSuffix(strings.TrimPrefix(lifecycleBody, `{"Rules":[`), `]}`)+`,{}]}`), 90), "規則が2つ")
	assert.False(t, lifecycleRule(obj(`{"Rules":["s"]}`), 90), "規則が object でない")
	assert.False(t, lifecycleRule(obj(`{}`), 90))
}

func Test_keyPolicy(t *testing.T) {
	t.Parallel()

	n := testNames("")
	doc := func(s string) map[string]any {
		v, ok := decode(t, s).(map[string]any)
		require.True(t, ok)

		return v
	}
	swap := func(old, repl string) map[string]any {
		require.Contains(t, keyPolicyDoc(), old)

		return doc(strings.Replace(keyPolicyDoc(), old, repl, 1))
	}
	sb := "arn:aws:s3:::" + testBucket

	assert.True(t, keyPolicy(doc(keyPolicyDoc()), n))

	six := doc(keyPolicyDoc())
	stmts, ok := six["Statement"].([]any)
	require.True(t, ok)

	six["Statement"] = stmts[:6]

	for name, d := range map[string]map[string]any{
		"文が6つ":                   six,
		"委任の文が無い":                {"Statement": nil},
		"委任の principal が違う":      swap(`"arn:aws:iam::123456789012:root"`, `"arn:aws:iam::123456789012:user/x"`),
		"plan の書き込みに余分な action":  swap(`["kms:GenerateDataKey","kms:Decrypt"]`, `["kms:GenerateDataKey","kms:Decrypt","kms:Encrypt"]`),
		"S3 を経由しない":              swap(`"s3.ap-northeast-1.amazonaws.com"`, `"s3.us-east-1.amazonaws.com"`),
		"unlock の lock file が違う": swap(`"`+sb+`/identity/terraform.tfstate.tflock",`, `"`+sb+`/identity/terraform.tfstate",`),
		"principal が2つ":          swap(`{"AWS":"arn:aws:iam::123456789012:role/tabp-admin-ci-apply"}`, `{"AWS":["arn:aws:iam::123456789012:role/tabp-admin-ci-apply","arn:aws:iam::123456789012:role/x"]}`),
		"Allow でない":              swap(`{"Effect":"Allow","Principal":{"AWS":"arn:aws:iam::123456789012:role/tabp-admin-ci-unlock"}`, `{"Effect":"Deny","Principal":{"AWS":"arn:aws:iam::123456789012:role/tabp-admin-ci-unlock"}`),
		"復号に書き込みの演算子を使う":         swap(`"StringEquals":{"kms:EncryptionContext:aws:s3:arn":["`+sb+`/workload/terraform.tfstate.tflock"],`, `"StringLike":{"kms:EncryptionContext:aws:s3:arn":["`+sb+`/workload/terraform.tfstate.tflock"]},"StringEquals":{`),
		"文が8つ":                   doc(strings.Replace(keyPolicyDoc(), `"Statement":[`, `"Statement":[{"Effect":"Deny","Principal":"*"},`, 1)),
		"principal が文字列 * の文が在る": doc(strings.Replace(keyPolicyDoc(), `{"Sid":"root",`, `{"Effect":"Allow","Principal":"*"},{"Sid":"root",`, 1)),
	} {
		t.Run(name+"なら偽", func(t *testing.T) {
			t.Parallel()

			assert.False(t, keyPolicy(d, n))
		})
	}
}

func Test_stage4Steps(t *testing.T) {
	t.Parallel()

	n := testNames("")

	t.Run("実行者なしで、すべて ok", func(t *testing.T) {
		t.Parallel()

		steps := stage4Steps(n, retentionReader(retentionFile))
		lines, err := runStepLines(t, verifyFake(t, stage4Responses(t, stateBucketPolicy), nil), steps)

		require.NoError(t, err)
		assert.Len(t, steps, stage4Count)
		assert.Equal(t, append(okLines(stage4Descs(noExecutorDesc)), "# 結果: 24 件のうち NG 0 件"), lines)
	})

	t.Run("実行者ありで、すべて ok", func(t *testing.T) {
		t.Parallel()

		policy := strings.Replace(stateBucketPolicy, `"arn:aws:iam::123456789012:role/tabp-admin-ci-apply"]`, `"arn:aws:iam::123456789012:role/tabp-admin-ci-apply","arn:aws:iam::123456789012:user/bob"]`, 1)
		steps := stage4Steps(testNames("bob"), retentionReader(retentionFile))
		lines, err := runStepLines(t, verifyFake(t, stage4Responses(t, policy), nil), steps)

		require.NoError(t, err)
		assert.Len(t, steps, stage4Count)
		assert.Equal(t, append(okLines(stage4Descs(executorDesc)), "# 結果: 24 件のうち NG 0 件"), lines)
	})

	t.Run("実行者なしで、Bucket Policy に IAM user が在れば NG", func(t *testing.T) {
		t.Parallel()

		policy := strings.Replace(stateBucketPolicy, `role/tabp-admin-ci-apply"]`, `role/tabp-admin-ci-apply","arn:aws:iam::123456789012:user/bob"]`, 1)
		lines, err := runStepLines(t, verifyFake(t, stage4Responses(t, policy), nil), stage4Steps(n, retentionReader(retentionFile)))

		require.ErrorIs(t, err, errMismatch)
		assert.Contains(t, lines, "NG  "+noExecutorDesc)
	})

	t.Run("実行者ありで、例外に実行者が無ければ NG", func(t *testing.T) {
		t.Parallel()

		lines, err := runStepLines(t, verifyFake(t, stage4Responses(t, stateBucketPolicy), nil), stage4Steps(testNames("bob"), retentionReader(retentionFile)))

		require.ErrorIs(t, err, errMismatch)
		assert.Contains(t, lines, "NG  "+executorDesc)
	})

	t.Run("保持日数を決められなければ、lifecycle を読まずに理由を添えた NG で、日数を ? にする", func(t *testing.T) {
		t.Parallel()

		r := stage4Responses(t, stateBucketPolicy)
		delete(r, s3Key("get-bucket-lifecycle-configuration", testBucket))

		lines, err := runStepLines(t, verifyFake(t, r, nil), stage4Steps(n, retentionReader("")))

		require.ErrorIs(t, err, errMismatch)
		assert.Contains(t, lines, "NG  lifecycle が規則1つ（版の保持 ? 日、delete marker の掃除、未完了の multipart の中止 1 日）（"+
			identityMain+" の state_noncurrent_version_retention_days の代入が1か所ではない（0 か所））")
	})

	t.Run("Bucket Policy を読めなければ、それを読む2行が理由を添えた NG", func(t *testing.T) {
		t.Parallel()

		f := verifyFake(t, stage4Responses(t, stateBucketPolicy), map[string]awsResult{s3Key("get-bucket-policy", testBucket): awsError("NoSuchBucketPolicy")})

		lines, err := runStepLines(t, f, stage4Steps(n, retentionReader(retentionFile)))

		require.ErrorIs(t, err, errMismatch)
		assert.Contains(t, lines, "NG  state Bucket の Bucket Policy の文がすべて Deny で、TLS でない要求を拒む文が在る（呼び出しが失敗: An error occurred (NoSuchBucketPolicy) when calling the X operation: detail）")
		assert.Contains(t, lines, "NG  "+noExecutorDesc+"（呼び出しが失敗: An error occurred (NoSuchBucketPolicy) when calling the X operation: detail）")
		assert.Equal(t, "# 結果: 24 件のうち NG 2 件", lines[len(lines)-1])
	})

	for name, tc := range map[string]struct {
		key  string
		body string
		desc string
	}{
		"OIDC の audience が2つ": {jsonKey("iam", "get-open-id-connect-provider", "--open-id-connect-provider-arn", "arn:aws:iam::123456789012:oidc-provider/token.actions.githubusercontent.com"),
			`{"ClientIDList":["sts.amazonaws.com","x"]}`, "OIDC provider の ClientIDList が sts.amazonaws.com だけ"},
		"inline policy が2つ": {jsonKey("iam", "list-role-policies", "--role-name", "tabp-admin-ci-plan"), `{"PolicyNames":["plan","x"]}`,
			"role tabp-admin-ci-plan の inline policy が plan だけ"},
		"inline policy が文字列": {jsonKey("iam", "list-role-policies", "--role-name", "tabp-admin-ci-plan"), `{"PolicyNames":"plan"}`,
			"role tabp-admin-ci-plan の inline policy が plan だけ"},
		"apply-identity に manage が無い": {jsonKey("iam", "list-attached-role-policies", "--role-name", "tabp-admin-ci-apply-identity"),
			`{"AttachedPolicies":[{"PolicyName":"tabp-admin-ci-apply-identity-governance"}]}`,
			"role tabp-admin-ci-apply-identity に付く managed policy が、統制の policy と委任管理者の policy と manage の policy だけ"},
		"apply に余分な policy": {jsonKey("iam", "list-attached-role-policies", "--role-name", "tabp-admin-ci-apply"),
			`{"AttachedPolicies":[{"PolicyName":"tabp-admin-ci-apply-workload"},{"PolicyName":"tabp-admin-ci-apply-workload-read"},{"PolicyName":"x"}]}`,
			"role tabp-admin-ci-apply に付く managed policy が、workload の3つの policy だけ"},
		"unlock に policy が付く": {jsonKey("iam", "list-attached-role-policies", "--role-name", "tabp-admin-ci-unlock"),
			`{"AttachedPolicies":[{"PolicyName":"x"}]}`, "role tabp-admin-ci-unlock に managed policy が付いていない"},
		"鍵が無効": {jsonKey("kms", "describe-key", "--key-id", testAlias), strings.Replace(describeKeyBody, `"Enabled"`, `"Disabled"`, 1),
			"state の鍵が有効な対称鍵（単一リージョン、AWS_KMS）"},
		"鍵が複数リージョン": {jsonKey("kms", "describe-key", "--key-id", testAlias), strings.Replace(describeKeyBody, `"MultiRegion":false`, `"MultiRegion":true`, 1),
			"state の鍵が有効な対称鍵（単一リージョン、AWS_KMS）"},
		"鍵が外部の素材": {jsonKey("kms", "describe-key", "--key-id", testAlias), strings.Replace(describeKeyBody, `"AWS_KMS"`, `"EXTERNAL"`, 1),
			"state の鍵が有効な対称鍵（単一リージョン、AWS_KMS）"},
		"組織が請求だけ":             {jsonKey("organizations", "describe-organization"), `{"Organization":{"FeatureSet":"CONSOLIDATED_BILLING"}}`, "組織の FeatureSet が ALL"},
		"break-glass にアクセスキー": {jsonKey("iam", "list-access-keys", "--user-name", "break-glass-2"), `{"AccessKeyMetadata":[{}]}`, "IAM user break-glass-2 がアクセスキーを持たない"},
		"MFA を求めない文が在る": {jsonKey("iam", "get-role", "--role-name", "tabp-break-glass"),
			`{"Role":{"AssumeRolePolicyDocument":{"Statement":[{"Condition":{"Bool":{"aws:MultiFactorAuthPresent":"true"}}},{"Effect":"Allow"}]}}}`,
			"role tabp-break-glass の信頼条件が MFA を求める"},
		"信頼条件の文が無い":          {jsonKey("iam", "get-role", "--role-name", "tabp-break-glass"), `{"Role":{"AssumeRolePolicyDocument":{"Statement":[]}}}`, "role tabp-break-glass の信頼条件が MFA を求める"},
		"rotation が 90 日":    {jsonKey("kms", "get-key-rotation-status", "--key-id", testAlias), `{"KeyRotationEnabled":true,"RotationPeriodInDays":90}`, "state の鍵の自動 rotation が 365 日"},
		"rotation が無効":       {jsonKey("kms", "get-key-rotation-status", "--key-id", testAlias), `{"KeyRotationEnabled":false,"RotationPeriodInDays":365}`, "state の鍵の自動 rotation が 365 日"},
		"lifecycle の保持日数が違う": {s3Key("get-bucket-lifecycle-configuration", testBucket), strings.Replace(lifecycleBody, `"NoncurrentDays":90`, `"NoncurrentDays":30`, 1), lifecycleDesc},
		"ClientIDList が無い": {jsonKey("iam", "get-open-id-connect-provider", "--open-id-connect-provider-arn", "arn:aws:iam::123456789012:oidc-provider/token.actions.githubusercontent.com"),
			`{}`, "OIDC provider の ClientIDList が sts.amazonaws.com だけ"},
		"ClientIDList が null": {jsonKey("iam", "get-open-id-connect-provider", "--open-id-connect-provider-arn", "arn:aws:iam::123456789012:oidc-provider/token.actions.githubusercontent.com"),
			`{"ClientIDList":null}`, "OIDC provider の ClientIDList が sts.amazonaws.com だけ"},
		"AccessKeyMetadata が無い":    {jsonKey("iam", "list-access-keys", "--user-name", "break-glass-1"), `{}`, "IAM user break-glass-1 がアクセスキーを持たない"},
		"AccessKeyMetadata が null": {jsonKey("iam", "list-access-keys", "--user-name", "break-glass-1"), `{"AccessKeyMetadata":null}`, "IAM user break-glass-1 がアクセスキーを持たない"},
		"rotation の項目が無い":          {jsonKey("kms", "get-key-rotation-status", "--key-id", testAlias), `{}`, "state の鍵の自動 rotation が 365 日"},
		"rotation の日数が null":       {jsonKey("kms", "get-key-rotation-status", "--key-id", testAlias), `{"KeyRotationEnabled":true,"RotationPeriodInDays":null}`, "state の鍵の自動 rotation が 365 日"},
	} {
		t.Run(name+"なら NG", func(t *testing.T) {
			t.Parallel()

			lines, err := runStepLines(t, verifyFake(t, stage4Responses(t, stateBucketPolicy), map[string]awsResult{tc.key: okResult(tc.body)}),
				stage4Steps(n, retentionReader(retentionFile)))

			require.ErrorIs(t, err, errMismatch)
			assert.Contains(t, lines, "NG  "+tc.desc)
			assert.Equal(t, "# 結果: 24 件のうち NG 1 件", lines[len(lines)-1])
		})
	}

	t.Run("Key Policy が7文でなければ NG", func(t *testing.T) {
		t.Parallel()

		f := verifyFake(t, stage4Responses(t, stateBucketPolicy), map[string]awsResult{
			jsonKey("kms", "get-key-policy", "--key-id", testAlias, "--policy-name", "default"): policyResult(t, `{"Statement":[]}`),
		})

		lines, err := runStepLines(t, f, stage4Steps(n, retentionReader(retentionFile)))

		require.ErrorIs(t, err, errMismatch)
		assert.Contains(t, lines, "NG  "+keyPolicyDesc)
	})

	for name, tc := range map[string]struct {
		r     awsResult
		first string
	}{
		"応答がすべて空":    {okResult(""), "state の鍵の ARN を読めない: 応答を読めない"},
		"呼び出しがすべて失敗": {failed("An error occurred (AccessDenied)"), "state の鍵の ARN を読めない: 呼び出しが失敗: An error occurred (AccessDenied)"},
	} {
		t.Run(name+"なら、全行が NG", func(t *testing.T) {
			t.Parallel()

			allNG(t, stage4Steps(n, retentionReader(retentionFile)), stage4Descs(noExecutorDesc), tc.r, tc.first)
		})
	}
}

func Test_stage5Steps(t *testing.T) {
	t.Parallel()

	n := testNames("")

	t.Run("すべて ok", func(t *testing.T) {
		t.Parallel()

		steps := stage5Steps(n)
		lines, err := runStepLines(t, verifyFake(t, stage5Responses(t), nil), steps)

		require.NoError(t, err)
		assert.Len(t, steps, stage5Count)
		assert.Equal(t, append(okLines(stage5Descs), "# 結果: 5 件のうち NG 0 件"), lines)
	})

	for name, tc := range map[string]struct {
		key  string
		body awsResult
		line string
	}{
		"state が別の鍵で書かれている": {s3Key("head-object", testBucket, "--key", "identity/terraform.tfstate"),
			okResult(`{"ServerSideEncryption":"aws:kms","SSEKMSKeyId":"arn:aws:kms:ap-northeast-1:123456789012:key/other"}`), "NG  " + stage5Descs[3]},
		"state が SSE-S3 で書かれている": {s3Key("head-object", testBucket, "--key", "identity/terraform.tfstate"),
			okResult(`{"ServerSideEncryption":"AES256"}`), "NG  " + stage5Descs[3]},
		"例外に IAM user が残る": {s3Key("get-bucket-policy", testBucket),
			okResult(`{"Policy":"{\"Statement\":[{\"Condition\":{\"ArnNotEquals\":{\"aws:PrincipalArn\":\"arn:aws:iam::123456789012:user/bob\"}}}]}"}`), "NG  " + stage5Descs[4]},
	} {
		t.Run(name+"なら NG", func(t *testing.T) {
			t.Parallel()

			lines, err := runStepLines(t, verifyFake(t, stage5Responses(t), map[string]awsResult{tc.key: tc.body}), stage5Steps(n))

			require.ErrorIs(t, err, errMismatch)
			assert.Contains(t, lines, tc.line)
			assert.Equal(t, "# 結果: 5 件のうち NG 1 件", lines[len(lines)-1])
		})
	}

	t.Run("state の鍵の ARN を読めなければ、鍵と比べる4行が理由を添えた NG", func(t *testing.T) {
		t.Parallel()

		f := verifyFake(t, stage5Responses(t), map[string]awsResult{jsonKey("kms", "describe-key", "--key-id", testAlias): awsError("NotFoundException")})

		lines, err := runStepLines(t, f, stage5Steps(n))

		require.ErrorIs(t, err, errMismatch)

		for _, d := range stage5Descs[:4] {
			assert.Contains(t, lines, "NG  "+d+"（state の鍵の ARN を読めない: 呼び出しが失敗: An error occurred (NotFoundException) when calling the X operation: detail）")
		}

		assert.Equal(t, "# 結果: 5 件のうち NG 4 件", lines[len(lines)-1])
	})

	for name, tc := range map[string]struct {
		r     awsResult
		first string
	}{
		"応答がすべて空":    {okResult(""), "state の鍵の ARN を読めない: 応答を読めない"},
		"呼び出しがすべて失敗": {failed("An error occurred (AccessDenied)"), "state の鍵の ARN を読めない: 呼び出しが失敗: An error occurred (AccessDenied)"},
	} {
		t.Run(name+"なら、全行が NG", func(t *testing.T) {
			t.Parallel()

			allNG(t, stage5Steps(n), stage5Descs, tc.r, tc.first)
		})
	}
}

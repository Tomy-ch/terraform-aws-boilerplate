package main

import (
	"bytes"
	"encoding/json"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/cientry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testAccount     = "123456789012"
	testBucket      = "tabp-admin-tfstate-123456789012-ap-northeast-1-an"
	testPlanBucket  = "tabp-admin-tfplan-123456789012-ap-northeast-1-an"
	testApplyBucket = "tabp-admin-tfapply-123456789012-ap-northeast-1-an"
	testKeyARN      = "arn:aws:kms:ap-northeast-1:123456789012:key/k-1"
	testAlias       = "alias/tabp-admin-tfstate"
	stsAccountKey   = "sts get-caller-identity --query Account --output text --region " + testRegion
)

func testNames(executor string) names {
	return newNames(verifyOptions{project: testProject, region: testRegion, executor: executor}, testAccount)
}

// s3Key は、所有者を確かめる s3api の呼び出しの引数列。
func s3Key(sub, bucket string, extra ...string) string {
	return jsonKey(append([]string{"s3api", sub, "--bucket", bucket, "--expected-bucket-owner", testAccount}, extra...)...)
}

func policyResult(t *testing.T, doc string) awsResult {
	t.Helper()

	b, err := json.Marshal(map[string]string{"Policy": doc})
	require.NoError(t, err)

	return okResult(string(b))
}

func awsError(code string) awsResult {
	return failed("\nAn error occurred (" + code + ") when calling the X operation: detail\n")
}

func verifyFake(t *testing.T, responses map[string]awsResult, override map[string]awsResult) *fakeAWS {
	t.Helper()

	merged := map[string]awsResult{}
	for k, v := range responses {
		merged[k] = v
	}

	for k, v := range override {
		merged[k] = v
	}

	return &fakeAWS{t: t, responses: merged}
}

func runStepLines(t *testing.T, f *fakeAWS, steps []step) ([]string, error) {
	t.Helper()

	var out bytes.Buffer

	err := runChecks(newQuery(f.fn, testRegion), &out, steps)

	return strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n"), err
}

func okLines(descs []string) []string {
	out := make([]string, 0, len(descs)+1)
	for _, d := range descs {
		out = append(out, "ok  "+d)
	}

	return out
}

func verifyDeps(f *fakeAWS, files map[string]string) deps {
	return deps{aws: f.fn, readFile: func(p string) ([]byte, error) {
		if s, ok := files[p]; ok {
			return []byte(s), nil
		}

		return nil, os.ErrNotExist
	}, getenv: func(k string) string {
		if k == "AWS_PROFILE" {
			return "tabp-admin"
		}

		return ""
	}}
}

func Test_parseVerifyArgs(t *testing.T) {
	t.Parallel()

	base := []string{"-project", "tabp", "-region", testRegion}

	for name, tc := range map[string]struct {
		args []string
		want verifyOptions
	}{
		"stage1": {append([]string{"-stage", "stage1"}, base...), verifyOptions{stage: "stage1", project: "tabp", region: testRegion}},
		"stage4 に実行者": {append([]string{"-stage", "stage4", "-executor", "bob"}, base...),
			verifyOptions{stage: "stage4", project: "tabp", region: testRegion, executor: "bob"}},
		"output-policy の既定の出所は live": {append([]string{"-stage", "output-policy"}, base...),
			verifyOptions{stage: "output-policy", project: "tabp", region: testRegion, policySource: "live"}},
		"output-policy に reference と置き場": {append([]string{"-stage", "output-policy", "-policy-source", "reference", "-reference-dir", "tmp/tf-policy/run"}, base...),
			verifyOptions{stage: "output-policy", project: "tabp", region: testRegion, policySource: "reference", referenceDir: "tmp/tf-policy/run"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, err := parseVerifyArgs(tc.args)

			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}

	for _, stage := range []string{"before-apply", "permissions", "stage5"} {
		t.Run(stage+" を読む", func(t *testing.T) {
			t.Parallel()

			got, err := parseVerifyArgs(append([]string{"-stage", stage}, base...))

			require.NoError(t, err)
			assert.Equal(t, stage, got.stage)
		})
	}

	for name, args := range map[string][]string{
		"引数なし":                   nil,
		"stage なし":               base,
		"知らない stage":             append([]string{"-stage", "stage2"}, base...),
		"project なし":             {"-stage", "stage1", "-region", testRegion},
		"region なし":              {"-stage", "stage1", "-project", "tabp"},
		"余分な引数":                  append(append([]string{"-stage", "stage1"}, base...), "x"),
		"知らない flag":              append([]string{"-stage", "stage1", "-x"}, base...),
		"stage4 でない段に実行者":        append([]string{"-stage", "stage5", "-executor", "bob"}, base...),
		"output-policy でない段に出所":  append([]string{"-stage", "stage4", "-policy-source", "live"}, base...),
		"知らない出所":                 append([]string{"-stage", "output-policy", "-policy-source", "x"}, base...),
		"reference に置き場が無い":      append([]string{"-stage", "output-policy", "-policy-source", "reference"}, base...),
		"reference に空の置き場":       append([]string{"-stage", "output-policy", "-policy-source", "reference", "-reference-dir", ""}, base...),
		"live に置き場":              append([]string{"-stage", "output-policy", "-policy-source", "live", "-reference-dir", "tmp/tf-policy/run"}, base...),
		"出所の既定（live）に置き場":        append([]string{"-stage", "output-policy", "-reference-dir", "tmp/tf-policy/run"}, base...),
		"output-policy でない段に置き場": append([]string{"-stage", "permissions", "-reference-dir", "tmp/tf-policy/run"}, base...),
	} {
		t.Run(name+"は使い方の誤り", func(t *testing.T) {
			t.Parallel()

			_, err := parseVerifyArgs(args)

			require.ErrorIs(t, err, errUsage)
		})
	}
}

func Test_ciRole(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "plan", ciRole(cientry.Plan, cientry.RootIdentity))
	assert.Equal(t, "apply-identity", ciRole(cientry.Apply, cientry.RootIdentity))
	assert.Equal(t, "apply", ciRole(cientry.Apply, cientry.RootWorkload))
	assert.Equal(t, "unlock-identity", ciRole(cientry.Unlock, cientry.RootIdentity))
	assert.Equal(t, "unlock", ciRole(cientry.Unlock, cientry.RootWorkload))
	assert.Panics(t, func() { ciRole("none", cientry.RootIdentity) }, "登録簿に無い種別")
	assert.Panics(t, func() { ciRole(cientry.Plan, cientry.Root("x")) }, "知らない root の種類")
}

func Test_newNames(t *testing.T) {
	t.Parallel()

	n := newNames(verifyOptions{project: "tabp", region: testRegion, executor: "bob"}, testAccount)

	assert.Equal(t, names{
		account: testAccount, region: testRegion, project: "tabp", prefix: "tabp-admin", executor: "bob",
		bucket: testBucket, planBucket: testPlanBucket, applyBucket: testApplyBucket, keyAlias: testAlias,
		plan: "plan", apply: "apply", applyIdentity: "apply-identity", unlockIdentity: "unlock-identity", unlock: "unlock",
	}, n)
}

func Test_names_ciRoles(t *testing.T) {
	t.Parallel()

	got := testNames("").ciRoles()

	assert.Equal(t, []string{"plan", "apply", "apply-identity", "unlock-identity", "unlock"}, got)
	assert.ElementsMatch(t, cientry.Roles(), got, "登録簿の role をすべて、1回ずつ確かめる")
}

func Test_names_roleName(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "tabp-admin-ci-plan", testNames("").roleName("plan"))
}

func Test_names_roleARN(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "arn:aws:iam::123456789012:role/tabp-admin-ci-apply", testNames("").roleARN("apply"))
}

func Test_names_policyARN(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "arn:aws:iam::123456789012:policy/tabp-admin-ci-apply-workload", testNames("").policyARN("apply-workload"))
}

func Test_names_s3(t *testing.T) {
	t.Parallel()

	n := testNames("")

	assert.Equal(t, []string{"s3api", "head-object", "--bucket", "b", "--expected-bucket-owner", testAccount, "--key", "k"}, n.s3("head-object", "b", "--key", "k"))
	assert.Equal(t, []string{"s3api", "get-bucket-policy", "--bucket", "b", "--expected-bucket-owner", testAccount}, n.s3("get-bucket-policy", "b"))
}

func Test_noteStep(t *testing.T) {
	t.Parallel()

	s := noteStep("# x")

	assert.Equal(t, "# x", s.note)
	assert.Nil(t, s.check.eval)
}

func Test_skipStep(t *testing.T) {
	t.Parallel()

	s := skipStep("  注記")

	assert.Equal(t, "  注記", s.note)
	assert.True(t, s.skipped)
	assert.Nil(t, s.check.eval)
}

func Test_checkStep(t *testing.T) {
	t.Parallel()

	s := checkStep(fixedCheck("d", verdict{ok: true}))

	assert.Equal(t, "d", s.check.desc)
	assert.NotNil(t, s.check.eval)
}

func Test_fixedCheck(t *testing.T) {
	t.Parallel()

	c := fixedCheck("d", verdict{reason: "r"})
	v, err := c.eval(nil)

	require.NoError(t, err)
	assert.Equal(t, "d", c.desc)
	assert.Equal(t, verdict{reason: "r"}, v)
}

func Test_runChecks(t *testing.T) {
	t.Parallel()

	t.Run("見出しは数えず、ok と NG を数えて結果の行に出す", func(t *testing.T) {
		t.Parallel()

		lines, err := runStepLines(t, verifyFake(t, nil, nil), []step{
			noteStep("# 区分"),
			checkStep(fixedCheck("a", verdict{ok: true})),
			noteStep("  注記"),
			checkStep(fixedCheck("b", verdict{})),
			checkStep(fixedCheck("c", verdict{reason: "理由"})),
		})

		require.ErrorIs(t, err, errMismatch)
		assert.Equal(t, []string{"# 区分", "ok  a", "  注記", "NG  b", "NG  c（理由）", "# 結果: 3 件のうち NG 2 件"}, lines)
	})

	t.Run("すべて ok ならエラーを返さない", func(t *testing.T) {
		t.Parallel()

		lines, err := runStepLines(t, verifyFake(t, nil, nil), []step{checkStep(fixedCheck("a", verdict{ok: true}))})

		require.NoError(t, err)
		assert.Equal(t, []string{"ok  a", "# 結果: 1 件のうち NG 0 件"}, lines)
	})

	for name, steps := range map[string][]step{
		"空":     nil,
		"見出しだけ": {noteStep("# 区分")},
	} {
		t.Run("確認が"+name+"なら errNoChecks で、結果の行を出さない", func(t *testing.T) {
			t.Parallel()

			lines, err := runStepLines(t, verifyFake(t, nil, nil), steps)

			require.ErrorIs(t, err, errNoChecks)
			assert.NotContains(t, strings.Join(lines, "\n"), "# 結果:")
		})
	}

	t.Run("確かめなかった区分があれば、その数を結果の行に添える", func(t *testing.T) {
		t.Parallel()

		ng := checkStep(fixedCheck("c", verdict{reason: "r"}))
		ng.skipped = true

		lines, err := runStepLines(t, verifyFake(t, nil, nil), []step{
			checkStep(fixedCheck("a", verdict{ok: true})),
			skipStep("  飛ばした"),
			ng,
		})

		require.ErrorIs(t, err, errMismatch)
		assert.Equal(t, []string{"ok  a", "  飛ばした", "NG  c（r）", "# 結果: 2 件のうち NG 1 件（確かめていない区分 2）"}, lines)
	})

	t.Run("確かめなかった区分があっても、NG が無ければエラーを返さない", func(t *testing.T) {
		t.Parallel()

		lines, err := runStepLines(t, verifyFake(t, nil, nil), []step{checkStep(fixedCheck("a", verdict{ok: true})), skipStep("  飛ばした")})

		require.NoError(t, err)
		assert.Equal(t, "# 結果: 1 件のうち NG 0 件（確かめていない区分 1）", lines[len(lines)-1])
	})

	t.Run("見出しも判定も持たない行は errBadStep で、結果の行を出さない", func(t *testing.T) {
		t.Parallel()

		lines, err := runStepLines(t, verifyFake(t, nil, nil), []step{checkStep(fixedCheck("a", verdict{ok: true})), {}})

		require.ErrorIs(t, err, errBadStep)
		assert.Equal(t, []string{"ok  a"}, lines)
	})

	t.Run("aws を起動できなくなれば、そこで止まり結果の行を出さない", func(t *testing.T) {
		t.Parallel()

		lines, err := runStepLines(t, verifyFake(t, nil, nil), []step{
			checkStep(fixedCheck("a", verdict{ok: true})),
			checkStep(check{desc: "b", eval: func(*query) (verdict, error) { return verdict{}, errAWSStart }}),
			checkStep(fixedCheck("c", verdict{ok: true})),
		})

		require.ErrorIs(t, err, errAWSStart)
		assert.Equal(t, []string{"ok  a"}, lines)
	})
}

func Test_expectError(t *testing.T) {
	t.Parallel()

	args := []string{"iam", "get-role", "--role-name", "r"}

	for name, tc := range map[string]struct {
		r    awsResult
		want verdict
	}{
		"code のエラーなら ok":            {awsError("NoSuchEntity"), verdict{ok: true}},
		"エラーが返らなければ NG":             {okResult(`{}`), verdict{reason: "エラーが返らなかった"}},
		"違う code なら、その code を添える":   {awsError("AccessDenied"), verdict{reason: "NoSuchEntity ではないエラー: AccessDenied"}},
		"code を前方一致で取り違えない":         {awsError("NoSuchEntityException"), verdict{reason: "NoSuchEntity ではないエラー: NoSuchEntityException"}},
		"本文にだけ code があっても ok にしない":  {failed("An error occurred (AccessDenied): NoSuchEntity"), verdict{reason: "NoSuchEntity ではないエラー: AccessDenied"}},
		"code を読めないエラーは code を添えない": {failed("Unable to locate credentials"), verdict{reason: "NoSuchEntity ではないエラー"}},
		"最初の code で比べる":             {failed("An error occurred (Throttling)\nAn error occurred (NoSuchEntity)"), verdict{reason: "NoSuchEntity ではないエラー: Throttling"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := verifyFake(t, map[string]awsResult{jsonKey(args...): tc.r}, nil)

			v, err := expectError("d", "NoSuchEntity", args...).eval(newQuery(f.fn, testRegion))

			require.NoError(t, err)
			assert.Equal(t, tc.want, v)
		})
	}

	t.Run("aws を起動できなければ errAWSStart", func(t *testing.T) {
		t.Parallel()

		q := newQuery(func(...string) awsResult { return awsResult{err: os.ErrNotExist} }, testRegion)

		_, err := expectError("d", "NoSuchEntity", args...).eval(q)

		require.ErrorIs(t, err, errAWSStart)
	})
}

func Test_query_document(t *testing.T) {
	t.Parallel()

	args := []string{"kms", "get-key-policy"}

	for name, tc := range map[string]struct {
		r          awsResult
		want       map[string]any
		wantReason string
	}{
		"Policy の文書を読む":       {okResult(`{"Policy":"{\"Statement\":[]}"}`), map[string]any{"Statement": []any{}}, ""},
		"呼び出しの失敗は理由を返す":       {failed("denied"), nil, "呼び出しが失敗: denied"},
		"応答が JSON でない":        {okResult(`x`), nil, "応答を読めない"},
		"Policy が無い":          {okResult(`{}`), nil, "応答を読めない"},
		"Policy が文字列でない":      {okResult(`{"Policy":{"Statement":[]}}`), nil, "応答を読めない"},
		"Policy が JSON でない":   {okResult(`{"Policy":"x"}`), nil, "応答を読めない"},
		"Policy が object でない": {okResult(`{"Policy":"[1]"}`), nil, "応答を読めない"},
		"Policy が null":       {okResult(`{"Policy":"null"}`), nil, "応答を読めない"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := verifyFake(t, map[string]awsResult{jsonKey(args...): tc.r}, nil)

			v, reason, err := newQuery(f.fn, testRegion).document(args...)

			require.NoError(t, err)
			assert.Equal(t, tc.want, v)
			assert.Equal(t, tc.wantReason, reason)
		})
	}

	t.Run("aws を起動できなければ errAWSStart", func(t *testing.T) {
		t.Parallel()

		_, _, err := newQuery(func(...string) awsResult { return awsResult{err: os.ErrNotExist} }, testRegion).document(args...)

		require.ErrorIs(t, err, errAWSStart)
	})
}

func Test_expectDocument(t *testing.T) {
	t.Parallel()

	args := []string{"kms", "get-key-policy"}
	pred := func(v map[string]any) bool { return v["a"] == "x" }

	for name, tc := range map[string]struct {
		r    awsResult
		want verdict
	}{
		"述語を満たせば ok": {okResult(`{"Policy":"{\"a\":\"x\"}"}`), verdict{ok: true}},
		"満たさなければ NG": {okResult(`{"Policy":"{\"a\":\"y\"}"}`), verdict{}},
		"読めなければ理由":   {okResult(`{"Policy":""}`), verdict{reason: "応答を読めない"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := verifyFake(t, map[string]awsResult{jsonKey(args...): tc.r}, nil)

			v, err := expectDocument("d", pred, args...).eval(newQuery(f.fn, testRegion))

			require.NoError(t, err)
			assert.Equal(t, tc.want, v)
		})
	}

	t.Run("aws を起動できなければ errAWSStart", func(t *testing.T) {
		t.Parallel()

		_, err := expectDocument("d", pred, args...).eval(newQuery(func(...string) awsResult { return awsResult{err: os.ErrNotExist} }, testRegion))

		require.ErrorIs(t, err, errAWSStart)
	})
}

func stepTexts(steps []step) []string {
	out := make([]string, 0, len(steps))

	for _, s := range steps {
		if s.check.eval == nil {
			out = append(out, s.note)
		} else {
			out = append(out, s.check.desc)
		}
	}

	return out
}

func Test_verifySteps(t *testing.T) {
	t.Parallel()

	e := verifyEnv{n: testNames(""), readFile: retentionReader(retentionFile)}

	for stage, want := range map[string][]string{
		"stage1":       stage1Descs,
		"before-apply": beforeApplyDescs,
		"stage4":       stage4Descs(noExecutorDesc),
		"stage5":       stage5Descs,
	} {
		t.Run(stage+" は aws を呼ばずに、その段の表を組む", func(t *testing.T) {
			t.Parallel()

			f := verifyFake(t, nil, nil)
			e := e
			e.q = newQuery(f.fn, testRegion)

			steps, err := verifySteps(stage, e)

			require.NoError(t, err)
			assert.Equal(t, want, stepTexts(steps))
			assert.Empty(t, f.calls)
		})
	}

	t.Run("permissions は、managed policy と鍵を読んで、その段の表を組む", func(t *testing.T) {
		t.Parallel()

		f := permFake(t, okResult(governanceAttached), okResult(workloadAttached))
		e := e
		e.q = newQuery(f.fn, testRegion)

		steps, err := verifySteps("permissions", e)

		require.NoError(t, err)

		want := permExpected(nil, nil, nil, nil)
		for i, l := range want {
			want[i] = strings.TrimPrefix(l, "ok  ")
		}

		assert.Equal(t, want, stepTexts(steps))
		// 鍵 1、2つの role の managed policy の一覧 2、5つの role の inline policy の一覧 5、残る3つの role の managed policy の
		// 一覧 3（2つは読んだ一覧を使い回す）、apply-identity の4つと apply の3つの managed policy の get-policy と
		// get-policy-version 14。
		assert.Len(t, f.calls, 1+2+5+3+14, "表を組むときは、鍵と、5つの role の managed policy の一覧と policy の文書だけを読む")
	})

	t.Run("output-policy は、文書を読んで、その段の表を組む", func(t *testing.T) {
		t.Parallel()

		f := verifyFake(t, map[string]awsResult{
			s3Key("get-bucket-policy", testPlanBucket):  policyResult(t, `{"Statement":[{"Sid":"p"}]}`),
			s3Key("get-bucket-policy", testApplyBucket): policyResult(t, `{"Statement":[{"Sid":"a"}]}`),
		}, nil)
		e := e
		e.q, e.source = newQuery(f.fn, testRegion), sourceLive

		steps, err := verifySteps("output-policy", e)

		require.NoError(t, err)

		want := []string{"# 文書の出所: live", "plan の出力の Bucket の Bucket Policy の文書を読めた", "apply の出力の Bucket の Bucket Policy の文書を読めた", "# plan の出力の Bucket"}
		want = append(append(append(want, expand(outputPolicyPlanDescs, "")...), "# apply の出力の Bucket"), expand(outputPolicyApplyDescs, "")...)

		assert.Equal(t, want, stepTexts(steps))
	})

	t.Run("知らない段は使い方の誤り", func(t *testing.T) {
		t.Parallel()

		_, err := verifySteps("stage2", e)

		require.ErrorIs(t, err, errUsage)
	})

	for _, stage := range []string{"permissions", "output-policy"} {
		t.Run(stage+" は表を組むときに aws を起動できなければ errAWSStart", func(t *testing.T) {
			t.Parallel()

			e := e
			e.source = sourceLive
			e.q = newQuery(func(...string) awsResult { return awsResult{err: os.ErrNotExist} }, testRegion)

			_, err := verifySteps(stage, e)

			require.ErrorIs(t, err, errAWSStart)
		})
	}
}

// writeOps は、aws の操作名が読むだけのものでない呼び出し。
func writeOps(calls []string) []string {
	var out []string

	for _, c := range calls {
		f := strings.Fields(c)
		if len(f) < 2 || !slices.ContainsFunc([]string{"get-", "list-", "describe-", "head-", "simulate-"}, func(p string) bool {
			return strings.HasPrefix(f[1], p)
		}) {
			out = append(out, c)
		}
	}

	return out
}

// permissiveHandler は、どの読み取りにも応答する。simulate は要求した action をすべて allowed で返し、
// managed policy は条件つきの確認がすべて走る付き方で返し、policy の文書はどの role でも読める。
func permissiveHandler(args []string) (awsResult, bool) {
	joined := strings.Join(args, " ")

	switch {
	case slices.Contains(args, "simulate-principal-policy"):
		return simResult(simActions(args), "allowed"), true
	case strings.Contains(joined, "list-attached-role-policies --role-name tabp-admin-ci-apply-identity "):
		return okResult(`{"AttachedPolicies":[{"PolicyName":"tabp-admin-ci-apply-identity-governance","PolicyArn":"arn:aws:iam::123456789012:policy/tabp-admin-ci-apply-identity-governance"},{"PolicyName":"tabp-admin-ci-apply-identity-members","PolicyArn":"arn:aws:iam::123456789012:policy/tabp-admin-ci-apply-identity-members"}]}`), true
	case strings.Contains(joined, "list-attached-role-policies --role-name tabp-admin-ci-apply "):
		return okResult(`{"AttachedPolicies":[{"PolicyName":"tabp-admin-ci-apply-workload","PolicyArn":"arn:aws:iam::123456789012:policy/tabp-admin-ci-apply-workload"},{"PolicyName":"tabp-admin-ci-apply-workload-2","PolicyArn":"arn:aws:iam::123456789012:policy/tabp-admin-ci-apply-workload-2"},{"PolicyName":"tabp-admin-ci-apply-workload-read","PolicyArn":"arn:aws:iam::123456789012:policy/tabp-admin-ci-apply-workload-read"}]}`), true
	case strings.Contains(joined, "describe-key"):
		return okResult(`{"KeyMetadata":{"Arn":"` + testKeyARN + `"}}`), true
	case strings.Contains(joined, "get-bucket-policy"):
		return okResult(`{"Policy":"{\"Statement\":[{\"Effect\":\"Deny\"}]}"}`), true
	case slices.Contains(args, "list-attached-role-policies"):
		// ほかの role の managed policy は、段4 の期待に当たらない1つ。
		return okResult(`{"AttachedPolicies":[{"PolicyName":"x","PolicyArn":"arn:aws:iam::123456789012:policy/x"}]}`), true
	}

	if r, ok := roleDocsHandler(args); ok {
		return r, true
	}

	return okResult(`{}`), true
}

func Test_checkVerify(t *testing.T) {
	t.Parallel()

	opts := verifyOptions{stage: "stage1", project: testProject, region: testRegion}

	run := func(t *testing.T, o verifyOptions, d deps) ([]string, error) {
		t.Helper()

		var out bytes.Buffer

		err := checkVerify(o, &out, d)

		return strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n"), err
	}

	t.Run("見出しに段・profile・region を出し、段の確認を回す", func(t *testing.T) {
		t.Parallel()

		f := verifyFake(t, stage1Responses(), map[string]awsResult{stsAccountKey: okResult(testAccount + "\n")})

		lines, err := run(t, opts, verifyDeps(f, nil))

		require.NoError(t, err)
		assert.Equal(t, append(append([]string{"# stage1（profile: tabp-admin、region: ap-northeast-1）"}, okLines(stage1Descs)...),
			"# 結果: 4 件のうち NG 0 件"), lines)
		assert.Equal(t, stsAccountKey, f.calls[0], "アカウント ID は sts から取る")
	})

	t.Run("NG があれば errMismatch", func(t *testing.T) {
		t.Parallel()

		f := verifyFake(t, stage1Responses(), map[string]awsResult{
			stsAccountKey: okResult(testAccount), s3Key("get-bucket-versioning", testBucket): okResult(`{"Status":"Suspended"}`),
		})

		lines, err := run(t, opts, verifyDeps(f, nil))

		require.ErrorIs(t, err, errMismatch)
		assert.Equal(t, "# 結果: 4 件のうち NG 1 件", lines[len(lines)-1])
	})

	t.Run("静的な資格情報が環境に在れば aws を起動せずに errStaticCredentials", func(t *testing.T) {
		t.Parallel()

		f := verifyFake(t, nil, nil)
		d := verifyDeps(f, nil)
		d.getenv = func(k string) string { return map[string]string{"AWS_PROFILE": "p", "AWS_ACCESS_KEY_ID": "a"}[k] }

		lines, err := run(t, opts, d)

		require.ErrorIs(t, err, errStaticCredentials)
		assert.Empty(t, f.calls)
		assert.Equal(t, []string{""}, lines)
	})

	t.Run("AWS_PROFILE が空なら aws を起動せずに errNoProfile", func(t *testing.T) {
		t.Parallel()

		f := verifyFake(t, nil, nil)
		d := verifyDeps(f, nil)
		d.getenv = func(string) string { return "" }

		lines, err := run(t, opts, d)

		require.ErrorIs(t, err, errNoProfile)
		assert.Empty(t, f.calls)
		assert.Equal(t, []string{""}, lines)
	})

	for name, tc := range map[string]struct {
		r    awsResult
		want string
	}{
		"11桁":     {okResult("12345678901"), `sts get-caller-identity の Account: "12345678901": `},
		"13桁":     {okResult("1234567890123"), `sts get-caller-identity の Account: "1234567890123": `},
		"空":       {okResult(""), `sts get-caller-identity の Account: "": `},
		"数字でない":   {okResult("None"), `sts get-caller-identity の Account: "None": `},
		"sts の失敗": {failed("ExpiredToken"), "sts get-caller-identity が失敗: ExpiredToken: "},
	} {
		t.Run("アカウント ID が"+name+"なら、確認をせずに errNoAccount", func(t *testing.T) {
			t.Parallel()

			f := verifyFake(t, nil, map[string]awsResult{stsAccountKey: tc.r})

			lines, err := run(t, opts, verifyDeps(f, nil))

			require.ErrorIs(t, err, errNoAccount)
			assert.Equal(t, tc.want+errNoAccount.Error(), err.Error())
			assert.Equal(t, []string{""}, lines)
			assert.Len(t, f.calls, 1)
		})
	}

	t.Run("aws を起動できなければ errAWSStart", func(t *testing.T) {
		t.Parallel()

		d := verifyDeps(verifyFake(t, nil, nil), nil)
		d.aws = func(...string) awsResult { return awsResult{err: os.ErrNotExist} }

		_, err := run(t, opts, d)

		require.ErrorIs(t, err, errAWSStart)
	})

	t.Run("表を組むときに aws を起動できなくなれば、見出しを出さずに errAWSStart", func(t *testing.T) {
		t.Parallel()

		f := verifyFake(t, nil, map[string]awsResult{stsAccountKey: okResult(testAccount)})
		d := verifyDeps(f, nil)
		d.aws = func(args ...string) awsResult {
			if len(args) > 1 && args[1] == "get-caller-identity" {
				return f.fn(args...)
			}

			return awsResult{err: os.ErrNotExist}
		}

		lines, err := run(t, verifyOptions{stage: "permissions", project: testProject, region: testRegion}, d)

		require.ErrorIs(t, err, errAWSStart)
		assert.Equal(t, []string{""}, lines)
	})

	// permissionsNG は、permissiveHandler（すべて allowed、委任管理者の policy は付いていない）に対して、allowed を
	// 期待しない行の数を区分ごとに表と独立に数えた和。
	permissionsNG := 15 + // S3 と IAM
		8 + // state の鍵
		13 + 19 + // 統制の構成物（条件つきを含む）
		5 + // 委任管理者の指定（条件つきは走らない）
		3 + // Chatbot の service-linked role
		12 + 15 + // workload（条件つきを含む）
		6 + 0 + // budgets（条件つきを含む）
		4 + // unlock-identity
		5 // unlock

	// 結果の行は、permissiveHandler の応答に対して表と独立に数えたもの。
	for _, tc := range []struct {
		o      verifyOptions
		result string
	}{
		{verifyOptions{stage: "stage1"}, "# 結果: 4 件のうち NG 4 件"},
		{verifyOptions{stage: "before-apply"}, "# 結果: 25 件のうち NG 25 件"},
		{verifyOptions{stage: "stage4"}, "# 結果: 25 件のうち NG 23 件"},
		{verifyOptions{stage: "stage4", executor: "bob"}, "# 結果: 25 件のうち NG 24 件"},
		{verifyOptions{stage: "permissions"}, "# 結果: " + strconv.Itoa(permissionsBase+governanceGated+workloadGatedCount+budgetsGatedCount) + " 件のうち NG " + strconv.Itoa(permissionsNG) + " 件（確かめていない区分 1）"},
		{verifyOptions{stage: "stage5"}, "# 結果: 5 件のうち NG 4 件"},
		{verifyOptions{stage: "output-policy", policySource: "live"}, "# 結果: 13 件のうち NG 7 件"},
		{verifyOptions{stage: "output-policy", policySource: "reference", referenceDir: testReferenceDir}, "# 結果: 13 件のうち NG 7 件"},
	} {
		o := tc.o
		t.Run(o.stage+o.executor+o.policySource+" は読むだけの操作（get-・list-・describe-・head-・simulate-）しか呼ばない", func(t *testing.T) {
			t.Parallel()

			f := &fakeAWS{t: t, responses: map[string]awsResult{stsAccountKey: okResult(testAccount)}, handler: permissiveHandler}
			d := verifyDeps(f, map[string]string{identityMain: "state_noncurrent_version_retention_days = 90\n", testReferenceList: testReferenceDir + "/runs/state-backend.jsonl\n", testReferencePlan: referencePlanJSON(t)})
			o.project, o.region = testProject, testRegion

			lines, err := run(t, o, d)

			require.ErrorIs(t, err, errMismatch)
			assert.Equal(t, tc.result, lines[len(lines)-1])
			assert.Greater(t, len(f.calls), 1, "sts のほかに段の読み取りを呼んだ")
			// query.call のガードが書き込みの操作を止めるので、これはガードを外したときの保険。
			assert.Empty(t, writeOps(f.calls))
		})
	}

}

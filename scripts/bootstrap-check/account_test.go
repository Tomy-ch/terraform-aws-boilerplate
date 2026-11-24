package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testRegion  = "ap-northeast-1"
	testProject = "tabp"
	testUser    = "alice"
	testARN     = "arn:aws:iam::123456789012:user/alice"
	monitorARN  = "arn:aws:ce::123456789012:anomalymonitor/m-1"
	subARN      = "arn:aws:ce::123456789012:anomalysubscription/s-1"
	// accountCheckCount は、確認の表の件数を表と独立に数えたもの（ADR-0702 決定15）。
	accountCheckCount = 15
)

// accountDescs は、確認の表の desc を、表と独立に書いたもの。順序も出力の契約である。
var accountDescs = []string{
	"1-0 アカウントが Paid plan",
	"1-1 root に MFA が在る（2台以上かはコンソールで見る）",
	"1-3 一時 IAM user のポリシーが AdministratorAccess と SignInLocalDevelopmentAccess だけ",
	"1-3 一時 IAM user の path が /",
	"1-3 一時 IAM user に MFA が在る",
	"1-3 一時 IAM user にアクセスキーが無い",
	"1-4 root にアクセスキーが無い",
	"2-1 連絡先に氏名・住所・電話番号が在る",
	"2-1 代替連絡先 BILLING が在る",
	"2-1 代替連絡先 OPERATIONS が在る",
	"2-1 代替連絡先 SECURITY が在る",
	"2-2 Cost Anomaly Detection の monitor が在る",
	"2-2 Cost Anomaly Detection の通知が在る",
	"2-4 AWS services monitor に tag tabp:bootstrap が在る",
	"2-4 AWS services monitor の通知に tag tabp:bootstrap が在る",
}

// fakeAWS は、引数列（--output と --region を含む）ごとの応答を返す awsFunc を作る。登録の無い呼び出しは
// テストを失敗にし、呼ばれた引数列を calls に残す。
// handler は、responses に無い呼び出しに応答する（偽なら登録の無い呼び出し）。
type fakeAWS struct {
	t         *testing.T
	mu        sync.Mutex
	responses map[string]awsResult
	handler   func(args []string) (awsResult, bool)
	calls     []string
}

func (f *fakeAWS) fn(args ...string) awsResult {
	f.mu.Lock()
	defer f.mu.Unlock()

	key := strings.Join(args, " ")
	f.calls = append(f.calls, key)

	r, ok := f.responses[key]
	if !ok && f.handler != nil {
		r, ok = f.handler(args)
	}

	if !ok {
		f.t.Errorf("想定していない aws の呼び出し: %s", key)

		return awsResult{err: &exec.ExitError{}}
	}

	return r
}

func jsonKey(args ...string) string {
	return strings.Join(args, " ") + " --output json --region " + testRegion
}

func okResult(body string) awsResult { return awsResult{stdout: []byte(body)} }

func failed(stderr string) awsResult {
	return awsResult{stderr: stderr, err: &exec.ExitError{}}
}

// goodResponses は、15 件すべてが ok になる応答。
func goodResponses() map[string]awsResult {
	return map[string]awsResult{
		"sts get-caller-identity --query Arn --output text --region " + testRegion: okResult(testARN + "\n"),
		jsonKey("freetier", "get-account-plan-state"):                              okResult(`{"accountPlanType":"PAID"}`),
		jsonKey("iam", "get-account-summary"):                                      okResult(`{"SummaryMap":{"AccountMFAEnabled":1,"AccountAccessKeysPresent":0}}`),
		jsonKey("iam", "list-attached-user-policies", "--user-name", testUser): okResult(
			`{"AttachedPolicies":[{"PolicyName":"SignInLocalDevelopmentAccess"},{"PolicyName":"AdministratorAccess"}]}`),
		jsonKey("iam", "get-user", "--user-name", testUser):                                   okResult(`{"User":{"Path":"/"}}`),
		jsonKey("iam", "list-mfa-devices", "--user-name", testUser):                           okResult(`{"MFADevices":[{}]}`),
		jsonKey("iam", "list-access-keys", "--user-name", testUser):                           okResult(`{"AccessKeyMetadata":[]}`),
		jsonKey("account", "get-contact-information"):                                         okResult(`{"ContactInformation":{"FullName":"x","AddressLine1":"y","PhoneNumber":"z"}}`),
		jsonKey("account", "get-alternate-contact", "--alternate-contact-type", "BILLING"):    okResult(`{"AlternateContact":{"AlternateContactType":"BILLING"}}`),
		jsonKey("account", "get-alternate-contact", "--alternate-contact-type", "OPERATIONS"): okResult(`{"AlternateContact":{"AlternateContactType":"OPERATIONS"}}`),
		jsonKey("account", "get-alternate-contact", "--alternate-contact-type", "SECURITY"):   okResult(`{"AlternateContact":{"AlternateContactType":"SECURITY"}}`),
		jsonKey("ce", "get-anomaly-monitors"): okResult(`{"AnomalyMonitors":[` +
			`{"MonitorArn":"` + monitorARN + `","MonitorType":"DIMENSIONAL","MonitorDimension":"SERVICE"},` +
			`{"MonitorArn":"arn:aws:ce::123456789012:anomalymonitor/m-2","MonitorType":"CUSTOM"}]}`),
		jsonKey("ce", "get-anomaly-subscriptions"): okResult(`{"AnomalySubscriptions":[` +
			`{"SubscriptionArn":"` + subARN + `","MonitorArnList":["` + monitorARN + `"]},` +
			`{"SubscriptionArn":"arn:aws:ce::123456789012:anomalysubscription/s-2","MonitorArnList":["other"]}]}`),
		jsonKey("ce", "list-tags-for-resource", "--resource-arn", monitorARN): okResult(`{"ResourceTags":[{"Key":"tabp:bootstrap","Value":"true"}]}`),
		jsonKey("ce", "list-tags-for-resource", "--resource-arn", subARN):     okResult(`{"ResourceTags":[{"Key":"x","Value":"y"},{"Key":"tabp:bootstrap","Value":"no"}]}`),
	}
}

func newFake(t *testing.T, override map[string]awsResult) *fakeAWS {
	t.Helper()

	responses := goodResponses()
	for k, v := range override {
		responses[k] = v
	}

	return &fakeAWS{t: t, responses: responses}
}

func accountDeps(f *fakeAWS) deps {
	return deps{aws: f.fn, getenv: func(k string) string {
		if k == "AWS_PROFILE" {
			return "tabp-bootstrap"
		}

		return ""
	}}
}

var accountOpts = accountOptions{project: testProject, region: testRegion}

func runAccount(t *testing.T, f *fakeAWS) ([]string, error) {
	t.Helper()

	var out bytes.Buffer

	err := checkAccount(accountOpts, &out, accountDeps(f))

	return strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n"), err
}

func Test_hostAWS(t *testing.T) {
	t.Parallel()

	fake := func(t *testing.T, code string) string {
		t.Helper()

		path := filepath.Join(t.TempDir(), "aws")
		body := "#!/bin/sh\nfor a in \"$@\"; do echo \"$a\"; done\necho \"home=${HOME:-}\" >&2\nexit " + code + "\n"
		require.NoError(t, os.WriteFile(path, []byte(body), 0o700))

		return path
	}

	t.Run("引数をそのまま渡し、stdout と stderr を分けて返す", func(t *testing.T) {
		t.Parallel()

		r := hostAWS(fake(t, "0"))("ce", "get-anomaly-monitors", "--output", "json")

		require.NoError(t, r.err)
		assert.Equal(t, "ce\nget-anomaly-monitors\n--output\njson\n", string(r.stdout))
		assert.Equal(t, "home="+os.Getenv("HOME")+"\n", r.stderr, "環境を子へ引き継ぐ")
	})

	t.Run("非ゼロで終われば ExitError を返し、stderr を残す", func(t *testing.T) {
		t.Parallel()

		r := hostAWS(fake(t, "254"))("iam", "get-user")

		var exitErr *exec.ExitError
		require.ErrorAs(t, r.err, &exitErr)
		assert.Equal(t, 254, exitErr.ExitCode())
		assert.Equal(t, "home="+os.Getenv("HOME")+"\n", r.stderr)
	})

	t.Run("名前は PATH から探す", func(t *testing.T) {
		t.Parallel()

		r := hostAWS("sh")("-c", "echo $0")

		require.NoError(t, r.err)
		assert.Equal(t, "sh\n", string(r.stdout))
	})

	t.Run("起動できなければ ExitError ではない error を返す", func(t *testing.T) {
		t.Parallel()

		r := hostAWS(filepath.Join(t.TempDir(), "missing"))("sts")

		var exitErr *exec.ExitError
		require.Error(t, r.err)
		assert.NotErrorAs(t, r.err, &exitErr)
	})
}

func Test_awsResult_failed(t *testing.T) {
	t.Parallel()

	assert.False(t, okResult("{}").failed())
	assert.True(t, failed("e").failed())
}

func Test_decodeObject(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		in   string
		want map[string]any
		ok   bool
	}{
		"object":    {`{"a":1}`, map[string]any{"a": float64(1)}, true},
		"空の object": {`{}`, map[string]any{}, true},
		"空":         {``, nil, false},
		"JSON でない":  {`x`, nil, false},
		"null":      {`null`, nil, false},
		"配列":        {`[]`, nil, false},
		"文字列":       {`"a"`, nil, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, ok := decodeObject([]byte(tc.in))

			assert.Equal(t, tc.ok, ok)

			if tc.ok {
				assert.Equal(t, tc.want, got)
			}
		})
	}
}

func Test_parseAccountArgs(t *testing.T) {
	t.Parallel()

	t.Run("project と region を受ける", func(t *testing.T) {
		t.Parallel()

		o, err := parseAccountArgs([]string{"-project", "p", "-region", "r"})

		require.NoError(t, err)
		assert.Equal(t, accountOptions{project: "p", region: "r"}, o)
	})

	for name, args := range map[string][]string{
		"引数なし":        nil,
		"project が無い": {"-region", "r"},
		"region が無い":  {"-project", "p"},
		"region が空":   {"-project", "p", "-region", ""},
		"余分な位置引数":     {"-project", "p", "-region", "r", "x"},
		"知らないフラグ":     {"-project", "p", "-region", "r", "-x"},
	} {
		t.Run(name+"は使い方の誤り", func(t *testing.T) {
			t.Parallel()

			_, err := parseAccountArgs(args)
			require.ErrorIs(t, err, errUsage)
		})
	}
}

func Test_newQuery(t *testing.T) {
	t.Parallel()

	q := newQuery(nil, "r")

	assert.Equal(t, "r", q.region)
	assert.NotNil(t, q.cache)
}

func Test_query_call(t *testing.T) {
	t.Parallel()

	t.Run("--output と --region を付け、同じ引数の呼び出しは1回にまとめる", func(t *testing.T) {
		t.Parallel()

		f := newFake(t, nil)
		q := newQuery(f.fn, testRegion)

		for range 2 {
			r, err := q.call("json", "iam", "get-account-summary")
			require.NoError(t, err)
			assert.Contains(t, string(r.stdout), "SummaryMap")
		}

		assert.Equal(t, []string{jsonKey("iam", "get-account-summary")}, f.calls)
	})

	t.Run("aws の非ゼロ終了は結果として返す", func(t *testing.T) {
		t.Parallel()

		q := newQuery(func(...string) awsResult { return failed("boom") }, testRegion)

		r, err := q.call("json", "x")

		require.NoError(t, err)
		assert.Error(t, r.err)
		assert.Equal(t, "boom", r.stderr)
	})

	t.Run("失敗した呼び出しも1回にまとめる", func(t *testing.T) {
		t.Parallel()

		calls := 0
		q := newQuery(func(...string) awsResult {
			calls++

			return failed("boom")
		}, testRegion)

		for range 2 {
			r, err := q.call("json", "x")
			require.NoError(t, err)
			assert.Equal(t, "boom", r.stderr)
		}

		assert.Equal(t, 1, calls)
	})

	t.Run("起動できなければ errAWSStart", func(t *testing.T) {
		t.Parallel()

		q := newQuery(func(...string) awsResult { return awsResult{err: os.ErrNotExist} }, testRegion)

		_, err := q.call("json", "x")
		require.ErrorIs(t, err, errAWSStart)
	})
}

func Test_query_object(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		r      awsResult
		reason string
	}{
		"object は読む":           {okResult(`{"a":1}`), ""},
		"呼び出しの失敗は stderr の1行目": {failed("\n  \nAn error occurred (AccessDenied)\nmore"), "呼び出しが失敗: An error occurred (AccessDenied)"},
		"stderr が空なら不明":        {failed(""), "呼び出しが失敗: 不明"},
		"空は読めない":               {okResult(""), "応答を読めない"},
		"JSON でなければ読めない":       {okResult("not json"), "応答を読めない"},
		"null は読めない":           {okResult("null"), "応答を読めない"},
		"配列は読めない":              {okResult("[]"), "応答を読めない"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			q := newQuery(func(...string) awsResult { return tc.r }, testRegion)

			v, reason, err := q.object("x")

			require.NoError(t, err)
			assert.Equal(t, tc.reason, reason)

			if tc.reason == "" {
				assert.Equal(t, map[string]any{"a": float64(1)}, v)
			} else {
				assert.Nil(t, v)
			}
		})
	}

	t.Run("起動できなければ errAWSStart", func(t *testing.T) {
		t.Parallel()

		q := newQuery(func(...string) awsResult { return awsResult{err: os.ErrNotExist} }, testRegion)

		_, _, err := q.object("x")
		require.ErrorIs(t, err, errAWSStart)
	})
}

func Test_firstLine(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct{ in, want string }{
		"最初の行":       {"a\nb", "a"},
		"空白だけの行は飛ばす": {" \n\t\nb\n", "b"},
		"CR は落とす":    {"a\r\nb", "a"},
		"空なら不明":      {"", "不明"},
		"空白だけなら不明":   {" \n ", "不明"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, firstLine(tc.in))
		})
	}
}

func Test_expectJSON(t *testing.T) {
	t.Parallel()

	pred := func(v map[string]any) bool { return at(v, "a") == "x" }

	for name, tc := range map[string]struct {
		r    awsResult
		want verdict
	}{
		"述語を満たせば ok":      {okResult(`{"a":"x"}`), verdict{ok: true}},
		"満たさなければ理由なしの NG": {okResult(`{"a":"y"}`), verdict{}},
		"読めなければ理由つきの NG":  {okResult(`x`), verdict{reason: "応答を読めない"}},
		"失敗なら理由つきの NG":    {failed("e"), verdict{reason: "呼び出しが失敗: e"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			c := expectJSON("d", pred, "s", "op")
			v, err := c.eval(newQuery(func(...string) awsResult { return tc.r }, testRegion))

			require.NoError(t, err)
			assert.Equal(t, "d", c.desc)
			assert.Equal(t, tc.want, v)
		})
	}

	t.Run("起動できなければ error", func(t *testing.T) {
		t.Parallel()

		_, err := expectJSON("d", pred).eval(newQuery(func(...string) awsResult { return awsResult{err: os.ErrNotExist} }, testRegion))
		require.ErrorIs(t, err, errAWSStart)
	})
}

func Test_accountChecks(t *testing.T) {
	t.Parallel()

	t.Run("件数と desc と順序が、表と独立に書いた一覧と一致する", func(t *testing.T) {
		t.Parallel()

		checks := accountChecks(testUser, testProject)

		require.Len(t, checks, accountCheckCount)
		require.Len(t, accountDescs, accountCheckCount)

		for i, c := range checks {
			assert.Equal(t, accountDescs[i], c.desc)
		}
	})

	t.Run("良い応答ではすべて ok", func(t *testing.T) {
		t.Parallel()

		q := newQuery(newFake(t, nil).fn, testRegion)

		for _, c := range accountChecks(testUser, testProject) {
			v, err := c.eval(q)
			require.NoError(t, err)
			assert.True(t, v.ok, c.desc)
		}
	})

	// 各行の判定の偽。1行だけを悪い応答に替え、その行だけが NG になることを見る。
	for name, tc := range map[string]struct {
		key    string
		body   string
		index  int
		reason string
	}{
		"Free plan":           {jsonKey("freetier", "get-account-plan-state"), `{"accountPlanType":"FREE"}`, 0, ""},
		"plan の型が違う":          {jsonKey("freetier", "get-account-plan-state"), `{"accountPlanType":1}`, 0, ""},
		"ポリシーが余分":             {jsonKey("iam", "list-attached-user-policies", "--user-name", testUser), `{"AttachedPolicies":[{"PolicyName":"AdministratorAccess"},{"PolicyName":"SignInLocalDevelopmentAccess"},{"PolicyName":"X"}]}`, 2, ""},
		"ポリシーが不足":             {jsonKey("iam", "list-attached-user-policies", "--user-name", testUser), `{"AttachedPolicies":[{"PolicyName":"AdministratorAccess"}]}`, 2, ""},
		"ポリシーの名前が無い":          {jsonKey("iam", "list-attached-user-policies", "--user-name", testUser), `{"AttachedPolicies":[{"PolicyName":"AdministratorAccess"},{}]}`, 2, ""},
		"ポリシーの一覧が無い":          {jsonKey("iam", "list-attached-user-policies", "--user-name", testUser), `{}`, 2, ""},
		"path が / でない":        {jsonKey("iam", "get-user", "--user-name", testUser), `{"User":{"Path":"/x/"}}`, 3, ""},
		"MFA が無い":             {jsonKey("iam", "list-mfa-devices", "--user-name", testUser), `{"MFADevices":[]}`, 4, ""},
		"MFA の一覧が無い":          {jsonKey("iam", "list-mfa-devices", "--user-name", testUser), `{}`, 4, ""},
		"アクセスキーが在る":           {jsonKey("iam", "list-access-keys", "--user-name", testUser), `{"AccessKeyMetadata":[{}]}`, 5, ""},
		"アクセスキーの一覧が無い":        {jsonKey("iam", "list-access-keys", "--user-name", testUser), `{}`, 5, ""},
		"氏名が空":                {jsonKey("account", "get-contact-information"), `{"ContactInformation":{"FullName":"","AddressLine1":"y","PhoneNumber":"z"}}`, 7, ""},
		"住所が無い":               {jsonKey("account", "get-contact-information"), `{"ContactInformation":{"FullName":"x","PhoneNumber":"z"}}`, 7, ""},
		"電話番号が無い":             {jsonKey("account", "get-contact-information"), `{"ContactInformation":{"FullName":"x","AddressLine1":"y"}}`, 7, ""},
		"代替連絡先が null":         {jsonKey("account", "get-alternate-contact", "--alternate-contact-type", "OPERATIONS"), `{"AlternateContact":null}`, 9, ""},
		"代替連絡先が無い":            {jsonKey("account", "get-alternate-contact", "--alternate-contact-type", "SECURITY"), `{}`, 10, ""},
		"monitor の tag が無い":   {jsonKey("ce", "list-tags-for-resource", "--resource-arn", monitorARN), `{"ResourceTags":[{"Key":"other","Value":"true"}]}`, 13, ""},
		"monitor の tag が読めない": {jsonKey("ce", "list-tags-for-resource", "--resource-arn", monitorARN), `{"ResourceTags":[{"Value":"true"}]}`, 13, ""},
		"通知の tag が無い":         {jsonKey("ce", "list-tags-for-resource", "--resource-arn", subARN), `{"ResourceTags":[]}`, 14, ""},
		"通知の tag の一覧が無い":      {jsonKey("ce", "list-tags-for-resource", "--resource-arn", subARN), `{}`, 14, ""},
		"通知の tag が読めない":       {jsonKey("ce", "list-tags-for-resource", "--resource-arn", subARN), `nope`, 14, "応答を読めない"},
	} {
		t.Run(name+"なら、その行だけが NG", func(t *testing.T) {
			t.Parallel()

			q := newQuery(newFake(t, map[string]awsResult{tc.key: okResult(tc.body)}).fn, testRegion)

			for i, c := range accountChecks(testUser, testProject) {
				v, err := c.eval(q)
				require.NoError(t, err)

				if i == tc.index {
					assert.Equal(t, verdict{reason: tc.reason}, v, c.desc)
				} else {
					assert.True(t, v.ok, c.desc)
				}
			}
		})
	}

	t.Run("get-account-summary の偽は 1-1 と 1-4 をそれぞれ見る", func(t *testing.T) {
		t.Parallel()

		for body, want := range map[string][2]bool{
			`{"SummaryMap":{"AccountMFAEnabled":0,"AccountAccessKeysPresent":0}}`: {false, true},
			`{"SummaryMap":{"AccountMFAEnabled":1,"AccountAccessKeysPresent":1}}`: {true, false},
			`{"SummaryMap":{}}`: {false, false},
		} {
			q := newQuery(newFake(t, map[string]awsResult{jsonKey("iam", "get-account-summary"): okResult(body)}).fn, testRegion)
			checks := accountChecks(testUser, testProject)

			v1, err := checks[1].eval(q)
			require.NoError(t, err)
			v6, err := checks[6].eval(q)
			require.NoError(t, err)

			assert.Equal(t, want, [2]bool{v1.ok, v6.ok}, body)
		}
	})

	t.Run("monitor の一覧が空なら 2-2 と 2-4 の2行が NG", func(t *testing.T) {
		t.Parallel()

		q := newQuery(newFake(t, map[string]awsResult{jsonKey("ce", "get-anomaly-monitors"): okResult(`{"AnomalyMonitors":[]}`)}).fn, testRegion)
		checks := accountChecks(testUser, testProject)

		var got []verdict

		for _, i := range []int{11, 13, 14} {
			v, err := checks[i].eval(q)
			require.NoError(t, err)

			got = append(got, v)
		}

		assert.Equal(t, []verdict{
			{},
			{reason: "AWS services monitor が1つではない（0 個）"},
			{reason: "monitor を特定できない: AWS services monitor が1つではない（0 個）"},
		}, got)
	})

	t.Run("通知の一覧が空なら 2-2 の通知と 2-4 の通知の2行が NG", func(t *testing.T) {
		t.Parallel()

		q := newQuery(newFake(t, map[string]awsResult{jsonKey("ce", "get-anomaly-subscriptions"): okResult(`{"AnomalySubscriptions":[]}`)}).fn, testRegion)
		checks := accountChecks(testUser, testProject)

		var got []verdict

		for _, i := range []int{12, 13, 14} {
			v, err := checks[i].eval(q)
			require.NoError(t, err)

			got = append(got, v)
		}

		assert.Equal(t, []verdict{{}, {ok: true}, {reason: "その monitor の subscription が1つではない（0 個）"}}, got)
	})

	for name, tc := range map[string]struct {
		body   string
		reason string
	}{
		"SERVICE の monitor が2個":       {`{"AnomalyMonitors":[{"MonitorArn":"a","MonitorDimension":"SERVICE"},{"MonitorArn":"b","MonitorDimension":"SERVICE"}]}`, "AWS services monitor が1つではない（2 個）"},
		"SERVICE の monitor の ARN が無い": {`{"AnomalyMonitors":[{"MonitorDimension":"SERVICE"}]}`, "AWS services monitor の ARN を読めない"},
	} {
		t.Run(name+"なら 2-4 の2行が理由つきの NG", func(t *testing.T) {
			t.Parallel()

			q := newQuery(newFake(t, map[string]awsResult{jsonKey("ce", "get-anomaly-monitors"): okResult(tc.body)}).fn, testRegion)
			checks := accountChecks(testUser, testProject)

			v13, err := checks[13].eval(q)
			require.NoError(t, err)
			v14, err := checks[14].eval(q)
			require.NoError(t, err)

			assert.Equal(t, verdict{reason: tc.reason}, v13)
			assert.Equal(t, verdict{reason: "monitor を特定できない: " + tc.reason}, v14)
		})
	}

	for name, tc := range map[string]struct {
		key   string
		index int
	}{
		"通知の一覧の呼び出しだけ":          {jsonKey("ce", "get-anomaly-subscriptions"), 14},
		"monitor の tag の呼び出しだけ": {jsonKey("ce", "list-tags-for-resource", "--resource-arn", monitorARN), 13},
		"通知の tag の呼び出しだけ":       {jsonKey("ce", "list-tags-for-resource", "--resource-arn", subARN), 14},
	} {
		t.Run(name+"が起動できなければ、2-4 の行は errAWSStart を返す", func(t *testing.T) {
			t.Parallel()

			q := newQuery(newFake(t, map[string]awsResult{tc.key: {err: os.ErrNotExist}}).fn, testRegion)

			_, err := accountChecks(testUser, testProject)[tc.index].eval(q)
			require.ErrorIs(t, err, errAWSStart)
		})
	}

	t.Run("起動できなければ、どの行も error を返す", func(t *testing.T) {
		t.Parallel()

		q := newQuery(func(...string) awsResult { return awsResult{err: os.ErrNotExist} }, testRegion)

		for _, c := range accountChecks(testUser, testProject) {
			_, err := c.eval(q)
			require.ErrorIs(t, err, errAWSStart, c.desc)
		}
	})
}

func Test_serviceMonitor(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		r           awsResult
		arn, reason string
	}{
		"SERVICE の monitor がちょうど1つ": {okResult(`{"AnomalyMonitors":[{"MonitorArn":"a","MonitorDimension":"SERVICE"},{"MonitorArn":"b"}]}`), "a", ""},
		"SERVICE が無い":               {okResult(`{"AnomalyMonitors":[{"MonitorArn":"b","MonitorType":"CUSTOM"}]}`), "", "AWS services monitor が1つではない（0 個）"},
		"SERVICE が2つ":               {okResult(`{"AnomalyMonitors":[{"MonitorArn":"a","MonitorDimension":"SERVICE"},{"MonitorArn":"b","MonitorDimension":"SERVICE"}]}`), "", "AWS services monitor が1つではない（2 個）"},
		"一覧が無い":                     {okResult(`{}`), "", "AWS services monitor が1つではない（0 個）"},
		"ARN が無い":                   {okResult(`{"AnomalyMonitors":[{"MonitorDimension":"SERVICE"}]}`), "", "AWS services monitor の ARN を読めない"},
		"呼び出しの失敗":                   {failed("e"), "", "呼び出しが失敗: e"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			arn, reason, err := serviceMonitor(newQuery(func(...string) awsResult { return tc.r }, testRegion))

			require.NoError(t, err)
			assert.Equal(t, tc.arn, arn)
			assert.Equal(t, tc.reason, reason)
		})
	}
}

func Test_monitorSubscription(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		r           awsResult
		arn, reason string
	}{
		"その monitor の subscription がちょうど1つ": {okResult(`{"AnomalySubscriptions":[{"SubscriptionArn":"s","MonitorArnList":["x","m"]},{"SubscriptionArn":"t","MonitorArnList":["x"]}]}`), "s", ""},
		"その monitor を含まない":                  {okResult(`{"AnomalySubscriptions":[{"SubscriptionArn":"t","MonitorArnList":["x"]}]}`), "", "その monitor の subscription が1つではない（0 個）"},
		"2つある":                              {okResult(`{"AnomalySubscriptions":[{"SubscriptionArn":"s","MonitorArnList":["m"]},{"SubscriptionArn":"t","MonitorArnList":["m"]}]}`), "", "その monitor の subscription が1つではない（2 個）"},
		"subscription の ARN が無い":            {okResult(`{"AnomalySubscriptions":[{"MonitorArnList":["m"]}]}`), "", "その monitor の subscription の ARN を読めない"},
		"MonitorArnList が読めない":              {okResult(`{"AnomalySubscriptions":[{"SubscriptionArn":"s","MonitorArnList":[1]}]}`), "", "その monitor の subscription が1つではない（0 個）"},
		"呼び出しの失敗":                           {failed("e"), "", "呼び出しが失敗: e"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			arn, reason, err := monitorSubscription(newQuery(func(...string) awsResult { return tc.r }, testRegion), "m")

			require.NoError(t, err)
			assert.Equal(t, tc.arn, arn)
			assert.Equal(t, tc.reason, reason)
		})
	}
}

func Test_exactlyOne(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		in          []string
		arn, reason string
	}{
		"1つなら ARN": {[]string{"a"}, "a", ""},
		"0個":       {nil, "", "X が1つではない（0 個）"},
		"2個":       {[]string{"a", "b"}, "", "X が1つではない（2 個）"},
		"空の ARN":   {[]string{""}, "", "X の ARN を読めない"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			arn, reason, err := exactlyOne(tc.in, "X")

			require.NoError(t, err)
			assert.Equal(t, tc.arn, arn)
			assert.Equal(t, tc.reason, reason)
		})
	}
}

func Test_hasTag(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		r    awsResult
		want verdict
	}{
		"key が在れば値によらず ok": {okResult(`{"ResourceTags":[{"Key":"k","Value":"false"}]}`), verdict{ok: true}},
		"key が無ければ NG":     {okResult(`{"ResourceTags":[{"Key":"K","Value":"true"}]}`), verdict{}},
		"一覧が無ければ NG":       {okResult(`{}`), verdict{}},
		"読めなければ理由つき":       {okResult(``), verdict{reason: "応答を読めない"}},
		"失敗なら理由つき":         {failed("e"), verdict{reason: "呼び出しが失敗: e"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var got []string

			q := newQuery(func(args ...string) awsResult {
				got = args

				return tc.r
			}, testRegion)

			v, err := hasTag(q, "arn:x", "k")

			require.NoError(t, err)
			assert.Equal(t, tc.want, v)
			assert.Equal(t, []string{"ce", "list-tags-for-resource", "--resource-arn", "arn:x", "--output", "json", "--region", testRegion}, got)
		})
	}
}

func Test_checkAccount(t *testing.T) {
	t.Parallel()

	t.Run("すべて ok なら、見出し・15行・結果の行を出し、エラーを返さない", func(t *testing.T) {
		t.Parallel()

		lines, err := runAccount(t, newFake(t, nil))

		require.NoError(t, err)

		want := []string{"# アカウントの準備（profile: tabp-bootstrap、user: alice）"}
		for _, d := range accountDescs {
			want = append(want, "ok  "+d)
		}

		want = append(want, "# 結果: 15 件のうち NG 0 件")
		assert.Equal(t, want, lines)
	})

	t.Run("NG があれば差分のエラーを返し、理由は括弧で添える", func(t *testing.T) {
		t.Parallel()

		lines, err := runAccount(t, newFake(t, map[string]awsResult{
			jsonKey("freetier", "get-account-plan-state"): okResult(`{"accountPlanType":"FREE"}`),
			jsonKey("account", "get-contact-information"): failed("An error occurred (AccessDenied)"),
		}))

		require.ErrorIs(t, err, errMismatch)
		assert.Equal(t, "NG  1-0 アカウントが Paid plan", lines[1])
		assert.Equal(t, "NG  2-1 連絡先に氏名・住所・電話番号が在る（呼び出しが失敗: An error occurred (AccessDenied)）", lines[8])
		assert.Equal(t, "# 結果: 15 件のうち NG 2 件", lines[len(lines)-1])
	})

	t.Run("path 付きの user は最後の要素を名前にする", func(t *testing.T) {
		t.Parallel()

		f := newFake(t, map[string]awsResult{
			"sts get-caller-identity --query Arn --output text --region " + testRegion: okResult("arn:aws:iam::123456789012:user/x/y/alice"),
		})

		lines, err := runAccount(t, f)

		require.NoError(t, err)
		assert.Equal(t, "# アカウントの準備（profile: tabp-bootstrap、user: alice）", lines[0])
	})

	for name, tc := range map[string]struct {
		r    awsResult
		want string
	}{
		"assumed-role": {okResult("arn:aws:sts::123456789012:assumed-role/r/s"), "arn:aws:sts::123456789012:assumed-role/r/s"},
		"root":         {okResult("arn:aws:iam::123456789012:root"), "arn:aws:iam::123456789012:root"},
		"不正な文字の user":  {okResult("arn:aws:iam::123456789012:user/a b"), "arn:aws:iam::123456789012:user/a b"},
		"空":            {okResult(""), "取得できず"},
		"sts の失敗":      {failed("expired"), "取得できず"},
	} {
		t.Run(name+"は一時 IAM user ではないとして、確認をせずに止まる", func(t *testing.T) {
			t.Parallel()

			f := newFake(t, map[string]awsResult{"sts get-caller-identity --query Arn --output text --region " + testRegion: tc.r})

			lines, err := runAccount(t, f)

			require.ErrorIs(t, err, errNotTempUser)
			assert.Equal(t, "sts get-caller-identity の Arn: "+tc.want+": "+errNotTempUser.Error(), err.Error())
			assert.Equal(t, []string{""}, lines)
			assert.Len(t, f.calls, 1)
		})
	}

	t.Run("読むだけの操作（get-・list-・describe-・head-・simulate-）しか呼ばない", func(t *testing.T) {
		t.Parallel()

		f := newFake(t, nil)

		_, err := runAccount(t, f)

		require.NoError(t, err)
		assert.Len(t, f.calls, len(goodResponses()))
		assert.Empty(t, writeOps(f.calls))
	})

	t.Run("AWS_PROFILE が空なら aws を起動せずに errNoProfile", func(t *testing.T) {
		t.Parallel()

		f := newFake(t, nil)
		d := accountDeps(f)
		d.getenv = func(string) string { return "" }

		var out bytes.Buffer

		require.ErrorIs(t, checkAccount(accountOpts, &out, d), errNoProfile)
		assert.Empty(t, f.calls)
		assert.Empty(t, out.String())
	})

	t.Run("aws を起動できなければ errAWSStart", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer

		d := deps{aws: func(...string) awsResult { return awsResult{err: os.ErrNotExist} }, getenv: func(string) string { return "p" }}

		require.ErrorIs(t, checkAccount(accountOpts, &out, d), errAWSStart)
	})

	t.Run("確認の途中で aws を起動できなくなれば、結果の行を出さずに errAWSStart", func(t *testing.T) {
		t.Parallel()

		f := newFake(t, nil)
		d := accountDeps(f)
		calls := 0
		d.aws = func(args ...string) awsResult {
			calls++
			if calls > 3 {
				return awsResult{err: os.ErrNotExist}
			}

			return f.fn(args...)
		}

		var out bytes.Buffer

		require.ErrorIs(t, checkAccount(accountOpts, &out, d), errAWSStart)
		assert.NotContains(t, out.String(), "# 結果:")
	})
}

func Test_at(t *testing.T) {
	t.Parallel()

	v := map[string]any{"a": map[string]any{"b": "x"}, "n": nil}

	assert.Equal(t, "x", at(v, "a", "b"))
	assert.Nil(t, at(v, "a", "c"))
	assert.Nil(t, at(v, "a", "b", "c"), "途中が object でなければ nil")
	assert.Nil(t, at(v, "n", "x"))
	assert.Equal(t, v, at(v))
}

func Test_arrayAt(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []any{"a"}, arrayAt([]any{"a"}))
	assert.Nil(t, arrayAt(map[string]any{}))
	assert.Nil(t, arrayAt(nil))
}

func Test_arrayLen(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 2, arrayLen([]any{1, 2}))
	assert.Equal(t, 0, arrayLen([]any{}))
	assert.Equal(t, -1, arrayLen(nil))
	assert.Equal(t, -1, arrayLen(map[string]any{"a": 1}))
}

func Test_stringsAt(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		v    any
		key  string
		want []string
		ok   bool
	}{
		"要素の key":        {[]any{map[string]any{"k": "a"}, map[string]any{"k": "b"}}, "k", []string{"a", "b"}, true},
		"key が空なら要素そのもの": {[]any{"a", "b"}, "", []string{"a", "b"}, true},
		"空の配列":           {[]any{}, "k", []string{}, true},
		"key の無い要素":      {[]any{map[string]any{"k": "a"}, map[string]any{}}, "k", nil, false},
		"文字列でない要素":       {[]any{1}, "", nil, false},
		"配列でない":          {map[string]any{}, "k", nil, false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got, ok := stringsAt(tc.v, tc.key)

			assert.Equal(t, tc.ok, ok)
			assert.Equal(t, tc.want, got)
		})
	}
}

func Test_nonEmpty(t *testing.T) {
	t.Parallel()

	assert.True(t, nonEmpty("a"))
	assert.False(t, nonEmpty(""))
	assert.False(t, nonEmpty(nil))
	assert.False(t, nonEmpty(float64(1)))
}

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/fakeexec"
	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/publicdiag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 合成の値。アカウント ID は 111111111111 だけを使う。
const (
	sessionKey = "ASIAAAAAAAAAAAAAAAAA"
	memberKey  = "ASIABBBBBBBBBBBBBBBB"
)

const regionsTF = `output "by_partition" { value = { aws = toset(["ap-northeast-1", "us-east-1", "us-west-2"]) } }`

const userARN = `{"Arn":"arn:aws:iam::111111111111:user/tabp-trail"}`

var now = time.Date(2026, 11, 27, 6, 0, 0, 0, time.UTC)

func trailJSON(t *testing.T, events ...map[string]any) []byte {
	t.Helper()

	wrapped := make([]map[string]string, 0, len(events))

	for _, ev := range events {
		raw, err := json.Marshal(ev)
		require.NoError(t, err)

		wrapped = append(wrapped, map[string]string{"CloudTrailEvent": string(raw)})
	}

	raw, err := json.Marshal(map[string]any{"Events": wrapped})
	require.NoError(t, err)

	return raw
}

func credentials(key string) map[string]any {
	return map[string]any{"credentials": map[string]any{"accessKeyId": key}}
}

func assumed(session, key string) map[string]any {
	return map[string]any{
		"eventName": "AssumeRoleWithWebIdentity", "requestParameters": map[string]any{"roleSessionName": session},
		"responseElements": credentials(key), "eventTime": "2026-11-27T04:30:00Z",
	}
}

func okEvent() map[string]any {
	return map[string]any{"eventName": "GetRole", "eventTime": "2026-11-27T04:00:00Z"}
}

func deniedEvent(region string) map[string]any {
	return map[string]any{
		"eventTime": "2026-11-27T04:37:21Z", "awsRegion": region, "eventSource": "iam.amazonaws.com", "eventName": "CreateServiceLinkedRole",
		"errorCode": "AccessDenied", "errorMessage": "User: arn:aws:sts::111111111111:assumed-role/tabp-admin-ci-apply-identity/gha-8-1 " +
			"is not authorized to perform: iam:CreateServiceLinkedRole on resource: arn:aws:iam::111111111111:role/x because no identity-based policy allows the iam:CreateServiceLinkedRole action",
	}
}

// fakeAWS は、呼び出しを calls に控え、responses の key（profile と引数を空白で繋いだものの部分文字列）に当たる
// 応答を返す（値が nil なら errAWS）。走査順は不定なので、key は互いに部分文字列にならないようにする。当たらなければ
// 空の Events を返す。get-caller-identity は "<profile> sts" の key で別に扱い、無ければ IAM user を返す。
func fakeAWS(t *testing.T, responses map[string][]byte, calls *[]string) awsFunc {
	t.Helper()

	return func(profile string, args ...string) ([]byte, error) {
		call := strings.TrimSpace(profile + " " + strings.Join(args, " "))
		*calls = append(*calls, call)

		if args[0] == "sts" {
			raw, ok := responses[strings.TrimSpace(profile+" sts")]
			switch {
			case !ok:
				return []byte(userARN), nil
			case raw == nil:
				return nil, errAWS
			}

			return raw, nil
		}

		for k, v := range responses {
			if !strings.HasSuffix(k, "sts") && strings.Contains(call, k) {
				if v == nil {
					return nil, errAWS
				}

				return v, nil
			}
		}

		return []byte(`{"Events":[]}`), nil
	}
}

func testDeps(t *testing.T, responses map[string][]byte, calls *[]string) deps {
	t.Helper()

	return deps{aws: fakeAWS(t, responses, calls), now: func() time.Time { return now }, readFile: os.ReadFile, getenv: func(string) string { return "" }}
}

func regionsFile(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "outputs.tf")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	return path
}

func baseArgs(t *testing.T, extra ...string) []string {
	t.Helper()

	return append([]string{"-run", "8", "-attempt", "1", "-root-region", "ap-northeast-1", "-regions-file", regionsFile(t, regionsTF)}, extra...)
}

// sessionFound は、session の key が見つかり、その key の event が us-east-1 に在る応答。
func sessionFound(t *testing.T, events ...map[string]any) map[string][]byte {
	t.Helper()

	responses := map[string][]byte{}
	responses["AttributeValue=AssumeRoleWithWebIdentity"] = trailJSON(t, assumed("gha-8-1", sessionKey))
	responses["--region us-east-1 --lookup-attributes AttributeKey=AccessKeyId,AttributeValue="+sessionKey] = trailJSON(t, append([]map[string]any{okEvent()}, events...)...)

	return responses
}

func Test_execute(t *testing.T) {
	t.Parallel()

	t.Run("引けたら 0 で、失敗した event の行を出す", func(t *testing.T) {
		t.Parallel()

		var (
			calls []string
			out   bytes.Buffer
		)

		assert.Equal(t, 0, execute(baseArgs(t), &out, testDeps(t, sessionFound(t, deniedEvent("us-east-1")), &calls)))
		assert.Contains(t, out.String(), "診断: kind=iam-deny; time=2026-11-27T04:37:21Z; region=us-east-1; source=iam.amazonaws.com; "+
			"operation=CreateServiceLinkedRole; code=AccessDenied; action=iam:CreateServiceLinkedRole; reason=implicit:identity-based\n")
		assert.NotContains(t, out.String(), "111111111111")
	})

	t.Run("使い方を誤れば 2 で、使い方を出す", func(t *testing.T) {
		t.Parallel()

		var (
			calls []string
			out   bytes.Buffer
		)

		assert.Equal(t, 2, execute([]string{"-run", "x"}, &out, testDeps(t, nil, &calls)))
		assert.Contains(t, out.String(), "使い方")
	})

	t.Run("session が見つからなければ 2 で、その理由を出す", func(t *testing.T) {
		t.Parallel()

		var (
			calls []string
			out   bytes.Buffer
		)

		assert.Equal(t, 2, execute(baseArgs(t), &out, testDeps(t, nil, &calls)))
		assert.Contains(t, out.String(), errSession.Error())
	})

	t.Run("出力を書けなければ 2", func(t *testing.T) {
		t.Parallel()

		var calls []string

		assert.Equal(t, 2, execute(baseArgs(t), failingWriter{}, testDeps(t, sessionFound(t), &calls)))
	})
}

type failingWriter struct{}

var errWrite = errors.New("write failed")

func (failingWriter) Write([]byte) (int, error) { return 0, errWrite }

func Test_hostAWS(t *testing.T) {
	t.Parallel()

	t.Run("profile を先頭に付けて起動し、stdout を返す", func(t *testing.T) {
		t.Parallel()

		bin := filepath.Join(t.TempDir(), "aws")
		fakeexec.Write(t, bin, "#!/bin/sh\necho \"$*\"\necho secret >&2\n", 0o700)

		out, err := hostAWS(bin)("p", "sts", "get-caller-identity")
		require.NoError(t, err)
		assert.Equal(t, "--profile p sts get-caller-identity\n", string(out))
	})

	t.Run("失敗したら errAWS で、stderr を error に入れない", func(t *testing.T) {
		t.Parallel()

		bin := filepath.Join(t.TempDir(), "aws")
		fakeexec.Write(t, bin, "#!/bin/sh\necho 111111111111 >&2\nexit 254\n", 0o700)

		_, err := hostAWS(bin)("", "cloudtrail", "lookup-events")
		require.ErrorIs(t, err, errAWS)
		assert.NotContains(t, err.Error(), "111111111111")
	})
}

func Test_parseArgs(t *testing.T) {
	t.Parallel()

	t.Run("既定の期間は 24 時間", func(t *testing.T) {
		t.Parallel()

		o, err := parseArgs([]string{"-run", "8", "-attempt", "1", "-root-region", "ap-northeast-1", "-regions-file", "f"})
		require.NoError(t, err)
		assert.Equal(t, 24*time.Hour, o.since)
		assert.False(t, o.raw)
	})

	type badArgs struct {
		name string
		args []string
	}

	for _, c := range []badArgs{
		{
			name: "run が数でない",
			args: []string{"-run", "x", "-attempt", "1", "-root-region", "ap-northeast-1", "-regions-file", "f"},
		},
		{
			name: "attempt が 0",
			args: []string{"-run", "8", "-attempt", "0", "-root-region", "ap-northeast-1", "-regions-file", "f"},
		},
		{
			name: "region の形でない",
			args: []string{"-run", "8", "-attempt", "1", "-root-region", "tokyo", "-regions-file", "f"},
		},
		{
			name: "regions-file が無い",
			args: []string{"-run", "8", "-attempt", "1", "-root-region", "ap-northeast-1"},
		},
		{
			name: "期間が正でない",
			args: []string{"-run", "8", "-attempt", "1", "-root-region", "ap-northeast-1", "-regions-file", "f", "-since", "0s"},
		},
		{
			name: "member-profile の形でない",
			args: []string{"-run", "8", "-attempt", "1", "-root-region", "ap-northeast-1", "-regions-file", "f", "-member-profile", "a b"},
		},
		{
			name: "余分な引数",
			args: []string{"-run", "8", "-attempt", "1", "-root-region", "ap-northeast-1", "-regions-file", "f", "x"},
		},
	} {
		t.Run(c.name+"なら errUsage", func(t *testing.T) {
			t.Parallel()

			_, err := parseArgs(c.args)
			require.ErrorIs(t, err, errUsage)
		})
	}
}

func Test_run(t *testing.T) {
	t.Parallel()

	t.Run("session の key を root の region と us-east-1 から集め、集合のすべてのリージョンで引く", func(t *testing.T) {
		t.Parallel()

		var (
			calls []string
			out   bytes.Buffer
		)

		responses := sessionFound(t)
		delete(responses, "AttributeValue=AssumeRoleWithWebIdentity")
		responses["--region us-east-1 --lookup-attributes AttributeKey=EventName,AttributeValue=AssumeRoleWithWebIdentity"] = trailJSON(t,
			assumed("gha-8-1", sessionKey), assumed("gha-9-1", memberKey))

		require.NoError(t, run(baseArgs(t), &out, testDeps(t, responses, &calls)))

		joined := strings.Join(calls, "\n")
		for _, r := range []string{"ap-northeast-1", "us-east-1", "us-west-2"} {
			assert.Contains(t, joined, "--region "+r+" --lookup-attributes AttributeKey=AccessKeyId,AttributeValue="+sessionKey)
		}

		assert.NotContains(t, joined, memberKey, "別の run の session の key を引かない")
		assert.Contains(t, joined, "--start-time 2026-11-26T06:00:00Z")
		assert.Contains(t, out.String(), "CloudTrail: session の key 1 件、メンバーの key 0 件、読めた event 1 件、失敗した event 0 件\n")
	})

	t.Run("session が見つからなければ errSession（何も見ずに緑にしない）", func(t *testing.T) {
		t.Parallel()

		var calls []string

		require.ErrorIs(t, run(baseArgs(t), &bytes.Buffer{}, testDeps(t, nil, &calls)), errSession)
	})

	t.Run("session の event があっても key を読めなければ errSession", func(t *testing.T) {
		t.Parallel()

		var calls []string

		responses := map[string][]byte{}
		responses["AttributeValue=AssumeRoleWithWebIdentity"] = trailJSON(t, assumed("gha-8-1", "AKIAAAAAAAAAAAAAAAAA"))
		require.ErrorIs(t, run(baseArgs(t), &bytes.Buffer{}, testDeps(t, responses, &calls)), errSession)
	})

	t.Run("key の event が1件も見えなければ errNoEvents（反映の遅れを拒否なしと読まない）", func(t *testing.T) {
		t.Parallel()

		var calls []string

		responses := map[string][]byte{}
		responses["AttributeValue=AssumeRoleWithWebIdentity"] = trailJSON(t, assumed("gha-8-1", sessionKey))
		require.ErrorIs(t, run(baseArgs(t), &bytes.Buffer{}, testDeps(t, responses, &calls)), errNoEvents)
	})

	t.Run("メンバーの key は member-profile の資格情報で引き、時刻の順に並べる", func(t *testing.T) {
		t.Parallel()

		var (
			calls []string
			out   bytes.Buffer
		)

		late := deniedEvent("us-east-1")
		late["eventTime"] = "2026-11-27T05:00:00Z"
		responses := sessionFound(t, late, map[string]any{"eventName": "AssumeRole", "responseElements": credentials(memberKey)})
		responses["member cloudtrail"] = trailJSON(t, deniedEvent("ap-northeast-1"))

		require.NoError(t, run(baseArgs(t, "-member-profile", "member"), &out, testDeps(t, responses, &calls)))
		assert.Contains(t, strings.Join(calls, "\n"), "member cloudtrail lookup-events --region ap-northeast-1 --lookup-attributes AttributeKey=AccessKeyId,AttributeValue="+memberKey)
		assert.Contains(t, out.String(), "メンバーの key 1 件")
		assert.Less(t, strings.Index(out.String(), "region=ap-northeast-1"), strings.Index(out.String(), "time=2026-11-27T05:00:00Z"))
		assert.NotContains(t, out.String(), "引いていません")
	})

	t.Run("member-profile が無ければメンバーの key を引かず、引いていないことを出す", func(t *testing.T) {
		t.Parallel()

		var (
			calls []string
			out   bytes.Buffer
		)

		responses := sessionFound(t, map[string]any{"eventName": "AssumeRole", "responseElements": credentials(memberKey)})
		require.NoError(t, run(baseArgs(t), &out, testDeps(t, responses, &calls)))
		assert.NotContains(t, strings.Join(calls, "\n"), "AttributeValue="+memberKey)
		assert.Contains(t, out.String(), "⚠️ メンバーの key 1 件は、-member-profile が無いので引いていません\n")
	})

	t.Run("member-profile の lookup が失敗すれば errAWS", func(t *testing.T) {
		t.Parallel()

		var calls []string

		responses := sessionFound(t, map[string]any{"eventName": "AssumeRole", "responseElements": credentials(memberKey)})
		responses["member cloudtrail"] = nil
		require.ErrorIs(t, run(baseArgs(t, "-member-profile", "member"), &bytes.Buffer{}, testDeps(t, responses, &calls)), errAWS)
	})

	t.Run("root の資格情報なら引かない", func(t *testing.T) {
		t.Parallel()

		var calls []string

		responses := map[string][]byte{"sts": []byte(`{"Arn":"arn:aws:iam::111111111111:root"}`)}
		require.ErrorIs(t, run(baseArgs(t), &bytes.Buffer{}, testDeps(t, responses, &calls)), errRoot)
		assert.Len(t, calls, 1)
	})

	t.Run("member-profile が root なら引かない", func(t *testing.T) {
		t.Parallel()

		var calls []string

		responses := map[string][]byte{"member sts": []byte(`{"Arn":"arn:aws:iam::111111111111:root"}`)}
		require.ErrorIs(t, run(baseArgs(t, "-member-profile", "member"), &bytes.Buffer{}, testDeps(t, responses, &calls)), errRoot)
		assert.Len(t, calls, 2)
	})

	t.Run("get-caller-identity が失敗すれば errAWS で、CloudTrail を引かない", func(t *testing.T) {
		t.Parallel()

		var calls []string

		require.ErrorIs(t, run(baseArgs(t), &bytes.Buffer{}, testDeps(t, map[string][]byte{"sts": nil}, &calls)), errAWS)
		assert.Len(t, calls, 1)
	})

	t.Run("資格情報を変える環境変数が残っていれば aws を起動しない", func(t *testing.T) {
		t.Parallel()

		var calls []string

		d := testDeps(t, nil, &calls)
		d.getenv = func(k string) string {
			if k == "AWS_ACCESS_KEY_ID" {
				return "x"
			}

			return ""
		}
		require.ErrorIs(t, run(baseArgs(t), &bytes.Buffer{}, d), errEnv)
		assert.Empty(t, calls)
	})

	t.Run("リージョンの集合を読めなければ CloudTrail を引かない", func(t *testing.T) {
		t.Parallel()

		var calls []string

		args := []string{"-run", "8", "-attempt", "1", "-root-region", "ap-northeast-1", "-regions-file", filepath.Join(t.TempDir(), "none.tf")}
		require.ErrorIs(t, run(args, &bytes.Buffer{}, testDeps(t, nil, &calls)), errRegions)
		assert.Len(t, calls, 1)
	})

	t.Run("lookup-events が失敗すれば errAWS", func(t *testing.T) {
		t.Parallel()

		var calls []string

		responses := map[string][]byte{"AttributeValue=AssumeRoleWithWebIdentity": nil}
		require.ErrorIs(t, run(baseArgs(t), &bytes.Buffer{}, testDeps(t, responses, &calls)), errAWS)
	})
}

func Test_refuseRoot(t *testing.T) {
	t.Parallel()

	type rootCase struct {
		name string
		body string
		want error
	}

	for _, c := range []rootCase{
		{
			name: "IAM user は通す",
			body: userARN,
		},
		{
			name: "assumed role は通す",
			body: `{"Arn":"arn:aws:sts::111111111111:assumed-role/x/y"}`,
		},
		{
			name: "root は errRoot",
			body: `{"Arn":"arn:aws:iam::111111111111:root"}`,
			want: errRoot,
		},
		{
			name: "読めない応答は errEvents",
			body: `x`,
			want: errEvents,
		},
		{
			name: "Arn の無い応答は errEvents",
			body: `{}`,
			want: errEvents,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			var calls []string

			err := refuseRoot(testDeps(t, map[string][]byte{"p sts": []byte(c.body)}, &calls), "p")
			if c.want == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, c.want)
			}

			assert.Equal(t, []string{"p sts get-caller-identity --output json"}, calls)
		})
	}
}

func Test_defaultRegions(t *testing.T) {
	t.Parallel()

	t.Run("aws の key の toset の literal を読む", func(t *testing.T) {
		t.Parallel()

		got, err := defaultRegions(os.ReadFile, regionsFile(t, regionsTF))
		require.NoError(t, err)
		assert.Equal(t, []string{"ap-northeast-1", "us-east-1", "us-west-2"}, got)
	})

	type badFile struct {
		name string
		body string
	}

	for _, c := range []badFile{
		{
			name: "ファイルが無い",
			body: "",
		},
		{
			name: "HCL でない",
			body: "output {",
		},
		{
			name: "output が無い",
			body: `output "other" { value = 1 }`,
		},
		{
			name: "value が無い",
			body: `output "by_partition" {}`,
		},
		{
			name: "object でない",
			body: `output "by_partition" { value = 1 }`,
		},
		{
			name: "aws の key が無い",
			body: `output "by_partition" { value = { "aws-cn" = toset(["cn-north-1"]) } }`,
		},
		{
			name: "toset でない",
			body: `output "by_partition" { value = { aws = ["us-east-1"] } }`,
		},
		{
			name: "空の集合",
			body: `output "by_partition" { value = { aws = toset([]) } }`,
		},
		{
			name: "リージョンの形でない要素がある",
			body: `output "by_partition" { value = { aws = toset(["us-east-1", "tokyo"]) } }`,
		},
		{
			name: "文字列でない要素がある",
			body: `output "by_partition" { value = { aws = toset(["us-east-1", 1]) } }`,
		},
		{
			name: "literal でない要素がある",
			body: `output "by_partition" { value = { aws = toset([var.x]) } }`,
		},
	} {
		t.Run(c.name+"なら errRegions", func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "none.tf")
			if c.body != "" {
				path = regionsFile(t, c.body)
			}

			_, err := defaultRegions(os.ReadFile, path)
			require.ErrorIs(t, err, errRegions)
		})
	}
}

func Test_partitionRegions(t *testing.T) {
	t.Parallel()

	t.Run("別の partition の key は読まない", func(t *testing.T) {
		t.Parallel()

		got, err := defaultRegions(os.ReadFile, regionsFile(t, `output "by_partition" { value = { "aws-cn" = toset(["cn-north-1"]), aws = toset(["us-east-1"]) } }`))
		require.NoError(t, err)
		assert.Equal(t, []string{"us-east-1"}, got)
	})
}

func Test_regionList(t *testing.T) {
	t.Parallel()

	t.Run("root の region を先頭に、us-east-1 と集合を重ねずに並べる", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, []string{"ap-northeast-1", "us-east-1", "us-west-2"}, regionList("ap-northeast-1", []string{"us-east-1", "ap-northeast-1", "us-west-2"}))
	})

	t.Run("root が集合に無くても先頭に置き、集合の順を保つ", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, []string{"ap-southeast-2", "us-east-1", "us-west-2", "eu-west-1"}, regionList("ap-southeast-2", []string{"us-west-2", "eu-west-1"}))
	})
}

func Test_lookup(t *testing.T) {
	t.Parallel()

	t.Run("Events の CloudTrailEvent を読み、形の読めない event を数える", func(t *testing.T) {
		t.Parallel()

		var calls []string

		body := `{"Events":[{"CloudTrailEvent":"x"},{"CloudTrailEvent":"{\"errorCode\":\"AccessDenied\"}"}]}`
		got, unread, err := lookup(testDeps(t, map[string][]byte{"lookup-events": []byte(body)}, &calls), "", "us-east-1", "EventName", "X", "s")
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, "AccessDenied", got[0].ErrorCode)
		assert.Equal(t, 1, unread)
	})

	type badResp struct {
		name string
		body string
	}

	for _, c := range []badResp{
		{
			name: "JSON でない",
			body: "x",
		},
		{
			name: "Events が無い",
			body: `{}`,
		},
		{
			name: "Events が null",
			body: `{"Events":null}`,
		},
		{
			name: "Events が一覧でない",
			body: `{"Events":{}}`,
		},
	} {
		t.Run(c.name+"なら errEvents（0件と読まない）", func(t *testing.T) {
			t.Parallel()

			var calls []string

			_, _, err := lookup(testDeps(t, map[string][]byte{"lookup-events": []byte(c.body)}, &calls), "", "us-east-1", "EventName", "X", "s")
			require.ErrorIs(t, err, errEvents)
		})
	}
}

func Test_sessionKeys(t *testing.T) {
	t.Parallel()

	t.Run("同じ key を2つのリージョンで見つけても1つに数える", func(t *testing.T) {
		t.Parallel()

		var calls []string

		got, err := sessionKeys(testDeps(t, map[string][]byte{"lookup-events": trailJSON(t, assumed("gha-8-1", sessionKey))}, &calls),
			"s", "gha-8-1", []string{"ap-northeast-1", "us-east-1"})
		require.NoError(t, err)
		assert.Equal(t, []string{sessionKey}, got)
	})

	t.Run("key が形に当たらないか応答が無い event は数えない", func(t *testing.T) {
		t.Parallel()

		var calls []string

		noResponse := map[string]any{"eventName": "AssumeRoleWithWebIdentity", "requestParameters": map[string]any{"roleSessionName": "gha-8-1"}}
		responses := map[string][]byte{"lookup-events": trailJSON(t, assumed("gha-8-1", "AKIAAAAAAAAAAAAAAAAA"), noResponse)}
		got, err := sessionKeys(testDeps(t, responses, &calls), "s", "gha-8-1", []string{"us-east-1"})
		require.NoError(t, err)
		assert.Empty(t, got)
	})
}

func Test_trailEvent_sessionName(t *testing.T) {
	t.Parallel()

	t.Run("要求の roleSessionName を返す", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, "gha-8-1", trailEvent{RequestParameters: json.RawMessage(`{"roleSessionName":"gha-8-1"}`)}.sessionName())
	})

	t.Run("要求が無いか形が違えば空", func(t *testing.T) {
		t.Parallel()

		assert.Empty(t, trailEvent{}.sessionName())
		assert.Empty(t, trailEvent{RequestParameters: json.RawMessage(`"x"`)}.sessionName())
	})
}

func Test_trailEvent_accessKey(t *testing.T) {
	t.Parallel()

	t.Run("一時の access key ID を返す", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, sessionKey, trailEvent{ResponseElements: json.RawMessage(`{"credentials":{"accessKeyId":"` + sessionKey + `"}}`)}.accessKey())
	})

	t.Run("応答が無いか形が違えば空", func(t *testing.T) {
		t.Parallel()

		assert.Empty(t, trailEvent{}.accessKey())
		assert.Empty(t, trailEvent{ResponseElements: json.RawMessage(`"x"`)}.accessKey())
		assert.Empty(t, trailEvent{ResponseElements: json.RawMessage(`{"credentials":{"accessKeyId":"AKIAAAAAAAAAAAAAAAAA"}}`)}.accessKey())
	})
}

func Test_keyEvents(t *testing.T) {
	t.Parallel()

	t.Run("失敗した event を時刻の順に返し、成功した event は数えるだけにする", func(t *testing.T) {
		t.Parallel()

		var calls []string

		late := deniedEvent("us-east-1")
		late["eventTime"] = "2026-11-27T05:00:00Z"
		responses := map[string][]byte{"lookup-events": trailJSON(t, late, okEvent(), deniedEvent("us-east-1"))}

		got, err := keyEvents(testDeps(t, responses, &calls), "", []string{"us-east-1"}, "s", []string{sessionKey})
		require.NoError(t, err)
		require.Len(t, got.failed, 2)
		assert.Equal(t, "2026-11-27T04:37:21Z", got.failed[0].EventTime)
		assert.Equal(t, 3, got.seen)
		assert.Empty(t, got.members)
	})

	t.Run("失敗した AssumeRole は members に入れず、同じ member key を重ねない", func(t *testing.T) {
		t.Parallel()

		var calls []string

		ok := map[string]any{"eventName": "AssumeRole", "responseElements": credentials(memberKey)}
		failed := map[string]any{"eventName": "AssumeRole", "errorCode": "AccessDenied", "responseElements": credentials("ASIACCCCCCCCCCCCCCCC")}
		responses := map[string][]byte{"lookup-events": trailJSON(t, ok, ok, failed)}

		got, err := keyEvents(testDeps(t, responses, &calls), "", []string{"us-east-1"}, "s", []string{sessionKey})
		require.NoError(t, err)
		assert.Equal(t, []string{memberKey}, got.members)
		assert.Len(t, got.failed, 1)
	})

	t.Run("lookup が失敗すれば errAWS", func(t *testing.T) {
		t.Parallel()

		var calls []string

		_, err := keyEvents(testDeps(t, map[string][]byte{"lookup-events": nil}, &calls), "", []string{"us-east-1"}, "s", []string{sessionKey})
		require.ErrorIs(t, err, errAWS)
	})
}

func Test_sortEvents(t *testing.T) {
	t.Parallel()

	t.Run("時刻の順に並べ、同じ時刻は元の順を保つ", func(t *testing.T) {
		t.Parallel()

		events := []trailEvent{{EventTime: "b", EventName: "1"}, {EventTime: "a"}, {EventTime: "b", EventName: "2"}}
		sortEvents(events)
		assert.Equal(t, []trailEvent{{EventTime: "a"}, {EventTime: "b", EventName: "1"}, {EventTime: "b", EventName: "2"}}, events)
	})
}

func eventOf(t *testing.T, m map[string]any) trailEvent {
	t.Helper()

	raw, err := json.Marshal(m)
	require.NoError(t, err)

	var ev trailEvent
	require.NoError(t, json.Unmarshal(raw, &ev))

	return ev
}

func Test_report(t *testing.T) {
	t.Parallel()

	denied := keyFindings{failed: []trailEvent{eventOf(t, deniedEvent("us-east-1"))}, seen: 2}

	t.Run("既定は文法に当たる行と件数だけで、errorMessage を出さない", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer
		require.NoError(t, report(&out, denied, 1, false, false))

		lines := strings.Split(strings.TrimSpace(out.String()), "\n")
		require.Len(t, lines, 2)
		assert.True(t, publicdiag.Valid(lines[0]), lines[0])
		assert.Equal(t, "CloudTrail: session の key 1 件、メンバーの key 0 件、読めた event 2 件、失敗した event 1 件", lines[1])
		assert.NotContains(t, out.String(), "111111111111")
		assert.NotContains(t, out.String(), "assumed-role")
	})

	t.Run("読めなかった event の数を出す", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer
		require.NoError(t, report(&out, keyFindings{seen: 1, unread: 2}, 1, false, false))
		assert.Contains(t, out.String(), "⚠️ 形の読めない event 2 件は数えていません\n")
	})

	t.Run("raw は errorMessage をそのまま出し、写さない注意を先頭に置く", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer
		require.NoError(t, report(&out, denied, 1, false, true))
		assert.True(t, strings.HasPrefix(out.String(), "⚠️ -raw の出力は人の端末だけで読み"))
		assert.Contains(t, out.String(), "assumed-role")
	})

	t.Run("最後の行を書けなければ error", func(t *testing.T) {
		t.Parallel()

		require.ErrorIs(t, report(failingWriter{}, keyFindings{}, 0, false, false), errWrite)
	})
}

func Test_eventLine(t *testing.T) {
	t.Parallel()

	t.Run("AccessDenied でない失敗は api-error で、Action を出さない", func(t *testing.T) {
		t.Parallel()

		line := eventLine(trailEvent{EventTime: "2026-11-27T04:37:21Z", AWSRegion: "us-east-1", EventSource: "s3.amazonaws.com", EventName: "PutObject", ErrorCode: "NoSuchBucket"})
		assert.Equal(t, "診断: kind=api-error; time=2026-11-27T04:37:21Z; region=us-east-1; source=s3.amazonaws.com; operation=PutObject; code=NoSuchBucket", line)
	})

	t.Run("文法に当たらない欄は伏せて数える", func(t *testing.T) {
		t.Parallel()

		line := eventLine(trailEvent{EventTime: "yesterday", AWSRegion: "us-east-1", EventSource: "s3.amazonaws.com", EventName: "PutObject", ErrorCode: "Client.111111111111"})
		assert.True(t, publicdiag.Valid(line), line)
		assert.Contains(t, line, "masked=2")
		assert.NotContains(t, line, "111111111111")
	})

	t.Run("errorMessage の識別子を出さない", func(t *testing.T) {
		t.Parallel()

		line := eventLine(eventOf(t, deniedEvent("us-east-1")))
		assert.True(t, publicdiag.Valid(line), line)
		assert.NotContains(t, line, "111111111111")
	})
}

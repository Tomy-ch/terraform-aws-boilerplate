package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	opsVariablesTF = `variable "env" {
	type = string

	validation {
		condition     = contains(["admin", "security", "log-archive", "sandbox", "dev", "stg", "prd"], var.env)
		error_message = "x"
	}
}
`
	opsMainTF = `locals {
	name_prefix = "${var.project}-${var.env}"
	topic_name  = "${local.name_prefix}-ops-notification"

	us_east_1_topic_name = "${local.topic_name}-us-east-1"

	chat_args = {
		name                     = "${local.name_prefix}-ops-chat"
		slack                    = var.chat.slack
		permissions_boundary_arn = var.permissions_boundary_arn
	}
}
`
	opsChatTF = `resource "aws_chatbot_slack_channel_configuration" "this" {
	region             = "us-west-2"
	configuration_name = var.name
}
`
	opsTopicARN   = "arn:aws:sns:ap-northeast-1:111111111111:tabp-dev-ops-notification"
	opsUSTopicARN = "arn:aws:sns:us-east-1:111111111111:tabp-dev-ops-notification-us-east-1"
	chatARN       = "arn:aws:chatbot::111111111111:chat-configuration/slack-channel/tabp-dev-ops-chat"
	chatRoleARN   = "arn:aws:iam::111111111111:role/tabp-dev-ops-chat"
	guardrailARN  = "arn:aws:iam::111111111111:policy/tabp-dev-ops-chat-read"
	listSubs      = "sns list-subscriptions"
	describeChats = "chatbot describe-slack-channel-configurations"
	getChatRole   = "iam get-role --role-name AWSServiceRoleForAWSChatbot"
)

func opsFiles() map[string]string {
	return map[string]string{
		path.Join(testUsecase, opsVariablesFile): opsVariablesTF,
		path.Join(testUsecase, opsMainFile):      opsMainTF,
		path.Join(testUsecase, opsChatFile):      opsChatTF,
	}
}

func opsOptions(files map[string]string) options {
	o := testOptions()
	o.usecase = testUsecase
	o.env = "dev"
	o.readFile = readFiles(files)
	o.sleep = func(time.Duration) {}

	return o
}

func testOpsShape() opsShape {
	return opsShape{
		topics:     []opsTopic{{"tabp-dev-ops-notification", testRegion}, {"tabp-dev-ops-notification-us-east-1", "us-east-1"}},
		chat:       "tabp-dev-ops-chat",
		chatRegion: "us-west-2",
	}
}

func chatbotSub(arn string) string {
	return `{"Protocol":"https","Endpoint":"` + chatbotEndpoint + `","TopicArn":"` + arn + `"}`
}

func subsJSON(items ...string) awsResult {
	return ok(`{"Subscriptions":[` + strings.Join(items, ",") + `]}`)
}

func testChatConfig() chatConfiguration {
	yes := true

	return chatConfiguration{
		ChatConfigurationArn: chatARN, ConfigurationName: "tabp-dev-ops-chat", SlackChannelID: "C1", SlackChannelName: "ops",
		IamRoleArn: chatRoleARN, SnsTopicArns: []string{opsUSTopicARN, opsTopicARN}, LoggingLevel: "NONE",
		GuardrailPolicyArns: []string{guardrailARN}, UserAuthorizationRequired: &yes,
	}
}

func chatsJSON(cfgs ...chatConfiguration) awsResult {
	if cfgs == nil {
		cfgs = []chatConfiguration{}
	}

	b, _ := json.Marshal(map[string]any{"SlackChannelConfigurations": cfgs})

	return ok(string(b))
}

// updateCall は、testChatConfig の値を保ち、SNS topics を topics にする書き込みの引数の列。
func updateCall(topics ...string) string {
	return "chatbot update-slack-channel-configuration --chat-configuration-arn " + chatARN + " --slack-channel-id C1 --slack-channel-name ops --iam-role-arn " +
		chatRoleARN + " --logging-level NONE --guardrail-policy-arns " + guardrailARN + " --sns-topic-arns " + strings.Join(topics, " ") + " --user-authorization-required"
}

func lastRegion(call []string) string {
	return call[len(call)-1]
}

func Test_readOpsShape(t *testing.T) {
	t.Parallel()

	t.Run("Topic と構成の名前を project と env で評価し、Chatbot の region を読む", func(t *testing.T) {
		t.Parallel()

		got, err := readOpsShape(opsOptions(opsFiles()))
		require.NoError(t, err)
		assert.Equal(t, testOpsShape(), got)
	})

	t.Run("provider の region の Topic は -region に置く", func(t *testing.T) {
		t.Parallel()

		o := opsOptions(opsFiles())
		o.region = "us-east-1"
		got, err := readOpsShape(o)
		require.NoError(t, err)
		assert.Equal(t, []opsTopic{{"tabp-dev-ops-notification", "us-east-1"}, {"tabp-dev-ops-notification-us-east-1", "us-east-1"}}, got.topics)
	})

	t.Run("宣言に無い env は errEnvName", func(t *testing.T) {
		t.Parallel()

		o := opsOptions(opsFiles())
		o.env = "qa"
		_, err := readOpsShape(o)
		require.ErrorIs(t, err, errEnvName)
	})

	for _, name := range []string{opsVariablesFile, opsMainFile, opsChatFile} {
		t.Run(name+" を読めなければ errHCL", func(t *testing.T) {
			t.Parallel()

			files := opsFiles()
			delete(files, path.Join(testUsecase, name))
			_, err := readOpsShape(opsOptions(files))
			require.ErrorIs(t, err, errHCL)
			require.ErrorIs(t, err, os.ErrNotExist)
		})
	}

	for name, tc := range map[string]struct{ file, src string }{
		"topic_name が無ければ":          {opsMainFile, strings.Replace(opsMainTF, "\ttopic_name  = \"${local.name_prefix}-ops-notification\"\n", "", 1)},
		"chat_args の name が無ければ":    {opsMainFile, strings.Replace(opsMainTF, "\t\tname                     = \"${local.name_prefix}-ops-chat\"\n", "", 1)},
		"Chatbot の構成の region が無ければ": {opsChatFile, "resource \"aws_chatbot_slack_channel_configuration\" \"this\" {\n}\n"},
		"Chatbot の構成が2つなら":          {opsChatFile, opsChatTF + strings.Replace(opsChatTF, "\"this\"", "\"other\"", 1)},
		"region がリテラルでなければ":         {opsChatFile, "resource \"aws_chatbot_slack_channel_configuration\" \"this\" {\n  region = var.region\n}\n"},
		"env の validation が無ければ":    {opsVariablesFile, "variable \"env\" {\n  type = string\n}\n"},
	} {
		t.Run(name+" errDeclShape", func(t *testing.T) {
			t.Parallel()

			files := opsFiles()
			files[path.Join(testUsecase, tc.file)] = tc.src
			_, err := readOpsShape(opsOptions(files))
			require.ErrorIs(t, err, errDeclShape)
		})
	}
}

func Test_checkChatbotRole(t *testing.T) {
	t.Parallel()

	t.Run("在れば通す", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer

		f := newFakeAWS(t, map[string]awsResult{getChatRole: ok(`{"Role":{"RoleName":"AWSServiceRoleForAWSChatbot"}}`)})
		require.NoError(t, checkChatbotRole(opsOptions(nil), testClient(f), &out))
		assert.Contains(t, out.String(), "# env dev のアカウントの Chatbot の service-linked role")
		assert.Contains(t, out.String(), "✅ AWSServiceRoleForAWSChatbot が在る")
	})

	t.Run("NoSuchEntity なら errNG", func(t *testing.T) {
		t.Parallel()

		var out bytes.Buffer

		f := newFakeAWS(t, map[string]awsResult{getChatRole: fail(t, "An error occurred (NoSuchEntity)")})
		require.ErrorIs(t, checkChatbotRole(opsOptions(nil), testClient(f), &out), errNG)
		assert.Contains(t, out.String(), "❌ AWSServiceRoleForAWSChatbot が在る")
	})

	t.Run("RoleName が違えば errAWSResponse", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{getChatRole: ok(`{"Role":{}}`)})
		require.ErrorIs(t, checkChatbotRole(opsOptions(nil), testClient(f), &bytes.Buffer{}), errAWSResponse)
	})

	t.Run("ほかの失敗は errAWSFailed", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{getChatRole: fail(t, "AccessDenied")})
		require.ErrorIs(t, checkChatbotRole(opsOptions(nil), testClient(f), &bytes.Buffer{}), errAWSFailed)
	})
}

func Test_slackWorkspaces(t *testing.T) {
	t.Parallel()

	t.Run("渡した region で読む", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{"chatbot describe-slack-workspaces": ok(`{"SlackWorkspaces":[{"SlackTeamId":"T1","SlackTeamName":"w","State":"ENABLED"}]}`)})
		got, err := slackWorkspaces(testClient(f), "us-west-2")
		require.NoError(t, err)
		assert.Equal(t, []slackWorkspace{{SlackTeamID: "T1", SlackTeamName: "w", State: "ENABLED"}}, got)
		assert.Equal(t, "us-west-2", lastRegion(f.calls[0]))
	})

	t.Run("0件は空のまま返す", func(t *testing.T) {
		t.Parallel()

		got, err := slackWorkspaces(testClient(newFakeAWS(t, map[string]awsResult{"chatbot describe-slack-workspaces": ok(`{"SlackWorkspaces":[]}`)})), "us-west-2")
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	for name, body := range map[string]string{
		"SlackWorkspaces が無ければ": `{}`,
		"SlackTeamId が欠けていれば":   `{"SlackWorkspaces":[{"SlackTeamName":"w"}]}`,
		"SlackTeamName が欠けていれば": `{"SlackWorkspaces":[{"SlackTeamId":"T1"}]}`,
		"object でなければ":          `[]`,
	} {
		t.Run(name+" errAWSResponse", func(t *testing.T) {
			t.Parallel()

			_, err := slackWorkspaces(testClient(newFakeAWS(t, map[string]awsResult{"chatbot describe-slack-workspaces": ok(body)})), "us-west-2")
			require.ErrorIs(t, err, errAWSResponse)
		})
	}
}

func Test_checkSlackWorkspaces(t *testing.T) {
	t.Parallel()

	run := func(t *testing.T, body string) (string, error) {
		t.Helper()

		var out bytes.Buffer

		f := newFakeAWS(t, map[string]awsResult{"chatbot describe-slack-workspaces": ok(body)})
		err := checkSlackWorkspaces(opsOptions(opsFiles()), testClient(f), &out)
		assert.Equal(t, "us-west-2", lastRegion(f.calls[0]))

		return out.String(), err
	}

	t.Run("ENABLED の workspace の team ID を出して通す", func(t *testing.T) {
		t.Parallel()

		out, err := run(t, `{"SlackWorkspaces":[{"SlackTeamId":"T1","SlackTeamName":"w","State":"ENABLED"},{"SlackTeamId":"T2","SlackTeamName":"old","State":"DISABLED"}]}`)
		require.NoError(t, err)
		assert.Contains(t, out, "  w: team ID T1（State: ENABLED）\n")
		assert.Contains(t, out, "  old: team ID T2（State: DISABLED）\n")
		assert.Contains(t, out, "✅ ENABLED の workspace が在る（今: 1 件）")
	})

	t.Run("workspace が0件なら errNG", func(t *testing.T) {
		t.Parallel()

		out, err := run(t, `{"SlackWorkspaces":[]}`)
		require.ErrorIs(t, err, errNG)
		assert.Contains(t, out, "❌ ENABLED の workspace が在る（今: 0 件）")
	})

	t.Run("DISABLED と State の無いものだけなら errNG", func(t *testing.T) {
		t.Parallel()

		out, err := run(t, `{"SlackWorkspaces":[{"SlackTeamId":"T1","SlackTeamName":"w","State":"DISABLED"},{"SlackTeamId":"T2","SlackTeamName":"x"}]}`)
		require.ErrorIs(t, err, errNG)
		assert.Contains(t, out, "  x: team ID T2（State: 無い）\n")
	})

	t.Run("Terraform を読めなければ aws を起動しない", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, nil)
		require.ErrorIs(t, checkSlackWorkspaces(opsOptions(nil), testClient(f), &bytes.Buffer{}), errHCL)
		assert.Empty(t, f.calls)
	})
}

func Test_listSubscriptions(t *testing.T) {
	t.Parallel()

	t.Run("渡した region で読む", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{listSubs: subsJSON(chatbotSub(opsTopicARN))})
		got, err := listSubscriptions(testClient(f), "us-east-1")
		require.NoError(t, err)
		assert.Equal(t, []snsSubscription{{Protocol: "https", Endpoint: chatbotEndpoint, TopicArn: opsTopicARN}}, got)
		assert.Equal(t, "us-east-1", lastRegion(f.calls[0]))
	})

	t.Run("購読が0件なら空で返す", func(t *testing.T) {
		t.Parallel()

		got, err := listSubscriptions(testClient(newFakeAWS(t, map[string]awsResult{listSubs: subsJSON()})), testRegion)
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	for name, body := range map[string]string{
		"Subscriptions が無ければ": `{}`,
		"Protocol が欠けていれば":    `{"Subscriptions":[{"Endpoint":"e","TopicArn":"a"}]}`,
		"Endpoint が欠けていれば":    `{"Subscriptions":[{"Protocol":"https","TopicArn":"a"}]}`,
		"TopicArn が欠けていれば":    `{"Subscriptions":[{"Protocol":"https","Endpoint":"e"}]}`,
		"null なら":             `null`,
	} {
		t.Run(name+" errAWSResponse", func(t *testing.T) {
			t.Parallel()

			_, err := listSubscriptions(testClient(newFakeAWS(t, map[string]awsResult{listSubs: ok(body)})), testRegion)
			require.ErrorIs(t, err, errAWSResponse)
		})
	}
}

func Test_isTopicARN(t *testing.T) {
	t.Parallel()

	topic := opsTopic{"tabp-dev-ops-notification", testRegion}

	cases := map[string]struct {
		arn  string
		want bool
	}{
		"名前と region が一致する":     {opsTopicARN, true},
		"ほかの partition でも一致する": {"arn:aws-cn:sns:ap-northeast-1:111111111111:tabp-dev-ops-notification", true},
		"region が違う":           {"arn:aws:sns:us-east-1:111111111111:tabp-dev-ops-notification", false},
		"名前の後ろが長い":             {opsTopicARN + "-us-east-1", false},
		"名前の前が長い":              {"arn:aws:sns:ap-northeast-1:111111111111:x-tabp-dev-ops-notification", false},
		"sns でない":              {"arn:aws:sqs:ap-northeast-1:111111111111:tabp-dev-ops-notification", false},
		"段が多い":                 {opsTopicARN + ":sub", false},
		"arn で始まらない":           {"x:aws:sns:ap-northeast-1:111111111111:tabp-dev-ops-notification", false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, isTopicARN(tc.arn, topic))
		})
	}
}

func Test_chatbotSubscriptionCounts(t *testing.T) {
	t.Parallel()

	t.Run("Topic ごとに、その region の Chatbot の https の購読だけを数える", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{listSubs: subsJSON(
			chatbotSub(opsTopicARN), chatbotSub(opsTopicARN),
			`{"Protocol":"email","Endpoint":"`+chatbotEndpoint+`","TopicArn":"`+opsUSTopicARN+`"}`,
			`{"Protocol":"https","Endpoint":"https://example.com","TopicArn":"`+opsUSTopicARN+`"}`,
		)})
		got, err := chatbotSubscriptionCounts(testClient(f), testOpsShape().topics)
		require.NoError(t, err)
		assert.Equal(t, []int{2, 0}, got)
		require.Len(t, f.calls, 2)
		assert.Equal(t, testRegion, lastRegion(f.calls[0]))
		assert.Equal(t, "us-east-1", lastRegion(f.calls[1]))
	})

	t.Run("同じ region の Topic は1回だけ読む", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{listSubs: subsJSON(chatbotSub("arn:aws:sns:us-east-1:111111111111:tabp-dev-ops-notification"), chatbotSub(opsUSTopicARN))})
		got, err := chatbotSubscriptionCounts(testClient(f), []opsTopic{{"tabp-dev-ops-notification", "us-east-1"}, {"tabp-dev-ops-notification-us-east-1", "us-east-1"}})
		require.NoError(t, err)
		assert.Equal(t, []int{1, 1}, got)
		assert.Len(t, f.calls, 1)
	})

	t.Run("読めなければエラー", func(t *testing.T) {
		t.Parallel()

		_, err := chatbotSubscriptionCounts(testClient(newFakeAWS(t, map[string]awsResult{listSubs: fail(t, "AccessDenied")})), testOpsShape().topics)
		require.ErrorIs(t, err, errAWSFailed)
	})
}

func Test_checkChatbotSubscriptions(t *testing.T) {
	t.Parallel()

	run := func(t *testing.T, subs awsResult) (string, error) {
		t.Helper()

		var out bytes.Buffer

		err := checkChatbotSubscriptions(opsOptions(opsFiles()), testClient(newFakeAWS(t, map[string]awsResult{listSubs: subs})), &out)

		return out.String(), err
	}

	t.Run("どちらの Topic も1つずつなら通す", func(t *testing.T) {
		t.Parallel()

		out, err := run(t, subsJSON(chatbotSub(opsTopicARN), chatbotSub(opsUSTopicARN)))
		require.NoError(t, err)
		assert.Contains(t, out, "✅ tabp-dev-ops-notification（ap-northeast-1）に Chatbot の購読がちょうど1つ在る（今: 1）")
		assert.Contains(t, out, "✅ tabp-dev-ops-notification-us-east-1（us-east-1）に Chatbot の購読がちょうど1つ在る（今: 1）")
	})

	t.Run("一覧に無い Topic は0で errNG", func(t *testing.T) {
		t.Parallel()

		out, err := run(t, subsJSON(chatbotSub(opsTopicARN)))
		require.ErrorIs(t, err, errNG)
		assert.Contains(t, out, "❌ tabp-dev-ops-notification-us-east-1（us-east-1）に Chatbot の購読がちょうど1つ在る（今: 0）")
	})

	t.Run("2つ在れば errNG", func(t *testing.T) {
		t.Parallel()

		out, err := run(t, subsJSON(chatbotSub(opsTopicARN), chatbotSub(opsTopicARN), chatbotSub(opsUSTopicARN)))
		require.ErrorIs(t, err, errNG)
		assert.Contains(t, out, "（今: 2）")
	})

	t.Run("購読が1つも無ければ errNG", func(t *testing.T) {
		t.Parallel()

		_, err := run(t, subsJSON())
		require.ErrorIs(t, err, errNG)
	})

	t.Run("読めない応答は errAWSResponse", func(t *testing.T) {
		t.Parallel()

		_, err := run(t, ok(`{}`))
		require.ErrorIs(t, err, errAWSResponse)
	})
}

func Test_findChatConfiguration(t *testing.T) {
	t.Parallel()

	other := testChatConfig()
	other.ConfigurationName = "tabp-stg-ops-chat"

	t.Run("名前の構成を Chatbot の region で読む", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{describeChats: chatsJSON(other, testChatConfig())})
		got, err := findChatConfiguration(testClient(f), testOpsShape())
		require.NoError(t, err)
		assert.Equal(t, testChatConfig(), got)
		assert.Equal(t, "us-west-2", lastRegion(f.calls[0]))
	})

	for name, cfgs := range map[string][]chatConfiguration{
		"無ければ":  {other},
		"0件なら":  {},
		"2つ在れば": {testChatConfig(), testChatConfig()},
	} {
		t.Run(name+" errPrecondition", func(t *testing.T) {
			t.Parallel()

			_, err := findChatConfiguration(testClient(newFakeAWS(t, map[string]awsResult{describeChats: chatsJSON(cfgs...)})), testOpsShape())
			require.ErrorIs(t, err, errPrecondition)
		})
	}

	t.Run("guardrail が無ければ errPrecondition", func(t *testing.T) {
		t.Parallel()

		cfg := testChatConfig()
		cfg.GuardrailPolicyArns = nil
		_, err := findChatConfiguration(testClient(newFakeAWS(t, map[string]awsResult{describeChats: chatsJSON(cfg)})), testOpsShape())
		require.ErrorIs(t, err, errPrecondition)
		assert.Contains(t, err.Error(), "AdministratorAccess")
	})

	for name, mutate := range map[string]func(*chatConfiguration){
		"ChatConfigurationArn":      func(c *chatConfiguration) { c.ChatConfigurationArn = "" },
		"SlackChannelId":            func(c *chatConfiguration) { c.SlackChannelID = "" },
		"SlackChannelName":          func(c *chatConfiguration) { c.SlackChannelName = "" },
		"IamRoleArn":                func(c *chatConfiguration) { c.IamRoleArn = "" },
		"LoggingLevel":              func(c *chatConfiguration) { c.LoggingLevel = "" },
		"UserAuthorizationRequired": func(c *chatConfiguration) { c.UserAuthorizationRequired = nil },
	} {
		t.Run(name+" が欠けていれば errAWSResponse", func(t *testing.T) {
			t.Parallel()

			cfg := testChatConfig()
			mutate(&cfg)
			_, err := findChatConfiguration(testClient(newFakeAWS(t, map[string]awsResult{describeChats: chatsJSON(cfg)})), testOpsShape())
			require.ErrorIs(t, err, errAWSResponse)
		})
	}

	t.Run("SlackChannelConfigurations が無ければ errAWSResponse", func(t *testing.T) {
		t.Parallel()

		_, err := findChatConfiguration(testClient(newFakeAWS(t, map[string]awsResult{describeChats: ok(`{}`)})), testOpsShape())
		require.ErrorIs(t, err, errAWSResponse)
	})
}

func Test_configTopicArns(t *testing.T) {
	t.Parallel()

	t.Run("宣言の Topic の順に並べる", func(t *testing.T) {
		t.Parallel()

		got, err := configTopicArns(testChatConfig(), testOpsShape())
		require.NoError(t, err)
		assert.Equal(t, []string{opsTopicARN, opsUSTopicARN}, got)
	})

	for name, arns := range map[string][]string{
		"宣言の Topic が欠けていれば":        {opsTopicARN},
		"宣言の Topic が2つ在れば":         {opsTopicARN, opsTopicARN, opsUSTopicARN},
		"宣言に無い Topic が在れば":         {opsTopicARN, opsUSTopicARN, "arn:aws:sns:ap-northeast-1:111111111111:other"},
		"Topic が無ければ":              nil,
		"region の違う同じ名前の Topic なら": {"arn:aws:sns:us-east-1:111111111111:tabp-dev-ops-notification", opsUSTopicARN},
	} {
		t.Run(name+" errPrecondition", func(t *testing.T) {
			t.Parallel()

			cfg := testChatConfig()
			cfg.SnsTopicArns = arns
			_, err := configTopicArns(cfg, testOpsShape())
			require.ErrorIs(t, err, errPrecondition)
		})
	}
}

func Test_updateArgs(t *testing.T) {
	t.Parallel()

	t.Run("読んだ値をすべて渡し、SNS topics だけを替える", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, updateCall(opsUSTopicARN), strings.Join(updateArgs(testChatConfig(), []string{opsUSTopicARN}), " "))
	})

	t.Run("guardrail が2つなら2つとも渡す", func(t *testing.T) {
		t.Parallel()

		cfg := testChatConfig()
		cfg.GuardrailPolicyArns = []string{"g1", "g2"}
		assert.Contains(t, strings.Join(updateArgs(cfg, []string{opsTopicARN}), " "), " --guardrail-policy-arns g1 g2 --sns-topic-arns "+opsTopicARN+" ")
	})

	t.Run("user の認可を求めない構成は --no-user-authorization-required", func(t *testing.T) {
		t.Parallel()

		cfg := testChatConfig()
		no := false
		cfg.UserAuthorizationRequired = &no
		got := updateArgs(cfg, []string{opsTopicARN})
		assert.Equal(t, "--no-user-authorization-required", got[len(got)-1])
		assert.NotContains(t, got, "--user-authorization-required")
	})
}

// resubscribeWorld は、構成が在り、Chatbot の購読が subs の世界。足し直す書き込み（両方の Topic を渡す）の後は、
// 両方の Topic に購読が在る。
func resubscribeWorld(t *testing.T, subs awsResult) *fakeAWS {
	t.Helper()

	f := newFakeAWS(t, map[string]awsResult{
		describeChats:                          chatsJSON(testChatConfig()),
		listSubs:                               subs,
		updateCall(opsUSTopicARN):              ok("{}"),
		updateCall(opsTopicARN):                ok("{}"),
		updateCall(opsTopicARN, opsUSTopicARN): ok("{}"),
	})
	f.onWrite(updateCall(opsTopicARN, opsUSTopicARN), map[string]awsResult{listSubs: subsJSON(chatbotSub(opsTopicARN), chatbotSub(opsUSTopicARN))})

	return f
}

func resubscribeClient(f *fakeAWS) *client {
	return testClient(f, "chatbot update-slack-channel-configuration")
}

func Test_resubscribeChatbot(t *testing.T) {
	t.Parallel()

	t.Run("購読の無い Topic を外して保存し、足し直して保存し、読み直す", func(t *testing.T) {
		t.Parallel()

		f := resubscribeWorld(t, subsJSON(chatbotSub(opsUSTopicARN)))
		term, out := tty("tabp\n")
		require.NoError(t, resubscribeChatbot(opsOptions(opsFiles()), resubscribeClient(f), term))
		assert.Equal(t, []string{updateCall(opsUSTopicARN), updateCall(opsTopicARN, opsUSTopicARN)}, f.writes())
		assert.Contains(t, out.String(), "  tabp-dev-ops-notification（ap-northeast-1）: Chatbot の購読 0\n")
		assert.Contains(t, out.String(), "✅ どの Topic にも Chatbot の購読がちょうど1つ在ります")

		for _, c := range f.calls {
			if op(c) == "chatbot update-slack-channel-configuration" {
				assert.Equal(t, "us-west-2", lastRegion(c))
			}
		}
	})

	t.Run("両方に無ければ、1つずつ外して足し直す", func(t *testing.T) {
		t.Parallel()

		f := resubscribeWorld(t, subsJSON())
		term, _ := tty("tabp\n")
		require.NoError(t, resubscribeChatbot(opsOptions(opsFiles()), resubscribeClient(f), term))
		assert.Equal(t, []string{
			updateCall(opsUSTopicARN), updateCall(opsTopicARN, opsUSTopicARN),
			updateCall(opsTopicARN), updateCall(opsTopicARN, opsUSTopicARN),
		}, f.writes())
	})

	t.Run("どちらにも在れば、確認を求めずに書かない", func(t *testing.T) {
		t.Parallel()

		f := resubscribeWorld(t, subsJSON(chatbotSub(opsTopicARN), chatbotSub(opsUSTopicARN)))
		term, out := tty("")
		require.NoError(t, resubscribeChatbot(opsOptions(opsFiles()), resubscribeClient(f), term))
		assert.Empty(t, f.writes())
		assert.Contains(t, out.String(), "書きません")
		assert.NotContains(t, out.String(), "project の名前")
	})

	t.Run("確認の入力が違えば書かない", func(t *testing.T) {
		t.Parallel()

		f := resubscribeWorld(t, subsJSON(chatbotSub(opsUSTopicARN)))
		term, _ := tty("nope\n")
		require.ErrorIs(t, resubscribeChatbot(opsOptions(opsFiles()), resubscribeClient(f), term), errNotConfirmed)
		assert.Empty(t, f.writes())
	})

	t.Run("端末でなければ書かない", func(t *testing.T) {
		t.Parallel()

		f := resubscribeWorld(t, subsJSON(chatbotSub(opsUSTopicARN)))
		term := newTerminal(strings.NewReader("tabp\n"), &bytes.Buffer{}, false)
		require.ErrorIs(t, resubscribeChatbot(opsOptions(opsFiles()), resubscribeClient(f), term), errNotTerminal)
		assert.Empty(t, f.writes())
	})

	t.Run("購読が2つ在る Topic があれば errPrecondition で書かない", func(t *testing.T) {
		t.Parallel()

		f := resubscribeWorld(t, subsJSON(chatbotSub(opsTopicARN), chatbotSub(opsTopicARN)))
		term, _ := tty("tabp\n")
		require.ErrorIs(t, resubscribeChatbot(opsOptions(opsFiles()), resubscribeClient(f), term), errPrecondition)
		assert.Empty(t, f.writes())
	})

	t.Run("構成が宣言の Topic を持たなければ errPrecondition で書かない", func(t *testing.T) {
		t.Parallel()

		f := resubscribeWorld(t, subsJSON(chatbotSub(opsUSTopicARN)))
		cfg := testChatConfig()
		cfg.SnsTopicArns = []string{opsUSTopicARN}
		f.responses[describeChats] = chatsJSON(cfg)

		term, _ := tty("tabp\n")
		require.ErrorIs(t, resubscribeChatbot(opsOptions(opsFiles()), resubscribeClient(f), term), errPrecondition)
		assert.Empty(t, f.writes())
	})

	t.Run("構成が無ければ errPrecondition で書かない", func(t *testing.T) {
		t.Parallel()

		f := resubscribeWorld(t, subsJSON(chatbotSub(opsUSTopicARN)))
		f.responses[describeChats] = chatsJSON()

		term, _ := tty("tabp\n")
		require.ErrorIs(t, resubscribeChatbot(opsOptions(opsFiles()), resubscribeClient(f), term), errPrecondition)
		assert.Empty(t, f.writes())
	})

	t.Run("外す保存が失敗すれば、足し直さずに止める", func(t *testing.T) {
		t.Parallel()

		f := resubscribeWorld(t, subsJSON(chatbotSub(opsUSTopicARN)))
		f.responses[updateCall(opsUSTopicARN)] = fail(t, "AccessDenied")

		term, _ := tty("tabp\n")
		err := resubscribeChatbot(opsOptions(opsFiles()), resubscribeClient(f), term)
		require.ErrorIs(t, err, errAWSFailed)
		assert.Contains(t, err.Error(), "外す保存が失敗しました")
		assert.Equal(t, []string{updateCall(opsUSTopicARN)}, f.writes())
	})

	t.Run("足し直す保存が失敗すれば、外したままであることを伝える", func(t *testing.T) {
		t.Parallel()

		f := resubscribeWorld(t, subsJSON(chatbotSub(opsUSTopicARN)))
		f.responses[updateCall(opsTopicARN, opsUSTopicARN)] = fail(t, "Throttling")

		term, _ := tty("tabp\n")
		err := resubscribeChatbot(opsOptions(opsFiles()), resubscribeClient(f), term)
		require.ErrorIs(t, err, errAWSFailed)
		assert.Contains(t, err.Error(), "外したまま止まりました")
		assert.Equal(t, []string{updateCall(opsUSTopicARN), updateCall(opsTopicARN, opsUSTopicARN)}, f.writes())
	})

	t.Run("読み直しで購読が見えなければ、やり直したうえでコンソールの手順へ戻す", func(t *testing.T) {
		t.Parallel()

		f := resubscribeWorld(t, subsJSON(chatbotSub(opsUSTopicARN)))
		f.after = nil

		var sleeps int

		o := opsOptions(opsFiles())
		o.sleep = func(time.Duration) { sleeps++ }

		term, _ := tty("tabp\n")
		err := resubscribeChatbot(o, resubscribeClient(f), term)
		require.ErrorIs(t, err, errAWSResponse)
		assert.Contains(t, err.Error(), "3.4 手順2 のコンソールの手順")
		assert.Equal(t, rereadTries-1, sleeps)
	})

	t.Run("読み直しの3回目で見えれば通す", func(t *testing.T) {
		t.Parallel()

		f := resubscribeWorld(t, subsJSON(chatbotSub(opsUSTopicARN)))
		f.after = nil
		missing, both := subsJSON(chatbotSub(opsUSTopicARN)), subsJSON(chatbotSub(opsTopicARN), chatbotSub(opsUSTopicARN))
		// 書く前の2回、読み直しの1回目と2回目の2回ずつ、3回目の2回。
		f.seq = map[string][]awsResult{listSubs: {missing, missing, missing, missing, missing, missing, both, both}}

		term, _ := tty("tabp\n")
		require.NoError(t, resubscribeChatbot(opsOptions(opsFiles()), resubscribeClient(f), term))
	})

	t.Run("書いた後の構成の guardrail が変われば errAWSResponse", func(t *testing.T) {
		t.Parallel()

		f := resubscribeWorld(t, subsJSON(chatbotSub(opsUSTopicARN)))
		changed := testChatConfig()
		changed.GuardrailPolicyArns = []string{"arn:aws:iam::aws:policy/AdministratorAccess"}
		f.onWrite(updateCall(opsTopicARN, opsUSTopicARN), map[string]awsResult{describeChats: chatsJSON(changed)})

		term, _ := tty("tabp\n")
		err := resubscribeChatbot(opsOptions(opsFiles()), resubscribeClient(f), term)
		require.ErrorIs(t, err, errAWSResponse)
		assert.Contains(t, err.Error(), "保存の前と違います")
	})

	t.Run("Terraform を読めなければ aws を起動しない", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, nil)
		term, _ := tty("tabp\n")
		require.ErrorIs(t, resubscribeChatbot(opsOptions(nil), resubscribeClient(f), term), errHCL)
		assert.Empty(t, f.calls)
	})

	t.Run("読み直しで購読が2つ見えれば errAWSResponse", func(t *testing.T) {
		t.Parallel()

		f := resubscribeWorld(t, subsJSON(chatbotSub(opsUSTopicARN)))
		f.onWrite(updateCall(opsTopicARN, opsUSTopicARN), map[string]awsResult{listSubs: subsJSON(chatbotSub(opsTopicARN), chatbotSub(opsTopicARN), chatbotSub(opsUSTopicARN))})

		term, _ := tty("tabp\n")
		err := resubscribeChatbot(opsOptions(opsFiles()), resubscribeClient(f), term)
		require.ErrorIs(t, err, errAWSResponse)
		assert.Contains(t, err.Error(), "3.4 手順2 のコンソールの手順")
	})

	t.Run("読み直しを読めなければ、やり直さずに errAWSFailed", func(t *testing.T) {
		t.Parallel()

		f := resubscribeWorld(t, subsJSON(chatbotSub(opsUSTopicARN)))
		f.onWrite(updateCall(opsTopicARN, opsUSTopicARN), map[string]awsResult{listSubs: fail(t, "AccessDenied")})

		var sleeps int

		o := opsOptions(opsFiles())
		o.sleep = func(time.Duration) { sleeps++ }

		term, _ := tty("tabp\n")
		require.ErrorIs(t, resubscribeChatbot(o, resubscribeClient(f), term), errAWSFailed)
		assert.Zero(t, sleeps)
	})

	t.Run("2つ目の Topic を外す保存が失敗すれば、1つ目を直した後で止め、2つ目の Topic を名指す", func(t *testing.T) {
		t.Parallel()

		f := resubscribeWorld(t, subsJSON())
		f.responses[updateCall(opsTopicARN)] = fail(t, "Throttling")

		term, _ := tty("tabp\n")
		err := resubscribeChatbot(opsOptions(opsFiles()), resubscribeClient(f), term)
		require.ErrorIs(t, err, errAWSFailed)
		assert.Contains(t, err.Error(), "tabp-dev-ops-notification-us-east-1 を外す保存が失敗しました")
		assert.Equal(t, []string{updateCall(opsUSTopicARN), updateCall(opsTopicARN, opsUSTopicARN), updateCall(opsTopicARN)}, f.writes())
	})

	t.Run("購読の無い Topic の後に購読が2つの Topic があれば errPrecondition で書かない", func(t *testing.T) {
		t.Parallel()

		f := resubscribeWorld(t, subsJSON(chatbotSub(opsUSTopicARN), chatbotSub(opsUSTopicARN)))
		term, _ := tty("tabp\n")
		require.ErrorIs(t, resubscribeChatbot(opsOptions(opsFiles()), resubscribeClient(f), term), errPrecondition)
		assert.Empty(t, f.writes())
	})
}

func Test_requireSameConfiguration(t *testing.T) {
	t.Parallel()

	t.Run("書く前と同じなら通す", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{describeChats: chatsJSON(testChatConfig())})
		require.NoError(t, requireSameConfiguration(testClient(f), testOpsShape(), testChatConfig()))
	})

	no := false

	for name, mutate := range map[string]func(*chatConfiguration){
		"guardrail が違えば":       func(c *chatConfiguration) { c.GuardrailPolicyArns = []string{"other"} },
		"role が違えば":            func(c *chatConfiguration) { c.IamRoleArn = "other" },
		"logging が違えば":         func(c *chatConfiguration) { c.LoggingLevel = "ERROR" },
		"user の認可が違えば":         func(c *chatConfiguration) { c.UserAuthorizationRequired = &no },
		"SNS topics が戻っていなければ": func(c *chatConfiguration) { c.SnsTopicArns = []string{opsUSTopicARN} },
	} {
		t.Run(name+" errAWSResponse", func(t *testing.T) {
			t.Parallel()

			after := testChatConfig()
			mutate(&after)
			f := newFakeAWS(t, map[string]awsResult{describeChats: chatsJSON(after)})
			require.ErrorIs(t, requireSameConfiguration(testClient(f), testOpsShape(), testChatConfig()), errAWSResponse)
		})
	}

	t.Run("読み直せなければそのエラーを返す", func(t *testing.T) {
		t.Parallel()

		f := newFakeAWS(t, map[string]awsResult{describeChats: chatsJSON()})
		err := requireSameConfiguration(testClient(f), testOpsShape(), testChatConfig())
		require.ErrorIs(t, err, errPrecondition)
		assert.Contains(t, err.Error(), "保存した後の構成")
	})
}

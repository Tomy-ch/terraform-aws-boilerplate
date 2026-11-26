package main

import (
	"fmt"
	"io"
	"path"
	"slices"
	"strings"

	"github.com/Tomy-ch/terraform-aws-boilerplate/scripts/lib/xerrors"
)

const (
	chatbotRole     = "AWSServiceRoleForAWSChatbot"
	chatbotEndpoint = "https://global.sns-api.chatbot.amazonaws.com"
	usEast1Region   = "us-east-1"
	// opsMainFile・opsVariablesFile・opsChatFile は、-usecase（usecases/ops-notification）の中で、Topic と構成の
	// 名前の locals、env の一覧、Chatbot の構成の resource を持つファイル。
	opsMainFile      = "main.tf"
	opsVariablesFile = "variables.tf"
	opsChatFile      = "internal/chat-slack/main.tf"
	chatResource     = "aws_chatbot_slack_channel_configuration"
)

// opsTopic は、Chatbot が購読する Topic の1つ。
type opsTopic struct {
	name   string
	region string
}

// opsShape は、ops-notification の Terraform から読む、手順が守るべき形。topics は provider の region の Topic、
// us-east-1 の Topic の順。
type opsShape struct {
	topics     []opsTopic
	chat       string
	chatRegion string
}

// readOpsShape は、-usecase の Terraform から、Topic と Chatbot の構成の名前と、Chatbot の API の region を読む。
// -env が variable "env" の許す env でなければ errEnvName。
func readOpsShape(o options) (opsShape, error) {
	variables, err := parseFile(o.readFile, path.Join(o.usecase, opsVariablesFile))
	if err != nil {
		return opsShape{}, err
	}

	if err := requireEnv(variables, o.env); err != nil {
		return opsShape{}, err
	}

	body, err := parseFile(o.readFile, path.Join(o.usecase, opsMainFile))
	if err != nil {
		return opsShape{}, err
	}

	locals, ctx, err := evalLocals(body, []string{"name_prefix", "topic_name", "us_east_1_topic_name"}, map[string]string{"project": o.project, "env": o.env})
	if err != nil {
		return opsShape{}, err
	}

	chat, err := localItemString(body, "chat_args", "name", ctx)
	if err != nil {
		return opsShape{}, err
	}

	chatBody, err := parseFile(o.readFile, path.Join(o.usecase, opsChatFile))
	if err != nil {
		return opsShape{}, err
	}

	res, err := singleResource(chatBody, chatResource)
	if err != nil {
		return opsShape{}, err
	}

	region, err := literalString(res.Body.Attributes["region"])
	if err != nil {
		return opsShape{}, err
	}

	return opsShape{
		topics:     []opsTopic{{locals["topic_name"], o.region}, {locals["us_east_1_topic_name"], usEast1Region}},
		chat:       chat,
		chatRegion: region,
	}, nil
}

// checkChatbotRole は、1 の前提: Chatbot の service-linked role が在る。
func checkChatbotRole(o options, c *client, w io.Writer) error {
	var resp struct {
		Role struct {
			RoleName string `json:"RoleName"`
		} `json:"Role"`
	}

	found, err := c.readOptional(noSuchEntity, &resp, "iam", "get-role", "--role-name", chatbotRole)
	if err != nil {
		return err
	}

	if found && resp.Role.RoleName != chatbotRole {
		return xerrors.Wrap(errAWSResponse, "iam get-role: "+chatbotRole+" の RoleName がありません")
	}

	r := newReport(w, "env "+o.env+" のアカウントの Chatbot の service-linked role（1 の前提）")
	r.check(found, chatbotRole+" が在る（無ければ identity root の apply が先）")

	return r.result()
}

// slackWorkspace は、Chatbot に認可した Slack の workspace の1つ。
type slackWorkspace struct {
	SlackTeamID   string `json:"SlackTeamId"`
	SlackTeamName string `json:"SlackTeamName"`
	State         string `json:"State"`
}

func slackWorkspaces(c *client, region string) ([]slackWorkspace, error) {
	var resp struct {
		SlackWorkspaces *[]slackWorkspace `json:"SlackWorkspaces"`
	}

	if err := c.readIn(region, &resp, "chatbot", "describe-slack-workspaces"); err != nil {
		return nil, err
	}

	if resp.SlackWorkspaces == nil {
		return nil, xerrors.Wrap(errAWSResponse, "chatbot describe-slack-workspaces: SlackWorkspaces がありません")
	}

	for _, ws := range *resp.SlackWorkspaces {
		if ws.SlackTeamID == "" || ws.SlackTeamName == "" {
			return nil, xerrors.Wrap(errAWSResponse, "chatbot describe-slack-workspaces: SlackTeamId か SlackTeamName の欠けた workspace があります")
		}
	}

	return *resp.SlackWorkspaces, nil
}

// checkSlackWorkspaces は、3.2 手順4・3.3 手順1: ENABLED の workspace が在り、その team ID を出す。
func checkSlackWorkspaces(o options, c *client, w io.Writer) error {
	shape, err := readOpsShape(o)
	if err != nil {
		return err
	}

	all, err := slackWorkspaces(c, shape.chatRegion)
	if err != nil {
		return err
	}

	r := newReport(w, "Chatbot に認可した Slack の workspace（3.2 手順4・3.3 手順1）")

	var enabled int

	for _, ws := range all {
		state := ws.State
		if state == "" {
			state = "無い"
		}

		fmt.Fprintf(w, "  %s: team ID %s（State: %s）\n", ws.SlackTeamName, ws.SlackTeamID, state)

		if ws.State == "ENABLED" {
			enabled++
		}
	}

	r.check(enabled > 0, fmt.Sprintf("ENABLED の workspace が在る（今: %d 件）", enabled))

	return r.result()
}

type snsSubscription struct {
	Protocol string `json:"Protocol"`
	Endpoint string `json:"Endpoint"`
	TopicArn string `json:"TopicArn"`
}

func listSubscriptions(c *client, region string) ([]snsSubscription, error) {
	var resp struct {
		Subscriptions *[]snsSubscription `json:"Subscriptions"`
	}

	if err := c.readIn(region, &resp, "sns", "list-subscriptions"); err != nil {
		return nil, err
	}

	if resp.Subscriptions == nil {
		return nil, xerrors.Wrap(errAWSResponse, "sns list-subscriptions: Subscriptions がありません")
	}

	for _, s := range *resp.Subscriptions {
		if s.Protocol == "" || s.Endpoint == "" || s.TopicArn == "" {
			return nil, xerrors.Wrap(errAWSResponse, "sns list-subscriptions: Protocol・Endpoint・TopicArn の欠けた購読があります")
		}
	}

	return *resp.Subscriptions, nil
}

// isTopicARN は、arn が topic の ARN（arn:<partition>:sns:<region>:<account>:<name>）か。
func isTopicARN(arn string, topic opsTopic) bool {
	p := strings.Split(arn, ":")

	return len(p) == 6 && p[0] == "arn" && p[2] == "sns" && p[3] == topic.region && p[5] == topic.name
}

// chatbotSubscriptionCounts は、topics のそれぞれの、Chatbot の https の購読の数。region ごとに1回だけ読む。
func chatbotSubscriptionCounts(c *client, topics []opsTopic) ([]int, error) {
	byRegion := map[string][]snsSubscription{}
	counts := make([]int, len(topics))

	for i, t := range topics {
		subs, ok := byRegion[t.region]
		if !ok {
			var err error
			if subs, err = listSubscriptions(c, t.region); err != nil {
				return nil, err
			}

			byRegion[t.region] = subs
		}

		for _, s := range subs {
			if s.Protocol == "https" && s.Endpoint == chatbotEndpoint && isTopicARN(s.TopicArn, t) {
				counts[i]++
			}
		}
	}

	return counts, nil
}

// checkChatbotSubscriptions は、3.4 手順1: 2つの Topic のそれぞれに、Chatbot の https の購読がちょうど1つ在る。
func checkChatbotSubscriptions(o options, c *client, w io.Writer) error {
	shape, err := readOpsShape(o)
	if err != nil {
		return err
	}

	counts, err := chatbotSubscriptionCounts(c, shape.topics)
	if err != nil {
		return err
	}

	r := newReport(w, "Topic の Chatbot の購読（3.4 手順1）")

	for i, t := range shape.topics {
		r.check(counts[i] == 1, fmt.Sprintf("%s（%s）に Chatbot の購読がちょうど1つ在る（今: %d）", t.name, t.region, counts[i]))
	}

	return r.result()
}

// chatConfiguration は、Chatbot の Slack の channel の構成の1つ。update-slack-channel-configuration は
// guardrail を省くと AdministratorAccess を既定にし、ほかの項目を省いたときの扱いは文書に無いので、書くときは
// 読んだ値をすべて渡し直す。
type chatConfiguration struct {
	ChatConfigurationArn      string   `json:"ChatConfigurationArn"`
	ConfigurationName         string   `json:"ConfigurationName"`
	SlackChannelID            string   `json:"SlackChannelId"`
	SlackChannelName          string   `json:"SlackChannelName"`
	IamRoleArn                string   `json:"IamRoleArn"`
	SnsTopicArns              []string `json:"SnsTopicArns"`
	LoggingLevel              string   `json:"LoggingLevel"`
	GuardrailPolicyArns       []string `json:"GuardrailPolicyArns"`
	UserAuthorizationRequired *bool    `json:"UserAuthorizationRequired"`
}

// findChatConfiguration は、shape の名前の構成。ただ1つに決まらなければ errPrecondition。書き直しに渡す値が
// 欠けていれば errAWSResponse、guardrail が無ければ（渡し直せないので）errPrecondition。
func findChatConfiguration(c *client, shape opsShape) (chatConfiguration, error) {
	var resp struct {
		SlackChannelConfigurations *[]chatConfiguration `json:"SlackChannelConfigurations"`
	}

	if err := c.readIn(shape.chatRegion, &resp, "chatbot", "describe-slack-channel-configurations"); err != nil {
		return chatConfiguration{}, err
	}

	if resp.SlackChannelConfigurations == nil {
		return chatConfiguration{}, xerrors.Wrap(errAWSResponse, "chatbot describe-slack-channel-configurations: SlackChannelConfigurations がありません")
	}

	var found []chatConfiguration

	for _, cfg := range *resp.SlackChannelConfigurations {
		if cfg.ConfigurationName == shape.chat {
			found = append(found, cfg)
		}
	}

	if len(found) != 1 {
		return chatConfiguration{}, xerrors.Wrapf(errPrecondition, "Chatbot の構成 %s が %d 件です（無ければ workload root の apply が先）", shape.chat, len(found))
	}

	cfg := found[0]

	switch {
	case cfg.ChatConfigurationArn == "", cfg.SlackChannelID == "", cfg.SlackChannelName == "", cfg.IamRoleArn == "", cfg.LoggingLevel == "", cfg.UserAuthorizationRequired == nil:
		return chatConfiguration{}, xerrors.Wrap(errAWSResponse, "chatbot describe-slack-channel-configurations: "+shape.chat+" の書き直しに渡す値が欠けています")
	case len(cfg.GuardrailPolicyArns) == 0:
		return chatConfiguration{}, xerrors.Wrap(errPrecondition, "構成 "+shape.chat+" に guardrail がありません。保存し直すと AdministratorAccess になるので書きません。apply で宣言に戻してください")
	}

	return cfg, nil
}

// configTopicArns は、構成の SnsTopicArns を shape の Topic の順に並べる。宣言の Topic とちょうど一致しなければ
// （apply との差分があるので）errPrecondition。
func configTopicArns(cfg chatConfiguration, shape opsShape) ([]string, error) {
	out := make([]string, len(shape.topics))

	for i, t := range shape.topics {
		var found []string

		for _, arn := range cfg.SnsTopicArns {
			if isTopicARN(arn, t) {
				found = append(found, arn)
			}
		}

		if len(found) != 1 {
			return nil, xerrors.Wrapf(errPrecondition, "構成 %s の SNS topics に %s（%s）が %d 件です", shape.chat, t.name, t.region, len(found))
		}

		out[i] = found[0]
	}

	if len(cfg.SnsTopicArns) != len(out) {
		return nil, xerrors.Wrapf(errPrecondition, "構成 %s の SNS topics に宣言に無い Topic があります（%s）", shape.chat, joinOrNone(cfg.SnsTopicArns))
	}

	return out, nil
}

// updateArgs は、cfg の値を保ち、SNS topics だけを topicArns にする update-slack-channel-configuration の引数。
func updateArgs(cfg chatConfiguration, topicArns []string) []string {
	args := []string{
		"chatbot", "update-slack-channel-configuration",
		"--chat-configuration-arn", cfg.ChatConfigurationArn,
		"--slack-channel-id", cfg.SlackChannelID,
		"--slack-channel-name", cfg.SlackChannelName,
		"--iam-role-arn", cfg.IamRoleArn,
		"--logging-level", cfg.LoggingLevel,
		"--guardrail-policy-arns",
	}
	args = append(args, cfg.GuardrailPolicyArns...)
	args = append(args, "--sns-topic-arns")
	args = append(args, topicArns...)

	if *cfg.UserAuthorizationRequired {
		return append(args, "--user-authorization-required")
	}

	return append(args, "--no-user-authorization-required")
}

// resubscribeChatbot は、3.4 手順2: Chatbot の購読の無い Topic を、構成の SNS topics から外して保存し、足し直して
// 保存する。購読し直すかは文書に無いので、購読を読み直して確かめ、見えなければ手順書のコンソールの手順へ戻す。
func resubscribeChatbot(o options, c *client, t *terminal) error {
	shape, err := readOpsShape(o)
	if err != nil {
		return err
	}

	cfg, err := findChatConfiguration(c, shape)
	if err != nil {
		return err
	}

	arns, err := configTopicArns(cfg, shape)
	if err != nil {
		return err
	}

	counts, err := chatbotSubscriptionCounts(c, shape.topics)
	if err != nil {
		return err
	}

	var missing []int

	for i, n := range counts {
		fmt.Fprintf(t.out, "  %s（%s）: Chatbot の購読 %d\n", shape.topics[i].name, shape.topics[i].region, n)

		switch {
		case n == 0:
			missing = append(missing, i)
		case n > 1:
			return xerrors.Wrapf(errPrecondition, "%s に Chatbot の購読が %d 在ります。外し直しでは減らないので、この入口では直しません", shape.topics[i].name, n)
		}
	}

	if len(missing) == 0 {
		fmt.Fprintln(t.out, "✅ どの Topic にも Chatbot の購読が在ります。書きません")

		return nil
	}

	for _, i := range missing {
		fmt.Fprintf(t.out, "構成 %s の SNS topics から %s を外して保存し、足し直して保存します（guardrail・role・logging の値は今のまま渡します）\n", shape.chat, shape.topics[i].name)
	}

	if err := t.confirm(o.project); err != nil {
		return err
	}

	for _, i := range missing {
		without := slices.Delete(slices.Clone(arns), i, i+1)

		if err := c.writeIn(shape.chatRegion, updateArgs(cfg, without)...); err != nil {
			return xerrors.Wrap(err, "構成 "+shape.chat+" から "+shape.topics[i].name+" を外す保存が失敗しました。ops-subscriptions-check と plan で確かめてください")
		}

		if err := c.writeIn(shape.chatRegion, updateArgs(cfg, arns)...); err != nil {
			return xerrors.Wrap(err, "構成 "+shape.chat+" から "+shape.topics[i].name+" を外したまま止まりました。この入口をやり直すか、apply で宣言に戻してください")
		}
	}

	if err := requireSameConfiguration(c, shape, cfg); err != nil {
		return err
	}

	if err := reread(o, func() (bool, error) {
		got, err := chatbotSubscriptionCounts(c, shape.topics)
		if err != nil {
			return false, err
		}

		return !slices.ContainsFunc(got, func(n int) bool { return n != 1 }), nil
	}); err != nil {
		return xerrors.Wrap(err, "保存し直した後も Chatbot の購読が見えません。手順書 3.4 手順2 のコンソールの手順で行ってください")
	}

	fmt.Fprintln(t.out, "✅ どの Topic にも Chatbot の購読がちょうど1つ在ります")

	return nil
}

// requireSameConfiguration は、書いた後の構成を読み直し、SNS topics が宣言の Topic に戻り、guardrail・role・
// logging・user の認可が書く前の before と同じであることを求める。違えば errAWSResponse。
func requireSameConfiguration(c *client, shape opsShape, before chatConfiguration) error {
	after, err := findChatConfiguration(c, shape)
	if err != nil {
		return xerrors.Wrap(err, "保存した後の構成 "+shape.chat+" を読めません。plan で確かめてください")
	}

	if _, err := configTopicArns(after, shape); err != nil ||
		!sameSet(after.GuardrailPolicyArns, before.GuardrailPolicyArns) || after.IamRoleArn != before.IamRoleArn ||
		after.LoggingLevel != before.LoggingLevel || *after.UserAuthorizationRequired != *before.UserAuthorizationRequired {
		return xerrors.Wrap(errAWSResponse, "保存した後の構成 "+shape.chat+" が保存の前と違います。plan で確かめ、apply で宣言に戻してください")
	}

	return nil
}

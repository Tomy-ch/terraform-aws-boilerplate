package plan.ops_notification_test

import data.plan.fixture
import data.plan.ops_notification as gate

m := "module.ops_notification."

topic_arn := "arn:aws:sns:ap-northeast-1:123456789012:tabp-sandbox-ops-notification"

group_arn := "arn:aws:scheduler:ap-northeast-1:123456789012:schedule-group/tabp-sandbox-ops-heartbeat"

heartbeat_arn := "arn:aws:iam::123456789012:role/tabp-sandbox-ops-heartbeat"

chat_arn := "arn:aws:iam::123456789012:role/tabp-sandbox-ops-chat"

read_arn := "arn:aws:iam::123456789012:policy/tabp-sandbox-ops-chat-read"

boundary := "arn:aws:iam::123456789012:policy/tabp-sandbox-workload-boundary"

services := ["budgets", "cloudwatch", "costalerts", "events"]

rc(type, address, after) := {"address": sprintf("%s%s", [m, address]), "type": type, "change": {"actions": ["create"], "after": after, "after_unknown": {}}}

doc(statements) := json.marshal({"Version": "2012-10-17", "Statement": statements})

topic_policy_statements := array.concat(
	[{"Effect": "Deny", "Principal": "*", "Action": "sns:Publish", "Resource": topic_arn, "Condition": {"Bool": {"aws:SecureTransport": "false"}}}],
	[{"Effect": "Allow", "Principal": {"Service": sprintf("%s.amazonaws.com", [s])}, "Action": "sns:Publish", "Resource": topic_arn, "Condition": {"StringEquals": {"aws:SourceAccount": "123456789012"}}} | some s in services],
)

key_statements := array.concat(
	[
		{"Sid": "AccountKeyAdministration", "Effect": "Allow", "Principal": {"AWS": "arn:aws:iam::123456789012:root"}, "Action": ["kms:DescribeKey"], "Resource": "*"},
		{"Sid": "RolesViaSns0", "Effect": "Allow", "Principal": {"AWS": [heartbeat_arn]}, "Action": ["kms:Decrypt", "kms:GenerateDataKey*"], "Resource": "*", "Condition": {"StringEquals": {"kms:ViaService": "sns.ap-northeast-1.amazonaws.com", "kms:EncryptionContext:aws:sns:topicArn": [topic_arn]}}},
		{"Sid": "SnsPublisherEvents", "Effect": "Allow", "Principal": {"Service": "events.amazonaws.com"}, "Action": ["kms:Decrypt", "kms:GenerateDataKey*"], "Resource": "*", "Condition": {"StringEquals": {"kms:EncryptionContext:aws:sns:topicArn": [topic_arn]}}},
	],
	[{"Effect": "Allow", "Principal": {"Service": sprintf("%s.amazonaws.com", [s])}, "Action": ["kms:Decrypt", "kms:GenerateDataKey*"], "Resource": "*", "Condition": {"StringEquals": {"aws:SourceAccount": "123456789012"}}} | some s in ["budgets", "cloudwatch", "costalerts"]],
)

heartbeat_trust := [{
	"Effect": "Allow",
	"Principal": {"Service": "scheduler.amazonaws.com"},
	"Action": "sts:AssumeRole",
	"Condition": {"StringEquals": {"aws:SourceAccount": "123456789012", "aws:SourceArn": group_arn}},
}]

chat_trust := [{"Effect": "Allow", "Principal": {"Service": "chatbot.amazonaws.com"}, "Action": "sts:AssumeRole"}]

read_statements := [
	{"Sid": "ReadAlarms", "Effect": "Allow", "Action": ["cloudwatch:DescribeAlarmHistory", "cloudwatch:DescribeAlarms", "cloudwatch:ListTagsForResource"], "Resource": "arn:aws:cloudwatch:ap-northeast-1:123456789012:alarm:*"},
	{"Sid": "ReadMetrics", "Effect": "Allow", "Action": ["cloudwatch:DescribeAlarmsForMetric", "cloudwatch:GetMetricData", "cloudwatch:GetMetricStatistics", "cloudwatch:GetMetricWidgetImage", "cloudwatch:ListMetrics"], "Resource": "*"},
]

base := {
	"topic": rc("aws_sns_topic", "module.sns.aws_sns_topic.this", {"arn": topic_arn, "policy": null}),
	"topic_policy": rc("aws_sns_topic_policy", "module.sns.aws_sns_topic_policy.this", {"arn": topic_arn, "policy": doc(topic_policy_statements)}),
	"key_policy": rc("aws_kms_key_policy", "module.kms.aws_kms_key_policy.this", {"key_id": "k", "policy": doc(key_statements)}),
	"heartbeat_role": rc("aws_iam_role", "module.heartbeat.aws_iam_role.this", {"name": "tabp-sandbox-ops-heartbeat", "arn": heartbeat_arn, "permissions_boundary": boundary, "assume_role_policy": doc(heartbeat_trust)}),
	"heartbeat_policy": rc("aws_iam_role_policy", "module.heartbeat.aws_iam_role_policy.this", {"name": "publish", "role": "tabp-sandbox-ops-heartbeat", "policy": doc([{"Effect": "Allow", "Action": "sns:Publish", "Resource": topic_arn}])}),
	"group": rc("aws_scheduler_schedule_group", "module.heartbeat.aws_scheduler_schedule_group.this", {"name": "tabp-sandbox-ops-heartbeat", "arn": group_arn}),
	"schedule": rc("aws_scheduler_schedule", "module.heartbeat.aws_scheduler_schedule.this", {"target": [{"arn": topic_arn, "role_arn": heartbeat_arn}]}),
	"chat_role": rc("aws_iam_role", "module.chat.aws_iam_role.this", {"name": "tabp-sandbox-ops-chat", "arn": chat_arn, "permissions_boundary": boundary, "assume_role_policy": doc(chat_trust)}),
	"chat_policy": rc("aws_iam_policy", "module.chat.aws_iam_policy.read", {"arn": read_arn, "policy": doc(read_statements)}),
	"attachments": rc("aws_iam_role_policy_attachments_exclusive", "module.chat.aws_iam_role_policy_attachments_exclusive.this", {"role_name": "tabp-sandbox-ops-chat", "policy_arns": [read_arn]}),
	"configuration": rc("aws_chatbot_slack_channel_configuration", "module.chat.aws_chatbot_slack_channel_configuration.this", {"iam_role_arn": chat_arn, "guardrail_policy_arns": [read_arn], "user_authorization_required": true}),
}

messages(changes) := msgs if {
	msgs := gate.deny with input as fixture.plan([v | some _, v in changes])
}

with_after(key, patch) := object.union(base, {key: object.union(base[key], {"change": {"after": patch}})})

without(key) := object.remove(base, [key])

test_pass_reference_shape if {
	count(messages(base)) == 0
}

test_violation_declaration_missing if {
	msgs := gate.deny with input as fixture.plan([v | some _, v in base]) with data.policy_plan.ops_notification as {}
	fixture.has("[PLAN-NOTIFY-00] 宣言の policy_plan.ops_notification.topic が、空でない文字列ではありません", msgs)
	fixture.has("[PLAN-NOTIFY-00] 宣言の policy_plan.ops_notification.publishers が、空でない文字列の配列ではありません", msgs)
}

test_violation_declared_address_missing if {
	fixture.has("[PLAN-NOTIFY-00] 宣言の policy_plan.ops_notification.chat.configuration の ", messages(without("configuration")))
}

test_violation_unknown_arn if {
	t := object.union(base.topic, {"change": {"after": {"arn": null}, "after_unknown": {"arn": true}}})
	fixture.has("[PLAN-NOTIFY-00] module.ops_notification.module.sns.aws_sns_topic.this の arn が plan で決まりません", messages(object.union(base, {"topic": t})))
}

test_violation_topic_policy_publishers_differ if {
	statements := [s | some s in topic_policy_statements; s.Principal != {"Service": "costalerts.amazonaws.com"}]
	fixture.has("[PLAN-NOTIFY-01] module.ops_notification.module.sns.aws_sns_topic_policy.this の送り手", messages(with_after("topic_policy", {"policy": doc(statements)})))
}

test_violation_key_policy_publishers_differ if {
	statements := array.concat(key_statements, [{"Effect": "Allow", "Principal": {"Service": "logs.ap-northeast-1.amazonaws.com"}, "Action": "kms:Decrypt", "Resource": "*"}])
	fixture.has("[PLAN-NOTIFY-01] module.ops_notification.module.kms.aws_kms_key_policy.this のサービス principal", messages(with_after("key_policy", {"policy": doc(statements)})))
}

test_violation_events_statement_does_not_name_topic if {
	statements := [s | some s in key_statements; object.get(s, "Sid", "") != "SnsPublisherEvents"]
	other := {"Effect": "Allow", "Principal": {"Service": "events.amazonaws.com"}, "Action": "kms:Decrypt", "Resource": "*", "Condition": {"StringEquals": {"kms:EncryptionContext:aws:sns:topicArn": ["arn:aws:sns:ap-northeast-1:123456789012:other"]}}}
	fixture.has("[PLAN-NOTIFY-02] module.ops_notification.module.kms.aws_kms_key_policy.this の events.amazonaws.com の文", messages(with_after("key_policy", {"policy": doc(array.concat(statements, [other]))})))
}

test_violation_iam_publisher_without_key_statement if {
	statements := [s | some s in key_statements; object.get(s, "Sid", "") != "RolesViaSns0"]
	fixture.has("[PLAN-NOTIFY-03] IAM で Topic へ sns:Publish を持つ role", messages(with_after("key_policy", {"policy": doc(statements)})))
}

test_violation_extra_iam_publisher if {
	extra_role := rc("aws_iam_role", "module.x.aws_iam_role.this", {"name": "x", "arn": "arn:aws:iam::123456789012:role/x", "permissions_boundary": boundary, "assume_role_policy": doc(chat_trust)})
	extra_policy := rc("aws_iam_role_policy", "module.x.aws_iam_role_policy.this", {"name": "p", "role": "x", "policy": doc([{"Effect": "Allow", "Action": "sns:*", "Resource": "*"}])})
	fixture.has("[PLAN-NOTIFY-03] IAM で Topic へ sns:Publish を持つ role", messages(object.union(base, {"extra_role": extra_role, "extra_policy": extra_policy})))
}

test_violation_heartbeat_trust if {
	every statements in [
		[object.remove(heartbeat_trust[0], ["Condition"])],
		[object.union(heartbeat_trust[0], {"Condition": {"StringEquals": {"aws:SourceArn": "arn:aws:scheduler:ap-northeast-1:123456789012:schedule/tabp-sandbox-ops-heartbeat/x"}}})],
		[object.union(heartbeat_trust[0], {"Principal": {"Service": "events.amazonaws.com"}})],
		array.concat(heartbeat_trust, chat_trust),
	] {
		fixture.has("[PLAN-NOTIFY-04] module.ops_notification.module.heartbeat.aws_iam_role.this の信頼", messages(with_after("heartbeat_role", {"assume_role_policy": doc(statements)})))
	}
}

test_violation_chat_trust if {
	fixture.has("[PLAN-NOTIFY-04] module.ops_notification.module.chat.aws_iam_role.this の信頼", messages(with_after("chat_role", {"assume_role_policy": doc([object.union(chat_trust[0], {"Principal": {"AWS": "*"}})])})))
}

test_violation_heartbeat_policy_beyond_publish if {
	every statements in [
		[{"Effect": "Allow", "Action": "sns:*", "Resource": topic_arn}],
		[{"Effect": "Allow", "Action": "sns:Publish", "Resource": "*"}],
		[{"Effect": "Allow", "Action": "sns:Publish", "Resource": topic_arn}, {"Effect": "Allow", "Action": "s3:GetObject", "Resource": "arn:aws:s3:::b/k"}],
	] {
		fixture.has("[PLAN-NOTIFY-05] module.ops_notification.module.heartbeat.aws_iam_role_policy.this が、Topic", messages(with_after("heartbeat_policy", {"policy": doc(statements)})))
	}
}

test_violation_heartbeat_policy_on_other_role if {
	fixture.has("[PLAN-NOTIFY-05] module.ops_notification.module.heartbeat.aws_iam_role_policy.this が心拍の role", messages(with_after("heartbeat_policy", {"role": "other"})))
}

test_violation_schedule_target if {
	every t in [[{"arn": "arn:aws:sns:ap-northeast-1:123456789012:other", "role_arn": heartbeat_arn}], [{"arn": topic_arn, "role_arn": chat_arn}], []] {
		s := object.union(base.schedule, {"change": {"after": {"target": t}}})
		fixture.has("[PLAN-NOTIFY-05] module.ops_notification.module.heartbeat.aws_scheduler_schedule.this の target", messages(object.union(base, {"schedule": s})))
	}
}

test_violation_guardrail if {
	every g in [[], ["arn:aws:iam::aws:policy/ReadOnlyAccess"], [read_arn, "arn:aws:iam::aws:policy/ReadOnlyAccess"]] {
		fixture.has("[PLAN-NOTIFY-06] module.ops_notification.module.chat.aws_chatbot_slack_channel_configuration.this の guardrail", messages(with_after("configuration", {"guardrail_policy_arns": g})))
	}
}

test_violation_channel_role_policy_differs if {
	fixture.has("[PLAN-NOTIFY-06] module.ops_notification.module.chat.aws_iam_role_policy_attachments_exclusive.this が channel role に付ける policy", messages(with_after("attachments", {"policy_arns": ["arn:aws:iam::aws:policy/ReadOnlyAccess"]})))
	fixture.has("[PLAN-NOTIFY-06] module.ops_notification.module.chat.aws_iam_role_policy_attachments_exclusive.this が channel role", messages(with_after("attachments", {"role_name": "other"})))
}

test_violation_user_authorization if {
	fixture.has("[PLAN-NOTIFY-06] module.ops_notification.module.chat.aws_chatbot_slack_channel_configuration.this の user_authorization_required", messages(with_after("configuration", {"user_authorization_required": false})))
}

test_violation_configuration_role if {
	fixture.has("[PLAN-NOTIFY-06] module.ops_notification.module.chat.aws_chatbot_slack_channel_configuration.this の channel role", messages(with_after("configuration", {"iam_role_arn": heartbeat_arn})))
}

test_violation_read_actions_differ if {
	extra := array.concat(read_statements, [{"Effect": "Allow", "Action": "cloudwatch:GetSpaceCredentials", "Resource": "*"}])
	fixture.has("[PLAN-NOTIFY-07] module.ops_notification.module.chat.aws_iam_policy.read の Action", messages(with_after("chat_policy", {"policy": doc(extra)})))
	fixture.has("[PLAN-NOTIFY-07] module.ops_notification.module.chat.aws_iam_policy.read の Action", messages(with_after("chat_policy", {"policy": doc([read_statements[0]])})))
}

test_pass_read_actions_case_insensitive if {
	statements := [object.union(s, {"Action": [upper(a) | some a in s.Action]}) | some s in read_statements]
	count(messages(with_after("chat_policy", {"policy": doc(statements)}))) == 0
}

test_violation_read_not_action if {
	statements := array.concat(read_statements, [{"Effect": "Allow", "NotAction": "cloudwatch:DescribeAlarms", "Resource": "arn:aws:cloudwatch:ap-northeast-1:123456789012:alarm:x"}])
	fixture.has("[PLAN-NOTIFY-07] module.ops_notification.module.chat.aws_iam_policy.read が、NotAction・NotResource を使わない Allow だけではありません", messages(with_after("chat_policy", {"policy": doc(statements)})))
}

test_violation_role_without_boundary if {
	r := object.union(base.chat_role, {"change": {"after": {"permissions_boundary": null}}})
	fixture.has("[PLAN-NOTIFY-08] p.json: module.ops_notification.module.chat.aws_iam_role.this が permissions boundary を持ちません", messages(object.union(base, {"chat_role": r})))
}

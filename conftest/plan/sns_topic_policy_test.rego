package plan.sns_topic_policy_test

import data.plan.fixture
import data.plan.sns_topic_policy as gate

arn := "arn:aws:sns:ap-northeast-1:123456789012:tabp-sandbox-ops-notification"

topic_address := "module.x.module.sns.aws_sns_topic.this"

policy_address := "module.x.module.sns.aws_sns_topic_policy.this"

tls_statement := {
	"Sid": "DenyInsecureTransport",
	"Effect": "Deny",
	"Principal": "*",
	"Action": "sns:Publish",
	"Resource": arn,
	"Condition": {"Bool": {"aws:SecureTransport": "false"}},
}

allow(service) := {
	"Effect": "Allow",
	"Principal": {"Service": sprintf("%s.amazonaws.com", [service])},
	"Action": "sns:Publish",
	"Resource": arn,
	"Condition": {"StringEquals": {"aws:SourceAccount": "123456789012"}},
}

good := [tls_statement, allow("budgets"), allow("cloudwatch"), allow("costalerts"), allow("events")]

topic := {"address": topic_address, "type": "aws_sns_topic", "change": {"actions": ["create"], "after": {"arn": arn, "policy": null}, "after_unknown": {}}}

policy(address, statements) := {
	"address": address,
	"type": "aws_sns_topic_policy",
	"change": {"actions": ["create"], "after": {"arn": arn, "policy": json.marshal({"Version": "2012-10-17", "Statement": statements})}, "after_unknown": {}},
}

messages(rcs) := msgs if {
	msgs := gate.deny with input as fixture.plan(rcs)
}

with_statements(statements) := messages([topic, policy(policy_address, statements)])

test_pass_service_publishers_and_tls_deny if {
	count(with_statements(good)) == 0
}

test_pass_tls_deny_only if {
	count(with_statements([tls_statement])) == 0
}

test_pass_aws_star_principal_in_deny if {
	count(with_statements([object.union(tls_statement, {"Principal": {"AWS": "*"}})])) == 0
}

test_violation_unreadable_policy if {
	rc := {"address": policy_address, "type": "aws_sns_topic_policy", "change": {"actions": ["create"], "after": {"arn": arn}, "after_unknown": {"policy": true}}}
	fixture.has("[PLAN-SNS-00] p.json: module.x.module.sns.aws_sns_topic_policy.this の policy が plan で決まらない", messages([topic, rc]))
}

test_violation_unknown_policy_arn if {
	rc := json.patch(policy(policy_address, good), [{"op": "remove", "path": "/change/after/arn"}])
	fixture.has("[PLAN-SNS-00] p.json: module.x.module.sns.aws_sns_topic_policy.this の arn が plan で決まりません", messages([topic, rc]))
}

test_violation_missing_tls_deny if {
	every statements in [
		[allow("cloudwatch")],
		[object.union(tls_statement, {"Condition": {"Bool": {"aws:SecureTransport": "true"}}})],
		[object.union(tls_statement, {"Principal": {"Service": "events.amazonaws.com"}})],
		[object.union(tls_statement, {"Action": "sns:Subscribe"})],
		[object.union(tls_statement, {"Resource": "*"})],
		[object.union(tls_statement, {"Effect": "Allow"})],
	] {
		fixture.has("[PLAN-SNS-01] p.json: module.x.module.sns.aws_sns_topic_policy.this が、TLS でない sns:Publish", with_statements(statements))
	}
}

test_violation_non_service_principal if {
	every p in ["*", {"AWS": "*"}, {"AWS": "arn:aws:iam::123456789012:root"}, {"Service": "events.amazonaws.com", "AWS": "*"}, {"Service": "*"}] {
		fixture.has("[PLAN-SNS-02] p.json: module.x.module.sns.aws_sns_topic_policy.this の Allow の Principal", with_statements([tls_statement, object.union(allow("events"), {"Principal": p})]))
	}
}

test_violation_not_principal if {
	s := object.union(object.remove(allow("events"), ["Principal"]), {"NotPrincipal": {"AWS": "arn:aws:iam::123456789012:root"}})
	fixture.has("[PLAN-SNS-02] p.json: module.x.module.sns.aws_sns_topic_policy.this の Allow の Principal", with_statements([tls_statement, s]))
}

test_violation_action_beyond_publish if {
	every a in ["sns:*", ["sns:Publish", "sns:Subscribe"], "SNS:Subscribe", "*"] {
		fixture.has("[PLAN-SNS-03] p.json: module.x.module.sns.aws_sns_topic_policy.this の Allow の Action が sns:Publish だけではありません", with_statements([tls_statement, object.union(allow("events"), {"Action": a})]))
	}
	s := object.union(object.remove(allow("events"), ["Action"]), {"NotAction": "sns:Subscribe"})
	fixture.has("[PLAN-SNS-03] p.json: module.x.module.sns.aws_sns_topic_policy.this の Allow の Action が sns:Publish だけではありません", with_statements([tls_statement, s]))
}

test_pass_action_case_insensitive if {
	count(with_statements([tls_statement, object.union(allow("events"), {"Action": ["SNS:Publish"]})])) == 0
}

test_violation_resource_beyond_topic if {
	every res in ["*", "arn:aws:sns:ap-northeast-1:123456789012:*", [arn, "arn:aws:sns:ap-northeast-1:123456789012:other"]] {
		fixture.has("[PLAN-SNS-03] p.json: module.x.module.sns.aws_sns_topic_policy.this の Allow の Resource が Topic の ARN", with_statements([tls_statement, object.union(allow("events"), {"Resource": res})]))
	}
}

test_violation_condition_not_fixed_own_account if {
	every c in [
		null,
		{},
		{"StringEquals": {"aws:SourceAccount": "210987654321"}},
		{"StringEquals": {"aws:SourceAccount": ["123456789012", "210987654321"]}},
		{"StringLike": {"aws:SourceAccount": "123456789012"}},
		{"StringEquals": {"aws:SourceAccount": "123456789012"}, "ArnLike": {"aws:SourceArn": "arn:aws:cloudwatch:ap-northeast-1:123456789012:alarm:*"}},
		{"StringEquals": {"aws:SourceAccount": "123456789012", "aws:SourceOrgID": "o-abc"}},
		{"StringEquals": {"aws:SourceOwner": "123456789012"}},
	] {
		s := object.union(object.remove(allow("events"), ["Condition"]), {"Condition": c})
		fixture.has("[PLAN-SNS-04] p.json: module.x.module.sns.aws_sns_topic_policy.this の Allow の Condition が、Topic のアカウント 123456789012", with_statements([tls_statement, s]))
	}
	s := object.remove(allow("events"), ["Condition"])
	fixture.has("[PLAN-SNS-04] p.json: module.x.module.sns.aws_sns_topic_policy.this の Allow の Condition", with_statements([tls_statement, s]))
}

test_violation_topic_without_policy if {
	fixture.has("[PLAN-SNS-05] p.json: Topic module.x.module.sns.aws_sns_topic.this を指す Topic Policy が 0 件です", messages([topic]))
}

test_violation_second_policy_on_topic if {
	fixture.has(
		"[PLAN-SNS-05] p.json: Topic module.x.module.sns.aws_sns_topic.this を指す Topic Policy が 2 件です",
		messages([topic, policy(policy_address, good), policy("module.evil.aws_sns_topic_policy.x", [tls_statement])]),
	)
}

test_violation_unknown_topic_arn if {
	t := {"address": topic_address, "type": "aws_sns_topic", "change": {"actions": ["create"], "after": {}, "after_unknown": {"arn": true}}}
	fixture.has("[PLAN-SNS-05] p.json: Topic module.x.module.sns.aws_sns_topic.this の arn が plan で決まらない", messages([t, policy(policy_address, good)]))
}

test_violation_policy_on_topic_outside_input if {
	fixture.has("[PLAN-SNS-05] p.json: module.x.module.sns.aws_sns_topic_policy.this が、入力に無い Topic", messages([policy(policy_address, good)]))
}

test_pass_removed_topic_is_not_checked if {
	t := {"address": topic_address, "type": "aws_sns_topic", "change": {"actions": ["delete"], "before": {"arn": arn}, "after": null}}
	count(messages([t])) == 0
}

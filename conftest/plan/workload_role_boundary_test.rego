package plan.workload_role_boundary_test

import data.plan.fixture
import data.plan.workload_role_boundary as p

boundary := "arn:aws:iam::123456789012:policy/tabp-sandbox-ci-boundary-ops-notification"

role(address, name, b) := {"address": address, "type": "aws_iam_role", "change": {"actions": ["create"], "after": {"name": name, "arn": sprintf("arn:aws:iam::123456789012:role/%s", [name]), "permissions_boundary": b}, "after_unknown": {}}}

heartbeat := "module.ops_notification.module.heartbeat.aws_iam_role.this"

chat := "module.ops_notification.module.chat.aws_iam_role.this"

valid := [role(heartbeat, "tabp-sandbox-ops-heartbeat", boundary), role(chat, "tabp-sandbox-ops-chat", boundary)]

decl := {"modules": {"module.ops_notification.": {"usecase": "ops-notification", "iam_prefix": "ops-"}}}

messages_for(rcs, d) := msgs if {
	msgs := p.deny with input as fixture.plan(rcs) with data.policy_plan as object.union(object.remove(data.policy_plan, ["workload_role_boundary"]), {"workload_role_boundary": d})
}

messages_without_declaration(rcs) := msgs if {
	msgs := p.deny with input as fixture.plan(rcs) with data.policy_plan as object.remove(data.policy_plan, ["workload_role_boundary"])
}

malformed := "[PLAN-WRB-00] data.policy_plan.workload_role_boundary が無いか"

test_pass_valid if {
	count(messages_for(valid, decl)) == 0
}

# 宣言の外の module の role と、宣言した module の role でない resource は見ない。
test_pass_other_resources_ignored if {
	other := role("module.network_foundation.module.vpc.aws_iam_role.flow_logs", "tabp-sandbox-vpc-flow-logs", null)
	policy := {"address": "module.ops_notification.module.chat.aws_iam_policy.read", "type": "aws_iam_policy", "change": {"actions": ["create"], "after": {"name": "tabp-sandbox-ops-chat-read"}, "after_unknown": {}}}
	count(messages_for(array.concat(valid, [other, policy]), decl)) == 0
}

# module の index key を除いた address で宣言と突き合わせる。
test_pass_indexed_module if {
	rcs := [role("module.ops_notification[\"a\"].module.heartbeat.aws_iam_role.this", "tabp-sandbox-ops-heartbeat", boundary)]
	count(messages_for(rcs, decl)) == 0
}

# role の ARN が plan で決まらないときは、アカウントの一致を見ない。
test_pass_unknown_role_arn if {
	rcs := [json.patch(valid[0], [{"op": "remove", "path": "/change/after/arn"}]), valid[1]]
	count(messages_for(rcs, decl)) == 0
}

test_violation_declaration_missing if {
	fixture.has(malformed, messages_without_declaration(valid))
}

test_violation_declaration_malformed if {
	every d in [
		{},
		{"modules": {}},
		{"modules": {"module.ops_notification.": "ops-notification"}},
		{"modules": {"module.ops_notification.": {"usecase": "", "iam_prefix": "ops-"}}},
		{"modules": {"module.ops_notification.": {"usecase": "ops-notification", "iam_prefix": "ops"}}},
		{"modules": {"module.ops_notification.": {"usecase": "ops-notification", "iam_prefix": "ci-"}}},
		{"modules": {"module.ops_notification.": {"usecase": "ops-notification"}}},
		{"modules": {"": {"usecase": "ops-notification", "iam_prefix": "ops-"}}},
		{"modules": {"module.ops_notification.": {"usecase": "ops-notification", "iam_prefix": "ops-"}}, "x": 1},
	] {
		fixture.has(malformed, messages_for(valid, d))
	}
}

test_violation_no_role_in_declared_module if {
	fixture.has("[PLAN-WRB-00] 宣言した module module.ops_notification. の下に aws_iam_role が入力にありません", messages_for([], decl))
}

test_violation_role_without_declared_boundary if {
	every b in [
		null,
		"",
		"arn:aws:iam::123456789012:policy/tabp-sandbox-workload-boundary",
		"arn:aws:iam::123456789012:policy/tabp-sandbox-ci-boundary-other",
		"arn:aws:iam::123456789012:policy/tabp-sandbox-ci-boundary-ops-notification-x",
		"x-arn:aws:iam::123456789012:policy/tabp-sandbox-ci-boundary-ops-notification",
		"arn:aws:iam::210987654321:policy/tabp-sandbox-ci-boundary-ops-notification",
		"arn:aws:iam::123456789012:policy/tabp-dev-ci-boundary-ops-notification",
		"arn:aws:iam::aws:policy/AdministratorAccess",
	] {
		rcs := [role(heartbeat, "tabp-sandbox-ops-heartbeat", b), valid[1]]
		fixture.has(sprintf("[PLAN-WRB-01] p.json: %s の permissions_boundary", [heartbeat]), messages_for(rcs, decl))
	}
}

# role の名前は <name_prefix>- に、宣言した usecase の IAM の接頭辞を続けて始まる。
test_violation_role_name_outside_usecase_prefix if {
	every name in ["tabp-sandbox-heartbeat", "tabp-sandbox-ci-heartbeat", "tabp-sandbox-OPS-heartbeat"] {
		rcs := [role(heartbeat, name, boundary), valid[1]]
		fixture.has(sprintf("[PLAN-WRB-01] p.json: %s の permissions_boundary", [heartbeat]), messages_for(rcs, decl))
	}
}

package plan.deployment_identity_plan_role_test

import data.plan.deployment_identity_plan_role as p
import data.plan.fixture

decl := {"plan_role": {"role": "module.x.aws_iam_role.this[\"plan\"]", "policy": "module.x.aws_iam_role_policy.this[\"plan\"]", "logs_actions": []}}

logs_decl(actions) := {"plan_role": object.union(decl.plan_role, {"logs_actions": actions})}

logs_reads := ["logs:DescribeLogGroups", "logs:GetDelivery", "logs:ListTagsForResource"]

plan_bucket := "arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an"

write_output := {"Effect": "Allow", "Action": "s3:PutObject", "Resource": [sprintf("%s/pr/*", [plan_bucket]), sprintf("%s/branch/*", [plan_bucket])]}

rc(type, address, after) := {"address": address, "type": type, "change": {"actions": ["create"], "after": after, "after_unknown": {}}}

role := rc("aws_iam_role", "module.x.aws_iam_role.this[\"plan\"]", {"name": "ci-plan"})

policies_exclusive := rc("aws_iam_role_policies_exclusive", "module.x.aws_iam_role_policies_exclusive.this[\"plan\"]", {"role_name": "ci-plan", "policy_names": ["plan"]})

attachments_exclusive := rc("aws_iam_role_policy_attachments_exclusive", "module.x.aws_iam_role_policy_attachments_exclusive.this[\"plan\"]", {"role_name": "ci-plan", "policy_arns": []})

plan_policy(statements) := object.union(fixture.role_policy(statements), {"change": {"after": object.union(fixture.role_policy(statements).change.after, {"name": "plan", "role": "ci-plan"})}})

with_role(statements, others) := fixture.plan(array.concat([role, policies_exclusive, attachments_exclusive, plan_policy(statements)], others))

test_pass_reads_and_plan_output_write if {
	statements := [fixture.allow(["s3:GetObject", "s3:ListBucket", "iam:GetRole", "ec2:DescribeVpcs", "s3:HeadObject"]), write_output]
	count(p.deny) == 0 with input as with_role(statements, []) with data.policy_plan.deployment_identity as decl
}

test_pass_reads_with_condition_and_wildcard_resource if {
	read_key := {
		"Effect": "Allow",
		"Action": ["kms:DescribeKey", "kms:GetKeyPolicy"],
		"Resource": "arn:aws:kms:ap-northeast-1:123456789012:key/*",
		"Condition": {"ForAnyValue:StringEquals": {"kms:ResourceAliases": "alias/tabp-admin-tfstate"}},
	}
	list_aliases := {"Effect": "Allow", "Action": "kms:ListAliases", "Resource": "*"}
	count(p.deny) == 0 with input as with_role([read_key, list_aliases, write_output], []) with data.policy_plan.deployment_identity as decl
}

test_pass_deny_statement_is_not_checked if {
	deny := {"Effect": "Deny", "Action": "s3:DeleteObject", "Resource": "*"}
	count(p.deny) == 0 with input as with_role([deny, write_output], []) with data.policy_plan.deployment_identity as decl
}

test_pass_other_role_policy_is_not_checked if {
	apply := fixture.doc("aws_iam_role_policy", "module.x.aws_iam_role_policy.this[\"apply\"]", "policy", {"Statement": [fixture.allow("s3:DeleteObject")]})
	apply_on_apply := object.union(apply, {"change": {"after": object.union(apply.change.after, {"role": "ci-apply"})}})
	count(p.deny) == 0 with input as with_role([write_output], [apply_on_apply]) with data.policy_plan.deployment_identity as decl
}

test_violation_plan_role_policy_missing if {
	fixture.has("[PLAN-ROLE-00]", p.deny) with input as fixture.plan([role, policies_exclusive, attachments_exclusive]) with data.policy_plan.deployment_identity as decl
}

fixture_only := fixture.plan([plan_policy([write_output])])

test_violation_plan_role_missing if {
	fixture.has("[PLAN-ROLE-00]", p.deny) with input as fixture.plan([plan_policy([write_output])]) with data.policy_plan.deployment_identity as decl
}

test_violation_declaration_missing if {
	fixture.has("[PLAN-ROLE-00]", p.deny) with input as with_role([write_output], []) with data.policy_plan.deployment_identity as {}
}

test_violation_not_action if {
	s := {"Effect": "Allow", "NotAction": "s3:DeleteObject", "Resource": "*"}
	fixture.has("[PLAN-ROLE-01]", p.deny) with input as with_role([s], []) with data.policy_plan.deployment_identity as decl
}

test_violation_not_resource if {
	s := {"Effect": "Allow", "Action": "s3:PutObject", "NotResource": "arn:aws:s3:::x/*"}
	fixture.has("[PLAN-ROLE-01]", p.deny) with input as with_role([s], []) with data.policy_plan.deployment_identity as decl
}

test_violation_mutating_action if {
	every a in ["s3:DeleteObject", "iam:CreateRole", "S3:PUTBUCKETPOLICY", "*", "s3:*", "s3:Put*", "kms:Encrypt", "kms:Decrypt", "kms:GenerateDataKey", ["s3:GetObject", "s3:DeleteObject"]] {
		fixture.has("[PLAN-ROLE-02]", p.deny) with input as with_role([fixture.allow(a)], []) with data.policy_plan.deployment_identity as decl
	}
}

test_violation_mutating_action_with_condition if {
	s := {
		"Effect": "Allow",
		"Action": "kms:PutKeyPolicy",
		"Resource": "*",
		"Condition": {"StringEquals": {"aws:ResourceAccount": "123456789012"}},
	}
	fixture.has("[PLAN-ROLE-02]", p.deny) with input as with_role([s], []) with data.policy_plan.deployment_identity as decl
}

test_violation_put_object_outside_plan_output_keys if {
	every r in [
		"*",
		"arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an/identity/terraform.tfstate",
		sprintf("%s/*", [plan_bucket]),
		sprintf("%s/pr/1/x.txt", [plan_bucket]),
		"arn:aws:s3:::*-tfplan-*/pr/*",
		sprintf("%s-*/pr/*", [plan_bucket]),
	] {
		s := {"Effect": "Allow", "Action": "s3:PutObject", "Resource": r}
		fixture.has("[PLAN-ROLE-03]", p.deny) with input as with_role([s], []) with data.policy_plan.deployment_identity as decl
	}
}

test_violation_put_object_without_resource if {
	s := {"Effect": "Allow", "Action": "s3:PutObject"}
	fixture.has("[PLAN-ROLE-03]", p.deny) with input as with_role([s], []) with data.policy_plan.deployment_identity as decl
}

test_violation_extra_policy_on_plan_role if {
	extra := rc("aws_iam_role_policy", "module.x.aws_iam_role_policy.extra", {"role": "ci-plan", "policy": "{}"})
	attachment := rc("aws_iam_role_policy_attachment", "module.x.aws_iam_role_policy_attachment.admin", {"role": "ci-plan", "policy_arn": "arn:aws:iam::aws:policy/AdministratorAccess"})
	every other in [extra, attachment] {
		fixture.has("[PLAN-ROLE-04]", p.deny) with input as with_role([write_output], [other]) with data.policy_plan.deployment_identity as decl
	}
}

test_violation_unknown_role_of_policy if {
	extra := {"address": "module.x.aws_iam_role_policy.extra", "type": "aws_iam_role_policy", "change": {"actions": ["create"], "after": {"policy": "{}"}, "after_unknown": {"role": true}}}
	fixture.has("[PLAN-ROLE-04]", p.deny) with input as with_role([write_output], [extra]) with data.policy_plan.deployment_identity as decl
}

test_violation_exclusive_missing if {
	every rcs in [[role, policies_exclusive, plan_policy([write_output])], [role, attachments_exclusive, plan_policy([write_output])]] {
		fixture.has("[PLAN-ROLE-04]", p.deny) with input as fixture.plan(rcs) with data.policy_plan.deployment_identity as decl
	}
}

test_violation_exclusive_allows_more if {
	names := rc("aws_iam_role_policies_exclusive", "module.x.aws_iam_role_policies_exclusive.this[\"plan\"]", {"role_name": "ci-plan", "policy_names": ["plan", "extra"]})
	arns := rc("aws_iam_role_policy_attachments_exclusive", "module.x.aws_iam_role_policy_attachments_exclusive.this[\"plan\"]", {"role_name": "ci-plan", "policy_arns": ["arn:aws:iam::aws:policy/AdministratorAccess"]})
	every rcs in [[role, names, attachments_exclusive, plan_policy([write_output])], [role, policies_exclusive, arns, plan_policy([write_output])]] {
		fixture.has("[PLAN-ROLE-04]", p.deny) with input as fixture.plan(rcs) with data.policy_plan.deployment_identity as decl
	}
}

test_pass_declared_logs_actions if {
	count(p.deny) == 0 with input as with_role([fixture.allow(logs_reads), write_output], []) with data.policy_plan.deployment_identity as logs_decl(logs_reads)
}

test_pass_declared_logs_action_in_other_case if {
	count(p.deny) == 0 with input as with_role([fixture.allow("LOGS:describeloggroups"), write_output], []) with data.policy_plan.deployment_identity as logs_decl(["logs:DescribeLogGroups"])
}

roles05(msgs, action) := {m | some m in msgs; startswith(m, "[PLAN-ROLE-05]"); contains(m, sprintf(" %s ", [action]))}

# get の動詞を持ち PLAN-ROLE-02 を通るが、ログの event を返す。大文字の service 名と Condition でも同じ。
test_violation_log_events_read if {
	every s in [
		fixture.allow(["logs:GetDelivery", "logs:GetLogEvents"]),
		fixture.allow(["logs:GetDelivery", "LOGS:GetLogEvents"]),
		fixture.allow(["logs:GetDelivery", "Logs:getlogevents"]),
		object.union(fixture.allow(["logs:GetDelivery", "logs:GetLogEvents"]), {"Condition": {"StringEquals": {"aws:RequestedRegion": "ap-northeast-1"}}}),
	] {
		msgs := p.deny with input as with_role([s, write_output], []) with data.policy_plan.deployment_identity as logs_decl(["logs:GetDelivery"])
		count({m | some m in msgs; startswith(m, "[PLAN-ROLE-05]")}) == 1
		count(roles05(msgs, "logs:getlogevents")) == 1
	}
}

# 宣言に在る Action に当たる部分 wildcard も、宣言に無い文字列なので落ちる。
test_violation_logs_partial_wildcard if {
	every a in ["logs:Get*", "logs:GetDeliver?", "logs:*"] {
		msgs := p.deny with input as with_role([fixture.allow(["logs:GetDelivery", a]), write_output], []) with data.policy_plan.deployment_identity as logs_decl(["logs:GetDelivery"])
		count(roles05(msgs, lower(a))) == 1
	}
}

test_violation_logs_actions_empty if {
	msgs := p.deny with input as with_role([fixture.allow("logs:DescribeLogGroups"), write_output], []) with data.policy_plan.deployment_identity as decl
	count(roles05(msgs, "logs:describeloggroups")) == 1
}

# with は data.policy_plan の下を decl.json とマージするので、キーの欠落は data.policy_plan ごと渡して作る。
test_violation_logs_actions_key_missing if {
	policy_plan := json.remove(object.union(data.policy_plan, {"deployment_identity": decl}), ["/deployment_identity/plan_role/logs_actions"])
	msgs := p.deny with input as with_role([fixture.allow("logs:DescribeLogGroups"), write_output], []) with data.policy_plan as policy_plan
	count(roles05(msgs, "logs:describeloggroups")) == 1
}

# 宣言の形が崩れたら、logs: の Action を許さずに落とす。
test_violation_logs_actions_not_array if {
	every bad in ["logs:DescribeLogGroups", {"a": "logs:DescribeLogGroups"}, null, 5] {
		msgs := p.deny with input as with_role([fixture.allow("logs:DescribeLogGroups"), write_output], []) with data.policy_plan.deployment_identity as logs_decl(bad)
		fixture.has("[PLAN-ROLE-07] data.policy_plan.deployment_identity.plan_role.logs_actions が配列ではありません", msgs)
		count(roles05(msgs, "logs:describeloggroups")) == 1
	}
}

test_violation_logs_actions_non_string_entry if {
	msgs := p.deny with input as with_role([fixture.allow("logs:DescribeLogGroups"), write_output], []) with data.policy_plan.deployment_identity as logs_decl(["logs:DescribeLogGroups", 5])
	fixture.has("[PLAN-ROLE-07] data.policy_plan.deployment_identity.plan_role.logs_actions に文字列でない要素 5 ", msgs)
}

# 宣言と policy を同じ向きに動かしても、wildcard と、閉じた集合の外の Action は宣言に置けない。
test_violation_declared_logs_action_malformed if {
	every a in ["logs:Get*", "logs:GetLogEvent?", "logs:*", "s3:GetObject", "logs:", "xlogs:getdelivery"] {
		msgs := p.deny with input as with_role([fixture.allow(a), write_output], []) with data.policy_plan.deployment_identity as logs_decl([a])
		some m in msgs
		startswith(m, sprintf("[PLAN-ROLE-07] data.policy_plan.deployment_identity.plan_role.logs_actions の %s は", [lower(a)]))
	}
}

test_violation_declared_logs_action_outside_closed_set if {
	every a in ["logs:GetLogEvents", "logs:GetLogRecord", "logs:GetQueryResults", "logs:ListAnomalies", "logs:FilterLogEvents", "logs:StartQuery", "logs:StartLiveTail", "logs:Unmask", "logs:GetLogObject"] {
		msgs := p.deny with input as with_role([fixture.allow(a), write_output], []) with data.policy_plan.deployment_identity as logs_decl([a])
		violations := {m | some m in msgs; startswith(m, "[PLAN-ROLE-08]")}
		count(violations) == 1
		some m in violations
		contains(m, sprintf(" %s ", [lower(a)]))
	}
}

# 数字を含む Action も形は通り、閉じた集合の外として落ちる。
test_violation_declared_logs_action_with_digit if {
	msgs := p.deny with input as with_role([fixture.allow("logs:ListSourcesForS3TableIntegration"), write_output], []) with data.policy_plan.deployment_identity as logs_decl(["logs:ListSourcesForS3TableIntegration"])
	fixture.has("[PLAN-ROLE-08]", msgs)
	not fixture.has("[PLAN-ROLE-07]", msgs)
}

# 使われているかは、plan の role の Allow だけで数える。Deny の文と別の role の Allow は数えない。
test_violation_declared_logs_action_unused if {
	other := fixture.doc("aws_iam_role_policy", "module.x.aws_iam_role_policy.this[\"apply\"]", "policy", {"Statement": [fixture.allow("logs:GetDelivery")]})
	other_on_apply := object.union(other, {"change": {"after": object.union(other.change.after, {"role": "ci-apply"})}})
	every inputs in [
		with_role([fixture.allow("logs:DescribeLogGroups"), write_output], []),
		with_role([fixture.allow("logs:DescribeLogGroups"), {"Effect": "Deny", "Action": "logs:GetDelivery", "Resource": "*"}, write_output], []),
		with_role([fixture.allow("logs:DescribeLogGroups"), write_output], [other_on_apply]),
	] {
		msgs := p.deny with input as inputs with data.policy_plan.deployment_identity as logs_decl(["logs:DescribeLogGroups", "logs:GetDelivery"])
		violations := {m | some m in msgs; startswith(m, "[PLAN-ROLE-06]")}
		count(violations) == 1
		some m in violations
		contains(m, " logs:getdelivery ")
	}
}

test_violation_declared_logs_actions_without_any_logs_allow if {
	fixture.has("[PLAN-ROLE-06]", p.deny) with input as with_role([write_output], []) with data.policy_plan.deployment_identity as logs_decl(["logs:DescribeLogGroups"])
}

test_pass_service_name_with_hyphen if {
	count(p.deny) == 0 with input as with_role([fixture.allow(["resource-groups:GetGroup", "s3:GetObject"]), write_output], []) with data.policy_plan.deployment_identity as decl
}

test_violation_service_name_wildcard if {
	every a in ["log?:GetLogEvents", "l*:GetLogEvents", "?ogs:GetLogEvents", "LOG?:GetLogEvents", "log?:StartQuery", "?3:GetObject", ":GetLogEvents", "logs"] {
		msgs := p.deny with input as with_role([fixture.allow(a), write_output], []) with data.policy_plan.deployment_identity as decl
		some m in msgs
		startswith(m, "[PLAN-ROLE-09]")
		contains(m, sprintf(" %s ", [lower(a)]))
	}
}

package plan.deployment_identity_plan_role_test

import data.plan.deployment_identity_plan_role as p
import data.plan.fixture
import data.plan.input as gate

decl := {"plan_role": {"role": "module.x.aws_iam_role.this[\"plan\"]", "policy": "module.x.aws_iam_role_policy.this[\"plan\"]"}}

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
		fixture.has(sprintf("[PLAN-ROLE-04] p.json: %s が plan の role に権限を足しています", [other.address]), p.deny) with input as with_role([write_output], [other]) with data.policy_plan.deployment_identity as decl
	}
}

test_violation_unknown_role_of_policy if {
	extra := {"address": "module.x.aws_iam_role_policy.extra", "type": "aws_iam_role_policy", "change": {"actions": ["create"], "after": {"policy": "{}"}, "after_unknown": {"role": true}}}
	fixture.has(sprintf("[PLAN-ROLE-04] p.json: %s がどの role に付くかを plan で読めません", [extra.address]), p.deny) with input as with_role([write_output], [extra]) with data.policy_plan.deployment_identity as decl
}

test_violation_unknown_role_of_attachment_and_exclusive if {
	every extra in [
		{"address": "module.x.aws_iam_role_policy_attachment.later", "type": "aws_iam_role_policy_attachment", "change": {"actions": ["create"], "after": {}, "after_unknown": {"role": true}}},
		{"address": "module.x.aws_iam_role_policies_exclusive.later", "type": "aws_iam_role_policies_exclusive", "change": {"actions": ["create"], "after": {}, "after_unknown": {"role_name": true}}},
		{"address": "module.x.aws_iam_role_policy_attachments_exclusive.later", "type": "aws_iam_role_policy_attachments_exclusive", "change": {"actions": ["create"], "after": {}, "after_unknown": {"role_name": true}}},
	] {
		fixture.has(sprintf("[PLAN-ROLE-04] p.json: %s がどの role に付くかを plan で読めません", [extra.address]), p.deny) with input as with_role([write_output], [extra]) with data.policy_plan.deployment_identity as decl
	}
}

test_violation_exclusive_count if {
	every e in [policies_exclusive, attachments_exclusive] {
		without := [r | some r in [role, policies_exclusive, attachments_exclusive, plan_policy([write_output])]; r.address != e.address]
		fixture.has(sprintf("[PLAN-ROLE-04] plan の role ci-plan の %s が 0 件です", [e.type]), p.deny) with input as fixture.plan(without) with data.policy_plan.deployment_identity as decl
		twice := json.patch(e, [{"op": "replace", "path": "/address", "value": sprintf("%s.again", [e.address])}])
		fixture.has(sprintf("[PLAN-ROLE-04] plan の role ci-plan の %s が 2 件です", [e.type]), p.deny) with input as with_role([write_output], [twice]) with data.policy_plan.deployment_identity as decl
	}
}

test_violation_exclusive_allows_more if {
	names := rc("aws_iam_role_policies_exclusive", "module.x.aws_iam_role_policies_exclusive.this[\"plan\"]", {"role_name": "ci-plan", "policy_names": ["plan", "extra"]})
	arns := rc("aws_iam_role_policy_attachments_exclusive", "module.x.aws_iam_role_policy_attachments_exclusive.this[\"plan\"]", {"role_name": "ci-plan", "policy_arns": ["arn:aws:iam::aws:policy/AdministratorAccess"]})
	fixture.has(sprintf("[PLAN-ROLE-04] p.json: %s の policy_names が", [names.address]), p.deny) with input as fixture.plan([role, names, attachments_exclusive, plan_policy([write_output])]) with data.policy_plan.deployment_identity as decl
	fixture.has(sprintf("[PLAN-ROLE-04] p.json: %s の policy_arns が空ではありません", [arns.address]), p.deny) with input as fixture.plan([role, policies_exclusive, arns, plan_policy([write_output])]) with data.policy_plan.deployment_identity as decl
}

test_violation_exclusive_contents_absent if {
	every c in [
		{"e": policies_exclusive, "attr": "policy_names", "says": "が"},
		{"e": attachments_exclusive, "attr": "policy_arns", "says": "が空ではありません"},
	] {
		absent := json.patch(c.e, [{"op": "remove", "path": sprintf("/change/after/%s", [c.attr])}])
		others := [r | some r in [role, policies_exclusive, attachments_exclusive, plan_policy([write_output])]; r.address != c.e.address]
		fixture.has(sprintf("[PLAN-ROLE-04] p.json: %s の %s %s", [c.e.address, c.attr, c.says]), p.deny) with input as fixture.plan(array.concat(others, [absent])) with data.policy_plan.deployment_identity as decl
	}
}

# 実際の plan には apply の role の排他と attachment も並ぶ。plan の role に付くものだけを数えることを見る。
test_pass_other_role_exclusive_and_attachment_are_not_checked if {
	others := [
		rc("aws_iam_role_policies_exclusive", "module.x.aws_iam_role_policies_exclusive.this[\"apply\"]", {"role_name": "ci-apply", "policy_names": ["apply"]}),
		rc("aws_iam_role_policy_attachments_exclusive", "module.x.aws_iam_role_policy_attachments_exclusive.this[\"apply\"]", {"role_name": "ci-apply", "policy_arns": ["arn:aws:iam::aws:policy/ReadOnlyAccess"]}),
		rc("aws_iam_role_policy_attachment", "module.x.aws_iam_role_policy_attachment.apply", {"role": "ci-apply", "policy_arn": "arn:aws:iam::aws:policy/ReadOnlyAccess"}),
	]
	count(p.deny) == 0 with input as with_role([write_output], others) with data.policy_plan.deployment_identity as decl
}

test_violation_policy_name_unknown if {
	unnamed := json.patch(plan_policy([write_output]), [{"op": "remove", "path": "/change/after/name"}, {"op": "add", "path": "/change/after_unknown/name", "value": true}])
	plan := fixture.plan([role, policies_exclusive, attachments_exclusive, unnamed])
	fixture.has(sprintf("[PLAN-ROLE-04] p.json: %s の policy_names が [null] ではありません", [policies_exclusive.address]), p.deny) with input as plan with data.policy_plan.deployment_identity as decl
}

test_pass_service_name_with_hyphen if {
	count(p.deny) == 0 with input as with_role([fixture.allow(["resource-groups:GetGroup", "s3:GetObject", "S3:GetBucketPolicy"]), write_output], []) with data.policy_plan.deployment_identity as decl
}

test_violation_service_name_wildcard if {
	every a in ["*:GetSecretValue", "*:Get*", "log?:GetLogEvents", "l*:GetLogEvents", "?ogs:GetLogEvents", "LOG?:GetLogEvents", "log?:StartQuery", "?3:GetObject", ":GetLogEvents", "logs"] {
		msgs := p.deny with input as with_role([fixture.allow(a), write_output], []) with data.policy_plan.deployment_identity as decl
		some m in msgs
		startswith(m, "[PLAN-ROLE-05]")
		contains(m, sprintf(" %s ", [lower(a)]))
	}
}

# 読めない文書の Allow はこの package からは見えない。落とすのは plan.input の PLAN-INPUT-04 である。
test_violation_unknown_plan_policy_is_left_to_input if {
	unknown := {"address": "module.x.aws_iam_role_policy.this[\"plan\"]", "type": "aws_iam_role_policy", "change": {"actions": ["create"], "after": {"name": "plan", "role": "ci-plan"}, "after_unknown": {"policy": true}}}
	plan := fixture.plan([role, policies_exclusive, attachments_exclusive, unknown])
	fixture.has("[PLAN-INPUT-04]", gate.deny) with input as plan with data.policy_plan.deployment_identity as decl
}

test_violation_put_object_policy_variable if {
	every r in ["arn:aws:s3:::${aws:username}-tfplan-x/pr/*", "arn:aws:s3:::tabp-admin-tfplan-${aws:PrincipalAccount}-ap-northeast-1-an/pr/*"] {
		s := {"Effect": "Allow", "Action": "s3:PutObject", "Resource": r}
		msgs := p.deny with input as with_role([s], []) with data.policy_plan.deployment_identity as decl
		{substring(m, 0, 14) | some m in msgs} == {"[PLAN-ROLE-03]"}
		some m in msgs
		contains(m, "policy 変数を含みます")
	}
}

package plan.deployment_identity_plan_role_test

import data.plan.deployment_identity_plan_role as p
import data.plan.fixture
import data.plan.input as gate

# with は decl.json の宣言と合わさるので、read_actions と managed_policies を空にして置き換える。
decl := {"plan_role": {"role": "module.x.aws_iam_role.this[\"plan\"]", "policy": "module.x.aws_iam_role_policy.this[\"plan\"]", "read_actions": [], "managed_policies": []}}

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
	fixture.has(sprintf("[PLAN-ROLE-04] p.json: %s が plan の role に権限を足しています", [extra.address]), p.deny) with input as with_role([write_output], [extra]) with data.policy_plan.deployment_identity as decl
	attachment := rc("aws_iam_role_policy_attachment", "module.x.aws_iam_role_policy_attachment.admin", {"role": "ci-plan", "policy_arn": "arn:aws:iam::aws:policy/AdministratorAccess"})
	fixture.has(sprintf("[PLAN-ROLE-04] p.json: %s が plan の role に付ける policy が [] のどれでもありません", [attachment.address]), p.deny) with input as with_role([write_output], [attachment]) with data.policy_plan.deployment_identity as decl
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
	fixture.has(sprintf("[PLAN-ROLE-04] p.json: %s の policy_arns が [] ではありません", [arns.address]), p.deny) with input as fixture.plan([role, policies_exclusive, arns, plan_policy([write_output])]) with data.policy_plan.deployment_identity as decl
}

test_violation_exclusive_contents_absent if {
	every c in [
		{"e": policies_exclusive, "attr": "policy_names", "says": "が"},
		{"e": attachments_exclusive, "attr": "policy_arns", "says": "が [] ではありません"},
	] {
		absent := json.patch(c.e, [{"op": "remove", "path": sprintf("/change/after/%s", [c.attr])}])
		others := [r | some r in [role, policies_exclusive, attachments_exclusive, plan_policy([write_output])]; r.address != c.e.address]
		fixture.has(sprintf("[PLAN-ROLE-04] p.json: %s の %s %s", [c.e.address, c.attr, c.says]), p.deny) with input as fixture.plan(array.concat(others, [absent])) with data.policy_plan.deployment_identity as decl
	}
}

# 実際の plan には apply の role の排他と attachment も並ぶ。
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

# Resource の配列は、正しい key が混ざっていても要素ごとに見る。
test_violation_put_object_resource_array if {
	valid := sprintf("%s/pr/*", [plan_bucket])
	every resources in [
		[valid, "arn:aws:s3:::${aws:username}-tfplan-x/pr/*"],
		[valid, "*"],
		[valid, 5],
		["*", valid],
	] {
		s := {"Effect": "Allow", "Action": "s3:PutObject", "Resource": resources}
		msgs := p.deny with input as with_role([s], []) with data.policy_plan.deployment_identity as decl
		{substring(m, 0, 14) | some m in msgs} == {"[PLAN-ROLE-03]"}
	}
}

read_decl(actions) := {"plan_role": object.union(decl.plan_role, {"read_actions": actions})}

test_pass_declared_read_action if {
	every allowed in ["budgets:ViewBudget", "BUDGETS:VIEWBUDGET", ["budgets:ViewBudget", "budgets:ListTagsForResource"]] {
		count(p.deny) == 0 with input as with_role([fixture.allow(allowed), write_output], []) with data.policy_plan.deployment_identity as read_decl(["budgets:ViewBudget"])
	}
}

test_violation_undeclared_read_action if {
	every d in [decl, read_decl([]), read_decl(["budgets:ViewBudget"])] {
		msgs := p.deny with input as with_role([fixture.allow(["budgets:ViewBudget", "budgets:ViewBudgetAction"]), write_output], []) with data.policy_plan.deployment_identity as d
		some m in msgs
		startswith(m, "[PLAN-ROLE-02]")
		contains(m, "budgets:viewbudgetaction")
	}
}

test_violation_read_actions_not_array if {
	every actions in ["budgets:ViewBudget", {"a": "budgets:ViewBudget"}, null] {
		msgs := p.deny with input as with_role([fixture.allow("budgets:ViewBudget"), write_output], []) with data.policy_plan.deployment_identity as read_decl(actions)
		fixture.has("[PLAN-ROLE-00] data.policy_plan.deployment_identity.plan_role.read_actions が配列ではありません", msgs)
		fixture.has("[PLAN-ROLE-02]", msgs)
	}
}

test_violation_read_actions_malformed if {
	every x in [5, "", "budgets", "budgets:", ":ViewBudget", "budgets:View*", "*:ViewBudget", "budgets:View?udget", "budgets:ViewBudget:x", "budgets: ViewBudget"] {
		msgs := p.deny with input as with_role([fixture.allow("budgets:ViewBudget"), write_output], []) with data.policy_plan.deployment_identity as read_decl(["budgets:ViewBudget", x])
		fixture.has(sprintf("[PLAN-ROLE-00] data.policy_plan.deployment_identity.plan_role.read_actions の %v が、wildcard を含まない", [x]), msgs)
	}
}

test_violation_read_actions_verb_not_allowed if {
	every x in ["iam:AttachRolePolicy", "kms:Decrypt", "budgets:ModifyBudget", "budgets:ExecuteBudgetAction", "ce:CreateAnomalyMonitor", "ce:TagResource", "s3:GetObject", "BUDGETS:MODIFYBUDGET", "budgets:XView", "iam:XSimulate"] {
		msgs := p.deny with input as with_role([fixture.allow(x), write_output], []) with data.policy_plan.deployment_identity as read_decl([x])
		fixture.has(sprintf("[PLAN-ROLE-00] data.policy_plan.deployment_identity.plan_role.read_actions の %s の動詞が", [x]), msgs)
		not lower(x) in p.declared_reads with data.policy_plan.deployment_identity as read_decl([x])
	}
	every x in ["iam:AttachRolePolicy", "kms:Decrypt", "budgets:ModifyBudget"] {
		msgs := p.deny with input as with_role([fixture.allow(x), write_output], []) with data.policy_plan.deployment_identity as read_decl([x])
		fixture.has(sprintf("[PLAN-ROLE-02] p.json: plan の role の Allow に変更系の Action %s があります", [lower(x)]), msgs)
	}
}

test_pass_declared_view_verb if {
	every x in ["budgets:ViewBudget", "BUDGETS:VIEWBUDGET", "budgets:viewbudget", "aws-portal:ViewBilling", "budgets:ViewUpdateHistory", "budgets:ViewDeleteMarker"] {
		count(p.deny) == 0 with input as with_role([fixture.allow(x), write_output], []) with data.policy_plan.deployment_identity as read_decl([x])
		lower(x) in p.declared_reads with data.policy_plan.deployment_identity as read_decl([x])
	}
}

test_pass_declared_simulate_verb if {
	every x in ["iam:SimulateCustomPolicy", "IAM:SIMULATECUSTOMPOLICY", "iam:simulatecustompolicy"] {
		count(p.deny) == 0 with input as with_role([fixture.allow(x), write_output], []) with data.policy_plan.deployment_identity as read_decl([x])
		lower(x) in p.declared_reads with data.policy_plan.deployment_identity as read_decl([x])
	}
}

test_violation_undeclared_simulate_action if {
	msgs := p.deny with input as with_role([fixture.allow("iam:SimulateCustomPolicy"), write_output], []) with data.policy_plan.deployment_identity as read_decl([])
	fixture.has("[PLAN-ROLE-02] p.json: plan の role の Allow に変更系の Action iam:simulatecustompolicy があります", msgs)
}

# with の data は decl.json と合わさってキーを消せないので、cfg を置き換える。
test_violation_read_actions_missing if {
	msgs := p.deny with input as with_role([fixture.allow("budgets:ViewBudget"), write_output], []) with p.cfg as object.remove(decl.plan_role, ["read_actions"])
	fixture.has("[PLAN-ROLE-02] p.json: plan の role の Allow に変更系の Action budgets:viewbudget があります", msgs)
	not fixture.has("[PLAN-ROLE-00] data.policy_plan.deployment_identity.plan_role.read_actions が配列ではありません", msgs)
}

test_violation_read_actions_non_string if {
	every x in [null, true, ["a"], {"a": 1}] {
		msgs := p.deny with input as with_role([fixture.allow("budgets:ViewBudget"), write_output], []) with data.policy_plan.deployment_identity as read_decl(["budgets:ViewBudget", x])
		fixture.has(sprintf("[PLAN-ROLE-00] data.policy_plan.deployment_identity.plan_role.read_actions の %v が、wildcard を含まない", [x]), msgs)
	}
}

test_violation_read_actions_unused if {
	every statements in [[write_output], [fixture.allow("budgets:ListTagsForResource"), write_output], [{"Effect": "Deny", "Action": "budgets:ViewBudget", "Resource": "*"}, write_output]] {
		msgs := p.deny with input as with_role(statements, []) with data.policy_plan.deployment_identity as read_decl(["budgets:ViewBudget"])
		fixture.has("[PLAN-ROLE-00] data.policy_plan.deployment_identity.plan_role.read_actions の budgets:ViewBudget は、plan の role の Allow で使われていません", msgs)
	}
}

# --- plan の role に付けてよい customer managed policy（deployment-identity/ADR-0201 決定17）---

read_policy_address := "module.x.aws_iam_policy.workload_read"

read_policy_arn := "arn:aws:iam::123456789012:policy/ci-apply-workload-read"

managed_decl := {"plan_role": object.union(decl.plan_role, {"managed_policies": [read_policy_address]})}

read_policy(statements) := object.union(doc_of(statements), {"change": {"after": object.union(doc_of(statements).change.after, {"name": "ci-apply-workload-read", "arn": read_policy_arn})}})

doc_of(statements) := fixture.doc("aws_iam_policy", read_policy_address, "policy", {"Statement": statements})

read_attachment := rc("aws_iam_role_policy_attachment", "module.x.aws_iam_role_policy_attachment.plan_workload_read", {"role": "ci-plan", "policy_arn": read_policy_arn})

attached_exclusive := rc("aws_iam_role_policy_attachments_exclusive", attachments_exclusive.address, {"role_name": "ci-plan", "policy_arns": [read_policy_arn]})

with_managed(statements, others) := fixture.plan(array.concat([role, policies_exclusive, attached_exclusive, plan_policy([write_output]), read_policy(statements), read_attachment], others))

test_pass_declared_managed_policy if {
	count(p.deny) == 0 with input as with_managed([fixture.allow(["sns:GetTopicAttributes", "config:DescribeDeliveryChannels"])], []) with data.policy_plan.deployment_identity as managed_decl
}

test_violation_write_in_managed_policy if {
	fixture.has("[PLAN-ROLE-02] p.json: plan の role の Allow に変更系の Action config:putdeliverychannel", p.deny) with input as with_managed([fixture.allow("config:PutDeliveryChannel")], []) with data.policy_plan.deployment_identity as managed_decl
}

# 宣言した policy の文が allows に入らないと、PLAN-ROLE-02 は付いた policy を見ずに緑になる。
test_violation_not_action_in_managed_policy if {
	s := {"Effect": "Allow", "NotAction": "s3:GetObject", "Resource": "*"}
	fixture.has("[PLAN-ROLE-01]", p.deny) with input as with_managed([s], []) with data.policy_plan.deployment_identity as managed_decl
}

test_violation_managed_policy_missing if {
	plan := fixture.plan([role, policies_exclusive, attached_exclusive, plan_policy([write_output]), read_attachment])
	fixture.has(sprintf("[PLAN-ROLE-00] plan の role に付ける policy %s が入力にないか", [read_policy_address]), p.deny) with input as plan with data.policy_plan.deployment_identity as managed_decl
}

test_violation_managed_policy_arn_unknown if {
	unknown := json.patch(read_policy([fixture.allow("s3:GetObject")]), [{"op": "remove", "path": "/change/after/arn"}, {"op": "add", "path": "/change/after_unknown/arn", "value": true}])
	plan := fixture.plan([role, policies_exclusive, attached_exclusive, plan_policy([write_output]), unknown, read_attachment])
	fixture.has(sprintf("[PLAN-ROLE-00] plan の role に付ける policy %s が入力にないか、ARN が plan で決まっていません", [read_policy_address]), p.deny) with input as plan with data.policy_plan.deployment_identity as managed_decl
}

test_violation_managed_policies_malformed if {
	every bad in ["x", [""], [1]] {
		fixture.has("[PLAN-ROLE-00] data.policy_plan.deployment_identity.plan_role.managed_policies が", p.deny) with input as with_role([write_output], []) with data.policy_plan.deployment_identity as {"plan_role": object.union(decl.plan_role, {"managed_policies": bad})}
	}
}

test_violation_attachment_policy_arn_unknown if {
	unknown := json.patch(read_attachment, [{"op": "remove", "path": "/change/after/policy_arn"}, {"op": "add", "path": "/change/after_unknown/policy_arn", "value": true}])
	plan := fixture.plan([role, policies_exclusive, attached_exclusive, plan_policy([write_output]), read_policy([fixture.allow("s3:GetObject")]), unknown])
	fixture.has(sprintf("[PLAN-ROLE-04] p.json: %s の policy_arn を plan で読めません", [read_attachment.address]), p.deny) with input as plan with data.policy_plan.deployment_identity as managed_decl
}

test_violation_exclusive_without_managed_policy if {
	fixture.has(sprintf("[PLAN-ROLE-04] p.json: %s の policy_arns が [%q] ではありません", [attachments_exclusive.address, read_policy_arn]), p.deny) with input as fixture.plan([role, policies_exclusive, attachments_exclusive, plan_policy([write_output]), read_policy([fixture.allow("s3:GetObject")]), read_attachment]) with data.policy_plan.deployment_identity as managed_decl
}

test_violation_other_policy_attached_beside_managed if {
	admin := rc("aws_iam_role_policy_attachment", "module.x.aws_iam_role_policy_attachment.admin", {"role": "ci-plan", "policy_arn": "arn:aws:iam::aws:policy/AdministratorAccess"})
	fixture.has(sprintf("[PLAN-ROLE-04] p.json: %s が plan の role に付ける policy が", [admin.address]), p.deny) with input as with_managed([fixture.allow("s3:GetObject")], [admin]) with data.policy_plan.deployment_identity as managed_decl
}

# 宣言した policy の ARN が決まらないとき、policy_arn の無い attachment を一致と数えない。
test_violation_attachment_without_policy_arn_while_managed_arn_unknown if {
	unknown := json.patch(read_policy([fixture.allow("s3:GetObject")]), [{"op": "remove", "path": "/change/after/arn"}, {"op": "add", "path": "/change/after_unknown/arn", "value": true}])
	bare := json.patch(read_attachment, [{"op": "remove", "path": "/change/after/policy_arn"}])
	plan := fixture.plan([role, policies_exclusive, attached_exclusive, plan_policy([write_output]), unknown, bare])
	fixture.has(sprintf("[PLAN-ROLE-04] p.json: %s が plan の role に付ける policy が", [read_attachment.address]), p.deny) with input as plan with data.policy_plan.deployment_identity as managed_decl
}

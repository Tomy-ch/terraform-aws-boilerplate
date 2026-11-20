package plan.deployment_identity_workload_policy_test

import data.plan.deployment_identity_workload_policy as p
import data.plan.fixture

g := "module.deployment_identity.module.github_oidc."

iam := "arn:aws:iam::123456789012"

boundary_arn := sprintf("%s:policy/tabp-admin-ci-boundary-ops-notification", [iam])

ops_roles := sprintf("%s:role/tabp-admin-ops-*", [iam])

ops_policies := sprintf("%s:policy/tabp-admin-ops-*", [iam])

workload_address := sprintf("%saws_iam_policy.workload", [g])

read_address := sprintf("%saws_iam_policy.workload_read", [g])

boundary_address := sprintf("%saws_iam_policy.boundary[\"ops-notification\"]", [g])

apply_address := sprintf("%saws_iam_role_policy.this[\"apply\"]", [g])

pb := {"StringEquals": {"iam:PermissionsBoundary": [boundary_arn]}}

slr := sprintf("%s:role/aws-service-role/management.chatbot.amazonaws.com/AWSServiceRoleForAWSChatbot", [iam])

slr_condition := {"StringEquals": {"iam:AWSServiceName": "management.chatbot.amazonaws.com"}}

valid_workload := [
	{"Effect": "Allow", "Action": ["iam:GetRole", "iam:ListRolePolicies"], "Resource": sprintf("%s:role/tabp-admin-*", [iam])},
	{"Effect": "Allow", "Action": ["iam:CreateRole", "iam:PutRolePolicy", "iam:DeleteRole"], "Resource": [ops_roles], "Condition": pb},
	{"Effect": "Allow", "Action": "iam:AttachRolePolicy", "Resource": [ops_roles], "Condition": object.union(pb, {"ArnLike": {"iam:PolicyARN": [ops_policies]}})},
	{"Effect": "Allow", "Action": ["iam:CreatePolicy", "iam:ListPolicyVersions"], "Resource": [ops_policies]},
	{"Effect": "Allow", "Action": "iam:PassRole", "Resource": [ops_roles], "Condition": {"StringEquals": {"iam:PassedToService": ["scheduler.amazonaws.com"]}}},
	{"Effect": "Allow", "Action": "iam:CreateServiceLinkedRole", "Resource": slr, "Condition": slr_condition},
	{"Effect": "Allow", "Action": "sns:CreateTopic", "Resource": "arn:aws:sns:ap-northeast-1:123456789012:tabp-admin-*"},
]

valid_read := [{"Effect": "Allow", "Action": ["sns:GetTopicAttributes"], "Resource": ["arn:aws:sns:ap-northeast-1:123456789012:tabp-admin-*", "arn:aws:sns:us-east-1:123456789012:tabp-admin-*"]}]

bootstrap_denies := [
	{"Sid": "DenyIdentityChange", "Effect": "Deny", "Action": "*", "Resource": [sprintf("%s:role/tabp-admin-ci-*", [iam])]},
	{"Sid": "DenyBreakGlassChange", "Effect": "Deny", "Action": "*", "Resource": [sprintf("%s:role/break-glass/b", [iam])]},
]

apply_only := [{"Sid": "DenyBoundaryRemoval", "Effect": "Deny", "Action": "iam:DeleteRolePermissionsBoundary", "Resource": "*"}]

boundary_allow := [{"Effect": "Allow", "Action": "sns:Publish", "Resource": "arn:aws:sns:ap-northeast-1:123456789012:tabp-admin-ops-*"}]

res(type, address, after) := {"address": address, "type": type, "change": {"actions": ["create"], "after": after, "after_unknown": {}}}

workload_rc(workload) := res("aws_iam_policy", workload_address, {"name": "tabp-admin-ci-apply-workload", "arn": sprintf("%s:policy/tabp-admin-ci-apply-workload", [iam]), "policy": json.marshal({"Statement": workload})})

read_rc(read) := res("aws_iam_policy", read_address, {"name": "tabp-admin-ci-apply-workload-read", "arn": sprintf("%s:policy/tabp-admin-ci-apply-workload-read", [iam]), "policy": json.marshal({"Statement": read})})

boundary_rc(boundary) := res("aws_iam_policy", boundary_address, {"name": "tabp-admin-ci-boundary-ops-notification", "arn": boundary_arn, "policy": json.marshal({"Statement": boundary})})

apply_rc(apply) := res("aws_iam_role_policy", apply_address, {"name": "apply", "role": "tabp-admin-ci-apply", "policy": json.marshal({"Statement": apply})})

world(workload, boundary, apply) := [workload_rc(workload), read_rc(valid_read), boundary_rc(boundary), apply_rc(apply)]

valid_boundary := array.concat(boundary_allow, bootstrap_denies)

valid_apply := array.concat(bootstrap_denies, apply_only)

valid_world := world(valid_workload, valid_boundary, valid_apply)

decl := {
	"apply_role": {"policy": apply_address},
	"workload": {
		"policy": workload_address,
		"read_policy": read_address,
		"boundaries": {"ops-notification": boundary_address},
		"usecase_iam_prefixes": ["ops-"],
		"service_linked_roles": ["management.chatbot.amazonaws.com"],
		"passed_to_services": ["chatbot.amazonaws.com", "scheduler.amazonaws.com"],
		"apply_only_denies": ["DenyBoundaryRemoval"],
	},
}

messages_for(rcs, d) := msgs if {
	msgs := p.deny with input as fixture.plan(rcs) with data.policy_plan as object.union(object.remove(data.policy_plan, ["deployment_identity"]), {"deployment_identity": d})
}

messages(workload) := messages_for(world(workload, valid_boundary, valid_apply), decl)

replace(xs, i, x) := array.concat(array.concat(array.slice(xs, 0, i), [x]), array.slice(xs, i + 1, count(xs)))

without(rcs, address) := [x | some x in rcs; x.address != address]

malformed := "[PLAN-AWP-00] data.policy_plan.deployment_identity.workload が無いか"

# --- 通る入力 ---

test_pass_valid if {
	count(messages_for(valid_world, decl)) == 0
}

# --- PLAN-AWP-00 ---

test_violation_declaration_missing if {
	fixture.has(malformed, messages_for(valid_world, {"apply_role": {"policy": apply_address}}))
}

test_violation_declaration_malformed if {
	every patch in [
		[{"op": "replace", "path": "/workload/usecase_iam_prefixes", "value": []}],
		[{"op": "replace", "path": "/workload/usecase_iam_prefixes", "value": ["Ops-"]}],
		[{"op": "replace", "path": "/workload/usecase_iam_prefixes", "value": ["ci-"]}],
		[{"op": "replace", "path": "/workload/usecase_iam_prefixes", "value": ["ops"]}],
		[{"op": "replace", "path": "/workload/usecase_iam_prefixes", "value": ["ops-X"]}],
		[{"op": "replace", "path": "/workload/usecase_iam_prefixes", "value": ["ops-/x"]}],
		[{"op": "replace", "path": "/workload/usecase_iam_prefixes", "value": ["-ops-"]}],
		[{"op": "replace", "path": "/workload/usecase_iam_prefixes", "value": ["-"]}],
		[{"op": "replace", "path": "/workload/usecase_iam_prefixes", "value": ["ops-\n"]}],
		[{"op": "replace", "path": "/workload/boundaries", "value": {}}],
		[{"op": "remove", "path": "/workload/apply_only_denies"}],
		[{"op": "replace", "path": "/workload/apply_only_denies", "value": [1]}],
		[{"op": "replace", "path": "/workload/service_linked_roles", "value": "x"}],
		[{"op": "replace", "path": "/workload/service_linked_roles", "value": [""]}],
		[{"op": "replace", "path": "/workload/passed_to_services", "value": []}],
		[{"op": "add", "path": "/workload/extra", "value": 1}],
		[{"op": "remove", "path": "/apply_role"}],
	] {
		fixture.has(malformed, messages_for(valid_world, json.patch(decl, patch)))
	}
}

test_violation_input_missing if {
	every address in [workload_address, read_address, boundary_address, apply_address] {
		fixture.has(sprintf("[PLAN-AWP-00] %s が入力にありません", [address]), messages_for(without(valid_world, address), decl))
	}
}

test_violation_policy_name if {
	bad := res("aws_iam_policy", workload_address, {"name": "tabp-admin-workload", "arn": sprintf("%s:policy/tabp-admin-workload", [iam]), "policy": json.marshal({"Statement": valid_workload})})
	fixture.has(sprintf("[PLAN-AWP-00] %s の名前", [workload_address]), messages_for(array.concat(without(valid_world, workload_address), [bad]), decl))
}

test_violation_read_policy_name if {
	bad := res("aws_iam_policy", read_address, {"name": "tabp-admin-ci-apply-workload-2", "arn": sprintf("%s:policy/tabp-admin-ci-apply-workload-2", [iam]), "policy": json.marshal({"Statement": valid_read})})
	fixture.has(sprintf("[PLAN-AWP-00] %s の名前", [read_address]), messages_for(array.concat(without(valid_world, read_address), [bad]), decl))
}

test_violation_iam_write_in_read_policy if {
	every c in [
		["PLAN-AWP-01", {"Effect": "Allow", "Action": "iam:CreateRole", "Resource": sprintf("%s:role/tabp-admin-*", [iam]), "Condition": pb}],
		["PLAN-AWP-02", {"Effect": "Allow", "Action": "iam:CreateRole", "Resource": [ops_roles]}],
		["PLAN-AWP-03", {"Effect": "Allow", "Action": "iam:PassRole", "Resource": [ops_roles]}],
		["PLAN-AWP-04", {"Effect": "Allow", "Action": "iam:AttachRolePolicy", "Resource": [ops_roles], "Condition": pb}],
	] {
		bad := read_rc(array.concat(valid_read, [c[1]]))
		fixture.has(sprintf("[%s] p.json: %s の Allow が", [c[0], read_address]), messages_for(array.concat(without(valid_world, read_address), [bad]), decl))
	}
}

test_violation_read_policy_unknown_arn if {
	bad := json.patch(read_rc(valid_read), [{"op": "remove", "path": "/change/after/arn"}])
	fixture.has(sprintf("[PLAN-AWP-00] %s の ARN が plan で決まっていません", [read_address]), messages_for(array.concat(without(valid_world, read_address), [bad]), decl))
}

test_violation_read_policy_same_as_policy if {
	d := json.patch(decl, [{"op": "replace", "path": "/workload/read_policy", "value": workload_address}])
	fixture.has(sprintf("[PLAN-AWP-00] %s の名前", [workload_address]), messages_for(valid_world, d))
}

test_violation_read_policy_malformed_declaration if {
	every v in ["", 1, [read_address], null, {}] {
		d := json.patch(decl, [{"op": "replace", "path": "/workload/read_policy", "value": v}])
		fixture.has("[PLAN-AWP-00] data.policy_plan.deployment_identity.workload が無いか", messages_for(valid_world, d))
	}
}

test_violation_read_policy_undeclared if {
	d := json.patch(decl, [{"op": "remove", "path": "/workload/read_policy"}])
	fixture.has("[PLAN-AWP-00] data.policy_plan.deployment_identity.workload が無いか", messages_for(valid_world, d))
}

test_violation_unknown_arns if {
	bad_workload := json.patch(workload_rc(valid_workload), [{"op": "remove", "path": "/change/after/arn"}])
	fixture.has(sprintf("[PLAN-AWP-00] %s の ARN が plan で決まっていないか", [workload_address]), messages_for(array.concat(without(valid_world, workload_address), [bad_workload]), decl))
	bad_boundary := json.patch(boundary_rc(valid_boundary), [{"op": "remove", "path": "/change/after/arn"}])
	fixture.has(sprintf("[PLAN-AWP-00] %s の ARN が plan で決まっていません", [boundary_address]), messages_for(array.concat(without(valid_world, boundary_address), [bad_boundary]), decl))
}

test_violation_boundary_instance_count if {
	extra := res("aws_iam_policy", sprintf("%saws_iam_policy.boundary[\"other\"]", [g]), {"name": "tabp-admin-ci-boundary-other", "arn": sprintf("%s:policy/tabp-admin-ci-boundary-other", [iam]), "policy": json.marshal({"Statement": valid_boundary})})
	fixture.has("[PLAN-AWP-00] plan の boundary の instance", messages_for(array.concat(valid_world, [extra]), decl))
}

# --- PLAN-AWP-01: IAM の書き込みの Resource ---

awp01(r) := sprintf("[PLAN-AWP-01] p.json: %s の Allow が iam:putrolepolicy を %s に許しています", [workload_address, r])

test_violation_iam_write_resource if {
	every r in [
		sprintf("%s:role/tabp-admin-*", [iam]),
		sprintf("%s:role/tabp-admin-ci-*", [iam]),
		sprintf("%s:role/tabp-admin-OPS-*", [iam]),
		sprintf("%s:role/tabp-admin-ops-Foo", [iam]),
		sprintf("%s:role/tabp-admin-ops-", [iam]),
		sprintf("%s:role/", [iam]),
		sprintf("%s:role/tabp-admin-ops-/x", [iam]),
		sprintf("%s:role/tabp-admin-ops-a/*", [iam]),
		"arn:aws:iam::210987654321:role/tabp-admin-ops-*",
		sprintf("%s:user/tabp-admin-ops-x", [iam]),
		sprintf("%s:role/*tabp-admin-ops-x", [iam]),
		sprintf("%s:role/x-tabp-admin-ops-x", [iam]),
		sprintf("%s:policy/*tabp-admin-ops-x", [iam]),
		sprintf("%s:policy/x-tabp-admin-ops-x", [iam]),
		sprintf("x%s", [ops_roles]),
		"*",
	] {
		fixture.has(awp01(r), messages(replace(valid_workload, 1, {"Effect": "Allow", "Action": "iam:PutRolePolicy", "Resource": r, "Condition": pb})))
	}
}

test_violation_iam_write_wildcard_action if {
	every a in ["iam:Create*", "*:CreateRole", "?am:CreateRole", "*:CreatePolicy", "*", "i*:*"] {
		s := {"Effect": "Allow", "Action": a, "Resource": sprintf("%s:role/tabp-admin-*", [iam])}
		fixture.has(sprintf("[PLAN-AWP-01] p.json: %s の Allow が %s を", [workload_address, lower(a)]), messages(array.concat(valid_workload, [s])))
	}
}

test_pass_iam_reads_outside_usecase if {
	s := {"Effect": "Allow", "Action": ["iam:GenerateCredentialReport", "iam:SimulatePrincipalPolicy", "iam:GetRole"], "Resource": sprintf("%s:role/other", [iam])}
	count(messages(array.concat(valid_workload, [s]))) == 0
}

# 名前の wildcard は書き込みに当たりうるので、読み取りの動詞で始まっても書き込みとする。
test_violation_iam_read_wildcard_outside_usecase if {
	s := {"Effect": "Allow", "Action": "iam:Get*", "Resource": sprintf("%s:role/other", [iam])}
	fixture.has(sprintf("[PLAN-AWP-01] p.json: %s の Allow が iam:get* を", [workload_address]), messages(array.concat(valid_workload, [s])))
}

test_pass_deny_statements_ignored if {
	s := {"Effect": "Deny", "Action": ["iam:PassRole", "iam:CreateRole", "iam:AttachRolePolicy"], "Resource": "*"}
	count(messages(array.concat(valid_workload, [s]))) == 0
}

test_violation_second_element if {
	other_role := sprintf("%s:role/tabp-admin-*", [iam])
	other_policy := sprintf("%s:policy/tabp-admin-ci-*", [iam])
	fixture.has(sprintf("[PLAN-AWP-01] p.json: %s の Allow が iam:putrolepolicy を %s に", [workload_address, other_role]), messages(replace(valid_workload, 1, {"Effect": "Allow", "Action": ["iam:CreateRole", "iam:PutRolePolicy"], "Resource": [ops_roles, other_role], "Condition": pb})))
	fixture.has(sprintf("[PLAN-AWP-02] p.json: %s の Allow が iam:createrole を", [workload_address]), messages(replace(valid_workload, 1, {"Effect": "Allow", "Action": ["iam:GetRole", "iam:CreateRole"], "Resource": [ops_roles]})))
	fixture.has(sprintf("[PLAN-AWP-03] p.json: %s の Allow が iam:PassRole", [workload_address]), messages(replace(valid_workload, 4, {"Effect": "Allow", "Action": ["iam:GetRole", "iam:PassRole"], "Resource": [ops_roles], "Condition": {"StringEquals": {"iam:PassedToService": ["scheduler.amazonaws.com", "ec2.amazonaws.com"]}}})))
	fixture.has(sprintf("[PLAN-AWP-04] p.json: %s の Allow が iam:AttachRolePolicy", [workload_address]), messages(replace(valid_workload, 2, {"Effect": "Allow", "Action": ["iam:CreateRole", "iam:AttachRolePolicy"], "Resource": [ops_roles], "Condition": object.union(pb, {"ArnLike": {"iam:PolicyARN": [ops_policies, other_policy]}})})))
}

test_pass_other_service_wildcard if {
	count(messages(array.concat(valid_workload, [{"Effect": "Allow", "Action": "s?s:CreateTopic", "Resource": "arn:aws:sns:ap-northeast-1:123456789012:tabp-admin-*"}]))) == 0
}

test_violation_service_linked_role_outside_declaration if {
	every s in [
		{"Effect": "Allow", "Action": "iam:CreateServiceLinkedRole", "Resource": slr},
		{"Effect": "Allow", "Action": "iam:CreateServiceLinkedRole", "Resource": slr, "Condition": {"StringEquals": {"iam:AWSServiceName": "other.amazonaws.com"}}},
		{"Effect": "Allow", "Action": "iam:CreateServiceLinkedRole", "Resource": sprintf("%s:role/aws-service-role/other.amazonaws.com/X", [iam]), "Condition": {"StringEquals": {"iam:AWSServiceName": "other.amazonaws.com"}}},
		{"Effect": "Allow", "Action": "iam:CreateServiceLinkedRole", "Resource": sprintf("%s/*", [slr]), "Condition": slr_condition},
		{"Effect": "Allow", "Action": "iam:CreateServiceLinkedRole", "Resource": sprintf("%s:role/aws-service-role/management.chatbot.amazonaws.com/*", [iam]), "Condition": slr_condition},
		{"Effect": "Allow", "Action": "iam:CreateServiceLinkedRole", "Resource": "arn:aws:iam::210987654321:role/aws-service-role/management.chatbot.amazonaws.com/AWSServiceRoleForAWSChatbot", "Condition": slr_condition},
		{"Effect": "Allow", "Action": "iam:PutRolePolicy", "Resource": slr, "Condition": slr_condition},
		{"Effect": "Allow", "Action": "iam:CreateServiceLinkedRole", "Resource": slr, "Condition": {"StringEquals": {"iam:AWSServiceName": ["management.chatbot.amazonaws.com", "other.amazonaws.com"]}}},
	] {
		fixture.has("[PLAN-AWP-01] ", messages(replace(valid_workload, 5, s)))
	}
}

# --- PLAN-AWP-02: role の書き込みの boundary の条件 ---

test_role_writes_pinned if {
	count(p.role_writes) == 10
}

test_pass_every_role_write_with_boundary if {
	s := {"Effect": "Allow", "Action": [a | some a in p.role_writes], "Resource": [ops_roles], "Condition": object.union(pb, {"ArnLike": {"iam:PolicyARN": [ops_policies]}})}
	count(messages(replace(valid_workload, 1, s))) == 0
}

test_violation_role_write_without_boundary if {
	every a in array.concat([a | some a in p.role_writes], ["iam:*", "iam:Create*"]) {
		every c in [
			{},
			{"StringEquals": {"iam:PermissionsBoundary": []}},
			{"StringEquals": {"iam:PermissionsBoundary": [sprintf("%s:policy/other", [iam])]}},
			{"StringEquals": {"iam:PermissionsBoundary": [boundary_arn, sprintf("%s:policy/other", [iam])]}},
			{"StringEqualsIfExists": {"iam:PermissionsBoundary": [boundary_arn]}},
			{"StringLike": {"iam:PermissionsBoundary": [boundary_arn]}},
		] {
			s := {"Effect": "Allow", "Action": a, "Resource": [ops_roles], "Condition": c}
			fixture.has(sprintf("[PLAN-AWP-02] p.json: %s の Allow が %s を", [workload_address, lower(a)]), messages(replace(valid_workload, 1, s)))
		}
	}
}

test_pass_boundary_as_string if {
	s := {"Effect": "Allow", "Action": "iam:CreateRole", "Resource": [ops_roles], "Condition": {"StringEquals": {"iam:PermissionsBoundary": boundary_arn}}}
	count(messages(replace(valid_workload, 1, s))) == 0
}

# --- PLAN-AWP-03: PassRole ---

test_violation_pass_role_service if {
	every c in [
		{},
		{"StringEquals": {"iam:PassedToService": []}},
		{"StringLike": {"iam:PassedToService": "*"}},
		{"StringEquals": {"iam:PassedToService": ["ec2.amazonaws.com"]}},
		{"StringEquals": {"iam:PassedToService": ["scheduler.amazonaws.com", "ec2.amazonaws.com"]}},
	] {
		s := {"Effect": "Allow", "Action": "iam:PassRole", "Resource": [ops_roles], "Condition": c}
		fixture.has(sprintf("[PLAN-AWP-03] p.json: %s の Allow が iam:PassRole", [workload_address]), messages(replace(valid_workload, 4, s)))
	}
}

test_violation_pass_role_wildcard_action if {
	every a in ["iam:Pass*", "iam:*", "*:PassRole"] {
		s := {"Effect": "Allow", "Action": a, "Resource": [ops_roles]}
		fixture.has(sprintf("[PLAN-AWP-03] p.json: %s の Allow が iam:PassRole", [workload_address]), messages(replace(valid_workload, 4, s)))
	}
}

test_pass_pass_role_declared_services if {
	s := {"Effect": "Allow", "Action": "iam:PassRole", "Resource": [ops_roles], "Condition": {"StringEquals": {"iam:PassedToService": ["chatbot.amazonaws.com", "scheduler.amazonaws.com"]}}}
	count(messages(replace(valid_workload, 4, s))) == 0
}

# --- PLAN-AWP-04: AttachRolePolicy の iam:PolicyARN ---

test_violation_attach_policy_arn if {
	every c in [
		pb,
		object.union(pb, {"ArnLike": {"iam:PolicyARN": []}}),
		object.union(pb, {"ArnLike": {"iam:PolicyARN": ["arn:aws:iam::aws:policy/*"]}}),
		object.union(pb, {"ArnLike": {"iam:PolicyARN": [sprintf("%s:policy/tabp-admin-ci-*", [iam])]}}),
		object.union(pb, {"ArnLike": {"iam:PolicyARN": [ops_roles]}}),
		object.union(pb, {"ArnEquals": {"iam:PolicyARN": [sprintf("%s:policy/tabp-admin-ci-apply-workload", [iam])]}}),
		object.union(pb, {"ArnNotLike": {"iam:PolicyARN": [sprintf("%s:policy/tabp-admin-ci-*", [iam])]}}),
	] {
		s := {"Effect": "Allow", "Action": "iam:AttachRolePolicy", "Resource": [ops_roles], "Condition": c}
		fixture.has(sprintf("[PLAN-AWP-04] p.json: %s の Allow が iam:AttachRolePolicy", [workload_address]), messages(replace(valid_workload, 2, s)))
	}
}

test_violation_attach_wildcard_action if {
	every a in ["iam:Attach*", "iam:*", "*:AttachRolePolicy"] {
		s := {"Effect": "Allow", "Action": a, "Resource": [ops_roles], "Condition": pb}
		fixture.has(sprintf("[PLAN-AWP-04] p.json: %s の Allow が iam:AttachRolePolicy", [workload_address]), messages(replace(valid_workload, 2, s)))
	}
}

test_pass_attach_policy_arn_equals if {
	s := {"Effect": "Allow", "Action": "iam:AttachRolePolicy", "Resource": [ops_roles], "Condition": object.union(pb, {"ArnEquals": {"iam:PolicyARN": [sprintf("%s:policy/tabp-admin-ops-chat-read", [iam])]}})}
	count(messages(replace(valid_workload, 2, s))) == 0
}

# --- PLAN-AWP-05: boundary の Deny と apply の role の Deny ---

awp05 := sprintf("[PLAN-AWP-05] p.json: %s の Deny が、apply の role %s の Deny", [boundary_address, apply_address])

test_violation_boundary_deny_missing if {
	fixture.has(awp05, messages_for(world(valid_workload, array.concat(boundary_allow, [bootstrap_denies[0]]), valid_apply), decl))
}

test_violation_boundary_deny_extra if {
	extra := {"Sid": "DenyOther", "Effect": "Deny", "Action": "*", "Resource": "arn:aws:s3:::x"}
	fixture.has(awp05, messages_for(world(valid_workload, array.concat(valid_boundary, [extra]), valid_apply), decl))
}

test_violation_boundary_deny_differs if {
	changed := json.patch(bootstrap_denies[1], [{"op": "replace", "path": "/Resource", "value": ["arn:aws:iam::123456789012:role/other"]}])
	fixture.has(awp05, messages_for(world(valid_workload, array.concat(boundary_allow, [bootstrap_denies[0], changed]), valid_apply), decl))
}

test_violation_apply_only_deny_in_boundary if {
	fixture.has(awp05, messages_for(world(valid_workload, array.concat(valid_boundary, apply_only), valid_apply), decl))
}

# boundary と apply の role の両方に Deny が無ければ一致してしまうので、apply の role の共有の Deny が無いことを落とす。
test_violation_no_shared_deny if {
	msgs := messages_for(world(valid_workload, boundary_allow, apply_only), decl)
	fixture.has(sprintf("[PLAN-AWP-05] apply の role の %s に、boundary と共有する Deny", [apply_address]), msgs)
}

test_violation_apply_only_deny_missing if {
	msgs := messages_for(world(valid_workload, valid_boundary, bootstrap_denies), decl)
	fixture.has(sprintf("[PLAN-AWP-05] apply の role の %s に、宣言した apply の role だけの Deny DenyBoundaryRemoval がありません", [apply_address]), msgs)
	fixture.has(sprintf("[PLAN-AWP-05] apply の role の %s が、iam:DeleteRolePermissionsBoundary を", [apply_address]), msgs)
}

test_violation_boundary_removal_deny_shape if {
	every s in [
		{"Sid": "DenyBoundaryRemoval", "Effect": "Deny", "Action": "iam:DeleteRolePermissionsBoundary", "Resource": ops_roles},
		{"Sid": "DenyBoundaryRemoval", "Effect": "Deny", "Action": "iam:DeleteRolePolicy", "Resource": "*"},
		{"Sid": "DenyBoundaryRemoval", "Effect": "Deny", "Action": "iam:DeleteRolePermissionsBoundary", "Resource": "*", "Condition": {"Bool": {"aws:SecureTransport": "true"}}},
		{"Sid": "DenyBoundaryRemoval", "Effect": "Deny", "Action": "iam:DeleteRolePermissionsBoundary", "NotResource": "arn:aws:iam::123456789012:role/x"},
	] {
		msgs := messages_for(world(valid_workload, valid_boundary, array.concat(bootstrap_denies, [s])), decl)
		fixture.has(sprintf("[PLAN-AWP-05] apply の role の %s が、iam:DeleteRolePermissionsBoundary を", [apply_address]), msgs)
	}
}

test_pass_boundary_removal_deny_forms if {
	s := {"Sid": "DenyBoundaryRemoval", "Effect": "Deny", "Action": ["IAM:DeleteRolePermissionsBoundary"], "Resource": ["*"]}
	count(messages_for(world(valid_workload, valid_boundary, array.concat(bootstrap_denies, [s])), decl)) == 0
}

package plan.state_backend_write_deny_test

import data.plan.fixture
import data.plan.state_backend_write_deny as p

decl := {
	"deployment_identity": {
		"plan_role": {"role": "module.d.aws_iam_role.this[\"plan\"]"},
		"apply_role": {"role": "module.d.aws_iam_role.this[\"apply\"]"},
		"apply_identity_role": {"role": "module.d.aws_iam_role.apply_identity"},
		"unlock_role": {"role": "module.d.aws_iam_role.this[\"unlock_identity\"]", "lock_key": "identity/terraform.tfstate.tflock"},
	},
	"state_backend": {
		"bucket": "module.s.aws_s3_bucket.this",
		"bucket_policy": "module.s.aws_s3_bucket_policy.this",
		"state_key": "module.k.aws_kms_key.this",
		"key_policy": "module.k.aws_kms_key_policy.this",
		"roots": [
			{"writer": "apply_identity_role", "state": "identity/terraform.tfstate"},
			{"writer": "apply_role", "state": "workload/terraform.tfstate"},
		],
		"unlocker": "unlock_role",
		"key_user_roles": ["plan_role", "apply_identity_role", "unlock_role"],
	},
}

b := "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an"

key := "arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"

plan_arn := "arn:aws:iam::123456789012:role/tabp-admin-ci-plan"

ai := "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity"

a := "arn:aws:iam::123456789012:role/tabp-admin-ci-apply"

u := "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock-identity"

identity_keys := [sprintf("%s/identity/terraform.tfstate", [b]), sprintf("%s/identity/terraform.tfstate.tflock", [b])]

workload_keys := [sprintf("%s/workload/terraform.tfstate", [b]), sprintf("%s/workload/terraform.tfstate.tflock", [b])]

lock_keys := [sprintf("%s/identity/terraform.tfstate.tflock", [b])]

apply_denied := ["s3:DeleteObjectVersion", "s3:ReplicateDelete", "s3:ReplicateObject"]

unlock_denied := ["s3:DeleteObjectVersion", "s3:PutObject", "s3:ReplicateDelete", "s3:ReplicateObject"]

writes := ["s3:DeleteObject", "s3:DeleteObjectVersion", "s3:PutObject", "s3:ReplicateDelete", "s3:ReplicateObject"]

change(address, type, after) := {"address": address, "type": type, "change": {"actions": ["create"], "after": after, "after_unknown": {}}}

tls := {"Sid": "DenyInsecureTransport", "Effect": "Deny", "Principal": "*", "Action": "s3:*", "Resource": [b, sprintf("%s/*", [b])], "Condition": {"Bool": {"aws:SecureTransport": "false"}}}

others(exceptions) := {"Sid": "DenyObjectWriteByOthers", "Effect": "Deny", "Principal": "*", "Action": writes, "Resource": sprintf("%s/*", [b]), "Condition": {"ArnNotEquals": {"aws:PrincipalArn": exceptions}}}

outside(arn, keys) := {"Sid": "DenyObjectWriteOutsideKeysX", "Effect": "Deny", "Principal": "*", "Action": writes, "NotResource": keys, "Condition": {"ArnEquals": {"aws:PrincipalArn": arn}}}

actions_of(arn, keys, actions) := {"Sid": "DenyObjectActionsOfX", "Effect": "Deny", "Principal": "*", "Action": actions, "Resource": keys, "Condition": {"ArnEquals": {"aws:PrincipalArn": arn}}}

other_than_kms := {"Sid": "E4a", "Effect": "Deny", "Principal": "*", "Action": "s3:PutObject", "Resource": sprintf("%s/*", [b]), "Condition": {
	"Null": {"s3:x-amz-server-side-encryption": "false"},
	"StringNotEquals": {"s3:x-amz-server-side-encryption": "aws:kms"},
}}

other_key(k) := {"Sid": "E4b", "Effect": "Deny", "Principal": "*", "Action": "s3:PutObject", "Resource": sprintf("%s/*", [b]), "Condition": {
	"Null": {"s3:x-amz-server-side-encryption-aws-kms-key-id": "false"},
	"StringNotEquals": {"s3:x-amz-server-side-encryption-aws-kms-key-id": k},
}}

managed_key := {"Sid": "E4c", "Effect": "Deny", "Principal": "*", "Action": "s3:PutObject", "Resource": sprintf("%s/*", [b]), "Condition": {
	"StringEquals": {"s3:x-amz-server-side-encryption": "aws:kms"},
	"Null": {"s3:x-amz-server-side-encryption-aws-kms-key-id": "true"},
}}

writer_statements := [
	outside(ai, identity_keys), actions_of(ai, identity_keys, apply_denied),
	outside(u, lock_keys), actions_of(u, lock_keys, unlock_denied),
	outside(a, workload_keys), actions_of(a, workload_keys, apply_denied),
]

valid_statements := array.concat(array.concat([tls, others([a, ai, u])], writer_statements), [other_than_kms, other_key(key), managed_key])

admin := {"Sid": "AccountKeyAdministration", "Effect": "Allow", "Principal": {"AWS": "arn:aws:iam::123456789012:root"}, "Action": ["kms:PutKeyPolicy"], "Resource": "*"}

read_write := {"Sid": "RolesViaS3ReadWrite", "Effect": "Allow", "Principal": {"AWS": [ai, plan_arn]}, "Action": ["kms:Decrypt", "kms:GenerateDataKey"], "Resource": "*"}

read_lock := {"Sid": "RolesViaS3Read0", "Effect": "Allow", "Principal": {"AWS": [u]}, "Action": "kms:Decrypt", "Resource": "*"}

valid_key_statements := [admin, read_write, read_lock]

roles := [
	change("module.d.aws_iam_role.this[\"plan\"]", "aws_iam_role", {"arn": plan_arn}),
	change("module.d.aws_iam_role.this[\"apply\"]", "aws_iam_role", {"arn": a}),
	change("module.d.aws_iam_role.apply_identity", "aws_iam_role", {"arn": ai}),
	change("module.d.aws_iam_role.this[\"unlock_identity\"]", "aws_iam_role", {"arn": u}),
]

input_with(statements, key_statements) := [
	{"path": "deployment-identity.json", "contents": {"plan_format_version": "1.2", "resource_changes": roles}},
	{"path": "state-backend.json", "contents": {"plan_format_version": "1.2", "resource_changes": [
		change("module.s.aws_s3_bucket.this", "aws_s3_bucket", {"arn": b}),
		change("module.k.aws_kms_key.this", "aws_kms_key", {"arn": key}),
		fixture.doc("aws_s3_bucket_policy", "module.s.aws_s3_bucket_policy.this", "policy", {"Statement": statements}),
		fixture.doc("aws_kms_key_policy", "module.k.aws_kms_key_policy.this", "policy", {"Statement": key_statements}),
	]}},
]

# 宣言は丸ごと差し替える。子のパスだけを with で差し替えると、規則が親から読むときに decl.json の値と重なる。
messages_for(inp, d) := msgs if {
	msgs := p.deny with input as inp with data.policy_plan as object.union(object.remove(data.policy_plan, ["deployment_identity", "state_backend"]), d)
}

messages_on(statements, key_statements, d) := messages_for(input_with(statements, key_statements), d)

messages_with(statements) := messages_on(statements, valid_key_statements, decl)

replace(statements, i, s) := array.concat(array.concat(array.slice(statements, 0, i), [s]), array.slice(statements, i + 1, count(statements)))

remove(statements, i) := array.concat(array.slice(statements, 0, i), array.slice(statements, i + 1, count(statements)))

test_pass_reference_shape if {
	count(messages_with(valid_statements)) == 0
}

test_pass_statement_order_independent if {
	count(messages_with(array.reverse(valid_statements))) == 0
}

test_violation_bucket_policy_missing if {
	msgs := messages_on([], valid_key_statements, object.union(decl, {"state_backend": object.union(decl.state_backend, {"bucket_policy": "module.s.aws_s3_bucket_policy.other"})}))
	fixture.has("[PLAN-SBW-00]", msgs)
}

test_violation_declaration_missing if {
	every name in ["bucket", "bucket_policy", "key_policy", "state_key"] {
		d := {"deployment_identity": decl.deployment_identity, "state_backend": object.remove(decl.state_backend, [name])}
		fixture.has(sprintf("[PLAN-SBW-00] data.policy_plan.state_backend.%s が宣言にありません", [name]), messages_on(valid_statements, valid_key_statements, d))
	}
}

test_violation_role_arn_unreadable if {
	msgs := messages_on(valid_statements, valid_key_statements, object.union(decl, {"deployment_identity": object.union(decl.deployment_identity, {"apply_role": {"role": "module.d.aws_iam_role.this[\"other\"]"}})}))
	fixture.has("[PLAN-SBW-00]", msgs)
}

test_violation_others_missing if {
	fixture.has("[PLAN-SBW-01]", messages_with(remove(valid_statements, 1)))
}

test_violation_others_includes_plan_role if {
	fixture.has("[PLAN-SBW-01]", messages_with(replace(valid_statements, 1, others([a, ai, plan_arn, u]))))
}

test_violation_others_misses_unlock_role if {
	fixture.has("[PLAN-SBW-01]", messages_with(replace(valid_statements, 1, others([a, ai]))))
}

test_violation_principal_arn_like if {
	s := json.patch(others([a, ai, u]), [{"op": "replace", "path": "/Condition", "value": {"ArnNotLike": {"aws:PrincipalArn": [a, ai, u]}}}])
	fixture.has("[PLAN-SBW-02]", messages_with(replace(valid_statements, 1, s)))
}

test_violation_principal_arn_if_exists if {
	s := json.patch(others([a, ai, u]), [{"op": "replace", "path": "/Condition", "value": {"ArnNotEqualsIfExists": {"aws:PrincipalArn": [a, ai, u]}}}])
	fixture.has("[PLAN-SBW-02]", messages_with(replace(valid_statements, 1, s)))
}

test_violation_principal_arn_wildcard if {
	fixture.has("[PLAN-SBW-02]", messages_with(replace(valid_statements, 1, others([a, ai, "arn:aws:iam::123456789012:role/tabp-admin-ci-*"]))))
}

test_violation_principal_arn_user if {
	fixture.has("[PLAN-SBW-02]", messages_with(replace(valid_statements, 1, others([a, ai, u, "arn:aws:iam::123456789012:user/tabp-bootstrap"]))))
}

test_violation_principal_arn_assumed_role if {
	fixture.has("[PLAN-SBW-02]", messages_with(replace(valid_statements, 1, others([a, ai, "arn:aws:sts::123456789012:assumed-role/tabp-admin-ci-unlock-identity/s"]))))
}

test_violation_write_deny_on_bucket if {
	s := json.patch(others([a, ai, u]), [{"op": "replace", "path": "/Resource", "value": [b, sprintf("%s/*", [b])]}])
	fixture.has("[PLAN-SBW-03]", messages_with(replace(valid_statements, 1, s)))
}

test_violation_encryption_deny_on_bucket if {
	s := json.patch(other_than_kms, [{"op": "replace", "path": "/Resource", "value": [b, sprintf("%s/*", [b])]}])
	fixture.has("[PLAN-SBW-03]", messages_with(replace(valid_statements, 8, s)))
}

test_violation_write_deny_bucket_configuration if {
	s := json.patch(others([a, ai, u]), [{"op": "replace", "path": "/Action", "value": ["s3:Put*", "s3:DeleteObject"]}])
	fixture.has("[PLAN-SBW-03]", messages_with(replace(valid_statements, 1, s)))
}

test_violation_write_deny_not_action if {
	s := json.patch(others([a, ai, u]), [{"op": "add", "path": "/NotAction", "value": "s3:GetObject"}])
	fixture.has("[PLAN-SBW-03]", messages_with(replace(valid_statements, 1, s)))
}

test_violation_unlock_role_may_put if {
	fixture.has("[PLAN-SBW-04]", messages_with(replace(valid_statements, 5, actions_of(u, lock_keys, apply_denied))))
}

test_violation_unlock_role_outside_lock if {
	fixture.has("[PLAN-SBW-04]", messages_with(replace(valid_statements, 4, outside(u, identity_keys))))
}

test_violation_workload_writer_is_apply_identity if {
	s := replace(replace(valid_statements, 6, outside(ai, workload_keys)), 7, actions_of(ai, workload_keys, apply_denied))
	fixture.has("[PLAN-SBW-04]", messages_with(s))
}

test_violation_apply_may_delete_versions if {
	fixture.has("[PLAN-SBW-04]", messages_with(replace(valid_statements, 7, actions_of(a, workload_keys, ["s3:PutObject", "s3:ReplicateDelete", "s3:ReplicateObject"]))))
}

test_violation_confines_undeclared_writer if {
	fixture.has("[PLAN-SBW-04]", messages_with(array.concat(valid_statements, [outside(plan_arn, workload_keys)])))
}

test_violation_roots_share_keys if {
	d := object.union(decl, {"state_backend": object.union(decl.state_backend, {"roots": [
		{"writer": "apply_identity_role", "state": "identity/terraform.tfstate"},
		{"writer": "apply_role", "state": "identity/terraform.tfstate"},
	]})})
	msgs := messages_on(valid_statements, valid_key_statements, d)
	fixture.has("[PLAN-SBW-05] root", msgs)
}

test_violation_unlock_role_writes_identity_state if {
	s := array.concat(valid_statements, [outside(u, array.concat(lock_keys, [sprintf("%s/identity/terraform.tfstate", [b])]))])
	fixture.has("[PLAN-SBW-05]", messages_with(s))
}

test_violation_key_policy_grants_undeclared_role if {
	extra := json.patch(read_write, [{"op": "replace", "path": "/Principal/AWS", "value": [ai, a, plan_arn]}])
	msgs := messages_on(valid_statements, [admin, extra, read_lock], decl)
	fixture.has("[PLAN-SBW-06]", msgs)
}

test_violation_key_policy_misses_declared_role if {
	msgs := messages_on(valid_statements, [admin, read_write], decl)
	fixture.has("[PLAN-SBW-06]", msgs)
}

test_violation_key_policy_wildcard_action_grants_undeclared_role if {
	extra := {"Sid": "X", "Effect": "Allow", "Principal": {"AWS": a}, "Action": "kms:*", "Resource": "*"}
	msgs := messages_on(valid_statements, array.concat(valid_key_statements, [extra]), decl)
	fixture.has("[PLAN-SBW-06]", msgs)
}

test_violation_key_policy_has_deny if {
	d := {"Sid": "X", "Effect": "Deny", "Principal": "*", "Action": "kms:Decrypt", "Resource": "*"}
	msgs := messages_on(valid_statements, array.concat(valid_key_statements, [d]), decl)
	fixture.has("[PLAN-SBW-06]", msgs)
}

test_violation_other_than_kms_missing if {
	fixture.has("[PLAN-SBW-07]", messages_with(remove(valid_statements, 8)))
}

test_violation_other_key_missing if {
	fixture.has("[PLAN-SBW-07]", messages_with(remove(valid_statements, 9)))
}

test_violation_other_key_value if {
	fixture.has("[PLAN-SBW-07]", messages_with(replace(valid_statements, 9, other_key("arn:aws:kms:ap-northeast-1:123456789012:key/00000000-0000-0000-0000-000000000000"))))
}

test_violation_managed_key_missing if {
	fixture.has("[PLAN-SBW-07]", messages_with(remove(valid_statements, 10)))
}

test_violation_null_boolean if {
	s := json.patch(managed_key, [{"op": "replace", "path": "/Condition/Null", "value": {"s3:x-amz-server-side-encryption-aws-kms-key-id": true}}])
	some m in messages_with(replace(valid_statements, 10, s))
	contains(m, "[PLAN-SBW-07] state-backend.json: Null の")
}

test_violation_tls_missing if {
	fixture.has("[PLAN-SBW-08]", messages_with(remove(valid_statements, 0)))
}

test_violation_tls_object_only if {
	s := json.patch(tls, [{"op": "replace", "path": "/Resource", "value": sprintf("%s/*", [b])}])
	fixture.has("[PLAN-SBW-08]", messages_with(replace(valid_statements, 0, s)))
}

test_violation_tls_allows_true if {
	s := json.patch(tls, [{"op": "replace", "path": "/Condition", "value": {"Bool": {"aws:SecureTransport": "true"}}}])
	fixture.has("[PLAN-SBW-08]", messages_with(replace(valid_statements, 0, s)))
}

test_violation_tls_partial_action if {
	s := json.patch(tls, [{"op": "replace", "path": "/Action", "value": "s3:GetObject"}])
	fixture.has("[PLAN-SBW-08]", messages_with(replace(valid_statements, 0, s)))
}

test_violation_others_misses_replication if {
	s := json.patch(others([a, ai, u]), [{"op": "replace", "path": "/Action", "value": ["s3:DeleteObject", "s3:DeleteObjectVersion", "s3:PutObject"]}])
	fixture.has("[PLAN-SBW-01]", messages_with(replace(valid_statements, 1, s)))
}

test_violation_confining_extra_condition_key if {
	s := json.patch(outside(a, workload_keys), [{"op": "add", "path": "/Condition/ArnEquals/aws:SourceArn", "value": "arn:aws:s3:::never"}])
	fixture.has("[PLAN-SBW-04]", messages_with(replace(valid_statements, 6, s)))
}

test_violation_confining_principal_narrowed if {
	s := json.patch(outside(ai, identity_keys), [{"op": "replace", "path": "/Principal", "value": {"AWS": plan_arn}}])
	fixture.has("[PLAN-SBW-04]", messages_with(replace(valid_statements, 2, s)))
}

test_violation_encryption_role_exception if {
	s := json.patch(other_than_kms, [{"op": "add", "path": "/Condition/ArnNotEquals", "value": {"aws:PrincipalArn": a}}])
	fixture.has("[PLAN-SBW-07]", messages_with(replace(valid_statements, 8, s)))
}

test_violation_other_key_extra_condition if {
	s := json.patch(other_key(key), [{"op": "add", "path": "/Condition/StringEquals", "value": {"s3:x-amz-acl": "public-read"}}])
	fixture.has("[PLAN-SBW-07]", messages_with(replace(valid_statements, 9, s)))
}

test_violation_managed_key_role_exception if {
	s := json.patch(managed_key, [{"op": "add", "path": "/Condition/ArnNotEquals", "value": {"aws:PrincipalArn": ai}}])
	fixture.has("[PLAN-SBW-07]", messages_with(replace(valid_statements, 10, s)))
}

test_violation_allow_statement if {
	allow := {"Sid": "X", "Effect": "Allow", "Principal": "*", "Action": "s3:GetObject", "Resource": sprintf("%s/*", [b])}
	fixture.has("[PLAN-SBW-09]", messages_with(array.concat(valid_statements, [allow])))
}

without_after(i) := json.patch(input_with(valid_statements, valid_key_statements), [{"op": "replace", "path": sprintf("/1/contents/resource_changes/%d/change/after", [i]), "value": {}}])

with_state_backend(patch) := {"deployment_identity": decl.deployment_identity, "state_backend": object.union(decl.state_backend, patch)}

test_violation_key_policy_missing if {
	fixture.has("[PLAN-SBW-00] state の鍵の Key Policy", messages_on(valid_statements, valid_key_statements, with_state_backend({"key_policy": "module.k.aws_kms_key_policy.other"})))
}

test_violation_bucket_arn_unreadable if {
	fixture.has("[PLAN-SBW-00] state Bucket \"module.s.aws_s3_bucket.this\" の ARN", messages_for(without_after(0), decl))
}

test_violation_state_key_arn_unreadable if {
	fixture.has("[PLAN-SBW-00] state の鍵 \"module.k.aws_kms_key.this\" の ARN", messages_for(without_after(1), decl))
}

test_violation_state_key_address_stale if {
	fixture.has("[PLAN-SBW-00] state の鍵 \"module.k.aws_kms_key.other\" の ARN", messages_on(valid_statements, valid_key_statements, with_state_backend({"state_key": "module.k.aws_kms_key.other"})))
}

test_violation_roots_empty if {
	fixture.has("[PLAN-SBW-00] root ごと", messages_on(valid_statements, valid_key_statements, with_state_backend({"roots": []})))
}

test_violation_unlocker_missing if {
	d := {"deployment_identity": decl.deployment_identity, "state_backend": object.remove(decl.state_backend, ["unlocker"])}
	fixture.has("[PLAN-SBW-00] lock を外す role（", messages_on(valid_statements, valid_key_statements, d))
}

test_violation_key_user_roles_empty if {
	fixture.has("[PLAN-SBW-00] state の鍵の暗号操作を許す role", messages_on(valid_statements, [admin], with_state_backend({"key_user_roles": []})))
}

test_violation_lock_key_empty if {
	d := {"deployment_identity": object.union(object.remove(decl.deployment_identity, ["unlock_role"]), {"unlock_role": {"role": decl.deployment_identity.unlock_role.role}}), "state_backend": decl.state_backend}
	fixture.has("[PLAN-SBW-00] lock を外す role が消せる lock file の key", messages_on(valid_statements, valid_key_statements, d))
}

test_violation_lock_key_not_a_root_lock if {
	other := [sprintf("%s/other/x.tflock", [b])]
	s := replace(replace(valid_statements, 4, outside(u, other)), 5, actions_of(u, other, unlock_denied))
	d := {"deployment_identity": object.union(decl.deployment_identity, {"unlock_role": {"role": decl.deployment_identity.unlock_role.role, "lock_key": "other/x.tflock"}}), "state_backend": decl.state_backend}
	fixture.has("[PLAN-SBW-05] lock を外す role が消せる key", messages_on(s, valid_key_statements, d))
}

test_violation_key_policy_service_principal if {
	svc := {"Sid": "X", "Effect": "Allow", "Principal": {"Service": "logs.ap-northeast-1.amazonaws.com"}, "Action": "kms:Decrypt", "Resource": "*"}
	fixture.has("[PLAN-SBW-06] state-backend.json", messages_on(valid_statements, array.concat(valid_key_statements, [svc]), decl))
}

test_violation_key_policy_not_action_grants_undeclared_role if {
	na := {"Sid": "X", "Effect": "Allow", "Principal": {"AWS": a}, "NotAction": "kms:PutKeyPolicy", "Resource": "*"}
	fixture.has("[PLAN-SBW-06] state の鍵の暗号操作を許す principal", messages_on(valid_statements, array.concat(valid_key_statements, [na]), decl))
}

test_violation_principal_arn_root if {
	fixture.has("[PLAN-SBW-02]", messages_with(replace(valid_statements, 1, others([a, ai, u, "arn:aws:iam::123456789012:root"]))))
}

test_violation_confining_not_resource_bucket if {
	s := json.patch(outside(a, workload_keys), [{"op": "replace", "path": "/NotResource", "value": array.concat(workload_keys, [b])}])
	fixture.has("[PLAN-SBW-03]", messages_with(replace(valid_statements, 6, s)))
}

# 段4 の実行者の例外を持つ文書は env の root にだけ現れ、Policy Test の入力には来ない。来たら落ちる。
test_violation_bootstrap_executor_shape if {
	x := "arn:aws:iam::123456789012:user/tabp-bootstrap"
	e4a := json.patch(other_than_kms, [{"op": "add", "path": "/Condition/ArnNotEquals", "value": {"aws:PrincipalArn": x}}])
	s := array.concat(replace(replace(valid_statements, 1, others([a, ai, u, x])), 8, e4a), [outside(x, identity_keys), actions_of(x, identity_keys, apply_denied)])
	msgs := messages_with(s)
	fixture.has("[PLAN-SBW-02]", msgs)
	fixture.has("[PLAN-SBW-07]", msgs)
}

test_pass_condition_case_insensitive if {
	s := json.patch(others([a, ai, u]), [
		{"op": "replace", "path": "/Condition", "value": {"arnnotequals": {"AWS:PRINCIPALARN": [a, ai, u]}}},
		{"op": "replace", "path": "/Action", "value": ["S3:DELETEOBJECT", "s3:deleteobjectversion", "s3:putobject", "s3:replicatedelete", "s3:replicateobject"]},
	])
	count(messages_with(replace(valid_statements, 1, s))) == 0
}

test_violation_writer_declared_twice if {
	fixture.has("[PLAN-SBW-00] root の writer と lock を外す role", messages_on(valid_statements, valid_key_statements, with_state_backend({"unlocker": "apply_identity_role"})))
}

test_violation_key_policy_mixed_principal if {
	mixed := {"Sid": "X", "Effect": "Allow", "Principal": {"AWS": ai, "Service": "logs.ap-northeast-1.amazonaws.com"}, "Action": "kms:Decrypt", "Resource": "*"}
	fixture.has("[PLAN-SBW-06] state-backend.json", messages_on(valid_statements, array.concat(valid_key_statements, [mixed]), decl))
}

test_violation_others_principal_narrowed if {
	s := json.patch(others([a, ai, u]), [{"op": "replace", "path": "/Principal", "value": {"AWS": plan_arn}}])
	fixture.has("[PLAN-SBW-01] state Bucket", messages_with(replace(valid_statements, 1, s)))
}

test_violation_tls_principal_narrowed if {
	s := json.patch(tls, [{"op": "replace", "path": "/Principal", "value": {"AWS": plan_arn}}])
	fixture.has("[PLAN-SBW-08]", messages_with(replace(valid_statements, 0, s)))
}

test_violation_encryption_principal_narrowed if {
	s := json.patch(other_than_kms, [{"op": "replace", "path": "/Principal", "value": {"AWS": plan_arn}}])
	fixture.has("[PLAN-SBW-07] state Bucket", messages_with(replace(valid_statements, 8, s)))
}

test_violation_other_key_value_includes_other_key if {
	fixture.has("[PLAN-SBW-07] state-backend.json: 別の鍵 ID", messages_with(replace(valid_statements, 9, other_key([key, "arn:aws:kms:ap-northeast-1:123456789012:key/00000000-0000-0000-0000-000000000000"]))))
}

test_violation_root_without_writer if {
	d := with_state_backend({"roots": [{"writer": "apply_identity_role", "state": "identity/terraform.tfstate"}, {"state": "workload/terraform.tfstate"}]})
	fixture.has("[PLAN-SBW-00] data.policy_plan.state_backend.roots の要素", messages_on(valid_statements, valid_key_statements, d))
}

test_violation_key_user_roles_not_array if {
	fixture.has("[PLAN-SBW-00] data.policy_plan.state_backend.key_user_roles が配列", messages_on(valid_statements, valid_key_statements, with_state_backend({"key_user_roles": "plan_role"})))
}

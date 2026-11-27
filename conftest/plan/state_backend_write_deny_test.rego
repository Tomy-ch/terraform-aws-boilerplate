package plan.state_backend_write_deny_test

import data.plan.fixture
import data.plan.state_backend_write_deny as p

decl := {
	"deployment_identity": {
		"module": "module.d.",
		"plan_role": {"role": "module.d.aws_iam_role.this[\"plan\"]", "policy": "module.d.aws_iam_role_policy.this[\"plan\"]"},
		"apply_role": {"role": "module.d.aws_iam_role.this[\"apply\"]", "policy": "module.d.aws_iam_role_policy.this[\"apply\"]", "bootstrap": {"modules": ["module.s.", "module.d."]}},
		"apply_identity_role": {"role": "module.d.aws_iam_role.apply_identity", "policy": "module.d.aws_iam_role_policy.apply_identity", "output_bucket": "module.s.module.plan_output.aws_s3_bucket.this"},
		"unlock_role": {"role": "module.d.aws_iam_role.this[\"unlock_identity\"]", "policy": "module.d.aws_iam_role_policy.this[\"unlock_identity\"]", "lock_key": "identity/terraform.tfstate.tflock"},
		"workload_unlock_role": {"role": "module.d.aws_iam_role.this[\"unlock\"]", "policy": "module.d.aws_iam_role_policy.this[\"unlock\"]", "lock_key": "workload/terraform.tfstate.tflock"},
	},
	"state_backend": {
		"module": "module.s.",
		"bucket": "module.s.module.s3.aws_s3_bucket.this",
		"bucket_policy": "module.s.module.s3.aws_s3_bucket_policy.this",
		"state_key": "module.s.module.kms.aws_kms_key.this",
		"key_policy": "module.s.module.kms.aws_kms_key_policy.this",
		"roots": [
			{"writer": "apply_identity_role", "state": "identity/terraform.tfstate", "unlocker": "unlock_role"},
			{"writer": "apply_role", "state": "workload/terraform.tfstate", "unlocker": "workload_unlock_role"},
		],
		"key_user_roles": ["plan_role", "apply_identity_role", "unlock_role", "apply_role", "workload_unlock_role"],
		"key_object_roles": ["plan_role", "apply_identity_role", "apply_role", "unlock_role", "workload_unlock_role"],
	},
}

b := "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an"

key := "arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"

plan_arn := "arn:aws:iam::123456789012:role/tabp-admin-ci-plan"

ai := "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity"

a := "arn:aws:iam::123456789012:role/tabp-admin-ci-apply"

u := "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock-identity"

wu := "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock"

identity_keys := [sprintf("%s/identity/terraform.tfstate", [b]), sprintf("%s/identity/terraform.tfstate.tflock", [b])]

workload_keys := [sprintf("%s/workload/terraform.tfstate", [b]), sprintf("%s/workload/terraform.tfstate.tflock", [b])]

lock_keys := [sprintf("%s/identity/terraform.tfstate.tflock", [b])]

workload_lock_keys := [sprintf("%s/workload/terraform.tfstate.tflock", [b])]

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

# 位置で差し替えるテストがあるので、後から足した書き手の文は末尾に置く。
valid_statements := array.concat(
	array.concat(array.concat([tls, others([a, ai, u, wu])], writer_statements), [other_than_kms, other_key(key), managed_key]),
	[outside(wu, workload_lock_keys), actions_of(wu, workload_lock_keys, unlock_denied)],
)

admin := {"Sid": "AccountKeyAdministration", "Effect": "Allow", "Principal": {"AWS": "arn:aws:iam::123456789012:root"}, "Action": ["kms:PutKeyPolicy"], "Resource": "*"}

pb := "arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an"

ab := "arn:aws:s3:::tabp-admin-tfapply-123456789012-ap-northeast-1-an"

state_keys := [sprintf("%s/identity/terraform.tfstate", [b]), sprintf("%s/workload/terraform.tfstate", [b])]

plan_writes := [sprintf("%s/branch/*", [pb]), sprintf("%s/pr/*", [pb])]

apply_identity_objects := array.concat(identity_keys, [sprintf("%s/apply/*", [pb])])

apply_objects := array.concat(workload_keys, [sprintf("%s/*", [ab])])

via_s3 := {"StringEquals": {"kms:ViaService": "s3.ap-northeast-1.amazonaws.com"}}

# modules/kms の RolesViaS3ReadWrite<n>・RolesViaS3Read<n> と同じ形の文。
read_write(arn, objects) := {"Sid": "RolesViaS3ReadWriteX", "Effect": "Allow", "Principal": {"AWS": [arn]}, "Action": ["kms:Decrypt", "kms:GenerateDataKey"], "Resource": "*", "Condition": object.union(via_s3, {"StringLike": {"kms:EncryptionContext:aws:s3:arn": objects}})}

read_only(arn, objects) := {"Sid": "RolesViaS3ReadX", "Effect": "Allow", "Principal": {"AWS": [arn]}, "Action": "kms:Decrypt", "Resource": "*", "Condition": {"StringEquals": {"kms:ViaService": "s3.ap-northeast-1.amazonaws.com", "kms:EncryptionContext:aws:s3:arn": objects}}}

plan_write := read_write(plan_arn, plan_writes)

plan_read := read_only(plan_arn, state_keys)

apply_identity_write := read_write(ai, apply_identity_objects)

apply_write := read_write(a, apply_objects)

read_lock := read_only(u, lock_keys)

read_workload_lock := read_only(wu, workload_lock_keys)

valid_key_statements := [admin, apply_write, apply_identity_write, plan_write, plan_read, read_lock, read_workload_lock]

# deployment-identity の inline policy の S3 の object の Allow と同じ形の文。
plan_policy := [
	{"Sid": "ListBuckets", "Effect": "Allow", "Action": "s3:ListBucket", "Resource": [b, pb, ab]},
	{"Sid": "ReadBucketConfiguration", "Effect": "Allow", "Action": ["s3:GetBucketPolicy", "s3:GetEncryptionConfiguration"], "Resource": [b, pb, ab]},
	{"Sid": "ReadState", "Effect": "Allow", "Action": "s3:GetObject", "Resource": state_keys},
	{"Sid": "WritePlanOutput", "Effect": "Allow", "Action": "s3:PutObject", "Resource": plan_writes},
	{"Sid": "ReadOrganization", "Effect": "Allow", "Action": "organizations:Describe*", "Resource": "*"},
]

apply_identity_policy := [
	{"Sid": "WriteIdentityState", "Effect": "Allow", "Action": ["s3:GetObject", "s3:PutObject"], "Resource": identity_keys[0]},
	{"Sid": "LockIdentityState", "Effect": "Allow", "Action": ["s3:DeleteObject", "s3:GetObject", "s3:PutObject"], "Resource": identity_keys[1]},
	{"Sid": "WriteApplyOutput", "Effect": "Allow", "Action": "s3:PutObject", "Resource": sprintf("%s/apply/*", [pb])},
]

apply_policy := [
	{"Sid": "WriteWorkloadState", "Effect": "Allow", "Action": ["s3:GetObject", "s3:PutObject"], "Resource": workload_keys[0]},
	{"Sid": "LockWorkloadState", "Effect": "Allow", "Action": ["s3:DeleteObject", "s3:GetObject", "s3:PutObject"], "Resource": workload_keys[1]},
	{"Sid": "WriteWorkloadApplyOutput", "Effect": "Allow", "Action": "s3:PutObject", "Resource": sprintf("%s/*", [ab])},
	{"Sid": "DenyIdentityStateChange", "Effect": "Deny", "Action": "*", "Resource": identity_keys},
]

unlock_policy(keys) := [
	{"Sid": "ListLock", "Effect": "Allow", "Action": "s3:ListBucket", "Resource": b},
	{"Sid": "RemoveLock", "Effect": "Allow", "Action": ["s3:DeleteObject", "s3:GetObject"], "Resource": keys},
]

role_policies := {
	"module.d.aws_iam_role_policy.this[\"plan\"]": plan_policy,
	"module.d.aws_iam_role_policy.apply_identity": apply_identity_policy,
	"module.d.aws_iam_role_policy.this[\"apply\"]": apply_policy,
	"module.d.aws_iam_role_policy.this[\"unlock_identity\"]": unlock_policy(lock_keys),
	"module.d.aws_iam_role_policy.this[\"unlock\"]": unlock_policy(workload_lock_keys),
}

roles := [
	change("module.d.aws_iam_role.this[\"plan\"]", "aws_iam_role", {"arn": plan_arn}),
	change("module.d.aws_iam_role.this[\"apply\"]", "aws_iam_role", {"arn": a}),
	change("module.d.aws_iam_role.apply_identity", "aws_iam_role", {"arn": ai}),
	change("module.d.aws_iam_role.this[\"unlock_identity\"]", "aws_iam_role", {"arn": u}),
	change("module.d.aws_iam_role.this[\"unlock\"]", "aws_iam_role", {"arn": wu}),
]

input_with(statements, key_statements) := input_on(statements, key_statements, role_policies)

input_on(statements, key_statements, policies) := [
	{"path": "deployment-identity.json", "contents": {"plan_format_version": "1.2", "resource_changes": array.concat(roles, [
		fixture.doc("aws_iam_role_policy", address, "policy", {"Statement": body}) |
			some address, body in policies
	])}},
	{"path": "state-backend.json", "contents": {"plan_format_version": "1.2", "resource_changes": [
		change("module.s.module.s3.aws_s3_bucket.this", "aws_s3_bucket", {"arn": b}),
		change("module.s.module.kms.aws_kms_key.this", "aws_kms_key", {"arn": key}),
		fixture.doc("aws_s3_bucket_policy", "module.s.module.s3.aws_s3_bucket_policy.this", "policy", {"Statement": statements}),
		fixture.doc("aws_kms_key_policy", "module.s.module.kms.aws_kms_key_policy.this", "policy", {"Statement": key_statements}),
	]}},
]

# 宣言は丸ごと差し替える（理由は lib/fixture.rego の inline_declaration）。
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
	msgs := messages_on([], valid_key_statements, object.union(decl, {"state_backend": object.union(decl.state_backend, {"bucket_policy": "module.s.module.s3.aws_s3_bucket_policy.other"})}))
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
	s := json.patch(others([a, ai, u, wu]), [{"op": "replace", "path": "/Condition", "value": {"ArnNotLike": {"aws:PrincipalArn": [a, ai, u, wu]}}}])
	fixture.has("[PLAN-SBW-02]", messages_with(replace(valid_statements, 1, s)))
}

test_violation_principal_arn_if_exists if {
	s := json.patch(others([a, ai, u, wu]), [{"op": "replace", "path": "/Condition", "value": {"ArnNotEqualsIfExists": {"aws:PrincipalArn": [a, ai, u, wu]}}}])
	fixture.has("[PLAN-SBW-02]", messages_with(replace(valid_statements, 1, s)))
}

test_violation_principal_arn_wildcard if {
	fixture.has("[PLAN-SBW-02]", messages_with(replace(valid_statements, 1, others([a, ai, "arn:aws:iam::123456789012:role/tabp-admin-ci-*"]))))
}

test_violation_principal_arn_user if {
	fixture.has("[PLAN-SBW-02]", messages_with(replace(valid_statements, 1, others([a, ai, u, wu, "arn:aws:iam::123456789012:user/tabp-bootstrap"]))))
}

test_violation_principal_arn_assumed_role if {
	fixture.has("[PLAN-SBW-02]", messages_with(replace(valid_statements, 1, others([a, ai, "arn:aws:sts::123456789012:assumed-role/tabp-admin-ci-unlock-identity/s"]))))
}

test_violation_write_deny_on_bucket if {
	s := json.patch(others([a, ai, u, wu]), [{"op": "replace", "path": "/Resource", "value": [b, sprintf("%s/*", [b])]}])
	fixture.has("[PLAN-SBW-03]", messages_with(replace(valid_statements, 1, s)))
}

test_violation_encryption_deny_on_bucket if {
	s := json.patch(other_than_kms, [{"op": "replace", "path": "/Resource", "value": [b, sprintf("%s/*", [b])]}])
	fixture.has("[PLAN-SBW-03]", messages_with(replace(valid_statements, 8, s)))
}

test_violation_write_deny_bucket_configuration if {
	s := json.patch(others([a, ai, u, wu]), [{"op": "replace", "path": "/Action", "value": ["s3:Put*", "s3:DeleteObject"]}])
	fixture.has("[PLAN-SBW-03]", messages_with(replace(valid_statements, 1, s)))
}

test_violation_write_deny_not_action if {
	s := json.patch(others([a, ai, u, wu]), [{"op": "add", "path": "/NotAction", "value": "s3:GetObject"}])
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
		{"writer": "apply_identity_role", "state": "identity/terraform.tfstate", "unlocker": "unlock_role"},
		{"writer": "apply_role", "state": "identity/terraform.tfstate", "unlocker": "workload_unlock_role"},
	]})})
	msgs := messages_on(valid_statements, valid_key_statements, d)
	fixture.has("[PLAN-SBW-05] root", msgs)
}

test_violation_unlock_role_writes_identity_state if {
	s := array.concat(valid_statements, [outside(u, array.concat(lock_keys, [sprintf("%s/identity/terraform.tfstate", [b])]))])
	fixture.has("[PLAN-SBW-05]", messages_with(s))
}

test_violation_key_policy_grants_undeclared_role if {
	extra := json.patch(plan_write, [{"op": "replace", "path": "/Principal/AWS", "value": ["arn:aws:iam::123456789012:role/other"]}])
	msgs := messages_on(valid_statements, array.concat(valid_key_statements, [extra]), decl)
	fixture.has("[PLAN-SBW-06]", msgs)
}

test_violation_key_policy_misses_declared_role if {
	msgs := messages_on(valid_statements, [admin, apply_write, apply_identity_write, plan_write, plan_read, read_lock], decl)
	fixture.has("[PLAN-SBW-06]", msgs)
}

test_violation_key_policy_wildcard_action_grants_undeclared_role if {
	extra := {"Sid": "X", "Effect": "Allow", "Principal": {"AWS": "arn:aws:iam::123456789012:role/other"}, "Action": "kms:*", "Resource": "*"}
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
	s := json.patch(others([a, ai, u, wu]), [{"op": "replace", "path": "/Action", "value": ["s3:DeleteObject", "s3:DeleteObjectVersion", "s3:PutObject"]}])
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

with_state_backend_removed(name) := {"deployment_identity": decl.deployment_identity, "state_backend": object.remove(decl.state_backend, [name])}

test_violation_key_policy_missing if {
	fixture.has("[PLAN-SBW-00] state の鍵の Key Policy", messages_on(valid_statements, valid_key_statements, with_state_backend({"key_policy": "module.s.module.kms.aws_kms_key_policy.other"})))
}

test_violation_bucket_arn_unreadable if {
	fixture.has("[PLAN-SBW-00] state Bucket \"module.s.module.s3.aws_s3_bucket.this\" の ARN", messages_for(without_after(0), decl))
}

test_violation_state_key_arn_unreadable if {
	fixture.has("[PLAN-SBW-00] state の鍵 \"module.s.module.kms.aws_kms_key.this\" の ARN", messages_for(without_after(1), decl))
}

test_violation_state_key_address_stale if {
	fixture.has("[PLAN-SBW-00] state の鍵 \"module.s.module.kms.aws_kms_key.other\" の ARN", messages_on(valid_statements, valid_key_statements, with_state_backend({"state_key": "module.s.module.kms.aws_kms_key.other"})))
}

test_violation_roots_empty if {
	fixture.has("[PLAN-SBW-00] root ごと", messages_on(valid_statements, valid_key_statements, with_state_backend({"roots": []})))
}

test_violation_unlocker_missing if {
	d := with_state_backend({"roots": [
		{"writer": "apply_identity_role", "state": "identity/terraform.tfstate", "unlocker": "unlock_role"},
		{"writer": "apply_role", "state": "workload/terraform.tfstate"},
	]})
	fixture.has("[PLAN-SBW-00] data.policy_plan.state_backend.roots の要素 {\"state\": \"workload/terraform.tfstate\", \"writer\": \"apply_role\"} が writer と state と unlocker の文字列を持っていません", messages_on(valid_statements, valid_key_statements, d))
}

test_violation_unlocker_empty if {
	d := with_state_backend({"roots": [
		{"writer": "apply_identity_role", "state": "identity/terraform.tfstate", "unlocker": "unlock_role"},
		{"writer": "apply_role", "state": "workload/terraform.tfstate", "unlocker": ""},
	]})
	fixture.has("[PLAN-SBW-00] data.policy_plan.state_backend.roots の要素", messages_on(valid_statements, valid_key_statements, d))
}

test_violation_workload_unlock_role_outside_lock if {
	fixture.has(sprintf("[PLAN-SBW-04] %s を、自分の key", [wu]), messages_with(replace(valid_statements, 11, outside(wu, workload_keys))))
}

test_violation_workload_unlock_role_may_put if {
	fixture.has(sprintf("[PLAN-SBW-04] %s の key", [wu]), messages_with(replace(valid_statements, 12, actions_of(wu, workload_lock_keys, apply_denied))))
}

test_violation_others_misses_workload_unlock_role if {
	fixture.has("[PLAN-SBW-01] state-backend.json: writer 以外の書き込みを拒否する文の例外", messages_with(replace(valid_statements, 1, others([a, ai, u]))))
}

test_violation_unlockers_swapped if {
	d := with_state_backend({"roots": [
		{"writer": "apply_identity_role", "state": "identity/terraform.tfstate", "unlocker": "workload_unlock_role"},
		{"writer": "apply_role", "state": "workload/terraform.tfstate", "unlocker": "unlock_role"},
	]})
	msgs := messages_on(valid_statements, valid_key_statements, d)
	fixture.has("[PLAN-SBW-05] lock を外す role workload_unlock_role が消せる key \"workload/terraform.tfstate.tflock\" が、その root の lock file \"identity/terraform.tfstate.tflock\" ではありません", msgs)
	fixture.has("[PLAN-SBW-05] lock を外す role unlock_role が消せる key \"identity/terraform.tfstate.tflock\" が、その root の lock file \"workload/terraform.tfstate.tflock\" ではありません", msgs)
}

test_violation_key_policy_misses_apply_role if {
	fixture.has("[PLAN-SBW-06] state の鍵の暗号操作を許す principal", messages_on(valid_statements, [admin, apply_identity_write, plan_write, plan_read, read_lock, read_workload_lock], decl))
}

test_violation_key_user_roles_empty if {
	fixture.has("[PLAN-SBW-00] state の鍵の暗号操作を許す role", messages_on(valid_statements, [admin], with_state_backend({"key_user_roles": []})))
}

test_violation_lock_key_empty if {
	d := {"deployment_identity": object.union(object.remove(decl.deployment_identity, ["unlock_role"]), {"unlock_role": {"role": decl.deployment_identity.unlock_role.role}}), "state_backend": decl.state_backend}
	fixture.has("[PLAN-SBW-00] lock を外す role が消せる lock file の key（data.policy_plan.deployment_identity.unlock_role.lock_key）", messages_on(valid_statements, valid_key_statements, d))
}

test_violation_lock_key_not_a_root_lock if {
	other := [sprintf("%s/other/x.tflock", [b])]
	s := replace(replace(valid_statements, 4, outside(u, other)), 5, actions_of(u, other, unlock_denied))
	d := {"deployment_identity": object.union(decl.deployment_identity, {"unlock_role": {"role": decl.deployment_identity.unlock_role.role, "lock_key": "other/x.tflock"}}), "state_backend": decl.state_backend}
	fixture.has("[PLAN-SBW-05] lock を外す role unlock_role が消せる key \"other/x.tflock\"", messages_on(s, valid_key_statements, d))
}

test_violation_key_policy_service_principal if {
	svc := {"Sid": "X", "Effect": "Allow", "Principal": {"Service": "logs.ap-northeast-1.amazonaws.com"}, "Action": "kms:Decrypt", "Resource": "*"}
	fixture.has("[PLAN-SBW-06] state-backend.json", messages_on(valid_statements, array.concat(valid_key_statements, [svc]), decl))
}

test_violation_key_policy_not_action_grants_undeclared_role if {
	na := {"Sid": "X", "Effect": "Allow", "Principal": {"AWS": "arn:aws:iam::123456789012:role/other"}, "NotAction": "kms:PutKeyPolicy", "Resource": "*"}
	fixture.has("[PLAN-SBW-06] state の鍵の暗号操作を許す principal", messages_on(valid_statements, array.concat(valid_key_statements, [na]), decl))
}

test_violation_principal_arn_root if {
	fixture.has("[PLAN-SBW-02]", messages_with(replace(valid_statements, 1, others([a, ai, u, wu, "arn:aws:iam::123456789012:root"]))))
}

test_violation_confining_not_resource_bucket if {
	s := json.patch(outside(a, workload_keys), [{"op": "replace", "path": "/NotResource", "value": array.concat(workload_keys, [b])}])
	fixture.has("[PLAN-SBW-03]", messages_with(replace(valid_statements, 6, s)))
}

# 段4 の実行者の例外を持つ文書は env の root にだけ現れ、Policy Test の入力には来ない。来たら落ちる。
test_violation_bootstrap_executor_shape if {
	x := "arn:aws:iam::123456789012:user/tabp-bootstrap"
	e4a := json.patch(other_than_kms, [{"op": "add", "path": "/Condition/ArnNotEquals", "value": {"aws:PrincipalArn": x}}])
	s := array.concat(replace(replace(valid_statements, 1, others([a, ai, u, wu, x])), 8, e4a), [outside(x, identity_keys), actions_of(x, identity_keys, apply_denied)])
	msgs := messages_with(s)
	fixture.has("[PLAN-SBW-02]", msgs)
	fixture.has("[PLAN-SBW-07]", msgs)
}

test_pass_condition_case_insensitive if {
	s := json.patch(others([a, ai, u, wu]), [
		{"op": "replace", "path": "/Condition", "value": {"arnnotequals": {"AWS:PRINCIPALARN": [a, ai, u, wu]}}},
		{"op": "replace", "path": "/Action", "value": ["S3:DELETEOBJECT", "s3:deleteobjectversion", "s3:putobject", "s3:replicatedelete", "s3:replicateobject"]},
	])
	count(messages_with(replace(valid_statements, 1, s))) == 0
}

test_violation_writer_declared_twice if {
	d := with_state_backend({"roots": [
		{"writer": "apply_identity_role", "state": "identity/terraform.tfstate", "unlocker": "apply_identity_role"},
		{"writer": "apply_role", "state": "workload/terraform.tfstate", "unlocker": "workload_unlock_role"},
	]})
	fixture.has("[PLAN-SBW-00] root の writer と lock を外す role", messages_on(valid_statements, valid_key_statements, d))
}

test_violation_key_policy_mixed_principal if {
	mixed := {"Sid": "X", "Effect": "Allow", "Principal": {"AWS": ai, "Service": "logs.ap-northeast-1.amazonaws.com"}, "Action": "kms:Decrypt", "Resource": "*"}
	fixture.has("[PLAN-SBW-06] state-backend.json", messages_on(valid_statements, array.concat(valid_key_statements, [mixed]), decl))
}

test_violation_others_principal_narrowed if {
	s := json.patch(others([a, ai, u, wu]), [{"op": "replace", "path": "/Principal", "value": {"AWS": plan_arn}}])
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
	d := with_state_backend({"roots": [{"writer": "apply_identity_role", "state": "identity/terraform.tfstate", "unlocker": "unlock_role"}, {"state": "workload/terraform.tfstate", "unlocker": "workload_unlock_role"}]})
	fixture.has("[PLAN-SBW-00] data.policy_plan.state_backend.roots の要素", messages_on(valid_statements, valid_key_statements, d))
}

test_violation_key_user_roles_not_array if {
	fixture.has("[PLAN-SBW-00] data.policy_plan.state_backend.key_user_roles が配列", messages_on(valid_statements, valid_key_statements, with_state_backend({"key_user_roles": "plan_role"})))
}

roots_with(rs) := with_state_backend({"roots": rs})

identity_root := {"writer": "apply_identity_role", "state": "identity/terraform.tfstate", "unlocker": "unlock_role"}

workload_root := {"writer": "apply_role", "state": "workload/terraform.tfstate", "unlocker": "workload_unlock_role"}

test_violation_roots_not_array if {
	fixture.has("[PLAN-SBW-00] data.policy_plan.state_backend.roots が配列ではありません", messages_on(valid_statements, valid_key_statements, roots_with(identity_root)))
}

test_violation_root_malformed_shapes if {
	every root in [
		"apply_role",
		object.union(workload_root, {"unlocker": 5}),
		object.remove(workload_root, ["state"]),
	] {
		fixture.has(sprintf("[PLAN-SBW-00] data.policy_plan.state_backend.roots の要素 %v が writer と state と unlocker の文字列を持っていません", [root]), messages_on(valid_statements, valid_key_statements, roots_with([identity_root, root])))
	}
}

test_violation_unlocker_undeclared_in_deployment_identity if {
	d := roots_with([identity_root, object.union(workload_root, {"unlocker": "nothing_role"})])
	d2 := json.patch(d, [{"op": "add", "path": "/state_backend/key_user_roles/-", "value": "nothing_role"}])
	msgs := messages_on(valid_statements, valid_key_statements, d2)
	fixture.has("[PLAN-SBW-00] role \"\"（data.policy_plan.deployment_identity.nothing_role.role）の ARN を入力から読めません", msgs)
	fixture.has("[PLAN-SBW-00] lock を外す role が消せる lock file の key（data.policy_plan.deployment_identity.nothing_role.lock_key）が宣言にありません", msgs)
}

test_violation_unlocker_shared_by_two_roots if {
	d := roots_with([identity_root, object.union(workload_root, {"unlocker": "unlock_role"})])
	fixture.has("[PLAN-SBW-00] root の writer と lock を外す role", messages_on(valid_statements, valid_key_statements, d))
}

test_violation_unlocker_not_a_key_user if {
	d := with_state_backend({"key_user_roles": ["plan_role", "apply_identity_role", "unlock_role", "apply_role"]})
	fixture.has("[PLAN-SBW-00] root の lock を外す role workload_unlock_role が data.policy_plan.state_backend.key_user_roles にありません", messages_on(valid_statements, valid_key_statements, d))
}

test_violation_key_user_role_undeclared if {
	d := with_state_backend({"key_user_roles": ["plan_role", "apply_identity_role", "unlock_role", "apply_role", "workload_unlock_role", "nothing_role"]})
	fixture.has("[PLAN-SBW-00] role \"\"（data.policy_plan.deployment_identity.nothing_role.role）の ARN を入力から読めません", messages_on(valid_statements, valid_key_statements, d))
}

test_violation_workload_lock_key_empty if {
	d := {"deployment_identity": json.patch(decl.deployment_identity, [{"op": "replace", "path": "/workload_unlock_role/lock_key", "value": ""}]), "state_backend": decl.state_backend}
	fixture.has("[PLAN-SBW-00] lock を外す role が消せる lock file の key（data.policy_plan.deployment_identity.workload_unlock_role.lock_key）が宣言にありません", messages_on(valid_statements, valid_key_statements, d))
}

test_violation_bucket_policy_document_unknown if {
	inp := json.patch(input_with(valid_statements, valid_key_statements), [
		{"op": "replace", "path": "/1/contents/resource_changes/2/change/after", "value": {}},
		{"op": "replace", "path": "/1/contents/resource_changes/2/change/after_unknown", "value": {"policy": true}},
	])
	fixture.has("[PLAN-SBW-00] state Bucket の Bucket Policy \"module.s.module.s3.aws_s3_bucket_policy.this\" が入力にありません", messages_for(inp, decl))
}

test_violation_bucket_policy_without_statement if {
	inp := json.patch(input_with(valid_statements, valid_key_statements), [{"op": "replace", "path": "/1/contents/resource_changes/2/change/after/policy", "value": json.marshal({"Version": "2012-10-17"})}])
	msgs := messages_for(inp, decl)
	fixture.has("[PLAN-SBW-01] state Bucket の Bucket Policy に", msgs)
	fixture.has("[PLAN-SBW-08] state Bucket の Bucket Policy に", msgs)
}

test_violation_key_policy_misses_roles_full_message if {
	msgs := messages_on(valid_statements, [admin, plan_write, plan_read, read_lock, read_workload_lock], decl)
	fixture.has(sprintf("[PLAN-SBW-06] state の鍵の暗号操作を許す principal %v が、宣言（data.policy_plan.state_backend.key_user_roles）の role %v と一致しません", [sort([plan_arn, u, wu]), sort([plan_arn, ai, u, a, wu])]), msgs)
}

sbw10(msgs) := {m | some m in msgs; startswith(m, "[PLAN-SBW-10]")}

key_with(i, st) := replace(valid_key_statements, i, st)

test_pass_key_objects_match_iam if {
	count(sbw10(messages_with(valid_statements))) == 0
}

# Key Policy が IAM より狭い場合も広い場合も落とす。
test_violation_key_object_missing if {
	every ks in [
		key_with(2, read_write(ai, [identity_keys[0], sprintf("%s/apply/*", [pb])])),
		key_with(4, read_only(plan_arn, [state_keys[0]])),
		key_with(3, read_write(plan_arn, [sprintf("%s/pr/*", [pb])])),
		key_with(1, read_write(a, workload_keys)),
		remove(valid_key_statements, 4),
	] {
		fixture.has("[PLAN-SBW-10]", messages_on(valid_statements, ks, decl))
	}
}

test_violation_key_object_extra if {
	every ks in [
		key_with(4, read_only(plan_arn, array.concat(state_keys, [sprintf("%s/apply/*", [pb])]))),
		key_with(1, read_write(a, array.concat(apply_objects, [identity_keys[0]]))),
		key_with(3, read_write(plan_arn, array.concat(plan_writes, [sprintf("%s/apply/*", [pb])]))),
		array.concat(valid_key_statements, [read_only(ai, [sprintf("%s/pr/x", [pb])])]),
	] {
		fixture.has("[PLAN-SBW-10]", messages_on(valid_statements, ks, decl))
	}
}

# 読むだけの state に鍵の生成を許すと、Decrypt の集合は一致しても GenerateDataKey の集合が IAM の PutObject とずれる。
test_violation_generate_data_key_on_read_only_object if {
	ks := key_with(4, json.patch(plan_read, [{"op": "replace", "path": "/Action", "value": ["kms:Decrypt", "kms:GenerateDataKey"]}]))
	msgs := sbw10(messages_on(valid_statements, ks, decl))
	fixture.has("[PLAN-SBW-10] plan_role", msgs)
	count({m | some m in msgs; contains(m, "kms:GenerateDataKey")}) > 0
}

test_violation_decrypt_dropped_from_write if {
	ks := key_with(3, json.patch(plan_write, [{"op": "replace", "path": "/Action", "value": "kms:GenerateDataKey"}]))
	fixture.has("[PLAN-SBW-10] plan_role", messages_on(valid_statements, ks, decl))
}

test_violation_iam_wildcard_action if {
	every extra in [
		{"Effect": "Allow", "Action": "s3:Get*", "Resource": sprintf("%s/x", [b])},
		{"Effect": "Allow", "Action": "S3:PUT*", "Resource": sprintf("%s/x", [b])},
		{"Effect": "Allow", "NotAction": "iam:*", "Resource": sprintf("%s/x", [b])},
		{"Effect": "Allow", "Action": "s3:*", "Resource": "*"},
	] {
		policies := object.union(role_policies, {"module.d.aws_iam_role_policy.this[\"plan\"]": array.concat(plan_policy, [extra])})
		fixture.has("[PLAN-SBW-10] plan_role", messages_for(input_on(valid_statements, valid_key_statements, policies), decl))
	}
}

test_pass_iam_not_action_excluding_s3_objects if {
	extra := {"Effect": "Allow", "NotAction": ["s3:GetObject", "s3:PutObject", "iam:*"], "Resource": "*"}
	policies := object.union(role_policies, {"module.d.aws_iam_role_policy.this[\"plan\"]": array.concat(plan_policy, [extra])})
	count(sbw10(messages_for(input_on(valid_statements, valid_key_statements, policies), decl))) == 0
}

test_violation_iam_not_resource if {
	extra := {"Effect": "Allow", "Action": "s3:GetObject", "NotResource": sprintf("%s/x", [b])}
	policies := object.union(role_policies, {"module.d.aws_iam_role_policy.this[\"plan\"]": array.concat(plan_policy, [extra])})
	fixture.has("[PLAN-SBW-10] deployment-identity.json: plan_role の IAM の policy", messages_for(input_on(valid_statements, valid_key_statements, policies), decl))
}

test_violation_key_grant_without_object if {
	unlimited := json.patch(plan_write, [{"op": "replace", "path": "/Condition", "value": via_s3}])
	fixture.has("[PLAN-SBW-10] state-backend.json: module.s.module.kms.aws_kms_key_policy.this が plan_role の role", messages_on(valid_statements, array.concat(valid_key_statements, [unlimited]), decl))
}

# 見るものが無いときに黙って通らない。
test_violation_key_object_roles_undeclared if {
	every d in [
		with_state_backend_removed("key_object_roles"),
		with_state_backend({"key_object_roles": []}),
		with_state_backend({"key_object_roles": "plan_role"}),
	] {
		fixture.has("[PLAN-SBW-00] data.policy_plan.state_backend.key_object_roles", messages_on(valid_statements, valid_key_statements, d))
	}
}

test_violation_key_object_role_not_key_user if {
	d := with_state_backend({"key_object_roles": ["plan_role", "apply_identity_role", "apply_role", "unlock_role", "workload_unlock_role", "other_role"]})
	fixture.has("[PLAN-SBW-00] data.policy_plan.state_backend.key_object_roles の other_role が key_user_roles にありません", messages_on(valid_statements, valid_key_statements, d))
}

test_violation_iam_policy_undeclared if {
	d := {"deployment_identity": object.union(object.remove(decl.deployment_identity, ["plan_role"]), {"plan_role": {"role": decl.deployment_identity.plan_role.role}}), "state_backend": decl.state_backend}
	fixture.has("[PLAN-SBW-00] data.policy_plan.deployment_identity.plan_role.policy が宣言にありません", messages_on(valid_statements, valid_key_statements, d))
}

test_violation_iam_policy_absent if {
	policies := object.remove(role_policies, ["module.d.aws_iam_role_policy.this[\"plan\"]"])
	fixture.has("[PLAN-SBW-00] plan_role の IAM の policy \"module.d.aws_iam_role_policy.this[\\\"plan\\\"]\" が入力にありません", messages_for(input_on(valid_statements, valid_key_statements, policies), decl))
}

test_violation_iam_policy_unknown if {
	inp := json.patch(input_with(valid_statements, valid_key_statements), [
		{"op": "replace", "path": sprintf("/0/contents/resource_changes/%d/change/after", [count(roles)]), "value": {}},
		{"op": "replace", "path": sprintf("/0/contents/resource_changes/%d/change/after_unknown", [count(roles)]), "value": {"policy": true}},
	])
	fixture.has("[PLAN-SBW-10] deployment-identity.json: ", messages_for(inp, decl))
}

test_violation_key_object_role_arn_unreadable if {
	d := {"deployment_identity": object.union(object.remove(decl.deployment_identity, ["apply_role"]), {"apply_role": {"role": "module.d.aws_iam_role.this[\"other\"]", "policy": decl.deployment_identity.apply_role.policy}}), "state_backend": decl.state_backend}
	fixture.has("[PLAN-SBW-00] role \"module.d.aws_iam_role.this[\\\"other\\\"]\"", messages_on(valid_statements, valid_key_statements, d))
}

with_plan_policy(statements) := object.union(role_policies, {"module.d.aws_iam_role_policy.this[\"plan\"]": statements})

# lock を外す role は、IAM の s3:GetObject の lock file と Key Policy の復号が一致し、鍵の生成はどちらにも無い。
test_violation_unlock_key_object_mismatch if {
	every c in [
		{"ks": replace(valid_key_statements, 5, read_only(u, array.concat(lock_keys, [identity_keys[0]]))), "role": "unlock_role", "action": "kms:Decrypt"},
		{"ks": replace(valid_key_statements, 6, read_write(wu, workload_lock_keys)), "role": "workload_unlock_role", "action": "kms:GenerateDataKey"},
	] {
		some m in messages_on(valid_statements, c.ks, decl)
		startswith(m, sprintf("[PLAN-SBW-10] %s の role", [c.role]))
		contains(m, sprintf("%s を許す object", [c.action]))
	}
}

test_violation_unlock_iam_put if {
	policies := object.union(role_policies, {"module.d.aws_iam_role_policy.this[\"unlock\"]": array.concat(unlock_policy(workload_lock_keys), [{"Effect": "Allow", "Action": "s3:PutObject", "Resource": workload_lock_keys}])})
	fixture.has("[PLAN-SBW-10] workload_unlock_role の role", messages_for(input_on(valid_statements, valid_key_statements, policies), decl))
}

# IAM は同じ条件キーの2つの演算子を AND で読む。和集合で読むと、どちらも満たさない文（lockout）を一致と見誤る。
test_violation_s3_arn_under_two_operators if {
	split := json.patch(plan_write, [{"op": "replace", "path": "/Condition", "value": {
		"StringEquals": {"kms:ViaService": "s3.ap-northeast-1.amazonaws.com", "kms:EncryptionContext:aws:s3:arn": [plan_writes[0]]},
		"StringLike": {"kms:EncryptionContext:aws:s3:arn": [plan_writes[1]]},
	}}])
	fixture.has("[PLAN-SBW-10] state-backend.json: module.s.module.kms.aws_kms_key_policy.this の plan_role の role への文が kms:EncryptionContext:aws:s3:arn を2つ以上の演算子", messages_on(valid_statements, replace(valid_key_statements, 3, split), decl))
}

test_violation_s3_arn_key_twice_in_one_operator if {
	twice := json.patch(plan_write, [{"op": "add", "path": "/Condition/StringLike/KMS:ENCRYPTIONCONTEXT:AWS:S3:ARN", "value": plan_writes}])
	fixture.has("[PLAN-SBW-10] state-backend.json: module.s.module.kms.aws_kms_key_policy.this の plan_role の role への文が kms:EncryptionContext:aws:s3:arn を2つ以上の演算子", messages_on(valid_statements, replace(valid_key_statements, 3, twice), decl))
}

# Key Policy も IAM も S3 の object を許さない role は、集合が一致しても何も突き合わせていない。
test_violation_key_object_role_without_any_object if {
	policies := with_plan_policy([s | some s in plan_policy; not s.Sid in {"ReadState", "WritePlanOutput"}])
	ks := [s | some s in valid_key_statements; not s in {plan_write, plan_read}]
	fixture.has("[PLAN-SBW-10] plan_role の role", messages_for(input_on(valid_statements, ks, policies), decl))
	count({m | some m in messages_for(input_on(valid_statements, ks, policies), decl); contains(m, "S3 の object を1つも")}) == 1
}

# Key Policy の側の wildcard と NotAction の grant も、許す Action として読む。
test_pass_key_grant_wildcard_and_not_action if {
	every st in [
		json.patch(plan_write, [{"op": "replace", "path": "/Action", "value": "kms:*"}]),
		json.patch(plan_write, [{"op": "remove", "path": "/Action"}, {"op": "add", "path": "/NotAction", "value": ["kms:Encrypt"]}]),
	] {
		count(sbw10(messages_on(valid_statements, replace(valid_key_statements, 3, st), decl))) == 0
	}
}

test_violation_key_grant_wildcard_and_not_action_extra_object if {
	extra := array.concat(plan_writes, [sprintf("%s/apply/*", [pb])])
	every st in [
		json.patch(read_write(plan_arn, extra), [{"op": "replace", "path": "/Action", "value": "kms:*"}]),
		json.patch(read_write(plan_arn, extra), [{"op": "remove", "path": "/Action"}, {"op": "add", "path": "/NotAction", "value": ["kms:Encrypt"]}]),
	] {
		fixture.has("[PLAN-SBW-10] plan_role の role", messages_on(valid_statements, replace(valid_key_statements, 3, st), decl))
	}
}

# Principal が "*" の Allow は、宣言の role の集合と一致しない。
test_violation_key_policy_star_principal if {
	every principal in ["*", {"AWS": "*"}, {"AWS": ["*"]}] {
		st := json.patch(plan_write, [{"op": "replace", "path": "/Principal", "value": principal}])
		fixture.has("[PLAN-SBW-06]", messages_on(valid_statements, array.concat(valid_key_statements, [st]), decl))
	}
}

# 鍵の暗号操作を許す role は、どれも S3 の object を IAM と突き合わせる。
test_violation_key_user_not_compared if {
	d := with_state_backend({"key_object_roles": ["plan_role", "apply_identity_role", "apply_role", "unlock_role"]})
	fixture.has("[PLAN-SBW-00] data.policy_plan.state_backend.key_user_roles の workload_unlock_role が key_object_roles にありません", messages_on(valid_statements, valid_key_statements, d))
}

test_declared_key_object_roles_cover_key_users if {
	cfg := data.policy_plan.state_backend
	{x | some x in cfg.key_object_roles} == {x | some x in cfg.key_user_roles}
}

# メンバーアカウントの instance（data.policy_plan.state_backend.instances）。宣言した root の名前のファイルでだけ数える。
# メンバーの値は、正規の instance の fixture の address・アカウント・名前を置き換えて作る。marker は置き換えない値の印。
member_map := {"module.s.": "module.sm.", "module.d.": "module.dm.", "123456789012": "111111111111", "tabp-admin-": "tabp-security-"}

member_of(x) := json.unmarshal(strings.replace_n(member_map, json.marshal(x)))

member_with(x, extra) := json.unmarshal(strings.replace_n(object.union(member_map, extra), json.marshal(x)))

security := {"name": "security", "state_backend": "module.sm.", "deployment_identity": "module.dm."}

creating_security := object.union(security, {"creating": {"reason": "メンバーアカウントの立ち上げの作成の plan"}})

with_instances(members) := with_state_backend({"instances": {"admin-identity": members}})

# 宣言の外の instance を見つける印（lib/instances.rego）。state-backend の印は plan の出力の Bucket で、deployment-identity の
# 印は apply-identity の role（メンバーの role の fixture に在る）。
markers := [change("module.sm.module.plan_output.aws_s3_bucket.this", "aws_s3_bucket", {"arn": "arn:aws:s3:::tabp-security-tfplan-111111111111-ap-northeast-1-an"})]

canonical_rcs(statements, key_statements) := array.concat(input_with(statements, key_statements)[0].contents.resource_changes, input_with(statements, key_statements)[1].contents.resource_changes)

admin_identity(rcs) := {"path": "input/admin-identity.json", "contents": {"plan_format_version": "1.2", "resource_changes": array.concat(rcs, markers)}}

with_member(rcs) := array.concat(input_with(valid_statements, valid_key_statements), [admin_identity(rcs)])

noop(rc) := json.patch(rc, [{"op": "replace", "path": "/change/actions", "value": ["no-op"]}])

member_noop(statements, key_statements) := [noop(rc) | some rc in member_of(canonical_rcs(statements, key_statements))]

# 作成の plan の形。role・Bucket・鍵の ARN と、鍵の ARN を持つ Bucket Policy と IAM の policy は決まらず、名前から組む Key Policy と
# lock を外す role の IAM の policy は決まる。
unknown_after(rc, attrs) := json.patch(rc, [
	{"op": "replace", "path": "/change/after", "value": {}},
	{"op": "replace", "path": "/change/after_unknown", "value": {a: true | some a in attrs}},
])

unlock_policy_addresses := {"module.dm.aws_iam_role_policy.this[\"unlock_identity\"]", "module.dm.aws_iam_role_policy.this[\"unlock\"]"}

creating_rc(rc) := json.patch(rc, [
	{"op": "replace", "path": "/change/after", "value": {"name": regex.replace(rc.change.after.arn, `^.*:role/`, ""), "path": "/"}},
	{"op": "replace", "path": "/change/after_unknown", "value": {"arn": true}},
]) if rc.type == "aws_iam_role"

# 名前を決めて作る Bucket は、bucket は決まり、arn が plan で決まらない。
creating_rc(rc) := json.patch(rc, [
	{"op": "replace", "path": "/change/after", "value": {"bucket": regex.replace(rc.change.after.arn, `^arn:[^:]+:s3:::`, "")}},
	{"op": "replace", "path": "/change/after_unknown", "value": {"arn": true}},
]) if rc.type == "aws_s3_bucket"

creating_rc(rc) := unknown_after(rc, ["arn"]) if rc.type == "aws_kms_key"

creating_rc(rc) := unknown_after(rc, ["policy"]) if rc.type == "aws_s3_bucket_policy"

creating_rc(rc) := unknown_after(rc, ["policy"]) if {
	rc.type == "aws_iam_role_policy"
	not rc.address in unlock_policy_addresses
}

creating_rc(rc) := rc if {
	rc.type == "aws_iam_role_policy"
	rc.address in unlock_policy_addresses
}

creating_rc(rc) := rc if rc.type == "aws_kms_key_policy"

member_creating(statements, key_statements) := [creating_rc(rc) | some rc in member_of(canonical_rcs(statements, key_statements))]

member_messages(rcs, members) := messages_for(with_member(rcs), with_instances(members))

test_pass_member_after_creation if {
	count(member_messages(member_noop(valid_statements, valid_key_statements), [security])) == 0
}

test_pass_member_creating if {
	count(member_messages(member_creating(valid_statements, valid_key_statements), [creating_security])) == 0
}

# 正規の instance の fixture は root の名前のファイルに無いので、メンバーの宣言は求めない。
test_pass_member_not_required_outside_its_root if {
	count(messages_for(input_with(valid_statements, valid_key_statements), with_instances([security]))) == 0
}

test_violation_member_bucket_policy_excepts_admin_role if {
	s := replace(valid_statements, 1, others(["ADMIN_APPLY", ai, u, wu]))
	rcs := [noop(rc) | some rc in member_with(canonical_rcs(s, valid_key_statements), {"ADMIN_APPLY": a})]
	msgs := member_messages(rcs, [security])
	fixture.has("[PLAN-SBW-01] admin-identity の instance security: input/admin-identity.json: writer 以外の書き込みを拒否する文の例外", msgs)
	not fixture.has("[PLAN-SBW-01] state-backend.json", msgs)
}

test_violation_member_declaration_absent_from_its_root if {
	msgs := member_messages([], [security])
	fixture.has("[PLAN-SBW-00] admin-identity の instance security: state の鍵の Key Policy \"module.sm.module.kms.aws_kms_key_policy.this\" が入力にありません", msgs)
	fixture.has("[PLAN-SBW-00] admin-identity の instance security: state Bucket の Bucket Policy \"module.sm.module.s3.aws_s3_bucket_policy.this\" が入力にありません", msgs)
	fixture.has("[PLAN-SBW-00] admin-identity の instance security: state Bucket \"module.sm.module.s3.aws_s3_bucket.this\" の ARN を入力から読めません。その instance の state の鍵を作る plan なら、data.policy_plan.state_backend.instances[\"admin-identity\"] の security に creating を宣言してください", msgs)
}

test_violation_member_declaration_only_in_another_file if {
	inp := array.concat(input_with(valid_statements, valid_key_statements), [
		admin_identity([]),
		{"path": "other.json", "contents": {"plan_format_version": "1.2", "resource_changes": member_noop(valid_statements, valid_key_statements)}},
	])
	fixture.has("[PLAN-SBW-00] admin-identity の instance security: state の鍵の Key Policy", messages_for(inp, with_instances([security])))
}

test_violation_undeclared_member if {
	msgs := messages_for(with_member(member_noop(valid_statements, valid_key_statements)), decl)
	fixture.has("[PLAN-SBW-00] input/admin-identity.json: state-backend の instance（prefix \"module.sm.\"）が宣言にありません。data.policy_plan.state_backend.instances[\"admin-identity\"] に足してください", msgs)
	fixture.has("[PLAN-SBW-00] input/admin-identity.json: deployment-identity の instance（prefix \"module.dm.\"）が宣言にありません。", msgs)
}

test_violation_instances_malformed if {
	every d in [
		with_state_backend({"instances": [security]}),
		with_state_backend({"instances": {}}),
		with_instances([]),
		with_instances([object.union(security, {"state_backend": "module.s."})]),
		with_instances([object.union(security, {"creating": {"reason": ""}})]),
	] {
		fixture.has("[PLAN-SBW-00] data.policy_plan.state_backend.instances", messages_for(with_member(member_noop(valid_statements, valid_key_statements)), d))
	}
}

test_violation_module_prefix_malformed if {
	d := {"deployment_identity": object.remove(decl.deployment_identity, ["module"]), "state_backend": decl.state_backend}
	fixture.has("[PLAN-SBW-00] data.policy_plan.deployment_identity.module が、module の address の接頭辞", messages_on(valid_statements, valid_key_statements, d))
}

test_violation_declared_address_outside_the_module if {
	msgs := messages_on(valid_statements, valid_key_statements, with_state_backend({"bucket": "module.x.aws_s3_bucket.this"}))
	fixture.has("[PLAN-SBW-00] data.policy_plan.state_backend.bucket \"module.x.aws_s3_bucket.this\" が state_backend.module か deployment_identity.module で始まりません", msgs)
	d := {"deployment_identity": json.patch(decl.deployment_identity, [{"op": "replace", "path": "/plan_role/policy", "value": "module.x.aws_iam_role_policy.this"}]), "state_backend": decl.state_backend}
	fixture.has("[PLAN-SBW-00] data.policy_plan.deployment_identity.plan_role.policy \"module.x.aws_iam_role_policy.this\" が state_backend.module か deployment_identity.module で始まりません", messages_on(valid_statements, valid_key_statements, d))
	d2 := {"deployment_identity": json.patch(decl.deployment_identity, [{"op": "replace", "path": "/plan_role/role", "value": "module.x.aws_iam_role.this"}]), "state_backend": decl.state_backend}
	fixture.has("[PLAN-SBW-00] data.policy_plan.deployment_identity.plan_role.role \"module.x.aws_iam_role.this\" が state_backend.module か deployment_identity.module で始まりません", messages_on(valid_statements, valid_key_statements, d2))
}

# creating は、その instance の state の鍵を作る plan でだけ効く。作成を apply した後に残すと、読める値を読まずに通す。
test_violation_creating_unused if {
	msgs := member_messages(member_noop(valid_statements, valid_key_statements), [creating_security])
	fixture.has("[PLAN-SBW-11] admin-identity の instance security: creating を宣言したのに、state の鍵 \"module.sm.module.kms.aws_kms_key.this\" を作る plan ではありません。作成を apply した後なら、data.policy_plan.state_backend.instances[\"admin-identity\"] の security から creating を外してください", msgs)
}

test_violation_creating_without_the_state_key if {
	fixture.has("[PLAN-SBW-11] admin-identity の instance security:", member_messages([], [creating_security]))
}

test_pass_creating_used if {
	msgs := member_messages(member_creating(valid_statements, valid_key_statements), [creating_security])
	not fixture.has("[PLAN-SBW-11]", msgs)
}

# 作成の扱いを宣言しない作成の plan は、読めない値を 00 で落とす。
test_violation_member_creating_without_declaration if {
	msgs := member_messages(member_creating(valid_statements, valid_key_statements), [security])
	fixture.has("[PLAN-SBW-00] admin-identity の instance security: state の鍵 \"module.sm.module.kms.aws_kms_key.this\" の ARN を入力から読めません。その instance の state の鍵を作る plan なら", msgs)
	fixture.has("[PLAN-SBW-00] admin-identity の instance security: role \"module.dm.aws_iam_role.this[\\\"plan\\\"]\"（data.policy_plan.deployment_identity.plan_role.role）の ARN を入力から読めません", msgs)
	fixture.has("[PLAN-SBW-10] admin-identity の instance security: input/admin-identity.json: plan_role の IAM の policy", msgs)
}

# 作成の plan でも、Key Policy の暗号操作の principal は role の名前と path から突き合わせる。
test_violation_member_creating_key_user_mismatch if {
	missing := member_messages(member_creating(valid_statements, remove(valid_key_statements, 6)), [creating_security])
	fixture.has("[PLAN-SBW-06] admin-identity の instance security: state の鍵の暗号操作を許す principal", missing)
	extra := json.patch(plan_write, [{"op": "replace", "path": "/Principal/AWS", "value": ["arn:aws:iam::111111111111:role/other"]}])
	undeclared := member_messages(member_creating(valid_statements, array.concat(valid_key_statements, [extra])), [creating_security])
	fixture.has("[PLAN-SBW-06] admin-identity の instance security: state の鍵の暗号操作を許す principal", undeclared)
}

test_violation_member_creating_role_path_differs if {
	rcs := [x |
		some rc in member_creating(valid_statements, valid_key_statements)
		x := object.union(rc, {"change": {"after": {"path": "/ci/"}}})
		rc.address == "module.dm.aws_iam_role.this[\"plan\"]"
	]
	others_rcs := [rc | some rc in member_creating(valid_statements, valid_key_statements); rc.address != "module.dm.aws_iam_role.this[\"plan\"]"]
	fixture.has("[PLAN-SBW-06] admin-identity の instance security:", member_messages(array.concat(others_rcs, rcs), [creating_security]))
}

test_violation_member_creating_role_name_unreadable if {
	rcs := [x |
		some rc in member_creating(valid_statements, valid_key_statements)
		x := unknown_after_if_plan_role(rc)
	]
	fixture.has("[PLAN-SBW-00] admin-identity の instance security: role \"module.dm.aws_iam_role.this[\\\"plan\\\"]\"（data.policy_plan.deployment_identity.plan_role.role）の名前を入力から読めません", member_messages(rcs, [creating_security]))
}

unknown_after_if_plan_role(rc) := unknown_after(rc, ["arn", "name"]) if rc.address == "module.dm.aws_iam_role.this[\"plan\"]"

unknown_after_if_plan_role(rc) := rc if rc.address != "module.dm.aws_iam_role.this[\"plan\"]"

# 作成の plan でも、決まる IAM の policy（lock を外す role）は Key Policy と突き合わせる。
test_violation_member_creating_unlock_key_object_mismatch if {
	ks := replace(valid_key_statements, 5, read_only(u, array.concat(lock_keys, [identity_keys[0]])))
	fixture.has("[PLAN-SBW-10] admin-identity の instance security: unlock_role の role", member_messages(member_creating(valid_statements, ks), [creating_security]))
}

test_violation_member_creating_key_policy_unreadable if {
	rcs := [x |
		some rc in member_creating(valid_statements, valid_key_statements)
		x := unknown_after_if_key_policy(rc)
	]
	fixture.has("[PLAN-SBW-00] admin-identity の instance security: state の鍵の Key Policy", member_messages(rcs, [creating_security]))
}

unknown_after_if_key_policy(rc) := unknown_after(rc, ["policy"]) if rc.type == "aws_kms_key_policy"

unknown_after_if_key_policy(rc) := rc if rc.type != "aws_kms_key_policy"

test_violation_member_creating_bucket_policy_absent if {
	rcs := [rc | some rc in member_creating(valid_statements, valid_key_statements); rc.type != "aws_s3_bucket_policy"]
	fixture.has("[PLAN-SBW-00] admin-identity の instance security: state Bucket の Bucket Policy \"module.sm.module.s3.aws_s3_bucket_policy.this\" が入力にありません", member_messages(rcs, [creating_security]))
}

member_creating_with(statements, key_statements, extra) := [creating_rc(rc) | some rc in member_with(canonical_rcs(statements, key_statements), extra)]

# 作成の plan の role の ARN は、Key Policy の委任のアカウントから組む。名前が同じでも別のアカウントの role は宣言の role ではない。
test_violation_member_creating_other_account_role if {
	ks := replace(valid_key_statements, 3, json.patch(plan_write, [{"op": "replace", "path": "/Principal/AWS", "value": ["OTHER_PLAN"]}]))
	rcs := member_creating_with(valid_statements, ks, {"OTHER_PLAN": "arn:aws:iam::222222222222:role/tabp-security-ci-plan"})
	msgs := member_messages(rcs, [creating_security])
	fixture.has(sprintf("[PLAN-SBW-06] admin-identity の instance security: state の鍵の暗号操作を許す principal %v", [sort(["arn:aws:iam::111111111111:role/tabp-security-ci-apply", "arn:aws:iam::111111111111:role/tabp-security-ci-apply-identity", "arn:aws:iam::111111111111:role/tabp-security-ci-plan", "arn:aws:iam::111111111111:role/tabp-security-ci-unlock", "arn:aws:iam::111111111111:role/tabp-security-ci-unlock-identity", "arn:aws:iam::222222222222:role/tabp-security-ci-plan"])]), msgs)
}

test_violation_member_creating_all_roles_in_other_account if {
	rcs := member_creating_with(valid_statements, valid_key_statements, {"arn:aws:iam::123456789012:role/": "arn:aws:iam::222222222222:role/"})
	fixture.has("[PLAN-SBW-06] admin-identity の instance security: state の鍵の暗号操作を許す principal", member_messages(rcs, [creating_security]))
}

account_undetermined(ks) := msgs if {
	msgs := member_messages(member_creating(valid_statements, ks), [creating_security])
}

account_undetermined_message := "[PLAN-SBW-00] admin-identity の instance security: 作成の plan の role の ARN を組むアカウントを、state の鍵の Key Policy \"module.sm.module.kms.aws_kms_key_policy.this\" のアカウントへの委任（arn:<partition>:iam::<account>:root）から1つに決められません"

test_violation_member_creating_account_without_root if {
	msgs := account_undetermined(remove(valid_key_statements, 0))
	fixture.has(account_undetermined_message, msgs)
	fixture.has("[PLAN-SBW-06] admin-identity の instance security:", msgs)
}

test_violation_member_creating_account_with_two_roots if {
	other_root := json.patch(admin, [{"op": "replace", "path": "/Principal/AWS", "value": "arn:aws:iam::222222222222:root"}])
	msgs := account_undetermined(array.concat(valid_key_statements, [other_root]))
	fixture.has(account_undetermined_message, msgs)
	fixture.has("[PLAN-SBW-06] admin-identity の instance security:", msgs)
}

# 作成の plan でも、読める Bucket Policy は読み、決まらない値を要らない判定は見る。
creating_readable_rc(rc) := rc if rc.type == "aws_s3_bucket_policy"

creating_readable_rc(rc) := creating_rc(rc) if rc.type != "aws_s3_bucket_policy"

member_creating_readable(statements) := [creating_readable_rc(rc) | some rc in member_of(canonical_rcs(statements, valid_key_statements))]

test_pass_member_creating_readable_bucket_policy if {
	count(member_messages(member_creating_readable(valid_statements), [creating_security])) == 0
}

member_allow := {"Sid": "X", "Effect": "Allow", "Principal": "*", "Action": "s3:GetObject", "Resource": sprintf("%s/*", [b])}

arn_not_like := replace(valid_statements, 1, json.patch(others([a, ai, u, wu]), [{"op": "replace", "path": "/Condition", "value": {"ArnNotLike": {"aws:PrincipalArn": [a, ai, u, wu]}}}]))

creating_readable_has(id, statements) if {
	msgs := member_messages(member_creating_readable(statements), [creating_security])
	fixture.has(sprintf("%s admin-identity の instance security: input/admin-identity.json:", [id]), msgs)
}

test_violation_member_creating_readable_bucket_policy_allow if {
	creating_readable_has("[PLAN-SBW-09]", array.concat(valid_statements, [member_allow]))
}

test_violation_member_creating_readable_bucket_policy_principal_arn_like if {
	creating_readable_has("[PLAN-SBW-02]", arn_not_like)
}

test_violation_member_creating_readable_bucket_policy_not_action if {
	creating_readable_has("[PLAN-SBW-03]", replace(valid_statements, 1, json.patch(others([a, ai, u, wu]), [{"op": "add", "path": "/NotAction", "value": "s3:GetObject"}])))
}

test_violation_member_creating_readable_bucket_policy_null_boolean if {
	creating_readable_has("[PLAN-SBW-07]", replace(valid_statements, 10, json.patch(managed_key, [{"op": "replace", "path": "/Condition/Null", "value": {"s3:x-amz-server-side-encryption-aws-kms-key-id": true}}])))
}

# 正規の instance の違反の fixture をメンバーへ写しても、メンバーの instance の違反として落ちる。
member_rule_has(id, statements) if {
	msgs := member_messages(member_noop(statements, valid_key_statements), [security])
	fixture.has(sprintf("%s admin-identity の instance security: ", [id]), msgs)
	not fixture.has(sprintf("%s state-backend.json", [id]), msgs)
}

test_violation_member_principal_arn_like if {
	member_rule_has("[PLAN-SBW-02]", arn_not_like)
}

test_violation_member_write_deny_on_bucket if {
	member_rule_has("[PLAN-SBW-03]", replace(valid_statements, 1, json.patch(others([a, ai, u, wu]), [{"op": "replace", "path": "/Resource", "value": [b, sprintf("%s/*", [b])]}])))
}

test_violation_member_unlock_role_may_put if {
	member_rule_has("[PLAN-SBW-04]", replace(valid_statements, 5, actions_of(u, lock_keys, apply_denied)))
}

test_violation_member_unlock_role_writes_identity_state if {
	member_rule_has("[PLAN-SBW-05]", array.concat(valid_statements, [outside(u, array.concat(lock_keys, [sprintf("%s/identity/terraform.tfstate", [b])]))]))
}

test_violation_member_other_than_kms_missing if {
	member_rule_has("[PLAN-SBW-07]", remove(valid_statements, 8))
}

test_violation_member_tls_missing if {
	member_rule_has("[PLAN-SBW-08]", remove(valid_statements, 0))
}

test_violation_member_allow_statement if {
	member_rule_has("[PLAN-SBW-09]", array.concat(valid_statements, [member_allow]))
}

# creating の作成は、その instance の root のファイルの、その instance の state の鍵の作成だけで読む。
test_violation_creating_with_the_state_key_created_elsewhere if {
	member_key_created := [rc | some rc in member_creating(valid_statements, valid_key_statements); rc.type == "aws_kms_key"]
	inp := array.concat(with_member(member_noop(valid_statements, valid_key_statements)), [{"path": "other.json", "contents": {"plan_format_version": "1.2", "resource_changes": member_key_created}}])
	fixture.has("[PLAN-SBW-11] admin-identity の instance security:", messages_for(inp, with_instances([creating_security])))
}

test_violation_creating_with_the_canonical_key_created if {
	canonical_key := [rc | some rc in canonical_rcs(valid_statements, valid_key_statements); rc.type == "aws_kms_key"]
	fixture.has("[PLAN-SBW-11] admin-identity の instance security:", member_messages(array.concat(member_noop(valid_statements, valid_key_statements), canonical_key), [creating_security]))
}

# 立ち上げを閉じる PR は、メンバーの instance を removed（destroy = false）で管理から外す。書かない変更は instance を残さない。
forget(rc) := json.patch(rc, [
	{"op": "replace", "path": "/change/actions", "value": ["forget"]},
	{"op": "add", "path": "/change/before", "value": rc.change.after},
	{"op": "replace", "path": "/change/after", "value": null},
])

test_pass_removed_member_forgotten if {
	rcs := [forget(rc) | some rc in array.concat(member_of(canonical_rcs(valid_statements, valid_key_statements)), markers)]
	inp := array.concat(input_with(valid_statements, valid_key_statements), [{"path": "input/admin-identity.json", "contents": {"plan_format_version": "1.2", "resource_changes": rcs}}])
	count(messages_for(inp, decl)) == 0
}

test_violation_removed_member_with_a_writing_marker if {
	rcs := array.concat([forget(rc) | some rc in member_of(canonical_rcs(valid_statements, valid_key_statements))], markers)
	inp := array.concat(input_with(valid_statements, valid_key_statements), [{"path": "input/admin-identity.json", "contents": {"plan_format_version": "1.2", "resource_changes": rcs}}])
	fixture.has("[PLAN-SBW-00] input/admin-identity.json: state-backend の instance（prefix \"module.sm.\"）が宣言にありません", messages_for(inp, decl))
}

# 作成の plan の委任のアカウントは、state Bucket の名前（account regional namespace）のアカウントと突き合わせる。Key Policy の文が
# すべて別のアカウントで書かれていても、宣言の role と一致したことにしない。
key_policy_in_account(rc, account) := json.patch(rc, [{"op": "replace", "path": "/change/after/policy", "value": strings.replace_n({"111111111111": account}, rc.change.after.policy)}]) if rc.type == "aws_kms_key_policy"

key_policy_in_account(rc, _) := rc if rc.type != "aws_kms_key_policy"

test_violation_member_creating_key_policy_all_in_other_account if {
	rcs := [key_policy_in_account(rc, "222222222222") | some rc in member_creating(valid_statements, valid_key_statements)]
	msgs := member_messages(rcs, [creating_security])
	fixture.has("[PLAN-SBW-00] admin-identity の instance security: 作成の plan の state の鍵の Key Policy \"module.sm.module.kms.aws_kms_key_policy.this\" の委任のアカウント 222222222222 が、state Bucket \"module.sm.module.s3.aws_s3_bucket.this\" の名前のアカウント 111111111111 と違います", msgs)
	fixture.has("[PLAN-SBW-06] admin-identity の instance security:", msgs)
}

bucket_without_name(rc) := unknown_after(rc, ["arn", "bucket"]) if rc.type == "aws_s3_bucket"

bucket_without_name(rc) := rc if rc.type != "aws_s3_bucket"

test_violation_member_creating_bucket_name_unreadable if {
	rcs := [bucket_without_name(rc) | some rc in member_creating(valid_statements, valid_key_statements)]
	msgs := member_messages(rcs, [creating_security])
	fixture.has("[PLAN-SBW-00] admin-identity の instance security: 作成の plan の state Bucket \"module.sm.module.s3.aws_s3_bucket.this\" の名前から、account regional namespace のアカウント（<名前>-<account>-<region>-an）を読めません", msgs)
	fixture.has("[PLAN-SBW-06] admin-identity の instance security:", msgs)
}

# 作成の plan で名前の決まらない書き手が居ても、ほかの書き手の 04 は見る。
unlock_name_unknown(rc) := unknown_after(rc, ["arn", "name"]) if rc.address == "module.dm.aws_iam_role.this[\"unlock_identity\"]"

unlock_name_unknown(rc) := rc if rc.address != "module.dm.aws_iam_role.this[\"unlock_identity\"]"

test_violation_member_creating_known_writer_confinement if {
	s := replace(valid_statements, 7, actions_of(a, workload_keys, ["s3:PutObject", "s3:ReplicateDelete", "s3:ReplicateObject"]))
	rcs := [unlock_name_unknown(rc) | some rc in member_creating_readable(s)]
	msgs := member_messages(rcs, [creating_security])
	fixture.has("[PLAN-SBW-04] admin-identity の instance security: arn:aws:iam::111111111111:role/tabp-security-ci-apply の key", msgs)
	fixture.has("[PLAN-SBW-00] admin-identity の instance security: role \"module.dm.aws_iam_role.this[\\\"unlock_identity\\\"]\"（data.policy_plan.deployment_identity.unlock_role.role）の名前を入力から読めません", msgs)
}

test_violation_member_creating_readable_bucket_policy_writer_confinement if {
	creating_readable_has_any("[PLAN-SBW-04] admin-identity の instance security: ", replace(valid_statements, 5, actions_of(u, lock_keys, apply_denied)))
}

test_violation_member_creating_readable_bucket_policy_others_exception if {
	creating_readable_has_any("[PLAN-SBW-01] admin-identity の instance security: ", replace(valid_statements, 1, others([a, ai, u])))
}

test_violation_member_creating_readable_bucket_policy_tls_missing if {
	creating_readable_has_any("[PLAN-SBW-08] admin-identity の instance security: ", remove(valid_statements, 0))
}

creating_readable_has_any(prefix, statements) if {
	msgs := member_messages(member_creating_readable(statements), [creating_security])
	fixture.has(prefix, msgs)
}

# 印に当たるのに change を object で持たない要素は、書くかを読めないので落とす。
test_violation_marker_without_change if {
	broken := {"address": "module.sx.module.plan_output.aws_s3_bucket.this", "type": "aws_s3_bucket"}
	msgs := messages_for(with_member(array.concat(member_noop(valid_statements, valid_key_statements), [broken])), with_instances([security]))
	fixture.has("[PLAN-SBW-00] input/admin-identity.json: module.sx.module.plan_output.aws_s3_bucket.this は宣言に無い instance を見つける印の address なのに、change を object で持っていません", msgs)
}

package plan.deployment_identity_unlock_role_test

import data.plan.deployment_identity_unlock_role as p
import data.plan.fixture

decl := {"unlock_role": {
	"role": "module.x.aws_iam_role.this[\"unlock_identity\"]",
	"policy": "module.x.aws_iam_role_policy.this[\"unlock_identity\"]",
	"lock_key": "identity/terraform.tfstate.tflock",
	"state_bucket": "module.s.aws_s3_bucket.this",
}}

bucket := "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an"

lock := "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an/identity/terraform.tfstate.tflock"

role_arn := "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock-identity"

list_lock := {"Sid": "ListIdentityLock", "Effect": "Allow", "Action": "s3:ListBucket", "Resource": bucket, "Condition": {"StringEquals": {"s3:prefix": "identity/terraform.tfstate.tflock"}}}

remove_lock := {"Sid": "RemoveIdentityLock", "Effect": "Allow", "Action": ["s3:DeleteObject", "s3:GetObject"], "Resource": lock}

change(address, type, after) := {"address": address, "type": type, "change": {"actions": ["create"], "after": after, "after_unknown": {}}}

state_bucket := change("module.s.aws_s3_bucket.this", "aws_s3_bucket", {"arn": bucket})

role := change("module.x.aws_iam_role.this[\"unlock_identity\"]", "aws_iam_role", {"name": "tabp-admin-ci-unlock-identity", "arn": role_arn})

policy(statements) := change("module.x.aws_iam_role_policy.this[\"unlock_identity\"]", "aws_iam_role_policy", {"name": "unlock_identity", "role": "tabp-admin-ci-unlock-identity", "policy": json.marshal({"Statement": statements})})

exclusives := [
	change("module.x.aws_iam_role_policies_exclusive.this[\"unlock_identity\"]", "aws_iam_role_policies_exclusive", {"role_name": "tabp-admin-ci-unlock-identity", "policy_names": ["unlock_identity"]}),
	change("module.x.aws_iam_role_policy_attachments_exclusive.this[\"unlock_identity\"]", "aws_iam_role_policy_attachments_exclusive", {"role_name": "tabp-admin-ci-unlock-identity", "policy_arns": []}),
]

read_grant(principal, action, objects) := {
	"Sid": "RolesViaS3Read0",
	"Effect": "Allow",
	"Principal": {"AWS": [principal]},
	"Action": action,
	"Resource": "*",
	"Condition": {"StringEquals": {"kms:ViaService": "s3.ap-northeast-1.amazonaws.com", "kms:EncryptionContext:aws:s3:arn": objects}},
}

admin := {"Sid": "AccountKeyAdministration", "Effect": "Allow", "Principal": {"AWS": "arn:aws:iam::123456789012:root"}, "Action": ["kms:PutKeyPolicy"], "Resource": "*"}

key_policy(statements) := fixture.doc("aws_kms_key_policy", "module.k.aws_kms_key_policy.this", "policy", {"Statement": array.concat([admin], statements)})

valid_key := key_policy([read_grant(role_arn, "kms:Decrypt", [lock])])

input_with(statements, key) := fixture.plan(array.concat([state_bucket, role, policy(statements), key], exclusives))

test_pass_reference_shape if {
	count(p.deny) == 0 with input as input_with([list_lock, remove_lock], valid_key) with data.policy_plan.deployment_identity as decl
}

test_pass_key_policy_in_other_file if {
	inp := [
		{"path": "deployment-identity.json", "contents": {"plan_format_version": "1.2", "resource_changes": array.concat([role, policy([list_lock, remove_lock])], exclusives)}},
		{"path": "state-backend.json", "contents": {"plan_format_version": "1.2", "resource_changes": [state_bucket, valid_key]}},
	]
	count(p.deny) == 0 with input as inp with data.policy_plan.deployment_identity as decl
}

test_violation_policy_missing if {
	msgs := p.deny with input as fixture.plan(array.concat([state_bucket, role, valid_key], exclusives)) with data.policy_plan.deployment_identity as decl
	fixture.has("[PLAN-UNLOCK-00]", msgs)
}

test_violation_role_arn_unreadable if {
	r := change("module.x.aws_iam_role.this[\"unlock_identity\"]", "aws_iam_role", {"name": "tabp-admin-ci-unlock-identity"})
	msgs := p.deny with input as fixture.plan(array.concat([state_bucket, r, policy([list_lock, remove_lock]), valid_key], exclusives)) with data.policy_plan.deployment_identity as decl
	fixture.has("[PLAN-UNLOCK-00]", msgs)
}

test_violation_key_policy_missing if {
	msgs := p.deny with input as fixture.plan(array.concat([state_bucket, role, policy([list_lock, remove_lock])], exclusives)) with data.policy_plan.deployment_identity as decl
	fixture.has("[PLAN-UNLOCK-00]", msgs)
}

test_violation_lock_key_undeclared if {
	msgs := p.deny with input as input_with([list_lock, remove_lock], valid_key) with data.policy_plan.deployment_identity as decl with data.policy_plan.deployment_identity.unlock_role.lock_key as ""
	fixture.has("[PLAN-UNLOCK-00]", msgs)
}

test_violation_extra_action if {
	extra := {"Effect": "Allow", "Action": "s3:PutObject", "Resource": lock}
	msgs := p.deny with input as input_with([list_lock, remove_lock, extra], valid_key) with data.policy_plan.deployment_identity as decl
	fixture.has("[PLAN-UNLOCK-01]", msgs)
}

test_violation_delete_object_version if {
	s := object.union(remove_lock, {"Action": ["s3:DeleteObject", "s3:DeleteObjectVersion", "s3:GetObject"]})
	msgs := p.deny with input as input_with([list_lock, s], valid_key) with data.policy_plan.deployment_identity as decl
	fixture.has("[PLAN-UNLOCK-01]", msgs)
}

test_violation_not_action if {
	s := {"Effect": "Allow", "NotAction": "iam:*", "Resource": lock}
	msgs := p.deny with input as input_with([list_lock, remove_lock, s], valid_key) with data.policy_plan.deployment_identity as decl
	fixture.has("[PLAN-UNLOCK-01]", msgs)
}

test_violation_state_key_object if {
	s := object.union(remove_lock, {"Resource": [lock, concat("", [bucket, "/identity/terraform.tfstate"])]})
	msgs := p.deny with input as input_with([list_lock, s], key_policy([read_grant(role_arn, "kms:Decrypt", [lock])])) with data.policy_plan.deployment_identity as decl
	fixture.has("[PLAN-UNLOCK-02]", msgs)
}

test_violation_wildcard_object if {
	s := object.union(remove_lock, {"Resource": concat("", [bucket, "/*/terraform.tfstate.tflock"])})
	msgs := p.deny with input as input_with([list_lock, s], valid_key) with data.policy_plan.deployment_identity as decl
	fixture.has("[PLAN-UNLOCK-02]", msgs)
}

test_violation_other_lock if {
	s := object.union(remove_lock, {"Resource": concat("", [bucket, "/workload/terraform.tfstate.tflock"])})
	msgs := p.deny with input as input_with([list_lock, s], valid_key) with data.policy_plan.deployment_identity as decl
	fixture.has("[PLAN-UNLOCK-02]", msgs)
}

test_violation_list_without_prefix if {
	s := object.remove(list_lock, ["Condition"])
	msgs := p.deny with input as input_with([s, remove_lock], valid_key) with data.policy_plan.deployment_identity as decl
	fixture.has("[PLAN-UNLOCK-02]", msgs)
}

test_violation_list_prefix_like if {
	s := object.union(object.remove(list_lock, ["Condition"]), {"Condition": {"StringLike": {"s3:prefix": "identity/*"}}})
	msgs := p.deny with input as input_with([s, remove_lock], valid_key) with data.policy_plan.deployment_identity as decl
	fixture.has("[PLAN-UNLOCK-02]", msgs)
}

test_violation_extra_policy_on_role if {
	extra := change("module.y.aws_iam_role_policy.extra", "aws_iam_role_policy", {"name": "extra", "role": "tabp-admin-ci-unlock-identity", "policy": json.marshal({"Statement": [list_lock]})})
	msgs := p.deny with input as fixture.plan(array.concat([state_bucket, role, policy([list_lock, remove_lock]), valid_key, extra], exclusives)) with data.policy_plan.deployment_identity as decl
	fixture.has("[PLAN-UNLOCK-03]", msgs)
}

test_violation_exclusive_missing if {
	msgs := p.deny with input as fixture.plan([state_bucket, role, policy([list_lock, remove_lock]), valid_key, exclusives[1]]) with data.policy_plan.deployment_identity as decl
	fixture.has("[PLAN-UNLOCK-03]", msgs)
}

test_violation_exclusive_keeps_other_policy if {
	e := change("module.x.aws_iam_role_policies_exclusive.this[\"unlock_identity\"]", "aws_iam_role_policies_exclusive", {"role_name": "tabp-admin-ci-unlock-identity", "policy_names": ["unlock_identity", "extra"]})
	msgs := p.deny with input as fixture.plan([state_bucket, role, policy([list_lock, remove_lock]), valid_key, e, exclusives[1]]) with data.policy_plan.deployment_identity as decl
	fixture.has("[PLAN-UNLOCK-03]", msgs)
}

test_violation_managed_policy_kept if {
	e := change("module.x.aws_iam_role_policy_attachments_exclusive.this[\"unlock_identity\"]", "aws_iam_role_policy_attachments_exclusive", {"role_name": "tabp-admin-ci-unlock-identity", "policy_arns": ["arn:aws:iam::aws:policy/AdministratorAccess"]})
	msgs := p.deny with input as fixture.plan([state_bucket, role, policy([list_lock, remove_lock]), valid_key, exclusives[0], e]) with data.policy_plan.deployment_identity as decl
	fixture.has("[PLAN-UNLOCK-03]", msgs)
}

test_violation_role_not_in_key_policy if {
	key := key_policy([read_grant("arn:aws:iam::123456789012:role/other", "kms:Decrypt", [lock])])
	msgs := p.deny with input as input_with([list_lock, remove_lock], key) with data.policy_plan.deployment_identity as decl
	fixture.has("[PLAN-UNLOCK-04]", msgs)
}

test_violation_generate_data_key if {
	key := key_policy([read_grant(role_arn, ["kms:Decrypt", "kms:GenerateDataKey"], [lock])])
	msgs := p.deny with input as input_with([list_lock, remove_lock], key) with data.policy_plan.deployment_identity as decl
	fixture.has("[PLAN-UNLOCK-04]", msgs)
}

# role が read-write の文に入ると、どの object でも復号できる。
test_violation_unlimited_decrypt if {
	s := {"Effect": "Allow", "Principal": {"AWS": [role_arn]}, "Action": "kms:Decrypt", "Resource": "*", "Condition": {"StringEquals": {"kms:ViaService": "s3.ap-northeast-1.amazonaws.com"}}}
	msgs := p.deny with input as input_with([list_lock, remove_lock], key_policy([read_grant(role_arn, "kms:Decrypt", [lock]), s])) with data.policy_plan.deployment_identity as decl
	fixture.has("[PLAN-UNLOCK-04]", msgs)
}

test_violation_wildcard_context if {
	key := key_policy([read_grant(role_arn, "kms:Decrypt", [concat("", [bucket, "/identity/*"])])])
	msgs := p.deny with input as input_with([list_lock, remove_lock], key) with data.policy_plan.deployment_identity as decl
	fixture.has("[PLAN-UNLOCK-04]", msgs)
}

test_violation_context_other_object if {
	key := key_policy([read_grant(role_arn, "kms:Decrypt", [concat("", [bucket, "/identity/terraform.tfstate"])])])
	msgs := p.deny with input as input_with([list_lock, remove_lock], key) with data.policy_plan.deployment_identity as decl
	fixture.has("[PLAN-UNLOCK-04]", msgs)
}

test_violation_context_extra_object if {
	key := key_policy([read_grant(role_arn, "kms:Decrypt", [lock, concat("", [bucket, "/identity/terraform.tfstate"])])])
	msgs := p.deny with input as input_with([list_lock, remove_lock], key) with data.policy_plan.deployment_identity as decl
	fixture.has("[PLAN-UNLOCK-04]", msgs)
}

test_violation_role_name_unknown if {
	r := {"address": "module.x.aws_iam_role.this[\"unlock_identity\"]", "type": "aws_iam_role", "change": {"actions": ["create"], "after": {"arn": role_arn}, "after_unknown": {"name": true}}}
	msgs := p.deny with input as fixture.plan(array.concat([state_bucket, r, policy([list_lock, remove_lock]), valid_key], exclusives)) with data.policy_plan.deployment_identity as decl
	fixture.has("[PLAN-UNLOCK-00]", msgs)
}

test_violation_state_bucket_unknown if {
	b := {"address": "module.s.aws_s3_bucket.this", "type": "aws_s3_bucket", "change": {"actions": ["create"], "after": {}, "after_unknown": {"arn": true}}}
	msgs := p.deny with input as fixture.plan(array.concat([b, role, policy([list_lock, remove_lock]), valid_key], exclusives)) with data.policy_plan.deployment_identity as decl
	fixture.has("[PLAN-UNLOCK-00]", msgs)
}

test_violation_not_resource if {
	s := {"Effect": "Allow", "Action": "s3:GetObject", "NotResource": lock}
	msgs := p.deny with input as input_with([list_lock, remove_lock, s], valid_key) with data.policy_plan.deployment_identity as decl
	fixture.has("[PLAN-UNLOCK-01]", msgs)
}

test_violation_lock_in_other_bucket if {
	other := "arn:aws:s3:::other-bucket"
	l := object.union(list_lock, {"Resource": other})
	r := object.union(remove_lock, {"Resource": concat("", [other, "/identity/terraform.tfstate.tflock"])})
	msgs := p.deny with input as input_with([l, r], key_policy([read_grant(role_arn, "kms:Decrypt", [r.Resource])])) with data.policy_plan.deployment_identity as decl
	fixture.has("[PLAN-UNLOCK-02]", msgs)
}

test_violation_list_any_bucket if {
	s := object.union(list_lock, {"Resource": "*"})
	msgs := p.deny with input as input_with([s, remove_lock], valid_key) with data.policy_plan.deployment_identity as decl
	fixture.has("[PLAN-UNLOCK-02]", msgs)
}

# ListBucket と object が別々の Bucket を指せば、少なくとも片方は state の Bucket ではない。
test_violation_list_and_object_buckets_differ if {
	s := object.union(list_lock, {"Resource": "arn:aws:s3:::other-bucket"})
	msgs := p.deny with input as input_with([s, remove_lock], valid_key) with data.policy_plan.deployment_identity as decl
	fixture.has("[PLAN-UNLOCK-02]", msgs)
}

test_violation_list_prefix_extra_value if {
	s := object.union(object.remove(list_lock, ["Condition"]), {"Condition": {"StringEquals": {"s3:prefix": ["identity/terraform.tfstate.tflock", "identity/"]}}})
	msgs := p.deny with input as input_with([s, remove_lock], valid_key) with data.policy_plan.deployment_identity as decl
	fixture.has("[PLAN-UNLOCK-02]", msgs)
}

test_violation_managed_policy_attached if {
	extra := change("module.y.aws_iam_role_policy_attachment.extra", "aws_iam_role_policy_attachment", {"role": "tabp-admin-ci-unlock-identity", "policy_arn": "arn:aws:iam::aws:policy/AdministratorAccess"})
	msgs := p.deny with input as fixture.plan(array.concat([state_bucket, role, policy([list_lock, remove_lock]), valid_key, extra], exclusives)) with data.policy_plan.deployment_identity as decl
	fixture.has("[PLAN-UNLOCK-03]", msgs)
}

test_violation_unknown_role_of_policy if {
	extra := {"address": "module.y.aws_iam_role_policy.extra", "type": "aws_iam_role_policy", "change": {"actions": ["create"], "after": {"policy": "{}"}, "after_unknown": {"role": true}}}
	msgs := p.deny with input as fixture.plan(array.concat([state_bucket, role, policy([list_lock, remove_lock]), valid_key, extra], exclusives)) with data.policy_plan.deployment_identity as decl
	fixture.has("[PLAN-UNLOCK-03]", msgs)
}

test_violation_unknown_role_of_exclusive if {
	e := {"address": "module.y.aws_iam_role_policies_exclusive.extra", "type": "aws_iam_role_policies_exclusive", "change": {"actions": ["create"], "after": {"policy_names": []}, "after_unknown": {"role_name": true}}}
	msgs := p.deny with input as fixture.plan(array.concat([state_bucket, role, policy([list_lock, remove_lock]), valid_key, e], exclusives)) with data.policy_plan.deployment_identity as decl
	some m in msgs
	startswith(m, "[PLAN-UNLOCK-03]")
	contains(m, "module.y.aws_iam_role_policies_exclusive.extra がどの role に付くかを plan で読めません")
}

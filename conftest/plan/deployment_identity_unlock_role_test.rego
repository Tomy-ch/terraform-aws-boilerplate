package plan.deployment_identity_unlock_role_test

import data.plan.deployment_identity_unlock_role as p
import data.plan.fixture

decl := {"unlock_roles": ["unlock_role"], "unlock_role": {
	"role": "module.x.aws_iam_role.this[\"unlock_identity\"]",
	"policy": "module.x.aws_iam_role_policy.this[\"unlock_identity\"]",
	"lock_key": "identity/terraform.tfstate.tflock",
	"state_bucket": "module.s.aws_s3_bucket.this",
}}

identity_root := {"writer": "apply_identity_role", "state": "identity/terraform.tfstate", "unlocker": "unlock_role"}

workload_root := {"writer": "apply_role", "state": "workload/terraform.tfstate", "unlocker": "workload_unlock_role"}

sb := {"roots": [identity_root], "key_user_roles": ["unlock_role"]}

sb2 := {"roots": [identity_root, workload_root], "key_user_roles": ["unlock_role", "workload_unlock_role"]}

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
	count(p.deny) == 0 with input as input_with([list_lock, remove_lock], valid_key) with data.policy_plan.deployment_identity as decl with data.policy_plan.state_backend as sb
}

test_pass_key_policy_in_other_file if {
	inp := [
		{"path": "deployment-identity.json", "contents": {"plan_format_version": "1.2", "resource_changes": array.concat([role, policy([list_lock, remove_lock])], exclusives)}},
		{"path": "state-backend.json", "contents": {"plan_format_version": "1.2", "resource_changes": [state_bucket, valid_key]}},
	]
	count(p.deny) == 0 with input as inp with data.policy_plan.deployment_identity as decl with data.policy_plan.state_backend as sb
}

test_violation_policy_missing if {
	msgs := p.deny with input as fixture.plan(array.concat([state_bucket, role, valid_key], exclusives)) with data.policy_plan.deployment_identity as decl with data.policy_plan.state_backend as sb
	fixture.has("[PLAN-UNLOCK-00]", msgs)
}

test_violation_role_arn_unreadable if {
	r := change("module.x.aws_iam_role.this[\"unlock_identity\"]", "aws_iam_role", {"name": "tabp-admin-ci-unlock-identity"})
	msgs := p.deny with input as fixture.plan(array.concat([state_bucket, r, policy([list_lock, remove_lock]), valid_key], exclusives)) with data.policy_plan.deployment_identity as decl with data.policy_plan.state_backend as sb
	fixture.has("[PLAN-UNLOCK-00]", msgs)
}

test_violation_key_policy_missing if {
	msgs := p.deny with input as fixture.plan(array.concat([state_bucket, role, policy([list_lock, remove_lock])], exclusives)) with data.policy_plan.deployment_identity as decl with data.policy_plan.state_backend as sb
	fixture.has("[PLAN-UNLOCK-00]", msgs)
}

test_violation_lock_key_undeclared if {
	msgs := p.deny with input as input_with([list_lock, remove_lock], valid_key) with data.policy_plan.deployment_identity as decl with data.policy_plan.state_backend as sb with data.policy_plan.deployment_identity.unlock_role.lock_key as ""
	fixture.has("[PLAN-UNLOCK-00]", msgs)
}

test_violation_extra_action if {
	extra := {"Effect": "Allow", "Action": "s3:PutObject", "Resource": lock}
	msgs := p.deny with input as input_with([list_lock, remove_lock, extra], valid_key) with data.policy_plan.deployment_identity as decl with data.policy_plan.state_backend as sb
	fixture.has("[PLAN-UNLOCK-01]", msgs)
}

test_violation_delete_object_version if {
	s := object.union(remove_lock, {"Action": ["s3:DeleteObject", "s3:DeleteObjectVersion", "s3:GetObject"]})
	msgs := p.deny with input as input_with([list_lock, s], valid_key) with data.policy_plan.deployment_identity as decl with data.policy_plan.state_backend as sb
	fixture.has("[PLAN-UNLOCK-01]", msgs)
}

test_violation_not_action if {
	s := {"Effect": "Allow", "NotAction": "iam:*", "Resource": lock}
	msgs := p.deny with input as input_with([list_lock, remove_lock, s], valid_key) with data.policy_plan.deployment_identity as decl with data.policy_plan.state_backend as sb
	fixture.has("[PLAN-UNLOCK-01]", msgs)
}

test_violation_state_key_object if {
	s := object.union(remove_lock, {"Resource": [lock, concat("", [bucket, "/identity/terraform.tfstate"])]})
	msgs := p.deny with input as input_with([list_lock, s], key_policy([read_grant(role_arn, "kms:Decrypt", [lock])])) with data.policy_plan.deployment_identity as decl with data.policy_plan.state_backend as sb
	fixture.has("[PLAN-UNLOCK-02]", msgs)
}

test_violation_wildcard_object if {
	s := object.union(remove_lock, {"Resource": concat("", [bucket, "/*/terraform.tfstate.tflock"])})
	msgs := p.deny with input as input_with([list_lock, s], valid_key) with data.policy_plan.deployment_identity as decl with data.policy_plan.state_backend as sb
	fixture.has("[PLAN-UNLOCK-02]", msgs)
}

test_violation_other_lock if {
	s := object.union(remove_lock, {"Resource": concat("", [bucket, "/workload/terraform.tfstate.tflock"])})
	msgs := p.deny with input as input_with([list_lock, s], valid_key) with data.policy_plan.deployment_identity as decl with data.policy_plan.state_backend as sb
	fixture.has("[PLAN-UNLOCK-02]", msgs)
}

test_violation_list_without_prefix if {
	s := object.remove(list_lock, ["Condition"])
	msgs := p.deny with input as input_with([s, remove_lock], valid_key) with data.policy_plan.deployment_identity as decl with data.policy_plan.state_backend as sb
	fixture.has("[PLAN-UNLOCK-02]", msgs)
}

test_violation_list_prefix_like if {
	s := object.union(object.remove(list_lock, ["Condition"]), {"Condition": {"StringLike": {"s3:prefix": "identity/*"}}})
	msgs := p.deny with input as input_with([s, remove_lock], valid_key) with data.policy_plan.deployment_identity as decl with data.policy_plan.state_backend as sb
	fixture.has("[PLAN-UNLOCK-02]", msgs)
}

test_violation_extra_policy_on_role if {
	extra := change("module.y.aws_iam_role_policy.extra", "aws_iam_role_policy", {"name": "extra", "role": "tabp-admin-ci-unlock-identity", "policy": json.marshal({"Statement": [list_lock]})})
	msgs := p.deny with input as fixture.plan(array.concat([state_bucket, role, policy([list_lock, remove_lock]), valid_key, extra], exclusives)) with data.policy_plan.deployment_identity as decl with data.policy_plan.state_backend as sb
	fixture.has("[PLAN-UNLOCK-03]", msgs)
}

test_violation_exclusive_missing if {
	msgs := p.deny with input as fixture.plan([state_bucket, role, policy([list_lock, remove_lock]), valid_key, exclusives[1]]) with data.policy_plan.deployment_identity as decl with data.policy_plan.state_backend as sb
	fixture.has("[PLAN-UNLOCK-03]", msgs)
}

test_violation_policy_name_unknown if {
	unnamed := json.patch(policy([list_lock, remove_lock]), [{"op": "remove", "path": "/change/after/name"}, {"op": "add", "path": "/change/after_unknown/name", "value": true}])
	msgs := p.deny with input as fixture.plan(array.concat([state_bucket, role, unnamed, valid_key], exclusives)) with data.policy_plan.deployment_identity as decl with data.policy_plan.state_backend as sb
	fixture.has(sprintf("[PLAN-UNLOCK-03] p.json: %s の policy_names が [null] ではありません", [exclusives[0].address]), msgs)
}

test_violation_exclusive_keeps_other_policy if {
	e := change("module.x.aws_iam_role_policies_exclusive.this[\"unlock_identity\"]", "aws_iam_role_policies_exclusive", {"role_name": "tabp-admin-ci-unlock-identity", "policy_names": ["unlock_identity", "extra"]})
	msgs := p.deny with input as fixture.plan([state_bucket, role, policy([list_lock, remove_lock]), valid_key, e, exclusives[1]]) with data.policy_plan.deployment_identity as decl with data.policy_plan.state_backend as sb
	fixture.has("[PLAN-UNLOCK-03]", msgs)
}

test_violation_managed_policy_kept if {
	e := change("module.x.aws_iam_role_policy_attachments_exclusive.this[\"unlock_identity\"]", "aws_iam_role_policy_attachments_exclusive", {"role_name": "tabp-admin-ci-unlock-identity", "policy_arns": ["arn:aws:iam::aws:policy/AdministratorAccess"]})
	msgs := p.deny with input as fixture.plan([state_bucket, role, policy([list_lock, remove_lock]), valid_key, exclusives[0], e]) with data.policy_plan.deployment_identity as decl with data.policy_plan.state_backend as sb
	fixture.has("[PLAN-UNLOCK-03]", msgs)
}

test_violation_role_not_in_key_policy if {
	key := key_policy([read_grant("arn:aws:iam::123456789012:role/other", "kms:Decrypt", [lock])])
	msgs := p.deny with input as input_with([list_lock, remove_lock], key) with data.policy_plan.deployment_identity as decl with data.policy_plan.state_backend as sb
	fixture.has("[PLAN-UNLOCK-04]", msgs)
}

test_violation_generate_data_key if {
	key := key_policy([read_grant(role_arn, ["kms:Decrypt", "kms:GenerateDataKey"], [lock])])
	msgs := p.deny with input as input_with([list_lock, remove_lock], key) with data.policy_plan.deployment_identity as decl with data.policy_plan.state_backend as sb
	fixture.has("[PLAN-UNLOCK-04]", msgs)
}

# role が read-write の文に入ると、どの object でも復号できる。
test_violation_unlimited_decrypt if {
	s := {"Effect": "Allow", "Principal": {"AWS": [role_arn]}, "Action": "kms:Decrypt", "Resource": "*", "Condition": {"StringEquals": {"kms:ViaService": "s3.ap-northeast-1.amazonaws.com"}}}
	msgs := p.deny with input as input_with([list_lock, remove_lock], key_policy([read_grant(role_arn, "kms:Decrypt", [lock]), s])) with data.policy_plan.deployment_identity as decl with data.policy_plan.state_backend as sb
	fixture.has("[PLAN-UNLOCK-04]", msgs)
}

test_violation_wildcard_context if {
	key := key_policy([read_grant(role_arn, "kms:Decrypt", [concat("", [bucket, "/identity/*"])])])
	msgs := p.deny with input as input_with([list_lock, remove_lock], key) with data.policy_plan.deployment_identity as decl with data.policy_plan.state_backend as sb
	fixture.has("[PLAN-UNLOCK-04]", msgs)
}

test_violation_context_other_object if {
	key := key_policy([read_grant(role_arn, "kms:Decrypt", [concat("", [bucket, "/identity/terraform.tfstate"])])])
	msgs := p.deny with input as input_with([list_lock, remove_lock], key) with data.policy_plan.deployment_identity as decl with data.policy_plan.state_backend as sb
	fixture.has("[PLAN-UNLOCK-04]", msgs)
}

test_violation_context_extra_object if {
	key := key_policy([read_grant(role_arn, "kms:Decrypt", [lock, concat("", [bucket, "/identity/terraform.tfstate"])])])
	msgs := p.deny with input as input_with([list_lock, remove_lock], key) with data.policy_plan.deployment_identity as decl with data.policy_plan.state_backend as sb
	fixture.has("[PLAN-UNLOCK-04]", msgs)
}

test_violation_role_name_unknown if {
	r := {"address": "module.x.aws_iam_role.this[\"unlock_identity\"]", "type": "aws_iam_role", "change": {"actions": ["create"], "after": {"arn": role_arn}, "after_unknown": {"name": true}}}
	msgs := p.deny with input as fixture.plan(array.concat([state_bucket, r, policy([list_lock, remove_lock]), valid_key], exclusives)) with data.policy_plan.deployment_identity as decl with data.policy_plan.state_backend as sb
	fixture.has("[PLAN-UNLOCK-00]", msgs)
}

test_violation_state_bucket_unknown if {
	b := {"address": "module.s.aws_s3_bucket.this", "type": "aws_s3_bucket", "change": {"actions": ["create"], "after": {}, "after_unknown": {"arn": true}}}
	msgs := p.deny with input as fixture.plan(array.concat([b, role, policy([list_lock, remove_lock]), valid_key], exclusives)) with data.policy_plan.deployment_identity as decl with data.policy_plan.state_backend as sb
	fixture.has("[PLAN-UNLOCK-00]", msgs)
}

test_violation_not_resource if {
	s := {"Effect": "Allow", "Action": "s3:GetObject", "NotResource": lock}
	msgs := p.deny with input as input_with([list_lock, remove_lock, s], valid_key) with data.policy_plan.deployment_identity as decl with data.policy_plan.state_backend as sb
	fixture.has("[PLAN-UNLOCK-01]", msgs)
}

test_violation_lock_in_other_bucket if {
	other := "arn:aws:s3:::other-bucket"
	l := object.union(list_lock, {"Resource": other})
	r := object.union(remove_lock, {"Resource": concat("", [other, "/identity/terraform.tfstate.tflock"])})
	msgs := p.deny with input as input_with([l, r], key_policy([read_grant(role_arn, "kms:Decrypt", [r.Resource])])) with data.policy_plan.deployment_identity as decl with data.policy_plan.state_backend as sb
	fixture.has("[PLAN-UNLOCK-02]", msgs)
}

test_violation_list_any_bucket if {
	s := object.union(list_lock, {"Resource": "*"})
	msgs := p.deny with input as input_with([s, remove_lock], valid_key) with data.policy_plan.deployment_identity as decl with data.policy_plan.state_backend as sb
	fixture.has("[PLAN-UNLOCK-02]", msgs)
}

# ListBucket と object が別々の Bucket を指せば、少なくとも片方は state の Bucket ではない。
test_violation_list_and_object_buckets_differ if {
	s := object.union(list_lock, {"Resource": "arn:aws:s3:::other-bucket"})
	msgs := p.deny with input as input_with([s, remove_lock], valid_key) with data.policy_plan.deployment_identity as decl with data.policy_plan.state_backend as sb
	fixture.has("[PLAN-UNLOCK-02]", msgs)
}

test_violation_list_prefix_extra_value if {
	s := object.union(object.remove(list_lock, ["Condition"]), {"Condition": {"StringEquals": {"s3:prefix": ["identity/terraform.tfstate.tflock", "identity/"]}}})
	msgs := p.deny with input as input_with([s, remove_lock], valid_key) with data.policy_plan.deployment_identity as decl with data.policy_plan.state_backend as sb
	fixture.has("[PLAN-UNLOCK-02]", msgs)
}

test_violation_managed_policy_attached if {
	extra := change("module.y.aws_iam_role_policy_attachment.extra", "aws_iam_role_policy_attachment", {"role": "tabp-admin-ci-unlock-identity", "policy_arn": "arn:aws:iam::aws:policy/AdministratorAccess"})
	msgs := p.deny with input as fixture.plan(array.concat([state_bucket, role, policy([list_lock, remove_lock]), valid_key, extra], exclusives)) with data.policy_plan.deployment_identity as decl with data.policy_plan.state_backend as sb
	fixture.has("[PLAN-UNLOCK-03]", msgs)
}

test_violation_unknown_role_of_policy if {
	extra := {"address": "module.y.aws_iam_role_policy.extra", "type": "aws_iam_role_policy", "change": {"actions": ["create"], "after": {"policy": "{}"}, "after_unknown": {"role": true}}}
	msgs := p.deny with input as fixture.plan(array.concat([state_bucket, role, policy([list_lock, remove_lock]), valid_key, extra], exclusives)) with data.policy_plan.deployment_identity as decl with data.policy_plan.state_backend as sb
	fixture.has("[PLAN-UNLOCK-03]", msgs)
}

test_violation_unknown_role_of_exclusive if {
	e := {"address": "module.y.aws_iam_role_policies_exclusive.extra", "type": "aws_iam_role_policies_exclusive", "change": {"actions": ["create"], "after": {"policy_names": []}, "after_unknown": {"role_name": true}}}
	msgs := p.deny with input as fixture.plan(array.concat([state_bucket, role, policy([list_lock, remove_lock]), valid_key, e], exclusives)) with data.policy_plan.deployment_identity as decl with data.policy_plan.state_backend as sb
	some m in msgs
	startswith(m, "[PLAN-UNLOCK-03]")
	contains(m, "module.y.aws_iam_role_policies_exclusive.extra がどの role に付くかを plan で読めません")
}

# key_user_roles に入るまでは、Key Policy に復号の Allow が無いことを咎めない。
decl2 := object.union(decl, {
	"unlock_roles": ["unlock_role", "workload_unlock_role"],
	"workload_unlock_role": {
		"role": "module.x.aws_iam_role.this[\"unlock\"]",
		"policy": "module.x.aws_iam_role_policy.this[\"unlock\"]",
		"lock_key": "workload/terraform.tfstate.tflock",
		"state_bucket": "module.s.aws_s3_bucket.this",
	},
})

wlock := "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an/workload/terraform.tfstate.tflock"

wrole_arn := "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock"

wlist := {"Sid": "ListWorkloadLock", "Effect": "Allow", "Action": "s3:ListBucket", "Resource": bucket, "Condition": {"StringEquals": {"s3:prefix": "workload/terraform.tfstate.tflock"}}}

wremove := {"Sid": "RemoveWorkloadLock", "Effect": "Allow", "Action": ["s3:DeleteObject", "s3:GetObject"], "Resource": wlock}

wrole := change("module.x.aws_iam_role.this[\"unlock\"]", "aws_iam_role", {"name": "tabp-admin-ci-unlock", "arn": wrole_arn})

wpolicy(statements) := change("module.x.aws_iam_role_policy.this[\"unlock\"]", "aws_iam_role_policy", {"name": "unlock", "role": "tabp-admin-ci-unlock", "policy": json.marshal({"Statement": statements})})

wexclusives := [
	change("module.x.aws_iam_role_policies_exclusive.this[\"unlock\"]", "aws_iam_role_policies_exclusive", {"role_name": "tabp-admin-ci-unlock", "policy_names": ["unlock"]}),
	change("module.x.aws_iam_role_policy_attachments_exclusive.this[\"unlock\"]", "aws_iam_role_policy_attachments_exclusive", {"role_name": "tabp-admin-ci-unlock", "policy_arns": []}),
]

input2(wstatements, key) := fixture.plan(array.concat(array.concat([state_bucket, role, policy([list_lock, remove_lock]), wrole, wpolicy(wstatements), key], exclusives), wexclusives))

test_pass_second_role_without_key_grant_until_declared if {
	count(p.deny) == 0 with input as input2([wlist, wremove], valid_key) with data.policy_plan.deployment_identity as decl2 with data.policy_plan.state_backend as sb
}

test_pass_two_roles_with_key_policy if {
	key := key_policy([read_grant(role_arn, "kms:Decrypt", [lock]), object.union(read_grant(wrole_arn, "kms:Decrypt", [wlock]), {"Sid": "RolesViaS3Read1"})])
	count(p.deny) == 0 with input as input2([wlist, wremove], key) with data.policy_plan.deployment_identity as decl2 with data.policy_plan.state_backend as sb2
}

test_violation_second_role_other_lock if {
	s := object.union(wremove, {"Resource": lock})
	msgs := p.deny with input as input2([wlist, s], valid_key) with data.policy_plan.deployment_identity as decl2 with data.policy_plan.state_backend as sb
	fixture.has("[PLAN-UNLOCK-02]", msgs)
}

test_violation_second_role_extra_action if {
	extra := {"Effect": "Allow", "Action": "s3:PutObject", "Resource": wlock}
	msgs := p.deny with input as input2([wlist, wremove, extra], valid_key) with data.policy_plan.deployment_identity as decl2 with data.policy_plan.state_backend as sb
	fixture.has("[PLAN-UNLOCK-01]", msgs)
}

test_violation_second_role_policy_missing if {
	inp := fixture.plan(array.concat(array.concat([state_bucket, role, policy([list_lock, remove_lock]), wrole, valid_key], exclusives), wexclusives))
	msgs := p.deny with input as inp with data.policy_plan.deployment_identity as decl2 with data.policy_plan.state_backend as sb
	fixture.has("[PLAN-UNLOCK-00]", msgs)
}

test_violation_second_role_declared_key_user_without_grant if {
	msgs := p.deny with input as input2([wlist, wremove], valid_key) with data.policy_plan.deployment_identity as decl2 with data.policy_plan.state_backend as sb2
	fixture.has(sprintf("[PLAN-UNLOCK-04] どの Key Policy も workload_unlock_role の role %s に lock file の復号を許していません", [wrole_arn]), msgs)
}

# Key Policy の宣言に入る前でも、Key Policy が許す復号は IAM の lock file に限る。
test_violation_second_role_unlimited_grant if {
	key := key_policy([read_grant(role_arn, "kms:Decrypt", [lock]), object.union(read_grant(wrole_arn, "kms:Decrypt", [lock]), {"Sid": "RolesViaS3Read1"})])
	msgs := p.deny with input as input2([wlist, wremove], key) with data.policy_plan.deployment_identity as decl2 with data.policy_plan.state_backend as sb
	fixture.has("[PLAN-UNLOCK-04]", msgs)
}

test_violation_same_lock_key if {
	d := json.patch(decl2, [{"op": "replace", "path": "/workload_unlock_role/lock_key", "value": "identity/terraform.tfstate.tflock"}])
	msgs := p.deny with input as input2([wlist, wremove], valid_key) with data.policy_plan.deployment_identity as d with data.policy_plan.state_backend as sb
	fixture.has("[PLAN-UNLOCK-00] unlock_role と workload_unlock_role が同じ lock file \"identity/terraform.tfstate.tflock\" を受け持っています", msgs)
}

# キーを外す代わりに値を null にする（with の差し替えは既存の値と重なる。lib/fixture.rego の inline_declaration）。
test_violation_unlock_roles_null if {
	msgs := p.deny with input as input_with([list_lock, remove_lock], valid_key) with data.policy_plan.deployment_identity as decl with data.policy_plan.deployment_identity.unlock_roles as null with data.policy_plan.state_backend as sb
	fixture.has("[PLAN-UNLOCK-00] lock を外す role の宣言（data.policy_plan.deployment_identity.unlock_roles）が配列ではありません", msgs)
}

test_violation_unlock_roles_not_array if {
	msgs := p.deny with input as input_with([list_lock, remove_lock], valid_key) with data.policy_plan.deployment_identity as object.union(decl, {"unlock_roles": "unlock_role"}) with data.policy_plan.state_backend as sb
	fixture.has("[PLAN-UNLOCK-00] lock を外す role の宣言（data.policy_plan.deployment_identity.unlock_roles）が配列ではありません", msgs)
}

test_violation_unlock_roles_empty if {
	msgs := p.deny with input as input_with([list_lock, remove_lock], valid_key) with data.policy_plan.deployment_identity as object.union(decl, {"unlock_roles": []}) with data.policy_plan.state_backend as sb
	fixture.has("[PLAN-UNLOCK-00] lock を外す role の宣言（data.policy_plan.deployment_identity.unlock_roles）が空です", msgs)
}

test_violation_unlock_role_entry_missing if {
	d := object.union(decl, {"unlock_roles": ["unlock_role", "nothing"]})
	msgs := p.deny with input as input_with([list_lock, remove_lock], valid_key) with data.policy_plan.deployment_identity as d with data.policy_plan.state_backend as sb
	fixture.has("[PLAN-UNLOCK-00] data.policy_plan.deployment_identity.nothing が宣言にありません", msgs)
}

test_violation_unlocker_not_in_unlock_roles if {
	d := object.union(decl2, {"unlock_roles": ["workload_unlock_role"]})
	msgs := p.deny with input as input2([wlist, wremove], valid_key) with data.policy_plan.deployment_identity as d with data.policy_plan.state_backend as sb
	fixture.has("[PLAN-UNLOCK-00] state_backend.roots の unlocker の unlock_role が data.policy_plan.deployment_identity.unlock_roles にありません", msgs)
}

test_violation_workload_unlocker_not_in_unlock_roles if {
	key := key_policy([read_grant(role_arn, "kms:Decrypt", [lock])])
	msgs := p.deny with input as input_with([list_lock, remove_lock], key) with data.policy_plan.deployment_identity as decl with data.policy_plan.state_backend as object.union(sb2, {"key_user_roles": ["unlock_role"]})
	fixture.has("[PLAN-UNLOCK-00] state_backend.roots の unlocker の workload_unlock_role が data.policy_plan.deployment_identity.unlock_roles にありません", msgs)
}

test_violation_second_role_exclusive_missing if {
	inp := fixture.plan(array.concat(array.concat([state_bucket, role, policy([list_lock, remove_lock]), wrole, wpolicy([wlist, wremove]), valid_key], exclusives), [wexclusives[0]]))
	msgs := p.deny with input as inp with data.policy_plan.deployment_identity as decl2 with data.policy_plan.state_backend as sb
	fixture.has("[PLAN-UNLOCK-03] workload_unlock_role の role tabp-admin-ci-unlock の aws_iam_role_policy_attachments_exclusive が 0 件です（1件を期待）", msgs)
}

test_violation_second_role_extra_policy if {
	extra := change("module.y.aws_iam_role_policy.extra", "aws_iam_role_policy", {"name": "extra", "role": "tabp-admin-ci-unlock", "policy": json.marshal({"Statement": [wlist]})})
	inp := fixture.plan(array.concat(array.concat([state_bucket, role, policy([list_lock, remove_lock]), wrole, wpolicy([wlist, wremove]), valid_key, extra], exclusives), wexclusives))
	msgs := p.deny with input as inp with data.policy_plan.deployment_identity as decl2 with data.policy_plan.state_backend as sb
	fixture.has("[PLAN-UNLOCK-03] p.json: module.y.aws_iam_role_policy.extra が workload_unlock_role の role に権限を足しています", msgs)
}

test_violation_second_role_list_prefix_other_lock if {
	s := json.patch(wlist, [{"op": "replace", "path": "/Condition/StringEquals/s3:prefix", "value": "identity/terraform.tfstate.tflock"}])
	msgs := p.deny with input as input2([s, wremove], valid_key) with data.policy_plan.deployment_identity as decl2 with data.policy_plan.state_backend as sb
	fixture.has("[PLAN-UNLOCK-02] p.json: workload_unlock_role の role の s3:ListBucket が、s3:prefix を StringEquals で workload/terraform.tfstate.tflock だけに限っていません", msgs)
}

test_violation_first_role_key_user_without_grant if {
	key := key_policy([object.union(read_grant(wrole_arn, "kms:Decrypt", [wlock]), {"Sid": "RolesViaS3Read1"})])
	msgs := p.deny with input as input2([wlist, wremove], key) with data.policy_plan.deployment_identity as decl2 with data.policy_plan.state_backend as sb2
	fixture.has(sprintf("[PLAN-UNLOCK-04] どの Key Policy も unlock_role の role %s に lock file の復号を許していません", [role_arn]), msgs)
}

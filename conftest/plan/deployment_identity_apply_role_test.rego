package plan.deployment_identity_apply_role_test

import data.plan.deployment_identity_apply_role as p
import data.plan.fixture

g := "module.deployment_identity.module.github_oidc."

account := "123456789012"

role_arn(name) := sprintf("arn:aws:iam::%s:role/%s", [account, name])

provider_arn := sprintf("arn:aws:iam::%s:oidc-provider/token.actions.githubusercontent.com", [account])

st := "arn:aws:s3:::st"

po := "arn:aws:s3:::po"

key := sprintf("arn:aws:kms:ap-northeast-1:%s:key/1234abcd-12ab-34cd-56ef-1234567890ab", [account])

other_key := sprintf("arn:aws:kms:ap-northeast-1:%s:key/ffffffff-12ab-34cd-56ef-1234567890ab", [account])

roles := {
	"this[\"plan\"]": "ci-plan",
	"this[\"apply\"]": "ci-apply",
	"this[\"unlock_identity\"]": "ci-unlock",
	"apply_identity": "ci-apply-identity",
}

role_arns := [role_arn(n) | some _, n in roles]

identity_keys := [sprintf("%s/identity/terraform.tfstate", [st]), sprintf("%s/identity/terraform.tfstate.tflock", [st])]

res(type, address, after) := {"address": address, "type": type, "change": {"actions": ["create"], "after": after, "after_unknown": {}}}

role_resources := [res("aws_iam_role", sprintf("%saws_iam_role.%s", [g, k]), {"name": n, "arn": role_arn(n)}) | some k, n in roles]

delegated_actions := ["kms:DescribeKey", "kms:PutKeyPolicy"]

key_policy := fixture.doc("aws_kms_key_policy", "module.state_backend.module.kms.aws_kms_key_policy.this", "policy", {"Statement": [
	{"Effect": "Allow", "Principal": {"AWS": sprintf("arn:aws:iam::%s:root", [account])}, "Action": delegated_actions, "Resource": "*"},
	{"Effect": "Allow", "Principal": {"AWS": role_arn("ci-plan")}, "Action": ["kms:Decrypt"], "Resource": "*"},
]})

full_deny(resources) := {"Effect": "Deny", "Action": "*", "Resource": resources}

valid_statements := [
	{"Effect": "Allow", "Action": ["s3:GetObject", "s3:PutObject"], "Resource": sprintf("%s/workload/terraform.tfstate", [st])},
	full_deny(array.concat(role_arns, [provider_arn])),
	full_deny(identity_keys),
	{"Effect": "Deny", "NotAction": ["s3:ListBucket"], "Resource": st},
	{"Effect": "Deny", "Action": "s3:DeleteObjectVersion", "Resource": sprintf("%s/*", [st])},
	full_deny([po, sprintf("%s/*", [po])]),
	{"Effect": "Deny", "Action": delegated_actions, "Resource": key},
]

apply_policy_address := sprintf("%saws_iam_role_policy.this[\"apply\"]", [g])

apply_policy(statements) := res("aws_iam_role_policy", apply_policy_address, {"name": "apply", "role": "ci-apply", "policy": json.marshal({"Statement": statements})})

exclusives := [
	res("aws_iam_role_policies_exclusive", sprintf("%saws_iam_role_policies_exclusive.this[\"apply\"]", [g]), {"role_name": "ci-apply", "policy_names": ["apply"]}),
	res("aws_iam_role_policy_attachments_exclusive", sprintf("%saws_iam_role_policy_attachments_exclusive.this[\"apply\"]", [g]), {"role_name": "ci-apply", "policy_arns": []}),
]

others := [
	res("aws_iam_openid_connect_provider", sprintf("%saws_iam_openid_connect_provider.github", [g]), {"arn": provider_arn}),
	res("aws_s3_bucket", "module.state_backend.module.s3.aws_s3_bucket.this", {"arn": st}),
	res("aws_s3_bucket_versioning", "module.state_backend.module.s3.aws_s3_bucket_versioning.this", {}),
	res("aws_s3_bucket", "module.state_backend.module.plan_output.aws_s3_bucket.this", {"arn": po}),
	res("aws_kms_key", "module.state_backend.module.kms.aws_kms_key.this", {"arn": key}),
	key_policy,
]

world(statements) := array.concat(array.concat(array.concat(role_resources, others), exclusives), [apply_policy(statements)])

cov(parent) := {"class": "covered_by", "parent": parent}

bootstrap := {
	"modules": ["module.state_backend.", "module.deployment_identity."],
	"types": {
		"aws_iam_openid_connect_provider": {"class": "target"},
		"aws_iam_role": {"class": "target"},
		"aws_iam_role_policies_exclusive": cov("aws_iam_role"),
		"aws_iam_role_policy": cov("aws_iam_role"),
		"aws_iam_role_policy_attachments_exclusive": cov("aws_iam_role"),
		"aws_kms_key": {"class": "key"},
		"aws_kms_key_policy": cov("aws_kms_key"),
		"aws_s3_bucket": {"class": "target", "children": true},
		"aws_s3_bucket_versioning": cov("aws_s3_bucket"),
	},
	"children": {
		"module.state_backend.module.s3.aws_s3_bucket.this": {"roots": true, "version_deletion": true},
		"module.state_backend.module.plan_output.aws_s3_bucket.this": {"suffixes": ["/*"]},
	},
}

decl := {
	"deployment_identity": {"apply_role": {
		"role": sprintf("%saws_iam_role.this[\"apply\"]", [g]),
		"policy": apply_policy_address,
		"bootstrap": bootstrap,
	}},
	"state_backend": {
		"state_key": "module.state_backend.module.kms.aws_kms_key.this",
		"roots": [
			{"writer": "apply_identity_role", "state": "identity/terraform.tfstate"},
			{"writer": "apply_role", "state": "workload/terraform.tfstate"},
		],
	},
}

# object.union は object を再帰的に重ねるので、差し替えるキーを先に外す。
with_bootstrap(patch) := json.patch(decl, [{"op": "add", "path": "/deployment_identity/apply_role/bootstrap", "value": object.union(object.remove(bootstrap, object.keys(patch)), patch)}])

# 宣言は丸ごと差し替える。子のパスだけを with で差し替えると、規則が親から読むときに decl.json の値と重なる。
messages_for(rcs, d) := msgs if {
	msgs := p.deny with input as fixture.plan(rcs) with data.policy_plan as object.union(object.remove(data.policy_plan, ["deployment_identity", "state_backend"]), d)
}

messages(statements) := messages_for(world(statements), decl)

replace(xs, i, x) := array.concat(array.concat(array.slice(xs, 0, i), [x]), array.slice(xs, i + 1, count(xs)))

remove(xs, i) := array.concat(array.slice(xs, 0, i), array.slice(xs, i + 1, count(xs)))

with_after(rcs, address, after) := [updated(x, address, after) | some x in rcs]

updated(x, address, after) := json.patch(x, [{"op": "replace", "path": "/change/after", "value": after}]) if x.address == address

updated(x, address, _) := x if x.address != address

without(rcs, address) := [x | some x in rcs; x.address != address]

# --- 通る入力 ---

test_pass_valid if {
	count(messages(valid_statements)) == 0
}

# NotAction の名前は大文字小文字を区別しない。Action を配列で書いても * は * である。
test_pass_case_and_array_forms if {
	s := replace(replace(valid_statements, 3, {"Effect": "Deny", "NotAction": ["S3:ListBucket", "s3:GetBucketPolicy"], "Resource": st}), 2, {"Effect": "Deny", "Action": ["*"], "Resource": identity_keys})
	count(messages(s)) == 0
}

test_pass_excluded_type_with_reason if {
	rcs := array.concat(world(valid_statements), [res("aws_cloudwatch_log_resource_policy", "module.state_backend.module.logs.aws_cloudwatch_log_resource_policy.this", {})])
	d := with_bootstrap({"types": object.union(bootstrap.types, {"aws_cloudwatch_log_resource_policy": {"class": "exclude", "reason": "Resource 型を持たない"}})})
	count(messages_for(rcs, d)) == 0
}

test_pass_kms_wildcard_on_key if {
	count(messages(replace(valid_statements, 6, {"Effect": "Deny", "Action": "kms:*", "Resource": key}))) == 0
}

# --- PLAN-APPLY-00 ---

test_violation_apply_policy_missing if {
	fixture.has("[PLAN-APPLY-00] apply の role の policy", messages_for(without(world(valid_statements), apply_policy_address), decl))
}

test_violation_role_missing if {
	rcs := without(world(valid_statements), sprintf("%saws_iam_role.this[\"apply\"]", [g]))
	fixture.has("[PLAN-APPLY-00] apply の role \"", messages_for(rcs, decl))
}

test_violation_policy_on_other_role if {
	rcs := with_after(world(valid_statements), apply_policy_address, {"name": "apply", "role": "ci-plan", "policy": json.marshal({"Statement": valid_statements})})
	fixture.has("[PLAN-APPLY-00] apply の role の policy \"", messages_for(rcs, decl))
}

test_violation_role_arn_name_mismatch if {
	address := sprintf("%saws_iam_role.this[\"plan\"]", [g])
	rcs := with_after(world(valid_statements), address, {"name": "ci-plan", "arn": role_arn("other")})
	fixture.has("[PLAN-APPLY-00] role ", messages_for(rcs, decl))
}

test_violation_role_path if {
	address := sprintf("%saws_iam_role.this[\"plan\"]", [g])
	rcs := with_after(world(valid_statements), address, {"name": "ci-plan", "arn": role_arn("ci-plan"), "path": "/ci/"})
	fixture.has("[PLAN-APPLY-00] role ", messages_for(rcs, decl))
}

test_violation_key_policy_missing if {
	fixture.has("[PLAN-APPLY-00] 鍵 module.state_backend.module.kms.aws_kms_key.this と同じ module の下の Key Policy（aws_kms_key_policy）が 0 件", messages_for(without(world(valid_statements), key_policy.address), decl))
}

test_violation_delegation_unreadable if {
	bare := fixture.doc("aws_kms_key_policy", key_policy.address, "policy", {"Statement": [{"Effect": "Allow", "Principal": {"AWS": account}, "Action": "kms:PutKeyPolicy", "Resource": "*"}]})
	rcs := array.concat(without(world(valid_statements), key_policy.address), [bare])
	fixture.has("[PLAN-APPLY-00] ", messages_for(rcs, decl))
	some m in messages_for(rcs, decl)
	contains(m, "アカウントへの委任の Allow")
}

test_violation_key_arn_unknown if {
	rcs := with_after(world(valid_statements), "module.state_backend.module.kms.aws_kms_key.this", {})
	fixture.has("[PLAN-APPLY-00] 鍵 module.state_backend.module.kms.aws_kms_key.this の ARN", messages_for(rcs, decl))
}

test_violation_bootstrap_malformed if {
	every b in [
		{"modules": []},
		{"modules": "module.state_backend."},
		{"types": {}},
		{"types": object.union(object.remove(bootstrap.types, ["aws_s3_bucket_versioning"]), {"aws_s3_bucket_versioning": {"class": "guarded"}})},
		{"types": object.union(object.remove(bootstrap.types, ["aws_s3_bucket_versioning"]), {"aws_s3_bucket_versioning": {"class": "covered_by"}})},
		{"types": object.union(object.remove(bootstrap.types, ["aws_s3_bucket_versioning"]), {"aws_s3_bucket_versioning": {"class": "exclude", "reason": " "}})},
		{"children": {"module.state_backend.module.s3.aws_s3_bucket.this": {"suffixes": [""]}}},
	] {
		fixture.has("[PLAN-APPLY-00] data.policy_plan.deployment_identity.apply_role.bootstrap が", messages_for(world(valid_statements), with_bootstrap(b)))
	}
}

test_violation_bootstrap_missing if {
	d := json.patch(decl, [{"op": "remove", "path": "/deployment_identity/apply_role/bootstrap"}])
	fixture.has("[PLAN-APPLY-00] data.policy_plan.deployment_identity.apply_role.bootstrap が", messages_for(world(valid_statements), d))
}

# 片方の use-case の address が変わると、その下の構成物が黙って対象から外れる。prefix ごとに数える。
test_violation_module_prefix_empty if {
	d := with_bootstrap({"modules": ["module.state_backend.", "module.renamed."]})
	fixture.has("[PLAN-APPLY-00] bootstrap の module \"module.renamed.\"", messages_for(world(valid_statements), d))
}

test_violation_target_type_absent if {
	rcs := without(world(valid_statements), sprintf("%saws_iam_openid_connect_provider.github", [g]))
	fixture.has("[PLAN-APPLY-00] bootstrap の構成物の型 aws_iam_openid_connect_provider", messages_for(rcs, decl))
}

# mock provider は override の無い computed な属性に乱数を入れる。ARN の形でない値を対象として数えない。
test_violation_target_arn_not_arn if {
	every v in [{}, {"arn": ""}, {"arn": "Zq8x1"}] {
		rcs := with_after(world(valid_statements), "module.state_backend.module.plan_output.aws_s3_bucket.this", v)
		fixture.has("[PLAN-APPLY-00] bootstrap の構成物 module.state_backend.module.plan_output.aws_s3_bucket.this の ARN", messages_for(rcs, decl))
	}
}

test_violation_children_address_absent if {
	d := with_bootstrap({"children": object.union(bootstrap.children, {"module.state_backend.module.gone.aws_s3_bucket.this": {"suffixes": ["/*"]}})})
	fixture.has("[PLAN-APPLY-00] data.policy_plan.deployment_identity.apply_role.bootstrap.children の module.state_backend.module.gone", messages_for(world(valid_statements), d))
}

test_violation_no_writer_roots if {
	d := json.patch(decl, [{"op": "replace", "path": "/state_backend/roots", "value": [{"writer": "apply_role", "state": "workload/terraform.tfstate"}]}])
	fixture.has("[PLAN-APPLY-00] data.policy_plan.state_backend.roots に", messages_for(world(valid_statements), d))
}

# --- PLAN-APPLY-01 ---

test_violation_delegated_action_not_denied if {
	msgs := messages(replace(valid_statements, 6, {"Effect": "Deny", "Action": "kms:PutKeyPolicy", "Resource": key}))
	fixture.has("[PLAN-APPLY-01]", msgs)
	some m in msgs
	contains(m, "kms:describekey")
}

test_violation_deny_on_other_key if {
	fixture.has("[PLAN-APPLY-01]", messages(replace(valid_statements, 6, {"Effect": "Deny", "Action": delegated_actions, "Resource": other_key})))
}

test_violation_conditional_key_deny if {
	s := {"Effect": "Deny", "Action": delegated_actions, "Resource": key, "Condition": {"Bool": {"aws:MultiFactorAuthPresent": "false"}}}
	fixture.has("[PLAN-APPLY-01]", messages(replace(valid_statements, 6, s)))
}

# --- PLAN-APPLY-02 ---

test_violation_unclassified_type if {
	rcs := array.concat(world(valid_statements), [res("aws_cloudwatch_log_group", "module.state_backend.module.logs.aws_cloudwatch_log_group.this", {"arn": "arn:aws:logs:ap-northeast-1:123456789012:log-group:x"})])
	fixture.has("[PLAN-APPLY-02] bootstrap の構成物 module.state_backend.module.logs.aws_cloudwatch_log_group.this の型", messages_for(rcs, decl))
}

test_violation_covered_by_parent_absent if {
	rcs := array.concat(world(valid_statements), [res("aws_s3_bucket_versioning", "module.state_backend.module.orphan.aws_s3_bucket_versioning.this", {})])
	fixture.has("[PLAN-APPLY-02] module.state_backend.module.orphan.aws_s3_bucket_versioning.this の親", messages_for(rcs, decl))
}

# 3つ目の Bucket は、子の ARN を宣言するまで赤にする。宣言が無いと、その object を守る Deny を誰も求めない。
test_violation_children_undeclared if {
	extra := "arn:aws:s3:::third"
	s := array.concat(valid_statements, [full_deny([extra])])
	rcs := array.concat(world(s), [res("aws_s3_bucket", "module.state_backend.module.third.aws_s3_bucket.this", {"arn": extra})])
	fixture.has("[PLAN-APPLY-02] module.state_backend.module.third.aws_s3_bucket.this の子の ARN", messages_for(rcs, decl))
}

# --- PLAN-APPLY-03 ---

# サービスの wildcard は同じ ARN への別のサービスの Action（role の ARN への sts:AssumeRole）を覆わない。
test_violation_service_wildcard if {
	s := replace(valid_statements, 1, {"Effect": "Deny", "Action": "iam:*", "Resource": array.concat(role_arns, [provider_arn])})
	msgs := messages(s)
	every arn in array.concat(role_arns, [provider_arn]) {
		fixture.has(sprintf("[PLAN-APPLY-03] apply の role %q が、%s へ", [apply_policy_address, arn]), msgs)
	}
}

test_violation_action_prefix_list if {
	s := replace(valid_statements, 3, {"Effect": "Deny", "Action": ["s3:Put*", "s3:Delete*"], "Resource": st})
	fixture.has(sprintf("[PLAN-APPLY-03] apply の role %q が、%s へ", [apply_policy_address, st]), messages(s))
}

test_violation_conditional_deny if {
	s := replace(valid_statements, 5, object.union(full_deny([po, sprintf("%s/*", [po])]), {"Condition": {"StringEquals": {"aws:RequestedRegion": "ap-northeast-1"}}}))
	fixture.has(sprintf("[PLAN-APPLY-03] apply の role %q が、%s へ", [apply_policy_address, po]), messages(s))
}

test_violation_not_resource if {
	s := replace(valid_statements, 5, {"Effect": "Deny", "Action": "*", "Resource": [po, sprintf("%s/*", [po])], "NotResource": "arn:aws:s3:::other"})
	fixture.has(sprintf("[PLAN-APPLY-03] apply の role %q が、%s へ", [apply_policy_address, po]), messages(s))
}

# Resource は文字列の完全一致で見る。pattern は評価しない。
test_violation_resource_pattern if {
	s := replace(valid_statements, 5, full_deny(["arn:aws:s3:::p*"]))
	fixture.has(sprintf("[PLAN-APPLY-03] apply の role %q が、%s へ", [apply_policy_address, po]), messages(s))
}

test_violation_not_action_names_non_read if {
	every names in [["s3:ListBucket", "s3:PutBucketPolicy"], ["s3:Get*"], ["s3:ListBucket", "s3:TagResource"]] {
		s := replace(valid_statements, 3, {"Effect": "Deny", "NotAction": names, "Resource": st})
		fixture.has(sprintf("[PLAN-APPLY-03] apply の role %q が、%s へ", [apply_policy_address, st]), messages(s))
	}
}

test_violation_allow_is_not_deny if {
	s := replace(valid_statements, 5, {"Effect": "Allow", "Action": "*", "Resource": [po, sprintf("%s/*", [po])]})
	fixture.has(sprintf("[PLAN-APPLY-03] apply の role %q が、%s へ", [apply_policy_address, po]), messages(s))
}

test_violation_identity_lock_uncovered if {
	s := replace(valid_statements, 2, full_deny([identity_keys[0]]))
	fixture.has(sprintf("[PLAN-APPLY-03] apply の role %q が、%s へ", [apply_policy_address, identity_keys[1]]), messages(s))
}

test_violation_plan_output_objects_uncovered if {
	s := replace(valid_statements, 5, full_deny([po]))
	fixture.has(sprintf("[PLAN-APPLY-03] apply の role %q が、%s/* へ", [apply_policy_address, po]), messages(s))
}

test_violation_new_role_uncovered if {
	rcs := array.concat(world(valid_statements), [res("aws_iam_role", sprintf("%saws_iam_role.this[\"extra\"]", [g]), {"name": "ci-extra", "arn": role_arn("ci-extra")})])
	fixture.has(sprintf("[PLAN-APPLY-03] apply の role %q が、%s へ", [apply_policy_address, role_arn("ci-extra")]), messages_for(rcs, decl))
}

test_violation_other_instance_deny if {
	other := res("aws_iam_role_policy", sprintf("%saws_iam_role_policy.this[\"plan\"]", [g]), {"name": "plan", "role": "ci-plan", "policy": json.marshal({"Statement": valid_statements})})
	rcs := array.concat(world(remove(valid_statements, 5)), [other])
	fixture.has(sprintf("[PLAN-APPLY-03] apply の role %q が、%s へ", [apply_policy_address, po]), messages_for(rcs, decl))
}

# --- PLAN-APPLY-04 ---

test_violation_version_deletion_not_denied if {
	every s in [
		remove(valid_statements, 4),
		replace(valid_statements, 4, {"Effect": "Deny", "Action": "s3:*", "Resource": sprintf("%s/*", [st])}),
		replace(valid_statements, 4, {"Effect": "Deny", "Action": "s3:Delete*", "Resource": sprintf("%s/*", [st])}),
		replace(valid_statements, 4, {"Effect": "Deny", "Action": "s3:DeleteObjectVersion", "Resource": sprintf("%s/*", [st]), "Condition": {"Bool": {"aws:SecureTransport": "true"}}}),
	] {
		fixture.has(sprintf("[PLAN-APPLY-04] apply の role %q が、%s/* への", [apply_policy_address, st]), messages(s))
	}
}

# --- PLAN-APPLY-05 ---

test_violation_extra_inline_policy if {
	extra := res("aws_iam_role_policy", sprintf("%saws_iam_role_policy.extra", [g]), {"name": "extra", "role": "ci-apply", "policy": json.marshal({"Statement": [{"Effect": "Allow", "Action": "s3:GetObject", "Resource": "*"}]})})
	fixture.has("[PLAN-APPLY-05] p.json: module.deployment_identity.module.github_oidc.aws_iam_role_policy.extra が apply の role に", messages_for(array.concat(world(valid_statements), [extra]), decl))
}

test_violation_managed_policy_attachment if {
	extra := res("aws_iam_role_policy_attachment", sprintf("%saws_iam_role_policy_attachment.extra", [g]), {"role": "ci-apply", "policy_arn": "arn:aws:iam::aws:policy/AdministratorAccess"})
	fixture.has("[PLAN-APPLY-05] p.json: module.deployment_identity.module.github_oidc.aws_iam_role_policy_attachment.extra が apply の role に", messages_for(array.concat(world(valid_statements), [extra]), decl))
}

test_violation_attachment_role_unknown if {
	extra := {"address": sprintf("%saws_iam_role_policy_attachment.later", [g]), "type": "aws_iam_role_policy_attachment", "change": {"actions": ["create"], "after": {}, "after_unknown": {"role": true}}}
	fixture.has("[PLAN-APPLY-05] p.json: module.deployment_identity.module.github_oidc.aws_iam_role_policy_attachment.later がどの role に", messages_for(array.concat(world(valid_statements), [extra]), decl))
}

test_violation_exclusive_missing if {
	every e in exclusives {
		fixture.has(sprintf("[PLAN-APPLY-05] apply の role ci-apply の %s が 0 件です", [e.type]), messages_for(without(world(valid_statements), e.address), decl))
	}
}

test_violation_exclusive_contents if {
	rcs := with_after(with_after(world(valid_statements), exclusives[0].address, {"role_name": "ci-apply", "policy_names": ["apply", "extra"]}), exclusives[1].address, {"role_name": "ci-apply", "policy_arns": ["arn:aws:iam::aws:policy/ReadOnlyAccess"]})
	msgs := messages_for(rcs, decl)
	fixture.has(sprintf("[PLAN-APPLY-05] p.json: %s の policy_names", [exclusives[0].address]), msgs)
	fixture.has(sprintf("[PLAN-APPLY-05] p.json: %s の policy_arns", [exclusives[1].address]), msgs)
}

# --- 2本目の鍵 ---

second_key := sprintf("arn:aws:kms:ap-northeast-1:%s:key/22222222-12ab-34cd-56ef-1234567890ab", [account])

second_key_resources := [
	res("aws_kms_key", "module.state_backend.module.kms_second.aws_kms_key.this", {"arn": second_key}),
	fixture.doc("aws_kms_key_policy", "module.state_backend.module.kms_second.aws_kms_key_policy.this", "policy", {"Statement": [
		{"Effect": "Allow", "Principal": {"AWS": sprintf("arn:aws:iam::%s:root", [account])}, "Action": ["kms:PutKeyPolicy", "kms:ScheduleKeyDeletion"], "Resource": "*"},
	]}),
]

test_pass_second_key_denied if {
	s := array.concat(valid_statements, [{"Effect": "Deny", "Action": ["kms:PutKeyPolicy", "kms:ScheduleKeyDeletion"], "Resource": second_key}])
	count(messages_for(array.concat(world(s), second_key_resources), decl)) == 0
}

test_violation_second_key_not_denied if {
	s := replace(valid_statements, 6, {"Effect": "Deny", "Action": ["kms:DescribeKey", "kms:PutKeyPolicy", "kms:ScheduleKeyDeletion"], "Resource": key})
	msgs := messages_for(array.concat(world(s), second_key_resources), decl)
	fixture.has("[PLAN-APPLY-01] p.json: module.state_backend.module.kms_second.aws_kms_key_policy.this がアカウントへ委任する", msgs)
}

test_violation_key_policy_without_key if {
	orphan := json.patch(key_policy, [{"op": "replace", "path": "/address", "value": "module.state_backend.module.orphan.aws_kms_key_policy.this"}])
	rcs := array.concat(world(valid_statements), [orphan])
	fixture.has("[PLAN-APPLY-00] p.json: Key Policy module.state_backend.module.orphan.aws_kms_key_policy.this と同じ module の下の鍵が 0 件", messages_for(rcs, decl))
}

test_violation_two_key_policies_one_key if {
	extra := json.patch(key_policy, [{"op": "replace", "path": "/address", "value": "module.state_backend.module.kms.aws_kms_key_policy.extra"}])
	fixture.has("[PLAN-APPLY-00] 鍵 module.state_backend.module.kms.aws_kms_key.this と同じ module の下の Key Policy（aws_kms_key_policy）が 2 件", messages_for(array.concat(world(valid_statements), [extra]), decl))
}

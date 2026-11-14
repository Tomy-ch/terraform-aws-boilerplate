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

apply_role_address := sprintf("%saws_iam_role.this[\"apply\"]", [g])

role_resources := [res("aws_iam_role", sprintf("%saws_iam_role.%s", [g, k]), {"name": n, "arn": role_arn(n), "inline_policy": [], "managed_policy_arns": []}) | some k, n in roles]

delegated_actions := ["kms:DescribeKey", "kms:PutKeyPolicy"]

key_policy_body := {"Statement": [
	{"Effect": "Allow", "Principal": {"AWS": sprintf("arn:aws:iam::%s:root", [account])}, "Action": delegated_actions, "Resource": "*"},
	{"Effect": "Allow", "Principal": {"AWS": role_arn("ci-plan")}, "Action": ["kms:Decrypt"], "Resource": "*"},
]}

key_policy_address := "module.state_backend.module.kms.aws_kms_key_policy.this"

key_policy := res("aws_kms_key_policy", key_policy_address, {"key_id": "key-id", "policy": json.marshal(key_policy_body)})

full_deny(resources) := {"Effect": "Deny", "Action": "*", "Resource": resources}

valid_statements := [
	{"Effect": "Allow", "Action": ["s3:GetObject", "s3:PutObject"], "Resource": sprintf("%s/workload/terraform.tfstate", [st])},
	full_deny(array.concat(role_arns, [provider_arn])),
	full_deny(identity_keys),
	{"Effect": "Deny", "NotAction": ["s3:ListBucket"], "Resource": st},
	{"Effect": "Deny", "Action": "s3:DeleteObjectVersion", "Resource": sprintf("%s/*", [st])},
	full_deny([po, sprintf("%s/*", [po])]),
	{"Effect": "Deny", "Action": delegated_actions, "Resource": key},
	wildcard_deny,
]

wildcard_deny := {"Effect": "Deny", "Action": ["logs:PutAccountPolicy", "LOGS:PutResourcePolicy"], "Resource": "*"}

apply_policy_address := sprintf("%saws_iam_role_policy.this[\"apply\"]", [g])

role_policy(k, name, statements) := res("aws_iam_role_policy", sprintf("%saws_iam_role_policy.this[\"%s\"]", [g, k]), {"name": k, "role": name, "policy": json.marshal({"Statement": statements})})

apply_policy(statements) := role_policy("apply", "ci-apply", statements)

exclusives_of(k, name) := [
	res("aws_iam_role_policies_exclusive", sprintf("%saws_iam_role_policies_exclusive.this[\"%s\"]", [g, k]), {"role_name": name, "policy_names": [k]}),
	res("aws_iam_role_policy_attachments_exclusive", sprintf("%saws_iam_role_policy_attachments_exclusive.this[\"%s\"]", [g, k]), {"role_name": name, "policy_arns": []}),
]

exclusives := exclusives_of("apply", "ci-apply")

# 実際の plan と同じく、apply のほかの role の policy と排他も入れる。apply の role に付くものだけを数えることを見るため。
siblings := array.concat([role_policy("plan", "ci-plan", [{"Effect": "Allow", "Action": "s3:GetObject", "Resource": "*"}])], exclusives_of("plan", "ci-plan"))

others := [
	res("aws_iam_openid_connect_provider", sprintf("%saws_iam_openid_connect_provider.github", [g]), {"arn": provider_arn}),
	res("aws_s3_bucket", "module.state_backend.module.s3.aws_s3_bucket.this", {"arn": st, "id": "st-id"}),
	res("aws_s3_bucket_versioning", "module.state_backend.module.s3.aws_s3_bucket_versioning.this", {"bucket": "st-id"}),
	res("aws_s3_bucket", "module.state_backend.module.plan_output.aws_s3_bucket.this", {"arn": po, "id": "po-id"}),
	res("aws_kms_key", "module.state_backend.module.kms.aws_kms_key.this", {"arn": key, "id": "key-id", "key_id": "key-kid"}),
	res("aws_kms_alias", "module.state_backend.module.kms.aws_kms_alias.this", {"target_key_id": "key-kid"}),
	key_policy,
]

world(statements) := array.concat(array.concat(array.concat(array.concat(role_resources, others), exclusives), siblings), [apply_policy(statements)])

cov(parent, ref, parent_ref) := {"class": "covered_by", "parent": parent, "ref": ref, "parent_ref": parent_ref}

bootstrap := {
	"modules": ["module.state_backend.", "module.deployment_identity."],
	"types": {
		"aws_iam_openid_connect_provider": {"class": "target"},
		"aws_iam_role": {"class": "target"},
		"aws_iam_role_policies_exclusive": cov("aws_iam_role", "role_name", "name"),
		"aws_iam_role_policy": cov("aws_iam_role", "role", "name"),
		"aws_iam_role_policy_attachments_exclusive": cov("aws_iam_role", "role_name", "name"),
		"aws_kms_alias": cov("aws_kms_key", "target_key_id", "key_id"),
		"aws_kms_key": {"class": "key"},
		"aws_kms_key_policy": cov("aws_kms_key", "key_id", "id"),
		"aws_s3_bucket": {"class": "target", "children": true},
		"aws_s3_bucket_versioning": cov("aws_s3_bucket", "bucket", "id"),
	},
	"children": {
		"module.state_backend.module.s3.aws_s3_bucket.this": {"roots": true, "version_deletion": true},
		"module.state_backend.module.plan_output.aws_s3_bucket.this": {"suffixes": ["/*"]},
	},
}

decl := {
	"deployment_identity": {"apply_role": {
		"role": apply_role_address,
		"policy": apply_policy_address,
		"bootstrap": bootstrap,
		"wildcard_denies": ["logs:PutAccountPolicy", "logs:PutResourcePolicy"],
	}},
	"state_backend": {
		"bucket": "module.state_backend.module.s3.aws_s3_bucket.this",
		"roots": [
			{"writer": "apply_identity_role", "state": "identity/terraform.tfstate"},
			{"writer": "apply_role", "state": "workload/terraform.tfstate"},
		],
	},
}

# object.union は object を再帰的に重ねるので、差し替えるキーを先に外す。
with_bootstrap(patch) := json.patch(decl, [{"op": "add", "path": "/deployment_identity/apply_role/bootstrap", "value": object.union(object.remove(bootstrap, object.keys(patch)), patch)}])

with_type(type, t) := with_bootstrap({"types": object.union(object.remove(bootstrap.types, [type]), {type: t})})

with_child(address, c) := with_bootstrap({"children": object.union(object.remove(bootstrap.children, [address]), {address: c})})

state_bucket := "module.state_backend.module.s3.aws_s3_bucket.this"

plan_bucket := "module.state_backend.module.plan_output.aws_s3_bucket.this"

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

denied_03(arn) := sprintf("[PLAN-APPLY-04] apply の role %q が、%s へのすべての Action（* か", [apply_policy_address, arn])

denied_03_strict(arn) := sprintf("[PLAN-APPLY-04] apply の role %q が、%s へのすべての Action（*）", [apply_policy_address, arn])

malformed := "[PLAN-APPLY-00] data.policy_plan.deployment_identity.apply_role.bootstrap が"

# --- 通る入力 ---

test_pass_valid if {
	count(messages(valid_statements)) == 0
}

# NotAction の名前は大文字小文字を区別しない。Action を配列で書いても * は * である。
test_pass_case_and_array_forms if {
	s := replace(replace(valid_statements, 3, {"Effect": "Deny", "NotAction": ["S3:ListBucket", "s3:GetBucketPolicy"], "Resource": st}), 2, {"Effect": "Deny", "Action": ["*"], "Resource": identity_keys})
	count(messages(s)) == 0
}

test_pass_kms_wildcard_on_key if {
	count(messages(replace(valid_statements, 6, {"Effect": "Deny", "Action": "kms:*", "Resource": key}))) == 0
}

test_pass_full_deny_on_key if {
	count(messages(replace(valid_statements, 6, full_deny([key])))) == 0
}

test_pass_deny_wider_than_delegation if {
	count(messages(replace(valid_statements, 6, {"Effect": "Deny", "Action": array.concat(delegated_actions, ["kms:TagResource"]), "Resource": key}))) == 0
}

test_pass_version_deletion_by_full_deny if {
	count(messages(replace(valid_statements, 4, full_deny([sprintf("%s/*", [st])])))) == 0
}

test_pass_role_path_slash if {
	rcs := with_after(world(valid_statements), apply_role_address, {"name": "ci-apply", "arn": role_arn("ci-apply"), "path": "/", "inline_policy": [], "managed_policy_arns": []})
	count(messages_for(rcs, decl)) == 0
}

test_pass_outside_modules_ignored if {
	rcs := array.concat(world(valid_statements), [res("aws_sqs_queue", "module.workload.aws_sqs_queue.this", {"arn": "arn:aws:sqs:ap-northeast-1:123456789012:q"})])
	count(messages_for(rcs, decl)) == 0
}

# --- PLAN-APPLY-00 ---

test_violation_apply_policy_missing if {
	fixture.has(sprintf("[PLAN-APPLY-00] apply の role の policy %q が入力にありません", [apply_policy_address]), messages_for(without(world(valid_statements), apply_policy_address), decl))
}

test_violation_role_missing if {
	fixture.has("[PLAN-APPLY-00] apply の role \"", messages_for(without(world(valid_statements), apply_role_address), decl))
}

test_violation_declaration_missing if {
	d := {"deployment_identity": {}, "state_backend": decl.state_backend}
	msgs := messages_for(world(valid_statements), d)
	fixture.has("[PLAN-APPLY-00] apply の role の policy \"\" が入力にありません", msgs)
	fixture.has(malformed, msgs)
}

test_violation_policy_on_other_role if {
	rcs := with_after(world(valid_statements), apply_policy_address, {"name": "apply", "role": "ci-plan", "policy": json.marshal({"Statement": valid_statements})})
	fixture.has(sprintf("[PLAN-APPLY-00] apply の role の policy %q が、apply の role \"ci-apply\" に付いていません", [apply_policy_address]), messages_for(rcs, decl))
}

test_violation_role_arn_name_mismatch if {
	address := sprintf("%saws_iam_role.this[\"plan\"]", [g])
	rcs := with_after(world(valid_statements), address, {"name": "ci-plan", "arn": role_arn("other")})
	fixture.has(sprintf("[PLAN-APPLY-00] role %s の path", [address]), messages_for(rcs, decl))
}

test_violation_role_path if {
	address := sprintf("%saws_iam_role.this[\"plan\"]", [g])
	rcs := with_after(world(valid_statements), address, {"name": "ci-plan", "arn": role_arn("ci-plan"), "path": "/ci/"})
	fixture.has(sprintf("[PLAN-APPLY-00] role %s の path", [address]), messages_for(rcs, decl))
}

test_violation_key_policy_missing if {
	fixture.has("[PLAN-APPLY-00] 鍵 module.state_backend.module.kms.aws_kms_key.this と同じ module の下の Key Policy（aws_kms_key_policy）が 0 件", messages_for(without(world(valid_statements), key_policy_address), decl))
}

# Principal が AWS の主体でない Allow は、アカウントへの委任ではない。
test_violation_delegation_unreadable if {
	body := {"Statement": [{"Effect": "Allow", "Principal": {"Service": "logs.amazonaws.com"}, "Action": "kms:Decrypt", "Resource": "*"}]}
	rcs := with_after(world(valid_statements), key_policy_address, {"key_id": "key-id", "policy": json.marshal(body)})
	fixture.has(sprintf("[PLAN-APPLY-00] p.json: %s に、アカウントへの委任の Allow", [key_policy_address]), messages_for(rcs, decl))
}

test_violation_key_arn_unknown if {
	rcs := with_after(world(valid_statements), "module.state_backend.module.kms.aws_kms_key.this", {"id": "key-id", "key_id": "key-kid"})
	fixture.has("[PLAN-APPLY-00] 鍵 module.state_backend.module.kms.aws_kms_key.this の ARN", messages_for(rcs, decl))
}

# 宣言の形が崩れると、規則は既定値を読んで要求を黙って落とす。どの崩れも PLAN-APPLY-00 で止める。
test_violation_bootstrap_malformed if {
	every d in [
		with_bootstrap({"modules": []}),
		with_bootstrap({"modules": "module.state_backend."}),
		with_bootstrap({"modules": ["module.state_backend.", ""]}),
		with_bootstrap({"modules": ["module.state_backend.", 1]}),
		with_bootstrap({"types": {}}),
		with_bootstrap({"types": ["aws_iam_role"]}),
		with_type("aws_s3_bucket_versioning", {"class": "guarded"}),
		with_type("aws_s3_bucket_versioning", {"class": "guarded", "parent": "aws_s3_bucket", "ref": "bucket", "parent_ref": "id"}),
		with_type("aws_s3_bucket_versioning", {"class": "covered_by", "parent": "aws_s3_bucket"}),
		with_type("aws_s3_bucket_versioning", cov("", "bucket", "id")),
		with_type("aws_s3_bucket_versioning", object.union(cov("aws_s3_bucket", "bucket", "id"), {"extra": true})),
		with_type("aws_s3_bucket_versioning", {"class": "exclude", "reason": "x"}),
		with_type("aws_s3_bucket", {"class": "target", "children": "true"}),
		with_type("aws_s3_bucket", {"class": "target", "child": true}),
		with_type("aws_kms_key", {"class": "key", "children": true}),
		with_bootstrap({"children": []}),
		with_child(state_bucket, true),
		with_child(state_bucket, {}),
		with_child(state_bucket, {"roots": "true", "version_deletion": true}),
		with_child(state_bucket, {"roots": true, "version_deletion": "true"}),
		with_child(state_bucket, {"roots": true, "version_deletions": true}),
		with_child(state_bucket, {"root": true, "version_deletion": true}),
		with_child(state_bucket, {"version_deletion": true}),
		with_child(state_bucket, {"roots": false, "version_deletion": true}),
		with_child(state_bucket, {"roots": "true", "suffixes": ["/*"], "version_deletion": true}),
		with_child(plan_bucket, {"suffixes": "/*"}),
		with_child(plan_bucket, {"suffixes": [""]}),
		with_child(plan_bucket, {"suffixes": [1]}),
		with_child(plan_bucket, {"suffixes": []}),
		with_child(plan_bucket, {"suffix": ["/*"]}),
	] {
		fixture.has(malformed, messages_for(world(valid_statements), d))
	}
}

test_violation_bootstrap_missing if {
	d := json.patch(decl, [{"op": "remove", "path": "/deployment_identity/apply_role/bootstrap"}])
	fixture.has(malformed, messages_for(world(valid_statements), d))
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

# 鍵が消えると組が無くなり、PLAN-APPLY-01 は何も見なくなる。
test_violation_key_type_absent if {
	rcs := without(without(without(world(valid_statements), "module.state_backend.module.kms.aws_kms_key.this"), key_policy_address), "module.state_backend.module.kms.aws_kms_alias.this")
	fixture.has("[PLAN-APPLY-00] bootstrap の構成物の型 aws_kms_key", messages_for(rcs, decl))
}

# mock provider は override の無い computed な属性に乱数を入れる。ARN の形でない値を対象として数えない。
test_violation_target_arn_not_arn if {
	every v in [{"id": "po-id"}, {"arn": "", "id": "po-id"}, {"arn": "Zq8x1", "id": "po-id"}] {
		rcs := with_after(world(valid_statements), plan_bucket, v)
		fixture.has(sprintf("[PLAN-APPLY-00] bootstrap の構成物 %s の ARN", [plan_bucket]), messages_for(rcs, decl))
	}
}

test_violation_children_address_absent if {
	d := with_child("module.state_backend.module.gone.aws_s3_bucket.this", {"suffixes": ["/*"]})
	fixture.has("[PLAN-APPLY-00] data.policy_plan.deployment_identity.apply_role.bootstrap.children の module.state_backend.module.gone", messages_for(world(valid_statements), d))
}

test_violation_children_on_non_target if {
	address := "module.state_backend.module.kms.aws_kms_key.this"
	fixture.has(sprintf("[PLAN-APPLY-00] data.policy_plan.deployment_identity.apply_role.bootstrap.children の %s は", [address]), messages_for(world(valid_statements), with_child(address, {"suffixes": ["/x"]})))
}

test_violation_no_writer_roots if {
	every roots in [[{"writer": "apply_role", "state": "workload/terraform.tfstate"}], []] {
		d := json.patch(decl, [{"op": "replace", "path": "/state_backend/roots", "value": roots}])
		fixture.has("[PLAN-APPLY-00] data.policy_plan.state_backend.roots に", messages_for(world(valid_statements), d))
	}
}

test_violation_roots_missing if {
	fixture.has("[PLAN-APPLY-00] data.policy_plan.state_backend.roots に", messages_for(world(valid_statements), json.patch(decl, [{"op": "remove", "path": "/state_backend/roots"}])))
}

# --- PLAN-APPLY-01 ---

test_violation_delegated_action_not_denied if {
	msgs := messages(replace(valid_statements, 6, {"Effect": "Deny", "Action": "kms:PutKeyPolicy", "Resource": key}))
	fixture.has(sprintf("[PLAN-APPLY-01] p.json: %s がアカウントへ委任する [\"kms:describekey\"]", [key_policy_address]), msgs)
}

# 鍵の ARN でない Resource（別の鍵、"*"、alias）への Deny は、その鍵を守る Deny として数えない。
test_violation_deny_not_on_key if {
	every resource in [other_key, "*", sprintf("arn:aws:kms:ap-northeast-1:%s:alias/x", [account])] {
		fixture.has("[PLAN-APPLY-01]", messages(replace(valid_statements, 6, {"Effect": "Deny", "Action": delegated_actions, "Resource": resource})))
	}
}

test_violation_conditional_key_deny if {
	s := {"Effect": "Deny", "Action": delegated_actions, "Resource": key, "Condition": {"Bool": {"aws:MultiFactorAuthPresent": "false"}}}
	fixture.has("[PLAN-APPLY-01]", messages(replace(valid_statements, 6, s)))
}

# --- PLAN-APPLY-03 ---

test_violation_unclassified_type if {
	rcs := array.concat(world(valid_statements), [res("aws_cloudwatch_log_group", "module.state_backend.module.logs.aws_cloudwatch_log_group.this", {"arn": "arn:aws:logs:ap-northeast-1:123456789012:log-group:x"})])
	fixture.has("[PLAN-APPLY-03] bootstrap の構成物 module.state_backend.module.logs.aws_cloudwatch_log_group.this の型", messages_for(rcs, decl))
}

# 子は、bootstrap の親を値で指さなければ親の ARN の Deny に守られない。
test_violation_covered_by_points_outside if {
	every c in [
		["module.state_backend.module.s3.aws_s3_bucket_versioning.this", {"bucket": "other-bucket"}],
		["module.state_backend.module.s3.aws_s3_bucket_versioning.this", {}],
		[key_policy_address, {"key_id": "other-key", "policy": json.marshal(key_policy_body)}],
		["module.state_backend.module.kms.aws_kms_alias.this", {"target_key_id": "other-key"}],
		[apply_policy_address, {"name": "apply", "role": "external-role", "policy": json.marshal({"Statement": valid_statements})}],
	] {
		fixture.has(sprintf("[PLAN-APPLY-03] %s の", [c[0]]), messages_for(with_after(world(valid_statements), c[0], c[1]), decl))
	}
}

# 親として宣言した型が target でも key でもなければ、Deny は親を守らない。
test_violation_covered_by_parent_not_guarded if {
	d := with_type("aws_s3_bucket_versioning", cov("aws_iam_role_policy", "bucket", "role"))
	rcs := with_after(world(valid_statements), "module.state_backend.module.s3.aws_s3_bucket_versioning.this", {"bucket": "ci-apply"})
	fixture.has("[PLAN-APPLY-03] module.state_backend.module.s3.aws_s3_bucket_versioning.this の bucket", messages_for(rcs, d))
}

# 3つ目の Bucket は、子の ARN を宣言するまで赤にする。宣言が無いと、その object を守る Deny を誰も求めない。
test_violation_children_undeclared if {
	extra := "arn:aws:s3:::third"
	s := array.concat(valid_statements, [full_deny([extra])])
	rcs := array.concat(world(s), [res("aws_s3_bucket", "module.state_backend.module.third.aws_s3_bucket.this", {"arn": extra, "id": "third"})])
	fixture.has("[PLAN-APPLY-03] module.state_backend.module.third.aws_s3_bucket.this の子の ARN", messages_for(rcs, decl))
}

# --- PLAN-APPLY-04 ---

# サービスの wildcard は同じ ARN への別のサービスの Action（role の ARN への sts:AssumeRole）を覆わない。
test_violation_service_wildcard if {
	msgs := messages(replace(valid_statements, 1, {"Effect": "Deny", "Action": "iam:*", "Resource": array.concat(role_arns, [provider_arn])}))
	every arn in array.concat(role_arns, [provider_arn]) {
		fixture.has(denied_03(arn), msgs)
	}
}

test_violation_action_prefix_list if {
	fixture.has(denied_03(st), messages(replace(valid_statements, 3, {"Effect": "Deny", "Action": ["s3:Put*", "s3:Delete*"], "Resource": st})))
}

test_violation_conditional_deny if {
	s := replace(valid_statements, 5, object.union(full_deny([po, sprintf("%s/*", [po])]), {"Condition": {"StringEquals": {"aws:RequestedRegion": "ap-northeast-1"}}}))
	fixture.has(denied_03(po), messages(s))
}

test_violation_not_resource if {
	s := replace(valid_statements, 5, {"Effect": "Deny", "Action": "*", "Resource": [po, sprintf("%s/*", [po])], "NotResource": "arn:aws:s3:::other"})
	fixture.has(denied_03(po), messages(s))
}

# Resource は文字列の完全一致で見る。pattern は評価しない。
test_violation_resource_pattern if {
	fixture.has(denied_03(po), messages(replace(valid_statements, 5, full_deny(["arn:aws:s3:::p*"]))))
}

test_violation_not_action_names_non_read if {
	every names in [["s3:ListBucket", "s3:PutBucketPolicy"], ["s3:Get*"], ["s3:ListBucket", "s3:TagResource"]] {
		fixture.has(denied_03(st), messages(replace(valid_statements, 3, {"Effect": "Deny", "NotAction": names, "Resource": st})))
	}
}

# Action と NotAction を両方持つ文は IAM が受け付けない。どちらの形としても数えない。
test_violation_action_and_not_action if {
	every s in [
		{"Effect": "Deny", "Action": "*", "NotAction": ["s3:ListBucket"], "Resource": st},
		{"Effect": "Deny", "Action": "s3:ListBucket", "NotAction": ["s3:ListBucket"], "Resource": st},
	] {
		fixture.has(denied_03(st), messages(replace(valid_statements, 3, s)))
	}
}

test_violation_allow_is_not_deny if {
	fixture.has(denied_03(po), messages(replace(valid_statements, 5, {"Effect": "Allow", "Action": "*", "Resource": [po, sprintf("%s/*", [po])]})))
}

test_violation_identity_state_uncovered if {
	every i in [0, 1] {
		msgs := messages(replace(valid_statements, 2, full_deny([identity_keys[1 - i]])))
		fixture.has(denied_03_strict(identity_keys[i]), msgs)
	}
}

test_violation_identity_state_read_opened if {
	s := replace(valid_statements, 2, {"Effect": "Deny", "NotAction": ["s3:GetObject"], "Resource": identity_keys})
	fixture.has(denied_03_strict(identity_keys[0]), messages(s))
}

test_violation_plan_output_objects_uncovered if {
	fixture.has(denied_03(sprintf("%s/*", [po])), messages(replace(valid_statements, 5, full_deny([po]))))
}

test_violation_new_role_uncovered if {
	rcs := array.concat(world(valid_statements), [res("aws_iam_role", sprintf("%saws_iam_role.this[\"extra\"]", [g]), {"name": "ci-extra", "arn": role_arn("ci-extra")})])
	fixture.has(denied_03(role_arn("ci-extra")), messages_for(rcs, decl))
}

# 別の instance（plan の role）の Deny は、apply の role の Deny として数えない。
test_violation_other_instance_deny if {
	rcs := array.concat(without(world(remove(valid_statements, 5)), sprintf("%saws_iam_role_policy.this[\"plan\"]", [g])), [role_policy("plan", "ci-plan", valid_statements)])
	fixture.has(denied_03(po), messages_for(rcs, decl))
}

# --- PLAN-APPLY-05 ---

# 版の削除の Deny は Action を名前か * で書く。s3:* と s3:Delete* は IAM では覆うが、wildcard を展開しないので数えない。
test_violation_version_deletion_not_named if {
	every s in [
		remove(valid_statements, 4),
		replace(valid_statements, 4, {"Effect": "Deny", "Action": "s3:*", "Resource": sprintf("%s/*", [st])}),
		replace(valid_statements, 4, {"Effect": "Deny", "Action": "s3:Delete*", "Resource": sprintf("%s/*", [st])}),
		replace(valid_statements, 4, {"Effect": "Deny", "Action": "s3:DeleteObjectVersion", "Resource": sprintf("%s/*", [st]), "Condition": {"Bool": {"aws:SecureTransport": "true"}}}),
	] {
		fixture.has(sprintf("[PLAN-APPLY-05] apply の role %q が、%s/* への", [apply_policy_address, st]), messages(s))
	}
}

# --- PLAN-APPLY-06 ---

test_violation_extra_inline_policy if {
	extra := res("aws_iam_role_policy", sprintf("%saws_iam_role_policy.extra", [g]), {"name": "extra", "role": "ci-apply", "policy": json.marshal({"Statement": [{"Effect": "Allow", "Action": "s3:GetObject", "Resource": "*"}]})})
	fixture.has(sprintf("[PLAN-APPLY-06] p.json: %saws_iam_role_policy.extra が apply の role に", [g]), messages_for(array.concat(world(valid_statements), [extra]), decl))
}

test_violation_managed_policy_attachment if {
	extra := res("aws_iam_role_policy_attachment", sprintf("%saws_iam_role_policy_attachment.extra", [g]), {"role": "ci-apply", "policy_arn": "arn:aws:iam::aws:policy/AdministratorAccess"})
	fixture.has(sprintf("[PLAN-APPLY-06] p.json: %saws_iam_role_policy_attachment.extra が apply の role に", [g]), messages_for(array.concat(world(valid_statements), [extra]), decl))
}

# role の引数と aws_iam_policy_attachment は、上の型を通らずに policy を付ける。
test_violation_role_arguments_attach_policy if {
	every v in [
		{"inline_policy": [{"name": "x", "policy": "{}"}], "managed_policy_arns": []},
		{"inline_policy": [], "managed_policy_arns": ["arn:aws:iam::aws:policy/AdministratorAccess"]},
	] {
		rcs := with_after(world(valid_statements), apply_role_address, object.union({"name": "ci-apply", "arn": role_arn("ci-apply")}, v))
		fixture.has(sprintf("[PLAN-APPLY-06] p.json: apply の role %s の", [apply_role_address]), messages_for(rcs, decl))
	}
}

test_violation_role_arguments_unknown if {
	unknown := json.patch([r | some r in world(valid_statements); r.address == apply_role_address][0], [{"op": "add", "path": "/change/after_unknown/managed_policy_arns", "value": [true]}])
	fixture.has(sprintf("[PLAN-APPLY-06] p.json: apply の role %s の managed_policy_arns", [apply_role_address]), messages_for(array.concat(without(world(valid_statements), apply_role_address), [unknown]), decl))
}

test_violation_policy_attachment_roles if {
	extra := res("aws_iam_policy_attachment", "module.workload.aws_iam_policy_attachment.this", {"roles": ["other", "ci-apply"], "policy_arn": "arn:aws:iam::aws:policy/AdministratorAccess"})
	fixture.has("[PLAN-APPLY-06] p.json: module.workload.aws_iam_policy_attachment.this が apply の role ci-apply に", messages_for(array.concat(world(valid_statements), [extra]), decl))
}

test_violation_policy_attachment_roles_unknown if {
	extra := {"address": "module.workload.aws_iam_policy_attachment.later", "type": "aws_iam_policy_attachment", "change": {"actions": ["create"], "after": {}, "after_unknown": {"roles": true}}}
	fixture.has("[PLAN-APPLY-06] p.json: module.workload.aws_iam_policy_attachment.later がどの role に", messages_for(array.concat(world(valid_statements), [extra]), decl))
}

test_violation_attachment_role_unknown if {
	every extra in [
		{"address": sprintf("%saws_iam_role_policy_attachment.later", [g]), "type": "aws_iam_role_policy_attachment", "change": {"actions": ["create"], "after": {}, "after_unknown": {"role": true}}},
		{"address": sprintf("%saws_iam_role_policies_exclusive.later", [g]), "type": "aws_iam_role_policies_exclusive", "change": {"actions": ["create"], "after": {}, "after_unknown": {"role_name": true}}},
	] {
		fixture.has(sprintf("[PLAN-APPLY-06] p.json: %s がどの role に", [extra.address]), messages_for(array.concat(world(valid_statements), [extra]), decl))
	}
}

test_violation_exclusive_count if {
	every e in exclusives {
		fixture.has(sprintf("[PLAN-APPLY-06] apply の role ci-apply の %s が 0 件です", [e.type]), messages_for(without(world(valid_statements), e.address), decl))
		twice := json.patch(e, [{"op": "replace", "path": "/address", "value": sprintf("%s.again", [e.address])}])
		fixture.has(sprintf("[PLAN-APPLY-06] apply の role ci-apply の %s が 2 件です", [e.type]), messages_for(array.concat(world(valid_statements), [twice]), decl))
	}
}

test_violation_exclusive_contents if {
	rcs := with_after(with_after(world(valid_statements), exclusives[0].address, {"role_name": "ci-apply", "policy_names": ["apply", "extra"]}), exclusives[1].address, {"role_name": "ci-apply", "policy_arns": ["arn:aws:iam::aws:policy/ReadOnlyAccess"]})
	msgs := messages_for(rcs, decl)
	fixture.has(sprintf("[PLAN-APPLY-06] p.json: %s の policy_names", [exclusives[0].address]), msgs)
	fixture.has(sprintf("[PLAN-APPLY-06] p.json: %s の policy_arns", [exclusives[1].address]), msgs)
}

# --- 2本目の鍵 ---

second_key := sprintf("arn:aws:kms:ap-northeast-1:%s:key/22222222-12ab-34cd-56ef-1234567890ab", [account])

second_key_policy_address := "module.state_backend.module.kms_second.aws_kms_key_policy.this"

second_key_resources := [
	res("aws_kms_key", "module.state_backend.module.kms_second.aws_kms_key.this", {"arn": second_key, "id": "second-id", "key_id": "second-kid"}),
	res("aws_kms_key_policy", second_key_policy_address, {"key_id": "second-id", "policy": json.marshal({"Statement": [
		{"Effect": "Allow", "Principal": {"AWS": sprintf("arn:aws:iam::%s:root", [account])}, "Action": ["kms:PutKeyPolicy", "kms:ScheduleKeyDeletion"], "Resource": "*"},
	]})}),
]

test_pass_second_key_denied if {
	s := array.concat(valid_statements, [{"Effect": "Deny", "Action": ["kms:PutKeyPolicy", "kms:ScheduleKeyDeletion"], "Resource": second_key}])
	count(messages_for(array.concat(world(s), second_key_resources), decl)) == 0
}

test_violation_second_key_not_denied if {
	s := replace(valid_statements, 6, {"Effect": "Deny", "Action": ["kms:DescribeKey", "kms:PutKeyPolicy", "kms:ScheduleKeyDeletion"], "Resource": key})
	fixture.has(sprintf("[PLAN-APPLY-01] p.json: %s がアカウントへ委任する", [second_key_policy_address]), messages_for(array.concat(world(s), second_key_resources), decl))
}

test_violation_key_policy_without_key if {
	orphan := json.patch(key_policy, [{"op": "replace", "path": "/address", "value": "module.state_backend.module.orphan.aws_kms_key_policy.this"}])
	fixture.has("[PLAN-APPLY-00] p.json: Key Policy module.state_backend.module.orphan.aws_kms_key_policy.this と同じ module の下の鍵が 0 件", messages_for(array.concat(world(valid_statements), [orphan]), decl))
}

test_violation_two_key_policies_one_key if {
	extra := json.patch(key_policy, [{"op": "replace", "path": "/address", "value": "module.state_backend.module.kms.aws_kms_key_policy.extra"}])
	fixture.has("[PLAN-APPLY-00] 鍵 module.state_backend.module.kms.aws_kms_key.this と同じ module の下の Key Policy（aws_kms_key_policy）が 2 件", messages_for(array.concat(world(valid_statements), [extra]), decl))
}

test_violation_two_keys_one_key_policy if {
	extra := res("aws_kms_key", "module.state_backend.module.kms.aws_kms_key.extra", {"arn": other_key, "id": "extra-id", "key_id": "extra-kid"})
	s := array.concat(valid_statements, [{"Effect": "Deny", "Action": delegated_actions, "Resource": other_key}])
	fixture.has(sprintf("[PLAN-APPLY-00] p.json: Key Policy %s と同じ module の下の鍵が 2 件", [key_policy_address]), messages_for(array.concat(world(s), [extra]), decl))
}

# --- 修正の回のレビューで見つかった、黙る分岐 ---

# identity root の state と lock file と版の削除の要求は、state Bucket の children だけが生む。
test_violation_state_bucket_children_weakened if {
	every c in [
		{"suffixes": ["/workload/terraform.tfstate"], "version_deletion": true},
		{"roots": true},
	] {
		fixture.has("[PLAN-APPLY-00] data.policy_plan.deployment_identity.apply_role.bootstrap.children の state Bucket", messages_for(world(valid_statements), with_child(state_bucket, c)))
	}
}

test_violation_roots_element_malformed if {
	every root in [
		{"writer": "other_role", "State": "other/terraform.tfstate"},
		{"writer": "other_role", "state": ""},
		{"writer": "other_role", "state": "other/terraform.tfstate", "lock": true},
		"identity/terraform.tfstate",
	] {
		d := json.patch(decl, [{"op": "add", "path": "/state_backend/roots/-", "value": root}])
		fixture.has("[PLAN-APPLY-00] data.policy_plan.state_backend.roots の要素", messages_for(world(valid_statements), d))
	}
}

test_violation_identity_state_conditional_deny if {
	every s in [
		object.union(full_deny(identity_keys), {"Condition": {"Bool": {"aws:SecureTransport": "true"}}}),
		object.union(full_deny(identity_keys), {"NotResource": "arn:aws:s3:::other"}),
		{"Effect": "Allow", "Action": "*", "Resource": identity_keys},
	] {
		fixture.has(denied_03_strict(identity_keys[0]), messages(replace(valid_statements, 2, s)))
	}
}

# 子が指す値は、宣言した親の型の、宣言した属性の値でなければならない（別の型の id や ARN では足りない）。
test_violation_covered_by_points_to_other_type if {
	every v in [{"bucket": "key-id"}, {"bucket": st}] {
		fixture.has("[PLAN-APPLY-03] module.state_backend.module.s3.aws_s3_bucket_versioning.this の", messages_for(with_after(world(valid_statements), "module.state_backend.module.s3.aws_s3_bucket_versioning.this", v), decl))
	}
}

test_violation_key_policy_points_to_other_key if {
	extra := res("aws_kms_key", "module.state_backend.module.kms_second.aws_kms_key.this", {"arn": other_key, "id": "second-id", "key_id": "second-kid"})
	extra_policy := res("aws_kms_key_policy", "module.state_backend.module.kms_second.aws_kms_key_policy.this", {"key_id": "key-id", "policy": json.marshal(key_policy_body)})
	s := array.concat(valid_statements, [{"Effect": "Deny", "Action": delegated_actions, "Resource": other_key}])
	fixture.has("[PLAN-APPLY-03] p.json: module.state_backend.module.kms_second.aws_kms_key_policy.this の key_id が", messages_for(array.concat(world(s), [extra, extra_policy]), decl))
}

test_violation_action_and_not_action_on_key_and_version if {
	fixture.has("[PLAN-APPLY-01]", messages(replace(valid_statements, 6, {"Effect": "Deny", "Action": ["*"], "NotAction": ["kms:PutKeyPolicy"], "Resource": key})))
	fixture.has("[PLAN-APPLY-05]", messages(replace(valid_statements, 4, {"Effect": "Deny", "Action": "s3:DeleteObjectVersion", "NotAction": ["s3:DeleteObjectVersion"], "Resource": sprintf("%s/*", [st])})))
}

test_violation_policy_attachment_roles_partially_unknown if {
	extra := {"address": "module.workload.aws_iam_policy_attachment.later", "type": "aws_iam_policy_attachment", "change": {"actions": ["create"], "after": {"roles": ["other"]}, "after_unknown": {"roles": [false, true]}}}
	fixture.has("[PLAN-APPLY-06] p.json: module.workload.aws_iam_policy_attachment.later がどの role に", messages_for(array.concat(world(valid_statements), [extra]), decl))
}

test_violation_policy_attachment_single_role if {
	extra := res("aws_iam_policy_attachment", "module.workload.aws_iam_policy_attachment.this", {"roles": ["ci-apply"], "policy_arn": "arn:aws:iam::aws:policy/AdministratorAccess"})
	fixture.has("[PLAN-APPLY-06] p.json: module.workload.aws_iam_policy_attachment.this が apply の role ci-apply に", messages_for(array.concat(world(valid_statements), [extra]), decl))
}

test_pass_policy_attachment_other_role if {
	extra := res("aws_iam_policy_attachment", "module.workload.aws_iam_policy_attachment.this", {"roles": ["other"], "policy_arn": "arn:aws:iam::aws:policy/AdministratorAccess"})
	count(messages_for(array.concat(world(valid_statements), [extra]), decl)) == 0
}

# inline_policy は block の一部だけが決まらないとき、入れ子の要素に true を持つ。
test_violation_role_inline_policy_nested_unknown if {
	unknown := json.patch([r | some r in world(valid_statements); r.address == apply_role_address][0], [{"op": "add", "path": "/change/after_unknown/inline_policy", "value": [{"name": false, "policy": true}]}])
	fixture.has(sprintf("[PLAN-APPLY-06] p.json: apply の role %s の inline_policy", [apply_role_address]), messages_for(array.concat(without(world(valid_statements), apply_role_address), [unknown]), decl))
}

test_violation_children_on_target_without_children if {
	address := sprintf("%saws_iam_openid_connect_provider.github", [g])
	fixture.has(sprintf("[PLAN-APPLY-00] data.policy_plan.deployment_identity.apply_role.bootstrap.children の %s は", [address]), messages_for(world(valid_statements), with_child(address, {"suffixes": ["/x"]})))
}

# --- Resource "*" で拒む Action ---

test_violation_wildcard_deny_missing if {
	msgs := messages(replace(valid_statements, 7, {"Effect": "Deny", "Action": "logs:PutAccountPolicy", "Resource": "*"}))
	violations := {m | some m in msgs; startswith(m, "[PLAN-APPLY-02]")}
	count(violations) == 1
	some m in violations
	contains(m, "logs:putresourcepolicy")
}

# Resource を名指した Deny と Condition の付いた Deny は、アカウント全体の要求を拒まない。
test_violation_wildcard_deny_not_on_everything if {
	every s in [
		{"Effect": "Deny", "Action": ["logs:PutAccountPolicy", "logs:PutResourcePolicy"], "Resource": "arn:aws:logs:ap-northeast-1:123456789012:log-group:x"},
		{"Effect": "Deny", "Action": ["logs:PutAccountPolicy", "logs:PutResourcePolicy"], "Resource": "*", "Condition": {"StringEquals": {"aws:RequestedRegion": "ap-northeast-1"}}},
		{"Effect": "Allow", "Action": ["logs:PutAccountPolicy", "logs:PutResourcePolicy"], "Resource": "*"},
	] {
		fixture.has("[PLAN-APPLY-02] apply の role", messages(replace(valid_statements, 7, s)))
	}
}

# 宣言の大文字小文字を区別しない。別の policy の Deny は apply の role を守らない。
test_violation_wildcard_deny_in_other_policy if {
	rcs := array.concat(without(world(remove(valid_statements, 7)), sprintf("%saws_iam_role_policy.this[\"plan\"]", [g])), [role_policy("plan", "ci-plan", [wildcard_deny])])
	fixture.has("[PLAN-APPLY-02] apply の role", messages_for(rcs, decl))
}

test_violation_wildcard_denies_empty if {
	d := json.patch(decl, [{"op": "replace", "path": "/deployment_identity/apply_role/wildcard_denies", "value": []}])
	fixture.has("[PLAN-APPLY-00] data.policy_plan.deployment_identity.apply_role.wildcard_denies", messages_for(world(valid_statements), d))
}

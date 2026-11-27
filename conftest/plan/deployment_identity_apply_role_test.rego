package plan.deployment_identity_apply_role_test

import data.plan.deployment_identity_apply_role as p
import data.plan.fixture

g := "module.deployment_identity.module.github_oidc."

account := "123456789012"

role_arn(name) := sprintf("arn:aws:iam::%s:role/%s", [account, name])

provider_arn := sprintf("arn:aws:iam::%s:oidc-provider/token.actions.githubusercontent.com", [account])

st := "arn:aws:s3:::st"

po := "arn:aws:s3:::po"

ao := "arn:aws:s3:::ao"

ao_objects := sprintf("%s/*", [ao])

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
	full_deny([ao]),
	{"Effect": "Deny", "NotAction": ["s3:PutObject"], "Resource": ao_objects},
	governance_deny,
	management_deny,
]

governance_actions := ["organizations:Create*", "sts:AssumeRoot"]

management_actions := ["iam:UpdateAccountPasswordPolicy"]

governance_deny := {"Effect": "Deny", "Action": governance_actions, "Resource": "*"}

management_deny := {"Effect": "Deny", "Action": management_actions, "Resource": "*"}

management_index := 10

governance_index := 9

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
	res("aws_s3_bucket", "module.state_backend.module.apply_output.aws_s3_bucket.this", {"arn": ao, "id": "ao-id"}),
	res("aws_kms_key", "module.state_backend.module.kms.aws_kms_key.this", {"arn": key, "id": "key-id", "key_id": "key-kid"}),
	res("aws_kms_alias", "module.state_backend.module.kms.aws_kms_alias.this", {"target_key_id": "key-kid"}),
	key_policy,
]

# 入力は管理アカウントの reference の plan なので、統制の policy と前提の service-linked role を既定で持つ。
world(statements) := array.concat(array.concat(array.concat(array.concat(role_resources, others), exclusives), siblings), [apply_policy(statements), governance_policy_rc, slr(slr_address)])

cov(parent, ref, parent_ref) := {"class": "covered_by", "parent": parent, "ref": ref, "parent_ref": parent_ref}

bootstrap := {
	"modules": ["module.state_backend.", "module.deployment_identity."],
	"types": {
		"aws_iam_openid_connect_provider": {"class": "target"},
		"aws_iam_role": {"class": "target"},
		"aws_iam_role_policies_exclusive": cov("aws_iam_role", "role_name", "name"),
		"aws_iam_role_policy": cov("aws_iam_role", "role", "name"),
		"aws_iam_role_policy_attachment": cov("aws_iam_role", "role", "name"),
		"aws_iam_role_policy_attachments_exclusive": cov("aws_iam_role", "role_name", "name"),
		"aws_iam_service_linked_role": {"class": "service_linked"},
		"aws_kms_alias": cov("aws_kms_key", "target_key_id", "key_id"),
		"aws_kms_key": {"class": "key"},
		"aws_kms_key_policy": cov("aws_kms_key", "key_id", "id"),
		"aws_s3_bucket": {"class": "target", "children": true},
		"aws_s3_bucket_versioning": cov("aws_s3_bucket", "bucket", "id"),
	},
	"children": {
		"module.state_backend.module.s3.aws_s3_bucket.this": {"roots": true, "version_deletion": true},
		"module.state_backend.module.plan_output.aws_s3_bucket.this": {"suffixes": ["/*"]},
		"module.state_backend.module.apply_output.aws_s3_bucket.this": {"suffixes": ["/*"], "writes": ["s3:PutObject"]},
	},
	"prerequisite_module": "module.deployment_identity.",
	"prerequisite_services": [slr_service],
}

decl := {
	"deployment_identity": {"apply_role": {
		"role": apply_role_address,
		"policy": apply_policy_address,
		"governance": {"actions": governance_actions, "management_actions": management_actions, "management_policy": governance_policy_address},
		"bootstrap": bootstrap,
	}},
	"state_backend": {
		"bucket": "module.state_backend.module.s3.aws_s3_bucket.this",
		"roots": [
			{"writer": "apply_identity_role", "state": "identity/terraform.tfstate", "unlocker": "unlock_role"},
			{"writer": "apply_role", "state": "workload/terraform.tfstate", "unlocker": "workload_unlock_role"},
		],
	},
}

# object.union は object を再帰的に重ねるので、差し替えるキーを先に外す。
with_bootstrap(patch) := json.patch(decl, [{"op": "add", "path": "/deployment_identity/apply_role/bootstrap", "value": object.union(object.remove(bootstrap, object.keys(patch)), patch)}])

with_type(type, t) := with_bootstrap({"types": object.union(object.remove(bootstrap.types, [type]), {type: t})})

with_child(address, c) := with_bootstrap({"children": object.union(object.remove(bootstrap.children, [address]), {address: c})})

state_bucket := "module.state_backend.module.s3.aws_s3_bucket.this"

plan_bucket := "module.state_backend.module.plan_output.aws_s3_bucket.this"

apply_bucket := "module.state_backend.module.apply_output.aws_s3_bucket.this"

# 宣言は丸ごと差し替える（理由は lib/fixture.rego の inline_declaration）。
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

denied_03(arn) := sprintf("[PLAN-APPLY-03] apply の role %q が、%s へのすべての Action（* か", [apply_policy_address, arn])

denied_03_writes(arn) := sprintf("[PLAN-APPLY-03] apply の role %q が、%s への [\"s3:putobject\"] のほかのすべての Action を、条件なしの Deny（NotAction）で拒んでいません", [apply_policy_address, arn])

denied_03_writes_denied(arn, a) := sprintf("[PLAN-APPLY-03] apply の role %q が、書き込み先として宣言した %s の s3:putobject を、Deny の Action %q で拒んでいます", [apply_policy_address, arn, a])

denied_03_strict(arn) := sprintf("[PLAN-APPLY-03] apply の role %q が、%s へのすべての Action（*）", [apply_policy_address, arn])

malformed := "[PLAN-APPLY-00] data.policy_plan.deployment_identity.apply_role.bootstrap が"

# --- 通る入力 ---

test_pass_valid if {
	count(messages(valid_statements)) == 0
}

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
		with_bootstrap({"prerequisite_module": ""}),
		with_bootstrap({"prerequisite_module": 1}),
		with_bootstrap({"prerequisite_module": "module.ops_notification."}),
		json.patch(decl, [{"op": "remove", "path": "/deployment_identity/apply_role/bootstrap/prerequisite_module"}]),
		with_bootstrap({"prerequisite_services": []}),
		with_bootstrap({"prerequisite_services": slr_service}),
		with_bootstrap({"prerequisite_services": [""]}),
		with_bootstrap({"prerequisite_services": [1]}),
		with_bootstrap({"prerequisite_services": [slr_service, null]}),
		json.patch(decl, [{"op": "remove", "path": "/deployment_identity/apply_role/bootstrap/prerequisite_services"}]),
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
		with_child(apply_bucket, {"writes": ["s3:PutObject"]}),
		with_child(apply_bucket, {"suffixes": ["/*"], "writes": []}),
		with_child(apply_bucket, {"suffixes": ["/*"], "writes": "s3:PutObject"}),
		with_child(apply_bucket, {"suffixes": ["/*"], "writes": [""]}),
		with_child(apply_bucket, {"suffixes": ["/*"], "writes": [1]}),
		with_child(apply_bucket, {"suffixes": ["/*"], "writes": ["s3:GetObject"]}),
		with_child(apply_bucket, {"suffixes": ["/*"], "writes": ["s3:PutObject", "S3:ListBucket"]}),
		with_child(apply_bucket, {"suffixes": ["/*"], "writes": ["s3:*"]}),
		with_child(apply_bucket, {"suffixes": ["/*"], "writes": ["*"]}),
		with_child(apply_bucket, {"suffixes": ["/*"], "writes": ["s3:Pu?Object"]}),
		with_child(apply_bucket, {"suffixes": ["/*"], "writes": ["s3:PutObject "]}),
		with_child(apply_bucket, {"suffixes": ["/*"], "writes": null}),
		with_child(apply_bucket, {"suffixes": ["/*"], "writes": {"s3:PutObject": true}}),
		with_child(apply_bucket, {"suffixes": ["/*"], "writes": [["s3:PutObject"]]}),
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
	d := with_bootstrap({"modules": ["module.state_backend.", "module.renamed."], "prerequisite_module": "module.renamed."})
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
	every roots in [[{"writer": "apply_role", "state": "workload/terraform.tfstate", "unlocker": "workload_unlock_role"}], []] {
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

test_violation_deny_not_on_key if {
	every resource in [other_key, "*", sprintf("arn:aws:kms:ap-northeast-1:%s:alias/x", [account])] {
		fixture.has("[PLAN-APPLY-01]", messages(replace(valid_statements, 6, {"Effect": "Deny", "Action": delegated_actions, "Resource": resource})))
	}
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

test_violation_covered_by_points_outside if {
	every c in [
		["module.state_backend.module.s3.aws_s3_bucket_versioning.this", {"bucket": "other-bucket"}],
		["module.state_backend.module.s3.aws_s3_bucket_versioning.this", {}],
		[key_policy_address, {"key_id": "other-key", "policy": json.marshal(key_policy_body)}],
		["module.state_backend.module.kms.aws_kms_alias.this", {"target_key_id": "other-key"}],
		[apply_policy_address, {"name": "apply", "role": "external-role", "policy": json.marshal({"Statement": valid_statements})}],
	] {
		fixture.has(sprintf("[PLAN-APPLY-02] %s の", [c[0]]), messages_for(with_after(world(valid_statements), c[0], c[1]), decl))
	}
}

test_violation_covered_by_parent_not_guarded if {
	d := with_type("aws_s3_bucket_versioning", cov("aws_iam_role_policy", "bucket", "role"))
	rcs := with_after(world(valid_statements), "module.state_backend.module.s3.aws_s3_bucket_versioning.this", {"bucket": "ci-apply"})
	fixture.has("[PLAN-APPLY-02] module.state_backend.module.s3.aws_s3_bucket_versioning.this の bucket", messages_for(rcs, d))
}

# 3つ目の Bucket は、子の ARN を宣言するまで赤にする。宣言が無いと、その object を守る Deny を誰も求めない。
test_violation_children_undeclared if {
	extra := "arn:aws:s3:::third"
	s := array.concat(valid_statements, [full_deny([extra])])
	rcs := array.concat(world(s), [res("aws_s3_bucket", "module.state_backend.module.third.aws_s3_bucket.this", {"arn": extra, "id": "third"})])
	fixture.has("[PLAN-APPLY-02] module.state_backend.module.third.aws_s3_bucket.this の子の ARN", messages_for(rcs, decl))
}

# --- PLAN-APPLY-03 ---

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

test_pass_resource_pattern if {
	count(messages(replace(valid_statements, 5, full_deny(["arn:aws:s3:::p*", sprintf("%s/*", [po])])))) == 0
}

test_pass_ci_role_pattern if {
	s := replace(valid_statements, 1, full_deny([sprintf("arn:aws:iam::%s:role/ci-*", [account]), provider_arn]))
	count(messages(s)) == 0
}

test_violation_resource_pattern if {
	fixture.has(denied_03(po), messages(replace(valid_statements, 5, full_deny(["arn:aws:s3:::q*"]))))
}

test_violation_ci_role_pattern_misses_role if {
	s := replace(valid_statements, 1, full_deny([sprintf("arn:aws:iam::%s:role/ci-plan*", [account]), provider_arn]))
	fixture.has(denied_03(role_arn("ci-apply")), messages(s))
}

test_violation_resource_pattern_strict_target if {
	msgs := messages(replace(valid_statements, 2, full_deny([sprintf("%s/identity/*", [st])])))
	fixture.has(denied_03_strict(identity_keys[0]), msgs)
	fixture.has(denied_03_strict(identity_keys[1]), msgs)
}

test_violation_resource_pattern_strict_lock_file if {
	msgs := messages(replace(valid_statements, 2, full_deny([identity_keys[0], sprintf("%s/identity/terraform.tfstate.tfloc?", [st])])))
	fixture.has(denied_03_strict(identity_keys[1]), msgs)
}

test_violation_resource_pattern_on_literal_wildcard_target if {
	every r in ["arn:aws:s3:::po/?", "arn:aws:s3:::po/x*"] {
		msgs := messages(replace(valid_statements, 5, full_deny([po, r])))
		fixture.has(denied_03(sprintf("%s/*", [po])), msgs)
	}
}

test_violation_resource_wildcard_alone if {
	msgs := messages(replace(valid_statements, 5, full_deny(["*"])))
	fixture.has(denied_03(po), msgs)
	fixture.has(denied_03(sprintf("%s/*", [po])), msgs)
}

test_violation_resource_wildcard_mixed if {
	msgs := messages(replace(valid_statements, 5, full_deny(["*", po])))
	fixture.has(denied_03(sprintf("%s/*", [po])), msgs)
	not fixture.has(denied_03(po), msgs)
}

test_pass_role_wildcard_pattern if {
	count(messages(replace(valid_statements, 1, full_deny([sprintf("arn:aws:iam::%s:role/*", [account]), provider_arn])))) == 0
}

test_pass_single_char_pattern if {
	s := replace(valid_statements, 1, full_deny(array.concat([role_arn(n) | some n in ["ci-apply", "ci-unlock", "ci-apply-identity"]], [role_arn("ci-pla?"), provider_arn])))
	count(messages(s)) == 0
}

test_violation_role_pattern_other_account if {
	msgs := messages(replace(valid_statements, 1, full_deny(["arn:aws:iam::999999999999:role/ci-*", provider_arn])))
	fixture.has(denied_03(role_arn("ci-plan")), msgs)
	fixture.has(denied_03(role_arn("ci-apply")), msgs)
}

test_violation_version_deletion_pattern if {
	s := replace(valid_statements, 4, {"Effect": "Deny", "Action": "s3:DeleteObjectVersion", "Resource": "arn:aws:s3:::s*/*"})
	fixture.has(sprintf("[PLAN-APPLY-04] apply の role %q が、%s/* への", [apply_policy_address, st]), messages(s))
}

test_violation_key_deny_pattern if {
	s := replace(valid_statements, 6, {"Effect": "Deny", "Action": delegated_actions, "Resource": sprintf("arn:aws:kms:*:%s:key/*", [account])})
	some m in messages(s)
	startswith(m, "[PLAN-APPLY-01] ")
	contains(m, sprintf(" %s がアカウントへ委任する ", [key_policy_address]))
	contains(m, sprintf("が鍵の ARN %v への Deny で拒んでいません", [key]))
}

test_violation_writes_target_pattern if {
	s := replace(valid_statements, 8, {"Effect": "Deny", "NotAction": ["s3:PutObject"], "Resource": "arn:aws:s3:::a*/*"})
	fixture.has(denied_03_writes(ao_objects), messages(s))
}

test_violation_resource_universal_pattern if {
	every r in ["**", "?*", "arn:*", "arn:aws:*", "arn:aws:s3:*"] {
		msgs := messages(replace(valid_statements, 5, full_deny([r, sprintf("%s/*", [po])])))
		fixture.has(denied_03(po), msgs)
	}
}

test_violation_not_action_names_non_read if {
	every names in [["s3:ListBucket", "s3:PutBucketPolicy"], ["s3:Get*"], ["s3:ListBucket", "s3:TagResource"]] {
		fixture.has(denied_03(st), messages(replace(valid_statements, 3, {"Effect": "Deny", "NotAction": names, "Resource": st})))
	}
}

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

test_violation_other_instance_deny if {
	rcs := array.concat(without(world(remove(valid_statements, 5)), sprintf("%saws_iam_role_policy.this[\"plan\"]", [g])), [role_policy("plan", "ci-plan", valid_statements)])
	fixture.has(denied_03(po), messages_for(rcs, decl))
}

# --- PLAN-APPLY-04 ---

# 版の削除の Deny は Action を名前か * で書く。s3:* と s3:Delete* は IAM では覆うが、wildcard を展開しないので数えない。
test_violation_version_deletion_not_named if {
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
	fixture.has(sprintf("[PLAN-APPLY-05] p.json: %saws_iam_role_policy.extra が apply の role に", [g]), messages_for(array.concat(world(valid_statements), [extra]), decl))
}

test_violation_managed_policy_attachment if {
	extra := res("aws_iam_role_policy_attachment", sprintf("%saws_iam_role_policy_attachment.extra", [g]), {"role": "ci-apply", "policy_arn": "arn:aws:iam::aws:policy/AdministratorAccess"})
	fixture.has(sprintf("[PLAN-APPLY-05] p.json: %saws_iam_role_policy_attachment.extra が apply の role に", [g]), messages_for(array.concat(world(valid_statements), [extra]), decl))
}

test_violation_role_arguments_attach_policy if {
	every v in [
		{"inline_policy": [{"name": "x", "policy": "{}"}], "managed_policy_arns": []},
		{"inline_policy": [], "managed_policy_arns": ["arn:aws:iam::aws:policy/AdministratorAccess"]},
	] {
		rcs := with_after(world(valid_statements), apply_role_address, object.union({"name": "ci-apply", "arn": role_arn("ci-apply")}, v))
		fixture.has(sprintf("[PLAN-APPLY-05] p.json: apply の role %s の", [apply_role_address]), messages_for(rcs, decl))
	}
}

test_violation_role_arguments_unknown if {
	unknown := json.patch([r | some r in world(valid_statements); r.address == apply_role_address][0], [{"op": "add", "path": "/change/after_unknown/managed_policy_arns", "value": [true]}])
	fixture.has(sprintf("[PLAN-APPLY-05] p.json: apply の role %s の managed_policy_arns", [apply_role_address]), messages_for(array.concat(without(world(valid_statements), apply_role_address), [unknown]), decl))
}

test_violation_policy_attachment_roles if {
	extra := res("aws_iam_policy_attachment", "module.workload.aws_iam_policy_attachment.this", {"roles": ["other", "ci-apply"], "policy_arn": "arn:aws:iam::aws:policy/AdministratorAccess"})
	fixture.has("[PLAN-APPLY-05] p.json: module.workload.aws_iam_policy_attachment.this が apply の role ci-apply に", messages_for(array.concat(world(valid_statements), [extra]), decl))
}

test_violation_policy_attachment_roles_unknown if {
	extra := {"address": "module.workload.aws_iam_policy_attachment.later", "type": "aws_iam_policy_attachment", "change": {"actions": ["create"], "after": {}, "after_unknown": {"roles": true}}}
	fixture.has("[PLAN-APPLY-05] p.json: module.workload.aws_iam_policy_attachment.later がどの role に", messages_for(array.concat(world(valid_statements), [extra]), decl))
}

test_violation_attachment_role_unknown if {
	every extra in [
		{"address": sprintf("%saws_iam_role_policy_attachment.later", [g]), "type": "aws_iam_role_policy_attachment", "change": {"actions": ["create"], "after": {}, "after_unknown": {"role": true}}},
		{"address": sprintf("%saws_iam_role_policies_exclusive.later", [g]), "type": "aws_iam_role_policies_exclusive", "change": {"actions": ["create"], "after": {}, "after_unknown": {"role_name": true}}},
	] {
		fixture.has(sprintf("[PLAN-APPLY-05] p.json: %s がどの role に", [extra.address]), messages_for(array.concat(world(valid_statements), [extra]), decl))
	}
}

test_violation_exclusive_count if {
	every e in exclusives {
		fixture.has(sprintf("[PLAN-APPLY-05] apply の role ci-apply の %s が 0 件です", [e.type]), messages_for(without(world(valid_statements), e.address), decl))
		twice := json.patch(e, [{"op": "replace", "path": "/address", "value": sprintf("%s.again", [e.address])}])
		fixture.has(sprintf("[PLAN-APPLY-05] apply の role ci-apply の %s が 2 件です", [e.type]), messages_for(array.concat(world(valid_statements), [twice]), decl))
	}
}

test_violation_exclusive_contents if {
	rcs := with_after(with_after(world(valid_statements), exclusives[0].address, {"role_name": "ci-apply", "policy_names": ["apply", "extra"]}), exclusives[1].address, {"role_name": "ci-apply", "policy_arns": ["arn:aws:iam::aws:policy/ReadOnlyAccess"]})
	msgs := messages_for(rcs, decl)
	fixture.has(sprintf("[PLAN-APPLY-05] p.json: %s の policy_names", [exclusives[0].address]), msgs)
	fixture.has(sprintf("[PLAN-APPLY-05] p.json: %s の policy_arns", [exclusives[1].address]), msgs)
}

workload_policy_address := sprintf("%saws_iam_policy.workload", [g])

workload_policy_arn := sprintf("arn:aws:iam::%s:policy/ci-apply-workload", [account])

with_workload_decl := json.patch(with_type("aws_iam_policy", {"class": "target"}), [{"op": "add", "path": "/deployment_identity/workload", "value": {"policy": workload_policy_address, "service_linked_roles": [slr_service]}}])

workload_world(arns) := array.concat(
	with_after(world(array.concat(valid_statements, [full_deny([workload_policy_arn])])), exclusives[1].address, {"role_name": "ci-apply", "policy_arns": arns}),
	[res("aws_iam_policy", workload_policy_address, {"name": "ci-apply-workload", "arn": workload_policy_arn, "policy": json.marshal({"Statement": []})})],
)

test_violation_workload_declared_without_read_policy if {
	fixture.has("[PLAN-APPLY-05] data.policy_plan.deployment_identity.workload.read_policy", messages_for(workload_world([workload_policy_arn]), with_workload_decl))
}

test_violation_declared_read_policy_absent if {
	fixture.has("[PLAN-APPLY-05] data.policy_plan.deployment_identity.workload.read_policy", messages_for(workload_world([workload_policy_arn]), with_two_workload_decl))
}

workload_attachment(arn) := res("aws_iam_role_policy_attachment", sprintf("%saws_iam_role_policy_attachment.workload", [g]), {"role": "ci-apply", "policy_arn": arn})

read_attachment := res("aws_iam_role_policy_attachment", sprintf("%saws_iam_role_policy_attachment.workload_read", [g]), {"role": "ci-apply", "policy_arn": read_policy_arn})

declared_world := two_workload_world(all_workload_arns)

test_pass_declared_workload_policy_attachment if {
	count(messages_for(array.concat(declared_world, [workload_attachment(workload_policy_arn), read_attachment]), with_two_workload_decl)) == 0
}

test_violation_attachment_of_undeclared_policy if {
	fixture.has(sprintf("[PLAN-APPLY-05] p.json: %saws_iam_role_policy_attachment.workload が apply の role に付ける policy が", [g]), messages_for(array.concat(declared_world, [workload_attachment("arn:aws:iam::aws:policy/ReadOnlyAccess")]), with_two_workload_decl))
}

read_policy_address := sprintf("%saws_iam_policy.workload_read", [g])

read_policy_arn := sprintf("arn:aws:iam::%s:policy/ci-apply-workload-read", [account])

policy_2_address := sprintf("%saws_iam_policy.workload_2", [g])

policy_2_arn := sprintf("arn:aws:iam::%s:policy/ci-apply-workload-2", [account])

with_two_workload_decl := json.patch(with_type("aws_iam_policy", {"class": "target"}), [{"op": "add", "path": "/deployment_identity/workload", "value": {"policy": workload_policy_address, "read_policy": read_policy_address, "policy_2": policy_2_address, "service_linked_roles": [slr_service]}}])

all_workload_arns := [workload_policy_arn, read_policy_arn, policy_2_arn]

# 宣言した型の aws_iam_policy は Deny で守る対象なので、3つの policy の ARN を Deny に載せる。
two_workload_world(arns) := array.concat(
	with_after(world(array.concat(valid_statements, [full_deny(all_workload_arns)])), exclusives[1].address, {"role_name": "ci-apply", "policy_arns": arns}),
	[
		res("aws_iam_policy", workload_policy_address, {"name": "ci-apply-workload", "arn": workload_policy_arn, "policy": json.marshal({"Statement": []})}),
		res("aws_iam_policy", read_policy_address, {"name": "ci-apply-workload-read", "arn": read_policy_arn, "policy": json.marshal({"Statement": []})}),
		res("aws_iam_policy", policy_2_address, {"name": "ci-apply-workload-2", "arn": policy_2_arn, "policy": json.marshal({"Statement": []})}),
	],
)

test_pass_two_workload_policies_attached_in_any_order if {
	every arns in [all_workload_arns, [policy_2_arn, read_policy_arn, workload_policy_arn]] {
		count(messages_for(two_workload_world(arns), with_two_workload_decl)) == 0
	}
}

test_violation_declared_policy_2_absent if {
	world := [r | some r in two_workload_world(all_workload_arns); r.address != policy_2_address]
	fixture.has("[PLAN-APPLY-05] data.policy_plan.deployment_identity.workload.policy_2", messages_for(world, with_two_workload_decl))
}

test_violation_two_workload_policies_not_attached if {
	every arns in [
		[workload_policy_arn], [read_policy_arn], [],
		[workload_policy_arn, read_policy_arn],
		[workload_policy_arn, read_policy_arn, policy_2_arn, "arn:aws:iam::aws:policy/ReadOnlyAccess"],
		[workload_policy_arn, "arn:aws:iam::aws:policy/ReadOnlyAccess"],
		[read_policy_arn, "arn:aws:iam::aws:policy/ReadOnlyAccess"],
		[workload_policy_arn, workload_policy_arn, policy_2_arn],
	] {
		fixture.has(sprintf("[PLAN-APPLY-05] p.json: %s の policy_arns", [exclusives[1].address]), messages_for(two_workload_world(arns), with_two_workload_decl))
	}
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

# --- 黙る分岐 ---

test_violation_state_bucket_children_weakened if {
	every c in [
		{"suffixes": ["/workload/terraform.tfstate"], "version_deletion": true},
		{"roots": true},
	] {
		fixture.has("[PLAN-APPLY-00] data.policy_plan.deployment_identity.apply_role.bootstrap.children の state Bucket", messages_for(world(valid_statements), with_child(state_bucket, c)))
	}
}

test_violation_state_bucket_declaration_missing if {
	every c in [
		{"op": {"op": "remove", "path": "/state_backend/bucket"}, "bucket": ""},
		{"op": {"op": "replace", "path": "/state_backend/bucket", "value": plan_bucket}, "bucket": plan_bucket},
	] {
		fixture.has(sprintf("[PLAN-APPLY-00] data.policy_plan.deployment_identity.apply_role.bootstrap.children の state Bucket %q", [c.bucket]), messages_for(world(valid_statements), json.patch(decl, [c.op])))
	}
}

test_violation_roots_element_malformed if {
	every root in [
		{"writer": "other_role", "State": "other/terraform.tfstate", "unlocker": "other_unlock_role"},
		{"writer": "other_role", "state": "", "unlocker": "other_unlock_role"},
		{"writer": "other_role", "state": "other/terraform.tfstate", "unlocker": "other_unlock_role", "lock": true},
		{"writer": "other_role", "state": "other/terraform.tfstate"},
		{"writer": "other_role", "state": "other/terraform.tfstate", "unlocker": ""},
		{"writer": "other_role", "state": "other/terraform.tfstate", "unlocker": 5},
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

test_violation_covered_by_points_to_other_type if {
	every v in [{"bucket": "key-id"}, {"bucket": st}] {
		fixture.has("[PLAN-APPLY-02] module.state_backend.module.s3.aws_s3_bucket_versioning.this の", messages_for(with_after(world(valid_statements), "module.state_backend.module.s3.aws_s3_bucket_versioning.this", v), decl))
	}
}

test_violation_key_policy_points_to_other_key if {
	extra := res("aws_kms_key", "module.state_backend.module.kms_second.aws_kms_key.this", {"arn": other_key, "id": "second-id", "key_id": "second-kid"})
	extra_policy := res("aws_kms_key_policy", "module.state_backend.module.kms_second.aws_kms_key_policy.this", {"key_id": "key-id", "policy": json.marshal(key_policy_body)})
	s := array.concat(valid_statements, [{"Effect": "Deny", "Action": delegated_actions, "Resource": other_key}])
	fixture.has("[PLAN-APPLY-02] p.json: module.state_backend.module.kms_second.aws_kms_key_policy.this の key_id が", messages_for(array.concat(world(s), [extra, extra_policy]), decl))
}

test_violation_action_and_not_action_on_key_and_version if {
	fixture.has("[PLAN-APPLY-01]", messages(replace(valid_statements, 6, {"Effect": "Deny", "Action": ["*"], "NotAction": ["kms:PutKeyPolicy"], "Resource": key})))
	fixture.has("[PLAN-APPLY-04]", messages(replace(valid_statements, 4, {"Effect": "Deny", "Action": "s3:DeleteObjectVersion", "NotAction": ["s3:DeleteObjectVersion"], "Resource": sprintf("%s/*", [st])})))
}

test_violation_policy_attachment_roles_partially_unknown if {
	extra := {"address": "module.workload.aws_iam_policy_attachment.later", "type": "aws_iam_policy_attachment", "change": {"actions": ["create"], "after": {"roles": ["other"]}, "after_unknown": {"roles": [false, true]}}}
	fixture.has("[PLAN-APPLY-05] p.json: module.workload.aws_iam_policy_attachment.later がどの role に", messages_for(array.concat(world(valid_statements), [extra]), decl))
}

test_violation_policy_attachment_single_role if {
	extra := res("aws_iam_policy_attachment", "module.workload.aws_iam_policy_attachment.this", {"roles": ["ci-apply"], "policy_arn": "arn:aws:iam::aws:policy/AdministratorAccess"})
	fixture.has("[PLAN-APPLY-05] p.json: module.workload.aws_iam_policy_attachment.this が apply の role ci-apply に", messages_for(array.concat(world(valid_statements), [extra]), decl))
}

test_pass_policy_attachment_other_role if {
	extra := res("aws_iam_policy_attachment", "module.workload.aws_iam_policy_attachment.this", {"roles": ["other"], "policy_arn": "arn:aws:iam::aws:policy/AdministratorAccess"})
	count(messages_for(array.concat(world(valid_statements), [extra]), decl)) == 0
}

test_violation_role_inline_policy_nested_unknown if {
	unknown := json.patch([r | some r in world(valid_statements); r.address == apply_role_address][0], [{"op": "add", "path": "/change/after_unknown/inline_policy", "value": [{"name": false, "policy": true}]}])
	fixture.has(sprintf("[PLAN-APPLY-05] p.json: apply の role %s の inline_policy", [apply_role_address]), messages_for(array.concat(without(world(valid_statements), apply_role_address), [unknown]), decl))
}

test_violation_children_on_target_without_children if {
	address := sprintf("%saws_iam_openid_connect_provider.github", [g])
	fixture.has(sprintf("[PLAN-APPLY-00] data.policy_plan.deployment_identity.apply_role.bootstrap.children の %s は", [address]), messages_for(world(valid_statements), with_child(address, {"suffixes": ["/x"]})))
}

# --- PLAN-APPLY-03: 書き込み先として宣言した子 ---

test_pass_writes_case_insensitive if {
	count(messages(replace(valid_statements, 8, {"Effect": "Deny", "NotAction": "S3:PutObject", "Resource": ao_objects}))) == 0
}

test_violation_writes_not_action_opens_more if {
	every names in [["s3:PutObject", "s3:GetObject"], ["s3:PutObject", "s3:DeleteObject"], ["s3:GetObject"], ["s3:DeleteObject"]] {
		fixture.has(denied_03_writes(ao_objects), messages(replace(valid_statements, 8, {"Effect": "Deny", "NotAction": names, "Resource": ao_objects})))
	}
}

test_violation_writes_conditional_deny if {
	s := replace(valid_statements, 8, {"Effect": "Deny", "NotAction": ["s3:PutObject"], "Resource": ao_objects, "Condition": {"Bool": {"aws:SecureTransport": "false"}}})
	fixture.has(denied_03_writes(ao_objects), messages(s))
}

test_violation_writes_full_deny if {
	msgs := messages(replace(valid_statements, 8, full_deny([ao_objects])))
	fixture.has(denied_03_writes(ao_objects), msgs)
	fixture.has(denied_03_writes_denied(ao_objects, "*"), msgs)
	not fixture.has(denied_03(ao_objects), msgs)
}

test_violation_writes_full_deny_alongside if {
	fixture.has(denied_03_writes_denied(ao_objects, "*"), messages(array.concat(valid_statements, [full_deny([ao_objects])])))
}

test_violation_writes_deny_missing if {
	fixture.has(denied_03_writes(ao_objects), messages(remove(valid_statements, 8)))
}

test_violation_writes_action_list if {
	fixture.has(denied_03_writes(ao_objects), messages(replace(valid_statements, 8, {"Effect": "Deny", "Action": ["s3:GetObject", "s3:DeleteObject"], "Resource": ao_objects})))
}

test_violation_apply_output_bucket_uncovered if {
	fixture.has(denied_03(ao), messages(remove(valid_statements, 7)))
}

test_violation_writes_denied_by_action if {
	every a in ["s3:PutObject", "S3:PUTOBJECT", "*", "s3:*", "s3:Put*", "s3:*Object", "s3:PutObj?ct"] {
		fixture.has(denied_03_writes_denied(ao_objects, lower(a)), messages(array.concat(valid_statements, [{"Effect": "Deny", "Action": ["s3:GetObject", a], "Resource": ao_objects}])))
	}
}

test_pass_writes_other_action_denied if {
	every s in [
		{"Effect": "Deny", "Action": ["s3:GetObject", "s3:Put*Acl", "s3:DeleteObject*", "s3:PutObj"], "Resource": ao_objects},
		{"Effect": "Deny", "Action": "s3:PutObject", "Resource": ao_objects, "Condition": {"Bool": {"aws:SecureTransport": "false"}}},
		{"Effect": "Deny", "Action": "*", "Resource": ao},
	] {
		count(messages(array.concat(valid_statements, [s]))) == 0
	}
}

# 書き込み先を宣言した子も、入力に無いか ARN が読めなければ PLAN-APPLY-00 で落ちる。03 が対象0件のまま通らないため。
test_violation_writes_child_absent if {
	fixture.has(sprintf("[PLAN-APPLY-00] data.policy_plan.deployment_identity.apply_role.bootstrap.children の %s が入力にありません", [apply_bucket]), messages_for(without(world(valid_statements), apply_bucket), decl))
}

test_violation_writes_child_arn_not_arn if {
	every v in [{"id": "ao-id"}, {"arn": "", "id": "ao-id"}, {"arn": "Zq8x1", "id": "ao-id"}] {
		rcs := with_after(world(valid_statements), apply_bucket, v)
		fixture.has(sprintf("[PLAN-APPLY-00] bootstrap の構成物 %s の ARN", [apply_bucket]), messages_for(rcs, decl))
	}
}

test_violation_writes_child_arn_unknown if {
	rcs := [unknown_arn(x) | some x in world(valid_statements)]
	fixture.has(sprintf("[PLAN-APPLY-00] bootstrap の構成物 %s の ARN", [apply_bucket]), messages_for(rcs, decl))
}

unknown_arn(x) := json.patch(x, [{"op": "replace", "path": "/change", "value": {"actions": ["create"], "after": {"id": "ao-id"}, "after_unknown": {"arn": true}}}]) if x.address == apply_bucket

unknown_arn(x) := x if x.address != apply_bucket

test_violation_writes_not_action_empty if {
	fixture.has(denied_03_writes(ao_objects), messages(replace(valid_statements, 8, {"Effect": "Deny", "NotAction": [], "Resource": ao_objects})))
}

# NotAction の Deny が書き込み先に IAM で掛かっていても、Resource が ARN と文字列で一致しなければ数えない。
test_violation_writes_not_action_other_resource_forms if {
	every s in [
		{"Effect": "Deny", "NotAction": ["s3:PutObject"], "Resource": "*"},
		{"Effect": "Deny", "NotAction": ["s3:PutObject"], "Resource": "arn:aws:s3:::a*"},
		{"Effect": "Deny", "NotAction": ["s3:PutObject"], "NotResource": "arn:aws:s3:::other/*"},
	] {
		fixture.has(denied_03_writes(ao_objects), messages(replace(valid_statements, 8, s)))
	}
}

test_violation_writes_action_and_not_action if {
	fixture.has(denied_03_writes(ao_objects), messages(replace(valid_statements, 8, {"Effect": "Deny", "Action": "s3:GetObject", "NotAction": ["s3:PutObject"], "Resource": ao_objects})))
}

test_pass_two_writes if {
	d := with_child(apply_bucket, {"suffixes": ["/*"], "writes": ["s3:PutObject", "s3:PutObjectTagging"]})
	s := replace(valid_statements, 8, {"Effect": "Deny", "NotAction": ["s3:PutObjectTagging", "s3:PutObject"], "Resource": ao_objects})
	count(messages_for(world(s), d)) == 0
}

test_violation_writes_denied_by_resource_pattern if {
	every s in [
		{"Effect": "Deny", "Action": "s3:PutObject", "Resource": "*"},
		{"Effect": "Deny", "Action": "s3:PutObject", "Resource": "arn:aws:s3:::a*"},
		{"Effect": "Deny", "Action": "s3:PutObject", "Resource": "arn:aws:s3:::a?/?"},
		{"Effect": "Deny", "Action": "s3:PutObject", "NotResource": "arn:aws:s3:::other/*"},
	] {
		fixture.has(denied_03_writes_denied(ao_objects, "s3:putobject"), messages(array.concat(valid_statements, [s])))
	}
}

test_pass_writes_denied_elsewhere if {
	every s in [
		{"Effect": "Deny", "Action": "s3:PutObject", "Resource": "arn:aws:s3:::p*"},
		{"Effect": "Deny", "Action": "s3:PutObject", "Resource": "arn:aws:s3:::ao"},
		{"Effect": "Deny", "Action": "s3:PutObject", "NotResource": "arn:aws:s3:::ao/*"},
		{"Effect": "Deny", "Action": "s3:PutObject", "NotResource": ["arn:aws:s3:::other", "arn:aws:s3:::a*"]},
	] {
		count(messages(array.concat(valid_statements, [s]))) == 0
	}
}

# --- PLAN-APPLY-06: 統制の構成物の変更の拒否 ---

with_governance(g) := json.patch(decl, [{"op": "replace", "path": "/deployment_identity/apply_role/governance", "value": governance_value(g)}])

# management_actions と management_policy を書かない宣言は、既定の値を持つとして扱う。
governance_value(g) := object.union({"management_actions": management_actions, "management_policy": governance_policy_address}, g) if is_object(g)

else := g

denied_06(a) := sprintf("[PLAN-APPLY-06] apply の role %q が、%s を Resource", [apply_policy_address, a])

malformed_governance := "[PLAN-APPLY-00] data.policy_plan.deployment_identity.apply_role.governance の"

test_pass_governance_case_and_array_resource if {
	s := replace(valid_statements, governance_index, {"Effect": "Deny", "Action": ["Organizations:Create*", "STS:AssumeRoot"], "Resource": ["*"]})
	count(messages(s)) == 0
}

test_pass_governance_split_statements if {
	s := array.concat(remove(valid_statements, governance_index), [
		{"Effect": "Deny", "Action": "organizations:Create*", "Resource": "*"},
		{"Effect": "Deny", "Action": "sts:AssumeRoot", "Resource": "*"},
	])
	count(messages(s)) == 0
}

test_violation_governance_action_missing if {
	s := replace(valid_statements, governance_index, {"Effect": "Deny", "Action": ["organizations:Create*"], "Resource": "*"})
	some m in messages(s)
	startswith(m, denied_06("sts:AssumeRoot"))
}

test_violation_governance_deny_removed if {
	msgs := messages(remove(valid_statements, governance_index))
	some m in msgs
	startswith(m, denied_06("organizations:Create*"))
	some n in msgs
	startswith(n, denied_06("sts:AssumeRoot"))
}

test_violation_governance_with_condition if {
	s := replace(valid_statements, governance_index, object.union(governance_deny, {"Condition": {"StringEquals": {"aws:PrincipalAccount": account}}}))
	some m in messages(s)
	startswith(m, denied_06("organizations:Create*"))
}

test_violation_governance_on_specific_resource if {
	s := replace(valid_statements, governance_index, object.union(governance_deny, {"Resource": "arn:aws:organizations::123456789012:ou/o-*/ou-*"}))
	some m in messages(s)
	startswith(m, denied_06("organizations:Create*"))
}

test_violation_governance_allow_not_deny if {
	s := replace(valid_statements, governance_index, object.union(governance_deny, {"Effect": "Allow"}))
	some m in messages(s)
	startswith(m, denied_06("sts:AssumeRoot"))
}

test_violation_governance_by_not_action if {
	s := replace(valid_statements, governance_index, {"Effect": "Deny", "NotAction": ["s3:GetObject"], "Resource": "*"})
	some m in messages(s)
	startswith(m, denied_06("organizations:Create*"))
}

test_violation_governance_declaration_missing if {
	d := json.patch(decl, [{"op": "remove", "path": "/deployment_identity/apply_role/governance"}])
	some m in messages_for(world(valid_statements), d)
	startswith(m, malformed_governance)
}

test_violation_governance_declaration_empty if {
	some m in messages_for(world(valid_statements), with_governance({"actions": []}))
	startswith(m, malformed_governance)
}

test_violation_governance_declaration_not_string if {
	some m in messages_for(world(valid_statements), with_governance({"actions": ["sts:AssumeRoot", 1]}))
	startswith(m, malformed_governance)
}

test_violation_governance_declaration_empty_string if {
	some m in messages_for(world(valid_statements), with_governance({"actions": [""]}))
	startswith(m, malformed_governance)
}

test_violation_governance_by_wildcard_action if {
	every action in ["*", "organizations:*", ["*"]] {
		s := replace(valid_statements, governance_index, {"Effect": "Deny", "Action": action, "Resource": "*"})
		some m in messages(s)
		startswith(m, denied_06("organizations:Create*"))
	}
}

sso_governance_actions := [
	"sso:AttachCustomerManagedPolicyReferenceToPermissionSet",
	"sso:AttachManagedPolicyToPermissionSet",
	"sso:CreateAccountAssignment",
	"sso:CreatePermissionSet",
	"sso:DeleteAccountAssignment",
	"sso:DeleteInlinePolicyFromPermissionSet",
	"sso:DeletePermissionSet",
	"sso:DeletePermissionsBoundaryFromPermissionSet",
	"sso:DetachCustomerManagedPolicyReferenceFromPermissionSet",
	"sso:DetachManagedPolicyFromPermissionSet",
	"sso:ProvisionPermissionSet",
	"sso:PutInlinePolicyToPermissionSet",
	"sso:PutPermissionsBoundaryToPermissionSet",
	"sso:PutPermissionsPolicy",
	"sso:UpdatePermissionSet",
]

sso_deny_actions := [
	"sso:*AccountAssignment",
	"sso:*FromPermissionSet",
	"sso:*ToPermissionSet",
	"sso:CreatePermissionSet",
	"sso:DeletePermissionSet",
	"sso:ProvisionPermissionSet",
	"sso:PutPermissionsPolicy",
	"sso:UpdatePermissionSet",
]

sso_messages(actions) := messages_for(
	world(replace(valid_statements, governance_index, {"Effect": "Deny", "Action": actions, "Resource": "*"})),
	with_governance({"actions": sso_governance_actions}),
)

test_pass_governance_sso_suffix_wildcards if {
	count(sso_messages(sso_deny_actions)) == 0
}

test_pass_governance_sso_suffix_wildcards_case if {
	count(sso_messages([upper(a) | some a in sso_deny_actions])) == 0
}

test_violation_governance_wildcard_not_covering if {
	msgs := sso_messages(["sso:*AccountAssignment"])
	some m in msgs
	startswith(m, denied_06("sso:CreatePermissionSet"))
	not any_startswith(msgs, denied_06("sso:CreateAccountAssignment"))
	not any_startswith(msgs, denied_06("sso:DeleteAccountAssignment"))
}

test_violation_governance_sso_wildcard_removed if {
	some m in sso_messages(array.slice(sso_deny_actions, 1, count(sso_deny_actions)))
	startswith(m, denied_06("sso:CreateAccountAssignment"))
}

test_violation_governance_other_service_wildcard if {
	some m in sso_messages(["iam:*PermissionSet", "iam:*AccountAssignment"])
	startswith(m, denied_06("sso:CreatePermissionSet"))
}

test_violation_governance_service_wide_wildcard if {
	every action in [["sso:*"], ["sso:**"], ["sso:?*"], ["*:CreatePermissionSet"], ["ss?:CreatePermissionSet"]] {
		some m in sso_messages(action)
		startswith(m, denied_06("sso:CreatePermissionSet"))
	}
}

test_pass_governance_question_mark_covers_name if {
	every action in [["sso:CreatePermissionSe?"], ["sso:Create?ermissionSet"]] {
		not any_startswith(sso_messages(action), denied_06("sso:CreatePermissionSet"))
	}
}

# 宣言の名前が * を持つとき、それに当たる pattern は宣言の名前すべて（organizations:CreateAccount など）を覆わない。
test_violation_governance_pattern_on_declared_wildcard if {
	s := replace(valid_statements, governance_index, {"Effect": "Deny", "Action": ["organizations:Create?", "sts:AssumeRoot"], "Resource": "*"})
	some m in messages(s)
	startswith(m, denied_06("organizations:Create*"))
}

any_startswith(msgs, prefix) if {
	some m in msgs
	startswith(m, prefix)
}

test_violation_governance_action_and_not_action if {
	s := replace(valid_statements, governance_index, object.union(governance_deny, {"NotAction": ["s3:GetObject"]}))
	some m in messages(s)
	startswith(m, denied_06("organizations:Create*"))
}

test_violation_governance_with_not_resource if {
	s := replace(valid_statements, governance_index, object.union(governance_deny, {"NotResource": "arn:aws:s3:::x"}))
	some m in messages(s)
	startswith(m, denied_06("organizations:Create*"))
}

test_violation_governance_deny_on_other_role if {
	rcs := array.concat(world(remove(valid_statements, governance_index)), [role_policy("plan_extra", "ci-plan", [governance_deny])])
	some m in messages_for(rcs, decl)
	startswith(m, denied_06("organizations:Create*"))
}

test_violation_governance_declaration_actions_not_array if {
	every g in [{"actions": "sts:AssumeRoot"}, {"actions": null}, {"actions": {"a": "sts:AssumeRoot"}}, "sts:AssumeRoot", null] {
		some m in messages_for(world(valid_statements), with_governance(g))
		startswith(m, malformed_governance)
	}
}

# workload の policy の ARN が plan で決まらないと、比べる先が [null] になる。値の無い attachment を一致と数えない。
test_violation_attachment_policy_arn_absent_while_workload_arn_unknown if {
	world := [r | some r in declared_world; r.address != workload_policy_address]
	unknown_policy := {"address": workload_policy_address, "type": "aws_iam_policy", "change": {"actions": ["create"], "after": {"name": "ci-apply-workload", "policy": json.marshal({"Statement": []})}, "after_unknown": {"arn": true}}}
	absent := res("aws_iam_role_policy_attachment", sprintf("%saws_iam_role_policy_attachment.workload", [g]), {"role": "ci-apply"})
	fixture.has(sprintf("[PLAN-APPLY-05] p.json: %saws_iam_role_policy_attachment.workload が apply の role に付ける policy が", [g]), messages_for(array.concat(world, [unknown_policy, absent]), with_two_workload_decl))
}

test_violation_attachment_policy_arn_unknown if {
	unknown := {"address": sprintf("%saws_iam_role_policy_attachment.workload", [g]), "type": "aws_iam_role_policy_attachment", "change": {"actions": ["create"], "after": {"role": "ci-apply"}, "after_unknown": {"policy_arn": true}}}
	msgs := messages_for(array.concat(declared_world, [unknown]), with_two_workload_decl)
	fixture.has(sprintf("[PLAN-APPLY-05] p.json: %saws_iam_role_policy_attachment.workload の policy_arn を plan で読めません", [g]), msgs)
	not fixture.has(sprintf("[PLAN-APPLY-05] p.json: %saws_iam_role_policy_attachment.workload が apply の role に付ける policy が", [g]), msgs)
}

test_pass_attachment_on_other_role if {
	other := res("aws_iam_role_policy_attachment", "module.workload.aws_iam_role_policy_attachment.other", {"role": "other", "policy_arn": "arn:aws:iam::aws:policy/ReadOnlyAccess"})
	count([m | some m in messages_for(array.concat(declared_world, [workload_attachment(workload_policy_arn), read_attachment, other]), with_two_workload_decl); startswith(m, "[PLAN-APPLY-05]")]) == 0
}

# 合成の宣言は2つの Action しか持たない。実際の宣言の Action（CloudFormation を含む）でも、Deny の過不足を見分けることを固定する。
declared_governance_actions := data.policy_plan.deployment_identity.apply_role.governance.actions

declared_decl := json.patch(decl, [{"op": "replace", "path": "/deployment_identity/apply_role/governance/actions", "value": declared_governance_actions}])

declared_messages(actions) := messages_for(world(replace(valid_statements, governance_index, {"Effect": "Deny", "Action": actions, "Resource": "*"})), declared_decl)

# 合成の sso の宣言を実際の宣言に揃える。宣言の Identity Center の Action が増減したら、ここで落ちる。
test_sso_governance_actions_match_declaration if {
	{a | some a in declared_governance_actions; startswith(a, "sso:")} == {a | some a in sso_governance_actions}
	count(sso_governance_actions) == 15
}

test_pass_declared_governance_actions if {
	count([a | some a in declared_governance_actions; startswith(a, "cloudformation:")]) > 0
	count(declared_messages(declared_governance_actions)) == 0
}

test_violation_declared_cloudformation_action_missing if {
	msgs := declared_messages([a | some a in declared_governance_actions; a != "cloudformation:UpdateStackInstances"])
	some m in msgs
	startswith(m, denied_06("cloudformation:UpdateStackInstances"))
}

# --- service-linked role（deployment-identity/ADR-0002 決定6）---

slr_service := "management.chatbot.amazonaws.com"

slr_arn := sprintf("arn:aws:iam::%s:role/aws-service-role/%s/AWSServiceRoleForAWSChatbot", [account, slr_service])

slr_address := "module.deployment_identity.module.service_linked_roles.aws_iam_service_linked_role.chatbot[0]"

slr(address) := res("aws_iam_service_linked_role", address, {"arn": slr_arn, "aws_service_name": slr_service})

boundary_address := sprintf("%saws_iam_policy.boundary[\"ops-notification\"]", [g])

boundary_arn := sprintf("arn:aws:iam::%s:policy/ci-boundary-ops-notification", [account])

slr_create := {"Effect": "Allow", "Action": "iam:CreateServiceLinkedRole", "Resource": slr_arn, "Condition": {"StringEquals": {"iam:AWSServiceName": slr_service}}}

slr_read := {"Effect": "Allow", "Action": ["iam:GetRole", "iam:List*"], "Resource": ["*", slr_arn]}

boundary_allow := {"Effect": "Allow", "Action": ["sns:Publish", "iam:Get*"], "Resource": "*"}

slr_decl := json.patch(with_two_workload_decl, [{"op": "add", "path": "/deployment_identity/workload/boundaries", "value": {"ops-notification": boundary_address}}])

managed_policy(address, name, arn, statements) := res("aws_iam_policy", address, {"name": name, "arn": arn, "policy": json.marshal({"Statement": statements})})

slr_world(apply_extra, workload, read, boundary, roles) := slr_world_2(apply_extra, workload, read, [], boundary, roles)

# 基底の world の前提の service-linked role を外し、roles で置き直す。
slr_world_2(apply_extra, workload, read, policy_2, boundary, roles) := array.concat(
	with_after(without(world(array.concat(array.concat(valid_statements, [full_deny(array.concat(all_workload_arns, [boundary_arn]))]), apply_extra)), slr_address), exclusives[1].address, {"role_name": "ci-apply", "policy_arns": all_workload_arns}),
	array.concat(
		[
			managed_policy(workload_policy_address, "ci-apply-workload", workload_policy_arn, workload),
			managed_policy(read_policy_address, "ci-apply-workload-read", read_policy_arn, read),
			managed_policy(policy_2_address, "ci-apply-workload-2", policy_2_arn, policy_2),
			managed_policy(boundary_address, "ci-boundary-ops-notification", boundary_arn, boundary),
		],
		roles,
	),
)

slr_messages(apply_extra, workload, read, boundary) := messages_for(slr_world(apply_extra, workload, read, boundary, [slr(slr_address)]), slr_decl)

slr_allowed(address, a) := sprintf("[PLAN-APPLY-02] p.json: %s の Allow が、service-linked role %s に %s を許しています", [address, slr_arn, a])

test_pass_service_linked_role if {
	count(slr_messages([], [slr_create], [slr_read], [boundary_allow])) == 0
}

test_violation_service_linked_role_update_in_apply_inline if {
	msgs := slr_messages([{"Effect": "Allow", "Action": "iam:UpdateRole", "Resource": slr_arn}], [slr_create], [slr_read], [boundary_allow])
	fixture.has(slr_allowed(apply_policy_address, "iam:updaterole"), msgs)
}

test_violation_service_linked_role_wildcard_in_boundary if {
	every a in ["iam:Put*", "iam:*", "*", "IAM:UpdateRoleDescription"] {
		msgs := slr_messages([], [slr_create], [slr_read], [boundary_allow, {"Effect": "Allow", "Action": a, "Resource": "*"}])
		fixture.has(slr_allowed(boundary_address, lower(a)), msgs)
	}
}

test_violation_service_linked_role_pattern_resource if {
	msgs := slr_messages([], [slr_create, {"Effect": "Allow", "Action": "iam:DeleteServiceLinkedRole", "Resource": sprintf("arn:aws:iam::%s:role/aws-service-role/*", [account])}], [slr_read], [boundary_allow])
	fixture.has(slr_allowed(workload_policy_address, "iam:deleteservicelinkedrole"), msgs)
}

test_violation_service_linked_role_create_unconditioned if {
	every create in [
		object.remove(slr_create, ["Condition"]),
		json.patch(slr_create, [{"op": "replace", "path": "/Condition/StringEquals/iam:AWSServiceName", "value": "config.amazonaws.com"}]),
		json.patch(slr_create, [{"op": "replace", "path": "/Condition", "value": {"StringLike": {"iam:AWSServiceName": slr_service}}}]),
	] {
		fixture.has(slr_allowed(workload_policy_address, "iam:createservicelinkedrole"), slr_messages([], [create], [slr_read], [boundary_allow]))
	}
}

test_violation_service_linked_role_not_action_allow if {
	msgs := slr_messages([], [slr_create], [slr_read, {"Effect": "Allow", "NotAction": "s3:*", "Resource": "*"}], [boundary_allow])
	fixture.has(sprintf("[PLAN-APPLY-02] p.json: %s の Allow の NotAction が、service-linked role %s への", [read_policy_address, slr_arn]), msgs)
}

test_pass_service_linked_role_not_action_excludes_iam if {
	count(slr_messages([], [slr_create], [slr_read, {"Effect": "Allow", "NotAction": ["iam:*", "s3:*"], "Resource": "*"}], [boundary_allow])) == 0
}

test_violation_service_linked_role_arn_unknown if {
	rcs := with_after(slr_world([], [slr_create], [slr_read], [boundary_allow], [slr(slr_address)]), slr_address, {"aws_service_name": slr_service})
	fixture.has(sprintf("[PLAN-APPLY-00] bootstrap の構成物 %s の ARN", [slr_address]), messages_for(rcs, slr_decl))
}

test_violation_service_linked_role_unclassified if {
	d := json.patch(slr_decl, [{"op": "remove", "path": "/deployment_identity/apply_role/bootstrap/types/aws_iam_service_linked_role"}])
	fixture.has(sprintf("[PLAN-APPLY-02] bootstrap の構成物 %s の型", [slr_address]), messages_for(slr_world([], [slr_create], [slr_read], [boundary_allow], [slr(slr_address)]), d))
}

test_violation_service_linked_class_extra_key if {
	d := json.patch(slr_decl, [{"op": "add", "path": "/deployment_identity/apply_role/bootstrap/types/aws_iam_service_linked_role/children", "value": true}])
	fixture.has(malformed, messages_for(slr_world([], [slr_create], [slr_read], [boundary_allow], [slr(slr_address)]), d))
}

# --- PLAN-APPLY-07: 前提の service-linked role の置き場 ---

outside_address := "module.ops_notification.module.chat_slack.aws_iam_service_linked_role.chatbot"

test_violation_prerequisite_in_workload_usecase if {
	msgs := messages_for(slr_world([], [slr_create], [slr_read], [boundary_allow], [slr(slr_address), slr(outside_address)]), slr_decl)
	fixture.has(sprintf("[PLAN-APPLY-07] p.json: %s が、宣言した前提の service-linked role（%s）を module.deployment_identity. の外で作っています", [outside_address, slr_service]), msgs)
}

test_violation_prerequisite_count if {
	every c in [
		[[], 0],
		[[slr(slr_address), slr("module.deployment_identity.module.service_linked_roles.aws_iam_service_linked_role.chatbot[1]")], 2],
	] {
		msgs := messages_for(slr_world([], [slr_create], [slr_read], [boundary_allow], c[0]), slr_decl)
		some m in msgs
		startswith(m, sprintf("[PLAN-APPLY-07] 宣言した前提の service-linked role（%s）が、module.deployment_identity. の下に", [slr_service]))
		endswith(m, sprintf("の下に %d 件です（1件を期待）", [c[1]]))
	}
}

# 宣言の外のサービス（account-baseline の Config）の service-linked role は、workload root の usecase が作ってよい。
test_pass_undeclared_service_outside_bootstrap if {
	config := res("aws_iam_service_linked_role", "module.account_baseline.module.config_recorder.aws_iam_service_linked_role.config", {"arn": sprintf("arn:aws:iam::%s:role/aws-service-role/config.amazonaws.com/AWSServiceRoleForConfig", [account]), "aws_service_name": "config.amazonaws.com"})
	count(messages_for(slr_world([], [slr_create], [slr_read], [boundary_allow], [slr(slr_address), config]), slr_decl)) == 0
}

test_violation_prerequisite_outside_prerequisite_module if {
	every address in [
		"module.state_backend.aws_iam_service_linked_role.chatbot",
		"aws_iam_service_linked_role.chatbot",
	] {
		msgs := messages_for(slr_world([], [slr_create], [slr_read], [boundary_allow], [slr(slr_address), slr(address)]), slr_decl)
		fixture.has(sprintf("[PLAN-APPLY-07] p.json: %s が、宣言した前提の service-linked role（%s）を module.deployment_identity. の外で作っています", [address, slr_service]), msgs)
	}
}

# 置き場の外の1件だけが落ち、置き場の下の1件は数を満たすので、数の検査は落ちない。
test_violation_prerequisite_only_outside_one_denied if {
	msgs := messages_for(slr_world([], [slr_create], [slr_read], [boundary_allow], [slr(slr_address), slr(outside_address)]), slr_decl)
	count(msgs) == 1
	fixture.has(sprintf("[PLAN-APPLY-07] p.json: %s が", [outside_address]), msgs)
}

member_slr_address := "module.deployment_identity_m.module.service_linked_roles.aws_iam_service_linked_role.chatbot[0]"

member_slr_decl := json.patch(slr_decl, [
	{"op": "add", "path": "/state_backend/module", "value": "module.state_backend."},
	{"op": "add", "path": "/deployment_identity/module", "value": "module.deployment_identity."},
	{"op": "add", "path": "/state_backend/instances", "value": {"p": [{"name": "m", "state_backend": "module.state_backend_m.", "deployment_identity": "module.deployment_identity_m."}]}},
])

test_pass_prerequisite_in_member_instance if {
	count(messages_for(slr_world([], [slr_create], [slr_read], [boundary_allow], [slr(slr_address), slr(member_slr_address)]), member_slr_decl)) == 0
}

test_violation_prerequisite_in_undeclared_instance if {
	address := "module.deployment_identity_x.module.service_linked_roles.aws_iam_service_linked_role.chatbot[0]"
	msgs := messages_for(slr_world([], [slr_create], [slr_read], [boundary_allow], [slr(slr_address), slr(address)]), member_slr_decl)
	fixture.has(sprintf("[PLAN-APPLY-07] p.json: %s が、宣言した前提の service-linked role（%s）を module.deployment_identity. の外で作っています", [address, slr_service]), msgs)
}

test_violation_prerequisite_count_ignores_member_instance if {
	msgs := messages_for(slr_world([], [slr_create], [slr_read], [boundary_allow], [slr(member_slr_address)]), member_slr_decl)
	fixture.has(sprintf("[PLAN-APPLY-07] 宣言した前提の service-linked role（%s）が、module.deployment_identity. の下に 0 件です（1件を期待）", [slr_service]), msgs)
}

test_violation_service_linked_role_service_unknown if {
	every after in [{"arn": slr_arn}, {"arn": slr_arn, "aws_service_name": null}, {"arn": slr_arn, "aws_service_name": 1}] {
		rcs := with_after(slr_world([], [slr_create], [slr_read], [boundary_allow], [slr(slr_address)]), slr_address, after)
		fixture.has(sprintf("[PLAN-APPLY-07] p.json: %s の aws_service_name が plan で決まっていません", [slr_address]), messages_for(rcs, slr_decl))
	}
}

test_violation_service_unknown_outside_bootstrap if {
	unknown := res("aws_iam_service_linked_role", outside_address, {"arn": slr_arn})
	msgs := messages_for(slr_world([], [slr_create], [slr_read], [boundary_allow], [slr(slr_address), unknown]), slr_decl)
	fixture.has(sprintf("[PLAN-APPLY-07] p.json: %s の aws_service_name が plan で決まっていません", [outside_address]), msgs)
}

test_violation_undeclared_service_in_prerequisite_module if {
	config_address := "module.deployment_identity.module.service_linked_roles.aws_iam_service_linked_role.config"
	config := res("aws_iam_service_linked_role", config_address, {"arn": sprintf("arn:aws:iam::%s:role/aws-service-role/config.amazonaws.com/AWSServiceRoleForConfig", [account]), "aws_service_name": "config.amazonaws.com"})
	msgs := messages_for(slr_world([], [slr_create], [slr_read], [boundary_allow], [slr(slr_address), config]), slr_decl)
	fixture.has(sprintf("[PLAN-APPLY-07] p.json: %s の service-linked role（config.amazonaws.com）が、宣言した前提のサービス", [config_address]), msgs)
}

# 同じ ARN の2つの role が別のサービスを持っても評価は衝突せず、宣言の外のサービスの組で作成の Allow が落ちる。
test_violation_same_arn_different_service if {
	other_address := "module.deployment_identity.module.service_linked_roles.aws_iam_service_linked_role.other"
	other := res("aws_iam_service_linked_role", other_address, {"arn": slr_arn, "aws_service_name": "config.amazonaws.com"})
	msgs := messages_for(slr_world([], [slr_create], [slr_read], [boundary_allow], [slr(slr_address), other]), slr_decl)
	fixture.has(slr_allowed(workload_policy_address, "iam:createservicelinkedrole"), msgs)
}

# --- 前提のサービスと、apply の role が作るサービスの区別 ---

config_service := "config.amazonaws.com"

config_slr_arn := sprintf("arn:aws:iam::%s:role/aws-service-role/%s/AWSServiceRoleForConfig", [account, config_service])

analyzer_service := "access-analyzer.amazonaws.com"

analyzer_slr_arn := sprintf("arn:aws:iam::%s:role/aws-service-role/%s/AWSServiceRoleForAccessAnalyzer", [account, analyzer_service])

# 実の宣言と同じく、apply の role は3つのサービスの作成を許され、前提は Chatbot だけである。
baseline_decl := json.patch(slr_decl, [{"op": "replace", "path": "/deployment_identity/workload/service_linked_roles", "value": [analyzer_service, config_service, slr_service]}])

baseline_slrs := [
	slr(slr_address),
	res("aws_iam_service_linked_role", "module.account_baseline.aws_iam_service_linked_role.config", {"arn": config_slr_arn, "aws_service_name": config_service}),
	res("aws_iam_service_linked_role", "module.account_baseline.aws_iam_service_linked_role.analyzer", {"arn": analyzer_slr_arn, "aws_service_name": analyzer_service}),
]

baseline_messages(policy_2) := messages_for(slr_world_2([], [slr_create], [slr_read], policy_2, [boundary_allow], baseline_slrs), baseline_decl)

config_create := {"Effect": "Allow", "Action": "iam:CreateServiceLinkedRole", "Resource": config_slr_arn, "Condition": {"StringEquals": {"iam:AWSServiceName": config_service}}}

config_pass := {"Effect": "Allow", "Action": "iam:PassRole", "Resource": config_slr_arn, "Condition": {"StringEquals": {"iam:PassedToService": config_service}}}

test_pass_workload_service_linked_roles_outside_prerequisite_module if {
	count(baseline_messages([config_create, config_pass])) == 0
}

test_pass_account_iam_write_in_policy_2 if {
	count(baseline_messages([{"Effect": "Allow", "Action": "iam:UpdateAccountPasswordPolicy", "Resource": "*"}])) == 0
}

test_violation_update_role_on_prerequisite_in_policy_2 if {
	fixture.has(slr_allowed(policy_2_address, "iam:updaterole"), baseline_messages([{"Effect": "Allow", "Action": "iam:UpdateRole", "Resource": slr_arn}]))
}

test_violation_pass_role_on_prerequisite_in_policy_2 if {
	fixture.has(slr_allowed(policy_2_address, "iam:passrole"), baseline_messages([config_pass, {"Effect": "Allow", "Action": "iam:PassRole", "Resource": slr_arn}]))
}

# 置き場の下では、apply の role が作成を許されるサービスでも、前提でなければ落ちる。
test_violation_workload_service_in_prerequisite_module if {
	config_address := "module.deployment_identity.module.service_linked_roles.aws_iam_service_linked_role.config"
	config := res("aws_iam_service_linked_role", config_address, {"arn": config_slr_arn, "aws_service_name": config_service})
	msgs := messages_for(slr_world_2([], [slr_create], [slr_read], [], [boundary_allow], [slr(slr_address), config]), baseline_decl)
	fixture.has(sprintf("[PLAN-APPLY-07] p.json: %s の service-linked role（%s）が、宣言した前提のサービス（[%q]）にありません", [config_address, config_service, slr_service]), msgs)
}

# role に向かない IAM の書き込みに当たる wildcard は数えず、role の書き込みに当たる wildcard は数える。
test_pass_account_level_wildcard_in_policy_2 if {
	count(baseline_messages([{"Effect": "Allow", "Action": ["iam:UpdateAccount*", "iam:*AccountAlias"], "Resource": "*"}])) == 0
}

test_violation_role_wildcard_in_policy_2 if {
	fixture.has(slr_allowed(policy_2_address, "iam:update*"), baseline_messages([{"Effect": "Allow", "Action": "iam:Update*", "Resource": "*"}]))
}

# --- PLAN-APPLY-02 の境界 ---

test_pass_write_to_other_role if {
	other_role := role_arn("ops-notification-chatbot")
	count(slr_messages([], [slr_create, {"Effect": "Allow", "Action": ["iam:UpdateRole", "iam:PutRolePolicy"], "Resource": other_role}], [slr_read], [boundary_allow])) == 0
}

test_pass_read_actions_on_service_linked_role if {
	count(slr_messages([], [slr_create], [slr_read, {"Effect": "Allow", "Action": ["iam:GenerateServiceLastAccessedDetails", "iam:SimulatePrincipalPolicy"], "Resource": slr_arn}], [boundary_allow])) == 0
}

# iam:PassRole は読み取りの動詞でないので、service-linked role に当たれば書き込みと数える。
test_violation_pass_role_on_service_linked_role if {
	msgs := slr_messages([], [slr_create, {"Effect": "Allow", "Action": "iam:PassRole", "Resource": slr_arn}], [slr_read], [boundary_allow])
	fixture.has(slr_allowed(workload_policy_address, "iam:passrole"), msgs)
}

test_violation_not_resource_excluding_other_role if {
	msgs := slr_messages([], [slr_create, {"Effect": "Allow", "Action": "iam:UpdateRole", "NotResource": role_arn("ci-plan")}], [slr_read], [boundary_allow])
	fixture.has(slr_allowed(workload_policy_address, "iam:updaterole"), msgs)
}

test_pass_not_resource_listing_service_linked_role if {
	count(slr_messages([], [slr_create, {"Effect": "Allow", "Action": "iam:UpdateRole", "NotResource": [sprintf("arn:aws:iam::%s:role/aws-service-role/*", [account])]}], [slr_read], [boundary_allow])) == 0
}

test_violation_create_condition_multiple_or_for_any_value if {
	every create in [
		json.patch(slr_create, [{"op": "replace", "path": "/Condition/StringEquals/iam:AWSServiceName", "value": [slr_service, "config.amazonaws.com"]}]),
		json.patch(slr_create, [{"op": "replace", "path": "/Condition", "value": {"ForAnyValue:StringEquals": {"iam:AWSServiceName": slr_service}}}]),
	] {
		fixture.has(slr_allowed(workload_policy_address, "iam:createservicelinkedrole"), slr_messages([], [create], [slr_read], [boundary_allow]))
	}
}

test_violation_create_resource_not_exact_arn if {
	every resource in [
		"*",
		[slr_arn, "*"],
		sprintf("arn:aws:iam::%s:role/aws-service-role/%s/*", [account, slr_service]),
		sprintf("arn:aws:iam::%s:role/aws-service-role/*", [account]),
	] {
		create := json.patch(slr_create, [{"op": "replace", "path": "/Resource", "value": resource}])
		fixture.has(slr_allowed(workload_policy_address, "iam:createservicelinkedrole"), slr_messages([], [create], [slr_read], [boundary_allow]))
	}
}

test_violation_create_not_resource if {
	create := json.patch(object.remove(slr_create, ["Resource"]), [{"op": "add", "path": "/NotResource", "value": role_arn("ci-plan")}])
	fixture.has(slr_allowed(workload_policy_address, "iam:createservicelinkedrole"), slr_messages([], [create], [slr_read], [boundary_allow]))
}

test_pass_create_resource_exact_arn if {
	every resource in [slr_arn, [slr_arn]] {
		create := json.patch(slr_create, [{"op": "replace", "path": "/Resource", "value": resource}])
		count(slr_messages([], [create], [slr_read], [boundary_allow])) == 0
	}
}

test_violation_not_action_only_iam_reads if {
	msgs := slr_messages([], [slr_create], [slr_read, {"Effect": "Allow", "NotAction": ["iam:GetRole", "iam:ListRoles"], "Resource": "*"}], [boundary_allow])
	fixture.has(sprintf("[PLAN-APPLY-02] p.json: %s の Allow の NotAction が、service-linked role %s への", [read_policy_address, slr_arn]), msgs)
}

# --- 管理アカウントでだけ拒む統制の Action（deployment-identity/ADR-0201 決定5）---

# bootstrap の modules の外に置き、PLAN-APPLY-02・03 の対象にしない。
governance_policy_address := "module.governance_marker.aws_iam_policy.governance[0]"

governance_policy_rc := res("aws_iam_policy", governance_policy_address, {"name": "tabp-admin-ci-apply-identity-governance", "arn": sprintf("arn:aws:iam::%s:policy/tabp-admin-ci-apply-identity-governance", [account]), "policy": json.marshal({"Statement": []})})

without_management_deny := remove(valid_statements, management_index)

test_violation_management_action_not_denied_in_admin if {
	fixture.has(denied_06("iam:UpdateAccountPasswordPolicy"), messages_for(world(without_management_deny), decl))
}

test_pass_management_action_denied_in_admin if {
	not fixture.has("[PLAN-APPLY-06]", messages_for(world(valid_statements), decl))
}

# 統制の policy が無い入力では、宣言の address のずれとして落とし、management_actions は求めない。
test_pass_management_action_not_required_without_governance_policy if {
	msgs := messages_for(without(world(without_management_deny), governance_policy_address), decl)
	not fixture.has(denied_06("iam:UpdateAccountPasswordPolicy"), msgs)
	fixture.has("[PLAN-APPLY-00] data.policy_plan.deployment_identity.apply_role.governance.management_policy", msgs)
}

# 宣言の address が入力に無いと、management_actions の検査が黙って外れる。role の名前によらず落とす。
test_violation_management_policy_absent if {
	rcs := without(world(valid_statements), governance_policy_address)
	fixture.has("[PLAN-APPLY-00] data.policy_plan.deployment_identity.apply_role.governance.management_policy", messages_for(rcs, decl))
}

test_violation_management_policy_at_other_address if {
	other := json.patch(governance_policy_rc, [{"op": "replace", "path": "/address", "value": "module.governance_marker.aws_iam_policy.governance_x[0]"}])
	fixture.has("[PLAN-APPLY-00] data.policy_plan.deployment_identity.apply_role.governance.management_policy", messages_for(array.concat(without(world(valid_statements), governance_policy_address), [other]), decl))
}

test_pass_management_policy_present if {
	not fixture.has("[PLAN-APPLY-00] data.policy_plan.deployment_identity.apply_role.governance.management_policy", messages_for(world(valid_statements), decl))
}

management_malformed(patch) if {
	some msg in messages_for(world(valid_statements), json.patch(decl, [patch]))
	startswith(msg, malformed_governance)
}

test_violation_management_actions_not_array if {
	management_malformed({"op": "replace", "path": "/deployment_identity/apply_role/governance/management_actions", "value": "iam:UpdateAccountPasswordPolicy"})
}

test_violation_management_actions_empty if {
	management_malformed({"op": "replace", "path": "/deployment_identity/apply_role/governance/management_actions", "value": []})
}

test_violation_management_actions_empty_string if {
	management_malformed({"op": "replace", "path": "/deployment_identity/apply_role/governance/management_actions", "value": [""]})
}

test_violation_management_actions_not_string if {
	management_malformed({"op": "replace", "path": "/deployment_identity/apply_role/governance/management_actions", "value": [1]})
}

test_violation_management_actions_missing if {
	management_malformed({"op": "remove", "path": "/deployment_identity/apply_role/governance/management_actions"})
}

test_violation_management_policy_missing if {
	management_malformed({"op": "remove", "path": "/deployment_identity/apply_role/governance/management_policy"})
}

test_violation_management_policy_empty if {
	management_malformed({"op": "replace", "path": "/deployment_identity/apply_role/governance/management_policy", "value": ""})
}

member_messages_for(rcs, d) := msgs if {
	msgs := messages_for(rcs, d) with data.plan.lib.member_form as true
}

test_pass_member_form_without_governance_policy if {
	msgs := member_messages_for(without(world(without_management_deny), governance_policy_address), decl)
	not fixture.has("[PLAN-APPLY-00] data.policy_plan.deployment_identity.apply_role.governance.management_policy", msgs)
	not fixture.has(denied_06("iam:UpdateAccountPasswordPolicy"), msgs)
}

test_pass_member_form_does_not_require_management_actions if {
	not fixture.has(denied_06("iam:UpdateAccountPasswordPolicy"), member_messages_for(world(without_management_deny), decl))
	fixture.has(denied_06("iam:UpdateAccountPasswordPolicy"), messages_for(world(without_management_deny), decl))
}

test_violation_member_form_still_requires_governance_actions if {
	some a in governance_actions
	statements := [s | some s in valid_statements; not s == governance_deny]
	fixture.has(denied_06(a), member_messages_for(without(world(statements), governance_policy_address), decl))
}

package plan.deployment_identity_apply_identity_output_test

import data.plan.deployment_identity_apply_identity_output as p
import data.plan.fixture

decl := {"apply_identity_role": {
	"role": "module.x.aws_iam_role.apply_identity",
	"policy": "module.x.aws_iam_role_policy.apply_identity",
	"output_bucket": "module.s.aws_s3_bucket.plan",
	"output_bucket_policy": "module.s.aws_s3_bucket_policy.plan",
	"managed_policies": [],
}}

gdecl := {"apply_identity_role": object.union(decl.apply_identity_role, {"managed_policies": ["module.x.aws_iam_policy.governance[0]"]})}

b := "arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an"

apply_key := sprintf("%s/apply/*", [b])

write_apply := {"Effect": "Allow", "Action": "s3:PutObject", "Resource": apply_key}

rc(type, address, after) := {"address": address, "type": type, "change": {"actions": ["create"], "after": after, "after_unknown": {}}}

role := rc("aws_iam_role", "module.x.aws_iam_role.apply_identity", {"name": "ci-apply-identity", "inline_policy": [], "managed_policy_arns": []})

bucket := rc("aws_s3_bucket", "module.s.aws_s3_bucket.plan", {"arn": b, "id": "bkt-id", "bucket": "tabp-admin-tfplan-123456789012-ap-northeast-1-an"})

tls_deny := {"Sid": "DenyInsecureTransport", "Effect": "Deny", "Principal": "*", "Action": "s3:*", "Resource": [b, sprintf("%s/*", [b])], "Condition": {"Bool": {"aws:SecureTransport": "false"}}}

bucket_policy(statements) := object.union(fixture.doc("aws_s3_bucket_policy", "module.s.aws_s3_bucket_policy.plan", "policy", {"Statement": statements}), {"change": {"after": {"bucket": "bkt-id"}}})

state_side := [bucket, bucket_policy([tls_deny])]

policies_exclusive := rc("aws_iam_role_policies_exclusive", "module.x.aws_iam_role_policies_exclusive.this[\"apply_identity\"]", {"role_name": "ci-apply-identity", "policy_names": ["apply_identity"]})

attachments_exclusive := rc("aws_iam_role_policy_attachments_exclusive", "module.x.aws_iam_role_policy_attachments_exclusive.this[\"apply_identity\"]", {"role_name": "ci-apply-identity", "policy_arns": []})

policy(statements) := object.union(
	fixture.doc("aws_iam_role_policy", "module.x.aws_iam_role_policy.apply_identity", "policy", {"Statement": statements}),
	{"change": {"after": {"name": "apply_identity", "role": "ci-apply-identity"}}},
)

identity_side(statements) := [role, policies_exclusive, attachments_exclusive, policy(statements)]

with_policy(statements, others) := fixture.plan(array.concat(array.concat(identity_side(statements), state_side), others))

# 宣言は丸ごと差し替える（理由は lib/fixture.rego の inline_declaration）。
messages_for(inp, d) := msgs if {
	msgs := p.deny with input as inp with data.policy_plan as object.union(object.remove(data.policy_plan, ["deployment_identity"]), {"deployment_identity": d})
}

messages(statements) := messages_for(with_policy(statements, []), decl)

messages_with(statements, others) := messages_for(with_policy(statements, others), decl)

test_pass_apply_output_write_and_bucket_configuration if {
	configure := {"Effect": "Allow", "Action": ["s3:PutBucketPolicy", "s3:PutLifecycleConfiguration", "s3:ListBucket"], "Resource": b}
	other_bucket := {"Effect": "Allow", "Action": ["s3:PutObject", "s3:DeleteObject"], "Resource": "arn:aws:s3:::tabp-admin-tfstate/identity/terraform.tfstate.tflock"}
	logs := {"Effect": "Allow", "Action": "logs:PutResourcePolicy", "Resource": "*"}
	count(messages([write_apply, configure, other_bucket, logs])) == 0
}

test_pass_bucket_in_other_file if {
	inp := [
		{"path": "deployment-identity.json", "contents": {"plan_format_version": "1.2", "resource_changes": identity_side([write_apply])}},
		{"path": "state-backend.json", "contents": {"plan_format_version": "1.2", "resource_changes": state_side}},
	]
	count(messages_for(inp, decl)) == 0
}

test_pass_deny_statement_is_not_checked if {
	d := {"Effect": "Deny", "Action": "s3:*", "Resource": sprintf("%s/*", [b])}
	count(messages([write_apply, d])) == 0
}

test_pass_write_to_other_bucket if {
	s := {"Effect": "Allow", "Action": "s3:PutObject", "Resource": "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an/identity/terraform.tfstate"}
	count(messages([write_apply, s])) == 0
}

# 実際の module は、ほかの role にも policy と排他の2つと、引数を持つ role を作る。
test_pass_other_role_policy_is_not_checked if {
	other := object.union(
		fixture.doc("aws_iam_role_policy", "module.x.aws_iam_role_policy.this[\"plan\"]", "policy", {"Statement": [{"Effect": "Allow", "Action": "s3:PutObject", "Resource": sprintf("%s/pr/*", [b])}]}),
		{"change": {"after": {"role": "ci-plan"}}},
	)
	plan_role := rc("aws_iam_role", "module.x.aws_iam_role.this[\"plan\"]", {"name": "ci-plan", "inline_policy": [{"name": "x"}], "managed_policy_arns": ["arn:aws:iam::aws:policy/ReadOnlyAccess"]})
	plan_policies := rc("aws_iam_role_policies_exclusive", "module.x.aws_iam_role_policies_exclusive.this[\"plan\"]", {"role_name": "ci-plan", "policy_names": ["plan", "x"]})
	plan_attachments := rc("aws_iam_role_policy_attachments_exclusive", "module.x.aws_iam_role_policy_attachments_exclusive.this[\"plan\"]", {"role_name": "ci-plan", "policy_arns": ["arn:aws:iam::aws:policy/ReadOnlyAccess"]})
	count(messages_with([write_apply], [other, plan_role, plan_policies, plan_attachments])) == 0
}

test_pass_role_arguments_null_or_absent if {
	every after in [{"name": "ci-apply-identity", "inline_policy": null, "managed_policy_arns": null}, {"name": "ci-apply-identity"}] {
		r := rc("aws_iam_role", "module.x.aws_iam_role.apply_identity", after)
		count(messages_for(fixture.plan(array.concat([r, policies_exclusive, attachments_exclusive, policy([write_apply])], state_side)), decl)) == 0
	}
}

test_violation_declaration_not_object if {
	every v in ["x", ["x"], null] {
		fixture.has("[PLAN-AIO-00] data.policy_plan.deployment_identity.apply_identity_role が object ではありません", messages_for(with_policy([write_apply], []), {"apply_identity_role": v}))
	}
}

test_violation_declaration_missing if {
	every k in ["role", "policy", "output_bucket", "output_bucket_policy"] {
		every v in [null, "", 1] {
			d := {"apply_identity_role": object.union(decl.apply_identity_role, {k: v})}
			fixture.has(sprintf("[PLAN-AIO-00] data.policy_plan.deployment_identity.apply_identity_role.%s", [k]), messages_for(with_policy([write_apply], []), d))
		}
	}
}

test_violation_policy_missing if {
	fixture.has(sprintf("[PLAN-AIO-00] apply-identity の role の policy %q が入力にありません", [decl.apply_identity_role.policy]), messages_for(fixture.plan(array.concat([role], state_side)), decl))
}

test_violation_role_missing if {
	fixture.has("[PLAN-AIO-00] apply-identity の role \"", messages_for(fixture.plan(array.concat([policy([write_apply])], state_side)), decl))
}

test_violation_bucket_unreadable if {
	unknown := {"address": "module.s.aws_s3_bucket.plan", "type": "aws_s3_bucket", "change": {"actions": ["create"], "after": {}, "after_unknown": {"arn": true}}}
	every inp in [
		fixture.plan(identity_side([write_apply])),
		fixture.plan(array.concat(identity_side([write_apply]), [unknown, bucket_policy([tls_deny])])),
	] {
		fixture.has("[PLAN-AIO-00] plan の出力の Bucket", messages_for(inp, decl))
	}
}

test_violation_policy_not_on_role if {
	on_plan := object.union(policy([write_apply]), {"change": {"after": {"role": "ci-plan"}}})
	msgs := messages_for(fixture.plan(array.concat([role, policies_exclusive, attachments_exclusive, on_plan], state_side)), decl)
	fixture.has("[PLAN-AIO-00] apply-identity の role の policy \"module.x.aws_iam_role_policy.apply_identity\" が、apply-identity の role", msgs)
}

test_violation_not_action_or_not_resource if {
	every s in [
		{"Effect": "Allow", "NotAction": "s3:GetObject", "Resource": "*"},
		{"Effect": "Allow", "Action": "s3:PutObject", "NotResource": "arn:aws:s3:::x/*"},
	] {
		fixture.has("[PLAN-AIO-01]", messages([write_apply, s]))
	}
}

test_violation_other_s3_action_on_output_objects if {
	every a in ["s3:DeleteObject", "s3:DeleteObjectVersion", "s3:Delete*", "s3:Put*", "s3:*", "*", "s?:DeleteObject", "s3:PutObjectAcl", "s3:PutObjectTagging", "s3:GetObject", "s3:ReplicateObject"] {
		every r in [apply_key, sprintf("%s/apply/1/*", [b]), sprintf("%s/pr/*", [b]), sprintf("%s/foo/*", [b]), sprintf("%s/out.json", [b]), "arn:aws:s3:::*-tfplan-*/apply/1/*", "arn:aws:s3:::*", "*", "arn:aws:s3:::*pr/*", "arn:aws:s3:::*/apply/*", "*/*"] {
			fixture.has("[PLAN-AIO-02]", messages([write_apply, {"Effect": "Allow", "Action": a, "Resource": r}]))
		}
	}
}

test_violation_write_outside_apply_key if {
	every r in [
		sprintf("%s/pr/*", [b]),
		sprintf("%s/branch/*", [b]),
		sprintf("%s/pr/1/*", [b]),
		sprintf("%s/pr/1/plan.txt", [b]),
		sprintf("%s/branch/main/*", [b]),
		sprintf("%s/apply/1/*", [b]),
		sprintf("%s/applyx/*", [b]),
		sprintf("%s/foo/*", [b]),
		sprintf("%s/*", [b]),
		sprintf("%s/out*", [b]),
		sprintf("%s/apply*", [b]),
		sprintf("%s/apply/*.json", [b]),
		"arn:aws:s3:::*-tfplan-*/apply/*",
		"arn:aws:s3:::*",
		"*",
		"arn:aws:s3:::*pr/*",
		"arn:aws:s3:::*apply/*",
		"arn:aws:s3:::*/apply/*",
		"*/*",
	] {
		fixture.has("[PLAN-AIO-03]", messages([write_apply, {"Effect": "Allow", "Action": "s3:PutObject", "Resource": r}]))
	}
}

test_violation_apply_write_missing if {
	every s in [
		{"Effect": "Allow", "Action": "s3:ListBucket", "Resource": b},
		{"Effect": "Allow", "Action": "s3:PutObject", "Resource": "arn:aws:s3:::other/apply/*"},
	] {
		fixture.has("[PLAN-AIO-04]", messages([s]))
	}
}

# apply/* への書き込みが他の Action や Resource と同じ文に在ると、apply/* に何を許したかが文から読めない。
test_violation_apply_write_mixed if {
	every s in [
		{"Effect": "Allow", "Action": ["s3:PutObject", "s3:ListBucket"], "Resource": apply_key},
		{"Effect": "Allow", "Action": "s3:PutObject", "Resource": [apply_key, "arn:aws:s3:::other/*"]},
	] {
		fixture.has("[PLAN-AIO-04]", messages([s]))
	}
}

test_violation_other_policy_on_role if {
	extra := object.union(
		fixture.doc("aws_iam_role_policy", "module.x.aws_iam_role_policy.extra", "policy", {"Statement": [fixture.allow("s3:GetObject")]}),
		{"change": {"after": {"role": "ci-apply-identity"}}},
	)
	fixture.has("[PLAN-AIO-05] p.json: module.x.aws_iam_role_policy.extra", messages_with([write_apply], [extra]))
}

test_violation_managed_policy_on_role if {
	every att in [
		rc("aws_iam_role_policy_attachment", "module.x.aws_iam_role_policy_attachment.extra", {"role": "ci-apply-identity", "policy_arn": "arn:aws:iam::aws:policy/AdministratorAccess"}),
		rc("aws_iam_policy_attachment", "module.x.aws_iam_policy_attachment.extra", {"roles": ["ci-apply-identity"], "policy_arn": "arn:aws:iam::aws:policy/AdministratorAccess"}),
	] {
		fixture.has(sprintf("[PLAN-AIO-05] p.json: %s", [att.address]), messages_with([write_apply], [att]))
	}
}

test_violation_role_argument_policy if {
	every attr in ["inline_policy", "managed_policy_arns"] {
		r := object.union(role, {"change": {"after": {attr: ["x"]}}})
		msgs := messages_for(fixture.plan(array.concat([r, policies_exclusive, attachments_exclusive, policy([write_apply])], state_side)), decl)
		fixture.has(sprintf("[PLAN-AIO-05] p.json: apply-identity の role module.x.aws_iam_role.apply_identity の %s", [attr]), msgs)
	}
}

test_violation_attached_role_unknown if {
	every att in [
		{"address": "module.x.aws_iam_role_policy_attachment.extra", "type": "aws_iam_role_policy_attachment", "change": {"actions": ["create"], "after": {}, "after_unknown": {"role": true}}},
		{"address": "module.x.aws_iam_policy_attachment.extra", "type": "aws_iam_policy_attachment", "change": {"actions": ["create"], "after": {}, "after_unknown": {"roles": [true]}}},
	] {
		fixture.has(sprintf("[PLAN-AIO-05] p.json: %s がどの role", [att.address]), messages_with([write_apply], [att]))
	}
}

test_violation_exclusive_missing if {
	msgs := messages_for(fixture.plan(array.concat([role, policy([write_apply])], state_side)), decl)
	fixture.has("[PLAN-AIO-05] apply-identity の role ci-apply-identity の aws_iam_role_policies_exclusive が 0 件", msgs)
	fixture.has("[PLAN-AIO-05] apply-identity の role ci-apply-identity の aws_iam_role_policy_attachments_exclusive が 0 件", msgs)
}

test_violation_exclusive_wrong_contents if {
	wrong_names := object.union(policies_exclusive, {"change": {"after": {"policy_names": ["apply_identity", "extra"]}}})
	wrong_arns := object.union(attachments_exclusive, {"change": {"after": {"policy_arns": ["arn:aws:iam::aws:policy/AdministratorAccess"]}}})
	msgs := messages_for(fixture.plan(array.concat([role, wrong_names, wrong_arns, policy([write_apply])], state_side)), decl)
	fixture.has("[PLAN-AIO-05] p.json: module.x.aws_iam_role_policies_exclusive.this[\"apply_identity\"] の policy_names", msgs)
	fixture.has("[PLAN-AIO-05] p.json: module.x.aws_iam_role_policy_attachments_exclusive.this[\"apply_identity\"] の policy_arns", msgs)
}

test_violation_policy_name_unknown if {
	unnamed := json.patch(policy([write_apply]), [{"op": "remove", "path": "/change/after/name"}, {"op": "add", "path": "/change/after_unknown/name", "value": true}])
	msgs := messages_for(fixture.plan(array.concat([role, policies_exclusive, attachments_exclusive, unnamed], state_side)), decl)
	fixture.has(sprintf("[PLAN-AIO-05] p.json: %s の policy_names が [null] ではありません", [policies_exclusive.address]), msgs)
}

# IAM の * は "/" を越えるので、Bucket 名の位置の wildcard が key まで届く。[ と { は IAM では文字そのものである。
test_violation_wildcard_spanning_bucket_and_key if {
	every s in [
		{"Effect": "Allow", "Action": "s3:DeleteObject", "Resource": "arn:aws:s3:::*apply/*"},
		{"Effect": "Allow", "Action": "s3:DeleteObject", "Resource": sprintf("%s*json", [b])},
		{"Effect": "Allow", "Action": ["s3:DeleteObject", "s3:DeleteObjectVersion"], "Resource": "arn:aws:s3:::*.tfplan"},
		{"Effect": "Allow", "Action": "s3:DeleteObject", "Resource": "arn:aws:s3:::*[/*"},
		{"Effect": "Allow", "Action": "s3:DeleteObject", "Resource": "arn:aws:s3:::*{/*"},
	] {
		fixture.has("[PLAN-AIO-02]", messages([write_apply, s]))
	}
	fixture.has("[PLAN-AIO-03]", messages([write_apply, {"Effect": "Allow", "Action": "s3:PutObject", "Resource": "arn:aws:s3:::*plan/*"}]))
}

# s[3:* は IAM では S3 の Action ではなく、. と + は Bucket 名の文字である。
test_pass_regex_characters_outside_output_bucket if {
	every s in [
		{"Effect": "Allow", "Action": "s3:DeleteObject", "Resource": "arn:aws:s3:::tabp.admin+tfstate/identity/terraform.tfstate.tflock"},
		{"Effect": "Allow", "Action": "s[3:DeleteObject", "Resource": "*"},
	] {
		count(messages([write_apply, s])) == 0
	}
}

test_violation_role_name_case if {
	extra := object.union(
		fixture.doc("aws_iam_role_policy", "module.x.aws_iam_role_policy.extra", "policy", {"Statement": [fixture.allow("s3:GetObject")]}),
		{"change": {"after": {"role": "CI-APPLY-IDENTITY"}}},
	)
	att := rc("aws_iam_policy_attachment", "module.x.aws_iam_policy_attachment.extra", {"roles": ["Ci-Apply-Identity"], "policy_arn": "arn:aws:iam::aws:policy/AdministratorAccess"})
	msgs := messages_with([write_apply], [extra, att])
	fixture.has("[PLAN-AIO-05] p.json: module.x.aws_iam_role_policy.extra", msgs)
	fixture.has("[PLAN-AIO-05] p.json: module.x.aws_iam_policy_attachment.extra", msgs)
}

test_violation_bucket_policy_allow if {
	allow := {"Sid": "X", "Effect": "Allow", "Principal": {"AWS": "arn:aws:iam::123456789012:role/ci-apply-identity"}, "Action": "s3:DeleteObject", "Resource": sprintf("%s/*", [b])}
	inp := fixture.plan(array.concat(identity_side([write_apply]), [bucket, bucket_policy([tls_deny, allow])]))
	fixture.has("[PLAN-AIO-06] p.json: plan の出力の Bucket の Bucket Policy module.s.aws_s3_bucket_policy.plan が Deny でない文 X", messages_for(inp, decl))
}

test_violation_bucket_policy_missing if {
	inp := fixture.plan(array.concat(identity_side([write_apply]), [bucket]))
	fixture.has("[PLAN-AIO-00] plan の出力の Bucket の Bucket Policy", messages_for(inp, decl))
}

unknown_attr(r, attr) := {"address": r.address, "type": r.type, "change": {"actions": ["create"], "after": object.remove(r.change.after, [attr]), "after_unknown": {attr: true}}}

test_violation_unknown_attributes if {
	every inp in [
		array.concat([unknown_attr(role, "inline_policy"), policies_exclusive, attachments_exclusive, policy([write_apply])], state_side),
		array.concat([unknown_attr(role, "managed_policy_arns"), policies_exclusive, attachments_exclusive, policy([write_apply])], state_side),
		array.concat([role, unknown_attr(policies_exclusive, "policy_names"), attachments_exclusive, policy([write_apply])], state_side),
		array.concat([role, policies_exclusive, unknown_attr(attachments_exclusive, "policy_arns"), policy([write_apply])], state_side),
	] {
		fixture.has("[PLAN-AIO-05]", messages_for(fixture.plan(inp), decl))
	}
	unknown_role := unknown_attr(policies_exclusive, "role_name")
	fixture.has(sprintf("[PLAN-AIO-05] p.json: %s がどの role", [unknown_role.address]), messages_for(fixture.plan(array.concat([role, unknown_role, attachments_exclusive, policy([write_apply])], state_side)), decl))
}

test_violation_policy_role_absent if {
	no_role := {"address": "module.x.aws_iam_role_policy.apply_identity", "type": "aws_iam_role_policy", "change": {"actions": ["create"], "after": {"name": "apply_identity", "policy": json.marshal({"Statement": [write_apply]})}, "after_unknown": {}}}
	fixture.has("[PLAN-AIO-00] apply-identity の role の policy \"module.x.aws_iam_role_policy.apply_identity\" が、apply-identity の role", messages_for(fixture.plan(array.concat([role, policies_exclusive, attachments_exclusive, no_role], state_side)), decl))
}

test_violation_exclusive_duplicated if {
	second := rc("aws_iam_role_policies_exclusive", "module.x.aws_iam_role_policies_exclusive.extra", {"role_name": "ci-apply-identity", "policy_names": ["apply_identity"]})
	fixture.has("[PLAN-AIO-05] apply-identity の role ci-apply-identity の aws_iam_role_policies_exclusive が 2 件", messages_with([write_apply], [second]))
}

test_violation_policy_variable_in_resource if {
	every r in ["arn:aws:s3:::tabp-admin-tfplan-${aws:PrincipalAccount}-ap-northeast-1-an/*", "arn:aws:s3:::${aws:PrincipalTag/bucket}/*"] {
		fixture.has("[PLAN-AIO-01]", messages([write_apply, {"Effect": "Allow", "Action": "s3:DeleteObject", "Resource": r}]))
	}
}

# --- 宣言した customer managed policy（deployment-identity/ADR-0201 決定7） ---

governance_arn := "arn:aws:iam::123456789012:policy/ci-apply-identity-governance"

governance(statements) := object.union(
	fixture.doc("aws_iam_policy", "module.x.aws_iam_policy.governance[0]", "policy", {"Statement": statements}),
	{"change": {"after": {"arn": governance_arn}}},
)

attached_governance := object.union(attachments_exclusive, {"change": {"after": {"policy_arns": [governance_arn]}}})

with_governance(statements, attachments) := fixture.plan(array.concat([role, policies_exclusive, attachments, policy([write_apply]), governance(statements)], state_side))

create_org := {"Effect": "Allow", "Action": "organizations:CreateOrganization", "Resource": "*"}

test_pass_governance_policy_attached if {
	count(messages_for(with_governance([create_org], attached_governance), gdecl)) == 0
}

governance_attachment(arn) := rc("aws_iam_role_policy_attachment", "module.x.aws_iam_role_policy_attachment.governance[0]", {"role": "ci-apply-identity", "policy_arn": arn})

with_governance_attachment(arn) := fixture.plan(array.concat([role, policies_exclusive, attached_governance, governance_attachment(arn), policy([write_apply]), governance([create_org])], state_side))

test_pass_governance_policy_attachment if {
	count(messages_for(with_governance_attachment(governance_arn), gdecl)) == 0
}

test_violation_attachment_of_undeclared_policy if {
	fixture.has("[PLAN-AIO-05] p.json: module.x.aws_iam_role_policy_attachment.governance[0] が apply-identity の role に付ける policy が", messages_for(with_governance_attachment("arn:aws:iam::aws:policy/AdministratorAccess"), gdecl))
}

test_pass_no_managed_policies_declared if {
	count(messages_for(with_policy([write_apply], []), decl)) == 0
}

test_violation_governance_policy_not_attached if {
	fixture.has("[PLAN-AIO-05] p.json: module.x.aws_iam_role_policy_attachments_exclusive.this[\"apply_identity\"] の policy_arns が、宣言した customer managed policy の ARN", messages_for(with_governance([create_org], attachments_exclusive), gdecl))
}

test_violation_undeclared_managed_policy_attached if {
	d := decl
	fixture.has("[PLAN-AIO-05] p.json: module.x.aws_iam_role_policy_attachments_exclusive.this[\"apply_identity\"] の policy_arns が", messages_for(with_governance([create_org], attached_governance), d))
}

test_violation_governance_policy_reads_output if {
	read := {"Effect": "Allow", "Action": "s3:GetObject", "Resource": sprintf("%s/*", [b])}
	fixture.has("[PLAN-AIO-02]", messages_for(with_governance([create_org, read], attached_governance), gdecl))
}

test_violation_governance_policy_writes_outside_apply if {
	write := {"Effect": "Allow", "Action": "s3:PutObject", "Resource": sprintf("%s/pr/*", [b])}
	fixture.has("[PLAN-AIO-03]", messages_for(with_governance([create_org, write], attached_governance), gdecl))
}

test_violation_governance_policy_arn_unknown if {
	unknown := json.patch(governance([create_org]), [
		{"op": "remove", "path": "/change/after/arn"},
		{"op": "add", "path": "/change/after_unknown/arn", "value": true},
	])
	inp := fixture.plan(array.concat([role, policies_exclusive, attachments_exclusive, policy([write_apply]), unknown], state_side))
	fixture.has("[PLAN-AIO-00] customer managed policy module.x.aws_iam_policy.governance[0] の ARN が plan で決まっていません", messages_for(inp, gdecl))
}

test_violation_managed_policies_malformed if {
	every v in ["module.x.aws_iam_policy.governance[0]", [""], [1], null] {
		d := {"apply_identity_role": object.union(decl.apply_identity_role, {"managed_policies": v})}
		fixture.has("[PLAN-AIO-00] data.policy_plan.deployment_identity.apply_identity_role.managed_policies", messages_for(with_policy([write_apply], []), d))
	}
}

test_violation_declared_managed_policy_absent if {
	fixture.has("[PLAN-AIO-00] data.policy_plan.deployment_identity.apply_identity_role.managed_policies の \"module.x.aws_iam_policy.governance[0]\" が、aws_iam_policy として入力にありません", messages_for(with_policy([write_apply], []), gdecl))
}

test_violation_declared_managed_policy_wrong_type if {
	wrong := rc("aws_iam_role_policy", "module.x.aws_iam_policy.governance[0]", {"name": "x", "role": "other"})
	inp := fixture.plan(array.concat([role, policies_exclusive, attachments_exclusive, policy([write_apply]), wrong], state_side))
	fixture.has("[PLAN-AIO-00] data.policy_plan.deployment_identity.apply_identity_role.managed_policies の \"module.x.aws_iam_policy.governance[0]\" が、aws_iam_policy として入力にありません", messages_for(inp, gdecl))
}

test_violation_managed_policies_key_missing if {
	d := {"apply_identity_role": object.remove(decl.apply_identity_role, ["managed_policies"])}
	fixture.has("[PLAN-AIO-00] data.policy_plan.deployment_identity.apply_identity_role.managed_policies", messages_for(with_policy([write_apply], []), d))
}

test_violation_managed_policies_mixed if {
	every v in [["module.x.aws_iam_policy.governance[0]", 1], {}, [["x"]]] {
		d := {"apply_identity_role": object.union(decl.apply_identity_role, {"managed_policies": v})}
		fixture.has("[PLAN-AIO-00] data.policy_plan.deployment_identity.apply_identity_role.managed_policies が", messages_for(with_policy([write_apply], []), d))
	}
}

test_violation_governance_policy_arns_element_unknown if {
	partial := json.patch(attached_governance, [{"op": "add", "path": "/change/after_unknown/policy_arns", "value": [false, true]}])
	fixture.has("[PLAN-AIO-05] p.json: module.x.aws_iam_role_policy_attachments_exclusive.this[\"apply_identity\"] の policy_arns に plan で決まらない ARN があります", messages_for(with_governance([create_org], partial), gdecl))
}

test_violation_governance_policy_not_action if {
	fixture.has("[PLAN-AIO-01]", messages_for(with_governance([{"Effect": "Allow", "NotAction": "s3:*", "Resource": "*"}], attached_governance), gdecl))
}

test_violation_governance_policy_variable_in_resource if {
	fixture.has("[PLAN-AIO-01]", messages_for(with_governance([{"Effect": "Allow", "Action": "s3:PutObject", "Resource": "${aws:username}/*"}], attached_governance), gdecl))
}

# 決定10 の apply/* への書き込みは、apply-identity の role のどの policy に在ってもよい。
test_pass_apply_write_in_governance_policy if {
	inp := fixture.plan(array.concat([role, policies_exclusive, attached_governance, policy([]), governance([create_org, write_apply])], state_side))
	count(messages_for(inp, gdecl)) == 0
}

test_pass_two_managed_policies if {
	second_arn := "arn:aws:iam::123456789012:policy/ci-apply-identity-governance-2"
	second := object.union(fixture.doc("aws_iam_policy", "module.x.aws_iam_policy.governance[1]", "policy", {"Statement": [create_org]}), {"change": {"after": {"arn": second_arn}}})
	both := object.union(attachments_exclusive, {"change": {"after": {"policy_arns": [governance_arn, second_arn]}}})
	d := {"apply_identity_role": object.union(decl.apply_identity_role, {"managed_policies": ["module.x.aws_iam_policy.governance[0]", "module.x.aws_iam_policy.governance[1]"]})}
	inp := fixture.plan(array.concat([role, policies_exclusive, both, policy([write_apply]), governance([create_org]), second], state_side))
	count(messages_for(inp, d)) == 0
}

test_violation_two_managed_policies_one_unattached if {
	second_arn := "arn:aws:iam::123456789012:policy/ci-apply-identity-governance-2"
	second := object.union(fixture.doc("aws_iam_policy", "module.x.aws_iam_policy.governance[1]", "policy", {"Statement": [create_org]}), {"change": {"after": {"arn": second_arn}}})
	d := {"apply_identity_role": object.union(decl.apply_identity_role, {"managed_policies": ["module.x.aws_iam_policy.governance[0]", "module.x.aws_iam_policy.governance[1]"]})}
	inp := fixture.plan(array.concat([role, policies_exclusive, attached_governance, policy([write_apply]), governance([create_org]), second], state_side))
	fixture.has("[PLAN-AIO-05] p.json: module.x.aws_iam_role_policy_attachments_exclusive.this[\"apply_identity\"] の policy_arns が、宣言した customer managed policy の ARN", messages_for(inp, d))
}

test_violation_second_managed_policy_reads_output if {
	second_arn := "arn:aws:iam::123456789012:policy/ci-apply-identity-governance-2"
	read := {"Effect": "Allow", "Action": "s3:GetObject", "Resource": sprintf("%s/*", [b])}
	second := object.union(fixture.doc("aws_iam_policy", "module.x.aws_iam_policy.governance[1]", "policy", {"Statement": [read]}), {"change": {"after": {"arn": second_arn}}})
	both := object.union(attachments_exclusive, {"change": {"after": {"policy_arns": [governance_arn, second_arn]}}})
	d := {"apply_identity_role": object.union(decl.apply_identity_role, {"managed_policies": ["module.x.aws_iam_policy.governance[0]", "module.x.aws_iam_policy.governance[1]"]})}
	inp := fixture.plan(array.concat([role, policies_exclusive, both, policy([write_apply]), governance([create_org]), second], state_side))
	fixture.has("[PLAN-AIO-02]", messages_for(inp, d))
}

test_violation_attachments_exclusive_duplicated if {
	dup := object.union(attachments_exclusive, {"address": "module.x.aws_iam_role_policy_attachments_exclusive.other"})
	fixture.has("[PLAN-AIO-05] apply-identity の role ci-apply-identity の aws_iam_role_policy_attachments_exclusive が 2 件", messages_for(with_policy([write_apply], [dup]), decl))
}

test_violation_attachment_policy_arn_unknown if {
	unknown := {"address": "module.x.aws_iam_role_policy_attachment.governance[0]", "type": "aws_iam_role_policy_attachment", "change": {"actions": ["create"], "after": {"role": "ci-apply-identity"}, "after_unknown": {"policy_arn": true}}}
	msgs := messages_for(fixture.plan(array.concat([role, policies_exclusive, attached_governance, unknown, policy([write_apply]), governance([create_org])], state_side)), gdecl)
	fixture.has("[PLAN-AIO-05] p.json: module.x.aws_iam_role_policy_attachment.governance[0] の policy_arn を plan で読めません", msgs)
	not fixture.has("[PLAN-AIO-05] p.json: module.x.aws_iam_role_policy_attachment.governance[0] が apply-identity の role に付ける policy が", msgs)
}

test_pass_attachment_on_other_role if {
	other := rc("aws_iam_role_policy_attachment", "module.y.aws_iam_role_policy_attachment.other", {"role": "other", "policy_arn": "arn:aws:iam::aws:policy/ReadOnlyAccess"})
	msgs := messages_for(fixture.plan(array.concat([role, policies_exclusive, attached_governance, governance_attachment(governance_arn), other, policy([write_apply]), governance([create_org])], state_side)), gdecl)
	count([m | some m in msgs; startswith(m, "[PLAN-AIO-05]")]) == 0
}

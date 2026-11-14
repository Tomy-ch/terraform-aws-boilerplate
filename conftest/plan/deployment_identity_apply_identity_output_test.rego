package plan.deployment_identity_apply_identity_output_test

import data.plan.deployment_identity_apply_identity_output as p
import data.plan.fixture

decl := {"apply_identity_role": {
	"role": "module.x.aws_iam_role.apply_identity",
	"policy": "module.x.aws_iam_role_policy.apply_identity",
	"output_bucket": "module.s.aws_s3_bucket.plan",
}}

b := "arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an"

apply_key := sprintf("%s/apply/*", [b])

write_apply := {"Effect": "Allow", "Action": "s3:PutObject", "Resource": apply_key}

rc(type, address, after) := {"address": address, "type": type, "change": {"actions": ["create"], "after": after, "after_unknown": {}}}

role := rc("aws_iam_role", "module.x.aws_iam_role.apply_identity", {"name": "ci-apply-identity"})

bucket := rc("aws_s3_bucket", "module.s.aws_s3_bucket.plan", {"arn": b})

policy(statements) := object.union(
	fixture.doc("aws_iam_role_policy", "module.x.aws_iam_role_policy.apply_identity", "policy", {"Statement": statements}),
	{"change": {"after": {"name": "apply_identity", "role": "ci-apply-identity"}}},
)

with_policy(statements, others) := fixture.plan(array.concat([role, bucket, policy(statements)], others))

# 宣言は丸ごと差し替える。子のパスだけを with で差し替えると、規則が親から読むときに decl.json の値と重なる。
messages_for(inp, d) := msgs if {
	msgs := p.deny with input as inp with data.policy_plan as object.union(object.remove(data.policy_plan, ["deployment_identity"]), {"deployment_identity": d})
}

messages(statements) := messages_for(with_policy(statements, []), decl)

test_pass_apply_output_write_and_bucket_configuration if {
	configure := {"Effect": "Allow", "Action": ["s3:PutBucketPolicy", "s3:PutLifecycleConfiguration"], "Resource": b}
	other_bucket := {"Effect": "Allow", "Action": ["s3:PutObject", "s3:DeleteObject"], "Resource": "arn:aws:s3:::tabp-admin-tfstate/identity/terraform.tfstate.tflock"}
	count(messages([write_apply, configure, other_bucket])) == 0
}

test_pass_deny_statement_is_not_checked if {
	d := {"Effect": "Deny", "Action": "s3:*", "Resource": sprintf("%s/*", [b])}
	count(messages([write_apply, d])) == 0
}

test_violation_declaration_missing if {
	every k in ["role", "policy", "output_bucket"] {
		d := {"apply_identity_role": object.remove(decl.apply_identity_role, [k])}
		msgs := messages_for(with_policy([write_apply], []), d)
		fixture.has(sprintf("[PLAN-AIO-00] data.policy_plan.deployment_identity.apply_identity_role.%s", [k]), msgs)
	}
}

test_violation_policy_missing if {
	msgs := messages_for(fixture.plan([role, bucket]), decl)
	fixture.has("[PLAN-AIO-00] apply-identity の role の policy", msgs)
}

test_violation_role_missing if {
	msgs := messages_for(fixture.plan([bucket, policy([write_apply])]), decl)
	fixture.has("[PLAN-AIO-00] apply-identity の role \"", msgs)
}

# Bucket が読めないと、下の規則がすべて黙る。
test_violation_bucket_unreadable if {
	msgs := messages_for(fixture.plan([role, policy([write_apply])]), decl)
	fixture.has("[PLAN-AIO-00] plan の出力の Bucket", msgs)
}

test_violation_not_action_or_not_resource if {
	every s in [
		{"Effect": "Allow", "NotAction": "s3:GetObject", "Resource": "*"},
		{"Effect": "Allow", "Action": "s3:PutObject", "NotResource": "arn:aws:s3:::x/*"},
	] {
		fixture.has("[PLAN-AIO-01]", messages([write_apply, s]))
	}
}

test_violation_write_outside_apply_key if {
	every r in [
		sprintf("%s/pr/*", [b]),
		sprintf("%s/branch/*", [b]),
		sprintf("%s/*", [b]),
		sprintf("%s/apply*", [b]),
		"arn:aws:s3:::*-tfplan-*/*",
		"arn:aws:s3:::*",
		"*",
	] {
		fixture.has("[PLAN-AIO-02]", messages([write_apply, {"Effect": "Allow", "Action": "s3:PutObject", "Resource": r}]))
	}
}

test_violation_write_via_action_wildcard if {
	every a in ["s3:Put*", "s3:*", "*", "S3:PUTOBJECT"] {
		fixture.has("[PLAN-AIO-02]", messages([write_apply, {"Effect": "Allow", "Action": a, "Resource": sprintf("%s/pr/*", [b])}]))
	}
}

test_violation_delete if {
	every a in ["s3:DeleteObject", "s3:DeleteObjectVersion", "s3:Delete*", "s3:*"] {
		fixture.has("[PLAN-AIO-03]", messages([write_apply, {"Effect": "Allow", "Action": a, "Resource": apply_key}]))
	}
}

test_violation_apply_write_missing if {
	fixture.has("[PLAN-AIO-04]", messages([{"Effect": "Allow", "Action": "s3:ListBucket", "Resource": b}]))
}

# apply/* への書き込みが他の Action と同じ文に在ると、apply/* に何を許したかが文から読めない。
test_violation_apply_write_mixed_with_other_actions if {
	s := {"Effect": "Allow", "Action": ["s3:PutObject", "s3:GetObject"], "Resource": apply_key}
	fixture.has("[PLAN-AIO-04]", messages([s]))
}

test_violation_other_policy_on_role if {
	extra := object.union(
		fixture.doc("aws_iam_role_policy", "module.x.aws_iam_role_policy.extra", "policy", {"Statement": [fixture.allow("s3:GetObject")]}),
		{"change": {"after": {"role": "ci-apply-identity"}}},
	)
	msgs := messages_for(with_policy([write_apply], [extra]), decl)
	fixture.has("[PLAN-AIO-05] p.json: module.x.aws_iam_role_policy.extra", msgs)
}

test_violation_managed_policy_on_role if {
	att := rc("aws_iam_role_policy_attachment", "module.x.aws_iam_role_policy_attachment.extra", {"role": "ci-apply-identity", "policy_arn": "arn:aws:iam::aws:policy/AdministratorAccess"})
	msgs := messages_for(with_policy([write_apply], [att]), decl)
	fixture.has("[PLAN-AIO-05]", msgs)
}

test_violation_attached_role_unknown if {
	att := {"address": "module.x.aws_iam_role_policy_attachment.extra", "type": "aws_iam_role_policy_attachment", "change": {"actions": ["create"], "after": {}, "after_unknown": {"role": true}}}
	msgs := messages_for(with_policy([write_apply], [att]), decl)
	fixture.has("[PLAN-AIO-05] p.json: module.x.aws_iam_role_policy_attachment.extra がどの role", msgs)
}

test_pass_other_role_policy_is_not_checked if {
	other := object.union(
		fixture.doc("aws_iam_role_policy", "module.x.aws_iam_role_policy.this[\"plan\"]", "policy", {"Statement": [{"Effect": "Allow", "Action": "s3:PutObject", "Resource": sprintf("%s/pr/*", [b])}]}),
		{"change": {"after": {"role": "ci-plan"}}},
	)
	count(messages_for(with_policy([write_apply], [other]), decl)) == 0
}

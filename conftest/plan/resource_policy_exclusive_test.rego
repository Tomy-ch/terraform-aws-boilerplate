package plan.resource_policy_exclusive_test

import data.plan.fixture
import data.plan.resource_policy_exclusive as gate

state_bucket := "module.state_backend.module.s3.aws_s3_bucket.this"

state_bucket_policy := "module.state_backend.module.s3.aws_s3_bucket_policy.this"

output_bucket := "module.state_backend.module.plan_output.aws_s3_bucket.this"

output_bucket_policy := "module.state_backend.module.plan_output.aws_s3_bucket_policy.this"

state_key := "module.state_backend.module.kms.aws_kms_key.this"

key_policy := "module.state_backend.module.kms.aws_kms_key_policy.this"

key_arn := "arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"

# mock の plan と同じく、id は名前や key_id と別の値になる。
rc(type, address, after) := {"address": address, "type": type, "change": {"actions": ["create"], "after": after, "after_unknown": {}}}

targets := [
	rc("aws_s3_bucket", state_bucket, {"id": "state-id", "bucket": "tabp-admin-tfstate", "arn": "arn:aws:s3:::tabp-admin-tfstate"}),
	rc("aws_s3_bucket", output_bucket, {"id": "plan-id", "bucket": "tabp-admin-tfplan", "arn": "arn:aws:s3:::tabp-admin-tfplan"}),
	rc("aws_kms_key", state_key, {"id": "key-id", "key_id": "key-key-id", "arn": key_arn}),
]

declared := [
	rc("aws_s3_bucket_policy", state_bucket_policy, {"bucket": "state-id"}),
	rc("aws_s3_bucket_policy", output_bucket_policy, {"bucket": "plan-id"}),
	rc("aws_kms_key_policy", key_policy, {"key_id": "key-id"}),
]

messages(extra) := msgs if {
	msgs := gate.deny with input as fixture.plan(array.concat(array.concat(targets, declared), extra))
}

unknown(type, address, attr) := {"address": address, "type": type, "change": {"actions": ["create"], "after": {}, "after_unknown": {attr: true}}}

test_pass_declared_policies_on_their_resources if {
	count(messages([])) == 0
}

test_violation_undeclared_bucket_policy_on_a_declared_bucket if {
	every ref in ["state-id", "tabp-admin-tfstate"] {
		evil := rc("aws_s3_bucket_policy", "module.evil.aws_s3_bucket_policy.x", {"bucket": ref})
		fixture.has(sprintf("[PLAN-RPE-01] p.json: module.evil.aws_s3_bucket_policy.x が %s を指します", [state_bucket]), messages([evil]))
	}
	every ref in ["plan-id", "tabp-admin-tfplan"] {
		evil := rc("aws_s3_bucket_policy", "module.evil.aws_s3_bucket_policy.x", {"bucket": ref})
		fixture.has(sprintf("[PLAN-RPE-01] p.json: module.evil.aws_s3_bucket_policy.x が %s を指します", [output_bucket]), messages([evil]))
	}
}

test_violation_undeclared_key_policy_on_the_state_key if {
	every ref in ["key-id", "key-key-id", key_arn] {
		evil := rc("aws_kms_key_policy", "module.evil.aws_kms_key_policy.x", {"key_id": ref})
		fixture.has(sprintf("[PLAN-RPE-01] p.json: module.evil.aws_kms_key_policy.x が %s を指します", [state_key]), messages([evil]))
	}
}

# bootstrap の module の下の2つ目の Key Policy は PLAN-APPLY-00 も落とす。ここでも落とす。
test_violation_second_key_policy_in_the_same_module if {
	dup := rc("aws_kms_key_policy", "module.state_backend.module.kms.aws_kms_key_policy.dup", {"key_id": "key-id"})
	fixture.has("[PLAN-RPE-01] p.json: module.state_backend.module.kms.aws_kms_key_policy.dup が ", messages([dup]))
}

test_violation_undeclared_policy_with_unknown_target if {
	every u in [unknown("aws_s3_bucket_policy", "module.evil.aws_s3_bucket_policy.x", "bucket"), unknown("aws_kms_key_policy", "module.evil.aws_kms_key_policy.x", "key_id")] {
		msgs := messages([u])
		fixture.has(sprintf("[PLAN-RPE-01] p.json: %s がどの resource を指すかを plan で読めません", [u.address]), msgs)
		count(msgs) == 1
	}
}

test_pass_undeclared_policy_on_other_resources if {
	others := [
		rc("aws_s3_bucket_policy", "module.other.aws_s3_bucket_policy.x", {"bucket": "other-id"}),
		rc("aws_kms_key_policy", "module.other.aws_kms_key_policy.x", {"key_id": "other-key"}),
	]
	count(messages(others)) == 0
}

test_pass_forget_and_data_source if {
	gone := [
		{"address": "module.evil.aws_s3_bucket_policy.x", "type": "aws_s3_bucket_policy", "change": {"actions": ["forget"], "before": {"bucket": "state-id"}, "after": null}},
		{"address": "module.evil.aws_kms_key_policy.x", "type": "aws_kms_key_policy", "change": {"actions": ["delete"], "before": {"key_id": "key-id"}, "after": null}},
		object.union(rc("aws_s3_bucket_policy", "data.aws_s3_bucket_policy.x", {"bucket": "state-id"}), {"mode": "data"}),
	]
	count(messages(gone)) == 0
}

test_violation_declared_policy_on_another_resource if {
	moved := [
		rc("aws_s3_bucket_policy", state_bucket_policy, {"bucket": "other-id"}),
		rc("aws_s3_bucket_policy", output_bucket_policy, {"bucket": "state-id"}),
		rc("aws_kms_key_policy", key_policy, {"key_id": "other-key"}),
	]
	msgs := gate.deny with input as fixture.plan(array.concat(targets, moved))
	fixture.has(sprintf("[PLAN-RPE-02] p.json: 宣言した %s が %s を指していません", [state_bucket_policy, state_bucket]), msgs)
	fixture.has(sprintf("[PLAN-RPE-02] p.json: 宣言した %s が %s を指していません", [output_bucket_policy, output_bucket]), msgs)
	fixture.has(sprintf("[PLAN-RPE-02] p.json: 宣言した %s が %s を指していません", [key_policy, state_key]), msgs)
}

# 宣言した policy が、決まらない値で既存の resource を指すこともある。参照先が決まっていれば落とす。
test_violation_declared_policy_unknown_on_a_known_target if {
	msgs := gate.deny with input as fixture.plan(array.concat(targets, [unknown("aws_kms_key_policy", key_policy, "key_id")]))
	fixture.has(sprintf("[PLAN-RPE-02] p.json: 宣言した %s が ", [key_policy]), msgs)
}

# 参照先と宣言した policy を同じ plan で作ると、どちらの値も plan で決まらない。
test_pass_declared_policy_created_with_its_target if {
	new_key := {"address": state_key, "type": "aws_kms_key", "change": {"actions": ["create"], "after": {}, "after_unknown": {"id": true, "key_id": true, "arn": true}}}
	inp := fixture.plan([new_key, unknown("aws_kms_key_policy", key_policy, "key_id")])
	count(gate.deny) == 0 with input as inp
}

# 宣言が無ければ何も見ない。宣言の欠けは各 package の PLAN-*-00 が落とす。
test_pass_declaration_absent if {
	evil := rc("aws_s3_bucket_policy", "module.evil.aws_s3_bucket_policy.x", {"bucket": "state-id"})
	count(gate.deny) == 0 with input as fixture.plan(array.concat(targets, [evil])) with data.policy_plan as {}
}

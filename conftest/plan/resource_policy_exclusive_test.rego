package plan.resource_policy_exclusive_test

import data.plan.fixture
import data.plan.resource_policy_exclusive as gate

state_bucket := "module.state_backend.module.s3.aws_s3_bucket.this"

state_bucket_policy := "module.state_backend.module.s3.aws_s3_bucket_policy.this"

output_bucket := "module.state_backend.module.plan_output.aws_s3_bucket.this"

output_bucket_policy := "module.state_backend.module.plan_output.aws_s3_bucket_policy.this"

apply_bucket := "module.state_backend.module.apply_output.aws_s3_bucket.this"

apply_bucket_policy := "module.state_backend.module.apply_output.aws_s3_bucket_policy.this"

state_key := "module.state_backend.module.kms.aws_kms_key.this"

key_policy := "module.state_backend.module.kms.aws_kms_key_policy.this"

key_arn := "arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"

# mock の plan と同じく、id は名前や key_id と別の値になる。
rc(type, address, after) := {"address": address, "type": type, "change": {"actions": ["create"], "after": after, "after_unknown": {}}}

targets := [
	rc("aws_s3_bucket", state_bucket, {"id": "state-id", "bucket": "tabp-admin-tfstate", "arn": "arn:aws:s3:::tabp-admin-tfstate"}),
	rc("aws_s3_bucket", output_bucket, {"id": "plan-id", "bucket": "tabp-admin-tfplan", "arn": "arn:aws:s3:::tabp-admin-tfplan"}),
	rc("aws_kms_key", state_key, {"id": "key-id", "key_id": "key-key-id", "arn": key_arn}),
	rc("aws_s3_bucket", apply_bucket, {"id": "apply-id", "bucket": "tabp-admin-tfapply", "arn": "arn:aws:s3:::tabp-admin-tfapply"}),
]

declared := [
	rc("aws_s3_bucket_policy", state_bucket_policy, {"bucket": "state-id"}),
	rc("aws_s3_bucket_policy", output_bucket_policy, {"bucket": "plan-id"}),
	rc("aws_kms_key_policy", key_policy, {"key_id": "key-id"}),
	rc("aws_s3_bucket_policy", apply_bucket_policy, {"bucket": "apply-id"}),
]

messages(extra) := msgs if {
	msgs := gate.deny with input as fixture.plan(array.concat(array.concat(targets, declared), extra))
}

unknown(type, address, attr) := {"address": address, "type": type, "change": {"actions": ["create"], "after": {}, "after_unknown": {attr: true}}}

test_pass_declared_policies_on_their_resources if {
	count(messages([])) == 0
}

test_violation_undeclared_bucket_policy_on_a_declared_bucket if {
	every ref in ["state-id", "tabp-admin-tfstate", "arn:aws:s3:::tabp-admin-tfstate"] {
		evil := rc("aws_s3_bucket_policy", "module.evil.aws_s3_bucket_policy.x", {"bucket": ref})
		fixture.has(sprintf("[PLAN-RPE-01] p.json: module.evil.aws_s3_bucket_policy.x が %s を指します", [state_bucket]), messages([evil]))
	}
	every ref in ["plan-id", "tabp-admin-tfplan", "arn:aws:s3:::tabp-admin-tfplan"] {
		evil := rc("aws_s3_bucket_policy", "module.evil.aws_s3_bucket_policy.x", {"bucket": ref})
		fixture.has(sprintf("[PLAN-RPE-01] p.json: module.evil.aws_s3_bucket_policy.x が %s を指します", [output_bucket]), messages([evil]))
	}
	every ref in ["apply-id", "tabp-admin-tfapply", "arn:aws:s3:::tabp-admin-tfapply"] {
		evil := rc("aws_s3_bucket_policy", "module.evil.aws_s3_bucket_policy.x", {"bucket": ref})
		fixture.has(sprintf("[PLAN-RPE-01] p.json: module.evil.aws_s3_bucket_policy.x が %s を指します", [apply_bucket]), messages([evil]))
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
		rc("aws_s3_bucket_policy", apply_bucket_policy, {"bucket": "plan-id"}),
	]
	msgs := gate.deny with input as fixture.plan(array.concat(targets, moved))
	fixture.has(sprintf("[PLAN-RPE-02] p.json: 宣言した %s が %s を指していません", [apply_bucket_policy, apply_bucket]), msgs)
	fixture.has(sprintf("[PLAN-RPE-02] p.json: 宣言した %s が %s を指していません", [state_bucket_policy, state_bucket]), msgs)
	fixture.has(sprintf("[PLAN-RPE-02] p.json: 宣言した %s が %s を指していません", [output_bucket_policy, output_bucket]), msgs)
	fixture.has(sprintf("[PLAN-RPE-02] p.json: 宣言した %s が %s を指していません", [key_policy, state_key]), msgs)
	not fixture.has("[PLAN-RPE-01]", msgs)
}

# 宣言した policy が、決まらない値で既存の resource を指すこともある。参照先が決まっていれば落とす。
test_violation_declared_policy_unknown_on_a_known_target if {
	msgs := gate.deny with input as fixture.plan(array.concat(targets, [unknown("aws_kms_key_policy", key_policy, "key_id")]))
	fixture.has(sprintf("[PLAN-RPE-02] p.json: 宣言した %s が ", [key_policy]), msgs)
}

# 名前を決めて作る Bucket は、bucket は決まり、id と arn が plan で決まらない。
test_pass_declared_bucket_policy_created_with_its_bucket if {
	new_bucket := {"address": state_bucket, "type": "aws_s3_bucket", "change": {"actions": ["create"], "after": {"bucket": "tabp-admin-tfstate"}, "after_unknown": {"id": true, "arn": true}}}
	inp := fixture.plan([new_bucket, unknown("aws_s3_bucket_policy", state_bucket_policy, "bucket"), targets[1], targets[2], targets[3], declared[1], declared[2], declared[3]])
	count(gate.deny) == 0 with input as inp
}

test_pass_declared_policy_created_with_its_target if {
	new_key := {"address": state_key, "type": "aws_kms_key", "change": {"actions": ["create"], "after": {}, "after_unknown": {"id": true, "key_id": true, "arn": true}}}
	inp := fixture.plan([new_key, unknown("aws_kms_key_policy", key_policy, "key_id"), targets[0], targets[1], targets[3], declared[0], declared[1], declared[3]])
	count(gate.deny) == 0 with input as inp
}

test_violation_declaration_absent_or_not_a_string if {
	every d in [{}, {"state_backend": {"bucket": 5}}] {
		msgs := gate.deny with input as fixture.plan(array.concat(targets, declared)) with data.policy_plan as d
		fixture.has("[PLAN-RPE-00] 宣言の policy_plan.state_backend.bucket が", msgs)
		fixture.has("[PLAN-RPE-00] 宣言の policy_plan.deployment_identity.apply_identity_role.output_bucket_policy が", msgs)
		fixture.has("[PLAN-RPE-00] 宣言の policy_plan.state_backend.apply_output_bucket_policy が", msgs)
	}
}

test_violation_declared_policy_without_its_target if {
	msgs := gate.deny with input as fixture.plan([rc("aws_s3_bucket_policy", state_bucket_policy, {"bucket": "state-id"})])
	fixture.has(sprintf("[PLAN-RPE-02] p.json: 宣言した %s が %s を指していません", [state_bucket_policy, state_bucket]), msgs)
}

test_violation_declared_resource_absent_from_the_input if {
	msgs := gate.deny with input as fixture.plan([rc("aws_sqs_queue", "module.x.aws_sqs_queue.this", {})])
	fixture.has(sprintf("[PLAN-RPE-00] 宣言の policy_plan.state_backend.key_policy の %s が入力にありません", [key_policy]), msgs)
	fixture.has(sprintf("[PLAN-RPE-00] 宣言の policy_plan.deployment_identity.apply_identity_role.output_bucket の %s が入力にありません", [output_bucket]), msgs)
	fixture.has(sprintf("[PLAN-RPE-00] 宣言の policy_plan.state_backend.apply_output_bucket_policy の %s が入力にありません", [apply_bucket_policy]), msgs)
}

test_pass_declared_policy_forgotten if {
	gone := [
		{"address": state_bucket_policy, "type": "aws_s3_bucket_policy", "change": {"actions": ["forget"], "before": {"bucket": "state-id"}, "after": null, "after_unknown": {"bucket": true}}},
		{"address": key_policy, "type": "aws_kms_key_policy", "change": {"actions": ["forget"], "before": {"key_id": "key-id"}, "after": null, "after_unknown": {"key_id": true}}},
	]
	count(gate.deny) == 0 with input as fixture.plan(array.concat(array.concat(targets, gone), [declared[1], declared[3]]))
}

# 既存の root に apply の出力の Bucket と Bucket Policy を足す plan（CI の plan-admin-identity の形）。Bucket は名前が
# 決まり、id と arn が決まらない。Bucket Policy は bucket も policy も決まらない。
ci_new_apply_bucket := {"address": apply_bucket, "type": "aws_s3_bucket", "change": {
	"actions": ["create"],
	"after": {"bucket": "tabp-admin-tfapply-123456789012-ap-northeast-1-an", "bucket_namespace": "account-regional", "force_destroy": false, "tags": null},
	"after_unknown": {"arn": true, "bucket_domain_name": true, "id": true, "policy": true, "region": true, "tags_all": true},
}}

ci_new_apply_bucket_policy := {"address": apply_bucket_policy, "type": "aws_s3_bucket_policy", "change": {
	"actions": ["create"],
	"after": {},
	"after_unknown": {"bucket": true, "id": true, "policy": true, "region": true},
}}

test_pass_apply_output_policy_created_with_its_bucket if {
	inp := fixture.plan(array.concat(array.slice(targets, 0, 3), array.concat(array.slice(declared, 0, 3), [ci_new_apply_bucket, ci_new_apply_bucket_policy])))
	count(gate.deny) == 0 with input as inp
}

# 宣言の外に置くと、同じ形の plan は 01 で落ちる。
test_violation_undeclared_apply_output_policy_created_with_its_bucket if {
	inp := fixture.plan(array.concat(array.slice(targets, 0, 3), array.concat(array.slice(declared, 0, 3), [ci_new_apply_bucket, ci_new_apply_bucket_policy])))
	d := json.patch(data.policy_plan, [{"op": "remove", "path": "/state_backend/apply_output_bucket"}, {"op": "remove", "path": "/state_backend/apply_output_bucket_policy"}])
	msgs := gate.deny with input as inp with data.policy_plan as d
	fixture.has(sprintf("[PLAN-RPE-01] p.json: %s がどの resource を指すかを plan で読めません", [apply_bucket_policy]), msgs)
}

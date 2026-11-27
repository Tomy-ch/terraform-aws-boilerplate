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
	rc("aws_s3_bucket_policy", output_bucket_policy, {"bucket": "plan-id", "policy": json.marshal({"Statement": statements(5)})}),
	rc("aws_kms_key_policy", key_policy, {"key_id": "key-id"}),
	rc("aws_s3_bucket_policy", apply_bucket_policy, {"bucket": "apply-id", "policy": json.marshal({"Statement": statements(3)})}),
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
		fixture.has("[PLAN-RPE-00] 宣言の policy_plan.state_backend.apply_output_bucket_policy_statements が、1以上の整数ではありません", msgs)
		fixture.has("[PLAN-RPE-00] 宣言の policy_plan.deployment_identity.apply_identity_role.output_bucket_policy_statements が、1以上の整数ではありません", msgs)
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

# 既存の root に apply の出力の Bucket と Bucket Policy を足す plan（CI の plan-admin-identity の形）。
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

test_violation_undeclared_apply_output_policy_created_with_its_bucket if {
	inp := fixture.plan(array.concat(array.slice(targets, 0, 3), array.concat(array.slice(declared, 0, 3), [ci_new_apply_bucket, ci_new_apply_bucket_policy])))
	d := json.patch(data.policy_plan, [{"op": "remove", "path": "/state_backend/apply_output_bucket"}, {"op": "remove", "path": "/state_backend/apply_output_bucket_policy"}])
	msgs := gate.deny with input as inp with data.policy_plan as d
	fixture.has(sprintf("[PLAN-RPE-01] p.json: %s がどの resource を指すかを plan で読めません", [apply_bucket_policy]), msgs)
}

statements(n) := [{"Sid": sprintf("S%d", [i]), "Effect": "Deny", "Principal": "*", "Action": "s3:PutObject", "Resource": "*"} | some i in numbers.range(1, n)]

with_documents(plan_n, apply_n) := array.concat(targets, [
	declared[0],
	fixture.doc("aws_s3_bucket_policy", output_bucket_policy, "policy", {"Statement": statements(plan_n)}),
	declared[2],
	fixture.doc("aws_s3_bucket_policy", apply_bucket_policy, "policy", {"Statement": statements(apply_n)}),
])

# 文書の bucket は fixture.doc が持たないので、02 は数えない。
statement_messages(plan_n, apply_n) := {m | some m in gate.deny with input as fixture.plan(with_documents(plan_n, apply_n)); startswith(m, "[PLAN-RPE-03]")}

test_pass_output_bucket_policy_statement_counts if {
	count(statement_messages(5, 3)) == 0
}

test_violation_output_bucket_policy_statement_counts if {
	fixture.has(sprintf("[PLAN-RPE-03] p.json: %s の文が 4 件で、宣言（policy_plan.deployment_identity.apply_identity_role.output_bucket_policy_statements）の 5 件と一致しません", [output_bucket_policy]), statement_messages(4, 3))
	fixture.has(sprintf("[PLAN-RPE-03] p.json: %s の文が 4 件で、宣言（policy_plan.state_backend.apply_output_bucket_policy_statements）の 3 件と一致しません", [apply_bucket_policy]), statement_messages(5, 4))
}

# CI の plan で Bucket と同時に作るときは、文書が plan で決まらなくても落とさない。
test_pass_unknown_document_created_with_its_bucket if {
	msgs := gate.deny with input as fixture.plan(array.concat(array.slice(targets, 0, 3), array.concat(array.slice(declared, 0, 3), [ci_new_apply_bucket, ci_new_apply_bucket_policy])))
	not fixture.has("[PLAN-RPE-03]", msgs)
}

# 既存の Bucket の Bucket Policy の文書が決まらなければ、数えられないので落とす。
test_violation_unknown_document_on_existing_bucket if {
	u := {"address": apply_bucket_policy, "type": "aws_s3_bucket_policy", "change": {"actions": ["update"], "after": {"bucket": "apply-id"}, "after_unknown": {"policy": true}}}
	msgs := gate.deny with input as fixture.plan(array.concat(targets, [declared[0], declared[1], declared[2], u]))
	fixture.has(sprintf("[PLAN-RPE-03] p.json: %s の文書が plan で決まらないか、JSON の object として読めないので", [apply_bucket_policy]), msgs)
}

test_violation_document_not_json_object if {
	every v in ["{", "[]"] {
		x := rc("aws_s3_bucket_policy", apply_bucket_policy, {"bucket": "apply-id", "policy": v})
		msgs := gate.deny with input as fixture.plan(array.concat(targets, [declared[0], declared[1], declared[2], x]))
		fixture.has(sprintf("[PLAN-RPE-03] p.json: %s の文書が plan で決まらないか、JSON の object として読めないので", [apply_bucket_policy]), msgs)
	}
}

test_violation_document_without_statement if {
	x := rc("aws_s3_bucket_policy", apply_bucket_policy, {"bucket": "apply-id", "policy": json.marshal({"Version": "2012-10-17"})})
	msgs := gate.deny with input as fixture.plan(array.concat(targets, [declared[0], declared[1], declared[2], x]))
	fixture.has(sprintf("[PLAN-RPE-03] p.json: %s の文が 0 件で", [apply_bucket_policy]), msgs)
}

test_violation_statement_count_declaration if {
	every v in [null, 0, 1.5, "4"] {
		d := json.patch(data.policy_plan, [{"op": "add", "path": "/state_backend/apply_output_bucket_policy_statements", "value": v}])
		msgs := gate.deny with input as fixture.plan(array.concat(targets, declared)) with data.policy_plan as d
		fixture.has("[PLAN-RPE-00] 宣言の policy_plan.state_backend.apply_output_bucket_policy_statements が、1以上の整数ではありません", msgs)
	}
}

apply_doc(body) := rc("aws_s3_bucket_policy", apply_bucket_policy, {"bucket": "apply-id", "policy": json.marshal(body)})

apply_messages(x) := {m | some m in gate.deny with input as fixture.plan(array.concat(targets, [declared[0], declared[1], declared[2], x])); startswith(m, "[PLAN-RPE-03]")}

test_violation_single_statement_object_counts_one if {
	fixture.has(sprintf("[PLAN-RPE-03] p.json: %s の文が 1 件で", [apply_bucket_policy]), apply_messages(apply_doc({"Statement": {"Sid": "S1", "Effect": "Deny", "Principal": "*", "Action": "s3:PutObject", "Resource": "*"}})))
}

test_violation_empty_statement_array if {
	fixture.has(sprintf("[PLAN-RPE-03] p.json: %s の文が 0 件で", [apply_bucket_policy]), apply_messages(apply_doc({"Statement": []})))
}

test_pass_forgotten_counted_policy if {
	gone := {"address": apply_bucket_policy, "type": "aws_s3_bucket_policy", "change": {"actions": ["forget"], "before": {"bucket": "apply-id"}, "after": null, "after_unknown": {}}}
	count(apply_messages(gone)) == 0
}

test_violation_output_bucket_policy_statements_invalid if {
	every v in [null, 0, 2.5, "5"] {
		d := json.patch(data.policy_plan, [{"op": "add", "path": "/deployment_identity/apply_identity_role/output_bucket_policy_statements", "value": v}])
		msgs := gate.deny with input as fixture.plan(array.concat(targets, declared)) with data.policy_plan as d
		fixture.has("[PLAN-RPE-00] 宣言の policy_plan.deployment_identity.apply_identity_role.output_bucket_policy_statements が、1以上の整数ではありません", msgs)
	}
}

# Bucket は既に在り、policy の bucket と文書だけが決まらない。同時の作成ではないので数えられないことを落とす。
test_violation_unknown_document_and_ref_on_existing_bucket if {
	u := {"address": apply_bucket_policy, "type": "aws_s3_bucket_policy", "change": {"actions": ["update"], "after": {}, "after_unknown": {"bucket": true, "policy": true}}}
	fixture.has(sprintf("[PLAN-RPE-03] p.json: %s の文書が plan で決まらないか", [apply_bucket_policy]), apply_messages(u))
}

test_violation_counted_policy_not_read_as_document if {
	msgs := gate.deny with input as fixture.plan(array.concat(targets, declared)) with data.policy_plan.documents as {}
	fixture.has(sprintf("[PLAN-RPE-03] 文の数を宣言した %s が入力に在るのに、policy の文書として読まれていません", [apply_bucket_policy]), msgs)
	fixture.has(sprintf("[PLAN-RPE-03] 文の数を宣言した %s が入力に在るのに、policy の文書として読まれていません", [output_bucket_policy]), msgs)
}

# メンバーアカウントの instance（decl.json の state_backend.instances）。宣言した root の名前のファイルでだけ数える。
member_of(x, prefix, account, env) := json.unmarshal(strings.replace_n(
	{"module.state_backend.": prefix, "123456789012": account, "tabp-admin-": sprintf("tabp-%s-", [env]), "-id\"": sprintf("-%s-id\"", [env])},
	json.marshal(x),
))

security(x) := member_of(x, "module.state_backend_security.", "111111111111", "security")

log_archive(x) := member_of(x, "module.state_backend_log_archive.", "222222222222", "log-archive")

admin_identity(rcs) := [{"path": "input/admin-identity.json", "contents": {"plan_format_version": "1.2", "resource_changes": rcs}}]

members := array.concat(security(array.concat(targets, declared)), log_archive(array.concat(targets, declared)))

member_messages(extra) := msgs if {
	msgs := gate.deny with input as admin_identity(array.concat(array.concat(array.concat(targets, declared), members), extra))
}

test_pass_member_policies_on_their_resources if {
	count(member_messages([])) == 0
}

test_violation_undeclared_policy_on_a_member_bucket if {
	evil := rc("aws_s3_bucket_policy", "module.evil.aws_s3_bucket_policy.x", {"bucket": "state-security-id"})
	fixture.has(sprintf("[PLAN-RPE-01] input/admin-identity.json: module.evil.aws_s3_bucket_policy.x が %s を指します", [security(state_bucket)]), member_messages([evil]))
}

test_violation_canonical_policy_on_a_member_bucket if {
	moved := rc("aws_s3_bucket_policy", state_bucket_policy, {"bucket": "state-security-id"})
	rcs := array.concat(array.concat(targets, [moved, declared[1], declared[2], declared[3]]), members)
	msgs := gate.deny with input as admin_identity(rcs)
	fixture.has(sprintf("[PLAN-RPE-02] input/admin-identity.json: 宣言した %s が %s を指していません", [state_bucket_policy, state_bucket]), msgs)
}

test_violation_member_declaration_absent_from_its_root if {
	msgs := gate.deny with input as admin_identity(array.concat(array.concat(targets, declared), security(array.concat(targets, declared))))
	fixture.has(sprintf("[PLAN-RPE-00] admin-identity の instance log-archive: 宣言の policy_plan.state_backend.key_policy の %s が入力にありません", [log_archive(key_policy)]), msgs)
	not fixture.has("[PLAN-RPE-00] admin-identity の instance security:", msgs)
}

test_violation_member_declaration_only_in_another_file if {
	inp := array.concat(admin_identity(array.concat(array.concat(targets, declared), security(array.concat(targets, declared)))), fixture.plan(log_archive(array.concat(targets, declared))))
	fixture.has("[PLAN-RPE-00] admin-identity の instance log-archive: ", gate.deny) with input as inp
}

test_violation_undeclared_instance if {
	other := member_of(array.concat(targets, declared), "module.state_backend_other.", "111111111111", "other")
	msgs := member_messages(other)
	fixture.has("[PLAN-RPE-00] input/admin-identity.json: state-backend の instance（prefix \"module.state_backend_other.\"）が宣言にありません。data.policy_plan.state_backend.instances[\"admin-identity\"] に足してください", msgs)
}

test_violation_instances_malformed if {
	every instances in [[], {}, {"admin-identity": []}, {"admin-identity": [{"name": "security"}]}] {
		d := json.patch(data.policy_plan, [{"op": "replace", "path": "/state_backend/instances", "value": instances}])
		msgs := gate.deny with input as admin_identity(array.concat(array.concat(targets, declared), members)) with data.policy_plan as d
		fixture.has("[PLAN-RPE-00] data.policy_plan.state_backend.instances", msgs)
	}
}

test_violation_declared_address_outside_the_module if {
	d := json.patch(data.policy_plan, [{"op": "replace", "path": "/state_backend/bucket", "value": "module.other.aws_s3_bucket.this"}])
	fixture.has("[PLAN-RPE-00] 宣言の policy_plan.state_backend.bucket が state_backend.module か deployment_identity.module で始まりません", gate.deny) with input as fixture.plan(array.concat(targets, declared)) with data.policy_plan as d
}

# メンバーの立ち上げの作成の plan。Bucket と鍵の id と arn、policy の参照先と文書が決まらない。
created(x, after, unknown) := json.patch(x, [
	{"op": "replace", "path": "/change/after", "value": after},
	{"op": "replace", "path": "/change/after_unknown", "value": {a: true | some a in unknown}},
])

creating(x) := created(x, {"bucket": x.change.after.bucket}, ["id", "arn"]) if x.type == "aws_s3_bucket"

creating(x) := created(x, {}, ["id", "key_id", "arn"]) if x.type == "aws_kms_key"

creating(x) := created(x, {}, ["bucket", "policy"]) if x.type == "aws_s3_bucket_policy"

creating(x) := created(x, {}, ["key_id", "policy"]) if x.type == "aws_kms_key_policy"

test_pass_member_policies_created_with_their_resources if {
	count(gate.deny) == 0 with input as admin_identity(array.concat(array.concat(targets, declared), [creating(x) | some x in members]))
}

# 作成の plan でも、宣言の外の policy が作る Bucket を指すかを読めなければ落とす。
test_violation_undeclared_policy_with_unknown_target_while_creating if {
	u := unknown("aws_s3_bucket_policy", "module.evil.aws_s3_bucket_policy.x", "bucket")
	msgs := gate.deny with input as admin_identity(array.concat(array.concat(array.concat(targets, declared), [creating(x) | some x in members]), [u]))
	fixture.has("[PLAN-RPE-01] input/admin-identity.json: module.evil.aws_s3_bucket_policy.x がどの resource を指すかを plan で読めません", msgs)
}

member_statement_messages(plan_n, apply_n) := {m |
	some m in gate.deny with input as admin_identity(array.concat(array.concat(targets, declared), array.concat(security(with_documents(plan_n, apply_n)), log_archive(array.concat(targets, declared)))))
	startswith(m, "[PLAN-RPE-03]")
}

test_pass_member_statement_counts if {
	count(member_statement_messages(5, 3)) == 0
}

test_violation_member_statement_counts if {
	fixture.has(sprintf("[PLAN-RPE-03] input/admin-identity.json: %s の文が 4 件で、宣言（policy_plan.deployment_identity.apply_identity_role.output_bucket_policy_statements）の 5 件と一致しません", [security(output_bucket_policy)]), member_statement_messages(4, 3))
	fixture.has(sprintf("[PLAN-RPE-03] input/admin-identity.json: %s の文が 4 件で、宣言（policy_plan.state_backend.apply_output_bucket_policy_statements）の 3 件と一致しません", [security(apply_bucket_policy)]), member_statement_messages(5, 4))
}

test_violation_member_counted_policy_not_read_as_document if {
	msgs := gate.deny with input as admin_identity(array.concat(array.concat(targets, declared), members)) with data.policy_plan.documents as {}
	fixture.has(sprintf("[PLAN-RPE-03] 文の数を宣言した %s が入力に在るのに、policy の文書として読まれていません", [security(apply_bucket_policy)]), msgs)
	fixture.has(sprintf("[PLAN-RPE-03] 文の数を宣言した %s が入力に在るのに、policy の文書として読まれていません", [log_archive(output_bucket_policy)]), msgs)
}

# 立ち上げを閉じる PR は、メンバーの instance を removed（destroy = false）で管理から外す。宣言を外したメンバーの forget は数えない。
test_pass_removed_members_forgotten if {
	gone := [json.patch(x, [
		{"op": "replace", "path": "/change/actions", "value": ["forget"]},
		{"op": "add", "path": "/change/before", "value": x.change.after},
		{"op": "replace", "path": "/change/after", "value": null},
	]) | some x in members]
	d := json.patch(data.policy_plan, [{"op": "remove", "path": "/state_backend/instances"}])
	count(gate.deny) == 0 with input as admin_identity(array.concat(array.concat(targets, declared), gone)) with data.policy_plan as d
}

test_violation_removed_member_still_written if {
	d := json.patch(data.policy_plan, [{"op": "remove", "path": "/state_backend/instances"}])
	msgs := gate.deny with input as admin_identity(array.concat(array.concat(targets, declared), members)) with data.policy_plan as d
	fixture.has("[PLAN-RPE-00] input/admin-identity.json: state-backend の instance（prefix \"module.state_backend_security.\"）が宣言にありません", msgs)
}

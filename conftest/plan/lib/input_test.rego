package plan.input_test

import data.plan.fixture
import data.plan.input as gate

valid := fixture.role_policy([fixture.allow("s3:GetObject")])

test_violation_empty_input if {
	fixture.has("[PLAN-INPUT-01]", gate.deny) with input as []
}

test_violation_format_version if {
	fixture.has("[PLAN-INPUT-02]", gate.deny) with input as [{"path": "p.json", "contents": {"plan_format_version": "1.1", "resource_changes": [valid]}}]
}

test_violation_empty_resource_changes if {
	fixture.has("[PLAN-INPUT-03]", gate.deny) with input as fixture.plan([])
}

test_violation_unknown_document if {
	rc := {"address": "module.x.aws_iam_role_policy.this", "type": "aws_iam_role_policy", "change": {"actions": ["create"], "after": {}, "after_unknown": {"policy": true}}}
	fixture.has("[PLAN-INPUT-04]", gate.deny) with input as fixture.plan([rc])
}

test_violation_broken_json if {
	rc := {"address": "module.x.aws_iam_role_policy.this", "type": "aws_iam_role_policy", "change": {"actions": ["create"], "after": {"policy": "l3mra53t"}, "after_unknown": {}}}
	fixture.has("[PLAN-INPUT-05]", gate.deny) with input as fixture.plan([rc])
}

test_violation_missing_document if {
	rc := {"address": "module.x.aws_iam_role_policy.this", "type": "aws_iam_role_policy", "change": {"actions": ["update"], "after": {}, "after_unknown": {}}}
	fixture.has("[PLAN-INPUT-05]", gate.deny) with input as fixture.plan([rc])
}

test_pass_valid_input if {
	count(gate.deny) == 0 with input as fixture.clean([valid]) with data.policy_plan.required_documents as ["aws_iam_role_policy"] with data.provider_schema as fixture.provider_schema
}

# mock provider は表に無い computed な属性に乱数を入れる。表にも inline_arguments にも無い属性は読まない。
test_pass_untracked_attribute if {
	rc := object.union(fixture.unwritten("aws_s3_bucket"), {"change": {"after": {"bucket_domain_name": "l3mra53t"}}})
	count(gate.deny) == 0 with input as fixture.clean([valid, rc]) with data.policy_plan.required_documents as ["aws_iam_role_policy"] with data.provider_schema as fixture.provider_schema
}

test_pass_delete_only if {
	rc := {"address": "module.x.aws_iam_role_policy.old", "type": "aws_iam_role_policy", "change": {"actions": ["delete"], "after": null, "after_unknown": {}}}
	count(gate.deny) == 0 with input as fixture.clean([valid, rc]) with data.policy_plan.required_documents as ["aws_iam_role_policy"] with data.provider_schema as fixture.provider_schema
}

test_violation_no_statement if {
	rc := fixture.doc("aws_iam_role_policy", "module.x.aws_iam_role_policy.this", "policy", {"statement": [fixture.allow("*")]})
	fixture.has("[PLAN-INPUT-06]", gate.deny) with input as fixture.plan([rc])
}

test_violation_top_level_array if {
	rc := fixture.doc("aws_iam_role_policy", "module.x.aws_iam_role_policy.this", "policy", [{"Statement": [fixture.allow("*")]}])
	fixture.has("[PLAN-INPUT-06]", gate.deny) with input as fixture.plan([rc])
}

test_violation_statement_not_object if {
	fixture.has("[PLAN-INPUT-06]", gate.deny) with input as fixture.plan([fixture.role_policy(["x", 5])])
}

test_violation_action_not_string if {
	s := {"Effect": "Allow", "Action": {"x": 1}, "Resource": "*"}
	fixture.has("[PLAN-INPUT-06]", gate.deny) with input as fixture.plan([fixture.role_policy([s])])
}

# Resource が文字列でないと、Resource を読む規則が型エラーで未定義になって黙る。
test_violation_resource_not_string if {
	every s in [
		{"Effect": "Allow", "Action": "s3:DeleteObject", "Resource": [1, {"x": 1}, null]},
		{"Effect": "Allow", "Action": "s3:DeleteObject", "NotResource": 5},
	] {
		fixture.has("[PLAN-INPUT-06]", gate.deny) with input as fixture.plan([fixture.role_policy([s])])
	}
}

test_violation_condition_null if {
	s := {"Effect": "Allow", "Action": "sts:AssumeRoleWithWebIdentity", "Condition": null}
	fixture.has("[PLAN-INPUT-06]", gate.deny) with input as fixture.plan([fixture.role_policy([s])])
}

test_pass_single_statement_object if {
	rc := fixture.doc("aws_iam_policy", "module.x.aws_iam_policy.this", "policy", {"Statement": fixture.allow("s3:GetObject")})
	count(gate.deny) == 0 with input as fixture.clean([rc]) with data.policy_plan.required_documents as ["aws_iam_policy"] with data.provider_schema as fixture.provider_schema
}

test_violation_effect_not_allow_or_deny if {
	every effect in [null, ["Allow"], "allow"] {
		s := {"Effect": effect, "Action": "*", "Resource": "*"}
		fixture.has("[PLAN-INPUT-06]", gate.deny) with input as fixture.plan([fixture.role_policy([s])])
	}
}

test_violation_effect_missing if {
	fixture.has("[PLAN-INPUT-06]", gate.deny) with input as fixture.plan([fixture.role_policy([{"Action": "*", "Resource": "*"}])])
}

test_violation_action_array_has_non_string if {
	fixture.has("[PLAN-INPUT-06]", gate.deny) with input as fixture.plan([fixture.role_policy([fixture.allow(["s3:GetObject", 5])])])
}

test_violation_not_action_not_string if {
	s := {"Effect": "Allow", "NotAction": {"x": 1}, "Resource": "*"}
	fixture.has("[PLAN-INPUT-06]", gate.deny) with input as fixture.plan([fixture.role_policy([s])])
}

test_violation_statement_scalar if {
	every statement in ["Allow", null] {
		rc := fixture.doc("aws_iam_policy", "module.x.aws_iam_policy.this", "policy", {"Statement": statement})
		fixture.has("[PLAN-INPUT-06]", gate.deny) with input as fixture.plan([rc])
	}
}

test_pass_condition_object_and_action_array if {
	s := {"Effect": "Allow", "Action": ["s3:GetObject", "s3:PutObject"], "Resource": "*", "Condition": {"Bool": {"aws:SecureTransport": "true"}}}
	count(gate.deny) == 0 with input as fixture.clean([fixture.role_policy([s])]) with data.policy_plan.required_documents as ["aws_iam_role_policy"] with data.provider_schema as fixture.provider_schema
}

test_violation_missing_format_version if {
	fixture.has("[PLAN-INPUT-02]", gate.deny) with input as [{"path": "p.json", "contents": {"resource_changes": [valid]}}]
}

test_violation_resource_changes_absent_or_not_array if {
	every contents in [{"plan_format_version": "1.2"}, {"plan_format_version": "1.2", "resource_changes": "abc"}] {
		fixture.has("[PLAN-INPUT-03]", gate.deny) with input as [{"path": "p.json", "contents": contents}]
	}
}

test_violation_change_without_actions if {
	rc := {"address": "module.x.aws_iam_role_policy.this", "type": "aws_iam_role_policy", "change": {"after": {"policy": json.marshal({"Statement": [fixture.allow("*")]})}, "after_unknown": {}}}
	fixture.has("[PLAN-INPUT-08]", gate.deny) with input as fixture.plan([valid, rc])
}

test_violation_required_type_absent if {
	msgs := gate.deny with input as fixture.plan([valid])
	fixture.has("[PLAN-INPUT-07] aws_iam_role ", msgs)
	fixture.has("[PLAN-INPUT-07] aws_s3_bucket_policy ", msgs)
	fixture.has("[PLAN-INPUT-07] aws_kms_key_policy ", msgs)
	not fixture.has("[PLAN-INPUT-07] aws_iam_role_policy ", msgs)
}

test_violation_declaration_incomplete if {
	every key in ["plan_format_version", "provider_format_version", "documents", "required_documents", "deployment_identity", "inline_arguments"] {
		fixture.has("[PLAN-DATA-01]", gate.deny) with input as fixture.plan([valid]) with data.policy_plan as object.remove(data.policy_plan, [key])
	}
}

test_violation_declaration_required_type_not_in_table if {
	fixture.has("[PLAN-DATA-01]", gate.deny) with input as fixture.plan([valid]) with data.policy_plan.required_documents as ["aws_kms_key"]
}

test_pass_declaration if {
	not fixture.has("[PLAN-DATA-01]", gate.deny) with input as fixture.plan([valid])
}

test_violation_declaration_empty_tables if {
	every key in ["documents", "required_documents", "inline_arguments"] {
		empty := {"documents": {}, "required_documents": [], "inline_arguments": {}}[key]
		fixture.has("[PLAN-DATA-01]", gate.deny) with input as fixture.plan([valid]) with data.policy_plan as object.union(object.remove(data.policy_plan, [key]), {key: empty})
	}
}

test_pass_data_source_not_read if {
	read := {"address": "data.aws_iam_policy_document.x", "mode": "data", "type": "aws_iam_policy", "change": {"actions": ["read"], "after": {"policy": "not json"}, "after_unknown": {}}}
	count(gate.deny) == 0 with input as fixture.clean([valid, read]) with data.policy_plan.required_documents as ["aws_iam_role_policy"] with data.provider_schema as fixture.provider_schema
}

# ADR-0402 決定17・18。宣言した引数ごとに、書いた値を落とす。
written_value(empty) := [{"x": "y"}] if empty == []

written_value(empty) := json.marshal({"Statement": [fixture.allow("s3:GetObject")]}) if empty == null

with_arg(type, arg, value) := object.union(fixture.unwritten(type), {"change": {"after": {arg: value}}})

test_violation_inline_argument_written if {
	every type, args in data.policy_plan.inline_arguments {
		every arg, empty in args {
			msgs := gate.deny with input as fixture.clean([valid, with_arg(type, arg, written_value(empty))]) with data.provider_schema as fixture.provider_schema
			fixture.has(sprintf("[PLAN-INPUT-09] p.json: module.x.%s.this の %s ", [type, arg]), msgs)
		}
	}
}

test_violation_inline_policy_empty_string_or_random if {
	every value in ["", "l3mra53t"] {
		msgs := gate.deny with input as fixture.clean([valid, with_arg("aws_s3_bucket", "policy", value)]) with data.provider_schema as fixture.provider_schema
		fixture.has("[PLAN-INPUT-09] p.json: module.x.aws_s3_bucket.this の policy ", msgs)
	}
}

test_violation_inline_argument_unknown if {
	rc := {"address": "module.x.aws_s3_bucket.this", "type": "aws_s3_bucket", "change": {"actions": ["create"], "after": object.remove(data.policy_plan.inline_arguments.aws_s3_bucket, ["policy"]), "after_unknown": {"policy": true}}}
	msgs := gate.deny with input as fixture.clean([valid, rc]) with data.provider_schema as fixture.provider_schema
	fixture.has("[PLAN-INPUT-09] p.json: module.x.aws_s3_bucket.this の policy ", msgs)
}

test_violation_inline_block_partly_unknown if {
	rc := object.union(with_arg("aws_s3_bucket", "versioning", [{"mfa_delete": false}]), {"change": {"after_unknown": {"versioning": [{"enabled": true}]}}})
	msgs := gate.deny with input as fixture.clean([valid, rc]) with data.provider_schema as fixture.provider_schema
	fixture.has("[PLAN-INPUT-09] p.json: module.x.aws_s3_bucket.this の versioning ", msgs)
}

test_violation_inline_argument_on_replace if {
	rc := object.union(with_arg("aws_kms_key", "policy", written_value(null)), {"change": {"actions": ["delete", "create"]}})
	msgs := gate.deny with input as fixture.clean([valid, rc]) with data.provider_schema as fixture.provider_schema
	fixture.has("[PLAN-INPUT-09] p.json: module.x.aws_kms_key.this の policy ", msgs)
}

test_pass_inline_block_unknown_list_is_empty if {
	rc := object.union(fixture.unwritten("aws_s3_bucket"), {"change": {"after_unknown": {"versioning": [], "website": []}}})
	count(gate.deny) == 0 with input as fixture.clean([valid, rc]) with data.policy_plan.required_documents as ["aws_iam_role_policy"] with data.provider_schema as fixture.provider_schema
}

test_pass_inline_argument_outside_declared_type if {
	rcs := [
		{"address": "module.x.aws_sqs_queue.this", "type": "aws_sqs_queue", "change": {"actions": ["create"], "after": {"policy": written_value(null)}, "after_unknown": {}}},
		{"address": "data.aws_s3_bucket.x", "mode": "data", "type": "aws_s3_bucket", "change": {"actions": ["read"], "after": {"policy": "x"}, "after_unknown": {}}},
		{"address": "module.x.aws_s3_bucket.old", "type": "aws_s3_bucket", "change": {"actions": ["delete"], "before": {"policy": "x"}, "after": null, "after_unknown": {}}},
		{"address": "module.x.aws_s3_bucket.gone", "type": "aws_s3_bucket", "change": {"actions": ["forget"], "before": {"policy": "x"}, "after": null}},
	]
	count(gate.deny) == 0 with input as fixture.clean(array.concat([valid], rcs)) with data.policy_plan.required_documents as ["aws_iam_role_policy"] with data.provider_schema as fixture.provider_schema
}

test_violation_inline_declaration_absent_from_input if {
	msgs := gate.deny with input as fixture.plan([valid]) with data.provider_schema as fixture.provider_schema
	fixture.has("[PLAN-INPUT-10] inline_arguments の aws_s3_bucket の policy ", msgs)
	fixture.has("[PLAN-INPUT-10] inline_arguments の aws_kms_key の policy ", msgs)
}

test_violation_inline_declaration_misspelled_argument if {
	args := object.union(data.policy_plan.inline_arguments.aws_kms_key, {"polic": null})
	msgs := gate.deny with input as fixture.clean([valid]) with data.policy_plan.inline_arguments.aws_kms_key as args with data.provider_schema as fixture.provider_schema
	fixture.has("[PLAN-INPUT-10] inline_arguments の aws_kms_key の polic ", msgs)
}

test_violation_provider_schema_absent_or_empty if {
	fixture.has("[PLAN-INPUT-11] mock の plan", gate.deny) with input as fixture.clean([valid])
	fixture.has("[PLAN-INPUT-11] mock の plan", gate.deny) with input as fixture.clean([valid]) with data.provider_schema as {"deprecated_arguments": {}}
}

test_violation_declared_type_not_in_provider_schema if {
	s := {"deprecated_arguments": object.remove(fixture.provider_schema.deprecated_arguments, ["aws_kms_key"])}
	fixture.has("[PLAN-INPUT-11] aws_kms_key が mock の plan", gate.deny) with input as fixture.clean([valid]) with data.provider_schema as s
}

test_violation_deprecated_argument_not_declared if {
	s := {"deprecated_arguments": object.union(fixture.provider_schema.deprecated_arguments, {"aws_s3_bucket": array.concat(fixture.provider_schema.deprecated_arguments.aws_s3_bucket, ["bucket_prefix"])})}
	fixture.has("[PLAN-INPUT-11] aws_s3_bucket の非推奨の引数 bucket_prefix ", gate.deny) with input as fixture.clean([valid]) with data.provider_schema as s
}

test_pass_provider_schema_wider_than_declaration if {
	s := {"deprecated_arguments": object.union(fixture.provider_schema.deprecated_arguments, {"aws_iam_role": ["inline_policy"]})}
	count(gate.deny) == 0 with input as fixture.clean([valid]) with data.policy_plan.required_documents as ["aws_iam_role_policy"] with data.provider_schema as s
}

test_violation_inline_declaration_malformed if {
	bad := [
		{"aws_s3_bucket": {"policy": ""}},
		{"aws_s3_bucket": {}},
		{"aws_s3_bucket": ["policy"]},
		{"aws_sqs_queue": {"policy": null}},
		{"aws_s3_bucket_policy": {"policy": null}},
	]
	every d in bad {
		fixture.has("[PLAN-DATA-01]", gate.deny) with input as fixture.plan([valid]) with data.policy_plan.inline_arguments as d with data.policy_plan.stateful_protection.protected_types as ["aws_s3_bucket", "aws_s3_bucket_policy"]
	}
}

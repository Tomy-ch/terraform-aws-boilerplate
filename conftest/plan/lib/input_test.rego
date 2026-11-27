package plan.input_test

import data.plan.fixture
import data.plan.lib
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
	every key in ["documents", "required_documents"] {
		empty := {"documents": {}, "required_documents": []}[key]
		fixture.has("[PLAN-DATA-01]", gate.deny) with input as fixture.plan([valid]) with data.policy_plan as object.union(object.remove(data.policy_plan, [key]), {key: empty})
	}
}

# Policy Test が持つ宣言は空でもよい（層の宣言から導く分が在る）。導く分も無ければ、実効の宣言が空なので落とす。
test_pass_raw_inline_arguments_empty if {
	not fixture.has("[PLAN-DATA-01]", gate.deny) with input as fixture.plan([valid]) with data.policy_plan as fixture.inline_declaration({})
}

test_violation_effective_inline_arguments_empty if {
	fixture.has("[PLAN-DATA-01]", gate.deny) with input as fixture.plan([valid]) with data.policy_plan as fixture.inline_declaration({})
		with data.layer_decl as {"attachments": {}, "unrelated_deprecated_arguments": {}, "inline_owned": {}}
}

test_violation_inline_argument_derivable if {
	d := object.union(data.policy_plan.inline_arguments, {"aws_kms_key": {"policy": null}})
	msgs := gate.deny with input as fixture.clean([valid]) with data.policy_plan as fixture.inline_declaration(d) with data.provider_schema as fixture.provider_schema
	fixture.has("[PLAN-INPUT-12] policy_plan.inline_arguments の aws_kms_key の policy は、層の宣言", msgs)
}

test_violation_layer_decl_attachments_empty if {
	msgs := gate.deny with input as fixture.clean([valid]) with data.layer_decl as {"attachments": {}, "unrelated_deprecated_arguments": {}, "inline_owned": {}} with data.provider_schema as fixture.provider_schema
	fixture.has("[PLAN-INPUT-12] 層の宣言 data.layer_decl", msgs)
}

test_violation_layer_decl_missing if {
	msgs := gate.deny with input as fixture.clean([valid]) with data.layer_decl as null with data.provider_schema as fixture.provider_schema
	fixture.has("[PLAN-INPUT-12] 層の宣言 data.layer_decl", msgs)
}

test_pass_protected_parent_type_with_only_owned_deprecated if {
	d := object.union(object.remove(data.layer_decl, ["attachments", "inline_owned"]), {
		"attachments": {"aws_s3_bucket_policy": object.union(object.remove(data.layer_decl.attachments.aws_s3_bucket_policy, ["parent_arguments"]), {"parent_arguments": {}})},
		"inline_owned": {"aws_s3_bucket": {"policy": "fixture"}},
	})
	msgs := gate.deny with input as fixture.clean([valid]) with data.layer_decl as d with data.provider_schema as {"deprecated_arguments": {"aws_s3_bucket": ["policy"]}}
	not fixture.has("[PLAN-INPUT-11] 保護の対象の型 aws_s3_bucket は", msgs)
}

test_violation_protected_parent_type_missing if {
	d := object.union(object.remove(data.layer_decl, ["attachments"]), {"attachments": {"aws_s3_bucket_policy": object.union(object.remove(data.layer_decl.attachments.aws_s3_bucket_policy, ["parent_arguments"]), {"parent_arguments": {}})}})
	msgs := gate.deny with input as fixture.clean([valid]) with data.layer_decl as d with data.provider_schema as {"deprecated_arguments": {"aws_s3_bucket": ["policy"]}}
	fixture.has("[PLAN-INPUT-11] 保護の対象の型 aws_s3_bucket は", msgs)
}

test_pass_inline_arguments_derived_from_layer if {
	lib.inline_arguments.aws_s3_bucket.website == []
	lib.inline_arguments.aws_kms_key.policy == null
	not data.policy_plan.inline_arguments.aws_s3_bucket
	msgs := gate.deny with input as fixture.clean([valid]) with data.provider_schema as fixture.provider_schema
	not fixture.has("[PLAN-INPUT-12]", msgs)
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
	rc := {"address": "module.x.aws_s3_bucket.this", "type": "aws_s3_bucket", "change": {"actions": ["create"], "after": object.remove(lib.inline_arguments.aws_s3_bucket, ["policy"]), "after_unknown": {"policy": true}}}
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
	d := object.union(data.policy_plan.inline_arguments, {"aws_kms_key": {"policy": null, "polic": null}})
	msgs := gate.deny with input as fixture.clean([valid]) with data.policy_plan as fixture.inline_declaration(d) with data.provider_schema as fixture.provider_schema
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

# 不正な形ごとに、宣言の残りは decl.json のまま差し替える。aws_kms_key_policy は保護の対象で、documents に policy を持つ。
test_violation_inline_declaration_malformed if {
	bad := [
		{"aws_s3_bucket": {"policy": ""}},
		{"aws_s3_bucket": {}},
		{"aws_s3_bucket": ["policy"]},
		{"aws_sqs_queue": {"policy": null}},
		{"aws_kms_key_policy": {"policy": null}},
	]
	every d in bad {
		fixture.has("[PLAN-DATA-01]", gate.deny) with input as fixture.plan([valid]) with data.policy_plan as fixture.inline_declaration(d)
	}
}

test_pass_inline_declaration_well_formed if {
	every d in [{"aws_s3_bucket": {"policy": null}}, {"aws_kms_key": {"policy": null}, "aws_s3_bucket": {"versioning": []}}] {
		not fixture.has("[PLAN-DATA-01]", gate.deny) with input as fixture.plan([valid]) with data.policy_plan as fixture.inline_declaration(d)
	}
}

test_violation_inline_block_unknown_inside if {
	rc := object.union(fixture.unwritten("aws_s3_bucket"), {"change": {"after_unknown": {"versioning": [{"enabled": true}]}}})
	msgs := gate.deny with input as fixture.clean([valid, rc]) with data.provider_schema as fixture.provider_schema
	fixture.has("[PLAN-INPUT-09] p.json: module.x.aws_s3_bucket.this の versioning ", msgs)
}

# 書いた値が plan で決まらない引数は after に現れない。after_unknown だけでも入力に在ると読む。
test_pass_inline_seen_only_in_after_unknown if {
	after := object.remove(lib.inline_arguments.aws_kms_key, ["policy"])
	rc := {"address": "module.x.aws_kms_key.this", "type": "aws_kms_key", "change": {"actions": ["create"], "after": after, "after_unknown": {"policy": true}}}
	count(lib.inline_arguments) > 1
	rcs := array.concat([valid, rc], [fixture.unwritten(t) | some t, _ in lib.inline_arguments; t != "aws_kms_key"])
	msgs := gate.deny with input as fixture.plan(rcs) with data.provider_schema as fixture.provider_schema
	not fixture.has("[PLAN-INPUT-10]", msgs)
	fixture.has("[PLAN-INPUT-09] p.json: module.x.aws_kms_key.this の policy ", msgs)
}

test_violation_inline_seen_not_by_another_type if {
	other := {"address": "module.x.aws_sqs_queue.this", "type": "aws_sqs_queue", "change": {"actions": ["create"], "after": {}, "after_unknown": {"policy": true}}}
	msgs := gate.deny with input as fixture.plan([valid, other, fixture.unwritten("aws_s3_bucket")]) with data.provider_schema as fixture.provider_schema
	fixture.has("[PLAN-INPUT-10] inline_arguments の aws_kms_key の policy ", msgs)
}

test_violation_provider_schema_not_object if {
	every v in [["x"], "x"] {
		fixture.has("[PLAN-INPUT-11] mock の plan", gate.deny) with input as fixture.clean([valid]) with data.provider_schema as {"deprecated_arguments": v}
	}
}

# 宣言を表で回すテストは、宣言が空なら何も見ずに通る。
test_pass_declaration_covers_the_bucket_and_the_key if {
	every type in ["aws_s3_bucket", "aws_kms_key"] {
		count(object.get(lib.inline_arguments, type, {})) > 0
	}
}

# 導く元の層の宣言。aws_s3_bucket は保護の対象、aws_iam_role は保護の対象でない親の型。
derive_decl(owned) := {
	"attachments": {
		"aws_s3_bucket_policy": {"parents": {"bucket": ["aws_s3_bucket"]}, "optional": false, "parent_arguments": {"aws_s3_bucket": {"policy": null, "website": []}}, "whole_document": true},
		"aws_iam_role_policy": {"parents": {"role": ["aws_iam_role"]}, "optional": false, "parent_arguments": {"aws_iam_role": {"inline_policy": []}}, "whole_document": false},
	},
	"unrelated_deprecated_arguments": {"aws_s3_bucket": {"acl": {"reason": "r", "unwritten": null}, "region": {"reason": "r", "unwritten": null}}},
	"inline_owned": owned,
}

test_pass_derived_without_inline_owned if {
	lib.derived_inline_arguments.aws_s3_bucket == {"policy": null, "website": [], "acl": null, "region": null} with data.layer_decl as derive_decl({})
}

# inline_owned の引数は、parent_arguments の側からは導かない。
test_pass_derived_skips_inline_owned_parent_argument if {
	lib.derived_inline_arguments.aws_s3_bucket == {"website": [], "acl": null, "region": null} with data.layer_decl as derive_decl({"aws_s3_bucket": {"policy": "r"}})
}

# inline_owned の引数は、unrelated_deprecated_arguments の側からも導かない。
test_pass_derived_skips_inline_owned_unrelated_argument if {
	lib.derived_inline_arguments.aws_s3_bucket == {"policy": null, "website": [], "region": null} with data.layer_decl as derive_decl({"aws_s3_bucket": {"acl": "r"}})
}

# 引数がすべて inline_owned なら、型ごと導かない。
test_pass_derived_skips_type_fully_owned if {
	owned := {"aws_s3_bucket": {"policy": "r", "website": "r", "acl": "r", "region": "r"}}
	not lib.derived_inline_arguments.aws_s3_bucket with data.layer_decl as derive_decl(owned)
}

test_pass_derived_skips_unprotected_parent_type if {
	not "aws_iam_role" in data.policy_plan.stateful_protection.protected_types
	"aws_iam_role" in lib.layer_parent_types with data.layer_decl as derive_decl({})
	not lib.derived_inline_arguments.aws_iam_role with data.layer_decl as derive_decl({})
}

# 同じ型を Policy Test の宣言と層の宣言の両方が持てば、実効の宣言は引数の和になる。
test_pass_inline_arguments_union_of_raw_and_derived if {
	got := lib.inline_arguments.aws_s3_bucket with data.layer_decl as derive_decl({})
		with data.policy_plan as fixture.inline_declaration({"aws_s3_bucket": {"bucket_prefix": null}})
	got == {"policy": null, "website": [], "acl": null, "region": null, "bucket_prefix": null}
}

# 保護の対象でも、層の宣言で親の型でなければ、実効の宣言に型が無くてよい。
test_pass_protected_type_not_parent if {
	"aws_ecr_lifecycle_policy" in data.policy_plan.stateful_protection.protected_types
	not "aws_ecr_lifecycle_policy" in lib.layer_parent_types
	not lib.inline_arguments.aws_ecr_lifecycle_policy
	s := {"deprecated_arguments": object.union(fixture.provider_schema.deprecated_arguments, {"aws_ecr_lifecycle_policy": ["x"]})}
	msgs := gate.deny with input as fixture.clean([valid]) with data.provider_schema as s
	not fixture.has("[PLAN-INPUT-11] 保護の対象の型 aws_ecr_lifecycle_policy ", msgs)
}

# 親の型でも、非推奨の引数が0件なら、実効の宣言に型が無くてよい。
test_pass_parent_type_without_deprecated_arguments if {
	d := object.union(object.remove(data.layer_decl, ["attachments"]), {"attachments": {"aws_s3_bucket_policy": object.union(object.remove(data.layer_decl.attachments.aws_s3_bucket_policy, ["parent_arguments"]), {"parent_arguments": {}})}})
	msgs := gate.deny with input as fixture.clean([valid]) with data.layer_decl as d with data.provider_schema as {"deprecated_arguments": {"aws_s3_bucket": []}}
	not fixture.has("[PLAN-INPUT-11] 保護の対象の型 aws_s3_bucket ", msgs)
}

test_iam_match_literal_equal if lib.iam_match("s3:getobject", "s3:getobject")

test_iam_match_literal_differs if not lib.iam_match("s3:getobject", "s3:getobjects")

test_iam_match_literal_dot_is_not_regex if not lib.iam_match("s3:get.cl", "s3:getacl")

test_iam_match_literal_bracket if lib.iam_match("arn:aws:s3:::b[1]", "arn:aws:s3:::b[1]")

test_iam_match_star_crosses_separators if lib.iam_match("arn:aws:s3:::b/*", "arn:aws:s3:::b/x/y:z")

test_iam_match_question_is_one_character if {
	lib.iam_match("s3:get?cl", "s3:getacl")
	not lib.iam_match("s3:get?cl", "s3:getcl")
}

test_iam_match_wildcard_with_regex_characters if {
	lib.iam_match("arn:aws:s3:::b.c/*", "arn:aws:s3:::b.c/k")
	not lib.iam_match("arn:aws:s3:::b.c/*", "arn:aws:s3:::bxc/k")
}

test_derived_merges_parent_arguments_across_attachments if {
	d := {
		"attachments": {
			"a": {"parent_arguments": {"aws_s3_bucket": {"policy": null}}},
			"b": {"parent_arguments": {"aws_s3_bucket": {"website": []}, "aws_iam_role": {"inline_policy": []}}},
		},
		"unrelated_deprecated_arguments": {},
		"inline_owned": {},
	}
	got := lib.derived_inline_arguments with data.layer_decl as d with data.policy_plan.stateful_protection.protected_types as ["aws_s3_bucket"]
	got == {"aws_s3_bucket": {"policy": null, "website": []}}
}

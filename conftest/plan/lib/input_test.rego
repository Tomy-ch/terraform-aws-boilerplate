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
	count(gate.deny) == 0 with input as fixture.plan([valid]) with data.policy_plan.required_documents as ["aws_iam_role_policy"]
}

# mock provider は表に無い computed な属性に乱数を入れる。表に無い属性は読まない。
test_pass_untracked_attribute if {
	rc := {"address": "module.x.aws_s3_bucket.this", "type": "aws_s3_bucket", "change": {"actions": ["create"], "after": {"policy": "l3mra53t"}, "after_unknown": {}}}
	count(gate.deny) == 0 with input as fixture.plan([valid, rc]) with data.policy_plan.required_documents as ["aws_iam_role_policy"]
}

test_pass_delete_only if {
	rc := {"address": "module.x.aws_iam_role_policy.old", "type": "aws_iam_role_policy", "change": {"actions": ["delete"], "after": null, "after_unknown": {}}}
	count(gate.deny) == 0 with input as fixture.plan([valid, rc]) with data.policy_plan.required_documents as ["aws_iam_role_policy"]
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

test_violation_condition_null if {
	s := {"Effect": "Allow", "Action": "sts:AssumeRoleWithWebIdentity", "Condition": null}
	fixture.has("[PLAN-INPUT-06]", gate.deny) with input as fixture.plan([fixture.role_policy([s])])
}

test_pass_single_statement_object if {
	rc := fixture.doc("aws_iam_policy", "module.x.aws_iam_policy.this", "policy", {"Statement": fixture.allow("s3:GetObject")})
	count(gate.deny) == 0 with input as fixture.plan([rc]) with data.policy_plan.required_documents as ["aws_iam_policy"]
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
	count(gate.deny) == 0 with input as fixture.plan([fixture.role_policy([s])]) with data.policy_plan.required_documents as ["aws_iam_role_policy"]
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
	not fixture.has("[PLAN-INPUT-07] aws_iam_role_policy ", msgs)
}

test_violation_declaration_incomplete if {
	every key in ["plan_format_version", "documents", "required_documents", "deployment_identity"] {
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

test_pass_data_source_not_read if {
	read := {"address": "data.aws_iam_policy_document.x", "mode": "data", "type": "aws_iam_policy", "change": {"actions": ["read"], "after": {"policy": "not json"}, "after_unknown": {}}}
	count(gate.deny) == 0 with input as fixture.plan([valid, read]) with data.policy_plan.required_documents as ["aws_iam_role_policy"]
}

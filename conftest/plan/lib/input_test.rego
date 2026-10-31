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
	count(gate.deny) == 0 with input as fixture.plan([valid])
}

# mock provider は表に無い computed な属性に乱数を入れる。表に無い属性は読まない。
test_pass_untracked_attribute if {
	rc := {"address": "module.x.aws_s3_bucket.this", "type": "aws_s3_bucket", "change": {"actions": ["create"], "after": {"policy": "l3mra53t"}, "after_unknown": {}}}
	count(gate.deny) == 0 with input as fixture.plan([valid, rc])
}

test_pass_delete_only if {
	rc := {"address": "module.x.aws_iam_role_policy.old", "type": "aws_iam_role_policy", "change": {"actions": ["delete"], "after": null, "after_unknown": {}}}
	count(gate.deny) == 0 with input as fixture.plan([valid, rc])
}

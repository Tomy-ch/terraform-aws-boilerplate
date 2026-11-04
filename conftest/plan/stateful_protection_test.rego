package plan.stateful_protection_test

import data.plan.fixture
import data.plan.stateful_protection

decl := {"protected_types": ["aws_s3_bucket"], "allow": {}}

bucket := "module.state_backend.module.s3.aws_s3_bucket.this"

change(address, type, actions) := {"address": address, "type": type, "change": {"actions": actions}}

plan_at(path, rcs) := [{"path": path, "contents": {"resource_changes": rcs}}]

test_violation_delete_of_protected_type if {
	msgs := stateful_protection.deny with input as fixture.plan([change(bucket, "aws_s3_bucket", ["delete"])]) with data.policy_plan.stateful_protection as decl
	fixture.has("[PLAN-STATE-01]", msgs)
}

test_violation_replace_of_protected_type if {
	msgs := stateful_protection.deny with input as fixture.plan([change(bucket, "aws_s3_bucket", ["delete", "create"])]) with data.policy_plan.stateful_protection as decl
	some m in msgs
	contains(m, "[PLAN-STATE-01]")
	contains(m, "置換")
}

test_violation_create_before_destroy_replace if {
	msgs := stateful_protection.deny with input as fixture.plan([change(bucket, "aws_s3_bucket", ["create", "delete"])]) with data.policy_plan.stateful_protection as decl
	fixture.has("[PLAN-STATE-01]", msgs)
}

test_pass_delete_of_unprotected_type if {
	msgs := stateful_protection.deny with input as fixture.plan([change("aws_iam_role.x", "aws_iam_role", ["delete"])]) with data.policy_plan.stateful_protection as decl
	count(msgs) == 0
}

test_pass_update_of_protected_type if {
	msgs := stateful_protection.deny with input as fixture.plan([change(bucket, "aws_s3_bucket", ["update"]), change("b", "aws_s3_bucket", ["no-op"])]) with data.policy_plan.stateful_protection as decl
	count(msgs) == 0
}

test_pass_data_source_is_not_a_removal if {
	rc := object.union(change(bucket, "aws_s3_bucket", ["delete"]), {"mode": "data"})
	msgs := stateful_protection.deny with input as fixture.plan([rc]) with data.policy_plan.stateful_protection as decl
	count(msgs) == 0
}

test_pass_allowed_removal_in_its_root if {
	d := {"protected_types": ["aws_s3_bucket"], "allow": {"admin-identity": [{"address": bucket, "reason": "作り直す"}]}}
	msgs := stateful_protection.deny with input as plan_at("tmp/ci-plan/admin-identity.json", [change(bucket, "aws_s3_bucket", ["delete"])]) with data.policy_plan.stateful_protection as d
	count(msgs) == 0
}

test_violation_allow_of_another_root_does_not_apply if {
	d := {"protected_types": ["aws_s3_bucket"], "allow": {"workload": [{"address": bucket, "reason": "作り直す"}]}}
	msgs := stateful_protection.deny with input as plan_at("tmp/ci-plan/admin-identity.json", [change(bucket, "aws_s3_bucket", ["delete"])]) with data.policy_plan.stateful_protection as d
	fixture.has("[PLAN-STATE-01] tmp/ci-plan/admin-identity.json", msgs)
}

test_violation_unmatched_allow_entry if {
	d := {"protected_types": ["aws_s3_bucket"], "allow": {"admin-identity": [{"address": bucket, "reason": "作り直す"}]}}
	msgs := stateful_protection.deny with input as plan_at("tmp/ci-plan/admin-identity.json", [change(bucket, "aws_s3_bucket", ["no-op"])]) with data.policy_plan.stateful_protection as d
	fixture.has("[PLAN-STATE-02]", msgs)
}

test_pass_allow_entry_of_absent_root_is_not_checked if {
	d := {"protected_types": ["aws_s3_bucket"], "allow": {"workload": [{"address": bucket, "reason": "作り直す"}]}}
	msgs := stateful_protection.deny with input as plan_at("tmp/ci-plan/admin-identity.json", [change(bucket, "aws_s3_bucket", ["no-op"])]) with data.policy_plan.stateful_protection as d
	count(msgs) == 0
}

test_violation_declaration_without_state_bucket_type if {
	msgs := stateful_protection.deny with input as fixture.plan([]) with data.policy_plan.stateful_protection as {"protected_types": ["aws_kms_key"], "allow": {}}
	fixture.has("[PLAN-STATE-00]", msgs)
}

test_violation_declaration_with_empty_reason if {
	d := {"protected_types": ["aws_s3_bucket"], "allow": {"p": [{"address": bucket, "reason": ""}]}}
	msgs := stateful_protection.deny with input as fixture.plan([]) with data.policy_plan.stateful_protection as d
	fixture.has("[PLAN-STATE-00]", msgs)
}

test_violation_declaration_missing if {
	msgs := stateful_protection.deny with input as fixture.plan([]) with data.policy_plan as {}
	fixture.has("[PLAN-STATE-00]", msgs)
}

test_violation_plan_without_resource_changes if {
	msgs := stateful_protection.deny with input as [{"path": "tmp/ci-plan/admin-identity.json", "contents": {"format_version": "1.2"}}] with data.policy_plan.stateful_protection as decl
	fixture.has("[PLAN-STATE-03] tmp/ci-plan/admin-identity.json: resource_changes が配列ではありません", msgs)
}

test_violation_removal_without_type if {
	msgs := stateful_protection.deny with input as fixture.plan([{"address": bucket, "change": {"actions": ["delete"]}}]) with data.policy_plan.stateful_protection as decl
	fixture.has("[PLAN-STATE-03]", msgs)
}

test_violation_change_without_actions if {
	msgs := stateful_protection.deny with input as fixture.plan([{"address": bucket, "type": "aws_s3_bucket", "change": {}}]) with data.policy_plan.stateful_protection as decl
	fixture.has("[PLAN-STATE-03]", msgs)
}

test_pass_well_formed_changes if {
	msgs := stateful_protection.deny with input as fixture.plan([change("aws_iam_role.x", "aws_iam_role", ["create"])]) with data.policy_plan.stateful_protection as decl
	count(msgs) == 0
}

test_pass_root_of_nested_path if {
	stateful_protection.root_of("a/b/state-backend.json") == "state-backend"
	stateful_protection.root_of("p.json") == "p"
}

package plan.stateful_protection_test

import data.plan.fixture
import data.plan.stateful_protection

decl := {"protected_types": ["aws_s3_bucket", "aws_s3_bucket_lifecycle_configuration"], "allow": {}}

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

# 宣言（decl.json）を差し替えずに読む。state の鍵の2型が保護の対象から外れると、ここが落ちる。
test_violation_delete_of_state_key_under_declaration if {
	msgs := stateful_protection.deny with input as plan_at("tmp/ci-plan/admin-identity.json", [
		change("module.state_backend.module.kms.aws_kms_key.this", "aws_kms_key", ["delete"]),
		change("module.state_backend.module.kms.aws_kms_key_policy.this", "aws_kms_key_policy", ["delete"]),
	])
	count({m | some m in msgs; contains(m, "[PLAN-STATE-01]")}) == 2
}

test_pass_allowed_removal_in_its_root if {
	d := {"protected_types": ["aws_s3_bucket", "aws_s3_bucket_lifecycle_configuration"], "allow": {"admin-identity": [{"address": bucket, "reason": "作り直す"}]}}
	msgs := stateful_protection.deny with input as plan_at("tmp/ci-plan/admin-identity.json", [change(bucket, "aws_s3_bucket", ["delete"])]) with data.policy_plan.stateful_protection as d
	count(msgs) == 0
}

test_violation_allow_of_another_root_does_not_apply if {
	d := {"protected_types": ["aws_s3_bucket", "aws_s3_bucket_lifecycle_configuration"], "allow": {"workload": [{"address": bucket, "reason": "作り直す"}]}}
	msgs := stateful_protection.deny with input as plan_at("tmp/ci-plan/admin-identity.json", [change(bucket, "aws_s3_bucket", ["delete"])]) with data.policy_plan.stateful_protection as d
	fixture.has("[PLAN-STATE-01] tmp/ci-plan/admin-identity.json", msgs)
}

test_violation_unmatched_allow_entry if {
	d := {"protected_types": ["aws_s3_bucket", "aws_s3_bucket_lifecycle_configuration"], "allow": {"admin-identity": [{"address": bucket, "reason": "作り直す"}]}}
	msgs := stateful_protection.deny with input as plan_at("tmp/ci-plan/admin-identity.json", [change(bucket, "aws_s3_bucket", ["no-op"])]) with data.policy_plan.stateful_protection as d
	fixture.has("[PLAN-STATE-02]", msgs)
}

test_pass_allow_entry_of_absent_root_is_not_checked if {
	d := {"protected_types": ["aws_s3_bucket", "aws_s3_bucket_lifecycle_configuration"], "allow": {"workload": [{"address": bucket, "reason": "作り直す"}]}}
	msgs := stateful_protection.deny with input as plan_at("tmp/ci-plan/admin-identity.json", [change(bucket, "aws_s3_bucket", ["no-op"])]) with data.policy_plan.stateful_protection as d
	count(msgs) == 0
}

test_violation_declaration_without_state_bucket_type if {
	msgs := stateful_protection.deny with input as fixture.plan([]) with data.policy_plan.stateful_protection as {"protected_types": ["aws_kms_key"], "allow": {}}
	fixture.has("[PLAN-STATE-00]", msgs)
}

test_violation_declaration_with_empty_reason if {
	d := {"protected_types": ["aws_s3_bucket", "aws_s3_bucket_lifecycle_configuration"], "allow": {"p": [{"address": bucket, "reason": ""}]}}
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

test_violation_resource_changes_not_array if {
	msgs := stateful_protection.deny with input as [{"path": "p.json", "contents": {"format_version": "1.2", "resource_changes": "abc"}}] with data.policy_plan.stateful_protection as decl
	fixture.has("[PLAN-STATE-03] p.json: resource_changes が配列ではありません", msgs)
}

test_violation_change_without_address if {
	msgs := stateful_protection.deny with input as fixture.plan([{"type": "aws_s3_bucket", "change": {"actions": ["delete"]}}]) with data.policy_plan.stateful_protection as decl
	fixture.has("[PLAN-STATE-03]", msgs)
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

lifecycle := "module.state_backend.module.s3.aws_s3_bucket_lifecycle_configuration.this"

lifecycle_decl := {"protected_types": ["aws_s3_bucket", "aws_s3_bucket_lifecycle_configuration"], "allow": {}}

rule(noncurrent, current) := {
	"id": "r",
	"status": "Enabled",
	"noncurrent_version_expiration": [{"noncurrent_days": noncurrent}],
	"expiration": [{"days": current}],
}

lifecycle_change(actions, before_rules, after_rules) := {
	"address": lifecycle,
	"type": "aws_s3_bucket_lifecycle_configuration",
	"change": {"actions": actions, "before": {"rule": before_rules}, "after": {"rule": after_rules}},
}

lifecycle_update(before_rules, after_rules) := lifecycle_change(["update"], before_rules, after_rules)

test_violation_shortened_noncurrent_days if {
	msgs := stateful_protection.deny with input as fixture.plan([lifecycle_update([rule(90, 0)], [rule(1, 0)])]) with data.policy_plan.stateful_protection as lifecycle_decl
	fixture.has("[PLAN-STATE-04]", msgs)
}

test_violation_shortened_current_days if {
	msgs := stateful_protection.deny with input as fixture.plan([lifecycle_update([rule(7, 7)], [rule(7, 3)])]) with data.policy_plan.stateful_protection as lifecycle_decl
	fixture.has("[PLAN-STATE-04]", msgs)
}

test_violation_current_expiration_added if {
	msgs := stateful_protection.deny with input as fixture.plan([lifecycle_update([rule(90, 0)], [rule(90, 365)])]) with data.policy_plan.stateful_protection as lifecycle_decl
	fixture.has("[PLAN-STATE-04]", msgs)
}

test_violation_shortened_by_a_renamed_rule if {
	renamed := object.union(rule(1, 0), {"id": "other"})
	msgs := stateful_protection.deny with input as fixture.plan([lifecycle_update([rule(90, 0)], [renamed])]) with data.policy_plan.stateful_protection as lifecycle_decl
	fixture.has("[PLAN-STATE-04]", msgs)
}

test_violation_enabling_a_shorter_disabled_rule if {
	disabled := object.union(rule(1, 0), {"id": "d", "status": "Disabled"})
	enabled := object.union(disabled, {"status": "Enabled"})
	msgs := stateful_protection.deny with input as fixture.plan([lifecycle_update([rule(90, 0), disabled], [rule(90, 0), enabled])]) with data.policy_plan.stateful_protection as lifecycle_decl
	fixture.has("[PLAN-STATE-04]", msgs)
}

test_violation_shortening_by_replace_needs_its_own_allow if {
	d := object.union(lifecycle_decl, {"allow": {"p": [{"address": lifecycle, "reason": "作り直す"}]}})
	msgs := stateful_protection.deny with input as fixture.plan([lifecycle_change(["delete", "create"], [rule(90, 0)], [rule(30, 0)])]) with data.policy_plan.stateful_protection as d
	fixture.has("[PLAN-STATE-04]", msgs)
}

test_violation_unmatched_shorten_allow_entry if {
	d := object.union(lifecycle_decl, {"shorten_allow": {"admin-identity": [{"address": lifecycle, "reason": "保持日数を見直す"}]}})
	msgs := stateful_protection.deny with input as plan_at("tmp/ci-plan/admin-identity.json", [lifecycle_update([rule(90, 0)], [rule(120, 0)])]) with data.policy_plan.stateful_protection as d
	fixture.has("[PLAN-STATE-05]", msgs)
}

test_violation_shorten_allow_with_empty_reason if {
	d := object.union(lifecycle_decl, {"shorten_allow": {"p": [{"address": lifecycle, "reason": ""}]}})
	msgs := stateful_protection.deny with input as fixture.plan([]) with data.policy_plan.stateful_protection as d
	fixture.has("[PLAN-STATE-00]", msgs)
}

test_violation_shorten_allow_not_an_object if {
	d := object.union(lifecycle_decl, {"shorten_allow": []})
	msgs := stateful_protection.deny with input as fixture.plan([]) with data.policy_plan.stateful_protection as d
	fixture.has("[PLAN-STATE-00]", msgs)
}

test_pass_allowed_shortening_in_its_root if {
	d := object.union(lifecycle_decl, {"shorten_allow": {"admin-identity": [{"address": lifecycle, "reason": "保持日数を見直す"}]}})
	msgs := stateful_protection.deny with input as plan_at("tmp/ci-plan/admin-identity.json", [lifecycle_update([rule(90, 0)], [rule(30, 0)])]) with data.policy_plan.stateful_protection as d
	count(msgs) == 0
}

test_violation_shorten_allow_of_another_root_does_not_apply if {
	d := object.union(lifecycle_decl, {"shorten_allow": {"workload": [{"address": lifecycle, "reason": "保持日数を見直す"}]}})
	msgs := stateful_protection.deny with input as plan_at("tmp/ci-plan/admin-identity.json", [lifecycle_update([rule(90, 0)], [rule(30, 0)])]) with data.policy_plan.stateful_protection as d
	fixture.has("[PLAN-STATE-04] tmp/ci-plan/admin-identity.json", msgs)
}

test_pass_lengthened_or_unchanged_days if {
	rcs := [lifecycle_update([rule(90, 0)], [rule(120, 0)]), lifecycle_update([rule(90, 0)], [rule(90, 0)])]
	msgs := stateful_protection.deny with input as fixture.plan(rcs) with data.policy_plan.stateful_protection as lifecycle_decl
	count(msgs) == 0
}

test_pass_created_lifecycle_has_no_before if {
	rc := {"address": lifecycle, "type": "aws_s3_bucket_lifecycle_configuration", "change": {"actions": ["create"], "before": null, "after": {"rule": [rule(90, 0)]}}}
	msgs := stateful_protection.deny with input as fixture.plan([rc]) with data.policy_plan.stateful_protection as lifecycle_decl
	count(msgs) == 0
}

test_pass_disabling_or_removing_rules_keeps_versions if {
	disabled := object.union(rule(1, 0), {"status": "Disabled"})
	rcs := [lifecycle_update([rule(90, 0)], [disabled]), lifecycle_update([rule(90, 0)], [])]
	msgs := stateful_protection.deny with input as fixture.plan(rcs) with data.policy_plan.stateful_protection as lifecycle_decl
	count(msgs) == 0
}

test_violation_declaration_without_lifecycle_type if {
	msgs := stateful_protection.deny with input as fixture.plan([lifecycle_update([rule(90, 0)], [rule(1, 0)])]) with data.policy_plan.stateful_protection as {"protected_types": ["aws_s3_bucket"], "allow": {}}
	fixture.has("[PLAN-STATE-00]", msgs)
}

test_pass_shortening_of_another_type if {
	rc := object.union(lifecycle_update([rule(90, 0)], [rule(1, 0)]), {"type": "aws_s3_bucket"})
	msgs := stateful_protection.deny with input as fixture.plan([rc]) with data.policy_plan.stateful_protection as decl
	count(msgs) == 0
}

dated(date) := object.union(rule(90, 0), {"expiration": [{"days": 0, "date": date}]})

test_violation_current_expiration_by_date_added if {
	msgs := stateful_protection.deny with input as fixture.plan([lifecycle_update([rule(90, 0)], [dated("2026-12-01T00:00:00Z")])]) with data.policy_plan.stateful_protection as lifecycle_decl
	fixture.has("[PLAN-STATE-04]", msgs)
}

test_violation_current_expiration_date_moved_earlier if {
	msgs := stateful_protection.deny with input as fixture.plan([lifecycle_update([dated("2027-01-01T00:00:00Z")], [dated("2026-12-01T00:00:00Z")])]) with data.policy_plan.stateful_protection as lifecycle_decl
	fixture.has("[PLAN-STATE-04]", msgs)
}

test_pass_current_expiration_date_moved_later if {
	msgs := stateful_protection.deny with input as fixture.plan([lifecycle_update([dated("2026-12-01T00:00:00Z")], [dated("2027-01-01T00:00:00Z")])]) with data.policy_plan.stateful_protection as lifecycle_decl
	count(msgs) == 0
}

test_pass_removing_the_shortest_of_two_rules_lengthens if {
	shortest := object.union(rule(30, 0), {"id": "s"})
	msgs := stateful_protection.deny with input as fixture.plan([lifecycle_update([rule(90, 0), shortest], [rule(60, 0)])]) with data.policy_plan.stateful_protection as lifecycle_decl
	count(msgs) == 0
}

test_pass_rule_without_status_is_not_enabled if {
	no_status := object.remove(rule(1, 0), ["status"])
	msgs := stateful_protection.deny with input as fixture.plan([lifecycle_update([rule(90, 0)], [no_status])]) with data.policy_plan.stateful_protection as lifecycle_decl
	count(msgs) == 0
}

test_violation_unknown_after_days if {
	rc := object.union(lifecycle_update([rule(90, 0)], [{"id": "r", "status": "Enabled", "noncurrent_version_expiration": [{}], "expiration": [{"days": 0}]}]), {"change": {"after_unknown": {"rule": [{"noncurrent_version_expiration": [{"noncurrent_days": true}]}]}}})
	msgs := stateful_protection.deny with input as fixture.plan([rc]) with data.policy_plan.stateful_protection as lifecycle_decl
	fixture.has("[PLAN-STATE-06]", msgs)
}

test_violation_unknown_after_rules if {
	rc := object.union(lifecycle_update([rule(90, 0)], []), {"change": {"after_unknown": {"rule": true}}})
	msgs := stateful_protection.deny with input as fixture.plan([rc]) with data.policy_plan.stateful_protection as lifecycle_decl
	fixture.has("[PLAN-STATE-06]", msgs)
}

test_pass_unknown_values_of_a_created_lifecycle if {
	rc := {"address": lifecycle, "type": "aws_s3_bucket_lifecycle_configuration", "change": {"actions": ["create"], "before": null, "after": {"rule": [rule(90, 0)]}, "after_unknown": {"rule": [{"expiration": [{"days": true}]}]}}}
	msgs := stateful_protection.deny with input as fixture.plan([rc]) with data.policy_plan.stateful_protection as lifecycle_decl
	count(msgs) == 0
}

test_pass_unknown_values_outside_the_limits if {
	rc := object.union(lifecycle_update([rule(90, 0)], [rule(90, 0)]), {"change": {"after_unknown": {"rule": [{"filter": [{"prefix": true}]}]}}})
	msgs := stateful_protection.deny with input as fixture.plan([rc]) with data.policy_plan.stateful_protection as lifecycle_decl
	count(msgs) == 0
}

other_lifecycle := "module.state_backend.module.s3.aws_s3_bucket_lifecycle_configuration.that"

forgotten := {"address": lifecycle, "type": "aws_s3_bucket_lifecycle_configuration", "change": {"actions": ["forget"], "before": {"rule": [rule(90, 0)]}, "after": null}}

created(rules) := {"address": other_lifecycle, "type": "aws_s3_bucket_lifecycle_configuration", "change": {"actions": ["create"], "before": null, "after": {"rule": rules}}}

test_violation_forget_of_protected_type if {
	msgs := stateful_protection.deny with input as fixture.plan([forgotten]) with data.policy_plan.stateful_protection as lifecycle_decl
	some m in msgs
	contains(m, "[PLAN-STATE-01]")
	contains(m, "管理から除外")
}

test_violation_shortening_by_forget_and_create_at_another_address if {
	d := object.union(lifecycle_decl, {"allow": {"p": [{"address": lifecycle, "reason": "address を替える"}]}})
	msgs := stateful_protection.deny with input as fixture.plan([forgotten, created([rule(1, 0)])]) with data.policy_plan.stateful_protection as d
	fixture.has(sprintf("[PLAN-STATE-04] p.json: %s", [other_lifecycle]), msgs)
}

test_pass_moving_to_another_address_without_shortening if {
	d := object.union(lifecycle_decl, {"allow": {"p": [{"address": lifecycle, "reason": "address を替える"}]}})
	msgs := stateful_protection.deny with input as fixture.plan([forgotten, created([rule(90, 0)])]) with data.policy_plan.stateful_protection as d
	count(msgs) == 0
}

test_violation_shorten_allow_matched_only_in_another_root if {
	d := object.union(lifecycle_decl, {"shorten_allow": {"admin-identity": [{"address": lifecycle, "reason": "保持日数を見直す"}], "workload": [{"address": lifecycle, "reason": "保持日数を見直す"}]}})
	inputs := array.concat(plan_at("tmp/ci-plan/admin-identity.json", [lifecycle_update([rule(90, 0)], [rule(90, 0)])]), plan_at("tmp/ci-plan/workload.json", [lifecycle_update([rule(90, 0)], [rule(30, 0)])]))
	msgs := stateful_protection.deny with input as inputs with data.policy_plan.stateful_protection as d
	fixture.has("[PLAN-STATE-05] tmp/ci-plan/admin-identity.json", msgs)
	count(msgs) == 1
}

test_pass_shorten_allow_entry_of_absent_root_is_not_checked if {
	d := object.union(lifecycle_decl, {"shorten_allow": {"workload": [{"address": lifecycle, "reason": "保持日数を見直す"}]}})
	msgs := stateful_protection.deny with input as plan_at("tmp/ci-plan/admin-identity.json", [lifecycle_update([rule(90, 0)], [rule(90, 0)])]) with data.policy_plan.stateful_protection as d
	count(msgs) == 0
}

test_violation_shorten_allow_address_not_a_string if {
	d := object.union(lifecycle_decl, {"shorten_allow": {"p": [{"address": 1, "reason": "保持日数を見直す"}]}})
	msgs := stateful_protection.deny with input as fixture.plan([]) with data.policy_plan.stateful_protection as d
	fixture.has("[PLAN-STATE-00]", msgs)
}

deleted := object.union(forgotten, {"change": {"actions": ["delete"]}})

moving_allow := object.union(lifecycle_decl, {"allow": {"p": [{"address": lifecycle, "reason": "address を替える"}]}})

test_violation_shortening_by_delete_and_create_at_another_address if {
	msgs := stateful_protection.deny with input as fixture.plan([deleted, created([rule(1, 0)])]) with data.policy_plan.stateful_protection as moving_allow
	fixture.has(sprintf("[PLAN-STATE-04] p.json: %s", [other_lifecycle]), msgs)
}

test_pass_update_beside_a_forget_is_compared_with_its_own_before if {
	updated := object.union(lifecycle_update([rule(30, 0)], [rule(60, 0)]), {"address": other_lifecycle})
	replaced := object.union(lifecycle_change(["delete", "create"], [rule(30, 0)], [rule(60, 0)]), {"address": "module.x.aws_s3_bucket_lifecycle_configuration.y"})
	d := object.union(lifecycle_decl, {"allow": {"p": [{"address": lifecycle, "reason": "外す"}, {"address": "module.x.aws_s3_bucket_lifecycle_configuration.y", "reason": "作り直す"}]}})
	msgs := stateful_protection.deny with input as fixture.plan([forgotten, updated, replaced]) with data.policy_plan.stateful_protection as d
	count(msgs) == 0
}

test_violation_unknown_values_of_a_paired_create if {
	rc := object.union(created([rule(90, 0)]), {"change": {"after_unknown": {"rule": [{"noncurrent_version_expiration": [{"noncurrent_days": true}]}]}}})
	msgs := stateful_protection.deny with input as fixture.plan([forgotten, rc]) with data.policy_plan.stateful_protection as moving_allow
	fixture.has("[PLAN-STATE-06]", msgs)
}

unknown_after(au) := object.union(lifecycle_update([rule(90, 0)], [rule(90, 0)]), {"change": {"after_unknown": {"rule": au}}})

test_violation_unknown_after_rule_element if {
	msgs := stateful_protection.deny with input as fixture.plan([unknown_after([true])]) with data.policy_plan.stateful_protection as lifecycle_decl
	fixture.has("[PLAN-STATE-06]", msgs)
}

test_violation_unknown_after_status if {
	msgs := stateful_protection.deny with input as fixture.plan([unknown_after([{"status": true}])]) with data.policy_plan.stateful_protection as lifecycle_decl
	fixture.has("[PLAN-STATE-06]", msgs)
}

test_violation_unknown_after_expiration_block if {
	msgs := stateful_protection.deny with input as fixture.plan([unknown_after([{"expiration": true}])]) with data.policy_plan.stateful_protection as lifecycle_decl
	fixture.has("[PLAN-STATE-06]", msgs)
}

test_violation_unknown_after_expiration_element if {
	msgs := stateful_protection.deny with input as fixture.plan([unknown_after([{"expiration": [true]}])]) with data.policy_plan.stateful_protection as lifecycle_decl
	fixture.has("[PLAN-STATE-06]", msgs)
}

test_pass_unknown_non_limit_attribute_inside_a_limit_block if {
	au := [{"expiration": [{"expired_object_delete_marker": true}], "noncurrent_version_expiration": [{"newer_noncurrent_versions": true}]}]
	msgs := stateful_protection.deny with input as fixture.plan([unknown_after(au)]) with data.policy_plan.stateful_protection as lifecycle_decl
	count(msgs) == 0
}

test_pass_empty_or_null_date_is_not_a_limit if {
	rcs := [lifecycle_update([rule(90, 0)], [dated("")]), lifecycle_update([rule(90, 0)], [dated(null)])]
	msgs := stateful_protection.deny with input as fixture.plan(rcs) with data.policy_plan.stateful_protection as lifecycle_decl
	count(msgs) == 0
}

test_violation_earlier_date_written_with_an_offset if {
	msgs := stateful_protection.deny with input as fixture.plan([lifecycle_update([dated("2026-11-30T20:00:00Z")], [dated("2026-12-01T00:00:00+09:00")])]) with data.policy_plan.stateful_protection as lifecycle_decl
	fixture.has("[PLAN-STATE-04]", msgs)
}

test_pass_same_date_written_with_an_offset if {
	msgs := stateful_protection.deny with input as fixture.plan([lifecycle_update([dated("2026-12-01T00:00:00Z")], [dated("2026-12-01T09:00:00+09:00")])]) with data.policy_plan.stateful_protection as lifecycle_decl
	count(msgs) == 0
}

test_violation_unreadable_date if {
	msgs := stateful_protection.deny with input as fixture.plan([lifecycle_update([rule(90, 0)], [dated("2026-12-01")])]) with data.policy_plan.stateful_protection as lifecycle_decl
	fixture.has("[PLAN-STATE-06]", msgs)
}

on_bucket(rc, bucket) := object.union(rc, {"change": {"after": {"bucket": bucket}}})

test_violation_two_lifecycles_on_one_bucket if {
	rcs := [on_bucket(lifecycle_update([rule(90, 0)], [rule(90, 0)]), "b"), on_bucket(created([rule(1, 0)]), "b")]
	msgs := stateful_protection.deny with input as fixture.plan(rcs) with data.policy_plan.stateful_protection as lifecycle_decl
	fixture.has("[PLAN-STATE-07]", msgs)
}

test_pass_lifecycles_on_different_buckets if {
	rcs := [on_bucket(lifecycle_update([rule(90, 0)], [rule(90, 0)]), "b"), on_bucket(created([rule(7, 7)]), "c")]
	msgs := stateful_protection.deny with input as fixture.plan(rcs) with data.policy_plan.stateful_protection as lifecycle_decl
	count(msgs) == 0
}

test_pass_moving_a_lifecycle_of_one_bucket if {
	moved_out := object.union(deleted, {"change": {"before": {"bucket": "b"}}})
	msgs := stateful_protection.deny with input as fixture.plan([moved_out, on_bucket(created([rule(90, 0)]), "b")]) with data.policy_plan.stateful_protection as moving_allow
	count(msgs) == 0
}

bucket_update(before_rules, after_rules) := {
	"address": bucket,
	"type": "aws_s3_bucket",
	"change": {"actions": ["update"], "before": {"lifecycle_rule": before_rules}, "after": {"lifecycle_rule": after_rules}},
}

inline_rule := {"enabled": true, "noncurrent_version_expiration": [{"days": 1}]}

test_violation_inline_lifecycle_rule if {
	msgs := stateful_protection.deny with input as fixture.plan([bucket_update([], [inline_rule])]) with data.policy_plan.stateful_protection as lifecycle_decl
	fixture.has("[PLAN-STATE-08]", msgs)
}

test_violation_inline_lifecycle_rule_on_a_created_bucket if {
	rc := {"address": bucket, "type": "aws_s3_bucket", "change": {"actions": ["create"], "before": null, "after": {"lifecycle_rule": [inline_rule]}}}
	msgs := stateful_protection.deny with input as fixture.plan([rc]) with data.policy_plan.stateful_protection as lifecycle_decl
	fixture.has("[PLAN-STATE-08]", msgs)
}

test_pass_unchanged_lifecycle_rule_read_from_the_bucket if {
	msgs := stateful_protection.deny with input as fixture.plan([bucket_update([inline_rule], [inline_rule]), bucket_update([], [])]) with data.policy_plan.stateful_protection as lifecycle_decl
	count(msgs) == 0
}

test_violation_shorten_allow_entries_not_an_array if {
	d := object.union(lifecycle_decl, {"shorten_allow": {"p": {"x": {"address": lifecycle, "reason": "保持日数を見直す"}}}})
	msgs := stateful_protection.deny with input as fixture.plan([]) with data.policy_plan.stateful_protection as d
	fixture.has("[PLAN-STATE-00]", msgs)
}

test_violation_shorten_allow_reason_not_a_string if {
	d := object.union(lifecycle_decl, {"shorten_allow": {"p": [{"address": lifecycle, "reason": 1}]}})
	msgs := stateful_protection.deny with input as fixture.plan([]) with data.policy_plan.stateful_protection as d
	fixture.has("[PLAN-STATE-00]", msgs)
}

test_violation_removal_of_protected_lifecycle if {
	rc := {"address": lifecycle, "type": "aws_s3_bucket_lifecycle_configuration", "change": {"actions": ["delete"]}}
	msgs := stateful_protection.deny with input as fixture.plan([rc]) with data.policy_plan.stateful_protection as lifecycle_decl
	fixture.has("[PLAN-STATE-01]", msgs)
}

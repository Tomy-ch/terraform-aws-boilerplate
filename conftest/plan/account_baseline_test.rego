package plan.account_baseline_test

import data.plan.account_baseline as p
import data.plan.fixture

decl := {
	"regions": ["ap-northeast-1", "us-east-1"],
	"regional": {
		"ebs_encryption": "aws_ebs_encryption_by_default",
		"analyzer": "aws_accessanalyzer_analyzer",
		"emr_block_public_access": "aws_emr_block_public_access_configuration",
		"ssm_service_setting": "aws_ssm_service_setting",
		"vpc_block_public_access": "aws_vpc_block_public_access_options",
	},
	"config": {
		"recorder": "aws_config_configuration_recorder",
		"delivery": "aws_config_delivery_channel",
		"status": "aws_config_configuration_recorder_status",
		"retention": "aws_config_retention_configuration",
	},
	"public_access_block": "aws_s3_account_public_access_block",
	"owners": {
		"aws_s3_account_public_access_block": ["account-baseline"],
		"aws_iam_account_password_policy": ["account-baseline", "organization-baseline"],
	},
}

plan_of(path, rcs) := [{"path": path, "contents": {"plan_format_version": "1.2", "resource_changes": rcs}}]

ab(rcs) := plan_of("tmp/tf-policy/account-baseline.json", rcs)

change(type, address, after) := {
	"address": address,
	"type": type,
	"change": {"actions": ["create"], "after": after, "after_unknown": {}},
}

slr := "arn:aws:iam::123456789012:role/aws-service-role/config.amazonaws.com/AWSServiceRoleForConfig"

regional(region) := [
	change("aws_ebs_encryption_by_default", sprintf("module.ab.module.regional_controls.aws_ebs_encryption_by_default.this[%q]", [region]), {"region": region, "enabled": true}),
	change("aws_accessanalyzer_analyzer", sprintf("module.ab.module.regional_controls.aws_accessanalyzer_analyzer.this[%q]", [region]), {"region": region, "type": "ACCOUNT"}),
	change("aws_emr_block_public_access_configuration", sprintf("module.ab.module.regional_controls.aws_emr_block_public_access_configuration.this[%q]", [region]), {"region": region, "block_public_security_group_rules": true, "permitted_public_security_group_rule_range": [{"min_range": 22, "max_range": 22}]}),
	change("aws_ssm_service_setting", sprintf("module.ab.module.regional_controls.aws_ssm_service_setting.document_public_sharing[%q]", [region]), {"region": region, "setting_id": "/ssm/documents/console/public-sharing-permission", "setting_value": "Disable"}),
	change("aws_ssm_service_setting", sprintf("module.ab.module.regional_controls.aws_ssm_service_setting.automation_log_destination[%q]", [region]), {"region": region, "setting_id": "/ssm/automation/customer-script-log-destination", "setting_value": "CloudWatch"}),
	change("aws_vpc_block_public_access_options", sprintf("module.ab.module.regional_controls.aws_vpc_block_public_access_options.this[%q]", [region]), {"region": region, "internet_gateway_block_mode": "block-bidirectional"}),
]

config(region, global) := [
	change("aws_config_configuration_recorder", sprintf("module.ab.module.config_recorder[0].aws_config_configuration_recorder.this[%q]", [region]), {"region": region, "role_arn": slr, "recording_group": [{"all_supported": true, "include_global_resource_types": global}]}),
	change("aws_config_delivery_channel", sprintf("module.ab.module.config_recorder[0].aws_config_delivery_channel.this[%q]", [region]), {"region": region}),
	change("aws_config_configuration_recorder_status", sprintf("module.ab.module.config_recorder[0].aws_config_configuration_recorder_status.this[%q]", [region]), {"region": region, "is_enabled": true}),
	change("aws_config_retention_configuration", sprintf("module.ab.module.config_recorder[0].aws_config_retention_configuration.this[%q]", [region]), {"region": region}),
]

pab := change("aws_s3_account_public_access_block", "module.ab.module.account_controls.aws_s3_account_public_access_block.this", {"block_public_acls": true, "block_public_policy": true, "ignore_public_acls": true, "restrict_public_buckets": true})

ok := array.concat(array.concat(array.concat(regional("ap-northeast-1"), regional("us-east-1")), array.concat(config("ap-northeast-1", true), config("us-east-1", false))), [pab])

patched(rc, address, after) := object.union(rc, {"change": object.union(rc.change, {"after": object.union(rc.change.after, after)})}) if {
	rc.address == address
}

patched(rc, address, _) := rc if rc.address != address

patch_all(rcs, address, after) := [patched(rc, address, after) | some rc in rcs]

unknown_region(rc, address) := object.union(rc, {"change": object.union(rc.change, {"after_unknown": {"region": true}})}) if {
	rc.address == address
}

unknown_region(rc, address) := rc if rc.address != address

without(rcs, address) := [rc | some rc in rcs; rc.address != address]

test_pass_every_region if {
	count(p.deny) == 0 with input as ab(ok) with data.policy_plan.account_baseline as decl
}

renamed(rc) := object.union(rc, {"address": replace(rc.address, "module.ab.", "module.sandbox.")})

test_pass_sandbox_without_config if {
	sandbox := [renamed(rc) | some rc in ok; not startswith(rc.type, "aws_config_"); rc.type != "aws_s3_account_public_access_block"]
	count(p.deny) == 0 with input as ab(array.concat(ok, sandbox)) with data.policy_plan.account_baseline as decl
}

test_violation_no_declaration if {
	fixture.has("[PLAN-ABL-00]", p.deny) with input as ab(ok) with data.policy_plan.account_baseline as null
}

test_violation_no_analyzer_at_all if {
	rcs := [rc | some rc in ok; rc.type != "aws_accessanalyzer_analyzer"]
	fixture.has("[PLAN-ABL-00]", p.deny) with input as ab(rcs) with data.policy_plan.account_baseline as decl
}

test_violation_region_missing if {
	rcs := without(ok, `module.ab.module.regional_controls.aws_ebs_encryption_by_default.this["us-east-1"]`)
	fixture.has("[PLAN-ABL-01]", p.deny) with input as ab(rcs) with data.policy_plan.account_baseline as decl
}

test_violation_config_region_missing if {
	rcs := without(ok, `module.ab.module.config_recorder[0].aws_config_delivery_channel.this["us-east-1"]`)
	fixture.has("[PLAN-ABL-01]", p.deny) with input as ab(rcs) with data.policy_plan.account_baseline as decl
}

test_violation_region_unknown if {
	address := `module.ab.module.regional_controls.aws_ebs_encryption_by_default.this["us-east-1"]`
	rcs := [unknown_region(rc, address) | some rc in ok]
	fixture.has("[PLAN-ABL-01]", p.deny) with input as ab(rcs) with data.policy_plan.account_baseline as decl
}

test_violation_unreadable_instance if {
	rcs := array.concat(ok, [change("aws_accessanalyzer_analyzer", "aws_accessanalyzer_analyzer.loose", {"region": "us-east-1", "type": "ACCOUNT"})])
	fixture.has("[PLAN-ABL-01]", p.deny) with input as ab(rcs) with data.policy_plan.account_baseline as decl
}

test_violation_ebs_disabled if {
	rcs := patch_all(ok, `module.ab.module.regional_controls.aws_ebs_encryption_by_default.this["us-east-1"]`, {"enabled": false})
	fixture.has("[PLAN-ABL-02]", p.deny) with input as ab(rcs) with data.policy_plan.account_baseline as decl
}

test_violation_organization_analyzer if {
	rcs := patch_all(ok, `module.ab.module.regional_controls.aws_accessanalyzer_analyzer.this["us-east-1"]`, {"type": "ORGANIZATION"})
	fixture.has("[PLAN-ABL-02]", p.deny) with input as ab(rcs) with data.policy_plan.account_baseline as decl
}

test_violation_emr_allows_other_port if {
	rcs := patch_all(ok, `module.ab.module.regional_controls.aws_emr_block_public_access_configuration.this["us-east-1"]`, {"permitted_public_security_group_rule_range": [{"min_range": 22, "max_range": 443}]})
	fixture.has("[PLAN-ABL-02]", p.deny) with input as ab(rcs) with data.policy_plan.account_baseline as decl
}

test_violation_document_sharing_enabled if {
	rcs := patch_all(ok, `module.ab.module.regional_controls.aws_ssm_service_setting.document_public_sharing["us-east-1"]`, {"setting_value": "Enable"})
	fixture.has("[PLAN-ABL-02]", p.deny) with input as ab(rcs) with data.policy_plan.account_baseline as decl
}

test_violation_automation_log_setting_missing if {
	rcs := [rc | some rc in ok; not contains(rc.address, "automation_log_destination")]
	fixture.has("[PLAN-ABL-02]", p.deny) with input as ab(rcs) with data.policy_plan.account_baseline as decl
}

test_violation_recorder_disabled if {
	rcs := patch_all(ok, `module.ab.module.config_recorder[0].aws_config_configuration_recorder_status.this["us-east-1"]`, {"is_enabled": false})
	fixture.has("[PLAN-ABL-02]", p.deny) with input as ab(rcs) with data.policy_plan.account_baseline as decl
}

test_violation_recorder_with_custom_role if {
	rcs := patch_all(ok, `module.ab.module.config_recorder[0].aws_config_configuration_recorder.this["us-east-1"]`, {"role_arn": "arn:aws:iam::123456789012:role/config-recorder"})
	fixture.has("[PLAN-ABL-02]", p.deny) with input as ab(rcs) with data.policy_plan.account_baseline as decl
}

test_violation_global_resources_twice if {
	rcs := patch_all(ok, `module.ab.module.config_recorder[0].aws_config_configuration_recorder.this["us-east-1"]`, {"recording_group": [{"all_supported": true, "include_global_resource_types": true}]})
	fixture.has("[PLAN-ABL-03]", p.deny) with input as ab(rcs) with data.policy_plan.account_baseline as decl
}

test_violation_public_access_block_off if {
	rcs := patch_all(ok, "module.ab.module.account_controls.aws_s3_account_public_access_block.this", {"restrict_public_buckets": false})
	fixture.has("[PLAN-ABL-04]", p.deny) with input as ab(rcs) with data.policy_plan.account_baseline as decl
}

test_violation_owned_outside if {
	other := plan_of("tmp/tf-policy/state-backend.json", [change("aws_s3_account_public_access_block", "module.sb.aws_s3_account_public_access_block.x", {"block_public_acls": true, "block_public_policy": true, "ignore_public_acls": true, "restrict_public_buckets": true})])
	fixture.has("[PLAN-ABL-05]", p.deny) with input as array.concat(ab(ok), other) with data.policy_plan.account_baseline as decl
}

test_pass_password_policy_in_organization_baseline if {
	other := plan_of("tmp/tf-policy/organization-baseline.json", [change("aws_iam_account_password_policy", "module.ob.module.break_glass.aws_iam_account_password_policy.this", {})])
	count(p.deny) == 0 with input as array.concat(ab(ok), other) with data.policy_plan.account_baseline as decl
}

test_violation_vpc_block_ingress_only if {
	rcs := patch_all(ok, `module.ab.module.regional_controls.aws_vpc_block_public_access_options.this["us-east-1"]`, {"internet_gateway_block_mode": "block-ingress"})
	fixture.has("[PLAN-ABL-02]", p.deny) with input as ab(rcs) with data.policy_plan.account_baseline as decl
}

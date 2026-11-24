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
	"password_policy_keys": ["minimum_password_length", "max_password_age"],
}

# password policy の持ち主は層の宣言（conftest/hcl/layer/decl.json の singletons）から導く。
layer_decl := {"singletons": {"aws_iam_account_password_policy": ["account-baseline", "organization-baseline"]}}

plan_of(path, rcs) := [{"path": path, "contents": {"plan_format_version": "1.2", "resource_changes": rcs}}]

ab(rcs) := plan_of("tmp/tf-policy/account-baseline.json", rcs)

# 管理アカウントの password policy は organization-baseline の break-glass が持つ。比べる相手として毎回入れる。
org_baseline := plan_of("tmp/tf-policy/organization-baseline.json", [password_policy("ob.module.break_glass", 14)])

denies(rcs) := msgs if {
	msgs := p.deny with input as array.concat(ab(rcs), org_baseline) with data.layer_decl as layer_decl with data.policy_plan.account_baseline as decl
}

# 期待する ID だけが出たこと。ほかの規則が同時に落ちた入力を、目当ての分岐のテストとして数えない。
only(id, msgs) if {
	count(msgs) > 0
	every m in msgs {
		startswith(m, id)
	}
}

change(type, address, after) := {
	"address": address,
	"type": type,
	"change": {"actions": ["create"], "after": after, "after_unknown": {}},
}

slr := "arn:aws:iam::123456789012:role/aws-service-role/config.amazonaws.com/AWSServiceRoleForConfig"

regional(m, region) := [
	change("aws_ebs_encryption_by_default", sprintf("module.%s.module.regional_controls.aws_ebs_encryption_by_default.this[%q]", [m, region]), {"region": region, "enabled": true}),
	change("aws_accessanalyzer_analyzer", sprintf("module.%s.module.regional_controls.aws_accessanalyzer_analyzer.this[%q]", [m, region]), {"region": region, "type": "ACCOUNT"}),
	change("aws_emr_block_public_access_configuration", sprintf("module.%s.module.regional_controls.aws_emr_block_public_access_configuration.this[%q]", [m, region]), {"region": region, "block_public_security_group_rules": true, "permitted_public_security_group_rule_range": [{"min_range": 22, "max_range": 22}]}),
	change("aws_ssm_service_setting", sprintf("module.%s.module.regional_controls.aws_ssm_service_setting.document_public_sharing[%q]", [m, region]), {"region": region, "setting_id": "/ssm/documents/console/public-sharing-permission", "setting_value": "Disable"}),
	change("aws_ssm_service_setting", sprintf("module.%s.module.regional_controls.aws_ssm_service_setting.automation_log_destination[%q]", [m, region]), {"region": region, "setting_id": "/ssm/automation/customer-script-log-destination", "setting_value": "CloudWatch"}),
	change("aws_vpc_block_public_access_options", sprintf("module.%s.module.regional_controls.aws_vpc_block_public_access_options.this[%q]", [m, region]), {"region": region, "internet_gateway_block_mode": "block-bidirectional"}),
]

config(m, region, global) := [
	change("aws_config_configuration_recorder", sprintf("module.%s.module.config_recorder[0].aws_config_configuration_recorder.this[%q]", [m, region]), {"region": region, "role_arn": slr, "recording_group": [{"all_supported": true, "include_global_resource_types": global}]}),
	change("aws_config_delivery_channel", sprintf("module.%s.module.config_recorder[0].aws_config_delivery_channel.this[%q]", [m, region]), {"region": region}),
	change("aws_config_configuration_recorder_status", sprintf("module.%s.module.config_recorder[0].aws_config_configuration_recorder_status.this[%q]", [m, region]), {"region": region, "is_enabled": true}),
	change("aws_config_retention_configuration", sprintf("module.%s.module.config_recorder[0].aws_config_retention_configuration.this[%q]", [m, region]), {"region": region}),
]

pab(m) := change("aws_s3_account_public_access_block", sprintf("module.%s.module.account_controls.aws_s3_account_public_access_block.this", [m]), {"block_public_acls": true, "block_public_policy": true, "ignore_public_acls": true, "restrict_public_buckets": true})

password_policy(path_module, minimum) := change("aws_iam_account_password_policy", sprintf("module.%s.aws_iam_account_password_policy.this", [path_module]), {"minimum_password_length": minimum, "max_password_age": 90})

instance(m) := array.concat(array.concat(array.concat(regional(m, "ap-northeast-1"), regional(m, "us-east-1")), array.concat(config(m, "ap-northeast-1", true), config(m, "us-east-1", false))), [pab(m), password_policy(sprintf("%s.module.account_controls", [m]), 14)])

# sandbox と同じく、Config を持たない instance。
no_config(m) := array.concat(array.concat(regional(m, "ap-northeast-1"), regional(m, "us-east-1")), [pab(m)])

ok := instance("dev")

patched(rc, address, after) := object.union(rc, {"change": object.union(rc.change, {"after": object.union(rc.change.after, after)})}) if {
	rc.address == address
}

patched(rc, address, _) := rc if rc.address != address

patch_all(rcs, address, after) := [patched(rc, address, after) | some rc in rcs]

unknown_of(rc, address, key) := object.union(rc, {"change": object.union(rc.change, {"after_unknown": {key: true}})}) if {
	rc.address == address
}

unknown_of(rc, address, _) := rc if rc.address != address

# object.union は入れ子のオブジェクトを再帰に合わせるので、キーを消した値で置き換えるときに使う。
set_key(obj, k, v) := object.union(object.remove(obj, [k]), {k: v})

without(rcs, address) := [rc | some rc in rcs; rc.address != address]

addr(type, name, region) := sprintf("module.dev.module.regional_controls.%s.%s[%q]", [type, name, region])

cfg(type, region) := sprintf("module.dev.module.config_recorder[0].%s.this[%q]", [type, region])

test_pass_every_region if {
	count(denies(ok)) == 0
}

test_pass_sandbox_without_config if {
	count(denies(array.concat(ok, no_config("sandbox")))) == 0
}

test_pass_deleted_unsafe_setting_is_not_read if {
	gone := {"address": "module.dev.module.regional_controls.aws_ebs_encryption_by_default.this[\"eu-west-1\"]", "type": "aws_ebs_encryption_by_default", "change": {"actions": ["delete"], "after": null, "after_unknown": {}}}
	count(denies(array.concat(ok, [gone]))) == 0
}

test_violation_no_declaration if {
	fixture.has("[PLAN-ACB-00]", p.deny) with input as array.concat(ab(ok), org_baseline) with data.layer_decl as layer_decl with data.policy_plan.account_baseline as null
}

test_violation_each_declaration_key_missing if {
	every k in ["regions", "regional", "config", "public_access_block", "password_policy_keys"] {
		fixture.has("[PLAN-ACB-00]", p.deny) with input as ab(ok) with data.layer_decl as layer_decl with data.policy_plan.account_baseline as object.remove(decl, [k])
	}
}

test_violation_declaration_inner_key_missing if {
	every k in ["recorder", "status", "delivery", "retention"] {
		fixture.has("[PLAN-ACB-00]", p.deny) with input as ab(ok) with data.layer_decl as layer_decl with data.policy_plan.account_baseline as set_key(decl, "config", object.remove(decl.config, [k]))
	}
	every k in ["ebs_encryption", "analyzer", "emr_block_public_access", "ssm_service_setting", "vpc_block_public_access"] {
		fixture.has("[PLAN-ACB-00]", p.deny) with input as ab(ok) with data.layer_decl as layer_decl with data.policy_plan.account_baseline as set_key(decl, "regional", object.remove(decl.regional, [k]))
	}
}

test_violation_declaration_values_malformed if {
	every bad in [
		["regions", []], ["regions", "ap-northeast-1"],
		["public_access_block", 1], ["password_policy_keys", []], ["password_policy_keys", [1]],
		["password_policy_keys", [""]], ["password_policy_keys", ["minimum_password_length", "minimum_password_length"]],
	] {
		fixture.has("[PLAN-ACB-00]", p.deny) with input as ab(ok) with data.layer_decl as layer_decl with data.policy_plan.account_baseline as set_key(decl, bad[0], bad[1])
	}
}

test_violation_empty_input if {
	fixture.has("[PLAN-ACB-00]", denies([]))
}

test_violation_each_required_type_absent if {
	every type in ["aws_ebs_encryption_by_default", "aws_accessanalyzer_analyzer", "aws_emr_block_public_access_configuration", "aws_ssm_service_setting", "aws_vpc_block_public_access_options", "aws_s3_account_public_access_block", "aws_config_configuration_recorder"] {
		fixture.has("[PLAN-ACB-00]", denies([rc | some rc in ok; rc.type != type]))
	}
}

test_violation_settings_only_in_another_usecase if {
	other := plan_of("tmp/tf-policy/state-backend.json", ok)
	fixture.has("[PLAN-ACB-00]", p.deny) with input as other with data.layer_decl as layer_decl with data.policy_plan.account_baseline as decl
}

test_violation_region_missing if {
	only("[PLAN-ACB-01]", denies(without(ok, addr("aws_ebs_encryption_by_default", "this", "us-east-1"))))
}

test_violation_region_extra if {
	only("[PLAN-ACB-01]", denies(array.concat(ok, [change("aws_ebs_encryption_by_default", addr("aws_ebs_encryption_by_default", "this", "af-south-1"), {"region": "af-south-1", "enabled": true})])))
}

test_violation_region_missing_in_one_instance if {
	sandbox := without(no_config("sandbox"), "module.sandbox.module.regional_controls.aws_vpc_block_public_access_options.this[\"us-east-1\"]")
	only("[PLAN-ACB-01]", denies(array.concat(ok, sandbox)))
}

test_violation_ssm_setting_missing_in_one_region if {
	every name in ["automation_log_destination", "document_public_sharing"] {
		only("[PLAN-ACB-01]", denies(without(ok, addr("aws_ssm_service_setting", name, "us-east-1"))))
	}
}

test_violation_config_region_missing if {
	every type in ["aws_config_configuration_recorder", "aws_config_delivery_channel", "aws_config_configuration_recorder_status", "aws_config_retention_configuration"] {
		fixture.has("[PLAN-ACB-01]", denies(without(ok, cfg(type, "us-east-1"))))
	}
}

test_violation_config_without_recorder_in_instance if {
	partial := [rc | some rc in config("sandbox", "ap-northeast-1", true); rc.type == "aws_config_delivery_channel"]
	fixture.has("[PLAN-ACB-01]", denies(array.concat(ok, partial)))
}

test_violation_region_unknown if {
	every a in [addr("aws_ebs_encryption_by_default", "this", "us-east-1"), cfg("aws_config_delivery_channel", "us-east-1")] {
		fixture.has("[PLAN-ACB-01]", denies([unknown_of(rc, a, "region") | some rc in ok]))
	}
}

test_violation_region_absent if {
	a := addr("aws_accessanalyzer_analyzer", "this", "us-east-1")
	rcs := [r | some rc in ok; rc.address == a; r := set_key(rc, "change", set_key(rc.change, "after", object.remove(rc.change.after, ["region"])))]
	fixture.has("[PLAN-ACB-01]", denies(array.concat(without(ok, a), rcs)))
}

test_violation_unreadable_instance if {
	fixture.has("[PLAN-ACB-01]", denies(array.concat(ok, [change("aws_accessanalyzer_analyzer", "aws_accessanalyzer_analyzer.loose", {"region": "us-east-1", "type": "ACCOUNT"})])))
}

test_violation_unsafe_regional_values if {
	every bad in [
		[addr("aws_ebs_encryption_by_default", "this", "us-east-1"), {"enabled": false}],
		[addr("aws_ebs_encryption_by_default", "this", "us-east-1"), {"enabled": null}],
		[addr("aws_accessanalyzer_analyzer", "this", "us-east-1"), {"type": "ORGANIZATION"}],
		[addr("aws_emr_block_public_access_configuration", "this", "us-east-1"), {"block_public_security_group_rules": false}],
		[addr("aws_emr_block_public_access_configuration", "this", "us-east-1"), {"permitted_public_security_group_rule_range": [{"min_range": 22, "max_range": 443}]}],
		[addr("aws_emr_block_public_access_configuration", "this", "us-east-1"), {"permitted_public_security_group_rule_range": [{"min_range": 23, "max_range": 23}]}],
		[addr("aws_emr_block_public_access_configuration", "this", "us-east-1"), {"permitted_public_security_group_rule_range": [{"min_range": 22, "max_range": 22}, {"min_range": 80, "max_range": 80}]}],
		[addr("aws_emr_block_public_access_configuration", "this", "us-east-1"), {"permitted_public_security_group_rule_range": null}],
		[addr("aws_ssm_service_setting", "document_public_sharing", "us-east-1"), {"setting_value": "Enable"}],
		[addr("aws_ssm_service_setting", "automation_log_destination", "us-east-1"), {"setting_value": "S3"}],
		[addr("aws_vpc_block_public_access_options", "this", "us-east-1"), {"internet_gateway_block_mode": "block-ingress"}],
	] {
		only("[PLAN-ACB-02]", denies(patch_all(ok, bad[0], bad[1])))
	}
}

test_violation_unknown_ssm_setting if {
	extra := change("aws_ssm_service_setting", addr("aws_ssm_service_setting", "other", "us-east-1"), {"region": "us-east-1", "setting_id": "/ssm/parameter-store/high-throughput-enabled", "setting_value": "true"})
	fixture.has("[PLAN-ACB-02]", denies(array.concat(ok, [extra])))
}

test_violation_recorder_disabled if {
	only("[PLAN-ACB-02]", denies(patch_all(ok, cfg("aws_config_configuration_recorder_status", "us-east-1"), {"is_enabled": false})))
}

test_violation_recorder_role_not_config_slr if {
	every role in ["arn:aws:iam::123456789012:role/config-recorder", "arn:aws:iam::12345678901:role/aws-service-role/config.amazonaws.com/AWSServiceRoleForConfig", "arn:aws:iam::123456789012:role/aws-service-role/config.amazonaws.com/AWSServiceRoleForConfigX"] {
		only("[PLAN-ACB-02]", denies(patch_all(ok, cfg("aws_config_configuration_recorder", "us-east-1"), {"role_arn": role})))
	}
}

test_pass_recorder_role_in_other_partition if {
	count(denies(patch_all(ok, cfg("aws_config_configuration_recorder", "us-east-1"), {"role_arn": "arn:aws-us-gov:iam::123456789012:role/aws-service-role/config.amazonaws.com/AWSServiceRoleForConfig"}))) == 0
}

test_violation_recorder_role_unknown if {
	a := cfg("aws_config_configuration_recorder", "us-east-1")
	only("[PLAN-ACB-02]", denies([unknown_of(rc, a, "role_arn") | some rc in patch_all(ok, a, {"role_arn": null})]))
}

test_violation_global_resources_twice if {
	only("[PLAN-ACB-03]", denies(patch_all(ok, cfg("aws_config_configuration_recorder", "us-east-1"), {"recording_group": [{"all_supported": true, "include_global_resource_types": true}]})))
}

test_violation_global_resources_nowhere if {
	only("[PLAN-ACB-03]", denies(patch_all(ok, cfg("aws_config_configuration_recorder", "ap-northeast-1"), {"recording_group": [{"all_supported": true, "include_global_resource_types": false}]})))
	only("[PLAN-ACB-03]", denies(patch_all(ok, cfg("aws_config_configuration_recorder", "ap-northeast-1"), {"recording_group": []})))
}

test_violation_public_access_block_off if {
	every key in ["block_public_acls", "block_public_policy", "ignore_public_acls", "restrict_public_buckets"] {
		only("[PLAN-ACB-04]", denies(patch_all(ok, "module.dev.module.account_controls.aws_s3_account_public_access_block.this", {key: false})))
	}
}

test_violation_password_policy_owners_not_declared if {
	every bad in [null, {}, {"singletons": {}}, {"singletons": {"aws_iam_account_password_policy": []}}, {"singletons": {"aws_iam_account_password_policy": "account-baseline"}}, {"singletons": {"aws_iam_account_password_policy": [""]}}] {
		msgs := p.deny with input as array.concat(ab(ok), org_baseline) with data.layer_decl as bad with data.policy_plan.account_baseline as decl
		only("[PLAN-ACB-00]", msgs)
	}
}

# 持ち主は層の宣言から読む。宣言に足した持ち主の password policy が入力に無ければ落とす。
test_violation_password_policy_of_layer_declared_owner_missing if {
	three := {"singletons": {"aws_iam_account_password_policy": ["account-baseline", "organization-baseline", "finops"]}}
	msgs := p.deny with input as array.concat(ab(ok), org_baseline) with data.layer_decl as three with data.policy_plan.account_baseline as decl
	only("[PLAN-ACB-06]", msgs)
	some m in msgs
	contains(m, "finops")
}

# 持ち主の外の password policy も値を比べる。持ち主の外に置くこと自体は層の検査（LY401）が落とす。
test_violation_password_policy_in_third_usecase_differs if {
	other := plan_of("tmp/tf-policy/finops.json", [password_policy("finops", 8)])
	msgs := p.deny with input as array.concat(array.concat(ab(ok), org_baseline), other) with data.layer_decl as layer_decl with data.policy_plan.account_baseline as decl
	only("[PLAN-ACB-06]", msgs)
}

test_violation_password_policy_values_differ if {
	only("[PLAN-ACB-06]", denies(patch_all(ok, "module.dev.module.account_controls.aws_iam_account_password_policy.this", {"minimum_password_length": 12})))
}

test_violation_password_policy_without_counterpart if {
	msgs := p.deny with input as ab(ok) with data.layer_decl as layer_decl with data.policy_plan.account_baseline as decl
	only("[PLAN-ACB-06]", msgs)
}

test_violation_password_policy_keys_not_attributes if {
	d := set_key(decl, "password_policy_keys", ["minimum_length_of_password"])
	msgs := p.deny with input as array.concat(ab(ok), org_baseline) with data.layer_decl as layer_decl with data.policy_plan.account_baseline as d
	only("[PLAN-ACB-06]", msgs)
}

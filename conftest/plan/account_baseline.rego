# account-baseline の安全側の設定の形（account-baseline/ADR-0101・0102、ADR-0402 決定1・2）。リージョンごとの設定が宣言の
# リージョンの集合のすべてに在り、値が安全側で、アカウントに1つの設定を account-baseline の外が持たないこと。
package plan.account_baseline

import data.plan.lib

default decl := null

decl := data.policy_plan.account_baseline

well_formed_decl if {
	is_array(decl.regions)
	count(decl.regions) > 0
	is_object(decl.regional)
	is_object(decl.config)
	is_object(decl.owners)
	is_string(decl.public_access_block)
}

regions := {r | some r in decl.regions}

regional_types := [t | some t in decl.regional]

config_types := [t | some t in decl.config]

# 作るか変える変更だけを見る。消す変更は after を持たない。
changes_of(type) := [c | some c in lib.changes; c.rc.type == type; is_object(c.rc.change.after)]

after(c, key) := object.get(c.rc.change.after, key, null)

unknown(c, key) if lib.unknown_value(object.get(c.rc.change.after_unknown, key, false))

# usecase の instance（reference の root の module）。
instance_of(address) := regex.find_n(`^module\.[^.\[]+(\[[^\]]*\])?`, address, 1)[0]

regions_of(path, instance, type) := {r |
	some c in changes_of(type)
	c.path == path
	instance_of(c.rc.address) == instance
	r := after(c, "region")
}

instances contains {"path": c.path, "instance": instance_of(c.rc.address)} if {
	some type in regional_types
	some c in changes_of(type)
}

config_instances contains {"path": c.path, "instance": instance_of(c.rc.address)} if {
	some c in changes_of(decl.config.recorder)
}

deny contains msg if {
	not well_formed_decl
	msg := "[PLAN-ABL-00] 宣言 account_baseline の regions・regional・config・owners・public_access_block が無いか、形が違います"
}

deny contains msg if {
	well_formed_decl
	some type in array.concat(regional_types, [decl.public_access_block, decl.config.recorder])
	count(changes_of(type)) == 0
	msg := sprintf("[PLAN-ABL-00] 入力に %s の変更がありません。見る対象が0件では通しません", [type])
}

deny contains msg if {
	some type in array.concat(regional_types, config_types)
	some c in changes_of(type)
	not regex.match(`^module\.[^.\[]+(\[[^\]]*\])?`, c.rc.address)
	msg := sprintf("[PLAN-ABL-01] %s: %s の address から usecase の instance を読めません", [c.path, c.rc.address])
}

deny contains msg if {
	some type in array.concat(regional_types, config_types)
	some c in changes_of(type)
	unknown(c, "region")
	msg := sprintf("[PLAN-ABL-01] %s: %s の region が plan で決まりません", [c.path, c.rc.address])
}

deny contains msg if {
	some i in instances
	some type in regional_types
	got := regions_of(i.path, i.instance, type)
	got != regions
	msg := sprintf("[PLAN-ABL-01] %s: %s の %s が、宣言のリージョンの集合に過不足なく在りません（足りない %v、余る %v）", [i.path, i.instance, type, regions - got, got - regions])
}

deny contains msg if {
	some i in config_instances
	some type in config_types
	got := regions_of(i.path, i.instance, type)
	got != regions
	msg := sprintf("[PLAN-ABL-01] %s: %s の %s が、宣言のリージョンの集合に過不足なく在りません（足りない %v、余る %v）", [i.path, i.instance, type, regions - got, got - regions])
}

safe(c, "ebs_encryption") if after(c, "enabled") == true

safe(c, "analyzer") if after(c, "type") == "ACCOUNT"

safe(c, "emr_block_public_access") if {
	after(c, "block_public_security_group_rules") == true
	ranges := after(c, "permitted_public_security_group_rule_range")
	count(ranges) == 1
	ranges[0].min_range == 22
	ranges[0].max_range == 22
}

safe(c, "vpc_block_public_access") if after(c, "internet_gateway_block_mode") == "block-bidirectional"

safe(c, "ssm_service_setting") if {
	expected := {
		"/ssm/documents/console/public-sharing-permission": "Disable",
		"/ssm/automation/customer-script-log-destination": "CloudWatch",
	}
	expected[after(c, "setting_id")] == after(c, "setting_value")
}

deny contains msg if {
	some name, type in decl.regional
	some c in changes_of(type)
	not safe(c, name)
	msg := sprintf("[PLAN-ABL-02] %s: %s の値が安全側の形ではありません", [c.path, c.rc.address])
}

deny contains msg if {
	some i in instances
	settings := {after(c, "setting_id") | some c in changes_of(decl.regional.ssm_service_setting); c.path == i.path; instance_of(c.rc.address) == i.instance}
	needed := {"/ssm/documents/console/public-sharing-permission", "/ssm/automation/customer-script-log-destination"}
	missing := needed - settings
	count(missing) > 0
	msg := sprintf("[PLAN-ABL-02] %s: %s が SSM の設定 %v を持ちません", [i.path, i.instance, missing])
}

deny contains msg if {
	some c in changes_of(decl.config.status)
	after(c, "is_enabled") != true
	msg := sprintf("[PLAN-ABL-02] %s: %s が recorder を有効にしていません", [c.path, c.rc.address])
}

deny contains msg if {
	some c in changes_of(decl.config.recorder)
	not unknown(c, "role_arn")
	not regex.match(`^arn:aws[a-z-]*:iam::[0-9]{12}:role/aws-service-role/config\.amazonaws\.com/AWSServiceRoleForConfig$`, after(c, "role_arn"))
	msg := sprintf("[PLAN-ABL-02] %s: %s の role_arn が AWS Config の service-linked role ではありません（Config.1）", [c.path, c.rc.address])
}

deny contains msg if {
	some c in changes_of(decl.config.recorder)
	unknown(c, "role_arn")
	msg := sprintf("[PLAN-ABL-02] %s: %s の role_arn が plan で決まりません", [c.path, c.rc.address])
}

global_regions(path, instance) := [r |
	some c in changes_of(decl.config.recorder)
	c.path == path
	instance_of(c.rc.address) == instance
	some g in after(c, "recording_group")
	g.include_global_resource_types == true
	r := after(c, "region")
]

deny contains msg if {
	some i in config_instances
	count(global_regions(i.path, i.instance)) != 1
	msg := sprintf("[PLAN-ABL-03] %s: %s でグローバルな resource を記録する recorder が、ちょうど1つのリージョンではありません（%v）", [i.path, i.instance, global_regions(i.path, i.instance)])
}

deny contains msg if {
	some c in changes_of(decl.public_access_block)
	some key in ["block_public_acls", "block_public_policy", "ignore_public_acls", "restrict_public_buckets"]
	after(c, key) != true
	msg := sprintf("[PLAN-ABL-04] %s: %s の %s が true ではありません（S3.1）", [c.path, c.rc.address, key])
}

usecase_of(path) := regex.replace(path, `^(.*/)?([^/]+)\.json$`, "$2")

deny contains msg if {
	some type, owners in decl.owners
	some c in changes_of(type)
	not usecase_of(c.path) in owners
	msg := sprintf("[PLAN-ABL-05] %s: %s はアカウントに1つしか置けない型で、%v の外が持っています", [c.path, c.rc.address, owners])
}

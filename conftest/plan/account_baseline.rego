# account-baseline の安全側の設定の形（account-baseline/ADR-0101・0102、ADR-0402 決定1・2）。リージョンごとの設定が宣言の
# リージョンの集合のすべてに在り、値が安全側で、アカウントに1つの設定を account-baseline の外が持たないこと。
package plan.account_baseline

import data.plan.lib

default decl := null

decl := data.policy_plan.account_baseline

regional_keys := {"ebs_encryption", "analyzer", "emr_block_public_access", "ssm_service_setting", "vpc_block_public_access"}

config_keys := {"recorder", "delivery", "status", "retention"}

strings_of(obj) if {
	is_object(obj)
	every v in obj {
		is_string(v)
	}
}

well_formed_decl if {
	is_array(decl.regions)
	count(decl.regions) > 0
	every r in decl.regions {
		is_string(r)
	}
	strings_of(decl.regional)
	{k | some k, _ in decl.regional} == regional_keys
	strings_of(decl.config)
	{k | some k, _ in decl.config} == config_keys
	is_string(decl.public_access_block)
	is_object(decl.owners)
	count(decl.owners) > 0
	every owners in decl.owners {
		is_array(owners)
		count(owners) > 0
		every o in owners {
			is_string(o)
			o != ""
		}
	}
	is_array(decl.owners.aws_iam_account_password_policy)
	is_array(decl.password_policy_keys)
	count(decl.password_policy_keys) > 0
	every k in decl.password_policy_keys {
		is_string(k)
		k != ""
	}
	count({k | some k in decl.password_policy_keys}) == count(decl.password_policy_keys)
}

regions := {r | some r in decl.regions}

regional_types := [t | some t in decl.regional]

config_types := [t | some t in decl.config]

usecase_of(path) := regex.replace(path, `^(.*/)?([^/]+)\.json$`, "$2")

# 作るか変える変更だけを見る。消す変更は after を持たない。
changes_of(type) := [c | some c in lib.changes; c.rc.type == type; is_object(c.rc.change.after)]

# 形の検査は account-baseline の reference の plan に掛ける。同じ型をほかの usecase が持つことは、owners に載せた型について 05 が見る。
# analyzer（security-tooling の組織の analyzer と同じ型）と service-linked role（ほかの usecase も持つ型）は owners に載せない。
own_changes_of(type) := [c | some c in changes_of(type); usecase_of(c.path) == "account-baseline"]

after(c, key) := object.get(c.rc.change.after, key, null)

unknown(c, key) if lib.unknown_value(object.get(c.rc.change.after_unknown, key, false))

# usecase の instance（reference の root の module）。
instance_of(address) := regex.find_n(`^module\.[^.\[]+(\[[^\]]*\])?`, address, 1)[0]

regions_of(path, instance, type) := {r |
	some c in own_changes_of(type)
	c.path == path
	instance_of(c.rc.address) == instance
	r := after(c, "region")
}

instances contains {"path": c.path, "instance": instance_of(c.rc.address)} if {
	some type in regional_types
	some c in own_changes_of(type)
}

config_instances contains {"path": c.path, "instance": instance_of(c.rc.address)} if {
	some type in config_types
	some c in own_changes_of(type)
}

deny contains msg if {
	not well_formed_decl
	msg := "[PLAN-ACB-00] 宣言 account_baseline の regions・regional・config・public_access_block・owners・password_policy_keys が無いか、形が違います"
}

deny contains msg if {
	well_formed_decl
	some type in array.concat(regional_types, [decl.public_access_block, decl.config.recorder])
	count(own_changes_of(type)) == 0
	msg := sprintf("[PLAN-ACB-00] 入力に account-baseline の %s の変更がありません。見る対象が0件では通しません", [type])
}

deny contains msg if {
	some type in array.concat(regional_types, config_types)
	some c in own_changes_of(type)
	not regex.match(`^module\.[^.\[]+(\[[^\]]*\])?`, c.rc.address)
	msg := sprintf("[PLAN-ACB-01] %s: %s の address から usecase の instance を読めません", [c.path, c.rc.address])
}

deny contains msg if {
	some type in array.concat(regional_types, config_types)
	some c in own_changes_of(type)
	unknown(c, "region")
	msg := sprintf("[PLAN-ACB-01] %s: %s の region が plan で決まりません", [c.path, c.rc.address])
}

deny contains msg if {
	some i in instances
	some type in regional_types
	type != decl.regional.ssm_service_setting
	got := regions_of(i.path, i.instance, type)
	got != regions
	msg := sprintf("[PLAN-ACB-01] %s: %s の %s が、宣言のリージョンの集合に過不足なく在りません（足りない %v、余る %v）", [i.path, i.instance, type, regions - got, got - regions])
}

ssm_settings := {
	"/ssm/documents/console/public-sharing-permission": "Disable",
	"/ssm/automation/customer-script-log-destination": "CloudWatch",
}

ssm_regions(path, instance, id) := {r |
	some c in own_changes_of(decl.regional.ssm_service_setting)
	c.path == path
	instance_of(c.rc.address) == instance
	after(c, "setting_id") == id
	r := after(c, "region")
}

deny contains msg if {
	some i in instances
	some id, _ in ssm_settings
	got := ssm_regions(i.path, i.instance, id)
	got != regions
	msg := sprintf("[PLAN-ACB-01] %s: %s の SSM の設定 %s が、宣言のリージョンの集合に過不足なく在りません（足りない %v、余る %v）", [i.path, i.instance, id, regions - got, got - regions])
}

deny contains msg if {
	some i in config_instances
	some type in config_types
	got := regions_of(i.path, i.instance, type)
	got != regions
	msg := sprintf("[PLAN-ACB-01] %s: %s の %s が、宣言のリージョンの集合に過不足なく在りません（足りない %v、余る %v）", [i.path, i.instance, type, regions - got, got - regions])
}

safe(c, "ebs_encryption") if after(c, "enabled") == true

safe(c, "analyzer") if after(c, "type") == "ACCOUNT"

safe(c, "emr_block_public_access") if {
	after(c, "block_public_security_group_rules") == true
	ranges := after(c, "permitted_public_security_group_rule_range")
	is_array(ranges)
	count(ranges) == 1
	ranges[0].min_range == 22
	ranges[0].max_range == 22
}

safe(c, "vpc_block_public_access") if after(c, "internet_gateway_block_mode") == "block-bidirectional"

safe(c, "ssm_service_setting") if ssm_settings[after(c, "setting_id")] == after(c, "setting_value")

deny contains msg if {
	some name, type in decl.regional
	some c in own_changes_of(type)
	not safe(c, name)
	msg := sprintf("[PLAN-ACB-02] %s: %s の値が安全側の形ではありません", [c.path, c.rc.address])
}

deny contains msg if {
	some c in own_changes_of(decl.config.status)
	after(c, "is_enabled") != true
	msg := sprintf("[PLAN-ACB-02] %s: %s が recorder を有効にしていません", [c.path, c.rc.address])
}

deny contains msg if {
	some c in own_changes_of(decl.config.recorder)
	not unknown(c, "role_arn")
	not regex.match(`^arn:aws[a-z-]*:iam::[0-9]{12}:role/aws-service-role/config\.amazonaws\.com/AWSServiceRoleForConfig$`, after(c, "role_arn"))
	msg := sprintf("[PLAN-ACB-02] %s: %s の role_arn が AWS Config の service-linked role ではありません（Config.1）", [c.path, c.rc.address])
}

deny contains msg if {
	some c in own_changes_of(decl.config.recorder)
	unknown(c, "role_arn")
	msg := sprintf("[PLAN-ACB-02] %s: %s の role_arn が plan で決まりません", [c.path, c.rc.address])
}

global_regions(path, instance) := [r |
	some c in own_changes_of(decl.config.recorder)
	c.path == path
	instance_of(c.rc.address) == instance
	some g in after(c, "recording_group")
	g.include_global_resource_types == true
	r := after(c, "region")
]

deny contains msg if {
	some i in config_instances
	count(global_regions(i.path, i.instance)) != 1
	msg := sprintf("[PLAN-ACB-03] %s: %s でグローバルな resource を記録する recorder が、ちょうど1つのリージョンではありません（%v）", [i.path, i.instance, global_regions(i.path, i.instance)])
}

deny contains msg if {
	some c in own_changes_of(decl.public_access_block)
	some key in ["block_public_acls", "block_public_policy", "ignore_public_acls", "restrict_public_buckets"]
	after(c, key) != true
	msg := sprintf("[PLAN-ACB-04] %s: %s の %s が true ではありません（S3.1）", [c.path, c.rc.address, key])
}

deny contains msg if {
	some type, owners in decl.owners
	some c in changes_of(type)
	not usecase_of(c.path) in owners
	msg := sprintf("[PLAN-ACB-05] %s: %s はアカウントに1つしか置けない型で、%v の外が持っています", [c.path, c.rc.address, owners])
}

# 管理アカウントは organization-baseline、メンバーは account-baseline が password policy を持つ。値はどちらも同じにする。
password_policies := [{"at": sprintf("%s: %s", [c.path, c.rc.address]), "values": {k: after(c, k) | some k in decl.password_policy_keys}} |
	some c in changes_of("aws_iam_account_password_policy")
]

deny contains msg if {
	some a in password_policies
	some b in password_policies
	a.values != b.values
	msg := sprintf("[PLAN-ACB-06] %s と %s の password policy の値が違います", [a.at, b.at])
}

# 宣言の属性名が provider に無いと、どの値も null になり、食い違いを比べられない。
deny contains msg if {
	some a in password_policies
	every _, v in a.values {
		v == null
	}
	msg := sprintf("[PLAN-ACB-06] %s の password policy の値が、宣言の password_policy_keys の属性で1つも読めません", [a.at])
}

# 比べる相手が無いと、比較が行われないまま通る。password policy を持つと宣言した usecase は、それぞれ1つ以上持つ。
deny contains msg if {
	well_formed_decl
	some owner in decl.owners.aws_iam_account_password_policy
	count([c | some c in changes_of("aws_iam_account_password_policy"); usecase_of(c.path) == owner]) == 0
	msg := sprintf("[PLAN-ACB-06] 入力に %s の password policy がありません。値を比べる相手が無いままでは通しません", [owner])
}

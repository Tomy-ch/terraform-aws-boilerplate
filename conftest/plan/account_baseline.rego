# account-baseline の安全側の設定の形（account-baseline/ADR-0101・0102、ADR-0402 決定1・2）。リージョンごとの設定が宣言の
# リージョンの集合のすべてに在り、値が安全側で、password policy の値が持ち主の間で同じであること。アカウントかリージョンに
# 1つの型を持ち主の外が宣言しないことは、層の検査（conftest/hcl/layer の LY401）が持つ。
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
	is_array(decl.password_policy_keys)
	count(decl.password_policy_keys) > 0
	every k in decl.password_policy_keys {
		is_string(k)
		k != ""
	}
	count({k | some k in decl.password_policy_keys}) == count(decl.password_policy_keys)
}

# password policy の持ち主は、層の宣言（data.layer_decl。conftest/hcl/layer/decl.json）の singletons から導く。
default password_policy_owners := null

password_policy_owners := data.layer_decl.singletons.aws_iam_account_password_policy

well_formed_owners if {
	is_array(password_policy_owners)
	count(password_policy_owners) > 0
	every o in password_policy_owners {
		is_string(o)
		o != ""
	}
}

regions := {r | some r in decl.regions}

regional_types := [t | some t in decl.regional]

config_types := [t | some t in decl.config]

usecase_of(path) := regex.replace(path, `^(.*/)?([^/]+)\.json$`, "$2")

# 作るか変える変更だけを見る。消す変更は after を持たない。
changes_of(type) := [c | some c in lib.changes; c.rc.type == type; is_object(c.rc.change.after)]

# 形の検査は account-baseline の reference の plan に掛ける。
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
	msg := "[PLAN-ACB-00] 宣言 account_baseline の regions・regional・config・public_access_block・password_policy_keys が無いか、形が違います"
}

deny contains msg if {
	not well_formed_owners
	msg := "[PLAN-ACB-00] 層の宣言 data.layer_decl.singletons.aws_iam_account_password_policy（conftest/hcl/layer/decl.json）が無いか、空でない文字列の空でない配列ではありません"
}

# アカウントかリージョンに1つの型を持ち主の外が宣言しないことは LY401 が見る。宣言の型が singletons に持ち主
# account-baseline として無ければ、LY401 はその型を見ない。analyzer は名前で区別する resource、recorder の status は
# attachment 型（recorder の持ち主に LY301 で縛られる）なので除く。
singleton_types := {decl.regional[k] | some k in regional_keys - {"analyzer"}} | {decl.config[k] | some k in config_keys - {"status"}} | {decl.public_access_block}

owned_by_baseline(type) if "account-baseline" in data.layer_decl.singletons[type]

deny contains msg if {
	well_formed_decl
	some type in singleton_types
	not owned_by_baseline(type)
	msg := sprintf("[PLAN-ACB-00] 宣言 account_baseline の %s が、層の宣言 data.layer_decl.singletons（conftest/hcl/layer/decl.json）に持ち主 account-baseline として在りません", [type])
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
	well_formed_owners
	some owner in password_policy_owners
	count([c | some c in changes_of("aws_iam_account_password_policy"); usecase_of(c.path) == owner]) == 0
	msg := sprintf("[PLAN-ACB-06] 入力に %s の password policy がありません。値を比べる相手が無いままでは通しません", [owner])
}

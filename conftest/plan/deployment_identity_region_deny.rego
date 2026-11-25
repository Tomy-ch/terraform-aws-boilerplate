# deployment-identity の CI の role で、ARN の region を `*` にした Allow が、同じ role の inline policy の
# aws:RequestedRegion の Deny で宣言の集合に限られていること（repository-wide/ADR-0209 決定4、ADR-0301 決定11 の但し書き）。
package plan.deployment_identity_region_deny

import data.plan.lib

di := object.get(data.policy_plan, "deployment_identity", {})

wl := object.get(di, "workload", {})

region_list := object.get(object.get(data.policy_plan, "default_regions", {}), "aws", null)

well_formed_regions if {
	is_array(region_list)
	count(region_list) > 0
	every r in region_list {
		is_string(r)
		regex.match(`^[a-z]{2}(-[a-z]+)+-[0-9]+$`, r)
	}
}

declared_regions := {r | some r in region_list} if well_formed_regions

# role ごとの inline policy の address と、付けてよい customer managed policy の address。
roles := {
	"plan": {
		"inline": object.get(object.get(di, "plan_role", {}), "policy", ""),
		"managed": lib.as_array(object.get(object.get(di, "plan_role", {}), "managed_policies", [])),
	},
	"apply": {
		"inline": object.get(object.get(di, "apply_role", {}), "policy", ""),
		"managed": [p | some k in ["policy", "read_policy", "policy_2"]; p := object.get(wl, k, ""); p != ""],
	},
}

region_wildcard(arn) if {
	is_string(arn)
	regex.match(`^arn:aws[a-z-]*:[a-z0-9-]+:\*:`, arn)
}

wildcard_allows contains {"role": role, "path": r.document.path, "address": r.document.address, "action": a} if {
	some role, cfg in roles
	some r in lib.readable
	r.document.attr == "policy"
	r.document.address in array.concat([cfg.inline], cfg.managed)
	some s in lib.statements(r.body)
	s.Effect == "Allow"
	some res in lib.as_array(object.get(s, "Resource", []))
	region_wildcard(res)
	some a in lib.lower_all(object.get(s, "Action", []))
}

# 集合の外を拒む Deny: Resource が "*" だけで、条件が StringNotEquals の aws:RequestedRegion だけのもの。
region_denies contains {"role": role, "path": r.document.path, "actions": lib.lower_all(s.Action), "regions": {v | some v in lib.as_array(s.Condition.StringNotEquals["aws:RequestedRegion"])}} if {
	some role, cfg in roles
	some r in lib.readable
	r.document.attr == "policy"
	r.document.address == cfg.inline
	some s in lib.statements(r.body)
	s.Effect == "Deny"
	"Action" in object.keys(s)
	lib.as_array(object.get(s, "Resource", [])) == ["*"]
	object.keys(object.get(s, "Condition", {})) == {"StringNotEquals"}
	object.keys(s.Condition.StringNotEquals) == {"aws:RequestedRegion"}
}

covered(role, action) if {
	some d in region_denies
	d.role == role
	d.regions == declared_regions
	action in d.actions
}

deny contains msg if {
	not well_formed_regions
	msg := sprintf("[PLAN-REGION-00] data.policy_plan.default_regions.aws が、region の形の文字列の空でない配列ではありません: %v", [region_list])
}

deny contains msg if {
	some x in wildcard_allows
	not covered(x.role, x.action)
	msg := sprintf("[PLAN-REGION-01] %s: %s の Allow が、ARN の region を * にして %s を許していますが、%s の role の inline policy に、それを aws:RequestedRegion が宣言の集合の外のとき拒む Deny がありません（repository-wide/ADR-0209 決定4）", [x.path, x.address, x.action, x.role])
}

deny contains msg if {
	well_formed_regions
	some d in region_denies
	d.regions != declared_regions
	msg := sprintf("[PLAN-REGION-02] %s: %s の role の aws:RequestedRegion の Deny のリージョンが、宣言の集合と違います（足りない %v、余る %v）", [d.path, d.role, sort(declared_regions - d.regions), sort(d.regions - declared_regions)])
}

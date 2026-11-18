# image-registry の push の role の権限（image-registry/ADR-0102、ADR-0301 決定7-9）。Action は語彙の集合と完全一致で見る。
# 拒否の一覧で見ると、AWS が足す Action（タグの付け替えや repository の作成）を見逃す。
package plan.image_registry_publisher

import data.plan.lib

default cfg := null

cfg := data.policy_plan.image_registry

publish_actions := {
	"ecr:batchchecklayeravailability", "ecr:batchgetimage", "ecr:completelayerupload", "ecr:describeimagescanfindings",
	"ecr:describeimages", "ecr:initiatelayerupload", "ecr:putimage", "ecr:uploadlayerpart",
}

login_actions := {"ecr:getauthorizationtoken"}

# 同じ module の instance（module.image_registry.module.repository["api"]）。
instance(address) := regex.replace(address, `\.[a-z0-9_]+\.[a-z0-9_]+$`, "")

declared_keys := ["role", "policy", "policies_exclusive", "attachments_exclusive"]

declared := [a | some k in declared_keys; a := cfg.publisher[k]]

of(path, inst, address) := [c.rc | some c in lib.changes; c.path == path; lib.strip_index(c.rc.address) == address; instance(c.rc.address) == inst]

policies contains r if {
	some r in lib.readable
	r.document.type == "aws_iam_role_policy"
	lib.strip_index(r.document.address) == cfg.publisher.policy
}

allows(body) := [s | some s in lib.statements(body); s.Effect == "Allow"]

repository_name(path, inst) := rc.change.after.name if {
	some rc in of(path, inst, cfg.repository)
	is_string(rc.change.after.name)
}

# 宣言が欠けると、それを読む規則が未定義になって黙る。
deny contains msg if {
	some k in declared_keys
	not is_string(object.get(object.get(object.get(data.policy_plan, "image_registry", {}), "publisher", {}), k, null))
	msg := sprintf("[PLAN-PUB-00] 宣言 data.policy_plan.image_registry.publisher.%s が無いか、文字列ではありません", [k])
}

deny contains msg if {
	some address in declared
	count({c | some c in lib.changes; lib.strip_index(c.rc.address) == address}) == 0
	msg := sprintf("[PLAN-PUB-00] push の role の %s が入力にありません。address が変わったなら data.policy_plan.image_registry を直してください", [address])
}

deny contains msg if {
	some r in policies
	got := {a | some s in allows(r.body); some a in lib.lower_all(s.Action)}
	got != publish_actions | login_actions
	msg := sprintf("[PLAN-PUB-01] %s: %s の Allow の Action %v が、push の語彙 %v と一致しません", [r.document.path, r.document.address, got, publish_actions | login_actions])
}

deny contains msg if {
	some r in policies
	some s in allows(r.body)
	some a in lib.lower_all(s.Action)
	a in publish_actions
	not targets_own_repository(r.document, s)
	msg := sprintf("[PLAN-PUB-02] %s: %s の %s の Resource が、同じ instance の repository の ARN だけではありません", [r.document.path, r.document.address, a])
}

deny contains msg if {
	some r in policies
	some s in allows(r.body)
	"*" in lib.as_array(s.Resource)
	{a | some a in lib.lower_all(s.Action)} != login_actions
	msg := sprintf("[PLAN-PUB-03] %s: %s の Resource \"*\" の文が ecr:GetAuthorizationToken だけではありません", [r.document.path, r.document.address])
}

deny contains msg if {
	some r in policies
	some s in allows(r.body)
	s.NotAction
	msg := sprintf("[PLAN-PUB-03] %s: %s が NotAction で許可しています", [r.document.path, r.document.address])
}

deny contains msg if {
	some c in lib.changes
	lib.strip_index(c.rc.address) == cfg.publisher.role
	inst := instance(c.rc.address)
	not exclusive(c.path, inst, c.rc.change.after.name)
	msg := sprintf("[PLAN-PUB-04] %s: %s の role に、inline の policy を push の policy だけに、管理 policy を空に保つ排他の resource がありません", [c.path, c.rc.address])
}

targets_own_repository(d, s) if {
	name := repository_name(d.path, instance(d.address))
	resources := lib.as_array(s.Resource)
	count(resources) == 1
	regex.match(sprintf(`^arn:aws[a-z-]*:ecr:[a-z0-9-]+:[0-9]{12}:repository/%s$`, [regex.replace(name, `[.+()\[\]{}|^$\\*?]`, `\${0}`)]), resources[0])
}

exclusive(path, inst, role_name) if {
	some p in of(path, inst, cfg.publisher.policy)
	some pe in of(path, inst, cfg.publisher.policies_exclusive)
	pe.change.after.role_name == role_name
	pe.change.after.policy_names == [p.change.after.name]
	some ae in of(path, inst, cfg.publisher.attachments_exclusive)
	ae.change.after.role_name == role_name
	ae.change.after.policy_arns == []
}

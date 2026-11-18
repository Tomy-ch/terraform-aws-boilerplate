# image-registry の repository・lifecycle・repository policy・registry のスキャン設定の形（image-registry/ADR-0202・0203・0301）。
# 不変タグ・repository 単位の scan on push・CMK・public の policy は Security の道具が見るので、ここで重ねない（ADR-0402 決定3）。
package plan.image_registry

import data.plan.lib

default cfg := null

cfg := data.policy_plan.image_registry

write_actions := {"ecr:completelayerupload", "ecr:initiatelayerupload", "ecr:putimage", "ecr:uploadlayerpart"}

declared := [a | some k in ["repository", "lifecycle_policy", "repository_policy", "registry_scanning"]; a := cfg[k]]

instance(address) := regex.replace(address, `\.[a-z0-9_]+\.[a-z0-9_]+$`, "")

managed contains {"path": f.path, "rc": rc} if {
	some f in input
	some rc in f.contents.resource_changes
	object.get(rc, "mode", "managed") == "managed"
	is_object(rc.change.after)
}

repositories contains {"path": m.path, "address": m.rc.address, "name": m.rc.change.after.name, "after": m.rc.change.after} if {
	some m in managed
	m.rc.type == "aws_ecr_repository"
	is_string(m.rc.change.after.name)
}

declared_repositories contains r if {
	some r in repositories
	lib.strip_index(r.address) == cfg.repository
}

scannings contains m if {
	some m in managed
	m.rc.type == "aws_ecr_registry_scanning_configuration"
}

deny contains msg if {
	some address in declared
	count({c | some c in lib.changes; lib.strip_index(c.rc.address) == address}) == 0
	msg := sprintf("[PLAN-ECR-00] %s が入力にありません。address が変わったなら data.policy_plan.image_registry を直してください", [address])
}

# アカウント×リージョンに1つの設定で、2つの書き手は互いに上書きする。
deny contains msg if {
	cfg != null
	some m in scannings
	lib.strip_index(m.rc.address) != cfg.registry_scanning
	msg := sprintf("[PLAN-ECR-01] %s: registry のスキャン設定 %s が、宣言した書き手 %s の外にあります", [m.path, m.rc.address, cfg.registry_scanning])
}

deny contains msg if {
	some m in scannings
	lib.strip_index(m.rc.address) == cfg.registry_scanning
	m.rc.change.after.scan_type != "ENHANCED"
	msg := sprintf("[PLAN-ECR-02] %s: %s の scan_type が ENHANCED ではありません", [m.path, m.rc.address])
}

deny contains msg if {
	some r in declared_repositories
	not continuously_scanned(r)
	msg := sprintf("[PLAN-ECR-03] %s: repository %s が、同じ入力の CONTINUOUS_SCAN の規則のどの filter にも当たりません", [r.path, r.name])
}

deny contains msg if {
	some r in declared_repositories
	n := count([m | some m in managed; m.path == r.path; m.rc.type == "aws_ecr_lifecycle_policy"; m.rc.change.after.repository == r.name])
	n != 1
	msg := sprintf("[PLAN-ECR-04] %s: repository %s の lifecycle policy が %d 件です（1件を期待）", [r.path, r.name, n])
}

deny contains msg if {
	some m in managed
	m.rc.type == "aws_ecr_lifecycle_policy"
	lib.strip_index(m.rc.address) == cfg.lifecycle_policy
	not count_based(m.rc.change.after.policy)
	msg := sprintf("[PLAN-ECR-05] %s: %s の規則が、タグ付きのイメージを2件以上残す imageCountMoreThan の expire の1つではありません", [m.path, m.rc.address])
}

deny contains msg if {
	some r in declared_repositories
	object.get(r.after, "force_delete", null) != false
	msg := sprintf("[PLAN-ECR-06] %s: %s の force_delete が false ではありません", [r.path, r.address])
}

deny contains msg if {
	some p in repository_policies
	some s in lib.statements(p.body)
	s.Effect != "Deny"
	msg := sprintf("[PLAN-ECR-07] %s: %s が Deny 以外の文を持ちます", [p.document.path, p.document.address])
}

deny contains msg if {
	some p in repository_policies
	not denies_foreign_writes(p)
	msg := sprintf("[PLAN-ECR-08] %s: %s が、同じ instance の push の role 以外による書き込みを、アカウント全体へ条件の形を崩さずに拒否していません", [p.document.path, p.document.address])
}

deny contains msg if {
	some p in repository_policies
	not denies_image_deletion(p)
	msg := sprintf("[PLAN-ECR-09] %s: %s が ecr:BatchDeleteImage をアカウント全体へ条件なしで拒否していません", [p.document.path, p.document.address])
}

# repository policy は repository に1つで、後から書いた方が先を置き換える。
deny contains msg if {
	some r in declared_repositories
	n := count([m | some m in managed; m.path == r.path; m.rc.type == "aws_ecr_repository_policy"; m.rc.change.after.repository == r.name])
	n != 1
	msg := sprintf("[PLAN-ECR-10] %s: repository %s の repository policy が %d 件です（1件を期待）", [r.path, r.name, n])
}

repository_policies contains r if {
	some r in lib.readable
	r.document.type == "aws_ecr_repository_policy"
	lib.strip_index(r.document.address) == cfg.repository_policy
}

continuously_scanned(r) if {
	some m in scannings
	m.path == r.path
	some rule in object.get(m.rc.change.after, "rule", [])
	rule.scan_frequency == "CONTINUOUS_SCAN"
	some f in rule.repository_filter
	f.filter_type == "WILDCARD"
	lib.iam_match(f.filter, r.name)
}

count_based(doc) if {
	is_string(doc)
	body := json.unmarshal(doc)
	count(body.rules) == 1
	rule := body.rules[0]
	rule.selection.tagStatus == "tagged"
	rule.selection.countType == "imageCountMoreThan"
	is_number(rule.selection.countNumber)
	rule.selection.countNumber >= 2
	rule.action.type == "expire"
}

own(p) := {"repository": r, "role": role} if {
	inst := instance(p.document.address)
	some r in declared_repositories
	r.path == p.document.path
	instance(r.address) == inst
	some m in managed
	m.path == p.document.path
	m.rc.type == "aws_iam_role"
	instance(m.rc.address) == inst
	is_string(m.rc.change.after.name)
	role := m.rc.change.after.name
}

repository_arn_matches(arn, name) if regex.match(sprintf(`^arn:aws[a-z-]*:ecr:[a-z0-9-]+:([0-9]{12}):repository/%s$`, [regex.replace(name, `[.+()\[\]{}|^$\\*?]`, `\${0}`)]), arn)

account_of(arn) := split(arn, ":")[4]

partition_of(arn) := split(arn, ":")[1]

account_root(s, arn) if s.Principal == {"AWS": sprintf("arn:%s:iam::%s:root", [partition_of(arn), account_of(arn)])}

denies_foreign_writes(p) if {
	o := own(p)
	some s in lib.statements(p.body)
	s.Effect == "Deny"
	is_string(s.Resource)
	repository_arn_matches(s.Resource, o.repository.name)
	account_root(s, s.Resource)
	{a | some a in lib.lower_all(s.Action)} == write_actions
	not s.NotAction
	s.Condition == {"ArnNotEquals": {"aws:PrincipalArn": sprintf("arn:%s:iam::%s:role/%s", [partition_of(s.Resource), account_of(s.Resource), o.role])}}
}

denies_image_deletion(p) if {
	o := own(p)
	some s in lib.statements(p.body)
	s.Effect == "Deny"
	is_string(s.Resource)
	repository_arn_matches(s.Resource, o.repository.name)
	account_root(s, s.Resource)
	"ecr:batchdeleteimage" in lib.lower_all(s.Action)
	not s.NotAction
	not s.Condition
}

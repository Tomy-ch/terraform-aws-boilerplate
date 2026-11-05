# deployment-identity の plan の role の権限（ADR-0602 決定13、deployment-identity/ADR-0201 決定8）。
# plan の role に変更系の権限を与えない。例外は plan の出力の Bucket の pr/* と branch/* への s3:PutObject だけ。
package plan.deployment_identity_plan_role

import data.plan.lib

cfg := object.get(object.get(data.policy_plan, "deployment_identity", {}), "plan_role", {})

# index key 付きの address（["plan"]）で宣言する。同じ resource の apply 系の instance と分けるため。
default target := ""

target := object.get(cfg, "policy", "")

default role := ""

role := object.get(cfg, "role", "")

# 読み取りの Action は、名前の動詞が Get・List・Describe・Head で始まるものとする。
read_verbs := ["get", "list", "describe", "head"]

# 動詞では読み取りに見えないが、変更系でない Action。決定13 は plan の role に state の復号を持たせ、
# deployment-identity/ADR-0201 決定8 は CMK への切り替えで plan の出力の暗号化の鍵の生成を足す。
non_mutating := {"kms:decrypt", "kms:generatedatakey"}

# 入力に Bucket の resource が無いので、plan の出力の Bucket を address で引けない。名前の形で見る。
plan_output_key := `^arn:aws:s3:::[^/*?]+-tfplan-[^/*?]+/(pr|branch)/\*$`

allows contains {"path": r.document.path, "statement": s} if {
	some r in lib.readable
	r.document.type == "aws_iam_role_policy"
	r.document.attr == "policy"
	r.document.address == target
	some s in lib.statements(r.body)
	s.Effect == "Allow"
}

read_only(action) if {
	parts := split(action, ":")
	count(parts) == 2
	some v in read_verbs
	startswith(parts[1], v)
}

read_only(action) if action in non_mutating

after(address) := c.rc.change.after if {
	some c in lib.changes
	c.rc.address == address
}

role_name := after(role).name

policy_name := after(target).name

# plan の role に付く policy の resource。role を名前で指すので、名前が unknown なものはどの role に付くか読めない。
attached contains {"path": c.path, "address": c.rc.address, "type": c.rc.type, "after": c.rc.change.after, "unknown": c.rc.change.after_unknown} if {
	some c in lib.changes
	c.rc.type in {"aws_iam_role_policy", "aws_iam_role_policy_attachment", "aws_iam_role_policies_exclusive", "aws_iam_role_policy_attachments_exclusive"}
}

role_key(a) := "role_name" if endswith(a.type, "_exclusive")

role_key(a) := "role" if not endswith(a.type, "_exclusive")

on_plan_role(a) if object.get(a.after, role_key(a), null) == role_name

exclusive(type) := {a | some a in attached; a.type == type; on_plan_role(a)}

deny contains msg if {
	count({d | some d in lib.documents; d.type == "aws_iam_role_policy"; d.address == target}) == 0
	msg := sprintf("[PLAN-ROLE-00] plan の role の policy %q が入力にありません。address が変わったなら data.policy_plan.deployment_identity.plan_role を直してください", [target])
}

role_known if is_string(role_name)

deny contains msg if {
	not role_known
	msg := sprintf("[PLAN-ROLE-00] plan の role %q の名前が入力にありません。address が変わったなら data.policy_plan.deployment_identity.plan_role を直してください", [role])
}

deny contains msg if {
	some a in allows
	some k in ["NotAction", "NotResource"]
	a.statement[k]
	msg := sprintf("[PLAN-ROLE-01] %s: plan の role の Allow が %s を使っています。許す Action と Resource を列挙してください", [a.path, k])
}

deny contains msg if {
	some a in allows
	some action in lib.lower_all(object.get(a.statement, "Action", []))
	action != "s3:putobject"
	not read_only(action)
	msg := sprintf("[PLAN-ROLE-02] %s: plan の role の Allow に変更系の Action %s があります（ADR-0602 決定13）", [a.path, action])
}

deny contains msg if {
	some a in allows
	"s3:putobject" in lib.lower_all(object.get(a.statement, "Action", []))
	some resource in lib.as_array(object.get(a.statement, "Resource", []))
	not regex.match(plan_output_key, resource)
	msg := sprintf("[PLAN-ROLE-03] %s: plan の role の s3:PutObject の Resource %s が、plan の出力の Bucket の pr/* か branch/* ではありません", [a.path, resource])
}

deny contains msg if {
	some a in allows
	"s3:putobject" in lib.lower_all(object.get(a.statement, "Action", []))
	count(lib.as_array(object.get(a.statement, "Resource", []))) == 0
	msg := sprintf("[PLAN-ROLE-03] %s: plan の role の s3:PutObject に Resource がありません", [a.path])
}

# 宣言した policy の外から plan の role へ権限を足す形。exclusive の2つは、宣言の外で付いた policy を外す。
deny contains msg if {
	some a in attached
	a.type in {"aws_iam_role_policy", "aws_iam_role_policy_attachment"}
	on_plan_role(a)
	a.address != target
	msg := sprintf("[PLAN-ROLE-04] %s: %s が plan の role に権限を足しています。plan の role の権限は %s だけに置いてください", [a.path, a.address, target])
}

deny contains msg if {
	some a in attached
	object.get(a.unknown, role_key(a), false) == true
	msg := sprintf("[PLAN-ROLE-04] %s: %s がどの role に付くかを plan で読めません", [a.path, a.address])
}

deny contains msg if {
	is_string(role_name)
	some type in ["aws_iam_role_policies_exclusive", "aws_iam_role_policy_attachments_exclusive"]
	count(exclusive(type)) != 1
	msg := sprintf("[PLAN-ROLE-04] plan の role %s の %s が %d 件です（1件を期待）", [role_name, type, count(exclusive(type))])
}

deny contains msg if {
	some a in exclusive("aws_iam_role_policies_exclusive")
	object.get(a.after, "policy_names", null) != [policy_name]
	msg := sprintf("[PLAN-ROLE-04] %s: %s の policy_names が %v ではありません", [a.path, a.address, [policy_name]])
}

deny contains msg if {
	some a in exclusive("aws_iam_role_policy_attachments_exclusive")
	object.get(a.after, "policy_arns", null) != []
	msg := sprintf("[PLAN-ROLE-04] %s: %s の policy_arns が空ではありません", [a.path, a.address])
}

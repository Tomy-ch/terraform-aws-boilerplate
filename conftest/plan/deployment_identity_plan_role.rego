# deployment-identity の plan の role の権限（ADR-0602 決定13、deployment-identity/ADR-0201 決定8）。
package plan.deployment_identity_plan_role

import data.plan.lib

cfg := object.get(object.get(data.policy_plan, "deployment_identity", {}), "plan_role", {})

# index key 付きの address（["plan"]）で宣言する。同じ resource の apply 系の instance と分けるため。
default target := ""

target := object.get(cfg, "policy", "")

default role := ""

role := object.get(cfg, "role", "")

# 暗号操作は動詞で読み取りに入らないので落ちる（deployment-identity/ADR-0201 決定3）。
read_verbs := ["get", "list", "describe", "head"]

# ログの event を返す Action は get・list・describe の動詞を持つものもあるので、動詞ではなく許可リストで見る（PLAN-ROLE-05）。
raw_logs_actions := object.get(cfg, "logs_actions", [])

logs_actions := {lower(a) | is_array(raw_logs_actions); some a in raw_logs_actions; is_string(a)}

# 宣言に置いてよい Action の閉じた集合（PLAN-ROLE-08）。ログの event を返さない読み取りだけを置き、足すときは
# Service Authorization Reference と API Reference で応答を確かめる（deployment-identity/ADR-0201「与えないもの」）。
plan_logs_allowed := {
	"logs:describeloggroups",
	"logs:describeresourcepolicies",
	"logs:getdelivery",
	"logs:getdeliverydestination",
	"logs:getdeliverysource",
	"logs:listtagsforresource",
}

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

after(address) := c.rc.change.after if {
	some c in lib.changes
	c.rc.address == address
}

role_name := after(role).name

policy_name := after(target).name

# policy の resource は role を名前で指すので、名前が unknown なものはどの role に付くか読めない。
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

# exclusive の2つは権限を足さず、宣言の外で付いた policy を外すので、ここでは見ない。
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

deny contains msg if {
	some a in allows
	some action in lib.lower_all(object.get(a.statement, "Action", []))
	startswith(action, "logs:")
	not action in logs_actions
	msg := sprintf("[PLAN-ROLE-05] %s: plan の role の Allow の %s が、data.policy_plan.deployment_identity.plan_role.logs_actions にありません（ログの event を読ませない。deployment-identity/ADR-0201）", [a.path, action])
}

deny contains msg if {
	not is_array(raw_logs_actions)
	msg := "[PLAN-ROLE-07] data.policy_plan.deployment_identity.plan_role.logs_actions が配列ではありません"
}

deny contains msg if {
	is_array(raw_logs_actions)
	some a in raw_logs_actions
	not is_string(a)
	msg := sprintf("[PLAN-ROLE-07] data.policy_plan.deployment_identity.plan_role.logs_actions に文字列でない要素 %v があります", [a])
}

deny contains msg if {
	some a in logs_actions
	not regex.match(`^logs:[a-z0-9]+$`, a)
	msg := sprintf("[PLAN-ROLE-07] data.policy_plan.deployment_identity.plan_role.logs_actions の %s は、wildcard を含まない logs: の Action ではありません", [a])
}

deny contains msg if {
	some a in logs_actions
	regex.match(`^logs:[a-z0-9]+$`, a)
	not a in plan_logs_allowed
	msg := sprintf("[PLAN-ROLE-08] data.policy_plan.deployment_identity.plan_role.logs_actions の %s は、plan の role に許す Action の閉じた集合にありません", [a])
}

plan_logs_actions contains action if {
	some a in allows
	some action in lib.lower_all(object.get(a.statement, "Action", []))
	startswith(action, "logs:")
}

deny contains msg if {
	some a in logs_actions
	not a in plan_logs_actions
	msg := sprintf("[PLAN-ROLE-06] data.policy_plan.deployment_identity.plan_role.logs_actions の %s は、plan の role のどの Allow にも使われていません。使わなくなったなら宣言から外してください", [a])
}

# service の名前の側の wildcard（log?:GetLogEvents）は、logs: で始まらないので PLAN-ROLE-05 を通る（PLAN-ROLE-09）。
deny contains msg if {
	some a in allows
	some action in lib.lower_all(object.get(a.statement, "Action", []))
	not regex.match(`^[a-z0-9-]+:`, action)
	msg := sprintf("[PLAN-ROLE-09] %s: plan の role の Allow の Action %s の service の名前が、wildcard を含むか読めません", [a.path, action])
}

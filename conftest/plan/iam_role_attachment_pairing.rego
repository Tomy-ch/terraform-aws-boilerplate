# role の managed policy の付け外しの持ち主は、policy ごとの aws_iam_role_policy_attachment である
# （ADR-0301 決定21）。aws_iam_role_policy_attachments_exclusive は破棄で policy を外さないので、
# 載せる ARN ごとに同じ role への attachment が1つずつ在ることを求める。
package plan.iam_role_attachment_pairing

import data.plan.lib

exclusives contains {"path": c.path, "address": c.rc.address, "change": c.rc.change} if {
	some c in lib.changes
	c.rc.type == "aws_iam_role_policy_attachments_exclusive"
	is_object(c.rc.change.after)
}

attachments contains {"path": c.path, "address": c.rc.address, "change": c.rc.change} if {
	some c in lib.changes
	c.rc.type == "aws_iam_role_policy_attachment"
	is_object(c.rc.change.after)
}

unknown(change, attr) if {
	walk(object.get(object.get(change, "after_unknown", {}), attr, false), [_, v])
	v == true
}

arns(e) := {a | some a in lib.as_array(object.get(e.change.after, "policy_arns", []))}

pairs(e, arn) := {a |
	some a in attachments
	a.path == e.path
	object.get(a.change.after, "role", null) == object.get(e.change.after, "role_name", null)
	object.get(a.change.after, "policy_arn", null) == arn
}

# --- PLAN-ATTACH-00: 宣言した排他の resource が入力に在ること ---

declared := object.get(data.policy_plan, ["iam_role_attachment_pairing", "exclusives"], null)

deny contains msg if {
	not is_array(declared)
	msg := "[PLAN-ATTACH-00] 宣言の policy_plan.iam_role_attachment_pairing.exclusives が配列ではありません"
}

deny contains msg if {
	is_array(declared)
	count(declared) == 0
	msg := "[PLAN-ATTACH-00] 宣言の policy_plan.iam_role_attachment_pairing.exclusives が空です"
}

deny contains msg if {
	is_array(declared)
	some address in declared
	count({e | some e in exclusives; e.address == address; count(arns(e)) > 0}) == 0
	msg := sprintf("[PLAN-ATTACH-00] 宣言した %s が、policy を載せる aws_iam_role_policy_attachments_exclusive として入力にありません。address が変わったなら宣言を直してください", [address])
}

# --- PLAN-ATTACH-01: 排他の resource と attachment の対 ---

deny contains msg if {
	some e in exclusives
	some attr in ["role_name", "policy_arns"]
	unknown(e.change, attr)
	msg := sprintf("[PLAN-ATTACH-01] %s: %s の %s を plan で読めません", [e.path, e.address, attr])
}

deny contains msg if {
	some a in attachments
	some attr in ["role", "policy_arn"]
	unknown(a.change, attr)
	msg := sprintf("[PLAN-ATTACH-01] %s: %s の %s を plan で読めません", [a.path, a.address, attr])
}

deny contains msg if {
	some e in exclusives
	some arn in arns(e)
	n := count(pairs(e, arn))
	n != 1
	msg := sprintf("[PLAN-ATTACH-01] %s: %s が載せる %s を、role %v へ付ける aws_iam_role_policy_attachment が %d 件です（1件を期待）", [e.path, e.address, arn, object.get(e.change.after, "role_name", null), n])
}

covered(a) if {
	some e in exclusives
	e.path == a.path
	object.get(e.change.after, "role_name", null) == object.get(a.change.after, "role", null)
	object.get(a.change.after, "policy_arn", null) in arns(e)
}

deny contains msg if {
	some a in attachments
	not unknown(a.change, "role")
	not unknown(a.change, "policy_arn")
	not covered(a)
	msg := sprintf("[PLAN-ATTACH-01] %s: %s の policy が、同じ role の aws_iam_role_policy_attachments_exclusive の policy_arns に載っていません", [a.path, a.address])
}

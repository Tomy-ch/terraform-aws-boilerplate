# finops の通知の形（finops/ADR-0101、ADR-0402 決定4）。予算を停止機構にせず、予算と異常の通知を同じアカウントの Topic 1つへだけ送る。
package plan.finops

import data.plan.lib

default decl := null

decl := data.policy_plan.finops

well_formed_decl if {
	is_string(decl.budget)
	is_string(decl.budget_action)
	is_string(decl.anomaly_subscription)
}

changes_of(type) := [c | some c in lib.changes; c.rc.type == type]

deny contains msg if {
	not well_formed_decl
	msg := "[PLAN-FINOPS-00] 宣言 finops の budget・budget_action・anomaly_subscription が無いか、文字列ではありません"
}

deny contains msg if {
	well_formed_decl
	some type in [decl.budget, decl.anomaly_subscription]
	count(changes_of(type)) == 0
	msg := sprintf("[PLAN-FINOPS-00] 入力に %s の変更がありません。見る対象が0件では通しません", [type])
}

deny contains msg if {
	some c in changes_of(decl.budget_action)
	msg := sprintf("[PLAN-FINOPS-01] %s: %s を作るか変えています。予算は停止機構に使いません", [c.path, c.rc.address])
}

non_empty(x) if {
	is_array(x)
	count(x) > 0
}

deny contains msg if {
	some c in changes_of(decl.budget)
	some n in object.get(c.rc.change.after, "notification", [])
	non_empty(object.get(n, "subscriber_email_addresses", null))
	msg := sprintf("[PLAN-FINOPS-02] %s: %s の通知が email の宛先を持っています", [c.path, c.rc.address])
}

deny contains msg if {
	some c in changes_of(decl.budget)
	some n in object.get(c.rc.change.after, "notification", [])
	topics := object.get(n, "subscriber_sns_topic_arns", null)
	not one(topics)
	msg := sprintf("[PLAN-FINOPS-02] %s: %s の通知の Topic がちょうど1つではありません", [c.path, c.rc.address])
}

one(x) if {
	is_array(x)
	count(x) == 1
}

deny contains msg if {
	some c in changes_of(decl.anomaly_subscription)
	some s in object.get(c.rc.change.after, "subscriber", [])
	s.type != "SNS"
	msg := sprintf("[PLAN-FINOPS-03] %s: %s の宛先に SNS でないもの（%s）があります", [c.path, c.rc.address, s.type])
}

deny contains msg if {
	some c in changes_of(decl.anomaly_subscription)
	not one(object.get(c.rc.change.after, "subscriber", null))
	msg := sprintf("[PLAN-FINOPS-03] %s: %s の宛先がちょうど1つではありません", [c.path, c.rc.address])
}

deny contains msg if {
	some c in changes_of(decl.anomaly_subscription)
	object.get(c.rc.change.after, "frequency", null) != "IMMEDIATE"
	msg := sprintf("[PLAN-FINOPS-03] %s: %s の頻度が IMMEDIATE ではありません", [c.path, c.rc.address])
}

# finops の予算と通知の形（finops/ADR-0001・0101、ADR-0402 決定1・2）。予算を停止機構にせず、費用を filter で絞らず、
# 予算と異常の通知を Topic 1つへだけ送る。plan で決まらない値は、違反の有無を判定できないので落とす。
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

after(c, key) := object.get(c.rc.change.after, key, null)

unknown(c, key) if lib.unknown_value(object.get(c.rc.change.after_unknown, key, false))

non_empty(x) if {
	is_array(x)
	count(x) > 0
}

one(x) if {
	is_array(x)
	count(x) == 1
}

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

deny contains msg if {
	some c in changes_of(decl.budget)
	not non_empty(after(c, "notification"))
	msg := sprintf("[PLAN-FINOPS-02] %s: %s が通知を持ちません", [c.path, c.rc.address])
}

deny contains msg if {
	some c in changes_of(decl.budget)
	some n in after(c, "notification")
	non_empty(object.get(n, "subscriber_email_addresses", null))
	msg := sprintf("[PLAN-FINOPS-02] %s: %s の通知が email の宛先を持っています", [c.path, c.rc.address])
}

deny contains msg if {
	some c in changes_of(decl.budget)
	some n in after(c, "notification")
	not one(object.get(n, "subscriber_sns_topic_arns", null))
	msg := sprintf("[PLAN-FINOPS-02] %s: %s の通知の Topic がちょうど1つではありません", [c.path, c.rc.address])
}

deny contains msg if {
	some c in changes_of(decl.budget)
	unknown(c, "notification")
	msg := sprintf("[PLAN-FINOPS-02] %s: %s の通知が plan で決まりません", [c.path, c.rc.address])
}

deny contains msg if {
	some c in changes_of(decl.anomaly_subscription)
	not one(after(c, "subscriber"))
	msg := sprintf("[PLAN-FINOPS-03] %s: %s の宛先がちょうど1つではありません", [c.path, c.rc.address])
}

deny contains msg if {
	some c in changes_of(decl.anomaly_subscription)
	some s in after(c, "subscriber")
	object.get(s, "type", null) != "SNS"
	msg := sprintf("[PLAN-FINOPS-03] %s: %s の宛先に SNS でないものがあります", [c.path, c.rc.address])
}

deny contains msg if {
	some c in changes_of(decl.anomaly_subscription)
	not unknown(c, "frequency")
	after(c, "frequency") != "IMMEDIATE"
	msg := sprintf("[PLAN-FINOPS-03] %s: %s の頻度が IMMEDIATE ではありません", [c.path, c.rc.address])
}

deny contains msg if {
	some c in changes_of(decl.anomaly_subscription)
	some key in ["subscriber", "frequency"]
	unknown(c, key)
	msg := sprintf("[PLAN-FINOPS-03] %s: %s の %s が plan で決まりません", [c.path, c.rc.address, key])
}

deny contains msg if {
	some c in changes_of(decl.budget)
	some key in ["cost_filter", "filter_expression"]
	non_empty(after(c, key))
	msg := sprintf("[PLAN-FINOPS-04] %s: %s が %s で費用を絞っています", [c.path, c.rc.address, key])
}

deny contains msg if {
	some c in changes_of(decl.budget)
	some key in ["cost_filter", "filter_expression"]
	unknown(c, key)
	msg := sprintf("[PLAN-FINOPS-04] %s: %s の %s が plan で決まりません", [c.path, c.rc.address, key])
}

# 予算と subscription は、usecase の同じ instance の下の2つの内部 module に1つずつある。
instance_of(address) := regex.replace(address, `\.?module\.[^.]+\.[^.]+\.[^.\[]+(\[[^\]]*\])?$`, "")

topics(path, instance) := {t |
	some c in changes_of(decl.budget)
	c.path == path
	instance_of(c.rc.address) == instance
	some n in after(c, "notification")
	some t in object.get(n, "subscriber_sns_topic_arns", [])
	is_string(t)
} | {s.address |
	some c in changes_of(decl.anomaly_subscription)
	c.path == path
	instance_of(c.rc.address) == instance
	some s in after(c, "subscriber")
	is_string(s.address)
}

parsed(address) if instance_of(address) != address

instances contains {"path": c.path, "instance": instance_of(c.rc.address)} if {
	some type in [decl.budget, decl.anomaly_subscription]
	some c in changes_of(type)
}

deny contains msg if {
	some i in instances
	count(topics(i.path, i.instance)) > 1
	msg := sprintf("[PLAN-FINOPS-05] %s: %s の予算と異常の通知が、別々の Topic（%v）へ送っています", [i.path, i.instance, topics(i.path, i.instance)])
}

deny contains msg if {
	some type in [decl.budget, decl.anomaly_subscription]
	some c in changes_of(type)
	not parsed(c.rc.address)
	msg := sprintf("[PLAN-FINOPS-05] %s: %s の address から usecase の instance を読めません", [c.path, c.rc.address])
}

deny contains msg if {
	some i in instances
	some type in [decl.budget, decl.anomaly_subscription]
	count([c | some c in changes_of(type); c.path == i.path; instance_of(c.rc.address) == i.instance]) != 1
	msg := sprintf("[PLAN-FINOPS-05] %s: %s が %s をちょうど1つ持ちません", [i.path, i.instance, type])
}

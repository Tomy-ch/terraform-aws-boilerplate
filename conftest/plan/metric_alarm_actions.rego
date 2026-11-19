# metric alarm の通知は、同じアカウントの通知の Topic 1つだけへ送る（docs/v1.0-plan/scope.md 第9.3節「アラームの置き場」）。ここで見るのは SNS の Topic 1つであることまでで、同じアカウントであることは alarm を持つ module の precondition が見る。
# 3つの action のどれかが空か別の宛先なら、状態の変化のどれかが通知されないか、Topic の外へ出る。
package plan.metric_alarm_actions

import data.plan.lib

action_attrs := ["alarm_actions", "ok_actions", "insufficient_data_actions"]

alarms contains c if {
	some c in lib.changes
	c.rc.type == "aws_cloudwatch_metric_alarm"
	is_object(c.rc.change.after)
}

unknown_attr(c, attr) if lib.unknown_value(object.get(object.get(c.rc.change, "after_unknown", {}), attr, false))

deny contains msg if {
	some c in alarms
	some attr in action_attrs
	unknown_attr(c, attr)
	msg := sprintf("[PLAN-ALARM-01] %s: %s の %s が plan で決まらないので、宛先を確かめられません", [c.path, c.rc.address, attr])
}

targets(c) := {t |
	some attr in action_attrs
	some t in lib.as_array(object.get(c.rc.change.after, attr, []))
}

deny contains msg if {
	some c in alarms
	every attr in action_attrs { not unknown_attr(c, attr) }
	some attr in action_attrs
	actions := lib.as_array(object.get(c.rc.change.after, attr, []))
	count(actions) != 1
	msg := sprintf("[PLAN-ALARM-01] %s: %s の %s が %d 件です（通知の Topic 1件を期待）", [c.path, c.rc.address, attr, count(actions)])
}

deny contains msg if {
	some c in alarms
	every attr in action_attrs { not unknown_attr(c, attr) }
	count(targets(c)) > 1
	msg := sprintf("[PLAN-ALARM-01] %s: %s の3つの action が同じ宛先を指していません（%v）", [c.path, c.rc.address, targets(c)])
}

deny contains msg if {
	some c in alarms
	some t in targets(c)
	not regex.match(`^arn:aws[a-z-]*:sns:[a-z0-9-]+:[0-9]{12}:[A-Za-z0-9_-]{1,256}$`, t)
	msg := sprintf("[PLAN-ALARM-01] %s: %s の action %v が SNS の Topic の ARN ではありません", [c.path, c.rc.address, t])
}

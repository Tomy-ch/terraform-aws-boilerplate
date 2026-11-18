package plan.metric_alarm_actions_test

import data.plan.fixture
import data.plan.metric_alarm_actions as p

topic := "arn:aws:sns:ap-northeast-1:123456789012:tabp-log-archive-ops-notification"

alarm(after, unknown) := {
	"address": "module.x.aws_cloudwatch_metric_alarm.this",
	"type": "aws_cloudwatch_metric_alarm",
	"change": {"actions": ["create"], "after": after, "after_unknown": unknown},
}

actions(a, o, i) := {"alarm_actions": a, "ok_actions": o, "insufficient_data_actions": i}

ids(rcs) := found if {
	found := {substring(m, 0, 15) | some m in p.deny} with input as fixture.plan(rcs)
}

test_pass_single_topic if {
	count(p.deny) == 0 with input as fixture.plan([alarm(actions([topic], [topic], [topic]), {})])
}

test_pass_no_alarm if {
	count(p.deny) == 0 with input as fixture.plan([fixture.doc("aws_iam_role_policy", "module.x.aws_iam_role_policy.this", "policy", {"Statement": []})])
}

test_violation_action_missing_or_extra if {
	other := "arn:aws:sns:ap-northeast-1:123456789012:other"
	every after in [
		actions([], [topic], [topic]),
		actions([topic], [], [topic]),
		actions([topic], [topic], []),
		{"alarm_actions": [topic], "ok_actions": [topic]},
		actions([topic, other], [topic], [topic]),
		actions([topic], [other], [topic]),
		actions([topic], [topic], ["arn:aws:autoscaling:ap-northeast-1:123456789012:scalingPolicy:x"]),
		actions(["arn:aws:sns:ap-northeast-1:123456789012:t.fifo"], ["arn:aws:sns:ap-northeast-1:123456789012:t.fifo"], ["arn:aws:sns:ap-northeast-1:123456789012:t.fifo"]),
	] {
		"[PLAN-ALARM-01]" in ids([alarm(after, {})])
	}
}

test_violation_action_unknown if {
	"[PLAN-ALARM-01]" in ids([alarm(actions([topic], [topic], []), {"insufficient_data_actions": true})])
}

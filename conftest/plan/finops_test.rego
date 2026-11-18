package plan.finops_test

import data.plan.finops as p
import data.plan.fixture

topic := "arn:aws:sns:ap-northeast-1:123456789012:tabp-sandbox-ops-notification"

notification(type, threshold) := {
	"notification_type": type,
	"comparison_operator": "GREATER_THAN",
	"threshold": threshold,
	"threshold_type": "PERCENTAGE",
	"subscriber_sns_topic_arns": [topic],
	"subscriber_email_addresses": null,
}

change(type, address, after) := {
	"address": address,
	"type": type,
	"change": {"actions": ["create"], "after": after, "after_unknown": {}},
}

budget(notifications) := change("aws_budgets_budget", "module.finops.module.budget.aws_budgets_budget.this", {"notification": notifications})

subscription(after) := change("aws_ce_anomaly_subscription", "module.finops.module.cost_anomaly.aws_ce_anomaly_subscription.this", after)

ok_budget := budget([notification("ACTUAL", 85), notification("ACTUAL", 100), notification("FORECASTED", 100)])

ok_subscription := subscription({"frequency": "IMMEDIATE", "subscriber": [{"type": "SNS", "address": topic}]})

test_pass_notify_through_one_topic if {
	count(p.deny) == 0 with input as fixture.plan([ok_budget, ok_subscription])
}

test_pass_budget_action_being_deleted if {
	gone := {"address": "aws_budgets_budget_action.x", "type": "aws_budgets_budget_action", "change": {"actions": ["delete"], "after": null, "after_unknown": {}}}
	count(p.deny) == 0 with input as fixture.plan([ok_budget, ok_subscription, gone])
}

test_violation_no_declaration if {
	fixture.has("[PLAN-FINOPS-00]", p.deny) with input as fixture.plan([ok_budget, ok_subscription])
		with data.policy_plan.finops as null
}

test_violation_no_budget if {
	fixture.has("[PLAN-FINOPS-00]", p.deny) with input as fixture.plan([ok_subscription])
}

test_violation_no_subscription if {
	fixture.has("[PLAN-FINOPS-00]", p.deny) with input as fixture.plan([ok_budget])
}

test_violation_budget_action if {
	action := change("aws_budgets_budget_action", "aws_budgets_budget_action.stop", {})
	fixture.has("[PLAN-FINOPS-01]", p.deny) with input as fixture.plan([ok_budget, ok_subscription, action])
}

test_violation_email_subscriber if {
	n := object.union(notification("ACTUAL", 100), {"subscriber_email_addresses": ["ops@example.com"]})
	fixture.has("[PLAN-FINOPS-02]", p.deny) with input as fixture.plan([budget([n]), ok_subscription])
}

test_violation_no_topic if {
	n := object.union(notification("ACTUAL", 100), {"subscriber_sns_topic_arns": null, "subscriber_email_addresses": ["ops@example.com"]})
	msgs := p.deny with input as fixture.plan([budget([n]), ok_subscription])
	count([m | some m in msgs; startswith(m, "[PLAN-FINOPS-02]")]) == 2
}

test_violation_two_topics if {
	n := object.union(notification("ACTUAL", 100), {"subscriber_sns_topic_arns": [topic, "arn:aws:sns:ap-northeast-1:123456789012:other"]})
	fixture.has("[PLAN-FINOPS-02]", p.deny) with input as fixture.plan([budget([n]), ok_subscription])
}

test_violation_email_anomaly_subscriber if {
	s := subscription({"frequency": "IMMEDIATE", "subscriber": [{"type": "EMAIL", "address": "ops@example.com"}]})
	fixture.has("[PLAN-FINOPS-03]", p.deny) with input as fixture.plan([ok_budget, s])
}

test_violation_two_anomaly_subscribers if {
	s := subscription({"frequency": "IMMEDIATE", "subscriber": [{"type": "SNS", "address": topic}, {"type": "SNS", "address": topic}]})
	fixture.has("[PLAN-FINOPS-03]", p.deny) with input as fixture.plan([ok_budget, s])
}

test_violation_daily_frequency if {
	s := subscription({"frequency": "DAILY", "subscriber": [{"type": "SNS", "address": topic}]})
	fixture.has("[PLAN-FINOPS-03]", p.deny) with input as fixture.plan([ok_budget, s])
}

test_violation_no_notification if {
	fixture.has("[PLAN-FINOPS-02]", p.deny) with input as fixture.plan([budget([]), ok_subscription])
}

test_violation_unknown_notification if {
	b := object.union(ok_budget, {"change": {"actions": ["create"], "after": {}, "after_unknown": {"notification": true}}})
	fixture.has("[PLAN-FINOPS-02]", p.deny) with input as fixture.plan([b, ok_subscription])
}

test_violation_subscriber_without_type if {
	s := subscription({"frequency": "IMMEDIATE", "subscriber": [{"address": topic}]})
	fixture.has("[PLAN-FINOPS-03]", p.deny) with input as fixture.plan([ok_budget, s])
}

test_violation_unknown_subscriber if {
	s := object.union(ok_subscription, {"change": {"actions": ["create"], "after": {"frequency": "IMMEDIATE"}, "after_unknown": {"subscriber": true}}})
	fixture.has("[PLAN-FINOPS-03]", p.deny) with input as fixture.plan([ok_budget, s])
}

test_violation_cost_filter if {
	b := budget([notification("ACTUAL", 100)])
	filtered := object.union(b, {"change": {"after": {"cost_filter": [{"name": "Service", "values": ["Amazon Elastic Compute Cloud - Compute"]}]}}})
	fixture.has("[PLAN-FINOPS-04]", p.deny) with input as fixture.plan([filtered, ok_subscription])
}

test_violation_unknown_filter_expression if {
	b := object.union(ok_budget, {"change": {"after_unknown": {"filter_expression": true}}})
	fixture.has("[PLAN-FINOPS-04]", p.deny) with input as fixture.plan([b, ok_subscription])
}

test_violation_budget_and_subscription_to_different_topics if {
	s := subscription({"frequency": "IMMEDIATE", "subscriber": [{"type": "SNS", "address": "arn:aws:sns:ap-northeast-1:123456789012:other"}]})
	fixture.has("[PLAN-FINOPS-05]", p.deny) with input as fixture.plan([ok_budget, s])
}

test_pass_each_instance_to_its_own_topic if {
	other := "arn:aws:sns:ap-northeast-1:123456789012:tabp-admin-ops-notification"
	n := object.union(notification("ACTUAL", 100), {"subscriber_sns_topic_arns": [other]})
	b2 := change("aws_budgets_budget", "module.finops_admin.module.budget.aws_budgets_budget.this", {"notification": [n]})
	s2 := change("aws_ce_anomaly_subscription", "module.finops_admin.module.cost_anomaly.aws_ce_anomaly_subscription.this", {"frequency": "IMMEDIATE", "subscriber": [{"type": "SNS", "address": other}]})
	count(p.deny) == 0 with input as fixture.plan([ok_budget, ok_subscription, b2, s2])
}

decl_without(key) := object.remove(data.policy_plan.finops, [key])

test_violation_declaration_without_budget_action if {
	fixture.has("[PLAN-FINOPS-00]", p.deny) with input as fixture.plan([ok_budget, ok_subscription])
		with data.policy_plan.finops as decl_without("budget_action")
}

test_violation_declaration_without_budget if {
	fixture.has("[PLAN-FINOPS-00]", p.deny) with input as fixture.plan([ok_budget, ok_subscription])
		with data.policy_plan.finops as decl_without("budget")
}

test_violation_declaration_without_anomaly_subscription if {
	fixture.has("[PLAN-FINOPS-00]", p.deny) with input as fixture.plan([ok_budget, ok_subscription])
		with data.policy_plan.finops as decl_without("anomaly_subscription")
}

test_violation_declaration_with_non_string_budget_action if {
	fixture.has("[PLAN-FINOPS-00]", p.deny) with input as fixture.plan([ok_budget, ok_subscription])
		with data.policy_plan.finops as object.union(data.policy_plan.finops, {"budget_action": 1})
}

test_pass_empty_lists_as_plan_writes_them if {
	n := object.union(notification("ACTUAL", 100), {"subscriber_email_addresses": []})
	b := budget([n])
	empty := object.union(b, {"change": {"after": {"cost_filter": [], "filter_expression": []}}})
	count(p.deny) == 0 with input as fixture.plan([empty, ok_subscription])
}

test_violation_filter_expression if {
	b := object.union(ok_budget, {"change": {"after": {"filter_expression": [{"dimensions": [{"key": "SERVICE", "values": ["Amazon Elastic Compute Cloud - Compute"]}]}]}}})
	fixture.has("[PLAN-FINOPS-04]", p.deny) with input as fixture.plan([b, ok_subscription])
}

test_violation_unknown_cost_filter if {
	b := object.union(ok_budget, {"change": {"after_unknown": {"cost_filter": true}}})
	fixture.has("[PLAN-FINOPS-04]", p.deny) with input as fixture.plan([b, ok_subscription])
}

test_violation_unknown_frequency if {
	s := subscription({"subscriber": [{"type": "SNS", "address": topic}]})
	unknown_frequency := object.union(s, {"change": {"after_unknown": {"frequency": true}}})
	msgs := p.deny with input as fixture.plan([ok_budget, unknown_frequency])
	some m in msgs
	contains(m, "frequency が plan で決まりません")
	not any_frequency_mismatch(msgs)
}

any_frequency_mismatch(msgs) if {
	some m in msgs
	contains(m, "頻度が IMMEDIATE ではありません")
}

test_violation_unknown_topic_in_notification if {
	b := object.union(ok_budget, {"change": {"after_unknown": {"notification": [{"subscriber_sns_topic_arns": true}]}}})
	fixture.has("[PLAN-FINOPS-02]", p.deny) with input as fixture.plan([b, ok_subscription])
}

test_violation_unknown_address_in_subscriber if {
	s := object.union(ok_subscription, {"change": {"after_unknown": {"subscriber": [{"address": true}]}}})
	fixture.has("[PLAN-FINOPS-03]", p.deny) with input as fixture.plan([ok_budget, s])
}

test_violation_notifications_to_different_topics if {
	other := object.union(notification("FORECASTED", 100), {"subscriber_sns_topic_arns": ["arn:aws:sns:ap-northeast-1:123456789012:other"]})
	fixture.has("[PLAN-FINOPS-05]", p.deny) with input as fixture.plan([budget([notification("ACTUAL", 100), other]), ok_subscription])
}

keyed(type, key, name, after) := change(type, sprintf("module.finops[%q].module.%s.%s.this", [key, name, type]), after)

test_pass_for_each_instances_to_their_own_topics if {
	a := "arn:aws:sns:ap-northeast-1:123456789012:a"
	b := "arn:aws:sns:ap-northeast-1:123456789012:b"
	na := object.union(notification("ACTUAL", 100), {"subscriber_sns_topic_arns": [a]})
	nb := object.union(notification("ACTUAL", 100), {"subscriber_sns_topic_arns": [b]})
	rcs := [
		keyed("aws_budgets_budget", "a.b", "budget", {"notification": [na]}),
		keyed("aws_ce_anomaly_subscription", "a.b", "cost_anomaly", {"frequency": "IMMEDIATE", "subscriber": [{"type": "SNS", "address": a}]}),
		keyed("aws_budgets_budget", "c", "budget", {"notification": [nb]}),
		keyed("aws_ce_anomaly_subscription", "c", "cost_anomaly", {"frequency": "IMMEDIATE", "subscriber": [{"type": "SNS", "address": b}]}),
	]
	count(p.deny) == 0 with input as fixture.plan(rcs)
}

test_violation_for_each_instance_to_different_topics if {
	rcs := [
		keyed("aws_budgets_budget", "a", "budget", {"notification": [notification("ACTUAL", 100)]}),
		keyed("aws_ce_anomaly_subscription", "a", "cost_anomaly", {"frequency": "IMMEDIATE", "subscriber": [{"type": "SNS", "address": "arn:aws:sns:ap-northeast-1:123456789012:other"}]}),
	]
	fixture.has("[PLAN-FINOPS-05]", p.deny) with input as fixture.plan(rcs)
}

test_pass_same_instance_in_different_plans_to_their_own_topics if {
	other := "arn:aws:sns:ap-northeast-1:123456789012:other"
	n := object.union(notification("ACTUAL", 100), {"subscriber_sns_topic_arns": [other]})
	s := subscription({"frequency": "IMMEDIATE", "subscriber": [{"type": "SNS", "address": other}]})
	second := [{"path": "q.json", "contents": {"plan_format_version": "1.2", "resource_changes": [budget([n]), s]}}]
	count(p.deny) == 0 with input as array.concat(fixture.plan([ok_budget, ok_subscription]), second)
}

test_violation_address_outside_usecase_module if {
	b := change("aws_budgets_budget", "aws_budgets_budget.this", {"notification": [notification("ACTUAL", 100)]})
	s := change("aws_ce_anomaly_subscription", "aws_ce_anomaly_subscription.this", {"frequency": "IMMEDIATE", "subscriber": [{"type": "SNS", "address": "arn:aws:sns:ap-northeast-1:123456789012:other"}]})
	fixture.has("[PLAN-FINOPS-05]", p.deny) with input as fixture.plan([b, s])
}

test_violation_instance_without_subscription if {
	b2 := change("aws_budgets_budget", "module.finops_admin.module.budget.aws_budgets_budget.this", {"notification": [notification("ACTUAL", 100)]})
	fixture.has("[PLAN-FINOPS-05]", p.deny) with input as fixture.plan([ok_budget, ok_subscription, b2])
}

test_pass_usecase_as_root if {
	b := change("aws_budgets_budget", "module.budget.aws_budgets_budget.this", {"notification": [notification("ACTUAL", 100)]})
	s := change("aws_ce_anomaly_subscription", "module.cost_anomaly.aws_ce_anomaly_subscription.this", {"frequency": "IMMEDIATE", "subscriber": [{"type": "SNS", "address": topic}]})
	count(p.deny) == 0 with input as fixture.plan([b, s])
}

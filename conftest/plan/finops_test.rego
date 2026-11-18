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

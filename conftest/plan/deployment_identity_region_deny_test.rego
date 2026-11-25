package plan.deployment_identity_region_deny_test

import data.plan.deployment_identity_region_deny as p
import data.plan.fixture

plan_address := "module.x.aws_iam_role_policy.this[\"plan\"]"

apply_address := "module.x.aws_iam_role_policy.this[\"apply\"]"

read_address := "module.x.aws_iam_policy.workload_read"

manage_address := "module.x.aws_iam_policy.workload_2"

di := {
	"plan_role": {"policy": plan_address, "managed_policies": [read_address]},
	"apply_role": {"policy": apply_address},
	"workload": {"policy": "module.x.aws_iam_policy.workload", "read_policy": read_address, "policy_2": manage_address},
}

regions := ["ap-northeast-1", "us-east-1"]

recorder := "arn:aws:config:*:123456789012:configuration-recorder/tabp-admin/*"

read_allow := {"Effect": "Allow", "Action": "config:DescribeConfigurationRecorders", "Resource": recorder}

manage_allow := {"Effect": "Allow", "Action": "config:PutConfigurationRecorder", "Resource": recorder}

region_deny(actions, rs) := {"Sid": "DenyBaselineOutsideRegions", "Effect": "Deny", "Action": actions, "Resource": "*", "Condition": {"StringNotEquals": {"aws:RequestedRegion": rs}}}

world(plan_statements, apply_statements) := [
	fixture.doc("aws_iam_role_policy", plan_address, "policy", {"Statement": plan_statements}),
	fixture.doc("aws_iam_role_policy", apply_address, "policy", {"Statement": apply_statements}),
	fixture.doc("aws_iam_policy", read_address, "policy", {"Statement": [read_allow]}),
	fixture.doc("aws_iam_policy", manage_address, "policy", {"Statement": [manage_allow]}),
]

valid_world := world(
	[region_deny(["config:DescribeConfigurationRecorders"], regions)],
	[region_deny(["config:DescribeConfigurationRecorders", "config:PutConfigurationRecorder"], regions)],
)

messages_for(rcs, d, rs) := msgs if {
	msgs := p.deny with input as fixture.plan(rcs) with data.policy_plan.deployment_identity as d with data.policy_plan.default_regions as {"aws": rs}
}

# --- PLAN-REGION-00 ---

test_pass_regions_declared if {
	count(messages_for(valid_world, di, regions)) == 0
}

test_violation_regions_malformed if {
	every bad in [null, [], "us-east-1", [1], ["global"]] {
		fixture.has("[PLAN-REGION-00]", p.deny) with input as fixture.plan(valid_world) with data.policy_plan.deployment_identity as di with data.policy_plan.default_regions as {"aws": bad}
	}
}

# --- PLAN-REGION-01 ---

test_pass_region_named_arn_needs_no_deny if {
	named := {"Effect": "Allow", "Action": "config:PutConfigurationRecorder", "Resource": "arn:aws:config:us-east-1:123456789012:configuration-recorder/tabp-admin/*"}
	rcs := [
		fixture.doc("aws_iam_role_policy", plan_address, "policy", {"Statement": []}),
		fixture.doc("aws_iam_role_policy", apply_address, "policy", {"Statement": []}),
		fixture.doc("aws_iam_policy", manage_address, "policy", {"Statement": [named]}),
	]
	count(messages_for(rcs, di, regions)) == 0
}

test_violation_apply_deny_missing if {
	msgs := messages_for(world([region_deny(["config:DescribeConfigurationRecorders"], regions)], []), di, regions)
	fixture.has(sprintf("[PLAN-REGION-01] p.json: %s の Allow が、ARN の region を * にして config:putconfigurationrecorder", [manage_address]), msgs)
	fixture.has(sprintf("[PLAN-REGION-01] p.json: %s の Allow が、ARN の region を * にして config:describeconfigurationrecorders", [read_address]), msgs)
}

# 読み取りの policy は plan と apply の両方に付くので、plan の role の Deny も要る。
test_violation_plan_deny_missing if {
	msgs := messages_for(world([], [region_deny(["config:DescribeConfigurationRecorders", "config:PutConfigurationRecorder"], regions)]), di, regions)
	fixture.has(sprintf("[PLAN-REGION-01] p.json: %s の Allow が、ARN の region を * にして config:describeconfigurationrecorders を許していますが、plan の role", [read_address]), msgs)
}

test_violation_deny_lacks_action if {
	msgs := messages_for(world([region_deny(["config:DescribeConfigurationRecorders"], regions)], [region_deny(["config:DescribeConfigurationRecorders"], regions)]), di, regions)
	fixture.has(sprintf("[PLAN-REGION-01] p.json: %s の Allow が、ARN の region を * にして config:putconfigurationrecorder", [manage_address]), msgs)
}

# 条件の形が違う Deny は、集合の外を拒む Deny と数えない。
test_violation_deny_shape_not_counted if {
	every bad in [
		object.union(region_deny(["config:DescribeConfigurationRecorders", "config:PutConfigurationRecorder"], regions), {"Resource": recorder}),
		{"Effect": "Deny", "Action": ["config:DescribeConfigurationRecorders", "config:PutConfigurationRecorder"], "Resource": "*", "Condition": {"StringEquals": {"aws:RequestedRegion": regions}}},
		{"Effect": "Deny", "Action": ["config:DescribeConfigurationRecorders", "config:PutConfigurationRecorder"], "Resource": "*", "Condition": {"StringNotEquals": {"aws:RequestedRegion": regions, "aws:PrincipalAccount": "1"}}},
		{"Effect": "Deny", "NotAction": ["s3:GetObject"], "Resource": "*", "Condition": {"StringNotEquals": {"aws:RequestedRegion": regions}}},
	] {
		msgs := messages_for(world([region_deny(["config:DescribeConfigurationRecorders"], regions)], [bad]), di, regions)
		fixture.has(sprintf("[PLAN-REGION-01] p.json: %s の Allow が", [manage_address]), msgs)
	}
}

# 宣言の集合と違う Deny は、覆いと数えない。
test_violation_deny_with_other_regions_does_not_cover if {
	msgs := messages_for(world([region_deny(["config:DescribeConfigurationRecorders"], regions)], [region_deny(["config:DescribeConfigurationRecorders", "config:PutConfigurationRecorder"], ["us-east-1"])]), di, regions)
	fixture.has(sprintf("[PLAN-REGION-01] p.json: %s の Allow が、ARN の region を * にして config:putconfigurationrecorder", [manage_address]), msgs)
}

# --- PLAN-REGION-02 ---

test_pass_deny_regions_match_in_any_order if {
	count(messages_for(world([region_deny(["config:DescribeConfigurationRecorders"], ["us-east-1", "ap-northeast-1"])], [region_deny(["config:DescribeConfigurationRecorders", "config:PutConfigurationRecorder"], regions)]), di, regions)) == 0
}

test_violation_deny_regions_differ if {
	msgs := messages_for(world([region_deny(["config:DescribeConfigurationRecorders"], ["us-east-1", "eu-west-1"])], [region_deny(["config:DescribeConfigurationRecorders", "config:PutConfigurationRecorder"], regions)]), di, regions)
	fixture.has(sprintf("[PLAN-REGION-02] p.json: plan の role の aws:RequestedRegion の Deny のリージョンが、宣言の集合と違います（足りない %v、余る %v）", [["ap-northeast-1"], ["eu-west-1"]]), msgs)
}

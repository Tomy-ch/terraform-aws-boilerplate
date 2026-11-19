package plan.network_foundation_test

import data.plan.fixture
import data.plan.network_foundation as p

decl := {"network_foundation": {
	"vpc": "module.n.module.vpc.aws_vpc.this",
	"default_security_group": "module.n.module.vpc.aws_default_security_group.this",
	"default_route_table": "module.n.module.vpc.aws_default_route_table.this",
	"flow_log": "module.n.module.vpc.aws_flow_log.this",
	"flow_logs_role": "module.n.module.vpc.aws_iam_role.flow_logs",
	"route_tables": {"address": "module.n.module.subnet_tiers.aws_route_table.this", "count": 2},
	"module_prefix": "module.n.",
}}

rc(type, address, after) := {"address": address, "type": type, "change": {"actions": ["create"], "after": after, "after_unknown": {}}}

trust := {"Version": "2012-10-17", "Statement": [{
	"Sid": "AllowVpcFlowLogsOfThisAccount",
	"Effect": "Allow",
	"Principal": {"Service": "vpc-flow-logs.amazonaws.com"},
	"Action": "sts:AssumeRole",
	"Condition": {
		"StringEquals": {"aws:SourceAccount": "123456789012"},
		"ArnLike": {"aws:SourceArn": "arn:aws:ec2:ap-northeast-1:123456789012:vpc-flow-log/*"},
	},
}]}

base := {
	"vpc": rc("aws_vpc", decl.network_foundation.vpc, {"cidr_block": "10.0.0.0/16"}),
	"sg": rc("aws_default_security_group", decl.network_foundation.default_security_group, {"ingress": [], "egress": []}),
	"drt": rc("aws_default_route_table", decl.network_foundation.default_route_table, {"route": []}),
	"fl": rc("aws_flow_log", decl.network_foundation.flow_log, {"traffic_type": "ALL", "log_destination_type": "cloud-watch-logs"}),
	"role": rc("aws_iam_role", decl.network_foundation.flow_logs_role, {"assume_role_policy": json.marshal(trust)}),
	"rt0": rc("aws_route_table", "module.n.module.subnet_tiers.aws_route_table.this[\"private-apne1-az1\"]", {"route": []}),
	"rt1": rc("aws_route_table", "module.n.module.subnet_tiers.aws_route_table.this[\"isolated-apne1-az1\"]", {"route": []}),
}

input_of(m) := fixture.plan([r | some r in m])

denies(m) := msgs if {
	msgs := p.deny with input as input_of(m) with data.policy_plan as decl
}

# object.union は入れ子の object を合わせるので、キーを除いてから置き換える。
put(m, k, v) := object.union(object.remove(m, [k]), {k: v})

route := {"cidr_block": "0.0.0.0/0", "gateway_id": "igw-0123456789abcdef0"}

test_pass_reference_shape if {
	count(denies(base)) == 0
}

test_pass_other_module_may_hold_egress if {
	count(denies(object.union(base, {"nat": rc("aws_nat_gateway", "module.e.aws_nat_gateway.this", {})}))) == 0
}

test_violation_missing_declaration if {
	msgs := p.deny with input as input_of(base) with data.policy_plan as {}
	fixture.has("[PLAN-NET-00]", msgs)
}

test_violation_declared_resource_absent if {
	fixture.has("[PLAN-NET-00]", denies(object.remove(base, ["role"])))
}

test_violation_route_tables_absent if {
	fixture.has("[PLAN-NET-00]", denies(object.remove(base, ["rt0", "rt1"])))
}

test_violation_default_security_group_has_egress if {
	sg := rc("aws_default_security_group", decl.network_foundation.default_security_group, {"ingress": [], "egress": [{"cidr_blocks": ["0.0.0.0/0"]}]})
	fixture.has("[PLAN-NET-01]", denies(put(base, "sg", sg)))
}

test_violation_default_security_group_rules_omitted if {
	sg := rc("aws_default_security_group", decl.network_foundation.default_security_group, {})
	fixture.has("[PLAN-NET-01]", denies(put(base, "sg", sg)))
}

test_violation_flow_logs_reject_only if {
	fl := rc("aws_flow_log", decl.network_foundation.flow_log, {"traffic_type": "REJECT", "log_destination_type": "cloud-watch-logs"})
	fixture.has("[PLAN-NET-02]", denies(put(base, "fl", fl)))
}

test_violation_flow_logs_to_s3 if {
	fl := rc("aws_flow_log", decl.network_foundation.flow_log, {"traffic_type": "ALL", "log_destination_type": "s3"})
	fixture.has("[PLAN-NET-02]", denies(put(base, "fl", fl)))
}

test_violation_trust_without_source_arn if {
	st := put(trust.Statement[0], "Condition", {"StringEquals": {"aws:SourceAccount": "123456789012"}})
	role := rc("aws_iam_role", decl.network_foundation.flow_logs_role, {"assume_role_policy": json.marshal({"Statement": [st]})})
	fixture.has("[PLAN-NET-03]", denies(put(base, "role", role)))
}

test_violation_trust_with_wide_source_arn if {
	st := put(trust.Statement[0], "Condition", {"StringEquals": {"aws:SourceAccount": "123456789012"}, "ArnLike": {"aws:SourceArn": "*"}})
	role := rc("aws_iam_role", decl.network_foundation.flow_logs_role, {"assume_role_policy": json.marshal({"Statement": [st]})})
	fixture.has("[PLAN-NET-03]", denies(put(base, "role", role)))
}

test_violation_trust_with_second_principal if {
	st := put(trust.Statement[0], "Principal", {"Service": ["vpc-flow-logs.amazonaws.com", "ec2.amazonaws.com"]})
	role := rc("aws_iam_role", decl.network_foundation.flow_logs_role, {"assume_role_policy": json.marshal({"Statement": [st]})})
	fixture.has("[PLAN-NET-03]", denies(put(base, "role", role)))
}

test_violation_route_table_with_default_route if {
	rt := rc("aws_route_table", "module.n.module.subnet_tiers.aws_route_table.this[\"private-apne1-az1\"]", {"route": [route]})
	fixture.has("[PLAN-NET-04]", denies(put(base, "rt0", rt)))
}

test_violation_main_route_table_with_route if {
	drt := rc("aws_default_route_table", decl.network_foundation.default_route_table, {"route": [route]})
	fixture.has("[PLAN-NET-04]", denies(put(base, "drt", drt)))
}

test_violation_internet_gateway_in_the_use_case if {
	igw := rc("aws_internet_gateway", "module.n.module.subnet_tiers.aws_internet_gateway.this", {})
	fixture.has("[PLAN-NET-04]", denies(put(base, "igw", igw)))
}

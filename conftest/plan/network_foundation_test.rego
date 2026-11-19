package plan.network_foundation_test

import data.plan.fixture
import data.plan.network_foundation as p

key_arn := "arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"

nf := {
	"vpc": "module.n.module.vpc.aws_vpc.this",
	"default_security_group": "module.n.module.vpc.aws_default_security_group.this",
	"default_route_table": "module.n.module.vpc.aws_default_route_table.this",
	"flow_log": "module.n.module.vpc.aws_flow_log.this",
	"flow_logs_role": "module.n.module.vpc.aws_iam_role.flow_logs",
	"flow_logs_log_group": "module.n.module.flow_logs.aws_cloudwatch_log_group.this",
	"flow_logs_key": "module.n.module.kms.aws_kms_key.this",
	"route_tables": {"address": "module.n.module.subnet_tiers.aws_route_table.this", "count": 2},
	"module_prefix": "module.n.",
	"allowed_types": ["aws_cloudwatch_log_group", "aws_default_route_table", "aws_default_security_group", "aws_flow_log", "aws_iam_role", "aws_kms_key", "aws_route_table", "aws_vpc"],
}

decl := {"network_foundation": nf}

rc(type, address, after) := {"address": address, "type": type, "change": {"actions": ["create"], "after": after, "after_unknown": {}}}

statement := {
	"Sid": "AllowVpcFlowLogsOfThisAccount",
	"Effect": "Allow",
	"Principal": {"Service": "vpc-flow-logs.amazonaws.com"},
	"Action": "sts:AssumeRole",
	"Condition": {
		"StringEquals": {"aws:SourceAccount": "123456789012"},
		"ArnLike": {"aws:SourceArn": "arn:aws:ec2:ap-northeast-1:123456789012:vpc-flow-log/*"},
	},
}

role_with(statements) := rc("aws_iam_role", nf.flow_logs_role, {"assume_role_policy": json.marshal({"Version": "2012-10-17", "Statement": statements})})

rt(key, after) := rc("aws_route_table", sprintf("module.n.module.subnet_tiers.aws_route_table.this[\"%s\"]", [key]), after)

base := {
	"vpc": rc("aws_vpc", nf.vpc, {"cidr_block": "10.0.0.0/16"}),
	"sg": rc("aws_default_security_group", nf.default_security_group, {"ingress": [], "egress": []}),
	"drt": rc("aws_default_route_table", nf.default_route_table, {"route": [], "propagating_vgws": []}),
	"fl": rc("aws_flow_log", nf.flow_log, {"traffic_type": "ALL", "log_destination_type": "cloud-watch-logs"}),
	"role": role_with([statement]),
	"lg": rc("aws_cloudwatch_log_group", nf.flow_logs_log_group, {"kms_key_id": key_arn}),
	"key": rc("aws_kms_key", nf.flow_logs_key, {"arn": key_arn}),
	"rt0": rt("private-apne1-az1", {"route": [], "propagating_vgws": []}),
	"rt1": rt("isolated-apne1-az1", {"route": [], "propagating_vgws": []}),
}

input_of(m) := fixture.plan([r | some r in m])

denies(m) := msgs if {
	msgs := p.deny with input as input_of(m) with data.policy_plan as decl
}

denies_with(m, d) := msgs if {
	msgs := p.deny with input as input_of(m) with data.policy_plan as {"network_foundation": d}
}

# object.union は入れ子の object を合わせるので、キーを除いてから置き換える。
put(m, k, v) := object.union(object.remove(m, [k]), {k: v})

route := {"cidr_block": "0.0.0.0/0", "gateway_id": "igw-0123456789abcdef0"}

test_pass_reference_shape if {
	count(denies(base)) == 0
}

test_pass_other_module_may_hold_egress if {
	count(denies(put(base, "nat", rc("aws_nat_gateway", "module.e.aws_nat_gateway.this", {})))) == 0
}

test_violation_missing_declaration if {
	msgs := p.deny with input as input_of(base) with data.policy_plan as {}
	fixture.has("[PLAN-NET-00]", msgs)
}

test_violation_each_declaration_key_missing if {
	every k in ["vpc", "default_security_group", "default_route_table", "flow_log", "flow_logs_role", "flow_logs_log_group", "flow_logs_key", "route_tables", "module_prefix", "allowed_types"] {
		fixture.has("[PLAN-NET-00]", denies_with(base, object.remove(nf, [k])))
	}
}

test_violation_malformed_declaration_values if {
	fixture.has("[PLAN-NET-00]", denies_with(base, put(nf, "module_prefix", "")))
	fixture.has("[PLAN-NET-00]", denies_with(base, put(nf, "module_prefix", "module.n")))
	fixture.has("[PLAN-NET-00]", denies_with(base, put(nf, "route_tables", {"address": nf.route_tables.address, "count": "2"})))
	fixture.has("[PLAN-NET-00]", denies_with(base, put(nf, "route_tables", {"address": nf.route_tables.address, "count": 0})))
	fixture.has("[PLAN-NET-00]", denies_with(base, put(nf, "allowed_types", [])))
	fixture.has("[PLAN-NET-00]", denies_with(base, put(nf, "vpc", "module.other.aws_vpc.this")))
}

test_violation_empty_input if {
	msgs := p.deny with input as fixture.plan([]) with data.policy_plan as decl
	fixture.has("[PLAN-NET-00]", msgs)
}

test_violation_each_declared_resource_absent if {
	every k in ["vpc", "sg", "drt", "fl", "role", "lg", "key"] {
		fixture.has("[PLAN-NET-00]", denies(object.remove(base, [k])))
	}
}

test_violation_route_tables_absent if {
	fixture.has("[PLAN-NET-00]", denies(object.remove(base, ["rt0", "rt1"])))
}

test_violation_route_tables_too_few if {
	fixture.has("[PLAN-NET-00]", denies(object.remove(base, ["rt1"])))
}

test_violation_route_tables_too_many if {
	fixture.has("[PLAN-NET-00]", denies(put(base, "rt2", rt("private-apne1-az2", {"route": [], "propagating_vgws": []}))))
}

test_violation_default_security_group_has_ingress if {
	sg := rc("aws_default_security_group", nf.default_security_group, {"ingress": [{"self": true}], "egress": []})
	some m in denies(put(base, "sg", sg))
	startswith(m, "[PLAN-NET-01]")
	contains(m, "ingress")
}

test_violation_default_security_group_has_egress if {
	sg := rc("aws_default_security_group", nf.default_security_group, {"ingress": [], "egress": [{"cidr_blocks": ["0.0.0.0/0"]}]})
	some m in denies(put(base, "sg", sg))
	startswith(m, "[PLAN-NET-01]")
	contains(m, "egress")
}

test_violation_default_security_group_rules_omitted if {
	sg := rc("aws_default_security_group", nf.default_security_group, {})
	fixture.has("[PLAN-NET-01]", denies(put(base, "sg", sg)))
}

test_violation_flow_logs_reject_only if {
	fl := rc("aws_flow_log", nf.flow_log, {"traffic_type": "REJECT", "log_destination_type": "cloud-watch-logs"})
	fixture.has("[PLAN-NET-02]", denies(put(base, "fl", fl)))
}

test_violation_flow_logs_to_s3 if {
	fl := rc("aws_flow_log", nf.flow_log, {"traffic_type": "ALL", "log_destination_type": "s3"})
	fixture.has("[PLAN-NET-02]", denies(put(base, "fl", fl)))
}

test_violation_flow_logs_attributes_omitted if {
	fixture.has("[PLAN-NET-02]", denies(put(base, "fl", rc("aws_flow_log", nf.flow_log, {}))))
}

test_violation_trust_variants if {
	every st in [
		put(statement, "Condition", {"StringEquals": statement.Condition.StringEquals}),
		put(statement, "Condition", {"ArnLike": statement.Condition.ArnLike}),
		put(statement, "Condition", {"StringEquals": statement.Condition.StringEquals, "ArnLike": {"aws:SourceArn": "*"}}),
		put(statement, "Condition", {"StringEquals": statement.Condition.StringEquals, "ArnLike": {"aws:SourceArn": "arn:aws:ec2:ap-northeast-1:123456789012:vpc/*"}}),
		put(statement, "Condition", {"StringEquals": {"aws:SourceAccount": "*"}, "ArnLike": statement.Condition.ArnLike}),
		put(statement, "Condition", {"StringEquals": {"aws:SourceAccount": "123456789012", "aws:SourceOrgID": "o-x"}, "ArnLike": statement.Condition.ArnLike}),
		put(statement, "Condition", object.union(statement.Condition, {"Bool": {"aws:SecureTransport": "true"}})),
		put(statement, "Principal", {"Service": ["vpc-flow-logs.amazonaws.com", "ec2.amazonaws.com"]}),
		put(statement, "Effect", "Deny"),
		put(statement, "Action", "sts:*"),
	] {
		fixture.has("[PLAN-NET-03]", denies(put(base, "role", role_with([st]))))
	}
}

test_violation_trust_two_statements if {
	extra := {"Effect": "Allow", "Principal": {"Service": "ec2.amazonaws.com"}, "Action": "sts:AssumeRole"}
	fixture.has("[PLAN-NET-03]", denies(put(base, "role", role_with([statement, extra]))))
}

test_violation_trust_policy_unreadable if {
	fixture.has("[PLAN-NET-03]", denies(put(base, "role", rc("aws_iam_role", nf.flow_logs_role, {}))))
	fixture.has("[PLAN-NET-03]", denies(put(base, "role", rc("aws_iam_role", nf.flow_logs_role, {"assume_role_policy": "{"}))))
}

test_violation_route_table_with_default_route if {
	fixture.has("[PLAN-NET-04]", denies(put(base, "rt0", rt("private-apne1-az1", {"route": [route], "propagating_vgws": []}))))
}

test_violation_route_table_with_propagation if {
	fixture.has("[PLAN-NET-04]", denies(put(base, "rt0", rt("private-apne1-az1", {"route": [], "propagating_vgws": ["vgw-0123456789abcdef0"]}))))
}

test_violation_route_table_attributes_omitted if {
	fixture.has("[PLAN-NET-04]", denies(put(base, "rt0", rt("private-apne1-az1", {}))))
}

test_violation_main_route_table_with_route if {
	drt := rc("aws_default_route_table", nf.default_route_table, {"route": [route], "propagating_vgws": []})
	fixture.has("[PLAN-NET-04]", denies(put(base, "drt", drt)))
}

test_violation_main_route_table_with_propagation if {
	drt := rc("aws_default_route_table", nf.default_route_table, {"route": [], "propagating_vgws": ["vgw-0123456789abcdef0"]})
	fixture.has("[PLAN-NET-04]", denies(put(base, "drt", drt)))
}

test_violation_type_outside_the_declaration if {
	every t in ["aws_route", "aws_internet_gateway", "aws_nat_gateway", "aws_egress_only_internet_gateway", "aws_vpc_peering_connection", "aws_vpn_gateway", "aws_vpn_gateway_route_propagation", "aws_ec2_transit_gateway_vpc_attachment", "aws_vpc_endpoint"] {
		fixture.has("[PLAN-NET-04]", denies(put(base, "x", rc(t, sprintf("module.n.module.subnet_tiers.%s.this", [t]), {}))))
	}
}

test_violation_log_group_with_other_key if {
	lg := rc("aws_cloudwatch_log_group", nf.flow_logs_log_group, {"kms_key_id": "arn:aws:kms:ap-northeast-1:123456789012:key/00000000-0000-0000-0000-000000000000"})
	fixture.has("[PLAN-NET-05]", denies(put(base, "lg", lg)))
}

test_violation_log_group_without_key if {
	fixture.has("[PLAN-NET-05]", denies(put(base, "lg", rc("aws_cloudwatch_log_group", nf.flow_logs_log_group, {}))))
}

test_violation_log_group_and_key_both_unknown if {
	lg := rc("aws_cloudwatch_log_group", nf.flow_logs_log_group, {"kms_key_id": null})
	key := rc("aws_kms_key", nf.flow_logs_key, {})
	fixture.has("[PLAN-NET-05]", denies(put(put(base, "lg", lg), "key", key)))
}

test_violation_type_outside_the_declaration_under_indexed_module if {
	fixture.has("[PLAN-NET-04]", denies(put(base, "x", rc("aws_internet_gateway", "module.n[0].module.subnet_tiers.aws_internet_gateway.this", {}))))
}

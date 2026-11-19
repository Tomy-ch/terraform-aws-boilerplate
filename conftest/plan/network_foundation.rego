# network-foundation の VPC の security invariant（Security Hub EC2.2・EC2.6、ADR-0301 決定16・20、
# network-foundation/ADR-0102・0201）。Security の道具は既定の SG の規則・Flow Logs の traffic_type・
# 配送 role の trust の Condition・経路の不在を見ない（ADR-0402 決定3・4）。役割は宣言した address から引く（同 決定13）。
package plan.network_foundation

import data.plan.lib

cfg := object.get(data.policy_plan, "network_foundation", {})

single_names := ["vpc", "default_security_group", "default_route_table", "flow_log", "flow_logs_role"]

after(address) := c.rc.change.after if {
	some c in lib.changes
	c.rc.address == address
}

present(address) if {
	some c in lib.changes
	c.rc.address == address
}

route_table_instances contains c.rc if {
	some c in lib.changes
	lib.strip_index(c.rc.address) == object.get(cfg, "route_tables", {"address": ""}).address
}

egress_types := {"aws_route", "aws_internet_gateway", "aws_nat_gateway", "aws_egress_only_internet_gateway", "aws_vpc_peering_connection"}

well_formed if {
	every name in single_names {
		is_string(object.get(cfg, name, null))
	}
	is_string(object.get(object.get(cfg, "route_tables", {}), "address", null))
	is_number(object.get(object.get(cfg, "route_tables", {}), "count", null))
	is_string(object.get(cfg, "module_prefix", null))
}

deny contains msg if {
	not well_formed
	msg := "[PLAN-NET-00] data.policy_plan.network_foundation の宣言が欠けているか形が違います"
}

deny contains msg if {
	well_formed
	some name in single_names
	not present(cfg[name])
	msg := sprintf("[PLAN-NET-00] 宣言した %s（%s）が入力にありません", [name, cfg[name]])
}

deny contains msg if {
	well_formed
	count(route_table_instances) != cfg.route_tables.count
	msg := sprintf("[PLAN-NET-00] 宣言した Route Table（%s）が %d 件ではなく %d 件です", [cfg.route_tables.address, cfg.route_tables.count, count(route_table_instances)])
}

deny contains msg if {
	well_formed
	some dir in ["ingress", "egress"]
	rules := object.get(after(cfg.default_security_group), dir, null)
	not rules == []
	msg := sprintf("[PLAN-NET-01] 既定の Security Group（%s）の %s が空の list ではありません", [cfg.default_security_group, dir])
}

deny contains msg if {
	well_formed
	fl := after(cfg.flow_log)
	not fl.traffic_type == "ALL"
	msg := sprintf("[PLAN-NET-02] Flow Logs（%s）の traffic_type が ALL ではありません", [cfg.flow_log])
}

deny contains msg if {
	well_formed
	fl := after(cfg.flow_log)
	not fl.log_destination_type == "cloud-watch-logs"
	msg := sprintf("[PLAN-NET-02] Flow Logs（%s）の宛先が CloudWatch Logs ではありません", [cfg.flow_log])
}

trust_ok(body) if {
	statements := lib.statements(body)
	count(statements) == 1
	st := statements[0]
	st.Effect == "Allow"
	st.Principal == {"Service": "vpc-flow-logs.amazonaws.com"}
	st.Action == "sts:AssumeRole"
	regex.match(`^[0-9]{12}$`, st.Condition.StringEquals["aws:SourceAccount"])
	regex.match(`^arn:aws[a-z-]*:ec2:[a-z0-9-]+:[0-9]{12}:vpc-flow-log/\*$`, st.Condition.ArnLike["aws:SourceArn"])
	count(object.keys(st.Condition)) == 2
}

deny contains msg if {
	well_formed
	role := after(cfg.flow_logs_role)
	not trust_ok(json.unmarshal(role.assume_role_policy))
	msg := sprintf("[PLAN-NET-03] 配送 role（%s）の trust policy が、vpc-flow-logs.amazonaws.com に aws:SourceAccount と aws:SourceArn（vpc-flow-log/*）の条件を付けた1文ではありません", [cfg.flow_logs_role])
}

deny contains msg if {
	well_formed
	some rc in route_table_instances
	not object.get(rc.change.after, "route", null) == []
	msg := sprintf("[PLAN-NET-04] Route Table（%s）の route が空の list ではありません", [rc.address])
}

deny contains msg if {
	well_formed
	not object.get(after(cfg.default_route_table), "route", null) == []
	msg := sprintf("[PLAN-NET-04] main Route Table（%s）の route が空の list ではありません", [cfg.default_route_table])
}

deny contains msg if {
	well_formed
	some c in lib.changes
	startswith(c.rc.address, cfg.module_prefix)
	c.rc.type in egress_types
	msg := sprintf("[PLAN-NET-04] network-foundation の下に外部への経路を作る resource（%s）があります", [c.rc.address])
}

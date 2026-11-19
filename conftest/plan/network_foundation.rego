# network-foundation の VPC の security invariant（Security Hub EC2.2・EC2.6、ADR-0301 決定16・20、
# network-foundation/ADR-0102・0201）。Security の道具は既定の SG の規則・Flow Logs の traffic_type・
# 配送 role の trust の Condition・経路の不在を見ない（ADR-0402 決定3・4）。役割は宣言した address から引く（同 決定13）。
package plan.network_foundation

import data.plan.lib

cfg := object.get(data.policy_plan, "network_foundation", {})

single_names := ["vpc", "default_security_group", "default_route_table", "flow_log", "flow_logs_role", "flow_logs_log_group", "flow_logs_key"]

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
	lib.strip_index(c.rc.address) == cfg.route_tables.address
}

# module の下の変更。宣言の外の型（Internet Gateway・NAT Gateway・VPN Gateway・Transit Gateway の attachment・
# VPC Endpoint・peering など）を、型を列挙せずに拾う。
use_case_changes contains c.rc if {
	some c in lib.changes
	startswith(c.rc.address, cfg.module_prefix)
}

prefix_ok if {
	is_string(object.get(cfg, "module_prefix", null))
	startswith(cfg.module_prefix, "module.")
	endswith(cfg.module_prefix, ".")
}

well_formed if {
	prefix_ok
	every name in single_names {
		is_string(object.get(cfg, name, null))
		startswith(cfg[name], cfg.module_prefix)
	}
	is_string(object.get(object.get(cfg, "route_tables", {}), "address", null))
	startswith(cfg.route_tables.address, cfg.module_prefix)
	is_number(object.get(object.get(cfg, "route_tables", {}), "count", null))
	cfg.route_tables.count >= 1
	is_array(object.get(cfg, "allowed_types", null))
	count(cfg.allowed_types) >= 1
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
	not object.get(after(cfg.default_security_group), dir, null) == []
	msg := sprintf("[PLAN-NET-01] 既定の Security Group（%s）の %s が空の list ではありません", [cfg.default_security_group, dir])
}

deny contains msg if {
	well_formed
	not object.get(after(cfg.flow_log), "traffic_type", null) == "ALL"
	msg := sprintf("[PLAN-NET-02] Flow Logs（%s）の traffic_type が ALL ではありません", [cfg.flow_log])
}

deny contains msg if {
	well_formed
	not object.get(after(cfg.flow_log), "log_destination_type", null) == "cloud-watch-logs"
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
	count(object.keys(st.Condition.StringEquals)) == 1
	regex.match(`^arn:aws[a-z-]*:ec2:[a-z0-9-]+:[0-9]{12}:vpc-flow-log/\*$`, st.Condition.ArnLike["aws:SourceArn"])
	count(object.keys(st.Condition.ArnLike)) == 1
	count(object.keys(st.Condition)) == 2
}

trust_document := json.unmarshal(object.get(after(cfg.flow_logs_role), "assume_role_policy", "")) if {
	is_string(object.get(after(cfg.flow_logs_role), "assume_role_policy", null))
	json.is_valid(after(cfg.flow_logs_role).assume_role_policy)
}

trust_valid if trust_ok(trust_document)

# 文書が plan で決まらないか読めないときも落とす。CI の実 plan の経路は plan.input を掛けない（conftest/plan/README.md の「入力」）。
deny contains msg if {
	well_formed
	present(cfg.flow_logs_role)
	not trust_valid
	msg := sprintf("[PLAN-NET-03] 配送 role（%s）の trust policy が、vpc-flow-logs.amazonaws.com に aws:SourceAccount と aws:SourceArn（vpc-flow-log/*）の条件を付けた1文ではありません", [cfg.flow_logs_role])
}

deny contains msg if {
	well_formed
	some rc in route_table_instances
	some attr in ["route", "propagating_vgws"]
	not object.get(rc.change.after, attr, null) == []
	msg := sprintf("[PLAN-NET-04] Route Table（%s）の %s が空の list ではありません", [rc.address, attr])
}

deny contains msg if {
	well_formed
	some attr in ["route", "propagating_vgws"]
	not object.get(after(cfg.default_route_table), attr, null) == []
	msg := sprintf("[PLAN-NET-04] main Route Table（%s）の %s が空の list ではありません", [cfg.default_route_table, attr])
}

deny contains msg if {
	well_formed
	some rc in use_case_changes
	not rc.type in {t | some t in cfg.allowed_types}
	msg := sprintf("[PLAN-NET-04] network-foundation の下に宣言の外の型の resource（%s）があります", [rc.address])
}

deny contains msg if {
	well_formed
	present(cfg.flow_logs_log_group)
	not object.get(after(cfg.flow_logs_log_group), "kms_key_id", "") == object.get(after(cfg.flow_logs_key), "arn", null)
	msg := sprintf("[PLAN-NET-05] Flow Logs の Log Group（%s）が、宣言した鍵（%s）で暗号化されていません", [cfg.flow_logs_log_group, cfg.flow_logs_key])
}

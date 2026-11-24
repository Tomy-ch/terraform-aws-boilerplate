package plan.security_group_ingress_test

import data.plan.fixture
import data.plan.security_group_ingress as p

decl(entrypoints) := {"security_group_ingress": {"public_entrypoints": entrypoints}}

https := [{"from": 443, "to": 443, "protocol": "tcp"}]

lb := [{"module": "module.lb", "ports": https}]

change(type, address, after, unknown) := {"address": address, "type": type, "change": {"actions": ["create"], "after": after, "after_unknown": unknown}}

with_actions(rc, actions) := json.patch(rc, [{"op": "replace", "path": "/change/actions", "value": actions}])

block(v4, v6) := {"from_port": 443, "to_port": 443, "protocol": "tcp", "cidr_blocks": v4, "ipv6_cidr_blocks": v6, "security_groups": []}

sg(address, blocks) := change("aws_security_group", address, {"name": "s", "ingress": blocks}, {})

default_sg(blocks) := change("aws_default_security_group", "module.v.aws_default_security_group.this", {"ingress": blocks}, {})

sg_rule(type, v4, v6) := change("aws_security_group_rule", "module.x.aws_security_group_rule.this", {"type": type, "from_port": 443, "to_port": 443, "protocol": "tcp", "cidr_blocks": v4, "ipv6_cidr_blocks": v6}, {})

vpc_rule(address, attr, cidr) := change("aws_vpc_security_group_ingress_rule", address, {"ip_protocol": "tcp", "from_port": 443, "to_port": 443, attr: cidr}, {})

ports_rule(address, protocol, from, to) := change("aws_vpc_security_group_ingress_rule", address, {"ip_protocol": protocol, "from_port": from, "to_port": to, "cidr_ipv4": "0.0.0.0/0"}, {})

messages(rcs, entrypoints) := msgs if {
	msgs := p.deny with input as fixture.plan(rcs) with data.policy_plan as decl(entrypoints)
}

outside(address, cidr) := sprintf("[PLAN-SG-01] p.json: %s が %s からの ingress を許します。公開点として宣言していません", [address, cidr])

undeclared_port(address, cidr, ports) := sprintf("[PLAN-SG-01] p.json: %s が %s からの ingress を、公開点として宣言していない port（%s）で許します", [address, cidr, ports])

unknown(address, attr) := sprintf("[PLAN-SG-02] p.json: %s の %s が plan で決まらないので、全開放でないことを確かめられません", [address, attr])

ports_unknown(address) := sprintf("[PLAN-SG-02] p.json: %s の port か protocol が plan で決まらないので、公開点として宣言した port の内かを確かめられません", [address])

malformed(address, attr) := sprintf("[PLAN-SG-02] p.json: %s の %s に文字列でない値があるので、全開放でないことを確かめられません", [address, attr])

shape := "[PLAN-SG-00] data.policy_plan.security_group_ingress.public_entrypoints の宣言が欠けているか形が違います"

sg_address := "module.x.aws_security_group.this"

rule_address := "module.x.aws_security_group_rule.this"

vpc_address := "module.x.aws_vpc_security_group_ingress_rule.this"

lb_address := "module.lb.aws_vpc_security_group_ingress_rule.public"

test_violation_inline_v4_outside_entrypoint if {
	messages([sg(sg_address, [block(["10.0.0.0/8", "0.0.0.0/0"], [])])], []) == {outside(sg_address, "0.0.0.0/0")}
}

test_violation_inline_v6_outside_entrypoint if {
	messages([sg(sg_address, [block([], ["::/0"])])], []) == {outside(sg_address, "::/0")}
}

test_violation_default_sg_outside_entrypoint if {
	messages([default_sg([block(["0.0.0.0/0"], [])])], []) == {outside("module.v.aws_default_security_group.this", "0.0.0.0/0")}
}

test_violation_rule_v4_outside_entrypoint if {
	messages([sg_rule("ingress", ["0.0.0.0/0"], [])], []) == {outside(rule_address, "0.0.0.0/0")}
}

test_violation_rule_v6_outside_entrypoint if {
	messages([sg_rule("ingress", [], ["::/0"])], []) == {outside(rule_address, "::/0")}
}

test_violation_vpc_rule_v4_outside_entrypoint if {
	messages([vpc_rule(vpc_address, "cidr_ipv4", "0.0.0.0/0")], []) == {outside(vpc_address, "0.0.0.0/0")}
}

test_violation_vpc_rule_v6_outside_entrypoint if {
	messages([vpc_rule(vpc_address, "cidr_ipv6", "::/0")], []) == {outside(vpc_address, "::/0")}
}

test_violation_one_block_v4_and_v6 if {
	messages([sg(sg_address, [block(["0.0.0.0/0"], ["::/0"])])], []) == {outside(sg_address, "0.0.0.0/0"), outside(sg_address, "::/0")}
}

test_violation_open_ingress_in_second_block if {
	rc := sg(sg_address, [block(["10.0.0.0/8"], []), block(["0.0.0.0/0"], [])])
	messages([rc], []) == {outside(sg_address, "0.0.0.0/0")}
}

test_violation_noncanonical_zero_prefix_v4 if {
	messages([vpc_rule(vpc_address, "cidr_ipv4", "10.0.0.0/0")], []) == {outside(vpc_address, "10.0.0.0/0")}
}

test_violation_noncanonical_zero_prefix_v6 if {
	messages([vpc_rule(vpc_address, "cidr_ipv6", "0:0:0:0:0:0:0:0/0")], []) == {outside(vpc_address, "0:0:0:0:0:0:0:0/0")}
}

test_violation_replace_checked if {
	rc := with_actions(vpc_rule(vpc_address, "cidr_ipv4", "0.0.0.0/0"), ["delete", "create"])
	messages([rc], []) == {outside(vpc_address, "0.0.0.0/0")}
}

test_violation_create_before_destroy_checked if {
	rc := with_actions(vpc_rule(vpc_address, "cidr_ipv4", "0.0.0.0/0"), ["create", "delete"])
	messages([rc], []) == {outside(vpc_address, "0.0.0.0/0")}
}

test_violation_no_op_checked if {
	rc := with_actions(vpc_rule(vpc_address, "cidr_ipv4", "0.0.0.0/0"), ["no-op"])
	messages([rc], []) == {outside(vpc_address, "0.0.0.0/0")}
}

test_violation_other_address_beside_entrypoint if {
	open := vpc_rule(vpc_address, "cidr_ipv4", "0.0.0.0/0")
	public := vpc_rule(lb_address, "cidr_ipv4", "0.0.0.0/0")
	messages([open, public], lb) == {outside(vpc_address, "0.0.0.0/0")}
}

test_violation_similar_module_prefix_not_exempt if {
	public := vpc_rule(lb_address, "cidr_ipv4", "0.0.0.0/0")
	similar := vpc_rule("module.lb2.aws_vpc_security_group_ingress_rule.public", "cidr_ipv4", "0.0.0.0/0")
	messages([public, similar], lb) == {outside("module.lb2.aws_vpc_security_group_ingress_rule.public", "0.0.0.0/0")}
}

test_violation_parent_module_not_covered_by_child if {
	child := vpc_rule("module.x.module.lb.aws_vpc_security_group_ingress_rule.public", "cidr_ipv4", "0.0.0.0/0")
	parent := vpc_rule(vpc_address, "cidr_ipv4", "0.0.0.0/0")
	messages([child, parent], [{"module": "module.x.module.lb", "ports": https}]) == {outside(vpc_address, "0.0.0.0/0")}
}

test_violation_cidr_unknown if {
	rule := change("aws_vpc_security_group_ingress_rule", vpc_address, {"ip_protocol": "tcp"}, {"cidr_ipv4": true})
	inline := change("aws_security_group", sg_address, {"ingress": [block(null, [])]}, {"ingress": [{"cidr_blocks": true}]})
	legacy := change("aws_security_group_rule", rule_address, {"type": "ingress", "cidr_blocks": [null]}, {"cidr_blocks": [true]})
	whole := change("aws_default_security_group", "module.v.aws_default_security_group.this", {}, {"ingress": true})
	type_unknown := change("aws_security_group_rule", "module.y.aws_security_group_rule.this", {"cidr_blocks": ["0.0.0.0/0"]}, {"type": true})
	messages([rule, inline, legacy, whole, type_unknown], []) == {
		unknown(vpc_address, "cidr_ipv4"),
		unknown(sg_address, "cidr_blocks"),
		unknown(rule_address, "cidr_blocks"),
		"[PLAN-SG-02] p.json: module.v.aws_default_security_group.this の ingress が plan で決まらないので、全開放でないことを確かめられません",
		"[PLAN-SG-02] p.json: module.y.aws_security_group_rule.this の type が plan で決まらないので、全開放でないことを確かめられません",
	}
}

test_violation_ipv6_unknown if {
	rule := change("aws_vpc_security_group_ingress_rule", vpc_address, {"ip_protocol": "tcp"}, {"cidr_ipv6": true})
	inline := change("aws_security_group", sg_address, {"ingress": [block([], null)]}, {"ingress": [{"ipv6_cidr_blocks": true}]})
	dsg := change("aws_default_security_group", "module.v.aws_default_security_group.this", {"ingress": [block([], null)]}, {"ingress": [{"ipv6_cidr_blocks": true}]})
	legacy := change("aws_security_group_rule", rule_address, {"type": "ingress", "ipv6_cidr_blocks": [null]}, {"ipv6_cidr_blocks": [true]})
	messages([rule, inline, dsg, legacy], []) == {
		unknown(vpc_address, "cidr_ipv6"),
		unknown(sg_address, "ipv6_cidr_blocks"),
		unknown("module.v.aws_default_security_group.this", "ipv6_cidr_blocks"),
		unknown(rule_address, "ipv6_cidr_blocks"),
	}
}

test_violation_ingress_block_unknown if {
	rc := change("aws_security_group", sg_address, {"ingress": [block(["10.0.0.0/8"], []), null]}, {"ingress": [{}, true]})
	messages([rc], []) == {"[PLAN-SG-02] p.json: module.x.aws_security_group.this の ingress が plan で決まらないので、全開放でないことを確かめられません"}
}

test_pass_cidr_unknown_on_declared_entrypoint if {
	public := change("aws_vpc_security_group_ingress_rule", lb_address, {"ip_protocol": "tcp", "from_port": 443, "to_port": 443}, {"cidr_ipv4": true})
	count(messages([public], lb)) == 0
}

test_violation_cidr_unknown_beside_declared_entrypoint if {
	public := change("aws_vpc_security_group_ingress_rule", lb_address, {"ip_protocol": "tcp", "from_port": 443, "to_port": 443}, {"cidr_ipv4": true})
	other := change("aws_vpc_security_group_ingress_rule", vpc_address, {"ip_protocol": "tcp", "from_port": 443, "to_port": 443}, {"cidr_ipv4": true})
	messages([public, other], lb) == {unknown(vpc_address, "cidr_ipv4")}
}

test_violation_cidr_unknown_on_entrypoint_outside_declared_port if {
	public := change("aws_vpc_security_group_ingress_rule", lb_address, {"ip_protocol": "tcp", "from_port": 22, "to_port": 22}, {"cidr_ipv4": true})
	messages([public], lb) == {unknown(lb_address, "cidr_ipv4")}
}

test_violation_whole_ingress_unknown_on_entrypoint if {
	whole := change("aws_security_group", "module.lb.aws_security_group.this", {}, {"ingress": true})
	messages([whole], lb) == {"[PLAN-SG-02] p.json: module.lb.aws_security_group.this の ingress が plan で決まらないので、全開放でないことを確かめられません"}
}

test_violation_cidr_element_not_string if {
	rc := sg_rule("ingress", [1], [])
	messages([rc], []) == {malformed(rule_address, "cidr_blocks")}
}

test_violation_vpc_cidr_not_string if {
	rc := vpc_rule(vpc_address, "cidr_ipv4", {"cidr": "0.0.0.0/0"})
	messages([rc], []) == {malformed(vpc_address, "cidr_ipv4")}
}

test_violation_inline_cidr_element_not_string if {
	rc := sg(sg_address, [block([["0.0.0.0/0"]], [])])
	messages([rc], []) == {malformed(sg_address, "cidr_blocks")}
}

test_violation_ingress_element_not_object if {
	rc := sg(sg_address, [block(["10.0.0.0/8"], []), "0.0.0.0/0"])
	messages([rc], []) == {"[PLAN-SG-02] p.json: module.x.aws_security_group.this の ingress が block の list の形でないので、全開放でないことを確かめられません"}
}

test_violation_ingress_not_list if {
	rc := sg(sg_address, "0.0.0.0/0")
	messages([rc], []) == {"[PLAN-SG-02] p.json: module.x.aws_security_group.this の ingress が block の list の形でないので、全開放でないことを確かめられません"}
}

test_violation_rule_type_null if {
	rc := sg_rule(null, ["0.0.0.0/0"], [])
	messages([rc], []) == {"[PLAN-SG-02] p.json: module.x.aws_security_group_rule.this の type が文字列でないので、ingress かを確かめられません"}
}

test_violation_rule_type_absent if {
	rc := change("aws_security_group_rule", rule_address, {"cidr_blocks": ["0.0.0.0/0"]}, {})
	messages([rc], []) == {"[PLAN-SG-02] p.json: module.x.aws_security_group_rule.this の type が文字列でないので、ingress かを確かめられません"}
}

test_pass_open_ingress_inside_declared_ports if {
	rcs := [
		vpc_rule("module.lb.aws_vpc_security_group_ingress_rule.public[\"v4\"]", "cidr_ipv4", "0.0.0.0/0"),
		vpc_rule("module.lb.aws_vpc_security_group_ingress_rule.public[\"v6\"]", "cidr_ipv6", "::/0"),
		change("aws_security_group", "module.lb.module.sg.aws_security_group.this", {"ingress": [block(["0.0.0.0/0"], ["::/0"])]}, {}),
		change("aws_security_group_rule", "module.lb.aws_security_group_rule.this", {"type": "ingress", "from_port": 443, "to_port": 443, "protocol": "TCP", "cidr_blocks": ["0.0.0.0/0"]}, {}),
	]
	count(messages(rcs, lb)) == 0
}

test_pass_open_ingress_inside_declared_range if {
	rc := ports_rule("module.lb.aws_vpc_security_group_ingress_rule.public", "tcp", 8000, 8080)
	count(messages([rc], [{"module": "module.lb", "ports": [{"from": 8000, "to": 9000, "protocol": "tcp"}]}])) == 0
}

test_pass_entrypoint_module_with_index_key if {
	rc := vpc_rule("module.lb[\"a\"].aws_vpc_security_group_ingress_rule.public", "cidr_ipv4", "0.0.0.0/0")
	count(messages([rc], lb)) == 0
}

test_violation_open_ingress_outside_declared_port if {
	rc := ports_rule(lb_address, "tcp", 22, 22)
	messages([rc], lb) == {undeclared_port(lb_address, "0.0.0.0/0", "tcp 22-22")}
}

test_violation_open_ingress_range_wider_than_declared if {
	rc := ports_rule(lb_address, "tcp", 443, 444)
	messages([rc], lb) == {undeclared_port(lb_address, "0.0.0.0/0", "tcp 443-444")}
}

test_violation_open_ingress_other_protocol if {
	rc := ports_rule(lb_address, "udp", 443, 443)
	messages([rc], lb) == {undeclared_port(lb_address, "0.0.0.0/0", "udp 443-443")}
}

test_violation_all_protocols_needs_declared_all if {
	rc := change("aws_vpc_security_group_ingress_rule", lb_address, {"ip_protocol": "-1", "cidr_ipv4": "0.0.0.0/0"}, {})
	messages([rc], [{"module": "module.lb", "ports": [{"from": 0, "to": 65535, "protocol": "tcp"}]}]) == {undeclared_port(lb_address, "0.0.0.0/0", "-1 0-65535")}
}

test_violation_inline_all_protocols_with_zero_ports if {
	rc := change("aws_security_group", "module.lb.aws_security_group.this", {"ingress": [{"from_port": 0, "to_port": 0, "protocol": "-1", "cidr_blocks": ["0.0.0.0/0"]}]}, {})
	messages([rc], lb) == {undeclared_port("module.lb.aws_security_group.this", "0.0.0.0/0", "-1 0-65535")}
}

test_violation_rule_protocol_all_needs_declared_all if {
	rc := change("aws_security_group_rule", "module.lb.aws_security_group_rule.this", {"type": "ingress", "from_port": 443, "to_port": 443, "protocol": "all", "cidr_blocks": ["0.0.0.0/0"]}, {})
	messages([rc], lb) == {undeclared_port("module.lb.aws_security_group_rule.this", "0.0.0.0/0", "-1 0-65535")}
}

test_pass_all_protocols_on_declared_all if {
	rc := change("aws_vpc_security_group_ingress_rule", lb_address, {"ip_protocol": "-1", "cidr_ipv4": "0.0.0.0/0"}, {})
	count(messages([rc], [{"module": "module.lb", "ports": [{"from": 0, "to": 65535, "protocol": "-1"}]}])) == 0
}

test_violation_rule_from_after_to_not_covered if {
	rc := ports_rule(lb_address, "tcp", 65535, 0)
	messages([rc], lb) == {undeclared_port(lb_address, "0.0.0.0/0", "tcp 65535-0")}
}

test_violation_icmp_on_entrypoint if {
	rc := ports_rule(lb_address, "icmp", 8, 0)
	messages([rc], lb) == {undeclared_port(lb_address, "0.0.0.0/0", "icmp 8-0")}
}

test_violation_icmp_by_number_under_declared_range if {
	rc := ports_rule(lb_address, "1", 8, 0)
	messages([rc], [{"module": "module.lb", "ports": [{"from": 0, "to": 65535, "protocol": "tcp"}]}]) == {undeclared_port(lb_address, "0.0.0.0/0", "1 8-0")}
}

test_violation_icmp_under_declared_all if {
	rc := ports_rule(lb_address, "icmp", 0, 0)
	messages([rc], [{"module": "module.lb", "ports": [{"from": 0, "to": 65535, "protocol": "-1"}]}]) == {undeclared_port(lb_address, "0.0.0.0/0", "icmp 0-0")}
}

test_violation_icmpv6_on_entrypoint if {
	rc := ports_rule(lb_address, "ICMPv6", 128, 0)
	messages([rc], lb) == {undeclared_port(lb_address, "0.0.0.0/0", "icmpv6 128-0")}
}

test_violation_icmp_port_unknown_on_entrypoint if {
	rc := change("aws_vpc_security_group_ingress_rule", lb_address, {"ip_protocol": "icmp", "cidr_ipv4": "0.0.0.0/0"}, {"from_port": true, "to_port": true})
	messages([rc], lb) == {undeclared_port(lb_address, "0.0.0.0/0", "icmp null-null")}
}

test_violation_open_ingress_ports_null if {
	rc := ports_rule(lb_address, "icmpv6", null, null)
	messages([rc], lb) == {undeclared_port(lb_address, "0.0.0.0/0", "icmpv6 null-null")}
}

test_violation_port_unknown_on_entrypoint if {
	rc := change("aws_vpc_security_group_ingress_rule", lb_address, {"ip_protocol": "tcp", "from_port": 443, "cidr_ipv4": "0.0.0.0/0"}, {"to_port": true})
	messages([rc], lb) == {ports_unknown(lb_address)}
}

test_violation_protocol_unknown_on_entrypoint if {
	rc := change("aws_security_group_rule", "module.lb.aws_security_group_rule.this", {"type": "ingress", "from_port": 443, "to_port": 443, "cidr_blocks": ["0.0.0.0/0"]}, {"protocol": true})
	messages([rc], lb) == {ports_unknown("module.lb.aws_security_group_rule.this")}
}

test_violation_port_and_cidr_unknown_on_entrypoint if {
	rc := change("aws_vpc_security_group_ingress_rule", lb_address, {"ip_protocol": "tcp", "to_port": 443}, {"from_port": true, "cidr_ipv4": true})
	messages([rc], lb) == {ports_unknown(lb_address)}
}

test_pass_port_unknown_on_entrypoint_with_limited_cidr if {
	rc := change("aws_vpc_security_group_ingress_rule", lb_address, {"ip_protocol": "tcp", "cidr_ipv4": "10.0.0.0/8"}, {"from_port": true, "to_port": true})
	count(messages([rc], lb)) == 0
}

test_pass_port_unknown_with_all_protocols_on_declared_all if {
	rc := change("aws_vpc_security_group_ingress_rule", lb_address, {"ip_protocol": "-1", "cidr_ipv4": "0.0.0.0/0"}, {"from_port": true, "to_port": true})
	count(messages([rc], [{"module": "module.lb", "ports": [{"from": 0, "to": 65535, "protocol": "-1"}]}])) == 0
}

test_violation_declaration_missing if {
	msgs := p.deny with input as fixture.plan([]) with data.policy_plan as {}
	msgs == {shape}
}

test_violation_declaration_null if {
	messages([], null) == {shape}
}

test_violation_declaration_object if {
	messages([], {}) == {shape}
}

test_violation_declaration_string_item if {
	messages([], ["module.lb"]) == {shape}
}

test_violation_declaration_mixed_items if {
	messages([], [lb[0], "module.x"]) == {shape}
}

test_violation_declaration_module_missing if {
	messages([], [{"ports": https}]) == {shape}
}

test_violation_declaration_module_empty if {
	messages([], [{"module": "", "ports": https}]) == {shape}
}

test_violation_declaration_module_not_module_address if {
	messages([], [{"module": lb_address, "ports": https}]) == {shape}
}

test_violation_declaration_module_trailing_dot if {
	messages([], [{"module": "module.lb.", "ports": https}]) == {shape}
}

test_violation_declaration_module_with_index_key if {
	messages([], [{"module": "module.lb[\"a\"]", "ports": https}]) == {shape}
}

test_violation_declaration_ports_missing if {
	messages([], [{"module": "module.lb"}]) == {shape}
}

test_violation_declaration_ports_empty if {
	messages([], [{"module": "module.lb", "ports": []}]) == {shape}
}

test_violation_declaration_port_not_object if {
	messages([], [{"module": "module.lb", "ports": [443]}]) == {shape}
}

test_violation_declaration_port_not_integer if {
	messages([], [{"module": "module.lb", "ports": [{"from": "443", "to": 443, "protocol": "tcp"}]}]) == {shape}
}

test_violation_declaration_port_fraction if {
	messages([], [{"module": "module.lb", "ports": [{"from": 443, "to": 443.5, "protocol": "tcp"}]}]) == {shape}
}

test_violation_declaration_port_protocol_missing if {
	messages([], [{"module": "module.lb", "ports": [{"from": 443, "to": 443}]}]) == {shape}
}

test_violation_declaration_port_protocol_empty if {
	messages([], [{"module": "module.lb", "ports": [{"from": 443, "to": 443, "protocol": ""}]}]) == {shape}
}

test_violation_declaration_port_extra_key if {
	messages([], [{"module": "module.lb", "ports": [{"from": 443, "to": 443, "protocol": "tcp", "cidr": "0.0.0.0/0"}]}]) == {shape}
}

test_violation_declaration_port_from_after_to if {
	messages([], [{"module": "module.lb", "ports": [{"from": 444, "to": 443, "protocol": "tcp"}]}]) == {shape}
}

test_violation_declaration_port_icmp if {
	messages([], [{"module": "module.lb", "ports": [{"from": 8, "to": 8, "protocol": "ICMP"}]}]) == {shape}
}

test_violation_declaration_port_icmpv6 if {
	messages([], [{"module": "module.lb", "ports": [{"from": 128, "to": 128, "protocol": "icmpv6"}]}]) == {shape}
}

test_violation_declaration_port_icmp_number if {
	messages([], [{"module": "module.lb", "ports": [{"from": 8, "to": 8, "protocol": "58"}]}]) == {shape}
}

test_violation_declaration_port_negative if {
	messages([], [{"module": "module.lb", "ports": [{"from": -1, "to": 443, "protocol": "tcp"}]}]) == {shape}
}

test_violation_declaration_port_above_range if {
	messages([], [{"module": "module.lb", "ports": [{"from": 443, "to": 65536, "protocol": "tcp"}]}]) == {shape}
}

test_pass_declaration_port_range_bounds if {
	rcs := [ports_rule(lb_address, "tcp", 0, 0), ports_rule("module.lb.aws_vpc_security_group_ingress_rule.high", "tcp", 65535, 65535)]
	count(messages(rcs, [{"module": "module.lb", "ports": [{"from": 0, "to": 0, "protocol": "tcp"}, {"from": 65535, "to": 65535, "protocol": "tcp"}]}])) == 0
}

test_violation_declaration_duplicate_port if {
	messages([], [{"module": "module.lb", "ports": [{"from": 0, "to": 65535, "protocol": "-1"}, {"from": 0, "to": 65535, "protocol": "ALL"}]}]) == {shape}
}

test_violation_declaration_duplicate_module if {
	messages([], [lb[0], {"module": "module.lb", "ports": [{"from": 80, "to": 80, "protocol": "tcp"}]}]) == {shape}
}

test_violation_declaration_extra_key if {
	messages([], [object.union(lb[0], {"address": lb_address})]) == {shape}
}

test_violation_malformed_declaration_exempts_nothing if {
	rc := vpc_rule(lb_address, "cidr_ipv4", "0.0.0.0/0")
	messages([rc], [lb[0], "module.x"]) == {shape, outside(lb_address, "0.0.0.0/0")}
}

test_violation_declared_entrypoint_absent if {
	messages([], lb) == {"[PLAN-SG-00] 公開点として宣言した module.lb の下の resource が入力にありません"}
}

test_violation_declared_entrypoint_only_deleted if {
	deleted := with_actions(vpc_rule(lb_address, "cidr_ipv4", "0.0.0.0/0"), ["delete"])
	messages([deleted], lb) == {"[PLAN-SG-00] 公開点として宣言した module.lb の下の resource が入力にありません"}
}

test_violation_declared_entrypoint_only_in_similar_module if {
	similar := vpc_rule("module.lb2.aws_vpc_security_group_ingress_rule.public", "cidr_ipv4", "10.0.0.0/8")
	messages([similar], lb) == {"[PLAN-SG-00] 公開点として宣言した module.lb の下の resource が入力にありません"}
}

test_pass_no_security_group if {
	count(messages([], [])) == 0
	count(messages([change("aws_s3_bucket", "module.x.aws_s3_bucket.this", {"bucket": "b"}, {})], [])) == 0
}

test_pass_empty_ingress if {
	count(messages([sg(sg_address, []), default_sg([])], [])) == 0
}

test_pass_ingress_absent if {
	count(messages([change("aws_security_group", sg_address, {"name": "s"}, {})], [])) == 0
}

test_pass_ingress_null if {
	count(messages([change("aws_security_group", sg_address, {"name": "s", "ingress": null}, {})], [])) == 0
}

sparse(unknown) := change("aws_security_group", sg_address, {"ingress": [block(["10.0.0.0/8"], []), block(["10.1.0.0/16"], [])]}, unknown)

test_pass_after_unknown_empty if {
	count(messages([sparse({})], [])) == 0
}

test_pass_after_unknown_ingress_empty_list if {
	count(messages([sparse({"ingress": []})], [])) == 0
}

test_pass_after_unknown_ingress_shorter if {
	count(messages([sparse({"ingress": [{}]})], [])) == 0
}

test_pass_after_unknown_ingress_false if {
	count(messages([sparse({"ingress": false})], [])) == 0
}

test_pass_after_unknown_ingress_false_elements if {
	count(messages([sparse({"ingress": [false, {"cidr_blocks": [false]}]})], [])) == 0
}

test_pass_limited_ingress if {
	rcs := [
		sg(sg_address, [block(["10.0.0.0/8"], ["2001:db8::/32"])]),
		sg_rule("ingress", ["10.0.0.0/16"], []),
		vpc_rule("module.x.aws_vpc_security_group_ingress_rule.v4", "cidr_ipv4", "10.0.0.0/16"),
		vpc_rule("module.x.aws_vpc_security_group_ingress_rule.v6", "cidr_ipv6", "2001:db8::/32"),
		change("aws_vpc_security_group_ingress_rule", "module.x.aws_vpc_security_group_ingress_rule.sg", {"ip_protocol": "tcp", "referenced_security_group_id": "sg-1"}, {"referenced_security_group_id": true}),
	]
	count(messages(rcs, [])) == 0
}

test_pass_prefix_lengths_ending_in_zero if {
	rcs := [
		vpc_rule("module.x.aws_vpc_security_group_ingress_rule.a", "cidr_ipv4", "10.0.0.0/10"),
		vpc_rule("module.x.aws_vpc_security_group_ingress_rule.b", "cidr_ipv4", "10.0.0.0/20"),
		vpc_rule("module.x.aws_vpc_security_group_ingress_rule.c", "cidr_ipv6", "2001:db8::1/120"),
		vpc_rule("module.x.aws_vpc_security_group_ingress_rule.d", "cidr_ipv6", "::/10"),
	]
	count(messages(rcs, [])) == 0
}

test_pass_egress_open if {
	egress := change("aws_vpc_security_group_egress_rule", "module.x.aws_vpc_security_group_egress_rule.this", {"ip_protocol": "-1", "cidr_ipv4": "0.0.0.0/0"}, {})
	count(messages([sg_rule("egress", ["0.0.0.0/0"], ["::/0"]), egress], [])) == 0
}

test_pass_inline_egress_open if {
	rc := change("aws_security_group", sg_address, {"ingress": [], "egress": [{"from_port": 0, "to_port": 0, "protocol": "-1", "cidr_blocks": ["0.0.0.0/0"], "ipv6_cidr_blocks": ["::/0"]}]}, {})
	count(messages([rc], [])) == 0
}

test_pass_deleted_open_ingress if {
	deleted := {"address": vpc_address, "type": "aws_vpc_security_group_ingress_rule", "change": {"actions": ["delete"], "after": null, "after_unknown": {}}}
	count(messages([deleted], [])) == 0
}

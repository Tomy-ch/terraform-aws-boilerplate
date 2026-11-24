# 全開放の ingress（接頭辞長が0の CIDR）は、宣言した公開点の module の、宣言した port に限る（ADR-0301 決定15、
# ADR-0402 決定6。conftest/plan/README.md の plan.security_group_ingress）。
package plan.security_group_ingress

import data.plan.lib

cfg := object.get(data.policy_plan, "security_group_ingress", {})

entrypoints := object.get(cfg, "public_entrypoints", null)

# ingress を block の list で持つ型。ほかの型は resource 自身が1つの規則である。
inline_types := {"aws_security_group", "aws_default_security_group"}

cidr_attrs := {
	"aws_security_group": ["cidr_blocks", "ipv6_cidr_blocks"],
	"aws_default_security_group": ["cidr_blocks", "ipv6_cidr_blocks"],
	"aws_security_group_rule": ["cidr_blocks", "ipv6_cidr_blocks"],
	"aws_vpc_security_group_ingress_rule": ["cidr_ipv4", "cidr_ipv6"],
}

protocol_attr := {
	"aws_security_group": "protocol",
	"aws_default_security_group": "protocol",
	"aws_security_group_rule": "protocol",
	"aws_vpc_security_group_ingress_rule": "ip_protocol",
}

well_formed if {
	is_array(entrypoints)
	every e in entrypoints {
		entrypoint_shape(e)
	}
	count({e.module | some e in entrypoints}) == count(entrypoints)
}

entrypoint_shape(e) if {
	is_object(e)
	object.keys(e) == {"module", "ports"}
	is_string(e.module)
	regex.match(`^module\.[A-Za-z0-9_-]+(\.module\.[A-Za-z0-9_-]+)*$`, e.module)
	is_array(e.ports)
	count(e.ports) > 0
	every p in e.ports {
		port_shape(p)
	}
	count({declared_port(p) | some p in e.ports}) == count(e.ports)
}

port_shape(p) if {
	is_object(p)
	object.keys(p) == {"from", "to", "protocol"}
	integer(p.from)
	integer(p.to)
	0 <= p.from
	p.from <= p.to
	p.to <= 65535
	protocol_of(p.protocol)
	not icmp(p.protocol)
}

integer(x) if {
	is_number(x)
	floor(x) == x
}

# "all" と "-1" はすべての protocol とすべての port を表す。
protocol_of(x) := "-1" if {
	is_string(x)
	lower(x) in {"-1", "all"}
} else := lower(x) if {
	is_string(x)
	x != ""
}

# ICMP の from_port・to_port は type と code であり port ではないので、宣言できず、覆われることもない。
icmp(x) if protocol_of(x) in {"icmp", "icmpv6", "1", "58"}

declared_port(p) := {"protocol": protocol_of(p.protocol), "from": p.from, "to": p.to}

deny contains msg if {
	not well_formed
	msg := "[PLAN-SG-00] data.policy_plan.security_group_ingress.public_entrypoints の宣言が欠けているか形が違います"
}

deny contains msg if {
	well_formed
	some e in entrypoints
	not declared_present(e.module)
	msg := sprintf("[PLAN-SG-00] 公開点として宣言した %s の下の resource が入力にありません", [e.module])
}

# module.lb は module.lb2 の下を覆わない。
under(address, module) if startswith(lib.strip_index(address), concat("", [module, "."]))

declared_present(module) if {
	some c in lib.changes
	under(c.rc.address, module)
}

valid_entrypoints := entrypoints if well_formed

default valid_entrypoints := []

declared_ports(c) := {declared_port(p) |
	some e in valid_entrypoints
	under(c.rc.address, e.module)
	some p in e.ports
}

public_entrypoint(c) if count(declared_ports(c)) > 0

ingress_rules contains {"c": c, "rule": rule, "unknown": unknown} if {
	some c in lib.changes
	c.rc.type in inline_types
	is_object(c.rc.change.after)
	blocks := object.get(c.rc.change.after, "ingress", [])
	is_array(blocks)
	some i, rule in blocks
	is_object(rule)
	unknown := block_unknown(object.get(after_unknown(c), "ingress", []), i)
}

after_unknown(c) := object.get(c.rc.change, "after_unknown", {})

block_unknown(blocks, i) := blocks[i] if {
	is_array(blocks)
	is_object(blocks[i])
} else := {}

ingress_rules contains rule_of(c) if {
	some c in lib.changes
	c.rc.type == "aws_vpc_security_group_ingress_rule"
	is_object(c.rc.change.after)
}

ingress_rules contains rule_of(c) if {
	some c in lib.changes
	c.rc.type == "aws_security_group_rule"
	is_object(c.rc.change.after)
	object.get(c.rc.change.after, "type", null) == "ingress"
}

rule_of(c) := {"c": c, "rule": c.rc.change.after, "unknown": after_unknown(c)}

open_cidr(cidr) if {
	is_string(cidr)
	regex.match(`/0+$`, cidr)
}

open_cidrs(r) := {cidr |
	some attr in cidr_attrs[r.c.rc.type]
	some cidr in lib.as_array(object.get(r.rule, attr, []))
	open_cidr(cidr)
}

cidr_unknown(r) := {attr |
	some attr in cidr_attrs[r.c.rc.type]
	lib.unknown_value(object.get(r.unknown, attr, false))
}

element_unknown(u, _) if u == true

element_unknown(u, i) if {
	is_array(u)
	u[i] == true
}

# plan で決まらない要素は null で表れるので、after_unknown が true の要素は除く。
cidr_malformed(r) := {attr |
	some attr in cidr_attrs[r.c.rc.type]
	v := object.get(r.rule, attr, null)
	v != null
	some i, x in lib.as_array(v)
	not is_string(x)
	not element_unknown(object.get(r.unknown, attr, false), i)
}

protocol_value(r) := object.get(r.rule, protocol_attr[r.c.rc.type], null)

all_protocols(r) if protocol_of(protocol_value(r)) == "-1"

# protocol が -1 の規則は、from_port と to_port に関わらず全 port を開ける。
rule_ports(r) := {"protocol": "-1", "from": 0, "to": 65535} if {
	all_protocols(r)
} else := {"protocol": protocol_of(protocol_value(r)), "from": object.get(r.rule, "from_port", null), "to": object.get(r.rule, "to_port", null)}

ports_unknown(r) if object.get(r.unknown, protocol_attr[r.c.rc.type], false) == true

ports_unknown(r) if {
	not all_protocols(r)
	not icmp(protocol_value(r))
	some attr in ["from_port", "to_port"]
	object.get(r.unknown, attr, false) == true
}

covered(r) if {
	ports := rule_ports(r)
	not icmp(ports.protocol)
	is_number(ports.from)
	is_number(ports.to)
	ports.from <= ports.to
	some p in declared_ports(r.c)
	p.protocol in {"-1", ports.protocol}
	p.from <= ports.from
	ports.to <= p.to
}

exempt(r) if {
	public_entrypoint(r.c)
	not ports_unknown(r)
	covered(r)
}

# 開いていたとしても通る規則は、CIDR が読めなくても落とさない。port が決まらない公開点の規則は別の文で落とす。
cidr_checked(r) if not public_entrypoint(r.c)

cidr_checked(r) if {
	public_entrypoint(r.c)
	not ports_unknown(r)
	not covered(r)
}

describe_ports(r) := sprintf("%s %v-%v", [p.protocol, p.from, p.to]) if {
	p := rule_ports(r)
} else := sprintf("%v %v-%v", [protocol_value(r), object.get(r.rule, "from_port", null), object.get(r.rule, "to_port", null)])

deny contains msg if {
	some r in ingress_rules
	not public_entrypoint(r.c)
	some cidr in open_cidrs(r)
	msg := sprintf("[PLAN-SG-01] %s: %s が %s からの ingress を許します。公開点として宣言していません", [r.c.path, r.c.rc.address, cidr])
}

deny contains msg if {
	some r in ingress_rules
	public_entrypoint(r.c)
	not ports_unknown(r)
	not covered(r)
	some cidr in open_cidrs(r)
	msg := sprintf("[PLAN-SG-01] %s: %s が %s からの ingress を、公開点として宣言していない port（%s）で許します", [r.c.path, r.c.rc.address, cidr, describe_ports(r)])
}

deny contains msg if {
	some r in ingress_rules
	public_entrypoint(r.c)
	ports_unknown(r)
	may_be_open(r)
	msg := sprintf("[PLAN-SG-02] %s: %s の port か protocol が plan で決まらないので、公開点として宣言した port の内かを確かめられません", [r.c.path, r.c.rc.address])
}

may_be_open(r) if count(open_cidrs(r)) > 0

may_be_open(r) if count(cidr_unknown(r)) > 0

may_be_open(r) if count(cidr_malformed(r)) > 0

deny contains msg if {
	some r in ingress_rules
	cidr_checked(r)
	some attr in cidr_unknown(r)
	msg := sprintf("[PLAN-SG-02] %s: %s の %s が plan で決まらないので、全開放でないことを確かめられません", [r.c.path, r.c.rc.address, attr])
}

deny contains msg if {
	some r in ingress_rules
	cidr_checked(r)
	some attr in cidr_malformed(r)
	msg := sprintf("[PLAN-SG-02] %s: %s の %s に文字列でない値があるので、全開放でないことを確かめられません", [r.c.path, r.c.rc.address, attr])
}

deny contains msg if {
	some c in lib.changes
	c.rc.type in inline_types
	ingress_unknown(c)
	msg := sprintf("[PLAN-SG-02] %s: %s の ingress が plan で決まらないので、全開放でないことを確かめられません", [c.path, c.rc.address])
}

ingress_unknown(c) if object.get(after_unknown(c), "ingress", false) == true

ingress_unknown(c) if {
	blocks := object.get(after_unknown(c), "ingress", false)
	is_array(blocks)
	some b in blocks
	b == true
}

deny contains msg if {
	some c in lib.changes
	c.rc.type in inline_types
	is_object(c.rc.change.after)
	ingress_malformed(c)
	msg := sprintf("[PLAN-SG-02] %s: %s の ingress が block の list の形でないので、全開放でないことを確かめられません", [c.path, c.rc.address])
}

ingress_malformed(c) if {
	blocks := object.get(c.rc.change.after, "ingress", null)
	blocks != null
	not is_array(blocks)
	object.get(after_unknown(c), "ingress", false) != true
}

ingress_malformed(c) if {
	blocks := object.get(c.rc.change.after, "ingress", null)
	is_array(blocks)
	some i, b in blocks
	not is_object(b)
	not element_unknown(object.get(after_unknown(c), "ingress", false), i)
}

deny contains msg if {
	some c in lib.changes
	c.rc.type == "aws_security_group_rule"
	object.get(after_unknown(c), "type", false) == true
	not exempt(rule_of(c))
	msg := sprintf("[PLAN-SG-02] %s: %s の type が plan で決まらないので、全開放でないことを確かめられません", [c.path, c.rc.address])
}

deny contains msg if {
	some c in lib.changes
	c.rc.type == "aws_security_group_rule"
	is_object(c.rc.change.after)
	object.get(after_unknown(c), "type", false) != true
	not is_string(object.get(c.rc.change.after, "type", null))
	not exempt(rule_of(c))
	msg := sprintf("[PLAN-SG-02] %s: %s の type が文字列でないので、ingress かを確かめられません", [c.path, c.rc.address])
}

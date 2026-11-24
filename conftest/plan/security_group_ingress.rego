# 全開放の ingress（接頭辞長が0の CIDR）は、宣言した公開点に限る（ADR-0301 決定15。conftest/plan/README.md の
# plan.security_group_ingress）。
package plan.security_group_ingress

import data.plan.lib

cfg := object.get(data.policy_plan, "security_group_ingress", {})

# ingress を block の list で持つ型。ほかの型は resource 自身が1つの規則である。
inline_types := {"aws_security_group", "aws_default_security_group"}

cidr_attrs := {
	"aws_security_group": ["cidr_blocks", "ipv6_cidr_blocks"],
	"aws_default_security_group": ["cidr_blocks", "ipv6_cidr_blocks"],
	"aws_security_group_rule": ["cidr_blocks", "ipv6_cidr_blocks"],
	"aws_vpc_security_group_ingress_rule": ["cidr_ipv4", "cidr_ipv6"],
}

well_formed if {
	is_array(object.get(cfg, "public_entrypoints", null))
	every a in cfg.public_entrypoints {
		is_string(a)
		a != ""
	}
}

deny contains msg if {
	not well_formed
	msg := "[PLAN-SG-00] data.policy_plan.security_group_ingress.public_entrypoints の宣言が欠けているか形が違います"
}

deny contains msg if {
	well_formed
	some a in cfg.public_entrypoints
	not declared_present(a)
	msg := sprintf("[PLAN-SG-00] 公開点として宣言した %s が入力にありません", [a])
}

declared_present(a) if {
	some c in lib.changes
	lib.strip_index(c.rc.address) == a
}

public_entrypoint(c) if lib.strip_index(c.rc.address) in object.get(cfg, "public_entrypoints", [])

ingress_rules contains {"c": c, "rule": rule, "unknown": unknown} if {
	some c in lib.changes
	c.rc.type in inline_types
	is_object(c.rc.change.after)
	blocks := object.get(c.rc.change.after, "ingress", [])
	is_array(blocks)
	some i, rule in blocks
	is_object(rule)
	unknown := block_unknown(object.get(object.get(c.rc.change, "after_unknown", {}), "ingress", []), i)
}

block_unknown(blocks, i) := blocks[i] if {
	is_array(blocks)
	is_object(blocks[i])
} else := {}

ingress_rules contains {"c": c, "rule": c.rc.change.after, "unknown": object.get(c.rc.change, "after_unknown", {})} if {
	some c in lib.changes
	c.rc.type == "aws_vpc_security_group_ingress_rule"
	is_object(c.rc.change.after)
}

ingress_rules contains {"c": c, "rule": c.rc.change.after, "unknown": object.get(c.rc.change, "after_unknown", {})} if {
	some c in lib.changes
	c.rc.type == "aws_security_group_rule"
	is_object(c.rc.change.after)
	object.get(c.rc.change.after, "type", null) == "ingress"
}

open_cidr(cidr) if {
	is_string(cidr)
	endswith(cidr, "/0")
}

deny contains msg if {
	some r in ingress_rules
	not public_entrypoint(r.c)
	some attr in cidr_attrs[r.c.rc.type]
	some cidr in lib.as_array(object.get(r.rule, attr, []))
	open_cidr(cidr)
	msg := sprintf("[PLAN-SG-01] %s: %s が %s からの ingress を許します。公開点として宣言していません", [r.c.path, r.c.rc.address, cidr])
}

# ingress の block の list 自体か、規則の CIDR が plan で決まらなければ、全開放でないことを確かめられない。
deny contains msg if {
	some c in lib.changes
	c.rc.type in inline_types
	not public_entrypoint(c)
	object.get(object.get(c.rc.change, "after_unknown", {}), "ingress", false) == true
	msg := sprintf("[PLAN-SG-02] %s: %s の ingress が plan で決まらないので、全開放でないことを確かめられません", [c.path, c.rc.address])
}

deny contains msg if {
	some r in ingress_rules
	not public_entrypoint(r.c)
	some attr in cidr_attrs[r.c.rc.type]
	lib.unknown_value(object.get(r.unknown, attr, false))
	msg := sprintf("[PLAN-SG-02] %s: %s の %s が plan で決まらないので、全開放でないことを確かめられません", [r.c.path, r.c.rc.address, attr])
}

# aws_security_group_rule の type が決まらなければ、ingress かどうかを確かめられない。
deny contains msg if {
	some c in lib.changes
	c.rc.type == "aws_security_group_rule"
	not public_entrypoint(c)
	object.get(object.get(c.rc.change, "after_unknown", {}), "type", false) == true
	msg := sprintf("[PLAN-SG-02] %s: %s の type が plan で決まらないので、全開放でないことを確かめられません", [c.path, c.rc.address])
}

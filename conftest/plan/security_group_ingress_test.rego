package plan.security_group_ingress_test

import data.plan.fixture
import data.plan.security_group_ingress as p

decl(entrypoints) := {"security_group_ingress": {"public_entrypoints": entrypoints}}

change(type, address, after, unknown) := {"address": address, "type": type, "change": {"actions": ["create"], "after": after, "after_unknown": unknown}}

block(v4, v6) := {"from_port": 443, "to_port": 443, "protocol": "tcp", "cidr_blocks": v4, "ipv6_cidr_blocks": v6, "security_groups": []}

sg(address, blocks) := change("aws_security_group", address, {"name": "s", "ingress": blocks}, {})

default_sg(blocks) := change("aws_default_security_group", "module.v.aws_default_security_group.this", {"ingress": blocks}, {})

sg_rule(type, v4, v6) := change("aws_security_group_rule", "module.x.aws_security_group_rule.this", {"type": type, "from_port": 443, "to_port": 443, "protocol": "tcp", "cidr_blocks": v4, "ipv6_cidr_blocks": v6}, {})

vpc_rule(address, attr, cidr) := change("aws_vpc_security_group_ingress_rule", address, {"ip_protocol": "tcp", "from_port": 443, "to_port": 443, attr: cidr}, {})

messages(rcs, entrypoints) := msgs if {
	msgs := p.deny with input as fixture.plan(rcs) with data.policy_plan as decl(entrypoints)
}

open_ingress_cases := [
	sg("module.x.aws_security_group.this", [block(["10.0.0.0/8", "0.0.0.0/0"], [])]),
	sg("module.x.aws_security_group.this", [block([], ["::/0"])]),
	default_sg([block(["0.0.0.0/0"], [])]),
	sg_rule("ingress", ["0.0.0.0/0"], []),
	sg_rule("ingress", [], ["::/0"]),
	vpc_rule("module.x.aws_vpc_security_group_ingress_rule.this", "cidr_ipv4", "0.0.0.0/0"),
	vpc_rule("module.x.aws_vpc_security_group_ingress_rule.this", "cidr_ipv6", "::/0"),
]

test_violation_open_ingress_outside_entrypoint if {
	every rc in open_ingress_cases {
		msgs := messages([rc], [])
		{m | some m in msgs; startswith(m, "[PLAN-SG-01]")} == {sprintf("[PLAN-SG-01] p.json: %s が %s からの ingress を許します。公開点として宣言していません", [rc.address, cidr]) | some cidr in ["0.0.0.0/0", "::/0"]; contains(json.marshal(rc.change.after), sprintf("\"%s\"", [cidr]))}
		count(msgs) == 1
	}
}

test_violation_open_ingress_in_second_block if {
	rc := sg("module.x.aws_security_group.this", [block(["10.0.0.0/8"], []), block(["0.0.0.0/0"], [])])
	fixture.has("[PLAN-SG-01] p.json: module.x.aws_security_group.this が 0.0.0.0/0", messages([rc], []))
}

test_violation_other_entrypoint_declared if {
	open := vpc_rule("module.x.aws_vpc_security_group_ingress_rule.this", "cidr_ipv4", "0.0.0.0/0")
	lb := vpc_rule("module.lb.aws_vpc_security_group_ingress_rule.public", "cidr_ipv4", "0.0.0.0/0")
	msgs := messages([open, lb], ["module.lb.aws_vpc_security_group_ingress_rule.public"])
	{m | some m in msgs} == {"[PLAN-SG-01] p.json: module.x.aws_vpc_security_group_ingress_rule.this が 0.0.0.0/0 からの ingress を許します。公開点として宣言していません"}
}

test_violation_cidr_unknown if {
	rule := change("aws_vpc_security_group_ingress_rule", "module.x.aws_vpc_security_group_ingress_rule.this", {"ip_protocol": "tcp"}, {"cidr_ipv4": true})
	inline := change("aws_security_group", "module.x.aws_security_group.this", {"ingress": [block(null, [])]}, {"ingress": [{"cidr_blocks": true}]})
	legacy := change("aws_security_group_rule", "module.x.aws_security_group_rule.this", {"type": "ingress", "cidr_blocks": [null]}, {"cidr_blocks": [true]})
	whole := change("aws_default_security_group", "module.v.aws_default_security_group.this", {}, {"ingress": true})
	type_unknown := change("aws_security_group_rule", "module.y.aws_security_group_rule.this", {"cidr_blocks": ["0.0.0.0/0"]}, {"type": true})
	msgs := messages([rule, inline, legacy, whole, type_unknown], [])
	{m | some m in msgs} == {
		"[PLAN-SG-02] p.json: module.x.aws_vpc_security_group_ingress_rule.this の cidr_ipv4 が plan で決まらないので、全開放でないことを確かめられません",
		"[PLAN-SG-02] p.json: module.x.aws_security_group.this の cidr_blocks が plan で決まらないので、全開放でないことを確かめられません",
		"[PLAN-SG-02] p.json: module.x.aws_security_group_rule.this の cidr_blocks が plan で決まらないので、全開放でないことを確かめられません",
		"[PLAN-SG-02] p.json: module.v.aws_default_security_group.this の ingress が plan で決まらないので、全開放でないことを確かめられません",
		"[PLAN-SG-02] p.json: module.y.aws_security_group_rule.this の type が plan で決まらないので、全開放でないことを確かめられません",
	}
}

test_violation_declaration_missing if {
	msgs := p.deny with input as fixture.plan([]) with data.policy_plan as {}
	msgs == {"[PLAN-SG-00] data.policy_plan.security_group_ingress.public_entrypoints の宣言が欠けているか形が違います"}
}

test_violation_declaration_malformed if {
	every entrypoints in [null, "module.lb.aws_vpc_security_group_ingress_rule.public", [""], [1]] {
		fixture.has("[PLAN-SG-00] data.policy_plan.security_group_ingress.public_entrypoints", messages([], entrypoints))
	}
}

test_violation_declared_entrypoint_absent if {
	messages([], ["module.lb.aws_vpc_security_group_ingress_rule.public"]) == {"[PLAN-SG-00] 公開点として宣言した module.lb.aws_vpc_security_group_ingress_rule.public が入力にありません"}
}

test_pass_no_security_group if {
	count(messages([], [])) == 0
	count(messages([change("aws_s3_bucket", "module.x.aws_s3_bucket.this", {"bucket": "b"}, {})], [])) == 0
}

test_pass_empty_ingress if {
	count(messages([sg("module.x.aws_security_group.this", []), default_sg([])], [])) == 0
}

test_pass_limited_ingress if {
	rcs := [
		sg("module.x.aws_security_group.this", [block(["10.0.0.0/8"], ["2001:db8::/32"])]),
		sg_rule("ingress", ["10.0.0.0/16"], []),
		vpc_rule("module.x.aws_vpc_security_group_ingress_rule.v4", "cidr_ipv4", "10.0.0.0/16"),
		vpc_rule("module.x.aws_vpc_security_group_ingress_rule.v6", "cidr_ipv6", "2001:db8::/32"),
		change("aws_vpc_security_group_ingress_rule", "module.x.aws_vpc_security_group_ingress_rule.sg", {"ip_protocol": "tcp", "referenced_security_group_id": "sg-1"}, {"referenced_security_group_id": true}),
	]
	count(messages(rcs, [])) == 0
}

test_pass_egress_open if {
	egress := change("aws_vpc_security_group_egress_rule", "module.x.aws_vpc_security_group_egress_rule.this", {"ip_protocol": "-1", "cidr_ipv4": "0.0.0.0/0"}, {})
	count(messages([sg_rule("egress", ["0.0.0.0/0"], ["::/0"]), egress], [])) == 0
}

test_pass_open_ingress_on_declared_entrypoint if {
	rcs := [
		vpc_rule("module.lb.aws_vpc_security_group_ingress_rule.public[\"v4\"]", "cidr_ipv4", "0.0.0.0/0"),
		vpc_rule("module.lb.aws_vpc_security_group_ingress_rule.public[\"v6\"]", "cidr_ipv6", "::/0"),
	]
	count(messages(rcs, ["module.lb.aws_vpc_security_group_ingress_rule.public"])) == 0
}

test_pass_deleted_open_ingress if {
	deleted := {"address": "module.x.aws_vpc_security_group_ingress_rule.this", "type": "aws_vpc_security_group_ingress_rule", "change": {"actions": ["delete"], "after": null, "after_unknown": {}}}
	count(messages([deleted], [])) == 0
}

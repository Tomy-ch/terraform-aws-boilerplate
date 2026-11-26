package plan.ram_resource_share_test

import data.plan.fixture
import data.plan.ram_resource_share as gate

address := "module.o.module.account_ids.aws_ram_resource_share.this"

share(after, unknown) := {"address": address, "type": "aws_ram_resource_share", "change": {"actions": ["create"], "after": after, "after_unknown": unknown}}

messages(rcs) := msgs if {
	msgs := gate.deny with input as fixture.plan(rcs)
}

test_pass_internal_only if {
	count(messages([share({"name": "tabp-account-ids", "allow_external_principals": false}, {})])) == 0
}

test_pass_without_shares if {
	count(messages([])) == 0
}

test_pass_deleted_share if {
	rc := {"address": address, "type": "aws_ram_resource_share", "change": {"actions": ["delete"], "after": null, "after_unknown": {}}}
	count(messages([rc])) == 0
}

test_violation_external_principals if {
	fixture.has("[PLAN-RAM-01] p.json: module.o.module.account_ids.aws_ram_resource_share.this の allow_external_principals が plan で false に決まりません", messages([share({"allow_external_principals": true}, {})]))
}

test_violation_unknown if {
	fixture.has("[PLAN-RAM-01] p.json: module.o.module.account_ids.aws_ram_resource_share.this の allow_external_principals が plan で false に決まりません", messages([share({}, {"allow_external_principals": true})]))
}

test_violation_omitted if {
	fixture.has("[PLAN-RAM-01]", messages([share({"name": "x"}, {})]))
}

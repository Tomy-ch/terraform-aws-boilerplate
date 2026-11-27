package plan.deployment_identity_instances_test

import data.plan.deployment_identity_apply_role_test as apply_fixture
import data.plan.deployment_identity_instances as p
import data.plan.fixture
import data.plan.lib

member := {"name": "m", "state_backend": "module.state_backend_m.", "deployment_identity": "module.deployment_identity_m.", "deployment_identity_root": "di"}

decl := object.union(data.policy_plan, {"state_backend": object.union(data.policy_plan.state_backend, {"instances": {"sb": [member]}})})

file(path, rcs) := {"path": path, "contents": {"plan_format_version": "1.2", "resource_changes": rcs}}

moved(rc) := object.union(rc, {"address": concat("", [member.deployment_identity, substring(rc.address, count("module.deployment_identity."), -1)])}) if startswith(rc.address, "module.deployment_identity.")

moved(rc) := object.union(rc, {"address": concat("", [member.state_backend, substring(rc.address, count("module.state_backend."), -1)])}) if startswith(rc.address, "module.state_backend.")

moved(rc) := rc if {
	not startswith(rc.address, "module.deployment_identity.")
	not startswith(rc.address, "module.state_backend.")
}

# deployment-identity の側の resource の集まり（apply の role のテストの world）。正規の接頭辞の下と外の resource を持つ。
rcs := apply_fixture.world(apply_fixture.valid_statements)

rogue := {"address": "module.audit_evidence.aws_iam_role_policy.rogue", "type": "aws_iam_role_policy", "change": {"actions": ["create"], "after": {"name": "rogue", "role": "ci-apply", "policy": json.marshal({"Statement": [{"Effect": "Allow", "Action": "s3:GetObject", "Resource": "arn:aws:s3:::x/*"}]})}, "after_unknown": {}}}

canonical_input(extra) := [file("sb.json", []), file("di.json", array.concat(rcs, extra))]

member_input(extra) := [file("sb.json", []), file("di.json", array.concat([moved(rc) | some rc in rcs], extra))]

messages(inp) := msgs if {
	msgs := p.deny with input as inp with data.policy_plan as decl
}

# package を変数で引くと、このテスト自身の package を含んで再帰になるので、package ごとに書く。
direct(inp) := msgs if {
	md := lib.member_decl with data.policy_plan as decl
	trust := data.plan.deployment_identity_trust.deny with input as inp with data.policy_plan as md with data.plan.lib.member_form as true
	plan_role := data.plan.deployment_identity_plan_role.deny with input as inp with data.policy_plan as md with data.plan.lib.member_form as true
	apply_role := data.plan.deployment_identity_apply_role.deny with input as inp with data.policy_plan as md with data.plan.lib.member_form as true
	aio := data.plan.deployment_identity_apply_identity_output.deny with input as inp with data.policy_plan as md with data.plan.lib.member_form as true
	unlock := data.plan.deployment_identity_unlock_role.deny with input as inp with data.policy_plan as md with data.plan.lib.member_form as true
	assume := data.plan.deployment_identity_bootstrap_assume.deny with input as inp with data.policy_plan as md with data.plan.lib.member_form as true
	awp := data.plan.deployment_identity_workload_policy.deny with input as inp with data.policy_plan as md with data.plan.lib.member_form as true
	region := data.plan.deployment_identity_region_deny.deny with input as inp with data.policy_plan as md with data.plan.lib.member_form as true
	inventory := data.plan.deployment_identity_policy_inventory.deny with input as inp with data.policy_plan as md with data.plan.lib.member_form as true
	all := ((((((((trust | plan_role) | apply_role) | aio) | unlock) | assume) | awp) | region) | inventory)
	msgs := {sprintf("[PLAN-DIM-01] instance m: %s", [m]) | some m in all}
}

test_pass_member_instance_is_evaluated_like_the_canonical_instance if {
	got := {m | some m in messages(member_input([])); startswith(m, "[PLAN-DIM-01]")}
	want := direct(canonical_input([]))
	count(want) > 0
	got == want
}

test_violation_policy_attached_from_outside_the_member_module if {
	fixture.has("[PLAN-DIM-01] instance m: [PLAN-APPLY-05] di.json: module.audit_evidence.aws_iam_role_policy.rogue が apply の role に policy を付けています", messages(member_input([rogue])))
}

# 正規の instance の resource は付け替えた入力に入らない。正規の instance の違反を二重に報告しない。
test_pass_canonical_resources_are_not_reported_again if {
	inp := [file("sb.json", []), file("di.json", array.concat(rcs, [moved(rc) | some rc in rcs]))]
	view := lib.member_view(member) with data.policy_plan as decl with input as inp
	every rc in view[1].contents.resource_changes {
		not startswith(rc.address, member.deployment_identity)
		not startswith(rc.address, member.state_backend)
	}
	{rc.address | some rc in view[1].contents.resource_changes} == {rc.address | some rc in rcs}
	count({rc | some rc in view[1].contents.resource_changes; startswith(rc.address, "module.deployment_identity.")}) == count({rc | some rc in rcs; startswith(rc.address, "module.deployment_identity.")})
}

test_violation_member_instance_missing if {
	fixture.has("[PLAN-DIM-00] 入力に、宣言したメンバーの形の instance", messages([file("di.json", rcs)]))
	fixture.has("[PLAN-DIM-00] 入力に、宣言したメンバーの形の instance", p.deny) with input as [file("sb.json", []), file("di.json", rcs)] with data.policy_plan as json.patch(decl, [{"op": "remove", "path": "/state_backend/instances"}])
}

test_violation_member_instance_in_management_shape if {
	governance := object.union(apply_fixture.governance_policy_rc, {"address": data.policy_plan.deployment_identity.apply_role.governance.management_policy})
	fixture.has("[PLAN-DIM-00] instance m が管理アカウントの形です", messages(member_input([moved(governance)])))
}

test_violation_management_only_policies_malformed if {
	every v in [null, [], "x", [5], ["module.x.aws_iam_policy.a"], ["module.deployment_identity.a", "module.deployment_identity.a"]] {
		d := json.patch(decl, [{"op": "add", "path": "/deployment_identity/management_only_policies", "value": v}])
		fixture.has("[PLAN-DIM-00] 宣言 deployment_identity.management_only_policies が", p.deny) with input as member_input([]) with data.policy_plan as d
	}
}

test_violation_management_only_policy_absent_from_canonical if {
	d := json.patch(decl, [{"op": "add", "path": "/deployment_identity/management_only_policies/-", "value": "module.deployment_identity.module.github_oidc.aws_iam_policy.other[0]"}])
	fixture.has("[PLAN-DIM-00] 宣言 deployment_identity.management_only_policies の module.deployment_identity.module.github_oidc.aws_iam_policy.other[0] が", p.deny) with input as member_input([]) with data.policy_plan as d
}

test_violation_member_form_requirement_not_declared if {
	d := json.patch(decl, [{"op": "replace", "path": "/deployment_identity/bootstrap_assume/apply_identity_policy", "value": "module.deployment_identity.module.github_oidc.aws_iam_policy.manage"}])
	fixture.has("[PLAN-DIM-00] 宣言 deployment_identity.bootstrap_assume.apply_identity_policy", p.deny) with input as member_input([]) with data.policy_plan as d
}

# 差分のケースの world で違反を出さない package も、メンバーの instance の違反を報告する。
member_address(suffix) := concat("", [member.deployment_identity, suffix])

test_violation_member_trust if {
	address := member_address("module.github_oidc.aws_iam_role.this[\"extra\"]")
	s := {"Effect": "Allow", "Action": "sts:AssumeRoleWithWebIdentity", "Principal": {"Federated": "x"}}
	trust := fixture.doc("aws_iam_role", address, "assume_role_policy", {"Statement": [s, s]})
	fixture.has("[PLAN-DIM-01] instance m: [PLAN-TRUST-01] di.json: module.deployment_identity.module.github_oidc.aws_iam_role.this[\"extra\"] の信頼条件は statement を1つだけ持ちます", messages(member_input([trust])))
	not fixture.has("[PLAN-DIM-01] instance m: [PLAN-TRUST-01]", messages(member_input([])))
}

test_violation_member_assume if {
	doc := fixture.doc("aws_iam_policy", member_address("module.github_oidc.aws_iam_policy.manage"), "policy", {"Statement": [{"Effect": "Allow", "Action": "sts:AssumeRole", "Resource": "arn:aws:iam::111111111111:role/OrganizationAccountAccessRole"}]})
	fixture.has("[PLAN-DIM-01] instance m: [PLAN-ASSUME-03] di.json: module.deployment_identity.module.github_oidc.aws_iam_policy.manage が sts:AssumeRole・sts:TagSession・sts:SetSourceIdentity のどれかを許します。メンバーアカウントの deployment-identity は", messages(member_input([doc])))
	not fixture.has("[PLAN-DIM-01] instance m: [PLAN-ASSUME-03]", messages(member_input([])))
}

test_violation_member_policy_inventory if {
	rogue_policy := fixture.doc("aws_iam_policy", member_address("module.github_oidc.aws_iam_policy.rogue"), "policy", {"Statement": []})
	fixture.has("[PLAN-DIM-01] instance m: [PLAN-CMP-01] di.json: module.deployment_identity.module.github_oidc.aws_iam_policy.rogue が deployment_identity.customer_managed_policies の宣言にありません", messages(member_input([rogue_policy])))
}

# メンバーの形の評価が外す要求の根拠は、2つとも宣言に在ることを求める。
test_violation_member_form_requirement_governance_not_declared if {
	every patch in [
		{"op": "replace", "path": "/deployment_identity/apply_role/governance/management_policy", "value": "module.deployment_identity.module.github_oidc.aws_iam_policy.manage"},
		{"op": "remove", "path": "/deployment_identity/apply_role/governance/management_policy"},
	] {
		d := json.patch(decl, [patch])
		fixture.has("[PLAN-DIM-00] 宣言 deployment_identity.apply_role.governance.management_policy", p.deny) with input as member_input([]) with data.policy_plan as d
	}
}

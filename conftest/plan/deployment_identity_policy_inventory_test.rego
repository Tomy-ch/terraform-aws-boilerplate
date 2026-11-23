package plan.deployment_identity_policy_inventory_test

import data.plan.deployment_identity_policy_inventory as p
import data.plan.fixture

g := "module.deployment_identity.module.github_oidc."

policy(name) := fixture.doc("aws_iam_policy", sprintf("%saws_iam_policy.%s", [g, name]), "policy", {"Statement": []})

declared := [sprintf("%saws_iam_policy.%s", [g, n]) | some n in ["boundary", "governance", "manage"]]

# 宣言は丸ごと差し替える（理由は lib/fixture.rego の inline_declaration）。
messages_for(rcs, d) := msgs if {
	msgs := p.deny with input as fixture.plan(rcs) with data.policy_plan as object.union(object.remove(data.policy_plan, ["deployment_identity"]), {"deployment_identity": {"customer_managed_policies": d}})
}

valid := [policy("boundary[\"ops-notification\"]"), policy("governance[0]"), policy("manage")]

test_pass_declared_policies_match if {
	count(messages_for(valid, declared)) == 0
}

test_pass_policy_outside_declared_module if {
	count(messages_for(array.concat(valid, [fixture.doc("aws_iam_policy", "module.ops_notification.aws_iam_policy.this", "policy", {"Statement": []})]), declared)) == 0
}

test_pass_deleted_policy_not_counted if {
	deleted := object.union(policy("old"), {"change": {"actions": ["delete"]}})
	count(messages_for(array.concat(valid, [deleted]), declared)) == 0
}

test_violation_undeclared_policy if {
	fixture.has("[PLAN-CMP-01] p.json: module.deployment_identity.module.github_oidc.aws_iam_policy.extra が deployment_identity.customer_managed_policies の宣言にありません", messages_for(array.concat(valid, [policy("extra[0]")]), declared))
}

test_violation_declared_policy_missing if {
	fixture.has("[PLAN-CMP-01] deployment_identity.customer_managed_policies の module.deployment_identity.module.github_oidc.aws_iam_policy.manage が、aws_iam_policy として入力にありません", messages_for([policy("boundary[\"ops-notification\"]"), policy("governance[0]")], declared))
}

test_violation_declaration_malformed if {
	every d in [null, [], "x", [1], [""], [sprintf("%saws_iam_policy.manage[0]", [g])], [sprintf("%saws_iam_role.manage", [g])], ["aws_iam_policy.manage"]] {
		fixture.has("[PLAN-CMP-00]", messages_for(valid, d))
	}
}

package plan.deployment_identity_trust_test

import data.plan.deployment_identity_trust as p
import data.plan.fixture

provider_arn := "arn:aws:iam::123456789012:oidc-provider/token.actions.githubusercontent.com"

provider := {
	"address": "module.deployment_identity.module.github_oidc.aws_iam_openid_connect_provider.github",
	"type": "aws_iam_openid_connect_provider",
	"change": {"actions": ["create"], "after": {"arn": provider_arn}, "after_unknown": {}},
}

eq := {
	"token.actions.githubusercontent.com:aud": "sts.amazonaws.com",
	"token.actions.githubusercontent.com:sub": "repo:octo-org@123456/octo-repo@456789:environment:admin-plan",
	"token.actions.githubusercontent.com:repository_id": "456789",
	"token.actions.githubusercontent.com:repository_owner_id": "123456",
}

statement(condition) := {
	"Effect": "Allow",
	"Action": "sts:AssumeRoleWithWebIdentity",
	"Principal": {"Federated": provider_arn},
	"Condition": condition,
}

# 入れ子 module と index key 付きの address を、data の index 無しの address と照合する。
role(statements) := fixture.doc("aws_iam_role", "module.deployment_identity.module.github_oidc.aws_iam_role.this[\"plan\"]", "assume_role_policy", {"Statement": statements})

trust(statements) := fixture.plan([provider, role(statements)])

test_pass_immutable_environment_subject if {
	count(p.deny) == 0 with input as trust([statement({"StringEquals": eq})])
}

test_violation_no_subject if {
	fixture.has("[PLAN-TRUST-00]", p.deny) with input as fixture.plan([provider])
}

test_violation_two_statements if {
	fixture.has("[PLAN-TRUST-01]", p.deny) with input as trust([statement({"StringEquals": eq}), statement({"StringEquals": eq})])
}

test_violation_other_action if {
	s := object.union(statement({"StringEquals": eq}), {"Action": ["sts:AssumeRoleWithWebIdentity", "sts:TagSession"]})
	fixture.has("[PLAN-TRUST-02]", p.deny) with input as trust([s])
}

test_violation_principal_not_provider if {
	s := object.union(statement({"StringEquals": eq}), {"Principal": {"Federated": "arn:aws:iam::123456789012:oidc-provider/other"}})
	fixture.has("[PLAN-TRUST-03]", p.deny) with input as trust([s])
}

test_violation_provider_arn_unknown if {
	unknown := {"address": provider.address, "type": provider.type, "change": {"actions": ["create"], "after": {}, "after_unknown": {"arn": true}}}
	fixture.has("[PLAN-TRUST-03]", p.deny) with input as fixture.plan([unknown, role([statement({"StringEquals": eq})])])
}

test_violation_string_like if {
	fixture.has("[PLAN-TRUST-04]", p.deny) with input as trust([statement({"StringEquals": eq, "StringLike": {"token.actions.githubusercontent.com:sub": "repo:octo-org/*"}})])
}

test_violation_missing_condition if {
	s := object.remove(statement({}), ["Condition"])
	msgs := p.deny with input as trust([s])
	fixture.has("[PLAN-TRUST-04]", msgs)
	fixture.has("[PLAN-TRUST-07]", msgs)
	fixture.has("[PLAN-TRUST-08]", msgs)
}

test_violation_missing_key if {
	fixture.has("[PLAN-TRUST-05]", p.deny) with input as trust([statement({"StringEquals": object.remove(eq, ["token.actions.githubusercontent.com:repository_id"])})])
}

test_violation_extra_key if {
	fixture.has("[PLAN-TRUST-05]", p.deny) with input as trust([statement({"StringEquals": object.union(eq, {"token.actions.githubusercontent.com:ref": "refs/heads/main"})})])
}

test_violation_wildcard_value if {
	fixture.has("[PLAN-TRUST-06]", p.deny) with input as trust([statement({"StringEquals": object.union(eq, {"token.actions.githubusercontent.com:sub": "repo:octo-org@123456/octo-repo@456789:environment:*"})})])
}

test_violation_multiple_values if {
	fixture.has("[PLAN-TRUST-06]", p.deny) with input as trust([statement({"StringEquals": object.union(eq, {"token.actions.githubusercontent.com:aud": ["sts.amazonaws.com", "other"]})})])
}

test_violation_audience if {
	fixture.has("[PLAN-TRUST-07]", p.deny) with input as trust([statement({"StringEquals": object.union(eq, {"token.actions.githubusercontent.com:aud": "api://AzureADTokenExchange"})})])
}

test_violation_ref_subject if {
	fixture.has("[PLAN-TRUST-08]", p.deny) with input as trust([statement({"StringEquals": object.union(eq, {"token.actions.githubusercontent.com:sub": "repo:octo-org@123456/octo-repo@456789:ref:refs/heads/main"})})])
}

test_violation_legacy_subject if {
	fixture.has("[PLAN-TRUST-08]", p.deny) with input as trust([statement({"StringEquals": object.union(eq, {"token.actions.githubusercontent.com:sub": "repo:octo-org/octo-repo:environment:admin-plan"})})])
}

test_violation_subject_id_mismatch if {
	fixture.has("[PLAN-TRUST-08]", p.deny) with input as trust([statement({"StringEquals": object.union(eq, {"token.actions.githubusercontent.com:repository_id": "999"})})])
}

test_pass_every_instance if {
	roles := [fixture.doc("aws_iam_role", sprintf("module.deployment_identity.module.github_oidc.aws_iam_role.this[%q]", [k]), "assume_role_policy", {"Statement": [statement({"StringEquals": object.union(eq, {"token.actions.githubusercontent.com:sub": sprintf("repo:octo-org@123456/octo-repo@456789:environment:admin-%s", [k])})})]}) | some k in ["plan", "apply", "apply-identity"]]
	count(p.deny) == 0 with input as fixture.plan(array.concat([provider], roles))
}

test_violation_declared_role_absent if {
	msgs := p.deny with input as trust([statement({"StringEquals": eq})]) with data.policy_plan.deployment_identity.roles as ["module.deployment_identity.module.github_oidc.aws_iam_role.this", "module.nope.aws_iam_role.x"]
	fixture.has("[PLAN-TRUST-00] 信頼条件の対象 module.nope.aws_iam_role.x ", msgs)
}

test_pass_undeclared_role_not_inspected if {
	other := fixture.doc("aws_iam_role", "module.other.aws_iam_role.this", "assume_role_policy", {"Statement": [{"Effect": "Allow", "Action": "sts:AssumeRole", "Principal": {"Service": "ec2.amazonaws.com"}}]})
	count(p.deny) == 0 with input as fixture.plan([provider, role([statement({"StringEquals": eq})]), other])
}

test_violation_zero_statements if {
	fixture.has("[PLAN-TRUST-01]", p.deny) with input as trust([])
}

test_violation_effect_deny if {
	fixture.has("[PLAN-TRUST-02]", p.deny) with input as trust([object.union(statement({"StringEquals": eq}), {"Effect": "Deny"})])
}

test_violation_not_principal_or_not_action if {
	every extra in [{"NotPrincipal": {"AWS": "arn:aws:iam::123456789012:root"}}, {"NotAction": "sts:TagSession"}] {
		fixture.has("[PLAN-TRUST-02]", p.deny) with input as trust([object.union(statement({"StringEquals": eq}), extra)])
	}
}

test_violation_provider_absent if {
	fixture.has("[PLAN-TRUST-03]", p.deny) with input as fixture.plan([role([statement({"StringEquals": eq})])])
}

test_violation_provider_in_other_file if {
	files := [
		{"path": "a.json", "contents": {"plan_format_version": "1.2", "resource_changes": [role([statement({"StringEquals": eq})])]}},
		{"path": "b.json", "contents": {"plan_format_version": "1.2", "resource_changes": [provider]}},
	]
	fixture.has("[PLAN-TRUST-03]", p.deny) with input as files
}

test_violation_condition_without_string_equals if {
	msgs := p.deny with input as trust([statement({"StringLike": eq})])
	fixture.has("[PLAN-TRUST-04]", msgs)
	fixture.has("[PLAN-TRUST-05]", msgs)
}

test_violation_question_mark_value if {
	fixture.has("[PLAN-TRUST-06]", p.deny) with input as trust([statement({"StringEquals": object.union(eq, {"token.actions.githubusercontent.com:repository_id": "4567?9"})})])
}

test_violation_event_subject if {
	fixture.has("[PLAN-TRUST-08]", p.deny) with input as trust([statement({"StringEquals": object.union(eq, {"token.actions.githubusercontent.com:sub": "repo:octo-org@123456/octo-repo@456789:pull_request"})})])
}

test_violation_subject_with_surrounding_text if {
	every sub in ["xrepo:octo-org@123456/octo-repo@456789:environment:admin-plan", "repo:octo-org@123456/octo-repo@456789:environment:admin-plan:x"] {
		fixture.has("[PLAN-TRUST-08]", p.deny) with input as trust([statement({"StringEquals": object.union(eq, {"token.actions.githubusercontent.com:sub": sub})})])
	}
}

test_violation_subject_owner_id_mismatch if {
	fixture.has("[PLAN-TRUST-08]", p.deny) with input as trust([statement({"StringEquals": object.union(eq, {"token.actions.githubusercontent.com:repository_owner_id": "999"})})])
}

test_pass_indexed_provider_address if {
	indexed := object.union(provider, {"address": "module.deployment_identity.module.github_oidc.aws_iam_openid_connect_provider.github[0]"})
	count(p.deny) == 0 with input as fixture.plan([indexed, role([statement({"StringEquals": eq})])])
}

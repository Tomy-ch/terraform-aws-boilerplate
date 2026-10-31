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

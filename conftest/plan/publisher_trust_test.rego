package plan.publisher_trust_test

import data.plan.fixture
import data.plan.publisher_trust as p

provider_arn := "arn:aws:iam::123456789012:oidc-provider/token.actions.githubusercontent.com"

eq := {
	"token.actions.githubusercontent.com:aud": "sts.amazonaws.com",
	"token.actions.githubusercontent.com:sub": "repo:example@1000001/go-boilerplate@2000001:environment:dev-publish",
	"token.actions.githubusercontent.com:repository_id": "2000001",
	"token.actions.githubusercontent.com:repository_owner_id": "1000001",
}

statement(condition) := {
	"Effect": "Allow",
	"Action": "sts:AssumeRoleWithWebIdentity",
	"Principal": {"Federated": provider_arn},
	"Condition": condition,
}

role(statements) := fixture.doc("aws_iam_role", "module.image_registry.module.repository[\"api\"].aws_iam_role.publisher", "assume_role_policy", {"Statement": statements})

trust(statements) := fixture.plan([role(statements)])

test_pass_immutable_environment_subject if {
	count(p.deny) == 0 with input as trust([statement({"StringEquals": eq})])
}

test_violation_role_absent if {
	fixture.has("[PLAN-PUBTRUST-00]", p.deny) with input as fixture.plan([fixture.role_policy([fixture.allow("s3:GetObject")])])
}

test_violation_two_statements if {
	fixture.has("[PLAN-PUBTRUST-01]", p.deny) with input as trust([statement({"StringEquals": eq}), statement({"StringEquals": eq})])
}

test_violation_assume_role_not_web_identity if {
	s := object.union(statement({"StringEquals": eq}), {"Action": "sts:AssumeRole"})
	fixture.has("[PLAN-PUBTRUST-02]", p.deny) with input as trust([s])
}

test_violation_principal_not_github_provider if {
	s := object.union(statement({"StringEquals": eq}), {"Principal": {"Federated": "arn:aws:iam::123456789012:oidc-provider/example.com"}})
	fixture.has("[PLAN-PUBTRUST-03]", p.deny) with input as trust([s])
}

test_violation_string_like if {
	fixture.has("[PLAN-PUBTRUST-04]", p.deny) with input as trust([statement({"StringLike": eq})])
}

test_violation_missing_repository_id if {
	fixture.has("[PLAN-PUBTRUST-05]", p.deny) with input as trust([statement({"StringEquals": object.remove(eq, ["token.actions.githubusercontent.com:repository_id"])})])
}

test_violation_wildcard_subject if {
	e := object.union(eq, {"token.actions.githubusercontent.com:sub": "repo:example@1000001/go-boilerplate@2000001:environment:*"})
	fixture.has("[PLAN-PUBTRUST-06]", p.deny) with input as trust([statement({"StringEquals": e})])
}

test_violation_audience_not_sts if {
	e := object.union(eq, {"token.actions.githubusercontent.com:aud": "example"})
	fixture.has("[PLAN-PUBTRUST-07]", p.deny) with input as trust([statement({"StringEquals": e})])
}

test_violation_ref_subject if {
	e := object.union(eq, {"token.actions.githubusercontent.com:sub": "repo:example/go-boilerplate:ref:refs/heads/main"})
	fixture.has("[PLAN-PUBTRUST-08]", p.deny) with input as trust([statement({"StringEquals": e})])
}

test_violation_subject_ids_disagree if {
	e := object.union(eq, {"token.actions.githubusercontent.com:repository_id": "2000002"})
	fixture.has("[PLAN-PUBTRUST-08]", p.deny) with input as trust([statement({"StringEquals": e})])
}

package plan.image_registry_publisher_test

import data.plan.fixture
import data.plan.image_registry_publisher as p

prefix := "module.image_registry.module.repository[\"api\"]"

repo_arn := "arn:aws:ecr:ap-northeast-1:123456789012:repository/tabp-dev/api"

rc(type, name, after) := {
	"address": sprintf("%s.%s.%s", [prefix, type, name]),
	"type": type,
	"change": {"actions": ["create"], "after": after, "after_unknown": {}},
}

publish := {"Effect": "Allow", "Action": [
	"ecr:BatchCheckLayerAvailability", "ecr:BatchGetImage", "ecr:CompleteLayerUpload", "ecr:DescribeImageScanFindings",
	"ecr:DescribeImages", "ecr:InitiateLayerUpload", "ecr:PutImage", "ecr:UploadLayerPart",
], "Resource": repo_arn}

login := {"Effect": "Allow", "Action": "ecr:GetAuthorizationToken", "Resource": "*"}

policy(statements) := object.union(
	fixture.doc("aws_iam_role_policy", sprintf("%s.aws_iam_role_policy.publisher", [prefix]), "policy", {"Statement": statements}),
	{"change": {"actions": ["create"], "after": {"name": "publish", "policy": json.marshal({"Statement": statements})}, "after_unknown": {}}},
)

base := [
	rc("aws_ecr_repository", "this", {"name": "tabp-dev/api"}),
	rc("aws_iam_role", "publisher", {"name": "tabp-dev-ecr-publish-api"}),
	rc("aws_iam_role_policies_exclusive", "publisher", {"role_name": "tabp-dev-ecr-publish-api", "policy_names": ["publish"]}),
	rc("aws_iam_role_policy_attachments_exclusive", "publisher", {"role_name": "tabp-dev-ecr-publish-api", "policy_arns": []}),
]

with_policy(statements) := fixture.plan(array.concat(base, [policy(statements)]))

test_pass_publish_vocabulary if {
	count(p.deny) == 0 with input as with_policy([publish, login])
}

test_violation_declared_address_absent if {
	fixture.has("[PLAN-PUB-00]", p.deny) with input as fixture.plan(base)
}

test_violation_extra_action if {
	s := object.union(publish, {"Action": array.concat(publish.Action, ["ecr:BatchDeleteImage"])})
	fixture.has("[PLAN-PUB-01]", p.deny) with input as with_policy([s, login])
}

test_violation_missing_login if {
	fixture.has("[PLAN-PUB-01]", p.deny) with input as with_policy([publish])
}

test_violation_other_repository if {
	s := object.union(publish, {"Resource": "arn:aws:ecr:ap-northeast-1:123456789012:repository/tabp-dev/other"})
	fixture.has("[PLAN-PUB-02]", p.deny) with input as with_policy([s, login])
}

test_violation_all_repositories if {
	s := object.union(publish, {"Resource": "arn:aws:ecr:ap-northeast-1:123456789012:repository/*"})
	fixture.has("[PLAN-PUB-02]", p.deny) with input as with_policy([s, login])
}

test_violation_publish_on_star if {
	s := object.union(login, {"Action": ["ecr:GetAuthorizationToken", "ecr:PutImage"]})
	fixture.has("[PLAN-PUB-03]", p.deny) with input as with_policy([publish, s])
}

test_violation_not_action if {
	s := {"Effect": "Allow", "NotAction": "iam:*", "Resource": repo_arn}
	fixture.has("[PLAN-PUB-03]", p.deny) with input as with_policy([publish, login, s])
}

test_violation_extra_inline_policy_allowed if {
	rcs := [
		rc("aws_ecr_repository", "this", {"name": "tabp-dev/api"}),
		rc("aws_iam_role", "publisher", {"name": "tabp-dev-ecr-publish-api"}),
		rc("aws_iam_role_policies_exclusive", "publisher", {"role_name": "tabp-dev-ecr-publish-api", "policy_names": ["publish", "extra"]}),
		rc("aws_iam_role_policy_attachments_exclusive", "publisher", {"role_name": "tabp-dev-ecr-publish-api", "policy_arns": []}),
		policy([publish, login]),
	]
	fixture.has("[PLAN-PUB-04]", p.deny) with input as fixture.plan(rcs)
}

test_violation_managed_policy_attached if {
	rcs := [
		rc("aws_ecr_repository", "this", {"name": "tabp-dev/api"}),
		rc("aws_iam_role", "publisher", {"name": "tabp-dev-ecr-publish-api"}),
		rc("aws_iam_role_policies_exclusive", "publisher", {"role_name": "tabp-dev-ecr-publish-api", "policy_names": ["publish"]}),
		rc("aws_iam_role_policy_attachments_exclusive", "publisher", {"role_name": "tabp-dev-ecr-publish-api", "policy_arns": ["arn:aws:iam::aws:policy/AdministratorAccess"]}),
		policy([publish, login]),
	]
	fixture.has("[PLAN-PUB-04]", p.deny) with input as fixture.plan(rcs)
}

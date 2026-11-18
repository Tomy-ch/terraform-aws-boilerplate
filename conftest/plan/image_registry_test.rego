package plan.image_registry_test

import data.plan.fixture
import data.plan.image_registry as p

prefix := "module.image_registry.module.repository[\"api\"]"

repo_arn := "arn:aws:ecr:ap-northeast-1:123456789012:repository/tabp-dev/api"

rc(address, type, after) := {
	"address": address,
	"type": type,
	"change": {"actions": ["create"], "after": after, "after_unknown": {}},
}

repository(after) := rc(sprintf("%s.aws_ecr_repository.this", [prefix]), "aws_ecr_repository", after)

valid_repository := repository({"name": "tabp-dev/api", "force_delete": false})

lifecycle_doc(rules) := json.marshal({"rules": rules})

count_rule(n) := {
	"rulePriority": 1,
	"selection": {"tagStatus": "tagged", "tagPatternList": ["*"], "countType": "imageCountMoreThan", "countNumber": n},
	"action": {"type": "expire"},
}

lifecycle(doc) := rc(sprintf("%s.aws_ecr_lifecycle_policy.this", [prefix]), "aws_ecr_lifecycle_policy", {"repository": "tabp-dev/api", "policy": doc})

valid_lifecycle := lifecycle(lifecycle_doc([count_rule(30)]))

writes_denial := {
	"Effect": "Deny",
	"Principal": {"AWS": "arn:aws:iam::123456789012:root"},
	"Action": ["ecr:CompleteLayerUpload", "ecr:InitiateLayerUpload", "ecr:PutImage", "ecr:UploadLayerPart"],
	"Resource": repo_arn,
	"Condition": {"ArnNotEquals": {"aws:PrincipalArn": "arn:aws:iam::123456789012:role/tabp-dev-ecr-publish-api"}},
}

deletion_denial := {
	"Effect": "Deny",
	"Principal": {"AWS": "arn:aws:iam::123456789012:root"},
	"Action": "ecr:BatchDeleteImage",
	"Resource": repo_arn,
}

repository_policy(statements) := rc(sprintf("%s.aws_ecr_repository_policy.this", [prefix]), "aws_ecr_repository_policy", {"repository": "tabp-dev/api", "policy": json.marshal({"Statement": statements})})

valid_repository_policy := repository_policy([writes_denial, deletion_denial])

role := rc(sprintf("%s.aws_iam_role.publisher", [prefix]), "aws_iam_role", {"name": "tabp-dev-ecr-publish-api"})

scanning(after) := rc("module.image_registry.module.registry_scanning.aws_ecr_registry_scanning_configuration.this", "aws_ecr_registry_scanning_configuration", after)

continuous(filter) := {"scan_type": "ENHANCED", "rule": [{"scan_frequency": "CONTINUOUS_SCAN", "repository_filter": [{"filter": filter, "filter_type": "WILDCARD"}]}]}

valid_scanning := scanning(continuous("tabp-dev/*"))

registry(rcs) := fixture.plan(rcs)

valid := [valid_repository, valid_lifecycle, valid_repository_policy, role, valid_scanning]

replace(i, x) := array.concat(array.concat(array.slice(valid, 0, i), [x]), array.slice(valid, i + 1, count(valid)))

test_pass_registry if {
	count(p.deny) == 0 with input as registry(valid)
}

test_violation_declared_address_absent if {
	fixture.has("[PLAN-ECR-00]", p.deny) with input as registry(array.slice(valid, 0, 4))
}

test_violation_second_scanning_writer if {
	other := rc("module.account_baseline.aws_ecr_registry_scanning_configuration.this", "aws_ecr_registry_scanning_configuration", continuous("*"))
	fixture.has("[PLAN-ECR-01]", p.deny) with input as registry(array.concat(valid, [other]))
}

test_violation_basic_scanning if {
	fixture.has("[PLAN-ECR-02]", p.deny) with input as registry(replace(4, scanning(object.union(continuous("tabp-dev/*"), {"scan_type": "BASIC"}))))
}

test_violation_repository_outside_filter if {
	fixture.has("[PLAN-ECR-03]", p.deny) with input as registry(replace(4, scanning(continuous("other/*"))))
}

test_violation_scan_on_push_only if {
	s := scanning({"scan_type": "ENHANCED", "rule": [{"scan_frequency": "SCAN_ON_PUSH", "repository_filter": [{"filter": "tabp-dev/*", "filter_type": "WILDCARD"}]}]})
	fixture.has("[PLAN-ECR-03]", p.deny) with input as registry(replace(4, s))
}

test_violation_lifecycle_missing if {
	other := lifecycle(lifecycle_doc([count_rule(30)]))
	moved := object.union(other, {"change": {"after": {"repository": "tabp-dev/other"}}})
	fixture.has("[PLAN-ECR-04]", p.deny) with input as registry(replace(1, moved))
}

test_violation_untagged_rule if {
	untagged := {"rulePriority": 2, "selection": {"tagStatus": "untagged", "countType": "sinceImagePushed", "countUnit": "days", "countNumber": 14}, "action": {"type": "expire"}}
	fixture.has("[PLAN-ECR-05]", p.deny) with input as registry(replace(1, lifecycle(lifecycle_doc([count_rule(30), untagged]))))
}

test_violation_single_retained_image if {
	fixture.has("[PLAN-ECR-05]", p.deny) with input as registry(replace(1, lifecycle(lifecycle_doc([count_rule(1)]))))
}

test_violation_force_delete if {
	fixture.has("[PLAN-ECR-06]", p.deny) with input as registry(replace(0, repository({"name": "tabp-dev/api", "force_delete": true})))
}

test_violation_allow_statement if {
	allow := {"Effect": "Allow", "Principal": {"AWS": "arn:aws:iam::210987654321:root"}, "Action": "ecr:BatchGetImage", "Resource": repo_arn}
	fixture.has("[PLAN-ECR-07]", p.deny) with input as registry(replace(2, repository_policy([writes_denial, deletion_denial, allow])))
}

test_violation_write_deny_names_other_role if {
	s := object.union(writes_denial, {"Condition": {"ArnNotEquals": {"aws:PrincipalArn": "arn:aws:iam::123456789012:role/other"}}})
	fixture.has("[PLAN-ECR-08]", p.deny) with input as registry(replace(2, repository_policy([s, deletion_denial])))
}

test_violation_write_deny_misses_put_image if {
	s := object.union(writes_denial, {"Action": ["ecr:CompleteLayerUpload", "ecr:InitiateLayerUpload", "ecr:UploadLayerPart"]})
	fixture.has("[PLAN-ECR-08]", p.deny) with input as registry(replace(2, repository_policy([s, deletion_denial])))
}

test_violation_delete_deny_absent if {
	fixture.has("[PLAN-ECR-09]", p.deny) with input as registry(replace(2, repository_policy([writes_denial])))
}

test_violation_delete_deny_conditioned if {
	s := object.union(deletion_denial, {"Condition": {"ArnNotEquals": {"aws:PrincipalArn": "arn:aws:iam::123456789012:role/tabp-dev-ci-apply"}}})
	fixture.has("[PLAN-ECR-09]", p.deny) with input as registry(replace(2, repository_policy([writes_denial, s])))
}

test_violation_second_repository_policy if {
	other := rc("module.extra.aws_ecr_repository_policy.this", "aws_ecr_repository_policy", {"repository": "tabp-dev/api", "policy": json.marshal({"Statement": [deletion_denial]})})
	fixture.has("[PLAN-ECR-10]", p.deny) with input as registry(array.concat(valid, [other]))
}

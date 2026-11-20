package plan.s3_bucket_namespace_test

import data.plan.fixture
import data.plan.s3_bucket_namespace as p

name := "tabp-x-111111111111-ap-northeast-1-an"

bucket(actions, after, unknown) := {
	"address": "module.x.aws_s3_bucket.this",
	"mode": "managed",
	"type": "aws_s3_bucket",
	"change": {"actions": actions, "before": null, "after": after, "after_unknown": unknown},
}

kept := [["create"], ["update"], ["no-op"], ["delete", "create"], ["create", "delete"]]

regional := {"bucket": name, "bucket_namespace": "account-regional"}

deny(rcs) := msgs if {
	msgs := p.deny with input as fixture.plan(rcs)
}

test_pass_account_regional if {
	every actions in kept {
		count(deny([bucket(actions, regional, {})])) == 0
	}
}

test_pass_unknown_absent_or_false if {
	every unknown in [{"bucket_namespace": false}, {"bucket": true}] {
		count(deny([bucket(["create"], regional, unknown)])) == 0
	}
	count(deny([object.union(bucket(["create"], regional, {}), {"change": {"after_unknown": null}})])) == 0
}

test_pass_no_bucket if {
	count(deny([fixture.doc("aws_iam_role_policy", "module.x.aws_iam_role_policy.this", "policy", {"Statement": []})])) == 0
}

test_pass_delete_and_forget if {
	every actions in [["delete"], ["forget"]] {
		rc := object.union(bucket(actions, null, {}), {"change": {"before": {"bucket": name, "bucket_namespace": "global"}}})
		count(deny([rc])) == 0
	}
}

test_pass_data_source if {
	rc := object.union(bucket(["read"], {"bucket": name, "bucket_namespace": "global"}, {}), {"mode": "data", "address": "data.aws_s3_bucket.this"})
	count(deny([rc])) == 0
}

test_violation_not_account_regional if {
	every actions in kept {
		every after in [
			{"bucket": name},
			{"bucket": name, "bucket_namespace": null},
			{"bucket": name, "bucket_namespace": "global"},
			{"bucket": name, "bucket_namespace": ""},
			{"bucket": name, "bucket_namespace": "Account-Regional"},
			{"bucket": name, "bucket_namespace": 1},
		] {
			fixture.has("[PLAN-S3NS-01] p.json: module.x.aws_s3_bucket.this の bucket_namespace が", deny([bucket(actions, after, {})]))
		}
	}
}

test_violation_after_unreadable if {
	fixture.has("[PLAN-S3NS-01] p.json: module.x.aws_s3_bucket.this の bucket_namespace が", deny([bucket(["create"], null, {})]))
}

test_violation_unknown if {
	msgs := deny([bucket(["create"], {"bucket": name}, {"bucket_namespace": true})])
	fixture.has("[PLAN-S3NS-02] p.json: module.x.aws_s3_bucket.this の bucket_namespace が plan で決まりません", msgs)
	not fixture.has("[PLAN-S3NS-01]", msgs)
}

test_violation_names_each_bucket if {
	other := object.union(bucket(["create"], {"bucket": "tabp-y", "bucket_namespace": "global"}, {}), {"address": "module.y.aws_s3_bucket.this"})
	msgs := deny([bucket(["create"], regional, {}), other])
	count(msgs) == 1
	fixture.has("[PLAN-S3NS-01] p.json: module.y.aws_s3_bucket.this の bucket_namespace が", msgs)
}

test_violation_names_each_file if {
	inp := [
		{"path": "a.json", "contents": {"plan_format_version": "1.2", "resource_changes": [bucket(["create"], regional, {})]}},
		{"path": "b.json", "contents": {"plan_format_version": "1.2", "resource_changes": [bucket(["create"], {"bucket": name, "bucket_namespace": "global"}, {})]}},
	]
	msgs := p.deny with input as inp
	count(msgs) == 1
	fixture.has("[PLAN-S3NS-01] b.json: module.x.aws_s3_bucket.this の bucket_namespace が", msgs)
}

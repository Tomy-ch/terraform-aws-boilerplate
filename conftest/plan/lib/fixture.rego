# Rego のテストが組む最小の入力。形は terraform test -json -verbose の test_plan に合わせる。
package plan.fixture

plan(rcs) := [{"path": "p.json", "contents": {"plan_format_version": "1.2", "resource_changes": rcs}}]

doc(type, address, attr, body) := {
	"address": address,
	"type": type,
	"change": {"actions": ["create"], "after": {attr: json.marshal(body)}, "after_unknown": {}},
}

role_policy(statements) := doc("aws_iam_role_policy", "module.x.aws_iam_role_policy.this[\"plan\"]", "policy", {"Statement": statements})

allow(action) := {"Effect": "Allow", "Action": action, "Resource": "arn:aws:s3:::b"}

has(prefix, msgs) if {
	some m in msgs
	startswith(m, prefix)
}

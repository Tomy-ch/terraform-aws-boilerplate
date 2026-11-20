# Rego のテストが組む最小の入力。形は terraform test -json -verbose の test_plan に合わせる。
package plan.fixture

import data.plan.lib

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

unwritten(type) := {
	"address": sprintf("module.x.%s.this", [type]),
	"type": type,
	"change": {"actions": ["create"], "after": lib.inline_arguments[type], "after_unknown": {}},
}

unwritten_all := [unwritten(type) | some type, _ in lib.inline_arguments]

# make tf-policy が Provider の schema から取り出す一覧の代わり。宣言した引数をそのまま非推奨の一覧とする。
provider_schema := {"deprecated_arguments": {type: [arg | some arg, _ in args] | some type, args in lib.inline_arguments}}

clean(rcs) := plan(array.concat(rcs, unwritten_all))

# with で data.policy_plan の下の object を差し替えると、既存の値と合わさる。宣言を丸ごと組み直して置き換える。
inline_declaration(d) := object.union(object.remove(data.policy_plan, ["inline_arguments"]), {"inline_arguments": d})

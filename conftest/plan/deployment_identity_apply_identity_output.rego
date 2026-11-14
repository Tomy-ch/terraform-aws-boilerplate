# deployment-identity の apply-identity の role が、plan の出力の Bucket の object へ書けるのは apply/* だけで、
# 消せないこと（ADR-0602 決定13、deployment-identity/ADR-0201 決定10）。IAM（deployment-identity）と
# Bucket（state-backend）は別の root の plan に在るので、結合した入力で突き合わせる。
package plan.deployment_identity_apply_identity_output

import data.plan.lib

cfg := object.get(object.get(data.policy_plan, "deployment_identity", {}), "apply_identity_role", {})

default role := ""

role := object.get(cfg, "role", "")

default target := ""

target := object.get(cfg, "policy", "")

default output_bucket := ""

output_bucket := object.get(cfg, "output_bucket", "")

after(address) := c.rc.change.after if {
	some c in lib.changes
	c.rc.address == address
}

role_name := after(role).name

bucket_arn := after(output_bucket).arn

apply_key := sprintf("%s/apply/*", [bucket_arn])

probe_keys := ["apply/x", "pr/x", "branch/x", "x"]

statements contains {"path": r.document.path, "statement": s} if {
	some r in lib.readable
	r.document.type == "aws_iam_role_policy"
	r.document.attr == "policy"
	r.document.address == target
	some s in lib.statements(r.body)
}

allows contains a if {
	some a in statements
	a.statement.Effect == "Allow"
}

actions(s) := lib.lower_all(object.get(s, "Action", []))

resources(s) := lib.as_array(object.get(s, "Resource", []))

grants(s, action) if {
	some a in actions(s)
	glob.match(a, null, action)
}

on_output_objects(resource) if {
	some k in probe_keys
	glob.match(resource, null, sprintf("%s/%s", [bucket_arn, k]))
}

role_known if is_string(role_name)

bucket_known if is_string(bucket_arn)

deny contains msg if {
	some k in ["role", "policy", "output_bucket"]
	object.get(cfg, k, "") == ""
	msg := sprintf("[PLAN-AIO-00] data.policy_plan.deployment_identity.apply_identity_role.%s が宣言にありません", [k])
}

deny contains msg if {
	target != ""
	count({d | some d in lib.documents; d.type == "aws_iam_role_policy"; d.address == target}) == 0
	msg := sprintf("[PLAN-AIO-00] apply-identity の role の policy %q が入力にありません。address が変わったなら data.policy_plan.deployment_identity.apply_identity_role を直してください", [target])
}

deny contains msg if {
	role != ""
	not role_known
	msg := sprintf("[PLAN-AIO-00] apply-identity の role %q の名前が入力にありません。address が変わったなら data.policy_plan.deployment_identity.apply_identity_role を直してください", [role])
}

deny contains msg if {
	output_bucket != ""
	not bucket_known
	msg := sprintf("[PLAN-AIO-00] plan の出力の Bucket %q の ARN を入力から読めません。address が変わったなら data.policy_plan.deployment_identity.apply_identity_role を直し、unknown なら tests/policy-input.tftest.hcl の override_resource で与えてください", [output_bucket])
}

deny contains msg if {
	some a in allows
	some k in ["NotAction", "NotResource"]
	a.statement[k]
	msg := sprintf("[PLAN-AIO-01] %s: apply-identity の role の Allow が %s を使っています。Action と Resource を列挙してください", [a.path, k])
}

# Bucket の ARN が読めなければ PLAN-AIO-00 が落とす。下の3つは ARN を読めたときだけ評価する。
deny contains msg if {
	bucket_known
	some a in allows
	grants(a.statement, "s3:putobject")
	some resource in resources(a.statement)
	on_output_objects(resource)
	resource != apply_key
	msg := sprintf("[PLAN-AIO-02] %s: apply-identity の role の s3:PutObject の Resource %s が、plan の出力の Bucket の %s ではありません", [a.path, resource, apply_key])
}

deny contains msg if {
	bucket_known
	some a in allows
	some action in ["s3:deleteobject", "s3:deleteobjectversion"]
	grants(a.statement, action)
	some resource in resources(a.statement)
	on_output_objects(resource)
	msg := sprintf("[PLAN-AIO-03] %s: apply-identity の role が plan の出力の Bucket の object %s に %s を許しています（ADR-0602 決定13）", [a.path, resource, action])
}

deny contains msg if {
	bucket_known
	count({a |
		some a in allows
		actions(a.statement) == ["s3:putobject"]
		resources(a.statement) == [apply_key]
	}) == 0
	msg := sprintf("[PLAN-AIO-04] apply-identity の role に、%s への s3:PutObject だけを許す文がありません", [apply_key])
}

# policy の resource は role を名前で指すので、名前が unknown なものはどの role に付くか読めない。
attached contains {"path": c.path, "address": c.rc.address, "type": c.rc.type, "after": c.rc.change.after, "unknown": object.get(c.rc.change, "after_unknown", {})} if {
	some c in lib.changes
	c.rc.type in {"aws_iam_role_policy", "aws_iam_role_policy_attachment"}
}

deny contains msg if {
	role_known
	some a in attached
	object.get(a.after, "role", null) == role_name
	a.address != target
	msg := sprintf("[PLAN-AIO-05] %s: %s が apply-identity の role に権限を足しています。この role の権限は %s だけに置いてください", [a.path, a.address, target])
}

deny contains msg if {
	some a in attached
	object.get(a.unknown, "role", false) == true
	msg := sprintf("[PLAN-AIO-05] %s: %s がどの role に付くかを plan で読めません", [a.path, a.address])
}

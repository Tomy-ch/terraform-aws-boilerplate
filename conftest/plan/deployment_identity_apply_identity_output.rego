# deployment-identity の apply-identity の role が、plan の出力の Bucket の object へ書けるのは apply/* だけで、
# 消せないこと（ADR-0602 決定13、deployment-identity/ADR-0201 決定10）。IAM（deployment-identity）と
# Bucket（state-backend）は別の root の plan に在るので、結合した入力で突き合わせる。
package plan.deployment_identity_apply_identity_output

import data.plan.lib

raw_cfg := object.get(object.get(data.policy_plan, "deployment_identity", {}), "apply_identity_role", {})

cfg := raw_cfg if is_object(raw_cfg)

cfg := {} if not is_object(raw_cfg)

declared(k) := v if {
	v := object.get(cfg, k, "")
	is_string(v)
}

declared(k) := "" if not is_string(object.get(cfg, k, ""))

role := declared("role")

target := declared("policy")

output_bucket := declared("output_bucket")

output_bucket_policy := declared("output_bucket_policy")

after(address) := c.rc.change.after if {
	some c in lib.changes
	c.rc.address == address
}

role_name := after(role).name

policy_name := after(target).name

bucket_arn := after(output_bucket).arn

apply_key := sprintf("%s/apply/*", [bucket_arn])

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

# IAM の Action と Resource の * と ? は ":" や "/" も越え、ほかの文字は文字そのものである。glob.match は [ と { を
# 構文として読み、閉じていないと評価が未定義になって規則が黙るので使わない。
iam_regex(p) := strings.replace_n({"?": ".", "*": ".*"}, regex.replace(p, `[.+()\[\]{}|^$\\]`, `\${0}`))

# service の名前に wildcard を含む Action（"*"、"s?:*" など）も S3 の Action に当たる。
s3_action(a) if regex.match(sprintf("^%s$", [iam_regex(split(a, ":")[0])]), "s3")

# Resource のある接頭辞が "<Bucket の ARN>/" に一致すれば、その Resource は Bucket のある object に一致する。
reaches_objects(resource) if {
	some k in numbers.range(1, count(resource))
	regex.match(sprintf("^%s$", [iam_regex(substring(resource, 0, k))]), sprintf("%s/", [bucket_arn]))
}

role_known if is_string(role_name)

bucket_known if is_string(bucket_arn)

deny contains msg if {
	not is_object(raw_cfg)
	msg := "[PLAN-AIO-00] data.policy_plan.deployment_identity.apply_identity_role が object ではありません"
}

deny contains msg if {
	some k in ["role", "policy", "output_bucket", "output_bucket_policy"]
	declared(k) == ""
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

# 宣言した policy が別の role に付いていると、下の規則は apply-identity の role の権限を1つも見ない。
deny contains msg if {
	role_known
	target != ""
	lower(object.get(after(target), "role", "")) != lower(role_name)
	msg := sprintf("[PLAN-AIO-00] apply-identity の role の policy %q が、apply-identity の role %q に付いていません", [target, role_name])
}

deny contains msg if {
	some a in allows
	some k in ["NotAction", "NotResource"]
	a.statement[k]
	msg := sprintf("[PLAN-AIO-01] %s: apply-identity の role の Allow が %s を使っています。Action と Resource を列挙してください", [a.path, k])
}

# policy 変数（${aws:PrincipalAccount} など）は IAM が評価時に置き換えるので、Resource がどの object に届くかを plan から読めない。
deny contains msg if {
	some a in allows
	some resource in resources(a.statement)
	contains(resource, "${")
	msg := sprintf("[PLAN-AIO-01] %s: apply-identity の role の Allow の Resource %s が policy 変数を使っています。値を書いてください", [a.path, resource])
}

# Bucket の ARN が読めなければ PLAN-AIO-00 が落とす。下の3つは ARN を読めたときだけ評価する。
deny contains msg if {
	bucket_known
	some a in allows
	some action in actions(a.statement)
	s3_action(action)
	action != "s3:putobject"
	some resource in resources(a.statement)
	reaches_objects(resource)
	msg := sprintf("[PLAN-AIO-02] %s: apply-identity の role が plan の出力の Bucket の object %s に %s を許しています。許すのは s3:PutObject だけです（ADR-0602 決定13）", [a.path, resource, action])
}

deny contains msg if {
	bucket_known
	some a in allows
	"s3:putobject" in actions(a.statement)
	some resource in resources(a.statement)
	reaches_objects(resource)
	resource != apply_key
	msg := sprintf("[PLAN-AIO-03] %s: apply-identity の role の s3:PutObject の Resource %s が、plan の出力の Bucket の %s ではありません", [a.path, resource, apply_key])
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
	c.rc.type in {"aws_iam_role_policy", "aws_iam_role_policy_attachment", "aws_iam_role_policies_exclusive", "aws_iam_role_policy_attachments_exclusive"}
}

role_key(a) := "role_name" if endswith(a.type, "_exclusive")

role_key(a) := "role" if not endswith(a.type, "_exclusive")

# IAM の role 名は大文字と小文字を区別しない。
on_role(a) if lower(object.get(a.after, role_key(a), "")) == lower(role_name)

exclusive(type) := {a | some a in attached; a.type == type; on_role(a)}

# exclusive の2つは権限を足さず、宣言の外で付いた policy を外すので、ここでは件数と中身だけを見る。
deny contains msg if {
	role_known
	some a in attached
	a.type in {"aws_iam_role_policy", "aws_iam_role_policy_attachment"}
	on_role(a)
	a.address != target
	msg := sprintf("[PLAN-AIO-05] %s: %s が apply-identity の role に権限を足しています。この role の権限は %s だけに置いてください", [a.path, a.address, target])
}

deny contains msg if {
	some a in attached
	object.get(a.unknown, role_key(a), false) == true
	msg := sprintf("[PLAN-AIO-05] %s: %s がどの role に付くかを plan で読めません", [a.path, a.address])
}

deny contains msg if {
	role_known
	some type in ["aws_iam_role_policies_exclusive", "aws_iam_role_policy_attachments_exclusive"]
	count(exclusive(type)) != 1
	msg := sprintf("[PLAN-AIO-05] apply-identity の role %s の %s が %d 件です（1件を期待）", [role_name, type, count(exclusive(type))])
}

deny contains msg if {
	some a in exclusive("aws_iam_role_policies_exclusive")
	object.get(a.after, "policy_names", null) != [policy_name]
	msg := sprintf("[PLAN-AIO-05] %s: %s の policy_names が %v ではありません", [a.path, a.address, [policy_name]])
}

deny contains msg if {
	some a in exclusive("aws_iam_role_policy_attachments_exclusive")
	object.get(a.after, "policy_arns", null) != []
	msg := sprintf("[PLAN-AIO-05] %s: %s の policy_arns が空ではありません", [a.path, a.address])
}

# role の引数で付ける policy は、上の型を通らない。
deny contains msg if {
	some c in lib.changes
	c.rc.address == role
	some attr in ["inline_policy", "managed_policy_arns"]
	not empty_known(c.rc.change, attr)
	msg := sprintf("[PLAN-AIO-05] %s: apply-identity の role %s の %s が空ではないか、plan で決まっていません", [c.path, role, attr])
}

empty_known(change, attr) if {
	not unknown_any(object.get(object.get(change, "after_unknown", {}), attr, false))
	object.get(change.after, attr, []) in {[], null}
}

unknown_any(x) if {
	walk(x, [_, v])
	v == true
}

deny contains msg if {
	role_known
	some c in lib.changes
	c.rc.type == "aws_iam_policy_attachment"
	some r in lib.as_array(object.get(c.rc.change.after, "roles", []))
	lower(r) == lower(role_name)
	msg := sprintf("[PLAN-AIO-05] %s: %s が apply-identity の role %s に managed policy を付けています", [c.path, c.rc.address, role_name])
}

deny contains msg if {
	some c in lib.changes
	c.rc.type == "aws_iam_policy_attachment"
	unknown_any(object.get(object.get(c.rc.change, "after_unknown", {}), "roles", false))
	msg := sprintf("[PLAN-AIO-05] %s: %s がどの role に付くかを plan で読めません", [c.path, c.rc.address])
}

# 同じアカウントの主体には Bucket Policy の Allow だけで権限が付くので、IAM の規則を迂回できる。
bucket_policies contains r if {
	some r in lib.readable
	r.document.type == "aws_s3_bucket_policy"
	r.document.address == output_bucket_policy
}

deny contains msg if {
	output_bucket_policy != ""
	count({d | some d in lib.documents; d.type == "aws_s3_bucket_policy"; d.address == output_bucket_policy}) == 0
	msg := sprintf("[PLAN-AIO-00] plan の出力の Bucket の Bucket Policy %q が入力にありません。address が変わったなら data.policy_plan.deployment_identity.apply_identity_role を直してください", [output_bucket_policy])
}

# Bucket Policy は Bucket ごとに1つなので、宣言の外の resource が同じ Bucket を指すと、apply の順で実体が決まる。
bucket_refs := {v | some v in [object.get(after(output_bucket), "id", null), object.get(after(output_bucket), "bucket", null), bucket_arn]; is_string(v)}

on_output_bucket(c) if object.get(c.rc.change.after, "bucket", null) in bucket_refs

on_output_bucket(c) if object.get(object.get(c.rc.change, "after_unknown", {}), "bucket", false) == true

deny contains msg if {
	bucket_known
	some c in lib.changes
	c.rc.type == "aws_s3_bucket_policy"
	on_output_bucket(c)
	c.rc.address != output_bucket_policy
	msg := sprintf("[PLAN-AIO-06] %s: %s が plan の出力の Bucket を指すか、どの Bucket を指すかを plan で読めません。この Bucket の Bucket Policy は %s だけに置いてください", [c.path, c.rc.address, output_bucket_policy])
}

deny contains msg if {
	bucket_known
	some c in lib.changes
	c.rc.address == output_bucket_policy
	not on_output_bucket(c)
	msg := sprintf("[PLAN-AIO-06] %s: %s が plan の出力の Bucket %s を指していません", [c.path, c.rc.address, bucket_arn])
}

deny contains msg if {
	some r in bucket_policies
	some s in lib.statements(r.body)
	s.Effect != "Deny"
	msg := sprintf("[PLAN-AIO-06] %s: plan の出力の Bucket の Bucket Policy %s が Deny でない文 %v を持っています", [r.document.path, r.document.address, object.get(s, "Sid", "")])
}

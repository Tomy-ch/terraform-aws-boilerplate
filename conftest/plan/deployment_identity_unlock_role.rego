# deployment-identity の unlock-identity の role の権限（ADR-0602 決定13、deployment-identity/ADR-0201 決定9）。
# IAM の Allow が state の Bucket の identity root の lock file の一覧・読み取り・削除だけであることと、state の鍵の Key Policy がこの role に
# 復号だけを、IAM が許す lock file の object に限って許すこと。IAM（deployment-identity）と Key Policy（state-backend）は
# 別の root の plan に在るので、結合した入力で突き合わせる。
package plan.deployment_identity_unlock_role

import data.plan.lib

cfg := object.get(object.get(data.policy_plan, "deployment_identity", {}), "unlock_role", {})

# index key 付きの address（["unlock_identity"]）で宣言する。同じ resource の plan と apply の instance と分けるため。
default target := ""

target := object.get(cfg, "policy", "")

default role := ""

role := object.get(cfg, "role", "")

default lock_key := ""

lock_key := object.get(cfg, "lock_key", "")

# lock file を置く state の Bucket。state-backend の plan に在る。
default state_bucket := ""

state_bucket := object.get(cfg, "state_bucket", "")

allowed_actions := {"s3:listbucket", "s3:getobject", "s3:deleteobject"}

object_actions := {"s3:getobject", "s3:deleteobject"}

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

after(address) := c.rc.change.after if {
	some c in lib.changes
	c.rc.address == address
}

role_name := after(role).name

role_arn := after(role).arn

policy_name := after(target).name

bucket_arn := after(state_bucket).arn

lock_arn := sprintf("%s/%s", [bucket_arn, lock_key])

# IAM が削除を許す lock file の object。Key Policy の復号の条件と同じでなければならない。
lock_objects := {res |
	some a in allows
	some act in actions(a.statement)
	act in object_actions
	some res in lib.as_array(object.get(a.statement, "Resource", []))
}

key_policies contains r if {
	some r in lib.readable
	r.document.type == "aws_kms_key_policy"
}

deny contains msg if {
	count({d | some d in lib.documents; d.type == "aws_iam_role_policy"; d.address == target}) == 0
	msg := sprintf("[PLAN-UNLOCK-00] unlock-identity の role の policy %q が入力にありません。address が変わったなら data.policy_plan.deployment_identity.unlock_role を直してください", [target])
}

role_known if is_string(role_name)

arn_known if is_string(role_arn)

bucket_known if is_string(bucket_arn)

deny contains msg if {
	not role_known
	msg := sprintf("[PLAN-UNLOCK-00] unlock-identity の role %q の名前が入力にありません。address が変わったなら data.policy_plan.deployment_identity.unlock_role を直してください", [role])
}

deny contains msg if {
	not arn_known
	msg := sprintf("[PLAN-UNLOCK-00] unlock-identity の role %q の ARN を入力から読めません。tests/policy-input.tftest.hcl の override_resource で与えてください", [role])
}

deny contains msg if {
	not bucket_known
	msg := sprintf("[PLAN-UNLOCK-00] state の Bucket %q の ARN を入力から読めません。address が変わったなら data.policy_plan.deployment_identity.unlock_role を直し、unknown なら tests/policy-input.tftest.hcl の override_resource で与えてください", [state_bucket])
}

deny contains msg if {
	count(key_policies) == 0
	msg := "[PLAN-UNLOCK-00] 突き合わせる Key Policy（aws_kms_key_policy）が入力にありません"
}

deny contains msg if {
	lock_key == ""
	msg := "[PLAN-UNLOCK-00] lock file の key（data.policy_plan.deployment_identity.unlock_role.lock_key）が宣言にありません"
}

deny contains msg if {
	some a in statements
	some k in ["NotAction", "NotResource"]
	a.statement[k]
	msg := sprintf("[PLAN-UNLOCK-01] %s: unlock-identity の role の文が %s を使っています。Action と Resource を列挙してください", [a.path, k])
}

deny contains msg if {
	some a in allows
	some act in actions(a.statement)
	not act in allowed_actions
	msg := sprintf("[PLAN-UNLOCK-01] %s: unlock-identity の role の Allow に %s があります。許すのは s3:ListBucket・s3:GetObject・s3:DeleteObject だけです", [a.path, act])
}

# Bucket の ARN が読めなければ PLAN-UNLOCK-00 が落とす。下の2つは ARN を読めたときだけ評価する。
deny contains msg if {
	bucket_known
	some a in allows
	some act in actions(a.statement)
	act in object_actions
	not single_lock_object(a.statement)
	msg := sprintf("[PLAN-UNLOCK-02] %s: unlock-identity の role の %s の Resource が、state の Bucket の lock file %s の1つではありません", [a.path, act, lock_arn])
}

deny contains msg if {
	bucket_known
	some a in allows
	"s3:listbucket" in actions(a.statement)
	lib.as_array(object.get(a.statement, "Resource", [])) != [bucket_arn]
	msg := sprintf("[PLAN-UNLOCK-02] %s: unlock-identity の role の s3:ListBucket の Resource が、state の Bucket %s の1つではありません", [a.path, bucket_arn])
}

deny contains msg if {
	some a in allows
	"s3:listbucket" in actions(a.statement)
	not prefix_limited(a.statement)
	msg := sprintf("[PLAN-UNLOCK-02] %s: unlock-identity の role の s3:ListBucket が、s3:prefix を StringEquals で %s だけに限っていません", [a.path, lock_key])
}

single_lock_object(s) if {
	lib.as_array(object.get(s, "Resource", [])) == [lock_arn]
}

# StringLike や IfExists では限らない。
prefix_limited(s) if {
	some op, kv in object.get(s, "Condition", {})
	lower(op) == "stringequals"
	some k, v in kv
	lower(k) == "s3:prefix"
	lib.as_array(v) == [lock_key]
}

# policy の resource は role を名前で指すので、名前が unknown なものはどの role に付くか読めない。
attached contains {"path": c.path, "address": c.rc.address, "type": c.rc.type, "after": c.rc.change.after, "unknown": object.get(c.rc.change, "after_unknown", {})} if {
	some c in lib.changes
	c.rc.type in {"aws_iam_role_policy", "aws_iam_role_policy_attachment", "aws_iam_role_policies_exclusive", "aws_iam_role_policy_attachments_exclusive"}
}

role_key(a) := "role_name" if endswith(a.type, "_exclusive")

role_key(a) := "role" if not endswith(a.type, "_exclusive")

on_unlock_role(a) if object.get(a.after, role_key(a), null) == role_name

exclusive(type) := {a | some a in attached; a.type == type; on_unlock_role(a)}

# exclusive の2つは権限を足さず、宣言の外で付いた policy を外すので、ここでは見ない。
deny contains msg if {
	some a in attached
	a.type in {"aws_iam_role_policy", "aws_iam_role_policy_attachment"}
	on_unlock_role(a)
	a.address != target
	msg := sprintf("[PLAN-UNLOCK-03] %s: %s が unlock-identity の role に権限を足しています。この role の権限は %s だけに置いてください", [a.path, a.address, target])
}

deny contains msg if {
	some a in attached
	object.get(a.unknown, role_key(a), false) == true
	msg := sprintf("[PLAN-UNLOCK-03] %s: %s がどの role に付くかを plan で読めません", [a.path, a.address])
}

deny contains msg if {
	is_string(role_name)
	some type in ["aws_iam_role_policies_exclusive", "aws_iam_role_policy_attachments_exclusive"]
	count(exclusive(type)) != 1
	msg := sprintf("[PLAN-UNLOCK-03] unlock-identity の role %s の %s が %d 件です（1件を期待）", [role_name, type, count(exclusive(type))])
}

deny contains msg if {
	some a in exclusive("aws_iam_role_policies_exclusive")
	object.get(a.after, "policy_names", null) != [policy_name]
	msg := sprintf("[PLAN-UNLOCK-03] %s: %s の policy_names が %v ではありません", [a.path, a.address, [policy_name]])
}

deny contains msg if {
	some a in exclusive("aws_iam_role_policy_attachments_exclusive")
	object.get(a.after, "policy_arns", null) != []
	msg := sprintf("[PLAN-UNLOCK-03] %s: %s の policy_arns が空ではありません", [a.path, a.address])
}

key_grants contains {"document": r.document, "statement": s} if {
	some r in key_policies
	some s in lib.statements(r.body)
	s.Effect == "Allow"
	is_object(s.Principal)
	role_arn in lib.as_array(object.get(s.Principal, "AWS", []))
}

deny contains msg if {
	is_string(role_arn)
	count(key_policies) > 0
	count(key_grants) == 0
	msg := sprintf("[PLAN-UNLOCK-04] どの Key Policy も unlock-identity の role %s に lock file の復号を許していません", [role_arn])
}

deny contains msg if {
	some g in key_grants
	lib.lower_all(g.statement.Action) != ["kms:decrypt"]
	msg := sprintf("[PLAN-UNLOCK-04] %s: %s が unlock-identity の role に kms:Decrypt 以外を許しています", [g.document.path, g.document.address])
}

deny contains msg if {
	some g in key_grants
	not decrypt_limited_to_lock(g.statement)
	msg := sprintf("[PLAN-UNLOCK-04] %s: %s の unlock-identity の role への復号が、kms:EncryptionContext:aws:s3:arn で IAM の lock file の object %v に限られていません", [g.document.path, g.document.address, sort(lock_objects)])
}

decrypt_limited_to_lock(s) if {
	vs := lib.limiting_values(s, "kms:encryptioncontext:aws:s3:arn")
	count(vs) > 0
	every v in vs {
		lib.no_wildcard(v)
	}
	{v | some v in vs} == lock_objects
}

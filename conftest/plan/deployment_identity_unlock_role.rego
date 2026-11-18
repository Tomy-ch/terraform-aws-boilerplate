# deployment-identity の lock を外す role（unlock_roles の各 role）の権限（ADR-0602 決定13、deployment-identity/ADR-0201 決定9・11。
# conftest/plan/README.md の plan.deployment_identity_unlock_role）。
# IAM（deployment-identity）と Key Policy（state-backend）は別の root の plan に在るので、結合した入力で突き合わせる。
package plan.deployment_identity_unlock_role

import data.plan.lib

di := object.get(data.policy_plan, "deployment_identity", {})

# state_backend.roots の unlocker と key_user_roles と同じ名前で指す。
default names := []

names := object.get(di, "unlock_roles", [])

cfg(n) := object.get(di, n, {})

# index key 付きの address（["unlock_identity"]）で宣言する。同じ resource の plan と apply の instance と分けるため。
target(n) := object.get(cfg(n), "policy", "")

role(n) := object.get(cfg(n), "role", "")

lock_key(n) := object.get(cfg(n), "lock_key", "")

# lock file を置く state の Bucket。state-backend の plan に在る。
state_bucket(n) := object.get(cfg(n), "state_bucket", "")

# Key Policy に復号を許される role。宣言に無い role に Key Policy が何も許さないことは PLAN-SBW-06 が見る。
key_users := {u | some u in object.get(object.get(data.policy_plan, "state_backend", {}), "key_user_roles", [])}

allowed_actions := {"s3:listbucket", "s3:getobject", "s3:deleteobject"}

object_actions := {"s3:getobject", "s3:deleteobject"}

statements(n) := {{"path": r.document.path, "statement": s} |
	some r in lib.readable
	r.document.type == "aws_iam_role_policy"
	r.document.attr == "policy"
	r.document.address == target(n)
	some s in lib.statements(r.body)
}

allows(n) := {a | some a in statements(n); a.statement.Effect == "Allow"}

actions(s) := lib.lower_all(object.get(s, "Action", []))

after(address) := c.rc.change.after if {
	some c in lib.changes
	c.rc.address == address
}

role_name(n) := after(role(n)).name

role_arn(n) := after(role(n)).arn

policy_name(n) := object.get(after(target(n)), "name", null)

bucket_arn(n) := after(state_bucket(n)).arn

lock_arn(n) := sprintf("%s/%s", [bucket_arn(n), lock_key(n)])

# IAM が削除を許す lock file の object。Key Policy の復号の条件と同じでなければならない。
lock_objects(n) := {res |
	some a in allows(n)
	some act in actions(a.statement)
	act in object_actions
	some res in lib.as_array(object.get(a.statement, "Resource", []))
}

key_policies contains r if {
	some r in lib.readable
	r.document.type == "aws_kms_key_policy"
}

deny contains msg if {
	not is_array(object.get(di, "unlock_roles", null))
	msg := "[PLAN-UNLOCK-00] lock を外す role の宣言（data.policy_plan.deployment_identity.unlock_roles）が配列ではありません"
}

deny contains msg if {
	is_array(object.get(di, "unlock_roles", null))
	count(names) == 0
	msg := "[PLAN-UNLOCK-00] lock を外す role の宣言（data.policy_plan.deployment_identity.unlock_roles）が空です"
}

# state_backend.roots の unlocker を unlock_roles から外すと、その role のどの検査も走らない。
deny contains msg if {
	some root in object.get(object.get(data.policy_plan, "state_backend", {}), "roots", [])
	unlocker := object.get(root, "unlocker", "")
	is_string(unlocker)
	unlocker != ""
	not unlocker in names
	msg := sprintf("[PLAN-UNLOCK-00] state_backend.roots の unlocker の %s が data.policy_plan.deployment_identity.unlock_roles にありません", [unlocker])
}

deny contains msg if {
	some n in names
	not is_object(object.get(di, n, null))
	msg := sprintf("[PLAN-UNLOCK-00] data.policy_plan.deployment_identity.%s が宣言にありません", [n])
}

deny contains msg if {
	some n in names
	count({d | some d in lib.documents; d.type == "aws_iam_role_policy"; d.address == target(n)}) == 0
	msg := sprintf("[PLAN-UNLOCK-00] %s の policy %q が入力にありません。address が変わったなら data.policy_plan.deployment_identity.%s を直してください", [n, target(n), n])
}

role_known(n) if is_string(role_name(n))

arn_known(n) if is_string(role_arn(n))

bucket_known(n) if is_string(bucket_arn(n))

deny contains msg if {
	some n in names
	not role_known(n)
	msg := sprintf("[PLAN-UNLOCK-00] %s の role %q の名前が入力にありません。address が変わったなら data.policy_plan.deployment_identity.%s を直してください", [n, role(n), n])
}

deny contains msg if {
	some n in names
	not arn_known(n)
	msg := sprintf("[PLAN-UNLOCK-00] %s の role %q の ARN を入力から読めません。tests/policy-input.tftest.hcl の override_resource で与えてください", [n, role(n)])
}

deny contains msg if {
	some n in names
	not bucket_known(n)
	msg := sprintf("[PLAN-UNLOCK-00] state の Bucket %q の ARN を入力から読めません。address が変わったなら data.policy_plan.deployment_identity.%s を直し、unknown なら tests/policy-input.tftest.hcl の override_resource で与えてください", [state_bucket(n), n])
}

deny contains msg if {
	count(key_policies) == 0
	msg := "[PLAN-UNLOCK-00] 突き合わせる Key Policy（aws_kms_key_policy）が入力にありません"
}

deny contains msg if {
	some n in names
	lock_key(n) == ""
	msg := sprintf("[PLAN-UNLOCK-00] lock file の key（data.policy_plan.deployment_identity.%s.lock_key）が宣言にありません", [n])
}

# 理由は deployment-identity/ADR-0201 決定11。
deny contains msg if {
	some n in names
	some m in names
	n < m
	lock_key(n) == lock_key(m)
	msg := sprintf("[PLAN-UNLOCK-00] %s と %s が同じ lock file %q を受け持っています", [n, m, lock_key(n)])
}

deny contains msg if {
	some n in names
	some a in statements(n)
	some k in ["NotAction", "NotResource"]
	a.statement[k]
	msg := sprintf("[PLAN-UNLOCK-01] %s: %s の role の文が %s を使っています。Action と Resource を列挙してください", [a.path, n, k])
}

deny contains msg if {
	some n in names
	some a in allows(n)
	some act in actions(a.statement)
	not act in allowed_actions
	msg := sprintf("[PLAN-UNLOCK-01] %s: %s の role の Allow に %s があります。許すのは s3:ListBucket・s3:GetObject・s3:DeleteObject だけです", [a.path, n, act])
}

# Bucket の ARN が読めなければ PLAN-UNLOCK-00 が落とす。下の2つは ARN を読めたときだけ評価する。
deny contains msg if {
	some n in names
	bucket_known(n)
	some a in allows(n)
	some act in actions(a.statement)
	act in object_actions
	lib.as_array(object.get(a.statement, "Resource", [])) != [lock_arn(n)]
	msg := sprintf("[PLAN-UNLOCK-02] %s: %s の role の %s の Resource が、state の Bucket の lock file %s の1つではありません", [a.path, n, act, lock_arn(n)])
}

deny contains msg if {
	some n in names
	bucket_known(n)
	some a in allows(n)
	"s3:listbucket" in actions(a.statement)
	lib.as_array(object.get(a.statement, "Resource", [])) != [bucket_arn(n)]
	msg := sprintf("[PLAN-UNLOCK-02] %s: %s の role の s3:ListBucket の Resource が、state の Bucket %s の1つではありません", [a.path, n, bucket_arn(n)])
}

deny contains msg if {
	some n in names
	some a in allows(n)
	"s3:listbucket" in actions(a.statement)
	not prefix_limited(a.statement, lock_key(n))
	msg := sprintf("[PLAN-UNLOCK-02] %s: %s の role の s3:ListBucket が、s3:prefix を StringEquals で %s だけに限っていません", [a.path, n, lock_key(n)])
}

# StringLike や IfExists では限らない。
prefix_limited(s, key) if {
	some op, kv in object.get(s, "Condition", {})
	lower(op) == "stringequals"
	some k, v in kv
	lower(k) == "s3:prefix"
	lib.as_array(v) == [key]
}

# policy の resource は role を名前で指すので、名前が unknown なものはどの role に付くか読めない。
attached contains {"path": c.path, "address": c.rc.address, "type": c.rc.type, "after": c.rc.change.after, "unknown": object.get(c.rc.change, "after_unknown", {})} if {
	some c in lib.changes
	c.rc.type in {"aws_iam_role_policy", "aws_iam_role_policy_attachment", "aws_iam_role_policies_exclusive", "aws_iam_role_policy_attachments_exclusive"}
}

role_key(a) := "role_name" if endswith(a.type, "_exclusive")

role_key(a) := "role" if not endswith(a.type, "_exclusive")

on_role(a, n) if object.get(a.after, role_key(a), null) == role_name(n)

exclusive(type, n) := {a | some a in attached; a.type == type; on_role(a, n)}

# exclusive の2つは権限を足さず、宣言の外で付いた policy を外すので、ここでは見ない。
deny contains msg if {
	some n in names
	some a in attached
	a.type in {"aws_iam_role_policy", "aws_iam_role_policy_attachment"}
	on_role(a, n)
	a.address != target(n)
	msg := sprintf("[PLAN-UNLOCK-03] %s: %s が %s の role に権限を足しています。この role の権限は %s だけに置いてください", [a.path, a.address, n, target(n)])
}

deny contains msg if {
	some a in attached
	object.get(a.unknown, role_key(a), false) == true
	msg := sprintf("[PLAN-UNLOCK-03] %s: %s がどの role に付くかを plan で読めません", [a.path, a.address])
}

deny contains msg if {
	some n in names
	role_known(n)
	some type in ["aws_iam_role_policies_exclusive", "aws_iam_role_policy_attachments_exclusive"]
	count(exclusive(type, n)) != 1
	msg := sprintf("[PLAN-UNLOCK-03] %s の role %s の %s が %d 件です（1件を期待）", [n, role_name(n), type, count(exclusive(type, n))])
}

deny contains msg if {
	some n in names
	some a in exclusive("aws_iam_role_policies_exclusive", n)
	object.get(a.after, "policy_names", null) != [policy_name(n)]
	msg := sprintf("[PLAN-UNLOCK-03] %s: %s の policy_names が %v ではありません", [a.path, a.address, [policy_name(n)]])
}

deny contains msg if {
	some n in names
	some a in exclusive("aws_iam_role_policy_attachments_exclusive", n)
	object.get(a.after, "policy_arns", null) != []
	msg := sprintf("[PLAN-UNLOCK-03] %s: %s の policy_arns が空ではありません", [a.path, a.address])
}

key_grants(n) := {{"document": r.document, "statement": s} |
	some r in key_policies
	some s in lib.statements(r.body)
	s.Effect == "Allow"
	is_object(s.Principal)
	role_arn(n) in lib.as_array(object.get(s.Principal, "AWS", []))
}

deny contains msg if {
	some n in names
	n in key_users
	arn_known(n)
	count(key_policies) > 0
	count(key_grants(n)) == 0
	msg := sprintf("[PLAN-UNLOCK-04] どの Key Policy も %s の role %s に lock file の復号を許していません", [n, role_arn(n)])
}

deny contains msg if {
	some n in names
	some g in key_grants(n)
	lib.lower_all(g.statement.Action) != ["kms:decrypt"]
	msg := sprintf("[PLAN-UNLOCK-04] %s: %s が %s の role に kms:Decrypt 以外を許しています", [g.document.path, g.document.address, n])
}

deny contains msg if {
	some n in names
	some g in key_grants(n)
	not decrypt_limited_to_lock(g.statement, n)
	msg := sprintf("[PLAN-UNLOCK-04] %s: %s の %s の role への復号が、kms:EncryptionContext:aws:s3:arn で IAM の lock file の object %v に限られていません", [g.document.path, g.document.address, n, sort(lock_objects(n))])
}

decrypt_limited_to_lock(s, n) if {
	vs := lib.limiting_values(s, "kms:encryptioncontext:aws:s3:arn")
	count(vs) > 0
	every v in vs {
		lib.no_wildcard(v)
	}
	{v | some v in vs} == lock_objects(n)
}

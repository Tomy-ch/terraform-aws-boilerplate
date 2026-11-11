# deployment-identity の apply の role が、state の鍵の Key Policy がアカウントへ委任する Action のすべてを Deny すること
# （ADR-0604 決定10、deployment-identity/ADR-0201 決定5）。KMS では IAM の Allow は委任された Action にしか効かないので、
# 委任の一覧と Deny の一覧がずれると、apply の role に KMS の Allow を足した日に鍵の設定を変えられるようになる。
# 2つの一覧は別の root（state-backend と deployment-identity）の plan に在るので、結合した入力で突き合わせる。
package plan.deployment_identity_apply_role

import data.plan.lib

cfg := object.get(object.get(data.policy_plan, "deployment_identity", {}), "apply_role", {})

# index key 付きの address（["apply"]）で宣言する。同じ resource の plan と apply-identity の instance と分けるため。
default target := ""

target := object.get(cfg, "policy", "")

account_root(arn) if regex.match(`^arn:aws[a-z-]*:iam::[0-9]{12}:root$`, arn)

key_arn(arn) if regex.match(`^arn:aws[a-z-]*:kms:[^:]+:[0-9]{12}:key/`, arn)

key_policies contains r if {
	some r in lib.readable
	r.document.type == "aws_kms_key_policy"
}

delegated(body) := {a |
	some s in lib.statements(body)
	s.Effect == "Allow"
	is_object(s.Principal)
	some p in lib.as_array(object.get(s.Principal, "AWS", []))
	account_root(p)
	some a in lib.lower_all(s.Action)
}

# 鍵の ARN を Resource に持つ Deny の Action。apply の role の Deny は鍵ごとに置くので、鍵の ARN でない Resource の文は数えない。
denied contains a if {
	some r in lib.readable
	r.document.type == "aws_iam_role_policy"
	r.document.address == target
	some s in lib.statements(r.body)
	s.Effect == "Deny"
	some res in lib.as_array(object.get(s, "Resource", []))
	key_arn(res)
	some a in lib.lower_all(s.Action)
}

covered(a) if a in denied

covered(_) if "kms:*" in denied

deny contains msg if {
	count({d | some d in lib.documents; d.type == "aws_iam_role_policy"; d.address == target}) == 0
	msg := sprintf("[PLAN-APPLY-00] apply の role の policy %q が入力にありません。address が変わったなら data.policy_plan.deployment_identity.apply_role を直してください", [target])
}

deny contains msg if {
	count(key_policies) == 0
	msg := "[PLAN-APPLY-00] 突き合わせる Key Policy（aws_kms_key_policy）が入力にありません"
}

deny contains msg if {
	some r in key_policies
	missing := {a | some a in delegated(r.body); not covered(a)}
	count(missing) > 0
	msg := sprintf("[PLAN-APPLY-01] %s: %s がアカウントへ委任する %v を、apply の role %q が鍵の ARN への Deny で拒んでいません", [r.document.path, r.document.address, sort(missing), target])
}

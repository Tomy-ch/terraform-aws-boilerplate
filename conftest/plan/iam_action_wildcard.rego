# ADR-0301 決定8（Allow の Action の "*" と "<service>:*" の禁止）を、同じ効果を持つ NotAction の Allow へ広げる。
# あわせて決定9（Allow の Resource "*" を resource-level 権限を持たない Action に限る）と決定12（KMS の暗号操作を
# IAM で与えない）を見る。
# Security の道具はこのリポジトリの書き方の文書を検査しないので、Policy が持つ（ADR-0402 決定4）。
package plan.iam_action_wildcard

import data.plan.kms_key_policy
import data.plan.lib

deny contains msg if {
	count(lib.documents) == 0
	msg := "[PLAN-IAM-00] policy の文書が0件です。data.policy_plan.documents の表と入力を確かめてください"
}

deny contains msg if {
	some r in lib.readable
	some s in lib.statements(r.body)
	s.Effect == "Allow"
	some a in lib.lower_all(s.Action)
	wildcard(a)
	msg := sprintf("[PLAN-IAM-01] %s: %s の %s が Action %q を許可しています", [r.document.path, r.document.address, r.document.attr, a])
}

deny contains msg if {
	some r in lib.readable
	some s in lib.statements(r.body)
	s.Effect == "Allow"
	s.NotAction
	msg := sprintf("[PLAN-IAM-02] %s: %s の %s が NotAction で許可しています", [r.document.path, r.document.address, r.document.attr])
}

# ADR-0301 決定9: identity-based policy の Allow の Resource "*" は、resource-level 権限を持たない Action に限る。
# resource-based policy（Bucket Policy、Key Policy）の "*" はその resource 自身を指すので見ない。
identity_policy_types := {"aws_iam_role_policy", "aws_iam_user_policy", "aws_iam_group_policy", "aws_iam_policy"}

resource_wildcard_actions := {lower(a) | some a in object.get(data.policy_plan, "resource_wildcard_actions", [])}

deny contains msg if {
	some r in lib.readable
	r.document.type in identity_policy_types
	some s in lib.statements(r.body)
	s.Effect == "Allow"
	"*" in lib.as_array(object.get(s, "Resource", []))
	some a in lib.lower_all(s.Action)
	not a in resource_wildcard_actions
	msg := sprintf("[PLAN-IAM-03] %s: %s の %s が Resource \"*\" で %s を許可しています。resource-level 権限を持たない Action は data.policy_plan.resource_wildcard_actions に宣言してください", [r.document.path, r.document.address, r.document.attr, a])
}

# ADR-0301 決定12: KMS の暗号操作は IAM で与えず、鍵の Key Policy が principal を名指して許す。
# 部分 wildcard（kms:Gen*）も、IAM の照合と同じく暗号操作に当たれば落とす。
deny contains msg if {
	some r in lib.readable
	r.document.type in identity_policy_types
	some s in lib.statements(r.body)
	s.Effect == "Allow"
	some a in lib.lower_all(s.Action)
	some c in kms_key_policy.crypto_actions
	glob.match(a, [], c)
	msg := sprintf("[PLAN-IAM-04] %s: %s の %s が KMS の暗号操作 %s を IAM で許可しています。鍵の Key Policy で principal を名指してください", [r.document.path, r.document.address, r.document.attr, c])
}

# IAM の照合では * は任意の列、? は任意の1文字である。名前の部分が * と ? だけで * を含めば、
# "*" / "<service>:*" と同じ範囲になる（"**"、"s3:**"、"iam:?*"）。
wildcard(a) if regex.match(`^[*?]*\*[*?]*$`, a)

wildcard(a) if regex.match(`^[^:]*:[*?]*\*[*?]*$`, a)

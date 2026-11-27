# ADR-0301 決定8（Allow の Action の "*" と "<service>:*" の禁止）を、同じ効果を持つ NotAction の Allow へ広げる。
# あわせて決定9（Allow の Resource "*" を resource-level 権限を持たない Action に限る）と決定12（KMS の暗号操作を
# IAM で与えない）を見る。
# Security の道具はこのリポジトリの書き方の文書を検査しないので、Policy が持つ（ADR-0402 決定4）。
package plan.iam_action_wildcard

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

# resource-based policy（Bucket Policy、Key Policy）の "*" はその resource 自身を指すので見ない（ADR-0301 決定9）。
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

# NotResource の Allow は、除いた resource の外のすべてを許すので、Resource "*" と同じく扱う。
deny contains msg if {
	some r in lib.readable
	r.document.type in identity_policy_types
	some s in lib.statements(r.body)
	s.Effect == "Allow"
	s.NotResource
	msg := sprintf("[PLAN-IAM-05] %s: %s の %s が NotResource で許可しています", [r.document.path, r.document.address, r.document.attr])
}

# 宣言のうち、結合した入力のどの Resource "*" の Allow にも現れないものは、黙って許可を残すので落とす。
wildcard_resource_actions_in_use contains a if {
	some r in lib.readable
	r.document.type in identity_policy_types
	some s in lib.statements(r.body)
	s.Effect == "Allow"
	"*" in lib.as_array(object.get(s, "Resource", []))
	some a in lib.lower_all(s.Action)
}

deny contains msg if {
	some a in resource_wildcard_actions
	not a in wildcard_resource_actions_in_use
	msg := sprintf("[PLAN-IAM-06] data.policy_plan.resource_wildcard_actions の %s は、入力のどの Resource \"*\" の Allow にも使われていません。使わなくなったなら宣言から外してください", [a])
}

# 部分 wildcard（kms:Gen*）も、IAM の照合と同じく暗号操作に当たれば落とす。区切りを持たない照合にするため null を渡す（ADR-0301 決定12）。
deny contains msg if {
	some r in lib.readable
	r.document.type in identity_policy_types
	some s in lib.statements(r.body)
	s.Effect == "Allow"
	some a in lib.lower_all(s.Action)
	some c in lib.crypto_actions
	glob.match(a, null, c)
	not boundary_via_service(r, s)
	msg := sprintf("[PLAN-IAM-04] %s: %s の %s が KMS の暗号操作 %s を IAM で許可しています。鍵の Key Policy で principal を名指してください", [r.document.path, r.document.address, r.document.attr, c])
}

# 宣言した permissions boundary の文書で、SNS を経た暗号化と復号だけを上限に置く文は外す。boundary は上限を置くだけで権限を
# 与えず、暗号操作は Key Policy が名指す主体に限られる（deployment-identity/ADR-0201 決定16）。Action は kms:Decrypt と
# kms:GenerateDataKey だけ、条件 StringEquals の kms:ViaService はすべて sns.<region>.amazonaws.com、Resource は鍵の ARN だけ。
# instance ごとの boundary は、宣言の address を instance の接頭辞へ付け替えて読む（lib/instances.rego）。
boundary_addresses := {lib.rebase(a, i) | some _, a in object.get(object.get(object.get(data.policy_plan, "deployment_identity", {}), "workload", {}), "boundaries", {}); some i in lib.instances}

boundary_via_service(r, s) if {
	r.document.type == "aws_iam_policy"
	r.document.address in boundary_addresses
	every a in lib.lower_all(s.Action) {
		a in {"kms:decrypt", "kms:generatedatakey"}
	}
	values := lib.as_array(object.get(object.get(object.get(s, "Condition", {}), "StringEquals", {}), "kms:ViaService", []))
	count(values) > 0
	every v in values {
		regex.match(`^sns\.[a-z0-9-]+\.amazonaws\.com$`, v)
	}
	resources := lib.as_array(object.get(s, "Resource", []))
	count(resources) > 0
	every x in resources {
		regex.match(`^arn:aws[a-z-]*:kms:[a-z0-9-]+:[0-9]{12}:key/`, x)
	}
}

# IAM の照合では * は任意の列、? は任意の1文字である。名前の部分が * と ? だけで * を含めば、
# "*" / "<service>:*" と同じ範囲になる（"**"、"s3:**"、"iam:?*"）。
wildcard(a) if regex.match(`^[*?]*\*[*?]*$`, a)

wildcard(a) if regex.match(`^[^:]*:[*?]*\*[*?]*$`, a)

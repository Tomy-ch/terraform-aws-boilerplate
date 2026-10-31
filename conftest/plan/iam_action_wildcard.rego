# ADR-0301 決定8（Allow の Action の "*" と "<service>:*" の禁止）を、同じ効果を持つ NotAction の Allow へ広げる。
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

# IAM の照合では * は任意の列、? は任意の1文字である。名前の部分が * と ? だけで * を含めば、
# "*" / "<service>:*" と同じ範囲になる（"**"、"s3:**"、"iam:?*"）。
wildcard(a) if regex.match(`^[*?]*\*[*?]*$`, a)

wildcard(a) if regex.match(`^[^:]*:[*?]*\*[*?]*$`, a)

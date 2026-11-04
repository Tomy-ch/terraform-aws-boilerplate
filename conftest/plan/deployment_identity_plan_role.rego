# deployment-identity の plan の role の権限（ADR-0602 決定13、deployment-identity/ADR-0201 決定8）。
# plan の role に変更系の権限を与えない。例外は plan の出力の Bucket の pr/* と branch/* への s3:PutObject だけ。
package plan.deployment_identity_plan_role

import data.plan.lib

# index key 付きの address（["plan"]）で宣言する。同じ resource の apply 系の instance と分けるため。
target := object.get(object.get(data.policy_plan, "deployment_identity", {}), "plan_role_policy", "")

# 読み取りの Action は、名前の動詞が Get・List・Describe・Head で始まるものとする。
read_verbs := ["get", "list", "describe", "head"]

plan_output_key := `^arn:aws:s3:::[^/*?]+-tfplan-[^/*?]+/(pr|branch)/\*$`

allows contains {"path": r.document.path, "statement": s} if {
	some r in lib.readable
	r.document.type == "aws_iam_role_policy"
	r.document.attr == "policy"
	r.document.address == target
	some s in lib.statements(r.body)
	s.Effect == "Allow"
}

read_only(action) if {
	parts := split(action, ":")
	count(parts) == 2
	some v in read_verbs
	startswith(parts[1], v)
}

deny contains msg if {
	count({d | some d in lib.documents; d.type == "aws_iam_role_policy"; d.address == target}) == 0
	msg := sprintf("[PLAN-ROLE-00] plan の role の policy %q が入力にありません。address が変わったなら data.policy_plan.deployment_identity.plan_role_policy を直してください", [target])
}

deny contains msg if {
	some a in allows
	some k in ["NotAction", "NotResource"]
	a.statement[k]
	msg := sprintf("[PLAN-ROLE-01] %s: plan の role の Allow が %s を使っています。許す Action と Resource を列挙してください", [a.path, k])
}

deny contains msg if {
	some a in allows
	some action in lib.lower_all(object.get(a.statement, "Action", []))
	action != "s3:putobject"
	not read_only(action)
	msg := sprintf("[PLAN-ROLE-02] %s: plan の role の Allow に変更系の Action %s があります（ADR-0602 決定13）", [a.path, action])
}

deny contains msg if {
	some a in allows
	"s3:putobject" in lib.lower_all(object.get(a.statement, "Action", []))
	some resource in lib.as_array(object.get(a.statement, "Resource", []))
	not regex.match(plan_output_key, resource)
	msg := sprintf("[PLAN-ROLE-03] %s: plan の role の s3:PutObject の Resource %s が、plan の出力の Bucket の pr/* か branch/* ではありません", [a.path, resource])
}

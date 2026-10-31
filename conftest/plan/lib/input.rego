# 入力そのものが読めないことを、どの Policy よりも先に違反として報告する。読めない文書を飛ばすと、
# その文書についての Policy が黙って緑になる。
package plan.input

import data.plan.lib

deny contains msg if {
	count(input) == 0
	msg := "[PLAN-INPUT-01] 入力が0件です"
}

deny contains msg if {
	some f in input
	not f.contents.plan_format_version == data.policy_plan.plan_format_version
	msg := sprintf("[PLAN-INPUT-02] %s: plan_format_version が %s ではありません（入力の形が変わった可能性があります）", [f.path, data.policy_plan.plan_format_version])
}

deny contains msg if {
	some f in input
	not resource_changes_present(f)
	msg := sprintf("[PLAN-INPUT-03] %s: resource_changes が空か、存在しません", [f.path])
}

deny contains msg if {
	some d in lib.documents
	lib.unknown(d)
	msg := sprintf("[PLAN-INPUT-04] %s: %s の %s が plan で決まっていません。参照する ARN を tests/policy.tftest.hcl の override_resource で与えてください", [d.path, d.address, d.attr])
}

deny contains msg if {
	some d in lib.documents
	not lib.unknown(d)
	not lib.parsed(d)
	msg := sprintf("[PLAN-INPUT-05] %s: %s の %s が JSON の文書として読めません", [d.path, d.address, d.attr])
}

resource_changes_present(f) if count(f.contents.resource_changes) > 0

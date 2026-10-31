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

# JSON として読めても、IAM の文書の形でなければ Policy の規則は未定義になって黙る。形をここで固定する。
deny contains msg if {
	some r in lib.readable
	not well_formed(r.body)
	msg := sprintf("[PLAN-INPUT-06] %s: %s の %s が IAM の policy 文書の形（Statement と、その Effect / Action / NotAction / Condition の型）ではありません", [r.document.path, r.document.address, r.document.attr])
}

resource_changes_present(f) if count(f.contents.resource_changes) > 0

well_formed(body) if {
	is_object(body)
	statement_container(body.Statement)
	every s in lib.statements(body) {
		well_formed_statement(s)
	}
}

statement_container(x) if is_array(x)

statement_container(x) if is_object(x)

well_formed_statement(s) if {
	is_object(s)
	is_string(s.Effect)
	strings_or_absent(s, "Action")
	strings_or_absent(s, "NotAction")
	object_or_absent(s, "Condition")
}

strings_or_absent(s, key) if not key in object.keys(s)

strings_or_absent(s, key) if is_string(s[key])

strings_or_absent(s, key) if {
	is_array(s[key])
	every x in s[key] {
		is_string(x)
	}
}

object_or_absent(s, key) if not key in object.keys(s)

object_or_absent(s, key) if is_object(s[key])

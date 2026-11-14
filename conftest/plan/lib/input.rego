# 入力そのものが読めないことを、どの Policy よりも先に違反として報告する。読めない文書を飛ばすと、
# その文書についての Policy が黙って緑になる。Policy が読まない経路で設定を書く引数（ADR-0402 決定17・18）も、
# 入力の欠けとしてここで落とす。
package plan.input

import data.plan.lib

# 宣言そのものが欠けると、それを参照する規則が未定義になって黙る。
deny contains msg if {
	not well_formed_declaration(data.policy_plan)
	msg := "[PLAN-DATA-01] 宣言の policy_plan が、plan_format_version / provider_format_version / documents / required_documents / deployment_identity / inline_arguments の形を満たしていません"
}

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
	msg := sprintf("[PLAN-INPUT-04] %s: %s の %s が plan で決まっていません。参照する ARN を tests/policy-input.tftest.hcl の override_resource で与えてください", [d.path, d.address, d.attr])
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
	msg := sprintf("[PLAN-INPUT-06] %s: %s の %s が IAM の policy 文書の形（Statement と、その Effect / Action / NotAction / Resource / NotResource / Condition の型）ではありません", [r.document.path, r.document.address, r.document.attr])
}

# 表に載せた型が入力のどこにも現れないなら、その型の文書は1つも検査されていない。
deny contains msg if {
	some type in data.policy_plan.required_documents
	count({d | some d in lib.documents; d.type == type}) == 0
	msg := sprintf("[PLAN-INPUT-07] %s の文書が入力のどこにもありません（data.policy_plan.required_documents）", [type])
}

# 形の崩れた変更は lib.changes に入らず、その文書は黙って読まれない。
deny contains msg if {
	some f in input
	some rc in f.contents.resource_changes
	not well_formed_change(rc)
	msg := sprintf("[PLAN-INPUT-08] %s: resource_changes の要素が address / type / change.actions の形を満たしていません", [f.path])
}

# 宣言した引数に、書かないときの値でない値か、plan で決まらない値が在る。
inline_writes contains {"path": c.path, "address": c.rc.address, "arg": arg} if {
	some c in lib.changes
	some arg, empty in data.policy_plan.inline_arguments[c.rc.type]
	inline_written(c.rc.change, arg, empty)
}

inline_written(change, arg, _) if lib.unknown_value(object.get(object.get(change, "after_unknown", {}), arg, false))

inline_written(change, arg, empty) if {
	is_object(change.after)
	object.get(change.after, arg, empty) != empty
}

deny contains msg if {
	some w in inline_writes
	msg := sprintf("[PLAN-INPUT-09] %s: %s の %s が書かれています。書かないなら tests/policy-input.tftest.hcl の override_resource で null を与え、書くなら同じ設定を書く別の resource で書いてください", [w.path, w.address, w.arg])
}

inline_seen(type, arg) if {
	some c in lib.changes
	c.rc.type == type
	arg in object.keys(object.get(c.rc.change, "after", {}))
}

inline_seen(type, arg) if {
	some c in lib.changes
	c.rc.type == type
	arg in object.keys(object.get(c.rc.change, "after_unknown", {}))
}

# 宣言が入力に当たらなければ、その引数は1つも検査されていない。
deny contains msg if {
	some type, args in data.policy_plan.inline_arguments
	some arg, _ in args
	not inline_seen(type, arg)
	msg := sprintf("[PLAN-INPUT-10] inline_arguments の %s の %s が、入力のどの変更にもありません", [type, arg])
}

# data.provider_schema は make tf-policy が mock の plan の provider_schemas から取り出す（型 → 書ける非推奨の引数）。
deprecated_arguments_present if {
	is_object(data.provider_schema.deprecated_arguments)
	count(data.provider_schema.deprecated_arguments) > 0
}

deny contains msg if {
	not deprecated_arguments_present
	msg := "[PLAN-INPUT-11] mock の plan の Provider の schema から、非推奨の引数の一覧を得られていません"
}

deny contains msg if {
	deprecated_arguments_present
	some type, _ in data.policy_plan.inline_arguments
	not type in object.keys(data.provider_schema.deprecated_arguments)
	msg := sprintf("[PLAN-INPUT-11] %s が mock の plan の Provider の schema にありません", [type])
}

deny contains msg if {
	some type, args in data.provider_schema.deprecated_arguments
	type in object.keys(data.policy_plan.inline_arguments)
	some arg in args
	not arg in object.keys(data.policy_plan.inline_arguments[type])
	msg := sprintf("[PLAN-INPUT-11] %s の非推奨の引数 %s が inline_arguments にありません", [type, arg])
}

resource_changes_present(f) if {
	is_array(f.contents.resource_changes)
	count(f.contents.resource_changes) > 0
}

well_formed_change(rc) if {
	is_string(rc.address)
	is_string(rc.type)
	is_array(rc.change.actions)
}

well_formed_declaration(decl) if {
	is_string(decl.plan_format_version)
	is_string(decl.provider_format_version)
	is_object(decl.documents)
	count(decl.documents) > 0
	every attrs in decl.documents {
		is_array(attrs)
		count(attrs) > 0
	}
	is_array(decl.required_documents)
	count(decl.required_documents) > 0
	every type in decl.required_documents {
		type in object.keys(decl.documents)
	}
	is_array(decl.deployment_identity.roles)
	count(decl.deployment_identity.roles) > 0
	every role in decl.deployment_identity.roles {
		is_string(role)
	}
	is_string(decl.deployment_identity.oidc_provider)
	well_formed_inline(decl)
}

well_formed_inline(decl) if {
	lib.inline_arguments_shape(decl)
	every type, _ in decl.inline_arguments {
		type in decl.stateful_protection.protected_types
	}
}

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
	s.Effect in {"Allow", "Deny"}
	strings_or_absent(s, "Action")
	strings_or_absent(s, "NotAction")
	strings_or_absent(s, "Resource")
	strings_or_absent(s, "NotResource")
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

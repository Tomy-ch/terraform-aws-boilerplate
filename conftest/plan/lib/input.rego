# 入力そのものが読めないことを、どの Policy よりも先に違反として報告する。読めない文書を飛ばすと、
# その文書についての Policy が黙って緑になる。Policy が読まない経路で設定を書く引数（ADR-0402 決定17・18）も、
# 入力の欠けとしてここで落とす。
package plan.input

import data.plan.lib

# 宣言そのものが欠けると、それを参照する規則が未定義になって黙る。
deny contains msg if {
	not well_formed_policy_plan(data.policy_plan)
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

inline_writes contains {"path": c.path, "address": c.rc.address, "arg": arg} if {
	some c in lib.changes
	some arg, empty in lib.inline_arguments[c.rc.type]
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

# 決定17 の宣言は、層の宣言から導ける引数を持たない（同じ引数を2つの宣言に別々に書かない）。層の宣言が
# 読めなければ、導く分が黙って欠けるので落とす。
deny contains msg if {
	not lib.layer_decl_ok
	msg := "[PLAN-INPUT-12] 層の宣言 data.layer_decl（conftest/hcl/layer/decl.json）を読めていません。inline_arguments の一部はそこから導きます"
}

deny contains msg if {
	some type, args in data.policy_plan.inline_arguments
	some arg, _ in args
	arg in object.keys(object.get(lib.derived_inline_arguments, type, {}))
	msg := sprintf("[PLAN-INPUT-12] policy_plan.inline_arguments の %s の %s は、層の宣言（ADR-0102 決定18）から導かれます → policy_plan から外す", [type, arg])
}

# 宣言が入力に当たらなければ、その引数は1つも検査されていない。
deny contains msg if {
	some type, args in lib.inline_arguments
	some arg, _ in args
	not inline_seen(type, arg)
	msg := sprintf("[PLAN-INPUT-10] inline_arguments の %s の %s が、入力のどの変更にもありません", [type, arg])
}

# data.provider_schema は、型 → 書ける非推奨の引数（ADR-0402 決定18）。
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
	some type, _ in lib.inline_arguments
	not type in object.keys(data.provider_schema.deprecated_arguments)
	msg := sprintf("[PLAN-INPUT-11] %s が mock の plan の Provider の schema にありません", [type])
}

deny contains msg if {
	some type, args in data.provider_schema.deprecated_arguments
	type in object.keys(lib.inline_arguments)
	some arg in args
	not arg in object.keys(lib.inline_arguments[type])
	msg := sprintf("[PLAN-INPUT-11] %s の非推奨の引数 %s が inline_arguments にありません", [type, arg])
}

# 層の宣言で attachment 型の親になる保護の対象の型は、非推奨の引数を層の宣言で必ず分類する（ADR-0102 決定18）ので、
# 実効の宣言に型ごと現れる。現れなければ、層の宣言の欠けで導く分が型ごと消えている。
deny contains msg if {
	lib.layer_decl_ok
	some type in data.policy_plan.stateful_protection.protected_types
	type in lib.layer_parent_types
	count({arg | some arg in object.get(data.provider_schema.deprecated_arguments, type, []); not lib.owned(type, arg)}) > 0
	not type in object.keys(lib.inline_arguments)
	msg := sprintf("[PLAN-INPUT-11] 保護の対象の型 %s は、層の宣言で attachment 型の親で非推奨の引数を持つのに、inline_arguments にありません", [type])
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

well_formed_policy_plan(decl) if {
	lib.raw_inline_shape(decl)
	well_formed_declaration(lib.effective_policy_plan)
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

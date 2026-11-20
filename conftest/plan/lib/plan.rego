# Policy の入力（conftest test --combine）を読む共通部。入力の各要素は {path, contents} で、contents は
# plan の JSON 表現である（ADR-0402 決定11）。Policy は contents.resource_changes だけを読む。
package plan.lib

# 削除だけの変更は、変更後の文書を持たない。data source の読み取りは作る文書ではない。
changes contains {"path": f.path, "rc": rc} if {
	some f in input
	some rc in f.contents.resource_changes
	object.get(rc, "mode", "managed") == "managed"
	rc.change.actions != ["delete"]
}

# mock provider は表に無い computed な属性に乱数を入れるので、属性名だけで拾わない。文書でない属性は inline_arguments が見る。
documents contains {"path": c.path, "address": c.rc.address, "type": c.rc.type, "attr": attr, "change": c.rc.change} if {
	some c in changes
	some attr in data.policy_plan.documents[c.rc.type]
}

unknown(d) if d.change.after_unknown[d.attr] == true

parsed(d) := json.unmarshal(d.change.after[d.attr]) if {
	is_string(d.change.after[d.attr])
	json.is_valid(d.change.after[d.attr])
}

readable contains {"document": d, "body": parsed(d)} if some d in documents

# index key（[0]、["plan"]）を除いた address。宣言のうち index key を持たない形の address と突き合わせる。
strip_index(address) := regex.replace(address, `\[[^\]]*\]`, "")

as_array(x) := x if is_array(x)

as_array(x) := [x] if not is_array(x)

statements(body) := as_array(body.Statement)

# IAM は Action の大文字小文字を区別しない。
lower_all(xs) := [lower(x) | some x in as_array(xs)]

# IAM の Action と Resource の * と ? は ":" や "/" も越え、ほかの文字は文字そのものである。glob.match は [ と { を
# 構文として読み、閉じていないと評価が未定義になって規則が黙るので使わない。
iam_regex(p) := strings.replace_n({"?": ".", "*": ".*"}, regex.replace(p, `[.+()\[\]{}|^$\\]`, `\${0}`))

# IAM の policy 変数（${...}）は要求の時点で置き換わる。
policy_variable(v) if contains(v, "${")

# policy 変数を任意の文字列と読んだ IAM の pattern。最初の ${ から最後の } まで（閉じなければ末尾まで）を * に置くので、
# 入れ子や既定値の中の } と改行でも、当たりうる範囲を狭めない（ADR-0402 決定21）。
policy_variable_as_wildcard(p) := regex.replace(p, `(?s)\$\{(.*\}|.*$)`, "*")

iam_match(pattern, value) if regex.match(sprintf("^%s$", [iam_regex(pattern)]), value)

# 鍵の暗号操作と grant。Key Policy のアカウントへの委任と IAM の Allow に置かない（ADR-0301 決定12）。
crypto_actions := {
	"kms:creategrant", "kms:decapsulate", "kms:decrypt", "kms:derivesharedsecret", "kms:encapsulate", "kms:encrypt", "kms:generatedatakey",
	"kms:generatedatakeypair", "kms:generatedatakeypairwithoutplaintext", "kms:generatedatakeywithoutplaintext",
	"kms:generatemac", "kms:getpublickey", "kms:reencryptfrom", "kms:reencryptto", "kms:sign", "kms:verify",
	"kms:verifymac",
}

# plan で決まらない値は、after_unknown の true か、block の中の true で表れる。
unknown_value(x) if x == true

unknown_value(x) if {
	walk(x, [_, v])
	v == true
}

# ADR-0402 決定17 の実効の宣言。Policy Test が持つ宣言（data.policy_plan.inline_arguments）と、ADR-0102 決定18 の
# 宣言（data.layer_decl。conftest/hcl/layer/decl.json）から導いた分の和である。inline_owned の引数は導かない。
default layer_decl_ok := false

layer_decl_ok if {
	is_object(data.layer_decl.attachments)
	count(data.layer_decl.attachments) > 0
	is_object(data.layer_decl.unrelated_deprecated_arguments)
	is_object(data.layer_decl.inline_owned)
}

derived_inline_arguments := {t: args |
	some t in data.policy_plan.stateful_protection.protected_types
	args := object.union(
		{arg: v |
			some _, a in data.layer_decl.attachments
			some arg, v in object.get(object.get(a, "parent_arguments", {}), t, {})
			not owned(t, arg)
		},
		{arg: e.unwritten |
			some arg, e in object.get(data.layer_decl.unrelated_deprecated_arguments, t, {})
			not owned(t, arg)
		},
	)
	count(args) > 0
} if layer_decl_ok

default derived_inline_arguments := {}

owned(t, arg) if data.layer_decl.inline_owned[t][arg]

layer_parent_types := {t | some _, a in data.layer_decl.attachments; some _, ts in a.parents; some t in ts} if layer_decl_ok

default layer_parent_types := set()

inline_arguments := object.union(derived_inline_arguments, data.policy_plan.inline_arguments) if is_object(data.policy_plan.inline_arguments)

default inline_arguments := null

effective_policy_plan := object.union(data.policy_plan, {"inline_arguments": inline_arguments}) if is_object(data.policy_plan)

# Policy Test が持つ宣言そのもの（導く前）の形。導く分が在るので空でもよい。ほかは inline_arguments_shape と同じ。
raw_inline_shape(decl) if {
	is_object(decl.inline_arguments)
	every _, args in decl.inline_arguments {
		is_object(args)
		count(args) > 0
		every _, empty in args {
			empty in [null, []]
		}
	}
}

# ADR-0402 決定17 の宣言 inline_arguments の形。CI の実 plan は plan.input を掛けないので、plan.stateful_protection も同じ形を見る。
inline_arguments_shape(decl) if {
	is_object(decl.inline_arguments)
	count(decl.inline_arguments) > 0
	every type, args in decl.inline_arguments {
		is_object(args)
		count(args) > 0
		every arg, empty in args {
			empty in [null, []]
			not arg in object.get(decl.documents, type, [])
		}
	}
}

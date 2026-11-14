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

# data の address 一覧は index key を除いた形で持つ（ADR-0402 決定15）。
strip_index(address) := regex.replace(address, `\[[^\]]*\]`, "")

as_array(x) := x if is_array(x)

as_array(x) := [x] if not is_array(x)

statements(body) := as_array(body.Statement)

# IAM は Action の大文字小文字を区別しない。
lower_all(xs) := [lower(x) | some x in as_array(xs)]

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

# ADR-0402 決定17 の宣言 inline_arguments の形。型ごとに空でない object で、値は書かないときの値（null か []）、
# documents の型と属性と重ならない。CI の実 plan は plan.input を掛けないので、plan.stateful_protection も同じ形を見る。
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

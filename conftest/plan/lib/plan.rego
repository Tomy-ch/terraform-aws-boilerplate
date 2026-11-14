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

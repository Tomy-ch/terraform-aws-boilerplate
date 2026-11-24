# インフラ内コードの検出（ADR-0103 決定9・10）。input は scripts/hcl-input が走査の root の下から作る JSON
# で、path はリポジトリの root からの相対パス。入力の形と数の検査は input.rego が持つ。
package infra_code

import data.lib.hcl_input as hcl

decl := data.infra_code_decl

# must_execute は、決定9 が決定5 の側に置くと定めた型。宣言がこれを決定5 でない側に置けば落とす。
must_execute := {
	"resource.aws_instance",
	"resource.aws_launch_configuration",
	"resource.aws_launch_template",
	"resource.aws_lightsail_instance",
	"resource.aws_spot_fleet_request",
	"resource.aws_spot_instance_request",
	"resource.aws_lambda_invocation",
	"data.aws_lambda_invocation",
	"action.aws_lambda_invoke",
	"data.external",
}

decl_keys := {"types", "providers", "languages", "allowlist"}

type_keys := {"executes_code", "body_args", "path_args"}

provider_keys := {"command_blocks"}

item_keys := {"id", "owner", "source", "language", "adr", "repository", "reasons"}

# reason_code は、許可リストの項目の reasons に書く理由の符号（ADR-0103 決定12）。型（決定9）、provisioner、provider の
# 設定の exec、中を読めない provider の設定、宣言した provider の block（command_block:<block>）の5つ。
reason_code := `^(type|provisioner|provider_exec|unresolved|command_block:[a-z0-9_]+(\.[a-z0-9_]+)*)$`

components := hcl.components(doc)

providers := hcl.providers(doc)

detected contains {"id": c.id, "path": c.path, "code": "type", "reason": sprintf("コードを実行させる型 %s", [c.key])} if {
	some c in components
	decl.types[c.key].executes_code == true
}

detected contains {"id": c.id, "path": c.path, "code": "provisioner", "reason": "provisioner"} if {
	some c in components
	hcl.has_provisioner(c.scope)
}

detected contains {"id": r.id, "path": r.path, "code": "provisioner", "reason": "provisioner"} if {
	some r in hcl.removed_blocks(doc)
	hcl.has_provisioner(r.scope)
}

detected contains {"id": p.id, "path": p.path, "code": "provider_exec", "reason": "provider の exec"} if {
	some p in providers
	hcl.has_key_anywhere(p.scope, "exec")
}

# 中を読めない式の中に exec などのコマンドの block が在るかは決められないので、検出として許可リストに委ねる。
detected contains {"id": p.id, "path": p.path, "code": "unresolved", "reason": "解けない式"} if {
	some p in providers
	hcl.unresolved_expr(p.scope)
}

# コマンドの block を宣言した provider では、参照・関数の呼び出し・補間の値も中を読めない式とする。参照先の locals や
# variables、呼び出しの結果に exec などのコマンドの block が在るかを、provider の block からは決められない。
detected contains {"id": p.id, "path": p.path, "code": "unresolved", "reason": "解けない式"} if {
	some p in providers
	source := hcl.provider_source(doc, p.module, p.name)
	count(decl.providers[source].command_blocks) > 0
	non_literal_value(p.scope)
}

non_literal_value(s) if {
	some a in hcl.attrs_in(s)
	walk(a.expr, [_, v])
	is_object(v)
	v.kind in {"traversal", "call", "template"}
}

detected contains {"id": p.id, "path": p.path, "code": concat("", ["command_block:", block]), "reason": sprintf("provider の %s", [block])} if {
	some p in providers
	source := hcl.provider_source(doc, p.module, p.name)
	some block in decl.providers[source].command_blocks
	count(hcl.values_at(p.scope, block)) > 0
}

detected_ids := {d.id | some d in detected}

allowlisted contains item.id if some item in decl.allowlist

# allowed は、許可リストの項目が受け入れる {id, 理由の符号} の対（ADR-0103 決定12）。
allowed contains [item.id, code] if {
	some item in decl.allowlist
	is_array(item.reasons)
	some code in item.reasons
}

deny contains msg if {
	some c in components
	not decl.types[c.key]
	msg := sprintf("IC010 [未分類の型] %s: %s は型の分類にありません → 宣言（infra_code_decl）の types に1行足す（code owner の review）", [c.path, c.key])
}

deny contains msg if {
	some d in detected
	not d.id in allowlisted
	msg := sprintf("IC011 [許可リストに無い] %s: %s（%s）はインフラ内コードです → allowlist に項目を足し、本リポジトリ所有なら ADR-0103 決定13 の ADR を書く", [d.path, d.id, d.reason])
}

deny contains msg if {
	some d in detected
	d.id in allowlisted
	not [d.id, d.code] in allowed
	msg := sprintf("IC011 [理由が許可リストに無い] %s: %s の項目は、この検出の理由 %s（%s）を reasons に挙げていません → 意図したコードなら reasons に足し、そうでなければ取り除く", [d.path, d.id, d.code, d.reason])
}

deny contains msg if {
	some key in must_execute
	decl.types[key]
	decl.types[key].executes_code != true
	msg := sprintf("IC012 [分類の誤り] %s はコードを実行させる型です（ADR-0103 決定9） → executes_code を true にする", [key])
}

deny contains msg if {
	some p in providers
	not hcl.declared_source(doc, p.module, p.name)
	source := hcl.provider_source(doc, p.module, p.name)
	not decl.providers[source]
	msg := sprintf("IC013 [未分類の provider] %s: %s は provider の分類にありません → 宣言（infra_code_decl）の providers に1行足す", [p.path, source])
}

# required_providers の source は、provider ブロックが無くても、その provider のバイナリを init で走らせる。
deny contains msg if {
	some [dir, name, source] in hcl.required_sources(doc)
	is_string(source)
	not decl.providers[hcl.normalize_source(source)]
	msg := sprintf("IC013 [未分類の provider] %s: required_providers の %s（%s）は provider の分類にありません → 宣言（infra_code_decl）の providers に1行足す", [dir, name, hcl.normalize_source(source)])
}

deny contains msg if {
	some [dir, name, source] in hcl.required_sources(doc)
	source == null
	msg := sprintf("IC014 [解釈できない provider] %s: required_providers の %s の source がリテラルではありません", [dir, name])
}


deny contains "IC004 [宣言の形] infra_code_decl の types がありません" if not decl.types

deny contains msg if {
	some i, item in decl.allowlist
	some key in ["id", "owner"]
	not item[key]
	msg := sprintf("IC004 [宣言の形] allowlist の %d 番目の項目に %s がありません", [i, key])
}

deny contains msg if {
	some key, _ in decl
	not key in decl_keys
	msg := sprintf("IC004 [宣言の形] infra_code_decl のキー %s を解釈できません", [key])
}

deny contains msg if {
	some type, class in decl.types
	some key, _ in class
	not key in type_keys
	msg := sprintf("IC004 [宣言の形] types.%s のキー %s を解釈できません", [type, key])
}

deny contains msg if {
	some type, class in decl.types
	not is_boolean(object.get(class, "executes_code", null))
	msg := sprintf("IC004 [宣言の形] types.%s の executes_code は true か false で書く", [type])
}

deny contains msg if {
	some source, class in decl.providers
	some key, _ in class
	not key in provider_keys
	msg := sprintf("IC004 [宣言の形] providers.%s のキー %s を解釈できません", [source, key])
}

deny contains msg if {
	some item in decl.allowlist
	not well_formed_reasons(object.get(item, "reasons", null))
	msg := sprintf("IC004 [宣言の形] allowlist の項目 %v の reasons が、理由の符号（type・provisioner・provider_exec・unresolved・command_block:<block>）の、重複の無い空でない配列ではありません", [object.get(item, "id", "?")])
}

well_formed_reasons(rs) if {
	is_array(rs)
	count(rs) > 0
	count({r | some r in rs}) == count(rs)
	every r in rs {
		is_string(r)
		regex.match(reason_code, r)
	}
}

deny contains msg if {
	some item in decl.allowlist
	some key, _ in item
	not key in item_keys
	msg := sprintf("IC004 [宣言の形] allowlist の項目 %v のキー %s を解釈できません", [object.get(item, "id", "?"), key])
}

# インフラ内コードの検出（ADR-0103 決定9・10）。`conftest test --combine` で、走査の root の下の全ファイルを
# 1つの入力として評価する。input は `{"path", "contents"}` の配列で、path はリポジトリの root からの相対パス。
package infra_code

import data.lib.hcl

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

item_keys := {"id", "owner", "source", "language", "adr", "repository"}

components := hcl.components(input)

providers := hcl.providers(input)

detected contains {"id": c.id, "path": c.path, "reason": sprintf("コードを実行させる型 %s", [c.key])} if {
	some c in components
	decl.types[c.key].executes_code == true
}

detected contains {"id": c.id, "path": c.path, "reason": "provisioner"} if {
	some c in components
	hcl.has_provisioner(c.body)
}

detected contains {"id": r.id, "path": r.path, "reason": "provisioner"} if {
	some r in hcl.removed_blocks(input)
	hcl.has_provisioner(r.body)
}

detected contains {"id": p.id, "path": p.path, "reason": "provider の exec"} if {
	some p in providers
	hcl.has_key_anywhere(p.body, "exec")
}

detected contains {"id": p.id, "path": p.path, "reason": sprintf("provider の %s", [block])} if {
	some p in providers
	source := hcl.provider_source(input, p.module, p.name)
	some block in decl.providers[source].command_blocks
	count(hcl.values_at(p.body, block)) > 0
}

detected_ids := {d.id | some d in detected}

allowlisted contains item.id if some item in decl.allowlist

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
	some key in must_execute
	decl.types[key]
	decl.types[key].executes_code != true
	msg := sprintf("IC012 [分類の誤り] %s はコードを実行させる型です（ADR-0103 決定9） → executes_code を true にする", [key])
}

deny contains msg if {
	some p in providers
	not hcl.declared_source(input, p.module, p.name)
	source := hcl.provider_source(input, p.module, p.name)
	not contains(source, "${")
	not decl.providers[source]
	msg := sprintf("IC013 [未分類の provider] %s: %s は provider の分類にありません → 宣言（infra_code_decl）の providers に1行足す", [p.path, source])
}

# required_providers の source は、provider ブロックが無くても、その provider のバイナリを init で走らせる。
deny contains msg if {
	some [dir, name, source] in hcl.required_sources(input)
	not contains(source, "${")
	not decl.providers[hcl.normalize_source(source)]
	msg := sprintf("IC013 [未分類の provider] %s: required_providers の %s（%s）は provider の分類にありません → 宣言（infra_code_decl）の providers に1行足す", [dir, name, hcl.normalize_source(source)])
}

deny contains msg if {
	some [dir, name, source] in hcl.required_sources(input)
	contains(source, "${")
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
	some key, _ in item
	not key in item_keys
	msg := sprintf("IC004 [宣言の形] allowlist の項目 %v のキー %s を解釈できません", [object.get(item, "id", "?"), key])
}

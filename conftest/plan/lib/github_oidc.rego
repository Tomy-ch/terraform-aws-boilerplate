# GitHub Actions の OIDC を信頼する role の信頼条件の共通の形（ADR-0602 決定14 の第1項・決定15・決定19）。
package plan.lib

github_issuer := "token.actions.githubusercontent.com"

github_required_keys := {sprintf("%s:%s", [github_issuer, k]) | some k in ["aud", "sub", "repository_id", "repository_owner_id"]}

# 不変形式の sub: repo:<owner>@<owner_id>/<repo>@<repo_id>:environment:<name>
github_sub_pattern := `^repo:[^/@:*?]+@([0-9]+)/[^/@:*?]+@([0-9]+):environment:[^:*?]+$`

# Condition か StringEquals が無い文書でも、これを使う規則が未定義で黙らないよう空の object を返す。
github_string_equals(s) := object.get(object.get(s, "Condition", {}), "StringEquals", {})

github_aud_is_sts(eq) if eq[sprintf("%s:aud", [github_issuer])] == "sts.amazonaws.com"

github_allow_web_identity(s) if {
	s.Effect == "Allow"
	lower_all(s.Action) == ["sts:assumerolewithwebidentity"]
	not s.NotAction
	not s.NotPrincipal
}

github_single_literal(v) if {
	is_string(v)
	not contains(v, "*")
	not contains(v, "?")
}

github_sub_consistent(eq) if {
	m := regex.find_all_string_submatch_n(github_sub_pattern, eq[sprintf("%s:sub", [github_issuer])], 1)
	m[0][1] == eq[sprintf("%s:repository_owner_id", [github_issuer])]
	m[0][2] == eq[sprintf("%s:repository_id", [github_issuer])]
}

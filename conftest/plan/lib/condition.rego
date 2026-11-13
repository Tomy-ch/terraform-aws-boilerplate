# policy の文の Condition を読む共通部。値で限る条件を読む Policy が共有する。
package plan.lib

# 限る条件として認める演算子。IfExists はキーの無い要求を通し、Not 系と Null は限らない。
limiting_operators := {"stringequals", "stringlike", "arnequals", "arnlike"}

# IAM は条件キーの大文字小文字を区別しない。key は小文字で渡す。
limiting_values(s, key) := [v |
	some op, kv in object.get(s, "Condition", {})
	lower(op) in limiting_operators
	some k, vs in kv
	lower(k) == key
	some v in as_array(vs)
]

no_wildcard(v) if {
	is_string(v)
	not contains(v, "*")
	not contains(v, "?")
}

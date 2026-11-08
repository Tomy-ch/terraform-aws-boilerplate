# ADR-0604 決定11・12（stateful な resource の保護）。保護の対象として宣言した型の削除と置換を、root ごとの
# 許可の宣言に載っていない限り落とす。入力の path の basename（.json を除く）を root の名前として読む。
package plan.stateful_protection

# 宣言が無ければ null にして、下の形の検査で落とす。参照のまま未定義にすると、どの規則も黙る。
default decl := null

decl := data.policy_plan.stateful_protection

root_of(path) := trim_suffix(base, ".json") if {
	parts := split(path, "/")
	base := parts[count(parts) - 1]
}

# 置換は actions に delete を含む（["delete", "create"] か ["create", "delete"]）。
removals contains {"root": root_of(f.path), "path": f.path, "address": rc.address, "type": rc.type, "actions": rc.change.actions} if {
	some f in input
	some rc in f.contents.resource_changes
	object.get(rc, "mode", "managed") == "managed"
	"delete" in rc.change.actions
}

allowed(root, address) if {
	some a in object.get(decl.allow, root, [])
	a.address == address
}

matched(root, address) if {
	some r in removals
	r.root == root
	r.address == address
}

# 形の崩れた要素は removals にも許可の突き合わせにも現れないので、ここで落とす。
well_formed_change(rc) if {
	is_string(rc.address)
	is_string(rc.type)
	is_array(rc.change.actions)
}

# state-backend/ADR-0103 決定7（保持日数の短縮）。保護の対象に宣言した lifecycle で、有効な規則が版を消す
# 日数の最小が、変更の前より短くなる変更を落とす。前に日数が無ければ、どの日数も短くなったと読む。
lifecycle_type := "aws_s3_bucket_lifecycle_configuration"

default shorten_allow := {}

shorten_allow := decl.shorten_allow if is_object(decl.shorten_allow)

days(state, block, attr) := {d |
	some r in object.get(state, "rule", [])
	r.status == "Enabled"
	bs := object.get(r, block, [])
	is_array(bs)
	some b in bs
	d := b[attr]
	is_number(d)
	d > 0
}

shorter(before, after, block, attr) if {
	count(days(after, block, attr)) > 0
	count(days(before, block, attr)) == 0
}

shorter(before, after, block, attr) if {
	count(days(before, block, attr)) > 0
	min(days(after, block, attr)) < min(days(before, block, attr))
}

reductions contains {"root": root_of(f.path), "path": f.path, "address": rc.address} if {
	some f in input
	some rc in f.contents.resource_changes
	object.get(rc, "mode", "managed") == "managed"
	rc.type == lifecycle_type
	rc.type in decl.protected_types
	is_object(rc.change.before)
	is_object(rc.change.after)
	some pair in [["noncurrent_version_expiration", "noncurrent_days"], ["expiration", "days"]]
	shorter(rc.change.before, rc.change.after, pair[0], pair[1])
}

shorten_allowed(root, address) if {
	some a in object.get(shorten_allow, root, [])
	a.address == address
}

shorten_matched(root, address) if {
	some r in reductions
	r.root == root
	r.address == address
}

verb(actions) := "置換" if count(actions) > 1

verb(actions) := "削除" if count(actions) == 1

# state Bucket の型を必ず含める（決定11）。宣言が崩れると、下の規則は未定義になって黙る。
well_formed(d) if {
	is_array(d.protected_types)
	"aws_s3_bucket" in d.protected_types
	every t in d.protected_types {
		is_string(t)
	}
	is_object(d.allow)
	every _, entries in d.allow {
		is_array(entries)
		every e in entries {
			is_string(e.address)
			is_string(e.reason)
			e.reason != ""
		}
	}
	is_object(object.get(d, "shorten_allow", {}))
	every _, entries in object.get(d, "shorten_allow", {}) {
		is_array(entries)
		every e in entries {
			is_string(e.address)
			is_string(e.reason)
			e.reason != ""
		}
	}
}

deny contains msg if {
	not well_formed(decl)
	msg := "[PLAN-STATE-00] 宣言の policy_plan.stateful_protection が、protected_types（aws_s3_bucket を含む）と allow・shorten_allow（root ごとの address と空でない reason の並び）の形を満たしていません"
}

deny contains msg if {
	some r in removals
	r.type in decl.protected_types
	not allowed(r.root, r.address)
	msg := sprintf("[PLAN-STATE-01] %s: 保護の対象 %s を%sします。意図した変更なら、root %s の許可の宣言に address と理由を足してください", [r.path, r.address, verb(r.actions), r.root])
}

deny contains msg if {
	some f in input
	root := root_of(f.path)
	some a in object.get(decl.allow, root, [])
	not matched(root, a.address)
	msg := sprintf("[PLAN-STATE-02] %s: 許可の宣言 %s が、この plan のどの削除と置換にも当たりません", [f.path, a.address])
}

deny contains msg if {
	some r in reductions
	not shorten_allowed(r.root, r.address)
	msg := sprintf("[PLAN-STATE-04] %s: %s が版を消す日数を短くします。日数を過ぎた版はすぐに消えます。意図した変更なら、root %s の短縮の許可の宣言に address と理由を足してください", [r.path, r.address, r.root])
}

deny contains msg if {
	some f in input
	root := root_of(f.path)
	some a in object.get(shorten_allow, root, [])
	not shorten_matched(root, a.address)
	msg := sprintf("[PLAN-STATE-05] %s: 短縮の許可の宣言 %s が、この plan のどの日数の短縮にも当たりません", [f.path, a.address])
}

deny contains msg if {
	some f in input
	not is_array(object.get(f.contents, "resource_changes", null))
	msg := sprintf("[PLAN-STATE-03] %s: resource_changes が配列ではありません。plan の JSON の形を読めません", [f.path])
}

deny contains msg if {
	some f in input
	some i, rc in f.contents.resource_changes
	not well_formed_change(rc)
	msg := sprintf("[PLAN-STATE-03] %s: resource_changes[%d] が address・type・change.actions の形を満たしていません", [f.path, i])
}

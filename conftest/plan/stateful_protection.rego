# ADR-0604 決定11・12（stateful な resource の保護）。保護の対象として宣言した型の削除・置換・管理からの除外と、
# 宣言した取り返しのつかない属性の変更を、root ごとの許可の宣言に載っていない限り落とす。入力の path の basename（.json を除く）を root の名前として読む。
package plan.stateful_protection

# 宣言が無ければ null にして、下の形の検査で落とす。参照のまま未定義にすると、どの規則も黙る。
default decl := null

decl := data.policy_plan.stateful_protection

root_of(path) := trim_suffix(base, ".json") if {
	parts := split(path, "/")
	base := parts[count(parts) - 1]
}

# 置換は actions に delete を含む（["delete", "create"] か ["create", "delete"]）。removed block の destroy = false は
# forget になり、実体を残したまま管理から外す。外した後の同じ型の作成が、実体の設定を黙って置き換えうる。
removals contains {"root": root_of(f.path), "path": f.path, "address": rc.address, "type": rc.type, "actions": rc.change.actions} if {
	some f in input
	some rc in f.contents.resource_changes
	object.get(rc, "mode", "managed") == "managed"
	some a in rc.change.actions
	a in {"delete", "forget"}
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
# 日数の最小か失効の日付の最早が、変更の前より早くなる変更を落とす。前に無ければ、どの値も早くなったと読む。
# 変更の前は、同じ address の値か、同じ plan で外す同じ型の値である（address を替えた作成）。
lifecycle_type := "aws_s3_bucket_lifecycle_configuration"

default shorten_allow := {}

shorten_allow := decl.shorten_allow if is_object(decl.shorten_allow)

# RFC 3339 の日付は文字列の順序が時刻の順序と一致するので、日数と同じ min で比べる。
limits(state, block, attr) := {v |
	some r in object.get(state, "rule", [])
	r.status == "Enabled"
	bs := object.get(r, block, [])
	is_array(bs)
	some b in bs
	v := b[attr]
	limit(v)
}

limit(v) if {
	is_number(v)
	v > 0
}

limit(v) if {
	is_string(v)
	v != ""
}

earlier(before, after, block, attr) if {
	count(limits(after, block, attr)) > 0
	count(limits(before, block, attr)) == 0
}

earlier(before, after, block, attr) if {
	count(limits(before, block, attr)) > 0
	min(limits(after, block, attr)) < min(limits(before, block, attr))
}

limit_attrs := [["noncurrent_version_expiration", "noncurrent_days"], ["expiration", "days"], ["expiration", "date"]]

protected_lifecycle(rc) if {
	object.get(rc, "mode", "managed") == "managed"
	rc.type == lifecycle_type
	rc.type in decl.protected_types
}

# 変更の前の値を持つ組。同じ address の更新と置換のほか、同じ plan で外した同じ型と、作った同じ型を組にする。
priors contains {"f": f, "rc": rc, "before": rc.change.before} if {
	some f in input
	some rc in f.contents.resource_changes
	protected_lifecycle(rc)
	is_object(rc.change.before)
	is_object(rc.change.after)
}

priors contains {"f": f, "rc": rc, "before": gone.change.before} if {
	some f in input
	some rc in f.contents.resource_changes
	protected_lifecycle(rc)
	rc.change.actions == ["create"]
	is_object(rc.change.after)
	some gone in f.contents.resource_changes
	protected_lifecycle(gone)
	gone.address != rc.address
	some a in gone.change.actions
	a in {"delete", "forget"}
	is_object(gone.change.before)
}

reductions contains {"root": root_of(p.f.path), "path": p.f.path, "address": p.rc.address} if {
	some p in priors
	some pair in limit_attrs
	earlier(p.before, p.rc.change.after, pair[0], pair[1])
}

# 変更の後の値が apply まで決まらなければ、短くなるかを plan で読めない。
unknown_limit(au) if au == true

unknown_limit(au) if au.status == true

unknown_limit(au) if {
	some pair in limit_attrs
	bs := object.get(au, pair[0], [])
	bs == true
}

unknown_limit(au) if {
	some pair in limit_attrs
	bs := object.get(au, pair[0], [])
	is_array(bs)
	some b in bs
	unknown_block(b, pair[1])
}

unknown_block(b, _) if b == true

unknown_block(b, attr) if b[attr] == true

unknown_lifecycles contains {"path": p.f.path, "address": p.rc.address} if {
	some p in priors
	au := object.get(object.get(p.rc.change, "after_unknown", {}), "rule", [])
	au == true
}

unknown_lifecycles contains {"path": p.f.path, "address": p.rc.address} if {
	some p in priors
	au := object.get(object.get(p.rc.change, "after_unknown", {}), "rule", [])
	is_array(au)
	some r in au
	unknown_limit(r)
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

verb(actions) := "削除" if actions == ["delete"]

verb(actions) := "管理から除外" if actions == ["forget"]

# state Bucket と lifecycle の型を必ず含める（決定11、state-backend/ADR-0103 決定7）。宣言が崩れると、下の規則は未定義になって黙る。
well_formed(d) if {
	is_array(d.protected_types)
	"aws_s3_bucket" in d.protected_types
	lifecycle_type in d.protected_types
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
	msg := "[PLAN-STATE-00] 宣言の policy_plan.stateful_protection が、protected_types（aws_s3_bucket と aws_s3_bucket_lifecycle_configuration を含む）と allow・shorten_allow（root ごとの address と空でない reason の並び）の形を満たしていません"
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
	msg := sprintf("[PLAN-STATE-04] %s: %s が版を消す日数か日付を早めます。日数を過ぎた版はすぐに消えます。意図した変更なら、root %s の短縮の許可の宣言に address と理由を足してください", [r.path, r.address, r.root])
}

deny contains msg if {
	some f in input
	root := root_of(f.path)
	some a in object.get(shorten_allow, root, [])
	not shorten_matched(root, a.address)
	msg := sprintf("[PLAN-STATE-05] %s: 短縮の許可の宣言 %s が、この plan のどの日数の短縮にも当たりません", [f.path, a.address])
}

deny contains msg if {
	some u in unknown_lifecycles
	msg := sprintf("[PLAN-STATE-06] %s: %s が版を消す日数か日付か規則の有効が plan で決まらず、短くなるかを読めません。plan で決まる値にしてください", [u.path, u.address])
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

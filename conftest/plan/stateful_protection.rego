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
}

deny contains msg if {
	not well_formed(decl)
	msg := "[PLAN-STATE-00] 宣言の policy_plan.stateful_protection が、protected_types（aws_s3_bucket を含む）と allow（root ごとの address と空でない reason の並び）の形を満たしていません"
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

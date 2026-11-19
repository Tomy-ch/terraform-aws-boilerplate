# workload root の usecase が作る IAM の role が、その usecase の permissions boundary を持つこと（deployment-identity/
# ADR-0201 決定14・15）。boundary は identity root の plan に在り、workload root の plan とは env が違いうるので、ARN の
# 形（<name_prefix>-ci-boundary-<usecase>）と、role の名前とアカウントとの一致で見る。
package plan.workload_role_boundary

import data.plan.lib

cfg := object.get(data.policy_plan, "workload_role_boundary", null)

well_formed_module(m, d) if {
	is_string(m)
	m != ""
	is_object(d)
	object.keys(d) == {"usecase", "iam_prefix"}
	is_string(d.usecase)
	regex.match(`^[a-z0-9][a-z0-9-]*$`, d.usecase)
	is_string(d.iam_prefix)
	regex.match(`^[a-z0-9][a-z0-9-]*-$`, d.iam_prefix)
	not startswith(d.iam_prefix, "ci-")
}

well_formed if {
	is_object(cfg)
	object.keys(cfg) == {"modules"}
	is_object(cfg.modules)
	count(cfg.modules) > 0
	every m, d in cfg.modules {
		well_formed_module(m, d)
	}
}

# module の index key を除いた address で宣言と突き合わせる。
roles contains {"path": c.path, "rc": c.rc, "module": m, "decl": d} if {
	well_formed
	some c in lib.changes
	c.rc.type == "aws_iam_role"
	some m, d in cfg.modules
	startswith(lib.strip_index(c.rc.address), m)
}

# boundary の ARN が <name_prefix>-ci-boundary-<usecase> で、role と同じアカウントにあり、role の名前が
# <name_prefix>-<usecase の IAM の接頭辞> で始まる。
bounded(r) if {
	b := r.rc.change.after.permissions_boundary
	is_string(b)
	parts := regex.find_all_string_submatch_n(sprintf(`^arn:aws[a-z-]*:iam::([0-9]{12}):policy/(.+)-ci-boundary-%s$`, [r.decl.usecase]), b, 1)
	count(parts) == 1
	startswith(r.rc.change.after.name, sprintf("%s-%s", [parts[0][2], r.decl.iam_prefix]))
	same_account(r.rc.change.after, parts[0][1])
}

# role の ARN が plan で決まらないときは、アカウントの一致を見ない。
same_account(after, _) if not is_string(object.get(after, "arn", null))

same_account(after, account) if split(after.arn, ":")[4] == account

deny contains msg if {
	not well_formed
	msg := "[PLAN-WRB-00] data.policy_plan.workload_role_boundary が無いか、modules（module の address の接頭辞を key に、usecase の名前（usecase）と IAM の名前の接頭辞（iam_prefix。小文字で - で終わり、ci- で始まらない）を値にした空でない object）だけを持つ形を満たしていません"
}

deny contains msg if {
	well_formed
	some m, _ in cfg.modules
	count({r | some r in roles; r.module == m}) == 0
	msg := sprintf("[PLAN-WRB-00] 宣言した module %s の下に aws_iam_role が入力にありません。address が変わったなら data.policy_plan.workload_role_boundary を直してください", [m])
}

deny contains msg if {
	some r in roles
	not bounded(r)
	msg := sprintf("[PLAN-WRB-01] %s: %s の permissions_boundary %v が、同じアカウントの <name_prefix>-ci-boundary-%s ではないか、role の名前が <name_prefix>-%s で始まりません（deployment-identity/ADR-0201 決定14・15）", [r.path, r.rc.address, object.get(r.rc.change.after, "permissions_boundary", null), r.decl.usecase, r.decl.iam_prefix])
}

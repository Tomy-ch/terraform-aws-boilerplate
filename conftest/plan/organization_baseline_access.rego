# IAM Identity Center の permission set と割当の形（ADR-0301 決定10・19、organization-baseline/ADR-0204。
# conftest/plan/README.md の plan.organization_baseline_access）。
package plan.organization_baseline_access

import data.plan.lib

ob := object.get(data.policy_plan, "organization_baseline", {})

access := object.get(ob, "access", {})

declared_sets := object.get(access, "permission_sets", {})

administrator_access_suffix := ":iam::aws:policy/AdministratorAccess"

# 文書を作らずに permission set へ権限を付ける型。付与は排他の resource だけで持つ（organization-baseline/ADR-0204 決定1）。
forbidden_types := {
	"aws_ssoadmin_customer_managed_policy_attachment",
	"aws_ssoadmin_customer_managed_policy_attachments_exclusive",
	"aws_ssoadmin_managed_policy_attachment",
	"aws_ssoadmin_permissions_boundary_attachment",
}

change_at(address) := c.rc.change if {
	some c in lib.changes
	c.rc.address == address
}

after(address) := change_at(address).after

changes_of(type) := [c.rc | some c in lib.changes; c.rc.type == type]

non_empty_string(x) if {
	is_string(x)
	x != ""
}

set_shape(s) if {
	is_object(s)
	object.keys(s) == {"address", "exclusive", "name_suffix", "session_duration", "managed_policy_arns"}
	non_empty_string(s.address)
	non_empty_string(s.exclusive)
	regex.match(`^[a-z][a-z0-9-]*$`, s.name_suffix)
	non_empty_string(s.session_duration)
	is_array(s.managed_policy_arns)
	count(s.managed_policy_arns) > 0
	every a in s.managed_policy_arns {
		non_empty_string(a)
	}
}

declaration_shape if {
	is_object(access)
	object.keys(access) == {"permission_sets", "inline_policy", "protected_resources"}
	is_object(declared_sets)
	count(declared_sets) == 5
	every _, s in declared_sets {
		set_shape(s)
	}
	non_empty_string(access.inline_policy)
	is_array(access.protected_resources)
	count(access.protected_resources) > 0
	every r in access.protected_resources {
		non_empty_string(r)
	}
}

deny contains msg if {
	not declaration_shape
	msg := "[PLAN-IDC-00] 宣言 organization_baseline.access の形が違います（permission set 5つの address・排他の付与・名前の接尾辞・セッション・managed policy、inline policy の address、Deny で守る ARN の並び）"
}

declared_addresses contains a if {
	declaration_shape
	some _, s in declared_sets
	some a in [s.address, s.exclusive]
}

declared_addresses contains access.inline_policy if declaration_shape

deny contains msg if {
	some a in declared_addresses
	not after(a)
	msg := sprintf("[PLAN-IDC-00] 宣言した Identity Center の %s が入力にありません", [a])
}

management_account_id := after(object.get(ob, "organization", "")).master_account_id

management_account_known if lib.fixed_value(management_account_id)

deny contains msg if {
	declaration_shape
	not management_account_known
	msg := "[PLAN-IDC-00] 組織の管理アカウントの ID が入力で決まりません"
}

set_arns := {k: after(s.address).arn |
	declaration_shape
	some k, s in declared_sets
	lib.fixed_value(after(s.address).arn)
}

deny contains msg if {
	declaration_shape
	some k, s in declared_sets
	after(s.address)
	not set_arns[k]
	msg := sprintf("[PLAN-IDC-00] permission set %s の ARN が plan で決まりません", [s.address])
}

assignments := changes_of("aws_ssoadmin_account_assignment")

deny contains msg if {
	declaration_shape
	count(assignments) == 0
	msg := "[PLAN-IDC-00] Identity Center の割当が入力にありません"
}

deny contains msg if {
	some rc in assignments
	some attr in ["permission_set_arn", "principal_type", "target_id", "target_type"]
	not known(rc, attr)
	msg := sprintf("[PLAN-IDC-00] 割当 %s の %s が plan で決まりません", [rc.address, attr])
}

known(rc, attr) if known_in(rc.change, attr)

known_in(change, attr) if {
	not lib.unknown_value(object.get(object.get(change, "after_unknown", {}), attr, false))
	non_empty_string(object.get(change.after, attr, null))
}

deny contains msg if {
	declaration_shape
	some rc in assignments
	known(rc, "permission_set_arn")
	not rc.change.after.permission_set_arn in {a | some a in set_arns}
	msg := sprintf("[PLAN-IDC-00] 割当 %s の permission set が、宣言した permission set ではありません", [rc.address])
}

# 01: 名前とセッションと relay state。閉じた一覧の外の permission set を作らない。
deny contains msg if {
	declaration_shape
	some _, s in declared_sets
	ch := change_at(s.address)
	not name_shape(ch, s.name_suffix)
	msg := sprintf("[PLAN-IDC-01] permission set %s の名前が <project>-%s ではありません", [s.address, s.name_suffix])
}

name_shape(ch, suffix) if {
	known_in(ch, "name")
	name := ch.after.name
	lib.fixed_value(name)
	endswith(name, concat("", ["-", suffix]))
	regex.match(`^[a-z0-9]{1,13}$`, substring(name, 0, (count(name) - count(suffix)) - 1))
}

deny contains msg if {
	declaration_shape
	some _, s in declared_sets
	ps := after(s.address)
	object.get(ps, "session_duration", null) != s.session_duration
	msg := sprintf("[PLAN-IDC-01] permission set %s のセッションの長さが %s ではありません", [s.address, s.session_duration])
}

deny contains msg if {
	declaration_shape
	some _, s in declared_sets
	ps := after(s.address)
	object.get(ps, "relay_state", null) != null
	msg := sprintf("[PLAN-IDC-01] permission set %s が relay_state を書いています", [s.address])
}

deny contains msg if {
	declaration_shape
	some rc in changes_of("aws_ssoadmin_permission_set")
	not rc.address in {s.address | some _, s in declared_sets}
	msg := sprintf("[PLAN-IDC-01] %s は閉じた一覧の外の permission set です", [rc.address])
}

# 02: managed policy は、宣言の排他の付与だけが、宣言の一覧と同じ集合を付ける。
deny contains msg if {
	declaration_shape
	some _, s in declared_sets
	ex := after(s.exclusive)
	{a | some a in object.get(ex, "managed_policy_arns", [])} != {a | some a in s.managed_policy_arns}
	msg := sprintf("[PLAN-IDC-02] %s の managed policy が宣言の一覧と一致しません", [s.exclusive])
}

deny contains msg if {
	declaration_shape
	some k, s in declared_sets
	ex := after(s.exclusive)
	object.get(ex, "permission_set_arn", null) != set_arns[k]
	msg := sprintf("[PLAN-IDC-02] %s が、宣言した permission set %s を指していません", [s.exclusive, s.address])
}

deny contains msg if {
	declaration_shape
	some rc in changes_of("aws_ssoadmin_managed_policy_attachments_exclusive")
	not rc.address in {s.exclusive | some _, s in declared_sets}
	msg := sprintf("[PLAN-IDC-02] %s は宣言の外の managed policy の排他の付与です", [rc.address])
}

deny contains msg if {
	some c in lib.changes
	c.rc.type in forbidden_types
	msg := sprintf("[PLAN-IDC-02] %s は、排他の付与の外で permission set に権限を付けます", [c.rc.address])
}

# AdministratorAccess を付ける permission set の ARN。key ではなく、排他の付与の中身で決める。
admin_set_arns contains arn if {
	some rc in changes_of("aws_ssoadmin_managed_policy_attachments_exclusive")
	some p in object.get(rc.change.after, "managed_policy_arns", [])
	endswith(p, administrator_access_suffix)
	arn := rc.change.after.permission_set_arn
	is_string(arn)
}

inline_policies := changes_of("aws_ssoadmin_permission_set_inline_policy")

inline_statements(rc) := lib.statements(json.unmarshal(rc.change.after.inline_policy)) if {
	is_string(rc.change.after.inline_policy)
	json.is_valid(rc.change.after.inline_policy)
}

# 03: AdministratorAccess の permission set だけが inline を持ち、それは Deny だけで、AssumeRoot と bootstrap の構成物の書き込みを拒む。
deny contains msg if {
	some rc in inline_policies
	not inline_statements(rc)
	msg := sprintf("[PLAN-IDC-03] %s の inline policy が plan で決まらないか、JSON として読めません", [rc.address])
}

deny contains msg if {
	some arn in admin_set_arns
	not denies_assume_root(arn)
	msg := sprintf("[PLAN-IDC-03] AdministratorAccess を付ける permission set %s に、Resource \"*\" で条件なしの sts:AssumeRoot の Deny がありません", [arn])
}

denies_assume_root(arn) if {
	some rc in inline_policies
	rc.change.after.permission_set_arn == arn
	some s in inline_statements(rc)
	s.Effect == "Deny"
	lib.lower_all(object.get(s, "Action", [])) == ["sts:assumeroot"]
	lib.as_array(object.get(s, "Resource", [])) == ["*"]
	not "Condition" in object.keys(s)
}

deny contains msg if {
	some rc in inline_policies
	not known(rc, "permission_set_arn")
	msg := sprintf("[PLAN-IDC-03] %s の inline policy を付ける permission set が plan で決まりません", [rc.address])
}

deny contains msg if {
	some rc in inline_policies
	known(rc, "permission_set_arn")
	not rc.change.after.permission_set_arn in admin_set_arns
	msg := sprintf("[PLAN-IDC-03] %s が、AdministratorAccess を付けない permission set に inline policy を付けます", [rc.address])
}

deny contains msg if {
	some rc in inline_policies
	some s in inline_statements(rc)
	object.get(s, "Effect", "") != "Deny"
	msg := sprintf("[PLAN-IDC-03] %s の inline policy に Deny でない文 %v があります", [rc.address, object.get(s, "Sid", "")])
}

deny contains msg if {
	some rc in inline_policies
	some s in inline_statements(rc)
	s.Effect == "Deny"
	"Condition" in object.keys(s)
	msg := sprintf("[PLAN-IDC-03] %s の inline policy の Deny %v が条件を持ちます", [rc.address, object.get(s, "Sid", "")])
}

deny contains msg if {
	some rc in inline_policies
	some s in inline_statements(rc)
	some a in lib.lower_all(object.get(s, "NotAction", []))
	not read_verb(a)
	msg := sprintf("[PLAN-IDC-03] %s の inline policy の Deny %v の NotAction に、読みの動詞のほかの %s があります", [rc.address, object.get(s, "Sid", ""), a])
}

deny contains msg if {
	declaration_shape
	some r in access.protected_resources
	not protected(r)
	msg := sprintf("[PLAN-IDC-03] AdministratorAccess を付ける permission set の inline policy が、%s の書き込みを拒んでいません", [r])
}

protected(r) if {
	some rc in inline_policies
	rc.change.after.permission_set_arn in admin_set_arns
	some s in inline_statements(rc)
	write_deny(s)
	r in lib.as_array(object.get(s, "Resource", []))
}

# 書き込みを拒む Deny は、条件を持たず、Action を持たず、NotAction が読みの動詞だけの文に限る。Action の Deny は名指した操作しか拒まない。
write_deny(s) if {
	object.get(s, "Effect", "") == "Deny"
	not "Condition" in object.keys(s)
	not "Action" in object.keys(s)
	read_actions := lib.lower_all(object.get(s, "NotAction", []))
	count(read_actions) > 0
	every a in read_actions {
		read_verb(a)
	}
}

read_verb(a) if regex.match(`^[a-z0-9-]+:(get|list|describe)\*$`, a)

deny contains msg if {
	declaration_shape
	some rc in inline_policies
	some s in inline_statements(rc)
	object.get(s, "Effect", "") == "Deny"
	some r in access.protected_resources
	r in lib.as_array(object.get(s, "Resource", []))
	not write_deny(s)
	msg := sprintf("[PLAN-IDC-03] %s の inline policy の Deny %v が %s を、条件なしで NotAction の読みの動詞だけを残す形で拒んでいません", [rc.address, object.get(s, "Sid", ""), r])
}

# 03: 管理アカウントの CI の role・policy・OIDC provider（deployment-identity の reference の plan）は、それぞれ protected_resources の
# どれかに当たる。Deny は CI の role と policy を prefix の pattern で名指すので、pattern の外の名前を作ると Deny が外れる
# （ADR-0301 決定11 の但し書きの検査。organization-baseline/ADR-0204 決定3）。account の部分は reference ごとに違うので比べない。
ci_module := object.get(object.get(data.policy_plan, "deployment_identity", {}), "module", "")

ci_kinds := {"aws_iam_role": "role", "aws_iam_policy": "policy", "aws_iam_openid_connect_provider": "oidc-provider"}

ci_identities contains c.rc if {
	non_empty_string(ci_module)
	some c in lib.changes
	startswith(c.rc.address, ci_module)
	c.rc.type in object.keys(ci_kinds)
}

ci_resource(rc) := sprintf("%s%s%s", [ci_kinds[rc.type], ci_path(rc), rc.change.after.name]) if {
	rc.type != "aws_iam_openid_connect_provider"
	known(rc, "name")
}

# path を書かない role と policy は "/" に置かれる。
ci_path(rc) := p if {
	p := object.get(rc.change.after, "path", null)
	is_string(p)
} else := "/"

ci_resource(rc) := concat("/", ["oidc-provider", trim_prefix(rc.change.after.url, "https://")]) if {
	rc.type == "aws_iam_openid_connect_provider"
	known(rc, "url")
}

protected_iam_resources contains m[0][1] if {
	some r in access.protected_resources
	m := regex.find_all_string_submatch_n(`^arn:[^:]+:iam::[0-9]{12}:(.+)$`, r, 1)
	count(m) == 1
}

deny contains msg if {
	declaration_shape
	count(ci_identities) == 0
	msg := "[PLAN-IDC-00] 管理アカウントの CI の role・policy・OIDC provider（宣言 deployment_identity.module の下）が入力にありません"
}

deny contains msg if {
	declaration_shape
	some rc in ci_identities
	not ci_resource(rc)
	msg := sprintf("[PLAN-IDC-00] 管理アカウントの CI の %s の名前が plan で決まりません", [rc.address])
}

deny contains msg if {
	declaration_shape
	some rc in ci_identities
	r := ci_resource(rc)
	not covered_ci_resource(r)
	msg := sprintf("[PLAN-IDC-03] 管理アカウントの CI の %s（%s）が、organization_admin の Deny が守る ARN の pattern の外にあります", [rc.address, r])
}

covered_ci_resource(r) if {
	some p in protected_iam_resources
	lib.iam_match(p, r)
}

# 04: AdministratorAccess の permission set は管理アカウントへ USER でだけ割り当て、管理アカウントへの割当は USER だけ、メンバーアカウントへの割当は GROUP だけにする。
deny contains msg if {
	some rc in assignments
	rc.change.after.permission_set_arn in admin_set_arns
	not admin_assignment(rc)
	msg := sprintf("[PLAN-IDC-04] %s が、AdministratorAccess を付ける permission set を管理アカウントの USER の外へ割り当てます", [rc.address])
}

admin_assignment(rc) if {
	rc.change.after.target_id == management_account_id
	rc.change.after.principal_type == "USER"
}

deny contains msg if {
	some rc in assignments
	rc.change.after.target_id == management_account_id
	rc.change.after.principal_type != "USER"
	msg := sprintf("[PLAN-IDC-04] %s が、管理アカウントへ USER の外で割り当てます", [rc.address])
}

deny contains msg if {
	some rc in assignments
	management_account_known
	known(rc, "target_id")
	rc.change.after.target_id != management_account_id
	object.get(rc.change.after, "principal_type", null) != "GROUP"
	msg := sprintf("[PLAN-IDC-04] %s が、メンバーアカウントへ GROUP の外で割り当てます", [rc.address])
}

deny contains msg if {
	declaration_shape
	count([rc | some rc in assignments; rc.change.after.permission_set_arn in admin_set_arns; admin_assignment(rc)]) == 0
	msg := "[PLAN-IDC-04] AdministratorAccess を付ける permission set の、管理アカウントへの USER の割当がありません"
}

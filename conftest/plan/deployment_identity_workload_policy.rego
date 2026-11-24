# deployment-identity の apply の role の workload の policy と、apply の role が作る role の permissions boundary の形
# （deployment-identity/ADR-0201 決定5・13-17）。IAM の書き込みは小文字の usecase の接頭辞に限り、role の書き込みは
# 宣言した boundary を条件に持ち、boundary の Deny は apply の role の Deny と同じ文であることを見る。
package plan.deployment_identity_workload_policy

import data.plan.lib

di := object.get(data.policy_plan, "deployment_identity", {})

wl := object.get(di, "workload", null)

apply_policy := object.get(object.get(di, "apply_role", {}), "policy", "")

nonempty_string(x) if {
	is_string(x)
	x != ""
}

nonempty_strings(xs) if {
	is_array(xs)
	every x in xs {
		nonempty_string(x)
	}
}

# 接頭辞は小文字で "-" で終わり、CI の構成物（<name_prefix>-ci-）と重ならないよう ci- で始まらない。
well_formed_prefix(x) if {
	is_string(x)
	regex.match(`^[a-z0-9][a-z0-9-]*-$`, x)
	not startswith(x, "ci-")
}

well_formed if {
	is_object(wl)
	object.keys(wl) == {"policy", "read_policy", "boundaries", "usecase_iam_prefixes", "service_linked_roles", "passed_to_services", "apply_only_denies"}
	nonempty_string(wl.policy)
	nonempty_string(wl.read_policy)
	is_object(wl.boundaries)
	count(wl.boundaries) > 0
	every _, a in wl.boundaries {
		nonempty_string(a)
	}
	is_array(wl.usecase_iam_prefixes)
	count(wl.usecase_iam_prefixes) > 0
	every x in wl.usecase_iam_prefixes {
		well_formed_prefix(x)
	}
	nonempty_strings(wl.service_linked_roles)
	count(wl.service_linked_roles) > 0
	nonempty_strings(wl.passed_to_services)
	count(wl.passed_to_services) > 0
	nonempty_strings(wl.apply_only_denies)
	nonempty_string(apply_policy)
}

after(address) := c.rc.change.after if {
	some c in lib.changes
	c.rc.address == address
}

doc(address) := r if {
	some r in lib.readable
	r.document.address == address
}

name_prefix := trim_suffix(after(wl.policy).name, "-ci-apply-workload") if {
	well_formed
	endswith(after(wl.policy).name, "-ci-apply-workload")
}

account := parts[0][1] if {
	well_formed
	parts := regex.find_all_string_submatch_n(`^arn:aws[a-z-]*:iam::([0-9]{12}):policy/`, object.get(after(wl.policy), "arn", ""), 1)
	count(parts) == 1
}

usecase_prefixes := {sprintf("%s-%s", [name_prefix, x]) | some x in wl.usecase_iam_prefixes}

boundary_arns := {after(a).arn | some _, a in wl.boundaries}

boundary_instances := {c.rc.address |
	some c in lib.changes
	c.rc.type == "aws_iam_policy"
	some _, a in wl.boundaries
	lib.strip_index(c.rc.address) == lib.strip_index(a)
}

# --- PLAN-AWP-00 ---

deny contains msg if {
	not well_formed
	msg := "[PLAN-AWP-00] data.policy_plan.deployment_identity.workload が無いか、policy・read_policy・boundaries（usecase を key にした address の空でない object）・usecase_iam_prefixes（小文字で - で終わり、ci- で始まらない接頭辞の空でない配列）・service_linked_roles・passed_to_services（空でない）・apply_only_denies（空でない文字列の配列）だけを持つ形を満たしていないか、deployment_identity.apply_role.policy がありません"
}

deny contains msg if {
	well_formed
	some address in array.concat([wl.policy, wl.read_policy, apply_policy], [a | some _, a in wl.boundaries])
	not doc(address)
	msg := sprintf("[PLAN-AWP-00] %s が入力にありません。address が変わったなら data.policy_plan.deployment_identity を直してください", [address])
}

deny contains msg if {
	well_formed
	doc(wl.policy)
	not name_prefix
	msg := sprintf("[PLAN-AWP-00] %s の名前 %v が <name_prefix>-ci-apply-workload の形ではありません", [wl.policy, object.get(after(wl.policy), "name", null)])
}

deny contains msg if {
	well_formed
	doc(wl.read_policy)
	name_prefix
	object.get(after(wl.read_policy), "name", null) != sprintf("%s-ci-apply-workload-read", [name_prefix])
	msg := sprintf("[PLAN-AWP-00] %s の名前 %v が %s-ci-apply-workload-read ではありません", [wl.read_policy, object.get(after(wl.read_policy), "name", null), name_prefix])
}

deny contains msg if {
	well_formed
	doc(wl.read_policy)
	not is_string(object.get(after(wl.read_policy), "arn", null))
	msg := sprintf("[PLAN-AWP-00] %s の ARN が plan で決まっていません。tests/policy-input.tftest.hcl の override_resource で与えてください", [wl.read_policy])
}

deny contains msg if {
	well_formed
	doc(wl.policy)
	not account
	msg := sprintf("[PLAN-AWP-00] %s の ARN が plan で決まっていないか、IAM の policy の ARN ではありません。tests/policy-input.tftest.hcl の override_resource で与えてください", [wl.policy])
}

deny contains msg if {
	well_formed
	some _, a in wl.boundaries
	doc(a)
	not is_string(object.get(after(a), "arn", null))
	msg := sprintf("[PLAN-AWP-00] %s の ARN が plan で決まっていません。tests/policy-input.tftest.hcl の override_resource で与えてください", [a])
}

deny contains msg if {
	well_formed
	count(boundary_instances) != count(wl.boundaries)
	msg := sprintf("[PLAN-AWP-00] plan の boundary の instance %v の数が、宣言した boundaries の数 %d と一致しません", [sort(boundary_instances), count(wl.boundaries)])
}

# --- 文 ---

workload_allows contains {"path": r.document.path, "address": address, "s": s} if {
	well_formed
	some address in [wl.policy, wl.read_policy]
	r := doc(address)
	some s in lib.statements(r.body)
	s.Effect == "Allow"
}

actions(s) := lib.lower_all(object.get(s, "Action", []))

iam_write(a) if {
	startswith(a, "iam:")
	not regex.match(`^iam:(get|list|generate|simulate)[a-z0-9]*$`, a)
}

iam_write(a) if {
	service := split(a, ":")[0]
	regex.match(`[*?]`, service)
	glob.match(service, null, "iam")
}

matches(a, name) if glob.match(a, null, name)

# iam:PermissionsBoundary を受ける role の書き込み（Service Authorization Reference。deployment-identity/ADR-0201 決定14）。
role_writes := {
	"iam:attachrolepolicy", "iam:createrole", "iam:deleterole", "iam:deleterolepolicy", "iam:detachrolepolicy",
	"iam:putrolepermissionsboundary", "iam:putrolepolicy", "iam:updateassumerolepolicy", "iam:updaterole",
	"iam:updateroledescription",
}

# 接頭辞は ci- で始まらないので（well_formed_prefix）、CI の構成物（<name_prefix>-ci-）と重ならない。
usecase_iam_arn(r) if {
	r == lower(r)
	parts := regex.find_all_string_submatch_n(`^arn:aws[a-z-]*:iam::([0-9]{12}):(role|policy)/(.+)$`, r, 1)
	count(parts) == 1
	parts[0][1] == account
	name := parts[0][3]
	some p in usecase_prefixes
	startswith(name, p)
	rest := substring(name, count(p), -1)
	rest != ""
	not contains(rest, "/")
}

# service-linked role の名前は AWS が決めるので、宣言したサービスの SLR を、そのサービスの名前の条件つきで作ることだけを許す。
service_linked(s, a, r) if {
	a == "iam:createservicelinkedrole"
	parts := regex.find_all_string_submatch_n(`^arn:aws[a-z-]*:iam::([0-9]{12}):role/aws-service-role/([^/*?]+)/[^/*?]+$`, r, 1)
	count(parts) == 1
	parts[0][1] == account
	service := parts[0][2]
	service in wl.service_linked_roles
	lib.as_array(object.get(object.get(object.get(s, "Condition", {}), "StringEquals", {}), "iam:AWSServiceName", [])) == [service]
}

condition_values(s, op, key) := lib.as_array(object.get(object.get(object.get(s, "Condition", {}), op, {}), key, []))

# --- PLAN-AWP-01: IAM の書き込みの Resource ---

deny contains msg if {
	some x in workload_allows
	some a in actions(x.s)
	iam_write(a)
	some r in lib.as_array(object.get(x.s, "Resource", []))
	not usecase_iam_arn(r)
	not service_linked(x.s, a, r)
	msg := sprintf("[PLAN-AWP-01] %s: %s の Allow が %s を %s に許しています。IAM の書き込みは、同じアカウントの、小文字の宣言した usecase の接頭辞（%v）の role と policy に限ってください（deployment-identity/ADR-0201 決定13）", [x.path, x.address, a, r, sort(usecase_prefixes)])
}

# --- PLAN-AWP-02: role の書き込みの boundary の条件 ---

deny contains msg if {
	some x in workload_allows
	some a in actions(x.s)
	some w in role_writes
	matches(a, w)
	not boundary_bound(x.s)
	msg := sprintf("[PLAN-AWP-02] %s: %s の Allow が %s を、条件 StringEquals の iam:PermissionsBoundary を宣言した boundary %v に限らずに許しています（deployment-identity/ADR-0201 決定14）", [x.path, x.address, a, sort(boundary_arns)])
}

boundary_bound(s) if {
	values := condition_values(s, "StringEquals", "iam:PermissionsBoundary")
	count(values) > 0
	every v in values {
		v in boundary_arns
	}
}

# --- PLAN-AWP-03: PassRole の渡し先 ---

deny contains msg if {
	some x in workload_allows
	some a in actions(x.s)
	matches(a, "iam:passrole")
	not passed_to_declared(x.s)
	msg := sprintf("[PLAN-AWP-03] %s: %s の Allow が iam:PassRole を、条件 StringEquals の iam:PassedToService を宣言したサービス %v に限らずに許しています（deployment-identity/ADR-0201 決定14）", [x.path, x.address, wl.passed_to_services])
}

passed_to_declared(s) if {
	values := condition_values(s, "StringEquals", "iam:PassedToService")
	count(values) > 0
	every v in values {
		v in wl.passed_to_services
	}
}

# --- PLAN-AWP-04: 作る role に付ける policy ---

deny contains msg if {
	some x in workload_allows
	some a in actions(x.s)
	matches(a, "iam:attachrolepolicy")
	not policy_arn_bound(x.s)
	msg := sprintf("[PLAN-AWP-04] %s: %s の Allow が iam:AttachRolePolicy を、条件 ArnLike か ArnEquals の iam:PolicyARN を宣言した usecase の接頭辞の policy に限らずに許しています（deployment-identity/ADR-0201 決定14）", [x.path, x.address])
}

policy_arn_bound(s) if {
	values := array.concat(condition_values(s, "ArnLike", "iam:PolicyARN"), condition_values(s, "ArnEquals", "iam:PolicyARN"))
	count(values) > 0
	every v in values {
		usecase_iam_arn(v)
		contains(v, ":policy/")
	}
}

# --- PLAN-AWP-05: boundary の Deny と apply の role の Deny ---

denies(address) := {s | some s in lib.statements(doc(address).body); s.Effect == "Deny"}

apply_bootstrap_denies := {s | some s in denies(apply_policy); not object.get(s, "Sid", "") in wl.apply_only_denies}

deny contains msg if {
	well_formed
	doc(apply_policy)
	count(apply_bootstrap_denies) == 0
	msg := sprintf("[PLAN-AWP-05] apply の role の %s に、boundary と共有する Deny（%v を除く Deny）がありません（deployment-identity/ADR-0201 決定5・15）", [apply_policy, wl.apply_only_denies])
}

deny contains msg if {
	well_formed
	doc(apply_policy)
	some _, b in wl.boundaries
	doc(b)
	denies(b) != apply_bootstrap_denies
	msg := sprintf("[PLAN-AWP-05] %s: %s の Deny が、apply の role %s の Deny（%v を除く）と同じ文ではありません（deployment-identity/ADR-0201 決定5・15）", [doc(b).document.path, b, apply_policy, wl.apply_only_denies])
}

deny contains msg if {
	well_formed
	doc(apply_policy)
	some sid in wl.apply_only_denies
	count({s | some s in denies(apply_policy); object.get(s, "Sid", "") == sid}) == 0
	msg := sprintf("[PLAN-AWP-05] apply の role の %s に、宣言した apply の role だけの Deny %s がありません（deployment-identity/ADR-0201 決定5）", [apply_policy, sid])
}

boundary_removal_denied if {
	some s in denies(apply_policy)
	count(object.get(s, "Condition", {})) == 0
	not "NotResource" in object.keys(s)
	not "NotAction" in object.keys(s)
	lib.lower_all(s.Action) == ["iam:deleterolepermissionsboundary"]
	lib.as_array(object.get(s, "Resource", [])) == ["*"]
}

deny contains msg if {
	well_formed
	doc(apply_policy)
	not boundary_removal_denied
	msg := sprintf("[PLAN-AWP-05] apply の role の %s が、iam:DeleteRolePermissionsBoundary を Resource \"*\" の条件なしの Deny で拒んでいません（deployment-identity/ADR-0201 決定14）", [apply_policy])
}

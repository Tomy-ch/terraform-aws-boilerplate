# メンバーアカウントの立ち上げの読み取りの role（organization-baseline/ADR-0203。conftest/plan/README.md の
# plan.organization_baseline_bootstrap_read）。
package plan.organization_baseline_bootstrap_read

import data.plan.deployment_identity_apply_role as di_apply_role
import data.plan.deployment_identity_plan_role as di_plan_role
import data.plan.lib

ob := object.get(data.policy_plan, "organization_baseline", {})

br := object.get(ob, "bootstrap_read", {})

after(address) := c.rc.change.after if {
	some c in lib.changes
	c.rc.address == address
}

stack_set := after(object.get(br, "stack_set", ""))

instances := after(object.get(br, "stack_instances", ""))

template := json.unmarshal(stack_set.template_body) if {
	is_string(stack_set.template_body)
	json.is_valid(stack_set.template_body)
}

role_properties := r.Properties if {
	count(template.Resources) == 1
	some r in template.Resources
	r.Type == "AWS::IAM::Role"
	count(r.Properties.Policies) == 1
	count(lib.statements(r.Properties.Policies[0].PolicyDocument)) > 0
}

role_pattern := sprintf("arn:aws:iam::*:role%s%s", [role_properties.Path, role_properties.RoleName])

read_statements := [s | some p in role_properties.Policies; some s in lib.statements(p.PolicyDocument)]

read_actions := {lower(a) | some s in read_statements; some a in lib.as_array(s.Action)}

# 管理アカウントの plan の role の inline policy の Allow（deployment-identity の reference の plan）。
plan_policy := object.get(object.get(object.get(data.policy_plan, "deployment_identity", {}), "plan_role", {}), "policy", "")

plan_actions := {lower(a) |
	some r in lib.readable
	r.document.type == "aws_iam_role_policy"
	r.document.address == plan_policy
	some s in lib.statements(r.body)
	s.Effect == "Allow"
	some a in lib.as_array(s.Action)
}

# 管理アカウントの構成物にだけ掛かる読みと、state の object の読み書き。メンバーの立ち上げの plan は読まない（organization-baseline/ADR-0203）。
management_only := {lower(e.action) | some e in object.get(br, "management_only_actions", []); is_object(e)}

well_formed_declaration if {
	is_string(br.stack_set)
	is_string(br.stack_instances)
	is_array(br.management_only_actions)
	count(br.management_only_actions) > 0
	every e in br.management_only_actions {
		is_object(e)
		object.keys(e) == {"action", "reason"}
		is_string(e.action)
		regex.match(`^[a-z0-9-]+:[A-Za-z0-9]+$`, e.action)
		is_string(e.reason)
		trim_space(e.reason) != ""
	}
}

deny contains msg if {
	not well_formed_declaration
	msg := "[PLAN-BR-00] 宣言 organization_baseline.bootstrap_read が、stack_set・stack_instances の address と、wildcard を含まない Action の名前（action）と空でない理由（reason）の組の空でない配列 management_only_actions を持ちません"
}

deny contains msg if {
	well_formed_declaration
	some a in [br.stack_set, br.stack_instances]
	not after(a)
	msg := sprintf("[PLAN-BR-00] 宣言した立ち上げの読み取りの %s が入力にありません", [a])
}

deny contains msg if {
	well_formed_declaration
	stack_set
	not role_properties
	msg := "[PLAN-BR-00] 立ち上げの読み取りの StackSet のテンプレートが、文を持つ inline policy を1つだけ持つ AWS::IAM::Role を1つだけ持つ JSON ではありません"
}

# 上位集合の突き合わせの相手が無ければ、何も見ずに通さない。
deny contains msg if {
	well_formed_declaration
	count(plan_actions) == 0
	msg := sprintf("[PLAN-BR-00] 突き合わせる plan の role の inline policy %v が入力にありません", [plan_policy])
}

deny contains msg if {
	stack_set
	not service_managed_auto
	msg := "[PLAN-BR-01] 立ち上げの読み取りの StackSet が、service-managed で新しいアカウントへ自動で置く形ではありません"
}

service_managed_auto if {
	stack_set.permission_model == "SERVICE_MANAGED"
	stack_set.auto_deployment[0].enabled == true
}

# CloudFormation は ASCII の外の文字を「?」に置き換えて保存する（PLAN-BG-05 と同じ）。
deny contains msg if {
	stack_set
	description := object.get(stack_set, "description", null)
	description != null
	not regex.match(`^[\x20-\x7E]*$`, description)
	msg := "[PLAN-BR-01] 立ち上げの読み取りの StackSet の description が、印字できる ASCII の外の文字を含みます"
}

organization_root_id := after(object.get(ob, "organization", "")).roots[0].id

deny contains msg if {
	stack_set
	not instances_shape
	msg := "[PLAN-BR-01] 立ち上げの読み取りの stack instance が、role と同じ名前の StackSet を名指して組織の Root だけへ置く形ではありません"
}

instances_shape if {
	stack_set.name == role_properties.RoleName
	instances.stack_set_name == stack_set.name
	count(instances.deployment_targets) == 1
	target := instances.deployment_targets[0]
	target.organizational_unit_ids == [organization_root_id]
	object.get(target, "accounts", null) == null
}

role_keys := {"RoleName", "Path", "MaxSessionDuration", "AssumeRolePolicyDocument", "Policies"}

deny contains msg if {
	role_properties
	not trust_shape
	msg := "[PLAN-BR-02] 立ち上げの読み取りの role の信頼条件が、管理アカウントの root を Principal にし、aws:PrincipalArn を管理アカウントの plan の role の ARN だけに限る形ではありません"
}

management_account := after(object.get(ob, "organization", "")).master_account_id

management_root := sprintf("arn:aws:iam::%s:root", [management_account])

plan_role := after(object.get(object.get(object.get(data.policy_plan, "deployment_identity", {}), "plan_role", {}), "role", ""))

# 信頼する主体は、管理アカウントの、宣言した plan の role と同じ path と名前の role（organization-baseline/ADR-0203 決定3）。
trusted_principal := sprintf("arn:aws:iam::%s:role%s%s", [management_account, plan_role_path, plan_role.name])

# path を書かない role は、AWS が既定の "/" に置く。plan では null で表れる。
default plan_role_path := "/"

plan_role_path := plan_role.path if is_string(plan_role.path)

trust_shape if {
	object.keys(role_properties) == role_keys
	statements := lib.statements(role_properties.AssumeRolePolicyDocument)
	count(statements) == 1
	some s in statements
	object.keys(s) == {"Effect", "Principal", "Action", "Condition"}
	s.Effect == "Allow"
	s.Action == "sts:AssumeRole"
	s.Principal == {"AWS": management_root}
	object.keys(s.Condition) == {"ArnEquals"}
	object.keys(s.Condition.ArnEquals) == {"aws:PrincipalArn"}
	s.Condition.ArnEquals["aws:PrincipalArn"] == trusted_principal
}

read_verbs := ["get", "list", "describe"]

deny contains msg if {
	some s in read_statements
	not read_only_statement(s)
	msg := sprintf("[PLAN-BR-03] 立ち上げの読み取りの role の文 %v が、S3・KMS・IAM の wildcard の無い読み取りの Action を、置き先のアカウントの Resource に許す Allow だけの形ではありません", [object.get(s, "Sid", "")])
}

read_only_statement(s) if {
	object.keys(s) == {"Sid", "Effect", "Action", "Resource"}
	s.Effect == "Allow"
	every a in lib.as_array(s.Action) {
		is_string(a)
		not contains(a, "*")
		not contains(a, "?")
		parts := split(lower(a), ":")
		count(parts) == 2
		parts[0] in read_services
		some v in read_verbs
		startswith(parts[1], v)
	}
	scoped_resources(s)
}

# 立ち上げの plan が読むのは、state-backend と deployment-identity の構成物（S3・KMS・IAM）だけである。
read_services := {"s3", "kms", "iam"}

# Resource は置き先のアカウントに限る。resource-level の権限を持たない kms:ListAliases だけが "*" を使う。
scoped_resources(s) if {
	lib.lower_all(s.Action) == ["kms:listaliases"]
	s.Resource == "*"
}

# アカウントの位置に置き先のアカウントを置く。S3 の Bucket の ARN はアカウントを持たないので、名前の中のアカウント ID で限る。
account_scoped := `^arn:\$\{AWS::Partition\}:(s3:::[^:/]+-\$\{AWS::AccountId\}-|kms:\$\{AWS::Region\}:\$\{AWS::AccountId\}:|iam::\$\{AWS::AccountId\}:)`

scoped_resources(s) if {
	every r in lib.as_array(s.Resource) {
		is_object(r)
		object.keys(r) == {"Fn::Sub"}
		is_string(r["Fn::Sub"])
		regex.match(account_scoped, r["Fn::Sub"])
	}
}

# state の object と鍵の暗号操作は読ませない。
deny contains msg if {
	some a in read_actions
	forbidden_read(a)
	msg := sprintf("[PLAN-BR-03] 立ち上げの読み取りの role が %s を許します。state の object と鍵の暗号操作は与えません", [a])
}

forbidden_read(a) if startswith(a, "s3:getobject")

forbidden_read(a) if a in lib.crypto_actions

forbidden_read("secretsmanager:getsecretvalue")

# 管理アカウントの plan の role の読みのうち、管理アカウントにだけ掛かるもののほかは、読み取りの role がすべて持つ。
deny contains msg if {
	role_properties
	count(plan_actions) > 0
	some a in plan_actions
	not a in read_actions
	not a in management_only
	msg := sprintf("[PLAN-BR-04] plan の role の %s を、立ち上げの読み取りの role が持たず、宣言 management_only_actions にもありません", [a])
}

deny contains msg if {
	well_formed_declaration
	count(plan_actions) > 0
	some a in management_only
	not a in plan_actions
	msg := sprintf("[PLAN-BR-05] 宣言 management_only_actions の %s を、plan の role が使っていません", [a])
}

deny contains msg if {
	some a in management_only
	a in read_actions
	msg := sprintf("[PLAN-BR-05] 宣言 management_only_actions の %s を、立ち上げの読み取りの role が持っています", [a])
}

# 立ち上げの外では SCP が閉じる。policy の address は organization_baseline.policies が持つ。
scp_statements(key) := lib.statements(json.unmarshal(after(object.get(object.get(ob, "policies", {}), key, "")).content))

deny contains msg if {
	role_properties
	not bootstrap_role_denied
	msg := sprintf("[PLAN-BR-06] deny-bootstrap-role が、立ち上げの読み取りの role %s の使用を拒んでいません", [role_pattern])
}

bootstrap_role_denied if {
	some s in scp_statements("deny-bootstrap-role")
	s.Effect == "Deny"
	s.Action == "*"
	s.Resource == "*"
	object.keys(s.Condition) == {"ArnLike"}
	role_pattern in lib.as_array(s.Condition.ArnLike["aws:PrincipalArn"])
}

deny contains msg if {
	role_properties
	not bootstrap_role_protected
	msg := sprintf("[PLAN-BR-06] protect-governance-roles が、立ち上げの読み取りの role %s の変更を、StackSet の実行の role のほかに拒んでいません", [role_pattern])
}

bootstrap_role_protected if {
	some s in scp_statements("protect-governance-roles")
	s.Effect == "Deny"
	s.Resource == role_pattern
	s.Condition == {"ArnNotLike": {"aws:PrincipalArn": "arn:aws:iam::*:role/stacksets-exec-*"}}
	every a in {"iam:create*", "iam:update*", "iam:put*", "iam:attach*", "iam:delete*", "iam:passrole"} {
		a in lib.lower_all(s.Action)
	}
}

# メンバーの構成物の ARN ごとに、そのメンバーの plan の role が Resource を限って許す読みを、この role がその ARN に許す
# （organization-baseline/ADR-0203 決定4）。構成物と plan の role の Allow は、reference のメンバーの形の instance を正規の
# 接頭辞へ付け替えた入力（lib.member_view）の上で、deployment-identity の規則から取る。要る読みを型ごとに別に持たない。
members contains i if {
	some i in lib.instances
	i.root != null
}

member_classes := {"target", "key", "service_linked"}

member_resources(i) := {r |
	some r in di_apply_role.resources with input as lib.member_view(i) with data.policy_plan as lib.member_decl with data.plan.lib.member_form as true
	di_apply_role.class_of(r.type) in member_classes
}

member_allows(i) := x if {
	x := di_plan_role.allows with input as lib.member_view(i) with data.policy_plan as lib.member_decl with data.plan.lib.member_form as true
}

concrete_arn(x) if {
	is_string(x)
	regex.match(`^arn:[a-z-]+:[a-z0-9-]+:`, x)
	not contains(x, "*")
	not contains(x, "?")
	not contains(x, "${")
}

member_arns(i) := {r.change.after.arn | some r in member_resources(i); concrete_arn(object.get(r.change.after, "arn", null))}

# S3 の ARN はアカウントの欄を持たないので、IAM と KMS の ARN から取る。
member_accounts(i) := {a |
	some x in member_arns(i)
	split(x, ":")[2] in {"iam", "kms"}
	a := split(x, ":")[4]
	a != ""
}

# ${AWS::Region} は stack instance の region で置き換える（決定1。region は provider の region だけ）。
stack_regions := {x | some x in object.get(instances, "regions", [])}

substituted(p, x, i) := strings.replace_n({"${AWS::Partition}": split(x, ":")[1], "${AWS::Region}": region, "${AWS::AccountId}": account}, p) if {
	is_string(p)
	count(stack_regions) == 1
	some region in stack_regions
	count(member_accounts(i)) == 1
	some account in member_accounts(i)
}

# 3つの疑似パラメータのほかの ${...} が残る pattern は、どの ARN にも当たらないと数える。
read_allows(a, x, i) if {
	some s in read_statements
	a in lib.lower_all(s.Action)
	some r in lib.as_array(s.Resource)
	read_resource_matches(r, x, i)
}

read_resource_matches("*", _, _)

read_resource_matches(r, x, i) if {
	is_object(r)
	p := substituted(r["Fn::Sub"], x, i)
	not contains(p, "${")
	lib.iam_match(p, x)
}

# Resource "*" の Allow は特定の ARN への認可ではない（PLAN-IAM-03 が resource-level の権限を持たない Action に限る）。
# Condition は無視して数え、要求を広い側に倒す。NotResource の Allow は、どの pattern も当たらない ARN に当たる。
plan_allows_arn(s, x) if {
	not "*" in lib.as_array(object.get(s, "Resource", []))
	some r in lib.as_array(object.get(s, "Resource", []))
	lib.iam_match(lib.policy_variable_as_wildcard(r), x)
}

plan_allows_arn(s, x) if {
	"NotResource" in object.keys(s)
	not not_resource_matches(s, x)
}

not_resource_matches(s, x) if {
	some r in lib.as_array(s.NotResource)
	lib.iam_match(lib.policy_variable_as_wildcard(r), x)
}

required(x, i) := {lower(a) |
	some al in member_allows(i)
	plan_allows_arn(al.statement, x)
	some a in lib.as_array(object.get(al.statement, "Action", []))
} - management_only

deny contains msg if {
	role_properties
	count(members) == 0
	msg := "[PLAN-BR-00] 入力に、構成物の ARN を照らすメンバーの形の instance（state_backend.instances）がありません。reference に置いてください"
}

# 管理アカウントの形の instance から取ると、メンバーに無い構成物への読みを要求する。
deny contains msg if {
	role_properties
	some i in members
	some a in lib.management_only_policies
	some f in input
	some rc in f.contents.resource_changes
	rc.address == lib.rebase(a, i)
	msg := sprintf("[PLAN-BR-00] instance %s が管理アカウントの形です（%s が入力に在ります）。構成物の ARN をメンバーの形の instance から取れません", [i.name, rc.address])
}

deny contains msg if {
	role_properties
	some i in members
	some side, prefix in {"state_backend": lib.sb_module, "deployment_identity": lib.di_module}
	count({r | some r in member_resources(i); startswith(r.address, prefix)}) == 0
	msg := sprintf("[PLAN-BR-00] instance %s の %s の側に、照らす構成物（%v）が入力にありません", [i.name, side, sort(member_classes)])
}

deny contains msg if {
	role_properties
	some i in members
	some r in member_resources(i)
	not concrete_arn(object.get(r.change.after, "arn", null))
	msg := sprintf("[PLAN-BR-00] instance %s の構成物 %s の ARN が、plan で決まった wildcard の無い ARN ではありません", [i.name, lib.rebase(r.address, i)])
}

deny contains msg if {
	role_properties
	some i in members
	count(member_accounts(i)) != 1
	msg := sprintf("[PLAN-BR-00] instance %s の構成物の IAM と KMS の ARN のアカウント %v が、ちょうど1つではありません", [i.name, sort(member_accounts(i))])
}

deny contains msg if {
	role_properties
	count(stack_regions) != 1
	msg := sprintf("[PLAN-BR-00] 立ち上げの読み取りの stack instance の region %v が、plan で決まったちょうど1つではありません", [sort(stack_regions)])
}

deny contains msg if {
	role_properties
	some i in members
	some x in member_arns(i)
	count(required(x, i)) == 0
	msg := sprintf("[PLAN-BR-00] instance %s の plan の role が、構成物 %s に Resource を限った読みを1つも許していません。fixture の値がずれたか、読みが Resource \"*\" だけの型なら deployment_identity.apply_role.bootstrap.types の分類を見直してください", [i.name, x])
}

deny contains msg if {
	role_properties
	some i in members
	some x in member_arns(i)
	some a in required(x, i)
	not read_allows(a, x, i)
	msg := sprintf("[PLAN-BR-07] instance %s の plan の role が構成物 %s に許す %s を、立ち上げの読み取りの role がその ARN に許しません", [i.name, x, a])
}

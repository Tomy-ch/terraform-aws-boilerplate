# 立ち上げの中のメンバーアカウントへの引き受け（repository-wide/ADR-0604 決定6、deployment-identity/ADR-0201 決定22。
# conftest/plan/README.md の plan.deployment_identity_bootstrap_assume）。
package plan.deployment_identity_bootstrap_assume

import data.plan.lib

di := object.get(data.policy_plan, "deployment_identity", {})

plan_policy := object.get(object.get(di, "plan_role", {}), "policy", "")

apply_identity_policy := object.get(object.get(di, "bootstrap_assume", {}), "apply_identity_policy", "")

# plan の role が引き受けてよいのは、メンバーの立ち上げの読み取りの role だけである（organization-baseline/ADR-0203 決定1）。
bootstrap_read_role_arn := `^arn:aws:iam::([0-9]{12}):role/bootstrap/[a-z0-9]+-bootstrap-read$`

access_role_arn := `^arn:aws:iam::([0-9]{12}):role/OrganizationAccountAccessRole$`

well_formed_declaration if {
	is_string(plan_policy)
	plan_policy != ""
	is_string(apply_identity_policy)
	apply_identity_policy != ""
}

deny contains msg if {
	not well_formed_declaration
	msg := "[PLAN-ASSUME-00] 宣言 deployment_identity.plan_role.policy と deployment_identity.bootstrap_assume.apply_identity_policy が、空でない address ではありません"
}

deny contains msg if {
	well_formed_declaration
	some a in required_policies
	count({r | some r in lib.readable; r.document.address == a}) == 0
	msg := sprintf("[PLAN-ASSUME-00] 宣言した引き受けの policy %s が入力にありません", [a])
}

# メンバーの形では apply-identity の側の policy（管理アカウントにだけ在る）を求めない。
required_policies := [plan_policy] if lib.member_form

else := [plan_policy, apply_identity_policy]

# メンバーの形では引き受けをどこにも置かない（deployment-identity/ADR-0201 決定22 は引き受けを管理アカウントにだけ与える）。
allowed_assumers := set() if lib.member_form

else := {plan_policy, apply_identity_policy}

# deployment-identity の CI の role の権限の文書。信頼条件（aws_iam_role の assume_role_policy）は含めない。
ci_documents contains r if {
	some r in lib.readable
	r.document.type in {"aws_iam_role_policy", "aws_iam_policy"}
	r.document.attr == "policy"
	startswith(r.document.address, "module.deployment_identity.")
}

assume_statements contains {"document": r.document, "statement": s} if {
	some r in ci_documents
	some s in lib.statements(r.body)
	s.Effect == "Allow"
	some action in assume_actions
	assumes(s, action)
}

# 引き受けと、引き受けに session tag と source identity を付ける Action（deployment-identity/ADR-0201 決定22）。
assume_actions := ["sts:assumerole", "sts:tagsession", "sts:setsourceidentity"]

assumes(s, action) if {
	some a in lib.lower_all(object.get(s, "Action", []))
	lib.iam_match(a, action)
}

assumes(s, action) if {
	"NotAction" in object.keys(s)
	not excluded_by_not_action(s, action)
}

excluded_by_not_action(s, action) if {
	some a in lib.lower_all(s.NotAction)
	lib.iam_match(a, action)
}

only_assume_role(s) if lib.lower_all(object.get(s, "Action", [])) == ["sts:assumerole"]

resources_match(s, pattern) if {
	resources := lib.as_array(object.get(s, "Resource", []))
	count(resources) > 0
	every r in resources {
		is_string(r)
		regex.match(pattern, r)
	}
}

deny contains msg if {
	some x in assume_statements
	x.document.address == plan_policy
	not plan_assume_shape(x.statement)
	msg := sprintf("[PLAN-ASSUME-01] %s: plan の role の引き受けが、Action を sts:AssumeRole だけにし、Resource をメンバーの立ち上げの読み取りの role（/bootstrap/<project>-bootstrap-read）の ARN だけにする形ではありません", [x.document.path])
}

plan_assume_shape(s) if {
	only_assume_role(s)
	resources_match(s, bootstrap_read_role_arn)
}

deny contains msg if {
	some x in assume_statements
	x.document.address == apply_identity_policy
	not apply_identity_assume_shape(x.statement)
	msg := sprintf("[PLAN-ASSUME-02] %s: apply-identity の role の引き受けが、Action を sts:AssumeRole だけにし、Resource をメンバーの OrganizationAccountAccessRole の ARN だけにする形ではありません", [x.document.path])
}

apply_identity_assume_shape(s) if {
	only_assume_role(s)
	resources_match(s, access_role_arn)
}

deny contains msg if {
	not lib.member_form
	some x in assume_statements
	not x.document.address in allowed_assumers
	msg := sprintf("[PLAN-ASSUME-03] %s: %s が sts:AssumeRole・sts:TagSession・sts:SetSourceIdentity のどれかを許します。メンバーの引き受けは、plan の role の inline policy と %s だけに置いてください", [x.document.path, x.document.address, apply_identity_policy])
}

deny contains msg if {
	lib.member_form
	some x in assume_statements
	msg := sprintf("[PLAN-ASSUME-03] %s: %s が sts:AssumeRole・sts:TagSession・sts:SetSourceIdentity のどれかを許します。メンバーアカウントの deployment-identity は、ほかのアカウントの引き受けを持ちません", [x.document.path, x.document.address])
}

accounts(address, pattern) := {regex.find_all_string_submatch_n(pattern, r, 1)[0][1] |
	some x in assume_statements
	x.document.address == address
	some r in lib.as_array(object.get(x.statement, "Resource", []))
	is_string(r)
	regex.match(pattern, r)
}

plan_accounts := accounts(plan_policy, bootstrap_read_role_arn)

apply_identity_accounts := accounts(apply_identity_policy, access_role_arn)

# 2つの role の引き受け先は、同じ入力 bootstrap_member_account_ids から組む。
deny contains msg if {
	plan_accounts != apply_identity_accounts
	msg := sprintf("[PLAN-ASSUME-04] plan の role の引き受け先のアカウント %v と、apply-identity の role の引き受け先のアカウント %v が一致しません", [sort(plan_accounts), sort(apply_identity_accounts)])
}

# 入力は reference の plan で、立ち上げの中のアカウントを持つ。引き受けが無ければ、ASSUME-01 から ASSUME-04 は何も見ていない。
deny contains msg if {
	not lib.member_form
	well_formed_declaration
	count(plan_accounts) == 0
	msg := "[PLAN-ASSUME-05] 入力に、plan の role の立ち上げの読み取りの role の引き受けがありません。reference の bootstrap_member_account_ids にアカウントを入れてください"
}

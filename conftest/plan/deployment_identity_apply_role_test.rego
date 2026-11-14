package plan.deployment_identity_apply_role_test

import data.plan.deployment_identity_apply_role as p
import data.plan.fixture

decl := {"apply_role": {"policy": "module.x.aws_iam_role_policy.this[\"apply\"]", "wildcard_denies": ["logs:PutAccountPolicy", "logs:PutResourcePolicy"]}}

wildcard_deny := {"Effect": "Deny", "Action": ["logs:PutAccountPolicy", "LOGS:PutResourcePolicy"], "Resource": "*"}

key := "arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"

key_policy(actions) := fixture.doc("aws_kms_key_policy", "module.k.aws_kms_key_policy.this", "policy", {"Statement": [
	{"Sid": "AccountKeyAdministration", "Effect": "Allow", "Principal": {"AWS": "arn:aws:iam::123456789012:root"}, "Action": actions, "Resource": "*"},
	{"Sid": "RolesViaS3ReadWrite", "Effect": "Allow", "Principal": {"AWS": "arn:aws:iam::123456789012:role/ci-plan"}, "Action": ["kms:Decrypt", "kms:GenerateDataKey"], "Resource": "*"},
]})

# 鍵の規則のテストでは、Resource "*" の Deny（PLAN-APPLY-02）を満たした policy を組む。
apply_policy(statements) := raw_apply_policy(array.concat(statements, [wildcard_deny]))

raw_apply_policy(statements) := fixture.doc("aws_iam_role_policy", "module.x.aws_iam_role_policy.this[\"apply\"]", "policy", {"Statement": statements})

key_statement(actions, resource) := {"Effect": "Deny", "Action": actions, "Resource": resource}

test_pass_delegated_actions_are_denied if {
	count(p.deny) == 0 with input as fixture.plan([key_policy(["kms:DescribeKey", "kms:PutKeyPolicy"]), apply_policy([key_statement(["kms:PutKeyPolicy", "KMS:DescribeKey"], key)])]) with data.policy_plan.deployment_identity as decl
}

# 暗号操作は委任に無いので、Deny に無くてよい。委任より広い Deny も通る。
test_pass_deny_wider_than_delegation if {
	count(p.deny) == 0 with input as fixture.plan([key_policy("kms:PutKeyPolicy"), apply_policy([key_statement(["kms:PutKeyPolicy", "kms:TagResource"], key)])]) with data.policy_plan.deployment_identity as decl
}

test_pass_kms_wildcard_deny if {
	count(p.deny) == 0 with input as fixture.plan([key_policy(["kms:DescribeKey", "kms:PutKeyPolicy"]), apply_policy([key_statement("kms:*", key)])]) with data.policy_plan.deployment_identity as decl
}

test_violation_delegated_action_not_denied if {
	msgs := p.deny with input as fixture.plan([key_policy(["kms:DescribeKey", "kms:PutKeyPolicy", "kms:ScheduleKeyDeletion"]), apply_policy([key_statement(["kms:DescribeKey", "kms:PutKeyPolicy"], key)])]) with data.policy_plan.deployment_identity as decl
	fixture.has("[PLAN-APPLY-01]", msgs)
	some m in msgs
	contains(m, "kms:schedulekeydeletion")
}

# 鍵の ARN でない Resource（Bucket や "*"）への Deny は、鍵の設定を守る Deny として数えない。
test_violation_deny_not_on_key if {
	every res in ["arn:aws:s3:::b", "*", "arn:aws:kms:ap-northeast-1:123456789012:alias/x"] {
		fixture.has("[PLAN-APPLY-01]", p.deny) with input as fixture.plan([key_policy("kms:PutKeyPolicy"), apply_policy([key_statement("kms:PutKeyPolicy", res)])]) with data.policy_plan.deployment_identity as decl
	}
}

# 条件の外の要求は拒まないので、Condition の付いた Deny は覆っていると数えない。
test_violation_conditional_deny if {
	conditional := object.union(key_statement("kms:PutKeyPolicy", key), {"Condition": {"Bool": {"aws:MultiFactorAuthPresent": "false"}}})
	fixture.has("[PLAN-APPLY-01]", p.deny) with input as fixture.plan([key_policy("kms:PutKeyPolicy"), apply_policy([conditional])]) with data.policy_plan.deployment_identity as decl
}

test_violation_delegation_unreadable if {
	bare := fixture.doc("aws_kms_key_policy", "module.k.aws_kms_key_policy.this", "policy", {"Statement": [{"Effect": "Allow", "Principal": {"AWS": "123456789012"}, "Action": "kms:PutKeyPolicy", "Resource": "*"}]})
	fixture.has("[PLAN-APPLY-00]", p.deny) with input as fixture.plan([bare, apply_policy([key_statement("kms:PutKeyPolicy", key)])]) with data.policy_plan.deployment_identity as decl
}

test_violation_allow_is_not_deny if {
	allow := {"Effect": "Allow", "Action": "kms:PutKeyPolicy", "Resource": key}
	fixture.has("[PLAN-APPLY-01]", p.deny) with input as fixture.plan([key_policy("kms:PutKeyPolicy"), apply_policy([allow])]) with data.policy_plan.deployment_identity as decl
}

# 同じ型のほかの instance（plan・apply-identity）の Deny は、apply の role の Deny として数えない。
test_violation_other_instance_deny if {
	other := fixture.doc("aws_iam_role_policy", "module.x.aws_iam_role_policy.this[\"apply_identity\"]", "policy", {"Statement": [key_statement("kms:PutKeyPolicy", key)]})
	fixture.has("[PLAN-APPLY-01]", p.deny) with input as fixture.plan([key_policy("kms:PutKeyPolicy"), apply_policy([]), other]) with data.policy_plan.deployment_identity as decl
}

test_violation_apply_policy_missing if {
	fixture.has("[PLAN-APPLY-00]", p.deny) with input as fixture.plan([key_policy("kms:PutKeyPolicy")]) with data.policy_plan.deployment_identity as decl
}

test_violation_declaration_missing if {
	fixture.has("[PLAN-APPLY-00]", p.deny) with input as fixture.plan([key_policy("kms:PutKeyPolicy"), apply_policy([key_statement("kms:PutKeyPolicy", key)])]) with data.policy_plan.deployment_identity as {}
}

test_violation_key_policy_missing if {
	fixture.has("[PLAN-APPLY-00]", p.deny) with input as fixture.plan([apply_policy([key_statement("kms:PutKeyPolicy", key)])]) with data.policy_plan.deployment_identity as decl
}

test_violation_wildcard_deny_missing if {
	msgs := p.deny with input as fixture.plan([key_policy("kms:PutKeyPolicy"), raw_apply_policy([key_statement("kms:PutKeyPolicy", key), {"Effect": "Deny", "Action": "logs:PutAccountPolicy", "Resource": "*"}])]) with data.policy_plan.deployment_identity as decl
	violations := {m | some m in msgs; startswith(m, "[PLAN-APPLY-02]")}
	count(violations) == 1
	some m in violations
	contains(m, "logs:putresourcepolicy")
}

# Resource を名指した Deny と Condition の付いた Deny は、アカウント全体の要求を拒まない。
test_violation_wildcard_deny_not_on_everything if {
	every s in [
		{"Effect": "Deny", "Action": ["logs:PutAccountPolicy", "logs:PutResourcePolicy"], "Resource": "arn:aws:logs:ap-northeast-1:123456789012:log-group:x"},
		{"Effect": "Deny", "Action": ["logs:PutAccountPolicy", "logs:PutResourcePolicy"], "Resource": "*", "Condition": {"StringEquals": {"aws:RequestedRegion": "ap-northeast-1"}}},
		{"Effect": "Allow", "Action": ["logs:PutAccountPolicy", "logs:PutResourcePolicy"], "Resource": "*"},
	] {
		fixture.has("[PLAN-APPLY-02]", p.deny) with input as fixture.plan([key_policy("kms:PutKeyPolicy"), raw_apply_policy([key_statement("kms:PutKeyPolicy", key), s])]) with data.policy_plan.deployment_identity as decl
	}
}

# 宣言の大文字小文字を区別しない。別の policy の Deny は apply の role を守らない。
test_violation_wildcard_deny_in_other_policy if {
	other := fixture.doc("aws_iam_role_policy", "module.x.aws_iam_role_policy.this[\"plan\"]", "policy", {"Statement": [wildcard_deny]})
	fixture.has("[PLAN-APPLY-02]", p.deny) with input as fixture.plan([key_policy("kms:PutKeyPolicy"), raw_apply_policy([key_statement("kms:PutKeyPolicy", key)]), other]) with data.policy_plan.deployment_identity as decl
}

test_violation_wildcard_denies_empty if {
	empty := {"apply_role": {"policy": decl.apply_role.policy, "wildcard_denies": []}}
	fixture.has("[PLAN-APPLY-00] data.policy_plan.deployment_identity.apply_role.wildcard_denies", p.deny) with input as fixture.plan([key_policy("kms:PutKeyPolicy"), apply_policy([key_statement("kms:PutKeyPolicy", key)])]) with data.policy_plan.deployment_identity as empty
}

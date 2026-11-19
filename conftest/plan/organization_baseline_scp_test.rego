package plan.organization_baseline_scp_test

import data.plan.fixture
import data.plan.organization_baseline_scp as p

m := "module.o."

decl := {
	"organization": "module.o.aws_organizations_organization.this",
	"organizational_units": {
		"Workloads": "module.o.aws_organizations_organizational_unit.top[\"Workloads\"]",
		"PolicyStaging": "module.o.aws_organizations_organizational_unit.top[\"PolicyStaging\"]",
		"Prod": "module.o.aws_organizations_organizational_unit.workloads[\"Prod\"]",
	},
	"policies": {k: sprintf("module.o.aws_organizations_policy.this[%q]", [k]) | some k in policy_keys},
	"attachments": {
		"deny-leave-and-close": ["Root"],
		"deny-root-user": ["PolicyStaging"],
		"deny-state-write": ["PolicyStaging"],
		"protect-governance-roles": ["PolicyStaging"],
		"deny-guardduty-stop": ["PolicyStaging"],
		"data-perimeter-prod-resources": ["PolicyStaging"],
		"data-perimeter-org-principals": ["PolicyStaging"],
		"data-perimeter-prod-principals": ["PolicyStaging"],
	},
	"assume_root_task_actions": ["s3:DeleteBucketPolicy", "s3:PutBucketPolicy", "sqs:SetQueueAttributes"],
	"break_glass": {"role": "module.b.aws_iam_role.this"},
}

policy_keys := [
	"deny-leave-and-close", "deny-root-user", "deny-state-write", "protect-governance-roles", "deny-guardduty-stop",
	"data-perimeter-prod-resources", "data-perimeter-org-principals", "data-perimeter-prod-principals",
]

change(address, type, after) := {"address": address, "type": type, "change": {"actions": ["create"], "after": after, "after_unknown": {}}}

bg_pattern := "arn:aws:iam::*:role/break-glass/tabp-break-glass"

prod_path := "o-exampleorg1/r-exam/ou-exam-workload/ou-exam-prodprod/"

valid_docs := {
	"deny-leave-and-close": {"Type": "SERVICE_CONTROL_POLICY", "Statement": [{"Effect": "Deny", "Action": ["account:CloseAccount", "organizations:LeaveOrganization"], "Resource": "*"}]},
	"deny-root-user": {"Type": "SERVICE_CONTROL_POLICY", "Statement": [{"Effect": "Deny", "Action": "*", "Resource": "*", "Condition": {"ArnLike": {"aws:PrincipalArn": "arn:aws:iam::*:root"}, "Null": {"aws:AssumedRoot": "true"}}}]},
	"deny-state-write": {"Type": "SERVICE_CONTROL_POLICY", "Statement": [{"Effect": "Deny", "Action": ["s3:PutObject*"], "Resource": "arn:aws:s3:::tabp-*-tfstate-*/*", "Condition": {"ArnNotLike": {"aws:PrincipalArn": ["arn:aws:iam::*:role/tabp-dev-ci-apply", bg_pattern]}}}]},
	"protect-governance-roles": {"Type": "SERVICE_CONTROL_POLICY", "Statement": [{"Effect": "Deny", "Action": ["iam:Put*"], "Resource": bg_pattern}]},
	"deny-guardduty-stop": {"Type": "SERVICE_CONTROL_POLICY", "Statement": [{"Effect": "Deny", "Action": ["guardduty:DeleteDetector"], "Resource": "*"}]},
	"data-perimeter-prod-resources": {"Type": "SERVICE_CONTROL_POLICY", "Statement": [{"Effect": "Deny", "Action": "*", "Resource": "*", "Condition": {"ForAnyValue:StringLike": {"aws:ResourceOrgPaths": concat("", [prod_path, "*"])}}}]},
	"data-perimeter-org-principals": {"Type": "RESOURCE_CONTROL_POLICY", "Statement": [{"Sid": "Org", "Effect": "Deny", "Principal": "*", "Action": ["s3:*", "sts:AssumeRole"], "Resource": "*", "Condition": {"StringNotEqualsIfExists": {"aws:PrincipalOrgID": "o-exampleorg1"}, "BoolIfExists": {"aws:PrincipalIsAWSService": "false"}}}]},
	"data-perimeter-prod-principals": {"Type": "RESOURCE_CONTROL_POLICY", "Statement": [{"Sid": "Prod", "Effect": "Deny", "Principal": "*", "Action": ["s3:*"], "Resource": "*", "Condition": {"ForAllValues:StringNotLike": {"aws:PrincipalOrgPaths": concat("", [prod_path, "*"])}, "BoolIfExists": {"aws:PrincipalIsAWSService": "false"}}}]},
}

ou(name, key, id) := change(sprintf("module.o.aws_organizations_organizational_unit.%s[%q]", [key, name]), "aws_organizations_organizational_unit", {"id": id, "name": name})

base := [
	change("module.o.aws_organizations_organization.this", "aws_organizations_organization", {"id": "o-exampleorg1", "roots": [{"id": "r-exam"}]}),
	ou("Workloads", "top", "ou-exam-workload"),
	ou("PolicyStaging", "top", "ou-exam-staginga"),
	ou("Prod", "workloads", "ou-exam-prodprod"),
	change("module.b.aws_iam_role.this", "aws_iam_role", {"name": "tabp-break-glass", "path": "/break-glass/"}),
]

policy(key, doc) := change(sprintf("module.o.aws_organizations_policy.this[%q]", [key]), "aws_organizations_policy", {
	"id": sprintf("p-%s", [key]),
	"type": doc.Type,
	"content": json.marshal({"Version": "2012-10-17", "Statement": doc.Statement}),
})

attachment(key, target) := change(sprintf("module.o.aws_organizations_policy_attachment.this[\"%s/x\"]", [key]), "aws_organizations_policy_attachment", {"policy_id": sprintf("p-%s", [key]), "target_id": target})

valid_attachments := [attachment(k, t) |
	some k, names in decl.attachments
	some n in names
	t := {"Root": "r-exam", "PolicyStaging": "ou-exam-staginga"}[n]
]

plan_with(docs, attachments) := fixture.plan(array.concat(array.concat(base, [policy(k, d) | some k, d in docs]), attachments))

valid := plan_with(valid_docs, valid_attachments)

with_doc(key, statement) := object.union(valid_docs, {key: {"Type": valid_docs[key].Type, "Statement": [statement]}})

test_pass_reference_shape if {
	count(p.deny) == 0 with input as valid with data.policy_plan as {"organization_baseline": decl}
}

test_violation_missing_declaration if {
	fixture.has("[PLAN-ORG-00]", p.deny) with input as valid with data.policy_plan as {}
}

test_violation_declared_policy_missing if {
	inp := plan_with(object.remove(valid_docs, ["deny-root-user"]), valid_attachments)
	fixture.has("[PLAN-ORG-00]", p.deny) with input as inp with data.policy_plan as {"organization_baseline": decl}
}

test_violation_extra_attachment_target if {
	inp := plan_with(valid_docs, array.concat(valid_attachments, [attachment("deny-root-user", "ou-exam-workload")]))
	fixture.has("[PLAN-ORG-01]", p.deny) with input as inp with data.policy_plan as {"organization_baseline": decl}
}

test_violation_missing_attachment if {
	inp := plan_with(valid_docs, [a | some a in valid_attachments; a.change.after.policy_id != "p-deny-root-user"])
	fixture.has("[PLAN-ORG-01]", p.deny) with input as inp with data.policy_plan as {"organization_baseline": decl}
}

test_violation_other_policy_on_root if {
	d := object.union(decl, {"attachments": object.union(decl.attachments, {"deny-root-user": ["PolicyStaging", "Root"]})})
	inp := plan_with(valid_docs, array.concat(valid_attachments, [attachment("deny-root-user", "r-exam")]))
	fixture.has("[PLAN-ORG-02]", p.deny) with input as inp with data.policy_plan as {"organization_baseline": d}
}

test_violation_leave_and_close_with_condition if {
	docs := with_doc("deny-leave-and-close", {"Effect": "Deny", "Action": ["account:CloseAccount", "organizations:LeaveOrganization"], "Resource": "*", "Condition": {"StringEquals": {"aws:PrincipalAccount": "1"}}})
	fixture.has("[PLAN-ORG-02]", p.deny) with input as plan_with(docs, valid_attachments) with data.policy_plan as {"organization_baseline": decl}
}

test_violation_root_user_blocks_assume_root if {
	docs := with_doc("deny-root-user", {"Effect": "Deny", "Action": "*", "Resource": "*", "Condition": {"ArnLike": {"aws:PrincipalArn": "arn:aws:iam::*:root"}}})
	fixture.has("[PLAN-ORG-03]", p.deny) with input as plan_with(docs, valid_attachments) with data.policy_plan as {"organization_baseline": decl}
}

test_violation_scp_blocks_bucket_policy_unlock if {
	docs := with_doc("deny-guardduty-stop", {"Effect": "Deny", "Action": ["guardduty:DeleteDetector", "s3:*BucketPolicy"], "Resource": "*"})
	fixture.has("[PLAN-ORG-04]", p.deny) with input as plan_with(docs, valid_attachments) with data.policy_plan as {"organization_baseline": decl}
}

test_violation_empty_task_actions if {
	fixture.has("[PLAN-ORG-04]", p.deny) with input as valid with data.policy_plan as {"organization_baseline": object.union(decl, {"assume_root_task_actions": []})}
}

test_violation_state_write_extra_exception if {
	docs := with_doc("deny-state-write", {"Effect": "Deny", "Action": ["s3:PutObject*"], "Resource": "*", "Condition": {"ArnNotLike": {"aws:PrincipalArn": ["arn:aws:iam::*:role/admin", bg_pattern]}}})
	fixture.has("[PLAN-ORG-05]", p.deny) with input as plan_with(docs, valid_attachments) with data.policy_plan as {"organization_baseline": decl}
}

test_violation_state_write_without_break_glass if {
	docs := with_doc("deny-state-write", {"Effect": "Deny", "Action": ["s3:PutObject*"], "Resource": "*", "Condition": {"ArnNotLike": {"aws:PrincipalArn": ["arn:aws:iam::*:role/tabp-dev-ci-apply"]}}})
	fixture.has("[PLAN-ORG-05]", p.deny) with input as plan_with(docs, valid_attachments) with data.policy_plan as {"organization_baseline": decl}
}

test_violation_break_glass_exempt_from_detection if {
	docs := with_doc("deny-guardduty-stop", {"Effect": "Deny", "Action": ["guardduty:DeleteDetector"], "Resource": "*", "Condition": {"ArnNotLike": {"aws:PrincipalArn": bg_pattern}}})
	fixture.has("[PLAN-ORG-05]", p.deny) with input as plan_with(docs, valid_attachments) with data.policy_plan as {"organization_baseline": decl}
}

test_violation_governance_roles_wider_exception if {
	docs := with_doc("protect-governance-roles", {"Effect": "Deny", "Action": ["iam:Put*"], "Resource": bg_pattern, "Condition": {"ArnNotLike": {"aws:PrincipalArn": "arn:aws:iam::*:role/stacksets-exec-*"}}})
	fixture.has("[PLAN-ORG-06]", p.deny) with input as plan_with(docs, valid_attachments) with data.policy_plan as {"organization_baseline": decl}
}

test_violation_prod_resources_wrong_path if {
	docs := with_doc("data-perimeter-prod-resources", {"Effect": "Deny", "Action": "*", "Resource": "*", "Condition": {"ForAnyValue:StringLike": {"aws:ResourceOrgPaths": "o-exampleorg1/*"}}})
	fixture.has("[PLAN-ORG-07]", p.deny) with input as plan_with(docs, valid_attachments) with data.policy_plan as {"organization_baseline": decl}
}

test_violation_rcp_blocks_web_identity if {
	docs := with_doc("data-perimeter-org-principals", {"Sid": "Org", "Effect": "Deny", "Principal": "*", "Action": ["sts:*"], "Resource": "*", "Condition": {"StringNotEqualsIfExists": {"aws:PrincipalOrgID": "o-exampleorg1"}, "BoolIfExists": {"aws:PrincipalIsAWSService": "false"}}})
	fixture.has("[PLAN-ORG-07]", p.deny) with input as plan_with(docs, valid_attachments) with data.policy_plan as {"organization_baseline": decl}
}

test_violation_guardduty_scp_denies_config if {
	docs := with_doc("deny-guardduty-stop", {"Effect": "Deny", "Action": ["guardduty:DeleteDetector", "config:StopConfigurationRecorder"], "Resource": "*"})
	fixture.has("[PLAN-ORG-08]", p.deny) with input as plan_with(docs, valid_attachments) with data.policy_plan as {"organization_baseline": decl}
}

test_violation_required_policy_missing_from_declaration if {
	d := object.union(object.remove(decl, ["policies"]), {"policies": object.remove(decl.policies, ["deny-root-user"])})
	fixture.has("[PLAN-ORG-00] 宣言の policies に deny-root-user", p.deny) with input as valid with data.policy_plan as {"organization_baseline": d}
}

test_violation_required_ou_missing_from_declaration if {
	d := object.union(object.remove(decl, ["organizational_units"]), {"organizational_units": object.remove(decl.organizational_units, ["Prod"])})
	fixture.has("[PLAN-ORG-00] 宣言の organizational_units に Prod", p.deny) with input as valid with data.policy_plan as {"organization_baseline": d}
}

test_violation_duplicate_attachment if {
	dup := change("module.o.aws_organizations_policy_attachment.this[\"deny-root-user/y\"]", "aws_organizations_policy_attachment", {"policy_id": "p-deny-root-user", "target_id": "ou-exam-staginga"})
	inp := plan_with(valid_docs, array.concat(valid_attachments, [dup]))
	fixture.has("[PLAN-ORG-01] policy deny-root-user が同じ付け先に2回", p.deny) with input as inp with data.policy_plan as {"organization_baseline": decl}
}

test_violation_attachment_target_unknown if {
	unknown := {"address": "module.o.aws_organizations_policy_attachment.this[\"x\"]", "type": "aws_organizations_policy_attachment", "change": {"actions": ["create"], "after": {"policy_id": "p-deny-root-user"}, "after_unknown": {"target_id": true}}}
	inp := plan_with(valid_docs, array.concat(valid_attachments, [unknown]))
	fixture.has("[PLAN-ORG-01] module.o.aws_organizations_policy_attachment.this[\"x\"] の target_id", p.deny) with input as inp with data.policy_plan as {"organization_baseline": decl}
}

test_violation_not_action_deny if {
	docs := with_doc("deny-guardduty-stop", {"Effect": "Deny", "NotAction": ["iam:Get*"], "Resource": "*"})
	fixture.has("[PLAN-ORG-04] policy deny-guardduty-stop の Deny が NotAction", p.deny) with input as plan_with(docs, valid_attachments) with data.policy_plan as {"organization_baseline": decl}
}

test_violation_resource_paths_outside_prod_do_not_spare_root_session if {
	d := object.union(decl, {"policies": object.union(decl.policies, {"deny-iam-user-like": "module.o.aws_organizations_policy.this[\"deny-iam-user-like\"]"}), "attachments": object.union(decl.attachments, {"deny-iam-user-like": []})})
	wide := object.union(valid_docs, {"deny-iam-user-like": {"Type": "SERVICE_CONTROL_POLICY", "Statement": [{"Effect": "Deny", "Action": "*", "Resource": "*", "Condition": {"ForAnyValue:StringLike": {"aws:ResourceOrgPaths": "o-exampleorg1/*"}}}]}})
	fixture.has("[PLAN-ORG-04] SCP deny-iam-user-like", p.deny) with input as plan_with(wide, valid_attachments) with data.policy_plan as {"organization_baseline": d}
}

test_violation_empty_principal_list_does_not_spare_root_session if {
	docs := with_doc("deny-guardduty-stop", {"Effect": "Deny", "Action": ["s3:PutBucketPolicy"], "Resource": "*", "Condition": {"ArnLike": {"aws:PrincipalArn": []}}})
	fixture.has("[PLAN-ORG-04] SCP deny-guardduty-stop", p.deny) with input as plan_with(docs, valid_attachments) with data.policy_plan as {"organization_baseline": decl}
}

test_violation_state_write_exception_with_wildcard_env if {
	docs := with_doc("deny-state-write", {"Effect": "Deny", "Action": ["s3:PutObject*"], "Resource": "*", "Condition": {"ArnNotLike": {"aws:PrincipalArn": ["arn:aws:iam::*:role/tabp-*-ci-apply", bg_pattern]}}})
	fixture.has("[PLAN-ORG-05] deny-state-write の例外 arn:aws:iam::*:role/tabp-*-ci-apply", p.deny) with input as plan_with(docs, valid_attachments) with data.policy_plan as {"organization_baseline": decl}
}

test_violation_prod_principals_without_condition if {
	docs := with_doc("data-perimeter-prod-principals", {"Sid": "Prod", "Effect": "Deny", "Principal": "*", "Action": ["s3:*"], "Resource": "*"})
	fixture.has("[PLAN-ORG-07] data-perimeter-prod-principals の文", p.deny) with input as plan_with(docs, valid_attachments) with data.policy_plan as {"organization_baseline": decl}
}

test_violation_prod_principals_other_operator if {
	docs := with_doc("data-perimeter-prod-principals", {"Sid": "Prod", "Effect": "Deny", "Principal": "*", "Action": ["s3:*"], "Resource": "*", "Condition": {"StringNotLike": {"aws:PrincipalOrgPaths": concat("", [prod_path, "*"])}, "BoolIfExists": {"aws:PrincipalIsAWSService": "false"}}})
	fixture.has("[PLAN-ORG-07] data-perimeter-prod-principals の文", p.deny) with input as plan_with(docs, valid_attachments) with data.policy_plan as {"organization_baseline": decl}
}

test_violation_org_principals_without_condition if {
	docs := with_doc("data-perimeter-org-principals", {"Sid": "Org", "Effect": "Deny", "Principal": "*", "Action": ["s3:*"], "Resource": "*"})
	fixture.has("[PLAN-ORG-07] data-perimeter-org-principals の文", p.deny) with input as plan_with(docs, valid_attachments) with data.policy_plan as {"organization_baseline": decl}
}

test_violation_rcp_without_service_exception if {
	docs := with_doc("data-perimeter-org-principals", {"Sid": "Org", "Effect": "Deny", "Principal": "*", "Action": ["s3:*"], "Resource": "*", "Condition": {"StringNotEqualsIfExists": {"aws:PrincipalOrgID": "o-exampleorg1"}}})
	fixture.has("[PLAN-ORG-07] RCP data-perimeter-org-principals の文", p.deny) with input as plan_with(docs, valid_attachments) with data.policy_plan as {"organization_baseline": decl}
}

test_violation_declared_organization_missing if {
	inp := fixture.plan(array.concat(
		[r | some r in base; r.type != "aws_organizations_organization"],
		array.concat([policy(k, d) | some k, d in valid_docs], valid_attachments),
	))
	fixture.has("[PLAN-ORG-00] 宣言した module.o.aws_organizations_organization.this が入力に", p.deny) with input as inp with data.policy_plan as {"organization_baseline": decl}
}

test_violation_root_id_undetermined if {
	org := change("module.o.aws_organizations_organization.this", "aws_organizations_organization", {"id": "o-exampleorg1", "roots": []})
	inp := fixture.plan(array.concat(
		array.concat([org], [r | some r in base; r.type != "aws_organizations_organization"]),
		array.concat([policy(k, d) | some k, d in valid_docs], valid_attachments),
	))
	fixture.has("[PLAN-ORG-00] 組織の Root の ID が plan で決まりません", p.deny) with input as inp with data.policy_plan as {"organization_baseline": decl}
}

test_violation_policy_content_not_json if {
	broken := change("module.o.aws_organizations_policy.this[\"deny-root-user\"]", "aws_organizations_policy", {"id": "p-deny-root-user", "type": "SERVICE_CONTROL_POLICY", "content": "{"})
	inp := fixture.plan(array.concat(
		array.concat(base, [policy(k, d) | some k, d in valid_docs; k != "deny-root-user"]),
		array.concat([broken], valid_attachments),
	))
	fixture.has("[PLAN-ORG-00] policy deny-root-user の文書が plan で決まらないか", p.deny) with input as inp with data.policy_plan as {"organization_baseline": decl}
}

test_violation_required_policy_without_statement_key if {
	empty := change("module.o.aws_organizations_policy.this[\"deny-guardduty-stop\"]", "aws_organizations_policy", {"id": "p-deny-guardduty-stop", "type": "SERVICE_CONTROL_POLICY", "content": "{}"})
	inp := fixture.plan(array.concat(
		array.concat(base, [policy(k, d) | some k, d in valid_docs; k != "deny-guardduty-stop"]),
		array.concat([empty], valid_attachments),
	))
	fixture.has("[PLAN-ORG-00] policy deny-guardduty-stop が文を持ちません", p.deny) with input as inp with data.policy_plan as {"organization_baseline": decl}
}

test_violation_policy_type_missing if {
	typeless := change("module.o.aws_organizations_policy.this[\"deny-root-user\"]", "aws_organizations_policy", {"id": "p-deny-root-user", "content": json.marshal({"Statement": valid_docs["deny-root-user"].Statement})})
	inp := fixture.plan(array.concat(
		array.concat(base, [policy(k, d) | some k, d in valid_docs; k != "deny-root-user"]),
		array.concat([typeless], valid_attachments),
	))
	fixture.has("[PLAN-ORG-00] policy deny-root-user の種類が", p.deny) with input as inp with data.policy_plan as {"organization_baseline": decl}
}

test_violation_attachments_declaration_missing if {
	d := object.remove(decl, ["attachments"])
	fixture.has("[PLAN-ORG-00] 宣言の attachments に deny-root-user", p.deny) with input as plan_with(valid_docs, []) with data.policy_plan as {"organization_baseline": d}
}

test_violation_attachment_of_undeclared_policy if {
	stray := change("module.o.aws_organizations_policy_attachment.this[\"stray\"]", "aws_organizations_policy_attachment", {"policy_id": "p-other", "target_id": "ou-exam-staginga"})
	inp := plan_with(valid_docs, array.concat(valid_attachments, [stray]))
	fixture.has("[PLAN-ORG-01] module.o.aws_organizations_policy_attachment.this[\"stray\"] が宣言の外の policy", p.deny) with input as inp with data.policy_plan as {"organization_baseline": decl}
}

test_violation_attachment_policy_id_missing if {
	missing := change("module.o.aws_organizations_policy_attachment.this[\"m\"]", "aws_organizations_policy_attachment", {"target_id": "ou-exam-staginga"})
	inp := plan_with(valid_docs, array.concat(valid_attachments, [missing]))
	fixture.has("[PLAN-ORG-01] module.o.aws_organizations_policy_attachment.this[\"m\"] の policy_id", p.deny) with input as inp with data.policy_plan as {"organization_baseline": decl}
}

test_pass_role_only_condition_spares_root_session if {
	docs := with_doc("deny-guardduty-stop", {"Effect": "Deny", "Action": ["guardduty:DeleteDetector", "s3:PutBucketPolicy"], "Resource": "*", "Condition": {"ArnLike": {"aws:PrincipalArn": ["arn:aws:iam::*:role/x"]}}})
	not fixture.has("[PLAN-ORG-04]", p.deny) with input as plan_with(docs, valid_attachments) with data.policy_plan as {"organization_baseline": decl}
}

test_violation_role_and_root_condition_does_not_spare_root_session if {
	docs := with_doc("deny-guardduty-stop", {"Effect": "Deny", "Action": ["guardduty:DeleteDetector", "s3:PutBucketPolicy"], "Resource": "*", "Condition": {"ArnLike": {"aws:PrincipalArn": ["arn:aws:iam::*:role/x", "arn:aws:iam::*:root"]}}})
	fixture.has("[PLAN-ORG-04] SCP deny-guardduty-stop が、sts:AssumeRoot の回復の Action s3:putbucketpolicy", p.deny) with input as plan_with(docs, valid_attachments) with data.policy_plan as {"organization_baseline": decl}
}

state_write_with(exception) := with_doc("deny-state-write", {"Effect": "Deny", "Action": ["s3:PutObject*"], "Resource": "*", "Condition": {"ArnNotLike": {"aws:PrincipalArn": [exception, bg_pattern]}}})

test_violation_state_write_exception_unknown_env if {
	fixture.has("[PLAN-ORG-05] deny-state-write の例外 arn:aws:iam::*:role/tabp-prod-ci-apply", p.deny) with input as plan_with(state_write_with("arn:aws:iam::*:role/tabp-prod-ci-apply"), valid_attachments) with data.policy_plan as {"organization_baseline": decl}
}

test_violation_state_write_exception_plan_role if {
	fixture.has("[PLAN-ORG-05] deny-state-write の例外 arn:aws:iam::*:role/tabp-dev-ci-plan", p.deny) with input as plan_with(state_write_with("arn:aws:iam::*:role/tabp-dev-ci-plan"), valid_attachments) with data.policy_plan as {"organization_baseline": decl}
}

test_violation_state_write_exception_suffix_appended if {
	fixture.has("[PLAN-ORG-05] deny-state-write の例外 arn:aws:iam::*:role/tabp-dev-ci-applyx", p.deny) with input as plan_with(state_write_with("arn:aws:iam::*:role/tabp-dev-ci-applyx"), valid_attachments) with data.policy_plan as {"organization_baseline": decl}
}

test_pass_state_write_exception_unlock_identity if {
	not fixture.has("[PLAN-ORG-05]", p.deny) with input as plan_with(state_write_with("arn:aws:iam::*:role/tabp-log-archive-ci-unlock-identity"), valid_attachments) with data.policy_plan as {"organization_baseline": decl}
}

test_violation_guardduty_policy_without_actions if {
	docs := with_doc("deny-guardduty-stop", {"Effect": "Deny", "Action": [], "Resource": "*"})
	fixture.has("[PLAN-ORG-08] deny-guardduty-stop が guardduty:DeleteDetector を拒んでいません", p.deny) with input as plan_with(docs, valid_attachments) with data.policy_plan as {"organization_baseline": decl}
}

package plan.iam_role_attachment_pairing_test

import data.plan.fixture
import data.plan.iam_role_attachment_pairing as gate

exclusive_address := "module.x.aws_iam_role_policy_attachments_exclusive.this"

attachment_address := "module.x.aws_iam_role_policy_attachment.read"

arn := "arn:aws:iam::123456789012:policy/read"

decl := {"iam_role_attachment_pairing": {"exclusives": [exclusive_address]}}

rc(type, address, after) := {"address": address, "type": type, "change": {"actions": ["create"], "after": after, "after_unknown": {}}}

exclusive(arns) := rc("aws_iam_role_policy_attachments_exclusive", exclusive_address, {"role_name": "r", "policy_arns": arns})

attachment(role, policy_arn) := rc("aws_iam_role_policy_attachment", attachment_address, {"role": role, "policy_arn": policy_arn})

messages(rcs) := m if {
	m := gate.deny with input as fixture.plan(rcs) with data.policy_plan as decl
}

test_pass_paired if {
	count(messages([exclusive([arn]), attachment("r", arn)])) == 0
}

test_pass_empty_exclusive_beside_declared if {
	empty := rc("aws_iam_role_policy_attachments_exclusive", "module.y.aws_iam_role_policy_attachments_exclusive.this", {"role_name": "other", "policy_arns": []})
	count(messages([exclusive([arn]), attachment("r", arn), empty])) == 0
}

test_violation_declaration_shape if {
	every d in [{}, {"iam_role_attachment_pairing": {}}, {"iam_role_attachment_pairing": {"exclusives": "x"}}] {
		fixture.has("[PLAN-ATTACH-00] 宣言の policy_plan.iam_role_attachment_pairing.exclusives が配列ではありません", gate.deny) with input as fixture.plan([exclusive([arn]), attachment("r", arn)]) with data.policy_plan as d
	}
	fixture.has("[PLAN-ATTACH-00] 宣言の policy_plan.iam_role_attachment_pairing.exclusives が空です", gate.deny) with input as fixture.plan([]) with data.policy_plan as {"iam_role_attachment_pairing": {"exclusives": []}}
}

# 入力が空でも、宣言した排他の resource が無ければ通さない。
test_violation_declared_exclusive_missing if {
	every rcs in [[], [exclusive([])], [attachment("r", arn)]] {
		fixture.has(sprintf("[PLAN-ATTACH-00] 宣言した %s が", [exclusive_address]), messages(rcs))
	}
}

test_violation_exclusive_without_attachment if {
	fixture.has(sprintf("[PLAN-ATTACH-01] p.json: %s が載せる %s を、role r へ付ける aws_iam_role_policy_attachment が 0 件です", [exclusive_address, arn]), messages([exclusive([arn])]))
}

test_violation_attachment_on_other_role_or_policy if {
	every a in [attachment("other", arn), attachment("r", "arn:aws:iam::aws:policy/ReadOnlyAccess")] {
		msgs := messages([exclusive([arn]), a])
		fixture.has(sprintf("[PLAN-ATTACH-01] p.json: %s が載せる %s を", [exclusive_address, arn]), msgs)
		fixture.has(sprintf("[PLAN-ATTACH-01] p.json: %s の policy が", [attachment_address]), msgs)
	}
}

test_violation_two_attachments if {
	twice := rc("aws_iam_role_policy_attachment", "module.x.aws_iam_role_policy_attachment.again", {"role": "r", "policy_arn": arn})
	fixture.has(sprintf("[PLAN-ATTACH-01] p.json: %s が載せる %s を、role r へ付ける aws_iam_role_policy_attachment が 2 件です", [exclusive_address, arn]), messages([exclusive([arn]), attachment("r", arn), twice]))
}

test_violation_attachment_on_role_without_exclusive if {
	lonely := rc("aws_iam_role_policy_attachment", "module.x.aws_iam_role_policy_attachment.lonely", {"role": "lonely", "policy_arn": arn})
	fixture.has("[PLAN-ATTACH-01] p.json: module.x.aws_iam_role_policy_attachment.lonely の policy が", messages([exclusive([arn]), attachment("r", arn), lonely]))
}

test_violation_attachment_in_other_plan if {
	inp := [
		{"path": "p.json", "contents": {"plan_format_version": "1.2", "resource_changes": [exclusive([arn])]}},
		{"path": "q.json", "contents": {"plan_format_version": "1.2", "resource_changes": [attachment("r", arn)]}},
	]
	msgs := gate.deny with input as inp with data.policy_plan as decl
	fixture.has(sprintf("[PLAN-ATTACH-01] p.json: %s が載せる", [exclusive_address]), msgs)
	fixture.has(sprintf("[PLAN-ATTACH-01] q.json: %s の policy が", [attachment_address]), msgs)
}

test_violation_unknown_values if {
	ue := json.patch(exclusive([arn]), [{"op": "add", "path": "/change/after_unknown/policy_arns", "value": [true]}])
	fixture.has(sprintf("[PLAN-ATTACH-01] p.json: %s の policy_arns を plan で読めません", [exclusive_address]), messages([ue, attachment("r", arn)]))
	ua := json.patch(attachment("r", arn), [{"op": "add", "path": "/change/after_unknown/policy_arn", "value": true}])
	fixture.has(sprintf("[PLAN-ATTACH-01] p.json: %s の policy_arn を plan で読めません", [attachment_address]), messages([exclusive([arn]), ua]))
}

test_pass_deleted_exclusive if {
	deleted := {"address": exclusive_address, "type": "aws_iam_role_policy_attachments_exclusive", "change": {"actions": ["delete"], "after": null, "after_unknown": {}}}
	other := rc("aws_iam_role_policy_attachments_exclusive", "module.z.aws_iam_role_policy_attachments_exclusive.this", {"role_name": "z", "policy_arns": [arn]})
	msgs := gate.deny with input as fixture.plan([deleted, other, rc("aws_iam_role_policy_attachment", "module.z.aws_iam_role_policy_attachment.this", {"role": "z", "policy_arn": arn})]) with data.policy_plan as {"iam_role_attachment_pairing": {"exclusives": ["module.z.aws_iam_role_policy_attachments_exclusive.this"]}}
	count(msgs) == 0
}

test_violation_unknown_role if {
	ue := {"address": exclusive_address, "type": "aws_iam_role_policy_attachments_exclusive", "change": {"actions": ["create"], "after": {"policy_arns": [arn]}, "after_unknown": {"role_name": true}}}
	ua := {"address": attachment_address, "type": "aws_iam_role_policy_attachment", "change": {"actions": ["create"], "after": {"policy_arn": arn}, "after_unknown": {"role": true}}}
	msgs := messages([ue, ua])
	fixture.has(sprintf("[PLAN-ATTACH-01] p.json: %s の role_name を plan で読めません", [exclusive_address]), msgs)
	fixture.has(sprintf("[PLAN-ATTACH-01] p.json: %s の role を plan で読めません", [attachment_address]), msgs)
}

test_violation_partially_unknown_policy_arns if {
	ue := json.patch(exclusive([arn]), [{"op": "add", "path": "/change/after_unknown/policy_arns", "value": [false, true]}])
	fixture.has(sprintf("[PLAN-ATTACH-01] p.json: %s の policy_arns を plan で読めません", [exclusive_address]), messages([ue, attachment("r", arn)]))
}

# 削除の計画は在ることに数えない。
test_violation_declared_exclusive_only_deleted if {
	deleted := {"address": exclusive_address, "type": "aws_iam_role_policy_attachments_exclusive", "change": {"actions": ["delete"], "after": null, "after_unknown": {}}}
	fixture.has(sprintf("[PLAN-ATTACH-00] 宣言した %s が", [exclusive_address]), messages([deleted, attachment("r", arn)]))
}

test_violation_attachment_deleted_while_exclusive_remains if {
	deleted := {"address": attachment_address, "type": "aws_iam_role_policy_attachment", "change": {"actions": ["delete"], "after": null, "after_unknown": {}}}
	fixture.has(sprintf("[PLAN-ATTACH-01] p.json: %s が載せる %s を、role r へ付ける aws_iam_role_policy_attachment が 0 件です", [exclusive_address, arn]), messages([exclusive([arn]), deleted]))
}

test_violation_one_of_two_arns_unpaired if {
	other_arn := "arn:aws:iam::123456789012:policy/write"
	msgs := messages([exclusive([arn, other_arn]), attachment("r", arn)])
	fixture.has(sprintf("[PLAN-ATTACH-01] p.json: %s が載せる %s を、role r へ付ける aws_iam_role_policy_attachment が 0 件です", [exclusive_address, other_arn]), msgs)
	not fixture.has(sprintf("[PLAN-ATTACH-01] p.json: %s が載せる %s を", [exclusive_address, arn]), msgs)
}

test_violation_second_declared_exclusive_missing if {
	d := {"iam_role_attachment_pairing": {"exclusives": [exclusive_address, "module.z.aws_iam_role_policy_attachments_exclusive.this"]}}
	msgs := gate.deny with input as fixture.plan([exclusive([arn]), attachment("r", arn)]) with data.policy_plan as d
	fixture.has("[PLAN-ATTACH-00] 宣言した module.z.aws_iam_role_policy_attachments_exclusive.this が", msgs)
	not fixture.has(sprintf("[PLAN-ATTACH-00] 宣言した %s が", [exclusive_address]), msgs)
}

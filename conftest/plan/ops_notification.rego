# ops-notification の合成の不変条件（ops-notification/ADR-0101・0102・0201、ADR-0301 決定7・12）。Topic Policy と Key Policy と
# IAM は別の module が組むので、送り手と publish する role が両方の文書で一致することを、ここで突き合わせる。
package plan.ops_notification

import data.plan.lib

decl := data.policy_plan.ops_notification

address_paths := [
	["topic"], ["topic_policy"], ["key_policy"],
	["heartbeat", "role"], ["heartbeat", "policy"], ["heartbeat", "schedule_group"], ["heartbeat", "schedule"],
	["chat", "role"], ["chat", "policy"], ["chat", "attachments"], ["chat", "configuration"],
]

declared_string(path) if {
	v := object.get(decl, path, null)
	is_string(v)
	v != ""
}

deny contains msg if {
	some path in array.concat(address_paths, [["module"]])
	not declared_string(path)
	msg := sprintf("[PLAN-NOTIFY-00] 宣言の policy_plan.ops_notification.%s が、空でない文字列ではありません", [concat(".", path)])
}

string_set(xs) if {
	is_array(xs)
	count(xs) > 0
	every x in xs {
		is_string(x)
	}
}

deny contains msg if {
	some path in [["publishers"], ["chat", "read_actions"]]
	not string_set(object.get(decl, path, null))
	msg := sprintf("[PLAN-NOTIFY-00] 宣言の policy_plan.ops_notification.%s が、空でない文字列の配列ではありません", [concat(".", path)])
}

after(address) := c.rc.change.after if {
	some c in lib.changes
	c.rc.address == address
	is_object(c.rc.change.after)
}

deny contains msg if {
	some path in address_paths
	declared_string(path)
	address := object.get(decl, path, null)
	not after(address)
	msg := sprintf("[PLAN-NOTIFY-00] 宣言の policy_plan.ops_notification.%s の %s が入力にありません。address が変わったなら宣言を直してください", [concat(".", path), address])
}

# 突き合わせに使う値。plan で決まらなければ比べられないので落とす。
known_values := [
	["topic", "arn"], ["heartbeat", "role", "arn"], ["heartbeat", "role", "name"], ["heartbeat", "schedule_group", "arn"],
	["chat", "role", "arn"], ["chat", "policy", "arn"],
]

value(path) := v if {
	address := object.get(decl, array.slice(path, 0, count(path) - 1), null)
	is_string(address)
	v := object.get(after(address), path[count(path) - 1], null)
	is_string(v)
}

deny contains msg if {
	some path in known_values
	address := object.get(decl, array.slice(path, 0, count(path) - 1), null)
	after(address)
	not value(path)
	msg := sprintf("[PLAN-NOTIFY-00] %s の %s が plan で決まりません。tests/policy-input.tftest.hcl の override_resource で与えてください", [address, path[count(path) - 1]])
}

# 読む文書。plan で決まらないか読めなければ、それを見る規則が黙るので 00 で落とす。
document_paths := [
	[["topic_policy"], "policy"], [["key_policy"], "policy"], [["heartbeat", "policy"], "policy"],
	[["chat", "policy"], "policy"], [["heartbeat", "role"], "assume_role_policy"], [["chat", "role"], "assume_role_policy"],
]

readable_document(address, attr) if {
	a := after(address)
	is_string(a[attr])
	json.is_valid(a[attr])
}

deny contains msg if {
	some p in document_paths
	address := object.get(decl, p[0], null)
	is_string(address)
	some c in lib.changes
	c.rc.address == address
	not readable_document(address, p[1])
	msg := sprintf("[PLAN-NOTIFY-00] %s の %s が plan で決まらないか、JSON の文書として読めません", [address, p[1]])
}

body(address) := b if {
	some r in lib.readable
	r.document.address == address
	b := r.body
}

topic_arn := value(["topic", "arn"])

account := split(topic_arn, ":")[4]

service_principals(b) := {p |
	some s in lib.statements(b)
	s.Effect == "Allow"
	is_object(s.Principal)
	some p in lib.as_array(object.get(s.Principal, "Service", []))
}

publishers := {p | some p in decl.publishers}

deny contains msg if {
	got := service_principals(body(decl.topic_policy))
	got != publishers
	msg := sprintf("[PLAN-NOTIFY-01] %s の送り手 %v が、宣言の送り手 %v と一致しません（ops-notification/ADR-0101）", [decl.topic_policy, sort(got), sort(publishers)])
}

deny contains msg if {
	got := service_principals(body(decl.key_policy))
	got != publishers
	msg := sprintf("[PLAN-NOTIFY-01] %s のサービス principal %v が、宣言の送り手 %v と一致しません。Topic Policy と Key Policy の送り手は同じにします", [decl.key_policy, sort(got), sort(publishers)])
}

# EventBridge は aws:Source* を受けないので、Key Policy は暗号化コンテキストで Topic を限る（modules/kms）。
events_names_topic(b) if {
	some s in lib.statements(b)
	s.Effect == "Allow"
	"events.amazonaws.com" in lib.as_array(object.get(object.get(s, "Principal", {}), "Service", []))
	topic_arn in lib.limiting_values(s, "kms:encryptioncontext:aws:sns:topicarn")
}

deny contains msg if {
	"events.amazonaws.com" in publishers
	b := body(decl.key_policy)
	not events_names_topic(b)
	msg := sprintf("[PLAN-NOTIFY-02] %s の events.amazonaws.com の文が、暗号化コンテキスト aws:sns:topicArn で Topic %s を名指しません", [decl.key_policy, topic_arn])
}

# IAM で Topic へ publish できる role（inline policy の Allow の Action が sns:Publish に当たり、Resource が Topic に当たる）。
role_arn_by_name := {c.rc.change.after.name: c.rc.change.after.arn |
	some c in lib.changes
	c.rc.type == "aws_iam_role"
	is_object(c.rc.change.after)
	is_string(c.rc.change.after.name)
	is_string(c.rc.change.after.arn)
}

grants_publish(s) if {
	s.Effect == "Allow"
	publish_action(s)
	reaches_topic(s)
}

publish_action(s) if {
	some a in lib.as_array(object.get(s, "Action", []))
	lib.iam_match(lower(a), "sns:publish")
}

publish_action(s) if {
	"NotAction" in object.keys(s)
	not excludes_publish(s)
}

excludes_publish(s) if {
	some a in lib.as_array(s.NotAction)
	lib.iam_match(lower(a), "sns:publish")
}

reaches_topic(s) if {
	some res in lib.as_array(object.get(s, "Resource", []))
	lib.iam_match(res, topic_arn)
}

reaches_topic(s) if "NotResource" in object.keys(s)

publishing_policies contains {"address": r.document.address, "role": object.get(c.rc.change.after, "role", null)} if {
	some r in lib.readable
	r.document.type == "aws_iam_role_policy"
	some c in lib.changes
	c.rc.address == r.document.address
	some s in lib.statements(r.body)
	grants_publish(s)
}

iam_publishers contains role_arn_by_name[p.role] if some p in publishing_policies

# 付け先の role を入力から引けない policy を黙って数えから落とさない。
deny contains msg if {
	some p in publishing_policies
	not role_arn_by_name[p.role]
	msg := sprintf("[PLAN-NOTIFY-03] %s が Topic へ publish できるが、付け先の role %v が入力に無いか、その arn が plan で決まりません", [p.address, object.get(p, "role", null)])
}

key_publishers contains a if {
	some s in lib.statements(body(decl.key_policy))
	s.Effect == "Allow"
	some v in lib.limiting_values(s, "kms:viaservice")
	startswith(v, "sns.")
	topic_arn in lib.limiting_values(s, "kms:encryptioncontext:aws:sns:topicarn")
	some a in lib.as_array(object.get(object.get(s, "Principal", {}), "AWS", []))
}

deny contains msg if {
	topic_arn
	iam_publishers != key_publishers
	msg := sprintf("[PLAN-NOTIFY-03] IAM で Topic へ sns:Publish を持つ role %v と、Key Policy が SNS 経由で Topic に許す role %v が一致しません", [sort(iam_publishers), sort(key_publishers)])
}

trust(address) := json.unmarshal(after(address).assume_role_policy) if is_string(after(address).assume_role_policy)

heartbeat_trust := {
	"Version": "2012-10-17",
	"Statement": [{
		"Effect": "Allow",
		"Principal": {"Service": "scheduler.amazonaws.com"},
		"Action": "sts:AssumeRole",
		"Condition": {"StringEquals": {"aws:SourceAccount": account, "aws:SourceArn": value(["heartbeat", "schedule_group", "arn"])}},
	}],
}

deny contains msg if {
	after(decl.heartbeat.role)
	not trust(decl.heartbeat.role) == heartbeat_trust
	msg := sprintf("[PLAN-NOTIFY-04] %s の信頼が、scheduler.amazonaws.com を aws:SourceAccount と schedule group の aws:SourceArn（StringEquals）で限る1文ではありません", [decl.heartbeat.role])
}

chat_trust := {
	"Version": "2012-10-17",
	"Statement": [{"Effect": "Allow", "Principal": {"Service": "chatbot.amazonaws.com"}, "Action": "sts:AssumeRole", "Condition": {"StringEquals": {"aws:SourceAccount": account}}}],
}

deny contains msg if {
	after(decl.chat.role)
	not trust(decl.chat.role) == chat_trust
	msg := sprintf("[PLAN-NOTIFY-04] %s の信頼が、chatbot.amazonaws.com を aws:SourceAccount（StringEquals）で自アカウントに限る1文ではありません", [decl.chat.role])
}

publish_to_topic_only(s) if {
	s.Effect == "Allow"
	lib.lower_all(s.Action) == ["sns:publish"]
	lib.as_array(s.Resource) == [topic_arn]
}

deny contains msg if {
	b := body(decl.heartbeat.policy)
	some s in lib.statements(b)
	not publish_to_topic_only(s)
	msg := sprintf("[PLAN-NOTIFY-05] %s が、Topic %s への sns:Publish の Allow だけではありません", [decl.heartbeat.policy, topic_arn])
}

deny contains msg if {
	a := after(decl.heartbeat.policy)
	not a.role == value(["heartbeat", "role", "name"])
	msg := sprintf("[PLAN-NOTIFY-05] %s が心拍の role %s に付いていません", [decl.heartbeat.policy, decl.heartbeat.role])
}

deny contains msg if {
	t := object.get(after(decl.heartbeat.schedule), "target", [])
	not schedule_targets_topic(t)
	msg := sprintf("[PLAN-NOTIFY-05] %s の target が、心拍の role で Topic %s を指す1つではありません", [decl.heartbeat.schedule, topic_arn])
}

schedule_targets_topic(t) if {
	count(t) == 1
	t[0].arn == topic_arn
	t[0].role_arn == value(["heartbeat", "role", "arn"])
}

chat_policy_set := {value(["chat", "policy", "arn"])}

deny contains msg if {
	c := after(decl.chat.configuration)
	not {a | some a in object.get(c, "guardrail_policy_arns", [])} == chat_policy_set
	msg := sprintf("[PLAN-NOTIFY-06] %s の guardrail が、読み取りの policy %s だけではありません。guardrail を省くと AdministratorAccess になります", [decl.chat.configuration, decl.chat.policy])
}

deny contains msg if {
	a := after(decl.chat.attachments)
	not {p | some p in object.get(a, "policy_arns", [])} == chat_policy_set
	msg := sprintf("[PLAN-NOTIFY-06] %s が channel role に付ける policy が、guardrail と同じ読み取りの policy だけではありません", [decl.chat.attachments])
}

deny contains msg if {
	a := after(decl.chat.attachments)
	not a.role_name == after(decl.chat.role).name
	msg := sprintf("[PLAN-NOTIFY-06] %s が channel role %s に付いていません", [decl.chat.attachments, decl.chat.role])
}

deny contains msg if {
	c := after(decl.chat.configuration)
	not {t | some t in object.get(c, "sns_topic_arns", [])} == {topic_arn}
	msg := sprintf("[PLAN-NOTIFY-06] %s が購読する Topic が、%s だけではありません", [decl.chat.configuration, topic_arn])
}

deny contains msg if {
	c := after(decl.chat.configuration)
	not c.user_authorization_required == true
	msg := sprintf("[PLAN-NOTIFY-06] %s の user_authorization_required が true ではありません", [decl.chat.configuration])
}

deny contains msg if {
	c := after(decl.chat.configuration)
	not c.iam_role_arn == value(["chat", "role", "arn"])
	msg := sprintf("[PLAN-NOTIFY-06] %s の channel role が %s ではありません", [decl.chat.configuration, decl.chat.role])
}

read_actions := {lower(a) | some a in decl.chat.read_actions}

chat_policy_actions := {a |
	some s in lib.statements(body(decl.chat.policy))
	some a in lib.lower_all(object.get(s, "Action", []))
}

deny contains msg if {
	b := body(decl.chat.policy)
	chat_policy_actions != read_actions
	msg := sprintf("[PLAN-NOTIFY-07] %s の Action %v が、read の Action 集合 %v と一致しません（ops-notification/ADR-0201）", [decl.chat.policy, sort(chat_policy_actions), sort(read_actions)])
}

deny contains msg if {
	b := body(decl.chat.policy)
	some s in lib.statements(b)
	not read_statement(s)
	msg := sprintf("[PLAN-NOTIFY-07] %s が、NotAction・NotResource を使わない Allow だけではありません", [decl.chat.policy])
}

read_statement(s) if {
	s.Effect == "Allow"
	not "NotAction" in object.keys(s)
	not "NotResource" in object.keys(s)
}

# 作る role がすべて permissions boundary を持つ（deployment-identity の apply の role が作れる role の範囲）。
deny contains msg if {
	some c in lib.changes
	c.rc.type == "aws_iam_role"
	startswith(lib.strip_index(c.rc.address), decl.module)
	is_object(c.rc.change.after)
	not boundary(c.rc.change)
	msg := sprintf("[PLAN-NOTIFY-08] %s: %s が permissions boundary を持ちません", [c.path, c.rc.address])
}

boundary(change) if {
	is_string(change.after.permissions_boundary)
	change.after.permissions_boundary != ""
}


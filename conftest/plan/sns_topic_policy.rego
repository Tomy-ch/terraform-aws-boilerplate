# SNS の Topic Policy が、同じアカウントの AWS のサービスの publish だけを許し、TLS でない publish を拒むこと
# （ADR-0301 決定1・18、modules/sns）。Security の道具は aws_sns_topic_policy の文書の Principal と条件を見ないので、
# Policy が持つ（ADR-0402 決定4）。宣言に依らず、入力のすべての Topic と Topic Policy に掛かる。
package plan.sns_topic_policy

import data.plan.lib

topic_policies contains r if {
	some r in lib.readable
	r.document.type == "aws_sns_topic_policy"
}

# 管理からの除外（forget）と削除は after を持たない。
topics contains c if {
	some c in lib.changes
	c.rc.type == "aws_sns_topic"
	is_object(c.rc.change.after)
}

raw_arn(r) := r.document.change.after.arn if is_string(r.document.change.after.arn)

# 形の崩れた ARN はアカウントを読めず、04 が黙る。00 で落とす。
topic_arn(r) := raw_arn(r) if regex.match(`^arn:aws[a-z-]*:sns:[a-z0-9-]+:[0-9]{12}:[A-Za-z0-9_-]{1,256}$`, raw_arn(r))

account_of(arn) := split(arn, ":")[4]

publish_only(s) if lib.lower_all(s.Action) == ["sns:publish"]

deny contains msg if {
	some d in lib.documents
	d.type == "aws_sns_topic_policy"
	not d.change.actions == ["forget"]
	not lib.parsed(d)
	msg := sprintf("[PLAN-SNS-00] %s: %s の %s が plan で決まらないか、JSON の文書として読めません", [d.path, d.address, d.attr])
}

deny contains msg if {
	some r in topic_policies
	arn := raw_arn(r)
	not topic_arn(r)
	msg := sprintf("[PLAN-SNS-00] %s: %s の arn %s が SNS の Topic の ARN の形ではありません", [r.document.path, r.document.address, arn])
}

deny contains msg if {
	some r in topic_policies
	not raw_arn(r)
	msg := sprintf("[PLAN-SNS-00] %s: %s の arn が plan で決まりません。Topic の ARN を tests/policy-input.tftest.hcl の override_resource で与えてください", [r.document.path, r.document.address])
}

# TLS でない publish を、すべての主体について拒む文（Amazon SNS security best practices）。
tls_deny(s, arn) if {
	s.Effect == "Deny"
	any_principal(s.Principal)
	"sns:publish" in lib.lower_all(s.Action)
	lib.as_array(s.Resource) == [arn]
	s.Condition == {"Bool": {"aws:SecureTransport": "false"}}
}

any_principal("*")

any_principal({"AWS": "*"})

deny contains msg if {
	some r in topic_policies
	arn := topic_arn(r)
	count([s | some s in lib.statements(r.body); tls_deny(s, arn)]) == 0
	msg := sprintf("[PLAN-SNS-01] %s: %s が、TLS でない sns:Publish を Principal \"*\" に拒む文（Bool aws:SecureTransport = \"false\"）を持ちません", [r.document.path, r.document.address])
}

allows(body) := [s | some s in lib.statements(body); s.Effect == "Allow"]

service_principal(p) if {
	is_object(p)
	object.keys(p) == {"Service"}
	every v in lib.as_array(p.Service) {
		is_string(v)
		regex.match(`^[a-z0-9-]+\.amazonaws\.com$`, v)
	}
}

deny contains msg if {
	some r in topic_policies
	some s in allows(r.body)
	not service_principal(object.get(s, "Principal", null))
	msg := sprintf("[PLAN-SNS-02] %s: %s の Allow の Principal が、AWS のサービス principal（Service だけの object）ではありません", [r.document.path, r.document.address])
}

deny contains msg if {
	some r in topic_policies
	some s in allows(r.body)
	not publish_only(s)
	msg := sprintf("[PLAN-SNS-03] %s: %s の Allow の Action が sns:Publish だけではありません", [r.document.path, r.document.address])
}

deny contains msg if {
	some r in topic_policies
	arn := topic_arn(r)
	some s in allows(r.body)
	not lib.as_array(object.get(s, "Resource", [])) == [arn]
	msg := sprintf("[PLAN-SNS-03] %s: %s の Allow の Resource が Topic の ARN %s だけではありません", [r.document.path, r.document.address, arn])
}

# 条件は、Topic のアカウントの固定値の aws:SourceAccount だけ（Security Hub SNS.4 が非公開とみなす形）。
own_account_only(s, arn) if {
	is_object(s.Condition)
	is_object(s.Condition.StringEquals)
	object.keys(s.Condition) == {"StringEquals"}
	object.keys(s.Condition.StringEquals) == {"aws:SourceAccount"}
	lib.as_array(s.Condition.StringEquals["aws:SourceAccount"]) == [account_of(arn)]
}

deny contains msg if {
	some r in topic_policies
	arn := topic_arn(r)
	some s in allows(r.body)
	not own_account_only(s, arn)
	msg := sprintf("[PLAN-SNS-04] %s: %s の Allow の Condition が、Topic のアカウント %s の StringEquals aws:SourceAccount だけではありません", [r.document.path, r.document.address, account_of(arn)])
}

policies_on(arn) := {r.document.address | some r in topic_policies; topic_arn(r) == arn}

deny contains msg if {
	some c in topics
	arn := c.rc.change.after.arn
	is_string(arn)
	count(policies_on(arn)) != 1
	msg := sprintf("[PLAN-SNS-05] %s: Topic %s を指す Topic Policy が %d 件です（1件を期待）", [c.path, c.rc.address, count(policies_on(arn))])
}

deny contains msg if {
	some c in topics
	not is_string(object.get(c.rc.change.after, "arn", null))
	msg := sprintf("[PLAN-SNS-05] %s: Topic %s の arn が plan で決まらないので、Topic Policy が1つであるかを確かめられません", [c.path, c.rc.address])
}

topic_arns := {c.rc.change.after.arn | some c in topics; is_string(c.rc.change.after.arn)}

deny contains msg if {
	some r in topic_policies
	arn := topic_arn(r)
	not arn in topic_arns
	msg := sprintf("[PLAN-SNS-05] %s: %s が、入力に無い Topic %s を指します", [r.document.path, r.document.address, arn])
}

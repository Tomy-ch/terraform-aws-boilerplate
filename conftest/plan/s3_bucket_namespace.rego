# すべての aws_s3_bucket を account regional namespace で作る（ADR-0207 決定1・7）。PLAN-KMS-12（ADR-0301 決定12）はこの性質を前提に、
# Bucket の名前で所有を読む。
package plan.s3_bucket_namespace

import data.plan.lib

# lib.changes は forget を除かないので使わない。削除と管理からの除外だけの変更は、変更後の Bucket を持たない。
buckets contains {"path": f.path, "rc": rc} if {
	some f in input
	some rc in f.contents.resource_changes
	object.get(rc, "mode", "managed") == "managed"
	rc.type == "aws_s3_bucket"
	some a in rc.change.actions
	not a in {"delete", "forget"}
}

unknown(c) if lib.unknown_value(object.get(object.get(c.rc.change, "after_unknown", {}), "bucket_namespace", false))

namespace(c) := object.get(c.rc.change.after, "bucket_namespace", null) if is_object(c.rc.change.after)

namespace(c) := null if not is_object(c.rc.change.after)

deny contains msg if {
	some c in buckets
	not unknown(c)
	namespace(c) != "account-regional"
	msg := sprintf("[PLAN-S3NS-01] %s: %s の bucket_namespace が %v です。\"account-regional\" にしてください", [c.path, c.rc.address, namespace(c)])
}

deny contains msg if {
	some c in buckets
	unknown(c)
	msg := sprintf("[PLAN-S3NS-02] %s: %s の bucket_namespace が plan で決まりません。\"account-regional\" を書いてください", [c.path, c.rc.address])
}

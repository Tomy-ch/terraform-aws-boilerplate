# RAM の共有が組織の外の principal を許さないこと（repository-wide/ADR-0605、ADR-0301 決定18）。Security の道具は
# aws_ram_resource_share の allow_external_principals を見ないので、Policy が持つ（ADR-0402 決定4）。宣言に依らず、
# 入力のすべての共有に掛かる。
package plan.ram_resource_share

import data.plan.lib

deny contains msg if {
	some c in lib.changes
	c.rc.type == "aws_ram_resource_share"
	is_object(c.rc.change.after)
	not c.rc.change.after.allow_external_principals == false
	msg := sprintf("[PLAN-RAM-01] %s: %s の allow_external_principals が plan で false に決まりません。組織の外の principal を共有の宛先にできます", [c.path, c.rc.address])
}

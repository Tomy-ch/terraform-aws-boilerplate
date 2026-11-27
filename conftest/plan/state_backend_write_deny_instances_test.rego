# lib/instances.rego の関数のテスト。lib は deny を持たないので、最も多く使う state_backend_write_deny のテストの package に置く。
package plan.state_backend_write_deny_test

import data.plan.lib

inst_security := {"name": "security", "state_backend": "module.sm.", "deployment_identity": "module.dm.", "creating": {"reason": "作成の plan"}}

inst_decl_with(instances) := {
	"deployment_identity": {
		"module": "module.d.",
		"apply_role": {"bootstrap": {"modules": ["module.s.", "module.d."]}},
		"apply_identity_role": {"role": "module.d.module.github_oidc.aws_iam_role.apply_identity", "output_bucket": "module.s.module.plan_output.aws_s3_bucket.this"},
	},
	"state_backend": {"module": "module.s.", "state_key": "module.s.module.kms.aws_kms_key.this", "instances": instances},
}

inst_decl := inst_decl_with({"admin-identity": [inst_security]})

rc(address, actions) := {"address": address, "type": "aws_kms_key", "change": {"actions": actions, "after": {}, "after_unknown": {}}}

file(path, rcs) := {"path": path, "contents": {"plan_format_version": "1.2", "resource_changes": rcs}}

shape_errors(d) := errs if {
	errs := lib.instance_shape_errors with data.policy_plan as d with input as []
}

test_valid_declaration_has_no_shape_errors if {
	count(shape_errors(inst_decl)) == 0
	count(shape_errors(object.union(inst_decl, {"state_backend": object.remove(inst_decl.state_backend, ["instances"])}))) == 0
}

test_malformed_modules if {
	every v in [null, "", "module.s", "module.S.", "aws_s3_bucket.", 5] {
		d := json.patch(inst_decl, [{"op": "replace", "path": "/state_backend/module", "value": v}])
		"data.policy_plan.state_backend.module が、module の address の接頭辞（module.<名前>. の並び）ではありません" in shape_errors(d)
	}
	d := json.patch(inst_decl, [{"op": "remove", "path": "/deployment_identity/module"}])
	"data.policy_plan.deployment_identity.module が、module の address の接頭辞（module.<名前>. の並び）ではありません" in shape_errors(d)
}

test_overlapping_modules if {
	d := json.patch(inst_decl, [
		{"op": "replace", "path": "/deployment_identity/module", "value": "module.s.module.d."},
		{"op": "replace", "path": "/deployment_identity/apply_role/bootstrap/modules", "value": ["module.s.", "module.s.module.d."]},
	])
	"data.policy_plan.state_backend.module \"module.s.\" と deployment_identity.module \"module.s.module.d.\" が重なっています（同じか、一方が他方の接頭辞です）" in shape_errors(d)
}

test_modules_absent_from_bootstrap if {
	d := json.patch(inst_decl, [{"op": "replace", "path": "/deployment_identity/apply_role/bootstrap/modules", "value": ["module.s."]}])
	"data.policy_plan.deployment_identity.module \"module.d.\" が deployment_identity.apply_role.bootstrap.modules にありません" in shape_errors(d)
	d2 := json.patch(inst_decl, [{"op": "remove", "path": "/deployment_identity/apply_role"}])
	"data.policy_plan.state_backend.module \"module.s.\" が deployment_identity.apply_role.bootstrap.modules にありません" in shape_errors(d2)
}

test_instances_not_object_or_empty if {
	"data.policy_plan.state_backend.instances が object ではありません" in shape_errors(inst_decl_with([inst_security]))
	"data.policy_plan.state_backend.instances が空の object です。instance が無いなら、キーごと外してください" in shape_errors(inst_decl_with({}))
	every v in [[], {}, "security", null] {
		"data.policy_plan.state_backend.instances[\"admin-identity\"] が空でない配列ではありません" in shape_errors(inst_decl_with({"admin-identity": v}))
	}
}

test_malformed_instance_elements if {
	every e in [
		"security",
		object.remove(inst_security, ["name"]),
		object.union(inst_security, {"name": ""}),
		object.union(inst_security, {"state_backend": "module.sm"}),
		object.union(inst_security, {"deployment_identity": 5}),
		object.union(inst_security, {"extra": true}),
		json.patch(inst_security, [{"op": "replace", "path": "/creating", "value": {}}]),
		object.union(inst_security, {"creating": {"reason": " "}}),
		object.union(inst_security, {"creating": {"reason": "x", "until": "x"}}),
		object.union(inst_security, {"creating": "作成の plan"}),
		object.union(inst_security, {"deployment_identity_root": ""}),
		object.union(inst_security, {"deployment_identity_root": 5}),
	] {
		some m in shape_errors(inst_decl_with({"admin-identity": [e]}))
		startswith(m, sprintf("data.policy_plan.state_backend.instances[\"admin-identity\"] の要素 %v が", [e]))
	}
	count(shape_errors(inst_decl_with({"admin-identity": [object.remove(inst_security, ["creating"])]}))) == 0
	count(shape_errors(inst_decl_with({"state-backend": [object.union(inst_security, {"deployment_identity_root": "deployment-identity"})]}))) == 0
}

test_overlapping_instance_prefixes if {
	every e in [
		object.union(inst_security, {"state_backend": "module.s."}),
		object.union(inst_security, {"deployment_identity": "module.d.module.x."}),
		object.union(inst_security, {"deployment_identity": "module.sm."}),
	] {
		some m in shape_errors(inst_decl_with({"admin-identity": [e]}))
		startswith(m, "data.policy_plan.state_backend.instances[\"admin-identity\"] の ")
		contains(m, "が重なっています")
	}
	two := [inst_security, object.union(inst_security, {"name": "log-archive"})]
	some m in shape_errors(inst_decl_with({"admin-identity": two}))
	contains(m, "が重なっています")
}

test_duplicate_instance_names if {
	two := [inst_security, object.union(inst_security, {"state_backend": "module.sm2.", "deployment_identity": "module.dm2."})]
	"data.policy_plan.state_backend.instances[\"admin-identity\"] の name [\"security\", \"security\"] が重なっています" in shape_errors(inst_decl_with({"admin-identity": two}))
}

test_root_name_is_the_basename_without_json if {
	lib.root_name("admin-identity.json") == "admin-identity"
	lib.root_name("input/admin-identity.json") == "admin-identity"
	lib.root_name("/app/tmp/run/inputs/state-backend.json") == "state-backend"
}

canonical := {"name": "canonical", "root": null, "deployment_identity_root": null, "state_backend": "module.s.", "deployment_identity": "module.d.", "creating": null}

member := {"name": "security", "root": "admin-identity", "deployment_identity_root": "admin-identity", "state_backend": "module.sm.", "deployment_identity": "module.dm.", "creating": {"reason": "作成の plan"}}

test_instances_count_declared_roots_present_in_the_input if {
	lib.instances == {canonical} with data.policy_plan as inst_decl with input as [file("state-backend.json", [])]
	lib.instances == {canonical, member} with data.policy_plan as inst_decl with input as [file("input/admin-identity.json", [])]
}

test_malformed_instance_is_not_counted if {
	d := inst_decl_with({"admin-identity": [object.union(inst_security, {"state_backend": "x"})]})
	lib.instances == {canonical} with data.policy_plan as d with input as [file("admin-identity.json", [])]
}

test_rebase_by_module_prefix if {
	lib.rebase("module.s.module.kms.aws_kms_key.this", member) == "module.sm.module.kms.aws_kms_key.this" with data.policy_plan as inst_decl
	lib.rebase("module.d.module.github_oidc.aws_iam_role.this[\"plan\"]", member) == "module.dm.module.github_oidc.aws_iam_role.this[\"plan\"]" with data.policy_plan as inst_decl
	lib.rebase("module.s.module.kms.aws_kms_key.this", canonical) == "module.s.module.kms.aws_kms_key.this" with data.policy_plan as inst_decl
	not lib.rebase("module.x.aws_kms_key.this", member) with data.policy_plan as inst_decl
	not lib.rebasable("module.x.aws_kms_key.this") with data.policy_plan as inst_decl
	not lib.rebasable(5) with data.policy_plan as inst_decl
}

test_present_reads_only_the_root_file_of_the_instance if {
	inp := [file("state-backend.json", [rc("module.sm.module.kms.aws_kms_key.this", ["create"])]), file("admin-identity.json", [])]
	lib.present("module.sm.module.kms.aws_kms_key.this", canonical) with data.policy_plan as inst_decl with input as inp
	not lib.present("module.sm.module.kms.aws_kms_key.this", member) with data.policy_plan as inst_decl with input as inp
}

marker_sb(prefix) := rc(sprintf("%smodule.plan_output.aws_s3_bucket.this", [prefix]), ["no-op"])

marker_di(prefix) := rc(sprintf("%smodule.github_oidc.aws_iam_role.apply_identity", [prefix]), ["no-op"])

test_undeclared_instances_by_markers if {
	inp := [
		file("admin-identity.json", [marker_sb("module.s."), marker_sb("module.sm."), marker_di("module.dm."), marker_sb("module.sx."), marker_di("module.dx.")]),
		file("state-backend.json", [marker_sb("module.sm.")]),
		file("other.json", [marker_sb("")]),
	]
	got := lib.undeclared_instances with data.policy_plan as inst_decl with input as inp
	got == {
		{"path": "admin-identity.json", "kind": "state_backend", "prefix": "module.sx."},
		{"path": "admin-identity.json", "kind": "deployment_identity", "prefix": "module.dx."},
		{"path": "state-backend.json", "kind": "state_backend", "prefix": "module.sm."},
		{"path": "other.json", "kind": "state_backend", "prefix": ""},
	}
}

test_undeclared_instances_none_for_declared_markers if {
	inp := [file("admin-identity.json", [marker_sb("module.s."), marker_di("module.d."), marker_sb("module.sm."), marker_di("module.dm.")])]
	count(lib.undeclared_instances) == 0 with data.policy_plan as inst_decl with input as inp
}

test_creating_only_while_the_state_key_is_created if {
	key := "module.sm.module.kms.aws_kms_key.this"
	lib.creating(member) with data.policy_plan as inst_decl with input as [file("admin-identity.json", [rc(key, ["create"])])]
	not lib.creating_unused(member) with data.policy_plan as inst_decl with input as [file("admin-identity.json", [rc(key, ["create"])])]
	every actions in [["no-op"], ["update"], ["delete", "create"]] {
		not lib.creating(member) with data.policy_plan as inst_decl with input as [file("admin-identity.json", [rc(key, actions)])]
		lib.creating_unused(member) with data.policy_plan as inst_decl with input as [file("admin-identity.json", [rc(key, actions)])]
	}
	lib.creating_unused(member) with data.policy_plan as inst_decl with input as [file("admin-identity.json", [])]
	not lib.creating(object.union(member, {"creating": null})) with data.policy_plan as inst_decl with input as [file("admin-identity.json", [rc(key, ["create"])])]
	not lib.creating_unused(canonical) with data.policy_plan as inst_decl with input as [file("admin-identity.json", [])]
}

test_instance_tag if {
	lib.instance_tag(canonical) == ""
	lib.instance_tag(member) == "admin-identity の instance security: "
}

test_instance_markers_derived_from_declarations if {
	got := lib.instance_markers with data.policy_plan as inst_decl
	got == {"state_backend": "module.plan_output.aws_s3_bucket.this", "deployment_identity": "module.github_oidc.aws_iam_role.apply_identity"}
}

# usecase の module の address が変われば、印も宣言と一緒に変わる。
test_instance_markers_follow_renamed_modules if {
	d := json.patch(inst_decl, [
		{"op": "replace", "path": "/deployment_identity/apply_identity_role/output_bucket", "value": "module.s.module.plan_out.aws_s3_bucket.this"},
		{"op": "replace", "path": "/deployment_identity/apply_identity_role/role", "value": "module.d.module.oidc.aws_iam_role.apply_identity"},
	])
	inp := [file("admin-identity.json", [
		rc("module.sx.module.plan_out.aws_s3_bucket.this", ["no-op"]),
		rc("module.dx.module.oidc.aws_iam_role.apply_identity", ["no-op"]),
		marker_sb("module.sy."),
	])]
	got := lib.undeclared_instances with data.policy_plan as d with input as inp
	got == {
		{"path": "admin-identity.json", "kind": "state_backend", "prefix": "module.sx."},
		{"path": "admin-identity.json", "kind": "deployment_identity", "prefix": "module.dx."},
	}
}

test_instance_markers_underivable if {
	every c in [
		{"patch": {"op": "remove", "path": "/deployment_identity/apply_identity_role/output_bucket"}, "kind": "state_backend", "path": "output_bucket"},
		{"patch": {"op": "replace", "path": "/deployment_identity/apply_identity_role/output_bucket", "value": "module.x.aws_s3_bucket.this"}, "kind": "state_backend", "path": "output_bucket"},
		{"patch": {"op": "replace", "path": "/deployment_identity/apply_identity_role/output_bucket", "value": "module.s."}, "kind": "state_backend", "path": "output_bucket"},
		{"patch": {"op": "remove", "path": "/deployment_identity/apply_identity_role/role"}, "kind": "deployment_identity", "path": "role"},
		{"patch": {"op": "replace", "path": "/deployment_identity/apply_identity_role/role", "value": 5}, "kind": "deployment_identity", "path": "role"},
	] {
		msg := sprintf("data.policy_plan.deployment_identity.apply_identity_role.%s が %s.module で始まる address ではありません。宣言に無い instance を見つける印を導けません", [c.path, c.kind])
		msg in shape_errors(json.patch(inst_decl, [c.patch]))
	}
}

# 削除・管理からの除外・data source の読み取りは instance を書かないので数えない。書く変更（no-op を含む）は数える。
gone(address, actions) := {"address": address, "type": "aws_s3_bucket", "change": {"actions": actions, "before": {}, "after": null}}

test_undeclared_instances_skip_non_writing_entries if {
	inp := [file("admin-identity.json", [
		gone("module.sx.module.plan_output.aws_s3_bucket.this", ["forget"]),
		gone("module.dx.module.github_oidc.aws_iam_role.apply_identity", ["delete"]),
		object.union(marker_sb("module.sy."), {"mode": "data"}),
		rc("module.sz.module.plan_output.aws_s3_bucket.this", ["update"]),
	])]
	got := lib.undeclared_instances with data.policy_plan as inst_decl with input as inp
	got == {{"path": "admin-identity.json", "kind": "state_backend", "prefix": "module.sz."}}
}

test_malformed_marker_entries if {
	inp := [file("admin-identity.json", [
		{"address": "module.sx.module.plan_output.aws_s3_bucket.this", "type": "aws_s3_bucket"},
		{"address": "module.dx.module.github_oidc.aws_iam_role.apply_identity", "type": "aws_iam_role", "change": "create"},
		{"address": "module.sy.module.plan_output.aws_s3_bucket.this", "type": "aws_s3_bucket", "change": {"actions": ["delete"]}},
		{"address": "module.sz.module.other.aws_s3_bucket.this", "type": "aws_s3_bucket"},
	])]
	lib.malformed_marker_entries == {
		{"path": "admin-identity.json", "address": "module.sx.module.plan_output.aws_s3_bucket.this"},
		{"path": "admin-identity.json", "address": "module.dx.module.github_oidc.aws_iam_role.apply_identity"},
	} with data.policy_plan as inst_decl with input as inp
	count(lib.undeclared_instances) == 0 with data.policy_plan as inst_decl with input as inp
}

# reference では、state-backend の側と deployment-identity の側が別の root のファイルに在る。
cross := object.union(object.remove(inst_security, ["creating"]), {"deployment_identity_root": "deployment-identity"})

cross_decl := inst_decl_with({"state-backend": [cross]})

cross_member := {"name": "security", "root": "state-backend", "deployment_identity_root": "deployment-identity", "state_backend": "module.sm.", "deployment_identity": "module.dm.", "creating": null}

test_cross_root_instance_counted_only_with_both_roots if {
	lib.instances == {canonical, cross_member} with data.policy_plan as cross_decl with input as [file("state-backend.json", []), file("deployment-identity.json", [])]
	lib.instances == {canonical} with data.policy_plan as cross_decl with input as [file("state-backend.json", [])]
	lib.instances == {canonical} with data.policy_plan as cross_decl with input as [file("deployment-identity.json", [])]
}

test_cross_root_instance_reads_both_root_files if {
	inp := [
		file("state-backend.json", [rc("module.sm.module.kms.aws_kms_key.this", ["create"])]),
		file("deployment-identity.json", [rc("module.dm.module.github_oidc.aws_iam_role.apply_identity", ["create"])]),
		file("other.json", [rc("module.sm.module.x.aws_kms_key.this", ["create"])]),
	]
	lib.present("module.sm.module.kms.aws_kms_key.this", cross_member) with data.policy_plan as cross_decl with input as inp
	lib.present("module.dm.module.github_oidc.aws_iam_role.apply_identity", cross_member) with data.policy_plan as cross_decl with input as inp
	not lib.present("module.sm.module.x.aws_kms_key.this", cross_member) with data.policy_plan as cross_decl with input as inp
}

test_cross_root_markers_known_only_in_their_own_root if {
	inp := [
		file("state-backend.json", [marker_sb("module.sm."), marker_di("module.dm.")]),
		file("deployment-identity.json", [marker_di("module.dm."), marker_sb("module.sm.")]),
	]
	got := lib.undeclared_instances with data.policy_plan as cross_decl with input as inp
	got == {
		{"path": "state-backend.json", "kind": "deployment_identity", "prefix": "module.dm."},
		{"path": "deployment-identity.json", "kind": "state_backend", "prefix": "module.sm."},
	}
}

test_member_decl_drops_management_only_policies_and_instances if {
	d := object.union(inst_decl, {"deployment_identity": {
		"management_only_policies": ["module.d.aws_iam_policy.governance[0]"],
		"apply_identity_role": {"managed_policies": ["module.d.aws_iam_policy.manage", "module.d.aws_iam_policy.governance[0]"]},
		"customer_managed_policies": ["module.d.aws_iam_policy.manage", "module.d.aws_iam_policy.governance"],
	}})
	got := lib.member_decl with data.policy_plan as d
	got.deployment_identity.apply_identity_role.managed_policies == ["module.d.aws_iam_policy.manage"]
	got.deployment_identity.apply_identity_role.role == inst_decl.deployment_identity.apply_identity_role.role
	got.deployment_identity.customer_managed_policies == ["module.d.aws_iam_policy.manage"]
	not "instances" in object.keys(got.state_backend)
	got.state_backend.state_key == inst_decl.state_backend.state_key
}

test_member_view_rebases_the_instance_and_keeps_the_rest if {
	inp := [file("sb.json", [rc("module.sm.module.kms.aws_kms_key.this", ["create"]), rc("module.s.module.kms.aws_kms_key.this", ["create"])]), file("di.json", [rc("module.dm.x.this", ["create"]), rc("module.d.x.this", ["create"]), rc("module.other.x.this", ["create"])])]
	got := lib.member_view(cross_member) with data.policy_plan as cross_decl with input as inp
	[[rc.address | some rc in f.contents.resource_changes] | some f in got] == [["module.s.module.kms.aws_kms_key.this"], ["module.d.x.this", "module.other.x.this"]]
	[f.path | some f in got] == ["sb.json", "di.json"]
}

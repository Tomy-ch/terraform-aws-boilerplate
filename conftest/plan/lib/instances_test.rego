package plan.lib.instances_test

import data.plan.lib

security := {"name": "security", "state_backend": "module.sm.", "deployment_identity": "module.dm.", "creating": {"reason": "作成の plan"}}

decl_with(instances) := {
	"deployment_identity": {"module": "module.d.", "apply_role": {"bootstrap": {"modules": ["module.s.", "module.d."]}}},
	"state_backend": {"module": "module.s.", "state_key": "module.s.module.kms.aws_kms_key.this", "instances": instances},
}

decl := decl_with({"admin-identity": [security]})

rc(address, actions) := {"address": address, "type": "aws_kms_key", "change": {"actions": actions, "after": {}, "after_unknown": {}}}

file(path, rcs) := {"path": path, "contents": {"plan_format_version": "1.2", "resource_changes": rcs}}

shape_errors(d) := errs if {
	errs := lib.instance_shape_errors with data.policy_plan as d with input as []
}

test_valid_declaration_has_no_shape_errors if {
	count(shape_errors(decl)) == 0
	count(shape_errors(object.union(decl, {"state_backend": object.remove(decl.state_backend, ["instances"])}))) == 0
}

test_malformed_modules if {
	every v in [null, "", "module.s", "module.S.", "aws_s3_bucket.", 5] {
		d := json.patch(decl, [{"op": "replace", "path": "/state_backend/module", "value": v}])
		"data.policy_plan.state_backend.module が、module の address の接頭辞（module.<名前>. の並び）ではありません" in shape_errors(d)
	}
	d := json.patch(decl, [{"op": "remove", "path": "/deployment_identity/module"}])
	"data.policy_plan.deployment_identity.module が、module の address の接頭辞（module.<名前>. の並び）ではありません" in shape_errors(d)
}

test_overlapping_modules if {
	d := json.patch(decl, [
		{"op": "replace", "path": "/deployment_identity/module", "value": "module.s.module.d."},
		{"op": "replace", "path": "/deployment_identity/apply_role/bootstrap/modules", "value": ["module.s.", "module.s.module.d."]},
	])
	"data.policy_plan.state_backend.module \"module.s.\" と deployment_identity.module \"module.s.module.d.\" が重なっています（同じか、一方が他方の接頭辞です）" in shape_errors(d)
}

test_modules_absent_from_bootstrap if {
	d := json.patch(decl, [{"op": "replace", "path": "/deployment_identity/apply_role/bootstrap/modules", "value": ["module.s."]}])
	"data.policy_plan.deployment_identity.module \"module.d.\" が deployment_identity.apply_role.bootstrap.modules にありません" in shape_errors(d)
	d2 := json.patch(decl, [{"op": "remove", "path": "/deployment_identity/apply_role"}])
	"data.policy_plan.state_backend.module \"module.s.\" が deployment_identity.apply_role.bootstrap.modules にありません" in shape_errors(d2)
}

test_instances_not_object_or_empty if {
	"data.policy_plan.state_backend.instances が object ではありません" in shape_errors(decl_with([security]))
	"data.policy_plan.state_backend.instances が空の object です。instance が無いなら、キーごと外してください" in shape_errors(decl_with({}))
	every v in [[], {}, "security", null] {
		"data.policy_plan.state_backend.instances[\"admin-identity\"] が空でない配列ではありません" in shape_errors(decl_with({"admin-identity": v}))
	}
}

test_malformed_instance_elements if {
	every e in [
		"security",
		object.remove(security, ["name"]),
		object.union(security, {"name": ""}),
		object.union(security, {"state_backend": "module.sm"}),
		object.union(security, {"deployment_identity": 5}),
		object.union(security, {"extra": true}),
		json.patch(security, [{"op": "replace", "path": "/creating", "value": {}}]),
		object.union(security, {"creating": {"reason": " "}}),
		object.union(security, {"creating": {"reason": "x", "until": "x"}}),
		object.union(security, {"creating": "作成の plan"}),
	] {
		some m in shape_errors(decl_with({"admin-identity": [e]}))
		startswith(m, sprintf("data.policy_plan.state_backend.instances[\"admin-identity\"] の要素 %v が", [e]))
	}
	count(shape_errors(decl_with({"admin-identity": [object.remove(security, ["creating"])]}))) == 0
}

test_overlapping_instance_prefixes if {
	every e in [
		object.union(security, {"state_backend": "module.s."}),
		object.union(security, {"deployment_identity": "module.d.module.x."}),
		object.union(security, {"deployment_identity": "module.sm."}),
	] {
		some m in shape_errors(decl_with({"admin-identity": [e]}))
		startswith(m, "data.policy_plan.state_backend.instances[\"admin-identity\"] の ")
		contains(m, "が重なっています")
	}
	two := [security, object.union(security, {"name": "log-archive"})]
	some m in shape_errors(decl_with({"admin-identity": two}))
	contains(m, "が重なっています")
}

test_duplicate_instance_names if {
	two := [security, object.union(security, {"state_backend": "module.sm2.", "deployment_identity": "module.dm2."})]
	"data.policy_plan.state_backend.instances[\"admin-identity\"] の name [\"security\", \"security\"] が重なっています" in shape_errors(decl_with({"admin-identity": two}))
}

test_root_name_is_the_basename_without_json if {
	lib.root_name("admin-identity.json") == "admin-identity"
	lib.root_name("input/admin-identity.json") == "admin-identity"
	lib.root_name("/app/tmp/run/inputs/state-backend.json") == "state-backend"
}

canonical := {"name": "canonical", "root": null, "state_backend": "module.s.", "deployment_identity": "module.d.", "creating": null}

member := {"name": "security", "root": "admin-identity", "state_backend": "module.sm.", "deployment_identity": "module.dm.", "creating": {"reason": "作成の plan"}}

test_instances_count_declared_roots_present_in_the_input if {
	lib.instances == {canonical} with data.policy_plan as decl with input as [file("state-backend.json", [])]
	lib.instances == {canonical, member} with data.policy_plan as decl with input as [file("input/admin-identity.json", [])]
}

test_malformed_instance_is_not_counted if {
	d := decl_with({"admin-identity": [object.union(security, {"state_backend": "x"})]})
	lib.instances == {canonical} with data.policy_plan as d with input as [file("admin-identity.json", [])]
}

test_rebase_by_module_prefix if {
	lib.rebase("module.s.module.kms.aws_kms_key.this", member) == "module.sm.module.kms.aws_kms_key.this" with data.policy_plan as decl
	lib.rebase("module.d.module.github_oidc.aws_iam_role.this[\"plan\"]", member) == "module.dm.module.github_oidc.aws_iam_role.this[\"plan\"]" with data.policy_plan as decl
	lib.rebase("module.s.module.kms.aws_kms_key.this", canonical) == "module.s.module.kms.aws_kms_key.this" with data.policy_plan as decl
	not lib.rebase("module.x.aws_kms_key.this", member) with data.policy_plan as decl
	not lib.rebasable("module.x.aws_kms_key.this") with data.policy_plan as decl
	not lib.rebasable(5) with data.policy_plan as decl
}

test_present_reads_only_the_root_file_of_the_instance if {
	inp := [file("state-backend.json", [rc("module.sm.module.kms.aws_kms_key.this", ["create"])]), file("admin-identity.json", [])]
	lib.present("module.sm.module.kms.aws_kms_key.this", canonical) with data.policy_plan as decl with input as inp
	not lib.present("module.sm.module.kms.aws_kms_key.this", member) with data.policy_plan as decl with input as inp
}

marker_sb(prefix) := rc(sprintf("%smodule.plan_output.aws_s3_bucket.this", [prefix]), ["no-op"])

marker_di(prefix) := rc(sprintf("%smodule.github_oidc.aws_iam_role.apply_identity", [prefix]), ["no-op"])

test_undeclared_instances_by_markers if {
	inp := [
		file("admin-identity.json", [marker_sb("module.s."), marker_sb("module.sm."), marker_di("module.dm."), marker_sb("module.sx."), marker_di("module.dx.")]),
		file("state-backend.json", [marker_sb("module.sm.")]),
		file("other.json", [marker_sb("")]),
	]
	got := lib.undeclared_instances with data.policy_plan as decl with input as inp
	got == {
		{"path": "admin-identity.json", "kind": "state_backend", "prefix": "module.sx."},
		{"path": "admin-identity.json", "kind": "deployment_identity", "prefix": "module.dx."},
		{"path": "state-backend.json", "kind": "state_backend", "prefix": "module.sm."},
		{"path": "other.json", "kind": "state_backend", "prefix": ""},
	}
}

test_undeclared_instances_none_for_declared_markers if {
	inp := [file("admin-identity.json", [marker_sb("module.s."), marker_di("module.d."), marker_sb("module.sm."), marker_di("module.dm.")])]
	count(lib.undeclared_instances) == 0 with data.policy_plan as decl with input as inp
}

test_creating_only_while_the_state_key_is_created if {
	key := "module.sm.module.kms.aws_kms_key.this"
	lib.creating(member) with data.policy_plan as decl with input as [file("admin-identity.json", [rc(key, ["create"])])]
	not lib.creating_unused(member) with data.policy_plan as decl with input as [file("admin-identity.json", [rc(key, ["create"])])]
	every actions in [["no-op"], ["update"], ["delete", "create"]] {
		not lib.creating(member) with data.policy_plan as decl with input as [file("admin-identity.json", [rc(key, actions)])]
		lib.creating_unused(member) with data.policy_plan as decl with input as [file("admin-identity.json", [rc(key, actions)])]
	}
	lib.creating_unused(member) with data.policy_plan as decl with input as [file("admin-identity.json", [])]
	not lib.creating(object.union(member, {"creating": null})) with data.policy_plan as decl with input as [file("admin-identity.json", [rc(key, ["create"])])]
	not lib.creating_unused(canonical) with data.policy_plan as decl with input as [file("admin-identity.json", [])]
}

test_instance_tag if {
	lib.instance_tag(canonical) == ""
	lib.instance_tag(member) == "admin-identity の instance security: "
}

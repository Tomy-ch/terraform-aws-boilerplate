package layer

# 最小の木（env → usecase → 一般 module。modules/a/main.tf の aws_s3_bucket_policy が aws_s3_bucket を親に持つ）に
# 1つの変更を足した fixture を、hcl-input の出力として層の検査に掛ける。宣言と schema は tree_decl・tree_schema。

tree_decl := {
	"attachments": {
		"aws_s3_bucket_policy": {
			"parents": {
				"bucket": [
					"aws_s3_bucket"
				]
			},
			"optional": false,
			"parent_arguments": {
				"aws_s3_bucket": {
					"policy": null
				}
			},
			"whole_document": true
		},
		"aws_guardduty_malware_protection_plan": {
			"parents": {
				"protected_resource.s3_bucket.bucket_name": [
					"aws_s3_bucket"
				]
			},
			"optional": false,
			"parent_arguments": {},
			"whole_document": false
		},
		"aws_iam_role_policy": {
			"parents": {
				"role": [
					"aws_iam_role"
				]
			},
			"optional": false,
			"parent_arguments": {
				"aws_iam_role": {
					"inline_policy": []
				}
			},
			"whole_document": false
		},
		"aws_iam_role_policies_exclusive": {
			"parents": {
				"role_name": [
					"aws_iam_role"
				]
			},
			"optional": false,
			"parent_arguments": {
				"aws_iam_role": {
					"inline_policy": []
				}
			},
			"whole_document": true
		},
		"aws_iam_role_policy_attachment": {
			"parents": {
				"role": [
					"aws_iam_role"
				]
			},
			"optional": false,
			"parent_arguments": {
				"aws_iam_role": {
					"managed_policy_arns": null
				}
			},
			"whole_document": false
		},
		"aws_iam_role_policy_attachments_exclusive": {
			"parents": {
				"role_name": [
					"aws_iam_role"
				]
			},
			"optional": false,
			"parent_arguments": {
				"aws_iam_role": {
					"managed_policy_arns": null
				}
			},
			"whole_document": true
		},
		"aws_security_group_rule": {
			"parents": {
				"security_group_id": [
					"aws_security_group"
				]
			},
			"optional": false,
			"parent_arguments": {
				"aws_security_group": {
					"ingress": null,
					"egress": null
				}
			},
			"whole_document": false
		},
		"aws_kms_key_policy": {
			"parents": {
				"key_id": [
					"aws_kms_key"
				]
			},
			"optional": false,
			"parent_arguments": {
				"aws_kms_key": {
					"policy": null
				}
			},
			"whole_document": true
		},
		"aws_route": {
			"parents": {
				"route_table_id": [
					"aws_route_table"
				]
			},
			"optional": false,
			"parent_arguments": {
				"aws_route_table": {
					"route": null
				}
			},
			"whole_document": false
		}
	},
	"not_attachments": {
		"aws_lb_listener": "子 resource",
		"aws_s3_bucket": "親になる resource",
		"aws_iam_role": "親になる resource",
		"aws_security_group": "親になる resource",
		"aws_default_security_group": "既定の SG",
		"aws_kms_key": "親になる resource",
		"aws_route_table": "親になる resource",
		"aws_s3_account_public_access_block": "アカウントの単位で設定する"
	},
	"singletons": {
		"aws_s3_account_public_access_block": [
			"account-baseline"
		]
	},
	"unrelated_deprecated_arguments": {
		"aws_s3_bucket": {
			"acl": {
				"reason": "fixture",
				"unwritten": null
			}
		}
	},
	"inline_owned": {
		"aws_route_table": {
			"route": "経路を排他に持つ"
		}
	}
}

tree_schema := {
	"aws_s3_account_public_access_block": {
		"arguments": [
			"block_public_acls"
		],
		"deprecated": [],
		"blocks": []
	},
	"aws_s3_bucket": {
		"arguments": [
			"bucket",
			"policy",
			"acl"
		],
		"deprecated": [
			"policy",
			"acl"
		],
		"blocks": []
	},
	"aws_iam_role": {
		"arguments": [
			"name",
			"inline_policy",
			"managed_policy_arns"
		],
		"deprecated": [
			"inline_policy",
			"managed_policy_arns"
		],
		"blocks": [
			"inline_policy"
		]
	},
	"aws_security_group": {
		"arguments": [
			"name",
			"ingress",
			"egress"
		],
		"deprecated": [],
		"blocks": []
	},
	"aws_kms_key": {
		"arguments": [
			"description",
			"policy"
		],
		"deprecated": [],
		"blocks": []
	},
	"aws_route_table": {
		"arguments": [
			"vpc_id",
			"route"
		],
		"deprecated": [],
		"blocks": []
	}
}

tree_schema_website := {
	"aws_s3_account_public_access_block": {
		"arguments": [
			"block_public_acls"
		],
		"deprecated": [],
		"blocks": []
	},
	"aws_s3_bucket": {
		"arguments": [
			"bucket",
			"policy",
			"acl",
			"website"
		],
		"deprecated": [
			"policy",
			"acl",
			"website"
		],
		"blocks": [
			"website"
		]
	},
	"aws_iam_role": {
		"arguments": [
			"name",
			"inline_policy",
			"managed_policy_arns"
		],
		"deprecated": [
			"inline_policy",
			"managed_policy_arns"
		],
		"blocks": [
			"inline_policy"
		]
	},
	"aws_security_group": {
		"arguments": [
			"ingress",
			"egress"
		],
		"deprecated": [],
		"blocks": []
	},
	"aws_kms_key": {
		"arguments": [
			"policy"
		],
		"deprecated": [],
		"blocks": []
	},
	"aws_route_table": {
		"arguments": [
			"route"
		],
		"deprecated": [],
		"blocks": []
	}
}

tree_denies(name, d, sch) := result if {
	doc := fx(name)
	result := deny with input as doc
		with data.layer_decl as d
		with data.layer_files as listed_of(doc)
		with data.hcl_parsed_files as parsed_of(doc)
		with data.hcl_block_counts as counts_of(doc)
		with data.layer_schema as sch
}

# tree_fails は、deny がちょうど1つで、その文言が pattern を含むかを返す。
tree_fails(name, d, sch, pattern) if only_with(tree_denies(name, d, sch), pattern)

test_LY301_deny_tree_escape1_quote_pair if {
	tree_fails("tree_escape1_quote_pair", tree_decl, tree_schema, "LY301 [親の同居] modules/a/main.tf の aws_s3_bucket_policy.this の bucket が、同じ module の親（aws_s3_bucket）だけを参照していません: module.m, var.z（")
}

test_LY301_deny_tree_escape2_brace_in_interpolation if {
	tree_fails("tree_escape2_brace_in_interpolation", tree_decl, tree_schema, "LY301 [親の同居] modules/a/main.tf の aws_s3_bucket_policy.this の bucket が、同じ module の親（aws_s3_bucket）だけを参照していません: var.x, var.y（")
}

test_LY301_deny_tree_escape3_template_directive if {
	tree_fails("tree_escape3_template_directive", tree_decl, tree_schema, "LY301 [親の同居] modules/a/main.tf の aws_s3_bucket_policy.this の bucket が、同じ module の親（aws_s3_bucket）だけを参照していません: var.a（")
}

test_LY301_deny_tree_escape4_concatenated_dollar if {
	tree_fails("tree_escape4_concatenated_dollar", tree_decl, tree_schema, "LY301 [親の同居] modules/a/main.tf の aws_s3_bucket_policy.this の bucket が、同じ module の親（aws_s3_bucket）だけを参照していません: var.x（")
}

test_LY301_deny_tree_escape5_comment_in_expression if {
	tree_fails("tree_escape5_comment_in_expression", tree_decl, tree_schema, "LY301 [親の同居] modules/a/main.tf の aws_s3_bucket_policy.this の bucket が、同じ module の親（aws_s3_bucket）だけを参照していません: var.x（")
}

test_LY301_deny_tree_escape5_comment_in_expression_line if {
	tree_fails("tree_escape5_comment_in_expression_line", tree_decl, tree_schema, "LY301 [親の同居] modules/a/main.tf の aws_s3_bucket_policy.this の bucket が、同じ module の親（aws_s3_bucket）だけを参照していません: var.x（")
}

test_LY301_deny_tree_escape6_iterator_shadowing if {
	tree_fails("tree_escape6_iterator_shadowing", tree_decl, tree_schema, "LY301 [親の同居] modules/a/main.tf の aws_s3_bucket_policy.this の bucket が、同じ module の親（aws_s3_bucket）だけを参照していません: var.y（")
}

test_LY301_deny_tree_escape6_iterator_shadowing_resource_type if {
	tree_fails("tree_escape6_iterator_shadowing_resource_type", tree_decl, tree_schema, "LY301 [親の同居] modules/a/main.tf の aws_s3_bucket_policy.this の bucket が、同じ module の親（aws_s3_bucket）だけを参照していません: aws_iam_role.r（")
}

test_LY301_deny_tree_escaped_only if {
	tree_fails("tree_escaped_only", tree_decl, tree_schema, "LY301 [親の同居] modules/a/main.tf の aws_s3_bucket_policy.this の bucket が、同じ module の親（aws_s3_bucket）だけを参照していません: 参照がありません（")
}

test_LY301_deny_tree_deep_nesting if {
	tree_fails("tree_deep_nesting", tree_decl, tree_schema, "LY301 [親の同居] modules/a/main.tf の aws_s3_bucket_policy.this の bucket が、同じ module の親（aws_s3_bucket）だけを参照していません: module.x（")
}

test_tree_allow_cond_in_interpolation if {
	count(tree_denies("tree_cond_in_interpolation", tree_decl, tree_schema)) == 0
}

test_tree_allow_comment_in_expression if {
	count(tree_denies("tree_comment_in_expression", tree_decl, tree_schema)) == 0
}

test_tree_allow_template_directive_parent if {
	count(tree_denies("tree_template_directive_parent", tree_decl, tree_schema)) == 0
}

test_tree_allow_escaped_literal_then_parent if {
	count(tree_denies("tree_escaped_literal_then_parent", tree_decl, tree_schema)) == 0
}

test_tree_allow_index_after_reference if {
	count(tree_denies("tree_index_after_reference", tree_decl, tree_schema)) == 0
}

test_LY301_deny_tree_index_after_attribute if {
	tree_fails("tree_index_after_attribute", tree_decl, tree_schema, "LY301 [親の同居] modules/a/main.tf の aws_s3_bucket_policy.this の bucket が、同じ module の親（aws_s3_bucket）だけを参照していません: module.m（")
}

test_LY301_deny_tree_second_index_after_reference if {
	tree_fails("tree_second_index_after_reference", tree_decl, tree_schema, "LY301 [親の同居] modules/a/main.tf の aws_s3_bucket_policy.this の bucket が、同じ module の親（aws_s3_bucket）だけを参照していません: module.m（")
}

test_tree_allow_splat_parent if {
	count(tree_denies("tree_splat_parent", tree_decl, tree_schema)) == 0
}

test_LY304_deny_tree_inline_policy_block if {
	tree_fails("tree_inline_policy_block", tree_decl, tree_schema, "LY304 [親の inline の引数] modules/a/x.tf の aws_iam_role.r が inline_policy を書いています")
}

test_LY304_deny_tree_inline_policy_dynamic if {
	tree_fails("tree_inline_policy_dynamic", tree_decl, tree_schema, "LY304 [親の inline の引数] modules/a/x.tf の aws_iam_role.r が inline_policy を書いています")
}

test_LY304_deny_tree_managed_policy_arns_null if {
	tree_fails("tree_managed_policy_arns_null", tree_decl, tree_schema, "LY304 [親の inline の引数] modules/a/x.tf の aws_iam_role.r が managed_policy_arns を書いています")
}

test_LY304_deny_tree_sg_ingress_empty if {
	tree_fails("tree_sg_ingress_empty", tree_decl, tree_schema, "LY304 [親の inline の引数] modules/a/x.tf の aws_security_group.s が ingress を書いています")
}

test_LY304_deny_tree_kms_key_policy if {
	tree_fails("tree_kms_key_policy", tree_decl, tree_schema, "LY304 [親の inline の引数] modules/a/x.tf の aws_kms_key.k が policy を書いています")
}

test_tree_allow_default_sg_empty if {
	count(tree_denies("tree_default_sg_empty", tree_decl, tree_schema)) == 0
}

test_tree_allow_route_table_owned if {
	count(tree_denies("tree_route_table_owned", tree_decl, tree_schema)) == 0
}

test_LY307_deny_tree_route_to_owned if {
	tree_fails("tree_route_to_owned", tree_decl, tree_schema, "LY307 [排他の引数の親への attachment] modules/a/x.tf の aws_route.r")
}

test_LY305_deny_tree_bucket_policy_twice if {
	tree_fails("tree_bucket_policy_twice", tree_decl, tree_schema, "LY305 [文書全体の重複] modules/a/x.tf の aws_s3_bucket_policy.b と aws_s3_bucket_policy.this が、同じ親 aws_s3_bucket.this の文書全体を書きます")
}

test_tree_allow_bucket_policy_distinct if {
	count(tree_denies("tree_bucket_policy_distinct", tree_decl, tree_schema)) == 0
}

test_tree_allow_exclusive_with_inline_policy if {
	count(tree_denies("tree_exclusive_with_inline_policy", tree_decl, tree_schema)) == 0
}

test_tree_allow_exclusive_with_attachment if {
	count(tree_denies("tree_exclusive_with_attachment", tree_decl, tree_schema)) == 0
}

test_LY305_deny_tree_exclusive_twice if {
	tree_fails("tree_exclusive_twice", tree_decl, tree_schema, "LY305 [文書全体の重複] modules/a/x.tf の aws_iam_role_policy_attachments_exclusive.e と aws_iam_role_policy_attachments_exclusive.f")
}

test_tree_allow_literal_distinct if {
	count(tree_denies("tree_literal_distinct", tree_decl, tree_schema)) == 0
}

test_LY305_deny_tree_literal_same if {
	tree_fails("tree_literal_same", tree_decl, tree_schema, "LY305 [文書全体の重複] modules/a/x.tf の aws_s3_bucket_policy.n0 と aws_s3_bucket_policy.n1")
}

test_tree_allow_for_each_per_bucket if {
	count(tree_denies("tree_for_each_per_bucket", tree_decl, tree_schema)) == 0
}

test_tree_allow_for_each_each_value if {
	count(tree_denies("tree_for_each_each_value", tree_decl, tree_schema)) == 0
}

test_LY308_deny_tree_for_each_and_single if {
	tree_fails("tree_for_each_and_single", tree_decl, tree_schema, "LY308 [判定できない文書全体の重複]")
}

test_LY308_deny_tree_conditional_count if {
	tree_fails("tree_conditional_count", tree_decl, tree_schema, "LY308 [判定できない文書全体の重複]")
}

test_tree_allow_local_selected_in_one_block if {
	count(tree_denies("tree_local_selected_in_one_block", tree_decl, tree_schema)) == 0
}

test_tree_allow_ok if {
	count(tree_denies("tree_ok", tree_decl, tree_schema)) == 0
}

test_LY101_deny_tree_usecase_resource if {
	tree_fails("tree_usecase_resource", tree_decl, tree_schema, "LY101 [usecase の resource] usecases/u/extra.tf")
}

test_LY203_deny_tree_env_to_module if {
	tree_fails("tree_env_to_module", tree_decl, tree_schema, "LY203 [層が許さない先] env/dev/main.tf")
}

test_LY201_deny_tree_remote_source if {
	tree_fails("tree_remote_source", tree_decl, tree_schema, "LY201 [相対パスでない source] usecases/u/main.tf")
}

test_LY301_deny_tree_attachment_module_ref if {
	tree_fails("tree_attachment_module_ref", tree_decl, tree_schema, "LY301 [親の同居] modules/a/main.tf")
}

test_LY301_deny_tree_escape7_object_key if {
	tree_fails("tree_escape7_object_key", tree_decl, tree_schema, "LY301 [親の同居] modules/a/main.tf の aws_s3_bucket_policy.this の bucket が、同じ module の親（aws_s3_bucket）だけを参照していません: module.m（")
}

test_LY301_deny_tree_for_each_module if {
	tree_fails("tree_for_each_module", tree_decl, tree_schema, "LY301 [親の同居] modules/a/main.tf の aws_s3_bucket_policy.this の bucket が、同じ module の親（aws_s3_bucket）だけを参照していません: module.m（")
}

test_tree_allow_for_each_parent if {
	count(tree_denies("tree_for_each_parent", tree_decl, tree_schema)) == 0
}

test_tree_allow_nested_parent_ok if {
	count(tree_denies("tree_nested_parent_ok", tree_decl, tree_schema)) == 0
}

test_LY301_deny_tree_nested_parent_module if {
	tree_fails("tree_nested_parent_module", tree_decl, tree_schema, "LY301 [親の同居] modules/a/plan.tf の aws_guardduty_malware_protection_plan.this の protected_resource.s3_bucket.bucket_name が、同じ module の親（aws_s3_bucket）だけを参照していません: module.b（")
}

test_tree_allow_dynamic_parent_ok if {
	count(tree_denies("tree_dynamic_parent_ok", tree_decl, tree_schema)) == 0
}

test_LY301_deny_tree_dynamic_parent_module if {
	tree_fails("tree_dynamic_parent_module", tree_decl, tree_schema, "LY301 [親の同居] modules/a/plan.tf の aws_guardduty_malware_protection_plan.this の protected_resource.s3_bucket.bucket_name が、同じ module の親（aws_s3_bucket）だけを参照していません: module.b（")
}

test_LY306_deny_tree_unclassified_deprecated if {
	tree_fails("tree_unclassified_deprecated", tree_decl, tree_schema_website, "LY306 [分類されていない非推奨の引数] 親の型 aws_s3_bucket の非推奨の引数 website")
}

test_LY306_deny_tree_schema_empty if {
	tree_fails("tree_schema_empty", tree_decl, {}, "LY306 [schema の一覧]")
}

test_LY001_deny_tree_empty_decl if {
	some m in tree_denies("tree_empty_decl", {"attachments": {}, "not_attachments": {}}, tree_schema)
	contains(m, "LY001 [宣言の形] data.layer_decl.attachments が空です")
}

test_LY303_deny_tree_unclassified_type if {
	tree_fails("tree_unclassified_type", tree_decl, tree_schema, "LY303 [分類されていない型] resource の型 aws_sqs_queue")
}

test_tree_allow_index_literal if {
	count(tree_denies("tree_index_literal", tree_decl, tree_schema)) == 0
}

test_LY002_deny_tree_no_tf if {
	tree_fails("tree_no_tf", tree_decl, tree_schema, "LY002 [対象が0件]")
}

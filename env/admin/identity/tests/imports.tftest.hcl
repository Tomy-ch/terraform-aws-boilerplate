# import block の取り込み先と id は見ない（usecases/state-backend/README.md「検証方法」の段2 の突合）。
# mock の provider からは import できないので、取り込み先をすべて override する。
mock_provider "aws" {
  mock_data "aws_caller_identity" {
    defaults = { account_id = "123456789012" }
  }
  mock_data "aws_region" {
    defaults = { region = "ap-northeast-1" }
  }
  mock_data "aws_partition" {
    defaults = { partition = "aws" }
  }
}

# state の鍵の Key Policy の precondition（同じアカウントの role）を plan で評価させるため、role の ARN を既知の値にする。
override_resource {
  target          = module.deployment_identity.module.github_oidc.aws_iam_role.this["plan"]
  override_during = plan
  values          = { arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan" }
}

override_resource {
  target          = module.deployment_identity.module.github_oidc.aws_iam_role.apply_identity
  override_during = plan
  values          = { arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity" }
}

override_resource {
  target          = module.deployment_identity.module.github_oidc.aws_iam_role.this["unlock_identity"]
  override_during = plan
  values          = { arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock-identity" }
}

# deployment_identity の state_bucket_arn と plan_output_bucket_arn の validation を通すため、Bucket の ARN だけ形のある値にする。
override_resource {
  target = module.state_backend.module.s3.aws_s3_bucket.this
  values = { arn = "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an" }
}

override_resource {
  target = module.state_backend.module.plan_output.aws_s3_bucket.this
  values = { arn = "arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an" }
}

override_resource {
  target = module.state_backend.module.s3.aws_s3_bucket_public_access_block.this
}

override_resource {
  target = module.state_backend.module.s3.aws_s3_bucket_ownership_controls.this
}

override_resource {
  target = module.state_backend.module.s3.aws_s3_bucket_versioning.this
}

run "admin_state_bucket_name" {
  command = plan

  assert {
    condition     = local.state_bucket_name == "tabp-admin-tfstate-123456789012-ap-northeast-1-an"
    error_message = "管理アカウントの state Bucket の名前が tabp-admin-tfstate-<account_id>-<region>-an になっていません。"
  }

  # CI の plan が init の後に読む output（.makefiles/terraform/operation/ci.mk）。
  assert {
    condition     = output.plan_output_bucket_name == "tabp-admin-tfplan-123456789012-ap-northeast-1-an"
    error_message = "output plan_output_bucket_name が管理アカウントの plan の出力の Bucket の名前を返していません。"
  }
}

run "admin_state_key_alias_name" {
  command = plan

  assert {
    condition     = module.state_backend.state_key_alias_name == "alias/tabp-admin-tfstate"
    error_message = "管理アカウントの state の鍵の alias が alias/tabp-admin-tfstate になっていません。"
  }
}

# bootstrap の手順の simulate が名指す名前。手順とこのテストがそれぞれ同じ値を直書きする。
run "admin_access_log_names" {
  command = plan

  assert {
    condition = [
      module.state_backend.access_log_group_arn,
      module.state_backend.access_log_destination_arn,
      ] == [
      "arn:aws:logs:ap-northeast-1:123456789012:log-group:tabp-admin-state-backend-access",
      "arn:aws:logs:ap-northeast-1:123456789012:delivery-destination:tabp-admin-state-backend-access",
    ]
    error_message = "管理アカウントのアクセスログの Log Group・delivery destination が tabp-admin-state-backend-access になっていません。"
  }

  assert {
    condition = module.state_backend.access_log_source_arns == [
      "arn:aws:logs:ap-northeast-1:123456789012:delivery-source:tabp-admin-tfstate",
      "arn:aws:logs:ap-northeast-1:123456789012:delivery-source:tabp-admin-tfplan",
    ]
    error_message = "管理アカウントの delivery source が tabp-admin-tfstate と tabp-admin-tfplan になっていません。"
  }
}

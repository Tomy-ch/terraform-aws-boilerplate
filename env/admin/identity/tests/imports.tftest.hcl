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
  target          = module.deployment_identity.module.github_oidc.aws_iam_role.this["apply_identity"]
  override_during = plan
  values          = { arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity" }
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

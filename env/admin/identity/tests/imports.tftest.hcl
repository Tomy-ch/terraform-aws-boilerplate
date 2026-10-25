# import block の取り込み先と id は見ない。terraform test には検査の手段が無く、段2 の plan だけが見る。
# mock の provider からは import できないので、取り込み先をすべて override する。
mock_provider "aws" {
  mock_data "aws_caller_identity" {
    defaults = { account_id = "123456789012" }
  }
  mock_data "aws_region" {
    defaults = { region = "ap-northeast-1" }
  }
}

override_resource {
  target = module.state_backend.module.s3.aws_s3_bucket.this
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

override_resource {
  target = module.state_backend.module.s3.aws_s3_bucket_server_side_encryption_configuration.this
}

run "admin_state_bucket_name" {
  command = plan

  assert {
    condition     = local.state_bucket_name == "tabp-admin-tfstate-123456789012-ap-northeast-1-an"
    error_message = "管理アカウントの state Bucket の名前が tabp-admin-tfstate-<account_id>-<region>-an になっていません。"
  }
}

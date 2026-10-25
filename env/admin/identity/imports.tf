# bootstrap の段1 で AWS CLI が作った state Bucket を取り込む（ADR-0602 決定4・6）。
locals {
  state_bucket_name = module.state_backend.bucket_name
}

import {
  to = module.state_backend.module.s3.aws_s3_bucket.this
  id = local.state_bucket_name
}

import {
  to = module.state_backend.module.s3.aws_s3_bucket_public_access_block.this
  id = local.state_bucket_name
}

import {
  to = module.state_backend.module.s3.aws_s3_bucket_ownership_controls.this
  id = local.state_bucket_name
}

import {
  to = module.state_backend.module.s3.aws_s3_bucket_versioning.this
  id = local.state_bucket_name
}

import {
  to = module.state_backend.module.s3.aws_s3_bucket_server_side_encryption_configuration.this
  id = local.state_bucket_name
}

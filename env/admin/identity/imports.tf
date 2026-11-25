# bootstrap の段1 で AWS CLI が作った state Bucket を取り込む（ADR-0602 決定4・6）。既定の暗号化は段1 で置かず、段4 の apply が作る（state-backend/ADR-0101 決定4）。
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

data "aws_caller_identity" "current" {}
data "aws_partition" "current" {}

# 管理アカウントには、Chatbot が作った service-linked role が identity root より前に在る（deployment-identity/ADR-0002 決定4）。
import {
  to = module.deployment_identity.module.service_linked_roles.aws_iam_service_linked_role.chatbot[0]
  id = "arn:${data.aws_partition.current.partition}:iam::${data.aws_caller_identity.current.account_id}:role/aws-service-role/management.chatbot.amazonaws.com/AWSServiceRoleForAWSChatbot"
}

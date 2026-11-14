provider "aws" {
  region = "ap-northeast-1"
}

module "deployment_identity" {
  source = "../"

  project = "tabp"
  env     = "admin"
  github = {
    owner         = "Tomy-ch"
    owner_id      = "182787471"
    repository    = "terraform-aws-boilerplate"
    repository_id = "1370808564"
  }
  state_bucket_arn       = "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an"
  state_key_arn          = "arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
  plan_output_bucket_arn = "arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an"

  access_log_group_arn       = "arn:aws:logs:ap-northeast-1:123456789012:log-group:tabp-admin-state-backend-access"
  access_log_destination_arn = "arn:aws:logs:ap-northeast-1:123456789012:delivery-destination:tabp-admin-state-backend-access"
  access_log_source_arns = [
    "arn:aws:logs:ap-northeast-1:123456789012:delivery-source:tabp-admin-tfstate",
    "arn:aws:logs:ap-northeast-1:123456789012:delivery-source:tabp-admin-tfplan",
  ]
}

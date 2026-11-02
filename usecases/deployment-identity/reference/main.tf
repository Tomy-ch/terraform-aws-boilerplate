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
  plan_output_bucket_arn = "arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an"
}

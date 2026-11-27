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
  state_bucket_arn        = "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an"
  state_key_arn           = "arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
  plan_output_bucket_arn  = "arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an"
  apply_output_bucket_arn = "arn:aws:s3:::tabp-admin-tfapply-123456789012-ap-northeast-1-an"
  break_glass_arns        = ["arn:aws:iam::123456789012:user/break-glass/break-glass-1", "arn:aws:iam::123456789012:user/break-glass/break-glass-2", "arn:aws:iam::123456789012:group/break-glass/break-glass", "arn:aws:iam::123456789012:role/break-glass/tabp-break-glass"]

  bootstrap_read_role          = { name = "tabp-bootstrap-read", path = "/bootstrap/" }
  bootstrap_member_account_ids = { security = "111111111111" }
}

provider "aws" {
  alias  = "security"
  region = "ap-northeast-1"
}

module "deployment_identity_security" {
  source = "../"
  providers = {
    aws = aws.security
  }

  project = "tabp"
  env     = "security"
  github = {
    owner         = "Tomy-ch"
    owner_id      = "182787471"
    repository    = "terraform-aws-boilerplate"
    repository_id = "1370808564"
  }
  state_bucket_arn        = "arn:aws:s3:::tabp-security-tfstate-111111111111-ap-northeast-1-an"
  state_key_arn           = "arn:aws:kms:ap-northeast-1:111111111111:key/1234abcd-12ab-34cd-56ef-1234567890ab"
  plan_output_bucket_arn  = "arn:aws:s3:::tabp-security-tfplan-111111111111-ap-northeast-1-an"
  apply_output_bucket_arn = "arn:aws:s3:::tabp-security-tfapply-111111111111-ap-northeast-1-an"
  break_glass_arns        = ["arn:aws:iam::111111111111:role/break-glass/tabp-break-glass"]
}

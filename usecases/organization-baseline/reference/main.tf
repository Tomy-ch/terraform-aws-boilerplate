provider "aws" {
  region = "ap-northeast-1"
}

module "organization_baseline" {
  source = "../"

  project                      = "tabp"
  bootstrap_read_principal_arn = "arn:aws:iam::111111111111:role/tabp-admin-ci-plan"
  # env/admin/identity と同じく、立ち上げ中のアカウントを入れる（decl.json の deny-bootstrap-role の付け先は空）。
  bootstrapping = ["security", "log-archive"]

  state_bucket_arn        = "arn:aws:s3:::tabp-admin-tfstate-111111111111-ap-northeast-1-an"
  plan_output_bucket_arn  = "arn:aws:s3:::tabp-admin-tfplan-111111111111-ap-northeast-1-an"
  apply_output_bucket_arn = "arn:aws:s3:::tabp-admin-tfapply-111111111111-ap-northeast-1-an"
  state_key_arn           = "arn:aws:kms:ap-northeast-1:111111111111:key/00000000-0000-0000-0000-000000000000"

  identity_center = local.identity_center
}

locals {
  identity_center = {
    management_assignments = [
      for p in ["read_only", "terraform_plan", "billing", "support", "organization_admin"] : { permission_set = p, user = "owner" }
    ]
    member_assignments = [
      for pair in setproduct(["security", "log-archive"], ["read_only", "terraform_plan", "billing", "support"]) :
      { account = pair[0], permission_set = pair[1], group = "operators" }
    ]
  }
}

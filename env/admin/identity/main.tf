data "aws_caller_identity" "current" {}
data "aws_partition" "current" {}

# break-glass の ARN は organization-baseline の break_glass_arns と同じ規則で組む。
locals {
  iam_arn_prefix = "arn:${data.aws_partition.current.partition}:iam::${data.aws_caller_identity.current.account_id}"
  break_glass_arns = [
    "${local.iam_arn_prefix}:user/break-glass/break-glass-1",
    "${local.iam_arn_prefix}:user/break-glass/break-glass-2",
    "${local.iam_arn_prefix}:group/break-glass/break-glass",
    "${local.iam_arn_prefix}:role/break-glass/tabp-break-glass",
  ]
}

module "state_backend" {
  source = "../../../usecases/state-backend"

  project                                 = "tabp"
  env                                     = "admin"
  state_noncurrent_version_retention_days = 90
  ci_role_arns = {
    plan            = module.deployment_identity.plan_role_arn
    apply           = module.deployment_identity.apply_role_arn
    apply_identity  = module.deployment_identity.apply_identity_role_arn
    unlock_identity = module.deployment_identity.unlock_identity_role_arn
  }
}

module "deployment_identity" {
  source = "../../../usecases/deployment-identity"

  project = "tabp"
  env     = "admin"
  github = {
    owner         = "Tomy-ch"
    owner_id      = "182787471"
    repository    = "terraform-aws-boilerplate"
    repository_id = "1370808564"
  }
  state_bucket_arn        = module.state_backend.bucket_arn
  state_key_arn           = module.state_backend.state_key_arn
  plan_output_bucket_arn  = module.state_backend.plan_output_bucket_arn
  apply_output_bucket_arn = module.state_backend.apply_output_bucket_arn
  break_glass_arns        = local.break_glass_arns
}

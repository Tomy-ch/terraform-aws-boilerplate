locals {
  state_name         = "${var.project}-${var.env}-tfstate"
  identity_state_key = "identity/terraform.tfstate"
  # identity root の lock file。backend.tf の key と、deployment-identity の unlock-identity の IAM の Resource が同じ key を持つ。
  identity_lock_key  = "identity/terraform.tfstate.tflock"
  identity_keys      = [local.identity_state_key, local.identity_lock_key]
  workload_state_key = "workload/terraform.tfstate"
  workload_lock_key  = "workload/terraform.tfstate.tflock"
  workload_keys      = [local.workload_state_key, local.workload_lock_key]
}

module "s3" {
  source = "../../modules/s3"

  name                              = local.state_name
  noncurrent_version_retention_days = var.state_noncurrent_version_retention_days
  kms_key_arn                       = module.kms.key_arn

  object_writers = {
    identity       = { role_arn = var.ci_role_arns.apply_identity, keys = local.identity_keys, access = "write" }
    workload       = { role_arn = var.ci_role_arns.apply, keys = local.workload_keys, access = "write" }
    identityUnlock = { role_arn = var.ci_role_arns.unlock_identity, keys = [local.identity_lock_key], access = "delete" }
    workloadUnlock = { role_arn = var.ci_role_arns.unlock, keys = [local.workload_lock_key], access = "delete" }
  }
  bootstrap_executor = var.bootstrap_executor_user_name == null ? null : { user_name = var.bootstrap_executor_user_name, keys = local.identity_keys }
}

module "kms" {
  source = "../../modules/kms"

  name = local.state_name
  users = {
    # 割当は state-backend/ADR-0201 決定9。出力の Bucket の object は Bucket Policy の書き手と同じ宣言から組む。
    roles = [
      { arn = var.ci_role_arns.plan, via = "s3", access = "read", objects = [for k in [local.identity_state_key, local.workload_state_key] : "${module.s3.bucket_arn}/${k}"] },
      { arn = var.ci_role_arns.plan, via = "s3", access = "read-write", object_prefixes = module.plan_output.writer_object_prefixes[var.ci_role_arns.plan] },
      {
        arn             = var.ci_role_arns.apply_identity
        via             = "s3"
        access          = "read-write"
        objects         = [for k in local.identity_keys : "${module.s3.bucket_arn}/${k}"]
        object_prefixes = module.plan_output.writer_object_prefixes[var.ci_role_arns.apply_identity]
      },
      { arn = var.ci_role_arns.unlock_identity, via = "s3", access = "read", objects = ["${module.s3.bucket_arn}/${local.identity_lock_key}"] },
      {
        arn             = var.ci_role_arns.apply
        via             = "s3"
        access          = "read-write"
        objects         = [for k in local.workload_keys : "${module.s3.bucket_arn}/${k}"]
        object_prefixes = module.apply_output.writer_object_prefixes[var.ci_role_arns.apply]
      },
      { arn = var.ci_role_arns.unlock, via = "s3", access = "read", objects = ["${module.s3.bucket_arn}/${local.workload_lock_key}"] },
    ]
  }
}

module "plan_output" {
  source = "./internal/plan-output"

  name           = "${var.project}-${var.env}-tfplan"
  retention_days = var.plan_output_retention_days
  kms_key_arn    = module.kms.key_arn
  # 接頭辞の割当は deployment-identity/ADR-0201 決定8・10。
  writers = [
    { role_arn = var.ci_role_arns.plan, key_prefix = "pr/" },
    { role_arn = var.ci_role_arns.plan, key_prefix = "branch/" },
    { role_arn = var.ci_role_arns.apply_identity, key_prefix = "apply/" },
  ]
}

module "apply_output" {
  source = "./internal/plan-output"

  name           = "${var.project}-${var.env}-tfapply"
  retention_days = var.plan_output_retention_days
  kms_key_arn    = module.kms.key_arn
  writers        = [{ role_arn = var.ci_role_arns.apply, key_prefix = "" }]
}

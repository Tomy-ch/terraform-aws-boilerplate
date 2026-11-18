module "github_oidc" {
  source = "./internal/github-oidc"

  name_prefix             = "${var.project}-${var.env}"
  env                     = var.env
  github                  = var.github
  state_bucket_arn        = var.state_bucket_arn
  state_key_arn           = var.state_key_arn
  plan_output_bucket_arn  = var.plan_output_bucket_arn
  apply_output_bucket_arn = var.apply_output_bucket_arn
  break_glass_arns        = var.break_glass_arns
}

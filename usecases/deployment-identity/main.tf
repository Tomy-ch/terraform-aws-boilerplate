module "github_oidc" {
  source = "./internal/github-oidc"

  name_prefix            = "${var.project}-${var.env}"
  env                    = var.env
  github                 = var.github
  state_bucket_arn       = var.state_bucket_arn
  state_key_arn          = var.state_key_arn
  plan_output_bucket_arn = var.plan_output_bucket_arn

  access_log_group_arn       = var.access_log_group_arn
  access_log_destination_arn = var.access_log_destination_arn
  access_log_source_arns     = var.access_log_source_arns
}

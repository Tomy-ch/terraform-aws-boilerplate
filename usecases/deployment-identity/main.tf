module "github_oidc" {
  source = "./internal/github-oidc"

  name_prefix            = "${var.project}-${var.env}"
  env                    = var.env
  github                 = var.github
  state_bucket_arn       = var.state_bucket_arn
  plan_output_bucket_arn = var.plan_output_bucket_arn
}

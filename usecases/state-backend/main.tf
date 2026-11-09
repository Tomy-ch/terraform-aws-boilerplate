module "s3" {
  source = "../../modules/s3"

  name                              = "${var.project}-${var.env}-tfstate"
  noncurrent_version_retention_days = var.state_noncurrent_version_retention_days
}

module "plan_output" {
  source = "./internal/plan-output"

  name           = "${var.project}-${var.env}-tfplan"
  retention_days = var.plan_output_retention_days
}

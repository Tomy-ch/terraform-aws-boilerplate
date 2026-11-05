module "s3" {
  source = "../../modules/s3"

  name = "${var.project}-${var.env}-tfstate"
}

module "plan_output" {
  source = "./internal/plan-output"

  name           = "${var.project}-${var.env}-tfplan"
  retention_days = var.plan_output_retention_days
}

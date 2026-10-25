module "s3" {
  source = "../../modules/s3"

  name = "${var.project}-${var.env}-tfstate"
}

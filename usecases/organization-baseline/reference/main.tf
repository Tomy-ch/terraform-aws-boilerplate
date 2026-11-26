provider "aws" {
  region = "ap-northeast-1"
}

module "organization_baseline" {
  source = "../"

  project                      = "tabp"
  bootstrap_read_principal_arn = "arn:aws:iam::111111111111:role/tabp-admin-ci-plan"
  # env/admin/identity と同じく、立ち上げ中のアカウントを入れる（decl.json の deny-bootstrap-role の付け先は空）。
  bootstrapping = ["security", "log-archive"]
}

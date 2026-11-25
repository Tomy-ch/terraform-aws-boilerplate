provider "aws" {
  region = "ap-northeast-1"
}

module "organization_baseline" {
  source = "../"

  project = "tabp"
  # env/admin/identity と同じく、立ち上げ中のアカウントを入れる（decl.json の deny-bootstrap-role の付け先は空）。
  bootstrapping = ["security", "log-archive"]
}

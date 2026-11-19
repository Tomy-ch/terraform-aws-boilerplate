provider "aws" {
  region = "ap-northeast-1"
}

module "ops_notification" {
  source = "../"

  project = "tabp"
  env     = "sandbox"
  chat = {
    slack = { team_id = "T07EA123LEP", channel_id = "C07EZ1ABC23" }
  }
  permissions_boundary_arn = "arn:aws:iam::123456789012:policy/tabp-sandbox-workload-boundary"
}

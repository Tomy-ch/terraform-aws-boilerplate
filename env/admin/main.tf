# workload root は identity root の state を読めないので、permissions boundary の ARN を名前の規則から組む
# （ADR-0102 決定16 のただし書き、deployment-identity/ADR-0201 決定15）。
data "aws_caller_identity" "current" {}

data "aws_partition" "current" {}

locals {
  ops_notification_boundary_arn = "arn:${data.aws_partition.current.partition}:iam::${data.aws_caller_identity.current.account_id}:policy/tabp-admin-ci-boundary-ops-notification"
}

module "ops_notification" {
  source = "../../usecases/ops-notification"

  project = "tabp"
  env     = "admin"
  chat = {
    slack = { team_id = "T0BKS4FL4AV", channel_id = "C0CK6PHQZJR" }
  }
  permissions_boundary_arn = local.ops_notification_boundary_arn
}

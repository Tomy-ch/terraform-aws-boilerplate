# workload root は identity root の state を読めないので、permissions boundary の ARN を名前から組む
# （deployment-identity/ADR-0201 決定15）。
data "aws_caller_identity" "current" {}

data "aws_partition" "current" {}

module "ops_notification" {
  source = "../../usecases/ops-notification"

  project = "tabp"
  env     = "admin"
  chat = {
    slack = { team_id = "T0BKS4FL4AV", channel_id = "C0CK6PHQZJR" }
  }
  permissions_boundary_arn = "arn:${data.aws_partition.current.partition}:iam::${data.aws_caller_identity.current.account_id}:policy/tabp-admin-ci-boundary-ops-notification"
}

# kics-scan disable=e592a0c5-5bdb-414c-9066-5dba7cdea370,e38a8e0a-b88b-4902-b3fe-b0fcb17d5c10
# e592a0c5（IAM Access Analyzer）: KICS はファイルごとに analyzer の宣言を求める。analyzer はアカウント単位の統制で account-baseline が持つ（docs/v1.0-plan/scope.md 第7節）。撤回: KICS がファイルの外の宣言を数えるようになったとき。
# e38a8e0a（tag）: tag は共通の単一入力でだけ付ける（ADR-0204 決定5）。この module はその入力を持たない。撤回: 共通の tag 入力を配線したとき。

data "aws_caller_identity" "current" {}

data "aws_partition" "current" {}

data "aws_region" "current" {}

# メンバーアカウントの立ち上げの読み取りの role（organization-baseline/ADR-0203）。description は ASCII に限る。
resource "aws_cloudformation_stack_set" "this" {
  name             = var.role_name
  description      = "Read-only role for bootstrapping member accounts"
  permission_model = "SERVICE_MANAGED"
  capabilities     = ["CAPABILITY_NAMED_IAM"]

  auto_deployment {
    enabled                          = true
    retain_stacks_on_account_removal = false
  }

  managed_execution {
    active = true
  }

  # 権限はメンバーの state-backend と deployment-identity の refresh に要る読み取りだけで、state の object と鍵の暗号操作を含まない（organization-baseline/ADR-0203）。
  # 管理アカウントの plan の role の読みを覆うことは plan.organization_baseline_bootstrap_read が見る。
  # Principal に role の ARN を書くと、role を作り直したときに一意 ID がずれて信頼が壊れるので、アカウントの root と aws:PrincipalArn で限る。
  template_body = templatefile("${path.module}/templates/member-bootstrap-read-role.json.tftpl", {
    role_name             = var.role_name
    role_path             = var.path
    project               = var.project
    partition             = data.aws_partition.current.partition
    management_account_id = data.aws_caller_identity.current.account_id
    trusted_principal_arn = var.trusted_principal_arn
  })

  lifecycle {
    prevent_destroy = true
  }
}

resource "aws_cloudformation_stack_instances" "this" {
  stack_set_name = aws_cloudformation_stack_set.this.name
  regions        = [data.aws_region.current.region]

  deployment_targets {
    organizational_unit_ids = [var.organization_root_id]
  }

  lifecycle {
    prevent_destroy = true
  }
}

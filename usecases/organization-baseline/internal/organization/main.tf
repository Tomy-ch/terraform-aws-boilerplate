# kics-scan disable=e592a0c5-5bdb-414c-9066-5dba7cdea370,e38a8e0a-b88b-4902-b3fe-b0fcb17d5c10
# e592a0c5（IAM Access Analyzer）: KICS はファイルごとに analyzer の宣言を求める。analyzer はアカウント単位の統制で account-baseline が持つ（docs/v1.0-plan/scope.md 第7節）。撤回: KICS がファイルの外の宣言を数えるようになったとき。
# e38a8e0a（tag）: tag は共通の単一入力でだけ付ける（ADR-0204 決定5）。この module はその入力を持たない。撤回: 共通の tag 入力を配線したとき。

resource "aws_organizations_organization" "this" {
  feature_set = "ALL"
  enabled_policy_types = [
    "DECLARATIVE_POLICY_EC2",
    "RESOURCE_CONTROL_POLICY",
    "SERVICE_CONTROL_POLICY",
  ]

  lifecycle {
    prevent_destroy = true
    # 信頼アクセスは各サービスの側で有効化する（organization-baseline/ADR-0001）。この一覧は authoritative で、
    # 書かなければ apply がサービスの側で有効化したものを外す。
    ignore_changes = [aws_service_access_principals]
  }
}

# root の資格情報の中央管理（aws_iam_organizations_features）の前提。
resource "aws_organizations_aws_service_access" "iam" {
  service_principal = "iam.amazonaws.com"

  depends_on = [aws_organizations_organization.this]
}

resource "aws_iam_organizations_features" "this" {
  enabled_features = ["RootCredentialsManagement", "RootSessions"]

  depends_on = [aws_organizations_aws_service_access.iam]
}

locals {
  root_id = aws_organizations_organization.this.roots[0].id
}

resource "aws_organizations_organizational_unit" "top" {
  for_each = toset(["Security", "Workloads", "Sandbox", "PolicyStaging"])

  name      = each.key
  parent_id = local.root_id

  lifecycle {
    prevent_destroy = true
  }
}

resource "aws_organizations_organizational_unit" "workloads" {
  for_each = toset(["Prod", "NonProd"])

  name      = each.key
  parent_id = aws_organizations_organizational_unit.top["Workloads"].id

  lifecycle {
    prevent_destroy = true
  }
}

# kics-scan disable=e592a0c5-5bdb-414c-9066-5dba7cdea370,e38a8e0a-b88b-4902-b3fe-b0fcb17d5c10
# e592a0c5（IAM Access Analyzer）: KICS はファイルごとに analyzer の宣言を求める。analyzer はアカウント単位の統制で account-baseline が持つ（docs/v1.0-plan/scope.md 第7節）。撤回: KICS がファイルの外の宣言を数えるようになったとき。
# e38a8e0a（tag）: tag は共通の単一入力でだけ付ける（ADR-0204 決定5）。この module はその入力を持たない。撤回: 共通の tag 入力を配線したとき。

# メンバーの root が自分のアカウントの外のアカウントの ID を受ける経路（repository-wide/ADR-0605）。
# 共有は advanced tier の parameter に限られる（Systems Manager User Guide「Working with shared parameters」）。
resource "aws_ssm_parameter" "this" {
  name        = "/${var.project}/organization-baseline/account-ids"
  description = "Member account IDs by env (JSON), shared read-only to the organization through AWS RAM."
  type        = "String"
  tier        = "Advanced"
  value       = jsonencode(var.account_ids)
}

resource "aws_ram_resource_share" "this" {
  name                      = "${var.project}-account-ids"
  allow_external_principals = false
}

resource "aws_ram_resource_association" "this" {
  resource_arn       = aws_ssm_parameter.this.arn
  resource_share_arn = aws_ram_resource_share.this.arn
}

# 組織を principal にするには、RAM の組織との共有を先に有効にしておく（docs/setup/organization-baseline.md 3.10）。
resource "aws_ram_principal_association" "organization" {
  principal          = var.organization_arn
  resource_share_arn = aws_ram_resource_share.this.arn
}

# kics-scan disable=e592a0c5-5bdb-414c-9066-5dba7cdea370
# e592a0c5（IAM Access Analyzer）: KICS はファイルごとに analyzer の宣言を求める。analyzer はアカウント単位の統制で account-baseline が持つ（docs/v1.0-plan/scope.md 第7節）。撤回: KICS がファイルの外の宣言を数えるようになったとき。

data "aws_region" "home" {}

# Config.1 は、recorder が service-linked role を使うことを求める（account-baseline/ADR-0102）。
resource "aws_iam_service_linked_role" "config" {
  aws_service_name = "config.amazonaws.com"
}

resource "aws_config_configuration_recorder" "this" {
  for_each = var.regions

  region   = each.key
  name     = var.name_prefix
  role_arn = aws_iam_service_linked_role.config.arn

  # IAM などのグローバルな resource は、home のリージョンでだけ記録する。全リージョンで記録すると同じ項目を重ねて数える。
  recording_group {
    all_supported                 = true
    include_global_resource_types = each.key == data.aws_region.home.region
  }

  recording_mode {
    recording_frequency = "CONTINUOUS"
  }

  lifecycle {
    precondition {
      condition     = contains(var.regions, data.aws_region.home.region)
      error_message = "provider のリージョン（${data.aws_region.home.region}）が regions に無く、グローバルな resource を記録する recorder が無くなります。"
    }
  }
}

resource "aws_config_delivery_channel" "this" {
  for_each = var.regions

  region         = each.key
  name           = var.name_prefix
  s3_bucket_name = var.delivery.bucket_name
  s3_kms_key_arn = var.delivery.kms_key_arn

  depends_on = [aws_config_configuration_recorder.this]
}

resource "aws_config_configuration_recorder_status" "this" {
  for_each = var.regions

  region     = each.key
  name       = aws_config_configuration_recorder.this[each.key].name
  is_enabled = true

  depends_on = [aws_config_delivery_channel.this]
}

resource "aws_config_retention_configuration" "this" {
  for_each = var.regions

  region                   = each.key
  retention_period_in_days = var.retention_days
}

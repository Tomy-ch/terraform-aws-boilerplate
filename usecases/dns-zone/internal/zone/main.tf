# kics-scan disable=e38a8e0a-b88b-4902-b3fe-b0fcb17d5c10,e592a0c5-5bdb-414c-9066-5dba7cdea370,084c6686-2a70-4710-91b1-000393e54c12,7dbba512-e244-42dc-98bb-422339827967
# e38a8e0a（tag）: tag は共通の単一入力でだけ付ける（ADR-0204 決定5）。この module はその入力を持たない。撤回: 共通の tag 入力を配線したとき。
# e592a0c5（IAM Access Analyzer）: KICS はファイルごとに analyzer の宣言を求める。analyzer はアカウント単位の統制で account-baseline が持つ（docs/v1.0-plan/scope.md 第7節）。撤回: KICS がファイルの外の宣言を数えるようになったとき。
# 084c6686（Shield Advanced）: アカウントか組織の単位で結ぶ有償の契約で、zone ごとに決めない（README の unsupported）。撤回: 組織が Shield Advanced を採るとき。
# 7dbba512（query log の Log Group）: Log Group は internal/query-log が持ち、入力で受ける。KICS は module をまたぐ参照を解けない。query log が宣言した Log Group を指すことは conftest/plan の PLAN-DNS-02 が見る。撤回: KICS が module をまたぐ参照を解くようになったとき。

data "aws_caller_identity" "current" {}
data "aws_partition" "current" {}

locals {
  ds_records = { for k, d in var.delegations : k => d.ds_record if d.ds_record != null }
}

resource "aws_route53_zone" "this" {
  name                        = var.zone_name
  comment                     = "${var.name} public hosted zone"
  force_destroy               = false # 管理外の record を含めて消さない（dns-zone/ADR-0201）
  enable_accelerated_recovery = false # dns-zone/ADR-0201
}

resource "aws_route53_query_log" "this" {
  zone_id                  = aws_route53_zone.this.zone_id
  cloudwatch_log_group_arn = var.query_log_group_arn
}

resource "aws_route53_key_signing_key" "this" {
  hosted_zone_id             = aws_route53_zone.this.zone_id
  key_management_service_arn = var.dnssec_key_arn
  name                       = replace(var.name, "-", "")
  status                     = "ACTIVE"

  lifecycle {
    precondition {
      condition     = startswith(var.dnssec_key_arn, "arn:${data.aws_partition.current.partition}:kms:us-east-1:${data.aws_caller_identity.current.account_id}:key/")
      error_message = "dnssec_key_arn が、zone と同じ partition・アカウントの us-east-1 の鍵ではありません。"
    }
  }
}

resource "aws_route53_hosted_zone_dnssec" "this" {
  hosted_zone_id = aws_route53_zone.this.zone_id
  signing_status = "SIGNING"

  depends_on = [aws_route53_key_signing_key.this]
}

# 委任の NS と DS は、子の zone のアカウントの apply の出力を env の値へ写したもの（dns-zone/ADR-0101）。
resource "aws_route53_record" "delegation_ns" {
  for_each = var.delegations

  zone_id         = aws_route53_zone.this.zone_id
  name            = "${each.key}.${var.zone_name}"
  type            = "NS"
  ttl             = 3600
  records         = each.value.name_servers
  allow_overwrite = false
}

resource "aws_route53_record" "delegation_ds" {
  for_each = local.ds_records

  zone_id         = aws_route53_zone.this.zone_id
  name            = "${each.key}.${var.zone_name}"
  type            = "DS"
  ttl             = 300
  records         = [each.value]
  allow_overwrite = false
}

# DNSSEC の署名が止まる故障と、KSK が鍵を使えなくなった状態を通知する（dns-zone/ADR-0302）。
resource "aws_cloudwatch_metric_alarm" "dnssec" {
  for_each = {
    internal_failure = "DNSSECInternalFailure"
    ksk_action       = "DNSSECKeySigningKeysNeedingAction"
  }

  region              = "us-east-1" # DNSSEC の metric は us-east-1 にだけ出る（Route 53 Developer Guide「Monitoring hosted zones using Amazon CloudWatch」）
  alarm_name          = "${var.name}-dns-${replace(each.key, "_", "-")}"
  alarm_description   = "Route 53 の DNSSEC の ${each.value} が 0 を超えました。"
  namespace           = "AWS/Route53"
  metric_name         = each.value
  statistic           = "Maximum"
  period              = 300
  evaluation_periods  = 1
  threshold           = 0
  comparison_operator = "GreaterThanThreshold"
  treat_missing_data  = "notBreaching" # 故障が無い間は metric が出ないことがある
  actions_enabled     = true

  dimensions = {
    HostedZoneId = aws_route53_zone.this.zone_id
  }

  alarm_actions             = [var.notify_topic_arn]
  ok_actions                = [var.notify_topic_arn]
  insufficient_data_actions = [var.notify_topic_arn]

  lifecycle {
    precondition {
      condition     = startswith(var.notify_topic_arn, "arn:${data.aws_partition.current.partition}:sns:us-east-1:${data.aws_caller_identity.current.account_id}:")
      error_message = "notify_topic_arn が、zone と同じ partition・アカウントの us-east-1 の Topic ではありません。"
    }
  }
}

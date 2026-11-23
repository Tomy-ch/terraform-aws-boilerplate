# kics-scan disable=e592a0c5-5bdb-414c-9066-5dba7cdea370,e38a8e0a-b88b-4902-b3fe-b0fcb17d5c10
# e592a0c5（IAM Access Analyzer）: KICS はファイルごとに analyzer の宣言を求める。analyzer はアカウント単位の統制で account-baseline が持つ（docs/v1.0-plan/scope.md 第7節）。撤回: KICS がファイルの外の宣言を数えるようになったとき。
# e38a8e0a（tag）: tag は共通の単一入力でだけ付ける（ADR-0204 決定5）。この module はその入力を持たない。撤回: 共通の tag 入力を配線したとき。

resource "aws_s3_bucket_metric" "delivery" {
  bucket = aws_s3_bucket.this.id
  name   = "delivery"

  filter {
    prefix = "AWSLogs/"
  }
}

# 組織の trail は数分ごとに配信するので、1時間の書き込みが0件なら配信が止まっている。
resource "aws_cloudwatch_metric_alarm" "delivery_stalled" {
  alarm_name          = "${local.bucket_name}-delivery-stalled"
  alarm_description   = "監査の証跡の Bucket に、1時間の書き込みがありません。"
  namespace           = "AWS/S3"
  metric_name         = "PutRequests"
  statistic           = "Sum"
  period              = 3600
  evaluation_periods  = 1
  threshold           = 1
  comparison_operator = "LessThanThreshold"
  treat_missing_data  = "breaching" # request metrics は要求が無い間は出ないので、欠落を途絶として読む（audit-evidence/ADR-0301 決定2）
  actions_enabled     = true

  dimensions = {
    BucketName = aws_s3_bucket.this.id
    FilterId   = aws_s3_bucket_metric.delivery.name
  }

  alarm_actions             = [var.notify_topic_arn]
  ok_actions                = [var.notify_topic_arn]
  insufficient_data_actions = [var.notify_topic_arn]

  lifecycle {
    precondition {
      condition     = startswith(var.notify_topic_arn, "arn:${data.aws_partition.current.partition}:sns:${data.aws_region.current.region}:${data.aws_caller_identity.current.account_id}:")
      error_message = "notify_topic_arn が、Bucket と同じ partition・リージョン・アカウントの Topic ではありません。"
    }
  }
}

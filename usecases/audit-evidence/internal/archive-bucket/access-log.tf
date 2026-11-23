# kics-scan disable=e592a0c5-5bdb-414c-9066-5dba7cdea370
# e592a0c5（IAM Access Analyzer）: KICS はファイルごとに analyzer の宣言を求める。analyzer はアカウント単位の統制で account-baseline が持つ（docs/v1.0-plan/scope.md 第7節）。撤回: KICS がファイルの外の宣言を数えるようになったとき。

# Object Lock の Bucket はサーバーアクセスログの宛先にできず、宛先の Bucket は SSE-S3 に限られる（Amazon S3 User Guide「Object Lock considerations」）。CloudWatch Logs へ配送する（docs/v1.0-plan/scope.md 第9.1節）。
resource "aws_cloudwatch_log_delivery_source" "access_log" {
  name         = var.access_log_source_name
  log_type     = "S3_SERVER_ACCESS_LOGS"
  resource_arn = aws_s3_bucket.this.arn
}

resource "aws_cloudwatch_log_delivery" "access_log" {
  delivery_source_name     = aws_cloudwatch_log_delivery_source.access_log.name
  delivery_destination_arn = var.access_log_destination_arn
}

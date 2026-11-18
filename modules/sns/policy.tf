# kics-scan disable=e592a0c5-5bdb-414c-9066-5dba7cdea370
# e592a0c5（IAM Access Analyzer）: アカウント単位の統制で account-baseline が持つ（docs/v1.0-plan/scope.md 第7節）。撤回: account-baseline を実装したとき。

locals {
  account   = data.aws_caller_identity.current.account_id
  topic_arn = "arn:${data.aws_partition.current.partition}:sns:${data.aws_region.current.region}:${local.account}:${var.name}"

  # CloudWatch と Budgets の公式の例の aws:SourceArn のワイルドカードを置かず、アカウントの固定値だけで限る（README「公式推奨からの逸脱」）。
  publish_statements = [for p in sort(tolist(var.publishers)) : {
    Sid       = { budgets = "AllowBudgetsPublish", cloudwatch = "AllowCloudWatchPublish", costalerts = "AllowCostAlertsPublish", events = "AllowEventsPublish" }[p]
    Effect    = "Allow"
    Principal = { Service = "${p}.amazonaws.com" }
    Action    = "sns:Publish"
    Resource  = local.topic_arn
    Condition = { StringEquals = { "aws:SourceAccount" = local.account } }
  }]

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = concat([{
      Sid       = "DenyInsecureTransport"
      Effect    = "Deny"
      Principal = "*"
      Action    = "sns:Publish"
      Resource  = local.topic_arn
      Condition = { Bool = { "aws:SecureTransport" = "false" } }
    }], local.publish_statements)
  })
}

resource "aws_sns_topic_policy" "this" {
  arn    = aws_sns_topic.this.arn
  policy = local.policy
}

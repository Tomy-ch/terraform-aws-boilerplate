locals {
  # read の Action 集合（ops-notification/ADR-0201 決定2）。
  read_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid      = "ReadAlarms"
        Effect   = "Allow"
        Action   = ["cloudwatch:DescribeAlarmHistory", "cloudwatch:DescribeAlarms", "cloudwatch:ListTagsForResource"]
        Resource = "arn:${local.partition}:cloudwatch:${local.region}:${local.account}:alarm:*"
      },
      {
        Sid      = "ReadMetrics"
        Effect   = "Allow"
        Action   = ["cloudwatch:DescribeAlarmsForMetric", "cloudwatch:GetMetricData", "cloudwatch:GetMetricStatistics", "cloudwatch:GetMetricWidgetImage", "cloudwatch:ListMetrics"]
        Resource = "*"
      },
    ]
  })
}

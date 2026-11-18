# kics-scan disable=e592a0c5-5bdb-414c-9066-5dba7cdea370,e38a8e0a-b88b-4902-b3fe-b0fcb17d5c10
# e592a0c5（IAM Access Analyzer）: アカウント単位の統制で account-baseline が持つ（docs/v1.0-plan/scope.md 第7節）。撤回: account-baseline を実装したとき。
# e38a8e0a（tag）: tag は共通の単一入力でだけ付ける（ADR-0204 決定5）。この module はその入力を持たない。撤回: 共通の tag 入力を配線したとき。

resource "aws_iam_role" "flow_logs" {
  name = "${var.name}-vpc-flow-logs"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Sid       = "AllowVpcFlowLogsOfThisAccount"
      Effect    = "Allow"
      Principal = { Service = "vpc-flow-logs.amazonaws.com" }
      Action    = "sts:AssumeRole"
      Condition = {
        StringEquals = { "aws:SourceAccount" = data.aws_caller_identity.current.account_id }
        # flow log の ID は role より後に決まるので、ID まで絞ると role と flow log が循環する。
        ArnLike = { "aws:SourceArn" = "arn:${data.aws_partition.current.partition}:ec2:${data.aws_region.current.region}:${data.aws_caller_identity.current.account_id}:vpc-flow-log/*" }
      }
    }]
  })
}

resource "aws_iam_role_policy" "flow_logs" {
  name = "deliver-flow-logs"
  role = aws_iam_role.flow_logs.name
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid      = "WriteLogEvents"
        Effect   = "Allow"
        Action   = ["logs:CreateLogStream", "logs:PutLogEvents"]
        Resource = "${var.flow_logs_log_group_arn}:log-stream:*"
      },
      {
        Sid      = "DescribeLogStreams"
        Effect   = "Allow"
        Action   = "logs:DescribeLogStreams"
        Resource = var.flow_logs_log_group_arn
      },
      {
        # DescribeLogGroups は resource-level の権限を持たない（Service Authorization Reference）。
        Sid      = "DescribeLogGroups"
        Effect   = "Allow"
        Action   = "logs:DescribeLogGroups"
        Resource = "*"
      },
    ]
  })
}

resource "aws_iam_role_policies_exclusive" "flow_logs" {
  role_name    = aws_iam_role.flow_logs.name
  policy_names = [aws_iam_role_policy.flow_logs.name]
}

resource "aws_iam_role_policy_attachments_exclusive" "flow_logs" {
  role_name   = aws_iam_role.flow_logs.name
  policy_arns = []
}

data "aws_caller_identity" "current" {}

locals {
  # Route 53 は公開の Hosted Zone のクエリログを us-east-1 の Log Group にしか送らない（Route 53 Developer Guide「Public DNS query logging」）。
  region         = "us-east-1"
  log_group_name = "/aws/route53/${var.zone_name}"
}

module "kms" {
  source = "../../../../modules/kms"

  name   = "${var.name}-dns-query-logs"
  region = local.region
  users = {
    services = { logs = [local.log_group_name] }
  }
}

module "log_group" {
  source = "../../../../modules/log-group"

  name                = local.log_group_name
  region              = local.region
  retention_days      = var.retention_days
  kms_key_arn         = module.kms.key_arn
  deletion_protection = var.deletion_protection
}

# Route 53 のクエリログは、アカウント単位の resource policy（policy_name）が許す Log Group にしか書けない。
# 枠はリージョンに10個までの共有の資源で、この usecase が us-east-1 の1つを持つ（README「所有者」）。
resource "aws_cloudwatch_log_resource_policy" "this" {
  region      = local.region
  policy_name = "${var.name}-dns-query-logs"
  policy_document = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Sid       = "Route53QueryLogs"
      Effect    = "Allow"
      Principal = { Service = "route53.amazonaws.com" }
      Action    = ["logs:CreateLogStream", "logs:PutLogEvents"]
      Resource  = "${module.log_group.log_group_arn}:*"
      Condition = { StringEquals = { "aws:SourceAccount" = data.aws_caller_identity.current.account_id } }
    }]
  })
}

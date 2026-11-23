# 引数なしで、このアカウントで有効なリージョンを返す。宣言の集合の外のリージョンが有効なら、そこに設定が届かないので plan で止める。
data "aws_regions" "enabled" {
  lifecycle {
    postcondition {
      condition     = length(setsubtract(self.names, var.regions)) == 0
      error_message = "このアカウントで、account-baseline の宣言の外のリージョンが有効になっています（${join(", ", sort(setsubtract(self.names, var.regions)))}）。opt-in のリージョンは使えません（account-baseline/ADR-0101）。"
    }
  }
}

resource "aws_ebs_encryption_by_default" "this" {
  for_each = var.regions

  region  = each.key
  enabled = true
}

resource "aws_accessanalyzer_analyzer" "this" {
  for_each = var.regions

  region        = each.key
  analyzer_name = "${var.name_prefix}-external"
  type          = "ACCOUNT"
}

resource "aws_ssm_service_setting" "document_public_sharing" {
  for_each = var.regions

  region        = each.key
  setting_id    = "/ssm/documents/console/public-sharing-permission"
  setting_value = "Disable"
}

# 既定の Log Group（/aws/ssm/automation/executeScript）は、無ければ Automation が作る。その暗号化と保持日数は持たない（README の保証しないこと）。
resource "aws_ssm_service_setting" "automation_log_destination" {
  for_each = var.regions

  region        = each.key
  setting_id    = "/ssm/automation/customer-script-log-destination"
  setting_value = "CloudWatch"
}

resource "aws_emr_block_public_access_configuration" "this" {
  for_each = var.regions

  region                            = each.key
  block_public_security_group_rules = true

  permitted_public_security_group_rule_range {
    min_range = 22
    max_range = 22
  }
}

# インターネットゲートウェイを通る通信を、両向きとも止める（EC2.172）。例外の宣言（exclusion）は持たない（README の unsupported）。
resource "aws_vpc_block_public_access_options" "this" {
  for_each = var.regions

  region                      = each.key
  internet_gateway_block_mode = "block-bidirectional"
}

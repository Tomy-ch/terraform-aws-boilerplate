# kics-scan disable=e592a0c5-5bdb-414c-9066-5dba7cdea370
# e592a0c5（IAM Access Analyzer）: KICS はファイルごとに analyzer の宣言を求める。analyzer はアカウント単位の統制で account-baseline が持つ（docs/v1.0-plan/scope.md 第7節）。撤回: KICS がファイルの外の宣言を数えるようになったとき。

# 1つのアカウントに2つ宣言すると、互いに上書きして差分が出続ける（account-baseline/ADR-0001）。
resource "aws_s3_account_public_access_block" "this" {
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_account_alternate_contact" "security" {
  alternate_contact_type = "SECURITY"
  name                   = var.security_contact.name
  title                  = var.security_contact.title
  email_address          = var.security_contact.email_address
  phone_number           = var.security_contact.phone_number
}

# 値は organization-baseline の break-glass が管理アカウントに置くものと同じにする。
resource "aws_iam_account_password_policy" "this" {
  count = var.manage_password_policy ? 1 : 0

  minimum_password_length        = 14
  password_reuse_prevention      = 24
  require_lowercase_characters   = true
  require_uppercase_characters   = true
  require_numbers                = true
  require_symbols                = true
  allow_users_to_change_password = true
  max_password_age               = 90
  hard_expiry                    = false
}

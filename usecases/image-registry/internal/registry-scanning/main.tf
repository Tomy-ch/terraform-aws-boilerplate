# kics-scan disable=e592a0c5-5bdb-414c-9066-5dba7cdea370
# e592a0c5（IAM Access Analyzer）: アカウント単位の統制で account-baseline が持つ（docs/v1.0-plan/scope.md 第7節）。撤回: account-baseline を実装したとき。

# アカウント×リージョンに1つの設定。destroy すると BASIC（規則なし）へ戻る（image-registry/ADR-0301）。
resource "aws_ecr_registry_scanning_configuration" "this" {
  scan_type = "ENHANCED"

  rule {
    scan_frequency = "CONTINUOUS_SCAN"

    repository_filter {
      filter      = var.repository_filter
      filter_type = "WILDCARD"
    }
  }
}

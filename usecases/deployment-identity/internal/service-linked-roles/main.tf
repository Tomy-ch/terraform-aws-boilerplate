# kics-scan disable=e592a0c5-5bdb-414c-9066-5dba7cdea370,e38a8e0a-b88b-4902-b3fe-b0fcb17d5c10
# e592a0c5（IAM Access Analyzer）: KICS はファイルごとに analyzer の宣言を求める。analyzer はアカウント単位の統制で account-baseline が持つ（docs/v1.0-plan/scope.md 第7節）。撤回: KICS がファイルの外の宣言を数えるようになったとき。
# e38a8e0a（tag）: tag は共通の単一入力でだけ付ける（ADR-0204 決定5）。この module はその入力を持たず、tag の書き込みの権限も持たない（deployment-identity/ADR-0201）。撤回: 共通の tag 入力を配線したとき。

data "aws_partition" "current" {}

locals {
  # AWS が作成のときに付ける値と同じにする（deployment-identity/ADR-0002 決定3）。
  chatbot_description = "The Service Linked Role used by AWS Chatbot."
}

# workload root の usecase が前提にする service-linked role。partition が aws のときだけ作る（deployment-identity/ADR-0002 決定1）。
resource "aws_iam_service_linked_role" "chatbot" {
  count = data.aws_partition.current.partition == "aws" ? 1 : 0

  aws_service_name = "management.chatbot.amazonaws.com"
  description      = local.chatbot_description

  lifecycle {
    prevent_destroy = true
  }
}

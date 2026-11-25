# kics-scan disable=e592a0c5-5bdb-414c-9066-5dba7cdea370,e38a8e0a-b88b-4902-b3fe-b0fcb17d5c10
# e592a0c5（IAM Access Analyzer）: KICS はファイルごとに analyzer の宣言を求める。analyzer はアカウント単位の統制で account-baseline が持つ（docs/v1.0-plan/scope.md 第7節）。撤回: KICS がファイルの外の宣言を数えるようになったとき。
# e38a8e0a（tag）: tag は共通の単一入力でだけ付ける（ADR-0204 決定5）。この module はその入力を持たない。撤回: 共通の tag 入力を配線したとき。

locals {
  # メンバーアカウントの env と、それを置く OU。行を足すとアカウントが作られ、行を消す変更は prevent_destroy が止める。
  member_accounts = {
    security      = aws_organizations_organizational_unit.top["Security"].id
    "log-archive" = aws_organizations_organizational_unit.top["Security"].id
    dev           = aws_organizations_organizational_unit.workloads["NonProd"].id
    stg           = aws_organizations_organizational_unit.workloads["NonProd"].id
    prd           = aws_organizations_organizational_unit.workloads["Prod"].id
    sandbox       = aws_organizations_organizational_unit.top["Sandbox"].id
  }
}

# メールは repository に置かず、管理アカウントの parameter に人が置く（docs/setup/organization-baseline.md）。
data "aws_ssm_parameter" "member_account_emails" {
  name = "/${var.project}/organization-baseline/member-account-emails"
}

locals {
  member_account_emails = jsondecode(data.aws_ssm_parameter.member_account_emails.value)
}

# role_name・iam_user_access_to_billing・close_on_deletion は書かず、既定（OrganizationAccountAccessRole・ALLOW・閉じない）に任せる。
# Read はこの3つを state に書かないので、書くと import の後に作り直しの差分が出る。
resource "aws_organizations_account" "this" {
  for_each = local.member_accounts

  name      = "${var.project}-${each.key}"
  email     = local.member_account_emails[each.key]
  parent_id = each.value

  # root の資格情報の中央管理を有効にしてから作る。後から有効にしても、作成時の root の資格情報は残る。
  depends_on = [aws_iam_organizations_features.this]

  lifecycle {
    prevent_destroy = true

    precondition {
      condition     = can(tomap(local.member_account_emails)) && toset(keys(local.member_account_emails)) == toset(keys(local.member_accounts))
      error_message = "parameter /<project>/organization-baseline/member-account-emails の JSON の key が、メンバーアカウントの env の一覧と一致しません。"
    }

    precondition {
      condition     = can(regex("^[^@\\s]+@[^@\\s]+\\.[^@\\s]+$", local.member_account_emails[each.key]))
      error_message = "parameter /<project>/organization-baseline/member-account-emails の値がメールの形ではありません。"
    }
  }
}

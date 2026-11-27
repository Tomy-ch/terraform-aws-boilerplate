# インスタンスは人が有効にする（organization-baseline/ADR-0001 決定4）。0件（未有効か別のリージョン）と2件以上を plan で落とす。
data "aws_ssoadmin_instances" "this" {
  lifecycle {
    postcondition {
      condition     = length(self.arns) == 1 && length(self.identity_store_ids) == 1
      error_message = "provider のリージョンに IAM Identity Center のインスタンスがちょうど1つではありません（docs/setup/organization-baseline.md 3.3）。"
    }
  }
}

locals {
  instance_arn = one(data.aws_ssoadmin_instances.this.arns)

  # 閉じた一覧（organization-baseline/ADR-0204 決定1）。
  permission_sets = {
    read_only          = { policy = "ReadOnlyAccess", session = "PT8H" }
    terraform_plan     = { policy = "ReadOnlyAccess", session = "PT1H" }
    billing            = { policy = "job-function/Billing", session = "PT1H" }
    support            = { policy = "AWSSupportAccess", session = "PT1H" }
    organization_admin = { policy = "AdministratorAccess", session = "PT1H" }
  }

  iam_arn_prefix = "arn:${var.partition}:iam::${var.management_account_id}"

  # 誤操作を止める Deny で、境界ではない（organization-baseline/ADR-0204 決定3）。
  organization_admin_denies = [
    { Sid = "DenyAssumeRoot", Effect = "Deny", Action = "sts:AssumeRoot", Resource = "*" },
    {
      Sid       = "DenyCiIdentityChange"
      Effect    = "Deny"
      NotAction = ["iam:Get*", "iam:List*"]
      Resource = [
        "${local.iam_arn_prefix}:role/${var.project}-admin-ci-*",
        "${local.iam_arn_prefix}:policy/${var.project}-admin-ci-*",
        "${local.iam_arn_prefix}:oidc-provider/token.actions.githubusercontent.com",
      ]
    },
    {
      Sid       = "DenyBootstrapBucketChange"
      Effect    = "Deny"
      NotAction = ["s3:Get*", "s3:List*"]
      Resource  = flatten([for b in var.bootstrap_bucket_arns : [b, "${b}/*"]])
    },
    { Sid = "DenyStateKeyChange", Effect = "Deny", NotAction = ["kms:Describe*", "kms:Get*", "kms:List*"], Resource = var.state_key_arn },
  ]
}

# 名前は ForceNew で、変えると全割当の role が作り直される（organization-baseline/ADR-0204 決定2。消されないように決定6）。
resource "aws_ssoadmin_permission_set" "this" {
  for_each = local.permission_sets

  instance_arn     = local.instance_arn
  name             = "${var.project}-${replace(each.key, "_", "-")}"
  session_duration = each.value.session

  lifecycle {
    prevent_destroy = true
  }
}

resource "aws_ssoadmin_managed_policy_attachments_exclusive" "this" {
  for_each = local.permission_sets

  instance_arn        = local.instance_arn
  permission_set_arn  = aws_ssoadmin_permission_set.this[each.key].arn
  managed_policy_arns = ["arn:${var.partition}:iam::aws:policy/${each.value.policy}"]
}

resource "aws_ssoadmin_permission_set_inline_policy" "organization_admin" {
  instance_arn       = local.instance_arn
  permission_set_arn = aws_ssoadmin_permission_set.this["organization_admin"].arn
  inline_policy      = jsonencode({ Version = "2012-10-17", Statement = local.organization_admin_denies })
}

# 主体の ID は repository に置かず、管理アカウントの parameter に人が置く（docs/setup/organization-baseline.md 3.3）。
data "aws_ssm_parameter" "principals" {
  name = "/${var.project}/organization-baseline/identity-center-principals"

  lifecycle {
    postcondition {
      condition = (
        can(jsondecode(self.value))
        && can(keys(tomap(jsondecode(self.value).users)))
        && can(keys(tomap(jsondecode(self.value).groups)))
        && try(toset(keys(jsondecode(self.value))) == toset(["groups", "users"]), false)
      )
      error_message = "parameter /<project>/organization-baseline/identity-center-principals が、users と groups だけを key に持つ JSON の object ではありません。"
    }

    # Provider が principal_id に課す形と同じ。形の誤りは最初の postcondition が落とすので、以降は読めないときに通す。
    postcondition {
      condition = try(alltrue([
        for k, v in tomap(jsondecode(self.value).users) :
        can(regex("^[a-z][a-z0-9-]{0,31}$", k)) && can(regex("^([0-9a-f]{10}-|)[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}$", v))
      ]), true)
      error_message = "parameter /<project>/organization-baseline/identity-center-principals の users の label か user ID の形が違います。"
    }

    postcondition {
      condition = try(alltrue([
        for k, v in tomap(jsondecode(self.value).groups) :
        can(regex("^[a-z][a-z0-9-]{0,31}$", k)) && can(regex("^([0-9a-f]{10}-|)[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}$", v))
      ]), true)
      error_message = "parameter /<project>/organization-baseline/identity-center-principals の groups の label か group ID の形が違います。"
    }

    postcondition {
      condition     = try(length(setintersection(keys(tomap(jsondecode(self.value).users)), keys(tomap(jsondecode(self.value).groups)))) == 0, true)
      error_message = "parameter /<project>/organization-baseline/identity-center-principals の users と groups に同じ label があります。"
    }

    postcondition {
      condition = try(
        alltrue([for a in var.management_assignments : contains(keys(tomap(jsondecode(self.value).users)), a.user)])
        && alltrue([for a in var.member_assignments : contains(keys(tomap(jsondecode(self.value).groups)), a.group)]),
        true
      )
      error_message = "割当が名指す user か group の label が、parameter /<project>/organization-baseline/identity-center-principals にありません。"
    }
  }
}

locals {
  principals = jsondecode(data.aws_ssm_parameter.principals.value)
}

# key は人が付けた label。ID を address に載せない（organization-baseline/ADR-0204 決定8）。
resource "aws_ssoadmin_account_assignment" "management" {
  for_each = { for a in var.management_assignments : "${a.permission_set}/${a.user}" => a }

  instance_arn       = local.instance_arn
  permission_set_arn = aws_ssoadmin_permission_set.this[each.value.permission_set].arn
  principal_id       = local.principals.users[each.value.user]
  principal_type     = "USER"
  target_id          = var.management_account_id
  target_type        = "AWS_ACCOUNT"

  # 割当が作る role に、Deny と managed policy が先に入っているようにする。
  depends_on = [aws_ssoadmin_permission_set_inline_policy.organization_admin, aws_ssoadmin_managed_policy_attachments_exclusive.this]
}

resource "aws_ssoadmin_account_assignment" "member" {
  for_each = { for a in var.member_assignments : "${a.account}/${a.permission_set}/${a.group}" => a }

  instance_arn       = local.instance_arn
  permission_set_arn = aws_ssoadmin_permission_set.this[each.value.permission_set].arn
  principal_id       = local.principals.groups[each.value.group]
  principal_type     = "GROUP"
  target_id          = lookup(var.member_account_ids, each.value.account, null)
  target_type        = "AWS_ACCOUNT"

  depends_on = [aws_ssoadmin_permission_set_inline_policy.organization_admin, aws_ssoadmin_managed_policy_attachments_exclusive.this]

  lifecycle {
    precondition {
      condition     = contains(keys(var.member_account_ids), each.value.account)
      error_message = "member_assignments の account は、メンバーアカウントの対応表の env にしてください。"
    }
  }
}

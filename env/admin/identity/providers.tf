provider "aws" {
  region = "ap-northeast-1"
}

# 立ち上げの中のメンバーアカウントの provider（organization-baseline/ADR-0203、deployment-identity/ADR-0201 決定22）。
# 引き受けてよい主体は、管理アカウントの IAM の Allow とメンバーの role の信頼条件が限る。
data "aws_caller_identity" "current" {}

data "aws_partition" "current" {}

data "aws_iam_session_context" "current" {
  arn = data.aws_caller_identity.current.arn
}

locals {
  bootstrap_role = (
    data.aws_iam_session_context.current.issuer_name == "tabp-admin-ci-apply-identity"
    ? "role/OrganizationAccountAccessRole"
    : "role${module.organization_baseline.bootstrap_read_role.path}${module.organization_baseline.bootstrap_read_role.name}"
  )
  bootstrap_role_arns = {
    for env, id in module.organization_baseline.bootstrapping_account_ids :
    env => "arn:${data.aws_partition.current.partition}:iam::${id}:${local.bootstrap_role}"
  }
}

provider "aws" {
  alias  = "security"
  region = "ap-northeast-1"

  assume_role {
    role_arn = local.bootstrap_role_arns["security"]
  }
}

provider "aws" {
  alias  = "log_archive"
  region = "ap-northeast-1"

  assume_role {
    role_arn = local.bootstrap_role_arns["log-archive"]
  }
}

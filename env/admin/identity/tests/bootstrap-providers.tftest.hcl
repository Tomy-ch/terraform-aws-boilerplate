# 立ち上げの中のメンバーの provider が引き受ける role を、呼び出し元の session の発行元で選ぶ分岐を固定する。
mock_provider "aws" {
  mock_data "aws_ssm_parameter" {
    defaults = { value = "{\"security\":\"security@example.com\",\"log-archive\":\"log-archive@example.com\"}" }
  }
  mock_data "aws_caller_identity" {
    defaults = { account_id = "123456789012", arn = "arn:aws:sts::123456789012:assumed-role/tabp-admin-ci-plan/session" }
  }
  mock_data "aws_iam_session_context" {
    defaults = { issuer_arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan" }
  }
  mock_data "aws_region" {
    defaults = { region = "ap-northeast-1" }
  }
  mock_data "aws_partition" {
    defaults = { partition = "aws" }
  }
  # 子の OU の parent_id の検査を通す形にする。
  mock_resource "aws_organizations_organization" {
    defaults = {
      id                = "o-exampleorg1"
      master_account_id = "123456789012"
      roots             = [{ id = "r-exam", arn = "arn:aws:organizations::123456789012:root/o-exampleorg1/r-exam", name = "Root", policy_types = [] }]
    }
  }
  mock_resource "aws_organizations_organizational_unit" {
    defaults = { id = "ou-exam-abcdefgh" }
  }
}

mock_provider "aws" {
  alias = "security"

  mock_data "aws_caller_identity" {
    defaults = { account_id = "111111111111" }
  }
  mock_data "aws_region" {
    defaults = { region = "ap-northeast-1" }
  }
  mock_data "aws_partition" {
    defaults = { partition = "aws" }
  }
}

mock_provider "aws" {
  alias = "log_archive"

  mock_data "aws_caller_identity" {
    defaults = { account_id = "222222222222" }
  }
  mock_data "aws_region" {
    defaults = { region = "ap-northeast-1" }
  }
  mock_data "aws_partition" {
    defaults = { partition = "aws" }
  }
}

# state の鍵の Key Policy の precondition（同じアカウントの role）を plan で評価させるため、role の ARN を既知の値にする。
override_resource {
  target          = module.deployment_identity.module.github_oidc.aws_iam_role.this["plan"]
  override_during = plan
  values          = { arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan" }
}

override_resource {
  target          = module.deployment_identity.module.github_oidc.aws_iam_role.apply_identity
  override_during = plan
  values          = { arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity" }
}

override_resource {
  target          = module.deployment_identity.module.github_oidc.aws_iam_role.this["unlock_identity"]
  override_during = plan
  values          = { arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock-identity" }
}

# state Bucket は import の取り込み先なので override で与える。出力の Bucket は、deployment_identity の
# plan_output_bucket_arn の validation を通すため、ARN だけ形のある値にする。
override_resource {
  target = module.state_backend.module.s3.aws_s3_bucket.this
  values = { arn = "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an" }
}

override_resource {
  target = module.state_backend.module.plan_output.aws_s3_bucket.this
  values = { arn = "arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an" }
}

override_resource {
  target = module.state_backend.module.s3.aws_s3_bucket_public_access_block.this
}

override_resource {
  target = module.state_backend.module.s3.aws_s3_bucket_ownership_controls.this
}

override_resource {
  target = module.state_backend.module.s3.aws_s3_bucket_versioning.this
}

override_resource {
  target          = module.organization_baseline.module.organization.aws_organizations_account.this["security"]
  override_during = plan
  values          = { id = "111111111111" }
}

override_resource {
  target          = module.organization_baseline.module.organization.aws_organizations_account.this["log-archive"]
  override_during = plan
  values          = { id = "222222222222" }
}

run "apply_identity_assumes_organization_access_role" {
  command = plan

  override_data {
    target = data.aws_iam_session_context.current
    values = { issuer_arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity" }
  }

  assert {
    condition = local.bootstrap_role_arns == {
      security      = "arn:aws:iam::111111111111:role/OrganizationAccountAccessRole"
      "log-archive" = "arn:aws:iam::222222222222:role/OrganizationAccountAccessRole"
    }
    error_message = "apply-identity の role の session で、メンバーの provider がそのアカウントの OrganizationAccountAccessRole を引き受けていません。"
  }
}

run "plan_assumes_bootstrap_read_role" {
  command = plan

  assert {
    condition = local.bootstrap_role_arns == {
      security      = "arn:aws:iam::111111111111:role/bootstrap/tabp-bootstrap-read"
      "log-archive" = "arn:aws:iam::222222222222:role/bootstrap/tabp-bootstrap-read"
    }
    error_message = "plan の role の session で、メンバーの provider がそのアカウントの立ち上げの読み取りの role を引き受けていません。"
  }
}

# apply-identity の role と名前の先頭が同じ role や、role の session でない主体（発行元を持たない）が、
# OrganizationAccountAccessRole へ落ちないことを見る。
run "near_name_assumes_bootstrap_read_role" {
  command = plan

  override_data {
    target = data.aws_iam_session_context.current
    values = { issuer_arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply" }
  }

  assert {
    condition     = local.bootstrap_role_arns["security"] == "arn:aws:iam::111111111111:role/bootstrap/tabp-bootstrap-read"
    error_message = "apply-identity の role でない session で、メンバーの provider が立ち上げの読み取りの role のほかを引き受けています。"
  }
}

run "other_principal_assumes_bootstrap_read_role" {
  command = plan

  override_data {
    target = data.aws_iam_session_context.current
    values = { issuer_arn = "" }
  }

  assert {
    condition     = local.bootstrap_role_arns["security"] == "arn:aws:iam::111111111111:role/bootstrap/tabp-bootstrap-read"
    error_message = "発行元を持たない主体で、メンバーの provider が立ち上げの読み取りの role のほかを引き受けています。"
  }
}

run "member_modules_use_member_accounts" {
  command = plan

  assert {
    condition = local.member_break_glass_arns == {
      security      = "arn:aws:iam::111111111111:role/break-glass/tabp-break-glass"
      "log-archive" = "arn:aws:iam::222222222222:role/break-glass/tabp-break-glass"
    }
    error_message = "メンバーの deployment-identity に渡す break-glass の ARN が、そのメンバーのアカウントの role の ARN になっていません。"
  }

  assert {
    condition     = module.state_backend_security.bucket_name == "tabp-security-tfstate-111111111111-ap-northeast-1-an" && module.state_backend_log_archive.bucket_name == "tabp-log-archive-tfstate-222222222222-ap-northeast-1-an"
    error_message = "メンバーの state-backend が、その env とメンバーのアカウントの provider で呼ばれていません。"
  }
}

mock_provider "aws" {
  mock_data "aws_caller_identity" {
    defaults = { account_id = "111111111111" }
  }
  mock_data "aws_partition" {
    defaults = { partition = "aws" }
  }
  mock_data "aws_region" {
    defaults = { region = "ap-northeast-1" }
  }
}

variables {
  role_name = "tabp-break-glass"
  path      = "/break-glass/"
}

override_resource {
  target          = aws_iam_user.this["break-glass-1"]
  override_during = apply
  values          = { arn = "arn:aws:iam::111111111111:user/break-glass/break-glass-1" }
}

override_resource {
  target          = aws_iam_user.this["break-glass-2"]
  override_during = apply
  values          = { arn = "arn:aws:iam::111111111111:user/break-glass/break-glass-2" }
}

run "users" {
  command = apply

  assert {
    condition     = toset(keys(aws_iam_user.this)) == toset(["break-glass-1", "break-glass-2"])
    error_message = "break-glass の IAM user は個人を表さない名前の2つです。"
  }

  assert {
    condition     = alltrue([for u in aws_iam_user.this : u.path == "/break-glass/"])
    error_message = "break-glass の user は break-glass の path に置いてください。"
  }

  assert {
    condition     = aws_iam_group_membership.this.users == toset(["break-glass-1", "break-glass-2"])
    error_message = "break-glass の user は group の policy で権限を持ちます（user に policy を直接付けない）。"
  }
}

run "group_policy" {
  command = apply

  assert {
    condition = jsondecode(aws_iam_group_policy.this.policy).Statement[0] == {
      Sid       = "AssumeBreakGlassRole"
      Effect    = "Allow"
      Action    = "sts:AssumeRole"
      Resource  = "arn:aws:iam::*:role/break-glass/tabp-break-glass"
      Condition = { Bool = { "aws:MultiFactorAuthPresent" = "true" } }
    }
    error_message = "break-glass の user が引き受けてよいのは、MFA で認証した後の各アカウントの break-glass の role だけです。"
  }

  assert {
    condition = jsondecode(aws_iam_group_policy.this.policy).Statement[1] == {
      Sid      = "ManageOwnPasswordAndMfa"
      Effect   = "Allow"
      Action   = ["iam:ChangePassword", "iam:EnableMFADevice", "iam:GetUser", "iam:ListMFADevices", "iam:ResyncMFADevice"]
      Resource = "arn:aws:iam::111111111111:user/break-glass/$${aws:username}"
    }
    error_message = "パスワードと MFA の管理は自分の user に限ってください。"
  }

  assert {
    condition = [for s in jsondecode(aws_iam_group_policy.this.policy).Statement : s.Action if s.Effect == "Deny"] == [
      ["iam:CreateAccessKey", "iam:CreateServiceSpecificCredential", "iam:UploadSSHPublicKey", "iam:UploadSigningCertificate"],
    ]
    error_message = "break-glass の user には、アクセスキーなど長期の資格情報を作ることを拒んでください。"
  }

  assert {
    condition     = length([for s in jsondecode(aws_iam_group_policy.this.policy).Statement : s if s.Effect == "Allow"]) == 4
    error_message = "break-glass の user の Allow は、role の引き受け、自分のパスワードと MFA の管理、password policy の読み取りだけです。"
  }

  assert {
    condition = jsondecode(aws_iam_group_policy.this.policy).Statement[2] == {
      Sid       = "DeactivateOwnMfaWithMfa"
      Effect    = "Allow"
      Action    = "iam:DeactivateMFADevice"
      Resource  = "arn:aws:iam::111111111111:user/break-glass/$${aws:username}"
      Condition = { Bool = { "aws:MultiFactorAuthPresent" = "true" } }
    }
    error_message = "自分の MFA を外すことは、MFA で認証したセッションに限ってください。"
  }
}

run "role" {
  command = apply

  assert {
    condition = jsondecode(aws_iam_role.this.assume_role_policy).Statement == [{
      Effect    = "Allow"
      Principal = { AWS = ["arn:aws:iam::111111111111:user/break-glass/break-glass-1", "arn:aws:iam::111111111111:user/break-glass/break-glass-2"] }
      Action    = "sts:AssumeRole"
      Condition = { Bool = { "aws:MultiFactorAuthPresent" = "true" } }
    }]
    error_message = "break-glass の role を引き受けられるのは、MFA で認証した break-glass の user だけです。"
  }

  assert {
    condition     = aws_iam_role_policy_attachments_exclusive.this.policy_arns == toset(["arn:aws:iam::aws:policy/AdministratorAccess"])
    error_message = "break-glass の role に付けるのは AdministratorAccess だけです（ADR-0301 決定19）。"
  }

  assert {
    condition     = aws_iam_role_policy_attachment.administrator_access.role == aws_iam_role.this.name && aws_iam_role_policy_attachment.administrator_access.policy_arn == "arn:aws:iam::aws:policy/AdministratorAccess"
    error_message = "AdministratorAccess を break-glass の role に付けるのが、attachment になっていません。"
  }

  assert {
    condition     = aws_iam_role.this.max_session_duration == 3600
    error_message = "break-glass の role のセッションは1時間です。"
  }
}

run "member_stack_set" {
  command = plan

  assert {
    condition = (
      aws_cloudformation_stack_set.this.name == "tabp-break-glass" &&
      aws_cloudformation_stack_set.this.permission_model == "SERVICE_MANAGED" &&
      aws_cloudformation_stack_set.this.capabilities == toset(["CAPABILITY_NAMED_IAM"]) &&
      aws_cloudformation_stack_set.this.auto_deployment[0].enabled &&
      aws_cloudformation_stack_set.this.managed_execution[0].active &&
      !aws_cloudformation_stack_set.this.auto_deployment[0].retain_stacks_on_account_removal &&
      aws_cloudformation_stack_set.this.call_as == null &&
      aws_cloudformation_stack_set.this.administration_role_arn == null &&
      length(coalesce(aws_cloudformation_stack_set.this.tags, {})) == 0
    )
    error_message = "メンバーの break-glass の StackSet は、break-glass の role と同じ名前の service-managed で、新しいアカウントへ自動で置き、tag を付けないものにしてください。"
  }

  assert {
    condition = jsondecode(aws_cloudformation_stack_set.this.template_body).Resources == {
      BreakGlassRole = {
        Type = "AWS::IAM::Role"
        Properties = {
          RoleName           = "tabp-break-glass"
          Path               = "/break-glass/"
          MaxSessionDuration = 3600
          ManagedPolicyArns  = ["arn:aws:iam::aws:policy/AdministratorAccess"]
          AssumeRolePolicyDocument = {
            Version = "2012-10-17"
            Statement = [{
              Effect    = "Allow"
              Principal = { AWS = ["arn:aws:iam::111111111111:user/break-glass/break-glass-1", "arn:aws:iam::111111111111:user/break-glass/break-glass-2"] }
              Action    = "sts:AssumeRole"
              Condition = { Bool = { "aws:MultiFactorAuthPresent" = "true" } }
            }]
          }
        }
      }
    }
    error_message = "メンバーの break-glass の role は、管理アカウントの break-glass の user と MFA だけを信頼し、AdministratorAccess だけを持つ1つの role にしてください。"
  }
}

# 管理アカウントの role と別に書いた文書が、ずれていないことを見る。
run "member_role_matches_management_role" {
  command = apply

  assert {
    condition = (
      jsondecode(aws_cloudformation_stack_set.this.template_body).Resources.BreakGlassRole.Properties.AssumeRolePolicyDocument == jsondecode(aws_iam_role.this.assume_role_policy) &&
      jsondecode(aws_cloudformation_stack_set.this.template_body).Resources.BreakGlassRole.Properties.RoleName == aws_iam_role.this.name &&
      jsondecode(aws_cloudformation_stack_set.this.template_body).Resources.BreakGlassRole.Properties.Path == aws_iam_role.this.path &&
      jsondecode(aws_cloudformation_stack_set.this.template_body).Resources.BreakGlassRole.Properties.MaxSessionDuration == aws_iam_role.this.max_session_duration &&
      toset(jsondecode(aws_cloudformation_stack_set.this.template_body).Resources.BreakGlassRole.Properties.ManagedPolicyArns) == toset(aws_iam_role_policy_attachments_exclusive.this.policy_arns)
    )
    error_message = "メンバーの break-glass の role が、管理アカウントの break-glass の role と同じ名前・path・信頼条件・権限になっていません。"
  }
}

run "password_policy" {
  command = plan

  assert {
    condition = (
      aws_iam_account_password_policy.this.max_password_age == 90 &&
      aws_iam_account_password_policy.this.hard_expiry == false &&
      aws_iam_account_password_policy.this.allow_users_to_change_password
    )
    error_message = "password は90日で失効し、失効した user が自分で変えられること（hard_expiry を有効にしない。organization-baseline/ADR-0201 決定5）。"
  }

  assert {
    condition = (
      aws_iam_account_password_policy.this.minimum_password_length == 14 &&
      aws_iam_account_password_policy.this.password_reuse_prevention == 24 &&
      aws_iam_account_password_policy.this.require_lowercase_characters &&
      aws_iam_account_password_policy.this.require_uppercase_characters &&
      aws_iam_account_password_policy.this.require_numbers &&
      aws_iam_account_password_policy.this.require_symbols
    )
    error_message = "password policy は14文字以上、24世代の再利用の禁止、4種の文字を求めてください（Security Hub IAM.7・IAM.15・IAM.16）。"
  }
}

run "arns_known_at_plan" {
  command = plan
  # 前の apply の run の state を引き継ぐと、作成済みの user の ARN が既知になり、resource の属性への退行を見逃す。
  state_key = "arns_known_at_plan"

  assert {
    condition = output.arns == [
      "arn:aws:iam::111111111111:user/break-glass/break-glass-1",
      "arn:aws:iam::111111111111:user/break-glass/break-glass-2",
      "arn:aws:iam::111111111111:group/break-glass/break-glass",
      "arn:aws:iam::111111111111:role/break-glass/tabp-break-glass",
    ]
    error_message = "break-glass の ARN が、plan の時点で path と名前から組んだ値になっていません。"
  }
}

run "arns_match_resources" {
  command = apply

  assert {
    condition = output.arns == concat(
      [for n in sort(keys(aws_iam_user.this)) : "arn:aws:iam::111111111111:user${aws_iam_user.this[n].path}${aws_iam_user.this[n].name}"],
      [
        "arn:aws:iam::111111111111:group${aws_iam_group.this.path}${aws_iam_group.this.name}",
        "arn:aws:iam::111111111111:role${aws_iam_role.this.path}${aws_iam_role.this.name}",
      ],
    )
    error_message = "break-glass の ARN が、作った user・group・role の path と名前を指していません。"
  }

  assert {
    condition     = length(output.arns) == length(aws_iam_user.this) + 2
    error_message = "break-glass の ARN は、作った user のすべてと group・role の分だけを含めてください。"
  }
}

run "path_without_leading_slash" {
  command = plan

  variables {
    path = "break-glass/"
  }

  expect_failures = [var.path]
}

run "path_without_trailing_slash" {
  command = plan

  variables {
    path = "/break-glass"
  }

  expect_failures = [var.path]
}

run "path_empty" {
  command = plan

  variables {
    path = ""
  }

  expect_failures = [var.path]
}

mock_provider "aws" {
  mock_data "aws_ssoadmin_instances" {
    defaults = { arns = ["arn:aws:sso:::instance/ssoins-0000000000000000"], identity_store_ids = ["d-0000000000"] }
  }
  mock_data "aws_ssm_parameter" {
    defaults = { value = "{\"users\":{\"owner\":\"00000000-0000-0000-0000-000000000001\"},\"groups\":{\"operators\":\"0000000000-00000000-0000-0000-0000-000000000002\"}}" }
  }
}

variables {
  project               = "tabp"
  partition             = "aws"
  management_account_id = "111111111111"
  member_account_ids    = { security = "222222222222", "log-archive" = "333333333333" }
  management_assignments = [
    { permission_set = "organization_admin", user = "owner" },
    { permission_set = "read_only", user = "owner" },
  ]
  member_assignments = [
    { account = "security", permission_set = "read_only", group = "operators" },
    { account = "log-archive", permission_set = "support", group = "operators" },
  ]
  bootstrap_bucket_arns = [
    "arn:aws:s3:::tabp-admin-tfstate-111111111111-ap-northeast-1-an",
    "arn:aws:s3:::tabp-admin-tfplan-111111111111-ap-northeast-1-an",
    "arn:aws:s3:::tabp-admin-tfapply-111111111111-ap-northeast-1-an",
  ]
  state_key_arn = "arn:aws:kms:ap-northeast-1:111111111111:key/00000000-0000-0000-0000-000000000000"
}

# 割当と付与がどの permission set を指すかを見分けるため、ARN を permission set ごとに変える。
override_resource {
  target = aws_ssoadmin_permission_set.this["read_only"]
  values = { arn = "arn:aws:sso:::permissionSet/ssoins-0000000000000000/ps-000000000000000a" }
}

override_resource {
  target = aws_ssoadmin_permission_set.this["terraform_plan"]
  values = { arn = "arn:aws:sso:::permissionSet/ssoins-0000000000000000/ps-000000000000000b" }
}

override_resource {
  target = aws_ssoadmin_permission_set.this["billing"]
  values = { arn = "arn:aws:sso:::permissionSet/ssoins-0000000000000000/ps-000000000000000c" }
}

override_resource {
  target = aws_ssoadmin_permission_set.this["support"]
  values = { arn = "arn:aws:sso:::permissionSet/ssoins-0000000000000000/ps-000000000000000d" }
}

override_resource {
  target = aws_ssoadmin_permission_set.this["organization_admin"]
  values = { arn = "arn:aws:sso:::permissionSet/ssoins-0000000000000000/ps-000000000000000e" }
}

run "permission_sets" {
  command = apply

  assert {
    condition = { for k, v in aws_ssoadmin_permission_set.this : k => [v.name, v.session_duration, v.instance_arn] } == {
      read_only          = ["tabp-read-only", "PT8H", "arn:aws:sso:::instance/ssoins-0000000000000000"]
      terraform_plan     = ["tabp-terraform-plan", "PT1H", "arn:aws:sso:::instance/ssoins-0000000000000000"]
      billing            = ["tabp-billing", "PT1H", "arn:aws:sso:::instance/ssoins-0000000000000000"]
      support            = ["tabp-support", "PT1H", "arn:aws:sso:::instance/ssoins-0000000000000000"]
      organization_admin = ["tabp-organization-admin", "PT1H", "arn:aws:sso:::instance/ssoins-0000000000000000"]
    }
    error_message = "permission set は閉じた一覧の5つで、名前・セッションの長さ・インスタンスが organization-baseline/ADR-0204 決定1・2 と違います。"
  }

  assert {
    condition     = alltrue([for v in aws_ssoadmin_permission_set.this : v.relay_state == null && v.description == null && v.tags == null])
    error_message = "permission set に relay_state・description・tag を書かないでください。"
  }

  assert {
    condition     = output.instance_arn == "arn:aws:sso:::instance/ssoins-0000000000000000"
    error_message = "instance_arn が、読んだインスタンスの ARN を返していません。"
  }

  assert {
    condition     = data.aws_ssm_parameter.principals.name == "/tabp/organization-baseline/identity-center-principals"
    error_message = "主体の parameter の名前が /<project>/organization-baseline/identity-center-principals ではありません。"
  }
}

run "managed_policies" {
  command = apply

  assert {
    condition = { for k, v in aws_ssoadmin_managed_policy_attachments_exclusive.this : k => [v.permission_set_arn, v.managed_policy_arns] } == {
      read_only          = [aws_ssoadmin_permission_set.this["read_only"].arn, toset(["arn:aws:iam::aws:policy/ReadOnlyAccess"])]
      terraform_plan     = [aws_ssoadmin_permission_set.this["terraform_plan"].arn, toset(["arn:aws:iam::aws:policy/ReadOnlyAccess"])]
      billing            = [aws_ssoadmin_permission_set.this["billing"].arn, toset(["arn:aws:iam::aws:policy/job-function/Billing"])]
      support            = [aws_ssoadmin_permission_set.this["support"].arn, toset(["arn:aws:iam::aws:policy/AWSSupportAccess"])]
      organization_admin = [aws_ssoadmin_permission_set.this["organization_admin"].arn, toset(["arn:aws:iam::aws:policy/AdministratorAccess"])]
    }
    error_message = "各 permission set に、閉じた一覧の AWS managed policy を1つだけ排他で付けてください。"
  }
}

run "organization_admin_denies" {
  command = apply

  assert {
    condition     = aws_ssoadmin_permission_set_inline_policy.organization_admin.permission_set_arn == aws_ssoadmin_permission_set.this["organization_admin"].arn
    error_message = "inline policy は organization_admin の permission set にだけ付けてください。"
  }

  assert {
    condition = jsondecode(aws_ssoadmin_permission_set_inline_policy.organization_admin.inline_policy) == {
      Version = "2012-10-17"
      Statement = [
        { Sid = "DenyAssumeRoot", Effect = "Deny", Action = "sts:AssumeRoot", Resource = "*" },
        {
          Sid       = "DenyCiIdentityChange"
          Effect    = "Deny"
          NotAction = ["iam:Get*", "iam:List*"]
          Resource = [
            "arn:aws:iam::111111111111:role/tabp-admin-ci-*",
            "arn:aws:iam::111111111111:policy/tabp-admin-ci-*",
            "arn:aws:iam::111111111111:oidc-provider/token.actions.githubusercontent.com",
          ]
        },
        {
          Sid       = "DenyBootstrapBucketChange"
          Effect    = "Deny"
          NotAction = ["s3:Get*", "s3:List*"]
          Resource = [
            "arn:aws:s3:::tabp-admin-tfstate-111111111111-ap-northeast-1-an",
            "arn:aws:s3:::tabp-admin-tfstate-111111111111-ap-northeast-1-an/*",
            "arn:aws:s3:::tabp-admin-tfplan-111111111111-ap-northeast-1-an",
            "arn:aws:s3:::tabp-admin-tfplan-111111111111-ap-northeast-1-an/*",
            "arn:aws:s3:::tabp-admin-tfapply-111111111111-ap-northeast-1-an",
            "arn:aws:s3:::tabp-admin-tfapply-111111111111-ap-northeast-1-an/*",
          ]
        },
        {
          Sid       = "DenyStateKeyChange"
          Effect    = "Deny"
          NotAction = ["kms:Describe*", "kms:Get*", "kms:List*"]
          Resource  = "arn:aws:kms:ap-northeast-1:111111111111:key/00000000-0000-0000-0000-000000000000"
        },
      ]
    }
    error_message = "organization_admin の inline policy が、AssumeRoot と bootstrap の構成物の書き込みを拒む Deny だけになっていません（organization-baseline/ADR-0204 決定3）。"
  }
}

# 入力の境界（project 13文字、最も長い partition、Bucket 名 63文字）で、permission set の inline policy の上限（空白を除いて
# 10,240 文字）に収まり、Deny を足す余地を残す。
run "inline_policy_length" {
  command = apply

  variables {
    project               = "abcdefghijklm"
    partition             = "aws-us-gov"
    management_account_id = "999999999999"
    bootstrap_bucket_arns = [
      "arn:aws-us-gov:s3:::${join("", [for i in range(63) : "a"])}",
      "arn:aws-us-gov:s3:::${join("", [for i in range(63) : "b"])}",
      "arn:aws-us-gov:s3:::${join("", [for i in range(63) : "c"])}",
    ]
    state_key_arn = "arn:aws-us-gov:kms:us-isof-south-1:999999999999:key/00000000-0000-0000-0000-000000000000"
  }

  assert {
    condition     = length(aws_ssoadmin_permission_set_inline_policy.organization_admin.inline_policy) <= 10240
    error_message = "organization_admin の inline policy が、入力の境界で ${length(aws_ssoadmin_permission_set_inline_policy.organization_admin.inline_policy)} 文字になり、上限の 10,240 文字を超えます。"
  }

  assert {
    condition     = length(aws_ssoadmin_permission_set_inline_policy.organization_admin.inline_policy) <= 8000
    error_message = "organization_admin の inline policy が、入力の境界で ${length(aws_ssoadmin_permission_set_inline_policy.organization_admin.inline_policy)} 文字になり、上限の 10,240 文字までの余地が 2,240 文字を切りました。"
  }

  assert {
    condition     = alltrue([for v in aws_ssoadmin_managed_policy_attachments_exclusive.this : alltrue([for a in v.managed_policy_arns : startswith(a, "arn:aws-us-gov:iam::aws:policy/")])])
    error_message = "AWS managed policy の ARN を、渡した partition で組んでください。"
  }

  assert {
    condition = output.organization_admin_deny_resources.DenyCiIdentityChange == [
      "arn:aws-us-gov:iam::999999999999:role/abcdefghijklm-admin-ci-*",
      "arn:aws-us-gov:iam::999999999999:policy/abcdefghijklm-admin-ci-*",
      "arn:aws-us-gov:iam::999999999999:oidc-provider/token.actions.githubusercontent.com",
    ]
    error_message = "CI の role・policy・OIDC provider の ARN を、渡した partition・管理アカウント・project で組んでください。"
  }
}

run "assignments" {
  command = apply

  assert {
    condition = { for k, v in aws_ssoadmin_account_assignment.management : k => [v.permission_set_arn, v.principal_type, v.principal_id, v.target_id, v.target_type] } == {
      "organization_admin/owner" = [aws_ssoadmin_permission_set.this["organization_admin"].arn, "USER", "00000000-0000-0000-0000-000000000001", "111111111111", "AWS_ACCOUNT"]
      "read_only/owner"          = [aws_ssoadmin_permission_set.this["read_only"].arn, "USER", "00000000-0000-0000-0000-000000000001", "111111111111", "AWS_ACCOUNT"]
    }
    error_message = "管理アカウントへの割当は、label の user の ID への USER の割当にしてください。"
  }

  assert {
    condition = { for k, v in aws_ssoadmin_account_assignment.member : k => [v.permission_set_arn, v.principal_type, v.principal_id, v.target_id, v.target_type] } == {
      "security/read_only/operators"  = [aws_ssoadmin_permission_set.this["read_only"].arn, "GROUP", "0000000000-00000000-0000-0000-0000-000000000002", "222222222222", "AWS_ACCOUNT"]
      "log-archive/support/operators" = [aws_ssoadmin_permission_set.this["support"].arn, "GROUP", "0000000000-00000000-0000-0000-0000-000000000002", "333333333333", "AWS_ACCOUNT"]
    }
    error_message = "メンバーアカウントへの割当は、label の group の ID への、env から引いたアカウントへの GROUP の割当にしてください。"
  }

  assert {
    condition     = alltrue([for v in concat(values(aws_ssoadmin_account_assignment.management), values(aws_ssoadmin_account_assignment.member)) : v.instance_arn == "arn:aws:sso:::instance/ssoins-0000000000000000"])
    error_message = "割当は読んだインスタンスに置いてください。"
  }

  assert {
    condition = output.assignment_target_ids == {
      management = { "organization_admin/owner" = "111111111111", "read_only/owner" = "111111111111" }
      member     = { "security/read_only/operators" = "222222222222", "log-archive/support/operators" = "333333333333" }
    }
    error_message = "assignment_target_ids が、割当の key から対象のアカウントの ID を返していません。"
  }

  assert {
    condition = output.organization_admin_deny_resources == {
      DenyAssumeRoot = "*"
      DenyCiIdentityChange = [
        "arn:aws:iam::111111111111:role/tabp-admin-ci-*",
        "arn:aws:iam::111111111111:policy/tabp-admin-ci-*",
        "arn:aws:iam::111111111111:oidc-provider/token.actions.githubusercontent.com",
      ]
      DenyBootstrapBucketChange = flatten([for b in var.bootstrap_bucket_arns : [b, "${b}/*"]])
      DenyStateKeyChange        = var.state_key_arn
    }
    error_message = "organization_admin_deny_resources が、inline policy の Deny の Resource を返していません。"
  }
}

run "no_member_assignments" {
  command = plan

  variables {
    member_assignments = []
  }

  assert {
    condition     = length(aws_ssoadmin_account_assignment.member) == 0 && length(aws_ssoadmin_account_assignment.management) == 2
    error_message = "member_assignments が空のとき、メンバーへの割当を作らないでください。"
  }
}

run "rejects_empty_management_assignments" {
  command = plan

  variables {
    management_assignments = []
  }

  expect_failures = [var.management_assignments]
}

run "rejects_unknown_member_account" {
  command = plan

  variables {
    member_assignments = [{ account = "dev", permission_set = "read_only", group = "operators" }]
  }

  expect_failures = [aws_ssoadmin_account_assignment.member["dev/read_only/operators"]]
}

run "rejects_no_instance" {
  command = plan

  override_data {
    target = data.aws_ssoadmin_instances.this
    values = { arns = [], identity_store_ids = [] }
  }

  expect_failures = [data.aws_ssoadmin_instances.this]
}

run "rejects_two_instances" {
  command = plan

  override_data {
    target = data.aws_ssoadmin_instances.this
    values = { arns = ["arn:aws:sso:::instance/ssoins-0000000000000000", "arn:aws:sso:::instance/ssoins-1111111111111111"], identity_store_ids = ["d-0000000000", "d-1111111111"] }
  }

  expect_failures = [data.aws_ssoadmin_instances.this]
}

run "rejects_instance_without_identity_store" {
  command = plan

  override_data {
    target = data.aws_ssoadmin_instances.this
    values = { arns = ["arn:aws:sso:::instance/ssoins-0000000000000000"], identity_store_ids = [] }
  }

  expect_failures = [data.aws_ssoadmin_instances.this]
}

run "rejects_identity_store_without_instance" {
  command = plan

  override_data {
    target = data.aws_ssoadmin_instances.this
    values = { arns = [], identity_store_ids = ["d-0000000000"] }
  }

  expect_failures = [data.aws_ssoadmin_instances.this]
}

run "rejects_principals_not_json" {
  command = plan

  override_data {
    target = data.aws_ssm_parameter.principals
    values = { value = "owner=00000000-0000-0000-0000-000000000001" }
  }

  expect_failures = [data.aws_ssm_parameter.principals]
}

run "rejects_principals_without_groups" {
  command = plan

  override_data {
    target = data.aws_ssm_parameter.principals
    values = { value = "{\"users\":{\"owner\":\"00000000-0000-0000-0000-000000000001\"}}" }
  }

  expect_failures = [data.aws_ssm_parameter.principals]
}

run "rejects_principals_with_extra_key" {
  command = plan

  override_data {
    target = data.aws_ssm_parameter.principals
    values = { value = "{\"users\":{\"owner\":\"00000000-0000-0000-0000-000000000001\"},\"groups\":{\"operators\":\"00000000-0000-0000-0000-000000000002\"},\"emails\":{}}" }
  }

  expect_failures = [data.aws_ssm_parameter.principals]
}

run "rejects_malformed_principal_id" {
  command = plan

  override_data {
    target = data.aws_ssm_parameter.principals
    values = { value = "{\"users\":{\"owner\":\"owner@example.com\"},\"groups\":{\"operators\":\"00000000-0000-0000-0000-000000000002\"}}" }
  }

  expect_failures = [data.aws_ssm_parameter.principals]
}

run "rejects_malformed_principal_label" {
  command = plan

  override_data {
    target = data.aws_ssm_parameter.principals
    values = { value = "{\"users\":{\"owner\":\"00000000-0000-0000-0000-000000000001\",\"Owner Name\":\"00000000-0000-0000-0000-000000000003\"},\"groups\":{\"operators\":\"00000000-0000-0000-0000-000000000002\"}}" }
  }

  expect_failures = [data.aws_ssm_parameter.principals]
}

run "rejects_missing_user_label" {
  command = plan

  override_data {
    target = data.aws_ssm_parameter.principals
    values = { value = "{\"users\":{\"someone\":\"00000000-0000-0000-0000-000000000001\"},\"groups\":{\"operators\":\"00000000-0000-0000-0000-000000000002\"}}" }
  }

  expect_failures = [data.aws_ssm_parameter.principals]
}

run "rejects_missing_group_label" {
  command = plan

  override_data {
    target = data.aws_ssm_parameter.principals
    values = { value = "{\"users\":{\"owner\":\"00000000-0000-0000-0000-000000000001\"},\"groups\":{}}" }
  }

  expect_failures = [data.aws_ssm_parameter.principals]
}

# expect_failures は data 単位で見るので、どの postcondition で落ちたかはここでは見分けない。以下の parameter は、
# 名前のとおりの1つだけが落ちる形を狙って作る。
run "rejects_empty_principals" {
  command = plan

  override_data {
    target = data.aws_ssm_parameter.principals
    values = { value = "" }
  }

  expect_failures = [data.aws_ssm_parameter.principals]
}

run "rejects_principals_json_array" {
  command = plan

  override_data {
    target = data.aws_ssm_parameter.principals
    values = { value = "[]" }
  }

  expect_failures = [data.aws_ssm_parameter.principals]
}

run "rejects_principals_json_null" {
  command = plan

  override_data {
    target = data.aws_ssm_parameter.principals
    values = { value = "null" }
  }

  expect_failures = [data.aws_ssm_parameter.principals]
}

run "rejects_principals_users_not_map" {
  command = plan

  override_data {
    target = data.aws_ssm_parameter.principals
    values = { value = "{\"users\":[\"00000000-0000-0000-0000-000000000001\"],\"groups\":{\"operators\":\"00000000-0000-0000-0000-000000000002\"}}" }
  }

  expect_failures = [data.aws_ssm_parameter.principals]
}

run "rejects_principals_groups_not_map" {
  command = plan

  override_data {
    target = data.aws_ssm_parameter.principals
    values = { value = "{\"users\":{\"owner\":\"00000000-0000-0000-0000-000000000001\"},\"groups\":\"operators\"}" }
  }

  expect_failures = [data.aws_ssm_parameter.principals]
}

run "rejects_numeric_principal_id" {
  command = plan

  override_data {
    target = data.aws_ssm_parameter.principals
    values = { value = "{\"users\":{\"owner\":1},\"groups\":{\"operators\":\"00000000-0000-0000-0000-000000000002\"}}" }
  }

  expect_failures = [data.aws_ssm_parameter.principals]
}

run "rejects_malformed_group_id" {
  command = plan

  override_data {
    target = data.aws_ssm_parameter.principals
    values = { value = "{\"users\":{\"owner\":\"00000000-0000-0000-0000-000000000001\"},\"groups\":{\"operators\":\"operators@example.com\"}}" }
  }

  expect_failures = [data.aws_ssm_parameter.principals]
}

run "rejects_malformed_group_label" {
  command = plan

  override_data {
    target = data.aws_ssm_parameter.principals
    values = { value = "{\"users\":{\"owner\":\"00000000-0000-0000-0000-000000000001\"},\"groups\":{\"operators\":\"00000000-0000-0000-0000-000000000002\",\"Ops Team\":\"00000000-0000-0000-0000-000000000004\"}}" }
  }

  expect_failures = [data.aws_ssm_parameter.principals]
}

run "accepts_32_character_label" {
  command = plan

  override_data {
    target = data.aws_ssm_parameter.principals
    values = { value = "{\"users\":{\"owner\":\"00000000-0000-0000-0000-000000000001\",\"${join("", [for i in range(32) : "a"])}\":\"00000000-0000-0000-0000-000000000003\"},\"groups\":{\"operators\":\"00000000-0000-0000-0000-000000000002\",\"${join("", [for i in range(32) : "b"])}\":\"00000000-0000-0000-0000-000000000004\"}}" }
  }
}

run "rejects_33_character_user_label" {
  command = plan

  override_data {
    target = data.aws_ssm_parameter.principals
    values = { value = "{\"users\":{\"owner\":\"00000000-0000-0000-0000-000000000001\",\"${join("", [for i in range(33) : "a"])}\":\"00000000-0000-0000-0000-000000000003\"},\"groups\":{\"operators\":\"00000000-0000-0000-0000-000000000002\"}}" }
  }

  expect_failures = [data.aws_ssm_parameter.principals]
}

run "rejects_33_character_group_label" {
  command = plan

  override_data {
    target = data.aws_ssm_parameter.principals
    values = { value = "{\"users\":{\"owner\":\"00000000-0000-0000-0000-000000000001\"},\"groups\":{\"operators\":\"00000000-0000-0000-0000-000000000002\",\"${join("", [for i in range(33) : "b"])}\":\"00000000-0000-0000-0000-000000000004\"}}" }
  }

  expect_failures = [data.aws_ssm_parameter.principals]
}

run "accepts_1_character_label" {
  command = plan

  variables {
    management_assignments = [{ permission_set = "organization_admin", user = "o" }]
    member_assignments     = [{ account = "security", permission_set = "read_only", group = "g" }]
  }

  override_data {
    target = data.aws_ssm_parameter.principals
    values = { value = "{\"users\":{\"o\":\"00000000-0000-0000-0000-000000000001\"},\"groups\":{\"g\":\"00000000-0000-0000-0000-000000000002\"}}" }
  }
}

run "rejects_label_with_leading_digit" {
  command = plan

  override_data {
    target = data.aws_ssm_parameter.principals
    values = { value = "{\"users\":{\"owner\":\"00000000-0000-0000-0000-000000000001\",\"1owner\":\"00000000-0000-0000-0000-000000000003\"},\"groups\":{\"operators\":\"00000000-0000-0000-0000-000000000002\"}}" }
  }

  expect_failures = [data.aws_ssm_parameter.principals]
}

run "rejects_label_with_leading_hyphen" {
  command = plan

  override_data {
    target = data.aws_ssm_parameter.principals
    values = { value = "{\"users\":{\"owner\":\"00000000-0000-0000-0000-000000000001\"},\"groups\":{\"operators\":\"00000000-0000-0000-0000-000000000002\",\"-ops\":\"00000000-0000-0000-0000-000000000004\"}}" }
  }

  expect_failures = [data.aws_ssm_parameter.principals]
}

run "rejects_principal_id_prefix_9_characters" {
  command = plan

  override_data {
    target = data.aws_ssm_parameter.principals
    values = { value = "{\"users\":{\"owner\":\"00000000-0000-0000-0000-000000000001\"},\"groups\":{\"operators\":\"000000000-00000000-0000-0000-0000-000000000002\"}}" }
  }

  expect_failures = [data.aws_ssm_parameter.principals]
}

run "rejects_principal_id_prefix_11_characters" {
  command = plan

  override_data {
    target = data.aws_ssm_parameter.principals
    values = { value = "{\"users\":{\"owner\":\"00000000000-00000000-0000-0000-0000-000000000001\"},\"groups\":{\"operators\":\"00000000-0000-0000-0000-000000000002\"}}" }
  }

  expect_failures = [data.aws_ssm_parameter.principals]
}

# 理由は organization-baseline/ADR-0204 決定8。
run "rejects_label_in_users_and_groups" {
  command = plan

  override_data {
    target = data.aws_ssm_parameter.principals
    values = { value = "{\"users\":{\"owner\":\"00000000-0000-0000-0000-000000000001\",\"operators\":\"00000000-0000-0000-0000-000000000003\"},\"groups\":{\"operators\":\"00000000-0000-0000-0000-000000000002\"}}" }
  }

  expect_failures = [data.aws_ssm_parameter.principals]
}

run "rejects_principals_users_null" {
  command = plan

  override_data {
    target = data.aws_ssm_parameter.principals
    values = { value = "{\"users\":null,\"groups\":{\"operators\":\"0000000000-00000000-0000-0000-0000-000000000002\"}}" }
  }

  expect_failures = [data.aws_ssm_parameter.principals]
}

# メンバーへの割当が無いと、groups の null を割当の postcondition が見ないので、形の postcondition だけが落とす。
run "rejects_principals_groups_null" {
  command = plan

  variables {
    member_assignments = []
  }

  override_data {
    target = data.aws_ssm_parameter.principals
    values = { value = "{\"users\":{\"owner\":\"00000000-0000-0000-0000-000000000001\"},\"groups\":null}" }
  }

  expect_failures = [data.aws_ssm_parameter.principals]
}

run "rejects_null_principal_id" {
  command = plan

  override_data {
    target = data.aws_ssm_parameter.principals
    values = { value = "{\"users\":{\"owner\":null},\"groups\":{\"operators\":\"0000000000-00000000-0000-0000-0000-000000000002\"}}" }
  }

  expect_failures = [data.aws_ssm_parameter.principals]
}

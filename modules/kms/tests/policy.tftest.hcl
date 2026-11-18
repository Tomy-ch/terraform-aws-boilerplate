mock_provider "aws" {
  mock_data "aws_caller_identity" {
    defaults = { account_id = "123456789012" }
  }
  mock_data "aws_region" {
    defaults = { region = "ap-northeast-1" }
  }
  mock_data "aws_partition" {
    defaults = { partition = "aws" }
  }
}

variables {
  name = "tabp-admin-tfstate"

  administration = {
    Sid       = "AccountKeyAdministration"
    Effect    = "Allow"
    Principal = { AWS = "arn:aws:iam::123456789012:root" }
    Action = [
      "kms:CancelKeyDeletion",
      "kms:CreateAlias",
      "kms:DeleteAlias",
      "kms:DescribeKey",
      "kms:DisableKey",
      "kms:DisableKeyRotation",
      "kms:EnableKey",
      "kms:EnableKeyRotation",
      "kms:GetKeyPolicy",
      "kms:GetKeyRotationStatus",
      "kms:ListGrants",
      "kms:ListKeyPolicies",
      "kms:ListResourceTags",
      "kms:PutKeyPolicy",
      "kms:RevokeGrant",
      "kms:RotateKeyOnDemand",
      "kms:ScheduleKeyDeletion",
      "kms:TagResource",
      "kms:UntagResource",
      "kms:UpdateAlias",
      "kms:UpdateKeyDescription",
    ]
    Resource = "*"
  }

  roles_via_s3 = {
    Sid    = "RolesViaS3ReadWrite"
    Effect = "Allow"
    Principal = { AWS = [
      "arn:aws:iam::123456789012:role/tabp-admin-ci-apply",
      "arn:aws:iam::123456789012:role/tabp-admin-ci-plan",
    ] }
    Action    = ["kms:Decrypt", "kms:GenerateDataKey"]
    Resource  = "*"
    Condition = { StringEquals = { "kms:ViaService" = "s3.ap-northeast-1.amazonaws.com" } }
  }

  logs_service = {
    Sid       = "LogsService"
    Effect    = "Allow"
    Principal = { Service = "logs.ap-northeast-1.amazonaws.com" }
    Action    = ["kms:Decrypt", "kms:Describe*", "kms:Encrypt", "kms:GenerateDataKey*", "kms:ReEncrypt*"]
    Resource  = "*"
    Condition = { ArnEquals = { "kms:EncryptionContext:aws:logs:arn" = [
      "arn:aws:logs:ap-northeast-1:123456789012:log-group:/tabp/admin/s3-access/plan",
      "arn:aws:logs:ap-northeast-1:123456789012:log-group:/tabp/admin/s3-access/state",
    ] } }
  }
}

# 文書は鍵の ARN を参照しない（Resource は Key Policy では "*" で、この鍵を指す）ので、plan で決まる。
run "roles_via_s3" {
  command = plan

  variables {
    users = {
      roles = [
        { arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan", via = "s3", access = "read-write" },
        { arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply", via = "s3", access = "read-write" },
      ]
    }
  }

  assert {
    condition = jsondecode(aws_kms_key_policy.this.policy) == {
      Version   = "2012-10-17"
      Statement = [var.administration, var.roles_via_s3]
    }
    error_message = "role だけを渡したときの Key Policy が、管理の委任と、S3 経由の利用の2文になっていません。"
  }
}

run "single_role" {
  command = plan

  variables {
    users = {
      roles = [{ arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan", via = "s3", access = "read-write" }]
    }
  }

  assert {
    condition     = jsondecode(aws_kms_key_policy.this.policy).Statement[1].Principal == { AWS = ["arn:aws:iam::123456789012:role/tabp-admin-ci-plan"] }
    error_message = "role が1件のときも Principal.AWS が配列になっていません。"
  }
}

# 入力の並べ替えで文書が変わると、差分が出続ける。
run "roles_order_does_not_matter" {
  command = plan

  variables {
    users = {
      roles = [
        { arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply", via = "s3", access = "read-write" },
        { arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan", via = "s3", access = "read-write" },
      ]
    }
  }

  assert {
    condition = jsondecode(aws_kms_key_policy.this.policy) == {
      Version   = "2012-10-17"
      Statement = [var.administration, var.roles_via_s3]
    }
    error_message = "role の並びを入れ替えると Key Policy が変わります。"
  }
}

run "logs_service" {
  command = plan

  variables {
    users = {
      services = { logs = ["/tabp/admin/s3-access/state", "/tabp/admin/s3-access/plan"] }
    }
  }

  assert {
    condition = jsondecode(aws_kms_key_policy.this.policy) == {
      Version   = "2012-10-17"
      Statement = [var.administration, var.logs_service]
    }
    error_message = "Log Group だけを渡したときの Key Policy が、管理の委任と、Log Group を名指した CloudWatch Logs の2文になっていません。"
  }
}

run "roles_and_logs" {
  command = plan

  variables {
    users = {
      roles = [
        { arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply", via = "s3", access = "read-write" },
        { arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan", via = "s3", access = "read-write" },
      ]
      services = { logs = ["/tabp/admin/s3-access/plan", "/tabp/admin/s3-access/state"] }
    }
  }

  assert {
    condition = jsondecode(aws_kms_key_policy.this.policy) == {
      Version   = "2012-10-17"
      Statement = [var.administration, var.roles_via_s3, var.logs_service]
    }
    error_message = "role と Log Group を渡したときの Key Policy が3文になっていません。"
  }
}

run "read_roles" {
  command = plan

  variables {
    users = {
      roles = [
        { arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan", via = "s3", access = "read-write" },
        {
          arn    = "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock-identity"
          via    = "s3"
          access = "read"
          objects = [
            "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an/identity/terraform.tfstate.tflock",
            "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an/a.tflock",
          ]
        },
        { arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-reader", via = "s3", access = "read", objects = ["arn:aws:s3:::bkt/k"] },
      ]
    }
  }

  assert {
    condition = jsondecode(aws_kms_key_policy.this.policy) == {
      Version = "2012-10-17"
      Statement = [
        var.administration,
        {
          Sid       = "RolesViaS3ReadWrite"
          Effect    = "Allow"
          Principal = { AWS = ["arn:aws:iam::123456789012:role/tabp-admin-ci-plan"] }
          Action    = ["kms:Decrypt", "kms:GenerateDataKey"]
          Resource  = "*"
          Condition = { StringEquals = { "kms:ViaService" = "s3.ap-northeast-1.amazonaws.com" } }
        },
        {
          Sid       = "RolesViaS3Read0"
          Effect    = "Allow"
          Principal = { AWS = ["arn:aws:iam::123456789012:role/tabp-admin-ci-reader"] }
          Action    = "kms:Decrypt"
          Resource  = "*"
          Condition = { StringEquals = {
            "kms:ViaService"                   = "s3.ap-northeast-1.amazonaws.com"
            "kms:EncryptionContext:aws:s3:arn" = ["arn:aws:s3:::bkt/k"]
          } }
        },
        {
          Sid       = "RolesViaS3Read1"
          Effect    = "Allow"
          Principal = { AWS = ["arn:aws:iam::123456789012:role/tabp-admin-ci-unlock-identity"] }
          Action    = "kms:Decrypt"
          Resource  = "*"
          Condition = { StringEquals = {
            "kms:ViaService" = "s3.ap-northeast-1.amazonaws.com"
            "kms:EncryptionContext:aws:s3:arn" = [
              "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an/a.tflock",
              "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an/identity/terraform.tfstate.tflock",
            ]
          } }
        },
      ]
    }
    error_message = "read の role の文が、ARN の昇順の Sid で、kms:Decrypt だけを名指した object の暗号化コンテキストに限って許していません。"
  }
}

run "read_role_only" {
  command = plan

  variables {
    users = {
      roles = [{ arn = "arn:aws:iam::123456789012:role/r", via = "s3", access = "read", objects = ["arn:aws:s3:::bkt/k"] }]
    }
  }

  assert {
    condition     = [for s in jsondecode(aws_kms_key_policy.this.policy).Statement : s.Sid] == ["AccountKeyAdministration", "RolesViaS3Read0"]
    error_message = "read の role だけを渡したときの Key Policy が、管理の委任と read の1文になっていません。"
  }
}

# 組織の trail は鍵の外のアカウント（管理アカウント）が持つ。値は ARN の文字列のまま、要求元と暗号化コンテキストの両方を限る。
run "cloudtrail_service" {
  command = plan

  variables {
    users = {
      services = { cloudtrail = [
        "arn:aws:cloudtrail:ap-northeast-1:111111111111:trail/tabp-org",
        "arn:aws:cloudtrail:ap-northeast-1:111111111111:trail/tabp-another",
      ] }
    }
  }

  assert {
    condition = jsondecode(aws_kms_key_policy.this.policy) == {
      Version = "2012-10-17"
      Statement = [
        var.administration,
        {
          Sid       = "CloudTrailEncrypt"
          Effect    = "Allow"
          Principal = { Service = "cloudtrail.amazonaws.com" }
          Action    = "kms:GenerateDataKey*"
          Resource  = "*"
          Condition = { StringEquals = {
            "aws:SourceArn" = [
              "arn:aws:cloudtrail:ap-northeast-1:111111111111:trail/tabp-another",
              "arn:aws:cloudtrail:ap-northeast-1:111111111111:trail/tabp-org",
            ]
            "kms:EncryptionContext:aws:cloudtrail:arn" = [
              "arn:aws:cloudtrail:ap-northeast-1:111111111111:trail/tabp-another",
              "arn:aws:cloudtrail:ap-northeast-1:111111111111:trail/tabp-org",
            ]
          } }
        },
        {
          Sid       = "CloudTrailDescribeKey"
          Effect    = "Allow"
          Principal = { Service = "cloudtrail.amazonaws.com" }
          Action    = "kms:DescribeKey"
          Resource  = "*"
          Condition = { StringEquals = { "aws:SourceArn" = [
            "arn:aws:cloudtrail:ap-northeast-1:111111111111:trail/tabp-another",
            "arn:aws:cloudtrail:ap-northeast-1:111111111111:trail/tabp-org",
          ] } }
        },
      ]
    }
    error_message = "trail だけを渡したときの Key Policy が、管理の委任と CloudTrail の2文になっていません。"
  }
}

run "config_service" {
  command = plan

  variables {
    users = {
      services = { config = ["333333333333", "222222222222"] }
    }
  }

  assert {
    condition = jsondecode(aws_kms_key_policy.this.policy) == {
      Version = "2012-10-17"
      Statement = [
        var.administration,
        {
          Sid       = "ConfigService"
          Effect    = "Allow"
          Principal = { Service = "config.amazonaws.com" }
          Action    = ["kms:Decrypt", "kms:GenerateDataKey"]
          Resource  = "*"
          Condition = { StringEquals = { "aws:SourceAccount" = ["222222222222", "333333333333"] } }
        },
      ]
    }
    error_message = "Config のアカウントだけを渡したときの Key Policy が、管理の委任と Config の文になっていません。"
  }
}

run "services_without_cloudtrail_and_config" {
  command = plan

  variables {
    users = {
      roles = [{ arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan", via = "s3", access = "read-write" }]
    }
  }

  assert {
    condition     = length([for s in jsondecode(aws_kms_key_policy.this.policy).Statement : s if contains(["CloudTrailEncrypt", "CloudTrailDescribeKey", "ConfigService"], s.Sid)]) == 0
    error_message = "trail と Config を渡さないのに、その文が出ています。"
  }
}

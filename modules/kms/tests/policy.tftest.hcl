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

run "sns_role" {
  command = plan

  variables {
    users = {
      roles = [
        { arn = "arn:aws:iam::123456789012:role/tabp-dev-heartbeat", via = "sns", access = "read-write", topics = ["tabp-dev-ops-notification", "tabp-dev-alerts"] },
        { arn = "arn:aws:iam::123456789012:role/tabp-dev-ci-apply", via = "sns", access = "read-write", topics = ["tabp-dev-alerts"] },
      ]
    }
  }

  assert {
    condition = jsondecode(aws_kms_key_policy.this.policy) == {
      Version = "2012-10-17"
      Statement = [
        var.administration,
        {
          Sid       = "RolesViaSns0"
          Effect    = "Allow"
          Principal = { AWS = ["arn:aws:iam::123456789012:role/tabp-dev-ci-apply"] }
          Action    = ["kms:Decrypt", "kms:GenerateDataKey*"]
          Resource  = "*"
          Condition = { StringEquals = {
            "kms:ViaService"                         = "sns.ap-northeast-1.amazonaws.com"
            "kms:EncryptionContext:aws:sns:topicArn" = ["arn:aws:sns:ap-northeast-1:123456789012:tabp-dev-alerts"]
          } }
        },
        {
          Sid       = "RolesViaSns1"
          Effect    = "Allow"
          Principal = { AWS = ["arn:aws:iam::123456789012:role/tabp-dev-heartbeat"] }
          Action    = ["kms:Decrypt", "kms:GenerateDataKey*"]
          Resource  = "*"
          Condition = { StringEquals = {
            "kms:ViaService" = "sns.ap-northeast-1.amazonaws.com"
            "kms:EncryptionContext:aws:sns:topicArn" = [
              "arn:aws:sns:ap-northeast-1:123456789012:tabp-dev-alerts",
              "arn:aws:sns:ap-northeast-1:123456789012:tabp-dev-ops-notification",
            ]
          } }
        },
      ]
    }
    error_message = "via が sns の role の文が、ARN の昇順の Sid で、SNS を経由し名指した Topic の暗号化コンテキストに限って許していません。"
  }
}

run "sns_services" {
  command = plan

  variables {
    users = {
      services = { sns = [
        { topic = "tabp-dev-ops-notification", publishers = ["events", "costalerts", "cloudwatch", "budgets"] },
        { topic = "tabp-dev-alerts", publishers = ["events"] },
      ] }
    }
  }

  assert {
    condition = jsondecode(aws_kms_key_policy.this.policy) == {
      Version = "2012-10-17"
      Statement = [
        var.administration,
        {
          Sid       = "SnsPublisherBudgets"
          Effect    = "Allow"
          Principal = { Service = "budgets.amazonaws.com" }
          Action    = ["kms:Decrypt", "kms:GenerateDataKey*"]
          Resource  = "*"
          Condition = { StringEquals = { "aws:SourceAccount" = "123456789012" } }
        },
        {
          Sid       = "SnsPublisherCloudWatch"
          Effect    = "Allow"
          Principal = { Service = "cloudwatch.amazonaws.com" }
          Action    = ["kms:Decrypt", "kms:GenerateDataKey*"]
          Resource  = "*"
          Condition = { StringEquals = { "aws:SourceAccount" = "123456789012" } }
        },
        {
          Sid       = "SnsPublisherCostAlerts"
          Effect    = "Allow"
          Principal = { Service = "costalerts.amazonaws.com" }
          Action    = ["kms:Decrypt", "kms:GenerateDataKey*"]
          Resource  = "*"
          Condition = { StringEquals = { "aws:SourceAccount" = "123456789012" } }
        },
        {
          Sid       = "SnsPublisherEvents"
          Effect    = "Allow"
          Principal = { Service = "events.amazonaws.com" }
          Action    = ["kms:Decrypt", "kms:GenerateDataKey*"]
          Resource  = "*"
          Condition = { StringEquals = { "kms:EncryptionContext:aws:sns:topicArn" = [
            "arn:aws:sns:ap-northeast-1:123456789012:tabp-dev-alerts",
            "arn:aws:sns:ap-northeast-1:123456789012:tabp-dev-ops-notification",
          ] } }
        },
      ]
    }
    error_message = "SNS へ publish するサービスの文が、Budgets・CloudWatch・Cost Anomaly Detection はアカウント、EventBridge は Topic の暗号化コンテキストに限った4文になっていません。"
  }
}

run "sns_services_only_named_publishers" {
  command = plan

  variables {
    users = {
      services = { sns = [{ topic = "tabp-dev-ops-notification", publishers = ["cloudwatch"] }] }
    }
  }

  assert {
    condition     = [for s in jsondecode(aws_kms_key_policy.this.policy).Statement : s.Sid] == ["AccountKeyAdministration", "SnsPublisherCloudWatch"]
    error_message = "publishers に無いサービスの文が出ています。"
  }
}

run "sns_events_only_for_topics_that_name_it" {
  command = plan

  variables {
    users = {
      services = { sns = [
        { topic = "tabp-dev-a", publishers = ["events"] },
        { topic = "tabp-dev-b", publishers = ["cloudwatch"] },
      ] }
    }
  }

  assert {
    condition = [for s in jsondecode(aws_kms_key_policy.this.policy).Statement : s.Condition if s.Sid == "SnsPublisherEvents"] == [
      { StringEquals = { "kms:EncryptionContext:aws:sns:topicArn" = ["arn:aws:sns:ap-northeast-1:123456789012:tabp-dev-a"] } },
    ]
    error_message = "EventBridge の文が、events を名指さない Topic の ARN を含んでいます。"
  }
}

run "sns_services_budgets_only" {
  command = plan

  variables {
    users = {
      services = { sns = [{ topic = "tabp-dev-ops-notification", publishers = ["budgets"] }] }
    }
  }

  assert {
    condition     = [for s in jsondecode(aws_kms_key_policy.this.policy).Statement : s.Sid] == ["AccountKeyAdministration", "SnsPublisherBudgets"]
    error_message = "publishers に無いサービスの文が出ています。"
  }
}

run "sns_services_events_only" {
  command = plan

  variables {
    users = {
      services = { sns = [{ topic = "tabp-dev-ops-notification", publishers = ["events"] }] }
    }
  }

  assert {
    condition     = [for s in jsondecode(aws_kms_key_policy.this.policy).Statement : s.Sid] == ["AccountKeyAdministration", "SnsPublisherEvents"]
    error_message = "publishers に無いサービスの文が出ています。"
  }
}

run "sns_services_costalerts_only" {
  command = plan

  variables {
    users = {
      services = { sns = [{ topic = "tabp-dev-ops-notification", publishers = ["costalerts"] }] }
    }
  }

  assert {
    condition     = [for s in jsondecode(aws_kms_key_policy.this.policy).Statement : s.Sid] == ["AccountKeyAdministration", "SnsPublisherCostAlerts"]
    error_message = "publishers に無いサービスの文が出ています。"
  }
}

run "all_users" {
  command = plan

  variables {
    users = {
      roles = [
        { arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan", via = "s3", access = "read-write" },
        { arn = "arn:aws:iam::123456789012:role/r", via = "s3", access = "read", objects = ["arn:aws:s3:::bkt/k"] },
        { arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan", via = "sns", access = "read-write", topics = ["t"] },
      ]
      services = {
        logs = ["/tabp/admin/s3-access/plan"]
        sns  = [{ topic = "t", publishers = ["budgets", "cloudwatch", "costalerts", "events"] }]
      }
    }
  }

  assert {
    condition = [for s in jsondecode(aws_kms_key_policy.this.policy).Statement : s.Sid] == [
      "AccountKeyAdministration", "RolesViaS3ReadWrite", "RolesViaS3Read0", "LogsService",
      "RolesViaSns0", "SnsPublisherBudgets", "SnsPublisherCloudWatch", "SnsPublisherCostAlerts", "SnsPublisherEvents",
    ]
    error_message = "利用者の種類をすべて渡したときの文の並びが変わっています。"
  }
}

run "roles_via_ecr" {
  command = plan

  variables {
    users = {
      roles = [{ arn = "arn:aws:iam::123456789012:role/tabp-dev-ci-apply", via = "ecr", access = "grant" }]
    }
  }

  assert {
    condition = jsondecode(aws_kms_key_policy.this.policy) == {
      Version = "2012-10-17"
      Statement = [var.administration, {
        Sid       = "RolesViaEcr"
        Effect    = "Allow"
        Principal = { AWS = ["arn:aws:iam::123456789012:role/tabp-dev-ci-apply"] }
        Action    = ["kms:CreateGrant", "kms:DescribeKey", "kms:RetireGrant"]
        Resource  = "*"
        Condition = { StringEquals = { "kms:ViaService" = "ecr.ap-northeast-1.amazonaws.com" } }
      }]
    }
    error_message = "via = ecr の role の Key Policy が、管理の委任と、ECR 経由の grant の作成・廃止の2文になっていません。"
  }
}

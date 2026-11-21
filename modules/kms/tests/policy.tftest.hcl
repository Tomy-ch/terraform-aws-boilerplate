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

  roles_via_s3 = [
    {
      Sid       = "RolesViaS3ReadWrite0"
      Effect    = "Allow"
      Principal = { AWS = ["arn:aws:iam::123456789012:role/tabp-admin-ci-apply"] }
      Action    = ["kms:Decrypt", "kms:GenerateDataKey"]
      Resource  = "*"
      Condition = {
        StringEquals = { "kms:ViaService" = "s3.ap-northeast-1.amazonaws.com" }
        StringLike = { "kms:EncryptionContext:aws:s3:arn" = [
          "arn:aws:s3:::tabp-admin-tfapply-123456789012-ap-northeast-1-an/*",
          "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an/workload/terraform.tfstate",
        ] }
      }
    },
    {
      Sid       = "RolesViaS3ReadWrite1"
      Effect    = "Allow"
      Principal = { AWS = ["arn:aws:iam::123456789012:role/tabp-admin-ci-plan"] }
      Action    = ["kms:Decrypt", "kms:GenerateDataKey"]
      Resource  = "*"
      Condition = {
        StringEquals = { "kms:ViaService" = "s3.ap-northeast-1.amazonaws.com" }
        StringLike = { "kms:EncryptionContext:aws:s3:arn" = [
          "arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an/branch/*",
          "arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an/pr/*",
        ] }
      }
    },
  ]

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
        { arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan", via = "s3", access = "read-write", object_prefixes = ["arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an/pr/", "arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an/branch/"] },
        { arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply", via = "s3", access = "read-write", objects = ["arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an/workload/terraform.tfstate"], object_prefixes = ["arn:aws:s3:::tabp-admin-tfapply-123456789012-ap-northeast-1-an/"] },
      ]
    }
  }

  assert {
    condition = jsondecode(aws_kms_key_policy.this.policy) == {
      Version   = "2012-10-17"
      Statement = concat([var.administration], var.roles_via_s3)
    }
    error_message = "read-write の role だけを渡したときの Key Policy が、管理の委任と、role ごとに ARN の昇順の Sid で object を StringLike で限った S3 経由の文になっていません。"
  }
}

run "single_role" {
  command = plan

  variables {
    users = {
      roles = [{ arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan", via = "s3", access = "read-write", object_prefixes = ["arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an/pr/", "arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an/branch/"] }]
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
        { arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply", via = "s3", access = "read-write", objects = ["arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an/workload/terraform.tfstate"], object_prefixes = ["arn:aws:s3:::tabp-admin-tfapply-123456789012-ap-northeast-1-an/"] },
        { arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan", via = "s3", access = "read-write", object_prefixes = ["arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an/branch/", "arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an/pr/"] },
      ]
    }
  }

  assert {
    condition = jsondecode(aws_kms_key_policy.this.policy) == {
      Version   = "2012-10-17"
      Statement = concat([var.administration], var.roles_via_s3)
    }
    error_message = "role と object の並びを入れ替えると Key Policy が変わります。"
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
        { arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply", via = "s3", access = "read-write", objects = ["arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an/workload/terraform.tfstate"], object_prefixes = ["arn:aws:s3:::tabp-admin-tfapply-123456789012-ap-northeast-1-an/"] },
        { arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan", via = "s3", access = "read-write", object_prefixes = ["arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an/pr/", "arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an/branch/"] },
      ]
      services = { logs = ["/tabp/admin/s3-access/plan", "/tabp/admin/s3-access/state"] }
    }
  }

  assert {
    condition = jsondecode(aws_kms_key_policy.this.policy) == {
      Version   = "2012-10-17"
      Statement = concat([var.administration], var.roles_via_s3, [var.logs_service])
    }
    error_message = "role と Log Group を渡したときの Key Policy が3文になっていません。"
  }
}

run "read_roles" {
  command = plan

  variables {
    users = {
      roles = [
        { arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan", via = "s3", access = "read-write", object_prefixes = ["arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an/pr/", "arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an/branch/"] },
        {
          arn    = "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock-identity"
          via    = "s3"
          access = "read"
          objects = [
            "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an/identity/terraform.tfstate.tflock",
            "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an/a.tflock",
          ]
        },
        { arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-reader", via = "s3", access = "read", objects = ["arn:aws:s3:::tabp.admin-reader-123456789012-ap-northeast-1-an/k"] },
      ]
    }
  }

  assert {
    condition = jsondecode(aws_kms_key_policy.this.policy) == {
      Version = "2012-10-17"
      Statement = [
        var.administration,
        merge(var.roles_via_s3[1], { Sid = "RolesViaS3ReadWrite0" }),
        {
          Sid       = "RolesViaS3Read0"
          Effect    = "Allow"
          Principal = { AWS = ["arn:aws:iam::123456789012:role/tabp-admin-ci-reader"] }
          Action    = "kms:Decrypt"
          Resource  = "*"
          Condition = { StringEquals = {
            "kms:ViaService"                   = "s3.ap-northeast-1.amazonaws.com"
            "kms:EncryptionContext:aws:s3:arn" = ["arn:aws:s3:::tabp.admin-reader-123456789012-ap-northeast-1-an/k"]
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
      roles = [{ arn = "arn:aws:iam::123456789012:role/r", via = "s3", access = "read", objects = ["arn:aws:s3:::tabp.admin-reader-123456789012-ap-northeast-1-an/k"] }]
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
        { arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan", via = "s3", access = "read-write", object_prefixes = ["arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an/pr/", "arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an/branch/"] },
        { arn = "arn:aws:iam::123456789012:role/r", via = "s3", access = "read", objects = ["arn:aws:s3:::tabp.admin-reader-123456789012-ap-northeast-1-an/k"] },
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
      "AccountKeyAdministration", "RolesViaS3ReadWrite0", "RolesViaS3Read0", "LogsService",
      "RolesViaSns0", "SnsPublisherBudgets", "SnsPublisherCloudWatch", "SnsPublisherCostAlerts", "SnsPublisherEvents",
    ]
    error_message = "利用者の種類をすべて渡したときの文の並びが変わっています。"
  }
}

run "key_policy_output" {
  command = plan

  variables {
    users = {
      roles = [
        { arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan", via = "s3", access = "read-write", object_prefixes = ["arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an/pr/", "arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an/branch/"] },
        { arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-unlock", via = "s3", access = "read", objects = ["arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an/workload/terraform.tfstate.tflock"] },
      ]
    }
  }

  assert {
    condition     = output.key_policy == aws_kms_key_policy.this.policy
    error_message = "key_policy が、Key Policy の resource の文書を返していません。"
  }

  assert {
    condition     = length(jsondecode(output.key_policy).Statement) == 3
    error_message = "key_policy が、委任・read-write・read の3文の文書になっていません。"
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
      roles = [{ arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan", via = "s3", access = "read-write", object_prefixes = ["arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an/pr/", "arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an/branch/"] }]
    }
  }

  assert {
    condition     = length([for s in jsondecode(aws_kms_key_policy.this.policy).Statement : s if contains(["CloudTrailEncrypt", "CloudTrailDescribeKey", "ConfigService"], s.Sid)]) == 0
    error_message = "trail と Config を渡さないのに、その文が出ています。"
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
        Sid       = "RolesViaEcrGrant"
        Effect    = "Allow"
        Principal = { AWS = ["arn:aws:iam::123456789012:role/tabp-dev-ci-apply"] }
        Action    = "kms:CreateGrant"
        Resource  = "*"
        Condition = {
          StringEquals = { "kms:ViaService" = "ecr.ap-northeast-1.amazonaws.com" }
          Bool         = { "kms:GrantIsForAWSResource" = "true" }
        }
        }, {
        Sid       = "RolesViaEcr"
        Effect    = "Allow"
        Principal = { AWS = ["arn:aws:iam::123456789012:role/tabp-dev-ci-apply"] }
        Action    = ["kms:DescribeKey", "kms:RetireGrant"]
        Resource  = "*"
        Condition = { StringEquals = { "kms:ViaService" = "ecr.ap-northeast-1.amazonaws.com" } }
      }]
    }
    error_message = "via = ecr の role の Key Policy が、管理の委任と、ECR 経由の grant の作成（AWS の resource 向けに限る）と、鍵の参照・grant の廃止の3文になっていません。"
  }
}

run "read_and_read_write_role" {
  command = plan

  variables {
    users = {
      roles = [
        { arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan", via = "s3", access = "read-write", object_prefixes = ["arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an/pr/", "arn:aws:s3:::tabp-admin-tfplan-123456789012-ap-northeast-1-an/branch/"] },
        {
          arn    = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan"
          via    = "s3"
          access = "read"
          objects = [
            "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an/workload/terraform.tfstate",
            "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an/identity/terraform.tfstate",
          ]
        },
      ]
    }
  }

  assert {
    condition = jsondecode(aws_kms_key_policy.this.policy) == {
      Version = "2012-10-17"
      Statement = [
        var.administration,
        merge(var.roles_via_s3[1], { Sid = "RolesViaS3ReadWrite0" }),
        {
          Sid       = "RolesViaS3Read0"
          Effect    = "Allow"
          Principal = { AWS = ["arn:aws:iam::123456789012:role/tabp-admin-ci-plan"] }
          Action    = "kms:Decrypt"
          Resource  = "*"
          Condition = { StringEquals = {
            "kms:ViaService" = "s3.ap-northeast-1.amazonaws.com"
            "kms:EncryptionContext:aws:s3:arn" = [
              "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an/identity/terraform.tfstate",
              "arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an/workload/terraform.tfstate",
            ]
          } }
        },
      ]
    }
    error_message = "read と read-write を併せ持つ role の文が、書く object の復号と鍵の生成の文と、読むだけの object の復号の文に分かれていません。"
  }
}

run "read_write_objects_only" {
  command = plan

  variables {
    users = {
      roles = [{ arn = "arn:aws:iam::123456789012:role/r", via = "s3", access = "read-write", objects = ["arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an/identity/terraform.tfstate"] }]
    }
  }

  assert {
    condition = jsondecode(aws_kms_key_policy.this.policy).Statement[1].Condition == {
      StringEquals = { "kms:ViaService" = "s3.ap-northeast-1.amazonaws.com" }
      StringLike   = { "kms:EncryptionContext:aws:s3:arn" = ["arn:aws:s3:::tabp-admin-tfstate-123456789012-ap-northeast-1-an/identity/terraform.tfstate"] }
    }
    error_message = "固定の object だけの read-write の文が、StringLike でその object に限っていません。"
  }
}

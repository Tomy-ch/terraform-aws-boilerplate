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
  users = {
    roles = [{ arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan", via = "s3", access = "read-write" }]
  }
}

run "name_rejects_other_characters" {
  command = plan

  variables {
    name = "tabp admin"
  }

  expect_failures = [var.name]
}

run "name_rejects_aws_prefix" {
  command = plan

  variables {
    name = "aws/tfstate"
  }

  expect_failures = [var.name]
}

run "name_rejects_empty" {
  command = plan

  variables {
    name = ""
  }

  expect_failures = [var.name]
}

run "name_accepts_250_characters" {
  command = plan

  variables {
    name = format("%0250d", 0)
  }
}

run "name_rejects_251_characters" {
  command = plan

  variables {
    name = format("%0251d", 0)
  }

  expect_failures = [var.name]
}

run "users_rejects_no_grantee" {
  command = plan

  variables {
    users = {}
  }

  expect_failures = [var.users]
}

run "users_rejects_unknown_via" {
  command = plan

  variables {
    users = {
      roles = [{ arn = "arn:aws:iam::123456789012:role/r", via = "sqs", access = "read-write" }]
    }
  }

  expect_failures = [var.users]
}

run "users_rejects_user_arn" {
  command = plan

  variables {
    users = {
      roles = [{ arn = "arn:aws:iam::123456789012:user/bootstrap", via = "s3", access = "read-write" }]
    }
  }

  expect_failures = [var.users]
}

run "users_rejects_wildcard_arn" {
  command = plan

  variables {
    users = {
      roles = [{ arn = "arn:aws:iam::123456789012:role/*", via = "s3", access = "read-write" }]
    }
  }

  expect_failures = [var.users]
}

run "users_rejects_account_root" {
  command = plan

  variables {
    users = {
      roles = [{ arn = "arn:aws:iam::123456789012:root", via = "s3", access = "read-write" }]
    }
  }

  expect_failures = [var.users]
}

run "users_rejects_duplicate_role" {
  command = plan

  variables {
    users = {
      roles = [
        { arn = "arn:aws:iam::123456789012:role/r", via = "s3", access = "read-write" },
        { arn = "arn:aws:iam::123456789012:role/r", via = "s3", access = "read-write" },
      ]
    }
  }

  expect_failures = [var.users]
}

run "users_rejects_log_group_arn" {
  command = plan

  variables {
    users = {
      services = { logs = ["arn:aws:logs:ap-northeast-1:123456789012:log-group:x"] }
    }
  }

  expect_failures = [var.users]
}

run "users_rejects_role_arn_without_name" {
  command = plan

  variables {
    users = {
      roles = [{ arn = "arn:aws:iam::123456789012:role/", via = "s3", access = "read-write" }]
    }
  }

  expect_failures = [var.users]
}

run "users_accepts_512_character_log_group" {
  command = plan

  variables {
    users = {
      services = { logs = [format("/%0511d", 0)] }
    }
  }
}

run "users_rejects_513_character_log_group" {
  command = plan

  variables {
    users = {
      services = { logs = [format("/%0512d", 0)] }
    }
  }

  expect_failures = [var.users]
}

run "users_rejects_empty_log_group" {
  command = plan

  variables {
    users = {
      services = { logs = [""] }
    }
  }

  expect_failures = [var.users]
}

run "users_rejects_log_group_with_asterisk" {
  command = plan

  variables {
    users = {
      services = { logs = ["*"] }
    }
  }

  expect_failures = [var.users]
}

run "users_rejects_log_group_with_question_mark" {
  command = plan

  variables {
    users = {
      services = { logs = ["a?b"] }
    }
  }

  expect_failures = [var.users]
}

run "users_rejects_log_group_with_colon" {
  command = plan

  variables {
    users = {
      services = { logs = ["a:b"] }
    }
  }

  expect_failures = [var.users]
}

run "users_rejects_log_group_with_space" {
  command = plan

  variables {
    users = {
      services = { logs = ["a b"] }
    }
  }

  expect_failures = [var.users]
}

run "users_rejects_duplicate_log_group" {
  command = plan

  variables {
    users = {
      services = { logs = ["/x", "/x"] }
    }
  }

  expect_failures = [var.users]
}

# 他のアカウントの role は、型では見られない（アカウント ID は data から決まる）。
run "users_rejects_other_account_role" {
  command = plan

  variables {
    users = {
      roles = [{ arn = "arn:aws:iam::210987654321:role/r", via = "s3", access = "read-write" }]
    }
  }

  expect_failures = [aws_kms_key_policy.this]
}

run "users_rejects_other_partition_role" {
  command = plan

  variables {
    users = {
      roles = [{ arn = "arn:aws-cn:iam::123456789012:role/r", via = "s3", access = "read-write" }]
    }
  }

  expect_failures = [aws_kms_key_policy.this]
}

run "policy_accepts_32768_characters" {
  command = plan

  variables {
    users = {
      services = { logs = concat([for i in range(57) : format("/%0500d", i)], [format("/%s", join("", [for i in range(195) : "z"]))]) }
    }
  }

  assert {
    condition     = length(aws_kms_key_policy.this.policy) == 32768
    error_message = "境界の文書が 32768 文字になっていません。文書の形が変わったなら、名前の長さを直してください。"
  }
}

run "policy_rejects_32769_characters" {
  command = plan

  variables {
    users = {
      services = { logs = concat([for i in range(57) : format("/%0500d", i)], [format("/%s", join("", [for i in range(196) : "z"]))]) }
    }
  }

  expect_failures = [aws_kms_key_policy.this]
}

run "policy_rejects_over_32kb" {
  command = plan

  variables {
    users = {
      services = { logs = [for i in range(70) : format("/%0500d", i)] }
    }
  }

  expect_failures = [aws_kms_key_policy.this]
}

run "users_rejects_unknown_access" {
  command = plan

  variables {
    users = {
      roles = [{ arn = "arn:aws:iam::123456789012:role/r", via = "s3", access = "write" }]
    }
  }

  expect_failures = [var.users]
}

run "users_rejects_read_without_objects" {
  command = plan

  variables {
    users = {
      roles = [{ arn = "arn:aws:iam::123456789012:role/r", via = "s3", access = "read" }]
    }
  }

  expect_failures = [var.users]
}

run "users_rejects_objects_on_read_write" {
  command = plan

  variables {
    users = {
      roles = [{ arn = "arn:aws:iam::123456789012:role/r", via = "s3", access = "read-write", objects = ["arn:aws:s3:::bkt/k"] }]
    }
  }

  expect_failures = [var.users]
}

run "users_rejects_bucket_arn_as_object" {
  command = plan

  variables {
    users = {
      roles = [{ arn = "arn:aws:iam::123456789012:role/r", via = "s3", access = "read", objects = ["arn:aws:s3:::bkt"] }]
    }
  }

  expect_failures = [var.users]
}

run "users_rejects_non_s3_object" {
  command = plan

  variables {
    users = {
      roles = [{ arn = "arn:aws:iam::123456789012:role/r", via = "s3", access = "read", objects = ["arn:aws:logs:ap-northeast-1:123456789012:log-group:x"] }]
    }
  }

  expect_failures = [var.users]
}

run "users_rejects_object_with_asterisk" {
  command = plan

  variables {
    users = {
      roles = [{ arn = "arn:aws:iam::123456789012:role/r", via = "s3", access = "read", objects = ["arn:aws:s3:::bkt/identity/*"] }]
    }
  }

  expect_failures = [var.users]
}

run "users_rejects_object_with_question_mark" {
  command = plan

  variables {
    users = {
      roles = [{ arn = "arn:aws:iam::123456789012:role/r", via = "s3", access = "read", objects = ["arn:aws:s3:::bkt/k?"] }]
    }
  }

  expect_failures = [var.users]
}

run "users_rejects_duplicate_object" {
  command = plan

  variables {
    users = {
      roles = [{ arn = "arn:aws:iam::123456789012:role/r", via = "s3", access = "read", objects = ["arn:aws:s3:::bkt/k", "arn:aws:s3:::bkt/k"] }]
    }
  }

  expect_failures = [var.users]
}

run "users_rejects_same_role_with_other_access" {
  command = plan

  variables {
    users = {
      roles = [
        { arn = "arn:aws:iam::123456789012:role/r", via = "s3", access = "read-write" },
        { arn = "arn:aws:iam::123456789012:role/r", via = "s3", access = "read", objects = ["arn:aws:s3:::bkt/k"] },
      ]
    }
  }

  expect_failures = [var.users]
}

run "users_rejects_object_in_other_partition" {
  command = plan

  variables {
    users = {
      roles = [{ arn = "arn:aws:iam::123456789012:role/r", via = "s3", access = "read", objects = ["arn:aws-cn:s3:::bkt/k"] }]
    }
  }

  expect_failures = [aws_kms_key_policy.this]
}

run "users_accepts_sns_role" {
  command = plan

  variables {
    users = {
      roles = [{ arn = "arn:aws:iam::123456789012:role/heartbeat", via = "sns", access = "read-write", topics = ["tabp-dev-ops-notification"] }]
    }
  }
}

run "users_rejects_sns_role_read" {
  command = plan

  variables {
    users = {
      roles = [{ arn = "arn:aws:iam::123456789012:role/heartbeat", via = "sns", access = "read", topics = ["t"], objects = ["arn:aws:s3:::bkt/k"] }]
    }
  }

  expect_failures = [var.users]
}

run "users_rejects_sns_role_without_topics" {
  command = plan

  variables {
    users = {
      roles = [{ arn = "arn:aws:iam::123456789012:role/heartbeat", via = "sns", access = "read-write" }]
    }
  }

  expect_failures = [var.users]
}

run "users_rejects_topics_on_s3_role" {
  command = plan

  variables {
    users = {
      roles = [{ arn = "arn:aws:iam::123456789012:role/heartbeat", via = "s3", access = "read-write", topics = ["t"] }]
    }
  }

  expect_failures = [var.users]
}

run "users_rejects_fifo_topic" {
  command = plan

  variables {
    users = {
      roles = [{ arn = "arn:aws:iam::123456789012:role/heartbeat", via = "sns", access = "read-write", topics = ["t.fifo"] }]
    }
  }

  expect_failures = [var.users]
}

run "users_rejects_topic_arn" {
  command = plan

  variables {
    users = {
      roles = [{ arn = "arn:aws:iam::123456789012:role/heartbeat", via = "sns", access = "read-write", topics = ["arn:aws:sns:ap-northeast-1:123456789012:t"] }]
    }
  }

  expect_failures = [var.users]
}

run "users_rejects_topic_with_asterisk" {
  command = plan

  variables {
    users = {
      roles = [{ arn = "arn:aws:iam::123456789012:role/heartbeat", via = "sns", access = "read-write", topics = ["t*"] }]
    }
  }

  expect_failures = [var.users]
}

run "users_accepts_256_character_topic" {
  command = plan

  variables {
    users = {
      roles = [{ arn = "arn:aws:iam::123456789012:role/heartbeat", via = "sns", access = "read-write", topics = [format("%0256d", 0)] }]
    }
  }
}

run "users_rejects_257_character_topic" {
  command = plan

  variables {
    users = {
      roles = [{ arn = "arn:aws:iam::123456789012:role/heartbeat", via = "sns", access = "read-write", topics = [format("%0257d", 0)] }]
    }
  }

  expect_failures = [var.users]
}

run "users_rejects_duplicate_topic" {
  command = plan

  variables {
    users = {
      roles = [{ arn = "arn:aws:iam::123456789012:role/heartbeat", via = "sns", access = "read-write", topics = ["t", "t"] }]
    }
  }

  expect_failures = [var.users]
}

run "users_accepts_only_sns_service" {
  command = plan

  variables {
    users = {
      services = { sns = [{ topic = "tabp-dev-ops-notification", publishers = ["cloudwatch"] }] }
    }
  }
}

run "users_rejects_sns_service_without_publishers" {
  command = plan

  variables {
    users = {
      services = { sns = [{ topic = "t", publishers = [] }] }
    }
  }

  expect_failures = [var.users]
}

run "users_rejects_unknown_sns_publisher" {
  command = plan

  variables {
    users = {
      services = { sns = [{ topic = "t", publishers = ["ses"] }] }
    }
  }

  expect_failures = [var.users]
}

run "users_rejects_duplicate_sns_service_topic" {
  command = plan

  variables {
    users = {
      services = { sns = [{ topic = "t", publishers = ["events"] }, { topic = "t", publishers = ["budgets"] }] }
    }
  }

  expect_failures = [var.users]
}

run "users_rejects_sns_service_fifo_topic" {
  command = plan

  variables {
    users = {
      services = { sns = [{ topic = "t.fifo", publishers = ["events"] }] }
    }
  }

  expect_failures = [var.users]
}

run "users_rejects_sns_service_topic_arn" {
  command = plan

  variables {
    users = {
      services = { sns = [{ topic = "arn:aws:sns:ap-northeast-1:123456789012:t", publishers = ["events"] }] }
    }
  }

  expect_failures = [var.users]
}

run "users_rejects_duplicate_sns_role" {
  command = plan

  variables {
    users = {
      roles = [
        { arn = "arn:aws:iam::123456789012:role/heartbeat", via = "sns", access = "read-write", topics = ["a"] },
        { arn = "arn:aws:iam::123456789012:role/heartbeat", via = "sns", access = "read-write", topics = ["b"] },
      ]
    }
  }

  expect_failures = [var.users]
}

run "users_rejects_empty_sns_services_only" {
  command = plan

  variables {
    users = {
      services = { sns = [] }
    }
  }

  expect_failures = [var.users]
}

run "users_rejects_sns_service_null_publishers" {
  command = plan

  variables {
    users = {
      services = { sns = [{ topic = "t", publishers = null }] }
    }
  }

  expect_failures = [var.users]
}

run "users_rejects_sns_publisher_wrong_case" {
  command = plan

  variables {
    users = {
      services = { sns = [{ topic = "t", publishers = ["Events"] }] }
    }
  }

  expect_failures = [var.users]
}

run "users_rejects_sns_topic_trailing_newline" {
  command = plan

  variables {
    users = {
      services = { sns = [{ topic = "t\n", publishers = ["events"] }] }
    }
  }

  expect_failures = [var.users]
}

run "users_accepts_256_character_sns_service_topic" {
  command = plan

  variables {
    users = {
      services = { sns = [{ topic = format("%0256d", 0), publishers = ["events"] }] }
    }
  }
}

run "users_rejects_257_character_sns_service_topic" {
  command = plan

  variables {
    users = {
      services = { sns = [{ topic = format("%0257d", 0), publishers = ["events"] }] }
    }
  }

  expect_failures = [var.users]
}

run "users_accepts_cloudtrail_only" {
  command = plan

  variables {
    users = { services = { cloudtrail = ["arn:aws:cloudtrail:ap-northeast-1:111111111111:trail/tabp-org"] } }
  }
}

run "users_accepts_config_only" {
  command = plan

  variables {
    users = { services = { config = ["111111111111"] } }
  }
}

run "users_rejects_cloudtrail_wildcard" {
  command = plan

  variables {
    users = { services = { cloudtrail = ["arn:aws:cloudtrail:ap-northeast-1:111111111111:trail/*"] } }
  }

  expect_failures = [var.users]
}

run "users_rejects_cloudtrail_non_trail_arn" {
  command = plan

  variables {
    users = { services = { cloudtrail = ["arn:aws:cloudtrail:ap-northeast-1:111111111111:eventdatastore/abc"] } }
  }

  expect_failures = [var.users]
}

run "users_rejects_cloudtrail_short_name" {
  command = plan

  variables {
    users = { services = { cloudtrail = ["arn:aws:cloudtrail:ap-northeast-1:111111111111:trail/ab"] } }
  }

  expect_failures = [var.users]
}

run "users_rejects_duplicate_cloudtrail" {
  command = plan

  variables {
    users = { services = { cloudtrail = [
      "arn:aws:cloudtrail:ap-northeast-1:111111111111:trail/tabp-org",
      "arn:aws:cloudtrail:ap-northeast-1:111111111111:trail/tabp-org",
    ] } }
  }

  expect_failures = [var.users]
}

run "users_rejects_other_partition_cloudtrail" {
  command = plan

  variables {
    users = { services = { cloudtrail = ["arn:aws-cn:cloudtrail:cn-north-1:111111111111:trail/tabp-org"] } }
  }

  expect_failures = [aws_kms_key_policy.this]
}

run "users_rejects_config_non_account" {
  command = plan

  variables {
    users = { services = { config = ["1111"] } }
  }

  expect_failures = [var.users]
}

run "users_rejects_config_wildcard" {
  command = plan

  variables {
    users = { services = { config = ["*"] } }
  }

  expect_failures = [var.users]
}

run "users_rejects_duplicate_config" {
  command = plan

  variables {
    users = { services = { config = ["111111111111", "111111111111"] } }
  }

  expect_failures = [var.users]
}

run "users_accepts_three_character_trail_name" {
  command = plan

  variables {
    users = { services = { cloudtrail = ["arn:aws:cloudtrail:ap-northeast-1:111111111111:trail/abc"] } }
  }
}

run "users_accepts_128_character_trail_name" {
  command = plan

  variables {
    users = { services = { cloudtrail = ["arn:aws:cloudtrail:ap-northeast-1:111111111111:trail/taaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaz"] } }
  }
}

run "users_rejects_129_character_trail_name" {
  command = plan

  variables {
    users = { services = { cloudtrail = ["arn:aws:cloudtrail:ap-northeast-1:111111111111:trail/taaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaazx"] } }
  }

  expect_failures = [var.users]
}

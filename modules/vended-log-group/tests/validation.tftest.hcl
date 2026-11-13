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
  name                = "tabp-admin-state-access"
  retention_days      = 365
  kms_key_arn         = "arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
  deletion_protection = true
  delivery_sources    = ["tabp-admin-tfstate"]
}

run "accepts_name_of_60_characters" {
  command = plan

  variables {
    name = join("", [for i in range(60) : "a"])
  }
}

run "accepts_name_with_every_allowed_character" {
  command = plan

  variables {
    name = "a_b-C9"
  }
}

run "rejects_empty_name" {
  command = plan

  variables {
    name = ""
  }

  expect_failures = [var.name]
}

run "rejects_name_of_61_characters" {
  command = plan

  variables {
    name = join("", [for i in range(61) : "a"])
  }

  expect_failures = [var.name]
}

run "rejects_name_with_slash" {
  command = plan

  variables {
    name = "tabp/admin"
  }

  expect_failures = [var.name]
}

run "rejects_name_with_dot" {
  command = plan

  variables {
    name = "tabp.admin"
  }

  expect_failures = [var.name]
}

run "rejects_name_with_space" {
  command = plan

  variables {
    name = "a b"
  }

  expect_failures = [var.name]
}

run "accepts_retention_365" {
  command = plan

  variables {
    retention_days = 365
  }
}

run "accepts_retention_1096" {
  command = plan

  variables {
    retention_days = 1096
  }
}

run "accepts_retention_3653" {
  command = plan

  variables {
    retention_days = 3653
  }
}

run "rejects_retention_zero" {
  command = plan

  variables {
    retention_days = 0
  }

  expect_failures = [var.retention_days]
}

run "rejects_retention_one" {
  command = plan

  variables {
    retention_days = 1
  }

  expect_failures = [var.retention_days]
}

run "rejects_retention_364" {
  command = plan

  variables {
    retention_days = 364
  }

  expect_failures = [var.retention_days]
}

run "rejects_retention_366" {
  command = plan

  variables {
    retention_days = 366
  }

  expect_failures = [var.retention_days]
}

run "rejects_fractional_retention" {
  command = plan

  variables {
    retention_days = 365.5
  }

  expect_failures = [var.retention_days]
}

run "rejects_negative_retention" {
  command = plan

  variables {
    retention_days = -1
  }

  expect_failures = [var.retention_days]
}

run "rejects_alias_arn" {
  command = plan

  variables {
    kms_key_arn = "arn:aws:kms:ap-northeast-1:123456789012:alias/tabp-admin-logs"
  }

  expect_failures = [var.kms_key_arn]
}

run "rejects_multi_region_key" {
  command = plan

  variables {
    kms_key_arn = "arn:aws:kms:ap-northeast-1:123456789012:key/mrk-1234abcd12ab34cd56ef1234567890ab"
  }

  expect_failures = [var.kms_key_arn]
}

run "rejects_bare_key_id" {
  command = plan

  variables {
    kms_key_arn = "00000000-0000-0000-0000-000000000000"
  }

  expect_failures = [var.kms_key_arn]
}

run "rejects_empty_key_arn" {
  command = plan

  variables {
    kms_key_arn = ""
  }

  expect_failures = [var.kms_key_arn]
}

run "rejects_key_in_other_region" {
  command = plan

  variables {
    kms_key_arn = "arn:aws:kms:us-east-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
  }

  expect_failures = [aws_cloudwatch_log_group.this]
}

run "rejects_key_in_other_account" {
  command = plan

  variables {
    kms_key_arn = "arn:aws:kms:ap-northeast-1:210987654321:key/1234abcd-12ab-34cd-56ef-1234567890ab"
  }

  expect_failures = [aws_cloudwatch_log_group.this]
}

run "accepts_key_arn_in_other_partition" {
  command = plan

  variables {
    kms_key_arn = "arn:aws-us-gov:kms:us-gov-west-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
  }

  override_data {
    target = data.aws_partition.current
    values = { partition = "aws-us-gov" }
  }
  override_data {
    target = data.aws_region.current
    values = { region = "us-gov-west-1" }
  }

  assert {
    condition     = aws_cloudwatch_log_group.this.kms_key_id == var.kms_key_arn
    error_message = "別の partition の鍵が Log Group に届いていません。"
  }
}

run "rejects_key_in_other_partition" {
  command = plan

  variables {
    kms_key_arn = "arn:aws-us-gov:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
  }

  expect_failures = [aws_cloudwatch_log_group.this]
}

run "rejects_key_arn_with_trailing_newline" {
  command = plan

  variables {
    kms_key_arn = "arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab\n"
  }

  expect_failures = [var.kms_key_arn]
}

run "rejects_key_arn_with_leading_space" {
  command = plan

  variables {
    kms_key_arn = " arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
  }

  expect_failures = [var.kms_key_arn]
}

run "rejects_key_id_with_uppercase_hex" {
  command = plan

  variables {
    kms_key_arn = "arn:aws:kms:ap-northeast-1:123456789012:key/1234ABCD-12AB-34CD-56EF-1234567890AB"
  }

  expect_failures = [var.kms_key_arn]
}

run "rejects_key_arn_with_11_digit_account" {
  command = plan

  variables {
    kms_key_arn = "arn:aws:kms:ap-northeast-1:12345678901:key/1234abcd-12ab-34cd-56ef-1234567890ab"
  }

  expect_failures = [var.kms_key_arn]
}

run "rejects_key_arn_without_region" {
  command = plan

  variables {
    kms_key_arn = "arn:aws:kms::123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
  }

  expect_failures = [var.kms_key_arn]
}

run "rejects_unknown_partition_name" {
  command = plan

  variables {
    kms_key_arn = "arn:gcp:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
  }

  expect_failures = [var.kms_key_arn]
}

run "accepts_retention_400" {
  command = plan

  variables {
    retention_days = 400
  }
}

run "accepts_retention_545" {
  command = plan

  variables {
    retention_days = 545
  }
}

run "accepts_retention_731" {
  command = plan

  variables {
    retention_days = 731
  }
}

run "accepts_retention_1827" {
  command = plan

  variables {
    retention_days = 1827
  }
}

run "accepts_retention_2192" {
  command = plan

  variables {
    retention_days = 2192
  }
}

run "accepts_retention_2557" {
  command = plan

  variables {
    retention_days = 2557
  }
}

run "accepts_retention_2922" {
  command = plan

  variables {
    retention_days = 2922
  }
}

run "accepts_retention_3288" {
  command = plan

  variables {
    retention_days = 3288
  }
}

run "rejects_retention_180" {
  command = plan

  variables {
    retention_days = 180
  }

  expect_failures = [var.retention_days]
}

run "rejects_retention_3654" {
  command = plan

  variables {
    retention_days = 3654
  }

  expect_failures = [var.retention_days]
}

run "accepts_name_of_1_character" {
  command = plan

  variables {
    name = "a"
  }
}

run "rejects_name_with_hash" {
  command = plan

  variables {
    name = "a#b"
  }

  expect_failures = [var.name]
}

run "accepts_delivery_source_of_60_characters" {
  command = plan

  variables {
    delivery_sources = [join("", [for i in range(60) : "a"])]
  }
}

run "accepts_several_delivery_sources" {
  command = plan

  variables {
    delivery_sources = ["tabp-admin-tfstate", "tabp-admin-plan-output"]
  }
}

run "rejects_empty_delivery_sources" {
  command = plan

  variables {
    delivery_sources = []
  }

  expect_failures = [var.delivery_sources]
}

run "rejects_delivery_source_of_61_characters" {
  command = plan

  variables {
    delivery_sources = [join("", [for i in range(61) : "a"])]
  }

  expect_failures = [var.delivery_sources]
}

run "rejects_delivery_source_with_slash" {
  command = plan

  variables {
    delivery_sources = ["tabp/admin"]
  }

  expect_failures = [var.delivery_sources]
}

run "rejects_empty_delivery_source_name" {
  command = plan

  variables {
    delivery_sources = [""]
  }

  expect_failures = [var.delivery_sources]
}

run "rejects_duplicate_delivery_sources" {
  command = plan

  variables {
    delivery_sources = ["tabp-admin-tfstate", "tabp-admin-tfstate"]
  }

  expect_failures = [var.delivery_sources]
}

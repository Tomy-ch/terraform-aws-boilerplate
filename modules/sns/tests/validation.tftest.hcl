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
  name        = "tabp-dev-ops-notification"
  kms_key_arn = "arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
  publishers  = ["cloudwatch"]
}

run "name_accepts_256_characters" {
  command = plan

  variables {
    name = format("%0256d", 0)
  }
}

run "name_rejects_257_characters" {
  command = plan

  variables {
    name = format("%0257d", 0)
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

run "name_rejects_fifo" {
  command = plan

  variables {
    name = "tabp-dev-ops-notification.fifo"
  }

  expect_failures = [var.name]
}

run "name_rejects_asterisk" {
  command = plan

  variables {
    name = "tabp-*"
  }

  expect_failures = [var.name]
}

run "kms_key_arn_rejects_alias_arn" {
  command = plan

  variables {
    kms_key_arn = "arn:aws:kms:ap-northeast-1:123456789012:alias/aws/sns"
  }

  expect_failures = [var.kms_key_arn]
}

run "kms_key_arn_rejects_alias_name" {
  command = plan

  variables {
    kms_key_arn = "alias/aws/sns"
  }

  expect_failures = [var.kms_key_arn]
}

run "kms_key_arn_rejects_multi_region_key" {
  command = plan

  variables {
    kms_key_arn = "arn:aws:kms:ap-northeast-1:123456789012:key/mrk-1234abcd12ab34cd56ef1234567890ab"
  }

  expect_failures = [var.kms_key_arn]
}

run "kms_key_arn_rejects_other_account" {
  command = plan

  variables {
    kms_key_arn = "arn:aws:kms:ap-northeast-1:210987654321:key/1234abcd-12ab-34cd-56ef-1234567890ab"
  }

  expect_failures = [aws_sns_topic.this]
}

run "kms_key_arn_rejects_other_region" {
  command = plan

  variables {
    kms_key_arn = "arn:aws:kms:us-east-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
  }

  expect_failures = [aws_sns_topic.this]
}

run "publishers_rejects_unknown" {
  command = plan

  variables {
    publishers = ["ses"]
  }

  expect_failures = [var.publishers]
}

run "publishers_accepts_empty" {
  command = plan

  variables {
    publishers = []
  }
}

run "name_rejects_arn" {
  command = plan

  variables {
    name = "arn:aws:sns:ap-northeast-1:123456789012:t"
  }

  expect_failures = [var.name]
}

run "name_rejects_trailing_newline" {
  command = plan

  variables {
    name = "t\n"
  }

  expect_failures = [var.name]
}

run "kms_key_arn_rejects_other_partition" {
  command = plan

  variables {
    kms_key_arn = "arn:aws-cn:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
  }

  expect_failures = [aws_sns_topic.this]
}

run "publishers_rejects_wrong_case" {
  command = plan

  variables {
    publishers = ["CloudWatch"]
  }

  expect_failures = [var.publishers]
}

run "publishers_accepts_costalerts" {
  command = plan

  variables {
    publishers = ["costalerts"]
  }
}

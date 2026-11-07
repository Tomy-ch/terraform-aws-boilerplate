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
    roles = [{ arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan", via = "s3" }]
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
      roles = [{ arn = "arn:aws:iam::123456789012:role/r", via = "sns" }]
    }
  }

  expect_failures = [var.users]
}

run "users_rejects_user_arn" {
  command = plan

  variables {
    users = {
      roles = [{ arn = "arn:aws:iam::123456789012:user/bootstrap", via = "s3" }]
    }
  }

  expect_failures = [var.users]
}

run "users_rejects_wildcard_arn" {
  command = plan

  variables {
    users = {
      roles = [{ arn = "arn:aws:iam::123456789012:role/*", via = "s3" }]
    }
  }

  expect_failures = [var.users]
}

run "users_rejects_account_root" {
  command = plan

  variables {
    users = {
      roles = [{ arn = "arn:aws:iam::123456789012:root", via = "s3" }]
    }
  }

  expect_failures = [var.users]
}

run "users_rejects_duplicate_role" {
  command = plan

  variables {
    users = {
      roles = [
        { arn = "arn:aws:iam::123456789012:role/r", via = "s3" },
        { arn = "arn:aws:iam::123456789012:role/r", via = "s3" },
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
      roles = [{ arn = "arn:aws:iam::123456789012:role/", via = "s3" }]
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

run "users_rejects_log_group_with_wildcard_or_colon" {
  command = plan

  variables {
    users = {
      services = { logs = ["*", "a?b", "a:b", "a b"] }
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
      roles = [{ arn = "arn:aws:iam::210987654321:role/r", via = "s3" }]
    }
  }

  expect_failures = [aws_kms_key_policy.this]
}

run "users_rejects_other_partition_role" {
  command = plan

  variables {
    users = {
      roles = [{ arn = "arn:aws-cn:iam::123456789012:role/r", via = "s3" }]
    }
  }

  expect_failures = [aws_kms_key_policy.this]
}

# 57 個の 501 文字の名前と 196 文字の名前で、文書がちょうど 32768 文字になる。
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

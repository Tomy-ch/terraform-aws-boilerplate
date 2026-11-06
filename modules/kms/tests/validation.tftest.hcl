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

# alias/<name> は 256 文字まで（KMS の CreateAlias）。alias/ の6文字を除いて 250 文字が境界。
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

# Key Policy は 32768 文字まで（KMS の PutKeyPolicy）。
run "policy_rejects_over_32kb" {
  command = plan

  variables {
    users = {
      services = { logs = [for i in range(70) : format("/%0500d", i)] }
    }
  }

  expect_failures = [aws_kms_key_policy.this]
}

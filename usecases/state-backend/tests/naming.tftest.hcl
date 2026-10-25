mock_provider "aws" {
  mock_data "aws_caller_identity" {
    defaults = { account_id = "123456789012" }
  }
  mock_data "aws_region" {
    defaults = { region = "ap-northeast-1" }
  }
}

run "name_follows_project_env" {
  command = plan

  variables {
    project = "tabp"
    env     = "admin"
  }

  assert {
    condition     = output.bucket_name == "tabp-admin-tfstate-123456789012-ap-northeast-1-an"
    error_message = "Bucket 名の前半が <project>-<env>-tfstate になっていません。"
  }
}

# 超えたときに落とすのは modules/s3 の precondition で、expect_failures は子 module の resource を指せない。
run "project_at_limit_with_sandbox" {
  command = plan

  variables {
    project = "abcdefghijklmnop"
    env     = "sandbox"
  }

  assert {
    condition     = length(output.bucket_name) == 63
    error_message = "project が16文字で env が sandbox のとき、plan が通っていません。"
  }
}

run "env_dev" {
  command = plan

  variables {
    project = "tabp"
    env     = "dev"
  }

  assert {
    condition     = output.bucket_name == "tabp-dev-tfstate-123456789012-ap-northeast-1-an"
    error_message = "env = dev から Bucket 名が組まれていません。"
  }
}

run "env_stg" {
  command = plan

  variables {
    project = "tabp"
    env     = "stg"
  }

  assert {
    condition     = output.bucket_name == "tabp-stg-tfstate-123456789012-ap-northeast-1-an"
    error_message = "env = stg から Bucket 名が組まれていません。"
  }
}

run "env_prd" {
  command = plan

  variables {
    project = "tabp"
    env     = "prd"
  }

  assert {
    condition     = output.bucket_name == "tabp-prd-tfstate-123456789012-ap-northeast-1-an"
    error_message = "env = prd から Bucket 名が組まれていません。"
  }
}

run "project_digits" {
  command = plan

  variables {
    project = "t4bp2"
    env     = "admin"
  }

  assert {
    condition     = output.bucket_name == "t4bp2-admin-tfstate-123456789012-ap-northeast-1-an"
    error_message = "数字を含む project が受け入れられていません。"
  }
}

run "project_hyphen" {
  command = plan

  variables {
    project = "tabp-x"
    env     = "admin"
  }

  expect_failures = [var.project]
}

run "project_uppercase" {
  command = plan

  variables {
    project = "Tabp"
    env     = "admin"
  }

  expect_failures = [var.project]
}

run "project_empty" {
  command = plan

  variables {
    project = ""
    env     = "admin"
  }

  expect_failures = [var.project]
}

run "env_unknown" {
  command = plan

  variables {
    project = "tabp"
    env     = "mgmt"
  }

  expect_failures = [var.env]
}

run "env_uppercase" {
  command = plan

  variables {
    project = "tabp"
    env     = "Dev"
  }

  expect_failures = [var.env]
}

run "env_suffix" {
  command = plan

  variables {
    project = "tabp"
    env     = "devx"
  }

  expect_failures = [var.env]
}

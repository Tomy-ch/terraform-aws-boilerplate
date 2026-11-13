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
  name                              = "tabp-admin-tfstate"
  kms_key_arn                       = "arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
  noncurrent_version_retention_days = 90
}

run "accepts_role_with_path" {
  command = plan

  variables {
    object_writers = {
      identity = {
        role_arn = "arn:aws:iam::123456789012:role/ci/tabp-admin-ci-apply-identity"
        keys     = ["identity/terraform.tfstate"]
        access   = "write"
      }
    }
  }

  assert {
    condition     = output.object_writers.identity.principal_arn == "arn:aws:iam::123456789012:role/ci/tabp-admin-ci-apply-identity"
    error_message = "path を持つ role の ARN が、書き込みの例外へそのまま届いていません。"
  }
}

run "rejects_no_writer" {
  command = plan

  variables {
    object_writers = {}
  }

  expect_failures = [var.object_writers]
}

run "rejects_key_not_usable_in_sid" {
  command = plan

  variables {
    object_writers = {
      "identity-unlock" = {
        role_arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity"
        keys     = ["identity/terraform.tfstate"]
        access   = "write"
      }
    }
  }

  expect_failures = [var.object_writers]
}

run "rejects_reserved_key" {
  command = plan

  variables {
    object_writers = {
      bootstrapExecutor = {
        role_arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity"
        keys     = ["identity/terraform.tfstate"]
        access   = "write"
      }
    }
  }

  expect_failures = [var.object_writers]
}

run "rejects_user_arn" {
  command = plan

  variables {
    object_writers = {
      identity = {
        role_arn = "arn:aws:iam::123456789012:user/tabp-bootstrap"
        keys     = ["identity/terraform.tfstate"]
        access   = "write"
      }
    }
  }

  expect_failures = [var.object_writers]
}

run "rejects_assumed_role_arn" {
  command = plan

  variables {
    object_writers = {
      identity = {
        role_arn = "arn:aws:sts::123456789012:assumed-role/tabp-admin-ci-apply-identity/session"
        keys     = ["identity/terraform.tfstate"]
        access   = "write"
      }
    }
  }

  expect_failures = [var.object_writers]
}

run "rejects_root_arn" {
  command = plan

  variables {
    object_writers = {
      identity = {
        role_arn = "arn:aws:iam::123456789012:root"
        keys     = ["identity/terraform.tfstate"]
        access   = "write"
      }
    }
  }

  expect_failures = [var.object_writers]
}

run "rejects_wildcard_role_arn" {
  command = plan

  variables {
    object_writers = {
      identity = {
        role_arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-*"
        keys     = ["identity/terraform.tfstate"]
        access   = "write"
      }
    }
  }

  expect_failures = [var.object_writers]
}

run "rejects_duplicate_role" {
  command = plan

  variables {
    object_writers = {
      identity = {
        role_arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity"
        keys     = ["identity/terraform.tfstate"]
        access   = "write"
      }
      workload = {
        role_arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity"
        keys     = ["workload/terraform.tfstate"]
        access   = "write"
      }
    }
  }

  expect_failures = [var.object_writers]
}

run "rejects_no_key" {
  command = plan

  variables {
    object_writers = {
      identity = {
        role_arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity"
        keys     = []
        access   = "write"
      }
    }
  }

  expect_failures = [var.object_writers]
}

run "rejects_wildcard_key" {
  command = plan

  variables {
    object_writers = {
      identity = {
        role_arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity"
        keys     = ["identity/*"]
        access   = "write"
      }
    }
  }

  expect_failures = [var.object_writers]
}

run "rejects_question_mark_key" {
  command = plan

  variables {
    object_writers = {
      identity = {
        role_arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity"
        keys     = ["identity/terraform.tfstat?"]
        access   = "write"
      }
    }
  }

  expect_failures = [var.object_writers]
}

run "rejects_key_with_leading_slash" {
  command = plan

  variables {
    object_writers = {
      identity = {
        role_arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity"
        keys     = ["/identity/terraform.tfstate"]
        access   = "write"
      }
    }
  }

  expect_failures = [var.object_writers]
}

run "rejects_unknown_access" {
  command = plan

  variables {
    object_writers = {
      identity = {
        role_arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity"
        keys     = ["identity/terraform.tfstate"]
        access   = "read-write"
      }
    }
  }

  expect_failures = [var.object_writers]
}

run "rejects_action_as_access" {
  command = plan

  variables {
    object_writers = {
      identity = {
        role_arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity"
        keys     = ["identity/terraform.tfstate"]
        access   = "s3:PutObject"
      }
    }
  }

  expect_failures = [var.object_writers]
}

run "accepts_bootstrap_executor" {
  command = plan

  variables {
    object_writers = {
      identity = {
        role_arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity"
        keys     = ["identity/terraform.tfstate"]
        access   = "write"
      }
    }
    bootstrap_executor = {
      user_name = "tabp-bootstrap"
      keys      = ["identity/terraform.tfstate"]
    }
  }
}

run "rejects_bootstrap_executor_with_path" {
  command = plan

  variables {
    object_writers = {
      identity = {
        role_arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity"
        keys     = ["identity/terraform.tfstate"]
        access   = "write"
      }
    }
    bootstrap_executor = {
      user_name = "bootstrap/tabp-bootstrap"
      keys      = ["identity/terraform.tfstate"]
    }
  }

  expect_failures = [var.bootstrap_executor]
}

run "rejects_bootstrap_executor_arn" {
  command = plan

  variables {
    object_writers = {
      identity = {
        role_arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity"
        keys     = ["identity/terraform.tfstate"]
        access   = "write"
      }
    }
    bootstrap_executor = {
      user_name = "arn:aws:iam::123456789012:user/tabp-bootstrap"
      keys      = ["identity/terraform.tfstate"]
    }
  }

  expect_failures = [var.bootstrap_executor]
}

run "rejects_long_bootstrap_executor_name" {
  command = plan

  variables {
    object_writers = {
      identity = {
        role_arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity"
        keys     = ["identity/terraform.tfstate"]
        access   = "write"
      }
    }
    bootstrap_executor = {
      user_name = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
      keys      = ["identity/terraform.tfstate"]
    }
  }

  expect_failures = [var.bootstrap_executor]
}

run "rejects_wildcard_bootstrap_executor_name" {
  command = plan

  variables {
    object_writers = {
      identity = {
        role_arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity"
        keys     = ["identity/terraform.tfstate"]
        access   = "write"
      }
    }
    bootstrap_executor = {
      user_name = "tabp-*"
      keys      = ["identity/terraform.tfstate"]
    }
  }

  expect_failures = [var.bootstrap_executor]
}

run "rejects_bootstrap_executor_without_key" {
  command = plan

  variables {
    object_writers = {
      identity = {
        role_arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity"
        keys     = ["identity/terraform.tfstate"]
        access   = "write"
      }
    }
    bootstrap_executor = {
      user_name = "tabp-bootstrap"
      keys      = []
    }
  }

  expect_failures = [var.bootstrap_executor]
}

run "rejects_bootstrap_executor_wildcard_key" {
  command = plan

  variables {
    object_writers = {
      identity = {
        role_arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity"
        keys     = ["identity/terraform.tfstate"]
        access   = "write"
      }
    }
    bootstrap_executor = {
      user_name = "tabp-bootstrap"
      keys      = ["identity/*"]
    }
  }

  expect_failures = [var.bootstrap_executor]
}

run "rejects_key_starting_with_uppercase" {
  command = plan

  variables {
    object_writers = {
      Identity = {
        role_arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity"
        keys     = ["identity/terraform.tfstate"]
        access   = "write"
      }
    }
  }

  expect_failures = [var.object_writers]
}

run "rejects_empty_key" {
  command = plan

  variables {
    object_writers = {
      identity = {
        role_arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity"
        keys     = [""]
        access   = "write"
      }
    }
  }

  expect_failures = [var.object_writers]
}

run "rejects_bootstrap_executor_key_with_leading_slash" {
  command = plan

  variables {
    object_writers = {
      identity = {
        role_arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity"
        keys     = ["identity/terraform.tfstate"]
        access   = "write"
      }
    }
    bootstrap_executor = {
      user_name = "tabp-bootstrap"
      keys      = ["/identity/terraform.tfstate"]
    }
  }

  expect_failures = [var.bootstrap_executor]
}

run "rejects_bootstrap_executor_question_mark_key" {
  command = plan

  variables {
    object_writers = {
      identity = {
        role_arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity"
        keys     = ["identity/terraform.tfstate"]
        access   = "write"
      }
    }
    bootstrap_executor = {
      user_name = "tabp-bootstrap"
      keys      = ["identity/terraform.tfstat?"]
    }
  }

  expect_failures = [var.bootstrap_executor]
}

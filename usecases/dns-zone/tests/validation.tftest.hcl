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

# modules/log-group の kms_key_arn の validation と、internal/zone の鍵の precondition を通すため、ARN を形のある値にする。
override_resource {
  target = module.query_log.module.kms.aws_kms_key.this
  values = { arn = "arn:aws:kms:us-east-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab" }
}

override_resource {
  target = module.query_log.module.log_group.aws_cloudwatch_log_group.this
  values = { arn = "arn:aws:logs:us-east-1:123456789012:log-group:/aws/route53/dev.example.com" }
}

override_resource {
  target = module.dnssec_key.aws_kms_key.this
  values = { arn = "arn:aws:kms:us-east-1:123456789012:key/abcd1234-12ab-34cd-56ef-1234567890ab" }
}

override_resource {
  target = module.zone.aws_route53_zone.this
  values = {
    zone_id      = "Z0123456789ABCDEFGHIJ"
    arn          = "arn:aws:route53:::hostedzone/Z0123456789ABCDEFGHIJ"
    name_servers = ["ns-1.awsdns-01.com", "ns-2.awsdns-02.net", "ns-3.awsdns-03.org", "ns-4.awsdns-04.co.uk"]
  }
}

override_resource {
  target = module.zone.aws_route53_key_signing_key.this
  values = { ds_record = "12345 13 2 0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF" }
}

variables {
  project                       = "tabp"
  env                           = "dev"
  apex_domain                   = "example.com"
  delegations                   = {}
  query_log_retention_days      = 365
  query_log_deletion_protection = true
  notify_topic_arn              = "arn:aws:sns:us-east-1:123456789012:tabp-dev-ops-notification"
}

run "project_rejects_hyphen" {
  command = plan

  variables {
    project = "ta-bp"
  }

  expect_failures = [var.project]
}

run "project_accepts_42_characters" {
  command = plan

  variables {
    project = "abcdefghijabcdefghijabcdefghijabcdefghij12"
  }
}

run "project_rejects_43_characters" {
  command = plan

  variables {
    project = "abcdefghijabcdefghijabcdefghijabcdefghij123"
  }

  expect_failures = [var.project]
}

run "env_rejects_admin" {
  command = plan

  variables {
    env = "admin"
  }

  expect_failures = [var.env]
}

run "apex_domain_rejects_uppercase" {
  command = plan

  variables {
    apex_domain = "Example.com"
  }

  expect_failures = [var.apex_domain]
}

run "apex_domain_rejects_trailing_dot" {
  command = plan

  variables {
    apex_domain = "example.com."
  }

  expect_failures = [var.apex_domain]
}

run "apex_domain_rejects_single_label" {
  command = plan

  variables {
    apex_domain = "localhost"
  }

  expect_failures = [var.apex_domain]
}

run "apex_domain_accepts_subdomain" {
  command = plan

  variables {
    apex_domain = "app.example.co.jp"
  }
}

run "delegations_rejects_sandbox_key" {
  command = plan

  variables {
    env = "prd"
    delegations = {
      sandbox = { name_servers = ["ns-1.awsdns-01.com", "ns-2.awsdns-02.net", "ns-3.awsdns-03.org", "ns-4.awsdns-04.co.uk"] }
    }
  }

  expect_failures = [var.delegations]
}

run "delegations_rejects_three_name_servers" {
  command = plan

  variables {
    env = "prd"
    delegations = {
      dev = { name_servers = ["ns-1.awsdns-01.com", "ns-2.awsdns-02.net", "ns-3.awsdns-03.org"] }
    }
  }

  expect_failures = [var.delegations]
}

run "delegations_rejects_duplicate_name_servers" {
  command = plan

  variables {
    env = "prd"
    delegations = {
      dev = { name_servers = ["ns-1.awsdns-01.com", "ns-1.awsdns-01.com", "ns-3.awsdns-03.org", "ns-4.awsdns-04.co.uk"] }
    }
  }

  expect_failures = [var.delegations]
}

run "delegations_rejects_non_route53_name_server" {
  command = plan

  variables {
    env = "prd"
    delegations = {
      dev = { name_servers = ["ns1.example.net", "ns-2.awsdns-02.net", "ns-3.awsdns-03.org", "ns-4.awsdns-04.co.uk"] }
    }
  }

  expect_failures = [var.delegations]
}

run "delegations_rejects_name_server_with_trailing_dot" {
  command = plan

  variables {
    env = "prd"
    delegations = {
      dev = { name_servers = ["ns-1.awsdns-01.com.", "ns-2.awsdns-02.net", "ns-3.awsdns-03.org", "ns-4.awsdns-04.co.uk"] }
    }
  }

  expect_failures = [var.delegations]
}

run "delegations_rejects_ds_with_other_algorithm" {
  command = plan

  variables {
    env = "prd"
    delegations = {
      dev = {
        name_servers = ["ns-1.awsdns-01.com", "ns-2.awsdns-02.net", "ns-3.awsdns-03.org", "ns-4.awsdns-04.co.uk"]
        ds_record    = "12345 8 2 0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF"
      }
    }
  }

  expect_failures = [var.delegations]
}

run "delegations_rejects_ds_with_short_digest" {
  command = plan

  variables {
    env = "prd"
    delegations = {
      dev = {
        name_servers = ["ns-1.awsdns-01.com", "ns-2.awsdns-02.net", "ns-3.awsdns-03.org", "ns-4.awsdns-04.co.uk"]
        ds_record    = "12345 13 2 0123456789ABCDEF"
      }
    }
  }

  expect_failures = [var.delegations]
}

run "query_log_retention_rejects_364" {
  command = plan

  variables {
    query_log_retention_days = 364
  }

  expect_failures = [var.query_log_retention_days]
}

run "query_log_retention_accepts_365" {
  command = plan

  variables {
    query_log_retention_days = 365
  }
}

run "notify_topic_rejects_other_region" {
  command = plan

  variables {
    notify_topic_arn = "arn:aws:sns:ap-northeast-1:123456789012:tabp-dev-ops-notification"
  }

  expect_failures = [var.notify_topic_arn]
}

run "notify_topic_rejects_fifo" {
  command = plan

  variables {
    notify_topic_arn = "arn:aws:sns:us-east-1:123456789012:tabp-dev-ops-notification.fifo"
  }

  expect_failures = [var.notify_topic_arn]
}

run "project_rejects_empty" {
  command = plan

  variables {
    project = ""
  }

  expect_failures = [var.project]
}

run "apex_domain_accepts_240_characters" {
  command = plan

  variables {
    env         = "prd"
    apex_domain = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb.com"
  }
}

run "apex_domain_rejects_241_characters" {
  command = plan

  variables {
    env         = "prd"
    apex_domain = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb.com"
  }

  expect_failures = [var.apex_domain]
}

run "delegations_rejects_five_name_servers" {
  command = plan

  variables {
    env = "prd"
    delegations = {
      dev = {
        name_servers = ["ns-1.awsdns-01.com", "ns-2.awsdns-02.net", "ns-3.awsdns-03.org", "ns-4.awsdns-04.co.uk", "ns-5.awsdns-05.com"]
      }
    }
  }

  expect_failures = [var.delegations]
}

run "delegations_rejects_empty_name_servers" {
  command = plan

  variables {
    env = "prd"
    delegations = {
      dev = {
        name_servers = []
      }
    }
  }

  expect_failures = [var.delegations]
}

run "delegations_rejects_lowercase_hex_ds" {
  command = plan

  variables {
    env = "prd"
    delegations = {
      dev = {
        name_servers = ["ns-1.awsdns-01.com", "ns-2.awsdns-02.net", "ns-3.awsdns-03.org", "ns-4.awsdns-04.co.uk"]
        ds_record    = "12345 13 2 0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
      }
    }
  }

  expect_failures = [var.delegations]
}

run "delegations_rejects_key_tag_65536" {
  command = plan

  variables {
    env = "prd"
    delegations = {
      dev = {
        name_servers = ["ns-1.awsdns-01.com", "ns-2.awsdns-02.net", "ns-3.awsdns-03.org", "ns-4.awsdns-04.co.uk"]
        ds_record    = "65536 13 2 0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF"
      }
    }
  }

  expect_failures = [var.delegations]
}

run "delegations_accepts_key_tag_65535" {
  command = plan

  variables {
    env = "prd"
    delegations = {
      dev = {
        name_servers = ["ns-1.awsdns-01.com", "ns-2.awsdns-02.net", "ns-3.awsdns-03.org", "ns-4.awsdns-04.co.uk"]
        ds_record    = "65535 13 2 0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF"
      }
    }
  }
}

run "delegations_accepts_stg_key" {
  command = plan

  variables {
    env = "prd"
    delegations = {
      stg = {
        name_servers = ["ns-1.awsdns-01.com", "ns-2.awsdns-02.net", "ns-3.awsdns-03.org", "ns-4.awsdns-04.co.uk"]
      }
    }
  }
}

run "query_log_retention_rejects_366" {
  command = plan

  variables {
    query_log_retention_days = 366
  }

  expect_failures = [var.query_log_retention_days]
}

run "query_log_retention_accepts_3653" {
  command = plan

  variables {
    query_log_retention_days = 3653
  }
}

run "env_accepts_stg" {
  command = plan

  variables {
    env = "stg"
  }
}

run "notify_topic_rejects_empty" {
  command = plan

  variables {
    notify_topic_arn = ""
  }

  expect_failures = [var.notify_topic_arn]
}

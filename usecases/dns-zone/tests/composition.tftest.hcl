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

# resource policy の Resource と、internal/zone の鍵の precondition を通すため、ARN を形のある値にする。
override_resource {
  target = module.query_log.aws_kms_key.this
  values = { arn = "arn:aws:kms:us-east-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab" }
}

override_resource {
  target = module.query_log.aws_cloudwatch_log_group.this
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

run "child_environment_zone_is_under_the_apex" {
  command = plan

  variables {
    project = "acme"
    env     = "sandbox"
  }

  assert {
    condition     = output.zone_name == "sandbox.example.com"
    error_message = "prd のほかの zone の名前が <env>.<apex_domain> ではありません。"
  }
  assert {
    condition     = module.query_log.log_group_name == "/aws/route53/sandbox.example.com"
    error_message = "クエリログの Log Group が /aws/route53/<zone の名前> ではありません。"
  }
  assert {
    condition     = module.dnssec_key.alias_name == "alias/acme-sandbox-dns-dnssec"
    error_message = "DNSSEC の鍵の alias が <project>-<env>-dns-dnssec ではありません。"
  }
}

run "prd_zone_is_the_apex_and_delegates" {
  command = plan

  variables {
    env = "prd"
    delegations = {
      dev = {
        name_servers = ["ns-1.awsdns-01.com", "ns-2.awsdns-02.net", "ns-3.awsdns-03.org", "ns-4.awsdns-04.co.uk"]
      }
    }
  }

  assert {
    condition     = output.zone_name == "example.com"
    error_message = "prd の zone の名前が apex_domain ではありません。"
  }
}

run "non_prd_env_rejects_delegations" {
  command = plan

  variables {
    delegations = {
      stg = {
        name_servers = ["ns-1.awsdns-01.com", "ns-2.awsdns-02.net", "ns-3.awsdns-03.org", "ns-4.awsdns-04.co.uk"]
      }
    }
  }

  expect_failures = [output.zone_name]
}

run "stg_env_rejects_delegations" {
  command = plan

  variables {
    env = "stg"
    delegations = {
      dev = {
        name_servers = ["ns-1.awsdns-01.com", "ns-2.awsdns-02.net", "ns-3.awsdns-03.org", "ns-4.awsdns-04.co.uk"]
      }
    }
  }

  expect_failures = [output.zone_name]
}

run "sandbox_env_rejects_delegations" {
  command = plan

  variables {
    env = "sandbox"
    delegations = {
      dev = {
        name_servers = ["ns-1.awsdns-01.com", "ns-2.awsdns-02.net", "ns-3.awsdns-03.org", "ns-4.awsdns-04.co.uk"]
      }
    }
  }

  expect_failures = [output.zone_name]
}

run "passes_query_log_retention_and_deletion_protection" {
  command = plan

  variables {
    query_log_retention_days      = 1096
    query_log_deletion_protection = false
  }

  assert {
    condition     = module.query_log.retention_days == 1096
    error_message = "query_log_retention_days がクエリログの Log Group に届いていません。"
  }
  assert {
    condition     = module.query_log.deletion_protection == false
    error_message = "query_log_deletion_protection = false がクエリログの Log Group に届いていません。"
  }
}

run "passes_query_log_deletion_protection_enabled" {
  command = plan

  assert {
    condition     = module.query_log.deletion_protection == true
    error_message = "query_log_deletion_protection = true がクエリログの Log Group に届いていません。"
  }
}

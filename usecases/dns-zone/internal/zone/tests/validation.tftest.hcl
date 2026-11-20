mock_provider "aws" {
  mock_data "aws_caller_identity" {
    defaults = { account_id = "123456789012" }
  }
  mock_data "aws_partition" {
    defaults = { partition = "aws" }
  }
}

variables {
  name                = "tabp-prd"
  zone_name           = "example.com"
  query_log_group_arn = "arn:aws:logs:us-east-1:123456789012:log-group:/aws/route53/example.com"
  dnssec_key_arn      = "arn:aws:kms:us-east-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
  notify_topic_arn    = "arn:aws:sns:us-east-1:123456789012:tabp-prd-ops-notification-us-east-1"
  delegations = {
    dev = {
      name_servers = ["ns-1.awsdns-01.com", "ns-2.awsdns-02.net", "ns-3.awsdns-03.org", "ns-4.awsdns-04.co.uk"]
      ds_record    = "12345 13 2 0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF"
    }
    stg = {
      name_servers = ["ns-5.awsdns-05.com", "ns-6.awsdns-06.net", "ns-7.awsdns-07.org", "ns-8.awsdns-08.co.uk"]
    }
  }
}

run "rejects_dnssec_key_outside_us_east_1" {
  command = plan

  variables {
    dnssec_key_arn = "arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
  }

  expect_failures = [aws_route53_key_signing_key.this]
}

run "rejects_dnssec_key_in_other_account" {
  command = plan

  variables {
    dnssec_key_arn = "arn:aws:kms:us-east-1:210987654321:key/1234abcd-12ab-34cd-56ef-1234567890ab"
  }

  expect_failures = [aws_route53_key_signing_key.this]
}

run "rejects_topic_outside_us_east_1" {
  command = plan

  variables {
    notify_topic_arn = "arn:aws:sns:ap-northeast-1:123456789012:tabp-prd-ops-notification"
  }

  expect_failures = [aws_cloudwatch_metric_alarm.dnssec]
}

run "rejects_topic_in_other_account" {
  command = plan

  variables {
    notify_topic_arn = "arn:aws:sns:us-east-1:210987654321:tabp-prd-ops-notification"
  }

  expect_failures = [aws_cloudwatch_metric_alarm.dnssec]
}

run "rejects_name_shorter_than_3_without_hyphens" {
  command = plan

  variables {
    name = "a-b"
  }

  expect_failures = [var.name]
}

run "name_accepts_3_characters_after_removing_hyphens" {
  command = plan

  variables {
    name = "a-bc"
  }

  assert {
    condition     = aws_route53_key_signing_key.this.name == "abc"
    error_message = "a-bc の KSK の名前が abc ではありません。"
  }
}

run "name_rejects_uppercase" {
  command = plan

  variables {
    name = "Tabp-prd"
  }

  expect_failures = [var.name]
}

run "name_rejects_101_characters" {
  command = plan

  variables {
    name = join("", [for i in range(101) : "a"])
  }

  expect_failures = [var.name]
}

run "rejects_dnssec_key_alias_arn" {
  command = plan

  variables {
    dnssec_key_arn = "arn:aws:kms:us-east-1:123456789012:alias/x"
  }

  expect_failures = [aws_route53_key_signing_key.this]
}

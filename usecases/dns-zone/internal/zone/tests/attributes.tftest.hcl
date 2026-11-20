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

run "zone_is_public_and_not_force_destroyed" {
  command = plan

  assert {
    condition     = aws_route53_zone.this.name == "example.com"
    error_message = "zone の名前が zone_name ではありません。"
  }
  assert {
    condition     = aws_route53_zone.this.force_destroy == false
    error_message = "force_destroy が false ではありません（管理外の record を消します）。"
  }
  assert {
    condition     = aws_route53_zone.this.enable_accelerated_recovery == false
    error_message = "enable_accelerated_recovery が false ではありません。"
  }
  assert {
    condition     = length(aws_route53_zone.this.vpc) == 0 && aws_route53_zone.this.delegation_set_id == null
    error_message = "zone が private（vpc）か、reusable delegation set を持っています。"
  }
}

run "logs_queries_and_signs_the_zone" {
  command = plan

  assert {
    condition     = aws_route53_query_log.this.cloudwatch_log_group_arn == var.query_log_group_arn
    error_message = "query log が query_log_group_arn の Log Group を指していません。"
  }
  assert {
    condition     = aws_route53_key_signing_key.this.key_management_service_arn == var.dnssec_key_arn && aws_route53_key_signing_key.this.status == "ACTIVE"
    error_message = "KSK が dnssec_key_arn の鍵で ACTIVE になっていません。"
  }
  assert {
    condition     = aws_route53_key_signing_key.this.name == "tabpprd"
    error_message = "KSK の名前が name からハイフンを除いたものではありません。"
  }
  assert {
    condition     = aws_route53_hosted_zone_dnssec.this.signing_status == "SIGNING"
    error_message = "DNSSEC の署名が有効になっていません。"
  }
}

run "writes_delegation_ns_and_ds_only" {
  command = plan

  assert {
    condition     = toset(keys(aws_route53_record.delegation_ns)) == toset(["dev", "stg"])
    error_message = "委任の NS が delegations の key ごとに1つありません。"
  }
  assert {
    condition = alltrue([for k, r in aws_route53_record.delegation_ns :
      r.name == "${k}.example.com" && r.type == "NS" && r.ttl == 3600 && r.allow_overwrite == false
    ])
    error_message = "委任の NS の名前・型・TTL・allow_overwrite が想定と違います。"
  }
  assert {
    condition     = toset(aws_route53_record.delegation_ns["stg"].records) == toset(var.delegations.stg.name_servers)
    error_message = "委任の NS の値が delegations の name_servers ではありません。"
  }
  assert {
    condition     = toset(keys(aws_route53_record.delegation_ds)) == toset(["dev"])
    error_message = "DS が ds_record を持つ委任だけにありません。"
  }
  assert {
    condition = (
      aws_route53_record.delegation_ds["dev"].name == "dev.example.com" &&
      aws_route53_record.delegation_ds["dev"].type == "DS" &&
      aws_route53_record.delegation_ds["dev"].ttl == 300 &&
      aws_route53_record.delegation_ds["dev"].allow_overwrite == false &&
      toset(aws_route53_record.delegation_ds["dev"].records) == toset([var.delegations.dev.ds_record])
    )
    error_message = "委任の DS の名前・型・TTL・値が想定と違います。"
  }
}

run "no_records_without_delegations" {
  command = plan

  variables {
    name        = "tabp-dev"
    zone_name   = "dev.example.com"
    delegations = {}
  }

  assert {
    condition     = length(aws_route53_record.delegation_ns) == 0 && length(aws_route53_record.delegation_ds) == 0
    error_message = "delegations が空なのに record を書いています。"
  }
}

run "alarms_on_dnssec_failures_in_us_east_1" {
  command = plan

  assert {
    condition     = toset([for a in aws_cloudwatch_metric_alarm.dnssec : a.metric_name]) == toset(["DNSSECInternalFailure", "DNSSECKeySigningKeysNeedingAction"])
    error_message = "DNSSEC の2つの metric の alarm がありません。"
  }
  assert {
    condition = alltrue([for a in aws_cloudwatch_metric_alarm.dnssec :
      a.region == "us-east-1" && a.namespace == "AWS/Route53" &&
      a.comparison_operator == "GreaterThanThreshold" && a.threshold == 0 && a.actions_enabled == true
    ])
    error_message = "DNSSEC の alarm が us-east-1 の AWS/Route53 の 0 を超える条件になっていません。"
  }
  assert {
    condition = alltrue([for a in aws_cloudwatch_metric_alarm.dnssec :
      a.statistic == "Maximum" && a.period == 300 && a.evaluation_periods == 1 && a.treat_missing_data == "notBreaching"
    ])
    error_message = "DNSSEC の alarm の statistic・period・evaluation_periods・treat_missing_data が想定と違います。"
  }
  assert {
    condition = alltrue([for a in aws_cloudwatch_metric_alarm.dnssec :
      a.alarm_actions == toset([var.notify_topic_arn]) && a.ok_actions == toset([var.notify_topic_arn]) && a.insufficient_data_actions == toset([var.notify_topic_arn])
    ])
    error_message = "DNSSEC の alarm の3つの action が notify_topic_arn の1件ではありません。"
  }
  assert {
    condition     = aws_cloudwatch_metric_alarm.dnssec["internal_failure"].alarm_name == "tabp-prd-dns-internal-failure" && aws_cloudwatch_metric_alarm.dnssec["ksk_action"].alarm_name == "tabp-prd-dns-ksk-action"
    error_message = "alarm の名前が <name>-dns-<kind> ではありません。"
  }
}

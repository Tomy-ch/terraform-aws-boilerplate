# Policy Test の入力を作る（make tf-policy）。monitor の ARN を与えないと、subscription の monitor_arn_list に mock の乱数が入り、Provider の ARN の検査で落ちる。
mock_provider "aws" {
  override_during = plan

  mock_data "aws_caller_identity" {
    defaults = { account_id = "123456789012" }
  }
  mock_data "aws_partition" {
    defaults = { partition = "aws" }
  }
}

override_resource {
  target          = module.finops_admin.module.cost_anomaly.aws_ce_anomaly_monitor.this
  override_during = plan
  values          = { arn = "arn:aws:ce::123456789012:anomalymonitor/1234abcd-12ab-34cd-56ef-1234567890ab" }
}

override_resource {
  target          = module.finops_sandbox.module.cost_anomaly.aws_ce_anomaly_monitor.this
  override_during = plan
  values          = { arn = "arn:aws:ce::123456789012:anomalymonitor/2234abcd-12ab-34cd-56ef-1234567890ab" }
}

run "policy_input" {
  command = plan
}

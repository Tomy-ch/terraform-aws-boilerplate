# 名前の組み立てと、内部の module への値の受け渡し。Key Policy と Topic Policy の文書の一致は Policy Test（plan.ops_notification）が見る。
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

override_resource {
  target = module.kms.aws_kms_key.this
  values = { arn = "arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab" }
}

# 取り違えが見えるよう、reference とは別の値を渡す。
variables {
  project                  = "t4bp"
  env                      = "dev"
  chat                     = { slack = { team_id = "T0000000001", channel_id = "C0000000002" } }
  permissions_boundary_arn = "arn:aws:iam::123456789012:policy/t4bp-dev-workload-boundary"
}

run "names" {
  command = plan

  assert {
    condition     = module.kms.alias_name == "alias/t4bp-dev-ops-notification"
    error_message = "鍵の alias が alias/<project>-<env>-ops-notification になっていません。"
  }

  assert {
    condition     = module.heartbeat.role_arn == "arn:aws:iam::123456789012:role/t4bp-dev-ops-heartbeat"
    error_message = "心拍の role が <project>-<env>-ops-heartbeat になっていません。"
  }
}

run "fixed_publishers" {
  command = plan

  assert {
    condition     = local.publishers == toset(["budgets", "cloudwatch", "costalerts", "events"])
    error_message = "送り手が budgets・cloudwatch・costalerts・events の4つになっていません（ops-notification/ADR-0101）。"
  }

  assert {
    condition = local.key_users == {
      roles    = [{ arn = "arn:aws:iam::123456789012:role/t4bp-dev-ops-heartbeat", via = "sns", access = "read-write", topics = ["t4bp-dev-ops-notification"] }]
      services = { sns = [{ topic = "t4bp-dev-ops-notification", publishers = toset(["budgets", "cloudwatch", "costalerts", "events"]) }] }
    }
    error_message = "鍵の利用者が、心拍の role（via = sns）と、Topic と同じ名前・同じ送り手になっていません。"
  }
}

# 公開 output の名前と、それが返す値の配線を固定する（ADR-0202 決定4）。
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
  target = module.sns.aws_sns_topic.this
  values = { arn = "arn:aws:sns:ap-northeast-1:123456789012:tabp-sandbox-ops-notification" }
}

override_resource {
  target = module.kms.aws_kms_key.this
  values = { arn = "arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab" }
}

# mock の apply では、ARN を検証する引数へ渡る値を与える。
override_resource {
  target = module.heartbeat.aws_iam_role.this
  values = { arn = "arn:aws:iam::123456789012:role/tabp-sandbox-ops-heartbeat" }
}

override_resource {
  target = module.chat.aws_iam_role.this
  values = { arn = "arn:aws:iam::123456789012:role/tabp-sandbox-ops-chat" }
}

override_resource {
  target = module.chat.aws_iam_policy.read
  values = { arn = "arn:aws:iam::123456789012:policy/tabp-sandbox-ops-chat-read" }
}

variables {
  project                  = "tabp"
  env                      = "sandbox"
  chat                     = { slack = { team_id = "T07EA123LEP", channel_id = "C07EZ1ABC23" } }
  permissions_boundary_arn = "arn:aws:iam::123456789012:policy/tabp-sandbox-workload-boundary"
}

run "outputs" {
  command = apply

  assert {
    condition     = output.topic_arn == module.sns.topic_arn
    error_message = "topic_arn が modules/sns の Topic の ARN を返していません。"
  }
}

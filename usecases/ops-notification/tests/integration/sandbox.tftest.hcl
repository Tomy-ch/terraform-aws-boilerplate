# Integration Test。実 provider で sandbox のアカウントに複製を作り、確かめてから破棄する（ADR-0401 決定14-16）。
# 実行は人が行う（README の「検証方法」）。CI と make tf-test はこのディレクトリを走らせない。
#
#   terraform -chdir=usecases/ops-notification test -test-directory=tests/integration \
#     -var project=it<実行の識別子> -var 'chat={slack={team_id="<ID>",channel_id="<ID>"}}' \
#     -var permissions_boundary_arn=<boundary の ARN>
provider "aws" {
  region = "ap-northeast-1"

  default_tags {
    tags = { "tabp:run" = var.project }
  }
}

variables {
  env = "sandbox"
}

run "replica_name" {
  command = plan

  module {
    source = "./tests/integration/guard"
  }
}

run "replica" {
  command = apply

  assert {
    condition     = output.topic_arn == "arn:${split(":", output.topic_arn)[1]}:sns:ap-northeast-1:${split(":", output.topic_arn)[4]}:${var.project}-sandbox-ops-notification"
    error_message = "複製の Topic が <project>-sandbox-ops-notification の名前で ap-northeast-1 に作られていません。"
  }

  assert {
    condition     = output.us_east_1_topic_arn == "arn:${split(":", output.topic_arn)[1]}:sns:us-east-1:${split(":", output.topic_arn)[4]}:${var.project}-sandbox-ops-notification-us-east-1"
    error_message = "複製の us-east-1 の Topic が、region 引数で us-east-1 に <project>-sandbox-ops-notification-us-east-1 の名前で作られていません。"
  }
}

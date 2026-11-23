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
  target = aws_iam_role.this
  values = { arn = "arn:aws:iam::123456789012:role/tabp-sandbox-ops-heartbeat" }
}

variables {
  name                     = "tabp-sandbox-ops-heartbeat"
  us_east_1_name           = "tabp-sandbox-ops-us-east-1"
  env                      = "sandbox"
  topic_arn                = "arn:aws:sns:ap-northeast-1:123456789012:tabp-sandbox-ops-notification"
  us_east_1_topic_arn      = "arn:aws:sns:us-east-1:123456789012:tabp-sandbox-ops-notification-us-east-1"
  permissions_boundary_arn = "arn:aws:iam::123456789012:policy/tabp-sandbox-workload-boundary"
}

run "schedule" {
  command = plan

  assert {
    condition     = aws_scheduler_schedule_group.this.name == "tabp-sandbox-ops-heartbeat"
    error_message = "schedule group の名前が name になっていません。"
  }

  assert {
    condition     = aws_scheduler_schedule.this.name == "tabp-sandbox-ops-heartbeat" && aws_scheduler_schedule.this.group_name == "tabp-sandbox-ops-heartbeat"
    error_message = "schedule が専用の schedule group に、name の名前で置かれていません。"
  }

  assert {
    condition = [
      aws_scheduler_schedule.this.schedule_expression,
      aws_scheduler_schedule.this.schedule_expression_timezone,
      aws_scheduler_schedule.this.state,
      aws_scheduler_schedule.this.action_after_completion,
      ] == [
      "cron(0 9 * * ? *)",
      "Asia/Tokyo",
      "ENABLED",
      "NONE",
    ]
    error_message = "schedule が毎日 09:00（Asia/Tokyo）・有効・完了後に残す、になっていません。"
  }

  assert {
    condition     = aws_scheduler_schedule.this.flexible_time_window[0].mode == "OFF" && aws_scheduler_schedule.this.flexible_time_window[0].maximum_window_in_minutes == null
    error_message = "flexible_time_window が OFF になっていません。"
  }

  assert {
    condition     = aws_scheduler_schedule.this.kms_key_arn == null
    error_message = "schedule が KMS の鍵を名指しています（AWS owned key を使う）。"
  }

  assert {
    condition     = aws_scheduler_schedule.this.target[0].arn == var.topic_arn
    error_message = "target が Topic の ARN（SNS の Publish の templated target）になっていません。"
  }

  assert {
    condition = (
      aws_scheduler_schedule.this.target[0].retry_policy[0].maximum_event_age_in_seconds == 3600
      && aws_scheduler_schedule.this.target[0].retry_policy[0].maximum_retry_attempts == 185
    )
    error_message = "retry_policy が 3600 秒・185 回になっていません。"
  }

  assert {
    condition     = length(aws_scheduler_schedule.this.target[0].dead_letter_config) == 0
    error_message = "target に DLQ が置かれています。"
  }

  assert {
    condition = jsondecode(aws_scheduler_schedule.this.target[0].input) == {
      version = "1.0"
      source  = "custom"
      content = {
        textType    = "client-markdown"
        title       = "ops-notification の心拍（ap-northeast-1。警報ではありません）"
        description = "通知経路が働いていることを示す毎日の心拍です。対応は要りません。\nenv: sandbox / region: ap-northeast-1 / 予定時刻: <aws.scheduler.scheduled-time>"
      }
    }
    error_message = "心拍の本文が、Chatbot の custom notification の形（env・region・予定時刻）になっていません。"
  }
}

# us-east-1 の Topic の心拍は us-east-1 に置き、本文の region で provider の region の心拍と区別する。
run "us_east_1_schedule" {
  command = plan

  assert {
    condition     = aws_scheduler_schedule_group.us_east_1.region == "us-east-1" && aws_scheduler_schedule_group.us_east_1.name == "tabp-sandbox-ops-us-east-1"
    error_message = "us-east-1 の schedule group が、us-east-1 に us_east_1_name の名前で置かれていません。"
  }

  assert {
    condition = (
      aws_scheduler_schedule.us_east_1.region == "us-east-1"
      && aws_scheduler_schedule.us_east_1.name == "tabp-sandbox-ops-us-east-1"
      && aws_scheduler_schedule.us_east_1.group_name == "tabp-sandbox-ops-us-east-1"
    )
    error_message = "us-east-1 の schedule が、us-east-1 の schedule group に us_east_1_name の名前で置かれていません。"
  }

  assert {
    condition = [
      aws_scheduler_schedule.us_east_1.schedule_expression,
      aws_scheduler_schedule.us_east_1.schedule_expression_timezone,
      aws_scheduler_schedule.us_east_1.state,
      aws_scheduler_schedule.us_east_1.action_after_completion,
      aws_scheduler_schedule.us_east_1.flexible_time_window[0].mode,
      aws_scheduler_schedule.us_east_1.target[0].retry_policy[0].maximum_event_age_in_seconds,
      aws_scheduler_schedule.us_east_1.target[0].retry_policy[0].maximum_retry_attempts,
      length(aws_scheduler_schedule.us_east_1.target[0].dead_letter_config),
      ] == [
      "cron(0 9 * * ? *)",
      "Asia/Tokyo",
      "ENABLED",
      "NONE",
      "OFF",
      3600,
      185,
      0,
    ]
    error_message = "us-east-1 の schedule の時刻・状態・retry・DLQ が、provider の region の schedule と同じになっていません。"
  }

  assert {
    condition     = aws_scheduler_schedule.us_east_1.kms_key_arn == null
    error_message = "us-east-1 の schedule が KMS の鍵を名指しています（AWS owned key を使う）。"
  }

  assert {
    condition     = aws_scheduler_schedule.us_east_1.target[0].arn == var.us_east_1_topic_arn
    error_message = "us-east-1 の schedule の target が us-east-1 の Topic になっていません。"
  }

  assert {
    condition = jsondecode(aws_scheduler_schedule.us_east_1.target[0].input) == {
      version = "1.0"
      source  = "custom"
      content = {
        textType    = "client-markdown"
        title       = "ops-notification の心拍（us-east-1。警報ではありません）"
        description = "通知経路が働いていることを示す毎日の心拍です。対応は要りません。\nenv: sandbox / region: us-east-1 / 予定時刻: <aws.scheduler.scheduled-time>"
      }
    }
    error_message = "us-east-1 の心拍の本文が、region に us-east-1 を載せた custom notification の形になっていません。"
  }

  assert {
    condition     = strcontains(aws_scheduler_schedule.us_east_1.target[0].input, "<aws.scheduler.scheduled-time>") && !strcontains(aws_scheduler_schedule.us_east_1.target[0].input, "\\u003c")
    error_message = "us-east-1 の心拍の生の input に <aws.scheduler.scheduled-time> がそのまま入っていません。"
  }
}

# Scheduler は生の input の中のトークンを置換する。jsonencode が < > を \u003c \u003e に変えると置換されない。
run "raw_input_keeps_context_attribute" {
  command = plan

  assert {
    condition     = strcontains(aws_scheduler_schedule.this.target[0].input, "<aws.scheduler.scheduled-time>")
    error_message = "生の input に <aws.scheduler.scheduled-time> がそのまま入っていません。"
  }

  assert {
    condition     = !strcontains(aws_scheduler_schedule.this.target[0].input, "\\u003c") && !strcontains(aws_scheduler_schedule.this.target[0].input, "\\u003e")
    error_message = "生の input に < > のエスケープが残っています。"
  }
}

run "message_names_env" {
  command = plan

  variables {
    name = "tabp-prd-ops-heartbeat"
    env  = "prd"
  }

  assert {
    condition     = strcontains(jsondecode(aws_scheduler_schedule.this.target[0].input).content.description, "env: prd / ")
    error_message = "心拍の本文が env を名指していません。"
  }
}

# role の ARN は plan では決まらないので、mock の apply で target と出力を突き合わせる。
run "role_wiring" {
  command = apply

  assert {
    condition     = aws_scheduler_schedule.this.target[0].role_arn == aws_iam_role.this.arn && aws_scheduler_schedule.us_east_1.target[0].role_arn == aws_iam_role.this.arn
    error_message = "2つの schedule の target の role_arn が心拍の role になっていません。"
  }

  assert {
    condition     = output.role_arn == "arn:aws:iam::123456789012:role/tabp-sandbox-ops-heartbeat"
    error_message = "role_arn の出力が、名前から組んだ role の ARN になっていません。"
  }
}

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
  env                      = "sandbox"
  topic_arn                = "arn:aws:sns:ap-northeast-1:123456789012:tabp-sandbox-ops-notification"
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
        title       = "ops-notification の心拍（警報ではありません）"
        description = "通知経路が働いていることを示す毎日の心拍です。対応は要りません。\nenv: sandbox / region: ap-northeast-1 / 予定時刻: <aws.scheduler.scheduled-time>"
      }
    }
    error_message = "心拍の本文が、Chatbot の custom notification の形（env・region・予定時刻）になっていません。"
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
    condition     = aws_scheduler_schedule.this.target[0].role_arn == aws_iam_role.this.arn
    error_message = "target の role_arn が心拍の role になっていません。"
  }

  assert {
    condition     = output.role_arn == "arn:aws:iam::123456789012:role/tabp-sandbox-ops-heartbeat"
    error_message = "role_arn の出力が、名前から組んだ role の ARN になっていません。"
  }
}

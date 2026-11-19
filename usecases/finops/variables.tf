variable "project" {
  description = "AWS 資源の名前の先頭に置くプロジェクトの token。予算の名前は `<project>-<env>-monthly-cost` になる。"
  type        = string
  nullable    = false

  # ハイフンを許すと `<project>-<env>` の境目を名前から読めなくなる。
  validation {
    condition     = can(regex("^[a-z0-9]+$", var.project))
    error_message = "project は英小文字と数字だけにしてください。"
  }

  # 最も長い予算の名前 `<project>-log-archive-monthly-cost` を Budgets の上限の100文字に収める。
  validation {
    condition     = length(var.project) <= 75
    error_message = "project は75文字以内にしてください。"
  }
}

variable "env" {
  description = "finops を置くアカウントの env の token。管理アカウントは `admin`。"
  type        = string
  nullable    = false

  validation {
    condition     = contains(["admin", "security", "log-archive", "sandbox", "dev", "stg", "prd"], var.env)
    error_message = "env は admin / security / log-archive / sandbox / dev / stg / prd のいずれかにしてください。"
  }
}

variable "notification_topic_arn" {
  description = "予算と異常の通知を送る SNS の Topic の ARN（同じアカウントの `ops-notification` の `topic_arn`）。Topic は `budgets.amazonaws.com` と `costalerts.amazonaws.com` の発行を許していなければならない。"
  type        = string
  nullable    = false

  validation {
    condition     = can(regex("^arn:aws[a-z-]*:sns:[a-z0-9-]+:[0-9]{12}:[A-Za-z0-9_-]{1,256}$", var.notification_topic_arn))
    error_message = "notification_topic_arn は SNS の Topic の ARN（arn:<partition>:sns:<region>:<account>:<name>）にしてください。FIFO の Topic は受けません。"
  }
}

variable "monthly_budget_usd" {
  description = "このアカウントの月の予算の額（USD）。管理アカウントでは組織全体の費用に対する予算になる。小数は2桁まで。"
  type        = number
  nullable    = false

  validation {
    condition     = var.monthly_budget_usd > 0 && can(regex("^[0-9]+(\\.[0-9]{1,2})?$", tostring(var.monthly_budget_usd)))
    error_message = "monthly_budget_usd は0より大きく、小数2桁までの数にしてください。"
  }
}

variable "anomaly_threshold_usd" {
  description = "通知する異常の影響額（USD）の下限。影響額がこの額以上で、かつ期待値に対して40%以上の異常を通知する。"
  type        = number
  nullable    = false

  validation {
    condition     = var.anomaly_threshold_usd > 0 && var.anomaly_threshold_usd <= 10000000000
    error_message = "anomaly_threshold_usd は0より大きく、10000000000 以下にしてください。"
  }
}

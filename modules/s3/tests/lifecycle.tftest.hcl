mock_provider "aws" {
  mock_data "aws_caller_identity" {
    defaults = { account_id = "123456789012" }
  }
  mock_data "aws_region" {
    defaults = { region = "ap-northeast-1" }
  }

  # prefix は Optional かつ Computed で、mock の apply は設定しない文字列を乱数で埋める。空を既定にして、
  # 設定した prefix だけが値として残るようにする。
  mock_resource "aws_s3_bucket_lifecycle_configuration" {
    defaults = { rule = { prefix = "", filter = { prefix = "" } } }
  }
}

variables {
  name                              = "tabp-mgmt-tfstate"
  noncurrent_version_retention_days = 90
}

run "lifecycle_rule" {
  command = plan

  assert {
    condition     = [for r in aws_s3_bucket_lifecycle_configuration.this.rule : r.id] == ["expire-noncurrent-versions"]
    error_message = "lifecycle の規則が1つではありません。"
  }

  assert {
    condition     = aws_s3_bucket_lifecycle_configuration.this.rule[0].status == "Enabled" && length(aws_s3_bucket_lifecycle_configuration.this.rule[0].filter) == 1
    error_message = "lifecycle の規則が、filter を1つ持つ有効な規則になっていません。"
  }

  assert {
    condition     = length(one(aws_s3_bucket_lifecycle_configuration.this.rule[0].filter).and) == 0 && length(one(aws_s3_bucket_lifecycle_configuration.this.rule[0].filter).tag) == 0
    error_message = "lifecycle の規則の filter が tag で対象を絞っています。"
  }

  assert {
    condition     = one(aws_s3_bucket_lifecycle_configuration.this.rule[0].noncurrent_version_expiration).noncurrent_days == 90
    error_message = "上書きされた版が noncurrent_version_retention_days で消えません。"
  }

  assert {
    condition     = one(aws_s3_bucket_lifecycle_configuration.this.rule[0].expiration).expired_object_delete_marker == true
    error_message = "失効した delete marker を消していません。"
  }

  assert {
    condition     = one(aws_s3_bucket_lifecycle_configuration.this.rule[0].expiration).date == null
    error_message = "現行版を日付で失効させています。"
  }

  assert {
    condition     = one(aws_s3_bucket_lifecycle_configuration.this.rule[0].noncurrent_version_expiration).newer_noncurrent_versions == null
    error_message = "版の数で上書きされた版を残しています。"
  }

  assert {
    condition     = one(aws_s3_bucket_lifecycle_configuration.this.rule[0].abort_incomplete_multipart_upload).days_after_initiation == 1
    error_message = "未完了の multipart upload が1日で中止されません。"
  }

  assert {
    condition     = length(aws_s3_bucket_lifecycle_configuration.this.rule[0].transition) == 0 && length(aws_s3_bucket_lifecycle_configuration.this.rule[0].noncurrent_version_transition) == 0
    error_message = "storage class を移す規則があります。"
  }
}

# days・prefix・bucket は plan では決まらない。mock の apply は設定しない number を 0 に、prefix を上の既定の空にし、
# 設定した値はそのまま残す。
run "whole_bucket_without_current_version_expiration" {
  command = apply

  assert {
    condition     = aws_s3_bucket_lifecycle_configuration.this.bucket == aws_s3_bucket.this.id
    error_message = "lifecycle がこの module の Bucket に付いていません。"
  }

  assert {
    condition     = aws_s3_bucket_lifecycle_configuration.this.rule[0].prefix == "" && one(aws_s3_bucket_lifecycle_configuration.this.rule[0].filter).prefix == ""
    error_message = "lifecycle の規則が prefix で対象を絞っています。"
  }

  assert {
    condition     = one(aws_s3_bucket_lifecycle_configuration.this.rule[0].filter).object_size_greater_than == 0 && one(aws_s3_bucket_lifecycle_configuration.this.rule[0].filter).object_size_less_than == 0
    error_message = "lifecycle の規則が object の大きさで対象を絞っています。"
  }

  assert {
    condition     = one(aws_s3_bucket_lifecycle_configuration.this.rule[0].expiration).days == 0
    error_message = "現行版を日数で失効させています。"
  }
}

run "retention_days_other_value" {
  command = plan

  variables {
    noncurrent_version_retention_days = 3
  }

  assert {
    condition     = output.noncurrent_version_retention_days == 3
    error_message = "noncurrent_version_retention_days が規則の日数に届いていません。"
  }
}

run "retention_days_minimum" {
  command = plan

  variables {
    noncurrent_version_retention_days = 1
  }

  assert {
    condition     = one(aws_s3_bucket_lifecycle_configuration.this.rule[0].noncurrent_version_expiration).noncurrent_days == 1
    error_message = "noncurrent_version_retention_days = 1 が規則の日数になっていません。"
  }
}

run "retention_days_zero" {
  command = plan

  variables {
    noncurrent_version_retention_days = 0
  }

  expect_failures = [var.noncurrent_version_retention_days]
}

run "retention_days_negative" {
  command = plan

  variables {
    noncurrent_version_retention_days = -1
  }

  expect_failures = [var.noncurrent_version_retention_days]
}

run "retention_days_fraction" {
  command = plan

  variables {
    noncurrent_version_retention_days = 1.5
  }

  expect_failures = [var.noncurrent_version_retention_days]
}

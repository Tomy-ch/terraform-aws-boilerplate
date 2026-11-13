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

  # prefix・大きさ・days は Optional かつ Computed で、mock の apply は設定しない値を乱数か 0 で埋める。
  # 設定しうる値と重ならない既定を置き、設定した値だけが既定から外れるようにする。
  mock_resource "aws_s3_bucket_lifecycle_configuration" {
    defaults = {
      rule = {
        prefix     = ""
        filter     = { prefix = "", object_size_greater_than = -1, object_size_less_than = -1 }
        expiration = { days = -1 }
      }
    }
  }
}

variables {
  kms_key_arn                       = "arn:aws:kms:ap-northeast-1:123456789012:key/1234abcd-12ab-34cd-56ef-1234567890ab"
  name                              = "tabp-mgmt-tfstate"
  noncurrent_version_retention_days = 90
  object_writers = {
    identity = {
      role_arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-apply-identity"
      keys     = ["identity/terraform.tfstate"]
      access   = "write"
    }
  }
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
    error_message = "lifecycle の規則の filter が and か tag で対象を絞っています。"
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

# days・prefix・大きさ・bucket は plan では決まらないので、mock の apply で上の既定のままであることを見る。
run "whole_bucket_without_current_version_expiration" {
  command = apply

  assert {
    condition     = aws_s3_bucket_lifecycle_configuration.this.bucket == aws_s3_bucket.this.id
    error_message = "lifecycle がこの module の Bucket に付いていません。"
  }

  assert {
    condition     = one(aws_s3_bucket_lifecycle_configuration.this.rule[0].filter).prefix == ""
    error_message = "lifecycle の規則の filter が prefix で対象を絞っています。"
  }

  # rule 直下の prefix は非推奨の属性で、偽になると Terraform は assert の診断を出す前に panic する。落ちること自体は変わらない。
  assert {
    condition     = aws_s3_bucket_lifecycle_configuration.this.rule[0].prefix == ""
    error_message = "lifecycle の規則が rule の prefix で対象を絞っています。"
  }

  assert {
    condition     = one(aws_s3_bucket_lifecycle_configuration.this.rule[0].filter).object_size_greater_than == -1 && one(aws_s3_bucket_lifecycle_configuration.this.rule[0].filter).object_size_less_than == -1
    error_message = "lifecycle の規則が object の大きさで対象を絞っています。"
  }

  assert {
    condition     = one(aws_s3_bucket_lifecycle_configuration.this.rule[0].expiration).days == -1
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

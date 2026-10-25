# 公開 output の名前と、それが返す値の配線を固定する。変えるなら公開契約の変更として扱う（ADR-0202 決定4）。
mock_provider "aws" {
  mock_data "aws_caller_identity" {
    defaults = { account_id = "123456789012" }
  }
  mock_data "aws_region" {
    defaults = { region = "ap-northeast-1" }
  }
}

variables {
  project = "tabp"
  env     = "admin"
}

run "outputs" {
  command = apply

  assert {
    condition     = can(regex("^tabp-admin-tfstate-[0-9]{12}-[a-z0-9-]+-an$", output.bucket_name))
    error_message = "bucket_name が state Bucket の完全な名前を返していません。"
  }

  assert {
    condition     = output.bucket_arn == module.s3.bucket_arn
    error_message = "bucket_arn が state Bucket の ARN を返していません。"
  }
}

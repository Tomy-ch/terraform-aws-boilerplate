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

variables {
  name = "tabp-admin-tfstate"
  users = {
    roles = [{ arn = "arn:aws:iam::123456789012:role/tabp-admin-ci-plan", via = "s3" }]
  }
}

run "key_invariants" {
  command = plan

  assert {
    condition     = aws_kms_key.this.enable_key_rotation == true && aws_kms_key.this.rotation_period_in_days == 365
    error_message = "鍵の自動 rotation が 365 日で有効になっていません。"
  }

  assert {
    condition     = aws_kms_key.this.deletion_window_in_days == 30
    error_message = "削除の猶予が 30 日になっていません。"
  }

  assert {
    condition     = aws_kms_key.this.key_usage == "ENCRYPT_DECRYPT" && aws_kms_key.this.customer_master_key_spec == "SYMMETRIC_DEFAULT"
    error_message = "鍵が対称の暗号化鍵になっていません。"
  }

  assert {
    condition     = aws_kms_key.this.multi_region == false && aws_kms_key.this.is_enabled == true
    error_message = "鍵が単一 region で有効になっていません。"
  }

  assert {
    condition     = aws_kms_key.this.bypass_policy_lockout_safety_check == false && aws_kms_key_policy.this.bypass_policy_lockout_safety_check == false
    error_message = "lockout の安全チェックを外しています。"
  }

  assert {
    condition     = aws_kms_key.this.tags == null
    error_message = "鍵に tag を付けています。tag は共通の単一入力でだけ付けます（ADR-0204 決定5）。"
  }

  assert {
    condition     = aws_kms_alias.this.name == "alias/tabp-admin-tfstate"
    error_message = "alias の名前が alias/<name> になっていません。"
  }
}

# key_id と ARN は plan では決まらないので、mock の apply で確かめる。
run "attachments_and_output" {
  command = apply

  assert {
    condition     = aws_kms_alias.this.target_key_id == aws_kms_key.this.key_id
    error_message = "alias がこの module の鍵を指していません。"
  }

  assert {
    condition     = aws_kms_key_policy.this.key_id == aws_kms_key.this.id
    error_message = "Key Policy がこの module の鍵に付いていません。"
  }

  assert {
    condition     = output.key_arn == aws_kms_key.this.arn
    error_message = "key_arn が鍵の ARN を返していません。"
  }
}

mock_provider "aws" {}

variables {
  security_contact = {
    name          = "Security Team"
    title         = "Security"
    email_address = "security@example.com"
    phone_number  = "+81 3 0000 0000"
  }
  manage_password_policy = true
}

run "member_account" {
  command = plan

  assert {
    condition = (
      aws_s3_account_public_access_block.this.block_public_acls &&
      aws_s3_account_public_access_block.this.block_public_policy &&
      aws_s3_account_public_access_block.this.ignore_public_acls &&
      aws_s3_account_public_access_block.this.restrict_public_buckets
    )
    error_message = "アカウントの S3 Public Access Block の4項目が true になっていません（S3.1）。"
  }

  assert {
    condition = (
      aws_account_alternate_contact.security.alternate_contact_type == "SECURITY" &&
      aws_account_alternate_contact.security.name == "Security Team" &&
      aws_account_alternate_contact.security.title == "Security" &&
      aws_account_alternate_contact.security.email_address == "security@example.com" &&
      aws_account_alternate_contact.security.phone_number == "+81 3 0000 0000"
    )
    error_message = "security_contact が SECURITY の代替の連絡先に渡っていません（Account.1）。"
  }

  # organization-baseline の break-glass が管理アカウントに置く値と同じ（IAM.7・15・16）。
  assert {
    condition = (
      aws_iam_account_password_policy.this[0].minimum_password_length == 14 &&
      aws_iam_account_password_policy.this[0].password_reuse_prevention == 24 &&
      aws_iam_account_password_policy.this[0].require_lowercase_characters &&
      aws_iam_account_password_policy.this[0].require_uppercase_characters &&
      aws_iam_account_password_policy.this[0].require_numbers &&
      aws_iam_account_password_policy.this[0].require_symbols &&
      aws_iam_account_password_policy.this[0].max_password_age == 90 &&
      aws_iam_account_password_policy.this[0].allow_users_to_change_password &&
      !aws_iam_account_password_policy.this[0].hard_expiry
    )
    error_message = "password policy の値が、organization-baseline の break-glass と同じになっていません。"
  }
}

run "management_account" {
  command = plan

  variables {
    manage_password_policy = false
  }

  assert {
    condition     = length(aws_iam_account_password_policy.this) == 0
    error_message = "manage_password_policy = false で password policy を置いています。"
  }
}

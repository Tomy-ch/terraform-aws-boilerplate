mock_provider "aws" {
  mock_data "aws_caller_identity" {
    defaults = { account_id = "123456789012" }
  }
  mock_data "aws_partition" {
    defaults = { partition = "aws" }
  }
}

variables {
  name      = "tabp-prd-dns-dnssec"
  zone_name = "example.com"
}

run "key_is_an_ecc_signing_key_in_us_east_1" {
  command = plan

  assert {
    condition     = aws_kms_key.this.region == "us-east-1" && aws_kms_alias.this.region == "us-east-1" && aws_kms_key_policy.this.region == "us-east-1"
    error_message = "鍵・alias・Key Policy が us-east-1 に置かれていません。"
  }
  assert {
    condition     = aws_kms_key.this.key_usage == "SIGN_VERIFY" && aws_kms_key.this.customer_master_key_spec == "ECC_NIST_P256"
    error_message = "鍵が ECC_NIST_P256 の署名鍵ではありません（Route 53 の KSK の要件）。"
  }
  assert {
    condition     = aws_kms_key.this.multi_region == false && aws_kms_key.this.enable_key_rotation == false && aws_kms_key.this.deletion_window_in_days == 30
    error_message = "鍵の multi_region・rotation・削除の猶予が想定と違います。"
  }
  assert {
    condition     = aws_kms_key.this.bypass_policy_lockout_safety_check == false && aws_kms_key_policy.this.bypass_policy_lockout_safety_check == false
    error_message = "lockout の安全チェックを外しています。"
  }
  assert {
    condition     = aws_kms_alias.this.name == "alias/tabp-prd-dns-dnssec" && output.alias_name == "alias/tabp-prd-dns-dnssec"
    error_message = "alias が alias/<name> ではありません。"
  }
}

run "key_policy_names_route53_dnssec_only" {
  command = plan

  assert {
    condition = { for st in jsondecode(aws_kms_key_policy.this.policy).Statement : st.Sid => st }["Route53DnssecService"] == {
      Sid       = "Route53DnssecService"
      Effect    = "Allow"
      Principal = { Service = "dnssec-route53.amazonaws.com" }
      Action    = ["kms:DescribeKey", "kms:GetPublicKey", "kms:Sign"]
      Resource  = "*"
      Condition = { StringEquals = { "aws:SourceAccount" = "123456789012" } }
    }
    error_message = "Route 53 の DNSSEC の文が、DescribeKey・GetPublicKey・Sign をアカウントで限った形になっていません。"
  }
  assert {
    condition = { for st in jsondecode(aws_kms_key_policy.this.policy).Statement : st.Sid => st }["Route53DnssecGrant"] == {
      Sid       = "Route53DnssecGrant"
      Effect    = "Allow"
      Principal = { Service = "dnssec-route53.amazonaws.com" }
      Action    = "kms:CreateGrant"
      Resource  = "*"
      Condition = {
        Bool         = { "kms:GrantIsForAWSResource" = "true" }
        StringEquals = { "aws:SourceAccount" = "123456789012" }
      }
    }
    error_message = "Route 53 の DNSSEC の grant の文が、GrantIsForAWSResource とアカウントで限った形になっていません。"
  }
  assert {
    condition     = toset(keys({ for st in jsondecode(aws_kms_key_policy.this.policy).Statement : st.Sid => st })) == toset(["AccountKeyAdministration", "Route53DnssecService", "Route53DnssecGrant"])
    error_message = "Key Policy が、管理の委任と Route 53 の DNSSEC の2文のほかの文を持っています。"
  }
}

run "account_delegation_is_exactly_the_administration_actions" {
  command = plan

  assert {
    condition     = { for st in jsondecode(aws_kms_key_policy.this.policy).Statement : st.Sid => st }["AccountKeyAdministration"].Principal == { AWS = "arn:aws:iam::123456789012:root" }
    error_message = "管理の委任の相手がアカウントの root ではありません。"
  }
  assert {
    condition     = { for st in jsondecode(aws_kms_key_policy.this.policy).Statement : st.Sid => st }["AccountKeyAdministration"].Effect == "Allow" && { for st in jsondecode(aws_kms_key_policy.this.policy).Statement : st.Sid => st }["AccountKeyAdministration"].Resource == "*"
    error_message = "管理の委任が Allow の Resource * ではありません。"
  }
  assert {
    condition = toset({ for st in jsondecode(aws_kms_key_policy.this.policy).Statement : st.Sid => st }["AccountKeyAdministration"].Action) == toset([
      "kms:CancelKeyDeletion", "kms:CreateAlias", "kms:DeleteAlias", "kms:DescribeKey", "kms:DisableKey", "kms:EnableKey",
      "kms:GetKeyPolicy", "kms:ListGrants", "kms:ListKeyPolicies", "kms:ListResourceTags", "kms:PutKeyPolicy", "kms:RevokeGrant",
      "kms:ScheduleKeyDeletion", "kms:TagResource", "kms:UntagResource", "kms:UpdateAlias", "kms:UpdateKeyDescription",
    ])
    error_message = "管理の委任の Action が、鍵の管理の17個ちょうどではありません（暗号操作と grant を含めない。ADR-0301 決定12）。"
  }
}

run "key_arn_output_is_the_key_and_the_description_names_the_zone" {
  command = apply

  assert {
    condition     = output.key_arn == aws_kms_key.this.arn
    error_message = "key_arn が鍵の ARN ではありません。"
  }
  assert {
    condition     = aws_kms_key.this.description == "Route 53 DNSSEC KSK for example.com"
    error_message = "鍵の説明が zone の名前を含む形ではありません。"
  }
}

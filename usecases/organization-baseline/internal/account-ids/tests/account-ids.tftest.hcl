mock_provider "aws" {}

variables {
  project          = "tabp"
  account_ids      = { security = "222222222222", "log-archive" = "333333333333" }
  organization_arn = "arn:aws:organizations::111111111111:organization/o-exampleorg1"
}

run "parameter" {
  command = plan

  assert {
    condition = (
      aws_ssm_parameter.this.name == "/tabp/organization-baseline/account-ids"
      && aws_ssm_parameter.this.type == "String"
      && aws_ssm_parameter.this.tier == "Advanced"
      && jsondecode(aws_ssm_parameter.this.value) == { security = "222222222222", "log-archive" = "333333333333" }
    )
    error_message = "parameter は /<project>/organization-baseline/account-ids の advanced tier の String にし、値を env から ID への JSON にしてください。"
  }
}

run "share" {
  command = plan

  assert {
    condition     = aws_ram_resource_share.this.name == "tabp-account-ids" && aws_ram_resource_share.this.allow_external_principals == false
    error_message = "共有は <project>-account-ids にし、組織の外の principal を許さないでください。"
  }

  assert {
    condition     = aws_ram_principal_association.organization.principal == "arn:aws:organizations::111111111111:organization/o-exampleorg1"
    error_message = "共有の宛先は組織の ARN だけにしてください。"
  }
}

mock_provider "aws" {
  # state_key_arn の precondition は、鍵の ARN を data source の partition・region・account と比べる。
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

# role の ARN は plan では決まらないので、mock の apply で見る。
run "reference_roles" {
  command = apply

  assert {
    condition = length(toset([
      module.deployment_identity.plan_role_arn,
      module.deployment_identity.apply_role_arn,
      module.deployment_identity.apply_identity_role_arn,
      module.deployment_identity.unlock_identity_role_arn,
    ])) == 4
    error_message = "reference の値から4つの role が組まれていません。"
  }
}

mock_provider "aws" {
  # attachment の policy_arn は ARN の形を検証するので、mock の policy の ARN を ARN の形にする。
  mock_resource "aws_iam_policy" {
    defaults = { arn = "arn:aws:iam::123456789012:policy/mock" }
  }
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

mock_provider "aws" {
  alias = "security"

  # 値を与える理由は、上の無名の mock_provider "aws" と同じ。
  mock_resource "aws_iam_policy" {
    defaults = { arn = "arn:aws:iam::111111111111:policy/mock" }
  }
  mock_data "aws_caller_identity" {
    defaults = { account_id = "111111111111" }
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
      module.deployment_identity.unlock_role_arn,
    ])) == 5
    error_message = "reference の値から5つの role が組まれていません。"
  }
}

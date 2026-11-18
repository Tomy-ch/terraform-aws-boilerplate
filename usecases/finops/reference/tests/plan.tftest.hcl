mock_provider "aws" {
  mock_data "aws_caller_identity" {
    defaults = { account_id = "123456789012" }
  }
  mock_data "aws_partition" {
    defaults = { partition = "aws" }
  }
}

# usecase は出力を持たないので、reference の値で plan が通ることだけを見る。値の配線は usecase の tests/contract.tftest.hcl が見る。
run "reference_plan" {
  command = plan
}

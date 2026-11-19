# root が ops-notification に渡す値が、usecase の入力の検証と、role の permissions boundary が同じアカウントと
# partition の policy であることの precondition を通ることを見る。root から観測できるのは子の module の output
# だけなので、boundary の名前は見ない。
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

run "plan_passes_the_usecase_checks" {
  command = plan
}

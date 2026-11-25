run "aws_default_regions" {
  command = plan

  assert {
    condition     = keys(output.by_partition) == ["aws"]
    error_message = "集合を持つ partition が aws だけではありません。"
  }

  assert {
    condition     = length(output.by_partition["aws"]) == 17
    error_message = "aws の既定で有効なリージョンが17ではありません。"
  }

  assert {
    condition     = contains(output.by_partition["aws"], "ap-northeast-1") && contains(output.by_partition["aws"], "us-east-1")
    error_message = "aws の集合に、既定で有効な ap-northeast-1 か us-east-1 がありません。"
  }

  assert {
    condition     = !contains(output.by_partition["aws"], "af-south-1") && !contains(output.by_partition["aws"], "ap-east-1")
    error_message = "aws の集合に opt-in のリージョンがあります。"
  }
}

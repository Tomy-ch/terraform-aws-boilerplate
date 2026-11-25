run "aws_default_regions" {
  command = plan

  assert {
    condition     = keys(output.by_partition) == ["aws"]
    error_message = "集合を持つ partition が aws だけではありません。"
  }

  # AWS Account Management Reference Guide の既定で有効なリージョンの表との完全一致。
  assert {
    condition = output.by_partition["aws"] == toset([
      "ap-northeast-1", "ap-northeast-2", "ap-northeast-3", "ap-south-1", "ap-southeast-1", "ap-southeast-2",
      "ca-central-1", "eu-central-1", "eu-north-1", "eu-west-1", "eu-west-2", "eu-west-3", "sa-east-1",
      "us-east-1", "us-east-2", "us-west-1", "us-west-2",
    ])
    error_message = "aws の集合が、既定で有効な17のリージョンと一致しません。"
  }

  assert {
    condition     = !contains(output.by_partition["aws"], "af-south-1") && !contains(output.by_partition["aws"], "ap-east-1")
    error_message = "aws の集合に opt-in のリージョンがあります。"
  }
}

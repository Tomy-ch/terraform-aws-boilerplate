# 既定で有効なリージョンは、2019年3月20日より前に提供されたもので、無効にできない（AWS Account Management Reference Guide）。
# 集合は増えも減りもしないので、literal で持つ（repository-wide/ADR-0209）。
output "by_partition" {
  description = "partition を key にした、AWS が既定で有効とするリージョン（opt-in でないもの）の集合。持たない partition の key は置かない"
  value = {
    aws = toset([
      "ap-northeast-1", "ap-northeast-2", "ap-northeast-3", "ap-south-1", "ap-southeast-1", "ap-southeast-2",
      "ca-central-1", "eu-central-1", "eu-north-1", "eu-west-1", "eu-west-2", "eu-west-3", "sa-east-1",
      "us-east-1", "us-east-2", "us-west-1", "us-west-2",
    ])
  }
}

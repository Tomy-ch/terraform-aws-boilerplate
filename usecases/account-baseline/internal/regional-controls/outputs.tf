output "regions" {
  description = "設定を置いたリージョンの集合。usecase の Contract Test が、partition ごとの集合が届いたことを確かめるのに使う。"
  value       = toset(keys(aws_ebs_encryption_by_default.this))
}

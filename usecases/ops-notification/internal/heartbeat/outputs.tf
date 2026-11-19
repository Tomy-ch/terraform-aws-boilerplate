output "role_arn" {
  description = "心拍の role の ARN。Topic の鍵の Key Policy（modules/kms の users.roles）が名指す。名前から組むので plan で決まる。"
  value       = "arn:${local.partition}:iam::${local.account}:role/${aws_iam_role.this.name}"

  # Key Policy は存在しない Principal を受けないので、role の作成の後に書く。
  depends_on = [aws_iam_role.this]
}

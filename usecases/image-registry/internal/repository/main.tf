# kics-scan disable=e592a0c5-5bdb-414c-9066-5dba7cdea370,e38a8e0a-b88b-4902-b3fe-b0fcb17d5c10
# e592a0c5（IAM Access Analyzer）: KICS はファイルごとに analyzer の宣言を求める。analyzer はアカウント単位の統制で account-baseline が持つ（docs/v1.0-plan/scope.md 第7節）。撤回: KICS がファイルの外の宣言を数えるようになったとき。
# e38a8e0a（tag）: tag は共通の単一入力でだけ付ける（ADR-0204 決定5）。この module はその入力を持たない。撤回: 共通の tag 入力を配線したとき。

data "aws_caller_identity" "current" {}
data "aws_region" "current" {}
data "aws_partition" "current" {}

locals {
  account   = data.aws_caller_identity.current.account_id
  region    = data.aws_region.current.region
  partition = data.aws_partition.current.partition

  # computed の arn を読まず名前から組む。policy の文書が plan で決まり、Policy Test が読める。
  repository_arn = "arn:${local.partition}:ecr:${local.region}:${local.account}:repository/${var.name}"
  publisher_arn  = "arn:${local.partition}:iam::${local.account}:role/${var.publisher_role_name}"
}

resource "aws_ecr_repository" "this" {
  name                 = var.name
  image_tag_mutability = "IMMUTABLE"
  force_delete         = false

  encryption_configuration {
    encryption_type = "KMS"
    kms_key         = var.kms_key_arn
  }

  # スキャンの正は registry 単位の設定（internal/registry-scanning）。repository 単位の値は同じ向きに揃える。
  image_scanning_configuration {
    scan_on_push = true
  }
}

# 件数だけで消す。untagged の規則を持たない（image-registry/ADR-0203）。
resource "aws_ecr_lifecycle_policy" "this" {
  repository = aws_ecr_repository.this.name
  policy = jsonencode({
    rules = [{
      rulePriority = 1
      description  = "keep the newest tagged images"
      selection = {
        tagStatus      = "tagged"
        tagPatternList = ["*"]
        countType      = "imageCountMoreThan"
        countNumber    = var.retained_images
      }
      action = { type = "expire" }
    }]
  })
}

locals {
  write_actions = [
    "ecr:CompleteLayerUpload",
    "ecr:InitiateLayerUpload",
    "ecr:PutImage",
    "ecr:UploadLayerPart",
  ]

  # Allow を持たない。同じアカウントの読み取りは IAM の policy が与える（image-registry/ADR-0202）。
  repository_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid       = "DenyWriteExceptPublisher"
        Effect    = "Deny"
        Principal = { AWS = "arn:${local.partition}:iam::${local.account}:root" }
        Action    = local.write_actions
        Resource  = local.repository_arn
        Condition = { ArnNotEquals = { "aws:PrincipalArn" = local.publisher_arn } }
      },
      {
        Sid       = "DenyImageDeletion"
        Effect    = "Deny"
        Principal = { AWS = "arn:${local.partition}:iam::${local.account}:root" }
        Action    = "ecr:BatchDeleteImage"
        Resource  = local.repository_arn
      },
    ]
  })
}

resource "aws_ecr_repository_policy" "this" {
  repository = aws_ecr_repository.this.name
  policy     = local.repository_policy
}

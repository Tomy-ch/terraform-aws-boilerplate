locals {
  name_prefix = "${var.project}-${var.env}"
}

module "kms" {
  source = "../../modules/kms"

  name = "${local.name_prefix}-ecr"
  users = {
    roles = [{ arn = var.registry_manager_role_arn, via = "ecr", access = "grant" }]
  }
}

module "repository" {
  source   = "./internal/repository"
  for_each = var.repositories

  name                     = "${local.name_prefix}/${each.key}"
  publisher_role_name      = "${local.name_prefix}-ecr-publish-${each.key}"
  kms_key_arn              = module.kms.key_arn
  retained_images          = each.value.retained_images
  publisher                = each.value.publisher
  github_oidc_provider_arn = var.github_oidc_provider_arn
}

module "registry_scanning" {
  source = "./internal/registry-scanning"

  repository_filter = "${local.name_prefix}/*"
}

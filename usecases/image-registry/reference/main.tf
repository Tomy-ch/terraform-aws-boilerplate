provider "aws" {
  region = "ap-northeast-1"
}

module "image_registry" {
  source = "../"

  project                   = "tabp"
  env                       = "dev"
  github_oidc_provider_arn  = "arn:aws:iam::123456789012:oidc-provider/token.actions.githubusercontent.com"
  registry_manager_role_arn = "arn:aws:iam::123456789012:role/tabp-dev-ci-apply"
  repositories = {
    api = {
      retained_images = 30
      publisher = {
        owner         = "example"
        owner_id      = "1000001"
        repository    = "go-boilerplate"
        repository_id = "2000001"
        environment   = "dev-publish"
      }
    }
  }
}

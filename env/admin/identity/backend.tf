# bucket は init の -backend-config で渡す（docs/setup/bootstrap.md の段2）。
terraform {
  backend "s3" {
    key          = "identity/terraform.tfstate"
    region       = "ap-northeast-1"
    use_lockfile = true
    encrypt      = true
  }
}

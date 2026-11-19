# bucket と kms_key_id は init の -backend-config で渡す（.makefiles/terraform/operation/ci.mk の CI_PREPARE）。
terraform {
  backend "s3" {
    key          = "workload/terraform.tfstate"
    region       = "ap-northeast-1"
    use_lockfile = true
    encrypt      = true
  }
}

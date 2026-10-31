package infra_code_input

every_top_level := concat("\n", [
  `terraform {}`,
  `provider "aws" {}`,
  `variable "v" {}`,
  `output "o" {`,
  `  value = 1`,
  `}`,
  `locals {}`,
  `module "m" {`,
  `  source = "./m"`,
  `}`,
  `resource "aws_s3_bucket" "b" {}`,
  `data "aws_region" "r" {}`,
  `moved {`,
  `  from = a.b`,
  `  to   = a.c`,
  `}`,
  `import {`,
  `  to = a.b`,
  `  id = "x"`,
  `}`,
  `removed {`,
  `  from = a.d`,
  `}`,
  `check "c" {}`,
  `ephemeral "aws_x" "e" {}`,
  `action "aws_lambda_invoke" "a" {}`,
])

denies(file, src) := result if {
  result := deny with data.conftest.file.name as file with input as parse_config("hcl2", src)
}

test_IC001_deny_other_extension if {
  some msg in deny with data.conftest.file.name as "terraform.tfvars" with input as {}
  startswith(msg, "IC001 ")
}

test_IC001_allow_terraform_files if {
  count(denies("main.tf", every_top_level)) == 0
  count(denies("x.tftest.hcl", `run "a" {}`)) == 0
}

test_IC002_deny_json_and_query if {
  every file in ["x.tf.json", "x.tftest.json", "x.tfquery.hcl"] {
    some msg in deny with data.conftest.file.name as file with input as {}
    startswith(msg, "IC002 ")
  }
}

test_IC002_allow_hcl if {
  every msg in denies("main.tf", `resource "aws_s3_bucket" "b" {}`) {
    not startswith(msg, "IC002 ")
  }
}

test_IC003_deny_unknown_block if {
  some msg in denies("main.tf", `future_block "x" {}`)
  startswith(msg, "IC003 ")
  some msg2 in denies("x.tftest.hcl", `resource "aws_s3_bucket" "b" {}`)
  startswith(msg2, "IC003 ")
}

test_IC003_allow_every_known_block if {
  count(denies("main.tf", every_top_level)) == 0
  count(denies("x.tftest.hcl", concat("\n", [
    `variables {}`,
    `provider "aws" {}`,
    `mock_provider "aws" {}`,
    `override_resource {`,
    `  target = aws_s3_bucket.b`,
    `}`,
    `override_data {`,
    `  target = data.aws_region.r`,
    `}`,
    `override_module {`,
    `  target = module.m`,
    `}`,
    `test {}`,
    `run "a" {}`,
  ]))) == 0
}

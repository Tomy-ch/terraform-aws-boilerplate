package infra_code

base_decl := {
  "types": {
    "resource.aws_s3_bucket": {"executes_code": false},
    "resource.aws_instance": {"executes_code": true, "body_args": ["user_data"]},
    "resource.terraform_data": {"executes_code": false},
    "data.external": {"executes_code": true, "path_args": ["program"]},
    "data.aws_region": {"executes_code": false},
  },
  "providers": {
    "registry.terraform.io/hashicorp/aws": {"command_blocks": []},
    "registry.terraform.io/hashicorp/kubernetes": {"command_blocks": []},
    "registry.terraform.io/example/runner": {"command_blocks": ["runner.command"]},
  },
  "languages": {},
  "allowlist": [],
}

tf(path, src) := {"path": path, "contents": parse_config("hcl2", src)}

denies(files, d) := result if {
  result := deny with input as files with data.infra_code_decl as d
}

with_id(msgs, id) := {m | some m in msgs; startswith(m, concat("", [id, " "]))}

test_IC010_deny_unclassified_type if {
  msgs := denies([tf("m/main.tf", `resource "aws_synthetics_canary" "c" {}`)], base_decl)
  count(with_id(msgs, "IC010")) == 1
}

test_IC010_deny_unclassified_data_inside_check if {
  msgs := denies([tf("m/main.tf", `check "c" {
  data "aws_caller_identity" "me" {}
  assert {
    condition     = true
    error_message = "x"
  }
}`)], base_decl)
  count(with_id(msgs, "IC010")) == 1
}

test_IC010_allow_classified_types if {
  msgs := denies([tf("m/main.tf", `resource "aws_s3_bucket" "b" {}
data "aws_region" "r" {}`)], base_decl)
  count(msgs) == 0
}

test_IC011_deny_code_executing_type if {
  msgs := denies([tf("m/main.tf", `resource "aws_instance" "i" {}`)], base_decl)
  some m in with_id(msgs, "IC011")
  contains(m, "m#resource.aws_instance.i")
}

test_IC011_deny_provisioner_on_any_type if {
  msgs := denies([tf("m/main.tf", `resource "terraform_data" "t" {
  provisioner "local-exec" {
    command = "curl x"
  }
}`)], base_decl)
  some m in with_id(msgs, "IC011")
  contains(m, "provisioner")
}

test_IC011_deny_dynamic_provisioner if {
  msgs := denies([tf("m/main.tf", `resource "aws_s3_bucket" "b" {
  dynamic "provisioner" {
    for_each = []
    content {}
  }
}`)], base_decl)
  count(with_id(msgs, "IC011")) == 1
}

test_IC011_deny_external_inside_check if {
  msgs := denies([tf("m/main.tf", `check "c" {
  data "external" "e" {
    program = ["sh"]
  }
  assert {
    condition     = true
    error_message = "x"
  }
}`)], base_decl)
  some m in with_id(msgs, "IC011")
  contains(m, "m#data.external.e")
}

test_IC011_deny_provider_exec_at_any_depth if {
  msgs := denies([tf("m/main.tf", `provider "kubernetes" {
  exec {
    command = "aws"
  }
}`)], base_decl)
  some m in with_id(msgs, "IC011")
  contains(m, "m#provider.kubernetes")
}

test_IC011_deny_provider_exec_in_tftest if {
  msgs := denies([tf("m/tests/a.tftest.hcl", `provider "kubernetes" {
  dynamic "exec" {
    for_each = [1]
    content {}
  }
}`)], base_decl)
  some m in with_id(msgs, "IC011")
  contains(m, "m/tests/a.tftest.hcl#provider.kubernetes")
}

test_IC011_deny_declared_command_block if {
  files := [
    tf("m/versions.tf", `terraform {
  required_providers {
    runner = {
      source = "example/runner"
    }
  }
}`),
    tf("m/providers.tf", `provider "runner" {
  runner {
    command = "x"
  }
}`),
  ]
  msgs := denies(files, base_decl)
  some m in with_id(msgs, "IC011")
  contains(m, "provider の runner.command")
}

test_IC011_deny_override_file_adding_provisioner if {
  files := [
    tf("m/main.tf", `resource "aws_s3_bucket" "b" {}`),
    tf("m/main_override.tf", `resource "aws_s3_bucket" "b" {
  provisioner "local-exec" {
    command = "x"
  }
}`),
  ]
  count(with_id(denies(files, base_decl), "IC011")) == 1
}

test_IC011_deny_provisioner_in_removed_block if {
  msgs := denies([tf("m/main.tf", `removed {
  from = aws_s3_bucket.old
  lifecycle {
    destroy = false
  }
  provisioner "local-exec" {
    when    = destroy
    command = "curl x"
  }
}`)], base_decl)
  some m in with_id(msgs, "IC011")
  contains(m, "m#removed.aws_s3_bucket.old")
}

test_IC011_allow_removed_block_without_provisioner if {
  msgs := denies([tf("m/main.tf", `removed {
  from = aws_s3_bucket.old
  lifecycle {
    destroy = false
  }
}`)], base_decl)
  count(msgs) == 0
}

test_IC011_allow_listed_code if {
  d := object.union(base_decl, {"allowlist": [{"id": "m#resource.aws_instance.i", "owner": "external", "repository": "github.com/o/r"}]})
  msgs := denies([tf("m/main.tf", `resource "aws_instance" "i" {}`)], d)
  count(msgs) == 0
}

test_IC004_deny_missing_or_non_boolean_executes_code if {
  d := object.union(base_decl, {"types": object.union(base_decl.types, {
    "resource.aws_lambda_invocation": {},
    "resource.aws_lambda_function": {"executes_code": "yes"},
  })})
  count(with_id(denies([], d), "IC004")) == 2
}

test_IC012_deny_misclassified_must_execute_type if {
  d := object.union(base_decl, {"types": object.union(base_decl.types, {"resource.aws_launch_template": {"executes_code": false}})})
  msgs := denies([tf("m/main.tf", `resource "aws_s3_bucket" "b" {}`)], d)
  count(with_id(msgs, "IC012")) == 1
}

test_IC012_allow_must_execute_types_on_the_executing_side if {
  msgs := denies([tf("m/main.tf", `resource "aws_s3_bucket" "b" {}`)], base_decl)
  count(with_id(msgs, "IC012")) == 0
}

test_IC013_deny_unclassified_provider if {
  msgs := denies([tf("m/main.tf", `provider "github" {}`)], base_decl)
  some m in with_id(msgs, "IC013")
  contains(m, "registry.terraform.io/hashicorp/github")
}

test_IC013_deny_required_source_without_provider_block if {
  msgs := denies([tf("m/versions.tf", `terraform {
  required_providers {
    aws = {
      source = "evil/aws"
    }
  }
}`), tf("m/main.tf", `resource "aws_s3_bucket" "b" {}`)], base_decl)
  some m in with_id(msgs, "IC013")
  contains(m, "registry.terraform.io/evil/aws")
}

test_IC013_allow_classified_provider_with_required_source if {
  files := [
    tf("m/versions.tf", `terraform {
  required_providers {
    aws = {
      source = "hashicorp/aws"
    }
  }
}`),
    tf("m/providers.tf", `provider "aws" {
  region = "ap-northeast-1"
}`),
  ]
  count(denies(files, base_decl)) == 0
}

test_IC014_deny_non_literal_source if {
  files := [
    tf("m/versions.tf", `terraform {
  required_providers {
    aws = {
      source = "${var.ns}/aws"
    }
  }
}`),
    tf("m/providers.tf", `provider "aws" {}`),
  ]
  msgs := denies(files, base_decl)
  count(with_id(msgs, "IC014")) == 1
  count(with_id(msgs, "IC013")) == 0
}

test_IC014_deny_non_literal_required_source_without_provider_block if {
  files := [tf("m/versions.tf", `terraform {
  required_providers {
    aws = {
      source = "${var.ns}/aws"
    }
  }
}`)]
  count(with_id(denies(files, base_decl), "IC014")) == 1
}

test_IC014_allow_literal_source if {
  files := [
    tf("m/versions.tf", `terraform {
  required_providers {
    aws = {
      source = "hashicorp/aws"
    }
  }
}`),
    tf("m/providers.tf", `provider "aws" {}`),
  ]
  count(denies(files, base_decl)) == 0
}

test_IC004_deny_unknown_keys if {
  d := object.union(base_decl, {
    "extra": true,
    "types": {"resource.aws_s3_bucket": {"executes_code": false, "x": 1}},
    "providers": {"registry.terraform.io/hashicorp/aws": {"y": 1}},
    "allowlist": [{"id": "a", "owner": "external", "z": 1}],
  })
  with_id(denies([], d), "IC004") == {
    "IC004 [宣言の形] infra_code_decl のキー extra を解釈できません",
    "IC004 [宣言の形] types.resource.aws_s3_bucket のキー x を解釈できません",
    "IC004 [宣言の形] providers.registry.terraform.io/hashicorp/aws のキー y を解釈できません",
    "IC004 [宣言の形] allowlist の項目 a のキー z を解釈できません",
  }
}

test_IC004_deny_allowlist_item_without_id_or_owner if {
  d := object.union(base_decl, {"allowlist": [{"repository": "github.com/o/r"}]})
  with_id(denies([], d), "IC004") == {
    "IC004 [宣言の形] allowlist の 0 番目の項目に id がありません",
    "IC004 [宣言の形] allowlist の 0 番目の項目に owner がありません",
  }
}

test_IC004_deny_missing_declaration if {
  msgs := denies([], {})
  "IC004 [宣言の形] infra_code_decl の types がありません" in msgs
}

test_IC004_allow_known_keys if {
  d := object.union(base_decl, {
    "languages": {"python": {"contexts": ["python-test"]}},
    "allowlist": [
      {"id": "m#resource.aws_instance.i", "owner": "repository", "source": "m/src", "language": "python", "adr": "x/y"},
      {"id": "n#resource.aws_instance.j", "owner": "external", "repository": "github.com/o/r"},
    ],
  })
  count(with_id(denies([], d), "IC004")) == 0
}

test_IC011_deny_declared_command_block_in_tftest if {
  files := [
    tf("m/versions.tf", `terraform {
  required_providers {
    runner = {
      source = "example/runner"
    }
  }
}`),
    tf("m/tests/a.tftest.hcl", `provider "runner" {
  runner {
    command = "x"
  }
}`),
  ]
  some msg in with_id(denies(files, base_decl), "IC011")
  contains(msg, "m/tests/a.tftest.hcl#provider.runner（provider の runner.command）")
}

test_IC011_allow_listed_provider_exec_and_removed_provisioner if {
  d := object.union(base_decl, {"allowlist": [
    {"id": "m#provider.kubernetes", "owner": "external", "repository": "github.com/o/r"},
    {"id": "m#removed.aws_s3_bucket.old", "owner": "external", "repository": "github.com/o/r"},
  ]})
  files := [tf("m/main.tf", `provider "kubernetes" {
  exec {
    command = "aws"
  }
}

removed {
  from = aws_s3_bucket.old
  lifecycle {
    destroy = false
  }
  provisioner "local-exec" {
    when    = destroy
    command = "x"
  }
}`)]
  count(denies(files, d)) == 0
}

test_IC010_deny_when_declaration_is_empty if {
  some msg in denies([tf("m/main.tf", `resource "aws_s3_bucket" "b" {}`)], {})
  startswith(msg, "IC010 ")
}

test_IC013_deny_required_provider_without_source if {
  msgs := denies([tf("m/versions.tf", `terraform {
  required_providers {
    evil = ">= 1.0"
  }
}`)], base_decl)
  some m in with_id(msgs, "IC013")
  contains(m, "registry.terraform.io/hashicorp/evil")
}

test_IC013_deny_once_when_provider_block_and_required_source_agree if {
  files := [
    tf("m/versions.tf", `terraform {
  required_providers {
    evil = {
      source = "evil/x"
    }
  }
}`),
    tf("m/providers.tf", `provider "evil" {}`),
  ]
  count(with_id(denies(files, base_decl), "IC013")) == 1
}

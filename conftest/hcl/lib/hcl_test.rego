package lib.hcl

files(path, src) := [{"path": path, "contents": parse_config("hcl2", src)}]

test_dir if {
  dir("modules/a/main.tf") == "modules/a"
  dir("main.tf") == "."
}

test_module_dir if {
  module_dir("modules/a/tests/x.tftest.hcl") == "modules/a"
  module_dir("modules/a/x.tftest.hcl") == "modules/a"
  module_dir("tests/x.tftest.hcl") == "."
  module_dir("x.tftest.hcl") == "."
  module_dir("modules/a/main.tf") == "modules/a"
}

test_components_reads_every_kind_and_check_data if {
  src := concat("\n", [
    `resource "aws_s3_bucket" "b" {}`,
    `data "aws_region" "r" {}`,
    `ephemeral "aws_x" "e" {}`,
    `action "aws_lambda_invoke" "a" {`,
    `  config {}`,
    `}`,
    `check "c" {`,
    `  data "external" "e" {`,
    `    program = ["a"]`,
    `  }`,
    `  assert {`,
    `    condition     = true`,
    `    error_message = "x"`,
    `  }`,
    `}`,
  ])
  ids := {c.id | some c in components(files("m/main.tf", src))}
  ids == {
    "m#resource.aws_s3_bucket.b",
    "m#data.aws_region.r",
    "m#ephemeral.aws_x.e",
    "m#action.aws_lambda_invoke.a",
    "m#data.external.e",
  }
}

test_components_ignores_tftest if {
  count(components(files("m/x.tftest.hcl", `run "a" {}`))) == 0
}

test_removed_blocks if {
  ids := {r.id | some r in removed_blocks(files("m/main.tf", `removed {
  from = module.a.aws_s3_bucket.b
}`))}
  ids == {"m#removed.module.a.aws_s3_bucket.b"}
}

test_has_provisioner if {
  has_provisioner(parse_config("hcl2", `provisioner "local-exec" {
  command = "x"
}`))
  has_provisioner(parse_config("hcl2", `dynamic "provisioner" {
  for_each = []
  content {}
}`))
  not has_provisioner(parse_config("hcl2", `bucket = "x"`))
}

test_providers_identity if {
  src := concat("\n", [`provider "aws" {}`, `provider "aws" {`, `  alias = "use1"`, `}`])
  ids := {p.id | some p in providers(files("env/dev/providers.tf", src))}
  ids == {"env/dev#provider.aws", "env/dev#provider.aws.use1"}
  tftest := {p.id | some p in providers(files("m/tests/x.tftest.hcl", `provider "aws" {}`))}
  tftest == {"m/tests/x.tftest.hcl#provider.aws"}
}

test_provider_source if {
  fs := [
    {"path": "m/versions.tf", "contents": parse_config("hcl2", `terraform {
  required_providers {
    k8s = {
      source = "HashiCorp/Kubernetes"
    }
  }
}`)},
    {"path": "n/main.tf", "contents": {}},
  ]
  provider_source(fs, "m", "k8s") == "registry.terraform.io/hashicorp/kubernetes"
  provider_source(fs, "m", "aws") == "registry.terraform.io/hashicorp/aws"
  provider_source(fs, "n", "k8s") == "registry.terraform.io/hashicorp/k8s"
}

test_values_at if {
  body := parse_config("hcl2", `user_data = "a"
program   = ["python3", "x.py"]
launch_specification {
  user_data = "b"
}
launch_specification {
  user_data = "c"
}`)
  values_at(body, "user_data") == ["a"]
  values_at(body, "program") == [["python3", "x.py"]]
  {v | some v in values_at(body, "launch_specification.user_data")} == {"b", "c"}
}

test_has_key_anywhere if {
  has_key_anywhere(parse_config("hcl2", `kubernetes {
  exec {
    command = "x"
  }
}`), "exec")
  has_key_anywhere(parse_config("hcl2", `dynamic "exec" {
  for_each = []
  content {}
}`), "exec")
  not has_key_anywhere(parse_config("hcl2", `region = "x"`), "exec")
}

test_clean if {
  clean("m/./src/../src/a.py") == "m/src/a.py"
  clean("m/a/../../b") == "b"
  clean("m//a") == "m/a"
  clean("a/b/..") == "a"
  clean("a/../b") == "b"
  clean("a/..") == ""
  clean(".") == ""
  not clean("m/../../x")
  not clean("../x")
  not clean("a/../..")
}

test_component_shape if {
  component("m/a.tf", "resource", "aws_x", "n", {"k": 1}) == {
    "id": "m#resource.aws_x.n",
    "key": "resource.aws_x",
    "path": "m/a.tf",
    "body": {"k": 1},
  }
}

test_provider_id_alias_in_tftest if {
  ids := {p.id | some p in providers(files("m/tests/x.tftest.hcl", `provider "aws" {
  alias = "use1"
}`))}
  ids == {"m/tests/x.tftest.hcl#provider.aws.use1"}
  {p.module | some p in providers(files("m/tests/x.tftest.hcl", `provider "aws" {}`))} == {"m"}
}

test_normalize_source if {
  normalize_source("HashiCorp/AWS") == "registry.terraform.io/hashicorp/aws"
  normalize_source("registry.terraform.io/HashiCorp/AWS") == "registry.terraform.io/hashicorp/aws"
  normalize_source("example.com/o/p") == "example.com/o/p"
}

test_required_sources_defaults_entries_without_source if {
  fs := [{"path": "m/versions.tf", "contents": parse_config("hcl2", `terraform {
  required_providers {
    aws = {
      version = "~> 6.0"
    }
    k8s = "~> 2.0"
    gh = {
      source = "integrations/github"
    }
  }
}`)}]
  required_sources(fs) == {["m", "aws", "hashicorp/aws"], ["m", "k8s", "hashicorp/k8s"], ["m", "gh", "integrations/github"]}
  provider_source(fs, "m", "gh") == "registry.terraform.io/integrations/github"
  declared_source(fs, "m", "aws")
}

test_values_at_folds_dynamic_blocks if {
  body := parse_config("hcl2", `dynamic "launch_specification" {
  for_each = [1]
  content {
    user_data = "d"
  }
}`)
  values_at(body, "launch_specification.user_data") == ["d"]
}

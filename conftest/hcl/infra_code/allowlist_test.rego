package infra_code

lambda_decl(items) := object.union(base_decl, {
  "types": object.union(base_decl.types, {
    "resource.aws_lambda_function": {"executes_code": true, "path_args": ["filename"]},
    "resource.aws_cloudfront_function": {"executes_code": true, "body_args": ["code"]},
    "data.external": {"executes_code": true, "path_args": ["program"]},
  }),
  "allowlist": items,
})

repo_item(id, source) := {"id": id, "owner": "repository", "source": source, "language": "python", "adr": "x/fn"}

lambda(filename) := tf("u/internal/fn/main.tf", sprintf(`resource "aws_lambda_function" "f" {
  filename = "%s"
}`, [filename]))

fn_id := "u/internal/fn#resource.aws_lambda_function.f"

test_IC020_deny_item_matching_nothing if {
  d := lambda_decl([repo_item("u/internal/fn#resource.aws_lambda_function.gone", "u/internal/fn/src")])
  msgs := denies([lambda("${path.module}/src/fn.zip")], d)
  count(with_id(msgs, "IC020")) == 1
}

test_IC020_deny_item_matching_code_free_component if {
  d := lambda_decl([{"id": "m#resource.aws_s3_bucket.b", "owner": "external", "repository": "github.com/o/r"}])
  msgs := denies([tf("m/main.tf", `resource "aws_s3_bucket" "b" {}`)], d)
  count(with_id(msgs, "IC020")) == 1
}

test_IC020_allow_item_matching_code if {
  d := lambda_decl([repo_item(fn_id, "u/internal/fn/src")])
  count(denies([lambda("${path.module}/src/fn.zip")], d)) == 0
}

test_IC030_deny_non_static_path if {
  d := lambda_decl([repo_item(fn_id, "u/internal/fn/src")])
  every f in ["${var.zip}", "${path.root}/x.zip", "${path.module}/${var.name}.zip", "${data.archive_file.a.output_path}"] {
    count(with_id(denies([lambda(f)], d), "IC030")) == 1
  }
}

test_IC030_deny_absolute_path if {
  d := lambda_decl([repo_item(fn_id, "u/internal/fn/src")])
  count(with_id(denies([lambda("/tmp/fn.zip")], d), "IC030")) == 1
}

test_IC030_deny_path_escaping_repository if {
  d := lambda_decl([repo_item(fn_id, "u/internal/fn/src")])
  count(with_id(denies([lambda("../../../../outside/fn.zip")], d), "IC030")) == 1
}

test_IC030_deny_non_static_inline_code if {
  d := lambda_decl([repo_item("u/internal/fn#resource.aws_cloudfront_function.c", "u/internal/fn/main.tf")])
  files := [tf("u/internal/fn/main.tf", `resource "aws_cloudfront_function" "c" {
  code = "${local.code}"
}`)]
  count(with_id(denies(files, d), "IC030")) == 1
}

test_IC030_allow_static_paths if {
  d := lambda_decl([repo_item(fn_id, "u/internal/fn/src")])
  every f in ["${path.module}/src/fn.zip", "src/fn.zip", "./src/../src/fn.zip"] {
    count(denies([lambda(f)], d)) == 0
  }
}

test_IC031_deny_path_outside_source if {
  d := lambda_decl([repo_item(fn_id, "u/internal/fn/src")])
  every f in ["${path.module}/other/fn.zip", "${path.module}/src/../other.zip", "${path.module}/srcx/fn.zip"] {
    count(with_id(denies([lambda(f)], d), "IC031")) == 1
  }
}

test_IC031_deny_file_function_outside_source if {
  d := lambda_decl([repo_item("u/internal/fn#resource.aws_cloudfront_function.c", "u/internal/fn/src")])
  files := [tf("u/internal/fn/main.tf", `resource "aws_cloudfront_function" "c" {
  code = file("${path.module}/other/fn.js")
}`)]
  count(with_id(denies(files, d), "IC031")) == 1
}

test_IC031_allow_path_under_source if {
  d := lambda_decl([repo_item(fn_id, "u/internal/fn/src")])
  count(denies([lambda("${path.module}/src/build/fn.zip")], d)) == 0
  d2 := lambda_decl([repo_item("u/internal/fn#resource.aws_cloudfront_function.c", "u/internal/fn/src")])
  every fn in ["file", "filebase64", "filesha256", "filebase64sha256", "filemd5"] {
    count(denies([tf("u/internal/fn/main.tf", sprintf(`resource "aws_cloudfront_function" "c" {
  code = %s("${path.module}/src/fn.js")
}`, [fn]))], d2)) == 0
  }
  count(denies([tf("u/internal/fn/main.tf", `resource "aws_cloudfront_function" "c" {
  code = templatefile("${path.module}/src/fn.js", {})
}`)], d2)) == 0
}

test_IC032_deny_external_item_referencing_repository_path if {
  d := lambda_decl([{"id": fn_id, "owner": "external", "repository": "github.com/o/r"}])
  count(with_id(denies([lambda("${path.module}/src/fn.zip")], d), "IC032")) == 1
}

test_IC032_allow_external_item_without_repository_path if {
  d := lambda_decl([{"id": fn_id, "owner": "external", "repository": "github.com/o/r"}])
  msgs := denies([lambda("${var.zip}")], d)
  count(with_id(msgs, "IC030")) == 1
  count(with_id(msgs, "IC032")) == 0
}

test_IC033_deny_inline_code_with_other_source if {
  d := lambda_decl([repo_item("u/internal/fn#resource.aws_cloudfront_function.c", "u/internal/fn/src")])
  files := [tf("u/internal/fn/main.tf", `resource "aws_cloudfront_function" "c" {
  code = "function handler(event) { return event.request; }"
}`)]
  count(with_id(denies(files, d), "IC033")) == 1
}

test_IC033_allow_inline_code_with_declaring_file_as_source if {
  d := lambda_decl([repo_item("u/internal/fn#resource.aws_cloudfront_function.c", "u/internal/fn/main.tf")])
  files := [tf("u/internal/fn/main.tf", `resource "aws_cloudfront_function" "c" {
  code = "function handler(event) { return event.request; }"
}`)]
  count(denies(files, d)) == 0
}

external_program := [tf("u/internal/fn/main.tf", `data "external" "e" {
  program = ["python3", "${path.module}/src/x.py"]
}`)]

test_IC034_deny_list_valued_code if {
  d := lambda_decl([repo_item("u/internal/fn#data.external.e", "u/internal/fn/src")])
  denies(external_program, d) == {"IC034 [path] u/internal/fn/main.tf: u/internal/fn#data.external.e の program はリストなので path を検査できず、許可リストに載せられません"}
}

test_IC034_allow_string_valued_path if {
  d := lambda_decl([repo_item(fn_id, "u/internal/fn/src")])
  count(with_id(denies([lambda("${path.module}/src/fn.zip")], d), "IC034")) == 0
}

test_IC031_deny_every_file_function_outside_source if {
  d := lambda_decl([repo_item("u/internal/fn#resource.aws_cloudfront_function.c", "u/internal/fn/src")])
  every fn in ["file", "filebase64", "filesha256", "filebase64sha256", "filemd5", "templatefile"] {
    count(with_id(denies([tf("u/internal/fn/main.tf", sprintf(`resource "aws_cloudfront_function" "c" {
  code = %s("${path.module}/other/fn.js")
}`, [fn]))], d), "IC031")) == 1
  }
}

test_IC030_deny_non_string_non_list_value if {
  d := lambda_decl([repo_item("u/internal/fn#resource.aws_cloudfront_function.c", "u/internal/fn/main.tf")])
  every v in ["5", "true"] {
    count(with_id(denies([tf("u/internal/fn/main.tf", sprintf(`resource "aws_cloudfront_function" "c" {
  code = %s
}`, [v]))], d), "IC030")) == 1
  }
}

test_IC030_deny_file_function_followed_by_more_code if {
  d := lambda_decl([repo_item("u/internal/fn#resource.aws_cloudfront_function.c", "u/internal/fn/src")])
  files := [tf("u/internal/fn/main.tf", `resource "aws_cloudfront_function" "c" {
  code = "${file("${path.module}/src/ok.js")}${file("${path.module}/other/evil.js")}"
}`)]
  count(with_id(denies(files, d), "IC030")) == 1
}

test_IC031_deny_path_in_dynamic_block if {
  d := object.union(lambda_decl([repo_item("u/internal/fn#resource.aws_spot_fleet_request.s", "u/internal/fn/src")]), {"types": object.union(lambda_decl([]).types, {"resource.aws_spot_fleet_request": {"executes_code": true, "body_args": ["launch_specification.user_data"]}})})
  files := [tf("u/internal/fn/main.tf", `resource "aws_spot_fleet_request" "s" {
  dynamic "launch_specification" {
    for_each = [1]
    content {
      user_data = file("${path.module}/other/evil.sh")
    }
  }
}`)]
  count(with_id(denies(files, d), "IC031")) == 1
}

mock_provider "aws" {
  mock_data "aws_availability_zones" {
    defaults = { zone_ids = ["apne1-az1", "apne1-az2"] }
  }
}

variables {
  vpc_id                = "vpc-0123456789abcdef0"
  vpc_cidr              = "10.0.0.0/16"
  availability_zone_ids = ["apne1-az1", "apne1-az2"]
}

# 空の集合は plan で見る。宣言から [] を消すと値が plan で unknown になり、この run が落ちる。
run "keeps_every_route_table_without_routes" {
  command = plan

  assert {
    condition     = length(aws_route_table.this) == 4
    error_message = "Route Table が tier と AZ ごとに1つではありません。"
  }
  assert {
    condition     = alltrue([for r in aws_route_table.this : length(r.route) == 0 && length(r.propagating_vgws) == 0])
    error_message = "Route Table が経路か経路の伝播を持ちます。"
  }
  assert {
    condition     = alltrue([for r in aws_route_table.this : r.tags == null])
    error_message = "Route Table に tag が付いています。"
  }
}

run "associates_each_subnet_with_its_own_route_table" {
  command = apply

  assert {
    condition     = length(aws_route_table_association.this) == 4 && length(aws_subnet.this) == 4
    error_message = "関連付けの数が subnet の数と違います。"
  }
  assert {
    condition     = alltrue([for k, a in aws_route_table_association.this : a.subnet_id == aws_subnet.this[k].id && a.route_table_id == aws_route_table.this[k].id])
    error_message = "subnet が同じ tier と AZ の Route Table に関連付いていません。"
  }
  assert {
    condition     = length(distinct([for a in aws_route_table_association.this : a.route_table_id])) == length(aws_route_table_association.this)
    error_message = "Route Table を2つ以上の subnet が共有しています。"
  }
}

run "outputs_ids_in_availability_zone_order" {
  command = apply

  variables {
    availability_zone_ids = ["apne1-az2", "apne1-az1"]
  }

  assert {
    condition     = output.private_subnet_ids == [aws_subnet.this["private-apne1-az2"].id, aws_subnet.this["private-apne1-az1"].id]
    error_message = "private_subnet_ids が availability_zone_ids の順ではありません。"
  }
  assert {
    condition     = output.isolated_subnet_ids == [aws_subnet.this["isolated-apne1-az2"].id, aws_subnet.this["isolated-apne1-az1"].id]
    error_message = "isolated_subnet_ids が availability_zone_ids の順ではありません。"
  }
  assert {
    condition     = output.private_route_table_ids == [aws_route_table.this["private-apne1-az2"].id, aws_route_table.this["private-apne1-az1"].id]
    error_message = "private_route_table_ids が private の tier の Route Table を availability_zone_ids の順に返していません。"
  }
}

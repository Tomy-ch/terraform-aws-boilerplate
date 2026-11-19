mock_provider "aws" {
  mock_data "aws_availability_zones" {
    defaults = { zone_ids = ["apne1-az1", "apne1-az2", "apne1-az4"] }
  }
}

variables {
  vpc_id                = "vpc-0123456789abcdef0"
  vpc_cidr              = "10.0.0.0/16"
  availability_zone_ids = ["apne1-az4", "apne1-az1", "apne1-az2"]
}

run "allocates_fixed_slots_by_tier_and_position" {
  command = plan

  assert {
    condition = { for k, s in aws_subnet.this : k => s.cidr_block } == {
      "private-apne1-az4"  = "10.0.0.0/20"
      "private-apne1-az1"  = "10.0.16.0/20"
      "private-apne1-az2"  = "10.0.32.0/20"
      "isolated-apne1-az4" = "10.0.64.0/20"
      "isolated-apne1-az1" = "10.0.80.0/20"
      "isolated-apne1-az2" = "10.0.96.0/20"
    }
    error_message = "subnet の CIDR が tier の slot と AZ の位置の割り付けと違います。"
  }
  assert {
    condition     = { for k, s in aws_subnet.this : k => s.availability_zone_id } == { for k in keys(aws_subnet.this) : k => trimprefix(trimprefix(k, "private-"), "isolated-") }
    error_message = "subnet の AZ が key の AZ ID と違います。"
  }
}

run "keeps_existing_cidrs_when_an_availability_zone_is_appended" {
  command = plan

  variables {
    vpc_cidr              = "192.168.0.0/24"
    availability_zone_ids = ["apne1-az4", "apne1-az1", "apne1-az2", "apne1-az3"]
  }

  override_data {
    target = data.aws_availability_zones.available
    values = { zone_ids = ["apne1-az1", "apne1-az2", "apne1-az3", "apne1-az4"] }
  }

  assert {
    condition     = aws_subnet.this["private-apne1-az4"].cidr_block == "192.168.0.0/28" && aws_subnet.this["private-apne1-az3"].cidr_block == "192.168.0.48/28"
    error_message = "末尾に足した AZ が、既存の AZ の CIDR を動かしています。"
  }
  assert {
    condition     = aws_subnet.this["isolated-apne1-az4"].cidr_block == "192.168.0.64/28" && aws_subnet.this["isolated-apne1-az3"].cidr_block == "192.168.0.112/28"
    error_message = "isolated の tier が slot 1 から切り出されていません。"
  }
  assert {
    condition     = length(aws_subnet.this) == 8
    error_message = "subnet の数が tier と AZ の積ではありません。"
  }
}

run "places_every_subnet_in_the_given_vpc" {
  command = plan

  assert {
    condition     = alltrue([for s in aws_subnet.this : s.vpc_id == "vpc-0123456789abcdef0"]) && alltrue([for r in aws_route_table.this : r.vpc_id == "vpc-0123456789abcdef0"])
    error_message = "subnet か Route Table が入力の VPC に置かれていません。"
  }
}

run "rejects_an_unavailable_availability_zone" {
  command = plan

  variables {
    availability_zone_ids = ["apne1-az4", "apne1-az3"]
  }

  expect_failures = [aws_subnet.this]
}

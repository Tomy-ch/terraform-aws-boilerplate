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

run "fixes_subnet_attributes" {
  command = plan

  assert {
    condition     = length(aws_subnet.this) == 4
    error_message = "subnet が tier と AZ ごとに1つではありません。"
  }

  assert {
    condition     = alltrue([for s in aws_subnet.this : s.map_public_ip_on_launch == false && s.assign_ipv6_address_on_creation == false])
    error_message = "subnet が公開 IP か IPv6 を自動で割り当てます。"
  }
  assert {
    condition     = alltrue([for s in aws_subnet.this : s.enable_dns64 == false && s.ipv6_native == false && s.enable_resource_name_dns_aaaa_record_on_launch == false])
    error_message = "subnet の IPv6 の属性が無効ではありません。"
  }
  assert {
    condition     = alltrue([for s in aws_subnet.this : s.enable_resource_name_dns_a_record_on_launch == false && s.private_dns_hostname_type_on_launch == "ip-name"])
    error_message = "subnet のホスト名の属性が既定の ip-name ではありません。"
  }
  assert {
    condition     = alltrue([for s in aws_subnet.this : s.tags == null])
    error_message = "subnet に tag が付いています。"
  }
}

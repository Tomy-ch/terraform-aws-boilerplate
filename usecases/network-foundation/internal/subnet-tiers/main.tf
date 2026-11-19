# kics-scan disable=e592a0c5-5bdb-414c-9066-5dba7cdea370,e38a8e0a-b88b-4902-b3fe-b0fcb17d5c10
# e592a0c5（IAM Access Analyzer）: アカウント単位の統制で、この module の resource を親に取らない。撤回: account-baseline を実装したとき。
# e38a8e0a（tag）: tag は共通の単一入力でだけ付ける（ADR-0204 決定5）。この module はその入力を持たない。撤回: 共通の tag 入力を配線したとき。

data "aws_availability_zones" "available" {
  state = "available"

  filter {
    name   = "zone-id"
    values = var.availability_zone_ids
  }
}

locals {
  # slot 2・3 は公開の入口と外部 egress の tier のために空ける（network-foundation/ADR-0101 決定2）。
  tier_slots = {
    private  = 0
    isolated = 1
  }

  subnets = {
    for pair in setproduct(keys(local.tier_slots), range(length(var.availability_zone_ids))) :
    "${pair[0]}-${var.availability_zone_ids[pair[1]]}" => {
      tier                 = pair[0]
      availability_zone_id = var.availability_zone_ids[pair[1]]
      cidr_block           = cidrsubnet(var.vpc_cidr, 4, local.tier_slots[pair[0]] * 4 + pair[1])
    }
  }
}

resource "aws_subnet" "this" {
  for_each = local.subnets

  vpc_id                                         = var.vpc_id
  cidr_block                                     = each.value.cidr_block
  availability_zone_id                           = each.value.availability_zone_id
  map_public_ip_on_launch                        = false
  assign_ipv6_address_on_creation                = false
  enable_dns64                                   = false
  ipv6_native                                    = false
  enable_resource_name_dns_a_record_on_launch    = false
  enable_resource_name_dns_aaaa_record_on_launch = false
  private_dns_hostname_type_on_launch            = "ip-name"

  lifecycle {
    precondition {
      condition     = contains(data.aws_availability_zones.available.zone_ids, each.value.availability_zone_id)
      error_message = "availability_zone_ids の ${each.value.availability_zone_id} が、このリージョンで使える AZ の ID ではありません。"
    }
  }
}

resource "aws_route_table" "this" {
  for_each = local.subnets

  vpc_id = var.vpc_id
  # 経路を排他に持つ。省くと Provider は既存の経路を無視し、帯域外で足された経路が plan に出ない。
  route            = []
  propagating_vgws = []
}

resource "aws_route_table_association" "this" {
  for_each = local.subnets

  subnet_id      = aws_subnet.this[each.key].id
  route_table_id = aws_route_table.this[each.key].id
}

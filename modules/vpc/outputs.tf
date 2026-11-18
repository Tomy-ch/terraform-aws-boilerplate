output "vpc_id" {
  description = "VPC の ID。subnet・Route Table・Security Group・VPC Endpoint を置く先。"
  value       = aws_vpc.this.id
}

output "vpc_cidr" {
  description = "VPC の IPv4 の CIDR。subnet の CIDR を切り出す基点。"
  value       = aws_vpc.this.cidr_block
}

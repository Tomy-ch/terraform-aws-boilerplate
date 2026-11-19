variable "vpc_id" {
  description = "subnet と Route Table を置く VPC の ID。"
  type        = string
  nullable    = false
}

variable "vpc_cidr" {
  description = "VPC の IPv4 の CIDR。subnet の CIDR をここから固定の割り付けで切り出す。"
  type        = string
  nullable    = false
}

variable "availability_zone_ids" {
  description = "subnet を置く AZ の ID。位置が subnet の CIDR を決めるので、AZ を足すときは末尾に足す。"
  type        = list(string)
  nullable    = false
}

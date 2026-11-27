output "instance_arn" {
  description = "IAM Identity Center のインスタンスの ARN。"
  value       = local.instance_arn
}

output "assignment_target_ids" {
  description = "割当の key から対象のアカウントの ID への map（`management`・`member`）。usecase の Contract Test が配線を確かめるのに使う。"
  value = {
    management = { for k, a in aws_ssoadmin_account_assignment.management : k => a.target_id }
    member     = { for k, a in aws_ssoadmin_account_assignment.member : k => a.target_id }
  }
}

output "organization_admin_deny_resources" {
  description = "`organization_admin` の inline policy の Deny の Sid から Resource への map。usecase の Contract Test が配線を確かめるのに使う。"
  value       = { for d in local.organization_admin_denies : d.Sid => d.Resource }
}

output "ops_notification_permissions_boundary_arn" {
  description = "ops-notification が作る role に付ける permissions boundary の ARN。deployment-identity の permissions_boundary_arns と同じ名前の規則で組む。"
  value       = local.ops_notification_boundary_arn
}

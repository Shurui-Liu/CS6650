output "subnet_ids" {
  description = "IDs of the default VPC subnets"
  value       = data.aws_subnets.default.ids
}

output "security_group_id" {
  description = "Security group ID for ECS"
  value       = aws_security_group.this.id
}

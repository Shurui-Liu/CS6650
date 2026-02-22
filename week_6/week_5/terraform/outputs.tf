# terraform/outputs.tf
# Output values for easy reference

output "ecr_repository_url" {
  description = "URL of the ECR repository (use this to push Docker images)"
  value       = aws_ecr_repository.app.repository_url
}

output "load_balancer_url" {
  description = "DNS name of the load balancer (your API endpoint)"
  value       = aws_lb.main.dns_name
}

output "ecs_cluster_name" {
  description = "Name of the ECS cluster"
  value       = aws_ecs_cluster.main.name
}

output "ecs_service_name" {
  description = "Name of the ECS service"
  value       = aws_ecs_service.main.name
}

output "aws_region" {
  description = "AWS region where resources are deployed"
  value       = var.aws_region
}

output "log_group_name" {
  description = "CloudWatch log group name"
  value       = aws_cloudwatch_log_group.app.name
}

output "api_endpoint" {
  description = "Full API endpoint URL"
  value       = "http://${aws_lb.main.dns_name}"
}
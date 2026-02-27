output "ecr_repository_url" {
  value = aws_ecr_repository.app.repository_url
}

output "load_balancer_url" {
  value = aws_lb.app.dns_name
}

output "ecs_service_name" {
  value = aws_ecs_service.app.name
}

output "log_group_name" {
  value = aws_cloudwatch_log_group.app.name
}
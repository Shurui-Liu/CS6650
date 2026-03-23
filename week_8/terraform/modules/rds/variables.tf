variable "vpc_id" {
  type        = string
  description = "VPC ID for the RDS security group"
}

variable "subnet_ids" {
  type        = list(string)
  description = "List of subnet IDs for the DB subnet group (needs 2+ AZs)"
}

variable "ecs_security_group_id" {
  type        = string
  description = "Security group ID of ECS tasks — only source allowed to reach MySQL"
}

variable "db_name" {
  type        = string
  description = "Initial database name"
}

variable "db_username" {
  type        = string
  description = "Master username"
}

variable "db_password" {
  type        = string
  sensitive   = true
  description = "Master password"
}

variable "service_name" {
  type        = string
  description = "Service name used for resource naming/tags"
}

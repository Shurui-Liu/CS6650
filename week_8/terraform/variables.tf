# terraform/variables.tf
# Variables for the starter repo structure

# Region to deploy into
variable "aws_region" {
  type        = string
  default     = "us-west-2"
  description = "AWS region for deployment"
}

# ECR settings
variable "ecr_repository_name" {
  type        = string
  default     = "ecommerce-product-api"
  description = "Name of the ECR repository"
}

# ECS Service settings
variable "service_name" {
  type        = string
  default     = "product-api-service"
  description = "Name of the ECS service"
}

variable "container_port" {
  type        = number
  default     = 8080
  description = "Port the container listens on"
}

variable "ecs_count" {
  type        = number
  default     = 1
  description = "Number of ECS tasks to run"
}

# CloudWatch Logs
variable "log_retention_days" {
  type        = number
  default     = 7
  description = "How long to keep CloudWatch logs (days)"
}

# Additional settings (optional)
variable "container_cpu" {
  type        = number
  default     = 256
  description = "CPU units for container (256 = 0.25 vCPU)"
}

variable "container_memory" {
  type        = number
  default     = 512
  description = "Memory for container in MB"
}

# RDS settings
variable "db_name" {
  type        = string
  default     = "ordersdb"
  description = "Initial MySQL database name"
}

variable "db_username" {
  type        = string
  default     = "admin"
  description = "RDS master username"
}

variable "db_password" {
  type        = string
  sensitive   = true
  description = "RDS master password — set via TF_VAR_db_password env var or terraform.tfvars"
}
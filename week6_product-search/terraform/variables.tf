variable "aws_region" {
  type    = string
  default = "us-west-2"
}

variable "ecr_repository_name" {
  type    = string
  default = "product-search-v2"
}

variable "service_name" {
  type    = string
  default = "product-search-service"
}

variable "container_port" {
  type    = number
  default = 8080
}

variable "ecs_count" {
  type    = number
  default = 2
}

variable "container_cpu" {
  type    = number
  default = 256
}

variable "container_memory" {
  type    = number
  default = 512
}

variable "log_retention_days" {
  type    = number
  default = 7
}
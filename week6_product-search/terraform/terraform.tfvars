aws_region          = "us-west-2"
ecr_repository_name = "product-search-v2"
service_name        = "product-search-service"
container_port      = 8080
ecs_count           = 1
container_cpu       = 256
container_memory    = 512
log_retention_days  = 7
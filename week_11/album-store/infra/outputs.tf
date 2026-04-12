output "alb_dns" {
  value = aws_lb.api.dns_name
}

output "base_url" {
  value = "http://${aws_lb.api.dns_name}"
}

output "ecr_repo_url" {
  value = aws_ecr_repository.app.repository_url
}

output "rds_endpoint" {
  value = aws_db_instance.postgres.address
}

output "rds_reader_endpoint" {
  value = aws_db_instance.postgres_replica.address
}

output "sqs_queue_url" {
  value = aws_sqs_queue.photos.url
}

output "s3_bucket" {
  value = aws_s3_bucket.photos.bucket
}

output "redis_endpoint" {
  value = aws_elasticache_cluster.redis.cache_nodes[0].address
}

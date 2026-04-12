output "ec2_public_ip" {
  value = aws_instance.app.public_ip
}

output "ecr_repo_url" {
  value = aws_ecr_repository.app.repository_url
}

output "base_url" {
  value = "http://${aws_instance.app.public_ip}:8080"
}

output "rds_endpoint" {
  value = aws_db_instance.postgres.address
}

output "sqs_queue_url" {
  value = aws_sqs_queue.photos.url
}

output "s3_bucket" {
  value = aws_s3_bucket.photos.bucket
}

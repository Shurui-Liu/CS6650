terraform {
  required_providers {
    aws    = { source = "hashicorp/aws",    version = "~> 5.0" }
    random = { source = "hashicorp/random", version = "~> 3.0" }
  }
}

provider "aws" {
  region = var.region
}

# ── VPC ──────────────────────────────────────────────────────────────────────

resource "aws_vpc" "main" {
  cidr_block           = "10.0.0.0/16"
  enable_dns_hostnames = true
  tags = { Name = "album-store-vpc" }
}

resource "aws_internet_gateway" "igw" {
  vpc_id = aws_vpc.main.id
}

resource "aws_subnet" "public_a" {
  vpc_id                  = aws_vpc.main.id
  cidr_block              = "10.0.1.0/24"
  availability_zone       = "${var.region}a"
  map_public_ip_on_launch = true
}

resource "aws_subnet" "public_b" {
  vpc_id                  = aws_vpc.main.id
  cidr_block              = "10.0.2.0/24"
  availability_zone       = "${var.region}b"
  map_public_ip_on_launch = true
}

resource "aws_subnet" "private_a" {
  vpc_id            = aws_vpc.main.id
  cidr_block        = "10.0.3.0/24"
  availability_zone = "${var.region}a"
}

resource "aws_subnet" "private_b" {
  vpc_id            = aws_vpc.main.id
  cidr_block        = "10.0.4.0/24"
  availability_zone = "${var.region}b"
}

resource "aws_route_table" "public" {
  vpc_id = aws_vpc.main.id
  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.igw.id
  }
}

resource "aws_route_table_association" "pub_a" {
  subnet_id      = aws_subnet.public_a.id
  route_table_id = aws_route_table.public.id
}

resource "aws_route_table_association" "pub_b" {
  subnet_id      = aws_subnet.public_b.id
  route_table_id = aws_route_table.public.id
}

# ── Security Groups ───────────────────────────────────────────────────────────

# ALB accepts public HTTP on port 80.
resource "aws_security_group" "alb" {
  name   = "album-store-alb-sg"
  vpc_id = aws_vpc.main.id

  ingress {
    from_port   = 80
    to_port     = 80
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }
  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

# EC2 instances: port 8080 only from the ALB; SSH from anywhere.
resource "aws_security_group" "ec2" {
  name   = "album-store-ec2-sg"
  vpc_id = aws_vpc.main.id

  ingress {
    from_port       = 8080
    to_port         = 8080
    protocol        = "tcp"
    security_groups = [aws_security_group.alb.id]
  }
  ingress {
    from_port   = 22
    to_port     = 22
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }
  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

resource "aws_security_group" "rds" {
  name   = "album-store-rds-sg"
  vpc_id = aws_vpc.main.id

  ingress {
    from_port       = 5432
    to_port         = 5432
    protocol        = "tcp"
    security_groups = [aws_security_group.ec2.id]
  }
}

resource "aws_security_group" "redis" {
  name   = "album-store-redis-sg"
  vpc_id = aws_vpc.main.id

  ingress {
    from_port       = 6379
    to_port         = 6379
    protocol        = "tcp"
    security_groups = [aws_security_group.ec2.id]
  }
}

# ── RDS PostgreSQL (Change 3: t3.medium + multi-AZ + read replica) ────────────

resource "aws_db_subnet_group" "main" {
  name       = "album-store-db-subnet"
  subnet_ids = [aws_subnet.private_a.id, aws_subnet.private_b.id]
}

resource "aws_db_instance" "postgres" {
  identifier              = "album-store-db"
  engine                  = "postgres"
  engine_version          = "15"
  instance_class          = "db.t3.medium"
  allocated_storage       = 20
  db_name                 = "albumstore"
  username                = "albumuser"
  password                = var.db_password
  db_subnet_group_name    = aws_db_subnet_group.main.name
  vpc_security_group_ids  = [aws_security_group.rds.id]
  skip_final_snapshot     = true
  publicly_accessible     = false
  multi_az                = true
  backup_retention_period = 7
}

resource "aws_db_instance" "postgres_replica" {
  identifier                = "album-store-db-replica"
  replicate_source_db       = aws_db_instance.postgres.identifier
  instance_class            = "db.t3.medium"
  skip_final_snapshot       = true
  publicly_accessible       = false
  vpc_security_group_ids    = [aws_security_group.rds.id]
  auto_minor_version_upgrade = true
}

# ── SQS ──────────────────────────────────────────────────────────────────────

resource "aws_sqs_queue" "photos" {
  name                       = "album-store-photos"
  visibility_timeout_seconds = 60
  message_retention_seconds  = 3600
}

# ── S3 ───────────────────────────────────────────────────────────────────────

resource "random_id" "suffix" {
  byte_length = 4
}

resource "aws_s3_bucket" "photos" {
  bucket        = "album-store-photos-${random_id.suffix.hex}"
  force_destroy = true
}

resource "aws_s3_bucket_public_access_block" "photos" {
  bucket                  = aws_s3_bucket.photos.id
  block_public_acls       = false
  block_public_policy     = false
  ignore_public_acls      = false
  restrict_public_buckets = false
}

resource "aws_s3_bucket_policy" "public_read" {
  bucket     = aws_s3_bucket.photos.id
  depends_on = [aws_s3_bucket_public_access_block.photos]
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = "*"
      Action    = "s3:GetObject"
      Resource  = "${aws_s3_bucket.photos.arn}/*"
    }]
  })
}

# ── ECR ──────────────────────────────────────────────────────────────────────

resource "aws_ecr_repository" "app" {
  name                 = "album-store"
  image_tag_mutability = "MUTABLE"
  force_delete         = true
}

# ── ElastiCache Redis (Change 4) ─────────────────────────────────────────────

resource "aws_elasticache_subnet_group" "redis" {
  name       = "album-store-redis-subnet"
  subnet_ids = [aws_subnet.private_a.id, aws_subnet.private_b.id]
}

resource "aws_elasticache_cluster" "redis" {
  cluster_id           = "album-store-redis"
  engine               = "redis"
  node_type            = "cache.t3.micro"
  num_cache_nodes      = 1
  parameter_group_name = "default.redis7"
  engine_version       = "7.0"
  port                 = 6379
  subnet_group_name    = aws_elasticache_subnet_group.redis.name
  security_group_ids   = [aws_security_group.redis.id]
}

# ── AMI data source ───────────────────────────────────────────────────────────

data "aws_ami" "amazon_linux" {
  most_recent = true
  owners      = ["amazon"]
  filter {
    name   = "name"
    values = ["al2023-ami-*-x86_64"]
  }
}

# ── ALB (Change 1) ────────────────────────────────────────────────────────────

resource "aws_lb" "api" {
  name               = "album-store-alb"
  internal           = false
  load_balancer_type = "application"
  security_groups    = [aws_security_group.alb.id]
  subnets = [
    aws_subnet.public_a.id,
    aws_subnet.public_b.id,
  ]
}

resource "aws_lb_target_group" "api" {
  name     = "album-store-tg"
  port     = 8080
  protocol = "HTTP"
  vpc_id   = aws_vpc.main.id

  health_check {
    path                = "/health"
    interval            = 15
    healthy_threshold   = 2
    unhealthy_threshold = 2
    timeout             = 5
  }
}

resource "aws_lb_listener" "http" {
  load_balancer_arn = aws_lb.api.arn
  port              = 80
  protocol          = "HTTP"

  default_action {
    type             = "forward"
    target_group_arn = aws_lb_target_group.api.arn
  }
}

# ── API Launch Template + ASG (Change 1) ──────────────────────────────────────
# db_url points to PgBouncer on localhost (Change 5).
# redis_addr and rds_reader_addr are included for Changes 4 and 3 respectively.

resource "aws_launch_template" "api" {
  name_prefix   = "album-store-api-"
  image_id      = data.aws_ami.amazon_linux.id
  instance_type = "t3.medium"
  key_name      = var.ec2_key_name

  iam_instance_profile {
    name = var.ec2_instance_profile
  }

  vpc_security_group_ids = [aws_security_group.ec2.id]

  user_data = base64encode(templatefile("${path.module}/user_data.sh", {
    ecr_repo        = aws_ecr_repository.app.repository_url
    region          = var.region
    db_url          = "postgres://albumuser:${var.db_password}@127.0.0.1:5432/albumstore"
    sqs_url         = aws_sqs_queue.photos.url
    s3_bucket       = aws_s3_bucket.photos.bucket
    s3_base_url     = "https://${aws_s3_bucket.photos.bucket}.s3.${var.region}.amazonaws.com"
    redis_addr      = "${aws_elasticache_cluster.redis.cache_nodes[0].address}:6379"
    rds_host        = aws_db_instance.postgres.address
    db_password     = var.db_password
    rds_reader_addr = aws_db_instance.postgres_replica.address
  }))

  tag_specifications {
    resource_type = "instance"
    tags = { Name = "album-store-api" }
  }
}

resource "aws_autoscaling_group" "api" {
  name                      = "album-store-api-asg"
  min_size                  = 1
  max_size                  = 4
  desired_capacity          = 2
  health_check_type         = "ELB"
  health_check_grace_period = 60
  vpc_zone_identifier = [
    aws_subnet.public_a.id,
    aws_subnet.public_b.id,
  ]
  target_group_arns = [aws_lb_target_group.api.arn]

  launch_template {
    id      = aws_launch_template.api.id
    version = "$Latest"
  }
}

resource "aws_autoscaling_policy" "api_scale_out" {
  name                   = "api-scale-out"
  autoscaling_group_name = aws_autoscaling_group.api.name
  adjustment_type        = "ChangeInCapacity"
  scaling_adjustment     = 1
  cooldown               = 60
}

resource "aws_autoscaling_policy" "api_scale_in" {
  name                   = "api-scale-in"
  autoscaling_group_name = aws_autoscaling_group.api.name
  adjustment_type        = "ChangeInCapacity"
  scaling_adjustment     = -1
  cooldown               = 120
}

resource "aws_cloudwatch_metric_alarm" "api_cpu_high" {
  alarm_name          = "album-store-api-cpu-high"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 2
  metric_name         = "CPUUtilization"
  namespace           = "AWS/EC2"
  period              = 60
  statistic           = "Average"
  threshold           = 60
  alarm_actions       = [aws_autoscaling_policy.api_scale_out.arn]
  dimensions = {
    AutoScalingGroupName = aws_autoscaling_group.api.name
  }
}

resource "aws_cloudwatch_metric_alarm" "api_cpu_low" {
  alarm_name          = "album-store-api-cpu-low"
  comparison_operator = "LessThanThreshold"
  evaluation_periods  = 3
  metric_name         = "CPUUtilization"
  namespace           = "AWS/EC2"
  period              = 60
  statistic           = "Average"
  threshold           = 20
  alarm_actions       = [aws_autoscaling_policy.api_scale_in.arn]
  dimensions = {
    AutoScalingGroupName = aws_autoscaling_group.api.name
  }
}

# ── Worker Launch Template + ASG (Change 2) ───────────────────────────────────
# Scales on SQS queue depth. No ALB attachment, no port published.
# db_url points to PgBouncer on localhost (Change 5).

resource "aws_launch_template" "worker" {
  name_prefix   = "album-store-worker-"
  image_id      = data.aws_ami.amazon_linux.id
  instance_type = "t3.medium"
  key_name      = var.ec2_key_name

  iam_instance_profile {
    name = var.ec2_instance_profile
  }

  vpc_security_group_ids = [aws_security_group.ec2.id]

  user_data = base64encode(templatefile("${path.module}/user_data_worker.sh", {
    ecr_repo        = aws_ecr_repository.app.repository_url
    region          = var.region
    db_url          = "postgres://albumuser:${var.db_password}@127.0.0.1:5432/albumstore"
    sqs_url         = aws_sqs_queue.photos.url
    s3_bucket       = aws_s3_bucket.photos.bucket
    s3_base_url     = "https://${aws_s3_bucket.photos.bucket}.s3.${var.region}.amazonaws.com"
    rds_host        = aws_db_instance.postgres.address
    db_password     = var.db_password
    rds_reader_addr = aws_db_instance.postgres_replica.address
  }))

  tag_specifications {
    resource_type = "instance"
    tags = { Name = "album-store-worker" }
  }
}

resource "aws_autoscaling_group" "worker" {
  name             = "album-store-worker-asg"
  min_size         = 1
  max_size         = 6
  desired_capacity = 1
  vpc_zone_identifier = [
    aws_subnet.public_a.id,
    aws_subnet.public_b.id,
  ]

  launch_template {
    id      = aws_launch_template.worker.id
    version = "$Latest"
  }
}

resource "aws_autoscaling_policy" "worker_scale_out" {
  name                   = "worker-scale-out"
  autoscaling_group_name = aws_autoscaling_group.worker.name
  adjustment_type        = "ChangeInCapacity"
  scaling_adjustment     = 1
  cooldown               = 60
}

resource "aws_autoscaling_policy" "worker_scale_in" {
  name                   = "worker-scale-in"
  autoscaling_group_name = aws_autoscaling_group.worker.name
  adjustment_type        = "ChangeInCapacity"
  scaling_adjustment     = -1
  cooldown               = 180
}

resource "aws_cloudwatch_metric_alarm" "sqs_depth_high" {
  alarm_name          = "album-store-sqs-depth-high"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 1
  metric_name         = "ApproximateNumberOfMessagesVisible"
  namespace           = "AWS/SQS"
  period              = 60
  statistic           = "Average"
  threshold           = 50
  alarm_actions       = [aws_autoscaling_policy.worker_scale_out.arn]
  dimensions = {
    QueueName = aws_sqs_queue.photos.name
  }
}

resource "aws_cloudwatch_metric_alarm" "sqs_depth_low" {
  alarm_name          = "album-store-sqs-depth-low"
  comparison_operator = "LessThanThreshold"
  evaluation_periods  = 3
  metric_name         = "ApproximateNumberOfMessagesVisible"
  namespace           = "AWS/SQS"
  period              = 60
  statistic           = "Average"
  threshold           = 5
  alarm_actions       = [aws_autoscaling_policy.worker_scale_in.arn]
  dimensions = {
    QueueName = aws_sqs_queue.photos.name
  }
}

# ==========================================
# RDS Module — MySQL 8.0 (Free Tier)
# ==========================================
#
# Network note: uses the default VPC subnets (which are public-routable)
# but sets publicly_accessible = false and restricts ingress to the ECS
# tasks security group only, making the instance effectively private.

# DB subnet group — RDS requires subnets in at least 2 AZs
resource "aws_db_subnet_group" "main" {
  name       = "${var.service_name}-db-subnet-group"
  subnet_ids = var.subnet_ids

  tags = {
    Name = "${var.service_name}-db-subnet-group"
  }
}

# Security group — allow MySQL only from ECS tasks
resource "aws_security_group" "rds" {
  name        = "${var.service_name}-rds-sg"
  description = "Allow MySQL access from ECS tasks only"
  vpc_id      = var.vpc_id

  ingress {
    description     = "MySQL from ECS tasks"
    from_port       = 3306
    to_port         = 3306
    protocol        = "tcp"
    security_groups = [var.ecs_security_group_id]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = {
    Name = "${var.service_name}-rds-sg"
  }
}

# RDS MySQL 8.0 instance
resource "aws_db_instance" "main" {
  identifier        = "${var.service_name}-mysql"
  engine            = "mysql"
  engine_version    = "8.0"
  instance_class    = "db.t3.micro"
  allocated_storage = 20
  storage_type      = "gp2"

  db_name  = var.db_name
  username = var.db_username
  password = var.db_password

  db_subnet_group_name   = aws_db_subnet_group.main.name
  vpc_security_group_ids = [aws_security_group.rds.id]
  publicly_accessible    = false

  # Assignment settings — do not use in production
  skip_final_snapshot = true
  deletion_protection = false

  # Keep backups off to stay within Free Tier
  backup_retention_period = 0

  tags = {
    Name        = "${var.service_name}-mysql"
    Environment = "dev"
  }
}

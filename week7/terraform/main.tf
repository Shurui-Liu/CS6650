# ==========================================
# SNS Topic — order-processing-events
# ==========================================

resource "aws_sns_topic" "orders" {
  name = "order-processing-events"

  tags = {
    Name = "order-processing-events"
  }
}

# ==========================================
# SQS Queue — order-processing-queue
# ==========================================

resource "aws_sqs_queue" "orders" {
  name                       = "order-processing-queue"
  visibility_timeout_seconds = 30          # default
  message_retention_seconds  = 345600      # 4 days
  receive_wait_time_seconds  = 20          # long polling

  tags = {
    Name = "order-processing-queue"
  }
}

# Allow SNS to send messages to SQS
resource "aws_sqs_queue_policy" "orders" {
  queue_url = aws_sqs_queue.orders.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect    = "Allow"
        Principal = { Service = "sns.amazonaws.com" }
        Action    = "sqs:SendMessage"
        Resource  = aws_sqs_queue.orders.arn
        Condition = {
          ArnEquals = {
            "aws:SourceArn" = aws_sns_topic.orders.arn
          }
        }
      }
    ]
  })
}

# Subscribe SQS to SNS topic
resource "aws_sns_topic_subscription" "orders" {
  topic_arn = aws_sns_topic.orders.arn
  protocol  = "sqs"
  endpoint  = aws_sqs_queue.orders.arn
}

# ==========================================
# ECR Repository — order processor
# ==========================================

resource "aws_ecr_repository" "processor" {
  name                 = "order-processor"
  image_tag_mutability = "MUTABLE"

  image_scanning_configuration {
    scan_on_push = true
  }

  tags = {
    Name = "order-processor"
  }
}

# ==========================================
# CloudWatch log group — processor
# ==========================================

resource "aws_cloudwatch_log_group" "processor" {
  name              = "/ecs/order-processor"
  retention_in_days = var.log_retention_days
}

# ==========================================
# ECS Task Definition — order-receiver
# (updates existing API task with SNS_TOPIC_ARN env var)
# ==========================================

resource "aws_ecs_task_definition" "receiver" {
  family                   = "order-receiver"
  network_mode             = "awsvpc"
  requires_compatibilities = ["FARGATE"]
  cpu                      = var.container_cpu
  memory                   = var.container_memory
  execution_role_arn       = data.aws_iam_role.lab_role.arn
  task_role_arn            = data.aws_iam_role.lab_role.arn

  container_definitions = jsonencode([
    {
      name      = "order-receiver-container"
      image     = "${aws_ecr_repository.app.repository_url}:latest"
      essential = true

      portMappings = [
        {
          containerPort = var.container_port
          hostPort      = var.container_port
          protocol      = "tcp"
        }
      ]

      healthCheck = {
        command     = ["CMD-SHELL", "curl -f http://localhost:${var.container_port}/health || exit 1"]
        interval    = 30
        timeout     = 5
        retries     = 2
        startPeriod = 60
      }

      logConfiguration = {
        logDriver = "awslogs"
        options = {
          "awslogs-group"         = aws_cloudwatch_log_group.app.name
          "awslogs-region"        = var.aws_region
          "awslogs-stream-prefix" = "ecs"
        }
      }

      environment = [
        { name = "PORT",          value = tostring(var.container_port) },
        { name = "AWS_REGION",    value = var.aws_region },
        { name = "SNS_TOPIC_ARN", value = aws_sns_topic.orders.arn }
      ]
    }
  ])
}

# Update the existing ECS service to use the new task definition
resource "aws_ecs_service" "receiver" {
  name            = "order-receiver"
  cluster         = aws_ecs_cluster.main.id
  task_definition = aws_ecs_task_definition.receiver.arn
  desired_count   = 1
  launch_type     = "FARGATE"

  network_configuration {
    security_groups  = [aws_security_group.ecs_tasks.id]
    subnets          = aws_subnet.private[*].id
    assign_public_ip = false
  }

  load_balancer {
    target_group_arn = aws_lb_target_group.app.arn
    container_name   = "order-receiver-container"
    container_port   = var.container_port
  }

  depends_on = [aws_lb_listener.app]
}

# ==========================================
# ECS Task Definition — order-processor
# ==========================================

resource "aws_ecs_task_definition" "processor" {
  family                   = "order-processor"
  network_mode             = "awsvpc"
  requires_compatibilities = ["FARGATE"]
  cpu                      = var.container_cpu
  memory                   = var.container_memory
  execution_role_arn       = data.aws_iam_role.lab_role.arn
  task_role_arn            = data.aws_iam_role.lab_role.arn

  container_definitions = jsonencode([
    {
      name      = "order-processor-container"
      image     = "${aws_ecr_repository.processor.repository_url}:latest"
      essential = true

      logConfiguration = {
        logDriver = "awslogs"
        options = {
          "awslogs-group"         = aws_cloudwatch_log_group.processor.name
          "awslogs-region"        = var.aws_region
          "awslogs-stream-prefix" = "ecs"
        }
      }

      environment = [
        { name = "AWS_REGION",   value = var.aws_region },
        { name = "SQS_QUEUE_URL", value = aws_sqs_queue.orders.url }
      ]
    }
  ])
}

# ==========================================
# ECS Service — order-processor (1 task, no ALB needed)
# ==========================================

resource "aws_ecs_service" "processor" {
  name            = "order-processor"
  cluster         = aws_ecs_cluster.main.id
  task_definition = aws_ecs_task_definition.processor.arn
  desired_count   = 1
  launch_type     = "FARGATE"

  network_configuration {
    security_groups  = [aws_security_group.ecs_tasks.id]
    subnets          = aws_subnet.private[*].id
    assign_public_ip = false
  }
}

# ==========================================
# Outputs
# ==========================================

output "sns_topic_arn" {
  value = aws_sns_topic.orders.arn
}

output "sqs_queue_url" {
  value = aws_sqs_queue.orders.url
}

output "processor_ecr_url" {
  value = aws_ecr_repository.processor.repository_url
}

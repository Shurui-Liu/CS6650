#!/bin/bash
set -e

# ── Docker ────────────────────────────────────────────────────────────────────
yum update -y
yum install -y docker
systemctl enable docker
systemctl start docker

# ── ECR login (instance role — no static keys) ────────────────────────────────
aws ecr get-login-password --region ${region} \
  | docker login --username AWS \
    --password-stdin ${ecr_repo}

# ── Environment file ──────────────────────────────────────────────────────────
cat > /opt/album-store.env <<EOF
DATABASE_URL=${db_url}
DATABASE_READER_URL=${db_reader_url}
SQS_QUEUE_URL=${sqs_url}
S3_BUCKET=${s3_bucket}
S3_BASE_URL=${s3_base_url}
AWS_REGION=${region}
PORT=8080
WORKER_CONCURRENCY=0
REDIS_ADDR=${redis_addr}
EOF

# ── Pull and run ──────────────────────────────────────────────────────────────
docker pull ${ecr_repo}:latest || true
docker run -d --name album-store \
  --env-file /opt/album-store.env \
  -p 8080:8080 \
  --restart unless-stopped \
  ${ecr_repo}:latest
